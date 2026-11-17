/**
 * The overlay pattern's primitives (T15.10, L108, design/system.md "Overlay
 * pattern"): the header row with the title and `esc`, the column label, the
 * pane header with its count, the inline radio row, the preview block and the
 * footer key line with ` · ` separators. Every line is exactly the width
 * asked for; ASCII mode prints only ASCII.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-primitives-overlay-pattern.test.ts
 */
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import {
	plainPainter, renderChoiceRow, renderPaneHeader, renderPreview, themePainter,
} from "../lib/ui/primitives/index.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";

const WIDTHS = [186, 120, 80, 40];
const paint = plainPainter();
const tagged = themePainter({ fg: (c, t) => `<${c}>${t}</${c}>`, bg: (c, t) => `[${c}]${t}[/${c}]`, bold: (t) => `<b>${t}</b>`, underline: (t) => `<u>${t}</u>` });
const ansi = themePainter({ fg: (c, t) => `\x1b[38;5;${c.length}m${t}\x1b[39m`, bg: (c, t) => `\x1b[48;5;${c.length}m${t}\x1b[49m`, bold: (t) => `\x1b[1m${t}\x1b[22m`, underline: (t) => `\x1b[4m${t}\x1b[24m` });
const ASCII_ONLY = /^[\x20-\x7e]*$/;

function assertExact(lines: string[], width: number, label: string, ascii = false): void {
	for (const line of lines) {
		assert.equal(visibleWidth(line), width, `${label} @${width}: ${JSON.stringify(stripAnsi(line))}`);
		if (ascii) assert.match(stripAnsi(line), ASCII_ONLY, `${label} ascii @${width}`);
	}
}

test("pane header: `<name> · <n>/<m>` bold accent; ASCII uses `.`; no count without rows", () => {
	assert.equal(renderPaneHeader({ width: 22, name: "Appearance", index: 1, total: 4, paint }), "Appearance · 1/4      ");
	assert.equal(renderPaneHeader({ width: 22, name: "Appearance", index: 1, total: 4, paint: tagged }), "<b><accent>Appearance · 1/4</accent></b>      ");
	assert.equal(renderPaneHeader({ width: 22, name: "Keys", index: 7, total: 7, ascii: true, paint }), "Keys . 7/7            ");
	assert.equal(renderPaneHeader({ width: 10, name: "Keys", index: 0, total: 0, paint }), "Keys      ");
});

test("choice row: label, then the options inline as radios, ● chosen in accent, ○ others dim; the cursor row on the tint", () => {
	const row = renderChoiceRow({ width: 50, label: "Animations", labelWidth: 14, options: ["full", "off"], value: "full", paint });
	assert.equal(row, `  Animations      ● full  ○ off${" ".repeat(19)}`);
	const painted = renderChoiceRow({ width: 40, label: "Tool icons", labelWidth: 12, options: ["geometric", "emoji"], value: "emoji", paint: tagged });
	assert.match(painted, /<text>Tool icons<\/text>.*<dim>○ geometric<\/dim>  <b><accent>● emoji<\/accent><\/b>/);
	const cursor = renderChoiceRow({ width: 40, label: "Sound", labelWidth: 6, options: ["on", "off"], value: "off", cursor: true, paint: tagged });
	assert.match(cursor, /^\[selectedBg\]<accent>▸<\/accent> /, "the cursor row is tinted and marked");
	assert.match(cursor, /<muted>○ on<\/muted>/, "on the tint the others are soft, not dim (L72)");
	assert.equal(renderChoiceRow({ width: 40, label: "Sound", labelWidth: 6, options: ["on", "off"], value: "on", ascii: true, paint }), `  Sound   * on  o off${" ".repeat(19)}`);
	// A pending pick (space) is underlined until enter applies it.
	assert.match(renderChoiceRow({ width: 40, label: "Sound", labelWidth: 6, options: ["on", "off"], value: "on", pending: "off", paint: tagged }), /<dim>○ <\/dim><u><text>off<\/text><\/u>/);
	// A display name for each option; a value outside the options leads as the chosen one.
	assert.equal(renderChoiceRow({ width: 44, label: "Sound", labelWidth: 6, options: ["none", "builtin:error"], value: "/x.wav", display: (o) => o.replace(/^builtin:/, ""), paint }).trimEnd(), "  Sound   ● /x.wav  ○ none  ○ error");
});

test("choice row: a free value shows as the value; narrow, the unchosen options drop from the end with …, the chosen stays", () => {
	assert.equal(renderChoiceRow({ width: 30, label: "Compact at (%)", labelWidth: 16, value: "60", paint }), `  Compact at (%)    60${" ".repeat(8)}`);
	assert.equal(renderChoiceRow({ width: 36, label: "Sound", labelWidth: 6, options: ["none", "success", "error", "attention"], value: "error", paint }), "  Sound   ○ none  ● error  …        ");
	for (const width of WIDTHS) for (const ascii of [false, true]) {
		assertExact([renderChoiceRow({ width, label: ascii ? "Sound - attention" : "Sound · attention", labelWidth: 20, options: ["none", "builtin:success", "builtin:error", "builtin:attention"], value: "builtin:error", cursor: true, ascii, paint: ansi })], width, "choice", ascii);
	}
});

test("preview block: `Preview · <value> · <caption>` bold accent, then the sample rows", () => {
	assert.deepEqual(renderPreview({ width: 40, value: "full", caption: "animated", lines: ["◆ Working…"], paint }), [
		`Preview · full · animated${" ".repeat(15)}`,
		`◆ Working…${" ".repeat(30)}`,
	]);
	assert.match(renderPreview({ width: 40, value: "full", lines: ["x"], paint: tagged })[0]!, /^<b><accent>Preview · full<\/accent><\/b>/);
	for (const width of WIDTHS) for (const ascii of [false, true]) {
		assertExact(renderPreview({ width, value: "geometric", caption: "the tool icons", lines: [ascii ? "r read" : "▤ read  ⌕ search"], ascii, paint: ansi }), width, "preview", ascii);
	}
});

