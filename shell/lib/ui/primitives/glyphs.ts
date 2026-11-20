/**
 * shell/lib/ui/primitives/glyphs.ts — the glyph table (design/system.md "Glyph
 * table"): every primitive reads its glyphs here, and the ASCII flag switches
 * the whole table, frames included. Each ASCII form is one cell.
 */

import { STATES } from "./states.ts";

export const GLYPHS = {
	topLeft: ["╭", "+"], topRight: ["╮", "+"], bottomLeft: ["╰", "+"], bottomRight: ["╯", "+"],
	rule: ["─", "-"], side: ["│", "|"],
	// The heavy run under a full-screen view's tab (design/shell-tui 06, 10).
	heavyRule: ["━", "="],
	cursor: ["▸", ">"], askCursor: ["❯", ">"],
	open: ["▾", "v"], closed: ["▸", ">"],
	result: ["↳", ">"], tree: ["└", "`"],
	meterFilled: ["▰", "#"], meterEmpty: ["▱", "-"],
	tabLeft: ["‹", "<"], tabRight: ["›", ">"], tabSep: ["·", "."],
	radioOn: ["●", "*"], radioOff: ["○", "o"],
	ellipsis: ["…", "~"], bar: ["▎", "|"], job: ["⧗", "~"], change: ["◆", "#"], branch: ["⎇", "@"],
	// The regular bar's separator, as G's; the brand, the one form wider than a cell.
	barSeparator: ["⟡", "|"], brand: ["力", "jhon"],
} as const satisfies Record<string, readonly [string, string]>;

export type GlyphName = keyof typeof GLYPHS;

export function glyph(name: GlyphName, ascii = false): string {
	return GLYPHS[name][ascii ? 1 : 0];
}

// Each ASCII form below is one cell, as its glyph is: folding keeps a row's
// width. The brand is the exception (力, two cells, reads jhon): a row that
// carries it picks glyph("brand", ascii) before it is fitted.
const FOLD: ReadonlyArray<readonly [string, string]> = [
	[" · ", " - "],
	...Object.values(GLYPHS).filter(([u]) => u !== "·"),
	...Object.values(STATES).map((s) => [s.glyph, s.ascii] as const),
	["–", "-"], ["—", "-"], ["−", "-"], ["↑", "^"], ["↓", "v"], ["←", "<"], ["→", ">"], ["✎", "*"], ["±", "*"], ["✕", "-"], ["◈", "*"], ["•", "*"], ["◇", "*"], ["❯", ">"],
];

/**
 * A row's glyphs in their ASCII forms (shell.appearance.ascii): the table's,
 * the states', `·` between words as `-`, arrows and dashes; the rest of the
 * text unchanged. For rows an overlay builds from words rather than from the
 * primitives, so ascii holds for the whole overlay.
 */
export function asciiText(text: string): string {
	let out = text;
	for (const [from, to] of FOLD) out = out.replaceAll(from, to);
	return out;
}
