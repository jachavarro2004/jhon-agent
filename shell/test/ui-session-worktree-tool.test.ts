import "./isolated-env.ts";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import { resolveSessionWorktree } from "../lib/ui/session-worktree-registry.ts";
import { session, shell } from "./shell-harness.ts";

// The session_worktree_register tool of extensions/ui.ts: the shell's
// registry made at each session start, with the session file as its record.

type Entry = { type: string; customType: string; data: { sessionId?: string; root?: string; evidence?: string; base?: string } };

/** A shell whose entries land in the session's own list. */
function recording(env: NodeJS.ProcessEnv = {}, deps: Parameters<typeof shell>[1] = {}) {
	const s = shell(env, deps);
	const entries: Entry[] = [];
	(s.pi as unknown as { appendEntry(type: string, data: unknown): void }).appendEntry = (customType, data) => { entries.push({ type: "custom", customType, data: data as Entry["data"] }); };
	const register = (ctx: ExtensionContext, path: string) => s.tools.get("session_worktree_register")!.execute("r", { path }, undefined, undefined, ctx);
	const roots = (sessionId = "shell-session") => entries.filter((entry) => entry.customType === "jhon-shell.session-worktree/v1" && entry.data.sessionId === sessionId).map((entry) => entry.data);
	return { s, entries, register, roots };
}

/** A real repository with one commit, in a temporary folder. */
function repository(t: { after(fn: () => void): void }) {
	const dir = realpathSync(mkdtempSync(join(tmpdir(), "jhon-worktree-tool-")));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const git = (cwd: string, ...args: string[]) => execFileSync("git", ["-C", cwd, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
	const repo = join(dir, "app");
	git(dir, "init", "-q", "--initial-branch=main", repo);
	writeFileSync(join(repo, "a.txt"), "one\n");
	git(repo, "add", ".");
	git(repo, "commit", "-q", "-m", "init");
	return { dir, repo, git };
}

const realGit = { resolveWorktree: resolveSessionWorktree, rootExists: (root: string) => existsSync(join(root, ".git")) };

test("a worktree of the session's clone is registered once, with the commit its HEAD was at", async (t) => {
	const r = repository(t);
	const linked = join(r.dir, "linked");
	r.git(r.repo, "worktree", "add", "-q", "-b", "linked", linked);
	const w = recording({}, realGit);
	const { ctx } = session({ entries: w.entries });
	(ctx as unknown as { cwd: string }).cwd = r.repo;
	await w.s.fire("session_start", ctx);
	assert.deepEqual(await w.register(ctx, linked), { content: [{ type: "text", text: `${linked} is now one of this session's worktrees.` }], details: { root: linked, sessionId: "shell-session" } });
	await w.register(ctx, linked);
	const head = r.git(r.repo, "rev-parse", "HEAD");
	assert.deepEqual(w.roots(), [{ sessionId: "shell-session", root: r.repo, evidence: "session:cwd", base: head }, { sessionId: "shell-session", root: linked, evidence: "explicit", base: head }]);
	await w.s.fire("session_shutdown", ctx);
});

test("a folder of another repository is refused and nothing is registered", async (t) => {
	const r = repository(t);
	const other = join(r.dir, "other");
	r.git(r.dir, "init", "-q", other);
	const w = recording({}, realGit);
	const { ctx } = session({ entries: w.entries });
	(ctx as unknown as { cwd: string }).cwd = r.repo;
	await w.s.fire("session_start", ctx);
	await assert.rejects(w.register(ctx, other), { message: "Name a worktree that exists in this session's own Git clone; a folder of another repository cannot be one of its worktrees." });
	assert.deepEqual(w.roots().map((entry) => entry.root), [r.repo]);
	await w.s.fire("session_shutdown", ctx);
});

test("the end of a run never waits on Git", async () => {
	const w = recording();
	(w.s.pi as unknown as { exec(): Promise<never> }).exec = () => new Promise<never>(() => {});
	const { ctx } = session();
	await w.s.fire("session_start", ctx);
	await Promise.race([w.s.fire("agent_end", ctx), new Promise((_, reject) => setTimeout(() => reject(new Error("agent_end waited")), 100))]);
	await w.s.fire("session_shutdown", ctx);
});

test("before a session starts, after it ends, and for another session the tool has no list", async () => {
	const w = recording();
	const { ctx } = session({ entries: w.entries });
	await assert.rejects(w.register(ctx, "/repo"), { message: "This session has no worktree list yet." });
	await w.s.fire("session_start", ctx);
	const other = session({ entries: w.entries, id: "other-session" });
	await assert.rejects(w.register(other.ctx, "/repo"), { message: "This session has no worktree list yet." });
	await w.s.fire("session_shutdown", ctx);
	await assert.rejects(w.register(ctx, "/repo"), { message: "This session has no worktree list yet." });
});

test("the clone is checked on every call: a folder that becomes a repository later counts from then on", async () => {
	let ready = false;
	const w = recording({ JHON_SHELL_CHANGES_WATCH_MS: "off" }, {
		resolveWorktree: (path) => (ready ? { root: path === "." ? "/repo" : path, commonDir: path === "/foreign" ? "/foreign/git" : "/repo/.git" } : undefined),
	});
	const { ctx, ui } = session({ entries: w.entries });
	await w.s.fire("session_start", ctx);
	await assert.rejects(w.register(ctx, "/repo"), /own Git clone/);
	ready = true;
	await w.register(ctx, "/repo");
	await w.register(ctx, "/repo");
	await assert.rejects(w.register(ctx, "/foreign"), /another repository cannot be one of its worktrees/);
	assert.equal(w.entries.length, 1);
	assert.equal(w.s.commands.has("jhon:changes"), false, "T174: no changes view");
	assert.equal(ui.view, undefined);
	await w.s.fire("session_shutdown", ctx);
});

test("a new session does not inherit the worktrees another session registered", async () => {
	const w = recording();
	const first = session({ entries: w.entries });
	await w.s.fire("session_start", first.ctx);
	await w.register(first.ctx, "/linked");
	await w.s.fire("session_shutdown", first.ctx);
	const next = session({ entries: w.entries, id: "new-session" });
	await w.s.fire("session_start", next.ctx);
	assert.ok(!w.roots("new-session").some((entry) => entry.root === "/linked"), JSON.stringify(w.roots("new-session")));
	await w.s.fire("session_shutdown", next.ctx);
});
