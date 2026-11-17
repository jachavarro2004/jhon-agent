import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { activeProfileName } from "../lib/profiles.ts";
import { installShellSettings, ShellSettings } from "../lib/settings.ts";
import { memoryJhon, useMemorySettings } from "./fake-jhon-settings.ts";

function profilesFile(t: test.TestContext, body: string): string {
	const dir = mkdtempSync(join(tmpdir(), "jhon-profiles-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const file = join(dir, "profiles.json");
	writeFileSync(file, body);
	return file;
}

test("the active profile is settings' pi.profile when the owner set it (T56b)", (t) => {
	useMemorySettings(t, { "pi.profile": "mixed" });
	assert.equal(activeProfileName(profilesFile(t, `{"active":"cheap","profiles":{"mixed":{},"cheap":{}}}`)), "mixed");
});

test("with pi.profile at its default, the active profile is profiles.json's active, else default", (t) => {
	useMemorySettings(t);
	assert.equal(activeProfileName(profilesFile(t, `{"active":"cheap","profiles":{"cheap":{}}}`)), "cheap");
	assert.equal(activeProfileName(profilesFile(t, `not json`)), "default");
});

test("a profile changed in settings.toml by another process shows without a refresh; an unchanged file costs no call (T56c)", (t) => {
	const dir = mkdtempSync(join(tmpdir(), "jhon-settings-file-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const path = join(dir, "settings.toml");
	writeFileSync(path, "[pi]\nprofile = \"mixed\"\n");
	const jhon = memoryJhon({ "pi.profile": "mixed" });
	let lists = 0;
	const settings = new ShellSettings({ list: () => { lists++; const l = jhon.backend.list(); return "error" in l ? l : { ...l, path }; }, set: jhon.backend.set });
	const previous = installShellSettings(settings);
	t.after(() => { installShellSettings(previous); });
	const profiles = profilesFile(t, `{"profiles":{"mixed":{},"cheap":{}}}`);
	assert.equal(activeProfileName(profiles), "mixed");
	assert.equal(lists, 1, "an unchanged file is not read again");
	jhon.file["pi.profile"] = "cheap";
	writeFileSync(path, "[pi]\nprofile = \"cheap\"\n");
	assert.equal(activeProfileName(profiles), "cheap");
	assert.equal(lists, 2);
});
