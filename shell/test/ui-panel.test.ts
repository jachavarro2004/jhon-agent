/**
 * The right pane's content (T174-S5), drawn to design/shell-tui 01 (the LDD
 * section, then Agents and Jobs while they run), 02 (idle: no Agents, no
 * Jobs) and 04 (the narrow form: the Running block above the prompt). Row
 * snapshots through test/rows.ts: the panel at its 42 columns, the Running
 * block at 100.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-panel.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { renderAgentsSection, renderJobsSection, renderLddPanel, renderRunningBlock, elapsed, shellEnabled, shellJobsCount, type RunningJobDisplay } from "../lib/ui/panel.ts";
import { lddFromStatus, type LddStatus } from "../lib/ui/ldd-status.ts";
import { PANE_TEXT } from "../lib/ui/pane.ts";
import { padded, plainTheme, rowsAt, spread } from "./rows.ts";

// The design's story (design/shell-tui/.src/parts.py): acme_app's add-invoice-export, mid-slice 3.
const NOW = Date.parse("2026-11-20T12:00:00Z");
const W = PANE_TEXT;
const task = (text: string, done: boolean) => ({ text, done });
const RUNNING: LddStatus = {
	change: "add-invoice-export",
	slice: "Slice 3: Date-range filter",
	slices: ["done", "done", "current", "pending"],
	detail: [
		{ name: "Slice 1: Invoice model and CSV writer", total: 4, completed: 4 },
		{ name: "Slice 2: Export endpoint", total: 3, completed: 3 },
		{ name: "Slice 3: Date-range filter", total: 4, completed: 2, tasks: [task("parse from and to", true), task("default to the current month", true), task("reject an inverted range", false), task("filter the invoice query", false)] },
		{ name: "Slice 4: Download button in billing", total: 3, completed: 0 },
	],
	tasks: { completed: 9, total: 14 },
	strictTdd: true,
	route: "standard",
	risk: "medium",
	next: "apply",
	tests: { passed: true, failed: 0, at: new Date(NOW - 6 * 60_000).toISOString(), scope: "affected", count: 42, seconds: 11.8, commit: "3f2a9c1" },
};
const IDLE: LddStatus = {
	...RUNNING,
	slice: "Slice 4: Download button in billing",
	slices: ["done", "done", "done", "current"],
	detail: [
		RUNNING.detail![0]!, RUNNING.detail![1]!,
		{ name: "Slice 3: Date-range filter", total: 4, completed: 4 },
		{ name: "Slice 4: Download button in billing", total: 3, completed: 0, tasks: [task("add the button to the billing page", false), task("stream the CSV download", false), task("disable it on an empty range", false)] },
	],
	tasks: { completed: 11, total: 14 },
	tests: { passed: true, failed: 0, at: new Date(NOW - 60_000).toISOString(), scope: "affected", count: 48, seconds: 12.9, commit: "7c41d0e" },
};
const WORK: RunningJobDisplay[] = [
	{ id: "agent-1", kind: "agent", label: "jhon-builder: slice 3", startedAt: NOW - 252_000 },
	{ id: "agent-2", kind: "agent", label: "jhon-checker: slice 2 tests", startedAt: NOW - 63_000 },
	{ id: "job-1", kind: "command", label: "pytest tests/billing -k range", startedAt: NOW - 18_000 },
];

const panel = (ldd: LddStatus | undefined, width = W, ascii = false) => rowsAt((w, theme) => renderLddPanel(ldd, theme, w, { now: NOW, ascii }), width);

test("01: the change, its route, the next step; the slices with the open one's tasks under it; the last run", () => {
	assert.deepEqual(panel(RUNNING), [
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
	]);
});

test("02, idle: the open slice not started yet is pending, its tasks all pending; the last run a minute ago", () => {
	assert.deepEqual(panel(IDLE), [
		"◆ add-invoice-export",
		"standard · strict TDD · risk medium",
		"next apply slice 4",
		"",
		"Slices  3/4 · tasks 11/14",
		spread("✓ 1 Invoice model and CSV writer", "4/4", W),
		spread("✓ 2 Export endpoint", "3/3", W),
		spread("✓ 3 Date-range filter", "4/4", W),
		spread("· 4 Download button in billing", "0/3", W),
		"  · add the button to the billing page",
		"  · stream the CSV download",
		"  · disable it on an empty range",
		"",
		"Last run",
		"✓ passed · affected · 1m ago",
		"  48 tests · 12.9s · 7c41d0e",
	]);
	// Nothing runs: Agents and Jobs draw nothing, so the pane shows neither (02).
	assert.deepEqual(rowsAt((w, theme) => renderAgentsSection([], theme, w, { now: NOW }), W), []);
	assert.deepEqual(rowsAt((w, theme) => renderJobsSection(undefined, theme, w, { now: NOW }), W), []);
});

test("no change: the section says so in one dim line", () => {
	assert.deepEqual(panel(undefined), ["No active LDD change"]);
	const dim: string[] = [];
	renderLddPanel(undefined, { fg: (role, text) => { dim.push(role); return text; }, bold: (text) => text }, W, { now: NOW });
	assert.deepEqual(dim, ["dim"]);
});

test("what jhon status does not say is left out: no risk, no count, no time; a failed run counts its failures", () => {
	const plain: LddStatus = { change: "x", slices: ["current"], detail: [{ name: "Slice 1: a", total: 2, completed: 0 }], route: "verified", next: "apply", tests: { passed: false, failed: 3, at: new Date(NOW - 2 * 3_600_000).toISOString(), commit: "0123456789abcdef" } };
	assert.deepEqual(panel(plain), [
		"◆ x",
		"verified",
		"next apply slice 1 → verify",
		"",
		"Slices  0/1",
		spread("· 1 a", "0/2", W),
		"",
		"Last run",
		"✗ 3 failed · 2h ago",
		"  0123456",
	]);
	assert.deepEqual(panel({ change: "x", slices: [] }), ["◆ x", "", "Last run", "· none yet"], "no slices, no route, no step, no run");
});

test("jhon status's risk and the run's commit reach the panel (route.tier, tests.commit)", () => {
	const status = lddFromStatus("x", { route: { level: "standard", tier: "high" }, tasks: { slices: [] }, tests: { timestamp: "2026-11-20T11:54:00Z", passed: true, failed: [], scope: "full", commit: "3f2a9c1d00" } });
	assert.equal(status?.risk, "high");
	assert.equal(status?.tests?.commit, "3f2a9c1d00");
	assert.deepEqual(panel(status).slice(1, 2), ["standard · risk high"]);
});

test("the panel's rows fit 42 columns: a long name and a long task are cut, the counts stay at the right edge", () => {
	const long: LddStatus = { ...RUNNING, change: "a-change-whose-name-runs-past-the-panel-width", worktree: "wt-billing", detail: RUNNING.detail!.map((d, i) => i === 2 ? { ...d, name: "Slice 3: a date-range filter that reads every invoice of the month", tasks: [task("reject an inverted range and say which date is wrong", false)] } : d) };
	const rows = panel(long);
	assert.equal(rows[0], spread("◆ a-change-whose-name-runs-p…", "⎇ wt-billing", W), "the worktree at the right when it is not the session's folder");
	assert.equal(rows[7], spread("◐ 3 a date-range filter that reads …", "2/4", W));
	assert.equal(rows[8], "  ◐ reject an inverted range and say whic…", "the slice has begun: its first open task is the one in progress");
	for (const row of rows) assert.ok(row.length <= W, row);
});

test("the other worktrees' changes follow the last run, four at most, an umbrella marked", () => {
	const lanes = [
		{ change: "add-pdf-export", worktree: "wt-pdf", done: 1, slices: 3, next: "apply" },
		{ change: "split-billing", worktree: "acme_app", done: 0, slices: 0, next: "start-change", umbrella: true as const },
	];
	assert.deepEqual(panel({ ...RUNNING, lanes }).slice(17), [
		"",
		"Other changes  2",
		spread("◆ add-pdf-export  ⎇ wt-pdf", "1/3", W),
		spread("○ split-billing  ⎇ acme_app", "umbrella", W),
	]);
});

const agents = (jobs: RunningJobDisplay[] | undefined, width = W) => rowsAt((w, theme) => renderAgentsSection(jobs, theme, w, { now: NOW }), width);
const jobs = (list: RunningJobDisplay[] | undefined, width = W) => rowsAt((w, theme) => renderJobsSection(list, theme, w, { now: NOW }), width);

test("01: Agents and Jobs, each its own section while something runs: glyph, name, what it does, the elapsed time right-aligned", () => {
	assert.deepEqual(agents(WORK), [
		"Agents  2 running",
		spread("◐ jhon-builder  slice 3", "4m12s", W),
		spread("◐ jhon-checker  slice 2 tests", "1m03s", W),
	]);
	assert.deepEqual(jobs(WORK), [
		"Jobs  1 running",
		spread("⧗ job-1 pytest tests/billing -k range", "18s", W),
	]);
});

test("the name column fits the longest agent name running, never jhon-buil… (L1475)", () => {
	const mixed: RunningJobDisplay[] = [
		{ id: "agent-3", kind: "agent", label: "jhon-scout: map the billing module", startedAt: NOW - 5_000 },
		{ id: "agent-1", kind: "agent", label: "jhon-builder: slice 3 date-range filter and its tests", startedAt: NOW - 3_725_000 },
	];
	assert.deepEqual(agents(mixed), [
		"Agents  2 running",
		spread("◐ jhon-builder  slice 3 date-range…", "1h02m", W),
		spread("◐ jhon-scout    map the billing module", "5s", W),
	], "in start order; the task is cut, the name and the time never");
	assert.deepEqual(agents([{ id: "agent-9", kind: "agent", label: "jhon-checker: x", startedAt: NOW - 1_000 }], 30), ["Agents  1 running", spread("◐ jhon-checker  x", "1s", 30)]);
});

test("finished work leaves the sections a minute after it ended (T174-S5b: it stays that minute); a monitor is a job", () => {
	const finished: RunningJobDisplay[] = [
		{ id: "job-2", kind: "command", label: "npm test", startedAt: NOW - 30_000, endedAt: NOW - 60_000, status: "completed", exitCode: 0 },
		{ id: "agent-4", kind: "agent", label: "jhon-checker: slice 2", startedAt: NOW - 30_000, endedAt: NOW - 60_000, status: "failed" },
	];
	assert.deepEqual(agents(finished), []);
	assert.deepEqual(jobs(finished), []);
	assert.deepEqual(jobs([...finished, { id: "job-3", kind: "monitor", label: "tail -f log/app.log", startedAt: NOW - 61_000 }]), ["Jobs  1 running", spread("⧗ job-3 tail -f log/app.log", "1m01s", W)], "a monitor is a job");
});

test("04 at 100 columns: the Running block above the prompt, a labelled rule, then one row per agent and job", () => {
	const block = renderRunningBlock(WORK, plainTheme, 100, { now: NOW });
	assert.deepEqual(rowsAt(() => block.rows, 100), [
		`── Running  2 agents · 1 job ${"─".repeat(71)}`,
		padded(spread("  ◐ jhon-builder  slice 3", "4m12s", 66), 100),
		padded(spread("  ◐ jhon-checker  slice 2 tests", "1m03s", 66), 100),
		padded(spread("  ⧗ job-1 pytest tests/billing -k range", "18s", 66), 100),
		padded("", 100),
	]);
	assert.deepEqual(block.opens, ["agents", "agents", "agents", "jobs", undefined], "a click opens the agents view or the jobs view");
	assert.deepEqual(renderRunningBlock([], plainTheme, 100, { now: NOW }), { rows: [], opens: [] }, "nothing runs: no block");
	const one = renderRunningBlock([WORK[2]!], plainTheme, 60, { now: NOW });
	assert.deepEqual(rowsAt(() => one.rows, 60), [`── Running  1 job ${"─".repeat(42)}`, padded(spread("  ⧗ job-1 pytest tests/billing -k range", "18s", 58), 60), padded("", 60)]);
	assert.deepEqual(one.opens, ["jobs", "jobs", undefined]);
});

test("ASCII: every glyph in its one-cell form, the widths unchanged", () => {
	const rows = panel(RUNNING, W, true);
	assert.equal(rows[0], "# add-invoice-export");
	assert.equal(rows[1], "standard - strict TDD - risk medium");
	assert.equal(rows[7], spread("~ 3 Date-range filter", "2/4", W));
	assert.equal(rows[11], "  . filter the invoice query");
	assert.equal(rows[15], "+ passed - affected - 6m ago");
	assert.deepEqual(rowsAt((w, theme) => renderJobsSection(WORK, theme, w, { now: NOW, ascii: true }), W)[1], spread("~ job-1 pytest tests/billing -k range", "18s", W));
	assert.equal(rowsAt(() => renderRunningBlock(WORK, plainTheme, 100, { now: NOW, ascii: true }).rows, 100)[0], `-- Running  2 agents - 1 job ${"-".repeat(71)}`);
});

test("the elapsed time: seconds, then minutes and seconds, then hours and minutes", () => {
	assert.deepEqual([0, 18_000, 63_000, 252_000, 3_725_000].map(elapsed), ["0s", "18s", "1m03s", "4m12s", "1h02m"]);
});

test("shellEnabled: off in a jhon child session and with JHON_SHELL off; the jobs count for the footer", () => {
	assert.equal(shellEnabled({}), true);
	assert.equal(shellEnabled({ JHON_SHELL: "1" }), true);
	for (const off of ["0", "false", "OFF"]) assert.equal(shellEnabled({ JHON_SHELL: off }), false, off);
	assert.equal(shellEnabled({ JHON_PI_CHILD: "1" }), false);
	assert.deepEqual([shellJobsCount(1), shellJobsCount(3)], ["⧗ 1 job", "⧗ 3 jobs"]);
});
