/**
 * shell/lib/ui/usage-view.ts — the usage overlay (/jhon:usage, alt+u;
 * T174-S5b, L1493-L1495): the overlay box with `Subscriptions` (and
 * `· refreshing…` while a refresh runs), each provider's windows
 * (usage.ts usageRows), and the key line `r refresh  esc close`. Its hints
 * are controls too: the pointer over one paints it in the hover role, a click
 * does what its key does. It reads the tracker on every render, so a settled
 * refresh only has to repaint.
 */
import { Key, matchesKey, visibleWidth, type TuiMouseEvent } from "@earendil-works/pi-tui";
import { BOX_PADDING, fitHints, HOVER_ROLE, renderBox, themePainter, type ThemeLike } from "./primitives/index.ts";
import { usageRows, type UsageTracker } from "./usage.ts";

export interface UsageOverlayDeps {
	tracker: Pick<UsageTracker, "get" | "failed" | "note">;
	theme: ThemeLike;
	now(): number;
	/** The providers to list, in order (usage.ts usageScope). */
	scope(): readonly string[];
	/** The session's provider. */
	active(): string | undefined;
	/** A forced refresh of every source in scope; its failure is the tracker's to show. */
	refresh(): Promise<void>;
	close(): void;
	requestRender(): void;
	ascii?: boolean;
}

const HINTS = [["r", "refresh"], ["esc", "close"]] as const;
/** Cells between two hints. */
const HINT_GAP = 2;

/** Where each fitted hint sits on its row, counted from the first cell inside the box. */
function hintSpans(hints: Array<[string, string]>): Array<{ hint: string; from: number; to: number }> {
	let at = 0;
	return hints.map(([hint, action]) => {
		const from = at;
		at += visibleWidth(hint) + 1 + visibleWidth(action);
		const span = { hint, from, to: at };
		at += HINT_GAP;
		return span;
	});
}

export class UsageOverlay {
	readonly #deps: UsageOverlayDeps;
	#refreshing = false;
	/** The hint under the pointer, by its key. */
	#hover: string | undefined;
	/** The last frame's hints and the row they sit on. */
	#drawn: { row: number; hints: Array<[string, string]> } | undefined;

	constructor(deps: UsageOverlayDeps) {
		this.#deps = deps;
	}

	/** r refreshes; esc or q closes; any other key does nothing. */
	handleInput(data: string): void {
		if (matchesKey(data, Key.escape) || data === "q") {
			this.#deps.close();
			return;
		}
		if (data === "r") this.refresh();
	}

	/** The key hint under the pointer: a move paints it, a left click does what its key does. */
	handleMouse(event: TuiMouseEvent): { handled: true; render?: boolean } | undefined {
		const drawn = this.#drawn;
		const x = event.x - BOX_PADDING / 2;
		const hit = drawn && event.y === drawn.row ? hintSpans(drawn.hints).find((span) => x >= span.from && x < span.to)?.hint : undefined;
		if (event.type === "move") {
			if (hit === this.#hover) return undefined;
			this.#hover = hit;
			this.#deps.requestRender();
			return { handled: true, render: true };
		}
		// pi makes a click only for the component that took its press.
		if (event.type === "press" && event.button === "left") return hit ? { handled: true } : undefined;
		if (event.type !== "click" || event.button !== "left" || !hit) return undefined;
		if (hit === "esc") this.#deps.close();
		else this.refresh();
		return { handled: true };
	}

	/** One refresh at a time; it never throws past here, and the title says it runs. */
	refresh(): void {
		if (this.#refreshing) return;
		this.#refreshing = true;
		this.#deps.requestRender();
		const settle = () => { this.#refreshing = false; this.#deps.requestRender(); };
		try { this.#deps.refresh().then(settle, settle); } catch { settle(); }
	}

	render(width: number): string[] {
		const paint = themePainter(this.#deps.theme);
		const ascii = this.#deps.ascii ?? false;
		const room = Math.max(1, width - BOX_PADDING);
		const title = paint.bold(paint.fg("accent", "Subscriptions")) + (this.#refreshing ? paint.fg("dim", ascii ? " - refreshing..." : " · refreshing…") : "");
		const body = usageRows({ scope: this.#deps.scope(), active: this.#deps.active(), tracker: this.#deps.tracker, now: this.#deps.now(), ascii }, this.#deps.theme, room);
		const hints = fitHints(HINTS, room);
		const theme = this.#deps.theme;
		// As paintHints draws them (keys bold), the one under the pointer in the hover role.
		const line = hints.map(([hint, action]) => {
			const role = hint === this.#hover ? HOVER_ROLE : "dim";
			return `${theme.bold(theme.fg(role, hint))} ${theme.fg(role, action)}`;
		}).join(" ".repeat(HINT_GAP));
		const rows = renderBox([title, "", ...body, "", line], width, theme, ascii);
		this.#drawn = { row: rows.length - 2, hints };
		return rows;
	}

	invalidate(): void {}
}
