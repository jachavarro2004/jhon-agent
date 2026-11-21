import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, writeFile, rm, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
	SessionChanges, changesModel, isSessionChangeEvidence, readChangeSnapshot, recentSessionRoots, sameSnapshot, sessionChangeRoots,
	sessionRootBases, sessionSubagentRoots, textSnapshot, MAX_CHANGE_BYTES, SESSION_CHANGE_ENTRY,
} from "../lib/ui/session-changes.ts";
import { SESSION_WORKTREE_ENTRY } from "../lib/ui/session-worktree-registry.ts";

// The session's own writes, counted from the copies each call left: never
// from Git, so what someone else changed in the same files never counts.

const text = (body: string) => ({ kind: "text" as const, text: body });
const absent = { kind: "absent" as const };
const missing = (reason: string) => ({ kind: "unavailable" as const, reason });
const call = (id: string, before: ReturnType<typeof text> | typeof absent | ReturnType<typeof missing> = text("one\n"), after: typeof before = text("two\n"), path = "app.ts", root = "/repo") => ({ id, root, path, before, after });

test("a new session has nothing, and reads nothing from disk", () => {
	const changes = new SessionChanges("s");
	assert.deepEqual(changes.model, { files: [], added: 0, deleted: 0 });
	assert.deepEqual(changes.worktrees, []);
});

test("lines count against the copy before the call, not against HEAD", () => {
	const changes = new SessionChanges("s");
	assert.equal(changes.record(call("a", text("someone's line\none\n"), text("someone's line\ntwo\n"))), true);
	assert.deepEqual(changes.model, { files: [{ path: "app.ts", status: "modified", added: 1, deleted: 1 }], added: 1, deleted: 1 });
});

test("content that looks like a diff header still counts as a line", () => {
	const changes = new SessionChanges("s");
	changes.record(call("a", text("-- dashes\n"), text("++ pluses\n")));
	assert.deepEqual([changes.model.added, changes.model.deleted], [1, 1]);
});

test("calls in a row on one file add up, and a revert to the first copy drops the file and its root", () => {
	const changes = new SessionChanges("s");
	changes.record(call("a"));
	changes.record(call("b", text("two\n"), text("three\n")));
	assert.deepEqual([changes.model.added, changes.model.deleted], [1, 1], "one line, one to three");
	changes.record(call("c", text("three\n"), text("one\n")));
	assert.deepEqual(changes.worktrees, []);
	changes.record(call("d", text("edited outside\n"), text("four\n")));
	assert.equal(changes.model.files[0]?.countsUnavailable, "The copies lost continuity (an external or unseen edit); no session diff.", "the kept final copy still shows an outside edit");
});

test("an edit nobody saw between two calls leaves the file without counts", () => {
	const changes = new SessionChanges("s");
	changes.record(call("a"));
	changes.record(call("b", text("not two\n"), text("five\n")));
	assert.deepEqual(changes.model.files, [{ path: "app.ts", status: "modified", added: 0, deleted: 0, countsUnavailable: "The copies lost continuity (an external or unseen edit); no session diff." }]);
});

test("added and deleted files; a call that changed nothing is counted but not kept", () => {
	const changes = new SessionChanges("s");
	changes.record(call("a", absent, text("new\n"), "new.ts", "/linked"));
	changes.record(call("b", text("gone\n"), absent, "old.ts", "/linked"));
	assert.equal(changes.record(call("c", text("same\n"), text("same\n"), "same.ts")), false);
	assert.deepEqual(changes.worktrees.map((tree) => [tree.root, tree.model.files.map((f) => `${f.status} ${f.path}`)]), [["/linked", ["added new.ts", "deleted old.ts"]]]);
});

test("with two roots each path carries its root; files sort by path", () => {
	const changes = new SessionChanges("s");
	changes.record(call("a", text("1\n"), text("2\n"), "z.ts", "/one"));
	changes.record(call("b", text("1\n"), text("2\n3\n"), "a.ts", "/two"));
	changes.record(call("c", text("1\n"), text("2\n"), "b.ts", "/one"));
	assert.deepEqual(changes.worktrees.map((tree) => [tree.root, tree.model.files.map((f) => f.path)]), [["/one", ["b.ts", "z.ts"]], ["/two", ["a.ts"]]]);
	assert.deepEqual(changes.model.files.map((f) => f.path), ["/one/b.ts", "/one/z.ts", "/two/a.ts"]);
	assert.deepEqual([changes.model.added, changes.model.deleted], [4, 3]);
	assert.deepEqual(changesModel([{ path: "b", added: 1, deleted: 2, status: "modified" }, { path: "a", added: 3, deleted: 0, status: "added" }]),
		{ files: [{ path: "a", added: 3, deleted: 0, status: "added" }, { path: "b", added: 1, deleted: 2, status: "modified" }], added: 4, deleted: 2 });
});

test("a reload takes only this session's entries, and each call once", () => {
	const entry = (sessionId: string, evidence: object) => ({ type: "custom", customType: SESSION_CHANGE_ENTRY, data: { sessionId, evidence } });
	const changes = new SessionChanges("s", [entry("s", call("a")), entry("s", call("a")), entry("other", call("b", undefined, undefined, "x.ts", "/x")), { type: "message", customType: SESSION_CHANGE_ENTRY }]);
	assert.equal(changes.model.files.length, 1);
	assert.equal(changes.record(call("a")), false, "the same call again");
	assert.equal(changes.model.added, 1);
});

test("a copy is refused for a large, binary, special, symlinked or unreadable file; a missing one is absent", async (t) => {
	const dir = await mkdtemp(join(tmpdir(), "jhon-changes-"));
	t.after(() => rm(dir, { recursive: true, force: true }));
	await writeFile(join(dir, "large"), Buffer.alloc(MAX_CHANGE_BYTES + 1, 65));
	await writeFile(join(dir, "binary"), Buffer.from([65, 0, 66]));
	await writeFile(join(dir, "latin1"), Buffer.from([0xe9, 0x0a]));
	await writeFile(join(dir, "plain"), "\uFEFFhi\n");
	await symlink(join(dir, "plain"), join(dir, "link"));
	assert.deepEqual(await readChangeSnapshot(join(dir, "large")), missing("Special or large file; no diff."));
	assert.deepEqual(await readChangeSnapshot(join(dir, "binary")), missing("Binary file; no line counts."));
	assert.deepEqual(await readChangeSnapshot(dir), missing("Special or large file; no diff."));
	assert.deepEqual(await readChangeSnapshot(join(dir, "latin1")), missing("Copy unavailable."));
	assert.deepEqual(await readChangeSnapshot(join(dir, "link")), missing("Copy unavailable."));
	assert.deepEqual(await readChangeSnapshot(join(dir, "plain")), text("hi\n"), "the decoder drops a BOM");
	assert.deepEqual(await readChangeSnapshot(join(dir, "missing")), absent);
});

test("a text copy has limits: 64 KiB, under 2000 newlines, no NUL", () => {
	const tooBig = missing("The file exceeds the session's diff limit.");
	assert.deepEqual(textSnapshot("x".repeat(MAX_CHANGE_BYTES)), text("x".repeat(MAX_CHANGE_BYTES)));
	assert.deepEqual(textSnapshot("é".repeat(MAX_CHANGE_BYTES / 2 + 1)), tooBig, "bytes, not characters");
	assert.equal(textSnapshot("\n".repeat(1999)).kind, "text");
	assert.deepEqual(textSnapshot("\n".repeat(2000)), tooBig);
	assert.deepEqual(textSnapshot("a\0b"), missing("Binary file; no line counts."));
});

test("a file without counts says why", () => {
	const changes = new SessionChanges("s");
	changes.record(call("a", missing("Binary file"), missing("Binary file"), "blob.bin"));
	assert.equal(changes.model.files[0]?.countsUnavailable, "Binary file");
	assert.equal(sameSnapshot(missing("x"), missing("x")), false, "two missing copies are never the same");
	assert.equal(sameSnapshot(absent, absent), true);
});

test("evidence that leaves its root or is malformed is refused", () => {
	const changes = new SessionChanges("s");
	for (const bad of [{ path: "../out" }, { path: ".git/config" }, { path: "a\\..\\b" }, { path: "/abs" }, { path: "" }, { root: "relative" }, { root: "/r\n" }, { id: "" }, { id: "x".repeat(257) },
		{ before: missing("x".repeat(201)) }, { after: { kind: "other" } }, { after: text("a\0") }]) {
		assert.equal(changes.record({ ...call("bad"), ...bad } as never), false, JSON.stringify(bad));
	}
	assert.equal(isSessionChangeEvidence(null), false);
	assert.equal(isSessionChangeEvidence(call("ok", missing("x".repeat(200)))), true);
});

test("a session records at most 256 calls", () => {
	const changes = new SessionChanges("s");
	for (let i = 0; i < 257; i++) changes.record(call(String(i), undefined, undefined, `${i}.ts`));
	assert.equal(changes.model.files.length, 256);
	assert.equal(changes.record(call("late", undefined, undefined, "late.ts")), false);
});

test("a session records at most 4 MiB of evidence", () => {
	const changes = new SessionChanges("s");
	const big = "y".repeat(60 * 1024);
	let kept = 0;
	for (let i = 0; i < 80; i++) if (changes.record(call(`w${i}`, absent, text(big), `f${i}`))) kept++;
	assert.ok(kept > 60 && kept < 80, String(kept));
});

const registered = (sessionId: string, root: unknown, evidence = "subagent:spawn", base?: unknown) => ({ type: "custom", customType: SESSION_WORKTREE_ENTRY, data: { sessionId, root, evidence, ...(base === undefined ? {} : { base }) } });
const captured = (sessionId: string, root: string) => ({ type: "custom", customType: SESSION_CHANGE_ENTRY, data: { sessionId, evidence: call(root, undefined, undefined, "f.ts", root) } });

test("the session's roots: its registered worktrees and the roots its writes changed, never another session's", () => {
	const entries = [registered("s", "/repo"), registered("s", "/linked"), captured("s", "/written"), captured("s", "/repo"),
		registered("o", "/theirs"), captured("o", "/theirs2"), registered("s", "relative"), { type: "custom", customType: SESSION_CHANGE_ENTRY, data: { sessionId: "s", evidence: { root: "/bad" } } }];
	assert.deepEqual(sessionChangeRoots("s", entries), ["/repo", "/linked", "/written"]);
	assert.deepEqual(recentSessionRoots("s", entries), ["/repo", "/written", "/linked"], "the last touched first");
	assert.deepEqual(sessionChangeRoots("nobody", entries), []);
});

// T132: a root's base and kind are those of its first registration in this session.
test("a root's base and whether a sub-agent runs there come from its first entry in this session", () => {
	const [a, b, c] = ["a".repeat(40), "b".repeat(40), "c".repeat(64)];
	const entries = [
		registered("o", "/repo", "session:cwd", b), registered("s", "/repo", "session:cwd", a), registered("s", "/repo", "explicit", b),
		registered("s", "/old", "explicit"), registered("s", "/old", "subagent:spawn", b),
		registered("s", "/child", "subagent:spawn", c), registered("s", "/short", "subagent:spawn", "abc123"), registered("s", "/named", "explicit", "main"),
		registered("s", "relative", "subagent:spawn", a), registered("o", "/theirs", "subagent:spawn", a),
	];
	assert.deepEqual([...sessionRootBases("s", entries)], [["/repo", a], ["/child", c]]);
	assert.deepEqual([...sessionSubagentRoots("s", entries)].sort(), ["/child", "/short", "relative"]);
	assert.deepEqual([...sessionRootBases("nobody", entries)], []);
});
