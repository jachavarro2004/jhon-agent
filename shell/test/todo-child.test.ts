/**
 * T134 H7a (L1019, L1021): jhon-builder, jhon-scout and jhon-checker keep their own todo
 * list. A child pi process (JHON_PI_CHILD=1) whose tools (JHON_PI_TOOLS, its
 * rendered `tools:`) name `todo` registers the tool; the list is the child's
 * alone (no card, no rail, nothing in the parent's list) and a child's
 * bounded task gets no start rule and no nudge.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import jhonTodo, { todoEnabled } from "../extensions/todo.ts";

type Handler = (event: unknown, ctx: ExtensionContext) => unknown;
type Tool = { name: string; execute(id: string, params: unknown, signal: undefined, onUpdate: undefined, ctx: ExtensionContext): Promise<{ content: Array<{ text: string }>; details: Record<string, unknown> }> };

const WORKER_TOOLS = "read,grep,find,ls,edit,write,bash,mcp__engram__mem_search,mcp__codegraph__codegraph_explore,todo";
const child = (agent: string, tools = WORKER_TOOLS) => ({ JHON_PI_CHILD: "1", JHON_PI_AGENT: agent, JHON_PI_TOOLS: tools });

function fakePi() {
	const handlers = new Map<string, Handler[]>();
	const tools = new Map<string, Tool>();
	const shortcuts: string[] = [];
	const sent: unknown[] = [];
	const pi = {
		on: (event: string, handler: Handler) => handlers.set(event, [...(handlers.get(event) ?? []), handler]),
		registerTool: (tool: Tool) => tools.set(tool.name, tool),
		registerShortcut: (key: string) => shortcuts.push(key),
		sendMessage: (message: unknown) => sent.push(message),
	} as unknown as ExtensionAPI;
	const fire = async (event: string, ctx: ExtensionContext, payload: unknown = {}) => {
		let last: unknown;
		for (const handler of handlers.get(event) ?? []) last = (await handler(payload, ctx)) ?? last;
		return last;
	};
	return { pi, tools, shortcuts, sent, handlers, fire };
}

function fakeContext(id: string, branch: unknown[] = []) {
	const widgets: string[] = [];
	const ctx = {
		hasUI: true,
		sessionManager: { getSessionId: () => id, getBranch: () => branch },
		ui: { setWidget: (key: string) => widgets.push(key) },
	} as unknown as ExtensionContext;
	return { ctx, widgets };
}

const turn = (name: string) => ({ message: { role: "assistant", content: [{ type: "toolCall", id: "t", name, arguments: { path: "a.ts" } }] } });

test("a jhon-builder, jhon-scout or jhon-checker child whose tools name todo registers it and can call it", async () => {
	for (const agent of ["worker", "explore", "verify"]) {
		const env = child(agent);
		assert.equal(todoEnabled(env), true, agent);
		const kid = fakePi();
		jhonTodo(kid.pi, env);
		const tool = kid.tools.get("todo");
		assert.ok(tool, `${agent}: the child registers todo`);
		const { ctx } = fakeContext(`child-${agent}`);
		await kid.fire("session_start", ctx);
		const wrote = await tool.execute("c1", { action: "write", tasks: [{ title: "Read the parser", status: "in_progress" }, { title: "Run the focused test" }] }, undefined, undefined, ctx);
		assert.equal((wrote.details.jhonTodo as { tasks: unknown[] }).tasks.length, 2);
		const listed = await tool.execute("c2", { action: "list" }, undefined, undefined, ctx);
		assert.match(listed.content[0]!.text, /Read the parser/);
		assert.match(listed.content[0]!.text, /Run the focused test/);
	}
});

test("a child whose tools do not name todo registers nothing, as before", () => {
	for (const env of [child("prism-safety", "inspect,mcp__codegraph__codegraph_explore"), { JHON_PI_CHILD: "1", JHON_PI_AGENT: "worker" }]) {
		assert.equal(todoEnabled(env), false, JSON.stringify(env));
		const kid = fakePi();
		jhonTodo(kid.pi, env);
		assert.equal(kid.tools.size, 0);
		assert.equal(kid.handlers.size, 0);
		assert.deepEqual(kid.shortcuts, []);
	}
});

test("a child's list stays in the child: no card, no rail, and the parent's list is untouched", async () => {
	const parent = fakePi();
	jhonTodo(parent.pi, {});
	const kid = fakePi();
	jhonTodo(kid.pi, child("worker"));
	assert.deepEqual(kid.shortcuts, [], "a child binds no keys");

	// The same session id on both sides: the worst case for any shared state.
	const parentCtx = fakeContext("s1");
	const kidCtx = fakeContext("s1");
	await parent.fire("session_start", parentCtx.ctx);
	await kid.fire("session_start", kidCtx.ctx);
	await kid.fire("before_agent_start", kidCtx.ctx);
	await kid.tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "Child step", status: "in_progress" }] }, undefined, undefined, kidCtx.ctx);
	await kid.fire("tool_execution_end", kidCtx.ctx, { toolName: "todo" });
	await kid.fire("agent_end", kidCtx.ctx);
	assert.deepEqual(kidCtx.widgets, [], "the child mounts no todo card or rail part");

	const parentList = await parent.tools.get("todo")!.execute("p1", { action: "list" }, undefined, undefined, parentCtx.ctx);
	assert.doesNotMatch(parentList.content[0]!.text, /Child step/);
	assert.deepEqual((parentList.details.jhonTodo as { tasks: unknown[] }).tasks, []);

	// The child's result reaches the parent only inside the subagent tool's result: never a todo list.
	const subagentResult = { type: "message", message: { role: "toolResult", toolName: "subagent", content: [{ type: "text", text: "done" }], details: { jhonTodo: { tasks: [{ id: 1, title: "Child step", status: "in_progress" }], nextId: 2 } } } };
	const resumed = fakePi();
	jhonTodo(resumed.pi, {});
	const resumedCtx = fakeContext("s2", [subagentResult]);
	await resumed.fire("session_start", resumedCtx.ctx);
	const after = await resumed.tools.get("todo")!.execute("p2", { action: "list" }, undefined, undefined, resumedCtx.ctx);
	assert.deepEqual((after.details.jhonTodo as { tasks: unknown[] }).tasks, []);
});

test("a child gets no start rule and no nudge; its open list still reaches it on a continued run", async () => {
	const kid = fakePi();
	jhonTodo(kid.pi, child("worker"));
	const { ctx } = fakeContext("k1");
	await kid.fire("session_start", ctx);
	assert.equal(await kid.fire("before_agent_start", ctx), undefined, "no start rule in a child");
	for (let i = 0; i < 6; i++) await kid.fire("turn_end", ctx, turn("edit"));
	assert.deepEqual(kid.sent, [], "no start nudge in a child");

	await kid.tools.get("todo")!.execute("c1", { action: "write", tasks: [{ title: "Fix the parser", status: "in_progress" }] }, undefined, undefined, ctx);
	for (let i = 0; i < 6; i++) await kid.fire("turn_end", ctx, turn("edit"));
	assert.deepEqual(kid.sent, [], "no stale-list reminder in a child");

	const continued = await kid.fire("before_agent_start", ctx) as { message?: { content: string } } | undefined;
	assert.match(continued?.message?.content ?? "", /1\. \[in_progress\] Fix the parser/);
});
