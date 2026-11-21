/**
 * shell/lib/ui/tasks-view.ts — the Agents view (`/jhon:agents`, alt+s) and the
 * Jobs view (`/jhon:jobs`, alt+o), separate since T92 (L657), drawn as
 * design/shell-tui 09 (T174-S10): a rounded card titled `Agents  this session ·
 * N running · M finished` with `esc` at its right; on the left the Running
 * section (a row per task, its time at the right, and a second line saying
 * what it does) and the Finished section (one row each); on the right the
 * chosen task's thread: its header (name, model, session, tokens and cost;
 * its state and time at the right), then an agent's task, one line per tool
 * (icon, verb, target, its state at the right), what it said and its answer,
 * or a command's line, output file and tail. The key hints close the card.
 *
 * Keys: j/k or the arrows choose a row (scroll the thread when it is alone),
 * enter or f follow the thread's end, ctrl+j/k and PgUp/PgDn scroll it, s (c,
 * x) stops the chosen task while it runs, a shows every session's agents and
 * back, o opens an agent's saved session, F shows the thread alone (tab or
 * enter on a narrow card, where the list is alone), esc goes back or closes,
 * q closes. The mouse: a click on a row chooses it, on a hint runs it, on esc
 * goes back or closes; a move paints the hover; the wheel scrolls the thread.
 */
import { Key, matchesKey, visibleWidth, wrapTextWithAnsi, type TuiMouseEvent, type TuiMouseEventResult } from "@earendil-works/pi-tui";
import { agentDoes, type AgentRun, type JobRecord, type TaskRecord, type ThreadItem } from "../background-tasks.ts";
import { taskOutcome, taskSucceeded } from "../background-tasks-tools.ts";
import { formatCost, formatTokens } from "./bars.ts";
import { sanitizeTerminalText } from "./terminal-theme.ts";
import { toolFamily } from "./tool-icons.ts";
import { HOVER_ROLE, STATES, THEME_NAME, asciiText, clip, fitHints, glyph, toolGlyph, type IconStyle, type Role } from "./primitives/index.ts";

export type Scope = "session" | "all";

export interface TasksViewTheme {
	fg(role: string, text: string): string;
	bold?(text: string): string;
	/** The chosen row's tint; without it the row is marked by `▸` alone. */
	bg?(role: string, text: string): string;
}

export interface TasksViewDeps {
	theme: TasksViewTheme;
	/** The terminal's rows: the card's height. */
	rows: () => number;
	/** The sub-agents, or the commands and monitors (the default). */
	view?: "agents" | "jobs";
	/** The owner's session, which the scope and an agent's header name. */
	sessionId?: string;
	/** This session's tasks, or every session's (the Agents view's `a`); the view orders a copy. */
	tasks: (scope?: Scope) => TaskRecord[];
	now: () => number;
	onStop(task: TaskRecord): void;
	/** Opens a sub-agent's saved session (o). */
	onOpen?(task: AgentRun): void;
	onClose(): void;
	requestRender(): void;
	/** shell.appearance.ascii: the frame, glyphs, icons and text in ASCII. */
	ascii?: boolean;
	/** shell.appearance.icons for the tool rows. */
	icons?: IconStyle;
}

const CLOCK_MS = 1000;
const EMPTY_LIST = "nothing started yet";
const EMPTY_THREAD = "Pick a row to read its thread";
const NO_OUTPUT = "no output yet";
const SCOPE_LABEL: Record<Scope, string> = { session: "this session", all: "all sessions" };
const CLOSE_ROW = "Close tasks";
/** The list's cells beside the thread (09); narrower cards shrink it to LIST_MIN_CELLS. */
const LIST_CELLS = 34;
const LIST_MIN_CELLS = 20;
/** The thread's cells the list leaves at least, and below which the list stands alone. */
const THREAD_ROOM = 40;
const THREAD_MIN_CELLS = 30;
/** Below these the card is one close row. */
const MIN_WIDTH = 24;
const MIN_ROWS = 5;
/** From these rows the card keeps 09's blank rows under the title and above the hints. */
const ROOMY_ROWS = 10;
/** The verb column of a tool row: `read  `, `edit  `. */
const VERB_CELLS = 5;
const OUTPUT_INDENT = 4;
const WHEEL_ROWS = 3;

/** 09's times: `18s`, `4m12s`, `1h05m`. */
export function formatViewTime(ms: number): string {
	const seconds = Math.max(0, Math.round(ms / 1000));
	const two = (n: number) => String(n).padStart(2, "0");
	if (seconds < 60) return `${seconds}s`;
	const minutes = Math.floor(seconds / 60);
	if (minutes < 60) return `${minutes}m${two(seconds % 60)}s`;
	return `${Math.floor(minutes / 60)}h${two(minutes % 60)}m`;
}

interface Geometry {
	width: number;
	height: number;
	/** The cells between `│ ` and ` │`. */
	inner: number;
	roomy: boolean;
	bodyTop: number;
	bodyRows: number;
	/** The list beside the thread; else one of them alone. */
	split: boolean;
	listCells: number;
	threadCells: number;
}

function measure(width: number, height: number): Geometry | undefined {
	if (width < MIN_WIDTH || height < MIN_ROWS) return undefined;
	const inner = width - 4;
	const roomy = height >= ROOMY_ROWS;
	const list = Math.min(LIST_CELLS, Math.max(LIST_MIN_CELLS, inner - 3 - THREAD_ROOM));
	const split = inner - list - 3 >= THREAD_MIN_CELLS;
	return { width, height, inner, roomy, bodyTop: roomy ? 3 : 2, bodyRows: height - (roomy ? 6 : 4), split, listCells: split ? list : inner, threadCells: split ? inner - list - 3 : inner };
}

type Paint = (text: string) => string;
/** A run of text and how it is painted. */
type Seg = readonly [string, Paint];

const cellsOf = (segs: readonly Seg[]) => segs.reduce((sum, [text]) => sum + visibleWidth(text), 0);

/** `left`, spaces, then `right` ending at cells: exactly cells wide. The left is cut with the ellipsis; the right drops when there is no room beside it. */
function compose(left: readonly Seg[], right: readonly Seg[], cells: number, ascii: boolean): string {
	if (cells <= 0) return "";
	const rightCells = cellsOf(right);
	const keepRight = rightCells > 0 && rightCells + 2 <= cells;
	const room = keepRight ? cells - rightCells - 1 : cells;
	let used = 0;
	let out = "";
	const cut = cellsOf(left) > room;
	for (const [text, paint] of left) {
		const width = visibleWidth(text);
		if (!cut || used + width < room) {
			out += paint(text);
			used += width;
			continue;
		}
		// The run that crosses the edge ends in the ellipsis, even when it would just fit.
		const shown = clip(width === room - used ? `${text} ` : text, room - used, { ascii });
		out += paint(shown);
		used += visibleWidth(shown);
		break;
	}
	return `${out}${" ".repeat(Math.max(0, cells - used - (keepRight ? rightCells : 0)))}${keepRight ? right.map(([text, paint]) => paint(text)).join("") : ""}`;
}

/** Words laid out at width, the first line at first; a word wider than a line is broken. */
function wrapWords(text: string, first: number, rest: number): string[] {
	const lines: string[] = [];
	let line = "";
	let width = Math.max(1, first);
	for (const word of text.split(" ")) {
		let piece = word;
		const joined = line ? `${line} ${piece}` : piece;
		if (visibleWidth(joined) <= width) { line = joined; continue; }
		if (line) { lines.push(line); line = ""; width = Math.max(1, rest); }
		while (visibleWidth(piece) > width) {
			const head = clip(piece, width, { ellipsis: "" });
			lines.push(head);
			piece = piece.slice(head.length);
			width = Math.max(1, rest);
		}
		line = piece;
	}
	lines.push(line);
	return lines;
}

/** Header parts joined by ` · `: as many as fit beside the right on the first line, the rest on more lines; a part wider than a line wraps by words. */
function layoutParts(parts: readonly string[], first: number, rest: number): { lines: string[]; nameLines: number } {
	const lines: string[] = [];
	let line = "";
	let width = first;
	let nameLines = 1;
	parts.forEach((part, index) => {
		const joined = line ? `${line} · ${part}` : part;
		if (visibleWidth(joined) <= width) { line = joined; return; }
		if (line) { lines.push(line); line = ""; width = rest; }
		if (visibleWidth(part) <= width) { line = part; return; }
		const wrapped = wrapWords(part, width, rest);
		lines.push(...wrapped.slice(0, -1));
		line = wrapped.at(-1) ?? "";
		width = rest;
		if (index === 0) nameLines = wrapped.length;
	});
	if (line) lines.push(line);
	return { lines, nameLines };
}

const clean = (text: string) => sanitizeTerminalText(text).replace(/\s+/g, " ").trim();
const running = (task: TaskRecord) => task.status === "running";
const canOpen = (task: TaskRecord | undefined): task is AgentRun => task?.kind === "agent" && Boolean(task.sessionDir && task.childSessionId);

interface Hint { key: string; action: string; priority?: number; run?: () => void }
interface Target { id: string; run?: () => void; select?: string }
interface Pointer { width: number; height: number; targets: Array<{ x: number; y: number; cells: number; target: Target }> }

export class TasksView {
	private readonly deps: TasksViewDeps;
	private pickedId: string | undefined;
	/** The thread alone: F on a split card, tab or enter on a narrow one. */
	private details = false;
	private listTop = 0;
	private scroll = 0;
	private follow = true;
	private scope: Scope = "session";
	private hovered: string | undefined;
	private closed = false;
	private pointer: Pointer | undefined;
	/** Keys carry no width: the last drawn card says split or narrow. */
	private lastSplit = true;
	/** The thread's rows below its header at the last draw: a page. */
	private page = 10;
	private readonly timer: ReturnType<typeof setInterval>;

	constructor(deps: TasksViewDeps) {
		this.deps = deps;
		// Times, tails and threads move while a task runs.
		this.timer = setInterval(() => {
			if (this.deps.tasks(this.scope).some(running)) this.deps.requestRender();
		}, CLOCK_MS);
		this.timer.unref?.();
	}

	dispose(): void {
		this.closed = true;
		clearInterval(this.timer);
	}

	invalidate(): void {
		this.pointer = undefined;
	}

	handleInput(data: string): void {
		if (this.closed) return;
		const tasks = this.ordered();
		const chosen = this.chosen(tasks);
		const index = Math.max(0, tasks.findIndex((task) => task.id === chosen?.id));
		const down = data === "j" || matchesKey(data, Key.down);
		const up = data === "k" || matchesKey(data, Key.up);
		if (data === "q") return this.deps.onClose();
		if (matchesKey(data, Key.escape)) return this.back();
		// ctrl+j is \n, which pi-tui also reads as enter: it is asked first.
		if (matchesKey(data, Key.pageDown) || matchesKey(data, Key.ctrl("j"))) this.scrollBy(this.page);
		else if (matchesKey(data, Key.pageUp) || matchesKey(data, Key.ctrl("k"))) this.scrollBy(-this.page);
		else if (data === "F" && this.lastSplit) this.toggleDetails();
		else if ((matchesKey(data, Key.tab) || matchesKey(data, Key.enter)) && !this.lastSplit) this.toggleDetails();
		else if (matchesKey(data, Key.enter)) this.follow = true;
		else if (this.details && (down || up)) this.scrollBy(down ? 1 : -1);
		else if (down) this.choose(tasks[Math.min(tasks.length - 1, index + 1)]);
		else if (up) this.choose(tasks[Math.max(0, index - 1)]);
		else if (data === "f" && chosen) this.follow = true;
		else if (data === "a" && this.agents()) this.toggleScope();
		else if (data === "o" && this.agents()) this.open(chosen);
		else if (data === "s" || data === "c" || data === "x") this.stop(chosen);
		else return;
		this.repaint();
	}

	handleMouse(event: TuiMouseEvent): TuiMouseEventResult | undefined {
		if (this.closed) return undefined;
		const pointer = this.pointer;
		if (!pointer || event.width !== pointer.width || event.height !== pointer.height) return undefined;
		const hit = pointer.targets.find(({ x, y, cells }) => event.y === y && event.x >= x && event.x < x + cells)?.target;
		if (event.type === "move") {
			const changed = hit?.id !== this.hovered;
			this.hovered = hit?.id;
			return { handled: true, render: changed };
		}
		if (event.type === "wheel" && event.wheelDelta) {
			this.scrollBy(Math.sign(event.wheelDelta) * WHEEL_ROWS);
			this.repaint();
			return { handled: true, render: true };
		}
		if (event.button !== "left" || (event.type !== "click" && event.type !== "press")) return undefined;
		if (!hit || (!hit.run && !hit.select)) return undefined;
		// pi-tui sends the click only to what took the press.
		if (event.type === "press") return { handled: true, render: false };
		if (hit.select) this.choose(this.ordered().find((task) => task.id === hit.select));
		else hit.run?.();
		this.repaint();
		return { handled: true, render: true };
	}

	render(width: number): string[] {
		const geometry = measure(Math.max(0, Math.floor(width)), Math.max(0, Math.floor(this.deps.rows())));
		const ascii = this.deps.ascii ?? false;
		if (!geometry) {
			this.pointer = undefined;
			return width > 0 ? [this.fold(compose([[`${ascii ? "x" : "×"} ${CLOSE_ROW}`, (t) => t]], [], Math.floor(width), ascii))] : [];
		}
		const { inner, bodyRows, bodyTop, split } = geometry;
		this.lastSplit = split;
		const tasks = this.ordered();
		const chosen = this.chosen(tasks);
		if (!chosen) this.details = false;
		const targets: Pointer["targets"] = [];
		const frame = (text: string) => this.fg("frameSoft", text);
		const side = frame(glyph("side", ascii));
		const row = (content: string) => `${side} ${content} ${side}`;
		const blank = row(" ".repeat(inner));

		// Title: `Agents  this session · 2 running · 1 finished`, esc at the right.
		const runningCount = tasks.filter(running).length;
		const name = this.agents() ? "Agents" : "Jobs";
		const summary = [...(this.agents() ? [SCOPE_LABEL[this.scope]] : []), `${runningCount} running`, `${tasks.length - runningCount} finished`].join(" · ");
		const escTarget: Target = { id: "esc", run: () => this.back() };
		targets.push({ x: 2 + inner - 3, y: 1, cells: 3, target: escTarget });
		const title = compose([[name, (t) => this.bold(this.fg("accent", t))], [`  ${summary}`, (t) => this.fg("dim", t)]], [["esc", (t) => this.paintHover(t, "esc", "accent")]], inner, ascii);

		// Body: the list beside the thread, or one of them alone.
		const showList = split || !this.details;
		const showThread = split || this.details;
		const listCells = split ? geometry.listCells : inner;
		const threadCells = split && !this.details ? geometry.threadCells : inner;
		const list = showList && !(split && this.details) ? this.listRows(tasks, chosen, listCells, bodyRows, bodyTop, targets) : [];
		const thread = showThread ? this.threadRows(chosen, threadCells, bodyRows) : [];
		const body: string[] = [];
		for (let i = 0; i < bodyRows; i += 1) {
			const left = list[i] ?? " ".repeat(listCells);
			const right = thread[i] ?? " ".repeat(threadCells);
			if (split && !this.details) body.push(row(`${left} ${frame(glyph("side", ascii))} ${right}`));
			else body.push(row(this.details ? right : left));
		}

		const footer = row(this.footer(tasks, chosen, geometry, targets));
		this.pointer = { width: geometry.width, height: geometry.height, targets };
		const rule = glyph("rule", ascii).repeat(geometry.width - 2);
		const lines = [
			frame(`${glyph("topLeft", ascii)}${rule}${glyph("topRight", ascii)}`),
			row(title),
			...(geometry.roomy ? [blank] : []),
			...body,
			...(geometry.roomy ? [blank] : []),
			footer,
			frame(`${glyph("bottomLeft", ascii)}${rule}${glyph("bottomRight", ascii)}`),
		];
		return lines.map((line) => this.fold(line));
	}

	// --- state ------------------------------------------------------------------------

	private agents(): boolean {
		return this.deps.view === "agents";
	}

	/** The view's tasks in 09's order: Running, the longest running first; then Finished, the latest to end first. */
	private ordered(): TaskRecord[] {
		const mine = this.deps.tasks(this.scope).filter((task) => (task.kind === "agent") === this.agents());
		const live = mine.filter(running).sort((a, b) => a.startedAt - b.startedAt);
		const done = mine.filter((task) => !running(task)).sort((a, b) => (b.endedAt ?? b.startedAt) - (a.endedAt ?? a.startedAt));
		return [...live, ...done];
	}

	private chosen(tasks: TaskRecord[]): TaskRecord | undefined {
		const task = tasks.find((item) => item.id === this.pickedId) ?? tasks[0];
		this.pickedId = task?.id;
		return task;
	}

	private choose(task: TaskRecord | undefined): void {
		if (!task || task.id === this.pickedId) return;
		this.pickedId = task.id;
		this.scroll = 0;
		this.follow = true;
	}

	private back(): void {
		if (this.details) {
			this.details = false;
			this.repaint();
		} else this.deps.onClose();
	}

	private toggleDetails(): void {
		if (this.details) this.details = false;
		else if (this.chosen(this.ordered())) this.details = true;
	}

	private toggleScope(): void {
		this.scope = this.scope === "session" ? "all" : "session";
		this.pickedId = undefined;
		this.listTop = 0;
	}

	private open(task: TaskRecord | undefined): void {
		if (canOpen(task)) this.deps.onOpen?.(task);
	}

	private stop(task: TaskRecord | undefined): void {
		if (task && running(task)) this.deps.onStop(task);
	}

	private scrollBy(delta: number): void {
		this.follow = false;
		this.scroll = Math.max(0, this.scroll + delta);
	}

	private repaint(): void {
		this.pointer = undefined;
		this.deps.requestRender();
	}

	// --- painting ---------------------------------------------------------------------

	private fg(role: Role, text: string): string {
		return text ? this.deps.theme.fg(THEME_NAME[role], text) : text;
	}

	private bold(text: string): string {
		return text && this.deps.theme.bold ? this.deps.theme.bold(text) : text;
	}

	private sel(text: string): string {
		return this.deps.theme.bg ? this.deps.theme.bg(THEME_NAME.sel, text) : text;
	}

	/** Text in the hover role while the pointer is over `id`, else in role. */
	private paintHover(text: string, id: string, role: Role): string {
		return this.hovered === id ? this.deps.theme.fg(HOVER_ROLE, text) : this.fg(role, text);
	}

	private fold(line: string): string {
		return this.deps.ascii ? asciiText(line) : line;
	}

	private time(task: TaskRecord): string {
		return formatViewTime((task.endedAt ?? this.deps.now()) - task.startedAt);
	}

	/** The state's glyph and role: ◐ (⧗ for a command) while running, ✓, ✗, – stopped. */
	private mark(task: TaskRecord): [string, Role] {
		const ascii = this.deps.ascii ?? false;
		if (running(task)) return task.kind === "agent" ? [ascii ? STATES.working.ascii : STATES.working.glyph, STATES.working.role] : [glyph("job", ascii), "warn"];
		if (task.status === "stopped") return [ascii ? "-" : "–", "dim"];
		return taskSucceeded(task) ? [ascii ? STATES.done.ascii : STATES.done.glyph, STATES.done.role] : [ascii ? STATES.failed.ascii : STATES.failed.glyph, STATES.failed.role];
	}

	/** The word beside the time in the header: an agent's outcome; a command's `exit N`. */
	private word(task: TaskRecord): string {
		if (task.kind === "agent" || task.status === "running" || task.status === "stopped") return taskOutcome(task);
		if (task.status === "failed" || task.executions?.failed) return "failed";
		return task.exitCode === null || task.exitCode === undefined ? "exited" : `exit ${task.exitCode}`;
	}

	// --- the list ---------------------------------------------------------------------

	/** The Running and Finished sections, windowed so the chosen row shows; exactly cells wide. */
	private listRows(tasks: TaskRecord[], chosen: TaskRecord | undefined, cells: number, rows: number, top: number, targets: Pointer["targets"]): string[] {
		const ascii = this.deps.ascii ?? false;
		if (tasks.length === 0) return [compose([[EMPTY_LIST, (t) => this.fg("dim", t)]], [], cells, ascii)];
		const lines: Array<{ text: string; id?: string }> = [];
		const heading = (label: string, count?: number) => lines.push({ text: compose([[label, (t) => this.bold(this.fg("soft", t))], ...(count === undefined ? [] : [[`  ${count}`, (t: string) => this.fg("dim", t)] as Seg])], [], cells, ascii) });
		const live = tasks.filter(running);
		const done = tasks.filter((task) => !running(task));
		if (live.length) heading("Running");
		for (const task of live) lines.push(...this.taskRows(task, task.id === chosen?.id, cells).map((text) => ({ text, id: task.id })));
		if (done.length) {
			if (lines.length) lines.push({ text: " ".repeat(cells) });
			heading("Finished", done.length);
		}
		for (const task of done) lines.push(...this.taskRows(task, task.id === chosen?.id, cells).map((text) => ({ text, id: task.id })));

		// Keep the chosen rows in the window, with their section's heading when it is just above.
		const first = lines.findIndex((line) => line.id === chosen?.id);
		const last = lines.findLastIndex((line) => line.id === chosen?.id);
		if (first >= 0) {
			const lead = first > 0 && lines[first - 1]!.id === undefined ? first - 1 : first;
			if (lead < this.listTop) this.listTop = lead;
			if (last >= this.listTop + rows) this.listTop = last - rows + 1;
		}
		this.listTop = Math.max(0, Math.min(this.listTop, lines.length - rows));
		const shown = lines.slice(this.listTop, this.listTop + rows);
		shown.forEach((line, i) => { if (line.id) targets.push({ x: 2, y: top + i, cells, target: { id: `row:${line.id}`, select: line.id } }); });
		return shown.map((line) => line.text);
	}

	/** A running task: its row and what it does under it; a finished one: one row. */
	private taskRows(task: TaskRecord, chosen: boolean, cells: number): string[] {
		const ascii = this.deps.ascii ?? false;
		const [mark, role] = this.mark(task);
		const live = running(task);
		// T195: an agent's rows say what it does by its label, never its brief's first line.
		const does = task.kind === "agent" ? clean(agentDoes(task)) : "";
		const name = task.kind === "agent" ? (live || does === "" ? clean(task.agent) : `${clean(task.agent)} · ${does}`) : clean(task.label);
		const nameRole: Role = chosen || live ? "ink" : "soft";
		const paintName: Paint = (t) => (this.hovered === `row:${task.id}` ? this.deps.theme.fg(HOVER_ROLE, t) : chosen ? this.bold(this.fg(nameRole, t)) : this.fg(nameRole, t));
		const cursor = chosen ? this.fg("accent", glyph("cursor", ascii)) : " ";
		const first = compose([[`${chosen ? glyph("cursor", ascii) : " "} `, () => `${cursor} `], [`${mark} `, (t) => this.fg(role, t)], [name, paintName]], [[this.time(task), (t) => this.fg(chosen ? "ink" : live ? "soft" : "dim", t)]], cells, ascii);
		const rows = [first];
		if (live) {
			const what = task.kind === "agent" ? does : [task.id, task.kind === "monitor" ? "monitor" : "", clean(task.command) !== clean(task.label) ? clean(task.command) : ""].filter(Boolean).join(" · ");
			rows.push(compose([["    ", (t) => t], [what, (t) => this.fg(chosen ? "soft" : "dim", t)]], [], cells, ascii));
		}
		return chosen ? rows.map((text) => this.sel(text)) : rows;
	}

	// --- the thread -------------------------------------------------------------------

	/** The chosen task's header, then its content, scrolled to the end while following; exactly cells wide. */
	private threadRows(task: TaskRecord | undefined, cells: number, rows: number): string[] {
		const ascii = this.deps.ascii ?? false;
		if (!task) return [compose([[EMPTY_THREAD, (t) => this.fg("dim", t)]], [], cells, ascii)];
		const header = this.header(task, cells);
		const content = (task.kind === "agent" ? this.agentContent(task, cells) : this.jobContent(task, cells));
		const visible = Math.max(0, rows - header.length);
		const end = Math.max(0, content.length - visible);
		this.page = Math.max(1, visible - 1);
		if (this.follow) this.scroll = end;
		this.scroll = Math.min(this.scroll, end);
		return [...header, ...content.slice(this.scroll, this.scroll + visible)].slice(0, rows);
	}

	/** `name · model · session · tokens · cost` (a job's `label · id · monitor · N events`), its state and time at the right; never cut (L345). */
	private header(task: TaskRecord, cells: number): string[] {
		const ascii = this.deps.ascii ?? false;
		const [mark, role] = this.mark(task);
		const status = `${mark} ${this.word(task)} ${this.time(task)}`;
		const parts = task.kind === "agent"
			? [clean(task.agent), task.model ? clean(task.model) : "", task.ownerSessionId === this.deps.sessionId ? SCOPE_LABEL.session : `session ${task.ownerSessionId.slice(0, 8)}`, task.tokens > 0 ? formatTokens(task.tokens) : "", task.cost > 0 ? formatCost(task.cost) : ""]
			: [clean(task.label), task.id, task.kind === "monitor" ? "monitor" : "", task.kind === "monitor" ? `${task.events ?? 0} event${task.events === 1 ? "" : "s"}` : ""];
		const statusCells = visibleWidth(status);
		const besideStatus = cells - statusCells - 2;
		const { lines, nameLines } = layoutParts(parts.filter(Boolean), besideStatus >= 10 ? besideStatus : cells, cells);
		const name = parts[0]!;
		const strong: Paint = (t) => this.bold(this.fg("accent", t));
		const soft: Paint = (t) => this.fg("soft", t);
		// The name in accent, the rest soft: the name's lines, then the line it ends on up to its first ` · `.
		const paintLine = (line: string, index: number): Seg[] => {
			if (index >= nameLines) return [[line, soft]];
			if (index < nameLines - 1) return [[line, strong]];
			const end = nameLines === 1 && line.startsWith(name) ? name.length : line.indexOf(" · ");
			return end < 0 ? [[line, strong]] : [[line.slice(0, end), strong], [line.slice(end), soft]];
		};
		const out = lines.map((line, index) => compose(paintLine(line, index), index === 0 && besideStatus >= 10 ? [[status, (t) => this.fg(role, t)]] : [], cells, ascii));
		if (besideStatus < 10) out.push(compose([], [[status, (t) => this.fg(role, t)]], cells, ascii));
		return out;
	}

	/** Text lines, each wrapped to cells and painted in role. */
	private paragraph(text: string, cells: number, role: Role, indent = 0): string[] {
		const ascii = this.deps.ascii ?? false;
		const pad = " ".repeat(indent);
		return sanitizeTerminalText(text).split("\n").flatMap((raw) => {
			const line = raw.replace(/\t/g, "  ").trimEnd();
			if (!line) return [" ".repeat(cells)];
			const room = Math.max(1, cells - indent);
			const wrapped = visibleWidth(line) <= room ? [line] : wrapTextWithAnsi(line, room);
			return wrapped.map((part) => compose([[pad, (t) => t], [part, (t) => this.fg(role, t)]], [], cells, ascii));
		});
	}

	/** An agent's task, a blank row, its thread (or its activity), then its answer or why it ended. */
	private agentContent(run: AgentRun, cells: number): string[] {
		const ascii = this.deps.ascii ?? false;
		const blank = " ".repeat(cells);
		const out: string[] = [...this.paragraph(run.task.trim(), cells, "dim"), blank];
		const start = out.length;
		const lastBlank = () => out.at(-1) === blank;
		for (const item of run.thread ?? []) {
			if (item.kind === "tool") {
				out.push(this.toolRow(item, cells));
				if (this.details && item.output) out.push(...this.paragraph(item.output, cells, "soft", OUTPUT_INDENT));
				continue;
			}
			if (!lastBlank()) out.push(blank);
			out.push(...this.paragraph(item.text, cells, item.kind === "thinking" ? "dim" : "ink"), blank);
		}
		if (!(run.thread ?? []).length) for (const line of run.activity) out.push(...this.paragraph(line, cells, "soft"));
		const answer = !running(run) && run.result;
		if (out.length === start && !answer) out.push(compose([[NO_OUTPUT, (t) => this.fg("dim", t)]], [], cells, ascii));
		while (lastBlank() && out.length > 1) out.pop();
		if (answer) out.push(blank, compose([[run.status === "completed" ? "Answer" : "Reason", (t) => this.bold(this.fg("soft", t))]], [], cells, ascii), ...this.paragraph(run.result!, cells, "ink"));
		return out;
	}

	/** 09's tool row: `▤ read  <target>` and its state at the right. */
	private toolRow(item: Extract<ThreadItem, { kind: "tool" }>, cells: number): string {
		const ascii = this.deps.ascii ?? false;
		const icon = toolGlyph(toolFamily(item.name), { ascii, style: this.deps.icons ?? "geometric" });
		const verb = `${clean(item.name).padEnd(VERB_CELLS)} `;
		const [state, role]: [string, Role] = item.isError ? [ascii ? STATES.failed.ascii : STATES.failed.glyph, "bad"] : item.running ? [ascii ? STATES.working.ascii : STATES.working.glyph, "warn"] : [ascii ? STATES.done.ascii : STATES.done.glyph, "ok"];
		return compose([[`${icon} `, (t) => this.fg("accent", t)], [verb, (t) => this.fg("soft", t)], [item.subject ? clean(item.subject) : "", (t) => this.fg("ink", t)]], [[state, (t) => this.fg(role, t)]], cells, ascii);
	}

	/** A command's line, its output file, why it failed when that is long, a blank row, then its tail. */
	private jobContent(job: JobRecord, cells: number): string[] {
		const ascii = this.deps.ascii ?? false;
		const out = [...this.paragraph(`$ ${clean(job.command)}`, cells, "ink"), ...this.paragraph(job.outputPath, cells, "dim")];
		if (job.status === "failed" || job.executions?.failed) out.push(...this.paragraph(taskOutcome(job), cells, "dim"));
		out.push(" ".repeat(cells));
		if (job.tail.length) for (const line of job.tail) out.push(...this.paragraph(line, cells, "ink"));
		else out.push(compose([[NO_OUTPUT, (t) => this.fg("dim", t)]], [], cells, ascii));
		return out;
	}

	// --- the hints --------------------------------------------------------------------

	/** The key hints that fit, each a pointer target that runs it. */
	private footer(tasks: TaskRecord[], chosen: TaskRecord | undefined, geometry: Geometry, targets: Pointer["targets"]): string {
		const live = chosen !== undefined && running(chosen);
		const noun = this.agents() ? "agent" : "job";
		const scope: Hint = { key: "a", action: this.scope === "session" ? SCOPE_LABEL.all : SCOPE_LABEL.session, priority: 5, run: () => this.toggleScope() };
		const stop: Hint = { key: "x", action: "stop", priority: 7, run: () => this.stop(chosen) };
		const open: Hint = { key: "o", action: "open session", priority: 6, run: () => this.open(chosen) };
		const follow = () => { this.follow = true; };
		const details = () => this.toggleDetails();
		const hints: Hint[] = [];
		if (this.details) {
			hints.push({ key: "↑↓", action: "scroll", priority: 9 }, { key: "f", action: "follow", priority: 6, run: follow });
			if (live) hints.push({ ...stop, priority: 8 });
			hints.push({ key: geometry.split ? "F" : "tab", action: "list", priority: 7, run: details });
			if (canOpen(chosen)) hints.push({ ...open, priority: 5 });
			hints.push({ key: "esc", action: "back", priority: 10, run: () => this.back() }, { key: "q", action: "close", run: () => this.deps.onClose() });
		} else {
			if (tasks.length) hints.push({ key: "↑↓", action: noun, priority: 9 });
			if (chosen && geometry.split) hints.push({ key: "enter", action: "follow", priority: 8, run: follow });
			if (chosen && !geometry.split) hints.push({ key: "tab", action: "thread", priority: 8, run: details });
			if (live) hints.push(stop);
			if (chosen && geometry.split) hints.push({ key: "F", action: "thread", priority: 4, run: details }, { key: "ctrl+j/k", action: "scroll", priority: 3 });
			if (this.agents()) hints.push(scope);
			if (canOpen(chosen)) hints.push(open);
			hints.push({ key: "esc", action: "close", run: () => this.deps.onClose() });
		}
		const fitted = fitHints(hints.map(({ key, action, priority }) => ({ key, action, priority })), geometry.inner);
		const y = geometry.height - 2;
		let x = 2;
		const segs: Seg[] = [];
		fitted.forEach(([key, action], index) => {
			const hint = hints.find((item) => item.key === key && item.action === action)!;
			const id = `hint:${key}`;
			if (index) segs.push(["  ", (t) => t]);
			segs.push([key, (t) => this.hovered === id ? this.deps.theme.fg(HOVER_ROLE, t) : this.bold(this.fg("soft", t))], [` ${action}`, (t) => (this.hovered === id ? `${t.slice(0, 1)}${this.deps.theme.fg(HOVER_ROLE, t.slice(1))}` : this.fg("dim", t))]);
			const cells = visibleWidth(key) + 1 + visibleWidth(action);
			if (hint.run) targets.push({ x, y, cells, target: { id, run: hint.run } });
			x += cells + 2;
		});
		return compose(segs, [], geometry.inner, this.deps.ascii ?? false);
	}
}
