/**
 * Unit tests for pi/agents.ts and pi/ask.ts: Jhon's named sub-agents run as
 * child pi processes (a fake pi here, set through JHON_PI_BIN, records what
 * it was given), and `ask_user` asks only where someone can answer.
 *
 *   node --experimental-strip-types --test shell/test/*.test.ts
 */

import "./isolated-env.ts";
import { test, after } from "node:test";
import assert from "node:assert/strict";
import { chmodSync, existsSync, mkdtempSync, mkdirSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import jhon, { isChild } from "../extensions/jhon.ts";
import { childArgs, childEnv, childSession, continuation, knownAgents, parseAgent, parseFrontmatter, refuseTask, resolveAgent, runAgent } from "../lib/agents.ts";
import { askUser, parseQuestions } from "../lib/ask.ts";

const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
function tempDir(prefix: string): string {
  const d = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
  temps.push(d);
  return d;
}

const WORKER = `---
name: jhon-builder
description: "Implement a scoped task: edits only the files named"
model: anthropic/claude-sonnet-4-5
thinking: medium
tools:
  - read
  - edit
  - write
  - bash
  - mcp__engram__mem_save
bounded: true
---

# Worker

Do the work.
`;

const EXPLORE = `---
name: jhon-scout
description: "Investigate before a change"
model: openai/gpt-5
tools:
  - read
  - mcp__codegraph__codegraph_explore
---
Look around.
`;

function agentDir(): string {
  const dir = tempDir("jhon-pi-agents-");
  mkdirSync(join(dir, "agents"));
  writeFileSync(join(dir, "agents", "jhon-builder.md"), WORKER);
  writeFileSync(join(dir, "agents", "jhon-scout.md"), EXPLORE);
  return dir;
}

/** A fake pi that records its argv, cwd, and the child markers, prints a result, and exits with FAKE_PI_EXIT. */
function fakePi(): { bin: string; log: string } {
  const dir = tempDir("jhon-pi-fake-");
  const bin = join(dir, "pi");
  const log = join(dir, "log.json");
  writeFileSync(bin, `#!/usr/bin/env node
const fs = require("node:fs");
fs.writeFileSync(${JSON.stringify(log)}, JSON.stringify({ argv: process.argv.slice(2), cwd: process.cwd(),
  env: { PI_CODING_AGENT_DIR: process.env.PI_CODING_AGENT_DIR, JHON_PI_CHILD: process.env.JHON_PI_CHILD, JHON_PI_AGENT: process.env.JHON_PI_AGENT },
  maxTurns: process.env.JHON_PI_MAX_TURNS, scope: process.env.JHON_PI_TASK_FILE ? fs.readFileSync(process.env.JHON_PI_TASK_FILE, "utf8") : undefined }));
const code = Number(process.env.FAKE_PI_EXIT || 0);
if (code) { process.stderr.write("model unavailable\\n"); process.exit(code); }
process.stdout.write("final answer from the child\\n");
`);
  chmodSync(bin, 0o755);
  return { bin, log };
}

function withEnv<T>(env: Record<string, string | undefined>, f: () => T): T {
  const saved: Record<string, string | undefined> = {};
  for (const k of Object.keys(env)) { saved[k] = process.env[k]; if (env[k] === undefined) delete process.env[k]; else process.env[k] = env[k]; }
  const restore = () => { for (const [k, v] of Object.entries(saved)) if (v === undefined) delete process.env[k]; else process.env[k] = v; };
  try {
    const r = f();
    if (r instanceof Promise) return r.finally(restore) as T;
    restore();
    return r;
  } catch (e) { restore(); throw e; }
}

// ── frontmatter and resolution ─────────────────────────────────────────────

test("the frontmatter gives the name, the JSON-quoted description, the model, thinking, the tool list, and bounded", () => {
  const a = parseAgent(WORKER, "fallback");
  assert.deepEqual({ ...a, body: undefined }, {
    name: "jhon-builder", description: "Implement a scoped task: edits only the files named", model: "anthropic/claude-sonnet-4-5",
    thinking: "medium", tools: ["read", "edit", "write", "bash", "mcp__engram__mem_save"], bounded: true, body: undefined,
  });
  assert.equal(a.body, "# Worker\n\nDo the work.");
  const e = parseAgent(EXPLORE, "jhon-scout");
  assert.equal(e.bounded, false);
  assert.equal(e.thinking, undefined);
  assert.deepEqual(parseFrontmatter("---\ntools: [read, bash]\n---\nbody").fields.tools, ["read", "bash"]);
  assert.throws(() => parseAgent("no frontmatter", "x"), /frontmatter/);
  assert.throws(() => parseAgent("---\nname: x\n---\n", "x"), /no model/);
});

// T131 (L905, L908): no agent has a turn cap; the context cap and the timeout
// are a child's limits. An agent file that still carries a `maxTurns` line
// (an older install) loads without error and the line is ignored.
test("an agent file's maxTurns line is ignored: the agent loads as without it and no cap reaches its child", () => {
  const plain = parseAgent(WORKER, "jhon-builder");
  for (const line of ["maxTurns: 80", "maxTurns: lots", "maxTurns: 0"]) {
    const old = parseAgent(WORKER.replace("bounded: true", `bounded: true\n${line}`), "jhon-builder");
    assert.deepEqual(old, plain, line);
    assert.deepEqual(childEnv(old, undefined, {}), childEnv(plain, undefined, {}), line);
    assert.equal(childEnv(old, undefined, {}).JHON_PI_MAX_TURNS, undefined, line);
  }
});

test("an agent resolves by name; an unknown one is refused with the agents there are", () => {
  const dir = agentDir();
  assert.deepEqual(knownAgents(dir), ["jhon-builder", "jhon-scout"]);
  assert.equal(resolveAgent(dir, "jhon-scout").model, "openai/gpt-5");
  assert.throws(() => resolveAgent(dir, "ghost"), /no sub-agent named "ghost"; known agents: jhon-builder, jhon-scout/);
  assert.throws(() => resolveAgent(dir, "../jhon"), /no sub-agent named/);
  assert.throws(() => resolveAgent(tempDir("jhon-pi-none-"), "jhon-builder"), /known agents: \(none\)/);
});

test("a bounded agent's task must name its allowed edit surfaces", () => {
  const worker = parseAgent(WORKER, "jhon-builder");
  assert.match(refuseTask(worker, "Fix the bug in a.ts.")!, /bounded agent.*## Allowed edit surfaces/);
  assert.equal(refuseTask(worker, "Fix the bug.\n\n## Allowed edit surfaces\n\n- src/a.ts\n"), null);
  assert.equal(refuseTask(parseAgent(EXPLORE, "jhon-scout"), "Look at a.ts."), null);
  assert.match(refuseTask(parseAgent(EXPLORE, "jhon-scout"), "  ")!, /empty/);
});

// ── the child process ──────────────────────────────────────────────────────

test("the child runs pi one-shot with the agent's model, thinking, tools, and prompt, marked as a child", () => {
  const worker = parseAgent(WORKER, "jhon-builder");
  assert.deepEqual(childArgs(worker, "do it"), [
    "--print", "--no-session", "--model", "anthropic/claude-sonnet-4-5", "--thinking", "medium",
    "--tools", "read,edit,write,bash,mcp__engram__mem_save", "--no-skills", "--no-context-files",
    "--append-system-prompt", "# Worker\n\nDo the work.", "--", "do it",
  ]);
  assert.deepEqual(childArgs(parseAgent(EXPLORE, "jhon-scout"), "@a.ts"), [
    "--print", "--no-session", "--model", "openai/gpt-5", "--tools", "read,mcp__codegraph__codegraph_explore",
    "--no-skills", "--no-context-files", "--append-system-prompt", "Look around.", "--", " @a.ts",
  ]);
  const env = childEnv(worker, "/t/task.md", { PI_CODING_AGENT_DIR: "/a", JHON_PI_CHILD: "0", JHON_PI_TOOLS: "bash", JHON_PI_TASK_FILE: "/outer" });
  assert.deepEqual(env, { PI_CODING_AGENT_DIR: "/a", JHON_PI_CHILD: "1", JHON_PI_AGENT: "jhon-builder",
    JHON_PI_TOOLS: "read,edit,write,bash,mcp__engram__mem_save", JHON_PI_TASK_FILE: "/t/task.md" });
  assert.deepEqual(childEnv(parseAgent("---\nname: x\nmodel: a/b\n---\n", "x"), undefined, { JHON_PI_TOOLS: "bash" }),
    { JHON_PI_CHILD: "1", JHON_PI_AGENT: "x" }, "an agent with no tools and no task file inherits neither");
});

test("a child's session is saved under the parent's, named for the subagent call; an unsaved parent saves none", () => {
  const worker = parseAgent(WORKER, "jhon-builder");
  const parent = { file: "/s/--p--/2026-10-25T00-00-00-000Z_abc.jsonl", id: "abc" };
  const session = childSession(worker, parent, "toolu_01X|fc/2");
  assert.deepEqual(session, {
    dir: "/s/--p--/2026-10-25T00-00-00-000Z_abc/subagents", id: "toolu_01X-fc-2",
    meta: { agentType: "jhon-builder", toolUseId: "toolu_01X|fc/2", parentSession: "abc" },
  });
  assert.deepEqual(childArgs(worker, "do it", session).slice(0, 7), [
    "--print", "--session-dir", "/s/--p--/2026-10-25T00-00-00-000Z_abc/subagents", "--session-id", "toolu_01X-fc-2", "--model", "anthropic/claude-sonnet-4-5",
  ]);
  assert.equal(childSession(worker, {}, "toolu_1"), null, "an in-memory parent");
  assert.equal(childSession(worker, { file: parent.file }, "toolu_1"), null, "no parent id");
  assert.equal(childSession(worker, parent, "--"), null, "a call id with nothing pi accepts");
  assert.equal(childSession(worker, parent, "-a.b_c-").id, "a.b_c");
});

test("the subagent tool saves the child's session under the parent's, with a meta file linking it to the call", async () => {
  const dir = agentDir();
  const cwd = tempDir("jhon-pi-cwd-");
  const sessions = tempDir("jhon-pi-sessions-");
  const { bin, log } = fakePi();
  const tools: any[] = [];
  withEnv({ PI_CODING_AGENT_DIR: dir, JHON_PI_CHILD: "1" }, () => jhon({ on: () => {}, registerTool: (t: any) => tools.push(t), registerCommand: () => {} }));
  const subagent = tools.find((t) => t.name === "subagent");
  const file = join(sessions, "2026-10-25T00-00-00-000Z_parent-1.jsonl");
  const sessionManager = { getSessionFile: () => file, getSessionId: () => "parent-1" };
  await withEnv({ JHON_PI_BIN: bin, FAKE_PI_EXIT: undefined }, () =>
    subagent.execute("toolu_9", { agent: "jhon-scout", task: "look" }, undefined, undefined, { cwd, sessionManager }));
  const children = join(sessions, "2026-10-25T00-00-00-000Z_parent-1", "subagents");
  assert.deepEqual(JSON.parse(readFileSync(join(children, "toolu_9.meta.json"), "utf8")), { agentType: "jhon-scout", toolUseId: "toolu_9", parentSession: "parent-1" });
  assert.deepEqual(JSON.parse(readFileSync(log, "utf8")).argv.slice(0, 5), ["--print", "--session-dir", children, "--session-id", "toolu_9"]);

  // A parent session pi cannot write under: the child still runs, unsaved.
  const blocked = join(sessions, "file");
  writeFileSync(blocked, "");
  await withEnv({ JHON_PI_BIN: bin, FAKE_PI_EXIT: undefined }, () =>
    subagent.execute("toolu_10", { agent: "jhon-scout", task: "look" }, undefined, undefined, { cwd, sessionManager: { getSessionFile: () => join(blocked, "x.jsonl"), getSessionId: () => "x" } }));
  assert.deepEqual(JSON.parse(readFileSync(log, "utf8")).argv.slice(0, 2), ["--print", "--no-session"]);
  assert.equal(existsSync(join(blocked, "x")), false);
});

test("the subagent tool runs the fake pi in the parent's cwd and returns its output; a failure is an error", async () => {
  const dir = agentDir();
  const cwd = tempDir("jhon-pi-cwd-");
  const { bin, log } = fakePi();
  const tools: any[] = [];
  withEnv({ PI_CODING_AGENT_DIR: dir, JHON_PI_CHILD: "1" }, () => jhon({ on: () => {}, registerTool: (t: any) => tools.push(t), registerCommand: () => {} }));
  const subagent = tools.find((t) => t.name === "subagent");
  assert.match(subagent.description, /Agents: jhon-builder, jhon-scout/);

  const task = "Investigate.\n\n## Allowed edit surfaces\n\n- a.ts";
  const r = await withEnv({ JHON_PI_BIN: bin, PI_CODING_AGENT_DIR: dir, FAKE_PI_EXIT: undefined },
    () => subagent.execute("1", { agent: "jhon-builder", task }, undefined, undefined, { cwd }));
  assert.equal(r.content[0].text, "final answer from the child");
  const seen = JSON.parse(readFileSync(log, "utf8"));
  assert.equal(seen.cwd, cwd);
  assert.deepEqual(seen.env, { PI_CODING_AGENT_DIR: dir, JHON_PI_CHILD: "1", JHON_PI_AGENT: "jhon-builder" });
  assert.deepEqual(seen.argv.slice(0, 4), ["--print", "--no-session", "--model", "anthropic/claude-sonnet-4-5"], "a parent with no saved session saves none for its child");
  assert.equal(seen.argv.at(-1), task);

  await withEnv({ JHON_PI_BIN: bin, FAKE_PI_EXIT: "3" }, () =>
    assert.rejects(subagent.execute("2", { agent: "jhon-scout", task: "look" }, undefined, undefined, { cwd }), /"jhon-scout" exited 3:\nmodel unavailable/));
  await assert.rejects(subagent.execute("3", { agent: "ghost", task: "x" }, undefined, undefined, { cwd }), /known agents: jhon-builder, jhon-scout/);
  await assert.rejects(subagent.execute("4", { agent: "jhon-builder", task: "no surfaces" }, undefined, undefined, { cwd }), /Allowed edit surfaces/);
});

// ── continuing a saved sub-agent ───────────────────────────────────────────

/** An agent file from an older install: it still names a turn cap, which nothing reads. */
const LEGACY = WORKER.replace("name: worker", "name: legacy").replace("bounded: true", "bounded: true\nmaxTurns: 5");

/** A parent session saved in a temp dir, with one saved sub-agent `id` that took `turns` turns on `task` in `cwd`; `later` are the messages its transcript holds after them. */
function savedChild(id: string, turns: number, task: string, cwd: string, later: { role: string; content: unknown }[] = []) {
  const sessions = tempDir("jhon-pi-sessions-");
  const file = join(sessions, "2026-10-28T00-00-00-000Z_parent-1.jsonl");
  const children = join(sessions, "2026-10-28T00-00-00-000Z_parent-1", "subagents");
  mkdirSync(children, { recursive: true });
  writeFileSync(join(children, `${id}.meta.json`), JSON.stringify({ agentType: "legacy", toolUseId: id, parentSession: "parent-1" }));
  const lines = [
    { type: "session", version: 3, id, cwd },
    { type: "message", message: { role: "system", content: "" } },
    { type: "message", message: { role: "user", content: [{ type: "text", text: task }] } },
    ...Array.from({ length: turns }, () => ({ type: "message", message: { role: "assistant", content: [] } })),
    ...later.map((message) => ({ type: "message", message })),
  ];
  writeFileSync(join(children, `2026-10-28T00-00-01-000Z_${id}.jsonl`), lines.map((l) => JSON.stringify(l)).join("\n") + "\n");
  return { file, children, sessionManager: { getSessionFile: () => file, getSessionId: () => "parent-1" } };
}

test("subagent_continue runs a saved sub-agent again on its own session, in its cwd, with its scope and no turn cap", async () => {
  const dir = agentDir();
  writeFileSync(join(dir, "agents", "legacy.md"), LEGACY);
  const cwd = tempDir("jhon-pi-cwd-");
  const task = "Fix it.\n\n## Allowed edit surfaces\n\n- a.ts";
  const saved = savedChild("toolu_7", 3, task, cwd);
  const { bin, log } = fakePi();
  const tools: any[] = [];
  withEnv({ PI_CODING_AGENT_DIR: dir, JHON_PI_CHILD: "1" }, () => jhon({ on: () => {}, registerTool: (t: any) => tools.push(t), registerCommand: () => {} }));
  const cont = tools.find((t) => t.name === "subagent_continue");
  const r = await withEnv({ JHON_PI_BIN: bin, PI_CODING_AGENT_DIR: dir, FAKE_PI_EXIT: undefined },
    () => cont.execute("toolu_8", { id: "toolu_7", message: "The owner granted the push; go on." }, undefined, undefined, { cwd: tmpdir(), sessionManager: saved.sessionManager }));
  assert.equal(r.content[0].text, "final answer from the child\n\n(sub-agent id toolu_7: subagent_continue continues it)");
  const seen = JSON.parse(readFileSync(log, "utf8"));
  assert.deepEqual(seen.argv.slice(0, 5), ["--print", "--session-dir", saved.children, "--session-id", "toolu_7"]);
  assert.equal(seen.argv.at(-1), "The owner granted the push; go on.");
  assert.equal(seen.cwd, cwd, "it runs where it ran, so pi finds its session");
  assert.equal(seen.maxTurns, undefined, "no turn cap is handed to it, whatever its agent file says");
  assert.equal(seen.scope, task, "its scope guard still reads the task it was launched with");
});

// T133: a continue carries a corrected edit surface. The scope is what the
// child's scope guard reads as its launch prompt (JHON_PI_TASK_FILE).
test("subagent_continue with its own `## Allowed edit surfaces` hands the child that list as its scope, and later continues keep it", async () => {
  const dir = agentDir();
  writeFileSync(join(dir, "agents", "legacy.md"), LEGACY);
  const cwd = tempDir("jhon-pi-cwd-");
  const task = "Fix it.\n\n## Allowed edit surfaces\n\n- a.ts";
  const wider = "b.ts is yours too.\n\n## Allowed edit surfaces\n\n- a.ts\n- b.ts";
  const { bin, log } = fakePi();
  const tools: any[] = [];
  withEnv({ PI_CODING_AGENT_DIR: dir, JHON_PI_CHILD: "1" }, () => jhon({ on: () => {}, registerTool: (t: any) => tools.push(t), registerCommand: () => {} }));
  const cont = tools.find((t) => t.name === "subagent_continue");
  const scopeAfter = async (saved: ReturnType<typeof savedChild>, message: string) => {
    await withEnv({ JHON_PI_BIN: bin, PI_CODING_AGENT_DIR: dir, FAKE_PI_EXIT: undefined },
      () => cont.execute("toolu_8", { id: "toolu_7", message }, undefined, undefined, { cwd: tmpdir(), sessionManager: saved.sessionManager }));
    const seen = JSON.parse(readFileSync(log, "utf8"));
    assert.equal(seen.argv.at(-1), message, "the child is told the message, whole");
    return seen.scope;
  };
  assert.match(cont.parameters.properties.message.description, /A `## Allowed edit surfaces` replaces its list\./, "the tool says so");
  assert.equal(await scopeAfter(savedChild("toolu_7", 3, task, cwd), wider), wider, "the corrected list is the scope its guard reads");
  // The session now holds that continue: one without the heading keeps the corrected list.
  const user = (text: string) => ({ role: "user", content: [{ type: "text", text }] });
  const continued = savedChild("toolu_7", 3, task, cwd, [user(wider), { role: "assistant", content: [] }]);
  assert.equal(await scopeAfter(continued, "The owner said yes; go on."), wider, "a later continue without the heading keeps the last list");
  const narrower = "Only a.ts after all.\n\n## Allowed edit surfaces\n\n- a.ts";
  const twice = savedChild("toolu_7", 3, task, cwd, [user(wider), user("go on"), user(narrower), user("and again")]);
  assert.equal(continuation(dir, { file: twice.file, id: "parent-1" }, "toolu_7").scope, narrower, "the last message that carries the heading wins");
});

test("only the launching side sets a sub-agent's scope: a list the child wrote or read changes nothing", async () => {
  const dir = agentDir();
  writeFileSync(join(dir, "agents", "legacy.md"), LEGACY);
  const cwd = tempDir("jhon-pi-cwd-");
  const task = "Fix it.\n\n## Allowed edit surfaces\n\n- a.ts";
  const own = "I also need b.ts.\n\n## Allowed edit surfaces\n\n- a.ts\n- b.ts";
  const saved = savedChild("toolu_7", 1, task, cwd, [
    { role: "assistant", content: [{ type: "text", text: own }] },
    { role: "toolResult", content: [{ type: "text", text: own }] },
    { role: "custom", content: own },
  ]);
  assert.equal(continuation(dir, { file: saved.file, id: "parent-1" }, "toolu_7").scope, task);
  const { bin, log } = fakePi();
  const tools: any[] = [];
  withEnv({ PI_CODING_AGENT_DIR: dir, JHON_PI_CHILD: "1" }, () => jhon({ on: () => {}, registerTool: (t: any) => tools.push(t), registerCommand: () => {} }));
  await withEnv({ JHON_PI_BIN: bin, PI_CODING_AGENT_DIR: dir, FAKE_PI_EXIT: undefined },
    () => tools.find((t) => t.name === "subagent_continue").execute("toolu_8", { id: "toolu_7", message: "go on" }, undefined, undefined, { cwd: tmpdir(), sessionManager: saved.sessionManager }));
  assert.equal(JSON.parse(readFileSync(log, "utf8")).scope, task, "its guard still reads the task it was launched with");
});

test("subagent_continue continues a sub-agent past any number of turns; an unknown one and any in an unsaved session are refused", () => {
  const dir = agentDir();
  writeFileSync(join(dir, "agents", "legacy.md"), LEGACY);
  const task = "t\n\n## Allowed edit surfaces\n\n- a.ts";
  const long = savedChild("toolu_7", 90, task, tmpdir());
  const c = continuation(dir, { file: long.file, id: "parent-1" }, "toolu_7");
  assert.deepEqual(c, { agent: resolveAgent(dir, "legacy"), session: { dir: long.children, id: "toolu_7", meta: { agentType: "legacy", toolUseId: "toolu_7", parentSession: "parent-1" } }, cwd: tmpdir(), scope: task });
  assert.equal("maxTurns" in c.agent, false);
  assert.throws(() => continuation(dir, { file: long.file, id: "parent-1" }, "toolu_99"), /no saved sub-agent toolu_99 in this session/);
  assert.throws(() => continuation(dir, { id: "parent-1" }, "toolu_7"), /not saved/);
});

// ── a foreground sub-agent's progress and timeout ──────────────────────────
// Foreground is a sub-agent's own way to run the ones it starts: the main
// session runs every sub-agent in the background (L649, subagent-background.test.ts).

/** A fake pi that streams pi's JSON events: a grep, then what it says; with FAKE_PI_HANG it never ends. */
function streamingPi(): { bin: string; log: string } {
  const dir = tempDir("jhon-pi-stream-");
  const bin = join(dir, "pi");
  const log = join(dir, "log.json");
  writeFileSync(bin, `#!/usr/bin/env node
require("node:fs").writeFileSync(${JSON.stringify(log)}, JSON.stringify({ argv: process.argv.slice(2) }));
const out = (event) => process.stdout.write(JSON.stringify(event) + "\\n");
out({ type: "tool_execution_start", toolName: "grep", args: { pattern: "parse(" } });
out({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "found 3 callers" }] } });
if (process.env.FAKE_PI_HANG) setInterval(() => {}, 1000);
`);
  chmodSync(bin, 0o755);
  return { bin, log };
}

function subagentTools(dir: string): Map<string, any> {
  const tools = new Map<string, any>();
  withEnv({ PI_CODING_AGENT_DIR: dir, JHON_PI_CHILD: "1" }, () => jhon({ on: () => {}, registerTool: (t: any) => tools.set(t.name, t), registerCommand: () => {} }));
  return tools;
}

test("a foreground sub-agent shows what it is doing as it works, and answers with what it said last", async () => {
  const dir = agentDir();
  const { bin, log } = streamingPi();
  const updates: string[] = [];
  const r = await withEnv({ JHON_PI_BIN: bin, FAKE_PI_HANG: undefined }, () =>
    subagentTools(dir).get("subagent").execute("1", { agent: "jhon-scout", task: "who calls parse?" }, undefined, (u: any) => updates.push(u.content[0].text), { cwd: tmpdir() }));
  assert.equal(r.content[0].text, "found 3 callers");
  assert.deepEqual(JSON.parse(readFileSync(log, "utf8")).argv.slice(0, 2), ["--mode", "json"], "it streams its events");
  assert.match(updates[0]!, /^jhon-scout is working \(\d+s\)\ngrep parse\($/);
  assert.match(updates.at(-1)!, /grep parse\(\n» found 3 callers$/);
});

test("a sub-agent past its timeout is stopped and the call fails, keeping what it had said; a bad timeout is refused", async () => {
  const dir = agentDir();
  const { bin } = streamingPi();
  const subagent = subagentTools(dir).get("subagent");
  await withEnv({ JHON_PI_BIN: bin, FAKE_PI_HANG: "1" }, () =>
    assert.rejects(subagent.execute("1", { agent: "jhon-scout", task: "look", timeout_seconds: 1 }, undefined, () => {}, { cwd: tmpdir() }),
      /"jhon-scout" ran past its 1s timeout and was stopped; it had said:\nfound 3 callers/));
  await assert.rejects(subagent.execute("2", { agent: "jhon-scout", task: "look", timeout_seconds: 0 }, undefined, undefined, { cwd: tmpdir() }), /timeout_seconds must be a whole number/);
});

test("a missing pi binary is an error result, not a crash", async () => {
  const r = await withEnv({ JHON_PI_BIN: "/nonexistent/pi" }, () => runAgent(parseAgent(EXPLORE, "jhon-scout"), "look", tmpdir()));
  assert.equal(r.ok, false);
  assert.match(r.text, /could not run \/nonexistent\/pi/);
});

// ── ask_user ───────────────────────────────────────────────────────────────

/** A UI that answers each dialog from `script` in turn, recording what it was shown. */
function scripted(script: Array<string | undefined>) {
  const shown: Array<[string, string, string[]?]> = [];
  const next = () => script.shift();
  const ui = {
    select: async (title: string, options: string[]) => { shown.push(["select", title, options]); return next(); },
    input: async (title: string) => { shown.push(["input", title]); return next(); },
  };
  return { ctx: { hasUI: true, ui }, shown };
}

test("ask_user puts each question in turn: a described choice, a typed Other, and a free answer", async () => {
  const { ctx, shown } = scripted(["Postgres (Recommended) — keeps the joins", "Write my own answer.", "Valkey", "every night"]);
  const r = await askUser({ questions: [
    { question: "Which database?", header: "Database", options: [{ label: "Postgres (Recommended)", description: "keeps the joins" }, { label: "SQLite" }] },
    { question: "Which cache?", options: [{ label: "Redis" }, { label: "None" }] },
    { question: "When should it back up?" },
  ] }, ctx, false);
  assert.deepEqual(r, { ok: true, text: "The owner answered:\n- Which database? → Postgres (Recommended)\n- Which cache? → Valkey (typed)\n- When should it back up? → every night" });
  assert.deepEqual(shown[0], ["select", "(1/3) Database: Which database?", ["Postgres (Recommended) — keeps the joins", "SQLite", "Write my own answer."]]);
  assert.deepEqual(shown.slice(1).map((s) => s.slice(0, 2)), [["select", "(2/3) Which cache?"], ["input", "(2/3) Which cache?"], ["input", "(3/3) When should it back up?"]]);
});

test("ask_user's multiple choice toggles options until Done, and keeps typed ones", async () => {
  const { ctx, shown } = scripted(["[ ] lint", "[ ] unit — fast", "[x] lint", "Write my own answer.", "e2e", "Done"]);
  const r = await askUser({ questions: [{ question: "Which checks?", multiSelect: true, options: [{ label: "lint" }, { label: "unit", description: "fast" }, { label: "types" }] }] }, ctx, false);
  assert.deepEqual(r, { ok: true, text: "The owner answered:\n- Which checks? → unit; e2e (typed)" });
  assert.deepEqual(shown[2], ["select", "Which checks?\n(pick any; Done when finished)", ["[x] lint", "[x] unit — fast", "[ ] types", "Write my own answer.", "Done"]]);
});

test("ask_user stops at a dismissed question and returns what was answered before it", async () => {
  const { ctx, shown } = scripted(["a", undefined]);
  const r = await askUser({ questions: [{ question: "First?", options: [{ label: "a" }, { label: "b" }] }, { question: "Second?" }, { question: "Third?" }] }, ctx, false);
  assert.deepEqual(r, { ok: true, text: "The owner dismissed \"Second?\" without answering, and was asked nothing more. Answered before that:\n- First? → a" });
  assert.equal(shown.length, 2);
});

test("ask_user refuses questions it cannot put", () => {
  const refused = (questions: unknown) => { const r = parseQuestions({ questions }); assert.equal(typeof r, "string"); return r as string; };
  assert.match(refused([]), /1 to 4 questions/);
  assert.match(refused(Array(5).fill({ question: "q" })), /1 to 4 questions/);
  assert.match(refused([{ question: " " }]), /question 1 has no question/);
  assert.match(refused([{ question: "q", options: [{ label: "only" }] }]), /2 to 4 options, or none/);
  assert.match(refused([{ question: "q", options: [{ label: "a" }, { label: "a" }] }]), /repeats the option "a"/);
  assert.match(refused([{ question: "q", header: "far too long a header" }]), /at most 12 characters/);
  assert.match(refused([{ question: "q", multiSelect: true }]), /multiple choice with no options/);
});

test("ask_user refuses in a child and in a session without a UI, and the tool reports it as an error", async () => {
  const questions = [{ question: "Which?" }];
  const ui = { hasUI: true, ui: { input: async () => "never" } };
  const kid = await askUser({ questions }, ui, true);
  assert.equal(kid.ok, false);
  assert.match(kid.text, /sub-agent, and no one can answer/);
  const headless = await askUser({ questions }, { hasUI: false }, false);
  assert.equal(headless.ok, false);
  assert.match(headless.text, /not interactive, so no one can answer/);

  const tools: any[] = [];
  withEnv({ PI_CODING_AGENT_DIR: agentDir(), JHON_PI_CHILD: "1" }, () => jhon({ on: () => {}, registerTool: (t: any) => tools.push(t), registerCommand: () => {} }));
  await assert.rejects(tools.find((t) => t.name === "ask_user").execute("1", { questions }, undefined, undefined, ui), /no one can answer/);
});

test("JHON_PI_CHILD=1 marks a child; anything else is the main session", () => {
  assert.equal(isChild({ JHON_PI_CHILD: "1" }), true);
  assert.equal(isChild({ JHON_PI_CHILD: "0" }), false);
  assert.equal(isChild({}), false);
});

// ── JSON event mode, for background sub-agents ────────────────────────────

test("in json mode the child streams pi's JSONL events instead of printing", () => {
  const worker = parseAgent(WORKER, "jhon-builder");
  const args = childArgs(worker, "do it", null, "json");
  assert.deepEqual(args.slice(0, 3), ["--mode", "json", "--no-session"]);
  assert.ok(!args.includes("--print"));
  assert.ok(childArgs(worker, "do it", null).includes("--print"), "the default stays print");
});

test("runAgent in json mode hands each event over and answers with the last assistant message's text", async () => {
  const dir = tempDir("jhon-pi-json-");
  const bin = join(dir, "pi");
  const events = [
    { type: "agent_start" },
    { type: "tool_execution_start", toolCallId: "c1", toolName: "bash", args: { command: "ls" } },
    { type: "tool_execution_end", toolCallId: "c1", toolName: "bash", isError: false },
    { type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "draft" }] } },
    { type: "message_end", message: { role: "assistant", content: [{ type: "thinking", thinking: "x" }, { type: "text", text: "final " }, { type: "text", text: "answer" }] } },
    { type: "agent_settled", aborted: false },
  ];
  // Split across writes, as a pipe may deliver it.
  const body = events.map((e) => JSON.stringify(e)).join("\n") + "\n";
  writeFileSync(bin, `#!/usr/bin/env node
const s = ${JSON.stringify(body)};
process.stdout.write(s.slice(0, 37)); setTimeout(() => process.stdout.write(s.slice(37)), 20);
`);
  chmodSync(bin, 0o755);
  const seen: string[] = [];
  const explore = parseAgent(EXPLORE, "jhon-scout");
  const r = await withEnv({ JHON_PI_BIN: bin }, () => runAgent(explore, "look", dir, { onEvent: (event) => seen.push(String(event.type)) }));
  assert.deepEqual(r, { ok: true, text: "final answer" });
  assert.deepEqual(seen, events.map((e) => e.type));
});

test("ask_user says the agent waits for the owner: jhon:owner-wait once while asking, settled when answered or dismissed (T15.4)", async () => {
  const tools: any[] = [];
  const sent: unknown[] = [];
  const events = { emit: (name: string, data: unknown) => sent.push([name, data]), on: () => () => {} };
  withEnv({ PI_CODING_AGENT_DIR: agentDir(), JHON_PI_CHILD: undefined }, () => jhon({ on: () => {}, registerTool: (t: any) => tools.push(t), registerCommand: () => {}, events } as any));
  const askTool = tools.find((t) => t.name === "ask_user");
  let during: unknown[] = [];
  const ui = { hasUI: true, ui: { input: async () => { during = [...sent]; return "typed"; } } };
  await askTool.execute("1", { questions: [{ question: "Which?" }] }, undefined, undefined, ui);
  assert.deepEqual(during, [["jhon:owner-wait", { active: true, label: "question" }]], "waiting while the owner is asked");
  assert.deepEqual(sent, [["jhon:owner-wait", { active: true, label: "question" }], ["jhon:owner-wait", { active: false }]], "settled once answered");
  sent.length = 0;
  await askTool.execute("2", { questions: [{ question: "Which?" }] }, undefined, undefined, { hasUI: true, ui: { input: async () => undefined } });
  assert.deepEqual(sent.map((s: any) => s[1].active), [true, false], "a dismissal settles it too");
  sent.length = 0;
  await assert.rejects(askTool.execute("3", { questions: [{ question: "Which?" }] }, undefined, undefined, { hasUI: true, ui: { input: async () => { throw new Error("gone"); } } }));
  assert.deepEqual(sent.map((s: any) => s[1].active), [true, false], "a failed prompt settles it too");
});

test("waitingForOwner wraps every wait: one blocked, one settle, whatever the wait does", async () => {
  const { waitingForOwner, OWNER_WAIT_EVENT } = await import("../lib/attention.ts");
  const sent: unknown[] = [];
  const events = { emit: (name: string, data: unknown) => sent.push([name, data]) };
  assert.equal(await waitingForOwner(events, "question", async () => 7), 7);
  await assert.rejects(waitingForOwner(events, "permission: push", async () => { throw new Error("x"); }));
  assert.equal(OWNER_WAIT_EVENT, "jhon:owner-wait");
  assert.deepEqual(sent, [[OWNER_WAIT_EVENT, { active: true, label: "question" }], [OWNER_WAIT_EVENT, { active: false }],
    [OWNER_WAIT_EVENT, { active: true, label: "permission: push" }], [OWNER_WAIT_EVENT, { active: false }]]);
  assert.equal(await waitingForOwner(undefined, "x", async () => 1), 1, "a pi without an event bus still asks");
});

test("an ask_user that asks no one (bad questions, a child) emits no owner wait", async () => {
  const tools: any[] = [];
  const sent: unknown[] = [];
  withEnv({ PI_CODING_AGENT_DIR: agentDir(), JHON_PI_CHILD: undefined }, () => jhon({ on: () => {}, registerTool: (t: any) => tools.push(t), registerCommand: () => {}, events: { emit: (n: string, d: unknown) => sent.push([n, d]), on: () => () => {} } } as any));
  await assert.rejects(tools.find((t) => t.name === "ask_user").execute("1", { questions: [] }, undefined, undefined, { hasUI: true, ui: { input: async () => "x" } }));
  assert.deepEqual(sent, []);
});

// T130 (L920 B1): a child whose model has no credential: pi fails it (an explicit --model never falls back), and the parent's result says which model.
test("a child pi refused for a missing credential is reported with its model, the provider and that no other model ran; another failure is as before", async () => {
  const { childFailure } = await import("../lib/agents.ts");
  const pi = "No API key found for anthropic.\n\nUse /login to log into a provider via OAuth or API key. See:\n  docs/providers.md";
  assert.equal(childFailure({ name: "jhon-builder", model: "anthropic/claude-opus-4-5" }, 1, null, pi),
    `jhon: the sub-agent "jhon-builder" exited 1: pi found no credential for the provider anthropic; the agent was launched on the model anthropic/claude-opus-4-5.\n${pi}`);
  // L926: the sentence says what pi's line proves, also when the provider is not the agent's.
  assert.equal(childFailure({ name: "jhon-builder", model: "nan/alpha" }, 1, null, "No API key found for openai."),
    `jhon: the sub-agent "jhon-builder" exited 1: pi found no credential for the provider openai; the agent was launched on the model nan/alpha.\nNo API key found for openai.`);
  // L930: only pi's whole line counts; anything else is reported as printed.
  for (const printed of ["note: No API key found for openai. See the docs", "No API key found for openai. See the docs", "No API key found for openai", "  No API key found for openai."]) {
    assert.equal(childFailure({ name: "jhon-builder", model: "nan/alpha" }, 1, null, printed), `jhon: the sub-agent "jhon-builder" exited 1:\n${printed}`, printed);
  }
  assert.equal(childFailure({ name: "jhon-scout", model: "x/y" }, 3, null, "model unavailable"), `jhon: the sub-agent "jhon-scout" exited 3:\nmodel unavailable`);
  assert.equal(childFailure({ name: "jhon-scout", model: "x/y" }, null, "SIGKILL", ""), `jhon: the sub-agent "jhon-scout" exited on SIGKILL`);
});
