/**
 * ask_user through the question view (lib/ui/ask-view.ts, design/shell-tui
 * 08): put in the dock with ui.custom, keys and clicks reach it, the answers
 * read as through pi's dialogs; a UI without ui.custom asks with pi's select
 * dialog. The view's rows are pinned in ui-overlay-rows.test.ts, what the
 * model reads in ask-model-text.test.ts.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { askUser, OTHER } from "../lib/ask.ts";
import { AskView } from "../lib/ui/ask-view.ts";
import { DockedView } from "../lib/ui/overlay.ts";

const tagged = { fg: (c: string, t: string) => `<${c}>${t}</${c}>`, bold: (t: string) => `<b>${t}</b>` };
const plain = { fg: (_c: string, t: string) => t, bold: (t: string) => t };
const DOWN = "\x1b[B";
const ENTER = "\r";

test("a multiple choice shows its [x] marks on the labels and its hint names space; space toggles the focused one", () => {
	const picked: Array<string | undefined> = [];
	const multi = new AskView({ step: 1, total: 2, title: "Pick", question: "Which?", multi: true, theme: plain, done: (v) => picked.push(v),
		items: [{ value: "[x] A", label: "[x] A" }, { value: "[ ] B", label: "[ ] B" }] });
	const rows = multi.render(60).map((l) => l.trimEnd());
	assert.deepEqual(rows.slice(4, 6), [" ❯ [x] A", "   [ ] B"]);
	assert.equal(rows.at(-2), " ↑↓ move · space toggle · enter select · esc cancel");
	multi.handleInput(DOWN);
	multi.handleInput(" ");
	assert.deepEqual(picked, ["[ ] B"]);
});

test("a long label wraps under itself and a long description under its option, four cells in", () => {
	const long = new AskView({ step: 1, total: 1, title: "Route", question: "Which route?", multi: false, theme: plain, done() {},
		items: [{ value: "v", label: "verified with a long name", description: "adds verify-report.md before delivery" }] });
	assert.deepEqual(long.render(24).map((l) => l.trimEnd()).slice(4, 9), [" ❯ verified with a long", "   name", "     adds", "     verify-report.md", "     before delivery"]);
});

test("ask_user puts a choice in the dock through ui.custom: the header in the step row, and the answer read as before", async () => {
	const shown: string[][] = [];
	const ui = {
		custom: (factory: any, options?: unknown) => new Promise((resolve) => {
			assert.equal(options, undefined, "in the dock, never an overlay");
			const component = factory({ requestRender() {} }, tagged, undefined, resolve);
			shown.push(component.render(60));
			component.handleInput(DOWN);
			component.handleInput(ENTER);
		}),
		select: () => { throw new Error("a UI with ui.custom asks through the view"); },
		input: async () => "typed",
	};
	const outcome = await askUser({ questions: [{ question: "Which route?", header: "Route", options: [{ label: "A", description: "the first" }, { label: "B" }] }] }, { hasUI: true, ui }, false);
	assert.deepEqual(outcome, { ok: true, text: "The owner answered:\n- Which route? → B" });
	assert.match(shown[0]![1]!, /<dim>\[1\/1\]<\/dim>  <accent><b>▸ Route<\/b><\/accent>/);
	assert.match(shown[0]![7]!, new RegExp(`^ {3}<text>${OTHER.replace(".", "\\.")}</text> +$`), "the typed answer's row comes last");
});

test("in the dock, each key reaches the view and asks for a render, and a click on an option picks it", async () => {
	let renders = 0;
	const ui = {
		custom: (factory: any) => new Promise((resolve) => {
			const docked = factory({ requestRender() { renders++; } }, plain, undefined, resolve);
			assert.ok(docked instanceof DockedView);
			docked.handleInput(DOWN);
			assert.equal(renders, 1);
			const lines: string[] = docked.render(60);
			assert.ok(lines.some((line) => line.trimEnd() === " ❯ B"), "the key moved the focus to B");
			const y = lines.findIndex((line) => line.trimEnd() === "   C");
			assert.ok(y > 0, JSON.stringify(lines));
			const click = (type: "press" | "click") => ({ type, button: "left", x: 4, y, screenX: 4, screenY: y, width: 60, height: lines.length, shift: false, alt: false, ctrl: false }) as never;
			assert.ok(docked.handleMouse(click("press")), "the press is taken, so pi sends the click");
			docked.handleMouse(click("click"));
		}),
		select: () => { throw new Error("a UI with ui.custom asks through the view"); },
		input: async () => "typed",
	};
	const outcome = await askUser({ questions: [{ question: "Which route?", options: [{ label: "A" }, { label: "B" }, { label: "C" }] }] }, { hasUI: true, ui }, false);
	assert.deepEqual(outcome, { ok: true, text: "The owner answered:\n- Which route? → C" });
});

test("an abort of the call dismisses the view in the dock", async () => {
	const controller = new AbortController();
	const ui = {
		custom: (factory: any) => new Promise((resolve) => { factory({ requestRender() {} }, plain, undefined, resolve); controller.abort(); }),
		input: async () => "x",
	};
	const outcome = await askUser({ questions: [{ question: "First?", options: [{ label: "a" }, { label: "b" }] }] }, { hasUI: true, ui }, false, controller.signal);
	assert.deepEqual(outcome, { ok: true, text: "The owner dismissed \"First?\" without answering, and was asked nothing more." });
});

test("the typed answer's row reads «Write my own answer.» (T170)", () => {
	assert.equal(OTHER, "Write my own answer.");
});

test("a UI without ui.custom asks through pi's select dialog", async () => {
	const titles: string[] = [];
	const ui = { select: async (title: string, options: string[]) => { titles.push(title); return options[0]; }, input: async () => "" };
	const outcome = await askUser({ questions: [{ question: "Which route?", options: [{ label: "A" }, { label: "B" }] }] }, { hasUI: true, ui }, false);
	assert.deepEqual(outcome, { ok: true, text: "The owner answered:\n- Which route? → A" });
	assert.deepEqual(titles, ["Which route?"]);
});
