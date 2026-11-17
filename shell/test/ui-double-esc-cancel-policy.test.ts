import assert from "node:assert/strict";
import { join } from "node:path";
import test from "node:test";
import {
	DOUBLE_ESC_CANCEL_KEY,
	DOUBLE_ESC_CANCEL_WINDOW_MS,
	loadDoubleEscCancelPolicy,
	resolveDoubleEscCancelPolicy,
	writeDoubleEscCancelPolicy,
} from "../lib/ui/double-esc-cancel-policy.ts";
import { fakeSettings } from "./fake-jhon-settings.ts";

// ---------------------------------------------------------------------------
// Double-esc-cancel policy.
//
// shell.behaviour.double_esc_cancel, read through `jhon settings` (a fake
// jhon here): JHON_SHELL_DOUBLE_ESC_CANCEL > settings.toml > default off, as
// the engine resolves it (S38). Global-only (see the feature scope in
// odd/tasks/double-esc-cancel.md); fail-closed to "off" when the settings
// cannot be read.
// ---------------------------------------------------------------------------

const EMPTY_ENV = {} as Record<string, string | undefined>;

test("the confirmation window is 1000ms, deliberately double Pi's own 500ms idle double-Esc window", () => {
	assert.equal(DOUBLE_ESC_CANCEL_WINDOW_MS, 1000);
});

// --- cascade: env > settings.toml > default off ---------------------------------

test("default is off with nothing in the settings and no env", (t) => {
	const { settings } = fakeSettings(t);
	assert.equal(loadDoubleEscCancelPolicy({ settings, env: EMPTY_ENV }), "off");
});

test("the environment variable, as the engine resolved it, overrides settings.toml", (t) => {
	const { settings } = fakeSettings(t, { file: { [DOUBLE_ESC_CANCEL_KEY]: true }, env: { [DOUBLE_ESC_CANCEL_KEY]: false } });
	assert.equal(loadDoubleEscCancelPolicy({ settings, env: { JHON_SHELL_DOUBLE_ESC_CANCEL: "off" } }), "off");
});

test("the shell never reads the variable itself: a value the engine ignored decides nothing", (t) => {
	const { settings } = fakeSettings(t);
	for (const raw of ["on", "1", "true", "ON", ""]) {
		assert.equal(loadDoubleEscCancelPolicy({ settings, env: { JHON_SHELL_DOUBLE_ESC_CANCEL: raw } }), "off", `env value "${raw}" is the engine's to resolve`);
	}
});

test("unreadable settings fail closed to off", (t) => {
	const fake = fakeSettings(t, { file: { [DOUBLE_ESC_CANCEL_KEY]: true }, env: { [DOUBLE_ESC_CANCEL_KEY]: true } });
	fake.failNext("settings.toml: expected '=' at line 1");
	fake.settings.refresh();
	assert.equal(loadDoubleEscCancelPolicy({ settings: fake.settings, env: { JHON_SHELL_DOUBLE_ESC_CANCEL: "on" } }), "off");
});

// --- resolver attribution ---------------------------------------------------

test("the resolver attributes the settings file when it decides", (t) => {
	const fake = fakeSettings(t, { file: { [DOUBLE_ESC_CANCEL_KEY]: true } });
	assert.deepEqual(resolveDoubleEscCancelPolicy({ settings: fake.settings, env: EMPTY_ENV }), {
		policy: "on",
		source: "global_file",
		malformed: false,
		globalFile: join(fake.dir, "settings.toml"),
		globalFileExists: true,
		envValue: undefined,
	});
});

test("the resolver attributes the environment variable when it decides", (t) => {
	const { settings } = fakeSettings(t, { env: { [DOUBLE_ESC_CANCEL_KEY]: true } });
	const resolution = resolveDoubleEscCancelPolicy({ settings, env: { JHON_SHELL_DOUBLE_ESC_CANCEL: "on" } });
	assert.equal(resolution.source, "environment");
	assert.equal(resolution.policy, "on");
	assert.equal(resolution.globalFileExists, false);
	assert.equal(resolution.envValue, "on");
});

test("the resolver attributes the built-in default when nothing else decides", (t) => {
	const { settings } = fakeSettings(t);
	const resolution = resolveDoubleEscCancelPolicy({ settings, env: EMPTY_ENV });
	assert.equal(resolution.source, "default");
	assert.equal(resolution.policy, "off");
	assert.equal(resolution.malformed, false);
});

test("the resolver attributes unreadable settings to them and does not fall through", (t) => {
	const fake = fakeSettings(t);
	fake.failNext("settings.toml: expected '=' at line 1");
	fake.settings.refresh();
	const resolution = resolveDoubleEscCancelPolicy({ settings: fake.settings, env: { JHON_SHELL_DOUBLE_ESC_CANCEL: "on" } });
	assert.equal(resolution.policy, "off");
	assert.equal(resolution.malformed, true);
	assert.match(resolution.globalFile, /expected '='/);
});

test("loadDoubleEscCancelPolicy delegates to the resolver so the two can never disagree", (t) => {
	const { settings } = fakeSettings(t, { file: { [DOUBLE_ESC_CANCEL_KEY]: true } });
	assert.equal(
		loadDoubleEscCancelPolicy({ settings, env: EMPTY_ENV }),
		resolveDoubleEscCancelPolicy({ settings, env: EMPTY_ENV }).policy,
	);
});

// --- writer -------------------------------------------------------------------

test("writeDoubleEscCancelPolicy saves through jhon settings set", (t) => {
	const fake = fakeSettings(t);
	const path = writeDoubleEscCancelPolicy("on", { settings: fake.settings });
	assert.equal(path, join(fake.dir, "settings.toml"));
	assert.deepEqual(fake.file(), { [DOUBLE_ESC_CANCEL_KEY]: true });
	assert.ok(fake.calls().some((call) => call.join(" ") === `settings set ${DOUBLE_ESC_CANCEL_KEY} true`));
	assert.equal(loadDoubleEscCancelPolicy({ settings: fake.settings, env: EMPTY_ENV }), "on");
});

test("writeDoubleEscCancelPolicy(off) round-trips through the resolver", (t) => {
	const { settings } = fakeSettings(t);
	writeDoubleEscCancelPolicy("on", { settings });
	writeDoubleEscCancelPolicy("off", { settings });
	const resolution = resolveDoubleEscCancelPolicy({ settings, env: EMPTY_ENV });
	assert.equal(resolution.policy, "off");
	assert.equal(resolution.source, "global_file");
	assert.equal(resolution.malformed, false);
});
