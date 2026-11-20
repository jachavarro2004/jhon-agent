import { ScrollView, VStack, visibleWidth, type Component, type TUI, type TuiMouseEvent } from "@earendil-works/pi-tui";
import { setSidebarHover, sidebarState, type SidebarRail } from "./shell-sidebar.ts";
import type { ShellBarTheme } from "./shell-bar.ts";
import { CARD_STYLE, CARD_TONE, cardBottom, cardLine, cardStyle, cardTop, floatRows, paintSurfaceRow, panelInnerWidth, type CardStyle } from "./shell-card.ts";
import { chromeSurface, PANEL_ROW_INDENT, panelSurfaceOpener, RAIL_ORDER, sectionGap } from "./shell-panel.ts";
import { renderSidebarBanner } from "./shell-sidebar-banner.ts";
import type { Density, StatusPlacement } from "./visual-customization-policy.ts";

export const SIDEBAR_BREAKPOINT = 140;
/** The rail is 50 columns at every width, as Gentle Shell's (C9). */
const RAIL_WIDTH = 50;
const RAIL_PADDING = 1;
// The rail's ScrollView keeps one column for its scrollbar; with the rail
// padding that puts the card's right border two columns in from the edge.
// The header row stops at the same column so its right group lines up with
// the card instead of touching the terminal edge.
const HEADER_RIGHT_INSET = RAIL_PADDING + 1;
const GAP = 3;
/** Right-edge columns the painting rail takes from the editor column, published as `railColumns`. */
export const SIDEBAR_RAIL_COLUMNS = RAIL_WIDTH + GAP;
// Experimental Pi 0.85.1 internals. Only the fullscreen layout tree is adapted;
// regular mode keeps native scrollback and the original bottom components.
const NODE = Symbol.for("@earendil-works/pi-tui/layout-node");
type LayoutNode = { type: string; entries?: unknown[]; gap?: number; align?: "stretch" | "start" | "center" | "end" };
type StackLayoutEntry = ConstructorParameters<typeof VStack>[0] extends Array<infer T> | undefined ? Exclude<T, Component> : never;
type LayoutRoot = Component & { [NODE]?: () => LayoutNode };
type Host = TUI & { mode?: string; layoutRoot?: LayoutRoot };
type SidebarCache = { revision: number };
// `x`/`row` map a rail cell to the part's own cells; `frameWidth`/`frameHeight` are
// the frame the part rendered, which its mouse handler checks events against.
// A card title row (`wholeRow`) answers across the whole rail row, its x clamped into the frame.
type RailHit = { slot: string; component: Component; startY: number; height: number; width: number; x: number; row: number; frameWidth: number; frameHeight: number; wholeRow?: boolean };
type SectionCacheEntry = { component: Component; digest: string | undefined; revision: number; contentWidth: number; renderWidth: number; cardStyle: CardStyle; theme: ShellBarTheme; lines: string[] };
// A transparent rail draws every panel section as its own card in the
// transcript's card style (neon outline with the title in the top rule, float
// tint with its accent bar), as the Gentle Shell's: one section card holds
// the title (with its count as the top rule's hint) and the body rows.
type CardGeometry = { headerRow: number; headerX: number; bodyRow: number; bodyX: number };
function sectionCardRows(header: { glyph?: string; title: string; count?: string }, body: readonly string[], theme: ShellBarTheme, width: number): string[] {
	const tone = CARD_TONE.INFO;
	return floatRows(tone, theme, width, (inner) => ({
		head: [cardTop({ title: header.title, ...(header.glyph ? { glyph: header.glyph } : {}), body: [], tone }, theme, inner, header.count), ...body.map((row) => cardLine(row, tone, theme, inner))],
		bottom: cardBottom(tone, theme, inner),
	}));
}
const CARD_PROBE = "\u2063probe";
const geometryCache = new Map<string, CardGeometry>();
function cardGeometry(theme: ShellBarTheme, width: number, style: CardStyle): CardGeometry {
	const id = `${style}:${width}:${themeSignature(theme)}`;
	const cached = geometryCache.get(id);
	if (cached) return cached;
	const rows = sectionCardRows({ glyph: "▾", title: "HEADER" }, [CARD_PROBE], theme, width).map((row) => row.replace(/\x1b\[[0-9;]*m/g, ""));
	const bodyRow = Math.max(0, rows.findIndex((row) => row.includes(CARD_PROBE)));
	const headerRow = Math.max(0, rows.findIndex((row) => row.includes("HEADER")));
	const geometry = { headerRow, headerX: Math.max(0, visibleWidth(rows[headerRow]!.slice(0, rows[headerRow]!.indexOf("▾")))), bodyRow, bodyX: Math.max(0, visibleWidth(rows[bodyRow]!.slice(0, rows[bodyRow]!.indexOf(CARD_PROBE)))) };
	if (geometryCache.size > 64) geometryCache.clear();
	geometryCache.set(id, geometry);
	return geometry;
}
// The header row a part renders (`▾ NAME … count`, styled, hover included),
// split at the gap before its right-aligned count into the card's glyph,
// title and count, keeping their styling. ASCII draws the fold as v or >.
function headerParts(line: string): { glyph?: string; title: string; count?: string } {
	const trimmed = line.replace(/ +((?:\x1b\[[0-9;]*m)*)$/, "$1");
	const split = /^(.*?\S)( {2,})(?=\S|\x1b)(.*)$/s.exec(trimmed);
	const left = split ? split[1]! : trimmed;
	const count = split?.[3]?.trim() ? split[3] : undefined;
	// The glyph keeps its own escape codes on both sides (the hover role paints it),
	// so no opening code leaks onto the title.
	const glyph = /^((?:\x1b\[[0-9;]*m)*[▾▸v>](?:\x1b\[[0-9;]*m)*)\s/.exec(left);
	return { ...(glyph ? { glyph: glyph[1] } : {}), title: glyph ? left.slice(glyph[0].length) : left, ...(count ? { count } : {}) };
}
// Pi passes extensions one live theme proxy, so identity never changes on a
// theme switch: the card geometry keys on how the theme paints a few roles.
const THEME_SIGNATURE_ROLES = ["accent", "border", "muted", "dim", "text"];
function themeSignature(theme: ShellBarTheme): string {
	try {
		return THEME_SIGNATURE_ROLES.map((role) => theme.fg(role, "·")).join("") + theme.bold("·");
	} catch {
		return "";
	}
}
type SidebarPresentation = { scrollTop: number; output: LayoutNode };
type PreparedRail = {
	revision: number;
	width: number;
	/** The terminal's rows: the sections band fills the rail's height. */
	rows: number;
	mode: string | undefined;
	surface: string;
	density: Density;
	cardStyle: CardStyle;
	root: LayoutRoot;
	theme: ShellBarTheme;
	parts: Array<[string, SidebarRail]>;
	digests: Array<string | undefined>;
	contentWidth: number;
	active: boolean;
	lines: string[];
	hits: RailHit[];
	// The header row is a full-width sibling above the hstack, not a rail
	// section: it never enters `lines`/`hits`, and an empty/blank result
	// falls back to the old hstack-direct shape with the banner restored.
	headerLines: string[];
	headerActive: boolean;
	/** Sections: the full-width status line under the rail and the transcript. */
	statusLines: string[];
	/** The tinted blank row that fills the rail below a trailing panel section, or "" when the rail ends on a card. */
	fillRow: string;
	presentation?: SidebarPresentation;
};
const CACHE = Symbol.for("jhon-shell.experimental-sidebar.cache");

function sidebarCache(tui: TUI): SidebarCache {
	const terminal = tui.terminal as unknown as Record<symbol, SidebarCache>;
	return terminal[CACHE] ??= { revision: 0 };
}

/** Mark terminal-owned fullscreen sidebar output stale after a part state change. */
export function invalidateSidebar(tui: TUI): void {
	if (tui.terminal) sidebarCache(tui).revision++;
}

// The memo keys on part identity and an explicit revision, neither of which can
// see live session state read inside a rail's render closure: a model switch, a
// new context percentage or an extension status change leaves the prepared lines
// intact. A rail that paints such state declares a digest of it, so the memo can
// notice by itself; a throwing digest degrades that rail to invalidation-only
// rather than taking the whole sidebar down with it.
function railDigest(rail: SidebarRail): string | undefined {
	try {
		return rail.digest?.();
	} catch {
		return undefined;
	}
}

export const STATUS_OWNER = { HEADER: "header", BOTTOM: "bottom" } as const;
export type StatusOwner = (typeof STATUS_OWNER)[keyof typeof STATUS_OWNER];
export interface StatusOwnerInput {
	mode: string | undefined;
	columns: number;
	statusPlacement: StatusPlacement;
}

/**
 * Which single status row owns a narrow fullscreen terminal, so the top line
 * and the status line never both carry it: the status line, unless Status is
 * hidden and the top line is all that is left. Wide and regular layouts keep
 * their own rules.
 */
export function narrowStatusOwner(input: StatusOwnerInput): StatusOwner | undefined {
	if (input.mode !== "fullscreen" || input.columns >= SIDEBAR_BREAKPOINT) return undefined;
	return input.statusPlacement === "hidden" ? STATUS_OWNER.HEADER : STATUS_OWNER.BOTTOM;
}

/** Installs the fullscreen rail: wraps the host layout root with the [rail, transcript] hstack and returns a disposer restoring the original layout. */
export function installSidebar(tui: TUI, theme: ShellBarTheme, placement: () => StatusPlacement = () => "auto", density: () => Density = () => "comfortable"): () => void {
	if (!tui.terminal) return () => {};
	const host = tui as Host;
	const state = sidebarState(tui);
	const cache = sidebarCache(tui);
	const cleanups: Array<() => void> = [];
	const roots = new Set<LayoutRoot>();
	let stopped = false;
	let failed = false;
	let railLines: string[] = [];
	let fillRow = "";
	let headerLines: string[] = [];
	let statusLines: string[] = [];
	let prepared: PreparedRail | undefined;
	// One rendered-lines cache per rail section key, independent of the
	// whole-rail `prepared` memo below: a section with its own digest is
	// revalidated by that digest alone, so a sibling's ticking digest (the
	// header/Status counters) never forces Agents or TODO to re-render. A
	// section with no digest (or a throwing one) falls back to the shared
	// revision counter, exactly like the whole-rail memo already did.
	const sectionCache = new Map<string, SectionCacheEntry>();
	state.active = false;
	// Hidden removes Status everywhere, including regular mode where the rail
	// never mounts, so it is published independently of the fullscreen layout.
	state.statusHidden = () => !stopped && placement() === "hidden";
	// A narrow top header is the only status row there. Whether it paints comes
	// from the last layout pass (a blank or failed header never swallows the
	// bottom bar); the geometry is read live so a resize applies before the next pass.
	const headerOwnsStatus = () => !stopped && !failed && headerLines.length > 0 &&
		narrowStatusOwner({ mode: host.mode, columns: tui.terminal.columns, statusPlacement: placement() }) === STATUS_OWNER.HEADER;
	state.headerOwnsStatus = headerOwnsStatus;
	state.railColumns = SIDEBAR_RAIL_COLUMNS;
	state.ownsHost = () => !stopped && host.mode === "fullscreen" && tui.terminal.columns >= SIDEBAR_BREAKPOINT && placement() === "auto" && !!host.layoutRoot && roots.has(host.layoutRoot);
	// A tinted surface runs to the bottom of the rail: below the last panel
	// section, blank tinted rows fill what is left of the viewport. They are
	// not content, so they never add scroll range or hit targets.
	const rail: Component = {
		render: () => {
			const room = scroll.viewportHeight - railLines.length;
			return fillRow && room > 0 ? [...railLines, ...Array.from({ length: room }, () => fillRow)] : railLines;
		},
		invalidate() {
			invalidateSidebar(tui);
			for (const part of state.parts.values()) part.invalidate();
		},
	};
	// The header row: a plain leaf component measured from its rendered lines
	// (one line with just the status bar; two once the rule row joins it),
	// painted full-width above the hstack when a "header" part is registered
	// and has something to show.
	// The header is not inside the rail's ScrollView, so it never goes through
	// dispatchPartMouse: it is its own leaf in the layout tree (no [NODE]),
	// and pi-tui's mouse dispatch (tui-alt-screen.js dispatchMouseToLayout)
	// finds and calls handleMouse on whatever leaf box is under the pointer
	// directly, without any wiring of our own. Delegate straight to whatever
	// the registered "header" part declares.
	const header: Component = {
		render: () => headerLines,
		invalidate() {},
		handleMouse: (event) => {
			if (event.type === "move" && event.button === "none") setSidebarHover(tui);
			return state.parts.get("header")?.handleMouse?.(event);
		},
	};
	// Sections: the status line spans the whole width under the rail and the
	// transcript, like the Gentle Shell's; it delegates like the header.
	const statusbar: Component = {
		render: () => statusLines,
		invalidate() {},
		handleMouse: (event) => {
			if (event.type === "move" && event.button === "none") setSidebarHover(tui);
			return state.parts.get("statusbar")?.handleMouse?.(event);
		},
	};
	const scroll = new ScrollView(rail, {
		follow: "none",
		primary: false,
		overscroll: "contain",
		// "always" re-slices the scrollbar column of every rail line on every
		// render pass (grapheme measurement per row). "auto" keeps the rail
		// scrollbar transient like pi's own fullscreen scrollbar.
		scrollbar: "auto",
		scrollbarTrackStyle: (text) => theme.fg("border", text),
		scrollbarThumbStyle: (text) => theme.fg("accent", text),
	});
	const nativeMouse = scroll.handleMouse.bind(scroll);
	const dispatchPartMouse = (event: TuiMouseEvent) => {
		const current = prepared;
		if (!current || stopped || failed || !current.active || current.revision !== cache.revision ||
			host.mode !== "fullscreen" || tui.terminal.columns !== current.width || host.layoutRoot !== current.root ||
			scroll.getContentWidth(event.width) !== current.contentWidth || event.x < 0 ||
			event.x >= current.contentWidth || event.y < 0 || event.y >= event.height) return undefined;
		const contentY = scroll.scrollTop + event.y;
		const hit = current.hits.find((candidate) => contentY >= candidate.startY && contentY < candidate.startY + candidate.height);
		if (!hit || state.parts.get(hit.slot) !== hit.component) return undefined;
		if (!hit.wholeRow && (event.x < hit.x || event.x >= hit.x + hit.width)) return undefined;
		// The part sees the event in the frame it rendered: its own width and all its rows.
		const local = { ...event, x: Math.max(0, Math.min(hit.frameWidth - 1, event.x - hit.x)), y: hit.row + contentY - hit.startY, width: hit.frameWidth, height: hit.frameHeight };
		const result = hit.component.handleMouse?.(local);
		if (!result || !(result.handled || result.capture || result.focus)) return undefined;
		// pi-tui 1.4.0 routes a handled event's drag, release, and focus to its target.
		return {
			...result,
			handled: true as const,
			target: { component: hit.component, originX: event.screenX - local.x, originY: event.screenY - local.y, width: hit.frameWidth, height: hit.frameHeight },
		};
	};
	scroll.handleMouse = (event) => {
		if (event.type === "wheel") {
			// A bounded section (Todo) owns its inner viewport. Only wheel
			// events it declines should move the surrounding rail.
			const sectionResult = dispatchPartMouse(event);
			if (sectionResult?.handled) return sectionResult;
			// Consume even at the boundary or over blank rail space: Pi 0.85.1
			// can send unconsumed delta to the primary transcript despite containment.
			scroll.scrollBy(event.wheelDelta ?? 0);
			return {
				handled: true,
				render: true,
				target: { component: scroll, originX: event.screenX - event.x, originY: event.screenY - event.y, width: event.width, height: event.height },
			};
		}
		// A move over no control clears the one hovered before (setSidebarHover).
		const result = dispatchPartMouse(event);
		if (!result && event.type === "move" && event.button === "none") setSidebarHover(tui);
		return result ?? nativeMouse(event);
	};
	const prepare = (width: number, root: LayoutRoot): boolean => {
		state.active = false;
		if (stopped || failed || host.mode !== "fullscreen") {
			prepared = undefined;
			headerLines = [];
			statusLines = [];
			return false;
		}
		const railEligible = width >= SIDEBAR_BREAKPOINT && placement() !== "hidden";
		if (!railEligible) {
			prepared = undefined;
			statusLines = [];
			try {
				const headerPart = state.parts.get("header");
				headerLines = headerPart ? [...(headerPart.render(Math.max(0, width)) ?? [])] : [];
				if (!headerLines.some((line) => line.trim() !== "")) headerLines = [];
				// A narrow terminal keeps the status bar row when its part paints one, as G's.
				const statusPart = state.parts.get("statusbar");
				const narrowStatus = statusPart ? [...(statusPart.render(Math.max(0, width)) ?? [])] : [];
				statusLines = narrowStatus.some((line) => line.trim() !== "") ? narrowStatus : [];
			} catch {
				failed = true;
				headerLines = [];
			}
			return false;
		}
		const style = cardStyle();
		const surface = chromeSurface();
		const parts = [...state.parts.entries()];
		const digests = parts.map(([, rail]) => railDigest(rail));
		const unchanged = prepared?.revision === cache.revision &&
			prepared.width === width && prepared.rows === tui.terminal.rows && prepared.mode === host.mode && prepared.surface === surface && prepared.density === density() && prepared.cardStyle === style && prepared.root === root && prepared.theme === theme &&
			prepared.parts.length === parts.length && prepared.parts.every(([slot, part], index) => parts[index]?.[0] === slot && parts[index]?.[1] === part) &&
			prepared.digests.length === digests.length && prepared.digests.every((digest, index) => digest === digests[index]);
		if (unchanged && prepared) {
			railLines = prepared.lines;
			statusLines = prepared.statusLines;
			fillRow = prepared.fillRow;
			state.active = prepared.active;
			return prepared.active;
		}
		try {
			// The header is a full-width sibling row, not a rail section: it reads
			// the terminal width, never the 50-column rail's content width (G's
			// title row keeps its own one-column margins).
			const headerPart = state.parts.get("header");
			const preparedHeaderLines = [...(headerPart?.render(Math.max(0, width)) ?? [])];
			const headerActive = headerPart !== undefined && preparedHeaderLines.some((line) => line.trim() !== "");
			const contentWidth = scroll.getContentWidth(RAIL_WIDTH);
			const statusPart = state.parts.get("statusbar");
			const preparedStatusLines = [...(statusPart?.render(Math.max(0, width)) ?? [])];
			// The header carries the brand once it is active; the banner is the
			// rail's fallback identity when no header is wired up.
			const branding = headerActive ? [] : renderSidebarBanner(theme, contentWidth - RAIL_PADDING * 2);
			headerLines = headerActive ? preparedHeaderLines : [];
			statusLines = preparedStatusLines.some((line) => line.trim() !== "") ? preparedStatusLines : [];
			// A transparent surface draws every panel section as its own card
			// (L153 answer 2); a carded section renders one indent wider and its
			// body rows drop that indent inside the card.
			const carded = surface === "transparent";
			// G's: float cards are tinted edge to edge and would fuse, so they keep
			// their blank row; otherwise the density decides the gap.
			const cardGap = () => (carded && style === CARD_STYLE.FLOAT) || sectionGap(density());
			const panelWidth = carded ? panelInnerWidth(theme, contentWidth) + PANEL_ROW_INDENT : contentWidth - RAIL_PADDING * 2;
			// LDD first (L174), then live work and the rest of Status, as Gentle Shell's;
			// TODOS is a bounded section with its own cap (C10).
			const sections = RAIL_ORDER.map((slot) => {
				const component = state.parts.get(slot);
				if (!component) {
					sectionCache.delete(slot);
					return { slot, component, lines: [] as string[], renderWidth: 0 };
				}
				const renderWidth = component.surface === "panel" ? panelWidth : contentWidth - RAIL_PADDING * 2;
				const digest = railDigest(component);
				const existing = sectionCache.get(slot);
				// Sections paint their card chrome in the active style, which their
				// own digests do not track, so a style switch must re-render them.
				const reusable = existing?.component === component && existing.contentWidth === contentWidth && existing.renderWidth === renderWidth && existing.cardStyle === style && existing.theme === theme &&
					(digest !== undefined ? existing.digest === digest : existing.digest === undefined && existing.revision === cache.revision);
				const lines = reusable ? existing.lines : (() => {
					const rendered = [...(component.render(renderWidth) ?? [])];
					while (rendered.length && rendered[rendered.length - 1]?.trim() === "") rendered.pop();
					return rendered;
				})();
				sectionCache.set(slot, { component, digest, revision: cache.revision, contentWidth, renderWidth, cardStyle: style, theme, lines });
				return { slot, component, lines, renderWidth };
			}).filter((section) => section.component !== undefined && section.lines.length > 0) as Array<{ slot: string; component: SidebarRail; lines: string[]; renderWidth: number }>;
			const hits: RailHit[] = [];
			railLines = [];
			fillRow = "";
			const open = panelSurfaceOpener(theme);
			const paint = (row: string) => paintSurfaceRow(row + " ".repeat(Math.max(0, contentWidth - visibleWidth(row))), open);
			// Tinted spacing continues the rail's surface; transparent spacing
			// leaves a clear row above the first card.
			if (sections.length && headerActive) railLines.push(carded ? "" : paint(""));
			if (sections.length && branding.length) {
				railLines.push(...branding.map((line) => " ".repeat(RAIL_PADDING) + line + " ".repeat(RAIL_PADDING)));
			}
			// One blank row separates a section from the banner or the previous
			// one: float cards are tinted edge to edge and would fuse into one
			// slab without it, neon cards and the tinted panel keep it too.
			let previousPanel = false;
			for (const section of sections) {
				const panel = section.component.surface === "panel";
				if ((hits.length > 0 || branding.length > 0) && (carded ? cardGap() : sectionGap(density()))) railLines.push(!carded && panel && previousPanel ? paint("") : "");
				if (carded && panel) {
					const layout = section.component.cardGroups?.(section.renderWidth) ?? { groups: [{ start: 0, end: section.lines.length }] };
					const geometry = cardGeometry(theme, contentWidth, style);
					const indent = " ".repeat(PANEL_ROW_INDENT);
					layout.groups.forEach((group, index) => {
						if (index > 0 && cardGap()) railLines.push("");
						const body = section.lines.slice(group.start + 1, group.end).map((row) => row.startsWith(indent) ? row.slice(PANEL_ROW_INDENT) : row);
						while (body.length && body[body.length - 1]?.trim() === "") body.pop();
						const top = railLines.length;
						railLines.push(...sectionCardRows(headerParts(section.lines[group.start] ?? ""), body, theme, contentWidth));
						// The title row folds the section like its header; body rows keep their own rows.
						const frame = { frameWidth: section.renderWidth, frameHeight: section.lines.length };
						hits.push({ slot: section.slot, component: section.component, startY: top + geometry.headerRow, height: 1, width: section.renderWidth, x: geometry.headerX, row: group.start, wholeRow: true, ...frame });
						if (body.length) hits.push({ slot: section.slot, component: section.component, startY: top + geometry.bodyRow, height: body.length, width: section.renderWidth - PANEL_ROW_INDENT, x: geometry.bodyX - PANEL_ROW_INDENT, row: group.start + 1, ...frame });
					});
					previousPanel = false;
					continue;
				}
				const startY = railLines.length;
				railLines.push(...section.lines.map((line) => {
					const row = " ".repeat(RAIL_PADDING) + line + " ".repeat(RAIL_PADDING);
					return !carded && panel ? paint(row) : row;
				}));
				previousPanel = panel;
				fillRow = !carded && panel ? paint("") : "";
				hits.push({ slot: section.slot, component: section.component, startY, height: section.lines.length, width: section.renderWidth, x: RAIL_PADDING, row: 0, frameWidth: section.renderWidth, frameHeight: section.lines.length });
			}
			// Height is owned by the native ScrollView, never by the transcript;
			// the rail has no key line (L143): "/ commands" is the prompt's alone.
			const active = railLines.length > 0 && railLines.every((line) => visibleWidth(line) <= contentWidth);
			prepared = { revision: cache.revision, width, rows: tui.terminal.rows, mode: host.mode, surface, density: density(), cardStyle: style, root, theme, parts, digests, contentWidth, active, lines: railLines, hits, headerLines, headerActive: headerLines.length > 0, statusLines, fillRow };
			state.active = active;
			return active;
		} catch {
			failed = true;
			return false;
		}
	};
	const attach = () => {
		if (stopped || failed) return;
		if (host.mode !== "fullscreen") { state.active = false; return; }
		try {
			const root = host.layoutRoot;
			if (!root || typeof root[NODE] !== "function") { state.active = false; return; }
			if (roots.has(root)) return;
			const original = root[NODE]!;
			const descriptor = Object.getOwnPropertyDescriptor(root, NODE);
			// Fullscreen gives this stretched stack an explicit viewport height.
			// Its intrinsic-height probe is unused; real painting traverses NODE.
			// Delegating that probe to root.render would render the transcript twice.
			// Pi's dock reserves one row for the footer (chat-viewport: minSize 1)
			// even though our footer paints nothing while the sidebar is active.
			// The row is baked in twice: the dock's own VStack.render pads it into
			// the intrinsic height the root measures, and its layout node keeps
			// it as minSize. Overriding only the node leaves the measured blank
			// row in place, so the dock is re-hosted in a real VStack over the
			// same children with the footer entry free to shrink to zero. One
			// wrapper per dock keeps component identity stable across frames.
			const docks = new WeakMap<Component, VStack>();
			const reclaimFooterRow = (node: LayoutNode): LayoutNode => {
				if (node.type !== "vstack" || !node.entries?.length) return node;
				const entries = node.entries as Array<{ component: Component & { [NODE]?: () => LayoutNode } }>;
				const dock = entries[entries.length - 1]!.component;
				if (typeof dock[NODE] !== "function") return node;
				let wrapped = docks.get(dock);
				if (!wrapped) {
					const inner = dock[NODE]!();
					if (inner.type !== "vstack" || !inner.entries?.length) return node;
					const last = inner.entries.length - 1;
					wrapped = new VStack(inner.entries.map((entry, index) => index === last ? { ...(entry as StackLayoutEntry), minSize: 0 } : entry as StackLayoutEntry), { gap: inner.gap, align: inner.align });
					docks.set(dock, wrapped);
				}
				return { ...node, entries: entries.map((entry, index) => index === entries.length - 1 ? { ...entry, component: wrapped! } : entry) };
			};
			// Without a rail the native layout stays in place; a hidden Status, or a
			// narrow top header owning it, only frees the footer's reserved dock row,
			// exactly as the rail does. Live work folded into the work row (compact
			// work) hands the footer row to the status bar too, as G's.
			const nativeLayout = () => placement() === "hidden" || headerOwnsStatus() || state.compactWork?.() ? reclaimFooterRow(original.call(root)) : original.call(root);
			const nativeHost = { render: () => [], invalidate() {}, [NODE]: nativeLayout };
			const left = { render: () => [], invalidate() {}, [NODE]: () => reclaimFooterRow(original.call(root)) };
			// Stable component wrapping the [left, scroll] hstack behind its own
			// NODE, exactly like `left` wraps the native transcript: the header
			// vstack's second entry recurses into it the same way pi-tui already
			// recurses into a nested layout via [NODE].
			const hstackHost: Component & { [NODE](): LayoutNode } = {
				render: () => [],
				invalidate() {},
				[NODE]: () => ({ type: "hstack", gap: GAP, align: "stretch", entries: [
					{ component: left, basis: 0, grow: 1, shrink: 1, minSize: 1 },
					{ component: scroll, basis: RAIL_WIDTH, grow: 0, shrink: 0, minSize: RAIL_WIDTH },
				] }),
			};
			const replacement = (): LayoutNode => {
				if (!prepare(tui.terminal.columns, root)) {
					if (failed || stopped || host.mode !== "fullscreen") return original.call(root);
					if (!headerLines.length && !statusLines.length) return nativeLayout();
					return { type: "vstack", gap: 0, align: "stretch", entries: [
						...(headerLines.length ? [{ component: header, basis: "auto" as const, grow: 0, shrink: 0, minSize: 1 }] : []),
						{ component: nativeHost, basis: 0, grow: 1, shrink: 1, minSize: 1 },
						...(statusLines.length ? [{ component: statusbar, basis: "auto" as const, grow: 0, shrink: 0, minSize: 1 }] : []),
					] };
				}
				const current = prepared!;
				if (current.presentation?.scrollTop === scroll.scrollTop) return current.presentation.output;
				const withHeader = current.headerActive;
				const output: LayoutNode = withHeader || current.statusLines.length > 0
					? { type: "vstack", gap: 0, align: "stretch", entries: [
						...(withHeader ? [{ component: header, basis: "auto" as const, grow: 0, shrink: 0, minSize: 1 }] : []),
						{ component: hstackHost, basis: 0, grow: 1, shrink: 1, minSize: 1 },
						...(current.statusLines.length > 0 ? [{ component: statusbar, basis: "auto" as const, grow: 0, shrink: 0, minSize: 1 }] : []),
					] }
					: hstackHost[NODE]();
				return (current.presentation = { scrollTop: scroll.scrollTop, output }).output;
			};
			root[NODE] = replacement;
			roots.add(root);
			tui.requestRender();
			cleanups.push(() => {
				if (root[NODE] !== replacement) return;
				if (descriptor) Object.defineProperty(root, NODE, descriptor);
				else Reflect.deleteProperty(root, NODE);
			});
		} catch {
			failed = true;
			state.active = false;
		}
	};
	attach();
	// Pi replaces renderers without a session event. Rebind only that transition;
	// resize and scroll remain owned by Pi's native layout/render loop.
	const timer = setInterval(attach, 100);
	timer.unref();
	return () => {
		stopped = true;
		setSidebarHover(tui);
		state.active = false;
		clearInterval(timer);
		scroll.setScrollbar("hidden");
		for (const cleanup of cleanups.reverse()) cleanup();
		tui.requestRender();
	};
}
