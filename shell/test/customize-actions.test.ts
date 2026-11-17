/**
 * T15.18 (L198): Customize's action rows, as G's. Appearance ends with Reset,
 * which puts every shell.appearance.* key back to its default after an
 * inline confirmation (enter holds it, a second enter applies, esc drops),
 * and nothing else; Keys ends with Reload now, which reloads pi so changed
 * keys apply without a restart. An action row is never edited in place.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { SETTINGS_DEFAULTS } from "../lib/settings.ts";
import { customizeRows } from "../lib/ui/customize/registry.ts";
import { SettingsView, type SettingRow } from "../lib/ui/settings-view.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { memorySettings } from "./fake-jhon-settings.ts";

const noop = () => {};
const plain = { fg: (_c: string, t: string) => t, bold: (t: string) => t };
const ENTER = "\r";
const ESC = "\x1b";
const DOWN = "\x1b[B";

const APPEARANCE = Object.keys(SETTINGS_DEFAULTS).filter((k) => k.startsWith("shell.appearance."));

test("Appearance ends with Reset: it puts every shell.appearance.* key back to its default and applies them, nothing else", () => {
	const changed = { "shell.appearance.animations": "off", "shell.appearance.ascii": true, "shell.appearance.icons": "emoji", "shell.appearance.status": "hidden", "shell.appearance.cards": "neon", "shell.appearance.surface": "tinted", "shell.appearance.density": "compact", "shell.keys.panel": "alt+p", "shell.behaviour.compact_at": 70 } as const;
	const settings = memorySettings({ ...changed });
	const live: string[] = [];
	const rows = customizeRows({ settings, animations: (p) => live.push(`animations ${p}`), doubleEsc: noop, status: () => live.push("status"), appearance: () => live.push("appearance"), redraw: noop });
	const appearance = rows.filter((r) => r.group === "Appearance");
	const reset = appearance.at(-1)!;
	assert.equal(reset.label, "Reset");
	assert.equal(reset.action, true);
	assert.equal(reset.confirm, true);
	assert.equal(reset.options, undefined);
	assert.equal(reset.apply!(reset.value()), undefined);
	for (const key of APPEARANCE) assert.deepEqual(settings.get(key as never), SETTINGS_DEFAULTS[key], key);
	assert.equal(settings.get("shell.keys.panel"), "alt+p", "a key is not appearance");
	assert.equal(settings.get("shell.behaviour.compact_at"), 70);
	assert.deepEqual(live, ["animations quality", "appearance", "status"]);
});

test("Keys ends with Reload now when pi can reload; it reloads once; without the hook no row", () => {
	let reloads = 0;
	const hooks = { settings: memorySettings(), animations: noop, doubleEsc: noop, status: noop, redraw: noop };
	assert.ok(!customizeRows(hooks).some((r) => r.label === "Reload now"));
	const keys = customizeRows({ ...hooks, reload: () => { reloads++; } }).filter((r) => r.group === "Keys");
	const reload = keys.at(-1)!;
	assert.equal(reload.label, "Reload now");
	assert.equal(reload.action, true);
	assert.ok(!reload.confirm);
	assert.match(reload.note!, /without restarting pi/);
	assert.equal(reload.apply!(reload.value()), undefined);
	assert.equal(reloads, 1);
});

function actionView() {
	const applied: string[] = [];
	const row = (label: string, confirm: boolean): SettingRow => ({
		group: "Appearance", key: label, label, value: () => "to defaults", action: true, confirm,
		apply: (v) => { applied.push(`${label} ${v}`); return undefined; },
	});
	let closed = 0;
	const view = new SettingsView({ rows: [row("Reset appearance", true), row("Reload now", false)], theme: plain, height: () => 30, group: "appearance", onClose: () => { closed++; }, requestRender() {} });
	return { view, applied, closed: () => closed, text: () => view.render(80).map(stripAnsi).join("\n") };
}

test("an action row with confirm: enter holds it (· enter applies), esc drops it, a second enter applies it once", () => {
	const v = actionView();
	v.view.handleInput(ENTER);
	assert.deepEqual(v.applied, []);
	assert.match(v.text(), /▸ Reset appearance +to defaults · enter applies/);
	assert.match(v.text(), /Reset appearance — to defaults: enter applies, esc drops it/);
	v.view.handleInput(ESC);
	assert.equal(v.closed(), 0, "esc drops the pending reset, the overlay stays");
	assert.doesNotMatch(v.text(), /enter applies/);
	v.view.handleInput(ENTER);
	v.view.handleInput(ENTER);
	assert.deepEqual(v.applied, ["Reset appearance to defaults"]);
	assert.doesNotMatch(v.text(), /enter applies|Reset appearance:/, "no edit field opened");
});

test("an action row without confirm applies on enter at once, never opening an edit field", () => {
	const v = actionView();
	v.view.handleInput(DOWN);
	v.view.handleInput(ENTER);
	assert.deepEqual(v.applied, ["Reload now to defaults"]);
	assert.doesNotMatch(v.text(), /Enter confirm · Esc cancel/);
});
