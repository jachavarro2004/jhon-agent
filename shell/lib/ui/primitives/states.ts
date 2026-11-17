/**
 * shell/lib/ui/primitives/states.ts — the one state table (design/system.md
 * "Badge and state glyph", the owner's L65 choice): the glyph, its ASCII
 * form, its colour role and its word, shared by the shell and the dashboard. `●` means new, and only new.
 */
import type { Role } from "./paint.ts";

export interface StateEntry { glyph: string; ascii: string; role: Role; word: string }

export const STATES = {
	done: { glyph: "✓", ascii: "+", role: "ok", word: "done" },
	pending: { glyph: "·", ascii: ".", role: "dim", word: "pending" },
	blocked: { glyph: "◉", ascii: "!", role: "bad", word: "blocked" },
	new: { glyph: "●", ascii: "*", role: "accent", word: "new" },
	working: { glyph: "◐", ascii: "~", role: "warn", word: "working" },
	failed: { glyph: "✗", ascii: "x", role: "bad", word: "failed" },
	unknown: { glyph: "?", ascii: "?", role: "dim", word: "unknown" },
} as const satisfies Record<string, StateEntry>;

export type StateName = keyof typeof STATES;

export function stateGlyph(name: StateName, ascii = false): string {
	return ascii ? STATES[name].ascii : STATES[name].glyph;
}
