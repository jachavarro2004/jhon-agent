// The ledger: transactions and budgets kept in one JSON file (docs/LEDGER.md,
// "The ledger file").

import { existsSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { balanceEffect, byDate, monthOf, summarizeBudgets, validateTransaction, type BudgetRow, type Transaction } from "../../core/src/index.ts";

export type NewTransaction = Omit<Transaction, "id">;

export class Ledger {
  readonly path: string;
  #transactions: Transaction[];
  #budgets: Record<string, number>;
  // The file's other keys, kept as they are.
  #rest: Record<string, unknown>;

  private constructor(path: string, data: Record<string, unknown>) {
    const { transactions, budgets, ...rest } = data;
    this.path = path;
    this.#transactions = Array.isArray(transactions) ? (transactions as Transaction[]) : [];
    this.#budgets = budgets !== null && typeof budgets === "object" && !Array.isArray(budgets) ? { ...(budgets as Record<string, number>) } : {};
    this.#rest = rest;
  }

  /** The ledger in path; an empty one when the file does not exist yet. */
  static open(path: string): Ledger {
    if (!existsSync(path)) {
      return new Ledger(path, {});
    }
    const data: unknown = JSON.parse(readFileSync(path, "utf8"));
    if (data === null || typeof data !== "object" || Array.isArray(data)) {
      throw new Error(`${path} is not a ledger: no JSON object`);
    }
    return new Ledger(path, data as Record<string, unknown>);
  }

  /** The transactions, by date then id; of one month (YYYY-MM) when given. */
  transactions(month?: string): Transaction[] {
    return this.#transactions
      .filter((t) => month === undefined || monthOf(t.date) === month)
      .map((t) => ({ ...t }))
      .sort(byDate);
  }

  /** Records tx with the next id and writes the file. */
  add(tx: NewTransaction): Transaction {
    validateTransaction(tx);
    const next = Math.max(0, ...this.#transactions.map((t) => Number(t.id.slice(1)))) + 1;
    const recorded: Transaction = { id: `t${next}`, date: tx.date, kind: tx.kind, category: tx.category, amountCents: tx.amountCents };
    this.#transactions.push(recorded);
    this.#save();
    return { ...recorded };
  }

  /** The balance in cents: every transaction's effect. */
  balanceCents(): number {
    return this.#transactions.reduce((sum, t) => sum + balanceEffect(t), 0);
  }

  /** Sets category's monthly limit, replacing any, and writes the file. */
  setBudget(category: string, limitCents: number): void {
    if (typeof category !== "string" || category.trim() === "") {
      throw new RangeError("a budget needs a category");
    }
    if (!Number.isSafeInteger(limitCents) || limitCents <= 0) {
      throw new RangeError(`not a limit in cents: ${limitCents}`);
    }
    this.#budgets[category] = limitCents;
    this.#save();
  }

  /** The monthly limits by category, a copy. */
  budgets(): Record<string, number> {
    return { ...this.#budgets };
  }

  /** core's budget rows for the ledger's month (YYYY-MM). */
  budgetReport(month: string): BudgetRow[] {
    return summarizeBudgets(this.#budgets, this.#transactions, month);
  }

  #save(): void {
    const data = { ...this.#rest, transactions: this.#transactions, budgets: this.#budgets };
    const tmp = `${this.path}.tmp`;
    writeFileSync(tmp, JSON.stringify(data, null, 2) + "\n");
    renameSync(tmp, this.path);
  }
}
