// The bench's hidden acceptance test of workspace-budgets, api's part.
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Ledger } from "../src/index.ts";

type Budgeted = Ledger & {
  setBudget(category: string, limitCents: number): void;
  budgets(): Record<string, number>;
  budgetReport(month: string): Record<string, unknown>[];
};
const open = (path: string) => Ledger.open(path) as Budgeted;

function scratch(t: { after: (fn: () => void) => void }): string {
  const dir = mkdtempSync(join(tmpdir(), "ledger-acceptance-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  return join(dir, "ledger.json");
}

test("a budget is kept in the file beside the transactions and other keys", (t) => {
  const path = scratch(t);
  writeFileSync(path, JSON.stringify({ owner: "Ana", transactions: [{ id: "t1", date: "2026-03-02", kind: "expense", category: "food", amountCents: 4000 }] }));
  const ledger = open(path);
  ledger.setBudget("food", 30000);
  ledger.setBudget("rent", 80000);
  ledger.setBudget("food", 25000);
  const again = open(path);
  assert.deepEqual(again.budgets(), { food: 25000, rent: 80000 });
  assert.equal(again.transactions().length, 1);
  const data = JSON.parse(readFileSync(path, "utf8"));
  assert.equal(data.owner, "Ana");
  assert.equal(data.transactions.length, 1);
});

test("a ledger with no budgets has none", (t) => {
  const path = scratch(t);
  assert.deepEqual(open(path).budgets(), {});
  writeFileSync(path, JSON.stringify({ transactions: [] }));
  assert.deepEqual(open(path).budgets(), {});
});

test("budgets() is a copy", (t) => {
  const ledger = open(scratch(t));
  ledger.setBudget("food", 100);
  ledger.budgets().food = 1;
  assert.deepEqual(ledger.budgets(), { food: 100 });
});

test("a limit that is not a positive whole number of cents is refused and nothing is written", (t) => {
  const path = scratch(t);
  const ledger = open(path);
  ledger.setBudget("food", 100);
  const before = readFileSync(path, "utf8");
  for (const bad of [0, -5, 12.5, Number.NaN]) {
    assert.throws(() => ledger.setBudget("food", bad), RangeError, String(bad));
  }
  assert.equal(readFileSync(path, "utf8"), before);
  assert.deepEqual(open(path).budgets(), { food: 100 });
});

test("budgetReport is core's rows for the ledger's month", (t) => {
  const ledger = open(scratch(t));
  ledger.add({ date: "2026-03-01", kind: "expense", category: "food", amountCents: 12000 });
  ledger.add({ date: "2026-03-09", kind: "refund", category: "food", amountCents: 2000 });
  ledger.add({ date: "2026-03-10", kind: "transfer", category: "savings", amountCents: 50000 });
  ledger.add({ date: "2026-04-01", kind: "expense", category: "food", amountCents: 7000 });
  ledger.setBudget("food", 8000);
  ledger.setBudget("savings", 1000);
  assert.deepEqual(ledger.budgetReport("2026-03"), [
    { category: "food", limitCents: 8000, spentCents: 10000, remainingCents: -2000, percentUsed: 125, over: true },
    { category: "savings", limitCents: 1000, spentCents: 0, remainingCents: 1000, percentUsed: 0, over: false },
  ]);
  assert.throws(() => ledger.budgetReport("2026-3"), RangeError);
});
