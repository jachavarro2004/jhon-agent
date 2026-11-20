/**
 * The shell suite never reads the machine owner's Jhon settings or state
 * (T147, L1209): every test file imports this module first, so each test
 * process starts, before any other module loads, with
 *
 *   - JHON_SETTINGS on a settings file of its own that sets only
 *     sandbox.mode = "off" (the owner's "on" once pinned the sandbox in
 *     extension.test.ts, and since T128f it is the default on Linux): a test
 *     that wants the sandbox or the default writes its own file;
 *   - XDG_STATE_HOME (grants, modes, session grants, the LDD switch) and
 *     XDG_CACHE_HOME (jhon's caches) on scratch directories;
 *   - JHON_PROFILES on a profile file that does not exist;
 *   - no JHON_SANDBOX_PROJECT (a sandbox pin of the session that runs the
 *     suite) and none of the variables that override a setting
 *     (engine/internal/settings/settings.go, `env:`).
 *
 * The parent's values are replaced, not kept: a suite run from a shell whose
 * JHON_SETTINGS names the owner's file is isolated all the same. A test that
 * needs its own value sets it after this import, as before. The scratch
 * directory is removed when the process exits.
 *
 * `node --test test/*.test.ts` runs each file in a process of its own and has
 * no per-process setup file, so the import is in every file;
 * harness-isolation.test.ts refuses a test file without it.
 */
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

/** Variables that override a Jhon setting (settings.go, `env:`): the owner's, never the suite's. */
export const SETTING_OVERRIDES: readonly string[] = Object.freeze([
	"JHON_TELEMETRY",
	"JHON_SHELL_ASCII",
	"JHON_SHELL_COMPACT_AT",
	"JHON_SHELL_STALL_SECONDS",
	"JHON_SHELL_THINKING_STALL_SECONDS",
	"JHON_SHELL_DOUBLE_ESC_CANCEL",
	"JHON_SHELL_JHON_KEY",
	"JHON_SHELL_TASKS_KEY",
	"JHON_SHELL_JOBS_KEY",
	"JHON_SHELL_USAGE_KEY",
	"JHON_SHELL_TODO_KEY",
]);

/** The scratch directory this process's Jhon settings and state live in. */
export const ISOLATED_ROOT = mkdtempSync(join(tmpdir(), "jhon-shell-test-env-"));
process.on("exit", () => rmSync(ISOLATED_ROOT, { recursive: true, force: true }));

/** A settings file with every setting at its default but sandbox.mode, off. */
export const ISOLATED_SETTINGS = join(ISOLATED_ROOT, "settings.toml");
writeFileSync(ISOLATED_SETTINGS, '[sandbox]\nmode = "off"\n');
for (const dir of ["state", "cache"]) mkdirSync(join(ISOLATED_ROOT, dir));

Object.assign(process.env, {
	JHON_SETTINGS: ISOLATED_SETTINGS,
	XDG_STATE_HOME: join(ISOLATED_ROOT, "state"),
	XDG_CACHE_HOME: join(ISOLATED_ROOT, "cache"),
	JHON_PROFILES: join(ISOLATED_ROOT, "profiles.json"),
});
delete process.env.JHON_SANDBOX_PROJECT;
for (const name of SETTING_OVERRIDES) delete process.env[name];
