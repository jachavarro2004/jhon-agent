// Foreground waits (T119, L716).
//
// A foreground `bash` call that only waits blocks the whole turn: in one real
// session 25 calls starting with `sleep N` held it for 82.5 minutes. The wait
// belongs in a background job, with its condition inside the command, so the
// job's end arrives as a message. This module recognises such a command; the
// main session's tool_call guard (lib/background-tasks-tools.ts) refuses it.
// bash_background and monitor commands are never judged here.

import { BASH_BACKGROUND_TOOL, MONITOR_TOOL } from "./ui/background-jobs.ts";

/** A leading sleep up to this many seconds still runs in the foreground. */
export const FOREGROUND_SLEEP_SECONDS = 5;
/** A `timeout N` up to this many seconds still runs in the foreground. */
export const FOREGROUND_TIMEOUT_SECONDS = 60;

const UNIT_SECONDS: Record<string, number> = { "": 1, s: 1, m: 60, h: 3600, d: 86400 };
const seconds = (amount: string, unit: string) => Number(amount) * (UNIT_SECONDS[unit] ?? 1);

// `sleep N` as the whole command or its first segment, ended by `;`, `&&` or a newline.
const LEADING_SLEEP = /^\s*sleep\s+(\d+(?:\.\d+)?)([smhd]?)\s*(?:$|;|&&|\n)/;
// `timeout [options] DURATION ...`: options with a value (-k 5, -s KILL) or without (--foreground).
const TIMEOUT = /^timeout\s+(?:(?:-[ks]\s+\S+|--(?:kill-after|signal)=\S+|-\S+)\s+)*(\d+(?:\.\d+)?)([smhd]?)(?:\s|$)/;
const LONG_WAITS: Array<[RegExp, string]> = [
	[/^gh\s+run\s+watch(?:\s|$)/, "`gh run watch`"],
	[/^wait(?:\s|$)/, "`wait`"],
	[/^tail\s+(?:\S+\s+)*(?:-[a-zA-Z]*[fF][a-zA-Z]*|--follow(?:=\S+)?)(?:\s|$)/, "`tail -f`"],
];

/** What the command waits on, or undefined when it does real work. */
export function foregroundWait(command: string): string | undefined {
	const sleep = LEADING_SLEEP.exec(command);
	if (sleep && seconds(sleep[1]!, sleep[2]!) > FOREGROUND_SLEEP_SECONDS) return `\`sleep ${sleep[1]}${sleep[2]}\``;
	for (const raw of command.split(/&&|\|\||[;|&\n]/)) {
		const segment = raw.trim();
		for (const [pattern, name] of LONG_WAITS) if (pattern.test(segment)) return name;
		const timeout = TIMEOUT.exec(segment);
		if (timeout && seconds(timeout[1]!, timeout[2]!) > FOREGROUND_TIMEOUT_SECONDS) return `\`timeout ${timeout[1]}${timeout[2]}\``;
	}
	return undefined;
}

/** The refusal for a foreground bash call that only waits, or undefined. */
export function foregroundWaitRefusal(command: string): string | undefined {
	const wait = foregroundWait(command);
	if (!wait) return undefined;
	return `A foreground wait blocks the turn: ${wait} here would hold it until it ends. Run it with ${BASH_BACKGROUND_TOOL}, with the wait condition inside the command (for example \`until <check>; do sleep 5; done; <cmd>\`), or watch its output with ${MONITOR_TOOL}; its end arrives as a message, so continue other work or end the turn.`;
}
