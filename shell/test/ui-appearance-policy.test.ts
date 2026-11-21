/**
 * The appearance settings (lib/ui/appearance-policy.ts): what the shell reads
 * at start and Customize writes. Animations are kept as full/off and read as
 * quality/potato; the status line auto or hidden; the surface transparent or
 * tinted; the density compact by default (L1498). A setting jhon refuses
 * throws and leaves the file; unreadable settings give the defaults and say so.
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import { join } from "node:path";
import test from "node:test";
import { ANIMATION_KEY, CHROME_SURFACE, DEFAULT_VISUAL_SETTINGS, DENSITY, resolveAnimationPolicy, resolveVisualSettings, STATUS_PLACEMENT, writeAnimationPolicy, writeVisualSettings } from "../lib/ui/appearance-policy.ts";
import { fakeSettings, memorySettings } from "./fake-jhon-settings.ts";

const STATUS = "shell.appearance.status";

test("animations: quality is saved as full and potato as off, each read back; anything else is refused and the file keeps its value", (t) => {
	const { settings, file } = fakeSettings(t);
	writeAnimationPolicy("quality", { settings });
	assert.equal(file()[ANIMATION_KEY], "full");
	assert.equal(resolveAnimationPolicy({ settings }).policy, "quality");
	writeAnimationPolicy("potato", { settings });
	assert.equal(file()[ANIMATION_KEY], "off");
	assert.equal(resolveAnimationPolicy({ settings }).policy, "potato");
	for (const refused of ["POTATO", false, "performance"]) {
		assert.throws(() => writeAnimationPolicy(refused as never, { settings }), /is not accepted/);
		assert.equal(file()[ANIMATION_KEY], "off");
	}
});

test("animations: quality and the default source until set, then the settings file; unreadable settings give quality, say so, and rewrite nothing", (t) => {
	const fake = fakeSettings(t);
	const options = { settings: fake.settings };
	assert.deepEqual([resolveAnimationPolicy(options).policy, resolveAnimationPolicy(options).source], ["quality", "default"]);
	assert.equal(writeAnimationPolicy("potato", options), join(fake.dir, "settings.toml"));
	assert.deepEqual([resolveAnimationPolicy(options).policy, resolveAnimationPolicy(options).source], ["potato", "global_file"]);
	fake.failNext("settings.toml: shell.appearance.animations: \"broken\" is not accepted");
	fake.settings.refresh();
	const read = resolveAnimationPolicy(options);
	assert.deepEqual([read.policy, read.source, read.malformed], ["quality", "global_file", true]);
	assert.match(read.globalFile, /is not accepted/);
	assert.equal(fake.file()[ANIMATION_KEY], "off");
});

test("layout: auto, transparent and compact by default; the density and surface as set, an unknown density compact", (t) => {
	const { settings } = fakeSettings(t);
	const read = resolveVisualSettings({ settings });
	assert.deepEqual(read.settings, { statusPlacement: "auto", surface: "transparent", density: "compact" });
	assert.equal(read.source, "default");
	assert.deepEqual(DEFAULT_VISUAL_SETTINGS, read.settings);
	assert.deepEqual(Object.values(DENSITY), ["comfortable", "compact", "minimal"]);
	assert.deepEqual(Object.values(CHROME_SURFACE), ["transparent", "tinted"]);
	for (const density of Object.values(DENSITY)) assert.equal(resolveVisualSettings({ settings: memorySettings({ "shell.appearance.density": density }) }).settings.density, density);
	assert.equal(resolveVisualSettings({ settings: memorySettings({ "shell.appearance.density": "tight" }) }).settings.density, "compact");
	assert.equal(resolveVisualSettings({ settings: memorySettings({ "shell.appearance.surface": "tinted" }) }).settings.surface, "tinted");
});

test("the status line: each placement saved through jhon settings and read back; a refused write keeps the file; unreadable settings show it", (t) => {
	const memory = memorySettings();
	for (const statusPlacement of Object.values(STATUS_PLACEMENT)) {
		writeVisualSettings({ statusPlacement }, { settings: memory });
		assert.equal(resolveVisualSettings({ settings: memory }).settings.statusPlacement, statusPlacement);
		assert.equal(memory.jhon.file[STATUS], statusPlacement);
	}
	const fake = fakeSettings(t);
	assert.equal(writeVisualSettings({ statusPlacement: "hidden" }, { settings: fake.settings }), join(fake.dir, "settings.toml"));
	assert.deepEqual(fake.file(), { [STATUS]: "hidden" });
	assert.equal(resolveVisualSettings({ settings: fake.settings }).source, "global_file");
	fake.failNext("open settings.toml: permission denied");
	assert.throws(() => writeVisualSettings(DEFAULT_VISUAL_SETTINGS, { settings: fake.settings }), /permission denied/);
	assert.deepEqual(fake.file(), { [STATUS]: "hidden" });
	fake.failNext("open settings.toml: is a directory");
	fake.settings.refresh();
	const unread = resolveVisualSettings({ settings: fake.settings });
	assert.deepEqual(unread.settings, DEFAULT_VISUAL_SETTINGS);
	assert.equal(unread.malformed, true);
});
