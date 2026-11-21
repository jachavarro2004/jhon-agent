/**
 * The prompt's text selection (T174-S9), through the prompt's own surface:
 * keys in, rows and text out.
 *
 *    力 ready for you
 *   ──────────────────────────────────────────────────────────────────────
 *    ❯ hello world                                    (the selection inverted)
 *   ───────────── 11 chars selected, Del deletes them, alt+a selects all ─
 *
 * shift+home and shift+end select to the line's start or end, alt+a and alt+e
 * select everything; Del, backspace, a typed character or a paste replace the
 * selection in one undo step; any other key drops it first. Dragging the
 * mouse and copying belong to pi's full-screen renderer, not to the prompt.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-prompt-selection.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { CURSOR_MARKER, matchesKey, visibleWidth } from "@earendil-works/pi-tui";
import { JhonPromptEditor } from "../extensions/ui.ts";
import { stripAnsi } from "../lib/ui/primitives/index.ts";
import { assertRows, padded, plainTheme, type PlainTheme } from "./rows.ts";

const identity = (text: string) => text;
const selectList = { selectedPrefix: identity, selectedText: identity, description: identity, scrollInfo: identity, noMatch: identity };
const ESC = "\x1b";
const CTRL_V = "\x16";
const keys = {
	matches: (data: string, action: string) =>
		(action === "app.interrupt" && data === ESC) || (action === "app.clipboard.pasteImage" && data === CTRL_V),
};
/** pi-tui's cursor markers (APC sequences) carry no cell; test/rows.ts strips CSI only. */
const noApc = (row: string) => row.replace(/\x1b_[^\x07]*\x07/g, "");

/** shift+home and shift+end, as xterm sends them. */
const [SHIFT_HOME, SHIFT_END] = ["\x1b[1;2H", "\x1b[1;2F"];
const HOME = "\x1b[H";
const END = "\x1b[F";
const LEFT = "\x1b[D";
const UP = "\x1b[A";
const DOWN = "\x1b[B";
const RIGHT = "\x1b[C";
const DEL = "\x1b[3~";
const BACKSPACE = "\x7f";
const ALT_A = "\x1ba";
const ALT_E = "\x1be";
const UNDO = "\x1f"; // ctrl+-, pi's undo
/** Bracketed paste's opening and closing marks. */
const [PASTE_START, PASTE_END] = ["\x1b[200~", "\x1b[201~"];
const MIB_16 = 16 * 1024 * 1024;

function prompt(setup: { ascii?: boolean; doubleEsc?: boolean } = {}) {
	const editor = new JhonPromptEditor({ terminal: { rows: 30, columns: 180 }, requestRender() {} } as never, { borderColor: identity, selectList } as never, keys as never, {
		fg: (_role, text) => text,
		bold: identity,
		italic: identity,
		requestRender() {},
		pending: () => false,
		now: () => 0,
		doubleEscCancelEnabled: () => setup.doubleEsc ?? true,
		dispatchQueuedText() {},
		schedule: () => () => {},
		ascii: () => setup.ascii ?? false,
	});
	editor.setAnimationPolicy("potato");
	editor.focused = true;
	const render = (width: number, _theme?: PlainTheme) => editor.render(width).map(noApc);
	const plain = (width: number) => render(width).map(stripAnsi);
	const type = (...inputs: string[]) => { for (const input of inputs) editor.handleInput(input); };
	return { editor, render, plain, type };
}

const rule = (width: number, glyph = "─") => glyph.repeat(width);
const label = (n: number) => ` ${n} char${n === 1 ? "" : "s"} selected, Del deletes them, alt+a selects all `;
/** The bottom rule with the selection's count at its right end. */
const hinted = (width: number, n: number, glyph = "─") => `${rule(width - visibleWidth(label(n)) - 1, glyph)}${label(n)}${glyph}`;
const hintOf = (rows: string[]) => /(\d+) chars? selected/.exec(rows.at(-1) ?? "")?.[1];

// --- the keys that select -------------------------------------------------------------

/** A completion provider of slash commands: choosing one makes it the whole draft. */
function slashCommands(...names: string[]) {
	return {
		getSuggestions: async () => ({ prefix: "/", items: names.map((name) => ({ value: `/${name}`, label: name })) }),
		applyCompletion: (_lines: string[], _line: number, _col: number, item: { value: string }) => ({ lines: [item.value], cursorLine: 0, cursorCol: item.value.length }),
	};
}

test("shift+home selects back to the line's start: inverted, counted on the bottom rule, at 180 and 100 columns", () => {
	const { editor, render, type } = prompt();
	try {
		editor.setText("hello world");
		type(SHIFT_HOME);
		assertRows(render, (width) => [padded(" 力 ready for you", width), rule(width), padded(" ❯ hello world", width), hinted(width, 11)]);
		assert.deepEqual(editor.getCursor(), { line: 0, col: 0 });
		assert.ok(render(100)[2]!.includes("\x1b[7m"), "the selected cells are inverted");
		type(DEL);
		assert.equal(editor.getText(), "");
		assert.equal(hintOf(render(100).map(stripAnsi)), undefined, "no count once the selection is gone");
	} finally { editor.dispose(); }
});

test("shift+end selects forward to the line's end; backspace deletes it", () => {
	const { editor, plain, type } = prompt();
	try {
		editor.setText("hello world");
		type(HOME, RIGHT, RIGHT, RIGHT, RIGHT, RIGHT, RIGHT, SHIFT_END);
		assert.equal(hintOf(plain(100)), "5");
		type(BACKSPACE);
		assert.equal(editor.getText(), "hello ");
		assert.deepEqual(editor.getCursor(), { line: 0, col: 6 });
	} finally { editor.dispose(); }
});

test("the anchor stays where the selection began: shift+end then shift+home turns it around", () => {
	const { editor, plain, type } = prompt();
	try {
		editor.setText("hello world");
		type(HOME, RIGHT, RIGHT, RIGHT, RIGHT, RIGHT, RIGHT, SHIFT_END);
		assert.equal(hintOf(plain(100)), "5");
		type(SHIFT_HOME);
		assert.equal(hintOf(plain(100)), "6");
		type("X");
		assert.equal(editor.getText(), "Xworld");
	} finally { editor.dispose(); }
});

test("a held shift+home at the line's start keeps the selection; one that selects nothing selects nothing", () => {
	const held = prompt();
	try {
		held.editor.setText("hello");
		held.type(SHIFT_HOME, SHIFT_HOME, SHIFT_HOME);
		assert.equal(hintOf(held.plain(100)), "5", "a terminal repeats the held key byte for byte");
	} finally { held.editor.dispose(); }
	const { editor, plain, type } = prompt();
	try {
		editor.setText("hi");
		type(SHIFT_END);
		assert.equal(hintOf(plain(100)), undefined, "shift+end at the end selects nothing");
		type(DEL);
		assert.equal(editor.getText(), "hi", "so Del does what it does at the end: nothing");
		type(HOME, SHIFT_HOME, DEL);
		assert.equal(editor.getText(), "i", "and at the start Del deletes forward as always");
	} finally { editor.dispose(); }
});

test("alt+a and alt+e (and kitty's alt+a) select every line; a typed character replaces them all", () => {
	for (const chord of [ALT_A, ALT_E, "\x1b[97;3u"]) {
		const { editor, render, plain, type } = prompt();
		try {
			editor.setText("abc\ndef");
			type(HOME, UP, chord);
			assert.equal(hintOf(plain(100)), "7", `${JSON.stringify(chord)}: the line break counts as one`);
			assert.deepEqual(editor.getCursor(), { line: 1, col: 3 }, "the caret goes to the end");
			const rows = render(40);
			assert.ok(rows[2]!.includes("\x1b[7mabc\x1b[27m") && rows[3]!.includes("\x1b[7mdef\x1b[27m"), "both lines inverted");
			type("x");
			assert.equal(editor.getText(), "x");
		} finally { editor.dispose(); }
	}
});

test("the count says 1 char for one; an empty prompt selects nothing and types as usual", () => {
	const { editor, plain, type } = prompt();
	try {
		editor.setText("x");
		type(ALT_A);
		assert.equal(plain(100).at(-1), hinted(100, 1));
		editor.setText("");
		type(ALT_A);
		assert.equal(plain(100).at(-1), rule(100));
		type("y");
		assert.equal(editor.getText(), "y");
	} finally { editor.dispose(); }
});

test("shift with the arrows or the word moves selects nothing (pi binds none of them)", () => {
	for (const key of ["\x1b[1;2D", "\x1b[1;2C", "\x1b[1;2A", "\x1b[1;2B", "\x1b[1;6D", "\x1b[1;4D"]) {
		const { editor, plain, type } = prompt();
		try {
			editor.setText("hello world");
			type(key);
			assert.equal(hintOf(plain(100)), undefined, JSON.stringify(key));
			assert.equal(editor.getText(), "hello world");
			type(ALT_A, key);
			assert.equal(hintOf(plain(100)), undefined, `${JSON.stringify(key)} drops a selection`);
			assert.equal(editor.getText(), "hello world");
		} finally { editor.dispose(); }
	}
});

// --- what replaces a selection ------------------------------------------------------------

test("Del, backspace and their shift forms delete the selection", () => {
	for (const key of [DEL, "\x1b[3;2~", BACKSPACE, "\x1b[127;2u"]) {
		const { editor, type } = prompt();
		try {
			editor.setText("hello");
			type(ALT_A, key);
			assert.equal(editor.getText(), "", JSON.stringify(key));
			assert.deepEqual(editor.getCursor(), { line: 0, col: 0 });
		} finally { editor.dispose(); }
	}
});

test("a typed character replaces the selection, in every encoding a terminal sends", () => {
	const cases: Array<[string, string]> = [
		["x", "x"], ["é", "é"],
		["\x1b[97u", "a"], ["\x1b[97;1:1u", "a"], // kitty
		["\x1b[27;1;97~", "a"], ["\x1b[27;2;65~", "A"], // modifyOtherKeys, shift too
	];
	for (const [data, text] of cases) {
		const { editor, type } = prompt();
		try {
			editor.setText("hello");
			type(ALT_A, data);
			assert.equal(editor.getText(), text, JSON.stringify(data));
			assert.deepEqual(editor.getCursor(), { line: 0, col: text.length });
		} finally { editor.dispose(); }
	}
});

test("a DEL or C1 code point sent as a key deletes the selection and inserts nothing", () => {
	for (const data of ["\x1b[27;1;127~", "\x1b[27;1;155~"]) {
		const { editor, type } = prompt();
		try {
			editor.setText("hi");
			type(SHIFT_HOME, data);
			assert.equal(editor.getText(), "", JSON.stringify(data));
		} finally { editor.dispose(); }
	}
});

test("a ctrl chord is no character: the selection drops and pi does the chord", () => {
	const { editor, plain, type } = prompt();
	try {
		editor.setText("hello");
		type(ALT_A, "\x1b[27;5;97~"); // ctrl+a, modifyOtherKeys: pi's line start
		assert.equal(editor.getText(), "hello");
		assert.deepEqual(editor.getCursor(), { line: 0, col: 0 });
		assert.equal(hintOf(plain(100)), undefined);
	} finally { editor.dispose(); }
});

test("a selection over lines is replaced in one undo step that brings back the text and the caret", () => {
	const { editor, type } = prompt();
	try {
		editor.setText("prefix old\ntext suffix");
		type(HOME, SHIFT_END, "Z");
		assert.equal(editor.getText(), "prefix old\nZ");
		assert.deepEqual(editor.getCursor(), { line: 1, col: 1 });
		type(UNDO);
		assert.equal(editor.getText(), "prefix old\ntext suffix");
		assert.deepEqual(editor.getCursor(), { line: 1, col: 11 });
		editor.setText("one\ntwo\nthree");
		type(ALT_A, "\x1b[27;1;127~");
		assert.equal(editor.getText(), "");
		type(UNDO);
		assert.equal(editor.getText(), "one\ntwo\nthree");
	} finally { editor.dispose(); }
});

test("after a replacement the next typing is an undo step of its own", () => {
	const { editor, type } = prompt();
	try {
		type(..."one two", ALT_A, "x", ..."yz");
		assert.equal(editor.getText(), "xyz");
		type(UNDO);
		assert.equal(editor.getText(), "x");
		type(UNDO);
		assert.equal(editor.getText(), "one two");
	} finally { editor.dispose(); }
});

test("a replacement tells pi the text changed", () => {
	const { editor, type } = prompt();
	try {
		const seen: string[] = [];
		editor.setText("abc");
		editor.onChange = (text) => seen.push(text);
		type(ALT_A, BACKSPACE);
		assert.deepEqual(seen, [""]);
	} finally { editor.dispose(); }
});

// --- paste --------------------------------------------------------------------------

test("a bracketed paste replaces the selection in one undo step; keys after its end still count", () => {
	const { editor, type } = prompt();
	try {
		editor.setText("prefix old\ntext suffix");
		type(HOME, SHIFT_END, `${PASTE_START}new\nlines${PASTE_END}`);
		assert.equal(editor.getText(), "prefix old\nnew\nlines");
		assert.deepEqual(editor.getCursor(), { line: 2, col: 5 });
		type(UNDO);
		assert.equal(editor.getText(), "prefix old\ntext suffix");
		editor.setText("hello");
		type(ALT_A, `${PASTE_START}a${PASTE_END}tail`);
		assert.equal(editor.getText(), "atail");
	} finally { editor.dispose(); }
});

test("a paste split into pieces, its end mark too, still replaces the selection", () => {
	const { editor, type } = prompt();
	try {
		editor.setText("prefix old\ntext suffix");
		type(HOME, SHIFT_END, PASTE_START, "new\nlines", ...PASTE_END);
		assert.equal(editor.getText(), "prefix old\nnew\nlines");
		type(UNDO);
		assert.equal(editor.getText(), "prefix old\ntext suffix");
	} finally { editor.dispose(); }
});

test("a long paste becomes pi's paste marker in place of the selection; an empty one only drops the selection", () => {
	const { editor, plain, type } = prompt();
	try {
		editor.setText("hello");
		type(ALT_A, `${PASTE_START}${"q".repeat(1200)}${PASTE_END}`);
		assert.equal(editor.getText(), "[paste #1 1200 chars]");
		assert.equal(editor.getExpandedText(), "q".repeat(1200));
		type(UNDO);
		assert.equal(editor.getText(), "hello");
		type(ALT_A, PASTE_START + PASTE_END);
		assert.equal(editor.getText(), "hello");
		assert.equal(hintOf(plain(100)), undefined);
	} finally { editor.dispose(); }
});

test("with no selection a paste goes in at the caret, as pi does it", () => {
	const { editor, type } = prompt();
	try {
		editor.setText("hello");
		type(`${PASTE_START}ab${PASTE_END}`);
		assert.equal(editor.getText(), "helloab");
	} finally { editor.dispose(); }
});

test("ctrl+v (pi's paste from the clipboard) drops the selection and inserts at the caret", () => {
	const { editor, type } = prompt();
	try {
		editor.onPasteImage = () => editor.insertTextAtCursor("CLIP");
		editor.setText("hello world");
		type(SHIFT_HOME, CTRL_V);
		assert.equal(editor.getText(), "CLIPhello world");
	} finally { editor.dispose(); }
});

test("an unfinished paste past 16 MiB of UTF-8 is dropped, never typed; the selection stays", () => {
	const { editor, plain, type } = prompt();
	try {
		editor.setText("hello");
		// é: one UTF-16 unit, two UTF-8 bytes: 10 Mi units are 20 MiB.
		type(ALT_A, PASTE_START, "é".repeat(10 * 1024 * 1024));
		assert.equal(editor.getText(), "hello");
		assert.equal(hintOf(plain(100)), "5");
		type("x");
		assert.equal(editor.getText(), "x");
	} finally { editor.dispose(); }
});

test("a paste of many pieces looks for its end mark in the new piece only, never the whole buffer again", () => {
	const { editor, type } = prompt();
	try {
		editor.setText("hello");
		type(ALT_A);
		const pieces = 1500;
		const searched: number[] = [];
		const indexOf = String.prototype.indexOf;
		String.prototype.indexOf = function (this: string, search: string, position?: number): number {
			if (search === PASTE_END) searched.push(this.length);
			return indexOf.call(this, search, position);
		};
		try {
			type(PASTE_START, ...Array.from({ length: pieces }, () => "z"), PASTE_END);
		} finally { String.prototype.indexOf = indexOf; }
		assert.match(editor.getText(), /^\[paste #1 \d+ chars\]$/);
		// The end mark's 5 carried units and the 6-unit last piece; pi may scan the finished frame once itself.
		assert.equal(searched.filter((n) => n <= 11).length, pieces + 2);
		assert.ok(searched.filter((n) => n > 11).length <= 1, searched.filter((n) => n > 11).join(", "));
	} finally { editor.dispose(); }
});

// --- other keys ----------------------------------------------------------------------

test("a move, a word move or a key release: moves drop the selection, a release keeps it", () => {
	const { editor, plain, type } = prompt();
	try {
		editor.setText("hello world");
		type(ALT_A, "\x1b[1;3D"); // alt+left
		assert.equal(hintOf(plain(100)), undefined);
		assert.deepEqual(editor.getCursor(), { line: 0, col: 6 });
		type(SHIFT_HOME, "\x1b[97;1:3u"); // kitty: the release of a
		assert.equal(hintOf(plain(100)), "6");
		assert.equal(editor.getText(), "hello world");
		type(RIGHT, DEL);
		assert.equal(editor.getText(), "hllo world", "after a move Del is pi's own");
	} finally { editor.dispose(); }
});

test("enter sends the whole text, not the selection", () => {
	const { editor, plain, type } = prompt();
	try {
		const sent: string[] = [];
		editor.onSubmit = (text) => sent.push(text);
		editor.setText("hello world");
		type(SHIFT_HOME, "\r");
		assert.deepEqual(sent, ["hello world"]);
		assert.equal(hintOf(plain(100)), undefined);
	} finally { editor.dispose(); }
});

test("esc drops the selection and opens the esc confirmation as usual", () => {
	const { editor, plain, type } = prompt();
	try {
		editor.setText("hello");
		type(ALT_A, ESC);
		const rows = plain(100);
		assert.equal(hintOf(rows), undefined);
		assert.match(rows[0]!, /esc again clears it/);
		assert.equal(editor.getText(), "hello");
	} finally { editor.dispose(); }
});

test("another extension's shortcut wins its chord over the selection", () => {
	const { editor, plain, type } = prompt();
	try {
		const seen: string[] = [];
		editor.onExtensionShortcut = (data) => { seen.push(data); return matchesKey(data, "alt+a") || matchesKey(data, "shift+home"); };
		editor.setText("abc");
		type(ALT_A, SHIFT_HOME);
		assert.deepEqual(seen, [ALT_A, SHIFT_HOME]);
		assert.equal(hintOf(plain(100)), undefined);
		type(ALT_E);
		assert.equal(hintOf(plain(100)), "3", "a chord no shortcut claims still selects");
	} finally { editor.dispose(); }
});

test("shift+home leaves the history the up arrow was browsing", () => {
	const { editor, type } = prompt();
	try {
		editor.addToHistory("old one");
		editor.setText("");
		type(UP, DOWN);
		assert.equal(editor.getText(), "", "down comes back from the history");
		type(UP, SHIFT_HOME, DOWN);
		assert.equal(editor.getText(), "old one", "after shift+home the text is the draft");
	} finally { editor.dispose(); }
});

test("select all closes the completion menu; typing over a selection closes it too", async () => {
	const { editor, type } = prompt();
	const settle = () => new Promise<void>((resolve) => setImmediate(resolve));
	editor.setAutocompleteProvider(slashCommands("alpha"));
	try {
		type("/");
		await settle();
		assert.equal(editor.isShowingAutocomplete(), true);
		type(ALT_A);
		assert.equal(editor.isShowingAutocomplete(), false);
		type(BACKSPACE, "/");
		await settle();
		assert.equal(editor.isShowingAutocomplete(), true);
		type(SHIFT_HOME);
		await settle();
		type("x");
		assert.equal(editor.isShowingAutocomplete(), false);
		assert.equal(editor.getText(), "x");
	} finally { editor.dispose(); }
});

// --- the mouse ----------------------------------------------------------------------

const mouse = (type: "click" | "press" | "drag" | "release", x: number, y: number, width = 60) =>
	({ type, button: "left", x, y, width, height: 5, screenX: x, screenY: y, shift: false, alt: false, ctrl: false }) as const;

test("a click moves the caret and the selection runs from its anchor to the click", () => {
	const { editor, plain, type } = prompt();
	try {
		editor.setText("hello world");
		type(SHIFT_HOME);
		plain(60);
		editor.handleMouse(mouse("click", 3 + 5, 2));
		assert.deepEqual(editor.getCursor(), { line: 0, col: 5 });
		assert.equal(hintOf(plain(60)), "6");
	} finally { editor.dispose(); }
});

test("press, drag and release over the input are left to pi's renderer (its screen selection copies on release)", () => {
	const { editor, plain } = prompt();
	try {
		editor.setText("hello world");
		plain(60);
		for (const kind of ["press", "drag", "release"] as const) assert.equal(editor.handleMouse(mouse(kind, 6, 2)), undefined, kind);
		assert.equal(editor.getText(), "hello world");
	} finally { editor.dispose(); }
});

// --- how it draws --------------------------------------------------------------------

test("the inverted cells keep pi's software cursor, whose closing mark turns inverse off", () => {
	const { editor, type } = prompt();
	try {
		editor.setText("abcd");
		type(LEFT, SHIFT_HOME);
		const row = editor.render(80)[2]!;
		assert.ok(row.includes(`\x1b[7m${CURSOR_MARKER}\x1b_pi:fc\x07a\x1b_pi:/fc\x07\x1b[7mbc\x1b[27md`), JSON.stringify(row));
	} finally { editor.dispose(); }
});

test("a selection paints every wrapped row, at pi's wrap width, and only the rows it covers when scrolled", () => {
	const { editor, render, type } = prompt();
	try {
		editor.setText("abcdefghijklmnop");
		type(ALT_A);
		let rows = render(12);
		assert.deepEqual(rows.map(stripAnsi).slice(2, 4), [" ❯ abcdefgh ", "   ijklmnop "]);
		assert.ok(rows[2]!.includes("\x1b[7mabcdefgh\x1b[27m") && rows[3]!.includes("\x1b[7mijklmnop\x1b[27m"));
		editor.setText(Array.from({ length: 12 }, (_, i) => `line ${i + 1}`).join("\n"));
		type(HOME, SHIFT_END);
		rows = render(80);
		assert.equal(stripAnsi(rows[10]!).trim(), "line 12");
		assert.ok(rows[10]!.includes("\x1b[7mline 12\x1b[27m"));
		assert.ok(rows.slice(2, 10).every((row) => !row.includes("\x1b[7m")), "the rows above are not selected");
	} finally { editor.dispose(); }
});

test("without pi's last wrap width the selection wraps at the prompt's own width", () => {
	const { editor, render, type } = prompt();
	try {
		Object.defineProperty(editor, "lastWidth", { get: () => 0, set() {}, configurable: true });
		editor.setText("abcdefghijklmnop");
		type(ALT_A);
		const rows = render(12);
		assert.ok(rows[2]!.includes("\x1b[7mabcdefgh\x1b[27m") && rows[3]!.includes("\x1b[7mijklmnop\x1b[27m"));
	} finally { editor.dispose(); }
});

test("the count takes the bottom rule's right end, over pi's ↓ more note", () => {
	const { editor, plain, type } = prompt();
	try {
		editor.setText(Array.from({ length: 12 }, (_, i) => `line ${i + 1}`).join("\n"));
		for (let i = 0; i < 11; i++) type(UP);
		type(HOME);
		assert.match(plain(80).at(-1)!, /↓ 3 more ─$/);
		type(SHIFT_END);
		assert.equal(plain(80).at(-1), hinted(80, 6));
	} finally { editor.dispose(); }
});

test("the count is plain text on a rule in pi's border colour; with ascii the rule is dashes", () => {
	const { editor, type } = prompt();
	try {
		editor.borderColor = (text) => `\x1b[36m${text}\x1b[39m`;
		editor.setText("hello");
		type(ALT_A);
		const bottom = editor.render(100).at(-1)!;
		assert.ok(bottom.endsWith(`${label(5)}\x1b[36m─\x1b[39m`), JSON.stringify(bottom));
		assert.equal(stripAnsi(bottom), hinted(100, 5));
	} finally { editor.dispose(); }
	const ascii = prompt({ ascii: true });
	try {
		ascii.editor.setText("hello");
		ascii.type(ALT_A);
		assert.deepEqual(ascii.plain(100).slice(1), [rule(100, "-"), padded(" > hello", 100), hinted(100, 5, "-")]);
	} finally { ascii.editor.dispose(); }
});

test("the count leaves the rule bare when it does not fit, and every row keeps the width", () => {
	const { editor, plain, type } = prompt();
	try {
		editor.setText("hello");
		type(ALT_A);
		const fits = visibleWidth(label(5)) + 3;
		assert.equal(plain(fits).at(-1), hinted(fits, 5));
		assert.equal(plain(fits - 1).at(-1), rule(fits - 1));
		for (const width of [0, 1, 4, 5, 9, fits - 1, fits, 100]) assert.ok(plain(width).every((row) => visibleWidth(row) <= width), `${width}`);
	} finally { editor.dispose(); }
});

// --- when pi changes inside -------------------------------------------------------------

test("when pi's editor lacks a member the selection needs, the prompt edits on with no selection, for good", () => {
	const { editor, plain, type } = prompt();
	try {
		const inside = editor as unknown as Record<string, unknown>;
		editor.setText("hello world");
		// pi renamed or dropped a member (setText sets this one, so it goes after it).
		const kept = inside.autocompleteState;
		delete inside.autocompleteState;
		type(SHIFT_HOME);
		assert.equal(hintOf(plain(100)), undefined);
		assert.ok(!editor.render(100).some((row) => row.includes("\x1b[7m")));
		type(ALT_A, "x");
		assert.equal(editor.getText(), "hello worldx", "keys reach pi as they came");
		inside.autocompleteState = kept;
		type(SHIFT_HOME);
		assert.equal(hintOf(plain(100)), undefined, "the check is made once");
	} finally { editor.dispose(); }
});

// --- Jhon's own code --------------------------------------------------------------------

test("the selection is Jhon's own: lib/ui/prompt-selection.ts, with pi's insides read in lib/ui/editor-access.ts", async () => {
	const { readFileSync } = await import("node:fs");
	const ui = readFileSync(new URL("../extensions/ui.ts", import.meta.url), "utf8");
	assert.match(ui, /from "\.\.\/lib\/ui\/prompt-selection\.ts"/);
	assert.doesNotMatch(ui, /selection-engine|pi-tui-keys|renderedVisibleLineCount/);
	const selection = readFileSync(new URL("../lib/ui/prompt-selection.ts", import.meta.url), "utf8");
	assert.match(selection, /from "\.\/editor-access\.ts"/);
	assert.doesNotMatch(selection, /as unknown as|state\.lines|setCursorCol|pushUndoSnapshot/, "pi's members are named in editor-access.ts only");
});

test("invertCells walks past escape sequences and turns inverse back on after a reset inside the span", async () => {
	const { invertCells } = await import("../lib/ui/prompt-selection.ts");
	assert.equal(invertCells("abcdef", 1, 3), "a\x1b[7mbc\x1b[27mdef");
	assert.equal(invertCells("abc", 1, 9), "a\x1b[7mbc\x1b[27m", "a span past the end closes at the end");
	assert.equal(invertCells("abc", 5, 9), "abc", "a span past the end draws nothing");
	assert.equal(invertCells("a\x1b[0mbcd", 0, 3), "\x1b[7ma\x1b[0m\x1b[7mbc\x1b[27md");
	assert.equal(invertCells("a\x1b[mbcd", 0, 3), "\x1b[7ma\x1b[m\x1b[7mbc\x1b[27md");
	assert.equal(invertCells("a\x1b[0;35mbcd", 0, 3), "\x1b[7ma\x1b[0;35m\x1b[7mbc\x1b[27md");
	assert.equal(invertCells("a\x1b[35mbcd", 0, 3), "\x1b[7ma\x1b[35mbc\x1b[27md", "a colour is no reset");
	assert.equal(invertCells("a\x1b[;35mbcd", 0, 3), "\x1b[7ma\x1b[;35mbc\x1b[27md", "nor is an empty first parameter followed by more");
	assert.equal(invertCells("a\x1b]8;;x\x07b", 1, 2), "a\x1b[7m\x1b]8;;x\x07b\x1b[27m", "OSC up to BEL");
	assert.equal(invertCells("a\x1b]8;;x\x1b\\b", 1, 2), "a\x1b[7m\x1b]8;;x\x1b\\b\x1b[27m", "OSC up to ST");
	assert.equal(invertCells("ab\x1b[12", 0, 1), "\x1b[7ma\x1b[27mb\x1b[12", "an unfinished CSI runs to the end");
	assert.equal(invertCells("a\x1b]x", 0, 5), "\x1b[7ma\x1b]x\x1b[27m", "an unfinished OSC runs to the end");
});

test("pasteInto keeps pi's own result when pi did more than insert at the caret", async () => {
	const { pasteInto } = await import("../lib/ui/prompt-selection.ts");
	// "hello world", selection [0, 5), caret 0; pi inserted "XY" at the caret.
	assert.deepEqual(pasteInto("hello world", "XYhello world", 0, 0, 5), { text: "XY world", caret: 2 });
	assert.equal(pasteInto("hello world", "something else", 0, 0, 5), undefined);
	assert.equal(pasteInto("hello world", "hello world", 0, 0, 5), undefined, "nothing inserted");
});
