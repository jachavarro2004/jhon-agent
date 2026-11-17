/**
 * shell/lib/ui/command-failure.ts and the red bash card: a failed command
 * says its exit code, its test counts, its first failure and where it is;
 * a running one says how long it has run and how to cancel it.
 *
 *   jhon test
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { commandFailure, failureHeadline, runningLine } from "../lib/ui/command-failure.ts";
import { failureRows } from "../extensions/quiet.ts";

test("pytest: the exit code, the counts, the first FAILED test and its file and line", () => {
  const out = "tests/test_a.py ..F\n=== FAILURES ===\ntests/test_a.py:88: AssertionError\nFAILED tests/test_a.py::test_wrap - assert 33 <= 31\n1 failed, 17 passed in 1.40s\n\nCommand exited with code 1";
  const f = commandFailure(out);
  assert.deepEqual(f, { exit: 1, failed: 1, passed: 17, first: "tests/test_a.py::test_wrap — assert 33 <= 31", where: "tests/test_a.py:88" });
  assert.equal(failureHeadline(f), "exited 1 · 1 failed · 17 passed");
});

test("node --test, go test, and a command that is no test runner", () => {
  const node = commandFailure("✖ wraps a long title onto two rows (2ms)\n  at shell/test/ui-wrap.test.ts:88:5\nℹ pass 17\nℹ fail 1\n\nCommand exited with code 1");
  assert.equal(node.first, "wraps a long title onto two rows (2ms)");
  assert.equal(node.where, "shell/test/ui-wrap.test.ts:88");
  assert.deepEqual([node.failed, node.passed], [1, 17]);
  const go = commandFailure("--- FAIL: TestWrap (0.00s)\n    wrap_test.go:12: got 33\n--- FAIL: TestOther (0.00s)\nFAIL\n\nCommand exited with code 1");
  assert.deepEqual([go.failed, go.first, go.where], [2, "TestWrap", "wrap_test.go:12"]);
  assert.equal(failureHeadline(commandFailure("cp: cannot stat 'x'\n\nCommand exited with code 1")), "exited 1");
  assert.deepEqual(commandFailure("no status at all"), {});
});

test("the red card's rows: the headline, the first failure, and where it is", () => {
  assert.deepEqual(failureRows("FAILED tests/t.py::test_x\n1 failed in 0.1s\n\nCommand exited with code 1"),
    ["exited 1 · 1 failed", "✗ tests/t.py::test_x"]);
  assert.deepEqual(failureRows("nothing to say"), []);
});

test("a running command says how long it has run and how to cancel it", () => {
  assert.equal(runningLine(3200, false), "running · 3s · esc to cancel");
  assert.equal(runningLine(125_000, true), "running · 2m 05s · esc esc to cancel");
});
