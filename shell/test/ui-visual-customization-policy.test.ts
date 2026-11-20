import "./isolated-env.ts";
import assert from "node:assert/strict";
import { join } from "node:path";
import { test } from "node:test";
import { CHROME_SURFACE, DEFAULT_VISUAL_SETTINGS, DENSITY, resolveVisualSettings, STATUS_PLACEMENT, writeVisualSettings } from "../lib/ui/visual-customization-policy.ts";
import { fakeSettings, memorySettings } from "./fake-jhon-settings.ts";

// T15.3: the layout's one choice is shell.appearance.status, auto or hidden.
const KEY = "shell.appearance.status";

test("missing settings show the status line where it fits", (t) => {
	const { settings } = fakeSettings(t);
	const resolved = resolveVisualSettings({ settings });
	assert.deepEqual(resolved.settings, { statusPlacement: "auto", surface: "transparent", density: "compact" });
	assert.equal(resolved.source, "default");
	assert.deepEqual(DEFAULT_VISUAL_SETTINGS, { statusPlacement: "auto", surface: "transparent", density: "compact" });
});

// L195: the pane's density is shell.appearance.density, compact by default (L1498).
test("the density reads shell.appearance.density: compact by default, comfortable or minimal", () => {
	assert.deepEqual(Object.values(DENSITY), ["comfortable", "compact", "minimal"]);
	assert.equal(resolveVisualSettings({ settings: memorySettings() }).settings.density, "compact");
	for (const density of ["comfortable", "compact", "minimal"]) assert.equal(resolveVisualSettings({ settings: memorySettings({ "shell.appearance.density": density }) }).settings.density, density);
	assert.equal(resolveVisualSettings({ settings: memorySettings({ "shell.appearance.density": "tight" }) }).settings.density, "compact");
});

// T15.12d (L153 answer 2): the chrome surface is shell.appearance.surface.
test("the chrome surface reads shell.appearance.surface: transparent by default, or tinted", () => {
	assert.deepEqual(Object.values(CHROME_SURFACE), ["transparent", "tinted"]);
	assert.equal(resolveVisualSettings({ settings: memorySettings({ "shell.appearance.surface": "tinted" }) }).settings.surface, "tinted");
});

test("each status value survives a write and read", () => {
	const settings = memorySettings();
	for (const statusPlacement of Object.values(STATUS_PLACEMENT)) {
		writeVisualSettings({ statusPlacement }, { settings });
		assert.deepEqual(resolveVisualSettings({ settings }).settings, { statusPlacement, surface: "transparent", density: "compact" });
		assert.equal(settings.jhon.file[KEY], statusPlacement);
	}
});

test("a write goes through jhon settings set to the settings file", (t) => {
	const fake = fakeSettings(t);
	const path = writeVisualSettings({ statusPlacement: "hidden" }, { settings: fake.settings });
	assert.equal(path, join(fake.dir, "settings.toml"));
	assert.deepEqual(fake.file(), { [KEY]: "hidden" });
	assert.equal(resolveVisualSettings({ settings: fake.settings }).source, "global_file");
});

test("unreadable settings are not malformed content and show the status line", (t) => {
	const fake = fakeSettings(t, { file: { [KEY]: "hidden" } });
	fake.failNext("open settings.toml: is a directory");
	fake.settings.refresh();
	const result = resolveVisualSettings({ settings: fake.settings });
	assert.deepEqual(result.settings, DEFAULT_VISUAL_SETTINGS);
	assert.equal(result.malformed, true, "unreadable settings are reported");
});

test("a failed write preserves the stored settings", (t) => {
	const fake = fakeSettings(t, { file: { [KEY]: "hidden" } });
	fake.failNext("open settings.toml: permission denied");
	assert.throws(() => writeVisualSettings(DEFAULT_VISUAL_SETTINGS, { settings: fake.settings }), /permission denied/);
	assert.deepEqual(fake.file(), { [KEY]: "hidden" });
});
