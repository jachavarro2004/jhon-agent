/**
 * T115d (L707, L708): the todo list never rewrites the system prompt. Its
 * block reaches the model as a persisted custom message, sent when it
 * changed; the nudges are persisted messages sent after a tool-use turn,
 * never request-local and never timestamped in what the model re-reads.
 */
import "./isolated-env.ts";
import { test } from "node:test";
import assert from "node:assert/strict";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import jhonTodo from "../extensions/todo.ts";

type Handler = (event: unknown, ctx: ExtensionContext) => unknown;

function setup(branch: unknown[] = []) {
	const handlers = new Map<string, Handler[]>();
	const tools = new Map<string, any>();
	const sent: Array<{ message: any; options: any }> = [];
	const pi = {
		on: (n: string, h: Handler) => handlers.set(n, [...(handlers.get(n) ?? []), h]),
		registerTool: (t: any) => tools.set(t.name, t),
		registerShortcut: () => {},
		sendMessage: (message: any, options: any) => sent.push({ message, options }),
	} as unknown as ExtensionAPI;
	jhonTodo(pi, {});
	const ctx = { hasUI: false, sessionManager: { getSessionId: () => "s1", getBranch: () => branch } } as unknown as ExtensionContext;
	const fire = async (n: string, e: unknown = {}) => { let r: unknown; for (const h of handlers.get(n) ?? []) r = (await h(e, ctx)) ?? r; return r; };
	const turn = async () => {
		const systemPromptOptions = { appendSystemPrompt: "", sections: {} as Record<string, string> };
		const result = await fire("before_agent_start", { systemPrompt: "base", systemPromptOptions }) as { message?: any } | undefined;
		return { prompt: JSON.stringify(systemPromptOptions), message: result?.message };
	};
	return { tools, sent, ctx, fire, turn, handlers };
}

const toolTurn = { message: { role: "assistant", content: [{ type: "toolCall", id: "c", name: "read", arguments: {} }] }, toolResults: [] };

test("the system prompt is byte-identical while the todo list changes; the block goes out as a message when it changed", async () => {
	const s = setup();
	await s.fire("session_start");
	const empty = await s.turn();
	assert.equal(empty.message?.customType, "jhon-todo-list");
	// T194 (L1608): the block reaches the model as user text, after the shell's mark.
	assert.match(empty.message.content, /^\[From the Jhon shell, not the owner; it does not set the reply language\] ## Todo list\nInside an LDD change/);
	assert.equal(empty.message.display, false);
	assert.ok(!("timestamp" in empty.message));
	assert.equal((await s.turn()).message, undefined, "an unchanged block is not sent again");
	await s.tools.get("todo").execute("w", { action: "write", tasks: [{ title: "Fix the bug" }] }, undefined, undefined, s.ctx);
	const open = await s.turn();
	assert.equal(open.prompt, empty.prompt, "the system prompt does not change with the list");
	assert.match(open.message.content, /1\. \[pending\] Fix the bug/);
	await s.tools.get("todo").execute("u", { action: "update", id: 1, status: "in_progress" }, undefined, undefined, s.ctx);
	await s.fire("turn_end", { message: { role: "assistant", content: [{ type: "toolCall", id: "c", name: "todo", arguments: {} }] }, toolResults: [] });
	const progressed = await s.turn();
	assert.equal(progressed.prompt, empty.prompt);
	assert.match(progressed.message.content, /1\. \[in_progress\] Fix the bug/);
});

test("a resumed session does not resend the block its branch already carries", async () => {
	const first = setup();
	await first.fire("session_start");
	const { message } = await first.turn();
	const resumed = setup([{ type: "custom_message", customType: "jhon-todo-list", content: message.content, display: false }]);
	await resumed.fire("session_start");
	assert.equal((await resumed.turn()).message, undefined);
});

test("the reminder is a persisted steer message sent after the fourth tool-use turn, with no request-local injection", async () => {
	const s = setup();
	await s.fire("session_start");
	await s.turn();
	await s.tools.get("todo").execute("w", { action: "write", tasks: [{ title: "Implement" }] }, undefined, undefined, s.ctx);
	await s.fire("turn_end", { message: { role: "assistant", content: [{ type: "toolCall", id: "c", name: "todo", arguments: {} }] }, toolResults: [] });
	for (let i = 0; i < 3; i++) await s.fire("turn_end", toolTurn);
	assert.equal(s.sent.length, 0);
	await s.fire("turn_end", toolTurn);
	assert.equal(s.sent.length, 1);
	assert.equal(s.sent[0]!.message.customType, "jhon-todo-reminder");
	assert.equal(s.sent[0]!.message.display, false);
	assert.ok(!("timestamp" in s.sent[0]!.message));
	assert.deepEqual(s.sent[0]!.options, { deliverAs: "steer" });
	assert.equal(s.handlers.has("context"), false, "nothing is injected per request");
});
