import { visibleWidth } from "@earendil-works/pi-tui";
import { asciiText, glyph } from "./primitives/glyphs.ts";
import { BAND_ROLE, paintBand, surfaceOpener, type CardTheme } from "./shell-card.ts";
import { HOVER_ROLE, paintHoverable } from "./shell-hover.ts";
import type { ChromeSurface } from "./visual-customization-policy.ts";

// The rail's sections drawn as Gentle Shell's panel (its lib/shell-panel.ts,
// T15.12b): a one-row header `▾ NAME … count`, dense rows indented under it,
// and the narrow work row (T169 removed the full-width sheet). A tinted
// surface paints them on rose ink's band, a transparent one leaves the
// terminal's own background (shell.appearance.surface, as G's Layout →
// Surface); ascii (shell.appearance.ascii) draws every glyph in its ASCII form.

// Each extension loads its own copy of this module: the surface lives in a
// process-wide slot, as the card style does.
const SURFACE_SLOT = Symbol.for("jhon-shell.chrome-surface");
const surfaceState = globalThis as typeof globalThis & { [SURFACE_SLOT]?: unknown };
/** The live chrome surface (shell.appearance.surface): transparent draws each rail section as its own card, tinted one painted panel. */
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
	const fold = paintHoverable(theme, glyph(header.open ? "open" : "closed", ascii), header.hovered ?? false, "muted");
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
	return chromeSurface() === "transparent" ? [...rows] : paintBand(rows, theme, PANEL_SURFACE_ROLE, width);
}

/** A one-row digest of a rail section for the narrow work row. */
export interface PanelSummary {
	left: string;
	/** Right-aligned count, e.g. `2/6`. */
	right?: string;
	/** Names the count when another summary leads the row, e.g. `todos`. */
	rightLabel?: string;
	/** Higher leads the row: live agents outrank the current todo. */
	priority?: number;
}

/** The index of the summary that leads the work row (highest priority, first on ties); -1 when there is none. */
export function workRowLead(summaries: readonly PanelSummary[]): number {
	let lead = -1;
	summaries.forEach((summary, index) => {
		if (lead === -1 || (summary.priority ?? 0) > (summaries[lead]!.priority ?? 0)) lead = index;
	});
	return lead;
}

/** ` ▸ <lead> … <count>`: the section that matters most, plus the todo count; undefined without live work. */
export function renderWorkRow(summaries: readonly PanelSummary[], theme: PanelTheme, width: number, hovered = false, ascii = false): string | undefined {
	if (summaries.length === 0) return undefined;
	const lead = summaries[workRowLead(summaries)]!;
	const counted = lead.right ? lead : summaries.find((summary) => summary.right);
	const right = !counted ? "" : counted === lead ? counted.right! : `${counted.right} ${counted.rightLabel ?? ""}`.trimEnd();
	const target = Math.max(0, Math.floor(width));
	// The ▸ is the row's tap affordance; hovering paints it like every other clickable surface.
	const row = fitPanelLine(`${paintHoverable(theme, glyph("cursor", ascii), hovered, "muted")} ${lead.left}`, right ? theme.fg("muted", right) : "", Math.max(0, target - 2), ascii);
	return paintPanelSurface(theme, [` ${ascii ? asciiText(row) : row} `], target)[0];
}

/** The rail's parts in order: LDD first (L174), then the agent's todo list (while no change shows), jobs, agents and the footer's slot (T174: empty, CHANGES and INTEGRATIONS are gone). */
export const RAIL_ORDER = ["ldd", "todo", "jobs", "agents", "footer"] as const;
