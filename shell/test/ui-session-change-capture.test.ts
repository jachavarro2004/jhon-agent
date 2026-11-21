import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, writeFile, rm, realpath, symlink, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { generateUnifiedPatch } from "@earendil-works/pi-coding-agent";
import * as captureModule from "../lib/ui/session-change-capture.ts";
import * as changesModule from "../lib/ui/session-changes.ts";
import { installSessionChangeCapture } from "../lib/ui/session-change-capture.ts";
import { SessionChanges, SESSION_CHANGE_ENTRY } from "../lib/ui/session-changes.ts";

// The capture follows one write or edit from its call to its end, against a
// real folder; the clone check is a stub that puts the folder in one clone.

type Handler = (event: unknown, ctx: unknown) => unknown;

async function harness(t: test.TestContext, options: { child?: boolean; clone?: (path: string) => string } = {}) {
	const root = await realpath(await mkdtemp(join(tmpdir(), "jhon-capture-")));
	t.after(() => rm(root, { recursive: true, force: true }));
	const handlers = new Map<string, Handler>();
	const entries: Array<{ type: string; customType: string; data: any }> = [];
	const listeners = new Map<string, (data: unknown) => void>();
	const pi = {
		on: (name: string, handler: Handler) => { handlers.set(name, handler); },
		appendEntry: (customType: string, data: unknown) => { entries.push({ type: "custom", customType, data }); },
		events: { on: (name: string, fn: (data: unknown) => void) => { listeners.set(name, fn); return () => listeners.delete(name); }, emit: (name: string, data: unknown) => listeners.get(name)?.(data) },
	};
	let sessionId = "s";
	const ctx = { cwd: root, sessionManager: { getSessionId: () => sessionId, getEntries: () => entries } };
	const clone = options.clone ?? (() => join(root, ".git"));
	installSessionChangeCapture(pi as never, options.child ? { JHON_PI_CHILD: "1" } : {}, (path) => ({ root, commonDir: clone(path) }));
	const fire = async (name: string, event: object = {}) => handlers.get(name)?.(event, ctx);
	await fire("session_start");
	/** One call, from its start to its end, with the file written as `write` says between them. */
	const run = async (toolName: string, input: Record<string, unknown>, id: string, effect?: () => Promise<void>, result: Record<string, unknown> = {}) => {
		await fire("tool_call", { toolCallId: id, toolName, input });
		await effect?.();
		await fire("tool_result", { toolCallId: id, toolName, input, isError: false, ...result });
		await fire("tool_execution_end", { toolCallId: id, toolName, isError: result.isError ?? false });
	};
	return { root, entries, listeners, handlers, pi, fire, run, switchSession: () => { sessionId = "other"; } };
}

// T132 (L932): a child's worktree is one of the session's roots, so nothing relays a child's evidence.
test("nothing listens for or publishes another process's evidence", async (t) => {
	const h = await harness(t);
	assert.deepEqual([...h.listeners.keys()], []);
	assert.equal("publishForeignSessionChange" in captureModule, false);
	assert.equal("SESSION_CHANGE_RELAY" in changesModule, false);
	h.pi.events.emit("jhon-shell:child-session-change", { sessionId: "s", evidence: { id: "x", root: "/e", path: "f", before: { kind: "absent" }, after: { kind: "text", text: "x\n" } } });
	assert.deepEqual(h.entries, []);
});

test("reads and other tools are not followed", async (t) => {
	const h = await harness(t);
	await h.run("read", { path: "missing" }, "r");
	await h.run("bash", { path: "x", command: "touch x" }, "b");
	await h.run("write", { file: "no-path-field", content: "x" }, "n");
	assert.deepEqual(h.entries, []);
});

test("a write is kept at its end, with the copy it left, not what came after", async (t) => {
	const h = await harness(t);
	await writeFile(join(h.root, "file"), "before\n");
	const input = { path: "file", content: "written\n" };
	await h.fire("tool_call", { toolCallId: "w", toolName: "write", input });
	await writeFile(join(h.root, "file"), input.content);
	await h.fire("tool_result", { toolCallId: "w", toolName: "write", input, isError: false });
	assert.equal(h.entries.length, 0, "nothing before the end");
	await h.fire("tool_execution_end", { toolCallId: "w", toolName: "write", isError: false });
	await writeFile(join(h.root, "file"), "edited later\n");
	assert.equal(h.entries[0]!.customType, SESSION_CHANGE_ENTRY);
	assert.deepEqual(h.entries[0]!.data, { sessionId: "s", evidence: { id: "w", root: h.root, path: "file", before: { kind: "text", text: "before\n" }, after: { kind: "text", text: "written\n" } } });
	assert.equal(new SessionChanges("s", h.entries).model.files.length, 1);
});

test("a write whose file does not hold what it wrote has no diff", async (t) => {
	const h = await harness(t);
	await h.run("write", { path: "a", content: "said\n" }, "w", () => writeFile(join(h.root, "a"), "other\n"));
	const unverified = { kind: "unavailable", reason: "No diff: the file's state before and after the tool could not be checked." };
	assert.deepEqual(h.entries[0]!.data.evidence, { id: "w", root: h.root, path: "a", before: unverified, after: unverified });
});

test("an edit is kept when its patch matches the copies, BOM and CRLF aside; otherwise it has no diff", async (t) => {
	const h = await harness(t);
	await writeFile(join(h.root, "e.ts"), "\uFEFFone\r\n");
	const patch = generateUnifiedPatch("e.ts", "one\n", "two\n");
	await h.run("edit", { path: "e.ts" }, "e1", () => writeFile(join(h.root, "e.ts"), "two\n"), { details: { patch } });
	assert.deepEqual(h.entries[0]!.data.evidence.after, { kind: "text", text: "two\n" });
	await h.run("edit", { path: "e.ts" }, "e2", () => writeFile(join(h.root, "e.ts"), "three\n"), { details: { patch: "not the patch" } });
	assert.equal(h.entries[1]!.data.evidence.after.kind, "unavailable");
});

test("a failed call, one whose session changed, or whose result names another path is not kept", async (t) => {
	const h = await harness(t);
	await h.run("write", { path: "a", content: "x\n" }, "fail", () => writeFile(join(h.root, "a"), "x\n"), { isError: true });
	const input = { path: "b", content: "y\n" };
	await h.fire("tool_call", { toolCallId: "moved", toolName: "write", input });
	await h.fire("tool_result", { toolCallId: "moved", toolName: "write", input: { path: "c", content: "y\n" }, isError: false });
	await h.fire("tool_execution_end", { toolCallId: "moved", toolName: "write", isError: false });
	await h.fire("tool_call", { toolCallId: "late", toolName: "write", input });
	h.switchSession();
	await h.fire("tool_result", { toolCallId: "late", toolName: "write", input, isError: false });
	await h.fire("tool_execution_end", { toolCallId: "late", toolName: "write", isError: false });
	assert.deepEqual(h.entries, []);
});

test("a symlinked target and a target in another clone are not followed; a new folder's file is, with a leading @", async (t) => {
	const h = await harness(t, { clone: (path) => (path.includes("vendor") ? "/other/.git" : "/repo/.git") });
	await writeFile(join(h.root, "real"), "x\n");
	await symlink(join(h.root, "real"), join(h.root, "link"));
	await h.run("write", { path: "link", content: "y\n" }, "l", () => writeFile(join(h.root, "real"), "y\n"));
	await mkdir(join(h.root, "vendor"));
	await h.run("write", { path: "vendor/v.ts", content: "v\n" }, "v", () => writeFile(join(h.root, "vendor", "v.ts"), "v\n"));
	assert.deepEqual(h.entries, []);
	await h.run("write", { path: "@new/deep/n.ts", content: "n\n" }, "n", async () => { await mkdir(join(h.root, "new", "deep"), { recursive: true }); await writeFile(join(h.root, "new", "deep", "n.ts"), "n\n"); });
	assert.deepEqual(h.entries.map((e) => [e.data.evidence.path, e.data.evidence.before.kind]), [["new/deep/n.ts", "absent"]]);
});

test("at most 32 calls are followed at once", async (t) => {
	const h = await harness(t);
	for (let i = 0; i < 33; i++) await h.fire("tool_call", { toolCallId: `c${i}`, toolName: "write", input: { path: `f${i}`, content: "z\n" } });
	for (let i = 0; i < 33; i++) {
		await writeFile(join(h.root, `f${i}`), "z\n");
		await h.fire("tool_result", { toolCallId: `c${i}`, toolName: "write", input: { path: `f${i}`, content: "z\n" }, isError: false });
		await h.fire("tool_execution_end", { toolCallId: `c${i}`, toolName: "write", isError: false });
	}
	assert.equal(h.entries.length, 32);
	assert.equal(h.entries.some((e) => e.data.evidence.id === "c32"), false);
});

// T132: a child's worktree is one of its parent's roots.
test("a child follows no call and leaves its results as they are", async (t) => {
	const h = await harness(t, { child: true });
	assert.deepEqual([...h.handlers.keys()], []);
	await h.run("write", { path: "n", content: "x\n" }, "w", () => writeFile(join(h.root, "n"), "x\n"), { details: { original: "kept" } });
	assert.deepEqual(h.entries, []);
});

test("the session's limits stop the records without any notice", async (t) => {
	const h = await harness(t);
	const notices: unknown[] = [];
	h.pi.events.on("jhon-shell:session-change", (data) => notices.push(data));
	const body = "x".repeat(60 * 1024) + "\n";
	for (let i = 0; i < 80; i++) await h.run("write", { path: `f${i}`, content: body }, `w${i}`, () => writeFile(join(h.root, `f${i}`), body));
	const kept = h.entries.filter((e) => e.customType === SESSION_CHANGE_ENTRY).length;
	assert.ok(kept > 0 && kept < 80, String(kept));
	assert.deepEqual(notices, []);
});
