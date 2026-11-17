import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { convertToLlm, type ExtensionAPI, type ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { Component, TUI } from "@earendil-works/pi-tui";
import jhonTodo, { todoCollapseKey, todoEnabled } from "../extensions/todo.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { sidebarState } from "../lib/ui/shell-sidebar.ts";
import { installSidebar } from "../lib/ui/shell-sidebar-layout.ts";
import { lddFromStatus, selectChange, shareSelection } from "../lib/ui/ldd-status.ts";
import { renderShellStatusSections } from "../lib/ui/shell-bar.ts";
import { renderLayoutFrame } from "@earendil-works/pi-tui/dist/layout.js";
import { envOverrides, memorySettings } from "./fake-jhon-settings.ts";

// The settings `jhon settings` would give the shell under an environment (S38).
const envSettings = (env: NodeJS.ProcessEnv) => memorySettings({}, undefined, envOverrides(env));

// The todo extension: the `todo` tool, the card above the editor,
// the per-turn prompt block, and the staleness signal, driven by fakes.

type Handler = (event: unknown, ctx: ExtensionContext) => unknown;
type SentMessage = { customType: string; content: string; display: boolean };
// The messages each fakePi's extension sent, and how many a test has read.
const SENT = new Map<unknown, { sent: Array<{ message: SentMessage; options: unknown }>; read: number }>();

interface Registered {
	renderShell?: string;
	execute(toolCallId: string, params: unknown, signal: undefined, onUpdate: undefined, ctx: ExtensionContext): Promise<{ content: Array<{ type: string; text: string }>; details: Record<string, unknown> }>;
	renderCall(args: unknown, theme: unknown): { render(width: number): string[] };
	renderResult(result: unknown, options: { expanded: boolean }, theme: unknown): { render(width: number): string[] };
}

const plainTheme = {
	fg(_color: string, text: string) {
		return text;
	},
	strikethrough(text: string) {
		return `~${text}~`;
	},
};
const fakeTui = { requestRender() {} };
function fakePi() {
	const handlers = new Map<string, Handler[]>();
	const tools = new Map<string, Registered>();
	const shortcuts = new Map<string, { handler(ctx: ExtensionContext): Promise<void> }>();
	const pi = {
		on(event: string, handler: Handler) {
			handlers.set(event, [...(handlers.get(event) ?? []), handler]);
		},
		registerTool(tool: Registered & { name: string }) {
			tools.set(tool.name, tool);
		},
		registerShortcut(key: string, registration: { handler(ctx: ExtensionContext): Promise<void> }) {
			shortcuts.set(key, registration);
		},
		// T115d: the nudges are persisted messages the extension sends.
		sendMessage(message: unknown, options: unknown) {
			sent.push({ message: message as SentMessage, options });
		},
	} as unknown as ExtensionAPI;
	const sent: Array<{ message: SentMessage; options: unknown }> = [];
	const fire = async (event: string, ctx: ExtensionContext, payload: unknown = {}) => {
		let last: unknown;
		for (const handler of handlers.get(event) ?? []) last = await handler(payload, ctx);
		// T115d: the block a run start delivers, as a persisted message, kept on its event.
		const message = (last as { message?: SentMessage } | undefined)?.message;
		if (event === "before_agent_start" && message && payload && typeof payload === "object") (payload as { delivered?: string }).delivered = message.content;
		return last;
	};
	SENT.set(fire, { sent, read: 0 });
	return { pi, tools, shortcuts, fire };
}

function fakeContext(branch: unknown[] = [], hasUI = true) {
	const widgets = new Map<string, (tui: unknown, theme: unknown) => Component>();
	const ctx = {
		hasUI,
		sessionManager: { getSessionId: () => "s1", getBranch: () => branch },
		ui: {
			setWidget(key: string, content: ((tui: unknown, theme: unknown) => Component) | undefined) {
				if (content === undefined) widgets.delete(key);
				else widgets.set(key, content);
			},
		},
	} as unknown as ExtensionContext;
	const widgetComponent = () => {
		const factory = widgets.get("jhon-todo");
		return factory?.(fakeTui, plainTheme);
	};
	const widget = () => widgetComponent()?.render(70).map(stripAnsi);
	return { ctx, widgets, widget, widgetComponent };
}

test("long Todo widgets stay bounded, scroll, resize and keep their collapse control in mobile and sidebar", async () => {
	for (const style of ["sections"]) {
		const { pi, tools, fire, shortcuts } = fakePi();
		jhonTodo(pi, {});
		const { ctx, widgets } = fakeContext();
		await fire("session_start", ctx);
		const title = "long active title ".repeat(150);
		const note = "description ".repeat(150);
		const result = await tools.get("todo")!.execute("write", { action: "write", tasks: [{ title, note, status: "in_progress" }, { title: "LAST TASK" }] }, undefined, undefined, ctx);
		await fire("tool_execution_end", ctx, { toolName: "todo" });
		const tui = { requestRender() {}, terminal: { rows: 30, columns: 40 } };
		const theme = { ...plainTheme, bg: (_color: string, text: string) => `\x1b[48;5;22m${text}\x1b[49m` };
		const bottom = widgets.get("jhon-todo")!(tui, theme);
		const rail = sidebarState(tui as never).parts.get("todo")!;
		// The card above the prompt floats (its header on row 1); the rail's section has its header on row 0.
		for (const [component, header, title] of [[bottom, 1, /Todos ▾ Collapse/], [rail, 0, /▾ TODOS/]] as const) {
			const before = component.render(40).map(stripAnsi);
			assert.ok(before.length <= 10, "card uses at most one third of terminal height");
			// G's position row: where the view is, and the key that scrolls the todos (L136).
			const hint = before.find((row) => row.includes("ctrl+shift+↑/↓"))!;
			assert.match(hint, /^ *↑↓ 1–\d+\/\d+ · ctrl\+shift\+↑\/↓/, `${style}: the position and the key: ${JSON.stringify(hint)}`);
			assert.match(before[header]!, title);
			assert.equal(component.handleMouse?.({ type: "wheel", button: "none", wheelDelta: 10000, x: 5, y: header + 2, screenX: 5, screenY: 5, width: 40, height: before.length, shift: false, alt: false, ctrl: false })?.handled, true);
			const after = component.render(40).map(stripAnsi);
			assert.match(after[header]!, title);
			assert.match(after.join("\n"), /LAST TASK/, "all details remain reachable by scrolling");
			assert.notDeepEqual(after, before);
		}
		tui.terminal.rows = 24;
		assert.ok(bottom.render(40).length <= 8, "height responds to resize");
		await shortcuts.get("alt+t")!.handler(ctx);
		const collapsed = bottom.render(40).map(stripAnsi);
		assert.ok(collapsed.length <= 6, `${style}: a collapsed card stays small: ${JSON.stringify(collapsed)}`);
		assert.match(collapsed[1]!, /Todos ▸ Expand/, "folded: G's card names its control");
		assert.equal((result.details.jhonTodo as { tasks: { title: string; note: string }[] }).tasks[0].title, title.trim());
		assert.equal((result.details.jhonTodo as { tasks: { title: string; note: string }[] }).tasks[0].note, note.trim());
		await shortcuts.get("alt+t")!.handler(ctx);
		await shortcuts.get("ctrl+shift+down")!.handler(ctx);
		assert.notDeepEqual(bottom.render(40), collapsed, "keyboard scrolling is available without mouse");
	}
});

// T15.12c slice 4: under the narrow chrome the TODOS card above the prompt
// steps aside and the rail part offers the work row its digest, as the
// Gentle Shell's (`◐ <task in progress>` and `done/planned todos`).
test("the TODOS card above the prompt steps aside while the work folds into the work row, whose digest the rail part gives (T15.12c)", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widgets } = fakeContext();
	await fire("session_start", ctx);
	await tools.get("todo")!.execute("w", { action: "write", tasks: [{ title: "Write the sheet", status: "in_progress" }, { title: "Test it" }, { title: "Read G", status: "done" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	const tui = { requestRender() {}, terminal: { rows: 30, columns: 100 } };
	const bottom = widgets.get("jhon-todo")!(tui, plainTheme);
	const state = sidebarState(tui as never);
	const rail = state.parts.get("todo") as unknown as { summary(): { left: string; right?: string; rightLabel?: string } | undefined };
	assert.deepEqual(rail.summary(), { left: "◐ Write the sheet", right: "1/3", rightLabel: "todos" });
	assert.match(bottom.render(100).map(stripAnsi)[0]!, /Todos ▾ Collapse/, "no work row: the card above the prompt");
	state.compactWork = () => true;
	assert.deepEqual(bottom.render(100), [], "the work row carries it");
	state.compactWork = () => false;
	assert.match(bottom.render(100).map(stripAnsi)[0]!, /Todos ▾ Collapse/);
});

test("Todo keyboard scrolling targets the visible card after a cached sidebar breakpoint round-trip", async () => {
	const node = Symbol.for("@earendil-works/pi-tui/layout-node");
	for (const style of ["sections"]) {
		const { pi, tools, fire, shortcuts } = fakePi();
		jhonTodo(pi, {});
		const { ctx, widgets } = fakeContext();
		await fire("session_start", ctx);
		// Longer than the rail's band (L141: TODOS takes the band's free rows), so it scrolls.
		await tools.get("todo")!.execute("write", { action: "write", tasks: [{ title: Array.from({ length: 400 }, (_, i) => `TITLE${i}`).join(" "), status: "in_progress" }] }, undefined, undefined, ctx);
		await fire("tool_execution_end", ctx, { toolName: "todo" });
		const root = { render: () => [], invalidate() {}, [node]: () => ({ type: "vstack", entries: [] }) };
		const tui = { mode: "fullscreen", terminal: { rows: 30, columns: 160 }, layoutRoot: root, requestRender() {} };
		const theme = { ...plainTheme, bold: (text: string) => text, bg: (_color: string, text: string) => `\x1b[48;5;22m${text}\x1b[49m` };
		const bottom = widgets.get("jhon-todo")!(tui, theme);
		const dispose = installSidebar(tui as unknown as TUI, theme);
		try {
			const railLines = () => {
				const layout = root[node]() as unknown as { entries: { component: Component }[] };
				return layout.entries[1]!.component.render(50).map(stripAnsi);
			};
			railLines();
			tui.terminal.columns = 40;
			root[node]();
			bottom.render(40);
			tui.terminal.columns = 160;
			const before = railLines();
			await shortcuts.get("ctrl+shift+down")!.handler(ctx);
			const after = railLines();
			assert.notDeepEqual(after, before, "DOWN changes the visible cached rail, not the hidden bottom widget");
			// DOWN moves the viewport three rows: the first changed row now shows what was three rows lower.
			const moved = after.findIndex((row, index) => row !== before[index]);
			assert.ok(moved >= 0 && before[moved + 3] !== undefined && after[moved] === before[moved + 3], `scrolled by three rows: ${JSON.stringify({ before, after })}`);
			await shortcuts.get("ctrl+shift+up")!.handler(ctx);
			assert.deepEqual(railLines(), before, "UP restores the expanded viewport");
		} finally {
			dispose();
		}
	}
});

test("sections: TODOS is bounded in the rail as above the prompt, a third of the screen and at most 16 rows, scrolling past them (C10)", async (t) => {
	const node = Symbol.for("@earendil-works/pi-tui/layout-node");
	const tasks = Array.from({ length: 21 }, (_, i) => ({ title: `Task-${i + 1}`, status: i === 0 ? "in_progress" : "pending" }));
	const theme = { ...plainTheme, bold: (text: string) => text, bg: (_color: string, text: string) => `\x1b[48;5;22m${text}\x1b[49m` };
	const start = async (rows: number) => {
		const { pi, tools, fire, shortcuts } = fakePi();
		jhonTodo(pi, {});
		const { ctx, widgets } = fakeContext();
		await fire("session_start", ctx);
		await tools.get("todo")!.execute("write", { action: "write", tasks }, undefined, undefined, ctx);
		await fire("tool_execution_end", ctx, { toolName: "todo" });
		const root = { render: () => [], invalidate() {}, [node]: () => ({ type: "vstack", entries: [] }) };
		const tui = { mode: "fullscreen", terminal: { rows, columns: 160 }, layoutRoot: root, requestRender() {} };
		widgets.get("jhon-todo")!(tui, theme);
		t.after(installSidebar(tui as unknown as TUI, theme));
		const frame = () => renderLayoutFrame(root as never, 160, rows, () => {}).lines.map(stripAnsi);
		return { ctx, shortcuts, frame };
	};
	const tall = (await start(60)).frame();
	const card = tall.filter((line) => /Task-\d+\b|TODOS|╰─|ctrl\+shift/.test(line));
	assert.ok(card.length <= 16, `at most 16 rows even in a tall band: ${card.length}`);
	assert.ok(!tall.some((line) => /Task-21\b/.test(line)), "the rest waits a scroll away");
	assert.ok(tall.some((line) => line.includes("ctrl+shift+↑/↓")), "an overflowing list shows the hint");
	const short = await start(12);
	const before = short.frame();
	assert.ok(before.some((line) => line.includes("ctrl+shift+↑/↓")), "a short band overflows: the hint shows");
	assert.ok(!before.some((line) => /Task-21\b/.test(line)));
	await short.shortcuts.get("ctrl+shift+down")!.handler(short.ctx);
	assert.notDeepEqual(short.frame(), before, "ctrl+shift+↓ scrolls the list");
	// The bottom widget above the prompt keeps its cap: a third of the screen, at most 16 rows.
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widgets } = fakeContext();
	await fire("session_start", ctx);
	await tools.get("todo")!.execute("write", { action: "write", tasks }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	const bottom = widgets.get("jhon-todo")!({ requestRender() {}, terminal: { rows: 30, columns: 100 } }, theme);
	const rows = bottom.render(100).map(stripAnsi);
	assert.ok(rows.length <= 10, `the bottom widget stays within a third of 30 rows: ${rows.length}`);
	assert.ok(rows.some((line) => line.includes("ctrl+shift+↑/↓")));
});

test("todoEnabled and todoCollapseKey read their environment flags", () => {
	assert.equal(todoEnabled({}), true);
	assert.equal(todoEnabled({ JHON_SHELL_TODO: "0" }), false);
	assert.equal(todoEnabled({ JHON_PI_CHILD: "1" }), false);
	assert.equal(todoCollapseKey(envSettings({})), "alt+t", "terminals take ctrl+shift+t for a new tab");
	assert.equal(todoCollapseKey(envSettings({ JHON_SHELL_TODO_KEY: "ctrl+o" })), "ctrl+o");
	assert.equal(todoCollapseKey(envSettings({ JHON_SHELL_TODO_KEY: "off" })), undefined);
	const off = fakePi();
	jhonTodo(off.pi, { JHON_SHELL_TODO: "off" });
	assert.equal(off.tools.size, 0);
});

test("todo registration owns its transparent transcript shell", () => {
	const { pi, tools } = fakePi();
	jhonTodo(pi, {});
	assert.equal(tools.get("todo")?.renderShell, "self");
});

test("the todo tool writes the list, shows the card after the call, and carries the snapshot in details", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widget } = fakeContext();
	await fire("session_start", ctx);
	assert.deepEqual(widget(), [], "no card without tasks");

	const tool = tools.get("todo")!;
	const result = await tool.execute("c1", { action: "write", tasks: [{ title: "Write the parser", status: "in_progress", note: "parsing" }, { title: "Add tests" }] }, undefined, undefined, ctx);
	assert.match(result.content[0].text, /2 tasks · 0 done · 1 in progress/);
	assert.equal((result.details.jhonTodo as { tasks: unknown[] }).tasks.length, 2);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	const lines = widget()!;
	assert.match(lines[0], /^╭─ 力 Todos ▾ Collapse · 0 of 2 ─+ alt\+t collapse ╮$/);
	assert.match(lines[1], /◐ Write the parser · parsing/);
	assert.match(lines[2], /· Add tests/);
	assert.equal(lines[lines.length - 1], "", "a blank line keeps the card off the prompt");

	const bad = await tool.execute("c2", { action: "update", id: 9, status: "done" }, undefined, undefined, ctx);
	assert.match(bad.content[0].text, /Error: no task #9/);
	assert.equal(bad.details.error, "no task #9");
	assert.match(tool.renderCall({ action: "write" }, plainTheme).render(40).join(""), /力 todo · write/);
	assert.equal(tool.renderResult({ content: [{ type: "text", text: "a\nb" }] }, { expanded: false }, plainTheme).render(40).join("|").trimEnd(), "a");
});

test("the Todo header is a fullscreen left-click control while non-click pointer events stay inert", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widgetComponent } = fakeContext();
	await fire("session_start", ctx);
	await tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "A", status: "in_progress" }, { title: "B" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });

	const component = widgetComponent()!;
	assert.match(stripAnsi(component.render(70)[0]!), /Todos ▾ Collapse/);
	const event = (type: "press" | "click", button: "left" | "right", y = 0) => ({
		type, button, x: 1, y, screenX: 1, screenY: y, width: 70, height: 5, shift: false, alt: false, ctrl: false,
	});
	assert.equal(component.handleMouse?.(event("press", "left")), undefined);
	assert.equal(component.handleMouse?.(event("click", "right")), undefined);
	assert.equal(component.handleMouse?.(event("click", "left", 1)), undefined);
	assert.match(stripAnsi(component.render(70)[0]!), /Todos ▾ Collapse/, "only a left click on the header toggles");
	assert.equal(component.handleMouse?.(event("click", "left"))?.handled, true);
	assert.match(stripAnsi(component.render(70)[0]!), /Todos ▸ Expand/);
});

// H1 (odd/tasks/usage-click-and-changes-attribution.md): the header control
// now paints the same shared hover role every other clickable surface uses.
test("the Todo header paints the shared hover role while hovered, and clears it off the header row or on leave", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widgetComponent } = fakeContext();
	await fire("session_start", ctx);
	await tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "A" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	const component = widgetComponent()!;
	const move = (y: number) => ({ type: "move" as const, button: "none" as const, x: 1, y, screenX: 1, screenY: y, width: 70, height: 5, shift: false, alt: false, ctrl: false });
	assert.match(stripAnsi(component.render(70)[0]!), /Todos ▾ Collapse/);

	const entered = component.handleMouse?.(move(0));
	assert.deepEqual(entered, { handled: true, render: true });
	assert.match(stripAnsi(component.render(70)[0]!), /Todos ▾ Collapse/, "the title is unchanged; only its role changes (not observable through plainTheme here)");

	// Moving to another row of the card (still inside the region, but off the
	// clickable header) clears the hover.
	const movedOff = component.handleMouse?.(move(1));
	assert.deepEqual(movedOff, { handled: true, render: true });

	// Re-entering, then a second move at the same row is a no-op (already hovered).
	component.handleMouse?.(move(0));
	assert.deepEqual(component.handleMouse?.(move(0)), { handled: true });
});

// The nudges reach the model as a user-role message (pi's custom messages
// convert to user text), so each carries the harness's frame (T42d).
const HARNESS_FRAME = "[Harness reminder from the Jhon shell, not a message from the owner] ";
const REMINDER = HARNESS_FRAME + "The todo plan has not been reviewed during several tool-use turns. Check whether it still matches the work performed and update tasks where needed. Do not mark work done without verification.";

function toolTurn(toolName = "read", args: Record<string, unknown> = {}) {
	return { message: { role: "assistant", content: [{ type: "toolCall", id: "call", name: toolName, arguments: args }] }, toolResults: [] };
}

// T115d: a nudge is a persisted message sent after a tool-use turn, delivered
// before the next request; this reads the one sent since the last read.
async function contextReminder(fire: ReturnType<typeof fakePi>["fire"], _ctx: ExtensionContext) {
	const box = SENT.get(fire)!;
	const fresh = box.sent.slice(box.read);
	box.read = box.sent.length;
	assert.ok(fresh.length <= 1, "one nudge at a time");
	return fresh[0];
}

// What the model reads must say it is the harness speaking, not the owner.
function assertHarnessFramed(message: unknown) {
	const text = ((message as { content: Array<{ text: string }> }).content)[0]!.text;
	assert.ok(text.startsWith(HARNESS_FRAME), `the nudge must open with the harness frame: ${text}`);
	assert.match(text, /not a message from the owner/);
}

async function workTurns(fire: ReturnType<typeof fakePi>["fire"], ctx: ExtensionContext, count: number) {
	for (let index = 0; index < count; index++) {
		assert.equal(await fire("turn_end", ctx, toolTurn()), undefined, "a reminder never requests continuation");
	}
}

test("runtime reminder arrives after four tool-use turns, only once, without changing todo state", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx } = fakeContext([], false);
	await fire("session_start", ctx);
	const tool = tools.get("todo")!;
	const written = await tool.execute("write", { action: "write", tasks: [{ title: "Implement" }] }, undefined, undefined, ctx);
	await fire("turn_end", ctx, toolTurn("todo"));
	await workTurns(fire, ctx, 3);
	assert.equal(await contextReminder(fire, ctx), undefined);
	await workTurns(fire, ctx, 1);
	const result = await contextReminder(fire, ctx);
	assert.ok(result, "the next model request must carry a reminder within the same run");
	assert.deepEqual(result, { message: { customType: "jhon-todo-reminder", content: REMINDER, display: false }, options: { deliverAs: "steer" } }, "persisted, delivered before the next request, no timestamp of its own");
	const converted = convertToLlm([{ role: "custom", ...result.message, timestamp: 1 }] as Parameters<typeof convertToLlm>[0]);
	assert.deepEqual(converted[0], { role: "user", content: [{ type: "text", text: REMINDER }], timestamp: 1 }, "Pi forwards the reminder as provider-compatible text");
	assertHarnessFramed(converted[0]);
	await workTurns(fire, ctx, 4);
	assert.equal(await contextReminder(fire, ctx), undefined, "no repeated nagging without a todo write");
	const listed = await tool.execute("list", { action: "list" }, undefined, undefined, ctx);
	assert.deepEqual(listed.details.jhonTodo, written.details.jhonTodo, "a nudge does not update tasks, freshness or IDs");
});

test("a successful todo update resets and rearms the runtime reminder", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx } = fakeContext();
	await fire("session_start", ctx);
	const tool = tools.get("todo")!;
	await tool.execute("write", { action: "write", tasks: [{ title: "Implement" }] }, undefined, undefined, ctx);
	await fire("turn_end", ctx, toolTurn("todo"));
	await workTurns(fire, ctx, 4);
	assert.ok(await contextReminder(fire, ctx));
	await tool.execute("update", { action: "update", id: 1, status: "in_progress" }, undefined, undefined, ctx);
	await fire("turn_end", ctx, toolTurn("todo"));
	await workTurns(fire, ctx, 3);
	assert.equal(await contextReminder(fire, ctx), undefined);
	await workTurns(fire, ctx, 1);
	assert.ok(await contextReminder(fire, ctx));
});

test("list and rejected todo writes do not reset or rearm runtime reminders", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx } = fakeContext();
	await fire("session_start", ctx);
	const tool = tools.get("todo")!;
	const written = await tool.execute("write", { action: "write", tasks: [{ title: "Implement" }] }, undefined, undefined, ctx);
	await fire("turn_end", ctx, toolTurn("todo"));
	await workTurns(fire, ctx, 2);
	const listed = await tool.execute("list", { action: "list" }, undefined, undefined, ctx);
	assert.deepEqual(listed.details.jhonTodo, written.details.jhonTodo);
	await fire("turn_end", ctx, toolTurn("todo"));
	const rejected = await tool.execute("bad", { action: "update", id: 999, status: "done" }, undefined, undefined, ctx);
	assert.equal(rejected.content[0].text, "Error: no task #999");
	assert.equal(rejected.details.error, "no task #999");
	assert.deepEqual(rejected.details.jhonTodo, written.details.jhonTodo);
	await fire("turn_end", ctx, toolTurn("todo"));
	assert.ok(await contextReminder(fire, ctx));
	await tool.execute("list-again", { action: "list" }, undefined, undefined, ctx);
	await tool.execute("bad-again", { action: "update", id: 999, status: "done" }, undefined, undefined, ctx);
	await workTurns(fire, ctx, 4);
	assert.equal(await contextReminder(fire, ctx), undefined);
});

test("runtime reminders skip empty, blocked-only and finished plans and non-tool turns", async () => {
	for (const tasks of [[], [{ title: "Wait", status: "blocked", note: "waiting for approval" }], [{ title: "Listo", status: "done" }, { title: "Obsolete", status: "dropped" }]]) {
		const { pi, tools, fire } = fakePi();
		jhonTodo(pi, {});
		const { ctx } = fakeContext();
		await fire("session_start", ctx);
		await tools.get("todo")!.execute("write", { action: "write", tasks }, undefined, undefined, ctx);
		await fire("turn_end", ctx, toolTurn("todo"));
		await workTurns(fire, ctx, 8);
		assert.equal(await contextReminder(fire, ctx), undefined);
	}
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx } = fakeContext();
	await fire("session_start", ctx);
	await tools.get("todo")!.execute("write", { action: "write", tasks: [{ title: "Implement" }] }, undefined, undefined, ctx);
	await fire("turn_end", ctx, toolTurn("todo"));
	for (let index = 0; index < 8; index++) await fire("turn_end", ctx, { message: { role: "assistant", content: [{ type: "text", text: "Thinking" }] }, toolResults: [] });
	assert.equal(await contextReminder(fire, ctx), undefined);
	await tools.get("todo")!.execute("clear", { action: "clear" }, undefined, undefined, ctx);
	await workTurns(fire, ctx, 4);
	assert.equal(await contextReminder(fire, ctx), undefined, "a cleared plan brings no reminder");
});

test("runtime reminder tracking is isolated by session and reset on session load", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const first = fakeContext();
	const second = fakeContext();
	second.ctx.sessionManager = { ...second.ctx.sessionManager, getSessionId: () => "s2" } as ExtensionContext["sessionManager"];
	await fire("session_start", first.ctx);
	await fire("session_start", second.ctx);
	const tool = tools.get("todo")!;
	const written = await tool.execute("write", { action: "write", tasks: [{ title: "Implement" }] }, undefined, undefined, first.ctx);
	await fire("turn_end", first.ctx, toolTurn("todo"));
	await workTurns(fire, second.ctx, 4);
	assert.equal(await contextReminder(fire, second.ctx), undefined, "another session cannot inherit the plan or reminder");
	await workTurns(fire, first.ctx, 4);
	assert.ok(await contextReminder(fire, first.ctx));
	await fire("session_shutdown", first.ctx);
	const reloaded = fakeContext([{ type: "message", message: { role: "toolResult", toolName: "todo", details: written.details } }]);
	await fire("session_start", reloaded.ctx);
	assert.equal(await contextReminder(fire, reloaded.ctx), undefined, "reloaded plans start with a fresh reminder counter");
	await workTurns(fire, reloaded.ctx, 4);
	assert.ok(await contextReminder(fire, reloaded.ctx), "reload does not inherit a consumed reminder");
});

// T115d: the todo block changes per owner message, so it never sits in the
// system prompt: it reaches the model as a persisted message the run start
// returns, sent when it changed. session() reads the block an event delivered.
function promptEvent(): { systemPrompt: string; systemPromptOptions: { appendSystemPrompt: string; sections: Record<string, string> }; delivered?: string } {
	return { systemPrompt: "base", systemPromptOptions: { appendSystemPrompt: "", sections: {} } };
}

const session = (event: ReturnType<typeof promptEvent>): string => {
	assert.equal(event.systemPromptOptions.appendSystemPrompt, "", "the appended prompt stays static");
	assert.deepEqual(event.systemPromptOptions.sections, {}, "the system prompt carries no todo block");
	return event.delivered ?? "";
};

// S34 (L139): the agent starts its own list. With no list the prompt carries
// the rule to start one, and working turns that change something without a
// list bring one request-local nudge per owner message.
// T134 (L1019): the list is the agent's own next steps, inside an LDD change too.
const START_RULE = "## Todo list\nInside an LDD change or not, the todo list holds your own next steps for this request; the change's tasks stay in `jhon status` and tasks.md, never copied here. For work of three or more steps, or any change to code, write it with `todo write` before the first edit and keep it current: one task in_progress, mark it done right after it is verified. A question, a single read or a one-line fix needs no list.";
// T134 H7b (L1019): the list is the agent's own next steps; the change's tasks stay in `jhon status`.
const START_NUDGE = HARNESS_FRAME + "This work has several steps and no todo list. Write your own next steps for this request with todo write and keep them current; the change's tasks stay in `jhon status`.";

function isNudge(result: unknown): boolean {
	const message = (result as { message?: SentMessage } | undefined)?.message;
	return message?.customType === "jhon-todo-start" && message.content === START_NUDGE;
}

test("with no list the prompt carries the rule to start one; open tasks replace it with the list block", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx } = fakeContext();
	await fire("session_start", ctx);
	const empty = promptEvent();
	const started = await fire("before_agent_start", ctx, empty) as { systemPrompt?: string; message?: SentMessage };
	assert.equal(started.systemPrompt, undefined, "the handler must not return a replacement systemPrompt");
	assert.deepEqual(started.message, { customType: "jhon-todo-list", content: START_RULE, display: false });
	assert.equal(session(empty), START_RULE);
	assert.equal(await fire("before_agent_start", ctx, promptEvent()), undefined, "an unchanged rule is not sent again");

	await tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "Fix the bug" }] }, undefined, undefined, ctx);
	const open = promptEvent();
	await fire("before_agent_start", ctx, open);
	assert.match(session(open), /^## Todo list\nKeep it current with the `todo` tool/);
	assert.match(session(open), /1\. \[pending\] Fix the bug/);
	assert.doesNotMatch(session(open), /before the first edit/);

	await tools.get("todo")!.execute("c2", { action: "clear" }, undefined, undefined, ctx);
	const cleared = promptEvent();
	await fire("before_agent_start", ctx, cleared);
	assert.equal(session(cleared), START_RULE, "an emptied list brings the rule back");
});

test("working turns that change something with no list bring one nudge per owner message", async () => {
	const { pi, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx } = fakeContext([], false);
	await fire("session_start", ctx);
	await fire("before_agent_start", ctx, promptEvent());
	await fire("turn_end", ctx, toolTurn("edit", { path: "a.ts" }));
	await workTurns(fire, ctx, 1);
	assert.equal(await contextReminder(fire, ctx), undefined, "two tool-use turns are not yet several");
	await workTurns(fire, ctx, 1);
	const result = await contextReminder(fire, ctx);
	assert.ok(isNudge(result), "the third tool-use turn with a write brings the nudge");
	assert.deepEqual(result, { message: { customType: "jhon-todo-start", content: START_NUDGE, display: false }, options: { deliverAs: "steer" } });
	assertHarnessFramed(convertToLlm([{ role: "custom", ...result!.message, timestamp: 1 }] as Parameters<typeof convertToLlm>[0])[0]);
	await workTurns(fire, ctx, 4);
	assert.equal(await contextReminder(fire, ctx), undefined, "once per owner message");

	await fire("before_agent_start", ctx, promptEvent());
	await workTurns(fire, ctx, 2);
	await fire("turn_end", ctx, toolTurn("write", { path: "b.ts" }));
	assert.ok(isNudge(await contextReminder(fire, ctx)), "a new owner message re-arms it");
});

test("the nudge needs a change: reads and read-only bash never bring it, a changing bash does", async () => {
	for (const [turns, nudged] of [
		[[toolTurn(), toolTurn("grep"), toolTurn("read"), toolTurn()], false],
		[[toolTurn("bash", { command: "git status --short && grep -rn foo src | head" }), toolTurn("bash", { command: "node --test test/a.test.ts 2>/dev/null" }), toolTurn()], false],
		[[toolTurn("bash", { command: "mkdir -p out" }), toolTurn(), toolTurn()], true],
		[[toolTurn(), toolTurn("bash", { command: "echo hi > notes.txt" }), toolTurn()], true],
		[[toolTurn(), toolTurn(), toolTurn("bash", { command: "cd shell && sed -i 's/a/b/' x.ts" })], true],
	] as const) {
		const { pi, fire } = fakePi();
		jhonTodo(pi, {});
		const { ctx } = fakeContext([], false);
		await fire("session_start", ctx);
		await fire("before_agent_start", ctx, promptEvent());
		for (const turn of turns) await fire("turn_end", ctx, turn);
		assert.equal(isNudge(await contextReminder(fire, ctx)), nudged, JSON.stringify(turns.map((turn) => turn.message.content[0])));
	}
});

test("the nudge never comes with a list, in a child session, or after the list was cleared in this owner turn", async () => {
	const working = async (fire: ReturnType<typeof fakePi>["fire"], ctx: ExtensionContext) => {
		await fire("turn_end", ctx, toolTurn("edit"));
		await workTurns(fire, ctx, 3);
		return contextReminder(fire, ctx);
	};

	const listed = fakePi();
	jhonTodo(listed.pi, {});
	const withList = fakeContext([], false);
	await listed.fire("session_start", withList.ctx);
	await listed.fire("before_agent_start", withList.ctx, promptEvent());
	await listed.tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "Implement", status: "in_progress" }] }, undefined, undefined, withList.ctx);
	await listed.fire("turn_end", withList.ctx, toolTurn("todo"));
	assert.equal(isNudge(await working(listed.fire, withList.ctx)), false, "a list already exists");

	const child = fakePi();
	jhonTodo(child.pi, { JHON_PI_CHILD: "1" });
	const inChild = fakeContext([], false);
	await child.fire("session_start", inChild.ctx);
	await child.fire("before_agent_start", inChild.ctx, promptEvent());
	assert.equal(await working(child.fire, inChild.ctx), undefined, "a sub-agent keeps no list");

	const clearing = fakePi();
	jhonTodo(clearing.pi, {});
	const cleared = fakeContext([], false);
	await clearing.fire("session_start", cleared.ctx);
	await clearing.fire("before_agent_start", cleared.ctx, promptEvent());
	await clearing.tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "Implement" }] }, undefined, undefined, cleared.ctx);
	await clearing.tools.get("todo")!.execute("c2", { action: "clear" }, undefined, undefined, cleared.ctx);
	await clearing.fire("turn_end", cleared.ctx, toolTurn("todo"));
	assert.equal(isNudge(await working(clearing.fire, cleared.ctx)), false, "a list cleared on purpose in this owner turn stays cleared");
	await clearing.fire("before_agent_start", cleared.ctx, promptEvent());
	assert.ok(isNudge(await working(clearing.fire, cleared.ctx)), "the next owner message re-arms it");
});

test("the todo tool's description says to start the list unasked, before the first edit of multi-step work", () => {
	const { pi, tools } = fakePi();
	jhonTodo(pi, {});
	const description = (tools.get("todo") as unknown as { description: string }).description;
	assert.match(description, /multi-step work/);
	assert.match(description, /before the first edit/);
	assert.match(description, /without being asked/);
	// T134 H7b (L1019): the agent's own next steps, never a copy of the change's tasks.
	assert.match(description, /your own next steps for this request/);
	assert.match(description, /A change's tasks are in `jhon status`: never copy them here/);
	assert.doesNotMatch(description, /tasks\.md is the list/);
	assert.ok(description.length <= 258, `the description is ${description.length} chars, no longer than before`);
});

// T42c: the todo tool's text is short and keeps every rule it states.
test("the todo tool's text stays short and keeps its rules", () => {
	const { pi, tools } = fakePi();
	jhonTodo(pi, {});
	const tool = tools.get("todo") as unknown as { description: string; promptSnippet: string; promptGuidelines: string[] };
	const text = [tool.description, tool.promptSnippet, ...tool.promptGuidelines].join("\n");
	for (const re of [/three or more steps/, /exactly one task in_progress/, /done right after/, /complete list/, /keep ids/, /tests fail or the work is partial/, /task for the blocker/, /pending once the condition holds/, /write.*update.*add.*clear.*list/s]) {
		assert.match(text, re);
	}
	assert.ok(text.length <= 900, `the todo tool's text is ${text.length} chars`);
});

test("a changed list reaches the model as a persisted message (never a returned systemPrompt) and the card goes stale after two silent turns", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widget } = fakeContext();
	await fire("session_start", ctx);
	await fire("before_agent_start", ctx, promptEvent());
	await tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "Fix the bug" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });

	const withTasksEvent = promptEvent();
	const withTasksResult = await fire("before_agent_start", ctx, withTasksEvent) as { systemPrompt?: string };
	assert.equal(withTasksResult.systemPrompt, undefined, "the handler must not return a replacement systemPrompt");
	assert.match(session(withTasksEvent), /^## Todo list/);
	assert.match(session(withTasksEvent), /1\. \[pending\] Fix the bug/);
	assert.doesNotMatch(widget()![0], /stale/);

	const staleEvent = promptEvent();
	await fire("before_agent_start", ctx, staleEvent);
	assert.match(session(staleEvent), /stale: 2 turns without an update/);
	assert.match(widget()![1], /stale · 2 turns/);

	await tools.get("todo")!.execute("c2", { action: "update", id: 1, status: "in_progress", note: "on it" }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	assert.doesNotMatch(widget()![0], /stale/);
});

test("before_agent_start is idempotent: a block the session's branch already carries is not sent again", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});

	// Capture the exact block a fresh turn produces (no staleness yet).
	const probe = fakeContext();
	await fire("session_start", probe.ctx);
	await tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "Fix the bug" }] }, undefined, undefined, probe.ctx);
	await fire("tool_execution_end", probe.ctx, { toolName: "todo" });
	const probeEvent = promptEvent();
	await fire("before_agent_start", probe.ctx, probeEvent);
	const block = session(probeEvent);
	assert.match(block, /^## Todo list/);

	// A resumed session whose branch already carries that exact block (the
	// persisted message) must not send it again.
	const { ctx } = fakeContext([{ type: "custom_message", customType: "jhon-todo-list", content: block, display: false }]);
	await fire("session_start", ctx);
	await tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "Fix the bug" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	const seededEvent = promptEvent();
	assert.equal(await fire("before_agent_start", ctx, seededEvent), undefined);
	assert.equal(session(seededEvent), "", "a block already present must not be sent again");
});

test("a finished list stays for its turn and clears at the next, and the collapse key folds the card", async () => {
	const { pi, tools, shortcuts, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widget } = fakeContext();
	await fire("session_start", ctx);
	await tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "A", status: "in_progress" }, { title: "B" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	await shortcuts.get("alt+t")!.handler(ctx);
	assert.equal(widget()!.filter((line) => line.trim() !== "").length, 3, "folded: G's card holds the current task alone");
	assert.match(widget()![0], /╭─ 力 Todos ▸ Expand /);
	await shortcuts.get("alt+t")!.handler(ctx);
	assert.match(widget()!.join("\n"), /◐ A/, "open again");

	await tools.get("todo")!.execute("c2", { action: "write", tasks: [{ id: 1, title: "A", status: "done" }, { id: 2, title: "B", status: "done" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	await fire("agent_end", ctx);
	assert.match(widget()![0], /Todos ▾ Collapse · 2 of 2 ─+ alt\+t collapse/, "the finished list is still visible at the end of its turn");
	const next = await fire("before_agent_start", ctx, { systemPrompt: "base" }) as { systemPrompt?: string; message?: SentMessage };
	assert.equal(next.systemPrompt, undefined, "no replacement systemPrompt");
	assert.equal(next.message?.content, START_RULE, "nothing open: only the rule to start a list, as a message");
	assert.deepEqual(widget(), [], "the card clears at the next turn");
});

test("session_start replays the list from the branch, rpiv-todo results included, and counts past turns", async () => {
	const { pi, fire } = fakePi();
	jhonTodo(pi, {});
	const branch = [
		{ type: "message", message: { role: "user", content: "hi" } },
		{ type: "message", message: { role: "toolResult", toolName: "todo", isError: false, details: { action: "create", params: {}, tasks: [{ id: 1, subject: "Old task", status: "in_progress", activeForm: "still going" }], nextId: 2 } } },
		{ type: "message", message: { role: "user", content: "again" } },
	];
	const { ctx, widget } = fakeContext(branch);
	await fire("session_start", ctx);
	const lines = widget()!;
	assert.match(lines[0], /Todos ▾ Collapse · 0 of 1 ─+ alt\+t collapse/);
	assert.match(lines[1], /◐ Old task · still going/);
	const headless = fakeContext(branch, false);
	await fire("session_start", headless.ctx);
	assert.equal(headless.widget(), undefined, "no UI, no card");
});

test("session_start drops a list that was already finished, so a reload never shows stale done work", async () => {
	const { pi, fire } = fakePi();
	jhonTodo(pi, {});
	const finished = [
		{ type: "message", message: { role: "user", content: "hi" } },
		{ type: "message", message: { role: "toolResult", toolName: "todo", isError: false, details: { jhonTodo: { tasks: [{ id: 1, title: "Done A", status: "done" }, { id: 2, title: "Done B", status: "done" }], nextId: 3, updatedTurn: 1 } } } },
	];
	const { ctx, widget } = fakeContext(finished);
	await fire("session_start", ctx);
	assert.deepEqual(widget(), [], "nothing to show after a reload of finished work");
	const prompt = (await fire("before_agent_start", ctx, { systemPrompt: "base" })) as { systemPrompt?: string; message?: SentMessage };
	assert.equal(prompt.systemPrompt, undefined, "and nothing is injected into the prompt");
	assert.equal(prompt.message?.content, START_RULE, "the finished tasks are not sent; only the rule to start a list");
});

test("the todo tool offers blocked and dropped in its schema and guidelines", () => {
	const { pi, tools } = fakePi();
	jhonTodo(pi, {});
	const tool = tools.get("todo") as unknown as { parameters: { properties: Record<string, { enum?: string[]; items?: { properties: Record<string, { enum?: string[]; description?: string }> } }> }; promptGuidelines: string[] };
	const statuses = ["pending", "in_progress", "blocked", "done", "dropped"];
	assert.deepEqual(tool.parameters.properties.status.enum, statuses);
	assert.deepEqual(tool.parameters.properties.tasks.items!.properties.status.enum, statuses);
	assert.match(tool.parameters.properties.tasks.items!.properties.note.description!, /required for blocked/);
	assert.ok(tool.promptGuidelines.some((line) => /blocked with a note naming what it waits for/.test(line)));
	assert.ok(tool.promptGuidelines.some((line) => /dropped/.test(line) && /obsolete/.test(line)));
});

test("a list left with only dropped tasks open finishes and clears; a blocked-only list stays in the prompt without going stale", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widget } = fakeContext();
	await fire("session_start", ctx);
	await tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "A", status: "done" }, { title: "Old plan", status: "dropped" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	await fire("agent_end", ctx);
	assert.match(widget()![0], /Todos ▾ Collapse · 1 of 1 ─+ alt\+t collapse/);
	await fire("before_agent_start", ctx, promptEvent());
	assert.deepEqual(widget(), [], "a done-and-dropped list clears at the next turn");

	await tools.get("todo")!.execute("c2", { action: "write", tasks: [{ title: "A", status: "done" }, { title: "Deploy", status: "blocked", note: "waiting for an admin" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	for (let turn = 0; turn < 4; turn++) {
		await fire("agent_end", ctx);
		const event = promptEvent();
		await fire("before_agent_start", ctx, event);
		// The block reaches the model once and stays the same: not stale, so not sent again.
		if (turn === 0) assert.match(session(event), /2\. \[blocked\] Deploy — waiting for an admin/);
		else assert.equal(session(event), "");
	}
	assert.match(widget()![2], /◉ Deploy · waiting for an admin/);
});

// L154 (the owner, reversing T49's slice in TODOS): the rail's TODOS shows
// only the tasks the agent works through with the todo tool; the change's
// tasks.md and its slices are the LDD section's alone, so nothing shows twice.
const CHANGE_STATUS = lddFromStatus("rail-redesign", { route: { level: "verified" }, next: "apply", tasks: { total: 3, completed: 1, slice: "Slice 1: rail", slices: [
	{ name: "Slice 1: rail", total: 3, completed: 1, tasks: [{ text: "Read tasks", done: true }, { text: "Draw rows", done: false }, { text: "Docs", done: false }] },
] } });

function inChange(t: { after(fn: () => void): void }, status = CHANGE_STATUS) {
	const withdraw = shareSelection("s1", async () => status);
	t.after(withdraw);
	selectChange("s1", status);
	return withdraw;
}

test("inside a change TODOS lists only the agent's own tasks and LDD only the change's: nothing shows in both (L154)", async (t) => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widget } = fakeContext();
	await fire("session_start", ctx);
	assert.deepEqual(widget(), [], "no list: no card");
	inChange(t);
	assert.deepEqual(widget(), [], "a change alone draws no TODOS: its tasks are the LDD section's");
	await tools.get("todo")!.execute("w", { action: "write", tasks: [{ title: "write the test", status: "in_progress" }, { title: "fix tsc" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	const todos = widget()!.join("\n");
	assert.match(todos, /Todos[\s\S]*0 of 2/);
	assert.match(todos, /◐ write the test[\s\S]*fix tsc/);
	assert.doesNotMatch(todos, /Read tasks|Draw rows|Docs|└|1\/3|rail-redesign|Slice/, "no task, slice or count of the change");
	const ldd = renderShellStatusSections({ cwd: "/repo", branch: "main", dirty: 0, sessionName: undefined, modelId: "m", effort: undefined, contextPercent: 1, contextWindow: 1000, costTotal: 0, subscription: false, usage: undefined, statuses: [], ldd: CHANGE_STATUS }, { ...plainTheme, bold: (text: string) => text }, 60)
		.map(stripAnsi).join("\n");
	assert.match(ldd, /▾ LDD[\s\S]*◆ rail-redesign[\s\S]*on 1: rail/);
	assert.doesNotMatch(ldd, /write the test|fix tsc/, "the agent's tasks are TODOS' alone");
	selectChange("s1", undefined);
	assert.equal(widget()!.join("\n"), todos, "outside a change, the same list");
});

test("inside a change the main session still gets the start rule and the nudge: its own steps, never the change's tasks (T134, L1019)", async (t) => {
	const { pi, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx } = fakeContext([], false);
	await fire("session_start", ctx);
	inChange(t);
	const inside = promptEvent();
	await fire("before_agent_start", ctx, inside);
	assert.equal(session(inside), START_RULE, "the start rule inside a change");
	await fire("turn_end", ctx, toolTurn("edit", { path: "a.ts" }));
	await workTurns(fire, ctx, 2);
	assert.ok(isNudge(await contextReminder(fire, ctx)), "the nudge inside a change");
});

// T134 (L1019): no todo text tells the agent to copy tasks.md into the list.
test("no todo text tells the agent to copy the change's tasks into the todo list", async (t) => {
	const copies = /one (todo )?per task|tasks\.md is the list|copy (its|the change's) tasks into/i;
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx } = fakeContext([], false);
	await fire("session_start", ctx);
	inChange(t);
	const event = promptEvent();
	await fire("before_agent_start", ctx, event);
	await fire("turn_end", ctx, toolTurn("edit", { path: "a.ts" }));
	await workTurns(fire, ctx, 2);
	const nudge = (await contextReminder(fire, ctx) as { message?: SentMessage } | undefined)?.message?.content ?? "";
	const tool = tools.get("todo") as unknown as { description: string; promptSnippet: string; promptGuidelines: string[] };
	const sources = ["../extensions/todo.ts", "../lib/ui/shell-todo.ts"].map((f) => readFileSync(new URL(f, import.meta.url), "utf8"));
	for (const text of [tool.description, tool.promptSnippet, ...tool.promptGuidelines, session(event), nudge, ...sources]) {
		assert.doesNotMatch(text, copies);
	}
	assert.ok(session(event) && nudge, "the rule and the nudge were read");
});

// L175 (5): TODOS idle reads as AGENTS idle does, in the rail and the sheet:
// the header with `idle` as its count and one row `no tasks`; above the
// prompt nothing shows, and the work row carries nothing.
test("TODOS with no tasks shows its idle row in the rail, as AGENTS does; the card above the prompt stays away (L175)", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widgets } = fakeContext();
	await fire("session_start", ctx);
	const tui = { requestRender() {}, terminal: { rows: 30, columns: 160 } };
	const factory = widgets.get("jhon-todo");
	assert.ok(factory, "the rail part is there with no tasks");
	const bottom = factory!(tui, plainTheme);
	const rail = sidebarState(tui as never).parts.get("todo")! as unknown as Component & { summary(): unknown };
	const idle = rail.render(48).map(stripAnsi);
	assert.match(idle[0]!, /^▾ TODOS +alt\+t {2}idle$/);
	assert.deepEqual(idle.slice(1).map((row) => row.trim()), ["no tasks"]);
	assert.deepEqual(bottom.render(100), [], "nothing above the prompt");
	assert.equal(rail.summary(), undefined, "no work row for an empty list");
	await tools.get("todo")!.execute("w", { action: "write", tasks: [{ title: "Write it", status: "in_progress" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	await tools.get("todo")!.execute("c", { action: "clear" }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	const again = widgets.get("jhon-todo")!(tui, plainTheme);
	assert.deepEqual(again.render(100), []);
	assert.match(stripAnsi(sidebarState(tui as never).parts.get("todo")!.render(48)[0]!), /idle$/, "cleared, idle again");
});
