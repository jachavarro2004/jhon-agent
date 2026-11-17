import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import pretty, { loadPiPretty } from "../extensions/pretty.ts";

test("pretty cannot replace or restore an editor owned by the host", async () => {
	const handlers = new Map<string, Function[]>();
	const pi = { on(name: string, handler: Function) { handlers.set(name, [...(handlers.get(name) ?? []), handler]); } };
	const owner = () => "custom";
	let editor: unknown = owner;
	let indicator: unknown;
	let message: unknown;
	let visible = true;
	const ctx = { mode: "tui", ui: {
		getEditorComponent: () => editor,
		setEditorComponent: (value: unknown) => { editor = value; },
		setWorkingIndicator: (value: unknown) => { indicator = value; },
		setWorkingMessage: (value: unknown) => { message = value; },
		setWorkingVisible(value: boolean) { visible = value; },
		theme: { fg: (role: string, text: string) => `<${role}>${text}</${role}>`, bold: (text: string) => text },
	} };
	const previousThinkingIndicator = process.env.PRETTY_THINKING_INDICATOR;
	let thinkingIndicatorDuringActivation: string | undefined;
	await pretty(pi, undefined, async (api: any) => {
		thinkingIndicatorDuringActivation = process.env.PRETTY_THINKING_INDICATOR;
		api.on("session_start", (_event: unknown, context: any) => {
			context.ui.setEditorComponent(() => "pretty");
			context.ui.setWorkingIndicator({ frames: ["foreign"] });
		});
		api.on("session_shutdown", (_event: unknown, context: any) => context.ui.setEditorComponent(undefined));
	}, {});
	assert.equal(thinkingIndicatorDuringActivation, "off", "the shell UI disables pi-pretty's global Thinking shimmer");
	assert.equal(process.env.PRETTY_THINKING_INDICATOR, previousThinkingIndicator, "activation must restore the caller's environment");
	for (const handler of handlers.get("session_start") ?? []) await handler({}, ctx);
	assert.equal(editor, owner);
	assert.equal(visible, false, "the prompt frame owns the live working state without a duplicate loader row");
	assert.equal(indicator, undefined);
	assert.equal(message, undefined);
	for (const handler of handlers.get("session_shutdown") ?? []) await handler({}, ctx);
	assert.equal(editor, owner);
	assert.equal(visible, true);
	assert.equal(indicator, undefined);
	assert.equal(message, undefined);
});

test("a disabled shell UI leaves pi-pretty's editor behavior untouched", async () => {
	let received: unknown;
	const pi = {};
	await pretty(pi, undefined, async (api: unknown) => { received = api; }, { JHON_SHELL: "0" });
	assert.equal(received, pi);
});

// A delegated child has no transcript to prettify, so the
// upstream fallback would start FFF indexing in every Jhon sub-agent.
test("Jhon's sub-agents never load upstream pi-pretty", async () => {
	let loaded = false;
	const pi = {};
	const result = await pretty(pi, undefined, async () => { loaded = true; }, { JHON_PI_CHILD: "1" });
	assert.equal(loaded, false);
	assert.equal(result, undefined);
});

// Without an agent dir Pi's deps reach pi-pretty unchanged, so its FFF store
// stays where it always was ($HOME/.pi/agent).
test("without an agent dir pi-pretty gets Pi's deps unchanged", async () => {
	const given = { TextComponent: "text" };
	for (const env of [{}, { JHON_SHELL: "0" }]) {
		for (const deps of [undefined, given]) {
			let received: unknown = "unset";
			await pretty({ on() {} }, deps, async (_api: unknown, forwarded: unknown) => { received = forwarded; }, env);
			assert.equal(received, deps, JSON.stringify(env));
		}
	}
});

// Under `jhon start pi` the FFF store follows Jhon's agent dir, never the owner's ~/.pi/agent.
test("with an agent dir pi-pretty gets it and Pi's tool factories through deps.sdk", async () => {
	const factories = ["createReadToolDefinition", "createBashToolDefinition", "createLsToolDefinition", "createFindToolDefinition", "createGrepToolDefinition"];
	for (const env of [{ PI_CODING_AGENT_DIR: "/agent-home" }, { PI_CODING_AGENT_DIR: "/agent-home", JHON_SHELL: "0" }]) {
		let received: any;
		await pretty({ on() {} }, { TextComponent: "text" }, async (_api: unknown, deps: unknown) => { received = deps; }, env);
		assert.equal(received.TextComponent, "text", "the host's other deps are kept");
		assert.equal(received.sdk.getAgentDir(), "/agent-home");
		for (const name of factories) assert.equal(typeof received.sdk[name], "function", name);
	}
	const sdk = { getAgentDir: () => "/host" };
	let kept: any;
	await pretty({ on() {} }, { sdk }, async (_api: unknown, deps: unknown) => { kept = deps; }, { PI_CODING_AGENT_DIR: "/agent-home" });
	assert.equal(kept.sdk, sdk, "a host that already passes its own sdk keeps it");
});

test("upstream pi-pretty loads from the agent dir's npm install, where pi installs it, and is skipped when absent", async (t) => {
	const agentDir = mkdtempSync(join(tmpdir(), "jhon-pretty-"));
	t.after(() => rmSync(agentDir, { recursive: true, force: true }));
	assert.equal(await loadPiPretty(agentDir), undefined, "no install, no pi-pretty");
	const root = join(agentDir, "npm", "node_modules", "@heyhuynhgiabuu", "pi-pretty");
	mkdirSync(join(root, "src"), { recursive: true });
	writeFileSync(join(root, "package.json"), JSON.stringify({ name: "@heyhuynhgiabuu/pi-pretty", type: "module", pi: { extensions: ["./src/index.js"] } }));
	writeFileSync(join(root, "src", "index.js"), "export default function prettyFake() { return \"fake\"; }\n");
	const extension = await loadPiPretty(agentDir);
	assert.equal(typeof extension, "function");
	assert.equal(await extension!({}, undefined), "fake");
});
