/**
 * shell/lib/ui/overlay.ts — how Jhon's views sit over the screen
 * (design/shell-tui 05-13):
 *
 *   - the card (05 Customize, 12 Grants): a rounded frame in the border role
 *     on the Frame surface, `<title>` bold accent one cell in with `esc` at
 *     the right on its first row, the body two cells in from each side;
 *   - its size: the artboards' width, centred, at most 85% of the rows; a
 *     terminal narrower than the card gives it every column;
 *   - the close: the view goes, then every cell is written again;
 *   - the dock: the ask view (08) takes the prompt's place, never an overlay.
 *
 * pi cannot dim what lies under an overlay (L1469): the card is drawn over
 * the screen as it is. The full-screen views (06, 10, 11, 13) draw their own
 * frame (fullscreen-view.ts).
 */
import { Container, visibleWidth, type Component, type TuiMouseEvent } from "@earendil-works/pi-tui";
import { clip, glyph, paintSurfaceRows, TINT } from "./primitives/index.ts";

/** Customize (05) and the usage card: 104 columns at 180, as the artboard. */
export const CARD_OVERLAY = { overlay: true, overlayOptions: { anchor: "center", width: 104, maxHeight: "85%" } } as const;
/** The grants (12): 96 columns. */
export const GRANTS_OVERLAY = { overlay: true, overlayOptions: { anchor: "center", width: 96, maxHeight: "85%" } } as const;

/** The cells a card takes around its body: a side and two spaces on each side. */
export const CARD_BODY_PADDING = 6;

export interface OverlayCardTheme {
	fg(role: string, text: string): string;
	bold?(text: string): string;
	/** Without it (a test theme) the card has no surface. */
	bg?(role: string, text: string): string;
}

export interface OverlayCardInput {
	title: string;
	/** What the first row shows at its right: `esc` unless said. */
	right?: string;
	/** The rows under the title, each cut to the body's width. */
	body: readonly string[];
	width: number;
	theme: OverlayCardTheme;
	ascii?: boolean;
}

/** The card: frame, title row, body; every row exactly width cells. */
export function renderOverlayCard({ title, right = "esc", body, width, theme, ascii = false }: OverlayCardInput): string[] {
	const cells = Math.max(CARD_BODY_PADDING + 1, Math.floor(width));
	const edge = (text: string) => theme.fg("border", text);
	const side = edge(glyph("side", ascii));
	const run = glyph("rule", ascii).repeat(cells - 2);
	const fitted = (row: string, room: number) => {
		const shown = clip(row, room, { ascii });
		return `${shown}${" ".repeat(Math.max(0, room - visibleWidth(shown)))}`;
	};
	const head = cells - 4;
	const tail = clip(right, Math.max(0, head - 2), { ascii });
	const name = clip(title, Math.max(0, head - visibleWidth(tail) - (tail ? 1 : 0)), { ascii });
	const accent = (text: string) => theme.fg("accent", theme.bold ? theme.bold(text) : text);
	const gap = " ".repeat(Math.max(0, head - visibleWidth(name) - visibleWidth(tail)));
	const rows = [
		edge(`${glyph("topLeft", ascii)}${run}${glyph("topRight", ascii)}`),
		`${side} ${accent(name)}${gap}${tail ? theme.fg("accent", tail) : ""} ${side}`,
		...body.map((row) => `${side}  ${fitted(row, cells - CARD_BODY_PADDING)}  ${side}`),
		edge(`${glyph("bottomLeft", ascii)}${run}${glyph("bottomRight", ascii)}`),
	];
	return paintSurfaceRows(rows, theme, TINT.band, cells);
}

/** A forced render: what the close asks of pi's TUI. */
export interface Repaintable {
	requestRender(force?: boolean): void;
}

/**
 * An overlay's done that, after the view is gone, has pi write every cell
 * again rather than only the rows it thinks changed: a view that covered the
 * screen can leave cells pi no longer tracks, and the screen then stops
 * following the keys (seen after /jhon:agents and esc). The rows are not
 * rebuilt, which on a long session takes seconds. A terminal that cannot be
 * written never breaks the close; a done that fails is not hidden.
 */
export function closeWithRepaint<T>(tui: Repaintable, done: (result: T) => void): (result: T) => void {
	return (result: T) => {
		done(result);
		try {
			tui.requestRender(true);
		} catch {
			// The terminal is gone: nothing left to repaint.
		}
	};
}

/**
 * A view in the dock (the ask view): keys go to the view, the pointer through
 * pi's Container, which hands the view its own rows. handleMouse is defined
 * here, not inherited, because pi passes over a layout container whose
 * handleMouse is Container's own, and the view would never see a click.
 */
export class DockedView extends Container {
	readonly #view: Component;

	constructor(view: Component) {
		super();
		this.#view = view;
		this.addChild(view);
	}

	handleInput(data: string): void {
		this.#view.handleInput?.(data);
	}

	override handleMouse(event: TuiMouseEvent): ReturnType<Container["handleMouse"]> {
		return super.handleMouse(event);
	}
}
