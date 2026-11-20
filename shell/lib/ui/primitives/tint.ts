/**
 * shell/lib/ui/primitives/tint.ts — the surfaces a row sits on (design/system.md
 * "Colour roles of the shell"): pi paints them as backgrounds. A surface is
 * opened once per row and opened again after every reset the row's own
 * content carries, so painted text never punches a hole in it, and closed at
 * the row's end, so it never leaks into the terminal's padding.
 */
import { visibleWidth } from "@earendil-works/pi-tui";

/** The surfaces by their rose ink name, as pi theme background roles. */
export const TINT = {
	/** Box: tool cards and the tinted block. */
	box: "toolSuccessBg",
	/** errorContainer: a failed or refused card's header. */
	error: "toolErrorBg",
	/** Frame: the status line, the top row, the rail. */
	band: "customMessageBg",
	/** BoxStrong: the owner's message. */
	strong: "userMessageBg",
	/** primaryContainer: the selected row. */
	sel: "selectedBg",
} as const;

/** The band the status line and the rail sit on. */
export const BAND_ROLE = TINT.band;

export interface SurfaceTheme {
	fg(color: string, text: string): string;
	/** Without it (a test theme), surfaces draw nothing. */
	bg?(color: string, text: string): string;
}

const CLOSE = "\x1b[49m";
// A full reset or a background reset ends the surface; the row opens it again after each.
const ENDS_SURFACE = /\x1b\[(?:0|49)?m/g;

/** The escape that opens role as a background, or "" when the theme cannot paint it. */
export function surfaceOpener(theme: SurfaceTheme, role: string): string {
	if (!theme.bg) return "";
	try {
		const sample = theme.bg(role, "");
		return sample.endsWith(CLOSE) ? sample.slice(0, sample.length - CLOSE.length) : "";
	} catch {
		return "";
	}
}

/** One row on the surface `open` starts; the row as it is when open is "". */
export function paintSurface(row: string, open: string): string {
	if (!open) return row;
	return `${open}${row.replace(ENDS_SURFACE, (reset) => `${reset}${open}`)}${CLOSE}`;
}

/** Rows padded to width and painted on role; unchanged when the theme cannot paint it. */
export function paintSurfaceRows(rows: readonly string[], theme: SurfaceTheme, role: string, width: number): string[] {
	const open = surfaceOpener(theme, role);
	if (!open) return [...rows];
	return rows.map((row) => paintSurface(`${row}${" ".repeat(Math.max(0, Math.floor(width) - visibleWidth(row)))}`, open));
}
