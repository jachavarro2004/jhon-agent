import { truncateToWidth, wrapTextWithAnsi } from "@earendil-works/pi-tui";
import { shellSettings } from "../settings.ts";
import { CARD_TONE, panelHeaderRow, panelInnerWidth, renderCard, type CardTheme, type CardTone } from "./shell-card.ts";
import { asciiText, glyph } from "./primitives/glyphs.ts";
import { paintHoverable } from "./shell-hover.ts";
import { PANEL_ROW_INDENT, renderPanelHeader, renderPanelRow } from "./shell-panel.ts";
import { sanitizeTerminalText } from "./terminal-theme.ts";
import { STATES } from "./primitives/states.ts";
import { THEME_NAME } from "./primitives/paint.ts";

// Jhon's todo: the task list the model keeps while it works. Everything here
// is pure. The state lives in the session branch (every tool result carries
// the full snapshot), and staleness is how many turns passed since the last
// write while tasks are still open.

export const TODO_STATUS = {
	PENDING: "pending",
	IN_PROGRESS: "in_progress",
	/** Open, but waiting on something outside the list; always carries a note naming it. */
	BLOCKED: "blocked",
	DONE: "done",
	/** Will not be done, on purpose: terminal, and never counts as open. */
	DROPPED: "dropped",
} as const;

export type TodoStatus = (typeof TODO_STATUS)[keyof typeof TODO_STATUS];

export const TODO_ACTION = {
	WRITE: "write",
	ADD: "add",
	UPDATE: "update",
	CLEAR: "clear",
	LIST: "list",
} as const;

export interface TodoTask {
	id: number;
	title: string;
	status: TodoStatus;
	note?: string;
}

export interface TodoState {
	tasks: TodoTask[];
	nextId: number;
	/** Turn index of the last write, or null before the first one. */
	updatedTurn: number | null;
}

export interface TodoTaskInput {
	id?: number;
	title?: string;
	status?: string;
	note?: string;
}

export interface TodoParams {
	action: string;
	tasks?: TodoTaskInput[];
	id?: number;
	title?: string;
	status?: string;
	note?: string;
}

export interface TodoResult {
	state: TodoState;
	text: string;
	error?: string;
}

export interface TodoSummary {
	done: number;
	total: number;
	open: number;
}

export interface TodoTheme extends CardTheme {
	strikethrough(text: string): string;
}

export interface TodoRenderOptions {
	/** A scrollable host supplies the height bound instead of folding tasks; each task but the one in progress is one row. */
	scrollable?: boolean;
	collapsed: boolean;
	staleTurns: number;
	/** The fold key (shell.keys.todos); read from the settings when absent. */
	collapseKey?: string;
	/** The pointer is over the clickable collapse/expand control. */
	hovered?: boolean;
}

/** Tool results carry the snapshot under this key; the old rpiv-todo shape is read too. */
export const TODO_DETAILS_KEY = "jhonTodo";
// The key before T59, still read so sessions saved under it restore.
export const LEGACY_TODO_DETAILS_KEY = "gentleTodo";

/** The snapshot a todo result's details carry, under either key; undefined when neither is there. */
export function todoDetails(details: Record<string, unknown> | undefined): { state: TodoState | undefined } | undefined {
	if (!details || typeof details !== "object") return undefined;
	for (const key of [TODO_DETAILS_KEY, LEGACY_TODO_DETAILS_KEY]) {
		if (Object.hasOwn(details, key)) return { state: details[key] as TodoState | undefined };
	}
	return undefined;
}
export const TODO_TOOL_NAME = "todo";
/** The todo card's glyph: G's brand ❀ becomes Jhon's 力 (L146). */
export const TODO_GLYPH = "力";
const STATUS_ALIASES: Record<string, TodoStatus> = {
	completed: TODO_STATUS.DONE,
	complete: TODO_STATUS.DONE,
	doing: TODO_STATUS.IN_PROGRESS,
	todo: TODO_STATUS.PENDING,
	cancelled: TODO_STATUS.DROPPED,
	canceled: TODO_STATUS.DROPPED,
	abandoned: TODO_STATUS.DROPPED,
};
const STATUS_LIST = "pending, in_progress, blocked, done, or dropped";
const BLOCKED_NEEDS_NOTE = "blocked needs a note naming what it waits for";
// The shell's state glyphs (states.ts): blocked needs you, so it is ◉ in peachRed.
const STATUS_GLYPH: Record<TodoStatus, string> = { [TODO_STATUS.PENDING]: STATES.pending.glyph, [TODO_STATUS.IN_PROGRESS]: STATES.working.glyph, [TODO_STATUS.BLOCKED]: STATES.blocked.glyph, [TODO_STATUS.DONE]: STATES.done.glyph, [TODO_STATUS.DROPPED]: "✕" };
const STATUS_ROLE: Record<TodoStatus, string> = { [TODO_STATUS.PENDING]: "text", [TODO_STATUS.IN_PROGRESS]: "accent", [TODO_STATUS.BLOCKED]: "text", [TODO_STATUS.DONE]: "dim", [TODO_STATUS.DROPPED]: "dim" };
const GLYPH_ROLE: Record<TodoStatus, string> = { [TODO_STATUS.PENDING]: THEME_NAME[STATES.pending.role], [TODO_STATUS.IN_PROGRESS]: THEME_NAME[STATES.working.role], [TODO_STATUS.BLOCKED]: THEME_NAME[STATES.blocked.role], [TODO_STATUS.DONE]: THEME_NAME[STATES.done.role], [TODO_STATUS.DROPPED]: "muted" };
const NOTE_ROLE = "muted";
const STALE_AFTER_TURNS = 2;
/** Above this many rows the finished tasks fold into one line and the rest is capped. */
const ROW_CAP = 12;

export function emptyTodo(): TodoState {
	return { tasks: [], nextId: 1, updatedTurn: null };
}

function cleanText(value: unknown): string {
	return typeof value === "string" ? sanitizeTerminalText(value).replace(/\s+/g, " ").trim() : "";
}

function parseStatus(value: unknown): TodoStatus | undefined {
	if (value === undefined || value === null || value === "") return TODO_STATUS.PENDING;
	if (typeof value !== "string") return undefined;
	const normalized = value.trim().toLowerCase().replace(/[\s-]+/g, "_");
	if ((Object.values(TODO_STATUS) as string[]).includes(normalized)) return normalized as TodoStatus;
	return STATUS_ALIASES[normalized];
}

function countStatus(state: TodoState, status: TodoStatus): number {
	return state.tasks.filter((task) => task.status === status).length;
}

/** Dropped tasks are neither done nor open. */
export function todoSummary(state: TodoState): TodoSummary {
	const done = countStatus(state, TODO_STATUS.DONE);
	return { done, total: state.tasks.length, open: state.tasks.length - done - countStatus(state, TODO_STATUS.DROPPED) };
}

// Only work the model can pick up goes stale: a list whose open tasks all
// wait on something outside it has nothing to bring up to date.
export function staleTurns(state: TodoState, currentTurn: number): number {
	const actionable = state.tasks.some((task) => task.status === TODO_STATUS.PENDING || task.status === TODO_STATUS.IN_PROGRESS);
	if (state.updatedTurn === null || !actionable) return 0;
	return Math.max(0, currentTurn - state.updatedTurn);
}

function summaryText(state: TodoState): string {
	const { done, total } = todoSummary(state);
	const inProgress = countStatus(state, TODO_STATUS.IN_PROGRESS);
	const blocked = countStatus(state, TODO_STATUS.BLOCKED);
	const dropped = countStatus(state, TODO_STATUS.DROPPED);
	return `${total} ${total === 1 ? "task" : "tasks"} · ${done} done · ${inProgress} in progress${blocked ? ` · ${blocked} blocked` : ""}${dropped ? ` · ${dropped} dropped` : ""}`;
}

function taskLine(task: TodoTask): string {
	return `[${task.status}] #${task.id} ${task.title}${task.note ? ` — ${task.note}` : ""}`;
}

function buildTask(input: TodoTaskInput, id: number): TodoTask | string {
	const title = cleanText(input.title);
	if (title.length === 0) return "title is required";
	const status = parseStatus(input.status);
	if (status === undefined) return `unknown status "${input.status}" (use ${STATUS_LIST})`;
	const note = cleanText(input.note);
	if (status === TODO_STATUS.BLOCKED && note.length === 0) return BLOCKED_NEEDS_NOTE;
	return { id, title, status, ...(note ? { note } : {}) };
}

function write(state: TodoState, inputs: TodoTaskInput[], turn: number): TodoResult {
	const tasks: TodoTask[] = [];
	let nextId = state.nextId;
	for (const input of inputs) {
		const keepId = typeof input.id === "number" && Number.isInteger(input.id) && input.id > 0 && !tasks.some((task) => task.id === input.id);
		const id = keepId ? (input.id as number) : nextId++;
		nextId = Math.max(nextId, id + 1);
		const built = buildTask(input, id);
		if (typeof built === "string") return { state, text: `Error: ${built}`, error: built };
		tasks.push(built);
	}
	const next = { tasks, nextId, updatedTurn: turn };
	return { state: next, text: `Todo list written: ${summaryText(next)}` };
}

function update(state: TodoState, params: TodoParams, turn: number): TodoResult {
	const task = state.tasks.find((candidate) => candidate.id === params.id);
	if (!task) {
		const error = `no task #${params.id}`;
		return { state, text: `Error: ${error}`, error };
	}
	if (params.status === undefined && params.title === undefined && params.note === undefined) {
		const error = "nothing to update: pass status, title, or note";
		return { state, text: `Error: ${error}`, error };
	}
	const built = buildTask({ id: task.id, title: params.title ?? task.title, status: params.status ?? task.status, note: params.note ?? task.note }, task.id);
	if (typeof built === "string") return { state, text: `Error: ${built}`, error: built };
	const next = { ...state, tasks: state.tasks.map((candidate) => (candidate.id === task.id ? built : candidate)), updatedTurn: turn };
	const change = task.status === built.status ? "updated" : `→ ${built.status}`;
	return { state: next, text: `#${built.id} ${built.title} ${change}` };
}

export function applyTodo(state: TodoState, params: TodoParams, turn: number): TodoResult {
	switch (params.action) {
		case TODO_ACTION.WRITE:
			return write(state, Array.isArray(params.tasks) ? params.tasks : [], turn);
		case TODO_ACTION.ADD: {
			const built = buildTask({ title: params.title, status: params.status, note: params.note }, state.nextId);
			if (typeof built === "string") return { state, text: `Error: ${built}`, error: built };
			const next = { tasks: [...state.tasks, built], nextId: state.nextId + 1, updatedTurn: turn };
			return { state: next, text: `Added #${built.id} ${built.title} (${built.status})` };
		}
		case TODO_ACTION.UPDATE:
			return update(state, params, turn);
		case TODO_ACTION.CLEAR:
			return { state: { ...emptyTodo(), updatedTurn: turn }, text: `Cleared ${state.tasks.length} ${state.tasks.length === 1 ? "task" : "tasks"}` };
		case TODO_ACTION.LIST:
			return { state, text: state.tasks.length === 0 ? "No tasks." : state.tasks.map(taskLine).join("\n") };
		default: {
			const error = `unknown action "${params.action}" (use write, add, update, clear, or list)`;
			return { state, text: `Error: ${error}`, error };
		}
	}
}

interface ReplayEntry {
	type?: string;
	message?: { role?: string; toolName?: string; isError?: boolean; details?: unknown };
}

interface RpivTask {
	id?: number;
	subject?: string;
	status?: string;
	activeForm?: string;
}

function fromRpiv(details: { tasks?: RpivTask[]; nextId?: number }): TodoState {
	const tasks: TodoTask[] = [];
	for (const raw of details.tasks ?? []) {
		if (raw.status === "deleted" || typeof raw.id !== "number") continue;
		const built = buildTask({ id: raw.id, title: raw.subject, status: raw.status, note: raw.status === TODO_STATUS.IN_PROGRESS ? raw.activeForm : undefined }, raw.id);
		if (typeof built !== "string") tasks.push(built);
	}
	return { tasks, nextId: typeof details.nextId === "number" ? details.nextId : tasks.reduce((max, task) => Math.max(max, task.id), 0) + 1, updatedTurn: null };
}

// The last successful `todo` result on the branch is the state: ours carries
// the snapshot under `jhonTodo` (`gentleTodo` before T59); rpiv-todo carried
// `tasks` + `nextId` at the top.
export function replayTodo(entries: readonly unknown[]): TodoState {
	let state = emptyTodo();
	for (const entry of entries as ReplayEntry[]) {
		const message = entry?.message;
		if (entry?.type !== "message" || message?.role !== "toolResult" || message.toolName !== TODO_TOOL_NAME || message.isError === true) continue;
		const details = message.details as Record<string, unknown> | undefined;
		if (!details || typeof details !== "object") continue;
		const ours = todoDetails(details)?.state;
		if (ours && Array.isArray(ours.tasks)) state = { tasks: ours.tasks, nextId: ours.nextId, updatedTurn: ours.updatedTurn ?? null };
		else if (Array.isArray(details.tasks)) state = fromRpiv(details as { tasks?: RpivTask[]; nextId?: number });
	}
	return state;
}

export function todoPromptBlock(state: TodoState, stale: number): string | undefined {
	if (todoSummary(state).open === 0) return undefined;
	const lines = state.tasks.map((task, index) => `${index + 1}. [${task.status}] ${task.title}${task.note ? ` — ${task.note}` : ""}`);
	const staleLine = stale >= STALE_AFTER_TURNS ? `\n(stale: ${stale} turns without an update — bring the list up to date now)` : "";
	return [
		"## Todo list",
		"Keep it current with the `todo` tool: mark a task in_progress before starting it, done right after finishing it, and rewrite the whole list with `write` whenever the plan changes. Update it before you end the turn.",
		"A blocked task waits on something outside the list: do not work on it until its condition holds. When the plan changes, mark the tasks it made obsolete dropped instead of leaving them pending or marking them done.",
		...lines,
	].join("\n") + staleLine;
}

// S34: with nothing open the model is told when to start a list, so it does
// so on its own instead of waiting to be asked. It ships every turn: keep it
// short. Inside an LDD change too: the list is the agent's own next steps,
// never the change's tasks (T134, L1019; T49 silenced it there).
export function todoStartBlock(state: TodoState): string | undefined {
	if (todoSummary(state).open > 0) return undefined;
	return [
		"## Todo list",
		"Inside an LDD change or not, the todo list holds your own next steps for this request; the change's tasks stay in `jhon status` and tasks.md, never copied here. For work of three or more steps, or any change to code, write it with `todo write` before the first edit and keep it current: one task in_progress, mark it done right after it is verified. A question, a single read or a one-line fix needs no list.",
	].join("\n");
}

function taskRow(task: TodoTask, theme: TodoTheme, inner: number): string {
	if (task.status === TODO_STATUS.DONE) {
		// Wrap before applying SGR 9: every physical title closes its own style
		// before renderCard adds padding and rails.
		return wrapTextWithAnsi(task.title, Math.max(1, inner - 2))
			.map((line, index) => `${index === 0 ? theme.fg(GLYPH_ROLE[task.status], STATUS_GLYPH[task.status]) : " "} ${theme.fg(STATUS_ROLE[task.status], theme.strikethrough(line))}`)
			.join("\n");
	}
	const showsNote = task.status === TODO_STATUS.IN_PROGRESS || task.status === TODO_STATUS.BLOCKED;
	const note = showsNote && task.note ? ` ${theme.fg(NOTE_ROLE, "·")} ${theme.fg(NOTE_ROLE, task.note)}` : "";
	return `${theme.fg(GLYPH_ROLE[task.status], STATUS_GLYPH[task.status])} ${theme.fg(STATUS_ROLE[task.status], task.title)}${note}`;
}

// G's body rows: every task keeps its row in order, a done one struck. Long
// lists keep the card short: done and dropped tasks fold into "✓ N done" and
// "✕ N dropped", and the open ones fill the remaining rows, with a trailing
// count for the rest.
function bodyRows(state: TodoState, theme: TodoTheme, inner: number): string[] {
	if (state.tasks.length <= ROW_CAP) return state.tasks.map((task) => taskRow(task, theme, inner));
	const open = state.tasks.filter((task) => task.status !== TODO_STATUS.DONE && task.status !== TODO_STATUS.DROPPED);
	const rows: string[] = [];
	for (const [status, label] of [[TODO_STATUS.DONE, "done"], [TODO_STATUS.DROPPED, "dropped"]] as const) {
		const count = countStatus(state, status);
		if (count > 0) rows.push(`${theme.fg(GLYPH_ROLE[status], STATUS_GLYPH[status])} ${theme.fg(NOTE_ROLE, `${count} ${label}`)}`);
	}
	const room = ROW_CAP - rows.length - (open.length > ROW_CAP - rows.length ? 1 : 0);
	for (const task of open.slice(0, room)) rows.push(taskRow(task, theme, inner));
	if (open.length > room) rows.push(theme.fg(NOTE_ROLE, `… ${open.length - room} more`));
	return rows;
}

/**
 * The todo list's one-row digest for the narrow work row, as the Gentle
 * Shell's: the task in progress (else the next pending or blocked one, else
 * `✓ N done`) and `done/planned`, named `todos` when the agents lead (T15.12c).
 */
export function todoWorkSummary(state: TodoState, theme: Pick<TodoTheme, "fg">): { left: string; right: string; rightLabel: string } | undefined {
	if (state.tasks.length === 0) return undefined;
	const { done, total } = todoSummary(state);
	const planned = total - countStatus(state, TODO_STATUS.DROPPED);
	const current = [TODO_STATUS.IN_PROGRESS, TODO_STATUS.PENDING, TODO_STATUS.BLOCKED].map((status) => state.tasks.find((task) => task.status === status)).find(Boolean);
	const left = current
		? `${theme.fg(GLYPH_ROLE[current.status], STATUS_GLYPH[current.status])} ${theme.fg(STATUS_ROLE[current.status], current.title)}`
		: `${theme.fg(GLYPH_ROLE[TODO_STATUS.DONE], STATUS_GLYPH[TODO_STATUS.DONE])} ${theme.fg(NOTE_ROLE, `${done} done`)}`;
	return { left, right: `${done}/${planned}`, rightLabel: "todos" };
}

/** The tone the Todos card renders in: warning once the list went stale, info otherwise. */
export function todoCardTone(staleTurns: number): CardTone {
	return staleTurns >= STALE_AFTER_TURNS ? CARD_TONE.WARNING : CARD_TONE.INFO;
}

/** A folded card's one row, as G's: the task in progress, else the next pending, else the blocked one, else `· N open`. */
function collapsedRow(state: TodoState, theme: TodoTheme, inner: number): string {
	const current = [TODO_STATUS.IN_PROGRESS, TODO_STATUS.PENDING, TODO_STATUS.BLOCKED].map((status) => state.tasks.find((task) => task.status === status)).find(Boolean);
	if (current) return taskRow(current, theme, inner);
	return `${theme.fg(GLYPH_ROLE[TODO_STATUS.PENDING], STATUS_GLYPH[TODO_STATUS.PENDING])} ${theme.fg(NOTE_ROLE, `${todoSummary(state).open} open`)}`;
}

/**
 * The todo list as the Gentle Shell's card above the editor (its
 * renderTodoCard, in the card style): `力 Todos ▾ Collapse · <done> of
 * <planned>`, the fold key at the right (with its action on a neon card), the
 * clickable control in the hover role while hovered; folded, the current
 * task alone; stale, a `stale · N turns` row first, in the warning tone.
 */
export function renderTodoCard(state: TodoState, theme: TodoTheme, width: number, options: TodoRenderOptions): string[] {
	if (state.tasks.length === 0) return [];
	const { done, total } = todoSummary(state);
	// Dropped work is not part of the plan anymore, so it leaves the denominator.
	const planned = total - countStatus(state, TODO_STATUS.DROPPED);
	const stale = options.staleTurns >= STALE_AFTER_TURNS;
	const action = options.collapsed ? "expand" : "collapse";
	const actionLabel = action[0]!.toUpperCase() + action.slice(1);
	const icon = glyph(options.collapsed ? "closed" : "open", shellSettings().ascii());
	const control = `${icon} ${width >= 28 ? actionLabel : ""}`.trimEnd();
	const tone = todoCardTone(options.staleTurns);
	const collapseKey = options.collapseKey ?? shellSettings().key("todos");
	const hint = collapseKey ? (panelHeaderRow(theme, width, tone) ? collapseKey : `${collapseKey} ${action}`) : undefined;
	// Content columns of the card body in the active style (float is narrower).
	const inner = panelInnerWidth(theme, width, tone);
	const rows = options.collapsed ? [collapsedRow(state, theme, inner)] : options.scrollable ? state.tasks.map((task) => taskRow(task, theme, inner)) : bodyRows(state, theme, inner);
	const staleLabel = theme.fg(NOTE_ROLE, `stale · ${options.staleTurns} turns`);
	let body = options.collapsed
		? [truncateToWidth(`${stale ? `${staleLabel} · ` : ""}${rows[0]}`, inner, "…")]
		: stale ? [staleLabel, ...rows] : rows;
	if (!options.collapsed && !options.scrollable) {
		// Budget physical lines after wrapping: one long task can exceed the
		// old task-count cap by itself. Scrollable hosts retain every line.
		body = body.flatMap((row) => wrapTextWithAnsi(row, Math.max(1, inner)));
		if (body.length > ROW_CAP) body = [...body.slice(0, ROW_CAP - 1), theme.fg(NOTE_ROLE, "… more")];
	}
	// The clickable control paints the shared hover role while hovered, as
	// every other clickable surface does, instead of its accent role.
	const ascii = shellSettings().ascii();
	return renderCard(
		{ title: `Todos ${paintHoverable(theme, control, options.hovered ?? false, "accent")}`, subtitle: `${done} of ${planned}`, body: ascii ? body.map(asciiText) : body, tone, glyph: TODO_GLYPH },
		theme,
		width,
		{ expanded: true, hint, panel: true },
	);
}

export interface TodoSection {
	/** The `▾ TODOS … done/planned` header row, the section's collapse control; "" without tasks. */
	header: string;
	/** One row per entry under the header, every row exactly `width` columns. */
	body: string[];
}

// The fullscreen rail's panel section: finished and dropped work fold into one
// row each, every open task takes one clipped row, and the note of the task in
// progress (or blocked) hangs under it. The task in progress or blocked is the
// one you read in full: its title and note wrap, so nothing about the current
// work is lost to a clip; every other task is one clipped row.
function sectionTaskRows(task: TodoTask, theme: TodoTheme, width: number): string[] {
	const mark = theme.fg(GLYPH_ROLE[task.status], STATUS_GLYPH[task.status]);
	const focus = task.status === TODO_STATUS.IN_PROGRESS || task.status === TODO_STATUS.BLOCKED;
	if (!focus) return [renderPanelRow(width, `${mark} ${theme.fg(STATUS_ROLE[task.status], task.title)}`)];
	const titleWidth = Math.max(1, width - PANEL_ROW_INDENT - 2);
	const rows = wrapTextWithAnsi(task.title, titleWidth).map((line, index) => renderPanelRow(width, `${index === 0 ? mark : " "} ${theme.fg(STATUS_ROLE[task.status], line)}`));
	if (!task.note) return rows;
	const noteWidth = Math.max(1, width - PANEL_ROW_INDENT - 4);
	return [...rows, ...wrapTextWithAnsi(task.note, noteWidth).map((line, index) => renderPanelRow(width, `${index === 0 ? theme.fg("dim", glyph("tree", shellSettings().ascii())) : " "} ${theme.fg(NOTE_ROLE, line)}`, "", PANEL_ROW_INDENT + 2))];
}

/**
 * The rail's TODOS as the Gentle Shell's panel section: `▾ TODOS … <key>
 * <done>/<planned>`, its own fold key dim before the count, the only key on a
 * rail header (C6); a stale list says so in the count, in the warning role,
 * and adds no row (T15.12d, L158); folded, the current task under the header.
 */
export function renderTodoSection(state: TodoState, theme: TodoTheme, width: number, options: Pick<TodoRenderOptions, "collapsed" | "staleTurns" | "hovered" | "collapseKey">): TodoSection {
	// The collapse key rides the header, small and dim, as the only rail key hint.
	const keyHint = options.collapseKey ? `${theme.fg("dim", options.collapseKey)}  ` : "";
	// Idle, as AGENTS idle reads (L175): `idle` as the count and one row, `no tasks`.
	if (state.tasks.length === 0) {
		const header = renderPanelHeader(theme, width, { title: "Todos", count: `${keyHint}idle`, open: !options.collapsed, hovered: options.hovered, ascii: shellSettings().ascii() });
		return { header, body: [renderPanelRow(width, theme.fg(NOTE_ROLE, "no tasks"))] };
	}
	const { done, total } = todoSummary(state);
	const planned = total - countStatus(state, TODO_STATUS.DROPPED);
	const stale = options.staleTurns >= STALE_AFTER_TURNS;
	const header = renderPanelHeader(theme, width, {
		title: "Todos",
		count: `${keyHint}${stale ? `stale · ${options.staleTurns} turns · ` : ""}${done}/${planned}`,
		countRole: stale ? "warning" : "muted",
		open: !options.collapsed,
		hovered: options.hovered,
		ascii: shellSettings().ascii(),
	});
	const open = state.tasks.filter((task) => task.status !== TODO_STATUS.DONE && task.status !== TODO_STATUS.DROPPED);
	// shell.appearance.ascii: the state glyphs in their one-cell ASCII forms.
	const shown = (rows: string[]) => shellSettings().ascii() ? rows.map(asciiText) : rows;
	if (options.collapsed) return { header, body: shown([renderPanelRow(width, collapsedRow(state, theme, width))]) };
	const body: string[] = [];
	for (const [status, label] of [[TODO_STATUS.DONE, "done"], [TODO_STATUS.DROPPED, "dropped"]] as const) {
		const count = countStatus(state, status);
		if (count > 0) body.push(renderPanelRow(width, `${theme.fg(GLYPH_ROLE[status], STATUS_GLYPH[status])} ${theme.fg(NOTE_ROLE, `${count} ${label}`)}`));
	}
	for (const task of open) body.push(...sectionTaskRows(task, theme, width));
	return { header, body: shown(body) };
}
