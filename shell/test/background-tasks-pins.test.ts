/**
 * T174-S11/S12 (L1493): the background tasks' behaviours no other test
 * pinned, written before the rewrite and run on the code it replaced
 * (characterization): the owner's stop reported to the agent, `o` opening
 * a sub-agent's saved session in $EDITOR, the footer status, the running
 * tasks event, notices of another session, the headless wait setting,
 * session end, the registry's small rules and the output's location.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test, { after } from "node:test";
import { activityLine, applyThreadEvent, createTaskRegistry, executionsResult, runsExecutions, type AgentRun, type JobRecord, type TaskRecord, type ThreadItem } from "../lib/background-tasks.ts";
import { formatTaskDuration, registerBackgroundTasks } from "../lib/background-tasks-tools.ts";
import { isStandaloneSleep, type JobExecOperations } from "../lib/background-tasks.ts";
import { TASKS_EVENT } from "../lib/events.ts";
import type { AgentDef, ChildResult } from "../lib/agents.ts";
import { session } from "./background-tasks-capture.ts";

const dirs: string[] = [];
const scratch = (prefix: string) => { const d = mkdtempSync(join(tmpdir(), prefix)); dirs.push(d); return d; };
after(() => { for (const d of dirs) rmSync(d, { recursive: true, force: true }); });
// T194 (L1608): every notice the model reads opens with the shell's mark.
const MARK = "[From the Jhon shell, not the owner; it does not set the reply language]";
const tick = () => new Promise((resolve) => setTimeout(resolve, 10));
async function until(check: () => boolean): Promise<void> {
	for (const deadline = Date.now() + 3000; !check(); await tick()) if (Date.now() > deadline) throw new Error("timed out waiting");
}
const worker: AgentDef = { name: "worker", description: "w", model: "m", tools: [], bounded: true, body: "" };
const plain = { fg: (_r: string, t: string) => t, bg: (_r: string, t: string) => t, bold: (t: string) => t };

/** Opens a view in a fake TUI, presses `keys`, and closes it; what the TUI was asked. */
async function inView(s: ReturnType<typeof session>, view: "agents" | "jobs", keys: string[], during?: () => void) {
	const asked: string[] = [];
	let done!: () => void;
	const closed = new Promise<void>((resolve) => { done = resolve; });
	s.ui.custom = (factory: any, options: unknown) => {
		asked.push(`custom ${JSON.stringify(options)}`);
		const tui = { terminal: { rows: 30 }, requestRender: (force?: boolean) => { asked.push(force ? "render!" : "render"); }, stop: () => { asked.push("stop"); }, start: () => { asked.push("start"); } };
		const v = factory(tui, plain, undefined, () => done());
		v.render(120);
		during?.();
		for (const key of keys) v.handleInput(key);
		v.handleInput("q");
		return closed;
	};
	await s.commands.get(`jhon:${view}`).handler("", s.ctx);
	return asked;
}

// --- The owner's stop (the views' x), reported to the agent ---

test("a job the owner stops in the Jobs view is reported to the agent as stopped by the owner, with its last output", async () => {
	const s = session();
	try {
		await s.fire("session_start");
		await s.tools.get("bash_background").execute("c", { command: "make serve", label: "serve" }, undefined, undefined, s.ctx);
		s.execs[0]!.output("ready\n");
		await inView(s, "jobs", ["x"]);
		await until(() => s.sent.length === 1);
		assert.equal(s.sent[0]!.message.content, `${MARK} Background job job-1 ("serve") was stopped by the owner after 0s.\nCommand: make serve\nLast output:\nready\nFull output: ${s.out}/job-1.log (use the read tool on it for more).`);
		assert.deepEqual(s.sent[0]!.options, { triggerTurn: true, deliverAs: "followUp" });
		assert.equal(s.sent[0]!.message.details.jhonTasks.status, "stopped");
		await tick();
		assert.equal(s.sent.length, 1, "reported once: the stop settles nothing else");
	} finally { s.cleanup(); }
});

test("a monitor the owner stops delivers its pending lines first, then the stop; a sub-agent the owner stops is reported too", async () => {
	const s = session();
	try {
		await s.fire("session_start");
		await s.tools.get("monitor").execute("c", { command: "tail -f x", timeout_seconds: 60, label: "watch" }, undefined, undefined, s.ctx);
		s.execs[0]!.output("pending line\n");
		await inView(s, "jobs", ["x"]);
		await until(() => s.sent.length === 2);
		assert.equal(s.sent[0]!.message.content, `${MARK} Monitor job-1 ("watch") reported 1 new line:\npending line`);
		assert.ok(s.sent[1]!.message.content.startsWith(`${MARK} `));
		assert.match(s.sent[1]!.message.content, /Monitor job-1 \("watch"\) was stopped by the owner after 0s, 1 event\./);
		s.advance(120_000);
		await tick();
		assert.equal(s.sent.length, 2, "its timeout is gone");
		const run = s.background!.startAgent(worker, "review", s.out, "main");
		await inView(s, "agents", ["x"]);
		await until(() => s.sent.length === 3);
		assert.equal(s.sent[2]!.message.content, `${MARK} Background sub-agent ${run.id} (worker) was stopped by the owner after 0s.\n\njhon: the sub-agent "worker" was stopped`);
		assert.equal(s.agents[0]!.task, "review");
	} finally { s.cleanup(); }
});

test("a stop by the owner held mid-turn waits for the turn to settle, as every notice does", async () => {
	const s = session();
	try {
		let idle = false;
		s.ctx.isIdle = () => idle;
		await s.fire("session_start");
		await s.tools.get("bash_background").execute("c", { command: "make serve" }, undefined, undefined, s.ctx);
		await inView(s, "jobs", ["x"]);
		await tick();
		assert.equal(s.sent.length, 0);
		idle = true;
		await s.fire("agent_settled", { aborted: false });
		assert.equal(s.sent.length, 1);
		assert.match(s.sent[0]!.message.content, /was stopped by the owner/);
	} finally { s.cleanup(); }
});

// --- o: the sub-agent's saved session, in $EDITOR ---

function editor(t: { after(fn: () => void): void }, code = 0) {
	const dir = scratch("jhon-editor-");
	const opened = join(dir, "opened");
	const script = join(dir, "ed.sh");
	writeFileSync(script, `#!/bin/sh\nprintf '%s\\n' "$1" >> '${opened}'\nexit ${code}\n`);
	chmodSync(script, 0o755);
	const before = { VISUAL: process.env.VISUAL, EDITOR: process.env.EDITOR };
	process.env.VISUAL = script;
	delete process.env.EDITOR;
	t.after(() => { for (const [k, v] of Object.entries(before)) { if (v === undefined) delete process.env[k]; else process.env[k] = v; } });
	return { script, opened: () => existsSync(opened) ? readFileSync(opened, "utf8").split("\n").filter(Boolean) : [] };
}
const childSession = (dir: string, id: string) => ({ dir, id, meta: { agentType: "worker", toolUseId: id, parentSession: "main" } });

test("o in the Agents view opens the sub-agent's saved session (<timestamp>_<id>.jsonl or <id>.jsonl) in $VISUAL, with the TUI stopped around it", { skip: process.platform === "win32" }, async (t) => {
	const ed = editor(t);
	const s = session();
	try {
		await s.fire("session_start");
		const dir = join(s.out, "subagents");
		mkdirSync(dir);
		writeFileSync(join(dir, "2026-11-20T10-00-00_call-a.jsonl"), "{}\n");
		writeFileSync(join(dir, "other_call-b.jsonl.bak"), "");
		s.background!.startAgent(worker, "a", s.out, "main", { session: childSession(dir, "call-a") });
		const asked = await inView(s, "agents", ["o"]);
		assert.deepEqual(ed.opened(), [join(dir, "2026-11-20T10-00-00_call-a.jsonl")]);
		assert.deepEqual(asked.filter((a) => a === "stop" || a === "start"), ["stop", "start"]);
		assert.deepEqual(s.notes, [], "no warning when the editor closed cleanly");
		s.agents[0]!.answer({ ok: true, text: "done" });
		await until(() => s.sent.length === 1);
		writeFileSync(join(dir, "call-b.jsonl"), "{}\n");
		s.background!.startAgent(worker, "b", s.out, "main", { session: childSession(dir, "call-b") });
		// The finished agent-1 comes after the running agent-2: o on the first row opens agent-2's.
		await inView(s, "agents", ["o"]);
		assert.deepEqual(ed.opened().at(-1), join(dir, "call-b.jsonl"));
	} finally { s.cleanup(); }
});

test("o warns when the session file is missing or the editor fails, and does nothing for an unsaved sub-agent or in the Jobs view", { skip: process.platform === "win32" }, async (t) => {
	const ed = editor(t, 3);
	const s = session();
	try {
		await s.fire("session_start");
		const dir = join(s.out, "subagents");
		mkdirSync(dir);
		const run = s.background!.startAgent(worker, "a", s.out, "main", { session: childSession(dir, "call-x") });
		await inView(s, "agents", ["o"]);
		assert.deepEqual(s.notes, [["warning", `worker ${run.id} has no session file yet.`]]);
		writeFileSync(join(dir, "call-x.jsonl"), "{}\n");
		await inView(s, "agents", ["o"]);
		assert.deepEqual(s.notes.at(-1), ["warning", `The editor (${ed.script}) ended with code 3.`]);
		await s.tools.get("bash_background").execute("c", { command: "make" }, undefined, undefined, s.ctx);
		await inView(s, "jobs", ["o"]);
		assert.equal(s.notes.length, 2, "the Jobs view has no o");
		const unsaved = session();
		try {
			await unsaved.fire("session_start");
			unsaved.background!.startAgent(worker, "a", unsaved.out, "main");
			await inView(unsaved, "agents", ["o"]);
			assert.deepEqual(unsaved.notes, [], "an unsaved sub-agent has nothing to open");
		} finally { unsaved.cleanup(); }
		delete process.env.VISUAL;
		await inView(s, "agents", ["o"]);
		assert.deepEqual(s.notes.at(-1), ["warning", "No editor: set $VISUAL or $EDITOR."]);
	} finally { s.cleanup(); }
});

test("the views open as a full-screen overlay, repaint as a sub-agent works, and force a repaint when closed", async () => {
	const s = session();
	try {
		await s.fire("session_start");
		s.background!.startAgent(worker, "a", s.out, "main");
		const asked = await inView(s, "agents", [], () => s.agents[0]!.emit({ type: "tool_execution_start", toolCallId: "c", toolName: "read", args: { path: "a.ts" } }));
		assert.equal(asked[0], `custom ${JSON.stringify({ overlay: true, overlayOptions: { width: "100%", maxHeight: "100%", margin: 0, anchor: "center" } })}`);
		assert.ok(asked.includes("render"), "the sub-agent's step repaints the open view");
		assert.equal(asked.at(-1), "render!", "closing forces a full repaint");
		const before = asked.length;
		s.agents[0]!.emit({ type: "tool_execution_start", toolCallId: "d", toolName: "read", args: { path: "b.ts" } });
		assert.equal(asked.length, before, "a closed view is not repainted");
	} finally { s.cleanup(); }
});

// --- The footer status and the running tasks event ---

test("with the shell off, pi's status line counts the running tasks under jhon-tasks; with it on, nothing is set", async () => {
	const off = session({ env: { JHON_SHELL: "off" } });
	try {
		await off.fire("session_start");
		await off.tools.get("bash_background").execute("c", { command: "a" }, undefined, undefined, off.ctx);
		off.background!.startAgent(worker, "b", off.out, "main");
		assert.deepEqual(off.statuses.at(-1), ["jhon-tasks", "⧗ 2 running"]);
		off.execs[0]!.finish(0);
		off.agents[0]!.answer({ ok: true, text: "x" });
		await until(() => off.sent.length === 2);
		assert.deepEqual(off.statuses.at(-1), ["jhon-tasks", undefined]);
	} finally { off.cleanup(); }
	const on = session();
	try {
		await on.fire("session_start");
		await on.tools.get("bash_background").execute("c", { command: "a" }, undefined, undefined, on.ctx);
		assert.deepEqual(on.statuses, []);
	} finally { on.cleanup(); }
});

test("the running tasks event: newest first with an agent's latest step and tokens; the three latest finished of each kind, for a minute", async () => {
	const s = session();
	try {
		await s.fire("session_start");
		const last = () => s.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)!.value;
		for (let i = 0; i < 5; i++) {
			await s.tools.get("bash_background").execute("c", { command: `c${i}` }, undefined, undefined, s.ctx);
			s.advance(1_000);
		}
		for (let i = 0; i < 5; i++) { s.background!.startAgent(worker, `t${i}`, s.out, "main"); s.advance(1_000); }
		s.agents[4]!.emit({ type: "tool_execution_start", toolCallId: "c", toolName: "read", args: { path: "a.ts" } });
		s.agents[4]!.emit({ type: "message_end", message: { role: "assistant", content: [], usage: { totalTokens: 42 } } });
		assert.deepEqual(last().tasks.map((t: any) => t.id), ["agent-5", "agent-4", "agent-3", "agent-2", "agent-1", "job-5", "job-4", "job-3", "job-2", "job-1"]);
		assert.deepEqual(last().tasks[0], { id: "agent-5", kind: "agent", label: "worker: t4", startedAt: s.emitted.length && last().tasks[0].startedAt, step: "read a.ts", tokens: 42 });
		assert.deepEqual(Object.keys(last().tasks.at(-1)), ["id", "kind", "label", "startedAt"]);
		for (let i = 0; i < 5; i++) { s.execs[i]!.finish(i); await until(() => s.sent.length === i + 1); s.advance(10); }
		for (let i = 0; i < 5; i++) { s.agents[i]!.answer({ ok: i % 2 === 0, text: "x" }); await tick(); s.advance(10); }
		await until(() => s.sent.length === 10);
		assert.deepEqual(last().tasks, []);
		assert.deepEqual(last().finished.map((t: any) => [t.id, t.status, t.exitCode]), [["job-5", "exited", 4], ["job-4", "exited", 3], ["job-3", "exited", 2], ["agent-5", "completed", null], ["agent-4", "failed", null], ["agent-3", "completed", null]]);
		// agent-4 ended 10 ms before agent-5, the last to end, 10 ms ago.
		s.advance(59_985);
		s.background!.startAgent(worker, "late", s.out, "main");
		assert.deepEqual(last().finished.map((t: any) => t.id), ["agent-5"], "a minute after its end a task leaves the event");
		assert.equal(last().sessionId, "main");
	} finally { s.cleanup(); }
});

// --- Sessions, the headless wait, session end ---

test("a task another session owns sends this session no notice", async () => {
	const s = session();
	try {
		await s.fire("session_start");
		s.background!.startAgent(worker, "theirs", s.out, "other");
		s.agents[0]!.answer({ ok: true, text: "x" });
		await tick(); await tick();
		assert.deepEqual(s.sent, []);
		assert.equal(s.background!.agentRun("agent-1", "main"), undefined, "nor can this session read it");
		assert.equal(s.background!.agentRun("agent-1", "other")?.status, "completed");
	} finally { s.cleanup(); }
});

test("JHON_HEADLESS_WAIT_SECONDS sets how long a headless run waits for its background work", async () => {
	const warned: string[] = [];
	const s = session({ mode: "json", env: { JHON_HEADLESS_WAIT_SECONDS: "0.05" }, warn: (line) => { warned.push(line); } });
	try {
		s.ctx.isIdle = () => false;
		await s.fire("session_start");
		s.background!.startAgent(worker, "slow", s.out, "main");
		const started = Date.now();
		await s.fire("agent_before_settle", {});
		assert.ok(Date.now() - started < 2_000, "it did not wait an hour");
		assert.deepEqual(warned, ["Headless session: stopped waiting after 0s for 1 background task (worker: slow); its result is lost."]);
	} finally { s.cleanup(); }
});

test("session end drops the held notices and every monitor's timer, stops every task and reports none", async () => {
	const s = session();
	try {
		let idle = false;
		s.ctx.isIdle = () => idle;
		await s.fire("session_start");
		await s.tools.get("bash_background").execute("c", { command: "a" }, undefined, undefined, s.ctx);
		s.execs[0]!.finish(0);
		await until(() => s.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)!.value.tasks.length === 0);
		await tick();
		await s.tools.get("monitor").execute("c", { command: "tail -f x", timeout_seconds: 5 }, undefined, undefined, s.ctx);
		s.execs[1]!.output("line\n");
		const run = s.background!.startAgent(worker, "b", s.out, "main");
		await s.fire("session_shutdown");
		idle = true;
		s.advance(10_000);
		await s.fire("agent_settled", { aborted: false });
		await tick();
		assert.deepEqual(s.sent, [], "the held notice, the monitor's line and its timeout are dropped");
		assert.equal(run.status, "stopped");
		assert.deepEqual(s.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)!.value.tasks, []);
	} finally { s.cleanup(); }
});

test("stopOwnerCommand ignores an agent's task and an unknown id", async () => {
	const s = session();
	try {
		await s.fire("session_start");
		await s.tools.get("bash_background").execute("c", { command: "a" }, undefined, undefined, s.ctx);
		const run = s.background!.startAgent(worker, "b", s.out, "main");
		s.background!.stopOwnerCommand("job-1");
		s.background!.stopOwnerCommand(run.id);
		s.background!.stopOwnerCommand("job-404");
		assert.equal(run.status, "running");
		assert.equal((await s.tools.get("task_result").execute("c", { id: "job-1" }, undefined, undefined, s.ctx)).details.jhonTasks.status, "running");
	} finally { s.cleanup(); }
});

test("without an output directory, each job's log is <id>.log in one jhon-tasks- directory under the system temp directory", async (t) => {
	const tmp = scratch("jhon-tmp-");
	const before = process.env.TMPDIR;
	process.env.TMPDIR = tmp;
	t.after(() => { if (before === undefined) delete process.env.TMPDIR; else process.env.TMPDIR = before; });
	const pi = { registerTool: (tool: any) => tools.set(tool.name, tool), registerCommand: () => {}, registerShortcut: () => {}, registerMessageRenderer: () => {}, on: () => {}, sendMessage: () => {}, events: { emit: () => {}, on: () => () => {} } };
	const tools = new Map<string, any>();
	const operations: JobExecOperations = { exec: () => new Promise(() => {}) };
	registerBackgroundTasks(pi as any, { env: {}, shell: () => ({ operations }) });
	const ctx = { cwd: tmp, sessionManager: { getSessionId: () => "main" } };
	const first = (await tools.get("bash_background").execute("c", { command: "a" }, undefined, undefined, ctx)).details.jhonTasks.outputPath as string;
	const second = (await tools.get("bash_background").execute("c", { command: "b" }, undefined, undefined, ctx)).details.jhonTasks.outputPath as string;
	assert.match(first, new RegExp(`^${tmp}/jhon-tasks-[^/]+/job-1\\.log$`));
	assert.equal(second, first.replace("job-1.log", "job-2.log"));
});

// --- The registry's small rules ---

function registry(extra: { outputDir?: () => string; commandPrefix?: string } = {}) {
	const dir = scratch("jhon-reg-");
	const runs: Array<{ command: string; data(text: string): void; exit(code: number): void }> = [];
	const agents: Array<{ emit(e: any): void; answer(r: ChildResult): void; fail(e: Error): void }> = [];
	const settled: TaskRecord[] = [];
	const reg = createTaskRegistry({
		outputDir: extra.outputDir ?? (() => dir),
		now: () => 5,
		shell: () => ({ operations: { exec: (command, _cwd, o) => new Promise((resolve, reject) => {
			runs.push({ command, data: (text) => o.onData(Buffer.from(text)), exit: (exitCode) => resolve({ exitCode }) });
			o.signal?.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
		}) }, commandPrefix: extra.commandPrefix }),
		runAgent: (_a, _t, _c, o) => new Promise((resolve, reject) => { agents.push({ emit: o.onEvent, answer: resolve, fail: reject }); }),
		onSettled: (task) => settled.push(task),
	});
	return { reg, runs, agents, settled, dir };
}

test("a label is trimmed, a blank one is the command; an agent's label is its name and the task's first line, cut at the rail's 40 (T195)", () => {
	const { reg } = registry();
	assert.equal(reg.start({ command: "make", cwd: "/", ownerSessionId: "s", label: "  build  " }).label, "build");
	assert.equal(reg.start({ command: "make", cwd: "/", ownerSessionId: "s", label: " " }).label, "make");
	assert.equal(reg.startAgent(worker, `  ${"a".repeat(70)}\nsecond`, "/", "s").label, `worker: ${"a".repeat(39)}…`);
	assert.equal(reg.startAgent(worker, "fix  the\tbug\nthen more", "/", "s").label, "worker: fix the bug");
});

test("a sub-agent that cannot start fails with why; events after a stop change nothing; its cost adds up", async () => {
	const { reg, agents, settled } = registry();
	const crashed = reg.startAgent(worker, "a", "/", "s");
	agents[0]!.fail(new Error("spawn pi ENOENT"));
	await tick();
	assert.equal(crashed.status, "failed");
	assert.equal(crashed.result, 'jhon: the sub-agent "worker" could not run: spawn pi ENOENT');
	assert.deepEqual(settled.map((t) => t.id), [crashed.id]);
	const run = reg.startAgent(worker, "b", "/", "s");
	const usage = (total: number, cost: number) => ({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "hi" }], usage: { totalTokens: total, cost: { total: cost } } } });
	agents[1]!.emit(usage(10, 0.5));
	agents[1]!.emit(usage(5, 0.25));
	agents[1]!.emit({ type: "message_end", message: { role: "assistant", content: [], usage: { cost: { total: -1 } } } });
	assert.equal(run.tokens, 15);
	assert.equal(run.cost, 0.75);
	reg.stop(run.id);
	agents[1]!.emit(usage(99, 9));
	agents[1]!.emit({ type: "tool_execution_start", toolCallId: "c", toolName: "read", args: { path: "x" } });
	assert.equal(run.tokens, 15);
	assert.equal(run.cost, 0.75);
	assert.deepEqual(run.activity, ["» hi", "» hi"]);
	assert.equal(run.thread.length, 2);
	agents[1]!.answer({ ok: true, text: "late" });
	await tick();
	assert.equal(run.status, "stopped");
	assert.equal(run.result, 'jhon: the sub-agent "worker" was stopped');
	assert.deepEqual(settled.map((t) => t.id), [crashed.id], "a stopped run never settles");
});

test("an activity line is one line cut at 100 characters; a thread keeps 200 items and each tool's last 4,000 characters", () => {
	const long = "word ".repeat(40);
	assert.equal(activityLine({ type: "tool_execution_start", toolName: "bash", args: { command: long } }), `bash ${"word ".repeat(40)}`.replace(/\s+/g, " ").trim().slice(0, 99) + "…");
	assert.equal(activityLine({ type: "tool_execution_start", args: {} }), "tool");
	assert.equal(activityLine({ type: "tool_execution_end", toolName: "edit", isError: false }), undefined);
	assert.equal(activityLine({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "  " }] } }), undefined);
	assert.equal(activityLine({ type: "message_end", message: { role: "user", content: [{ type: "text", text: "hi" }] } }), undefined);
	const thread: ThreadItem[] = [];
	for (let i = 0; i < 205; i++) applyThreadEvent(thread, { type: "message_end", message: { role: "assistant", content: [{ type: "text", text: `t${i}` }, { type: "thinking", thinking: " " }] } });
	assert.equal(thread.length, 200);
	assert.deepEqual(thread[0], { kind: "text", text: "t5" });
	applyThreadEvent(thread, { type: "tool_execution_start", toolCallId: "c", toolName: "bash", args: { command: "x" } });
	applyThreadEvent(thread, { type: "tool_execution_end", toolCallId: "c", toolName: "bash", result: { content: [{ type: "text", text: `head${"y".repeat(5000)}` }, { type: "image" }] } });
	const tool = thread.at(-1) as Extract<ThreadItem, { kind: "tool" }>;
	assert.equal(tool.output, `…${"y".repeat(4000)}`);
	applyThreadEvent(thread, { type: "tool_execution_end", toolCallId: "d", toolName: "read", result: { content: "plain string" } });
	assert.equal((thread.at(-1) as any).output, "plain string");
});

test("the shell's prefix runs first, then the executions report's export, then the command as written", () => {
	const { reg, runs } = registry({ commandPrefix: "shopt -s expand_aliases" });
	const job = reg.start({ command: "jhon  executions   run c1", cwd: "/", ownerSessionId: "s" });
	assert.equal(runs[0]!.command, `shopt -s expand_aliases\nexport JHON_EXECUTIONS_REPORT='${job.outputPath}.executions'\njhon  executions   run c1`);
	assert.equal(job.command, "jhon  executions   run c1");
	for (const [command, runs_] of [["jhon executions run x", true], ["./jhon executions run", true], ["jhonx executions run", false], ["jhon executions list", false], ["echo jhon executions running", false]] as const) {
		assert.equal(runsExecutions(command), runs_, command);
	}
});

test("a quote in the report's path is escaped for the shell", () => {
	const dir = scratch("it's-");
	const { reg, runs } = registry({ outputDir: () => dir });
	const job = reg.start({ command: "jhon executions run", cwd: "/", ownerSessionId: "s" });
	assert.equal(runs[0]!.command.split("\n")[0], `export JHON_EXECUTIONS_REPORT='${job.outputPath.replace(/'/g, "'\\''")}.executions'`);
});

test("the executions result: records named once each; an unreadable or incomplete record, or a non-pass, fails it; none named is no result", () => {
	const dir = scratch("jhon-exec-");
	const write = (name: string, body: unknown) => { const p = join(dir, name); writeFileSync(p, typeof body === "string" ? body : JSON.stringify(body)); return p; };
	const pass = write("pass.json", { complete: true, executions: [{ status: "pass" }, { status: "pass" }] });
	const fail = write("fail.json", { complete: true, executions: [{ status: "fail" }, { status: "pass" }, {}] });
	const partial = write("partial.json", { complete: false, executions: [{ status: "pass" }] });
	const broken = write("broken.json", "{");
	const noList = write("nolist.json", { complete: true });
	assert.deepEqual(executionsResult(write("r1", `${pass}\n ${pass} \n\n`)), { records: [pass], failed: false, failures: 0, total: 2 });
	assert.deepEqual(executionsResult(write("r2", `${pass}\n${fail}\n`)), { records: [pass, fail], failed: true, failures: 2, total: 5 });
	assert.deepEqual(executionsResult(write("r3", partial)), { records: [partial], failed: true, failures: 0, total: 1 });
	assert.deepEqual(executionsResult(write("r4", `${broken}\n${join(dir, "missing.json")}`)), { records: [broken, join(dir, "missing.json")], failed: true, failures: 0, total: 0 });
	assert.deepEqual(executionsResult(write("r5", noList)), { records: [noList], failed: false, failures: 0, total: 0 });
	assert.equal(executionsResult(write("r6", "\n  \n")), undefined);
	assert.equal(executionsResult(join(dir, "no-report")), undefined);
});

test("a job whose log cannot be written still runs, keeps its tail and settles", async () => {
	const { reg, runs, settled } = registry({ outputDir: () => "/nonexistent-jhon-dir" });
	const job = reg.start({ command: "x", cwd: "/", ownerSessionId: "s" });
	assert.equal(job.outputPath, "/nonexistent-jhon-dir/job-1.log");
	runs[0]!.data("one\n");
	runs[0]!.exit(0);
	await until(() => settled.length === 1);
	assert.deepEqual((settled[0] as JobRecord).tail, ["one"]);
	assert.equal(settled[0]!.status, "exited");
});

test("only a command of literal sleeps is a standalone sleep", () => {
	for (const command of ["sleep 1", "sleep 0.5", "sleep 2m", "sleep 1h && sleep 1d", "sleep 1; sleep 2s;", "  sleep 3  "]) assert.equal(isStandaloneSleep(command), true, command);
	for (const command of ["", ";", "sleep", "sleep x", "sleep 1 | cat", "sleep 1 && ls", "until x; do sleep 1; done", "sleep 1 || true", "sleep 1w", "sleep $N", "sleep 1 &"]) assert.equal(isStandaloneSleep(command), false, command);
});

test("a duration reads in seconds, minutes and seconds, or hours and minutes", () => {
	assert.deepEqual([-5, 0, 499, 500, 59_499, 59_500, 60_000, 3_599_000, 3_600_000, 3_660_000, 90_000_000].map(formatTaskDuration),
		["0s", "0s", "0s", "1s", "59s", "1m 0s", "1m 0s", "59m 59s", "1h 0m", "1h 1m", "25h 0m"]);
});

test("a sub-agent keeps its model, saved session and the options it was started with", () => {
	const seen: unknown[] = [];
	const reg = createTaskRegistry({ outputDir: () => "/", now: () => 1, shell: () => { throw new Error("none"); }, onSettled: () => {},
		runAgent: (_a, _t, _c, o) => { seen.push({ ...o, signal: typeof o.signal, onEvent: typeof o.onEvent }); return new Promise(() => {}); } });
	const session = childSession("/s/subagents", "call-1");
	const run = reg.startAgent(worker, "t", "/repo", "main", { session, scope: "the scope", timeoutSeconds: 60, skills: ["a"] } as any) as AgentRun;
	assert.deepEqual([run.model, run.sessionDir, run.childSessionId, run.ownerSessionId, run.startedAt], ["m", "/s/subagents", "call-1", "main", 1]);
	assert.deepEqual(seen, [{ session, scope: "the scope", timeoutSeconds: 60, skills: ["a"], signal: "object", onEvent: "function" }]);
	const unsaved = reg.startAgent(worker, "t", "/repo", "main");
	assert.equal("sessionDir" in unsaved, false);
	assert.equal(unsaved.childSessionId, undefined);
});
