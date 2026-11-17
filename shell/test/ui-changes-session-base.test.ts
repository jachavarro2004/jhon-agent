/**
 * T132: Changes counts what the session's agents changed, committed or not.
 * Each root is compared against the commit its HEAD was at when the session
 * first registered it, kept in the session's entry; a base that is gone or no
 * longer in the root's history, or an entry with none, reads against HEAD as
 * before, and the view says so. Real repositories, the shell's own registry,
 * tracker, Git runner and view.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import test from "node:test";
import { SessionManager } from "@earendil-works/pi-coding-agent";
import { loadFileDiff, shellGitRunner } from "../extensions/ui.ts";
import { SessionWorktreeRegistry, SESSION_WORKTREE_ENTRY } from "../lib/ui/session-worktree-registry.ts";
import { sessionChangeRoots, sessionRootBases, sessionSubagentRoots } from "../lib/ui/session-changes.ts";
import { CHANGES_FALLBACK, WorktreeChangesTracker } from "../lib/ui/shell-changes.ts";
import { WorktreeChangesView } from "../lib/ui/shell-changes-view.ts";

// Every repository and worktree belongs to the test's own fixture.
function fixture(t: test.TestContext) {
	const dir = realpathSync(mkdtempSync(join(tmpdir(), "changes-base-")));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const empty = join(dir, "empty");
	mkdirSync(empty);
	writeFileSync(join(empty, "config"), "");
	const env: NodeJS.ProcessEnv = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("GIT_")));
	Object.assign(env, { GIT_CONFIG_GLOBAL: join(empty, "config"), GIT_CONFIG_NOSYSTEM: "1", GIT_ATTR_NOSYSTEM: "1" });
	const git = (cwd: string, ...args: string[]) => execFileSync("git", ["-C", cwd, "-c", `core.hooksPath=${empty}`, "-c", "commit.gpgsign=false", "-c", "user.name=Test", "-c", "user.email=test@example.invalid", ...args], { env, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
	const main = join(dir, "main");
	git(dir, "init", "--quiet", "--initial-branch=main", `--template=${empty}`, main);
	writeFileSync(join(main, "a.txt"), "one\n");
	writeFileSync(join(main, "gone.txt"), "leaves\n");
	git(main, "add", ".");
	git(main, "commit", "-q", "-m", "first");
	const commit = (cwd: string, message: string) => { git(cwd, "add", "-A"); git(cwd, "commit", "-q", "-m", message); return git(cwd, "rev-parse", "HEAD"); };
	const worktree = (name: string) => { const root = join(dir, name); git(main, "worktree", "add", "-q", "-b", name, root); return root; };
	return { dir, main, env, git, commit, worktree, head: (cwd: string) => git(cwd, "rev-parse", "HEAD") };
}

function sessionAt(cwd: string, session = SessionManager.inMemory(cwd)) {
	const pi = { appendEntry: (type: string, data: unknown) => { session.appendCustomEntry(type, data); }, events: { emit() {} } };
	const registry = new SessionWorktreeRegistry(pi, session, cwd);
	registry.start();
	return { session, registry, pi };
}

const lineCount = (root: string) => async (path: string) => {
	const text = readFileSync(join(root, path), "utf8");
	return text === "" ? 0 : text.split("\n").length - (text.endsWith("\n") ? 1 : 0);
};

/** The session's tracker as the shell builds it: its roots and their bases from the session's entries, Git through the shell's runner. */
function trackerOf(session: { getSessionId(): string; getEntries(): readonly unknown[] }, cwd: string, env: NodeJS.ProcessEnv) {
	const id = session.getSessionId();
	const git = (root: string) => shellGitRunner(root, env);
	const entries = () => session.getEntries() as never[];
	return new WorktreeChangesTracker(git(cwd), git, lineCount, () => sessionChangeRoots(id, entries()).filter((root) => existsSync(join(root, ".git"))), undefined, () => sessionRootBases(id, entries()));
}

/** What the owner reads: per root, each file's status, path and counts. */
const rows = (tracker: WorktreeChangesTracker) => Object.fromEntries(tracker.worktrees.map((tree) => [basename(tree.root), tree.model.files.map((file) => `${file.status} ${file.path} +${file.added} -${file.deleted}`)]));

const registrations = (session: { getEntries(): readonly unknown[] }) => (session.getEntries() as Array<{ type: string; customType?: string; data?: { root?: string; evidence?: string; base?: string } }>)
	.filter((entry) => entry.type === "custom" && entry.customType === SESSION_WORKTREE_ENTRY).map((entry) => entry.data!);

const plain = { fg: (_role: string, text: string) => text };
function viewText(tracker: WorktreeChangesTracker, width = 220): string {
	const view = new WorktreeChangesView(tracker.worktrees, { theme: plain, rows: 12, loadDiff: async () => "", onOpen() {}, onClose() {}, onRefresh() {}, requestRender() {} });
	try { return view.render(width).join("\n"); } finally { view.dispose(); }
}
/** The view's right pane at a width, its rows joined: a wrapped sentence reads whole. */
const paneText = (tracker: WorktreeChangesTracker, width: number) => viewText(tracker, width).split("\n").slice(1, -2).map((row) => row.split("│")[2]!.trim()).join(" ");

test("each root is compared with the commit its HEAD was at when the session first registered it: committed and uncommitted work both count, untracked files as before", async (t) => {
	const f = fixture(t);
	const first = f.head(f.main);
	const { session } = sessionAt(f.main);
	assert.deepEqual(registrations(session), [{ sessionId: session.getSessionId(), root: f.main, evidence: "session:cwd", base: first }], "the session's entry keeps the base");
	const tracker = trackerOf(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), {}, "nothing differs from the base yet");

	writeFileSync(join(f.main, "a.txt"), "one\ntwo\n");
	await tracker.refresh();
	assert.deepEqual(rows(tracker), { main: ["modified a.txt +1 -0"] }, "an uncommitted edit");

	f.commit(f.main, "second");
	await tracker.refresh();
	assert.deepEqual(rows(tracker), { main: ["modified a.txt +1 -0"] }, "the same edit, committed, is still the session's change");
	assert.deepEqual([tracker.model.files.length, tracker.model.added, tracker.model.deleted], [1, 1, 0], "the counter the rail and the footer read");

	writeFileSync(join(f.main, "new.txt"), "x\ny\nz\n");
	rmSync(join(f.main, "gone.txt"));
	f.commit(f.main, "third");
	writeFileSync(join(f.main, "a.txt"), "zero\none\ntwo\n");
	writeFileSync(join(f.main, "notes.md"), "untracked\nfile\n");
	await tracker.refresh();
	assert.deepEqual(rows(tracker), { main: ["modified a.txt +2 -0", "deleted gone.txt +0 -1", "added new.txt +3 -0", "untracked notes.md +2 -0"] }, "two commits and an uncommitted edit add up against the base; an untracked file counts its lines");
	assert.deepEqual(tracker.worktrees[0]!.comparison, { against: "base", base: first });
	assert.deepEqual(tracker.failedRoots, []);
});

test("a renamed file, committed, is one renamed row against the base", async (t) => {
	const f = fixture(t);
	const { session } = sessionAt(f.main);
	renameSync(join(f.main, "gone.txt"), join(f.main, "kept.txt"));
	f.commit(f.main, "rename");
	const tracker = trackerOf(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), { main: ["renamed kept.txt +0 -0"] });
});

test("resuming the session compares against the same base, and registering a root again never moves it", async (t) => {
	const f = fixture(t);
	const first = f.head(f.main);
	const file = join(f.dir, "sessions");
	const session = SessionManager.create(f.main, file);
	// Pi persists a session's entries after its first assistant message.
	session.appendMessage({ role: "user", content: "start", timestamp: 1 });
	session.appendMessage({ role: "assistant", content: [], api: "openai-responses", provider: "test", model: "test", usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } }, stopReason: "stop", timestamp: 2 });
	const started = sessionAt(f.main, session);
	const linked = f.worktree("linked");
	started.registry.register(linked, "explicit");
	writeFileSync(join(f.main, "a.txt"), "one\ntwo\n");
	f.commit(f.main, "second");
	writeFileSync(join(linked, "b.txt"), "b\n");
	f.commit(linked, "in the worktree");

	const resumed = sessionAt(f.main, SessionManager.open(session.getSessionFile()!));
	resumed.registry.register(linked, "subagent:spawn");
	assert.deepEqual(registrations(resumed.session).map((entry) => [entry.root, entry.evidence, entry.base]), [[f.main, "session:cwd", first], [linked, "explicit", first]], "no second entry, and the bases are the first ones");
	const tracker = trackerOf(resumed.session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), { main: ["modified a.txt +1 -0"], linked: ["added b.txt +1 -0"] });
});

test("a base that is no longer an ancestor of the root's HEAD compares against HEAD as before, and the view says so", async (t) => {
	const f = fixture(t);
	writeFileSync(join(f.main, "a.txt"), "one\ntwo\n");
	const dropped = f.commit(f.main, "the base, later rewritten");
	const { session } = sessionAt(f.main);
	// The commit the session started on is replaced: it leaves the history.
	f.git(f.main, "commit", "-q", "--amend", "-m", "rewritten");
	writeFileSync(join(f.main, "c.txt"), "c\n");
	f.commit(f.main, "after the rewrite");
	writeFileSync(join(f.main, "a.txt"), "one\ntwo\nthree\n");
	const tracker = trackerOf(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), { main: ["modified a.txt +1 -0"] }, "only what git status reports, as before");
	assert.deepEqual(tracker.worktrees[0]!.comparison, { against: "head", base: dropped, fallback: CHANGES_FALLBACK.NOT_ANCESTOR });
	const text = viewText(tracker);
	assert.match(text, /main · main · vs HEAD/, "the root's row says what it is compared with");
	const sentence = new RegExp(`Compared with HEAD \\(uncommitted changes only\\): the session's base ${dropped.slice(0, 7)} is no longer an ancestor of this root's HEAD\\.`);
	assert.match(text, sentence);
	assert.match(paneText(tracker, 100), sentence, "a narrow pane wraps the sentence, it does not clip it");
});

test("a base that no longer exists compares against HEAD as before, and the view says so", async (t) => {
	const f = fixture(t);
	const session = SessionManager.inMemory(f.main);
	const missing = "0123456789abcdef0123456789abcdef01234567";
	session.appendCustomEntry(SESSION_WORKTREE_ENTRY, { sessionId: session.getSessionId(), root: f.main, evidence: "session:cwd", base: missing });
	sessionAt(f.main, session);
	assert.equal(registrations(session).length, 1);
	writeFileSync(join(f.main, "c.txt"), "c\n");
	f.commit(f.main, "committed");
	writeFileSync(join(f.main, "a.txt"), "one\ntwo\n");
	const tracker = trackerOf(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), { main: ["modified a.txt +1 -0"] });
	assert.deepEqual(tracker.worktrees[0]!.comparison, { against: "head", base: missing, fallback: CHANGES_FALLBACK.MISSING });
	const text = viewText(tracker);
	assert.match(text, /main · main · vs HEAD/);
	assert.match(text, /Compared with HEAD \(uncommitted changes only\): the session's base 0123456 no longer exists\./);
});

test("an entry saved before bases were kept reads as before, and the view says so", async (t) => {
	const f = fixture(t);
	const session = SessionManager.inMemory(f.main);
	session.appendCustomEntry(SESSION_WORKTREE_ENTRY, { sessionId: session.getSessionId(), root: f.main, evidence: "session:cwd" });
	const { registry } = sessionAt(f.main, session);
	assert.deepEqual(registry.roots(), [f.main], "the saved session still restores its root");
	assert.deepEqual(registrations(session), [{ sessionId: session.getSessionId(), root: f.main, evidence: "session:cwd" }], "and gains no base on resume");
	writeFileSync(join(f.main, "c.txt"), "c\n");
	f.commit(f.main, "committed");
	const tracker = trackerOf(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), {}, "committed work is not listed without a base");
	writeFileSync(join(f.main, "a.txt"), "one\ntwo\n");
	await tracker.refresh();
	assert.deepEqual(rows(tracker), { main: ["modified a.txt +1 -0"] });
	assert.deepEqual(tracker.worktrees[0]!.comparison, { against: "head", fallback: CHANGES_FALLBACK.NO_BASE });
	const text = viewText(tracker);
	assert.match(text, /main · main · vs HEAD/);
	assert.match(paneText(tracker, 220), /Compared with HEAD \(uncommitted changes only\): the session kept no base for this root \(it was registered before bases were kept or before its first commit, or is known only from an edit\)\./);
});

test("the one rule holds for a registered worktree as for the session's folder, and a root back at its base's content is hidden", async (t) => {
	const f = fixture(t);
	const { session, registry } = sessionAt(f.main);
	const linked = f.worktree("linked");
	registry.register(linked, "explicit");
	const tracker = trackerOf(session, f.main, f.env);
	writeFileSync(join(linked, "a.txt"), "one\nworker\n");
	f.commit(linked, "the worker's slice");
	writeFileSync(join(f.main, "a.txt"), "one\nowner\n");
	f.commit(f.main, "the orchestrator's own commit");
	await tracker.start();
	assert.deepEqual(rows(tracker), { main: ["modified a.txt +1 -0"], linked: ["modified a.txt +1 -0"] });
	assert.match(viewText(tracker), /Compared with the session's base [0-9a-f]{7}: committed and uncommitted changes\./);
	assert.doesNotMatch(viewText(tracker), /vs HEAD/);
	// Both go back to the base's content, one committed and one not.
	writeFileSync(join(linked, "a.txt"), "one\n");
	f.commit(linked, "undone");
	writeFileSync(join(f.main, "a.txt"), "one\n");
	await tracker.refresh();
	assert.deepEqual(rows(tracker), {}, "nothing differs from the base: hidden");
	assert.deepEqual([tracker.model.files.length, tracker.failedRoots.length, tracker.scannedRootCount], [0, 0, 2]);
});

test("the diff a file shows is against the same base as its count", async (t) => {
	const f = fixture(t);
	const first = f.head(f.main);
	const { session } = sessionAt(f.main);
	writeFileSync(join(f.main, "a.txt"), "one\ncommitted\n");
	f.commit(f.main, "second");
	writeFileSync(join(f.main, "a.txt"), "one\ncommitted\npending\n");
	writeFileSync(join(f.main, "notes.md"), "untracked\n");
	const tracker = trackerOf(session, f.main, f.env);
	await tracker.start();
	const [edited, untracked] = tracker.worktrees[0]!.model.files;
	assert.deepEqual([edited!.path, edited!.added, edited!.diffBase], ["a.txt", 2, first]);
	const diff = await loadFileDiff(shellGitRunner(f.main, f.env), edited!);
	assert.deepEqual(diff.split("\n").filter((line) => /^[+-][^+-]/.test(line)), ["+committed", "+pending"], "the committed line and the pending one, as counted");
	assert.equal(untracked!.diffBase, undefined);
	assert.match(await loadFileDiff(shellGitRunner(f.main, f.env), untracked!), /^\+untracked$/m);
});

// GAP-3 (L943): numstat is read with -z, as the names are.
test("a file whose name Git would quote (not plain ASCII, a tab, a newline) counts its lines, committed or dirty: the totals are git diff --numstat's", async (t) => {
	const f = fixture(t);
	const names = ["año.txt", "tab\tname.txt", "line\nbreak.txt"];
	const ten = Array.from({ length: 10 }, (_, index) => `${index + 1}\n`).join("");
	for (const name of names) writeFileSync(join(f.main, name), ten);
	f.commit(f.main, "names Git quotes");
	const { session } = sessionAt(f.main);
	const base = f.head(f.main);
	const byHand = () => f.git(f.main, "diff", "--numstat", base).split("\n").filter(Boolean).reduce(([added, deleted], line) => { const [a, d] = line.split("\t"); return [added + Number(a), deleted + Number(d)]; }, [0, 0]);
	const tracker = trackerOf(session, f.main, f.env);
	// Dirty, the root at its base: the mismatch that predates bases.
	for (const name of names) writeFileSync(join(f.main, name), `${ten}dos\ntres\n`);
	await tracker.start();
	assert.deepEqual(rows(tracker), { main: ["modified año.txt +2 -0", "modified line\nbreak.txt +2 -0", "modified tab\tname.txt +2 -0"] });
	assert.deepEqual([tracker.model.added, tracker.model.deleted], byHand());
	// Committed, the root ahead of its base; one renamed with its two new lines (numstat's rename form), one edited again.
	renameSync(join(f.main, "line\nbreak.txt"), join(f.main, "línea nueva.txt"));
	f.commit(f.main, "committed");
	writeFileSync(join(f.main, "año.txt"), `${ten.slice(2)}dos\ntres\ncuatro\n`);
	await tracker.refresh();
	assert.deepEqual(rows(tracker), { main: ["modified año.txt +3 -1", "renamed línea nueva.txt +2 -0", "modified tab\tname.txt +2 -0"] });
	assert.deepEqual([tracker.model.added, tracker.model.deleted], byHand());
	assert.deepEqual([tracker.model.added, tracker.model.deleted], [7, 1]);
});

// The cost of a scan (L943): no Git call per file, and no more calls than these.
test("a scan asks Git three times for a root at its base and four for one ahead of it, never once per file", async (t) => {
	const f = fixture(t);
	const { session } = sessionAt(f.main);
	const base = f.head(f.main);
	const calls: string[][] = [];
	const id = session.getSessionId();
	const git = (root: string) => { const run = shellGitRunner(root, f.env); return (args: string[]) => { calls.push(args); return run(args); }; };
	const tracker = new WorktreeChangesTracker(git(f.main), git, lineCount, () => sessionChangeRoots(id, session.getEntries() as never[]), undefined, () => sessionRootBases(id, session.getEntries() as never[]));
	for (let index = 0; index < 12; index++) writeFileSync(join(f.main, `file-${index}.txt`), "x\n");
	writeFileSync(join(f.main, "a.txt"), "one\ntwo\n");
	await tracker.start();
	assert.equal(tracker.model.files.length, 13);
	const status = ["status", "--porcelain=v1", "--untracked-files=all", "-z"];
	const placed = ["rev-list", "--left-right", "--count", `${base}...HEAD`];
	assert.deepEqual(calls, [["worktree", "list", "--porcelain", "-z"], placed, ["diff", "--numstat", "-z", base], status], "at its base: the discovery, then three for the root, and no names asked");
	f.commit(f.main, "thirteen files committed");
	calls.length = 0;
	await tracker.refresh();
	assert.equal(tracker.model.files.length, 13);
	assert.deepEqual(calls, [["worktree", "list", "--porcelain", "-z"], placed, ["diff", "--numstat", "-z", base], status, ["diff", "--name-status", "-z", base]], "ahead of it: one more, for the names");
});

test("a names read that fails is a failed root, never a shorter list", async (t) => {
	const f = fixture(t);
	const { session } = sessionAt(f.main);
	writeFileSync(join(f.main, "a.txt"), "one\ntwo\n");
	f.commit(f.main, "committed");
	writeFileSync(join(f.main, "notes.md"), "untracked\n");
	const id = session.getSessionId();
	let broken = true;
	const git = (root: string) => { const run = shellGitRunner(root, f.env); return async (args: string[]) => broken && args.includes("--name-status") ? { stdout: "", code: 128 } : run(args); };
	const tracker = new WorktreeChangesTracker(git(f.main), git, lineCount, () => sessionChangeRoots(id, session.getEntries() as never[]), undefined, () => sessionRootBases(id, session.getEntries() as never[]));
	await tracker.start();
	assert.deepEqual(rows(tracker), {}, "not the untracked file alone");
	assert.deepEqual([tracker.failedRoots, tracker.scannedRootCount], [[f.main], 1]);
	broken = false;
	await tracker.refresh();
	assert.deepEqual(rows(tracker), { main: ["modified a.txt +1 -0", "untracked notes.md +1 -0"] });
	assert.deepEqual(tracker.failedRoots, []);
});

// GAP-2's bounds (L943): only a sub-agent's root is folded into another, and only into one that shows its work.
function subagentTracker(session: { getSessionId(): string; getEntries(): readonly unknown[] }, cwd: string, env: NodeJS.ProcessEnv) {
	const id = session.getSessionId();
	const git = (root: string) => shellGitRunner(root, env);
	const entries = () => session.getEntries() as never[];
	return new WorktreeChangesTracker(git(cwd), git, lineCount, () => sessionChangeRoots(id, entries()).filter((root) => existsSync(join(root, ".git"))), undefined, () => sessionRootBases(id, entries()), () => sessionSubagentRoots(id, entries()));
}

test("merged work is folded only for a sub-agent's root: a worktree registered by hand stays listed after its branch is merged", async (t) => {
	const f = fixture(t);
	const { session, registry } = sessionAt(f.main);
	const [byHand, child] = [f.worktree("by-hand"), f.worktree("child")];
	registry.register(byHand, "explicit");
	registry.registerChild(child);
	writeFileSync(join(byHand, "h.txt"), "h\n");
	f.commit(byHand, "by hand");
	writeFileSync(join(child, "c.txt"), "c\n");
	f.commit(child, "child");
	f.git(f.main, "merge", "-q", "--no-edit", "by-hand");
	f.git(f.main, "merge", "-q", "--no-edit", "child");
	const tracker = subagentTracker(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), { main: ["added c.txt +1 -0", "added h.txt +1 -0"], "by-hand": ["added h.txt +1 -0"] });
});

test("a sub-agent's root stays listed when the root that holds its HEAD was registered after it", async (t) => {
	const f = fixture(t);
	const { session, registry } = sessionAt(f.main);
	const child = f.worktree("child");
	registry.registerChild(child);
	writeFileSync(join(child, "c.txt"), "c\n");
	f.commit(child, "child");
	// A worktree started from the child's work and registered afterwards: its base already holds c.txt.
	const later = join(f.dir, "later");
	f.git(f.main, "worktree", "add", "-q", "-b", "later", later, "child");
	registry.register(later, "explicit");
	writeFileSync(join(later, "l.txt"), "l\n");
	f.commit(later, "later");
	const tracker = subagentTracker(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), { child: ["added c.txt +1 -0"], later: ["added l.txt +1 -0"] }, "the later root holds the child's HEAD and does not show c.txt");
});

test("a sub-agent's root stays listed when the root that holds its HEAD is read against HEAD", async (t) => {
	const f = fixture(t);
	const session = SessionManager.inMemory(f.main);
	// The session's own root kept a base that no longer exists: it lists uncommitted work only.
	session.appendCustomEntry(SESSION_WORKTREE_ENTRY, { sessionId: session.getSessionId(), root: f.main, evidence: "session:cwd", base: "0123456789abcdef0123456789abcdef01234567" });
	const { registry } = sessionAt(f.main, session);
	const child = f.worktree("child");
	registry.registerChild(child);
	writeFileSync(join(child, "c.txt"), "c\n");
	f.commit(child, "child");
	f.git(f.main, "merge", "-q", "--no-edit", "child");
	writeFileSync(join(f.main, "pending.txt"), "p\n");
	const tracker = subagentTracker(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), { main: ["untracked pending.txt +1 -0"], child: ["added c.txt +1 -0"] }, "the merge is in main's HEAD, which main does not show");
});

// BREAKS-1 and GAP-A (L950): holding the child's HEAD is not showing its files.
test("a sub-agent's root is not folded into a root that holds its HEAD but no longer shows its files: its merge was reverted", async (t) => {
	const f = fixture(t);
	const { session, registry } = sessionAt(f.main);
	const child = f.worktree("child");
	registry.registerChild(child);
	writeFileSync(join(child, "c1.txt"), "c\n");
	writeFileSync(join(child, "c2.txt"), "c\n");
	f.commit(child, "the child's slice");
	writeFileSync(join(f.main, "m.txt"), "m\n");
	f.commit(f.main, "the session's own");
	f.git(f.main, "merge", "-q", "--no-ff", "--no-edit", "child");
	f.git(f.main, "revert", "-m", "1", "--no-edit", "HEAD");
	const tracker = subagentTracker(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), { main: ["added m.txt +1 -0"], child: ["added c1.txt +1 -0", "added c2.txt +1 -0"] });
	assert.deepEqual(tracker.foldedRoots, []);
});

test("a sub-agent's root is not folded into a root whose base already holds its work: a worktree made behind the session's HEAD that fast-forwards", async (t) => {
	const f = fixture(t);
	writeFileSync(join(f.main, "later.txt"), "l\n");
	f.commit(f.main, "the commit the session starts on");
	const { session, registry } = sessionAt(f.main);
	const child = join(f.dir, "child");
	f.git(f.main, "worktree", "add", "-q", "-b", "child", child, "HEAD~1");
	registry.registerChild(child);
	f.git(child, "merge", "-q", "--ff-only", "main");
	writeFileSync(join(f.main, "dirty.txt"), "d\n");
	const tracker = subagentTracker(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), { main: ["untracked dirty.txt +1 -0"], child: ["added later.txt +1 -0"] });
});

test("a sub-agent's root is not folded when an uncommitted edit in the root that holds its HEAD puts one of its files back at the base's content", async (t) => {
	const f = fixture(t);
	const { session, registry } = sessionAt(f.main);
	const child = f.worktree("child");
	registry.registerChild(child);
	writeFileSync(join(child, "a.txt"), "one\nchild\n");
	writeFileSync(join(child, "c.txt"), "c\n");
	f.commit(child, "the child's slice");
	f.git(f.main, "merge", "-q", "--no-edit", "child");
	writeFileSync(join(f.main, "a.txt"), "one\n");
	const tracker = subagentTracker(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), { main: ["added c.txt +1 -0"], child: ["modified a.txt +1 -0", "added c.txt +1 -0"] }, "main no longer lists a.txt, so the child's root keeps it in sight");
});

// Preserved (L950): the owner's sequence, and merges that copy the work instead of holding its HEAD.
test("the owner's sequence counts 1, 3, 3 and 3 files: the child's two commits, the merge into the session's branch, the worktree's removal", async (t) => {
	const f = fixture(t);
	const { session, registry } = sessionAt(f.main);
	const child = f.worktree("child");
	registry.registerChild(child);
	const tracker = subagentTracker(session, f.main, f.env);
	await tracker.start();
	const counted = async () => { await tracker.refresh(); return [tracker.model.files.length, tracker.worktrees.map((tree) => basename(tree.root)).join("+")]; };
	writeFileSync(join(child, "c1.txt"), "c\n");
	f.commit(child, "first");
	assert.deepEqual(await counted(), [1, "child"]);
	writeFileSync(join(child, "c2.txt"), "c\n");
	writeFileSync(join(child, "c3.txt"), "c\n");
	f.commit(child, "second");
	assert.deepEqual(await counted(), [3, "child"]);
	f.git(f.main, "merge", "-q", "--no-edit", "child");
	assert.deepEqual(await counted(), [3, "main"], "merged: the session's root lists every file of the child's, so the child's root folds into it");
	assert.deepEqual(tracker.foldedRoots, [{ root: child, into: f.main }]);
	f.git(f.main, "worktree", "remove", "--force", child);
	assert.deepEqual(await counted(), [3, "main"]);
	assert.deepEqual(tracker.foldedRoots, []);
});

test("a squash merge and a cherry-pick still list both roots: the session's branch does not hold the child's HEAD", async (t) => {
	const f = fixture(t);
	const { session, registry } = sessionAt(f.main);
	const [squashed, picked] = [f.worktree("squashed"), f.worktree("picked")];
	registry.registerChild(squashed);
	registry.registerChild(picked);
	writeFileSync(join(squashed, "s.txt"), "s\n");
	f.commit(squashed, "to squash");
	writeFileSync(join(picked, "p.txt"), "p\n");
	f.commit(picked, "to pick");
	f.git(f.main, "merge", "-q", "--squash", "squashed");
	f.git(f.main, "commit", "-q", "-m", "squashed");
	f.git(f.main, "cherry-pick", "picked");
	const tracker = subagentTracker(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), { main: ["added p.txt +1 -0", "added s.txt +1 -0"], squashed: ["added s.txt +1 -0"], picked: ["added p.txt +1 -0"] });
});

// GAP-B (L950): a clean root read against HEAD is shown beside the roots that have changes.
test("a clean root read against HEAD has a row with no files in the view while another root has changes, with its mark and its sentence", async (t) => {
	const f = fixture(t);
	const session = SessionManager.inMemory(f.main);
	session.appendCustomEntry(SESSION_WORKTREE_ENTRY, { sessionId: session.getSessionId(), root: f.main, evidence: "session:cwd" });
	const { registry } = sessionAt(f.main, session);
	const linked = f.worktree("linked");
	registry.register(linked, "explicit");
	writeFileSync(join(f.main, "a.txt"), "one\ntwo\n");
	f.commit(f.main, "a commit the session cannot count");
	writeFileSync(join(linked, "l.txt"), "l\n");
	const tracker = subagentTracker(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(rows(tracker), { linked: ["untracked l.txt +1 -0"] }, "the counted roots are those with files");
	assert.deepEqual(tracker.shownRoots.map((tree) => [basename(tree.root), tree.branch, tree.model.files.length, tree.comparison?.against]), [["linked", "linked", 1, "base"], ["main", "main", 0, "head"]]);
	assert.deepEqual(tracker.model.files.map((file) => file.path), ["l.txt"], "one counted root: plain paths, as before");
	const view = new WorktreeChangesView(tracker.shownRoots, { theme: plain, rows: 12, loadDiff: async () => "", onOpen() {}, onClose() {}, onRefresh() {}, requestRender() {} });
	try {
		assert.match(view.render(220).join("\n"), /▸ linked · linked *│[^\n]*\n│ +▸ main · main · vs HEAD /);
		view.handleInput("j");
		const pane = view.render(220).slice(1, -2).map((row) => row.split("│")[2]!.trim()).join(" ");
		assert.ok(pane.startsWith(`${f.main} No uncommitted changes. Compared with HEAD (uncommitted changes only): the session kept no base for this root (`), pane);
		assert.doesNotMatch(pane, /Expand a group/);
		view.handleInput("\r");
		assert.match(view.render(220).join("\n"), /▾ main · main · vs HEAD/, "it opens on nothing");
	} finally { view.dispose(); }
	// With nothing listed anywhere there is no view: the notice says it.
	rmSync(join(linked, "l.txt"));
	await tracker.refresh();
	assert.deepEqual(tracker.shownRoots, []);
});

// Preserved behaviour.

test("the session's own uncommitted edits show exactly as they did before bases were kept", async (t) => {
	const f = fixture(t);
	writeFileSync(join(f.main, "staged.txt"), "s\n");
	f.commit(f.main, "more files");
	const { session } = sessionAt(f.main);
	const legacy = SessionManager.inMemory(f.main);
	legacy.appendCustomEntry(SESSION_WORKTREE_ENTRY, { sessionId: legacy.getSessionId(), root: f.main, evidence: "session:cwd" });
	// Unstaged, staged, staged then edited, deleted, added to the index, untracked.
	writeFileSync(join(f.main, "a.txt"), "one\ntwo\n");
	writeFileSync(join(f.main, "staged.txt"), "s\nstaged\n");
	f.git(f.main, "add", "staged.txt");
	writeFileSync(join(f.main, "staged.txt"), "s\nstaged\nthen edited\n");
	rmSync(join(f.main, "gone.txt"));
	writeFileSync(join(f.main, "indexed.txt"), "i\n");
	f.git(f.main, "add", "indexed.txt");
	mkdirSync(join(f.main, "dir"));
	writeFileSync(join(f.main, "dir", "new file.md"), "n\nn\n");
	const [now, before] = [trackerOf(session, f.main, f.env), trackerOf(legacy, f.main, f.env)];
	await Promise.all([now.start(), before.start()]);
	const expected = { main: ["modified a.txt +1 -0", "untracked dir/new file.md +2 -0", "deleted gone.txt +0 -1", "added indexed.txt +1 -0", "modified staged.txt +2 -0"] };
	assert.deepEqual(rows(before), expected, "what an entry without a base reads, the code path of before");
	assert.deepEqual(rows(now), expected, "a root at its base reads the same");
	assert.deepEqual(now.model.files.map(({ diffBase: _base, ...file }) => file), before.model.files);
});

test("a registered root that is clean and at its base shows nothing", async (t) => {
	const f = fixture(t);
	const { session, registry } = sessionAt(f.main);
	registry.register(f.worktree("linked"), "explicit");
	const tracker = trackerOf(session, f.main, f.env);
	await tracker.start();
	assert.deepEqual(tracker.worktrees, []);
	assert.deepEqual([tracker.model.files.length, tracker.failedRoots.length, tracker.scannedRootCount], [0, 0, 2]);
});
