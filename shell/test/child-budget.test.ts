/**
 * T122 (L762 F2, L763): a sub-agent child's context and read budget. The
 * T115a/e ladder is the main session's; a child (JHON_PI_CHILD=1) gets its own:
 *
 * - a context budget notice once per crossing at min(shell.behaviour.child_backstop_tokens
 *   (80000; 0 off), 40% of the window): wrap up, return path:line evidence;
 * - a context cap at min(shell.behaviour.child_cap_tokens (150000; 0 off), 75%
 *   of the window): from it every tool call is refused so the child returns,
 *   and it is stopped three turns later — pi 1.5.0 cannot compact a
 *   child mid-run from an extension (ctx.compact aborts the run) nor take a
 *   per-process compaction setting, so the cap stands in for compaction;
 * - for an explore child with CodeGraph, one read-budget reminder past 12
 *   lookups or 100,000 bytes without a CodeGraph call;
 * - the parent's result names the context cap the child stopped at.
 *
 * T131 (L905, L908): there is no turn cap; a run past 80 turns ends as
 * completed, and the context cap, the timeout and a cancel end it as before.
 *
 * Every notice rides the tool result (the T115e/T117e channel), so the
 * system prompt stays byte-stable (L708). The main session is unaffected.
 */
import "./isolated-env.ts";
import { test, after, type TestContext } from "node:test";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, readdirSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import jhon, { CONFIG_FILE } from "../extensions/jhon.ts";
import { parseAgent, runAgent, type AgentDef, type ChildResult } from "../lib/agents.ts";
import { childBudgetNotice, childCapReason, readBudgetNotice, workerReadBudgetNotice } from "../lib/child-budget.ts";
import { installShellSettings, SETTINGS_DEFAULTS, shellSettings } from "../lib/settings.ts";
import { telemetryDir } from "../lib/telemetry.ts";
import { memorySettings, OWNER_SETTINGS, useMemorySettings } from "./fake-jhon-settings.ts";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
function tempDir(prefix: string): string {
  const d = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
  temps.push(d);
  return d;
}

function agentDir(): string {
  const dir = tempDir("jhon-childbudget-agent-");
  writeFileSync(join(dir, "orchestrator.md"), "# Orchestrator\n\nJhon prompt.");
  writeFileSync(join(dir, CONFIG_FILE), JSON.stringify({ jhon: process.env.JHON_TEST_BIN || join(REPO, "build", "jhon"), orchestrator: join(dir, "orchestrator.md"), vcsDirs: [".git"], protected: [], inspectAllow: [] }));
  return dir;
}

type Handler = (event: any, ctx: any) => Promise<any>;
function load(env: Record<string, string> = {}) {
  const saved = { ...process.env };
  const isolated = tempDir("jhon-childbudget-home-");
  Object.assign(process.env, { PI_CODING_AGENT_DIR: agentDir(), CLAUDE_CONFIG_DIR: join(isolated, "claude"), JHON_PROFILES: join(isolated, "profiles.json") }, env);
  for (const k of ["JHON_PI_CHILD", "JHON_PI_AGENT", "JHON_PI_TOOLS"]) if (!(k in env)) delete process.env[k];
  const all = new Map<string, Handler[]>();
  try {
    jhon({ on: (n: string, h: Handler) => { all.set(n, [...(all.get(n) ?? []), h]); }, registerTool: () => {}, registerCommand: () => {}, sendMessage: () => {} } as any);
  } finally {
    for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
    Object.assign(process.env, saved);
  }
  const fire = async (name: string, event: any, ctx: any) => {
    let result: any;
    for (const h of all.get(name) ?? []) result = (await h(event, ctx)) ?? result;
    return result;
  };
  return { fire };
}

/** A context whose usage the test sets. */
function usage(contextWindow: number) {
  const state = { tokens: 0, shutdowns: 0 };
  const ctx = {
    cwd: tempDir("jhon-childbudget-cwd-"),
    hasUI: false,
    ui: { notify: () => {} },
    shutdown: () => { state.shutdowns += 1; },
    getContextUsage: () => ({ tokens: state.tokens, contextWindow, percent: (state.tokens / contextWindow) * 100 }),
  };
  return { ctx, state };
}

const result = (toolName = "read", text = "a\n", input: Record<string, unknown> = { path: "a.txt" }) => ({ toolName, input, content: [{ type: "text", text }], isError: false });
const appended = (r: any): string[] => (r?.content ?? []).slice(1).map((c: any) => c.text);
const CHILD = { JHON_PI_CHILD: "1", JHON_PI_AGENT: "worker" };
const EXPLORE = { JHON_PI_CHILD: "1", JHON_PI_AGENT: "explore", JHON_PI_TOOLS: "read,grep,find,ls,mcp__codegraph__codegraph_explore" };

function clock(t: TestContext) {
  t.mock.timers.enable({ apis: ["Date"], now: 1_000_000 });
  return (ms: number) => t.mock.timers.tick(ms);
}

test("the child budgets are shell.behaviour.child_backstop_tokens 80000 and child_cap_tokens 150000", () => {
  assert.equal(SETTINGS_DEFAULTS["shell.behaviour.child_backstop_tokens"], 80000);
  assert.equal(SETTINGS_DEFAULTS["shell.behaviour.child_cap_tokens"], 150000);
  const previous = installShellSettings(memorySettings({ "shell.behaviour.child_backstop_tokens": 50000, "shell.behaviour.child_cap_tokens": 90000 }));
  try {
    assert.equal(shellSettings().childBackstopTokens(), 50000);
    assert.equal(shellSettings().childCapTokens(), 90000);
  } finally { installShellSettings(previous); }
});

test("a child hears the context budget once at its threshold, appended to the tool result", async (t) => {
  const tick = clock(t);
  const { fire } = load(CHILD);
  const { ctx, state } = usage(1_000_000);
  state.tokens = 79_000;
  assert.deepEqual(appended(await fire("tool_result", result(), ctx)), [], "under 80K");
  tick(5000);
  state.tokens = 81_200;
  const r = await fire("tool_result", result(), ctx);
  assert.deepEqual(r.content[0], result().content[0], "the result as the tool gave it");
  assert.deepEqual(appended(r), [childBudgetNotice(81_200, 80_000)]);
  assert.match(appended(r)[0], /^Context budget: this sub-agent's context is at 81K tokens, past its 80K-token budget\. Wrap up.*path:line/);
  tick(5000);
  state.tokens = 120_000;
  assert.deepEqual(appended(await fire("tool_result", result(), ctx)), [], "once per crossing");
});

test("on a 100K window the child budget is 40% of it, and the setting moves it", async (t) => {
  const tick = clock(t);
  const { fire } = load(CHILD);
  const { ctx, state } = usage(100_000);
  state.tokens = 41_000;
  assert.deepEqual(appended(await fire("tool_result", result(), ctx)), [childBudgetNotice(41_000, 40_000)]);
  useMemorySettings(t, { "shell.behaviour.child_backstop_tokens": 30000 });
  const other = load(CHILD);
  tick(5000);
  state.tokens = 31_000;
  assert.deepEqual(appended(await other.fire("tool_result", result(), ctx)), [childBudgetNotice(31_000, 30_000)]);
});

test("the main session never hears the child budget nor meets the child cap", async (t) => {
  const tick = clock(t);
  const { fire } = load();
  const { ctx, state } = usage(1_000_000);
  state.tokens = 95_000;
  const r = await fire("tool_result", result(), ctx);
  assert.equal(appended(r).filter((n) => n.startsWith("Context budget")).length, 0);
  tick(5000);
  state.tokens = 400_000;
  assert.equal(await fire("tool_call", { toolName: "read", input: { path: "a.txt" } }, ctx), undefined, "no cap in the main session");
});

test("at the child cap every tool call is refused so it returns, and it is stopped past the grace", async (t) => {
  clock(t);
  const { fire } = load(CHILD);
  const { ctx, state } = usage(1_000_000);
  const call = () => fire("tool_call", { toolName: "read", input: { path: "a.txt" } }, ctx);
  state.tokens = 149_000;
  assert.equal(await call(), undefined, "under 150K");
  state.tokens = 151_000;
  const refused = await call();
  assert.equal(refused?.block, true);
  assert.equal(refused.reason, childCapReason(151_000, 150_000));
  assert.match(refused.reason, /sub-agent context cap.*Call no more tools: reply now with what is done/);
  for (let i = 0; i < 3; i++) await fire("turn_end", {}, ctx);
  assert.equal(state.shutdowns, 0, "within the grace it may still reply");
  await fire("turn_end", {}, ctx);
  assert.equal(state.shutdowns, 1, "past the grace it is stopped");
});

test("child_cap_tokens 0 turns the cap off, the share too", async (t) => {
  clock(t);
  useMemorySettings(t, { "shell.behaviour.child_cap_tokens": 0 });
  const { fire } = load(CHILD);
  const { ctx, state } = usage(200_000);
  state.tokens = 190_000;
  assert.equal(await fire("tool_call", { toolName: "read", input: { path: "a.txt" } }, ctx), undefined);
});

test("an explore child with CodeGraph hears the read budget once past 12 lookups without a CodeGraph call", async (t) => {
  clock(t);
  const { fire } = load(EXPLORE);
  const { ctx } = usage(1_000_000);
  const read = async (tool = "read", text = "x") => appended(await fire("tool_result", result(tool, text), ctx)).filter((n) => n.startsWith("Read budget"));
  for (let i = 0; i < 12; i++) assert.deepEqual(await read(i % 2 ? "grep" : "read"), []);
  assert.deepEqual(await read(), [readBudgetNotice(13, 13)]);
  assert.match(readBudgetNotice(13, 13), /CodeGraph.*path:line/);
  for (let i = 0; i < 20; i++) assert.deepEqual(await read(), [], "once per child");
});

test("a CodeGraph call restarts the explore read count; bytes count too", async (t) => {
  clock(t);
  const { fire } = load(EXPLORE);
  const { ctx } = usage(1_000_000);
  const read = async (tool = "read", text = "x") => appended(await fire("tool_result", result(tool, text), ctx)).filter((n) => n.startsWith("Read budget"));
  for (let i = 0; i < 10; i++) assert.deepEqual(await read(), []);
  assert.deepEqual(await read("mcp__codegraph__codegraph_explore", "symbols"), []);
  for (let i = 0; i < 10; i++) assert.deepEqual(await read(), [], "counted from the CodeGraph call");
  const big = "y".repeat(100_001);
  assert.deepEqual(await read("read", big), [readBudgetNotice(11, 100_011)]);
});

// T125 (L783 F5): worker children read code through bash (cat, sed -n, grep) and never called CodeGraph.
const WORKER = { JHON_PI_CHILD: "1", JHON_PI_AGENT: "worker", JHON_PI_TOOLS: "read,grep,find,ls,edit,write,bash,mcp__codegraph__codegraph_explore" };
const bash = (command: string, text = "x") => result("bash", text, { command });

test("a worker child with CodeGraph hears the worker read budget once past 12 lookups, read-only bash included", async (t) => {
  clock(t);
  for (const agent of ["worker", "jd-fix-agent", "gentle-ai-worker"]) {
    const { fire } = load({ ...WORKER, JHON_PI_AGENT: agent });
    const { ctx } = usage(1_000_000);
    const heard = async (event: any) => appended(await fire("tool_result", event, ctx)).filter((n) => n.startsWith("Read budget"));
    const reads = [bash("cat a.ts"), bash("sed -n 1,80p a.ts"), bash("grep -rn foo shell/lib | head"), bash("ls shell"), result("read", "x")];
    for (let i = 0; i < 12; i++) assert.deepEqual(await heard(reads[i % reads.length]), [], agent);
    assert.deepEqual(await heard(bash("head -n 5 a.ts")), [workerReadBudgetNotice(13, 13)], agent);
    for (let i = 0; i < 20; i++) assert.deepEqual(await heard(bash("cat a.ts")), [], "once per child");
  }
  const notice = workerReadBudgetNotice(13, 13);
  assert.ok(notice.length < 300, String(notice.length));
  assert.match(notice, /codegraph_explore.*already read/);
  assert.match(notice, /only the lines you change/);
  assert.match(notice, /read tool with offset\/limit.*cat\/sed/);
});

// T134 (L1021 H3): the reads real children make open with `cd <dir> &&`; they count, so the reminder fires.
test("a worker's reads that open with `cd <dir> &&` count toward the read budget; `cd x && rm f` does not", async (t) => {
  clock(t);
  const { fire } = load(WORKER);
  const { ctx } = usage(1_000_000);
  const heard = async (event: any) => appended(await fire("tool_result", event, ctx)).filter((n) => n.startsWith("Read budget"));
  for (let i = 0; i < 30; i++) assert.deepEqual(await heard(bash("cd x && rm f")), [], "a removal is no lookup");
  for (let i = 0; i < 12; i++) assert.deepEqual(await heard(bash("cd /repo && cat a.ts")), []);
  assert.deepEqual(await heard(bash("cd /repo && cat a.ts")), [workerReadBudgetNotice(13, 13)]);
});

test("a bash call that writes or runs is no worker lookup; bash read bytes count", async (t) => {
  clock(t);
  const { fire } = load(WORKER);
  const { ctx } = usage(1_000_000);
  const heard = async (event: any) => appended(await fire("tool_result", event, ctx)).filter((n) => n.startsWith("Read budget"));
  for (let i = 0; i < 30; i++) {
    for (const command of ["sed -i s/a/b/ a.ts", "cat a > b.ts", "npm test", "go test ./...", "git commit -m x"]) assert.deepEqual(await heard(bash(command, "y".repeat(10_000))), [], command);
  }
  assert.deepEqual(await heard(bash("cat big.ts", "z".repeat(100_001))), [workerReadBudgetNotice(1, 100_001)]);
});

test("an explore child counts read-only bash too and keeps its own notice", async (t) => {
  clock(t);
  const { fire } = load(EXPLORE);
  const { ctx } = usage(1_000_000);
  const heard = async (event: any) => appended(await fire("tool_result", event, ctx)).filter((n) => n.startsWith("Read budget"));
  for (let i = 0; i < 12; i++) assert.deepEqual(await heard(bash("cat a.ts")), []);
  assert.deepEqual(await heard(bash("cat a.ts")), [readBudgetNotice(13, 13)]);
});

test("a CodeGraph call re-arms a worker's count", async (t) => {
  clock(t);
  const { fire } = load(WORKER);
  const { ctx } = usage(1_000_000);
  const heard = async (event: any) => appended(await fire("tool_result", event, ctx)).filter((n) => n.startsWith("Read budget"));
  for (let i = 0; i < 10; i++) assert.deepEqual(await heard(bash("cat a.ts")), []);
  assert.deepEqual(await heard(result("mcp__codegraph__codegraph_explore", "symbols")), []);
  for (let i = 0; i < 12; i++) assert.deepEqual(await heard(bash("cat a.ts")), [], "counted from the CodeGraph call");
  assert.deepEqual(await heard(bash("cat a.ts")), [workerReadBudgetNotice(13, 13)]);
});

test("a worker without CodeGraph, other agents, and the main session never hear the read budget from bash reads", async (t) => {
  clock(t);
  for (const env of [{ ...WORKER, JHON_PI_TOOLS: "read,bash" }, { ...WORKER, JHON_PI_AGENT: "review-risk" }, {}]) {
    const { fire } = load(env);
    const { ctx } = usage(1_000_000);
    for (let i = 0; i < 20; i++) {
      const r = await fire("tool_result", bash("cat a.ts"), ctx);
      assert.equal(appended(r).filter((n) => n.startsWith("Read budget")).length, 0, JSON.stringify(env));
    }
  }
});

test("a worker child, an explore without CodeGraph, and the main session never hear the read budget", async (t) => {
  clock(t);
  for (const env of [CHILD, { JHON_PI_CHILD: "1", JHON_PI_AGENT: "explore", JHON_PI_TOOLS: "read,grep" }, {}]) {
    const { fire } = load(env);
    const { ctx } = usage(1_000_000);
    for (let i = 0; i < 20; i++) {
      const r = await fire("tool_result", result(), ctx);
      assert.equal(appended(r).filter((n) => n.startsWith("Read budget")).length, 0, JSON.stringify(env));
    }
  }
});

test("the child's system prompt stays byte-identical before and after its notices", async (t) => {
  const tick = clock(t);
  const { fire } = load(EXPLORE);
  const { ctx, state } = usage(1_000_000);
  const prompt = async () => {
    const options = { appendSystemPrompt: "base", sections: {} as Record<string, string>, selectedTools: ["read"] };
    await fire("before_agent_start", { systemPromptOptions: options }, ctx);
    return JSON.stringify(options);
  };
  const before = await prompt();
  state.tokens = 90_000;
  for (let i = 0; i < 13; i++) { await fire("tool_result", result(), ctx); tick(5000); }
  assert.equal(await prompt(), before);
});

/** A fake pi streaming JSON events: a refused tool call (its reason in the tool result), then a final answer. */
function fakeJsonPi(reason: string): string {
  const dir = tempDir("jhon-childbudget-pi-");
  const bin = join(dir, "pi");
  const events = [
    { type: "message_end", message: { role: "assistant", content: [{ type: "toolCall", name: "read" }] } },
    { type: "message_end", message: { role: "toolResult", isError: true, content: [{ type: "text", text: reason }] } },
    { type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "found a.ts:3" }] } },
  ];
  writeFileSync(bin, `#!/usr/bin/env node\nprocess.stdout.write(${JSON.stringify(events.map((e) => JSON.stringify(e)).join("\n") + "\n")});\n`);
  chmodSync(bin, 0o755);
  return bin;
}

const AGENT: AgentDef = { name: "explore", description: "", model: "m/x", tools: ["read"], bounded: false, body: "" };

test("the parent's result names the context cap its child stopped at", async () => {
  const saved = process.env.JHON_PI_BIN;
  process.env.JHON_PI_BIN = fakeJsonPi(childCapReason(151_000, 150_000));
  try {
    const r = await runAgent(AGENT, "look", tempDir("jhon-childbudget-run-"), { onEvent: () => {} });
    assert.equal(r.ok, true);
    assert.equal(r.text, "found a.ts:3\n\n[jhon: explore stopped at its sub-agent context cap; what remains goes to a fresh explore with a handoff, not subagent_continue]");
  } finally { if (saved === undefined) delete process.env.JHON_PI_BIN; else process.env.JHON_PI_BIN = saved; }
});

/** A fake pi streaming `turns` assistant turns, each a tool call but the last, which is its answer; with FAKE_PI_HANG it never ends. */
function fakeTurnsPi(turns: number): string {
  const dir = tempDir("jhon-childbudget-pi-");
  const bin = join(dir, "pi");
  const events = Array.from({ length: turns }, (_, i) => ({ type: "message_end", message: { role: "assistant", content: i + 1 < turns ? [{ type: "toolCall", name: "read" }] : [{ type: "text", text: `done after ${turns} turns` }] } }));
  writeFileSync(bin, `#!/usr/bin/env node\nprocess.stdout.write(${JSON.stringify(events.map((e) => JSON.stringify(e)).join("\n") + "\n")});\nif (process.env.FAKE_PI_HANG) setInterval(() => {}, 1000);\n`);
  chmodSync(bin, 0o755);
  return bin;
}

/** One run of `bin` as pi, with the jhon.subagent events it recorded in a HOME of the test's own. */
async function recorded(t: TestContext, bin: string, run: () => Promise<ChildResult>, env: Record<string, string> = {}): Promise<{ result: ChildResult; events: any[] }> {
  useMemorySettings(t, OWNER_SETTINGS);
  const home = tempDir("jhon-childbudget-telemetry-");
  const saved = { ...process.env };
  Object.assign(process.env, { HOME: home, JHON_PI_BIN: bin }, env);
  delete process.env.JHON_TELEMETRY;
  try {
    const result = await run();
    const dir = telemetryDir({ HOME: home });
    const events = readdirSync(dir).flatMap((f) => readFileSync(join(dir, f), "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)));
    return { result, events: events.map((e) => ({ name: e.name, severity: e.severity, ...e.attributes, "jhon.duration_ms": undefined })) };
  } finally {
    for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
    Object.assign(process.env, saved);
  }
}
const event = (outcome: string, severity: string, turns: number) => ({ name: "jhon.subagent", severity, "gen_ai.agent.name": "explore", "jhon.outcome": outcome, "jhon.duration_ms": undefined, "jhon.turns": turns });

test("a worker past 80 turns keeps running and ends as completed, whatever turn cap its agent file still names", async (t) => {
  for (const line of ["", "maxTurns: 80\n", "maxTurns: 2\n"]) {
    const agent = parseAgent(`---\nname: explore\nmodel: m/x\n${line}tools:\n  - read\n---\nbody`, "explore");
    const { result, events } = await recorded(t, fakeTurnsPi(85), () => runAgent(agent, "look", tempDir("jhon-childbudget-run-"), { onEvent: () => {} }));
    assert.deepEqual(result, { ok: true, text: "done after 85 turns" }, line);
    assert.deepEqual(events, [event("completed", "INFO", 85)], line);
  }
});

test("a run its context cap stopped is still recorded as context-cap", async (t) => {
  const { result, events } = await recorded(t, fakeJsonPi(childCapReason(151_000, 150_000)), () => runAgent(AGENT, "look", tempDir("jhon-childbudget-run-"), { onEvent: () => {} }));
  assert.equal(result.text, "found a.ts:3\n\n[jhon: explore stopped at its sub-agent context cap; what remains goes to a fresh explore with a handoff, not subagent_continue]");
  assert.deepEqual(events, [event("context-cap", "WARN", 2)]);
});

test("a run past its timeout is still stopped and recorded as timeout", async (t) => {
  const { result, events } = await recorded(t, fakeTurnsPi(1), () => runAgent(AGENT, "look", tempDir("jhon-childbudget-run-"), { onEvent: () => {}, timeoutSeconds: 1 }), { FAKE_PI_HANG: "1" });
  assert.deepEqual(result, { ok: false, text: 'jhon: the sub-agent "explore" ran past its 1s timeout and was stopped; it had said:\ndone after 1 turns' });
  assert.deepEqual(events, [event("timeout", "WARN", 1)]);
});

test("a cancelled run still fails as cancelled and is recorded so", async (t) => {
  const stop = new AbortController();
  const { result, events } = await recorded(t, fakeTurnsPi(1), () => runAgent(AGENT, "look", tempDir("jhon-childbudget-run-"), { onEvent: () => stop.abort(), signal: stop.signal }), { FAKE_PI_HANG: "1" });
  assert.deepEqual(result, { ok: false, text: 'jhon: the sub-agent "explore" was cancelled' });
  assert.deepEqual(events, [event("cancelled", "WARN", 1)]);
});

test("the README documents the child budget keys", () => {
  const readme = readFileSync(join(REPO, "shell", "README.md"), "utf8");
  for (const key of ["child_backstop_tokens", "child_cap_tokens", "lib/child-budget.ts"]) assert.ok(readme.includes(key), key);
});
