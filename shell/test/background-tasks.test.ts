/**
 * lib/background-tasks.ts, its sub-agents: the main session runs them in the
 * background beside its commands, in one registry. Each settles exactly once
 * (completed or failed), reported through onSettled; a stop cancels it and
 * reports nothing, as it does for a command.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { createTaskRegistry, type AgentRun, type TaskRecord } from "../lib/background-tasks.ts";
import type { AgentDef, ChildEvent, ChildResult } from "../lib/agents.ts";

const worker: AgentDef = { name: "worker", description: "w", model: "m", tools: [], bounded: true, body: "" };

/** A task registry on a fake clock, whose sub-agents finish when the test says. */
function rig() {
	const runs: Array<{ task: string; signal?: AbortSignal; finish: (r: ChildResult) => void; emit: (event: ChildEvent) => void }> = [];
	let activityChanges = 0;
	const timeouts: Array<number | undefined> = [];
	const settled: TaskRecord[] = [];
	let clock = 100;
	const registry = createTaskRegistry({
		outputDir: () => { throw new Error("no command runs here"); },
		shell: () => { throw new Error("no command runs here"); },
		runAgent: (_agent, task, _cwd, { signal, onEvent, timeoutSeconds }) => new Promise((resolve) => {
			timeouts.push(timeoutSeconds);
			runs.push({ task, signal, finish: resolve, emit: (event) => onEvent?.(event) });
			signal?.addEventListener("abort", () => resolve({ ok: false, text: "cancelled" }), { once: true });
		}),
		now: () => clock++,
		onSettled: (task) => settled.push({ ...task }),
		onActivity: () => { activityChanges++; },
	});
	return { registry, runs, settled, timeouts, activity: () => activityChanges };
}

const tick = () => new Promise((resolve) => setImmediate(resolve));

test("a started sub-agent is running at once and owned by its session", () => {
	const h = rig();
	const run = h.registry.startAgent(worker, "fix the bug", "/repo", "main");
	assert.match(run.id, /^agent-\d+$/);
	assert.equal(run.status, "running");
	assert.equal(run.agent, "worker");
	assert.equal(run.kind, "agent");
	assert.equal(run.label, "worker: fix the bug");
	assert.deepEqual(h.registry.list("main").map((r) => r.id), [run.id]);
	assert.deepEqual(h.registry.list("other"), []);
	assert.equal(h.runs[0]!.task, "fix the bug");
});

test("it settles once as completed or failed, with the sub-agent's answer", async () => {
	const h = rig();
	const ok = h.registry.startAgent(worker, "a", "/repo", "main");
	const bad = h.registry.startAgent(worker, "b", "/repo", "main");
	h.runs[0]!.finish({ ok: true, text: "done: 3 files" });
	h.runs[1]!.finish({ ok: false, text: "exited 1" });
	await tick();
	assert.deepEqual(h.settled.map((r) => [r.id, r.status, r.result]), [[ok.id, "completed", "done: 3 files"], [bad.id, "failed", "exited 1"]]);
	assert.ok(h.registry.get(ok.id)!.endedAt! > ok.startedAt);
});

test("a stop cancels the run and reports nothing; stopping again changes nothing", async () => {
	const h = rig();
	const run = h.registry.startAgent(worker, "a", "/repo", "main");
	assert.equal(h.registry.stop(run.id)?.status, "stopped");
	assert.equal(h.runs[0]!.signal?.aborted, true);
	assert.match(run.result!, /was stopped/);
	await tick();
	h.registry.stop(run.id);
	await tick();
	assert.deepEqual(h.settled, [], "whoever stopped it decides who is told");
	assert.equal(h.registry.get(run.id)?.status, "stopped");
	assert.equal(h.registry.stop("agent-404"), undefined);
});

test("stopAll cancels every running sub-agent without reporting them", async () => {
	const h = rig();
	h.registry.startAgent(worker, "a", "/repo", "main");
	h.registry.startAgent(worker, "b", "/repo", "main");
	await h.registry.stopAll();
	await tick();
	assert.deepEqual(h.registry.list("main").map((r) => r.status), ["stopped", "stopped"]);
	assert.deepEqual(h.settled, [], "the session that stopped them is ending; nobody is left to tell");
});

test("a run keeps its recent activity from the child's events: tools it runs and what it says", () => {
	const h = rig();
	const run = h.registry.startAgent(worker, "a", "/repo", "main");
	h.runs[0]!.emit({ type: "tool_execution_start", toolName: "bash", args: { command: "go test ./..." } });
	h.runs[0]!.emit({ type: "tool_execution_end", toolName: "bash", isError: true });
	h.runs[0]!.emit({ type: "tool_execution_start", toolName: "read", args: { path: "a.ts" } });
	h.runs[0]!.emit({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "Found it.\nThe cause is X." }] } });
	h.runs[0]!.emit({ type: "turn_start" });
	assert.deepEqual((h.registry.get(run.id) as AgentRun).activity, ["bash go test ./...", "✗ bash failed", "read a.ts", "» Found it."]);
	assert.equal(h.activity(), 4, "each line it keeps is announced; other events are not");
	for (let i = 0; i < 40; i++) h.runs[0]!.emit({ type: "tool_execution_start", toolName: "read", args: { path: `f${i}` } });
	assert.equal((h.registry.get(run.id) as AgentRun).activity.length, 30, "only the latest lines are kept");
});

test("a background sub-agent runs with the timeout it was started with", () => {
	const h = rig();
	h.registry.startAgent(worker, "a", "/repo", "main", { timeoutSeconds: 600 });
	h.registry.startAgent(worker, "b", "/repo", "main");
	assert.deepEqual(h.timeouts, [600, undefined]);
});

test("a running sub-agent counts its tokens from each assistant message's usage, for AGENTS (L168)", () => {
	const h = rig();
	const run = h.registry.startAgent(worker, "a", "/repo", "main");
	assert.equal(run.tokens, 0, "none known yet");
	const before = h.activity();
	h.runs[0]!.emit({ type: "message_end", message: { role: "assistant", content: [{ type: "toolCall" }], usage: { input: 10, output: 5, cacheRead: 1000, cacheWrite: 0, totalTokens: 1015 } } } as ChildEvent);
	h.runs[0]!.emit({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "done" }], usage: { totalTokens: 2000 } } } as ChildEvent);
	h.runs[0]!.emit({ type: "message_end", message: { role: "user", content: [], usage: { totalTokens: 99 } } } as ChildEvent);
	assert.equal(run.tokens, 3015);
	assert.ok(h.activity() >= before + 2, "a token change repaints the rail");
});

// T195 (L1607, L1609): two builders on the rail both read "## Why dele…", the
// first line of their briefs. A sub-agent is named by a short label of what it
// does: the one the call gives, else the brief's first meaningful phrase.
const BRIEF = [
	"## Why delegated",
	"Writer rule: the slice touches four files.",
	"",
	"## Allowed edit surfaces",
	"- web/src/pages/categories/**",
	"",
	"Spec: odd/tasks/plan.md, T12 (S3)",
	"",
	"## Task",
	"Build the categories page. Then wire the route.",
].join("\n");

test("a sub-agent's label is the one it was started with, its agent's name before it; the child still gets the whole brief (T195)", () => {
	const h = rig();
	const run = h.registry.startAgent(worker, BRIEF, "/repo", "main", { label: "  web: categories\npage  " });
	assert.equal(run.label, "worker: web: categories page");
	assert.equal(h.runs[0]!.task, BRIEF, "the label is not part of the task");
});

test("without a label, it is the brief's first meaningful phrase: headings, boilerplate sections and Spec lines skipped (T195)", () => {
	const h = rig();
	assert.equal(h.registry.startAgent(worker, BRIEF, "/repo", "main").label, "worker: Build the categories page");
	assert.equal(h.registry.startAgent(worker, "# Categories\n\n**Why delegated:** writer rule.\n- Map who calls `parse()`; then report", "/repo", "main").label, "worker: Map who calls `parse()`");
	const long = "Rewrite the whole billing export so every invoice row carries its tax lines";
	assert.equal(h.registry.startAgent(worker, `## Goal\n${long}`, "/repo", "main").label, `worker: ${long.slice(0, 39)}…`, "cut to the rail's width");
	assert.equal(h.registry.startAgent(worker, "## Why delegated\nWriter rule.\n## Allowed edit surfaces\n- a.ts", "/repo", "main").label, "worker", "nothing meaningful: the agent's name alone, never a heading");
	assert.equal(h.registry.startAgent(worker, "fix the bug", "/repo", "main", { label: "   " }).label, "worker: fix the bug", "a blank label is no label");
});
