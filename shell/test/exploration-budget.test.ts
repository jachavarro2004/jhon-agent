/**
 * T117e (L745, L746): the exploration-budget notice. Per owner request the
 * main session's lookups (read, grep, find, ls, inspect, codegraph, a bash
 * command that only reads) and the bytes they returned are counted; past 3
 * lookups or 40,000 bytes the tool result carries one short notice, its
 * wording by delegation mode. Children never; a new owner message resets it.
 */

import "./isolated-env.ts";
import { test, after, type TestContext } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import jhon, { CONFIG_FILE } from "../extensions/jhon.ts";
import { explorationNotice, readOnlyBash } from "../lib/exploration-budget.ts";
import { useMemorySettings } from "./fake-jhon-settings.ts";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const HOOK_BIN = process.env.JHON_TEST_BIN || join(REPO, "build", "jhon");
const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
const tempDir = (prefix: string) => { const d = realpathSync(mkdtempSync(join(tmpdir(), prefix))); temps.push(d); return d; };

type Handler = (event: any, ctx: any) => Promise<any>;

async function session(t: TestContext, mode: "inline" | "writer", env: Record<string, string> = {}) {
	useMemorySettings(t, { "shell.behaviour.backstop_tokens": 0 });
	const dir = tempDir("jhon-xbudget-agent-");
	const repo = tempDir("jhon-xbudget-repo-");
	spawnSync("git", ["init", "-q"], { cwd: repo });
	writeFileSync(join(dir, "jhon-orchestrator.md"), "# O\n<!-- delegation:begin -->\nOrganic.\n<!-- delegation:end -->\n");
	writeFileSync(join(dir, "jhon-orchestrator-writer.md"), "Writer.\n");
	writeFileSync(join(dir, CONFIG_FILE), JSON.stringify({ jhon: HOOK_BIN, orchestrator: join(dir, "jhon-orchestrator.md"), vcsDirs: [".git"], protected: ["/opt/none"], inspectAllow: [] }));
	const profiles = join(dir, "profiles.json");
	writeFileSync(profiles, JSON.stringify({ active: "p", profiles: { p: mode === "writer" ? { delegation: "writer" } : {} } }));
	const savedProfiles = process.env.JHON_PROFILES;
	process.env.JHON_PROFILES = profiles;
	t.after(() => { if (savedProfiles === undefined) delete process.env.JHON_PROFILES; else process.env.JHON_PROFILES = savedProfiles; });
	const saved = { ...process.env };
	Object.assign(process.env, { PI_CODING_AGENT_DIR: dir, CLAUDE_CONFIG_DIR: join(dir, "claude"), ...env });
	const handlers = new Map<string, Handler[]>();
	try {
		jhon({ on: (n: string, h: Handler) => handlers.set(n, [...(handlers.get(n) ?? []), h]), registerTool: () => {}, registerCommand: () => {} } as any);
	} finally {
		for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
		Object.assign(process.env, saved);
	}
	const fire = async (name: string, event: any, ctx: any) => { let r: any; for (const h of handlers.get(name) ?? []) r = (await h(event, ctx)) ?? r; return r; };
	await fire("before_agent_start", { systemPromptOptions: { appendSystemPrompt: "", sections: {} } }, { cwd: repo, ui: { notify() {} } });
	/** The notice appended to this lookup's result, if any. */
	const look = async (toolName: string, input: Record<string, unknown>, text = "x") => {
		const content = [{ type: "text", text }];
		const r = await fire("tool_result", { toolName, input, content, isError: false }, { cwd: repo, ui: { notify() {} } });
		const parts: any[] = r?.content ?? content;
		return parts.slice(1).map((p) => p.text).find((s: string) => s.startsWith("Exploration budget")) as string | undefined;
	};
	const owner = (source = "interactive") => fire("input", { type: "input", text: "go", source }, { cwd: repo });
	return { look, owner, fire, repo };
}

test("past 3 lookups the fourth result carries the notice, once per request; a new owner message resets it, a notice does not", async (t) => {
	const s = await session(t, "inline");
	assert.equal(await s.look("read", { path: "a.ts" }), undefined);
	assert.equal(await s.look("grep", { pattern: "x" }), undefined);
	assert.equal(await s.look("bash", { command: "git log --oneline -5 | head -3" }), undefined);
	assert.equal(await s.look("bash", { command: "npm test" }), undefined, "a command that does more than read is no lookup");
	const n = await s.look("find", { pattern: "*.ts" });
	assert.equal(n, explorationNotice(4, 4, "inline"));
	assert.equal(await s.look("ls", {}), undefined, "once per request");
	await s.fire("input", { type: "input", text: "notice", source: "extension", streamingBehavior: "followUp" }, { cwd: s.repo });
	assert.equal(await s.look("read", { path: "b.ts" }), undefined, "an extension message is no new request");
	await s.owner();
	for (let i = 0; i < 3; i++) assert.equal(await s.look("inspect", { command: "git diff" }), undefined);
	assert.ok(await s.look("read", { path: "c.ts" }), "a new request, a new notice");
});

test("past 40,000 bytes the notice comes on fewer lookups", async (t) => {
	const s = await session(t, "inline");
	assert.equal(await s.look("read", { path: "a.ts" }, "é".repeat(15_000)), undefined);
	assert.equal(await s.look("read", { path: "b.ts" }, "y".repeat(10_001)), explorationNotice(2, 40_001, "inline"));
});

test("the wording by mode, short, and the writer's names the worker's brief", async (t) => {
	const inline = explorationNotice(4, 50_000, "inline");
	const writer = explorationNotice(4, 50_000, "writer");
	assert.match(inline, /one explore sub-agent with a short handoff/);
	assert.match(inline, /prepares an understood inline write is exempt/);
	assert.match(writer, /belongs in its brief/);
	assert.ok(inline.length < 300 && writer.length < 300, `${inline.length} ${writer.length}`);
	const s = await session(t, "writer");
	for (let i = 0; i < 3; i++) await s.look("read", { path: "a.ts" });
	assert.equal(await s.look("read", { path: "a.ts" }), explorationNotice(4, 4, "writer"));
});

test("a child never hears it", async (t) => {
	const s = await session(t, "inline", { JHON_PI_CHILD: "1" });
	for (let i = 0; i < 6; i++) assert.equal(await s.look("read", { path: "a.ts" }, "z".repeat(20_000)), undefined);
});

// T134 (L1021 H3): 5,198 of 5,688 real sub-agent bash reads open with `cd <dir> &&`; they count, and nothing else changes.
test("a read that opens with `cd <dir> &&` counts; a write or a run behind one does not", () => {
	for (const c of ["cd x && cat f", "cd /home/me/repo && git log --oneline -3", "cd shell && sed -n 1,20p a.ts | head", "cd 'my dir' && rg foo src"]) assert.ok(readOnlyBash(c), c);
	for (const c of ["cd x && rm f", "cd x && cat f > g", "cd x && npm test", "cd x", "cd x && cd y && cat f", "cd x; cat f", "cd x || cat f", "cat f && cd x"]) assert.ok(!readOnlyBash(c), c);
});

test("read-only bash: reading pipes and chains count; writes, redirections and actions do not", () => {
	for (const c of ["cat a | wc -l", "git show HEAD:a.ts", "sed -n 1,20p a.ts && rg foo src", "find . -name '*.ts' 2>/dev/null"]) assert.ok(readOnlyBash(c), c);
	for (const c of ["echo x > f", "sed -i s/a/b/ f", "find . -delete", "git commit -m x", "rm a", "cat $(ls)", "go test ./..."]) assert.ok(!readOnlyBash(c), c);
});
