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
		["appearance", "Appearance"], ["behaviour", "Behaviour"], ["keys", "Keys"], ["notifications", "Notifications"], ["permissions", "Permissions"],
	]);
	assert.deepEqual(CUSTOMIZE_WORDS, ["appearance", "behaviour", "keys", "notifications", "permissions"]);
	assert.equal(customizeDescription(), "Customize the shell: appearance, behaviour, keys, notifications, permissions");
	assert.equal(unknownCategoryNotice("colours"), `No Customize group "colours": appearance, behaviour, keys, notifications, permissions.`);
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
		else if (entry.kind === "sound") assert.ok(row.values?.every((v) => v === "none" || v.startsWith("builtin:")), row.key);
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

// T115a (L691): each share row has its token budget beside it; 0 turns the budget off.
test("Behaviour has the token budgets after their shares: compact_tokens 200000 and fresh_session_tokens 150000, 0 off", () => {
	const rows = customizeRows(hooks);
	const keys = rows.filter(({ group }) => group === "Behaviour").map(({ key }) => key);
	assert.equal(keys.indexOf("shell.behaviour.compact_tokens"), keys.indexOf("shell.behaviour.compact_at") + 1);
	assert.equal(keys.indexOf("shell.behaviour.fresh_session_tokens"), keys.indexOf("shell.behaviour.fresh_session_at") + 1);
	const compact = rows.find(({ key }) => key === "shell.behaviour.compact_tokens")!;
	assert.equal(compact.label, "Compact at (tokens)");
	assert.equal(compact.value(), "200000");
	assert.deepEqual(compact.preview!("200000", false).lines, ["Past 200000 tokens the session compacts, or at the share if that comes first."]);
	assert.deepEqual(compact.preview!("0", false).lines, ["0: off; only the share compacts."]);
	const fresh = rows.find(({ key }) => key === "shell.behaviour.fresh_session_tokens")!;
	assert.equal(fresh.label, "Fresh session at (tokens)");
	assert.equal(fresh.value(), "150000");
	assert.deepEqual(fresh.preview!("150000", false).lines, ["Past 150000 tokens the context notice suggests a fresh session, or at the share if that comes first."]);
	assert.deepEqual(fresh.preview!("0", false).lines, ["0: off; only the share brings the notice."]);
	const share = rows.find(({ key }) => key === "shell.behaviour.fresh_session_at")!;
	assert.deepEqual(share.preview!("50", false).lines, ["From 50% of the window the context notice suggests a fresh session."], "the share row as before");
});

// T115e (L715): the delegation backstop, the ladder's first rung, after the fresh-session rows.
test("Behaviour has the context backstop after the fresh-session budget: backstop_tokens 100000, 0 off", () => {
	const rows = customizeRows(hooks);
	const keys = rows.filter(({ group }) => group === "Behaviour").map(({ key }) => key);
	assert.equal(keys.indexOf("shell.behaviour.backstop_tokens"), keys.indexOf("shell.behaviour.fresh_session_tokens") + 1);
	const row = rows.find(({ key }) => key === "shell.behaviour.backstop_tokens")!;
	assert.equal(row.label, "Delegate at (tokens)");
	assert.equal(row.value(), "100000");
	assert.deepEqual(row.preview!("100000", false).lines, ["Past 100000 tokens, or 15% of the window if that comes first, the orchestrator hears once to delegate its next several-step unit."]);
	assert.deepEqual(row.preview!("0", false).lines, ["0: off; no delegation backstop."]);
});
