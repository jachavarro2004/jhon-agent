/**
 * Unit tests for shell/lib/m365.ts: m365_read runs a headless Claude Code
 * whose command line admits only Microsoft 365's read tools, and returns its
 * answer or why there is none.
 *
 *   jhon test
 */

import { test, after } from "node:test";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { M365_DENIED, M365_PREFIX, M365_READ, M365_TOOLS, m365Args, m365Read } from "../lib/m365.ts";

const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
const tempDir = () => { const d = realpathSync(mkdtempSync(join(tmpdir(), "jhon-m365-test-"))); temps.push(d); return d; };

/** A fake claude: records its arguments and working directory, then runs body. */
function fakeClaude(body: string): { bin: string; argsFile: string } {
  const d = tempDir();
  const bin = join(d, "claude"), argsFile = join(d, "args");
  writeFileSync(bin, `#!/bin/sh\nfor a in "$@"; do printf '%s\\n' "$a"; done > ${argsFile}\npwd >> ${argsFile}\n${body}\n`);
  chmodSync(bin, 0o755);
  return { bin, argsFile };
}

const flag = (args: string[], name: string) => args[args.indexOf(name) + 1];
const WRITES = /send|create|delete|update|upload|move|rename|copy|modify|trash|forward|respond|reply|set_|batch/;

test("only searching and reading SharePoint and OneDrive is allowed; nothing that writes", () => {
  for (const t of M365_READ) {
    assert.ok(M365_TOOLS.includes(t), t);
    assert.doesNotMatch(t.slice(M365_PREFIX.length), WRITES, t);
  }
  assert.deepEqual([...M365_READ, ...M365_DENIED].sort(), [...M365_TOOLS].sort(), "every known tool is allowed or denied");
  for (const t of M365_TOOLS) if (WRITES.test(t.slice(M365_PREFIX.length))) assert.ok(M365_DENIED.includes(t), t);
});

test("the command line is the limit: dontAsk, no built-in tool, the read tools allowed, every other one denied", () => {
  const args = m365Args("read HU-ABC-001", "sonnet");
  assert.deepEqual(args.slice(0, 2), ["-p", "read HU-ABC-001"]);
  assert.equal(flag(args, "--permission-mode"), "dontAsk");
  assert.equal(flag(args, "--tools"), "");
  assert.deepEqual(flag(args, "--allowedTools").split(","), M365_READ);
  assert.deepEqual(flag(args, "--disallowedTools").split(","), M365_DENIED);
  assert.equal(flag(args, "--model"), "sonnet");
  assert.ok(args.includes("--no-session-persistence"));
  // Both would drop the claude.ai connectors, Microsoft 365 with them.
  assert.ok(!args.includes("--strict-mcp-config") && !args.includes("--setting-sources"));
});

test("the answer comes back from an empty directory that is removed after", async () => {
  const { bin, argsFile } = fakeClaude("echo 'CA-01 verbatim'");
  const r = await m365Read("read HU-ABC-001", { bin, model: "haiku" });
  assert.deepEqual(r, { ok: true, text: "CA-01 verbatim" });
  const lines = readFileSync(argsFile, "utf8").trim().split("\n");
  assert.equal(flag(lines, "--model"), "haiku");
  const cwd = lines.at(-1)!;
  assert.match(cwd, /jhon-m365-/);
  assert.throws(() => readFileSync(join(cwd, "x")), /ENOENT/);
});

test("a failed, silent, missing, or slow claude is a failure that says why", async () => {
  assert.match((await m365Read("x", { bin: fakeClaude("echo denied >&2; exit 3").bin })).text, /exited 3: denied/);
  assert.equal((await m365Read("x", { bin: fakeClaude("true").bin })).ok, false);
  assert.match((await m365Read("x", { bin: "/nonexistent/claude" })).text, /could not run/);
  const slow = await m365Read("x", { bin: fakeClaude("exec sleep 30").bin, timeoutMs: 200 });
  assert.match(slow.text, /stopped \(SIGTERM\)/);
});
