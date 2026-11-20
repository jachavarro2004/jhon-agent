/**
 * shell/lib/ui/primitives/card.ts — the conversation card: notices, tool calls
 * and the rail's section cards. Two styles, chosen with shell.appearance.cards:
 *
 *   neon   a rounded outline in the card's tone, the title (and a dim hint at
 *          the right) in the top rule;
 *   float  the default: the same rows one cell in from each side on the tone's
 *          surface, the outline turned into a `▎` bar and spaces, with a blank
 *          row above the heading and before the body.
 *
 * A theme that cannot paint the float's surface, or a width under ten, draws
 * the outline. shell.appearance.ascii draws both in ASCII. The style lives in
 * a process-wide slot because pi loads each extension with its own module
 * cache. The look changes to design/shell-tui's tinted block (block.ts) in
 * later T174 slices; until then this keeps the shell's current rows.
 */
import { truncateToWidth, visibleWidth, wrapTextWithAnsi } from "@earendil-works/pi-tui";
import { shellSettings } from "../../settings.ts";
import { asciiText, glyph } from "./glyphs.ts";
import { paintSurface, surfaceOpener, type SurfaceTheme } from "./tint.ts";

export const CARD_TONE = { INFO: "info", SUCCESS: "success", WARNING: "warning", ERROR: "error" } as const;
export type CardTone = (typeof CARD_TONE)[keyof typeof CARD_TONE];

export const CARD_STYLE = { NEON: "neon", FLOAT: "float" } as const;
export type CardStyle = (typeof CARD_STYLE)[keyof typeof CARD_STYLE];

export interface CardTheme extends SurfaceTheme {
	bold?(text: string): string;
}

/** A card's heading and body. */
export interface Card {
	title: string;
	subtitle?: string;
	body: string[];
	tone: CardTone;
	/** The heading's glyph: 力 when absent, none when "". */
	glyph?: string;
}

export interface CardRenderOptions {
	/** Every body row, or only the first (a notice) or the preview budget. */
	expanded: boolean;
	/** Body rows kept after wrapping; set, the card draws in the active style. */
	previewRows?: number;
	/** Right of the title in the top rule, dim. May carry ANSI. */
	hint?: string;
}

/** The brand, the glyph a card names when it names none. */
export const CARD_GLYPH = glyph("brand");

const STYLE_SLOT = Symbol.for("jhon-shell.card-style");
const slot = globalThis as typeof globalThis & { [STYLE_SLOT]?: unknown };

export function cardStyle(): CardStyle {
	return slot[STYLE_SLOT] === CARD_STYLE.NEON ? CARD_STYLE.NEON : CARD_STYLE.FLOAT;
}

export function setCardStyle(style: CardStyle): void {
	slot[STYLE_SLOT] = style;
}

// --- the chrome -------------------------------------------------------------------

/** The cells that draw a card around its rows; every set keeps the same widths. */
interface Chrome {
	/** The top rule's left end and the cells before the title. */
	open: string; lead: string;
	/** The top rule's right end. */
	close: string;
	rule: string;
	/** The body rows' left and right edges. */
	left: string; right: string;
	/** The closing rule's ends. */
	bottomLeft: string; bottomRight: string;
}

function outline(ascii: boolean): Chrome {
	const g = (name: Parameters<typeof glyph>[0]) => glyph(name, ascii);
	return { open: g("topLeft"), lead: `${g("rule")} `, close: g("topRight"), rule: g("rule"), left: g("side"), right: g("side"), bottomLeft: g("bottomLeft"), bottomRight: g("bottomRight") };
}

// The bar is one cell narrower than `╭─ `: the spare cell goes to the right
// end, so the heading's glyph starts in the body text's column.
function floating(ascii: boolean): Chrome {
	const bar = glyph("bar", ascii);
	return { open: bar, lead: " ", close: "  ", rule: " ", left: bar, right: " ", bottomLeft: bar, bottomRight: " " };
}

const EDGE_ROLE: Record<CardTone, string> = { info: "border", success: "success", warning: "warning", error: "error" };
const TITLE_ROLE: Record<CardTone, string> = { info: "accent", success: "success", warning: "warning", error: "error" };
const FLOAT_SURFACE: Record<CardTone, string> = { info: "toolSuccessBg", success: "toolSuccessBg", warning: "toolPendingBg", error: "toolErrorBg" };
const BODY_ROLE = "text";
const SOFT_ROLE = "muted";
const HINT_ROLE = "dim";
// Two edges and one space inside each.
const EDGE_CELLS = 4;
const FLOAT_MARGIN = 1;
const FLOAT_MIN_WIDTH = 10;
// A continued heading keeps this many cells for the call before its hint drops.
const MIN_CALL = 12;

/** The content cells of a card row at width. */
export function cardInnerWidth(width: number): number {
	return Math.max(1, width - EDGE_CELLS);
}

const ascii = () => shellSettings().ascii();
const cellsOf = (width: number) => Math.max(0, Math.floor(width));

/** The heading text: glyph and title, or the title alone when the glyph is "". */
function headingOf(card: Pick<Card, "glyph" | "title">, asciiOn: boolean): string {
	if (card.glyph === "") return card.title;
	const mark = card.glyph ?? CARD_GLYPH;
	return `${asciiOn ? asciiText(mark) : mark} ${card.title}`;
}

/** What a card's top rule shows: the heading in the tone and the subtitle muted. */
type Heading = Pick<Card, "glyph" | "title" | "subtitle">;

/**
 * The rows of one card in one chrome and tone. Each method returns rows of
 * exactly its width, and never wider at any width.
 */
export interface CardFrame {
	/** `╭─ <glyph> <title> · <subtitle> ──── <hint> ╮`; the hint drops before the title is cut. */
	top(heading: Heading, width: number, hint?: string): string;
	/** The top rule, then the heading's overflow on body rows: a long or multi-line call, every word kept. */
	topRows(heading: Heading, width: number, hint?: string): string[];
	/** One body row, cut with `…`. */
	line(text: string, width: number): string;
	/** Rows wrapped to the content cells; a budget applies after wrapping unless expanded. */
	bodyRows(rows: readonly string[], width: number, options: Pick<CardRenderOptions, "expanded" | "previewRows">): string[];
	/** The closing rule (a blank padded row in the float). */
	close(width: number): string;
	/** The muted `running…` row of a call still waiting for its result. */
	running(width: number): string;
}

function makeFrame(chrome: Chrome, tone: CardTone, theme: CardTheme, asciiOn: boolean): CardFrame {
	const edge = (text: string) => theme.fg(EDGE_ROLE[tone], text);
	const ellipsis = glyph("ellipsis", asciiOn);
	const subtitleSeparator = asciiOn ? "-" : glyph("tabSep");

	const top = (heading: Heading, width: number, hint?: string): string => {
		const cells = cellsOf(width);
		if (cells === 0) return "";
		if (cells < 5) return cells === 1 ? edge(chrome.open) : edge(chrome.open) + edge(`${chrome.rule.repeat(cells - 2)}${chrome.close.slice(0, 1)}`);
		const text = headingOf(heading, asciiOn);
		const painted = heading.subtitle
			? `${theme.fg(TITLE_ROLE[tone], text)} ${theme.fg(SOFT_ROLE, subtitleSeparator)} ${theme.fg(SOFT_ROLE, heading.subtitle)}`
			: theme.fg(TITLE_ROLE[tone], text);
		const titleCells = visibleWidth(text) + (heading.subtitle ? visibleWidth(heading.subtitle) + 3 : 0);
		// Fixed cells: the open and lead (3), the space after the title, the close.
		const fixed = 5;
		const hintCells = hint ? visibleWidth(hint) + 2 : 0;
		const withHint = !!hint && titleCells + fixed + hintCells <= cells;
		const room = Math.max(0, cells - fixed - (withHint ? hintCells : 0));
		const shown = titleCells <= room ? painted : truncateToWidth(painted, room, "");
		const shownCells = titleCells <= room ? titleCells : visibleWidth(shown);
		const fill = chrome.rule.repeat(Math.max(0, cells - shownCells - fixed - (withHint ? hintCells : 0)));
		const tail = withHint ? ` ${theme.fg(HINT_ROLE, hint!)} ` : "";
		return `${edge(chrome.open)}${edge(chrome.lead)}${shown}${edge(` ${fill}`)}${tail}${edge(chrome.close)}`;
	};

	const line = (text: string, width: number): string => {
		const cells = cellsOf(width);
		if (cells === 0) return "";
		const left = edge(chrome.left);
		if (cells === 1) return left;
		if (cells === 2) return left + edge(chrome.right);
		if (cells === 3) return `${left} ${edge(chrome.right)}`;
		const room = cells - EDGE_CELLS;
		const shown = room === 0 ? "" : truncateToWidth(text, room, ellipsis);
		return `${left} ${shown}${" ".repeat(Math.max(0, room - visibleWidth(shown)))} ${edge(chrome.right)}`;
	};

	const bodyRows: CardFrame["bodyRows"] = (rows, width, { expanded, previewRows }) => {
		if (width <= 0) return [];
		const room = cardInnerWidth(width);
		const wrapped = rows.flatMap((row) => row.split("\n").flatMap((part) => (part === "" ? [""] : wrapTextWithAnsi(part, room))));
		const kept = expanded ? wrapped : wrapped.slice(0, Math.max(0, Math.floor(previewRows ?? 3)));
		return kept.map((row) => line(row, width));
	};

	const close = (width: number): string => {
		const cells = cellsOf(width);
		if (cells === 0) return "";
		const left = edge(chrome.bottomLeft);
		return cells === 1 ? left : left + edge(`${chrome.rule.repeat(cells - 2)}${chrome.bottomRight}`);
	};

	const topRows = (heading: Heading, width: number, hint?: string): string[] => {
		const cells = cellsOf(width);
		if (cells === 0) return [];
		if (cells < 8) return [top(heading, cells, hint)];
		const [first = "", ...more] = heading.title.split("\n");
		const lead = visibleWidth(headingOf({ ...heading, title: "" }, asciiOn)) + (heading.subtitle ? visibleWidth(heading.subtitle) + 3 : 0);
		const available = cells - 6 - lead;
		const hintCells = hint ? visibleWidth(hint) + 2 : 0;
		const keepHint = !!hint && available - hintCells >= MIN_CALL;
		const [shown = "", ...overflow] = wrapTextWithAnsi(first, Math.max(1, available - (keepHint ? hintCells : 0)));
		return [top({ ...heading, title: shown }, cells, keepHint ? hint : undefined), ...bodyRows([...overflow, ...more], cells, { expanded: true })];
	};

	const running = (width: number) => line(theme.fg(SOFT_ROLE, `running${glyph("ellipsis", asciiOn)}`), width);

	return { top, topRows, line, bodyRows, close, running };
}

// --- drawing a card ---------------------------------------------------------------

/** The rows one card component draws, by position. */
export interface CardParts {
	/** The top rule and any heading continuation. */
	head?: readonly string[];
	body?: readonly string[];
	/** The closing rule, when this component closes the card. */
	bottom?: string;
	/** A component above this one drew the heading (a tool's result under its call). */
	afterHeading?: boolean;
}

/** The float's surface at this width in the active style, or "" for the outline. */
function floatSurface(theme: CardTheme, tone: CardTone, width: number): string {
	return cardStyle() === CARD_STYLE.FLOAT && Math.floor(width) >= FLOAT_MIN_WIDTH ? surfaceOpener(theme, FLOAT_SURFACE[tone]) : "";
}

/**
 * One card component in the active style. `build` gets the frame to draw
 * with and the width to draw at (the float's is two cells narrower); the
 * float adds its blank rows, its surface and its margins.
 */
export function drawCard(tone: CardTone, theme: CardTheme, width: number, build: (frame: CardFrame, width: number) => CardParts): string[] {
	const cells = cellsOf(width);
	const asciiOn = ascii();
	const open = floatSurface(theme, tone, cells);
	if (!open) {
		const { head = [], body = [], bottom } = build(makeFrame(outline(asciiOn), tone, theme, asciiOn), width);
		return [...head, ...body, ...(bottom === undefined ? [] : [bottom])];
	}
	const frame = makeFrame(floating(asciiOn), tone, theme, asciiOn);
	const inner = cells - FLOAT_MARGIN * 2;
	const { head = [], body = [], bottom, afterHeading = false } = build(frame, inner);
	const blank = frame.close(inner);
	const rows = [
		...(head.length > 0 ? [blank, ...head] : []),
		...(body.length > 0 && (head.length > 0 || afterHeading) ? [blank] : []),
		...body,
		...(bottom === undefined ? [] : [bottom]),
	];
	const margin = " ".repeat(FLOAT_MARGIN);
	return rows.map((row) => `${margin}${paintSurface(row, open)}${margin}`);
}

/** Body cells of a section card in the active style (the float's are two fewer). */
export function panelInnerWidth(theme: CardTheme, width: number, tone: CardTone = CARD_TONE.INFO): number {
	return cardInnerWidth(floatSurface(theme, tone, width) ? Math.floor(width) - FLOAT_MARGIN * 2 : width);
}

/**
 * A whole card. With previewRows it draws in the active style and nothing at
 * a nonpositive width; without, it is always the outline, the body collapsed
 * to its first line unless expanded, and keeps its row count at any width.
 */
export function renderCard(card: Card, theme: CardTheme, width: number, options: CardRenderOptions): string[] {
	if (options.previewRows !== undefined) {
		if (width <= 0) return [];
		return drawCard(card.tone, theme, width, (frame, inner) => ({
			head: [frame.top(card, inner, options.hint)],
			body: frame.bodyRows(card.body.map((row) => theme.fg(BODY_ROLE, row)), inner, options),
			bottom: frame.close(inner),
		}));
	}
	const asciiOn = ascii();
	const frame = makeFrame(outline(asciiOn), card.tone, theme, asciiOn);
	return [frame.top(card, width, options.hint), ...noticeText(card, theme, cardInnerWidth(width), options.expanded, asciiOn).map((row) => frame.line(row, width)), frame.close(width)];
}

/** A notice's body wrapped to its content cells: all of it, or its first line with `…` when more follows. */
function noticeText(card: Card, theme: CardTheme, room: number, expanded: boolean, asciiOn: boolean): string[] {
	const rows = card.body.flatMap((paragraph) => (paragraph === "" ? [""] : wrapTextWithAnsi(paragraph, room)));
	if (rows.length === 0) return [];
	if (expanded) return rows.map((row) => (row === "" ? "" : theme.fg(BODY_ROLE, row)));
	const first = rows.find((row) => row !== "") ?? "";
	const shown = rows.length > 1 ? `${truncateToWidth(first, Math.max(1, room - 1), "")}${glyph("ellipsis", asciiOn)}` : first;
	return [theme.fg(BODY_ROLE, shown)];
}

// --- a tool row's call and result ---------------------------------------------------

// Pi draws a tool row as its call component, then, once a result exists, the
// result component, which closes the card. The call cannot see the result, so
// the result renderer marks the row state both share.
const RESULT = Symbol.for("jhon-shell.card-result");

/** The fields of pi's tool render context that say whether a result exists. */
export interface CardRowContext {
	isPartial?: boolean;
	state?: unknown;
}

/** Marks the shared row state: from now on the result component closes the card. */
export function markCardResult(state: unknown): void {
	if (state !== null && typeof state === "object") (state as Record<symbol, unknown>)[RESULT] = true;
}

/** Whether a call card still closes its own frame. Read at render time: pi builds the result after the call. */
export function cardAwaitingResult(row: CardRowContext): boolean {
	if (row.isPartial === false) return false;
	const state = row.state;
	return !(state !== null && typeof state === "object" && (state as Record<symbol, unknown>)[RESULT] === true);
}
