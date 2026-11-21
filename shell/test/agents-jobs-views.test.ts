// T92 (L657): jobs and agents get separate views (test/ui-tasks-view.test.ts
// draws them, design 09). Here: what a background sub-agent's child events
// leave in its record for the Agents view to draw: its thread (thinking, text,
// each tool with its target, status and output), its model and its cost.
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import type { AgentDef, ChildEvent } from "../lib/agents.ts";
import { applyThreadEvent, createTaskRegistry, type ThreadItem } from "../lib/background-tasks.ts";

test("a background agent's events build its thread: thinking, text, each tool with its target, status and output; its model and cost", async () => {
	const events: ChildEvent[] = [
		{ type: "tool_execution_start", toolCallId: "c1", toolName: "bash", args: { command: "npm test" } },
		{ type: "tool_execution_end", toolCallId: "c1", toolName: "bash", result: { content: [{ type: "text", text: "ok 12 tests" }] }, isError: false },
		{ type: "tool_execution_start", toolCallId: "c2", toolName: "read", args: { path: "a.ts" } },
		{ type: "tool_execution_end", toolCallId: "c2", toolName: "read", result: { content: [{ type: "text", text: "no such file" }] }, isError: true },
		{ type: "message_end", message: { role: "assistant", content: [{ type: "thinking", thinking: "Done, report." }, { type: "text", text: "All green." }], usage: { totalTokens: 900, cost: { total: 0.03 } } } },
	];
	let finish!: () => void;
	const registry = createTaskRegistry({
		outputDir: () => "/tmp", now: () => 0, shell: () => { throw new Error("no jobs"); }, onSettled: () => {},
		runAgent: (_a, _t, _c, options) => { for (const e of events) options.onEvent(e); return new Promise((resolve) => { finish = () => resolve({ ok: true, text: "All green." }); }); },
	});
	const def: AgentDef = { name: "worker", description: "", model: "claude-bridge/claude-opus-5-5", tools: [], bounded: false, body: "" };
	const run = registry.startAgent(def, "fix", "/repo", "s1", { session: { dir: "/s/subagents", id: "agent-1", meta: { agentType: "worker", toolUseId: "t", parentSession: "s1" } } });
	assert.equal(run.model, "claude-bridge/claude-opus-5-5");
	assert.equal(run.cost, 0.03);
	assert.equal(run.tokens, 900);
	assert.equal(run.sessionDir, "/s/subagents");
	assert.deepEqual(run.thread, [
		{ kind: "tool", callId: "c1", name: "bash", subject: "npm test", output: "ok 12 tests", running: false, isError: false },
		{ kind: "tool", callId: "c2", name: "read", subject: "a.ts", output: "no such file", running: false, isError: true },
		{ kind: "thinking", text: "Done, report." },
		{ kind: "text", text: "All green." },
	]);
	finish();
});

test("a tool's target (its command, path, pattern or agent, as the activity line picks it): set at its start, kept at its end, absent without one", () => {
	const thread: ThreadItem[] = [];
	applyThreadEvent(thread, { type: "tool_execution_start", toolCallId: "c1", toolName: "grep", args: { pattern: "parse(", path: "src" } });
	assert.deepEqual(thread, [{ kind: "tool", callId: "c1", name: "grep", subject: "src", output: "", running: true, isError: false }], "path before pattern, as activityLine");
	applyThreadEvent(thread, { type: "tool_execution_end", toolCallId: "c1", toolName: "grep", result: { content: [{ type: "text", text: "a.ts:3" }] }, isError: false });
	assert.deepEqual(thread, [{ kind: "tool", callId: "c1", name: "grep", subject: "src", output: "a.ts:3", running: false, isError: false }]);
	applyThreadEvent(thread, { type: "tool_execution_start", toolCallId: "c2", toolName: "subagent", args: { agent: "explore", task: "map" } });
	applyThreadEvent(thread, { type: "tool_execution_start", toolCallId: "c3", toolName: "todo", args: { tasks: [] } });
	applyThreadEvent(thread, { type: "tool_execution_end", toolCallId: "c3", toolName: "todo", result: { content: [] }, isError: false });
	assert.equal((thread[1] as { subject?: string }).subject, "explore");
	assert.deepEqual(thread[2], { kind: "tool", callId: "c3", name: "todo", output: "", running: false, isError: false }, "no target: no subject key");
	applyThreadEvent(thread, { type: "tool_execution_end", toolCallId: "c9", toolName: "bash", result: { content: [] }, isError: true });
	assert.deepEqual(thread[3], { kind: "tool", callId: "c9", name: "bash", output: "", running: false, isError: true }, "an end without its start has no target");
});
