import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { matchesKey } from "@earendil-works/pi-tui";
import { extractQueuedText, JhonPromptEditor } from "../extensions/ui.ts";
import { escKey, freshHome, homeSettings, noKeys, prompt, rows, session, shell, shown, tui } from "./shell-harness.ts";

// The shell's prompt as pi's editor: when the shell takes the editor slot
// and when it leaves it, what pi's own editor keeps doing under it (paste,
// completion, shortcuts), and what the working line shows.

const editorTheme = { borderColor: (text: string) => text, selectList: {} };
const bare = { fg: (_role: string, text: string) => text, bold: (text: string) => text, requestRender() {}, pending: () => false, now: () => 0, doubleEscCancelEnabled: () => false, dispatchQueuedText() {} };
const MiB16 = 16 * 1024 * 1024;
/** Bracketed paste's opening and closing marks. */
const [PASTE_START, PASTE_END] = ["\x1b[200~", "\x1b[201~"];

/** Presses `key` over the draft "abc\ndef": whether it selected all of it, drawn inverse and then erased by one backspace. */
function selectsWholeDraft(editor: JhonPromptEditor, key: string): boolean {
	editor.setText("abc\ndef");
	editor.handleInput(key);
	const inverse = /\x1b\[7mabc/.test(editor.render(40).join("\n"));
	editor.handleInput("\x7f");
	return inverse && editor.getText() === "";
}

/** pi's editor's own undo, which its type does not expose. */
const undo = (editor: JhonPromptEditor): void => (editor as unknown as { undo(): void }).undo();
/** shift+home: selects the draft, so a paste replaces it. */
const SELECT_TO_START = "\x1b[1;2H";

test("the shell takes the editor and hides pi's Working row, whatever terminal or multiplexer it runs in", () => {
	const s = shell({ PI_CODING_AGENT_DIR: "/nonexistent", TMUX: "/tmp/tmux-1/default,1,0" });
	const { ctx, ui } = session();
	const editor = prompt(s, ctx, ui);
	assert.ok(editor instanceof JhonPromptEditor);
	assert.equal(ui.workingVisible, false);
	editor.dispose();
});

test("an editor another extension set stays, and pi's Working row with it", () => {
	const s = shell();
	const theirs = () => ({});
	const { ctx, ui } = session({ editor: theirs });
	s.emit("session_start", ctx);
	assert.equal(ui.editor, theirs);
	assert.equal(ui.workingVisible, undefined);
});

test("after a reload the new shell replaces the old one's editor, and its handlers drive the prompt", () => {
	const { ctx, ui } = session();
	const before = shell();
	const old = prompt(before, ctx, ui);
	const oldFactory = ui.editor;
	const after = shell();
	const editor = prompt(after, ctx, ui);
	try {
		assert.notEqual(ui.editor, oldFactory);
		after.emit("agent_start", ctx);
		assert.match(rows(editor)[0]!, /Working…/);
	} finally { old.dispose(); editor.dispose(); }
});

test("at shutdown pi gets its editor and Working row back, but never another extension's editor taken away", () => {
	const s = shell();
	const { ctx, ui } = session();
	prompt(s, ctx, ui).dispose();
	s.emit("session_shutdown", ctx);
	assert.equal(ui.editor, undefined);
	assert.equal(ui.workingVisible, true);
	const theirs = () => ({});
	const other = session({ editor: theirs });
	s.emit("session_start", other.ctx);
	s.emit("session_shutdown", other.ctx);
	assert.equal(other.ui.editor, theirs);
	assert.equal(other.ui.workingVisible, undefined);
});

// Another extension's shortcut wins its chord over the prompt's selection keys, empty draft or not.
test("an extension's alt+a beats select-all; alt+e still selects everything", () => {
	const s = shell();
	const { ctx, ui } = session();
	const editor = prompt(s, ctx, ui);
	try {
		const asked: string[] = [];
		editor.onExtensionShortcut = (data) => { asked.push(data); return matchesKey(data, "alt+a"); };
		for (const altA of ["\x1ba", "\x1b[97;3u", "\x1b[97;3:1u"]) {
			for (const draft of ["", "abc\ndef"]) {
				asked.length = 0;
				editor.setText(draft);
				editor.handleInput(altA);
				assert.deepEqual(asked, [altA], JSON.stringify([altA, draft]));
			}
			assert.doesNotMatch(editor.render(40).join("\n"), /\x1b\[7mabc/);
			editor.handleInput("\x7f");
			assert.equal(editor.getText(), "abc\nde", "nothing was selected");
		}
		assert.ok(selectsWholeDraft(editor, "\x1be"), "alt+e, which no extension claims, still selects the whole draft");
	} finally { editor.dispose(); }
});

test("with no extension claiming it, alt+a selects the whole draft", () => {
	const s = shell();
	const { ctx, ui } = session();
	const editor = prompt(s, ctx, ui);
	try {
		editor.onExtensionShortcut = () => false;
		assert.ok(selectsWholeDraft(editor, "\x1ba"));
	} finally { editor.dispose(); }
});

test("the clear hint leaves at the end of its window by itself: one repaint, the draft kept", () => {
	let now = 0;
	let repaints = 0;
	const timers: Array<{ fn: () => void; ms: number }> = [];
	const editor = new JhonPromptEditor(tui as never, editorTheme as never, escKey as never, {
		...bare, now: () => now, requestRender() { repaints++; }, schedule: (fn, ms) => { timers.push({ fn, ms }); return () => {}; },
	});
	try {
		editor.focused = true;
		editor.setText("draft");
		editor.handleInput("\x1b");
		assert.match(shown(editor, 80), /esc again clears it/);
		assert.equal(timers.length, 1);
		now = timers[0]!.ms;
		const before = repaints;
		timers[0]!.fn();
		assert.equal(repaints, before + 1);
		assert.doesNotMatch(shown(editor, 80), /esc again/);
		assert.equal(editor.getText(), "draft");
	} finally { editor.dispose(); }
});

test("the completion list never draws wider than the prompt", () => {
	const s = shell();
	const { ctx, ui } = session();
	const editor = prompt(s, ctx, ui);
	const inside = editor as unknown as { autocompleteState: string; autocompleteList: { render(width: number): string[] } };
	inside.autocompleteState = "force";
	inside.autocompleteList = { render: () => ["completion"] };
	try {
		for (const width of [0, 1, 2, 30]) {
			const drawn = rows(editor, width);
			assert.ok(drawn.every((row) => [...row].length <= width), `${width}: ${JSON.stringify(drawn)}`);
			if (width === 30) assert.ok(drawn.some((row) => row.includes("completion")));
		}
	} finally { editor.dispose(); }
});

test("the prompt paints no background, idle, working or with messages queued", () => {
	const s = shell();
	let queued = false;
	const { ctx, ui } = session({ theme: { fg: (_r: string, t: string) => t, bold: (t: string) => t, bg: (_r: string, t: string) => t, getBgAnsi: () => "\x1b[44m" }, pending: () => queued });
	const editor = prompt(s, ctx, ui);
	try {
		for (const state of ["idle", "working", "queued"]) {
			if (state === "working") s.emit("agent_start", ctx);
			queued = state === "queued";
			for (const width of [8, 40, 80]) assert.doesNotMatch(editor.render(width).join("\n"), /\x1b\[44m/, state);
		}
	} finally { editor.dispose(); }
	assert.equal(s.tools.get("session_worktree_register")?.renderShell, "self");
});

test("the working line follows the run: working, queued, still working after the run's end, ready once settled", () => {
	const s = shell();
	let queued = false;
	const { ctx, ui } = session({ pending: () => queued });
	const editor = prompt(s, ctx, ui);
	s.emit("agent_start", ctx);
	assert.match(rows(editor)[0]!, /^ 力 Working…  thinking · \d+s +esc interrupts $/);
	queued = true;
	assert.match(rows(editor)[0]!, /^ 力 Working…  thinking · \d+s · queued/);
	s.emit("agent_end", ctx);
	assert.match(rows(editor)[0]!, /queued/, "the end of a run is not its settle");
	queued = false;
	s.emit("agent_settled", ctx);
	assert.match(rows(editor)[0]!, /^ 力 ready for you +$/);
	editor.dispose();
});

// Bracketed paste is pi's editor's own; the shell's layer must not cap or leak it.
test("a paste that arrives in pieces up to exactly 16 MiB completes, and one undo restores the draft", () => {
	const editor = new JhonPromptEditor(tui as never, editorTheme as never, noKeys as never, bare);
	try {
		editor.setText("hello");
		editor.handleInput(SELECT_TO_START);
		editor.handleInput(PASTE_START);
		const body = "z".repeat(MiB16 - PASTE_START.length);
		editor.handleInput(body);
		editor.handleInput(PASTE_END);
		assert.equal(editor.getExpandedText(), body);
		undo(editor);
		assert.equal(editor.getText(), "hello", "one undo brings the draft back");
	} finally { editor.dispose(); }
});

test("a whole paste in one piece larger than 16 MiB is never cut", () => {
	const editor = new JhonPromptEditor(tui as never, editorTheme as never, noKeys as never, bare);
	try {
		editor.setText("hello");
		editor.handleInput(SELECT_TO_START);
		const body = "z".repeat(MiB16 + 1024);
		editor.handleInput(`${PASTE_START}${body}${PASTE_END}`);
		assert.equal(editor.getExpandedText(), body);
		undo(editor);
		assert.equal(editor.getText(), "hello", "one undo brings the draft back");
	} finally { editor.dispose(); }
});

test("an unfinished paste past 16 MiB is dropped whole; the next key types over the selection", () => {
	const editor = new JhonPromptEditor(tui as never, editorTheme as never, noKeys as never, bare);
	try {
		editor.setText("hello");
		editor.handleInput(SELECT_TO_START);
		editor.handleInput(PASTE_START);
		editor.handleInput("z".repeat(MiB16 - PASTE_START.length + 1));
		assert.equal(editor.getText(), "hello", "no piece of it reaches the editor");
		editor.handleInput("x");
		assert.equal(editor.getText(), "x");
	} finally { editor.dispose(); }
});

// T29 S3: no vim mode, even with an old shell.editor.vim = true.
test("there is no vim mode: Esc then i types the i, and no mode is named", () => {
	const home = freshHome();
	homeSettings(home, { "shell.editor.vim": true });
	const s = shell({ JHON_SHELL_CONFIG_HOME: home });
	const { ctx, ui } = session();
	const editor = prompt(s, ctx, ui);
	try {
		editor.setText("draft");
		editor.handleInput("\x1b");
		editor.handleInput("i");
		assert.equal(editor.getText(), "drafti");
		assert.doesNotMatch(editor.render(60).join("\n"), /NORMAL|INSERT|VISUAL/);
	} finally { editor.dispose(); }
});

test("extractQueuedText, as the shell exports it, takes back what pi queued ahead of the draft", () => {
	assert.equal(extractQueuedText("later\n\nmy draft", "my draft"), "later");
	assert.equal(extractQueuedText("my draft", "my draft"), "");
	assert.equal(extractQueuedText("later", ""), "later");
	assert.equal(extractQueuedText("later", "   "), "later");
	assert.equal(extractQueuedText("", "   "), "");
	assert.equal(extractQueuedText("", ""), "");
	assert.equal(extractQueuedText("other", "my draft"), undefined);
});
