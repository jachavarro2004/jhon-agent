import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after } from "node:test";
import jhon, { CONFIG_FILE } from "../extensions/jhon.ts";
import { FULL_MESSAGE, changeMoment, contextLevel, fullOnBranch, fullTrigger, leanDeferred } from "../lib/lean-context.ts";
import { parseSessionBrief } from "../lib/session-brief.ts";
import { ToolPolicy, toolsMoreTool } from "../lib/tools-deferred.ts";
import { LDD_REMINDER } from "../lib/ldd-reminder.ts";

// T146h (L1278, L1280): pi's session starts lean when no change is active or
// the active one is small, and loads the full context once, as one
// persisted `jhon-full` message, when `jhon status` says the change is not
// small or a sub-agent is launched (T3); the system prompt never changes.
// On a lean session Jhon's own tools wait behind tools_more (L3), except the
// delegation tools, offered from the first request (T192 S3).

// T194 (L1608): the mark the shell's own messages open with; pi hands each to the model as user text.
const MARK = "[From the Jhon shell, not the owner; it does not set the reply language]";

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

// T192 S7: the moments a change may appear or be selected, where a lean session asks the engine once.
test("a change may appear on a write or edit under a layout's changes folder, or a bash command that makes or selects one", () => {
	assert.equal(changeMoment("write", { path: "ldd/changes/c1/tasks.md" }, "/r"), "/r/ldd/changes/c1");
	assert.equal(changeMoment("edit", { path: "/r/openspec/changes/c1/proposal.md" }, "/elsewhere"), "/r/openspec/changes/c1");
	assert.equal(changeMoment("write", { file_path: "sub/ldd/changes/c1/tasks.md" }, "/r"), "/r/sub/ldd/changes/c1");
	for (const command of ["mkdir -p ldd/changes/c1", "git switch -c feat/c1", "cd /r && git checkout feat/c1", "jhon executions run", "jhon risk --json"]) assert.equal(changeMoment("bash", { command }, "/r"), "/r", command);
	for (const [tool, input] of [
		["write", { path: "src/a.ts" }], ["edit", { path: "ldd/specs/a/spec.md" }], ["write", { path: "docs/changes/notes.md" }], ["write", { path: "ldd/config.yaml" }],
		["read", { path: "ldd/changes/c1/tasks.md" }], ["bash", { command: "ls" }], ["bash", { command: "git status" }], ["bash", { command: "jhon status" }],
		["inspect", { command: "git checkout --help" }], ["subagent", { agent: "worker", task: "ldd/changes/c1" }],
	] as const) assert.equal(changeMoment(tool, input, "/r"), undefined, `${tool} ${JSON.stringify(input)}`);
});

// T7 (L5): routes that are no status read of jhon's never switch pi to full.
test("another server's status, Engram, and Claude Code's plugin name are no pi trigger", () => {
	const notSmall = JSON.stringify({ ok: true, notes: ["not small: read /h/full-pi.md before the next edit"] });
	for (const tool of ["mcp__other__status", "mcp__engram__mem_search", "mcp__plugin_jhon_jhon__status", "mcp__plugin_engram_engram__mem_search"]) assert.equal(fullTrigger(tool, {}, notSmall), false, tool);
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

// T258: on claude-bridge a tool enabled mid-turn is available, but only from the next prompt; the answer says so.
const BRIDGE_ONE = " On claude-bridge the tool list is read when a prompt starts, so it is available but reaches the model only from the next prompt: its absence from this prompt's tools does not make it unavailable.";
const BRIDGE_MANY = " On claude-bridge the tool list is read when a prompt starts, so they are available but reach the model only from the next prompt: their absence from this prompt's tools does not make them unavailable.";

test("tools_more says a tool the session already has is enabled, and on claude-bridge that it is available from the next prompt", async () => {
	const all = ["read", ...DELEGATION, "jhon_review", "mcp__engram__mem_update"];
	for (const [provider, said] of [
		["anthropic", "Already enabled: subagent."],
		["claude-bridge", `Already enabled: subagent.${BRIDGE_ONE}`],
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
		const again = provider === "claude-bridge" ? BRIDGE_MANY : "";
		// Enabled during the turn: on claude-bridge not from the next step but from the next prompt.
		const enabled = provider === "claude-bridge" ? `Enabled: jhon_review.${BRIDGE_ONE}` : "Enabled: jhon_review (callable from the next step on).";
		assert.equal(mixed, `${enabled}\nAlready enabled: monitor, task_list.${again}\nUnknown or not deferred: nope.`, provider);
		assert.deepEqual(h.active(), ["read", ...DELEGATION, "jhon_review"], provider);
		assert.deepEqual(h.entries, [["jhon-tools-more", { names: ["jhon_review"] }]], provider);
	}
});

// T7 (L5): a session that started full has every Jhon tool already; tools_more says so, and sets nothing.
test("tools_more in a session that started full says Jhon's tools are already enabled", async () => {
	const all = ["read", ...DELEGATION, "jhon_review", "mcp__engram__mem_update"];
	const h = moreHost(all, all);
	const more = toolsMoreTool(h.host, new ToolPolicy(true, () => true));
	const text = (await more.execute("c1", { names: ["jhon_review", "subagent"] }, undefined, undefined, { cwd: "/r", model: { provider: "anthropic" } })).content[0].text;
	assert.equal(text, "Already enabled: jhon_review, subagent.");
	assert.deepEqual(h.active(), all);
	assert.deepEqual(h.entries, []);
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

function load(dir: string, tools: readonly string[] = MAIN_TOOLS, child = false) {
	const saved = { ...process.env };
	Object.assign(process.env, { PI_CODING_AGENT_DIR: dir, CLAUDE_CONFIG_DIR: join(dir, "claude") });
	delete process.env.JHON_PI_CHILD;
	if (child) process.env.JHON_PI_CHILD = "1";
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
		assert.ok(full[0].message.content.startsWith(`${MARK} Jhon: this work needs the full context; it binds from now on.\n\n`), full[0].message.content);
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

// T7 (L5): a sub-agent's session is never lean: its tools and prompt stay as before, and no status switches it.
test("a child session's tools are untouched by the lean start", async () => {
	const tools = ["read", "bash", ...DELEGATION, "jhon_review", "mcp__engram__mem_search"];
	const dir = agentDir("lean"), cwd = temp("jhon-lean-cwd-");
	const s = load(dir, tools, true);
	const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, selectedTools: s.active() };
	await s.emit("before_agent_start", { systemPromptOptions: options }, ctx(cwd));
	assert.deepEqual(options.selectedTools, tools);
	assert.doesNotMatch(options.appendSystemPrompt, /LEAN PROMPT|FULL PROMPT/);
	await s.emit("tool_result", statusResult("  - not small: read /h/full-pi.md before the next edit"), ctx(cwd));
	assert.deepEqual(s.sent.filter((m) => m.message.customType === FULL_MESSAGE), []);
});

// ── T192 S7: a change that appears mid-session ──

/** An agent dir whose jhon answers session-brief with the context in its `context` file, and logs each ask. */
function changingDir(): { dir: string; context: (c: "lean" | "full") => void; asks: () => number; hang: () => void } {
	const dir = agentDir("lean");
	const fake = join(dir, "jhon");
	writeFileSync(join(dir, "context"), "lean");
	writeFileSync(fake, `#!/bin/sh\nif [ "$2" = session-brief ]; then echo x >> '${dir}/asks'; if [ -f '${dir}/hang' ]; then exec sleep 30; fi; printf '{"lead":"Lead.","brief":"## The project rules","context":"%s"}' "$(cat '${dir}/context')"; exit 0; fi\nif [ "$1" = status ]; then printf '{}'; exit 0; fi\nexit 0\n`);
	chmodSync(fake, 0o755);
	const asks = () => { try { return readFileSync(join(dir, "asks"), "utf8").split("\n").filter(Boolean).length; } catch { return 0; } };
	return { dir, context: (c) => writeFileSync(join(dir, "context"), c), asks, hang: () => writeFileSync(join(dir, "hang"), "") };
}

const writeResult = (path: string) => ({ toolName: "write", input: { path, content: "## 1. Tasks\n" }, content: [{ type: "text", text: `Wrote ${path}` }], isError: false });
const bashResult = (command: string, text = "") => ({ toolName: "bash", input: { command }, content: [{ type: "text", text }], isError: false });

test("a lean session switches to full once when the agent creates or selects a change that is not small", async () => {
	for (const [name, moment] of [
		["tasks.md written", (cwd: string) => writeResult(join(cwd, "ldd/changes/c1/tasks.md"))],
		["proposal edited", (cwd: string) => ({ toolName: "edit", input: { path: "openspec/changes/c1/proposal.md", edits: [] }, content: [{ type: "text", text: "Edited" }], isError: false })],
		["branch selected", () => bashResult("git switch feat/c1", "Switched to branch 'feat/c1'")],
	] as const) {
		const d = changingDir(), cwd = temp("jhon-lean-cwd-");
		mkdirSync(join(cwd, "ldd/changes/c1"), { recursive: true });
		mkdirSync(join(cwd, "openspec/changes/c1"), { recursive: true });
		const s = load(d.dir);
		const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, selectedTools: s.active() };
		await s.emit("before_agent_start", { systemPromptOptions: options }, ctx(cwd));
		assert.equal(contextLevel(), "lean", name);
		const prompt = options.appendSystemPrompt;
		const started = d.asks();
		// Other tools never ask the engine.
		await s.emit("tool_result", writeResult(join(cwd, "src/a.ts")), ctx(cwd));
		await s.emit("tool_result", bashResult("ls"), ctx(cwd));
		assert.equal(d.asks(), started, `${name}: no ask for other tools`);
		d.context("full");
		await s.emit("tool_result", moment(cwd), ctx(cwd));
		const full = s.sent.filter((m) => m.message.customType === FULL_MESSAGE);
		assert.equal(full.length, 1, name);
		assert.match(full[0].message.content, /FULL PROMPT/, name);
		assert.equal(full[0].options?.deliverAs, "steer", `${name}: before the next request`);
		assert.equal(options.appendSystemPrompt, prompt, `${name}: the system prompt never changes`);
		assert.equal(contextLevel(), "full", name);
		assert.ok(s.active().includes("jhon_review"), `${name}: Jhon's tools are back`);
		// Once full, nothing asks again or sends a second message.
		const after = d.asks();
		await s.emit("tool_result", moment(cwd), ctx(cwd));
		await s.emit("tool_result", bashResult("jhon executions run"), ctx(cwd));
		assert.equal(d.asks(), after, `${name}: no ask once full`);
		assert.equal(s.sent.filter((m) => m.message.customType === FULL_MESSAGE).length, 1, name);
	}
});

test("a small change that appears mid-session keeps the session lean", async () => {
	const d = changingDir(), cwd = temp("jhon-lean-cwd-");
	const s = load(d.dir);
	const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, selectedTools: s.active() };
	await s.emit("before_agent_start", { systemPromptOptions: options }, ctx(cwd));
	const started = d.asks();
	await s.emit("tool_result", bashResult("git switch -c fix-typo", "Switched to a new branch 'fix-typo'"), ctx(cwd));
	await s.emit("tool_result", bashResult("jhon executions run", "RED"), ctx(cwd));
	assert.equal(d.asks(), started + 2, "each moment asks the engine");
	assert.deepEqual(s.sent.filter((m) => m.message.customType === FULL_MESSAGE), []);
	assert.equal(contextLevel(), "lean");
	assert.ok(!s.active().includes("jhon_review"), "Jhon's own tools still wait");
});

// T192 fix round (L11): three change moments at once ask the engine once, and the session gets one full message.
test("concurrent change moments ask the engine once and send one full message", async () => {
	const d = changingDir(), cwd = temp("jhon-lean-cwd-");
	mkdirSync(join(cwd, "ldd/changes/c1"), { recursive: true });
	const s = load(d.dir);
	const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, selectedTools: s.active() };
	await s.emit("before_agent_start", { systemPromptOptions: options }, ctx(cwd));
	const started = d.asks();
	d.context("full");
	await Promise.all([
		s.emit("tool_result", writeResult(join(cwd, "ldd/changes/c1/tasks.md")), ctx(cwd)),
		s.emit("tool_result", bashResult("git switch feat/c1"), ctx(cwd)),
		s.emit("tool_result", bashResult("jhon executions run"), ctx(cwd)),
	]);
	assert.equal(d.asks(), started + 1, "one ask");
	assert.equal(s.sent.filter((m) => m.message.customType === FULL_MESSAGE).length, 1, "one full message");
	assert.equal(contextLevel(), "full");
});

// T192 fix round (L11): a slow or hung engine never holds a tool result for long; the moment is unknown, the session stays lean.
test("a hung session-brief at a change moment gives up within a few seconds and keeps the session lean", async () => {
	const d = changingDir(), cwd = temp("jhon-lean-cwd-");
	const s = load(d.dir);
	const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, selectedTools: s.active() };
	await s.emit("before_agent_start", { systemPromptOptions: options }, ctx(cwd));
	d.context("full");
	d.hang();
	const t0 = Date.now();
	const r = await s.emit("tool_result", bashResult("git switch feat/c1"), ctx(cwd));
	const took = Date.now() - t0;
	assert.ok(took < 6_000, `held the tool result ${took} ms`);
	assert.equal(r, undefined, "no error reaches the model");
	assert.deepEqual(s.sent.filter((m) => m.message.customType === FULL_MESSAGE), []);
	assert.equal(contextLevel(), "lean");
});

test("T258: tools_more's answers are unchanged off claude-bridge, for one tool and several", async () => {
	const all = ["read", ...DELEGATION, "jhon_review", "mcp__engram__mem_update"];
	for (const provider of [undefined, "anthropic", "openai"]) {
		const policy = new ToolPolicy(true, () => true);
		policy.setLean(true);
		const h = moreHost(["read", ...DELEGATION], all);
		const more = toolsMoreTool(h.host, policy);
		const ctx = { cwd: "/r", model: provider ? { provider } : undefined };
		assert.equal((await more.execute("c1", { names: ["jhon_review"] }, undefined, undefined, ctx)).content[0].text, "Enabled: jhon_review (callable from the next step on).", String(provider));
		const p2 = new ToolPolicy(true, () => true);
		p2.setLean(true);
		const two = toolsMoreTool(moreHost(["read", ...DELEGATION], all).host, p2);
		assert.equal((await two.execute("c2", { names: ["jhon_review", "mcp__engram__mem_update", "read"] }, undefined, undefined, ctx)).content[0].text, "Enabled: jhon_review, mcp__engram__mem_update (callable from the next step on).\nAlready enabled: read.", String(provider));
	}
});
