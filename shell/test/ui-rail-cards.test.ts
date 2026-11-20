/**
 * The rail's sections in the right pane (T15.12d; T174-S4): each section
 * renders panel lines, a `▾ NAME … count` header and rows indented under it,
 * and the pane (lib/ui/pane.ts) stacks them as they are: no card chrome on a
 * transparent surface (design 01 draws none), one painted panel on a tinted
 * one. ASCII draws v / >.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-rail-cards.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test, { type TestContext } from "node:test";
import { VStack, visibleWidth, type Component, type TUI } from "@earendil-works/pi-tui";
import { renderShellAgentsSection, renderShellJobsSection, renderShellStatusLayout, renderShellStatusSections, type ShellBarModel } from "../lib/ui/shell-bar.ts";
import { CARD_STYLE, cardStyle, setCardStyle } from "../lib/ui/primitives/index.ts";
import { chromeSurface, setChromeSurface } from "../lib/ui/shell-panel.ts";
import { installPane, paneSlot, PANE_TEXT } from "../lib/ui/pane.ts";
import { PI_LAYOUT_NODE } from "../lib/ui/pi-layout-hook.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { useMemorySettings } from "./fake-jhon-settings.ts";

const plain = { fg: (_c: string, t: string) => t, bold: (t: string) => t };
const tagged = { fg: (c: string, t: string) => `<${c}>${t}</${c}>`, bold: (t: string) => `<b>${t}</b>` };
const BG = "\x1b[48;5;236m";
const painted = { ...plain, bg: (_c: string, t: string) => `${BG}${t}\x1b[49m` };
const NOW = 1_788_600_000_000;
const jobs = [
	{ id: "bg-1", kind: "command" as const, label: "npm test", startedAt: NOW - 65_000 },
	{ id: "ag-1", kind: "agent" as const, label: "explorer: map the rail", startedAt: NOW - 95_000, tokens: 48_200 },
];
const status = (hostShowsPath = false): ShellBarModel => ({
	cwd: "/w", branch: "main", sessionName: undefined, modelId: "m", effort: "high", profile: "jhon",
	contextPercent: 10, contextWindow: 1000, costTotal: 0, subscription: true, hostShowsPath,
	ldd: undefined,
} as unknown as ShellBarModel);
const plainRows = (lines: string[]) => lines.map((line) => stripAnsi(line).trimEnd());

function look(t: TestContext, style: "float" | "neon", surface: "transparent" | "tinted", ascii = false) {
	useMemorySettings(t, ascii ? { "shell.appearance.ascii": true } : {});
	const before = [cardStyle(), chromeSurface()] as const;
	setCardStyle(style);
	setChromeSurface(surface);
	t.after(() => { setCardStyle(before[0]); setChromeSurface(before[1]); });
}

/** The pane's panel rows as the pane host draws them (lib/ui/pane.ts, T174-S4), with LDD and JOBS as its parts (LDD first, L174). */
function rail(t: TestContext, theme: typeof plain, density?: "comfortable" | "compact" | "minimal"): string[] {
	const root = new VStack([]);
	const tui = { mode: "fullscreen", terminal: { columns: 160, rows: 40 }, layoutRoot: root, requestRender() {} } as unknown as TUI;
	paneSlot(tui, "jobs", { render: (w: number) => renderShellJobsSection(jobs, theme, w, NOW), invalidate() {} });
	paneSlot(tui, "ldd", { render: (w: number) => renderShellStatusLayout(status(), theme, w, undefined, undefined, density).lines, invalidate() {} });
	t.after(installPane(tui, theme, { placement: () => "auto", density: () => density ?? "comfortable", note: () => {} }));
	type Node = { entries: Array<{ component: Component & { child?: Component; [PI_LAYOUT_NODE]?(): Node } }> };
	const tree = (root as unknown as Record<symbol, () => Node>)[PI_LAYOUT_NODE]!();
	const split = tree.entries[2]!.component[PI_LAYOUT_NODE]!();
	return split.entries[2]!.component.child!.render(PANE_TEXT + 2);
}

test("each section is panel lines: `▾ NAME … count`, the name upper-case text bold, the count muted, rows indented under it", (t) => {
	look(t, "float", "transparent");
	const job = plainRows(renderShellJobsSection(jobs, plain, 48, NOW));
	assert.match(job[0]!, /^▾ JOBS +1 running$/);
	assert.match(job[1]!, /^ {2}⧗ bg-1 npm test +1m 5s$/);
	assert.match(plainRows(renderShellAgentsSection(jobs, plain, 48, NOW))[0]!, /^▾ AGENTS +1 running$/);
	const st = renderShellStatusLayout(status(), plain, 48);
	assert.deepEqual(st.groups.map((group) => plainRows(st.lines)[group.start]!.replace(/ +.*$/, "")), ["▾"]);
	assert.deepEqual(st.groups.map((group) => plainRows(st.lines)[group.start]!.split(/ +/)[1]), ["LDD"], "T174: no CHANGES, no INTEGRATIONS");
	for (const line of [...job, ...st.lines]) assert.ok(visibleWidth(line) <= 48, line);
	const top = renderShellJobsSection(jobs, tagged, 300, NOW)[0]!;
	assert.match(top, /^<muted>▾<\/muted> <text><b>JOBS<\/b><\/text>/);
	assert.match(top, /<muted>1 running<\/muted>$/);
	assert.deepEqual(plainRows(renderShellJobsSection(jobs, plain, 48, NOW, true)).slice(1), job.slice(1, 2), "folded: the first job stays, as G's");
	assert.match(plainRows(renderShellJobsSection(jobs, plain, 48, NOW, true))[0]!, /^▸ JOBS/);
});

test("transparent: the pane stacks the sections' panel lines as they are, LDD first, a blank row apart; no card chrome in any card style (T174-S4, design 01)", (t) => {
	for (const style of ["float", "neon"] as const) {
		look(t, style, "transparent");
		const lines = rail(t, painted);
		const text = plainRows(lines);
		const jobsAt = text.findIndex((line) => /^▾ JOBS +1 running$/.test(line));
		assert.ok(jobsAt > 0, text.join("\n"));
		assert.match(text[jobsAt + 1]!, /^ {2}⧗ bg-1 npm test +1m 5s$/, "rows keep their panel indent");
		assert.equal(text[jobsAt - 1], "", "a blank row between two sections");
		const lddAt = text.findIndex((line) => /^▾ LDD/.test(line));
		assert.equal(lddAt, 1, "LDD leads the pane, under one blank row");
		assert.equal(text[0], "");
		assert.doesNotMatch(text.join("\n"), /[╭╰▎]|CHANGES|INTEGRATIONS/, `${style}: no card chrome, no CHANGES or INTEGRATIONS`);
		assert.ok(lines.every((line) => !line.includes(BG)), "nothing paints: the surface is transparent");
		assert.ok(lines.every((line) => visibleWidth(line) === PANE_TEXT + 2), "every row the pane's width");
	}
});

test("tinted: one painted panel, `▾ NAME … count` headers and their rows on it, the blank rows between sections painted too", (t) => {
	look(t, "float", "tinted");
	const lines = rail(t, painted);
	const text = plainRows(lines);
	assert.ok(text.some((line) => /^▾ JOBS +1 running$/.test(line)), text.join("\n"));
	assert.ok(text.some((line) => /^ {2}⧗ bg-1 npm test +1m 5s$/.test(line)));
	assert.ok(text.some((line) => /^▾ LDD *$/.test(line)), text.join("\n"));
	assert.doesNotMatch(text.join("\n"), /[╭│╰▎]/, "no card chrome on a tinted panel");
	const first = text.findIndex((line) => line.includes("▾"));
	assert.ok(lines.slice(first).every((line) => line.startsWith(BG)), "every row from the first section on is on the panel surface, the gaps included");
});

// G's Density (L195): comfortable keeps a blank row between the pane's
// sections; compact and minimal close them up.
test("density: comfortable keeps a blank row between sections, compact and minimal none, on either surface", (t) => {
	for (const surface of ["tinted", "transparent"] as const) {
		look(t, "float", surface);
		const blankBetween = (text: string[]) => { const first = text.findIndex((line) => line.includes("▾")); return text.slice(first).some((line) => line.trim() === ""); };
		assert.equal(blankBetween(plainRows(rail(t, painted, "comfortable"))), true, `${surface} comfortable: a gap`);
		for (const density of ["compact", "minimal"] as const) {
			const text = plainRows(rail(t, painted, density));
			assert.equal(blankBetween(text), false, `${surface} ${density}: ${text.join("\n")}`);
			const jobsAt = text.findIndex((line) => /^▾ JOBS/.test(line));
			assert.ok(jobsAt > 0 && text[jobsAt - 1]!.trim() !== "", `${density}: JOBS right under LDD`);
		}
	}
});

test("ASCII: the fold is v or >", (t) => {
	look(t, "neon", "transparent", true);
	const text = plainRows(rail(t, plain));
	assert.ok(text.some((line) => /^v JOBS +1 running$/.test(line)), text.join("\n"));
	assert.ok(text.some((line) => /^v LDD/.test(line)), text.join("\n"));
});

test("under a multiplexer that shows the path the Status sections are the same (L112)", (t) => {
	look(t, "float", "transparent");
	assert.deepEqual(renderShellStatusSections(status(true), plain, 48), renderShellStatusSections(status(false), plain, 48));
});
