/**
 * shell/lib/session-profile.ts — T120 (L979 option A, L980): the profile one
 * session uses, apart from the default for new sessions.
 *
 * The engine keeps one default profile (settings' pi.profile, else
 * profiles.json's `active`) and renders the agent folder from it; every
 * session reads that folder. A session that uses another profile records its
 * name in a session entry (SESSION_PROFILE_ENTRY) and the shell overlays it:
 *
 * - a sub-agent it launches runs on the profile's model and thinking level
 *   for that agent, read from `jhon profile json` at each launch, in place of
 *   the ones its rendered file names;
 * - the orchestrator switches to the profile's model, and the delegation
 *   mode follows the profile (extensions/jhon.ts);
 * - the header, the status bar and the profiles panel show it, marked while
 *   it differs from the default.
 *
 * Nothing global is written: the default profile, the settings and the
 * rendered files stay as they are, so the other sessions are untouched. The
 * entry is read from the session's branch at each session start, so a
 * resumed session comes back with its profile and a new one starts on the
 * default. A sub-agent is its own pi process: it hears its parent session's
 * profile through SESSION_PROFILE_ENV, so the sub-agents it launches follow
 * it too. A profile that no longer exists (renamed or deleted) puts the
 * session back on the default.
 *
 * Claude Code sessions and `jhon review-native` read the default profile.
 */
import type { AgentDef } from "./agents.ts";

/** The session entry that records the profile a session uses: `{ profile: <name> }`, or `{ profile: null }` once it is back on the default. */
export const SESSION_PROFILE_ENTRY = "jhon-shell.session-profile/v1";
/** Said on pi.events when a session's profile changes: `{ sessionId, profile? }`. */
export const SESSION_PROFILE_CHANGED = "jhon-shell:session-profile-changed";
/** A sub-agent's parent session's profile, in the child's environment. */
export const SESSION_PROFILE_ENV = "JHON_PI_SESSION_PROFILE";
/** The agent whose row is the orchestrator's (the engine's pi main session). */
export const ORCHESTRATOR = "jhon-orchestrator";

/** One agent's pi model in a profile, split as the render writes it to the agent's file (`model:` and `thinking:`). */
export interface PiRow { model: string; thinking?: string }
export type PiRows = Map<string, PiRow>;

/** What the shell reads of `jhon profile json`. */
export interface ProfilesJson {
	profiles?: Array<{ name?: string; agents?: Array<{ agent?: string; runtime?: string; model?: string }> }>;
}

/** The profile the branch's last session-profile entry names; undefined when none does, or the last one put the session back on the default. */
export function sessionProfileOnBranch(branch: readonly unknown[] | undefined): string | undefined {
	let name: string | undefined;
	for (const e of branch ?? []) {
		const entry = e as { type?: string; customType?: string; data?: { profile?: unknown } };
		if (entry?.type !== "custom" || entry.customType !== SESSION_PROFILE_ENTRY) continue;
		name = typeof entry.data?.profile === "string" && entry.data.profile ? entry.data.profile : undefined;
	}
	return name;
}

/**
 * A profile's pi rows by agent, each value split at its first `@` as the
 * engine splits it (profiles.TierValue); undefined when the data has no
 * profile of that name.
 */
export function piRows(data: ProfilesJson | undefined, name: string): PiRows | undefined {
	const profile = (data?.profiles ?? []).find((p) => p?.name === name);
	if (!profile) return undefined;
	const rows: PiRows = new Map();
	for (const a of profile.agents ?? []) {
		if (a?.runtime !== "pi" || typeof a.agent !== "string" || typeof a.model !== "string" || !a.model) continue;
		const at = a.model.indexOf("@");
		const model = at < 0 ? a.model : a.model.slice(0, at), thinking = at < 0 ? "" : a.model.slice(at + 1);
		if (model) rows.set(a.agent, thinking ? { model, thinking } : { model });
	}
	return rows;
}

/** The profile as the header and the status bar name it: the session's, marked, while it differs from the default; else the default. */
export function sessionMark(session: string | undefined, active: string): string {
	return session && session !== active ? `${session} (session)` : active;
}

/** Why `u` is refused on a profile whose models this session's pi cannot run, and what to do (L980 A7); it follows «Could not use <name>: ». */
export function unavailableRefusal(models: readonly string[]): string {
	return `${models.join(", ")} cannot run in this pi (provider not loaded, or no credential). To use it: make it the default with d, then restart pi.`;
}

/** What the owner hears when `u` switches the orchestrator's model. `later`: pi has no thinking-level setter, so the level waits. */
export function orchestratorSwitchNotice(row: PiRow, later: boolean): string {
	if (!row.thinking) return `The orchestrator switches to ${row.model} from the next turn.`;
	return later
		? `The orchestrator switches to ${row.model} from the next turn; its thinking level ${row.thinking} applies at the next start.`
		: `The orchestrator switches to ${row.model} · ${row.thinking} from the next turn.`;
}

/** What the owner hears when the session's profile is gone (L980 A9). */
export function lostProfileNotice(was: string, active: string): string {
	return `jhon: the profile ${was} this session used no longer exists (renamed or deleted); the session is back on the default profile ${active}.`;
}

/** The session's profile read again (SessionProfile.refresh). */
export type Refreshed =
	| { state: "none" }
	| { state: "ok" }
	/** The profile is gone: the session follows the default again. */
	| { state: "missing"; was: string }
	/** `jhon profile json` could not be read: the rows read before stand. */
	| { state: "unread"; why: string };

/** One session's profile and its rows as last read. */
export class SessionProfile {
	private name: string | undefined;
	private rows: PiRows | undefined;
	private readonly load: (cwd: string) => Promise<ProfilesJson>;

	/** load: `jhon profile json`, parsed. */
	constructor(load: (cwd: string) => Promise<ProfilesJson>) {
		this.load = load;
	}

	/** The profile this session uses; undefined while it follows the default. */
	current(): string | undefined {
		return this.name;
	}

	/** Whether the profile's rows were read. */
	known(): boolean {
		return this.rows !== undefined;
	}

	/** A session start: the profile its branch names (a child: its parent's); its rows are read at the first need. */
	start(name: string | undefined): void {
		this.name = name || undefined;
		this.rows = undefined;
	}

	/** `u`: the session uses name from now on, with the rows just read. */
	use(name: string, rows: PiRows): void {
		this.name = name;
		this.rows = rows;
	}

	/** Reads the session's profile again; a profile that is gone puts the session back on the default. */
	async refresh(cwd: string): Promise<Refreshed> {
		const name = this.name;
		if (!name) return { state: "none" };
		let data: ProfilesJson;
		try { data = await this.load(cwd); } catch (error) { return { state: "unread", why: (error as Error)?.message ?? String(error) }; }
		const rows = piRows(data, name);
		// The session may have changed its profile while the read ran: its answer is the newer one's.
		if (this.name !== name) return { state: "ok" };
		if (!rows) {
			this.start(undefined);
			return { state: "missing", was: name };
		}
		this.rows = rows;
		return { state: "ok" };
	}

	/** The agent's row in the session's profile, as last read. */
	row(agent: string): PiRow | undefined {
		return this.name ? this.rows?.get(agent) : undefined;
	}

	/** The agent as the session's profile runs it: its model and thinking level from the profile's row; itself when the session follows the default or the profile names no pi model for it. */
	agent(def: AgentDef): AgentDef {
		const row = this.row(def.name);
		if (!row) return def;
		const { thinking: _rendered, ...rest } = def;
		return { ...rest, model: row.model, ...(row.thinking ? { thinking: row.thinking } : {}) };
	}
}
