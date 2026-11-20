/**
 * shell/lib/ui/primitives/fit.ts — text measured in terminal cells: a CJK
 * glyph takes two, an escape sequence none. Every row the shell draws goes
 * through clip (never wider than its width) or fit (exactly its width).
 */
import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";

// CSI sequences (colours, cursor) and OSC sequences (hyperlinks, titles).
const ESCAPES = /\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g;

/** The text a terminal shows: escape sequences removed. */
export function stripAnsi(text: string): string {
	return text.replace(ESCAPES, "");
}

export interface ClipOptions {
	/** The ASCII ellipsis `~` instead of `…`. */
	ascii?: boolean;
	/** The mark a cut ends with; "" cuts hard. Defaults to the glyph table's ellipsis. */
	ellipsis?: string;
}

const graphemes = new Intl.Segmenter();

/** Text cut to width cells, ending in the ellipsis when cut; never wider, never a split wide glyph. */
export function clip(text: string, width: number, { ascii = false, ellipsis }: ClipOptions = {}): string {
	if (width <= 0) return "";
	if (visibleWidth(text) <= width) return text;
	const mark = ellipsis ?? (ascii ? "~" : "…");
	// Painted text: pi-tui's cut keeps every escape whole and resets after it.
	if (text.includes("\x1b")) return truncateToWidth(text, width, mark);
	const room = width - visibleWidth(mark);
	let kept = "";
	let used = 0;
	for (const { segment } of graphemes.segment(text)) {
		const cells = visibleWidth(segment);
		if (used + cells > room) break;
		kept += segment;
		used += cells;
	}
	return `${kept}${mark}`;
}

/** Text of exactly width cells: clipped when long, padded with spaces when short. */
export function fit(text: string, width: number, options: ClipOptions = {}): string {
	if (width <= 0) return "";
	const clipped = clip(text, width, options);
	return clipped + " ".repeat(Math.max(0, width - visibleWidth(clipped)));
}

/**
 * `left`, a gap, then `right` ending at width: exactly width cells. The left
 * part is cut first; a right part with no room beside one cell of gap drops.
 */
export function spread(left: string, right: string, width: number, options: ClipOptions = {}): string {
	const rightCells = visibleWidth(right);
	if (!right || rightCells + 1 > width) return fit(left, width, options);
	const kept = clip(left, width - rightCells - 1, options);
	return `${kept}${" ".repeat(width - visibleWidth(kept) - rightCells)}${right}`;
}
