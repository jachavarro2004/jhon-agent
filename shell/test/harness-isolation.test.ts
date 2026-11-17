/**
 * The shell suite never reads the machine owner's Jhon settings or state
 * (T147, L1209, L1213): isolated-env.ts, which every test file imports first,
 * puts JHON_SETTINGS on an empty scratch file and Jhon's state and caches on
 * scratch directories, whatever the parent environment names.
 */
import "./isolated-env.ts";
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { ISOLATED_ROOT, ISOLATED_SETTINGS, SETTING_OVERRIDES } from "./isolated-env.ts";

const TEST_DIR = dirname(fileURLToPath(import.meta.url));
const SHELL = join(TEST_DIR, "..");
const BIN = process.env.JHON_TEST_BIN || join(SHELL, "..", "build", "jhon");
const ISOLATION = 'import "./isolated-env.ts";';

test("every test file imports isolated-env.ts before any other module", () => {
	const files = readdirSync(TEST_DIR).filter((f) => f.endsWith(".test.ts"));
	assert.ok(files.length > 1, "no test files found");
	const missing = files.filter((f) => {
		const first = readFileSync(join(TEST_DIR, f), "utf8").split("\n").find((line) => /^import\b/.test(line));
		return first !== ISOLATION;
	});
	assert.deepEqual(missing, [], `these test files do not start with ${ISOLATION}`);
});

test("a test process holds an empty settings file and Jhon state of its own, whatever its parent names", () => {
	assert.equal(process.env.JHON_SETTINGS, ISOLATED_SETTINGS);
	assert.equal(readFileSync(ISOLATED_SETTINGS, "utf8"), "");
	assert.equal(process.env.XDG_STATE_HOME, join(ISOLATED_ROOT, "state"));
	assert.equal(process.env.XDG_CACHE_HOME, join(ISOLATED_ROOT, "cache"));
	assert.equal(process.env.JHON_PROFILES, join(ISOLATED_ROOT, "profiles.json"));
	assert.equal(process.env.JHON_SANDBOX_PROJECT, undefined);
	for (const name of SETTING_OVERRIDES) assert.equal(process.env[name], undefined, name);
});

// L1209: with the owner's settings at sandbox.mode on, these two tests got
// "the sandbox was pinned by this call, after the session started".
test("with the parent's JHON_SETTINGS on a file holding sandbox.mode on, the extension's ask and grant tests pass", (t) => {
	if (!existsSync(BIN)) { t.skip(`needs the tree's jhon (JHON_TEST_BIN or build/jhon): ${BIN}`); return; }
	const scratch = mkdtempSync(join(tmpdir(), "jhon-shell-harness-"));
	t.after(() => rmSync(scratch, { recursive: true, force: true }));
	const owner = join(scratch, "settings.toml");
	writeFileSync(owner, '[sandbox]\nmode = "on"\n');
	// The file is what the owner's was: the binary reads sandbox.mode on from it.
	const listed = spawnSync(BIN, ["settings", "list", "--json"], { encoding: "utf8", env: { ...process.env, JHON_SETTINGS: owner } });
	assert.equal(listed.status, 0, listed.stderr);
	const mode = JSON.parse(listed.stdout).settings.find((s: { key: string }) => s.key === "sandbox.mode");
	assert.deepEqual([mode?.value, mode?.source], ["on", "file"]);

	const env: NodeJS.ProcessEnv = { ...process.env, JHON_SETTINGS: owner, JHON_TEST_BIN: BIN };
	delete env.NODE_TEST_CONTEXT; // a run of its own, not a child of this runner
	const run = spawnSync(process.execPath, [
		"--experimental-strip-types", "--test", "--test-reporter=tap", "--test-timeout=120000",
		"--test-name-pattern=^tool_call blocks a refusal, puts an ask to the owner",
		"--test-name-pattern=^the owner's yes is always on record",
		"test/extension.test.ts",
	], { cwd: SHELL, encoding: "utf8", env, timeout: 300_000 });
	const out = run.stdout + run.stderr;
	assert.doesNotMatch(out, /the sandbox was pinned by this call/);
	assert.match(out, /^# pass 2$/m, out);
	assert.match(out, /^# fail 0$/m, out);
	assert.equal(run.status, 0, out);
});
