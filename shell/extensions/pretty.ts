/**
 * shell/extensions/pretty.ts — pi-pretty (@heyhuynhgiabuu/pi-pretty, MIT:
 * syntax-highlighted reads, diffs, and file trees) under jhon-shell's UI. The
 * render lists the package pinned with every resource filtered out, so pi
 * installs it into `<agent dir>/npm` without loading it; this extension loads
 * it from there and keeps the prompt editor and the working row the UI's.
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
import { shellEnabled } from "../lib/ui/shell-bar.ts";

type PiPrettyExtension = (pi: unknown, deps?: unknown) => unknown;

// jhon-shell's quiet tools (extensions/quiet.ts) draw these; pi-pretty leaves them to it.
const QUIET_TOOL_NAMES = ["read", "bash", "ls", "find", "grep"] as const;

function mergeDisabledTools(existing: string | undefined, tools: readonly string[]): string {
	const disabled = new Set((existing ?? "").split(",").map((tool) => tool.trim().toLowerCase()).filter(Boolean));
	for (const tool of tools) disabled.add(tool);
	return [...disabled].join(",");
}

/** pi-pretty's extension as pi installed it under `<agent dir>/npm`, or undefined when it is not installed. */
export async function loadPiPretty(agentDir: string): Promise<PiPrettyExtension | undefined> {
	const root = join(agentDir, "npm", "node_modules", "@heyhuynhgiabuu", "pi-pretty");
	let entry: string;
	try {
		const manifest = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
		entry = manifest?.pi?.extensions?.[0];
		if (typeof entry !== "string") return undefined;
	} catch {
		return undefined;
	}
	const module = await import(pathToFileURL(join(root, entry)).href);
	const extension: unknown = typeof module === "function" ? module : module.default;
	if (typeof extension !== "function") throw new TypeError("pi-pretty must export an extension function");
	return extension as PiPrettyExtension;
}

// pi-pretty keeps its FFF database under deps.sdk.getAgentDir(), else
// $HOME/.pi/agent. Under `jhon start pi` it gets Jhon's agent dir; a deps.sdk
// also replaces the host tool factories it would otherwise import, so the same
// five come along. A host that passes its own sdk keeps it.
function withAgentDir(deps: unknown, env: NodeJS.ProcessEnv): unknown {
	const agentDir = env.PI_CODING_AGENT_DIR;
	const given = (deps ?? {}) as { sdk?: unknown };
	if (!agentDir || given.sdk !== undefined) return deps;
	return {
		...given,
		sdk: {
			createReadToolDefinition,
			createBashToolDefinition,
			createLsToolDefinition,
			createFindToolDefinition,
			createGrepToolDefinition,
			getAgentDir: () => agentDir,
		},
	};
}

export default async function jhonPretty(
	pi: unknown,
	deps?: unknown,
	upstream?: PiPrettyExtension,
	env: NodeJS.ProcessEnv = process.env,
): Promise<unknown> {
	// A sub-agent has no transcript to prettify, and pi-pretty would index files in every child.
	if (env.JHON_PI_CHILD === "1") return undefined;
	const extension = upstream ?? (env.PI_CODING_AGENT_DIR ? await loadPiPretty(env.PI_CODING_AGENT_DIR) : undefined);
	if (!extension) return undefined;
	// The shell always draws these tools (L76): pi-pretty never registers its own.
	process.env.PRETTY_DISABLE_TOOLS = mergeDisabledTools(process.env.PRETTY_DISABLE_TOOLS, QUIET_TOOL_NAMES);
	deps = withAgentDir(deps, env);
	if (!shellEnabled(env)) return extension(pi, deps);
	const api = pi as ExtensionAPI;
	// Its tools, autocomplete, and status pass through; the prompt editor and the
	// working row stay the shell UI's, which also blocks stale restores.
	const owned = new Set(["setEditorComponent", "setWorkingIndicator", "setWorkingMessage", "setWorkingVisible"]);
	const delegated = new Proxy(api, {
		get(target, key) {
			if (key !== "on") return Reflect.get(target, key);
			return (event: Parameters<ExtensionAPI["on"]>[0], handler: (event: unknown, ctx: ExtensionContext) => unknown) => {
				api.on(event, ((event: unknown, ctx: ExtensionContext) => {
					if (ctx.mode !== "tui") return handler(event, ctx);
					const ui = new Proxy(ctx.ui, { get: (target, key) => owned.has(String(key)) ? () => {} : Reflect.get(target, key) });
					return handler(event, new Proxy(ctx, { get: (target, key) => key === "ui" ? ui : Reflect.get(target, key) }));
				}) as never);
			};
		},
	});
	// pi-pretty's hidden-thinking animation is global and can animate every
	// historical Thinking row; the shell keeps collapsed labels static.
	const previousThinkingIndicator = process.env.PRETTY_THINKING_INDICATOR;
	process.env.PRETTY_THINKING_INDICATOR = "off";
	let result: unknown;
	try {
		result = await extension(delegated, deps);
	} finally {
		if (previousThinkingIndicator === undefined) delete process.env.PRETTY_THINKING_INDICATOR;
		else process.env.PRETTY_THINKING_INDICATOR = previousThinkingIndicator;
	}
	// The prompt frame carries the live working state, so pi's separate loader
	// row would repeat it.
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
