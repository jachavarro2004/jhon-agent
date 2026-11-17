/**
 * L93 (T17b): the notification events live once in the shell, in
 * lib/settings.ts, and equal the engine's (engine/internal/settings): the
 * same events, in the same order, with the same default sounds. The engine
 * is the tree's own binary, build/jhon, as the guard tests run it
 * (extension.test.ts); JHON_TEST_BIN names another.
 *
 *   node --experimental-strip-types --test test/notification-events-parity.test.ts
 */
import "./isolated-env.ts";
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import * as settings from "../lib/settings.ts";
import * as policy from "../lib/ui/notification-policy.ts";

test("notification-policy.ts reads the event list from settings.ts: one list in the shell", () => {
	assert.ok(Array.isArray(settings.NOTIFICATION_EVENTS), "settings.ts exports the list");
	assert.equal(policy.NOTIFICATION_EVENTS, settings.NOTIFICATION_EVENTS, "the very same list, not a copy");
});

test("the shell's default event sounds equal `jhon settings list --json`'s, in its order", (t) => {
	const bin = process.env.JHON_TEST_BIN || join(dirname(fileURLToPath(import.meta.url)), "..", "..", "build", "jhon");
	const home = mkdtempSync(join(tmpdir(), "jhon-events-parity-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	const run = spawnSync(bin, ["settings", "list", "--json"], { encoding: "utf8", env: { PATH: process.env.PATH, HOME: home, JHON_SETTINGS: join(home, "settings.toml") } });
	assert.equal(run.status, 0, `${bin}: ${run.error?.message ?? run.stderr}`);
	const prefix = "shell.notifications.audio.events.";
	const engine = (JSON.parse(run.stdout).settings as { key: string; value: unknown }[]).filter((s) => s.key.startsWith(prefix)).map((s) => [s.key, s.value]);
	const shell = Object.entries(settings.SETTINGS_DEFAULTS).filter(([key]) => key.startsWith(prefix));
	assert.ok(engine.length > 0, "the engine lists the events");
	assert.deepEqual(shell, engine);
	assert.deepEqual(settings.NOTIFICATION_EVENTS.map(settings.notificationEventKey), engine.map(([key]) => key));
});
