// Background jobs.
//
// A background job runs a shell command while the parent keeps working or
// stays idle, so waiting on CI, a build, or a server never costs a sleep loop
// or a subagent. A wait condition lives inside the command itself (for
// example `until grep -q ready log; do sleep 1; done` or
// `gh run watch <id> --exit-status`): the job's exit is the notification.
//
// This module holds what a job's command needs: the tool names, the output
// tail, and the shell operations it runs through. lib/background-tasks.ts
// runs the jobs, beside the background sub-agents.

/** The background sibling of Pi's native `bash` tool. */
export const BASH_BACKGROUND_TOOL = "bash_background";
/** A background job whose output lines are events. */
export const MONITOR_TOOL = "monitor";

const STANDALONE_SLEEP_SEGMENT = /^sleep\s+\d+(?:\.\d+)?[smhd]?$/;

/**
 * True only for a command that does nothing but sleep: one or more literal
 * `sleep <number>[smhd]` segments joined by `&&` or `;`. Wait conditions
 * (`until ...; do sleep 1; done`), loops, pipes and sleeps mixed with real
 * commands are never standalone (#1903).
 */
export function isStandaloneSleep(command: string): boolean {
	const segments = command.split(/&&|;/).map((segment) => segment.trim()).filter((segment) => segment.length > 0);
	return segments.length > 0 && segments.every((segment) => STANDALONE_SLEEP_SEGMENT.test(segment));
}

/** Lines kept for the exit notice and job listings. */
export const TAIL_LINES = 20;
/**
 * Characters kept per tail line. Progress bars and minified output can print
 * one endless line; only its newest characters are kept, after an ellipsis.
 */
export const MAX_TAIL_LINE_CHARS = 2000;

/** Collects the last TAIL_LINES lines of a stream of output chunks. */
export function createOutputTail() {
	const complete: string[] = [];
	let partial = "";
	let partialTruncated = false;
	const bound = (line: string, truncated: boolean) => {
		const cut = line.length > MAX_TAIL_LINE_CHARS;
		const kept = cut ? line.slice(-MAX_TAIL_LINE_CHARS) : line;
		return cut || truncated ? `…${kept}` : kept;
	};
	const clean = (line: string) => (line.endsWith("\r") ? line.slice(0, -1) : line);
	return {
		push(text: string) {
			const parts = (partial + text).split("\n");
			partial = parts.pop() ?? "";
			for (const [index, part] of parts.entries()) {
				complete.push(bound(clean(part), index === 0 && partialTruncated));
				if (complete.length > TAIL_LINES) complete.shift();
				if (index === 0) partialTruncated = false;
			}
			if (partial.length > MAX_TAIL_LINE_CHARS) {
				partial = partial.slice(-MAX_TAIL_LINE_CHARS);
				partialTruncated = true;
			}
		},
		lines(): string[] {
			const all = partial.length > 0 ? [...complete, bound(clean(partial), partialTruncated)] : [...complete];
			return all.slice(-TAIL_LINES);
		},
	};
}

/** The subset of Pi's `BashOperations` a job needs. */
export interface JobExecOperations {
	exec(command: string, cwd: string, options: { onData: (data: Buffer) => void; signal?: AbortSignal }): Promise<{ exitCode: number | null }>;
}
