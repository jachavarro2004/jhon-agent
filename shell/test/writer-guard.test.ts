/**
 * T117c (L720): in writer mode the main session's edit or write changes at
 * most ~20 lines per call; a larger one is refused toward a jhon-builder. odd/**
 * always passes; inline mode and children are unaffected. The guard reads
 * the session's frozen mode, the one the prompt read.
 */

import "./isolated-env.ts";
import { test, after, type TestContext } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
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

type Handler = (event: any, ctx: any) => Promise<any>;

/** A main session (or a child) whose active profile has mode, after its first turn, in a git project. */
async function session(t: TestContext, mode: "inline" | "writer", env: Record<string, string> = {}, git = true) {
	useMemorySettings(t);
	const dir = tempDir("jhon-wguard-agent-");
	const repo = tempDir("jhon-wguard-repo-");
	// Outside a repository route-guard lets code edits run, so their results count (T117e).
	if (git) spawnSync("git", ["init", "-q"], { cwd: repo });
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
	const call = (toolName: string, input: Record<string, unknown>) => fire("tool_call", { toolName, input }, { cwd: repo, hasUI: false });
	let n = 0;
	// A call pi runs: its tool_call, then (when let through) its result.
	const run = async (toolName: string, input: Record<string, unknown>, isError = false) => {
		const toolCallId = `c${++n}`;
		const r = await fire("tool_call", { toolName, input, toolCallId }, { cwd: repo, hasUI: false });
		if (!(r && "block" in r)) await fire("tool_result", { toolName, input, toolCallId, isError, content: [] }, { cwd: repo, ui: { notify() {} } });
		return r;
	};
	const owner = (source = "interactive") => fire("input", { type: "input", text: "next", source }, { cwd: repo });
	return { repo, call, run, owner, fire };
}

const block = (n: number, tag = "x") => Array.from({ length: n }, (_, i) => `${tag}${i}`).join("\n") + "\n";
const writerBlock = (r: any) => (r && "block" in r && /writer mode/.test(r.reason) ? r.reason as string : undefined);

test("writer mode: an edit changing 19 lines passes, one changing 21 is refused toward a jhon-builder", async (t) => {
	const s = await session(t, "writer");
	writeFileSync(join(s.repo, "a.ts"), block(30, "old"));
	assert.equal(writerBlock(await s.call("edit", { path: "a.ts", edits: [{ oldText: block(19, "old"), newText: block(19, "new") }] })), undefined);
	await s.owner(); // T117e: a new request, so the next call is judged on its own.
	const why = writerBlock(await s.call("edit", { path: "a.ts", edits: [{ oldText: block(21, "old"), newText: block(21, "new") }] }));
	assert.ok(why, "refused");
	assert.match(why!, /changes 21 lines of a\.ts, more than the 20/);
	assert.match(why!, /jhon-builder \(subagent\) with a self-contained brief/);
	// The replacements of one call add up: 11 + 11 lines.
	assert.ok(writerBlock(await s.call("edit", { path: "a.ts", edits: [{ oldText: block(11, "old"), newText: block(11, "p") }, { oldText: block(11, "q"), newText: block(11, "r") }] })));
});

test("writer mode: a write counts its new lines against the file's current content", async (t) => {
	const s = await session(t, "writer");
	assert.ok(writerBlock(await s.call("write", { path: "new.ts", content: block(21) })), "a new 21-line file is refused");
	assert.equal(writerBlock(await s.call("write", { path: "new2.ts", content: block(19) })), undefined);
	await s.owner();
	writeFileSync(join(s.repo, "b.ts"), block(40));
	assert.equal(writerBlock(await s.call("write", { path: "b.ts", content: block(40).replace("x5\n", "y5\n") })), undefined, "one changed line of a long file passes");
});

test("writer mode: the plan doc, the Log and anything under odd/ always pass", async (t) => {
	const s = await session(t, "writer");
	mkdirSync(join(s.repo, "odd", "tasks"), { recursive: true });
	assert.equal(writerBlock(await s.call("write", { path: "odd/tasks/plan.md", content: block(80) })), undefined);
	assert.equal(writerBlock(await s.call("write", { path: join(s.repo, "odd", "notes.md"), content: block(80) })), undefined);
	assert.ok(writerBlock(await s.call("write", { path: "notodd/plan.md", content: block(80) })), "only odd/ itself");
});

test("inline mode and a child are not limited", async (t) => {
	const inline = await session(t, "inline");
	assert.equal(writerBlock(await inline.call("write", { path: "big.ts", content: block(80) })), undefined);
	const kid = await session(t, "writer", { JHON_PI_CHILD: "1" });
	assert.equal(writerBlock(await kid.call("write", { path: "big.ts", content: block(80) })), undefined);
});

// T117e (L746): the lines add up over the calls since the owner's last message.
test("writer mode: three 8-line edits in one request, the third is refused naming the total", async (t) => {
	const s = await session(t, "writer", {}, false);
	writeFileSync(join(s.repo, "c.ts"), block(40, "old"));
	assert.equal(writerBlock(await s.run("edit", { path: "c.ts", edits: [{ oldText: block(8, "old"), newText: block(8, "a") }] })), undefined);
	assert.equal(writerBlock(await s.run("edit", { path: "c.ts", edits: [{ oldText: block(8, "a"), newText: block(8, "b") }] })), undefined);
	const why = writerBlock(await s.run("edit", { path: "c.ts", edits: [{ oldText: block(8, "b"), newText: block(8, "c") }] }));
	assert.ok(why, "the third is refused");
	assert.match(why!, /changes 8 lines of c\.ts, 24 with the 16 already written inline since the owner's last message, more than the 20/);
	assert.match(why!, /Hand the rest to a jhon-builder/);
});

test("writer mode: a new owner message resets the count; a notice, a sub-agent completion or a follow-up does not", async (t) => {
	const s = await session(t, "writer", {}, false);
	writeFileSync(join(s.repo, "d.ts"), block(40, "old"));
	const edit8 = () => s.run("edit", { path: "d.ts", edits: [{ oldText: block(8, "old"), newText: block(8, "new") }] });
	assert.equal(writerBlock(await edit8()), undefined);
	assert.equal(writerBlock(await edit8()), undefined);
	// Messages the extensions send (notices, completions, follow-ups) come with source "extension".
	await s.fire("input", { type: "input", text: "agent done", source: "extension", streamingBehavior: "followUp" }, { cwd: s.repo });
	await s.fire("before_agent_start", { systemPromptOptions: { appendSystemPrompt: "", sections: {} } }, { cwd: s.repo, ui: { notify() {} } });
	assert.ok(writerBlock(await edit8()), "still 16 + 8");
	await s.owner();
	assert.equal(writerBlock(await edit8()), undefined, "the owner's message starts a new count");
	await s.owner("rpc");
	assert.equal(writerBlock(await edit8()), undefined);
});

test("writer mode: odd/** is not counted, and a failed call is taken back", async (t) => {
	const s = await session(t, "writer", {}, false);
	mkdirSync(join(s.repo, "odd"), { recursive: true });
	assert.equal(writerBlock(await s.run("write", { path: "odd/plan.md", content: block(80) })), undefined);
	assert.equal(writerBlock(await s.run("write", { path: "e.ts", content: block(15) }, true)), undefined);
	assert.equal(writerBlock(await s.run("write", { path: "f.ts", content: block(20) })), undefined, "neither odd/ nor the failed write counted");
	assert.ok(writerBlock(await s.run("write", { path: "g.ts", content: block(1) })), "20 + 1");
	// A call another guard refuses never ran, so it is taken back too.
	await s.owner();
	assert.equal((await s.run("write", { path: "/opt/none/x.ts", content: block(15) }) as any)?.block, true, "a protected path is refused");
	assert.equal(writerBlock(await s.run("write", { path: "h.ts", content: block(20) })), undefined);
});
