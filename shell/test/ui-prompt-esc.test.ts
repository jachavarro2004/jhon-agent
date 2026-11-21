import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import type { JhonPromptEditor } from "../extensions/ui.ts";
import { customize, escKey, homeSettings, prompt, saved, session, shell, shown, type Shell } from "./shell-harness.ts";

// Esc on the shell's prompt (lib/ui/double-esc-cancel-policy.ts through
// JhonPromptEditor and extensions/ui.ts). pi's own Esc handling is the
// editor's onEscape: here a stand-in that records the stop and, when the
// test says so, puts text back as pi does (its queued messages, a blank
// line, then the draft).

const STOP_HINT = /esc again stops it/;
const CLEAR_HINT = /esc again clears it/;
const anyEsc = { matches: (_data: string, action: string) => action === "app.interrupt" };

/** A shell, a session and its prompt; `clock` moves the time the Esc gate reads. */
function rig(env: NodeJS.ProcessEnv = {}, keys: unknown = anyEsc) {
	const clock = { now: 1_000_000 };
	const s = shell(env, { now: () => clock.now });
	const { ctx, ui } = session();
	const editor = prompt(s, ctx, ui, keys);
	let stops = 0;
	editor.onEscape = () => { stops++; };
	return { s, ctx, ui, editor, clock, stops: () => stops, working: () => s.emit("agent_start", ctx), settled: (on = ctx) => s.emit("agent_settled", on) };
}

/** pi's restore on a stop: the queued text, a blank line, the draft. */
function restores(editor: JhonPromptEditor, queued: string) {
	editor.onEscape = () => { editor.setText(`${queued}\n\n${editor.getText()}`); };
}

const sentTexts = (s: Shell) => s.sent.map((message) => message.content);

test("with double Esc off (the default) one Esc while working stops the run at once", () => {
	const r = rig();
	r.working();
	r.editor.handleInput("\x1b");
	assert.equal(r.stops(), 1);
	assert.doesNotMatch(shown(r.editor), STOP_HINT);
	r.editor.dispose();
});

test("with double Esc on, the first Esc while working only shows the hint", () => {
	const r = rig({ JHON_SHELL_DOUBLE_ESC_CANCEL: "on" });
	r.working();
	r.editor.handleInput("\x1b");
	assert.equal(r.stops(), 0);
	assert.match(shown(r.editor), STOP_HINT);
	r.editor.dispose();
});

test("with double Esc on, a second Esc within the second stops the run and the hint goes", () => {
	const r = rig({ JHON_SHELL_DOUBLE_ESC_CANCEL: "on" });
	r.working();
	r.editor.handleInput("\x1b");
	r.clock.now += 500;
	r.editor.handleInput("\x1b");
	assert.equal(r.stops(), 1);
	assert.doesNotMatch(shown(r.editor), STOP_HINT);
	r.editor.dispose();
});

test("with double Esc on, an Esc after the window starts over", () => {
	const r = rig({ JHON_SHELL_DOUBLE_ESC_CANCEL: "on" });
	r.working();
	r.editor.handleInput("\x1b");
	r.clock.now += 1001;
	r.editor.handleInput("\x1b");
	assert.equal(r.stops(), 0);
	assert.match(shown(r.editor), STOP_HINT);
	r.editor.dispose();
});

test("with double Esc on, Esc while idle on an empty prompt goes straight to pi", () => {
	const r = rig({ JHON_SHELL_DOUBLE_ESC_CANCEL: "on" });
	r.editor.handleInput("\x1b");
	assert.equal(r.stops(), 1);
	r.editor.dispose();
});

test("with double Esc on, Esc while the completion list shows is the list's, as in pi's editor", () => {
	const r = rig({ JHON_SHELL_DOUBLE_ESC_CANCEL: "on" });
	(r.editor as unknown as { isShowingAutocomplete(): boolean }).isShowingAutocomplete = () => true;
	r.working();
	r.editor.handleInput("\x1b");
	assert.doesNotMatch(shown(r.editor), STOP_HINT);
	r.editor.dispose();
});

// A stop with messages queued: pi puts them back ahead of the draft; the
// shell keeps the draft in the prompt and sends the queue once the stopped
// run has settled.

test("a stop with a message queued keeps the draft and sends the message once the run settles", () => {
	const r = rig();
	r.editor.setText("my draft");
	restores(r.editor, "queued one");
	r.working();
	r.editor.handleInput("\x1b");
	assert.equal(r.editor.getText(), "my draft");
	assert.deepEqual(r.s.sent, []);
	r.settled();
	assert.deepEqual(sentTexts(r.s), ["queued one"]);
	r.editor.dispose();
});

test("a stop with nothing queued leaves pi's own restore as the only write, and sends nothing", () => {
	const r = rig();
	r.editor.setText("my draft");
	r.editor.onEscape = () => { r.editor.setText(r.editor.getText()); };
	let writes = 0;
	const write = r.editor.setText.bind(r.editor);
	(r.editor as unknown as { setText(text: string): void }).setText = (text: string) => { writes++; write(text); };
	r.working();
	r.editor.handleInput("\x1b");
	assert.equal(writes, 1, "pi's restore, and no write of the shell's");
	assert.equal(r.editor.getText(), "my draft");
	r.settled();
	assert.deepEqual(r.s.sent, []);
	r.editor.dispose();
});

test("a restore the shell does not recognise stays as pi left it, and nothing is sent", () => {
	const r = rig();
	r.editor.setText("my draft");
	r.editor.onEscape = () => { r.editor.setText("something else"); };
	r.working();
	r.editor.handleInput("\x1b");
	assert.equal(r.editor.getText(), "something else");
	r.settled();
	assert.deepEqual(r.s.sent, []);
	r.editor.dispose();
});

test("with no draft the whole restore is the queue: sent, and the prompt stays empty", () => {
	const r = rig();
	r.editor.onEscape = () => { r.editor.setText("just queued"); };
	r.working();
	r.editor.handleInput("\x1b");
	assert.equal(r.editor.getText(), "");
	r.settled();
	assert.deepEqual(sentTexts(r.s), ["just queued"]);
	r.editor.dispose();
});

test("with double Esc on, the confirming Esc sends the queue the same way", () => {
	const r = rig({ JHON_SHELL_DOUBLE_ESC_CANCEL: "on" });
	r.editor.setText("my draft");
	restores(r.editor, "queued one");
	r.working();
	r.editor.handleInput("\x1b");
	r.clock.now += 500;
	r.editor.handleInput("\x1b");
	assert.equal(r.editor.getText(), "my draft");
	r.settled();
	assert.deepEqual(sentTexts(r.s), ["queued one"]);
	r.editor.dispose();
});

test("the queue goes once, however many settles follow, and never from a run's start", () => {
	const r = rig();
	r.editor.setText("my draft");
	restores(r.editor, "queued one");
	r.working();
	r.editor.handleInput("\x1b");
	r.working();
	assert.deepEqual(r.s.sent, [], "a run that starts before the settle sends nothing");
	r.settled();
	r.settled();
	assert.deepEqual(r.s.sent.map((m) => ({ content: m.content, deliverAs: m.options?.deliverAs })), [{ content: "queued one", deliverAs: undefined }], "a plain user message");
	r.working();
	assert.equal(r.s.sent.length, 1);
	r.editor.dispose();
});

test("a queue of only blank lines is no queue: nothing is sent and the draft comes back alone", () => {
	const r = rig();
	r.editor.setText("my draft");
	restores(r.editor, "   ");
	r.working();
	r.editor.handleInput("\x1b");
	r.settled();
	assert.deepEqual(r.s.sent, []);
	assert.equal(r.editor.getText(), "my draft");
	r.editor.dispose();
});

test("a send that fails puts the queue back ahead of the draft, says why, and the prompt is idle", () => {
	const r = rig();
	r.editor.setText("my draft");
	restores(r.editor, "queued one");
	r.working();
	r.editor.handleInput("\x1b");
	r.s.pi.sendUserMessage = () => { throw new Error("switching sessions"); };
	r.settled();
	assert.deepEqual(r.ui.notices, ["Could not send the queued message after cancelling; it went back to the editor: switching sessions"]);
	assert.equal(r.editor.getText(), "queued one\n\nmy draft");
	assert.doesNotMatch(shown(r.editor), STOP_HINT);
	r.editor.dispose();
});

test("a send that fails without a UI still puts the queue back, with no notice", () => {
	const r = rig();
	const headless = session({ hasUI: false });
	r.editor.setText("my draft");
	restores(r.editor, "queued one");
	r.working();
	r.editor.handleInput("\x1b");
	r.s.pi.sendUserMessage = () => { throw new Error("switching sessions"); };
	r.settled(headless.ctx);
	assert.deepEqual(headless.ui.notices, []);
	assert.equal(r.editor.getText(), "queued one\n\nmy draft");
	r.editor.dispose();
});

test("a settle while pi is not idle sends nothing and leaves the prompt working; the idle settle sends", () => {
	const r = rig();
	const busy = session({ idle: false });
	r.editor.setText("my draft");
	restores(r.editor, "queued one");
	r.working();
	r.editor.handleInput("\x1b");
	r.settled(busy.ctx);
	assert.deepEqual(r.s.sent, []);
	let stops = 0;
	r.editor.onEscape = () => { stops++; };
	r.editor.handleInput("\x1b");
	assert.equal(stops, 1, "still working: Esc stops");
	assert.doesNotMatch(shown(r.editor), CLEAR_HINT);
	r.settled();
	assert.deepEqual(sentTexts(r.s), ["queued one"]);
	r.editor.dispose();
});

test("two stops before an idle settle send both queues, in order, as one message", () => {
	const r = rig();
	const busy = session({ idle: false });
	let stop = 0;
	r.editor.onEscape = () => { stop++; r.editor.setText(stop === 1 ? "first" : "second"); };
	r.working();
	r.editor.handleInput("\x1b");
	r.settled(busy.ctx);
	r.working();
	r.editor.handleInput("\x1b");
	r.settled();
	assert.deepEqual(sentTexts(r.s), ["first\n\nsecond"]);
	r.editor.dispose();
});

test("a queue never reaches the next session", () => {
	const r = rig();
	r.editor.setText("my draft");
	restores(r.editor, "queued one");
	r.working();
	r.editor.handleInput("\x1b");
	r.s.emit("session_shutdown", r.ctx);
	r.settled();
	assert.deepEqual(r.s.sent, []);
});

// An idle draft: the first Esc shows a hint, a second within half a second
// clears it into history. An empty prompt, the completion list, a bash-mode
// draft and a working run keep pi's own Esc.

test("idle with a draft, the first Esc keeps the text and shows the clear hint", () => {
	const r = rig();
	r.editor.setText("my draft");
	r.editor.handleInput("\x1b");
	assert.equal(r.stops(), 0);
	assert.equal(r.editor.getText(), "my draft");
	assert.match(shown(r.editor), CLEAR_HINT);
	r.editor.dispose();
});

test("idle with a draft, a second Esc within the window puts the draft in history and clears it", () => {
	const r = rig();
	r.editor.setText("my draft");
	const history: string[] = [];
	(r.editor as unknown as { addToHistory(text: string): void }).addToHistory = (text) => { history.push(text); };
	r.editor.handleInput("\x1b");
	r.clock.now += 400;
	r.editor.handleInput("\x1b");
	assert.deepEqual(history, ["my draft"]);
	assert.equal(r.editor.getText(), "");
	assert.doesNotMatch(shown(r.editor), CLEAR_HINT);
	r.editor.dispose();
});

test("idle with a draft, an Esc after the window starts over", () => {
	const r = rig();
	r.editor.setText("my draft");
	r.editor.handleInput("\x1b");
	r.clock.now += 501;
	r.editor.handleInput("\x1b");
	assert.equal(r.editor.getText(), "my draft");
	assert.match(shown(r.editor), CLEAR_HINT);
	r.editor.dispose();
});

test("idle and empty, Esc is pi's (its tree and fork double Esc)", () => {
	const r = rig();
	r.editor.handleInput("\x1b");
	assert.equal(r.stops(), 1);
	r.editor.dispose();
});

test("idle with the completion list showing, Esc is the list's and the draft stays", () => {
	const r = rig();
	r.editor.setText("my draft");
	(r.editor as unknown as { isShowingAutocomplete(): boolean }).isShowingAutocomplete = () => true;
	r.editor.handleInput("\x1b");
	assert.equal(r.editor.getText(), "my draft");
	assert.doesNotMatch(shown(r.editor), CLEAR_HINT);
	r.editor.dispose();
});

test("while working, a draft's Esc is the stop gate's, never the clear hint", () => {
	const r = rig();
	r.editor.setText("my draft");
	r.working();
	r.editor.onEscape = () => { r.editor.setText(r.editor.getText()); };
	r.editor.handleInput("\x1b");
	assert.doesNotMatch(shown(r.editor), CLEAR_HINT);
	r.editor.dispose();
});

test("an edit between the two Esc presses starts a new window instead of clearing the edit", () => {
	const r = rig();
	r.editor.setText("my draft");
	r.editor.handleInput("\x1b");
	r.editor.setText("my draft, edited");
	assert.doesNotMatch(shown(r.editor), CLEAR_HINT, "the edit drops the pending clear and its hint");
	r.clock.now += 400;
	r.editor.handleInput("\x1b");
	assert.equal(r.editor.getText(), "my draft, edited");
	assert.match(shown(r.editor), CLEAR_HINT);
	r.clock.now += 400;
	r.editor.handleInput("\x1b");
	assert.equal(r.editor.getText(), "", "the new window's second Esc clears");
	r.editor.dispose();
});

test("a key typed and deleted between the presses still drops the pending clear", () => {
	const r = rig({}, escKey);
	r.editor.setText("my draft");
	r.editor.handleInput("\x1b");
	r.editor.handleInput("x");
	r.editor.handleInput("\x7f");
	assert.equal(r.editor.getText(), "my draft");
	r.clock.now += 400;
	r.editor.handleInput("\x1b");
	assert.equal(r.editor.getText(), "my draft");
	r.editor.dispose();
});

test("a selection, or typing the same character over it, drops the pending clear too", () => {
	const r = rig({}, escKey);
	try {
		for (const keys of [["\x1b[1;2H"], ["\x1b[1;2H", "x"]]) {
			r.editor.setText("x");
			r.editor.handleInput("\x1b");
			for (const key of keys) r.editor.handleInput(key);
			assert.equal(r.editor.getText(), "x");
			assert.doesNotMatch(shown(r.editor), CLEAR_HINT);
			r.clock.now += 400;
			r.editor.handleInput("\x1b");
			assert.equal(r.editor.getText(), "x", "a first press again");
			assert.match(shown(r.editor), CLEAR_HINT);
			r.clock.now += 501;
		}
	} finally { r.editor.dispose(); }
});

test("a bash-mode draft (`!`, leading blanks allowed, as pi reads it) keeps pi's Esc", () => {
	for (const draft of ["!ls", "  !ls"]) {
		const r = rig();
		r.editor.setText(draft);
		r.editor.handleInput("\x1b");
		assert.equal(r.stops(), 1, draft);
		assert.equal(r.editor.getText(), draft);
		assert.doesNotMatch(shown(r.editor), CLEAR_HINT);
		r.editor.dispose();
	}
});

// The Double Esc row in Customize.

test("Customize's Double Esc row writes on and off to the settings", async () => {
	const s = shell();
	const { ctx, ui } = session();
	await customize(s, ctx, ui, "Double Esc", "on");
	assert.equal(saved(s.home, "double-esc"), "on");
	await customize(s, ctx, ui, "Double Esc", "off");
	assert.equal(saved(s.home, "double-esc"), "off");
});

test("turning Double Esc on in Customize changes the prompt that is already there", async () => {
	const r = rig();
	r.working();
	r.editor.handleInput("\x1b");
	assert.equal(r.stops(), 1);
	await customize(r.s, r.ctx, r.ui, "Double Esc", "on");
	r.editor.handleInput("\x1b");
	assert.equal(r.stops(), 1, "now the first Esc waits for a second");
	assert.match(shown(r.editor), STOP_HINT);
	r.editor.dispose();
});

test("Customize reads Double Esc again when it opens, so its row and the gate agree", async () => {
	const r = rig();
	r.working();
	// Another session, or a hand edit, turned it on.
	homeSettings(r.s.home).jhon.file["shell.behaviour.double_esc_cancel"] = true;
	const open = r.s.commands.get("jhon:customize")!.handler("behaviour", r.ctx);
	const lines = () => r.ui.view!.render(120).join("\n");
	for (let i = 0; i < 30 && !lines().includes("▸ Double Esc"); i++) r.ui.view!.handleInput("\x1b[B");
	assert.match(lines(), /Double Esc to cancel\s+● on/);
	r.ui.view!.handleInput("\x1b[C");
	r.ui.view!.handleInput("\x1b");
	await open;
	assert.equal(saved(r.s.home, "double-esc"), "off");
	r.editor.handleInput("\x1b");
	assert.equal(r.stops(), 1, "off: the first Esc stops");
	r.editor.dispose();
});
