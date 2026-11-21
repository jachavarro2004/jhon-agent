import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Ledger } from "../src/index.ts";

function scratch(t: { after: (fn: () => void) => void }): string {
  const dir = mkdtempSync(join(tmpdir(), "ledger-api-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  return join(dir, "ledger.json");
}

test("a new ledger is empty and add writes the file", (t) => {
  const path = scratch(t);
  const ledger = Ledger.open(path);
  assert.deepEqual(ledger.transactions(), []);
  const tx = ledger.add({ date: "2026-03-02", kind: "expense", category: "food", amountCents: 1250 });
  assert.equal(tx.id, "t1");
  assert.deepEqual(Ledger.open(path).transactions(), [tx]);
});

test("transactions come by date then id, and by month", (t) => {
  const ledger = Ledger.open(scratch(t));
  ledger.add({ date: "2026-03-05", kind: "expense", category: "food", amountCents: 100 });
  ledger.add({ date: "2026-02-20", kind: "income", category: "salary", amountCents: 500000 });
  ledger.add({ date: "2026-03-01", kind: "expense", category: "rent", amountCents: 90000 });
  assert.deepEqual(ledger.transactions().map((t) => t.id), ["t2", "t3", "t1"]);
  assert.deepEqual(ledger.transactions("2026-03").map((t) => t.id), ["t3", "t1"]);
});

test("the balance follows each kind", (t) => {
  const ledger = Ledger.open(scratch(t));
  ledger.add({ date: "2026-03-01", kind: "income", category: "salary", amountCents: 100000 });
  ledger.add({ date: "2026-03-02", kind: "expense", category: "food", amountCents: 3000 });
  ledger.add({ date: "2026-03-03", kind: "refund", category: "food", amountCents: 500 });
  ledger.add({ date: "2026-03-04", kind: "transfer", category: "savings", amountCents: 20000 });
  assert.equal(ledger.balanceCents(), 97500);
});

test("a bad transaction is refused and nothing is written", (t) => {
  const path = scratch(t);
  const ledger = Ledger.open(path);
  assert.throws(() => ledger.add({ date: "2026-02-30", kind: "expense", category: "food", amountCents: 1 }), RangeError);
  assert.deepEqual(Ledger.open(path).transactions(), []);
});

test("keys the ledger does not know are kept", (t) => {
  const path = scratch(t);
  writeFileSync(path, JSON.stringify({ owner: "Ana", transactions: [] }));
  Ledger.open(path).add({ date: "2026-03-02", kind: "expense", category: "food", amountCents: 1 });
  assert.equal(JSON.parse(readFileSync(path, "utf8")).owner, "Ana");
});
