/**
 * shell/lib/ui/primitives/rule.ts — horizontal rules and pane dividers
 * (design/shell-tui): one thin rule in the faint role separates what sits
 * side by side or one above the other. A rule may carry a label (04's
 * `── Running  2 agents · 1 job ───`) or open with a heavy run in accent (the
 * tab underline of the full-screen views).
 */
import { visibleWidth } from "@earendil-works/pi-tui";
import { fit, clip } from "./fit.ts";
import { glyph } from "./glyphs.ts";
import type { Painter, Role } from "./paint.ts";

interface Drawn { ascii?: boolean; paint: Painter; role?: Role }

export interface RuleInput extends Drawn {
	width: number;
	/** A section name after the rule's lead, bold soft. */
	label?: string;
	/** Dim words after the label; the first thing to give way. */
	note?: string;
	/** Cells drawn heavy in accent at the start. */
	heavy?: number;
}

const LEAD_CELLS = 2;
const MIN_LABELLED = 5;

/** One rule of exactly width cells. */
export function renderRule({ width, label, note, heavy = 0, ascii = false, paint, role = "faint" }: RuleInput): string {
	if (width <= 0) return "";
	const line = (cells: number) => (cells > 0 ? paint.fg(role, glyph("rule", ascii).repeat(cells)) : "");
	if (!label || width < MIN_LABELLED) {
		const strong = Math.min(width, Math.max(0, heavy));
		return `${strong > 0 ? paint.fg("accent", glyph("heavyRule", ascii).repeat(strong)) : ""}${line(width - strong)}`;
	}
	// `── ` + label + `  note` + ` ` + the fill; the note shows only with at least one cell of fill after it.
	const shown = clip(label, width - LEAD_CELLS - 2, { ascii });
	const used = LEAD_CELLS + 1 + visibleWidth(shown) + 1;
	const withNote = note ? used + 2 + visibleWidth(note) : used;
	const tail = note && withNote < width ? paint.fg("dim", `  ${note}`) : "";
	const fill = width - (tail ? withNote : used);
	return `${line(LEAD_CELLS)} ${paint.bold(paint.fg("soft", shown))}${tail} ${line(fill)}`;
}

/** A pane divider: one column of the side glyph, height rows tall. */
export function renderDivider(height: number, { ascii = false, paint, role = "faint" }: Drawn): string[] {
	return Array.from({ length: Math.max(0, height) }, () => paint.fg(role, glyph("side", ascii)));
}

export interface PanesInput extends Drawn {
	left: readonly string[];
	right: readonly string[];
	/** The left pane's cells; the right pane takes what is left after the divider. */
	leftWidth: number;
	/** The whole row's cells. */
	width: number;
	/** Spaces on each side of the divider. */
	gap?: number;
}

/** Two panes side by side behind one divider (design 01's split): every row exactly width, each pane clipped to its own. */
export function joinPanes({ left, right, leftWidth, width, gap = 1, ascii = false, paint, role = "faint" }: PanesInput): string[] {
	const space = " ".repeat(Math.max(0, gap));
	const rightWidth = Math.max(0, width - leftWidth - 1 - space.length * 2);
	const divider = paint.fg(role, glyph("side", ascii));
	return Array.from({ length: Math.max(left.length, right.length) }, (_, row) =>
		`${fit(left[row] ?? "", leftWidth, { ascii })}${space}${divider}${space}${fit(right[row] ?? "", rightWidth, { ascii })}`);
}
