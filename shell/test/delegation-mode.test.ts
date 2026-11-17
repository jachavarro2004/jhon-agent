/**
 * T117c (L720): the active profile's delegation mode in the main session's
 * prompt, read once per session (byte-stable), writer rules spliced over the
 * orchestrator's marked region with a whole-organic fallback said once; the
 * profiles panel's `w` (jhon profile mode) and `u` (use in this session only,
 * T120: the prompt rebuilt once on a mode change, with a notice, and no
 * `jhon profile use`; test/session-profile.test.ts covers the rest of `u`).
 */

import "./isolated-env.ts";
import { test, after, type TestContext } from "node:test";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import jhon, { CONFIG_FILE } from "../extensions/jhon.ts";
import { useMemorySettings } from "./fake-jhon-settings.ts";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const HOOK_BIN = process.env.JHON_TEST_BIN || join(REPO, "build", "jhon");
const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
const tempDir = (prefix: string) => { const d = realpathSync(mkdtempSync(join(tmpdir(), prefix))); temps.push(d); return d; };

const ORGANIC = "# Orchestrator\n\nIntro.\n<!-- delegation:begin -->\nOrganic rules.\n<!-- delegation:end -->\nTail.\n";
const WRITER = "Writer rules.\n";

const p = (name: string, delegation: string) => ({ name, delegation, agents: [{ agent: "jhon-orchestrator", runtime: "pi", model: "x/m@high", source: "profile" }] });

type Handler = (event: any, ctx: any) => Promise<any>;

/** An agent dir with the orchestrator (and the writer file unless null), and a profiles file whose active profile has mode. */
function setup(t: TestContext, mode: "inline" | "writer", prompt = ORGANIC, writer: string | null = WRITER) {
	useMemorySettings(t);
	const dir = tempDir("jhon-deleg-");
	writeFileSync(join(dir, "jhon-orchestrator.md"), prompt);
	if (writer !== null) writeFileSync(join(dir, "jhon-orchestrator-writer.md"), writer);
	const profiles = join(dir, "profiles.json");
	writeProfiles(profiles, "p", { p: mode, i: "inline", w: "writer" });
	writeFileSync(join(dir, CONFIG_FILE), JSON.stringify({ jhon: HOOK_BIN, orchestrator: join(dir, "jhon-orchestrator.md"), vcsDirs: [".git"], protected: ["/opt/none"], inspectAllow: [] }));
	const log = join(dir, "calls.log");
	const bin = join(dir, "jhon");
	writeFileSync(bin, `#!/bin/sh\necho "$*" >> "${log}"\ncase "$1 $2" in\n  "profile json") echo '${JSON.stringify({ active: "p", file: "PROFILES", profiles: [p("p", "inline"), p("i", "inline"), p("w", "writer")] }).replace("PROFILES", profiles)}' ;;\nesac\n`);
	chmodSync(bin, 0o755);
	const saved = process.env.JHON_PROFILES;
	process.env.JHON_PROFILES = profiles;
	t.after(() => { if (saved === undefined) delete process.env.JHON_PROFILES; else process.env.JHON_PROFILES = saved; });
	return { dir, profiles, bin, calls: () => { try { return readFileSync(log, "utf8").trim().split("\n"); } catch { return []; } } };
}

function writeProfiles(file: string, active: string, modes: Record<string, string>) {
	const profiles: Record<string, unknown> = {};
	for (const [name, mode] of Object.entries(modes)) profiles[name] = mode === "writer" ? { delegation: "writer" } : {};
	writeFileSync(file, JSON.stringify({ active, profiles }));
}

function load(dir: string, env: Record<string, string> = {}) {
	const saved = { ...process.env };
	Object.assign(process.env, { PI_CODING_AGENT_DIR: dir, CLAUDE_CONFIG_DIR: join(dir, "claude"), ...env });
	const handlers = new Map<string, Handler[]>();
	const commands = new Map<string, any>();
	try {
		jhon({ on: (n: string, h: Handler) => handlers.set(n, [...(handlers.get(n) ?? []), h]), registerTool: () => {}, registerCommand: (n: string, c: any) => commands.set(n, c) } as any);
	} finally {
		for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
		Object.assign(process.env, saved);
	}
	const fire = async (name: string, event: any, ctx: any) => { let r: any; for (const h of handlers.get(name) ?? []) r = (await h(event, ctx)) ?? r; return r; };
	return { fire, commands };
}

/** One turn's appended prompt, and the notices said during it. */
async function turn(ext: ReturnType<typeof load>, cwd: string, notes: string[] = []) {
	const options = { appendSystemPrompt: "", sections: {} as Record<string, string> };
	await ext.fire("before_agent_start", { systemPromptOptions: options }, { cwd, ui: { notify: (text: string) => notes.push(text) } });
	return options.appendSystemPrompt;
}

test("writer mode splices the writer rules over the marked region; the prompt is byte-identical across turns", async (t) => {
	const s = setup(t, "writer");
	const ext = load(s.dir);
	const notes: string[] = [];
	const first = await turn(ext, s.dir, notes);
	assert.match(first, /Intro\.\nWriter rules\.\nTail\./);
	assert.doesNotMatch(first, /Organic rules|delegation:begin|delegation:end/);
	// The profiles file changes mid-session: the prompt waits for the next session.
	writeProfiles(s.profiles, "p", { p: "inline" });
	assert.equal(await turn(ext, s.dir, notes), first);
	assert.deepEqual(notes, []);
});

test("inline mode keeps the organic rules; the prompt is byte-identical across turns", async (t) => {
	const s = setup(t, "inline");
	const ext = load(s.dir);
	const first = await turn(ext, s.dir);
	assert.match(first, /Organic rules\./);
	assert.doesNotMatch(first, /Writer rules/);
	writeProfiles(s.profiles, "p", { p: "writer" });
	assert.equal(await turn(ext, s.dir), first);
});

for (const [name, prompt, writer, why] of [
	["the writer file is missing", ORGANIC, null, /jhon-orchestrator-writer\.md is missing/],
	["the begin marker is missing", ORGANIC.replace("<!-- delegation:begin -->\n", ""), WRITER, /lacks the <!-- delegation:begin --> marker/],
	["the end marker is missing", ORGANIC.replace("<!-- delegation:end -->\n", ""), WRITER, /lacks the <!-- delegation:end --> marker/],
	["the markers are out of order", "# O\n<!-- delegation:end -->\nOrganic rules.\n<!-- delegation:begin -->\n", WRITER, /out of order/],
] as const) {
	test(`writer mode keeps the organic rules whole when ${name}, and says so once`, async (t) => {
		const s = setup(t, "writer", prompt, writer);
		const ext = load(s.dir);
		const notes: string[] = [];
		const first = await turn(ext, s.dir, notes);
		assert.ok(first.startsWith(prompt.replace(/\n$/, "")), first);
		assert.doesNotMatch(first, /Writer rules/);
		assert.equal(await turn(ext, s.dir, notes), first);
		assert.equal(notes.length, 1, JSON.stringify(notes));
		assert.match(notes[0], why);
	});
}

test("a child never gets the orchestrator's prompt, in writer mode either", async (t) => {
	const s = setup(t, "writer");
	assert.equal(await turn(load(s.dir, { JHON_PI_CHILD: "1" }), s.dir), "");
});

/** The profiles panel's deps, as the panel calls them. */
async function panelDeps(ext: ReturnType<typeof load>, cwd: string, notes: string[]) {
	let panel: any;
	const tui = { terminal: { rows: 40 }, requestRender() {} };
	const theme = { fg: (_r: string, x: string) => x, bold: (x: string) => x };
	await ext.commands.get("jhon:profile").handler("", { cwd, ui: { notify: (text: string) => notes.push(text), custom: async (f: any) => { panel = f(tui, theme, null, () => {}); return null; } } });
	return panel.deps;
}

test("the panel's setMode() runs `jhon profile mode` and leaves this session's prompt as it is", async (t) => {
	const s = setup(t, "inline");
	const ext = load(s.dir, { JHON_BIN: s.bin });
	const notes: string[] = [];
	const first = await turn(ext, s.dir);
	const deps = await panelDeps(ext, s.dir, notes);
	await deps.setMode("p", "writer");
	assert.ok(s.calls().includes("profile mode p writer"), JSON.stringify(s.calls()));
	writeProfiles(s.profiles, "p", { p: "writer" });
	assert.equal(await turn(ext, s.dir), first, "saved without using: from the next session");
	assert.deepEqual(notes, []);
});

test("`u` with a mode change rebuilds the prompt once with a notice; without a change it does not", async (t) => {
	const s = setup(t, "inline");
	const ext = load(s.dir, { JHON_BIN: s.bin });
	const notes: string[] = [];
	const first = await turn(ext, s.dir);
	const deps = await panelDeps(ext, s.dir, notes);
	// The same mode: nothing is rebuilt.
	assert.equal(await deps.useNow("i"), undefined);
	assert.deepEqual(notes, []);
	assert.equal(await turn(ext, s.dir), first);
	// To writer: one notice, the next turn's prompt has the writer rules, then it stays. T120 (L980 A1):
	// u is this session's alone, so `jhon profile use` does not run and the default profile stays p.
	assert.equal(await deps.useNow("w"), undefined);
	assert.ok(!s.calls().some((c) => c.startsWith("profile use")), JSON.stringify(s.calls()));
	assert.deepEqual(notes, ["The orchestrator's rules switch to writer mode from the next turn."]);
	const switched = await turn(ext, s.dir);
	assert.match(switched, /Writer rules\./);
	assert.equal(await deps.useNow("w"), undefined);
	assert.equal(notes.length, 1, "using the same mode again says nothing");
	assert.equal(await turn(ext, s.dir), switched);
	// makeDefault activates without rebuilding the prompt.
	assert.equal(await deps.makeDefault("i"), undefined);
	assert.ok(s.calls().includes("profile use i"));
	assert.equal(notes.length, 1);
	assert.equal(await turn(ext, s.dir), switched);
});

test("end to end through the profiles panel: w then s saves the mode, u uses it once with the notice, d makes it the default without a rebuild", async (t) => {
	const s = setup(t, "inline");
	const ext = load(s.dir, { JHON_BIN: s.bin });
	const notes: string[] = [];
	const first = await turn(ext, s.dir);
	let panel: any;
	const tui = { terminal: { rows: 40 }, requestRender() {} };
	const theme = { fg: (_r: string, x: string) => x, bold: (x: string) => x };
	await ext.commands.get("jhon:profile").handler("", { cwd: s.dir, ui: { notify: (text: string) => notes.push(text), custom: async (f: any) => { panel = f(tui, theme, null, () => {}); return null; } } });
	const settle = async (done: () => boolean) => { for (let i = 0; i < 100 && !done(); i++) await new Promise((r) => setTimeout(r, 20)); };
	await settle(() => /Orchestrator/.test(panel.render(100).join("\n")));
	panel.handleInput("w");
	panel.handleInput("s");
	await settle(() => s.calls().includes("profile mode p writer"));
	assert.ok(s.calls().includes("profile mode p writer"), JSON.stringify(s.calls()));
	assert.deepEqual(notes, [], "saving applies from the next session");
	assert.equal(await turn(ext, s.dir), first);
	await settle(() => !/loading|saving/.test(panel.render(100).join("\n")));
	// u on the writer profile: this session's mode changes, so the prompt is rebuilt once. T120 (L980 A1):
	// u runs no `jhon profile use`; the default profile stays p.
	panel.handleInput("\x1b[C");
	panel.handleInput("\x1b[C");
	panel.handleInput("u");
	await settle(() => notes.length > 0);
	assert.ok(!s.calls().some((c) => c.startsWith("profile use")), JSON.stringify(s.calls()));
	assert.deepEqual(notes, ["The orchestrator's rules switch to writer mode from the next turn."]);
	const switched = await turn(ext, s.dir);
	assert.match(switched, /Writer rules\./);
	await settle(() => !/using|loading/.test(panel.render(100).join("\n")));
	// d: profile use again, no rebuild and no notice.
	const before = s.calls().length;
	panel.handleInput("d");
	await settle(() => s.calls().slice(before).includes("profile use w"));
	assert.ok(s.calls().slice(before).includes("profile use w"), JSON.stringify(s.calls()));
	await new Promise((r) => setTimeout(r, 50));
	assert.equal(notes.length, 1);
	assert.equal(await turn(ext, s.dir), switched);
});
