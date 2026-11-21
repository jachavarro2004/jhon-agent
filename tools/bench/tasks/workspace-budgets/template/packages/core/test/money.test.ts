import { test } from "node:test";
import assert from "node:assert/strict";
import { formatCents, parseAmount } from "../src/index.ts";

test("parseAmount reads whole amounts and up to two decimals", () => {
  assert.equal(parseAmount("12"), 1200);
  assert.equal(parseAmount("12.5"), 1250);
  assert.equal(parseAmount("12.05"), 1205);
  assert.equal(parseAmount(" 0.99 "), 99);
});

test("parseAmount refuses what is not an amount", () => {
  for (const bad of ["", "-1", "1.005", "1,50", "abc", "1."]) {
    assert.throws(() => parseAmount(bad), RangeError, bad);
  }
});

test("formatCents prints two decimals", () => {
  assert.equal(formatCents(0), "0.00");
  assert.equal(formatCents(5), "0.05");
  assert.equal(formatCents(123450), "1234.50");
  assert.equal(formatCents(-1999), "-19.99");
});
