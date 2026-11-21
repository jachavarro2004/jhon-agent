import "./isolated-env.ts";
import assert from "node:assert/strict";
import childProcess, { execFileSync } from "node:child_process";
import { syncBuiltinESMExports } from "node:module";
import { mkdirSync, mkdtempSync, realpathSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { SessionManager } from "@earendil-works/pi-coding-agent";
import {
	SessionWorktreeRegistry, clearSessionWorktreeMemo, memoizeWorktreeResolver, registerSubagentWorktree, resolveSessionWorktree,
	resolveSessionWorktreeWithGit, resolveWorktreeHeadWithGit, SESSION_WORKTREE_CHANGED, SESSION_WORKTREE_ENTRY, SUBAGENT_WORKTREE_EVIDENCE, worktreeGitEnvironment,
} from "../lib/ui/session-worktree-registry.ts";

// The session's worktree list, against real Git repositories made for each
// test in a temporary folder (never the clone the suite runs in).

const REFUSED = {
	foreign: "Name a worktree that exists in this session's own Git clone; a folder of another repository cannot be one of its worktrees.",
	inactive: "This session is no longer the active one: it takes no new worktree.",
	moved: "The session's repository is not the one it started in; the worktree is not registered.",
};

function scratch(t: test.TestContext, prefix: string) {
	const dir = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const blank = join(dir, "blank");
	mkdirSync(blank);
	writeFileSync(join(blank, "config"), "");
	const env = { ...worktreeGitEnvironment(), GIT_CONFIG_GLOBAL: join(blank, "config"), GIT_CONFIG_NOSYSTEM: "1", GIT_ATTR_NOSYSTEM: "1" };
	const git = (cwd: string, ...args: string[]) => String(execFileSync("git", ["-C", cwd, "-c", `core.hooksPath=${blank}`, "-c", "commit.gpgsign=false", ...args], { env, stdio: ["ignore", "pipe", "pipe"] }));
	const commit = (cwd: string) => git(cwd, "-c", "user.name=T", "-c", "user.email=t@example.invalid", "commit", "--allow-empty", "-qm", "c");
	return { dir, blank, git, commit };
}

/** A clone `main` with a linked worktree `linked` (a file in it, a symlink `alias` to it) and an unrelated repository `other`. */
function clones(t: test.TestContext) {
	const s = scratch(t, "jhon-worktrees-");
	const [main, linked, other, alias] = ["main", "linked", "other", "alias"].map((name) => join(s.dir, name));
	s.git(s.dir, "init", "-q", "--initial-branch=main", `--template=${s.blank}`, main);
	s.commit(main);
	s.git(main, "worktree", "add", "-q", "-b", "linked", linked);
	s.git(s.dir, "init", "-q", `--template=${s.blank}`, other);
	mkdirSync(join(linked, "src"));
	writeFileSync(join(linked, "src", "file.ts"), "x\n");
	symlinkSync(linked, alias, "dir");
	return { ...s, main, linked, other, alias };
}

function session(cwd: string, manager = SessionManager.inMemory(cwd)) {
	const emitted: Array<[string, unknown]> = [];
	const host = { appendEntry: (type: string, data: unknown) => { manager.appendCustomEntry(type, data); }, events: { emit: (name: string, data: unknown) => { emitted.push([name, data]); } } };
	const recorded = () => manager.getEntries().filter((e) => e.type === "custom" && e.customType === SESSION_WORKTREE_ENTRY);
	return { host, manager, emitted, recorded, registry: () => new SessionWorktreeRegistry(host, manager, cwd) };
}

test("the session's folder is its first root; a worktree joins once, by any spelling; another repository is refused", (t) => {
	const c = clones(t);
	const s = session(c.main);
	const list = s.registry();
	list.start();
	assert.deepEqual(list.roots(), [c.main]);
	assert.equal(list.register(join(c.alias, "src", "file.ts"), "tool:read"), c.linked, "a file through a symlink names its worktree");
	assert.equal(list.register(c.linked, "explicit"), c.linked);
	assert.equal(list.register(`@${c.linked}`, "explicit"), c.linked, "pi's @ spelling");
	assert.deepEqual(list.roots(), [c.main, c.linked]);
	assert.equal(s.recorded().length, 2);
	assert.deepEqual(s.emitted.map(([name]) => name), [SESSION_WORKTREE_CHANGED, SESSION_WORKTREE_CHANGED]);
	assert.deepEqual(s.emitted[0]![1], { sessionId: s.manager.getSessionId() });
	assert.throws(() => list.register(c.other, "explicit"), { message: REFUSED.foreign });
	assert.throws(() => list.register(join(c.dir, "nowhere"), "explicit"), { message: REFUSED.foreign });
	assert.equal(resolveSessionWorktree(c.alias, c.main)?.root, c.linked);
});

test("a root's entry keeps the commit its HEAD was at when first recorded, and only a commit id", (t) => {
	const c = clones(t);
	const head = c.git(c.main, "rev-parse", "HEAD").trim();
	assert.equal(resolveWorktreeHeadWithGit(c.main), head);
	assert.equal(resolveWorktreeHeadWithGit(c.other), undefined, "no commit yet");
	const s = session(c.main);
	s.registry().start();
	assert.deepEqual(s.recorded()[0]!.data, { sessionId: s.manager.getSessionId(), root: c.main, evidence: "session:cwd", base: head });
	let asked = 0;
	for (const base of ["main", () => { throw new Error("no git"); }]) {
		const manager = SessionManager.inMemory(c.main);
		const list = new SessionWorktreeRegistry({ appendEntry: (type, data) => { manager.appendCustomEntry(type, data); }, events: { emit() {} } }, manager, c.main, resolveSessionWorktree,
			() => { asked++; return typeof base === "function" ? base() : base; });
		list.start();
		list.start();
		assert.equal(manager.getEntries().length, 1);
		assert.deepEqual(Object.keys((manager.getEntries()[0] as unknown as { data: object }).data), ["sessionId", "root", "evidence"], "no base field");
	}
	assert.equal(asked, 2, "the base is read once per root, at its first record");
});

test("a folder that becomes a repository later is taken, but only the session's own folder", (t) => {
	const c = clones(t);
	const cwd = join(c.dir, "later");
	mkdirSync(cwd);
	const s = session(cwd);
	const list = s.registry();
	list.start();
	assert.deepEqual(list.roots(), []);
	assert.throws(() => list.register(c.other, "explicit"), { message: REFUSED.foreign });
	assert.throws(() => list.register(cwd, "explicit"), { message: REFUSED.foreign });
	c.git(c.dir, "init", "-q", `--template=${c.blank}`, cwd);
	const routing = { GIT_DIR: join(c.other, ".git"), GIT_WORK_TREE: c.other, GIT_COMMON_DIR: join(c.other, ".git") };
	const saved = Object.fromEntries(Object.keys(routing).map((name) => [name, process.env[name]]));
	try {
		Object.assign(process.env, routing);
		assert.equal(list.register(cwd, "explicit"), cwd, "the ambient GIT_* variables pick nothing");
	} finally {
		for (const [name, value] of Object.entries(saved)) if (value === undefined) delete process.env[name]; else process.env[name] = value;
	}
	list.register(cwd, SUBAGENT_WORKTREE_EVIDENCE);
	assert.deepEqual(list.roots(), [cwd]);
	assert.equal(s.manager.getEntries().length, 1, "taken once");
	assert.throws(() => list.register(c.other, "explicit"), { message: REFUSED.foreign });
});

test("a .git file that points nowhere makes no repository, and records nothing", (t) => {
	const c = clones(t);
	const cwd = join(c.dir, "broken");
	mkdirSync(cwd);
	writeFileSync(join(cwd, ".git"), "gitdir: nowhere\n");
	const s = session(cwd);
	const list = s.registry();
	list.start();
	assert.throws(() => list.register(c.other, "explicit"), { message: REFUSED.foreign });
	assert.throws(() => list.register(cwd, "explicit"), { message: REFUSED.foreign });
	assert.deepEqual(list.roots(), []);
	assert.deepEqual(s.manager.getEntries(), []);
});

for (const change of ["root", "commonDir", "gone"] as const) {
	test(`a session whose clone changed (${change}) is refused and keeps the clone it started with`, () => {
		const started = { root: "/work", commonDir: "/work/.git" };
		let now: typeof started | undefined = started;
		const appended: unknown[] = [];
		const list = new SessionWorktreeRegistry({ appendEntry: (_type, data) => { appended.push(data); }, events: { emit() {} } },
			{ getSessionId: () => "s", getEntries: () => [] }, "/work/sub", (path) => (path === "/work/sub" ? now : started));
		list.start();
		now = change === "gone" ? undefined : { ...started, [change]: "/elsewhere" };
		assert.throws(() => list.register(started.root, "explicit"), { message: REFUSED.moved });
		assert.deepEqual(list.roots(), []);
		assert.equal(appended.length, 1);
		now = started;
		assert.equal(list.validate(started.root), started.root);
	});
}

test("a closed or replaced session takes nothing and asks Git nothing more", () => {
	for (const how of ["closed", "replaced"]) {
		let id = "first";
		let asked = 0;
		let identity: { root: string; commonDir: string } | undefined;
		const list = new SessionWorktreeRegistry({ appendEntry: () => assert.fail("appended"), events: { emit() {} } },
			{ getSessionId: () => id, getEntries: () => [] }, "/w", () => { asked++; return identity; });
		identity = { root: "/w", commonDir: "/w/.git" };
		if (how === "closed") list.close(); else id = "second";
		assert.throws(() => list.register("/w", "explicit"), { message: REFUSED.inactive });
		assert.deepEqual(list.roots(), []);
		assert.equal(asked, 1, "only the constructor asked");
	}
	let reads = 0;
	const failing = new SessionWorktreeRegistry({ appendEntry() {}, events: { emit() {} } }, { getSessionId: () => { if (reads++) throw new Error("gone"); return "s"; }, getEntries: () => [] }, "/w", () => undefined);
	assert.deepEqual(failing.roots(), [], "a session that cannot say its id is not the active one");
});

test("the record is the session file: a resumed session keeps its roots; a fork or copied entries under a new id do not", (t) => {
	const c = clones(t);
	const manager = SessionManager.create(c.main, join(c.dir, "sessions"));
	const first = manager.appendMessage({ role: "user", content: "go", timestamp: 1 });
	// pi writes the file after the first assistant message.
	manager.appendMessage({ role: "assistant", content: [], api: "openai-responses", provider: "t", model: "t", usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } }, stopReason: "stop", timestamp: 2 });
	const s = session(c.main, manager);
	s.registry().start();
	s.registry().register(c.linked, "explicit");
	manager.branch(first);
	assert.deepEqual(s.registry().roots(), [c.main, c.linked], "a root recorded on another branch still counts");
	assert.deepEqual(session(c.main, SessionManager.open(manager.getSessionFile()!)).registry().roots(), [c.main, c.linked]);
	const fork = session(c.main, SessionManager.open(manager.createBranchedSession(manager.getEntries().at(-1)!.id)!));
	fork.registry().start();
	assert.deepEqual(fork.registry().roots(), [c.main], "the fork's inherited entries carry the old id");
	const copy = session(c.main);
	for (const entry of manager.getEntries()) if (entry.type === "custom") copy.manager.appendCustomEntry(entry.customType, entry.data);
	copy.registry().start();
	assert.deepEqual(copy.registry().roots(), [c.main]);
});

test("two registries on one session append each root once; a closed one refuses", (t) => {
	const c = clones(t);
	const s = session(c.main);
	const shell = s.registry();
	shell.start();
	const agents = s.registry();
	agents.register(c.linked, SUBAGENT_WORKTREE_EVIDENCE);
	assert.deepEqual(shell.roots(), [c.main, c.linked]);
	shell.register(c.alias, "explicit");
	assert.equal(s.recorded().length, 2);
	agents.close();
	assert.throws(() => agents.register(c.main, "explicit"), { message: REFUSED.inactive });
});

test("a removed worktree leaves the list but keeps its entry; malformed and foreign entries are ignored", (t) => {
	const c = clones(t);
	const s = session(c.main);
	const list = s.registry();
	list.start();
	list.register(c.linked, "explicit");
	s.manager.appendCustomEntry(SESSION_WORKTREE_ENTRY, { sessionId: s.manager.getSessionId(), root: c.other, evidence: "explicit" });
	s.manager.appendCustomEntry(SESSION_WORKTREE_ENTRY, { sessionId: s.manager.getSessionId(), root: 42 });
	s.manager.appendCustomEntry(SESSION_WORKTREE_ENTRY, null);
	rmSync(c.linked, { recursive: true });
	assert.deepEqual(list.roots(), [c.main]);
	assert.ok(s.recorded().some((e) => (e as { data?: { root?: string } }).data?.root === c.linked));
});

test("a sub-agent's worktree joins the session's roots; its own root adds nothing; a failure records nothing and never throws", (t) => {
	const c = clones(t);
	const s = session(c.main);
	s.registry().start();
	assert.equal(registerSubagentWorktree(s.host, s.manager, c.main, c.main), undefined);
	assert.equal(registerSubagentWorktree(s.host, s.manager, c.main, join(c.linked, "src")), c.linked);
	assert.equal((s.recorded()[1] as { data: { evidence: string } }).data.evidence, SUBAGENT_WORKTREE_EVIDENCE);
	assert.equal(registerSubagentWorktree(s.host, s.manager, c.main, c.other), undefined);
	assert.equal(registerSubagentWorktree({}, s.manager, c.main, c.linked), undefined);
	assert.equal(registerSubagentWorktree(s.host, undefined, c.main, c.linked), undefined);
	assert.equal(registerSubagentWorktree(undefined as never, s.manager, c.main, c.linked), undefined);
	assert.equal(s.recorded().length, 2);
});

test("a Git child gets an environment without any GIT_ variable, and the source stays as it was", () => {
	const env = { PATH: "/bin", HOME: "/h", GIT_DIR: "/x/.git", GIT_WORK_TREE: "/x", GIT_INDEX_FILE: "/x/i", GIT_COMMON_DIR: "/x/c", GIT_CONFIG_COUNT: "1", GIT_CONFIG_KEY_0: "core.worktree", GIT_CONFIG_VALUE_0: "/x", git_dir: "/lower" };
	const copy = { ...env };
	assert.deepEqual(worktreeGitEnvironment(env), { PATH: "/bin", HOME: "/h" });
	assert.deepEqual(env, copy);
});

test("Git is asked twice per path, hidden, with no shell", (t) => {
	const c = clones(t);
	const calls: Array<{ command: string; args: readonly string[]; options: Record<string, unknown> }> = [];
	const run = ((command: string, args: readonly string[], options: Record<string, unknown>) => {
		calls.push({ command, args, options });
		return args.at(-1) === "--show-toplevel" ? `${c.main}\n` : `${join(c.main, ".git")}\r\n`;
	}) as typeof execFileSync;
	assert.deepEqual(resolveSessionWorktreeWithGit(c.main, c.main, run), { root: c.main, commonDir: join(c.main, ".git") });
	assert.deepEqual(calls.map((call) => call.args.at(-1)), ["--show-toplevel", "--git-common-dir"]);
	for (const call of calls) {
		assert.equal(call.command, "git");
		assert.deepEqual(call.args.slice(0, 5), ["--no-optional-locks", "-C", c.main, "rev-parse", "--path-format=absolute"]);
		assert.equal(call.options.shell, false);
		assert.equal(call.options.windowsHide, true);
		assert.equal(call.options.timeout, 5000);
	}
	assert.equal(resolveSessionWorktreeWithGit(c.main, c.main, (() => { throw new Error("no git"); }) as never), undefined);
});

// C2: a repository inside another resolves to the inner one, as Git walks up from the file's folder.
test("a file of a repository nested in another belongs to the nearest one", (t) => {
	const s = scratch(t, "jhon-worktrees-nested-");
	const outer = join(s.dir, "work");
	const inner = join(outer, "app");
	s.git(s.dir, "init", "-q", "--initial-branch=main", `--template=${s.blank}`, outer);
	s.git(s.dir, "init", "-q", "--initial-branch=feature", `--template=${s.blank}`, inner);
	s.commit(inner);
	mkdirSync(join(inner, "odd", "tasks"), { recursive: true });
	writeFileSync(join(inner, "odd", "tasks", "t.md"), "x\n");
	writeFileSync(join(outer, "README.md"), "x\n");
	assert.equal(resolveSessionWorktree(join("app", "odd", "tasks", "t.md"), outer)?.root, inner);
	assert.equal(resolveSessionWorktree("app", outer)?.root, inner);
	assert.equal(resolveSessionWorktree(join("odd", "tasks", "t.md"), inner)?.root, inner);
	assert.equal(resolveSessionWorktree("README.md", outer)?.root, outer);
});

function gitChildren(t: test.TestContext): () => number {
	let count = 0;
	const real = childProcess.execFileSync;
	childProcess.execFileSync = ((...args: Parameters<typeof real>) => { if (args[0] === "git") count++; return real(...args); }) as typeof real;
	syncBuiltinESMExports();
	t.after(() => { childProcess.execFileSync = real; syncBuiltinESMExports(); });
	return () => count;
}

// S4: at startup every extension asks about the same folder; Git answers once.
test("the same question asked again is answered from memory until the memory is cleared", (t) => {
	const c = clones(t);
	clearSessionWorktreeMemo();
	const children = gitChildren(t);
	const answer = resolveSessionWorktree(c.main, c.main);
	for (let i = 0; i < 4; i++) assert.deepEqual(resolveSessionWorktree(c.main, c.main), answer);
	assert.equal(children(), 2, "one answer is two rev-parse calls");
	clearSessionWorktreeMemo();
	resolveSessionWorktree(c.main, c.main);
	assert.equal(children(), 4);
});

test("a remembered answer gives way to a new nested repository, a removed clone, and time", (t) => {
	const c = clones(t);
	let clock = 0;
	const remember = memoizeWorktreeResolver(resolveSessionWorktreeWithGit, { ttlMs: 1000, now: () => clock });
	const nested = join(c.main, "nested");
	mkdirSync(nested);
	assert.equal(remember(nested, c.main)?.root, c.main);
	c.git(c.dir, "init", "-q", nested);
	assert.equal(remember(nested, c.main)?.root, realpathSync(nested));
	rmSync(join(c.main, ".git"), { recursive: true, force: true });
	assert.equal(remember(c.main, c.main), undefined);
	let asked = 0;
	const counting = memoizeWorktreeResolver((path, cwd) => { asked++; return resolveSessionWorktreeWithGit(path, cwd); }, { ttlMs: 1000, now: () => clock });
	counting(c.other, c.other);
	counting(c.other, c.other);
	assert.equal(asked, 1);
	clock = 1001;
	counting(c.other, c.other);
	assert.equal(asked, 2, "an expired answer is asked again");
	counting(c.blank, c.dir);
	counting(c.blank, c.dir);
	assert.equal(asked, 4, "no repository is never remembered");
	const copy = counting(c.other, c.other)!;
	copy.root = "/changed";
	assert.equal(counting(c.other, c.other)?.root, realpathSync(c.other), "the caller gets a copy");
});
