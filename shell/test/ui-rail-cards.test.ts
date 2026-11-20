/**
 * The rail's sections as the Gentle Shell draws them (T15.12d, L153 answers
 * 1-2): each section renders panel lines, a `▾ NAME … count` header and rows
 * indented under it; on a transparent surface (the default) the rail draws
 * each one as its own card in the card style (float: the `▎` bar on a tint,
 * a blank row above the header; neon: the rounded outline, the header in its
 * top rule), and on a tinted surface it paints them all on one panel. ASCII
 * draws + - | and v / >.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-rail-cards.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test, { type TestContext } from "node:test";
import { ScrollView, visibleWidth, type TUI } from "@earendil-works/pi-tui";
import { renderShellAgentsSection, renderShellJobsSection, renderShellStatusLayout, renderShellStatusSections, type ShellBarModel } from "../lib/ui/shell-bar.ts";
import { CARD_STYLE, cardStyle, setCardStyle } from "../lib/ui/shell-card.ts";
import { chromeSurface, setChromeSurface } from "../lib/ui/shell-panel.ts";
import { installSidebar } from "../lib/ui/shell-sidebar-layout.ts";
import { sidebarPart } from "../lib/ui/shell-sidebar.ts";
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
	cwd: "/w", branch: "main", dirty: undefined, sessionName: undefined, modelId: "m", effort: "high", profile: "jhon",
	contextPercent: 10, contextWindow: 1000, costTotal: 0, subscription: true, usage: undefined, statuses: ["indexing"], hostShowsPath,
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

/** The rail, as installSidebar lays it out, with JOBS and Status as panel parts. */
function rail(t: TestContext, theme: typeof plain, density?: "comfortable" | "compact" | "minimal"): string[] {
	const NODE = Symbol.for("@earendil-works/pi-tui/layout-node");
	const root = { render: () => ["transcript"], invalidate() {}, [NODE]: () => ({ type: "vstack", entries: [] }) };
	const tui = { mode: "fullscreen", terminal: { columns: 160, rows: 40 }, layoutRoot: root, requestRender() {} } as unknown as TUI;
	const groups = new Map<number, Array<{ start: number; end: number }>>();
	sidebarPart(tui, "jobs", { render: () => [], invalidate() {} }, { surface: "panel", render: (w: number) => renderShellJobsSection(jobs, theme, w, NOW), invalidate() {} });
	sidebarPart(tui, "footer", { render: () => [], invalidate() {} }, {
		surface: "panel",
		render: (w: number) => { const layout = renderShellStatusLayout(status(), theme, w, undefined, undefined, undefined, density); groups.set(w, layout.groups); return layout.lines; },
		cardGroups: (w: number) => ({ groups: groups.get(w) ?? [] }),
		invalidate() {},
	});
	t.after(density ? installSidebar(tui, theme, () => "auto", () => density) : installSidebar(tui, theme));
	const node = (root as unknown as Record<symbol, () => { entries: Array<{ component: ScrollView }> }>)[NODE]!();
	return node.entries[1]!.component.render(50);
}

test("each section is panel lines: `▾ NAME … count`, the name upper-case text bold, the count muted, rows indented under it", (t) => {
	look(t, "float", "transparent");
	const job = plainRows(renderShellJobsSection(jobs, plain, 48, NOW));
	assert.match(job[0]!, /^▾ JOBS +1 running$/);
	assert.match(job[1]!, /^ {2}⧗ bg-1 npm test +1m 5s$/);
	assert.match(plainRows(renderShellAgentsSection(jobs, plain, 48, NOW))[0]!, /^▾ AGENTS +1 running$/);
	const st = renderShellStatusLayout(status(), plain, 48);
	assert.deepEqual(st.groups.map((group) => plainRows(st.lines)[group.start]!.replace(/ +.*$/, "")), ["▾", "▾", "▾"]);
	assert.deepEqual(st.groups.map((group) => plainRows(st.lines)[group.start]!.split(/ +/)[1]), ["LDD", "CHANGES", "INTEGRATIONS"]);
	assert.match(st.lines.map(stripAnsi).join("\n"), /^ {2}- indexing/m, "an extension's status is an INTEGRATIONS row");
	for (const line of [...job, ...st.lines]) assert.ok(visibleWidth(line) <= 48, line);
	const top = renderShellJobsSection(jobs, tagged, 300, NOW)[0]!;
	assert.match(top, /^<muted>▾<\/muted> <text><b>JOBS<\/b><\/text>/);
	assert.match(top, /<muted>1 running<\/muted>$/);
	assert.deepEqual(plainRows(renderShellJobsSection(jobs, plain, 48, NOW, true)).slice(1), job.slice(1, 2), "folded: the first job stays, as G's");
	assert.match(plainRows(renderShellJobsSection(jobs, plain, 48, NOW, true))[0]!, /^▸ JOBS/);
});

test("transparent + float: each section its own float card, a blank tinted row above `▎ ▾ NAME … count`, the ▎ bar on every row, a blank row between cards", (t) => {
	look(t, "float", "transparent");
	const lines = rail(t, painted);
	const text = plainRows(lines);
	const jobsAt = text.findIndex((line) => / ▎ ▾ JOBS +1 running$/.test(line));
	assert.ok(jobsAt > 0, text.join("\n"));
	assert.equal(text[jobsAt - 1], " ▎", "the float card's padding row above the header");
	assert.match(text[jobsAt + 1]!, /^ ▎ ⧗ bg-1 npm test +1m 5s$/, "rows lose their panel indent inside the card");
	assert.equal(text[jobsAt + 2], " ▎", "the closing padding row");
	assert.equal(text[jobsAt + 3], "", "a blank row between two cards");
	assert.match(text[jobsAt + 5]!, /^ ▎ ▾ LDD/);
	assert.ok(text.some((line) => /^ ▎ ▾ CHANGES +none$/.test(line)) && text.some((line) => /^ ▎ ▾ INTEGRATIONS +1$/.test(line)), "every Status section is its own card");
	assert.ok(lines.filter((line) => line.includes("▎")).every((line) => line.startsWith(` ${BG}`)), "each card's rows are tinted inside a one-column margin");
	assert.ok(lines.filter((line) => line.trim() === "").every((line) => !line.includes(BG)), "nothing paints between cards: the surface is transparent");
});

test("transparent + neon: each section its own rounded card, `╭─ ▾ NAME ── count ╮`, the rows between │, `╰──╯`", (t) => {
	look(t, "neon", "transparent");
	const text = plainRows(rail(t, painted));
	const jobsAt = text.findIndex((line) => /^╭─ ▾ JOBS ─+ 1 running ╮$/.test(line));
	assert.ok(jobsAt >= 0, text.join("\n"));
	assert.match(text[jobsAt + 1]!, /^│ ⧗ bg-1 npm test +1m 5s │$/);
	assert.match(text[jobsAt + 2]!, /^╰─+╯$/);
	assert.ok(text.some((line) => /^╭─ ▾ CHANGES ─+ none ╮$/.test(line)));
	for (const line of text.filter((line) => line.startsWith("╭") || line.startsWith("│") || line.startsWith("╰"))) assert.equal(visibleWidth(line), text.find((l) => l.startsWith("╭"))!.length, line);
});

test("tinted: one painted panel, `▾ NAME … count` headers and their rows on it, the blank rows between sections painted too, down to the bottom", (t) => {
	look(t, "float", "tinted");
	const lines = rail(t, painted);
	const text = plainRows(lines);
	assert.ok(text.some((line) => /^ ▾ JOBS +1 running$/.test(line)), text.join("\n"));
	assert.ok(text.some((line) => /^ {3}⧗ bg-1 npm test +1m 5s$/.test(line)));
	assert.ok(text.some((line) => /^ ▾ CHANGES +none$/.test(line)));
	assert.doesNotMatch(text.join("\n"), /[╭│╰▎]/, "no card chrome on a tinted panel");
	const first = text.findIndex((line) => line.includes("▾"));
	assert.ok(lines.slice(first).every((line) => line.startsWith(BG)), "every row from the first section on is on the panel surface, the gaps included");
});

// G's Density (L195): comfortable keeps a blank row between rail sections;
// compact and minimal close them up, except float cards, which would fuse.
test("density: comfortable keeps a blank row between sections, compact and minimal none (tinted panel, neon cards); float cards keep theirs", (t) => {
	look(t, "float", "tinted");
	const blankBetween = (text: string[]) => { const first = text.findIndex((line) => line.includes("▾")); return text.slice(first).some((line) => line.trim() === ""); };
	assert.equal(blankBetween(plainRows(rail(t, painted, "comfortable"))), true, "comfortable: a gap");
	for (const density of ["compact", "minimal"] as const) {
		const text = plainRows(rail(t, painted, density));
		assert.equal(blankBetween(text), false, `${density}: ${text.join("\n")}`);
		const jobsAt = text.findIndex((line) => /^ ▾ JOBS/.test(line));
		assert.match(text[jobsAt + 2]!, /^ ▾ LDD/, `${density}: LDD right under JOBS`);
	}
	look(t, "neon", "transparent");
	const neon = plainRows(rail(t, painted, "compact"));
	const closing = neon.findIndex((line) => /^╰/.test(line));
	assert.match(neon[closing + 1]!, /^╭─ ▾ /, "neon: the next card right under");
	look(t, "float", "transparent");
	const float = plainRows(rail(t, painted, "compact"));
	const jobsAt = float.findIndex((line) => / ▎ ▾ JOBS/.test(line));
	assert.equal(float[jobsAt + 3], "", "float: the blank row stays, as G's");
});

test("ASCII: the fold is v or >, neon cards draw + - |", (t) => {
	look(t, "neon", "transparent", true);
	const text = plainRows(rail(t, plain));
	assert.ok(text.some((line) => /^\+- v JOBS -+ 1 running \+$/.test(line)), text.join("\n"));
	assert.ok(text.some((line) => /^\| ~ bg-1 npm test +1m 5s \|$/.test(line) || /^\| ⧗ bg-1 npm test/.test(line)));
	assert.ok(text.some((line) => /^\+-+\+$/.test(line)));
});

test("under a multiplexer that shows the path the Status sections are the same (L112)", (t) => {
	look(t, "float", "transparent");
	assert.deepEqual(renderShellStatusSections(status(true), plain, 48), renderShellStatusSections(status(false), plain, 48));
});
