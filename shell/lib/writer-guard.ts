/**
 * shell/lib/writer-guard.ts — T117c (L720): in writer mode the main session
 * edits inline only a change cheaper than starting a builder, at most
 * WRITER_INLINE_LINES changed lines per owner request (T117e: summed over
 * its calls since the owner's last message); a larger edit or write is
 * refused toward a jhon-builder with a self-contained brief. The plan doc, the
 * Log and every file under odd/ always pass. Inline mode and children never
 * reach this check.
 */

import { existsSync, readFileSync } from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

/** The most lines one inline edit or write may change in writer mode. */
export const WRITER_INLINE_LINES = 20;

const lines = (text: string) => (text === "" ? [] : text.replace(/\n$/, "").split("\n"));

/** The lines that differ between before and after: their common head and tail left out, the larger side counted. */
export function changedLines(before: string, after: string): number {
	const a = lines(before), b = lines(after);
	let head = 0;
	while (head < a.length && head < b.length && a[head] === b[head]) head++;
	let tail = 0;
	while (tail < a.length - head && tail < b.length - head && a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail++;
	return Math.max(a.length - head - tail, b.length - head - tail);
}

/** The changed lines of a pi edit or write call; undefined for any other tool. */
export function callChangedLines(toolName: string, input: Record<string, unknown>, file: string): number | undefined {
	const text = (v: unknown) => (typeof v === "string" ? v : "");
	if (toolName === "write") {
		let current = "";
		try { if (existsSync(file)) current = readFileSync(file, "utf8"); } catch { current = ""; }
		return changedLines(current, text(input.content));
	}
	if (toolName !== "edit") return undefined;
	const edits = Array.isArray(input.edits) ? input.edits as Record<string, unknown>[] : [{ oldText: input.oldText, newText: input.newText }];
	return edits.reduce((sum, e) => sum + changedLines(text(e?.oldText), text(e?.newText)), 0);
}

/** Whether path, resolved against cwd, is under one of roots' odd/ folders. */
function underOdd(path: string, roots: string[]): boolean {
	return roots.some((root) => {
		const rel = relative(root, path);
		return !!rel && !rel.startsWith("..") && !isAbsolute(rel) && rel.split(sep)[0] === "odd";
	});
}

/** The changed lines a writer-mode call counts, or undefined when it does not count (another tool, no path, odd/**). */
export function writerCallLines(toolName: string, input: Record<string, unknown>, cwd: string, root: () => string): { lines: number; path: string } | undefined {
	if (toolName !== "edit" && toolName !== "write") return undefined;
	const raw = typeof input.path === "string" ? input.path.replace(/^@/, "") : "";
	if (!raw) return undefined;
	const file = resolve(cwd, raw);
	if (underOdd(file, [cwd, root()])) return undefined;
	return { lines: callChangedLines(toolName, input, file) ?? 0, path: raw };
}

function refusal(toolName: string, path: string, changed: number, prior: number): string {
	const total = prior ? `, ${prior + changed} with the ${prior} already written inline since the owner's last message,` : ",";
	return `jhon: writer mode: this ${toolName} changes ${changed} lines of ${path}${total} more than the ${WRITER_INLINE_LINES} the orchestrator writes inline per request. Hand the rest to a jhon-builder (subagent) with a self-contained brief: the spec lines, the exact files and line ranges, the decisions taken, and how to verify. The plan doc and the Log (odd/**) stay yours.`;
}

/** Why writer mode refuses this call in the main session on its own; undefined when it passes. */
export function writerRefusal(toolName: string, input: Record<string, unknown>, cwd: string, root: () => string): string | undefined {
	const call = writerCallLines(toolName, input, cwd, root);
	return call && call.lines > WRITER_INLINE_LINES ? refusal(toolName, call.path, call.lines, 0) : undefined;
}

/**
 * T117e (L746): the lines the main session wrote inline since the owner's
 * last message, summed over its edit and write calls; the call that would
 * pass WRITER_INLINE_LINES is refused. A call counts when it is let through
 * and is taken back if it fails; an owner message starts a new count.
 */
export class WriterBudget {
	private used = 0;
	private readonly pending = new Map<string, number>();

	/** The owner's new message: nothing written yet. */
	reset(): void {
		this.used = 0;
		this.pending.clear();
	}

	/** The lines written so far in this request, pending calls included. */
	total(): number {
		let n = this.used;
		for (const v of this.pending.values()) n += v;
		return n;
	}

	/** The refusal for this call, or undefined after counting it. */
	check(toolName: string, input: Record<string, unknown>, cwd: string, root: () => string, id?: string): string | undefined {
		const call = writerCallLines(toolName, input, cwd, root);
		if (!call) return undefined;
		const prior = this.total();
		if (prior + call.lines > WRITER_INLINE_LINES) return refusal(toolName, call.path, call.lines, prior);
		if (id) this.pending.set(id, call.lines);
		else this.used += call.lines;
		return undefined;
	}

	/** A counted call's result: kept when it succeeded, taken back when it failed. */
	settle(id: string | undefined, failed: boolean): void {
		if (!id || !this.pending.has(id)) return;
		if (!failed) this.used += this.pending.get(id)!;
		this.pending.delete(id);
	}
}
