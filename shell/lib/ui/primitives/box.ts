/**
 * shell/lib/ui/primitives/box.ts — the overlay box: a rounded frame in the
 * border role with one cell of padding inside each side, the frame Customize
 * and the LDD detail draw their rows in; and the pick row a list in it uses.
 */
import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import { asciiText, glyph } from "./glyphs.ts";

export interface BoxTheme {
	fg(color: string, text: string): string;
	/** With it, the focused pick row is a full-width bar on the selection tint instead of a `▸`. */
	bg?(color: string, text: string): string;
}

/** The cells a box takes around its rows: two sides and a space inside each. */
export const BOX_PADDING = 4;

/** The rows inside a rounded border: each fitted to width minus the padding, cut with `…`. */
export function renderBox(rows: readonly string[], width: number, theme?: BoxTheme, ascii = false): string[] {
	const room = Math.max(1, width - BOX_PADDING);
	const frame = (text: string) => (theme ? theme.fg("border", text) : text);
	const rule = glyph("rule", ascii).repeat(room + 2);
	const side = frame(glyph("side", ascii));
	const inside = (row: string) => {
		const cells = visibleWidth(row);
		return cells > room ? truncateToWidth(row, room, glyph("ellipsis", ascii), true) : `${row}${" ".repeat(room - cells)}`;
	};
	return [
		frame(`${glyph("topLeft", ascii)}${rule}${glyph("topRight", ascii)}`),
		...rows.map((row) => `${side} ${inside(row)} ${side}`),
		frame(`${glyph("bottomLeft", ascii)}${rule}${glyph("bottomRight", ascii)}`),
	];
}

// A control byte in a label shows as `\xNN`, never vanishes and never reaches the terminal.
const CONTROL = /[\x00-\x08\x0B-\x1F\x7F-\x9F]/g;
const shown = (label: string) => label.replace(CONTROL, (byte) => `\\x${byte.charCodeAt(0).toString(16).toUpperCase().padStart(2, "0")}`);

/** A pickable row of width cells: focused, the selection bar when the theme has a background, else `▸ ` in accent. */
export function renderPick(label: string, width: number, focused: boolean, theme?: BoxTheme, ascii = false): string {
	const text = shown(ascii ? asciiText(label) : label);
	const ellipsis = glyph("ellipsis", ascii);
	if (focused && theme?.bg) {
		const row = truncateToWidth(`  ${text}`, width, ellipsis, true);
		return theme.bg("selectedBg", `${row}${" ".repeat(Math.max(0, width - visibleWidth(row)))}`);
	}
	const cursor = `${glyph("cursor", ascii)} `;
	const lead = focused ? (theme ? theme.fg("accent", cursor) : cursor) : "  ";
	const fitted = truncateToWidth(text, Math.max(0, width - 2), ellipsis, true);
	return `${lead}${theme ? theme.fg("text", fitted) : fitted}`;
}
