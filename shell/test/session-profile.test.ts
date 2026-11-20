/**
 * T120 (L979 option A, L980 A1-A10): a profile used in one session only.
 * `u` in the profiles panel records the profile in the session's own entry
 * and writes nothing global; the session's sub-agents, its orchestrator's
 * model, its delegation mode, its header and its panel follow it; a resumed
 * session comes back with it and a new one starts on the default; `d` is
 * `jhon profile use`, as before.
 *
 * The extension runs against a fake pi API, a fake `jhon` (it logs its calls
 * and prints the profiles a test wrote) and a fake pi child that records the
 * arguments and the environment it was started with.
 *
 *   jhon test
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test, { after, type TestContext } from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import jhon, { CONFIG_FILE } from "../extensions/jhon.ts";
import { childEnv, parseAgent } from "../lib/agents.ts";
import { SESSION_PROFILE_CHANGED, SESSION_PROFILE_ENTRY, SESSION_PROFILE_ENV, SessionProfile, piRows, sessionMark, sessionProfileOnBranch } from "../lib/session-profile.ts";
import { useMemorySettings } from "./fake-jhon-settings.ts";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const HOOK_BIN = process.env.JHON_TEST_BIN || join(REPO, "build", "jhon");
const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
const tempDir = (prefix: string) => { const d = realpathSync(mkdtempSync(join(tmpdir(), prefix))); temps.push(d); return d; };

const ORGANIC = "# Orchestrator\n\nIntro.\n<!-- delegation:begin -->\nOrganic rules.\n<!-- delegation:end -->\nTail.\n";
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

const rows = (orchestrator: string, explore: string) => [
	{ agent: "jhon-orchestrator", runtime: "pi", model: orchestrator, source: "profile" },
	{ agent: "explore", runtime: "pi", model: explore, source: "profile" },
	{ agent: "explore", runtime: "claude-code", model: "claude-opus-5-5@max", source: "agents.yaml" },
];
/** `jhon profile json`: p is the default; x is a writer profile on models this pi lists; bare names no level; far names a provider that is not loaded. */
const PROFILES = {
	active: "p", file: "~/.config/jhon/profiles.json",
	profiles: [
		{ name: "p", delegation: "inline", agents: rows("base/orch@medium", "openai/gpt-5@high") },
		{ name: "x", delegation: "writer", agents: rows("xprov/xorch@low", "xprov/xmodel@low") },
		{ name: "bare", delegation: "inline", agents: rows("xprov/xorch", "xprov/plain") },
		{ name: "far", delegation: "inline", agents: rows("far/model@high", "openai/gpt-5@high") },
	],
};
const AVAILABLE = ["base/orch", "openai/gpt-5", "xprov/xorch", "xprov/xmodel", "xprov/plain"];

type Handler = (event: any, ctx: any) => Promise<any>;
interface Options { child?: boolean; entries?: unknown[]; setter?: boolean; env?: Record<string, string> }

/** A fake pi child: it appends its arguments and the session profile it heard to FAKE_PI_LOG, then answers. */
function fakePi(dir: string): string {
	const bin = join(dir, "pi");
	writeFileSync(bin, `#!/usr/bin/env node
const args = process.argv.slice(2);
require("node:fs").appendFileSync(process.env.FAKE_PI_LOG, JSON.stringify({ args, profile: process.env.${SESSION_PROFILE_ENV} ?? null }) + "\\n");
if (args.includes("json")) process.stdout.write(JSON.stringify({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "done" }] } }) + "\\n");
else process.stdout.write("done\\n");
`);
	chmodSync(bin, 0o755);
	return bin;
}

function setup(t: TestContext, options: Options = {}) {
	const settings = useMemorySettings(t);
	const dir = tempDir("jhon-session-profile-");
	const files = {
		orchestrator: join(dir, "jhon-orchestrator.md"), writer: join(dir, "jhon-orchestrator-writer.md"), agent: join(dir, "agents", "explore.md"),
		settings: join(dir, "settings.json"), profiles: join(dir, "profiles.json"), json: join(dir, "profile-json.json"),
	};
	mkdirSync(join(dir, "agents"));
	writeFileSync(files.orchestrator, ORGANIC);
	writeFileSync(files.writer, "Writer rules.\n");
	writeFileSync(files.agent, EXPLORE);
	writeFileSync(files.settings, `${JSON.stringify({ theme: "rose-ink", quietStartup: true })}\n`);
	writeFileSync(files.profiles, JSON.stringify({ active: "p", profiles: { p: {}, x: { delegation: "writer" }, bare: {}, far: {} } }));
	writeFileSync(files.json, JSON.stringify(PROFILES));
	writeFileSync(join(dir, CONFIG_FILE), JSON.stringify({ jhon: HOOK_BIN, orchestrator: files.orchestrator, vcsDirs: [".git"], protected: ["/opt/none"], inspectAllow: [] }));
	const log = join(dir, "calls.log"), piLog = join(dir, "pi.log");
	const bin = join(dir, "jhon");
	writeFileSync(bin, `#!/bin/sh\necho "$*" >> "${log}"\ncase "$1 $2" in\n  "profile json") cat "${files.json}" ;;\nesac\n`);
	chmodSync(bin, 0o755);

	const handlers = new Map<string, Handler[]>();
	const tools = new Map<string, any>();
	const commands = new Map<string, any>();
	const entries: any[] = [...(options.entries ?? [])];
	const emitted: Array<{ name: string; value: any }> = [];
	const notes: string[] = [];
	// What the owner can read of the notices, as pi's status line draws them (interactive-mode.js
	// showStatus): an info notice replaces the one before it while nothing was drawn between them,
	// modelled as the notices of one tick: only the last is readable.
	const readable: string[] = [];
	let replaces = false;
	const notify = (text: string, type?: string) => {
		notes.push(text);
		const info = type === undefined || type === "info";
		if (info && replaces) readable[readable.length - 1] = text;
		else readable.push(text);
		replaces = info;
		setImmediate(() => { replaces = false; });
	};
	const setModels: string[] = [], levels: string[] = [];
	const ctx: any = {
		cwd: dir, hasUI: true, model: { provider: "base", id: "orch" },
		modelRegistry: {
			getAvailable: () => AVAILABLE.map((m) => ({ provider: m.split("/")[0], id: m.split("/")[1] })),
			find: (provider: string, id: string) => (AVAILABLE.includes(`${provider}/${id}`) ? { provider, id } : undefined),
			hasConfiguredAuth: () => true,
		},
		sessionManager: { getSessionId: () => "s1", getSessionFile: () => undefined, getBranch: () => entries, getEntries: () => entries },
		ui: { notify },
	};
	const pi: any = {
		on: (n: string, h: Handler) => handlers.set(n, [...(handlers.get(n) ?? []), h]),
		registerTool: (tool: any) => tools.set(tool.name, tool),
		registerCommand: (n: string, c: any) => commands.set(n, c),
		sendMessage() {},
		events: { emit: (name: string, value: any) => emitted.push({ name, value }), on: () => () => {} },
		appendEntry: (customType: string, data: unknown) => entries.push({ type: "custom", customType, data }),
		setModel: async (model: any) => { setModels.push(`${model.provider}/${model.id}`); ctx.model = model; return true; },
		getThinkingLevel: () => "medium",
		...(options.setter === false ? {} : { setThinkingLevel: (level: string) => { levels.push(level); } }),
	};
	// The variables the extension reads when pi loads it, and the ones a child's spawn reads later.
	const vars: Record<string, string | undefined> = {
		PI_CODING_AGENT_DIR: dir, CLAUDE_CONFIG_DIR: join(dir, "claude"), JHON_BIN: bin, JHON_PROFILES: files.profiles,
		JHON_PI_BIN: fakePi(dir), FAKE_PI_LOG: piLog, JHON_PI_CHILD: options.child ? "1" : undefined, JHON_PI_AGENT: options.child ? "explore" : undefined,
		[SESSION_PROFILE_ENV]: undefined, ...options.env,
	};
	const saved: Record<string, string | undefined> = {};
	for (const [k, v] of Object.entries(vars)) { saved[k] = process.env[k]; if (v === undefined) delete process.env[k]; else process.env[k] = v; }
	t.after(() => { for (const [k, v] of Object.entries(saved)) if (v === undefined) delete process.env[k]; else process.env[k] = v; });
	jhon(pi);

	const read = (file: string) => { try { return readFileSync(file, "utf8"); } catch { return ""; } };
	const fire = async (name: string, event: any = {}) => { let r: any; for (const h of handlers.get(name) ?? []) r = (await h(event, ctx)) ?? r; return r; };
	const launches = () => read(piLog).split("\n").filter(Boolean).map((l) => JSON.parse(l) as { args: string[]; profile: string | null });
	let calls = 0;
	return {
		dir, files, ctx, entries, emitted, notes, readable, setModels, levels, settings, fire, tools, launches,
		calls: () => read(log).split("\n").filter(Boolean),
		/** The session's profile entries, in order. */
		recorded: () => entries.filter((e) => e.customType === SESSION_PROFILE_ENTRY).map((e) => e.data),
		/** One turn's appended prompt. */
		turn: async () => {
			const options = { appendSystemPrompt: "", sections: {} as Record<string, string> };
			await fire("before_agent_start", { systemPromptOptions: options });
			return options.appendSystemPrompt;
		},
		/** Opens /jhon:profile; the panel pi would draw. */
		panel: async () => {
			let panel: any;
			const tui = { terminal: { rows: 40 }, requestRender() {} };
			const theme = { fg: (_r: string, x: string) => x, bold: (x: string) => x };
			await commands.get("jhon:profile").handler("", { ...ctx, ui: { ...ctx.ui, custom: async (f: any) => { panel = f(tui, theme, null, () => {}); return null; } } });
			return panel;
		},
		/** Launches explore and waits for its child to start; the child's `--model` and `--thinking`, and the session profile it heard. */
		launch: async () => {
			const before = launches().length;
			await tools.get("subagent").execute(`call-${++calls}`, { agent: "explore", task: "look" }, undefined, undefined, ctx);
			for (const deadline = Date.now() + 5000; launches().length <= before; await new Promise((r) => setTimeout(r, 20))) if (Date.now() > deadline) throw new Error("the sub-agent's child did not start");
			const { args, profile } = launches().at(-1)!;
			const flag = (name: string) => (args.includes(name) ? args[args.indexOf(name) + 1] : undefined);
			return { model: flag("--model"), thinking: flag("--thinking"), profile };
		},
	};
}

/** `u` on name, as the panel calls it: undefined, or why it was refused. */
async function use(s: ReturnType<typeof setup>, name: string): Promise<string | undefined> {
	return (await s.panel()).deps.useNow(name);
}

const settle = async (done: () => boolean) => { for (let i = 0; i < 150 && !done(); i++) await new Promise((r) => setTimeout(r, 20)); };

test("A1: u records the profile in the session's own entry and writes no global state", async (t) => {
	const s = setup(t);
	await s.fire("session_start", { reason: "startup" });
	const before = Object.values(s.files).map((file) => readFileSync(file, "utf8"));
	assert.equal(await use(s, "x"), undefined);
	assert.deepEqual(s.recorded(), [{ profile: "x" }], "the session records the profile in its own entry");
	assert.deepEqual(s.calls().filter((c) => /^profile (use|set|mode|new|rm)\b/.test(c) || c.startsWith("settings set")), [], "no jhon command that writes ran");
	assert.deepEqual(Object.values(s.files).map((file) => readFileSync(file, "utf8")), before, "profiles.json, the agent folder's settings.json and the rendered files are byte-identical");
	assert.equal(s.settings.source("pi.profile"), "default", "settings' pi.profile is not written");
	assert.deepEqual(s.emitted.filter((e) => e.name === SESSION_PROFILE_CHANGED).map((e) => e.value), [{ sessionId: "s1", profile: "x" }]);
});

test("A2: the next sub-agent runs on the session profile's model and thinking level for that agent; a level the profile does not name is not passed", async (t) => {
	const s = setup(t);
	await s.fire("session_start", { reason: "startup" });
	assert.equal(await use(s, "x"), undefined);
	assert.deepEqual(await s.launch(), { model: "xprov/xmodel", thinking: "low", profile: "x" });
	assert.equal(await use(s, "bare"), undefined);
	assert.deepEqual(await s.launch(), { model: "xprov/plain", thinking: undefined, profile: "bare" });
});

test("unchanged: a session with no session profile launches a sub-agent on its agent file's model and level, and reads no profile for it", async (t) => {
	const s = setup(t);
	await s.fire("session_start", { reason: "startup" });
	assert.deepEqual(await s.launch(), { model: "openai/gpt-5", thinking: "high", profile: null });
	assert.deepEqual(s.calls().filter((c) => c.startsWith("profile")), [], "no jhon profile call for a launch on the default");
	assert.deepEqual(s.recorded(), []);
});

test("A3: u switches the orchestrator to the profile's model and thinking level through pi, with a notice", async (t) => {
	const s = setup(t);
	await s.fire("session_start", { reason: "startup" });
	assert.equal(await use(s, "bare"), undefined);
	assert.deepEqual(s.setModels, ["xprov/xorch"]);
	assert.deepEqual(s.levels, [], "a profile that names no level sets none");
	assert.deepEqual(s.notes, ["The orchestrator switches to xprov/xorch from the next turn."]);
	// The same model, another level: only the level is set.
	assert.equal(await use(s, "x"), undefined);
	assert.deepEqual(s.setModels, ["xprov/xorch"], "the model it already runs on is not set again");
	assert.deepEqual(s.levels, ["low"]);
	assert.equal(s.notes[1], "The orchestrator switches to xprov/xorch · low from the next turn.");
});

test("A3: with a pi that has no thinking-level setter, the notice says the level applies at the next start", async (t) => {
	const s = setup(t, { setter: false });
	await s.fire("session_start", { reason: "startup" });
	assert.equal(await use(s, "x"), undefined);
	assert.deepEqual(s.setModels, ["xprov/xorch"]);
	assert.equal(s.notes[0], "The orchestrator switches to xprov/xorch from the next turn; its thinking level low applies at the next start.");
});

test("A3: u on a profile of the other mode says the model switch and the mode switch in one notice, both readable where a notice replaces the one before it", async (t) => {
	const s = setup(t);
	await s.fire("session_start", { reason: "startup" });
	await s.turn();
	assert.equal(await use(s, "x"), undefined);
	const read = s.readable.join("\n");
	assert.ok(read.includes("The orchestrator switches to xprov/xorch · low from the next turn."), `the model switch is readable: ${JSON.stringify(s.readable)}`);
	assert.ok(read.includes("The orchestrator's rules switch to writer mode from the next turn."), `the mode switch is readable: ${JSON.stringify(s.readable)}`);
	assert.deepEqual(s.notes, ["The orchestrator switches to xprov/xorch · low from the next turn. The orchestrator's rules switch to writer mode from the next turn."], "one notice, each sentence as it is said alone");
	assert.deepEqual(s.setModels, ["xprov/xorch"]);
	assert.match(await s.turn(), /Writer rules\./);
});

test("A4: the session's delegation mode follows its profile, the prompt rebuilt once with the notice, while the default profile stays", async (t) => {
	const s = setup(t);
	await s.fire("session_start", { reason: "startup" });
	const first = await s.turn();
	assert.match(first, /Organic rules\./);
	assert.equal(await use(s, "x"), undefined);
	assert.ok(s.notes.some((note) => note.endsWith("The orchestrator's rules switch to writer mode from the next turn.")), JSON.stringify(s.notes));
	const switched = await s.turn();
	assert.match(switched, /Writer rules\./);
	assert.doesNotMatch(switched, /Organic rules/);
	assert.equal(await s.turn(), switched, "byte-identical from then on");
	assert.equal(JSON.parse(readFileSync(s.files.profiles, "utf8")).active, "p");
});

test("A5: the panel shows the session's profile, marked while it differs from the default, and still does when it is opened again", async (t) => {
	const s = setup(t);
	await s.fire("session_start", { reason: "startup" });
	const panel = await s.panel();
	await settle(() => /Orchestrator/.test(panel.render(120).join("\n")));
	assert.match(panel.render(120).join("\n"), /│ p {3}This session runs on it\. New sessions start on it\./);
	assert.doesNotMatch(panel.render(120).join("\n"), /this session only/, "no mark while the session follows the default");
	panel.handleInput("\x1b[C");
	panel.handleInput("u");
	await settle(() => s.recorded().length > 0 && /x is used in this session\./.test(panel.render(120).join("\n")));
	// Opened again: a new panel reads the session's profile, not the default.
	const again = await s.panel();
	await settle(() => /Orchestrator/.test(again.render(120).join("\n")));
	const text: string = again.render(120).join("\n");
	assert.match(text, /│ {2}p ⊙★ {2}x ⇄● /, "the tabs: ★ on the default, ● on this session's profile");
	assert.match(text, /● this session only/, "the legend names the mark");
	again.handleInput("\x1b[C");
	assert.match(again.render(120).join("\n"), /│ x {3}This session runs on it\./);
});

test("A5: the header names the session's profile, marked while it differs from the default", async (t) => {
	// The header is the poster, drawn where a desktop palette stands and the settings follow it: the
	// shell's settings, and the engine's that `jhon palette` reads (JHON_SETTINGS), both a test's own.
	let header: any;
	const colors = join(tempDir("jhon-session-profile-colors-"), "colors.json");
	writeFileSync(colors, JSON.stringify({ primary: "#aabbcc" }));
	writeFileSync(join(dirname(colors), "settings.toml"), "schema = 1\n\n[integrations.theme]\npalette = \"desktop\"\n");
	const h = setup(t, { env: { JHON_DESKTOP_COLORS: colors, JHON_DESKTOP_NVIM_SPEC: "/nonexistent/spec.lua", JHON_SETTINGS: join(dirname(colors), "settings.toml") } });
	useMemorySettings(t, { "integrations.theme.palette": "desktop" });
	h.ctx.mode = "tui";
	h.ctx.ui.setHeader = (f: any) => { header = f; };
	await h.fire("session_start", { reason: "startup" });
	const text = () => header(null, { fg: (_r: string, x: string) => x }).render(100).join("\n");
	assert.match(text(), /\bp\b/);
	assert.doesNotMatch(text(), /\(session\)/);
	assert.equal(await use(h, "x"), undefined);
	assert.match(text(), /x \(session\)/);
});

test("A6: a resumed session comes back with its profile; /new returns to the default", async (t) => {
	const s = setup(t, { entries: [{ type: "custom", customType: SESSION_PROFILE_ENTRY, data: { profile: "x" } }] });
	await s.fire("session_start", { reason: "resume" });
	assert.match(await s.turn(), /Writer rules\./, "its delegation mode");
	assert.deepEqual(await s.launch(), { model: "xprov/xmodel", thinking: "low", profile: "x" }, "its sub-agents");
	assert.deepEqual(s.setModels, [], "pi restores a resumed session's model itself");
	// /new: the new session's branch holds no entry.
	s.entries.length = 0;
	await s.fire("session_start", { reason: "new" });
	assert.match(await s.turn(), /Organic rules\./);
	assert.deepEqual(await s.launch(), { model: "openai/gpt-5", thinking: "high", profile: null });
});

test("A7: u on a profile that names a model this pi does not list is refused with one line that says why and what to do, and nothing changes", async (t) => {
	const s = setup(t);
	await s.fire("session_start", { reason: "startup" });
	const first = await s.turn();
	const refusal = await use(s, "far");
	assert.equal(refusal, "far/model cannot run in this pi (provider not loaded, or no credential). To use it: make it the default with d, then restart pi.");
	assert.deepEqual(s.recorded(), []);
	assert.deepEqual(s.setModels, []);
	assert.deepEqual(s.levels, []);
	assert.deepEqual(s.notes, []);
	assert.deepEqual(s.emitted.filter((e) => e.name === SESSION_PROFILE_CHANGED), []);
	assert.equal(await s.turn(), first);
	assert.deepEqual(await s.launch(), { model: "openai/gpt-5", thinking: "high", profile: null });
	// The panel says it as one line.
	const panel = await s.panel();
	await settle(() => /Orchestrator/.test(panel.render(200).join("\n")));
	for (let i = 0; i < 3; i++) panel.handleInput("\x1b[C");
	panel.handleInput("u");
	await settle(() => /Could not use far/.test(panel.render(260).join("\n")));
	assert.match(panel.render(260).join("\n"), /│ Could not use far: far\/model cannot run in this pi \(provider not loaded, or no credential\)\. To use it: make it the default with d, then restart pi\. +│/);
});

test("A7: at 120 columns the panel's refusal still says what to do, wrapped onto further rows, not cut", async (t) => {
	const s = setup(t);
	await s.fire("session_start", { reason: "startup" });
	const panel = await s.panel();
	await settle(() => /Orchestrator/.test(panel.render(120).join("\n")));
	for (let i = 0; i < 3; i++) panel.handleInput("\x1b[C");
	panel.handleInput("u");
	await settle(() => /Could not use far/.test(panel.render(120).join("\n")));
	const lines: string[] = panel.render(120);
	// The frame's rows read as one text: each without its borders, a wrap read as a space.
	const text = lines.map((l) => l.replace(/^│ /, "").replace(/ │$/, "").trimEnd()).filter(Boolean).join(" ");
	assert.ok(text.includes("make it the default with d"), `the refusal at 120 columns says "make it the default with d": ${text.slice(text.indexOf("Could not use"))}`);
	assert.ok(text.includes("restart pi"), `the refusal at 120 columns says "restart pi": ${text.slice(text.indexOf("Could not use"))}`);
	assert.ok(text.includes("Could not use far: far/model cannot run in this pi (provider not loaded, or no credential). To use it: make it the default with d, then restart pi."), text.slice(text.indexOf("Could not use")));
	for (const l of lines) assert.equal(visibleWidth(l), 120, l);
	assert.deepEqual(s.recorded(), []);
});

test("A8: d is `jhon profile use`, as before, and records no session profile; a session keeps its profile after a d elsewhere", async (t) => {
	const s = setup(t);
	await s.fire("session_start", { reason: "startup" });
	const panel = await s.panel();
	assert.equal(await panel.deps.makeDefault("bare"), undefined);
	assert.deepEqual(s.calls().filter((c) => c.startsWith("profile use")), ["profile use bare"]);
	assert.deepEqual(s.recorded(), []);
	assert.deepEqual(s.setModels, [], "d switches no model in this session");
	assert.deepEqual(s.notes, []);
	// This session uses x; elsewhere another profile becomes the default and the agent folder is rendered again.
	assert.equal(await use(s, "x"), undefined);
	writeFileSync(s.files.profiles, JSON.stringify({ active: "bare", profiles: { p: {}, x: { delegation: "writer" }, bare: {}, far: {} } }));
	writeFileSync(s.files.json, JSON.stringify({ ...PROFILES, active: "bare" }));
	writeFileSync(s.files.agent, EXPLORE.replace("openai/gpt-5", "xprov/plain").replace("thinking: high\n", ""));
	assert.deepEqual(await s.launch(), { model: "xprov/xmodel", thinking: "low", profile: "x" });
});

test("A9: a session profile that was deleted or renamed falls back to the default with a one-line notice, said once", async (t) => {
	const s = setup(t);
	await s.fire("session_start", { reason: "startup" });
	assert.equal(await use(s, "bare"), undefined);
	s.notes.length = 0;
	writeFileSync(s.files.json, JSON.stringify({ ...PROFILES, profiles: PROFILES.profiles.filter((p) => p.name !== "bare") }));
	assert.deepEqual(await s.launch(), { model: "openai/gpt-5", thinking: "high", profile: null });
	assert.deepEqual(s.notes, ["jhon: the profile bare this session used no longer exists (renamed or deleted); the session is back on the default profile p."]);
	assert.deepEqual(s.recorded(), [{ profile: "bare" }, { profile: null }], "the session's entry says it is back on the default");
	assert.deepEqual(s.emitted.filter((e) => e.name === SESSION_PROFILE_CHANGED).at(-1)?.value, { sessionId: "s1" });
	assert.deepEqual(await s.launch(), { model: "openai/gpt-5", thinking: "high", profile: null });
	assert.equal(s.notes.length, 1, "said once");
	// A resumed session whose profile is gone says it at its start.
	const r = setup(t, { entries: [{ type: "custom", customType: SESSION_PROFILE_ENTRY, data: { profile: "renamed" } }] });
	await r.fire("session_start", { reason: "resume" });
	assert.deepEqual(r.notes, ["jhon: the profile renamed this session used no longer exists (renamed or deleted); the session is back on the default profile p."]);
	assert.match(await r.turn(), /Organic rules\./);
});

test("A10: a sub-agent hears its parent session's profile, and the sub-agents it launches run on that profile", async (t) => {
	// The parent's side is A2's launch: the child's environment names the profile.
	const explore = parseAgent(EXPLORE, "explore");
	assert.equal(childEnv(explore, undefined, {}, "x")[SESSION_PROFILE_ENV], "x");
	assert.equal(SESSION_PROFILE_ENV in childEnv(explore, undefined, { [SESSION_PROFILE_ENV]: "outer" }), false, "a session on the default passes none on");
	// The child's side: the extension loaded as a sub-agent of a session that uses x.
	const s = setup(t, { child: true, env: { [SESSION_PROFILE_ENV]: "x" } });
	const r = await s.tools.get("subagent").execute("call-1", { agent: "explore", task: "look" }, undefined, undefined, s.ctx);
	assert.match(r.content[0].text, /done/);
	const { args, profile } = s.launches().at(-1)!;
	assert.equal(args[args.indexOf("--model") + 1], "xprov/xmodel");
	assert.equal(args[args.indexOf("--thinking") + 1], "low");
	assert.equal(profile, "x", "and its own sub-agents hear it too");
	assert.deepEqual(s.recorded(), [], "a child records nothing");
});

test("the session-profile entry and a profile's pi rows, read", async () => {
	const entry = (profile: unknown) => ({ type: "custom", customType: SESSION_PROFILE_ENTRY, data: { profile } });
	assert.equal(sessionProfileOnBranch(undefined), undefined);
	assert.equal(sessionProfileOnBranch([{ type: "custom", customType: "other", data: { profile: "a" } }, { type: "message" }]), undefined);
	assert.equal(sessionProfileOnBranch([entry("a"), entry("b")]), "b", "the last one on the branch");
	assert.equal(sessionProfileOnBranch([entry("a"), entry(null)]), undefined, "back on the default");
	assert.deepEqual([...piRows(PROFILES, "x")!], [["jhon-orchestrator", { model: "xprov/xorch", thinking: "low" }], ["explore", { model: "xprov/xmodel", thinking: "low" }]], "pi's rows only, split at @");
	assert.equal(piRows(PROFILES, "gone"), undefined);
	assert.equal(sessionMark("x", "p"), "x (session)");
	assert.equal(sessionMark("p", "p"), "p");
	assert.equal(sessionMark(undefined, "p"), "p");
	// A read that fails keeps the rows read before.
	let fail = false;
	const profile = new SessionProfile(async () => { if (fail) throw new Error("jhon is gone"); return PROFILES; });
	assert.deepEqual(await profile.refresh("/"), { state: "none" });
	profile.start("x");
	assert.deepEqual(await profile.refresh("/"), { state: "ok" });
	fail = true;
	assert.deepEqual(await profile.refresh("/"), { state: "unread", why: "jhon is gone" });
	assert.equal(profile.agent(parseAgent(EXPLORE, "explore")).model, "xprov/xmodel");
});
