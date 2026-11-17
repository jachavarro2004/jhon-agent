/**
 * shell/lib/delegation-mode.ts — T117c (L720): the delegation mode of the
 * session's profile (its own, T120, else the default) in the main session.
 *
 * - `inline` (the default) keeps the orchestrator's organic rules.
 * - `writer` puts the rendered writer rules (jhon-orchestrator-writer.md,
 *   beside the orchestrator's file) in place of the orchestrator's region
 *   between DELEGATION_BEGIN and DELEGATION_END. A missing file, a missing
 *   marker, or markers out of order keep the organic rules whole, never a
 *   half-spliced prompt, and say why once.
 *
 * The mode is read once per session, with the rest of the prompt's frozen
 * parts (lib/session-prompt.ts), so the prompt stays byte-stable; `u` in the
 * profiles panel reads it again once when it changes the mode.
 */

import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";

export type Delegation = "inline" | "writer";

export const DELEGATION_BEGIN = "<!-- delegation:begin -->";
export const DELEGATION_END = "<!-- delegation:end -->";

/** The writer rules the engine renders beside the orchestrator's file (render/pi.go piWriterFile). */
export function writerFile(orchestrator: string): string {
	return join(dirname(orchestrator), "jhon-orchestrator-writer.md");
}

/** The orchestrator's prompt for mode; `note` says why writer mode kept the organic rules. */
export function delegationPrompt(organic: string, mode: Delegation, writerPath: string): { text: string; note?: string } {
	if (mode !== "writer") return { text: organic };
	const keep = (why: string) => ({ text: organic, note: `Writer mode: ${why}, so the orchestrator keeps its organic delegation rules this session; run \`jhon sync\`.` });
	if (!existsSync(writerPath)) return keep(`${writerPath} is missing`);
	let writer: string;
	try { writer = readFileSync(writerPath, "utf8").replace(/\s+$/, ""); } catch (error) { return keep(`${writerPath} cannot be read (${(error as Error).message})`); }
	if (!writer) return keep(`${writerPath} is empty`);
	const begin = organic.indexOf(DELEGATION_BEGIN);
	const end = organic.indexOf(DELEGATION_END);
	if (begin < 0 || end < 0) return keep(`the orchestrator's prompt lacks the ${begin < 0 ? DELEGATION_BEGIN : DELEGATION_END} marker`);
	if (end < begin) return keep("the orchestrator's delegation markers are out of order");
	return { text: organic.slice(0, begin) + writer + organic.slice(end + DELEGATION_END.length) };
}

/** What the owner hears when `u` switches the session's mode. */
export function modeSwitchNotice(mode: Delegation): string {
	return `The orchestrator's rules switch to ${mode} mode from the next turn.`;
}

/** What the profiles panel's mode actions reach. */
export interface ProfileModeDeps {
	/** `jhon profile use <name>`: the default for new sessions. */
	use(name: string): Promise<unknown>;
	/** T120: makes the profile this session's (lib/session-profile.ts), writing nothing global. `refused`: why it was not, with nothing changed; `notice`: what the owner hears of it (the orchestrator's switch). */
	useInSession(name: string): Promise<{ refused?: string; notice?: string }>;
	/** `jhon profile mode <profile> <mode>`. */
	setMode(profile: string, mode: Delegation): Promise<unknown>;
	/** The profile's delegation as the profiles file says now. */
	delegationOf(profile: string): Delegation;
	/** The session's frozen mode; undefined before its first turn read it. */
	sessionMode(): Delegation | undefined;
	/** Reads the session prompt again at the next turn. */
	resetPrompt(): void;
	notify(text: string): void;
}

/**
 * The session's mode follows the profile it uses now: when that changes the
 * mode its prompt was built with, the prompt is rebuilt once at the next
 * turn, with a notice. Before the first turn read a mode, nothing is rebuilt.
 * `said`: what the caller has to say of the same change (`u`: the
 * orchestrator's switch). It goes first in the same notice, or alone when the
 * mode stays: pi draws only the last of two info notices sent back to back.
 */
export function followMode(deps: Pick<ProfileModeDeps, "sessionMode" | "delegationOf" | "resetPrompt" | "notify">, profile: string, said?: string): void {
	const now = deps.sessionMode();
	const next = deps.delegationOf(profile);
	const switched = now !== undefined && now !== next;
	if (switched) deps.resetPrompt();
	const notice = [said, switched ? modeSwitchNotice(next) : undefined].filter(Boolean).join(" ");
	if (notice) deps.notify(notice);
}

/**
 * The panel's actions: `mode` saves a profile's mode (the next session reads
 * it); `useNow` makes a profile this session's (T120: this session only) and,
 * when that changes the session's mode, rebuilds the prompt once with a
 * notice; `makeDefault` makes it the default for new sessions (`jhon profile
 * use`), with no rebuild here.
 */
export function profileModeActions(deps: ProfileModeDeps) {
	const failed = (error: unknown) => (error as Error)?.message ?? String(error);
	return {
		async mode(profile: string, mode: Delegation): Promise<void> {
			await deps.setMode(profile, mode);
		},
		async useNow(name: string): Promise<string | undefined> {
			let used: { refused?: string; notice?: string };
			try { used = await deps.useInSession(name); } catch (error) { return failed(error); }
			if (used.refused) return used.refused;
			followMode(deps, name, used.notice);
			return undefined;
		},
		async makeDefault(name: string): Promise<string | undefined> {
			try { await deps.use(name); } catch (error) { return failed(error); }
			return undefined;
		},
	};
}
