/**
 * shell/lib/ui/primitives/list-row.ts — the List row (design/system.md "List
 * row"): `[cursor 2][mark 2][text …][gap ≥2][right columns]`. The cursor row
 * has `▸` in accent and the whole row on the selection tint; its right
 * columns are soft, because dim on the tint is below AA (L72). Right columns
 * give way from the first when the text would get under 16 cells; the last
 * (the time) stays longest. Wrapped rows hang at the text column.
 */
import { visibleWidth, wrapTextWithAnsi } from "@earendil-works/pi-tui";
import { glyph } from "./glyphs.ts";
import { cut, fill, type Painter, type Role } from "./paint.ts";
import { STATES, stateGlyph, type StateName } from "./states.ts";

export interface ListRowInput {
	width: number;
	text: string;
	/** A state by name, or any one-cell glyph (a tool icon) in its role. */
	mark?: StateName | { glyph: string; role: Role };
	/** Right-aligned columns, the first to give way first. */
	right?: readonly string[];
	cursor?: boolean;
	/** The cursor's glyph when not `▸` (the ask view keeps `❯`, glyph table `askCursor`). */
	cursorGlyph?: "cursor" | "askCursor";
	/** No mark slot: rows that never carry a state (palette commands) start their text at column 2. */
	noMark?: boolean;
	/** Wrap the text with a hanging indent instead of cutting it with `…`. */
	wrap?: boolean;
	ascii?: boolean;
	paint: Painter;
}

const SLOT = 2;
const MIN_TEXT = 16;
const COLUMN_GAP = 2;

export function renderListRow({ width, text, mark, right = [], cursor = false, noMark = false, cursorGlyph = "cursor", wrap = false, ascii = false, paint }: ListRowInput): string[] {
	const TEXT_COLUMN = noMark ? SLOT : SLOT * 2;
	if (width <= 0) return [""];
	// Narrower than the slots: the cursor alone, or blank.
	if (width <= (noMark ? 2 : 4)) return [cursor ? `${paint.fg("accent", glyph(cursorGlyph, ascii))}${" ".repeat(width - 1)}` : " ".repeat(width)];
	const columns = [...right];
	const columnCells = () => (columns.length ? columns.reduce((sum, c) => sum + visibleWidth(c), 0) + COLUMN_GAP * (columns.length - 1) + COLUMN_GAP : 0);
	while (columns.length && width - TEXT_COLUMN - columnCells() < MIN_TEXT) columns.shift();
	const room = Math.max(0, width - TEXT_COLUMN - columnCells());

	const cursorSlot = cursor ? `${paint.fg("accent", glyph(cursorGlyph, ascii))} ` : "  ";
	const entry = mark === undefined ? undefined : typeof mark === "string" ? { glyph: stateGlyph(mark, ascii), role: STATES[mark].role as Role } : mark;
	const markSlot = noMark ? "" : entry ? `${paint.fg(entry.role, entry.glyph)}${" ".repeat(Math.max(0, SLOT - visibleWidth(entry.glyph)))}` : "  ";
	const texts = wrap && room > 0 ? wrapTextWithAnsi(text, room) : [cut(text, room, ascii)];
	const rightText = columns.length ? `${" ".repeat(COLUMN_GAP)}${paint.fg(cursor ? "soft" : "dim", columns.join(" ".repeat(COLUMN_GAP)))}` : "";

	return texts.map((row, index) => {
		const lead = index === 0 ? `${cursorSlot}${markSlot}` : " ".repeat(TEXT_COLUMN);
		const line = fill(`${lead}${fill(paint.fg("ink", row), visibleWidth(row), room)}${index === 0 ? rightText : " ".repeat(columnCells())}`, TEXT_COLUMN + room + columnCells(), width);
		return cursor ? paint.sel(line) : line;
	});
}
