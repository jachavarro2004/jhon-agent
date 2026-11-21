/**
 * shell/lib/ui/shell-prompt.ts — the prompt and the working line (design/shell-tui 01, T174-S8).
 *
 *    力 Working…  thinking · 38s                         esc esc interrupts
 *   ──────────────────────────────────────────────────────────────────────
 *    ❯ █ write, or / for a command
 *   ──────────────────────────────────────────────────────────────────────
 *
 * The working line sits over the rule in every state, so the prompt keeps its
 * height when a turn starts and ends: idle it reads `力 ready for you`, working
 * `力 Working…` with what the agent does and for how long, queued it adds
 * `queued`. Its right end says how esc stops the turn, or the open esc
 * confirmation. pi's editor draws the input (rendered PROMPT_INDENT cells
 * narrower); this file draws everything around it. Pure: rows in, rows out.
 */
import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import { stripAnsi } from "./primitives/fit.ts";
import { asciiText, glyph } from "./primitives/glyphs.ts";
import { elapsed } from "./panel.ts";

/** Where the session stands, as the working line shows it: at rest, at work, or at work with a message waiting. */
export type PromptState = "idle" | "working" | "queued";
export const PROMPT_STATE = { IDLE: "idle", WORKING: "working", QUEUED: "queued" } as const satisfies Record<string, PromptState>;

/** The brand on the working line; `*` under shell.appearance.ascii (L165). */
export const PROMPT_PETAL = "力";
const PETAL_ASCII = "*";
/** The input's mark (design 01); `>` in ASCII. */
export const PROMPT_MARK = "❯";
/** The placeholder of an empty prompt: B's wording, which the owner saw live and kept. */
export const PROMPT_HINT = "write, or / for a command";
export const IDLE_LABEL = "ready for you";
export const WORKING_WORD = "Working…";
const WORKING_WORD_ASCII = "Working...";
export const QUEUED_LABEL = "queued";
/** What the agent does when nothing more precise is known. */
export const DEFAULT_ACTIVITY = "thinking";
export const DOUBLE_ESC_CANCEL_HINT = "esc again stops it";
export const IDLE_ESC_CLEAR_HINT = "esc again clears it";
/** The cells before the input: a space, the mark, a space. */
export const PROMPT_INDENT = 3;

/** The working line's right end: how many esc presses stop the turn. */
export function interruptHint(doubleEsc: boolean): [keys: string, word: string] {
	return [doubleEsc ? "esc esc" : "esc", "interrupts"];
}

// --- motion -------------------------------------------------------------------------

/**
 * The motion's pace: one frame every 80 ms (shell.appearance.animations full;
 * off draws the line still and only its seconds move), and 15 frames for one
 * pass of the light across the working word.
 */
export const [SHELL_PULSE_MS, SHELL_SCANNER_STEPS] = [80, 15] as const;
/** The light starts this many cells before the word, so each pass enters from the left. */
const SCAN_LEAD = 3;
/** The light's roles by distance from its centre; anything farther is muted. */
const SCAN_RAMP = ["borderAccent", "accent", "thinkingHigh"] as const;

/** `n` wrapped into 0..length-1, a negative `n` too. */
const cycle = (n: number, length: number): number => ((n % length) + length) % length;

/** Paints text in a theme role. */
type Painter = (role: string, text: string) => string;

/** A band of light crossing the text, one cell per frame; the text itself unchanged. */
export function scanWorkingText(text: string, tick: number, paint: Painter): string {
	const centre = cycle(Math.floor(tick), SHELL_SCANNER_STEPS) - SCAN_LEAD;
	let out = "";
	let index = 0;
	for (const char of text) {
		out += paint(SCAN_RAMP[Math.abs(centre - index)] ?? "muted", char);
		index += 1;
	}
	return out;
}

/** The petal's tone: the rose ramp while working (one step per frame), warning while a message waits, bright at rest. */
const PETAL_RAMP = ["mdQuoteBorder", "thinkingHigh", "accent", "borderAccent", "accent", "thinkingHigh", "mdQuoteBorder", "mdQuoteBorder"] as const;
export type PetalTone = (typeof PETAL_RAMP)[number] | "warning";

export const petalTone = (state: PromptState, tick: number): PetalTone =>
	state === "queued" ? "warning" : state === "working" ? PETAL_RAMP[cycle(tick, PETAL_RAMP.length)]! : "borderAccent";

// --- the working line ---------------------------------------------------------------

export interface PromptTheme {
	fg(role: string, text: string): string;
	bold?(text: string): string;
	italic?(text: string): string;
}

export interface WorkingLineInput {
	state: PromptState;
	tick: number;
	/** shell.appearance.animations: the light crosses the word and the petal walks its ramp. */
	animate?: boolean;
	/** Since the turn began. */
	elapsedMs?: number;
	/** What the agent does: thinking, writing, a tool's name. */
	activity?: string;
	/** In place of `activity · time` (lib/ui/api-wait.ts: `waiting for Claude API · 72s`). */
	wait?: string;
	/** The open esc confirmation, in place of the interrupt hint. */
	escHint?: string;
	/** shell.behaviour.double_esc_cancel: the interrupt hint reads `esc esc`. */
	doubleEsc?: boolean;
	ascii?: boolean;
}

type Part = readonly [text: string, paint: (text: string) => string];

function partsWidth(parts: readonly Part[]): number {
	return parts.reduce((sum, [text]) => sum + visibleWidth(text), 0);
}

function paintParts(parts: readonly Part[]): string {
	return parts.map(([text, paint]) => paint(text)).join("");
}

/** The working line, exactly width cells: the right end gives way first, then the words are cut. */
export function renderWorkingLine(width: number, input: WorkingLineInput, theme: PromptTheme): string {
	width = Math.max(0, Math.floor(width));
	if (width === 0) return "";
	const ascii = input.ascii ?? false;
	const fold = (text: string) => (ascii ? asciiText(text) : text);
	const bold = (text: string) => (theme.bold ? theme.bold(text) : text);
	const italic = (text: string) => (theme.italic ? theme.italic(text) : text);
	const busy = input.state !== PROMPT_STATE.IDLE;
	const tick = input.animate ? input.tick : 3;
	const petal = ascii ? PETAL_ASCII : PROMPT_PETAL;
	const left: Part[] = [[" ", (t) => t], [petal, (t) => theme.fg(input.animate || !busy ? petalTone(input.state, tick) : input.state === PROMPT_STATE.QUEUED ? "warning" : "accent", bold(t))]];
	if (!busy) {
		left.push([` ${IDLE_LABEL}`, (t) => theme.fg("muted", t)]);
	} else {
		const word = ascii ? WORKING_WORD_ASCII : WORKING_WORD;
		left.push([" ", (t) => t], [word, (t) => italic(input.animate ? scanWorkingText(t, input.tick, theme.fg) : theme.fg("accent", t))]);
		if (input.wait) left.push([`  ${fold(input.wait)}`, (t) => theme.fg("dim", t)]);
		else {
			left.push([`  ${fold(`${input.activity || DEFAULT_ACTIVITY} · `)}`, (t) => theme.fg("dim", t)]);
			left.push([elapsed(input.elapsedMs ?? 0), (t) => theme.fg("muted", t)]);
		}
		if (input.state === PROMPT_STATE.QUEUED) left.push([fold(" · "), (t) => theme.fg("dim", t)], [QUEUED_LABEL, (t) => theme.fg("warning", t)]);
	}
	const right: Part[] = input.escHint
		? [[input.escHint, (t) => theme.fg("muted", t)]]
		: busy
			? (([keys, word]) => [[keys, (t: string) => theme.fg("muted", t)], [` ${word}`, (t: string) => theme.fg("dim", t)]] as Part[])(interruptHint(input.doubleEsc ?? false))
			: [];
	const leftWidth = partsWidth(left);
	const rightWidth = right.length ? partsWidth(right) + 1 : 0;
	if (right.length && leftWidth + 2 + rightWidth <= width) {
		return `${paintParts(left)}${" ".repeat(width - leftWidth - rightWidth)}${paintParts(right)} `;
	}
	// Painted text is measured by its parts, never by the painted string.
	if (leftWidth <= width) return paintParts(left) + " ".repeat(width - leftWidth);
	const cut = truncateToWidth(paintParts(left), width, "");
	return cut + " ".repeat(Math.max(0, width - visibleWidth(cut)));
}

// --- the prompt around pi's editor --------------------------------------------------

/** pi's scroll indicator in one of its rules (`↑ 2 more`), if any. */
function scrollNote(row: string | undefined): string | undefined {
	return row ? stripAnsi(row).match(/[↑↓] \d+ more/)?.[0] : undefined;
}

/** A rule of exactly width cells, with an optional note at its right end: `──── ↑ 2 more ─`. */
export function renderPromptRule(width: number, paint: (text: string) => string, note?: string, ascii = false): string {
	width = Math.max(0, Math.floor(width));
	const line = glyph("rule", ascii);
	const shown = note && ascii ? asciiText(note) : note;
	if (!shown || visibleWidth(shown) + 4 > width) return paint(line.repeat(width));
	return paint(`${line.repeat(width - visibleWidth(shown) - 3)} ${shown} ${line}`);
}

/** One input row: the mark (first row) or its indent, then pi's row, exactly width cells. */
export function promptInputRow(row: string, width: number, mark: string | undefined): string {
	const room = Math.max(0, width - PROMPT_INDENT);
	const content = truncateToWidth(row, room, "");
	const lead = mark === undefined ? " ".repeat(PROMPT_INDENT) : ` ${mark} `;
	return lead + content + " ".repeat(Math.max(0, room - visibleWidth(content)));
}

/** The placeholder after the cursor of an empty input row; the row unchanged when it does not fit. */
export function withPlaceholder(row: string, hint: string, paint: (text: string) => string): string {
	const body = row.replace(/ +$/, "");
	const room = visibleWidth(row) - visibleWidth(body);
	if (room < visibleWidth(hint) + 1) return row;
	return `${body} ${paint(hint)}${" ".repeat(room - visibleWidth(hint) - 1)}`;
}

export interface PromptFrameInput {
	/** The rows pi's editor drew at width - PROMPT_INDENT: its top rule, the input rows, its bottom rule, then any completion rows. */
	rows: readonly string[];
	/** How many input rows pi drew (its renderedVisibleLineCount). */
	inputRows: number;
	/** pi's live rule colour (thinking level, bash mode). */
	border: (text: string) => string;
	theme: PromptTheme;
	ascii?: boolean;
	/** The bottom rule, once drawn (the selection count rides it). */
	decorateBottom?: (rule: string) => string;
}

/** The rule, the ❯ rows, the rule and pi's completion rows, each exactly width cells. */
export function framePrompt(width: number, input: PromptFrameInput): string[] {
	width = Math.max(0, Math.floor(width));
	const { rows, theme } = input;
	const ascii = input.ascii ?? false;
	const count = Math.max(0, Math.min(input.inputRows, rows.length - 2));
	if (width < PROMPT_INDENT + 2) return rows.map((row) => truncateToWidth(row, width, ""));
	const mark = theme.fg("accent", theme.bold ? theme.bold(glyph("askCursor", ascii)) : glyph("askCursor", ascii));
	const bottom = renderPromptRule(width, input.border, scrollNote(rows[count + 1]), ascii);
	return [
		renderPromptRule(width, input.border, scrollNote(rows[0]), ascii),
		...rows.slice(1, count + 1).map((row, index) => promptInputRow(row, width, index === 0 ? mark : undefined)),
		input.decorateBottom ? input.decorateBottom(bottom) : bottom,
		...rows.slice(count + 2).map((row) => promptInputRow(row, width, undefined)),
	];
}

// --- what the agent does ------------------------------------------------------------

/** The working line's `thinking` / `writing` / a tool's name, from pi's events (extensions/ui.ts feeds it). */
export interface WorkingActivity {
	/** A turn began: thinking until the stream says otherwise. */
	start(): void;
	/** A stream event (pi's assistantMessageEvent.type). */
	stream(type: string | undefined): void;
	toolStart(id: string, name: string): void;
	toolEnd(id: string): void;
	current(): string;
}

export function createWorkingActivity(): WorkingActivity {
	let streaming = DEFAULT_ACTIVITY;
	const tools = new Map<string, string>();
	return {
		start() { streaming = DEFAULT_ACTIVITY; tools.clear(); },
		stream(type) {
			if (type?.startsWith("thinking")) streaming = DEFAULT_ACTIVITY;
			else if (type?.startsWith("text")) streaming = "writing";
		},
		toolStart(id, name) { tools.delete(id); tools.set(id, name || "tool"); },
		toolEnd(id) { tools.delete(id); if (tools.size === 0) streaming = DEFAULT_ACTIVITY; },
		// The tool started last that still runs; else what the stream says.
		current: () => [...tools.values()].at(-1) ?? streaming,
	};
}
