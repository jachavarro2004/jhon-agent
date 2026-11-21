/**
 * shell/lib/ui/double-esc-cancel-policy.ts — what esc does in the prompt (T174-S8).
 *
 * shell.behaviour.double_esc_cancel (lib/settings.ts; off by default, the
 * owner's settings or JHON_SHELL_DOUBLE_ESC_CANCEL as `jhon settings`
 * resolves it; no project layer, so the same key never stops a turn in one
 * repository and not in another) decides whether a running turn stops on the
 * first esc or on a second one within DOUBLE_ESC_CANCEL_WINDOW_MS. Idle, a
 * draft clears on a second esc within IDLE_ESC_CLEAR_WINDOW_MS, and only
 * while it still reads as it did at the first press.
 *
 * The gate decides; the prompt (extensions/ui.ts) acts and shows the hint.
 */
import { shellSettings, type ShellSettings } from "../settings.ts";

/** Whether a running turn waits for a second esc before it stops. */
export type DoubleEscCancelPolicy = ReturnType<typeof loadDoubleEscCancelPolicy>;

export const DOUBLE_ESC_CANCEL_KEY = "shell.behaviour.double_esc_cancel";
/** Clearing a draft keeps it in the history: pi's own idle esc-esc window. */
export const IDLE_ESC_CLEAR_WINDOW_MS = 500;
/** Stopping a turn throws its work away, so the second press gets a full second: twice pi's own idle esc-esc window. */
export const DOUBLE_ESC_CANCEL_WINDOW_MS = 2 * IDLE_ESC_CLEAR_WINDOW_MS;

interface PolicyOptions { settings?: ShellSettings }

/** The policy the settings hold now; off when they cannot be read. */
export function loadDoubleEscCancelPolicy(options: PolicyOptions = {}): "on" | "off" {
	const settings = options.settings ?? shellSettings();
	return settings.doubleEscCancel() ? "on" : "off";
}

/** Saves the policy with `jhon settings set` (Customize -> Behaviour); returns the settings file, throws when jhon refuses. */
export function writeDoubleEscCancelPolicy(policy: DoubleEscCancelPolicy, options: PolicyOptions = {}): string {
	const settings = options.settings ?? shellSettings();
	const saved = settings.set(DOUBLE_ESC_CANCEL_KEY, policy === "on");
	if (!saved.ok) throw new Error(saved.error);
	return settings.path;
}

// --- the gate -----------------------------------------------------------------------

/**
 * What one esc press does:
 * - `pass`: pi handles it (autocomplete, bash mode, an empty idle prompt's tree/fork).
 * - `stop`: the turn stops now.
 * - `arm`: the first press of a pair; the hint shows until the window closes.
 * - `clear`: the draft goes (into the history).
 */
export type EscOutcome = "pass" | "stop" | "arm" | "clear";

export interface EscPress {
	working: boolean;
	/** pi shows its completion list: esc closes it. */
	completing: boolean;
	doubleEsc: boolean;
	/** The draft as it reads now. */
	text: string;
}

export interface EscGate {
	press(press: EscPress): EscOutcome;
	/** Any other key: an armed clear disarms (its draft is about to change). */
	other(): void;
	/** A turn started or ended: every armed press disarms. */
	reset(): void;
	/** The hint for the line's right end while a window is open, with the draft as it reads now. */
	hint(working: boolean, text: string): "stop" | "clear" | undefined;
	/** The open window's length in ms after an `arm`, for the repaint that takes the hint away. */
	window(): number;
}

export function createEscGate(now: () => number): EscGate {
	let stopUntil: number | undefined;
	let clearUntil: number | undefined;
	let clearText: string | undefined;
	let lastWindow = 0;
	const stopOpen = () => stopUntil !== undefined && now() < stopUntil;
	const clearOpen = (text: string) => clearUntil !== undefined && now() < clearUntil && clearText === text;
	const disarm = () => { stopUntil = undefined; clearUntil = undefined; clearText = undefined; };
	return {
		press({ working, completing, doubleEsc, text }) {
			if (completing) return "pass";
			if (working) {
				clearUntil = undefined; clearText = undefined;
				if (!doubleEsc || stopOpen()) { stopUntil = undefined; return "stop"; }
				stopUntil = now() + DOUBLE_ESC_CANCEL_WINDOW_MS;
				lastWindow = DOUBLE_ESC_CANCEL_WINDOW_MS;
				return "arm";
			}
			stopUntil = undefined;
			// pi owns esc on an empty prompt (tree/fork) and in bash mode (`!`).
			if (text.trim() === "" || text.trimStart().startsWith("!")) return "pass";
			if (clearOpen(text)) { disarm(); return "clear"; }
			clearUntil = now() + IDLE_ESC_CLEAR_WINDOW_MS;
			clearText = text;
			lastWindow = IDLE_ESC_CLEAR_WINDOW_MS;
			return "arm";
		},
		other() { clearUntil = undefined; clearText = undefined; },
		reset: disarm,
		hint(working, text) {
			if (working) return stopOpen() ? "stop" : undefined;
			return clearOpen(text) ? "clear" : undefined;
		},
		window: () => lastWindow,
	};
}

/**
 * pi's esc-abort puts its queued messages back in the editor ahead of the
 * draft, joined by a blank line (`[queued, draft].filter(nonblank).join("\n\n")`).
 * This takes the queued part back out: "" when nothing was queued, undefined
 * when the editor holds something else (then pi's text stays as it is).
 */
export function extractQueuedText(restored: string, draft: string): string | undefined {
	if (restored === draft) return "";
	// pi joins only what is not blank: with a blank draft, all it restored was queued.
	if (!draft.trim()) return restored;
	const joint = "\n\n" + draft;
	return restored.endsWith(joint) ? restored.slice(0, restored.length - joint.length) : undefined;
}
