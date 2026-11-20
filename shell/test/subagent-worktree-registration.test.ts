/**
 * T132: the `subagent` tool registers the worktree it creates or is given as
 * one of the session's roots, through the session's worktree registry, for a
 * child that answers in the call and for one that runs in the background,
 * with the commit its HEAD was at (its base); the LDD section looks there for
 * the change the child works on. A child in the session's own folder adds no
 * root.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test, { after } from "node:test";
import jhon, { CONFIG_FILE } from "../extensions/jhon.ts";
import { SESSION_WORKTREE_ENTRY } from "../lib/ui/session-worktree-registry.ts";
import { sessionChangeRoots, sessionRootBases } from "../lib/ui/session-changes.ts";

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

/** A repository with one commit, and a worktree of it beside it. */
function layout() {
	const base = tempDir("jhon-wt-register-");
	const repo = join(base, "app");
	mkdirSync(repo);
	git(repo, "init", "-q");
	writeFileSync(join(repo, "a.txt"), "a\n");
	git(repo, "add", ".");
	git(repo, "commit", "-qm", "init");
	const tree = join(base, "app-worktrees", "change");
	git(repo, "worktree", "add", "-q", "-b", "change", tree);
	const other = join(base, "other");
	mkdirSync(other);
	git(other, "init", "-q");
	return { base, repo, tree, other, head: git(repo, "rev-parse", "HEAD") };
}

const WORKER = "---\nname: explore\ndescription: \"Investigate\"\nmodel: openai/gpt-5\ntools:\n  - read\n---\nLook around.\n";

/** A fake pi child that commits one file where it runs, as a jhon-builder commits its slice, and answers. */
function fakePiBin(): string {
	const bin = join(tempDir("jhon-wt-fake-"), "pi");
	writeFileSync(bin, `#!/usr/bin/env node
const { execFileSync } = require("node:child_process");
const { writeFileSync } = require("node:fs");
writeFileSync("slice.txt", "one\\ntwo\\n");
const git = (...args) => execFileSync("git", ["-c", "user.email=t@t", "-c", "user.name=t", ...args], { stdio: "ignore" });
git("add", "slice.txt");
// A second child in the same folder has nothing new to commit.
try { git("commit", "-qm", "slice"); } catch {}
const said = "committed in " + process.cwd();
process.stdout.write(process.argv.includes("json") ? JSON.stringify({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: said }] } }) + "\\n" : said + "\\n");
`);
	chmodSync(bin, 0o755);
	return bin;
}

/** The jhon extension's tools over a session whose entries are kept; child: register as a sub-agent does, whose own sub-agents answer in the call. */
function setup(cwd: string, child = false) {
	const dir = tempDir("jhon-wt-agent-");
	mkdirSync(join(dir, "agents"));
	writeFileSync(join(dir, "agents", "explore.md"), WORKER);
	writeFileSync(join(dir, CONFIG_FILE), JSON.stringify({ jhon: HOOK_BIN, orchestrator: "/nonexistent", vcsDirs: [".git"], protected: ["/opt/jhon-framework-under-test"], inspectAllow: ["git diff"] }));
	const tools = new Map<string, any>();
	const sent: unknown[] = [];
	const entries: Array<{ type: string; customType: string; data: any }> = [];
	const vars: Record<string, string | undefined> = { PI_CODING_AGENT_DIR: dir, JHON_PI_BIN: fakePiBin(), JHON_PI_CHILD: child ? "1" : undefined, CLAUDE_CONFIG_DIR: join(dir, "claude") };
	const swap = () => {
		const saved: Record<string, string | undefined> = {};
		for (const [k, v] of Object.entries(vars)) { saved[k] = process.env[k]; if (v === undefined) delete process.env[k]; else process.env[k] = v; }
		return () => { for (const [k, v] of Object.entries(saved)) if (v === undefined) delete process.env[k]; else process.env[k] = v; };
	};
	const restore = swap();
	try {
		jhon({ on: () => {}, registerTool: (t: any) => tools.set(t.name, t), registerCommand: () => {}, sendMessage: (message: unknown) => { sent.push(message); },
			appendEntry: (customType: string, data: unknown) => { entries.push({ type: "custom", customType, data }); }, events: { emit: () => {}, on: () => () => {} } });
	} finally { restore(); }
	const ctx = { cwd, hasUI: false, sessionManager: { getSessionId: () => "parent", getSessionFile: () => undefined, getEntries: () => entries } };
	const run = async (params: any) => {
		const back = swap();
		try { return await tools.get("subagent").execute("call-1", params, undefined, undefined, ctx); } finally { back(); }
	};
	const textOf = (r: any) => r.content.map((c: any) => c.text).join("\n");
	const registered = () => entries.filter((entry) => entry.customType === SESSION_WORKTREE_ENTRY).map((entry) => entry.data);
	/** The session's roots that exist, each with its base, as the shell reads them from these entries. */
	const roots = () => {
		const bases = sessionRootBases("parent", entries);
		return Object.fromEntries(sessionChangeRoots("parent", entries).filter((root) => existsSync(join(root, ".git"))).map((root) => [root, bases.get(root)]));
	};
	return { run, textOf, registered, roots, sent };
}

async function until(check: () => boolean): Promise<void> {
	for (const deadline = Date.now() + 10_000; !check(); await new Promise((r) => setTimeout(r, 20))) if (Date.now() > deadline) throw new Error("timed out waiting");
}

test("a sub-agent that answers in the call: the worktree the tool creates for it is one of the session's roots, with its base", async () => {
	const { repo, head } = layout();
	const s = setup(repo, true);
	const answer = s.textOf(await s.run({ agent: "explore", task: "write the slice", worktree: true }));
	const created = /^committed in (\S+)$/m.exec(answer)?.[1];
	assert.ok(created && dirname(created) === `${repo}-worktrees`, answer);
	assert.deepEqual(s.registered(), [{ sessionId: "parent", root: created, evidence: "subagent:spawn", base: head }]);
	assert.deepEqual(s.roots(), { [created]: head }, "a session root, against the commit it started from");
});

test("a sub-agent that answers in the call: the worktree it is given (cwd) is registered once, however many children run there", async () => {
	const { repo, tree, head } = layout();
	const s = setup(repo, true);
	assert.match(s.textOf(await s.run({ agent: "explore", task: "write the slice", cwd: tree })), new RegExp(`committed in ${tree}$`, "m"));
	assert.deepEqual(s.registered(), [{ sessionId: "parent", root: tree, evidence: "subagent:spawn", base: head }]);
	await s.run({ agent: "explore", task: "again", cwd: join(tree, ".") });
	assert.equal(s.registered().length, 1, "the same root is not registered twice");
	assert.deepEqual(s.roots(), { [tree]: head });
});

test("a background sub-agent: its worktree is registered when it starts, before it has answered", async () => {
	const { repo, head } = layout();
	const s = setup(repo);
	const started = s.textOf(await s.run({ agent: "explore", task: "write the slice", worktree: true }));
	assert.match(started, /Started background sub-agent agent-\d+/);
	const created = /in its own worktree (\S+), on branch/.exec(started)?.[1];
	assert.ok(created && basename(dirname(created)) === "app-worktrees", started);
	assert.deepEqual(s.registered(), [{ sessionId: "parent", root: created, evidence: "subagent:spawn", base: head }]);
	// The child works after the call returned: wait for its commit.
	await until(() => git(created, "rev-list", "--count", "HEAD") === "2");
	assert.equal(readFileSync(join(created, "slice.txt"), "utf8"), "one\ntwo\n");
	assert.deepEqual(s.roots(), { [created]: head });
});

test("a sub-agent in the session's own folder adds no root, and a folder of another repository is still refused and registers nothing", async () => {
	const { repo, other } = layout();
	const s = setup(repo, true);
	assert.match(s.textOf(await s.run({ agent: "explore", task: "write the slice" })), new RegExp(`committed in ${repo}$`, "m"));
	assert.deepEqual(s.registered(), [], "the child shares the session's folder: nothing to add");
	await assert.rejects(s.run({ agent: "explore", task: "elsewhere", cwd: other }), /must be a git worktree of this session's repository/);
	assert.deepEqual(s.registered(), []);
});
