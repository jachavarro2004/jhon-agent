/**
 * What esc does in the prompt (lib/ui/double-esc-cancel-policy.ts, T174-S8):
 * the shell.behaviour.double_esc_cancel setting and the gate that decides one
 * press. The prompt's own esc behaviour, end to end, is test/ui-shell.test.ts's.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-double-esc-cancel-policy.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { join } from "node:path";
import test from "node:test";
import {
	createEscGate,
	DOUBLE_ESC_CANCEL_KEY,
	DOUBLE_ESC_CANCEL_WINDOW_MS,
	extractQueuedText,
	IDLE_ESC_CLEAR_WINDOW_MS,
	loadDoubleEscCancelPolicy,
	writeDoubleEscCancelPolicy,
	type EscPress,
} from "../lib/ui/double-esc-cancel-policy.ts";
import { fakeSettings } from "./fake-jhon-settings.ts";

test("stopping a turn waits a full second for the second esc; clearing a draft, pi's own half second", () => {
	assert.equal(DOUBLE_ESC_CANCEL_WINDOW_MS, 1000);
	assert.equal(IDLE_ESC_CLEAR_WINDOW_MS, 500);
	assert.equal(DOUBLE_ESC_CANCEL_KEY, "shell.behaviour.double_esc_cancel");
});

// --- the setting ------------------------------------------------------------------

test("the setting is off with nothing saved, on when the settings file or the environment (as jhon resolved it) says so", (t) => {
	assert.equal(loadDoubleEscCancelPolicy({ settings: fakeSettings(t).settings }), "off");
	assert.equal(loadDoubleEscCancelPolicy({ settings: fakeSettings(t, { file: { [DOUBLE_ESC_CANCEL_KEY]: true } }).settings }), "on");
	assert.equal(loadDoubleEscCancelPolicy({ settings: fakeSettings(t, { file: { [DOUBLE_ESC_CANCEL_KEY]: true }, env: { [DOUBLE_ESC_CANCEL_KEY]: false } }).settings }), "off", "the environment wins");
});

test("unreadable settings leave the setting off", (t) => {
	const fake = fakeSettings(t, { file: { [DOUBLE_ESC_CANCEL_KEY]: true } });
	fake.failNext("settings.toml: expected '=' at line 1");
	fake.settings.refresh();
	assert.equal(loadDoubleEscCancelPolicy({ settings: fake.settings }), "off");
});

test("writing the setting goes through jhon settings set and reads back", (t) => {
	const fake = fakeSettings(t);
	assert.equal(writeDoubleEscCancelPolicy("on", { settings: fake.settings }), join(fake.dir, "settings.toml"));
	assert.deepEqual(fake.file(), { [DOUBLE_ESC_CANCEL_KEY]: true });
	assert.ok(fake.calls().some((call) => call.join(" ") === `settings set ${DOUBLE_ESC_CANCEL_KEY} true`));
	assert.equal(loadDoubleEscCancelPolicy({ settings: fake.settings }), "on");
	writeDoubleEscCancelPolicy("off", { settings: fake.settings });
	assert.equal(loadDoubleEscCancelPolicy({ settings: fake.settings }), "off");
});

// --- the gate ---------------------------------------------------------------------

function gate() {
	const clock = { now: 0 };
	return { clock, esc: createEscGate(() => clock.now) };
}
const working = (over: Partial<EscPress> = {}): EscPress => ({ working: true, completing: false, doubleEsc: true, text: "", ...over });
const idle = (text: string, over: Partial<EscPress> = {}): EscPress => ({ working: false, completing: false, doubleEsc: true, text, ...over });

test("working with double-esc off, one esc stops the turn", () => {
	const { esc } = gate();
	assert.equal(esc.press(working({ doubleEsc: false })), "stop");
	assert.equal(esc.hint(true, ""), undefined);
});

test("working with double-esc on: the first esc arms for a second, the second within it stops; after it, a fresh first press", () => {
	const { clock, esc } = gate();
	assert.equal(esc.press(working()), "arm");
	assert.equal(esc.window(), DOUBLE_ESC_CANCEL_WINDOW_MS);
	assert.equal(esc.hint(true, ""), "stop");
	clock.now = 999;
	assert.equal(esc.press(working()), "stop");
	assert.equal(esc.hint(true, ""), undefined, "the stop closes the window");
	assert.equal(esc.press(working()), "arm");
	clock.now += DOUBLE_ESC_CANCEL_WINDOW_MS;
	assert.equal(esc.hint(true, ""), undefined, "the window closed");
	assert.equal(esc.press(working()), "arm");
});

test("with pi's completion list open, esc is pi's: it closes the list", () => {
	const { esc } = gate();
	assert.equal(esc.press(working({ completing: true })), "pass");
	assert.equal(esc.press(idle("draft", { completing: true })), "pass");
	assert.equal(esc.hint(true, ""), undefined);
	assert.equal(esc.hint(false, "draft"), undefined);
});

test("idle, an empty prompt and a bash-mode draft are pi's (tree/fork, bash mode)", () => {
	const { esc } = gate();
	assert.equal(esc.press(idle("")), "pass");
	assert.equal(esc.press(idle("   ")), "pass");
	assert.equal(esc.press(idle(" !ls")), "pass");
});

test("idle with a draft: the first esc arms for half a second, the second clears only the same draft", () => {
	const { clock, esc } = gate();
	assert.equal(esc.press(idle("draft")), "arm");
	assert.equal(esc.window(), IDLE_ESC_CLEAR_WINDOW_MS);
	assert.equal(esc.hint(false, "draft"), "clear");
	assert.equal(esc.hint(false, "drafts"), undefined, "an edited draft is not the one armed");
	assert.equal(esc.press(idle("drafts")), "arm", "a fresh first press on the new text");
	assert.equal(esc.press(idle("drafts")), "clear");
	assert.equal(esc.press(idle("again")), "arm");
	clock.now += IDLE_ESC_CLEAR_WINDOW_MS;
	assert.equal(esc.press(idle("again")), "arm", "after the window, a fresh first press");
	esc.other();
	assert.equal(esc.hint(false, "again"), undefined, "any other key disarms");
	assert.equal(esc.press(idle("again")), "arm");
});

test("a turn starting or ending disarms every press; working never shows the clear hint", () => {
	const { esc } = gate();
	esc.press(idle("draft"));
	esc.reset();
	assert.equal(esc.hint(false, "draft"), undefined);
	esc.press(working());
	assert.equal(esc.hint(false, ""), undefined);
	esc.reset();
	assert.equal(esc.hint(true, ""), undefined);
	esc.press(idle("draft"));
	assert.equal(esc.press(working()), "arm", "working decides by its own window");
	assert.equal(esc.hint(true, "draft"), "stop");
});

// --- the queue pi puts back -------------------------------------------------------

test("extractQueuedText takes pi's queued messages back out of what it restored", () => {
	assert.equal(extractQueuedText("draft", "draft"), "", "nothing queued");
	assert.equal(extractQueuedText("", ""), "");
	assert.equal(extractQueuedText("queued", ""), "queued", "an empty draft: everything restored was queued");
	assert.equal(extractQueuedText("queued", "  "), "queued", "a blank draft is empty to pi too");
	assert.equal(extractQueuedText("one\n\ntwo\n\ndraft", "draft"), "one\n\ntwo");
	assert.equal(extractQueuedText("something else", "draft"), undefined, "not pi's shape: leave it");
});
