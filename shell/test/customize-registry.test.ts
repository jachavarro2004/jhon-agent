/**
 * T15.15 (L179): Customize is one registry, one file per category under
 * lib/ui/customize/. The category order, the /jhon:customize words, their
 * help and notice, and the rows the overlay shows all come from it; the
 * engine (`jhon settings list --json`) is the one source of each key's kind,
 * values and default, and the shell's SETTINGS_DEFAULTS copy (kept for a
 * shell running without jhon) must agree with it field by field. A key the
 * engine lists is in that copy or declared engine-only (ENGINE_ONLY_SETTINGS):
 * a new one fails here until someone places it.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { ENGINE_ONLY_SETTINGS, SETTINGS_DEFAULTS } from "../lib/settings.ts";
import { CUSTOMIZE_CATEGORIES, CUSTOMIZE_WORDS, customizeDescription, customizeRows, unknownCategoryNotice } from "../lib/ui/customize/registry.ts";
import { memorySettings } from "./fake-jhon-settings.ts";

const noop = () => {};
const hooks = { settings: memorySettings(), animations: noop, doubleEsc: noop, status: noop, redraw: noop };
const withMode = { ...hooks, mode: { value: () => "normal", set: async () => undefined } };

test("the registry's categories, in order, are the /jhon:customize words, its help and its notice", () => {
	assert.deepEqual(CUSTOMIZE_CATEGORIES.map((c) => [c.id, c.title]), [
		["appearance", "Appearance"], ["behaviour", "Behaviour"], ["keys", "Keys"], ["permissions", "Permissions"],
	]);
	assert.deepEqual(CUSTOMIZE_WORDS, ["appearance", "behaviour", "keys", "permissions"]);
	assert.equal(customizeDescription(), "Customize the shell: appearance, behaviour, keys, permissions");
	assert.equal(unknownCategoryNotice("colours"), `No Customize group "colours": appearance, behaviour, keys, permissions.`);
});

// T169 (L1438): the notification sounds are gone, and Customize's Notifications with them.
test("Customize has no Notifications: no category, no row and no default under shell.notifications", () => {
	assert.equal(CUSTOMIZE_CATEGORIES.some((c) => c.id === "notifications" || c.title === "Notifications"), false);
	assert.deepEqual(customizeRows(withMode).filter((row) => row.group === "Notifications" || row.key.startsWith("shell.notifications.")).map((row) => row.key), []);
	assert.deepEqual(Object.keys(SETTINGS_DEFAULTS).filter((key) => key.startsWith("shell.notifications.")), []);
	assert.equal(unknownCategoryNotice("notifications"), `No Customize group "notifications": appearance, behaviour, keys, permissions.`);
});

test("the rows come category by category in the registry's order; a hidden row hides its empty category", () => {
	const rows = customizeRows(withMode);
	const titles = CUSTOMIZE_CATEGORIES.map((c) => c.title);
	assert.deepEqual([...new Set(rows.map((r) => r.group))], titles);
	const order = rows.map((r) => titles.indexOf(r.group));
	assert.deepEqual(order, [...order].sort((a, b) => a - b), "no category interleaves another");
	for (const category of CUSTOMIZE_CATEGORIES) {
		assert.deepEqual(rows.filter((r) => r.group === category.title).map((r) => r.key), category.rows.filter((r) => !r.hidden?.(withMode)).map((r) => r.key), category.id);
	}
	assert.ok(!customizeRows(hooks).some((r) => r.group === "Permissions"), "the mode row hides without its hook, and Permissions with it");
});

// G's Density (L195) is an Appearance row: comfortable by default, set with
// jhon settings set, applied at once (the chrome and the rail redraw).
test("Appearance has G's Density row: comfortable, compact, minimal; enter saves shell.appearance.density and applies it", () => {
	let applied = 0;
	const settings = memorySettings();
	const rows = customizeRows({ ...hooks, settings, appearance: () => { applied++; } });
	const density = rows.find(({ label }) => label === "Density")!;
	assert.equal(density.group, "Appearance");
	assert.deepEqual(density.options, ["comfortable", "compact", "minimal"]);
	assert.equal(density.value(), "comfortable");
	assert.equal(density.apply("compact"), undefined);
	assert.equal(settings.density(), "compact");
	assert.equal(settings.get("shell.appearance.density"), "compact");
	assert.equal(density.value(), "compact");
	assert.equal(applied, 1);
});

test("every setting row agrees with `jhon settings list --json`: the key exists, choices are the engine's values, and SETTINGS_DEFAULTS matches each default", (t) => {
	const bin = process.env.JHON_TEST_BIN || join(dirname(fileURLToPath(import.meta.url)), "..", "..", "build", "jhon");
	const home = mkdtempSync(join(tmpdir(), "jhon-customize-registry-"));
	t.after(() => rmSync(home, { recursive: true, force: true }));
	const run = spawnSync(bin, ["settings", "list", "--json"], { encoding: "utf8", env: { PATH: process.env.PATH, HOME: home, JHON_SETTINGS: join(home, "settings.toml") } });
	assert.equal(run.status, 0, `${bin}: ${run.error?.message ?? run.stderr}`);
	const engine = new Map((JSON.parse(run.stdout).settings as { key: string; kind: string; values?: string[]; default: unknown }[]).map((e) => [e.key, e]));

	// The shell's defaults copy, key by key and value by value (runtimes.* the engine detects per machine).
	// Every engine key is the shell's too, or declared the engine's alone: never both, never neither.
	const engineOnly = new Set(ENGINE_ONLY_SETTINGS);
	assert.deepEqual(ENGINE_ONLY_SETTINGS.filter((key) => !engine.has(key)), [], "an engine-only key the engine no longer lists");
	assert.deepEqual(ENGINE_ONLY_SETTINGS.filter((key) => key in SETTINGS_DEFAULTS), [], "an engine-only key the shell keeps a default for");
	assert.deepEqual(CUSTOMIZE_CATEGORIES.flatMap((c) => c.rows).map((row) => row.key).filter((key) => engineOnly.has(key)), [], "an engine-only key with a Customize row");
	assert.deepEqual(Object.keys(SETTINGS_DEFAULTS), [...engine.keys()].filter((key) => !engineOnly.has(key)), "the same keys in the engine's order, the engine-only ones aside");
	for (const [key, entry] of engine) {
		assert.ok(entry.kind, `${key} has a kind`);
		if (engineOnly.has(key)) continue;
		if (key.startsWith("runtimes.")) assert.equal(typeof entry.default, "boolean", key);
		else assert.deepEqual(SETTINGS_DEFAULTS[key], entry.default, key);
	}

	for (const row of CUSTOMIZE_CATEGORIES.flatMap((c) => c.rows)) {
		const entry = engine.get(row.key);
		if (!entry) {
			assert.ok(row.save, `${row.key}: not an engine key, so it saves through its own handler`);
			continue;
		}
		if (entry.kind === "enum") assert.deepEqual(row.values, entry.values, `${row.key}: the engine's values`);
		else if (entry.kind === "bool") assert.deepEqual(row.values, ["on", "off"], `${row.key}: a bool is on/off`);
		else assert.equal(row.kind, "text", `${row.key}: a ${entry.kind} is typed`);
	}
});

// T38 (L382): the long stall threshold for the bridge or a thinking model sits beside stall_seconds.
test("Behaviour has the thinking stall row after stall recovery: a typed shell.behaviour.thinking_stall_seconds, 300 by default", () => {
	const rows = customizeRows(hooks);
	const keys = rows.filter(({ group }) => group === "Behaviour").map(({ key }) => key);
	assert.equal(keys.indexOf("shell.behaviour.thinking_stall_seconds"), keys.indexOf("shell.behaviour.stall_seconds") + 1);
	const row = rows.find(({ key }) => key === "shell.behaviour.thinking_stall_seconds")!;
	assert.equal(row.label, "Thinking stall recovery (s)");
	assert.equal(row.value(), "300");
});

// T155 (L1377, L1394): the context ladder is two rows, compact_at and compact_tokens; the notices come from them.
test("Behaviour has the context ladder as two rows, compact_at then compact_tokens 200000, and the sub-agent wrap-up; no removed key", () => {
	const rows = customizeRows(hooks);
	const keys = rows.filter(({ group }) => group === "Behaviour").map(({ key }) => key);
	assert.equal(keys.indexOf("shell.behaviour.compact_tokens"), keys.indexOf("shell.behaviour.compact_at") + 1);
	assert.equal(keys.indexOf("shell.behaviour.child_backstop_tokens"), keys.indexOf("shell.behaviour.compact_tokens") + 1);
	for (const gone of ["fresh_session_at", "fresh_session_tokens", "backstop_tokens", "child_cap_tokens"]) assert.equal(keys.includes(`shell.behaviour.${gone}`), false, gone);
	const at = rows.find(({ key }) => key === "shell.behaviour.compact_at")!;
	assert.deepEqual(at.preview!("60", false).lines, ["At 60% of the context window the session compacts; from 50% the context notice suggests a fresh session."]);
	assert.deepEqual(at.preview!("100", false).lines, ["100: pi decides when the session compacts; the fresh-session notice comes at 90%."]);
	const compact = rows.find(({ key }) => key === "shell.behaviour.compact_tokens")!;
	assert.equal(compact.label, "Compact at (tokens)");
	assert.equal(compact.value(), "200000");
	assert.deepEqual(compact.preview!("200000", false).lines, [
		"Past 200000 tokens the session compacts, or at the share if that comes first.",
		"Past 150000 tokens the context notice suggests a fresh session; past 100000 tokens, or 15% of the window, the orchestrator hears once to delegate.",
	]);
	assert.deepEqual(compact.preview!("0", false).lines, ["0: off; only the shares compact and suggest a fresh session, and no delegation notice."]);
	const child = rows.find(({ key }) => key === "shell.behaviour.child_backstop_tokens")!;
	assert.equal(child.value(), "80000");
});
