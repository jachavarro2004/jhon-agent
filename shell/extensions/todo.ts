import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { Text, type TUI } from "@earendil-works/pi-tui";
import { invalidatePane, paneSlot } from "../lib/ui/pane.ts";
import { renderPanelHeader, renderPanelRow, type PanelTheme } from "../lib/ui/shell-panel.ts";
import { asciiText } from "../lib/ui/primitives/glyphs.ts";
import { STATES } from "../lib/ui/primitives/states.ts";
import { THEME_NAME } from "../lib/ui/primitives/paint.ts";
import { selectedChange } from "../lib/ui/ldd-status.ts";
import { contextLevel } from "../lib/lean-context.ts";
import {
	applyTodo,
	emptyTodo,
	replayTodo,
	staleTurns,
	TODO_DETAILS_KEY,
	TODO_GLYPH,
	TODO_STATUS,
	TODO_TOOL_NAME,
	todoPromptBlock,
	todoStartBlock,
	todoSummary,
	type TodoParams,
	type TodoState,
	type TodoStatus,
} from "../lib/todo.ts";
import { counted } from "../lib/ui-use.ts";
import { shellSettings } from "../lib/settings.ts";

// Jhon's todo: the task list the model keeps while it works, drawn in the
// right pane's TODOS section while no LDD change is active (T174, L1469:
// with one, the LDD section rules and the list is hidden). Three things keep it current that a
// static tool description cannot: `write` replaces the whole list in one
// call, each run's system prompt carries the open tasks and the rules, and
// a list that goes untouched while tasks stay open is marked stale for both
// the human and the model. Long runs also receive one request-local reminder
// after several tool-use turns, without waking the agent or changing tasks.
// With no list, the prompt says when to start one, and work that changes
// something without a list gets one nudge per owner message (S34). The list
// is the agent's own next steps, never the change's tasks, which stay in
// `jhon status` (L1019): so the start rule and the nudge hold inside an LDD
// change too (T134; T49 had silenced them there). jhon-builder, jhon-scout and jhon-checker
// keep one too (T134), private to their child process, with no section, no
// start rule and no nudge, since a child's task is bounded.

const WIDGET_KEY = "jhon-todo";
const STATUS_ENUM = ["pending", "in_progress", "blocked", "done", "dropped"];
const REMIND_AFTER_TOOL_TURNS = 4;
// pi turns a custom message into user text, so a nudge would read as the
// owner's: each opens with the harness's frame (T42d).
const HARNESS_FRAME = "[Harness reminder from the Jhon shell, not a message from the owner] ";
const TODO_REMINDER = HARNESS_FRAME + "The todo plan has not been reviewed during several tool-use turns. Check whether it still matches the work performed and update tasks where needed. Do not mark work done without verification.";
const START_AFTER_TOOL_TURNS = 3;
// T115d (L707, L708): the list block, the start rule and the nudges reach the
// model as persisted custom messages, never in the system prompt and never
// request-local, so the cached prefix holds on every turn.
export const TODO_LIST_MESSAGE = "jhon-todo-list";
export const TODO_REMINDER_MESSAGE = "jhon-todo-reminder";
export const TODO_START_MESSAGE = "jhon-todo-start";
const START_NUDGE = HARNESS_FRAME + "This work has several steps and no todo list. Write your own next steps for this request with todo write and keep them current; the change's tasks stay in `jhon status`.";
// A bash call that changes something: a writing command where a command
// starts, sed -i, a git or package command that writes, or a redirect to a
// file. Reads, searches and test runs do not count.
const COMMAND_START = String.raw`(?:^|[;&|(]\s*|\b(?:sudo|xargs)\s+)`;
const CHANGING_BASH = new RegExp([
	String.raw`${COMMAND_START}(?:rm|rmdir|mv|cp|mkdir|touch|tee|chmod|chown|ln|truncate|patch|install)\b`,
	String.raw`\bsed\s+(?:-\w*\s+)*-i`,
	String.raw`\bgit\s+(?:add|commit|checkout|switch|reset|restore|merge|rebase|apply|stash|mv|rm|cherry-pick|revert|tag|push|pull)\b`,
	String.raw`\b(?:npm|pnpm|yarn|bun)\s+(?:i|install|add|remove|uninstall|update)\b`,
	String.raw`(?:^|[^0-9&>=])>>?\s*(?!&|/dev/null)[^\s>]`,
].join("|"));

function changesSomething(name: string, args: unknown): boolean {
	const tool = name.toLowerCase();
	if (tool === "edit" || tool === "write") return true;
	const command = (args as { command?: unknown } | undefined)?.command;
	return tool === "bash" && typeof command === "string" && CHANGING_BASH.test(command);
}
const TOOL_PARAMETERS = {
	type: "object",
	additionalProperties: false,
	required: ["action"],
	properties: {
		action: { type: "string", enum: ["write", "add", "update", "clear", "list"], description: "write replaces the whole list; add appends one task; update changes one task by id; clear empties the list; list reports it." },
		tasks: {
			type: "array",
			description: "For write: the complete ordered list. Keep the id of tasks that already exist so their history survives; omit it for new ones.",
			items: {
				type: "object",
				additionalProperties: false,
				required: ["title"],
				properties: {
					id: { type: "integer", description: "Existing task id to keep." },
					title: { type: "string", description: "Short imperative title, e.g. 'Write the parser'." },
					status: { type: "string", enum: STATUS_ENUM, description: "Defaults to pending. blocked: open but waiting on something outside the list; dropped: will not be done, on purpose." },
					note: { type: "string", description: "What is happening right now, shown while in_progress, e.g. 'writing tests'; required for blocked, naming what it waits for, e.g. 'waiting for an admin'." },
				},
			},
		},
		id: { type: "integer", description: "Task id for update." },
		title: { type: "string", description: "Title for add, or a new title for update." },
		status: { type: "string", enum: STATUS_ENUM, description: "Status for add or update." },
		note: { type: "string", description: "Note for add or update; required for blocked." },
	},
} as const;

/** A child (sub-agent) process: it has the tool only when its tools, its rendered `tools:`, name it. */
const inChild = (env: NodeJS.ProcessEnv): boolean => env.JHON_PI_CHILD === "1";

export function todoEnabled(env: NodeJS.ProcessEnv = process.env): boolean {
	return !inChild(env) || (env.JHON_PI_TOOLS ?? "").split(",").includes(TODO_TOOL_NAME);
}

// The shell's state glyphs (primitives/states.ts): blocked needs you, so it is
// ◉ in the bad role; dropped is a dim ✕.
const MARK: Record<TodoStatus, string> = { [TODO_STATUS.PENDING]: STATES.pending.glyph, [TODO_STATUS.IN_PROGRESS]: STATES.working.glyph, [TODO_STATUS.BLOCKED]: STATES.blocked.glyph, [TODO_STATUS.DONE]: STATES.done.glyph, [TODO_STATUS.DROPPED]: "✕" };
const MARK_ROLE: Record<TodoStatus, string> = { [TODO_STATUS.PENDING]: THEME_NAME[STATES.pending.role], [TODO_STATUS.IN_PROGRESS]: THEME_NAME[STATES.working.role], [TODO_STATUS.BLOCKED]: THEME_NAME[STATES.blocked.role], [TODO_STATUS.DONE]: THEME_NAME[STATES.done.role], [TODO_STATUS.DROPPED]: "muted" };

/**
 * The right pane's TODOS section: `▾ TODOS … done/planned`, then one row per
 * task, its state glyph, its title and, in progress or blocked, its note.
 * Nothing without tasks. S5's LDD panel replaces it.
 */
export function renderTodoRail(state: TodoState, theme: PanelTheme, width: number, ascii = false): string[] {
	if (state.tasks.length === 0) return [];
	const { done, total } = todoSummary(state);
	const planned = total - state.tasks.filter((task) => task.status === TODO_STATUS.DROPPED).length;
	const rows = [
		renderPanelHeader(theme, width, { title: "Todos", count: `${done}/${planned}`, open: true, ascii }),
		...state.tasks.map((task) => {
			const closed = task.status === TODO_STATUS.DONE || task.status === TODO_STATUS.DROPPED;
			const note = task.note && (task.status === TODO_STATUS.IN_PROGRESS || task.status === TODO_STATUS.BLOCKED) ? theme.fg("muted", ` · ${task.note}`) : "";
			return renderPanelRow(width, `${theme.fg(MARK_ROLE[task.status], MARK[task.status])} ${theme.fg(closed ? "dim" : "text", task.title)}${note}`);
		}),
	];
	return ascii ? rows.map(asciiText) : rows;
}

interface TodoSession {
	state: TodoState;
	turn: number;
	/** Separate from the existing run-based card freshness. */
	toolTurnsSinceWrite: number;
	reminded: boolean;
	wroteThisTurn: boolean;
	/** Since the owner's last message: tool-use turns, whether one changed something, the start nudge, a list cleared on purpose. */
	ownerToolTurns: number;
	ownerChanged: boolean;
	startNudged: boolean;
	clearedByOwnerTurn: boolean;
	/** A finished list stays on screen for the turn it finished in, then clears. */
	clearOnNextTurn: boolean;
	ui: ExtensionContext["ui"] | undefined;
	tui: TUI | undefined;
	/** The session's id, for the LDD section's selection of its change. */
	id: string;
	/** The last list block sent to the model, so an unchanged one is not sent again. */
	lastBlock?: string;
}

/** The last list block a branch already carries. */
function lastBlockOn(branch: readonly unknown[]): string | undefined {
	let last: string | undefined;
	for (const e of branch) {
		const entry = e as { type?: string; customType?: string; content?: unknown };
		if (entry?.type === "custom_message" && entry.customType === TODO_LIST_MESSAGE && typeof entry.content === "string") last = entry.content;
	}
	return last;
}

function sessionKey(ctx: ExtensionContext): string {
	return ctx.sessionManager.getSessionId() ?? "";
}

export default function jhonTodo(pi: ExtensionAPI, env: NodeJS.ProcessEnv = process.env): void {
	// Its commands and shortcuts are counted (lib/ui-use.ts).
	pi = counted(pi);
	if (!todoEnabled(env)) return;
	const child = inChild(env);
	const sessions = new Map<string, TodoSession>();

	const session = (ctx: ExtensionContext): TodoSession => {
		const key = sessionKey(ctx);
		let current = sessions.get(key);
		if (!current) {
			current = { state: emptyTodo(), turn: 0, toolTurnsSinceWrite: 0, reminded: false, wroteThisTurn: false, ownerToolTurns: 0, ownerChanged: false, startNudged: false, clearedByOwnerTurn: false, clearOnNextTurn: false, ui: undefined, tui: undefined, id: key };
			sessions.set(key, current);
		}
		return current;
	};

	// The right pane's TODOS section, while the LDD section shows no change; a
	// regular terminal and the narrow form show no list (T174, L1469).
	const show = (current: TodoSession) => {
		if (current.tui) invalidatePane(current.tui);
		// A child's list is its own: no section (T134).
		if (!current.ui || child) return;
		const snapshot = current;
		current.ui.setWidget(WIDGET_KEY, (tui, theme) => {
			snapshot.tui = tui;
			const hidden = () => selectedChange(snapshot.id) !== undefined;
			const dispose = paneSlot(tui, "todo", {
				digest: () => JSON.stringify([snapshot.state.tasks, hidden(), shellSettings().ascii()]),
				render: (width) => hidden() ? [] : renderTodoRail(snapshot.state, theme, width, shellSettings().ascii()),
				invalidate() {},
			});
			return { render: () => [], invalidate() {}, dispose };
		});
	};

	pi.registerTool({
		name: TODO_TOOL_NAME,
		renderShell: "self",
		label: "Todo",
		description: "Plan and track your own next steps for this request; start the list yourself, without being asked, before the first edit of multi-step work. A change's tasks are in `jhon status`: never copy them here. Actions: write (whole list), update, add, clear, list.",
		promptSnippet: "Track multi-step work",
		promptGuidelines: [
			"Use todo for three or more steps or a list the user hands you; skip trivial requests. A small change keeps no list.",
			"Keep exactly one task in_progress; mark it done right after it is verified, never while tests fail or the work is partial (add a task for the blocker).",
			"When the plan changes, write the complete list and keep ids of existing tasks; mark obsolete tasks dropped, never done.",
			"Mark a task blocked with a note naming what it waits for only when that is outside the list (a person, an authorization); a prerequisite in the list keeps it pending. Back to pending once the condition holds.",
		],
		parameters: TOOL_PARAMETERS,
		executionMode: "sequential",
		renderCall(args, theme) {
			const params = args as TodoParams;
			const title = `${TODO_GLYPH} todo · ${params.action}`;
			return new Text(theme.fg("toolTitle", shellSettings().ascii() ? asciiText(title) : title), 0, 0);
		},
		renderResult(result, options, theme) {
			const text = result.content.map((part) => (part.type === "text" ? part.text : "")).join("\n");
			return new Text(options.expanded ? text : theme.fg("muted", text.split("\n")[0] ?? ""), 0, 0);
		},
		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			const current = session(ctx);
			const result = applyTodo(current.state, params as TodoParams, current.turn);
			if (!result.error) {
				current.state = result.state;
				current.clearOnNextTurn = false;
				if (params.action !== "list") {
					current.toolTurnsSinceWrite = 0;
					current.reminded = false;
					current.wroteThisTurn = true;
					if (result.state.tasks.length === 0) current.clearedByOwnerTurn = true;
				}
				if (current.tui) invalidatePane(current.tui);
			}
			return {
				content: [{ type: "text", text: result.text }],
				details: { [TODO_DETAILS_KEY]: result.state, ...(result.error ? { error: result.error } : {}) },
			};
		},
	});

	pi.on("session_start", (_event, ctx) => {
		const current = session(ctx);
		// A list that was already finished when the session was left is history,
		// not work: it would otherwise sit on screen until two more turns pass.
		const replayed = replayTodo(ctx.sessionManager.getBranch());
		current.state = replayed.tasks.length > 0 && todoSummary(replayed).open === 0 ? { ...replayed, tasks: [] } : replayed;
		current.turn = ctx.sessionManager.getBranch().filter((entry) => (entry as { type?: string }).type === "message" && (entry as { message?: { role?: string } }).message?.role === "user").length;
		current.toolTurnsSinceWrite = 0;
		current.reminded = false;
		current.wroteThisTurn = false;
		current.ownerToolTurns = 0;
		current.ownerChanged = false;
		current.startNudged = false;
		current.clearedByOwnerTurn = false;
		current.lastBlock = lastBlockOn(ctx.sessionManager.getBranch());
		current.ui = ctx.hasUI ? ctx.ui : undefined;
		show(current);
	});

	pi.on("session_shutdown", (_event, ctx) => {
		sessions.delete(sessionKey(ctx));
	});

	pi.on("before_agent_start", (_event, ctx) => {
		const current = session(ctx);
		current.turn += 1;
		if (current.tui) invalidatePane(current.tui);
		if (current.clearOnNextTurn) {
			current.state = { ...current.state, tasks: [] };
			current.clearOnNextTurn = false;
			show(current);
		}
		// A new owner message: the start nudge counts again from here.
		current.ownerToolTurns = 0;
		current.ownerChanged = false;
		current.startNudged = false;
		current.clearedByOwnerTurn = false;
		// A child gets its open list back on a continued run, never the start rule.
		// T146h L4: a lean session (no change, or a small one) gets no start rule.
		const block = todoPromptBlock(current.state, staleTurns(current.state, current.turn)) ?? (child || contextLevel() === "lean" ? undefined : todoStartBlock(current.state));
		// T115d: a persisted message, sent when the block changed; the system prompt never carries it.
		if (!block || block === current.lastBlock) return undefined;
		current.lastBlock = block;
		return { message: { customType: TODO_LIST_MESSAGE, content: block, display: false } };
	});

	pi.on("turn_end", (event, ctx) => {
		const current = session(ctx);
		// A turn containing a valid todo write is already reconciled. Failed
		// writes and list reads deliberately do not refresh this counter.
		const calls = event.message.role === "assistant" ? event.message.content.filter((part) => part.type === "toolCall") : [];
		if (!current.wroteThisTurn && calls.length > 0) {
			current.toolTurnsSinceWrite += 1;
		}
		if (calls.length > 0) {
			current.ownerToolTurns += 1;
			if (calls.some((call) => changesSomething(call.name, call.arguments))) current.ownerChanged = true;
		}
		current.wroteThisTurn = false;
		// The nudges go out after a tool-use turn, persisted, before the next request; a child gets none.
		if (calls.length === 0 || child) return;
		const remind = (customType: string, content: string) => pi.sendMessage({ customType, content, display: false }, { deliverAs: "steer" });
		if (current.state.tasks.length === 0) {
			if (contextLevel() === "lean" || current.startNudged || current.clearedByOwnerTurn || !current.ownerChanged || current.ownerToolTurns < START_AFTER_TOOL_TURNS) return;
			current.startNudged = true;
			remind(TODO_START_MESSAGE, START_NUDGE);
			return;
		}
		if (current.reminded || current.toolTurnsSinceWrite < REMIND_AFTER_TOOL_TURNS) return;
		if (!current.state.tasks.some((task) => task.status === "pending" || task.status === "in_progress")) return;
		current.reminded = true;
		remind(TODO_REMINDER_MESSAGE, TODO_REMINDER);
	});

	pi.on("tool_execution_end", (event, ctx) => {
		if (event.toolName !== TODO_TOOL_NAME) return;
		show(session(ctx));
	});

	pi.on("agent_end", (_event, ctx) => {
		const current = session(ctx);
		if (current.state.tasks.length > 0 && todoSummary(current.state).open === 0) current.clearOnNextTurn = true;
		show(current);
	});
}
