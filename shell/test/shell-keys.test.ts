/**
 * T174-S5b (L1493-L1495): every shell key is a setting and a Customize → Keys
 * row: LDD status (alt+j), Agents (alt+s), Jobs (alt+o, new), Usage (alt+u)
 * and Todos (alt+t). Each row refuses a key pi already uses. alt+o, the jobs
 * key, opens /jhon:jobs: pi binds no alt+o (customize/keys.ts PI_KEYS, on any
 * platform) and the shell's other keys (alt+a and alt+e select all, alt+j,
 * alt+s, alt+t, alt+u) do not take it.
 *
 *   cd shell && node --experimental-strip-types --test test/shell-keys.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { tmpdir } from "node:os";
import { customizeRows } from "../lib/ui/customize/registry.ts";
import { KEYS, piReservedAction } from "../lib/ui/customize/keys.ts";
import { registerBackgroundTasks, jobsKey } from "../lib/background-tasks-tools.ts";
import { SETTINGS_DEFAULTS } from "../lib/settings.ts";
import { envOverrides, memorySettings } from "./fake-jhon-settings.ts";

const noop = () => {};
const envSettings = (env: NodeJS.ProcessEnv) => memorySettings({}, undefined, envOverrides(env));

test("the shell's keys and their defaults: panel alt+j, agents alt+s, jobs alt+o, usage alt+u, todos alt+t", () => {
	const keys = Object.entries(SETTINGS_DEFAULTS).filter(([key]) => key.startsWith("shell.keys."));
	assert.deepEqual(keys, [["shell.keys.panel", "alt+j"], ["shell.keys.agents", "alt+s"], ["shell.keys.jobs", "alt+o"], ["shell.keys.usage", "alt+u"], ["shell.keys.todos", "alt+t"]]);
	assert.equal(piReservedAction("alt+o", false), undefined, "pi binds no alt+o");
	assert.equal(piReservedAction("alt+o", true), undefined, "nor on Windows or WSL");
});

test("Customize → Keys has one row per key, as before T174: LDD status, Agents, Jobs, Usage, Todos, then Reload now", () => {
	assert.deepEqual(KEYS.rows.map((row) => [row.key, row.label]), [
		["shell.keys.panel", "LDD status"], ["shell.keys.agents", "Agents"], ["shell.keys.jobs", "Jobs"],
		["shell.keys.usage", "Usage"], ["shell.keys.todos", "Todos"], ["reload", "Reload now"],
	]);
	assert.equal(KEYS.summary, "The shell's keys: LDD status, agents, jobs, usage, todos; reload");
	const settings = memorySettings();
	const rows = customizeRows({ settings, animations: noop, doubleEsc: noop, status: noop, redraw: noop }).filter((row) => row.group === "Keys" && !row.action);
	const preview = (key: string) => rows.find((row) => row.key === key)!.preview!(String(settings.get(key as never))).lines;
	assert.deepEqual(preview("shell.keys.jobs"), ["alt+o opens the Jobs view."]);
	assert.deepEqual(preview("shell.keys.usage"), ["alt+u opens the subscription usage."]);
	assert.deepEqual(preview("shell.keys.todos"), ["alt+t folds the todo list."]);
});

test("each new row keeps the refusal of pi's keys and writes nothing; a free key saves", async () => {
	const settings = memorySettings();
	const rows = customizeRows({ settings, animations: noop, doubleEsc: noop, status: noop, redraw: noop }).filter((row) => row.group === "Keys" && !row.action);
	for (const key of ["shell.keys.jobs", "shell.keys.usage", "shell.keys.todos"]) {
		const row = rows.find((r) => r.key === key)!;
		assert.equal(await row.apply!("alt+y"), `alt+y is pi's key for "Cycle through deleted text after yank" (tui.editor.yankPop); pick another key.`, key);
	}
	assert.deepEqual(settings.jhon.sets, [], "no jhon settings set ran");
	assert.equal(await rows.find((r) => r.key === "shell.keys.jobs")!.apply!("alt+w"), undefined);
	assert.deepEqual(settings.jhon.sets, [["shell.keys.jobs", "alt+w"]]);
});

test("the jobs key (shell.keys.jobs) opens /jhon:jobs; JHON_SHELL_JOBS_KEY sets another or turns it off", async () => {
	assert.equal(jobsKey(envSettings({})), "alt+o");
	assert.equal(jobsKey(envSettings({ JHON_SHELL_JOBS_KEY: "ctrl+shift+j" })), "ctrl+shift+j");
	assert.equal(jobsKey(envSettings({ JHON_SHELL_JOBS_KEY: "off" })), undefined);
	const register = (env: NodeJS.ProcessEnv) => {
		const shortcuts = new Map<string, { description: string; handler(ctx: unknown): Promise<void> }>();
		const pi = { registerTool: noop, registerCommand: noop, registerShortcut: (k: string, s: never) => shortcuts.set(k, s), registerMessageRenderer: noop, on: noop, sendMessage: noop, appendEntry: noop, events: { emit: noop, on: () => noop } };
		registerBackgroundTasks(pi, { env, settings: envSettings(env), outputDir: () => tmpdir() });
		return shortcuts;
	};
	const shortcuts = register({});
	assert.deepEqual([...shortcuts.keys()], ["alt+s", "alt+o"]);
	let noted = "";
	await shortcuts.get("alt+o")!.handler({ hasUI: true, mode: "rpc", sessionManager: { getSessionId: () => "main" }, ui: { notify: (m: string) => { noted = m; } } });
	assert.equal(noted, "The Jobs view needs TUI mode.", "alt+o asks for the Jobs view");
	assert.deepEqual([...register({ JHON_SHELL_JOBS_KEY: "off" }).keys()], ["alt+s"]);
});
