import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Ledger } from "../src/index.ts";

function scratch(t: { after: (fn: () => void) => void }): string {
  const dir = mkdtempSync(join(tmpdir(), "ledger-budget-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  return join(dir, "ledger.json");
}

test("setBudget keeps the limit in the file and replaces it", (t) => {
  const path = scratch(t);
  const ledger = Ledger.open(path);
  ledger.add({ date: "2026-03-01", kind: "expense", category: "food", amountCents: 100 });
  ledger.setBudget("food", 500);
  ledger.setBudget("food", 400);
  const again = Ledger.open(path);
  assert.deepEqual(again.budgets(), { food: 400 });
  assert.equal(again.transactions().length, 1);
});

test("a bad limit is refused and nothing is written", (t) => {
  const path = scratch(t);
  const ledger = Ledger.open(path);
  ledger.setBudget("food", 500);
  const before = readFileSync(path, "utf8");
  assert.throws(() => ledger.setBudget("food", 0), RangeError);
  assert.throws(() => ledger.setBudget("food", 1.5), RangeError);
  assert.equal(readFileSync(path, "utf8"), before);
});

test("budgetReport summarizes the ledger's month", (t) => {
  const ledger = Ledger.open(scratch(t));
  ledger.add({ date: "2026-03-01", kind: "expense", category: "food", amountCents: 300 });
  ledger.setBudget("food", 1000);
  assert.deepEqual(ledger.budgetReport("2026-03"), [
    { category: "food", limitCents: 1000, spentCents: 300, remainingCents: 700, percentUsed: 30, over: false },
  ]);
});
