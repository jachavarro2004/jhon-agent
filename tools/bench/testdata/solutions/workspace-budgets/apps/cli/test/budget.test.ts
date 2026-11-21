import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { main } from "../src/main.ts";

function scratch(t: { after: (fn: () => void) => void }): string {
  const dir = mkdtempSync(join(tmpdir(), "ledger-cli-budget-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  return join(dir, "ledger.json");
}

function cli(argv: string[]): { code: number; out: string[]; err: string[] } {
  const out: string[] = [];
  const err: string[] = [];
  return { code: main(argv, { out: (l) => out.push(l), err: (l) => err.push(l) }), out, err };
}

test("budget set and report", (t) => {
  const file = scratch(t);
  assert.deepEqual(cli([file, "budget", "set", "food", "10"]).out, ["budget food 10.00"]);
  cli([file, "add", "2026-03-01", "expense", "food", "12.5"]);
  assert.deepEqual(cli([file, "budget", "report", "2026-03"]).out, ["food 12.50 / 10.00 (125%) OVER"]);
});

test("a bad amount or month exits 2", (t) => {
  const file = scratch(t);
  assert.equal(cli([file, "budget", "set", "food", "1.005"]).code, 2);
  assert.equal(cli([file, "budget", "report", "2026-3"]).code, 2);
});
