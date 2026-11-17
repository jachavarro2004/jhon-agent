/**
 * A fake `jhon` for the shell's settings (lib/settings.ts): a small node
 * script that answers `jhon settings list --json` and `jhon settings set`
 * from a state file beside it, as the engine would (S38), and logs every
 * call. No test reads or writes the owner's settings.
 */
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { TestContext } from "node:test";
import { installShellSettings, jhonSettingsBackend, SETTINGS_DEFAULTS, ShellSettings, type RetiredSetting, type SettingsBackend, type SettingValue } from "../lib/settings.ts";

/** The settings the migration writes on the owner's machine (L199): every opt-in integration on, and the desktop palette. */
export const OWNER_SETTINGS: Readonly<Record<string, SettingValue>> = Object.freeze({
	"integrations.telemetry": true,
	"integrations.theme.palette": "desktop",
});

/** Install, for this test, in-memory settings holding `file`; restored after it. */
export function useMemorySettings(t: TestContext, file: Record<string, SettingValue> = {}): ShellSettings {
	const settings = memorySettings(file);
	const previous = installShellSettings(settings);
	t.after(() => { installShellSettings(previous); });
	return settings;
}

export interface FakeJhon {
	bin: string;
	dir: string;
	/** Every call's arguments, in order. */
	calls(): string[][];
	/** What settings.toml holds now: the values written, by key. */
	file(): Record<string, SettingValue>;
}

const ENUMS: Record<string, string[]> = {
	"shell.appearance.animations": ["full", "off"],
	"shell.appearance.icons": ["geometric", "emoji"],
	"shell.appearance.status": ["auto", "hidden"],
	"shell.appearance.cards": ["float", "neon"],
	"shell.appearance.surface": ["transparent", "tinted"],
};

const SCRIPT = (defaults: Record<string, SettingValue>, enums: Record<string, string[]>, env: Record<string, string>) => `#!/usr/bin/env node
const fs = require("node:fs"), path = require("node:path");
const dir = __dirname, stateFile = path.join(dir, "state.json");
const defaults = ${JSON.stringify(defaults)}, enums = ${JSON.stringify(enums)}, env = ${JSON.stringify(env)};
const args = process.argv.slice(2);
fs.appendFileSync(path.join(dir, "calls.log"), JSON.stringify(args) + "\\n");
const state = JSON.parse(fs.readFileSync(stateFile, "utf8"));
if (state.fail) { process.stderr.write("jhon settings: " + state.fail + "\\n"); process.exit(1); }
if (args[0] !== "settings") process.exit(2);
if (args[1] === "list" && args[2] === "--json") {
  const settings = Object.keys(defaults).map((key) => key in env ? { key, value: env[key], source: "env", env: "ENV" } : key in state.file ? { key, value: state.file[key], source: "file" } : { key, value: defaults[key], source: "default" });
  process.stdout.write(JSON.stringify({ schema: 1, path: path.join(dir, "settings.toml"), settings, project: null }) + "\\n");
  process.exit(0);
}
if (args[1] === "set" && args.length === 4) {
  const [, , key, raw] = args;
  if (!(key in defaults)) { process.stderr.write("jhon settings: " + key + ": unknown key\\n"); process.exit(2); }
  let value = raw;
  const d = defaults[key];
  if (typeof d === "boolean") value = raw === "true" ? true : raw === "false" ? false : undefined;
  else if (typeof d === "number") value = /^-?\\d+$/.test(raw) ? Number(raw) : undefined;
  if (value === undefined || (enums[key] && !enums[key].includes(raw))) { process.stderr.write("jhon settings: " + key + ": " + JSON.stringify(raw) + " is not accepted\\n"); process.exit(2); }
  state.file[key] = value;
  fs.writeFileSync(stateFile, JSON.stringify(state));
  process.stdout.write(key + " = " + raw + "\\n");
  process.exit(0);
}
process.exit(2);
`;

/**
 * A fake jhon whose settings.toml holds `file`; `env` are the values an
 * environment variable decides; `fail` makes every call exit 1 with it.
 */
export function fakeJhon(t: TestContext, options: { file?: Record<string, SettingValue>; env?: Record<string, SettingValue>; fail?: string } = {}): FakeJhon {
	const dir = mkdtempSync(join(tmpdir(), "jhon-fake-settings-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const bin = join(dir, "jhon");
	writeFileSync(join(dir, "state.json"), JSON.stringify({ file: options.file ?? {}, fail: options.fail }));
	writeFileSync(join(dir, "calls.log"), "");
	writeFileSync(bin, SCRIPT({ ...SETTINGS_DEFAULTS }, ENUMS, (options.env ?? {}) as Record<string, string>));
	chmodSync(bin, 0o755);
	return {
		bin,
		dir,
		calls: () => readFileSync(join(dir, "calls.log"), "utf8").split("\n").filter(Boolean).map((line) => JSON.parse(line)),
		file: () => JSON.parse(readFileSync(join(dir, "state.json"), "utf8")).file,
	};
}

/**
 * Install, for this test, the process's settings from a fake jhon; restored
 * after it. Returns the fake and the installed settings.
 */
export function useFakeSettings(t: TestContext, options: Parameters<typeof fakeJhon>[1] = {}): FakeJhon & { settings: ShellSettings } {
	const fake = fakeJhon(t, options);
	const settings = new ShellSettings(jhonSettingsBackend(fake.bin));
	const previous = installShellSettings(settings);
	t.after(() => { installShellSettings(previous); });
	return { ...fake, settings };
}

/** An in-memory `jhon settings`, for tests that make many shells: the same answers as the fake binary, without a process. */
export interface MemoryJhon {
	backend: SettingsBackend;
	/** What settings.toml holds now. */
	file: Record<string, SettingValue>;
	/** Every `set`, in order. */
	sets: [string, string][];
	/** When set, every call fails with it (settings that cannot be read). */
	fail?: string;
	/** The values environment variables decide, by key. */
	env: Record<string, SettingValue>;
	/** When set, every `set` fails with it, and reading still works. */
	failSet?: string;
	/** The retired keys the file holds (T15.3); `migrate` drops them. */
	retired: RetiredSetting[];
	/** Every `migrate`. */
	migrations: number;
}

export function memoryJhon(file: Record<string, SettingValue> = {}, fail?: string, env: Record<string, SettingValue> = {}): MemoryJhon {
	const jhon: MemoryJhon = { file: { ...file }, sets: [], fail, env: { ...env }, retired: [], migrations: 0, backend: undefined as unknown as SettingsBackend };
	jhon.backend = {
		list() {
			if (jhon.fail) return { error: `jhon settings: ${jhon.fail}` };
			const values = new Map<string, { value: SettingValue; source: "default" | "file" | "env" }>();
			for (const [key, value] of Object.entries(SETTINGS_DEFAULTS)) values.set(key, key in jhon.env ? { value: jhon.env[key], source: "env" } : key in jhon.file ? { value: jhon.file[key], source: "file" } : { value, source: "default" });
			return { path: "/fake/settings.toml", values, retired: [...jhon.retired] };
		},
		set(key, raw) {
			if (jhon.fail) return `jhon settings: ${jhon.fail}`;
			if (jhon.failSet) return `jhon settings: ${jhon.failSet}`;
			jhon.sets.push([key, raw]);
			if (!(key in SETTINGS_DEFAULTS)) return `jhon settings: ${key}: unknown key`;
			const d = SETTINGS_DEFAULTS[key];
			const value = typeof d === "boolean" ? (raw === "true" ? true : raw === "false" ? false : undefined)
				: typeof d === "number" ? (/^-?\d+$/.test(raw) ? Number(raw) : undefined) : raw;
			if (value === undefined || (ENUMS[key] && !ENUMS[key].includes(raw))) return `jhon settings: ${key}: ${JSON.stringify(raw)} is not accepted`;
			jhon.file[key] = value;
			return null;
		},
		migrate() {
			if (jhon.fail) return { ok: false, error: `jhon settings: ${jhon.fail}` };
			jhon.migrations++;
			const dropped = jhon.retired.length;
			jhon.retired = [];
			return { ok: true, lines: dropped ? [`dropped ${dropped} retired setting(s) from /fake/settings.toml`, "backup: /fake/settings-backup/settings.toml"] : ["nothing to migrate"] };
		},
	};
	return jhon;
}

/** Settings over an in-memory jhon: the settings and the jhon behind them. */
export function memorySettings(file: Record<string, SettingValue> = {}, fail?: string, env: Record<string, SettingValue> = {}): ShellSettings & { jhon: MemoryJhon } {
	const jhon = memoryJhon(file, fail, env);
	return Object.assign(new ShellSettings(jhon.backend), { jhon });
}

// The engine's environment overrides (engine/internal/settings), each read as
// the engine reads it, so a test that sets a JHON_SHELL_* variable gets the
// value `jhon settings` would give the shell.
const ENV_OVERRIDES: [string, string, (v: string) => SettingValue | undefined][] = [
	["JHON_SHELL_ASCII", "shell.appearance.ascii", (v) => v === "" ? undefined : v === "1"],
	["JHON_SHELL_COMPACT_AT", "shell.behaviour.compact_at", (v) => {
		const t = v.trim().toLowerCase();
		if (t === "off" || t === "0") return 100;
		return /^\d+$/.test(t) && Number(t) > 0 && Number(t) < 100 ? Number(t) : undefined;
	}],
	["JHON_SHELL_STALL_SECONDS", "shell.behaviour.stall_seconds", (v) => /^\d+$/.test(v.trim()) ? Number(v.trim()) : undefined],
	["JHON_SHELL_THINKING_STALL_SECONDS", "shell.behaviour.thinking_stall_seconds", (v) => /^\d+$/.test(v.trim()) ? Number(v.trim()) : undefined],
	["JHON_SHELL_DOUBLE_ESC_CANCEL", "shell.behaviour.double_esc_cancel", (v) => v === "on" ? true : v === "off" ? false : undefined],
	...([["JHON_SHELL_JHON_KEY", "panel"], ["JHON_SHELL_TASKS_KEY", "tasks"], ["JHON_SHELL_TODO_KEY", "todos"], ["JHON_SHELL_CHANGES_KEY", "changes"], ["JHON_SHELL_RAIL_KEY", "rail"], ["JHON_SHELL_COMMANDS_KEY", "commands"]] as const)
		.map(([name, key]): [string, string, (v: string) => SettingValue | undefined] => [name, `shell.keys.${key}`, (v) => {
			const t = v.trim();
			if (t === "" || t.toLowerCase() === "off") return "off";
			return /^[A-Za-z0-9+-]+$/.test(t) ? t : undefined;
		}]),
];

/** The values an environment's JHON_SHELL_* variables decide, as `jhon settings` resolves them. */
export function envOverrides(env: NodeJS.ProcessEnv): Record<string, SettingValue> {
	const out: Record<string, SettingValue> = {};
	for (const [name, key, read] of ENV_OVERRIDES) {
		const raw = env[name];
		if (raw === undefined) continue;
		const value = read(raw);
		if (value !== undefined) out[key] = value;
	}
	return out;
}

/** Settings read through a fake jhon binary (not installed for the process). */
export function fakeSettings(t: TestContext, options: Parameters<typeof fakeJhon>[1] = {}): FakeJhon & { settings: ShellSettings; failNext(why: string | undefined): void } {
	const fake = fakeJhon(t, options);
	const failNext = (why: string | undefined) => {
		const path = join(fake.dir, "state.json");
		writeFileSync(path, JSON.stringify({ ...JSON.parse(readFileSync(path, "utf8")), fail: why }));
	};
	return { ...fake, settings: new ShellSettings(jhonSettingsBackend(fake.bin)), failNext };
}
