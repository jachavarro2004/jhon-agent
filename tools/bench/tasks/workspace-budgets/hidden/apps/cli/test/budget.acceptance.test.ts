// The bench's hidden acceptance test of workspace-budgets, the CLI's part.
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { main } from "../src/main.ts";

function scratch(t: { after: (fn: () => void) => void }): string {
  const dir = mkdtempSync(join(tmpdir(), "ledger-acceptance-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  return join(dir, "ledger.json");
}

function cli(argv: string[]): { code: number; out: string[]; err: string[] } {
  const out: string[] = [];
  const err: string[] = [];
  const code = main(argv, { out: (l) => out.push(l), err: (l) => err.push(l) });
  return { code, out, err };
}

test("budget set prints the budget with two decimals", (t) => {
  const file = scratch(t);
  assert.deepEqual(cli([file, "budget", "set", "food", "300"]), { code: 0, out: ["budget food 300.00"], err: [] });
  assert.deepEqual(cli([file, "budget", "set", "rent", "800.5"]), { code: 0, out: ["budget rent 800.50"], err: [] });
});

test("budget report prints one line per category, OVER past its limit", (t) => {
  const file = scratch(t);
  for (const argv of [
    ["add", "2026-03-01", "expense", "food", "120"],
    ["add", "2026-03-10", "refund", "food", "20"],
    ["add", "2026-03-11", "transfer", "savings", "500"],
    ["add", "2026-03-12", "expense", "rent", "900"],
    ["add", "2026-02-12", "expense", "rent", "900"],
    ["budget", "set", "rent", "800"],
    ["budget", "set", "food", "300"],
    ["budget", "set", "savings", "100"],
    ["budget", "set", "books", "10"],
  ]) {
    assert.equal(cli([file, ...argv]).code, 0, argv.join(" "));
  }
  assert.deepEqual(cli([file, "budget", "report", "2026-03"]), {
    code: 0,
    out: ["books 0.00 / 10.00 (0%)", "food 100.00 / 300.00 (33%)", "rent 900.00 / 800.00 (113%) OVER", "savings 0.00 / 100.00 (0%)"],
    err: [],
  });
});

test("a bad amount or month exits 2 with an error and changes nothing", (t) => {
  const file = scratch(t);
  assert.equal(cli([file, "budget", "set", "food", "300"]).code, 0);
  const before = readFileSync(file, "utf8");
  for (const argv of [
    ["budget", "set", "food", "1.005"],
    ["budget", "set", "food", "-3"],
    ["budget", "set", "food", "0"],
    ["budget", "report", "2026-13"],
    ["budget", "report", "03-2026"],
  ]) {
    const r = cli([file, ...argv]);
    assert.equal(r.code, 2, argv.join(" "));
    assert.deepEqual(r.out, [], argv.join(" "));
    assert.ok(r.err.length > 0, argv.join(" "));
  }
  assert.equal(readFileSync(file, "utf8"), before);
});

test("a bad amount on a new ledger writes no file", (t) => {
  const file = scratch(t);
  assert.equal(cli([file, "budget", "set", "food", "abc"]).code, 2);
  assert.equal(existsSync(file), false);
});

test("the program runs budget report", (t) => {
  const file = scratch(t);
  assert.equal(cli([file, "budget", "set", "food", "50"]).code, 0);
  const program = fileURLToPath(new URL("../src/main.ts", import.meta.url));
  const r = spawnSync(process.execPath, [program, file, "budget", "report", "2026-03"], { encoding: "utf8" });
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.stdout, "food 0.00 / 50.00 (0%)\n");
  const bad = spawnSync(process.execPath, [program, file, "budget", "report", "2026-3"], { encoding: "utf8" });
  assert.equal(bad.status, 2);
  assert.equal(bad.stdout, "");
});
