import assert from "node:assert/strict";
import { join } from "node:path";
import test from "node:test";
import { ANIMATION_KEY, resolveAnimationPolicy, writeAnimationPolicy } from "../lib/ui/animation-policy.ts";
import { fakeSettings } from "./fake-jhon-settings.ts";

test("animation policy accepts only quality and potato, kept under the settings' names full and off", (t) => {
	const { settings, file } = fakeSettings(t);
	for (const [policy, saved] of [["quality", "full"], ["potato", "off"]] as const) {
		writeAnimationPolicy(policy, { settings });
		assert.equal(file()[ANIMATION_KEY], saved);
		assert.equal(resolveAnimationPolicy({ settings }).policy, policy);
	}
	for (const bad of ["POTATO", false, "performance"]) {
		assert.throws(() => writeAnimationPolicy(bad as never, { settings }), /is not accepted/);
		assert.equal(file()[ANIMATION_KEY], "off");
	}
});

test("global policy defaults to quality, persists through jhon settings, and attributes unreadable settings without rewriting", (t) => {
	const fake = fakeSettings(t);
	const options = { settings: fake.settings };
	assert.equal(resolveAnimationPolicy(options).policy, "quality");
	assert.equal(resolveAnimationPolicy(options).source, "default");
	for (const policy of ["potato", "quality"] as const) {
		const path = writeAnimationPolicy(policy, options);
		assert.equal(path, join(fake.dir, "settings.toml"));
		assert.equal(resolveAnimationPolicy(options).policy, policy);
		assert.equal(resolveAnimationPolicy(options).source, "global_file");
	}
	writeAnimationPolicy("potato", options);
	fake.failNext("settings.toml: shell.appearance.animations: \"broken\" is not accepted");
	fake.settings.refresh();
	const result = resolveAnimationPolicy(options);
	assert.equal(result.policy, "quality");
	assert.equal(result.source, "global_file");
	assert.equal(result.malformed, true);
	assert.match(result.globalFile, /is not accepted/);
	assert.equal(fake.file()[ANIMATION_KEY], "off");
});
