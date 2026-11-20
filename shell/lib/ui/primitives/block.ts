/**
 * shell/lib/ui/primitives/block.ts — the tinted block, design/shell-tui 01's
 * card: no outline, only the Box surface. The header row is one cell in: the
 * icon (bold accent, bold bad on an error), the title, and the state ending
 * one cell before the edge. Body rows are three cells in and cut with `…`
 * two cells before the edge. An error's header sits on errorContainer.
 * Every row is exactly width cells.
 */
import { visibleWidth } from "@earendil-works/pi-tui";
import { clip, fit } from "./fit.ts";
import { TINT, paintSurface, surfaceOpener, type SurfaceTheme } from "./tint.ts";

export interface BlockTheme extends SurfaceTheme {
	bold?(text: string): string;
}

export interface TintedBlockInput {
	width: number;
	/** One glyph (a tool icon, a state), already in its ASCII form when ascii is on. */
	icon: string;
	/** The title, painted by the caller. */
	title: string;
	/** The state at the right end of the header (`✓ 2.3s`), painted by the caller. */
	right?: string;
	/** Body rows, painted by the caller. */
	body?: readonly string[];
	tone?: "normal" | "error";
	theme: BlockTheme;
	ascii?: boolean;
}

const HEAD_INSET = 1;
const BODY_INSET = 3;
const BODY_END = 2;
const STATE_GAP = 2;
// The title keeps at least this many cells before the state drops.
const MIN_TITLE = 12;

export function renderTintedBlock({ width, icon, title, right, body = [], tone = "normal", theme, ascii = false }: TintedBlockInput): string[] {
	const cells = Math.max(0, Math.floor(width));
	const bold = (text: string) => (theme.bold ? theme.bold(text) : text);
	const head = `${bold(theme.fg(tone === "error" ? "error" : "accent", icon))} ${title}`;
	const inner = cells - HEAD_INSET * 2;
	const titleRoom = right ? inner - visibleWidth(right) - STATE_GAP : -1;
	const header = titleRoom >= MIN_TITLE
		? `${" ".repeat(HEAD_INSET)}${fit(head, titleRoom, { ascii })}${" ".repeat(STATE_GAP)}${right}${" ".repeat(HEAD_INSET)}`
		: `${" ".repeat(HEAD_INSET)}${fit(head, inner, { ascii })}${" ".repeat(HEAD_INSET)}`;
	const rows = body.map((row) => `${" ".repeat(BODY_INSET)}${clip(row, cells - BODY_INSET - BODY_END, { ascii })}`);
	const box = surfaceOpener(theme, TINT.box);
	const top = tone === "error" ? surfaceOpener(theme, TINT.error) : box;
	return [paintSurface(fit(header, cells, { ascii, ellipsis: "" }), top), ...rows.map((row) => paintSurface(fit(row, cells, { ascii, ellipsis: "" }), box))];
}
