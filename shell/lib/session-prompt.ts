/**
 * shell/lib/session-prompt.ts — T115c/T115d (L707, L708): the main session's
 * system prompt is byte-identical across turns, so the provider's prompt
 * cache holds on every turn, and its fixed base stays small.
 *
 * - The text appended at a session's first run is kept for the session: a
 *   rewritten orchestrator file (`jhon sync`) or a re-read brief does not
 *   rewrite the prompt mid-session (each rewrite was a full cache miss).
 * - The brief's route line follows the change's diff, so it leaves the
 *   prompt and reaches the model as a persisted message, sent once and again
 *   only when it changes.
 * - The project memory is an index (its MEMORY.md, one line per memory, long
 *   lines cut) under a short protocol, read once per session.
 * - The skills catalog keeps each description's opening, where the trigger is.
 */

import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { MEMORY_INDEX_LINES } from "./memory.ts";

/** The custom message that carries the brief's route line. */
export const ROUTE_MESSAGE = "jhon-route";

const ROUTE_LINE = /^- Route, from the change's diff now:.*$/m;

/** The brief without its route line, and that line; the route is undefined when the brief has none. */
export function splitRoute(brief: string): { rules: string; route?: string } {
	const match = ROUTE_LINE.exec(brief);
	if (!match) return { rules: brief };
	return { rules: (brief.slice(0, match.index) + brief.slice(match.index + match[0].length + 1)).replace(/\n+$/, ""), route: match[0] };
}

/** A longest index line in the prompt; MEMORY.md keeps the whole line. */
export const MEMORY_LINE_MAX = 160;

/** The project memory as an index under a short protocol (lib/memory.ts's memoryPrompt carries the long one). */
export function memoryIndexPrompt(dir: string): string {
	const index = join(dir, "MEMORY.md");
	let loaded = "";
	if (existsSync(index)) {
		const lines = readFileSync(index, "utf8").split("\n");
		loaded = lines.slice(0, MEMORY_INDEX_LINES).map((line) => (line.length > MEMORY_LINE_MAX ? `${line.slice(0, MEMORY_LINE_MAX - 1)}…` : line)).join("\n").trim();
		if (lines.length > MEMORY_INDEX_LINES) loaded += `\n\n(MEMORY.md has ${lines.length} lines; only the first ${MEMORY_INDEX_LINES} are loaded. Keep the index short.)`;
	}
	return [
		"## Project memory",
		"",
		`This project's file memory is \`${dir}/\`, shared with Claude Code; it is not Engram. One fact per file, frontmatter \`name\`, \`description\`, \`metadata.type\` (user | feedback | project | reference); then one line in MEMORY.md: \`- [Title](file.md) — hook\`. Update the file that covers a fact instead of adding one; read a memory before acting on it. The index below is read at the session's start.`,
		"",
		loaded ? `### MEMORY.md\n\n${loaded}` : "MEMORY.md does not exist yet.",
	].join("\n");
}

/** The longest skill description the catalog carries; the skill's file has the rest. */
export const SKILL_DESCRIPTION_MAX = 200;

interface CatalogSkill { description?: string }

/** Cuts each skill's description to its opening, in place. */
export function compactSkills(options: { skills?: CatalogSkill[] } | null | undefined): void {
	for (const skill of options?.skills ?? []) {
		const text = skill?.description;
		if (typeof text !== "string" || text.length <= SKILL_DESCRIPTION_MAX) continue;
		const cut = text.slice(0, SKILL_DESCRIPTION_MAX - 1);
		const space = cut.lastIndexOf(" ");
		skill.description = `${(space > SKILL_DESCRIPTION_MAX / 2 ? cut.slice(0, space) : cut).replace(/[\s,;:]+$/, "")}…`;
	}
}

/**
 * The session's prompt parts, computed at its first run in a folder and kept
 * until the next session starts; and the last route sent, so an unchanged
 * route is not sent again.
 */
export class SessionPrompt {
	private kept = new Map<string, string[]>();
	private route: string | undefined;
	private mode: "inline" | "writer" | undefined;

	/** T117c: the session's delegation mode, read once; the guard and the prompt read the same. */
	delegation(compute: () => "inline" | "writer"): "inline" | "writer" {
		if (this.mode === undefined) this.mode = compute();
		return this.mode;
	}

	/** The session's mode once read; undefined before. */
	frozenDelegation(): "inline" | "writer" | undefined {
		return this.mode;
	}

	/** The parts for cwd: computed once per session, then the same. */
	parts(cwd: string, compute: () => string[]): string[] {
		let parts = this.kept.get(cwd);
		if (!parts) {
			parts = compute();
			this.kept.set(cwd, parts);
		}
		return parts;
	}

	/** The route when it differs from the last one sent; undefined otherwise. */
	routeChange(route: string | undefined): string | undefined {
		if (!route || route === this.route) return undefined;
		this.route = route;
		return route;
	}

	/** A new session: everything is read again. */
	reset(): void {
		this.kept.clear();
		this.route = undefined;
		this.mode = undefined;
	}
}
