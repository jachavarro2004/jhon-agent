import { test } from "node:test";
import assert from "node:assert/strict";
import { summarizeBudgets, type Transaction } from "../src/index.ts";

const tx = (id: string, date: string, kind: Transaction["kind"], category: string, amountCents: number): Transaction => ({ id, date, kind, category, amountCents });

test("spending is the month's expenses minus refunds; income and transfers never count", () => {
  const rows = summarizeBudgets(
    { food: 10000, savings: 5000 },
    [
      tx("t1", "2026-03-01", "expense", "food", 6000),
      tx("t2", "2026-03-02", "refund", "food", 1000),
      tx("t3", "2026-04-01", "expense", "food", 9000),
      tx("t4", "2026-03-03", "transfer", "savings", 7000),
      tx("t5", "2026-03-04", "income", "food", 7000),
    ],
    "2026-03",
  );
  assert.deepEqual(rows, [
    { category: "food", limitCents: 10000, spentCents: 5000, remainingCents: 5000, percentUsed: 50, over: false },
    { category: "savings", limitCents: 5000, spentCents: 0, remainingCents: 5000, percentUsed: 0, over: false },
  ]);
});

test("a refund alone spends nothing; over is above the limit", () => {
  const rows = summarizeBudgets({ gym: 100, rent: 100 }, [tx("t1", "2026-03-01", "refund", "gym", 50), tx("t2", "2026-03-01", "expense", "rent", 101)], "2026-03");
  assert.equal(rows[0].spentCents, 0);
  assert.equal(rows[1].over, true);
  assert.equal(rows[1].remainingCents, -1);
});

test("percentUsed rounds half up", () => {
  const pct = (spent: number, limit: number) => summarizeBudgets({ x: limit }, [tx("t1", "2026-03-01", "expense", "x", spent)], "2026-03")[0].percentUsed;
  assert.equal(pct(1, 8), 13);
  assert.equal(pct(1, 201), 0);
  assert.equal(pct(5000, 5000), 100);
});

test("a bad month is a RangeError", () => {
  assert.throws(() => summarizeBudgets({}, [], "2026-13"), RangeError);
});
