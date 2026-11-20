import "./isolated-env.ts";
import { setContextLevel } from "../lib/lean-context.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { convertToLlm, type ExtensionAPI, type ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { Component, TUI } from "@earendil-works/pi-tui";
import jhonTodo, { todoEnabled } from "../extensions/todo.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { paneState } from "../lib/ui/pane.ts";
import { lddFromStatus, selectChange, shareSelection } from "../lib/ui/ldd-status.ts";
import { renderLddPanel } from "../lib/ui/panel.ts";


// The todo extension: the `todo` tool, its list in the right pane (T174),
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
	// T174 (L1469): the list is the right pane's TODOS section alone; nothing renders above the prompt.
	const widget = () => {
		const factory = widgets.get("jhon-todo");
		if (!factory) return undefined;
		const tui = { ...fakeTui, terminal: { rows: 48, columns: 180 } };
		assert.deepEqual(factory(tui, plainTheme).render(70), [], "nothing above the prompt");
		return paneState(tui as never).parts.get("todo")!.render(70).map(stripAnsi);
	};
	return { ctx, widgets, widget };
}

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

// T174: JHON_SHELL_TODO is gone with the card; the tool is the agent's, and a
// child has it only when its tools name it.
test("todoEnabled: the main session always has the tool, a child only when its tools name it", () => {
	assert.equal(todoEnabled({}), true);
	assert.equal(todoEnabled({ JHON_SHELL_TODO: "0" }), true, "the variable is no longer read");
	assert.equal(todoEnabled({ JHON_PI_CHILD: "1" }), false);
	assert.equal(todoEnabled({ JHON_PI_CHILD: "1", JHON_PI_TOOLS: "read,todo" }), true);
});

test("todo registration owns its transparent transcript shell", () => {
	const { pi, tools } = fakePi();
	jhonTodo(pi, {});
	assert.equal(tools.get("todo")?.renderShell, "self");
});

test("the todo tool writes the list, shows it in the right pane after the call, and carries the snapshot in details", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widget } = fakeContext();
	await fire("session_start", ctx);
	assert.deepEqual(widget(), [], "no section without tasks");

	const tool = tools.get("todo")!;
	const result = await tool.execute("c1", { action: "write", tasks: [{ title: "Write the parser", status: "in_progress", note: "parsing" }, { title: "Add tests" }] }, undefined, undefined, ctx);
	assert.match(result.content[0].text, /2 tasks · 0 done · 1 in progress/);
	assert.equal((result.details.jhonTodo as { tasks: unknown[] }).tasks.length, 2);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	const lines = widget()!;
	assert.match(lines[0], /^▾ TODOS +0\/2$/);
	assert.match(lines[1], /◐ Write the parser · parsing/);
	assert.match(lines[2], /· Add tests/);
	assert.equal(lines.length, 3);

	const bad = await tool.execute("c2", { action: "update", id: 9, status: "done" }, undefined, undefined, ctx);
	assert.match(bad.content[0].text, /Error: no task #9/);
	assert.equal(bad.details.error, "no task #9");
	assert.match(tool.renderCall({ action: "write" }, plainTheme).render(40).join(""), /力 todo · write/);
	assert.equal(tool.renderResult({ content: [{ type: "text", text: "a\nb" }] }, { expanded: false }, plainTheme).render(40).join("|").trimEnd(), "a");
});

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
const START_RULE = "## Todo list\nInside an LDD change or not, the todo list holds your own next steps for this request; the change's tasks stay in `jhon status` and tasks.md, never copied here. For work of three or more steps, write it with `todo write` before the first edit and keep it current: one task in_progress, mark it done right after it is verified. A question, a single read or a one-line fix needs no list; a small change (`jhon status` says so) keeps no tasks.md, archive, light verify or todo list.";
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

test("a changed list reaches the model as a persisted message (never a returned systemPrompt) and goes stale after two silent turns", async () => {
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

	const staleEvent = promptEvent();
	await fire("before_agent_start", ctx, staleEvent);
	assert.match(session(staleEvent), /stale: 2 turns without an update/);
	assert.match(widget()![0], /TODOS +0\/1$/);
	assert.equal(widget()![1].trim(), "· Fix the bug");
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

test("a finished list stays for its turn and clears at the next", async () => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widget } = fakeContext();
	await fire("session_start", ctx);
	await tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "A", status: "in_progress" }, { title: "B" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	assert.match(widget()!.join("\n"), /◐ A/);

	await tools.get("todo")!.execute("c2", { action: "write", tasks: [{ id: 1, title: "A", status: "done" }, { id: 2, title: "B", status: "done" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	await fire("agent_end", ctx);
	assert.match(widget()![0], /TODOS +2\/2$/, "the finished list is still visible at the end of its turn");
	const next = await fire("before_agent_start", ctx, { systemPrompt: "base" }) as { systemPrompt?: string; message?: SentMessage };
	assert.equal(next.systemPrompt, undefined, "no replacement systemPrompt");
	assert.equal(next.message?.content, START_RULE, "nothing open: only the rule to start a list, as a message");
	assert.deepEqual(widget(), [], "the list clears at the next turn");
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
	assert.match(lines[0], /TODOS +0\/1$/);
	assert.match(lines[1], /◐ Old task · still going/);
	const headless = fakeContext(branch, false);
	await fire("session_start", headless.ctx);
	assert.equal(headless.widget(), undefined, "no UI, no section");
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
	assert.match(widget()![0], /TODOS +1\/1$/);
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

// L154: the change's tasks.md and its slices are the LDD section's alone;
// T174 (L1469): with a change shown the LDD section rules and TODOS hides,
// so nothing shows twice.
const CHANGE_STATUS = lddFromStatus("rail-redesign", { route: { level: "verified" }, next: "apply", tasks: { total: 3, completed: 1, slice: "Slice 1: rail", slices: [
	{ name: "Slice 1: rail", total: 3, completed: 1, tasks: [{ text: "Read tasks", done: true }, { text: "Draw rows", done: false }, { text: "Docs", done: false }] },
] } });

function inChange(t: { after(fn: () => void): void }, status = CHANGE_STATUS) {
	const withdraw = shareSelection("s1", async () => status);
	t.after(withdraw);
	selectChange("s1", status);
	return withdraw;
}

test("inside a change TODOS hides and LDD shows only the change's tasks: nothing shows in both (L154, L1469)", async (t) => {
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx, widget } = fakeContext();
	await fire("session_start", ctx);
	assert.deepEqual(widget(), [], "no list: no section");
	inChange(t);
	assert.deepEqual(widget(), [], "a change alone draws no TODOS: its tasks are the LDD section's");
	await tools.get("todo")!.execute("w", { action: "write", tasks: [{ title: "write the test", status: "in_progress" }, { title: "fix tsc" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	assert.deepEqual(widget(), [], "inside a change the LDD section rules: the list hides");
	const ldd = renderLddPanel(CHANGE_STATUS, { ...plainTheme, bold: (text: string) => text }, 42, { now: Date.now() })
		.map(stripAnsi).join("\n");
	assert.match(ldd, /◆ rail-redesign[\s\S]*◐ 1 rail[\s\S]*✓ Read tasks[\s\S]*◐ Draw rows/, "the change's own tasks, under its open slice");
	assert.doesNotMatch(ldd, /write the test|fix tsc/, "the agent's tasks are TODOS' alone");
	selectChange("s1", undefined);
	const todos = widget()!.join("\n");
	assert.match(todos, /TODOS +0\/2/);
	assert.match(todos, /◐ write the test[\s\S]*fix tsc/);
	assert.doesNotMatch(todos, /Read tasks|Draw rows|Docs|└|1\/3|rail-redesign|Slice/, "outside a change: the agent's own tasks, nothing of the change");
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
	const sources = ["../extensions/todo.ts", "../lib/todo.ts"].map((f) => readFileSync(new URL(f, import.meta.url), "utf8"));
	for (const text of [tool.description, tool.promptSnippet, ...tool.promptGuidelines, session(event), nudge, ...sources]) {
		assert.doesNotMatch(text, copies);
	}
	assert.ok(session(event) && nudge, "the rule and the nudge were read");
});

// T146h L4 (L1277): a lean session (no change, or a small one) gets no start
// rule and no start nudge; an open list keeps its block. The tool's guideline
// says a small change keeps no list.
test("a lean session gets no start rule and no start nudge", async (t) => {
	setContextLevel("lean");
	t.after(() => setContextLevel(undefined));
	const { pi, tools, fire } = fakePi();
	jhonTodo(pi, {});
	const { ctx } = fakeContext([], false);
	await fire("session_start", ctx);
	assert.equal(await fire("before_agent_start", ctx, promptEvent()), undefined, "no start rule");
	await fire("turn_end", ctx, toolTurn("edit", { path: "a.ts" }));
	await workTurns(fire, ctx, 4);
	assert.equal(await contextReminder(fire, ctx), undefined, "no start nudge");
	await tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "Fix the bug" }] }, undefined, undefined, ctx);
	const open = promptEvent();
	await fire("before_agent_start", ctx, open);
	assert.match(session(open), /1\. \[pending\] Fix the bug/, "a list the agent keeps is still shown");
	assert.ok(tools.get("todo")!.promptGuidelines.some((g: string) => g.includes("A small change keeps no list.")), tools.get("todo")!.promptGuidelines);
});
