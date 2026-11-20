/**
 * shell/lib/ui/panel.ts — what the right pane shows (design/shell-tui 01, 02
 * and 04; T174-S5).
 *
 * The LDD section: the change, its route, the next step; its slices, the open
 * one's tasks under it; the last run; then the other worktrees' changes. With
 * no change, one dim line. Agents and Jobs are sections of their own, drawn
 * only while something runs: one row each, its state glyph, its name, what it
 * does and its elapsed time at the right edge. In the narrow form (04) the
 * same rows sit above the prompt as the Running block, under one labelled
 * rule. Every row is at most width cells; the pane host (pane.ts) places them.
 */
import { visibleWidth } from "@earendil-works/pi-tui";
import { clip, glyph, renderRule, spread, stateGlyph, STATES, themePainter, type Painter, type Role, type StateName, type ThemeLike } from "./primitives/index.ts";
import { sanitizeTerminalText } from "./terminal-theme.ts";
import type { LddStatus, Lane } from "./ldd-status.ts";
import { ago as shortAgo } from "./pane.ts";

/** A running background task (a job or a sub-agent) as lib/background-tasks-tools.ts reports it. */
export interface RunningJobDisplay {
	id: string;
	/** What runs: a shell command, a monitor, or a sub-agent. */
	kind?: "command" | "monitor" | "agent";
	/** A sub-agent's is `<agent>: <task>`; a job's, its command or its label. */
	label: string;
	startedAt: number;
	/** A sub-agent's latest step. */
	step?: string;
	/** A sub-agent's tokens so far. */
	tokens?: number;
	/** A finished task: when it ended, how, and its exit code. */
	endedAt?: number;
	status?: "exited" | "completed" | "failed" | "stopped";
	exitCode?: number | null;
}

/** How long a finished job is reported after it ends (lib/background-tasks-tools.ts). */
export const JOBS_FINISHED_TTL_MS = 60_000;

/** The shell draws its chrome unless JHON_SHELL turns it off; never in a jhon child session. */
export function shellEnabled(env: NodeJS.ProcessEnv = process.env): boolean {
	if (env.JHON_PI_CHILD === "1") return false;
	const value = env.JHON_SHELL?.trim().toLowerCase();
	return !(value === "0" || value === "false" || value === "off");
}

/** `⧗ N jobs`: the regular terminal's footer while Status is hidden. */
export function shellJobsCount(count: number): string {
	return `${glyph("job")} ${count} job${count === 1 ? "" : "s"}`;
}

/** `18s`, `1m03s`, `4m12s`, `1h02m`. */
export function elapsed(ms: number): string {
	const seconds = Math.floor(Math.max(0, ms) / 1000);
	if (seconds < 60) return `${seconds}s`;
	const minutes = Math.floor(seconds / 60);
	if (minutes < 60) return `${minutes}m${String(seconds % 60).padStart(2, "0")}s`;
	return `${Math.floor(minutes / 60)}h${String(minutes % 60).padStart(2, "0")}m`;
}

export interface PanelOptions {
	now: number;
	/** shell.appearance.ascii: every glyph in its one-cell form. */
	ascii?: boolean;
}

interface Run { text: string; role: Role; bold?: boolean }
const run = (text: string, role: Role, bold = false): Run => ({ text, role, bold });

/** Runs painted one after another. */
function painted(paint: Painter, runs: readonly Run[]): string {
	return runs.map(({ text, role, bold }) => {
		if (!text.trim()) return text;
		const coloured = paint.fg(role, text);
		return bold ? paint.bold(coloured) : coloured;
	}).join("");
}

const cells = (runs: readonly Run[]) => runs.reduce((sum, r) => sum + visibleWidth(r.text), 0);

/** `left`, a gap, then `right` ending at width; the left's last run is cut first. */
function row(paint: Painter, left: Run[], right: Run[], width: number, gap = 1): string {
	const rightCells = cells(right);
	const shownRight = right.length && rightCells + gap <= width ? right : [];
	const room = width - (shownRight.length ? rightCells + gap : 0);
	const kept = fitRuns(left, room);
	const pad = shownRight.length ? width - cells(kept) - rightCells : 0;
	return `${painted(paint, kept)}${" ".repeat(Math.max(0, pad))}${painted(paint, shownRight)}`;
}

/** Runs cut to width cells, the cut one ending in `…`. */
function fitRuns(runs: readonly Run[], width: number): Run[] {
	const out: Run[] = [];
	let used = 0;
	for (const r of runs) {
		const n = visibleWidth(r.text);
		if (used + n <= width) { out.push(r); used += n; continue; }
		const cut = clip(r.text, width - used);
		if (cut) out.push({ ...r, text: cut });
		break;
	}
	return out;
}

const clean = (text: string) => sanitizeTerminalText(text.replace(/[\r\n\t]/g, " ")).replace(/ +/g, " ").trim();
/** A slice's name without its «Slice N: » lead. */
const sliceName = (name: string) => name.replace(/^Slice \d+:\s*/i, "");
const stateRun = (state: StateName, ascii = false) => run(`${stateGlyph(state, ascii)} `, STATES[state].role);
/** The separator between facts: ` · `, ` - ` in ASCII. */
const dot = (ascii = false) => (ascii ? " - " : " · ");

/** The next step: the open slice's number after apply, and verify after it on the verified route. */
function nextText(ldd: LddStatus, ascii = false): string | undefined {
	if (!ldd.next) return undefined;
	const at = ldd.slices.indexOf("current");
	const step = ldd.next === "apply" && at >= 0 ? `apply slice ${at + 1}` : ldd.next;
	return ldd.next === "apply" && ldd.route === "verified" ? `${step} ${ascii ? "->" : "→"} verify` : step;
}

/** How long ago the run was: `6m ago`, `now`. */
function runAgo(at: string, now: number): string | undefined {
	const when = shortAgo(at, now);
	return when === undefined ? undefined : when === "now" ? "now" : `${when} ago`;
}

/** `11.8s`, `2m05s`. */
function runTime(seconds: number): string {
	return seconds < 60 ? `${Math.round(seconds * 10) / 10}s` : elapsed(seconds * 1000);
}

const LANES_SHOWN = 4;

/** The LDD section (01, 02): no change is one dim line. */
export function renderLddPanel(ldd: LddStatus | undefined, theme: ThemeLike, width: number, { now, ascii }: PanelOptions): string[] {
	const paint = themePainter(theme);
	if (!ldd) return [painted(paint, fitRuns([run("No active LDD change", "dim")], width))];
	const rows: string[] = [];
	const line = (left: Run[], right: Run[] = [], gap = 1) => rows.push(row(paint, left, right, width, gap));
	const sep = run(dot(ascii), "dim");
	const state = (name: StateName) => stateRun(name, ascii);
	const joined = (parts: Run[][]) => parts.flatMap((part, i) => (i ? [sep, ...part] : part));

	line([run(`${glyph("change", ascii)} `, "accent", true), run(clean(ldd.change), "ink", true)], ldd.worktree ? [run(`${glyph("branch", ascii)} ${clean(ldd.worktree)}`, "dim")] : []);
	const facts: Run[][] = [];
	if (ldd.route) facts.push([run(clean(ldd.route), "dim")]);
	if (ldd.strictTdd) facts.push([run("strict TDD", "dim")]);
	if (ldd.risk) facts.push([run(`risk ${clean(ldd.risk)}`, ldd.risk === "high" ? "bad" : ldd.risk === "medium" ? "warn" : "dim")]);
	if (facts.length) line(joined(facts));
	const next = nextText(ldd, ascii);
	if (next) line([run("next ", "dim"), run(clean(next), "ink", true)]);

	const n = ldd.slices.length;
	if (n) {
		rows.push("");
		const done = ldd.slices.filter((s) => s === "done").length;
		line([run("Slices", "soft", true), run(`  ${done}/${n}${ldd.tasks ? `${dot(ascii)}tasks ${ldd.tasks.completed}/${ldd.tasks.total}` : ""}`, "dim")]);
		ldd.slices.forEach((slice, i) => {
			const detail = ldd.detail?.[i];
			const open = slice === "current";
			const started = open && (detail?.completed ?? 0) > 0;
			const name = clip(`${i + 1} ${clean(sliceName(detail?.name || (open ? ldd.slice ?? "" : "")))}`.trimEnd(), width - 8);
			line([state(slice === "done" ? "done" : started ? "working" : "pending"), run(name, "ink", open)], detail && detail.total > 0 ? [run(`${detail.completed}/${detail.total}`, "dim")] : []);
			if (!open) return;
			let working = started;
			for (const t of detail?.tasks ?? []) {
				const tState: StateName = t.done ? "done" : working ? "working" : "pending";
				if (!t.done) working = false;
				line([run("  ", "ink"), state(tState), run(clip(clean(t.text), width - 4), t.done ? "dim" : "ink")]);
			}
		});
	}

	rows.push("");
	line([run("Last run", "soft", true)]);
	const t = ldd.tests;
	if (!t) line([state("pending"), run("none yet", "dim")]);
	else {
		const verdict = t.passed ? [state("done"), run("passed", "ok")] : [state("failed"), run(t.failed ? `${t.failed} failed` : "failed", "bad")];
		const when = runAgo(t.at, now);
		line([...verdict, ...[t.scope, when].filter((x): x is string => Boolean(x)).flatMap((x) => [sep, run(clean(x), "dim")])]);
		const facts = [t.count !== undefined ? `${t.count} tests` : "", t.seconds !== undefined ? runTime(t.seconds) : ""].filter(Boolean);
		const commit = t.commit ? clean(t.commit).slice(0, 7) : "";
		if (facts.length || commit) line([run(`  ${facts.join(dot(ascii))}${facts.length && commit ? dot(ascii) : ""}`, "dim"), ...(commit ? [run(commit, "soft")] : [])]);
	}

	const lanes = ldd.lanes ?? [];
	if (lanes.length) {
		rows.push("");
		line([run("Other changes", "soft", true), run(`  ${lanes.length}`, "dim")]);
		for (const lane of lanes.slice(0, LANES_SHOWN)) line(laneRun(lane, ascii), [run(lane.umbrella ? "umbrella" : lane.slices ? `${lane.done}/${lane.slices}` : "", "dim")]);
		if (lanes.length > LANES_SHOWN) line([run(`+${lanes.length - LANES_SHOWN} more`, "dim")]);
	}
	return rows;
}

function laneRun(lane: Lane, ascii = false): Run[] {
	return [run(`${lane.umbrella ? glyph("radioOff", ascii) : glyph("change", ascii)} `, "dim"), run(clean(lane.change), "ink"), run(`  ${glyph("branch", ascii)} ${clean(lane.worktree)}`, "dim")];
}

// ---------------------------------------------------------------- Agents and Jobs

/** The running sub-agents, oldest first. */
function runningAgents(jobs: readonly RunningJobDisplay[] | undefined): RunningJobDisplay[] {
	return (jobs ?? []).filter((job) => job.kind === "agent" && job.endedAt === undefined).sort((a, b) => a.startedAt - b.startedAt);
}

/** The running commands and monitors, oldest first. */
function runningJobs(jobs: readonly RunningJobDisplay[] | undefined): RunningJobDisplay[] {
	return (jobs ?? []).filter((job) => job.kind !== "agent" && job.endedAt === undefined).sort((a, b) => a.startedAt - b.startedAt);
}

/** A sub-agent's name and task from its label, `<agent>: <task>`. */
function agentParts(job: RunningJobDisplay): { name: string; what: string } {
	const label = clean(job.label);
	const split = label.indexOf(": ");
	return split > 0 ? { name: label.slice(0, split), what: label.slice(split + 2) } : { name: label, what: "" };
}

/** One row per running sub-agent: `◐ <name padded to the longest>  <what>` and its time at the right edge. */
function agentRows(paint: Painter, agents: readonly RunningJobDisplay[], width: number, now: number, ascii = false): string[] {
	const parts = agents.map(agentParts);
	// The longest name running sets the column (L1475), never more than half the row.
	const column = Math.min(Math.max(0, ...parts.map((p) => visibleWidth(p.name))), Math.max(1, Math.floor((width - 2) / 2)));
	return agents.map((job, i) => {
		const { name, what } = parts[i]!;
		const shown = clip(name, column);
		const left = [stateRun("working", ascii), run(shown, "ink"), run(" ".repeat(column - visibleWidth(shown) + 2), "ink"), run(what, "dim")];
		return row(paint, left, [run(elapsed(now - job.startedAt), "soft")], width, 2);
	});
}

/** One row per running job: `⧗ <command>` and its time at the right edge. */
function jobRows(paint: Painter, jobs: readonly RunningJobDisplay[], width: number, now: number, ascii = false): string[] {
	return jobs.map((job) => row(paint, [run(`${glyph("job", ascii)} `, "warn"), run(clean(job.label), "soft")], [run(elapsed(now - job.startedAt), "soft")], width, 2));
}

const header = (paint: Painter, title: string, count: number, width: number) => row(paint, [run(title, "soft", true), run(`  ${count} running`, "dim")], [], width);

/** 01's Agents section: nothing while no sub-agent runs. */
export function renderAgentsSection(jobs: readonly RunningJobDisplay[] | undefined, theme: ThemeLike, width: number, { now, ascii }: PanelOptions): string[] {
	const agents = runningAgents(jobs);
	if (!agents.length) return [];
	const paint = themePainter(theme);
	return [header(paint, "Agents", agents.length, width), ...agentRows(paint, agents, width, now, ascii)];
}

/** 01's Jobs section: nothing while no command or monitor runs. */
export function renderJobsSection(jobs: readonly RunningJobDisplay[] | undefined, theme: ThemeLike, width: number, { now, ascii }: PanelOptions): string[] {
	const list = runningJobs(jobs);
	if (!list.length) return [];
	const paint = themePainter(theme);
	return [header(paint, "Jobs", list.length, width), ...jobRows(paint, list, width, now, ascii)];
}

/** What a click on a row opens: the agents view or the jobs view. */
export type RunningView = "agents" | "jobs";

/** The widest the Running block's rows run (04: 64 columns, two in from the edge). */
const RUNNING_ROWS_MAX = 64;

/**
 * 04's Running block, above the prompt: `── Running  2 agents · 1 job ───`,
 * one row per agent and job two columns in, then a blank row; each row's view
 * in opens (the rule opens the agents view while any runs). Nothing runs, no
 * block.
 */
export function renderRunningBlock(jobs: readonly RunningJobDisplay[] | undefined, theme: ThemeLike, width: number, { now, ascii }: PanelOptions): { rows: string[]; opens: Array<RunningView | undefined> } {
	const agents = runningAgents(jobs);
	const list = runningJobs(jobs);
	if (!agents.length && !list.length) return { rows: [], opens: [] };
	const paint = themePainter(theme);
	const total = Math.max(0, Math.floor(width));
	const inner = Math.max(1, Math.min(RUNNING_ROWS_MAX, total - 4));
	const counts = [agents.length ? `${agents.length} agent${agents.length === 1 ? "" : "s"}` : "", list.length ? `${list.length} job${list.length === 1 ? "" : "s"}` : ""].filter(Boolean).join(dot(ascii));
	const indent = (line: string) => `  ${line}${" ".repeat(Math.max(0, total - 2 - visibleWidth(line)))}`;
	const rows = [
		renderRule({ width: total, label: "Running", note: counts, paint, ascii }),
		...agentRows(paint, agents, inner, now, ascii).map(indent),
		...jobRows(paint, list, inner, now, ascii).map(indent),
		" ".repeat(total),
	];
	const opens: Array<RunningView | undefined> = [agents.length ? "agents" : "jobs", ...agents.map(() => "agents" as const), ...list.map(() => "jobs" as const), undefined];
	return { rows, opens };
}
