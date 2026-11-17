/**
 * T134 (L1021 H1, H2): a sub-agent's first message carries, after its brief,
 * the decisions of the handoff packet (`## Decisions on record`) and
 * CodeGraph's result for what the brief names (`## Code map`); bounded, and
 * left out on any failure, timeout, empty result or missing index, so a
 * session with neither Engram nor CodeGraph launches as before. The scope the
 * guard reads stays the brief; a continue carries neither block.
 *
 * Fakes only: a fake `jhon` (JHON_BIN), a fake `codegraph` first on PATH, a
 * fake pi (JHON_PI_BIN) that records what it was given.
 */
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";
import test, { after } from "node:test";
import jhon from "../extensions/jhon.ts";
import { continuation } from "../lib/agents.ts";
import { CODE_MAP_HEADING, DECISIONS_HEADING, DECISIONS_LINES, TESTS_TO_JUDGE_HEADING, briefNames, decisionsBlock, launchContext, withoutLaunchContext } from "../lib/launch-context.ts";

const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
function tempDir(prefix: string): string {
  const d = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
  temps.push(d);
  return d;
}

const WORKER = "---\nname: worker\ndescription: \"Implement\"\nmodel: anthropic/claude-sonnet-4-5\ntools:\n  - read\n  - edit\n  - mcp__codegraph__codegraph_explore\nbounded: true\n---\nWork.\n";
const BRIEF = "Fix `runAgent` in shell/lib/agents.ts:180 so the launch is bounded.\n\n## Allowed edit surfaces\n- shell/lib/agents.ts\n";
const PACKET = { context_id: "x", change: null, phase: "none", decisions: [{ id: 41, type: "decision", title: "Launch context goes at the end of the brief" }, { id: 7, type: "architecture", title: "The guard reads the task file" }] };
const QUERY = [
  { node: { kind: "function", name: "runAgent", filePath: "shell/lib/agents.ts", startLine: 264, signature: "(agent: AgentDef, task: string): Promise<ChildResult>" }, score: 9 },
  { node: { kind: "function", name: "runAgentLater", filePath: "shell/lib/other.ts", startLine: 3, signature: "()" }, score: 2 },
];
const CALLERS = { symbol: "runAgent", definitions: [{ callers: [{ name: "foreground", kind: "function", filePath: "shell/extensions/jhon.ts", startLine: 316 }] }] };

/** A git repository holding shell/lib/agents.ts; indexed gives it a CodeGraph database. */
function repo(indexed: boolean): string {
  const d = tempDir("jhon-launch-repo-");
  spawnSync("git", ["init", "-q"], { cwd: d });
  mkdirSync(join(d, "shell", "lib"), { recursive: true });
  writeFileSync(join(d, "shell", "lib", "agents.ts"), "export function runAgent() {}\n");
  if (indexed) { mkdirSync(join(d, ".codegraph")); writeFileSync(join(d, ".codegraph", "codegraph.db"), ""); }
  return d;
}

/** Fake jhon, codegraph and pi in one bin dir; each logs its argv to <dir>/<name>.log. */
function fakes(): { dir: string; jhonBin: string; piBin: string; piLog: string } {
  const dir = tempDir("jhon-launch-fakes-");
  const script = (name: string, body: string) => {
    const f = join(dir, name);
    writeFileSync(f, `#!/usr/bin/env node\nconst fs = require("node:fs");\nfs.appendFileSync(${JSON.stringify(join(dir, `${name}.log`))}, JSON.stringify(process.argv.slice(2)) + "\\n");\n${body}`);
    chmodSync(f, 0o755);
    return f;
  };
  const jhonBin = script("jhon", `const wait = Number(process.env.FAKE_JHON_SLEEP || 0);
setTimeout(() => { if (process.env.FAKE_JHON_EXIT) process.exit(Number(process.env.FAKE_JHON_EXIT)); process.stdout.write(process.env.FAKE_PACKET ?? "{}"); }, wait);\n`);
  script("codegraph", `const wait = Number(process.env.FAKE_CG_SLEEP || 0);
setTimeout(() => { if (process.env.FAKE_CG_EXIT) process.exit(Number(process.env.FAKE_CG_EXIT)); const a = process.argv[2]; process.stdout.write(a === "callers" ? process.env.FAKE_CALLERS ?? "{}" : process.env.FAKE_QUERY ?? "[]"); }, wait);\n`);
  const piLog = join(dir, "pi.json");
  const piBin = script("pi", `fs.writeFileSync(${JSON.stringify(piLog)}, JSON.stringify({ argv: process.argv.slice(2), scope: process.env.JHON_PI_TASK_FILE ? fs.readFileSync(process.env.JHON_PI_TASK_FILE, "utf8") : undefined }));
process.stdout.write("done\\n");\n`);
  return { dir, jhonBin, piBin, piLog };
}

const log = (dir: string, name: string): string[][] => { try { return readFileSync(join(dir, `${name}.log`), "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)); } catch { return []; } };

async function withEnv<T>(vars: Record<string, string | undefined>, fn: () => Promise<T> | T): Promise<T> {
  const saved: Record<string, string | undefined> = {};
  for (const [k, v] of Object.entries(vars)) { saved[k] = process.env[k]; if (v === undefined) delete process.env[k]; else process.env[k] = v; }
  try { return await fn(); } finally { for (const [k, v] of Object.entries(saved)) if (v === undefined) delete process.env[k]; else process.env[k] = v; }
}

/**
 * Launch a worker on brief in cwd from a child session (its sub-agents run
 * in the call), with the fakes and env; what pi received, how long the call took.
 */
async function launch(cwd: string, brief: string, env: Record<string, string | undefined> = {}, f = fakes()) {
  const agentDir = tempDir("jhon-launch-agent-");
  mkdirSync(join(agentDir, "agents"));
  writeFileSync(join(agentDir, "agents", "worker.md"), WORKER);
  const vars = { PI_CODING_AGENT_DIR: agentDir, JHON_PI_CHILD: "1", JHON_BIN: f.jhonBin, JHON_PI_BIN: f.piBin, PATH: `${f.dir}${delimiter}${process.env.PATH}`, FAKE_PACKET: JSON.stringify(PACKET), FAKE_QUERY: JSON.stringify(QUERY), FAKE_CALLERS: JSON.stringify(CALLERS), ...env };
  return withEnv(vars, async () => {
    const tools = new Map<string, any>();
    jhon({ on: () => {}, registerTool: (t: any) => tools.set(t.name, t), registerCommand: () => {} });
    const started = Date.now();
    await tools.get("subagent").execute("call-1", { agent: "worker", task: brief }, undefined, undefined, { cwd, hasUI: false, sessionManager: { getSessionId: () => "parent", getSessionFile: () => undefined } });
    const seen = JSON.parse(readFileSync(f.piLog, "utf8")) as { argv: string[]; scope?: string };
    return { prompt: seen.argv.at(-1)!, argv: seen.argv, scope: seen.scope, ms: Date.now() - started, f };
  });
}

test("a launch ends its first message with the decisions on record and the code map, and the scope is the brief", async () => {
  const cwd = repo(true);
  const { prompt, scope, f } = await launch(cwd, BRIEF);
  assert.equal(prompt, `${BRIEF}

${DECISIONS_HEADING}
Added at launch from \`jhon context --handoff\`; search Engram before deciding against one.
- #41 (decision) Launch context goes at the end of the brief
- #7 (architecture) The guard reads the task file

${CODE_MAP_HEADING}
Added at launch from CodeGraph for what the brief names; lines are as of the launch.
- function runAgent shell/lib/agents.ts:264 (agent: AgentDef, task: string): Promise<ChildResult>; callers: foreground (shell/extensions/jhon.ts:316)`);
  assert.equal(scope, BRIEF, "the scope guard reads the brief as written");
  assert.deepEqual(log(f.dir, "jhon").find((a) => a[0] === "context"), ["context", "--phase", "none", "--handoff", "--json"]);
  const cg = log(f.dir, "codegraph");
  assert.ok(cg.some((a) => a.join(" ") === "query runAgent --json"), JSON.stringify(cg));
  assert.ok(cg.some((a) => a.join(" ") === "query agents --json"), "the named file is asked for by its name");
});

test("with neither Engram nor CodeGraph a launch is byte-identical to the brief alone", async () => {
  const cwd = repo(false);
  const noEngram = JSON.stringify({ ...PACKET, decisions: null, decisions_note: "engram unavailable: search Engram for the change's decisions before handing off" });
  const { prompt, argv, scope, f } = await launch(cwd, BRIEF, { FAKE_PACKET: noEngram });
  assert.equal(prompt, BRIEF);
  assert.equal(argv.filter((a) => a.includes(DECISIONS_HEADING) || a.includes(CODE_MAP_HEADING)).length, 0);
  assert.equal(scope, BRIEF);
  assert.deepEqual(log(f.dir, "codegraph"), [], "no index: CodeGraph is never run");
});

test("a failing or slow engine, and a failing or slow CodeGraph, leave the headings out and the launch goes on in time", async () => {
  const cwd = repo(true);
  for (const env of [{ FAKE_JHON_EXIT: "1", FAKE_CG_EXIT: "2" }, { FAKE_PACKET: "not json", FAKE_QUERY: "[" }, { FAKE_PACKET: JSON.stringify({ decisions: [] }), FAKE_QUERY: "[]" }]) {
    const { prompt, scope } = await launch(cwd, BRIEF, env);
    assert.equal(prompt, BRIEF, JSON.stringify(env));
    assert.equal(scope, BRIEF);
  }
  const slow = await launch(cwd, BRIEF, { FAKE_JHON_SLEEP: "8000", FAKE_CG_SLEEP: "8000" });
  assert.equal(slow.prompt, BRIEF);
  assert.ok(slow.ms < 6000, `a slow engine and CodeGraph hold the launch ${slow.ms} ms; the deadlines are 2 s and 3 s`);
});

test("the decisions block is at most 12 lines; the code map at most 4 KB", async () => {
  const many = { decisions: Array.from({ length: 30 }, (_, i) => ({ id: i + 1, type: "decision", title: `Decision ${i + 1}` })) };
  assert.equal(decisionsBlock(many).split("\n").length, DECISIONS_LINES);
  const cwd = repo(true);
  for (let i = 0; i < 8; i++) writeFileSync(join(cwd, "shell", "lib", `f${i}.ts`), "");
  const long = Array.from({ length: 40 }, (_, i) => ({ node: { kind: "function", name: "runAgent", filePath: `shell/lib/agents.ts`, startLine: i + 1, signature: `(${"x".repeat(180)})` } }));
  const f = fakes();
  const block = await withEnv({ PATH: `${f.dir}${delimiter}${process.env.PATH}`, FAKE_QUERY: JSON.stringify(long), FAKE_CALLERS: "{}", FAKE_PACKET: "{}" }, () => launchContext("`runAgent`", cwd, { bin: f.jhonBin }));
  assert.ok(block.startsWith(`\n\n${CODE_MAP_HEADING}\n`), block.slice(0, 80));
  assert.ok(Buffer.byteLength(block.trim(), "utf8") <= 4096, String(Buffer.byteLength(block, "utf8")));
});

test("the brief's names: existing files and path:line references, backticked identifiers; at most 8", () => {
  const cwd = repo(false);
  assert.deepEqual(briefNames("See `runAgent()`, `ExplorationBudget.reset`, shell/lib/agents.ts:12, `shell/lib/agents.ts`, gone/missing.ts:3, `a b`, `x`.", cwd), [
    { kind: "symbol", name: "runAgent" }, { kind: "symbol", name: "reset" }, { kind: "file", path: "shell/lib/agents.ts" },
  ]);
  assert.equal(briefNames(Array.from({ length: 20 }, (_, i) => `\`symbol${i}\``).join(" "), cwd).length, 8);
});

test("a continue repeats neither block, and its scope is still the brief the launch was given", async () => {
  const cwd = repo(true);
  const { prompt } = await launch(cwd, BRIEF);
  assert.notEqual(prompt, BRIEF);
  assert.equal(withoutLaunchContext(prompt), BRIEF);
  // A saved child whose first message carried both blocks.
  const agentDir = tempDir("jhon-launch-agent-");
  mkdirSync(join(agentDir, "agents"));
  writeFileSync(join(agentDir, "agents", "worker.md"), WORKER);
  const sessions = tempDir("jhon-launch-sessions-");
  const file = join(sessions, "2026-11-16T00-00-00-000Z_parent-1.jsonl");
  const children = join(sessions, "2026-11-16T00-00-00-000Z_parent-1", "subagents");
  mkdirSync(children, { recursive: true });
  writeFileSync(join(children, "toolu_1.meta.json"), JSON.stringify({ agentType: "worker", toolUseId: "toolu_1", parentSession: "parent-1" }));
  writeFileSync(join(children, "2026-11-16T00-00-01-000Z_toolu_1.jsonl"), [
    { type: "session", version: 3, id: "toolu_1", cwd },
    { type: "message", message: { role: "user", content: [{ type: "text", text: prompt }] } },
  ].map((l) => JSON.stringify(l)).join("\n") + "\n");
  const sessionManager = { getSessionFile: () => file, getSessionId: () => "parent-1" };
  assert.equal(continuation(agentDir, { file, id: "parent-1" }, "toolu_1").scope, BRIEF, "the scope guard of a continued child reads the brief, not the blocks");
  const f = fakes();
  await withEnv({ PI_CODING_AGENT_DIR: agentDir, JHON_PI_CHILD: "1", JHON_BIN: f.jhonBin, JHON_PI_BIN: f.piBin, PATH: `${f.dir}${delimiter}${process.env.PATH}`, FAKE_PACKET: JSON.stringify(PACKET), FAKE_QUERY: JSON.stringify(QUERY) }, async () => {
    const tools = new Map<string, any>();
    jhon({ on: () => {}, registerTool: (t: any) => tools.set(t.name, t), registerCommand: () => {} });
    await tools.get("subagent_continue").execute("call-2", { id: "toolu_1", message: "Go on with `runAgent`." }, undefined, undefined, { cwd, sessionManager });
  });
  const seen = JSON.parse(readFileSync(f.piLog, "utf8"));
  assert.equal(seen.argv.at(-1), "Go on with `runAgent`.");
  assert.equal(seen.scope, BRIEF);
  assert.deepEqual(log(f.dir, "jhon").filter((a) => a[0] === "context"), [], "a continue asks the engine for nothing");
  assert.deepEqual(log(f.dir, "codegraph"), []);
});

/**
 * A change's linked worktree of repo(true): main holds the index and commits
 * shell/lib/agents.ts (not the index); the worktree has the file and no index.
 */
function worktree(): { main: string; wt: string } {
  const main = repo(true);
  const git = (...args: string[]) => {
    const r = spawnSync("git", ["-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", ...args], { cwd: main, encoding: "utf8" });
    assert.equal(r.status, 0, r.stderr);
  };
  git("add", "shell");
  git("commit", "-q", "-m", "init");
  const wt = join(tempDir("jhon-launch-wt-"), "change");
  git("worktree", "add", "-q", "-b", "change", wt);
  return { main, wt };
}

test("a launch in a change's linked worktree maps the main checkout's index into the worktree", async () => {
  const { main, wt } = worktree();
  const callers = { symbol: "runAgent", definitions: [{ callers: [
    { name: "launch", kind: "function", filePath: "shell/lib/agents.ts", startLine: 20 },
    { name: "foreground", kind: "function", filePath: "shell/extensions/jhon.ts", startLine: 316 },
  ] }] };
  const { prompt, scope, f } = await launch(wt, BRIEF, { FAKE_CALLERS: JSON.stringify(callers) });
  assert.equal(prompt, `${BRIEF}

${DECISIONS_HEADING}
Added at launch from \`jhon context --handoff\`; search Engram before deciding against one.
- #41 (decision) Launch context goes at the end of the brief
- #7 (architecture) The guard reads the task file

${CODE_MAP_HEADING}
Added at launch from CodeGraph for what the brief names; lines are as of the launch.
- function runAgent shell/lib/agents.ts:264 (agent: AgentDef, task: string): Promise<ChildResult>; callers: launch (shell/lib/agents.ts:20)`);
  assert.equal(scope, BRIEF);
  const cg = log(f.dir, "codegraph");
  assert.ok(cg.some((a) => a.join(" ") === `query runAgent --json -p ${main}`), JSON.stringify(cg));
  assert.ok(cg.some((a) => a.join(" ") === `query agents --json -p ${main}`), JSON.stringify(cg));
  assert.ok(cg.some((a) => a.join(" ") === `callers runAgent --json --limit 5 -p ${main}`), JSON.stringify(cg));
  assert.ok(cg.every((a) => a[0] === "query" || a[0] === "callers"), "no init, no index written");
  assert.equal(existsSync(join(wt, ".codegraph")), false, "the worktree gets no index");
});

test("in a linked worktree, results whose files are not in it are left out; with none left, no heading", async () => {
  const { main, wt } = worktree();
  const elsewhere = [{ node: { kind: "function", name: "runAgent", filePath: "shell/lib/moved.ts", startLine: 3, signature: "()" } }];
  const { prompt, f } = await launch(wt, BRIEF, { FAKE_PACKET: "{}", FAKE_QUERY: JSON.stringify(elsewhere), FAKE_CALLERS: "{}" });
  assert.equal(prompt, BRIEF);
  assert.ok(log(f.dir, "codegraph").some((a) => a.join(" ") === `query runAgent --json -p ${main}`), "the main checkout's index was asked");
});

test("a linked worktree of a checkout with no index gets no code map and never runs CodeGraph", async () => {
  const { main, wt } = worktree();
  rmSync(join(main, ".codegraph"), { recursive: true, force: true });
  const { prompt, f } = await launch(wt, BRIEF, { FAKE_PACKET: "{}" });
  assert.equal(prompt, BRIEF);
  assert.deepEqual(log(f.dir, "codegraph"), []);
});

// T137 (L1064 KB2, KB3): verify and each 4R lens get, after the other
// blocks, the tests the change's record marks as turned green by editing
// only their test (`jhon context --judge`); any other agent, and a change
// with nothing to judge, get no such block.
const JUDGED = { ...PACKET, tests_to_judge: [{ project: "b", test: "tests/test_ids.py::test_ids", file: "b/tests/test_ids.py", red_run: "2026-01-01T00:01:00.000Z", green_run: "2026-01-01T00:02:00.000Z", red_tree: "t1", green_tree: "t2" }] };
const TESTS_TO_JUDGE = `${TESTS_TO_JUDGE_HEADING}
Added at launch from the change's execution record: each test below failed in one recorded run and passed in a later one while only its own test file changed in its project. Judge whether it was weakened to pass (an assertion dropped or loosened, a case removed, an expectation rewritten to what the code does); if it was, report it as a finding.
- \`tests/test_ids.py::test_ids\` (b, b/tests/test_ids.py): failed in run 2026-01-01T00:01:00.000Z, passed in run 2026-01-01T00:02:00.000Z`;

test("verify and the 4R lenses are launched with the tests to judge; other agents, and nothing to judge, are not", async () => {
  const cwd = repo(false);
  const f = fakes();
  const noDecisions = { ...JUDGED, decisions: [] };
  await withEnv({ FAKE_PACKET: JSON.stringify(noDecisions), PATH: `${f.dir}${delimiter}${process.env.PATH}` }, async () => {
    for (const agent of ["verify", "review-risk", "review-resilience", "review-readability", "review-reliability"]) {
      assert.equal(await launchContext(BRIEF, cwd, { bin: f.jhonBin, change: "c1", agent }), `\n\n${TESTS_TO_JUDGE}`, agent);
    }
    assert.ok(log(f.dir, "jhon").some((a) => a.join(" ") === "context c1 --phase none --judge --json"), JSON.stringify(log(f.dir, "jhon")));
    const judged = () => log(f.dir, "jhon").filter((a) => a.includes("--judge")).length;
    assert.equal(judged(), 5);
    for (const agent of ["worker", "explore", undefined]) assert.equal(await launchContext(BRIEF, cwd, { bin: f.jhonBin, change: "c1", agent }), "", String(agent));
    assert.equal(judged(), 5, "only a judge's launch asks for the tests to judge");
  });
  await withEnv({ FAKE_PACKET: JSON.stringify({ ...PACKET, decisions: [] }) }, async () => {
    assert.equal(await launchContext(BRIEF, cwd, { bin: f.jhonBin, agent: "verify" }), "", "nothing to judge: no block");
  });
  assert.equal(withoutLaunchContext(`${BRIEF}\n\n${TESTS_TO_JUDGE}`), BRIEF, "a continue cuts the block too");
});

test("a verify launched through the subagent tool ends its first message with the tests to judge", async () => {
  const cwd = repo(false);
  const f = fakes();
  const agentDir = tempDir("jhon-launch-agent-");
  mkdirSync(join(agentDir, "agents"));
  writeFileSync(join(agentDir, "agents", "verify.md"), WORKER.replace("name: worker", "name: verify").replace("bounded: true\n", ""));
  await withEnv({ PI_CODING_AGENT_DIR: agentDir, JHON_PI_CHILD: "1", JHON_BIN: f.jhonBin, JHON_PI_BIN: f.piBin, PATH: `${f.dir}${delimiter}${process.env.PATH}`, FAKE_PACKET: JSON.stringify({ ...JUDGED, decisions: [] }) }, async () => {
    const tools = new Map<string, any>();
    jhon({ on: () => {}, registerTool: (t: any) => tools.set(t.name, t), registerCommand: () => {} });
    await tools.get("subagent").execute("call-1", { agent: "verify", task: "Verify c1.\n" }, undefined, undefined, { cwd, hasUI: false, sessionManager: { getSessionId: () => "parent", getSessionFile: () => undefined } });
    const seen = JSON.parse(readFileSync(f.piLog, "utf8")) as { argv: string[] };
    assert.equal(seen.argv.at(-1), `Verify c1.\n\n\n${TESTS_TO_JUDGE}`);
  });
});
