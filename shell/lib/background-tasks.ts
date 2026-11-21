/**
 * shell/lib/background-tasks.ts — what the main session runs in the
 * background, kept in one place: shell commands (`bash_background`), monitors
 * (`monitor`, lib/ui/task-monitor.ts) and sub-agents (`subagent`). The tools
 * that start, list, read and stop them live in lib/background-tasks-tools.ts.
 *
 * Every task gets an id (`job-N` or `agent-N`), names the session that
 * started it, and is running until it ends on its own, which is reported
 * once through `onSettled`, or is stopped, which reports nothing: the one who
 * stopped it says who hears of it. `stopAll` ends the rest at session end.
 *
 * A command runs through the shell operations it is handed (pi's own Bash,
 * or the sandbox's, lib/sandbox.ts); its bytes go to `<id>.log` and its last
 * lines to the record. A sub-agent is a child pi (lib/agents.ts) whose JSON
 * events become its activity, its thread, its tokens and its cost.
 */
import { createWriteStream, readFileSync } from "node:fs";
import { join } from "node:path";
import type { AgentDef, ChildEvent, ChildResult, RunOptions } from "./agents.ts";

/** The tool that runs a command in the background, beside pi's `bash`. */
export const BASH_BACKGROUND_TOOL = "bash_background";
/** The tool whose command's output lines reach the agent while it runs. */
export const MONITOR_TOOL = "monitor";

/** How many tasks may run at once in the process. */
export const MAX_RUNNING_TASKS = 25;
/**
 * How much of a job's output its record keeps: its newest 20 lines (for its
 * notice and task_result), each cut to its newest 2000 characters after an
 * ellipsis.
 */
export const [TAIL_LINES, MAX_TAIL_LINE_CHARS] = [20, 2000] as const;

/** The one call a job makes on pi's `BashOperations`: run a command, streaming its bytes, until it exits or the signal aborts it. */
export type JobExecOperations = {
	exec: (command: string, cwd: string, options: { onData: (data: Buffer) => void; signal?: AbortSignal }) => Promise<{ exitCode: number | null }>;
};

/** exited: a command ended with its exit code; completed: a sub-agent answered; failed: it could not run or finish. */
export type TaskStatus = "running" | "exited" | "completed" | "failed" | "stopped";

/** What every task has: who started it and when, and where it stands. */
type TaskBase = Readonly<{ id: string; label: string; ownerSessionId: string; startedAt: number }> & { status: TaskStatus; endedAt?: number };

export interface JobRecord extends TaskBase {
	readonly kind: "command" | "monitor";
	readonly command: string;
	readonly cwd: string;
	readonly outputPath: string;
	exitCode?: number | null;
	error?: string;
	/** Its last output lines, kept current until it ends. */
	tail: string[];
	/** A monitor's output lines reported so far, and its timeout as started. */
	events?: number; timeoutSeconds?: number;
	/** Started by the owner (JobStartRequest.byOwner). */
	readonly byOwner?: boolean;
	/** What the records of a `jhon executions run` say, read when it ends (executionsResult). */
	executions?: ExecutionsResult;
}

/** The records one `jhon executions run` wrote: failed when one cannot be read, is incomplete, or holds an execution that did not pass. */
export interface ExecutionsResult {
	records: string[];
	failed: boolean;
	/** Executions that did not pass, and all executions, over every record. */
	failures: number;
	total: number;
}

export interface AgentRun extends TaskBase {
	readonly kind: "agent";
	/** The sub-agent's name. */
	readonly agent: string;
	readonly task: string;
	/** The id of its saved session, when it is saved. */
	readonly childSessionId?: string;
	/** Its final answer, or why it has none. */
	result?: string;
	/** Its latest steps, oldest first: each tool it starts, each that fails, the first line of what it says. */
	activity: string[];
	/** The tokens its assistant messages reported (usage.totalTokens), added up. */
	tokens: number;
	/** What its assistant messages cost (usage.cost.total), added up. */
	cost: number;
	/** The model its definition names. */
	readonly model?: string;
	/** The directory of its saved session (`<…>/subagents`), when it is saved. */
	readonly sessionDir?: string;
	/** What the Agents view draws, oldest first: thinking, text, and each tool call with its output. */
	thread: ThreadItem[];
}

/** One entry of a sub-agent's thread. */
export type ThreadItem =
	| { kind: "thinking" | "text"; text: string }
	| { kind: "tool"; callId: string; name: string; /** What it works on (activityLine's pick), when its args name it. */ subject?: string; output: string; running: boolean; isError: boolean };

export type TaskRecord = JobRecord | AgentRun;

/** A command to start: what runs where, for which session, and how it is named. */
export type JobStartRequest = Pick<JobRecord, "command" | "cwd" | "ownerSessionId"> & {
	label?: string;
	kind?: JobRecord["kind"];
	/** The owner started it (the change detail's `t`): the agent hears nothing of it and its tools never see it. */
	byOwner?: boolean;
	/** Receives each complete output line, and what is left of the last one when the command ends. */
	onLine?: (line: string) => void;
};

/** A background sub-agent's run options: runAgent's, less the two the registry supplies, and the short label it is named by (T195). */
export type AgentStartOptions = Omit<RunOptions, "signal" | "onEvent"> & { label?: string };

export interface TaskRegistryDeps {
	/** Where each job's `<id>.log` goes; asked at every start. */
	outputDir: () => string;
	now(): number;
	/** The shell a job runs in, asked at every start so a changed setting applies to the next job; `byOwner` for the owner's own command, which no sandbox wraps (lib/sandbox.ts). */
	shell(cwd: string, byOwner?: boolean): { operations: JobExecOperations; commandPrefix?: string };
	/** Runs a sub-agent's child, handing each JSON event to onEvent as it comes. */
	runAgent(agent: AgentDef, task: string, cwd: string, options: RunOptions & { signal: AbortSignal; onEvent: (event: ChildEvent) => void }): Promise<ChildResult>;
	/** Called once for a task that ends on its own; never for a stop. */
	onSettled(task: TaskRecord): void;
	/** Called when a running sub-agent's record changes, so a view can repaint. */
	onActivity?(run: AgentRun): void;
}

// ---------------------------------------------------------------- output

const STANDALONE_SLEEP = /^sleep\s+\d+(?:\.\d+)?[smhd]?$/;

/**
 * Whether `command` does nothing but sleep: literal `sleep <number>[smhd]`
 * steps joined by `&&` or `;`. A sleep inside a loop, a pipe or beside any
 * other command is real work.
 */
export const isStandaloneSleep = (command: string): boolean => {
	const steps = command.split(/&&|;/).map((step) => step.trim()).filter((step) => step !== "");
	return steps.length > 0 && steps.every((step) => STANDALONE_SLEEP.test(step));
};

/** `line` without a trailing carriage return, cut to its newest characters after `…` when too long or already cut. */
function boundLine(line: string, cut: boolean): string {
	const text = line.endsWith("\r") ? line.slice(0, -1) : line;
	if (text.length > MAX_TAIL_LINE_CHARS) return `…${text.slice(-MAX_TAIL_LINE_CHARS)}`;
	return cut ? `…${text}` : text;
}

/**
 * A command's decoded output split into lines: every complete line once
 * (bounded), the newest TAIL_LINES of them, and the line still being written.
 */
/** A command's output as lines: push decoded text in, read the line still being written and the newest lines. */
export type OutputTail = { push(text: string): string[]; pending(): string | undefined; lines(): string[] };

export function createOutputTail(): OutputTail {
	const kept: string[] = [];
	let open = "";
	let openCut = false;
	const keep = (line: string) => {
		kept.push(line);
		if (kept.length > TAIL_LINES) kept.splice(0, kept.length - TAIL_LINES);
	};
	return {
		/** Adds output; returns the lines it completed. */
		push(text: string): string[] {
			const pieces = `${open}${text}`.split("\n");
			open = pieces.pop()!;
			const done = pieces.map((piece, i) => boundLine(piece, i === 0 && openCut));
			if (done.length > 0) openCut = false;
			for (const line of done) keep(line);
			// A line that never ends keeps only its newest characters.
			if (open.length > MAX_TAIL_LINE_CHARS) {
				open = open.slice(-MAX_TAIL_LINE_CHARS);
				openCut = true;
			}
			return done;
		},
		/** The line still being written, bounded; undefined when none is. */
		pending(): string | undefined {
			return open === "" ? undefined : boundLine(open, openCut);
		},
		/** The newest TAIL_LINES lines, the one still being written last. */
		lines(): string[] {
			const last = this.pending();
			return (last === undefined ? kept : [...kept, last]).slice(-TAIL_LINES);
		},
	};
}

// ---------------------------------------------------------------- jhon executions

/** Whether a command runs `jhon executions run`, whose records then decide its result (T108, L671). */
export const runsExecutions = (command: string) => /\bjhon\s+executions\s+run\b/.test(command);

/**
 * The result of the records named in `report` (JHON_EXECUTIONS_REPORT: one
 * absolute path per line), or undefined when it names none. A pipeline exits
 * with its last command's code; the records say what the run found.
 */
export function executionsResult(report: string): ExecutionsResult | undefined {
	let listed: string;
	try { listed = readFileSync(report, "utf8"); } catch { return undefined; }
	const records = [...new Set(listed.split("\n").map((line) => line.trim()).filter((line) => line !== ""))];
	if (records.length === 0) return undefined;
	let failed = false;
	let failures = 0;
	let total = 0;
	for (const record of records) {
		let body: { complete?: unknown; executions?: unknown };
		try { body = JSON.parse(readFileSync(record, "utf8")); } catch { failed = true; continue; }
		const executions: Array<{ status?: unknown } | null> = Array.isArray(body.executions) ? body.executions : [];
		const notPassed = executions.reduce((n, execution) => n + (execution?.status === "pass" ? 0 : 1), 0);
		total += executions.length;
		failures += notPassed;
		if (notPassed > 0 || body.complete !== true) failed = true;
	}
	return { records, failed, failures, total };
}

/** `value` as one single-quoted shell word. */
const shellWord = (value: string) => `'${value.split("'").join(`'\\''`)}'`;

// ---------------------------------------------------------------- sub-agent events

/** Activity lines a sub-agent keeps. */
const ACTIVITY_LINES = 30;
/** Thread entries a sub-agent keeps, and the characters kept of a tool's output (its end). */
const THREAD_ITEMS = 200;
const TOOL_OUTPUT_CHARS = 4000;

/** `text` on one line, cut at `width` characters with `…`. */
function oneLine(text: string, width = 100): string {
	const flat = text.replace(/\s+/g, " ").trim();
	return flat.length <= width ? flat : `${flat.slice(0, width - 1)}…`;
}

/** The assistant message a `message_end` event carries, or undefined. */
function assistantMessage(event: ChildEvent): { content: any[]; usage?: Record<string, any> } | undefined {
	if (event.type !== "message_end") return undefined;
	const message = event.message as { role?: unknown; content?: unknown; usage?: unknown } | undefined;
	if (message?.role !== "assistant") return undefined;
	return { content: Array.isArray(message.content) ? message.content : [], usage: message.usage && typeof message.usage === "object" ? message.usage as Record<string, any> : undefined };
}

/** The first string among a tool start's command, path, pattern and agent arguments; undefined when it is none or empty. */
function toolSubject(event: ChildEvent): string | undefined {
	const args = (event.args ?? {}) as Record<string, unknown>;
	for (const value of [args.command, args.path, args.pattern, args.agent]) if (typeof value === "string") return value === "" ? undefined : value;
	return undefined;
}

/** The text blocks of a tool result's content, joined by newlines. */
function toolOutput(result: unknown): string {
	const content = (result as { content?: unknown } | undefined)?.content;
	if (typeof content === "string") return content;
	if (!Array.isArray(content)) return "";
	return content.filter((block) => block?.type === "text" && typeof block.text === "string").map((block) => block.text).join("\n");
}

/** Adds one child event to `thread`: a tool's start or end, an assistant message's thinking and text. */
export function applyThreadEvent(thread: ThreadItem[], event: ChildEvent): void {
	const callId = typeof event.toolCallId === "string" ? event.toolCallId : "";
	const name = typeof event.toolName === "string" ? event.toolName : "tool";
	if (event.type === "tool_execution_start") {
		const subject = toolSubject(event);
		thread.push(subject === undefined ? { kind: "tool", callId, name, output: "", running: true, isError: false } : { kind: "tool", callId, name, subject, output: "", running: true, isError: false });
	} else if (event.type === "tool_execution_end") {
		const output = toolOutput(event.result).trim();
		let open: ThreadItem | undefined;
		for (let i = thread.length - 1; i >= 0 && !open; i--) {
			const item = thread[i]!;
			if (item.kind === "tool" && item.running && item.callId === callId) open = item;
		}
		const subject = open?.kind === "tool" ? open.subject : undefined;
		const ended: ThreadItem = { kind: "tool", callId, name, ...(subject === undefined ? {} : { subject }), output: output.length > TOOL_OUTPUT_CHARS ? `…${output.slice(-TOOL_OUTPUT_CHARS)}` : output, running: false, isError: event.isError === true };
		if (open) Object.assign(open, ended);
		else thread.push(ended);
	} else {
		for (const block of assistantMessage(event)?.content ?? []) {
			if (block?.type === "thinking" && typeof block.thinking === "string" && block.thinking.trim() !== "") thread.push({ kind: "thinking", text: block.thinking.trim() });
			else if (block?.type === "text" && typeof block.text === "string" && block.text.trim() !== "") thread.push({ kind: "text", text: block.text.trim() });
		}
	}
	if (thread.length > THREAD_ITEMS) thread.splice(0, thread.length - THREAD_ITEMS);
}

/** The activity line one child event adds: a tool's start, a tool's failure, the first line an assistant message says. */
export function activityLine(event: ChildEvent): string | undefined {
	const tool = typeof event.toolName === "string" ? event.toolName : "tool";
	if (event.type === "tool_execution_start") {
		const subject = toolSubject(event);
		return oneLine(subject === undefined ? tool : `${tool} ${subject}`);
	}
	if (event.type === "tool_execution_end") return event.isError === true ? `✗ ${tool} failed` : undefined;
	const said = (assistantMessage(event)?.content ?? []).filter((block) => block?.type === "text" && typeof block.text === "string").map((block) => block.text).join("").trim();
	return said === "" ? undefined : `» ${oneLine(said.split("\n")[0]!)}`;
}

// ---------------------------------------------------------------- sub-agent labels

/**
 * The characters a sub-agent's label keeps of what it does: the rail's 42
 * text columns (lib/ui/pane.ts) less its state glyph. The rail cuts it
 * further beside a long agent name and its time.
 */
export const AGENT_LABEL_CHARS = 40;

/** Brief sections and labelled lines that bound the work or route it, never say what it is (T195, L1607). */
const BOILERPLATE = new Set([
	"why", "why delegated", "allowed edit surfaces", "edit surfaces", "spec", "specs", "spec lines",
	"skills", "skills to load before work", "verification", "validation", "checks", "known environmental failures",
	"decisions on record", "code map", "return", "risk", "owner", "owner verbatim", "worktree", "branch",
]);
const sectionKey = (text: string) => text.toLowerCase().replace(/[^a-z]+/g, " ").trim();
const HEADING = /^#{1,6}\s+(.*)$/;
const LABELLED = /^([A-Za-z][A-Za-z ,'’/-]{0,40}):(?:\s|$)/;

/**
 * What a brief says its sub-agent does, in a few words: the first phrase of
 * its first meaningful line, cut to AGENT_LABEL_CHARS. Markdown headings,
 * the sections under a boilerplate heading (`## Why delegated`, `## Allowed
 * edit surfaces`, …) and boilerplate lines (`Spec: …`) are skipped; a list
 * marker is dropped; the phrase ends at the first `.`, `;`, `!` or `?`
 * before a space. Empty when nothing is left.
 */
export function briefLabel(brief: string): string {
	let skipping = false;
	for (const raw of brief.split("\n")) {
		const line = raw.replace(/\*\*|__/g, "").trim();
		const heading = HEADING.exec(line);
		if (heading) {
			skipping = BOILERPLATE.has(sectionKey(heading[1]!));
			continue;
		}
		if (skipping || line === "") continue;
		const labelled = LABELLED.exec(line);
		if (labelled && BOILERPLATE.has(sectionKey(labelled[1]!))) continue;
		const text = line.replace(/^(?:[-*+>]|\d+[.)])\s+/, "");
		const phrase = (text.split(/[.;!?](?:\s|$)/)[0] ?? "").trim();
		if (phrase !== "") return oneLine(phrase, AGENT_LABEL_CHARS);
	}
	return "";
}

/** A sub-agent's label: `<agent>: <what it does>`, the label given or else its brief's (briefLabel); the agent's name alone when neither says anything. */
export function agentLabel(agent: string, brief: string, label?: string): string {
	const given = label === undefined ? "" : oneLine(label, AGENT_LABEL_CHARS);
	const what = given || briefLabel(brief);
	return what === "" ? agent : `${agent}: ${what}`;
}

/** What a sub-agent does, as its label says it beside its name; a record whose label does not start with its name says it through its brief. */
export function agentDoes(run: Pick<AgentRun, "agent" | "label" | "task">): string {
	const prefix = `${run.agent}: `;
	if (run.label.startsWith(prefix)) return run.label.slice(prefix.length);
	return run.label === run.agent ? "" : briefLabel(run.task);
}

const positive = (value: unknown) => (typeof value === "number" && Number.isFinite(value) && value > 0 ? value : 0);

/** The tokens an assistant message's usage reports: its totalTokens, else its parts added; 0 for any other event. */
export function messageTokens(event: ChildEvent): number {
	const usage = assistantMessage(event)?.usage;
	if (!usage) return 0;
	return positive(usage.totalTokens) || positive(usage.input) + positive(usage.output) + positive(usage.cacheRead) + positive(usage.cacheWrite);
}

/** What an assistant message's usage says it cost (usage.cost.total); 0 for any other event. */
export function messageCost(event: ChildEvent): number {
	return positive(assistantMessage(event)?.usage?.cost?.total);
}

// ---------------------------------------------------------------- the registry

export function createTaskRegistry(deps: TaskRegistryDeps) {
	const tasks = new Map<string, TaskRecord>();
	const cancels = new Map<string, AbortController>();
	// Each job until its log is closed and its end reported.
	const closing = new Set<Promise<void>>();
	let jobs = 0;
	let agents = 0;

	const runningCount = () => {
		let n = 0;
		for (const task of tasks.values()) if (task.status === "running") n++;
		return n;
	};
	const admit = () => {
		if (runningCount() >= MAX_RUNNING_TASKS) throw new Error(`${MAX_RUNNING_TASKS} background tasks are already running; stop one with task_stop first.`);
	};
	const register = (task: TaskRecord) => {
		const cancel = new AbortController();
		tasks.set(task.id, task);
		cancels.set(task.id, cancel);
		return cancel.signal;
	};

	/** Runs a command in the background. */
	const start = (request: Readonly<JobStartRequest>): JobRecord => {
		admit();
		const { command, cwd, ownerSessionId, byOwner } = request;
		const { operations, commandPrefix } = deps.shell(cwd, byOwner === true);
		const id = `job-${++jobs}`;
		// The name shown is the label when it says anything, else the command itself.
		const label = request.label?.trim() || command;
		const logFile = join(deps.outputDir(), id + ".log");
		const job: JobRecord = { id, kind: request.kind ?? "command", label, command, cwd, ownerSessionId, outputPath: logFile, startedAt: deps.now(), status: "running", tail: [] };
		if (byOwner) Object.assign(job, { byOwner: true });
		const log = createWriteStream(job.outputPath);
		// The log is a convenience: losing it must not take the session down; the tail stays.
		log.on("error", () => {});
		const output = createOutputTail();
		// One decoder for the whole stream, so a character split across chunks arrives whole.
		const decoder = new TextDecoder("utf-8");
		const signal = register(job);
		const read = (text: string) => {
			const lines = output.push(text);
			job.tail = output.lines();
			if (request.onLine) for (const line of lines) request.onLine(line);
		};
		const drain = () => {
			const rest = decoder.decode();
			if (rest !== "") read(rest);
			const last = output.pending();
			if (last !== undefined) request.onLine?.(last);
		};
		// A `jhon executions run` lists the records it writes in this file, read when it ends.
		const report = runsExecutions(request.command) ? `${job.outputPath}.executions` : undefined;
		let script = report ? `export JHON_EXECUTIONS_REPORT=${shellWord(report)}\n${request.command}` : request.command;
		if (commandPrefix) script = `${commandPrefix}\n${script}`;
		const end = async (change: () => void) => {
			drain();
			if (job.status === "running") {
				change();
				job.endedAt = deps.now();
			}
			await new Promise<void>((resolve) => log.end(resolve));
			cancels.delete(id);
			if (job.status === "exited" || job.status === "failed") deps.onSettled(job);
		};
		const run = operations.exec(script, request.cwd, {
			signal,
			onData: (data) => {
				log.write(data);
				read(decoder.decode(data, { stream: true }));
			},
		}).then(
			({ exitCode }) => end(() => {
				job.status = "exited";
				job.exitCode = exitCode;
				const executions = report ? executionsResult(report) : undefined;
				if (executions) job.executions = executions;
			}),
			(error: unknown) => end(() => {
				job.status = "failed";
				job.error = error instanceof Error ? error.message : String(error);
			}),
		).finally(() => closing.delete(run));
		closing.add(run);
		return job;
	};

	/** Runs a sub-agent in the background: saved in `session`, keeping a continued one's `scope`, stopped past `timeoutSeconds`, named by `label` (agentLabel). */
	const startAgent = (agent: AgentDef, task: string, cwd: string, ownerSessionId: string, { label, ...options }: AgentStartOptions = {}): AgentRun => {
		admit();
		const session = options.session;
		const run: AgentRun = {
			id: `agent-${++agents}`,
			kind: "agent",
			// T195: named by what it does, never by its brief's first line ("## Why delegated").
			label: agentLabel(agent.name, task, label),
			agent: agent.name,
			task,
			ownerSessionId,
			childSessionId: session?.id,
			startedAt: deps.now(),
			status: "running",
			activity: [],
			tokens: 0,
			cost: 0,
			thread: [],
			model: agent.model,
			...(session ? { sessionDir: session.dir } : {}),
		};
		const signal = register(run);
		const onEvent = (event: ChildEvent) => {
			// A stopped run's last events change nothing.
			if (run.status !== "running") return;
			const tokens = messageTokens(event);
			run.tokens += tokens;
			run.cost += messageCost(event);
			const items = run.thread.length;
			applyThreadEvent(run.thread, event);
			const line = activityLine(event);
			if (line !== undefined) {
				run.activity.push(line);
				if (run.activity.length > ACTIVITY_LINES) run.activity.splice(0, run.activity.length - ACTIVITY_LINES);
			}
			// A tool's end rewrites its thread entry in place.
			const changed = line !== undefined || tokens > 0 || run.thread.length !== items || event.type === "tool_execution_end";
			if (changed) deps.onActivity?.(run);
		};
		const settle = (status: "completed" | "failed", result: string) => {
			if (run.status !== "running") return;
			run.status = status;
			run.result = result;
			run.endedAt = deps.now();
			cancels.delete(run.id);
			deps.onSettled(run);
		};
		deps.runAgent(agent, task, cwd, { ...options, signal, onEvent }).then(
			(answer) => settle(answer.ok ? "completed" : "failed", answer.text),
			(error: unknown) => settle("failed", `jhon: the sub-agent "${agent.name}" could not run: ${error instanceof Error ? error.message : String(error)}`),
		);
		return run;
	};

	/** Stops a running task; reports nothing. Returns the task, or undefined for an unknown id. */
	const stop = (id: string): TaskRecord | undefined => {
		const task = tasks.get(id);
		if (task?.status !== "running") return task;
		task.status = "stopped";
		task.endedAt = deps.now();
		if (task.kind === "agent") task.result = `jhon: the sub-agent "${task.agent}" was stopped`;
		cancels.get(id)?.abort();
		// A job lets go of its controller once its log is closed.
		if (task.kind === "agent") cancels.delete(id);
		return task;
	};

	return {
		start,
		startAgent,
		stop,
		get: (id: string) => tasks.get(id),
		/** Every task in start order, or only those `ownerSessionId` started. */
		list: (ownerSessionId?: string) => [...tasks.values()].filter((task) => ownerSessionId === undefined || task.ownerSessionId === ownerSessionId),
		running: runningCount,
		/** Jobs still running or closing their log. */
		pending: () => closing.size,
		/** Stops every running task; resolves once every job's log is closed. */
		async stopAll() {
			for (const id of Array.from(tasks.keys())) stop(id);
			await Promise.allSettled(Array.from(closing));
		},
	};
}

export type TaskRegistry = ReturnType<typeof createTaskRegistry>;
