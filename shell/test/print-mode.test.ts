/**
 * T8f (odd/tasks/studio-integration-v1.md, L47): `jhon start pi -p` runs the
 * agent once and prints its answer. pi's print mode ends the session as soon
 * as the prompt it was given returns; a command such as /jhon-ldd only starts
 * the run its prompt asks for, so before T8f pi disposed the session under
 * that run: nothing was printed and every extension's handler failed with
 * "This extension ctx is stale after session replacement or reload".
 *
 * The shell runs here as `jhon start pi` loads it: pi's own CLI entry (the
 * package's bin), an agent dir whose settings.json names this shell package,
 * and pi's offline faux model (test/fixtures/faux-provider.ts).
 */
import "./isolated-env.ts";
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const SHELL = join(dirname(fileURLToPath(import.meta.url)), "..");
const FAUX = join(SHELL, "test", "fixtures", "faux-provider.ts");
/** pi's CLI, as its package's bin names it. */
const PI_CLI = (() => {
	let dir = dirname(fileURLToPath(import.meta.resolve("@earendil-works/pi-coding-agent")));
	while (!existsSync(join(dir, "package.json"))) dir = dirname(dir);
	return join(dir, (JSON.parse(readFileSync(join(dir, "package.json"), "utf8")) as { bin: { pi: string } }).bin.pi);
})();

interface Run { code: number | null; stdout: string; stderr: string }

/** `pi -p <prompt>` on the faux model, in a home, agent dir and project of its own. */
function printRun(t: { after(fn: () => void): void }, prompt: string): Promise<Run> {
	const scratch = mkdtempSync(join(tmpdir(), "jhon-print-mode-"));
	t.after(() => rmSync(scratch, { recursive: true, force: true }));
	const home = join(scratch, "home"), agentDir = join(home, ".pi", "jhon"), project = join(scratch, "project");
	for (const dir of [agentDir, project]) mkdirSync(dir, { recursive: true });
	writeFileSync(join(agentDir, "settings.json"), JSON.stringify({ packages: [SHELL], quietStartup: true }));
	const env: NodeJS.ProcessEnv = {};
	// The parent's pi session (PI_SESSION_FILE, PI_PROVIDER, ...) is not this run's.
	for (const [name, value] of Object.entries(process.env)) if (!name.startsWith("PI_") && name !== "NODE_TEST_CONTEXT") env[name] = value;
	Object.assign(env, { HOME: home, XDG_CONFIG_HOME: join(home, ".config"), XDG_DATA_HOME: join(home, ".local", "share"), PI_CODING_AGENT_DIR: agentDir, PI_OFFLINE: "1" });
	return new Promise((resolve, reject) => {
		const run = spawn(process.execPath, [PI_CLI, "-e", FAUX, "--provider", "faux", "--model", "faux-1", "-p", prompt], { cwd: project, env, stdio: ["ignore", "pipe", "pipe"] });
		let stdout = "", stderr = "";
		run.stdout.setEncoding("utf8").on("data", (d: string) => { stdout += d; });
		run.stderr.setEncoding("utf8").on("data", (d: string) => { stderr += d; });
		const timer = setTimeout(() => run.kill("SIGKILL"), 90_000);
		run.once("error", (error) => { clearTimeout(timer); reject(error); });
		run.once("close", (code) => { clearTimeout(timer); resolve({ code, stdout, stderr }); });
	});
}

test("pi -p with a command that starts a run (/jhon-ldd) waits for that run: the answer is printed, exit 0, nothing on stderr", { timeout: 120_000 }, async (t) => {
	const run = await printRun(t, "/jhon-ldd status");
	assert.deepEqual(run, { code: 0, stdout: "ok\n", stderr: "" });
});
