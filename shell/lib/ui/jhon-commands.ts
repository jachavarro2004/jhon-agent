/**
 * shell/lib/ui/jhon-commands.ts — what pi's `/` list says of Jhon's
 * commands (L194): each command its own `/jhon:<option>` with one short
 * line, and an argument's values offered with a line each
 * (getArgumentCompletions, as the Gentle Shell's /gentle:scratch).
 */
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { findUp } from "./ldd-status.ts";

/** One value pi's autocomplete offers for a command's argument. */
export interface Completion {
	value: string;
	label: string;
	description?: string;
}

/** The items whose value starts with prefix, for a one-word argument; null when none match or a second word started. */
export function completeWord(items: readonly Completion[], prefix: string): Completion[] | null {
	const word = prefix.trimStart();
	if (/\s/.test(word)) return null;
	const hits = items.filter((item) => item.value.startsWith(word.toLowerCase()));
	return hits.length ? hits : null;
}

/** What a change without an intent says. */
const NO_INTENT = "An LDD change, with no intent written yet";

/** The first line under tasks.md's `## Intent`, or undefined. */
function intentLine(tasks: string): string | undefined {
	const lines = tasks.split("\n");
	const at = lines.findIndex((line) => line.trim() === "## Intent");
	if (at < 0) return undefined;
	for (const line of lines.slice(at + 1)) {
		if (line.startsWith("#")) return undefined;
		if (line.trim()) return line.trim();
	}
	return undefined;
}

/**
 * The LDD changes of the project cwd is in (openspec/changes, the archive
 * left out), each with its intent's first line: /jhon:status's values.
 */
export function lddChanges(cwd: string): Completion[] {
	const dir = findUp(cwd, join("openspec", "changes")) ?? findUp(cwd, join("ldd", "changes"));
	if (!dir) return [];
	let names: string[];
	try {
		names = readdirSync(dir, { withFileTypes: true }).filter((entry) => entry.isDirectory() && entry.name !== "archive").map((entry) => entry.name).sort();
	} catch {
		return [];
	}
	return names.map((name) => {
		let description = NO_INTENT;
		try { description = intentLine(readFileSync(join(dir, name, "tasks.md"), "utf8")) ?? NO_INTENT; } catch { /* no tasks.md yet */ }
		return { value: name, label: name, description };
	});
}

/** /jhon:profile's words, as `jhon profile --help` names them. */
export const PROFILE_ACTIONS: readonly Completion[] = Object.freeze([
	{ value: "list", label: "list", description: "Lists the model profiles" },
	{ value: "show", label: "show", description: "Shows a profile: show [<name>], the active one with no name" },
	{ value: "use", label: "use", description: "Makes a profile the active one: use <name>" },
]);
