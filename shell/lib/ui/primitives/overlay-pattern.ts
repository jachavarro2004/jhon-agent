/**
 * shell/lib/ui/primitives/overlay-pattern.ts — the rows the Jhon overlays
 * drawn in G's Customization card keep (T15.10, narrowed by T15.12e: the
 * header, column labels and footer key line went with the per-overlay looks):
 * pane headers `<name> · <n>/<m>`, choices inline as radios (`●` chosen in
 * accent, `○` others dim) and a preview block for the cursor item.
 */
import { visibleWidth } from "@earendil-works/pi-tui";
import { glyph } from "./glyphs.ts";
import { cut, fill, pad, type Painter } from "./paint.ts";

interface Line { width: number; ascii?: boolean; paint: Painter }

const dot = (ascii: boolean) => ` ${glyph("tabSep", ascii)} `;

export interface PaneHeaderInput extends Line { name: string; index: number; total: number }

/** `<name> · <n>/<m>` bold accent; the name alone without rows. */
export function renderPaneHeader({ width, name, index, total, ascii = false, paint }: PaneHeaderInput): string {
	const text = cut(total > 0 ? `${name}${dot(ascii)}${index}/${total}` : name, width, ascii);
	return fill(paint.bold(paint.fg("accent", text)), visibleWidth(text), width);
}

export interface ChoiceRowInput extends Line {
	label: string;
	/** The label column's cells, so the values line up. */
	labelWidth: number;
	/** The choices; without them the row shows its value as text. */
	options?: readonly string[];
	value: string;
	/** An option picked but not applied yet: underlined. */
	pending?: string;
	/** An option's name as shown (the value is kept). */
	display?(option: string): string;
	cursor?: boolean;
}

const RADIO_GAP = "  ";

/** `▸ Label      ● chosen  ○ other`, exactly width; the cursor row on the tint. */
export function renderChoiceRow({ width, label, labelWidth, options, value, pending, display = (o) => o, cursor = false, ascii = false, paint }: ChoiceRowInput): string {
	const lead = cursor ? `${paint.fg("accent", glyph("cursor", ascii))} ` : "  ";
	const labelCells = Math.max(0, Math.min(labelWidth, width - 2));
	const name = cut(label, labelCells, ascii);
	const room = Math.max(0, width - 2 - labelCells - 2);
	const other = cursor ? "soft" : "dim";
	let shown: string;
	let cells: number;
	if (!options?.length) {
		const text = cut(value, room, ascii);
		shown = paint.fg(cursor ? "ink" : "soft", text);
		cells = visibleWidth(text);
	} else {
		const all = options.includes(value) ? [...options] : [value, ...options];
		const radio = (o: string) => `${glyph(o === value ? "radioOn" : "radioOff", ascii)} ${display(o)}`;
		const more = `${RADIO_GAP}${glyph("ellipsis", ascii)}`;
		const span = (list: string[], cutShort: boolean) => list.reduce((sum, o, i) => sum + visibleWidth(radio(o)) + (i ? RADIO_GAP.length : 0), 0) + (cutShort ? visibleWidth(more) : 0);
		const kept = [...all];
		while (span(kept, kept.length < all.length) > room && kept.some((o) => o !== value)) kept.splice(kept.map((o) => o !== value).lastIndexOf(true), 1);
		// The chosen one alone too wide: no `…`, the chosen one cut.
		const short = kept.length < all.length && span(kept, true) <= room;
		const painted = kept.map((o) => {
			if (o === value) return paint.bold(paint.fg("accent", radio(o)));
			if (o === pending) return `${paint.fg(other, `${glyph("radioOff", ascii)} `)}${paint.underline(paint.fg("ink", display(o)))}`;
			return paint.fg(other, radio(o));
		}).join(RADIO_GAP);
		cells = Math.min(room, span(kept, short));
		shown = span(kept, short) > room ? cut(painted, room, ascii) : `${painted}${short ? paint.fg(other, more) : ""}`;
	}
	const line = fill(`${lead}${fill(paint.fg("ink", name), visibleWidth(name), labelCells)}  ${shown}`, 2 + labelCells + 2 + cells, width);
	return cursor ? paint.sel(line) : line;
}

export interface PreviewInput extends Line {
	value: string;
	/** What the sample shows (`static animation sample`). */
	caption?: string;
	/** The sample, painted or plain. */
	lines: readonly string[];
}

/** `Preview · <value> · <caption>` bold accent, then the sample, each row exactly width. */
export function renderPreview({ width, value, caption, lines, ascii = false, paint }: PreviewInput): string[] {
	const head = cut(["Preview", value, ...(caption ? [caption] : [])].join(dot(ascii)), width, ascii);
	return [fill(paint.bold(paint.fg("accent", head)), visibleWidth(head), width), ...lines.map((line) => pad(line, width))];
}
