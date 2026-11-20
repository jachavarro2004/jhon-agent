/**
 * shell/lib/ui/todo-section.ts — the agent's todo list on screen (T174-S5b,
 * L1493-L1495): the right pane's TODOS section and, where there is no pane
 * (a regular terminal, the narrow form), the Todos block above the prompt,
 * drawn as the Running block is (04).
 *
 * Either form names its fold key (shell.keys.todos) on its title row with
 * `done/planned`, and `stale · N turns` once the agent let open work sit for
 * STALE_AFTER_TURNS turns. Folded, it is the title and the task at hand.
 * Open, the done and dropped tasks fold into one `✓ N done` row, then a row
 * per open task; the task at hand (in progress or blocked) wraps, so nothing
 * of it is lost to a clip. It keeps to todoHeight of the terminal: past that
 * its rows scroll (alt+PgUp/PgDn, TODO_SCROLL_KEYS, or the wheel over the
 * pane's section) and one row says where it is,
 * `↑↓ 4–11/12 · alt+PgUp/PgDn`. Nothing without tasks.
 *
 * Pure: the list, the view's state and a width in, rows of at most that
 * width out, with the scroll position clamped to what can be shown.
 */
import { visibleWidth, wrapTextWithAnsi } from "@earendil-works/pi-tui";
import { STALE_AFTER_TURNS, TODO_STATUS, todoSummary, type TodoState, type TodoTask, type TodoStatus } from "../todo.ts";
import { asciiText, clip, fit, glyph, renderRule, STATES, THEME_NAME, themePainter, type ThemeLike } from "./primitives/index.ts";
import { renderPanelHeader, renderPanelRow, type PanelTheme } from "./shell-panel.ts";

/** The keys that scroll the list, and how its position row names them. pi binds neither (docs/keybindings.md). */
export const TODO_SCROLL_KEYS = Object.freeze({ up: "alt+pageUp", down: "alt+pageDown", hint: "alt+PgUp/PgDn" } as const);
/** Rows one scroll key moves. */
export const TODO_SCROLL_ROWS = 3;
/** The tallest the list draws: a third of the terminal, at most 16 rows. */
export function todoHeight(terminalRows: number): number {
	return Math.max(3, Math.min(16, Math.floor(terminalRows / 3)));
}

/** What the owner did to the list: folded it, scrolled it. */
export interface TodoView {
	collapsed: boolean;
	/** The first task row shown. */
	top: number;
	/** Turns since the agent last wrote open work (lib/todo.ts staleTurns). */
	staleTurns: number;
	/** The fold key, named on the title; none when off. */
	key?: string;
	/** Every row the form may take, its title and closing rows included. */
	height: number;
	ascii?: boolean;
	/** The pointer is over the pane's title row, the fold control. */
	hover?: boolean;
}

/** The rows drawn, the scroll position they were drawn at, and the furthest it goes (0 when every row shows). */
export interface TodoRows { rows: string[]; top: number; maxTop?: number }

// The shell's state glyphs (primitives/states.ts): blocked needs you, so it is
// ◉ in the bad role; dropped is a dim ✕.
const MARK: Record<TodoStatus, string> = { [TODO_STATUS.PENDING]: STATES.pending.glyph, [TODO_STATUS.IN_PROGRESS]: STATES.working.glyph, [TODO_STATUS.BLOCKED]: STATES.blocked.glyph, [TODO_STATUS.DONE]: STATES.done.glyph, [TODO_STATUS.DROPPED]: "✕" };
const MARK_ROLE: Record<TodoStatus, string> = { [TODO_STATUS.PENDING]: THEME_NAME[STATES.pending.role], [TODO_STATUS.IN_PROGRESS]: THEME_NAME[STATES.working.role], [TODO_STATUS.BLOCKED]: THEME_NAME[STATES.blocked.role], [TODO_STATUS.DONE]: THEME_NAME[STATES.done.role], [TODO_STATUS.DROPPED]: "muted" };

/** One task: its state glyph, its title and, in progress or blocked, its note. */
function taskText(task: TodoTask, theme: PanelTheme): string {
	const closed = task.status === TODO_STATUS.DONE || task.status === TODO_STATUS.DROPPED;
	const note = task.note && (task.status === TODO_STATUS.IN_PROGRESS || task.status === TODO_STATUS.BLOCKED) ? theme.fg("muted", ` · ${task.note}`) : "";
	return `${theme.fg(MARK_ROLE[task.status], MARK[task.status])} ${theme.fg(closed ? "dim" : "text", task.title)}${note}`;
}

/**
 * The list's lines, at most cells wide, unindented: the done and dropped
 * tasks as one row (`✓ 3 done · 1 dropped`), then each open task; the one in
 * progress or blocked wraps instead of being cut.
 */
function taskLines(state: TodoState, theme: PanelTheme, cells: number): string[] {
	const lines: string[] = [];
	const count = (status: TodoStatus) => state.tasks.filter((task) => task.status === status).length;
	const done = count(TODO_STATUS.DONE);
	const dropped = count(TODO_STATUS.DROPPED);
	if (done || dropped) {
		const lead = done ? TODO_STATUS.DONE : TODO_STATUS.DROPPED;
		const words = [done ? `${done} done` : "", dropped ? `${dropped} dropped` : ""].filter(Boolean).join(" · ");
		lines.push(`${theme.fg(MARK_ROLE[lead], MARK[lead])} ${theme.fg("muted", words)}`);
	}
	for (const task of state.tasks) {
		if (task.status === TODO_STATUS.DONE || task.status === TODO_STATUS.DROPPED) continue;
		const one = taskText(task, theme);
		const focus = task.status === TODO_STATUS.IN_PROGRESS || task.status === TODO_STATUS.BLOCKED;
		if (!focus || visibleWidth(one) <= cells) { lines.push(one); continue; }
		const text = `${task.title}${task.note ? ` · ${task.note}` : ""}`;
		wrapTextWithAnsi(text, Math.max(1, cells - 2)).forEach((part, i) => lines.push(`${i ? " " : theme.fg(MARK_ROLE[task.status], MARK[task.status])} ${theme.fg("text", part)}`));
	}
	return lines;
}

/** Folded, the task at hand: in progress, else the next pending, else the blocked one, else `✓ N done`. */
function currentText(state: TodoState, theme: PanelTheme): string {
	const current = [TODO_STATUS.IN_PROGRESS, TODO_STATUS.PENDING, TODO_STATUS.BLOCKED].map((status) => state.tasks.find((task) => task.status === status)).find(Boolean);
	if (current) return taskText(current, theme);
	return `${theme.fg(MARK_ROLE[TODO_STATUS.DONE], MARK[TODO_STATUS.DONE])} ${theme.fg("muted", `${todoSummary(state).done} done`)}`;
}

/** `alt+t`, `stale · 2 turns`, `1/3`: what the title says beside the name. */
function titleFacts(state: TodoState, view: TodoView): { key?: string; stale?: string; count: string } {
	const { done, total } = todoSummary(state);
	const planned = total - state.tasks.filter((task) => task.status === TODO_STATUS.DROPPED).length;
	return {
		...(view.key ? { key: view.key } : {}),
		...(view.staleTurns >= STALE_AFTER_TURNS ? { stale: `stale · ${view.staleTurns} turns` } : {}),
		count: `${done}/${planned}`,
	};
}

/** The task rows a window of `room` rows shows from `top`, and the position row when they do not all fit. */
function window(lines: string[], top: number, room: number, ascii: boolean): { shown: string[]; position?: string; top: number; maxTop: number } {
	if (lines.length <= room) return { shown: lines, top: 0, maxTop: 0 };
	const rows = Math.max(1, room - 1);
	const maxTop = lines.length - rows;
	const at = Math.max(0, Math.min(top, maxTop));
	const shown = lines.slice(at, at + rows);
	const arrows = ascii ? "^v" : "↑↓";
	const dash = ascii ? "-" : "–";
	return { shown, top: at, maxTop, position: `${arrows} ${at + 1}${dash}${at + shown.length}/${lines.length}${ascii ? " - " : " · "}${TODO_SCROLL_KEYS.hint}` };
}

/**
 * The right pane's TODOS: `▾ TODOS … alt+t  stale · 2 turns · 0/2`, then a
 * row per task under it (the key dim, the stale words in the warning role).
 */
export function renderTodoPane(state: TodoState, theme: PanelTheme, width: number, view: TodoView): TodoRows {
	if (!state.tasks.length) return { rows: [], top: view.top };
	const ascii = view.ascii ?? false;
	const facts = titleFacts(state, view);
	const key = facts.key ? `${theme.fg("dim", facts.key)}  ` : "";
	const count = `${key}${facts.stale ? `${theme.fg("warning", facts.stale)}${theme.fg("muted", " · ")}` : ""}${facts.count}`;
	const header = renderPanelHeader(theme, width, { title: "Todos", count, open: !view.collapsed, hovered: view.hover ?? false, ascii });
	const shownRows = (rows: string[]) => (ascii ? rows.map(asciiText) : rows);
	if (view.collapsed) return { rows: shownRows([header, renderPanelRow(width, currentText(state, theme))]), top: view.top, maxTop: 0 };
	const lines = taskLines(state, theme, Math.max(1, width - 2)).map((line) => renderPanelRow(width, line));
	const shown = window(lines, view.top, Math.max(1, view.height - 1), ascii);
	const position = shown.position ? [renderPanelRow(width, theme.fg("muted", shown.position))] : [];
	return { rows: shownRows([header, ...shown.shown, ...position]), top: shown.top, maxTop: shown.maxTop };
}

/** The widest the block's rows run, as the Running block's (04: 64 columns, two in from the edge). */
const BLOCK_ROWS_MAX = 64;

/**
 * The Todos block above the prompt: `── ▾ Todos  alt+t · 0/2 ───`, a row per
 * task two columns in, then a blank row; every row exactly width cells.
 */
export function renderTodoBlock(state: TodoState, theme: ThemeLike & PanelTheme, width: number, view: TodoView): TodoRows {
	if (!state.tasks.length) return { rows: [], top: view.top };
	const ascii = view.ascii ?? false;
	const total = Math.max(0, Math.floor(width));
	const inner = Math.max(1, Math.min(BLOCK_ROWS_MAX, total - 4));
	const facts = titleFacts(state, view);
	const dot = ascii ? " - " : " · ";
	const note = [facts.key, facts.stale, facts.count].filter(Boolean).join(dot);
	const label = `${glyph(view.collapsed ? "closed" : "open", ascii)} Todos`;
	const rule = renderRule({ width: total, label, note: ascii ? asciiText(note) : note, paint: themePainter(theme), ascii });
	const indent = (line: string) => fit(`  ${clip(ascii ? asciiText(line) : line, inner, { ascii })}`, total, { ascii });
	const blank = " ".repeat(total);
	if (view.collapsed) return { rows: [rule, indent(currentText(state, theme)), blank], top: view.top };
	const lines = taskLines(state, theme, inner);
	const shown = window(lines, view.top, Math.max(1, view.height - 2), ascii);
	const position = shown.position ? [indent(theme.fg("muted", shown.position))] : [];
	return { rows: [rule, ...shown.shown.map(indent), ...position, blank], top: shown.top, maxTop: shown.maxTop };
}

/** The rows of the list with no view state: open, no key, never stale, every task (the ASCII check's). */
export function plainTodoRows(state: TodoState, theme: PanelTheme, width: number, ascii = false): string[] {
	return renderTodoPane(state, theme, width, { collapsed: false, top: 0, staleTurns: 0, height: Number.MAX_SAFE_INTEGER, ascii }).rows;
}
