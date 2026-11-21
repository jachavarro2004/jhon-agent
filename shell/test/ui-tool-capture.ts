/**
 * What extensions/ui.ts shows the model: the session_worktree_register tool
 * as registered, its result, and its refusals, from scripted calls against a
 * stand-in pi and a stub of the clone check. ui-model-text.test.ts compares
 * it with the snapshot taken before the T174-S14 rewrite.
 *
 * Nothing here reads Git, the clock or a path of the machine.
 */
import "./isolated-env.ts";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import installShellUi from "../extensions/ui.ts";

type Handler = (event: unknown, ctx: ExtensionContext) => unknown;
interface Tool { name: string; label: string; description: string; parameters: unknown; renderShell?: string; promptSnippet?: unknown; promptGuidelines?: unknown; execute(id: string, params: unknown, signal: undefined, update: undefined, ctx: ExtensionContext): Promise<unknown> }

function stand() {
	const handlers = new Map<string, Handler[]>();
	const tools = new Map<string, Tool>();
	const appended: unknown[] = [];
	const pi = new Proxy({
		on: (name: string, handler: Handler) => { handlers.set(name, [...(handlers.get(name) ?? []), handler]); },
		registerTool: (tool: Tool) => { tools.set(tool.name, tool); },
		appendEntry: (type: string, data: unknown) => { appended.push({ type, data }); },
		events: { on: () => () => {}, emit: () => {} },
		getThinkingLevel: () => "off",
	} as Record<string, unknown>, { get: (target, name) => (name in target ? target[name as string] : () => undefined) }) as unknown as ExtensionAPI;
	return { pi, handlers, tools, appended };
}

export async function capture() {
	const { pi, handlers, tools, appended } = stand();
	// The clone check: /repo and its worktrees are one clone, /other another.
	const resolveWorktree = (path: string) => (path === "/missing" ? undefined : { root: path.startsWith("/other") ? "/other" : path.startsWith("/repo-b") ? "/repo-b" : "/repo", commonDir: path.startsWith("/other") ? "/other/.git" : "/repo/.git" });
	installShellUi(pi, { JHON_SHELL: "1" }, { resolveWorktree, rootExists: () => true, lddStatus: async () => undefined, now: () => 0 });
	const tool = tools.get("session_worktree_register")!;
	const definition = { name: tool.name, label: tool.label, description: tool.description, parameters: tool.parameters, renderShell: tool.renderShell, promptSnippet: tool.promptSnippet ?? null, promptGuidelines: tool.promptGuidelines ?? null };
	let id = "s1";
	const entries: unknown[] = [];
	const ctx = { hasUI: false, mode: "print", cwd: "/repo", sessionManager: { getSessionId: () => id, getEntries: () => entries, getBranch: () => [], getCwd: () => "/repo" }, ui: {} } as unknown as ExtensionContext;
	const call = async (path: string) => {
		try { return { result: await tool.execute("c", { path }, undefined, undefined, ctx) }; } catch (error) { return { error: (error as Error).message }; }
	};
	const calls: unknown[] = [];
	calls.push(await call("/repo-b"));
	for (const handler of handlers.get("session_start") ?? []) await handler({}, ctx);
	for (const path of ["/repo-b", "/repo-b/src", "/other", "/missing"]) calls.push(await call(path));
	id = "s2";
	calls.push(await call("/repo-b"));
	return { definition, calls, appended: appended.map((entry) => (entry as { type: string }).type) };
}

if (import.meta.url === `file://${process.argv[1]}`) process.stdout.write(JSON.stringify(await capture(), null, "\t") + "\n");
