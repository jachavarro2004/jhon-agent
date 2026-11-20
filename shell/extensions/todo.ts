import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { ScrollView, Text, truncateToWidth, type Component, type TUI } from "@earendil-works/pi-tui";
import { NativePointerRegion } from "../lib/ui/native-pointer-region.ts";
import { setSidebarHover, sidebarPart, sidebarState } from "../lib/ui/shell-sidebar.ts";
import { invalidateSidebar } from "../lib/ui/shell-sidebar-layout.ts";
import { panelHeaderRow } from "../lib/ui/shell-card.ts";
import { renderPanelRow } from "../lib/ui/shell-panel.ts";
import { asciiText } from "../lib/ui/primitives/glyphs.ts";
import { onSelection } from "../lib/ui/ldd-status.ts";
import { contextLevel } from "../lib/lean-context.ts";
import {
	applyTodo,
	emptyTodo,
	renderTodoCard,
	renderTodoSection,
	replayTodo,
	staleTurns,
	TODO_DETAILS_KEY,
	TODO_GLYPH,
	TODO_TOOL_NAME,
	todoPromptBlock,
	todoStartBlock,
	todoCardTone,
	todoSummary,
	todoWorkSummary,
	type TodoParams,
	type TodoState,
} from "../lib/ui/shell-todo.ts";
import { counted } from "../lib/ui-use.ts";
import { shellSettings, type ShellSettings } from "../lib/settings.ts";

// Jhon's todo: the task list the model keeps while it works, drawn as a
// jhon-shell card above the editor. Three things keep it current that a
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
// keep one too (T134), private to their child process, with no card, no
// keys, no start rule and no nudge, since a child's task is bounded.

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
	if (inChild(env) && !(env.JHON_PI_TOOLS ?? "").split(",").includes(TODO_TOOL_NAME)) return false;
	const value = env.JHON_SHELL_TODO?.trim().toLowerCase();
	return !(value === "0" || value === "false" || value === "off");
}

/**
 * The key that folds the todo card, shell.keys.todos (JHON_SHELL_TODO_KEY
 * overrides it), or off. alt+t by default, beside the shell's alt+g, alt+j,
 * alt+s and alt+u: terminals take ctrl+shift+t for a new tab (Ghostty does),
 * so it never reached pi.
 */
export function todoCollapseKey(settings: ShellSettings = shellSettings()): string | undefined {
	return settings.key("todos");
}

/**
 * The keys that scroll the todo list three rows, and how its position row
 * names them (T165). Not ctrl+shift+up/down: pi 1.5.2 binds those to
 * tui.altScreen.previousPrompt/nextPrompt, and in fullscreen its transcript
 * listener consumes them before the editor asks the extensions, so the
 * todo's shortcut never ran there. pi binds no alt+pageUp/alt+pageDown
 * (docs/keybindings.md), and its plain pageUp/pageDown do not match them.
 * A constant until shell.keys.* holds a pair for it (settings.ts, the engine).
 */
export const TODO_SCROLL_KEYS = Object.freeze({ up: "alt+pageUp", down: "alt+pageDown", hint: "alt+PgUp/PgDn" } as const);

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
	collapsed: boolean;
	/** A finished list stays on screen for the turn it finished in, then clears. */
	clearOnNextTurn: boolean;
	ui: ExtensionContext["ui"] | undefined;
	host: { requestRender(): void } | undefined;
	tui: TUI | undefined;
	/** Resolve the visible host when the key is pressed, not when it renders. */
	scrollTodo?: (lines: number) => void;
	/** The session's id, for STATUS's selection of its LDD change. */
	id: string;
	/** Stops following that selection. */
	unfollow?: () => void;
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
	const collapseKey = child ? undefined : todoCollapseKey();

	const session = (ctx: ExtensionContext): TodoSession => {
		const key = sessionKey(ctx);
		let current = sessions.get(key);
		if (!current) {
			current = { state: emptyTodo(), turn: 0, toolTurnsSinceWrite: 0, reminded: false, wroteThisTurn: false, ownerToolTurns: 0, ownerChanged: false, startNudged: false, clearedByOwnerTurn: false, collapsed: false, clearOnNextTurn: false, ui: undefined, host: undefined, tui: undefined, id: key };
			sessions.set(key, current);
		}
		return current;
	};

	const toggle = (current: TodoSession) => {
		current.collapsed = !current.collapsed;
		if (current.tui) invalidateSidebar(current.tui);
		current.host?.requestRender();
	};

	// The bottom widget keeps a spacer row under the card; the rail does not.
	// The rail draws the Gentle Shell's panel section (header row, task rows, no
	// closing rule), which the rail itself puts in a card or on its panel; the
	// bottom widget keeps the card with its chrome, as G's gentle-todo.
	const todoCard = (current: TodoSession, theme: Parameters<typeof renderTodoCard>[1], spacer: boolean): Component & { dispose(): void; digest(): string; scrollBy(lines: number): void; surface?: "panel"; summary?(): ReturnType<typeof todoWorkSummary> } => {
		let hovered = false;
		let body: string[] = [];
		let collapsed = current.collapsed;
		// Widgets are measured as leaves, so native layout cannot assign this
		// body a viewport. Use native scroll state and explicitly slice its lines
		// while keeping the collapse header and card footer outside the viewport.
		const scroll = new ScrollView({ render: () => body, invalidate() {} }, { follow: "none", overscroll: "contain" });
		const scrollBy = (lines: number) => {
			if (current.collapsed) return;
			scroll.scrollBy(lines);
			if (current.tui) invalidateSidebar(current.tui);
			current.host?.requestRender();
		};
		// The row the rendered card draws its header on: 0 for the outlined
		// frame and the rail's section, 1 below the float panel's top padding row.
		let headerRow = 0;
		const card: Component = {
			render(width: number) {
				const stale = staleTurns(current.state, current.turn);
				let head: string[];
				let foot: string[];
				if (spacer) {
					headerRow = panelHeaderRow(theme, width, todoCardTone(stale));
					const lines = renderTodoCard(current.state, theme, width, {
						collapsed: current.collapsed,
						staleTurns: stale,
						...(collapseKey ? { collapseKey } : {}),
						hovered,
						scrollable: true,
					});
					const bodyStart = headerRow === 1 ? 3 : 1;
					head = lines.slice(0, bodyStart);
					body = lines.slice(bodyStart, -1);
					foot = lines.slice(-1);
				} else {
					headerRow = 0;
					const section = renderTodoSection(current.state, theme, width, { collapsed: current.collapsed, staleTurns: stale, hovered, ...(collapseKey ? { collapseKey } : {}) });
					head = section.header ? [section.header] : [];
					body = section.body;
					foot = [];
				}
				if (collapsed !== current.collapsed) {
					collapsed = current.collapsed;
					scroll.scrollToStart();
				}
				// Above the prompt and in the rail alike, as Gentle Shell's: at most
				// one third of the screen, capped at 16 rows (C10). Read height on
				// every render so a narrow resize immediately releases chat space.
				const terminalRows = current.tui?.terminal?.rows ?? 48;
				const height = Math.min(16, Math.floor(terminalRows / 3));
				if (head.length === 0) return [];
				const overflow = !current.collapsed && head.length + body.length + foot.length + Number(spacer) > height;
				const room = Math.max(1, height - head.length - foot.length - Number(spacer) - Number(overflow));
				scroll.updateLayout(body.length, current.collapsed ? body.length : room, () => current.host?.requestRender());
				const visible = scroll.render(width).slice(scroll.scrollTop, scroll.scrollTop + scroll.viewportHeight);
				const output = [...head, ...visible, ...foot];
				if (overflow) {
					const position = `↑↓ ${scroll.scrollTop + 1}–${scroll.scrollTop + visible.length}/${body.length} · ${TODO_SCROLL_KEYS.hint}`;
					output.push(spacer ? theme.fg("muted", truncateToWidth(position, width)) : renderPanelRow(width, theme.fg("muted", position)));
				}
				return spacer && output.length > 0 ? [...output, ""] : output;
			},
			invalidate() {
				hovered = false;
			},
		};
		const region = new NativePointerRegion(card, {
			onHover(event) {
				// The region spans the whole card, but only the header row is the
				// clickable control, so a move elsewhere in the card clears hover
				// exactly like leaving the region entirely would.
				const next = event.y === headerRow;
				// One hovered control across the shell: a move elsewhere clears this one.
				if (current.tui) {
					if (next) setSidebarHover(current.tui, region, () => { region.clearHover(); });
					else setSidebarHover(current.tui);
				}
				if (next === hovered) return { handled: true };
				hovered = next;
				return { handled: true, render: true };
			},
			onLeave() {
				if (!hovered) return;
				hovered = false;
				current.host?.requestRender();
			},
			onClick(event) {
				if (event.button !== "left" || event.y !== headerRow) return undefined;
				toggle(current);
				return { handled: true, render: true };
			},
			onWheel(event) {
				if (current.collapsed || body.length <= scroll.viewportHeight) return undefined;
				scrollBy(event.wheelDelta ?? 0);
				return { handled: true, render: true };
			},
		});
		return {
			scrollBy,
			// Sidebar caches must observe height-only resizes and local viewport
			// changes, even when terminal width and task content stay the same.
			digest: () => JSON.stringify([current.tui?.terminal?.rows, current.collapsed, current.turn, hovered, scroll.scrollTop]),
			render: (width) => region.render(width),
			handleMouse: (event) => region.handleMouse(event),
			invalidate: () => region.invalidate(),
			dispose: () => region.dispose(),
			// The rail part is a panel section and gives the narrow work row its digest (T15.12c).
			...(spacer ? {} : { surface: "panel" as const, summary: () => todoWorkSummary(current.state, theme) }),
		};
	};

	const show = (current: TodoSession) => {
		if (current.tui) invalidateSidebar(current.tui);
		// A child's list is its own: no card, no rail part (T134).
		if (!current.ui || child) return;
		// An empty list stays mounted: the rail shows TODOS idle,
		// as AGENTS (L175); the card above the prompt renders nothing.
		const snapshot = current;
		current.ui.setWidget(WIDGET_KEY, (tui, theme) => {
			snapshot.host = tui;
			snapshot.tui = tui;
			const bottom = todoCard(snapshot, theme, true);
			const rail = todoCard(snapshot, theme, false);
			snapshot.scrollTodo = (lines) => {
				// Cached rail lines do not re-render when a narrow→wide resize
				// restores the sidebar. Select ownership from the live layout.
				const state = tui.terminal ? sidebarState(tui) : undefined;
				(state?.active && state.ownsHost?.() ? rail : bottom).scrollBy(lines);
			};
			return sidebarPart(tui, "todo", bottom, rail);
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
				if (current.tui) invalidateSidebar(current.tui);
			}
			return {
				content: [{ type: "text", text: result.text }],
				details: { [TODO_DETAILS_KEY]: result.state, ...(result.error ? { error: result.error } : {}) },
			};
		},
	});

	if (collapseKey) {
		pi.registerShortcut(collapseKey as Parameters<ExtensionAPI["registerShortcut"]>[0], {
			description: "Collapse or expand the plan",
			handler: async (ctx) => {
				toggle(session(ctx));
			},
		});
	}

	for (const [key, lines] of child ? [] : [[TODO_SCROLL_KEYS.up, -3], [TODO_SCROLL_KEYS.down, 3]] as const) {
		pi.registerShortcut(key, {
			description: `Scroll the plan ${lines < 0 ? "up" : "down"}`,
			handler: async (ctx) => session(ctx).scrollTodo?.(lines),
		});
	}

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
		// STATUS selects the change after its own read: the card follows it.
		current.unfollow?.();
		if (!child) current.unfollow = onSelection(() => show(current));
		show(current);
	});

	pi.on("session_shutdown", (_event, ctx) => {
		sessions.get(sessionKey(ctx))?.unfollow?.();
		sessions.delete(sessionKey(ctx));
	});

	pi.on("before_agent_start", (_event, ctx) => {
		const current = session(ctx);
		current.turn += 1;
		if (current.tui) invalidateSidebar(current.tui);
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
