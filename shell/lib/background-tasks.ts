/**
 * shell/lib/background-tasks.ts — the main session's background work, in one
 * registry: commands and monitors (`bash_background`, `monitor`) and
 * sub-agents (`subagent` with `background: true`). Every task has an id in
 * one space, belongs to the session that started it, and is listed, read,
 * and stopped the same way (lib/background-tasks-tools.ts).
 *
 * A task that ends on its own settles exactly once through `onSettled`. A
 * stop settles nothing: whoever stopped it decides who is told. stopAll, at
 * session end, stops the rest silently.
 *
 * A command runs through pi's own shell operations (`createLocalBashOperations`;
 * the sandbox's with sandbox.mode on, lib/sandbox.ts),
 * so the configured shell, process groups, and process-tree kill match native
 * Bash; its output goes to a log file and a tail. A sub-agent is a child pi
 * process (lib/agents.ts, runAgent) whose JSON events become its activity.
 */
import { createWriteStream, readFileSync, type WriteStream } from "node:fs";
import { join } from "node:path";
import type { AgentDef, ChildEvent, ChildResult, ChildSession, RunOptions } from "./agents.ts";
import { createOutputTail, MAX_TAIL_LINE_CHARS, type JobExecOperations } from "./ui/background-jobs.ts";

/** Running tasks allowed at once across the process. */
export const MAX_RUNNING_TASKS = 25;

/** exited: a command ended, with its exit code; completed: an agent answered; failed: it could not run or finish. */
export type TaskStatus = "running" | "exited" | "completed" | "failed" | "stopped";

interface TaskBase {
	readonly id: string;
	readonly label: string;
	readonly ownerSessionId: string;
	readonly startedAt: number;
	status: TaskStatus;
	endedAt?: number;
}

export interface JobRecord extends TaskBase {
	readonly kind: "command" | "monitor";
	readonly command: string;
	readonly cwd: string;
	readonly outputPath: string;
	exitCode?: number | null;
	error?: string;
	/** Last output lines; refreshed until the job ends. */
	tail: string[];
	/** Monitor only: output lines reported as events so far. */
	events?: number;
	/** Monitor only: the mandatory timeout it was started with. */
	timeoutSeconds?: number;
	/** The owner started it, not the agent (JobStartRequest.byOwner). */
	readonly byOwner?: boolean;
	/** A `jhon executions run` job: the result its records hold, read at exit (executionsResult). */
	executions?: ExecutionsResult;
}

/** What the records a job's `jhon executions run` wrote say: failed when any is incomplete, unreadable, or holds a non-pass. */
export interface ExecutionsResult {
	records: string[];
	failed: boolean;
	/** Executions that did not pass, and all of them, across the records. */
	failures: number;
	total: number;
}

/** A command that runs `jhon executions run`; its job learns which records the run wrote (T108, L671). */
export const runsExecutions = (command: string) => /\bjhon\s+executions\s+run\b/.test(command);

/**
 * The result in the records `jhon executions run` named in `report` (one
 * absolute path per line, JHON_EXECUTIONS_REPORT), or undefined when it named
 * none. A pipe through tail exits with tail's code; the record does not.
 */
export function executionsResult(report: string): ExecutionsResult | undefined {
	let text: string;
	try { text = readFileSync(report, "utf8"); } catch { return undefined; }
	const records = [...new Set(text.split("\n").map((l) => l.trim()).filter(Boolean))];
	if (records.length === 0) return undefined;
	const result: ExecutionsResult = { records, failed: false, failures: 0, total: 0 };
	for (const path of records) {
		let record: { complete?: unknown; executions?: Array<{ status?: unknown }> };
		try { record = JSON.parse(readFileSync(path, "utf8")); } catch { result.failed = true; continue; }
		const executions = Array.isArray(record.executions) ? record.executions : [];
		const failures = executions.filter((e) => e?.status !== "pass").length;
		result.total += executions.length;
		result.failures += failures;
		if (record.complete !== true || failures > 0) result.failed = true;
	}
	return result;
}

const shellQuote = (value: string) => `'${value.replace(/'/g, `'\\''`)}'`;

export interface AgentRun extends TaskBase {
	readonly kind: "agent";
	/** The sub-agent's name. */
	readonly agent: string;
	readonly task: string;
	/** The child's saved session id, when it has one. */
	readonly childSessionId?: string;
	/** The sub-agent's final answer, or why it did not finish. */
	result?: string;
	/** Its latest activity, oldest first: the tools it runs, their failures, and what it says. */
	activity: string[];
	/** Tokens its assistant messages reported so far (usage.totalTokens, summed); 0 while none. */
	tokens: number;
	/** What its assistant messages cost so far (usage.cost.total, summed). */
	cost: number;
	/** The model it runs on, as its definition names it. */
	readonly model?: string;
	/** Where its saved session lives (the child's `<…>/subagents` directory); absent when unsaved. */
	readonly sessionDir?: string;
	/** Its live thread, oldest first, as the Agents view draws it: thinking, text and each tool call with its output (G's ThreadItem). */
	thread: ThreadItem[];
}

/** One item of a sub-agent's thread. */
export type ThreadItem =
	| { kind: "thinking" | "text"; text: string }
	| { kind: "tool"; callId: string; name: string; output: string; running: boolean; isError: boolean };

export type TaskRecord = JobRecord | AgentRun;

export interface JobStartRequest {
	command: string;
	cwd: string;
	ownerSessionId: string;
	label?: string;
	kind?: "command" | "monitor";
	/** The owner started it (the change detail's `t`), not the agent: no notice reaches the agent, and its tools never see it. */
	byOwner?: boolean;
	/** Called with each complete output line, and the last partial one at exit. */
	onLine?: (line: string) => void;
}

const ACTIVITY_LINES = 30;
/** Thread items kept per sub-agent, and characters kept of each tool's output (its tail). */
const THREAD_ITEMS = 200;
const TOOL_OUTPUT_CHARS = 4000;

/** The text of a tool result's content blocks. */
function resultText(result: unknown): string {
	const content = (result as { content?: unknown } | undefined)?.content;
	if (typeof content === "string") return content;
	return Array.isArray(content) ? content.filter((b: any) => b?.type === "text" && typeof b.text === "string").map((b: any) => b.text).join("\n") : "";
}

/** Grow `thread` by one child event: a tool's start and end, an assistant message's thinking and text. */
export function applyThreadEvent(thread: ThreadItem[], event: ChildEvent): void {
	const callId = typeof event.toolCallId === "string" ? event.toolCallId : "";
	const name = typeof event.toolName === "string" ? event.toolName : "tool";
	if (event.type === "tool_execution_start") thread.push({ kind: "tool", callId, name, output: "", running: true, isError: false });
	else if (event.type === "tool_execution_end") {
		const item = thread.findLast((i) => i.kind === "tool" && i.callId === callId && i.running);
		const output = resultText(event.result).trim();
		const done = { kind: "tool" as const, callId, name, output: output.length > TOOL_OUTPUT_CHARS ? `…${output.slice(-TOOL_OUTPUT_CHARS)}` : output, running: false, isError: event.isError === true };
		if (item) Object.assign(item, done);
		else thread.push(done);
	} else if (event.type === "message_end") {
		const message = event.message as { role?: unknown; content?: unknown } | undefined;
		if (message?.role !== "assistant" || !Array.isArray(message.content)) return;
		for (const block of message.content as any[]) {
			if (block?.type === "thinking" && typeof block.thinking === "string" && block.thinking.trim()) thread.push({ kind: "thinking", text: block.thinking.trim() });
			else if (block?.type === "text" && typeof block.text === "string" && block.text.trim()) thread.push({ kind: "text", text: block.text.trim() });
		}
	}
	if (thread.length > THREAD_ITEMS) thread.splice(0, thread.length - THREAD_ITEMS);
}

/** The cost one child event reports: an assistant message's usage.cost.total, 0 for any other event. */
export function messageCost(event: ChildEvent): number {
	if (event.type !== "message_end") return 0;
	const message = event.message as { role?: unknown; usage?: { cost?: { total?: unknown } } } | undefined;
	const total = message?.role === "assistant" ? message.usage?.cost?.total : undefined;
	return typeof total === "number" && Number.isFinite(total) && total > 0 ? total : 0;
}
const one = (text: string, width = 100) => { const line = text.replace(/\s+/g, " ").trim(); return line.length > width ? `${line.slice(0, width - 1)}…` : line; };
const firstLine = (text: string) => one(text.trim().split("\n")[0] ?? "", 60);

/** The activity line one child event adds, or undefined for events that add none. */
export function activityLine(event: ChildEvent): string | undefined {
	const tool = typeof event.toolName === "string" ? event.toolName : "tool";
	if (event.type === "tool_execution_start") {
		const args = (event.args ?? {}) as Record<string, unknown>;
		const subject = [args.command, args.path, args.pattern, args.agent].find((v) => typeof v === "string") as string | undefined;
		return one(subject ? `${tool} ${subject}` : tool);
	}
	if (event.type === "tool_execution_end" && event.isError === true) return `✗ ${tool} failed`;
	if (event.type === "message_end") {
		const message = event.message as { role?: unknown; content?: unknown } | undefined;
		if (message?.role !== "assistant" || !Array.isArray(message.content)) return undefined;
		const text = message.content.filter((b: any) => b?.type === "text" && typeof b.text === "string").map((b: any) => b.text).join("").trim();
		return text ? `» ${one(text.split("\n")[0] ?? "")}` : undefined;
	}
	return undefined;
}

/** The tokens one child event reports: an assistant message's usage, 0 for any other event. */
export function messageTokens(event: ChildEvent): number {
	if (event.type !== "message_end") return 0;
	const message = event.message as { role?: unknown; usage?: { totalTokens?: unknown; input?: unknown; output?: unknown; cacheRead?: unknown; cacheWrite?: unknown } } | undefined;
	if (message?.role !== "assistant" || !message.usage) return 0;
	const n = (v: unknown) => typeof v === "number" && Number.isFinite(v) && v > 0 ? v : 0;
	const u = message.usage;
	return n(u.totalTokens) || n(u.input) + n(u.output) + n(u.cacheRead) + n(u.cacheWrite);
}

/** A background sub-agent's run options: all of runAgent's but the ones the registry owns. */
export type AgentStartOptions = Omit<RunOptions, "signal" | "onEvent">;

export interface TaskRegistryDeps {
	/** Directory receiving one `<id>.log` file per job; read at each start. */
	outputDir(): string;
	now(): number;
	/** Resolved at each start so a changed shell setting applies to the next job. */
	/** The shell a job runs in; `byOwner` for a command the owner started, which no sandbox wraps (lib/sandbox.ts). */
	shell(cwd: string, byOwner?: boolean): { operations: JobExecOperations; commandPrefix?: string };
	/** Runs a sub-agent's child; onEvent receives its JSON events as they arrive. */
	runAgent(agent: AgentDef, task: string, cwd: string, options: RunOptions & { signal: AbortSignal; onEvent: (event: ChildEvent) => void }): Promise<ChildResult>;
	/** Called once when a task ends on its own, never for a stop. */
	onSettled(task: TaskRecord): void;
	/** Called when a running sub-agent's activity grows, so a view can repaint. */
	onActivity?(run: AgentRun): void;
}

export function createTaskRegistry(deps: TaskRegistryDeps) {
	const tasks = new Map<string, TaskRecord>();
	const aborts = new Map<string, AbortController>();
	// Every job's run, until its log file is closed and its exit reported.
	const runs = new Set<Promise<void>>();
	const counters = { job: 0, agent: 0 };

	const running = () => [...tasks.values()].filter((task) => task.status === "running").length;
	const admit = () => {
		if (running() >= MAX_RUNNING_TASKS) throw new Error(`${MAX_RUNNING_TASKS} background tasks are already running; stop one with task_stop first.`);
	};

	const finish = async (job: JobRecord, output: WriteStream, update: () => void) => {
		update();
		await new Promise<void>((resolve) => output.end(resolve));
		aborts.delete(job.id);
	};

	/** Run a command in the background. */
	const start = (request: JobStartRequest): JobRecord => {
		admit();
		const { operations, commandPrefix } = deps.shell(request.cwd, request.byOwner === true);
		const id = `job-${++counters.job}`;
		const job: JobRecord = {
			id,
			kind: request.kind ?? "command",
			label: request.label?.trim() || request.command,
			command: request.command,
			cwd: request.cwd,
			ownerSessionId: request.ownerSessionId,
			outputPath: join(deps.outputDir(), `${id}.log`),
			startedAt: deps.now(),
			status: "running",
			tail: [],
			...(request.byOwner ? { byOwner: true } : {}),
		};
		const output = createWriteStream(job.outputPath);
		output.on("error", () => { /* A lost log must not crash the session; the tail survives. */ });
		const tail = createOutputTail();
		// Streaming decode keeps a multibyte character split across chunks intact.
		const decoder = new TextDecoder("utf-8");
		const abort = new AbortController();
		tasks.set(id, job);
		aborts.set(id, abort);
		// A `jhon executions run` names each record it writes here, so its end reads the record.
		const report = runsExecutions(request.command) ? `${job.outputPath}.executions` : undefined;
		const command = report ? `export JHON_EXECUTIONS_REPORT=${shellQuote(report)}\n${request.command}` : request.command;
		const execCommand = commandPrefix ? `${commandPrefix}\n${command}` : command;
		let linePartial = "";
		const emitLines = (text: string, end: boolean) => {
			if (!request.onLine) return;
			const parts = (linePartial + text).split("\n");
			linePartial = end ? "" : parts.pop() ?? "";
			// Keep one extra character so a cut partial still reads as cut.
			if (linePartial.length > MAX_TAIL_LINE_CHARS) linePartial = linePartial.slice(-(MAX_TAIL_LINE_CHARS + 1));
			for (const part of parts) {
				const line = part.endsWith("\r") ? part.slice(0, -1) : part;
				request.onLine(line.length > MAX_TAIL_LINE_CHARS ? `…${line.slice(-MAX_TAIL_LINE_CHARS)}` : line);
			}
		};
		const flushDecoder = () => {
			const rest = decoder.decode();
			if (rest) {
				tail.push(rest);
				job.tail = tail.lines();
			}
			emitLines(rest, true);
		};
		const onData = (data: Buffer) => {
			output.write(data);
			const text = decoder.decode(data, { stream: true });
			tail.push(text);
			job.tail = tail.lines();
			emitLines(text, false);
		};
		const run = operations.exec(execCommand, request.cwd, { onData, signal: abort.signal }).then(
			({ exitCode }) => finish(job, output, () => {
				flushDecoder();
				if (job.status !== "running") return;
				job.status = "exited";
				job.exitCode = exitCode;
				const executions = report ? executionsResult(report) : undefined;
				if (executions) job.executions = executions;
				job.endedAt = deps.now();
			}),
			(error: unknown) => finish(job, output, () => {
				flushDecoder();
				if (job.status !== "running") return;
				job.status = "failed";
				job.error = error instanceof Error ? error.message : String(error);
				job.endedAt = deps.now();
			}),
		).then(() => {
			if (job.status === "exited" || job.status === "failed") deps.onSettled(job);
		}).finally(() => runs.delete(run));
		runs.add(run);
		return job;
	};

	/** Run a sub-agent in the background: saved in `session`, keeping a continued one's `scope`, stopped past `timeoutSeconds`. */
	const startAgent = (agent: AgentDef, task: string, cwd: string, ownerSessionId: string, options: AgentStartOptions = {}): AgentRun => {
		const { session } = options;
		admit();
		const run: AgentRun = { id: `agent-${++counters.agent}`, kind: "agent", label: `${agent.name}: ${firstLine(task)}`, agent: agent.name, task, ownerSessionId, childSessionId: session?.id, startedAt: deps.now(), status: "running", activity: [], tokens: 0, cost: 0, thread: [], model: agent.model, ...(session ? { sessionDir: session.dir } : {}) };
		const abort = new AbortController();
		tasks.set(run.id, run);
		aborts.set(run.id, abort);
		const settle = (status: TaskStatus, result: string) => {
			if (run.status !== "running") return;
			run.status = status;
			run.result = result;
			run.endedAt = deps.now();
			aborts.delete(run.id);
			deps.onSettled(run);
		};
		const onEvent = (event: ChildEvent) => {
			const used = run.status === "running" ? messageTokens(event) : 0;
			if (used) run.tokens += used;
			const before = run.thread.length;
			if (run.status === "running") {
				run.cost += messageCost(event);
				applyThreadEvent(run.thread, event);
			}
			// A tool's end changes its thread item in place.
			const grew = run.status === "running" && (run.thread.length !== before || event.type === "tool_execution_end");
			const line = run.status === "running" ? activityLine(event) : undefined;
			if (!line) {
				if (used || grew) deps.onActivity?.(run);
				return;
			}
			run.activity.push(line);
			if (run.activity.length > ACTIVITY_LINES) run.activity.splice(0, run.activity.length - ACTIVITY_LINES);
			deps.onActivity?.(run);
		};
		deps.runAgent(agent, task, cwd, { ...options, signal: abort.signal, onEvent }).then(
			(r) => settle(r.ok ? "completed" : "failed", r.text),
			(e) => settle("failed", `jhon: the sub-agent "${agent.name}" could not run: ${e instanceof Error ? e.message : String(e)}`),
		);
		return run;
	};

	const stop = (id: string): TaskRecord | undefined => {
		const task = tasks.get(id);
		if (!task) return undefined;
		if (task.status === "running") {
			task.status = "stopped";
			task.endedAt = deps.now();
			if (task.kind === "agent") task.result = `jhon: the sub-agent "${task.agent}" was stopped`;
			aborts.get(id)?.abort();
			if (task.kind === "agent") aborts.delete(id);
		}
		return task;
	};

	return {
		start,
		startAgent,
		stop,
		get: (id: string) => tasks.get(id),
		/** Tasks in start order, optionally only those one session owns. */
		list: (ownerSessionId?: string) => [...tasks.values()].filter((task) => ownerSessionId === undefined || task.ownerSessionId === ownerSessionId),
		running,
		/** Job runs not yet finished: still executing or closing their log. */
		pending: () => runs.size,
		/** Stops every running task and resolves once every job's log file is closed. */
		stopAll: async () => {
			for (const id of tasks.keys()) stop(id);
			await Promise.allSettled([...runs]);
		},
	};
}

export type TaskRegistry = ReturnType<typeof createTaskRegistry>;
