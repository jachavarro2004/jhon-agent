// The ledger: transactions kept in one JSON file (docs/LEDGER.md, "The
// ledger file").

import { existsSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { balanceEffect, byDate, monthOf, validateTransaction, type Transaction } from "../../core/src/index.ts";

export type NewTransaction = Omit<Transaction, "id">;

export class Ledger {
  readonly path: string;
  #transactions: Transaction[];
  // The file's other keys, kept as they are.
  #rest: Record<string, unknown>;

  private constructor(path: string, data: Record<string, unknown>) {
    const { transactions, ...rest } = data;
    this.path = path;
    this.#transactions = Array.isArray(transactions) ? (transactions as Transaction[]) : [];
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

  #save(): void {
    const data = { ...this.#rest, transactions: this.#transactions };
    const tmp = `${this.path}.tmp`;
    writeFileSync(tmp, JSON.stringify(data, null, 2) + "\n");
    renameSync(tmp, this.path);
  }
}
