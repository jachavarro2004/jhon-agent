/**
 * Tool-call icons (T33, L97): every tool call's header starts with the icon of
 * its family, like the Gentle Shell beside Jhon's pi; memory calls read as one
 * line and their result as `↳ ✓ <summary>`. JHON_SHELL_ASCII=1 trades the
 * icons for plain letters.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { TOOL_FAMILY, memoryToolRenderers, toolFamily, toolIcon, withToolIcon } from "../lib/ui/tool-icons.ts";
import { envOverrides, memorySettings } from "./fake-jhon-settings.ts";

// The settings `jhon settings` would give the shell under an environment (S38).
const envSettings = (env: NodeJS.ProcessEnv) => memorySettings({}, undefined, envOverrides(env));
import { stripAnsi } from "../lib/ui/terminal-theme.ts";

const tagged = { fg: (c: string, t: string) => `<${c}>${t}</${c}>`, bold: (t: string) => `<b>${t}</b>` };
const plain = { fg: (_c: string, t: string) => t, bold: (t: string) => t };
const lines = (component: { render(width: number): string[] }, width = 80) => component.render(width).map((line) => stripAnsi(line).trimEnd());

test("each tool falls in its family, MCP tools by their server and tool", () => {
	const cases: Array<[string, string]> = [
		["mem_save", TOOL_FAMILY.MEMORY], ["mcp__engram__mem_search", TOOL_FAMILY.MEMORY], ["mcp__engram__mem_context", TOOL_FAMILY.MEMORY],
		["grep", TOOL_FAMILY.SEARCH], ["find", TOOL_FAMILY.SEARCH], ["mcp__codegraph__codegraph_explore", TOOL_FAMILY.SEARCH],
		["web_search", TOOL_FAMILY.WEB], ["fetch_content", TOOL_FAMILY.WEB],
		["bash", TOOL_FAMILY.SHELL],
		["edit", TOOL_FAMILY.EDIT], ["write", TOOL_FAMILY.EDIT],
		["read", TOOL_FAMILY.READ],
		["subagent", TOOL_FAMILY.AGENTS], ["subagent_continue", TOOL_FAMILY.AGENTS],
		["ask_user", TOOL_FAMILY.ASK], ["ask_user_question", TOOL_FAMILY.ASK], ["ask_user_choice", TOOL_FAMILY.ASK],
		["ls", TOOL_FAMILY.OTHER], ["todo", TOOL_FAMILY.OTHER], ["mcp__jhon__status", TOOL_FAMILY.OTHER],
	];
	for (const [name, family] of cases) assert.equal(toolFamily(name), family, name);
});

test("shell.appearance.icons = emoji: 🧠 🔎 🌐 $ ✎ 📄 🤖 ❓ and · for anything else", () => {
	const env = memorySettings({ "shell.appearance.icons": "emoji" });
	assert.deepEqual(
		["mem_save", "grep", "web_search", "bash", "edit", "read", "subagent", "ask_user", "ls"].map((name) => toolIcon(name, env)),
		["🧠", "🔎", "🌐", "$", "✎", "📄", "🤖", "❓", "·"],
	);
});

test("the geometric style is the default (T15.3, the design system's): ▤ ⌕ ◍ $ ✎ ▢ ◇ ? ·; ASCII wins over both", () => {
	const names = ["read", "grep", "web_search", "bash", "edit", "mem_save", "subagent", "ask_user", "ls"];
	assert.deepEqual(names.map((name) => toolIcon(name, envSettings({}))), ["▤", "⌕", "◍", "$", "✎", "▢", "◇", "?", "·"]);
	assert.equal(toolIcon("read", envSettings({ JHON_SHELL_ASCII: "1" })), "r");
	assert.equal(toolIcon("read", memorySettings({ "shell.appearance.icons": "emoji", "shell.appearance.ascii": true })), "r");
});

test("under JHON_SHELL_ASCII=1 every icon is a plain letter", () => {
	const env = envSettings({ JHON_SHELL_ASCII: "1" });
	const icons = ["mem_save", "grep", "web_search", "bash", "edit", "read", "subagent", "ask_user", "ls"].map((name) => toolIcon(name, env));
	assert.deepEqual(icons, ["m", "s", "w", "$", "e", "r", "a", "?", "-"]);
	for (const icon of icons) assert.match(icon, /^[\x20-\x7e]$/);
	assert.equal(toolIcon("read", envSettings({ JHON_SHELL_ASCII: "0" })), "▤", "only 1 turns it on");
});

test("a wrapped call puts the icon before the tool's own header, inside the width the icon takes", () => {
	const inner = { render: (width: number) => [`${"x".repeat(width)}`, "second"], invalidate() {} };
	const renderers = withToolIcon("web_search", { renderCall: () => inner, renderResult: undefined }, envSettings({}))!;
	const component = renderers.renderCall!({}, plain as any, { lastComponent: undefined } as any);
	const rows = component.render(20);
	assert.equal(rows[0], `◍ ${"x".repeat(18)}`);
	assert.equal(rows[1], "  second", "continuation rows keep the header's column");
	for (const row of rows) assert.ok(visibleWidth(row) <= 20, JSON.stringify(row));
	assert.deepEqual(component.render(2).every((row) => visibleWidth(row) <= 2), true);
});

test("a wrapped call hands the tool its own last component back, and keeps the other renderers", () => {
	const seen: unknown[] = [];
	const own = { render: () => ["web_search query"], invalidate() {} };
	const renderResult = () => own;
	const renderers = withToolIcon("web_search", { renderShell: "default", renderCall: (_a: unknown, _t: unknown, context: any) => { seen.push(context.lastComponent); return own; }, renderResult }, envSettings({}))!;
	const first = renderers.renderCall!({}, plain as any, { lastComponent: undefined } as any);
	renderers.renderCall!({}, plain as any, { lastComponent: first } as any);
	assert.deepEqual(seen, [undefined, own]);
	assert.equal(renderers.renderResult, renderResult);
	assert.equal(renderers.renderShell, "default");
});

test("a tool without a call renderer gets pi's header with the icon: bold name, then its arguments", () => {
	const renderers = withToolIcon("subagent", { renderCall: undefined }, envSettings({}))!;
	const row = renderers.renderCall!({ agent: "worker", task: "build" }, tagged as any, { lastComponent: undefined, expanded: false } as any).render(200)[0]!;
	assert.match(row, /^<accent>◇<\/accent> <toolTitle><b>subagent<\/b><\/toolTitle> <muted>agent="worker" task="build"<\/muted>/);
	assert.equal(withToolIcon("subagent", undefined, envSettings({})), undefined, "an unknown tool keeps pi's own fallback");
	const bash = { renderCall: () => ({ render: () => ["$ ls"], invalidate() {} }) };
	assert.equal(withToolIcon("bash", bash, envSettings({})), bash, "pi's bash header already starts with its $ prompt");
});

test("the memory line: ▢, the action in bold and its argument; the result ↳ ✓ with a short summary", () => {
	const r = memoryToolRenderers("mcp__engram__mem_save", envSettings({}));
	const call = r.renderCall!({ title: "Watched movies always get a watch log; backfill missing ones", type: "decision" }, tagged as any, {} as any);
	// Tagged roles count as columns here, so Text pads the row past the width: drop the pad.
	const head = call.render(300)[0]!.replace(/ *(?:\x1b\[0m)?$/, "");
	assert.match(head, /^<accent>▢<\/accent> <toolTitle><b>save<\/b><\/toolTitle> <muted>“Watched movies always get a watch log; backfill…”<\/muted>$/);
	const saved = { content: [{ type: "text", text: "Memory saved: #2476\nmore detail" }] } as any;
	assert.deepEqual(lines(r.renderResult!(saved, { expanded: false, isPartial: false }, plain as any, { isError: false } as any)), ["↳ ✓ Memory saved: #2476"]);
	assert.match(r.renderResult!(saved, { expanded: false, isPartial: false }, tagged as any, { isError: false } as any).render(200)[0]!, /^<dim>↳<\/dim> <success>✓<\/success> <muted>Memory saved: #2476<\/muted>/);
	const bad = r.renderResult!({ content: [{ type: "text", text: "engram is offline" }] } as any, { expanded: false, isPartial: false }, tagged as any, { isError: true } as any);
	assert.match(bad.render(200)[0]!, /^<error>↳ ✗ engram is offline<\/error>/);
	const open = r.renderResult!({ content: [{ type: "text", text: "Memory saved: #2476\nmore detail" }] } as any, { expanded: true, isPartial: false }, plain as any, { isError: false } as any);
	assert.deepEqual(lines(open), ["↳ ✓ Memory saved: #2476", "", "Memory saved: #2476", "more detail"]);
	const ids = memoryToolRenderers("mem_get_observation", envSettings({})).renderCall!({ id: 42 }, plain as any, {} as any);
	assert.deepEqual(lines(ids), ["▢ get observation #42"]);
	for (const width of [1, 2, 3, 10]) for (const row of call.render(width)) assert.ok(visibleWidth(row) <= width, `${width}: ${JSON.stringify(row)}`);
});
