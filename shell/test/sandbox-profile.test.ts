/**
 * T128g (L1303 G1): in pi, `bash`, `bash_background` and `monitor` run a
 * line whose programs are all git with a network subcommand, or gh with a
 * listed subcommand, as `jhon sandbox run ... --profile unsandboxed`, which
 * runs it with no sandbox; every other line with `work`. The engine decides
 * (`jhon sandbox profile -- <command>`, the function Claude Code's wrap
 * calls); a fake jhon logs each call, hands `sandbox profile` to the tree's
 * own binary or answers what a test gives, and runs nothing for `sandbox
 * run`: no command here opens a connection.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test, { after, type TestContext } from "node:test";
import { fileURLToPath } from "node:url";
import { registerBackgroundTasks } from "../lib/background-tasks-tools.ts";
import { sandboxProfile, wireSandbox } from "../lib/sandbox.ts";
import { waitFor } from "./bounded-wait.ts";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const BIN = process.env.JHON_TEST_BIN || join(REPO, "build", "jhon");

const FAKE = `#!/usr/bin/env node
const fs = require("node:fs"), path = require("node:path"), cp = require("node:child_process");
const dir = __dirname, args = process.argv.slice(2);
const state = JSON.parse(fs.readFileSync(path.join(dir, "state.json"), "utf8"));
fs.appendFileSync(path.join(dir, "calls.log"), JSON.stringify({ args, cwd: process.cwd() }) + "\\n");
if (args[0] === "sandbox" && args[1] === "status") {
  process.stdout.write(JSON.stringify({ mode: "on", can_run: true, reason: "", fix: "", project: state.project }) + "\\n");
  process.exit(0);
}
if (args[0] === "sandbox" && args[1] === "profile" && state.real) {
  const r = cp.spawnSync(state.real, args, { stdio: "inherit" });
  process.exit(r.status === null ? 1 : r.status);
}
if (args[0] === "sandbox" && args[1] === "profile" && state.answer) {
  if (state.answer.sleep) { setTimeout(() => process.exit(0), state.answer.sleep); return; }
  process.stdout.write(state.answer.stdout);
  process.exit(state.answer.status);
}
if (args[0] === "sandbox" && args[1] === "run") process.exit(0);
process.exit(2);
`;

type Answer = { stdout: string; status: number } | { sleep: number };

function scratch(t: TestContext, prefix: string): string {
	const d = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
	t.after(() => rmSync(d, { recursive: true, force: true }));
	return d;
}

function fakeJhon(t: TestContext, project: string, real: string | undefined, answer?: Answer) {
	const dir = scratch(t, "jhon-sbx-profile-fake-");
	writeFileSync(join(dir, "state.json"), JSON.stringify({ project, real, answer }));
	writeFileSync(join(dir, "calls.log"), "");
	const bin = join(dir, "jhon");
	writeFileSync(bin, FAKE);
	chmodSync(bin, 0o755);
	const calls = (): string[][] => readFileSync(join(dir, "calls.log"), "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l).args);
	/** Each run's argv before the command, its profile, and the command (the last argument). */
	const runs = () => calls().filter((a) => a[1] === "run").map((a) => ({ profile: a[a.indexOf("--profile") + 1], command: a[a.length - 1] }));
	return { bin, calls, runs };
}

function fakePi() {
	const tools = new Map<string, any>();
	const handlers = new Map<string, ((event: any, ctx: any) => unknown)[]>();
	const pi = {
		registerTool: (tool: any) => tools.set(tool.name, tool),
		registerCommand: () => {},
		registerMessageRenderer: () => {},
		on: (name: string, h: (event: any, ctx: any) => unknown) => handlers.set(name, [...(handlers.get(name) ?? []), h]),
		sendMessage: () => {},
	};
	const fire = async (name: string, event: any, ctx: any) => { for (const h of handlers.get(name) ?? []) await h(event, ctx); };
	return { pi, tools, fire };
}

const ctxAt = (cwd: string) => ({ cwd, hasUI: false, mode: "rpc", sessionManager: { getSessionId: () => "main", getSessionFile: () => undefined }, ui: { setStatus: () => {}, notify: () => {} } });

/** The lines and how each runs (L1303 G1, the classifier of L1245 K1). */
const LINES: [string, "unsandboxed" | "work"][] = [
	["git fetch", "unsandboxed"],
	["git push origin HEAD", "unsandboxed"],
	["git pull --rebase", "unsandboxed"],
	["git ls-remote origin", "unsandboxed"],
	["git clone git@github.com:o/r.git", "unsandboxed"],
	["git remote update", "unsandboxed"],
	["gh pr view 1", "unsandboxed"],
	["gh api repos/o/r", "unsandboxed"],
	["git fetch origin 2>&1", "unsandboxed"],
	["git commit -m x", "work"],
	["git rebase origin/main", "work"],
	["git status", "work"],
	["git fetch && git rebase origin/main", "work"],
	["git fetch | cat", "work"],
	["git fetch > refs.txt", "work"],
	["git fetch 'unterminated", "work"],
	["git ls-remote --upl=x /tmp/r", "work"],
	["git clone -nux /tmp/r", "work"],
	["git fetch {a,--upload-pack=x}", "work"],
	["gh {auth,} token", "work"],
	["gh -u bob auth token", "work"],
	["gh auth token", "work"],
	["gh extension install o/r", "work"],
	["git() { gh x; }; git fetch", "work"],
	["git pull origin -- --upload-pack=x", "work"],
	["git submodule update --init", "work"],
	["gh pr checkout 1", "work"],
	["gh api repos/o/r/issues -F body=@x", "work"],
	["gh api x -F=b=@file", "work"],
	["gh api x -iF=b=@file", "work"],
	["GIT_SSH_COMMAND=x git fetch", "work"],
	// T128h (L1332 K1): docker is the owner's deployment tool.
	["docker compose up -d", "unsandboxed"],
	["docker-compose logs", "unsandboxed"],
	["docker login", "work"],
	["docker context use rootless", "work"],
	["docker ps && npm test", "work"],
	// T128h2 (L1338): quoted words are literal; an unquoted glob is not.
	["docker ps -a --format '{{.Names}}'", "unsandboxed"],
	["docker --version", "unsandboxed"],
	["docker ps --filter name=a*", "work"],
];

const AGENT = mkdtempSync(join(tmpdir(), "jhon-sbx-profile-agent-"));
after(() => rmSync(AGENT, { recursive: true, force: true }));
Object.assign(process.env, { PI_CODING_AGENT_DIR: join(AGENT, "agent"), JHON_PROFILES: join(AGENT, "profiles.json"), CLAUDE_CONFIG_DIR: join(AGENT, "claude") });
const haveBin = existsSync(BIN) && existsSync("/bin/bash");
const needs = !haveBin && `needs /bin/bash and the tree's jhon (JHON_TEST_BIN or build/jhon): ${BIN}`;

function project(t: TestContext): string {
	const d = scratch(t, "jhon-sbx-profile-proj-");
	mkdirSync(join(d, ".git"));
	return d;
}

test("G1: the engine names how a line runs: unsandboxed for git's network subcommands and gh, work for anything else", { skip: needs }, async () => {
	for (const [line, profile] of LINES) assert.equal(await sandboxProfile({ jhon: BIN }, line, REPO), profile, line);
});

test("G1: bash runs a git network or gh line with --profile unsandboxed and any other line with work", { skip: needs }, async (t) => {
	const d = project(t);
	const fake = fakeJhon(t, d, BIN);
	const { pi, tools, fire } = fakePi();
	wireSandbox(pi, { jhon: fake.bin });
	await fire("session_start", {}, ctxAt(d));
	for (const [line] of LINES) await tools.get("bash").execute("c", { command: line }, undefined, undefined, ctxAt(d));
	assert.deepEqual(fake.runs(), LINES.map(([command, profile]) => ({ profile, command })));
	// The run is the one the work profile gets, but for the profile: the pinned project, the shell, -c and the line.
	const run = fake.calls().filter((a) => a[1] === "run")[0];
	assert.deepEqual(run, ["sandbox", "run", "--project", d, "--profile", "unsandboxed", "--", "/bin/bash", "-c", "git fetch"]);
	// The engine is asked for the pinned project (L1306: a git line must work inside it).
	assert.deepEqual(fake.calls().find((a) => a[1] === "profile"), ["sandbox", "profile", "--project", d, "--", "git fetch"]);
	// A git line working outside the project runs with work.
	await tools.get("bash").execute("c", { command: "git -C / fetch" }, undefined, undefined, ctxAt(d));
	assert.deepEqual(fake.runs().at(-1), { profile: "work", command: "git -C / fetch" });
});

test("G1: bash_background and monitor take the profile the same way", { skip: needs }, async (t) => {
	const d = project(t);
	const fake = fakeJhon(t, d, BIN);
	const { pi, tools, fire } = fakePi();
	const sandbox = wireSandbox(pi, { jhon: fake.bin });
	const out = scratch(t, "jhon-sbx-profile-jobs-");
	assert.ok(registerBackgroundTasks(pi as any, { env: {}, outputDir: () => out, sandbox: sandbox.jobOperations }));
	await fire("session_start", {}, ctxAt(d));
	await tools.get("bash_background").execute("c1", { command: "git fetch" }, undefined, undefined, ctxAt(d));
	await tools.get("monitor").execute("c2", { command: "gh run watch", timeout_seconds: 30 }, undefined, undefined, ctxAt(d));
	await tools.get("bash_background").execute("c3", { command: "git status" }, undefined, undefined, ctxAt(d));
	await waitFor(() => fake.runs().length === 3, "the three jobs ran");
	assert.deepEqual(fake.runs().sort((a, b) => a.command.localeCompare(b.command)), [
		{ profile: "unsandboxed", command: "gh run watch" },
		{ profile: "unsandboxed", command: "git fetch" },
		{ profile: "work", command: "git status" },
	]);
});

test("G1: where the engine gives no answer, or any other answer, the line runs with work (fail closed)", { skip: !existsSync("/bin/bash") && "needs /bin/bash" }, async (t) => {
	const d = project(t);
	for (const [name, answer] of [
		["no such subcommand", undefined],
		["an error", { stdout: "unsandboxed\n", status: 1 }],
		["another word", { stdout: "delivery\n", status: 0 }],
		["no newline", { stdout: "unsandboxed", status: 0 }],
		["more lines", { stdout: "unsandboxed\nwork\n", status: 0 }],
	] as [string, Answer | undefined][]) {
		const fake = fakeJhon(t, d, undefined, answer);
		const { pi, tools, fire } = fakePi();
		wireSandbox(pi, { jhon: fake.bin });
		await fire("session_start", {}, ctxAt(d));
		await tools.get("bash").execute("c", { command: "git fetch" }, undefined, undefined, ctxAt(d));
		assert.deepEqual(fake.runs(), [{ profile: "work", command: "git fetch" }], name);
		assert.ok(fake.calls().some((a) => a[1] === "profile"), `${name}: the engine was asked`);
	}
	// No answer in time.
	const slow = fakeJhon(t, d, undefined, { sleep: 3000 });
	assert.equal(await sandboxProfile({ jhon: slow.bin }, "git fetch", d, 300), "work");
	assert.equal(await sandboxProfile({ jhon: join(d, "no-such-jhon") }, "git fetch", d), "work");
	const yes = fakeJhon(t, d, undefined, { stdout: "unsandboxed\n", status: 0 });
	assert.equal(await sandboxProfile({ jhon: yes.bin }, "git fetch", d), "unsandboxed");
});
