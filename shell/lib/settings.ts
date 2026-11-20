/**
 * shell/lib/settings.ts — the owner's settings, as the shell reads and
 * writes them: only through `jhon settings` (S38), the binary the guards run
 * (jhon.json's `jhon`, lib/guards.ts).
 *
 * The process loads them once, at the first read of the session, with
 * `jhon settings list --json`, and keeps them: every extension shares one
 * copy (pi gives each extension its own module cache, so the copy lives in
 * a process-wide slot). `set` runs `jhon settings set <key> <value>` and
 * loads them again. The engine has already applied the environment
 * overrides (JHON_SHELL_* and the rest), so nothing here reads them again.
 *
 * When the binary is missing, fails, or does not answer within
 * SETTINGS_TIMEOUT_MS, every key takes its default, S38's as corrected at
 * L199 (SETTINGS_DEFAULTS), and `set` refuses: the shell runs without Jhon,
 * just unconfigured. Under node's test runner nothing is ever spawned unless
 * a test installs settings of its own, so a test never reads or migrates the
 * owner's real settings.
 */
import { spawnSync } from "node:child_process";
import { statSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { loadConfig } from "./guards.ts";

/** How long `jhon settings` may take, as `jhon contract` (guards.ts CONTRACT_TIMEOUT_MS). */
export const SETTINGS_TIMEOUT_MS = 5_000;

export type SettingValue = boolean | number | string;
/** Where a value comes from: its default, settings.toml, or its environment variable. */
export type SettingSource = "default" | "file" | "env";

/**
 * Every key's default, the engine's schema (engine/internal/settings), in its
 * order: the shell's copy for when jhon cannot answer (and SettingsKey's
 * source). test/customize-registry.test.ts holds it equal, key by key, to the
 * `default` `jhon settings list --json` gives; a key the engine lists that is
 * not here is declared in ENGINE_ONLY_SETTINGS below, or that test fails.
 */
export const SETTINGS_DEFAULTS: Readonly<Record<string, SettingValue>> = Object.freeze({
	"runtimes.claude_code": true,
	"runtimes.pi": true,
	"integrations.engram": true,
	"integrations.codegraph": true,
	"integrations.telemetry": false,
	"integrations.theme.palette": "rose-ink",
	"claude_code.connectors": false,
	"pi.profile": "default",
	"shell.appearance.animations": "full",
	"shell.appearance.ascii": false,
	"shell.appearance.icons": "geometric",
	"shell.appearance.status": "auto",
	"shell.appearance.cards": "float",
	"shell.appearance.surface": "transparent",
	"shell.appearance.density": "comfortable",
	"shell.behaviour.compact_at": 60,
	"shell.behaviour.compact_tokens": 200000,
	"shell.behaviour.child_backstop_tokens": 80000,
	"shell.behaviour.stall_seconds": 75,
	"shell.behaviour.thinking_stall_seconds": 300,
	"shell.behaviour.double_esc_cancel": false,
	"shell.keys.panel": "alt+j",
	"shell.keys.tasks": "alt+s",
	"shell.keys.todos": "alt+t",
	"shell.keys.changes": "alt+g",
	"shell.keys.rail": "alt+r",
	"shell.keys.commands": "alt+k",
});

/**
 * The keys `jhon settings list --json` gives that the shell keeps no copy of,
 * reads through no getter and shows in no Customize row: the engine's alone,
 * set with `jhon settings set` (engine/internal/setupui, Elsewhere).
 * `sandbox.mode` reaches the shell only through `jhon sandbox status --json`,
 * pinned per session (lib/sandbox.ts), so no default here can stand in for it;
 * `sandbox.allow_write` is a list of folders, which a SettingValue does not hold;
 * `integrations.context7` is read by the render and doctor alone (T133e);
 * `agents.act_on_announce` by `jhon hook announce-stop`, which pi's shell runs (T135, lib/announce-stop.ts).
 */
export const ENGINE_ONLY_SETTINGS: readonly string[] = Object.freeze(["integrations.context7", "agents.act_on_announce", "sandbox.mode", "sandbox.allow_write"]);

export type SettingsKey = keyof typeof SETTINGS_DEFAULTS & string;
export type ShellKey = "panel" | "tasks" | "todos" | "changes" | "rail" | "commands";
export type AnimationsSetting = "full" | "off";
export type IconsSetting = "geometric" | "emoji";
export type StatusSetting = "auto" | "hidden";
export type CardsSetting = "float" | "neon";
export type SurfaceSetting = "transparent" | "tinted";
export type DensitySetting = "comfortable" | "compact" | "minimal";

/** One value as `jhon settings list --json` gives it. */
export interface SettingEntry { value: SettingValue; source: SettingSource; env?: string }
/** A key the settings file holds that the shell no longer reads (T15.3), as `jhon settings list --json` gives it. */
export interface RetiredSetting { key: string; was: SettingValue; why: string; now?: string; silent?: boolean }
/** What a load gave: the values, the retired keys and the file, or why there are none. */
export type SettingsListing = { path: string; values: Map<string, SettingEntry>; retired?: RetiredSetting[] } | { error: string };

/** How settings are read and written; `jhonSettingsBackend` is the real one, a test may give its own. */
export interface SettingsBackend {
	list(): SettingsListing;
	/** Null when the value was written, else the refusal. */
	set(key: string, value: string): string | null;
	/** `jhon settings migrate`: its report's lines, or why it failed. */
	migrate?(): { ok: true; lines: string[] } | { ok: false; error: string };
}

/** The result of a write: done, or the refusal as `jhon settings` said it. */
export type SetResult = { ok: true } | { ok: false; error: string };

function firstLine(text: string): string {
	return text.trim().split("\n")[0] ?? "";
}

/** `jhon settings` run as `bin`, each call bounded by timeoutMs. */
export function jhonSettingsBackend(bin: string, timeoutMs = SETTINGS_TIMEOUT_MS): SettingsBackend {
	const run = (args: string[]) => spawnSync(bin, ["settings", ...args], { encoding: "utf8", timeout: timeoutMs, killSignal: "SIGKILL" });
	return {
		list() {
			const r = run(["list", "--json"]);
			if (r.error || r.status !== 0) {
				const why = r.error ? ((r.error as NodeJS.ErrnoException).code === "ETIMEDOUT" ? `did not answer in ${timeoutMs / 1000}s` : r.error.message) : firstLine(r.stderr ?? "") || `exit ${r.status}`;
				return { error: `${bin} settings list: ${why}` };
			}
			try {
				const out = JSON.parse(r.stdout) as { path?: unknown; settings?: unknown; retired?: unknown };
				if (typeof out.path !== "string" || !Array.isArray(out.settings)) return { error: `${bin} settings list: unexpected output` };
				const values = new Map<string, SettingEntry>();
				for (const item of out.settings as { key?: unknown; value?: unknown; source?: unknown; env?: unknown }[]) {
					const v = item?.value;
					if (typeof item?.key !== "string" || !(typeof v === "boolean" || typeof v === "number" || typeof v === "string")) continue;
					const source = item.source === "file" || item.source === "env" ? item.source : "default";
					values.set(item.key, { value: v, source, ...(typeof item.env === "string" ? { env: item.env } : {}) });
				}
				const isRetired = (entry: RetiredSetting) => typeof entry?.key === "string" && typeof entry?.why === "string";
				const retired = Array.isArray(out.retired) ? (out.retired as RetiredSetting[]).filter(isRetired) : [];
				return { path: out.path, values, retired };
			} catch {
				return { error: `${bin} settings list: unexpected output` };
			}
		},
		set(key, value) {
			const r = run(["set", key, value]);
			if (r.error) return `${bin} settings set: ${(r.error as NodeJS.ErrnoException).code === "ETIMEDOUT" ? `did not answer in ${timeoutMs / 1000}s` : r.error.message}`;
			return r.status === 0 ? null : firstLine(r.stderr ?? "") || `${bin} settings set: exit ${r.status}`;
		},
		migrate() {
			const r = run(["migrate"]);
			if (r.error || r.status !== 0) return { ok: false, error: r.error ? r.error.message : firstLine(r.stderr ?? "") || `${bin} settings migrate: exit ${r.status}` };
			return { ok: true, lines: (r.stdout ?? "").split("\n").filter((line) => line.trim() !== "") };
		},
	};
}

/** A file's identity, size and modification time, or "" when it is absent. */
function fileStamp(path: string): string {
	try {
		const st = statSync(path, { bigint: true });
		return `${st.ino}:${st.size}:${st.mtimeNs}`;
	} catch {
		return "";
	}
}

/** How long a failed load waits before refreshIfChanged tries it again. */
const RETRY_MS = 30_000;

/** The settings, loaded once and kept, with a typed getter per key. */
export class ShellSettings {
	#backend: SettingsBackend | undefined;
	#values = new Map<string, SettingEntry>();
	/** The retired keys the settings file holds (T15.3). */
	retired: RetiredSetting[] = [];
	/** The settings file, or "" when they could not be loaded. */
	path = "";
	/** Why the settings could not be loaded; undefined when they were. */
	error: string | undefined;
	/** The settings file's stamp at the last load, to see another process change it. */
	#stamp = "";
	#now: () => number;
	/** When the last load was tried, to retry a failed one at most every RETRY_MS. */
	#triedAt = 0;

	/** With no backend every key takes its default and `set` refuses. */
	constructor(backend?: SettingsBackend, why = "jhon is not available", now: () => number = Date.now) {
		this.#backend = backend;
		this.#now = now;
		if (backend) this.refresh();
		else this.error = why;
	}

	/** Whether the values came from `jhon settings`. */
	get loaded(): boolean { return this.error === undefined; }

	/** Whether there is a jhon but its settings could not be read (a failure, not an absence). */
	get unreadable(): boolean { return this.#backend !== undefined && this.error !== undefined; }

	/** Where a value is kept, for a message: the settings file, or what kept it from loading. */
	get where(): string { return this.path || `(${this.error})`; }

	/** Load again from `jhon settings`; on a failure every key takes its default. */
	refresh(): void {
		if (!this.#backend) return;
		this.#triedAt = this.#now();
		const listing = this.#backend.list();
		if ("error" in listing) {
			this.#values = new Map();
			this.path = "";
			this.error = listing.error;
			return;
		}
		this.#values = listing.values;
		this.retired = listing.retired ?? [];
		this.path = listing.path;
		this.error = undefined;
		this.#stamp = fileStamp(this.path);
	}

	/**
	 * Load again only when the settings file changed since the last load
	 * (another process wrote it); a stat, no call, otherwise. After a failed
	 * load there is no file to stat, so the load is tried again at most once
	 * every RETRY_MS.
	 */
	refreshIfChanged(): void {
		if (!this.#backend) return;
		if (this.path ? fileStamp(this.path) !== this.#stamp : this.#now() - this.#triedAt >= RETRY_MS) this.refresh();
	}

	get(key: SettingsKey): SettingValue {
		const v = this.#values.get(key)?.value;
		const d = SETTINGS_DEFAULTS[key];
		return v !== undefined && typeof v === typeof d ? v : d;
	}

	source(key: string): SettingSource {
		return this.#values.get(key)?.source ?? "default";
	}

	bool(key: SettingsKey): boolean { return this.get(key) === true; }
	int(key: SettingsKey): number { const v = this.get(key); return typeof v === "number" ? v : Number(SETTINGS_DEFAULTS[key]); }
	string(key: SettingsKey): string { return String(this.get(key)); }

	animations(): AnimationsSetting { return this.string("shell.appearance.animations") === "off" ? "off" : "full"; }
	ascii(): boolean { return this.bool("shell.appearance.ascii"); }
	/** The tool icon family; shell.appearance.ascii wins over it. */
	icons(): IconsSetting { return this.string("shell.appearance.icons") === "emoji" ? "emoji" : "geometric"; }
	/** The status bar and the rail's Status sections: shown where they fit, or hidden. */
	status(): StatusSetting { return this.string("shell.appearance.status") === "hidden" ? "hidden" : "auto"; }
	/** The Gentle Shell's card style: float (a ▎ bar on a tint) or neon (the outline). */
	cards(): CardsSetting { return this.string("shell.appearance.cards") === "neon" ? "neon" : "float"; }
	/** The chrome surface: transparent (each rail section its own card) or tinted (one painted panel). */
	surface(): SurfaceSetting { return this.string("shell.appearance.surface") === "tinted" ? "tinted" : "transparent"; }
	/** G's Density: comfortable pads the chrome and spaces the rail; compact and minimal do not. */
	density(): DensitySetting { const v = this.string("shell.appearance.density"); return v === "compact" || v === "minimal" ? v : "comfortable"; }
	/** The share of the window at which the main session compacts; 100 leaves it to pi. */
	compactAt(): number { return this.int("shell.behaviour.compact_at"); }
	/** The context tokens past which the main session compacts, whichever is first with compact_at; 0 is no budget, and turns every token rung of the ladder off. */
	compactTokens(): number { return this.int("shell.behaviour.compact_tokens"); }
	/** T155 (L1394): the fresh-session notice's share, ten points of the window before compact_at, at least 1. */
	freshSessionAt(): number { return Math.max(1, this.compactAt() - 10); }
	/** T155: the context tokens past which the fresh-session notice comes, whichever is first with its share: three quarters of compact_tokens; 0 is no budget. */
	freshSessionTokens(): number { return Math.floor((this.compactTokens() * 3) / 4); }
	/** T115e, T155: the context tokens past which the orchestrator hears once to delegate, or 15% of the window if smaller: half of compact_tokens; 0 turns it off. */
	backstopTokens(): number { return Math.floor(this.compactTokens() / 2); }
	/** T122: the context tokens past which a sub-agent child hears once to wrap up, or 40% of the window if smaller; 0 turns it off. A child is never capped (T155). */
	childBackstopTokens(): number { return this.int("shell.behaviour.child_backstop_tokens"); }
	/** Seconds of API silence before stall recovery aborts the turn on a direct API provider with thinking off or minimal; 0 turns all stall recovery off. */
	stallSeconds(): number { return this.int("shell.behaviour.stall_seconds"); }
	/** The same on claude-bridge or while the model thinks (T38, L382); 0 never recovers there. */
	thinkingStallSeconds(): number { return this.int("shell.behaviour.thinking_stall_seconds"); }
	doubleEscCancel(): boolean { return this.bool("shell.behaviour.double_esc_cancel"); }
	/** A shortcut's key, or undefined when it is off. */
	key(name: ShellKey): string | undefined {
		const v = this.string(`shell.keys.${name}`).trim();
		return v === "" || v.toLowerCase() === "off" ? undefined : v;
	}

	/**
	 * Drop the retired keys from the settings file with `jhon settings migrate`
	 * (backed up first), then load again: its report's lines, or why it could not.
	 */
	migrateRetired(): { ok: true; lines: string[] } | { ok: false; error: string } {
		if (!this.#backend?.migrate) return { ok: false, error: `cannot migrate settings: ${this.error ?? "no jhon"}` };
		const result = this.#backend.migrate();
		this.refresh();
		return result;
	}

	/** Write one value with `jhon settings set`, then load again. */
	set(key: string, value: SettingValue): SetResult {
		return this.setMany([[key, value]]);
	}

	/** Write values in order, stopping at the first refusal, then load again once. */
	setMany(entries: readonly (readonly [string, SettingValue])[]): SetResult {
		if (!this.#backend) return { ok: false, error: `cannot save settings: ${this.error}` };
		let refused: string | null = null;
		for (const [key, value] of entries) {
			refused = this.#backend.set(key, String(value));
			if (refused !== null) break;
		}
		this.refresh();
		return refused === null ? { ok: true } : { ok: false, error: refused };
	}
}

/** The jhon binary jhon.json names in the agent dir (PI_CODING_AGENT_DIR, else ~/.pi/agent). */
export function settingsBin(env: NodeJS.ProcessEnv = process.env): string | undefined {
	return loadConfig(env.PI_CODING_AGENT_DIR || join(homedir(), ".pi", "agent"))?.jhon;
}

const SLOT = Symbol.for("jhon-shell.settings");
const state = globalThis as typeof globalThis & { [SLOT]?: ShellSettings };

/**
 * The process's settings: loaded at the first call and kept. Under node's
 * test runner they are the defaults unless a test installed its own.
 */
export function shellSettings(env: NodeJS.ProcessEnv = process.env): ShellSettings {
	if (state[SLOT]) return state[SLOT];
	let settings: ShellSettings;
	if (process.env.NODE_TEST_CONTEXT) settings = new ShellSettings(undefined, "no jhon under the test runner");
	else {
		const bin = settingsBin(env);
		settings = bin ? new ShellSettings(jhonSettingsBackend(bin)) : new ShellSettings(undefined, "jhon.json names no jhon binary; run `jhon sync`");
	}
	return (state[SLOT] = settings);
}

/** Replace the process's settings (undefined loads them again at the next read); returns the previous ones. */
export function installShellSettings(settings: ShellSettings | undefined): ShellSettings | undefined {
	const previous = state[SLOT];
	state[SLOT] = settings;
	return previous;
}

/**
 * The one notice an owner gets when the shell dropped retired settings
 * (T15.3): what went, why, what took its place, and where the backup is.
 * Silent retirements (a key that never chose anything) are not listed.
 */
export function retiredNotice(retired: readonly RetiredSetting[], report: readonly string[]): string {
	const lines = retired.filter((r) => !r.silent).map((r) => `  ${r.key} = ${JSON.stringify(r.was)}${r.now ? ` → ${r.now}` : ""}: ${r.why}`);
	return ["Settings changed; these went from settings.toml:", ...lines, ...report.filter((line) => line.startsWith("backup:"))].join("\n");
}
