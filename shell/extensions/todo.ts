import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { Text, type Component, type TUI } from "@earendil-works/pi-tui";
import { invalidatePane, paneSlot, paneState, setPaneHover } from "../lib/ui/pane.ts";
import type { PanelTheme } from "../lib/ui/shell-panel.ts";
import { asciiText } from "../lib/ui/primitives/glyphs.ts";
import { plainTodoRows, renderTodoBlock, renderTodoPane, todoHeight, TODO_SCROLL_KEYS, TODO_SCROLL_ROWS, type TodoView } from "../lib/ui/todo-section.ts";
import { selectedChange } from "../lib/ui/ldd-status.ts";
import { contextLevel } from "../lib/lean-context.ts";
import {
	applyTodo,
	emptyTodo,
	replayTodo,
	staleTurns,
	TODO_DETAILS_KEY,
	TODO_GLYPH,
	TODO_TOOL_NAME,
	todoPromptBlock,
	todoStartBlock,
	todoSummary,
	type TodoParams,
	type TodoState,
} from "../lib/todo.ts";
import { counted } from "../lib/ui-use.ts";
import { shellSettings } from "../lib/settings.ts";
import { NOT_THE_OWNER, fromTheShell, withoutTheMark } from "../lib/from-the-shell.ts";

/*
 * The agent's todo list: the `todo` tool and where the list shows.
 *
 * On screen it is the right pane's TODOS section, or, where the pane is not
 * beside the conversation (a regular terminal, the narrow form), a block
 * above the prompt (lib/ui/todo-section.ts). Either shows only while the list
 * has tasks and no LDD change is selected: with one, the LDD section shows
 * the change's tasks and this list hides (T174, L1469). alt+t
 * (shell.keys.todos) folds it and alt+PgUp/PgDn scroll it (T174-S5b, L1495).
 *
 * For the model, what a tool description alone cannot do: `write` replaces
 * the whole list in one call; each run starts with the open list and its
 * rules, or, with none open, the rule for when to start one; a list left
 * without an update while work is open is stale, for the owner and for the
 * model; after several tool-use turns with no update the model gets one
 * reminder, and work that changes something without a list gets one nudge
 * per owner message (S34). The list is the agent's own next steps, never the
 * change's tasks, which stay in `jhon status` (L1019), so the start rule and
 * the nudge hold inside a change too (T134).
 *
 * jhon-builder, jhon-scout and jhon-checker keep a list of their own (T134),
 * private to the child process: no section, no keys, no start rule, no nudge.
 */

const WIDGET_KEY = "jhon-todo";

// T115d (L707, L708): the list block, the start rule and the nudges reach the
// model as persisted custom messages, never in the system prompt and never
// for one request only, so the cached prefix holds on every turn.
export const TODO_LIST_MESSAGE = "jhon-todo-list";
export const TODO_REMINDER_MESSAGE = "jhon-todo-reminder";
export const TODO_START_MESSAGE = "jhon-todo-start";

/** Tool-use turns without an update before the reminder; tool-use turns that change something before the start nudge. */
const REMINDER_AFTER = 4;
const START_NUDGE_AFTER = 3;

// pi hands a custom message to the model as user text, so every nudge says
// who speaks first (T42d), and that it does not set the reply language (T194).
const FROM_THE_HARNESS = `${NOT_THE_OWNER} `;
const REMINDER_TEXT = `${FROM_THE_HARNESS}Several tool-use turns went by without a look at the todo list. Compare it with the work done so far and update any task that no longer matches. Mark nothing done until it is verified.`;
const START_TEXT = `${FROM_THE_HARNESS}This work has several steps and no todo list. Write your own next steps for this request with todo write and keep them current; the change's tasks stay in \`jhon status\`.`;

/*
 * A bash command that changes something: a writing command where a command
 * starts (also after sudo or xargs), sed -i, a git or package-manager command
 * that writes, or output redirected to a file other than /dev/null. Reads,
 * searches and test runs do not count.
 */
const AT_COMMAND = String.raw`(?:^|[;&|(]\s*|\b(?:sudo|xargs)\s+)`;
const WRITING_COMMAND = String.raw`${AT_COMMAND}(?:rm|rmdir|mv|cp|mkdir|touch|tee|chmod|chown|ln|truncate|patch|install)\b`;
const SED_IN_PLACE = String.raw`\bsed\s+(?:-\w*\s+)*-i`;
const GIT_WRITE = String.raw`\bgit\s+(?:add|commit|checkout|switch|reset|restore|merge|rebase|apply|stash|mv|rm|cherry-pick|revert|tag|push|pull)\b`;
const PACKAGE_WRITE = String.raw`\b(?:npm|pnpm|yarn|bun)\s+(?:i|install|add|remove|uninstall|update)\b`;
const REDIRECT_TO_FILE = String.raw`(?:^|[^0-9&>=])>>?\s*(?!&|/dev/null)[^\s>]`;
const CHANGING_COMMAND = new RegExp([WRITING_COMMAND, SED_IN_PLACE, GIT_WRITE, PACKAGE_WRITE, REDIRECT_TO_FILE].join("|"));

/** Whether a tool call changes something: an edit, a write, or a changing bash command. */
function changes(toolName: string, args: unknown): boolean {
	const name = toolName.toLowerCase();
	if (name === "edit" || name === "write") return true;
	if (name !== "bash") return false;
	const command = (args as { command?: unknown } | undefined)?.command;
	return typeof command === "string" && CHANGING_COMMAND.test(command);
}

const STATUSES = ["pending", "in_progress", "blocked", "done", "dropped"];

/** The tool's schema, as the model reads it. */
const PARAMETERS = {
	type: "object",
	additionalProperties: false,
	required: ["action"],
	properties: {
		action: { type: "string", enum: ["write", "add", "update", "clear", "list"], description: "write sets the whole list, add puts one task at the end, update edits a task by id, clear removes every task, list shows them." },
		tasks: {
			type: "array",
			description: "For write: every task, in order. Give a kept task its id back so its history stays; leave id out for new tasks.",
			items: {
				type: "object",
				additionalProperties: false,
				required: ["title"],
				properties: {
					id: { type: "integer", description: "Id of a task you keep." },
					title: { type: "string", description: "A short command as title, like 'Write the parser'." },
					status: { type: "string", enum: STATUSES, description: "pending when omitted. blocked: still open, held by something beyond the list; dropped: deliberately left undone." },
					note: { type: "string", description: "The current activity, shown during in_progress, like 'writing tests'. A blocked task needs one that says what it waits for, like 'waiting for an admin'." },
				},
			},
		},
		id: { type: "integer", description: "Id of the task to update." },
		title: { type: "string", description: "add: its title; update: a new title." },
		status: { type: "string", enum: STATUSES, description: "add or update: its status." },
		note: { type: "string", description: "add or update: its note; blocked needs one." },
	},
} as const;

const DESCRIPTION = "Plan and track your own next steps for this request; start the list yourself, without being asked, before the first edit of multi-step work. A change's tasks are in `jhon status`: never copy them here. Actions: write (whole list), update, add, clear, list.";
const GUIDELINES = [
	"Use todo for three or more steps or a list the user hands you; skip trivial requests. A small change keeps no list.",
	"Keep exactly one task in_progress; mark it done right after it is verified, never with failing tests or partial work (the blocker becomes a new task).",
	"When the plan changes, write the complete list and keep ids of existing tasks; mark obsolete tasks dropped, never done.",
	"Set blocked only for a wait outside the list (a person, an authorization), with a note that names it; a prerequisite inside the list keeps the task pending. When the condition is met, back to pending.",
];

/** A sub-agent's process: it has the tool only when its tools (its rendered `tools:`) name it. */
const isChild = (env: NodeJS.ProcessEnv): boolean => env.JHON_PI_CHILD === "1";

/** Whether this process gets the todo tool: the main session always, a sub-agent when its tools name it. */
export const todoEnabled = (env: NodeJS.ProcessEnv = process.env): boolean =>
	!isChild(env) || (env.JHON_PI_TOOLS ?? "").split(",").includes(TODO_TOOL_NAME);

/** The right pane's TODOS section unfolded, with no key and every task (lib/ui/todo-section.ts); nothing without tasks. */
export function renderTodoRail(state: TodoState, theme: PanelTheme, width: number, ascii = false): string[] {
	return plainTodoRows(state, theme, width, ascii);
}

/** One session's list and what the nudges count. */
interface ListSession {
	id: string;
	state: TodoState;
	/** Runs started in the session (owner messages). */
	turn: number;
	/** For the reminder: tool-use turns since the list last changed, and whether it was sent. */
	turnsWithoutUpdate: number;
	reminded: boolean;
	updatedThisTurn: boolean;
	/** For the start nudge, since the owner's last message: tool-use turns, whether one changed something, whether it was sent, whether the list was emptied on purpose. */
	ownerTurns: number;
	ownerChanged: boolean;
	startSent: boolean;
	emptiedThisMessage: boolean;
	/** A finished list stays for the turn it finished in and goes at the next. */
	clearAtNextRun: boolean;
	/** Folded by the owner (alt+t), and the first task row shown (alt+PgUp/PgDn). */
	folded: boolean;
	top: number;
	ui: ExtensionContext["ui"] | undefined;
	tui: TUI | undefined;
	/** The last list block the model got: an unchanged one is not sent again. */
	lastBlock?: string;
}

function newSession(id: string): ListSession {
	return { id, state: emptyTodo(), turn: 0, turnsWithoutUpdate: 0, reminded: false, updatedThisTurn: false, ownerTurns: 0, ownerChanged: false, startSent: false, emptiedThisMessage: false, clearAtNextRun: false, folded: false, top: 0, ui: undefined, tui: undefined };
}

/** The last list block a branch carries already, without the shell's mark (a branch from before T194 has none). */
function lastBlockOn(branch: readonly unknown[]): string | undefined {
	let last: string | undefined;
	for (const item of branch) {
		const entry = item as { type?: string; customType?: string; content?: unknown };
		if (entry?.type === "custom_message" && entry.customType === TODO_LIST_MESSAGE && typeof entry.content === "string") last = withoutTheMark(entry.content);
	}
	return last;
}

/** Owner messages on a branch: the session's turn count. */
const ownerMessages = (branch: readonly unknown[]) => branch.filter((entry) => {
	const item = entry as { type?: string; message?: { role?: string } };
	return item.type === "message" && item.message?.role === "user";
}).length;

const hasActionable = (state: TodoState) => state.tasks.some((task) => task.status === "pending" || task.status === "in_progress");

type MouseEvent = Parameters<NonNullable<Component["handleMouse"]>>[0];

export default function jhonTodo(pi: ExtensionAPI, env: NodeJS.ProcessEnv = process.env): void {
	// Its commands and shortcuts are counted (lib/ui-use.ts).
	pi = counted(pi);
	if (!todoEnabled(env)) return;
	const child = isChild(env);
	const sessions = new Map<string, ListSession>();
	// A child's list is never on a screen: no keys.
	const foldKey = child ? undefined : shellSettings().key("todos");

	const sessionOf = (ctx: ExtensionContext): ListSession => {
		const id = ctx.sessionManager.getSessionId() ?? "";
		let found = sessions.get(id);
		if (!found) { found = newSession(id); sessions.set(id, found); }
		return found;
	};

	const repaint = (list: ListSession) => {
		if (!list.tui) return;
		invalidatePane(list.tui);
		list.tui.requestRender();
	};
	const fold = (list: ListSession) => {
		list.folded = !list.folded;
		list.top = 0;
		repaint(list);
	};
	const scrollBy = (list: ListSession, rows: number) => {
		if (list.folded) return;
		list.top = Math.max(0, list.top + rows);
		repaint(list);
	};

	/** The pane's TODOS section, and the block above the prompt where the pane is not beside the conversation. */
	const draw = (list: ListSession) => {
		if (list.tui) invalidatePane(list.tui);
		if (!list.ui || child) return;
		list.ui.setWidget(WIDGET_KEY, (tui, theme) => {
			list.tui = tui;
			const hidden = () => list.state.tasks.length === 0 || selectedChange(list.id) !== undefined;
			// Whether the pointer is on the section's title row, and how far the list scrolled as last drawn.
			let hovered = false;
			let maxTop = 0;
			const view = (): TodoView => ({
				collapsed: list.folded, top: list.top, staleTurns: staleTurns(list.state, list.turn),
				...(foldKey ? { key: foldKey } : {}), height: todoHeight(tui.terminal?.rows ?? 48), ascii: shellSettings().ascii(),
			});
			const unhover = () => { hovered = false; repaint(list); };
			const section = {
				digest: () => JSON.stringify([list.state.tasks, hidden(), view(), hovered]),
				render: (width: number) => {
					if (hidden()) return [];
					const drawn = renderTodoPane(list.state, theme, width, { ...view(), hover: hovered });
					list.top = drawn.top;
					maxTop = drawn.maxTop ?? 0;
					return drawn.rows;
				},
				// The title row folds on a click and lights under the pointer; the
				// wheel scrolls the list and, past either end, is left to the pane.
				handleMouse(event: MouseEvent) {
					switch (event.type) {
						case "wheel": {
							if (list.folded) return undefined;
							const top = Math.max(0, Math.min(maxTop, list.top + Math.trunc(event.wheelDelta ?? 0)));
							if (top === list.top) return undefined;
							list.top = top;
							repaint(list);
							return { handled: true, render: true };
						}
						case "move": {
							if (event.button !== "none") return undefined;
							if (event.y !== 0) { setPaneHover(tui); return undefined; }
							if (hovered) return undefined;
							hovered = true;
							setPaneHover(tui, section, unhover);
							repaint(list);
							return { handled: true, render: true };
						}
						// pi sends a click only to the component that took its press.
						case "press":
							return event.button === "left" && event.y === 0 ? { handled: true } : undefined;
						case "click":
							if (event.button !== "left" || event.y !== 0) return undefined;
							fold(list);
							return { handled: true, render: true };
						default:
							return undefined;
					}
				},
				invalidate() {},
			};
			const dispose = paneSlot(tui, "todo", section);
			return {
				render: (width: number) => {
					if (hidden() || paneState(tui).wide?.()) return [];
					const drawn = renderTodoBlock(list.state, theme, width, view());
					list.top = drawn.top;
					return drawn.rows;
				},
				invalidate() {},
				dispose,
			};
		});
	};

	if (foldKey) {
		pi.registerShortcut(foldKey as Parameters<ExtensionAPI["registerShortcut"]>[0], { description: "Folds or unfolds the todo list", handler: async (ctx) => { fold(sessionOf(ctx)); } });
	}
	if (!child) {
		pi.registerShortcut(TODO_SCROLL_KEYS.up, { description: "Scrolls the todo list up", handler: async (ctx) => { scrollBy(sessionOf(ctx), -TODO_SCROLL_ROWS); } });
		pi.registerShortcut(TODO_SCROLL_KEYS.down, { description: "Scrolls the todo list down", handler: async (ctx) => { scrollBy(sessionOf(ctx), TODO_SCROLL_ROWS); } });
	}

	pi.registerTool({
		name: TODO_TOOL_NAME,
		renderShell: "self",
		label: "Todo",
		description: DESCRIPTION,
		promptSnippet: "Track a multi-step plan",
		promptGuidelines: GUIDELINES,
		parameters: PARAMETERS,
		executionMode: "sequential",
		renderCall(args, theme) {
			const title = `${TODO_GLYPH} todo · ${(args as TodoParams).action}`;
			return new Text(theme.fg("toolTitle", shellSettings().ascii() ? asciiText(title) : title), 0, 0);
		},
		renderResult(result, options, theme) {
			const text = result.content.map((part) => (part.type === "text" ? part.text : "")).join("\n");
			return new Text(options.expanded ? text : theme.fg("muted", text.split("\n")[0] ?? ""), 0, 0);
		},
		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			const list = sessionOf(ctx);
			const result = applyTodo(list.state, params as TodoParams, list.turn);
			if (!result.error) {
				list.state = result.state;
				list.clearAtNextRun = false;
				// Listing reads the list: it does not count as keeping it current.
				if (params.action !== "list") {
					list.turnsWithoutUpdate = 0;
					list.reminded = false;
					list.updatedThisTurn = true;
					if (result.state.tasks.length === 0) list.emptiedThisMessage = true;
				}
				if (list.tui) invalidatePane(list.tui);
			}
			const details = { [TODO_DETAILS_KEY]: result.state, ...(result.error ? { error: result.error } : {}) };
			return { content: [{ type: "text", text: result.text }], details };
		},
	});

	pi.on("session_start", (_event, ctx) => {
		const list = sessionOf(ctx);
		const branch = ctx.sessionManager.getBranch();
		// A list already finished when the session was left is history, not work.
		const replayed = replayTodo(branch);
		const finished = replayed.tasks.length > 0 && todoSummary(replayed).open === 0;
		list.state = finished ? { ...replayed, tasks: [] } : replayed;
		list.turn = ownerMessages(ctx.sessionManager.getBranch());
		list.turnsWithoutUpdate = 0;
		list.reminded = false;
		list.updatedThisTurn = false;
		list.ownerTurns = 0;
		list.ownerChanged = false;
		list.startSent = false;
		list.emptiedThisMessage = false;
		list.lastBlock = lastBlockOn(ctx.sessionManager.getBranch());
		list.ui = ctx.hasUI ? ctx.ui : undefined;
		draw(list);
	});

	pi.on("session_shutdown", (_event, ctx) => {
		sessions.delete(ctx.sessionManager.getSessionId() ?? "");
	});

	pi.on("before_agent_start", (_event, ctx) => {
		const list = sessionOf(ctx);
		list.turn += 1;
		if (list.tui) invalidatePane(list.tui);
		if (list.clearAtNextRun) {
			list.state = { ...list.state, tasks: [] };
			list.clearAtNextRun = false;
			draw(list);
		}
		// A new owner message: the start nudge counts from here.
		list.ownerTurns = 0;
		list.ownerChanged = false;
		list.startSent = false;
		list.emptiedThisMessage = false;
		// The open list; with none, the start rule, which neither a child nor a
		// lean session (T146h L4) gets.
		const open = todoPromptBlock(list.state, staleTurns(list.state, list.turn));
		const block = open ?? (child || contextLevel() === "lean" ? undefined : todoStartBlock(list.state));
		// T115d: sent as a persisted message when it changed; never in the system prompt.
		if (!block || block === list.lastBlock) return undefined;
		list.lastBlock = block;
		// pi hands it to the model as user text: it says it is not the owner (T194).
		return { message: { customType: TODO_LIST_MESSAGE, content: fromTheShell(block), display: false } };
	});

	pi.on("turn_end", (event, ctx) => {
		const list = sessionOf(ctx);
		const calls = event.message.role === "assistant" ? event.message.content.filter((part) => part.type === "toolCall") : [];
		// A turn with a todo change is up to date; a failed change or a list read is not.
		if (calls.length > 0 && !list.updatedThisTurn) list.turnsWithoutUpdate += 1;
		if (calls.length > 0) {
			list.ownerTurns += 1;
			if (calls.some((call) => changes(call.name, call.arguments))) list.ownerChanged = true;
		}
		list.updatedThisTurn = false;
		// The nudges follow a tool-use turn, persisted, ahead of the next request; a child gets none.
		if (calls.length === 0 || child) return;
		const nudge = (customType: string, content: string) => pi.sendMessage({ customType, content, display: false }, { deliverAs: "steer" });
		if (list.state.tasks.length === 0) {
			const due = !list.startSent && !list.emptiedThisMessage && list.ownerChanged && list.ownerTurns >= START_NUDGE_AFTER;
			if (contextLevel() === "lean" || !due) return;
			list.startSent = true;
			nudge(TODO_START_MESSAGE, START_TEXT);
			return;
		}
		if (list.reminded || list.turnsWithoutUpdate < REMINDER_AFTER || !hasActionable(list.state)) return;
		list.reminded = true;
		nudge(TODO_REMINDER_MESSAGE, REMINDER_TEXT);
	});

	pi.on("tool_execution_end", (event, ctx) => {
		if (event.toolName === TODO_TOOL_NAME) draw(sessionOf(ctx));
	});

	pi.on("agent_end", (_event, ctx) => {
		const list = sessionOf(ctx);
		if (list.state.tasks.length > 0 && todoSummary(list.state).open === 0) list.clearAtNextRun = true;
		draw(list);
	});
}
