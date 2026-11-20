/**
 * `subagent` (extensions/jhon.ts over lib/background-tasks-tools.ts): in the
 * main session every sub-agent runs in the background (L649): it returns a
 * task id at once; the answer reaches the agent as a follow-up message, and
 * the shell UI hears the running ones.
 * task_result reads a run, task_stop cancels one; a sub-agent, which has no
 * background tasks, runs the ones it starts in the foreground.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { chmodSync, mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test, { after } from "node:test";
import jhon from "../extensions/jhon.ts";
import { TASK_NOTICE_TYPE } from "../lib/background-tasks-tools.ts";
import { TASKS_EVENT } from "../lib/events.ts";
import { renderShellAgentsSection } from "../lib/ui/shell-bar.ts";

const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
function tempDir(prefix: string): string {
	const d = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
	temps.push(d);
	return d;
}

const EXPLORE = `---
name: explore
description: "Investigate before a change"
model: openai/gpt-5
thinking: high
tools:
  - read
---
Look around.
`;

/** A fake pi child that streams pi's JSON events (--mode json) and answers after FAKE_PI_DELAY milliseconds. */
function fakePiBin(): string {
	const bin = join(tempDir("jhon-bg-fake-"), "pi");
	writeFileSync(bin, `#!/usr/bin/env node
const out = (event) => process.stdout.write(JSON.stringify(event) + "\\n");
out({ type: "tool_execution_start", toolName: "grep", args: { pattern: "parse(" } });
setTimeout(() => out({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "found 3 callers" }] } }), Number(process.env.FAKE_PI_DELAY || 0));
`);
	chmodSync(bin, 0o755);
	return bin;
}

function setup(env: Record<string, string | undefined> = {}) {
	const dir = tempDir("jhon-bg-agent-");
	mkdirSync(join(dir, "agents"));
	writeFileSync(join(dir, "agents", "explore.md"), EXPLORE);
	const tools = new Map<string, any>();
	const commands = new Map<string, any>();
	const handlers = new Map<string, Array<(e: any, c: any) => unknown>>();
	const sent: Array<{ message: any; options: any }> = [];
	const emitted: Array<{ name: string; value: any }> = [];
	const saved: Record<string, string | undefined> = {};
	const vars = { PI_CODING_AGENT_DIR: dir, JHON_PI_BIN: fakePiBin(), JHON_PI_CHILD: undefined, ...env };
	for (const [k, v] of Object.entries(vars)) { saved[k] = process.env[k]; if (v === undefined) delete process.env[k]; else process.env[k] = v; }
	try {
		jhon({
			on: (n: string, h: any) => handlers.set(n, [...(handlers.get(n) ?? []), h]),
			registerTool: (t: any) => tools.set(t.name, t),
			registerCommand: (name: string, command: any) => commands.set(name, command),
			sendMessage: (message: any, options: any) => { sent.push({ message, options }); },
			events: { emit: (name: string, value: any) => emitted.push({ name, value }), on: () => () => {} },
		});
	} finally {
		for (const [k, v] of Object.entries(saved)) if (v === undefined) delete process.env[k]; else process.env[k] = v;
	}
	const ctx = { cwd: dir, hasUI: false, sessionManager: { getSessionId: () => "parent", getSessionFile: () => undefined } };
	const fire = async (name: string) => { for (const h of handlers.get(name) ?? []) await h({}, ctx); };
	const run = (name: string, params: any) => tools.get(name).execute("call-1", params, undefined, undefined, ctx);
	return { tools, commands, sent, emitted, ctx, fire, run, vars };
}

async function until(check: () => boolean): Promise<void> {
	for (const deadline = Date.now() + 5000; !check(); await new Promise((r) => setTimeout(r, 20))) if (Date.now() > deadline) throw new Error("timed out waiting");
}

/** Runs f with the setup's variables in process.env, as the child spawn reads them there. */
async function within<T>(vars: Record<string, string | undefined>, f: () => Promise<T>): Promise<T> {
	const saved: Record<string, string | undefined> = {};
	for (const [k, v] of Object.entries(vars)) { saved[k] = process.env[k]; if (v === undefined) delete process.env[k]; else process.env[k] = v; }
	try { return await f(); } finally { for (const [k, v] of Object.entries(saved)) if (v === undefined) delete process.env[k]; else process.env[k] = v; }
}

test("a sub-agent launched without asking for it runs in the background: it returns its id at once; its answer arrives as a follow-up, with the UI's running list", async () => {
	const s = setup();
	await s.fire("session_start");
	await within({ ...s.vars, FAKE_PI_DELAY: "100" }, async () => {
		const started = await s.run("subagent", { agent: "explore", task: "who calls parse?" });
		const id = /agent-\d+/.exec(started.content[0].text)?.[0];
		assert.ok(id, started.content[0].text);
		assert.equal(s.sent.length, 0, "nothing is delivered before it finishes");
		assert.deepEqual(s.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)?.value.tasks.map((a: any) => a.id), [id]);
		await until(() => s.sent.length > 0);
		const notice = s.sent[0]!;
		assert.equal(notice.message.customType, TASK_NOTICE_TYPE);
		assert.match(notice.message.content, new RegExp(`${id}.*explore.*completed[\\s\\S]*found 3 callers`));
		assert.deepEqual(notice.options, { triggerTurn: true, deliverAs: "followUp" });
		assert.deepEqual(s.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)?.value.tasks, []);
		const read = await s.run("task_result", { id });
		assert.match(read.content[0].text, /completed[\s\S]*found 3 callers/);
	});
});

test("task_stop cancels a running background sub-agent, with no message the agent did not need", async () => {
	const s = setup();
	await s.fire("session_start");
	await within({ ...s.vars, FAKE_PI_DELAY: "5000" }, async () => {
		const started = await s.run("subagent", { agent: "explore", task: "slow" });
		const id = /agent-\d+/.exec(started.content[0].text)![0];
		const stopped = await s.run("task_stop", { id });
		assert.match(stopped.content[0].text, /Stopped agent-1/);
		assert.equal(s.sent.length, 0);
		assert.match((await s.run("task_result", { id })).content[0].text, /was stopped/);
		await assert.rejects(s.run("task_result", { id: "agent-404" }), /No background task agent-404 in this session/);
	});
});

test("a sub-agent has no background tasks: the sub-agents it starts run in the foreground and answer in the call", async () => {
	const s = setup({ JHON_PI_CHILD: "1", JHON_PI_AGENT: "explore" });
	await within({ ...s.vars, JHON_PI_CHILD: "1" }, async () => {
		const r = await s.run("subagent", { agent: "explore", task: "x" });
		assert.match(r.content[0].text, /found 3 callers/);
		assert.doesNotMatch(r.content[0].text, /Started background/);
	});
	assert.doesNotMatch(s.tools.get("subagent").description, /background/);
	assert.equal(s.tools.has("task_result"), false);
	assert.equal(s.commands.has("jhon:agents"), false);
});

test("/jhon:agents is the main session's, and needs a terminal", async () => {
	const s = setup();
	assert.ok(s.commands.has("jhon:agents"));
	let noted = "";
	await s.commands.get("jhon:agents").handler("", { ...s.ctx, hasUI: true, mode: "rpc", ui: { notify: (m: string) => { noted = m; } } });
	assert.match(noted, /needs TUI mode/);
});

test("subagent_continue takes a background sub-agent's task id: not while it runs, nor when it ran unsaved", async () => {
	const s = setup();
	await s.fire("session_start");
	await within({ ...s.vars, FAKE_PI_DELAY: "100" }, async () => {
		const started = await s.run("subagent", { agent: "explore", task: "who calls parse?" });
		const id = /agent-\d+/.exec(started.content[0].text)![0];
		await assert.rejects(s.run("subagent_continue", { id, message: "go on" }), new RegExp(`${id} is still running`));
		await until(() => s.sent.length > 0);
		await assert.rejects(s.run("subagent_continue", { id, message: "go on" }), new RegExp(`${id} ran without a saved session`));
	});
});

test("several sub-agents launched in one turn each get a background task, all on the agents rail at once, each answer its own message", async () => {
	const s = setup();
	await s.fire("session_start");
	await within({ ...s.vars, FAKE_PI_DELAY: "150" }, async () => {
		const started = await Promise.all([1, 2, 3].map((n) => s.tools.get("subagent").execute(`call-${n}`, { agent: "explore", task: `look ${n}` }, undefined, undefined, s.ctx)));
		const ids = started.map((r: any) => /Started background sub-agent (agent-\d+)/.exec(r.content[0].text)?.[1]);
		assert.equal(new Set(ids).size, 3, JSON.stringify(ids));
		const rail = s.emitted.filter((e) => e.name === TASKS_EVENT).at(-1)?.value.tasks;
		assert.deepEqual(rail.map((t: any) => t.id).sort(), [...ids].sort());
		assert.ok(rail.every((t: any) => t.kind === "agent" && /^explore: look \d$/.test(t.label)), JSON.stringify(rail));
		const plain = { fg: (_c: string, t: string) => t, bg: (_c: string, t: string) => t, bold: (t: string) => t };
		const drawn = renderShellAgentsSection(rail, plain as any, 60, Date.now()).join("\n");
		assert.match(drawn, /AGENTS[\s\S]*3 running/);
		for (const n of [1, 2, 3]) assert.match(drawn, new RegExp(`look ${n}\\s+explore\\s+\\d+s`));
		await until(() => s.sent.length === 3);
		for (const id of ids) assert.ok(s.sent.some((m) => new RegExp(`${id}.*explore.*completed`).test(m.message.content)), id);
	});
});

test("the main session's subagent offers no foreground: no background option in its schema or description, and an old background: false still runs in the background", async () => {
	const s = setup();
	for (const name of ["subagent", "subagent_continue"]) {
		const tool = s.tools.get(name);
		assert.equal(tool.parameters.properties.background, undefined, name);
		assert.doesNotMatch(tool.description, /background: true/, name);
		assert.match(tool.description, /in the background/, name);
	}
	await s.fire("session_start");
	await within({ ...s.vars, FAKE_PI_DELAY: "50" }, async () => {
		const r = await s.run("subagent", { agent: "explore", task: "look", background: false });
		assert.match(r.content[0].text, /^Started background sub-agent agent-\d+ \(explore\)/);
		await until(() => s.sent.length > 0);
	});
});

test("a foreground request (background: false, or any mode but background) passes pi's validation, runs in the background anyway, and the result says so (T119, L716)", async () => {
	const { validateToolArguments } = await import("@earendil-works/pi-ai");
	const s = setup();
	await s.fire("session_start");
	for (const args of [{ background: false }, { mode: "foreground" }, { mode: "default" }, { mode: "task" }]) {
		const params = validateToolArguments(s.tools.get("subagent"), { name: "subagent", arguments: { agent: "explore", task: "look", ...args } });
		await within({ ...s.vars, FAKE_PI_DELAY: "50" }, async () => {
			const r = await s.run("subagent", params);
			const out = r.content[0].text;
			assert.match(out, /^Started background sub-agent (agent-\d+) \(explore\)/, JSON.stringify(args));
			assert.match(out, /Foreground was requested; in the main session every sub-agent runs in the background\./, JSON.stringify(args));
			assert.match(out, /Its completion is pushed automatically; continue other work or end the turn\./);
		});
	}
	await until(() => s.sent.length >= 4);
	const plain = await within({ ...s.vars, FAKE_PI_DELAY: "50" }, () => s.run("subagent", { agent: "explore", task: "look" }));
	assert.doesNotMatch(plain.content[0].text, /Foreground was requested/);
	assert.match(plain.content[0].text, /Its completion is pushed automatically; continue other work or end the turn\./);
	await until(() => s.sent.length >= 5);
	assert.doesNotThrow(() => validateToolArguments(s.tools.get("subagent_continue"), { name: "subagent_continue", arguments: { id: "x", message: "go", mode: "foreground" } }));
});

test("a delegation's card: the heading names the agent, its model and its thinking; the started line is its body", async () => {
	const s = setup();
	await s.fire("session_start");
	const theme = { fg: (_c: string, t: string) => t, bg: (_c: string, t: string) => t, bold: (t: string) => t };
	const tool = s.tools.get("subagent");
	const heading = tool.renderCall({ agent: "explore", task: "look" }, theme, { expanded: false, state: {}, isPartial: true }).render(120).join("\n");
	assert.match(heading, /→ Delegating · explore · openai\/gpt-5 · high/);
	await within({ ...s.vars, FAKE_PI_DELAY: "50" }, async () => {
		const r = await s.run("subagent", { agent: "explore", task: "look" });
		const body = tool.renderResult(r, { expanded: false, isPartial: false }, theme, { isError: false, state: {} }).render(120).join("\n");
		assert.match(body, /Started background sub-agent agent-\d+ \(explore\)/);
		await until(() => s.sent.length > 0);
	});
	const unknown = tool.renderCall({ agent: "nobody" }, theme, { expanded: false, state: {} }).render(120).join("\n");
	assert.match(unknown, /→ Delegating · nobody/);
});
