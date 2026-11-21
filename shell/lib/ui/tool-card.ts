/**
 * shell/lib/ui/tool-card.ts — a tool call's card (design/shell-tui 01, T174-S6).
 *
 * A card is a header (the tool's icon, its title, and at the right its state:
 * `✓ 2.3s`, `+3 −1`, `↳ 61 lines`) and body rows under it, one cell in from
 * each side of the conversation. shell.appearance.cards picks its form:
 *
 *   float  (the default) the tinted block (primitives/block.ts): no outline,
 *          the rows on the Box surface, an error's header on errorContainer;
 *   neon   design 02's outline: a rounded frame, the title and the state in
 *          its top edge, the frame in the bad role on an error.
 *
 * pi draws a tool row as two components, the call and then its result, so
 * the header and the body render apart: headRows draws the header (and the
 * call's own overflow rows), bodyRows the result's rows, and the outline's
 * closing edge goes with whichever component ends the card.
 *
 * Also here: the numbered diff (pi's edit diff as `86 − text` rows), the
 * guard refusal read from a tool error's text, and the split card a tool's
 * own renderers draw with (the Prism review and delegation cards, T174-S7).
 */
import { keyHint } from "@earendil-works/pi-coding-agent";
import { visibleWidth, wrapTextWithAnsi, type Component } from "@earendil-works/pi-tui";
import { shellSettings } from "../settings.ts";
import { renderTintedBlock, renderTintedBody, tintedBodyRoom, tintedTitleRoom, type BlockTheme } from "./primitives/block.ts";
import { CARD_STYLE, cardStyle } from "./primitives/card.ts";
import { clip, fit, stripAnsi } from "./primitives/fit.ts";
import { glyph } from "./primitives/glyphs.ts";

export type CardForm = "tint" | "outline";

export interface CardLook {
	form: CardForm;
	ascii: boolean;
	theme: BlockTheme;
}

export interface CardHead {
	/** The icon, already in its ASCII form when ascii is on. */
	icon: string;
	/** The title, painted. */
	title: string;
	/** The state at the right, painted; it drops before the title gets under twelve cells. */
	right?: string;
	tone?: "normal" | "error";
}

/** The blank cell on each side of a card, when the width allows it. */
const MARGIN = 1;
// Outline cells: `╭─ ` before the icon, ` ─╮` (and one fill cell) after the title; `│ ` and ` │` around a body row.
const OUTLINE_LEAD = 3;
const OUTLINE_TAIL = 4;
const OUTLINE_SIDES = 4;
const MIN_TITLE = 12;

function blockWidth(width: number): number {
	const cells = Math.max(0, Math.floor(width));
	return cells > MARGIN * 2 + 2 ? cells - MARGIN * 2 : cells;
}

function withMargins(rows: readonly string[], width: number): string[] {
	const cells = Math.max(0, Math.floor(width));
	if (blockWidth(cells) === cells) return rows.map((row) => fit(row, cells, { ellipsis: "" }));
	const margin = " ".repeat(MARGIN);
	return rows.map((row) => `${margin}${row}${margin}`);
}

function edgeRole(head: Pick<CardHead, "tone">): string {
	return head.tone === "error" ? "error" : "border";
}

/** The outline's title cells at block width b: the right part drops before the title gets under twelve. */
function outlineTitleRoom(b: number, right?: string): number {
	const plain = b - OUTLINE_LEAD - OUTLINE_TAIL;
	if (!right) return plain;
	const room = plain - visibleWidth(right) - 2;
	return room >= MIN_TITLE ? room : plain;
}

/** The cells the title text gets after the icon and its space, at width: where a long call wraps. */
export function titleCells(width: number, look: CardLook, head: Pick<CardHead, "icon" | "right">): number {
	const b = blockWidth(width);
	const room = look.form === "tint" ? tintedTitleRoom(b, head.right) : outlineTitleRoom(b, head.right);
	return Math.max(1, room - visibleWidth(head.icon) - 1);
}

/** The cells a body row's text gets at width. */
export function bodyCells(width: number, look: CardLook): number {
	const b = blockWidth(width);
	return Math.max(1, look.form === "tint" ? tintedBodyRoom(b) : b - OUTLINE_SIDES);
}

function outlineTop(head: CardHead, b: number, look: CardLook): string {
	const { theme, ascii } = look;
	const edge = (text: string) => (text ? theme.fg(edgeRole(head), text) : "");
	if (b < OUTLINE_LEAD + OUTLINE_TAIL + 1) return edge(fit(`${glyph("topLeft", ascii)}${glyph("rule", ascii).repeat(b)}`, b, { ellipsis: "" }));
	const bold = (text: string) => (theme.bold ? theme.bold(text) : text);
	const room = outlineTitleRoom(b, head.right);
	const withRight = !!head.right && room < b - OUTLINE_LEAD - OUTLINE_TAIL;
	const shown = clip(`${bold(theme.fg(head.tone === "error" ? "error" : "accent", head.icon))} ${head.title}`, room, { ascii });
	const right = withRight ? ` ${head.right} ` : "";
	const fill = b - OUTLINE_LEAD - visibleWidth(shown) - 1 - visibleWidth(right) - 2;
	const rule = glyph("rule", ascii);
	return `${edge(`${glyph("topLeft", ascii)}${rule} `)}${shown}${edge(` ${rule.repeat(Math.max(0, fill))}`)}${right}${edge(`${rule}${glyph("topRight", ascii)}`)}`;
}

function outlineSide(text: string, b: number, head: Pick<CardHead, "tone">, look: CardLook): string {
	const { theme, ascii } = look;
	const side = theme.fg(edgeRole(head), glyph("side", ascii));
	if (b < OUTLINE_SIDES + 1) return fit(side, b, { ellipsis: "" });
	return `${side} ${fit(text, b - OUTLINE_SIDES, { ascii })} ${side}`;
}

function outlineBottom(b: number, head: Pick<CardHead, "tone">, look: CardLook): string {
	const { theme, ascii } = look;
	if (b < 2) return theme.fg(edgeRole(head), glyph("bottomLeft", ascii).slice(0, b));
	return theme.fg(edgeRole(head), `${glyph("bottomLeft", ascii)}${glyph("rule", ascii).repeat(b - 2)}${glyph("bottomRight", ascii)}`);
}

/**
 * The header row, then `overflow` (the call's own rows that did not fit the
 * header) as body rows; `close` ends an outline here (a call with no result
 * yet). Every row is exactly width cells.
 */
export function headRows(head: CardHead, overflow: readonly string[], width: number, look: CardLook, close = false): string[] {
	const cells = Math.max(0, Math.floor(width));
	if (cells === 0) return [];
	const b = blockWidth(cells);
	if (look.form === "tint") {
		return withMargins(renderTintedBlock({ width: b, icon: head.icon, title: head.title, right: head.right, body: overflow, tone: head.tone, theme: look.theme, ascii: look.ascii }), cells);
	}
	return withMargins([outlineTop(head, b, look), ...overflow.map((row) => outlineSide(row, b, head, look)), ...(close ? [outlineBottom(b, head, look)] : [])], cells);
}

/** Body rows under a header drawn apart; `close` ends an outline (the result closes the card). */
export function bodyRows(rows: readonly string[], width: number, look: CardLook, tone: CardHead["tone"] = "normal", close = true): string[] {
	const cells = Math.max(0, Math.floor(width));
	if (cells === 0) return [];
	const b = blockWidth(cells);
	if (look.form === "tint") return withMargins(renderTintedBody(rows, b, look.theme, look.ascii), cells);
	return withMargins([...rows.map((row) => outlineSide(row, b, { tone }, look)), ...(close ? [outlineBottom(b, { tone }, look)] : [])], cells);
}

/** Text wrapped to cells, every line kept; an empty line stays one empty row. */
export function wrapRows(text: string, cells: number): string[] {
	return text.split("\n").flatMap((line) => (line === "" ? [""] : wrapTextWithAnsi(line, Math.max(1, cells))));
}

// --- the numbered diff ---------------------------------------------------------------

export interface DiffRow {
	kind: "add" | "remove" | "context" | "gap" | "other";
	/** The line number as pi wrote it, trimmed; "" when the row has none. */
	number: string;
	text: string;
}

// pi's edit diff (core/tools/edit-diff.js): `+12 text`, `-12 text`, ` 12 text`, and ` <pad> ...` for skipped context.
const NUMBERED = /^([+\- ])( *)(\d+) (.*)$/;
const GAP = /^ +\.\.\.$/;

/** A diff's rows: pi's numbered lines, or a plain unified diff's `+`/`-` lines without numbers. */
export function diffRows(diff: string): DiffRow[] {
	const rows: DiffRow[] = [];
	for (const line of diff.replace(/\r\n/g, "\n").replace(/\n$/, "").split("\n")) {
		if (line.startsWith("+++") || line.startsWith("---")) continue;
		if (GAP.test(line)) { rows.push({ kind: "gap", number: "", text: "..." }); continue; }
		const numbered = NUMBERED.exec(line);
		const sign = line[0];
		const kind = sign === "+" ? "add" : sign === "-" ? "remove" : sign === " " ? "context" : "other";
		if (numbered) rows.push({ kind, number: numbered[3]!, text: numbered[4]! });
		else if (kind === "other") rows.push({ kind, number: "", text: line });
		else rows.push({ kind, number: "", text: line.slice(1) });
	}
	return rows;
}

/** `+a` and `−r`: the lines a diff adds and removes. */
export function diffCounts(diff: string): { added: number; removed: number } {
	let added = 0;
	let removed = 0;
	for (const row of diffRows(diff)) {
		if (row.kind === "add") added++;
		if (row.kind === "remove") removed++;
	}
	return { added, removed };
}

/**
 * The numbered diff's rows, painted: ` 86   text` for context, ` 87 − text`
 * in the error role and ` 87 + text` in the ok role; the number column as
 * wide as the widest number.
 */
export function paintDiff(rows: readonly DiffRow[], theme: { fg(role: string, text: string): string }, ascii = false): string[] {
	const digits = Math.max(0, ...rows.map((row) => row.number.length));
	const minus = ascii ? "-" : "−";
	return rows.map((row) => {
		const number = row.number.padStart(digits, " ");
		if (row.kind === "add") return theme.fg("success", ` ${number} + ${row.text}`);
		if (row.kind === "remove") return theme.fg("error", ` ${number} ${minus} ${row.text}`);
		if (row.kind === "other") return theme.fg("dim", row.text);
		return theme.fg("muted", ` ${number}   ${row.text}`);
	});
}

// --- a guard's refusal ----------------------------------------------------------------

export interface GuardRefusal {
	/** Who refused, in words: `safety guard`, `writer guard`, `sandbox`, `permission`, `jhon`. */
	guard: string;
	/** What was refused and why. */
	reason: string;
	/** What to do instead; "" when the refusal names nothing. */
	fix: string;
	/** Any further lines (a session note). */
	more: string[];
}

// The engine's guards print `<name>-guard: <reason>` (engine/internal/hook);
// the shell's own refusals start `jhon: ` (lib/guards.ts, lib/sandbox.ts,
// lib/writer-guard.ts), and an ask the owner declined ends `: the owner did
// not authorize it` (extensions/jhon.ts).
const GUARD = /^([a-z]+)-guard: /;
const SILENT = /^jhon hook ([a-z]+)-guard did not answer /;
const FAILED = /^([a-z]+)-guard failed \(/;
const WRITER = /^jhon: writer mode: /;
const SHELL = /^jhon: /;
const DECLINED = /: the owner (?:did not authorize it|chose to grant it, but jhon grant failed: .*)$/;

/** Where a refusal's reason ends and its fix starts: at `Fix: `, else after the first sentence, else at the first `; `. */
function splitFix(text: string): [string, string] {
	const fix = text.indexOf(" Fix: ");
	if (fix >= 0) return [text.slice(0, fix).trim(), text.slice(fix + " Fix: ".length).trim()];
	const sentence = /[.)] (?=[A-Z`])/.exec(text);
	if (sentence) return [text.slice(0, sentence.index + 1).trim(), text.slice(sentence.index + 2).trim()];
	const clause = text.indexOf("; ");
	if (clause >= 0) {
		const rest = text.slice(clause + 2).trim();
		return [text.slice(0, clause).trim(), `${rest.charAt(0).toUpperCase()}${rest.slice(1)}`];
	}
	return [text.trim(), ""];
}

/** A tool error's text read as a Jhon guard's refusal, or undefined when it is some other failure. */
export function guardRefusal(text: string): GuardRefusal | undefined {
	const lines = text.replace(/\r\n/g, "\n").trim().split("\n");
	const first = lines[0] ?? "";
	const more = lines.slice(1).map((line) => line.trim()).filter(Boolean);
	const of = (guard: string, rest: string): GuardRefusal => {
		const [reason, fix] = splitFix(rest.replace(/^refused: /, ""));
		return { guard, reason, fix, more };
	};
	let m: RegExpExecArray | null;
	if ((m = GUARD.exec(first))) return of(`${m[1]} guard`, first.slice(m[0].length));
	if ((m = SILENT.exec(first))) return { guard: `${m[1]} guard`, reason: first, fix: "", more };
	if ((m = FAILED.exec(first))) return { guard: `${m[1]} guard`, reason: first, fix: "", more };
	if (WRITER.test(first)) return of("writer guard", first.replace(WRITER, ""));
	if (DECLINED.test(first)) return { guard: "permission", reason: first, fix: "", more };
	if (SHELL.test(first) && /\brefused\b/.test(first)) return of(/sandbox/.test(first) ? "sandbox" : "jhon", first.replace(SHELL, ""));
	return undefined;
}

// --- the split card --------------------------------------------------------------------

/** The card's form and glyphs as the settings say now. */
export function cardLook(theme: BlockTheme): CardLook {
	let ascii = false;
	try { ascii = shellSettings().ascii(); } catch { /* No settings: Unicode. */ }
	return { form: cardStyle() === CARD_STYLE.NEON ? "outline" : "tint", ascii, theme };
}

/** What a result tells its card: the header's state and tone, and its body collapsed and expanded. */
export interface SplitResult {
	/** The state at the right of the header, painted; none leaves it empty. */
	right?: string;
	tone: "normal" | "error";
	/** The body's rows at `cells` text cells: collapsed, and expanded (ctrl+o). */
	rows(cells: number, look: CardLook, expanded: boolean): string[];
}

/** The context pi hands a tool's renderers; only what a split card reads. */
export interface SplitContext {
	state?: unknown;
	isPartial?: boolean;
	isError?: boolean;
	expanded?: boolean;
}

interface SplitRow { result?: SplitResult }

const SPLIT = Symbol.for("jhon-shell.split-card-row");

function splitRow(state: unknown): SplitRow {
	if (state === null || typeof state !== "object") return {};
	const slot = state as Record<symbol, SplitRow | undefined>;
	return (slot[SPLIT] ??= {});
}

/** Whether ctrl+o changes a result's body at width. */
function expands(result: SplitResult, cells: number, look: CardLook): boolean {
	const room = bodyCells(cells, look);
	const collapsed = result.rows(room, look, false);
	return collapsed.some((row) => visibleWidth(row) > room) || JSON.stringify(collapsed) !== JSON.stringify(result.rows(room, look, true));
}

/**
 * A call's header for a tool whose result is a SplitResult: the icon and the
 * title, at the right the result's state (and ctrl+o's hint while it changes
 * the body), or `running` until a result exists, when an outline also closes here.
 */
export class SplitCall implements Component {
	private readonly head: (look: CardLook) => { icon: string; title: string };
	private readonly theme: BlockTheme;
	private readonly context: SplitContext;

	constructor(head: (look: CardLook) => { icon: string; title: string }, theme: BlockTheme, context: SplitContext) {
		this.head = head;
		this.theme = theme;
		this.context = context;
	}

	render(width: number): string[] {
		const cells = Math.max(0, Math.floor(width));
		if (cells === 0) return [];
		const look = cardLook(this.theme);
		const t = this.theme;
		const result = splitRow(this.context.state).result;
		const { icon, title } = this.head(look);
		let right: string | undefined;
		if (result) {
			const expanded = this.context.expanded === true;
			const hint = expands(result, cells, look) ? t.fg("dim", stripAnsi(keyHint("app.tools.expand", expanded ? "to collapse" : "to expand"))) : "";
			right = hint && result.right ? `${hint}  ${result.right}` : hint || result.right;
		} else {
			right = t.fg("warning", `${look.ascii ? "~" : "◐"} running`);
		}
		const tone = result?.tone ?? (this.context.isError ? "error" : "normal");
		return headRows({ icon, title, right, tone }, [], cells, look, !result);
	}

	invalidate(): void {}
}

/** A result's body under a SplitCall; it hands the call its state through the row state pi shares between them. */
export class SplitBody implements Component {
	private readonly result: SplitResult;
	private readonly expanded: boolean;
	private readonly theme: BlockTheme;

	constructor(result: SplitResult, context: SplitContext, theme: BlockTheme) {
		this.result = result;
		this.expanded = context.expanded === true;
		this.theme = theme;
		splitRow(context.state).result = result;
	}

	render(width: number): string[] {
		const cells = Math.max(0, Math.floor(width));
		if (cells === 0) return [];
		const look = cardLook(this.theme);
		return bodyRows(this.result.rows(bodyCells(cells, look), look, this.expanded), cells, look, this.result.tone);
	}

	invalidate(): void {}
}
