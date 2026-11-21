/**
 * The right pane's sections on its surface (T174-S4, S5): the pane
 * (lib/ui/pane.ts) stacks the sections' rows as they are, LDD first, with no
 * card chrome on a transparent surface (design 01 draws none) and one painted
 * panel on a tinted one. The two forms (L1495, T174-S5b): comfortable draws
 * each section in its own box, compact keeps a blank row between sections
 * (01), minimal none.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-rail-cards.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test, { type TestContext } from "node:test";
import { VStack, visibleWidth, type Component, type TUI } from "@earendil-works/pi-tui";
import { renderJobsSection, renderLddPanel } from "../lib/ui/panel.ts";
import { cardStyle, setCardStyle } from "../lib/ui/primitives/index.ts";
import { chromeSurface, setChromeSurface } from "../lib/ui/shell-panel.ts";
import { installPane, paneSlot, PANE_TEXT } from "../lib/ui/pane.ts";
import { PI_LAYOUT_NODE } from "../lib/ui/pi-layout-hook.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";

const plain = { fg: (_c: string, t: string) => t, bold: (t: string) => t };
const BG = "\x1b[48;5;236m";
const painted = { ...plain, bg: (_c: string, t: string) => `${BG}${t}\x1b[49m` };
/** A fixed clock: 5 September 2026, 09:20 UTC. */
const NOW = Date.UTC(2026, 8, 5, 9, 20);
const jobs = [
	{ id: "job-1", kind: "command" as const, label: "npm test", startedAt: NOW - 65_000 },
	{ id: "agent-1", kind: "agent" as const, label: "jhon-scout: map the pane", startedAt: NOW - 95_000 },
];
const plainRows = (lines: string[]) => lines.map((line) => stripAnsi(line).trimEnd());

function look(t: TestContext, style: "float" | "neon", surface: "transparent" | "tinted") {
	const before = [cardStyle(), chromeSurface()] as const;
	setCardStyle(style);
	setChromeSurface(surface);
	t.after(() => { setCardStyle(before[0]); setChromeSurface(before[1]); });
}

/** The pane's rows as its host draws them, with LDD (no change) and Jobs as its parts. */
function pane(t: TestContext, theme: typeof plain, density: "comfortable" | "compact" | "minimal" = "compact"): string[] {
	const root = new VStack([]);
	const tui = { mode: "fullscreen", terminal: { columns: 160, rows: 40 }, layoutRoot: root, requestRender() {} } as unknown as TUI;
	paneSlot(tui, "jobs", { render: (w: number) => renderJobsSection(jobs, theme, w, { now: NOW }), invalidate() {} });
	paneSlot(tui, "ldd", { render: (w: number) => renderLddPanel(undefined, theme, w, { now: NOW }), invalidate() {} });
	t.after(installPane(tui, theme, { placement: () => "auto", density: () => density, note: () => {} }));
	type Node = { entries: Array<{ component: Component & { child?: Component; [PI_LAYOUT_NODE]?(): Node } }> };
	const tree = (root as unknown as Record<symbol, () => Node>)[PI_LAYOUT_NODE]!();
	const split = tree.entries[2]!.component[PI_LAYOUT_NODE]!();
	return split.entries[2]!.component.child!.render(PANE_TEXT + 2);
}

test("transparent, compact: the pane stacks the sections' rows as they are, LDD first under one blank row, Jobs a blank row after it; no card chrome", (t) => {
	for (const style of ["float", "neon"] as const) {
		look(t, style, "transparent");
		const lines = pane(t, painted);
		const text = plainRows(lines);
		assert.deepEqual(text.slice(0, 5), ["", "No active LDD change", "", "Jobs  1 running", `⧗ job-1 npm test${" ".repeat(PANE_TEXT - 16 - 5)}1m05s`], `${style}:\n${text.join("\n")}`);
		assert.doesNotMatch(text.join("\n"), /[╭╰▎▾]/, `${style}: no card chrome, no fold glyph`);
		assert.ok(lines.every((line) => !line.includes(BG)), "nothing paints: the surface is transparent");
		assert.ok(lines.every((line) => visibleWidth(line) === PANE_TEXT + 2), "every row the pane's width");
	}
});

test("tinted: one painted panel, the rows and the blank rows between sections on it", (t) => {
	look(t, "float", "tinted");
	const lines = pane(t, painted);
	assert.ok(plainRows(lines).includes("Jobs  1 running"));
	assert.ok(lines.every((line) => line.startsWith(BG)), "every row is on the panel surface, the gaps included");
});

test("density: comfortable boxes each section, compact keeps a blank row between them, minimal none, on either surface", (t) => {
	for (const surface of ["tinted", "transparent"] as const) {
		look(t, "float", surface);
		const rows = (density: "comfortable" | "compact" | "minimal") => plainRows(pane(t, painted, density));
		const before = (density: "comfortable" | "compact" | "minimal") => { const text = rows(density); return text[text.indexOf("Jobs  1 running") - 1]; };
		assert.equal(before("compact"), "", `${surface} compact: a gap`);
		assert.equal(before("minimal"), "No active LDD change", `${surface} minimal: Jobs right under LDD`);
		assert.deepEqual(rows("comfortable").slice(0, 5), ["", `╭${"─".repeat(PANE_TEXT - 2)}╮`, `│ No active LDD change${" ".repeat(PANE_TEXT - 24)} │`, `╰${"─".repeat(PANE_TEXT - 2)}╯`, `╭${"─".repeat(PANE_TEXT - 2)}╮`], `${surface} comfortable: a box each`);
	}
});
