import { visibleWidth } from "@earendil-works/pi-tui";
import { glyph } from "./primitives/glyphs.ts";
import { BAND_ROLE, HOVER_ROLE, paintHover, paintSurfaceRows, surfaceOpener, type CardTheme } from "./primitives/index.ts";
import type { ChromeSurface } from "./appearance-policy.ts";

// The right pane's section chrome (design 01): each section opens with one
// header row, a fold glyph, the section's name and a count pushed to the right
// edge, and its rows sit two cells in under that header. The pane is either
// left on the terminal's own background or laid on rose ink's band
// (shell.appearance.surface); shell.appearance.ascii swaps every glyph for its
// ASCII form.

// pi gives each extension its own module cache, so the surface choice is kept
// on globalThis where every copy of this module reads the same value.
const SURFACE_KEY = Symbol.for("jhon-shell.chrome-surface");
type SurfaceHolder = { [SURFACE_KEY]?: unknown };
const holder = globalThis as SurfaceHolder;

/** The live chrome surface (shell.appearance.surface): transparent leaves the right pane on the terminal's background, tinted paints one panel. */
export function chromeSurface(): ChromeSurface {
	const stored = holder[SURFACE_KEY];
	return stored === "tinted" ? stored : "transparent";
}
export function setChromeSurface(value: ChromeSurface): void {
	holder[SURFACE_KEY] = value;
}

/** The theme background a tinted rail paints behind its sections: rose ink's band. */
export const PANEL_SURFACE_ROLE = BAND_ROLE;

const tinted = (): boolean => chromeSurface() !== "transparent";

/** The colour code that starts rose ink's band behind the pane; empty on a transparent pane or a theme without that background. */
export const panelSurfaceOpener = (theme: CardTheme): string => (tinted() ? surfaceOpener(theme, PANEL_SURFACE_ROLE) : "");

/** The pane's two forms (L1495): comfortable draws each section in its own box. */
export function sectionBoxes(density: string | undefined): boolean {
	return density === "comfortable";
}

/** Compact (the default, L1498) keeps a blank row between the pane's sections (01's); minimal none; comfortable's boxes need none. */
export const sectionGap = (density?: string): boolean => (density ?? "compact") === "compact";

/** How far a section's rows sit in from its header, in cells. */
export const PANEL_ROW_INDENT: number = 2;

/** A card theme that may also embolden text (the section names). */
export type PanelTheme = CardTheme & { bold?: (text: string) => string };

/** What a section's header row shows. */
export type PanelHeader = {
	/** Printed in capitals after the fold glyph. */
	title: string;
	/** Short text at the right edge (`2/6`, `1 running`); none when absent. */
	count?: string;
	/** The count's theme role; muted when absent. */
	countRole?: string;
	/** Whether the section is unfolded: ▾ when it is, ▸ when not. */
	open: boolean;
	/** The pointer rests on the header, which folds the section when clicked. */
	hovered?: boolean;
	ascii?: boolean;
};

// An SGR colour or weight code, matched only where the scan stands.
const SGR_AT = /\x1b\[[0-9;]*m/y;

/** `text` (painted or not) cut to `width` cells, its last cell an ellipsis when cut; every colour code before the cut stays. */
export function clipPanelText(text: string, width: number, ascii = false): string {
	if (width <= 0) return "";
	if (visibleWidth(text) <= width) return text;
	// One cell stays free for the ellipsis; colour codes pass at no width, so
	// a cut never lands inside one.
	const budget = width - 1;
	let kept = "";
	let at = 0;
	while (at < text.length) {
		SGR_AT.lastIndex = at;
		const code = SGR_AT.exec(text);
		if (code !== null) {
			kept += code[0];
			at += code[0].length;
			continue;
		}
		const char = String.fromCodePoint(text.codePointAt(at) ?? 0);
		if (visibleWidth(kept + char) > budget) break;
		kept += char;
		at += char.length;
	}
	return kept + glyph("ellipsis", ascii);
}

/** One row of exactly `width` cells: `left` at the start, `right` at the end; when room runs out, `left` is cut before `right` is dropped. */
export function fitPanelLine(left: string, right: string, width: number, ascii = false): string {
	const cells = width > 0 ? Math.floor(width) : 0;
	// The right part needs its own cells plus one of gap, or it is left out.
	const tail = visibleWidth(right) < cells ? right : "";
	const gap = tail === "" ? 0 : 1;
	const head = clipPanelText(left, cells - visibleWidth(tail) - gap, ascii);
	const spaces = cells - visibleWidth(head) - visibleWidth(tail);
	return head + (spaces > 0 ? " ".repeat(spaces) : "") + tail;
}

/** A section's header row: fold glyph, capitalised name, and the count at the right edge. */
export function renderPanelHeader(theme: PanelTheme, width: number, { title, count, countRole, open, hovered = false, ascii = false }: PanelHeader): string {
	const caps = title.toUpperCase();
	const strong = theme.bold === undefined ? caps : theme.bold(caps);
	// Hovering lights the name as well as the glyph: one cell alone reads poorly.
	const label = theme.fg(hovered ? HOVER_ROLE : "text", strong);
	const fold = paintHover(theme, glyph(open ? "open" : "closed", ascii), hovered, "muted");
	const right = count === undefined || count === "" ? "" : theme.fg(countRole ?? "muted", count);
	return fitPanelLine(`${fold} ${label}`, right, width, ascii);
}

/** A row of a section: `left` set in under the header, `right` (if any) at the row's end. */
export const renderPanelRow = (width: number, left: string, right = "", indent = PANEL_ROW_INDENT): string => fitPanelLine(" ".repeat(indent) + left, right, width);

/** Every row on the panel surface, padded to width; transparent, or a theme that cannot paint it, leaves rows as they are. */
export function paintPanelSurface(theme: CardTheme, rows: readonly string[], width: number): string[] {
	return tinted() ? paintSurfaceRows(rows, theme, PANEL_SURFACE_ROLE, width) : rows.slice();
}

/** The right pane's parts in order (lib/ui/pane.ts, design 01): LDD first (L174), then the agent's todo list (while no change shows), Agents and Jobs. */
export const RAIL_ORDER = ["ldd", "todo", "agents", "jobs"] as const;
