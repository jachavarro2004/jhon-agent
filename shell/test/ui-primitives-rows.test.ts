/**
 * The base primitives, part two (T15.1, design/system.md "The shell's design
 * system"): the state table, the state block, the list row, the key hints
 * and the tool icons. Every line is exactly the width asked for, ASCII mode
 * prints only ASCII.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-primitives-rows.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import {
	STATES, TOOL_ICONS, fitHints, plainPainter, renderListRow, renderStateBlock,
	stateGlyph, themePainter, toolGlyph, type StateName,
} from "../lib/ui/primitives/index.ts";
import { THEME_NAME } from "../lib/ui/primitives/paint.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";

const WIDTHS = [120, 80, 60];
const paint = plainPainter();
const ansi = themePainter({ fg: (c, t) => `\x1b[38;5;${c.length}m${t}\x1b[39m`, bg: (c, t) => `\x1b[48;5;${c.length}m${t}\x1b[49m`, bold: (t) => `\x1b[1m${t}\x1b[22m`, underline: (t) => `\x1b[4m${t}\x1b[24m` });
const tagged = themePainter({ fg: (c, t) => `<${c}>${t}</${c}>`, bg: (c, t) => `[${c}]${t}[/${c}]`, bold: (t) => `<b>${t}</b>`, underline: (t) => `<u>${t}</u>` });
const ASCII_ONLY = /^[\x20-\x7e]*$/;

function assertExact(lines: string[], width: number, label: string, ascii = false): void {
	for (const line of lines) {
		assert.equal(visibleWidth(line), width, `${label} @${width}: ${JSON.stringify(stripAnsi(line))}`);
		if (ascii) assert.match(stripAnsi(line), ASCII_ONLY, `${label} ascii @${width}`);
	}
}

test("state table: ✓ · ◉ ● ◐ ✗ ? with their roles and ASCII + . ! * ~ x ?", () => {
	assert.deepEqual(Object.fromEntries(Object.entries(STATES).map(([name, s]) => [name, [s.glyph, s.ascii, s.role, s.word]])), {
		done: ["✓", "+", "ok", "done"], pending: ["·", ".", "dim", "pending"], blocked: ["◉", "!", "bad", "blocked"], new: ["●", "*", "accent", "new"],
		working: ["◐", "~", "warn", "working"], failed: ["✗", "x", "bad", "failed"], unknown: ["?", "?", "dim", "unknown"],
	});
	for (const name of Object.keys(STATES) as StateName[]) assert.deepEqual([stateGlyph(name), stateGlyph(name, true)], [STATES[name].glyph, STATES[name].ascii]);
});

test("the state table's roles read as these pi theme names", () => {
	assert.equal(THEME_NAME[STATES.new.role], "accent");
	assert.equal(THEME_NAME[STATES.blocked.role], "error");
	assert.equal(THEME_NAME[STATES.working.role], "warning");
	assert.equal(THEME_NAME[STATES.done.role], "success");
});

test("state block: loading, empty and error say the cause, the next step, and only the keys that act", () => {
	const loading = renderStateBlock({ width: 40, kind: "loading", text: "Reading the change…", elapsed: 3, paint });
	assert.deepEqual(loading.lines.map((l) => l.trimEnd()), ["◐ Reading the change…  3s"]);
	assert.deepEqual(loading.keys, [["esc", "close"]]);
	const empty = renderStateBlock({ width: 40, kind: "empty", text: "No change yet.", next: [["n", "new change"]], paint });
	assert.deepEqual(empty.lines.map((l) => l.trimEnd()), ["No change yet.", "n new change"]);
	assert.deepEqual(empty.keys, [["n", "new change"], ["esc", "close"]]);
	const error = renderStateBlock({ width: 30, kind: "error", text: "Could not read the change", cause: "jhon status: exit 2: the change directory odd/changes/x is missing", next: [["r", "retry"]], paint });
	assert.deepEqual(error.lines.map((l) => l.trimEnd()), [
		"✗ Could not read the change", "  jhon status: exit 2: the", "  change directory", "  odd/changes/x is missing", "r retry",
	]);
	const painted = renderStateBlock({ width: 40, kind: "error", text: "Failed", paint: tagged });
	assert.match(painted.lines[0]!, /^<b><error>✗ Failed<\/error><\/b>/);
	for (const width of WIDTHS) for (const ascii of [false, true]) for (const kind of ["loading", "empty", "error"] as const) {
		const block = renderStateBlock({ width, kind, text: "Could not read the change", cause: "x ".repeat(90), next: [["r", "retry"]], elapsed: 2, ascii, paint: ansi });
		assertExact(block.lines, width, `state block ${kind}`, ascii);
	}
});

test("list row: cursor ▸ in accent and the tint, the mark slot, right columns soft on the cursor row and dim elsewhere", () => {
	assert.deepEqual(renderListRow({ width: 40, text: "v0-1-release", mark: "done", right: ["3/6", "2m"], paint }), [`    ✓ v0-1-release${" ".repeat(17)}3/6  2m`.slice(2)]);
	assert.deepEqual(renderListRow({ width: 40, text: "v0-1-release", mark: "done", right: ["2m"], cursor: true, paint }), [`▸ ✓ v0-1-release${" ".repeat(22)}2m`]);
	const [focused] = renderListRow({ width: 30, text: "a", right: ["2m"], cursor: true, paint: tagged });
	assert.match(focused!, /^\[selectedBg\]<accent>▸<\/accent> .*<muted>2m<\/muted>\[\/selectedBg\]$/);
	const [rest] = renderListRow({ width: 30, text: "a", right: ["2m"], paint: tagged });
	assert.match(rest!, /<dim>2m<\/dim>$/);
});

test("list row: right columns give way from the first when the text would get under 16 cells; the last column stays longest", () => {
	const [row] = renderListRow({ width: 30, text: "a fairly long task name", right: ["opus", "12k", "4m"], paint });
	assert.equal(row, "    a fairly long ta…  12k  4m");
	assert.equal(row!.length, 30);
});

test("list row: wrapped text hangs at the text column", () => {
	const rows = renderListRow({ width: 24, text: "a risk that is long enough to wrap twice here", mark: "blocked", wrap: true, paint });
	assert.deepEqual(rows.map((r) => r.trimEnd()), ["  ◉ a risk that is long", "    enough to wrap twice", "    here"]);
	for (const width of WIDTHS) for (const ascii of [false, true]) for (const cursor of [false, true]) {
		const lines = renderListRow({ width, text: "word ".repeat(40), mark: "working", right: ["subagent", "12k tokens", "4m 12s"], cursor, wrap: true, ascii, paint: ansi });
		assertExact(lines, width, `list row cursor=${cursor}`, ascii);
	}
});

const KEYS: Array<[string, string]> = [["↑↓", "choose"], ["enter", "open"], ["tab", "next tab"], ["r", "refresh"], ["e", "edit"], ["esc", "close"]];

// The state block's next-step line fits its hints so (fitHints).
test("key hints: drop the lowest priority first, never esc close, never a cut hint", () => {
	const shown = (hints: Parameters<typeof fitHints>[0], width: number) => fitHints(hints, width).map(([key, action]) => `${key} ${action}`).join("  ");
	assert.equal(shown(KEYS, 200), "↑↓ choose  enter open  tab next tab  r refresh  e edit  esc close");
	assert.equal(shown(KEYS, 30), "↑↓ choose  esc close");
	assert.equal(shown(KEYS, 9), "esc close");
	assert.equal(shown(KEYS, 8), "", "esc close is never cut with …");
	const ranked = [{ key: "a", action: "low", priority: 1 }, { key: "b", action: "high", priority: 9 }, { key: "c", action: "mid", priority: 5 }];
	assert.equal(shown(ranked, 12), "b high");
	assert.equal(shown(ranked, 13), "b high  c mid");
});

test("borders paint with pi's border (rose ink's frameSoft, T15.5) and the rail band with customMessageBg (rose ink's railBand)", async () => {
	const { THEME_NAME } = await import("../lib/ui/primitives/index.ts");
	const { PANEL_SURFACE_ROLE } = await import("../lib/ui/shell-panel.ts");
	assert.equal(THEME_NAME.frameSoft, "border");
	assert.equal(PANEL_SURFACE_ROLE, "customMessageBg");
});

test("tool icons: geometric by default, emoji as an option, ASCII letters", () => {
	const families = ["read", "search", "web", "shell", "edit", "memory", "agents", "ask", "other"] as const;
	assert.deepEqual(families.map((f) => toolGlyph(f)), ["▤", "⌕", "◍", "$", "✎", "▢", "◇", "?", "·"]);
	assert.deepEqual(families.map((f) => toolGlyph(f, { style: "emoji" })), ["📄", "🔎", "🌐", "$", "✎", "🧠", "🤖", "❓", "·"]);
	assert.deepEqual(families.map((f) => toolGlyph(f, { ascii: true, style: "emoji" })), ["r", "s", "w", "$", "e", "m", "a", "?", "-"]);
	for (const family of families) assert.equal(visibleWidth(TOOL_ICONS[family].geometric), 1, family);
});
