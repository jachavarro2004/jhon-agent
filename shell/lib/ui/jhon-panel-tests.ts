/**
 * shell/lib/ui/jhon-panel-tests.ts — the change detail's test run (t):
 * `jhon executions run <change> --affected <base>` for the session's change,
 * in the background, its time against the plan's estimate and its end.
 *
 * The run is the owner's: BACKGROUND shows it beside the agent's work,
 * but its end comes back here, never as a turn of the agent's, and the
 * agent's task tools never see it. It runs through jhon executions, so its
 * record is the one `jhon deliver check` reads.
 */
import { STATES } from "./primitives/states.ts";
import { THEME_NAME } from "./primitives/paint.ts";

/** One project of the dry run's `projects`. */
export interface ProjectPlan {
	project: string;
	/** testmon, own, related, whole, none, or "" (no tests). */
	tests: string;
	modules?: number;
	estimate?: { tests: number; total: number; seconds: number; all_seconds: number };
	last_seconds?: number;
	checks: string[];
	reused?: boolean;
}

export interface TestsData {
	change?: string;
	base?: string;
	/** jhon status's tests: the last recorded run. */
	last?: { timestamp?: string; passed?: boolean; failed?: string[]; scope?: string; clean?: boolean; complete?: boolean };
	projects?: ProjectPlan[];
	timeoutMinutes?: number;
	/** Where the change is, and its worktree's name when it is not pi's folder. */
	root?: string;
	worktree?: string;
}

export interface TestsPaint {
	fg(role: string, text: string): string;
	bold(text: string): string;
}

export interface TestsDeps {
	load(): Promise<TestsData>;
	/** Start the run in the background; onEnd gets its exit code (null when stopped). */
	start(data: TestsData, onEnd: (exitCode: number | null) => void): { id: string; startedAt: number };
	stop(id: string): void;
	requestRender(): void;
	now?(): number;
}

/** Seconds as the owner reads a duration: «45 s», «3 min», «1 h 5 min». */
export function duration(seconds: number): string {
	if (seconds < 60) return `${Math.max(1, Math.round(seconds))} s`;
	const m = Math.round(seconds / 60);
	return m < 60 ? `${m} min` : m % 60 ? `${Math.floor(m / 60)} h ${m % 60} min` : `${m / 60} h`;
}

function elapsed(ms: number): string {
	const s = Math.max(0, Math.floor(ms / 1000));
	return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

/**
 * A recorded run in the one vocabulary every surface speaks (S29): «✓ passed»,
 * «✗ N failed», or «✗ failed» when the record names no project. The record
 * holds no count of tests, so «passed» carries none.
 */
export function runVerdict(t: { passed?: boolean; failed?: string[] }, paint: TestsPaint): string {
	if (t.passed) return paint.fg(THEME_NAME[STATES.done.role], `${STATES.done.glyph} passed`);
	return paint.fg(THEME_NAME[STATES.failed.role], `${STATES.failed.glyph} ${t.failed?.length ? `${t.failed.length} failed` : "failed"}`);
}

/** How long a project's tests should take, by the estimate or the last run. */
function expected(p: ProjectPlan): number | undefined {
	return p.estimate?.seconds ?? p.last_seconds;
}

export class TestsCard {
	private data: TestsData | undefined;
	private error: string | undefined;
	busy: string | undefined;
	loaded = false;
	private run: { id: string; startedAt: number } | undefined;
	private note: { text: string; ok: boolean } | undefined;
	private ticker: ReturnType<typeof setInterval> | undefined;
	/** A run asked for before the plan was read: it starts once the plan is. */
	private pending = false;
	private readonly deps: TestsDeps;

	constructor(deps: TestsDeps) {
		this.deps = deps;
	}

	private now(): number {
		return this.deps.now?.() ?? Date.now();
	}

	reload(): void {
		this.busy = "loading…";
		this.deps.requestRender();
		const settle = (data?: TestsData, error?: string) => {
			this.busy = undefined;
			this.loaded = true;
			if (data) [this.data, this.error] = [data, undefined];
			else this.error = error;
			if (this.pending) {
				this.pending = false;
				if (this.runnable()) this.start();
			}
			this.deps.requestRender();
		};
		try {
			this.deps.load().then((d) => settle(d), (e) => settle(undefined, e instanceof Error ? e.message : String(e)));
		} catch (e) {
			settle(undefined, e instanceof Error ? e.message : String(e));
		}
	}

	private runnable(): boolean {
		return !this.run && !!this.data?.change && !!this.data.projects?.length;
	}

	/** Run the change's tests (the detail's t): now when the plan is read, else as soon as it is. */
	runWhenReady(): void {
		if (this.run) return;
		if (this.loaded && !this.busy) {
			if (this.runnable()) this.start();
			this.deps.requestRender();
			return;
		}
		this.pending = true;
		if (!this.busy) this.reload();
	}

	/** s stops the run; whether the key was the card's. */
	handleInput(data: string): boolean {
		if (data !== "s" || !this.run) return false;
		this.stop();
		this.deps.requestRender();
		return true;
	}

	private start(): void {
		const data = this.data!;
		this.note = undefined;
		try {
			this.run = this.deps.start(data, (code) => {
				this.run = undefined;
				this.stopTicker();
				this.note = code === null ? { text: "Run stopped: its record is partial.", ok: false }
					: code === 0 ? { text: `Finished ${STATES.done.glyph} passed: the record is the one jhon deliver check reads.`, ok: true }
					: { text: `Finished with failures (code ${code}): the last run says which.`, ok: false };
				this.reload();
			});
		} catch (e) {
			this.note = { text: `Did not start: ${e instanceof Error ? e.message : String(e)}`, ok: false };
			return;
		}
		// The elapsed time moves every second while the run lasts.
		this.ticker = setInterval(() => this.deps.requestRender(), 1000);
		this.ticker.unref?.();
	}

	private stop(): void {
		if (!this.run) return;
		this.deps.stop(this.run.id);
	}

	private stopTicker(): void {
		if (this.ticker) clearInterval(this.ticker);
		this.ticker = undefined;
	}

	dispose(): void {
		this.stopTicker();
	}

	/** Whether a run is going: s stops it. */
	running(): boolean {
		return !!this.run;
	}

	/** The owner's run as the change detail shows it: reading the plan, running with its time, or how it ended; nothing before t. */
	runLines(paint: TestsPaint): string[] {
		if (this.pending || (this.busy && !this.run && !this.note)) return [paint.fg("dim", "Reading the test plan…")];
		if (this.run) {
			const longest = Math.max(0, ...(this.data?.projects ?? []).map((p) => expected(p) ?? 0));
			return [`${paint.fg(THEME_NAME[STATES.working.role], `${STATES.working.glyph} running`)} ${paint.bold(elapsed(this.now() - this.run.startedAt))}${longest ? paint.fg("dim", ` of ~${duration(longest)}`) : ""} ${paint.fg("dim", "· also in BACKGROUND")}`];
		}
		if (this.note) return [paint.fg(this.note.ok ? "success" : "error", this.note.text)];
		if (this.loaded && !this.runnable()) return [paint.fg("dim", this.error ? `Could not read the test plan: ${this.error}` : "Nothing to run: jhon executions finds no tests for the change.")];
		return [];
	}
}
