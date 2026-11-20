/**
 * T174-S5b part 2 (L1493-L1496): what T174 dropped from the right pane and
 * the bars without an owner decision, back in Jhon's own code.
 *
 * - Finished work stays a minute: the newest three jobs (✓ ✗ –), a failed
 *   sub-agent, and the `✓ N done` and `– N cancelled` rows; the sections show
 *   only while something runs or lingers.
 * - Agent rows carry their tokens; Agents keeps eight rows and a
 *   `… N more · /jhon:agents` row; job rows carry their id.
 * - The pane: a click on a section's title folds it, the pointer paints
 *   titles and the Running block's rows, and the wheel is the pane's own (the
 *   part under it first, the todo list's), never the conversation's: through
 *   pi's own TuiAltScreen and ScrollView.
 * - Two forms (L1495): comfortable draws each section in its own box; compact
 *   keeps one blank row between sections (01: between Last run and Agents).
 * - The bar: other extensions' statuses, the branch's `±N`, and the profile
 *   gives way before the windows' meters and resets.
 *
 * Row snapshots at the pane's 42 columns (both densities) and the narrow
 * form at 100.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-pane-sections.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { ScrollView, Text, TuiAltScreen, VStack, visibleWidth, type Component, type TUI } from "@earendil-works/pi-tui";
import { renderLayoutFrame } from "@earendil-works/pi-tui/dist/layout.js";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { foldablePart, installPane, paneSlot, paneRows, PANE_COLUMNS, PANE_TEXT, type PanePart } from "../lib/ui/pane.ts";
import { AGENTS_ROWS, renderAgentsSection, renderJobsSection, renderLddPanel, renderRunningBlock, type RunningJobDisplay } from "../lib/ui/panel.ts";
import { renderStatusBar, type BarModel } from "../lib/ui/bars.ts";
import { SESSION_CHANGE_ENTRY } from "../lib/ui/session-changes.ts";
import type { LddStatus } from "../lib/ui/ldd-status.ts";
import { stripAnsi } from "../lib/ui/primitives/index.ts";
import jhonShellUi from "../extensions/ui.ts";
import { padded, plainTheme, rowsAt, spread } from "./rows.ts";
import { useMemorySettings } from "./fake-jhon-settings.ts";

const NOW = Date.parse("2026-11-20T12:00:00Z");
const W = PANE_TEXT;
const LDD: LddStatus = {
	change: "add-invoice-export",
	slice: "Slice 3: Date-range filter",
	slices: ["done", "done", "current", "pending"],
	detail: [
		{ name: "Invoice model and CSV writer", total: 4, completed: 4 },
		{ name: "Export endpoint", total: 3, completed: 3 },
		{ name: "Date-range filter", total: 4, completed: 2, tasks: [
			{ text: "parse from and to", done: true }, { text: "default to the current month", done: true },
			{ text: "reject an inverted range", done: false }, { text: "filter the invoice query", done: false },
		] },
		{ name: "Download button in billing", total: 3, completed: 0 },
	],
	tasks: { completed: 9, total: 14 },
	strictTdd: true, route: "standard", risk: "medium", next: "apply",
	tests: { passed: true, failed: 0, at: new Date(NOW - 6 * 60_000).toISOString(), scope: "affected", count: 42, seconds: 11.8, commit: "3f2a9c1" },
};
const agent = (id: string, label: string, started: number, more: Partial<RunningJobDisplay> = {}): RunningJobDisplay => ({ id, kind: "agent", label, startedAt: NOW - started, ...more });
const job = (id: string, label: string, started: number, more: Partial<RunningJobDisplay> = {}): RunningJobDisplay => ({ id, kind: "command", label, startedAt: NOW - started, ...more });
const WORK: RunningJobDisplay[] = [
	agent("agent-1", "jhon-builder: slice 3", 252_000, { tokens: 48_200 }),
	agent("agent-2", "jhon-checker: slice 2 tests", 63_000, { tokens: 950 }),
	job("job-1", "pytest tests/billing -k range", 18_000),
];
/** The pointer's hover role made visible: «text» for the warning role (a running task's glyph wears it too). */
const tagged = { ...plainTheme, fg: (role: string, text: string) => (role === "warning" ? `«${text}»` : text) };

const agents = (jobs: RunningJobDisplay[], options: { folded?: boolean; hover?: number; now?: number } = {}) =>
	rowsAt((w, theme) => renderAgentsSection(jobs, theme, w, { now: options.now ?? NOW, ...options }), W);
const jobs = (list: RunningJobDisplay[], options: { folded?: boolean; now?: number } = {}) =>
	rowsAt((w, theme) => renderJobsSection(list, theme, w, { now: options.now ?? NOW, ...options }), W);

// ---------------------------------------------------------------- Agents and Jobs

test("agent rows carry their tokens beside their time; job rows carry their id", () => {
	assert.deepEqual(agents(WORK), [
		"Agents  2 running",
		spread("◐ jhon-builder  slice 3", "48k · 4m12s", W),
		spread("◐ jhon-checker  slice 2 tests", "950 · 1m03s", W),
	]);
	assert.deepEqual(jobs(WORK), [
		"Jobs  1 running",
		spread("⧗ job-1 pytest tests/billing -k range", "18s", W),
	]);
});

test("finished jobs stay a minute, the newest three, ✓ a clean exit, ✗ a failure, – stopped; then the section goes", () => {
	const list = [
		job("job-1", "pytest -k range", 18_000),
		job("job-2", "npm run build", 40_000, { endedAt: NOW - 5_000, status: "exited", exitCode: 0 }),
		job("job-3", "npm run lint", 50_000, { endedAt: NOW - 10_000, status: "exited", exitCode: 1 }),
		job("job-4", "tail -f app.log", 70_000, { kind: "monitor", endedAt: NOW - 20_000, status: "stopped" }),
		job("job-5", "make docs", 80_000, { endedAt: NOW - 30_000, status: "exited", exitCode: 0 }),
	];
	assert.deepEqual(jobs(list), [
		"Jobs  1 running · 3 finished",
		spread("⧗ job-1 pytest -k range", "18s", W),
		spread("✓ job-2 npm run build", "35s", W),
		spread("✗ job-3 npm run lint", "40s", W),
		spread("– job-4 tail -f app.log", "50s", W),
	]);
	assert.deepEqual(jobs(list.slice(1, 2), { now: NOW + 54_000 }), ["Jobs  1 finished", spread("✓ job-2 npm run build", "35s", W)], "a finished job alone keeps the section for its minute");
	assert.deepEqual(jobs(list.slice(1, 2), { now: NOW + 55_000 }), [], "a minute after it ended it is gone, and the section with it");
});

test("a failed sub-agent stays a minute; the done and cancelled fold into one row each, the first naming /jhon:agents", () => {
	const list = [
		agent("agent-1", "jhon-builder: slice 3", 252_000, { tokens: 48_200 }),
		agent("agent-2", "jhon-checker: slice 2 tests", 63_000, { endedAt: NOW - 3_000, status: "failed" }),
		agent("agent-3", "jhon-scout: map billing", 90_000, { endedAt: NOW - 10_000, status: "completed" }),
		agent("agent-5", "jhon-checker: slice 1", 99_000, { endedAt: NOW - 30_000, status: "stopped" }),
		// The fourth to end: only the newest three finished stay.
		agent("agent-4", "jhon-scout: map exports", 95_000, { endedAt: NOW - 40_000, status: "completed" }),
	];
	assert.deepEqual(agents(list), [
		"Agents  1 running",
		spread("◐ jhon-builder  slice 3", "48k · 4m12s", W),
		spread("✗ jhon-checker  slice 2 tes…", "failed 1m00s", W),
		"✓ 1 done · /jhon:agents",
		"– 1 cancelled",
	]);
	assert.deepEqual(agents(list.slice(1)), [
		"Agents  1 failed · 1 done · 1 cancelled",
		spread("✗ jhon-checker  slice 2 tes…", "failed 1m00s", W),
		"✓ 1 done · /jhon:agents",
		"– 1 cancelled",
	], "nothing runs: the section stays while the finished ones do");
	assert.deepEqual(agents(list.slice(1), { now: NOW + 58_000 }), [], "a minute after the last one ended, no section");
});

test("Agents keeps eight rows: the first running ones, then `… N more · /jhon:agents` and the summary rows", () => {
	const many = Array.from({ length: 10 }, (_, i) => agent(`agent-${i + 1}`, `jhon-builder: slice ${i + 1}`, 100_000 - i * 1000));
	const rows = agents([...many, agent("agent-99", "jhon-scout: map", 5_000, { endedAt: NOW - 1_000, status: "completed" })]);
	assert.equal(rows.length, 1 + AGENTS_ROWS);
	assert.equal(rows[0], "Agents  10 running");
	assert.equal(rows[6], spread("◐ jhon-builder  slice 6", "1m35s", W));
	assert.deepEqual(rows.slice(7), ["… 4 more · /jhon:agents", "✓ 1 done · /jhon:agents"]);
});

test("a folded section is its title and its first row; the title wears ▸, and under the pointer the hover role and ▾", () => {
	assert.deepEqual(agents(WORK, { folded: true }), [spread("Agents  2 running", "▸", W), spread("◐ jhon-builder  slice 3", "48k · 4m12s", W)]);
	assert.deepEqual(jobs([...WORK, job("job-2", "npm test", 9_000)], { folded: true }), [spread("Jobs  2 running", "▸", W), spread("⧗ job-1 pytest tests/billing -k range", "18s", W), "… 1 more"]);
	const hovered = renderAgentsSection(WORK, tagged, W, { now: NOW, hover: 0 }).map(stripAnsi);
	assert.match(hovered[0]!, /^«Agents»  2 running +«▾»$/, "the title and the fold glyph in the hover role");
	const row = renderAgentsSection(WORK, tagged, W, { now: NOW, hover: 2 }).map(stripAnsi);
	assert.match(row[2]!, /^«◐ »«jhon-checker»  «slice 2 tests» +950 · 1m03s$/, "a row under the pointer: its text in the hover role");
	assert.match(row[1]!, /^«◐ »jhon-builder  slice 3/, "the others as they were");
	assert.deepEqual(rowsAt((w, theme) => renderLddPanel(LDD, theme, w, { now: NOW, folded: true }), W), [spread("◆ add-invoice-export", "▸", W), "next apply slice 3"], "LDD folds to its change and next step");
	assert.match(stripAnsi(renderLddPanel(LDD, tagged, W, { now: NOW, hover: 0 })[0]!), /^◆ «add-invoice-export» +«▾»$/);
});

// ---------------------------------------------------------------- the narrow form

test("04 at 100 columns: the Running block keeps the finished work of the last minute; the row under the pointer takes the hover", () => {
	const list = [...WORK, job("job-2", "npm run build", 40_000, { endedAt: NOW - 5_000, status: "exited", exitCode: 0 }), agent("agent-3", "jhon-scout: map", 20_000, { endedAt: NOW - 2_000, status: "completed" })];
	const block = renderRunningBlock(list, plainTheme, 100, { now: NOW });
	assert.deepEqual(rowsAt(() => block.rows, 100), [
		`── Running  2 agents · 1 job ${"─".repeat(71)}`,
		padded(spread("  ◐ jhon-builder  slice 3", "48k · 4m12s", 66), 100),
		padded(spread("  ◐ jhon-checker  slice 2 tests", "950 · 1m03s", 66), 100),
		padded("  ✓ 1 done · /jhon:agents", 100),
		padded(spread("  ⧗ job-1 pytest tests/billing -k range", "18s", 66), 100),
		padded(spread("  ✓ job-2 npm run build", "35s", 66), 100),
		padded("", 100),
	]);
	assert.deepEqual(block.opens, ["agents", "agents", "agents", "agents", "jobs", "jobs", undefined]);
	const over = renderRunningBlock(list, tagged, 100, { now: NOW, hover: 4 }).rows.map(stripAnsi);
	assert.match(over[4]!, /^ {2}«⧗ »«job-1 »«pytest tests\/billing -k range» +18s/);
	assert.equal(over.filter((row) => /«(jhon-|job-|pytest|npm)/.test(row)).length, 1, "one row at a time");
});

// ---------------------------------------------------------------- the pane host, through pi

/** pi's full-screen root: the transcript in pi's primary ScrollView, at its end, over the editor. */
function piRoot(): { root: Component; transcript: ScrollView } {
	const transcript = new ScrollView(new Text(Array.from({ length: 60 }, (_, i) => ` line ${i + 1}`).join("\n"), 0, 0), { primary: true, follow: "end" });
	const editor: Component = { render: (width) => ["─".repeat(width), " ❯ Ask", "─".repeat(width)], invalidate() {} };
	return {
		transcript,
		root: new VStack([
			{ component: transcript, basis: 0, grow: 1, shrink: 1, minSize: 1 },
			{ component: editor, basis: "auto", grow: 0, shrink: 1, minSize: 1 },
		]),
	};
}

/**
 * pi's own TuiAltScreen at columns × rows, its root hooked by the pane, the
 * sections registered as extensions/ui.ts registers them; `input` feeds pi a
 * raw mouse sequence and returns the frame after it.
 */
function hosted(columns: number, rows: number, density: "comfortable" | "compact" | "minimal", theme = plainTheme, extra: Record<string, PanePart> = {}) {
	const terminal = { columns, rows, write() {}, start() {}, stop() {}, hideCursor() {}, showCursor() {} };
	const tui = new TuiAltScreen(terminal as never, false);
	const { root, transcript } = piRoot();
	tui.setLayoutRoot(root);
	const host = tui as unknown as TUI;
	const opened: string[] = [];
	const disposers = [
		paneSlot(host, "ldd", foldablePart(host, { render: (width, view) => renderLddPanel(LDD, theme, width, { now: NOW, ...view }) })),
		paneSlot(host, "agents", foldablePart(host, { render: (width, view) => renderAgentsSection(WORK, theme, width, { now: NOW, ...view }), rows: true, open: () => { opened.push("agents"); return true; } })),
		paneSlot(host, "jobs", foldablePart(host, { render: (width, view) => renderJobsSection(WORK, theme, width, { now: NOW, ...view }), rows: true, open: () => { opened.push("jobs"); return true; } })),
		...Object.entries(extra).map(([key, part]) => paneSlot(host, key, part)),
	];
	const uninstall = installPane(host, theme, { placement: () => "auto", density: () => density, note() {} });
	const frame = () => {
		const laid = renderLayoutFrame(tui.layoutRoot!, columns, rows, () => {});
		(tui as unknown as { currentLayout: unknown }).currentLayout = laid;
		return laid.lines.map(stripAnsi);
	};
	frame();
	const input = (sequence: string) => { (tui as unknown as { handleViewportInput(data: string): unknown }).handleViewportInput(sequence); return frame(); };
	/** The pane's text, row by row; x is the pane text's first column. */
	const x = columns - PANE_COLUMNS + 3;
	// The text columns alone: the pane's scrollbar sits in its margin.
	const pane = (lines: string[]) => lines.map((line) => line.slice(x, x + PANE_TEXT).trimEnd());
	const at = (column: number, row: number) => `${column + 1};${row + 1}`;
	return {
		frame, input, pane, transcript, opened,
		wheel: (direction: "up" | "down", row: number, column = x + 2) => input(`\x1b[<${direction === "up" ? 64 : 65};${at(column, row)}M`),
		click: (row: number, column = x + 2) => { input(`\x1b[<0;${at(column, row)}M`); return input(`\x1b[<0;${at(column, row)}m`); },
		move: (row: number, column = x + 2) => input(`\x1b[<35;${at(column, row)}M`),
		dispose: () => { uninstall(); for (const dispose of disposers) dispose(); },
	};
}

const DESIGN_LDD = [
	"◆ add-invoice-export",
	"standard · strict TDD · risk medium",
	"next apply slice 3",
	"",
	"Slices  2/4 · tasks 9/14",
	spread("✓ 1 Invoice model and CSV writer", "4/4", W),
	spread("✓ 2 Export endpoint", "3/3", W),
	spread("◐ 3 Date-range filter", "2/4", W),
	"  ✓ parse from and to",
	"  ✓ default to the current month",
	"  ◐ reject an inverted range",
	"  · filter the invoice query",
	spread("· 4 Download button in billing", "0/3", W),
	"",
	"Last run",
	"✓ passed · affected · 6m ago",
	"  42 tests · 11.8s · 3f2a9c1",
];

test("compact, 01 at 42 columns: one blank row between Last run and Agents, and between Agents and Jobs", (t) => {
	const s = hosted(180, 34, "compact");
	t.after(s.dispose);
	assert.deepEqual(s.pane(s.frame()).slice(0, 25), [
		"",
		...DESIGN_LDD,
		"",
		"Agents  2 running",
		spread("◐ jhon-builder  slice 3", "48k · 4m12s", W),
		spread("◐ jhon-checker  slice 2 tests", "950 · 1m03s", W),
		"",
		"Jobs  1 running",
		spread("⧗ job-1 pytest tests/billing -k range", "18s", W),
	].map((row) => row.trimEnd()));
});

test("minimal stacks the sections; comfortable draws each in its own box, in the same pane", () => {
	const parts = new Map<string, PanePart>([
		["ldd", { render: (width) => renderLddPanel(LDD, plainTheme, width, { now: NOW }), invalidate() {} }],
		["agents", { render: (width) => renderAgentsSection(WORK, plainTheme, width, { now: NOW }), invalidate() {} }],
		["jobs", { render: (width) => renderJobsSection(WORK, plainTheme, width, { now: NOW }), invalidate() {} }],
	]);
	const minimal = paneRows(parts, "minimal", W).rows.map(stripAnsi);
	assert.deepEqual(minimal.slice(17, 19), ["  42 tests · 11.8s · 3f2a9c1", "Agents  2 running"], "minimal: no blank row");
	const box = (rows: string[]) => [`╭${"─".repeat(W - 2)}╮`, ...rows.map((row) => `│ ${padded(row, W - 4)} │`), `╰${"─".repeat(W - 2)}╯`];
	const inner = W - 4;
	assert.deepEqual(paneRows(parts, "comfortable", W).rows.map(stripAnsi), [
		"",
		...box([
			"◆ add-invoice-export",
			"standard · strict TDD · risk medium",
			"next apply slice 3",
			"",
			"Slices  2/4 · tasks 9/14",
			spread("✓ 1 Invoice model and CSV writer", "4/4", inner),
			spread("✓ 2 Export endpoint", "3/3", inner),
			spread("◐ 3 Date-range filter", "2/4", inner),
			"  ✓ parse from and to",
			"  ✓ default to the current month",
			"  ◐ reject an inverted range",
			"  · filter the invoice query",
			spread("· 4 Download button in billing", "0/3", inner),
			"",
			"Last run",
			"✓ passed · affected · 6m ago",
			"  42 tests · 11.8s · 3f2a9c1",
		]),
		...box([
			"Agents  2 running",
			spread("◐ jhon-builder  slice 3", "48k · 4m12s", inner),
			"◐ jhon-checker  slice 2 …  950 · 1m03s",
		]),
		...box([
			"Jobs  1 running",
			"⧗ job-1 pytest tests/billing -k …  18s",
		]),
	]);
	const roles: string[] = [];
	paneRows(parts, "comfortable", W, { theme: { ...plainTheme, fg: (role, text) => { if (/[╭│╰]/.test(text)) roles.push(role); return text; } } });
	assert.deepEqual([...new Set(roles)], ["border"], "the frame in rose ink's border role");
});

test("through pi: a click on a section's title folds it and a second unfolds it; a click on an agent row opens its view", (t) => {
	const s = hosted(180, 40, "compact");
	t.after(s.dispose);
	// Screen row 0 is the top row (none here, so the pane starts at 0): pane row 1 is LDD's title.
	const ldd = s.pane(s.frame()).indexOf("◆ add-invoice-export");
	assert.ok(ldd > 0);
	let rows = s.pane(s.click(ldd));
	assert.deepEqual(rows.slice(ldd, ldd + 4), [spread("◆ add-invoice-export", "▸", W), "next apply slice 3", "", "Agents  2 running"], "folded to its change and next step");
	rows = s.pane(s.click(ldd));
	assert.equal(rows[ldd + 1], "standard · strict TDD · risk medium", "a second click unfolds it");
	const title = rows.indexOf("Agents  2 running");
	rows = s.pane(s.click(title));
	assert.deepEqual(rows.slice(title, title + 3), [spread("Agents  2 running", "▸", W), spread("◐ jhon-builder  slice 3", "48k · 4m12s", W), ""]);
	s.click(title + 1);
	assert.deepEqual(s.opened, ["agents"], "a row opens /jhon:agents");
});

test("through pi: the pointer paints a section's title, then an agent's row, one at a time; off the controls, nothing", (t) => {
	const s = hosted(180, 40, "compact", tagged);
	t.after(s.dispose);
	const title = s.pane(s.frame()).indexOf("Agents  2 running");
	let rows = s.pane(s.move(title));
	// The tags take two cells: the fold glyph is cut off this row; the unit test above sees it.
	assert.match(rows[title]!, /^«Agents»  2 running\b/);
	rows = s.pane(s.move(title + 1));
	assert.doesNotMatch(rows[title]!, /«Agents»/, "the title let go");
	assert.match(rows[title + 1]!, /«jhon-builder»/);
	rows = s.pane(s.move(title - 1));
	assert.equal(rows.filter((row) => /«(Agents|jhon-|add-invoice|▾)/.test(row)).length, 0, "a blank row clears it");
	rows = s.pane(s.move(1));
	assert.match(rows[1]!, /^◆ «add-invoice-export»/, "LDD's title too");
});

test("through pi's ScrollView: the wheel over the pane scrolls the pane alone, the part under it first; the conversation never moves", (t) => {
	let todoWheels = 0;
	const todo: PanePart = {
		render: () => ["▾ TODOS", "  · a", "  · b"],
		invalidate() {},
		// The todo list takes two wheels (its own scroll), then leaves the rest to the pane.
		handleMouse: (event) => (event.type === "wheel" && todoWheels < 2 ? (todoWheels++, { handled: true, render: true }) : undefined),
	};
	const s = hosted(180, 16, "compact", plainTheme, { todo });
	t.after(s.dispose);
	const before = s.frame();
	const conversation = (lines: string[]) => lines.map((line) => line.slice(0, 180 - PANE_COLUMNS));
	assert.equal(s.pane(before)[1], "◆ add-invoice-export");
	let after = s.wheel("up", 2);
	assert.deepEqual(conversation(after), conversation(before), "at the pane's top a wheel up stops there: the conversation stays");
	const todoRow = s.pane(s.frame()).indexOf("▾ TODOS");
	assert.equal(todoRow, -1, "the todo part sits below the fold of a 16-row pane");
	after = s.wheel("down", 2);
	assert.notEqual(s.pane(after)[1], "◆ add-invoice-export", "a wheel down scrolls the pane");
	assert.deepEqual(conversation(after), conversation(before), "and only the pane");
	for (let i = 0; i < 30; i++) after = s.wheel("down", 5);
	assert.deepEqual(conversation(after), conversation(before), "past the pane's end too");
	const todoAt = s.pane(after).indexOf("▾ TODOS");
	assert.ok(todoAt > 0, "scrolled to the end, the todo part shows");
	const shown = s.pane(after);
	s.wheel("down", todoAt + 1);
	s.wheel("down", todoAt + 1);
	assert.equal(todoWheels, 2, "over the todo part the wheel goes to it first");
	assert.deepEqual(s.pane(s.frame()), shown, "and the pane did not move while it took them");
	assert.notDeepEqual(conversation(s.wheel("up", 1, 3)), conversation(before), "over the conversation the wheel is pi's own");
});

// ---------------------------------------------------------------- the bar

const BAR: BarModel = {
	cwd: "/home/dev/acme_app", branch: "feat/add-invoice-export", sandbox: "on",
	modelId: "opus-5-5", effort: "high", contextPercent: 38, contextWindow: 1_000_000, costTotal: 1.42, subscription: false,
};

test("the bar: the branch's ±N and other extensions' statuses; they give way before model · effort, the cost stays", () => {
	const model = { ...BAR, dirty: 3, statuses: ["MCP 2 servers", "\x1b[32mlsp ok\x1b[0m"] };
	assert.equal(stripAnsi(renderStatusBar(model, plainTheme, 180, { ascii: false, compactAt: null })),
		spread(" ⎇ feat/add-invoice-export ±3 │ sandbox on │ MCP 2 servers · lsp ok", "opus-5-5 · high │ ctx ▰▰▰▱▱▱▱▱ 38% │ $1.42 ", 180));
	const at100 = stripAnsi(renderStatusBar(model, plainTheme, 100, { ascii: false, compactAt: null }));
	assert.equal(at100, spread(" ⎇ feat/add-invoice-… ±3 │ sandbox on │ MCP 2 servers · lsp ok", "opus-5-5 · high │ ctx 38% │ $1.42 ", 100));
	const at80 = stripAnsi(renderStatusBar(model, plainTheme, 80, { ascii: false, compactAt: null }));
	assert.doesNotMatch(at80, /MCP/, "the statuses go first");
	assert.match(at80, /opus-5-5 · high/);
});

test("B's drop order: the profile gives way before the windows' meters and resets; the cost is the last thing left", () => {
	const model: BarModel = { ...BAR, profile: "jhon-nan", usage: [{ label: "week", percent: 23, resetAt: Date.parse("2026-11-26T14:00:00"), reset: true, meter: true }] };
	const facts: Array<[string, RegExp]> = [["profile", /jhon-nan/], ["resets", /resets/], ["week meter", /week ▰/]];
	const gone = (width: number) => facts.map(([, fact]) => !fact.test(stripAnsi(renderStatusBar(model, plainTheme, width, { ascii: false, compactAt: null, narrow: false }))));
	const firstGone = facts.map((_, i) => { for (let width = 240; width > 10; width--) if (gone(width)[i]) return width; return 0; });
	assert.ok(firstGone[0]! > firstGone[1]! && firstGone[0]! > firstGone[2]!, `the profile first: ${JSON.stringify(firstGone)}`);
	assert.match(stripAnsi(renderStatusBar(model, plainTheme, 20, { ascii: false, compactAt: null, narrow: false })), /\$1\.42/);
});

test("through extensions/ui.ts: pi's other statuses and the files the session's writes changed reach the status bar", async (t) => {
	useMemorySettings(t, {});
	const handlers = new Map<string, Array<(event: unknown, ctx: ExtensionContext) => unknown>>();
	const pi = {
		on(event: string, handler: never) { handlers.set(event, [...(handlers.get(event) ?? []), handler]); },
		appendEntry() {}, registerTool() {}, registerMessageRenderer() {}, sendMessage() {}, registerCommand() {}, registerShortcut() {},
		events: { on() { return () => {}; }, emit() {} },
		getThinkingLevel: () => "high",
	} as unknown as ExtensionAPI;
	jhonShellUi(pi, { JHON_SHELL: "1" }, { lddStatus: async () => undefined, now: () => NOW });
	const change = (id: string, path: string) => ({ type: "custom", customType: SESSION_CHANGE_ENTRY, data: { sessionId: "bar-session", evidence: { id, root: "/repo", path, before: { kind: "text", text: "a\n" }, after: { kind: "text", text: "b\n" } } } });
	let footer: ((tui: unknown, theme: unknown, data: unknown) => { render(width: number): string[] }) | undefined;
	const ctx = {
		hasUI: true, mode: "tui", cwd: "/repo",
		model: { id: "opus-5-5", provider: "anthropic", reasoning: true, contextWindow: 200_000 },
		hasPendingMessages: () => false, isIdle: () => true, abort() {},
		sessionManager: { getCwd: () => "/repo", getEntries: () => [change("w1", "a.ts"), change("w2", "b.ts"), change("w3", "a.ts")], getBranch: () => [], getSessionId: () => "bar-session" },
		modelRegistry: { isUsingOAuth: () => false, getApiKeyForProvider: async () => undefined, getAll: () => [] },
		getContextUsage: () => ({ tokens: 20_000, contextWindow: 200_000, percent: 10 }),
		ui: { theme: plainTheme, setFooter(factory: never) { footer = factory; }, setWidget() {}, setEditorComponent() {}, getEditorComponent: () => undefined, setWorkingVisible() {}, notify() {} },
	} as unknown as ExtensionContext;
	for (const handler of handlers.get("session_start") ?? []) await handler({}, ctx);
	const data = { getGitBranch: () => "main", getExtensionStatuses: () => new Map([["pi-mcp-adapter", "MCP 2 servers"]]), getAvailableProviderCount: () => 1, onBranchChange: () => () => {} };
	const row = stripAnsi(footer!({ requestRender() {} }, plainTheme, data).render(180)[0]!);
	assert.match(row, /^ ⎇ main ±2 │ MCP 2 servers +opus-5-5/, row);
	assert.equal(visibleWidth(row), 180);
});
