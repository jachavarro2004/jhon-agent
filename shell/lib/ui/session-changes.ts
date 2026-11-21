/**
 * What the session's own write and edit calls changed, file by file and root
 * by root, from the copies taken before and after each call
 * (session-change-capture.ts records them as session entries). It tells the
 * LDD selection which roots the session touched, and the bar's `±N` how many
 * files. A sub-agent's work is not recorded here: the worktree it runs in is
 * one of the session's roots (session-worktree-registry.ts, T132).
 */
import { constants } from "node:fs";
import { open, type FileHandle } from "node:fs/promises";
import { isAbsolute } from "node:path";
import { generateUnifiedPatch } from "@earendil-works/pi-coding-agent";
import { SESSION_WORKTREE_ENTRY, SUBAGENT_WORKTREE_EVIDENCE, isCommitId } from "./session-worktree-registry.ts";

/** Lines added and deleted, in a file or summed over files. */
type LineCounts = { added: number; deleted: number };

/** One file the session's own writes changed, with its line counts; `countsUnavailable` says why they are zero. */
export type ChangedFile = LineCounts & { path: string; status: "added" | "modified" | "deleted"; countsUnavailable?: string };

/** A root's changed files, sorted by path, with their summed counts. */
export type ChangesModel = LineCounts & { files: ChangedFile[] };

/** One root and what changed in it. */
export type WorktreeChanges = { root: string; model: ChangesModel };

export function changesModel(list: readonly ChangedFile[]): ChangesModel {
	const totals: LineCounts = { added: 0, deleted: 0 };
	for (const { added, deleted } of list) {
		totals.added += added;
		totals.deleted += deleted;
	}
	return { files: list.slice().sort((a, b) => a.path.localeCompare(b.path)), ...totals };
}

export const SESSION_CHANGE_ENTRY = "jhon-shell.session-change/v1";
/** The largest file a copy is taken of. */
export const MAX_CHANGE_BYTES = 65_536;
/** How much one session records: calls, bytes of evidence, and lines per copy. */
const LIMITS = { records: 256, sessionBytes: 4 * 1024 * 1024, lines: 2000, reason: 200, id: 256, path: 4096 } as const;

const REASONS = {
	tooBig: "The file exceeds the session's diff limit.",
	binary: "Binary file; no line counts.",
	special: "Special or large file; no diff.",
	moving: "The file changed while its copy was read.",
	unreadable: "Copy unavailable.",
	unseenEdit: "The copies lost continuity (an external or unseen edit); no session diff.",
} as const;

/** A copy of a file: its text, its absence, or why no copy was taken. */
export type ChangeSnapshot =
	| { kind: "text"; text: string }
	| { kind: "absent" }
	| { kind: "unavailable"; reason: string };

/** What one write or edit call left as a record: the file (root and relative path) before and after it. */
export type SessionChangeEvidence = { id: string; root: string; path: string } & Record<"before" | "after", ChangeSnapshot>;

/** A session entry as far as this module reads it. */
type Entry = Readonly<{ type?: string; customType?: string; data?: unknown }>;

/** A file as the session left it: its first copy, its last, and why no diff can be drawn, if so. */
type FileState = Record<"before" | "after", ChangeSnapshot> & { unavailable?: string; diff?: string };

function unavailable(reason: string): ChangeSnapshot {
	const missing: ChangeSnapshot = { kind: "unavailable", reason };
	return missing;
}

/** Whether `text` has at least `count` newlines, counting no further. */
function hasNewlines(text: string, count: number): boolean {
	let found = 0;
	for (let at = text.indexOf("\n"); at >= 0; at = text.indexOf("\n", at + 1)) if (++found >= count) return true;
	return false;
}

/** A copy of a file's text, or why there is none: too large, too many lines, or binary. */
export function textSnapshot(content: string): ChangeSnapshot {
	const oversized = Buffer.byteLength(content) > MAX_CHANGE_BYTES || hasNewlines(content, LIMITS.lines);
	const refusal = oversized ? REASONS.tooBig : content.includes("\0") ? REASONS.binary : undefined;
	return refusal === undefined ? { kind: "text", text: content } : unavailable(refusal);
}

// Read only, never through a symlink, and never waiting on a FIFO's writer.
const SNAPSHOT_FLAGS = constants.O_RDONLY | constants.O_NONBLOCK | (constants.O_NOFOLLOW ?? 0);

/** A failed open or read: a missing file is absent, anything else has no copy. */
function failedCopy(error: unknown): ChangeSnapshot {
	const code = (error as { code?: unknown } | undefined)?.code;
	return code === "ENOENT" ? { kind: "absent" } : unavailable(REASONS.unreadable);
}

/** The opened file's text, refused for a non-file, an oversize file or one that changed under the read. */
async function copyOf(handle: FileHandle): Promise<ChangeSnapshot> {
	const info = await handle.stat();
	if (!info.isFile() || info.size > MAX_CHANGE_BYTES) return unavailable(REASONS.special);
	// One byte past the limit, so a file that grew since the stat shows as moving.
	const buffer = Buffer.alloc(MAX_CHANGE_BYTES + 1);
	const got = (await handle.read(buffer, 0, buffer.length, 0)).bytesRead;
	if (got !== info.size) return unavailable(REASONS.moving);
	return textSnapshot(new TextDecoder("utf-8", { fatal: true }).decode(buffer.subarray(0, got)));
}

/**
 * A copy of the one file a call names. It never follows a symlink, never
 * blocks on a FIFO and reads no folder; a file that is missing is absent.
 */
export async function readChangeSnapshot(file: string): Promise<ChangeSnapshot> {
	let handle: FileHandle;
	try {
		handle = await open(file, SNAPSHOT_FLAGS);
	} catch (error) {
		return failedCopy(error);
	}
	try {
		return await copyOf(handle);
	} catch (error) {
		return failedCopy(error);
	} finally {
		await handle.close().catch(() => undefined);
	}
}

function isSnapshot(value: unknown): value is ChangeSnapshot {
	if (!value || typeof value !== "object") return false;
	const snapshot = value as ChangeSnapshot;
	switch (snapshot.kind) {
		case "absent": return true;
		case "text": return typeof snapshot.text === "string" && textSnapshot(snapshot.text).kind === "text";
		case "unavailable": return typeof snapshot.reason === "string" && snapshot.reason.length <= LIMITS.reason;
		default: return false;
	}
}

/** A relative path inside its root: no `..` and nothing under `.git`. */
function staysInRoot(path: string): boolean {
	return !isAbsolute(path) && path.split(/[\\/]/).every((part) => part !== ".." && part !== ".git");
}

const sized = (value: unknown, max: number): value is string => typeof value === "string" && value.length > 0 && value.length <= max;

export function isSessionChangeEvidence(candidate: unknown): candidate is SessionChangeEvidence {
	if (typeof candidate !== "object" || candidate === null) return false;
	const { id, root, path, before, after } = candidate as { [K in keyof SessionChangeEvidence]?: unknown };
	if (!sized(id, LIMITS.id)) return false;
	if (typeof root !== "string" || root.length > LIMITS.path || !isAbsolute(root)) return false;
	if (!sized(path, LIMITS.path) || !staysInRoot(path)) return false;
	// No NUL or line break anywhere in the location: it is printed and joined into paths.
	if (/[\0\r\n]/.test(`${root}${path}`)) return false;
	return isSnapshot(before) && isSnapshot(after);
}

/** Two copies with the same content; an unavailable copy is never the same as anything. */
export const sameSnapshot = (a: ChangeSnapshot, b: ChangeSnapshot): boolean => {
	if (a.kind !== b.kind) return false;
	if (a.kind === "absent") return true;
	return a.kind === "text" && b.kind === "text" && a.text === b.text;
};
const contentOf = (snapshot: ChangeSnapshot) => (snapshot.kind === "text" ? snapshot.text : "");

/** The custom entries of this session's own records, with their data. */
function* ownEntries(sessionId: string, entries: readonly Entry[]): Generator<{ customType?: string; data: Record<string, unknown> }> {
	for (const entry of entries) {
		if (entry.type !== "custom" || !entry.data || typeof entry.data !== "object") continue;
		const data = entry.data as Record<string, unknown>;
		if (data.sessionId === sessionId) yield { customType: entry.customType, data };
	}
}

/** Each root's first worktree registration in this session, in the order they came. */
function firstRegistrations(sessionId: string, entries: readonly Entry[]): Map<string, Record<string, unknown>> {
	const first = new Map<string, Record<string, unknown>>();
	for (const { customType, data } of ownEntries(sessionId, entries)) {
		if (customType !== SESSION_WORKTREE_ENTRY || typeof data.root !== "string" || first.has(data.root)) continue;
		first.set(data.root, data);
	}
	return first;
}

/**
 * The roots this session works in: its registered worktrees (its own root,
 * those added by hand and those of its sub-agents) and the roots its own
 * writes changed. Only roots, never files.
 */
export function sessionChangeRoots(sessionId: string, log: readonly Entry[]): string[] {
	const found = new Set<string>();
	for (const { customType, data } of ownEntries(sessionId, log)) {
		let root: unknown;
		if (customType === SESSION_CHANGE_ENTRY) root = isSessionChangeEvidence(data.evidence) ? data.evidence.root : undefined;
		else if (customType === SESSION_WORKTREE_ENTRY) root = data.root;
		if (typeof root === "string" && isAbsolute(root)) found.add(root);
	}
	return Array.from(found);
}

/**
 * Each registered root's base (T132): the commit its HEAD was at when this
 * session first registered it, as that first entry holds it. A root whose
 * first entry has none (older, or no commit yet), and a root known only from
 * the session's writes, have no base.
 */
export function sessionRootBases(sessionId: string, entries: readonly Entry[]): Map<string, string> {
	const bases = new Map<string, string>();
	for (const [root, data] of firstRegistrations(sessionId, entries)) {
		if (isAbsolute(root) && isCommitId(data.base)) bases.set(root, data.base);
	}
	return bases;
}

/** The roots a sub-agent runs in (T132): those whose first registration is the `subagent` tool's. */
export function sessionSubagentRoots(sessionId: string, entries: readonly Entry[]): Set<string> {
	const roots = new Set<string>();
	for (const [root, data] of firstRegistrations(sessionId, entries)) if (data.evidence === SUBAGENT_WORKTREE_EVIDENCE) roots.add(root);
	return roots;
}

/** The session's roots, the one it touched last first. */
export function recentSessionRoots(sessionId: string, entries: readonly Entry[]): string[] {
	return sessionChangeRoots(sessionId, [...entries].reverse());
}

export class SessionChanges {
	readonly sessionId: string;
	/** Every call recorded so far, kept or not, and the bytes of their evidence. */
	private readonly calls = new Set<string>();
	private bytes = 0;
	private readonly roots = new Map<string, Map<string, FileState>>();
	private drawn: WorktreeChanges[] | undefined;

	constructor(sessionId: string, entries: readonly Entry[] = []) {
		this.sessionId = sessionId;
		this.restore(entries);
	}

	/** Records the evidence this session's entries hold. */
	restore(log: readonly Entry[]): void {
		for (const { customType, data } of ownEntries(this.sessionId, log)) {
			if (customType === SESSION_CHANGE_ENTRY && isSessionChangeEvidence(data.evidence)) this.record(data.evidence);
		}
	}

	/**
	 * Adds one call's evidence; true when it is new and changed the file. A
	 * call is counted once, and the session stops recording at its limits, so
	 * the session file cannot grow without end.
	 */
	record(evidence: Readonly<SessionChangeEvidence>): boolean {
		if (!isSessionChangeEvidence(evidence) || this.calls.has(evidence.id)) return false;
		const size = Buffer.byteLength(JSON.stringify(evidence));
		if (this.calls.size >= LIMITS.records || this.bytes + size > LIMITS.sessionBytes) return false;
		this.calls.add(evidence.id);
		this.bytes += size;
		if (sameSnapshot(evidence.before, evidence.after)) return false;
		let files = this.roots.get(evidence.root);
		if (!files) { files = new Map(); this.roots.set(evidence.root, files); }
		const earlier = files.get(evidence.path);
		const state: FileState = { before: earlier ? earlier.before : structuredClone(evidence.before), after: structuredClone(evidence.after), unavailable: earlier?.unavailable };
		// A file that changed between two of the session's calls has no diff of the session's own.
		if (earlier && !sameSnapshot(earlier.after, evidence.before)) state.unavailable = REASONS.unseenEdit;
		for (const copy of [evidence.before, evidence.after]) if (copy.kind === "unavailable") state.unavailable ??= copy.reason;
		// The final copy stays even when the session reverted the file, so a later outside edit still shows.
		this.patchOf(evidence.path, state);
		files.set(evidence.path, state);
		this.drawn = undefined;
		return true;
	}

	/** The file's diff, or the reason there is none. */
	private patchOf(path: string, state: FileState): string {
		if (state.unavailable) return state.unavailable;
		state.diff ??= generateUnifiedPatch(path, contentOf(state.before), contentOf(state.after));
		return state.diff;
	}

	private filesOf(root: string): ChangedFile[] {
		const changed: ChangedFile[] = [];
		for (const [path, state] of this.roots.get(root) ?? []) {
			if (!state.unavailable && sameSnapshot(state.before, state.after)) continue;
			const lines = this.patchOf(path, state).split("\n");
			const hunk = lines.findIndex((line) => line.startsWith("@@"));
			const body = hunk < 0 ? [] : lines.slice(hunk + 1);
			const count = (sign: string) => (state.unavailable ? 0 : body.filter((line) => line.startsWith(sign)).length);
			const status = state.before.kind === "absent" ? "added" : state.after.kind === "absent" ? "deleted" : "modified";
			changed.push({ path, status, added: count("+"), deleted: count("-"), ...(state.unavailable ? { countsUnavailable: state.unavailable } : {}) });
		}
		return changed;
	}

	/** Each root with changed files, in the order the session first touched them. */
	get worktrees(): WorktreeChanges[] {
		this.drawn ??= [...this.roots.keys()]
			.map((root) => ({ root, model: changesModel(this.filesOf(root)) }))
			.filter((tree) => tree.model.files.length > 0);
		return this.drawn;
	}

	/** Every changed file together; with more than one root, each path carries its root. */
	get model(): ChangesModel {
		const trees = this.worktrees;
		const files = trees.flatMap((tree) => tree.model.files.map((file) => ({ ...file, path: trees.length === 1 ? file.path : `${tree.root}/${file.path}` })));
		return changesModel(files);
	}
}
