// Stall recovery (T47, L164): a Claude API request with no first byte can
// hold a turn for minutes while Claude Code retries, and what always worked
// was the owner's Esc and "Sigue.". Past the threshold with no stream event
// and no tool running (lib/ui/api-wait.ts), the shell does the same, once per
// owner message. It uses a single one-shot timer, armed only while a turn
// waits on the API and cleared on every event and at the turn's end: never an
// animation tick, so potato stays still.
//
// A model that thinks can go minutes with no stream event (T38, L378-L382),
// and on the claude-bridge provider pi sees no event while Claude Code's model
// thinks; aborting there threw the thinking away and resent the history. Yet
// real first-byte stalls happen on the bridge too (T47), so those turns wait
// the longer shell.behaviour.thinking_stall_seconds instead of stall_seconds.

import { shellSettings, type ShellSettings } from "../settings.ts";
import type { ApiWait } from "./api-wait.ts";

/** The threshold in seconds, shell.behaviour.stall_seconds (JHON_SHELL_STALL_SECONDS overrides it): 0 turns recovery off; a child never recovers. */
export function stallSeconds(env: NodeJS.ProcessEnv, settings: ShellSettings = shellSettings(env)): number {
	if (env.JHON_PI_CHILD === "1") return 0;
	return settings.stallSeconds();
}

/** The provider that runs a turn through Claude Code (pi-claude-bridge). */
export const CLAUDE_BRIDGE_PROVIDER = "claude-bridge";

/** The seconds of silence that make a stall for this turn: `thinking` on claude-bridge or for a reasoning model thinking above minimal, else `stall`; 0 never recovers, and `stall` 0 turns both off. */
export function stallThreshold(model: { provider?: string; reasoning?: boolean } | undefined, thinkingLevel: string | undefined, seconds: { stall: number; thinking: number }): number {
	if (seconds.stall <= 0) return 0;
	const thinks = model?.reasoning === true && thinkingLevel !== undefined && thinkingLevel !== "off" && thinkingLevel !== "minimal";
	return model?.provider === CLAUDE_BRIDGE_PROVIDER || thinks ? seconds.thinking : seconds.stall;
}

/** The follow-up the agent receives after a recovered stall. */
export function stallContinueText(seconds: number): string {
	return `The Claude API sent no answer for ${seconds}s; the turn was restarted. Continue where you stopped.`;
}

/** What the owner sees when the shell recovers a stall. */
export function stallNotice(seconds: number): string {
	return `Claude API did not answer in ${seconds}s; retried`;
}

export type Schedule = (fn: () => void, ms: number) => () => void;

export interface StallRecovery {
	/** The wait changed (a turn, an event, a tool, a prompt, the end): re-arm or clear the timer. */
	sync(): void;
	/** A new owner message: recovery may run again. */
	ownerMessage(): void;
}

export function createStallRecovery(options: {
	wait: ApiWait;
	/** The threshold for the current turn (stallThreshold), read on every sync. */
	seconds(): number;
	schedule: Schedule;
	/** An owner prompt is open: never recover under it. */
	held(): boolean;
	/** Abort the turn and queue the continue that names the seconds waited. */
	recover(seconds: number): void;
}): StallRecovery {
	const thresholdMs = () => options.seconds() * 1000;
	let cancel: (() => void) | undefined;
	let used = false;
	const clear = () => { cancel?.(); cancel = undefined; };
	const sync = () => {
		clear();
		const ms = thresholdMs();
		if (ms <= 0 || used || options.held()) return;
		const waited = options.wait.waited();
		if (waited === undefined) return;
		cancel = options.schedule(fire, Math.max(0, ms - waited));
	};
	const fire = () => {
		cancel = undefined;
		const waited = options.wait.waited();
		const ms = thresholdMs();
		if (ms <= 0 || used || options.held() || waited === undefined) return;
		if (waited < ms) { sync(); return; }
		used = true;
		options.recover(ms / 1000);
	};
	return {
		sync,
		ownerMessage() { used = false; sync(); },
	};
}
