/**
 * shell/lib/ui/primitives/state-block.ts — the empty, loading and error
 * state block (design/system.md "Empty, loading and error state block"): one
 * block where the content goes. Loading: `◐ text` and the seconds after one
 * second. Empty: what is missing, then the next step with its key. Error:
 * `✗ text` bold bad, the cause in soft with a hanging indent, then the next
 * step. It returns the keys that act in that state, for the frame's key line.
 */
import { visibleWidth, wrapTextWithAnsi } from "@earendil-works/pi-tui";
import type { KeyHint } from "./key-hint.ts";
import { fitHints, hintCells, paintHints } from "./key-hint.ts";
import { cut, fill, type Painter } from "./paint.ts";
import { stateGlyph } from "./states.ts";

export type StateBlockKind = "loading" | "empty" | "error";

export interface StateBlockInput {
	width: number;
	kind: StateBlockKind;
	text: string;
	/** Why it failed: the error's own words, not the whole JSON. */
	cause?: string;
	/** The next step: only keys that act in this state. */
	next?: readonly KeyHint[];
	/** Seconds spent loading; shown from 1. */
	elapsed?: number;
	ascii?: boolean;
	paint: Painter;
}

const CLOSE: KeyHint = ["esc", "close"];
const HANG = 2;

export function renderStateBlock(input: StateBlockInput): { lines: string[]; keys: KeyHint[] } {
	const { width, kind, text, cause, next = [], elapsed, ascii = false, paint } = input;
	const lines: Array<[string, number]> = [];
	if (kind === "loading") {
		const seconds = elapsed !== undefined && elapsed >= 1 ? `  ${Math.floor(elapsed)}s` : "";
		const head = cut(`${stateGlyph("working", ascii)} ${text}`, Math.max(0, width - seconds.length), ascii);
		lines.push([`${paint.fg("warn", head.slice(0, 1))}${paint.fg("ink", head.slice(1))}${paint.fg("dim", seconds)}`, visibleWidth(head) + seconds.length]);
	} else if (kind === "empty") {
		const line = cut(text, width, ascii);
		lines.push([paint.fg("soft", line), visibleWidth(line)]);
	} else {
		const line = cut(`${stateGlyph("failed", ascii)} ${text}`, width, ascii);
		lines.push([paint.bold(paint.fg("bad", line)), visibleWidth(line)]);
	}
	if (kind === "error" && cause && width > HANG) {
		for (const row of wrapTextWithAnsi(cause.trim(), width - HANG)) lines.push([`${" ".repeat(HANG)}${paint.fg("soft", row)}`, HANG + visibleWidth(row)]);
	}
	if (kind !== "loading" && next.length) {
		const fitted = fitHints(next, width);
		lines.push([paintHints(fitted, paint), hintCells(fitted)]);
	}
	return { lines: lines.map(([line, cells]) => fill(line, cells, width)), keys: [...(kind === "loading" ? [] : next), CLOSE] };
}
