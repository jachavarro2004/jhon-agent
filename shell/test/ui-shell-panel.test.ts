/**
 * The right pane's section chrome (lib/ui/shell-panel.ts): the header row
 * and the indented rows under it. Every row is exactly the width asked for at
 * 120/80/60 columns; ASCII prints only ASCII.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-shell-panel.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import {
	PANEL_ROW_INDENT, RAIL_ORDER, clipPanelText, fitPanelLine, renderPanelHeader, renderPanelRow,
} from "../lib/ui/shell-panel.ts";

const WIDTHS = [120, 80, 60];
const plain = { fg: (_role: string, text: string) => text, bold: (text: string) => text };
const tagged = { fg: (role: string, text: string) => `<${role}>${text}</${role}>`, bold: (text: string) => `<b>${text}</b>` };
const ASCII_ONLY = /^[\x20-\x7e]*$/;

test("fitPanelLine: left and right on one row exactly the width; left gives way with an ellipsis, right drops when it cannot fit", () => {
	for (const width of WIDTHS) {
		assert.equal(visibleWidth(fitPanelLine("left", "right", width)), width);
		assert.equal(fitPanelLine("left", "right", width), `left${" ".repeat(width - 9)}right`);
	}
	assert.equal(fitPanelLine("abcdefghij", "12", 8), "abcd… 12");
	assert.equal(fitPanelLine("ab", "toolong", 5), "ab   ");
	assert.equal(clipPanelText("\x1b[1mabcdef\x1b[22m", 4), "\x1b[1mabc…", "SGR codes are kept, never split");
	assert.equal(clipPanelText("abc", 0), "");
});

test("renderPanelHeader: `▾ NAME … count`, the name UPPERCASE text bold, the count muted or its state role; folded ▸; hover paints glyph and name in the hover role", () => {
	for (const width of WIDTHS) {
		const line = renderPanelHeader(plain, width, { title: "Changes", count: "2 files", open: true });
		assert.equal(visibleWidth(line), width);
		assert.match(line, /^▾ CHANGES +2 files$/);
	}
	assert.match(renderPanelHeader(tagged, 200, { title: "Ldd", count: "needs you", countRole: "warning", open: true }),
		/^<muted>▾<\/muted> <text><b>LDD<\/b><\/text> +<warning>needs you<\/warning>$/);
	assert.match(renderPanelHeader(plain, 30, { title: "Jobs", open: false }), /^▸ JOBS +$/);
	assert.match(renderPanelHeader(tagged, 200, { title: "Jobs", open: true, hovered: true }), /^<warning>▾<\/warning> <warning><b>JOBS<\/b><\/warning>/);
	const ascii = renderPanelHeader(plain, 30, { title: "Jobs", count: "1", open: false, ascii: true });
	assert.match(ascii, /^> JOBS +1$/);
	assert.match(ascii, ASCII_ONLY);
});

test("renderPanelRow: a row indented under its header with an optional right column", () => {
	for (const width of WIDTHS) {
		const row = renderPanelRow(width, "+3 −1", "/jhon:changes");
		assert.equal(visibleWidth(row), width);
		assert.ok(row.startsWith(" ".repeat(PANEL_ROW_INDENT) + "+3 −1"));
		assert.ok(row.endsWith("/jhon:changes"));
	}
});

// T169 (L1437, L1438): the panel sheet is gone, and with it the rail's STATS
// card; the pane's parts, in order, are LDD first (L174), then the todo list
// (while no change shows), Agents and Jobs (design 01, T174-S5).
test("the pane's order is ldd → todo → agents → jobs, with no stats (T169) and no footer slot (T174-S4)", () => {
	assert.deepEqual([...RAIL_ORDER], ["ldd", "todo", "agents", "jobs"]);
});
