// The bench's hidden acceptance test of workspace-budgets, core's part.
import { test } from "node:test";
import assert from "node:assert/strict";
import * as core from "../src/index.ts";

type Tx = { id: string; date: string; kind: "income" | "expense" | "refund" | "transfer"; category: string; amountCents: number };
const summarize = (core as Record<string, unknown>).summarizeBudgets as (b: Record<string, number>, t: Tx[], m: string) => Record<string, unknown>[];

let n = 0;
const tx = (date: string, kind: Tx["kind"], category: string, amountCents: number): Tx => ({ id: `t${++n}`, date, kind, category, amountCents });

const march: Tx[] = [
  tx("2026-02-28", "expense", "food", 5000), // another month
  tx("2026-03-01", "expense", "food", 12000),
  tx("2026-03-15", "expense", "food", 6000),
  tx("2026-03-16", "refund", "food", 2000),
  tx("2026-03-31", "expense", "food", 1000),
  tx("2026-04-01", "expense", "food", 9999), // another month
  tx("2026-03-03", "transfer", "savings", 50000), // never spending
  tx("2026-03-04", "income", "salary", 300000), // never spending
  tx("2026-03-05", "expense", "rent", 90000),
  tx("2026-03-06", "refund", "gym", 3000), // a refund with no expense: spent stays 0
  tx("2026-03-07", "expense", "books", 125),
];

test("summarizeBudgets is exported from core's index", () => {
  assert.equal(typeof summarize, "function");
});

test("one row per budgeted category, sorted, with spending as the ledger defines it", () => {
  const rows = summarize({ rent: 80000, food: 30000, savings: 10000, salary: 1000, gym: 4000, books: 1000, travel: 5000 }, march, "2026-03");
  assert.deepEqual(rows, [
    { category: "books", limitCents: 1000, spentCents: 125, remainingCents: 875, percentUsed: 13, over: false },
    { category: "food", limitCents: 30000, spentCents: 17000, remainingCents: 13000, percentUsed: 57, over: false },
    { category: "gym", limitCents: 4000, spentCents: 0, remainingCents: 4000, percentUsed: 0, over: false },
    { category: "rent", limitCents: 80000, spentCents: 90000, remainingCents: -10000, percentUsed: 113, over: true },
    { category: "salary", limitCents: 1000, spentCents: 0, remainingCents: 1000, percentUsed: 0, over: false },
    { category: "savings", limitCents: 10000, spentCents: 0, remainingCents: 10000, percentUsed: 0, over: false },
    { category: "travel", limitCents: 5000, spentCents: 0, remainingCents: 5000, percentUsed: 0, over: false },
  ]);
});

test("percentUsed rounds half up, and exactly the limit is not over", () => {
  const at = (spent: number, limit: number) => summarize({ x: limit }, [tx("2026-05-10", "expense", "x", spent)], "2026-05")[0];
  assert.equal(at(1, 8).percentUsed, 13); // 12.5
  assert.equal(at(1, 200).percentUsed, 1); // 0.5
  assert.equal(at(1, 201).percentUsed, 0); // 0.4975
  assert.equal(at(2, 3).percentUsed, 67);
  assert.equal(at(1005, 10000).percentUsed, 10); // 10.05
  const exact = at(5000, 5000);
  assert.equal(exact.percentUsed, 100);
  assert.equal(exact.over, false);
  assert.equal(exact.remainingCents, 0);
});

test("no budgets, no rows; the transactions are left as they are", () => {
  const copy = structuredClone(march);
  assert.deepEqual(summarize({}, march, "2026-03"), []);
  summarize({ food: 1 }, march, "2026-03");
  assert.deepEqual(march, copy);
});

test("a month that is not YYYY-MM is a RangeError", () => {
  for (const bad of ["2026-3", "2026-13", "2026-00", "2026-03-01", "March", ""]) {
    assert.throws(() => summarize({ food: 100 }, march, bad), RangeError, bad);
  }
});
