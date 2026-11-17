import { matchesKey, truncateToWidth, wrapTextWithAnsi, type Component, type TuiMouseEvent, type TuiMouseEventResult } from "@earendil-works/pi-tui";
import { asciiText, glyph } from "./primitives/index.ts";
import { paintHoverable } from "./shell-hover.ts";

// ask_user drawn as the Gentle Shell's questionnaire (its ask_user_question,
// lib/questionnaire/questionnaire-view.ts): in the dock, never an overlay,
// between two accent rules: `[n/m]  ▸ Header`, a blank row, the question bold
// accent, each option behind `❯ ` (the focus) or two spaces with its
// description dim four cells in, a blank row and the dim hint. Each item keeps
// the value pi's select dialog would have returned, so lib/ask.ts reads the
// answer the same way; a multiple choice is asked one toggle at a time, so
// space toggles as enter does.

export interface AskItem {
	/** What picking the item returns: the string pi's select dialog would have. */
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
	/** A multiple choice: enter (or space) toggles rather than answers. */
	multi: boolean;
	theme: { fg(color: string, text: string): string; bg?(color: string, text: string): string; bold?(text: string): string };
	done(value: string | undefined): void;
	/** shell.appearance.ascii: the rules, glyphs and text in ASCII. */
	ascii?: boolean;
}

const DESCRIPTION_INDENT = "    ";

export class AskView implements Component {
	private readonly o: AskViewOptions;
	private focus = 0;
	private hovered: number | undefined;
	/** The item each rendered row belongs to, for the pointer. */
	private owners: Array<number | undefined> = [];
	private renderedWidth: number | undefined;

	constructor(options: AskViewOptions) {
		this.o = options;
	}

	render(width: number): string[] {
		const target = Math.max(1, Math.floor(width));
		const { theme, items } = this.o;
		const ascii = this.o.ascii ?? false;
		const text = (t: string) => ascii ? asciiText(t) : t;
		const accent = (t: string) => theme.fg("accent", theme.bold ? theme.bold(t) : t);
		const lines: string[] = [];
		const owners: Array<number | undefined> = [];
		const push = (row: string, owner?: number) => {
			for (const line of wrapTextWithAnsi(row, target)) {
				lines.push(truncateToWidth(line, target, ""));
				owners.push(owner);
			}
		};
		const rule = theme.fg("accent", glyph("rule", ascii).repeat(target));
		lines.push(rule);
		owners.push(undefined);
		push(`${theme.fg("dim", `[${this.o.step}/${this.o.total}]`)}  ${accent(`${glyph("cursor", ascii)} ${text(this.o.title)}`)}`);
		push("");
		push(accent(text(this.o.question)));
		items.forEach((item, index) => {
			const cursor = index === this.focus ? accent(`${glyph("askCursor", ascii)} `) : "  ";
			push(`${cursor}${paintHoverable(theme, text(item.label), this.hovered === index)}`, index);
			if (item.description && target > DESCRIPTION_INDENT.length) {
				for (const row of wrapTextWithAnsi(text(item.description), target - DESCRIPTION_INDENT.length)) {
					lines.push(`${DESCRIPTION_INDENT}${theme.fg("dim", row)}`);
					owners.push(index);
				}
			}
		});
		push("");
		const move = ascii ? "up/down move" : "↑↓ move";
		push(theme.fg("dim", text([move, ...(this.o.multi ? ["space toggle"] : []), "enter select", "esc cancel"].join(" · "))));
		lines.push(rule);
		owners.push(undefined);
		this.owners = owners;
		this.renderedWidth = target;
		return lines;
	}

	handleInput(data: string): void {
		this.hovered = undefined;
		if (matchesKey(data, "up")) this.focus = Math.max(0, this.focus - 1);
		else if (matchesKey(data, "down")) this.focus = Math.min(this.o.items.length - 1, this.focus + 1);
		else if (matchesKey(data, "enter") || (this.o.multi && matchesKey(data, "space"))) this.o.done(this.o.items[this.focus]?.value);
		else if (matchesKey(data, "escape")) this.o.done(undefined);
	}

	/** G's pointer: a move paints the option's label in the hover role, a click picks it. */
	handleMouse(event: TuiMouseEvent): TuiMouseEventResult | undefined {
		if (event.width !== this.renderedWidth) return undefined;
		const owner = this.owners[event.y];
		if (event.type === "move" && event.button === "none") {
			const changed = this.hovered !== owner;
			this.hovered = owner;
			return { handled: true, render: changed };
		}
		if (event.button !== "left" || owner === undefined) return undefined;
		if (event.type === "press") return { handled: true, render: false };
		if (event.type !== "click") return undefined;
		this.focus = owner;
		this.o.done(this.o.items[owner]?.value);
		return { handled: true, render: true };
	}

	invalidate(): void {
		this.hovered = undefined;
	}
}
