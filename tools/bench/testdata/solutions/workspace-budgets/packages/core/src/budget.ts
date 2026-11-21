// Monthly budgets per category: what each budgeted category spent in a month.

import type { Transaction } from "./transaction.ts";
import { monthOf } from "./transaction.ts";

export interface BudgetRow {
  category: string;
  limitCents: number;
  spentCents: number;
  remainingCents: number;
  percentUsed: number;
  over: boolean;
}

const MONTH = /^\d{4}-(0[1-9]|1[0-2])$/;

/** Whether text is a month written YYYY-MM. */
export function isMonth(text: string): boolean {
  return MONTH.test(text);
}

/**
 * One row per budgeted category, sorted by category. Spending is the
 * month's expenses minus its refunds (docs/LEDGER.md), never below zero;
 * income and transfers never count.
 */
export function summarizeBudgets(budgets: Record<string, number>, transactions: readonly Transaction[], month: string): BudgetRow[] {
  if (!isMonth(month)) {
    throw new RangeError(`not a month (YYYY-MM): ${JSON.stringify(month)}`);
  }
  const net = new Map<string, number>();
  for (const t of transactions) {
    if (monthOf(t.date) !== month) {
      continue;
    }
    const effect = t.kind === "expense" ? t.amountCents : t.kind === "refund" ? -t.amountCents : 0;
    net.set(t.category, (net.get(t.category) ?? 0) + effect);
  }
  return Object.keys(budgets)
    .sort()
    .map((category) => {
      const limitCents = budgets[category];
      const spentCents = Math.max(0, net.get(category) ?? 0);
      return {
        category,
        limitCents,
        spentCents,
        remainingCents: limitCents - spentCents,
        // Half up in whole numbers: floor((200 * spent + limit) / (2 * limit)).
        percentUsed: Math.floor((200 * spentCents + limitCents) / (2 * limitCents)),
        over: spentCents > limitCents,
      };
    });
}
