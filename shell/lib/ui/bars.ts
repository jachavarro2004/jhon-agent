/**
 * shell/lib/ui/bars.ts — the shell's two bars (design/shell-tui 01 and 04,
 * T174-S3).
 *
 * The top row says where the session is: `力 repo  ~/path`, nothing more.
 * The status bar at the bottom says how it runs: the branch with `±N` (the
 * files the session changed), the sandbox, the owner's mode and what other
 * extensions report (an MCP adapter's servers) on the left; model · effort,
 * the context, the subscription's windows (the week, the 5h one from 70%;
 * T174-S5b) and the cost on the right, the most volatile last, thin dividers
 * between.
 *
 * Under NARROW_BELOW columns the status bar takes the narrow form (04): no
 * meter, the branch capped at NARROW_BRANCH_CELLS. When a row still does not
 * fit, the branch shortens first (to BRANCH_MIN_CELLS), then the pieces in
 * DROP_ORDER give way one at a time, why the sandbox is not running and the
 * other extensions' statuses first; the profile before the windows' meters
 * and resets (as before T174), the windows before model · effort, the cost
 * is the last thing left.
 *
 * Both renders are pure: data, a theme and a width in, one row of exactly
 * that width out.
 */
import { basename } from "node:path";
import { homedir } from "node:os";
import { visibleWidth } from "@earendil-works/pi-tui";
import { clip, glyph, METER_CELLS, meterTone, paintSurface, renderMeter, themePainter, type MeterTone, type Painter, type Role, type ThemeLike } from "./primitives/index.ts";
import { sanitizeTerminalText } from "./terminal-theme.ts";
import { panelSurfaceOpener } from "./shell-panel.ts";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { shellSettings, type ShellSettings } from "../settings.ts";
import { resetDay, type BarWindow } from "./usage.ts";

/** What the two bars show. */
export interface BarModel {
	/** The session's folder, absolute. */
	cwd: string;
	/** A multiplexer host shows the path itself (lib/host.ts): the top row names the project alone (L112). */
	hostShowsPath?: boolean;
	branch: string | null;
	/** The `±N` beside the branch: the files the session's own writes changed (session-changes.ts). */
	dirty?: number;
	/** What other extensions report on pi's status line (ctx.ui.setStatus), in their keys' order. */
	statuses?: string[];
	/** What the session's pin means for the commands (lib/sandbox.ts sandboxShown); unknown until the pin is read. */
	sandbox?: "on" | "off" | "not running";
	/** Why the sandbox is not running, in a few words. */
	sandboxWhy?: string;
	/** The owner's permission mode (`jhon status --json`, S31): a badge unless normal. */
	mode?: string;
	modelId: string;
	effort?: string;
	/** The active Jhon profile, when not the default (T120). */
	profile?: string;
	/** The context used, in percent (null when pi cannot say), and the window's size in tokens. */
	contextPercent: number | null; contextWindow?: number;
	costTotal: number;
	/** The model is paid by subscription: the cost reads `sub`. */
	subscription?: boolean;
	/** The subscription's windows to show (usage.ts barWindows), the 5h first. */
	usage?: BarWindow[];
}

export interface BarOptions {
	/** ASCII forms (shell.appearance.ascii); the setting by default. */
	ascii?: boolean;
	/** The home folder the top row writes as `~`. */
	home?: string;
	/** The narrow form; by default under NARROW_BELOW columns. */
	narrow?: boolean;
	/** Where Jhon compacts the session, percent, or null for pi's own; the setting by default. */
	compactAt?: number | null;
}

/** The status bar takes its narrow form under this many columns (design/system.md: the rail breakpoint). */
export const NARROW_BELOW = 140;
/** The narrow form's longest branch, ellipsis included (04: `feat/add-invoice-…`). */
export const NARROW_BRANCH_CELLS = 18;
/** A branch is never shortened below this; with less room it goes. */
export const BRANCH_MIN_CELLS = 8;

// ---------------------------------------------------------------- text

/** A piece of text in one role. */
interface Run { role: Role; text: string; bold?: boolean }

const width = (runs: readonly Run[]) => runs.reduce((sum, run) => sum + visibleWidth(run.text), 0);

function paintRuns(runs: readonly Run[], paint: Painter): string {
	return runs.map((run) => {
		// Blank runs keep no colour: only text carries a role.
		if (!run.text.trim()) return run.text;
		const painted = paint.fg(run.role, run.text);
		return run.bold ? paint.bold(painted) : painted;
	}).join("");
}

/** Text the bars may print: no escapes or control characters, whitespace as single spaces. */
function clean(text: string): string {
	return sanitizeTerminalText(text.replace(/\s+/g, " ")).trim();
}

/** The row on the band when the chrome is tinted (shell.appearance.surface). */
function onBand(row: string, theme: ThemeLike): string {
	return paintSurface(row, panelSurfaceOpener(theme));
}

// ---------------------------------------------------------------- words

/** pi's thinking levels in the owner's words. */
export const EFFORT_WORDS: Record<string, string> = { off: "no reasoning", minimal: "minimal", low: "low", medium: "medium", high: "high", xhigh: "extra high", max: "max" };

/** The badge of each mode other than normal (S31): YOLO a warning, SUPER YOLO a danger, both bold. */
export const MODE_BADGES: Record<string, { text: string; role: Role; detail: string }> = {
	yolo: { text: "YOLO", role: "warn", detail: "nothing asks but destroy" },
	"super-yolo": { text: "SUPER YOLO", role: "bad", detail: "nothing asks, destroy included" },
};

/** `$1.42`, three decimals under a dollar; `sub` when a subscription pays. */
export const formatCost = (total: number, subscription = false): string => `$${total.toFixed(total < 1 ? 3 : 2)}${subscription ? " sub" : ""}`;

/** `950`, `4.2k`, `272k`, `2.6M`, `13M`. */
export function formatTokens(count: number): string {
	const short = (value: number, unit: string) => `${value < 10 ? value.toFixed(1) : Math.round(value)}${unit}`;
	if (count < 1000) return String(count);
	if (count < 1_000_000) return short(count / 1000, "k");
	return short(count / 1_000_000, "M");
}

/** `~/code/jhon`: the path with the home folder as `~`. */
export function tildePath(path: string, home = homedir()): string {
	const trimmed = path.length > 1 ? path.replace(/\/+$/, "") : path;
	if (!home) return trimmed;
	if (trimmed === home) return "~";
	return trimmed.startsWith(`${home}/`) ? `~${trimmed.slice(home.length)}` : trimmed;
}

// ---------------------------------------------------------------- compaction

/** pi compacts when this much of the window is left (its compaction.reserveTokens default). */
export const COMPACTION_RESERVE_TOKENS = 16384;
/** The ctx segment names the compaction point from this many points before it. */
const NOTE_FROM = 10;

/**
 * Where Jhon compacts the main session, percent of the window
 * (shell.behaviour.compact_at, 60 by default): undefined at 100 or more,
 * which leaves it to pi alone. Each turn re-reads the whole context, so a
 * lighter one answers sooner.
 */
export function compactAt(settings: ShellSettings = shellSettings()): number | undefined {
	const at = settings.compactAt();
	return at < 100 ? at : undefined;
}

/** The percent of a window at which the session compacts: Jhon's point, or pi's when that comes first; undefined for an unknown window. */
export function compactionPercent(contextWindow: number | undefined, at: number | null = compactAt() ?? null): number | undefined {
	if (!contextWindow || contextWindow <= COMPACTION_RESERVE_TOKENS) return undefined;
	const pis = Math.round(100 * (1 - COMPACTION_RESERVE_TOKENS / contextWindow));
	return at === null ? pis : Math.min(pis, at);
}

// ---------------------------------------------------------------- the top row

/** ` 力 acme_app  ~/acme_app`: the project's name and path, exactly width cells; the path is clipped first, then the name. */
export function renderTopRow(model: Pick<BarModel, "cwd" | "hostShowsPath">, theme: ThemeLike, cells: number, options: BarOptions = {}): string {
	const total = Math.max(0, Math.floor(cells));
	const ascii = options.ascii ?? shellSettings().ascii();
	const paint = themePainter(theme);
	const room = Math.max(0, total - 1);
	const folder = model.cwd.length > 1 ? model.cwd.replace(/\/+$/, "") : model.cwd;
	// ASCII has no mark: the brand's ASCII form, `jhon`, before the name would read as a second name (L165).
	const mark: Run[] = ascii ? [] : [{ role: "accent", text: glyph("brand"), bold: true }, { role: "ink", text: " " }];
	const name = clean(basename(folder) || folder);
	const path = model.hostShowsPath ? "" : clean(tildePath(folder, options.home ?? homedir()));
	const fixed = width(mark);
	let runs: Run[];
	const pathRoom = room - fixed - visibleWidth(name) - 2;
	if (path && pathRoom >= Math.min(2, visibleWidth(path))) {
		runs = [...mark, { role: "ink", text: name, bold: true }, { role: "ink", text: "  " }, { role: "dim", text: clip(path, pathRoom, { ascii }) }];
	} else if (fixed + visibleWidth(name) <= room) {
		runs = [...mark, { role: "ink", text: name, bold: true }];
	} else {
		runs = [...mark, { role: "ink", text: clip(name, room - fixed, { ascii }), bold: true }];
		if (room < fixed + 1) runs = [{ role: "ink", text: clip(name, room, { ascii }), bold: true }];
	}
	if (total === 0) return "";
	const content = paintRuns(runs, paint);
	return onBand(` ${content}${" ".repeat(Math.max(0, room - width(runs)))}`, theme);
}

// ---------------------------------------------------------------- the status bar

/**
 * What gives way when the bar does not fit, first to last; the cost is never
 * in it. Of the subscription's windows the reset goes first, then the week's
 * meter, then the window without the reset, then the windows.
 */
export const DROP_ORDER = ["why", "statuses", "profile", "resets", "usageMeter", "meter", "note", "window", "usage", "exec", "sandbox", "branch", "ctx", "mode"] as const;
type Droppable = (typeof DROP_ORDER)[number];

const METER_ROLE: Record<MeterTone, Role> = { success: "ok", warning: "warn", error: "bad", dim: "dim" };
/** Cells kept free between the left group and the right one. */
const GAP = 2;

/** The status bar: exactly width cells. */
export function renderStatusBar(model: BarModel, theme: ThemeLike, cells: number, options: BarOptions = {}): string {
	const total = Math.max(0, Math.floor(cells));
	const ascii = options.ascii ?? shellSettings().ascii();
	const narrow = options.narrow ?? total < NARROW_BELOW;
	const paint = themePainter(theme);
	const divider: Run = { role: "faint", text: ` ${glyph("side", ascii)} ` };
	const dot = (role: Role = "dim"): Run => ({ role, text: ascii ? " - " : " · " });

	const branch = model.branch ? clean(model.branch) : "";
	const badge = model.mode ? MODE_BADGES[model.mode] : undefined;
	const profile = model.profile ? clean(model.profile) : "";
	const percent = model.contextPercent === null ? null : Math.round(model.contextPercent);
	const compacts = compactionPercent(model.contextWindow, options.compactAt === undefined ? compactAt() ?? null : options.compactAt);
	const note = compacts !== undefined && percent !== null && percent >= compacts - NOTE_FROM;
	const effort = model.effort ? EFFORT_WORDS[model.effort] ?? clean(model.effort) : "";
	// The `±N` stays with the branch: it is cut from the name, never dropped alone.
	const dirty = model.dirty ? `±${model.dirty}` : "";
	// Another extension may paint its status itself: the bar owns the palette, its escapes go.
	const statuses = (model.statuses ?? []).map(clean).filter(Boolean);

	// One layout with some pieces dropped and the branch at most `branchCells` wide.
	const layout = (dropped: ReadonlySet<Droppable>, branchCells: number): { left: Run[][]; right: Run[][] } => {
		const shown = (piece: Droppable) => !dropped.has(piece);
		const left: Run[][] = [];
		if (branch && shown("branch")) left.push([{ role: "accent", text: glyph("branch", ascii) }, { role: "ink", text: " " }, { role: "ink", text: clip(branch, branchCells, { ascii }) }, ...(dirty ? [{ role: "warn" as const, text: ` ${dirty}` }] : [])]);
		if (model.sandbox && shown("sandbox")) {
			const state: Run = model.sandbox === "on" ? { role: "ok", text: "on" } : model.sandbox === "off" ? { role: "warn", text: "off" } : { role: "bad", text: "not running" };
			const why = model.sandbox === "not running" && model.sandboxWhy && shown("why") ? [{ role: "dim" as const, text: `${dot().text}${clean(model.sandboxWhy)}` }] : [];
			left.push([{ role: "dim", text: "sandbox" }, { role: "ink", text: " " }, state, ...why]);
		}
		if (badge && shown("mode")) left.push([{ role: badge.role, text: badge.text, bold: true }]);
		if (statuses.length && shown("statuses")) left.push(statuses.flatMap((status, i) => [...(i ? [dot()] : []), { role: "soft" as const, text: status }]));
		const right: Run[][] = [];
		if (shown("exec")) {
			const exec: Run[] = [{ role: "ink", text: clean(model.modelId) }];
			if (effort) exec.push(dot(), { role: "soft", text: effort });
			if (profile && shown("profile")) exec.push(dot(), { role: "soft", text: profile });
			right.push(exec);
		}
		if (shown("ctx")) {
			const ctx: Run[] = [{ role: "dim", text: "ctx" }, { role: "ink", text: " " }];
			if (!narrow && shown("meter")) ctx.push({ role: METER_ROLE[meterTone(percent)], text: renderMeter(percent, undefined, ascii) }, { role: "ink", text: " " });
			ctx.push({ role: "ink", text: percent === null ? "?%" : `${percent}%` });
			if (note && shown("note")) ctx.push({ role: "dim", text: `${dot().text}compacts at ${compacts}%` });
			right.push(ctx);
		}
		if (shown("usage")) {
			const windows = shown("window") || (model.usage?.length ?? 0) < 2 ? model.usage ?? [] : (model.usage ?? []).filter((w) => w.reset).slice(0, 1);
			for (const w of windows.length ? windows : (model.usage ?? []).slice(0, 1)) {
				const tone = METER_ROLE[meterTone(w.percent)];
				const group: Run[] = [{ role: "dim", text: clean(w.label) }, { role: "ink", text: " " }];
				if (w.meter && !narrow && shown("usageMeter")) {
					const meter = renderMeter(w.percent, undefined, ascii);
					const filled = Math.round((Math.max(0, Math.min(100, w.percent)) / 100) * METER_CELLS) * glyph("meterFilled", ascii).length;
					group.push({ role: tone, text: meter.slice(0, filled) }, { role: "frameSoft", text: meter.slice(filled) }, { role: "ink", text: " " });
				}
				group.push({ role: tone, text: `${Math.round(w.percent)}%` });
				if (w.reset && w.resetAt !== null && shown("resets")) group.push({ role: "dim", text: `${dot().text}resets ${resetDay(w.resetAt)}` });
				right.push(group);
			}
		}
		right.push([{ role: "ink", text: formatCost(model.costTotal, model.subscription) }]);
		return { left, right };
	};
	const join = (groups: Run[][]) => groups.flatMap((group, index) => (index ? [divider, ...group] : group));
	const row = (left: Run[], right: Run[]) => {
		const space = total - 2 - width(left) - width(right);
		return onBand(` ${paintRuns(left, paint)}${" ".repeat(Math.max(0, space))}${paintRuns(right, paint)} `, theme);
	};

	if (total < 2) return onBand(" ".repeat(total), theme);
	const longest = Math.min(narrow ? NARROW_BRANCH_CELLS : Number.POSITIVE_INFINITY, visibleWidth(branch));
	const shortest = Math.min(BRANCH_MIN_CELLS, visibleWidth(branch));
	const dropped = new Set<Droppable>(narrow ? ["meter"] : []);
	for (let level = 0; level <= DROP_ORDER.length; level++) {
		if (level > 0) dropped.add(DROP_ORDER[level - 1]!);
		// The branch shortens before anything goes, and stays short once something has.
		const cells = level === 0 ? longest : shortest;
		const full = layout(dropped, cells);
		const over = width(join(full.left)) + (full.left.length ? GAP : 0) + width(join(full.right)) - (total - 2);
		if (over <= 0) return row(join(full.left), join(full.right));
		if (level > 0 || !branch || cells - over < shortest) continue;
		const fitted = layout(dropped, cells - over);
		return row(join(fitted.left), join(fitted.right));
	}
	// The cost alone, clipped when even it does not fit.
	const cost = formatCost(model.costTotal, model.subscription);
	return row([], [{ role: "ink", text: clip(cost, total - 2, { ascii }) }]);
}

// ---------------------------------------------------------------- the model

/** What the footer data gives the bars: pi's branch read. */
export interface BranchSource {
	getGitBranch(): string | null;
	/** What other extensions set on pi's status line, by key. */
	getExtensionStatuses?(): ReadonlyMap<string, string>;
}

interface Usage { costTotal: number; percent: number | null; contextWindow: number | undefined }
interface UsageEntry { type?: string; message?: { role?: string; usage?: { cost?: { total?: number } } } }

// The session's cost and context, computed again only when the session moved
// on (a new entry, another leaf, another model): the bars draw every frame.
const usageCache = new WeakMap<object, { key: unknown[]; usage: Usage }>();

function sessionUsage(ctx: ExtensionContext): Usage {
	const manager = ctx.sessionManager as ExtensionContext["sessionManager"] & { getEntryCount?(): number };
	const read = (): Usage => {
		let costTotal = 0;
		for (const entry of manager.getEntries() as UsageEntry[]) {
			if (entry.type === "message" && entry.message?.role === "assistant") costTotal += entry.message.usage?.cost?.total ?? 0;
		}
		const context = ctx.getContextUsage();
		return { costTotal, percent: context?.percent ?? null, contextWindow: context?.contextWindow ?? ctx.model?.contextWindow };
	};
	// A host without a leaf id cannot say when the session moved: read every time.
	if (typeof manager.getLeafId !== "function") return read();
	const key = [manager.getSessionId(), manager.getLeafId(), manager.getEntryCount?.(), ctx.model, ctx.model?.contextWindow];
	const cached = usageCache.get(manager);
	if (cached && cached.key.every((value, index) => value === key[index])) return cached.usage;
	const usage = read();
	usageCache.set(manager, { key, usage });
	return usage;
}

/** The bars' data from pi: the session's folder, the branch, the model and its effort, the context, the cost and the other extensions' statuses. */
export function barModel(pi: Pick<ExtensionAPI, "getThinkingLevel">, ctx: ExtensionContext, footer: BranchSource, extra: Partial<BarModel> = {}): BarModel {
	const usage = sessionUsage(ctx);
	const model = ctx.model;
	const statuses = [...(footer.getExtensionStatuses?.() ?? new Map<string, string>()).entries()].sort(([a], [b]) => a.localeCompare(b)).map(([, text]) => text);
	return {
		cwd: ctx.sessionManager.getCwd(),
		branch: footer.getGitBranch(),
		...(statuses.length ? { statuses } : {}),
		modelId: model?.id ?? "no-model",
		...(model?.reasoning ? { effort: pi.getThinkingLevel() } : {}),
		contextPercent: usage.percent,
		...(usage.contextWindow ? { contextWindow: usage.contextWindow } : {}),
		costTotal: usage.costTotal,
		subscription: model ? ctx.modelRegistry.isUsingOAuth(model) : false,
		...extra,
	};
}
