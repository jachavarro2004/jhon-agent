import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { chmodSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after } from "node:test";
import jhon, { CONFIG_FILE } from "../extensions/jhon.ts";
import { FULL_MESSAGE, contextLevel, fullOnBranch, fullTrigger, leanDeferred } from "../lib/lean-context.ts";
import { parseSessionBrief } from "../lib/session-brief.ts";
import { ToolPolicy, toolsMoreTool } from "../lib/tools-deferred.ts";
import { LDD_REMINDER } from "../lib/ldd-reminder.ts";

// T146h (L1278, L1280): pi's session starts lean when no change is active or
// the active one is small, and loads the full context once, as one
// persisted `jhon-full` message, when `jhon status` says the change is not
// small or a sub-agent is launched (T3); the system prompt never changes.
// On a lean session Jhon's own tools wait behind tools_more (L3), except the
// delegation tools, offered from the first request (T192 S3).

/** T192 S3: the delegation tools every main session is offered from its first request, lean or full. */
const DELEGATION = ["subagent", "subagent_continue", "task_list", "task_result", "task_stop", "bash_background", "monitor"];

const dirs: string[] = [];
after(() => { for (const d of dirs) rmSync(d, { recursive: true, force: true }); });
const temp = (prefix: string) => { const d = mkdtempSync(join(tmpdir(), prefix)); dirs.push(d); return d; };

test("session-brief's context is read with the brief or the inactive line", () => {
	assert.deepEqual(parseSessionBrief(JSON.stringify({ lead: "L", brief: "B", context: "lean" })), { lead: "L", brief: "B", context: "lean" });
	assert.deepEqual(parseSessionBrief(JSON.stringify({ lead: "L", brief: "B", context: "full" })), { lead: "L", brief: "B", context: "full" });
	assert.deepEqual(parseSessionBrief(JSON.stringify({ inactive: "I", context: "lean" })), { inactive: "I", context: "lean" });
	assert.deepEqual(parseSessionBrief(JSON.stringify({ lead: "L", brief: "B", context: "other" })), { lead: "L", brief: "B" });
});

test("the full context is due on status's not-small line and on a sub-agent launch, nothing else", () => {
	assert.equal(fullTrigger("bash", { command: "jhon status" }, "notes\n  - not small: read /h/full-pi.md before the next edit"), true);
	assert.equal(fullTrigger("bash", { command: "cd /r && jhon status --brief" }, "not small: read /h/full-pi.md before the next edit"), true);
	assert.equal(fullTrigger("bash", { command: "jhon status" }, "  - small change (no folder, ...)"), false);
	assert.equal(fullTrigger("bash", { command: "cat notes.txt" }, "not small: read x before the next edit"), false);
	assert.equal(fullTrigger("subagent", { agent: "worker", task: "x" }, ""), true);
	assert.equal(fullTrigger("subagent_continue", { id: "1" }, ""), true);
	assert.equal(fullTrigger("read", { path: "x" }, "not small: read"), false);
});

// T192 S5: whatever route read the status, its not-small line switches to full.
test("the full context is due on the not-small line read through inspect or the jhon MCP server's status tool", () => {
	const notSmall = JSON.stringify({ ok: true, notes: ["not small: read /h/full-pi.md before the next edit"] });
	const small = JSON.stringify({ ok: true, notes: ["small change (no folder, ...)"] });
	assert.equal(fullTrigger("mcp__jhon__status", {}, notSmall), true);
	assert.equal(fullTrigger("mcp__jhon__status", { change: "c", root: "/r" }, notSmall), true);
	assert.equal(fullTrigger("mcp__jhon__status", {}, small), false);
	assert.equal(fullTrigger("mcp__jhon__audit", { session: "s" }, notSmall), false);
	assert.equal(fullTrigger("inspect", { command: "jhon status" }, "  - not small: read /h/full-pi.md before the next edit"), true);
	assert.equal(fullTrigger("inspect", { command: "jhon status" }, "  - small change (no folder, ...)"), false);
	assert.equal(fullTrigger("inspect", { command: "git log" }, "not small: read x"), false);
});

test("a lean session defers Jhon's own tools but the delegation ones, and keeps todo, ask_user, inspect and codegraph", () => {
	for (const name of ["jhon_review", "m365_read", "mcp__engram__mem_search", "mcp__engram__mem_save"]) assert.equal(leanDeferred(name), true, name);
	for (const name of [...DELEGATION, "todo", "ask_user", "inspect", "mcp__codegraph__codegraph_explore", "read", "bash", "edit", "write", "tools_more"]) assert.equal(leanDeferred(name), false, name);
	const policy = new ToolPolicy(true, () => true);
	policy.setLean(true);
	assert.deepEqual(policy.filter(["read", "todo", "subagent", "jhon_review", "mcp__engram__mem_search", "inspect"], "/r"), ["read", "todo", "subagent", "inspect"]);
	policy.enable(["jhon_review"]);
	assert.deepEqual(policy.filter(["read", "jhon_review", "mcp__engram__mem_search"], "/r"), ["read", "jhon_review"]);
	policy.setLean(false);
	assert.equal(policy.filter(["read", "jhon_review", "mcp__engram__mem_search"], "/r"), undefined);
	// A child is never lean.
	const kid = new ToolPolicy(false, () => true);
	kid.setLean(true);
	assert.equal(kid.filter(["jhon_review", "mcp__engram__mem_search"], "/r"), undefined);
});

// T192 S4: tools_more never calls a tool the session already has unknown.
function moreHost(active: string[], all: string[]) {
	let now = [...active];
	const entries: unknown[] = [];
	const host = {
		getActiveTools: () => [...now], setActiveTools: (names: string[]) => { now = [...names]; },
		getAllTools: () => all.map((name) => ({ name, description: `${name} does one thing.` })),
		appendEntry: (type: string, data: unknown) => entries.push([type, data]),
	};
	return { host, entries, active: () => now };
}

test("tools_more says a tool the session already has is enabled, and on claude-bridge that it reaches the model at the next prompt", async () => {
	const all = ["read", ...DELEGATION, "jhon_review", "mcp__engram__mem_update"];
	for (const [provider, said] of [
		["anthropic", "Already enabled: subagent."],
		["claude-bridge", "Already enabled: subagent. On claude-bridge the tool list is read when a prompt starts, so it reaches the model at the next prompt."],
	] as const) {
		// S2: after the full switch the tool is active and no longer deferred.
		const full = moreHost(all, all);
		const afterSwitch = toolsMoreTool(full.host, new ToolPolicy(true, () => true));
		assert.equal((await afterSwitch.execute("c0", { names: ["subagent"] }, undefined, undefined, { cwd: "/r", model: { provider } })).content[0].text, said, `${provider}: full`);
		assert.deepEqual(full.entries, [], `${provider}: full, nothing recorded`);
		const policy = new ToolPolicy(true, () => true);
		policy.setLean(true);
		const h = moreHost(["read", ...DELEGATION], all);
		const more = toolsMoreTool(h.host, policy);
		const ctx = { cwd: "/r", model: { provider, id: "claude-opus-5-5" } };
		assert.equal((await more.execute("c1", { names: ["subagent"] }, undefined, undefined, ctx)).content[0].text, said, provider);
		assert.deepEqual(h.active(), ["read", ...DELEGATION], `${provider}: nothing set`);
		assert.deepEqual(h.entries, [], `${provider}: nothing recorded`);
		// A really deferred tool is listed and enabled as before; a tool nobody has is still unknown.
		const listed = (await more.execute("c2", {}, undefined, undefined, ctx)).content[0].text;
		assert.equal(listed, "Deferred tools (enable with names):\n- jhon_review: jhon_review does one thing.\n- mcp__engram__mem_update: mcp__engram__mem_update does one thing.", provider);
		const mixed = (await more.execute("c3", { names: ["jhon_review", "monitor", "task_list", "nope"] }, undefined, undefined, ctx)).content[0].text;
		const again = provider === "claude-bridge" ? " On claude-bridge the tool list is read when a prompt starts, so they reach the model at the next prompt." : "";
		assert.equal(mixed, `Enabled: jhon_review (callable from the next step on).\nAlready enabled: monitor, task_list.${again}\nUnknown or not deferred: nope.`, provider);
		assert.deepEqual(h.active(), ["read", ...DELEGATION, "jhon_review"], provider);
		assert.deepEqual(h.entries, [["jhon-tools-more", { names: ["jhon_review"] }]], provider);
	}
});

test("a session whose branch holds the full message is full", () => {
	assert.equal(fullOnBranch([{ type: "custom_message", customType: FULL_MESSAGE }]), true);
	assert.equal(fullOnBranch([{ type: "custom_message", customType: "jhon-route" }]), false);
	assert.equal(fullOnBranch(undefined), false);
});

test("the LDD reminder names the bare run", () => {
	assert.match(LDD_REMINDER, /`jhon executions run` while it fails/);
	assert.doesNotMatch(LDD_REMINDER, /<change>/);
});

// ── the extension ──

/** An agent dir with a lean and a full prompt, and a jhon whose session-brief says context. */
function agentDir(context: "lean" | "full" | "none"): string {
	const dir = temp("jhon-lean-");
	writeFileSync(join(dir, "full.md"), "# Orchestrator\n\nFULL PROMPT.");
	writeFileSync(join(dir, "lean.md"), "# Jhon\n\nLEAN PROMPT.");
	const fake = join(dir, "jhon");
	const brief = context === "none" ? "" : JSON.stringify({ lead: "Lead.", brief: "## The project rules\n\n- Delivery: one.", context });
	writeFileSync(fake, `#!/bin/sh\nif [ "$2" = session-brief ]; then printf '%s' '${brief}'; exit 0; fi\nif [ "$1" = status ]; then printf '{}'; exit 0; fi\nexit 0\n`);
	chmodSync(fake, 0o755);
	writeFileSync(join(dir, CONFIG_FILE), JSON.stringify({ jhon: fake, orchestrator: join(dir, "full.md"), lean: join(dir, "lean.md"), full: join(dir, "full.md"), vcsDirs: [".git"], protected: ["/opt/x"], inspectAllow: ["git diff"] }));
	return dir;
}

const MAIN_TOOLS = ["read", "bash", "todo", "ask_user", "inspect", "subagent", "jhon_review", "mcp__engram__mem_search"];

function load(dir: string, tools: readonly string[] = MAIN_TOOLS) {
	const saved = { ...process.env };
	Object.assign(process.env, { PI_CODING_AGENT_DIR: dir, CLAUDE_CONFIG_DIR: join(dir, "claude") });
	delete process.env.JHON_PI_CHILD;
	const all = new Map<string, Array<(e: any, c: any) => any>>();
	const sent: Array<{ message: any; options: any }> = [];
	let active = [...tools];
	const api = {
		on: (name: string, h: any) => all.set(name, [...(all.get(name) ?? []), h]),
		registerTool: () => {}, registerCommand: () => {},
		sendMessage: (message: any, options: any) => sent.push({ message, options }),
		getActiveTools: () => [...active], setActiveTools: (names: string[]) => { active = [...names]; },
		getAllTools: () => tools.map((name) => ({ name })),
	};
	try { jhon(api as any); } finally {
		for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
		Object.assign(process.env, saved);
	}
	const emit = async (name: string, event: any, ctx: any) => { let r: any; for (const h of all.get(name) ?? []) r = (await h(event, ctx)) ?? r; return r; };
	return { emit, sent, active: () => active };
}

const ctx = (cwd: string, provider = "anthropic") => ({ cwd, model: { provider, id: "claude-opus-5-5" }, sessionManager: { getSessionId: () => "s1", getBranch: () => [] } });
const statusResult = (text: string) => ({ toolName: "bash", input: { command: "jhon status" }, content: [{ type: "text", text }], isError: false });

test("a small task starts lean: the lean prompt, Jhon's tools but the delegation ones deferred, no full message", async () => {
	const dir = agentDir("lean"), cwd = temp("jhon-lean-cwd-");
	const s = load(dir);
	const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, selectedTools: s.active(), skills: [{ name: "ldd", description: "LDD", filePath: "/x/SKILL.md" }] };
	await s.emit("before_agent_start", { systemPromptOptions: options }, ctx(cwd));
	assert.match(options.appendSystemPrompt, /LEAN PROMPT/);
	assert.doesNotMatch(options.appendSystemPrompt, /FULL PROMPT/);
	assert.deepEqual(options.selectedTools, ["read", "bash", "todo", "ask_user", "inspect", "subagent"]);
	assert.deepEqual(options.skills, [], "the skills list waits for the full context");
	assert.equal(contextLevel(), "lean");
	await s.emit("tool_result", statusResult("  - small change (no folder, ...)"), ctx(cwd));
	await s.emit("tool_result", mcpStatusResult(JSON.stringify({ ok: true, notes: ["small change (no folder, ...)"] })), ctx(cwd));
	assert.deepEqual(s.sent.filter((m) => m.message.customType === FULL_MESSAGE), []);
});

// T192 S3: the first request of a lean main session offers every delegation tool, on every provider.
test("a lean main session's first request offers every delegation tool, on claude-bridge and on another provider", async () => {
	const tools = ["read", "bash", "todo", "ask_user", "inspect", ...DELEGATION, "jhon_review", "m365_read", "mcp__engram__mem_search"];
	for (const provider of ["claude-bridge", "anthropic"]) {
		const dir = agentDir("lean"), cwd = temp("jhon-lean-cwd-");
		const s = load(dir, tools);
		const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, selectedTools: s.active() };
		await s.emit("before_agent_start", { systemPromptOptions: options }, ctx(cwd, provider));
		assert.equal(contextLevel(), "lean", provider);
		assert.deepEqual(options.selectedTools, ["read", "bash", "todo", "ask_user", "inspect", ...DELEGATION], provider);
		await s.emit("context", { messages: [] }, ctx(cwd, provider));
		assert.deepEqual(s.active(), ["read", "bash", "todo", "ask_user", "inspect", ...DELEGATION], `${provider}: the request's active tools`);
	}
});

const mcpStatusResult = (text: string) => ({ toolName: "mcp__jhon__status", input: {}, content: [{ type: "text", text }], isError: false });

test("a not-small status, or a sub-agent launch, loads the full context once, as a persisted message", async () => {
	for (const trigger of [statusResult("  - not small: read /h/full-pi.md before the next edit"), mcpStatusResult(JSON.stringify({ ok: true, notes: ["not small: read /h/full-pi.md before the next edit"] })), { toolName: "subagent", input: { agent: "worker", task: "x" }, content: [{ type: "text", text: "task 1" }], isError: false }]) {
		const dir = agentDir("lean"), cwd = temp("jhon-lean-cwd-");
		const s = load(dir);
		const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, selectedTools: s.active() };
		await s.emit("before_agent_start", { systemPromptOptions: options }, ctx(cwd));
		const prompt = options.appendSystemPrompt;
		await s.emit("tool_result", trigger, ctx(cwd));
		await s.emit("tool_result", trigger, ctx(cwd));
		const full = s.sent.filter((m) => m.message.customType === FULL_MESSAGE);
		assert.equal(full.length, 1, trigger.toolName);
		assert.match(full[0].message.content, /FULL PROMPT/);
		assert.equal(full[0].message.display, false);
		assert.equal(options.appendSystemPrompt, prompt, "the system prompt never changes");
		assert.ok(s.active().includes("subagent") && s.active().includes("jhon_review"), "Jhon's tools are back");
		assert.equal(contextLevel(), "full");
	}
});

test("a change that is not small starts full, as today, and gets no full message", async () => {
	for (const context of ["full", "none"] as const) {
		const dir = agentDir(context), cwd = temp("jhon-lean-cwd-");
		const s = load(dir);
		const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, selectedTools: s.active() };
		await s.emit("before_agent_start", { systemPromptOptions: options }, ctx(cwd));
		assert.match(options.appendSystemPrompt, /FULL PROMPT/, context);
		assert.doesNotMatch(options.appendSystemPrompt, /LEAN PROMPT/, context);
		assert.ok(options.selectedTools.includes("subagent"), context);
		await s.emit("tool_result", statusResult("  - not small: read /h/full-pi.md before the next edit"), ctx(cwd));
		assert.deepEqual(s.sent.filter((m) => m.message.customType === FULL_MESSAGE), [], context);
	}
});
