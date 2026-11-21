/**
 * shell/lib/background-tasks-tools.ts — background tasks in Jhon's main
 * session (lib/background-tasks.ts). `bash_background` and `monitor` start a
 * command, the `subagent` tool (extensions/jhon.ts) starts a sub-agent, and
 * every task is listed, read, and stopped with the same tools: `task_list`,
 * `task_result`, and `task_stop`. `/jhon:agents` shows the sub-agents and
 * `/jhon:jobs` the commands and monitors, each in its own view (T92, L657).
 *
 * A task's end (or a monitor's lines) reaches the agent as one follow-up
 * message through pi's own delivery, which starts a turn when the agent is
 * idle. A task the agent stopped sends nothing; one the owner stopped is
 * reported, since the agent was promised a notice. Jhon's guards judge these
 * commands as they judge bash (lib/guards.ts), and with sandbox.mode on they
 * run in the OS sandbox as bash does (lib/sandbox.ts). A sub-agent gets none of it:
 * its tasks would die with it.
 *
 * extensions/jhon.ts registers it, so the `subagent` tool shares the registry.
 */
import { createLocalBashOperations, keyHint, SettingsManager, getAgentDir, type ExtensionContext } from "@earendil-works/pi-coding-agent";
import { mkdtempSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { runAgent, type AgentDef } from "./agents.ts";
import { shellSettings, type ShellSettings } from "./settings.ts";
import { createTaskRegistry, MAX_RUNNING_TASKS, type AgentRun, type AgentStartOptions, type JobRecord, type TaskRecord, type TaskRegistryDeps } from "./background-tasks.ts";
import { TasksView } from "./ui/tasks-view.ts";
import { openInExternalEditor } from "./ui/jhon-panel-editor.ts";
import { OPEN_TASKS_EVENT, TASKS_EVENT } from "./events.ts";
import { recordUse } from "./ui-use.ts";
import { BASH_BACKGROUND_TOOL, isStandaloneSleep, MONITOR_TOOL, type JobExecOperations } from "./ui/background-jobs.ts";
import { foregroundWaitNote } from "./foreground-wait.ts";
import { createMonitorController, MONITOR_BATCH_MS, MONITOR_FLOOD_LINES, MONITOR_FLOOD_WINDOW_MS, MONITOR_LINES_PER_NOTICE, MONITOR_MAX_TIMEOUT_SECONDS, type MonitorBatch, type MonitorNotice, type MonitorStopReason } from "./ui/background-monitor.ts";
import { withOverlayRepaint } from "./ui/overlay-repaint.ts";
import { CARD_TONE, renderCard, type CardTheme } from "./ui/primitives/index.ts";
import { JOBS_FINISHED_TTL_MS, shellEnabled } from "./ui/panel.ts";
import { sanitizeTerminalText } from "./ui/terminal-theme.ts";

export const TASK_NOTICE_TYPE = "jhon-tasks.notice";
export const TASK_GLYPH = "⧗";
const TASKS_STATUS_KEY = "jhon-tasks";
// The rail's BACKGROUND key (L96). alt+a is the prompt's select-all alias
// (prompt-selection.ts, advertised in its bottom rule), and pi binds
// alt+b/d/f/p/q/v/y/z: alt+s is free.

/** The key that opens /jhon:agents, shell.keys.agents: alt+s, another (JHON_SHELL_TASKS_KEY overrides it), or off. */
export function backgroundTasksKey(settings: ShellSettings = shellSettings()): string | undefined {
	return settings.key("agents");
}

/**
 * The key that opens /jhon:jobs, shell.keys.jobs (T174-S5b, L1495): alt+o,
 * another (JHON_SHELL_JOBS_KEY overrides it), or off. pi binds no alt+o and
 * the shell's other keys leave it free.
 */
export function jobsKey(settings: ShellSettings = shellSettings()): string | undefined {
	return settings.key("jobs");
}

export function formatTaskDuration(ms: number): string {
	const seconds = Math.max(0, Math.round(ms / 1000));
	if (seconds < 60) return `${seconds}s`;
	const minutes = Math.floor(seconds / 60);
	if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
	return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

const quoted = (task: TaskRecord) => `${task.id} ("${task.label}")`;
const noun = (task: TaskRecord) => (task.kind === "monitor" ? "Monitor" : task.kind === "agent" ? "Background sub-agent" : "Background job");
const plural = (count: number, word: string) => `${count} ${word}${count === 1 ? "" : "s"}`;

/** One-line outcome, shared by the notice, the list, and the tasks view. */
export function taskOutcome(task: TaskRecord): string {
	if (task.status === "running" || task.status === "stopped" || task.status === "completed") return task.status;
	if (task.kind === "agent") return "failed";
	if (task.status === "failed") return `could not run: ${task.error ?? "unknown error"}`;
	const exit = task.exitCode === null || task.exitCode === undefined ? "exited without an exit code" : `exited with code ${task.exitCode}`;
	const runs = task.executions;
	if (!runs?.failed) return exit;
	return `${exit}, but jhon executions recorded a failure (${runs.failures} of ${runs.total} failed; record ${runs.records.join(", ")})`;
}

/** True for a task that ended well: a command that exited 0, or an agent that answered. */
export const taskSucceeded = (task: TaskRecord) => task.status === "completed" || (task.status === "exited" && task.kind !== "agent" && task.exitCode === 0 && !task.executions?.failed);

/**
 * Model-facing text for a task: everything needed to act without polling.
 * It is what task_result reads, and the end notice (taskNoticeText bounds a
 * long sub-agent result there); `stoppedBy` names who
 * stopped it when the agent did not.
 */
export function taskText(task: TaskRecord, now: number, stoppedBy?: "owner"): string {
	const elapsed = formatTaskDuration((task.endedAt ?? now) - task.startedAt);
	const outcome = task.status === "running" ? `is running (${elapsed})`
		: task.status === "stopped" ? `was stopped${stoppedBy ? " by the owner" : ""} after ${elapsed}`
		: `${taskOutcome(task)} after ${elapsed}`;
	if (task.kind === "agent") {
		const head = `${noun(task)} ${task.id} (${task.agent}) ${outcome}.`;
		if (task.status === "running") return [head, ...(task.activity.length ? ["Recent activity:", ...task.activity.slice(-5)] : [])].join("\n");
		return task.result ? `${head}\n\n${task.result}` : head;
	}
	const events = task.kind === "monitor" ? `, ${plural(task.events ?? 0, "event")}` : "";
	const lines = [`${noun(task)} ${quoted(task)} ${outcome}${events}.`, `Command: ${task.command}`];
	if (task.tail.length > 0) lines.push("Last output:", ...task.tail);
	else lines.push("No output.");
	lines.push(`Full output: ${task.outputPath} (read it with the read tool if you need more).`);
	return lines.join("\n");
}

/**
 * The most of a sub-agent's result an end notice carries (T115b, L691). The
 * notice stays in the agent's context and is re-read every later turn; in the
 * L689 session notices averaged 3.3K chars and one pasted a 21K review report.
 * 1,500 keeps a typical answer whole; task_result keeps the full result.
 */
export const NOTICE_RESULT_MAX_CHARS = 1_500;
const NOTICE_FIELD_MAX_CHARS = 160;
const NOTICE_FINDINGS_SHOWN = 5;

// Most severe first, so a critical finding is never hidden behind lesser ones; unknown severities last.
const SEVERITY_RANK: Record<string, number> = { CRITICAL: 0, BLOCKER: 0, HIGH: 0, ERROR: 0, WARNING: 1, MEDIUM: 1, LOW: 2, INFO: 2, SUGGESTION: 2 };
const severityRank = (severity: unknown) => (typeof severity === "string" ? SEVERITY_RANK[severity.toUpperCase()] : undefined) ?? 3;

const clip = (value: string, max = NOTICE_FIELD_MAX_CHARS) => {
	const flat = value.replace(/\s+/g, " ").trim();
	return flat.length > max ? `${flat.slice(0, max - 1)}…` : flat;
};

/** A JSON object result (e.g. a review facet report, possibly fenced), or undefined. */
function jsonObject(result: string): Record<string, unknown> | undefined {
	const body = result.trim().replace(/^```(?:json)?\s*\n([\s\S]*)\n```$/, "$1");
	if (!body.startsWith("{")) return undefined;
	try {
		const value = JSON.parse(body);
		return value && typeof value === "object" && !Array.isArray(value) ? value : undefined;
	} catch { return undefined; }
}

/** Key fields of a JSON result: scalars, nested scalars one level down, array sizes, findings by severity. */
function summarizeJson(value: Record<string, unknown>, total: number, id: string): string {
	const lines = ["JSON result, summarized:"];
	const scalar = (v: unknown) => typeof v === "string" || typeof v === "number" || typeof v === "boolean" || v === null;
	for (const [key, field] of Object.entries(value)) {
		if (scalar(field)) lines.push(`${key}: ${clip(String(field))}`);
		else if (Array.isArray(field)) {
			const severities = new Map<string, number>();
			for (const item of field) {
				const severity = (item as { severity?: unknown } | null)?.severity;
				if (typeof severity === "string") severities.set(severity, (severities.get(severity) ?? 0) + 1);
			}
			const counts = severities.size ? ` (${[...severities].map(([s, n]) => `${s} ${n}`).join(", ")})` : "";
			lines.push(`${key}: ${field.length}${counts}`);
			if (severities.size) {
				// Array.prototype.sort is stable: equal severities keep the report's order.
				const ordered = ([...field] as Array<Record<string, unknown>>).sort((a, b) => severityRank(a?.severity) - severityRank(b?.severity));
				for (const item of ordered.slice(0, NOTICE_FINDINGS_SHOWN)) {
					lines.push(`- ${[item?.severity, item?.location].filter((v) => typeof v === "string").join(" ")}: ${clip(String(item?.claim ?? item?.message ?? ""))}`);
				}
				const unlisted = ordered.slice(NOTICE_FINDINGS_SHOWN);
				if (unlisted.length) {
					const bySeverity = new Map<string, number>();
					for (const item of unlisted) { const s = typeof item?.severity === "string" ? item.severity : "unrated"; bySeverity.set(s, (bySeverity.get(s) ?? 0) + 1); }
					lines.push(`- … ${unlisted.length} more not listed (${[...bySeverity].map(([s, n]) => `${s} ${n}`).join(", ")})`);
				}
			}
		} else if (field && typeof field === "object") {
			for (const [inner, v] of Object.entries(field)) if (scalar(v)) lines.push(`${key}.${inner}: ${clip(String(v))}`);
		}
	}
	lines.push(`[Full JSON (${total} characters): task_result with id "${id}" returns it.]`);
	return lines.join("\n");
}

/** A sub-agent's result as its end notice carries it: whole when short, else bounded with a pointer to task_result. */
export function noticeResult(result: string, id: string): string {
	if (result.length <= NOTICE_RESULT_MAX_CHARS) return result;
	const json = jsonObject(result);
	if (json) return summarizeJson(json, result.length, id);
	const cut = result.lastIndexOf("\n", NOTICE_RESULT_MAX_CHARS);
	const shown = result.slice(0, cut > NOTICE_RESULT_MAX_CHARS / 2 ? cut : NOTICE_RESULT_MAX_CHARS);
	return `${shown}\n[${result.length - shown.length} more characters not shown; task_result with id "${id}" returns the full result.]`;
}

/** The end notice: taskText, with a sub-agent's result bounded (noticeResult). */
export function taskNoticeText(task: TaskRecord, now: number, stoppedBy?: "owner"): string {
	if (task.kind !== "agent" || !task.result || task.result.length <= NOTICE_RESULT_MAX_CHARS) return taskText(task, now, stoppedBy);
	return taskText({ ...task, result: noticeResult(task.result, task.id) }, now, stoppedBy);
}

/** Model-facing notice for one batch of monitor lines. */
export function monitorEventsText(job: JobRecord, batch: MonitorBatch): string {
	const lines = [`Monitor ${quoted(job)} reported ${plural(batch.lines.length, "new line")}:`, ...batch.lines];
	if (batch.omitted > 0) lines.push(`(${plural(batch.omitted, "more line")} not shown; full output in ${job.outputPath})`);
	return lines.join("\n");
}

/** Model-facing notice for a monitor the harness stopped. */
export function monitorStoppedText(job: JobRecord, reason: MonitorStopReason): string {
	const events = plural(job.events ?? 0, "event");
	const output = `Full output: ${job.outputPath}`;
	if (reason === "timeout") {
		const limit = job.timeoutSeconds === undefined ? "its timeout" : `its ${job.timeoutSeconds}s timeout`;
		return `Monitor ${quoted(job)} reached ${limit} after ${events} and was stopped. Start a new monitor if you still need to watch it.\n${output}`;
	}
	return `Monitor ${quoted(job)} was stopped: it printed more than ${MONITOR_FLOOD_LINES} lines within ${MONITOR_FLOOD_WINDOW_MS / 1000}s (${events}). Narrow the command's filter before watching it again.\n${output}`;
}

function listLine(task: TaskRecord, now: number): string {
	const status = task.status === "exited" && task.kind !== "agent" ? `exited ${task.exitCode ?? "?"}` : task.status;
	const output = task.kind === "agent" ? "" : ` · ${task.outputPath}`;
	return `${task.id} · ${task.kind} · ${status} · ${task.label} · ${formatTaskDuration((task.endedAt ?? now) - task.startedAt)}${output}`;
}

type ToolText = { content: Array<{ type: "text"; text: string }>; details: Record<string, unknown> };
const text = (value: string, details: Record<string, unknown> = {}): ToolText => ({ content: [{ type: "text", text: value }], details });
export const taskDetails = (task: TaskRecord, event?: "events" | MonitorStopReason) => ({
	jhonTasks: { id: task.id, kind: task.kind, label: task.label, status: task.status, succeeded: taskSucceeded(task), ...(task.kind === "agent" ? {} : { exitCode: task.exitCode ?? null, outputPath: task.outputPath }), ...(event ? { event } : {}) },
});

export interface BackgroundTasksDeps {
	env?: NodeJS.ProcessEnv;
	/** The owner's settings (lib/settings.ts); the process's by default. */
	settings?: ShellSettings;
	/** Directory receiving one log per job. */
	outputDir?: () => string;
	/** The shell jobs run in; pi's configured Bash by default. */
	shell?: (cwd: string, byOwner?: boolean) => { operations: JobExecOperations; commandPrefix?: string };
	/**
	 * The sandbox's operations for a command the agent starts (lib/sandbox.ts):
	 * undefined runs it as before, and a refusal is thrown before any job starts.
	 */
	sandbox?: (shellPath: string | undefined) => JobExecOperations | undefined;
	/** The child a background sub-agent runs as; lib/agents.ts by default. */
	runAgent?: TaskRegistryDeps["runAgent"];
	now?: () => number;
	schedule?: (fn: () => void, ms: number) => () => void;
	/**
	 * How long a headless run (`pi -p`, `--mode json`) waits at one settle for
	 * its background work before it ends anyway (L741); JHON_HEADLESS_WAIT_SECONDS
	 * or an hour by default.
	 */
	headlessWaitMs?: number;
	/** Where a headless run reports the cap; stderr by default. */
	warn?: (line: string) => void;
}

const HEADLESS_WAIT_SECONDS = 3600;
/** A run pi ends at its settle and never wakes again: print and json mode (pi's print-mode.js). */
const headlessMode = (ctx: ExtensionContext | undefined) => { try { return ctx?.mode === "print" || ctx?.mode === "json"; } catch { return false; } };

/** What the `subagent` tool needs to run a sub-agent in the background. */
export interface BackgroundTasks {
	startAgent(agent: AgentDef, task: string, cwd: string, ownerSessionId: string, options?: AgentStartOptions): AgentRun;
	/** The background sub-agent `id` the session owns, if any. */
	agentRun(id: string, ownerSessionId: string): AgentRun | undefined;
	/**
	 * A command the owner starts (the change detail's `t`): BACKGROUND shows it, but
	 * its end reaches onEnd, never the agent, and the agent's task tools never
	 * see it.
	 */
	startOwnerCommand(command: string, cwd: string, ownerSessionId: string, label: string, onEnd: (job: JobRecord) => void): JobRecord;
	/** Stop a command the owner started. */
	stopOwnerCommand(id: string): void;
}

type TaskNotice = { task: TaskRecord; kind: "end"; stoppedBy?: "owner" } | MonitorNotice;

/** Register background tasks on `pi`, the main session's; a sub-agent gets none (undefined). */
export function registerBackgroundTasks(pi: any, deps: BackgroundTasksDeps = {}): BackgroundTasks | undefined {
	const env = deps.env ?? process.env;
	if (env.JHON_PI_CHILD === "1") return undefined;
	const now = deps.now ?? (() => Date.now());
	const schedule = deps.schedule ?? ((fn: () => void, ms: number) => { const timer = setTimeout(fn, ms); return () => clearTimeout(timer); });
	let outputDir: string | undefined;
	let current: ExtensionContext | undefined;
	let repaint: (() => void) | undefined;
	const sessionId = () => current?.sessionManager?.getSessionId?.() ?? "";
	const callerSession = (ctx: ExtensionContext | undefined) => ctx?.sessionManager?.getSessionId?.() ?? sessionId();

	const ownerEnds = new Map<string, (job: JobRecord) => void>();
	// Notices that landed while a turn ran, sent once, in order, when it settles.
	let held: Array<Record<string, unknown>> = [];
	// A headless run waiting at its settle; woken by any change to the tasks.
	let wake: (() => void) | undefined;
	// A stale context throws; it runs no turn.
	const running = (ctx: ExtensionContext | undefined) => { try { return ctx?.isIdle?.() === false; } catch { return false; } };
	const deliver = (notice: TaskNotice) => {
		const task = notice.kind === "end" ? notice.task : notice.job;
		if (task.ownerSessionId !== sessionId()) return;
		// The owner's own command ends in its panel, never as the agent's turn.
		if (task.kind !== "agent" && task.byOwner) {
			ownerEnds.get(task.id)?.(task);
			ownerEnds.delete(task.id);
			return;
		}
		const content = notice.kind === "events" ? monitorEventsText(notice.job, notice.batch)
			: notice.kind === "stopped" ? monitorStoppedText(notice.job, notice.reason) : taskNoticeText(task, now(), notice.stoppedBy);
		const event = notice.kind === "events" ? "events" : notice.kind === "stopped" ? notice.reason : undefined;
		const message = { customType: TASK_NOTICE_TYPE, content, display: true, details: taskDetails(task, event) };
		// pi starts a turn for an idle agent. A running turn's follow-up queue is
		// cleared by the owner's Esc, which lost agent-2's notice (T47, L164), so
		// a notice that lands mid-turn waits here until the turn settles.
		if (running(current)) { held.push(message); wake?.(); }
		else pi.sendMessage(message, { triggerTurn: true, deliverAs: "followUp" });
	};

	const tasks = createTaskRegistry({
		outputDir: deps.outputDir ?? (() => (outputDir ??= mkdtempSync(join(tmpdir(), "jhon-tasks-")))),
		now,
		shell: deps.shell ?? ((cwd, byOwner) => {
			const settings = SettingsManager.create(cwd, getAgentDir());
			const shellPath = settings.getShellPath();
			// The agent's command runs in the sandbox when the session's mode says so (T128b); the owner's own never.
			const sandboxed = byOwner ? undefined : deps.sandbox?.(shellPath);
			return { operations: sandboxed ?? createLocalBashOperations({ shellPath }), commandPrefix: settings.getShellCommandPrefix() };
		}),
		runAgent: deps.runAgent ?? runAgent,
		onSettled: (task) => {
			// A monitor's pending lines are delivered before its end.
			if (task.kind === "monitor") monitors.finish(task);
			deliver({ task, kind: "end" });
			refresh();
		},
		// A running sub-agent's latest step reaches the Status card too.
		onActivity: () => { repaint?.(); refresh(); },
	});
	const monitors = createMonitorController({ registry: tasks, schedule, now, deliver });

	// The shell UI draws the running tasks; without it, pi's status line counts them.
	const refresh = () => {
		wake?.();
		const running = tasks.list(sessionId()).filter((task) => task.status === "running").sort((a, b) => b.startedAt - a.startedAt);
		// JOBS shows a finished command or monitor for a minute beside the running
		// ones (L168), AGENTS a finished sub-agent (T174-S5b): the newest three of each.
		const ended = tasks.list(sessionId()).filter((task) => task.status !== "running" && task.endedAt !== undefined && now() - task.endedAt < JOBS_FINISHED_TTL_MS)
			.sort((a, b) => b.endedAt! - a.endedAt!);
		const finished = [...ended.filter((task) => task.kind !== "agent").slice(0, 3), ...ended.filter((task) => task.kind === "agent").slice(0, 3)];
		pi.events?.emit?.(TASKS_EVENT, { sessionId: sessionId(), tasks: running.map((task) => {
			const step = task.kind === "agent" ? task.activity.at(-1) : undefined;
			return { id: task.id, kind: task.kind, label: task.label, startedAt: task.startedAt, ...(step ? { step } : {}), ...(task.kind === "agent" ? { tokens: task.tokens } : {}) };
		}), finished: finished.map((task) => ({ id: task.id, kind: task.kind, label: task.label, startedAt: task.startedAt, endedAt: task.endedAt, status: task.status, exitCode: task.kind === "agent" ? null : task.exitCode ?? null, ...(task.kind === "agent" ? { tokens: task.tokens } : {}) })) });
		if (!shellEnabled(env)) {
			try { current?.ui.setStatus(TASKS_STATUS_KEY, running.length === 0 ? undefined : `${TASK_GLYPH} ${running.length} running`); } catch { /* Cosmetic. */ }
		}
	};

	const owned = (ctx: ExtensionContext, id: unknown): TaskRecord => {
		const task = typeof id === "string" ? tasks.get(id) : undefined;
		if (!task || task.ownerSessionId !== callerSession(ctx) || (task.kind !== "agent" && task.byOwner)) throw new Error(`No background task ${String(id)} in this session. task_list lists them.`);
		return task;
	};
	const idParameter = { type: "object", additionalProperties: false, required: ["id"], properties: { id: { type: "string", description: "The task's id, e.g. job-1 or agent-2." } } };

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
			if (typeof params.command !== "string" || !params.command.trim()) throw new Error("bash_background requires a non-empty command");
			const job = tasks.start({ command: params.command, cwd: ctx.cwd, ownerSessionId: callerSession(ctx), label: typeof params.label === "string" ? params.label : undefined });
			refresh();
			return text(`Started background job ${quoted(job)}.\nOutput: ${job.outputPath}\nYou will be notified once when it exits; keep working or end your turn. task_stop stops it.`, taskDetails(job));
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
			if (typeof params.command !== "string" || !params.command.trim()) throw new Error("monitor requires a non-empty command");
			const timeoutSeconds = typeof params.timeout_seconds === "number" ? params.timeout_seconds : Number.NaN;
			const job = monitors.start({ command: params.command, cwd: ctx.cwd, ownerSessionId: callerSession(ctx), label: typeof params.label === "string" ? params.label : undefined, timeoutSeconds });
			refresh();
			return text(`Started monitor ${quoted(job)} for up to ${timeoutSeconds}s.\nOutput: ${job.outputPath}\nEach new output line reaches you as an event; keep working or end your turn. task_stop stops it.`, taskDetails(job));
		},
	});

	pi.registerTool({
		name: "task_list",
		label: "List background tasks",
		description: "List this session's background tasks (commands, monitors, sub-agents) with status and output file.",
		parameters: { type: "object", additionalProperties: false, properties: {} },
		async execute(_id: string, _params: unknown, _signal: unknown, _onUpdate: unknown, ctx: ExtensionContext) {
			// The owner's own commands (the change detail's `t`) are theirs, not the agent's.
			const list = tasks.list(callerSession(ctx)).filter((task) => task.kind === "agent" || !task.byOwner);
			if (list.length === 0) return text("No background tasks in this session.");
			return text(list.map((task) => listLine(task, now())).join("\n"), { jhonTasks: { count: list.length } });
		},
	});

	pi.registerTool({
		name: "task_result",
		label: "Task result",
		description: "Read a background task's status and result: a command's exit and last lines, or a sub-agent's answer. Its end arrives as a message anyway; do not poll.",
		parameters: idParameter,
		async execute(_id: string, params: { id?: unknown }, _signal: unknown, _onUpdate: unknown, ctx: ExtensionContext) {
			const task = owned(ctx, params.id);
			return text(taskText(task, now()), taskDetails(task));
		},
	});

	pi.registerTool({
		name: "task_stop",
		label: "Stop a task",
		description: "Stop a background task (a command with its process tree, or a sub-agent). A stopped task sends no notice.",
		parameters: idParameter,
		async execute(_id: string, params: { id?: unknown }, _signal: unknown, _onUpdate: unknown, ctx: ExtensionContext) {
			const task = owned(ctx, params.id);
			const wasRunning = task.status === "running";
			monitors.cancel(task.id);
			tasks.stop(task.id);
			refresh();
			return text(wasRunning ? `Stopped ${quoted(task)}.` : `${quoted(task)} had already ${task.status === "stopped" ? "been stopped" : `ended: ${taskOutcome(task)}`}.`, taskDetails(task));
		},
	});

	pi.registerMessageRenderer?.(TASK_NOTICE_TYPE, (message: any, renderOptions: any, theme: unknown) => {
		const details = (message.details as { jhonTasks?: { label?: string; kind?: string; event?: string; succeeded?: boolean } } | undefined)?.jhonTasks;
		const content = typeof message.content === "string" ? message.content : message.content.map((part: any) => (part.type === "text" ? part.text : "")).join("\n");
		const lines = sanitizeTerminalText(content).split("\n");
		const tone = details?.event === "events" ? CARD_TONE.INFO : details?.succeeded ? CARD_TONE.SUCCESS : CARD_TONE.ERROR;
		const title = details?.kind === "monitor" ? "Monitor" : details?.kind === "agent" ? "Sub-agent" : "Background job";
		let hint: string;
		try { hint = keyHint("app.tools.expand", renderOptions.expanded ? "collapse" : "expand"); } catch { hint = renderOptions.expanded ? "collapse" : "expand"; }
		return {
			render(width: number) {
				return renderCard({ title, subtitle: details?.label, body: lines, tone, glyph: TASK_GLYPH }, theme as CardTheme, width, { expanded: renderOptions.expanded, previewRows: 3, hint });
			},
			invalidate() {},
		};
	});

	// A turn that sleeps while background work runs only burns the turn: each
	// end already arrives as a message. bash_background and monitor are the way
	// to wait. A foreground bash that sleeps over a few seconds or waits long
	// still runs (T155, L1394: advisory, it was refused since T119); its result
	// carries one note, by its tool call id.
	const waitNotes = new Map<string, string>();
	pi.on("tool_call", (event: any) => {
		if (event.toolName !== "bash" || typeof event.toolCallId !== "string") return undefined;
		const command = (event.input as { command?: unknown }).command;
		if (typeof command !== "string") return undefined;
		const running = isStandaloneSleep(command) ? tasks.list(sessionId()).filter((task) => task.status === "running").length : 0;
		const note = running > 0
			? `Standalone sleep while background work runs (${plural(running, "background task")}): it only held the turn. Each task's end arrives as a message and starts a new turn, so end the turn instead.`
			: foregroundWaitNote(command);
		if (note) waitNotes.set(event.toolCallId, note);
		return undefined;
	});
	pi.on("tool_result", (event: any) => {
		const note = typeof event.toolCallId === "string" ? waitNotes.get(event.toolCallId) : undefined;
		if (!note) return undefined;
		waitNotes.delete(event.toolCallId);
		return { content: [...(event.content ?? []), { type: "text", text: note }] };
	});

	/** A sub-agent's saved session file: `<id>.jsonl` or `<timestamp>_<id>.jsonl` in its directory. */
	const sessionFile = (run: AgentRun): string | undefined => {
		try {
			const name = readdirSync(run.sessionDir ?? "").find((f) => f === `${run.childSessionId}.jsonl` || f.endsWith(`_${run.childSessionId}.jsonl`));
			return name ? join(run.sessionDir!, name) : undefined;
		} catch { return undefined; }
	};
	const openTasks = async (ctx: any, kind: "agents" | "jobs" = "agents") => {
		if (!ctx?.hasUI) return;
		if (ctx.mode !== "tui") {
			ctx.ui.notify(`The ${kind === "agents" ? "Agents" : "Jobs"} view needs TUI mode.`, "warning");
			return;
		}
		let view: TasksView | undefined;
		await ctx.ui.custom((tui: any, theme: any, _keybindings: unknown, done: (value: null) => void) => {
			const close = withOverlayRepaint(tui, done);
			repaint = () => tui.requestRender();
			view = new TasksView({
				theme,
				rows: () => Math.max(0, tui.terminal.rows),
				view: kind,
				sessionId: callerSession(ctx),
				tasks: (scope) => tasks.list(scope === "all" ? undefined : callerSession(ctx)),
				now,
				// o: the agent's saved session, in the owner's editor.
				onOpen: (run) => {
					const file = sessionFile(run);
					const failed = file ? openInExternalEditor(tui, file) : `${run.agent} ${run.id} has no session file yet.`;
					if (failed) ctx.ui.notify(failed, "warning");
				},
				// The agent was promised a notice, so a stop by the owner is reported.
				onStop: (task) => {
					if (task.kind === "monitor") monitors.finish(task);
					const stopped = tasks.stop(task.id);
					if (stopped) deliver({ task: stopped, kind: "end", stoppedBy: "owner" });
					refresh();
				},
				onClose: () => close(null),
				requestRender: () => tui.requestRender(),
				ascii: shellSettings().ascii(),
				icons: shellSettings().icons(),
			});
			return view;
		}, { overlay: true, overlayOptions: { width: "100%", maxHeight: "100%", margin: 0, anchor: "center" } }).finally(() => { repaint = undefined; view?.dispose(); });
	};
	pi.registerCommand("jhon:agents", {
		description: "Agents: sub-agents and their live threads; a shows every session's",
		handler: async (_args: string, ctx: any) => openTasks(ctx, "agents"),
	});
	pi.registerCommand("jhon:jobs", {
		description: "Jobs: this session's background commands and monitors, running first",
		handler: async (_args: string, ctx: any) => openTasks(ctx, "jobs"),
	});
	// A click on a row of Agents, Jobs or the Running block asks for the Agents view, or the Jobs view;
	// each click is counted as a command is (T171, lib/ui-use.ts).
	pi.events?.on?.(OPEN_TASKS_EVENT, (payload: { ctx?: ExtensionContext; view?: unknown } | undefined) => {
		if (!payload?.ctx) return;
		const view = payload.view === "jobs" ? "jobs" : "agents";
		recordUse(pi, `click ${view}`);
		void openTasks(payload.ctx, view);
	});
	const tasksKey = backgroundTasksKey(deps.settings ?? shellSettings(env));
	if (tasksKey) pi.registerShortcut?.(tasksKey, { description: "Opens the Agents view (the rail's AGENTS)", handler: async (ctx: any) => openTasks(ctx, "agents") });
	const jobsShortcut = jobsKey(deps.settings ?? shellSettings(env));
	if (jobsShortcut) pi.registerShortcut?.(jobsShortcut, { description: "Opens the Jobs view (the pane's Jobs)", handler: async (ctx: any) => openTasks(ctx, "jobs") });

	pi.on("session_start", (_event: unknown, ctx: ExtensionContext) => {
		current = ctx;
		refresh();
	});
	pi.on("agent_settled", (event: { aborted?: boolean } | undefined, ctx: ExtensionContext) => {
		if (!held.length || running(ctx ?? current)) return;
		const pending = held;
		held = [];
		// After the owner's abort the notice rides with the next message rather
		// than restarting the agent the owner just stopped.
		for (const message of pending) pi.sendMessage(message, event?.aborted ? { deliverAs: "nextTurn" } : { triggerTurn: true, deliverAs: "followUp" });
	});
	// A headless run cannot be woken once it settles: pi's print mode returns and
	// the process exits (L741). While this session's background work runs, the
	// run waits here, then each notice goes out as a follow-up that pi runs before
	// settling, as in interactive mode, until nothing is pending or the cap.
	const pendingWork = () => tasks.list(sessionId()).filter((task) => task.status === "running" && !(task.kind !== "agent" && task.byOwner));
	const configured = Number(env.JHON_HEADLESS_WAIT_SECONDS);
	const headlessWaitMs = deps.headlessWaitMs ?? (configured > 0 ? configured : HEADLESS_WAIT_SECONDS) * 1000;
	const warn = deps.warn ?? ((line: string) => { process.stderr.write(`${line}\n`); });
	const waitForNotice = () => new Promise<boolean>((resolve) => {
		const timer = setTimeout(() => { wake = undefined; resolve(true); }, headlessWaitMs);
		wake = () => {
			if (!held.length && pendingWork().length) return;
			clearTimeout(timer);
			wake = undefined;
			resolve(false);
		};
	});
	pi.on("agent_before_settle", async (_event: unknown, ctx: ExtensionContext) => {
		if (!headlessMode(ctx ?? current)) return undefined;
		if (!held.length && pendingWork().length && await waitForNotice() && !held.length) {
			const left = pendingWork();
			const notice = `Headless session: stopped waiting after ${formatTaskDuration(headlessWaitMs)} for ${plural(left.length, "background task")} (${left.map((task) => task.label).join(", ")}); ${left.length === 1 ? "its result is" : "their results are"} lost.`;
			warn(notice);
			pi.sendMessage({ customType: TASK_NOTICE_TYPE, content: notice, display: true }, { triggerTurn: false });
			return undefined;
		}
		const pending = held;
		held = [];
		for (const message of pending) pi.sendMessage(message, { triggerTurn: true, deliverAs: "followUp" });
		return undefined;
	});
	pi.on("session_shutdown", async () => {
		// Tasks never outlive their session; a stopped task sends no notice.
		held = [];
		waitNotes.clear();
		wake?.();
		monitors.cancelAll();
		const stopped = tasks.stopAll();
		refresh();
		current = undefined;
		await stopped;
	});

	return {
		startAgent: (agent, task, cwd, ownerSessionId, options) => {
			const run = tasks.startAgent(agent, task, cwd, ownerSessionId, options);
			refresh();
			return run;
		},
		agentRun: (id, ownerSessionId) => {
			const task = tasks.get(id);
			return task?.kind === "agent" && task.ownerSessionId === ownerSessionId ? task : undefined;
		},
		startOwnerCommand: (command, cwd, ownerSessionId, label, onEnd) => {
			const job = tasks.start({ command, cwd, ownerSessionId, label, byOwner: true });
			ownerEnds.set(job.id, onEnd);
			refresh();
			return job;
		},
		stopOwnerCommand: (id) => {
			const task = tasks.get(id);
			if (task?.kind === "agent" || !task?.byOwner) return;
			// A stopped task sends no notice: the panel hears its end here.
			const end = ownerEnds.get(id);
			ownerEnds.delete(id);
			tasks.stop(id);
			refresh();
			end?.(task);
		},
	};
}
