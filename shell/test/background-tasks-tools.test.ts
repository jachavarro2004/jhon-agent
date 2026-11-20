/**
 * lib/background-tasks-tools.ts: background tasks in Jhon's main session. A
 * job's exit reaches the agent as a follow-up message through pi's own
 * delivery, the shell UI hears the running tasks, task_list, task_result, and
 * task_stop take any task, and a sub-agent gets none of it. A foreground
 * wait (a standalone sleep while a task runs, a long sleep, a known long
 * wait) runs, and its result carries an advisory note (T155, L1394).
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test, { after } from "node:test";
import { backgroundTasksKey, registerBackgroundTasks, TASK_NOTICE_TYPE, type BackgroundTasksDeps } from "../lib/background-tasks-tools.ts";
import { OPEN_TASKS_EVENT, TASKS_EVENT } from "../lib/events.ts";
import type { JobExecOperations } from "../lib/ui/background-jobs.ts";
import type { AgentDef, ChildResult } from "../lib/agents.ts";
import { envOverrides, memorySettings } from "./fake-jhon-settings.ts";

// The settings `jhon settings` would give the shell under an environment (S38).
const envSettings = (env: NodeJS.ProcessEnv) => memorySettings({}, undefined, envOverrides(env));

const dirs: string[] = [];
after(() => { for (const d of dirs) rmSync(d, { recursive: true, force: true }); });

type Handler = (event: any, ctx: any) => unknown;

function fakeShell() {
	const runs: Array<{ command: string; finish: (code: number) => void; output: (text: string) => void }> = [];
	const operations: JobExecOperations = {
		exec: (command, _cwd, options) => new Promise((resolve, reject) => {
			runs.push({ command, finish: (exitCode) => resolve({ exitCode }), output: (text) => options.onData(Buffer.from(text)) });
			options.signal?.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
		}),
	};
	return { runs, operations };
}

function harness(env: NodeJS.ProcessEnv = {}, extra: Partial<BackgroundTasksDeps> = {}) {
	const tools = new Map<string, any>();
	const commands = new Map<string, any>();
	const handlers = new Map<string, Handler[]>();
	const sent: Array<{ message: any; options: any }> = [];
	const emitted: Array<{ name: string; value: any }> = [];
	const listeners = new Map<string, (value: any) => void>();
	const pi = {
		registerTool: (tool: any) => tools.set(tool.name, tool),
		registerCommand: (name: string, command: any) => commands.set(name, command),
		registerMessageRenderer: () => {},
		on: (name: string, handler: Handler) => handlers.set(name, [...(handlers.get(name) ?? []), handler]),
		sendMessage: (message: any, options: any) => { sent.push({ message, options }); },
		events: { emit: (name: string, value: any) => emitted.push({ name, value }), on: (name: string, fn: (v: any) => void) => { listeners.set(name, fn); return () => listeners.delete(name); } },
	};
	const shell = fakeShell();
	const outputDir = mkdtempSync(join(tmpdir(), "jhon-jobs-test-"));
	dirs.push(outputDir);
	const answers: Array<(r: ChildResult) => void> = [];
	const runAgent = (_agent: AgentDef, _task: string, _cwd: string, { signal }: { signal: AbortSignal }) => new Promise<ChildResult>((resolve) => {
		answers.push(resolve);
		signal.addEventListener("abort", () => resolve({ ok: false, text: "cancelled" }), { once: true });
	});
	const background = registerBackgroundTasks(pi as any, { env, outputDir: () => outputDir, shell: () => ({ operations: shell.operations }), runAgent, now: () => 1_000, schedule: (fn) => { const t = setTimeout(fn, 0); return () => clearTimeout(t); }, ...extra });
	const ctx = { cwd: outputDir, hasUI: false, mode: "rpc", sessionManager: { getSessionId: () => "main" }, ui: { setStatus: () => {}, notify: () => {} } };
	const fire = async (name: string, event: any = {}) => {
		let result: unknown;
		for (const handler of handlers.get(name) ?? []) result = (await handler(event, ctx)) ?? result;
		return result;
	};
	const run = (name: string, params: any) => tools.get(name).execute("call-1", params, undefined, undefined, ctx);
	return { tools, commands, sent, emitted, listeners, shell, answers, background, ctx, fire, run };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 20));

/** Waits until check holds, polling, for at most two seconds: a job's log closes asynchronously. */
async function until(check: () => boolean): Promise<void> {
	for (const deadline = Date.now() + 2000; !check(); await settle()) if (Date.now() > deadline) throw new Error("timed out waiting");
}

const worker: AgentDef = { name: "worker", description: "w", model: "m", tools: [], bounded: true, body: "" };

test("the main session gets bash_background, monitor, task_list, task_result, task_stop, and the separate /jhon:agents and /jhon:jobs views (T92, L657)", () => {
	const h = harness();
	assert.deepEqual([...h.tools.keys()].sort(), ["bash_background", "monitor", "task_list", "task_result", "task_stop"]);
	assert.deepEqual([...h.commands.keys()], ["jhon:agents", "jhon:jobs"]);
});

test("a sub-agent gets no background tasks: they would die with its process", () => {
	const h = harness({ JHON_PI_CHILD: "1" });
	assert.equal(h.background, undefined);
	assert.equal(h.tools.size, 0);
	assert.equal(h.commands.size, 0);
});

test("a job's exit reaches the agent as a follow-up that starts a turn when idle, and the UI hears the running jobs", async () => {
	const h = harness();
	await h.fire("session_start");
	await h.tools.get("bash_background").execute("call-1", { command: "make build", label: "build" }, undefined, undefined, h.ctx);
	assert.equal(h.shell.runs.length, 1);
	const started = h.emitted.filter((e) => e.name === TASKS_EVENT).at(-1);
	assert.deepEqual(started?.value.tasks.map((task: any) => task.label), ["build"]);
	assert.equal(started?.value.sessionId, "main");
	h.shell.runs[0]!.output("compiled\n");
	h.shell.runs[0]!.finish(0);
	await until(() => h.sent.length > 0);
	const notice = h.sent.at(-1);
	assert.equal(notice?.message.customType, TASK_NOTICE_TYPE);
	assert.match(notice?.message.content, /build/);
	assert.deepEqual(notice?.options, { triggerTurn: true, deliverAs: "followUp" });
	assert.deepEqual(h.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)?.value.tasks, []);
});

// T47 (L164): agent-2's notice was lost because pi's follow-up queue, where a
// notice sent mid-turn waited, is cleared when the owner presses Esc. A notice
// that lands while a turn runs waits in the shell and goes out exactly once,
// when the turn settles.
test("a notice that lands while a turn runs is held, then delivered exactly once when the turn settles", async () => {
	const h = harness();
	let idle = false;
	(h.ctx as any).isIdle = () => idle;
	await h.fire("session_start");
	await h.tools.get("bash_background").execute("call-1", { command: "make build", label: "build" }, undefined, undefined, h.ctx);
	h.shell.runs[0]!.finish(0);
	await until(() => h.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)?.value.tasks.length === 0);
	await settle();
	assert.equal(h.sent.length, 0, "nothing enters pi's queue mid-turn");
	// A settle that pi reports while a run is still in flight keeps it waiting.
	await h.fire("agent_settled", { type: "agent_settled", aborted: false });
	assert.equal(h.sent.length, 0);
	idle = true;
	await h.fire("agent_settled", { type: "agent_settled", aborted: false });
	assert.equal(h.sent.length, 1);
	assert.equal(h.sent[0]!.message.customType, TASK_NOTICE_TYPE);
	assert.match(h.sent[0]!.message.content, /build/);
	assert.deepEqual(h.sent[0]!.options, { triggerTurn: true, deliverAs: "followUp" });
	await h.fire("agent_settled", { type: "agent_settled", aborted: false });
	assert.equal(h.sent.length, 1, "delivered once");
});

test("a notice held through a turn the owner aborted rides with the next message instead of restarting the agent", async () => {
	const h = harness();
	let idle = false;
	(h.ctx as any).isIdle = () => idle;
	await h.fire("session_start");
	const started = h.background!.startAgent(worker, "review", h.ctx.cwd, "main");
	h.answers[0]!({ ok: true, text: "report ready" });
	await until(() => h.background!.agentRun(started.id, "main")?.status === "completed");
	await settle();
	assert.equal(h.sent.length, 0);
	idle = true;
	await h.fire("agent_settled", { type: "agent_settled", aborted: true });
	assert.equal(h.sent.length, 1);
	assert.match(h.sent[0]!.message.content, /report ready/);
	assert.deepEqual(h.sent[0]!.options, { deliverAs: "nextTurn" });
	await h.fire("agent_settled", { type: "agent_settled", aborted: false });
	assert.equal(h.sent.length, 1, "delivered once");
});

// T155 (L1377, L1394): a foreground wait is advisory: the command runs, and its result carries one note.
const bash = (h: ReturnType<typeof harness>, command: string, toolName = "bash") => h.fire("tool_call", { toolName, toolCallId: "call-w", input: { command } }) as Promise<{ block: boolean; reason: string } | undefined>;
/** The call, then its result: the guard's verdict and the notes appended to the output. */
async function ran(h: ReturnType<typeof harness>, command: string, toolName = "bash", toolCallId = "call-w") {
	const call = await h.fire("tool_call", { toolName, toolCallId, input: { command } });
	const result = await h.fire("tool_result", { toolName, toolCallId, input: { command }, content: [{ type: "text", text: "out" }], isError: false }) as { content?: Array<{ text: string }> } | undefined;
	if (result) assert.equal(result.content![0]!.text, "out", "the output as the command gave it");
	return { call, notes: (result?.content ?? []).slice(1).map((c) => c.text) };
}

test("a standalone sleep while a job runs still runs, and its result says the wait only held the turn", async () => {
	const h = harness();
	await h.fire("session_start");
	assert.deepEqual(await ran(h, "sleep 3"), { call: undefined, notes: [] }, "no job runs");
	await h.tools.get("bash_background").execute("call-1", { command: "gh run watch 1" }, undefined, undefined, h.ctx);
	for (const command of ["sleep 3", "sleep 30"]) {
		const { call, notes } = await ran(h, command);
		assert.equal(call, undefined, command);
		assert.deepEqual(notes, ["Standalone sleep while background work runs (1 background task): it only held the turn. Each task's end arrives as a message and starts a new turn, so end the turn instead."], command);
	}
	assert.deepEqual(await ran(h, "npm test"), { call: undefined, notes: [] });
});

test("a standalone sleep while a background sub-agent runs gets the note too, and none once it ended", async () => {
	const h = harness();
	await h.fire("session_start");
	h.background!.startAgent(worker, "fix it", h.ctx.cwd, "main");
	const { call, notes } = await ran(h, "sleep 30");
	assert.equal(call, undefined);
	assert.match(notes[0]!, /^Standalone sleep while background work runs \(1 background task\)/);
	h.answers[0]!({ ok: true, text: "fixed" });
	await until(() => h.sent.length > 0);
	assert.deepEqual(await ran(h, "sleep 3"), { call: undefined, notes: [] });
});

const waitNote = (wait: string) => `Foreground wait: ${wait} held the turn until it ended. Next time run a wait with bash_background, its condition inside the command (for example \`until <check>; do sleep 5; done; <cmd>\`), or watch the output with monitor; its end arrives as a message, so the turn can go on with other work or end.`;

for (const [command, wait] of [["sleep 420", "`sleep 420`"], ["sleep 500; gh pr checks 12", "`sleep 500`"], ["sleep 60 && npm test", "`sleep 60`"], ["sleep 2m\nmake", "`sleep 2m`"], ["  sleep 10;make", "`sleep 10`"]]) {
	test(`a foreground bash that starts by sleeping over 5 s runs, and its result points to bash_background with the wait inside: ${JSON.stringify(command)}`, async () => {
		const h = harness();
		await h.fire("session_start");
		assert.deepEqual(await ran(h, command!), { call: undefined, notes: [waitNote(wait!)] });
	});
}

for (const [command, wait] of [["gh run watch 123 --exit-status", "`gh run watch`"], ["cd app && gh run watch", "`gh run watch`"], ["wait", "`wait`"], ["make & wait $!", "`wait`"], ["tail -f build.log", "`tail -f`"], ["tail -F log | grep -m1 ready", "`tail -f`"], ["timeout 600 npm test", "`timeout 600`"], ["timeout 2m make", "`timeout 2m`"]]) {
	test(`a foreground bash that is a known long wait runs, and its result points to bash_background or monitor: ${JSON.stringify(command)}`, async () => {
		const h = harness();
		await h.fire("session_start");
		assert.deepEqual(await ran(h, command!), { call: undefined, notes: [waitNote(wait!)] });
	});
}

test("the note comes once per call, on that call's result alone, a failed one too", async () => {
	const h = harness();
	await h.fire("session_start");
	assert.equal(await bash(h, "sleep 420"), undefined);
	const other = await h.fire("tool_result", { toolName: "bash", toolCallId: "call-other", input: { command: "ls" }, content: [{ type: "text", text: "a" }], isError: false });
	assert.equal(other, undefined, "another call's result");
	const failed = await h.fire("tool_result", { toolName: "bash", toolCallId: "call-w", input: { command: "sleep 420" }, content: [{ type: "text", text: "Command aborted" }], isError: true }) as any;
	assert.deepEqual(failed.content.map((c: any) => c.text), ["Command aborted", waitNote("`sleep 420`")]);
	assert.equal(await h.fire("tool_result", { toolName: "bash", toolCallId: "call-w", input: { command: "sleep 420" }, content: [{ type: "text", text: "x" }], isError: false }), undefined, "once");
});

for (const command of ["sleep 5", "sleep 0.5", "sleep 3 && ls", "for i in 1 2 3; do sleep 0.5; done", "until curl -s localhost:3000; do sleep 5; done; npm test", "while ! test -f done; do sleep 10; done", "npm test", "timeout 30 npm test", "tail -n 50 build.log", "echo wait", "git log --oneline -3", "make sleep 30"]) {
	test(`a foreground bash that does not just wait runs with no note: ${JSON.stringify(command)}`, async () => {
		const h = harness();
		await h.fire("session_start");
		assert.deepEqual(await ran(h, command), { call: undefined, notes: [] });
	});
}

for (const command of ["sleep 420; gh pr checks 12", "until gh run view 1 --json status | grep -q completed; do sleep 5; done; gh run view 1", "gh run watch 1", "tail -f log"]) {
	test(`a wait inside bash_background or monitor gets no note: ${JSON.stringify(command)}`, async () => {
		const h = harness();
		await h.fire("session_start");
		assert.deepEqual(await ran(h, command, "bash_background"), { call: undefined, notes: [] });
		assert.deepEqual(await ran(h, command, "monitor"), { call: undefined, notes: [] });
	});
}

test("session shutdown stops every job without a notice", async () => {
	const h = harness();
	await h.fire("session_start");
	await h.tools.get("bash_background").execute("call-1", { command: "npm run dev" }, undefined, undefined, h.ctx);
	await h.fire("session_shutdown");
	await settle();
	assert.equal(h.sent.length, 0);
});

test("task_list, task_result, and task_stop take jobs and sub-agents alike, by id", async () => {
	const h = harness();
	await h.fire("session_start");
	await h.run("bash_background", { command: "make ci", label: "ci" });
	const agent = h.background!.startAgent(worker, "fix the bug", h.ctx.cwd, "main");
	const list = (await h.run("task_list", {})).content[0].text;
	assert.match(list, /^job-1 · command · running · ci/m);
	assert.match(list, new RegExp(`^${agent.id} · agent · running · worker: fix the bug`, "m"));
	assert.match((await h.run("task_result", { id: "job-1" })).content[0].text, /is running[\s\S]*Command: make ci/);
	assert.match((await h.run("task_result", { id: agent.id })).content[0].text, /sub-agent agent-1 \(worker\) is running/);
	assert.match((await h.run("task_stop", { id: agent.id })).content[0].text, /Stopped agent-1/);
	assert.match((await h.run("task_stop", { id: "job-1" })).content[0].text, /Stopped job-1/);
	assert.match((await h.run("task_stop", { id: "job-1" })).content[0].text, /already been stopped/);
	await new Promise((r) => setTimeout(r, 20));
	assert.equal(h.sent.length, 0, "a task the agent stopped sends no notice");
	assert.match((await h.run("task_result", { id: agent.id })).content[0].text, /was stopped/);
	await assert.rejects(h.run("task_result", { id: "agent-404" }), /No background task agent-404 in this session/);
});

test("a background sub-agent's answer arrives as a task notice, and no notification event (T169)", async () => {
	const h = harness();
	await h.fire("session_start");
	const agent = h.background!.startAgent(worker, "who calls parse?", h.ctx.cwd, "main");
	assert.deepEqual(h.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)?.value.tasks.map((t: any) => t.id), [agent.id]);
	h.answers[0]!({ ok: true, text: "found 3 callers" });
	await until(() => h.sent.length > 0);
	const notice = h.sent[0]!;
	assert.equal(notice.message.customType, TASK_NOTICE_TYPE);
	assert.match(notice.message.content, /sub-agent agent-1 \(worker\) completed after 0s\.\n\nfound 3 callers/);
	assert.equal(notice.message.details.jhonTasks.succeeded, true);
	assert.equal(h.emitted.some((e) => e.name === "jhon-shell:notification:source"), false, "the notification sounds are gone (T169)");
	assert.deepEqual(h.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)?.value.tasks, []);
});

test("/jhon:agents and /jhon:jobs need a terminal", async () => {
	const h = harness();
	const noted: string[] = [];
	for (const name of ["jhon:agents", "jhon:jobs"]) await h.commands.get(name).handler("", { ...h.ctx, hasUI: true, mode: "rpc", ui: { notify: (m: string) => { noted.push(m); } } });
	assert.deepEqual(noted, ["The Agents view needs TUI mode.", "The Jobs view needs TUI mode."]);
});

// T15.12d (L158): a tap on the narrow work row while sub-agents lead opens
// this view, as the Gentle Shell's opens its agents view: the shell asks on
// pi.events, never by sending /jhon:agents as a message; the JOBS card asks for /jhon:jobs.
test("the open-tasks event opens /jhon:agents, or /jhon:jobs when it names the jobs view", async () => {
	const h = harness();
	const noted: string[] = [];
	const ctx = { ...h.ctx, hasUI: true, mode: "rpc", ui: { notify: (m: string) => { noted.push(m); } } };
	h.listeners.get(OPEN_TASKS_EVENT)?.({ ctx });
	h.listeners.get(OPEN_TASKS_EVENT)?.({ ctx, view: "jobs" });
	await settle();
	assert.deepEqual(noted, ["The Agents view needs TUI mode.", "The Jobs view needs TUI mode."]);
});

test("the BACKGROUND key defaults to alt+s; JHON_SHELL_TASKS_KEY sets another or turns it off", () => {
	assert.equal(backgroundTasksKey(envSettings({})), "alt+s");
	assert.equal(backgroundTasksKey(envSettings({ JHON_SHELL_TASKS_KEY: "ctrl+shift+b" })), "ctrl+shift+b");
	assert.equal(backgroundTasksKey(envSettings({ JHON_SHELL_TASKS_KEY: "off" })), undefined);
	assert.equal(backgroundTasksKey(envSettings({ JHON_SHELL_TASKS_KEY: "" })), undefined);
});

test("the BACKGROUND key (alt+s) opens /jhon:agents, and registers nothing when off", async () => {
	const register = (env: NodeJS.ProcessEnv) => {
		const shortcuts = new Map<string, any>();
		const commands = new Map<string, any>();
		const pi = { registerTool: () => {}, registerCommand: (n: string, c: any) => commands.set(n, c), registerShortcut: (k: string, s: any) => shortcuts.set(k, s), registerMessageRenderer: () => {}, on: () => {}, sendMessage: () => {}, events: { emit: () => {}, on: () => () => {} } };
		registerBackgroundTasks(pi as any, { env, settings: envSettings(env), outputDir: () => tmpdir() });
		return { shortcuts, commands };
	};
	const { shortcuts } = register({});
	assert.deepEqual([...shortcuts.keys()], ["alt+s"]);
	let noted = "";
	await shortcuts.get("alt+s").handler({ hasUI: true, mode: "rpc", sessionManager: { getSessionId: () => "main" }, ui: { notify: (m: string) => { noted = m; } } });
	assert.equal(noted, "The Agents view needs TUI mode.");
	assert.equal(register({ JHON_SHELL_TASKS_KEY: "off" }).shortcuts.size, 0);
});

test("the owner's own command shows among the running tasks, ends in its panel and never as the agent's turn, and the agent's tools never see it", async () => {
	const h = harness();
	await h.fire("session_start");
	const ends: Array<number | null | undefined> = [];
	const job = h.background!.startOwnerCommand("jhon executions run c --affected main", h.ctx.cwd, "main", "tests of c (you)", (j) => ends.push(j.exitCode));
	assert.deepEqual(h.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)?.value.tasks.map((task: any) => task.label), ["tests of c (you)"], "SEGUNDO PLANO shows it");
	const list = await h.run("task_list", {});
	assert.match(list.content[0].text, /No background tasks/, "the agent's task_list does not list it");
	await assert.rejects(h.run("task_stop", { id: job.id }), /No background task/, "nor can the agent stop it");
	h.shell.runs[0]!.finish(1);
	await until(() => ends.length > 0);
	assert.deepEqual(ends, [1]);
	assert.equal(h.sent.length, 0, "no notice reaches the agent");

	const stopped: string[] = [];
	const second = h.background!.startOwnerCommand("sleep 1", h.ctx.cwd, "main", "x", (j) => stopped.push(j.status));
	h.background!.stopOwnerCommand(second.id);
	await settle();
	assert.deepEqual(stopped, ["stopped"], "the panel hears a stop of its own, once");
	assert.equal(h.sent.length, 0);
});

test("the UI hears a finished job beside the running ones, for JOBS' finished count, and a sub-agent's tokens (L168)", async () => {
	const h = harness();
	await h.fire("session_start");
	await h.tools.get("bash_background").execute("call-1", { command: "make build", label: "build" }, undefined, undefined, h.ctx);
	h.shell.runs[0]!.finish(0);
	await until(() => h.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)?.value.tasks.length === 0);
	const last = h.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)?.value;
	assert.deepEqual(last.tasks, [], "running tasks only, as before");
	assert.deepEqual(last.finished.map((task: any) => [task.label, task.kind, task.status, task.exitCode, typeof task.endedAt]), [["build", "command", "exited", 0, "number"]]);
	h.background!.startAgent(worker, "fix it", h.ctx.cwd, "main");
	const agent = h.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)?.value.tasks[0];
	assert.equal(agent.kind, "agent");
	assert.equal(agent.tokens, 0);
});

// T92 (L658): the rail's STATS counts what this session's sub-agents spent, finished ones too.
test("the running-tasks event carries this session's sub-agent spend: runs, tokens and cost", async () => {
	const emitted: any[] = [];
	const handlers = new Map<string, Handler>();
	const pi = { registerTool: () => {}, registerCommand: () => {}, registerShortcut: () => {}, registerMessageRenderer: () => {}, on: (name: string, fn: Handler) => handlers.set(name, fn), sendMessage: () => {}, events: { emit: (name: string, value: any) => { if (name === TASKS_EVENT) emitted.push(value); }, on: () => () => {} } };
	const usage = { type: "message_end", message: { role: "assistant", content: [], usage: { totalTokens: 1000, cost: { total: 0.25 } } } };
	const background = registerBackgroundTasks(pi as any, { env: {}, outputDir: () => tmpdir(), now: () => 1_000,
		runAgent: (_a: AgentDef, _t: string, _c: string, options: any) => { options.onEvent(usage); return Promise.resolve({ ok: true, text: "done" }); } });
	await handlers.get("session_start")!({}, { sessionManager: { getSessionId: () => "main" }, ui: { setStatus: () => {} } });
	background!.startAgent(worker, "a", tmpdir(), "main");
	background!.startAgent(worker, "b", tmpdir(), "main");
	background!.startAgent(worker, "c", tmpdir(), "other");
	await settle();
	assert.deepEqual(emitted.at(-1)?.spend, { runs: 2, tokens: 2000, cost: 0.5 });
});

// T115b (L691, L692): a completion notice enters the agent's context and is re-read on every later
// turn, so a long result arrives bounded, with a pointer to task_result, which keeps the whole of it.
async function agentNotice(result: string) {
	const h = harness();
	await h.fire("session_start");
	const agent = h.background!.startAgent(worker, "review", h.ctx.cwd, "main");
	h.answers[0]!({ ok: true, text: result });
	await until(() => h.sent.length > 0);
	return { h, agent, content: h.sent[0]!.message.content as string, details: h.sent[0]!.message.details };
}

test("a long sub-agent result arrives bounded at a line, with how much was left out and task_result to read it", async () => {
	const result = Array.from({ length: 200 }, (_, i) => `line ${i} ${"x".repeat(40)}`).join("\n");
	const { h, agent, content, details } = await agentNotice(result);
	assert.ok(content.length < 2_000, `notice is ${content.length} chars`);
	assert.ok(content.startsWith("Background sub-agent agent-1 (worker) completed after 0s.\n\nline 0 "));
	const shown = content.split("\n\n").slice(1).join("\n\n").split("\n").slice(0, -1);
	for (const line of shown) assert.match(line, /^line \d+ x{40}$/, "cut at a line boundary");
	const left = result.length - shown.join("\n").length;
	assert.equal(content.split("\n").at(-1), `[${left} more characters not shown; task_result with id "${agent.id}" returns the full result.]`);
	assert.equal(details.jhonTasks.succeeded, true);
	assert.equal((await h.run("task_result", { id: agent.id })).content[0].text, `Background sub-agent agent-1 (worker) completed after 0s.\n\n${result}`);
});

test("a long JSON result (a review lens's report) is summarized by its key fields, never cut mid-JSON", async () => {
	const findings = Array.from({ length: 40 }, (_, i) => ({ location: `a.ts:${i}`, severity: i % 3 === 0 ? "CRITICAL" : "WARNING", claim: `claim ${i} ${"y".repeat(200)}`, evidence_class: "deterministic", causal_disposition: "introduced", proof_refs: ["p"] }));
	const report = JSON.stringify({ subject_hash: "abc123", lens: "risk", inspection: { status: "completed", paths: ["a.ts", "b.ts"] }, findings, evidence: ["read a.ts"] });
	const { h, agent, content } = await agentNotice(report);
	assert.ok(content.length < 2_000, `notice is ${content.length} chars`);
	assert.doesNotMatch(content, /\{"subject_hash"/, "no raw JSON");
	assert.match(content, /^JSON result, summarized:$/m);
	assert.match(content, /^subject_hash: abc123$/m);
	assert.match(content, /^lens: risk$/m);
	assert.match(content, /^inspection\.status: completed$/m);
	assert.match(content, /^findings: 40 \(CRITICAL 14, WARNING 26\)$/m);
	assert.match(content, /^- CRITICAL a\.ts:0: claim 0 y+…$/m);
	assert.equal(content.split("\n").at(-1), `[Full JSON (${report.length} characters): task_result with id "${agent.id}" returns it.]`);
	assert.ok((await h.run("task_result", { id: agent.id })).content[0].text.endsWith(`\n\n${report}`));
});

test("a short result's notice is unchanged", async () => {
	const { content } = await agentNotice('{"findings":[],"evidence":["read a.ts"]}');
	assert.equal(content, 'Background sub-agent agent-1 (worker) completed after 0s.\n\n{"findings":[],"evidence":["read a.ts"]}');
});

test("a JSON summary lists the most severe findings first and counts the unlisted ones by severity", async () => {
	const finding = (i: number, severity: string) => ({ location: `b.ts:${i}`, severity, claim: `claim ${i} ${"z".repeat(300)}` });
	const findings = [...[0, 1, 2, 3, 4, 5].map((i) => finding(i, "WARNING")), finding(6, "CRITICAL"), finding(7, "INFO"), finding(8, "odd")];
	const { content } = await agentNotice(JSON.stringify({ findings }));
	const listed = content.split("\n").filter((line) => line.startsWith("- "));
	assert.deepEqual(listed.map((line) => line.split(":")[0]), ["- CRITICAL b.ts", "- WARNING b.ts", "- WARNING b.ts", "- WARNING b.ts", "- WARNING b.ts", "- … 4 more not listed (WARNING 2, INFO 1, odd 1)"]);
	assert.deepEqual(listed.slice(1, 5).map((line) => line.match(/b\.ts:(\d)/)![1]), ["0", "1", "2", "3"], "stable within a level");
});

// L741 (T90/T119): `pi -p` and `--mode json` cannot be woken later: pi's print
// mode returns when session.prompt settles and then exits. The shell keeps the
// run open at agent_before_settle while this session's background work runs,
// and delivers each completion as a follow-up turn, exactly as interactive mode.
async function headless(mode: "print" | "json", extra: Partial<BackgroundTasksDeps> = {}) {
	const warned: string[] = [];
	const h = harness({}, { warn: (line) => { warned.push(line); }, ...extra });
	Object.assign(h.ctx, { mode, isIdle: () => false });
	await h.fire("session_start");
	return { ...h, warned };
}

test("a headless turn that ends with a background sub-agent running waits for it and gets its answer as a follow-up turn", async () => {
	for (const mode of ["print", "json"] as const) {
		const h = await headless(mode);
		h.background!.startAgent(worker, "review", h.ctx.cwd, "main");
		let settled = false;
		const boundary = Promise.resolve(h.fire("agent_before_settle", { type: "agent_before_settle", outcome: "completed" })).then((r) => { settled = true; return r; });
		await settle();
		assert.equal(settled, false, `${mode}: the run does not settle while the sub-agent runs`);
		assert.equal(h.sent.length, 0);
		h.answers[0]!({ ok: true, text: "report ready" });
		await boundary;
		assert.equal(h.sent.length, 1, `${mode}: delivered once`);
		assert.equal(h.sent[0]!.message.customType, TASK_NOTICE_TYPE);
		assert.match(h.sent[0]!.message.content, /report ready/);
		assert.deepEqual(h.sent[0]!.options, { triggerTurn: true, deliverAs: "followUp" });
		// The next settle has nothing pending: the run may end.
		await h.fire("agent_before_settle", { type: "agent_before_settle", outcome: "completed" });
		assert.equal(h.sent.length, 1);
		await h.fire("agent_settled", { type: "agent_settled", aborted: false });
		assert.equal(h.sent.length, 1, "not sent again at settle");
		assert.deepEqual(h.warned, []);
	}
});

test("a headless run with two sub-agents delivers each completion as its own turn until nothing is pending", async () => {
	const h = await headless("print");
	h.background!.startAgent(worker, "one", h.ctx.cwd, "main");
	h.background!.startAgent(worker, "two", h.ctx.cwd, "main");
	const first = h.fire("agent_before_settle", { type: "agent_before_settle" });
	h.answers[1]!({ ok: true, text: "second done" });
	await first;
	assert.deepEqual(h.sent.map((s) => s.message.content.includes("second done")), [true]);
	let settled = false;
	const second = Promise.resolve(h.fire("agent_before_settle", { type: "agent_before_settle" })).then(() => { settled = true; });
	await settle();
	assert.equal(settled, false, "still waits for the first");
	h.answers[0]!({ ok: true, text: "first done" });
	await second;
	assert.equal(h.sent.length, 2);
	assert.match(h.sent[1]!.message.content, /first done/);
});

test("a headless run also waits for a background job, not for the owner's own command", async () => {
	const h = await headless("print");
	await h.tools.get("bash_background").execute("call-1", { command: "make build", label: "build" }, undefined, undefined, h.ctx);
	const boundary = h.fire("agent_before_settle", { type: "agent_before_settle" });
	h.shell.runs[0]!.finish(0);
	await boundary;
	assert.equal(h.sent.length, 1);
	assert.match(h.sent[0]!.message.content, /build/);
	h.background!.startOwnerCommand("make test", h.ctx.cwd, "main", "tests (you)", () => {});
	await h.fire("agent_before_settle", { type: "agent_before_settle" });
	assert.equal(h.sent.length, 1, "the owner's command does not hold the run");
});

test("a headless run stops waiting at the cap, with a notice, and the run ends", async () => {
	const h = await headless("print", { headlessWaitMs: 40 });
	h.background!.startAgent(worker, "review", h.ctx.cwd, "main");
	const result = await h.fire("agent_before_settle", { type: "agent_before_settle" });
	assert.equal((result as any)?.continue, undefined, "no continuation is requested");
	assert.equal(h.sent.length, 1);
	assert.deepEqual(h.sent[0]!.options, { triggerTurn: false });
	const notice = "Headless session: stopped waiting after 0s for 1 background task (worker: review); its result is lost.";
	assert.equal(h.sent[0]!.message.content, notice);
	assert.deepEqual(h.warned, [notice]);
});

test("an interactive or RPC session never waits at agent_before_settle: completions wake the next turn", async () => {
	for (const mode of ["tui", "rpc"]) {
		const h = harness();
		Object.assign(h.ctx, { mode, isIdle: () => false });
		await h.fire("session_start");
		h.background!.startAgent(worker, "review", h.ctx.cwd, "main");
		await h.fire("agent_before_settle", { type: "agent_before_settle" });
		assert.equal(h.sent.length, 0, mode);
		h.answers[0]!({ ok: true, text: "report ready" });
		await settle();
		assert.equal(h.sent.length, 0, `${mode}: held until the turn settles`);
		(h.ctx as any).isIdle = () => true;
		await h.fire("agent_settled", { type: "agent_settled", aborted: false });
		assert.equal(h.sent.length, 1);
		assert.deepEqual(h.sent[0]!.options, { triggerTurn: true, deliverAs: "followUp" });
	}
});

// T108 (L671): `jhon executions run … | tail -25` exited 0 for a failed run.
// The job tells the run where to name its record (JHON_EXECUTIONS_REPORT), and
// its notice reads the record, not the pipeline's exit, which is the last command's.
async function executionsJob(record: unknown) {
	const h = harness();
	await h.fire("session_start");
	await h.run("bash_background", { command: "jhon executions run c1 | tail -5", label: "tests" });
	const command = h.shell.runs[0]!.command;
	const report = /^export JHON_EXECUTIONS_REPORT='([^']+)'\n/.exec(command)?.[1];
	assert.ok(report, `the job names a report file: ${command}`);
	assert.ok(command.endsWith("\njhon executions run c1 | tail -5"), "the owner's command runs as written");
	const recordPath = join(h.ctx.cwd, "execution-record.json");
	writeFileSync(recordPath, JSON.stringify(record));
	writeFileSync(report!, `${recordPath}\n`);
	h.shell.runs[0]!.output("pass  a (format)\n");
	h.shell.runs[0]!.finish(0);
	await until(() => h.sent.length > 0);
	return { h, recordPath, notice: h.sent.at(-1)! };
}

test("a piped jhon executions run whose record failed is reported failed, though the pipeline exited 0", async () => {
	const { h, recordPath, notice } = await executionsJob({ complete: true, executions: [{ project: "a", status: "fail" }, { project: "a", kind: "lint", status: "pass" }] });
	assert.equal(notice.message.details.jhonTasks.succeeded, false);
	assert.match(notice.message.content, new RegExp(`exited with code 0, but jhon executions recorded a failure \\(1 of 2 failed; record ${recordPath.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\)`));
	const result = await h.run("task_result", { id: "job-1" });
	assert.equal(result.details.jhonTasks.succeeded, false);
});

test("a piped jhon executions run whose record passed is reported as before", async () => {
	const { notice } = await executionsJob({ complete: true, executions: [{ project: "a", status: "pass" }] });
	assert.equal(notice.message.details.jhonTasks.succeeded, true);
	assert.match(notice.message.content, /exited with code 0 after/);
});

test("a command that runs no jhon executions is started unchanged and judged by its exit code", async () => {
	const h = harness();
	await h.fire("session_start");
	await h.run("bash_background", { command: "go test ./... | tail -5" });
	assert.equal(h.shell.runs[0]!.command, "go test ./... | tail -5");
	h.shell.runs[0]!.finish(0);
	await until(() => h.sent.length > 0);
	assert.equal(h.sent.at(-1)!.message.details.jhonTasks.succeeded, true);
});
