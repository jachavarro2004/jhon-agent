import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { main } from "../src/main.ts";

function scratch(t: { after: (fn: () => void) => void }): string {
  const dir = mkdtempSync(join(tmpdir(), "ledger-cli-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  return join(dir, "ledger.json");
}

function cli(argv: string[]): { code: number; out: string[]; err: string[] } {
  const out: string[] = [];
  const err: string[] = [];
  const code = main(argv, { out: (l) => out.push(l), err: (l) => err.push(l) });
  return { code, out, err };
}

test("add, list and balance", (t) => {
  const file = scratch(t);
  assert.deepEqual(cli([file, "add", "2026-03-01", "income", "salary", "1000"]).out, ["added t1"]);
  assert.deepEqual(cli([file, "add", "2026-03-02", "expense", "food", "12.5"]).out, ["added t2"]);
  assert.deepEqual(cli([file, "list", "2026-03"]).out, ["t1 2026-03-01 income salary 1000.00", "t2 2026-03-02 expense food 12.50"]);
  assert.deepEqual(cli([file, "list", "2026-04"]).out, []);
  assert.deepEqual(cli([file, "balance"]).out, ["balance 987.50"]);
});

test("a bad amount, kind or month exits 2 with an error", (t) => {
  const file = scratch(t);
  for (const argv of [
    [file, "add", "2026-03-01", "expense", "food", "1.005"],
    [file, "add", "2026-03-01", "gift", "food", "1"],
    [file, "list", "2026-13"],
  ]) {
    const r = cli(argv);
    assert.equal(r.code, 2, argv.join(" "));
    assert.match(r.err.join("\n"), /error: /);
  }
  assert.deepEqual(cli([file, "list"]).out, []);
});

test("an unknown command prints the usage and exits 2", (t) => {
  const r = cli([scratch(t), "export"]);
  assert.equal(r.code, 2);
  assert.match(r.err.join("\n"), /usage: ledger/);
});

test("it runs as a program", (t) => {
  const file = scratch(t);
  const main = fileURLToPath(new URL("../src/main.ts", import.meta.url));
  const r = spawnSync(process.execPath, [main, file, "balance"], { encoding: "utf8" });
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.stdout, "balance 0.00\n");
});
