/**
 * T115c (L707, L708): the orchestrator's fixed per-turn base, the system
 * sections and the tool definitions the shell controls, under pinned byte
 * budgets so it cannot creep back. The orchestrator's prose is the engine's.
 */
import { test, after } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import jhon, { CONFIG_FILE } from "../extensions/jhon.ts";
import { SESSION_SECTION } from "../lib/ui/append-system-prompt.ts";

const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
const temp = (p: string) => { const d = realpathSync(mkdtempSync(join(tmpdir(), p))); temps.push(d); return d; };

// The 50 tools the L689 session declared (L707), by name.
const ENGRAM_ALL = ["mem_save", "mem_update", "mem_delete", "mem_suggest_topic_key", "mem_search", "mem_context", "mem_timeline", "mem_get_observation", "mem_save_prompt", "mem_session_summary", "mem_session_start", "mem_session_end", "mem_stats", "mem_capture_passive", "mem_merge_projects", "mem_current_project", "mem_judge", "mem_judge_many", "mem_review", "mem_compare", "mem_doctor", "mem_conflicts", "mem_relations"].map((t) => `mcp__engram__${t}`);
const WEB = ["web_search", "fetch_content", "get_search_content", "source_check"];
const CORE = ["read", "bash", "edit", "write", "grep", "find", "ls", "inspect", "subagent", "subagent_continue", "ask_user", "todo", "task_list", "task_result", "task_stop", "bash_background", "monitor", "mcp__jhon__audit", "mcp__jhon__status", "tools_more"];
const KEPT_ENGRAM = ["mem_search", "mem_save", "mem_get_observation", "mem_context", "mem_judge", "mem_judge_many", "mem_session_summary"].map((t) => `mcp__engram__${t}`);

function load(env: Record<string, string> = {}, api: Record<string, unknown> = {}) {
	const dir = temp("jhon-budget-agent-");
	writeFileSync(join(dir, "orchestrator.md"), "# Orchestrator\n\nJhon prompt.");
	writeFileSync(join(dir, CONFIG_FILE), JSON.stringify({ jhon: "/nonexistent/jhon", orchestrator: join(dir, "orchestrator.md"), vcsDirs: [".git"], protected: [], inspectAllow: ["git diff"] }));
	const saved = { ...process.env };
	Object.assign(process.env, { PI_CODING_AGENT_DIR: dir, CLAUDE_CONFIG_DIR: join(dir, "claude"), ...env });
	const handlers = new Map<string, Array<(e: any, c: any) => any>>();
	const tools: any[] = [];
	try {
		jhon({ on: (n: string, h: any) => handlers.set(n, [...(handlers.get(n) ?? []), h]), registerTool: (t: any) => tools.push(t), registerCommand: () => {}, ...api });
	} finally {
		for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
		Object.assign(process.env, saved);
	}
	const fire = async (name: string, event: any, ctx: any) => { let r; for (const h of handlers.get(name) ?? []) r = (await h(event, ctx)) ?? r; return r; };
	return { dir, tools, fire };
}

const declared = (t: any) => JSON.stringify({ name: t.name, description: t.description, parameters: t.parameters });

test("the orchestrator declares only the tools it uses: the rest of Engram, the web tools and unindexed codegraph are deferred", async () => {
	let active = [...CORE, ...ENGRAM_ALL, ...WEB, "mcp__codegraph__codegraph_explore"];
	const { fire } = load({}, { getActiveTools: () => [...active], setActiveTools: (n: string[]) => { active = n; } });
	await fire("context", { messages: [] }, { cwd: temp("jhon-budget-cwd-") });
	assert.deepEqual(active, [...CORE, ...ENGRAM_ALL.filter((n) => KEPT_ENGRAM.includes(n))]);
});

// Measured at T115c: jhon.ts's own tools declare 8,027 bytes (inspect, subagent with its skills,
// ask_user, background tasks, m365_read, tools_more); the session prose ~450 plus its path.
const JHON_TOOLS_BUDGET = 8_300;
const SESSION_PROSE_BUDGET = 650;
const SKILL_DESCRIPTION_MAX = 200;

test("the shell's own tool definitions stay under their byte budget", () => {
	const { tools } = load();
	const bytes = tools.reduce((n, t) => n + declared(t).length, 0);
	assert.ok(tools.some((t) => t.name === "tools_more"));
	assert.ok(bytes <= JHON_TOOLS_BUDGET, `the shell's tools declare ${bytes} bytes, budget ${JHON_TOOLS_BUDGET}`);
});

test("the session section is an index under its prose budget, and the skills catalog is compact", async () => {
	const { fire } = load();
	const cwd = temp("jhon-budget-cwd-");
	const long = "Trigger: when asked. " + "A long description that goes on. ".repeat(40);
	const options = { appendSystemPrompt: "", sections: {} as Record<string, string>, skills: [{ name: "a", description: long, filePath: "/s/a/SKILL.md" }, { name: "b", description: "Short one.", filePath: "/s/b/SKILL.md" }] };
	await fire("before_agent_start", { systemPromptOptions: options }, { cwd });
	const section = options.sections[SESSION_SECTION];
	assert.match(section, /^## Project memory/);
	assert.match(section, /MEMORY\.md does not exist yet\./);
	assert.ok(section.length <= SESSION_PROSE_BUDGET, `the session section's prose is ${section.length} bytes, budget ${SESSION_PROSE_BUDGET}`);
	assert.ok(options.skills[0].description.length <= SKILL_DESCRIPTION_MAX, `${options.skills[0].description.length}`);
	assert.ok(options.skills[0].description.startsWith("Trigger: when asked."));
	assert.ok(options.skills[0].description.endsWith("…"));
	assert.equal(options.skills[1].description, "Short one.");
	// A child's catalog is the child's business (T115f).
	const kid = load({ JHON_PI_CHILD: "1" });
	const kidOptions = { appendSystemPrompt: "", skills: [{ name: "a", description: long, filePath: "/s/a/SKILL.md" }] };
	await kid.fire("before_agent_start", { systemPromptOptions: kidOptions }, { cwd });
	assert.equal(kidOptions.skills[0].description, long);
});
