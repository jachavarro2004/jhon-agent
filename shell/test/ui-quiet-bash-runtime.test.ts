import "./isolated-env.ts";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

// extensions/quiet.ts redraws pi's bash tool; this proves the command still
// runs as pi's own settings say (#107): the configured shell and command
// prefix, with and without the extension, through a real pi SDK session. It
// runs in a child process with a home, an agent dir and a PATH of its own.

const CHILD_FLAG = "JHON_TEST_BASH_CHILD";

/** In the child: one SDK session per case, a bash call, its text. */
async function runBashInSdk() {
	const sdk = await import("@earendil-works/pi-coding-agent");
	const cwd = process.cwd();
	const agentDir = process.env.PI_CODING_AGENT_DIR!;
	const shellPath = process.env.JHON_TEST_SHELL!;
	// No PATH and no Program Files Git: only the configured shell can run the
	// command, and on POSIX the prefix shows the configured executor ran it.
	for (const name of Object.keys(process.env)) if (name.toLowerCase() === "path") process.env[name] = "";
	process.env.ProgramFiles = join(cwd, "no-git");
	process.env["ProgramFiles(x86)"] = join(cwd, "no-git-x86");
	const modelRuntime = await sdk.ModelRuntime.create({ authPath: join(agentDir, "auth.json"), modelsPath: null, modelsStorePath: join(agentDir, "models.json"), allowModelNetwork: false });
	for (const withQuiet of [false, true]) {
		const extensions = withQuiet ? [fileURLToPath(new URL("../extensions/quiet.ts", import.meta.url))] : [];
		const runtime = await sdk.createAgentSessionRuntime(async ({ cwd, sessionManager, sessionStartEvent }) => {
			const settingsManager = sdk.SettingsManager.inMemory({ shellPath, shellCommandPrefix: "printf prefix-ran;", retry: { enabled: false }, compaction: { enabled: false } });
			const services = await sdk.createAgentSessionServices({
				cwd, agentDir, modelRuntime, settingsManager,
				resourceLoaderOptions: { noExtensions: true, noSkills: true, noPromptTemplates: true, noThemes: true, noContextFiles: true, additionalExtensionPaths: extensions },
			});
			assert.deepEqual(services.resourceLoader.getExtensions().errors, []);
			const created = await sdk.createAgentSessionFromServices({ services, sessionManager, sessionStartEvent, tools: ["bash"] });
			return { ...created, services, diagnostics: services.diagnostics };
		}, { cwd, agentDir, sessionManager: sdk.SessionManager.inMemory(cwd) });
		try {
			await runtime.session.bindExtensions({ mode: "print" });
			const bash = runtime.session.agent.state.tools.find((tool) => tool.name === "bash");
			assert.ok(bash, "the bash tool is there");
			const result = await bash.execute("call", { command: "printf ran-ok" }, new AbortController().signal);
			const text = result.content.flatMap((part) => (part.type === "text" ? [part.text] : [])).join("\n");
			assert.equal(text, "prefix-ranran-ok", `with quiet ${withQuiet}: pi's shell and prefix ran the command`);
			assert.equal(runtime.session.messages.length, 0, "a direct call adds no message");
			console.log(`bash under the SDK, quiet ${withQuiet}: ok`);
		} finally {
			await runtime.dispose();
		}
	}
}

if (process.env[CHILD_FLAG] === "1") {
	await runBashInSdk();
} else {
	test("bash runs with pi's configured shell and prefix, with the quiet tools and without them (#107)", (t) => {
		const bashes = process.platform === "win32"
			? [join(process.env.ProgramFiles ?? "C:\\Program Files", "Git", "bin", "bash.exe"), "C:\\Program Files\\Git\\usr\\bin\\bash.exe"]
			: ["/bin/bash", "/usr/bin/bash"];
		const shell = bashes.find(existsSync);
		if (!shell) { t.skip("no bash installed here; nothing is installed for the test"); return; }
		const root = mkdtempSync(join(tmpdir(), "jhon-quiet-bash-"));
		t.after(() => rmSync(root, { recursive: true, force: true }));
		const cwd = join(root, "project");
		const home = join(root, "home");
		const agentDir = join(home, "agent");
		for (const dir of [cwd, agentDir]) mkdirSync(dir, { recursive: true });
		const env = {
			...process.env, HOME: home, USERPROFILE: home, XDG_CONFIG_HOME: join(home, "config"), PI_CODING_AGENT_DIR: agentDir,
			JHON_SHELL_CONFIG_HOME: join(home, "jhon-shell"), PI_OFFLINE: "1", JHON_SHELL_QUIET_TOOLS: "1", [CHILD_FLAG]: "1", JHON_TEST_SHELL: shell,
		};
		const child = spawnSync(process.execPath, ["--experimental-strip-types", fileURLToPath(import.meta.url)], { cwd, env, encoding: "utf8", timeout: 30_000 });
		assert.ifError(child.error);
		assert.equal(child.status, 0, `${child.stdout}\n${child.stderr}`);
		assert.match(child.stdout, /quiet false: ok/);
		assert.match(child.stdout, /quiet true: ok/);
	});
}
