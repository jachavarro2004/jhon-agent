/**
 * T115e (L705, L715): the context backstop. The main session, never a child,
 * tells the orchestrator once per crossing to delegate the next unit that needs
 * several steps, at min(shell.behaviour.backstop_tokens (100000 by default; 0
 * turns it off), 15% of the model's window). It reads ctx.getContextUsage() at
 * most once per 5 s, re-arms after a compaction drops the context below the
 * threshold, and reaches the agent appended to a tool result or as a persisted
 * message: the system prompt never changes. The ladder: backstop, then the
 * fresh-session notice (150K), then auto-compaction (200K); two on one reply
 * are one notice.
 */
import "./isolated-env.ts";
import { test, after, type TestContext } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import jhon, { CONFIG_FILE } from "../extensions/jhon.ts";
import { BACKSTOP_ENTRY } from "../lib/context-backstop.ts";
import { FRESH_SESSION_ENTRY } from "../lib/fresh-session.ts";
import { installShellSettings, SETTINGS_DEFAULTS, shellSettings } from "../lib/settings.ts";
import { memorySettings } from "./fake-jhon-settings.ts";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
function tempDir(prefix: string): string {
  const d = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
  temps.push(d);
  return d;
}

function agentDir(): string {
  const dir = tempDir("jhon-backstop-agent-");
  writeFileSync(join(dir, "orchestrator.md"), "# Orchestrator\n\nJhon prompt.");
  writeFileSync(join(dir, CONFIG_FILE), JSON.stringify({ jhon: process.env.JHON_TEST_BIN || join(REPO, "build", "jhon"), orchestrator: join(dir, "orchestrator.md"), vcsDirs: [".git"], protected: [], inspectAllow: [] }));
  return dir;
}

type Handler = (event: any, ctx: any) => Promise<any>;
function load(env: Record<string, string> = {}) {
  const saved = { ...process.env };
  const isolated = tempDir("jhon-backstop-home-");
  Object.assign(process.env, { PI_CODING_AGENT_DIR: agentDir(), CLAUDE_CONFIG_DIR: join(isolated, "claude"), JHON_PROFILES: join(isolated, "profiles.json") }, env);
  if (!("JHON_PI_CHILD" in env)) delete process.env.JHON_PI_CHILD;
  const all = new Map<string, Handler[]>();
  const sent: any[] = [];
  try {
    jhon({ on: (n: string, h: Handler) => { all.set(n, [...(all.get(n) ?? []), h]); }, registerTool: () => {}, registerCommand: () => {}, sendMessage: (m: any, o: any) => sent.push({ ...m, options: o }) });
  } finally {
    for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
    Object.assign(process.env, saved);
  }
  const fire = async (name: string, event: any, ctx: any) => {
    let result: any;
    for (const h of all.get(name) ?? []) result = (await h(event, ctx)) ?? result;
    return result;
  };
  return { fire, sent, has: (n: string) => all.has(n) };
}

/** A context whose usage the test sets, counting each getContextUsage call. */
function usage(contextWindow: number) {
  const state = { tokens: 0, calls: 0, notes: [] as string[] };
  const ctx = {
    cwd: tempDir("jhon-backstop-cwd-"),
    ui: { notify: (m: string) => state.notes.push(m) },
    getContextUsage: () => { state.calls += 1; return { tokens: state.tokens, contextWindow, percent: (state.tokens / contextWindow) * 100 }; },
  };
  return { ctx, state };
}

const RESULT = { toolName: "read", input: { path: "a.txt" }, content: [{ type: "text", text: "a\n" }], isError: false };
const appended = (r: any): string[] => (r?.content ?? []).slice(1).map((c: any) => c.text);
const notice = (k: number, t: number) => `Context backstop: the context is at ${k}K tokens, past its ${t}K-token backstop. Delegate the next unit that needs several steps (its reads, edits and checks) to a sub-agent with a short handoff (paths, line ranges, decisions); keep single small edits and state checks inline.`;

function clock(t: TestContext) {
  t.mock.timers.enable({ apis: ["Date"], now: 1_000_000 });
  return (ms: number) => t.mock.timers.tick(ms);
}

test("the backstop is shell.behaviour.backstop_tokens, 100000 by default", () => {
  assert.equal(SETTINGS_DEFAULTS["shell.behaviour.backstop_tokens"], 100000);
  const previous = installShellSettings(memorySettings({ "shell.behaviour.backstop_tokens": 80000 }));
  try { assert.equal(shellSettings().backstopTokens(), 80000); } finally { installShellSettings(previous); }
  const again = installShellSettings(memorySettings());
  try { assert.equal(shellSettings().backstopTokens(), 100000); } finally { installShellSettings(again); }
});

test("on a 1M window it fires once at 100K, appended to the tool result, and the result's own content is untouched", async (t) => {
  const tick = clock(t);
  const { fire } = load();
  const { ctx, state } = usage(1_000_000);
  state.tokens = 99_000;
  assert.equal(appended(await fire("tool_result", RESULT, ctx)).length, 0, "under 100K");
  tick(5000);
  state.tokens = 101_400;
  const r = await fire("tool_result", RESULT, ctx);
  assert.deepEqual(r.content[0], RESULT.content[0], "the result as the tool gave it");
  assert.deepEqual(appended(r), [notice(101, 100)]);
  assert.deepEqual(state.notes, [notice(101, 100)], "the owner hears it too");
  tick(5000);
  state.tokens = 140_000;
  assert.equal(appended(await fire("tool_result", RESULT, ctx)).length, 0, "once per crossing");
});

test("on a 200K window it fires at 15% of the window, 30K", async (t) => {
  const tick = clock(t);
  const { fire } = load();
  const { ctx, state } = usage(200_000);
  state.tokens = 29_000;
  assert.equal(appended(await fire("tool_result", RESULT, ctx)).length, 0);
  tick(5000);
  state.tokens = 31_000;
  assert.deepEqual(appended(await fire("tool_result", RESULT, ctx)), [notice(31, 30)]);
});

test("a child never gets it and never measures", async (t) => {
  clock(t);
  const { fire, sent } = load({ JHON_PI_CHILD: "1" });
  const { ctx, state } = usage(1_000_000);
  state.tokens = 500_000;
  // T122: a child hears its own context budget (test/child-budget.test.ts), never this delegation backstop.
  assert.equal(appended(await fire("tool_result", RESULT, ctx)).filter((n) => n.startsWith("Context backstop")).length, 0);
  await fire("agent_settled", {}, ctx);
  assert.equal(state.calls <= 2, true, "at most the child budget's read and the existing compaction read");
  assert.equal(sent.filter((m) => m.customType === BACKSTOP_ENTRY).length, 0);
  assert.equal(state.notes.filter((n) => n.startsWith("Context backstop")).length, 0);
});

test("a compaction that drops the context below the threshold re-arms it", async (t) => {
  const tick = clock(t);
  const { fire } = load();
  const { ctx, state } = usage(1_000_000);
  state.tokens = 105_000;
  assert.equal(appended(await fire("tool_result", RESULT, ctx)).length, 1);
  await fire("session_compact", { reason: "threshold" }, ctx);
  state.tokens = 40_000;
  assert.equal(appended(await fire("tool_result", RESULT, ctx)).length, 0, "below after the compaction, measured at once");
  tick(5000);
  state.tokens = 102_000;
  assert.deepEqual(appended(await fire("tool_result", RESULT, ctx)), [notice(102, 100)], "the next crossing fires again");
});

test("0 turns it off, the window's share too", async (t) => {
  const tick = clock(t);
  const previous = installShellSettings(memorySettings({ "shell.behaviour.backstop_tokens": 0 }));
  try {
    const { fire } = load();
    const { ctx, state } = usage(200_000);
    state.tokens = 120_000;
    assert.equal(appended(await fire("tool_result", RESULT, ctx)).length, 0);
    tick(5000);
    assert.equal(appended(await fire("tool_result", RESULT, ctx)).length, 0);
    assert.equal(state.calls, 0, "off: never measured");
  } finally { installShellSettings(previous); }
});

test("it reads the context usage at most once per 5 s", async (t) => {
  const tick = clock(t);
  const { fire } = load();
  const { ctx, state } = usage(1_000_000);
  state.tokens = 10_000;
  for (let i = 0; i < 10; i++) { await fire("tool_result", RESULT, ctx); tick(400); }
  assert.equal(state.calls, 1, "4 s of tool results, one read");
  tick(1000);
  await fire("tool_result", RESULT, ctx);
  assert.equal(state.calls, 2, "after 5 s, the next one");
  state.tokens = 150_000;
  await fire("tool_result", RESULT, ctx);
  assert.equal(state.calls, 2, "a crossing within the 5 s waits for the next read");
});

test("the system prompt stays byte-identical before and after it fires", async (t) => {
  const tick = clock(t);
  const { fire } = load();
  const { ctx, state } = usage(1_000_000);
  const prompt = async () => {
    const options = { appendSystemPrompt: "base", sections: {} as Record<string, string>, selectedTools: ["read", "bash"] };
    await fire("before_agent_start", { systemPromptOptions: options }, ctx);
    return JSON.stringify(options);
  };
  const before = await prompt();
  state.tokens = 120_000;
  assert.equal(appended(await fire("tool_result", RESULT, ctx)).length, 1, "it fired");
  tick(5000);
  assert.equal(await prompt(), before);
});

test("the ladder: after a reply past both the backstop and the fresh-session budget, one combined notice", async (t) => {
  clock(t);
  const { fire, sent } = load();
  const { ctx, state } = usage(1_000_000);
  state.tokens = 160_000;
  await fire("agent_settled", {}, ctx);
  assert.equal(state.calls, 1, "the reply's one read serves both");
  assert.equal(sent.length, 1, "one message");
  assert.equal(sent[0].customType, FRESH_SESSION_ENTRY);
  assert.equal(sent[0].display, false);
  assert.deepEqual(sent[0].options, { deliverAs: "nextTurn" });
  assert.ok(sent[0].content.startsWith(notice(160, 100)), sent[0].content);
  assert.match(sent[0].content, /One session per change: the context is at 160K tokens, past its 150K-token budget/);
  assert.deepEqual(state.notes, [sent[0].content], "one notice for the owner");
});

test("after a reply past the backstop alone, a persisted backstop message", async (t) => {
  clock(t);
  const { fire, sent } = load();
  const { ctx, state } = usage(1_000_000);
  state.tokens = 110_000;
  await fire("agent_settled", {}, ctx);
  assert.deepEqual(sent.map((m) => [m.customType, m.content]), [[BACKSTOP_ENTRY, notice(110, 100)]]);
  await fire("agent_settled", {}, ctx);
  assert.equal(sent.length, 1, "once per crossing");
});
