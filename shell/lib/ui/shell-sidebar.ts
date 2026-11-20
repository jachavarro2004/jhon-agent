import type { Component, TUI } from "@earendil-works/pi-tui";
import type { PanelSummary } from "./shell-panel.ts";

// Store on the terminal, not a module singleton: extension loaders may isolate
// modules, while Pi keeps this terminal across regular/fullscreen transitions.
const STATE = Symbol.for("jhon-shell.experimental-sidebar.state");
/** In-process notification after a visual preference is persisted. */
export const VISUAL_SETTINGS_CHANGED = "jhon-shell.visual-settings-changed";
export interface SidebarState {
	active: boolean;
	ownsHost?: () => boolean;
	/**
	 * Columns the fullscreen rail reserves at the right edge (rail plus gap)
	 * while `active && ownsHost()`. Overlays read it to stay inside the editor
	 * column (the prompt-history picker's right margin) without importing
	 * jhon-shell.
	 */
	railColumns?: number;
	/** True while Status placement is "hidden": the bottom Status bar paints nothing at any width or mode. */
	statusHidden?: () => boolean;
	/** True while a painting top header is the only status row of a narrow fullscreen terminal: the bottom Status bar steps aside. */
	headerOwnsStatus?: () => boolean;
	/** True while a fullscreen terminal shows no rail and folds live work into the editor's work row: the TODOS and AGENTS cards step aside (T15.12c), and the footer too, its status row being the "statusbar" part's, as G's. */
	compactWork?: () => boolean;
	/** The pointer's current control on the shell's surfaces (setSidebarHover). */
	hover?: { owner: object; clear: () => void };
	parts: Map<string, SidebarRail>;
}

/** A rail slot in the fullscreen sidebar. */
export interface SidebarRail extends Component {
	/**
	 * Cheap digest of the live state this rail paints. The fullscreen layout memo
	 * re-renders a part only when its digest changes, so a rail that reads session
	 * data (model, thinking level, context, cost, extension statuses) must declare
	 * one; explicit invalidateSidebar() stays for discrete state changes.
	 */
	digest?(): string;
	/** The section's one-row digest for the narrow work row (` ▸ <lead> … <count>`); undefined without live work. */
	summary?(): PanelSummary | undefined;
	/** The section's own view, which a tap on the work row opens when this section leads it; without one the tap opens nothing (T169). */
	open?(): void;
	/** The section renders panel lines (`▾ NAME … count`, then rows): a transparent rail draws it as cards, a tinted one paints it. */
	surface?: "panel";
	/** Each headed section's row range in the last render at this width, one card each on a transparent rail; the whole part is one otherwise. */
	cardGroups?(width: number): { groups: Array<{ start: number; end: number }> } | undefined;
}
export function sidebarState(tui: TUI): SidebarState {
	const terminal = tui.terminal as unknown as Record<symbol, SidebarState>;
	return terminal[STATE] ??= { active: false, parts: new Map() };
}

/** Keep the original bottom component mounted, suppressing only its paint. */
export function sidebarPart<T extends Component & { dispose?(): void }>(tui: TUI, key: string, bottom: T, rail: SidebarRail = bottom): T {
	// Minimal extension hosts cannot share terminal-owned layout state.
	if (!tui.terminal) return bottom;
	const state = sidebarState(tui);
	state.parts.set(key, rail);
	return {
		...bottom,
		render: (width: number) => (key === "footer" && (state.statusHidden?.() || state.headerOwnsStatus?.() || state.compactWork?.())) || ((key === "todo" || key === "agents") && state.compactWork?.()) || (state.active && state.ownsHost?.()) ? [] : bottom.render(width),
		dispose() {
			if (state.parts.get(key) === rail) state.parts.delete(key);
			bottom.dispose?.();
		},
	};
}

/**
 * Register the fullscreen header rail: the one row above the hstack that
 * carries the brand, session identity, and the per-frame counters. There is
 * no narrow-mode bottom counterpart — the compact bar already carries this
 * data when the sidebar is inactive — so this only ever writes the "header"
 * slot in sidebarState(tui).parts, and returns its own disposer.
 */
export function sidebarHeader(tui: TUI, rail: SidebarRail): () => void {
	return sidebarSlot(tui, "header", rail);
}

/**
 * Register a part that has no bottom counterpart in a named slot: the
 * sections style's full-width status line under the rail ("statusbar").
 * Returns its own disposer.
 */
export function sidebarSlot(tui: TUI, key: string, rail: SidebarRail): () => void {
	if (!tui.terminal) return () => {};
	const state = sidebarState(tui);
	state.parts.set(key, rail);
	return () => {
		if (state.parts.get(key) === rail) state.parts.delete(key);
	};
}

/**
 * The one surface the pointer is over, as the Gentle Shell keeps it: a
 * control that paints its hover registers how to clear it, and a move that
 * lands anywhere else (another control, the transcript, the rail's blank
 * rows) clears the previous one.
 */
export function setSidebarHover(tui: TUI, owner?: object, clear?: () => void): void {
	if (!tui.terminal) return;
	const state = sidebarState(tui);
	if (state.hover?.owner === owner) return;
	const previous = state.hover;
	state.hover = owner && clear ? { owner, clear } : undefined;
	previous?.clear();
}
