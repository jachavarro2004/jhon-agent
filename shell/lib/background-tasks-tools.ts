/**
 * shell/lib/background-tasks-tools.ts — the main session's background tasks
 * (lib/background-tasks.ts) as the agent and the owner use them.
 *
 * The agent starts a command with `bash_background` or `monitor`, and a
 * sub-agent with `subagent` (extensions/jhon.ts, through BackgroundTasks);
 * `task_list`, `task_result` and `task_stop` take any of them by id. The
 * owner sees them in /jhon:agents and /jhon:jobs (alt+s, alt+o), where `x`
 * stops one and `o` opens a sub-agent's saved session in $VISUAL or $EDITOR.
 *
 * A task's end, or a monitor's lines, reach the agent as one follow-up
 * message, which starts a turn when the agent is idle. One that lands during
 * a turn waits here until the turn settles. A task the agent stopped sends
 * nothing; one the owner stopped is reported, since the agent was told it
 * would hear. The owner's own command (the change detail's `t`) ends in its
 * panel. A headless run waits for its tasks before it ends. A sub-agent gets
 * none of this: its tasks would die with it.
 */
import { createLocalBashOperations, getAgentDir, keyHint, SettingsManager, type ExtensionContext } from "@earendil-works/pi-coding-agent";
import { mkdtempSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { runAgent, type AgentDef } from "./agents.ts";
import { BASH_BACKGROUND_TOOL, createTaskRegistry, isStandaloneSleep, MAX_RUNNING_TASKS, MONITOR_TOOL, type AgentRun, type AgentStartOptions, type JobExecOperations, type JobRecord, type TaskRecord, type TaskRegistryDeps } from "./background-tasks.ts";
import { OPEN_TASKS_EVENT, TASKS_EVENT } from "./events.ts";
import { foregroundWaitNote } from "./foreground-wait.ts";
import { shellSettings, type ShellSettings } from "./settings.ts";
import { recordUse } from "./ui-use.ts";
import { openInExternalEditor } from "./ui/jhon-panel-editor.ts";
import { closeWithRepaint } from "./ui/overlay.ts";
import { JOBS_FINISHED_TTL_MS, shellEnabled } from "./ui/panel.ts";
import { CARD_TONE, renderCard, type CardTheme } from "./ui/primitives/index.ts";
import { TasksView } from "./ui/tasks-view.ts";
import { createMonitorController, MONITOR_BATCH_MS, MONITOR_FLOOD_LINES, MONITOR_FLOOD_WINDOW_MS, MONITOR_LINES_PER_NOTICE, MONITOR_MAX_TIMEOUT_SECONDS, type MonitorBatch, type MonitorNotice, type MonitorStopReason } from "./ui/task-monitor.ts";
import { sanitizeTerminalText } from "./ui/terminal-theme.ts";

/** The custom message type of every task notice. */
export const TASK_NOTICE_TYPE = "jhon-tasks.notice";
export const TASK_GLYPH = "⧗";
/** pi's status line entry that counts running tasks while the shell is off. */
const STATUS_KEY = "jhon-tasks";

/**
 * The key that opens /jhon:agents: shell.keys.agents, alt+s by default
 * (JHON_SHELL_TASKS_KEY overrides it), or off. alt+a selects all in the
 * prompt and pi binds alt+b/d/f/p/q/v/y/z; alt+s is free.
 */
export function backgroundTasksKey(settings: ShellSettings = shellSettings()): string | undefined {
	return settings.key("agents");
}

/** The key that opens /jhon:jobs: shell.keys.jobs, alt+o by default (JHON_SHELL_JOBS_KEY overrides it), or off (T174-S5b, L1495). */
export function jobsKey(settings: ShellSettings = shellSettings()): string | undefined {
	return settings.key("jobs");
}

// ---------------------------------------------------------------- texts

/** `42s`, `3m 5s`, `2h 10m`. */
export function formatTaskDuration(ms: number): string {
	const total = Math.max(0, Math.round(ms / 1000));
	const minutes = Math.floor(total / 60);
	if (minutes === 0) return `${total}s`;
	if (minutes < 60) return `${minutes}m ${total % 60}s`;
	return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

const count = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;
const named = (task: TaskRecord) => `${task.id} ("${task.label}")`;
const elapsed = (task: TaskRecord, now: number) => formatTaskDuration((task.endedAt ?? now) - task.startedAt);

/** How a task stands or ended, in a few words: for its notice, task_list and the views. */
export function taskOutcome(task: TaskRecord): string {
	switch (task.status) {
		case "running":
		case "stopped":
		case "completed":
			return task.status;
		case "failed":
			return task.kind === "agent" ? "failed" : `could not run: ${task.error ?? "unknown error"}`;
	}
	if (task.kind === "agent") return "failed";
	const exit = task.exitCode == null ? "exited without an exit code" : `exited with code ${task.exitCode}`;
	const records = task.executions;
	return records?.failed ? `${exit}, but jhon executions recorded a failure (${records.failures} of ${records.total} failed; record ${records.records.join(", ")})` : exit;
}

/** Whether a task ended well: a sub-agent answered, or a command exited 0 with no failed execution record. */
export function taskSucceeded(task: TaskRecord): boolean {
	if (task.status === "completed") return true;
	return task.kind !== "agent" && task.status === "exited" && task.exitCode === 0 && !task.executions?.failed;
}

/**
 * What the agent reads of a task: enough to act on without polling. It is
 * task_result's answer and, with a long sub-agent result bounded
 * (taskNoticeText), the end notice; `stoppedBy` says the owner stopped it.
 */
export function taskText(task: TaskRecord, now: number, stoppedBy?: "owner"): string {
	const time = elapsed(task, now);
	let outcome: string;
	if (task.status === "running") outcome = `is running (${time})`;
	else if (task.status === "stopped") outcome = `was stopped${stoppedBy === undefined ? "" : " by the owner"} after ${time}`;
	else outcome = `${taskOutcome(task)} after ${time}`;
	if (task.kind === "agent") {
		const head = `Background sub-agent ${task.id} (${task.agent}) ${outcome}.`;
		if (task.status === "running") return task.activity.length === 0 ? head : [head, "Recent activity:", ...task.activity.slice(-5)].join("\n");
		return task.result ? `${head}\n\n${task.result}` : head;
	}
	const events = task.kind === "monitor" ? `, ${count(task.events ?? 0, "event")}` : "";
	return [
		`${task.kind === "monitor" ? "Monitor" : "Background job"} ${named(task)} ${outcome}${events}.`,
		`Command: ${task.command}`,
		...(task.tail.length === 0 ? ["No output."] : ["Last output:", ...task.tail]),
		`Full output: ${task.outputPath} (read it with the read tool if you need more).`,
	].join("\n");
}

/**
 * The longest sub-agent result an end notice carries whole (T115b, L691).
 * A notice stays in the agent's context for every later turn; in the L689
 * session they averaged 3.3K characters and one carried a 21K review report.
 * task_result still returns the whole result.
 */
export const NOTICE_RESULT_MAX_CHARS = 1_500;
/** A summarized JSON field's characters, and the findings it lists. */
const FIELD_CHARS = 160;
const FINDINGS_LISTED = 5;

/** Severities by rank, most severe first; any other comes last. */
const SEVERITY_ORDER: ReadonlyArray<readonly string[]> = [["CRITICAL", "BLOCKER", "HIGH", "ERROR"], ["WARNING", "MEDIUM"], ["LOW", "INFO", "SUGGESTION"]];
function severityRank(severity: unknown): number {
	if (typeof severity !== "string") return SEVERITY_ORDER.length;
	const rank = SEVERITY_ORDER.findIndex((level) => level.includes(severity.toUpperCase()));
	return rank < 0 ? SEVERITY_ORDER.length : rank;
}

/** `value` on one line, at most `max` characters, cut with `…`. */
function field(value: unknown, max = FIELD_CHARS): string {
	const flat = String(value).replace(/\s+/g, " ").trim();
	return flat.length <= max ? flat : `${flat.slice(0, max - 1)}…`;
}

const isScalar = (value: unknown) => value === null || ["string", "number", "boolean"].includes(typeof value);

/** A result that is one JSON object (a review facet's report, perhaps in a ```json fence), parsed; undefined otherwise. */
function parseObject(result: string): Record<string, unknown> | undefined {
	const fenced = /^```(?:json)?\s*\n([\s\S]*)\n```$/.exec(result.trim());
	const body = fenced ? fenced[1]! : result.trim();
	if (!body.startsWith("{")) return undefined;
	try {
		const parsed: unknown = JSON.parse(body);
		return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed) ? parsed as Record<string, unknown> : undefined;
	} catch {
		return undefined;
	}
}

/** `name n, name n` of a tally, in first-seen order. */
const tally = (counts: Map<string, number>) => [...counts].map(([name, n]) => `${name} ${n}`).join(", ");
const bump = (counts: Map<string, number>, name: string) => counts.set(name, (counts.get(name) ?? 0) + 1);

/** A JSON result by its key fields: scalars, an object's scalars, each array's size, and its findings by severity. */
function jsonSummary(report: Record<string, unknown>, size: number, id: string): string {
	const out = ["JSON result, summarized:"];
	for (const [key, value] of Object.entries(report)) {
		if (isScalar(value)) {
			out.push(`${key}: ${field(value)}`);
		} else if (Array.isArray(value)) {
			const items = value as Array<Record<string, unknown> | null>;
			const severities = new Map<string, number>();
			for (const item of items) if (typeof item?.severity === "string") bump(severities, item.severity);
			out.push(severities.size === 0 ? `${key}: ${items.length}` : `${key}: ${items.length} (${tally(severities)})`);
			if (severities.size === 0) continue;
			// A stable sort: findings of one severity keep the report's order.
			const ranked = [...items].sort((a, b) => severityRank(a?.severity) - severityRank(b?.severity));
			for (const item of ranked.slice(0, FINDINGS_LISTED)) {
				const where = [item?.severity, item?.location].filter((part) => typeof part === "string").join(" ");
				out.push(`- ${where}: ${field(item?.claim ?? item?.message ?? "")}`);
			}
			const rest = ranked.slice(FINDINGS_LISTED);
			if (rest.length > 0) {
				const unlisted = new Map<string, number>();
				for (const item of rest) bump(unlisted, typeof item?.severity === "string" ? item.severity : "unrated");
				out.push(`- … ${rest.length} more not listed (${tally(unlisted)})`);
			}
		} else if (value !== null && typeof value === "object") {
			for (const [inner, innerValue] of Object.entries(value)) if (isScalar(innerValue)) out.push(`${key}.${inner}: ${field(innerValue)}`);
		}
	}
	out.push(`[Full JSON (${size} characters): task_result with id "${id}" returns it.]`);
	return out.join("\n");
}

/** A sub-agent's result as its end notice carries it: whole when short; else a JSON report summarized, or the text cut at a line, with task_result to read the rest. */
export function noticeResult(result: string, id: string): string {
	if (result.length <= NOTICE_RESULT_MAX_CHARS) return result;
	const report = parseObject(result);
	if (report) return jsonSummary(report, result.length, id);
	const lineEnd = result.lastIndexOf("\n", NOTICE_RESULT_MAX_CHARS);
	const shown = result.slice(0, lineEnd > NOTICE_RESULT_MAX_CHARS / 2 ? lineEnd : NOTICE_RESULT_MAX_CHARS);
	return `${shown}\n[${result.length - shown.length} more characters not shown; task_result with id "${id}" returns the full result.]`;
}

/** A task's end notice: its taskText, a long sub-agent result bounded by noticeResult. */
export function taskNoticeText(task: TaskRecord, now: number, stoppedBy?: "owner"): string {
	const long = task.kind === "agent" && task.result !== undefined && task.result.length > NOTICE_RESULT_MAX_CHARS;
	return taskText(long ? { ...task, result: noticeResult(task.result!, task.id) } : task, now, stoppedBy);
}

/** A batch of a monitor's lines: a count line, the lines, and how many more did not fit. */
function batchText(job: JobRecord, { lines, omitted }: MonitorBatch): string {
	const rows = [`Monitor ${named(job)} reported ${count(lines.length, "new line")}:`, ...lines];
	if (omitted > 0) rows.push(`(${count(omitted, "more line")} not shown; full output in ${job.outputPath})`);
	return rows.join("\n");
}

/** What the agent reads of a monitor: a batch of its lines, or why it was stopped (its timeout, or a flood). */
export function monitorNoticeText(notice: MonitorNotice): string {
	if (notice.kind === "events") return batchText(notice.job, notice.batch);
	const { job, reason } = notice;
	const events = count(job.events ?? 0, "event");
	const why = reason === "timeout"
		? `reached ${job.timeoutSeconds === undefined ? "its timeout" : `its ${job.timeoutSeconds}s timeout`} after ${events} and was stopped. Start a new monitor if you still need to watch it.`
		: `was stopped: it printed more than ${MONITOR_FLOOD_LINES} lines within ${MONITOR_FLOOD_WINDOW_MS / 1000}s (${events}). Narrow the command's filter before watching it again.`;
	return `Monitor ${named(job)} ${why}\nFull output: ${job.outputPath}`;
}

/** One task_list row. */
function listRow(task: TaskRecord, now: number): string {
	const status = task.kind !== "agent" && task.status === "exited" ? `exited ${task.exitCode ?? "?"}` : task.status;
	const row = [task.id, task.kind, status, task.label, elapsed(task, now)].join(" · ");
	return task.kind === "agent" ? row : `${row} · ${task.outputPath}`;
}

/** The details a task's tool result and notice carry, for their cards. */
export function taskDetails(task: TaskRecord, event?: "events" | MonitorStopReason) {
	const job = task.kind === "agent" ? {} : { exitCode: task.exitCode ?? null, outputPath: task.outputPath };
	return { jhonTasks: { id: task.id, kind: task.kind, label: task.label, status: task.status, succeeded: taskSucceeded(task), ...job, ...(event ? { event } : {}) } };
}

/** A tool's answer: one text block for the model, and details for its card. */
const reply = (text: string, details: Record<string, unknown> = {}) => ({ content: [{ type: "text" as const, text }], details });

// ---------------------------------------------------------------- registration

export interface BackgroundTasksDeps {
	env?: NodeJS.ProcessEnv;
	/** The owner's settings (lib/settings.ts); the process's by default. */
	settings?: ShellSettings;
	/** Where each job's log goes; one jhon-tasks- directory in the system temp directory by default. */
	outputDir?: () => string;
	/** The shell jobs run in; pi's configured Bash by default. */
	shell?: (cwd: string, byOwner?: boolean) => { operations: JobExecOperations; commandPrefix?: string };
	/**
	 * The sandbox's operations for a command the agent starts (lib/sandbox.ts):
	 * undefined runs it unwrapped; a refusal is thrown before any job starts.
	 */
	sandbox?: (shellPath: string | undefined) => JobExecOperations | undefined;
	/** How a background sub-agent's child runs; lib/agents.ts by default. */
	runAgent?: TaskRegistryDeps["runAgent"];
	now?: () => number;
	schedule?: (fn: () => void, ms: number) => () => void;
	/**
	 * How long a headless run (`pi -p`, `--mode json`) waits at one settle for
	 * its background work before it ends anyway (L741): JHON_HEADLESS_WAIT_SECONDS,
	 * an hour by default.
	 */
	headlessWaitMs?: number;
	/** Where a headless run says it stopped waiting; stderr by default. */
	warn?: (line: string) => void;
}

/** What the `subagent` tools and the change detail need of the background tasks. */
export interface BackgroundTasks {
	startAgent(agent: AgentDef, task: string, cwd: string, ownerSessionId: string, options?: AgentStartOptions): AgentRun;
	/** The session's background sub-agent `id`, if it has one. */
	agentRun(id: string, ownerSessionId: string): AgentRun | undefined;
	/**
	 * Runs a command for the owner (the change detail's `t`): the pane shows
	 * it, its end goes to `onEnd` and never to the agent, and the agent's task
	 * tools never see it.
	 */
	startOwnerCommand(command: string, cwd: string, ownerSessionId: string, label: string, onEnd: (job: JobRecord) => void): JobRecord;
	/** Stops a command the owner started; `onEnd` hears it. */
	stopOwnerCommand(id: string): void;
}

type TaskNotice = { task: TaskRecord; kind: "end"; stoppedBy?: "owner" } | MonitorNotice;

const DEFAULT_HEADLESS_WAIT_SECONDS = 3600;
/** A follow-up that starts a turn when the agent is idle. */
const followUp = () => ({ triggerTurn: true, deliverAs: "followUp" });

/** A run pi ends at its settle and never wakes again: print and json mode. */
function isHeadless(ctx: ExtensionContext | undefined): boolean {
	try { return ctx?.mode === "print" || ctx?.mode === "json"; } catch { return false; }
}
/** Whether a turn runs; a stale context runs none. */
function inTurn(ctx: ExtensionContext | undefined): boolean {
	try { return ctx?.isIdle?.() === false; } catch { return false; }
}
const ownersOwn = (task: TaskRecord) => task.kind !== "agent" && task.byOwner === true;

/** Registers the background tasks on the main session's `pi`; a sub-agent gets none (undefined). */
export function registerBackgroundTasks(pi: any, deps: BackgroundTasksDeps = {}): BackgroundTasks | undefined {
	const env = deps.env ?? process.env;
	if (env.JHON_PI_CHILD === "1") return undefined;
	const now = deps.now ?? Date.now;
	const schedule = deps.schedule ?? ((fn: () => void, ms: number) => { const timer = setTimeout(fn, ms); return () => clearTimeout(timer); });
	let logs: string | undefined;
	let session: ExtensionContext | undefined;
	/** Repaints the open Agents or Jobs view. */
	let repaintView: (() => void) | undefined;
	const sessionId = () => session?.sessionManager?.getSessionId?.() ?? "";
	const callerId = (ctx: ExtensionContext | undefined) => ctx?.sessionManager?.getSessionId?.() ?? sessionId();

	// ---- delivery

	/** Where each of the owner's commands reports its end. */
	const ownerEnds = new Map<string, (job: JobRecord) => void>();
	/** Notices that came during a turn, sent in order once it settles. */
	let held: Array<Record<string, unknown>> = [];
	/** Wakes a headless run waiting at its settle, on any change. */
	let wake: (() => void) | undefined;

	const deliver = (notice: TaskNotice) => {
		const task = notice.kind === "end" ? notice.task : notice.job;
		if (task.ownerSessionId !== sessionId()) return;
		if (ownersOwn(task)) {
			// The owner's command ends in its panel, never as a turn of the agent.
			const onEnd = ownerEnds.get(task.id);
			ownerEnds.delete(task.id);
			onEnd?.(task as JobRecord);
			return;
		}
		const event = notice.kind === "end" ? undefined : notice.kind === "events" ? "events" : notice.reason;
		const content = notice.kind === "end" ? taskNoticeText(task, now(), notice.stoppedBy) : monitorNoticeText(notice);
		const message = { customType: TASK_NOTICE_TYPE, content, display: true, details: taskDetails(task, event) };
		// pi queues a follow-up sent during a turn, and the owner's Esc clears
		// that queue: agent-2's notice was lost so (T47, L164). Held here, it
		// goes out when the turn settles.
		if (inTurn(session)) {
			held.push(message);
			wake?.();
		} else pi.sendMessage(message, followUp());
	};

	// ---- the registry and the monitors

	const tasks = createTaskRegistry({
		outputDir: deps.outputDir ?? (() => (logs ??= mkdtempSync(join(tmpdir(), "jhon-tasks-")))),
		now,
		shell: deps.shell ?? ((cwd, byOwner) => {
			const settings = SettingsManager.create(cwd, getAgentDir());
			const shellPath = settings.getShellPath();
			// The agent's command runs in the sandbox when the session's mode says so (T128b); the owner's never does.
			const sandboxed = byOwner ? undefined : deps.sandbox?.(shellPath);
			return { operations: sandboxed ?? createLocalBashOperations({ shellPath }), commandPrefix: settings.getShellCommandPrefix() };
		}),
		runAgent: deps.runAgent ?? runAgent,
		onSettled: (task) => {
			// A monitor's open batch goes out before its end.
			if (task.kind === "monitor") watchers.finish(task);
			deliver({ task, kind: "end" });
			announce();
		},
		onActivity: () => {
			repaintView?.();
			announce();
		},
	});
	const watchers = createMonitorController({ registry: tasks, schedule, now, deliver });

	/** Tells the shell UI what runs and what ended in the last minute; with the shell off, pi's status line counts the running tasks. */
	const announce = () => {
		wake?.();
		const mine = tasks.list(sessionId());
		const running = mine.filter((task) => task.status === "running").sort((a, b) => b.startedAt - a.startedAt);
		// The newest three finished commands or monitors (L168), and sub-agents (T174-S5b).
		const ended = mine.filter((task) => task.status !== "running" && task.endedAt !== undefined && now() - task.endedAt < JOBS_FINISHED_TTL_MS).sort((a, b) => b.endedAt! - a.endedAt!);
		const finished = [...ended.filter((task) => task.kind !== "agent").slice(0, 3), ...ended.filter((task) => task.kind === "agent").slice(0, 3)];
		const tokens = (task: TaskRecord) => (task.kind === "agent" ? { tokens: task.tokens } : {});
		pi.events?.emit?.(TASKS_EVENT, {
			sessionId: sessionId(),
			tasks: running.map((task) => {
				const step = task.kind === "agent" ? task.activity.at(-1) : undefined;
				return { id: task.id, kind: task.kind, label: task.label, startedAt: task.startedAt, ...(step ? { step } : {}), ...tokens(task) };
			}),
			finished: finished.map((task) => ({ id: task.id, kind: task.kind, label: task.label, startedAt: task.startedAt, endedAt: task.endedAt, status: task.status, exitCode: task.kind === "agent" ? null : task.exitCode ?? null, ...tokens(task) })),
		});
		if (shellEnabled(env)) return;
		try { session?.ui.setStatus(STATUS_KEY, running.length > 0 ? `${TASK_GLYPH} ${running.length} running` : undefined); } catch { /* Only the status line. */ }
	};

	// ---- the tools

	/** The caller's task `id`; the owner's own commands are not the agent's to see. */
	const callersTask = (ctx: ExtensionContext, id: unknown): TaskRecord => {
		const task = typeof id === "string" ? tasks.get(id) : undefined;
		if (task && task.ownerSessionId === callerId(ctx) && !ownersOwn(task)) return task;
		throw new Error(`No background task ${String(id)} in this session. task_list lists them.`);
	};
	const label = (value: unknown) => (typeof value === "string" ? value : undefined);
	const taskId = { type: "object", additionalProperties: false, required: ["id"], properties: { id: { type: "string", description: "The task's id, e.g. job-1 or agent-2." } } };

	pi.registerTool({
		name: BASH_BACKGROUND_TOOL,
		label: "Bash (background)",
		description: [
			"Run a shell command in the background; returns a task id at once. You are notified once when it exits (exit code, last lines, output file); do not poll, sleep, or delegate a subagent to wait.",
			"Use it for anything over about 30 seconds or of unknown duration (CI, builds, long tests, servers); quick checks stay in bash.",
			"Make the command's exit the event, e.g. `gh run watch <run-id> --exit-status`.",
			`Read its output file with the read tool. task_stop stops it. At most ${MAX_RUNNING_TASKS} run at once.`,
		].join(" "),
		parameters: {
			type: "object",
			additionalProperties: false,
			required: ["command"],
			properties: {
				command: { type: "string", description: "Shell command to run in the background, in the session working directory." },
				label: { type: "string", maxLength: 80, description: "Short human-readable label, e.g. 'CI for PR 1834'." },
			},
		},
		async execute(_id: string, params: { command?: unknown; label?: unknown }, _signal: unknown, _onUpdate: unknown, ctx: ExtensionContext) {
			const command = params.command;
			if (typeof command !== "string" || command.trim() === "") throw new Error("bash_background requires a non-empty command");
			const job = tasks.start({ command, cwd: ctx.cwd, ownerSessionId: callerId(ctx), label: label(params.label) });
			announce();
			return reply(`Started background job ${named(job)}.\nOutput: ${job.outputPath}\nYou will be notified once when it exits; keep working or end your turn. task_stop stops it.`, taskDetails(job));
		},
	});

	pi.registerTool({
		name: MONITOR_TOOL,
		label: "Monitor",
		description: [
			"Run a command in the background and get each output line as an event while it runs. Do not poll or sleep between events.",
			"E.g. `tail -f app.log | grep --line-buffered ERROR`. For one result when a command ends, use bash_background instead.",
			`Lines within ${MONITOR_BATCH_MS} ms arrive as one notice (at most ${MONITOR_LINES_PER_NOTICE}). timeout_seconds is mandatory (1-${MONITOR_MAX_TIMEOUT_SECONDS}); it stops at the timeout or past ${MONITOR_FLOOD_LINES} lines in ${MONITOR_FLOOD_WINDOW_MS / 1000}s, so filter the command's output.`,
			"task_stop stops it.",
		].join(" "),
		parameters: {
			type: "object",
			additionalProperties: false,
			required: ["command", "timeout_seconds"],
			properties: {
				command: { type: "string", description: "Shell command whose output lines are events, run in the session working directory." },
				timeout_seconds: { type: "integer", minimum: 1, maximum: MONITOR_MAX_TIMEOUT_SECONDS, description: "Mandatory: stop the monitor after this many seconds." },
				label: { type: "string", maxLength: 80, description: "Short human-readable label, e.g. 'CI checks for PR 1842'." },
			},
		},
		async execute(_id: string, params: { command?: unknown; label?: unknown; timeout_seconds?: unknown }, _signal: unknown, _onUpdate: unknown, ctx: ExtensionContext) {
			const command = params.command;
			if (typeof command !== "string" || command.trim() === "") throw new Error("monitor requires a non-empty command");
			const timeoutSeconds = typeof params.timeout_seconds === "number" ? params.timeout_seconds : Number.NaN;
			const job = watchers.start({ command, cwd: ctx.cwd, ownerSessionId: callerId(ctx), label: label(params.label), timeoutSeconds });
			announce();
			return reply(`Started monitor ${named(job)} for up to ${timeoutSeconds}s.\nOutput: ${job.outputPath}\nEach new output line reaches you as an event; keep working or end your turn. task_stop stops it.`, taskDetails(job));
		},
	});

	pi.registerTool({
		name: "task_list",
		label: "List background tasks",
		description: "List this session's background tasks (commands, monitors, sub-agents) with status and output file.",
		parameters: { type: "object", additionalProperties: false, properties: {} },
		async execute(_id: string, _params: unknown, _signal: unknown, _onUpdate: unknown, ctx: ExtensionContext) {
			const listed = tasks.list(callerId(ctx)).filter((task) => !ownersOwn(task));
			if (listed.length === 0) return reply("No background tasks in this session.");
			return reply(listed.map((task) => listRow(task, now())).join("\n"), { jhonTasks: { count: listed.length } });
		},
	});

	pi.registerTool({
		name: "task_result",
		label: "Task result",
		description: "Read a background task's status and result: a command's exit and last lines, or a sub-agent's answer. Its end arrives as a message anyway; do not poll.",
		parameters: taskId,
		async execute(_id: string, params: { id?: unknown }, _signal: unknown, _onUpdate: unknown, ctx: ExtensionContext) {
			const task = callersTask(ctx, params.id);
			return reply(taskText(task, now()), taskDetails(task));
		},
	});

	pi.registerTool({
		name: "task_stop",
		label: "Stop a task",
		description: "Stop a background task (a command with its process tree, or a sub-agent). A stopped task sends no notice.",
		parameters: taskId,
		async execute(_id: string, params: { id?: unknown }, _signal: unknown, _onUpdate: unknown, ctx: ExtensionContext) {
			const task = callersTask(ctx, params.id);
			const wasRunning = task.status === "running";
			watchers.cancel(task.id);
			tasks.stop(task.id);
			announce();
			if (wasRunning) return reply(`Stopped ${named(task)}.`, taskDetails(task));
			return reply(`${named(task)} had already ${task.status === "stopped" ? "been stopped" : `ended: ${taskOutcome(task)}`}.`, taskDetails(task));
		},
	});

	// ---- the notice card

	pi.registerMessageRenderer?.(TASK_NOTICE_TYPE, (message: any, options: any, theme: unknown) => {
		const info = (message.details as { jhonTasks?: { label?: string; kind?: string; event?: string; succeeded?: boolean } } | undefined)?.jhonTasks;
		const content: string = typeof message.content === "string" ? message.content : message.content.map((part: any) => (part.type === "text" ? part.text : "")).join("\n");
		const body = sanitizeTerminalText(content).split("\n");
		const tone = info?.event === "events" ? CARD_TONE.INFO : info?.succeeded ? CARD_TONE.SUCCESS : CARD_TONE.ERROR;
		const title = info?.kind === "monitor" ? "Monitor" : info?.kind === "agent" ? "Sub-agent" : "Background job";
		const action = options.expanded ? "collapse" : "expand";
		let hint = action;
		try { hint = keyHint("app.tools.expand", action); } catch { /* No keybindings: the bare word. */ }
		return {
			render: (width: number) => renderCard({ title, subtitle: info?.label, body, tone, glyph: TASK_GLYPH }, theme as CardTheme, width, { expanded: options.expanded, previewRows: 3, hint }),
			invalidate() {},
		};
	});

	// ---- waits in the foreground

	// A bash call that only waits holds the turn, while each background task's
	// end arrives as a message anyway. It still runs (T155, L1394: advisory,
	// refused from T119 until then); its result carries one note, kept by its
	// tool call id until the result comes.
	const waitNotes = new Map<string, string>();
	pi.on("tool_call", (event: any) => {
		if (event.toolName !== "bash" || typeof event.toolCallId !== "string") return undefined;
		const command = (event.input as { command?: unknown }).command;
		if (typeof command !== "string") return undefined;
		const busy = isStandaloneSleep(command) ? tasks.list(sessionId()).filter((task) => task.status === "running").length : 0;
		const note = busy > 0
			? `Standalone sleep while background work runs (${count(busy, "background task")}): it only held the turn. Each task's end arrives as a message and starts a new turn, so end the turn instead.`
			: foregroundWaitNote(command);
		if (note) waitNotes.set(event.toolCallId, note);
		return undefined;
	});
	pi.on("tool_result", (event: any) => {
		if (typeof event.toolCallId !== "string") return undefined;
		const note = waitNotes.get(event.toolCallId);
		if (!note) return undefined;
		waitNotes.delete(event.toolCallId);
		return { content: [...(event.content ?? []), { type: "text", text: note }] };
	});

	// ---- the Agents and Jobs views

	/** A sub-agent's saved session file in its directory: `<id>.jsonl` or `<timestamp>_<id>.jsonl`. */
	const savedSession = (run: AgentRun): string | undefined => {
		let files: string[];
		try { files = readdirSync(run.sessionDir ?? ""); } catch { return undefined; }
		const file = files.find((name) => name === `${run.childSessionId}.jsonl` || name.endsWith(`_${run.childSessionId}.jsonl`));
		return file === undefined ? undefined : join(run.sessionDir!, file);
	};
	const openView = async (ctx: any, kind: "agents" | "jobs" = "agents") => {
		if (!ctx?.hasUI) return;
		if (ctx.mode !== "tui") {
			ctx.ui.notify(`The ${kind === "agents" ? "Agents" : "Jobs"} view needs TUI mode.`, "warning");
			return;
		}
		let view: TasksView | undefined;
		const overlay = { overlay: true, overlayOptions: { width: "100%", maxHeight: "100%", margin: 0, anchor: "center" } };
		await ctx.ui.custom((tui: any, theme: any, _keybindings: unknown, done: (value: null) => void) => {
			const close = closeWithRepaint(tui, done);
			repaintView = () => tui.requestRender();
			view = new TasksView({
				theme,
				rows: () => Math.max(0, tui.terminal.rows),
				view: kind,
				sessionId: callerId(ctx),
				tasks: (scope) => tasks.list(scope === "all" ? undefined : callerId(ctx)),
				now,
				// o: the sub-agent's saved session, in the owner's editor.
				onOpen: (run) => {
					const file = savedSession(run);
					const problem = file === undefined ? `${run.agent} ${run.id} has no session file yet.` : openInExternalEditor(tui, file);
					if (problem) ctx.ui.notify(problem, "warning");
				},
				// x: the agent was told it would hear, so the owner's stop is reported.
				onStop: (task) => {
					if (task.kind === "monitor") watchers.finish(task);
					const stopped = tasks.stop(task.id);
					if (stopped) deliver({ task: stopped, kind: "end", stoppedBy: "owner" });
					announce();
				},
				onClose: () => close(null),
				requestRender: () => tui.requestRender(),
				ascii: shellSettings().ascii(),
				icons: shellSettings().icons(),
			});
			return view;
		}, overlay).finally(() => {
			repaintView = undefined;
			view?.dispose();
		});
	};
	pi.registerCommand("jhon:agents", {
		description: "Agents: sub-agents and their live threads; a shows every session's",
		handler: async (_args: string, ctx: any) => openView(ctx, "agents"),
	});
	pi.registerCommand("jhon:jobs", {
		description: "Jobs: this session's background commands and monitors, running first",
		handler: async (_args: string, ctx: any) => openView(ctx, "jobs"),
	});
	// A click on an Agents, Jobs or Running row asks for a view; each click is counted as a command is (T171, lib/ui-use.ts).
	pi.events?.on?.(OPEN_TASKS_EVENT, (payload: { ctx?: ExtensionContext; view?: unknown } | undefined) => {
		if (!payload?.ctx) return;
		const view = payload.view === "jobs" ? "jobs" : "agents";
		recordUse(pi, `click ${view}`);
		void openView(payload.ctx, view);
	});
	const settings = deps.settings ?? shellSettings(env);
	const agentsShortcut = backgroundTasksKey(settings);
	if (agentsShortcut) pi.registerShortcut?.(agentsShortcut, { description: "Opens the Agents view (the rail's AGENTS)", handler: async (ctx: any) => openView(ctx, "agents") });
	const jobsShortcut = jobsKey(settings);
	if (jobsShortcut) pi.registerShortcut?.(jobsShortcut, { description: "Opens the Jobs view (the pane's Jobs)", handler: async (ctx: any) => openView(ctx, "jobs") });

	// ---- the session

	pi.on("session_start", (_event: unknown, ctx: ExtensionContext) => {
		session = ctx;
		announce();
	});
	pi.on("agent_settled", (event: { aborted?: boolean } | undefined, ctx: ExtensionContext) => {
		if (held.length === 0 || inTurn(ctx ?? session)) return;
		const out = held;
		held = [];
		// After the owner's Esc a notice rides with the next message instead of restarting the agent just stopped.
		for (const message of out) pi.sendMessage(message, event?.aborted ? { deliverAs: "nextTurn" } : followUp());
	});

	// A headless run cannot be woken after it settles: pi's print mode returns
	// and the process exits (L741). While this session's work runs, the run
	// waits at its settle; each notice then goes out as a follow-up pi runs
	// before settling, as in a terminal, until nothing runs or the wait ends.
	const awaited = () => tasks.list(sessionId()).filter((task) => task.status === "running" && !ownersOwn(task));
	const configured = Number(env.JHON_HEADLESS_WAIT_SECONDS);
	const headlessWaitMs = deps.headlessWaitMs ?? (configured > 0 ? configured : DEFAULT_HEADLESS_WAIT_SECONDS) * 1000;
	const warn = deps.warn ?? ((line: string) => { process.stderr.write(`${line}\n`); });
	/** Resolves false when a notice is held or nothing runs any more, true when the wait ran out. */
	const waitForWork = () => new Promise<boolean>((resolve) => {
		const timer = setTimeout(() => {
			wake = undefined;
			resolve(true);
		}, headlessWaitMs);
		wake = () => {
			if (held.length === 0 && awaited().length > 0) return;
			clearTimeout(timer);
			wake = undefined;
			resolve(false);
		};
	});
	pi.on("agent_before_settle", async (_event: unknown, ctx: ExtensionContext) => {
		if (!isHeadless(ctx ?? session)) return undefined;
		if (held.length === 0 && awaited().length > 0 && await waitForWork() && held.length === 0) {
			const left = awaited();
			const notice = `Headless session: stopped waiting after ${formatTaskDuration(headlessWaitMs)} for ${count(left.length, "background task")} (${left.map((task) => task.label).join(", ")}); ${left.length === 1 ? "its result is" : "their results are"} lost.`;
			warn(notice);
			pi.sendMessage({ customType: TASK_NOTICE_TYPE, content: notice, display: true }, { triggerTurn: false });
			return undefined;
		}
		const out = held;
		held = [];
		for (const message of out) pi.sendMessage(message, followUp());
		return undefined;
	});
	pi.on("session_shutdown", async () => {
		// No task outlives its session, and a stopped task sends no notice.
		held = [];
		waitNotes.clear();
		wake?.();
		watchers.cancelAll();
		const stopping = tasks.stopAll();
		announce();
		session = undefined;
		await stopping;
	});

	return {
		startAgent: (agent, task, cwd, ownerSessionId, options) => {
			const run = tasks.startAgent(agent, task, cwd, ownerSessionId, options);
			announce();
			return run;
		},
		agentRun: (id, ownerSessionId) => {
			const task = tasks.get(id);
			return task?.kind === "agent" && task.ownerSessionId === ownerSessionId ? task : undefined;
		},
		startOwnerCommand: (command, cwd, ownerSessionId, name, onEnd) => {
			const job = tasks.start({ command, cwd, ownerSessionId, label: name, byOwner: true });
			ownerEnds.set(job.id, onEnd);
			announce();
			return job;
		},
		stopOwnerCommand: (id) => {
			const task = tasks.get(id);
			if (!task || !ownersOwn(task)) return;
			// A stop reports nothing: the panel hears of it here.
			const onEnd = ownerEnds.get(id);
			ownerEnds.delete(id);
			tasks.stop(id);
			announce();
			onEnd?.(task as JobRecord);
		},
	};
}
