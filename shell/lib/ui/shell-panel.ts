import { visibleWidth } from "@earendil-works/pi-tui";
import { glyph } from "./primitives/glyphs.ts";
import { BAND_ROLE, HOVER_ROLE, paintHover, paintSurfaceRows, surfaceOpener, type CardTheme } from "./primitives/index.ts";
import type { ChromeSurface } from "./visual-customization-policy.ts";

// The rail's sections drawn as Gentle Shell's panel (its lib/shell-panel.ts,
// T15.12b): a one-row header `▾ NAME … count` and dense rows indented under
// it (the todo list's). A tinted
// surface paints them on rose ink's band, a transparent one leaves the
// terminal's own background (shell.appearance.surface, as G's Layout →
// Surface); ascii (shell.appearance.ascii) draws every glyph in its ASCII form.

// Each extension loads its own copy of this module: the surface lives in a
// process-wide slot, as the card style does.
const SURFACE_SLOT = Symbol.for("jhon-shell.chrome-surface");
const surfaceState = globalThis as typeof globalThis & { [SURFACE_SLOT]?: unknown };
/** The live chrome surface (shell.appearance.surface): transparent leaves the right pane on the terminal's background, tinted paints one panel. */
export function chromeSurface(): ChromeSurface { return surfaceState[SURFACE_SLOT] === "tinted" ? "tinted" : "transparent"; }
export function setChromeSurface(value: ChromeSurface): void { surfaceState[SURFACE_SLOT] = value; }

/** The theme background a tinted rail paints behind its sections: rose ink's band. */
export const PANEL_SURFACE_ROLE = BAND_ROLE;
/** The SGR that opens the panel surface, or "" when the chrome is transparent or the theme cannot paint it. */
export function panelSurfaceOpener(theme: CardTheme): string {
	return chromeSurface() === "transparent" ? "" : surfaceOpener(theme, PANEL_SURFACE_ROLE);
}

/** Rail sections keep a blank row between them in comfortable density, whatever the surface (G's). */
export function sectionGap(density: string | undefined): boolean {
	return density === undefined || density === "comfortable";
}

/** Columns a panel row is indented under its header. */
export const PANEL_ROW_INDENT = 2;

export interface PanelTheme extends CardTheme {
	bold?(text: string): string;
}

export interface PanelHeader {
	title: string;
	/** Right-aligned summary, e.g. `2/6` or `1 running`. */
	count?: string;
	/** Role for the count; muted unless the section signals a state. */
	countRole?: string;
	open: boolean;
	/** The pointer is over the header, the section's collapse control. */
	hovered?: boolean;
	ascii?: boolean;
}

/** Clips plain-or-ANSI `text` to `width` columns with an ellipsis; SGR codes are kept. */
export function clipPanelText(text: string, width: number, ascii = false): string {
	if (width <= 0) return "";
	if (visibleWidth(text) <= width) return text;
	let clipped = "";
	// Walk escape sequences as zero-width units so a clip never splits one.
	for (const unit of text.match(/\x1b\[[0-9;]*m|[\s\S]/gu) ?? []) {
		if (unit.startsWith("\x1b[")) { clipped += unit; continue; }
		if (visibleWidth(clipped + unit) > width - 1) break;
		clipped += unit;
	}
	return `${clipped}${glyph("ellipsis", ascii)}`;
}

/** `left` and `right` on one row exactly `width` columns wide; `left` gives way first. */
export function fitPanelLine(left: string, right: string, width: number, ascii = false): string {
	const target = Math.max(0, Math.floor(width));
	const rightWidth = visibleWidth(right);
	const shownRight = rightWidth + 1 <= target ? right : "";
	const room = target - visibleWidth(shownRight) - (shownRight ? 1 : 0);
	const shownLeft = clipPanelText(left, room, ascii);
	return `${shownLeft}${" ".repeat(Math.max(0, target - visibleWidth(shownLeft) - visibleWidth(shownRight)))}${shownRight}`;
}

export function renderPanelHeader(theme: PanelTheme, width: number, header: PanelHeader): string {
	const ascii = header.ascii ?? false;
	const fold = paintHover(theme, glyph(header.open ? "open" : "closed", ascii), header.hovered ?? false, "muted");
	const name = header.title.toUpperCase();
	// The hover paints the name too: a lone glyph is too small a signal.
	const title = theme.fg(header.hovered ? HOVER_ROLE : "text", theme.bold ? theme.bold(name) : name);
	const count = header.count ? theme.fg(header.countRole ?? "muted", header.count) : "";
	return fitPanelLine(`${fold} ${title}`, count, width, ascii);
}

/** One section row, indented under the header, with an optional right column. */
export function renderPanelRow(width: number, left: string, right = "", indent = PANEL_ROW_INDENT): string {
	return fitPanelLine(`${" ".repeat(indent)}${left}`, right, width);
}

/** Every row on the panel surface, padded to width; transparent, or a theme that cannot paint it, leaves rows as they are. */
export function paintPanelSurface(theme: CardTheme, rows: readonly string[], width: number): string[] {
	return chromeSurface() === "transparent" ? [...rows] : paintSurfaceRows(rows, theme, PANEL_SURFACE_ROLE, width);
}

/** The right pane's parts in order (lib/ui/pane.ts, design 01): LDD first (L174), then the agent's todo list (while no change shows), Agents and Jobs. */
export const RAIL_ORDER = ["ldd", "todo", "agents", "jobs"] as const;
