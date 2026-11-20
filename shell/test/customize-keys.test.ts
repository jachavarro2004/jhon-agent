/**
 * T165 (L1436, L1438): Customize → Keys refuses a key pi 1.5.2 already binds
 * in the editor, the app or the fullscreen transcript, and names pi's action;
 * nothing is written. Any other key, and off, saves as before.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { customizeRows } from "../lib/ui/customize/registry.ts";
import { piReservedAction } from "../lib/ui/customize/keys.ts";
import { memorySettings } from "./fake-jhon-settings.ts";

const noop = () => {};
const KEY_SETTINGS = ["shell.keys.panel", "shell.keys.rail", "shell.keys.todos", "shell.keys.tasks", "shell.keys.changes", "shell.keys.commands"];

function keyRows() {
	const settings = memorySettings();
	const rows = customizeRows({ settings, animations: noop, doubleEsc: noop, status: noop, redraw: noop }).filter((r) => r.group === "Keys" && !r.action);
	return { settings, rows };
}

test("every Keys row refuses pi's ctrl+shift+up, naming pi's action, and writes nothing", async () => {
	const { settings, rows } = keyRows();
	assert.deepEqual(rows.map((r) => r.key), KEY_SETTINGS);
	for (const row of rows) {
		const before = settings.get(row.key);
		const result = await row.apply!("ctrl+shift+up");
		assert.equal(result, `ctrl+shift+up is pi's key for "Jump to the previous marked message" (tui.altScreen.previousPrompt); pick another key.`, row.key);
		assert.equal(settings.get(row.key), before, `${row.key} unchanged`);
	}
	assert.deepEqual(settings.jhon.sets, [], "no jhon settings set ran");
});

test("a Keys row refuses pi's editor and app keys, whatever their case or modifier order", async () => {
	const { settings, rows } = keyRows();
	const todos = rows.find((r) => r.key === "shell.keys.todos")!;
	assert.equal(await todos.apply!("ctrl+o"), `ctrl+o is pi's key for "Collapse or expand tool output" (app.tools.expand); pick another key.`);
	assert.equal(await todos.apply!("Shift+Ctrl+Down"), `Shift+Ctrl+Down is pi's key for "Jump to the next marked message" (tui.altScreen.nextPrompt); pick another key.`);
	assert.equal(await todos.apply!(" alt+b "), `alt+b is pi's key for "Move cursor word left" (tui.editor.cursorWordLeft); pick another key.`);
	assert.equal(await todos.apply!("ctrl+k"), `ctrl+k is pi's key for "Delete to line end" (tui.editor.deleteToLineEnd); pick another key.`);
	assert.equal(settings.get("shell.keys.todos"), "alt+t");
	assert.deepEqual(settings.jhon.sets, []);
});

test("a free key and off still save through jhon settings set (preserved)", async () => {
	const { settings, rows } = keyRows();
	const todos = rows.find((r) => r.key === "shell.keys.todos")!;
	assert.equal(await todos.apply!("alt+x"), undefined);
	assert.equal(settings.get("shell.keys.todos"), "alt+x");
	assert.equal(await todos.apply!("off"), undefined);
	assert.equal(settings.get("shell.keys.todos"), "off");
	assert.deepEqual(settings.jhon.sets, [["shell.keys.todos", "alt+x"], ["shell.keys.todos", "off"]]);
});

test("piReservedAction reads pi 1.5.2's defaults, Windows and WSL variants apart", () => {
	assert.deepEqual(piReservedAction("ctrl+shift+up", false), { id: "tui.altScreen.previousPrompt", description: "Jump to the previous marked message" });
	assert.deepEqual(piReservedAction("ctrl+up", false)?.id, "tui.altScreen.previousPrompt", "pi 1.5.2 binds ctrl+up beside ctrl+shift+up off Windows too");
	assert.equal(piReservedAction("ctrl+shift+p", false)?.id, "app.model.cycleBackward");
	assert.equal(piReservedAction("alt+p", false), undefined);
	assert.equal(piReservedAction("alt+p", true)?.id, "app.model.cycleBackward");
	assert.equal(piReservedAction("alt+enter", false)?.id, "app.message.followUp");
	assert.equal(piReservedAction("alt+up", false)?.id, "app.message.dequeue");
	assert.equal(piReservedAction("ctrl+shift+f", false)?.id, "tui.altScreen.search");
	assert.equal(piReservedAction("pageUp", false)?.id, "tui.altScreen.pageUp");
	for (const free of ["alt+pageUp", "alt+pageDown", "alt+j", "alt+s", "alt+t", "alt+g", "alt+r", "alt+k", "off", ""]) {
		assert.equal(piReservedAction(free, false), undefined, free);
		assert.equal(piReservedAction(free, true), undefined, `${free} on Windows`);
	}
});
