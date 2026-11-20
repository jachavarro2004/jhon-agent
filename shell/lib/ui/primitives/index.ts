/**
 * shell/lib/ui/primitives — the shell's design system as pure render
 * functions (design/system.md "The shell's design system", T15.1): each takes
 * a width, its data, a painter and the ASCII flag, and returns lines of
 * exactly that width. The frame primitives (T174-S2): fit (clip and fit),
 * rule (rules and pane dividers), tint (surfaces), block (design/shell-tui's
 * tinted block), card (the conversation card), box (the overlay box), hover.
 */
export * from "./fit.ts";
export * from "./rule.ts";
export * from "./tint.ts";
export * from "./block.ts";
export * from "./card.ts";
export * from "./box.ts";
export * from "./hover.ts";
export * from "./paint.ts";
export * from "./glyphs.ts";
export * from "./key-hint.ts";
export * from "./overlay-pattern.ts";
export * from "./states.ts";
export * from "./meter.ts";
export * from "./state-block.ts";
export * from "./list-row.ts";
export * from "./tool-icons.ts";
