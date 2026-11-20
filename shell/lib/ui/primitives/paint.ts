/**
 * shell/lib/ui/primitives/paint.ts — the colour roles of the shell's design
 * system (design/system.md "Colour roles of the shell") and the painter every
 * primitive draws with, so a primitive names a role and never a pi theme key.
 */
import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import { clip } from "./fit.ts";

export type Role = "ink" | "soft" | "dim" | "accent" | "accentSoft" | "frameSoft" | "line" | "faint" | "ok" | "warn" | "bad";

/** Each role's pi theme name; `sel` is the cursor row's background. `faint` (outlineVariant) draws rules and pane dividers (design/shell-tui). */
export const THEME_NAME: Record<Role | "sel", string> = {
	ink: "text", soft: "muted", dim: "dim", accent: "accent", accentSoft: "customMessageLabel",
	frameSoft: "border", line: "borderMuted", faint: "mdHr", ok: "success", warn: "warning", bad: "error", sel: "selectedBg",
};

export interface Painter {
	fg(role: Role, text: string): string;
	/** The cursor row's tint behind text. */
	sel(text: string): string;
	bold(text: string): string;
	underline(text: string): string;
}

/** No colour at all: tests and plain output. */
export function plainPainter(): Painter {
	const same = (text: string) => text;
	return { fg: (_role, text) => text, sel: same, bold: same, underline: same };
}

export interface ThemeLike {
	fg(color: string, text: string): string;
	/** Without bg (a test theme, a card's paint), the tint draws plain. */
	bg?(color: string, text: string): string;
	bold(text: string): string;
	/** Without underline, plain. */
	underline?(text: string): string;
}

/** A painter over a pi theme. */
export function themePainter(theme: ThemeLike): Painter {
	return {
		fg: (role, text) => (text ? theme.fg(THEME_NAME[role], text) : text),
		sel: (text) => (theme.bg ? theme.bg(THEME_NAME.sel, text) : text),
		bold: (text) => (text ? theme.bold(text) : text),
		underline: (text) => (text && theme.underline ? theme.underline(text) : text),
	};
}

/** Text cut to width with the truncation glyph (`…`, ASCII `~`); never wider (fit.ts clip). */
export function cut(text: string, width: number, ascii = false): string {
	return clip(text, width, { ascii });
}

/** Painted text already known to take `cells` cells, padded with spaces to width (painted text is never measured). */
export function fill(painted: string, cells: number, width: number): string {
	return `${painted}${" ".repeat(Math.max(0, width - cells))}`;
}

/** A line of exactly width cells: cut without a glyph, then padded with spaces. */
export function pad(line: string, width: number): string {
	if (width <= 0) return "";
	const fitted = visibleWidth(line) <= width ? line : truncateToWidth(line, width, "");
	return fitted + " ".repeat(Math.max(0, width - visibleWidth(fitted)));
}
