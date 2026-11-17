// T92 (L657): jobs and agents get separate views; the Agents view is the
// Gentle Shell's Agents overlay: `Agents · this session · N active · M
// finished`, `Subagent <type>` rows, the selected agent's live thread
// (header, thinking, tool events and their output), and its footer
// [ Follow ] [ Open session ] [Stop] [Scope] with j/k, ctrl+j/k, f, s, a, o, esc, q.
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import type { AgentDef, ChildEvent } from "../lib/agents.ts";
import { createTaskRegistry, type AgentRun, type JobRecord, type TaskRecord } from "../lib/background-tasks.ts";
import { TasksView } from "../lib/background-tasks-view.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";

const theme = { fg: (_role: string, text: string) => text };
const NOW = 125_000;

function agent(overrides: Partial<AgentRun> & Pick<AgentRun, "id">): AgentRun {
	return { kind: "agent", label: "worker: fix it", agent: "worker", task: "fix the parser\nmore", ownerSessionId: "s1", startedAt: 0, status: "running", activity: [], tokens: 0, cost: 0, thread: [], ...overrides };
}
function job(overrides: Partial<JobRecord> & Pick<JobRecord, "id">): JobRecord {
	return { kind: "command", label: overrides.id, command: `run ${overrides.id}`, cwd: "/repo", ownerSessionId: "s1", outputPath: `/tmp/${overrides.id}.log`, startedAt: 0, status: "running", tail: [], ...overrides };
}

function harness(all: TaskRecord[], view: "agents" | "jobs", rows = 20) {
	const opened: string[] = [];
	const stopped: string[] = [];
	const scopes: string[] = [];
	const v = new TasksView({
		theme, rows: () => rows, view, sessionId: "s1", now: () => NOW,
		tasks: (scope) => { scopes.push(scope ?? "session"); return scope === "all" ? all : all.filter((t) => t.ownerSessionId === "s1"); },
		onStop: (t) => { stopped.push(t.id); }, onOpen: (t) => { opened.push(t.id); }, onClose: () => {}, requestRender: () => {},
	});
	return { v, opened, stopped, scopes, screen: (width = 170) => v.render(width).map(stripAnsi) };
}

const live = agent({
	id: "agent-1", model: "claude-bridge/claude-opus-5-5", tokens: 1234, cost: 0.042, childSessionId: "agent-1", sessionDir: "/s/subagents",
	thread: [
		{ kind: "thinking", text: "Look at the parser first." },
		{ kind: "tool", callId: "c1", name: "bash", output: "ok 12 tests", running: false, isError: false },
		{ kind: "tool", callId: "c2", name: "read", output: "", running: true, isError: false },
	],
});

test("the Agents view is G's overlay: its title, Subagent rows, the thread header, thinking and tool events, its footer and keys", () => {
	const h = harness([live, agent({ id: "agent-2", status: "completed", endedAt: 5_000, result: "done" }), job({ id: "job-1" })], "agents");
	try {
		const lines = h.screen();
		const text = lines.join("\n");
		assert.match(lines[0]!, /^╭─ 力 Agents · this session · 1 active · 1 finished ─+ \[F Fullscreen\] \[× Close\]╮$/);
		assert.match(text, /▸ └ ◐ Subagent worker · fix the p…/);
		assert.match(text, / {3}└ ✓ Subagent worker · fix the p…/);
		assert.doesNotMatch(text, /job-1|run job-1|Jobs ·/, "no job in the Agents view");
		assert.match(text, /│ worker · running · claude-bridge\/claude-opus-5-5 · this session · 1\.2k · \$0\.042 · 2m 5s/);
		assert.match(text, /Thinking\s*│?\n.*│ {3}Look at the parser first\./);
		assert.match(text, /Tool · bash · Complete/);
		assert.match(text, /  Output/);
		assert.match(text, / {4}ok 12 tests/);
		assert.match(text, /Tool · read · Running/);
		assert.match(lines.at(-2)!, /^│ \[ Follow \] \[ Open session \] \[Stop\] \[Scope\] j\/k task {3}ctrl\+j\/k scroll {3}f follow {3}s Stop selected {3}a all sessions {3}o open session {3}esc back {3}q close +│$/);
	} finally { h.v.dispose(); }
});

test("a lists every session's agents and back; o opens the selected agent's session; s stops it", () => {
	const other = agent({ id: "agent-3", ownerSessionId: "s0", agent: "explore", label: "explore: map", task: "map the repo" });
	const h = harness([live, other], "agents");
	try {
		assert.doesNotMatch(h.screen().join("\n"), /Subagent explore/);
		h.v.handleInput("a");
		const all = h.screen();
		assert.match(all[0]!, /力 Agents · all sessions · 2 active · 0 finished/);
		assert.match(all.join("\n"), /└ ◐ Subagent explore · map the …/);
		assert.match(all.at(-2)!, /a this session/);
		h.v.handleInput("j");
		assert.match(h.screen().join("\n"), /explore · running · session s0/);
		h.v.handleInput("a");
		assert.match(h.screen()[0]!, /this session · 1 active/);
		h.v.handleInput("o");
		h.v.handleInput("s");
		assert.deepEqual(h.opened, ["agent-1"]);
		assert.deepEqual(h.stopped, ["agent-1"]);
		const unsaved = harness([agent({ id: "agent-9" })], "agents");
		unsaved.v.handleInput("o");
		assert.deepEqual(unsaved.opened, [], "an agent without a saved session has none to open");
		assert.doesNotMatch(unsaved.screen().at(-2)!, /Open session|o open session/);
		unsaved.v.dispose();
	} finally { h.v.dispose(); }
});

test("the Jobs view lists only commands and monitors under `Jobs · N active · M finished`", () => {
	const h = harness([live, job({ id: "job-1", label: "CI" }), job({ id: "job-2", status: "exited", exitCode: 0, endedAt: 1_000 })], "jobs");
	try {
		const lines = h.screen();
		assert.match(lines[0]!, /^╭─ 力 Jobs · 1 active · 1 finished ─+/);
		assert.match(lines.join("\n"), /▸ └ ◐ CI  2m 5s/);
		assert.doesNotMatch(lines.join("\n"), /Subagent|worker/);
		assert.doesNotMatch(lines.at(-2)!, /Open session|Scope|a all sessions/);
	} finally { h.v.dispose(); }
});

test("a background agent's events build its thread: thinking, text, each tool with its status and output; its model and cost", async () => {
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
	assert.equal(run.sessionDir, "/s/subagents");
	assert.deepEqual(run.thread, [
		{ kind: "tool", callId: "c1", name: "bash", output: "ok 12 tests", running: false, isError: false },
		{ kind: "tool", callId: "c2", name: "read", output: "no such file", running: false, isError: true },
		{ kind: "thinking", text: "Done, report." },
		{ kind: "text", text: "All green." },
	]);
	finish();
});
