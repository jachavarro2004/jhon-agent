/**
 * Everything the background tasks show the model and the owner, captured by
 * one scripted session: the registered tools (names, labels, descriptions,
 * schemas), the commands and keys, every tool result, error and notice, and
 * the notice cards as the owner sees them. background-tasks-model-text.test.ts
 * compares it with the snapshot taken before the T174-S11/S12 rewrite, so
 * the rewrite changes no word the model reads.
 */
import "./isolated-env.ts";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { registerBackgroundTasks, type BackgroundTasksDeps } from "../lib/background-tasks-tools.ts";
import type { AgentDef, ChildEvent, ChildResult, RunOptions } from "../lib/agents.ts";
import { memorySettings } from "./fake-jhon-settings.ts";

type Handler = (event: any, ctx: any) => unknown;
type Exec = { command: string; output(text: string): void; finish(code: number | null): void; fail(error: Error): void };
type AgentCall = { task: string; emit(event: ChildEvent): void; answer(r: ChildResult): void; fail(error: Error): void };

const plain = { fg: (_role: string, text: string) => text, bg: (_role: string, text: string) => text, bold: (text: string) => text };
const tick = () => new Promise((resolve) => setTimeout(resolve, 5));
async function until(check: () => boolean): Promise<void> {
	for (const deadline = Date.now() + 3000; !check(); await tick()) if (Date.now() > deadline) throw new Error("capture: timed out waiting");
}

let sessions = 0;
/**
 * The session's output directory: a fixed absolute path of fixed length,
 * never under tmpdir(). The notice cards wrap the full path before <out>
 * replaces it, so a path whose length followed TMPDIR changed the rows.
 */
function outputDirectory(): string {
	const out = `/tmp/jhon-capture-snapshot-${String(process.pid).padStart(8, "0")}-${String(++sessions % 100).padStart(2, "0")}`;
	rmSync(out, { recursive: true, force: true });
	mkdirSync(out, { recursive: true });
	return out;
}

/** One session of the background tasks, with a fake shell, sub-agents, clock and timers. */
export function session(extra: Partial<BackgroundTasksDeps> & { mode?: string; env?: NodeJS.ProcessEnv } = {}) {
	const out = outputDirectory();
	const tools = new Map<string, any>();
	const commands = new Map<string, any>();
	const shortcuts = new Map<string, any>();
	const renderers = new Map<string, any>();
	const handlers = new Map<string, Handler[]>();
	const listeners = new Map<string, (value: any) => void>();
	const sent: Array<{ message: any; options: any }> = [];
	const emitted: Array<{ name: string; value: any }> = [];
	const pi = {
		appendEntry: () => {},
		registerTool: (tool: any) => tools.set(tool.name, tool),
		registerCommand: (name: string, command: any) => commands.set(name, command),
		registerShortcut: (key: string, shortcut: any) => shortcuts.set(key, shortcut),
		registerMessageRenderer: (type: string, render: any) => renderers.set(type, render),
		on: (name: string, handler: Handler) => handlers.set(name, [...(handlers.get(name) ?? []), handler]),
		sendMessage: (message: any, options: any) => { sent.push({ message, options }); },
		events: { emit: (name: string, value: any) => emitted.push({ name, value }), on: (name: string, fn: (v: any) => void) => { listeners.set(name, fn); return () => listeners.delete(name); } },
	};
	const execs: Exec[] = [];
	const agents: AgentCall[] = [];
	let clock = 1_000_000;
	const timers: Array<{ at: number; fn: () => void; live: boolean }> = [];
	const statuses: Array<[string, string | undefined]> = [];
	const notes: Array<[string, string]> = [];
	const ui = { setStatus: (key: string, value: string | undefined) => { statuses.push([key, value]); }, notify: (message: string, level: string) => { notes.push([level, message]); }, custom: undefined as any };
	const ctx: any = { cwd: out, hasUI: true, mode: extra.mode ?? "tui", isIdle: () => true, sessionManager: { getSessionId: () => "main" }, ui };
	const background = registerBackgroundTasks(pi as any, {
		env: extra.env ?? {},
		settings: memorySettings({}),
		outputDir: () => out,
		shell: () => ({ operations: { exec: (command, _cwd, options) => new Promise((resolve, reject) => {
			execs.push({ command, output: (text) => options.onData(Buffer.from(text)), finish: (exitCode) => resolve({ exitCode }), fail: reject });
			options.signal?.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
		}) } }),
		runAgent: (_agent: AgentDef, task: string, _cwd: string, options: RunOptions & { signal: AbortSignal; onEvent: (event: ChildEvent) => void }) => new Promise<ChildResult>((resolve, reject) => {
			agents.push({ task, emit: options.onEvent, answer: resolve, fail: reject });
			options.signal.addEventListener("abort", () => resolve({ ok: false, text: "cancelled" }), { once: true });
		}),
		now: () => clock,
		schedule: (fn, ms) => { const timer = { at: clock + ms, fn, live: true }; timers.push(timer); return () => { timer.live = false; }; },
		...extra,
	});
	const advance = (ms: number) => {
		clock += ms;
		for (const timer of timers.filter((t) => t.live && t.at <= clock)) { timer.live = false; timer.fn(); }
	};
	const fire = async (name: string, event: any = {}, context: any = ctx) => {
		let result: unknown;
		for (const handler of handlers.get(name) ?? []) result = (await handler(event, context)) ?? result;
		return result;
	};
	return { out, pi, tools, commands, shortcuts, renderers, handlers, listeners, sent, emitted, execs, agents, ctx, ui, statuses, notes, background, advance, fire, until, cleanup: () => rmSync(out, { recursive: true, force: true }) };
}

const worker: AgentDef = { name: "worker", description: "w", model: "m", tools: [], bounded: true, body: "" };

/** The model- and owner-facing text of the background tasks, with the scratch directory as <out>. */
export async function capture(): Promise<unknown> {
	const s = session();
	const log: unknown[] = [];
	const call = async (name: string, params: unknown) => {
		try {
			const r = await s.tools.get(name).execute("call", params, undefined, undefined, s.ctx);
			log.push({ tool: name, params, result: r });
		} catch (e) { log.push({ tool: name, params, error: (e as Error).message }); }
	};
	let seen = 0;
	const notices = async (count: number) => {
		await until(() => s.sent.length >= seen + count);
		for (const item of s.sent.slice(seen)) log.push({ sent: item });
		seen = s.sent.length;
	};
	try {
		await s.fire("session_start");
		// bash_background: errors, an exit with output, one without, a failure, no exit code.
		await call("bash_background", {});
		await call("bash_background", { command: "  " });
		await call("bash_background", { command: "make build", label: "build" });
		s.execs[0]!.output("compiled\r\nok\n");
		s.advance(4_200);
		s.execs[0]!.finish(0);
		await notices(1);
		await call("bash_background", { command: "false" });
		s.advance(65_000);
		s.execs[1]!.finish(2);
		await notices(1);
		await call("bash_background", { command: "nope", label: "  " });
		s.execs[2]!.fail(new Error("Working directory does not exist: /missing"));
		await notices(1);
		await call("bash_background", { command: "killed" });
		s.advance(3_700_000);
		s.execs[3]!.finish(null);
		await notices(1);
		// task_list, task_result, task_stop.
		await call("bash_background", { command: "npm run dev", label: "dev" });
		s.execs[4]!.output("listening on 3000\npartial");
		await call("task_list", {});
		await call("task_result", { id: "job-5" });
		await call("task_result", { id: "job-1" });
		await call("task_result", { id: "job-404" });
		await call("task_result", {});
		await call("task_stop", { id: "job-5" });
		await call("task_stop", { id: "job-5" });
		await call("task_stop", { id: "job-1" });
		await call("task_stop", { id: "job-3" });
		// monitor: errors, a batch, an overfull batch, the timeout; a flood; an exit with events.
		await call("monitor", { timeout_seconds: 30 });
		for (const timeout of [0, 1801, 1.5, "30"]) await call("monitor", { command: "x", timeout_seconds: timeout });
		await call("monitor", { command: "tail -f app.log | grep --line-buffered ERROR", timeout_seconds: 30, label: "errors" });
		const m1 = s.execs.at(-1)!;
		m1.output("ERROR one\n\nERROR two\n");
		s.advance(200);
		await notices(1);
		m1.output(Array.from({ length: 25 }, (_, i) => `line ${i}`).join("\n") + "\n");
		s.advance(200);
		await notices(1);
		s.advance(30_000);
		await notices(1);
		await call("monitor", { command: "yes", timeout_seconds: 600 });
		s.execs.at(-1)!.output(Array.from({ length: 130 }, (_, i) => `spam ${i}`).join("\n") + "\n");
		await notices(2);
		await call("monitor", { command: "gh run watch 7", timeout_seconds: 1800, label: "CI" });
		s.execs.at(-1)!.output("lint: pass\n");
		s.execs.at(-1)!.output("test: pass");
		s.execs.at(-1)!.finish(0);
		await notices(2);
		await call("task_list", {});
		// Sub-agents: running with activity, completed, failed, a crash, long results, a JSON report.
		const a1 = s.background!.startAgent(worker, "find the callers of parse\nand list them", s.out, "main");
		s.agents[0]!.emit({ type: "tool_execution_start", toolCallId: "c1", toolName: "grep", args: { pattern: "parse(" } });
		s.agents[0]!.emit({ type: "tool_execution_end", toolCallId: "c1", toolName: "grep", isError: true });
		s.agents[0]!.emit({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: `Found ${"x".repeat(150)}\nmore` }], usage: { totalTokens: 10 } } });
		await call("task_result", { id: a1.id });
		await call("task_list", {});
		s.agents[0]!.answer({ ok: true, text: "found 3 callers" });
		await notices(1);
		s.background!.startAgent(worker, "b".repeat(80), s.out, "main");
		s.agents[1]!.answer({ ok: false, text: "jhon: the sub-agent \"worker\" exited 1" });
		await notices(1);
		s.background!.startAgent(worker, "crash", s.out, "main");
		s.agents[2]!.fail(new Error("spawn pi ENOENT"));
		await notices(1);
		s.background!.startAgent(worker, "long", s.out, "main");
		s.agents[3]!.answer({ ok: true, text: Array.from({ length: 120 }, (_, i) => `row ${i} ${"z".repeat(20)}`).join("\n") });
		await notices(1);
		s.background!.startAgent(worker, "unbroken", s.out, "main");
		s.agents[4]!.answer({ ok: true, text: "w".repeat(3000) });
		await notices(1);
		s.background!.startAgent(worker, "review", s.out, "main");
		const findings = [
			...Array.from({ length: 6 }, (_, i) => ({ severity: "WARNING", location: `a.ts:${i}`, claim: `warn ${i} ${"q".repeat(200)}` })),
			{ severity: "CRITICAL", location: "b.ts:1", message: "critical one" },
			{ severity: "info", claim: "lower case" },
			{ severity: "odd" },
			{ location: "no severity" },
		];
		s.agents[5]!.answer({ ok: true, text: "```json\n" + JSON.stringify({ facet: "risk", ok: false, count: 3, none: null, inspection: { status: "completed", paths: ["a"] }, findings, tags: ["x", "y"], notes: "n".repeat(2000) }, null, 2) + "\n```" });
		await notices(1);
		await call("task_result", { id: "agent-6" });
		await call("task_stop", { id: "agent-1" });
		const a7 = s.background!.startAgent(worker, "stop me", s.out, "main");
		await call("task_stop", { id: a7.id });
		await call("task_result", { id: a7.id });
		// The foreground waits: the standalone sleep while work runs, a long sleep, a known wait.
		const a8 = s.background!.startAgent(worker, "keep running", s.out, "main");
		for (const [id, command] of [["w1", "sleep 3"], ["w2", "sleep 420"], ["w3", "gh run watch 9"], ["w4", "npm test"]]) {
			await s.fire("tool_call", { toolName: "bash", toolCallId: id, input: { command } });
			log.push({ wait: command, result: await s.fire("tool_result", { toolName: "bash", toolCallId: id, input: { command }, content: [{ type: "text", text: "out" }], isError: false }) });
		}
		// The owner stops a job, a monitor with a pending line, and a sub-agent from the views.
		await call("bash_background", { command: "make serve", label: "serve" });
		const serve = s.execs.at(-1)!;
		serve.output("ready\n");
		const press = async (view: "jobs" | "agents", keys: string[]) => {
			let done!: () => void;
			const closed = new Promise<void>((resolve) => { done = resolve; });
			s.ui.custom = (factory: any) => {
				const tui = { terminal: { rows: 30 }, requestRender: () => {}, stop: () => {}, start: () => {} };
				const v = factory(tui, plain, undefined, () => done());
				log.push({ view, rows: v.render(120).length > 0 });
				for (const key of keys) v.handleInput(key);
				v.handleInput("q");
				return closed;
			};
			await s.commands.get(`jhon:${view}`).handler("", s.ctx);
		};
		await press("jobs", ["x"]);
		await notices(1);
		await call("monitor", { command: "tail -f x", timeout_seconds: 60, label: "watch" });
		s.execs.at(-1)!.output("pending line\n");
		await press("jobs", ["x"]);
		await notices(2);
		await press("agents", ["x"]);
		await notices(1);
		await call("task_result", { id: a8.id });
		// Twenty-five at once.
		for (let i = 0; i < 26; i++) await call("bash_background", { command: `c${i}` });
		await call("task_list", {});
		await s.fire("session_shutdown");
		// The notice cards, as the owner sees them, collapsed and expanded.
		const render = s.renderers.get("jhon-tasks.notice");
		const cards = [];
		for (const { message } of s.sent.slice(0, 3).concat(s.sent.filter((m) => m.message.details?.jhonTasks?.kind !== "command").slice(0, 3))) {
			for (const expanded of [false, true]) cards.push(render(message, { expanded }, plain).render(80));
		}
		cards.push(render({ content: [{ type: "text", text: "a\u001b[31mred\u0007" }], details: undefined }, { expanded: false }, plain).render(60));
		// The tone each notice draws in: its theme roles, tagged.
		const tagged = { fg: (role: string, text: string) => `<${role}>${text}</>`, bg: (role: string, text: string) => `[${role}]${text}[/]`, bold: (text: string) => `*${text}*` };
		for (const event of [undefined, "events", "timeout"]) {
			for (const succeeded of [true, false]) cards.push(render({ content: "x", details: { jhonTasks: { label: "l", kind: "monitor", event, succeeded } } }, { expanded: false }, tagged).render(40));
		}
		log.push({ cards });
		log.push({ statuses: s.statuses, notes: s.notes });
	} finally { s.cleanup(); }
	// The headless cap.
	const warned: string[] = [];
	const h = session({ mode: "print", headlessWaitMs: 30, warn: (line) => { warned.push(line); } });
	try {
		h.ctx.isIdle = () => false;
		await h.fire("session_start");
		h.background!.startAgent(worker, "one", h.out, "main");
		h.background!.startAgent(worker, "two", h.out, "main");
		await h.fire("agent_before_settle", {});
		log.push({ headless: h.sent, warned });
	} finally { h.cleanup(); }
	const definitions = {
		tools: [...s.tools.values()].map((tool) => JSON.parse(JSON.stringify(tool))),
		commands: [...s.commands].map(([name, c]) => ({ name, description: c.description })),
		shortcuts: [...s.shortcuts].map(([key, c]) => ({ key, description: c.description })),
		renderers: [...s.renderers.keys()],
	};
	const text = JSON.stringify({ definitions, log }, null, 1).split(s.out).join("<out>").split(h.out).join("<out>");
	return JSON.parse(text);
}

/** `node --experimental-strip-types test/background-tasks-capture.ts <file>` writes the snapshot. */
if (process.argv[1] && import.meta.url.endsWith(process.argv[1].split("/").pop()!) && process.argv[2]) {
	mkdirSync(join(process.argv[2], ".."), { recursive: true });
	writeFileSync(process.argv[2], `${JSON.stringify(await capture(), null, "\t")}\n`);
}
