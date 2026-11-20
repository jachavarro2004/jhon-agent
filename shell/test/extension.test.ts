/**
 * Unit tests for shell/extensions/jhon.ts, Jhon's extension for pi: each pi tool call is
 * reshaped as the Claude Code hook payload and judged by the release's own
 * guards (`jhon hook <name>`, HOOK_BIN), run here from this tree; the main session receives the
 * orchestrator's prompt and a child does not; the reviewers' `inspect` tool
 * runs only what the inspect guard admits.
 *
 *   jhon test
 */

import "./isolated-env.ts";
import { test, after, type TestContext } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { chmodSync, existsSync, mkdtempSync, mkdirSync, readFileSync, realpathSync, rmSync, unwatchFile, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { OWNER_WAIT_EVENT } from "../lib/attention.ts";
import jhon, { claudeCall, inspect, judge, loadConfig, CONFIG_FILE, SESSION_ENTRY, AUTHORIZATION_ENTRY, ANSWER_ONCE, ANSWER_GRANT, ANSWER_NO, ANSWER_SESSION, askClasses, type JhonPiConfig } from "../extensions/jhon.ts";
import { createServer } from "node:net";
import { memoryDir } from "../lib/memory.ts";
import { continuedScope } from "../lib/agents.ts";
import { ModelChoice } from "../lib/model-choice.ts";
import { lddPrompt } from "../lib/ldd-command.ts";
import { OWNER_SETTINGS, useMemorySettings } from "./fake-jhon-settings.ts";
import { SHELL_CONTRACT, contractMismatch, formatEdited } from "../lib/guards.ts";
import { SESSION_SECTION } from "../lib/ui/append-system-prompt.ts";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
// The machine's own Jhon files stay out of reach: the agent dir, the active
// profile and Claude's memory default to a scratch dir a test may override.
const ISOLATED = mkdtempSync(join(tmpdir(), "jhon-ext-isolated-"));
after(() => rmSync(ISOLATED, { recursive: true, force: true }));
Object.assign(process.env, { PI_CODING_AGENT_DIR: join(ISOLATED, "agent"), JHON_PROFILES: join(ISOLATED, "profiles.json"), CLAUDE_CONFIG_DIR: join(ISOLATED, "claude") });
/** The jhon binary the guards run as `jhon hook <name>`: the tree's own, which `jhon test` builds first. */
const HOOK_BIN = process.env.JHON_TEST_BIN || join(REPO, "build", "jhon");
const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });

function tempDir(prefix: string): string {
  const d = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
  temps.push(d);
  return d;
}

/** A git repository with one commit and an uncommitted change. */
function project(): string {
  const d = tempDir("jhon-pi-proj-");
  const git = (...a: string[]) => spawnSync("git", ["-c", "user.email=t@t", "-c", "user.name=t", ...a], { cwd: d, encoding: "utf8" });
  git("init", "-q");
  writeFileSync(join(d, "a.txt"), "a\n");
  git("add", "a.txt");
  git("commit", "-qm", "init");
  writeFileSync(join(d, "a.txt"), "a\nb\n");
  return d;
}

/** project() with LDD set up (ldd/config.yaml): its code edits pass route-guard (T89). */
function lddProject(): string {
  const d = project();
  mkdirSync(join(d, "ldd"), { recursive: true });
  writeFileSync(join(d, "ldd", "config.yaml"), "context: test\n");
  return d;
}

const FRAMEWORK = "/opt/jhon-framework-under-test";
const config = (over: Partial<JhonPiConfig> = {}): JhonPiConfig => ({
  jhon: HOOK_BIN, orchestrator: "/nonexistent", vcsDirs: [".git", ".hg", ".svn"], protected: [FRAMEWORK],
  inspectAllow: ["git diff", "git show", "codegraph callers", "codegraph impact", "codegraph affected"], ...over,
});
const parent = (cwd: string, interactive = true) => ({ config: config(), cwd, child: false, interactive });
const child = (cwd: string) => ({ config: config(), cwd, child: true, interactive: false });

test("pi calls are reshaped as Claude Code calls; a tool no guard reads passes", () => {
  assert.deepEqual(claudeCall("bash", { command: "ls" }), { tool_name: "Bash", tool_input: { command: "ls" } });
  assert.deepEqual(claudeCall("bash_background", { command: "npm test", label: "t" }), { tool_name: "Bash", tool_input: { command: "npm test" } });
  assert.deepEqual(claudeCall("monitor", { command: "tail -f log", timeoutSeconds: 60 }), { tool_name: "Bash", tool_input: { command: "tail -f log" } });
  assert.deepEqual(claudeCall("write", { path: "a.ts", content: "x" }), { tool_name: "Write", tool_input: { file_path: "a.ts", content: "x" } });
  assert.deepEqual(claudeCall("edit", { path: "a.ts", edits: [] }), { tool_name: "Edit", tool_input: { file_path: "a.ts", edits: [] } });
  // The write guard computes the edited file from pi's edits, as from Claude Code's old_string and new_string.
  assert.deepEqual(claudeCall("edit", { path: "a.ts", edits: [{ oldText: "a", newText: "b" }] }), { tool_name: "Edit", tool_input: { file_path: "a.ts", edits: [{ old_string: "a", new_string: "b" }] } });
  assert.deepEqual(claudeCall("read", { path: "a.ts" }), { tool_name: "Read", tool_input: { file_path: "a.ts" } });
  assert.deepEqual(claudeCall("grep", { pattern: "x", path: "src", glob: "*.ts" }), { tool_name: "Grep", tool_input: { path: "src", glob: "*.ts" } });
  // pi's find is Claude Code's Glob: the secrets guard reads its pattern under its path.
  assert.deepEqual(claudeCall("find", { pattern: "**/*.json", path: "src", limit: 5 }), { tool_name: "Glob", tool_input: { pattern: "**/*.json", path: "src" } });
  assert.equal(claudeCall("subagent_run", { agent: "worker", task: "x" }), null);
});

test("a call no guard can judge is refused: powershell, and AskClaude outside read or none", () => {
  assert.match((claudeCall("powershell", { command: "ls" }) as { refuse: string }).refuse, /powershell/);
  assert.match((claudeCall("AskClaude", { prompt: "fix it", mode: "full" }) as { refuse: string }).refuse, /mode "read" or "none"/);
  assert.match((claudeCall("AskClaude", { prompt: "fix it" }) as { refuse: string }).refuse, /AskClaude/);
  assert.equal(claudeCall("AskClaude", { prompt: "review", mode: "read" }), null);
});

test("the safety guard refuses an irreversible command in the main session and in a child, and the work survives", async () => {
  const d = project();
  for (const ctx of [parent(d), child(d)]) {
    const r = await judge("bash", { command: "git push --force" }, ctx);
    assert.ok(r && "block" in r, JSON.stringify(r));
    assert.match(r.reason, /^safety-guard: refused: git push --force/);
  }
  // A deletion that loses work is the owner's to approve: asked in the main session, refused in a child.
  const asked = await judge("bash", { command: "git reset --hard" }, parent(d));
  assert.ok(asked && "ask" in asked && /^destroy: git reset --hard/.test(asked.ask), JSON.stringify(asked));
  const refused = await judge("bash", { command: "git reset --hard" }, child(d));
  assert.ok(refused && "block" in refused && /jhon grant destroy/.test(refused.reason), JSON.stringify(refused));
  assert.equal(await judge("bash", { command: "git status --short" }, parent(d)), null);
  assert.equal(spawnSync("git", ["status", "--short"], { cwd: d, encoding: "utf8" }).stdout.trim(), "M a.txt");
});

test("an outward-facing command is asked in an interactive main session and refused in a child or without a UI", async () => {
  const d = project();
  const asked = await judge("bash", { command: "git push origin main" }, parent(d));
  assert.ok(asked && "ask" in asked, JSON.stringify(asked));
  assert.match(asked.ask, /push/);
  // A child never asks, even were it to report a UI: its payload carries an agent_id.
  for (const ctx of [child(d), parent(d, false), { ...child(d), interactive: true }]) {
    const r = await judge("bash", { command: "git push origin main" }, ctx);
    assert.ok(r && "block" in r, JSON.stringify(r));
    assert.match(r.reason, /jhon grant push/);
  }
});

test("the write guards refuse a write into a VCS directory and into the framework; a project file passes", async () => {
  const d = lddProject();
  const vcs = await judge("write", { path: join(d, ".git", "config"), content: "x" }, child(d));
  assert.ok(vcs && "block" in vcs && /vcs-guard/.test(vcs.reason), JSON.stringify(vcs));
  // Inside the project, so the write guard passes it and the framework guard judges it.
  const framework = join(d, "framework");
  const fw = await judge("edit", { path: join(framework, "orchestrator.md") }, { ...child(d), config: config({ protected: [framework] }) });
  assert.ok(fw && "block" in fw && /framework-guard/.test(fw.reason), JSON.stringify(fw));
  assert.equal(await judge("write", { path: join(d, "src", "b.ts"), content: "x" }, child(d)), null);
});

test("in a repository with no LDD layout pi's first code edit runs with a note naming init.md; setting LDD up and reads pass with none (T89, T153)", async () => {
  const d = project();
  // The steps the session line names: the install the hook runs from (JHON_ROOT, else the binary's root).
  const root = process.env.JHON_ROOT || dirname(dirname(HOOK_BIN));
  const steps = existsSync(join(root, "prose/skills/ldd/references/init.md")) ? join(root, "prose/skills/ldd/references/init.md") : "~/.local/share/jhon/current/prose/skills/ldd/references/init.md";
  const want = "note (not a refusal; the call ran): LDD is not set up in this project yet: set it up by following `" + steps +
    "` (ask how to deliver and test with your ask tool, or use the recommended values), and before you finish, start this edit's change (tasks.md) and record a run (`jhon executions run`)";
  for (const [tool, input] of [["edit", { path: join(d, "a.txt"), edits: [{ oldText: "b", newText: "c" }] }], ["write", { path: join(d, "src", "x.py"), content: "x" }],
    ["bash", { command: "sed -i 's/b/c/' a.txt" }]] as const) {
    for (const ctx of [child(d), parent(d)]) {
      assert.equal(await judge(tool, input, ctx), null, tool);
      assert.equal(await formatEdited(ctx.config, tool, input, d), want, tool);
    }
  }
  for (const [tool, input] of [["bash", { command: "mkdir -p ldd/specs ldd/changes/archive" }], ["write", { path: join(d, "ldd", "config.yaml"), content: "context: x\n" }],
    ["read", { path: join(d, "a.txt") }], ["grep", { pattern: "a", path: d }], ["find", { pattern: "*.txt", path: d }]] as const) {
    assert.equal(await judge(tool, input, child(d)), null, `${tool} ${JSON.stringify(input)}`);
  }
  mkdirSync(join(d, "ldd"), { recursive: true });
  writeFileSync(join(d, "ldd", "config.yaml"), "context: x\n");
  assert.equal(await judge("edit", { path: join(d, "a.txt"), edits: [{ oldText: "b", newText: "c" }] }, child(d)), null, "once set up");
});

test("no file tool reads or edits a credentials file, nor a command deletes one; a command may read it; its template is any file", async () => {
  const d = lddProject();
  for (const [tool, input] of [["read", { path: join(d, ".env") }], ["edit", { path: join(d, ".env.production") }], ["write", { path: join(d, ".env"), content: "x" }],
    ["grep", { pattern: "KEY", path: join(d, ".env") }], ["bash", { command: "rm .env.local" }]] as const) {
    const r = await judge(tool, input, child(d));
    assert.ok(r && "block" in r && /holds credentials/.test(r.reason), `${tool} ${JSON.stringify(input)}: ${JSON.stringify(r)}`);
  }
  for (const [tool, input] of [["read", { path: join(d, ".env.example") }], ["edit", { path: join(d, ".env.example") }], ["bash", { command: "cat .env.sample" }],
    ["bash", { command: "cat .env" }], ["bash", { command: "docker compose --env-file .env up -d" }]] as const) {
    assert.equal(await judge(tool, input, child(d)), null, `${tool} ${JSON.stringify(input)}`);
  }
});

test("a guard that cannot run refuses the call", async () => {
  const d = project();
  const r = await judge("bash", { command: "ls" }, { ...parent(d), config: config({ jhon: "/nonexistent-jhon" }) });
  assert.ok(r && "block" in r && /failed/.test(r.reason), JSON.stringify(r));
});

/** A fake jhon that never answers: it sleeps past any test's timeout. */
function silentJhon(): string {
  const bin = join(tempDir("jhon-pi-silent-"), "jhon");
  writeFileSync(bin, "#!/bin/sh\nexec sleep 30\n");
  chmodSync(bin, 0o755);
  return bin;
}

test("a guard that does not answer in time refuses the call, says why, and never holds pi past the timeout", async () => {
  const d = project();
  const started = Date.now();
  const r = await judge("bash", { command: "ls" }, { ...parent(d), config: config({ jhon: silentJhon() }), hookTimeoutMs: 300 });
  const took = Date.now() - started;
  assert.deepEqual(r, { block: true, reason: "jhon hook pre-tool did not answer in 0.3s; the call is refused; try again or check `jhon doctor`" });
  assert.ok(took < 5000, `judged in ${took} ms`);
  // A child's call is refused the same way; nothing a silent guard does lets a call through.
  const write = await judge("write", { path: join(d, "b.ts"), content: "x" }, { ...child(d), config: config({ jhon: silentJhon() }), hookTimeoutMs: 300 });
  assert.ok(write && "block" in write && /did not answer in 0\.3s/.test(write.reason), JSON.stringify(write));
  assert.equal(existsSync(join(d, "b.ts")), false);
});

/** A fake jhon running `body` as its shell script. */
function scriptJhon(body: string): string {
  const bin = join(tempDir("jhon-pi-script-"), "jhon");
  writeFileSync(bin, `#!/bin/sh\n${body}\n`);
  chmodSync(bin, 0o755);
  return bin;
}

/** Whether a timer set before `run` fires before `run`'s verdict settles: the guard leaves pi's event loop free (T102). */
async function loopFreeWhile<T>(run: () => T | Promise<T>): Promise<{ verdict: T; ticked: boolean }> {
  let ticked = false;
  const timer = setTimeout(() => { ticked = true; }, 20);
  const verdict = await run();
  const settledTicked = ticked;
  clearTimeout(timer);
  return { verdict, ticked: settledTicked };
}

test("a slow guard leaves pi's event loop free while it judges, and its verdict still counts (T102)", async () => {
  const d = project();
  const slow = scriptJhon("cat >/dev/null; sleep 0.5; echo 'safety-guard: refused: slow' >&2; exit 2");
  const { verdict, ticked } = await loopFreeWhile(() => judge("bash", { command: "ls" }, { ...parent(d), config: config({ jhon: slow }) }));
  assert.equal(ticked, true, "a timer fired while the guard ran");
  assert.deepEqual(verdict, { block: true, reason: "safety-guard: refused: slow" });
});

test("a guard that times out refuses with the same message without blocking the event loop (T102)", async () => {
  const d = project();
  const started = Date.now();
  const { verdict, ticked } = await loopFreeWhile(() => judge("bash", { command: "ls" }, { ...parent(d), config: config({ jhon: silentJhon() }), hookTimeoutMs: 300 }));
  assert.ok(Date.now() - started < 5000, "never held past the timeout");
  assert.equal(ticked, true, "a timer fired while the guard ran");
  assert.deepEqual(verdict, { block: true, reason: "jhon hook pre-tool did not answer in 0.3s; the call is refused; try again or check `jhon doctor`" });
});

test("a guard exiting non-zero refuses with the same message: exit 2 its stderr, any other its failure (T102)", async () => {
  const d = project();
  const two = await judge("bash", { command: "ls" }, { ...parent(d), config: config({ jhon: scriptJhon("cat >/dev/null; echo 'vcs-guard: refused: x' >&2; exit 2") }) });
  assert.deepEqual(two, { block: true, reason: "vcs-guard: refused: x" });
  const { verdict, ticked } = await loopFreeWhile(() => judge("bash", { command: "ls" }, { ...parent(d), config: config({ jhon: scriptJhon("cat >/dev/null; sleep 0.2; echo boom >&2; exit 3") }) }));
  assert.equal(ticked, true, "a timer fired while the guard ran");
  assert.deepEqual(verdict, { block: true, reason: "pre-tool failed (boom); the call is refused" });
  const silent = await judge("bash", { command: "ls" }, { ...parent(d), config: config({ jhon: scriptJhon("cat >/dev/null; exit 5") }) });
  assert.deepEqual(silent, { block: true, reason: "pre-tool failed (exit 5); the call is refused" });
});

test("tool_call blocks a slow guard's refusal end to end: nothing passes while the verdict is pending (T102)", async () => {
  const d = project();
  const dir = agentDir();
  writeFileSync(join(dir, CONFIG_FILE), JSON.stringify(config({ jhon: scriptJhon(`if [ "$1" = contract ]; then printf '{"shell":"${SHELL_CONTRACT}"}'; exit 0; fi\ncat >/dev/null; sleep 0.3; echo 'safety-guard: refused: slow' >&2; exit 2`) })));
  const call = loaded(dir).handlers.get("tool_call")!;
  const { verdict, ticked } = await loopFreeWhile(() => call({ toolName: "bash", input: { command: "ls" } }, { cwd: d, hasUI: false }));
  assert.equal(ticked, true, "a timer fired while the guard ran");
  assert.deepEqual(verdict, { block: true, reason: "safety-guard: refused: slow" });
});

test("a binary that does not answer jhon contract in time is a mismatch, like one that cannot run", () => {
  const started = Date.now();
  assert.match(contractMismatch(config({ jhon: silentJhon() }), SHELL_CONTRACT, 300) ?? "", /speaks shell contract \(none\).*jhon sync/);
  assert.ok(Date.now() - started < 5000);
});

/** A fake jhon whose `contract --json` prints `out` and exits with `code`. */
function fakeJhon(out: string, code = 0): string {
  const bin = join(tempDir("jhon-pi-fake-"), "jhon");
  writeFileSync(bin, `#!/bin/sh\nprintf '%s' '${out}'\nexit ${code}\n`);
  chmodSync(bin, 0o755);
  return bin;
}

test("the shell speaks the contract its package.json names, and the tree's binary speaks it too", () => {
  assert.equal(SHELL_CONTRACT, JSON.parse(readFileSync(join(REPO, "shell", "package.json"), "utf8")).jhon.contract);
  assert.equal(contractMismatch(config()), null);
});

test("a binary that speaks another contract, or none, is a mismatch that names both sides and jhon sync", () => {
  const other = contractMismatch(config({ jhon: fakeJhon('{"shell":"jhon.shell/v0"}') }));
  assert.match(other ?? "", /speaks shell contract jhon\.shell\/v0/);
  assert.match(other ?? "", new RegExp(`needs ${SHELL_CONTRACT.replace(/[./]/g, "\\$&")}`));
  assert.match(other ?? "", /jhon sync/);
  assert.match(contractMismatch(config({ jhon: fakeJhon("", 2) })) ?? "", /speaks shell contract \(none\)/);
  assert.match(contractMismatch(config({ jhon: "/nonexistent-jhon" })) ?? "", /\(none\)/);
});

test("with a mismatched binary the extension refuses every guarded call and lets the rest through", async () => {
  const dir = tempDir("jhon-pi-agent-");
  writeFileSync(join(dir, CONFIG_FILE), JSON.stringify(config({ jhon: fakeJhon('{"shell":"jhon.shell/v0"}') })));
  const { handlers, tools } = loaded(dir);
  const call = handlers.get("tool_call")!;
  assert.match((await call({ toolName: "bash", input: { command: "ls" } }, { cwd: dir })).reason, /speaks shell contract jhon\.shell\/v0/);
  assert.equal(await call({ toolName: "ls", input: { path: "a" } }, { cwd: dir }), undefined);
  await assert.rejects(tools.find((t) => t.name === "inspect").execute("1", { command: "git diff" }, undefined, undefined, { cwd: dir }), /speaks shell contract/);
});

test("inspect runs what the inspect guard admits and refuses the rest", async () => {
  const d = project();
  const diff = await inspect("git diff -- a.txt", config(), d);
  assert.equal(diff.ok, true, diff.text);
  assert.match(diff.text, /^\+b$/m);
  const refused = await inspect("git checkout -- a.txt", config(), d);
  assert.equal(refused.ok, false);
  assert.match(refused.text, /inspect-guard/);
  assert.equal(spawnSync("git", ["status", "--short"], { cwd: d, encoding: "utf8" }).stdout.trim(), "M a.txt");
});

// ── the extension as pi loads it ────────────────────────────────────────────

type Handler = (event: any, ctx: any) => Promise<any>;
/**
 * pi's event bus as the extension sees it: every handler of an event runs, in
 * order, and the event's result is the last one a handler returned.
 */
function bus() {
  const all = new Map<string, Handler[]>();
  const on = (name: string, h: Handler) => { all.set(name, [...(all.get(name) ?? []), h]); };
  const handlers = {
    get: (name: string): Handler | undefined => all.has(name) ? (async (event: any, ctx: any) => {
      let result: any;
      for (const h of all.get(name)!) result = (await h(event, ctx)) ?? result;
      return result;
    }) as Handler : undefined,
  };
  return { on, handlers };
}

function loaded(agentDir: string, env: Record<string, string | undefined> = {}) {
  const saved = { PI_CODING_AGENT_DIR: process.env.PI_CODING_AGENT_DIR, JHON_PI_CHILD: process.env.JHON_PI_CHILD, JHON_PI_MAX_TURNS: process.env.JHON_PI_MAX_TURNS, CLAUDE_CONFIG_DIR: process.env.CLAUDE_CONFIG_DIR };
  // The project memory is read from a Claude config dir of the test's own, never the real ~/.claude.
  Object.assign(process.env, { PI_CODING_AGENT_DIR: agentDir, CLAUDE_CONFIG_DIR: join(agentDir, "claude"), ...env });
  if (env.JHON_PI_CHILD === undefined) delete process.env.JHON_PI_CHILD;
  if (env.JHON_PI_MAX_TURNS === undefined) delete process.env.JHON_PI_MAX_TURNS;
  const { on, handlers } = bus();
  const tools: any[] = [];
  try {
    jhon({ on, registerTool: (t: any) => tools.push(t), registerCommand: () => {} });
  } finally {
    for (const [k, v] of Object.entries(saved)) if (v === undefined) delete process.env[k]; else process.env[k] = v;
  }
  return { handlers, tools };
}

function agentDir(prompt = "# Orchestrator\n\nJhon prompt."): string {
  const dir = tempDir("jhon-pi-agent-");
  writeFileSync(join(dir, "orchestrator.md"), prompt);
  writeFileSync(join(dir, CONFIG_FILE), JSON.stringify(config({ orchestrator: join(dir, "orchestrator.md") })));
  return dir;
}

test("the main session's prompt gains the orchestrator once; a child's does not", async () => {
  const dir = agentDir();
  assert.ok(loadConfig(dir));
  const main = loaded(dir).handlers.get("before_agent_start")!;
  const options = { appendSystemPrompt: "base", sections: {} as Record<string, string> };
  await main({ systemPromptOptions: options }, { cwd: dir });
  await main({ systemPromptOptions: options }, { cwd: dir });
  assert.ok(options.appendSystemPrompt.startsWith("base\n\n# Orchestrator\n\nJhon prompt."), options.appendSystemPrompt);
  assert.equal(options.appendSystemPrompt.split("# Orchestrator").length, 2, "appended once");
  assert.ok(options.sections[SESSION_SECTION].startsWith("## Project memory"), options.sections[SESSION_SECTION]);
  assert.equal(options.sections[SESSION_SECTION].split("## Project memory").length, 2, "appended once");
  const kid = { appendSystemPrompt: "" };
  await loaded(dir, { JHON_PI_CHILD: "1" }).handlers.get("before_agent_start")!({ systemPromptOptions: kid }, {});
  assert.equal(kid.appendSystemPrompt, "");
});

// T31 D: in a project that uses LDD, pi's orchestrator is headed by the
// binding lead Claude Code's gets and followed by the project's rules; never
// in a project without LDD, never in a child.
test("the main session's prompt in an LDD project carries the binding lead and the project's rules; a child's and a plain project's do not", async () => {
  const dir = agentDir(), repo = project(), plain = project();
  mkdirSync(join(repo, "openspec"), { recursive: true });
  writeFileSync(join(repo, "openspec", "config.yaml"), "delivery: change\n");
  const main = loaded(dir).handlers.get("before_agent_start")!;
  const ldd = { appendSystemPrompt: "" };
  await main({ systemPromptOptions: ldd }, { cwd: repo });
  assert.ok(ldd.appendSystemPrompt.startsWith("Jhon runs this session as the jhon-orchestrator agent. Its definition is binding: it says how this session works, whatever the session's own system prompt says.\n\n# Orchestrator\n\nJhon prompt.\n\n## This project's rules"), ldd.appendSystemPrompt);
  assert.match(ldd.appendSystemPrompt, /One PR per change/);
  // T115d: the route line changes with the diff, so it never sits in the appended prompt.
  assert.doesNotMatch(ldd.appendSystemPrompt, /Route, from the change's diff now/);
  // L264: pi's "no `!`" rule reaches the session once, in the shell's note.
  assert.equal(ldd.appendSystemPrompt.split("has no `!`").length - 1, 1, ldd.appendSystemPrompt);
  const other = { appendSystemPrompt: "" };
  await main({ systemPromptOptions: other }, { cwd: plain });
  assert.ok(other.appendSystemPrompt.startsWith("# Orchestrator\n\nJhon prompt."), other.appendSystemPrompt);
  assert.doesNotMatch(other.appendSystemPrompt, /is binding|This project's rules/);
  // T57c, T72: where LDD is not set up yet one line says so; an LDD project has none.
  // T64: the line is the engine's, the one Claude Code's session start gets.
  const LDD_INACTIVE = JSON.parse(spawnSync(HOOK_BIN, ["hook", "session-brief", "jhon-orchestrator"], { input: JSON.stringify({ cwd: plain }), encoding: "utf8" }).stdout).inactive;
  assert.ok(LDD_INACTIVE.startsWith("LDD is not set up in this project yet (`jhon status`); read-only work sets up nothing. At your first code change, set it up by following `"), LDD_INACTIVE);
  // L506: pi may have no skill to run either; the line names the steps' file,
  // L534: by its absolute path in the install the hook runs from (JHON_ROOT,
  // else the binary's root), the ~ text only where that install has none.
  const INIT_ROOT = process.env.JHON_ROOT || dirname(dirname(HOOK_BIN));
  const INIT_STEPS = existsSync(join(INIT_ROOT, "prose/skills/ldd/references/init.md")) ? join(INIT_ROOT, "prose/skills/ldd/references/init.md") : "~/.local/share/jhon/current/prose/skills/ldd/references/init.md";
  assert.ok(LDD_INACTIVE.endsWith("`" + INIT_STEPS + "`: ask how to deliver and test by your ask tool only (else the recommended values, never asked in prose), say in one line what you set up, then work under LDD on the change's own branch. Until it is set up, a commit or a PR is refused."), LDD_INACTIVE);
  assert.equal(other.appendSystemPrompt.split(LDD_INACTIVE).length - 1, 1, other.appendSystemPrompt);
  assert.ok(!ldd.appendSystemPrompt.includes(LDD_INACTIVE));
  // T72 (L555): `jhon ldd off` is gone; a project its old record says off
  // keeps its rules, and is told once that LDD is always on.
  const state = tempDir("jhon-pi-ldd-state-"), savedState = process.env.XDG_STATE_HOME;
  process.env.XDG_STATE_HOME = state;
  try {
    assert.notEqual(spawnSync(HOOK_BIN, ["ldd", "off"], { cwd: repo, encoding: "utf8" }).status, 0);
    const root = realpathSync(repo);
    mkdirSync(join(state, "jhon", "ldd-switch"), { recursive: true });
    writeFileSync(join(state, "jhon", "ldd-switch", createHash("sha256").update(root).digest("hex") + ".json"), JSON.stringify({ ldd: "off", root, since: "2026-10-01T00:00:00Z" }));
    const NOTICE = "LDD is always on with Jhon now; the old switch-off no longer applies.";
    const first = { appendSystemPrompt: "" }, again = { appendSystemPrompt: "" };
    await loaded(dir).handlers.get("before_agent_start")!({ systemPromptOptions: first }, { cwd: repo });
    assert.match(first.appendSystemPrompt, /This project's rules/);
    assert.equal(first.appendSystemPrompt.split(NOTICE).length - 1, 1, first.appendSystemPrompt);
    await loaded(dir).handlers.get("before_agent_start")!({ systemPromptOptions: again }, { cwd: repo });
    assert.match(again.appendSystemPrompt, /This project's rules/);
    assert.ok(!again.appendSystemPrompt.includes(NOTICE), again.appendSystemPrompt);
  } finally {
    if (savedState === undefined) delete process.env.XDG_STATE_HOME; else process.env.XDG_STATE_HOME = savedState;
  }
  // A session-brief that fails says nothing: no rules, and no "inactive" line either (L477, A4).
  const broken = agentDir();
  const fake = join(broken, "jhon-fake");
  writeFileSync(fake, `#!/bin/sh\nif [ "$2" = session-brief ]; then exit 1; fi\nexec '${HOOK_BIN}' "$@"\n`);
  chmodSync(fake, 0o755);
  writeFileSync(join(broken, CONFIG_FILE), JSON.stringify(config({ jhon: fake, orchestrator: join(broken, "orchestrator.md") })));
  const failed = { appendSystemPrompt: "" };
  await loaded(broken).handlers.get("before_agent_start")!({ systemPromptOptions: failed }, { cwd: repo });
  assert.ok(failed.appendSystemPrompt.startsWith("# Orchestrator\n\nJhon prompt."), failed.appendSystemPrompt);
  assert.ok(!failed.appendSystemPrompt.includes(LDD_INACTIVE), failed.appendSystemPrompt);
  const kid = { appendSystemPrompt: "" };
  await loaded(dir, { JHON_PI_CHILD: "1" }).handlers.get("before_agent_start")!({ systemPromptOptions: kid }, { cwd: repo });
  assert.equal(kid.appendSystemPrompt, "");
});

// T42c: codegraph's tool and its contract reach the model only in a
// repository CodeGraph has indexed (its root holds .codegraph/codegraph.db).
test("codegraph's contract and tool reach a session only when its repository is indexed", async () => {
  const CG = "mcp__codegraph__codegraph_explore";
  const INIT = "- In a real project whose root (`git rev-parse --show-toplevel`) has no\n  `.codegraph/`, run `codegraph init <root>` once; never in `$HOME` or a\n  temporary directory.";
  const dir = agentDir(`# Orchestrator\n\nJhon prompt.\n\n## CodeGraph\n\nQuery CodeGraph first.\n\n${INIT} Fall back to filesystem tools only when CodeGraph\n  fails, and say so.\n- Never run \`codegraph uninit\`.\n\n## Engram\n\nMemory rules.`);
  const indexed = project(), plain = project();
  mkdirSync(join(indexed, ".codegraph"), { recursive: true });
  writeFileSync(join(indexed, ".codegraph", "codegraph.db"), "");
  mkdirSync(join(indexed, "sub"), { recursive: true });
  const main = loaded(dir).handlers.get("before_agent_start")!;
  const on = { appendSystemPrompt: "", selectedTools: ["read", CG] };
  await main({ systemPromptOptions: on }, { cwd: join(indexed, "sub") });
  assert.ok(on.appendSystemPrompt.includes(`## CodeGraph\n\nQuery CodeGraph first.\n\n${INIT} Fall back`), on.appendSystemPrompt);
  assert.match(on.appendSystemPrompt, /codegraph uninit`\.\n\n## Engram/);
  assert.deepEqual(on.selectedTools, ["read", CG]);
  const off = { appendSystemPrompt: "", selectedTools: ["read", CG] };
  await main({ systemPromptOptions: off }, { cwd: plain });
  // The init rule stays, word for word, so a new repository still gets indexed.
  assert.ok(off.appendSystemPrompt.includes(`Jhon prompt.\n\n## CodeGraph\n\n${INIT}\n\n## Engram\n\nMemory rules.`), off.appendSystemPrompt);
  assert.doesNotMatch(off.appendSystemPrompt, /Query CodeGraph first|Fall back|uninit/);
  assert.deepEqual(off.selectedTools, ["read"]);
  const kid = { appendSystemPrompt: "", selectedTools: ["read", CG] };
  await loaded(dir, { JHON_PI_CHILD: "1" }).handlers.get("before_agent_start")!({ systemPromptOptions: kid }, { cwd: plain });
  assert.deepEqual(kid.selectedTools, ["read"], "a child in an unindexed repository loses the tool too");
  const kidOn = { appendSystemPrompt: "", selectedTools: ["read", CG] };
  await loaded(dir, { JHON_PI_CHILD: "1" }).handlers.get("before_agent_start")!({ systemPromptOptions: kidOn }, { cwd: indexed });
  assert.deepEqual(kidOn.selectedTools, ["read", CG]);
});

// T45b: pi connects its MCP servers after before_agent_start, so codegraph's
// tool joins the active set late; each request drops it again where the
// repository has no index, before the provider (pi-claude-bridge) reads it.
test("codegraph's tool, connected after the run starts, still leaves each request in an unindexed repository", async () => {
  const CG = "mcp__codegraph__codegraph_explore";
  const dir = agentDir(), plain = project(), indexed = project();
  mkdirSync(join(indexed, ".codegraph"), { recursive: true });
  writeFileSync(join(indexed, ".codegraph", "codegraph.db"), "");
  for (const [cwd, kept] of [[plain, ["read", "mcp__engram__mem_save"]], [indexed, undefined]] as const) {
    let active = ["read", CG, "mcp__engram__mem_save"];
    const set: string[][] = [];
    const api = { getActiveTools: () => [...active], setActiveTools: (names: string[]) => { set.push(names); active = names; } };
    const context = loadedWith(dir, { PI_CODING_AGENT_DIR: dir }, api).handlers.get("context")!;
    assert.equal(await context({ messages: [] }, { cwd }), undefined, "the messages stay as they are");
    assert.deepEqual(set, kept ? [kept] : [], cwd);
    await context({ messages: [] }, { cwd });
    assert.equal(set.length, kept ? 1 : 0, "once the tool is gone, nothing is set again");
  }
});

// T115d (L708): the memory index is read once per session, so a memory saved
// mid-session never rewrites the system prompt; a new session reads it again.
test("the main session's prompt carries the project memory Claude Code keeps, read once per session", async () => {
  const dir = agentDir(), cwd = project();
  const memory = memoryDir(cwd, { CLAUDE_CONFIG_DIR: join(dir, "claude") });
  mkdirSync(memory, { recursive: true });
  writeFileSync(join(memory, "MEMORY.md"), "- [Plan](plan.md) — slice 5 next\n");
  const { handlers } = loaded(dir);
  const main = handlers.get("before_agent_start")!;
  const first = { appendSystemPrompt: "", sections: {} as Record<string, string> };
  await main({ systemPromptOptions: first }, { cwd });
  assert.ok(first.sections[SESSION_SECTION].includes(`\`${memory}/\``), first.sections[SESSION_SECTION]);
  assert.match(first.sections[SESSION_SECTION], /slice 5 next/);
  writeFileSync(join(memory, "MEMORY.md"), "- [Plan](plan.md) — slice 6 next\n");
  const next = { appendSystemPrompt: "", sections: {} as Record<string, string> };
  await main({ systemPromptOptions: next }, { cwd });
  assert.equal(next.sections[SESSION_SECTION], first.sections[SESSION_SECTION], "the session section is byte-identical within the session");
  await handlers.get("session_start")!({ reason: "new" }, { cwd });
  const fresh = { appendSystemPrompt: "", sections: {} as Record<string, string> };
  await main({ systemPromptOptions: fresh }, { cwd });
  assert.match(fresh.sections[SESSION_SECTION], /slice 6 next/, "a new session reads the index again");
});

// T115d (L707, L708): the cache holds only while the system prompt is
// byte-identical across turns. The route line follows the diff and the
// orchestrator file may be rewritten by `jhon sync`: neither rewrites the
// appended prompt mid-session; the route reaches the model as a persisted
// message, once and again only when it changes.
test("the appended prompt stays byte-identical across turns while the diff's route and the orchestrator file change", async () => {
  const dir = agentDir(), repo = project();
  const fake = join(dir, "jhon-fake"), count = join(dir, "count");
  writeFileSync(count, "0");
  writeFileSync(fake, `#!/bin/sh\nif [ "$2" = session-brief ]; then n=$(cat '${count}'); n=$((n+1)); echo $n > '${count}'; printf '{"lead":"Lead.","brief":"## This project\\\\u0027s rules\\\\n\\\\n- Delivery: one.\\\\n- Route, from the change\\\\u0027s diff now: risk %s.\\\\n- Reviews by risk."}' $n; exit 0; fi\nexec '${HOOK_BIN}' "$@"\n`);
  chmodSync(fake, 0o755);
  writeFileSync(join(dir, CONFIG_FILE), JSON.stringify(config({ jhon: fake, orchestrator: join(dir, "orchestrator.md") })));
  const { handlers } = loaded(dir);
  const main = handlers.get("before_agent_start")!;
  let id = "";
  const ctx = () => ({ cwd: repo, sessionManager: { getSessionId: () => id } });
  const turn = async () => {
    const options = { appendSystemPrompt: "", sections: {} as Record<string, string> };
    const result = await main({ systemPromptOptions: options }, ctx());
    return { options, message: result?.message };
  };
  const one = await turn();
  assert.match(one.options.appendSystemPrompt, /- Delivery: one\.\n- Reviews by risk\./, one.options.appendSystemPrompt);
  assert.doesNotMatch(one.options.appendSystemPrompt, /Route/);
  assert.deepEqual(one.message, { customType: "jhon-route", content: "- Route, from the change's diff now: risk 1.", display: false });
  // The session's id appears, so the brief is read again and its route moved; the orchestrator file is rewritten.
  id = "s1";
  writeFileSync(join(dir, "orchestrator.md"), "# Orchestrator\n\nRewritten by a sync.");
  const two = await turn();
  assert.equal(two.options.appendSystemPrompt, one.options.appendSystemPrompt, "byte-identical across turns");
  assert.deepEqual(two.options.sections, one.options.sections);
  assert.deepEqual(two.message, { customType: "jhon-route", content: "- Route, from the change's diff now: risk 2.", display: false });
  const three = await turn();
  assert.equal(three.options.appendSystemPrompt, one.options.appendSystemPrompt);
  assert.equal(three.message, undefined, "an unchanged route is not sent again");
});

// T115c (L707): the orchestrator keeps the tools it uses; the rest of Engram,
// the web tools and codegraph without an index stay out of every request,
// decided once, so the tool list is identical across requests.
const ENGRAM = ["mem_search", "mem_save", "mem_get_observation", "mem_context", "mem_judge", "mem_judge_many", "mem_session_summary", "mem_update", "mem_delete", "mem_timeline", "mem_stats"].map((t) => `mcp__engram__${t}`);
const KEPT = ["read", "bash", "subagent", ...ENGRAM.slice(0, 7)];
const ALL = [...KEPT, ...ENGRAM.slice(7), "web_search", "fetch_content", "get_search_content", "source_check", "mcp__codegraph__codegraph_explore"];

function toolsApi(start: string[]) {
  let active = [...start];
  const set: string[][] = [], entries: [string, unknown][] = [];
  const api = {
    getActiveTools: () => [...active],
    setActiveTools: (names: string[]) => { set.push(names); active = [...names]; },
    getAllTools: () => ALL.map((name) => ({ name, description: `${name} does one thing.\nMore detail.`, parameters: {} })),
    appendEntry: (type: string, data: unknown) => entries.push([type, data]),
  };
  return { api, set, entries, active: () => active };
}

function loadedTools(dir: string, api: Record<string, unknown>, env: Record<string, string> = {}) {
  const saved = { ...process.env };
  Object.assign(process.env, { PI_CODING_AGENT_DIR: dir, CLAUDE_CONFIG_DIR: join(dir, "claude"), ...env });
  const { on, handlers } = bus();
  const tools = new Map<string, any>();
  try {
    jhon({ on, registerTool: (t: any) => tools.set(t.name, t), registerCommand: () => {}, ...api });
  } finally {
    for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
    Object.assign(process.env, saved);
  }
  return { handlers, tools };
}

test("the orchestrator's tool list is decided once and stays identical across requests", async () => {
  const dir = agentDir(), cwd = project();
  const t = toolsApi(ALL);
  const { handlers } = loadedTools(dir, t.api);
  const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, selectedTools: [...ALL] };
  await handlers.get("before_agent_start")!({ systemPromptOptions: options }, { cwd });
  assert.deepEqual(options.selectedTools, KEPT, "the run starts with the kept tools only");
  const context = handlers.get("context")!;
  for (let i = 0; i < 3; i++) assert.equal(await context({ messages: [] }, { cwd }), undefined);
  assert.deepEqual(t.set, [KEPT], "set once, never toggled per request");
  assert.deepEqual(t.active(), KEPT);
  // A child keeps its --tools list: only codegraph without an index leaves it.
  const k = toolsApi(ALL);
  await loadedTools(dir, k.api, { JHON_PI_CHILD: "1" }).handlers.get("context")!({ messages: [] }, { cwd });
  assert.deepEqual(k.set, [ALL.filter((n) => n !== "mcp__codegraph__codegraph_explore")]);
});

// T118c: the native Prism review is the main session's; a child (a facet agent among them) has none.
test("jhon_review is registered in the main session only, with a small schema", async () => {
  const dir = agentDir();
  const review = loadedTools(dir, toolsApi(ALL).api).tools.get("jhon_review");
  assert.ok(review, "the main session has jhon_review");
  assert.deepEqual(Object.keys(review.parameters.properties), ["base", "change"]);
  assert.deepEqual(review.parameters.required ?? [], []);
  assert.match(review.description, /few minutes/);
  assert.ok(!loadedTools(dir, toolsApi(ALL).api, { JHON_PI_CHILD: "1" }).tools.has("jhon_review"), "a child has none");
});

test("tools_more lists the deferred tools and enables a named one for the rest of the session", async () => {
  const dir = agentDir(), cwd = project();
  const t = toolsApi(ALL);
  const { handlers, tools } = loadedTools(dir, t.api);
  const more = tools.get("tools_more");
  assert.ok(more, "the main session has the loader");
  assert.ok(!loadedTools(dir, toolsApi(ALL).api, { JHON_PI_CHILD: "1" }).tools.has("tools_more"), "a child has none");
  const context = handlers.get("context")!;
  await context({ messages: [] }, { cwd });
  const listed = (await more.execute("c1", {}, undefined, undefined, { cwd })).content[0].text;
  for (const name of [...ENGRAM.slice(7), "web_search", "fetch_content", "get_search_content", "source_check"]) assert.match(listed, new RegExp(`^- ${name}: ${name} does one thing\\.$`, "m"));
  assert.doesNotMatch(listed, /mem_search|codegraph|More detail/);
  const enabled = (await more.execute("c2", { names: ["mcp__engram__mem_update", "nope"] }, undefined, undefined, { cwd })).content[0].text;
  assert.match(enabled, /Enabled: mcp__engram__mem_update/);
  assert.match(enabled, /Unknown or not deferred: nope/);
  assert.deepEqual(t.active(), [...KEPT, "mcp__engram__mem_update"]);
  assert.deepEqual(t.entries, [["jhon-tools-more", { names: ["mcp__engram__mem_update"] }]]);
  const sets = t.set.length;
  for (let i = 0; i < 3; i++) await context({ messages: [] }, { cwd });
  assert.equal(t.set.length, sets, "the enabled tool stays; nothing toggles afterwards");
  // A resumed session keeps what it enabled.
  const r = toolsApi(ALL);
  const resumed = loadedTools(dir, r.api).handlers;
  await resumed.get("session_start")!({ reason: "resume" }, { cwd, sessionManager: { getBranch: () => [{ type: "custom", customType: "jhon-tools-more", data: { names: ["mcp__engram__mem_update"] } }] } });
  await resumed.get("context")!({ messages: [] }, { cwd });
  assert.deepEqual(r.active(), [...KEPT, "mcp__engram__mem_update"]);
});

// T133e (L1016): mcp.json mounts context7 `direct`, the only exposure a child's
// --tools can name, and pi declares a direct tool to the main session too. The
// tools are jhon-scout's and jhon-builder's: the orchestrator never declares them and
// tools_more neither lists nor enables them; a child keeps its --tools list.
test("context7's tools stay out of the main session and of tools_more; a jhon-scout or jhon-builder child keeps them", async () => {
  const dir = agentDir(), cwd = project();
  const C7 = ["mcp__context7__resolve_library_id", "mcp__context7__query_docs"];
  const mounted = [...ALL, ...C7];
  const all = () => mounted.map((name) => ({ name, description: `${name} does one thing.`, parameters: {} }));
  const t = toolsApi(mounted);
  const { handlers, tools } = loadedTools(dir, { ...t.api, getAllTools: all });
  const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, selectedTools: [...mounted] };
  await handlers.get("before_agent_start")!({ systemPromptOptions: options }, { cwd });
  assert.deepEqual(options.selectedTools, KEPT, "the run starts without context7's tools");
  await handlers.get("context")!({ messages: [] }, { cwd });
  assert.deepEqual(t.active(), KEPT, "the main session declares no context7 tool");
  const more = tools.get("tools_more");
  const listed = (await more.execute("c1", {}, undefined, undefined, { cwd })).content[0].text;
  assert.doesNotMatch(listed, /context7/, "tools_more does not list them");
  assert.match(listed, /^- web_search: /m, "the deferred tools are listed as before");
  const asked = (await more.execute("c2", { names: [C7[0]] }, undefined, undefined, { cwd })).content[0].text;
  assert.equal(asked, `Unknown or not deferred: ${C7[0]}.`);
  assert.deepEqual(t.active(), KEPT, "and does not enable one");
  assert.deepEqual(t.entries, []);
  // A session resumed from a branch that names one still declares none.
  const r = toolsApi(mounted);
  const resumed = loadedTools(dir, { ...r.api, getAllTools: all }).handlers;
  await resumed.get("session_start")!({ reason: "resume" }, { cwd, sessionManager: { getBranch: () => [{ type: "custom", customType: "jhon-tools-more", data: { names: [C7[1]] } }] } });
  await resumed.get("context")!({ messages: [] }, { cwd });
  assert.deepEqual(r.active(), KEPT);
  // A child's --tools list stands: only codegraph without an index leaves it.
  for (const agent of ["jhon-scout", "jhon-builder"]) {
    const k = toolsApi(mounted);
    await loadedTools(dir, { ...k.api, getAllTools: all }, { JHON_PI_CHILD: "1", JHON_PI_AGENT: agent }).handlers.get("context")!({ messages: [] }, { cwd });
    assert.deepEqual(k.active(), mounted.filter((n) => n !== "mcp__codegraph__codegraph_explore"), agent);
  }
});

// T45b: pi-claude-bridge, like pi's own builder, renders the appended prompt
// and then the custom sections. What changes per run or per owner message
// (the memory index and its path, which names the project's directory)
// rides in the session section, so the appended prompt, and the
// cached prefix up to it, stays byte-identical.
test("two runs in different projects and memories share the appended prompt; what differs comes after it", async () => {
  const dir = agentDir();
  const render = (o: { appendSystemPrompt: string; sections: Record<string, string> }) =>
    [o.appendSystemPrompt, ...Object.entries(o.sections).map(([k, v]) => `<${k}>\n${v}\n</${k}>`)].join("\n\n");
  const runs = [];
  for (const slice of ["slice 5", "slice 6"]) {
    const cwd = project();
    const memory = memoryDir(cwd, { CLAUDE_CONFIG_DIR: join(dir, "claude") });
    mkdirSync(memory, { recursive: true });
    writeFileSync(join(memory, "MEMORY.md"), `- [Plan](plan.md) — ${slice} next\n`);
    const { handlers } = loadedWith(dir, { PI_CODING_AGENT_DIR: dir, CLAUDE_CONFIG_DIR: join(dir, "claude") });
    const options = { appendSystemPrompt: "", sections: {} as Record<string, string> };
    await handlers.get("before_agent_start")!({ systemPromptOptions: options }, { cwd });
    runs.push({ options, memory, slice, text: render(options) });
  }
  assert.equal(runs[0].options.appendSystemPrompt, runs[1].options.appendSystemPrompt, "the static prefix is byte-identical");
  for (const run of runs) {
    const prefix = run.options.appendSystemPrompt;
    assert.ok(run.text.startsWith(prefix), "the static text comes first");
    for (const dynamic of [run.memory, run.slice]) {
      assert.ok(!prefix.includes(dynamic), `${dynamic} stays out of the static prefix`);
      assert.ok(run.text.indexOf(dynamic) > prefix.length, `${dynamic} comes after it`);
    }
  }
});

test("the main session records its mode, agent and why it started; a child records nothing", async () => {
  const dir = agentDir();
  const env = { PI_CODING_AGENT_DIR: dir, JHON_DESKTOP_COLORS: join(dir, "none.json") };
  const entries: [string, unknown][] = [];
  const appendEntry = (type: string, data: unknown) => entries.push([type, data]);
  const main = loadedWith(dir, env, { appendEntry }).handlers.get("session_start")!;
  await main({ reason: "resume" }, { cwd: dir, mode: "tui" });
  await main({}, { cwd: dir, mode: "tui" });
  // pi's reason (startup, reload, new, resume, fork) lets the audit tell a reload from a reopening.
  assert.deepEqual(entries, [[SESSION_ENTRY, { mode: "tui", agent: "jhon-orchestrator", reason: "resume" }], [SESSION_ENTRY, { mode: "tui", agent: "jhon-orchestrator", reason: "unknown" }]]);
  entries.length = 0;
  await loadedWith(dir, { ...env, JHON_PI_CHILD: "1" }, { appendEntry }).handlers.get("session_start")!({}, { cwd: dir, mode: "json" });
  assert.equal(entries.length, 0);
});

test("m365_read runs the claude JHON_CLAUDE_BIN names and returns its answer; an empty request is refused", async () => {
  const dir = agentDir();
  const bin = join(dir, "claude");
  writeFileSync(bin, "#!/bin/sh\necho 'CA-01 verbatim'\n");
  chmodSync(bin, 0o755);
  const saved = process.env.JHON_CLAUDE_BIN;
  process.env.JHON_CLAUDE_BIN = bin;
  try {
    const tool = loaded(dir).tools.find((t) => t.name === "m365_read");
    assert.equal((await tool.execute("1", { request: "HU-ABC-001" }, undefined, undefined, {})).content[0].text, "CA-01 verbatim");
    await assert.rejects(tool.execute("1", { request: " " }, undefined, undefined, {}), /needs a request/);
  } finally {
    if (saved === undefined) delete process.env.JHON_CLAUDE_BIN; else process.env.JHON_CLAUDE_BIN = saved;
  }
});

// T131 (L905, L908): a child has no turn cap. A JHON_PI_MAX_TURNS left in its
// environment (a parent from an older install) is not read.
test("a child past 80 turns keeps calling tools and is never shut down for its turns", async () => {
  const d = project();
  for (const env of [{ JHON_PI_CHILD: "1" }, { JHON_PI_CHILD: "1", JHON_PI_MAX_TURNS: "2" }, { JHON_PI_MAX_TURNS: "2" }]) {
    const { handlers } = loaded(agentDir(), env);
    const call = handlers.get("tool_call")!, turnEnd = handlers.get("turn_end")!;
    let shutdowns = 0;
    const ctx = { cwd: d, hasUI: false, shutdown: () => { shutdowns += 1; } };
    for (let turn = 1; turn <= 90; turn++) {
      await turnEnd({}, ctx);
      assert.equal(await call({ toolName: "read", input: { path: "a.txt" } }, ctx), undefined, `${JSON.stringify(env)} turn ${turn}`);
    }
    assert.equal(shutdowns, 0, JSON.stringify(env));
  }
});

// T131 (L920): no turn cap; T155 (L1394): no context cap either.
test("a child reads freely for 89 turns; from turn 90 at 160K tokens every read still runs and it is never shut down, a stale JHON_PI_MAX_TURNS or not", async () => {
  const d = project();
  for (const env of [{ JHON_PI_CHILD: "1" }, { JHON_PI_CHILD: "1", JHON_PI_MAX_TURNS: "5" }]) {
    const { handlers } = loaded(agentDir(), env);
    const call = handlers.get("tool_call")!, turnEnd = handlers.get("turn_end")!;
    let shutdowns = 0, tokens = 100_000;
    const ctx = { cwd: d, hasUI: false, shutdown: () => { shutdowns += 1; }, getContextUsage: () => ({ tokens, percent: tokens / 10_000, contextWindow: 1_000_000 }) };
    const read = () => call({ toolName: "read", input: { path: "a.txt" } }, ctx);
    for (let turn = 1; turn <= 89; turn++) {
      await turnEnd({}, ctx);
      assert.equal(await read(), undefined, `${JSON.stringify(env)} turn ${turn}`);
    }
    tokens = 160_000;
    for (let turn = 90; turn <= 100; turn++) {
      await turnEnd({}, ctx);
      assert.equal(await read(), undefined, `${JSON.stringify(env)} turn ${turn}`);
    }
    assert.equal(shutdowns, 0, JSON.stringify(env));
  }
});

test("tool_call blocks a refusal, puts an ask to the owner, and refuses what the owner declines", async () => {
  const d = project();
  const call = loaded(agentDir()).handlers.get("tool_call")!;
  const ui = (answer: string | undefined) => ({ cwd: d, hasUI: true, ui: { select: async () => answer } });
  assert.match((await call({ toolName: "bash", input: { command: "git push --force" } }, ui(ANSWER_ONCE))).reason, /safety-guard: refused/);
  // A deletion that loses work runs on the owner's yes.
  assert.equal(await call({ toolName: "bash", input: { command: "git reset --hard" } }, ui(ANSWER_ONCE)), undefined);
  assert.equal(await call({ toolName: "bash", input: { command: "git push origin main" } }, ui(ANSWER_ONCE)), undefined);
  assert.match((await call({ toolName: "bash", input: { command: "git push origin main" } }, ui(ANSWER_NO))).reason, /the owner did not authorize it/);
  assert.match((await call({ toolName: "bash", input: { command: "git push origin main" } }, ui(undefined))).reason, /the owner did not authorize it/, "a dismissed question is a no");
  assert.equal(await call({ toolName: "read", input: { path: "a.txt" } }, ui(ANSWER_ONCE)), undefined);
});

test("the owner's yes is always on record: once, in the session; for 30 minutes, a jhon grant in the grants' history", async () => {
  const d = project();
  const dir = agentDir();
  const bin = join(dir, "jhon-fake");
  const log = join(dir, "grant.log");
  writeFileSync(bin, `#!/bin/sh\nif [ "$1" = grant ]; then echo "$*" >> '${log}'; echo granted; exit 0; fi\nexec '${HOOK_BIN}' "$@"\n`);
  chmodSync(bin, 0o755);
  const entries: [string, any][] = [];
  const call = loadedWith(dir, { PI_CODING_AGENT_DIR: dir, JHON_BIN: bin }, { appendEntry: (type: string, data: unknown) => entries.push([type, data]) }).handlers.get("tool_call")!;
  const ui = (answer: string) => ({ cwd: d, hasUI: true, ui: { select: async (title: string, options: string[]) => { assert.match(title, /Jhon safety guard · push: git push/); assert.deepEqual(options, [ANSWER_ONCE, ANSWER_GRANT, ANSWER_NO]); return answer; } } });
  assert.equal(await call({ toolName: "bash", input: { command: "git push origin main" } }, ui(ANSWER_ONCE)), undefined);
  assert.equal(entries.at(-1)![0], AUTHORIZATION_ENTRY);
  assert.deepEqual([entries.at(-1)![1].classes, entries.at(-1)![1].scope, entries.at(-1)![1].tool], [["push"], "once", "bash"]);
  assert.equal(await call({ toolName: "bash", input: { command: "git push origin main" } }, ui(ANSWER_GRANT)), undefined);
  assert.equal(readFileSync(log, "utf8").trim(), "grant push --for 30m");
  assert.equal(entries.at(-1)![1].scope, "grant");
  assert.deepEqual(askClasses("push: git push; gh-write: gh pr create; push: git push --tags"), ["push", "gh-write"]);
});

test("in a folder that is no project, the owner's yes to a write lasts the session, and ends with it (T57a)", async () => {
  const folder = tempDir("jhon-pi-folder-");
  const dir = agentDir();
  const bin = join(dir, "jhon-fake");
  const log = join(dir, "grant.log");
  writeFileSync(bin, `#!/bin/sh\nif [ "$1" = grant ]; then echo "$*" >> '${log}'; echo granted; exit 0; fi\nexit 1\n`);
  chmodSync(bin, 0o755);
  const { handlers } = loadedWith(dir, { PI_CODING_AGENT_DIR: dir, JHON_BIN: bin });
  const call = handlers.get("tool_call")!;
  const seen: string[][] = [];
  const ctx = (answer: string, id = "sess-1") => ({ cwd: folder, hasUI: true, sessionManager: { getSessionId: () => id }, ui: { select: async (_title: string, options: string[]) => { seen.push(options); return answer; } } });
  assert.equal(await call({ toolName: "write", input: { path: "/etc/jhon.conf", content: "x" } }, ctx(ANSWER_SESSION)), undefined);
  assert.deepEqual(seen.at(-1), [ANSWER_ONCE, ANSWER_SESSION, ANSWER_NO]);
  assert.equal(readFileSync(log, "utf8").trim(), "grant outside-write --session sess-1");
  // Another class there takes no grant for a while: a folder has no project to hold it.
  await call({ toolName: "bash", input: { command: "ssh host" } }, ctx(ANSWER_NO));
  assert.deepEqual(seen.at(-1), [ANSWER_ONCE, ANSWER_NO]);
  // A session without an id is offered no session grant.
  await call({ toolName: "write", input: { path: "/etc/jhon.conf", content: "x" } }, ctx(ANSWER_NO, ""));
  assert.deepEqual(seen.at(-1), [ANSWER_ONCE, ANSWER_NO]);
  await handlers.get("session_shutdown")!({}, { cwd: folder });
  assert.deepEqual(readFileSync(log, "utf8").trim().split("\n"), ["grant outside-write --session sess-1", "grant --revoke outside-write --session sess-1"]);
});

test("pi offers no session grant where jhon grant would refuse it: HOME and the folders above it, and jhon's state (T57a)", async () => {
  const home = tempDir("jhon-pi-home-");
  const state = tempDir("jhon-pi-state-");
  mkdirSync(join(state, "jhon", "session-grants"), { recursive: true });
  const dir = agentDir();
  const bin = join(dir, "jhon-fake");
  writeFileSync(bin, "#!/bin/sh\nexit 1\n");
  chmodSync(bin, 0o755);
  const call = loadedWith(dir, { PI_CODING_AGENT_DIR: dir, JHON_BIN: bin }).handlers.get("tool_call")!;
  const saved = process.env.HOME, savedState = process.env.XDG_STATE_HOME;
  process.env.HOME = home;
  process.env.XDG_STATE_HOME = state;
  try {
    for (const cwd of [home, dirname(home), state, join(state, "jhon"), join(state, "jhon", "session-grants")]) {
      const seen: string[][] = [];
      const ctx = { cwd, hasUI: true, sessionManager: { getSessionId: () => "sess-1" }, ui: { select: async (_title: string, options: string[]) => { seen.push(options); return ANSWER_NO; } } };
      await call({ toolName: "write", input: { path: "/etc/jhon.conf", content: "x" } }, ctx);
      assert.deepEqual(seen.at(-1), [ANSWER_ONCE, ANSWER_NO], cwd);
    }
  } finally {
    process.env.HOME = saved;
    if (savedState === undefined) delete process.env.XDG_STATE_HOME; else process.env.XDG_STATE_HOME = savedState;
  }
});

test("while the owner is asked, jhon:owner-wait names the permission by its class only (L87, T15.3)", async () => {
  const d = project();
  const dir = agentDir();
  const emitted: [string, unknown][] = [];
  const events = { on: () => () => {}, emit: (name: string, data: unknown) => { emitted.push([name, data]); } };
  const call = loadedWith(dir, { PI_CODING_AGENT_DIR: dir }, { events }).handlers.get("tool_call")!;
  const ctx = { cwd: d, hasUI: true, ui: { select: async () => ANSWER_NO } };
  await call({ toolName: "bash", input: { command: "git push origin main" } }, ctx);
  assert.deepEqual(emitted.filter(([name]) => name === OWNER_WAIT_EVENT).map(([, data]) => data), [{ active: true, label: "permission: push" }, { active: false }]);
  assert.deepEqual(emitted.filter(([name]) => name === OWNER_WAIT_EVENT).length, 2);
  assert.equal(OWNER_WAIT_EVENT, "jhon:owner-wait");
  assert.deepEqual(emitted.filter(([name]) => name !== OWNER_WAIT_EVENT && name !== "jhon:host?"), [], "no other name, and no multiplexer's");
});

test("the main session asks for a multiplexer host at start (jhon:host?); a child does not", async () => {
  const dir = agentDir();
  for (const [env, expected] of [[{ PI_CODING_AGENT_DIR: dir }, 1], [{ PI_CODING_AGENT_DIR: dir, JHON_PI_CHILD: "1" }, 0]] as const) {
    const emitted: [string, unknown][] = [];
    const events = { on: () => () => {}, emit: (name: string, data: unknown) => { emitted.push([name, data]); } };
    const start = loadedWith(dir, env, { events }).handlers.get("session_start")!;
    await start({ reason: "startup" }, { cwd: dir, mode: "print", hasUI: false });
    assert.deepEqual(emitted.filter(([name]) => name === "jhon:host?"), expected ? [["jhon:host?", {}]] : []);
  }
});

test("without its configuration the extension refuses every guarded call and lets the rest through", async () => {
  const empty = tempDir("jhon-pi-empty-");
  const { handlers, tools } = loaded(empty);
  const call = handlers.get("tool_call")!;
  assert.match((await call({ toolName: "bash", input: { command: "ls" } }, { cwd: empty })).reason, /jhon\.json is missing or invalid/);
  assert.equal(await call({ toolName: "ls", input: { path: "a" } }, { cwd: empty }), undefined);
  await assert.rejects(tools.find((t) => t.name === "inspect").execute("1", { command: "git diff" }, undefined, undefined, { cwd: empty }), /jhon\.json/);
});

test("a malformed configuration is no configuration", () => {
  const dir = tempDir("jhon-pi-bad-");
  writeFileSync(join(dir, CONFIG_FILE), JSON.stringify({ jhon: "/j", orchestrator: "/o", vcsDirs: [".git"], protected: "not a list", inspectAllow: [] }));
  assert.equal(loadConfig(dir), null);
  mkdirSync(join(dir, "x"));
  writeFileSync(join(dir, "x", CONFIG_FILE), "{ not json");
  assert.equal(loadConfig(join(dir, "x")), null);
});

// ── the desktop dress and the jhon commands ────────────────────────────────
// The colors are the package's Kanagawa theme, never written here; where a
// desktop palette stands the main session draws the rashin poster as its header, and
// a child, being headless, does not. /jhon:status [<name>] opens the
// LDD detail (T15.3: the old /jhon status and /jhon risk merged into it; T15.10: no tabs);
// /jhon:profile <args> runs the jhon CLI.

function loadedWith(agentDir: string, env: Record<string, string>, api: Record<string, unknown> = {}) {
  const saved = { ...process.env };
  Object.assign(process.env, env);
  const { on, handlers } = bus();
  const commands = new Map<string, any>();
  try {
    jhon({ on, registerTool: () => {}, registerCommand: (n: string, c: any) => commands.set(n, c), ...api });
  } finally {
    for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
    Object.assign(process.env, saved);
  }
  return { handlers, commands };
}

/**
 * The desktop palette file a session's rose ink follows, stopped when t ends:
 * the follower polls it for the life of the process, and the agent folder's
 * removal would otherwise start one more `jhon palette --write-pi`, writing
 * themes/ into the folder again after it is gone.
 */
function followedColors(t: TestContext, dir: string): string {
  const colors = join(dir, "colors.json");
  t.after(() => unwatchFile(colors));
  return colors;
}

/** The theme file `jhon palette --write-pi` writes, once it stands (the write is asynchronous). */
async function roseInk(dir: string): Promise<any> {
  const file = join(dir, "themes", "rose-ink.json");
  for (let i = 0; i < 100 && !existsSync(file); i++) await new Promise((r) => setTimeout(r, 50));
  return existsSync(file) ? JSON.parse(readFileSync(file, "utf8")) : null;
}

/**
 * The owner's settings (L199) for this test: the shell's, and the engine's
 * that `jhon palette` reads (JHON_SETTINGS), both following the desktop palette.
 */
function ownerSettings(t: TestContext, palette = "desktop"): void {
  const file = join(tempDir("jhon-pi-settings-"), "settings.toml");
  writeFileSync(file, `schema = 1\n\n[integrations]\ndashboard = true\ntelemetry = true\n\n[integrations.theme]\npalette = "${palette}"\n`);
  const saved = process.env.JHON_SETTINGS;
  process.env.JHON_SETTINGS = file;
  t.after(() => { if (saved === undefined) delete process.env.JHON_SETTINGS; else process.env.JHON_SETTINGS = saved; });
  useMemorySettings(t, { ...OWNER_SETTINGS, "integrations.theme.palette": palette });
}

// T50f (F3): "ryoku", the desktop palette's older name, still follows it.
test("integrations.theme.palette = \"ryoku\", the older name of \"desktop\", still follows the desktop palette", async (t) => {
  ownerSettings(t, "ryoku");
  const dir = agentDir();
  const colors = followedColors(t, dir);
  writeFileSync(colors, JSON.stringify({ primary: "#aabbcc" }));
  const loaded = loadedWith(dir, { PI_CODING_AGENT_DIR: dir, JHON_DESKTOP_COLORS: colors, JHON_DESKTOP_NVIM_SPEC: join(dir, "absent.lua") });
  await loaded.handlers.get("session_start")!({}, { cwd: project(), mode: "tui", hasUI: true, ui: { setHeader: () => {} } });
  assert.equal((await roseInk(dir))?.vars?.primary, "#aabbcc", "rose ink follows the desktop palette");
});

test("with a desktop palette the main session draws the poster and writes rose ink from the palette; a child draws nothing (L50)", async (t) => {
  ownerSettings(t);
  const dir = agentDir();
  const colors = followedColors(t, dir);
  writeFileSync(colors, JSON.stringify({ primary: "#aabbcc", error: "#ff0000" }));
  const env = { PI_CODING_AGENT_DIR: dir, JHON_DESKTOP_COLORS: colors, JHON_DESKTOP_NVIM_SPEC: join(dir, "absent.lua") };
  let header: any = null;
  const ctx = { cwd: project(), mode: "tui", hasUI: true, ui: { setHeader: (f: any) => { header = f; } } };
  await loadedWith(dir, env).handlers.get("session_start")!({}, ctx);
  assert.equal((await roseInk(dir))?.vars?.primary, "#aabbcc", "rose ink follows the desktop palette");
  const lines: string[] = header(null, { fg: (_r: string, t: string) => t }).render(100);
  assert.match(lines[0], /^ ●  │/);
  assert.ok(lines.some((l) => /│ +JHON +│/.test(l)), "the title, with no wordmark to read");

  const kid = agentDir();
  let kidHeader = null;
  await loadedWith(kid, { ...env, PI_CODING_AGENT_DIR: kid, JHON_PI_CHILD: "1" }).handlers.get("session_start")!({}, { ...ctx, ui: { setHeader: (f: any) => { kidHeader = f; } } });
  assert.equal(kidHeader, null);
});

test("without a desktop palette pi keeps its own header and wears rose ink's fixed values", async (t) => {
  ownerSettings(t);
  const dir = agentDir();
  let header = null;
  await loadedWith(dir, { PI_CODING_AGENT_DIR: dir, JHON_DESKTOP_COLORS: join(dir, "none.json") })
    .handlers.get("session_start")!({}, { cwd: dir, mode: "tui", hasUI: true, ui: { setHeader: (f: any) => { header = f; } } });
  assert.equal((await roseInk(dir))?.vars?.primary, "#e7bbc8");
  assert.equal(header, null);
});

test("with integrations.theme.palette = \"rose-ink\", the default, nothing follows a desktop palette, even one that stands: no follower, no poster, no wordmark read (L197, T55)", async (t) => {
  useMemorySettings(t);
  const dir = agentDir();
  const colors = join(dir, "colors.json");
  writeFileSync(colors, JSON.stringify({ primary: "#aabbcc" }));
  const spec = join(dir, "spec.lua");
  writeFileSync(spec, "header = [[\n  RYOKU\n]]\n");
  let header = null;
  await loadedWith(dir, { PI_CODING_AGENT_DIR: dir, JHON_DESKTOP_COLORS: colors, JHON_DESKTOP_NVIM_SPEC: spec })
    .handlers.get("session_start")!({}, { cwd: dir, mode: "tui", hasUI: true, ui: { setHeader: (f: any) => { header = f; } } });
  await new Promise((r) => setTimeout(r, 300));
  assert.equal(existsSync(join(dir, "themes", "rose-ink.json")), false, "the follower did not start: the render's own theme stays");
  assert.equal(header, null, "pi keeps its own header");
});

test("/jhon:status opens the LDD detail, with the risk row and no toast, and /jhon:status <name> names one (L214: /jhon:ldd is /jhon:status); /jhon:profile <args> runs the CLI; /jhon:dashboard starts jhon dashboard when it does not answer (S30, T17b)", async () => {
  const dir = agentDir();
  const bin = join(dir, "jhon");
  writeFileSync(bin, `#!/bin/sh
case "$1" in
  status) if [ "$2" = "--json" ]; then c=c1; else c="$2"; fi
    echo "{\\"change\\":\\"$c\\",\\"directory\\":\\"openspec/changes/$c\\",\\"route\\":{\\"level\\":\\"standard\\",\\"tier\\":\\"medium\\",\\"reasons\\":[]}}" ;;
  risk) echo '{"tier":"medium","reasons":[{"level":"medium","file":"a.txt","why":"code"}]}' ;;
  dashboard) echo "$*" > "${join(dir, "dashboard.ran")}" ;;
  *) echo "ran: $*" ;;
esac
`);
  chmodSync(bin, 0o755);
  const { commands } = loadedWith(dir, { PI_CODING_AGENT_DIR: dir, JHON_BIN: bin });
  // L194: every Jhon command is its own /jhon:<option>; /jhon with a word is gone.
  assert.deepEqual([...commands.keys()].sort(), ["jhon-ldd", "jhon:agents", "jhon:dashboard", "jhon:grants", "jhon:jobs", "jhon:profile", "jhon:status"]);
  assert.equal(commands.has("jhon"), false, "no /jhon command");
  assert.equal(commands.has("plan"), false, "no /plan command: plan mode is gone (T50d)");
  const shown: string[] = [];
  const panels: any[] = [];
  const tui = { terminal: { rows: 40 }, requestRender() {} };
  const theme = { fg: (_r: string, t: string) => t, bold: (t: string) => t };
  const ctx = { cwd: dir, ui: { notify: (t: string) => shown.push(t), custom: async (f: any) => { panels.push(f(tui, theme, null, () => {})); return null; } } };
  const drawn = async (panel: any, pattern: RegExp) => {
    for (let i = 0; i < 100 && !pattern.test(panel.render(100).join("\n")); i++) await new Promise((r) => setTimeout(r, 30));
    return panel.render(100).join("\n");
  };

  assert.equal(commands.get("jhon:status").description, "The LDD status of the session's change, or the one named");
  assert.equal(commands.has("jhon:ldd"), false, "/jhon:ldd is /jhon:status (L214)");
  assert.equal(commands.has("jhon:mode"), false, "/jhon:mode is /jhon:grants (L214)");
  assert.equal(commands.get("jhon:grants").description, "The grants for this project: give, revoke, duration");
  await commands.get("jhon:status").handler("", ctx);
  const text = await drawn(panels[0], /standard route/);
  assert.match(text, /LDD · c1 +│\n│ +│\n│ standard route +│\n│ risk medium · a\.txt \(code\) +│/, "the change named once, and the risk jhon risk --json gives");
  assert.match(text, /│ {2}LDD · c1 +│/, "the LDD detail, the change's name in the title row (L174)");
  assert.match(text, /t tests {2}o tasks\.md {2}r refresh {2}esc close/, "with its actions");
  await commands.get("jhon:status").handler(" other ", ctx);
  assert.match(await drawn(panels[1], /│ {2}LDD · other +│/), /other/, "a named change is the one shown");
  assert.deepEqual(shown, [], "no toast");

  await commands.get("jhon:profile").handler("use mixed", ctx);
  assert.deepEqual(shown, ["ran: profile use mixed"]);
  await commands.get("jhon:profile").handler("", ctx);
  assert.equal(panels.length, 3, "/jhon:profile alone opens its view");
  assert.match(await drawn(panels[2], /profiles/), /←→ profile/, "/jhon:profile alone opens the profiles (T117d: its tabs view)");

  // A port nothing answers on, so the owner's own dashboard is never asked
  // and no browser opens: the command starts the server and says it did not answer.
  const closed = await new Promise<number>((resolve) => { const srv = createServer().listen(0, "127.0.0.1", () => { const { port } = srv.address() as { port: number }; srv.close(() => resolve(port)); }); });
  process.env.JHON_DASHBOARD_URL = `http://127.0.0.1:${closed}`;
  try { await commands.get("jhon:dashboard").handler("", ctx); } finally { delete process.env.JHON_DASHBOARD_URL; }
  const ran = join(dir, "dashboard.ran");
  for (let i = 0; i < 100 && !existsSync(ran); i++) await new Promise((r) => setTimeout(r, 30));
  assert.equal(readFileSync(ran, "utf8").trim(), `dashboard --port ${closed}`, "started on JHON_DASHBOARD_URL's port (T17d)");
  assert.equal(shown.at(-1), `jhon dashboard did not answer at http://127.0.0.1:${closed} after 2 s: run \`jhon dashboard\` in a terminal to see why`);
});

// T130 (L902, L909): /jhon:profile's model list is pi's registry (ctx.modelRegistry) united with the profile-named models.
test("/jhon:profile offers every model of pi's registry and every model a profile names, the count the list's length; a registry that cannot be read leaves the profile-named ones (T130)", async () => {
  const dir = agentDir();
  const bin = join(dir, "jhon");
  const rows = (o: string, w: string) => [{ agent: "jhon-orchestrator", runtime: "pi", model: o, source: "profile agent x" }, { agent: "worker", runtime: "pi", model: w, source: "profile agent x" }];
  const data = { active: "mine", file: "~/.config/jhon/profiles.json", efforts: { pi: ["low", "high"] }, profiles: [
    { name: "mine", provider: "nan", delegation: "inline", agents: rows("nan/alpha-flash@high", "nan/beta-flash@high") },
    { name: "default", provider: "claude-bridge", delegation: "inline", agents: rows("claude-bridge/claude-opus-5-5@medium", "claude-bridge/claude-opus-5-5@medium") },
  ] };
  writeFileSync(join(dir, "profiles.json"), JSON.stringify(data));
  writeFileSync(bin, `#!/bin/sh\nif [ "$1 $2" = "profile json" ]; then cat "${join(dir, "profiles.json")}"; else echo "ran: $*"; fi\n`);
  chmodSync(bin, 0o755);
  const { commands } = loadedWith(dir, { PI_CODING_AGENT_DIR: dir, JHON_BIN: bin });
  const tui = { terminal: { rows: 40 }, requestRender() {} };
  const theme = { fg: (_r: string, t: string) => t, bold: (t: string) => t };
  const opened = async (modelRegistry: unknown) => {
    let panel: any;
    await commands.get("jhon:profile").handler("", { cwd: dir, modelRegistry, ui: { notify() {}, custom: async (f: any) => { panel = f(tui, theme, null, () => {}); return null; } } });
    for (let i = 0; i < 100 && !/Orchestrator/.test(panel.render(100).join("\n")); i++) await new Promise((r) => setTimeout(r, 30));
    panel.handleInput("\r");
    const text: string = panel.render(100).join("\n");
    return { text, models: text.split("\n").map((l) => /^│ {5}(?:▸ | {2})(\S+\/\S+) *│$/.exec(l)?.[1]).filter(Boolean) };
  };
  const bridge = ["claude-fable-5-2", "claude-haiku-5-5", "claude-opus-5-5", "claude-sonnet-5-5"].map((id) => ({ provider: "claude-bridge", id }));
  const live = await opened({ getAvailable: () => [...bridge, { provider: "nan", id: "beta-flash" }, { provider: "nan", id: "alpha-flash" }] });
  assert.match(live.text, /model: type to filter▏ 6 available/, "4 + 2 from the registry; the profile-named ones are among them");
  assert.deepEqual(live.models, ["claude-bridge/claude-fable-5-2", "claude-bridge/claude-haiku-5-5", "claude-bridge/claude-opus-5-5", "claude-bridge/claude-sonnet-5-5", "nan/alpha-flash", "nan/beta-flash"]);
  // The owner's case (L902): a session whose pi loaded no bridge still offers the bridge model a profile names.
  const lean = await opened({ getAvailable: () => [{ provider: "nan", id: "alpha-flash" }, { provider: "nan", id: "beta-flash" }, { provider: "nan", id: "gamma" }] });
  assert.deepEqual(lean.models, ["claude-bridge/claude-opus-5-5", "nan/alpha-flash", "nan/beta-flash", "nan/gamma"]);
  for (const registry of [undefined, { getAvailable: () => { throw new Error("registry down"); } }]) {
    const none = await opened(registry);
    assert.match(none.text, /model: type to filter▏ 3 available/);
    assert.deepEqual(none.models, ["claude-bridge/claude-opus-5-5", "nan/alpha-flash", "nan/beta-flash"]);
    assert.doesNotMatch(none.text, /registry down|Could not/, "no error on screen");
  }
});

// T130 (L920 B1): a session never calls a model nobody chose.
test("a session pi started on a model nobody chose makes no call: without a UI it ends with exit 1 and the line; with one it shows the line and refuses prompts until the owner picks a model; chosen models are untouched (B1)", async () => {
  const dir = agentDir();
  writeFileSync(join(dir, "settings.json"), JSON.stringify({ defaultProvider: "anthropic", defaultModel: "claude-opus-4-5" }));
  const line = "jhon: the profile names anthropic/claude-opus-4-5 for this session, but its provider anthropic has no credential; this session is on claude-bridge/claude-fable-5-2, which nobody chose in this run.";
  ModelChoice.forget();
  const registry = (auth: string[]) => ({ find: (provider: string, id: string) => ({ provider, id }), hasConfiguredAuth: (m: any) => auth.includes(m.provider), getAvailable: () => [] });
  const session = (model: { provider: string; id: string }, auth: string[], hasUI: boolean) => {
    const shown: Array<[string, string]> = [];
    const ctx = { cwd: dir, mode: hasUI ? "tui" : "print", hasUI, model, modelRegistry: registry(auth), scopedModels: [], sessionManager: { getBranch: () => [{ type: "model_change", provider: model.provider, modelId: model.id }] }, ui: { notify: (t: string, level: string) => shown.push([level, t]) } };
    return { ctx, shown, h: loadedWith(dir, { PI_CODING_AGENT_DIR: dir }).handlers };
  };
  const fallback = { provider: "claude-bridge", id: "claude-fable-5-2" };
  const argv = process.argv;
  const exit = process.exit, write = process.stderr.write;
  process.argv = ["pi", "pi", "-p", "hi"];
  try {
    // Print mode: the line on stderr and exit 1, at session start, before any prompt.
    const printed: string[] = [], exits: unknown[] = [];
    (process as any).exit = (code: unknown) => { exits.push(code); };
    (process.stderr as any).write = (t: string) => { printed.push(String(t)); return true; };
    const print = session(fallback, ["claude-bridge"], false);
    try { await print.h.get("session_start")!({ reason: "startup" }, print.ctx); } finally { process.exit = exit; process.stderr.write = write; }
    assert.deepEqual(exits, [1]);
    assert.deepEqual(printed, [`${line} No model was called: pass --model <provider/id> to choose one.\n`]);
    assert.deepEqual(await print.h.get("input")!({ source: "interactive", text: "hi" }, print.ctx), { action: "handled" }, "and a prompt would not run either");

    // The TUI: the line as an error, each prompt refused, until the owner picks a model.
    const tui = session(fallback, ["claude-bridge"], true);
    await tui.h.get("session_start")!({ reason: "startup" }, tui.ctx);
    const said = `${line} Your next prompt asks whether to run on it; you can also pick another model, or restart with the credential set.`;
    assert.deepEqual(tui.shown, [["error", said]]);
    assert.deepEqual(await tui.h.get("input")!({ source: "interactive", text: "hi" }, tui.ctx), { action: "handled" });
    assert.deepEqual(tui.shown, [["error", said], ["error", said]], "said again at each refused prompt");
    await tui.h.get("model_select")!({ source: "set", model: { provider: "claude-bridge", id: "claude-opus-5-5" }, previousModel: fallback }, tui.ctx);
    assert.equal(await tui.h.get("input")!({ source: "interactive", text: "hi" }, tui.ctx), undefined, "the owner's pick runs");
    assert.equal(tui.shown.length, 2);

    // Untouched: the profile's own model, and an explicit --model.
    const named = session({ provider: "anthropic", id: "claude-opus-4-5" }, ["anthropic"], true);
    await named.h.get("session_start")!({ reason: "startup" }, named.ctx);
    assert.equal(await named.h.get("input")!({ source: "interactive", text: "hi" }, named.ctx), undefined);
    assert.deepEqual(named.shown, []);
    process.argv = ["pi", "pi", "--model", "claude-bridge/claude-fable-5-2"];
    const explicit = session(fallback, ["claude-bridge"], true);
    await explicit.h.get("session_start")!({ reason: "startup" }, explicit.ctx);
    assert.equal(await explicit.h.get("input")!({ source: "interactive", text: "hi" }, explicit.ctx), undefined);
    assert.deepEqual(explicit.shown, []);
  } finally {
    process.argv = argv; process.exit = exit; process.stderr.write = write;
  }
});

// T130 (L926): the re-verification's breaks and gaps.
test("a project's own runnable default in use is a chosen model: nothing printed, no exit, though the profile's default has no credential (BREAK 3)", async () => {
  const dir = agentDir(), cwd = tempDir("jhon-pi-project-");
  ModelChoice.forget();
  writeFileSync(join(dir, "settings.json"), JSON.stringify({ defaultProvider: "anthropic", defaultModel: "claude-opus-4-5" }));
  mkdirSync(join(cwd, ".pi"));
  writeFileSync(join(cwd, ".pi", "settings.json"), JSON.stringify({ defaultProvider: "claude-bridge", defaultModel: "claude-opus-5-5" }));
  const model = { provider: "claude-bridge", id: "claude-opus-5-5" };
  const ctx = { cwd, mode: "print", hasUI: false, model, scopedModels: [], sessionManager: { getBranch: () => [{ type: "model_change", provider: model.provider, modelId: model.id }] },
    modelRegistry: { find: (provider: string, id: string) => ({ provider, id }), hasConfiguredAuth: (m: any) => m.provider === "claude-bridge", getAvailable: () => [] } };
  const h = loadedWith(dir, { PI_CODING_AGENT_DIR: dir }).handlers;
  const argv = process.argv, exit = process.exit, write = process.stderr.write;
  const printed: string[] = [], exits: unknown[] = [];
  process.argv = ["pi", "pi", "--approve", "-p", "hi"];
  (process as any).exit = (code: unknown) => { exits.push(code); };
  (process.stderr as any).write = (t: string) => { printed.push(String(t)); return true; };
  try { await h.get("session_start")!({ reason: "startup" }, ctx); } finally { process.argv = argv; process.exit = exit; process.stderr.write = write; }
  assert.deepEqual([exits, printed], [[], []]);
  assert.equal(await h.get("input")!({ source: "interactive", text: "hi" }, ctx), undefined, "and its prompt runs");
});

test("a held session makes no model call by any road: a run another extension starts is aborted before its first request, a compaction and a tree summary are cancelled; a pick lifts all three (BREAK 2, GAP 2)", async () => {
  const dir = agentDir();
  writeFileSync(join(dir, "settings.json"), JSON.stringify({ defaultProvider: "anthropic", defaultModel: "claude-opus-4-5" }));
  const fallback = { provider: "claude-bridge", id: "claude-fable-5-2" };
  const shown: string[] = [];
  ModelChoice.forget();
  let aborts = 0;
  const ctx = { cwd: dir, mode: "tui", hasUI: true, model: fallback, scopedModels: [], abort: () => { aborts += 1; }, sessionManager: { getBranch: () => [{ type: "model_change", provider: fallback.provider, modelId: fallback.id }], getSessionId: () => "s1" },
    modelRegistry: { find: (provider: string, id: string) => ({ provider, id }), hasConfiguredAuth: (m: any) => m.provider === "claude-bridge", getAvailable: () => [] }, ui: { notify: (t: string) => shown.push(t) } };
  const argv = process.argv;
  process.argv = ["pi", "pi"];
  try {
    const h = loadedWith(dir, { PI_CODING_AGENT_DIR: dir }).handlers;
    await h.get("session_start")!({ reason: "startup" }, ctx);
    assert.equal(shown.length, 1, "held");
    const run = { systemPromptOptions: { selectedTools: [], skills: [], sections: {} } };
    await h.get("before_agent_start")!(run, ctx);
    assert.equal(aborts, 1, "the run is aborted while it starts: pi stops it at agent_start, before the first request");
    assert.deepEqual(await h.get("session_before_compact")!({ reason: "manual" }, ctx), { cancel: true });
    assert.deepEqual(await h.get("session_before_tree")!({ preparation: { userWantsSummary: true } }, ctx), { cancel: true });
    assert.equal(await h.get("session_before_tree")!({ preparation: { userWantsSummary: false } }, ctx), undefined, "moving in the tree without a summary calls no model");
    assert.equal(shown.length, 4, "each one said, with the line");
    assert.ok(shown.every((t) => t === shown[0]));
    await h.get("model_select")!({ source: "set", model: { provider: "claude-bridge", id: "claude-opus-5-5" }, previousModel: fallback }, ctx);
    await h.get("before_agent_start")!(run, ctx);
    assert.equal(aborts, 1, "the owner's pick runs");
    assert.equal(await h.get("session_before_compact")!({ reason: "manual" }, ctx), undefined);
    assert.equal(await h.get("session_before_tree")!({ preparation: { userWantsSummary: true } }, ctx), undefined);
    assert.equal(shown.length, 4);
  } finally { process.argv = argv; }
});

test("the owner's pick of this process is put back on a new session pi started on its fallback, with no hold; a pick pi cannot set is held as before (GAP 3)", async () => {
  const dir = agentDir();
  writeFileSync(join(dir, "settings.json"), JSON.stringify({ defaultProvider: "anthropic", defaultModel: "claude-opus-4-5" }));
  const fallback = { provider: "claude-bridge", id: "claude-fable-5-2" }, pick = { provider: "claude-bridge", id: "claude-opus-5-5" };
  ModelChoice.forget();
  const session = (canSet: boolean) => {
    const shown: string[] = [], set: unknown[] = [];
    let h: any;
    const ctx: any = { cwd: dir, mode: "tui", hasUI: true, model: fallback, scopedModels: [], sessionManager: { getBranch: () => [{ type: "model_change", provider: fallback.provider, modelId: fallback.id }] },
      modelRegistry: { find: (provider: string, id: string) => ({ provider, id, found: true }), hasConfiguredAuth: (m: any) => m.provider === "claude-bridge", getAvailable: () => [] }, ui: { notify: (t: string) => shown.push(t) } };
    // pi's setModel, as the extension API gives it: the model set, and model_select fired with source set.
    const setModel = async (model: any) => { set.push(model); if (!canSet) return false; ctx.model = model; await h.get("model_select")!({ source: "set", model, previousModel: fallback }, ctx); return true; };
    h = loadedWith(dir, { PI_CODING_AGENT_DIR: dir }, { setModel }).handlers;
    return { ctx, shown, set, h };
  };
  const argv = process.argv;
  process.argv = ["pi", "pi"];
  try {
    const first = session(true);
    await first.h.get("session_start")!({ reason: "startup" }, first.ctx);
    assert.equal(first.shown.length, 1, "held: nothing was picked yet");
    assert.deepEqual(first.set, [], "and nothing is set on the owner's behalf");
    await first.h.get("model_select")!({ source: "set", model: pick, previousModel: fallback }, first.ctx);
    // /new: pi starts the new session on its fallback again.
    const next = session(true);
    await next.h.get("session_start")!({ reason: "new" }, next.ctx);
    assert.deepEqual(next.set, [{ ...pick, found: true }], "the pick, as the registry has it");
    assert.deepEqual(next.shown, [], "no hold, nothing to pick again");
    assert.equal(await next.h.get("input")!({ source: "interactive", text: "hi" }, next.ctx), undefined);
    const refusedSet = session(false);
    await refusedSet.h.get("session_start")!({ reason: "new" }, refusedSet.ctx);
    assert.equal(refusedSet.shown.length, 1, "pi would not set it (its credential is gone): held");
    assert.deepEqual(await refusedSet.h.get("input")!({ source: "interactive", text: "hi" }, refusedSet.ctx), { action: "handled" });
  } finally { process.argv = argv; }
});

// T130 (L930): the third verification's breaks and test gaps.
const HELD_LINE = "jhon: the profile names anthropic/claude-opus-4-5 for this session, but its provider anthropic has no credential; this session is on claude-bridge/claude-fable-5-2, which nobody chose in this run.";
/** A session for the model-choice wiring: the agent folder's default anthropic/claude-opus-4-5, and what pi shows and sets. */
function choiceSession(dir: string, over: Record<string, unknown> = {}, api: Record<string, unknown> = {}) {
  const shown: string[] = [], asked: Array<{ question: string; options: string[] }> = [];
  const answers: Array<string | undefined> = [];
  const fallback = { provider: "claude-bridge", id: "claude-fable-5-2" };
  const ctx: any = { cwd: dir, mode: "tui", hasUI: true, model: fallback, scopedModels: [], abort() {},
    sessionManager: { getBranch: () => [{ type: "model_change", provider: ctx.model.provider, modelId: ctx.model.id }], getSessionId: () => "s1" },
    modelRegistry: { find: (provider: string, id: string) => ({ provider, id }), hasConfiguredAuth: (m: any) => m.provider === "claude-bridge", getAvailable: () => [] },
    ui: { notify: (t: string) => shown.push(t), select: async (question: string, options: string[]) => { asked.push({ question, options }); return answers.shift(); } }, ...over };
  return { ctx, shown, asked, answers, fallback, h: loadedWith(dir, { PI_CODING_AGENT_DIR: dir }, api).handlers };
}
const withArgv = async (argv: string[], run: () => Promise<void>) => { const saved = process.argv; process.argv = ["pi", "pi", ...argv]; try { await run(); } finally { process.argv = saved; } };
const profileOn = (dir: string, provider: string, id: string) => writeFileSync(join(dir, "settings.json"), JSON.stringify({ defaultProvider: provider, defaultModel: id }));

test("a held prompt asks the owner whether to run on pi's fallback: yes makes it their choice and the prompt goes through, later prompts ask nothing; no refuses and asks again; another extension's input asks nothing (N2)", async () => {
  const dir = agentDir();
  profileOn(dir, "anthropic", "claude-opus-4-5");
  ModelChoice.forget();
  await withArgv([], async () => {
    const s = choiceSession(dir);
    await s.h.get("session_start")!({ reason: "startup" }, s.ctx);
    assert.equal(s.shown.length, 1, "held, the line said");
    const prompt = () => s.h.get("input")!({ source: "interactive", text: "hi" }, s.ctx);
    const question = { question: `${HELD_LINE} Run this session on claude-bridge/claude-fable-5-2?`, options: ["Yes: it is my choice for this session", "No: keep it held"] };
    // Another extension's input: refused with the line, no question.
    assert.deepEqual(await s.h.get("input")!({ source: "extension", text: "go" }, s.ctx), { action: "handled" });
    assert.deepEqual([s.asked.length, s.shown.length], [0, 2]);
    // No, and a dismissed dialog: refused, asked again at the next prompt; the dialog showed the line, it is not said again (L942).
    s.answers.push("No: keep it held", undefined);
    assert.deepEqual(await prompt(), { action: "handled" });
    assert.deepEqual(await prompt(), { action: "handled" });
    assert.deepEqual(s.asked, [question, question], "once per refused prompt: the profile's model, the missing credential and the fallback");
    assert.equal(s.shown.length, 2);
    // Yes: the prompt that was asked about goes through, and nothing is asked or said afterwards.
    s.answers.push("Yes: it is my choice for this session");
    assert.equal(await prompt(), undefined);
    assert.equal(await prompt(), undefined);
    assert.deepEqual([s.asked.length, s.shown.length], [3, 2]);
    assert.deepEqual(ModelChoice.pick(), s.fallback, "remembered for this process, as a pick is");
    // The same-model pick needed no model_select; and a /new on the fallback is not held again.
    const next = choiceSession(dir);
    await next.h.get("session_start")!({ reason: "new" }, next.ctx);
    assert.equal(await next.h.get("input")!({ source: "interactive", text: "hi" }, next.ctx), undefined);
    assert.deepEqual([next.asked.length, next.shown.length], [0, 0]);
  });
  // A pick of another model with pi's picker still releases, with no question.
  ModelChoice.forget();
  await withArgv([], async () => {
    const s = choiceSession(dir);
    await s.h.get("session_start")!({ reason: "startup" }, s.ctx);
    await s.h.get("model_select")!({ source: "set", model: { provider: "claude-bridge", id: "claude-opus-5-5" }, previousModel: s.fallback }, s.ctx);
    assert.equal(await s.h.get("input")!({ source: "interactive", text: "hi" }, s.ctx), undefined);
    assert.equal(s.asked.length, 0);
  });
  ModelChoice.forget();
});

test("the project's default without a credential, the profile's runnable, a third model in use: print mode exits 1 with the line naming the project's file (GAP 1, through the wiring)", async () => {
  const dir = agentDir(), cwd = tempDir("jhon-pi-project-");
  profileOn(dir, "claude-bridge", "claude-opus-5-5");
  mkdirSync(join(cwd, ".pi"));
  writeFileSync(join(cwd, ".pi", "settings.json"), JSON.stringify({ defaultProvider: "anthropic", defaultModel: "claude-opus-4-5" }));
  ModelChoice.forget();
  const s = choiceSession(dir, { cwd, mode: "print", hasUI: false });
  const exit = process.exit, write = process.stderr.write;
  const printed: string[] = [], exits: unknown[] = [];
  (process as any).exit = (code: unknown) => { exits.push(code); };
  (process.stderr as any).write = (t: string) => { printed.push(String(t)); return true; };
  try { await withArgv(["--approve", "-p", "hi"], async () => { await s.h.get("session_start")!({ reason: "startup" }, s.ctx); }); } finally { process.exit = exit; process.stderr.write = write; }
  assert.deepEqual(exits, [1]);
  assert.deepEqual(printed, ["jhon: the project's .pi/settings.json names anthropic/claude-opus-4-5 for this session, but its provider anthropic has no credential; this session is on claude-bridge/claude-fable-5-2, which nobody chose in this run. No model was called: pass --model <provider/id> to choose one.\n"]);
});

test("a project's runnable default in use holds nothing in the TUI either, with no pick in the process: nothing shown, the prompt passes (BREAK 3)", async () => {
  const dir = agentDir(), cwd = tempDir("jhon-pi-project-");
  profileOn(dir, "anthropic", "claude-opus-4-5");
  mkdirSync(join(cwd, ".pi"));
  writeFileSync(join(cwd, ".pi", "settings.json"), JSON.stringify({ defaultProvider: "claude-bridge", defaultModel: "claude-opus-5-5" }));
  ModelChoice.forget();
  await withArgv(["--approve"], async () => {
    const s = choiceSession(dir, { cwd, model: { provider: "claude-bridge", id: "claude-opus-5-5" } });
    await s.h.get("session_start")!({ reason: "startup" }, s.ctx);
    assert.equal(await s.h.get("input")!({ source: "interactive", text: "hi" }, s.ctx), undefined);
    assert.deepEqual([s.shown, s.asked], [[], []]);
    assert.equal(ModelChoice.pick(), undefined, "and no pick was needed");
  });
});

test("the pick is put back only on a session that would be held, and with the model alone: nothing is saved to the settings (GAP 3)", async () => {
  const dir = agentDir();
  profileOn(dir, "anthropic", "claude-opus-4-5");
  ModelChoice.forget();
  const calls: unknown[][] = [];
  const setModel = async (...args: unknown[]) => { calls.push(args); return true; };
  await withArgv([], async () => {
    const first = choiceSession(dir, {}, { setModel });
    await first.h.get("model_select")!({ source: "set", model: { provider: "claude-bridge", id: "claude-opus-5-5" }, previousModel: first.fallback }, first.ctx);
    // A new session on the profile's own model (its credential is back): nothing to put back.
    const onProfile = choiceSession(dir, { model: { provider: "anthropic", id: "claude-opus-4-5" }, modelRegistry: { find: (provider: string, id: string) => ({ provider, id }), hasConfiguredAuth: () => true, getAvailable: () => [] } }, { setModel });
    await onProfile.h.get("session_start")!({ reason: "new" }, onProfile.ctx);
    assert.deepEqual(calls, [], "a session on a chosen model gets no setModel");
    // A new session pi started on its fallback: the pick, and only the model.
    const onFallback = choiceSession(dir, {}, { setModel });
    await onFallback.h.get("session_start")!({ reason: "new" }, onFallback.ctx);
    assert.deepEqual(calls, [[{ provider: "claude-bridge", id: "claude-opus-5-5" }]], "one argument: the model, no persist option");
    assert.deepEqual(onFallback.shown, []);
  });
  ModelChoice.forget();
});

test("the line is said once while it stays the same across session starts; a pick or a start that holds nothing lets it be said again", async () => {
  const dir = agentDir();
  profileOn(dir, "anthropic", "claude-opus-4-5");
  ModelChoice.forget();
  await withArgv([], async () => {
    const resumed = choiceSession(dir);
    await resumed.h.get("session_start")!({ reason: "startup" }, resumed.ctx);
    const fresh = choiceSession(dir);
    await fresh.h.get("session_start")!({ reason: "new" }, fresh.ctx);
    assert.deepEqual([resumed.shown.length, fresh.shown.length], [1, 0], "/new on a held session: held for the same reason, said once");
    assert.deepEqual(await fresh.h.get("input")!({ source: "extension", text: "go" }, fresh.ctx), { action: "handled" }, "and still held");
    const chosen = choiceSession(dir, { model: { provider: "anthropic", id: "claude-opus-4-5" } });
    await chosen.h.get("session_start")!({ reason: "new" }, chosen.ctx);
    const again = choiceSession(dir);
    await again.h.get("session_start")!({ reason: "new" }, again.ctx);
    assert.equal(again.shown.length, 1, "after a session that held nothing, it is news again");
  });
  ModelChoice.forget();
});

// T130 (L942, L944): the rule reduced; no history is read.
test("a resumed session on a model of an earlier process, a named default broken: with a UI it is held and the owner's prompt asks; without one it exits 1 and the line says to pass --model (L944)", async () => {
  const dir = agentDir();
  profileOn(dir, "anthropic", "claude-opus-4-5");
  const earlier = { provider: "claude-bridge", id: "claude-opus-5-5" };
  // The session as pi resumed it: its own model, its own answers. None of it is read.
  const history = () => { throw new Error("the session's history was read"); };
  const line = HELD_LINE.replace("claude-bridge/claude-fable-5-2", "claude-bridge/claude-opus-5-5");
  ModelChoice.forget();
  await withArgv(["--continue"], async () => {
    const s = choiceSession(dir, { model: earlier });
    s.ctx.sessionManager.getBranch = history;
    await s.h.get("session_start")!({ reason: "startup" }, s.ctx);
    assert.deepEqual(s.shown, [`${line} Your next prompt asks whether to run on it; you can also pick another model, or restart with the credential set.`]);
    s.answers.push("Yes: it is my choice for this session");
    assert.equal(await s.h.get("input")!({ source: "interactive", text: "hi" }, s.ctx), undefined, "asked once, yes runs");
    assert.deepEqual(s.asked.map((a) => a.question), [`${line} Run this session on claude-bridge/claude-opus-5-5?`]);
    assert.equal(await s.h.get("input")!({ source: "interactive", text: "again" }, s.ctx), undefined);
    assert.equal(s.asked.length, 1, "once per process");
  });
  ModelChoice.forget();
  const exit = process.exit, write = process.stderr.write;
  const printed: string[] = [], exits: unknown[] = [];
  (process as any).exit = (code: unknown) => { exits.push(code); };
  (process.stderr as any).write = (t: string) => { printed.push(String(t)); return true; };
  try {
    await withArgv(["--continue", "-p", "hi"], async () => {
      const s = choiceSession(dir, { model: earlier, mode: "print", hasUI: false });
      s.ctx.sessionManager.getBranch = history;
      await s.h.get("session_start")!({ reason: "startup" }, s.ctx);
    });
  } finally { process.exit = exit; process.stderr.write = write; }
  assert.deepEqual(exits, [1]);
  assert.deepEqual(printed, [`${line} No model was called: pass --model <provider/id> to choose one.\n`]);
  // With --model the same session is the owner's: nothing printed, no exit.
  await withArgv(["--continue", "--model", "claude-bridge/claude-opus-5-5", "-p", "hi"], async () => {
    const s = choiceSession(dir, { model: earlier, mode: "print", hasUI: false });
    await s.h.get("session_start")!({ reason: "startup" }, s.ctx);
    assert.equal(await s.h.get("input")!({ source: "interactive", text: "hi" }, s.ctx), undefined);
  });
  assert.deepEqual([exits, printed.length], [[1], 1]);
});

test("one question at a time: a prompt that arrives while one is open is refused with the line and asks nothing; each answer is its own prompt's; a pick made meanwhile does not turn a no into a send (G1)", async () => {
  const dir = agentDir();
  profileOn(dir, "anthropic", "claude-opus-4-5");
  ModelChoice.forget();
  await withArgv([], async () => {
    // The verifier's shape: two prompts, the client slow to answer.
    const pending: Array<(answer: string | undefined) => void> = [];
    const s = choiceSession(dir);
    s.ctx.ui.select = (_question: string, _options: string[]) => new Promise<string | undefined>((resolve) => { pending.push(resolve); });
    await s.h.get("session_start")!({ reason: "startup" }, s.ctx);
    const first = s.h.get("input")!({ source: "rpc", text: "one" }, s.ctx);
    await new Promise((r) => setImmediate(r));
    assert.equal(pending.length, 1);
    assert.deepEqual(await s.h.get("input")!({ source: "rpc", text: "two" }, s.ctx), { action: "handled" }, "the second is refused while the first is asked about");
    assert.deepEqual([pending.length, s.shown.length], [1, 2], "no second question; the line said");
    pending[0]!("Yes: it is my choice for this session");
    assert.equal(await first, undefined, "yes passes the prompt it was asked about, and only that one");
    assert.equal(await s.h.get("input")!({ source: "rpc", text: "three" }, s.ctx), undefined, "chosen now: later prompts run");
    assert.equal(pending.length, 1);
  });
  ModelChoice.forget();
  await withArgv([], async () => {
    // No to the first, then a second: asked again, and its no is its own.
    const pending: Array<(answer: string | undefined) => void> = [];
    const s = choiceSession(dir);
    s.ctx.ui.select = () => new Promise<string | undefined>((resolve) => { pending.push(resolve); });
    await s.h.get("session_start")!({ reason: "startup" }, s.ctx);
    const first = s.h.get("input")!({ source: "interactive", text: "one" }, s.ctx);
    await new Promise((r) => setImmediate(r));
    // The owner picks another model with pi's picker while the question is open, then answers no.
    await s.h.get("model_select")!({ source: "set", model: { provider: "claude-bridge", id: "claude-opus-5-5" }, previousModel: s.fallback }, s.ctx);
    pending[0]!("No: keep it held");
    assert.deepEqual(await first, { action: "handled" }, "no refuses its prompt, though the hold was lifted meanwhile");
    assert.equal(await s.h.get("input")!({ source: "interactive", text: "two" }, s.ctx), undefined, "the pick stands for the next prompt");
    assert.deepEqual([pending.length, s.shown.length], [1, 1], "nothing more asked; the line not said again after the no");
    assert.deepEqual(ModelChoice.pick(), { provider: "claude-bridge", id: "claude-opus-5-5" }, "the pick, not the fallback");
  });
  ModelChoice.forget();
  await withArgv([], async () => {
    // Yes after a pick made meanwhile: the prompt runs, and the pick is not replaced by the fallback.
    const pending: Array<(answer: string | undefined) => void> = [];
    const s = choiceSession(dir);
    s.ctx.ui.select = () => new Promise<string | undefined>((resolve) => { pending.push(resolve); });
    await s.h.get("session_start")!({ reason: "startup" }, s.ctx);
    const first = s.h.get("input")!({ source: "interactive", text: "one" }, s.ctx);
    await new Promise((r) => setImmediate(r));
    await s.h.get("model_select")!({ source: "set", model: { provider: "claude-bridge", id: "claude-opus-5-5" }, previousModel: s.fallback }, s.ctx);
    pending[0]!("Yes: it is my choice for this session");
    assert.equal(await first, undefined);
    assert.deepEqual(ModelChoice.pick(), { provider: "claude-bridge", id: "claude-opus-5-5" });
    // A dialog that throws is a dismissed one.
    ModelChoice.forget();
    const broken = choiceSession(dir);
    broken.ctx.ui.select = async () => { throw new Error("dialog closed"); };
    await broken.h.get("session_start")!({ reason: "startup" }, broken.ctx);
    assert.deepEqual(await broken.h.get("input")!({ source: "interactive", text: "one" }, broken.ctx), { action: "handled" });
    broken.ctx.ui.select = async () => "Yes: it is my choice for this session";
    assert.equal(await broken.h.get("input")!({ source: "interactive", text: "two" }, broken.ctx), undefined, "and the next prompt can ask again");
  });
  ModelChoice.forget();
});

test("a held session shows the line after a reload and a resume, as at startup; only a /new that repeats it is quiet (L942)", async () => {
  const dir = agentDir();
  profileOn(dir, "anthropic", "claude-opus-4-5");
  ModelChoice.forget();
  await withArgv([], async () => {
    const counts: number[] = [];
    for (const reason of ["startup", "reload", "resume", "new", "reload", "fork"]) {
      const s = choiceSession(dir);
      await s.h.get("session_start")!({ reason }, s.ctx);
      counts.push(s.shown.length);
    }
    assert.deepEqual(counts, [1, 1, 1, 0, 1, 1]);
  });
  ModelChoice.forget();
});

test("the project's file and the profile name the same model that cannot run: the line names the profile (L942)", async () => {
  const dir = agentDir(), cwd = tempDir("jhon-pi-project-");
  profileOn(dir, "anthropic", "claude-opus-4-5");
  mkdirSync(join(cwd, ".pi"));
  writeFileSync(join(cwd, ".pi", "settings.json"), JSON.stringify({ defaultProvider: "anthropic", defaultModel: "claude-opus-4-5" }));
  ModelChoice.forget();
  await withArgv([], async () => {
    const s = choiceSession(dir, { cwd });
    await s.h.get("session_start")!({ reason: "startup" }, s.ctx);
    assert.deepEqual(s.shown, [`${HELD_LINE} Your next prompt asks whether to run on it; you can also pick another model, or restart with the credential set.`]);
  });
  ModelChoice.forget();
});

// L194: the `/` list says what each command does; an argument offers its values.
test("every Jhon command says what it does in one short line, and /jhon:status and /jhon:profile complete their argument, a line each (L194)", async () => {
  const dir = agentDir();
  const repo = tempDir("jhon-pi-ldd-");
  mkdirSync(join(repo, "openspec", "changes", "archive", "2026-01-01-old"), { recursive: true });
  mkdirSync(join(repo, "openspec", "changes", "abc-process"), { recursive: true });
  writeFileSync(join(repo, "openspec", "changes", "abc-process", "tasks.md"), "# Tasks\n\n## Intent\n\nThe ABC process, end to end.\nMore of it.\n");
  mkdirSync(join(repo, "openspec", "changes", "rail-polish"), { recursive: true });
  writeFileSync(join(repo, "openspec", "changes", "notes.md"), "not a change\n");
  mkdirSync(join(repo, "sub"));
  const { commands } = loadedWith(dir, { PI_CODING_AGENT_DIR: dir });
  for (const [name, command] of commands) {
    assert.ok(command.description.length <= 80, `${name}: ${command.description}`);
    assert.doesNotMatch(command.description, /\/jhon[ \]]/, `${name} names no old /jhon <word> form`);
  }
  const ldd = commands.get("jhon:status").getArgumentCompletions;
  const profile = commands.get("jhon:profile").getArgumentCompletions;
  const home = process.cwd();
  process.chdir(join(repo, "sub"));
  try {
    assert.deepEqual(await ldd(""), [
      { value: "abc-process", label: "abc-process", description: "The ABC process, end to end." },
      { value: "rail-polish", label: "rail-polish", description: "An LDD change, with no intent written yet" },
    ], "the project's changes, never the archived ones, each with its intent's first line");
    assert.deepEqual((await ldd("ra")).map((item: any) => item.value), ["rail-polish"]);
    assert.equal(await ldd("zz"), null);
    assert.equal(await ldd("abc-process x"), null, "one change only");
  } finally {
    process.chdir(home);
  }
  process.chdir(tempDir("jhon-pi-none-"));
  try { assert.equal(await ldd(""), null, "no LDD project: nothing to offer"); } finally { process.chdir(home); }
  assert.deepEqual(await profile(""), [
    { value: "list", label: "list", description: "Lists the model profiles" },
    { value: "show", label: "show", description: "Shows a profile: show [<name>], the active one with no name" },
    { value: "use", label: "use", description: "Makes a profile the active one: use <name>" },
  ]);
  assert.deepEqual((await profile("u")).map((item: any) => item.value), ["use"]);
});

// L194: pi's /jhon-ldd is the shell's command, no longer a prompt template.
test("/jhon-ldd sends the agent the LDD prompt for its arguments, steering while the agent works as a typed prompt did; its actions complete", async () => {
  const dir = agentDir();
  const sent: Array<{ text: string; options: any }> = [];
  const { commands } = loadedWith(dir, { PI_CODING_AGENT_DIR: dir }, { sendUserMessage: (text: string, options: any) => sent.push({ text, options }) });
  const ldd = commands.get("jhon-ldd");
  assert.equal(ldd.description, "Runs an LDD action on a change; with no action it continues");
  const skill = readFileSync(join(REPO, "runtimes", "claude-code", "plugin", "skills", "jhon-ldd", "SKILL.md"), "utf8");
  await ldd.handler("new abc-process", { isIdle: () => true });
  await ldd.handler("", { isIdle: () => false });
  assert.deepEqual(sent, [
    { text: lddPrompt(skill, "new abc-process"), options: undefined },
    { text: lddPrompt(skill, ""), options: { deliverAs: "steer" } },
  ]);
  assert.deepEqual((await ldd.getArgumentCompletions("ar")).map((item: any) => item.value), ["archive"]);
});

test("/jhon-ldd that pi refuses (a compaction running) says so once and sends nothing", async () => {
  const dir = agentDir();
  const notes: Array<[string, string]> = [];
  const { commands } = loadedWith(dir, { PI_CODING_AGENT_DIR: dir }, { sendUserMessage: () => { throw new Error("Cannot submit a prompt while compaction is in progress"); } });
  await commands.get("jhon-ldd").handler("status", { isIdle: () => false, ui: { notify: (text: string, level: string) => notes.push([text, level]) } });
  assert.deepEqual(notes, [["jhon-ldd: Cannot submit a prompt while compaction is in progress; run it again when pi is ready", "warning"]]);
});

test("/jhon:grants shows only the grants: y, s and n run no jhon mode, the mode is Customize's (L214)", async () => {
  const dir = agentDir();
  const bin = join(dir, "jhon");
  const log = join(dir, "jhon.log");
  writeFileSync(bin, `#!/bin/sh
echo "$*" >> '${log}'
case "$1" in
  grant) echo '{"root":"${dir}","classes":["push","destroy"],"grants":[]}' ;;
  mode) echo '{"mode":"normal","root":"${dir}","since":null}' ;;
  status) echo '{"change":null,"mode":"normal"}'; exit 1 ;;
  *) echo "ran: $*" ;;
esac
`);
  chmodSync(bin, 0o755);
  const { commands } = loadedWith(dir, { PI_CODING_AGENT_DIR: dir, JHON_BIN: bin });
  const panels: any[] = [];
  const selects: string[] = [];
  const tui = { terminal: { rows: 40 }, requestRender() {} };
  const ctx = { cwd: dir, ui: { notify: () => {}, select: async (title: string) => { selects.push(title); return "Yes"; }, custom: async (f: any) => { panels.push(f(tui, { fg: (_r: string, t: string) => t, bold: (t: string) => t }, null, () => {})); return null; } } };
  const until = async (ok: () => boolean) => { for (let i = 0; i < 400 && !ok(); i++) await new Promise((r) => setTimeout(r, 30)); };
  const ran = () => existsSync(log) ? readFileSync(log, "utf8").trim().split("\n") : [];
  await commands.get("jhon:grants").handler("", ctx);
  const panel = panels[0];
  await until(() => /Active grants · 0/.test(panel.render(120).join("\n")));
  const text = panel.render(120).join("\n");
  assert.match(text, /│ Grants +esc │/);
  assert.doesNotMatch(text, /Mode|● normal|yolo|since/, "no Mode row, no since line");
  assert.doesNotMatch(panel.render(120).at(-2)!, /y yolo|s super yolo|n normal/, "no mode keys in the hints");
  for (const key of ["y", "s", "n"]) panel.handleInput(key);
  await new Promise((r) => setTimeout(r, 300));
  assert.deepEqual(ran().filter((line) => line.startsWith("mode")), [], "no jhon mode ran");
  assert.deepEqual(selects, [], "nothing asked");
});

test("alt+j opens the change's detail, the rail's LDD section expanded (L96, L113)", async () => {
  const dir = agentDir();
  const bin = join(dir, "jhon");
  writeFileSync(bin, `#!/bin/sh
case "$1" in
  status) echo '{"change":"c1","directory":"openspec/changes/c1","route":{"level":"standard","tier":"medium","reasons":[]}}' ;;
  risk) echo '{"tier":"medium","reasons":[]}' ;;
  *) echo "ran: $*" ;;
esac
`);
  chmodSync(bin, 0o755);
  const shortcuts = new Map<string, any>();
  const { commands } = loadedWith(dir, { PI_CODING_AGENT_DIR: dir, JHON_BIN: bin }, { registerShortcut: (k: string, s: any) => shortcuts.set(k, s) });
  const panels: any[] = [];
  const tui = { terminal: { rows: 40 }, requestRender() {} };
  const options: any[] = [];
  const ctx = { cwd: dir, ui: { notify: () => {}, custom: async (f: any, o: any) => { options.push(o); panels.push(f(tui, { fg: (_r: string, t: string) => t, bold: (t: string) => t }, null, () => {})); return null; } } };
  // L174: alt+j is named as LDD's, never as a "change" that could be git's.
  assert.equal(shortcuts.get("alt+j").description, "Opens the LDD status, as /jhon:status (the rail's LDD section expanded)");
  await shortcuts.get("alt+j").handler(ctx);
  let text = "";
  for (let i = 0; i < 100 && !/LDD · c1 +│/.test(text); i++) { text = panels[0].render(100).join("\n"); await new Promise((r) => setTimeout(r, 30)); }
  assert.match(text, /c1/);
  assert.match(text, /t tests {2}o tasks\.md/, "the LDD detail is open");
  // L174: a work view, near full screen as G's profiles view; /jhon:grants is the small card.
  assert.deepEqual(options[0], { overlay: true, overlayOptions: { anchor: "center", width: "100%", maxHeight: "100%", margin: 0 } });
  await commands.get("jhon:grants").handler("", ctx);
  await commands.get("jhon:profile").handler("", ctx);
  assert.deepEqual(options[1], { overlay: true, overlayOptions: { anchor: "center", width: "70%", minWidth: 60, maxHeight: "85%" } }, "/jhon:grants: the small card");
  assert.deepEqual(options[2], options[0], "/jhon:profile: full screen");
});

test("a child calls only its own tools: any other is refused before a guard runs", async () => {
  const d = project();
  const r = await judge("write", { path: join(d, "b.txt"), content: "x" }, { ...child(d), tools: ["read", "bash"] });
  assert.ok(r && "block" in r && /write is not one of this agent's tools \(read, bash\)/.test(r.reason), JSON.stringify(r));
  assert.equal(await judge("bash", { command: "ls" }, { ...child(d), tools: ["read", "bash"] }), null);
});

test("a bounded child's task scopes its writes: outside its allowed edit surfaces is refused, inside is allowed", async () => {
  const d = lddProject();
  const launchPrompt = "Fix it.\n\n## Allowed edit surfaces\n- a.txt\n";
  const out = await judge("write", { path: join(d, "b.txt"), content: "x" }, { ...child(d), launchPrompt });
  assert.ok(out && "block" in out && /scope-guard: .*b\.txt is outside the allowed edit surfaces \(a\.txt\)/.test(out.reason), JSON.stringify(out));
  const shell = await judge("bash", { command: "echo x > b.txt" }, { ...child(d), launchPrompt });
  assert.ok(shell && "block" in shell && /outside the allowed edit surfaces/.test(shell.reason), JSON.stringify(shell));
  assert.equal(await judge("write", { path: join(d, "a.txt"), content: "x" }, { ...child(d), launchPrompt }), null);
});

// T133: a continue carries a corrected edit surface. The scope a continue
// leaves a child with (continuedScope) is judged by the release's own guard.
test("a continue with a wider `## Allowed edit surfaces` lets the child write the added path; one without the heading keeps the old list", async () => {
  const d = lddProject();
  const launched = "Fix it.\n\n## Allowed edit surfaces\n- a.txt\n";
  const write = (file: string, launchPrompt: string) => judge("write", { path: join(d, file), content: "x" }, { ...child(d), launchPrompt });
  const wider = continuedScope(launched, "b.txt is yours too.\n\n## Allowed edit surfaces\n- a.txt\n- b.txt\n");
  assert.equal(await write("b.txt", wider), null, "the added path");
  assert.equal(await write("a.txt", wider), null, "a path both lists name");
  const other = await write("c.txt", wider);
  assert.ok(other && "block" in other && /scope-guard: .*c\.txt is outside the allowed edit surfaces \(a\.txt, b\.txt\)/.test(other.reason), JSON.stringify(other));
  const kept = continuedScope(launched, "The owner said yes: edit b.txt and go on.");
  const out = await write("b.txt", kept);
  assert.ok(out && "block" in out && /scope-guard: .*b\.txt is outside the allowed edit surfaces \(a\.txt\)/.test(out.reason), JSON.stringify(out));
  assert.equal(await write("a.txt", kept), null);
  assert.equal(await write("b.txt", continuedScope(wider, "go on")), null, "a later continue without the heading keeps the wider list");
});

test("a bounded child's bash writes pass the scope guard as its writes do: sed -i, a heredoc, tee, cp, perl -pi", async () => {
  const d = lddProject();
  const launchPrompt = "Fix it.\n\n## Allowed edit surfaces\n- a.txt\n";
  for (const form of ["sed -i.bak 's/a/b/' %s", "cat > %s <<'EOF'\nx\nEOF", "echo x | tee -a %s", "cp a.txt %s", "perl -pi -e 's/a/b/' %s", "echo x >> %s"]) {
    const out = await judge("bash", { command: form.replaceAll("%s", "b.txt") }, { ...child(d), launchPrompt });
    assert.ok(out && "block" in out && /scope-guard: the command writes .*b\.txt, outside the allowed edit surfaces/.test(out.reason), `${form}: ${JSON.stringify(out)}`);
    assert.equal(await judge("bash", { command: form.replaceAll("%s", "a.txt").replace("cp a.txt a.txt", "cp /tmp/x.txt a.txt") }, { ...child(d), launchPrompt }), null, form);
  }
});

test("a pi write to tasks.md is never refused for a slice's size or depth, as Claude Code's is not: jhon status notes it", async () => {
  const d = project();
  const tasks = join(d, "openspec", "changes", "c", "tasks.md");
  mkdirSync(dirname(tasks), { recursive: true });
  const many = Array.from({ length: 13 }, (_, i) => `- [ ] 1.${i + 1} t`).join("\n");
  assert.equal(await judge("write", { path: tasks, content: `## Slice 1: a\n\nDelivers: x\n\n${many}\n` }, child(d)), null, "13 tasks");
  assert.equal(await judge("write", { path: tasks, content: "## Slice 1: a\n\nDelivers: x\n\n- [ ] 1.1 t\n  - [ ] 1.1.1 u\n    - [ ] 1.1.1.1 v\n" }, child(d)), null, "three levels");
});

test("a child's limits come from its environment; an unreadable task file allows no surface", async () => {
  const { childLimits } = await import("../extensions/jhon.ts");
  const dir = tempDir("jhon-pi-limits-");
  const task = join(dir, "task.md");
  writeFileSync(task, "## Allowed edit surfaces\n- a.txt\n");
  assert.deepEqual(childLimits({ JHON_PI_CHILD: "1", JHON_PI_TOOLS: "read,bash", JHON_PI_TASK_FILE: task }), { tools: ["read", "bash"], launchPrompt: "## Allowed edit surfaces\n- a.txt\n" });
  assert.deepEqual(childLimits({ JHON_PI_CHILD: "1", JHON_PI_TASK_FILE: join(dir, "gone.md") }), { launchPrompt: "## Allowed edit surfaces\n" });
  assert.deepEqual(childLimits({ JHON_PI_TOOLS: "read" }), {}, "the main session has no limits");
  assert.deepEqual(childLimits({ JHON_PI_CHILD: "1", JHON_PI_MAX_TURNS: "40" }), {}, "a turn cap in the environment is not a limit");
});

// T42c: the shell's own tools reach the model with short descriptions that
// still carry every instruction the agent needs.
const TOOL_TEXT_BUDGET = 2800;
test("the shell's tools keep short descriptions that carry what the agent needs", () => {
  const dir = agentDir();
  const { tools } = loaded(dir);
  const byName = new Map(tools.map((t: any) => [t.name, t]));
  const textOf = (t: any) => [t.description, t.promptSnippet ?? "", ...(t.promptGuidelines ?? [])].join("\n");
  const pins: Record<string, RegExp[]> = {
    inspect: [/read-only/, /No shell/, /quotes, pipes, redirections/, /git log --oneline -20 -- src\/a\.ts/],
    subagent: [/Agents: /, /## Allowed edit surfaces/, /runs in the background/, /do not poll or sleep/, /several in one turn/, /task_result/, /task_stop/, /worktree: true/, /integrate the branch/],
    subagent_continue: [/stopped short/, /id subagent returned/, /Its scope carries over/, /context cap/, /fresh one with a handoff/, /runs in the background/],
    ask_user: [/1 to \d+ questions/, /\d+ to \d+ options/, /never add one/, /multiSelect/, /\(Recommended\)/, /interactive main session/],
    bash_background: [/task id at once/, /notified once/, /do not poll, sleep, or delegate/, /about 30 seconds/, /gh run watch <run-id> --exit-status/, /read tool/, /task_stop/, /At most \d+/],
    monitor: [/each output line as an event/, /Do not poll or sleep/, /bash_background instead/, /timeout_seconds is mandatory/, /filter the command's output/, /task_stop/],
    task_list: [/background tasks/, /output file/],
    task_result: [/sub-agent's answer/, /do not poll/],
    task_stop: [/process tree/, /sends no notice/],
  };
  let total = 0;
  for (const [name, res] of Object.entries(pins)) {
    const tool = byName.get(name);
    assert.ok(tool, `${name} is registered`);
    const text = textOf(tool);
    total += text.length;
    for (const re of res) assert.match(text, re, `${name} keeps ${re}`);
  }
  assert.doesNotMatch(textOf(byName.get("subagent_continue")), /turn cap|remaining turns/, "T131: no turn cap to speak of");
  assert.ok(total <= TOOL_TEXT_BUDGET, `the shell's tool text is ${total} chars, over ${TOOL_TEXT_BUDGET}`);
});

test("the package loads Jhon's extension, its shell UI, and its Kanagawa theme, and nothing else", () => {
  const manifest = JSON.parse(readFileSync(join(REPO, "shell", "package.json"), "utf8")).pi;
  assert.deepEqual(manifest, { extensions: ["./extensions/jhon.ts", "./extensions/ui.ts", "./extensions/pretty.ts", "./extensions/todo.ts", "./extensions/quiet.ts"], themes: ["./themes/*.json"] });
  // T169 (L1438): the notification sounds are gone, their extension with them.
  assert.equal(manifest.extensions.some((path: string) => /notification/.test(path)), false);
});

test("the safety guard refuses an irreversible command run in the background or as a monitor", async () => {
  const d = project();
  for (const tool of ["bash_background", "monitor"]) {
    const r = await judge(tool, { command: "git push --force", label: "x" }, parent(d));
    assert.ok(r && "block" in r, `${tool}: ${JSON.stringify(r)}`);
    assert.match(r.reason, /^safety-guard: refused: git push --force/);
  }
});

// Docket AY B2: style is never the agent's call. A file pi edits or writes
// is rewritten by its project's formatter (`jhon hook format-edit`), and the
// agent is told when its copy went stale.
test("an edited file is rewritten by its project's formatter, and the agent is told", async (t) => {
  if (spawnSync("gofmt", ["-h"]).error) return t.skip("gofmt is not installed");
  const d = tempDir("jhon-pi-fmt-");
  writeFileSync(join(d, "go.mod"), "module example.com/x\n");
  writeFileSync(join(d, "a.go"), "package x\nfunc  A( ) int { return 1 }\n");
  const result = loaded(agentDir()).handlers.get("tool_result")!;
  const event = (toolName: string, path: string, isError = false) => ({ toolName, input: { path }, content: [{ type: "text", text: "Wrote a.go" }], isError });
  const r = await result(event("write", "a.go"), { cwd: d });
  assert.equal(readFileSync(join(d, "a.go"), "utf8"), "package x\n\nfunc A() int { return 1 }\n");
  assert.deepEqual(r?.content?.map((c: any) => c.text), ["Wrote a.go", "jhon: gofmt reformatted a.go; read it again before the next edit"]);
  assert.equal(await result(event("edit", join(d, "a.go")), { cwd: d }), undefined, "already formatted: nothing to add");
  writeFileSync(join(d, "b.go"), "package x\nfunc  B( ) {}\n");
  assert.equal(await result(event("write", "b.go", true), { cwd: d }), undefined, "a failed write is left alone");
  assert.equal(readFileSync(join(d, "b.go"), "utf8"), "package x\nfunc  B( ) {}\n");
});

test("a sub-agent launch is judged as Claude Code's Agent call, so the route guard sees which agent runs", () => {
  assert.deepEqual(claudeCall("subagent", { agent: "verify", task: "x" }), { tool_name: "Agent", tool_input: { subagent_type: "verify", prompt: "x" } });
});

test("the main session compacts after a reply once its context reaches 60% of its window, one at a time; a child never does", async () => {
  const settle = loaded(agentDir()).handlers.get("agent_settled")!;
  const compactions: Array<{ onComplete?: () => void }> = [];
  const ctx = (percent: number) => ({ cwd: project(), ui: { notify: () => {} }, getContextUsage: () => ({ percent, tokens: 1, contextWindow: 1_000_000 }), compact: (o: any) => compactions.push(o) });
  await settle({}, ctx(45));
  assert.equal(compactions.length, 0, "under 60%");
  await settle({}, ctx(65));
  assert.equal(compactions.length, 1);
  await settle({}, ctx(66));
  assert.equal(compactions.length, 1, "one compaction at a time");
  compactions[0]!.onComplete?.();
  await settle({}, ctx(61));
  assert.equal(compactions.length, 2);
  const child = loaded(agentDir(), { JHON_PI_CHILD: "1" }).handlers.get("agent_settled");
  if (child) await child({}, ctx(90));
  assert.equal(compactions.length, 2, "a child never compacts here");
});

// T115a (L691): compaction too comes at whichever is first, compact_at's
// share or shell.behaviour.compact_tokens (200000 by default; 0 is none).
test("on a 1M window the main session compacts once past the token budget; a small window compacts at the share as before; a budget of 0 leaves only the share", async () => {
  const { installShellSettings } = await import("../lib/settings.ts");
  const { memorySettings } = await import("./fake-jhon-settings.ts");
  const settle = loaded(agentDir()).handlers.get("agent_settled")!;
  const compactions: Array<{ onComplete?: () => void }> = [];
  const notes: string[] = [];
  const ctx = (tokens: number, contextWindow: number) => ({ cwd: project(), ui: { notify: (m: string) => notes.push(m) }, getContextUsage: () => ({ percent: (tokens / contextWindow) * 100, tokens, contextWindow }), compact: (o: any) => compactions.push(o) });
  await settle({}, ctx(190_000, 1_000_000));
  assert.equal(compactions.length, 0, "under 200K and 60%");
  await settle({}, ctx(205_000, 1_000_000));
  await settle({}, ctx(210_000, 1_000_000));
  assert.equal(compactions.length, 1, "once, one at a time");
  assert.ok(notes.includes("Compacting the context: it is at 205K tokens and Jhon compacts at 200K tokens."), notes.join("\n"));
  compactions[0]!.onComplete?.();
  await settle({}, ctx(70_000, 128_000));
  assert.equal(compactions.length, 1, "54% of a small window, under both");
  await settle({}, ctx(80_000, 128_000));
  assert.equal(compactions.length, 2, "the share first on a small window");
  assert.ok(notes.includes("Compacting the context: it is at 63% and Jhon compacts at 60%."), "the share's wording as before");
  compactions[1]!.onComplete?.();
  const previous = installShellSettings(memorySettings({ "shell.behaviour.compact_tokens": 0 }));
  try {
    await settle({}, ctx(500_000, 1_000_000));
    assert.equal(compactions.length, 2, "0 turns the budget off");
    await settle({}, ctx(600_000, 1_000_000));
    assert.equal(compactions.length, 3, "the share still compacts");
  } finally { installShellSettings(previous); }
});

// T121 (L762 F1): the main session's own compaction also comes before the
// first prompt of a session already over budget (a resume), and between the
// turns of a long run, which then carries on; a child never compacts here.
const usageOf = (tokens: number | null, contextWindow = 1_000_000) => ({ tokens, percent: tokens === null ? null : (tokens / contextWindow) * 100, contextWindow });

test("a resumed session over budget compacts before the owner's prompt, and the prompt runs right after it; under budget or mid-stream nothing happens", async () => {
  const { handlers } = loadedWith(agentDir(), {}, { sendMessage: () => {} });
  const input = handlers.get("input")!;
  const compactions: Array<{ onComplete?: () => void }> = [];
  const notes: string[] = [];
  const ctx = (tokens: number) => ({ cwd: project(), ui: { notify: (m: string) => notes.push(m) }, getContextUsage: () => usageOf(tokens), compact: (o: any) => compactions.push(o) });
  let ran = false;
  const prompt = input({ type: "input", text: "sigue", source: "interactive" }, ctx(375_057)).then((r: any) => { ran = true; return r; });
  await new Promise((r) => setTimeout(r, 20));
  assert.equal(compactions.length, 1, "compacts before the prompt");
  assert.equal(ran, false, "the prompt waits for the compaction");
  assert.ok(notes.includes("Compacting the context: it is at 375K tokens and Jhon compacts at 200K tokens."), notes.join("\n"));
  compactions[0]!.onComplete?.();
  assert.equal(await prompt, undefined, "the owner's prompt is neither handled nor transformed: it runs");
  assert.equal(await input({ type: "input", text: "otra", source: "interactive" }, ctx(150_000)), undefined);
  assert.equal(compactions.length, 1, "under budget: no compaction");
  await input({ type: "input", text: "steer", source: "interactive", streamingBehavior: "steer" }, ctx(375_000));
  assert.equal(compactions.length, 1, "a message to a running agent never compacts before it");
  // A failed compaction (pi: nothing to compact) still lets the prompt run.
  const failing = { ...ctx(375_000), compact: (o: any) => { compactions.push(o); o.onError?.(new Error("Already compacted")); } };
  assert.equal(await input({ type: "input", text: "x", source: "interactive" }, failing), undefined);
  assert.equal(compactions.length, 2);
});

test("a turn that ends over budget mid-run compacts and the run carries on; the last turn, under budget, headless and child runs do not", async () => {
  const sent: Array<{ message: any; options: any }> = [];
  const { handlers } = loadedWith(agentDir(), {}, { sendMessage: (message: any, options: any) => sent.push({ message, options }) });
  const turnEnd = handlers.get("turn_end")!;
  const settle = handlers.get("agent_settled")!;
  const compactions: Array<{ onComplete?: () => void; onError?: (e: Error) => void }> = [];
  const ctx = (tokens: number | null, extra: Record<string, unknown> = {}) => ({ cwd: project(), ui: { notify: () => {} }, getContextUsage: () => usageOf(tokens), compact: (o: any) => compactions.push(o), shutdown: () => {}, ...extra });
  const toolTurn = { type: "turn_end", turnIndex: 3, toolResults: [{ role: "toolResult" }] };
  await turnEnd(toolTurn, ctx(150_000));
  assert.equal(compactions.length, 0, "under budget");
  await turnEnd({ ...toolTurn, toolResults: [] }, ctx(250_000));
  assert.equal(compactions.length, 0, "the reply's last turn is left to agent_settled");
  await turnEnd(toolTurn, ctx(250_000, { mode: "print" }));
  assert.equal(compactions.length, 0, "a headless run is not interrupted");
  await turnEnd(toolTurn, ctx(250_000));
  assert.equal(compactions.length, 1, "between turns");
  await settle({ aborted: true }, ctx(250_000));
  await turnEnd(toolTurn, ctx(251_000));
  assert.equal(compactions.length, 1, "one at a time; the aborted settle does not compact again on the stale usage");
  assert.deepEqual(sent, [], "nothing sent while it compacts");
  compactions[0]!.onComplete?.();
  assert.equal(sent.length, 1);
  assert.equal(sent[0]!.message.customType, "jhon-compaction-continue");
  assert.match(sent[0]!.message.content, /^\[Harness note from the Jhon shell, not a message from the owner\] The context was compacted between the turns of your run/);
  assert.deepEqual(sent[0]!.options, { triggerTurn: true }, "a new turn continues the run");
  await turnEnd(toolTurn, ctx(null));
  assert.equal(compactions.length, 1, "after the compaction the context is unknown until the next reply: nothing recompacts");
  // A compaction that fails still continues the run it stopped.
  await turnEnd(toolTurn, ctx(260_000));
  compactions[1]!.onError?.(new Error("boom"));
  assert.equal(sent.length, 2);
  const child = loadedWith(agentDir(), { JHON_PI_CHILD: "1" }, { sendMessage: (message: any, options: any) => sent.push({ message, options }) }).handlers;
  await child.get("turn_end")!(toolTurn, ctx(250_000));
  assert.equal(compactions.length, 2, "a child never compacts here");
  assert.equal(child.get("input"), undefined, "a child has no pre-prompt compaction");
});

// T124 (L762 F4): the notice judged the context the compaction was about to
// drop (393K, delivered after it at 48K); it now waits for a reply that measures the compacted context.
test("a reply that compacts gives no fresh-session or backstop notice; after the compaction one comes only past its threshold", async () => {
  const sent: any[] = [];
  const notices: string[] = [];
  const { handlers } = loadedWith(agentDir(), {}, { sendMessage: (m: any) => sent.push(m) });
  const settle = handlers.get("agent_settled")!;
  const compactions: Array<{ onComplete?: () => void }> = [];
  const ctx = (tokens: number | null) => ({ cwd: project(), ui: { notify: (m: string) => notices.push(m) }, getContextUsage: () => usageOf(tokens), compact: (o: any) => compactions.push(o) });
  await settle({}, ctx(392_767));
  assert.equal(compactions.length, 1);
  assert.deepEqual(sent.map((m) => m.customType), [], "no notice on the pre-compaction context");
  assert.ok(!notices.some((n) => n.startsWith("One session per change")), notices.join("\n"));
  compactions[0]!.onComplete?.();
  await settle({}, ctx(null));
  await settle({}, ctx(48_792));
  assert.deepEqual(sent.map((m) => m.customType), [], "below its 150K budget after the compaction");
  await settle({}, ctx(160_000));
  assert.ok(sent.some((m) => m.customType === "jhon-fresh-session"), "past it, it fires once as before");
});

// S35 (L145): one session per change. The main session suggests a fresh one
// with a handoff once when its context passes 50% after a reply, and once
// per archived change; it is a notice, never a block, and a child gets none.
test("the main session suggests a fresh session once at 50% of its context and once after an archive; a child never does", async () => {
  const notices: string[] = [];
  const sent: string[] = [];
  const api = { sendMessage: (m: any) => sent.push(m.content) };
  const main = loadedWith(agentDir(), {}, api).handlers;
  const ctx = (percent: number) => ({ cwd: project(), ui: { notify: (m: string) => notices.push(m) }, getContextUsage: () => ({ percent, tokens: 1, contextWindow: 1_000_000 }) });
  await main.get("agent_settled")!({}, ctx(45));
  assert.deepEqual(notices, [], "under 50%");
  await main.get("agent_settled")!({}, ctx(52));
  await main.get("agent_settled")!({}, ctx(55));
  assert.equal(notices.length, 1, "once at 50%");
  assert.match(notices[0]!, /^One session per change: the context is at 52%/);
  assert.match(notices[0]!, /`jhon context <change> --phase none --handoff`, then mem_session_summary/);
  assert.match(notices[0]!, /\/new in pi; \/clear or a restart in Claude Code/);
  assert.deepEqual(sent, notices, "the agent gets the same notice");
  const line = "One session per change: c1 is archived. Save a handoff (`jhon context --phase none --handoff`, then mem_session_summary), then start a fresh session (/new in pi; /clear or a restart in Claude Code) and give it the handoff.";
  const archived = { toolName: "bash", input: { command: "jhon archive c1 2026-11-05" }, content: [{ type: "text", text: `method: mv\nreadback: empty\n${line}\n` }] };
  await main.get("tool_result")!(archived, ctx(10));
  await main.get("tool_result")!(archived, ctx(10));
  assert.deepEqual(notices.slice(1), [line], "once after the archive of c1");
  assert.equal(sent.length, 1, "the agent already read it in the archive's output");
  await main.get("tool_result")!({ ...archived, isError: true, content: [{ type: "text", text: "archive destination collision" }] }, ctx(10));
  assert.equal(notices.length, 2, "a failed archive says nothing");
  const kid = loadedWith(agentDir(), { JHON_PI_CHILD: "1" }, api).handlers;
  await kid.get("agent_settled")?.({}, ctx(90));
  await kid.get("tool_result")?.(archived, ctx(10));
  assert.equal(notices.length, 2, "a child never suggests one");
  assert.equal(sent.length, 1);
});

// T115a (L691): on a very large window the token budget (150K by default, three quarters of compact_tokens) comes first.
test("on a very large window the main session suggests a fresh session once past the token budget", async () => {
  // T115e's backstop (100K, half of compact_tokens) comes first on this window; it fires at 120K alone (test/context-backstop.test.ts has the ladder).
  const notices: string[] = [];
  const sent: string[] = [];
  const main = loadedWith(agentDir(), {}, { sendMessage: (m: any) => sent.push(m.content) }).handlers;
  const ctx = (tokens: number) => ({ cwd: project(), ui: { notify: (m: string) => notices.push(m) }, getContextUsage: () => ({ percent: tokens / 10_000, tokens, contextWindow: 1_000_000 }) });
  await main.get("agent_settled")!({}, ctx(120_000));
  assert.equal(notices.length, 1, "the delegation backstop");
  assert.match(notices[0]!, /^Context backstop: /);
  notices.length = 0;
  sent.length = 0;
  await main.get("agent_settled")!({}, ctx(140_000));
  assert.deepEqual(notices, [], "under the budget");
  await main.get("agent_settled")!({}, ctx(152_000));
  await main.get("agent_settled")!({}, ctx(383_000));
  assert.equal(notices.length, 1, "once");
  assert.match(notices[0]!, /^One session per change: the context is at 152K tokens, past its 150K-token budget, and every turn re-reads it\. Save a handoff/);
  assert.deepEqual(sent, notices, "the agent gets the same notice");
});

test("the orchestrator learns the owner's shell and that pi has no `!`: a command for the owner is a plain line for their own terminal, written for their shell (L222)", async () => {
  const { ownerShellNote } = await import("../lib/owner-shell.ts");
  assert.equal(ownerShellNote("/usr/bin/fish"), "- The owner's shell is fish. pi has no `!`: a command you hand the owner is a plain line they run in their own terminal, written for fish: `set -x NAME value` or `env NAME=value cmd`, never `export`; `(cmd)` or `$(cmd)`; no heredocs, `[[ ]]`, or `for x in ...; do ...; done` (fish: `for x in ...; ...; end`). Your own Bash tool stays POSIX.");
  assert.equal(ownerShellNote("/usr/bin/nu"), "- The owner's shell is nushell. pi has no `!`: a command you hand the owner is a plain line they run in their own terminal, written for nu, or wrapped in `bash -c '...'`. Your own Bash tool stays POSIX.");
  for (const s of ["bash", "zsh", "sh"]) {
    assert.equal(ownerShellNote(`/usr/bin/${s}`), `- pi has no \`!\`: a command you hand the owner is a plain line they run in their own terminal, written for ${s}.`);
  }
  assert.equal(ownerShellNote(undefined), "- pi has no `!`: a command you hand the owner is a plain line they run in their own terminal, written for a POSIX shell.");
  for (const s of ["/usr/bin/fish", "/usr/bin/nu", "/bin/bash", undefined]) assert.doesNotMatch(ownerShellNote(s), /with `!`/, "never Claude Code's `!`");
});

test("changeRoot reads each folder's status once and hands back the one it found, for the panel to show without reading it again", async () => {
  const { changeRoot } = await import("../extensions/jhon.ts");
  const dir = tempDir("change-root-");
  const wt = join(dir, "geo-handlers");
  mkdirSync(wt);
  const log = join(dir, "calls.log");
  const bin = join(dir, "jhon");
  writeFileSync(bin, `#!/bin/sh\necho "$PWD" >> "${log}"\ncase "$PWD" in *geo-handlers) echo '{"change":"fix-geo","next":"apply","tasks":{"slices":[]}}';; *) echo '{"change":null}'; exit 1;; esac\n`);
  chmodSync(bin, 0o755);
  const entry = { type: "custom", customType: "jhon-shell.session-worktree/v1", data: { sessionId: "s", root: wt, evidence: "explicit" } };
  const ctx = { sessionManager: { getSessionId: () => "s", getEntries: () => [entry] } };
  const where = await changeRoot(ctx, dir, bin);
  assert.deepEqual([where.root, where.worktree, where.status?.change, where.status?.next], [wt, "geo-handlers", "fix-geo", "apply"]);
  assert.deepEqual(readFileSync(log, "utf8").trim().split("\n"), [realpathSync(dir), realpathSync(wt)], "pi's folder, then the worktree: once each");
});

test("a guard's note on an allowed call reaches the agent with the call's result, and the call is not blocked", async () => {
  const d = project();
  const dir = agentDir();
  const note = "route verified (risk high reaches the verified tier (high)): verify is due before delivery; launch the jhon-checker agent and record verify-report.md before the PR";
  const bin = join(dir, "jhon-fake");
  const out = JSON.stringify({ hookSpecificOutput: { hookEventName: "PreToolUse", additionalContext: note } });
  writeFileSync(bin, `#!/bin/sh\nif [ "$1" = contract ]; then printf '%s' '{"shell":"${SHELL_CONTRACT}"}'; exit 0; fi\nif [ "$1" = sandbox ]; then printf '%s' '{"mode":"off","can_run":true,"reason":"","fix":"","project":""}'; exit 0; fi\nif [ "$2" = pre-tool ]; then printf '%s' '${out}'; fi\nexit 0\n`);
  chmodSync(bin, 0o755);
  writeFileSync(join(dir, CONFIG_FILE), JSON.stringify(config({ jhon: bin, orchestrator: join(dir, "orchestrator.md") })));
  const { handlers } = loaded(dir);
  const input = { command: "git commit -m 'slice 1'" };
  assert.equal(await handlers.get("tool_call")!({ toolName: "bash", input }, { cwd: d }), undefined, "the call is not blocked");
  const result = await handlers.get("tool_result")!({ toolName: "bash", input, content: [{ type: "text", text: "committed" }] }, { cwd: d });
  assert.deepEqual(result?.content, [{ type: "text", text: "committed" }, { type: "text", text: note }], "the note follows the call's result");
  assert.equal(await handlers.get("tool_result")!({ toolName: "bash", input, content: [] }, { cwd: d }), undefined, "the note is given once");
});

// T31 (E, L256): in a project that uses LDD, a code edit with no change selected gets one non-blocking reminder per session.
test("a code edit with no LDD change selected gets one reminder, never a block, only in a project that uses LDD, never in a child (T31 E)", async () => {
  const dir = agentDir();
  const state = join(dir, "status.json");
  const bin = join(dir, "jhon-fake");
  writeFileSync(bin, `#!/bin/sh\nif [ "$1" = contract ]; then printf '%s' '{"shell":"${SHELL_CONTRACT}"}'; exit 0; fi\nif [ "$1" = status ]; then A=false; [ -f openspec/config.yaml ] && A=true; sed "s/^{/{\\"ldd\\":{\\"active\\":$A},/" "${state}"; grep -q '"change":null' "${state}" && exit 1; exit 0; fi\nexit 0\n`);
  chmodSync(bin, 0o755);
  writeFileSync(join(dir, CONFIG_FILE), JSON.stringify(config({ jhon: bin, orchestrator: join(dir, "orchestrator.md") })));
  writeFileSync(state, '{"change":null,"activeChanges":[]}');
  const ldd = project();
  mkdirSync(join(ldd, "openspec", "specs"), { recursive: true });
  const other = project();
  const edit = (path: string, toolName = "edit", isError = false) => ({ toolName, input: { path }, content: [{ type: "text", text: "Edited" }], isError });
  const reminder = "jhon (LDD): this project uses LDD and this session has no change selected. Start with `jhon status`, then write the RED test first and run it through `jhon executions run` while it fails, before the behaviour change. A reminder, not a refusal.";
  let result = loaded(dir).handlers.get("tool_result")!;
  assert.equal(await result(edit("src/a.ts"), { cwd: other }), undefined, "a project with no openspec/ is not an LDD one");
  // "Uses LDD" is the engine's ldd.active (this fake: an openspec/config.yaml in the cwd), not any openspec/.
  assert.equal(await result(edit("src/a.ts"), { cwd: ldd }), undefined, "an openspec/ without config.yaml is not an LDD project");
  writeFileSync(join(ldd, "openspec", "config.yaml"), "schema: spec-driven\n");
  assert.equal(await result(edit("README.md"), { cwd: ldd }), undefined, "a document is no code");
  assert.equal(await result(edit("openspec/changes/x/tasks.md"), { cwd: ldd }), undefined, "the LDD record is no code");
  assert.equal(await result(edit("src/a.ts", "edit", true), { cwd: ldd }), undefined, "a failed edit changed nothing");
  const r = await result(edit("src/a.ts"), { cwd: ldd });
  assert.deepEqual(r?.content?.map((c: any) => c.text), ["Edited", reminder], "appended to the edit's result");
  assert.equal(r?.block, undefined, "never a block");
  assert.equal(await result(edit("src/b.go", "write"), { cwd: ldd }), undefined, "once per session");

  result = loaded(dir).handlers.get("tool_result")!;
  assert.deepEqual((await result(edit("src/b.go", "write"), { cwd: ldd }))?.content?.map((c: any) => c.text), ["Edited", reminder], "a write too, in a new session");
  result = loaded(dir).handlers.get("tool_result")!;
  writeFileSync(state, '{"change":"feat-small-fix","directory":null,"folder":false,"tasks":{"slices":[]}}');
  assert.equal(await result(edit("src/a.ts"), { cwd: ldd }), undefined, "a change selected, folder or not: no reminder");
  writeFileSync(state, '{"change":null,"activeChanges":[]}');
  assert.equal(await result(edit("src/a.ts"), { cwd: ldd }), undefined, "once a change existed, the session is never reminded");
  const kid = loaded(dir, { JHON_PI_CHILD: "1" }).handlers.get("tool_result")!;
  assert.equal(await kid(edit("src/a.ts"), { cwd: ldd }), undefined, "a child never");
});

// T115f follow-up (L709): a child starts with --no-skills; the subagent tool
// passes the skills its agent's definition names (a `skills/<name>/` path of a
// skill this session resolved) and the exact SKILL.md paths the call adds.
test("a jhon-builder child's argv carries --skill for the skills its definition names and the ones the call passes", async () => {
  const dir = agentDir(), cwd = project();
  const skillsRoot = tempDir("jhon-pi-skills-");
  const skill = (name: string) => { mkdirSync(join(skillsRoot, name), { recursive: true }); const f = join(skillsRoot, name, "SKILL.md"); writeFileSync(f, `---\nname: ${name}\n---\n`); return f; };
  const ldd = skill("ldd"), other = skill("release-notes"), extra = skill("archify");
  mkdirSync(join(dir, "agents"), { recursive: true });
  writeFileSync(join(dir, "agents", "worker.md"), "---\nname: worker\ndescription: \"Implement\"\nmodel: anthropic/claude-sonnet-4-5\ntools:\n  - read\n  - edit\nbounded: true\n---\nFor an LDD apply, follow `~/.local/share/jhon/current/prose/skills/ldd/references/apply.md`; a project skill lives in `.claude/skills/<name>/SKILL.md`.\n");
  const argvFile = join(dir, "argv.json");
  const fakePi = join(dir, "fake-pi");
  writeFileSync(fakePi, `#!/usr/bin/env node\nrequire("fs").writeFileSync(${JSON.stringify(argvFile)}, JSON.stringify(process.argv.slice(2)));\nprocess.stdout.write(JSON.stringify({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "done" }] } }) + "\\n");\n`);
  chmodSync(fakePi, 0o755);
  const env = { JHON_PI_BIN: fakePi };
  const { handlers, tools } = loadedTools(dir, { sendMessage: () => {}, events: { emit: () => {}, on: () => () => {} } }, env);
  const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, skills: [{ name: "ldd", description: "LDD.", filePath: ldd }, { name: "release-notes", description: "Notes.", filePath: other }, { name: "archify", description: "Diagrams.", filePath: extra }] };
  await handlers.get("before_agent_start")!({ systemPromptOptions: options }, { cwd });
  const saved = { ...process.env };
  Object.assign(process.env, { PI_CODING_AGENT_DIR: dir, ...env });
  try {
    const ctx = { cwd, hasUI: false, sessionManager: { getSessionId: () => "parent", getSessionFile: () => undefined } };
    await tools.get("subagent").execute("call-1", { agent: "worker", task: "## Allowed edit surfaces\n- a.txt\n\nDo it.", skills: [extra] }, undefined, undefined, ctx);
    for (const deadline = Date.now() + 5000; !existsSync(argvFile); await new Promise((r) => setTimeout(r, 20))) if (Date.now() > deadline) throw new Error("the child never ran");
    const argv: string[] = JSON.parse(readFileSync(argvFile, "utf8"));
    const skills = argv.flatMap((a, i) => (a === "--skill" ? [argv[i + 1]] : []));
    assert.deepEqual(skills, [ldd, extra], argv.join(" "));
    assert.ok(argv.includes("--no-skills"));
    await assert.rejects(tools.get("subagent").execute("call-2", { agent: "worker", task: "## Allowed edit surfaces\n- a.txt\n\nDo it.", skills: [join(skillsRoot, "nope", "SKILL.md")] }, undefined, undefined, ctx), /skills: .*nope.* is not a SKILL\.md file/);
  } finally {
    for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
    Object.assign(process.env, saved);
  }
});
