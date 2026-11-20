/**
 * Row snapshots (T174 plan, "Tests per slice"): a component renders with a
 * plain theme at the two widths the design is drawn at (design/shell-tui: 180
 * columns, and the narrow form at 100), ANSI is stripped, and the rows are
 * compared exactly, trailing spaces included.
 */
import assert from "node:assert/strict";
import { visibleWidth } from "@earendil-works/pi-tui";
import { stripAnsi } from "../lib/ui/primitives/index.ts";

export const SNAPSHOT_WIDTHS = [180, 100] as const;
export type SnapshotWidth = (typeof SNAPSHOT_WIDTHS)[number];

/** No colour and no background: what a terminal without a theme would show. */
export const plainTheme = {
	fg: (_role: string, text: string) => text,
	bold: (text: string) => text,
	italic: (text: string) => text,
	underline: (text: string) => text,
	strikethrough: (text: string) => text,
};
export type PlainTheme = typeof plainTheme;

/** The rows a render draws at width, ANSI stripped. */
export function rowsAt(render: (width: number, theme: PlainTheme) => readonly string[], width: number): string[] {
	return render(width, plainTheme).map(stripAnsi);
}

/** `left`, spaces, then `right` ending at width (display cells, so wide glyphs count twice). */
export function spread(left: string, right: string, width: number): string {
	return `${left}${" ".repeat(Math.max(0, width - visibleWidth(left) - visibleWidth(right)))}${right}`;
}

/** `text` padded with spaces to width display cells. */
export function padded(text: string, width: number): string {
	return spread(text, "", width);
}

/**
 * Asserts the exact rows at 180 and 100 columns. `expected` is either the rows
 * per width or a function of the width, for rows that only stretch.
 */
export function assertRows(
	render: (width: number, theme: PlainTheme) => readonly string[],
	expected: Record<SnapshotWidth, readonly string[]> | ((width: SnapshotWidth) => readonly string[]),
	message = "rows",
): void {
	for (const width of SNAPSHOT_WIDTHS) {
		const want = typeof expected === "function" ? expected(width) : expected[width];
		assert.deepEqual(rowsAt(render, width), [...want], `${message} at ${width} columns`);
	}
}
