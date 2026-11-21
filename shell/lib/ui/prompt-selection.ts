/**
 * The prompt's text selection (T174-S9), Jhon's own.
 *
 * Keys: shift+home and shift+end select from the caret to the line's start
 * or end (a held key keeps the selection; one that selects nothing selects
 * nothing); alt+a and alt+e select the whole text. Over a selection, Del,
 * backspace (and their shift forms), a typed character or a bracketed paste
 * replace it in one undo step; any other key drops it first and then does
 * what it does. A shortcut another extension registered wins its chord.
 *
 * Look: the selected cells inverted, and the bottom rule's right end says
 * ` N chars selected, Del deletes them, alt+a selects all `.
 *
 * pi's editor is read from the inside through lib/ui/editor-access.ts only;
 * when pi no longer has what that file reads, keys go to pi unchanged and
 * nothing is painted. Dragging with the mouse and copying are pi's own
 * (its full-screen renderer selects screen cells and copies on release).
 */
import { decodeKittyPrintable, isKeyRelease, matchesKey, truncateToWidth, visibleWidth, type EditorComponent } from "@earendil-works/pi-tui";
import { editorAccess, type Caret, type EditorAccess } from "./editor-access.ts";

const INVERSE_ON = "\x1b[7m";
const INVERSE_OFF = "\x1b[27m";
/** Where pi-tui's software cursor ends (an APC mark the renderer turns into "inverse off"); pi-tui does not export it. */
const SOFT_CURSOR_END = "\x1b_pi:/fc\x07";

const PASTE_START = "\x1b[200~";
const PASTE_END = "\x1b[201~";
/** A paste whose end mark has not come yet is dropped past this many UTF-8 bytes. A finished paste has no limit. */
const UNFINISHED_PASTE_LIMIT = 16 * 1024 * 1024;

const SELECT_CHORDS = ["shift+home", "shift+end", "alt+a", "alt+e"] as const;
const DELETE_KEYS = ["backspace", "shift+backspace", "delete", "shift+delete"] as const;

/** The bottom rule's words for a selection of n characters. */
export function selectionHint(n: number): string {
	return ` ${n} char${n === 1 ? "" : "s"} selected, Del deletes them, alt+a selects all `;
}

// --- what a key types ------------------------------------------------------------------

/** The character a modifyOtherKeys sequence (`ESC [ 27 ; mod ; code ~`) types: shift at most, caps and num lock ignored. */
function modifyOtherKeysText(data: string): string | undefined {
	const found = /^\x1b\[27;(\d+);(\d+)~$/.exec(data);
	if (!found) return undefined;
	const held = (Number(found[1]) - 1) & ~(64 | 128);
	const code = Number(found[2]);
	if ((held & ~1) !== 0 || code < 32 || code > 0x10ffff) return undefined;
	return String.fromCodePoint(code);
}

/** A control code: C0, DEL or C1. */
const isControl = (code: number): boolean => code < 32 || code === 127 || (code >= 0x80 && code <= 0x9f);

/**
 * What the key types, if it types anything: kitty's and modifyOtherKeys'
 * encodings, else a raw run of up to 4 code units with no control code.
 */
function typedBy(data: string): string | undefined {
	const encoded = decodeKittyPrintable(data) ?? modifyOtherKeysText(data);
	if (encoded !== undefined) return encoded;
	if (data.length > 4) return undefined;
	for (let i = 0; i < data.length; i++) if (isControl(data.charCodeAt(i))) return undefined;
	return data;
}

// --- painting ---------------------------------------------------------------------------

/** The escape sequence at row[i] (row[i] is ESC): CSI to its final byte, OSC or APC to BEL or ST, else ESC and one more. Unfinished: the rest of the row. */
const ESCAPE = /\x1b(?:\[[^\x40-\x7e]*[\x40-\x7e]|[\]_][\s\S]*?(?:\x07|\x1b\\)|[^\[\]_])/y;
function escapeAt(row: string, i: number): string {
	ESCAPE.lastIndex = i;
	return ESCAPE.exec(row)?.[0] ?? row.slice(i);
}

/** SGR with no parameter, or whose first parameter is 0: it turns inverse off too. */
function resetsInverse(sequence: string): boolean {
	const params = /^\x1b\[([\d;]*)m$/.exec(sequence)?.[1];
	return params !== undefined && (params === "" || /^0+$/.test(params.split(";")[0]!));
}

/**
 * The row with its code units [from, to) of plain text inverted. Escape
 * sequences in the row pass through and take no column; after one that ends
 * inverse inside the span (a reset, the software cursor's end) inverse goes
 * on again. Inverse ends with "inverse off" only, so the row's colours stay.
 */
export function invertCells(row: string, from: number, to: number): string {
	let out = "";
	let column = 0;
	let inverted = false;
	for (let i = 0; i < row.length;) {
		if (!inverted && column >= from) { out += INVERSE_ON; inverted = true; }
		if (inverted && column >= to) return out + INVERSE_OFF + row.slice(i);
		if (row[i] === "\x1b") {
			const sequence = escapeAt(row, i);
			out += sequence;
			i += sequence.length;
			if (inverted && (resetsInverse(sequence) || sequence === SOFT_CURSOR_END)) out += INVERSE_ON;
			continue;
		}
		out += row[i];
		i += 1;
		column += 1;
	}
	return inverted ? out + INVERSE_OFF : out;
}

// --- paste ------------------------------------------------------------------------------

/**
 * The text once a paste replaces the selection [start, end) (offsets in the
 * text joined with \n): pi inserted `after` at `caret` of `before`, and its
 * insertion goes in place of the selection. Undefined when pi did something
 * other than insert at the caret, or inserted nothing: pi's result stays.
 */
export function pasteInto(before: string, after: string, caret: number, start: number, end: number): { text: string; caret: number } | undefined {
	const head = before.slice(0, caret);
	const tail = before.slice(caret);
	if (after.length <= before.length || !after.startsWith(head) || !after.endsWith(tail)) return undefined;
	const pasted = after.slice(head.length, after.length - tail.length);
	return { text: before.slice(0, start) + pasted + before.slice(end), caret: start + pasted.length };
}

/** An unfinished bracketed paste, gathered piece by piece. */
interface PendingPaste {
	pieces: string[];
	/** Code units gathered before the newest piece. */
	units: number;
	/** UTF-8 bytes gathered, checked against UNFINISHED_PASTE_LIMIT. */
	bytes: number;
	/** The last code units gathered (one short of the end mark), so an end mark split across pieces is still found. */
	tail: string;
}

// --- the selection ----------------------------------------------------------------------

const before = (a: Caret, b: Caret): boolean => a.line < b.line || (a.line === b.line && a.col < b.col);
const same = (a: Caret, b: Caret): boolean => a.line === b.line && a.col === b.col;

export class PromptSelection {
	private readonly editor: EditorComponent;
	/** Where the selection began; the caret is its other end. */
	private anchor: Caret | undefined;
	private paste: PendingPaste | undefined;

	constructor(editor: EditorComponent) {
		this.editor = editor;
	}

	/** pi's editor from the inside, or undefined when pi changed: then no selection at all. */
	private get pi(): EditorAccess | undefined {
		return editorAccess(this.editor);
	}

	/** The selection's ends in text order, while one is open. */
	private span(pi: EditorAccess): [Caret, Caret] | undefined {
		if (!this.anchor) return undefined;
		const caret = pi.caret();
		return before(this.anchor, caret) ? [this.anchor, caret] : [caret, this.anchor];
	}

	/** Characters selected, a line break counting one. */
	size(): number {
		const pi = this.anchor && this.pi;
		const span = pi && this.span(pi);
		if (!pi || !span) return 0;
		const [start, end] = span;
		if (start.line === end.line) return end.col - start.col;
		const lines = pi.lines();
		let n = (lines[start.line] ?? "").length - start.col + end.col;
		for (let line = start.line + 1; line < end.line; line++) n += (lines[line] ?? "").length;
		return n + end.line - start.line;
	}

	/**
	 * A key or a piece of a paste. `pass` hands it to the prompt's own keys
	 * (pi's editor behind the esc gate); `shortcut` runs another extension's
	 * shortcut for a selection chord and says whether one took it.
	 */
	input(data: string, pass: (data: string) => void, shortcut?: (data: string) => boolean): void {
		const pi = this.pi;
		if (!pi) { pass(data); return; }
		if (this.paste || (this.anchor && data.startsWith(PASTE_START))) { this.gather(pi, data, pass, shortcut); return; }
		// kitty reports releases too; they match their key, so they go before any match.
		if (isKeyRelease(data)) return;
		if (SELECT_CHORDS.some((chord) => matchesKey(data, chord)) && shortcut?.(data)) return;
		if (matchesKey(data, "shift+home")) { this.toLineEdge(pi, "start"); return; }
		if (matchesKey(data, "shift+end")) { this.toLineEdge(pi, "end"); return; }
		if (matchesKey(data, "alt+a") || matchesKey(data, "alt+e")) { this.selectAll(pi); return; }
		if (this.anchor && this.replaces(data) && this.replace(pi, data)) return;
		this.anchor = undefined;
		pass(data);
	}

	private replaces(data: string): boolean {
		return DELETE_KEYS.some((key) => matchesKey(data, key)) || typedBy(data) !== undefined;
	}

	private toLineEdge(pi: EditorAccess, edge: "start" | "end"): void {
		const from = pi.caret();
		if (edge === "start") pi.toLineStart();
		else pi.toLineEnd();
		pi.leaveHistory();
		// A held key repeats byte for byte: the anchor stays where the first press found the caret.
		this.anchor ??= from;
		if (same(this.anchor, pi.caret())) this.anchor = undefined;
		if (pi.completing()) pi.refreshCompletions();
		pi.repaint();
	}

	private selectAll(pi: EditorAccess): void {
		const lines = pi.lines();
		const last = Math.max(0, lines.length - 1);
		this.anchor = { line: 0, col: 0 };
		pi.moveCaret({ line: last, col: (lines[last] ?? "").length });
		pi.endRun();
		pi.leaveHistory();
		if (pi.completing()) pi.closeCompletions();
		pi.repaint();
	}

	/** Deletes the selection and puts what the key types in its place, as one undo step. False when nothing is selected. */
	private replace(pi: EditorAccess, data: string): boolean {
		const span = this.span(pi);
		if (!span || same(...span)) return false;
		const [start, end] = span;
		const typed = typedBy(data);
		// A DEL or C1 code point sent as a key deletes and types nothing.
		const text = typed !== undefined && !isControl(typed.codePointAt(0) ?? 0) ? typed : "";
		const lines = pi.lines();
		pi.snapshot();
		const joined = (lines[start.line] ?? "").slice(0, start.col) + text + (lines[end.line] ?? "").slice(end.col);
		pi.write([...lines.slice(0, start.line), joined, ...lines.slice(end.line + 1)], { line: start.line, col: start.col + text.length });
		this.anchor = undefined;
		pi.leaveHistory();
		pi.endRun();
		if (pi.completing()) pi.closeCompletions();
		this.changed(pi);
		return true;
	}

	private changed(pi: EditorAccess): void {
		this.editor.onChange?.(this.editor.getText());
		pi.repaint();
	}

	/**
	 * A bracketed paste over the selection, gathered until its end mark. Only
	 * the newest piece and the carried tail are searched for the mark. An
	 * unfinished paste past the limit is dropped, never handed on: the
	 * selection stays.
	 */
	private gather(pi: EditorAccess, data: string, pass: (data: string) => void, shortcut?: (data: string) => boolean): void {
		const paste = (this.paste ??= { pieces: [], units: 0, bytes: 0, tail: "" });
		const window = paste.tail + data;
		const mark = window.indexOf(PASTE_END);
		paste.pieces.push(data);
		if (mark < 0) {
			paste.units += data.length;
			paste.bytes += Buffer.byteLength(data, "utf8");
			paste.tail = window.slice(-(PASTE_END.length - 1));
			if (paste.bytes > UNFINISHED_PASTE_LIMIT) this.paste = undefined;
			return;
		}
		const frameEnd = paste.units - paste.tail.length + mark + PASTE_END.length;
		const gathered = paste.pieces.join("");
		this.paste = undefined;
		this.pasteOver(pi, gathered.slice(0, frameEnd), pass);
		const rest = gathered.slice(frameEnd);
		if (rest) this.input(rest, pass, shortcut);
	}

	/** pi takes the paste as it would (its own undo step, its paste marker); then its insertion moves into the selection's place. */
	private pasteOver(pi: EditorAccess, frame: string, pass: (data: string) => void): void {
		const span = this.span(pi);
		this.anchor = undefined;
		if (!span || frame === PASTE_START + PASTE_END || same(...span)) { pass(frame); return; }
		// Offsets into the text joined with \n, taken before pi edits its lines.
		const lines = pi.lines();
		const offset = (p: Caret) => lines.slice(0, p.line).reduce((n, line) => n + line.length + 1, p.col);
		const text = lines.join("\n");
		const [caret, start, end] = [offset(pi.caret()), offset(span[0]), offset(span[1])];
		pass(frame);
		const result = pasteInto(text, pi.lines().join("\n"), caret, start, end);
		if (!result) return;
		const head = result.text.slice(0, result.caret).split("\n");
		pi.write(result.text.split("\n"), { line: head.length - 1, col: head.at(-1)!.length });
		pi.endRun();
		this.changed(pi);
	}

	/** pi's rendered rows (its top rule first, then the input rows) with the selected cells inverted; `width` is the width pi drew at. */
	paint(rows: string[], width: number): string[] {
		const pi = this.anchor && this.pi;
		const span = pi && this.span(pi);
		if (!pi || !span) return rows;
		const [start, end] = span;
		const pad = pi.padding();
		pi.drawnRows(width).forEach((row, r) => {
			if (row.line < start.line || row.line > end.line) return;
			const clip = (col: number) => Math.min(Math.max(col - row.start, 0), row.length);
			const from = (row.line === start.line ? clip(start.col) : 0) + pad;
			const to = (row.line === end.line ? clip(end.col) : row.length) + pad;
			if (to > from && r + 1 < rows.length) rows[r + 1] = invertCells(rows[r + 1] ?? "", from, to);
		});
		return rows;
	}

	/** The bottom rule with the selection's count at its right end, before `end` (the rule's last cell); the bare rule when they do not fit. */
	hint(rule: string, width: number, end: string): string {
		const n = this.size();
		if (n <= 0) return rule;
		const words = selectionHint(n);
		const room = width - visibleWidth(words) - visibleWidth(end);
		if (room < 2) return rule;
		return truncateToWidth(rule, room, "") + words + end;
	}
}
