/**
 * ask_user drawn as the Gentle Shell's questionnaire (its ask_user_question,
 * lib/questionnaire/questionnaire-view.ts; T15.12e): in the dock, between two
 * accent rules, `[n/m]  ▸ Header`, the question bold accent, `❯` on the
 * focused option with its description dim under it, and the dim hint
 * `↑↓ move · enter select · esc cancel`. A UI without ui.custom gets pi's own
 * select dialog.
 */
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { askUser, OTHER } from "../lib/ask.ts";
import { AskView } from "../lib/ui/ask-view.ts";
import { NativeFullscreenInteraction } from "../lib/ui/native-fullscreen-interaction.ts";

const tagged = { fg: (c: string, t: string) => `<${c}>${t}</${c}>`, bold: (t: string) => `<b>${t}</b>` };
const plain = { fg: (_c: string, t: string) => t, bold: (t: string) => t };
const DOWN = "\x1b[B";
const UP = "\x1b[A";
const ENTER = "\r";
const ESC = "\x1b";

function view(done: (value: string | undefined) => void = () => {}, theme: typeof tagged = tagged) {
	return new AskView({
		step: 1, total: 2, title: "Route", question: "Which route?",
		items: [{ value: "A — the first", label: "A", description: "the first" }, { value: "B", label: "B" }, { value: OTHER, label: OTHER }],
		multi: false, theme, done,
	});
}

test("the question view is G's questionnaire: accent rules, `[n/m]  ▸ Header`, the question, ❯ on the focus, descriptions dim under their option, G's hint", () => {
	const lines = view(undefined, plain).render(48).map((line) => line.trimEnd());
	assert.deepEqual(lines, [
		"─".repeat(48),
		"[1/2]  ▸ Route",
		"",
		"Which route?",
		"❯ A",
		"    the first",
		"  B",
		`  ${OTHER}`,
		"",
		"↑↓ move · enter select · esc cancel",
		"─".repeat(48),
	]);
	const painted = view().render(200).join("\n");
	assert.match(painted, /<accent>─+<\/accent>/, "the rules in the accent");
	assert.match(painted, /<dim>\[1\/2\]<\/dim>  <accent><b>▸ Route<\/b><\/accent>/);
	assert.match(painted, /<accent><b>Which route\?<\/b><\/accent>/, "the question bold accent");
	assert.match(painted, /<accent><b>❯ <\/b><\/accent>A/);
	assert.match(painted, /<dim>the first<\/dim>/);
	assert.match(painted, /<dim>↑↓ move · enter select · esc cancel<\/dim>/);
	for (const width of [1, 5, 12, 40]) for (const line of view(undefined, plain).render(width)) assert.ok(visibleWidth(line) <= width, `${width}: ${JSON.stringify(line)}`);
});

test("a multiple choice keeps its [x] marks; space toggles as enter does, as G's hint says", () => {
	const picked: Array<string | undefined> = [];
	const multi = new AskView({ step: 1, total: 2, title: "Pick", question: "Which?", multi: true, theme: plain, done: (v) => picked.push(v),
		items: [{ value: "[x] A", label: "[x] A" }, { value: "[ ] B", label: "[ ] B" }] });
	const rows = multi.render(60).map((l) => l.trimEnd());
	assert.equal(rows[4], "❯ [x] A");
	assert.equal(rows[5], "  [ ] B");
	assert.equal(rows.at(-2), "↑↓ move · space toggle · enter select · esc cancel");
	multi.handleInput(DOWN);
	multi.handleInput(" ");
	assert.deepEqual(picked, ["[ ] B"]);
});

test("a long description wraps under its option at G's four-cell indent", () => {
	const long = new AskView({ step: 1, total: 1, title: "Route", question: "Which route?", multi: false, theme: plain, done() {},
		items: [{ value: "v", label: "verified", description: "adds verify-report.md before delivery" }] });
	const rows = long.render(30).map((l) => l.trimEnd());
	assert.deepEqual(rows.slice(4, 7), ["❯ verified", "    adds verify-report.md", "    before delivery"]);
});

test("arrows move the focus, enter picks the focused option's value, esc dismisses; a click picks, a move paints the hovered label", () => {
	const picked: Array<string | undefined> = [];
	const v = view((value) => picked.push(value), plain);
	v.handleInput(DOWN);
	assert.equal(v.render(40)[6]!.trimEnd(), "❯ B");
	v.handleInput(DOWN);
	v.handleInput(DOWN);
	assert.equal(v.render(40)[7]!.trimEnd(), `❯ ${OTHER}`, "the focus stops at the last option");
	v.handleInput(UP);
	v.handleInput(ENTER);
	assert.deepEqual(picked, ["B"]);
	const dismissed: Array<string | undefined> = [];
	view((value) => dismissed.push(value), plain).handleInput(ESC);
	assert.deepEqual(dismissed, [undefined]);
	const clicked: Array<string | undefined> = [];
	const c = view((value) => clicked.push(value), tagged);
	c.render(200);
	const event = (type: "move" | "click", y: number) => ({ type, button: type === "move" ? "none" : "left", x: 3, y, width: 200, height: 11, shift: false, alt: false, ctrl: false }) as never;
	assert.deepEqual(c.handleMouse(event("move", 6)), { handled: true, render: true });
	assert.match(c.render(200)[6]!, /<warning>B<\/warning>/);
	c.handleMouse(event("click", 6));
	assert.deepEqual(clicked, ["B"]);
});

test("ASCII: the rules -, the cursor >, the chip >", () => {
	const ascii = new AskView({ step: 1, total: 1, title: "Route", question: "Which?", multi: false, theme: plain, done() {}, ascii: true,
		items: [{ value: "A", label: "A", description: "the first" }, { value: "B", label: "B" }] });
	const rows = ascii.render(60).map((l) => l.trimEnd());
	assert.equal(rows[0], "-".repeat(60));
	assert.equal(rows[1], "[1/1]  > Route");
	assert.equal(rows[4], "> A");
	assert.equal(rows.at(-2), "up/down move - enter select - esc cancel");
});

test("ask_user asks through the question view, in the dock as G's; the answers read as before", async (t) => {
	const shown: string[][] = [];
	const ui = {
		custom: (factory: any) => new Promise((resolve) => {
			const component = factory({ requestRender() {} }, tagged, undefined, resolve);
			shown.push(component.render(60));
			component.handleInput(DOWN);
			component.handleInput(ENTER);
		}),
		select: () => { throw new Error("sections asks through its own view"); },
		input: async () => "typed",
	};
	const outcome = await askUser({ questions: [{ question: "Which route?", header: "Route", options: [{ label: "A", description: "the first" }, { label: "B" }] }] }, { hasUI: true, ui }, false);
	assert.deepEqual(outcome, { ok: true, text: "The owner answered:\n- Which route? → B" });
	assert.match(shown[0]![1]!, /<dim>\[1\/1\]<\/dim>  <accent><b>▸ Route<\/b><\/accent>/);
});

test("ask_user's view is wrapped in G's native fullscreen interaction: keys and clicks reach it, each key asks for a render", async () => {
	let renders = 0;
	let wrapper: any;
	const ui = {
		custom: (factory: any) => new Promise((resolve) => {
			wrapper = factory({ requestRender() { renders++; } }, plain, undefined, resolve);
			assert.ok(wrapper instanceof NativeFullscreenInteraction, "G's ask_user_question returns createNativeFullscreenInteraction");
			wrapper.handleInput(DOWN);
			assert.equal(renders, 1, "a key goes to the view and asks for a render");
			const lines: string[] = wrapper.render(60);
			const y = lines.findIndex((line) => /^(  |❯ )B$/.test(line.trimEnd()));
			assert.ok(y > 0, JSON.stringify(lines));
			const click = (type: "press" | "click") => ({ type, button: "left", x: 3, y, screenX: 3, screenY: y, width: 60, height: lines.length, shift: false, alt: false, ctrl: false }) as never;
			wrapper.handleMouse(click("press"));
			wrapper.handleMouse(click("click"));
		}),
		select: () => { throw new Error("sections asks through its own view"); },
		input: async () => "typed",
	};
	const outcome = await askUser({ questions: [{ question: "Which route?", options: [{ label: "A" }, { label: "B" }, { label: "C" }] }] }, { hasUI: true, ui }, false);
	assert.deepEqual(outcome, { ok: true, text: "The owner answered:\n- Which route? → B" }, "the click picked B");
});

test("the typed-answer row reads as G's: «Type something.»", () => {
	assert.equal(OTHER, "Type something.");
});

test("a UI without ui.custom asks through pi's select dialog", async () => {
	const titles: string[] = [];
	const ui = { select: async (title: string, options: string[]) => { titles.push(title); return options[0]; }, input: async () => "" };
	const outcome = await askUser({ questions: [{ question: "Which route?", options: [{ label: "A" }, { label: "B" }] }] }, { hasUI: true, ui }, false);
	assert.deepEqual(outcome, { ok: true, text: "The owner answered:\n- Which route? → A" });
	assert.deepEqual(titles, ["Which route?"]);
});
