/**
 * shell/lib/ui/command-failure.ts — what a failed command's output says, for
 * the red card: its exit code, the tests it counted, and the first failure
 * with the file and line it names, so the card says why before it is opened.
 *
 * Read from the text alone (pi ends a failed command's output with "Command
 * exited with code N"); the runners read are pytest, go test, node --test,
 * Vitest and Jest. Anything else still gets its exit code.
 */

export interface CommandFailure {
	exit?: number;
	failed?: number;
	passed?: number;
	/** The first failure, one line. */
	first?: string;
	/** The first `file:line` the output names. */
	where?: string;
}

const EXIT = /Command exited with code (\d+)/;
const COUNTS: ReadonlyArray<[RegExp, "failed" | "passed"]> = [
	[/(\d+) failed/, "failed"], [/(\d+) passed/, "passed"],
	[/ℹ fail (\d+)/, "failed"], [/ℹ pass (\d+)/, "passed"],
	[/Tests:\s+(\d+) failed/, "failed"], [/Tests:.*?(\d+) passed/, "passed"],
	[/--- FAIL:/g, "failed"],
];
const FIRST = [
	/^FAILED (\S+)(?: - (.*))?$/m,
	/^--- FAIL: (\S+)/m,
	/^[ \t]*(?:✗|✖|×|FAIL)[ \t]+(.+)$/m,
	/^E[ \t]+(.+)$/m,
	/^(?:AssertionError|Error|TypeError|ReferenceError)\b.*$/m,
];
const WHERE = /((?:[\w.@-]+\/)*[\w.@-]+\.(?:py|go|ts|tsx|js|jsx|mjs|rs|rb|java|kt|cs)):(\d+)/;

/** What the failed output text says; empty when it says nothing a card can use. */
export function commandFailure(text: string): CommandFailure {
	const out: CommandFailure = {};
	const exit = text.match(EXIT);
	if (exit) out.exit = Number(exit[1]);
	for (const [re, key] of COUNTS) {
		if (out[key] !== undefined) continue;
		if (re.global) {
			const n = text.match(re)?.length ?? 0;
			if (n) out[key] = n;
			continue;
		}
		const m = text.match(re);
		if (m) out[key] = Number(m[1]);
	}
	for (const re of FIRST) {
		const m = text.match(re);
		if (!m) continue;
		out.first = (m[2] ? `${m[1]} — ${m[2]}` : m[1] ?? m[0]).trim().slice(0, 200);
		break;
	}
	const where = text.match(WHERE);
	if (where) out.where = `${where[1]}:${where[2]}`;
	return out;
}

/** The card's summary line: «exited 1 · 1 failed · 17 passed»; empty when there is nothing to say. */
export function failureHeadline(f: CommandFailure): string {
	const parts: string[] = [];
	if (f.exit !== undefined) parts.push(`exited ${f.exit}`);
	if (f.failed) parts.push(`${f.failed} failed`);
	if (f.passed) parts.push(`${f.passed} passed`);
	return parts.join(" · ");
}

/** A running command's line: how long it has run and how to stop it. */
export function runningLine(elapsedMs: number, doubleEsc: boolean): string {
	const s = Math.max(0, Math.floor(elapsedMs / 1000));
	const took = s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, "0")}s`;
	return `running · ${took} · ${doubleEsc ? "esc esc" : "esc"} to cancel`;
}
