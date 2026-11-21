/**
 * shell/lib/ui/fullscreen-view.ts — the views that take the whole terminal
 * (design/shell-tui 06 Profiles, 10 and 11 the LDD change, 13 its editor
 * message; L1465): the LDD detail (alt+j, /jhon:status) and /jhon:profile.
 * The grants and Customize are cards (overlay.ts).
 */
import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import { asciiText, glyph } from "./primitives/index.ts";
import { THEME_NAME } from "./primitives/paint.ts";

/** The overlay takes every row and column. */
export const FULLSCREEN_OVERLAY = { overlay: true, overlayOptions: { anchor: "center", width: "100%", maxHeight: "100%", margin: 0 } } as const;

export interface FullscreenViewOptions {
	theme: { fg(role: string, text: string): string; bold?(text: string): string };
	width: number;
	/** The terminal's rows: the frame takes them all. */
	rows: number;
	/** The view's tab, active: `LDD`, `Profiles`. */
	tab: string;
	title: string;
	/** A muted sentence after the title, dropped when it does not fit. */
	sentence?: string;
	body: string[];
	/** `<key> <action>` each, in order; `esc close` last and kept. */
	hints: string[];
	/** Up to two rows under the hints (G's feedback). */
	feedback?: string[];
	ascii?: boolean;
}

/** The rows before the body (tab, rule, title, blank) and after it (blank, hints, two feedback rows), and the frame's two. */
const CHROME_ROWS = 4 + 4 + 2;

/** How many body rows fit in a terminal this tall. */
export function fullscreenBodyRows(rows: number): number {
	return Math.max(1, Math.floor(rows) - CHROME_ROWS);
}

/** As many hints as fit, in order; the last one (`esc close`) is never dropped. */
function fitHints(hints: string[], room: number): string[] {
	const last = hints.at(-1);
	const kept = hints.slice(0, -1);
	const cells = (list: string[]) => visibleWidth(` ${list.join("  ")}`);
	while (kept.length && cells([...kept, ...(last ? [last] : [])]) > room) kept.pop();
	return [...kept, ...(last ? [last] : [])];
}

/**
 * The view over the whole terminal (design/shell-tui 06, 10, 11, 13): the
 * rounded frame, the tab one cell in with the heavy run under it, the title
 * bold accent and its sentence muted two cells after, the body one cell in,
 * and at the bottom the keys (each key muted, its action dim) and two
 * feedback rows. Every row is exactly width.
 */
export function renderFullscreenView(o: FullscreenViewOptions): string[] {
	const ascii = o.ascii ?? false;
	const t = o.theme;
	const width = Math.max(4, Math.floor(o.width));
	const inner = Math.max(1, width - 4);
	const plain = (text: string) => ascii ? asciiText(text) : text;
	const bold = (text: string) => (t.bold ? t.bold(text) : text);
	// One row is one terminal line, cut with the ellipsis and padded to the inner width.
	const fit = (line: string) => {
		const shown = visibleWidth(line) > inner ? truncateToWidth(line, inner, ascii ? "~" : "…") : line;
		return shown + " ".repeat(Math.max(0, inner - visibleWidth(shown)));
	};
	const tab = plain(o.tab);
	const heavy = Math.min(inner, visibleWidth(tab) + 2);
	const rule = t.fg("accent", glyph("heavyRule", ascii).repeat(heavy)) + t.fg(THEME_NAME.faint, glyph("rule", ascii).repeat(Math.max(0, inner - heavy)));
	const title = plain(o.title);
	const sentence = o.sentence ? plain(o.sentence) : "";
	const head = ` ${t.fg("accent", bold(title))}${sentence && visibleWidth(` ${title}  ${sentence}`) <= inner ? `  ${t.fg("muted", sentence)}` : ""}`;
	const keys = fitHints(o.hints.map(plain), inner).map((hint) => {
		const [key = "", ...action] = hint.split(" ");
		return `${t.fg("muted", key)}${action.length ? t.fg("dim", ` ${action.join(" ")}`) : ""}`;
	});
	const [first = "", second = ""] = o.feedback ?? [];
	const room = Math.max(0, Math.floor(o.rows) - 2);
	const top = [` ${t.fg("accent", bold(tab))}`, rule, head, ""];
	const bottom = ["", ` ${keys.join("  ")}`, first ? t.fg("muted", plain(first)) : "", second ? t.fg("muted", plain(second)) : ""];
	const bodyRoom = Math.max(0, room - top.length - bottom.length);
	const body = o.body.slice(0, bodyRoom).map((line) => ` ${ascii ? asciiText(line) : line}`);
	const lines = [...top, ...body, ...Array<string>(Math.max(0, bodyRoom - body.length)).fill(""), ...bottom];
	const h = glyph("rule", ascii);
	const v = t.fg("border", glyph("side", ascii));
	return [
		t.fg("border", `${glyph("topLeft", ascii)}${h.repeat(width - 2)}${glyph("topRight", ascii)}`),
		...lines.slice(0, Math.max(1, room)).map((line) => `${v} ${fit(line)} ${v}`),
		t.fg("border", `${glyph("bottomLeft", ascii)}${h.repeat(width - 2)}${glyph("bottomRight", ascii)}`),
	];
}
