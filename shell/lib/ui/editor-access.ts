/**
 * The one place the shell reads pi's editor from the inside (T174-S9).
 *
 * pi's Editor (pi-tui components/editor.js, which CustomEditor extends) keeps
 * its text, caret, layout, undo and completion menu in members it does not
 * export. The prompt's selection (lib/ui/prompt-selection.ts) needs them to
 * move the caret, replace text in one undo step and paint the selected cells,
 * and the prompt's frame needs the count of input rows pi just drew. Every
 * such read goes through this file, so a pi upgrade is checked here only.
 *
 * When a member is missing or no longer a function, `editorAccess` answers
 * undefined and the selection turns itself off: keys go to pi as they come,
 * nothing is painted, editing goes on. A pi upgrade can cost the selection,
 * never the prompt.
 */

/** A place in the text: the logical line and the code unit within it. */
export interface Caret {
	line: number;
	col: number;
}

/** One drawn input row: the logical line it shows and the code units [start, start + length) of it. */
export interface DrawnRow {
	line: number;
	start: number;
	length: number;
}

/** What pi's editor offers the selection, in its own words. */
export interface EditorAccess {
	/** The live lines (the array pi edits). */
	lines(): string[];
	caret(): Caret;
	/** Puts the caret at p (pi clamps the column and keeps its sticky column right). */
	moveCaret(p: Caret): void;
	/** pi's own home and end (they also end a kill or yank run). */
	toLineStart(): void;
	toLineEnd(): void;
	/** Writes the whole text and the caret, outside pi's undo (the caller took its snapshot, or pi did). */
	write(lines: string[], caret: Caret): void;
	/** pi's undo snapshot of the text and caret as they are now. */
	snapshot(): void;
	/** Ends a kill/yank/typed-word run, so the next edit is a step of its own. */
	endRun(): void;
	/** Leaves the prompt history the up arrow was browsing. */
	leaveHistory(): void;
	/** True while the completion menu is open. */
	completing(): boolean;
	refreshCompletions(): void;
	closeCompletions(): void;
	repaint(): void;
	/** The input rows drawn by the last render at width (pi's own wrap), top to bottom. */
	drawnRows(width: number): DrawnRow[];
	/** The columns pi pads each input row with on each side. */
	padding(): number;
}

/** pi's private members, as the shell expects them. */
interface PiEditorInside {
	state: { lines: string[]; cursorLine: number; cursorCol: number };
	paddingX: number;
	scrollOffset: number;
	renderedVisibleLineCount: number;
	/** The width the last render wrapped at; 0 or absent falls back to the width the prompt gave. */
	lastWidth?: number;
	tui: { requestRender(): void };
	autocompleteState: "regular" | "force" | null;
	lastAction: string | null;
	pushUndoSnapshot(): void;
	setCursorCol(col: number): void;
	moveToLineStart(): void;
	moveToLineEnd(): void;
	exitHistoryBrowsing(): void;
	cancelAutocomplete(): void;
	updateAutocomplete(): void;
	buildVisualLineMap(width: number): Array<{ logicalLine: number; startCol: number; length: number }>;
}

/** Fields pi sets in its constructor (instance fields: read on the instance, never on a prototype). null is a value. */
const FIELDS = ["state", "paddingX", "scrollOffset", "renderedVisibleLineCount", "autocompleteState", "lastAction", "tui"] as const;
const METHODS = [
	"pushUndoSnapshot", "setCursorCol", "moveToLineStart", "moveToLineEnd",
	"exitHistoryBrowsing", "cancelAutocomplete", "updateAutocomplete", "buildVisualLineMap",
] as const;

/** The members `editor` lacks of those the selection reads; empty when pi still has them all. */
export function missingMembers(editor: object): string[] {
	const inside = editor as Record<string, unknown>;
	return [
		...FIELDS.filter((name) => inside[name] === undefined),
		...METHODS.filter((name) => typeof inside[name] !== "function"),
	];
}

/**
 * pi's editor seen from the inside, or undefined once a member is missing.
 * The check runs once per editor, on first use, and its answer stays.
 */
const checked = new WeakMap<object, EditorAccess | null>();
export function editorAccess(editor: object): EditorAccess | undefined {
	let access = checked.get(editor);
	if (access === undefined) {
		access = missingMembers(editor).length === 0 ? open(editor as PiEditorInside) : null;
		checked.set(editor, access);
	}
	return access ?? undefined;
}

function open(pi: PiEditorInside): EditorAccess {
	return {
		lines: () => pi.state.lines,
		caret: () => ({ line: pi.state.cursorLine, col: pi.state.cursorCol }),
		moveCaret(p) {
			pi.state.cursorLine = p.line;
			pi.setCursorCol(p.col);
		},
		toLineStart: () => pi.moveToLineStart(),
		toLineEnd: () => pi.moveToLineEnd(),
		write(lines, caret) {
			pi.state.lines = lines;
			pi.state.cursorLine = caret.line;
			pi.setCursorCol(caret.col);
		},
		snapshot: () => pi.pushUndoSnapshot(),
		endRun() { pi.lastAction = null; },
		leaveHistory: () => pi.exitHistoryBrowsing(),
		completing: () => Boolean(pi.autocompleteState),
		refreshCompletions: () => pi.updateAutocomplete(),
		closeCompletions: () => pi.cancelAutocomplete(),
		repaint: () => pi.tui.requestRender(),
		drawnRows(width) {
			const wrap = pi.lastWidth || Math.max(1, width - pi.paddingX * 2 - (pi.paddingX ? 0 : 1));
			const map = pi.buildVisualLineMap(wrap);
			const rows: DrawnRow[] = [];
			for (let r = 0; r < pi.renderedVisibleLineCount; r++) {
				const row = map[pi.scrollOffset + r];
				if (!row) break;
				rows.push({ line: row.logicalLine, start: row.startCol, length: row.length });
			}
			return rows;
		},
		padding: () => pi.paddingX,
	};
}

/**
 * How many input rows pi drew between its two rules in the last render, when
 * pi still says so and it fits the rows given; undefined otherwise (the caller
 * then takes every row between the rules).
 */
export function drawnInputRows(editor: object, renderedRows: number): number | undefined {
	const count = (editor as { renderedVisibleLineCount?: unknown }).renderedVisibleLineCount;
	return typeof count === "number" && Number.isInteger(count) && count >= 1 && count + 2 <= renderedRows ? count : undefined;
}
