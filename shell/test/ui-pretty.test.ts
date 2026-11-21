import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import pretty, { loadPiPretty } from "../extensions/pretty.ts";

// extensions/pretty.ts runs pi-pretty under the shell, with a stand-in for
// pi-pretty: what the stand-in receives and does is what pi-pretty would.

type Handler = (event: unknown, ctx: unknown) => unknown;
function stubPi() {
	const handlers = new Map<string, Handler[]>();
	return {
		handlers,
		pi: { on: (name: string, handler: Handler) => { handlers.set(name, [...(handlers.get(name) ?? []), handler]); }, extra: "kept" },
		fire: async (name: string, ctx: unknown) => { for (const handler of handlers.get(name) ?? []) await handler({}, ctx); },
	};
}

/** A context whose UI records the shell-owned calls that reach it. */
function uiContext(mode: string) {
	const calls: string[] = [];
	const record = (name: string) => (...args: unknown[]) => { calls.push(`${name}(${args.map((arg) => JSON.stringify(arg)).join(",")})`); };
	const ui = {
		setEditorComponent: record("setEditorComponent"), setWorkingIndicator: record("setWorkingIndicator"),
		setWorkingMessage: record("setWorkingMessage"), setWorkingVisible: record("setWorkingVisible"),
		notify: record("notify"), theme: { name: "t" },
	};
	return { ctx: { mode, ui, cwd: "/w" }, calls };
}

test("in the TUI pi-pretty cannot touch the editor or the working row; the shell hides pi's loader row and gives it back at shutdown", async () => {
	const host = stubPi();
	const indicatorBefore = process.env.PRETTY_THINKING_INDICATOR;
	let indicatorDuring: string | undefined;
	let seen: { cwd?: string; theme?: unknown } = {};
	await pretty(host.pi, undefined, async (api: any) => {
		indicatorDuring = process.env.PRETTY_THINKING_INDICATOR;
		assert.equal(api.extra, "kept", "the rest of pi's API passes through");
		api.on("session_start", (_event: unknown, ctx: any) => {
			ctx.ui.setEditorComponent(() => "pretty");
			ctx.ui.setWorkingIndicator({ frames: ["x"] });
			ctx.ui.setWorkingMessage("busy");
			ctx.ui.setWorkingVisible(true);
			ctx.ui.notify("hello");
			seen = { cwd: ctx.cwd, theme: ctx.ui.theme };
		});
		api.on("session_shutdown", (_event: unknown, ctx: any) => ctx.ui.setEditorComponent(undefined));
	}, {});
	assert.equal(indicatorDuring, "off", "pi-pretty's global thinking animation is off while it loads");
	assert.equal(process.env.PRETTY_THINKING_INDICATOR, indicatorBefore, "and the variable is back as it was");
	const tui = uiContext("tui");
	await host.fire("session_start", tui.ctx);
	assert.deepEqual(tui.calls, ['notify("hello")', "setWorkingVisible(false)"], "only the shell's own call reaches the UI");
	assert.deepEqual(seen, { cwd: "/w", theme: { name: "t" } });
	tui.calls.length = 0;
	await host.fire("session_shutdown", tui.ctx);
	assert.deepEqual(tui.calls, ["setWorkingVisible(true)", "setWorkingIndicator()", "setWorkingMessage()"]);
	const print = uiContext("print");
	await host.fire("session_start", print.ctx);
	await host.fire("session_shutdown", print.ctx);
	// A function and undefined have no JSON: both show as an empty argument.
	assert.deepEqual(print.calls, ["setEditorComponent()", 'setWorkingIndicator({"frames":["x"]})', 'setWorkingMessage("busy")', "setWorkingVisible(true)", 'notify("hello")', "setEditorComponent()"], "outside the TUI pi-pretty's calls pass and the shell adds none");
});

test("PRETTY_THINKING_INDICATOR comes back even when pi-pretty throws, and is removed when it was unset", async () => {
	const before = process.env.PRETTY_THINKING_INDICATOR;
	try {
		process.env.PRETTY_THINKING_INDICATOR = "spin";
		await assert.rejects(pretty(stubPi().pi, undefined, async () => { throw new Error("broken"); }, {}), /broken/);
		assert.equal(process.env.PRETTY_THINKING_INDICATOR, "spin");
		delete process.env.PRETTY_THINKING_INDICATOR;
		await pretty(stubPi().pi, undefined, async () => {}, {});
		assert.equal("PRETTY_THINKING_INDICATOR" in process.env, false);
	} finally {
		if (before === undefined) delete process.env.PRETTY_THINKING_INDICATOR; else process.env.PRETTY_THINKING_INDICATOR = before;
	}
});

test("pi-pretty is told to leave the shell's five tools, added to what the variable named", async () => {
	const before = process.env.PRETTY_DISABLE_TOOLS;
	try {
		process.env.PRETTY_DISABLE_TOOLS = " Diff, , read,EDIT";
		await pretty(stubPi().pi, undefined, async () => {}, {});
		assert.equal(process.env.PRETTY_DISABLE_TOOLS, "diff,read,edit,bash,ls,find,grep");
		delete process.env.PRETTY_DISABLE_TOOLS;
		await pretty({}, undefined, async () => {}, { JHON_SHELL: "0" });
		assert.equal(process.env.PRETTY_DISABLE_TOOLS, "read,bash,ls,find,grep", "with the shell UI off too");
	} finally {
		if (before === undefined) delete process.env.PRETTY_DISABLE_TOOLS; else process.env.PRETTY_DISABLE_TOOLS = before;
	}
});

test("with the shell UI off pi-pretty gets pi itself", async () => {
	const pi = {};
	let got: unknown;
	assert.equal(await pretty(pi, undefined, async (api: unknown) => { got = api; return "its result"; }, { JHON_SHELL: "0" }), "its result");
	assert.equal(got, pi);
});

// A sub-agent has no transcript; pi-pretty there would index files in every child.
test("a sub-agent loads nothing, not even the thinking label", async () => {
	let loaded = false;
	const host = stubPi();
	assert.equal(await pretty(host.pi, undefined, async () => { loaded = true; }, { JHON_PI_CHILD: "1" }), undefined);
	assert.equal(loaded, false);
	assert.equal(host.handlers.size, 0);
});

test("with no pi-pretty installed only the thinking label is set up", async () => {
	const host = stubPi();
	assert.equal(await pretty(host.pi, undefined, undefined, {}), undefined);
	assert.deepEqual([...host.handlers.keys()].sort(), ["agent_start", "session_shutdown", "session_start"]);
});

// Without an agent dir the deps go as pi gave them: pi-pretty's index stays in $HOME/.pi/agent.
test("without an agent dir pi-pretty gets pi's deps untouched", async () => {
	for (const env of [{}, { JHON_SHELL: "0" }]) {
		for (const deps of [undefined, { TextComponent: "text" }]) {
			let got: unknown = "unset";
			await pretty({ on() {} }, deps, async (_api: unknown, given: unknown) => { got = given; }, env);
			assert.equal(got, deps, JSON.stringify(env));
		}
	}
});

// Under `jhon start pi` the index follows Jhon's agent dir, never the owner's ~/.pi/agent.
test("with an agent dir pi-pretty gets it, and pi's five tool factories, through deps.sdk; a host sdk is kept", async () => {
	const factories = ["createReadToolDefinition", "createBashToolDefinition", "createLsToolDefinition", "createFindToolDefinition", "createGrepToolDefinition"];
	for (const env of [{ PI_CODING_AGENT_DIR: "/agents" }, { PI_CODING_AGENT_DIR: "/agents", JHON_SHELL: "0" }]) {
		for (const deps of [{ TextComponent: "text" }, undefined]) {
			let got: any;
			await pretty({ on() {} }, deps, async (_api: unknown, given: unknown) => { got = given; }, env);
			if (deps) assert.equal(got.TextComponent, "text");
			assert.equal(got.sdk.getAgentDir(), "/agents");
			assert.deepEqual(Object.keys(got.sdk), [...factories, "getAgentDir"]);
			for (const name of factories) assert.equal(typeof got.sdk[name], "function", name);
		}
	}
	const sdk = { getAgentDir: () => "/host" };
	let kept: any;
	await pretty({ on() {} }, { sdk }, async (_api: unknown, given: unknown) => { kept = given; }, { PI_CODING_AGENT_DIR: "/agents" });
	assert.equal(kept.sdk, sdk);
});

test("pi-pretty loads from where pi installs it in the agent dir; a missing or unusable install is skipped, a wrong export refused", async (t) => {
	const agentDir = mkdtempSync(join(tmpdir(), "jhon-pretty-"));
	t.after(() => rmSync(agentDir, { recursive: true, force: true }));
	assert.equal(await loadPiPretty(agentDir), undefined, "not installed");
	const root = join(agentDir, "npm", "node_modules", "@heyhuynhgiabuu", "pi-pretty");
	mkdirSync(join(root, "src"), { recursive: true });
	const manifest = (pi: unknown) => writeFileSync(join(root, "package.json"), JSON.stringify({ name: "@heyhuynhgiabuu/pi-pretty", type: "module", pi }));
	writeFileSync(join(root, "package.json"), "{ not json");
	assert.equal(await loadPiPretty(agentDir), undefined, "an unreadable manifest");
	manifest({ extensions: [] });
	assert.equal(await loadPiPretty(agentDir), undefined, "no extension named");
	manifest({ extensions: ["./src/index.js"] });
	writeFileSync(join(root, "src", "index.js"), "export default function stand() { return \"stand-in\"; }\n");
	const extension = await loadPiPretty(agentDir);
	assert.equal(await extension!({}, undefined), "stand-in");
	manifest({ extensions: ["./src/other.js"] });
	writeFileSync(join(root, "src", "other.js"), "export default 42;\n");
	await assert.rejects(loadPiPretty(agentDir), { name: "TypeError", message: "pi-pretty must export an extension function" });
	let got: unknown;
	manifest({ extensions: ["./src/index.js"] });
	assert.equal(await pretty({ on() {} }, undefined, undefined, { PI_CODING_AGENT_DIR: agentDir, JHON_SHELL: "0" }).then((value) => { got = value; return value; }), "stand-in", "the installed one runs when none is given");
	assert.equal(got, "stand-in");
});
