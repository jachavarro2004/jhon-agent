/**
 * The split chrome as the Gentle Shell draws it (T15.12d slice 6, L153
 * answer 3): a title row on top (`力 name  path`, the session name at the
 * right; narrow, the model and the profile) and the status bar at the bottom
 * (branch, dirty count, the mode's badge and, without a rail, `⧗ N jobs` on
 * the left; model · effort · profile, the context meter, the cost and
 * Jhon's usage windows on the right). Wide, comfortable: each between two
 * margin rows. Painted on the panel only when the surface is tinted. Jhon's
 * 70/90 ladder and its state glyphs stay (C8).
 *
 *   cd shell && node --experimental-strip-types --test test/ui-shell-chrome.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test, { type TestContext } from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { renderShellBar, renderShellChromeBar, renderShellStatusBar, renderShellTitleRow, renderShellTitleRows, resetText, type ShellBarModel, type ShellBarTheme } from "../lib/ui/shell-bar.ts";
import { chromeSurface, setChromeSurface } from "../lib/ui/shell-panel.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import type { ProviderUsage } from "../lib/ui/shell-usage.ts";

const plain: ShellBarTheme = { fg: (_c, t) => t, bold: (t) => t };
const tagged: ShellBarTheme = { fg: (c, t) => `<${c}>${t}</${c}>`, bold: (t) => `<b>${t}</b>` };
const BG = "\x1b[48;5;236m";
const painted = { ...plain, bg: (_c: string, t: string) => `${BG}${t}\x1b[49m` };
const NOW = 1_788_600_000_000;
const usage = (fiveHour: number, week: number): ProviderUsage => ({
	provider: "claude-bridge", plan: undefined, fetchedAt: NOW,
	limits: [{ name: "claude", limitReached: false, windows: [
		{ label: "5h", usedPercent: fiveHour, windowSeconds: 18_000, resetAt: NOW + 2 * 3_600_000 },
		{ label: "week", usedPercent: week, windowSeconds: 604_800, resetAt: NOW + 3 * 86_400_000 + 2 * 3_600_000 },
	] }],
});
const model = (overrides: Partial<ShellBarModel> = {}): ShellBarModel => ({
	cwd: "/home/owner/code/jhon", branch: "main", dirty: undefined, sessionName: undefined, modelId: "claude-opus-5-5", effort: "high", profile: "jhon",
	contextPercent: 45, contextWindow: 1_000_000, costTotal: 0, subscription: true, usage: undefined, statuses: [], ...overrides,
});
const text = (row: string) => stripAnsi(row);

function surface(t: TestContext, value: "transparent" | "tinted") {
	const before = chromeSurface();
	setChromeSurface(value);
	t.after(() => setChromeSurface(before));
}

test("the title row: ` 力 name  ~/path` on the left, the session name on the right, a one-column margin each side, exactly the width", (t) => {
	surface(t, "transparent");
	const row = renderShellTitleRow(model({ sessionName: "rail work" }), plain, 100, "/home/owner");
	assert.equal(visibleWidth(row), 100);
	assert.match(text(row), /^ 力 jhon {2}~\/code\/jhon +rail work $/);
	assert.match(renderShellTitleRow(model(), tagged, 160, "/home/owner"), /^ <accent>力<\/accent> <text><b>jhon<\/b><\/text> {2}<muted>~\/code\/jhon<\/muted>/, "the brand in the accent, the name bold");
	assert.match(text(renderShellTitleRow(model(), plain, 100, "/home/owner")), /^ 力 jhon {2}~\/code\/jhon +$/, "no session name: nothing at the right");
});

test("narrow, the title row carries the model · effort · profile at its right, as G's (the status bar leaves them out)", (t) => {
	surface(t, "transparent");
	const row = text(renderShellTitleRow(model(), plain, 100, "/home/owner", { model: true }));
	assert.match(row, /^ 力 jhon {2}~\/code\/jhon +claude-opus-5-5 · high · jhon $/);
	const narrow = text(renderShellTitleRow(model(), plain, 40, "/home/owner", { model: true }));
	assert.equal(visibleWidth(narrow), 40);
	assert.match(narrow, /claude-opus-5-5 · high · jhon $/, "the model stays whole");
	// G's renderShellTitleRow clips the path, never drops it (T15.12g); G's
	// rows at these widths, one cell shorter for 力, two cells wide where ✿ is one.
	assert.equal(narrow, " 力 jhon… claude-opus-5-5 · high · jhon ");
	assert.equal(text(renderShellTitleRow(model(), plain, 50, "/home/owner", { model: true })), " 力 jhon  ~/code/j… claude-opus-5-5 · high · jhon ");
	assert.equal(text(renderShellTitleRow(model({ sessionName: "a long session name here" }), plain, 40, "/home/owner")), " 力 jhon  ~/c… a long session name here ");
});

test("when a multiplexer host shows the path (jhon:host showsPath) the title row names the project only; without it the path shows (L112)", (t) => {
	surface(t, "transparent");
	const row = text(renderShellTitleRow(model({ hostShowsPath: true, sessionName: "s" }), plain, 100, "/home/owner"));
	assert.match(row, /^ 力 jhon +s $/);
	assert.doesNotMatch(row, /~\/code/);
	assert.match(text(renderShellTitleRow(model({ sessionName: "s" }), plain, 100, "/home/owner")), /^ 力 jhon {2}~\/code\/jhon +s $/);
});

test("the status bar: branch ±dirty │ the mode's badge │ ⧗ N jobs on the left; model · effort · profile │ ctx │ cost │ the usage windows on the right", (t) => {
	surface(t, "transparent");
	const jobs = [{ id: "a", label: "npm test", startedAt: NOW }, { id: "b", label: "lint", startedAt: NOW }];
	const bar = renderShellStatusBar(model({ dirty: 2, mode: "yolo", usage: usage(20, 62), jobs }), plain, 200, { jobs: true });
	assert.equal(bar.rows.length, 1);
	const row = text(bar.rows[0]!);
	assert.equal(visibleWidth(row), 200);
	const reset = resetText(NOW + 3 * 86_400_000 + 2 * 3_600_000);
	assert.match(row, new RegExp(`^ main ±2 │ YOLO │ ⧗ 2 jobs +claude-opus-5-5 · high · jhon │ ctx ▰▰▰▰▱▱▱▱ 45% │ \\$0\\.000 sub │ week ▰▰▰▰▰▱▱▱ 62% · resets ${reset} $`));
	assert.doesNotMatch(text(renderShellStatusBar(model({ jobs }), plain, 200).rows[0]!), /jobs/, "beside the rail the jobs are the rail's");
	const agent = { id: "c", kind: "agent" as const, label: "explore: x", startedAt: NOW };
	assert.match(text(renderShellStatusBar(model({ jobs: [...jobs, agent] }), plain, 200, { jobs: true }).rows[0]!), /⧗ 2 jobs/, "a sub-agent is no job: it leads the work row");
	assert.doesNotMatch(text(renderShellStatusBar(model({ mode: "normal" }), plain, 200).rows[0]!), /YOLO|│ +│/, "normal: no badge");
	assert.match(renderShellStatusBar(model({ mode: "super-yolo" }), tagged, 300).rows[0]!, /<b><error>SUPER YOLO<\/error><\/b>/, "Jhon's badge roles (S31)");
	const narrow = text(renderShellStatusBar(model(), plain, 100, { modelDetails: false }).rows[0]!);
	assert.doesNotMatch(narrow, /claude/, "narrow: the model is the title row's");
	assert.match(narrow, /^ main +ctx ▰▰▰▰▱▱▱▱ 45% │ \$0\.000 sub $/);
});

test("the usage windows keep Jhon's ladder: 5h from 70 (warn) and 90 (bad), the reset on the window with the highest tone (S1, S3, C8)", (t) => {
	surface(t, "transparent");
	// Tagged roles count as columns: read at a width they fit in.
	const bar = (fiveHour: number, week: number, theme = plain) => renderShellStatusBar(model({ usage: usage(fiveHour, week) }), theme, 600).rows[0]!;
	assert.doesNotMatch(bar(69, 10), /5h/);
	assert.match(bar(70, 10, tagged), /<warning>5h 70%<\/warning>/);
	assert.match(bar(90, 10, tagged), /<error>5h 90%<\/error>/);
	assert.match(bar(0, 69, tagged), /<success>69%<\/success>/);
	assert.match(bar(0, 70, tagged), /<warning>70%<\/warning>/);
	const fiveReset = resetText(NOW + 2 * 3_600_000);
	assert.match(text(bar(84, 62)), new RegExp(`5h 84% · resets ${fiveReset} │ week ▰▰▰▰▰▱▱▱ 62% +$`));
});

test("narrowing, as G's cascade: at each usage shape the profile gives way first, the usage's reset and gauges go, then the windows but the first, then the usage, then the model; the badge stays last", (t) => {
	surface(t, "transparent");
	const data = model({ mode: "super-yolo", usage: usage(84, 62) });
	const at = (width: number) => text(renderShellStatusBar(data, plain, width).rows[0]!);
	// What each width still shows, widest first: each fact goes once and never comes back, in G's order.
	const facts = { reset: /resets/, gauge: /week ▰/, week: /week/, usage: /5h/, model: /claude/ } as Record<string, RegExp>;
	const order = Object.keys(facts);
	let gone = 0;
	for (let width = 220; width >= 14; width--) {
		const row = at(width);
		assert.equal(visibleWidth(row), width, `${width}: ${row}`);
		assert.match(row, /SUPER YOLO/, `${width}: the badge stays`);
		const shown = order.map((fact) => facts[fact]!.test(row));
		const first = shown.indexOf(true);
		assert.ok(first < 0 || shown.slice(first).every(Boolean), `${width}: the facts go in G's order: ${row}`);
		while (gone < order.length && !shown[gone]) gone++;
		for (let i = 0; i < gone; i++) assert.equal(shown[i], false, `${width}: ${order[i]} came back: ${row}`);
	}
	assert.equal(gone, order.length, "at 14 columns only the branch and the badge are left");
	assert.doesNotMatch(at(60), /claude/, at(60));
});

// G's Density (L195): comfortable keeps the margin rows around the title row
// and the status bar; compact and minimal draw the row alone.
test("density: comfortable pads the title row and the status bar with a margin row each side, compact and minimal draw no pad", () => {
	assert.equal(renderShellChromeBar(model(), plain, 120).rows.length, 3, "comfortable by default, as G's");
	assert.equal(renderShellChromeBar(model(), plain, 120, {}, "comfortable").rows.length, 3);
	assert.equal(renderShellTitleRows(model(), plain, 120, "/home/owner").length, 3);
	for (const density of ["compact", "minimal"] as const) {
		const bar = renderShellChromeBar(model(), plain, 120, {}, density);
		assert.equal(bar.rows.length, 1, density);
		assert.equal(bar.headerRow, 0, `${density}: the usage hit stays on the bar row`);
		assert.deepEqual(renderShellTitleRows(model(), plain, 120, "/home/owner", density), [renderShellTitleRow(model(), plain, 120, "/home/owner")], density);
	}
});

test("the chrome bar: comfortable, the status bar between two margin rows; tinted paints all three on the panel, transparent none", (t) => {
	surface(t, "tinted");
	const tinted = renderShellChromeBar(model(), painted, 120);
	assert.equal(tinted.rows.length, 3);
	assert.equal(tinted.headerRow, 1);
	assert.ok(tinted.rows.every((row) => row.startsWith(BG)), "the panel surface");
	assert.equal(text(tinted.rows[0]!), " ".repeat(120));
	setChromeSurface("transparent");
	const clear = renderShellChromeBar(model(), painted, 120);
	assert.ok(clear.rows.every((row) => !row.includes(BG)), "transparent: the terminal's background");
	assert.ok(!renderShellTitleRow(model(), painted, 120).includes(BG));
	setChromeSurface("tinted");
	assert.ok(renderShellTitleRow(model(), painted, 120).startsWith(BG));
});

test("the regular terminal's bar is G's renderShellBar: `力 jhon ⟡ path branch ±dirty ⟡ model · effort ⟡ ctx ⟡ cost ⟡ usage ⟡ jobs ⟡ mode ⟡ statuses`, the session name right; overflowing, the path shrinks, then segments go from the end", () => {
	const full = model({ cwd: "~/code/jhon", dirty: 3, sessionName: "rail work", usage: usage(10, 62), mode: "yolo", statuses: ["MCP: 2 servers"], jobs: [{ id: "j", label: "build", startedAt: 1 }] });
	const wide = text(renderShellBar(full, plain, 220)[0]!);
	assert.match(wide, /^力 jhon ⟡ ~\/code\/jhon main ±3 ⟡ claude-opus-5-5 · high ⟡ ctx ▰+▱+ 45% ⟡ \$0\.000 sub ⟡ week ▰+▱+ 62%[^⟡]* ⟡ ⧗ 1 job ⟡ YOLO ⟡ MCP: 2 servers +rail work$/);
	assert.equal(visibleWidth(wide), 220);
	const mid = text(renderShellBar(full, plain, 120)[0]!);
	assert.doesNotMatch(mid, /rail work/, "the session name goes first");
	const narrow = text(renderShellBar(full, plain, 60)[0]!);
	assert.ok(visibleWidth(narrow) <= 60);
	assert.match(narrow, /^力 jhon ⟡ jhon main ±3 ⟡/, "the path keeps its last segment");
	assert.match(renderShellBar(model(), tagged, 200)[0]!, /^<accent>力 jhon<\/accent> <dim>⟡<\/dim> /);
});

// G's regular bar (renderShellBar) carries its jobs count and its permission
// mode as extension statuses ("gentle-jobs", "gentle:mode"), in the key order
// of every status; Jhon's count and badge take those places as "jhon-jobs"
// and "jhon:mode", so the statuses that sort before them lead (T15.12g).
test("the regular bar orders the jobs count and the mode's badge among the statuses by key, as G's", () => {
	const jobs = [{ id: "a", label: "npm test", startedAt: NOW }];
	const row = renderShellBar(model({ cwd: "~/code/jhon", mode: "yolo", jobs, statuses: ["CodeGraph: ready", "MCP: 2 servers"], statusKeys: ["codegraph", "mcp"] }), plain, 220)[0]!;
	assert.match(text(row), /^力 jhon ⟡ ~\/code\/jhon main ⟡ claude-opus-5-5 · high ⟡ ctx \S+ 45% ⟡ \$0\.000 sub ⟡ CodeGraph: ready ⟡ ⧗ 1 job ⟡ YOLO ⟡ MCP: 2 servers$/);
	// Narrowing pops from the end, as G's: the statuses after the badge go first.
	assert.match(text(renderShellBar(model({ cwd: "~/code/jhon", mode: "yolo", jobs, statuses: ["CodeGraph: ready", "MCP: 2 servers"], statusKeys: ["codegraph", "mcp"] }), plain, 120)[0]!), /^力 jhon ⟡ jhon main ⟡ .* ⟡ CodeGraph: ready ⟡ ⧗ 1 job ⟡ YOLO$/);
});
