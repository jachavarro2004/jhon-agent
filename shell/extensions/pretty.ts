/**
 * shell/extensions/pretty.ts: pi-pretty (@heyhuynhgiabuu/pi-pretty, MIT:
 * highlighted reads, diffs and file trees) under the shell's UI. The render
 * pins the package with every resource filtered out, so pi installs it into
 * `<agent dir>/npm` without loading it; this extension loads it from there.
 * The shell keeps what is its own: the five tools it draws (extensions/quiet.ts),
 * the prompt editor, the working row and the thinking label.
 */
import {
	createBashToolDefinition,
	createFindToolDefinition,
	createGrepToolDefinition,
	createLsToolDefinition,
	createReadToolDefinition,
	type ExtensionAPI,
	type ExtensionContext,
} from "@earendil-works/pi-coding-agent";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { shellEnabled } from "../lib/ui/panel.ts";
import { installThinkingLabel } from "../lib/ui/thinking-label.ts";

/** pi-pretty's default export, as far as the shell calls it: an extension factory taking pi and its optional dependencies. */
type PiPrettyExtension = (...args: [pi: unknown, deps?: unknown]) => unknown;

/** The tools the shell draws itself (L76): pi-pretty is told to leave them alone. */
const SHELL_TOOLS = ["read", "bash", "ls", "find", "grep"] as const;
/** The UI calls that stay the shell's while pi-pretty runs in the TUI. */
const SHELL_UI = new Set(["setEditorComponent", "setWorkingIndicator", "setWorkingMessage", "setWorkingVisible"]);

/** A comma list of tool names with the shell's added: lower case, without blanks or repeats, the list's own first. */
function withShellTools(list: string | undefined): string {
	const names: string[] = [];
	for (const name of [...(list ?? "").split(","), ...SHELL_TOOLS]) {
		const tool = name.trim().toLowerCase();
		if (tool && !names.includes(tool)) names.push(tool);
	}
	return names.join(",");
}

/** pi-pretty's extension as pi installed it under `<agent dir>/npm`, or undefined when it is not there. */
export async function loadPiPretty(agentDir: string): Promise<PiPrettyExtension | undefined> {
	const root = join(agentDir, "npm", "node_modules", "@heyhuynhgiabuu", "pi-pretty");
	let entry: unknown;
	try {
		entry = JSON.parse(readFileSync(join(root, "package.json"), "utf8"))?.pi?.extensions?.[0];
	} catch {
		return undefined;
	}
	if (typeof entry !== "string") return undefined;
	const loaded = await import(pathToFileURL(join(root, entry)).href);
	const extension: unknown = typeof loaded === "function" ? loaded : loaded.default;
	if (typeof extension !== "function") throw new TypeError("pi-pretty must export an extension function");
	return extension as PiPrettyExtension;
}

/**
 * pi-pretty keeps its file index under deps.sdk.getAgentDir(), and under
 * $HOME/.pi/agent without one. Under `jhon start pi` it gets Jhon's agent
 * dir; since a deps.sdk also takes the place of the tool factories it would
 * import, the same five come with it. A host that passes its own sdk keeps it.
 */
function depsWithAgentDir(deps: unknown, env: NodeJS.ProcessEnv): unknown {
	const agentDir = env.PI_CODING_AGENT_DIR;
	const given = (deps ?? {}) as { sdk?: unknown };
	if (!agentDir || given.sdk !== undefined) return deps;
	const sdk = { createReadToolDefinition, createBashToolDefinition, createLsToolDefinition, createFindToolDefinition, createGrepToolDefinition, getAgentDir: () => agentDir };
	return { ...given, sdk };
}

/** In the TUI, a context whose UI ignores the calls that are the shell's. */
function withShellUi(ctx: ExtensionContext): ExtensionContext {
	if (ctx.mode !== "tui") return ctx;
	const ui = new Proxy(ctx.ui, { get: (target, name) => (SHELL_UI.has(String(name)) ? () => {} : Reflect.get(target, name)) });
	return new Proxy(ctx, { get: (target, name) => (name === "ui" ? ui : Reflect.get(target, name)) });
}

/** pi's API as pi-pretty sees it: the same, except that its handlers get withShellUi's context. */
function apiForPretty(api: ExtensionAPI): ExtensionAPI {
	type Handler = (event: unknown, ctx: ExtensionContext) => unknown;
	const on = (event: Parameters<ExtensionAPI["on"]>[0], handler: Handler) => {
		api.on(event, ((payload: unknown, ctx: ExtensionContext) => handler(payload, withShellUi(ctx))) as never);
	};
	return new Proxy(api, { get: (target, name) => (name === "on" ? on : Reflect.get(target, name)) });
}

/** Runs `fn` with an environment variable set, and puts the variable back as it was. */
async function withEnv<T>(name: string, value: string, fn: () => Promise<T> | T): Promise<T> {
	const before = process.env[name];
	process.env[name] = value;
	try {
		return await fn();
	} finally {
		if (before === undefined) delete process.env[name];
		else process.env[name] = before;
	}
}

export default async function jhonPretty(
	pi: unknown,
	deps?: unknown,
	upstream?: PiPrettyExtension,
	env: NodeJS.ProcessEnv = process.env,
): Promise<unknown> {
	// A sub-agent has no transcript to dress, and pi-pretty would index files in every child.
	if (env.JHON_PI_CHILD === "1") return undefined;
	// The thinking row's label is the shell's, with pi-pretty or without (lib/ui/thinking-label.ts).
	installThinkingLabel(pi as ExtensionAPI, env);
	const extension = upstream ?? (env.PI_CODING_AGENT_DIR ? await loadPiPretty(env.PI_CODING_AGENT_DIR) : undefined);
	if (!extension) return undefined;
	process.env.PRETTY_DISABLE_TOOLS = withShellTools(process.env.PRETTY_DISABLE_TOOLS);
	const prettyDeps = depsWithAgentDir(deps, env);
	if (!shellEnabled(env)) return extension(pi, prettyDeps);
	const api = pi as ExtensionAPI;
	// pi-pretty's animated thinking indicator is global and would move every
	// old Thinking row; the shell's label stays still (lib/ui/thinking-label.ts).
	const result = await withEnv("PRETTY_THINKING_INDICATOR", "off", () => extension(apiForPretty(api), prettyDeps));
	// The prompt frame shows the working state, so pi's own loader row would say it twice.
	api.on("session_start", (_event, ctx) => {
		if (ctx.mode === "tui") ctx.ui.setWorkingVisible(false);
	});
	api.on("session_shutdown", (_event, ctx) => {
		if (ctx.mode !== "tui") return;
		ctx.ui.setWorkingVisible(true);
		ctx.ui.setWorkingIndicator();
		ctx.ui.setWorkingMessage();
	});
	return result;
}
