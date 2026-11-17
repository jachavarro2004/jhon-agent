/**
 * T81: from a session in the main checkout, `subagent` and `inspect` take
 * `cwd`, the change's worktree. It is admitted only when it is a git worktree
 * of the session's repository (the same common git dir), resolved to its top
 * level; another repository's folder or a plain folder is refused. Without it
 * both run in the session's folder, as before.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test, { after } from "node:test";
import jhon, { CONFIG_FILE } from "../extensions/jhon.ts";
import { sameRepoWorktree } from "../lib/worktree.ts";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const HOOK_BIN = process.env.JHON_TEST_BIN || join(REPO, "build", "jhon");
const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
function tempDir(prefix: string): string {
	const d = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
	temps.push(d);
	return d;
}
const git = (cwd: string, ...args: string[]) => execFileSync("git", ["-c", "user.email=t@t", "-c", "user.name=t", ...args], { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();

/** A repository with one commit, a change's worktree beside it with an uncommitted edit, another repository, and a plain folder. */
function layout() {
	const base = tempDir("jhon-wt-target-");
	const repo = join(base, "app");
	mkdirSync(join(repo, "src"), { recursive: true });
	git(repo, "init", "-q");
	writeFileSync(join(repo, "src", "a.txt"), "a\n");
	git(repo, "add", ".");
	git(repo, "commit", "-qm", "init");
	const tree = join(base, "app-worktrees", "change");
	git(repo, "worktree", "add", "-q", "-b", "change", tree);
	writeFileSync(join(tree, "src", "a.txt"), "a\nworktree edit\n");
	const other = join(base, "other");
	mkdirSync(other);
	git(other, "init", "-q");
	const plain = join(base, "plain");
	mkdirSync(plain);
	return { repo, tree, other, plain };
}

const EXPLORE = "---\nname: explore\ndescription: \"Investigate\"\nmodel: openai/gpt-5\ntools:\n  - read\n---\nLook around.\n";

/** A fake pi child (print mode) that answers with the folder it ran in. */
function fakePiBin(): string {
	const bin = join(tempDir("jhon-wt-fake-"), "pi");
	writeFileSync(bin, `#!/usr/bin/env node
process.stdout.write("ran in " + process.cwd() + "\\n");
`);
	chmodSync(bin, 0o755);
	return bin;
}

/** child: register as a sub-agent does, whose subagent calls answer in the call (the main session's run in the background). */
function setup(cwd: string, child = false) {
	const dir = tempDir("jhon-wt-agent-");
	mkdirSync(join(dir, "agents"));
	writeFileSync(join(dir, "agents", "explore.md"), EXPLORE);
	writeFileSync(join(dir, CONFIG_FILE), JSON.stringify({ jhon: HOOK_BIN, orchestrator: "/nonexistent", vcsDirs: [".git"], protected: ["/opt/jhon-framework-under-test"], inspectAllow: ["git diff", "git status", "git show"] }));
	const tools = new Map<string, any>();
	const vars: Record<string, string | undefined> = { PI_CODING_AGENT_DIR: dir, JHON_PI_BIN: fakePiBin(), JHON_PI_CHILD: child ? "1" : undefined, CLAUDE_CONFIG_DIR: join(dir, "claude") };
	const swap = () => {
		const saved: Record<string, string | undefined> = {};
		for (const [k, v] of Object.entries(vars)) { saved[k] = process.env[k]; if (v === undefined) delete process.env[k]; else process.env[k] = v; }
		return () => { for (const [k, v] of Object.entries(saved)) if (v === undefined) delete process.env[k]; else process.env[k] = v; };
	};
	const restore = swap();
	try {
		jhon({ on: () => {}, registerTool: (t: any) => tools.set(t.name, t), registerCommand: () => {}, sendMessage: () => {}, events: { emit: () => {}, on: () => () => {} } });
	} finally { restore(); }
	const ctx = { cwd, hasUI: false, sessionManager: { getSessionId: () => "parent", getSessionFile: () => undefined } };
	const run = async (name: string, params: any) => {
		const back = swap();
		try { return await tools.get(name).execute("call-1", params, undefined, undefined, ctx); } finally { back(); }
	};
	const textOf = (r: any) => r.content.map((c: any) => c.text).join("\n");
	return { run, textOf };
}

test("sameRepoWorktree admits the session's own repository's worktrees, at their top level, and refuses the rest naming the rule", () => {
	const { repo, tree, other, plain } = layout();
	assert.equal(sameRepoWorktree(repo, join(tree, "src")), tree);
	assert.equal(sameRepoWorktree(join(repo, "src"), tree), tree);
	assert.equal(sameRepoWorktree(tree, repo), repo, "the main checkout is a worktree of the same repository too");
	for (const dir of [other, plain, join(tree, "missing")]) {
		assert.throws(() => sameRepoWorktree(repo, dir), /must be a git worktree of this session's repository \(the same git common dir\)/, dir);
	}
});

test("subagent with cwd runs the child in the change's worktree; another repository or a plain folder is refused; without it the child runs in the session's folder", async () => {
	const { repo, tree, other, plain } = layout();
	const s = setup(repo, true);
	assert.match(s.textOf(await s.run("subagent", { agent: "explore", task: "where?", cwd: join(tree, "src") })), new RegExp(`ran in ${tree}$`, "m"));
	for (const dir of [other, plain]) {
		await assert.rejects(s.run("subagent", { agent: "explore", task: "where?", cwd: dir }), /must be a git worktree of this session's repository/);
	}
	assert.match(s.textOf(await s.run("subagent", { agent: "explore", task: "where?" })), new RegExp(`ran in ${repo}$`, "m"));
});

test("inspect with cwd reads the change's worktree; another repository or a plain folder is refused; without it inspect reads the session's folder", async () => {
	const { repo, tree, other, plain } = layout();
	const s = setup(repo);
	assert.match(s.textOf(await s.run("inspect", { command: "git diff -- src/a.txt", cwd: tree })), /^\+worktree edit$/m);
	assert.match(s.textOf(await s.run("inspect", { command: "git status --short", cwd: tree })), /M src\/a\.txt/);
	for (const dir of [other, plain]) {
		await assert.rejects(s.run("inspect", { command: "git status --short", cwd: dir }), /must be a git worktree of this session's repository/);
	}
	const main = await s.run("inspect", { command: "git status --short", cwd: undefined });
	assert.doesNotMatch(s.textOf(main), /src\/a\.txt/, "the main checkout has no edit");
	assert.equal(s.textOf(await s.run("inspect", { command: "git diff" })), "(exit 0, no output)");
});
