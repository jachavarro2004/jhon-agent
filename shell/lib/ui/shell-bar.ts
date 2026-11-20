import { visibleWidth, wrapTextWithAnsi } from "@earendil-works/pi-tui";
import { asciiText } from "./primitives/glyphs.ts";
import { sanitizeTerminalText } from "./terminal-theme.ts";
import { PANEL_ROW_INDENT, renderPanelHeader, renderPanelRow, sectionGap } from "./shell-panel.ts";
import type { Density } from "./visual-customization-policy.ts";
import { changeCount, estadoLines, routeText, type LddStatus } from "./ldd-status.ts";
import { STATES } from "./primitives/states.ts";
import { formatTokens } from "./bars.ts";
import { shellSettings } from "../settings.ts";

// The rail's sections (LDD, JOBS, AGENTS), until the right pane replaces
// them (T174-S5). The top row and the status bar are lib/ui/bars.ts (S3).

/** What the rail's sections draw. */
export interface ShellBarModel {
	/** The LDD change the session works on (lib/ui/ldd-status.ts). */
	ldd?: LddStatus;
	jobs?: RunningJobDisplay[];
}

/** A running background task (a job or a sub-agent) as the rail shows it (lib/background-tasks-tools.ts). */
export interface RunningJobDisplay {
	id: string;
	/** What runs: a shell command, a monitor, or a sub-agent. */
	kind?: "command" | "monitor" | "agent";
	label: string;
	startedAt: number;
	/** A sub-agent's latest step: the tool it runs, or what it says. */
	step?: string;
	/** A sub-agent's tokens so far; 0 or absent while unknown. */
	tokens?: number;
	/** A finished job: when it ended, how, and its exit code. */
	endedAt?: number;
	status?: "exited" | "completed" | "failed" | "stopped";
	exitCode?: number | null;
}

export const JOB_GLYPH = "⧗";

/** G's compact jobs count, `⧗ N jobs`: the regular footer's while Status is hidden. */
export function shellJobsCount(count: number): string {
	return `${JOB_GLYPH} ${count} job${count === 1 ? "" : "s"}`;
}

export interface ShellBarTheme {
	fg(color: string, text: string): string;
	bold(text: string): string;
}

// Theme roles the rail paints with.
const ROLE = {
	LABEL: "muted",
	VALUE: "text",
} as const;

export function shellEnabled(env: NodeJS.ProcessEnv = process.env): boolean {
	if (env.JHON_PI_CHILD === "1") return false;
	const value = env.JHON_SHELL?.trim().toLowerCase();
	return !(value === "0" || value === "false" || value === "off");
}

// Extensions may paint their status themselves (pi-mcp-adapter does); the bar
// owns the palette, so their escapes go and the text takes the status role.
function sanitizeStatus(text: string): string {
	return sanitizeTerminalText(text.replace(/[\r\n\t]/g, " ")).replace(/ +/g, " ").trim();
}

// The rail's sections as the Gentle Shell draws them (its lib/shell-bar.ts
// and shell-panel.ts, T15.12d): each a panel section, a `▾ NAME … count`
// header and rows indented under it; the right pane (pane.ts) stacks them as
// they are, on one painted panel when tinted (shell.appearance.surface).

/** A rendered chrome row in its ASCII forms when shell.appearance.ascii is on (one cell each: the width holds). */
function chromeAscii(rows: string[], ascii = shellSettings().ascii()): string[] {
	return ascii ? rows.map(asciiText) : rows;
}

/** The rail's Status sections: LDD alone, which leads the rail (L174); T174 removed CHANGES and INTEGRATIONS (L1462). */
export const STATUS_SECTIONS = ["LDD"] as const;
export type StatusSection = (typeof STATUS_SECTIONS)[number];
/** What the pointer is over in Status: a section's header. */
export type StatusHover = StatusSection;
/** A click target in the Status sections, by row: a header folds its section. */
export type StatusTarget = { row: number; kind: "toggle"; section: StatusSection };
/** The Status sections' rows, their click targets, and each section's row range (one card each on a transparent rail). */
export interface StatusLayout {
	lines: string[];
	targets: StatusTarget[];
	groups: Array<{ start: number; end: number }>;
}

// Sidebar groups use structured fields, never positional compact-bar segments
// or inferred meanings from opaque extension status strings.
/**
 * The rail's Status: the LDD section, one block per change in progress, the
 * session's own first (the change detail, alt+j, shows the slices one by
 * one); its count is the route, or how many changes. No key on a header
 * (C6). Under a multiplexer too: it shows nothing of LDD (L112).
 */
export function renderShellStatusLayout(model: ShellBarModel, theme: ShellBarTheme, width: number, folded: ReadonlySet<string> = new Set(), hovered?: StatusHover, density?: Density): StatusLayout {
	const ascii = shellSettings().ascii();
	const value = (text: string) => theme.fg(ROLE.VALUE, theme.bold(text));
	const label = (text: string) => theme.fg(ROLE.LABEL, text);
	const inner = Math.max(1, width - PANEL_ROW_INDENT);
	const header = (title: StatusSection, count?: string) => renderPanelHeader(theme, width, { title, ...(count ? { count } : {}), open: !folded.has(title), hovered: hovered === title, ascii });
	const row = (left: string, right = "") => renderPanelRow(width, left, right);
	const count = changeCount(model.ldd);
	const ldd = estadoLines(model.ldd, { value, label, fg: (role: string, text: string) => theme.fg(role, text) }, inner, { routeShown: count !== 1 })
		.flatMap((line) => line === "" ? [""] : wrapTextWithAnsi(line, inner)).map((line) => row(line));
	const sections: Array<{ title: StatusSection; header: string; rows: string[] }> = [
		{ title: "LDD", header: header("LDD", model.ldd ? (count >= 2 ? `${count} changes` : routeText(model.ldd) || undefined) : undefined), rows: ldd },
	];
	const lines: string[] = [];
	const targets: StatusTarget[] = [];
	const groups: Array<{ start: number; end: number }> = [];
	sections.forEach((section, index) => {
		// G's: the blank row between sections is comfortable density's.
		if (index && sectionGap(density)) lines.push(" ".repeat(Math.max(0, width)));
		const start = lines.length;
		targets.push({ row: start, kind: "toggle", section: section.title });
		lines.push(section.header);
		if (!folded.has(section.title)) lines.push(...section.rows);
		groups.push({ start, end: lines.length });
	});
	return { lines: chromeAscii(lines, ascii), targets, groups };
}

/** The Status sections one after another, as the rail draws them on a tinted panel. */
export function renderShellStatusSections(model: ShellBarModel, theme: ShellBarTheme, width: number, folded: ReadonlySet<string> = new Set(), hovered?: StatusHover): string[] {
	return renderShellStatusLayout(model, theme, width, folded, hovered).lines;
}

/** How long a task has run, to the second while it is under an hour: «45s», «5m 12s», «1h 5m». */
export function elapsed(ms: number): string {
	const seconds = Math.floor(Math.max(0, ms) / 1000);
	if (seconds < 60) return `${seconds}s`;
	const minutes = Math.floor(seconds / 60);
	return minutes < 60 ? `${minutes}m ${seconds % 60}s` : `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

/** How long a finished job lingers in JOBS, as in Gentle Shell: a minute, the newest three. */
export const JOBS_FINISHED_TTL_MS = 60_000;
const FINISHED_SHOWN = 3;
// G's section roles (its jobs-widget.ts and agents-widget.ts): the glyph in
// its state's role, the name in text, the label and the time dim.
const NAME_ROLE = "text";
const KIND_ROLE = "muted";
const META_ROLE = "dim";
const ELLIPSIS = "…";
/** The view a tap on AGENTS opens, alt+s (G's /gentle:agents). */
const AGENTS_VIEW = "/jhon:agents";
/** AGENTS keeps at most this many rows, its overflow and summary rows included (G's SECTION_ROWS). */
const AGENTS_SECTION_ROWS = 8;
/** The widest a sub-agent's kind column grows (G's KIND_MAX). */
const AGENT_KIND_MAX = 10;

function railHeader(title: string, count: string, theme: ShellBarTheme, width: number, folded: boolean, hovered: boolean): string {
	return renderPanelHeader(theme, width, { title, count, open: !folded, hovered, ascii: shellSettings().ascii() });
}

/** A job's time, as G's formatJobDuration: «5s», «1m 15s», «1h 5m». */
function jobDuration(ms: number): string {
	const seconds = Math.max(0, Math.round(ms / 1000));
	if (seconds < 60) return `${seconds}s`;
	const minutes = Math.floor(seconds / 60);
	return minutes < 60 ? `${minutes}m ${seconds % 60}s` : `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

/** A job's mark in G's roles: ⧗ running in accent, ✓ a clean exit, ✗ a failure, – stopped. */
function jobMark(job: RunningJobDisplay): [string, string] {
	if (job.endedAt === undefined) return [JOB_GLYPH, "accent"];
	if (job.status === "stopped") return ["–", "dim"];
	return job.status === "failed" || (job.exitCode ?? 0) !== 0 ? ["✗", "error"] : ["✓", "success"];
}

/** The newest finished work of the last minute, three at most (G's widgetJobs and widgetTasks). */
function recentlyFinished(jobs: readonly RunningJobDisplay[], now: number): RunningJobDisplay[] {
	return jobs.filter((job) => job.endedAt !== undefined && now - job.endedAt < JOBS_FINISHED_TTL_MS)
		.sort((a, b) => b.endedAt! - a.endedAt!).slice(0, FINISHED_SHOWN);
}

/**
 * The rail's JOBS section as G's renderJobsSection: the session's background
 * commands and monitors, running newest first, then those finished in the
 * last minute; one row each, `<glyph> <id> <label>` and its time at the right
 * end; the count `N running · N finished`. Folded, the first job and
 * `… N more`. Nothing to show, no section.
 */
export function renderShellJobsSection(jobs: readonly RunningJobDisplay[] | undefined, theme: ShellBarTheme, width: number, now = Date.now(), folded = false, hovered = false): string[] {
	const commands = (jobs ?? []).filter((job) => (job.kind ?? "command") !== "agent");
	const running = commands.filter((job) => job.endedAt === undefined).sort((a, b) => b.startedAt - a.startedAt);
	const finished = recentlyFinished(commands, now);
	const shown = [...running, ...finished];
	if (!shown.length) return [];
	const rows = (folded ? shown.slice(0, 1) : shown).map((job) => {
		const [mark, role] = jobMark(job);
		return renderPanelRow(width, `${theme.fg(role, mark)} ${theme.fg(NAME_ROLE, sanitizeStatus(job.id))} ${theme.fg(META_ROLE, sanitizeStatus(job.label))}`, theme.fg(META_ROLE, jobDuration((job.endedAt ?? now) - job.startedAt)));
	});
	if (folded && shown.length > 1) rows.push(renderPanelRow(width, theme.fg(META_ROLE, `${ELLIPSIS} ${shown.length - 1} more`)));
	const counts = [running.length ? `${running.length} running` : "", finished.length ? `${finished.length} finished` : ""].filter(Boolean).join(" · ");
	return chromeAscii([railHeader("JOBS", counts, theme, width, folded, hovered), ...rows]);
}

type AgentState = "running" | "failed" | "done" | "cancelled";
// G's LOOK for the states a Jhon sub-agent reaches.
const AGENT_LOOK: Record<AgentState, { glyph: string; role: string }> = {
	running: { glyph: STATES.working.glyph, role: "accent" },
	failed: { glyph: "✗", role: "error" },
	done: { glyph: "✓", role: "success" },
	cancelled: { glyph: "–", role: "dim" },
};

function agentState(job: RunningJobDisplay): AgentState {
	if (job.endedAt === undefined) return "running";
	if (job.status === "stopped") return "cancelled";
	return job.status === "failed" || (job.exitCode ?? 0) !== 0 ? "failed" : "done";
}

/** A sub-agent's time, as G's formatElapsed: «45s», «1m05s», «1h05m». */
function agentElapsed(ms: number): string {
	const total = Math.max(0, Math.floor(ms / 1000));
	if (total < 60) return `${total}s`;
	const minutes = Math.floor(total / 60);
	if (minutes < 60) return `${minutes}m${String(total % 60).padStart(2, "0")}s`;
	return `${Math.floor(minutes / 60)}h${String(minutes % 60).padStart(2, "0")}m`;
}

// G's plain clip: truncateToWidth wraps its result in resets, which would end
// up inside a painted column.
function clipCells(text: string, cells: number): string {
	if (cells <= 0) return "";
	if (visibleWidth(text) <= cells) return text;
	let out = "";
	for (const char of text) {
		if (visibleWidth(out + char) > cells - 1) break;
		out += char;
	}
	return `${out}${ELLIPSIS}`;
}

/**
 * The rail's AGENTS section as G's renderAgentsSection: the running
 * sub-agents, then those that failed in the last minute, each in start order,
 * one row: `◐ <task>`, its kind in its own column, then its tokens and time
 * (`failed <time>` for a failure) at the right end; the done and the
 * cancelled fold into one row each, the first naming /jhon:agents. Past eight
 * rows the list keeps its first ones and `… N more · /jhon:agents`; folded,
 * the first row alone. Idle, as G's: the header and `none running ·
 * /jhon:agents` (L165).
 */
export function renderShellAgentsSection(jobs: readonly RunningJobDisplay[] | undefined, theme: ShellBarTheme, width: number, now = Date.now(), folded = false, hovered = false): string[] {
	const all = (jobs ?? []).filter((job) => job.kind === "agent");
	const shown = [...all.filter((job) => job.endedAt === undefined), ...recentlyFinished(all, now)].sort((a, b) => a.startedAt - b.startedAt);
	if (!shown.length) return chromeAscii([railHeader("AGENTS", "idle", theme, width, folded, hovered), renderPanelRow(width, theme.fg(META_ROLE, `no agent running · ${AGENTS_VIEW}`))]);
	const count = (state: AgentState) => shown.filter((job) => agentState(job) === state).length;
	const running = count("running");
	const header = railHeader("AGENTS", running ? `${running} running` : [count("failed") ? `${count("failed")} failed` : "", count("done") ? `${count("done")} done` : ""].filter(Boolean).join(" · ") || "idle", theme, width, folded, hovered);
	const ordered = (["running", "failed"] as const).flatMap((state) => shown.filter((job) => agentState(job) === state));
	const summaries = (["done", "cancelled"] as const).map((state) => [count(state), state] as const).filter(([n]) => n > 0)
		.map(([n, state], index) => renderPanelRow(width, `${theme.fg(AGENT_LOOK[state].role, AGENT_LOOK[state].glyph)} ${theme.fg(META_ROLE, `${n} ${state}${index === 0 ? ` · ${AGENTS_VIEW}` : ""}`)}`));
	const fits = ordered.length + summaries.length <= AGENTS_SECTION_ROWS;
	const listed = fits ? ordered : ordered.slice(0, Math.max(0, AGENTS_SECTION_ROWS - 1 - summaries.length));
	// A sub-agent's label is `<kind>: <task>` (lib/background-tasks.ts).
	const parts = listed.map((job) => {
		const label = sanitizeStatus(job.label);
		const split = label.indexOf(": ");
		const state = agentState(job);
		const time = agentElapsed((job.endedAt ?? now) - job.startedAt);
		return {
			state,
			kind: split > 0 ? label.slice(0, split) : "",
			task: split > 0 ? label.slice(split + 2) : label,
			right: state === "failed" ? `failed ${time}` : [job.tokens ? formatTokens(job.tokens) : "", time].filter(Boolean).join(" "),
		};
	});
	const kindWidth = Math.min(AGENT_KIND_MAX, Math.max(0, ...parts.map((part) => visibleWidth(part.kind))));
	const rightWidth = Math.max(0, ...parts.map((part) => visibleWidth(part.right)));
	const taskWidth = Math.max(1, width - PANEL_ROW_INDENT - 2 - 1 - kindWidth - 1 - rightWidth);
	const rows = parts.map(({ state, kind, task, right }) => {
		const look = AGENT_LOOK[state];
		const text = clipCells(task, taskWidth);
		const left = `${theme.fg(look.role, look.glyph)} ${theme.fg(state === "failed" ? "error" : NAME_ROLE, text)}${" ".repeat(taskWidth - visibleWidth(text))} ${theme.fg(KIND_ROLE, clipCells(kind, kindWidth))}`;
		return renderPanelRow(width, left, theme.fg(META_ROLE, right.padStart(rightWidth)));
	});
	if (!fits) rows.push(renderPanelRow(width, theme.fg(META_ROLE, `${ELLIPSIS} ${ordered.length - listed.length} more · ${AGENTS_VIEW}`)));
	const body = [...rows, ...summaries];
	return chromeAscii([header, ...(folded ? body.slice(0, 1) : body)]);
}

/**
 * The work row's digest of the sub-agents, as G's agentsSummary: `◐ N agents`
 * while any runs and `✗ N` for those that failed in the last minute;
 * undefined when none is live or failed.
 */
export function agentsWorkSummary(jobs: readonly RunningJobDisplay[] | undefined, theme: ShellBarTheme, now = Date.now()): { left: string; priority: number } | undefined {
	const all = (jobs ?? []).filter((job) => job.kind === "agent");
	const live = all.filter((job) => job.endedAt === undefined).length;
	const failed = recentlyFinished(all, now).filter((job) => agentState(job) === "failed").length;
	const parts = [
		live ? theme.fg("accent", `${AGENT_LOOK.running.glyph} ${live} ${live === 1 ? "agent" : "agents"}`) : "",
		failed ? theme.fg("error", `${AGENT_LOOK.failed.glyph} ${failed}`) : "",
	].filter(Boolean);
	return live || failed ? { left: parts.join(theme.fg(META_ROLE, " · ")), priority: 1 } : undefined;
}
