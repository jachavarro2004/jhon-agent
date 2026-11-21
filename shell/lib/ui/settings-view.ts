/**
 * shell/lib/ui/settings-view.ts — the Customize overlay (Settings until
 * L174; design/shell-tui 05 and 07, the Gentle Shell's Customization view as
 * a design reference): the overlay card (overlay.ts) titled `Customize … esc`, CATEGORIES on the
 * left and `<Category> · n/m` over the selected category's settings on the
 * right, each setting one line with its values inline (`● current  ○ other`,
 * `‹ value › n/m` when they do not fit), a Preview of the highlighted row,
 * and `Click · <keys>` at the bottom; at most 24 rows. Under 60 columns the
 * categories take the whole card until one is opened. Keys as G's: ↑/↓ (or
 * j/k) move in the focused pane, → Tab Enter or Space open a category, ←/→ on
 * a setting change its value and apply it (← past the first value goes back),
 * Tab returns, Enter on a free value edits it in place (Enter saves, Esc
 * cancels), Esc closes. A click picks a category, a setting or a value. Pure
 * over its rows: the shell wires what a change does (extensions/ui.ts).
 */
import { matchesKey, sliceByColumn, truncateToWidth, visibleWidth, type TuiMouseEvent, type TuiMouseEventResult } from "@earendil-works/pi-tui";
import { asciiText, glyph, paintHover, renderPick } from "./primitives/index.ts";
import { CARD_BODY_PADDING, renderOverlayCard } from "./overlay.ts";
import { stripAnsi } from "./terminal-theme.ts";

export interface SettingRow {
	/** The category's title (Appearance, Behaviour, Keys…). */
	group: string;
	/** The settings key. */
	key: string;
	label: string;
	/** The value as the row shows it. */
	value(): string;
	/** A choice's values in order; without them Enter edits a free value. */
	options?: readonly string[];
	/** What the setting does: the Preview's headline on a focused choice, its last line otherwise. */
	note?: string;
	/** An option's name as shown (the value is kept). */
	display?(option: string): string;
	/** The highlighted row's sample for a value: what the Preview shows. */
	preview?(value: string, ascii: boolean): { caption?: string; lines: string[] };
	/** ←/→ only move a pending choice and enter applies it; esc or leaving the row drops it (the permission mode). Every other row applies at once, as G's. */
	confirm?: boolean;
	/** Enter runs it (Reset, Reload now): no values, never edited in place; with confirm, the first enter only holds it. */
	action?: boolean;
	/** Apply a new value: undefined when done, else why not. A row with no apply is read-only. */
	apply?(value: string): string | undefined | Promise<string | undefined>;
}

export interface SettingsViewOptions {
	rows: readonly SettingRow[];
	theme: { fg(role: string, text: string): string; bold?(text: string): string; bg?(role: string, text: string): string };
	/** The rows the overlay may take, frame included; the card keeps to 24, as G's. */
	height(): number;
	/** The category to open on (case-insensitive), its settings focused. */
	group?: string;
	/** Plain glyphs (shell.appearance.ascii). */
	ascii?(): boolean;
	/** A refused value, as G's onError: the shell notifies it; the Preview keeps its headline. */
	onError?(message: string): void;
	onClose(): void;
	requestRender(): void;
}

const EDIT_CURSOR = "▏";
const TITLE = "Customize";
const MAX_HEIGHT = 24;

/** The first row of a window of `visible` rows over `length` that keeps row `position` in sight, the window kept full. */
const scrollTop = (position: number, length: number, visible: number): number => Math.max(0, Math.min(position + 1 - visible, length - visible));

/** Preview rows under the list by the card's height: the list gets the room first. */
const PREVIEW_STEPS: ReadonlyArray<readonly [minHeight: number, rows: number]> = [[18, 6], [11, 3], [8, 1]];

/**
 * How the card's rows and columns are shared at this size. Two panes from
 * SETTINGS_TWO_PANES columns, the categories taking 28% of the inner width
 * within 14..20 cells plus a 3-cell divider; the headings and the click
 * footer from 6 rows, the gap under the headings from 12, the one above the
 * footer from 9; the list keeps what is left, at least one row.
 */
function cardLayout(inner: number, height: number, narrow: boolean) {
	const leftWidth = narrow ? 0 : Math.max(14, Math.min(20, Math.floor(inner * 0.28)));
	const previewLines = PREVIEW_STEPS.find(([minHeight]) => height >= minHeight)?.[1] ?? 0;
	// The rows each optional part needs before it shows.
	const [showHeading, showTopGap, showBottomGap] = [6, 12, 9].map((rows) => height >= rows);
	// The overlay card's top border, title row and bottom border.
	let taken = 3 + previewLines;
	if (showHeading) taken += 2;
	if (showTopGap) taken += 1;
	if (showBottomGap) taken += 1;
	return { leftWidth, rightWidth: inner - leftWidth - (leftWidth > 0 ? 3 : 0), previewLines, showHeading, showTopGap, showBottomGap, contentHeight: Math.max(1, height - taken) };
}
/** Under this many columns the categories take the whole card until one is opened, as G's. */
export const SETTINGS_TWO_PANES = 60;

type Pane = "categories" | "controls";

interface Hit {
	id: string;
	x: number;
	end: number;
	y: number;
	activate(): void;
}

interface Frame {
	width: number;
	height: number;
	hits: Hit[];
}

export class SettingsView {
	private readonly o: SettingsViewOptions;
	private readonly categories: string[];
	private categoryAt = 0;
	private pane: Pane = "categories";
	private readonly cursors = new Map<string, number>();
	private editing: string | undefined;
	private busy = false;
	private frame: Frame | undefined;
	private hovered: string | undefined;
	/** A confirm row's pending choice, applied only by enter. */
	private pending: { row: SettingRow; value: string } | undefined;

	constructor(options: SettingsViewOptions) {
		this.o = options;
		this.categories = [...new Set(options.rows.map((row) => row.group))];
		const named = options.group ? this.categories.findIndex((name) => name.toLowerCase() === options.group!.toLowerCase()) : -1;
		if (named >= 0) {
			this.categoryAt = named;
			this.pane = "controls";
		}
	}

	private category(): string {
		return this.categories[this.categoryAt] ?? TITLE;
	}

	private rows(): SettingRow[] {
		return this.o.rows.filter((row) => row.group === this.category());
	}

	private cursor(): number {
		return Math.min(this.cursors.get(this.category()) ?? 0, Math.max(0, this.rows().length - 1));
	}

	private settle(row: SettingRow, result: string | undefined): void {
		this.busy = false;
		if (result !== undefined) this.o.onError?.(result);
		this.o.requestRender();
	}

	private apply(row: SettingRow, value: string): void {
		if (!row.apply) {
			this.o.onError?.(`${row.label} is not changed here${row.note ? `: ${row.note}` : ""}`);
			return;
		}
		this.busy = true;
		try {
			const result = row.apply(value);
			if (result instanceof Promise) result.then((r) => this.settle(row, r), (e) => this.settle(row, e instanceof Error ? e.message : String(e)));
			else this.settle(row, result);
		} catch (error) {
			this.settle(row, error instanceof Error ? error.message : String(error));
		}
	}

	/** Step a choice's value by one and apply it; false when there is no value that way. */
	private step(row: SettingRow, delta: number): boolean {
		const options = row.options ?? [];
		const at = options.indexOf(this.shown(row));
		const next = options[at + delta];
		if (at < 0 ? !options[0] : next === undefined) return false;
		this.choose(row, at < 0 ? options[0]! : next!);
		return true;
	}

	/** The value the row shows: its pending choice, else its own. */
	private shown(row: SettingRow): string {
		return this.pending?.row === row ? this.pending.value : row.value();
	}

	/** A confirm row only holds the choice; any other applies it. */
	private choose(row: SettingRow, value: string): void {
		if (!row.confirm) return this.apply(row, value);
		this.pending = value === row.value() ? undefined : { row, value };
	}

	private act(row: SettingRow): void {
		if (row.action && row.confirm && this.pending?.row !== row) {
			this.pending = { row, value: row.value() };
			return;
		}
		if (row.confirm) {
			const chosen = this.pending?.row === row ? this.pending.value : undefined;
			this.pending = undefined;
			if (chosen !== undefined) this.apply(row, chosen);
			return;
		}
		if (row.options?.length || row.action) this.apply(row, row.value());
		else if (row.apply) this.editing = row.value();
		else this.apply(row, row.value());
	}

	handleInput(data: string): void {
		if (this.busy) return;
		this.frame = undefined;
		this.hovered = undefined;
		const rows = this.rows();
		const row = rows[this.cursor()];
		if (this.editing !== undefined) {
			if (matchesKey(data, "escape")) this.editing = undefined;
			else if (matchesKey(data, "enter") || matchesKey(data, "return")) {
				const value = this.editing.trim();
				this.editing = undefined;
				if (row) this.apply(row, value);
			} else if (matchesKey(data, "backspace")) this.editing = this.editing.slice(0, -1);
			else if (printable(data)) this.editing += data;
			this.o.requestRender();
			return;
		}
		if (matchesKey(data, "escape")) {
			if (!this.pending) return this.o.onClose();
			this.pending = undefined;
			return this.o.requestRender();
		}
		const enter = matchesKey(data, "enter") || matchesKey(data, "return") || matchesKey(data, "space");
		const up = matchesKey(data, "up") || data === "k";
		const down = matchesKey(data, "down") || data === "j";
		const length = this.categories.length;
		if (this.pane === "categories") {
			if (up && length) this.categoryAt = (this.categoryAt + length - 1) % length;
			else if (down && length) this.categoryAt = (this.categoryAt + 1) % length;
			else if (matchesKey(data, "right") || matchesKey(data, "tab") || enter) { if (rows.length) this.pane = "controls"; }
			else return;
		} else {
			const choice = Boolean(row?.options?.length);
			if (choice && matchesKey(data, "left")) { if (!this.step(row!, -1)) this.pane = "categories"; }
			else if (choice && matchesKey(data, "right")) this.step(row!, 1);
			else if (matchesKey(data, "left") || matchesKey(data, "tab") || matchesKey(data, "shift+tab")) this.pane = "categories";
			else if (up && rows.length) this.cursors.set(this.category(), (this.cursor() + rows.length - 1) % rows.length);
			else if (down && rows.length) this.cursors.set(this.category(), (this.cursor() + 1) % rows.length);
			else if (enter && row) this.act(row);
			else return;
		}
		// Leaving the row drops its pending choice.
		if (this.pending && (this.pane !== "controls" || this.rows()[this.cursor()] !== this.pending.row)) this.pending = undefined;
		this.o.requestRender();
	}

	/** Mouse coordinates are local to the rendered card, its border included. */
	handleMouse(event: TuiMouseEvent): TuiMouseEventResult | undefined {
		const moving = event.type === "move" && event.button === "none";
		if (!moving && (event.button !== "left" || (event.type !== "press" && event.type !== "click"))) return undefined;
		const frame = this.frame;
		if (!frame || frame.width !== event.width || frame.height !== event.height) {
			if (!moving) return undefined;
			const changed = this.hovered !== undefined;
			this.hovered = undefined;
			return changed ? { handled: true, render: true } : undefined;
		}
		const hit = this.busy || this.editing !== undefined ? undefined : frame.hits.find((item) => item.y === event.y && event.x >= item.x && event.x < item.end);
		if (moving) {
			const changed = this.hovered !== hit?.id;
			this.hovered = hit?.id;
			return { handled: true, render: changed };
		}
		if (!hit) return undefined;
		if (event.type === "press") return { handled: true, capture: true, render: false };
		this.frame = undefined;
		this.hovered = undefined;
		hit.activate();
		this.o.requestRender();
		return { handled: true, render: true };
	}

	render(width: number): string[] {
		const ascii = this.o.ascii?.() ?? false;
		const theme = this.o.theme;
		const text = (value: string) => ascii ? asciiText(value) : value;
		const height = Math.max(0, Math.min(MAX_HEIGHT, Math.floor(this.o.height())));
		if (width < CARD_BODY_PADDING + 1 || height < 4) {
			this.frame = undefined;
			return Array.from({ length: Math.min(height, 2) }, (_, i) => truncateToWidth(i ? "Terminal too small" : TITLE, Math.max(0, width), ""));
		}
		const inner = width - CARD_BODY_PADDING;
		const category = this.category();
		const rows = this.rows();
		const cursor = this.cursor();
		const narrow = width < SETTINGS_TWO_PANES;
		// Narrow and on the categories, the one pane lists them (with more than one to pick).
		const navOnly = this.pane === "categories" && narrow && this.categories.length > 1;
		const { leftWidth, rightWidth, previewLines, showHeading, showTopGap, showBottomGap, contentHeight } = cardLayout(inner, height, narrow);
		const focusedRow = this.pane === "controls" ? rows[cursor] : undefined;
		const focusedSetting = focusedRow?.options?.length ? focusedRow : undefined;
		const position = navOnly ? this.categoryAt : cursor;
		const nameWidth = Math.min(20, Math.max(0, ...rows.map((row) => visibleWidth(row.label))));
		const start = scrollTop(position, navOnly ? this.categories.length : rows.length, contentHeight);
		const navStart = scrollTop(this.categoryAt, this.categories.length, contentHeight);
		const paint = (value: string, size: number, role = "text") => {
			const clipped = truncateToWidth(text(value), Math.max(0, size), glyph("ellipsis", ascii));
			return theme.fg(role, clipped + " ".repeat(Math.max(0, size - visibleWidth(clipped))));
		};
		const divider = glyph("side", ascii);
		const join = (left: string, right: string) => leftWidth ? `${left}${" ".repeat(Math.max(0, leftWidth - visibleWidth(stripAnsi(left))))} ${paint(divider, 1, "border")} ${right}` : right;
		const heading = navOnly ? "Categories · pick one →" : `${category} · ${rows.length ? `${cursor + 1}/${rows.length}` : "empty"}`;
		// The card draws the title row (overlay.ts); these are the rows under it.
		const lines: string[] = [];
		const hits: Hit[] = [];
		const fgOnly = { fg: theme.fg.bind(theme) };
		const openCategory = (index: number) => {
			this.categoryAt = index;
			this.pane = narrow ? "controls" : "categories";
		};
		if (showHeading) {
			const over = leftWidth > 0 ? paint("CATEGORIES", leftWidth, "accent") : "";
			lines.push(join(over, paint(heading, rightWidth, "accent")));
		}
		// The divider runs through the gap under the headings, as artboard 05.
		if (showTopGap) lines.push(join("", ""));
		const rightX = 3 + (leftWidth ? leftWidth + 3 : 0);
		for (let i = 0; i < contentHeight; i++) {
			const y = lines.length + 2; // the card adds its top border and title row
			const nav = this.categories[navStart + i];
			const left = leftWidth && nav ? renderPick(nav, leftWidth, navStart + i === this.categoryAt, this.pane === "categories" ? theme : fgOnly, ascii) : "";
			if (leftWidth && nav) hits.push({ id: `category:${navStart + i}`, x: 5, end: 5 + Math.min(visibleWidth(nav), leftWidth - 2), y, activate: () => openCategory(navStart + i) });
			let right: string;
			if (navOnly) {
				const name = this.categories[start + i];
				right = name ? renderPick(name, rightWidth, start + i === position, theme, ascii) : "";
				if (name) hits.push({ id: `category:${start + i}`, x: rightX + 2, end: rightX + 2 + Math.min(visibleWidth(name), rightWidth - 2), y, activate: () => openCategory(start + i) });
			} else {
				const index = start + i;
				const row = rows[index];
				right = row ? this.settingLine(row, index, rightWidth, nameWidth, this.pane === "controls" && index === cursor, ascii, rightX, y, hits) : rows.length || i ? "" : paint("Nothing to set here", rightWidth, "muted");
			}
			lines.push(join(left, right));
		}
		if (this.editing !== undefined && focusedRow) {
			const field = `${focusedRow.label}: ${this.editing}${EDIT_CURSOR}`;
			if (previewLines > 0) {
				lines.push(paint(field, inner, "accent"));
				if (previewLines > 1) lines.push(paint("Enter applies · Esc cancels", inner, "muted"));
			}
		} else if (previewLines >= 3) {
			const row = navOnly ? undefined : rows[cursor];
			const value = row ? this.shown(row) : "";
			let sample: { caption?: string; lines: string[] } | undefined;
			try { sample = row?.preview?.(value, ascii); } catch { /* An unreadable sample is not shown. */ }
			const held = row && this.pending?.row === row ? row.display?.(value) ?? value : undefined;
			const headline = navOnly ? "→ the settings · Esc closes"
				: held !== undefined ? `${row!.label} — ${held}: enter applies, esc drops it`
				: focusedSetting?.note ? `${focusedSetting.label} — ${focusedSetting.note}`
				: sample ? `Preview · ${[value, sample.caption].filter(Boolean).join(" · ")}`
				: row?.note ? `Preview · ${value}` : "No preview";
			lines.push("");
			lines.push(paint(headline, inner, "accent"));
			const body = navOnly ? [""] : [...(sample?.lines ?? []), ...(row?.note && !focusedSetting?.note ? [row.note] : [])];
			if (!navOnly && !body.length) body.push("This choice has no sample");
			for (let i = 0; i < previewLines - 2; i++) lines.push(paint(body[i] ?? "", inner, "muted"));
		} else if (previewLines === 1) lines.push(paint(navOnly ? "→ the settings · Esc closes" : "No room for the preview", inner, "muted"));
		if (showBottomGap) lines.push("");
		if (showHeading) lines.push(paint(`Click · ${this.footer(navOnly, previewLines, focusedSetting !== undefined)}`, inner, "muted"));
		const card = renderOverlayCard({ title: TITLE, body: lines.slice(0, height - 3), width, theme, ascii });
		return this.installFrame(card, { width, height: card.length, hits: hits.filter((hit) => hit.y < card.length - 1) });
	}

	private footer(navOnly: boolean, previewLines: number, setting: boolean): string {
		if (navOnly) return "↑/↓ category · → opens it · Esc closes";
		if (setting) return "↑/↓ setting · ←/→ changes it · Tab categories · Esc closes";
		return previewLines ? "←/→ or Tab panes · ↑/↓ moves · Enter applies · Esc closes" : "No preview room · ←/→ panes · ↑/↓ moves · Enter applies · Esc closes";
	}

	/** One setting with its values inline (● current, ○ other); too wide, the current value alone between arrows; a free value as text. */
	private settingLine(row: SettingRow, index: number, width: number, nameWidth: number, focused: boolean, ascii: boolean, x: number, y: number, hits: Hit[]): string {
		const theme = this.o.theme;
		const text = (value: string) => ascii ? asciiText(value) : value;
		const name = text(row.label);
		const lead = `${name}${" ".repeat(Math.max(0, nameWidth - visibleWidth(name)))}  `;
		const bodyStart = 2 + visibleWidth(lead);
		const select = () => { this.pane = "controls"; this.cursors.set(this.category(), index); };
		hits.push({ id: `setting:${index}`, x: x + 2, end: x + Math.min(width, 2 + visibleWidth(name)), y, activate: select });
		const current = this.shown(row);
		const waiting = this.pending?.row === row;
		const show = (option: string) => text(row.display?.(option) ?? option);
		let body: string;
		const options = row.options ?? [];
		if (!options.length) {
			body = theme.fg(focused ? "text" : "muted", text(current)) + (waiting ? theme.fg("warning", " · enter applies") : "");
		} else {
			const mark = (option: string) => theme.fg(option === current ? "accent" : "muted", `${glyph(option === current ? "radioOn" : "radioOff", ascii)} ${show(option)}`);
			body = options.map(mark).join("  ") + (waiting ? theme.fg("warning", " · enter applies") : "");
			if (bodyStart + visibleWidth(body) > width) {
				const at = Math.max(0, options.indexOf(current));
				body = `${theme.fg("accent", `${glyph("tabLeft", ascii)} ${show(options[at]!)} ${glyph("tabRight", ascii)}`)} ${theme.fg("muted", `${at + 1}/${options.length}`)}`;
			} else {
				let start = bodyStart;
				for (const option of options) {
					const length = visibleWidth(`${glyph("radioOff", ascii)} ${show(option)}`);
					hits.push({ id: `choice:${index}:${option}`, x: x + start, end: x + start + length, y, activate: () => { select(); if (option !== this.shown(row)) this.choose(row, option); } });
					start += length + 2;
				}
			}
		}
		const line = truncateToWidth(`${focused ? theme.fg("accent", `${glyph("cursor", ascii)} `) : "  "}${theme.fg(focused ? "text" : "muted", lead)}${body}`, width, glyph("ellipsis", ascii));
		const padded = line + " ".repeat(Math.max(0, width - visibleWidth(line)));
		return focused && theme.bg ? theme.bg("selectedBg", padded) : padded;
	}

	/** Keep the painted hits, and paint the hovered one in the shared hover role. */
	private installFrame(lines: string[], next: Frame): string[] {
		const hit = next.hits.find((item) => item.id === this.hovered);
		if (!hit) this.hovered = undefined;
		else {
			const line = lines[hit.y]!;
			const trailing = line.match(/(?:\x1b\[[0-9;:]*m)+$/)?.[0] ?? "";
			const target = stripAnsi(sliceByColumn(line, hit.x, hit.end - hit.x, true));
			lines[hit.y] = sliceByColumn(line, 0, hit.x, true) + paintHover(this.o.theme, target, true) + sliceByColumn(line, hit.end, next.width - hit.end, true) + trailing;
		}
		this.frame = next;
		return lines;
	}

	invalidate(): void {
		this.frame = undefined;
		this.hovered = undefined;
	}
}

/** One printable character typed (a paste is not a keystroke). */
function printable(data: string): boolean {
	if ([...data].length !== 1) return false;
	const code = data.codePointAt(0) ?? 0;
	return code >= 0x20 && code !== 0x7f;
}
