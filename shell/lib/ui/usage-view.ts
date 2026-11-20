/**
 * shell/lib/ui/usage-view.ts — the usage overlay (/jhon:usage, alt+u;
 * T174-S5b, L1493-L1495): the overlay box with `Subscriptions` (and
 * `· refreshing…` while a refresh runs), each provider's windows
 * (usage.ts usageRows), and the key line `r refresh  esc close`. It reads the
 * tracker on every render, so a settled refresh only has to repaint.
 */
import { Key, matchesKey } from "@earendil-works/pi-tui";
import { BOX_PADDING, fitHints, paintHints, renderBox, themePainter, type ThemeLike } from "./primitives/index.ts";
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

export class UsageOverlay {
	readonly #deps: UsageOverlayDeps;
	#refreshing = false;

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
		return renderBox([title, "", ...body, "", paintHints(fitHints(HINTS, room), paint)], width, this.#deps.theme, ascii);
	}

	invalidate(): void {}
}
