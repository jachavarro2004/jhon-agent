/**
 * shell/lib/ui/pane.ts — the right pane and its narrow strip (design/shell-tui
 * 01 and 04, T174-S4).
 *
 * Full screen and at least PANE_BREAKPOINT columns wide, the terminal is the
 * top row, then the conversation and the editor on the left with the pane on
 * the right behind one thin divider, then the status bar across the whole
 * width (01). The pane has a fixed width: the divider, a space, PANE_TEXT
 * columns of panel and PANE_MARGIN blank ones. Narrower, the pane folds into
 * one strip under the top row: the change, its slice and tasks, the next step,
 * the last run and the key that opens the change (04).
 *
 * The pane shows the parts registered in it (paneSlot), in PANE_ORDER: LDD,
 * the agent's todo list (while no LDD change shows), jobs and agents. Each
 * part renders its own rows; the pane stacks them, a blank row apart in the
 * comfortable density, and hands a click to the part under it.
 *
 * pi lays the full-screen terminal out itself; the pane gets beside the
 * conversation only through pi-tui's layout hook (lib/ui/pi-layout-hook.ts).
 * A regular terminal has no such layout, and a pi-tui without the hook gets
 * none either: both show the narrow form through pi's public slots (the
 * chrome widget above the editor, the footer under it), never a broken one.
 */
import { HStack, ScrollView, VStack, visibleWidth, type Component, type TUI, type TuiMouseEvent } from "@earendil-works/pi-tui";
import { hookLayout } from "./pi-layout-hook.ts";
import { NARROW_BELOW } from "./bars.ts";
import { clip, fit, glyph, paintSurface, renderDivider, stateGlyph, STATES, themePainter, type Role, type ThemeLike } from "./primitives/index.ts";
import { chromeSurface, paintPanelSurface, panelSurfaceOpener, RAIL_ORDER, sectionGap, type PanelSummary } from "./shell-panel.ts";
import type { Density, StatusPlacement } from "./visual-customization-policy.ts";
import type { LddStatus } from "./ldd-status.ts";

/** Under this many columns the pane folds into the strip (design/system.md's breakpoint; the status bar's narrow form too). */
export const PANE_BREAKPOINT = NARROW_BELOW;
/** The panel's text columns (01: the panel 42 cells wide). */
export const PANE_TEXT = 42;
/** Blank columns right of the panel text, to the terminal's edge. */
export const PANE_MARGIN = 2;
/** The divider with a space each side. */
const DIVIDER_COLUMNS = 3;
/** Every column the pane takes from the conversation: at 180 columns the conversation keeps 133, as 01's. */
export const PANE_COLUMNS = DIVIDER_COLUMNS + PANE_TEXT + PANE_MARGIN;
/** The pane's parts, top to bottom. */
export const PANE_ORDER = RAIL_ORDER;

// ---------------------------------------------------------------- parts

/** A part of the pane: its rows, and what the shell's other surfaces read from it. */
export interface PanePart extends Component {
	/** Cheap digest of the live state the part paints: an unchanged one reuses its rows. */
	digest?(): string;
	/** One-row digest for the narrow work row above the editor. */
	summary?(): PanelSummary | undefined;
	/** The part's own view, which a tap on the work row opens. */
	open?(): void;
	/** The narrow form's strip (04), one row exactly width cells, or undefined with nothing to show. */
	strip?(width: number): string | undefined;
}

export interface PaneState {
	parts: Map<string, PanePart>;
	/** Bumped by invalidatePane: parts without a digest render again. */
	revision: number;
	/** The pointer's current control (setPaneHover). */
	hover?: { owner: object; clear: () => void };
	/** The pane host lays this full-screen terminal out: the top row, the pane or the strip and the status bar are its rows. */
	hosted?: () => boolean;
	/** The pane shows beside the conversation. */
	wide?: () => boolean;
}

// On the terminal, not in a module: each extension loads its own copy of this module.
const STATE = Symbol.for("jhon-shell.pane.state");
const loose: PaneState = { parts: new Map(), revision: 0 };

export function paneState(tui: TUI): PaneState {
	if (!tui.terminal) return loose;
	const terminal = tui.terminal as unknown as Record<symbol, PaneState>;
	return terminal[STATE] ??= { parts: new Map(), revision: 0 };
}

/** Registers a part under key and returns its disposer, which never removes a part registered after it. */
export function paneSlot(tui: TUI, key: string, part: PanePart): () => void {
	if (!tui.terminal) return () => {};
	const state = paneState(tui);
	state.parts.set(key, part);
	return () => {
		if (state.parts.get(key) === part) state.parts.delete(key);
	};
}

/** A part's state changed: the pane draws it again. */
export function invalidatePane(tui: TUI): void {
	if (tui.terminal) paneState(tui).revision++;
}

/** The one control the pointer is over: a move onto another clears the one before. */
export function setPaneHover(tui: TUI, owner?: object, clear?: () => void): void {
	if (!tui.terminal) return;
	const state = paneState(tui);
	if (state.hover?.owner === owner) return;
	const previous = state.hover;
	state.hover = owner && clear ? { owner, clear } : undefined;
	previous?.clear();
}

// ---------------------------------------------------------------- the pane's rows

interface Section { key: string; part: PanePart; start: number; height: number }

function rowsOf(part: PanePart | undefined, width: number): string[] {
	if (!part) return [];
	const rows = [...(part.render(width) ?? [])];
	while (rows.length && rows[rows.length - 1]!.trim() === "") rows.pop();
	return rows;
}

/**
 * The pane's panel rows, PANE_TEXT wide: a blank row under the top row, then
 * each part's rows in PANE_ORDER, a blank row apart in the comfortable density.
 */
export function paneRows(parts: ReadonlyMap<string, PanePart>, density?: Density, width = PANE_TEXT): { rows: string[]; sections: Section[] } {
	const rows: string[] = [""];
	const sections: Section[] = [];
	for (const key of PANE_ORDER) {
		const part = parts.get(key);
		const lines = rowsOf(part, width);
		if (!part || !lines.length) continue;
		if (sections.length && sectionGap(density)) rows.push("");
		sections.push({ key, part, start: rows.length, height: lines.length });
		rows.push(...lines);
	}
	return { rows, sections };
}

// ---------------------------------------------------------------- the strip

/** What the strip says (04). */
export interface StripModel {
	change: string;
	/** The slice in progress, 1-based, of how many. */
	slice?: { at: number; of: number };
	/** Its tasks, or the change's when no slice is in progress. */
	tasks?: { done: number; total: number };
	next?: string;
	lastRun?: { passed: boolean; failed: number; ago?: string };
	/** The key that opens the change (shell.keys.panel). */
	key?: string;
}

/** `6m`, `2h`, `3d`; `now` under a minute; undefined for a time that cannot be read. */
export function ago(at: string | undefined, now: number): string | undefined {
	const then = at ? Date.parse(at) : Number.NaN;
	if (!Number.isFinite(then)) return undefined;
	const minutes = Math.floor(Math.max(0, now - then) / 60_000);
	if (minutes < 1) return "now";
	if (minutes < 60) return `${minutes}m`;
	const hours = Math.floor(minutes / 60);
	return hours < 24 ? `${hours}h` : `${Math.floor(hours / 24)}d`;
}

/** The strip's model from what `jhon status` said of the change. */
export function stripModel(ldd: LddStatus, now: number, key?: string): StripModel {
	const index = ldd.slices.indexOf("current");
	const detail = index >= 0 ? ldd.detail?.[index] : undefined;
	const tasks = detail ? { done: detail.completed, total: detail.total } : ldd.tasks ? { done: ldd.tasks.completed, total: ldd.tasks.total } : undefined;
	const when = ldd.tests ? ago(ldd.tests.at, now) : undefined;
	return {
		change: ldd.change,
		...(index >= 0 ? { slice: { at: index + 1, of: ldd.slices.length } } : {}),
		...(tasks && tasks.total > 0 ? { tasks } : {}),
		...(ldd.next ? { next: ldd.next } : {}),
		...(ldd.tests ? { lastRun: { passed: ldd.tests.passed, failed: ldd.tests.failed, ...(when ? { ago: when } : {}) } } : {}),
		...(key ? { key } : {}),
	};
}

interface Run { role: Role; text: string; bold?: boolean }

/** What gives way when the strip does not fit, first to last; the change is never in it. */
export const STRIP_DROP_ORDER = ["key", "lastRun", "next", "tasks", "slice"] as const;
type StripPiece = (typeof STRIP_DROP_ORDER)[number];

/**
 * ` ◆ change  ◐ slice 3/4 · tasks 2/4 · next apply slice 3  ✓ passed 6m … alt+j ▾ `:
 * one row of exactly width cells on the chrome's band.
 */
export function renderStrip(model: StripModel, theme: ThemeLike, width: number, { ascii = false }: { ascii?: boolean } = {}): string {
	const total = Math.max(0, Math.floor(width));
	const paint = themePainter(theme);
	const dot: Run = { role: "dim", text: ascii ? " - " : " · " };
	const clean = (text: string) => text.replace(/[\x00-\x1f\x7f]/g, "").replace(/\s+/g, " ").trim();
	const layout = (dropped: ReadonlySet<StripPiece>): { left: Run[]; right: Run[] } => {
		const shown = (piece: StripPiece) => !dropped.has(piece);
		const left: Run[] = [{ role: "accent", text: `${glyph("change", ascii)} `, bold: true }, { role: "ink", text: clean(model.change), bold: true }];
		const work: Run[][] = [];
		if (model.slice && shown("slice")) work.push([{ role: "warn", text: `${stateGlyph("working", ascii)} ` }, { role: "ink", text: `slice ${model.slice.at}/${model.slice.of}` }]);
		if (model.tasks && shown("tasks")) work.push([{ role: "dim", text: `tasks ${model.tasks.done}/${model.tasks.total}` }]);
		if (model.next && shown("next")) work.push([{ role: "dim", text: "next " }, { role: "ink", text: clean(model.next) }]);
		if (work.length) left.push({ role: "ink", text: "  " }, ...work.flatMap((group, index) => (index ? [dot, ...group] : group)));
		if (model.lastRun && shown("lastRun")) {
			const state = model.lastRun.passed ? "done" : "failed";
			const words = model.lastRun.passed ? "passed" : `failed ${model.lastRun.failed}`;
			left.push({ role: "ink", text: "  " }, { role: STATES[state].role, text: `${stateGlyph(state, ascii)} ` }, { role: "dim", text: model.lastRun.ago ? `${words} ${model.lastRun.ago}` : words });
		}
		const right: Run[] = model.key && shown("key") ? [{ role: "soft", text: model.key }, { role: "dim", text: ` ${glyph("open", ascii)}` }] : [];
		return { left, right };
	};
	const cells = (runs: readonly Run[]) => runs.reduce((sum, run) => sum + visibleWidth(run.text), 0);
	const painted = (runs: readonly Run[]) => runs.map((run) => {
		if (!run.text.trim()) return run.text;
		const text = paint.fg(run.role, run.text);
		return run.bold ? paint.bold(text) : text;
	}).join("");
	const room = Math.max(0, total - 2);
	const dropped = new Set<StripPiece>();
	let shown = layout(dropped);
	for (const piece of STRIP_DROP_ORDER) {
		if (cells(shown.left) + (shown.right.length ? 2 + cells(shown.right) : 0) <= room) break;
		dropped.add(piece);
		shown = layout(dropped);
	}
	if (total === 0) return "";
	let left = painted(shown.left);
	if (cells(shown.left) > room) left = paint.bold(paint.fg("ink", clip(`${glyph("change", ascii)} ${clean(model.change)}`, room, { ascii })));
	const gap = room - visibleWidth(left) - cells(shown.right);
	return paintSurface(` ${left}${" ".repeat(Math.max(0, gap))}${painted(shown.right)} `, panelSurfaceOpener(theme));
}

// ---------------------------------------------------------------- the host

export interface PaneOptions {
	/** shell.appearance.status: hidden takes the status bar, the pane and the strip away. */
	placement(): StatusPlacement;
	density(): Density;
	/** Said once when pi-tui has no layout hook. */
	note(text: string): void;
}

/**
 * Lays a full-screen terminal out as 01 and 04 draw it and publishes
 * `hosted` and `wide` on the pane state; returns the disposer that gives pi
 * its own layout back. The top row and the status bar are the "header" and
 * "statusbar" parts; the strip is the "ldd" part's.
 */
export function installPane(tui: TUI, theme: ThemeLike, options: PaneOptions): () => void {
	if (!tui.terminal) return () => {};
	const state = paneState(tui);
	const shown = () => options.placement() !== "hidden";
	const wideAt = (columns: number) => columns >= PANE_BREAKPOINT && shown();
	const paint = themePainter(theme);
	const delegate = (key: string) => (event: TuiMouseEvent) => {
		if (event.type === "move" && event.button === "none") setPaneHover(tui);
		return state.parts.get(key)?.handleMouse?.(event);
	};
	const row = (key: string): Component => ({ render: (width) => rowsOf(state.parts.get(key), width), invalidate() {}, handleMouse: delegate(key) });
	const top = row("header");
	const status: Component = { render: (width) => (shown() ? rowsOf(state.parts.get("statusbar"), width) : []), invalidate() {}, handleMouse: delegate("statusbar") };
	const strip: Component = {
		render: (width) => {
			const line = shown() ? state.parts.get("ldd")?.strip?.(width) : undefined;
			return line ? [line] : [];
		},
		invalidate() {},
		handleMouse: delegate("ldd"),
	};
	// The divider runs the pane's whole height: the layout clips it to the rows it gets.
	const divider: Component = { render: () => renderDivider(tui.terminal.rows, { paint }).map((rule) => ` ${rule} `), invalidate() {} };
	let memo: { key: string; lines: string[]; sections: Section[] } | undefined;
	const content: Component = {
		render: (width) => {
			const density = options.density();
			const digests = PANE_ORDER.map((key) => {
				const part = state.parts.get(key);
				if (!part) return "-";
				try { return part.digest ? `d${part.digest()}` : `r${state.revision}`; } catch { return `r${state.revision}`; }
			});
			const key = JSON.stringify([width, density, chromeSurface(), digests, PANE_ORDER.map((name) => state.parts.get(name) !== undefined)]);
			if (memo?.key === key && memo.sections.every((section) => state.parts.get(section.key) === section.part)) return memo.lines;
			const laid = paneRows(state.parts, density, PANE_TEXT);
			const lines = paintPanelSurface(theme, laid.rows.map((line) => fit(line, width)), width);
			memo = { key, lines, sections: laid.sections };
			return lines;
		},
		invalidate() { memo = undefined; },
		handleMouse: (event) => {
			const hit = memo?.sections.find((section) => event.y >= section.start && event.y < section.start + section.height);
			if (!hit || state.parts.get(hit.key) !== hit.part || event.x >= PANE_TEXT) {
				if (event.type === "move" && event.button === "none") setPaneHover(tui);
				return undefined;
			}
			const result = hit.part.handleMouse?.({ ...event, y: event.y - hit.start, width: PANE_TEXT, height: hit.height });
			if (result?.render) memo = undefined;
			return result;
		},
	};
	const scroll = new ScrollView(content, {
		follow: "none",
		primary: false,
		overscroll: "contain",
		scrollbar: "auto",
		scrollbarTrackStyle: (text) => paint.fg("faint", text),
		scrollbarThumbStyle: (text) => paint.fg("accent", text),
	});
	const wide = (viewport: { width: number }) => wideAt(viewport.width);
	const build = (native: Component) => {
		const split = new HStack([
			{ component: native, basis: 0, grow: 1, shrink: 1, minSize: 1 },
			{ component: divider, basis: DIVIDER_COLUMNS, grow: 0, shrink: 0, minSize: DIVIDER_COLUMNS, visible: wide },
			{ component: scroll, basis: PANE_TEXT + PANE_MARGIN, grow: 0, shrink: 0, minSize: PANE_TEXT + PANE_MARGIN, visible: wide },
		]);
		return new VStack([
			{ component: top, basis: "auto", grow: 0, shrink: 0 },
			{ component: strip, basis: "auto", grow: 0, shrink: 0, visible: (viewport) => !wide(viewport) },
			{ component: split, basis: 0, grow: 1, shrink: 1, minSize: 1 },
			{ component: status, basis: "auto", grow: 0, shrink: 0 },
		]);
	};
	const hook = hookLayout(tui, build, () => true, options.note);
	state.hosted = () => hook.attached();
	state.wide = () => hook.attached() && wideAt(tui.terminal.columns);
	return () => {
		hook.dispose();
		state.hosted = undefined;
		state.wide = undefined;
		setPaneHover(tui);
	};
}
