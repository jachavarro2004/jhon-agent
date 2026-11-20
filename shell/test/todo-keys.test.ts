/**
 * T165 (L1436, L1438): the todo list scrolls on keys pi leaves free.
 * pi 1.5.2 binds ctrl+shift+up/down to tui.altScreen.previousPrompt/nextPrompt;
 * in fullscreen its transcript listener consumes them before the editor asks
 * the extensions, so the todo's shortcut never ran there.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { matchesKey } from "@earendil-works/pi-tui";
import jhonTodo, { TODO_SCROLL_KEYS } from "../extensions/todo.ts";
import { piReservedAction } from "../lib/ui/customize/keys.ts";

function registered(env: NodeJS.ProcessEnv): Map<string, string | undefined> {
	const keys = new Map<string, string | undefined>();
	const pi = {
		on() {},
		registerTool() {},
		registerShortcut(key: string, options: { description?: string }) { keys.set(key, options.description); },
		sendMessage() {},
	} as unknown as ExtensionAPI;
	jhonTodo(pi, env);
	return keys;
}

test("the todo scrolls on alt+pageUp/alt+pageDown, not on pi's ctrl+shift+up/down", () => {
	assert.deepEqual(TODO_SCROLL_KEYS, { up: "alt+pageUp", down: "alt+pageDown", hint: "alt+PgUp/PgDn" });
	const keys = registered({});
	assert.equal(keys.get("alt+pageUp"), "Scroll the plan up");
	assert.equal(keys.get("alt+pageDown"), "Scroll the plan down");
	assert.ok(!keys.has("ctrl+shift+up") && !keys.has("ctrl+shift+down"), `no pi-reserved scroll key: ${[...keys.keys()]}`);
	assert.deepEqual([...keys.keys()].sort(), ["alt+pageDown", "alt+pageUp", "alt+t"]);
});

test("no key the todo registers is one pi 1.5.2 binds, on any platform", () => {
	for (const key of registered({}).keys()) {
		assert.equal(piReservedAction(key, false), undefined, key);
		assert.equal(piReservedAction(key, true), undefined, `${key} on Windows`);
	}
});

test("pi's transcript page keys do not take alt+pageUp/alt+pageDown (CSI 5;3~ and 6;3~)", () => {
	for (const [data, key, plain] of [["\x1b[5;3~", "alt+pageUp", "pageUp"], ["\x1b[6;3~", "alt+pageDown", "pageDown"], ["\x1b[5;3~", "alt+pageUp", "ctrl+pageUp"]] as const) {
		assert.equal(matchesKey(data, key), true, `${JSON.stringify(data)} is ${key}`);
		assert.equal(matchesKey(data, plain), false, `${JSON.stringify(data)} is not ${plain}`);
	}
});

test("a child registers no scroll key", () => {
	assert.equal(registered({ JHON_PI_CHILD: "1", JHON_PI_TOOLS: "todo" }).size, 0);
});
