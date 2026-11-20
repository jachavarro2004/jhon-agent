import { sanitizeTerminalText } from "./ui/terminal-theme.ts";

// The agent's todo list (extensions/todo.ts): the steps the model keeps while
// it works. Everything here is pure. The state lives in the session branch
// (every tool result carries the full snapshot), and staleness is how many
// turns passed since the last write while tasks are still open.

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
/** A list with open work untouched for this many turns is stale. */
export const STALE_AFTER_TURNS = 2;

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
		"Inside an LDD change or not, the todo list holds your own next steps for this request; the change's tasks stay in `jhon status` and tasks.md, never copied here. For work of three or more steps, write it with `todo write` before the first edit and keep it current: one task in_progress, mark it done right after it is verified. A question, a single read or a one-line fix needs no list; a small change (`jhon status` says so) keeps no tasks.md, archive, light verify or todo list.",
	].join("\n");
}
