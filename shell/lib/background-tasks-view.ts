// The Agents view (`/jhon:agents`) and the Jobs view (`/jhon:jobs`), separate
// (T92, L657), both drawn as the Gentle Shell's agents overlay (its
// lib/agents-view.ts; T15.12e): the top border titled `力 Agents · this
// session · N active · M finished` (or `力 Jobs · N active · M finished`) with
// `[F Fullscreen]` (or `[← Back]`) and `[× Close]` at its right, the rows
// `▸ └ ◐ <name>  <time>` (an agent's `Subagent <agent> · <its task's first
// line>`), beside the selected task's thread: its header, the name whole and
// wrapped to the pane (L345) — an agent's `<agent> · <status> · <model> ·
// <session> · <tokens> · <cost> · <time>` — then a command's line, output file
// and tail, or a sub-agent's task and its thread (Thinking, Text, `Tool ·
// <name> · Complete` and its Output) and answer. G's footer: `[ Follow ]`,
// an agent's `[ Open session ]`, `[Stop]` while it runs, an agent's
// `[Scope]`, then its keys: j/k move, ctrl+j/k scroll the thread, f follows
// its end, s stops the selected task, a shows all sessions' agents (or this
// session's), o opens the agent's saved session, F (or tab narrow) shows the
// thread alone, esc goes back, q closes; a click selects a row or presses a button.

import { Key, matchesKey, truncateToWidth, visibleWidth, wrapTextWithAnsi, type TuiMouseEvent, type TuiMouseEventResult } from "@earendil-works/pi-tui";
import type { AgentRun, JobRecord, TaskRecord, ThreadItem } from "./background-tasks.ts";
import { formatTaskDuration, taskOutcome, taskSucceeded } from "./background-tasks-tools.ts";
import { measureAgentsViewLayout, type AgentsViewLayout } from "./ui/agents-view-layout.ts";
import { formatCost, formatTokens } from "./ui/bars.ts";
import { sanitizeTerminalText } from "./ui/terminal-theme.ts";
import { STATES } from "./ui/primitives/states.ts";
import { THEME_NAME } from "./ui/primitives/paint.ts";
import { HOVER_ROLE, asciiText, glyph as tableGlyph, paintHover } from "./ui/primitives/index.ts";

export interface TasksViewDeps {
	theme: { fg(role: string, text: string): string; bold?(text: string): string };
	rows: () => number;
	/** Which view: the sub-agents, or the commands and monitors; jobs when absent. */
	view?: "agents" | "jobs";
	/** The owner's session, which the Agents view's scope and headers name. */
	sessionId?: string;
	/** The tasks of this session, or of every session (the Agents view's `a`); the view sorts a copy by activity and recency. */
	tasks: (scope?: Scope) => TaskRecord[];
	now: () => number;
	onStop(task: TaskRecord): void;
	/** Opens a sub-agent's saved session (o, [ Open session ]). */
	onOpen?(task: AgentRun): void;
	onClose(): void;
	requestRender(): void;
	/** shell.appearance.ascii: the frame, glyphs and text in ASCII. */
	ascii?: boolean;
}

const ROLE = { FRAME: "border", TITLE: "customMessageLabel", SELECTED: "accent", NAME_IDLE: "muted", META: "dim", KEY: "accent", KEY_TEXT: "dim", EMPTY: "dim" } as const;
const REFRESH_MS = 1000;
/** G's agents view titles with its brand ❀; Jhon's brand is 力 (jhon in ASCII). */
const TITLE_GLYPH = (ascii: boolean) => (ascii ? "jhon" : "力");
const EMPTY_LIST = "nothing started yet";
const EMPTY_THREAD = "Pick a row to read its thread";
/** A job with no output, or a sub-agent with no activity, yet (L345). */
const NO_OUTPUT = "no output yet";
const FOLLOW_BUTTON = "[ Follow ]";
const STOP_BUTTON = "[Stop]";
const OPEN_BUTTON = "[ Open session ]";
const SCOPE_BUTTON = "[Scope]";
export type Scope = "session" | "all";
const SCOPE_LABEL: Record<Scope, string> = { session: "this session", all: "all sessions" };
const THREAD_LABEL = { thinking: "Thinking", text: "Text" } as const;
const CLOSE_BUTTON = "[× Close]";

interface Button { x: number; width: number; press(): void }

interface Pointer { width: number; height: number; layout: AgentsViewLayout; rows: Array<string | undefined>; buttons: Button[]; footer: Button[] }

function stateGlyph(task: TaskRecord): [string, string] {
	// One glyph per state (ui/primitives/states.ts): a running monitor is working too.
	if (task.status === "running") return [STATES.working.glyph, THEME_NAME[STATES.working.role]];
	if (task.status === "stopped") return ["–", "dim"];
	return taskSucceeded(task) ? [STATES.done.glyph, THEME_NAME[STATES.done.role]] : [STATES.failed.glyph, THEME_NAME[STATES.failed.role]];
}

const clean = (text: string) => sanitizeTerminalText(text).replace(/\s+/g, " ");

/** What a row calls a task: an agent `Subagent <agent> · <its task's first line>` (L318, L657). */
const taskName = (task: TaskRecord) => clean(task.kind === "agent" ? `Subagent ${task.agent} · ${task.task.trim().split("\n")[0] ?? ""}` : task.label);

/** A sub-agent that left a saved session to open. */
const canOpen = (task: TaskRecord | undefined): task is AgentRun => task?.kind === "agent" && Boolean(task.sessionDir && task.childSessionId);

/** A list row: a task. */
type ListRow = { task: TaskRecord };

export class TasksView {
	private readonly deps: TasksViewDeps;
	private selectedId: string | undefined;
	private hoveredId: string | undefined;
	private hoveredButton: string | undefined;
	/** The thread alone: F on a wide terminal, tab on a narrow one. */
	private details = false;
	private listScroll = 0;
	private scroll = 0;
	private follow = true;
	private scope: Scope = "session";
	private closed = false;
	private pointer: Pointer | undefined;
	/** Keyboard input has no width: the last rendered one decides narrow or panes, as G's. */
	private lastWidth: number | undefined;
	private readonly timer: ReturnType<typeof setInterval>;

	constructor(deps: TasksViewDeps) {
		this.deps = deps;
		// Elapsed time, the output tail, and the activity move while a task runs.
		this.timer = setInterval(() => {
			if (this.deps.tasks(this.scope).some((task) => task.status === "running")) this.deps.requestRender();
		}, REFRESH_MS);
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
		const tasks = this.orderedTasks();
		const index = Math.max(0, tasks.findIndex((task) => task.id === this.selected(tasks)?.id));
		const narrow = this.narrow();
		if (data === "q") return this.close();
		if (matchesKey(data, Key.escape)) return this.back();
		if (data === "F" && !narrow) this.details = !this.details;
		else if ((data === "\t" || matchesKey(data, Key.enter)) && narrow) {
			if (this.details) this.details = false;
			else if (this.selected(tasks)) this.details = true;
		} else if (this.details && (data === "j" || data === "k" || matchesKey(data, Key.down) || matchesKey(data, Key.up))) this.scrollBy(data === "j" || matchesKey(data, Key.down) ? 1 : -1);
		else if (data === "j" || matchesKey(data, Key.down)) this.select(tasks[Math.min(tasks.length - 1, index + 1)]);
		else if (data === "k" || matchesKey(data, Key.up)) this.select(tasks[Math.max(0, index - 1)]);
		else if (matchesKey(data, Key.pageDown) || matchesKey(data, Key.ctrl("j"))) this.scrollBy(this.pageRows());
		else if (matchesKey(data, Key.pageUp) || matchesKey(data, Key.ctrl("k"))) this.scrollBy(-this.pageRows());
		else if (data === "f" && this.selected(tasks)) this.follow = true;
		else if (data === "a" && this.agents()) { this.scope = this.scope === "session" ? "all" : "session"; this.selectedId = undefined; }
		else if (data === "o" && this.agents()) { const task = this.selected(tasks); if (canOpen(task)) this.deps.onOpen?.(task); }
		else if (data === "s" || data === "c") {
			const task = this.selected(tasks);
			if (task?.status === "running") this.deps.onStop(task);
		} else return;
		this.pointer = undefined;
		this.deps.requestRender();
	}

	handleMouse(event: TuiMouseEvent): TuiMouseEventResult | undefined {
		if (this.closed) return undefined;
		const pointer = this.pointer;
		if (!pointer || event.width !== pointer.width || event.height !== pointer.height) return undefined;
		const moving = event.type === "move" && event.button === "none";
		const buttons = event.y === 0 ? pointer.buttons : event.y === pointer.height - 2 ? pointer.footer : [];
		const button = buttons.find((item) => event.x >= item.x && event.x < item.x + item.width);
		const listX = pointer.layout.listX;
		const row = !button && event.y >= 1 && event.y <= pointer.layout.bodyRows && event.x >= listX && event.x < listX + pointer.layout.listWidth && (pointer.layout.mode === "panes" || !this.details)
			? pointer.rows[event.y - 1] : undefined;
		if (moving) {
			const nextButton = button ? `${event.y}:${button.x}` : undefined;
			const changed = nextButton !== this.hoveredButton || row !== this.hoveredId;
			this.hoveredButton = nextButton;
			this.hoveredId = row;
			return { handled: true, render: changed };
		}
		if (event.type === "wheel" && event.wheelDelta) {
			this.scrollBy(Math.sign(event.wheelDelta) * 3);
			this.deps.requestRender();
			return { handled: true, render: true };
		}
		if (event.button !== "left" || (event.type !== "click" && event.type !== "press")) return undefined;
		if (!button && !row) return undefined;
		if (event.type === "press") return { handled: true, render: false };
		this.pointer = undefined;
		if (button) button.press();
		else this.select(this.orderedTasks().find((task) => task.id === row));
		this.deps.requestRender();
		return { handled: true, render: true };
	}

	render(width: number): string[] {
		const layout = measureAgentsViewLayout(width, this.deps.rows(), this.details);
		this.lastWidth = layout.width;
		if (layout.width === 0 || layout.height === 0) return [];
		const ascii = this.deps.ascii ?? false;
		if (layout.mode === "fallback") {
			this.pointer = undefined;
			return [this.text(`${ascii ? "x" : "×"}${fit(" Close tasks", layout.width - 1, ascii)}`)];
		}
		const { theme } = this.deps;
		const tasks = this.orderedTasks();
		const selected = this.selected(tasks);
		const inner = layout.width - 2;
		const bar = theme.fg(ROLE.FRAME, tableGlyph("side", ascii));
		const rule = (length: number) => tableGlyph("rule", ascii).repeat(Math.max(0, length));
		const narrowList = layout.mode !== "panes";
		const closeLabel = this.text(layout.width < 60 ? "[×]" : CLOSE_BUTTON).replace("×", ascii ? "x" : "×");
		const modeLabel = this.details ? (layout.width < 24 ? "←" : "[← Back]") : !narrowList && selected ? "[F Fullscreen]" : undefined;
		const closeWidth = visibleWidth(closeLabel);
		const modeWidth = modeLabel ? visibleWidth(modeLabel) : 0;
		const controlsWidth = closeWidth + 1 + (modeLabel ? modeWidth + 1 : 0);
		const closeX = layout.width - 1 - closeWidth;
		const buttons: Button[] = [{ x: closeX, width: closeWidth, press: () => this.close() }];
		if (modeLabel) buttons.push({ x: closeX - modeWidth - 1, width: modeWidth, press: () => this.back(true) });
		const running = tasks.filter((task) => task.status === "running").length;
		const title = truncateToWidth(`${TITLE_GLYPH(ascii)} ${this.agents() ? `Agents · ${SCOPE_LABEL[this.scope]}` : "Jobs"} · ${running} active · ${tasks.length - running} finished`, Math.max(0, inner - 3 - controlsWidth), tableGlyph("ellipsis", ascii));
		const hoverOr = (label: string, x: number, y: number, role: string) => theme.fg(this.hoveredButton === `${y}:${x}` ? HOVER_ROLE : role, label);
		const mode = modeLabel ? ` ${hoverOr(modeLabel, closeX - modeWidth - 1, 0, ROLE.KEY)}` : "";
		const top = theme.fg(ROLE.FRAME, `${tableGlyph("topLeft", ascii)}${rule(1)} `) + theme.fg(ROLE.TITLE, title) + theme.fg(ROLE.FRAME, ` ${rule(inner - visibleWidth(title) - 3 - controlsWidth)}`) + mode + ` ${hoverOr(closeLabel, closeX, 0, ROLE.KEY)}` + theme.fg(ROLE.FRAME, tableGlyph("topRight", ascii));
		this.followList(tasks, layout.bodyRows);
		const list = this.listLines(tasks, selected, layout.bodyRows, ascii);
		const thread = layout.mode === "panes" || this.details ? this.threadWindow(selected, layout.bodyRows, layout.threadWidth) : [];
		const body: string[] = [];
		for (let row = 0; row < layout.bodyRows; row += 1) {
			if (layout.mode === "panes") body.push(`${bar} ${fit(list[row]?.text ?? "", layout.listWidth, ascii)} ${bar} ${fit(thread[row] ?? "", layout.threadWidth, ascii)}${bar}`);
			else body.push(`${bar} ${fit(this.details ? thread[row] ?? "" : list[row]?.text ?? "", layout.listWidth, ascii)} ${bar}`);
		}
		const footerButtons: Button[] = [];
		const footer = this.footer(selected, layout, inner - 2, footerButtons);
		this.pointer = { width: layout.width, height: layout.height, layout, rows: list.map((line) => line.id), buttons, footer: footerButtons };
		const lines = [top, ...body, `${bar} ${fit(footer, inner - 2, ascii)} ${bar}`, theme.fg(ROLE.FRAME, `${tableGlyph("bottomLeft", ascii)}${rule(inner)}${tableGlyph("bottomRight", ascii)}`)];
		// The thread and the list are words with glyphs among them (· ◐ –): folded as the frame is.
		return ascii ? lines.map(asciiText) : lines;
	}

	private narrow(): boolean {
		return measureAgentsViewLayout(this.lastWidth ?? 120, 3).mode !== "panes";
	}

	private back(toggle = false): void {
		if (this.details) this.details = false;
		else if (toggle) this.details = true;
		else return this.close();
		this.pointer = undefined;
		this.deps.requestRender();
	}

	private text(value: string): string {
		return this.deps.ascii ? asciiText(value) : value;
	}

	private agents(): boolean {
		return this.deps.view === "agents";
	}

	/** The view's tasks in display order: running first, then the newest. */
	private orderedTasks(): TaskRecord[] {
		return this.deps.tasks(this.scope).filter((task) => (task.kind === "agent") === this.agents()).sort((a, b) =>
			Number(b.status === "running") - Number(a.status === "running") || b.startedAt - a.startedAt);
	}

	private listRows(tasks: TaskRecord[]): ListRow[] {
		return tasks.map((task) => ({ task }));
	}

	private close(): void {
		this.deps.onClose();
	}

	private select(task: TaskRecord | undefined): void {
		if (!task || task.id === this.selectedId) return;
		this.selectedId = task.id;
		this.scroll = 0;
		this.follow = true;
	}

	private selected(tasks: TaskRecord[]): TaskRecord | undefined {
		const selected = tasks.find((task) => task.id === this.selectedId) ?? tasks[0];
		this.selectedId = selected?.id;
		return selected;
	}

	private elapsed(task: TaskRecord): string {
		return formatTaskDuration((task.endedAt ?? this.deps.now()) - task.startedAt);
	}

	private pageRows(): number {
		return Math.max(1, this.deps.rows() - 4);
	}

	private scrollBy(delta: number): void {
		this.follow = false;
		this.scroll = Math.max(0, this.scroll + delta);
	}

	// Keep the selected row inside the list window, moving the window by the least amount needed; its section's heading too when it is the first.
	private followList(tasks: TaskRecord[], rows: number): void {
		const list = this.listRows(tasks);
		const index = list.findIndex((row) => row.task.id === this.selectedId);
		if (index >= 0 && index < this.listScroll) this.listScroll = index;
		else if (index >= this.listScroll + rows) this.listScroll = index - rows + 1;
		this.listScroll = Math.max(0, Math.min(this.listScroll, Math.max(0, list.length - rows)));
	}

	/** G's list rows: `▸` on the selection (`▹` under the pointer), `└`, the state glyph, the name, the time. */
	private listLines(tasks: TaskRecord[], selected: TaskRecord | undefined, rows: number, ascii: boolean): Array<{ text: string; id?: string }> {
		const { theme } = this.deps;
		if (tasks.length === 0) return [{ text: theme.fg(ROLE.EMPTY, EMPTY_LIST) }];
		return this.listRows(tasks).slice(this.listScroll, this.listScroll + rows).map(({ task }) => {
			const isSelected = task.id === selected?.id;
			const hovered = !isSelected && task.id === this.hoveredId;
			const marker = isSelected ? theme.fg(ROLE.SELECTED, tableGlyph("cursor", ascii)) : hovered ? theme.fg(HOVER_ROLE, ascii ? ">" : "▹") : " ";
			const [mark, role] = stateGlyph(task);
			const name = paintHover(theme, taskName(task), hovered, isSelected ? ROLE.SELECTED : ROLE.NAME_IDLE);
			return { text: `${marker} ${theme.fg(ROLE.META, tableGlyph("tree", ascii))} ${theme.fg(role, mark)} ${name}  ${theme.fg(ROLE.META, this.elapsed(task))}`, id: task.id };
		});
	}

	/** The selected task's thread: its header, then its lines, scrolled to the end while following. */
	private threadWindow(task: TaskRecord | undefined, rows: number, width: number): string[] {
		const { theme } = this.deps;
		if (!task) return [theme.fg(ROLE.EMPTY, EMPTY_THREAD)];
		// The header names the task whole, wrapped to the pane, never cut (L345).
		const header = wrap(this.header(task), width).map((line) => theme.fg(ROLE.META, line));
		const lines = (task.kind === "agent" ? this.agentLines(task) : this.jobLines(task)).flatMap((line) => line === "" ? [""] : wrap(line, width));
		const visible = Math.max(0, rows - header.length);
		const maxScroll = Math.max(0, lines.length - visible);
		if (this.follow) this.scroll = maxScroll;
		const start = Math.min(this.scroll, maxScroll);
		return [...header, ...lines.slice(start, start + visible)];
	}

	/** G's thread header: an agent's `<agent> · <status> · <model> · <session> · <tokens> · <cost> · <time>`; a job's name, status, time and a monitor's events. */
	private header(task: TaskRecord): string {
		if (task.kind === "agent") {
			const session = task.ownerSessionId === this.deps.sessionId ? SCOPE_LABEL.session : `session ${task.ownerSessionId.slice(0, 8)}`;
			return [task.agent, taskOutcome(task), task.model ? clean(task.model) : "", session, task.tokens > 0 ? formatTokens(task.tokens) : "", task.cost > 0 ? formatCost(task.cost) : "", this.elapsed(task)].filter(Boolean).join(" · ");
		}
		const events = task.kind === "monitor" ? `${task.events ?? 0} event${task.events === 1 ? "" : "s"}` : "";
		return [taskName(task), clean(taskOutcome(task)), this.elapsed(task), events].filter(Boolean).join(" · ");
	}

	private jobLines(job: JobRecord): string[] {
		const { theme } = this.deps;
		const tail = job.tail.map((line) => sanitizeTerminalText(line));
		return [`$ ${clean(job.command)}`, theme.fg(ROLE.META, clean(job.outputPath)), "", ...(tail.length ? tail : [theme.fg(ROLE.EMPTY, NO_OUTPUT)])];
	}

	private agentLines(run: AgentRun): string[] {
		const { theme } = this.deps;
		const answer = run.status === "running" || !run.result ? [] : ["", theme.fg(ROLE.META, run.status === "completed" ? "Answer" : "Reason"), ...run.result.split("\n").map((line) => sanitizeTerminalText(line))];
		const thread = (run.thread ?? []).flatMap((item) => this.threadItem(item));
		const activity = run.activity.map((line) => sanitizeTerminalText(line));
		const body = thread.length ? thread : activity.length ? activity : [theme.fg(ROLE.EMPTY, NO_OUTPUT)];
		return [...run.task.split("\n").map((line) => sanitizeTerminalText(line)), "", ...body, ...answer];
	}

	/** G's thread item: `Thinking` or `Text` and its lines indented; `Tool · <name> · Running|Complete|Error`, then `  Output` and its lines. */
	private threadItem(item: ThreadItem): string[] {
		const { theme } = this.deps;
		const lines = (text: string, indent: number, role: string) => text.split("\n").map((line) => theme.fg(role, `${" ".repeat(indent)}${sanitizeTerminalText(line)}`));
		if (item.kind !== "tool") return [theme.fg(ROLE.TITLE, THREAD_LABEL[item.kind]), ...lines(item.text, 2, item.kind === "thinking" ? "dim" : "text")];
		const status = item.isError ? "Error" : item.running ? "Running" : "Complete";
		const head = theme.fg(item.isError ? "error" : "accent", `Tool · ${clean(item.name)} · ${status}`);
		return item.output ? [head, theme.fg("muted", "  Output"), ...lines(item.output, 4, "muted")] : [head];
	}

	/** G's footer: `[ Follow ]`, `[Stop]` while the task runs, then the keys; the buttons are pointer targets. */
	private footer(task: TaskRecord | undefined, layout: AgentsViewLayout, width: number, buttons: Button[]): string {
		const { theme } = this.deps;
		const y = layout.height - 2;
		const controls = [{ label: FOLLOW_BUTTON, enabled: Boolean(task), press: () => { this.follow = true; } }];
		if (canOpen(task)) controls.push({ label: OPEN_BUTTON, enabled: true, press: () => this.deps.onOpen?.(task) });
		if (task?.status === "running") controls.push({ label: STOP_BUTTON, enabled: true, press: () => this.deps.onStop(task) });
		if (this.agents()) controls.push({ label: SCOPE_BUTTON, enabled: true, press: () => { this.scope = this.scope === "session" ? "all" : "session"; this.selectedId = undefined; } });
		let text = "";
		for (const control of controls) {
			if (text) text += " ";
			const x = 2 + visibleWidth(text);
			if (control.enabled) buttons.push({ x, width: control.label.length, press: control.press });
			text += control.enabled ? theme.fg(this.hoveredButton === `${y}:${x}` ? HOVER_ROLE : ROLE.KEY, control.label) : theme.fg(ROLE.META, control.label);
		}
		const keys: Array<[string, string]> = [["j/k", "task"], ...(layout.mode !== "panes" && task ? [["tab", this.details ? "back" : "details"] as [string, string]] : []), ["ctrl+j/k", "scroll"], ["f", "follow"], ...(task?.status === "running" ? [["s", "stop it"] as [string, string]] : []),
			...(this.agents() ? [["a", this.scope === "session" ? SCOPE_LABEL.all : SCOPE_LABEL.session] as [string, string]] : []), ...(canOpen(task) ? [["o", "open session"] as [string, string]] : []), ["esc", "back"], ["q", "close"]];
		const keyText = keys.map(([key, label]) => `${theme.fg(ROLE.KEY, key)} ${theme.fg(ROLE.KEY_TEXT, label)}`).join("   ");
		if (visibleWidth(text) + 1 < width) text += " " + truncateToWidth(keyText, width - visibleWidth(text) - 1, "…");
		return text;
	}
}

function fit(text: string, width: number, ascii = false): string {
	const clipped = truncateToWidth(text, Math.max(0, width), tableGlyph("ellipsis", ascii));
	return clipped + " ".repeat(Math.max(0, width - visibleWidth(clipped)));
}

function wrap(line: string, width: number): string[] {
	return width <= 0 || visibleWidth(line) <= width ? [line] : wrapTextWithAnsi(line, width);
}
