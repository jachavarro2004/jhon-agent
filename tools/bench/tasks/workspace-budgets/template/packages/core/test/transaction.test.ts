import { test } from "node:test";
import assert from "node:assert/strict";
import { balanceEffect, byDate, isDate, monthOf, validateTransaction, type Transaction } from "../src/index.ts";

const tx = (over: Partial<Transaction>): Transaction => ({
  id: "t1",
  date: "2026-03-02",
  kind: "expense",
  category: "food",
  amountCents: 1250,
  ...over,
});

test("isDate takes real calendar days only", () => {
  assert.equal(isDate("2026-02-28"), true);
  assert.equal(isDate("2028-02-29"), true);
  assert.equal(isDate("2026-02-29"), false);
  assert.equal(isDate("2026-13-01"), false);
  assert.equal(isDate("2026-3-1"), false);
});

test("monthOf is the date's YYYY-MM", () => {
  assert.equal(monthOf("2026-03-31"), "2026-03");
});

test("validateTransaction names what is wrong", () => {
  assert.doesNotThrow(() => validateTransaction(tx({})));
  assert.throws(() => validateTransaction(tx({ date: "2026-02-30" })), /not a date/);
  assert.throws(() => validateTransaction(tx({ kind: "gift" as Transaction["kind"] })), /not a kind/);
  assert.throws(() => validateTransaction(tx({ category: " " })), /category/);
  assert.throws(() => validateTransaction(tx({ amountCents: 0 })), /amount/);
  assert.throws(() => validateTransaction(tx({ amountCents: 12.5 })), /amount/);
});

test("each kind moves the balance its own way", () => {
  assert.equal(balanceEffect(tx({ kind: "income" })), 1250);
  assert.equal(balanceEffect(tx({ kind: "expense" })), -1250);
  assert.equal(balanceEffect(tx({ kind: "refund" })), 1250);
  assert.equal(balanceEffect(tx({ kind: "transfer" })), 0);
});

test("byDate orders by date, then by id as a number", () => {
  const list = [tx({ id: "t10", date: "2026-03-01" }), tx({ id: "t2", date: "2026-03-01" }), tx({ id: "t1", date: "2026-03-05" })];
  assert.deepEqual(list.sort(byDate).map((t) => t.id), ["t2", "t10", "t1"]);
});
