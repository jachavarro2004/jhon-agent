/**
 * shell/lib/ui/task-monitor.ts — the `monitor` tool's jobs: a background
 * command (lib/background-tasks.ts) whose every non-blank output line is an
 * event for the agent while the command keeps running, e.g.
 * `tail -f app.log | grep --line-buffered ERROR`.
 *
 * Lines that arrive within MONITOR_BATCH_MS of the first share one notice of
 * at most MONITOR_LINES_PER_NOTICE lines (the rest are counted). A monitor
 * ends at its mandatory timeout, or as a flood past MONITOR_FLOOD_LINES lines
 * in MONITOR_FLOOD_WINDOW_MS, so a noisy command cannot spend the agent's
 * turns. Timers come from the caller's `schedule`, notices go to `deliver`.
 */
import type { JobRecord, JobStartRequest, TaskRegistry } from "../background-tasks.ts";

/** Lines within this many milliseconds of a notice's first line join it. */
export const MONITOR_BATCH_MS = 200;
/** Lines one notice carries. */
export const MONITOR_LINES_PER_NOTICE = 20;
/** A monitor printing more lines than this within MONITOR_FLOOD_WINDOW_MS is stopped. */
export const MONITOR_FLOOD_LINES = 120;
export const MONITOR_FLOOD_WINDOW_MS = 60_000;
/** The longest timeout a monitor takes: 30 minutes. */
export const MONITOR_MAX_TIMEOUT_SECONDS = 1800;

/** The lines one notice carries, and how many more arrived with them. */
export interface MonitorBatch {
	lines: string[];
	omitted: number;
}

export type MonitorStopReason = "timeout" | "flood";

export type MonitorNotice =
	| { job: JobRecord; kind: "events"; batch: MonitorBatch }
	| { job: JobRecord; kind: "stopped"; reason: MonitorStopReason };

export interface MonitorControllerDeps {
	registry: TaskRegistry;
	/** Runs `fn` after `ms`; returns what cancels it. */
	schedule(fn: () => void, ms: number): () => void;
	now(): number;
	/** Receives each notice, in order. */
	deliver(notice: MonitorNotice): void;
}

export interface MonitorStartRequest extends Omit<JobStartRequest, "kind" | "onLine"> {
	/** The monitor is stopped after this many seconds: a whole number from 1 to MONITOR_MAX_TIMEOUT_SECONDS. */
	timeoutSeconds: number;
}

/** One live monitor: its open batch, its line times inside the flood window, its timers. */
interface Watch {
	readonly job: JobRecord;
	batch?: MonitorBatch;
	readonly arrivals: number[];
	lines: number;
	cancelBatch?: () => void;
	readonly cancelTimeout: () => void;
}

export function createMonitorController(deps: MonitorControllerDeps) {
	const watches = new Map<string, Watch>();

	/** Delivers the open batch, if any. */
	const flush = (watch: Watch) => {
		watch.cancelBatch?.();
		watch.cancelBatch = undefined;
		const batch = watch.batch;
		watch.batch = undefined;
		if (batch) deps.deliver({ job: watch.job, kind: "events", batch });
	};
	/** Forgets a monitor: its timers go, its open batch is dropped. */
	const cancel = (id: string) => {
		const watch = watches.get(id);
		if (!watch) return;
		watches.delete(id);
		watch.cancelBatch?.();
		watch.cancelTimeout();
	};
	/** Stops a running monitor at a limit: its lines so far, then why it stopped. */
	const halt = (watch: Watch, reason: MonitorStopReason) => {
		// A command that ended first reports its own end.
		if (watch.job.status !== "running") return;
		flush(watch);
		cancel(watch.job.id);
		deps.registry.stop(watch.job.id);
		deps.deliver({ job: watch.job, kind: "stopped", reason });
	};
	const line = (id: string | undefined, text: string) => {
		const watch = id === undefined ? undefined : watches.get(id);
		// Nothing more from a monitor that ended, stopped or was let go.
		if (!watch || watch.job.status !== "running" || text.trim() === "") return;
		const at = deps.now();
		watch.lines++;
		watch.job.events = watch.lines;
		watch.arrivals.push(at);
		while (watch.arrivals.length > 0 && at - watch.arrivals[0]! >= MONITOR_FLOOD_WINDOW_MS) watch.arrivals.shift();
		const opened = watch.batch === undefined;
		const batch = (watch.batch ??= { lines: [], omitted: 0 });
		if (batch.lines.length < MONITOR_LINES_PER_NOTICE) batch.lines.push(text);
		else batch.omitted++;
		if (watch.arrivals.length > MONITOR_FLOOD_LINES) return halt(watch, "flood");
		if (opened) watch.cancelBatch = deps.schedule(() => flush(watch), MONITOR_BATCH_MS);
	};

	return {
		/** Starts a monitor; throws before anything runs when the timeout is out of range. */
		start(request: MonitorStartRequest): JobRecord {
			const { timeoutSeconds, ...job } = request;
			if (!Number.isInteger(timeoutSeconds) || timeoutSeconds < 1 || timeoutSeconds > MONITOR_MAX_TIMEOUT_SECONDS) {
				throw new Error(`timeout_seconds must be a whole number from 1 to ${MONITOR_MAX_TIMEOUT_SECONDS}.`);
			}
			let id: string | undefined;
			const started = deps.registry.start({ ...job, kind: "monitor", onLine: (text) => line(id, text) });
			id = started.id;
			started.events = 0;
			started.timeoutSeconds = timeoutSeconds;
			const watch: Watch = { job: started, arrivals: [], lines: 0, cancelTimeout: deps.schedule(() => halt(watch, "timeout"), timeoutSeconds * 1000) };
			watches.set(started.id, watch);
			return started;
		},
		/** The command ended on its own: its open batch goes out before its end. */
		finish(job: JobRecord) {
			const watch = watches.get(job.id);
			if (!watch) return;
			flush(watch);
			cancel(job.id);
		},
		/** task_stop, or the session's end: timers and the open batch are dropped. */
		cancel,
		cancelAll() {
			for (const id of [...watches.keys()]) cancel(id);
		},
	};
}
