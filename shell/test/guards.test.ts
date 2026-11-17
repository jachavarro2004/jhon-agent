/**
 * pi's guard spawn and the explore write scope (T123, L773): judge() runs
 * `jhon hook pre-tool` with the child's environment, so vcs-guard reads the
 * agent from JHON_PI_AGENT (set by childEnv, shell/lib/agents.ts), and pi's
 * payload names no agent_type.
 *
 *   jhon test
 */

import { test, after } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync, chmodSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { judge, type CallContext, type JhonPiConfig } from "../lib/guards.ts";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const HOOK_BIN = process.env.JHON_TEST_BIN || join(REPO, "build", "jhon");
const SCRATCH = realpathSync(mkdtempSync(join(tmpdir(), "jhon-guards-")));
after(() => rmSync(SCRATCH, { recursive: true, force: true }));
Object.assign(process.env, { HOME: "/home/jhon-guards-test", XDG_STATE_HOME: join(SCRATCH, "state"), JHON_ROOT: REPO });

const configOf = (jhon: string): JhonPiConfig => ({ jhon, orchestrator: "/nonexistent", vcsDirs: [".git"], protected: [join(SCRATCH, "fw")], inspectAllow: [] });

/** A git repository under SCRATCH. */
function repo(name: string): string {
  const dir = join(SCRATCH, name);
  mkdirSync(dir, { recursive: true });
  assert.equal(spawnSync("git", ["init", "-q", dir]).status, 0);
  return dir;
}

/** Runs fn as a pi explore child: the environment childEnv gives it. */
async function asExplore<T>(fn: () => Promise<T>): Promise<T> {
  const saved = { child: process.env.JHON_PI_CHILD, agent: process.env.JHON_PI_AGENT };
  Object.assign(process.env, { JHON_PI_CHILD: "1", JHON_PI_AGENT: "explore" });
  try { return await fn(); } finally {
    for (const [k, v] of [["JHON_PI_CHILD", saved.child], ["JHON_PI_AGENT", saved.agent]] as const) {
      if (v === undefined) delete process.env[k]; else process.env[k] = v;
    }
  }
}

test("the guard spawn inherits JHON_PI_AGENT and the payload names no agent_type", async () => {
  const dir = repo("record");
  const bin = join(SCRATCH, "fake-jhon");
  const log = join(SCRATCH, "fake-jhon.log");
  writeFileSync(bin, `#!/bin/sh\n{ printf 'agent=%s\\n' "$JHON_PI_AGENT"; cat; } > '${log}'\nexit 0\n`);
  chmodSync(bin, 0o755);
  const ctx: CallContext = { config: configOf(bin), cwd: dir, child: true, interactive: false };
  assert.equal(await asExplore(() => judge("write", { path: join(dir, "src/app.go"), content: "x" }, ctx)), null);
  const [first, ...rest] = readFileSync(log, "utf8").split("\n");
  assert.equal(first, "agent=explore");
  const payload = JSON.parse(rest.join("\n"));
  assert.equal("agent_type" in payload, false);
  assert.equal(payload.agent_id, "pi-child");
});

test("an explore child's write outside its record is refused through the real binary", async () => {
  const dir = repo("real");
  const ctx: CallContext = { config: configOf(HOOK_BIN), cwd: dir, child: true, interactive: false };
  const refused = await asExplore(() => judge("write", { path: join(dir, "openspec/changes/c/tasks.md"), content: "x" }, ctx));
  assert.ok(refused && "block" in refused, JSON.stringify(refused));
  assert.match(refused.reason, /^vcs-guard: explore writes only its change's exploration\.md or research\.md/);
  assert.equal(await asExplore(() => judge("write", { path: join(dir, "openspec/changes/c/exploration.md"), content: "x" }, ctx)), null);
});
