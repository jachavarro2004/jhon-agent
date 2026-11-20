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
import { Text, type Component } from "@earendil-works/pi-tui";
import { homedir } from "node:os";
import { commandFailure, failureHeadline, runningLine } from "../lib/ui/command-failure.ts";
import { loadDoubleEscCancelPolicy } from "../lib/ui/double-esc-cancel-policy.ts";
import {
	CARD_TONE, cardAwaitingResult, cardInnerWidth, cardStyle, drawCard, markCardResult,
	type CardRowContext, type CardTheme,
} from "../lib/ui/primitives/index.ts";
import { sanitizeTerminalText, stripAnsi } from "../lib/ui/terminal-theme.ts";
import { shellSettings } from "../lib/settings.ts";
import { isMemoryTool, memoryToolRenderers, ownsIcon, toolIcon, withToolIcon } from "../lib/ui/tool-icons.ts";

type QuietToolName = "read" | "bash" | "grep" | "find" | "ls" | "edit" | "write";
type RegisteredToolName = Exclude<QuietToolName, "bash">;
type ThemeLike = {
	bold(value: string): string;
	fg(color: string, value: string): string;
};

const TOOL_CREATORS = {
	read: createReadToolDefinition,
	grep: createGrepTool,
	find: createFindTool,
	ls: createLsTool,
	edit: createEditTool,
	write: createWriteTool,
} satisfies Record<RegisteredToolName, (cwd: string) => any>;

const COLLAPSED_COUNT_LABELS: Partial<Record<QuietToolName, string>> = {
	grep: "matches",
	find: "files",
	ls: "entries",
};

const COLLAPSED_TAIL_LINE_LIMIT = 10;
const PREVIEW_LINE_LIMIT = 3;
/** A full bash card's output tail, as the Gentle Shell's (pi's) bash card. */
const BASH_TAIL_LINE_LIMIT = 5;

/**
 * T91 (L650): tool cards are full by default, as the Gentle Shell's: bash shows
 * its whole command, its output tail and how long it took. shell.appearance.density
 * "minimal" keeps the quiet cards (bash collapsed to `↳ ✓ N lines` and a short
 * preview), as G's "Card content: minimal" does.
 */
export function quietCards(): boolean {
	try { return shellSettings().density() === "minimal"; } catch { return false; }
}

/** pi's duration wording: `0.4s`, `2m 5s`, `1h 2m 5s`. */
export function formatDuration(ms: number): string {
	const seconds = Math.max(0, ms) / 1000;
	if (seconds < 60) return `${seconds.toFixed(1)}s`;
	const total = Math.floor(seconds);
	const minutes = Math.floor(total / 60);
	if (minutes < 60) return `${minutes}m ${total % 60}s`;
	return `${Math.floor(minutes / 60)}h ${minutes % 60}m ${total % 60}s`;
}

const TIMER = Symbol.for("jhon-shell.tool-timer");
type Timer = { startedAt?: number; endedAt?: number };

function timerOf(state: unknown): Timer | undefined {
	if (state === null || typeof state !== "object") return undefined;
	const slot = state as Record<symbol, Timer | undefined>;
	return (slot[TIMER] ??= {});
}

/** Starts the row's clock once execution starts; from renderCall. */
function startTimer(context: ToolRenderContextLike | undefined): void {
	const timer = timerOf(context?.state);
	if (timer && context?.executionStarted && timer.startedAt === undefined) timer.startedAt = Date.now();
}

/** `Took 0.4s` for a finished row: the recorded duration first, else the row's own clock. */
function tookText(context: ToolRenderContextLike | undefined): string | undefined {
	if (typeof context?.durationMs === "number") return `Took ${formatDuration(context.durationMs)}`;
	const timer = timerOf(context?.state);
	if (timer?.startedAt === undefined) return undefined;
	timer.endedAt ??= Date.now();
	return `Took ${formatDuration(timer.endedAt - timer.startedAt)}`;
}

const EMPTY_RESULT_MESSAGES: Partial<Record<QuietToolName, string[]>> = {
	grep: ["No matches found"],
	find: ["No files found matching pattern"],
	ls: ["Directory is empty", "(empty directory)"],
	bash: ["(no output)"],
};

const toolCache = new Map<string, Record<RegisteredToolName, any>>();

function createBuiltInTools(cwd: string): Record<RegisteredToolName, any> {
	return Object.fromEntries(
		(Object.entries(TOOL_CREATORS) as [RegisteredToolName, (cwd: string) => any][]).map(
			([name, createTool]) => [name, createTool(cwd)],
		),
	) as Record<RegisteredToolName, any>;
}

function getBuiltInTools(cwd: string): Record<RegisteredToolName, any> {
	let tools = toolCache.get(cwd);
	if (!tools) {
		tools = createBuiltInTools(cwd);
		toolCache.set(cwd, tools);
	}
	return tools;
}

function shortenPath(path: unknown): string {
	if (typeof path !== "string" || path.length === 0) return "";
	const home = homedir();
	return path.startsWith(home) ? `~${path.slice(home.length)}` : path;
}

function asString(value: unknown, fallback = ""): string {
	return typeof value === "string" && value.length > 0 ? value : fallback;
}

export function countNonEmptyLines(text: string): number {
	return text.split("\n").filter((line) => line.trim().length > 0).length;
}

export function tailLines(text: string, limit: number): string {
	const lines = text.split("\n");
	return lines.slice(Math.max(0, lines.length - limit)).join("\n");
}

function outputLines(text: string): string[] {
	const normalized = text.replace(/\r\n/g, "\n").replace(/\n$/, "");
	return normalized.length > 0 ? normalized.split("\n") : [];
}

function firstLines(text: string, limit: number): string {
	return outputLines(text).slice(0, limit).join("\n");
}

function lastOutputLines(text: string, limit: number): string {
	return outputLines(text).slice(-limit).join("\n");
}

function lastMeaningfulOutputLines(text: string, limit: number): string {
	return outputLines(text)
		.filter((line) => line.trim().length > 0)
		.slice(-limit)
		.join("\n");
}

function semanticJsonPreview(text: string): string | undefined {
	const trimmed = text.trim();
	if (!/^[\[{]/.test(trimmed)) return undefined;
	let parsed: unknown;
	try {
		parsed = JSON.parse(trimmed);
	} catch {
		return undefined;
	}
	const normalized = JSON.stringify(parsed, null, 2);
	if (normalized === undefined) return undefined;
	return outputLines(normalized)
		.filter((line) => !/^[\s{}\[\],]*$/.test(line))
		.slice(0, PREVIEW_LINE_LIMIT)
		.join("\n");
}

export function extractTextContent(result: AgentToolResult<unknown>): string {
	return result.content
		.flatMap((content) => (content.type === "text" ? [content.text] : []))
		.join("\n");
}

function safeText(value: string): string {
	return sanitizeTerminalText(value);
}

function sanitizeValue(value: unknown): unknown {
	if (typeof value === "string") return safeText(value);
	return value;
}

function sanitizedArgs(args: Record<string, unknown> | undefined): Record<string, unknown> {
	return (sanitizeValue(args ?? {}) as Record<string, unknown>) ?? {};
}

function sanitizedResult(result: AgentToolResult<unknown>): AgentToolResult<unknown> {
	return {
		...result,
		content: result.content.map((content) => content.type === "text" ? { ...content, text: safeText(content.text) } : content.type === "image" ? { ...content, mimeType: safeText(content.mimeType) } : content),
		details: sanitizeValue(result.details),
	};
}

function isEmptyResultMessage(toolName: QuietToolName, text: string): boolean {
	const normalized = text.trim();
	return EMPTY_RESULT_MESSAGES[toolName]?.some((message) => normalized === message) ?? false;
}

function grepMatchCount(text: string, args: Record<string, unknown> | undefined): number {
	const context = args?.context;
	if (typeof context !== "number" || context <= 0) return countNonEmptyLines(text);
	return outputLines(text).filter((line) => /^\s*.+:\d+:\s?/.test(line)).length;
}

function isGitCommand(args: Record<string, unknown> | undefined): boolean {
	const command = typeof args?.command === "string" ? args.command.trim() : "";
	return /^(?:env\s+\S+=\S+\s+|command\s+|\w+=\S+\s+)*git(?:\s|$)/.test(command);
}

interface ToolResultFormatOptions {
	expanded: boolean;
	isError?: boolean;
	args?: Record<string, unknown>;
}

function detailsRecord(result: AgentToolResult<unknown>): Record<string, unknown> {
	const details = sanitizeValue(result.details);
	return details && typeof details === "object" && !Array.isArray(details)
		? details as Record<string, unknown>
		: {};
}

function diffStats(diff: string): { additions: number; removals: number } {
	let additions = 0;
	let removals = 0;
	for (const line of diff.split("\n")) {
		if (line.startsWith("+") && !line.startsWith("+++")) additions++;
		if (line.startsWith("-") && !line.startsWith("---")) removals++;
	}
	return { additions, removals };
}

function editSummary(result: AgentToolResult<unknown>): string {
	const diff = detailsRecord(result).diff;
	if (typeof diff === "string") {
		const stats = diffStats(diff);
		return `✓ +${stats.additions} / -${stats.removals}`;
	}
	return "✓ applied";
}

function writeSummary(text: string): string {
	const bytes = text.match(/(?:Successfully )?wrote\s+(\d+)\s+bytes/i)?.[1];
	return `✓ ${bytes ? `wrote ${bytes} bytes` : "written"}`;
}

function expandedResultText(toolName: QuietToolName, result: AgentToolResult<unknown>, text: string): string {
	if (toolName === "edit") {
		const diff = detailsRecord(result).diff;
		if (typeof diff === "string") return safeText(diff);
	}
	return text;
}

export function formatToolResultOutput(
	toolName: QuietToolName,
	result: AgentToolResult<unknown>,
	{ expanded, isError = false, args }: ToolResultFormatOptions,
): string {
	const text = safeText(extractTextContent(result));
	if (expanded) {
		// Write results only acknowledge the operation; the body lives in the call args.
		const detail = toolName === "write" && !isError && typeof args?.content === "string" && args.content.length > 0
			? safeText(args.content)
			: expandedResultText(toolName, result, text);
		return detail ? `\n${detail}` : "";
	}
	if (isError) {
		const tail = lastMeaningfulOutputLines(text, PREVIEW_LINE_LIMIT);
		return tail ? `\n${tail}` : "";
	}
	const summaryLabel = COLLAPSED_COUNT_LABELS[toolName];
	if (summaryLabel) {
		if (isEmptyResultMessage(toolName, text)) return "";
		const count = toolName === "grep" ? grepMatchCount(text, args) : countNonEmptyLines(text);
		return count > 0 ? ` → ${count} ${summaryLabel}` : "";
	}
	if (toolName === "bash" && isGitCommand(args)) {
		const tail = tailLines(text, COLLAPSED_TAIL_LINE_LIMIT);
		return tail ? `\n${tail}` : "";
	}
	if (toolName === "read") {
		const head = firstLines(text, PREVIEW_LINE_LIMIT);
		return head ? `\n${head}` : "";
	}
	if (toolName === "bash") {
		const preview = semanticJsonPreview(text);
		const tail = preview ?? lastOutputLines(text, PREVIEW_LINE_LIMIT);
		return tail ? `\n${tail}` : "";
	}
	if (toolName === "edit") return `\n${editSummary(result)}`;
	if (toolName === "write") return `\n${writeSummary(text)}`;
	return "";
}

function lineRangeSuffix(args: Record<string, unknown>, theme: ThemeLike): string {
	if (args.offset === undefined && args.limit === undefined) return "";
	const startLine = typeof args.offset === "number" ? args.offset : 1;
	const endLine = typeof args.limit === "number" ? startLine + args.limit - 1 : undefined;
	return theme.fg("warning", `:${startLine}${endLine === undefined ? "" : `-${endLine}`}`);
}

interface ToolRenderContextLike {
	args?: Record<string, unknown>;
	argsComplete?: boolean;
	executionStarted?: boolean;
	isPartial?: boolean;
	isError?: boolean;
	lastComponent?: unknown;
	state?: unknown;
	cwd?: string;
	expanded?: boolean;
	toolCallId?: string;
	durationMs?: number;
}

function formatToolCall(toolName: QuietToolName, args: Record<string, unknown>, theme: ThemeLike): string {
	switch (toolName) {
		case "read": {
			const path = safeText(shortenPath(args.path) || "...");
			return `${theme.fg("toolTitle", theme.bold("read"))} ${theme.fg("accent", path)}${lineRangeSuffix(args, theme)}`;
		}
		case "bash": {
			const command = safeText(asString(args.command, "..."));
			const timeout = typeof args.timeout === "number" ? theme.fg("muted", ` (timeout ${args.timeout}s)`) : "";
			// The name in bold, then the command (T33).
			return `${theme.fg("toolTitle", theme.bold("bash"))} ${theme.fg("accent", command)}${timeout}`;
		}
		case "grep": {
			let text = `${theme.fg("toolTitle", theme.bold("grep"))} ${theme.fg("accent", `/${safeText(asString(args.pattern))}/`)} in ${safeText(shortenPath(args.path) || ".")}`;
			if (typeof args.glob === "string") text += theme.fg("toolOutput", ` (${safeText(args.glob)})`);
			if (typeof args.limit === "number") text += theme.fg("toolOutput", ` limit ${args.limit}`);
			return text;
		}
		case "find": {
			let text = `${theme.fg("toolTitle", theme.bold("find"))} ${theme.fg("accent", safeText(asString(args.pattern, "*")))} in ${safeText(shortenPath(args.path) || ".")}`;
			if (typeof args.limit === "number") text += theme.fg("toolOutput", ` limit ${args.limit}`);
			return text;
		}
		case "ls": {
			let text = `${theme.fg("toolTitle", theme.bold("ls"))} ${theme.fg("accent", safeText(shortenPath(args.path) || "."))}`;
			if (typeof args.limit === "number") text += theme.fg("toolOutput", ` limit ${args.limit}`);
			return text;
		}
		case "edit":
			return `${theme.fg("toolTitle", theme.bold("edit"))} ${theme.fg("accent", safeText(shortenPath(args.path) || "..."))}`;
		case "write": {
			const content = typeof args.content === "string" ? args.content : "";
			const lineInfo = content.length > 0 ? theme.fg("muted", ` (${content.split("\n").length} lines)`) : "";
			return `${theme.fg("toolTitle", theme.bold("write"))} ${theme.fg("accent", safeText(shortenPath(args.path) || "..."))}${lineInfo}`;
		}
	}
}

interface BoundedRowSection {
	text: string;
	rows: number;
	tail?: boolean;
}

// Cache the rendered preview slice per stable tool-result object: pi re-renders
// every visible card each frame, so re-tokenizing the full output text per pass is
// pure waste. Only the returned preview slice is retained, never the full wrapped
// text, so an arbitrarily large output cannot pin every wrapped line for the
// result object's lifetime.
const boundedRowsLineCache = new WeakMap<object, Array<{ text: string; width: number; rows: number; tail: boolean; lines: string[] } | undefined>>();

class BoundedRows implements Component {
	private readonly sections: readonly BoundedRowSection[];
	private readonly cacheKey: object | undefined;

	constructor(sections: readonly BoundedRowSection[], cacheKey?: object) {
		this.sections = sections;
		this.cacheKey = cacheKey;
	}

	/** Renders each section through the wrapped-line cache, sliced to the section row budget; cache hits skip re-tokenizing and re-wrapping the section text. */
	render(width: number): string[] {
		const cache = this.cacheKey ? boundedRowsLineCache.get(this.cacheKey) : undefined;
		return this.sections.flatMap(({ text, rows, tail = false }, index) => {
			if (rows <= 0) return [];
			const hit = cache?.[index];
			if (hit && hit.text === text && hit.width === width && hit.rows === rows && hit.tail === tail) {
				return [...hit.lines];
			}
			const rendered = new Text(text, 0, 0).render(width);
			const sliced = tail ? rendered.slice(-rows) : rendered.slice(0, rows);
			if (this.cacheKey) {
				const slot = boundedRowsLineCache.get(this.cacheKey) ?? [];
				slot[index] = { text, width, rows, tail, lines: sliced };
				boundedRowsLineCache.set(this.cacheKey, slot);
			}
			return [...sliced];
		});
	}

	invalidate(): void {}
}

// Tool card: every quiet tool call is a rounded card, so the call and its output read as one block apart from assistant
// prose. pi does not paint tool backgrounds for renderShell "self", so the
// frame tone carries the status the painted Box would otherwise show. The call
// owns the top rule; the result draws the sides and always closes the frame.
// Until the first result exists, the call closes the frame itself.
type ToolTone = typeof CARD_TONE.WARNING | typeof CARD_TONE.SUCCESS | typeof CARD_TONE.ERROR;

function toolTone(pending: boolean, failed: boolean): ToolTone {
	if (failed) return CARD_TONE.ERROR;
	return pending ? CARD_TONE.WARNING : CARD_TONE.SUCCESS;
}

/** One settled frame per component: bounded memory, including all card chrome. */
class ToolCardRows {
	private frame?: { width: number; style: ReturnType<typeof cardStyle>; running: boolean; lines: string[] };

	render(width: number, running: boolean, build: () => string[]): string[] {
		const style = cardStyle();
		if (this.frame?.width === width && this.frame.style === style && this.frame.running === running) return this.frame.lines;
		const lines = build();
		this.frame = { width, style, running, lines };
		return lines;
	}

	invalidate(): void { this.frame = undefined; }
}

class ToolCardTop implements Component {
	private readonly rows = new ToolCardRows();
	private readonly header: () => string;
	private readonly glyph: string;
	private readonly tone: ToolTone;
	private readonly theme: CardTheme;
	private readonly hint: string | undefined;
	private readonly row: CardRowContext | undefined;

	constructor(header: () => string, tone: ToolTone, theme: CardTheme, glyph: string, hint?: string, row?: CardRowContext) {
		this.header = header;
		this.glyph = glyph;
		this.tone = tone;
		this.theme = theme;
		this.hint = hint;
		this.row = row;
	}

	/** Renders `╭─ <icon> <call> ── <hint> ╮`, reserving applicable hint space before wrapping the call. A call that does not fit the rule (a long or multi-line command) continues on card rows below it, so every line of the command stays visible. */
	render(width: number): string[] {
		const target = Math.max(0, Math.floor(width));
		if (target === 0) return [];
		const running = this.row !== undefined && cardAwaitingResult(this.row);
		return this.rows.render(target, running, () => drawCard(this.tone, this.theme, target, (frame, inner) => ({
			head: frame.topRows({ title: this.header(), glyph: this.glyph }, inner, this.hint),
			body: running ? [frame.running(inner)] : undefined,
			bottom: running ? frame.close(inner) : undefined,
		})));
	}

	invalidate(): void { this.rows.invalidate(); }
}

class ToolCardBody implements Component {
	private readonly rows = new ToolCardRows();
	private readonly inner: () => Component;
	private readonly tone: ToolTone;
	private readonly theme: CardTheme;

	private readonly cacheable: boolean;

	constructor(inner: () => Component, tone: ToolTone, theme: CardTheme, cacheable = true) {
		this.inner = inner;
		this.tone = tone;
		this.theme = theme;
		this.cacheable = cacheable;
	}

	/** Renders the inner component between the card sides and closes the frame, even when the result has no rows. */
	render(width: number): string[] {
		const target = Math.max(0, Math.floor(width));
		if (target === 0) return [];
		const build = () => drawCard(this.tone, this.theme, target, (frame, inner) => ({
			body: this.inner().render(cardInnerWidth(inner)).map((line) => frame.line(line.trimEnd(), inner)),
			bottom: frame.close(inner),
			afterHeading: true,
		}));
		return this.cacheable ? this.rows.render(target, false, build) : build();
	}

	invalidate(): void { this.rows.invalidate(); }
}

function shouldRenderPreviewTail(
	toolName: QuietToolName,
	text: string,
	isError: boolean,
	args: Record<string, unknown> | undefined,
): boolean {
	if (isError) return true;
	if (toolName !== "bash") return false;
	return isGitCommand(args) || semanticJsonPreview(text) === undefined;
}

function partialLabel(toolName: QuietToolName, text: string): string {
	const lineCount = countNonEmptyLines(text);
	return lineCount === 0
		? `… ${toolName}`
		: `… ${toolName} · ${lineCount} ${lineCount === 1 ? "line" : "lines"}`;
}

function hasImageContent(result: AgentToolResult<unknown>): boolean {
	return result.content.some((content) => content.type === "image");
}

function isRenderArgs(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function sanitizedRenderContext(context: (Omit<ToolRenderContextLike, "args"> & { args?: unknown }) | undefined): ToolRenderContextLike {
	if (!context) return { args: {} };
	return {
		...context,
		args: sanitizedArgs(isRenderArgs(context.args) ? context.args : undefined),
		cwd: typeof context.cwd === "string" ? safeText(context.cwd) : context.cwd,
	};
}

/** When each running command started, and whether its cancel takes a double Esc, by tool call id. */
const running = new Map<string, { started: number; doubleEsc: boolean }>();

/** A running bash call's label: its lines, how long it has run, and how to stop it. */
function runningLabel(toolName: QuietToolName, text: string, toolCallId: string | undefined, now = Date.now()): string {
	const label = partialLabel(toolName, text);
	if (toolName !== "bash" || !toolCallId) return label;
	let run = running.get(toolCallId);
	if (!run) {
		let doubleEsc = false;
		try { doubleEsc = loadDoubleEscCancelPolicy() === "on"; } catch { /* a missing preference is off */ }
		run = { started: now, doubleEsc };
		running.set(toolCallId, run);
	}
	return `${label} · ${runningLine(now - run.started, run.doubleEsc)}`;
}

/** A failed bash call's first rows: the exit code and counts, the first failure, and where it is. */
export function failureRows(text: string): string[] {
	const f = commandFailure(text);
	return [failureHeadline(f), f.first ? `✗ ${f.first}` : "", f.where ? `at ${f.where}` : ""].filter(Boolean);
}

const SINGULAR: Record<string, string> = { lines: "line", matches: "match", files: "file", entries: "entry" };

function counted(count: number, label: string): string {
	return `${count} ${count === 1 ? SINGULAR[label] ?? label : label}`;
}

/** A finished call's result in a few words, for the sections `↳ ✓` line (T33). */
function resultSummary(toolName: QuietToolName, result: AgentToolResult<unknown>, text: string, args: Record<string, unknown> | undefined): string {
	const label = COLLAPSED_COUNT_LABELS[toolName];
	if (label) {
		if (isEmptyResultMessage(toolName, text) || countNonEmptyLines(text) === 0) return toolName === "ls" ? "empty" : `no ${label}`;
		return counted(toolName === "grep" ? grepMatchCount(text, args) : countNonEmptyLines(text), label);
	}
	if (toolName === "edit") return editSummary(result).replace(/^✓ /, "");
	if (toolName === "write") return writeSummary(text).replace(/^✓ /, "");
	if (toolName === "read" && hasImageContent(result)) return "image";
	if (toolName === "bash" && (isEmptyResultMessage(toolName, text) || outputLines(text).length === 0)) return "no output";
	return counted(outputLines(text).length, "lines");
}

/** The rows under a collapsed sections `↳ ✓` line: the preview the float card shows, without its own summary. */
function sectionsPreview(toolName: QuietToolName, result: AgentToolResult<unknown>, text: string, args: Record<string, unknown> | undefined): string {
	if (COLLAPSED_COUNT_LABELS[toolName]) return isEmptyResultMessage(toolName, text) ? "" : firstLines(text, PREVIEW_LINE_LIMIT - 1);
	if (toolName === "edit") {
		const diff = detailsRecord(result).diff;
		return typeof diff === "string" ? firstLines(safeText(diff).split("\n").filter((line) => /^[+-]/.test(line)).join("\n"), PREVIEW_LINE_LIMIT - 1) : "";
	}
	if (toolName === "write" || (toolName === "bash" && isEmptyResultMessage(toolName, text))) return "";
	return formatToolResultOutput(toolName, result, { expanded: false, args }).replace(/^\n/, "");
}

/**
 * A finished result in the sections style (T33, T34): `↳ ✓ <summary>`, or
 * `↳ ✗ <why>` in the bad color, then the preview (or the whole output when
 * expanded). Minimal cards keep only the `↳` line while collapsed.
 */
function sectionsResult(
	toolName: QuietToolName, result: AgentToolResult<unknown>, text: string, isError: boolean, expanded: boolean,
	args: Record<string, unknown> | undefined, theme: ThemeLike, cacheKey: object | undefined,
): Component {
	if (isError) {
		const why = toolName === "bash" ? failureRows(text) : [];
		const meaningful = outputLines(text).filter((line) => line.trim().length > 0);
		const headline = why[0] ?? meaningful[0] ?? "failed";
		const reasons = why.slice(1).map((row) => ({ text: theme.fg(row.startsWith("at ") ? "accent" : "error", row), rows: 1 }));
		// Say it once (B2): a tail row that repeats a why row (its failure, its place) is dropped.
		const said = why.slice(1).map((row) => row.replace(/^(✗ |at )/, "").trim()).filter(Boolean);
		const fresh = (tail: string) => tail.split("\n").filter((line) => { const plain = stripAnsi(line).trim(); return !said.some((row) => plain.includes(row)); }).join("\n");
		// Expanded, the whole output follows, without the line the headline already says.
		const lines = text.split("\n");
		const saidAt = why.length ? -1 : lines.findIndex((line) => line.trim() === headline.trim());
		const rest = expanded
			? (saidAt >= 0 ? [...lines.slice(0, saidAt), ...lines.slice(saidAt + 1)].join("\n").replace(/^\n+/, "") : text)
			: (why.length ? fresh(lastMeaningfulOutputLines(text.replace(/\n?[ \t]*Command exited with code \d+[ \t]*$/, ""), PREVIEW_LINE_LIMIT)) : meaningful.slice(1).slice(-(PREVIEW_LINE_LIMIT - 1)).join("\n"));
		return new BoundedRows([
			{ text: theme.fg("error", `↳ ✗ ${headline}`), rows: 1 },
			...reasons,
			...(rest ? [{ text: theme.fg("muted", rest), rows: expanded ? Number.MAX_SAFE_INTEGER : PREVIEW_LINE_LIMIT, tail: true }] : []),
		], expanded ? undefined : cacheKey);
	}
	const head = `${theme.fg("dim", "↳")} ${theme.fg("success", "✓")} ${theme.fg("muted", resultSummary(toolName, result, text, args))}`;
	if (expanded) {
		const detail = formatToolResultOutput(toolName, result, { expanded: true, args }).replace(/^\n/, "");
		return new Text(detail ? `${head}\n${theme.fg("toolOutput", detail)}` : head, 0, 0);
	}
	const preview = sectionsPreview(toolName, result, text, args);
	return new BoundedRows([
		{ text: head, rows: 1 },
		...(preview ? [{ text: theme.fg("muted", preview), rows: PREVIEW_LINE_LIMIT, tail: shouldRenderPreviewTail(toolName, text, false, args) }] : []),
	], cacheKey);
}

/**
 * A full bash card's result (T91), the Gentle Shell's: a failure's `↳ ✗` why
 * first (Jhon's headline, first failure and place), then the output tail with
 * `... (N earlier lines, <expand key>)` above it, or all of it expanded, then
 * `Took <duration>`.
 */
function bashCardResult(text: string, isError: boolean, expanded: boolean, took: string | undefined, theme: ThemeLike, cacheKey: object | undefined): Component {
	const sections: BoundedRowSection[] = [];
	let output = text;
	if (isError) {
		const why = failureRows(text);
		const lines = outputLines(text);
		const headline = why[0] ?? lines.find((line) => line.trim().length > 0) ?? "failed";
		sections.push({ text: theme.fg("error", `↳ ✗ ${headline}`), rows: 1 });
		for (const row of why.slice(1)) sections.push({ text: theme.fg(row.startsWith("at ") ? "accent" : "error", row), rows: 1 });
		if (why.length) output = text.replace(/\n?\n?[ \t]*Command exited with code \d+[ \t]*$/, "");
		else {
			const at = lines.findIndex((line) => line.trim() === headline.trim());
			output = at >= 0 ? [...lines.slice(0, at), ...lines.slice(at + 1)].join("\n") : text;
		}
	}
	if (isEmptyResultMessage("bash", output)) output = "";
	const lines = outputLines(output.replace(/^\n+/, "").replace(/\s+$/, ""));
	if (lines.length > 0) {
		if (expanded) sections.push({ text: theme.fg("toolOutput", lines.join("\n")), rows: Number.MAX_SAFE_INTEGER });
		else {
			const hidden = lines.length - BASH_TAIL_LINE_LIMIT;
			if (hidden > 0) sections.push({ text: `${theme.fg("muted", `... (${hidden} earlier ${hidden === 1 ? "line" : "lines"},`)} ${stripAnsi(keyHint("app.tools.expand", "to expand"))}${theme.fg("muted", ")")}`, rows: 1 });
			sections.push({ text: theme.fg("toolOutput", lines.slice(-BASH_TAIL_LINE_LIMIT).join("\n")), rows: BASH_TAIL_LINE_LIMIT, tail: true });
		}
	}
	if (took) sections.push({ text: theme.fg("muted", took), rows: 1 });
	return new BoundedRows(sections, expanded ? undefined : cacheKey);
}

/** Rendering-only factory; the default export attaches the Bash renderers to native Bash through pi.registerToolRenderer, so execution and schema stay pi's. */
export function createQuietToolRenderer(
	toolName: QuietToolName,
	officialRenderResult?: ToolDefinition["renderResult"],
): Pick<ToolDefinition, "renderShell" | "renderCall" | "renderResult"> {

	return {
		renderShell: "self",
		renderCall: ownsIcon((args, theme, context) => {
			const callArgs = args as Record<string, unknown>;
			const renderContext = sanitizedRenderContext(context);
			startTimer(renderContext);
			const tone = toolTone(renderContext.isPartial !== false, renderContext.isError === true);
			// The expand key rides the top rule once the call finished.
			const finished = renderContext.isPartial === false && (renderContext.executionStarted === true || renderContext.isError === true);
			const hint = finished ? stripAnsi(keyHint("app.tools.expand", renderContext.expanded === true ? "to collapse" : "to expand")) : undefined;
			return new ToolCardTop(() => formatToolCall(toolName, callArgs, theme), tone, theme, toolIcon(toolName), hint, renderContext);
		}),
		/** Builds the card component for this render pass; collapsed cards delegate to the wrapped-line cache keyed by the tool result object. */
		renderResult(result, options, theme, context) {
			const renderContext = context ? sanitizedRenderContext(context) : undefined;
			markCardResult(renderContext?.state);
			const cacheKey = typeof result === "object" && result !== null ? result : undefined;
			const safeResult = sanitizedResult(result);
			const text = safeText(extractTextContent(safeResult));
			// Current Pi carries errors on the context. Older renderer callers
			// supplied them on options; keep that structural fallback for those hosts.
			const legacyError = "isError" in options && options.isError === true;
			const isError = renderContext?.isError ?? legacyError;
			const resultTone = toolTone(options.isPartial === true, isError);
			const carded = (component: () => Component, cacheable = options.isPartial !== true): Component => new ToolCardBody(component, resultTone, theme, cacheable);
			if (options.isPartial) {
				const label = runningLabel(toolName, text, renderContext?.toolCallId);
				if (options.expanded) return carded(() => new Text(`${theme.fg("warning", label)}\n${theme.fg("muted", text)}`, 0, 0));
				const visible = lastOutputLines(text, PREVIEW_LINE_LIMIT);
				return carded(() => new BoundedRows([
					{ text: theme.fg("warning", label), rows: 1 },
					...(visible ? [{ text: theme.fg("muted", visible), rows: PREVIEW_LINE_LIMIT, tail: true }] : []),
				], cacheKey));
			}
			if (renderContext?.toolCallId) running.delete(renderContext.toolCallId);
			if (options.expanded && toolName === "read" && hasImageContent(safeResult) && officialRenderResult) {
				return carded(() => officialRenderResult(
					safeResult,
					options,
					theme,
					sanitizedRenderContext(renderContext) as any,
				), false);
			}
			if (toolName === "bash" && !quietCards()) {
				const took = tookText(renderContext);
				return carded(() => bashCardResult(text, isError, options.expanded, took, theme, cacheKey), !options.expanded);
			}
			return carded(() => sectionsResult(toolName, safeResult, text, isError, options.expanded, renderContext?.args, theme, cacheKey), !options.expanded);
		},
	};
}

function registerQuietTool(pi: ExtensionAPI, toolName: RegisteredToolName): void {
	const registrationTool = getBuiltInTools(process.cwd())[toolName];
	pi.registerTool({
		...registrationTool,
		async execute(toolCallId, params, signal, onUpdate, ctx) {
			return getBuiltInTools(ctx.cwd)[toolName].execute(toolCallId, params, signal, onUpdate, ctx);
		},
		...createQuietToolRenderer(toolName, registrationTool.renderResult),
	});
}

/** Jhon's tool cards, always (L76: shell.behaviour.quiet_tools and its pi-pretty fallback are gone). */
export default function quietTools(pi: ExtensionAPI): ReturnType<ExtensionFactory> {
	for (const toolName of Object.keys(TOOL_CREATORS) as RegisteredToolName[]) registerQuietTool(pi, toolName);
	const host = pi as typeof pi & { registerToolRenderer?: (resolver: (toolName: string, next: () => unknown) => unknown) => void };
	if (typeof host.registerToolRenderer !== "function") return;
	// T33 (L97): every call starts with its family's icon. Registered first, so
	// it wraps what the resolvers after it and the tool itself would draw;
	// memory calls read as one line with a `↳ ✓` result.
	// T91 (L650): bash is a Jhon card too, its full command, output tail and
	// duration; pi keeps its execution and schema.
	const bashCard = createQuietToolRenderer("bash") as ToolRenderers;
	host.registerToolRenderer((toolName, next) => {
		if (toolName === "bash") return { ...(next() as ToolRenderers | undefined), ...bashCard };
		const renderers = next() as ToolRenderers | undefined;
		if (isMemoryTool(toolName) && renderers) return { ...renderers, renderShell: "default", ...memoryToolRenderers(toolName) };
		return withToolIcon(toolName, renderers);
	});
}
