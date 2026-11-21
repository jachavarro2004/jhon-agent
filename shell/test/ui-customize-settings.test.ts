import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { CARD_STYLE, cardStyle, setCardStyle } from "../lib/ui/primitives/index.ts";
import { chromeSurface, setChromeSurface } from "../lib/ui/shell-panel.ts";
import { customize, freshHome, homeSettings, prompt, rows, saved, session, shell, tui } from "./shell-harness.ts";

// /jhon:customize writing the settings the prompt and the cards read: every
// change is a `jhon settings set` (S38) and applies at once.

test("Customize registers once, as one command, in place of the old ones", () => {
	const s = shell();
	assert.equal(s.registered.filter((name) => name === "jhon:customize").length, 1);
	for (const gone of ["jhon:vim", "jhon:animations", "jhon:double-esc-cancel", "jhon:settings"]) assert.equal(s.commands.has(gone), false, gone);
	assert.equal(s.commands.get("jhon:customize")!.description, "Customize the shell: appearance, behaviour, keys, permissions");
});

test("Animations off is written as `shell.appearance.animations off`, and nothing else", async () => {
	const s = shell();
	const { ctx, ui } = session();
	const editor = prompt(s, ctx, ui);
	let before: string[];
	try { before = await customize(s, ctx, ui, "Animations", "off"); } finally { editor.dispose(); }
	assert.ok(before.some((row) => /Animations.*off/.test(row)), before.join("\n"));
	assert.deepEqual(s.settings.jhon.sets, [["shell.appearance.animations", "off"]]);
	assert.deepEqual(s.settings.jhon.file, { "shell.appearance.animations": "off" });
	assert.equal(saved(s.home, "animations"), "potato");
});

test("Animations stops and starts the working line's pulse at once; the polls go on", async (t) => {
	const delays: number[] = [];
	let running = 0;
	t.mock.method(globalThis, "setInterval", (_fn: () => void, delay: number) => { delays.push(delay); running++; return { unref() {} }; });
	t.mock.method(globalThis, "clearInterval", () => { running--; });
	const s = shell();
	const { ctx, ui } = session();
	const editor = prompt(s, ctx, ui);
	s.emit("agent_start", ctx);
	assert.deepEqual(delays, [2000, 60_000, 80], "the profile poll, STATUS's minute and the pulse");
	assert.equal(running, 3);
	await customize(s, ctx, ui, "Animations", "off");
	assert.equal(running, 2);
	assert.match(rows(editor)[0]!, /Working…/);
	await customize(s, ctx, ui, "Animations", "full");
	assert.deepEqual(delays, [2000, 60_000, 80, 80]);
	s.emit("agent_settled", ctx);
	assert.equal(running, 2);
	editor.dispose();
	await s.fire("session_shutdown", ctx);
	assert.equal(running, 0);
});

test("with animations off, the working state still shows on the next frame, with no timer of its own", async (t) => {
	const home = freshHome();
	homeSettings(home, { "shell.appearance.animations": "off" });
	const timers = t.mock.method(globalThis, "setInterval", (_fn: () => void, delay: number) => {
		assert.ok(delay === 2000 || delay === 60_000, `no animation timer (${delay})`);
		return { unref() {} };
	});
	const frames = t.mock.method(tui, "requestRender", () => {});
	const s = shell({ JHON_SHELL_CONFIG_HOME: home });
	let queued = false;
	const { ctx, ui } = session({ pending: () => queued });
	const editor = prompt(s, ctx, ui);
	let before = frames.mock.callCount();
	s.emit("agent_start", ctx);
	assert.ok(frames.mock.callCount() > before, "the start repaints");
	assert.match(rows(editor)[0]!, /力 Working…/);
	before = frames.mock.callCount();
	queued = true;
	assert.equal(frames.mock.callCount(), before, "a queued message is not a repaint of the shell's");
	// pi repaints when it queues; this stands for that next frame.
	tui.requestRender();
	assert.match(rows(editor)[0]!, /· queued/);
	before = frames.mock.callCount();
	s.emit("agent_settled", ctx);
	assert.ok(frames.mock.callCount() > before, "the settle repaints");
	assert.doesNotMatch(rows(editor)[0]!, /Working|queued/);
	s.emit("session_shutdown", ctx);
	assert.equal(timers.mock.callCount(), 2);
});

test("a write the settings refuse is a `Customize:` notice, and nothing is saved", async () => {
	const s = shell();
	const { ctx, ui } = session();
	s.settings.jhon.failSet = "EPERM: settings.toml is not writable";
	const before = await customize(s, ctx, ui, "Animations", "off");
	assert.match(ui.notices.join("\n"), /^Customize: .*EPERM/m);
	assert.doesNotMatch(before.join("\n"), /EPERM/);
	assert.deepEqual(s.settings.jhon.file, {});
});

// T15.12d (L153 answers 1-2): Cards and Surface are Appearance rows.
test("Cards and Surface are written, apply at once, and are what the next session starts with", async (t) => {
	t.after(() => { setCardStyle(CARD_STYLE.FLOAT); setChromeSurface("transparent"); });
	const s = shell();
	const { ctx, ui } = session();
	const editor = prompt(s, ctx, ui);
	try {
		await customize(s, ctx, ui, "Cards", "neon");
		assert.equal(cardStyle(), CARD_STYLE.NEON);
		await customize(s, ctx, ui, "Surface", "tinted");
		assert.equal(chromeSurface(), "tinted");
	} finally { editor.dispose(); }
	assert.deepEqual(s.settings.jhon.sets, [["shell.appearance.cards", "neon"], ["shell.appearance.surface", "tinted"]]);
	setCardStyle(CARD_STYLE.FLOAT);
	setChromeSurface("transparent");
	await s.fire("session_start", ctx);
	assert.equal(cardStyle(), CARD_STYLE.NEON);
	assert.equal(chromeSurface(), "tinted");
});

test("outside an interactive terminal Customize opens nothing and points at `jhon settings`", async () => {
	const s = shell();
	const rpc = session();
	(rpc.ctx as unknown as { mode: string }).mode = "rpc";
	await s.commands.get("jhon:customize")!.handler("", rpc.ctx);
	assert.equal(rpc.ui.view, undefined);
	assert.deepEqual(rpc.ui.notices, ["Customize needs an interactive terminal; `jhon settings` lists and sets them."]);
	const headless = session({ hasUI: false });
	await s.commands.get("jhon:customize")!.handler("", headless.ctx);
	assert.deepEqual(headless.ui.notices, [], "no UI: not even a notice");
});
