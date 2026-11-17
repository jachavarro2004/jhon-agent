/**
 * shell/lib/ldd-reminder.ts — pi's LDD reminder (T31 E, L256): where the
 * engine reports LDD active (`jhon status --json`'s ldd.active), the
 * first code file the agent edits or writes while the session has no change
 * selected gets one note on its result, never a block. Once per session, and
 * never again once a change exists; a child never gets one (extensions/jhon.ts).
 */
import { dirname, extname, isAbsolute, join, relative, resolve } from "node:path";
import { findUp } from "./ui/ldd-status.ts";

export const LDD_REMINDER = "jhon (LDD): this project uses LDD and this session has no change selected. Start with `jhon status`, then write the RED test first and run it through `jhon executions run <change>` while it fails, before the behaviour change. A reminder, not a refusal.";

/** Files that are no code: documents and images. The LDD record (openspec/) is none either. */
const NOT_CODE = new Set([".md", ".mdx", ".markdown", ".txt", ".rst", ".adoc", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico", ".pdf"]);

/** A layout's record: openspec/, or ldd/'s config.yaml, changes/ and specs/. */
const RECORD = /(^|[\\/])(openspec([\\/]|$)|ldd[\\/](config\.yaml$|changes[\\/]|specs[\\/]))/;

/**
 * Whether path, relative to cwd or absolute, is a code file a project with
 * an LDD config may hold: no document, no part of a layout's record, and an
 * openspec/ or ldd/ config.yaml in one of its folders. That is a cheap
 * filter only; whether LDD is active there is the engine's to say
 * (reminderAnswer), never this file's (L479).
 */
export function lddCodeFile(path: string, cwd: string): boolean {
	const file = isAbsolute(path) ? path : resolve(cwd, path);
	if (NOT_CODE.has(extname(file).toLowerCase()) || RECORD.test(relative(cwd, file))) return false;
	return findUp(dirname(file), join("openspec", "config.yaml")) !== undefined || findUp(dirname(file), join("ldd", "config.yaml")) !== undefined;
}

/**
 * The reminder's reading of `jhon status --json` in cwd: true when nothing
 * is to be said (LDD not set up there, or a change is selected), false when
 * LDD is active with no change, undefined when the engine did not answer.
 */
export function reminderAnswer(status: any): boolean | undefined {
	if (!status || typeof status !== "object") return undefined;
	if (status.ldd?.active === false) return true;
	if (status.ldd?.active !== true) return undefined;
	return typeof status.change === "string";
}

/**
 * The session's reminder: given an edit or write's tool name, input and cwd,
 * the note to append, or undefined. selected answers whether the session has
 * a change (STATUS's selection, else `jhon status`), or undefined when it
 * cannot tell; then nothing is said, and it asks again at the next edit.
 */
export function lddReminder(selected: (cwd: string) => Promise<boolean | undefined>): (toolName: string, input: Record<string, unknown>, cwd: string) => Promise<string | undefined> {
	let settled = false;
	return async (toolName, input, cwd) => {
		if (settled || (toolName !== "edit" && toolName !== "write")) return undefined;
		const path = typeof input.path === "string" ? input.path : typeof input.file_path === "string" ? input.file_path : undefined;
		if (!path || !lddCodeFile(path, cwd)) return undefined;
		const has = await selected(cwd).catch(() => undefined);
		if (has === undefined || settled) return undefined;
		settled = true;
		return has ? undefined : LDD_REMINDER;
	};
}
