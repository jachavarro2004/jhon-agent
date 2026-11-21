/**
 * Everything the agent's todo list shows the model, captured by scripted
 * sessions: the `todo` tool as registered (name, label, description, guidelines,
 * schema) in the main session and in a child, every tool result, the list
 * block and the start rule each run start delivers, and the nudges sent
 * after tool-use turns, plus the pure list functions (lib/todo.ts) and the
 * appended-prompt helpers (lib/ui/append-system-prompt.ts) on fixed inputs.
 * todo-model-text.test.ts compares it with the snapshot taken before the
 * T174-S14 rewrite, so the rewrite changes no word the model reads.
 *
 * Nothing here reads the clock, the terminal or a path: every input is fixed.
 */
import "./isolated-env.ts";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import jhonTodo from "../extensions/todo.ts";
import { setContextLevel } from "../lib/lean-context.ts";
import { applyTodo, emptyTodo, replayTodo, staleTurns, todoPromptBlock, todoStartBlock, todoSummary, type TodoParams, type TodoState } from "../lib/todo.ts";
import { appendSessionOnce, appendSystemPromptOnce } from "../lib/ui/append-system-prompt.ts";

type Handler = (event: unknown, ctx: ExtensionContext) => unknown;
interface Tool {
	name: string; label: string; description: string; promptSnippet?: string; promptGuidelines?: string[];
	parameters: unknown; executionMode?: string; renderShell?: string;
	execute(id: string, params: unknown, signal: undefined, onUpdate: undefined, ctx: ExtensionContext): Promise<unknown>;
	renderCall(args: unknown, theme: unknown): { render(width: number): string[] };
	renderResult(result: unknown, options: { expanded: boolean }, theme: unknown): { render(width: number): string[] };
}
const plain = { fg: (_role: string, text: string) => text, bold: (text: string) => text, strikethrough: (text: string) => text };

function host(env: NodeJS.ProcessEnv) {
	const handlers = new Map<string, Handler[]>();
	const tools: Tool[] = [];
	const shortcuts: string[] = [];
	const log: unknown[] = [];
	const pi = {
		on: (event: string, handler: Handler) => { handlers.set(event, [...(handlers.get(event) ?? []), handler]); },
		registerTool: (tool: Tool) => { tools.push(tool); },
		registerShortcut: (_key: string, options: { description: string }) => { shortcuts.push(options.description); },
		sendMessage: (message: unknown, options: unknown) => { log.push({ sent: message, options }); },
	} as unknown as ExtensionAPI;
	jhonTodo(pi, env);
	const fire = async (event: string, ctx: ExtensionContext, payload: unknown = {}) => {
		let last: unknown;
		for (const handler of handlers.get(event) ?? []) last = await handler(payload, ctx);
		return last;
	};
	return { tools, shortcuts, log, fire };
}

function context(id: string, branch: unknown[] = []): ExtensionContext {
	return { hasUI: false, sessionManager: { getSessionId: () => id, getBranch: () => branch } } as unknown as ExtensionContext;
}

const turn = (name: string, args: Record<string, unknown> = {}) => ({ message: { role: "assistant", content: [{ type: "toolCall", id: "c", name, arguments: args }] }, toolResults: [] });
const quiet = { message: { role: "assistant", content: [{ type: "text", text: "done" }] }, toolResults: [] };

type Step = { call: TodoParams | Record<string, unknown> } | { run: true } | { turn: string; args?: Record<string, unknown> } | { quiet: true } | { end: true };

const SCRIPTS: Array<{ name: string; env?: NodeJS.ProcessEnv; lean?: boolean; branch?: unknown[]; steps: Step[] }> = [
	{ name: "a list's life", steps: [
		{ run: true },
		{ call: { action: "list" } },
		{ call: { action: "write", tasks: [{ title: "Read the spec", status: "done" }, { title: "Write  the\nparser", status: "in-progress", note: "writing tests" }, { title: "Ship it" }] } },
		{ turn: "todo" }, { run: true },
		{ call: { action: "add", title: "Wait for review", status: "blocked", note: "an admin" } },
		{ call: { action: "add", title: "Docs", status: "Completed" } },
		{ call: { action: "update", id: 2, status: "done" } },
		{ call: { action: "update", id: 3, title: "Ship the parser" } },
		{ call: { action: "update", id: 3, note: "tagging" } },
		{ call: { action: "list" } },
		{ run: true }, { turn: "read" }, { turn: "read" }, { turn: "read" }, { turn: "read" }, { turn: "read" }, { quiet: true },
		{ run: true }, { run: true }, { run: true },
		{ call: { action: "write", tasks: [{ id: 3, title: "Ship the parser", status: "done" }, { id: 3, title: "dup id" }, { id: 0, title: "zero id", status: "cancelled" }, { title: "later", status: "dropped" }] } },
		{ end: true }, { run: true }, { run: true },
		{ call: { action: "clear" } }, { run: true },
	] },
	{ name: "every refusal", steps: [
		{ call: { action: "write", tasks: [{ title: "  " }] } },
		{ call: { action: "write", tasks: [{ title: "x", status: "soon" }] } },
		{ call: { action: "write", tasks: [{ title: "x", status: "blocked" }] } },
		{ call: { action: "add" } },
		{ call: { action: "add", title: "x", status: 7 } },
		{ call: { action: "update", id: 9, status: "done" } },
		{ call: { action: "write", tasks: [{ title: "one" }] } },
		{ call: { action: "update", id: 1 } },
		{ call: { action: "update", id: 1, status: "blocked" } },
		{ call: { action: "update", id: 1, status: "nope" } },
		{ call: { action: "rename" } },
		{ call: { action: "write" } },
		{ call: { action: "clear" } },
		{ call: { action: "add", title: "solo" } },
		{ call: { action: "clear" } },
	] },
	{ name: "start nudge", steps: [
		{ run: true }, { turn: "edit", args: { path: "a.ts" } }, { turn: "read" }, { turn: "read" }, { turn: "read" }, { turn: "read" },
		{ run: true }, { turn: "bash", args: { command: "ls -la && cat a > /dev/null" } }, { turn: "bash", args: { command: "npm test" } }, { turn: "bash", args: { command: "git status" } }, { turn: "read" },
		{ run: true }, { turn: "bash", args: { command: "echo x > out.txt" } }, { turn: "read" }, { turn: "read" },
	] },
	{ name: "lean", lean: true, steps: [
		{ run: true }, { turn: "write", args: { path: "a" } }, { turn: "read" }, { turn: "read" }, { turn: "read" },
		{ call: { action: "write", tasks: [{ title: "Fix the bug" }] } }, { run: true },
	] },
	{ name: "child", env: { JHON_PI_CHILD: "1", JHON_PI_TOOLS: "read,todo" }, steps: [
		{ run: true }, { turn: "edit" }, { turn: "read" }, { turn: "read" }, { turn: "read" },
		{ call: { action: "write", tasks: [{ title: "Scout", status: "in_progress", note: "reading" }] } },
		{ run: true }, { turn: "read" }, { turn: "read" }, { turn: "read" }, { turn: "read" }, { turn: "read" }, { run: true },
	] },
	{ name: "resumed", branch: [
		{ type: "message", message: { role: "user" } },
		{ type: "message", message: { role: "toolResult", toolName: "todo", details: { tasks: [{ id: 4, subject: "old rpiv", status: "in_progress", activeForm: "porting" }, { id: 5, subject: "gone", status: "deleted" }], nextId: 6 } } },
		{ type: "message", message: { role: "user" } },
		{ type: "custom_message", customType: "jhon-todo-list", content: "## Todo list\nstale copy" },
	], steps: [{ run: true }, { run: true }, { call: { action: "add", title: "next" } }, { run: true }] },
];

const STATES: TodoState[] = [
	emptyTodo(),
	{ tasks: [{ id: 1, title: "a", status: "done" }, { id: 2, title: "b", status: "in_progress", note: "doing b" }, { id: 3, title: "c", status: "blocked", note: "a person" }, { id: 4, title: "d", status: "dropped" }], nextId: 5, updatedTurn: 1 },
	{ tasks: [{ id: 1, title: "only", status: "pending" }], nextId: 2, updatedTurn: 0 },
	{ tasks: [{ id: 1, title: "wait", status: "blocked", note: "x" }], nextId: 2, updatedTurn: 0 },
	{ tasks: [{ id: 1, title: "fin", status: "done" }], nextId: 2, updatedTurn: 0 },
];

export async function capture() {
	const definitions: unknown[] = [];
	const scripts: unknown[] = [];
	for (const script of SCRIPTS) {
		if (script.lean) setContextLevel("lean");
		try {
			const { tools, shortcuts, log, fire } = host(script.env ?? {});
			const tool = tools[0];
			if (!tool) { scripts.push({ name: script.name, registered: false }); continue; }
			definitions.push({ script: script.name, name: tool.name, label: tool.label, description: tool.description, promptSnippet: tool.promptSnippet, promptGuidelines: tool.promptGuidelines, parameters: tool.parameters, executionMode: tool.executionMode, renderShell: tool.renderShell, shortcuts: shortcuts.length });
			const ctx = context(`s-${script.name}`, script.branch);
			log.push({ start: await fire("session_start", ctx) ?? null });
			for (const step of script.steps) {
				if ("call" in step) {
					const result = await tool.execute("t", step.call, undefined, undefined, ctx) as { content: Array<{ text: string }> };
					await fire("tool_execution_end", ctx, { toolName: "todo" });
					log.push({ call: step.call, result, card: tool.renderCall(step.call, plain).render(200), folded: tool.renderResult(result, { expanded: false }, plain).render(200), open: tool.renderResult(result, { expanded: true }, plain).render(200) });
				} else if ("run" in step) log.push({ run: await fire("before_agent_start", ctx, { prompt: "go", systemPrompt: "base" }) ?? null });
				else if ("turn" in step) log.push({ turn: step.turn, end: await fire("turn_end", ctx, turn(step.turn, step.args)) ?? null });
				else if ("quiet" in step) log.push({ quiet: await fire("turn_end", ctx, quiet) ?? null });
				else log.push({ agentEnd: await fire("agent_end", ctx) ?? null });
			}
			await fire("session_shutdown", ctx);
			scripts.push({ name: script.name, log });
		} finally { setContextLevel(undefined); }
	}
	const pure = {
		states: STATES.map((state) => ({ summary: todoSummary(state), stale: [0, 1, 2, 3, 9].map((now) => staleTurns(state, now)), block: [0, 1, 2, 5].map((stale) => todoPromptBlock(state, stale) ?? null), start: todoStartBlock(state) ?? null, list: applyTodo(state, { action: "list" }, 3).text })),
		replay: replayTodo([
			{ type: "message", message: { role: "toolResult", toolName: "todo", details: { gentleTodo: STATES[2] } } },
			{ type: "message", message: { role: "toolResult", toolName: "todo", isError: true, details: { jhonTodo: STATES[1] } } },
			{ type: "message", message: { role: "toolResult", toolName: "other", details: { jhonTodo: STATES[1] } } },
		]),
	};
	const appended: unknown[] = [];
	for (const [start, texts] of [["", ["\n\nHarness", "Harness", "\n\nTodo"]], ["user text", ["\n\n", "", "Block"]]] as const) {
		const options: { appendSystemPrompt: string; sections?: Record<string, string> } = { appendSystemPrompt: start };
		for (const text of texts) { appendSystemPromptOnce(options, text); appendSessionOnce(options, text); }
		appended.push(options);
	}
	return { definitions, scripts, pure, appended };
}

if (import.meta.url === `file://${process.argv[1]}`) process.stdout.write(JSON.stringify(await capture(), null, "\t") + "\n");
