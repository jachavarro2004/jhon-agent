/**
 * shell.appearance.ascii reaches every chrome piece forked from the Gentle
 * Shell (T15.12g, inventory row 79): the prompt rule and the card prompt, the
 * title row (力 reads jhon), the status bar and the regular bar with their
 * separators and meters, the LDD rows, the TODOS, JOBS and AGENTS sections
 * and a notice's title draw nothing outside 0x20-0x7e, in the
 * forms design/system.md's glyph table gives, at 160 and 100 columns.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-ascii-chrome.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test, { type TestContext } from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { renderShellAgentsSection, renderShellBar, renderShellChromeBar, renderShellJobsSection, renderShellStatusBar, renderShellStatusSections, renderShellTitleRow, renderShellTitleRows, type ShellBarModel } from "../lib/ui/shell-bar.ts";
import { CARD_STYLE, renderCard, setCardStyle, cardStyle, CARD_TONE } from "../lib/ui/primitives/index.ts";
import { framePromptLines, PROMPT_STATE, resolvePromptLayout } from "../lib/ui/shell-prompt.ts";
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
const model = (overrides: Partial<ShellBarModel> = {}): ShellBarModel => ({
	cwd: "/home/owner/code/jhon", branch: "feat/shell-gentle", sessionName: "rail work", modelId: "claude-opus-5-5", effort: "high", profile: "jhon",
	contextPercent: 85, contextWindow: 1_000_000, costTotal: 1.25, subscription: true, mode: "yolo", jobs,
	ldd: {
		change: "v0-1-release", slice: "Slice 2: launch", slices: ["done", "current", "pending"], route: "verified", next: "apply",
		tests: { passed: false, failed: 2, at: new Date(NOW - 60_000).toISOString() },
		lanes: [{ change: "fix-infra", worktree: "infra", done: 2, slices: 5, next: "verify" }],
	},
	...overrides,
});
const todos = { tasks: [
	{ id: 1, title: "Read the rail", status: "done" },
	{ id: 2, title: "Cards → ASCII", status: "in_progress", note: "the todo rows · its notes" },
	{ id: 3, title: "Blocked one", status: "blocked", note: "waiting" },
	{ id: 4, title: "Later", status: "pending" },
	{ id: 5, title: "Gone", status: "dropped" },
], nextId: 6, updatedTurn: 1 } as TodoState;

test("the prompt rule, the neon card prompt and the float card prompt draw in ASCII, idle and working", (t) => {
	ascii(t);
	const previous = cardStyle();
	t.after(() => setCardStyle(previous));
	for (const width of WIDTHS) {
		const lines = ["─".repeat(width), "hello → there", "─".repeat(width)];
		for (const state of [PROMPT_STATE.IDLE, PROMPT_STATE.WORKING, PROMPT_STATE.QUEUED]) {
			const options = { state, tick: 1, borderColor: (s: string) => s, fg: plain.fg, bold: plain.bold, bg: plain.bg };
			const panel = framePromptLines(lines, width, options, resolvePromptLayout(width, undefined, undefined, true));
			pureAscii(`prompt rule ${state} ${width}`, panel.slice(0, 1));
			assert.match(stripAnsi(panel[1]!), /^> hello/, "the mark reads >");
			assert.match(stripAnsi(panel[0]!), /^- \* /, "the petal reads * (L165)");
			const neon = framePromptLines(lines, width, { ...options, style: CARD_STYLE.NEON });
			pureAscii(`neon prompt chrome ${state} ${width}`, [neon[0]!, neon.at(-1)!, stripAnsi(neon[1]!).replace("hello → there", "")]);
			const float = framePromptLines(lines, width, { ...options, style: CARD_STYLE.FLOAT }, resolvePromptLayout(width, CARD_STYLE.FLOAT, plain.bg));
			pureAscii(`float prompt chrome ${state} ${width}`, [float[0]!, float.at(-1)!, stripAnsi(float[1]!).replace("hello → there", "")]);
		}
	}
});

test("the title row and the title rows draw in ASCII, the brand 力 as jhon, each still exactly the width", (t) => {
	ascii(t);
	for (const width of WIDTHS) {
		const row = renderShellTitleRow(model(), plain, width, "/home/owner");
		pureAscii(`title row ${width}`, [row]);
		assert.equal(visibleWidth(row), width);
		assert.equal(stripAnsi(row).slice(0, 20), " jhon  ~/code/jhon  ", "no brand before the name: never jhon jhon (L165)");
		pureAscii(`title row with the model ${width}`, [renderShellTitleRow(model(), plain, width, "/home/owner", { model: true })]);
		pureAscii(`title rows ${width}`, renderShellTitleRows(model(), plain, width, "/home/owner"));
		pureAscii(`narrow title row ${width}`, [renderShellTitleRow(model(), plain, 40, "/home/owner", { model: true })]);
	}
});

test("the status bar, the chrome bar and the regular bar draw their separators and meters in ASCII", (t) => {
	ascii(t);
	for (const width of WIDTHS) {
		const bar = renderShellStatusBar(model(), plain, width, { jobs: true });
		pureAscii(`status bar ${width}`, bar.rows);
		assert.match(stripAnsi(bar.rows[0]!), / \| /, "the divider reads |");
		assert.match(stripAnsi(renderShellStatusBar(model(), plain, 200, { jobs: true }).rows[0]!), /ctx #+-* 85%/, "the meter reads # and -");
		pureAscii(`chrome bar ${width}`, renderShellChromeBar(model(), plain, width).rows);
		pureAscii(`narrow status bar ${width}`, renderShellStatusBar(model(), plain, 60, { jobs: true, modelDetails: false }).rows);
		const regular = renderShellBar(model({ cwd: "~/code/jhon" }), plain, width);
		pureAscii(`regular bar ${width}`, regular);
		assert.match(stripAnsi(regular[0]!), /^jhon \| \S/, "the brand reads jhon, ⟡ reads |");
	}
});

test("the Status sections and the LDD rows draw in ASCII", (t) => {
	ascii(t);
	for (const width of [46, 80, 100, 160]) pureAscii(`status sections ${width}`, renderShellStatusSections(model(), plain, width));
});

test("TODOS, JOBS and AGENTS draw their glyphs in ASCII, JOBS and AGENTS folded too", (t) => {
	ascii(t);
	for (const width of [46, 80, 100, 160]) {
		for (const folded of [false, true]) {
			if (!folded) pureAscii(`TODOS ${width}`, renderTodoRail(todos, plain, width, true));
			pureAscii(`JOBS ${width} ${folded}`, renderShellJobsSection(jobs, plain, width, NOW, folded));
			pureAscii(`AGENTS ${width} ${folded}`, renderShellAgentsSection(jobs, plain, width, NOW, folded));
		}
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
