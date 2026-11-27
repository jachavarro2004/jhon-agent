/**
 * T128b (L894): the OS sandbox around the commands the model chooses in pi
 * (lib/sandbox.ts). A fake jhon answers `sandbox status --json`, logs every
 * call with its argv and folder, and runs what `sandbox run` is given; one
 * test runs the tree's own binary under real bubblewrap.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test, { after, beforeEach, type TestContext } from "node:test";
import { fileURLToPath } from "node:url";
import { createBashToolDefinition } from "@earendil-works/pi-coding-agent";
import jhon, { CONFIG_FILE } from "../extensions/jhon.ts";
import { childEnv } from "../lib/agents.ts";
import { registerBackgroundTasks } from "../lib/background-tasks-tools.ts";
import { createTaskRegistry } from "../lib/background-tasks.ts";
import { HOOK_TIMEOUT_MS, SHELL_CONTRACT } from "../lib/guards.ts";
import { readSandboxStatus, SANDBOX_READ_REUSE_MS, sandboxOperations, sandboxShown, sandboxVerdict, wireSandbox } from "../lib/sandbox.ts";
import { SANDBOX_EVENT } from "../lib/events.ts";
import { waitFor } from "./bounded-wait.ts";
import { useMemorySettings } from "./fake-jhon-settings.ts";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

type Status = { mode: "on" | "off"; can_run: boolean; reason: string; fix: string; project?: string; timed_out?: boolean };
const ON: Status = { mode: "on", can_run: true, reason: "", fix: "", project: "@cwd" };
const OFF: Status = { mode: "off", can_run: true, reason: "", fix: "", project: "@cwd" };
const CANNOT: Status = { mode: "on", can_run: false, reason: "bubblewrap is not installed", fix: "install bubblewrap (pacman -S bubblewrap)", project: "@cwd" };
/** The engine's probe did not end in time (T128f slice 8c, L1382, L1383): no verdict, so the mode stays on and can_run stays false. */
const TIMED_OUT: Status = { mode: "on", can_run: false, reason: "bubblewrap did not start a sandbox within 5s, which says nothing on whether it can", fix: "run jhon sandbox status again; a sandboxed command fails with bubblewrap's own message if it cannot start", project: "@cwd", timed_out: true };

const FAKE = `#!/usr/bin/env node
const fs = require("node:fs"), path = require("node:path"), cp = require("node:child_process");
const dir = __dirname, args = process.argv.slice(2);
const state = JSON.parse(fs.readFileSync(path.join(dir, "state.json"), "utf8"));
fs.appendFileSync(path.join(dir, "calls.log"), JSON.stringify({ args, cwd: process.cwd() }) + "\\n");
if (args[0] === "contract") { process.stdout.write(JSON.stringify({ shell: state.contract })); process.exit(0); }
if (args[0] === "hook") {
  if (fs.readFileSync(0, "utf8").includes("GUARD-REFUSES")) { process.stderr.write("safety-guard: refused: GUARD-REFUSES\\n"); process.exit(2); }
  process.exit(0);
}
if (args[0] === "sandbox" && args[1] === "status" && args[2] === "--json" && (args.length === 3 || (args.length === 5 && args[3] === "--project"))) {
  if (state.statusFails) { process.stderr.write("jhon sandbox: " + state.statusFails + "\\n"); process.exit(1); }
  let project = state.status.project;
  if (args.length === 5) {
    if (!fs.existsSync(args[4])) { process.stderr.write("jhon sandbox: --project takes an absolute folder that exists: " + args[4] + "\\n"); process.exit(2); }
    project = args[4];
  } else if (project === "@cwd") {
    project = "";
    for (let d = process.cwd(); ; d = path.dirname(d)) {
      if (fs.existsSync(path.join(d, ".git"))) { project = d; break; }
      if (path.dirname(d) === d) break;
    }
  }
  process.stdout.write(JSON.stringify({ ...state.status, project }) + "\\n");
  process.exit(0);
}
if (args[0] === "sandbox" && args[1] === "run") {
  const at = args.indexOf("--");
  const r = cp.spawnSync(args[at + 1], args.slice(at + 2), { stdio: "inherit" });
  process.exit(r.status === null ? 1 : r.status);
}
process.exit(2);
`;

// T258: the mode is pinned when a session starts; a refusal says so, and what a new session would pick up when that differs.
const KEPT = "jhon: refused. This session started with sandbox.mode on and keeps it until it ends";
const WAS_NO_REPOSITORY = "When it started, it was in no git repository, so there was no project to pin.";
const WAS_CANNOT = "When it started, the sandbox could not run here: bubblewrap is not installed.";
const REPOSITORY_FIX = "start the session inside a repository (the owner may run `git init` here and start it again)";
const BUBBLEWRAP_FIX = "install bubblewrap (pacman -S bubblewrap)";
const PINNED_NO_REPOSITORY = `${KEPT}. ${WAS_NO_REPOSITORY} Fix: ${REPOSITORY_FIX}.`;
const PINNED_CANNOT = `${KEPT}. ${WAS_CANNOT} Fix: ${BUBBLEWRAP_FIX}.`;
const PINNED_NOW_OFF_NO_REPOSITORY = `${KEPT}; the setting says off now, which a new session picks up. ${WAS_NO_REPOSITORY} Fix: ask the owner to start a new session (or ${REPOSITORY_FIX}).`;
const NO_REPOSITORY_TODAY = "jhon: sandbox.mode is on and this session is in no git repository, so there is no project to pin and the command is refused, never run unpinned. Fix: start the session inside a repository (the owner may run `git init` here and start it again)";

type Call = { args: string[]; cwd: string };
interface Fake { bin: string; calls(): Call[]; runs(): Call[]; asks(): Call[]; set(status: Status | { fails: string }): void }

function fakeJhonAt(dir: string, status: Status): Fake {
	const write = (s: Status | { fails: string }) => writeFileSync(join(dir, "state.json"), JSON.stringify("fails" in s ? { contract: SHELL_CONTRACT, status: OFF, statusFails: s.fails } : { contract: SHELL_CONTRACT, status: s }));
	write(status);
	writeFileSync(join(dir, "calls.log"), "");
	const bin = join(dir, "jhon");
	writeFileSync(bin, FAKE);
	chmodSync(bin, 0o755);
	const calls = (): Call[] => readFileSync(join(dir, "calls.log"), "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l));
	return { bin, calls, runs: () => calls().filter((c) => c.args[1] === "run"), asks: () => calls().filter((c) => c.args[1] === "status"), set: write };
}

/**
 * A fake jhon that only answers `jhon sandbox status --json`, as FAKE does
 * for `status` asked in `cwd`, and logs a `sandbox run`: a /bin/sh script
 * holding its answer, so a read costs a shell's start, not node's (T264,
 * L1745: on a loaded machine node's start outlasted the session's bound on a
 * read, and the test asserted the machine, not the refusal).
 * JHON_TEST_STATUS_DELAY slows each answer down, as a loaded machine does.
 */
function shellStatusFakeAt(dir: string, cwd: string, status: Status): Fake {
	const projectOf = (from: string): string => {
		for (let d = from; ; d = dirname(d)) {
			if (existsSync(join(d, ".git"))) return d;
			if (dirname(d) === d) return "";
		}
	};
	const bin = join(dir, "jhon"), log = join(dir, "calls.log");
	const write = (s: Status | { fails: string }) => {
		if ("fails" in s) throw new Error("shellStatusFakeAt answers a status, not a failure");
		const answer = JSON.stringify({ ...s, project: s.project === "@cwd" ? projectOf(cwd) : s.project });
		writeFileSync(bin, `#!/bin/sh\ncase "$1 $2 $3" in\n"sandbox status --json") printf '{"args":["sandbox","status"]}\\n' >> '${log}'; [ -n "$JHON_TEST_STATUS_DELAY" ] && sleep "$JHON_TEST_STATUS_DELAY"; printf '%s\\n' '${answer}' ;;\n"sandbox run "*) printf '{"args":["sandbox","run"]}\\n' >> '${log}'; exit 1 ;;\n*) exit 2 ;;\nesac\n`);
		chmodSync(bin, 0o755);
	};
	write(status);
	writeFileSync(log, "");
	const calls = (): Call[] => readFileSync(log, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l));
	return { bin, calls, runs: () => calls().filter((c) => c.args[1] === "run"), asks: () => calls().filter((c) => c.args[1] === "status"), set: write };
}

function scratch(t: TestContext, prefix: string): string {
	const d = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
	t.after(() => rmSync(d, { recursive: true, force: true }));
	return d;
}

/** A folder that holds `.git`, as a project root does. */
function project(t: TestContext): string {
	const d = scratch(t, "jhon-sbx-proj-");
	mkdirSync(join(d, ".git"));
	return d;
}

type Handler = (event: any, ctx: any) => unknown;
function fakePi() {
	const tools = new Map<string, any>();
	const handlers = new Map<string, Handler[]>();
	const pi = {
		registerTool: (tool: any) => tools.set(tool.name, tool),
		registerCommand: () => {},
		registerMessageRenderer: () => {},
		on: (name: string, h: Handler) => handlers.set(name, [...(handlers.get(name) ?? []), h]),
		sendMessage: () => {},
	};
	const fire = async (name: string, event: any, ctx: any) => {
		let result: any;
		for (const h of handlers.get(name) ?? []) result = (await h(event, ctx)) ?? result;
		return result;
	};
	return { pi, tools, fire };
}

const ctxAt = (cwd: string) => ({ cwd, hasUI: false, mode: "rpc", sessionManager: { getSessionId: () => "main", getSessionFile: () => undefined }, ui: { setStatus: () => {}, notify: () => {} } });
const textOf = (result: any): string => (result.content ?? []).map((c: any) => c.text ?? "").join("\n");
/** A command with both quote kinds, a dollar, a backslash and a newline: what a re-quoting would break. */
const TRICKY = `printf '%s|' "it's" 'a "b"' "\\$HOME"\necho done`;
const wrapped = (bin: string, project: string, command: string) => ({ args: ["sandbox", "run", "--project", project, "--profile", "work", "--", "/bin/bash", "-c", command], bin });

function suite(): void {
	// pi's own settings (the configured shell and prefix) are read from a scratch agent dir, never the owner's.
	const AGENT = mkdtempSync(join(tmpdir(), "jhon-sbx-agent-"));
	after(() => rmSync(AGENT, { recursive: true, force: true }));
	Object.assign(process.env, { PI_CODING_AGENT_DIR: join(AGENT, "agent"), JHON_PROFILES: join(AGENT, "profiles.json"), CLAUDE_CONFIG_DIR: join(AGENT, "claude") });
	const bash = existsSync("/bin/bash");
	// A session exports the project it pinned (E5); no test inherits another's.
	beforeEach(() => { delete process.env.JHON_SANDBOX_PROJECT; });

	// L1489 (T174-S4): the status bar says the real state, from the same pin.
	test("the pin says the status bar's state: on, off, or not running and why in a few words", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t);
		const cases: Array<[string, Status, unknown]> = [
			["on", ON, { mode: "on", state: "on" }],
			["off", OFF, { mode: "off", state: "off" }],
			["cannot run", CANNOT, { mode: "on", state: "not running", why: "bubblewrap is not installed" }],
			["probe timed out: wrapped as usual", TIMED_OUT, { mode: "on", state: "on" }],
			["no git repository", { ...ON, project: "" }, { mode: "on", state: "not running", why: "not in a repository" }],
		];
		for (const [name, status, said] of cases) {
			fake.set(status);
			const emitted: unknown[] = [];
			const { pi, fire } = fakePi();
			wireSandbox({ ...pi, events: { emit: (event: string, payload: unknown) => { if (event === SANDBOX_EVENT) emitted.push(payload); } } }, { jhon: fake.bin });
			await fire("session_start", {}, ctxAt(d));
			assert.deepEqual(emitted, [said], name);
		}
		const broken = fakePi();
		const emitted: unknown[] = [];
		wireSandbox({ ...broken.pi, events: { emit: (_event: string, payload: unknown) => emitted.push(payload) } }, { jhon: join(d, "no-such-jhon") });
		await broken.fire("session_start", {}, ctxAt(d));
		assert.deepEqual(emitted, [{ mode: undefined, state: "not running", why: "no status answer" }], "no answer: every command is refused");
	});

	test("the why is a few words of the engine's reason: up to its first aside, never a long line", () => {
		const down = (reason: string, extra: object = {}) => sandboxShown({ mode: "on", canRun: false, reason, fix: "", project: "/p", ...extra });
		assert.deepEqual(down("unprivileged user namespaces are off (kernel.unprivileged_userns_clone = 0)"), { state: "not running", why: "unprivileged user namespaces are off" });
		assert.deepEqual(down("the sandbox does not run on this system (darwin)"), { state: "not running", why: "does not run on this system" });
		assert.deepEqual(down("bubblewrap (bwrap) is not on PATH"), { state: "not running", why: "bubblewrap is not on PATH" });
		assert.deepEqual(down("bubblewrap is on PATH only in a folder a sandboxed command may write and a much longer tail"), { state: "not running", why: "bubblewrap is on PATH only in a fol…" });
		assert.deepEqual(down(""), { state: "not running", why: "cannot run here" });
		assert.deepEqual(sandboxShown({ mode: "on", canRun: true, reason: "", fix: "", project: undefined }), { state: "not running", why: "no project" });
		assert.deepEqual(sandboxShown({ mode: "off", canRun: false, reason: "bubblewrap is not on PATH", fix: "", project: undefined }), { state: "off" }, "off is off, whatever the probe");
		assert.deepEqual(sandboxShown({ unread: "exit 2" }), { state: "not running", why: "no status answer" });
	});

	test("rule 1: the session asks `jhon sandbox status --json` once, in its folder, and keeps the answer", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t);
		const { pi, tools, fire } = fakePi();
		const sandbox = wireSandbox(pi, { jhon: fake.bin });
		await fire("session_start", { reason: "startup" }, ctxAt(d));
		assert.deepEqual(fake.asks(), [{ args: ["sandbox", "status", "--json"], cwd: d }]);
		for (const tool of ["bash", "bash_background", "monitor", "bash"]) assert.equal(await sandbox.gate(tool, d), undefined);
		// The owner turns the mode off mid-session: the session keeps what it pinned.
		fake.set(OFF);
		if (bash) await tools.get("bash").execute("c1", { command: "echo kept" }, undefined, undefined, ctxAt(d));
		assert.equal(fake.asks().length, 1, "asked once for the whole session");
		if (bash) assert.deepEqual(fake.runs().map((c) => c.args.slice(0, 4)), [["sandbox", "run", "--project", d]]);
		// The answer as the shell reads it.
		fake.set({ ...CANNOT, project: "/home/u/project" });
		assert.deepEqual(await readSandboxStatus({ jhon: fake.bin }, d), { mode: "on", canRun: false, reason: CANNOT.reason, fix: CANNOT.fix, project: "/home/u/project" });
	});

	test("rule 2: with the mode on, bash runs as `jhon sandbox run --project <pinned> --profile work -- <shell> -c <command>`, the command one argument", { skip: !bash && "needs /bin/bash" }, async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t);
		const sub = join(d, "pkg");
		mkdirSync(sub);
		const { pi, tools, fire } = fakePi();
		wireSandbox(pi, { jhon: fake.bin });
		// The session starts in a sub-folder: the project is the folder that holds .git.
		await fire("session_start", {}, ctxAt(sub));
		const result = await tools.get("bash").execute("c1", { command: TRICKY }, undefined, undefined, ctxAt(sub));
		assert.equal(textOf(result), `it's|a "b"|$HOME|done\n`);
		assert.deepEqual(fake.runs(), [{ args: wrapped(fake.bin, d, TRICKY).args, cwd: sub }]);
		// The exit code is the command's.
		const failed = await tools.get("bash").execute("c2", { command: "echo no; exit 3" }, undefined, undefined, ctxAt(sub));
		assert.equal(failed.isError, true);
		assert.equal(textOf(failed), "no\n\n\nCommand exited with code 3");
	});

	test("rule 2: bash_background and monitor run their command the same way", { skip: !bash && "needs /bin/bash" }, async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t);
		const { pi, tools, fire } = fakePi();
		const sandbox = wireSandbox(pi, { jhon: fake.bin });
		const out = scratch(t, "jhon-sbx-jobs-");
		const background = registerBackgroundTasks(pi as any, { env: {}, outputDir: () => out, sandbox: sandbox.jobOperations });
		assert.ok(background);
		await fire("session_start", {}, ctxAt(d));
		await tools.get("bash_background").execute("c1", { command: TRICKY }, undefined, undefined, ctxAt(d));
		await tools.get("monitor").execute("c2", { command: "echo 'line one'\necho \"line two\"", timeout_seconds: 30 }, undefined, undefined, ctxAt(d));
		await waitFor(() => existsSync(join(out, "job-2.log")) && readFileSync(join(out, "job-2.log"), "utf8").includes("line two") && readFileSync(join(out, "job-1.log"), "utf8").includes("done"), "both jobs wrote their output");
		assert.equal(readFileSync(join(out, "job-1.log"), "utf8"), `it's|a "b"|$HOME|done\n`);
		assert.deepEqual(fake.runs().map((c) => c.args).sort(), [wrapped(fake.bin, d, TRICKY).args, wrapped(fake.bin, d, "echo 'line one'\necho \"line two\"").args].sort());
		// The owner's own command (the change detail's `t`) is theirs: it runs as before.
		let ended = false;
		background.startOwnerCommand("echo owner", d, "main", "tests (you)", () => { ended = true; });
		await waitFor(() => ended, "the owner's command ended");
		assert.equal(fake.runs().length, 2);
	});

	test("rule 3: the re-registered bash is pi's built-in tool for the model and the chat, with the mode on and off", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t);
		const on = fakePi();
		wireSandbox(on.pi, { jhon: fake.bin });
		await on.fire("session_start", {}, ctxAt(d));
		const builtIn = createBashToolDefinition(d) as Record<string, any>;
		const ours = on.tools.get("bash") as Record<string, any>;
		assert.ok(ours, "with the mode on, bash is registered again");
		assert.deepEqual(Object.keys(ours).sort(), Object.keys(builtIn).sort());
		for (const key of ["name", "label", "description", "promptSnippet"]) assert.equal(ours[key], builtIn[key], key);
		for (const key of ["parameters", "promptGuidelines", "outputSchema", "constrainedSampling"]) assert.equal(JSON.stringify(ours[key]), JSON.stringify(builtIn[key]), key);
		assert.ok(builtIn.promptSnippet && builtIn.promptGuidelines?.length, "the built-in carries the prompt lines an override must repeat");
		// The chat: pi's own renderers, which extensions/quiet.ts wraps by the tool's name.
		for (const key of Object.keys(builtIn).filter((k) => k.startsWith("render"))) assert.equal(String(ours[key]), String(builtIn[key]), key);
		// Off (T219 S9): bash is registered too, pi's built-in definition, so the agent's command carries its marker.
		fake.set(OFF);
		const off = fakePi();
		const sandbox = wireSandbox(off.pi, { jhon: fake.bin });
		await off.fire("session_start", {}, ctxAt(d));
		assert.deepEqual([...off.tools.keys()], ["bash"]);
		const offBash = off.tools.get("bash") as Record<string, any>;
		for (const key of ["name", "label", "description", "promptSnippet"]) assert.equal(offBash[key], builtIn[key], key);
		for (const tool of ["bash", "bash_background", "monitor"]) assert.equal(await sandbox.gate(tool, d), undefined);
		assert.ok(sandbox.jobOperations(undefined), "the agent's jobs run through pi's local operations with the marker");
	});

	test("T219 S9: with the mode off the agent's bash and jobs carry JHON_AGENT=1 and nothing else changes; the owner's `!` (this process's environment) carries none", { skip: !bash && "needs /bin/bash" }, async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), OFF);
		const d = project(t);
		const { pi, tools, fire } = fakePi();
		const env: NodeJS.ProcessEnv = { ...process.env };
		const sandbox = wireSandbox(pi, { jhon: fake.bin, env });
		await fire("session_start", {}, ctxAt(d));
		const result = await tools.get("bash").execute("c1", { command: `printf '%s|%s' "$JHON_AGENT" "$PWD"` }, undefined, undefined, ctxAt(d));
		assert.equal(textOf(result).trim(), `1|${d}`, "the marker, in the same folder");
		assert.equal(textOf(await tools.get("bash").execute("c2", { command: "echo no; exit 3" }, undefined, undefined, ctxAt(d))), "no\n\n\nCommand exited with code 3", "the same output and exit");
		const ran: string[] = [];
		await sandbox.jobOperations(undefined)!.exec(`printf %s "$JHON_AGENT"`, d, { onData: (b: Buffer) => ran.push(b.toString()) });
		assert.equal(ran.join(""), "1", "an agent's job carries the marker");
		assert.equal(env.JHON_AGENT, undefined, "the session's environment, which the owner's `!` commands inherit, carries no marker");
		assert.equal(process.env.JHON_AGENT, undefined);
		assert.deepEqual(fake.runs(), [], "no sandbox runs");
	});

	test("rule 3: in a real pi session the system prompt and the tool list are the same bytes with the sandbox on, and bash runs through it", { skip: !bash && "needs /bin/bash" }, (t) => {
		const root = scratch(t, "jhon-sbx-sdk-");
		const cwd = join(root, "project"), home = join(root, "home"), agentDir = join(home, "agent"), fakeDir = join(root, "fake");
		for (const p of [join(cwd, ".git"), agentDir, fakeDir]) mkdirSync(p, { recursive: true });
		const fake = fakeJhonAt(fakeDir, ON);
		const env = { ...process.env, HOME: home, USERPROFILE: home, XDG_CONFIG_HOME: join(home, "config"), PI_CODING_AGENT_DIR: agentDir,
			JHON_SHELL_CONFIG_HOME: join(home, "jhon-shell"), PI_OFFLINE: "1", JHON_TEST_SANDBOX_CHILD: "1", JHON_TEST_FAKE_JHON: fake.bin };
		const r = spawnSync(process.execPath, ["--experimental-strip-types", fileURLToPath(import.meta.url)], { cwd, env, encoding: "utf8", timeout: 60_000 });
		assert.ifError(r.error);
		assert.equal(r.status, 0, `${r.stdout}\n${r.stderr}`);
		assert.match(r.stdout, /sandboxed bash in a real pi session: passed/);
		assert.deepEqual(fake.runs(), [{ args: wrapped(fake.bin, cwd, TRICKY).args, cwd }]);
	});

	test("rule 4: with the mode on where the sandbox cannot run, the three tools are refused with the reason and the fix, and nothing runs", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), CANNOT);
		const d = project(t);
		const { pi, tools, fire } = fakePi();
		const sandbox = wireSandbox(pi, { jhon: fake.bin });
		const out = scratch(t, "jhon-sbx-jobs-");
		registerBackgroundTasks(pi as any, { env: {}, outputDir: () => out, sandbox: sandbox.jobOperations });
		await fire("session_start", {}, ctxAt(d));
		const refusal = PINNED_CANNOT;
		for (const tool of ["bash", "bash_background", "monitor"]) assert.equal(await sandbox.gate(tool, d), refusal, tool);
		// Tools that run no command of the model's are not its business.
		for (const tool of ["read", "write", "task_stop", "m365_read"]) assert.equal(await sandbox.gate(tool, d), undefined, tool);
		// Past the gate the job tools still refuse, and start no job.
		await assert.rejects(tools.get("bash_background").execute("c1", { command: "echo ran > ran.txt" }, undefined, undefined, ctxAt(d)), { message: refusal });
		await assert.rejects(tools.get("monitor").execute("c2", { command: "echo ran > ran.txt", timeout_seconds: 5 }, undefined, undefined, ctxAt(d)), { message: refusal });
		assert.equal(textOf(await tools.get("task_list").execute("c3", {}, undefined, undefined, ctxAt(d))), "No background tasks in this session.");
		assert.equal(existsSync(join(d, "ran.txt")), false);
		assert.deepEqual(fake.runs(), []);
		assert.equal(tools.has("bash"), false, "no sandboxed bash is registered where the sandbox cannot run");
	});

	test("rule 4b (L1383): a probe that timed out is no verdict: the three tools run their command through `jhon sandbox run`; without timed_out the same answer refuses", { skip: !bash && "needs /bin/bash" }, async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), TIMED_OUT);
		const d = project(t);
		assert.deepEqual(await readSandboxStatus({ jhon: fake.bin }, d), { mode: "on", canRun: false, reason: TIMED_OUT.reason, fix: TIMED_OUT.fix, project: d, timedOut: true });
		const { pi, tools, fire } = fakePi();
		const sandbox = wireSandbox(pi, { jhon: fake.bin });
		await fire("session_start", {}, ctxAt(d));
		for (const tool of ["bash", "bash_background", "monitor"]) assert.equal(await sandbox.gate(tool, d), undefined, tool);
		assert.ok(tools.has("bash"), "the sandboxed bash is registered");
		const result = await tools.get("bash").execute("c1", { command: TRICKY }, undefined, undefined, ctxAt(d));
		assert.equal(textOf(result), `it's|a "b"|$HOME|done\n`);
		assert.deepEqual(fake.runs(), [{ args: wrapped(fake.bin, d, TRICKY).args, cwd: d }]);
		// The same answer without timed_out (or with it false) still refuses, and nothing runs.
		for (const status of [{ ...TIMED_OUT, timed_out: undefined }, { ...TIMED_OUT, timed_out: false }]) {
			const other = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), status);
			const host = fakePi();
			const refused = wireSandbox(host.pi, { jhon: other.bin });
			await host.fire("session_start", {}, ctxAt(d));
			assert.match((await refused.gate("bash", d)) ?? "", /^jhon: refused\. This session started with sandbox\.mode on and keeps it until it ends\. When it started, the sandbox could not run here: bubblewrap did not start a sandbox within 5s/);
			assert.equal(host.tools.has("bash"), false);
			assert.deepEqual(other.runs(), []);
		}
	});

	test("rule 4: no answer, no project field, or no repository refuses too; a session with no jhon to ask is left to the guards", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t), bare = scratch(t, "jhon-sbx-bare-");
		fake.set({ fails: "settings.toml is not readable" });
		const unread = wireSandbox(fakePi().pi, { jhon: fake.bin });
		assert.equal(await unread.gate("bash", d), "jhon: `jhon sandbox status --json` gave no answer (jhon sandbox: settings.toml is not readable), so whether this command runs in the sandbox is unknown; the command is refused, never run unsandboxed in silence; run `jhon doctor`");
		assert.throws(() => unread.jobOperations(undefined), /gave no answer/);
		// An engine that names no project: never run unpinned.
		const { project: _dropped, ...noProject } = ON;
		assert.deepEqual(sandboxVerdict({ mode: "on", canRun: true, reason: "", fix: "", project: undefined }), { refuse: "jhon: sandbox.mode is on and `jhon sandbox status --json` names no project to pin, so the command is refused, never run unpinned. Fix: run `jhon sync`" });
		fake.set(noProject);
		assert.match((await wireSandbox(fakePi().pi, { jhon: fake.bin }).gate("monitor", d))!, /names no project to pin/);
		// A session in no repository: "" is no project.
		fake.set(ON);
		assert.equal(await wireSandbox(fakePi().pi, { jhon: fake.bin }).gate("bash", bare), "jhon: sandbox.mode is on and this session is in no git repository, so there is no project to pin and the command is refused, never run unpinned. Fix: start the session inside a repository (the owner may run `git init` here and start it again)");
		// The same answers with the mode off run as before.
		assert.deepEqual(sandboxVerdict({ mode: "off", canRun: false, reason: "x", fix: "y", project: "" }), { run: "plain" });
		const none = wireSandbox(fakePi().pi, { jhon: undefined });
		assert.equal(await none.gate("bash", d), undefined);
		assert.equal(none.jobOperations(undefined), undefined);
		assert.deepEqual(fake.runs(), []);
	});

	test("T258: a refusal names the session's pin, and what a new session would pick up when that differs; the setting is read again only for a refusal, and never lets the command run", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const bare = scratch(t, "jhon-sbx-bare-");
		const { pi, tools, fire } = fakePi();
		let clock = 0;
		const sandbox = wireSandbox(pi, { jhon: fake.bin, env: {}, clock: () => clock });
		await fire("session_start", {}, ctxAt(bare));
		// The owner sets sandbox.mode off mid-session: the pin still refuses, and says why.
		fake.set(OFF);
		for (const tool of ["bash", "bash_background", "monitor"]) assert.equal(await sandbox.gate(tool, bare), PINNED_NOW_OFF_NO_REPOSITORY, tool);
		assert.deepEqual(await sandbox.startArgv("git", ["status"], bare), { refuse: PINNED_NOW_OFF_NO_REPOSITORY }, "inspect");
		assert.throws(() => sandbox.jobOperations(undefined), { message: PINNED_NOW_OFF_NO_REPOSITORY }, "a job past the gate: the line the gate just gave");
		assert.deepEqual(fake.asks().map((c) => c.args), Array(2).fill(["sandbox", "status", "--json"]), "asked at the start, then once for refusals that came within the reuse window");
		assert.equal(tools.has("bash"), false, "the read did not register a bash");
		// Within the window the read is reused: a change the owner makes now is read once the window has passed.
		fake.set(ON);
		clock += SANDBOX_READ_REUSE_MS - 1;
		assert.equal(await sandbox.gate("bash", bare), PINNED_NOW_OFF_NO_REPOSITORY, "still the read of a moment ago");
		assert.equal(fake.asks().length, 2);
		clock += 1;
		// The setting still says on: the pin is named, nothing more.
		assert.equal(await sandbox.gate("bash", bare), PINNED_NO_REPOSITORY);
		assert.equal(fake.asks().length, 3);
		// The setting says off again, then cannot be read: today's line, from the gate and from a job past it.
		fake.set(OFF);
		clock += SANDBOX_READ_REUSE_MS;
		assert.equal(await sandbox.gate("bash", bare), PINNED_NOW_OFF_NO_REPOSITORY);
		fake.set({ fails: "settings.toml is not readable" });
		clock += SANDBOX_READ_REUSE_MS;
		assert.equal(await sandbox.gate("bash", bare), NO_REPOSITORY_TODAY);
		assert.throws(() => sandbox.jobOperations(undefined), { message: NO_REPOSITORY_TODAY }, "a failed read leaves no earlier 'says off' behind");
		assert.deepEqual(fake.runs(), [], "nothing ran");
		// A sandbox that cannot run: its reason, and its own fix after the new session; the mode off, or a sandbox that can run now.
		const d = project(t);
		for (const [now, change] of [[OFF, "the setting says off now"], [ON, "the sandbox can run here now"], [CANNOT, undefined]] as const) {
			const cannot = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), CANNOT);
			const host = fakePi();
			const wiring = wireSandbox(host.pi, { jhon: cannot.bin, env: {} });
			await host.fire("session_start", {}, ctxAt(d));
			cannot.set(now);
			assert.equal(await wiring.gate("bash", d), change ? `${KEPT}; ${change}, which a new session picks up. ${WAS_CANNOT} Fix: ask the owner to start a new session (or ${BUBBLEWRAP_FIX}).` : PINNED_CANNOT, String(change));
			assert.equal(host.tools.has("bash"), false);
			assert.deepEqual(cannot.runs(), []);
		}
		// A folder that is a repository now (`git init` mid-session): a new session pins it.
		fake.set(ON);
		const later = scratch(t, "jhon-sbx-bare-");
		const inited = wireSandbox(fakePi().pi, { jhon: fake.bin, env: {} });
		await inited.start(later);
		mkdirSync(join(later, ".git"));
		assert.equal(await inited.gate("bash", later), `${KEPT}; there is a project to pin now (${later}), which a new session picks up. ${WAS_NO_REPOSITORY} Fix: ask the owner to start a new session (or ${REPOSITORY_FIX}).`);
		// A command that may run reads nothing again.
		const ok = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const passing = wireSandbox(fakePi().pi, { jhon: ok.bin, env: {} });
		await passing.start(d);
		for (const tool of ["bash", "bash_background", "monitor", "bash"]) assert.equal(await passing.gate(tool, d), undefined, tool);
		assert.equal(ok.asks().length, 1);
	});

	test("T258: a session in no git repository says once, at its start, that commands will be refused and the two ways out", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const bare = scratch(t, "jhon-sbx-bare-"), d = project(t);
		const NOTE = "Jhon: no git repository here, so with sandbox.mode on every agent command (bash, bash_background, monitor, inspect) will be refused. Start pi inside a repository, or run `jhon settings set sandbox.mode off` and start a new session.";
		const session = async (status: Status, cwd: string, env: NodeJS.ProcessEnv = {}) => {
			fake.set(status);
			const notes: unknown[] = [];
			const { pi, fire } = fakePi();
			const sandbox = wireSandbox(pi, { jhon: fake.bin, env });
			const ctx = { ...ctxAt(cwd), ui: { setStatus: () => {}, notify: (text: string, level: string) => notes.push([text, level]) } };
			await fire("session_start", {}, ctx);
			return { notes, sandbox };
		};
		const bareOn = await session(ON, bare);
		assert.deepEqual(bareOn.notes, [[NOTE, "warning"]]);
		// Said once: the refusals that follow add no note.
		await bareOn.sandbox.gate("bash", bare);
		await bareOn.sandbox.gate("monitor", bare);
		assert.equal(bareOn.notes.length, 1);
		assert.deepEqual((await session(TIMED_OUT, bare)).notes, [[NOTE, "warning"]], "a probe that timed out pins as on");
		// Any other session says nothing at its start.
		assert.deepEqual((await session(ON, d)).notes, [], "in a repository");
		assert.deepEqual((await session(OFF, bare)).notes, [], "the mode off");
		assert.deepEqual((await session(CANNOT, bare)).notes, [], "a sandbox that cannot run: its reason is the status bar's");
		assert.deepEqual((await session(ON, bare, { JHON_PI_CHILD: "1" })).notes, [], "a sub-agent: its parent said it");
		assert.deepEqual(fake.runs(), []);
	});

	test("T258: the second read never turns a refusal into a pass: a mode off, a sandbox that can run, another project, or no answer at all leave the command refused", async (t) => {
		const NO_REPOSITORY_TODAY = "jhon: sandbox.mode is on and this session is in no git repository, so there is no project to pin and the command is refused, never run unpinned. Fix: start the session inside a repository (the owner may run `git init` here and start it again)";
		const answers: [string, (f: Fake) => void, string][] = [
			["the mode off", (f) => f.set(OFF), PINNED_NOW_OFF_NO_REPOSITORY],
			["a sandbox that can run in another project", (f) => f.set({ ...ON, project: "/elsewhere" }), `${KEPT}; there is a project to pin now (/elsewhere), which a new session picks up. ${WAS_NO_REPOSITORY} Fix: ask the owner to start a new session (or ${REPOSITORY_FIX}).`],
			["malformed JSON", (f) => writeFileSync(f.bin, "#!/bin/sh\nprintf '{not json'\n"), NO_REPOSITORY_TODAY],
			["an empty body", (f) => writeFileSync(f.bin, "#!/bin/sh\nexit 0\n"), NO_REPOSITORY_TODAY],
			["a non-zero exit", (f) => writeFileSync(f.bin, "#!/bin/sh\nexit 3\n"), NO_REPOSITORY_TODAY],
			["a hang past the timeout", (f) => writeFileSync(f.bin, "#!/bin/sh\nexec sleep 30\n"), NO_REPOSITORY_TODAY],
			["no jhon", (f) => rmSync(f.bin), NO_REPOSITORY_TODAY],
		];
		// The session's own bound on each read (T264, L1745): every answer but
		// the hang must come within it, so the fake answers from a shell
		// script (shellStatusFakeAt); a 0.5 s bound on node's start lost the
		// session start's read on a loaded machine.
		const timeoutMs = HOOK_TIMEOUT_MS;
		for (const [name, answer, said] of answers) {
			const bare = scratch(t, "jhon-sbx-bare-");
			const fake = shellStatusFakeAt(scratch(t, "jhon-sbx-fake-"), bare, ON);
			const env: NodeJS.ProcessEnv = {};
			const { pi, tools, fire } = fakePi();
			const sandbox = wireSandbox(pi, { jhon: fake.bin, env, timeoutMs });
			await fire("session_start", {}, ctxAt(bare));
			answer(fake);
			const began = Date.now();
			for (const tool of ["bash", "bash_background", "monitor"]) assert.equal(await sandbox.gate(tool, bare), said, `${name}: ${tool}`);
			assert.ok(Date.now() - began < 3 * timeoutMs + 1500, `${name}: each read bounded by the timeout`);
			assert.deepEqual(await sandbox.startArgv("git", ["status"], bare), { refuse: said }, `${name}: inspect`);
			assert.throws(() => sandbox.jobOperations(undefined), `${name}: a job`);
			assert.equal(tools.has("bash"), false, `${name}: no bash registered`);
			assert.equal(env.JHON_SANDBOX_PROJECT, undefined, `${name}: the read pinned no project`);
			assert.deepEqual(fake.runs(), [], `${name}: nothing ran`);
		}
	});

	test("T258: a command that may run reads the status only at the start; each refusal reads it once", async (t) => {
		for (const status of [ON, OFF, TIMED_OUT]) {
			const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), status);
			const d = project(t);
			const sandbox = wireSandbox(fakePi().pi, { jhon: fake.bin, env: {} });
			await sandbox.start(d);
			for (const tool of ["bash", "bash_background", "monitor"]) assert.equal(await sandbox.gate(tool, d), undefined, tool);
			assert.ok(!("refuse" in await sandbox.startArgv("git", ["status"], d)));
			sandbox.jobOperations(undefined);
			assert.equal(fake.asks().length, 1, `${status.mode}${status.timed_out ? " timed out" : ""}: asked at the start only`);
		}
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const bare = scratch(t, "jhon-sbx-bare-");
		const { pi, fire } = fakePi();
		const sandbox = wireSandbox(pi, { jhon: fake.bin, env: {} });
		await fire("session_start", {}, ctxAt(bare));
		await Promise.all(Array.from({ length: 4 }, () => sandbox.gate("bash", bare)));
		assert.equal(fake.asks().length, 1 + 1, "the start, then one read the four refusals share");
	});

	test("rule 4: a session pinned by its first call, not by a session start, never runs that bash call unwrapped", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t);
		const { pi, tools } = fakePi();
		const sandbox = wireSandbox(pi, { jhon: fake.bin });
		assert.equal(await sandbox.gate("bash", d), "jhon: the sandbox was pinned by this call, after the session started, so this command did not run; run it again");
		assert.ok(tools.has("bash"));
		assert.equal(await sandbox.gate("bash", d), undefined);
		assert.equal(fake.asks().length, 1);
	});

	/** Jhon's whole extension on a fake pi, its guards and its sandbox answered by the fake jhon. */
	function extension(t: TestContext, fake: Fake, env: Record<string, string> = {}) {
		useMemorySettings(t);
		const agentDir = scratch(t, "jhon-sbx-agentdir-");
		writeFileSync(join(agentDir, CONFIG_FILE), JSON.stringify({ jhon: fake.bin, orchestrator: join(agentDir, "orchestrator.md"), vcsDirs: [".git"], protected: [], inspectAllow: [] }));
		const keys = ["PI_CODING_AGENT_DIR", "CLAUDE_CONFIG_DIR", "JHON_PI_CHILD", "JHON_PI_TOOLS", "JHON_PI_TASK_FILE", "JHON_PI_MAX_TURNS", "JHON_SANDBOX_PROJECT"];
		const saved = keys.map((k) => [k, process.env[k]] as const);
		for (const k of keys.slice(2)) delete process.env[k];
		Object.assign(process.env, { PI_CODING_AGENT_DIR: agentDir, CLAUDE_CONFIG_DIR: join(agentDir, "claude"), ...env });
		const host = fakePi();
		try { jhon(host.pi); } finally { for (const [k, v] of saved) if (v === undefined) delete process.env[k]; else process.env[k] = v; }
		return { ...host, agentDir };
	}

	test("rule 5: the guards judge the original command first; the sandbox answers only for what they let through", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), CANNOT);
		const d = project(t);
		const { fire } = extension(t, fake);
		await fire("session_start", {}, ctxAt(d));
		for (const toolName of ["bash", "bash_background", "monitor"]) {
			// Refused by a guard: its reason, as with the sandbox off.
			assert.deepEqual(await fire("tool_call", { toolName, toolCallId: "c1", input: { command: "echo GUARD-REFUSES" } }, ctxAt(d)), { block: true, reason: "safety-guard: refused: GUARD-REFUSES" }, toolName);
			// Let through by the guards: the sandbox's refusal.
			const r = await fire("tool_call", { toolName, toolCallId: "c2", input: { command: "echo fine" } }, ctxAt(d));
			assert.match(r?.reason ?? "", /^jhon: refused\. This session started with sandbox\.mode on and keeps it until it ends\. When it started, the sandbox could not run here: bubblewrap is not installed\. Fix: install bubblewrap/, toolName);
		}
		// The guard read the command as the model wrote it, never a wrapped one.
		assert.equal(fake.calls().some((c) => c.args[0] === "hook" && c.args.includes("sandbox")), false);
		assert.deepEqual(fake.runs(), []);
		// A file tool and m365_read are not the sandbox's.
		assert.equal(await fire("tool_call", { toolName: "m365_read", toolCallId: "c3", input: { query: "x" } }, ctxAt(d)), undefined);
	});

	test("rule 5: with the mode on, a guard's refusal stops the call before any sandbox runs, and an allowed bash passes the gate", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t);
		const { fire, tools } = extension(t, fake);
		await fire("session_start", {}, ctxAt(d));
		assert.deepEqual(await fire("tool_call", { toolName: "bash", toolCallId: "c1", input: { command: "echo GUARD-REFUSES" } }, ctxAt(d)), { block: true, reason: "safety-guard: refused: GUARD-REFUSES" });
		assert.equal(await fire("tool_call", { toolName: "bash", toolCallId: "c2", input: { command: "echo fine" } }, ctxAt(d)), undefined);
		assert.ok(tools.has("bash"));
		assert.deepEqual(fake.runs(), []);
	});

	/** A program that prints its own argv, one `|` after each word: what `inspect` started, and with which arguments. */
	function argvPrinter(t: TestContext): string {
		const file = join(scratch(t, "jhon-sbx-argv-"), "show-argv");
		writeFileSync(file, `#!/bin/sh\nprintf '%s|' "$@"\n`);
		chmodSync(file, 0o755);
		return file;
	}

	test("inspect: with the mode on its program starts as `jhon sandbox run --project <pinned> --profile work -- <program> <args>`, after the inspect guard", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t);
		const sub = join(d, "pkg");
		mkdirSync(sub);
		const show = argvPrinter(t);
		const { fire, tools } = extension(t, fake);
		await fire("session_start", {}, ctxAt(sub));
		const result = await tools.get("inspect").execute("c1", { command: `${show} log  --oneline -3` }, undefined, undefined, ctxAt(sub));
		assert.equal(textOf(result), "log|--oneline|-3|");
		assert.deepEqual(fake.runs(), [{ args: ["sandbox", "run", "--project", d, "--profile", "work", "--", show, "log", "--oneline", "-3"], cwd: sub }]);
		// The guard read the command as the model wrote it, before anything started.
		const guards = fake.calls().filter((c) => c.args[0] === "hook");
		assert.deepEqual(guards.map((c) => c.args), [["hook", "inspect-guard"]]);
		await assert.rejects(tools.get("inspect").execute("c2", { command: `${show} GUARD-REFUSES` }, undefined, undefined, ctxAt(sub)), { message: "safety-guard: refused: GUARD-REFUSES" });
		assert.equal(fake.runs().length, 1, "a command the guard refused starts no sandbox");
	});

	test("inspect: with the mode on where the sandbox cannot run, or with no answer to pin, it is refused with bash's line and nothing runs", async (t) => {
		const d = project(t);
		const made = join(d, "made-by-inspect");
		for (const [status, refusal] of [
			[CANNOT, PINNED_CANNOT],
			[{ fails: "settings.toml: broken" }, "jhon: `jhon sandbox status --json` gave no answer (jhon sandbox: settings.toml: broken), so whether this command runs in the sandbox is unknown; the command is refused, never run unsandboxed in silence; run `jhon doctor`"],
		] as const) {
			const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
			fake.set(status);
			const { fire, tools } = extension(t, fake);
			await fire("session_start", {}, ctxAt(d));
			await assert.rejects(tools.get("inspect").execute("c1", { command: `touch ${made}` }, undefined, undefined, ctxAt(d)), { message: refusal });
			assert.deepEqual(fake.runs(), []);
			assert.equal(existsSync(made), false, "the program did not run");
			// The same line the gate gives bash.
			assert.equal((await fire("tool_call", { toolName: "bash", toolCallId: "c2", input: { command: "echo fine" } }, ctxAt(d)))?.reason, refusal);
		}
	});

	test("inspect: with the mode off its program starts as before, the argv its whitespace split gives, and no sandbox runs", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), OFF);
		const d = project(t);
		const show = argvPrinter(t);
		const { fire, tools } = extension(t, fake);
		await fire("session_start", {}, ctxAt(d));
		const result = await tools.get("inspect").execute("c1", { command: ` ${show} log  --oneline -3 ` }, undefined, undefined, ctxAt(d));
		assert.equal(textOf(result), "log|--oneline|-3|");
		assert.deepEqual(fake.runs(), []);
		// What the binary was asked: the session's one status, and the inspect guard with the command as written.
		assert.deepEqual(fake.calls().filter((c) => c.args[0] !== "contract" && c.args[0] !== "settings").map((c) => c.args), [["sandbox", "status", "--json"], ["hook", "inspect-guard"]]);
		await assert.rejects(tools.get("inspect").execute("c2", { command: join(d, "no-such-program") }, undefined, undefined, ctxAt(d)), { message: /^inspect: could not run .*no-such-program: / });
	});

	test("rule 5: a sub-agent is a child pi that loads this extension; it pins its parent's project, or with none to inherit the folder it runs in (E5)", { skip: !bash && "needs /bin/bash" }, async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		// Two checkouts: the parent's, and the worktree a child was started in (its `cwd`).
		const main = project(t), worktree = project(t);
		for (const [folder, inherits, pins] of [[main, undefined, main], [worktree, undefined, worktree], [worktree, main, main]] as const) {
			const child = extension(t, fake, { JHON_PI_CHILD: "1", ...(inherits ? { JHON_SANDBOX_PROJECT: inherits } : {}) });
			await child.fire("session_start", { reason: "startup" }, ctxAt(folder));
			assert.equal(child.tools.has("bash_background"), false, "a child has no background tools");
			assert.equal(await child.fire("tool_call", { toolName: "bash", toolCallId: "c1", input: { command: "echo child" } }, ctxAt(folder)), undefined);
			await child.tools.get("bash").execute("c1", { command: "echo child" }, undefined, undefined, ctxAt(folder));
			assert.deepEqual(fake.runs().at(-1), { args: wrapped(fake.bin, pins, "echo child").args, cwd: folder });
			delete process.env.JHON_SANDBOX_PROJECT;
		}
		assert.deepEqual(fake.asks(), [
			{ args: ["sandbox", "status", "--json"], cwd: main }, { args: ["sandbox", "status", "--json"], cwd: worktree },
			{ args: ["sandbox", "status", "--json", "--project", main], cwd: worktree },
		], "each child asked once: in its own folder, and for its parent's project where it inherits one");
	});

	test("the mode off changes nothing but the agent's marker: background jobs and monitors start and stop as before, and no sandbox runs", { skip: !bash && "needs /bin/bash" }, async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), OFF);
		const d = project(t);
		const { pi, tools, fire } = fakePi();
		const sandbox = wireSandbox(pi, { jhon: fake.bin });
		const out = scratch(t, "jhon-sbx-jobs-");
		registerBackgroundTasks(pi as any, { env: {}, outputDir: () => out, sandbox: sandbox.jobOperations });
		await fire("session_start", {}, ctxAt(d));
		assert.equal(tools.has("bash"), true, "bash carries the agent's marker (T219 S9)");
		await tools.get("bash_background").execute("c1", { command: "echo started; sleep 30" }, undefined, undefined, ctxAt(d));
		await tools.get("monitor").execute("c2", { command: "echo watched; sleep 30", timeout_seconds: 60 }, undefined, undefined, ctxAt(d));
		await waitFor(() => existsSync(join(out, "job-2.log")) && readFileSync(join(out, "job-1.log"), "utf8") === "started\n" && readFileSync(join(out, "job-2.log"), "utf8") === "watched\n", "both ran");
		for (const id of ["job-1", "job-2"]) assert.match(textOf(await tools.get("task_stop").execute("c3", { id }, undefined, undefined, ctxAt(d))), /stopped/i);
		assert.match(textOf(await tools.get("task_list").execute("c4", {}, undefined, undefined, ctxAt(d))), /job-1.*stopped[\s\S]*job-2.*stopped/);
		assert.deepEqual(fake.runs(), []);
	});

	test("a job the owner starts spawns as it did before with the mode on: the configured shell with `-c <command>`, and no sandbox run", { skip: !bash && "needs /bin/bash" }, async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), OFF);
		const d = project(t);
		// pi's configured shell, as a program that logs its own argv before it runs bash with it.
		const shellDir = scratch(t, "jhon-sbx-shell-");
		const shell = join(shellDir, "logging-bash"), log = join(shellDir, "spawns.log");
		writeFileSync(shell, `#!/usr/bin/env node\nconst fs = require("node:fs"), cp = require("node:child_process");\nfs.appendFileSync(${JSON.stringify(log)}, JSON.stringify(process.argv.slice(1)) + "\\n");\nconst r = cp.spawnSync("/bin/bash", process.argv.slice(2), { stdio: "inherit" });\nprocess.exit(r.status === null ? 1 : r.status);\n`);
		chmodSync(shell, 0o755);
		const settingsFile = join(process.env.PI_CODING_AGENT_DIR!, "settings.json");
		mkdirSync(dirname(settingsFile), { recursive: true });
		writeFileSync(settingsFile, JSON.stringify({ shellPath: shell }));
		t.after(() => rmSync(settingsFile, { force: true }));
		const spawns = (): string[][] => (existsSync(log) ? readFileSync(log, "utf8") : "").split("\n").filter(Boolean).map((l) => JSON.parse(l));

		const ownerRun = async (status: Status) => {
			fake.set(status);
			const { pi, tools, fire } = fakePi();
			const sandbox = wireSandbox(pi, { jhon: fake.bin });
			const out = scratch(t, "jhon-sbx-jobs-");
			const background = registerBackgroundTasks(pi as any, { env: {}, outputDir: () => out, sandbox: sandbox.jobOperations })!;
			await fire("session_start", {}, ctxAt(d));
			const before = spawns().length;
			let ended: any;
			const job = background.startOwnerCommand(TRICKY, d, "main", "tests of example-change (you)", (j) => { ended = j; });
			await waitFor(() => ended !== undefined, "the owner's command ended");
			assert.equal(ended.exitCode, 0);
			assert.equal(readFileSync(job.outputPath, "utf8"), `it's|a "b"|$HOME|done\n`);
			return { spawned: spawns().slice(before), tools, out };
		};
		const off = await ownerRun(OFF);
		assert.deepEqual(off.spawned, [[shell, "-c", TRICKY]], "before: the configured shell, `-c`, the command");
		const on = await ownerRun(ON);
		assert.deepEqual(on.spawned, off.spawned, "the same program and arguments with the mode on");
		assert.deepEqual(fake.runs(), [], "no sandbox run for the owner's command");
		// The same session's agent command is the one that is wrapped, with that same shell inside.
		await on.tools.get("bash_background").execute("c1", { command: "echo agent" }, undefined, undefined, ctxAt(d));
		await waitFor(() => existsSync(join(on.out, "job-2.log")) && readFileSync(join(on.out, "job-2.log"), "utf8") === "agent\n", "the agent's job ran");
		assert.deepEqual(fake.runs().map((c) => c.args), [["sandbox", "run", "--project", d, "--profile", "work", "--", shell, "-c", "echo agent"]]);
	});

	// T155 (L1394): a foreground wait runs, and its result carries the note (refused before).
	test("the foreground-sleep note still holds with the sandbox on", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t);
		const { pi, fire } = fakePi();
		const sandbox = wireSandbox(pi, { jhon: fake.bin });
		registerBackgroundTasks(pi as any, { env: {}, outputDir: () => d, sandbox: sandbox.jobOperations });
		await fire("session_start", {}, ctxAt(d));
		const r = await fire("tool_call", { toolName: "bash", toolCallId: "w1", input: { command: "sleep 60" } }, ctxAt(d));
		assert.equal(r?.block, undefined, JSON.stringify(r));
		const result = await fire("tool_result", { toolName: "bash", toolCallId: "w1", input: { command: "sleep 60" }, content: [{ type: "text", text: "out" }], isError: false }, ctxAt(d));
		assert.match(textOf(result), /^out\nForeground wait: `sleep 60` held the turn until it ended\./);
	});

	// T128f (L1352 F1): pi reads the engine's effective mode, on by default on Linux and off elsewhere (D3);
	// on Linux only where bubblewrap can run (T128f slice 8, L1375): a CI runner whose AppArmor stops it is off.
	test("with an empty settings file the tree's jhon gives pi the default mode: on on Linux where the sandbox can run", async (t) => {
		const bin = process.env.JHON_TEST_BIN || join(REPO, "build", "jhon");
		if (!existsSync(bin)) { t.skip(`needs the tree's jhon (JHON_TEST_BIN or build/jhon): ${bin}`); return; }
		const d = scratch(t, "jhon-sbx-default-");
		const file = join(d, "settings.toml");
		const was = process.env.JHON_SETTINGS;
		t.after(() => { process.env.JHON_SETTINGS = was; });
		writeFileSync(file, '[sandbox]\nmode = "on"\n');
		process.env.JHON_SETTINGS = file;
		const probe = await readSandboxStatus({ jhon: bin }, d);
		const canRun = !("unread" in probe) && probe.canRun;
		const want = process.platform === "linux" && canRun ? "on" : "off";
		for (const [text, mode] of [["", want], ['[sandbox]\nmode = "off"\n', "off"], ['[sandbox]\nmode = "on"\n', "on"]]) {
			writeFileSync(file, text);
			process.env.JHON_SETTINGS = file;
			const pin = await readSandboxStatus({ jhon: bin }, d);
			assert.equal("unread" in pin ? pin.unread : pin.mode, mode, JSON.stringify(text));
		}
	});

	test("rule 6: an abort, the timeout and a job's stop end the program inside a real bubblewrap sandbox", async (t) => {
		const bin = process.env.JHON_TEST_BIN || join(REPO, "build", "jhon");
		if (process.platform !== "linux" || !existsSync(bin) || !bash) { t.skip(`needs Linux, /bin/bash and the tree's jhon (JHON_TEST_BIN or build/jhon): ${bin}`); return; }
		const d = scratch(t, "jhon-sbx-real-");
		spawnSync("git", ["init", "-q"], { cwd: d });
		// jhon sandbox run refuses a settings file a sandboxed command could
		// rewrite, and /tmp and $TMPDIR are writable inside: the isolated file
		// (under tmpdir()) would stop every run (exit 126). This run's file
		// lives in the tree's build folder, outside the scratch project.
		mkdirSync(join(REPO, "build"), { recursive: true });
		const settingsDir = mkdtempSync(join(REPO, "build", "jhon-sbx-settings-"));
		// The state folder too (XDG_STATE_HOME, under tmpdir() in the isolated
		// env): jhon sandbox run refuses one a sandboxed command could move.
		const settingsWas = process.env.JHON_SETTINGS, stateWas = process.env.XDG_STATE_HOME;
		t.after(() => { process.env.JHON_SETTINGS = settingsWas; process.env.XDG_STATE_HOME = stateWas; rmSync(settingsDir, { recursive: true, force: true }); });
		writeFileSync(join(settingsDir, "settings.toml"), '[sandbox]\nmode = "off"\n');
		mkdirSync(join(settingsDir, "state"));
		process.env.JHON_SETTINGS = join(settingsDir, "settings.toml");
		process.env.XDG_STATE_HOME = join(settingsDir, "state");
		const pin = await readSandboxStatus({ jhon: bin }, d);
		if ("unread" in pin || !pin.canRun) { t.skip(`bubblewrap cannot run here, so stopping a sandboxed program is not proven on this machine: ${"unread" in pin ? pin.unread : `${pin.reason}; ${pin.fix}`}`); return; }
		const operations = sandboxOperations({ jhon: bin, project: d });
		// The shell's pid and its child's, written from inside; GH_TOKEN is unset only inside the sandbox.
		const program = (name: string) => `echo "token:\${GH_TOKEN:-unset}"; sleep 300 & echo "$$ $!" > ${name}.pids; wait`;
		const pids = (name: string) => readFileSync(join(d, `${name}.pids`), "utf8").trim().split(" ").map(Number);
		const started = (name: string) => waitFor(() => existsSync(join(d, `${name}.pids`)) && pids(name).length === 2, `${name}: the sandboxed program started`);
		const alive = (pid: number) => { try { process.kill(pid, 0); return true; } catch { return false; } };
		const gone = (name: string) => waitFor(() => pids(name).every((pid) => !alive(pid)), `${name}: the sandboxed shell and its sleep ended`);
		const env = { ...process.env, GH_TOKEN: "not-a-token" };

		let output = "";
		const abort = new AbortController();
		const aborted = operations.exec(program("abort"), d, { onData: (b) => { output += b; }, signal: abort.signal, env });
		await started("abort");
		assert.ok(pids("abort").every(alive));
		// jhon sandbox run copies the program's output through: the pid file can
		// show before the echo reached this side, and the abort kills the copy.
		await waitFor(() => output.length > 0, "abort: the sandboxed program's first line arrived");
		assert.equal(output, "token:unset\n", "the program ran inside the sandbox");
		abort.abort();
		await assert.rejects(aborted, { message: "aborted" });
		await gone("abort");
		assert.equal(output, "token:unset\n", "nothing more came after the abort");

		const timed = operations.exec(program("timeout"), d, { onData: () => {}, timeout: 2, env });
		await started("timeout");
		await assert.rejects(timed, { message: "timeout:2" });
		await gone("timeout");

		const out = scratch(t, "jhon-sbx-jobs-");
		const registry = createTaskRegistry({ outputDir: () => out, now: () => Date.now(), shell: () => ({ operations }), runAgent: async () => ({ ok: true, text: "" }), onSettled: () => {} });
		const job = registry.start({ command: program("job"), cwd: d, ownerSessionId: "main" });
		await started("job");
		assert.equal(registry.stop(job.id)?.status, "stopped");
		await gone("job");
		await registry.stopAll();
	});
}

/** In a child process: a real pi session with and without the sandbox wiring. */
async function proveInRealPi(): Promise<void> {
	const { createAgentSessionFromServices, createAgentSessionRuntime, createAgentSessionServices, ModelRuntime, SessionManager, SettingsManager } = await import("@earendil-works/pi-coding-agent");
	const cwd = process.cwd();
	const agentDir = process.env.PI_CODING_AGENT_DIR!;
	const modelRuntime = await ModelRuntime.create({ authPath: join(agentDir, "empty-auth.json"), modelsPath: null, modelsStorePath: join(agentDir, "models-store.json"), allowModelNetwork: false });
	const seen: Record<string, string> = {};
	for (const sandboxed of [false, true]) {
		const runtime = await createAgentSessionRuntime(async ({ cwd, sessionManager, sessionStartEvent }) => {
			const services = await createAgentSessionServices({ cwd, agentDir, modelRuntime,
				settingsManager: SettingsManager.inMemory({ retry: { enabled: false }, compaction: { enabled: false } }),
				resourceLoaderOptions: { noExtensions: true, noSkills: true, noPromptTemplates: true, noThemes: true, noContextFiles: true,
					additionalExtensionPaths: sandboxed ? [fileURLToPath(new URL("./fixtures/sandbox-extension.ts", import.meta.url))] : [] },
			});
			assert.deepEqual(services.resourceLoader.getExtensions().errors, []);
			return { ...(await createAgentSessionFromServices({ services, sessionManager, sessionStartEvent })), services, diagnostics: services.diagnostics };
		}, { cwd, agentDir, sessionManager: SessionManager.inMemory(cwd) });
		try {
			await runtime.session.bindExtensions({ mode: "print" });
			const tools = runtime.session.agent.state.tools;
			// What the model reads: the prompt, and each tool's name, description and schema, in order.
			seen[String(sandboxed)] = JSON.stringify({ prompt: runtime.session.systemPrompt, active: runtime.session.getActiveToolNames(),
				tools: tools.map((tool: any) => ({ name: tool.name, description: tool.description, parameters: tool.parameters })) });
			if (sandboxed) {
				const bash = tools.find((tool: any) => tool.name === "bash")!;
				const result: any = await bash.execute("c1", { command: TRICKY }, new AbortController().signal);
				assert.equal(result.content.map((part: any) => part.text ?? "").join(""), `it's|a "b"|$HOME|done\n`);
			}
		} finally { await runtime.dispose(); }
	}
	assert.ok(seen.false.includes("Execute bash commands") && seen.false.includes('"name":"bash"'), "the plain session declares bash and its prompt line");
	assert.equal(seen.true, seen.false, "the model reads the same bytes with the sandbox on");
	console.log("sandboxed bash in a real pi session: passed");
}

/**
 * L925, E5: one pin per session. A sub-agent is a child pi in a folder the
 * model may choose (the `subagent` tool's `cwd`, a worktree of the session's
 * repository): it uses the project its parent pinned, never its own folder's.
 */
function childSuite(): void {
	const bash = existsSync("/bin/bash");

	test("E5: a session exports the project it pinned, and a sub-agent child pins that one, not its own folder's", { skip: !bash && "needs /bin/bash" }, async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t), worktree = project(t);
		// The parent: what it pinned is what its children and its guards read.
		const parentEnv: NodeJS.ProcessEnv = {};
		const parent = fakePi();
		wireSandbox(parent.pi, { jhon: fake.bin, env: parentEnv });
		await parent.fire("session_start", {}, ctxAt(d));
		assert.deepEqual(fake.asks(), [{ args: ["sandbox", "status", "--json"], cwd: d }]);
		assert.equal(parentEnv.JHON_SANDBOX_PROJECT, d);
		// The child inherits it (childEnv keeps the parent's environment) and starts in the worktree.
		const agent = { name: "worker", description: "", tools: ["bash"], model: "m", body: "", bounded: false } as any;
		const childEnvironment = childEnv(agent, undefined, { ...parentEnv });
		assert.equal(childEnvironment.JHON_SANDBOX_PROJECT, d);
		assert.equal(childEnvironment.JHON_PI_CHILD, "1");
		const child = fakePi();
		const wiring = wireSandbox(child.pi, { jhon: fake.bin, env: childEnvironment });
		await child.fire("session_start", {}, ctxAt(worktree));
		assert.deepEqual(fake.asks()[1], { args: ["sandbox", "status", "--json", "--project", d], cwd: worktree });
		const result = await child.tools.get("bash").execute("c1", { command: "echo child" }, undefined, undefined, ctxAt(worktree));
		assert.equal(textOf(result), "child\n");
		assert.deepEqual(fake.runs(), [{ args: wrapped(fake.bin, d, "echo child").args, cwd: worktree }]);
		assert.deepEqual(await wiring.startArgv("git", ["status"], worktree), { program: fake.bin, args: ["sandbox", "run", "--project", d, "--profile", "work", "--", "git", "status"] });
		assert.equal(fake.asks().length, 2, "the child asked once");
	});

	test("E5: a child whose parent's project is gone is refused, never run in its own folder's", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const worktree = project(t);
		const gone = join(scratch(t, "jhon-sbx-gone-"), "not-there");
		const child = fakePi();
		const wiring = wireSandbox(child.pi, { jhon: fake.bin, env: { JHON_PI_CHILD: "1", JHON_SANDBOX_PROJECT: gone } });
		await child.fire("session_start", {}, ctxAt(worktree));
		assert.deepEqual(fake.asks(), [{ args: ["sandbox", "status", "--json", "--project", gone], cwd: worktree }]);
		const refusal = await wiring.gate("bash", worktree);
		assert.match(refusal ?? "", /^jhon: `jhon sandbox status --json` gave no answer \(jhon sandbox: --project takes an absolute folder that exists: .*not-there\), so whether this command runs in the sandbox is unknown; the command is refused/);
		assert.deepEqual(fake.runs(), []);
	});

	test("E5 preserve: a top-level session asks in its own folder whatever the variable says, and a child with no pin to inherit asks as before", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t), other = project(t);
		const top = fakePi();
		const env: NodeJS.ProcessEnv = { JHON_SANDBOX_PROJECT: other };
		wireSandbox(top.pi, { jhon: fake.bin, env });
		await top.fire("session_start", {}, ctxAt(d));
		assert.deepEqual(fake.asks(), [{ args: ["sandbox", "status", "--json"], cwd: d }]);
		assert.equal(env.JHON_SANDBOX_PROJECT, d, "the session's own pin replaces what it inherited");
		const child = fakePi();
		wireSandbox(child.pi, { jhon: fake.bin, env: { JHON_PI_CHILD: "1" } });
		await child.fire("session_start", {}, ctxAt(other));
		assert.deepEqual(fake.asks()[1], { args: ["sandbox", "status", "--json"], cwd: other });
		// With the mode off nothing is exported: nothing reads a pin.
		fake.set(OFF);
		const offEnv: NodeJS.ProcessEnv = {};
		const off = fakePi();
		wireSandbox(off.pi, { jhon: fake.bin, env: offEnv });
		await off.fire("session_start", {}, ctxAt(d));
		assert.deepEqual(offEnv, {});
	});

	const NO_REPOSITORY = PINNED_NO_REPOSITORY;

	test("G1: a session that ends with no pin takes the earlier session's project out of the environment, and a child started from it is refused", { skip: !bash && "needs /bin/bash" }, async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const first = project(t), bare = scratch(t, "jhon-sbx-bare-");
		const env: NodeJS.ProcessEnv = {};
		const parent = fakePi();
		const wiring = wireSandbox(parent.pi, { jhon: fake.bin, env });
		// The first session pins its project.
		await parent.fire("session_start", {}, ctxAt(first));
		assert.equal(env.JHON_SANDBOX_PROJECT, first);
		// The next session of the same process starts in a folder with no repository: refused.
		await parent.fire("session_start", {}, ctxAt(bare));
		assert.deepEqual(fake.asks(), [{ args: ["sandbox", "status", "--json"], cwd: first }, { args: ["sandbox", "status", "--json"], cwd: bare }]);
		assert.equal(await wiring.gate("bash", bare), NO_REPOSITORY);
		await assert.rejects(parent.tools.get("bash").execute("c1", { command: "echo parent" }, undefined, undefined, ctxAt(bare)), { message: NO_REPOSITORY });
		assert.equal("JHON_SANDBOX_PROJECT" in env, false, `the environment still names ${env.JHON_SANDBOX_PROJECT}`);
		// A child built from that environment has no project to ask for, and is refused in its own folder.
		const agent = { name: "worker", description: "", tools: ["bash"], model: "m", body: "", bounded: false } as any;
		const childEnvironment = childEnv(agent, undefined, { ...env });
		assert.equal("JHON_SANDBOX_PROJECT" in childEnvironment, false);
		const child = fakePi();
		const childWiring = wireSandbox(child.pi, { jhon: fake.bin, env: childEnvironment });
		await child.fire("session_start", {}, ctxAt(bare));
		// T258: the parent's two refusals above shared one read again, to name the pin; then the child's start.
		assert.deepEqual(fake.asks().slice(2), Array(2).fill({ args: ["sandbox", "status", "--json"], cwd: bare }));
		assert.equal(await childWiring.gate("bash", bare), NO_REPOSITORY);
		assert.equal(child.tools.has("bash"), false, "the child registered no sandboxed bash");
		assert.deepEqual(await childWiring.startArgv("git", ["status"], bare), { refuse: NO_REPOSITORY });
		assert.throws(() => childWiring.jobOperations(undefined), { message: NO_REPOSITORY });
		assert.deepEqual(fake.runs(), [], "nothing ran");
		assert.equal(fake.asks().length, 5, "each session start once, and each session's refusals one read more (T258)");
	});

	test("G1: every session start that ends with no pin clears it: the status cannot be read, the sandbox cannot run here, the mode is off", async (t) => {
		const fake = fakeJhonAt(scratch(t, "jhon-sbx-fake-"), ON);
		const d = project(t);
		const env: NodeJS.ProcessEnv = {};
		const top = fakePi();
		const wiring = wireSandbox(top.pi, { jhon: fake.bin, env });
		const shapes: [string, Status | { fails: string }, string | undefined][] = [
			["the status cannot be read", { fails: "cannot read the settings" }, "jhon: `jhon sandbox status --json` gave no answer (jhon sandbox: cannot read the settings), so whether this command runs in the sandbox is unknown; the command is refused, never run unsandboxed in silence; run `jhon doctor`"],
			["the sandbox cannot run here", CANNOT, PINNED_CANNOT],
			["the status names no project", { mode: "on", can_run: true, reason: "", fix: "" }, `${KEPT}. When it started, \`jhon sandbox status --json\` named no project to pin. Fix: run \`jhon sync\`.`],
			["the mode is off", OFF, undefined],
		];
		for (const [name, status, refusal] of shapes) {
			fake.set(ON);
			await top.fire("session_start", {}, ctxAt(d));
			assert.equal(env.JHON_SANDBOX_PROJECT, d, `${name}: the session before it pinned its project`);
			fake.set(status);
			await top.fire("session_start", {}, ctxAt(d));
			assert.equal(await wiring.gate("bash", d), refusal, name);
			assert.equal("JHON_SANDBOX_PROJECT" in env, false, `${name}: the environment still names ${env.JHON_SANDBOX_PROJECT}`);
		}
		assert.deepEqual(fake.runs(), []);
		// While a session start has not answered yet, the earlier pin is not exported either.
		fake.set(ON);
		await top.fire("session_start", {}, ctxAt(d));
		const pending = top.fire("session_start", {}, ctxAt(d));
		assert.equal("JHON_SANDBOX_PROJECT" in env, false, "the earlier pin is exported while the next session's is unknown");
		await pending;
		assert.equal(env.JHON_SANDBOX_PROJECT, d);
	});
}

if (process.env.JHON_TEST_SANDBOX_CHILD === "1") {
	await proveInRealPi();
} else {
	suite();
	childSuite();
}
