/**
 * shell/lib/session-brief.ts — what pi's main session receives beside the
 * orchestrator in a project that uses LDD (T31 D): the binding lead Claude
 * Code's session gets at SessionStart, and the project's rules from its
 * openspec/config.yaml, both from `jhon hook session-brief <agent>`
 * (engine/internal/hook/agenthooks.go). Where LDD is not set up yet the
 * engine prints instead the one line that says so (its key stays
 * `inactive`), the text Claude Code's session start gets (T64, T72). A
 * child never asks (extensions/jhon.ts).
 */

export interface SessionBrief {
	/** Heads the orchestrator: its definition binds the session. */
	lead: string;
	/** The project's rules, read when the session first asks. */
	brief: string;
}

/** Where LDD is not set up yet: the engine's line that says so. */
export interface LddInactive {
	inactive: string;
}

/** The brief or the inactive line in the engine's output, or undefined when it printed neither. */
export function parseSessionBrief(stdout: string): SessionBrief | LddInactive | undefined {
	try {
		const v = JSON.parse(stdout);
		if (typeof v?.inactive === "string" && v.inactive) return { inactive: v.inactive };
		return typeof v?.lead === "string" && typeof v?.brief === "string" && v.lead && v.brief ? { lead: v.lead, brief: v.brief } : undefined;
	} catch {
		return undefined;
	}
}

/**
 * The session's briefs: run asks the engine for a folder's output once per
 * session and folder, so each turn's prompt stays the same, as Claude Code's
 * SessionStart reads it once. A failure, or output it cannot read, is
 * undefined: nothing is known, so nothing is said (L477).
 */
export function sessionBriefs(run: (cwd: string) => Promise<string>): (session: string, cwd: string) => Promise<SessionBrief | LddInactive | undefined> {
	const seen = new Map<string, Promise<SessionBrief | LddInactive | undefined>>();
	return (session, cwd) => {
		const key = `${session}\0${cwd}`;
		let brief = seen.get(key);
		if (!brief) {
			brief = run(cwd).then(parseSessionBrief, () => undefined);
			seen.set(key, brief);
		}
		return brief;
	};
}
