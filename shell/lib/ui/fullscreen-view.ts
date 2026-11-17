/**
 * shell/lib/ui/fullscreen-view.ts — Jhon's work views near full screen, drawn
 * as the Gentle Shell draws its profiles view (gentle-ai.ts: RoutingTablePanel
 * renderFrame and tableLines, showRoutingTablePanel's overlay options; L174):
 * the whole terminal, a rounded frame in the border role with `│ ` margins;
 * inside, the view's tab and its rule (the active tab's `━` in the accent),
 * the title in the accent with a muted sentence, a blank row, the body, a
 * blank row, the key hints dim joined by two spaces, and two feedback rows;
 * blank rows fill what is left, as G pads its body. The LDD detail (alt+j)
 * and /jhon:profile use it; /jhon:grants and Customize stay small cards.
 */
import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import { asciiText } from "./primitives/index.ts";

/** G's showRoutingTablePanel: the overlay takes the whole terminal. */
export const FULLSCREEN_OVERLAY = { overlay: true, overlayOptions: { anchor: "center", width: "100%", maxHeight: "100%", margin: 0 } } as const;

export interface FullscreenViewOptions {
	theme: { fg(role: string, text: string): string };
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
function fitHints(hints: string[], room: number): string {
	const last = hints.at(-1);
	const kept = hints.slice(0, -1);
	const join = (list: string[]) => ` ${list.join("  ")}`;
	while (kept.length && visibleWidth(join([...kept, ...(last ? [last] : [])])) > room) kept.pop();
	return join([...kept, ...(last ? [last] : [])]);
}

export function renderFullscreenView(o: FullscreenViewOptions): string[] {
	const ascii = o.ascii ?? false;
	const t = o.theme;
	const width = Math.max(4, Math.floor(o.width));
	const inner = Math.max(1, width - 4);
	const plain = (text: string) => ascii ? asciiText(text) : text;
	// One row is one terminal line, cut with G's ellipsis and padded to the inner width.
	const fit = (line: string) => {
		const shown = visibleWidth(line) > inner ? truncateToWidth(line, inner, ascii ? "~" : "…") : line;
		return shown + " ".repeat(Math.max(0, inner - visibleWidth(shown)));
	};
	const tab = ` ${plain(o.tab)} `;
	const tabEnd = 1 + visibleWidth(tab);
	const rule = t.fg("border", (ascii ? "-" : "─").repeat(Math.min(1, inner))) + t.fg("accent", (ascii ? "=" : "━").repeat(Math.max(0, Math.min(inner, tabEnd) - 1))) + t.fg("border", (ascii ? "-" : "─").repeat(Math.max(0, inner - tabEnd)));
	const title = plain(o.title);
	const sentence = o.sentence ? plain(o.sentence) : "";
	const head = ` ${t.fg("accent", title)}${sentence && visibleWidth(` ${title}   ${sentence}`) <= inner ? `   ${t.fg("muted", sentence)}` : ""}`;
	const [first = "", second = ""] = o.feedback ?? [];
	const lines = [
		` ${t.fg("accent", tab)}`, rule, head, "",
		...o.body.map((line) => ascii ? asciiText(line) : line), "",
		t.fg("dim", plain(fitHints(o.hints, inner))),
		first ? t.fg("muted", plain(first)) : "", second ? t.fg("muted", plain(second)) : "",
	];
	const rows = Math.max(1, Math.floor(o.rows) - 2);
	const padded = lines.length < rows ? [...lines, ...Array<string>(rows - lines.length).fill("")] : lines.slice(0, rows);
	const h = ascii ? "-" : "─";
	const v = t.fg("border", ascii ? "|" : "│");
	return [
		t.fg("border", `${ascii ? "+" : "╭"}${h.repeat(width - 2)}${ascii ? "+" : "╮"}`),
		...padded.map((line) => `${v} ${fit(line)} ${v}`),
		t.fg("border", `${ascii ? "+" : "╰"}${h.repeat(width - 2)}${ascii ? "+" : "╯"}`),
	];
}
