/**
 * T174-S5b (L1493-L1495): the agent's todo list, while no LDD change is
 * active, folds with alt+t (shell.keys.todos, named on its title), scrolls
 * with alt+PgUp/PgDn, says `stale · N turns` when the agent let it sit, and
 * shows in the right pane, or, in a regular terminal and the narrow form,
 * above the prompt the way the Running block does. Nothing shows without
 * tasks.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-todo-keys.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test, { type TestContext } from "node:test";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { Component, TUI } from "@earendil-works/pi-tui";
import jhonTodo from "../extensions/todo.ts";
import { paneState, PANE_TEXT } from "../lib/ui/pane.ts";
import { lddFromStatus, selectChange, shareSelection } from "../lib/ui/ldd-status.ts";
import { stripAnsi } from "../lib/ui/primitives/index.ts";
import { padded, plainTheme, spread } from "./rows.ts";
import { useMemorySettings } from "./fake-jhon-settings.ts";

type Handler = (event: unknown, ctx: ExtensionContext) => unknown;

function harness(t: TestContext, file: Record<string, string> = {}, terminalRows = 40) {
	useMemorySettings(t, file);
	const handlers = new Map<string, Handler[]>();
	const shortcuts = new Map<string, { handler(ctx: ExtensionContext): Promise<void> }>();
	const tools = new Map<string, { execute(id: string, params: unknown, signal: undefined, update: undefined, ctx: ExtensionContext): Promise<unknown> }>();
	const pi = {
		on(event: string, handler: Handler) { handlers.set(event, [...(handlers.get(event) ?? []), handler]); },
		appendEntry() {}, sendMessage() {}, registerMessageRenderer() {}, registerCommand() {},
		events: { on() { return () => {}; }, emit() {} },
		registerTool(tool: { name: string }) { tools.set(tool.name, tool as never); },
		registerShortcut(key: string, registration: never) { shortcuts.set(key, registration); },
	} as unknown as ExtensionAPI;
	jhonTodo(pi, {});
	const widgets = new Map<string, (tui: unknown, theme: unknown) => Component>();
	const ctx = {
		hasUI: true,
		sessionManager: { getSessionId: () => `todo-keys-${terminalRows}`, getBranch: () => [] },
		ui: { setWidget(key: string, content: ((tui: unknown, theme: unknown) => Component) | undefined) { if (content) widgets.set(key, content); else widgets.delete(key); } },
	} as unknown as ExtensionContext;
	const fire = async (event: string, payload: unknown = {}) => { for (const handler of handlers.get(event) ?? []) await handler(payload, ctx); };
	const write = async (tasks: Array<{ title: string; status?: string; note?: string }>) => {
		await tools.get("todo")!.execute("w", { action: "write", tasks }, undefined, undefined, ctx);
		await fire("tool_execution_end", { toolName: "todo" });
	};
	const press = (key: string) => shortcuts.get(key)!.handler(ctx);
	/** A terminal: regular (no pane host), the narrow form (hosted, not wide) or wide (the pane beside the conversation). */
	const terminal = (form: "regular" | "narrow" | "wide") => {
		const tui = { requestRender() {}, terminal: { rows: terminalRows, columns: form === "wide" ? 180 : 100 } } as unknown as TUI;
		if (form !== "regular") {
			paneState(tui).hosted = () => true;
			paneState(tui).wide = () => form === "wide";
		}
		const widget = [...widgets.values()].at(-1)!(tui, plainTheme);
		return {
			above: (width: number) => widget.render(width).map(stripAnsi),
			pane: () => (paneState(tui).parts.get("todo")?.render(PANE_TEXT) ?? []).map(stripAnsi),
			click: (y: number) => paneState(tui).parts.get("todo")?.handleMouse?.({ type: "click", button: "left", x: 2, y, width: PANE_TEXT, height: 4 } as never),
		};
	};
	return { fire, write, press, shortcuts, terminal };
}

const TWO = [{ title: "write the test", status: "in_progress", note: "writing" }, { title: "fix tsc" }];

test("the right pane's TODOS names its fold key on the title row; alt+t folds it to the current task and back", async (t) => {
	const h = harness(t);
	await h.fire("session_start");
	await h.write(TWO);
	assert.ok(h.shortcuts.has("alt+t"), "alt+t is registered");
	const wide = h.terminal("wide");
	assert.deepEqual(wide.above(100), [], "wide, nothing above the prompt: the pane has it");
	assert.deepEqual(wide.pane(), [
		spread("▾ TODOS", "alt+t  0/2", PANE_TEXT),
		padded("  ◐ write the test · writing", PANE_TEXT),
		padded("  · fix tsc", PANE_TEXT),
	]);
	await h.press("alt+t");
	assert.deepEqual(wide.pane(), [spread("▸ TODOS", "alt+t  0/2", PANE_TEXT), padded("  ◐ write the test · writing", PANE_TEXT)], "folded: the task in progress");
	await h.press("alt+t");
	assert.equal(wide.pane().length, 3, "alt+t again unfolds it");
	wide.click(0);
	assert.equal(wide.pane().length, 2, "a click on the title row folds it too");
});

test("in a regular terminal and in the narrow form the list sits above the prompt, as the Running block does", async (t) => {
	const h = harness(t);
	await h.fire("session_start");
	await h.write(TWO);
	for (const form of ["regular", "narrow"] as const) {
		const terminal = h.terminal(form);
		assert.deepEqual(terminal.above(100), [
			`── ▾ Todos  alt+t · 0/2 ${"─".repeat(76)}`,
			padded("  ◐ write the test · writing", 100),
			padded("  · fix tsc", 100),
			padded("", 100),
		], form);
	}
	await h.press("alt+t");
	assert.deepEqual(h.terminal("regular").above(100), [`── ▸ Todos  alt+t · 0/2 ${"─".repeat(76)}`, padded("  ◐ write the test · writing", 100), padded("", 100)], "folded above the prompt too");
});

test("a list the agent let sit for two turns says `stale · N turns`, on the pane's title and the block's rule", async (t) => {
	const h = harness(t);
	await h.fire("session_start");
	await h.write(TWO);
	await h.fire("before_agent_start");
	assert.doesNotMatch(h.terminal("wide").pane()[0]!, /stale/, "one turn is not stale");
	await h.fire("before_agent_start");
	assert.equal(h.terminal("wide").pane()[0], spread("▾ TODOS", "alt+t  stale · 2 turns · 0/2", PANE_TEXT));
	assert.equal(h.terminal("narrow").above(100)[0], `── ▾ Todos  alt+t · stale · 2 turns · 0/2 ${"─".repeat(58)}`);
	await h.write(TWO);
	assert.doesNotMatch(h.terminal("wide").pane()[0]!, /stale/, "a write makes it fresh");
});

test("a long list keeps to a third of the screen; alt+PgUp/PgDn scroll it three rows, and a row says where it is", async (t) => {
	const h = harness(t, {}, 30);
	await h.fire("session_start");
	await h.write(Array.from({ length: 12 }, (_, i) => ({ title: `task ${i + 1}` })));
	assert.ok(h.shortcuts.has("alt+pageUp") && h.shortcuts.has("alt+pageDown"));
	const wide = h.terminal("wide");
	const rows = () => wide.pane();
	assert.equal(rows().length, 10, "at most a third of the 30 rows");
	assert.deepEqual(rows().slice(1, 3), [padded("  · task 1", PANE_TEXT), padded("  · task 2", PANE_TEXT)]);
	assert.equal(rows().at(-1), padded("  ↑↓ 1–8/12 · alt+PgUp/PgDn", PANE_TEXT));
	await h.press("alt+pageDown");
	assert.equal(rows()[1], padded("  · task 4", PANE_TEXT));
	assert.equal(rows().at(-1), padded("  ↑↓ 4–11/12 · alt+PgUp/PgDn", PANE_TEXT));
	await h.press("alt+pageDown");
	assert.equal(rows().at(-1), padded("  ↑↓ 5–12/12 · alt+PgUp/PgDn", PANE_TEXT), "it stops at the end");
	await h.press("alt+pageUp");
	assert.equal(rows().at(-1), padded("  ↑↓ 2–9/12 · alt+PgUp/PgDn", PANE_TEXT));
	const above = h.terminal("narrow").above(100);
	assert.equal(above.length, 10, "above the prompt the same third");
	assert.equal(above[1], padded("  · task 2", 100), "with the same position");
	assert.equal(above.at(-2), padded("  ↑↓ 2–8/12 · alt+PgUp/PgDn", 100));
	assert.equal(above.at(-1), padded("", 100));
});

test("nothing shows without tasks, nor while an LDD change is active; with shell.keys.todos off the title names no key", async (t) => {
	const h = harness(t, { "shell.keys.todos": "off" });
	await h.fire("session_start");
	assert.deepEqual(h.terminal("wide").pane(), [], "no tasks: no section");
	assert.deepEqual(h.terminal("regular").above(100), [], "no tasks: no block");
	assert.ok(!h.shortcuts.has("alt+t"), "the key is off");
	await h.write(TWO);
	assert.equal(h.terminal("wide").pane()[0], spread("▾ TODOS", "0/2", PANE_TEXT));
	assert.equal(h.terminal("regular").above(100)[0], `── ▾ Todos  0/2 ${"─".repeat(84)}`);
	const change = lddFromStatus("rail-redesign", { route: { level: "verified" }, next: "apply", tasks: { total: 1, completed: 0, slice: "Slice 1: rail", slices: [{ name: "Slice 1: rail", total: 1, completed: 0, tasks: [{ text: "Draw rows", done: false }] }] } });
	const withdraw = shareSelection("todo-keys-40", async () => change);
	t.after(withdraw);
	selectChange("todo-keys-40", change);
	assert.deepEqual(h.terminal("wide").pane(), [], "an LDD change rules");
	assert.deepEqual(h.terminal("regular").above(100), [], "above the prompt too");
	selectChange("todo-keys-40", undefined);
	assert.equal(h.terminal("regular").above(100).length, 4, "the change gone, the list is back");
});
