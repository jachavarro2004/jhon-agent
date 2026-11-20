import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import { asciiText, glyph } from "./primitives/index.ts";
import { stripAnsi } from "./terminal-theme.ts";

// The overlay card helpers of the Gentle Shell's Commands card (its
// lib/command-palette.ts): the card chrome and the selectable row that
// Customize (settings-view.ts) and the LDD detail (jhon-panel.ts) draw with.
// T169 (L1438) removed the command palette itself; these stayed.

export interface OverlayCardTheme {
	fg(color: string, text: string): string;
	bold?(text: string): string;
	/** Optional: when present, the selected row is drawn as a full-width
	 * highlighted bar via bg("selectedBg", ...) instead of a `▸` marker. */
	bg?(color: string, text: string): string;
}

// A stray control byte in a label is escaped into a visible "\xNN", never
// silently dropped (lib/terminal-theme.ts deletes them instead).
function sanitizeTerminalText(value: string): string {
	return value.replace(/[\x00-\x08\x0B-\x1F\x7F-\x9F]/g, (control) => `\\x${control.charCodeAt(0).toString(16).toUpperCase().padStart(2, "0")}`);
}

// Two border glyphs ("│ " and " │") plus the padding space on each side.
export const CARD_PADDING = 4;

/** G's Commands card chrome, shared by the overlay panels; `+ - |` in ASCII. */
export function renderPaletteCard(lines: string[], width: number, theme?: OverlayCardTheme, ascii = false): string[] {
	const innerWidth = Math.max(1, width - CARD_PADDING);
	const border = (text: string) => theme ? theme.fg("border", text) : text;
	const horizontal = glyph("rule", ascii).repeat(innerWidth + 2);
	const side = glyph("side", ascii);
	return [border(`${glyph("topLeft", ascii)}${horizontal}${glyph("topRight", ascii)}`), ...lines.map((content) => {
		const visible = visibleWidth(stripAnsi(content));
		const fitted = visible > innerWidth ? truncateToWidth(content, innerWidth, glyph("ellipsis", ascii), true) : content + " ".repeat(innerWidth - visible);
		return `${border(side)} ${fitted} ${border(side)}`;
	}), border(`${glyph("bottomLeft", ascii)}${horizontal}${glyph("bottomRight", ascii)}`)];
}

/** A selectable row as G draws it: the `selectedBg` bar when the theme has one, else `▸ `. */
export function renderPaletteSelection(label: string, width: number, focused: boolean, theme?: OverlayCardTheme, ascii = false): string {
	const safe = sanitizeTerminalText(ascii ? asciiText(label) : label);
	if (focused && theme?.bg) {
		const row = truncateToWidth(`  ${safe}`, width, glyph("ellipsis", ascii), true);
		return theme.bg("selectedBg", row + " ".repeat(Math.max(0, width - visibleWidth(stripAnsi(row)))));
	}
	const mark = `${glyph("cursor", ascii)} `;
	const prefix = focused ? (theme ? theme.fg("accent", mark) : mark) : "  ";
	const text = truncateToWidth(safe, Math.max(0, width - 2), glyph("ellipsis", ascii), true);
	return prefix + (theme ? theme.fg("text", text) : text);
}
