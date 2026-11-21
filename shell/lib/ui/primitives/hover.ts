/**
 * shell/lib/ui/primitives/hover.ts — the one hover look: every clickable text
 * (a footer hint, a rail header, a list row) turns to the warning role while
 * the pointer is over it. Each surface tracks its own hover; this only paints
 * it. Multiplexers (tmux, zellij, screen) forward no plain pointer moves, so
 * there no hover ever shows; clicks still work.
 */

/** Anything that can paint text in a theme role. */
export type HoverTheme = { fg: (role: string, text: string) => string };

/** The theme role every hovered target is painted in. */
export const HOVER_ROLE = "warning" as const;

/** text in the hover role while hovered, else in idleRole (or as it is). */
export function paintHover(theme: HoverTheme, text: string, hovered: boolean, idleRole?: string): string {
	const role = hovered ? HOVER_ROLE : idleRole;
	return role === undefined ? text : theme.fg(role, text);
}
