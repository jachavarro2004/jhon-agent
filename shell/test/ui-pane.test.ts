/**
 * The right pane and its narrow strip (T174-S4), drawn to design/shell-tui 01
 * (180 columns: the conversation left, the pane right behind one thin
 * divider, the status bar across the whole width) and 04 (100 columns: the
 * pane folds into one strip under the top row). The frames are pi-tui's own
 * (its layout renderer, the pinned version in shell/node_modules) with the
 * existing rail sections in the pane: LDD, AGENTS and JOBS.
 *
 * The pane rides a pi-tui internal (L1469): one test pins it against the
 * pinned pi, another takes it away and expects the narrow form, never a
 * broken layout.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-pane.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { Text, TuiAltScreen, VStack, type Component, type TUI } from "@earendil-works/pi-tui";
import { renderLayoutFrame } from "@earendil-works/pi-tui/dist/layout.js";
import { LAYOUT_NODE } from "@earendil-works/pi-tui/dist/layout-node.js";
import { installPane, invalidatePane, paneSlot, paneState, renderStrip, stripModel, PANE_COLUMNS, PANE_TEXT, type PanePart } from "../lib/ui/pane.ts";
import { layoutHookState, LAYOUT_HOOK_MISSING_NOTE, PI_LAYOUT_NODE, resetLayoutHookNote } from "../lib/ui/pi-layout-hook.ts";
import { renderStatusBar, renderTopRow, type BarModel } from "../lib/ui/bars.ts";
import { renderShellAgentsSection, renderShellJobsSection, renderShellStatusLayout, type RunningJobDisplay } from "../lib/ui/shell-bar.ts";
import type { LddStatus } from "../lib/ui/ldd-status.ts";
import { stripAnsi } from "../lib/ui/primitives/index.ts";
import { assertRows, padded, plainTheme, spread } from "./rows.ts";

// The design's story (design/shell-tui/.src/parts.py): acme_app, mid-slice 3.
const NOW = Date.parse("2026-11-20T12:00:00Z");
const BARS: BarModel = {
	cwd: "/home/dev/acme_app", branch: "feat/add-invoice-export", sandbox: "on",
	modelId: "opus-5-5", effort: "high", contextPercent: 38, contextWindow: 1_000_000, costTotal: 1.42, subscription: false,
};
const LDD: LddStatus = {
	change: "add-invoice-export",
	slice: "Slice 3: Date-range filter",
	slices: ["done", "done", "current", "pending"],
	detail: [
		{ name: "Invoice model and CSV writer", total: 4, completed: 4 },
		{ name: "Export endpoint", total: 3, completed: 3 },
		{ name: "Date-range filter", total: 4, completed: 2 },
		{ name: "Download button in billing", total: 3, completed: 0 },
	],
	tasks: { completed: 9, total: 14 },
	strictTdd: true,
	route: "standard",
	next: "apply slice 3",
	tests: { passed: true, failed: 0, at: new Date(NOW - 6 * 60_000).toISOString() },
};
const JOBS: RunningJobDisplay[] = [
	{ id: "a1", kind: "agent", label: "jhon-builder: slice 3", startedAt: NOW - 252_000 },
	{ id: "a2", kind: "agent", label: "jhon-checker: slice 2 tests", startedAt: NOW - 63_000 },
	{ id: "j1", kind: "command", label: "pytest tests/billing -k range", startedAt: NOW - 18_000 },
];

/** pi's full-screen root, as pi-coding-agent's chat viewport builds it: the transcript over the dock (editor, footer). */
function piRoot(footer: Component = new Text("", 0, 0)): Component {
	const transcript = new Text(" Slice 2's endpoint answers the CSV and its 14 tests pass.", 0, 0);
	const editor: Component = { render: (width) => ["─".repeat(width), " ❯ Ask, or / for commands", "─".repeat(width)], invalidate() {} };
	const dock = new VStack([{ component: editor, shrink: 1, minSize: 3 }, { component: footer, shrink: 1, minSize: 0 }]);
	return new VStack([
		{ component: transcript, basis: 0, grow: 1, shrink: 1, minSize: 1 },
		{ component: dock, basis: "auto", grow: 0, shrink: 1, minSize: 1 },
	]);
}

type FakeTui = { mode?: string; terminal: { columns: number; rows: number }; layoutRoot?: Component; requestRender(): void };

/** A full-screen TUI over pi's root, with the bars and the rail sections registered as ui.ts registers them. */
function shell(columns: number, rows = 24, tui: FakeTui = { mode: "fullscreen", terminal: { columns, rows }, layoutRoot: piRoot(), requestRender() {} }, placement: () => "auto" | "hidden" = () => "auto") {
	const host = tui as unknown as TUI;
	const disposers = [
		paneSlot(host, "header", { render: (width) => [renderTopRow(BARS, plainTheme, width, { home: "/home/owner", ascii: false })], invalidate() {} }),
		paneSlot(host, "statusbar", { render: (width) => [renderStatusBar(BARS, plainTheme, width, { ascii: false, compactAt: null })], invalidate() {} }),
		paneSlot(host, "ldd", {
			render: (width) => renderShellStatusLayout({ ldd: LDD }, plainTheme, width).lines,
			strip: (width) => renderStrip(stripModel(LDD, NOW, "alt+j"), plainTheme, width),
			invalidate() {},
		}),
		paneSlot(host, "jobs", { render: (width) => renderShellJobsSection(JOBS, plainTheme, width, NOW), invalidate() {} }),
		paneSlot(host, "agents", { render: (width) => renderShellAgentsSection(JOBS, plainTheme, width, NOW), invalidate() {} }),
	];
	const notes: string[] = [];
	const uninstall = installPane(host, plainTheme, { placement, density: () => "comfortable", note: (text) => notes.push(text) });
	const frame = (width = tui.terminal.columns, height = tui.terminal.rows) => {
		tui.terminal.columns = width;
		return renderLayoutFrame(tui.layoutRoot!, width, height, () => {}).lines.map(stripAnsi);
	};
	return { tui, host, frame, notes, state: paneState(host), dispose: () => { uninstall(); for (const dispose of disposers) dispose(); } };
}

test("01 at 180 columns: the top row, the conversation left and the pane right behind one thin divider, the status bar across the width", (t) => {
	const s = shell(180);
	t.after(s.dispose);
	assert.equal(s.state.hosted?.(), true);
	assert.equal(s.state.wide?.(), true);
	const left = (text = "") => padded(text, 180 - PANE_COLUMNS);
	const pane = (text = "") => ` │ ${padded(text, PANE_TEXT + 2)}`;
	assertRows((width) => s.frame(width), {
		180: [
			padded(" 力 acme_app  /home/dev/acme_app", 180),
			left(" Slice 2's endpoint answers the CSV and its 14 tests pass.") + pane(),
			// The existing rail sections, unchanged (S5 draws the panel to 01).
			left() + pane(spread("▾ LDD", "standard route", PANE_TEXT)),
			left() + pane("  ◆ add-invoice-export"),
			left() + pane("    2/4 slices · on 3: Date-range filter"),
			left() + pane("    next apply slice 3 · tests ✓ passed"),
			left() + pane(),
			left() + pane(spread("▾ JOBS", "1 running", PANE_TEXT)),
			left() + pane(spread("  ⧗ j1 pytest tests/billing -k range", "18s", PANE_TEXT)),
			left() + pane(),
			left() + pane(spread("▾ AGENTS", "2 running", PANE_TEXT)),
			left() + pane(spread("  ◐ slice 3               jhon-buil…", "4m12s", PANE_TEXT)),
			left() + pane(spread("  ◐ slice 2 tests         jhon-chec…", "1m03s", PANE_TEXT)),
			...Array.from({ length: 7 }, () => left() + pane()),
			"─".repeat(133) + pane(),
			padded(" ❯ Ask, or / for commands", 133) + pane(),
			"─".repeat(133) + pane(),
			spread(" ⎇ feat/add-invoice-export │ sandbox on", "opus-5-5 · high │ ctx ▰▰▰▱▱▱▱▱ 38% │ $1.42 ", 180),
		],
		100: [
			padded(" 力 acme_app  /home/dev/acme_app", 100),
			spread(" ◆ add-invoice-export  ◐ slice 3/4 · tasks 2/4 · next apply slice 3  ✓ passed 6m", "alt+j ▾ ", 100),
			padded(" Slice 2's endpoint answers the CSV and its 14 tests pass.", 100),
			// pi paints its own rows as they come: no pane, nothing pads them.
			...Array.from({ length: 17 }, () => ""),
			"─".repeat(100),
			" ❯ Ask, or / for commands",
			"─".repeat(100),
			spread(" ⎇ feat/add-invoice-… │ sandbox on", "opus-5-5 · high │ ctx 38% │ $1.42 ", 100),
		],
	}, "the main screen");
});

test("the pane has a fixed width: wider terminals give the conversation the room, and 140 columns is the breakpoint", (t) => {
	const s = shell(180);
	t.after(s.dispose);
	for (const width of [140, 180, 240]) {
		const row = s.frame(width)[2]!;
		assert.equal(row.indexOf("│"), width - PANE_COLUMNS + 1, `the divider at ${width} columns`);
		assert.equal(s.state.wide?.(), true);
	}
	const narrow = s.frame(139);
	assert.equal(narrow.some((row) => row.includes(" │ ▾ LDD")), false, "139 columns: no pane");
	assert.match(narrow[1]!, /^ ◆ add-invoice-export/, "the strip under the top row");
	assert.equal(s.state.wide?.(), false);
});

test("Status hidden takes the status bar, the pane and the strip away at every width; the top row stays", (t) => {
	let placement: "auto" | "hidden" = "hidden";
	const s = shell(180, 12, undefined, () => placement);
	t.after(s.dispose);
	for (const width of [180, 100]) {
		const rows = s.frame(width, 12);
		assert.match(rows[0]!, /^ 力 acme_app/, `${width}: the top row`);
		assert.equal(rows.some((row) => /▾ LDD|◆ add-invoice|sandbox on/.test(row)), false, `${width}: ${rows.join("\n")}`);
		assert.equal(s.state.wide?.(), false);
	}
	placement = "auto";
	assert.match(s.frame(180, 12).at(-1)!, /sandbox on/, "auto: the status bar again");
	assert.equal(s.state.wide?.(), true);
});

test("a click in the pane reaches the part under it, in the part's own rows", (t) => {
	const s = shell(180);
	t.after(s.dispose);
	const seen: Array<{ y: number; width: number }> = [];
	const part: PanePart = { render: () => ["▾ JOBS", "  ⧗ build"], invalidate() {}, handleMouse: (event) => { seen.push({ y: event.y, width: event.width }); return { handled: true }; } };
	paneSlot(s.host, "jobs", part);
	invalidatePane(s.host);
	s.frame(180);
	// The pane content row 7 is JOBS's second row (a blank row, LDD's four, a blank row, then JOBS).
	const pane = (tui: FakeTui) => (tui.layoutRoot as Component & { [PI_LAYOUT_NODE](): { entries: Array<{ component: Component & { [PI_LAYOUT_NODE]?(): { entries: Array<{ component: { child?: Component } }> } } }> } })[PI_LAYOUT_NODE]();
	const split = pane(s.tui).entries[2]!.component;
	const content = split[PI_LAYOUT_NODE]!().entries[2]!.component.child!;
	assert.deepEqual(content.handleMouse?.({ type: "click", button: "left", x: 4, y: 7, width: 44, height: 20 } as never), { handled: true });
	assert.deepEqual(seen, [{ y: 1, width: PANE_TEXT }]);
	assert.equal(content.handleMouse?.({ type: "click", button: "left", x: 4, y: 0, width: 44, height: 20 } as never), undefined, "the blank row above the sections is no part's");
});

test("the internal the pane rides is where the pinned pi-tui keeps it (L1469)", () => {
	assert.equal(PI_LAYOUT_NODE, LAYOUT_NODE, "pi-tui's layout symbol");
	const terminal = { columns: 120, rows: 30, write() {}, start() {}, stop() {}, hideCursor() {}, showCursor() {} };
	const tui = new TuiAltScreen(terminal as never, false);
	assert.equal(layoutHookState(tui), "pending", "a full-screen TUI keeps a layout root, unset until pi mounts one");
	tui.setLayoutRoot(piRoot());
	assert.equal(layoutHookState(tui), "ready", "pi's root answers the layout symbol");
	const s = shell(120, 24, tui as unknown as FakeTui);
	try {
		assert.equal(s.state.hosted?.(), true, "the hook takes pi's real TuiAltScreen");
		assert.deepEqual(s.notes, []);
		assert.match(s.frame(120)[1]!, /^ ◆ add-invoice-export/);
	} finally { s.dispose(); }
	assert.equal(Object.prototype.hasOwnProperty.call((tui as unknown as { layoutRoot: object }).layoutRoot, PI_LAYOUT_NODE), false, "dispose gives pi its own layout back");
});

test("without the internal the pane says so once and keeps the narrow form, never a broken layout", (t) => {
	resetLayoutHookNote();
	// A pi-tui that moved the internal: a full-screen TUI with no layoutRoot, then one whose root has no layout method.
	const moved: FakeTui = { mode: "fullscreen", terminal: { columns: 180, rows: 24 }, requestRender() {} };
	const s = shell(180, 24, moved);
	t.after(s.dispose);
	assert.deepEqual(s.notes, [LAYOUT_HOOK_MISSING_NOTE]);
	assert.equal(s.state.hosted?.(), false, "not hosted: the footer and the chrome widget draw the narrow form");
	assert.equal(s.state.wide?.(), false, "no pane");
	const plain: FakeTui = { mode: "fullscreen", terminal: { columns: 180, rows: 24 }, layoutRoot: { render: () => ["pi's own"], invalidate() {} }, requestRender() {} };
	const again = shell(180, 24, plain);
	t.after(again.dispose);
	assert.equal(again.state.hosted?.(), false);
	assert.deepEqual(again.notes, [], "the note is given once per process");
	assert.deepEqual(renderLayoutFrame(plain.layoutRoot!, 180, 2, () => {}).lines.map(stripAnsi), ["pi's own", ""], "pi's layout is left as it was");
	assert.equal(layoutHookState({ terminal: {} } as never), "regular", "a regular terminal has no layout root by design: no note");
});

test("the strip gives way from the right: the key, the last run, the next step, the tasks, the slice; the change is the last left", () => {
	const model = stripModel(LDD, NOW, "alt+j");
	const at = (width: number) => stripAnsi(renderStrip(model, plainTheme, width));
	assert.equal(at(100), spread(" ◆ add-invoice-export  ◐ slice 3/4 · tasks 2/4 · next apply slice 3  ✓ passed 6m", "alt+j ▾ ", 100));
	assert.equal(at(82), padded(" ◆ add-invoice-export  ◐ slice 3/4 · tasks 2/4 · next apply slice 3  ✓ passed 6m", 82), "the key goes first");
	assert.equal(at(80), padded(" ◆ add-invoice-export  ◐ slice 3/4 · tasks 2/4 · next apply slice 3", 80), "then the last run");
	assert.equal(at(60), padded(" ◆ add-invoice-export  ◐ slice 3/4 · tasks 2/4", 60));
	assert.equal(at(30), padded(" ◆ add-invoice-export", 30));
	assert.equal(at(12), " ◆ add-inv… ");
	assert.equal(stripAnsi(renderStrip({ change: "x", lastRun: { passed: false, failed: 3, ago: "2h" } }, plainTheme, 40)), padded(" ◆ x  ✗ failed 3 2h", 40));
	assert.equal(stripAnsi(renderStrip(model, plainTheme, 100, { ascii: true })), spread(" # add-invoice-export  ~ slice 3/4 - tasks 2/4 - next apply slice 3  + passed 6m", "alt+j v ", 100));
});
