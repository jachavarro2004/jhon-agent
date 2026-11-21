import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { convertToLlm, type ExtensionAPI, type ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { Component } from "@earendil-works/pi-tui";
import jhonTodo, { todoEnabled } from "../extensions/todo.ts";
import { setContextLevel } from "../lib/lean-context.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { paneState } from "../lib/ui/pane.ts";
import { lddFromStatus, selectChange, shareSelection } from "../lib/ui/ldd-status.ts";
import { renderLddPanel } from "../lib/ui/panel.ts";

// The agent's todo list through its extension: the tool, the TODOS section of
// the right pane, the block and the rule each run starts with, and the two
// nudges (reminder and start), all against a stand-in for pi.

type Handler = (event: unknown, ctx: ExtensionContext) => unknown;
type Message = { customType: string; content: string; display: boolean };
type Sent = { message: Message; options: unknown };
interface TodoTool {
	name: string; renderShell?: string; description: string; promptSnippet: string; promptGuidelines: string[];
	parameters: { properties: Record<string, { enum?: string[]; items?: { properties: Record<string, { enum?: string[]; description?: string }> } }> };
	execute(id: string, params: unknown, signal: undefined, onUpdate: undefined, ctx: ExtensionContext): Promise<{ content: Array<{ type: string; text: string }>; details: Record<string, unknown> }>;
	renderCall(args: unknown, theme: unknown): { render(width: number): string[] };
	renderResult(result: unknown, options: { expanded: boolean }, theme: unknown): { render(width: number): string[] };
}

const plain = { fg: (_role: string, text: string) => text, strikethrough: (text: string) => `~${text}~`, bold: (text: string) => text };

const FRAME = "[Harness reminder from the Jhon shell, not a message from the owner] ";
const REMINDER = `${FRAME}The todo plan has not been reviewed during several tool-use turns. Check whether it still matches the work performed and update tasks where needed. Do not mark work done without verification.`;
const START_NUDGE = `${FRAME}This work has several steps and no todo list. Write your own next steps for this request with todo write and keep them current; the change's tasks stay in \`jhon status\`.`;
const START_RULE = "## Todo list\nInside an LDD change or not, the todo list holds your own next steps for this request; the change's tasks stay in `jhon status` and tasks.md, never copied here. For work of three or more steps, write it with `todo write` before the first edit and keep it current: one task in_progress, mark it done right after it is verified. A question, a single read or a one-line fix needs no list; a small change (`jhon status` says so) keeps no tasks.md, archive, light verify or todo list.";

/** The extension loaded on a stand-in pi: its tool, its handlers, and the messages it sent. */
function rig(env: NodeJS.ProcessEnv = {}) {
	const handlers = new Map<string, Handler[]>();
	const tools = new Map<string, TodoTool>();
	const sent: Sent[] = [];
	let read = 0;
	const pi = {
		on: (name: string, handler: Handler) => { handlers.set(name, [...(handlers.get(name) ?? []), handler]); },
		registerTool: (tool: TodoTool) => { tools.set(tool.name, tool); },
		registerShortcut: () => {},
		sendMessage: (message: Message, options: unknown) => { sent.push({ message, options }); },
	} as unknown as ExtensionAPI;
	jhonTodo(pi, env);
	const fire = async (name: string, ctx: ExtensionContext, payload: unknown = {}) => {
		let last: unknown;
		for (const handler of handlers.get(name) ?? []) last = await handler(payload, ctx);
		return last;
	};
	return {
		tools,
		tool: () => tools.get("todo")!,
		fire,
		call: (ctx: ExtensionContext, params: unknown, id = "c") => tools.get("todo")!.execute(id, params, undefined, undefined, ctx),
		/** The message sent since the last look, if any: never more than one at a time. */
		nudge: (): Sent | undefined => {
			const fresh = sent.slice(read);
			read = sent.length;
			assert.ok(fresh.length <= 1, "one nudge at a time");
			return fresh[0];
		},
	};
}
type Rig = ReturnType<typeof rig>;

/** A session; `section()` draws its TODOS in the pane beside the conversation (undefined before it was ever set). */
function session(id = "s1", branch: unknown[] = [], ui = true) {
	const widgets = new Map<string, (tui: unknown, theme: unknown) => Component>();
	const ctx = {
		hasUI: ui,
		sessionManager: { getSessionId: () => id, getBranch: () => branch },
		ui: { setWidget: (key: string, factory?: (tui: unknown, theme: unknown) => Component) => { if (factory) widgets.set(key, factory); else widgets.delete(key); } },
	} as unknown as ExtensionContext;
	const section = () => {
		const factory = widgets.get("jhon-todo");
		if (!factory) return undefined;
		const tui = { requestRender() {}, terminal: { rows: 48, columns: 180 } };
		paneState(tui as never).wide = () => true;
		assert.deepEqual(factory(tui, plain).render(70), [], "with the pane beside the conversation nothing sits above the prompt");
		return paneState(tui as never).parts.get("todo")!.render(70).map(stripAnsi);
	};
	return { ctx, section };
}

/** A turn whose assistant message calls one tool. */
const toolTurn = (name = "read", args: Record<string, unknown> = {}) => ({ message: { role: "assistant", content: [{ type: "toolCall", id: "t", name, arguments: args }] }, toolResults: [] });
const textTurn = { message: { role: "assistant", content: [{ type: "text", text: "thinking" }] }, toolResults: [] };

async function toolTurns(r: Rig, ctx: ExtensionContext, count: number, name = "read") {
	for (let i = 0; i < count; i++) assert.equal(await r.fire("turn_end", ctx, toolTurn(name)), undefined, "a nudge never asks pi to continue");
}

/**
 * A run start (T115d): what it sends the model, as a persisted message; it
 * never returns a system prompt or writes into the prompt options.
 */
async function runStart(r: Rig, ctx: ExtensionContext): Promise<string> {
	const options = { appendSystemPrompt: "", sections: {} };
	const out = await r.fire("before_agent_start", ctx, { prompt: "go", systemPrompt: "base", systemPromptOptions: options }) as { systemPrompt?: string; message?: Message } | undefined;
	assert.equal(out?.systemPrompt, undefined);
	assert.deepEqual(options, { appendSystemPrompt: "", sections: {} }, "the system prompt carries no todo text");
	if (out?.message) assert.deepEqual(Object.keys(out.message), ["customType", "content", "display"]);
	if (out?.message) assert.equal(out.message.display, false);
	if (out?.message) assert.equal(out.message.customType, "jhon-todo-list");
	return out?.message?.content ?? "";
}

/** What pi turns a sent message into for the model: user text that says the harness speaks. */
function asModelReads(sent: Sent) {
	const [converted] = convertToLlm([{ role: "custom", ...sent.message, timestamp: 1 }] as Parameters<typeof convertToLlm>[0]);
	const text = (converted as { content: Array<{ text: string }> }).content[0]!.text;
	assert.ok(text.startsWith(FRAME), text);
	return converted;
}

const isStartNudge = (sent: Sent | undefined) => sent?.message.customType === "jhon-todo-start" && sent.message.content === START_NUDGE;

// T174: the tool belongs to the agent; a child has it only when its tools name it.
test("a main session always has the tool; a child only when its tools name it; JHON_SHELL_TODO is not read", () => {
	assert.equal(todoEnabled({}), true);
	assert.equal(todoEnabled({ JHON_SHELL_TODO: "0" }), true);
	assert.equal(todoEnabled({ JHON_PI_CHILD: "1" }), false);
	assert.equal(todoEnabled({ JHON_PI_CHILD: "1", JHON_PI_TOOLS: "read,todo" }), true);
	assert.equal(todoEnabled({ JHON_PI_CHILD: "1", JHON_PI_TOOLS: "read,todos" }), false);
	assert.equal(rig({ JHON_PI_CHILD: "1" }).tools.size, 0);
});

test("the tool draws its own transcript shell", () => {
	assert.equal(rig().tool().renderShell, "self");
});

test("a write answers with its progress, keeps the list in its details, and shows in TODOS once the call ends", async () => {
	const r = rig();
	const s = session();
	await r.fire("session_start", s.ctx);
	assert.deepEqual(s.section(), [], "no tasks, no section");
	const result = await r.call(s.ctx, { action: "write", tasks: [{ title: "Write the parser", status: "in_progress", note: "parsing" }, { title: "Add tests" }] });
	assert.equal(result.content[0]!.text, "Todo list written: 2 tasks · 0 done · 1 in progress");
	assert.deepEqual(result.details, { jhonTodo: { tasks: [{ id: 1, title: "Write the parser", status: "in_progress", note: "parsing" }, { id: 2, title: "Add tests", status: "pending" }], nextId: 3, updatedTurn: 0 } });
	await r.fire("tool_execution_end", s.ctx, { toolName: "todo" });
	const rows = s.section()!;
	assert.equal(rows.length, 3);
	assert.match(rows[0]!, /^▾ TODOS +alt\+t {2}0\/2$/);
	assert.match(rows[1]!, /◐ Write the parser · parsing/);
	assert.match(rows[2]!, /· Add tests/);
	const refused = await r.call(s.ctx, { action: "update", id: 9, status: "done" });
	assert.equal(refused.content[0]!.text, "Error: no task #9");
	assert.equal(refused.details.error, "no task #9");
	assert.match(r.tool().renderCall({ action: "write" }, plain).render(40).join(""), /力 todo · write/);
	assert.equal(r.tool().renderResult({ content: [{ type: "text", text: "a\nb" }] }, { expanded: false }, plain).render(40).join("|").trimEnd(), "a");
	assert.deepEqual(r.tool().renderResult({ content: [{ type: "text", text: "a\nb" }] }, { expanded: true }, plain).render(40).map((row) => row.trimEnd()), ["a", "b"]);
});

test("four tool-use turns without an update bring one reminder, which changes nothing in the list", async () => {
	const r = rig();
	const s = session("s1", [], false);
	await r.fire("session_start", s.ctx);
	const written = await r.call(s.ctx, { action: "write", tasks: [{ title: "Implement" }] });
	await r.fire("turn_end", s.ctx, toolTurn("todo"));
	await toolTurns(r, s.ctx, 3);
	assert.equal(r.nudge(), undefined);
	await toolTurns(r, s.ctx, 1);
	const reminder = r.nudge()!;
	assert.deepEqual(reminder, { message: { customType: "jhon-todo-reminder", content: REMINDER, display: false }, options: { deliverAs: "steer" } }, "persisted and steered in before the next request");
	assert.deepEqual(asModelReads(reminder), { role: "user", content: [{ type: "text", text: REMINDER }], timestamp: 1 });
	await toolTurns(r, s.ctx, 4);
	assert.equal(r.nudge(), undefined, "once until the list changes");
	assert.deepEqual((await r.call(s.ctx, { action: "list" })).details.jhonTodo, written.details.jhonTodo);
});

test("an update arms the reminder again, counting from zero", async () => {
	const r = rig();
	const s = session();
	await r.fire("session_start", s.ctx);
	await r.call(s.ctx, { action: "write", tasks: [{ title: "Implement" }] });
	await r.fire("turn_end", s.ctx, toolTurn("todo"));
	await toolTurns(r, s.ctx, 4);
	assert.ok(r.nudge());
	await r.call(s.ctx, { action: "update", id: 1, status: "in_progress" });
	await r.fire("turn_end", s.ctx, toolTurn("todo"));
	await toolTurns(r, s.ctx, 3);
	assert.equal(r.nudge(), undefined);
	await toolTurns(r, s.ctx, 1);
	assert.ok(r.nudge());
});

test("listing the tasks or a refused change neither counts as an update nor arms the reminder again", async () => {
	const r = rig();
	const s = session();
	await r.fire("session_start", s.ctx);
	const written = await r.call(s.ctx, { action: "write", tasks: [{ title: "Implement" }] });
	await r.fire("turn_end", s.ctx, toolTurn("todo"));
	await toolTurns(r, s.ctx, 2);
	assert.deepEqual((await r.call(s.ctx, { action: "list" })).details.jhonTodo, written.details.jhonTodo);
	await r.fire("turn_end", s.ctx, toolTurn("todo"));
	const refused = await r.call(s.ctx, { action: "update", id: 999, status: "done" });
	assert.deepEqual([refused.content[0]!.text, refused.details.error], ["Error: no task #999", "no task #999"]);
	assert.deepEqual(refused.details.jhonTodo, written.details.jhonTodo);
	await r.fire("turn_end", s.ctx, toolTurn("todo"));
	assert.ok(r.nudge(), "the fourth turn without an update");
	await r.call(s.ctx, { action: "list" });
	await r.call(s.ctx, { action: "update", id: 999, status: "done" });
	await toolTurns(r, s.ctx, 4);
	assert.equal(r.nudge(), undefined);
});

test("no reminder for an empty, waiting-only or finished list, after turns without tools, or after a clear", async () => {
	for (const tasks of [[], [{ title: "Wait", status: "blocked", note: "an approval" }], [{ title: "Done", status: "done" }, { title: "Old", status: "dropped" }]]) {
		const r = rig();
		const s = session();
		await r.fire("session_start", s.ctx);
		await r.call(s.ctx, { action: "write", tasks });
		await r.fire("turn_end", s.ctx, toolTurn("todo"));
		await toolTurns(r, s.ctx, 8);
		assert.equal(r.nudge(), undefined, JSON.stringify(tasks));
	}
	const r = rig();
	const s = session();
	await r.fire("session_start", s.ctx);
	await r.call(s.ctx, { action: "write", tasks: [{ title: "Implement" }] });
	await r.fire("turn_end", s.ctx, toolTurn("todo"));
	for (let i = 0; i < 8; i++) await r.fire("turn_end", s.ctx, textTurn);
	assert.equal(r.nudge(), undefined, "turns without a tool call do not count");
	await r.call(s.ctx, { action: "clear" });
	await toolTurns(r, s.ctx, 4);
	assert.equal(r.nudge(), undefined);
});

test("each session counts on its own, and a reloaded one starts counting again", async () => {
	const r = rig();
	const first = session("s1");
	const second = session("s2");
	await r.fire("session_start", first.ctx);
	await r.fire("session_start", second.ctx);
	const written = await r.call(first.ctx, { action: "write", tasks: [{ title: "Implement" }] });
	await r.fire("turn_end", first.ctx, toolTurn("todo"));
	await toolTurns(r, second.ctx, 4);
	assert.equal(r.nudge(), undefined, "the other session has no list");
	await toolTurns(r, first.ctx, 4);
	assert.ok(r.nudge());
	await r.fire("session_shutdown", first.ctx);
	const reloaded = session("s1", [{ type: "message", message: { role: "toolResult", toolName: "todo", details: written.details } }]);
	await r.fire("session_start", reloaded.ctx);
	assert.equal(r.nudge(), undefined);
	await toolTurns(r, reloaded.ctx, 4);
	assert.ok(r.nudge(), "the reminder sent before the reload does not carry over");
});

// S34 (L139): with no list the run starts with the rule to start one.
test("with no list a run starts with the start rule; open tasks replace it with the list; an emptied list brings it back", async () => {
	const r = rig();
	const s = session();
	await r.fire("session_start", s.ctx);
	assert.equal(await runStart(r, s.ctx), START_RULE);
	assert.equal(await runStart(r, s.ctx), "", "the same rule is not sent twice");
	await r.call(s.ctx, { action: "write", tasks: [{ title: "Fix the bug" }] });
	const open = await runStart(r, s.ctx);
	assert.match(open, /^## Todo list\nKeep it current with the `todo` tool/);
	assert.match(open, /1\. \[pending\] Fix the bug/);
	assert.doesNotMatch(open, /before the first edit/);
	await r.call(s.ctx, { action: "clear" });
	assert.equal(await runStart(r, s.ctx), START_RULE);
});

test("work that changes something with no list brings one start nudge, at the third tool-use turn, per owner message", async () => {
	const r = rig();
	const s = session("s1", [], false);
	await r.fire("session_start", s.ctx);
	await runStart(r, s.ctx);
	await r.fire("turn_end", s.ctx, toolTurn("edit", { path: "a.ts" }));
	await toolTurns(r, s.ctx, 1);
	assert.equal(r.nudge(), undefined, "two turns are not several");
	await toolTurns(r, s.ctx, 1);
	const nudge = r.nudge()!;
	assert.deepEqual(nudge, { message: { customType: "jhon-todo-start", content: START_NUDGE, display: false }, options: { deliverAs: "steer" } });
	asModelReads(nudge);
	await toolTurns(r, s.ctx, 4);
	assert.equal(r.nudge(), undefined);
	await runStart(r, s.ctx);
	await toolTurns(r, s.ctx, 2);
	await r.fire("turn_end", s.ctx, toolTurn("write", { path: "b.ts" }));
	assert.ok(isStartNudge(r.nudge()), "a new owner message arms it again");
});

test("only a change brings the start nudge: reads and read-only bash do not; a writing bash command does", async () => {
	const cases: Array<[ReturnType<typeof toolTurn>[], boolean]> = [
		[[toolTurn(), toolTurn("grep"), toolTurn("read"), toolTurn()], false],
		[[toolTurn("bash", { command: "git status --short && grep -rn foo src | head" }), toolTurn("bash", { command: "node --test test/a.test.ts 2>/dev/null" }), toolTurn()], false],
		[[toolTurn("bash", { command: "ls >&2" }), toolTurn("bash", { command: "a 2>&1 >/dev/null" }), toolTurn("BASH", { command: "x=1 && [ $x == 1 ]" })], false],
		[[toolTurn("bash", { command: "mkdir -p out" }), toolTurn(), toolTurn()], true],
		[[toolTurn(), toolTurn("bash", { command: "echo hi > notes.txt" }), toolTurn()], true],
		[[toolTurn(), toolTurn(), toolTurn("bash", { command: "cd shell && sed -i 's/a/b/' x.ts" })], true],
		[[toolTurn("bash", { command: "sudo rm -rf build" }), toolTurn(), toolTurn()], true],
		[[toolTurn("bash", { command: "git commit -m x" }), toolTurn(), toolTurn()], true],
		[[toolTurn("bash", { command: "pnpm add zod" }), toolTurn(), toolTurn()], true],
		[[toolTurn("Edit"), toolTurn(), toolTurn()], true],
	];
	for (const [turns, nudged] of cases) {
		const r = rig();
		const s = session("s1", [], false);
		await r.fire("session_start", s.ctx);
		await runStart(r, s.ctx);
		for (const turn of turns) await r.fire("turn_end", s.ctx, turn);
		assert.equal(isStartNudge(r.nudge()), nudged, JSON.stringify(turns.map((turn) => turn.message.content[0])));
	}
});

test("no start nudge with a list, in a child, or after the list was emptied since the owner's message", async () => {
	const busy = async (r: Rig, ctx: ExtensionContext) => {
		await r.fire("turn_end", ctx, toolTurn("edit"));
		await toolTurns(r, ctx, 3);
		return r.nudge();
	};
	const withList = rig();
	const a = session("s1", [], false);
	await withList.fire("session_start", a.ctx);
	await runStart(withList, a.ctx);
	await withList.call(a.ctx, { action: "write", tasks: [{ title: "Implement", status: "in_progress" }] });
	await withList.fire("turn_end", a.ctx, toolTurn("todo"));
	assert.equal(isStartNudge(await busy(withList, a.ctx)), false);

	const child = rig({ JHON_PI_CHILD: "1", JHON_PI_TOOLS: "todo" });
	const b = session("s1", [], false);
	await child.fire("session_start", b.ctx);
	assert.equal(await runStart(child, b.ctx), "", "no start rule in a child");
	assert.equal(await busy(child, b.ctx), undefined);

	const emptied = rig();
	const c = session("s1", [], false);
	await emptied.fire("session_start", c.ctx);
	await runStart(emptied, c.ctx);
	await emptied.call(c.ctx, { action: "write", tasks: [{ title: "Implement" }] });
	await emptied.call(c.ctx, { action: "clear" });
	await emptied.fire("turn_end", c.ctx, toolTurn("todo"));
	assert.equal(isStartNudge(await busy(emptied, c.ctx)), false, "emptied on purpose");
	await runStart(emptied, c.ctx);
	assert.ok(isStartNudge(await busy(emptied, c.ctx)), "the next owner message arms it again");
});

test("the description tells the agent to start the list unasked, as its own next steps, in at most 258 characters", () => {
	const { description, promptSnippet } = rig().tool();
	for (const said of [/multi-step work/, /before the first edit/, /without being asked/, /your own next steps for this request/, /A change's tasks are in `jhon status`: never copy them here/]) assert.match(description, said);
	assert.doesNotMatch(description, /tasks\.md is the list/);
	assert.ok(description.length <= 258, String(description.length));
	assert.equal(promptSnippet, "Track multi-step work");
});

// T42c: short, and every rule still there.
test("the tool's whole text stays within 900 characters and keeps each rule", () => {
	const tool = rig().tool();
	const text = [tool.description, tool.promptSnippet, ...tool.promptGuidelines].join("\n");
	for (const rule of [/three or more steps/, /exactly one task in_progress/, /done right after/, /complete list/, /keep ids/, /tests fail or the work is partial/, /task for the blocker/, /pending once the condition holds/, /write.*update.*add.*clear.*list/s]) {
		assert.match(text, rule);
	}
	assert.ok(text.length <= 900, String(text.length));
});

test("the list reaches the model when it changes and, untouched for two runs, says it is stale, here and in TODOS", async () => {
	const r = rig();
	const s = session();
	await r.fire("session_start", s.ctx);
	await runStart(r, s.ctx);
	await r.call(s.ctx, { action: "write", tasks: [{ title: "Fix the bug" }] });
	await r.fire("tool_execution_end", s.ctx, { toolName: "todo" });
	const fresh = await runStart(r, s.ctx);
	assert.match(fresh, /^## Todo list/);
	assert.match(fresh, /1\. \[pending\] Fix the bug/);
	assert.doesNotMatch(fresh, /stale/);
	assert.match(await runStart(r, s.ctx), /\(stale: 2 turns without an update — bring the list up to date now\)$/);
	const rows = s.section()!;
	assert.match(rows[0]!, /TODOS +alt\+t {2}stale · 2 turns · 0\/1$/, "T174-S5b");
	assert.equal(rows[1]!.trim(), "· Fix the bug");
});

test("a resumed session does not send again the block its branch carries", async () => {
	const r = rig();
	const probe = session("p");
	await r.fire("session_start", probe.ctx);
	await r.call(probe.ctx, { action: "write", tasks: [{ title: "Fix the bug" }] });
	const block = await runStart(r, probe.ctx);
	assert.match(block, /^## Todo list/);
	const resumed = session("s1", [{ type: "custom_message", customType: "jhon-todo-list", content: block, display: false }, { type: "custom_message", customType: "other", content: "x" }]);
	await r.fire("session_start", resumed.ctx);
	await r.call(resumed.ctx, { action: "write", tasks: [{ title: "Fix the bug" }] });
	assert.equal(await runStart(r, resumed.ctx), "");
});

test("a finished list stays on screen for its turn and is gone at the next run", async () => {
	const r = rig();
	const s = session();
	await r.fire("session_start", s.ctx);
	await r.call(s.ctx, { action: "write", tasks: [{ title: "A", status: "in_progress" }, { title: "B" }] });
	await r.fire("tool_execution_end", s.ctx, { toolName: "todo" });
	assert.match(s.section()!.join("\n"), /◐ A/);
	await r.call(s.ctx, { action: "write", tasks: [{ id: 1, title: "A", status: "done" }, { id: 2, title: "B", status: "done" }] });
	await r.fire("tool_execution_end", s.ctx, { toolName: "todo" });
	await r.fire("agent_end", s.ctx);
	assert.match(s.section()![0]!, /TODOS +alt\+t {2}2\/2$/);
	assert.equal(await runStart(r, s.ctx), START_RULE, "nothing open: the start rule");
	assert.deepEqual(s.section(), []);
	assert.deepEqual((await r.call(s.ctx, { action: "list" })).details.jhonTodo, { tasks: [], nextId: 3, updatedTurn: 0 }, "the ids go on from where they were");
});

test("a new session replays the list from its branch (rpiv-todo's too) and counts the owner's messages as turns", async () => {
	const r = rig();
	const branch = [
		{ type: "message", message: { role: "user", content: "hi" } },
		{ type: "message", message: { role: "toolResult", toolName: "todo", isError: false, details: { action: "create", params: {}, tasks: [{ id: 1, subject: "Old task", status: "in_progress", activeForm: "still going" }], nextId: 2 } } },
		{ type: "message", message: { role: "user", content: "again" } },
	];
	const s = session("s1", branch);
	await r.fire("session_start", s.ctx);
	const rows = s.section()!;
	assert.match(rows[0]!, /TODOS +alt\+t {2}0\/1$/);
	assert.match(rows[1]!, /◐ Old task · still going/);
	await r.call(s.ctx, { action: "add", title: "New" });
	const listed = await r.call(s.ctx, { action: "list" });
	assert.equal(listed.content[0]!.text, "[in_progress] #1 Old task — still going\n[pending] #2 New");
	assert.equal((listed.details.jhonTodo as { updatedTurn: number }).updatedTurn, 2, "two owner messages: turn 2");
	const headless = session("s2", branch, false);
	await r.fire("session_start", headless.ctx);
	assert.equal(headless.section(), undefined, "no UI, no section");
});

test("a list already finished when the session was left is not brought back", async () => {
	const r = rig();
	const s = session("s1", [
		{ type: "message", message: { role: "user", content: "hi" } },
		{ type: "message", message: { role: "toolResult", toolName: "todo", isError: false, details: { jhonTodo: { tasks: [{ id: 1, title: "Done A", status: "done" }, { id: 2, title: "Done B", status: "done" }], nextId: 3, updatedTurn: 1 } } } },
	]);
	await r.fire("session_start", s.ctx);
	assert.deepEqual(s.section(), []);
	assert.equal(await runStart(r, s.ctx), START_RULE);
});

test("blocked and dropped are in the schema and the guidelines", () => {
	const tool = rig().tool();
	const statuses = ["pending", "in_progress", "blocked", "done", "dropped"];
	assert.deepEqual(tool.parameters.properties.status!.enum, statuses);
	assert.deepEqual(tool.parameters.properties.tasks!.items!.properties.status!.enum, statuses);
	assert.match(tool.parameters.properties.tasks!.items!.properties.note!.description!, /required for blocked/);
	assert.ok(tool.promptGuidelines.some((line) => /blocked with a note naming what it waits for/.test(line)));
	assert.ok(tool.promptGuidelines.some((line) => /dropped/.test(line) && /obsolete/.test(line)));
});

test("done and dropped finish a list; a list with only waiting work stays, sent once, never stale", async () => {
	const r = rig();
	const s = session();
	await r.fire("session_start", s.ctx);
	await r.call(s.ctx, { action: "write", tasks: [{ title: "A", status: "done" }, { title: "Old plan", status: "dropped" }] });
	await r.fire("tool_execution_end", s.ctx, { toolName: "todo" });
	await r.fire("agent_end", s.ctx);
	assert.match(s.section()![0]!, /TODOS +alt\+t {2}1\/1$/);
	await runStart(r, s.ctx);
	assert.deepEqual(s.section(), []);
	await r.call(s.ctx, { action: "write", tasks: [{ title: "A", status: "done" }, { title: "Deploy", status: "blocked", note: "waiting for an admin" }] });
	await r.fire("tool_execution_end", s.ctx, { toolName: "todo" });
	for (let run = 0; run < 4; run++) {
		await r.fire("agent_end", s.ctx);
		const sent = await runStart(r, s.ctx);
		if (run === 0) assert.match(sent, /2\. \[blocked\] Deploy — waiting for an admin/);
		else assert.equal(sent, "", "unchanged and not stale: not sent again");
	}
	assert.match(s.section()![2]!, /◉ Deploy · waiting for an admin/);
});

// L154, L1469: a change's tasks are the LDD section's; while a change shows, TODOS hides.
const CHANGE = lddFromStatus("rail-redesign", { route: { level: "verified" }, next: "apply", tasks: { total: 3, completed: 1, slice: "Slice 1: rail", slices: [
	{ name: "Slice 1: rail", total: 3, completed: 1, tasks: [{ text: "Read tasks", done: true }, { text: "Draw rows", done: false }, { text: "Docs", done: false }] },
] } });

function inChange(t: { after(fn: () => void): void }) {
	t.after(shareSelection("s1", async () => CHANGE));
	selectChange("s1", CHANGE);
}

test("with a change selected TODOS hides and the LDD section shows the change's tasks only; without, TODOS shows the agent's only", async (t) => {
	const r = rig();
	const s = session();
	await r.fire("session_start", s.ctx);
	assert.deepEqual(s.section(), []);
	inChange(t);
	assert.deepEqual(s.section(), []);
	await r.call(s.ctx, { action: "write", tasks: [{ title: "write the test", status: "in_progress" }, { title: "fix tsc" }] });
	await r.fire("tool_execution_end", s.ctx, { toolName: "todo" });
	assert.deepEqual(s.section(), [], "the change rules");
	const ldd = renderLddPanel(CHANGE, plain, 42, { now: Date.now() }).map(stripAnsi).join("\n");
	assert.match(ldd, /◆ rail-redesign[\s\S]*◐ 1 rail[\s\S]*✓ Read tasks[\s\S]*◐ Draw rows/);
	assert.doesNotMatch(ldd, /write the test|fix tsc/);
	selectChange("s1", undefined);
	const todos = s.section()!.join("\n");
	assert.match(todos, /TODOS +alt\+t {2}0\/2/);
	assert.match(todos, /◐ write the test[\s\S]*fix tsc/);
	assert.doesNotMatch(todos, /Read tasks|Draw rows|Docs|└|1\/3|rail-redesign|Slice/);
});

// T134 (L1019): inside a change the list is still the agent's own next steps.
test("inside a change the main session still gets the start rule and the start nudge", async (t) => {
	const r = rig();
	const s = session("s1", [], false);
	await r.fire("session_start", s.ctx);
	inChange(t);
	assert.equal(await runStart(r, s.ctx), START_RULE);
	await r.fire("turn_end", s.ctx, toolTurn("edit", { path: "a.ts" }));
	await toolTurns(r, s.ctx, 2);
	assert.ok(isStartNudge(r.nudge()));
});

test("no todo text, nor the sources, tells the agent to copy the change's tasks into its list", async (t) => {
	const copying = /one (todo )?per task|tasks\.md is the list|copy (its|the change's) tasks into/i;
	const r = rig();
	const s = session("s1", [], false);
	await r.fire("session_start", s.ctx);
	inChange(t);
	const rule = await runStart(r, s.ctx);
	await r.fire("turn_end", s.ctx, toolTurn("edit", { path: "a.ts" }));
	await toolTurns(r, s.ctx, 2);
	const nudge = r.nudge()?.message.content ?? "";
	const tool = r.tool();
	const sources = ["../extensions/todo.ts", "../lib/todo.ts"].map((file) => readFileSync(new URL(file, import.meta.url), "utf8"));
	for (const text of [tool.description, tool.promptSnippet, ...tool.promptGuidelines, rule, nudge, ...sources]) assert.doesNotMatch(text, copying);
	assert.ok(rule && nudge);
});

// T146h L4 (L1277): a lean session gets neither the start rule nor the start nudge; an open list still goes.
test("a lean session gets no start rule and no start nudge, but its open list still reaches the model", async (t) => {
	setContextLevel("lean");
	t.after(() => setContextLevel(undefined));
	const r = rig();
	const s = session("s1", [], false);
	await r.fire("session_start", s.ctx);
	assert.equal(await runStart(r, s.ctx), "");
	await r.fire("turn_end", s.ctx, toolTurn("edit", { path: "a.ts" }));
	await toolTurns(r, s.ctx, 4);
	assert.equal(r.nudge(), undefined);
	await r.call(s.ctx, { action: "write", tasks: [{ title: "Fix the bug" }] });
	assert.match(await runStart(r, s.ctx), /1\. \[pending\] Fix the bug/);
	assert.ok(r.tool().promptGuidelines.some((line) => line.includes("A small change keeps no list.")));
});
