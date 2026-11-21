/**
 * shell.appearance.ascii reaches every chrome piece forked from the Gentle
 * Shell (T15.12g, inventory row 79): the prompt rule and the card prompt, the
 * top row and the status bar (T174-S3) with their dividers and meter, the right pane's LDD, Agents and Jobs sections and
 * the narrow form's Running block (T174-S5), the TODOS section
 * and a notice's title draw nothing outside 0x20-0x7e, in the
 * forms design/system.md's glyph table gives, at 160 and 100 columns.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-ascii-chrome.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test, { type TestContext } from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { renderAgentsSection, renderJobsSection, renderLddPanel, renderRunningBlock } from "../lib/ui/panel.ts";
import type { LddStatus } from "../lib/ui/ldd-status.ts";
import { renderStatusBar, renderTopRow, type BarModel } from "../lib/ui/bars.ts";
import { CARD_STYLE, renderCard, setCardStyle, cardStyle, CARD_TONE } from "../lib/ui/primitives/index.ts";
import { framePrompt, PROMPT_STATE, renderWorkingLine } from "../lib/ui/shell-prompt.ts";
import { renderTodoRail } from "../extensions/todo.ts";
import type { TodoState } from "../lib/todo.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { useMemorySettings } from "./fake-jhon-settings.ts";

const plain = { fg: (_r: string, t: string) => t, bold: (t: string) => t, bg: (_r: string, t: string) => `\x1b[48;5;236m${t}\x1b[49m`, strikethrough: (t: string) => t };
const NOW = 1_788_600_000_000;
const WIDTHS = [160, 100, 80];

function pureAscii(name: string, lines: string[]): void {
	assert.ok(lines.length > 0, `${name} drew nothing`);
	const bad = lines.map(stripAnsi).filter((line) => !/^[\x20-\x7e]*$/.test(line));
	assert.deepEqual(bad, [], `${name} leaks non-ASCII:\n${lines.map(stripAnsi).join("\n")}`);
}

function ascii(t: TestContext): void {
	useMemorySettings(t, { "shell.appearance.ascii": true });
}

const jobs = [
	{ id: "job-1", label: "npm test", startedAt: NOW - 65_000 },
	{ id: "job-2", label: "lint", startedAt: NOW - 90_000, endedAt: NOW - 5_000, status: "exited" as const, exitCode: 0 },
	{ id: "job-3", label: "deploy", startedAt: NOW - 90_000, endedAt: NOW - 6_000, status: "failed" as const },
	{ id: "ag-1", kind: "agent" as const, label: "explore: map the rail", startedAt: NOW - 95_000, tokens: 48_200 },
	{ id: "ag-2", kind: "agent" as const, label: "verify: the slice", startedAt: NOW - 95_000, endedAt: NOW - 2_000, status: "failed" as const },
	{ id: "ag-3", kind: "agent" as const, label: "explore: docs", startedAt: NOW - 95_000, endedAt: NOW - 3_000, status: "completed" as const },
	{ id: "ag-4", kind: "agent" as const, label: "explore: stop", startedAt: NOW - 95_000, endedAt: NOW - 4_000, status: "stopped" as const },
];
const ldd: LddStatus = {
	change: "v0-1-release", slice: "Slice 2: launch", slices: ["done", "current", "pending"], route: "verified", risk: "high", strictTdd: true, next: "apply", worktree: "rail",
	detail: [{ name: "Slice 1: read", total: 1, completed: 1 }, { name: "Slice 2: launch", total: 2, completed: 1, tasks: [{ text: "draw", done: true }, { text: "wire", done: false }] }, { name: "Slice 3: docs", total: 1, completed: 0 }],
	tests: { passed: false, failed: 2, at: new Date(NOW - 60_000).toISOString(), scope: "affected", commit: "3f2a9c1" },
	lanes: [{ change: "fix-infra", worktree: "infra", done: 2, slices: 5, next: "verify" }, { change: "split", worktree: "main", done: 0, slices: 0, umbrella: true }],
};
const bars: BarModel = {
	cwd: "/home/owner/code/jhon", branch: "feat/shell-gentle", sandbox: "on", modelId: "claude-opus-5-5", effort: "high", profile: "jhon",
	contextPercent: 85, contextWindow: 1_000_000, costTotal: 1.25, subscription: true, mode: "yolo",
};
const todos = { tasks: [
	{ id: 1, title: "Read the rail", status: "done" },
	{ id: 2, title: "Cards → ASCII", status: "in_progress", note: "the todo rows · its notes" },
	{ id: 3, title: "Blocked one", status: "blocked", note: "waiting" },
	{ id: 4, title: "Later", status: "pending" },
	{ id: 5, title: "Gone", status: "dropped" },
], nextId: 6, updatedTurn: 1 } as TodoState;

test("the working line, the prompt's rules and its mark draw in ASCII, idle, working and queued (T174-S8)", (t) => {
	ascii(t);
	for (const width of WIDTHS) {
		for (const state of [PROMPT_STATE.IDLE, PROMPT_STATE.WORKING, PROMPT_STATE.QUEUED]) {
			for (const wait of [undefined, "waiting for Claude API · 72s"]) {
				const line = renderWorkingLine(width, { state, tick: 1, animate: true, elapsedMs: 65_000, activity: "thinking", wait, doubleEsc: true, ascii: true }, plain);
				pureAscii(`working line ${state} ${width}`, [line]);
				assert.match(stripAnsi(line), /^ \* /, "the petal reads * (L165)");
				assert.equal(visibleWidth(line), width);
			}
		}
		const inner = width - 3;
		const rows = framePrompt(width, { rows: [`─── ↑ 2 more ${"─".repeat(inner - 13)}`, "hello → there", `─── ↓ 1 more ${"─".repeat(inner - 13)}`], inputRows: 1, border: (s) => s, theme: plain, ascii: true });
		pureAscii(`prompt rules ${width}`, [rows[0]!, rows.at(-1)!]);
		assert.match(stripAnsi(rows[1]!), /^ > hello/, "the mark reads >");
		assert.match(stripAnsi(rows[0]!), /-+ \^ 2 more -$/);
	}
});

test("the top row draws in ASCII without the brand mark, exactly the width (L165: never jhon jhon)", (t) => {
	ascii(t);
	for (const width of WIDTHS) {
		const row = renderTopRow(bars, plain, width, { home: "/home/owner" });
		pureAscii(`top row ${width}`, [row]);
		assert.equal(visibleWidth(row), width);
		assert.equal(stripAnsi(row).slice(0, 20), " jhon  ~/code/jhon  ", "no brand before the name");
		pureAscii(`narrow top row ${width}`, [renderTopRow(bars, plain, 12, { home: "/home/owner" })]);
	}
});

test("the status bar draws its dividers, branch mark and meter in ASCII, wide and in the narrow form", (t) => {
	ascii(t);
	for (const width of WIDTHS) {
		const row = renderStatusBar(bars, plain, width);
		pureAscii(`status bar ${width}`, [row]);
		assert.equal(visibleWidth(row), width);
		assert.match(stripAnsi(row), /^ @ feat\/sh\S+ \| sandbox on \| YOLO /, "⎇ reads @, │ reads |");
		pureAscii(`status bar at 30, ${width}`, [renderStatusBar(bars, plain, 30)]);
	}
	assert.match(stripAnsi(renderStatusBar(bars, plain, 200)), /ctx #+-* 85%/, "the meter reads # and -");
});

test("the right pane's LDD section draws in ASCII, with a change and without one", () => {
	for (const width of [42, 80, 100, 160]) {
		pureAscii(`LDD ${width}`, renderLddPanel(ldd, plain, width, { now: NOW, ascii: true }));
		pureAscii(`no change ${width}`, renderLddPanel(undefined, plain, width, { now: NOW, ascii: true }));
	}
});

test("TODOS, Agents, Jobs and the narrow form's Running block draw their glyphs in ASCII", (t) => {
	ascii(t);
	for (const width of [42, 80, 100, 160]) {
		pureAscii(`TODOS ${width}`, renderTodoRail(todos, plain, width, true));
		pureAscii(`Jobs ${width}`, renderJobsSection(jobs, plain, width, { now: NOW, ascii: true }));
		pureAscii(`Agents ${width}`, renderAgentsSection(jobs, plain, width, { now: NOW, ascii: true }));
		pureAscii(`Running ${width}`, renderRunningBlock(jobs, plain, width, { now: NOW, ascii: true }).rows);
	}
});

test("a notice's title draws in ASCII in both card styles", (t) => {
	ascii(t);
	const previous = cardStyle();
	t.after(() => setCardStyle(previous));
	for (const style of [CARD_STYLE.FLOAT, CARD_STYLE.NEON]) {
		setCardStyle(style);
		for (const width of WIDTHS) {
			pureAscii(`notice ${style} ${width}`, renderCard({ title: "Settings", body: ["saved"], tone: CARD_TONE.INFO }, plain, width, { expanded: true }));
		}
	}
});
