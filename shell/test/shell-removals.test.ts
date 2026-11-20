/**
 * T174-S1 (L1462, L1465, L1469): the shell loses the changes view, the usage
 * view, the integrations section and the todo card and keys. The agent's
 * `todo` tool stays; its list shows in the right pane only while no LDD
 * change is active. The three keys those views had are retired with a notice.
 *
 *   jhon test
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { Component } from "@earendil-works/pi-tui";
import jhonShellUi from "../extensions/ui.ts";
import jhonTodo from "../extensions/todo.ts";
import { sidebarState } from "../lib/ui/shell-sidebar.ts";
import { lddFromStatus, selectChange, shareSelection } from "../lib/ui/ldd-status.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { KEYS } from "../lib/ui/customize/keys.ts";
import { jhonSettingsBackend, SETTINGS_DEFAULTS, ShellSettings } from "../lib/settings.ts";

type Handler = (event: unknown, ctx: ExtensionContext) => unknown;

/** A pi that records what an extension registers. */
function recordingPi() {
	const handlers = new Map<string, Handler[]>();
	const commands = new Set<string>();
	const shortcuts = new Set<string>();
	const tools = new Map<string, { execute(id: string, params: unknown, signal: undefined, update: undefined, ctx: ExtensionContext): Promise<unknown> }>();
	const pi = {
		on(event: string, handler: Handler) { handlers.set(event, [...(handlers.get(event) ?? []), handler]); },
		appendEntry() {},
		events: { on() { return () => {}; }, emit() {} },
		registerTool(tool: { name: string }) { tools.set(tool.name, tool as never); },
		registerCommand(name: string) { commands.add(name); },
		registerShortcut(key: string) { shortcuts.add(key); },
		registerMessageRenderer() {},
		sendMessage() {},
		getThinkingLevel: () => "medium",
	} as unknown as ExtensionAPI;
	const fire = async (event: string, ctx: ExtensionContext, payload: unknown = {}) => {
		for (const handler of handlers.get(event) ?? []) await handler(payload, ctx);
	};
	return { pi, commands, shortcuts, tools, fire };
}

test("/jhon:changes and /jhon:usage are no longer registered, nor alt+g, alt+u, alt+t or the todo's scroll keys", () => {
	const { pi, commands, shortcuts, tools } = recordingPi();
	jhonShellUi(pi, { JHON_SHELL: "1" });
	jhonTodo(pi, {});
	for (const gone of ["jhon:changes", "jhon:usage"]) assert.ok(!commands.has(gone), `${gone} is still registered`);
	for (const gone of ["alt+g", "alt+u", "alt+t", "alt+pageUp", "alt+pageDown"]) assert.ok(!shortcuts.has(gone), `${gone} is still registered`);
	assert.ok(commands.has("jhon:customize"), "the UI still registers its own commands");
	assert.ok(tools.has("todo"), "the agent's todo tool stays");
	assert.ok(tools.has("session_worktree_register"), "child registration stays");
});

test("Customize's Keys lists the LDD status and Agents alone; the shell keeps no default for the retired keys", () => {
	assert.deepEqual(KEYS.rows.map((row) => row.key).filter((key) => key.startsWith("shell.keys.")), ["shell.keys.panel", "shell.keys.agents"]);
	for (const gone of ["shell.keys.todos", "shell.keys.changes", "shell.keys.usage"]) assert.ok(!(gone in SETTINGS_DEFAULTS), gone);
});

test("each retired key reaches the shell with its notice: the engine reads it until v0.4 and says why", (t) => {
	const bin = process.env.JHON_TEST_BIN || join(dirname(fileURLToPath(import.meta.url)), "..", "..", "build", "jhon");
	const home = mkdtempSync(join(tmpdir(), "jhon-shell-removals-"));
	const file = join(home, "settings.toml");
	writeFileSync(file, '[sandbox]\nmode = "off"\n\n[shell.keys]\nchanges = "alt+g"\nusage = "alt+u"\ntodos = "alt+t"\n');
	const saved = process.env.JHON_SETTINGS;
	process.env.JHON_SETTINGS = file;
	t.after(() => { process.env.JHON_SETTINGS = saved; rmSync(home, { recursive: true, force: true }); });
	const settings = new ShellSettings(jhonSettingsBackend(bin));
	assert.equal(settings.loaded, true, "a file with the retired keys still loads");
	const why = Object.fromEntries(settings.retired.map((r) => [r.key, r.why]));
	assert.deepEqual(why, {
		"shell.keys.changes": "the changes view (/jhon:changes) was removed from the shell",
		"shell.keys.usage": "the usage view (/jhon:usage) was removed from the shell; the dashboard has the stats",
		"shell.keys.todos": "the todo card and its keys were removed; the agent's list shows in the right pane while no LDD change is active",
	});
	assert.ok(settings.retired.every((r) => !r.silent), "the owner is told of each one");
});

const CHANGE = lddFromStatus("rail-redesign", { route: { level: "verified" }, next: "apply", tasks: { total: 1, completed: 0, slice: "Slice 1: rail", slices: [{ name: "Slice 1: rail", total: 1, completed: 0, tasks: [{ text: "Draw rows", done: false }] }] } });
const plainTheme = { fg: (_role: string, text: string) => text, bold: (text: string) => text };

test("the todo list shows in the right pane while no LDD change is active, and hides while one is", async (t) => {
	const { pi, tools, fire } = recordingPi();
	jhonTodo(pi, {});
	const widgets = new Map<string, (tui: unknown, theme: unknown) => Component>();
	const ctx = {
		hasUI: true,
		sessionManager: { getSessionId: () => "s1", getBranch: () => [] },
		ui: { setWidget(key: string, content: ((tui: unknown, theme: unknown) => Component) | undefined) { if (content) widgets.set(key, content); else widgets.delete(key); } },
	} as unknown as ExtensionContext;
	await fire("session_start", ctx);
	await tools.get("todo")!.execute("w", { action: "write", tasks: [{ title: "write the test", status: "in_progress" }, { title: "fix tsc" }] }, undefined, undefined, ctx);
	await fire("tool_execution_end", ctx, { toolName: "todo" });
	const tui = { requestRender() {}, terminal: { rows: 40, columns: 180 } };
	const [factory] = [...widgets.values()];
	assert.ok(factory, "the todo extension mounts its right-pane section");
	const below = factory!(tui, plainTheme);
	assert.deepEqual(below.render(100), [], "nothing above or below the prompt: the list lives in the right pane only");
	const rail = () => sidebarState(tui as never).parts.get("todo")!.render(48).map(stripAnsi);
	const shown = rail();
	assert.match(shown[0]!, /^▾ TODOS +0\/2$/);
	assert.deepEqual(shown.slice(1).map((row) => row.trim()), ["◐ write the test", "· fix tsc"]);
	const withdraw = shareSelection("s1", async () => CHANGE);
	t.after(withdraw);
	selectChange("s1", CHANGE);
	assert.deepEqual(rail(), [], "with an LDD change the LDD section rules and the list is hidden");
	selectChange("s1", undefined);
	assert.deepEqual(rail(), shown, "the change gone, the list is back");
});
