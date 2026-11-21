import { sanitizeTerminalText } from "./ui/terminal-theme.ts";

// The agent's own todo list (extensions/todo.ts): the steps the model keeps
// for the request it works on. Nothing here has state of its own: the list
// lives on the session branch, where every todo result carries a whole copy,
// and a list goes stale when turns pass with open work and no update.

export const TODO_STATUS = {
	PENDING: "pending",
	IN_PROGRESS: "in_progress",
	/** Open, but waiting on something outside the list; it always has a note naming what. */
	BLOCKED: "blocked",
	DONE: "done",
	/** Given up on purpose: finished, never open. */
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
	/** The turn of the last change, or null before the first. */
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

/** Where a todo result's details keep the list's copy. */
export const TODO_DETAILS_KEY = "jhonTodo";
/** Where they kept it before T59; sessions saved then still restore. */
export const LEGACY_TODO_DETAILS_KEY = "gentleTodo";

/** The list's copy in a todo result's details, under either key; undefined when neither is there. */
export function todoDetails(details: Record<string, unknown> | undefined): { state: TodoState | undefined } | undefined {
	if (!details || typeof details !== "object") return undefined;
	const key = [TODO_DETAILS_KEY, LEGACY_TODO_DETAILS_KEY].find((candidate) => Object.hasOwn(details, candidate));
	return key === undefined ? undefined : { state: details[key] as TodoState | undefined };
}

export const TODO_TOOL_NAME = "todo";
/** The glyph of the todo card's title (L146). */
export const TODO_GLYPH = "力";
/** Turns with open work and no update after which the list is stale. */
export const STALE_AFTER_TURNS = 2;

/** Other words a model uses for a status. */
const SYNONYMS: Record<string, TodoStatus> = {
	completed: "done", complete: "done",
	doing: "in_progress",
	todo: "pending",
	cancelled: "dropped", canceled: "dropped", abandoned: "dropped",
};
const STATUSES: readonly string[] = Object.values(TODO_STATUS);

const ERRORS = {
	noTitle: "title is required",
	blockedNote: "blocked needs a note naming what it waits for",
	status: (given: unknown) => `unknown status "${given}" (use pending, in_progress, blocked, done, or dropped)`,
	noTask: (id: unknown) => `no task #${id}`,
	nothing: "nothing to update: pass status, title, or note",
	action: (given: unknown) => `unknown action "${given}" (use write, add, update, clear, or list)`,
} as const;

export function emptyTodo(): TodoState {
	return { tasks: [], nextId: 1, updatedTurn: null };
}

/** A title or note on one line, with nothing a terminal would act on. */
function oneLine(value: unknown): string {
	if (typeof value !== "string") return "";
	return sanitizeTerminalText(value).replace(/\s+/g, " ").trim();
}

/** A status as the model wrote it: none is pending; case, spaces and dashes do not matter; a synonym counts. */
function statusOf(value: unknown): TodoStatus | undefined {
	if (value === undefined || value === null || value === "") return "pending";
	if (typeof value !== "string") return undefined;
	const word = value.trim().toLowerCase().replace(/[\s-]+/g, "_");
	return STATUSES.includes(word) ? (word as TodoStatus) : SYNONYMS[word];
}

const countOf = (state: TodoState, status: TodoStatus) => state.tasks.reduce((count, task) => count + (task.status === status ? 1 : 0), 0);
const plural = (count: number) => (count === 1 ? "task" : "tasks");
const noteSuffix = (task: TodoTask) => (task.note ? ` — ${task.note}` : "");

/** Done is done; dropped tasks count in the total but neither as done nor as open. */
export function todoSummary(state: TodoState): TodoSummary {
	const done = countOf(state, "done");
	const total = state.tasks.length;
	return { done, total, open: total - done - countOf(state, "dropped") };
}

/** Turns since the last update, while some task can be worked on; tasks that only wait never go stale. */
export function staleTurns(state: TodoState, currentTurn: number): number {
	if (state.updatedTurn === null) return 0;
	if (!state.tasks.some((task) => task.status === "pending" || task.status === "in_progress")) return 0;
	return Math.max(0, currentTurn - state.updatedTurn);
}

function progressLine(state: TodoState): string {
	const { done, total } = todoSummary(state);
	const parts = [`${total} ${plural(total)}`, `${done} done`, `${countOf(state, "in_progress")} in progress`];
	const blocked = countOf(state, "blocked");
	const dropped = countOf(state, "dropped");
	if (blocked) parts.push(`${blocked} blocked`);
	if (dropped) parts.push(`${dropped} dropped`);
	return parts.join(" · ");
}

/** A task from what the model gave, or the reason it is refused. */
function taskFrom(input: TodoTaskInput, id: number): TodoTask | string {
	const title = oneLine(input.title);
	if (!title) return ERRORS.noTitle;
	const status = statusOf(input.status);
	if (!status) return ERRORS.status(input.status);
	const note = oneLine(input.note);
	if (status === "blocked" && !note) return ERRORS.blockedNote;
	const task: TodoTask = { id, title, status };
	if (note) task.note = note;
	return task;
}

/** The list unchanged, with the error. */
const refused = (state: TodoState, error: string): TodoResult => ({ state, text: `Error: ${error}`, error });

function writeAll(state: TodoState, inputs: TodoTaskInput[], turn: number): TodoResult {
	const tasks: TodoTask[] = [];
	let nextId = state.nextId;
	for (const input of inputs) {
		// An id the model passes back is kept, once; a new task gets the next one.
		const given = input.id;
		const reuse = typeof given === "number" && Number.isInteger(given) && given > 0 && tasks.every((task) => task.id !== given);
		const id = reuse ? (given as number) : nextId++;
		if (id + 1 > nextId) nextId = id + 1;
		const task = taskFrom(input, id);
		if (typeof task === "string") return refused(state, task);
		tasks.push(task);
	}
	const written: TodoState = { tasks, nextId, updatedTurn: turn };
	return { state: written, text: `Todo list written: ${progressLine(written)}` };
}

function addOne(state: TodoState, params: TodoParams, turn: number): TodoResult {
	const task = taskFrom({ title: params.title, status: params.status, note: params.note }, state.nextId);
	if (typeof task === "string") return refused(state, task);
	return { state: { tasks: [...state.tasks, task], nextId: state.nextId + 1, updatedTurn: turn }, text: `Added #${task.id} ${task.title} (${task.status})` };
}

function updateOne(state: TodoState, params: TodoParams, turn: number): TodoResult {
	const current = state.tasks.find((task) => task.id === params.id);
	if (!current) return refused(state, ERRORS.noTask(params.id));
	if (params.status === undefined && params.title === undefined && params.note === undefined) return refused(state, ERRORS.nothing);
	const task = taskFrom({ id: current.id, title: params.title ?? current.title, status: params.status ?? current.status, note: params.note ?? current.note }, current.id);
	if (typeof task === "string") return refused(state, task);
	const tasks = state.tasks.map((other) => (other.id === task.id ? task : other));
	const what = current.status === task.status ? "updated" : `→ ${task.status}`;
	return { state: { ...state, tasks, updatedTurn: turn }, text: `#${task.id} ${task.title} ${what}` };
}

function listed(state: TodoState): TodoResult {
	if (state.tasks.length === 0) return { state, text: "No tasks." };
	return { state, text: state.tasks.map((task) => `[${task.status}] #${task.id} ${task.title}${noteSuffix(task)}`).join("\n") };
}

export function applyTodo(state: TodoState, params: TodoParams, turn: number): TodoResult {
	switch (params.action) {
		case "write": return writeAll(state, Array.isArray(params.tasks) ? params.tasks : [], turn);
		case "add": return addOne(state, params, turn);
		case "update": return updateOne(state, params, turn);
		case "clear": {
			const count = state.tasks.length;
			return { state: { ...emptyTodo(), updatedTurn: turn }, text: `Cleared ${count} ${plural(count)}` };
		}
		case "list": return listed(state);
		default: return refused(state, ERRORS.action(params.action));
	}
}

interface BranchEntry {
	type?: string;
	message?: { role?: string; toolName?: string; isError?: boolean; details?: unknown };
}

/** A task of rpiv-todo, the list's shape before Jhon's. */
interface RpivTask {
	id?: number;
	subject?: string;
	status?: string;
	activeForm?: string;
}

function fromRpiv(details: { tasks?: RpivTask[]; nextId?: number }): TodoState {
	const tasks: TodoTask[] = [];
	for (const old of details.tasks ?? []) {
		if (old.status === "deleted" || typeof old.id !== "number") continue;
		const task = taskFrom({ id: old.id, title: old.subject, status: old.status, note: old.status === "in_progress" ? old.activeForm : undefined }, old.id);
		if (typeof task !== "string") tasks.push(task);
	}
	const nextId = typeof details.nextId === "number" ? details.nextId : Math.max(0, ...tasks.map((task) => task.id)) + 1;
	return { tasks, nextId, updatedTurn: null };
}

/**
 * The list as the branch's last successful todo result left it: Jhon's
 * results keep a copy under `jhonTodo` (`gentleTodo` before T59); rpiv-todo's
 * kept `tasks` and `nextId` at the top of the details.
 */
export function replayTodo(entries: readonly unknown[]): TodoState {
	let state = emptyTodo();
	for (const entry of entries as BranchEntry[]) {
		const message = entry?.message;
		if (entry?.type !== "message" || message?.role !== "toolResult" || message.toolName !== TODO_TOOL_NAME || message.isError === true) continue;
		const details = message.details as Record<string, unknown> | undefined;
		if (!details || typeof details !== "object") continue;
		const copy = todoDetails(details)?.state;
		if (copy && Array.isArray(copy.tasks)) state = { tasks: copy.tasks, nextId: copy.nextId, updatedTurn: copy.updatedTurn ?? null };
		else if (Array.isArray(details.tasks)) state = fromRpiv(details as { tasks?: RpivTask[]; nextId?: number });
	}
	return state;
}

const LIST_HEADING = "## Todo list";

/** The open list as the model reads it at a run's start, with its rules, and the stale line once it is stale. */
export function todoPromptBlock(state: TodoState, stale: number): string | undefined {
	if (todoSummary(state).open === 0) return undefined;
	const rows = state.tasks.map((task, index) => `${index + 1}. [${task.status}] ${task.title}${noteSuffix(task)}`);
	const block = [
		LIST_HEADING,
		"Keep it current with the `todo` tool: mark a task in_progress before starting it, done right after finishing it, and rewrite the whole list with `write` whenever the plan changes. Update it before you end the turn.",
		"A blocked task waits on something outside the list: do not work on it until its condition holds. When the plan changes, mark the tasks it made obsolete dropped instead of leaving them pending or marking them done.",
		...rows,
	].join("\n");
	return stale >= STALE_AFTER_TURNS ? `${block}\n(stale: ${stale} turns without an update — bring the list up to date now)` : block;
}

/**
 * S34: with nothing open, the rule for when to start a list, so the model
 * starts one unasked. It goes with every run, so it stays short. It holds
 * inside an LDD change too: the list is the agent's own next steps, never the
 * change's tasks (T134, L1019).
 */
export function todoStartBlock(state: TodoState): string | undefined {
	if (todoSummary(state).open > 0) return undefined;
	return [
		LIST_HEADING,
		"Inside an LDD change or not, the todo list holds your own next steps for this request; the change's tasks stay in `jhon status` and tasks.md, never copied here. For work of three or more steps, write it with `todo write` before the first edit and keep it current: one task in_progress, mark it done right after it is verified. A question, a single read or a one-line fix needs no list; a small change (`jhon status` says so) keeps no tasks.md, archive, light verify or todo list.",
	].join("\n");
}
