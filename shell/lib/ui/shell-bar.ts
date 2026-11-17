import { basename } from "node:path";
import { homedir } from "node:os";
import { truncateToWidth, visibleWidth, wrapTextWithAnsi } from "@earendil-works/pi-tui";
import { gaugeTone, paintGauge, renderGauge, type GaugeTone } from "./shell-gauge.ts";
import { METER_WARN } from "./primitives/meter.ts";
import { asciiText, glyph } from "./primitives/glyphs.ts";
import { selectUsageLimit, type ProviderUsage } from "./shell-usage.ts";
import { sanitizeTerminalText } from "./terminal-theme.ts";
import { paintSurfaceRow, type CardTheme } from "./shell-card.ts";
import { clipPanelText, fitPanelLine, PANEL_ROW_INDENT, panelSurfaceOpener, renderPanelHeader, renderPanelRow, sectionGap } from "./shell-panel.ts";
import type { Density } from "./visual-customization-policy.ts";
import { changeCount, estadoLines, routeText, type LddStatus } from "./ldd-status.ts";
import { STATES } from "./primitives/states.ts";
import { THEME_NAME } from "./primitives/paint.ts";
import { HOVER_ROLE } from "./shell-hover.ts";
import { shellSettings, type ShellSettings } from "../settings.ts";

export { gaugeTone, renderGauge, type GaugeTone };

// jhon-shell status bar: one line of segments that replaces pi's built-in
// three-line footer. Everything here is pure so the bar can be rendered and
// verified without a live TUI.

export interface ShellBarModel {
	jobs?: RunningJobDisplay[];
	/** The LDD change the session works on (lib/ui/ldd-status.ts). */
	ldd?: LddStatus;
	/** A multiplexer host shows the path (jhon:host showsPath, lib/host.ts): the title row names the project alone. */
	hostShowsPath?: boolean;
	/** The TODOS card shows the agent's task list, the slice's tasks live: STATUS leaves them to it. */
	planShown?: boolean;
	profile?: string;
	/**
	 * headOnly: `<dir>: <why>` for each root read against HEAD, whose commits are not counted (T132).
	 * failed: how many of the scanned roots Git could not read the status of.
	 */
	changes?: { files: number; added: number; deleted: number; notice?: string; headOnly?: readonly string[]; failed?: { roots: number; of: number } };
	cwd: string;
	branch: string | null;
	/** The `±N` beside the branch: the files Changes lists, committed or not (T132), not only a dirty working tree. */
	dirty: number | undefined;
	sessionName: string | undefined;
	modelId: string;
	effort: string | undefined;
	contextPercent: number | null;
	contextWindow: number;
	costTotal: number;
	subscription: boolean;
	usage: ProviderUsage | undefined;
	statuses: string[];
	/** Each status's extension key, in the statuses' order: the regular bar sorts its jobs count and mode badge among them. */
	statusKeys?: string[];
	/** The owner's permission mode for the project (`jhon status --json`'s mode, S31); a badge when it is not normal. */
	mode?: string;
}

/** A running background task (a job or a sub-agent) as the bar shows it (lib/background-tasks-tools.ts). */
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

/** G's compact jobs count, `⧗ N jobs`: the status bar's without a rail. */
export function shellJobsCount(count: number): string {
	return `${JOB_GLYPH} ${count} job${count === 1 ? "" : "s"}`;
}

/** A chrome bar's rows and the row its content sits on (G's ShellHeaderChrome). */
export interface ShellHeaderChrome {
	rows: string[];
	headerRow: number;
}

export interface ShellBarTheme {
	fg(color: string, text: string): string;
	bold(text: string): string;
}

// Theme roles the bar paints with. Keys are pi theme colors; the Jhon themes
// map them to the rose palette (accent = rose, syntaxFunction = powder blue).
const ROLE = {
	BRAND: "accent",
	SEPARATOR: "dim",
	PATH: "muted",
	BRANCH: "text",
	DIRTY: "warning",
	MODEL: "text",
	EFFORT: "syntaxFunction",
	LABEL: "muted",
	VALUE: "text",
	STATUS: "muted",
	SESSION: "dim",
} as const;

const RIGHT_PADDING = 2;

export function shellEnabled(env: NodeJS.ProcessEnv = process.env): boolean {
	if (env.JHON_PI_CHILD === "1") return false;
	const value = env.JHON_SHELL?.trim().toLowerCase();
	return !(value === "0" || value === "false" || value === "off");
}

export function formatTokens(count: number): string {
	if (count < 1000) return count.toString();
	if (count < 10_000) return `${(count / 1000).toFixed(1)}k`;
	if (count < 1_000_000) return `${Math.round(count / 1000)}k`;
	if (count < 10_000_000) return `${(count / 1_000_000).toFixed(1)}M`;
	return `${Math.round(count / 1_000_000)}M`;
}

export function formatCost(total: number, subscription = false): string {
	const amount = total >= 1 ? total.toFixed(2) : total.toFixed(3);
	return subscription ? `$${amount} sub` : `$${amount}`;
}

// Extensions may paint their status themselves (pi-mcp-adapter does); the bar
// owns the palette, so their escapes go and the text takes the status role.
function sanitizeStatus(text: string): string {
	return sanitizeTerminalText(text.replace(/[\r\n\t]/g, " ")).replace(/ +/g, " ").trim();
}

/** pi's thinking levels in the owner's words. */
export const EFFORT_WORDS: Record<string, string> = { off: "no reasoning", minimal: "minimal", low: "low", medium: "medium", high: "high", xhigh: "extra high", max: "max" };

/** The badge each mode other than normal wears (S31): YOLO in the warning role, SUPER YOLO in the bad one, bold. */
export const MODE_BADGES: Record<string, { text: string; role: string; bold?: boolean; detail: string }> = {
	yolo: { text: "YOLO", role: THEME_NAME[STATES.working.role], detail: "nothing asks but destroy" },
	"super-yolo": { text: "SUPER YOLO", role: THEME_NAME[STATES.failed.role], bold: true, detail: "nothing asks, destroy included" },
};

/** The owner's mode as the bars show it, or undefined for normal, none, or a mode the shell does not know. */
export function modeBadge(mode: string | undefined, theme: ShellBarTheme): string | undefined {
	const badge = mode ? MODE_BADGES[mode] : undefined;
	if (!badge) return undefined;
	const painted = theme.fg(badge.role, badge.text);
	return badge.bold ? theme.bold(painted) : painted;
}

function executionSegment(modelId: string, effort: string | undefined, theme: ShellBarTheme): string {
	return effort
		? `${theme.fg(ROLE.MODEL, modelId)} ${theme.fg(ROLE.LABEL, "·")} ${theme.fg(ROLE.EFFORT, EFFORT_WORDS[effort] ?? effort)}`
		: theme.fg(ROLE.MODEL, modelId);
}

/** pi's default free context below which it compacts (its compaction.reserveTokens). */
export const COMPACTION_RESERVE_TOKENS = 16384;
/** The gauge says where the session compacts from this many points before it. */
const COMPACTION_NOTE_BEFORE = 10;
/**
 * The share of the context window at which Jhon compacts the main session
 * (shell.behaviour.compact_at, 60 by default; 100, or JHON_SHELL_COMPACT_AT=off,
 * leaves it to pi alone): every turn re-reads the whole context, so a
 * lighter one answers sooner.
 */
export function compactAt(settings: ShellSettings = shellSettings()): number | undefined {
	const at = settings.compactAt();
	return at >= 100 ? undefined : at;
}

/** The share of a context window at which the session compacts: Jhon's, or pi's own if sooner; undefined for an unknown window. */
export function compactionPercent(contextWindow: number | undefined, at: number | null = compactAt() ?? null): number | undefined {
	if (!contextWindow || contextWindow <= COMPACTION_RESERVE_TOKENS) return undefined;
	const pis = Math.round((contextWindow - COMPACTION_RESERVE_TOKENS) / contextWindow * 100);
	return at === null ? pis : Math.min(at, pis);
}

function contextSegment(contextPercent: number | null, theme: ShellBarTheme, contextWindow?: number): string {
	const percentText = contextPercent === null ? "?%" : `${Math.round(contextPercent)}%`;
	const compacts = compactionPercent(contextWindow);
	const note = compacts !== undefined && contextPercent !== null && contextPercent >= compacts - COMPACTION_NOTE_BEFORE
		? ` ${theme.fg(USAGE_HINT_ROLE, `· compacts at ${compacts}%`)}` : "";
	return `${theme.fg(ROLE.LABEL, "ctx")} ${paintGauge(contextPercent, theme)} ${theme.fg(ROLE.VALUE, percentText)}${note}`;
}

function costSegment(costTotal: number, subscription: boolean, theme: ShellBarTheme): string {
	return theme.fg(ROLE.VALUE, formatCost(costTotal, subscription));
}

// The rail's sections as the Gentle Shell draws them (its lib/shell-bar.ts
// and shell-panel.ts, T15.12d): each a panel section, a `▾ NAME … count`
// header and rows indented under it; the rail (shell-sidebar-layout.ts)
// draws each one as its own card in the card style on a transparent surface,
// or on one painted panel when tinted (shell.appearance.surface).

/** A rendered chrome row in its ASCII forms when shell.appearance.ascii is on (one cell each: the width holds). */
function chromeAscii(rows: string[], ascii = shellSettings().ascii()): string[] {
	return ascii ? rows.map(asciiText) : rows;
}

/** The rail's Status sections, in order (L153 answer 4): LDD, then Changes and Integrations; no Stats. LDD leads the rail as its own part (L174). */
export const STATUS_SECTIONS = ["LDD", "CHANGES", "INTEGRATIONS"] as const;
export type StatusSection = (typeof STATUS_SECTIONS)[number];
/** What the pointer is over in Status: a section's header, or the CHANGES row that opens /jhon:changes. */
export type StatusHover = StatusSection | "open-changes";
/** A click target in the Status sections, by row: a header folds its section, the CHANGES row opens /jhon:changes. */
export type StatusTarget = { row: number; kind: "toggle"; section: StatusSection } | { row: number; kind: "open-changes" };
/** The Status sections' rows, their click targets, and each section's row range (one card each on a transparent rail). */
export interface StatusLayout {
	lines: string[];
	targets: StatusTarget[];
	groups: Array<{ start: number; end: number }>;
}

// Sidebar groups use structured fields, never positional compact-bar segments
// or inferred meanings from opaque extension status strings.
/**
 * The rail's Status as the Gentle Shell draws it: sections, never one STATUS
 * card, one blank row between them. LDD is one block per change in progress,
 * the session's own first (the change detail, alt+j, shows the slices one by
 * one); its count is the route, or how many changes. CHANGES counts the
 * files that differ from where the session started, committed or not (each
 * of its roots against its base, T132), with `+a −d` and `/jhon:changes`
 * (the row opens it), or `none` over `no changes since session start`, or,
 * while a root is read against HEAD, over `no uncommitted changes vs HEAD`
 * and that root's reason (the reason also under other roots' counts), or
 * `unknown` over `Git status could not be read` when a status failed;
 * INTEGRATIONS says what a mode other than normal means, then lists what
 * extensions report. No key on a header (C6). Under a multiplexer too: it shows
 * nothing of LDD (L112).
 */
export function renderShellStatusLayout(model: ShellBarModel, theme: ShellBarTheme, width: number, folded: ReadonlySet<string> = new Set(), hovered?: StatusHover, only: readonly StatusSection[] = STATUS_SECTIONS, density?: Density): StatusLayout {
	const ascii = shellSettings().ascii();
	const value = (text: string) => theme.fg(ROLE.VALUE, theme.bold(text));
	const label = (text: string) => theme.fg(ROLE.LABEL, text);
	const inner = Math.max(1, width - PANEL_ROW_INDENT);
	const header = (title: StatusSection, count?: string) => renderPanelHeader(theme, width, { title, ...(count ? { count } : {}), open: !folded.has(title), hovered: hovered === title, ascii });
	const row = (left: string, right = "") => renderPanelRow(width, left, right);
	const count = changeCount(model.ldd);
	const ldd = estadoLines(model.ldd, { value, label, fg: (role: string, text: string) => theme.fg(role, text) }, inner, { routeShown: count !== 1 })
		.flatMap((line) => line === "" ? [""] : wrapTextWithAnsi(line, inner)).map((line) => row(line));
	const changes = model.changes;
	// Nothing to list: "since session start" only when every root was read and
	// compared with its base. A status Git could not read is unknown, not no
	// changes. A root read against HEAD is named with its reason, beside other
	// roots' counts too: the count is not the session's total while its commits
	// are not counted.
	const headOnly = changes?.headOnly ?? [];
	const unread = !changes?.files && changes?.failed?.roots ? changes.failed : undefined;
	const changed = changes?.files ? `${theme.fg("success", `+${changes.added}`)} ${theme.fg("error", `−${changes.deleted}`)}`
		: label(unread ? "Git status could not be read" : headOnly.length ? "no uncommitted changes vs HEAD" : "no changes since session start");
	const headRows = [
		...(unread ? [`in ${unread.roots} of ${unread.of} ${unread.of === 1 ? "root" : "roots"}`] : []),
		...(changes?.files && headOnly.length ? ["commits not counted, read vs HEAD:"] : []),
		...headOnly.map((why) => sanitizeStatus(why)),
	].flatMap((text) => wrapTextWithAnsi(text, inner)).map((line) => row(label(line)));
	const integrationRows = (text: string) => wrapTextWithAnsi(text, Math.max(1, inner - 2))
		.map((line, index) => row(`${label(index === 0 ? "- " : "  ")}${line}`));
	// The status bar shows the mode's badge; INTEGRATIONS says what it means,
	// as the Gentle Shell's `permissions <detail>` row (T15.12c).
	const detail = model.mode ? MODE_BADGES[model.mode]?.detail : undefined;
	const permission = detail ? integrationRows(`${label("permissions")} ${theme.fg(ROLE.STATUS, detail)}`) : [];
	const reported = model.statuses.length + (permission.length ? 1 : 0);
	const command = theme.fg(hovered === "open-changes" ? HOVER_ROLE : ROLE.LABEL, "/jhon:changes");
	const all: Array<{ title: StatusSection; header: string; rows: string[]; openChangesRow?: number }> = [
		{ title: "LDD", header: header("LDD", model.ldd ? (count >= 2 ? `${count} changes` : routeText(model.ldd) || undefined) : undefined), rows: ldd },
		{ title: "CHANGES", header: header("CHANGES", changes?.files ? `${changes.files} ${changes.files === 1 ? "file" : "files"}` : unread ? "unknown" : "none"), rows: [row(changed, command), ...headRows], openChangesRow: 0 },
		{ title: "INTEGRATIONS", header: header("INTEGRATIONS", reported ? String(reported) : "none"), rows: [...permission, ...model.statuses.flatMap((status) => integrationRows(theme.fg(ROLE.STATUS, sanitizeStatus(status))))].concat(reported ? [] : [row(label("no status reported"))]) },
	];
	const sections = all.filter((section) => only.includes(section.title));
	const lines: string[] = [];
	const targets: StatusTarget[] = [];
	const groups: Array<{ start: number; end: number }> = [];
	sections.forEach((section, index) => {
		// G's: the blank row between sections is comfortable density's.
		if (index && sectionGap(density)) lines.push(" ".repeat(Math.max(0, width)));
		const start = lines.length;
		targets.push({ row: start, kind: "toggle", section: section.title });
		lines.push(section.header);
		if (!folded.has(section.title)) {
			if (section.openChangesRow !== undefined) targets.push({ row: lines.length + section.openChangesRow, kind: "open-changes" });
			lines.push(...section.rows);
		}
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
	if (!shown.length) return chromeAscii([railHeader("AGENTS", "idle", theme, width, folded, hovered), renderPanelRow(width, theme.fg(META_ROLE, `none running · ${AGENTS_VIEW}`))]);
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

const USAGE_HINT_ROLE = "dim";

// The split chrome as the Gentle Shell draws it (its lib/shell-bar.ts,
// T15.12d slice 6, L153 answer 3): a title row with the project identity
// above the transcript and the rail, and one status bar row below them. Both
// sit on the panel surface when it is tinted (shell.appearance.surface).
const STATUS_BAR_DIVIDER = "│";
const COMPACT_BRANCH_WIDTH = 15;
const WEEK_SECONDS = 604_800;
const FIVE_HOUR_SECONDS = 18_000;
/** The 5h window joins the status bar from the ladder's warn step on (S1). */
const FIVE_HOUR_FROM = METER_WARN;
const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"] as const;
const paintChromeRow = (theme: ShellBarTheme, row: string) => paintSurfaceRow(row, panelSurfaceOpener(theme as CardTheme));

/** `~/code/jhon`: the folder with the home directory as `~`. */
export function tildePath(path: string, home = homedir()): string {
	const trimmed = path.replace(/\/+$/, "") || path;
	return home && (trimmed === home || trimmed.startsWith(`${home}/`)) ? `~${trimmed.slice(home.length)}` : trimmed;
}

/** When a window resets, in the local week: «Thu 14:00». */
export function resetText(at: number): string {
	const d = new Date(at);
	return `${WEEKDAYS[d.getDay()]} ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

const TONE_RANK: Record<string, number> = { success: 0, warning: 1, error: 2 };
/** The week's state role: the one meter ladder, ok below 70%, warn from 70%, bad from 90% (S1). */
export function weekTone(percent: number): string {
	return gaugeTone(percent);
}

// The project's name and its path, sanitized: `力 name  ~/path`. Under a
// multiplexer host that shows each space's path, the name alone (L112).
function projectIdentity(model: Pick<ShellBarModel, "cwd" | "hostShowsPath">, theme: ShellBarTheme, home: string, ascii: boolean): { name: string; full: string } {
	const path = sanitizeStatus(tildePath(model.cwd, home));
	const name = sanitizeStatus(basename(model.cwd.replace(/\/+$/, "")) || model.cwd);
	// ASCII drops the brand: its `jhon` before the name would read `jhon jhon` (L165).
	const label = `${ascii ? "" : `${theme.fg(ROLE.BRAND, glyph("brand"))} `}${theme.fg(ROLE.VALUE, theme.bold(name))}`;
	return { name: label, full: model.hostShowsPath ? label : `${label}  ${theme.fg(ROLE.PATH, path)}` };
}

/** ` 力 name  path` left; the session name, or narrow the model · effort · profile, right. */
export function renderShellTitleRow(model: ShellBarModel, theme: ShellBarTheme, width: number, home = homedir(), options: { model?: boolean } = {}): string {
	const target = Math.max(0, Math.floor(width));
	const ascii = shellSettings().ascii();
	const identity = projectIdentity(model, theme, home, ascii);
	// Narrow: the status bar leaves the model out, so the title row carries it
	// with the agent profile; wide, the status bar carries both and this row keeps the session.
	const profileName = model.profile ? sanitizeStatus(model.profile) : "";
	const profile = profileName ? ` ${theme.fg(ROLE.SEPARATOR, "·")} ${theme.fg(ROLE.LABEL, profileName)}` : "";
	const right = options.model
		? `${executionSegment(model.modelId, model.effort, theme)}${profile}`
		: model.sessionName ? theme.fg(ROLE.VALUE, sanitizeStatus(model.sessionName)) : "";
	// As G's: the right side stays whole and the path is clipped, never dropped.
	return chromeAscii([paintChromeRow(theme, ` ${fitPanelLine(identity.full, right, Math.max(0, target - 2))} `)], ascii)[0]!;
}

function toneGauge(percent: number, role: string, theme: ShellBarTheme): string {
	const gauge = renderGauge(percent);
	const filled = gauge.replace(/▱+$/, "");
	return theme.fg(role, filled) + theme.fg("border", gauge.slice(filled.length));
}

// Jhon's subscription windows in G's usage slot (S3): the week (or a
// provider's own first window when it reports no week), and 5h only once it
// nears its limit (warn and up on the one ladder). The reset sits on the
// window with the highest tone, the 5h one winning a tie: it is the one about
// to bite. G's three shapes, narrowest last: every window with its gauge and
// reset, every window as text, the first window alone.
type UsageStage = "full" | "text" | "primary";
function usageSegments(model: ShellBarModel, theme: ShellBarTheme, stage: UsageStage): string[] {
	const limit = model.usage ? selectUsageLimit(model.usage, model.modelId) : undefined;
	if (!limit || limit.windows.length === 0) return [];
	const full = stage === "full";
	const fiveHour = limit.windows.find((window) => window.windowSeconds === FIVE_HOUR_SECONDS);
	const week = limit.windows.find((window) => window.windowSeconds === WEEK_SECONDS) ?? (fiveHour ? undefined : limit.windows[0]);
	const out: string[] = [];
	const shownFive = fiveHour && fiveHour.usedPercent >= FIVE_HOUR_FROM ? fiveHour : undefined;
	const rank = (percent: number) => TONE_RANK[weekTone(percent)] ?? 0;
	const resetOnFive = !!shownFive && (!week || rank(shownFive.usedPercent) >= rank(week.usedPercent));
	if (shownFive) {
		const role = weekTone(shownFive.usedPercent);
		out.push([
			theme.fg(role, `5h ${Math.round(shownFive.usedPercent)}%`),
			...(full && resetOnFive && shownFive.resetAt ? [theme.fg(USAGE_HINT_ROLE, `· resets ${resetText(shownFive.resetAt)}`)] : []),
		].join(" "));
	}
	if (week) {
		const role = weekTone(week.usedPercent);
		const label = week.windowSeconds === WEEK_SECONDS ? "week" : week.label || limit.name;
		out.push([
			theme.fg(ROLE.LABEL, label),
			...(full ? [toneGauge(week.usedPercent, role, theme)] : []),
			theme.fg(role, `${Math.round(week.usedPercent)}%`),
			...(full && !resetOnFive && week.resetAt ? [theme.fg(USAGE_HINT_ROLE, `· resets ${resetText(week.resetAt)}`)] : []),
		].join(" "));
	}
	return stage === "primary" ? out.slice(0, 1) : out;
}

export interface StatusBarExtras {
	/** Without a rail the running jobs keep their compact count here. */
	jobs?: boolean;
	/** False drops model · effort · profile (narrow: the title row carries them). */
	modelDetails?: boolean;
}

/**
 * G's status bar: the branch, its `±N` (the files the session changed, committed or not) and a mode other than normal
 * (Jhon's badge, S31) on the left, `⧗ N jobs` with them without a rail;
 * model · effort · profile, the context meter (Jhon's 70/90 ladder and its
 * compaction note), the cost and Jhon's usage windows on the right, `│`
 * between. Narrowing, G's cascade: at each usage shape (reset and gauges,
 * text, the first window, none) the profile gives way before the next shape,
 * then the model goes; a long branch shortens before them; the badge stays last.
 */
export function renderShellStatusBar(model: ShellBarModel, theme: ShellBarTheme, width: number, extras: StatusBarExtras = {}): ShellHeaderChrome {
	const bar = statusBar(model, theme, width, extras);
	return { ...bar, rows: chromeAscii(bar.rows) };
}

function statusBar(model: ShellBarModel, theme: ShellBarTheme, width: number, extras: StatusBarExtras): ShellHeaderChrome {
	const target = Math.max(0, Math.floor(width));
	if (target < 2) return { rows: [paintChromeRow(theme, " ".repeat(target))], headerRow: 0 };
	const room = target - 2;
	const divider = ` ${theme.fg(ROLE.SEPARATOR, STATUS_BAR_DIVIDER)} `;
	const join = (segments: string[]) => segments.filter(Boolean).join(divider);
	const dirty = model.dirty ? theme.fg(ROLE.DIRTY, `±${model.dirty}`) : "";
	const branchName = model.branch ? sanitizeStatus(model.branch) : "";
	const branch = [branchName ? theme.fg(ROLE.BRANCH, branchName) : "", dirty].filter(Boolean).join(" ");
	const mode = modeBadge(model.mode, theme) ?? "";
	// G's jobs are the background commands and monitors: sub-agents lead the work row instead.
	const runningJobs = extras.jobs ? (model.jobs?.filter((job) => job.endedAt === undefined && job.kind !== "agent").length ?? 0) : 0;
	const tail = join([mode, runningJobs ? theme.fg(ROLE.STATUS, shellJobsCount(runningJobs)) : ""]);
	const left = join([branch, tail]);
	const profileName = model.profile ? sanitizeStatus(model.profile) : "";
	const profile = profileName ? ` ${theme.fg(ROLE.LABEL, "·")} ${theme.fg(ROLE.LABEL, profileName)}` : "";
	const exec = extras.modelDetails === false ? "" : executionSegment(model.modelId, model.effort, theme);
	const counters = [contextSegment(model.contextPercent, theme, model.contextWindow), costSegment(model.costTotal, model.subscription, theme)];
	const stages: Array<UsageStage | undefined> = ["full", "text", "primary", undefined];
	const attempts = [
		...stages.flatMap((stage) => [{ stage, withExec: true, withProfile: true }, ...(profile ? [{ stage, withExec: true, withProfile: false }] : [])]),
		{ stage: undefined, withExec: false, withProfile: false },
	];
	for (const { stage, withExec, withProfile } of attempts) {
		const usage = stage ? join(usageSegments(model, theme, stage)) : "";
		const right = join([withExec ? exec + (exec && withProfile ? profile : "") : "", ...counters, usage]);
		let shownLeft = left;
		if (visibleWidth(shownLeft) + (right ? RIGHT_PADDING : 0) + visibleWidth(right) > room) {
			if (!branchName || visibleWidth(branchName) <= COMPACT_BRANCH_WIDTH) continue;
			// Reserve the dirty count and the badges before clipping the branch
			// itself; a recognizable prefix while usage shows, a shorter one after.
			const protectedWidth = visibleWidth(join([dirty, tail])) + (dirty ? 1 : tail ? visibleWidth(divider) : 0);
			const branchRoom = room - visibleWidth(right) - (right ? RIGHT_PADDING : 0) - protectedWidth;
			if (branchRoom < (stage ? COMPACT_BRANCH_WIDTH : 4)) continue;
			const shortened = theme.fg(ROLE.BRANCH, clipPanelText(branchName, branchRoom));
			shownLeft = join([[shortened, dirty].filter(Boolean).join(" "), tail]);
			if (visibleWidth(shownLeft) + (right ? RIGHT_PADDING : 0) + visibleWidth(right) > room) continue;
		}
		return { rows: [paintChromeRow(theme, ` ${shownLeft}${" ".repeat(room - visibleWidth(shownLeft) - visibleWidth(right))}${right} `)], headerRow: 0 };
	}
	// Still too wide: the branch gives way before context and cost, which give
	// way last, context after cost. Keep the badges when only they fit.
	const tailWidth = tail ? visibleWidth(tail) + (branch ? visibleWidth(divider) : 0) : 0;
	for (const right of [join(counters), counters[0] ?? "", ""]) {
		const leftRoom = room - visibleWidth(right) - (right ? RIGHT_PADDING : 0);
		const branchRoom = leftRoom - tailWidth;
		if (branch && branchRoom < 4 && right) continue;
		if (branch && branchRoom < 1 && !right && tail) return { rows: [paintChromeRow(theme, ` ${fitPanelLine(tail, "", room)} `)], headerRow: 0 };
		const fitted = join([branch ? clipPanelText(branch, Math.max(1, branchRoom)) : "", tail]);
		if (visibleWidth(fitted) > leftRoom && right) continue;
		return { rows: [paintChromeRow(theme, ` ${fitPanelLine(fitted, right, room)} `)], headerRow: 0 };
	}
	return { rows: [paintChromeRow(theme, ` ${fitPanelLine(left, "", room)} `)], headerRow: 0 };
}

/** One blank row on the chrome surface: the margin a chrome bar keeps toward the content (G's comfortable density). */
export function renderShellChromePad(theme: ShellBarTheme, width: number): string {
	return paintChromeRow(theme, " ".repeat(Math.max(0, Math.floor(width))));
}

/** The wide chrome bar: in comfortable density the status bar between two margin rows, as G's; compact and minimal draw the row alone. */
export function renderShellChromeBar(model: ShellBarModel, theme: ShellBarTheme, width: number, extras: StatusBarExtras = {}, density: Density = "comfortable"): ShellHeaderChrome {
	const bar = renderShellStatusBar(model, theme, width, extras);
	if (density !== "comfortable") return bar;
	const pad = renderShellChromePad(theme, width);
	return { ...bar, rows: [pad, ...bar.rows, pad], headerRow: bar.headerRow + 1 };
}

/** The wide title rows: in comfortable density the title row between two margin rows, as G's; compact and minimal draw it alone. */
export function renderShellTitleRows(model: ShellBarModel, theme: ShellBarTheme, width: number, home = homedir(), density: Density = "comfortable"): string[] {
	if (density !== "comfortable") return [renderShellTitleRow(model, theme, width, home)];
	const pad = renderShellChromePad(theme, width);
	return [pad, renderShellTitleRow(model, theme, width, home), pad];
}

// The regular (not fullscreen) terminal's one bar, as G's renderShellBar:
// every segment on one row, `⟡` between them, the session name at the right
// while it fits. G's jobs count and permission mode are extension statuses
// ("gentle-jobs", "gentle:mode") sorted by key with the others; Jhon's count
// and badge take those places as "jhon-jobs" and "jhon:mode".
const JOBS_STATUS_KEY = "jhon-jobs";
const MODE_STATUS_KEY = "jhon:mode";
export const SHELL_BAR_BRAND = `${glyph("brand")} jhon`;
export const SHELL_BAR_SEPARATOR = "⟡";

function compactBarSegments(model: ShellBarModel, theme: ShellBarTheme): string[] {
	const dirty = model.dirty ? ` ${theme.fg(ROLE.DIRTY, `±${model.dirty}`)}` : "";
	const location = model.branch ? `${theme.fg(ROLE.PATH, model.cwd)} ${theme.fg(ROLE.BRANCH, sanitizeStatus(model.branch))}${dirty}` : theme.fg(ROLE.PATH, model.cwd) + dirty;
	const usage = usageSegments(model, theme, "full");
	const jobs = model.jobs?.filter((job) => job.endedAt === undefined && job.kind !== "agent").length ?? 0;
	const mode = modeBadge(model.mode, theme);
	// A status without a known key keeps its place after Jhon's own.
	const statuses: Array<[string, string]> = [
		...(jobs ? [[JOBS_STATUS_KEY, theme.fg(ROLE.STATUS, shellJobsCount(jobs))] as [string, string]] : []),
		...(mode ? [[MODE_STATUS_KEY, mode] as [string, string]] : []),
		...model.statuses.map((status, index): [string, string] => [model.statusKeys?.[index] ?? "\uffff", theme.fg(ROLE.STATUS, sanitizeStatus(status))]),
	];
	return [
		theme.fg(ROLE.BRAND, shellSettings().ascii() ? glyph("brand", true) : SHELL_BAR_BRAND),
		location,
		executionSegment(model.modelId, model.effort, theme),
		contextSegment(model.contextPercent, theme, model.contextWindow),
		costSegment(model.costTotal, model.subscription, theme),
		...(usage.length ? [usage.join(" ")] : []),
		...statuses.map((entry, index) => [entry, index] as const).sort(([[a], i], [[b], j]) => a.localeCompare(b) || i - j).map(([[, segment]]) => segment),
	];
}

// When the line overflows, the location gives way first: the path shrinks to
// its last segment and a long branch is clipped, then segments go from the end.
export function renderShellBar(model: ShellBarModel, theme: ShellBarTheme, width: number): string[] {
	const join = (segments: string[]) => segments.join(` ${theme.fg(ROLE.SEPARATOR, SHELL_BAR_SEPARATOR)} `);
	let segments = compactBarSegments(model, theme);
	let left = join(segments);
	const right = model.sessionName ? theme.fg(ROLE.SESSION, sanitizeStatus(model.sessionName)) : undefined;
	if (right && visibleWidth(left) + RIGHT_PADDING + visibleWidth(right) <= width) {
		return chromeAscii([left + " ".repeat(width - visibleWidth(left) - visibleWidth(right)) + right]);
	}
	if (visibleWidth(left) > width) {
		const cwd = model.cwd.split("/").filter((part) => part.length > 0).pop() ?? model.cwd;
		const branch = model.branch && visibleWidth(model.branch) > COMPACT_BRANCH_WIDTH ? clipPanelText(model.branch, COMPACT_BRANCH_WIDTH) : model.branch;
		segments = compactBarSegments({ ...model, cwd, branch }, theme);
		left = join(segments);
	}
	while (segments.length > 1 && visibleWidth(left) > width) {
		segments.pop();
		left = join(segments);
	}
	return chromeAscii([truncateToWidth(left, width, "…")]);
}
