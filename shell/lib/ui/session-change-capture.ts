/**
 * Records what the session's own write and edit calls changed: a copy of the
 * target before the call, one after it, kept as a session entry once the
 * call ended well (session-changes.ts reads them). Only the call's own
 * outcome counts, never Git status or what a shell printed. The roots in the
 * evidence are the ones the LDD selection follows. A sub-agent records
 * nothing: the worktree it runs in is one of its parent's roots (T132).
 */
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { generateUnifiedPatch } from "@earendil-works/pi-coding-agent";
import { lstat, realpath } from "node:fs/promises";
import { dirname, relative, resolve } from "node:path";
import { homedir } from "node:os";
import { resolveSessionWorktree } from "./session-worktree-registry.ts";
import { SessionChanges, readChangeSnapshot, sameSnapshot, textSnapshot,
	SESSION_CHANGE_ENTRY, type SessionChangeEvidence, type ChangeSnapshot } from "./session-changes.ts";

/** A write or edit in flight: where it writes, and the copy from before it. */
interface InFlight { sessionId: string; inputPath: string; path: string; root: string; relativePath: string; before: ChangeSnapshot; toolName: string; evidence?: SessionChangeEvidence }

/** At most this many calls are followed at once, and a missing folder is looked for this many levels up. */
const MAX_IN_FLIGHT = 32;
const MAX_LEVELS_UP = 32;

const unverified = (): ChangeSnapshot => ({ kind: "unavailable", reason: "No diff: the file's state before and after the tool could not be checked." });
/** Text as the edit tool compares it: no BOM, LF line ends. */
const asEdited = (text: string) => text.replace(/^\uFEFF/, "").replace(/\r\n/g, "\n");

/** A tool's path as pi spells it (a leading @, odd spaces, ~), absolute from the session's folder. */
function targetPath(cwd: string, spelled: string): string {
	let path = spelled.replace(/^@/, "").replace(/[\u00a0\u2000-\u200a\u202f\u205f\u3000]/g, " ");
	if (path === "~" || path.startsWith("~/")) path = homedir() + path.slice(1);
	return resolve(cwd, path);
}

/** The nearest folder above `path` that exists, or undefined when none does up to the filesystem root. */
async function existingFolderAbove(path: string): Promise<string | undefined> {
	let folder = dirname(path);
	for (let level = 0; level < MAX_LEVELS_UP; level++) {
		try {
			await lstat(folder);
			return folder;
		} catch {
			if (dirname(folder) === folder) return undefined;
			folder = dirname(folder);
		}
	}
	return folder;
}

/** Whether `path` is a symlink, or cannot be checked; a missing file is neither. */
async function refusedTarget(path: string): Promise<boolean> {
	try {
		return (await lstat(path)).isSymbolicLink();
	} catch (error) {
		return (error as NodeJS.ErrnoException).code !== "ENOENT";
	}
}

export function installSessionChangeCapture(pi: ExtensionAPI, env: NodeJS.ProcessEnv = process.env, resolver = resolveSessionWorktree): void {
	if (env.JHON_PI_CHILD === "1") return;
	const inFlight = new Map<string, InFlight>();
	let session: ExtensionContext | undefined;
	let changes: SessionChanges | undefined;
	const idOf = (ctx: ExtensionContext) => ctx.sessionManager.getSessionId();

	const keep = (evidence: SessionChangeEvidence) => {
		if (!session || !changes || idOf(session) !== changes.sessionId) return;
		if (changes.record(evidence)) pi.appendEntry(SESSION_CHANGE_ENTRY, { sessionId: changes.sessionId, evidence });
	};

	pi.on("session_start", (_event, ctx) => {
		inFlight.clear();
		session = ctx;
		changes = new SessionChanges(idOf(ctx), ctx.sessionManager.getEntries());
	});

	// Before the call: the target, in the session's own clone, and its copy.
	pi.on("tool_call", async (event, ctx) => {
		const spelled = (event.input as Record<string, unknown>).path;
		if ((event.toolName !== "write" && event.toolName !== "edit") || typeof spelled !== "string" || inFlight.size >= MAX_IN_FLIGHT) return;
		const sessionId = idOf(ctx);
		try {
			const path = targetPath(ctx.cwd, spelled);
			if (await refusedTarget(path)) return;
			const folder = await existingFolderAbove(path);
			if (folder === undefined) return;
			const realFolder = await realpath(folder);
			const realPath = resolve(realFolder, relative(folder, path));
			const own = resolver(ctx.cwd, ctx.cwd);
			const target = resolver(realFolder, ctx.cwd);
			if (!own || !target || own.commonDir !== target.commonDir) return;
			const before = await readChangeSnapshot(realPath);
			if (idOf(ctx) !== sessionId || (session && idOf(session)) !== sessionId) return;
			inFlight.set(event.toolCallId, { sessionId, inputPath: spelled, path: realPath, root: target.root, relativePath: relative(target.root, realPath), before, toolName: event.toolName });
		} catch { /* Bookkeeping never stops an edit. */ }
	});

	// After it: the copy from after, checked against what the call says it did.
	pi.on("tool_result", async (event, ctx) => {
		const call = inFlight.get(event.toolCallId);
		const same = call && call.sessionId === idOf(ctx) && call.toolName === event.toolName && event.input.path === call.inputPath && event.isError === false;
		if (!call || !same) { inFlight.delete(event.toolCallId); return; }
		try {
			const after = await readChangeSnapshot(call.path);
			let before = call.before;
			let kept = after;
			if (event.toolName === "write" && after.kind === "text") {
				const content = event.input.content;
				if (typeof content !== "string" || !sameSnapshot(after, textSnapshot(content))) { before = unverified(); kept = unverified(); }
			}
			if (event.toolName === "edit" && before.kind === "text" && after.kind === "text") {
				const patch = (event.details as { patch?: unknown } | undefined)?.patch;
				const input = event.input.path;
				if (typeof input !== "string" || patch !== generateUnifiedPatch(input, asEdited(before.text), asEdited(after.text))) { before = unverified(); kept = unverified(); }
			}
			if (call.sessionId !== idOf(ctx) || inFlight.get(event.toolCallId) !== call) return;
			call.evidence = { id: event.toolCallId, root: call.root, path: call.relativePath, before, after: kept };
		} catch {
			inFlight.delete(event.toolCallId);
		}
	});

	// Kept only once the call ended well, in the session it began in.
	pi.on("tool_execution_end", (event, ctx) => {
		const call = inFlight.get(event.toolCallId);
		inFlight.delete(event.toolCallId);
		if (event.isError !== false || !call?.evidence || call.sessionId !== idOf(ctx)) return;
		try { keep(call.evidence); } catch { /* The tool's outcome stands. */ }
	});

	pi.on("session_shutdown", () => {
		inFlight.clear();
		session = undefined;
		changes = undefined;
	});
}
