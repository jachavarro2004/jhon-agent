/**
 * The Agents view (`/jhon:agents`, alt+s) and the Jobs view (`/jhon:jobs`,
 * alt+o), drawn as design/shell-tui 09 (T174-S10): a rounded card titled
 * `Agents  this session · N running · M finished … esc`, the Running and
 * Finished sections on the left (a running row and its second line, a
 * finished row on one line, the time at the right), the chosen task's thread
 * on the right (its header, then one line per tool: icon, verb, target and its
 * state at the right), and the key hints under it. Every option the G view
 * had stays (the T174-S10 inventory): j/k and the arrows, tab and enter, F,
 * ctrl+j/k and PgUp/PgDn, f, s (c, x), a, o, esc, q, the mouse.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-tasks-view.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import type { AgentRun, JobRecord, TaskRecord, ThreadItem } from "../lib/background-tasks.ts";
import { TasksView, formatViewTime } from "../lib/ui/tasks-view.ts";
import { stripAnsi } from "../lib/ui/primitives/index.ts";
import { assertRows, padded, plainTheme, spread, type SnapshotWidth } from "./rows.ts";

const NOW = 10_000_000;

function agent(overrides: Partial<AgentRun> & Pick<AgentRun, "id">): AgentRun {
	return { kind: "agent", label: "x", agent: "worker", task: "fix it", ownerSessionId: "s1", startedAt: NOW - 125_000, status: "running", activity: [], tokens: 0, cost: 0, thread: [], ...overrides };
}
/** A running command of session s1 named after its id, started 125 s ago; `fields` change any of it. */
const job = (fields: Partial<JobRecord> & { id: string }): JobRecord => {
	const { id } = fields;
	return { kind: "command", label: id, command: `run ${id}`, cwd: "/repo", ownerSessionId: "s1", outputPath: `/tmp/jobs/${id}.log`, startedAt: NOW - 125_000, status: "running", tail: [], ...fields };
};
const tool = (name: string, subject: string | undefined, state: "done" | "error" | "running" = "done", output = ""): ThreadItem =>
	({ kind: "tool", callId: `${name}-${subject}`, name, ...(subject ? { subject } : {}), output, running: state === "running", isError: state === "error" });

// Artboard 09's scene, split into the two views Jhon keeps (T92, L657).
const builder = agent({
	id: "agent-1", agent: "jhon-builder", model: "claude-opus-5-5", startedAt: NOW - 252_000, tokens: 48_200, cost: 0.42,
	task: "slice 3 Date-range filter\nwriter · surfaces billing/api/filters.py, tests/billing/", childSessionId: "c1", sessionDir: "/s/subagents",
	thread: [
		tool("read", "odd/changes/add-invoice-export/tasks.md"),
		tool("read", "billing/api/filters.py"),
		tool("edit", "tests/billing/test_range.py"),
		tool("bash", "pytest tests/billing/test_range.py", "error", "3 failed"),
		tool("edit", "billing/api/filters.py"),
		tool("bash", "pytest tests/billing/test_range.py", "done", "3 passed"),
		{ kind: "text", text: "Parsing and the month default pass.\nNext: the inverted range answers 422." },
		tool("edit", "tests/billing/test_range.py"),
		tool("bash", "pytest tests/billing/test_range.py -k inverted", "running"),
	],
});
const checker = agent({ id: "agent-2", agent: "jhon-checker", task: "slice 2 tests", startedAt: NOW - 63_000 });
const scout = agent({ id: "agent-3", agent: "jhon-scout", task: "map billing\nmore", startedAt: NOW - 600_000, endedAt: NOW - 470_000, status: "completed", result: "export.py builds rows" });
const failed = agent({ id: "agent-4", agent: "jhon-builder", task: "slice 2", startedAt: NOW - 968_000, endedAt: NOW - 500_000, status: "failed", result: "the child exited 1" });
const elsewhere = agent({ id: "agent-5", agent: "jhon-explore", task: "map the repo", ownerSessionId: "s0", startedAt: NOW - 30_000 });
const pytest = job({ id: "job-3", label: "range tests", command: "pytest -k range", startedAt: NOW - 18_000, tail: ["collected 3 items", "tests/billing/test_range.py ..F"] });
const web = job({ id: "job-2", label: "pytest tests/web", command: "pytest tests/web", startedAt: NOW - 200_000, endedAt: NOW - 159_000, status: "exited", exitCode: 1, tail: ["1 failed"] });
const ALL: TaskRecord[] = [failed, scout, builder, checker, elsewhere, pytest, web];

interface Harness { view: TasksView; stopped: string[]; opened: string[]; closed: () => number; renders: () => number; screen(width?: number): string[] }

function harness(view: "agents" | "jobs", { rows = 24, tasks = ALL, ascii = false, theme = plainTheme as TasksViewTheme }: { rows?: number; tasks?: TaskRecord[]; ascii?: boolean; theme?: TasksViewTheme } = {}): Harness {
	const stopped: string[] = [];
	const opened: string[] = [];
	let closed = 0;
	let renders = 0;
	const v = new TasksView({
		theme, rows: () => rows, view, sessionId: "s1", now: () => NOW, ascii,
		tasks: (scope) => (scope === "all" ? tasks : tasks.filter((t) => t.ownerSessionId === "s1")),
		onStop: (t) => { stopped.push(t.id); }, onOpen: (t) => { opened.push(t.id); },
		onClose: () => { closed += 1; }, requestRender: () => { renders += 1; },
	});
	return { view: v, stopped, opened, closed: () => closed, renders: () => renders, screen: (width = 180) => v.render(width).map(stripAnsi) };
}
type TasksViewTheme = { fg(role: string, text: string): string; bold?(text: string): string; bg?(role: string, text: string): string };

/** Artboard 09's card: a rounded frame, `│ ` and ` │` around each row of width - 4 cells. */
function card(width: number, rows: readonly string[]): string[] {
	return [`╭${"─".repeat(width - 2)}╮`, ...rows.map((row) => `│ ${padded(row, width - 4)} │`), `╰${"─".repeat(width - 2)}╯`];
}
/** The two panes: the list's 34 cells, ` │ `, the thread. */
function panes(width: number, left: readonly string[], right: readonly string[], rows: number): string[] {
	return Array.from({ length: rows }, (_, i) => `${padded(left[i] ?? "", 34)} │ ${padded(right[i] ?? "", width - 4 - 37)}`);
}

test("the Agents view is artboard 09's card at 180 and 100 columns: Running and Finished beside the chosen agent's thread, one line per tool", () => {
	const h = harness("agents");
	try {
		const left = [
			"Running",
			spread("▸ ◐ jhon-builder", "4m12s", 34),
			"    slice 3 Date-range filter",
			spread("  ◐ jhon-checker", "1m03s", 34),
			"    slice 2 tests",
			"",
			"Finished  2",
			spread("  ✓ jhon-scout · map billing", "2m10s", 34),
			spread("  ✗ jhon-builder · slice 2", "7m48s", 34),
		];
		const thread = (width: number) => {
			const t = width - 41;
			const head = t >= 80
				? [spread("jhon-builder · claude-opus-5-5 · this session · 48k · $0.420", "◐ running 4m12s", t)]
				: [spread("jhon-builder · claude-opus-5-5", "◐ running 4m12s", t), "this session · 48k · $0.420"];
			return [
				...head,
				"slice 3 Date-range filter",
				"writer · surfaces billing/api/filters.py, tests/billing/",
				"",
				spread("▤ read  odd/changes/add-invoice-export/tasks.md", "✓", t),
				spread("▤ read  billing/api/filters.py", "✓", t),
				spread("✎ edit  tests/billing/test_range.py", "✓", t),
				spread("$ bash  pytest tests/billing/test_range.py", "✗", t),
				spread("✎ edit  billing/api/filters.py", "✓", t),
				spread("$ bash  pytest tests/billing/test_range.py", "✓", t),
				"",
				"Parsing and the month default pass.",
				"Next: the inverted range answers 422.",
				"",
				spread("✎ edit  tests/billing/test_range.py", "✓", t),
				spread("$ bash  pytest tests/billing/test_range.py -k inverted", "◐", t),
			];
		};
		const hints = (width: number) => width === 180
			? "↑↓ agent  enter follow  x stop  F thread  ctrl+j/k scroll  a all sessions  o open session  esc close"
			: "↑↓ agent  enter follow  x stop  F thread  a all sessions  o open session  esc close";
		assertRows((width) => h.view.render(width), (width: SnapshotWidth) => card(width, [
			spread("Agents  this session · 2 running · 2 finished", "esc", width - 4),
			"",
			...panes(width, left, thread(width), 18),
			"",
			hints(width),
		]), "the Agents view");
	} finally { h.view.dispose(); }
});

test("the Jobs view is the same card with the commands and monitors alone: ⧗ while running, the command, its output file and tail", () => {
	const h = harness("jobs", { rows: 16 });
	try {
		const left = [
			"Running",
			spread("▸ ⧗ range tests", "18s", 34),
			"    job-3 · pytest -k range",
			"",
			"Finished  1",
			spread("  ✗ pytest tests/web", "41s", 34),
		];
		const thread = (width: number) => {
			const t = width - 41;
			return [
				spread("range tests · job-3", "⧗ running 18s", t),
				"$ pytest -k range",
				"/tmp/jobs/job-3.log",
				"",
				"collected 3 items",
				"tests/billing/test_range.py ..F",
			];
		};
		assertRows((width) => h.view.render(width), (width: SnapshotWidth) => card(width, [
			spread("Jobs  1 running · 1 finished", "esc", width - 4),
			"",
			...panes(width, left, thread(width), 10),
			"",
			"↑↓ job  enter follow  x stop  F thread  ctrl+j/k scroll  esc close",
		]), "the Jobs view");
	} finally { h.view.dispose(); }
});

test("every row is the card's width, at any width from the narrow form to 180 columns", () => {
	for (const kind of ["agents", "jobs"] as const) {
		const h = harness(kind);
		try {
			for (const width of [180, 140, 100, 80, 60, 56, 40, 30]) {
				const lines = h.screen(width);
				assert.equal(lines.length, 24, `${kind} ${width}: the card fills the terminal's height`);
				for (const line of lines) assert.equal(visibleWidth(line), width, `${kind} ${width}: ${line}`);
			}
		} finally { h.view.dispose(); }
	}
});

test("j/k and the arrows move the selection in display order (running first, oldest first as 09 lists them; then the latest to finish); a new task never steals it", () => {
	const tasks = [...ALL];
	const h = harness("agents", { tasks });
	try {
		const cursor = () => h.screen().find((line) => line.includes("▸ "))!;
		assert.match(cursor(), /▸ ◐ jhon-builder/, "the first row is chosen at first");
		h.view.handleInput("j");
		assert.match(cursor(), /▸ ◐ jhon-checker/);
		h.view.handleInput("\x1b[B");
		assert.match(cursor(), /▸ ✓ jhon-scout/);
		h.view.handleInput("j");
		h.view.handleInput("j");
		assert.match(cursor(), /▸ ✗ jhon-builder · slice 2/, "j stops at the last row");
		h.view.handleInput("k");
		h.view.handleInput("\x1b[A");
		assert.match(cursor(), /▸ ◐ jhon-checker/);
		tasks.push(agent({ id: "agent-9", agent: "jhon-newest", startedAt: NOW - 1_000 }));
		assert.match(cursor(), /▸ ◐ jhon-checker/, "selection is kept by id");
		assert.match(h.screen().join("\n"), /◐ jhon-checker[\s\S]*◐ jhon-newest[\s\S]*Finished/, "a new running agent joins the end of Running");
	} finally { h.view.dispose(); }
});

test("s, c and x stop the selected task only while it runs; q closes; esc closes the list", () => {
	const h = harness("agents");
	try {
		h.view.handleInput("x");
		h.view.handleInput("j");
		h.view.handleInput("s");
		h.view.handleInput("j");
		h.view.handleInput("c");
		assert.deepEqual(h.stopped, ["agent-1", "agent-2"], "a finished agent is not stopped");
		assert.doesNotMatch(h.screen().at(-2)!, /x stop/, "no stop hint on a finished one");
		h.view.handleInput("q");
		assert.equal(h.closed(), 1);
		h.view.handleInput("\x1b");
		assert.equal(h.closed(), 2);
	} finally { h.view.dispose(); }
});

test("F shows the thread alone, with each tool's output under its row; F again or esc returns to the panes", () => {
	const h = harness("agents", { rows: 40 });
	try {
		h.view.handleInput("F");
		const lines = h.screen(100);
		const text = lines.join("\n");
		assert.doesNotMatch(text, /◐ jhon-checker/, "the list gives way");
		assert.match(text, /\$ bash  pytest tests\/billing\/test_range\.py +✗ │\n│ {5}3 failed/, "the output under its tool");
		assert.match(lines.at(-2)!, /^│ ↑↓ scroll {2}f follow {2}x stop {2}F list {2}o open session {2}esc back {2}q close +│$/);
		h.view.handleInput("F");
		assert.match(h.screen(100).join("\n"), /◐ jhon-checker/);
		h.view.handleInput("F");
		h.view.handleInput("\x1b");
		assert.equal(h.closed(), 0, "esc from the thread goes back first");
		assert.match(h.screen(100).join("\n"), /◐ jhon-checker/);
	} finally { h.view.dispose(); }
});

test("a narrow terminal shows the list alone; tab or enter opens the thread and goes back", () => {
	const h = harness("jobs", { rows: 16 });
	try {
		let lines = h.screen(50);
		assert.match(lines[1]!, /^│ Jobs {2}1 running · 1 finished +esc │$/);
		assert.match(lines[3]!, /^│ Running +│$/);
		assert.match(lines.join("\n"), /▸ ⧗ range tests +18s/);
		assert.doesNotMatch(lines.join("\n"), /collected 3 items/);
		assert.match(lines.at(-2)!, /^│ ↑↓ job {2}tab thread {2}x stop {2}esc close +│$/);
		h.view.handleInput("\t");
		lines = h.screen(50);
		assert.match(lines.join("\n"), /collected 3 items/);
		assert.match(lines.at(-2)!, /tab list/);
		h.view.handleInput("\r");
		assert.doesNotMatch(h.screen(50).join("\n"), /collected 3 items/, "enter goes back");
		h.view.handleInput("\r");
		assert.match(h.screen(50).join("\n"), /collected 3 items/, "enter opens it too");
		h.view.handleInput("\x1b");
		assert.equal(h.closed(), 0);
		h.view.handleInput("\x1b");
		assert.equal(h.closed(), 1);
	} finally { h.view.dispose(); }
});

test("following keeps the thread's end in sight; ctrl+j/k, PgUp/PgDn, the arrows in the thread and the wheel scroll it; f and enter follow again", () => {
	const long = agent({ id: "agent-1", task: "t", thread: Array.from({ length: 40 }, (_, i) => tool("read", `file-${i}.ts`)) });
	const h = harness("agents", { rows: 14, tasks: [long] });
	try {
		const visible = () => h.screen(100).join("\n");
		assert.match(visible(), /file-39\.ts/, "following: the end shows");
		h.view.handleInput("\x0b");
		assert.doesNotMatch(visible(), /file-39\.ts/, "ctrl+k scrolls up a page");
		h.view.handleInput("f");
		assert.match(visible(), /file-39\.ts/);
		h.view.handleInput("\x1b[5~");
		assert.doesNotMatch(visible(), /file-39\.ts/, "PgUp scrolls up");
		h.view.handleInput("\r");
		assert.match(visible(), /file-39\.ts/, "enter follows");
		h.screen(100);
		h.view.handleMouse({ type: "wheel", button: "none", x: 60, y: 6, screenX: 60, screenY: 6, width: 100, height: 14, shift: false, alt: false, ctrl: false, wheelDelta: -1 });
		assert.doesNotMatch(visible(), /file-39\.ts/, "the wheel scrolls up");
		h.view.handleInput("\x0a");
		h.view.handleInput("\x1b[6~");
		h.view.handleInput("\x1b[6~");
		assert.match(visible(), /file-39\.ts/, "ctrl+j and PgDn scroll down");
		h.view.handleInput("F");
		h.view.handleInput("k");
		assert.doesNotMatch(visible(), /file-39\.ts/, "k scrolls the thread alone");
		h.view.handleInput("j");
		assert.match(visible(), /file-39\.ts/);
	} finally { h.view.dispose(); }
});

test("a shows every session's agents and back; o opens a saved session; the Jobs view ignores both", () => {
	const h = harness("agents");
	try {
		assert.doesNotMatch(h.screen().join("\n"), /jhon-explore/);
		h.view.handleInput("a");
		const all = h.screen();
		assert.match(all[1]!, /Agents {2}all sessions · 3 running · 2 finished/);
		assert.match(all.at(-2)!, /a this session/);
		assert.match(all.join("\n"), /▸ ◐ jhon-builder/, "the selection starts over");
		h.view.handleInput("j");
		h.view.handleInput("j");
		const explore = h.screen().join("\n");
		assert.match(explore, /▸ ◐ jhon-explore/);
		assert.match(explore, /│ jhon-explore · session s0 +◐ running 30s │/);
		h.view.handleInput("o");
		assert.deepEqual(h.opened, [], "an agent without a saved session has none to open");
		assert.doesNotMatch(h.screen().at(-2)!, /open session/);
		h.view.handleInput("a");
		assert.match(h.screen()[1]!, /this session · 2 running/);
		h.view.handleInput("o");
		assert.deepEqual(h.opened, ["agent-1"]);
	} finally { h.view.dispose(); }
	const jobs = harness("jobs");
	try {
		jobs.view.handleInput("a");
		jobs.view.handleInput("o");
		assert.match(jobs.screen()[1]!, /Jobs {2}1 running/);
		assert.deepEqual(jobs.opened, []);
	} finally { jobs.view.dispose(); }
});

test("the mouse: a click on either line of a row selects it, on a hint runs it, on esc closes; a move paints the hover", () => {
	const roles: string[] = [];
	const theme = { fg: (role: string, text: string) => { roles.push(`${role}:${text}`); return text; }, bold: (t: string) => t };
	const h = harness("agents", { theme });
	const at = (type: "press" | "click" | "move", x: number, y: number) => h.view.handleMouse({ type, button: type === "move" ? "none" : "left", x, y, screenX: x, screenY: y, width: 180, height: 24, shift: false, alt: false, ctrl: false });
	try {
		let lines = h.screen();
		const checkerY = lines.findIndex((l) => l.includes("jhon-checker"));
		assert.deepEqual(at("press", 6, checkerY + 1), { handled: true, render: false }, "the press is taken so the click comes");
		at("click", 6, checkerY + 1);
		assert.match(h.screen()[checkerY]!, /▸ ◐ jhon-checker/);
		lines = h.screen();
		const footer = lines.length - 2;
		at("click", lines[footer]!.indexOf("x stop") + 1, footer);
		assert.deepEqual(h.stopped, ["agent-2"]);
		h.screen();
		roles.length = 0;
		assert.deepEqual(at("move", lines[footer]!.indexOf("F thread") + 1, footer), { handled: true, render: true });
		h.screen();
		assert.ok(roles.includes("warning:F") && roles.includes("warning:thread"), "the hovered hint turns to the hover role");
		roles.length = 0;
		const scoutY = lines.findIndex((l) => l.includes("jhon-scout"));
		at("move", 8, scoutY);
		h.screen();
		assert.ok(roles.some((r) => r.startsWith("warning:") && r.includes("jhon-scout")), "the hovered row's name too");
		at("click", lines[footer]!.indexOf("F thread") + 1, footer);
		assert.doesNotMatch(h.screen().join("\n"), /◐ jhon-builder/, "F thread pressed");
		lines = h.screen();
		at("click", lines[1]!.indexOf("esc") + 1, 1);
		assert.equal(h.closed(), 0, "esc on the thread goes back");
		h.screen();
		at("click", lines[1]!.indexOf("esc") + 1, 1);
		assert.equal(h.closed(), 1);
		assert.equal(at("click", 150, 22), undefined, "a click on nothing is not taken");
	} finally { h.view.dispose(); }
});

test("a finished agent's thread ends with its answer, a failed one's with the reason; activity stands in for a thread", () => {
	const h = harness("agents", { tasks: [scout, failed, agent({ id: "agent-7", task: "t", activity: ["bash go test ./...", "✗ bash failed"] }), agent({ id: "agent-8", task: "t", startedAt: NOW - 1_000 })] });
	try {
		assert.match(h.screen().join("\n"), /bash go test \.\/\.\.\.[\s\S]*✗ bash failed/);
		h.view.handleInput("j");
		let text = h.screen().join("\n");
		assert.match(text, /no output yet/, "nothing yet reads so");
		h.view.handleInput("j");
		text = h.screen().join("\n");
		assert.match(text, /jhon-scout +│?[^\n]*✓ completed 2m10s/);
		assert.match(text, /Answer +│?\n│[^\n]*│ export\.py builds rows/);
		h.view.handleInput("j");
		assert.match(h.screen().join("\n"), /✗ failed 7m48s[\s\S]*Reason[\s\S]*the child exited 1/);
	} finally { h.view.dispose(); }
});

test("a job's header: a monitor's events, a stopped job's dash, an execution failure in full; a long label wraps, never cut (L345)", () => {
	const label = "CI for PR 887 (HS256), attempt 3, watching every required check until it ends";
	const h = harness("jobs", { tasks: [
		job({ id: "job-1", kind: "monitor", label: "errors", command: "tail -f app.log", events: 3 }),
		job({ id: "job-4", label: "watch", status: "stopped", startedAt: NOW - 90_000, endedAt: NOW - 30_000 }),
		job({ id: "job-5", label: "runs", status: "exited", exitCode: 0, startedAt: NOW - 70_000, endedAt: NOW - 60_000, executions: { records: ["/r/1.json"], failed: true, failures: 1, total: 4 } }),
		job({ id: "job-6", label, startedAt: NOW - 200_000, endedAt: NOW - 100_000, status: "exited", exitCode: 0 }),
	] });
	try {
		let text = h.screen(100).join("\n");
		assert.match(text, /errors · job-1 · monitor · 3 events +⧗ running 2m05s/);
		h.view.handleInput("j");
		text = h.screen(100).join("\n");
		assert.match(text, /▸ – watch +1m00s/);
		assert.match(text, /– stopped 1m00s/);
		h.view.handleInput("j");
		text = h.screen(180).join("\n");
		assert.match(text, /▸ ✗ runs/);
		assert.match(text, /✗ failed 10s/);
		assert.match(text, /exited with code 0, but jhon executions recorded a failure \(1 of 4 failed; record \/r\/1\.json\)/);
		h.view.handleInput("j");
		const thread = h.screen(100).slice(3, -3).map((l) => l.split("│")[2]!.trim()).join(" ");
		assert.match(thread, /✓ exit 0 1m40s/);
		assert.ok(thread.replace(/\s+✓ exit 0 1m40s/, "").includes(label), thread);
	} finally { h.view.dispose(); }
});

test("empty: the list says nothing started yet, and the thread asks for a row", () => {
	const h = harness("agents", { tasks: [] });
	try {
		const lines = h.screen(100);
		assert.match(lines[1]!, /Agents {2}this session · 0 running · 0 finished/);
		assert.match(lines[3]!, /^│ nothing started yet +│ Pick a row to read its thread +│$/);
		assert.match(lines.at(-2)!, /^│ a all sessions {2}esc close +│$/);
	} finally { h.view.dispose(); }
});

test("ASCII: the frame, glyphs, icons and hints fold to plain ASCII", () => {
	for (const kind of ["agents", "jobs"] as const) {
		const h = harness(kind, { ascii: true });
		try {
			for (const width of [180, 100, 50]) {
				const lines = h.screen(width);
				const bad = lines.filter((line) => !/^[\x20-\x7e]*$/.test(line));
				assert.deepEqual(bad, [], `${kind} ${width}`);
			}
			const lines = h.screen(100);
			assert.equal(lines[0], `+${"-".repeat(98)}+`);
			if (kind === "agents") {
				assert.match(lines[4]!, /^\| > ~ jhon-builder +4m12s \| /);
				assert.match(lines.join("\n"), /r read {2}odd\/changes\/add-invoice-export\/tasks\.md +\+ \|/);
				assert.match(lines.at(-2)!, /^\| \^v agent {2}enter follow {2}x stop/);
			} else assert.match(lines[4]!, /^\| > ~ range tests +18s \| /);
		} finally { h.view.dispose(); }
	}
});

test("the compact card below 10 rows drops its blank rows; a tiny terminal gets one close row", () => {
	const h = harness("jobs", { rows: 7 });
	try {
		const lines = h.screen(100);
		assert.equal(lines.length, 7);
		assert.match(lines[1]!, /Jobs {2}1 running/);
		assert.match(lines[2]!, /^│ Running +│ range tests/);
		assert.match(lines[5]!, /↑↓ job/);
	} finally { h.view.dispose(); }
	const tiny = harness("jobs", { rows: 4 });
	try {
		assert.deepEqual(tiny.screen(100), [padded("× Close tasks", 100)]);
		assert.deepEqual(tiny.view.render(10).map(stripAnsi), ["× Close t…"]);
	} finally { tiny.view.dispose(); }
});

test("times: seconds, then minutes and padded seconds, then hours and padded minutes", () => {
	assert.equal(formatViewTime(0), "0s");
	assert.equal(formatViewTime(18_400), "18s");
	assert.equal(formatViewTime(63_000), "1m03s");
	assert.equal(formatViewTime(252_000), "4m12s");
	assert.equal(formatViewTime(3_900_000), "1h05m");
});

test("tokens and cost show only once there are some; an agent row's tint is the selection's", () => {
	const fresh = agent({ id: "agent-1", agent: "jhon-builder", model: "m", task: "t" });
	const h = harness("agents", { tasks: [fresh], theme: { fg: (_r, t) => t, bold: (t) => t, bg: (role, t) => `<${role}>${t}</>` } });
	try {
		const raw = h.view.render(180);
		assert.match(stripAnsi(raw.join("\n")).replace(/<\/?[a-zA-Z]*>/g, ""), /jhon-builder · m · this session +◐ running/);
		assert.ok(raw.some((l) => l.includes("<selectedBg>▸ ◐ jhon-builder")), "the chosen row sits on the selection tint");
	} finally { h.view.dispose(); }
});

test("while a task runs the view repaints each second, and stops after dispose", async (t) => {
	t.mock.timers.enable({ apis: ["setInterval"] });
	const h = harness("agents");
	t.mock.timers.tick(1000);
	assert.equal(h.renders(), 1);
	h.view.dispose();
	t.mock.timers.tick(3000);
	assert.equal(h.renders(), 1);
	t.mock.timers.reset();
});
