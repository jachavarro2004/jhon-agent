/**
 * shell/extensions/quiet.ts — Jhon's tool cards (T174-S6, design/shell-tui 01).
 *
 * read, grep, find, ls, edit and write run as pi's own tools under Jhon's
 * cards; bash keeps pi's tool and gets the same card through a renderer
 * resolver; every other tool's call gets its family icon there too
 * (lib/ui/tool-icons.ts), and memory calls their one line.
 *
 * A card (lib/ui/tool-card.ts): the icon and the title, the state at the
 * right of the header (`◐ running`, `✓ 2.3s`, `+3 −1`, `↳ 61 lines`,
 * `✗ failed`), then the body: bash's `$ command` and its output tail, the
 * numbered diff of an edit, a preview of what a read or a search found. A
 * call a Jhon guard refused is a red band, `✗ refused  <call>  <guard>`, with
 * the reason and the fix under it. ctrl+o (pi's app.tools.expand) shows all
 * of it; the key's hint rides the header while expanding shows more.
 * shell.appearance.density "minimal" keeps bash quiet (`↳ N lines` and a
 * short preview, no tail or duration).
 */
import type { AgentToolResult, ExtensionAPI, ExtensionFactory, ToolDefinition, ToolRenderers } from "@earendil-works/pi-coding-agent";
import {
	createEditTool,
	createFindTool,
	createGrepTool,
	createLsTool,
	createReadToolDefinition,
	createWriteTool,
	keyHint,
} from "@earendil-works/pi-coding-agent";
import { visibleWidth, type Component } from "@earendil-works/pi-tui";
import { homedir } from "node:os";
import { commandFailure, failureHeadline, runningLine } from "../lib/ui/command-failure.ts";
import { loadDoubleEscCancelPolicy } from "../lib/ui/double-esc-cancel-policy.ts";
import { CARD_STYLE, asciiText, cardStyle, stateGlyph, stripAnsi } from "../lib/ui/primitives/index.ts";
import { bodyCells, bodyRows, diffCounts, diffRows, guardRefusal, headRows, paintDiff, titleCells, wrapRows, type CardHead, type CardLook, type GuardRefusal } from "../lib/ui/tool-card.ts";
import { sanitizeTerminalText } from "../lib/ui/terminal-theme.ts";
import { shellSettings } from "../lib/settings.ts";
import { isMemoryTool, memoryToolRenderers, ownsIcon, toolIcon, withToolIcon } from "../lib/ui/tool-icons.ts";

type CardTool = "read" | "bash" | "grep" | "find" | "ls" | "edit" | "write";
type PiTool = Exclude<CardTool, "bash">;
type Paint = { fg(role: string, text: string): string; bold(text: string): string; bg?(role: string, text: string): string };
type Args = Record<string, unknown>;

/** The context pi hands a tool's renderers; only what the cards read. */
interface RowContext {
	args?: unknown;
	toolCallId?: string;
	state?: unknown;
	executionStarted?: boolean;
	isPartial?: boolean;
	isError?: boolean;
	expanded?: boolean;
	durationMs?: number;
	cwd?: string;
}

const PI_TOOLS: Record<PiTool, (cwd: string) => any> = {
	read: createReadToolDefinition,
	grep: createGrepTool,
	find: createFindTool,
	ls: createLsTool,
	edit: createEditTool,
	write: createWriteTool,
};

/** Rows a collapsed preview keeps: a read's first, a search's first, bash's last (minimal), a failure's tail. */
const PREVIEW = 3;
/** A full bash card's output tail. */
const BASH_TAIL = 5;
/** A collapsed git command keeps more of its tail: a status or a diff reads from it. */
const GIT_TAIL = 10;
/** A collapsed edit's numbered diff rows; minimal keeps a preview's worth. */
const DIFF_ROWS = 12;
/** A call narrower than this is cut in its header, not wrapped under it. */
const MIN_WRAP = 12;

const COUNTED: Partial<Record<CardTool, [string, string]>> = {
	grep: ["match", "matches"],
	find: ["file", "files"],
	ls: ["entry", "entries"],
};

// The texts pi's tools answer when they found nothing.
const NOTHING: Partial<Record<CardTool, readonly string[]>> = {
	grep: ["No matches found"],
	find: ["No files found matching pattern"],
	ls: ["Directory is empty", "(empty directory)"],
	bash: ["(no output)"],
};

/** T91 (L650): cards are full; shell.appearance.density "minimal" keeps bash quiet. */
export function quietCards(): boolean {
	try { return shellSettings().density() === "minimal"; } catch { return false; }
}

/** A duration as pi words it: `0.4s`, `2m 5s`, `1h 2m 5s`. */
export function formatDuration(ms: number): string {
	const tenths = Math.max(0, ms) / 1000;
	if (tenths < 60) return `${tenths.toFixed(1)}s`;
	const all = Math.floor(tenths);
	const m = Math.floor(all / 60);
	return m < 60 ? `${m}m ${all % 60}s` : `${Math.floor(m / 60)}h ${m % 60}m ${all % 60}s`;
}

/** A failed command's first rows: the exit code and counts, the first failure, where it is. */
export function failureRows(text: string): string[] {
	const failure = commandFailure(text);
	const rows = [failureHeadline(failure)];
	if (failure.first) rows.push(`✗ ${failure.first}`);
	if (failure.where) rows.push(`at ${failure.where}`);
	return rows.filter((row) => row.length > 0);
}

// --- reading a call and its result ----------------------------------------------------------

const clean = (value: string) => sanitizeTerminalText(value);
const field = (args: Args, key: string): string => (typeof args[key] === "string" && (args[key] as string).length > 0 ? clean(args[key] as string) : "");

function home(path: string): string {
	const dir = homedir();
	return path.startsWith(dir) ? `~${path.slice(dir.length)}` : path;
}

function argsOf(value: unknown): Args {
	return typeof value === "object" && value !== null && !Array.isArray(value) ? (value as Args) : {};
}

function textOf(result: AgentToolResult<unknown>): string {
	return clean(result.content.flatMap((part) => (part.type === "text" ? [part.text] : [])).join("\n"));
}

/** Output lines: CRLF folded, the last newline dropped; none for empty text. */
function linesOf(text: string): string[] {
	const body = text.replace(/\r\n/g, "\n").replace(/\n$/, "");
	return body ? body.split("\n") : [];
}

const filled = (lines: readonly string[]) => lines.filter((line) => line.trim().length > 0);

function saysNothing(tool: CardTool, text: string): boolean {
	return NOTHING[tool]?.includes(text.trim()) ?? false;
}

function isGit(args: Args): boolean {
	return /^(?:env\s+\S+=\S+\s+|command\s+|\w+=\S+\s+)*git(?:\s|$)/.test(field(args, "command").trim());
}

/** grep with context lines counts its `path:line:` rows only. */
function grepCount(text: string, args: Args): number {
	if (typeof args.context === "number" && args.context > 0) return linesOf(text).filter((line) => /^\s*.+:\d+:\s?/.test(line)).length;
	return filled(linesOf(text)).length;
}

/** Pretty JSON's first lines with only structure left out, for a bash that printed one compact JSON value. */
function jsonPreview(text: string): string[] | undefined {
	const trimmed = text.trim();
	if (!/^[\[{]/.test(trimmed)) return undefined;
	try {
		const pretty = JSON.stringify(JSON.parse(trimmed), null, 2);
		return pretty === undefined ? undefined : linesOf(pretty).filter((line) => !/^[\s{}\[\],]*$/.test(line)).slice(0, PREVIEW);
	} catch {
		return undefined;
	}
}

const plural = (n: number, [one, many]: [string, string]) => `${n} ${n === 1 ? one : many}`;

/** What `Successfully wrote N bytes` says, in a few words. */
function wrote(text: string): string {
	const bytes = /(?:Successfully )?wrote\s+(\d+)\s+bytes/i.exec(text)?.[1];
	return bytes ? `wrote ${bytes} bytes` : "written";
}

// --- the call's title -------------------------------------------------------------------------

function titleOf(tool: CardTool, args: Args, t: Paint): string {
	const name = t.fg("toolTitle", t.bold(tool));
	const path = (key = "path", fallback = "...") => t.fg("mdCode", home(field(args, key)) || fallback);
	const limit = typeof args.limit === "number" ? t.fg("toolOutput", ` limit ${args.limit}`) : "";
	switch (tool) {
		case "bash": {
			const timeout = typeof args.timeout === "number" ? t.fg("muted", ` (timeout ${args.timeout}s)`) : "";
			return `${t.fg("toolTitle", t.bold(field(args, "command") || "..."))}${timeout}`;
		}
		case "read": {
			if (args.offset === undefined && args.limit === undefined) return `${name}  ${path()}`;
			const from = typeof args.offset === "number" ? args.offset : 1;
			const to = typeof args.limit === "number" ? `-${from + args.limit - 1}` : "";
			return `${name}  ${path()}${t.fg("warning", `:${from}${to}`)}`;
		}
		case "grep": {
			const glob = field(args, "glob") ? t.fg("toolOutput", ` (${field(args, "glob")})`) : "";
			return `${name}  ${t.fg("mdCode", `/${field(args, "pattern")}/`)} in ${home(field(args, "path")) || "."}${glob}${limit}`;
		}
		case "find":
			return `${name}  ${t.fg("mdCode", field(args, "pattern") || "*")} in ${home(field(args, "path")) || "."}${limit}`;
		case "ls":
			return `${name}  ${path("path", ".")}${limit}`;
		case "edit":
			return `${name}  ${path()}`;
		case "write": {
			const content = typeof args.content === "string" ? args.content : "";
			return `${name}  ${path()}${content ? t.fg("muted", ` (${content.split("\n").length} lines)`) : ""}`;
		}
	}
}

/** A refused call in a few words: `$ <command>`, or the tool and its target. */
function refusedCall(tool: CardTool, args: Args, t: Paint): string {
	if (tool === "bash") return t.fg("toolTitle", `$ ${field(args, "command")}`);
	const target = field(args, "path") || field(args, "pattern");
	return t.fg("toolTitle", target ? `${tool} ${home(target)}` : tool);
}

// --- the shared row state -----------------------------------------------------------------------

/** What the result tells the call's header, in the row state pi shares between them. */
interface Settled {
	right: string;
	tone: "normal" | "error";
	refused?: GuardRefusal;
	/** Whether ctrl+o changes the body at this width. */
	expandable?: (width: number, look: CardLook) => boolean;
	/** The collapsed body says its own expand key (bash's earlier lines): the header leaves it out. */
	ownHint?: boolean;
}

interface RowState { body?: true; settled?: () => Settled; startedAt?: number; endedAt?: number }

const ROW = Symbol.for("jhon-shell.tool-card-row");

function rowState(state: unknown): RowState {
	if (state === null || typeof state !== "object") return {};
	const slot = state as Record<symbol, RowState | undefined>;
	return (slot[ROW] ??= {});
}

/** The card's form and glyphs as the settings say now. */
function lookOf(theme: Paint): CardLook {
	let ascii = false;
	try { ascii = shellSettings().ascii(); } catch { /* No settings: Unicode. */ }
	return { form: cardStyle() === CARD_STYLE.NEON ? "outline" : "tint", ascii, theme };
}

const shellText = (text: string, ascii: boolean) => (ascii ? asciiText(text) : text);

function took(context: RowContext, row: RowState): string | undefined {
	if (typeof context.durationMs === "number") return formatDuration(context.durationMs);
	if (row.startedAt === undefined) return undefined;
	row.endedAt ??= Date.now();
	return formatDuration(row.endedAt - row.startedAt);
}

/** When each running bash started and whether its cancel takes a double esc, by call id. */
const running = new Map<string, { started: number; doubleEsc: boolean }>();

function runningLabel(tool: CardTool, text: string, callId: string | undefined, now = Date.now()): string {
	const n = filled(linesOf(text)).length;
	const label = n === 0 ? `… ${tool}` : `… ${tool} · ${n} ${n === 1 ? "line" : "lines"}`;
	if (tool !== "bash" || !callId) return label;
	let run = running.get(callId);
	if (!run) {
		let doubleEsc = false;
		try { doubleEsc = loadDoubleEscCancelPolicy() === "on"; } catch { /* Off when unset. */ }
		run = { started: now, doubleEsc };
		running.set(callId, run);
	}
	return `${label} · ${runningLine(now - run.started, run.doubleEsc)}`;
}

// --- the components ---------------------------------------------------------------------------

/** One component's rows, kept while nothing that draws them changed. */
class Kept {
	private key?: string;
	private rows: string[] = [];
	get(key: string, draw: () => string[]): string[] {
		if (key !== this.key) { this.rows = draw(); this.key = key; }
		return this.rows;
	}
	clear(): void { this.key = undefined; }
}

/** The call: the header (and the rest of a long command), closing an outline while no result exists. */
class CallCard implements Component {
	private readonly kept = new Kept();
	private readonly tool: CardTool;
	private readonly args: Args;
	private readonly theme: Paint;
	private readonly context: RowContext;

	constructor(tool: CardTool, args: Args, theme: Paint, context: RowContext) {
		this.tool = tool;
		this.args = args;
		this.theme = theme;
		this.context = context;
	}

	render(width: number): string[] {
		const cells = Math.max(0, Math.floor(width));
		if (cells === 0) return [];
		const look = lookOf(this.theme);
		const row = rowState(this.context.state);
		const settled = row.settled?.();
		const t = this.theme;
		let head: CardHead;
		let title: string;
		// The expand key rides the header while ctrl+o would change the body.
		const expanded = this.context.expanded === true;
		const hint = settled?.expandable?.(cells, look) && (expanded || !settled.ownHint) ? t.fg("dim", stripAnsi(keyHint("app.tools.expand", expanded ? "to collapse" : "to expand"))) : "";
		const withHint = (state: string) => (hint ? `${hint}  ${state}` : state);
		if (settled?.refused) {
			head = { icon: stateGlyph("failed", look.ascii), title: "", right: withHint(t.fg("text", settled.refused.guard)), tone: "error" };
			title = `${t.bold(t.fg("text", "refused"))}  ${refusedCall(this.tool, this.args, t)}`;
		} else {
			let right: string | undefined;
			if (settled) {
				right = withHint(settled.right);
			} else if (this.context.isPartial !== false) {
				right = t.fg("warning", `${stateGlyph("working", look.ascii)} running`);
			}
			head = { icon: toolIcon(this.tool), title: "", right, tone: settled?.tone ?? (this.context.isError ? "error" : "normal") };
			title = titleOf(this.tool, this.args, t);
		}
		const awaiting = !row.body && this.context.isPartial !== false;
		const key = JSON.stringify([cells, look.form, look.ascii, title, head.right, head.tone, head.icon, awaiting]);
		return this.kept.get(key, () => {
			const [first = "", ...rest] = title.split("\n");
			const room = titleCells(cells, look, head);
			// Too narrow to keep the words: the header cuts the call instead of wrapping it a few cells at a time.
			if (room < MIN_WRAP) return headRows({ ...head, title: first }, [], cells, look, awaiting);
			const [shown = "", ...wrapped] = wrapRows(first, room);
			const overflow = [...wrapped, ...rest.flatMap((line) => wrapRows(t.fg("toolTitle", line), bodyCells(cells, look)))];
			return headRows({ ...head, title: shown }, overflow, cells, look, awaiting);
		});
	}

	invalidate(): void { this.kept.clear(); }
}

/** A result's body: `lines` while collapsed, `full` expanded (wrapped, every word kept). */
interface Body {
	lines: string[];
	full: string[];
	/** Collapsing hides whole lines. */
	hides: boolean;
	/** A component whose own rows are the expanded body (an image read). */
	own?: Component;
}

/** The result: its body, painted when first drawn and again after an invalidate (a theme change). */
class ResultCard implements Component {
	private readonly kept = new Kept();
	private readonly body: () => Body;
	private readonly expanded: boolean;
	private readonly tone: () => CardHead["tone"];
	private readonly theme: Paint;
	private readonly forget: () => void;

	constructor(body: () => Body, expanded: boolean, tone: () => CardHead["tone"], theme: Paint, forget: () => void) {
		this.body = body;
		this.expanded = expanded;
		this.tone = tone;
		this.theme = theme;
		this.forget = forget;
	}

	render(width: number): string[] {
		const cells = Math.max(0, Math.floor(width));
		if (cells === 0) return [];
		const look = lookOf(this.theme);
		return this.kept.get(JSON.stringify([cells, look.form, look.ascii]), () => {
			const body = this.body();
			const room = bodyCells(cells, look);
			const rows = body.own && this.expanded
				? body.own.render(room).map((line) => line.trimEnd())
				: this.expanded ? body.full.flatMap((line) => wrapRows(line, room)) : body.lines;
			return bodyRows(rows, cells, look, this.tone());
		});
	}

	invalidate(): void { this.body().own?.invalidate(); this.kept.clear(); this.forget(); }
}

/** Whether a collapsed body hides anything at width: whole lines, or a line cut at the card's edge. */
function hidesAt(body: Body): (width: number, look: CardLook) => boolean {
	return (width, look) => body.hides || body.lines.some((line) => visibleWidth(line) > bodyCells(width, look));
}

// --- each result's body -------------------------------------------------------------------------

/** A partial result: the running label, then the newest lines. */
function partialBody(tool: CardTool, text: string, callId: string | undefined, t: Paint, ascii: boolean): Body {
	const label = t.fg("warning", shellText(runningLabel(tool, text, callId), ascii));
	const lines = linesOf(text);
	return { lines: [label, ...lines.slice(-PREVIEW).map((line) => t.fg("muted", line))], full: [label, ...lines.map((line) => t.fg("muted", line))], hides: lines.length > PREVIEW };
}

function refusalBody(refused: GuardRefusal, t: Paint): Body {
	const said = [refused.reason, ...(refused.fix ? [refused.fix] : [])].map((line) => t.fg("muted", line));
	return { lines: said, full: [...said, ...refused.more.map((line) => t.fg("dim", line))], hides: refused.more.length > 0 };
}

/** A failure: why first (bash: Jhon's headline, the first failure, its place), then the output once. */
function failureBody(tool: CardTool, text: string, full: boolean, t: Paint, ascii: boolean): Body {
	const lines = linesOf(text);
	const why = tool === "bash" ? failureRows(text) : [];
	const tail = full ? BASH_TAIL : why.length ? PREVIEW : PREVIEW - 1;
	const headline = why[0] ?? filled(lines)[0]?.trim() ?? "failed";
	const head = [t.fg("error", why.length ? shellText(headline, ascii) : headline), ...why.slice(1).map((row) => t.fg(row.startsWith("at ") ? "accent" : "error", shellText(row, ascii)))];
	// Say it once: the rows the why already says leave the output.
	const said = why.slice(1).map((row) => row.replace(/^(✗ |at )/, "").trim());
	let rest: string[];
	if (why.length) rest = linesOf(text.replace(/\n?\n?[ \t]*Command exited with code \d+[ \t]*$/, ""));
	else {
		const at = lines.findIndex((line) => line.trim() === headline);
		rest = at >= 0 ? [...lines.slice(0, at), ...lines.slice(at + 1)] : lines;
	}
	const fresh = filled(rest).filter((line) => !said.some((row) => row && line.includes(row)));
	const shown = fresh.slice(-tail);
	const earlier = fresh.length - shown.length;
	return {
		lines: [...head, ...shown.map((line) => t.fg("muted", line))],
		full: [...head, ...rest.map((line) => t.fg("toolOutput", line))],
		hides: earlier > 0 || filled(rest).length > fresh.length,
	};
}

/** A full bash card's body: the output tail under `... (N earlier lines, <key> to expand)`. */
function bashBody(text: string, t: Paint): Body {
	const lines = saysNothing("bash", text) ? [] : linesOf(text.replace(/^\n+/, "").replace(/\s+$/, ""));
	const hidden = lines.length - BASH_TAIL;
	const earlier = hidden > 0 ? [`${t.fg("muted", `... (${hidden} earlier ${hidden === 1 ? "line" : "lines"},`)} ${stripAnsi(keyHint("app.tools.expand", "to expand"))}${t.fg("muted", ")")}`] : [];
	return { lines: [...earlier, ...lines.slice(-BASH_TAIL).map((line) => t.fg("toolOutput", line))], full: lines.map((line) => t.fg("toolOutput", line)), hides: hidden > 0 };
}

/** A collapsed preview: `count` lines from the head or the tail; all of them expanded. */
function previewBody(lines: readonly string[], count: number, from: "head" | "tail", t: Paint, shown?: readonly string[]): Body {
	const kept = shown ?? (from === "head" ? lines.slice(0, count) : lines.slice(-count));
	return { lines: kept.map((line) => t.fg("muted", line)), full: lines.map((line) => t.fg("toolOutput", line)), hides: lines.length > kept.length || shown !== undefined };
}

/**
 * The card for one result: the header's state and the body. Pure: the time
 * and the running label come in through `context` and `row`.
 */
function settle(tool: CardTool, result: AgentToolResult<unknown>, text: string, isError: boolean, context: RowContext, row: RowState, t: Paint, ascii: boolean): { settled: Settled; body: Body } {
	const args = argsOf(context.args);
	const sign = (name: "done" | "failed") => stateGlyph(name, ascii);
	const arrow = ascii ? ">" : "↳";
	if (isError) {
		const refused = guardRefusal(text);
		if (refused) {
			const body = refusalBody(refused, t);
			return { settled: { right: refused.guard, tone: "error", refused, expandable: hidesAt(body) }, body };
		}
		const full = tool === "bash" && !quietCards();
		const body = failureBody(tool, text, full, t, ascii);
		const time = full ? took(context, row) : undefined;
		return { settled: { right: t.fg("error", `${sign("failed")} ${time ?? "failed"}`), tone: "error", expandable: hidesAt(body) }, body };
	}
	const summary = (words: string) => t.fg("muted", `${arrow} ${words}`);
	const lines = linesOf(text);
	switch (tool) {
		case "bash": {
			if (!quietCards()) {
				const body = bashBody(text, t);
				const time = took(context, row);
				return { settled: { right: `${t.fg("success", sign("done"))}${time ? ` ${t.fg("muted", time)}` : ""}`, tone: "normal", expandable: hidesAt(body), ownHint: body.hides }, body };
			}
			const empty = saysNothing("bash", text) || lines.length === 0;
			const output = empty ? [] : lines;
			const shown = empty ? [] : isGit(args) ? lines.slice(-GIT_TAIL) : jsonPreview(text) ?? lines.slice(-PREVIEW);
			const body = previewBody(output, PREVIEW, "tail", t, shown.length === output.length && shown.every((line, i) => line === output[i]) ? undefined : shown);
			return { settled: { right: summary(empty ? "no output" : plural(lines.length, ["line", "lines"])), tone: "normal", expandable: hidesAt(body) }, body };
		}
		case "read": {
			if (result.content.some((part) => part.type === "image")) {
				return { settled: { right: summary("image"), tone: "normal" }, body: { lines: [], full: [], hides: false } };
			}
			const body = previewBody(lines, PREVIEW, "head", t);
			return { settled: { right: summary(plural(lines.length, ["line", "lines"])), tone: "normal", expandable: hidesAt(body) }, body };
		}
		case "grep":
		case "find":
		case "ls": {
			const words = COUNTED[tool]!;
			const empty = saysNothing(tool, text) || filled(lines).length === 0;
			const count = tool === "grep" ? grepCount(text, args) : filled(lines).length;
			const body = previewBody(empty ? [] : lines, PREVIEW, "head", t);
			return { settled: { right: summary(empty ? (tool === "ls" ? "empty" : `no ${words[1]}`) : plural(count, words)), tone: "normal", expandable: hidesAt(body) }, body };
		}
		case "edit": {
			const diff = argsOf(result.details).diff;
			if (typeof diff !== "string") {
				const body = previewBody(lines, 0, "head", t);
				return { settled: { right: summary("applied"), tone: "normal", expandable: hidesAt(body) }, body };
			}
			const rows = paintDiff(diffRows(clean(diff)), t, ascii);
			const { added, removed } = diffCounts(clean(diff));
			const kept = rows.slice(0, quietCards() ? PREVIEW : DIFF_ROWS);
			const body = { lines: kept, full: rows, hides: rows.length > kept.length };
			const minus = ascii ? "-" : "−";
			return { settled: { right: `${t.fg("success", `+${added}`)} ${t.fg("error", `${minus}${removed}`)}`, tone: "normal", expandable: hidesAt(body) }, body };
		}
		case "write": {
			// The result only says it wrote; what it wrote is the call's content.
			const content = typeof args.content === "string" && args.content.length > 0 ? clean(args.content) : text;
			const body = { lines: [], full: linesOf(content).map((line) => t.fg("toolOutput", line)), hides: content.length > 0 };
			return { settled: { right: summary(wrote(text)), tone: "normal", expandable: hidesAt(body) }, body };
		}
	}
}

/** The card renderers for one tool; bash's attach to pi's own bash through the resolver below. */
export function createQuietToolRenderer(tool: CardTool, piRenderResult?: ToolDefinition["renderResult"]): Pick<ToolDefinition, "renderShell" | "renderCall" | "renderResult"> {
	return {
		renderShell: "self",
		renderCall: ownsIcon((args, theme, context) => {
			const row = rowState(context?.state);
			if (context?.executionStarted && row.startedAt === undefined) row.startedAt = Date.now();
			return new CallCard(tool, argsOf(args), theme as unknown as Paint, (context ?? {}) as RowContext);
		}),
		renderResult(result, options, theme, context) {
			const t = theme as unknown as Paint;
			const ctx = (context ?? {}) as RowContext;
			const row = rowState(ctx.state);
			row.body = true;
			const text = textOf(result);
			// pi says an error on the context; older hosts said it on the options.
			const isError = ctx.isError ?? ("isError" in options && (options as { isError?: boolean }).isError === true);
			const tone = isError ? "error" : "normal";
			if (options.isPartial) {
				row.settled = undefined;
				let partial: Body | undefined;
				return new ResultCard(() => (partial ??= partialBody(tool, text, ctx.toolCallId, t, lookOf(t).ascii)), options.expanded, () => tone, t, () => { partial = undefined; });
			}
			if (ctx.toolCallId) running.delete(ctx.toolCallId);
			let card: { settled: Settled; body: Body } | undefined;
			const made = () => {
				if (card) return card;
				card = settle(tool, result, text, isError === true, ctx, row, t, lookOf(t).ascii);
				if (tool === "read" && options.expanded && piRenderResult && result.content.some((part) => part.type === "image")) {
					const safe = { ...result, content: result.content.map((part) => (part.type === "image" ? { ...part, mimeType: clean(part.mimeType) } : part.type === "text" ? { ...part, text: clean(part.text) } : part)) };
					card.body.own = piRenderResult(safe, options, theme, { ...(context as object), args: argsOf(ctx.args), cwd: typeof ctx.cwd === "string" ? clean(ctx.cwd) : ctx.cwd } as any);
				}
				return card;
			};
			row.settled = () => made().settled;
			return new ResultCard(() => made().body, options.expanded, () => made().settled.tone, t, () => { card = undefined; });
		},
	};
}

/** Registers one of pi's tools under Jhon's card, running it in the session's working folder. */
function register(pi: ExtensionAPI, tool: PiTool): void {
	const made = new Map<string, any>();
	const toolAt = (cwd: string) => {
		let found = made.get(cwd);
		if (!found) { found = PI_TOOLS[tool](cwd); made.set(cwd, found); }
		return found;
	};
	const definition = toolAt(process.cwd());
	pi.registerTool({
		...definition,
		async execute(toolCallId, params, signal, onUpdate, ctx) {
			return toolAt(ctx.cwd).execute(toolCallId, params, signal, onUpdate, ctx);
		},
		...createQuietToolRenderer(tool, definition.renderResult),
	});
}

/** Jhon's tool cards, always (L76). */
export default function quietTools(pi: ExtensionAPI): ReturnType<ExtensionFactory> {
	for (const tool of Object.keys(PI_TOOLS) as PiTool[]) register(pi, tool);
	const host = pi as typeof pi & { registerToolRenderer?: (resolver: (toolName: string, next: () => unknown) => unknown) => void };
	if (typeof host.registerToolRenderer !== "function") return;
	// T91 (L650): bash is a Jhon card over pi's own tool; T33 (L97): every
	// other call starts with its family's icon, memory calls are one line.
	const bash = createQuietToolRenderer("bash") as ToolRenderers;
	host.registerToolRenderer((toolName, next) => {
		if (toolName === "bash") return { ...(next() as ToolRenderers | undefined), ...bash };
		const renderers = next() as ToolRenderers | undefined;
		if (isMemoryTool(toolName) && renderers) return { ...renderers, renderShell: "default", ...memoryToolRenderers(toolName) };
		return withToolIcon(toolName, renderers);
	});
}
