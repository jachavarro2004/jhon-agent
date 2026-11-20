import "./isolated-env.ts";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { installShellSettings, jhonSettingsBackend, retiredNotice, SETTINGS_DEFAULTS, shellSettings, ShellSettings } from "../lib/settings.ts";
import { FreshSessionLatch } from "../lib/fresh-session.ts";
import { fakeJhon, memorySettings } from "./fake-jhon-settings.ts";

const SHELL = join(dirname(fileURLToPath(import.meta.url)), "..");

// S38 as corrected at L199: today's behaviour stays the default.
const S38 = {
	animations: "full", ascii: false, icons: "geometric", status: "auto", cards: "float", surface: "transparent", compactAt: 60, freshSessionAt: 50, stallSeconds: 75, thinkingStallSeconds: 300,
	doubleEscCancel: false,
	keys: { panel: "alt+j", agents: "alt+s" },
};

function snapshot(s: ShellSettings) {
	return {
		animations: s.animations(), ascii: s.ascii(), icons: s.icons(), status: s.status(), cards: s.cards(), surface: s.surface(), compactAt: s.compactAt(), freshSessionAt: s.freshSessionAt(),
		stallSeconds: s.stallSeconds(), thinkingStallSeconds: s.thinkingStallSeconds(), doubleEscCancel: s.doubleEscCancel(),
		keys: { panel: s.key("panel"), agents: s.key("agents") },
	};
}

test("the values come from `jhon settings list --json`, each typed", (t) => {
	const fake = fakeJhon(t, { file: { "shell.appearance.icons": "emoji", "shell.behaviour.stall_seconds": 0, "shell.keys.agents": "off", "shell.keys.panel": "ctrl+shift+j" }, env: { "shell.behaviour.double_esc_cancel": true } });
	const s = new ShellSettings(jhonSettingsBackend(fake.bin));
	assert.equal(s.loaded, true);
	assert.equal(s.path, join(fake.dir, "settings.toml"));
	assert.equal(s.icons(), "emoji");
	assert.equal(s.stallSeconds(), 0);
	assert.equal(s.key("agents"), undefined, "off turns the shortcut off");
	assert.equal(s.key("panel"), "ctrl+shift+j");
	assert.equal(s.doubleEscCancel(), true, "an environment override arrives resolved by the engine");
	assert.equal(s.source("shell.behaviour.double_esc_cancel"), "env");
	assert.equal(s.source("shell.appearance.icons"), "file");
	assert.deepEqual(fake.calls(), [["settings", "list", "--json"]], "loaded once");
	s.icons(); s.doubleEscCancel();
	assert.equal(fake.calls().length, 1, "getters read the cache");
});

// T15.12d (L153 answers 1-2): the Gentle Shell's card style and chrome surface.
test("the card style and the chrome surface read from shell.appearance.cards and .surface", (t) => {
	const fake = fakeJhon(t, { file: { "shell.appearance.cards": "neon", "shell.appearance.surface": "tinted" } });
	const s = new ShellSettings(jhonSettingsBackend(fake.bin));
	assert.equal(s.cards(), "neon");
	assert.equal(s.surface(), "tinted");
	assert.deepEqual(s.set("shell.appearance.cards", "sections"), { ok: false, error: 'jhon settings: shell.appearance.cards: "sections" is not accepted' });
	assert.equal(s.cards(), "neon", "a refused value leaves the stored one");
});

test("after a failed load the settings are tried again at most once every 30 s", () => {
	let fail = true, calls = 0, now = 1_000_000;
	const backend = {
		list() {
			calls++;
			return fail ? { error: "jhon settings: busy" } : { path: "/nonexistent/settings.toml", values: new Map([["shell.appearance.ascii", { value: true, source: "file" as const }]]) };
		},
		set() { return null; },
	};
	const s = new ShellSettings(backend, undefined, () => now);
	assert.equal(s.loaded, false);
	assert.equal(calls, 1);
	fail = false;
	now += 29_999;
	s.refreshIfChanged();
	assert.equal(calls, 1, "no call within 30 s");
	now += 1;
	s.refreshIfChanged();
	assert.equal(calls, 2);
	assert.equal(s.loaded, true);
	assert.equal(s.bool("shell.appearance.ascii"), true);
});

test("a missing binary gives S38's defaults and set refuses", () => {
	const s = new ShellSettings(jhonSettingsBackend("/nonexistent/jhon"));
	assert.equal(s.loaded, false);
	assert.match(s.error ?? "", /\/nonexistent\/jhon settings list/);
	assert.deepEqual(snapshot(s), S38);
	const r = s.set("shell.appearance.ascii", true);
	assert.equal(r.ok, false);
});

test("a failing binary gives S38's defaults", (t) => {
	const fake = fakeJhon(t, { fail: "settings.toml: shell.appearance.ascii: \"yes\" is not accepted" });
	const s = new ShellSettings(jhonSettingsBackend(fake.bin));
	assert.equal(s.loaded, false);
	assert.match(s.error ?? "", /is not accepted/);
	assert.deepEqual(snapshot(s), S38);
});

test("a binary that does not answer in time gives S38's defaults", (t) => {
	const dir = mkdtempSync(join(tmpdir(), "jhon-silent-settings-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const bin = join(dir, "jhon");
	writeFileSync(bin, "#!/bin/sh\nsleep 5\n");
	chmodSync(bin, 0o755);
	const s = new ShellSettings(jhonSettingsBackend(bin, 200));
	assert.match(s.error ?? "", /did not answer in 0.2s/);
	assert.deepEqual(snapshot(s), S38);
});

test("with no binary at all every key is its default", () => {
	const s = new ShellSettings();
	assert.deepEqual(snapshot(s), S38);
	for (const [key, value] of Object.entries(SETTINGS_DEFAULTS)) assert.equal(s.get(key), value, key);
	// T169 (L1438): the notification sounds are gone, their keys with them.
	assert.deepEqual(Object.keys(SETTINGS_DEFAULTS).filter((key) => key.startsWith("shell.notifications.")), []);
});

test("set runs `jhon settings set` and loads the settings again", (t) => {
	const fake = fakeJhon(t);
	const s = new ShellSettings(jhonSettingsBackend(fake.bin));
	assert.equal(s.doubleEscCancel(), false);
	assert.deepEqual(s.set("shell.behaviour.double_esc_cancel", true), { ok: true });
	assert.equal(s.doubleEscCancel(), true);
	assert.deepEqual(fake.calls(), [["settings", "list", "--json"], ["settings", "set", "shell.behaviour.double_esc_cancel", "true"], ["settings", "list", "--json"]]);
	assert.deepEqual(fake.file(), { "shell.behaviour.double_esc_cancel": true });
});

test("a value jhon refuses is reported and nothing changes", (t) => {
	const fake = fakeJhon(t);
	const s = new ShellSettings(jhonSettingsBackend(fake.bin));
	const r = s.set("shell.appearance.icons", "boxes");
	assert.equal(r.ok, false);
	assert.match(!r.ok ? r.error : "", /icons: "boxes" is not accepted/);
	assert.equal(s.icons(), "geometric");
	assert.deepEqual(fake.file(), {});
});

test("setMany writes in order and loads once", (t) => {
	const fake = fakeJhon(t);
	const s = new ShellSettings(jhonSettingsBackend(fake.bin));
	assert.deepEqual(s.setMany([["shell.appearance.ascii", true], ["shell.appearance.icons", "emoji"]]), { ok: true });
	assert.equal(s.get("shell.appearance.icons"), "emoji");
	assert.equal(fake.calls().filter((c) => c[1] === "list").length, 2);
});

test("the process keeps one copy, and under the test runner never spawns jhon", () => {
	const previous = installShellSettings(undefined);
	try {
		const a = shellSettings({ PI_CODING_AGENT_DIR: "/nonexistent" });
		assert.equal(a, shellSettings());
		assert.equal(a.loaded, false);
		assert.deepEqual(snapshot(a), S38);
	} finally { installShellSettings(previous); }
});

test("no shell module reads the old jhon-shell/*.json files", () => {
	const OLD = ["card-style", "card-content", "visual-customization", "visual-profiles", "animations", "vim", "notifications", "double-esc-cancel"];
	const offenders: string[] = [];
	const walk = (dir: string) => {
		for (const name of readdirSync(dir)) {
			const path = join(dir, name);
			if (statSync(path).isDirectory()) { walk(path); continue; }
			if (!name.endsWith(".ts")) continue;
			const text = readFileSync(path, "utf8");
			for (const old of OLD) if (text.includes(`"${old}.json"`) || text.includes(`'${old}.json'`) || text.includes(`/${old}.json`)) offenders.push(`${relative(SHELL, path)}: ${old}.json`);
			if (/config-home(\.ts)?["']/.test(text)) offenders.push(`${relative(SHELL, path)}: config-home`);
		}
	};
	walk(join(SHELL, "lib"));
	walk(join(SHELL, "extensions"));
	assert.deepEqual(offenders, []);
});

// The variables `jhon settings` resolves (engine/internal/settings): the
// engine reads them, the shell reads the resolved values (S38).
const SETTINGS_ENV = [
	"JHON_SHELL_JHON_KEY", "JHON_SHELL_TASKS_KEY", "JHON_SHELL_TODO_KEY", "JHON_SHELL_CHANGES_KEY", "JHON_SHELL_USAGE_KEY",
	"JHON_SHELL_COMPACT_AT", "JHON_SHELL_STALL_SECONDS", "JHON_SHELL_THINKING_STALL_SECONDS", "JHON_SHELL_QUIET_TOOLS", "JHON_SHELL_ASCII", "JHON_SHELL_KEY_HINTS", "JHON_SHELL_DOUBLE_ESC_CANCEL",
];

test("no shell module reads a variable `jhon settings` resolves; only the engine does", () => {
	const offenders: string[] = [];
	const walk = (dir: string) => {
		for (const name of readdirSync(dir)) {
			const path = join(dir, name);
			if (statSync(path).isDirectory()) { walk(path); continue; }
			if (!name.endsWith(".ts")) continue;
			const text = readFileSync(path, "utf8");
			for (const env of SETTINGS_ENV) {
				// env.NAME, env?.NAME, env["NAME"], or a constant naming it to index with.
				const read = new RegExp(`\\??\\.${env}\\b|\\[\\s*["'\`]${env}["'\`]\\s*\\]|=\\s*["'\`]${env}["'\`]`);
				if (read.test(text)) offenders.push(`${relative(SHELL, path)}: ${env}`);
			}
		}
	};
	walk(join(SHELL, "lib"));
	walk(join(SHELL, "extensions"));
	assert.deepEqual(offenders, []);
});

// T155 (L1377, L1394): the context ladder is compact_at and compact_tokens; the notices come from them.
test("the fresh-session notice comes ten points of the window before shell.behaviour.compact_at, 50 by default, at least 1", () => {
	assert.equal("shell.behaviour.fresh_session_at" in SETTINGS_DEFAULTS, false);
	const previous = installShellSettings(memorySettings({ "shell.behaviour.compact_at": 80, "shell.behaviour.fresh_session_at": 20 }));
	try {
		assert.equal(shellSettings().freshSessionAt(), 70, "an old fresh_session_at changes nothing");
		const latch = new FreshSessionLatch();
		assert.equal(latch.context(69), undefined);
		assert.match(latch.context(70) ?? "", /^One session per change: the context is at 70%/);
	} finally { installShellSettings(previous); }
	for (const [at, fresh] of [[5, 1], [11, 1], [12, 2], [100, 90]]) {
		const p = installShellSettings(memorySettings({ "shell.behaviour.compact_at": at }));
		try { assert.equal(shellSettings().freshSessionAt(), fresh, String(at)); } finally { installShellSettings(p); }
	}
	installShellSettings(memorySettings());
	try {
		const latch = new FreshSessionLatch();
		assert.equal(latch.context(49), undefined);
		assert.match(latch.context(50) ?? "", /^One session per change: the context is at 50%/);
	} finally { installShellSettings(previous); }
});

test("the fresh-session token budget is three quarters of shell.behaviour.compact_tokens, 150000 by default, 0 off", () => {
	assert.equal("shell.behaviour.fresh_session_tokens" in SETTINGS_DEFAULTS, false);
	for (const [tokens, fresh] of [[undefined, 150000], [100000, 75000], [1001, 750], [0, 0]] as const) {
		const p = installShellSettings(memorySettings(tokens === undefined ? { "shell.behaviour.fresh_session_tokens": 9 } : { "shell.behaviour.compact_tokens": tokens }));
		try { assert.equal(shellSettings().freshSessionTokens(), fresh, String(tokens)); } finally { installShellSettings(p); }
	}
});

// T30 (L314): v0.2 retired keys too, so the drop notice names no version.
test("the retired-settings notice names no version, only what went, why and the backup", () => {
	const notice = retiredNotice([
		{ key: "shell.editor.vim", was: false, why: "the vim editor was removed from the shell" },
		{ key: "shell.notifications.audio.backend", was: "auto", why: "it only ever had one value", silent: true },
	], ["dropped: shell.editor.vim", "backup: /fake/settings-backup/settings.toml"]);
	assert.equal(notice, [
		"Settings changed; these went from settings.toml:",
		"  shell.editor.vim = false: the vim editor was removed from the shell",
		"backup: /fake/settings-backup/settings.toml",
	].join("\n"));
});
