import { constants } from "node:fs";
import { open } from "node:fs/promises";
import { isAbsolute } from "node:path";
import { generateUnifiedPatch } from "@earendil-works/pi-coding-agent";
import { SESSION_WORKTREE_ENTRY, SUBAGENT_WORKTREE_EVIDENCE, isCommitId } from "./session-worktree-registry.ts";

// Captured write/edit evidence of a session's own tool calls. It tells the
// LDD selection which roots the session touched. A sub-agent's work is not
// captured here: the worktree it runs in is one of the session's roots
// (session-worktree-registry.ts, T132).

/** One file the session's own writes changed, with its line counts. */
export interface ChangedFile {
	path: string;
	added: number;
	deleted: number;
	status: "modified" | "added" | "deleted";
	countsUnavailable?: string;
}
/** A root's changed files, sorted by path, and their totals. */
export interface ChangesModel {
	files: ChangedFile[];
	added: number;
	deleted: number;
}
export interface WorktreeChanges {
	root: string;
	model: ChangesModel;
}

export function changesModel(changed: ChangedFile[]): ChangesModel {
	const files = [...changed].sort((a, b) => a.path.localeCompare(b.path));
	return { files, added: files.reduce((total, file) => total + file.added, 0), deleted: files.reduce((total, file) => total + file.deleted, 0) };
}
export const SESSION_CHANGE_ENTRY = "jhon-shell.session-change/v1";
export const MAX_CHANGE_BYTES = 64 * 1024;
const MAX_RECORDS = 256;
const MAX_SESSION_BYTES = 4 * 1024 * 1024;
const MAX_LINES = 2000;
export type ChangeSnapshot = { kind: "text"; text: string } | { kind: "absent" } | { kind: "unavailable"; reason: string };
export interface SessionChangeEvidence {
	id: string;
	root: string;
	path: string;
	before: ChangeSnapshot;
	after: ChangeSnapshot;
}
type Entry = { type?: string; customType?: string; data?: unknown };
interface FileState { before: ChangeSnapshot; after: ChangeSnapshot; unavailable?: string; diff?: string }

const unavailable = (reason: string): ChangeSnapshot => ({ kind: "unavailable", reason });
export function textSnapshot(text: string): ChangeSnapshot {
	if (Buffer.byteLength(text) > MAX_CHANGE_BYTES || text.split("\n", MAX_LINES + 1).length > MAX_LINES) return unavailable("The file exceeds the session's diff limit.");
	if (text.includes("\0")) return unavailable("Binary file; no line counts.");
	return { kind: "text", text };
}

/** Read only the named mutation target. Never traverse, follow symlinks, or open a FIFO for blocking I/O. */
export async function readChangeSnapshot(path: string): Promise<ChangeSnapshot> {
	let file: Awaited<ReturnType<typeof open>> | undefined;
	try {
		file = await open(path, constants.O_RDONLY | constants.O_NONBLOCK | (constants.O_NOFOLLOW ?? 0));
		const stat = await file.stat();
		if (!stat.isFile() || stat.size > MAX_CHANGE_BYTES) return unavailable("Special or large file; no diff.");
		const buffer = Buffer.alloc(MAX_CHANGE_BYTES + 1);
		const { bytesRead } = await file.read(buffer, 0, buffer.length, 0);
		if (bytesRead !== stat.size) return unavailable("The file changed while its copy was read.");
		return textSnapshot(new TextDecoder("utf-8", { fatal: true }).decode(buffer.subarray(0, bytesRead)));
	} catch (error) {
		return (error as NodeJS.ErrnoException).code === "ENOENT" ? { kind: "absent" } : unavailable("Copy unavailable.");
	} finally { await file?.close().catch(() => {}); }
}

function validSnapshot(value: unknown): value is ChangeSnapshot {
	if (!value || typeof value !== "object") return false;
	const v = value as ChangeSnapshot;
	return v.kind === "absent" || (v.kind === "text" && typeof v.text === "string" && textSnapshot(v.text).kind === "text") ||
		(v.kind === "unavailable" && typeof v.reason === "string" && v.reason.length <= 200);
}
export function isSessionChangeEvidence(value: unknown): value is SessionChangeEvidence {
	if (!value || typeof value !== "object") return false;
	const v = value as SessionChangeEvidence;
	return typeof v.id === "string" && v.id.length > 0 && v.id.length <= 256 &&
		typeof v.root === "string" && isAbsolute(v.root) && v.root.length <= 4096 &&
		typeof v.path === "string" && v.path.length > 0 && v.path.length <= 4096 &&
		!isAbsolute(v.path) && !/[\0\r\n]/.test(v.root + v.path) &&
		!v.path.split(/[\\/]/).some(part => part === ".." || part === ".git") &&
		validSnapshot(v.before) && validSnapshot(v.after);
}
export const sameSnapshot = (a: ChangeSnapshot, b: ChangeSnapshot): boolean =>
	a.kind === b.kind && (a.kind === "absent" || (a.kind === "text" && b.kind === "text" && a.text === b.text));
const snapshotText = (value: ChangeSnapshot) => value.kind === "text" ? value.text : "";
const shown = (state: FileState) => Boolean(state.unavailable) || !sameSnapshot(state.before, state.after);

/**
 * Roots this session works in: its registered worktrees (own root, explicit and
 * subagent registrations) and the roots of its own captured write/edit
 * evidence. Only roots, never files.
 */
export function sessionChangeRoots(sessionId: string, entries: readonly Entry[]): string[] {
	const roots = new Set<string>();
	for (const entry of entries) {
		if (entry.type !== "custom" || !entry.data || typeof entry.data !== "object") continue;
		const data = entry.data as { sessionId?: unknown; root?: unknown; evidence?: unknown };
		if (data.sessionId !== sessionId) continue;
		if (entry.customType === SESSION_WORKTREE_ENTRY && typeof data.root === "string" && isAbsolute(data.root)) roots.add(data.root);
		if (entry.customType === SESSION_CHANGE_ENTRY && isSessionChangeEvidence(data.evidence)) roots.add(data.evidence.root);
	}
	return [...roots];
}

/**
 * The base of each registered root (T132): the commit its HEAD was at when
 * this session first registered it, as the root's first entry holds it. A
 * root whose first entry has none (saved before bases were kept, or with no
 * commit yet) is absent, and so is a root known only from captured evidence.
 */
export function sessionRootBases(sessionId: string, entries: readonly Entry[]): Map<string, string> {
	const bases = new Map<string, string>();
	const seen = new Set<string>();
	for (const entry of entries) {
		if (entry.type !== "custom" || entry.customType !== SESSION_WORKTREE_ENTRY || !entry.data || typeof entry.data !== "object") continue;
		const data = entry.data as { sessionId?: unknown; root?: unknown; base?: unknown };
		if (data.sessionId !== sessionId || typeof data.root !== "string" || !isAbsolute(data.root) || seen.has(data.root)) continue;
		seen.add(data.root);
		if (isCommitId(data.base)) bases.set(data.root, data.base);
	}
	return bases;
}

/** The roots a sub-agent runs in (T132): those the `subagent` tool registered, by their first entry. */
export function sessionSubagentRoots(sessionId: string, entries: readonly Entry[]): Set<string> {
	const roots = new Set<string>();
	const seen = new Set<string>();
	for (const entry of entries) {
		if (entry.type !== "custom" || entry.customType !== SESSION_WORKTREE_ENTRY || !entry.data || typeof entry.data !== "object") continue;
		const data = entry.data as { sessionId?: unknown; root?: unknown; evidence?: unknown };
		if (data.sessionId !== sessionId || typeof data.root !== "string" || seen.has(data.root)) continue;
		seen.add(data.root);
		if (data.evidence === SUBAGENT_WORKTREE_EVIDENCE) roots.add(data.root);
	}
	return roots;
}

/** The session's worktree roots, the one it touched last first. */
export function recentSessionRoots(sessionId: string, entries: readonly Entry[]): string[] {
	const roots = sessionChangeRoots(sessionId, [...entries].reverse());
	return roots;
}

export class SessionChanges {
	private readonly seen = new Set<string>();
	private readonly files = new Map<string, Map<string, FileState>>();
	private bytes = 0;
	readonly sessionId: string;
	private cached: WorktreeChanges[] | undefined;
	constructor(sessionId: string, entries: readonly Entry[] = []) { this.sessionId = sessionId; this.restore(entries); }

	restore(entries: readonly Entry[]): void {
		for (const entry of entries) {
			if (entry.type !== "custom" || entry.customType !== SESSION_CHANGE_ENTRY) continue;
			const data = entry.data as { sessionId?: string; evidence?: unknown } | undefined;
			if (data?.sessionId === this.sessionId && isSessionChangeEvidence(data.evidence)) this.record(data.evidence);
		}
	}
	record(evidence: SessionChangeEvidence): boolean {
		if (!isSessionChangeEvidence(evidence) || this.seen.has(evidence.id)) return false;
		const bytes = Buffer.byteLength(JSON.stringify(evidence));
		// Bounds session-file growth.
		if (this.seen.size >= MAX_RECORDS || this.bytes + bytes > MAX_SESSION_BYTES) return false;
		this.seen.add(evidence.id);
		this.bytes += bytes;
		if (sameSnapshot(evidence.before, evidence.after)) return false;
		let files = this.files.get(evidence.root);
		if (!files) this.files.set(evidence.root, files = new Map());
		const previous = files.get(evidence.path);
		const state: FileState = { before: previous?.before ?? structuredClone(evidence.before), after: structuredClone(evidence.after) };
		state.unavailable = previous?.unavailable;
		if (previous && !sameSnapshot(previous.after, evidence.before)) state.unavailable = "The copies lost continuity (an external or unseen edit); no session diff.";
		if (evidence.before.kind === "unavailable") state.unavailable ??= evidence.before.reason;
		if (evidence.after.kind === "unavailable") state.unavailable ??= evidence.after.reason;
		// Keep the final state even after an own revert, to detect later external edits.
		this.patch(evidence.path, state);
		files.set(evidence.path, state);
		this.cached = undefined;
		return true;
	}
	private changed(root: string): ChangedFile[] {
		return [...(this.files.get(root) ?? [])].flatMap(([path, state]) => {
			if (!shown(state)) return [];
			const patch = this.patch(path, state);
			const patchLines = patch.split("\n");
			const firstHunk = patchLines.findIndex(line => line.startsWith("@@"));
			const lines = firstHunk < 0 ? [] : patchLines.slice(firstHunk + 1);
			return [{
				path, status: state.before.kind === "absent" ? "added" as const : state.after.kind === "absent" ? "deleted" as const : "modified" as const,
				added: state.unavailable ? 0 : lines.filter(line => line.startsWith("+")).length,
				deleted: state.unavailable ? 0 : lines.filter(line => line.startsWith("-")).length,
				...(state.unavailable ? { countsUnavailable: state.unavailable } : {}),
			}];
		});
	}
	private patch(path: string, state: FileState): string {
		if (state.unavailable) return state.unavailable;
		return state.diff ??= generateUnifiedPatch(path, snapshotText(state.before), snapshotText(state.after));
	}
	get worktrees(): WorktreeChanges[] {
		return this.cached ??= [...this.files.keys()].map(root => ({ root, model: changesModel(this.changed(root)) })).filter(tree => tree.model.files.length > 0);
	}
	get model() {
		const trees = this.worktrees;
		return changesModel(trees.flatMap(tree => tree.model.files.map(file => ({ ...file, path: trees.length === 1 ? file.path : tree.root + "/" + file.path }))));
	}
}
