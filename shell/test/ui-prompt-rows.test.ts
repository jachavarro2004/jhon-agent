/**
 * The prompt and the working line, row by row (T174-S8; design/shell-tui 01):
 *
 *    力 Working…  thinking · 38s                         esc esc interrupts
 *   ──────────────────────────────────────────────────────────────────────
 *    ❯ █ write, or / for a command
 *   ──────────────────────────────────────────────────────────────────────
 *
 * The working line sits over the rule in every state (idle it says "ready for
 * you"), so the prompt keeps its height when a turn starts and ends. Its right
 * end says how esc stops the turn, or the esc confirmation while it is open.
 * Rows are compared exactly at 180 and 100 columns (test/rows.ts).
 *
 *   cd shell && node --experimental-strip-types --test test/ui-prompt-rows.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { JhonPromptEditor } from "../extensions/ui.ts";
import * as shellPrompt from "../lib/ui/shell-prompt.ts";
import { PROMPT_STATE } from "../lib/ui/shell-prompt.ts";
import { assertRows, padded, plainTheme, spread, type PlainTheme } from "./rows.ts";

const identity = (text: string) => text;
const selectList = { selectedPrefix: identity, selectedText: identity, description: identity, scrollInfo: identity, noMatch: identity };
const ESC = "\x1b";
const escKeys = { matches: (data: string, action: string) => action === "app.interrupt" && data === ESC };
/** pi-tui's cursor markers (APC sequences) carry no cell; test/rows.ts strips CSI only. */
const noApc = (row: string) => row.replace(/\x1b_[^\x07]*\x07/g, "");

interface Setup {
	doubleEsc?: boolean;
	pending?: boolean;
	activity?: string;
	wait?: string;
	ascii?: boolean;
	topRows?: (width: number) => string[];
}

function prompt(setup: Setup = {}) {
	const clock = { now: 0 };
	const editor = new JhonPromptEditor({ terminal: { rows: 30, columns: 180 }, requestRender() {} } as never, { borderColor: identity, selectList } as never, escKeys as never, {
		fg: (_role, text) => text,
		bold: identity,
		italic: identity,
		requestRender() {},
		pending: () => setup.pending ?? false,
		now: () => clock.now,
		doubleEscCancelEnabled: () => setup.doubleEsc ?? true,
		dispatchQueuedText() {},
		schedule: () => () => {},
		activity: () => setup.activity,
		workingLabel: () => setup.wait,
		ascii: () => setup.ascii ?? false,
		...(setup.topRows ? { topRows: setup.topRows } : {}),
	});
	editor.setAnimationPolicy("potato");
	const render = (width: number, _theme: PlainTheme) => editor.render(width).map(noApc);
	return { editor, clock, render };
}

const rule = (width: number, char = "─") => char.repeat(width);
const empty = (width: number, mark = "❯") => padded(` ${mark}   write, or / for a command`, width);

test("idle: the working line says ready for you over the rule, then ❯ and the placeholder between two rules", () => {
	const { editor, render } = prompt();
	try {
		assertRows(render, (width) => [padded(" 力 ready for you", width), rule(width), empty(width), rule(width)]);
		editor.setText("hola");
		assertRows(render, (width) => [padded(" 力 ready for you", width), rule(width), padded(" ❯ hola ", width), rule(width)], "the placeholder leaves with the first character");
	} finally { editor.dispose(); }
});

test("working: 力 Working…, what the agent does and for how long, and how esc stops it at the right end (design 01)", () => {
	const { editor, clock, render } = prompt();
	try {
		editor.setWorking(true);
		clock.now = 38_000;
		assertRows(render, (width) => [spread(" 力 Working…  thinking · 38s", "esc esc interrupts ", width), rule(width), empty(width), rule(width)]);
		clock.now = 130_000;
		assertRows(render, (width) => [spread(" 力 Working…  thinking · 2m10s", "esc esc interrupts ", width), rule(width), empty(width), rule(width)], "past a minute");
	} finally { editor.dispose(); }
});

test("working with double-esc off: one esc interrupts", () => {
	const { editor, clock, render } = prompt({ doubleEsc: false });
	try {
		editor.setWorking(true);
		clock.now = 5_000;
		assertRows(render, (width) => [spread(" 力 Working…  thinking · 5s", "esc interrupts ", width), rule(width), empty(width), rule(width)]);
	} finally { editor.dispose(); }
});

test("the working line names the running tool, the queued message and the wait on the Claude API", () => {
	const tool = prompt({ activity: "bash" });
	try {
		tool.editor.setWorking(true);
		tool.clock.now = 12_000;
		assertRows(tool.render, (width) => [spread(" 力 Working…  bash · 12s", "esc esc interrupts ", width), rule(width), empty(width), rule(width)]);
	} finally { tool.editor.dispose(); }
	const queued = prompt({ pending: true });
	try {
		queued.editor.setWorking(true);
		queued.clock.now = 38_000;
		assertRows(queued.render, (width) => [spread(" 力 Working…  thinking · 38s · queued", "esc esc interrupts ", width), rule(width), empty(width), rule(width)]);
	} finally { queued.editor.dispose(); }
	const wait = prompt({ wait: "waiting for Claude API · 72s" });
	try {
		wait.editor.setWorking(true);
		wait.clock.now = 80_000;
		assertRows(wait.render, (width) => [spread(" 力 Working…  waiting for Claude API · 72s", "esc esc interrupts ", width), rule(width), empty(width), rule(width)]);
	} finally { wait.editor.dispose(); }
});

test("the first esc while working says esc again stops it at the right end; idle with a draft, esc again clears it", () => {
	const { editor, clock, render } = prompt();
	try {
		editor.setWorking(true);
		clock.now = 38_000;
		editor.handleInput(ESC);
		assertRows(render, (width) => [spread(" 力 Working…  thinking · 38s", "esc again stops it ", width), rule(width), empty(width), rule(width)]);
		editor.setWorking(false);
		editor.setText("draft");
		editor.handleInput(ESC);
		assertRows(render, (width) => [spread(" 力 ready for you", "esc again clears it ", width), rule(width), padded(" ❯ draft ", width), rule(width)]);
	} finally { editor.dispose(); }
});

test("under shell.appearance.ascii the line, the mark and the rules read in ASCII", () => {
	const { editor, clock, render } = prompt({ ascii: true });
	try {
		assertRows(render, (width) => [padded(" * ready for you", width), rule(width, "-"), empty(width, ">"), rule(width, "-")]);
		editor.setWorking(true);
		clock.now = 38_000;
		assertRows(render, (width) => [spread(" * Working...  thinking - 38s", "esc esc interrupts ", width), rule(width, "-"), empty(width, ">"), rule(width, "-")]);
	} finally { editor.dispose(); }
});

test("pi's scroll indicators ride the rules' right ends; continuation rows keep the input's column", () => {
	const { editor, render } = prompt();
	try {
		editor.setText(Array.from({ length: 12 }, (_, i) => `line ${i + 1}`).join("\n"));
		const rows = render(100, plainTheme);
		assert.equal(rows[1], `${rule(89)} ↑ 3 more ─`);
		assert.equal(rows[2], padded(" ❯ line 4", 100));
		assert.equal(rows[3], padded("   line 5", 100));
		assert.equal(rows.at(-1), rule(100));
		for (const row of rows) assert.equal(visibleWidth(row), 100);
	} finally { editor.dispose(); }
});

test("a selection's count rides the bottom rule (selection-engine.ts decorates it)", () => {
	const { editor, render } = prompt();
	try {
		editor.setText("hello world");
		editor.handleInput("\x1b[F");
		editor.handleInput("\x1b[1;2H");
		const label = " 11 chars selected, Del deletes them, alt+a selects all ";
		assertRows(render, (width) => [padded(" 力 ready for you", width), rule(width), padded(" ❯ hello world ", width), `${rule(width - visibleWidth(label) - 1)}${label}─`]);
	} finally { editor.dispose(); }
});

test("rows the shell draws above (the narrow form's Running block) sit over the working line; a click past ❯ places the cursor", () => {
	const { editor, render } = prompt({ topRows: (width) => [padded("── Running", width)] });
	try {
		editor.setText("hello");
		assertRows(render, (width) => [padded("── Running", width), padded(" 力 ready for you", width), rule(width), padded(" ❯ hello ", width), rule(width)]);
		editor.render(100);
		editor.handleMouse({ type: "click", button: "left", x: 5, y: 3, width: 100, height: 5, screenX: 5, screenY: 3, shift: false, alt: false, ctrl: false } as never);
		editor.handleInput("X");
		assert.equal(editor.getText(), "heXllo");
	} finally { editor.dispose(); }
});

test("renderWorkingLine is exactly the width; the right end gives way first, then the words are cut", () => {
	const line = (width: number) => shellPrompt.renderWorkingLine(width, { state: PROMPT_STATE.WORKING, tick: 0, elapsedMs: 38_000, doubleEsc: true }, plainTheme);
	assert.equal(line(60), spread(" 力 Working…  thinking · 38s", "esc esc interrupts ", 60));
	assert.equal(line(40), padded(" 力 Working…  thinking · 38s", 40));
	for (const width of [0, 1, 5, 12, 20]) assert.equal(visibleWidth(line(width)), width, `${width}`);
});
