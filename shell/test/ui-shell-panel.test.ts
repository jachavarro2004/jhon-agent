/**
 * The panel primitives (T15.12b, slice 1): Gentle Shell's lib/shell-panel.ts
 * with Jhon's brand (力) and rose ink's band. Every row is exactly the width
 * asked for at 120/80/60 columns; ASCII prints only ASCII.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-shell-panel.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import {
	PANEL_ROW_INDENT, clipPanelText, fitPanelLine, panelSheetBackHit, renderPanelHeader, renderPanelRow, renderPanelSheet, renderWorkRow, workRowLead,
	type PanelSummary,
	setChromeSurface,
} from "../lib/ui/shell-panel.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";

const WIDTHS = [120, 80, 60];
const plain = { fg: (_role: string, text: string) => text, bold: (text: string) => text };
const tagged = { fg: (role: string, text: string) => `<${role}>${text}</${role}>`, bold: (text: string) => `<b>${text}</b>` };
const banded = { ...plain, bg: (role: string, text: string) => `\x1b[48;5;1m${text}\x1b[49m` };
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

test("workRowLead and renderWorkRow: ` ▸ <lead> … <count>`, the highest priority leads, the todo count named when it is not the lead's", () => {
	const todo: PanelSummary = { left: "◐ Write the tests", right: "2/6", rightLabel: "todos" };
	const agents: PanelSummary = { left: "◐ 1 agent running", priority: 1 };
	assert.equal(workRowLead([]), -1);
	assert.equal(workRowLead([todo, agents]), 1);
	assert.equal(workRowLead([todo, { ...agents, priority: 0 }]), 0, "first on ties");
	assert.equal(renderWorkRow([], plain, 60), undefined, "no live work, no row");
	for (const width of WIDTHS) {
		const row = renderWorkRow([todo, agents], plain, width)!;
		assert.equal(visibleWidth(row), width);
		assert.match(row, /^ ▸ ◐ 1 agent running +2\/6 todos $/);
		assert.match(renderWorkRow([todo], plain, width)!, /^ ▸ ◐ Write the tests +2\/6 $/);
	}
	assert.match(renderWorkRow([todo], tagged, 200, true)!, /^ <warning>▸<\/warning>/, "hover paints the ▸");
	assert.match(stripAnsi(renderWorkRow([todo], plain, 40, false, true)!), /^ > /);
	assert.ok(!renderWorkRow([todo], banded, 40)!.includes("\x1b[48;5;1m"), "a transparent surface leaves it unpainted (shell.appearance.surface)");
	setChromeSurface("tinted");
	try {
		const painted = renderWorkRow([todo], banded, 40)!;
		assert.ok(painted.startsWith("\x1b[48;5;1m"), "tinted: on the panel surface");
		assert.equal(visibleWidth(painted), 40);
	} finally { setChromeSurface("transparent"); }
});

const part = (lines: string[]) => ({ render: (_width: number) => lines });

test("renderPanelSheet: ` 力 Panel … esc back`, then every section in rail order ldd → todo → jobs → agents → footer with a blank row between, exactly width × rows (L174: LDD first)", () => {
	const parts = new Map([["footer", part(["▾ CHANGES"])], ["agents", part(["▾ AGENTS"])], ["todo", part(["▾ TODOS", "  ◐ one", ""])], ["jobs", part([])], ["ldd", part(["▾ LDD", "  ◆ rail"])]]);
	for (const width of WIDTHS) {
		const sheet = renderPanelSheet(parts, plain, width, { rows: 12, scrollTop: 0 });
		assert.equal(sheet.lines.length, 12);
		for (const line of sheet.lines) assert.equal(visibleWidth(line), width);
		const text = sheet.lines.map((line) => line.trimEnd());
		assert.match(text[0]!, /^ 力 Panel +esc back$/);
		assert.deepEqual(text.slice(1, 11), ["", " ▾ LDD", "   ◆ rail", "", " ▾ TODOS", "   ◐ one", "", " ▾ AGENTS", "", " ▾ CHANGES"]);
		assert.equal(sheet.maxScroll, 0);
	}
	const ascii = renderPanelSheet(parts, plain, 60, { rows: 12, scrollTop: 0, ascii: true });
	assert.match(ascii.lines[0]!, /^ jhon Panel +esc back $/);
	assert.ok(ascii.lines.every((line) => ASCII_ONLY.test(line)), "ASCII prints only ASCII");
});

test("renderPanelSheet scrolls: `↓ N more · scroll` while rows hide below, the last page without it", () => {
	const parts = new Map([["todo", part(Array.from({ length: 20 }, (_, i) => `task ${i}`))]]);
	const top = renderPanelSheet(parts, plain, 40, { rows: 6, scrollTop: 0 });
	assert.equal(top.maxScroll, 16);
	assert.deepEqual(top.lines.map((line) => line.trimEnd()).slice(1), ["", " task 0", " task 1", " task 2", " ↓ 17 more · scroll"]);
	const end = renderPanelSheet(parts, plain, 40, { rows: 6, scrollTop: 99 });
	assert.equal(end.lines.at(-1)!.trimEnd(), " task 19");
	assert.match(renderPanelSheet(parts, tagged, 40, { rows: 6, scrollTop: 0, backHovered: true }).lines[0]!, /<warning>esc back<\/warning>/);
});

test("panelSheetBackHit: only row 0, on ` esc back ` at the right end", () => {
	assert.equal(panelSheetBackHit(60, 59, 0), true);
	assert.equal(panelSheetBackHit(60, 50, 0), true);
	assert.equal(panelSheetBackHit(60, 49, 0), false);
	assert.equal(panelSheetBackHit(60, 55, 1), false);
});
