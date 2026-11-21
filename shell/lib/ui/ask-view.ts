/**
 * shell/lib/ui/ask-view.ts — the question ask_user puts to the owner, drawn
 * as design/shell-tui 08: in the dock, in the prompt's place, between two
 * accent rules, never over the conversation. One cell in from the left:
 *
 *   [1/2]  ▸ Empty range                       the step dim, the header bold accent
 *
 *   How should the export answer …?            bold accent, wrapped
 *   ❯ 422 with a problem body                  the focus: ❯ and the label bold
 *       Spec S4 as written: …                  a description dim, four cells under
 *     200 with a header-only CSV
 *
 *   ↑↓ move · enter select · esc cancel        dim; `space toggle` in a multiple choice
 *
 * Every row is exactly the width. Each option keeps the value pi's select
 * dialog would return, so lib/ask.ts reads an answer the same way through
 * either. A multiple choice is put one toggle at a time: space toggles, as
 * enter does. The pointer: a move over an option (its label or description)
 * paints its label in the hover role; a press on it is taken, so pi sends the
 * click, and the click picks it.
 */
import { matchesKey, wrapTextWithAnsi, type Component, type TuiMouseEvent, type TuiMouseEventResult } from "@earendil-works/pi-tui";
import { asciiText, fit, glyph, paintHover } from "./primitives/index.ts";

export interface AskItem {
	/** What picking it returns: the string pi's select dialog would have. */
	value: string;
	label: string;
	description?: string;
}

export interface AskViewOptions {
	step: number;
	total: number;
	title: string;
	question: string;
	items: AskItem[];
	/** A multiple choice: space picks as enter does. */
	multi: boolean;
	theme: { fg(role: string, text: string): string; bg?(role: string, text: string): string; bold?(text: string): string };
	done(value: string | undefined): void;
	/** shell.appearance.ascii. */
	ascii?: boolean;
	/** Asked after every key. */
	requestRender?(): void;
}

/** The cells before every row. */
const MARGIN = " ";
/** Before an option's label: the focus mark or two spaces. */
const LEAD = 2;
/** Before a description, after the margin. */
const UNDER = "    ";

export class AskView implements Component {
	readonly #o: AskViewOptions;
	#focus = 0;
	#hovered: number | undefined;
	/** Which option each drawn row belongs to, for the pointer. */
	#rowOwners: Array<number | undefined> = [];
	#drawnWidth: number | undefined;

	constructor(options: AskViewOptions) {
		this.#o = options;
	}

	render(width: number): string[] {
		const cells = Math.max(1, Math.floor(width));
		const { theme, items } = this.#o;
		const ascii = this.#o.ascii ?? false;
		const words = (text: string) => (ascii ? asciiText(text) : text);
		const bold = (text: string) => (theme.bold ? theme.bold(text) : text);
		const accent = (text: string) => theme.fg("accent", bold(text));
		const rows: string[] = [];
		const owners: Array<number | undefined> = [];
		const add = (row: string, owner?: number) => {
			rows.push(fit(row, cells, { ellipsis: "" }));
			owners.push(owner);
		};
		/** Text wrapped to the room after `indent`, each row behind it. */
		const wrapped = (text: string, indent: string, paint: (line: string) => string, owner?: number) => {
			const room = cells - indent.length;
			if (room < 1) return add(indent, owner);
			for (const line of wrapTextWithAnsi(text, room)) add(`${indent}${paint(line)}`, owner);
		};
		const rule = theme.fg("accent", glyph("rule", ascii).repeat(cells));
		rows.push(rule);
		owners.push(undefined);
		add(`${MARGIN}${theme.fg("dim", `[${this.#o.step}/${this.#o.total}]`)}  ${accent(`${glyph("cursor", ascii)} ${words(this.#o.title)}`)}`);
		add("");
		wrapped(words(this.#o.question), MARGIN, accent);
		items.forEach((item, index) => {
			const focused = index === this.#focus;
			const label = words(item.label);
			const [first = "", ...rest] = cells > MARGIN.length + LEAD ? wrapTextWithAnsi(label, cells - MARGIN.length - LEAD) : [label];
			const paint = (text: string) => paintHover(theme, focused ? bold(text) : text, this.#hovered === index, "text");
			add(`${MARGIN}${focused ? accent(`${glyph("askCursor", ascii)} `) : " ".repeat(LEAD)}${paint(first)}`, index);
			for (const line of rest) add(`${MARGIN}${" ".repeat(LEAD)}${paint(line)}`, index);
			if (item.description) wrapped(words(item.description), `${MARGIN}${UNDER}`, (line) => theme.fg("dim", line), index);
		});
		add("");
		const keys = [ascii ? "up/down move" : "↑↓ move", ...(this.#o.multi ? ["space toggle"] : []), "enter select", "esc cancel"];
		add(`${MARGIN}${theme.fg("dim", words(keys.join(" · ")))}`);
		rows.push(rule);
		owners.push(undefined);
		this.#rowOwners = owners;
		this.#drawnWidth = cells;
		return rows;
	}

	handleInput(data: string): void {
		this.#hovered = undefined;
		const last = this.#o.items.length - 1;
		if (matchesKey(data, "up")) this.#focus = Math.max(0, this.#focus - 1);
		else if (matchesKey(data, "down")) this.#focus = Math.max(0, Math.min(last, this.#focus + 1));
		else if (matchesKey(data, "enter") || (this.#o.multi && matchesKey(data, "space"))) this.#o.done(this.#o.items[this.#focus]?.value);
		else if (matchesKey(data, "escape")) this.#o.done(undefined);
		this.#o.requestRender?.();
	}

	handleMouse(event: TuiMouseEvent): TuiMouseEventResult | undefined {
		if (event.width !== this.#drawnWidth) return undefined;
		const owner = this.#rowOwners[event.y];
		if (event.type === "move" && event.button === "none") {
			const changed = owner !== this.#hovered;
			this.#hovered = owner;
			return { handled: true, render: changed };
		}
		if (owner === undefined || event.button !== "left") return undefined;
		if (event.type === "press") return { handled: true, render: false };
		if (event.type !== "click") return undefined;
		this.#focus = owner;
		this.#o.done(this.#o.items[owner]?.value);
		return { handled: true, render: true };
	}

	invalidate(): void {
		this.#hovered = undefined;
	}
}
