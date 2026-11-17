/**
 * shell/lib/ui/primitives/key-hint.ts — the key hint line (design/system.md
 * "Key hint"): `key action` pairs two spaces apart; when the line does not
 * fit, whole hints drop from the lowest priority, and `esc close` is pinned:
 * the last to go and never cut with `…` (audit P2).
 */
import { visibleWidth } from "@earendil-works/pi-tui";
import type { Painter } from "./paint.ts";

/** `[key, action]`, or with an explicit priority (higher stays longer). Without one, earlier hints rank higher. */
export type KeyHint = readonly [string, string] | { key: string; action: string; priority?: number };

interface Ranked { key: string; action: string; rank: number }

const SEP = "  ";

function ranked(hints: readonly KeyHint[]): Ranked[] {
	return hints.map((hint, index) => {
		const { key, action, priority } = "key" in hint ? hint : { key: hint[0], action: hint[1], priority: undefined };
		return { key, action, rank: action === "close" ? Number.POSITIVE_INFINITY : (priority ?? hints.length - index) };
	});
}

const cellsOf = (kept: Ranked[], sepWidth: number) =>
	kept.reduce((sum, hint, i) => sum + visibleWidth(hint.key) + 1 + visibleWidth(hint.action) + (i ? sepWidth : 0), 0);

/** The hints that fit width, in their given order: the lowest-ranked dropped first (the later on a tie). */
export function fitHints(hints: readonly KeyHint[], width: number, sepWidth = SEP.length): Array<[string, string]> {
	const kept = ranked(hints);
	while (kept.length && cellsOf(kept, sepWidth) > Math.max(0, width)) {
		let drop = 0;
		for (let i = 1; i < kept.length; i++) if (kept[i]!.rank <= kept[drop]!.rank) drop = i;
		kept.splice(drop, 1);
	}
	return kept.map((hint) => [hint.key, hint.action]);
}

/** The cells fitted hints take on one line. */
export function hintCells(hints: Array<[string, string]>): number {
	return cellsOf(hints.map(([key, action]) => ({ key, action, rank: 0 })), SEP.length);
}

/** Fitted hints painted: keys bold dim, actions dim. */
export function paintHints(hints: Array<[string, string]>, paint: Painter): string {
	return hints.map(([key, action]) => `${paint.bold(paint.fg("dim", key))} ${paint.fg("dim", action)}`).join(SEP);
}
