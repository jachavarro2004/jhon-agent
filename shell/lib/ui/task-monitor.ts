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

/**
 * A monitor's limits: a notice gathers the lines that come within `batchMs`
 * of its first one, `perNotice` of them at most; more than `floodLines` lines
 * within `floodWindowMs` stop the monitor; its timeout is at most
 * `maxTimeoutSeconds` (30 minutes).
 */
export const {
	batchMs: MONITOR_BATCH_MS,
	perNotice: MONITOR_LINES_PER_NOTICE,
	floodLines: MONITOR_FLOOD_LINES,
	floodWindowMs: MONITOR_FLOOD_WINDOW_MS,
	maxTimeoutSeconds: MONITOR_MAX_TIMEOUT_SECONDS,
} = { batchMs: 200, perNotice: 20, floodLines: 120, floodWindowMs: 60_000, maxTimeoutSeconds: 1800 } as const;

/** One notice's lines, and the count of those that came with them but did not fit. */
export type MonitorBatch = { lines: string[]; omitted: number };

/** Why a monitor was stopped while its command still ran. */
export type MonitorStopReason = "flood" | "timeout";

/** What the agent hears of a monitor: a batch of its lines, or that it was stopped. */
export type MonitorNotice = { job: JobRecord } & ({ kind: "events"; batch: MonitorBatch } | { kind: "stopped"; reason: MonitorStopReason });

/** What a monitor controller is handed: the task registry, a clock, a timer whose result cancels it, and where notices go (in order). */
export type MonitorControllerDeps = {
	registry: TaskRegistry;
	now: () => number;
	schedule: (fn: () => void, ms: number) => () => void;
	deliver: (notice: MonitorNotice) => void;
};

/** A monitor's command, as a job takes it, plus its timeout: a whole number of seconds from 1 to MONITOR_MAX_TIMEOUT_SECONDS. */
export type MonitorStartRequest = Omit<JobStartRequest, "kind" | "onLine"> & { timeoutSeconds: number };

/** One live monitor: its open batch, its line times inside the flood window, its timers. */
interface Watch {
	readonly job: JobRecord;
	batch?: MonitorBatch;
	readonly arrivals: number[];
	lines: number;
	cancelBatch?: () => void;
	readonly cancelTimeout: () => void;
}

export function createMonitorController({ registry, now, schedule, deliver }: MonitorControllerDeps) {
	const watches = new Map<string, Watch>();

	/** Delivers the open batch, if any. */
	const flush = (watch: Watch) => {
		watch.cancelBatch?.();
		watch.cancelBatch = undefined;
		const batch = watch.batch;
		watch.batch = undefined;
		if (batch) deliver({ job: watch.job, kind: "events", batch });
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
		registry.stop(watch.job.id);
		deliver({ job: watch.job, kind: "stopped", reason });
	};
	const line = (id: string | undefined, text: string) => {
		const watch = id === undefined ? undefined : watches.get(id);
		// Nothing more from a monitor that ended, stopped or was let go.
		if (!watch || watch.job.status !== "running" || text.trim() === "") return;
		const at = now();
		watch.lines++;
		watch.job.events = watch.lines;
		watch.arrivals.push(at);
		while (watch.arrivals.length > 0 && at - watch.arrivals[0]! >= MONITOR_FLOOD_WINDOW_MS) watch.arrivals.shift();
		const opened = watch.batch === undefined;
		const batch = (watch.batch ??= { lines: [], omitted: 0 });
		if (batch.lines.length < MONITOR_LINES_PER_NOTICE) batch.lines.push(text);
		else batch.omitted++;
		if (watch.arrivals.length > MONITOR_FLOOD_LINES) return halt(watch, "flood");
		if (opened) watch.cancelBatch = schedule(() => flush(watch), MONITOR_BATCH_MS);
	};

	return {
		/** Starts a monitor; throws before anything runs when the timeout is out of range. */
		start({ timeoutSeconds, ...job }: MonitorStartRequest): JobRecord {
			const allowed = Number.isInteger(timeoutSeconds) && timeoutSeconds >= 1 && timeoutSeconds <= MONITOR_MAX_TIMEOUT_SECONDS;
			if (!allowed) {
				throw new Error(`timeout_seconds must be a whole number from 1 to ${MONITOR_MAX_TIMEOUT_SECONDS}.`);
			}
			let id: string | undefined;
			const started = registry.start({ ...job, kind: "monitor", onLine: (text) => line(id, text) });
			id = started.id;
			started.events = 0;
			started.timeoutSeconds = timeoutSeconds;
			const watch: Watch = { job: started, arrivals: [], lines: 0, cancelTimeout: schedule(() => halt(watch, "timeout"), timeoutSeconds * 1000) };
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
