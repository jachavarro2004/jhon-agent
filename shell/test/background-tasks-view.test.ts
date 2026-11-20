import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import type { AgentRun, JobRecord, TaskRecord } from "../lib/background-tasks.ts";
import { TasksView } from "../lib/background-tasks-view.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";

const theme = { fg: (_role: string, text: string) => text };

function job(overrides: Partial<JobRecord> & Pick<JobRecord, "id">): JobRecord {
	return {
		kind: "command",
		label: overrides.id,
		command: `run ${overrides.id}`,
		cwd: "/repo",
		ownerSessionId: "s1",
		outputPath: `/tmp/jobs/${overrides.id}.log`,
		startedAt: 0,
		status: "running",
		tail: [],
		...overrides,
	};
}

function harness(jobs: TaskRecord[], rows = 14, kind: "agents" | "jobs" = "jobs") {
	const stopped: string[] = [];
	let closed = 0;
	let renders = 0;
	const view = new TasksView({
		theme,
		rows: () => rows,
		view: kind,
		sessionId: "s1",
		tasks: () => jobs,
		now: () => 125_000,
		onStop: (selected) => { stopped.push(selected.id); },
		onClose: () => { closed += 1; },
		requestRender: () => { renders += 1; },
	});
	const screen = (width = 100) => view.render(width).map(stripAnsi);
	/** s stops the selected task at once, as G's agents view. */
	const stop = () => { view.handleInput("s"); };
	return { view, screen, stop, stopped, closed: () => closed, renders: () => renders };
}

test("the split view is G's agents overlay: the titled top border with [F Fullscreen] [× Close], the list beside the selected task's thread, G's footer with its buttons and keys", () => {
	const h = harness([
		job({ id: "job-1", label: "CI for PR 1834", command: "gh run watch 42 --exit-status", tail: ["check lint: pass", "check test: pending"] }),
		job({ id: "job-2", label: "build", status: "exited", exitCode: 1, endedAt: 4_000 }),
	]);
	try {
		const lines = h.screen();
		assert.equal(lines.length, 14, "the overlay fills the terminal height");
		for (const line of lines) assert.equal(visibleWidth(line), 100, "every row spans the full width");
		const text = lines.join("\n");
		assert.match(lines[0]!, /^╭─ 力 Jobs · 1 active · 1 finished ─+ \[F Fullscreen\] \[× Close\]╮$/);
		// T92 (L657): the Jobs view has the jobs alone, no section heading.
		assert.match(lines[1]!, /^│ ▸ └ ◐ CI for PR 1834  2m 5s +│ CI for PR 1834 · running · 2m 5s +│$/, "G's list row beside the thread's header");
		assert.match(lines[2]!, /^│ {3}└ ✗ build  4s +│/);
		assert.match(text, /\$ gh run watch 42 --exit-status/);
		assert.match(text, /\/tmp\/jobs\/job-1\.log/);
		assert.match(text, /check test: pending/);
		assert.match(lines.at(-2)!, /^│ \[ Follow \] \[Stop\] j\/k task {3}ctrl\+j\/k scroll {3}f follow {3}s stop it {3}esc back {3}q close +│$/);
		assert.match(lines.at(-1)!, /^╰─+╯$/);
	} finally { h.view.dispose(); }
});

test("j/k move the selection, s stops only a running job, and q closes", () => {
	const h = harness([job({ id: "job-1" }), job({ id: "job-2", status: "exited", exitCode: 0, endedAt: 1_000, tail: ["done"] })]);
	try {
		h.view.handleInput("j");
		const text = h.screen().join("\n");
		assert.match(text, /exited with code 0/);
		assert.match(text, /done/);
		assert.doesNotMatch(h.screen().at(-2)!, /Stop/, "a finished job offers no stop");
		h.stop();
		assert.deepEqual(h.stopped, []);
		h.view.handleInput("k");
		h.stop();
		assert.deepEqual(h.stopped, ["job-1"]);
		h.view.handleInput("q");
		assert.equal(h.closed(), 1);
	} finally { h.view.dispose(); }
});

test("jobs show running first and newest first within each group without changing source order", () => {
	const jobs = [
		job({ id: "old-finished", status: "exited", exitCode: 0, startedAt: 1_000, endedAt: 2_000 }),
		job({ id: "old-running", startedAt: 3_000 }),
		job({ id: "new-finished", status: "stopped", startedAt: 9_000, endedAt: 10_000 }),
		job({ id: "new-running", startedAt: 5_000 }),
	];
	const h = harness(jobs);
	try {
		const lines = h.screen();
		const names = ["new-running", "old-running", "new-finished", "old-finished"];
		const positions = names.map((name) => lines.findIndex((line) => line.includes(name)));
		assert.ok(positions.every((index, i) => index > 0 && (i === 0 || index > positions[i - 1]!)));
		h.stop();
		assert.deepEqual(h.stopped, ["new-running"], "the default selection follows display order");
		h.view.handleInput("j");
		h.stop();
		assert.deepEqual(h.stopped, ["new-running", "old-running"], "navigation follows the same sorted order");
		jobs.push(job({ id: "latest-running", startedAt: 11_000 }));
		assert.match(h.screen().join("\n"), /▸ └ ◐ old-running/, "a new job preserves selection by ID");
		assert.deepEqual(jobs.map((entry) => entry.id), ["old-finished", "old-running", "new-finished", "new-running", "latest-running"]);
	} finally { h.view.dispose(); }
});

test("initial selection survives new jobs and completion before any keyboard navigation", () => {
	const first = job({ id: "job-A", startedAt: 1_000 });
	const jobs = [first];
	const h = harness(jobs);
	try {
		assert.match(h.screen().join("\n"), /▸ └ ◐ job-A/);
		jobs.push(job({ id: "job-B", startedAt: 2_000 }));
		assert.match(h.screen().join("\n"), /▸ └ ◐ job-A/, "a newly started job does not steal selection");
		h.stop();
		assert.deepEqual(h.stopped, ["job-A"], "stop still targets the initially selected job");
		first.status = "exited";
		first.exitCode = 0;
		first.endedAt = 3_000;
		assert.match(h.screen().join("\n"), /▸ └ ✓ job-A/, "completion does not steal selection");
		assert.match(h.screen().join("\n"), /exited with code 0/);
		h.stop();
		assert.deepEqual(h.stopped, ["job-A"], "stop on a finished selection does not target another running job");
		assert.deepEqual(jobs.map((entry) => entry.id), ["job-A", "job-B"]);
	} finally { h.view.dispose(); }
});

test("an empty session says so", () => {
	const h = harness([]);
	try {
		const text = h.screen().join("\n");
		assert.match(text, /力 Jobs · 0 active · 0 finished/);
		assert.match(text, /nothing started yet/, "the empty list");
		assert.match(text, /Pick a row to read its thread/, "the empty thread");
	} finally { h.view.dispose(); }
});

test("a narrow terminal shows the list, and tab toggles the selected task's thread with [← Back]; esc goes back first", () => {
	const h = harness([job({ id: "job-1", label: "ci", command: "gh run watch 7", tail: ["waiting"] })]);
	try {
		let lines = h.screen(40);
		assert.match(lines[0]!, /\[×\]╮$/, "under 60 columns the close button is [×]");
		assert.match(lines.join("\n"), /◐ ci/);
		assert.doesNotMatch(lines.join("\n"), /gh run watch 7/);
		assert.match(lines.at(-2)!, /tab de/);
		h.view.handleInput("\t");
		lines = h.screen(40);
		assert.match(lines[0]!, /\[← Back\] \[×\]╮$/);
		assert.match(lines.join("\n"), /gh run watch 7/);
		assert.match(lines.join("\n"), /waiting/);
		h.view.handleInput("\u001b");
		assert.equal(h.closed(), 0, "escape from details goes back to the list first");
		assert.doesNotMatch(h.screen(40).join("\n"), /gh run watch 7/);
		h.view.handleInput("\u001b");
		assert.equal(h.closed(), 1);
	} finally { h.view.dispose(); }
});

test("F shows the selected task's thread full width, as G's [F Fullscreen]; F again or esc returns", () => {
	const h = harness([job({ id: "job-1", label: "ci", command: "gh run watch 7", tail: ["waiting"] })]);
	try {
		h.view.handleInput("F");
		const lines = h.screen();
		assert.match(lines[0]!, /\[← Back\] \[× Close\]╮$/);
		assert.doesNotMatch(lines.join("\n"), /└ ◐ ci/, "the list gives way");
		assert.match(lines.join("\n"), /gh run watch 7/);
		h.view.handleInput("F");
		assert.match(h.screen().join("\n"), /└ ◐ ci/);
	} finally { h.view.dispose(); }
});

test("G's pointer: a click on a row selects it, [× Close] closes, [Stop] stops", () => {
	const h = harness([job({ id: "job-1", label: "one", startedAt: 2_000 }), job({ id: "job-2", label: "two", startedAt: 1_000 })]);
	try {
		const lines = h.screen();
		const click = (x: number, y: number) => h.view.handleMouse({ type: "click", button: "left", x, y, width: 100, height: 14, shift: false, alt: false, ctrl: false } as never);
		click(6, 2);
		assert.match(h.screen().join("\n"), /▸ └ ◐ two/);
		const footer = h.screen().at(-2)!;
		click(footer.indexOf("[Stop]") + 1, 12);
		assert.deepEqual(h.stopped, ["job-2"]);
		h.screen();
		click(lines[0]!.indexOf("[× Close]") + 2, 0);
		assert.equal(h.closed(), 1);
	} finally { h.view.dispose(); }
});

test("a monitor shows its kind and event count in the list and details", () => {
	const h = harness([job({ id: "job-1", kind: "monitor", label: "errors", command: "tail -f app.log | grep ERROR", events: 3, tail: ["ERROR x"] })]);
	try {
		const text = h.screen().join("\n");
		assert.match(text, /◐ errors/);
		assert.match(text, /errors · running · .* · 3 events/);
	} finally { h.view.dispose(); }
});

const runs: AgentRun[] = [
	{ id: "agent-1", kind: "agent", label: "explore: who calls parse?", agent: "explore", task: "who calls parse?\nmore detail", ownerSessionId: "s1", startedAt: 0, status: "completed", endedAt: 65_000, result: "found 3 callers\nin a.ts", activity: ["grep parse(", "» found 3 callers"], tokens: 0, cost: 0, thread: [] },
	{ id: "agent-2", kind: "agent", label: "worker: fix the bug", agent: "worker", task: "fix the bug", ownerSessionId: "s1", startedAt: 10_000, status: "running", activity: ["bash go test ./...", "✗ bash failed"], tokens: 0, cost: 0, thread: [] },
];

test("the Agents view has the sub-agents alone, running first; a thread without items shows the task, activity, and answer", () => {
	const h = harness([job({ id: "job-1", label: "ci", startedAt: 5_000 }), ...runs], 30, "agents");
	try {
		const running = h.screen(140).join("\n");
		assert.match(h.screen(140)[0]!, /Agents · this session · 1 active · 1 finished/);
		assert.ok(running.indexOf("└ ◐ Subagent worker · fix the bug") < running.indexOf("└ ✓ Subagent explore"), running);
		assert.doesNotMatch(running, /◐ ci/, "no job in the Agents view");
		assert.match(running, /worker · running · this session · 1m 55s/);
		assert.match(running, /✗ bash failed/);
		h.view.handleInput("j");
		const done = h.screen(140).join("\n");
		assert.match(done, /explore · completed/);
		assert.match(done, /Answer[\s\S]*found 3 callers/);
		h.view.handleInput("k");
		h.stop();
		assert.deepEqual(h.stopped, ["agent-2"]);
	} finally { h.view.dispose(); }
});

test("the Agents view holds in a narrow and an ASCII layout", () => {
	const h = harness(runs, 20, "agents");
	try {
		assert.match(h.screen(40).join("\n"), /Agents · this[\s\S]*Subagent worker/);
	} finally { h.view.dispose(); }
	const view = new TasksView({ theme, rows: () => 20, view: "agents", tasks: () => runs, now: () => 125_000, onStop() {}, onClose() {}, requestRender() {}, ascii: true });
	try {
		const text = view.render(100).map(stripAnsi).join("\n");
		assert.match(text, /jhon Agents - this session - 1 active[\s\S]*Subagent worker - fix th/);
		assert.ok(/^[\x20-\x7e\n]*$/.test(text), "ASCII only");
	} finally { view.dispose(); }
});

// L345: the thread's header names the selected task in full, wrapped to the panel, never cut; no output yet reads so.
const LONG_LABEL = "CI for PR 887 (HS256), attempt 3, watching every required check until it ends";

test("a job's header names its full label, wrapped to the panel and never cut, then its status and time (L345)", () => {
	const h = harness([job({ id: "job-1", label: LONG_LABEL, command: "gh pr checks 887 --watch" })], 20);
	try {
		const wide = h.screen(100);
		const right = (lines: string[]) => lines.slice(1, -2).map((l) => l.split("│")[2]?.trim() ?? "").join(" ");
		const header = right(wide);
		assert.ok(header.includes(`${LONG_LABEL} · running · 2m 5s`), header);
		assert.doesNotMatch(header, /…/, "nothing cut in the thread");
		assert.doesNotMatch(header, /command · running/);
		assert.match(header, /\$ gh pr checks 887 --watch/, "the command line under it, as before");
		h.screen(40);
		h.view.handleInput("\t");
		const narrow = h.screen(40);
		const text = narrow.slice(1, -2).map((l) => l.slice(2, -2).trim()).join(" ");
		assert.ok(text.includes(`${LONG_LABEL} · running · 2m 5s`), text);
		assert.doesNotMatch(narrow.slice(1, -2).join("\n"), /…/, "nothing cut when narrow");
	} finally { h.view.dispose(); }
});

test("a sub-agent's thread names its agent, status and time in G's header, then the full first line of its task, wrapped, never cut (L345, L657)", () => {
	const first = "review the whole background tasks view and report every header that cuts a name short";
	const h = harness([{ id: "agent-9", kind: "agent", label: "x", agent: "reviewer", task: `${first}\nsecond line`, ownerSessionId: "s1", startedAt: 0, status: "running", activity: [], tokens: 0, cost: 0, thread: [] } as AgentRun], 20, "agents");
	try {
		const lines = h.screen(100);
		const header = lines.slice(1, -2).map((l) => l.split("│")[2]?.trim() ?? "").join(" ");
		assert.ok(header.startsWith(`reviewer · running · this session · 2m 5s ${first}`), header);
		assert.doesNotMatch(header, /…/);
	} finally { h.view.dispose(); }
});

test("a job or sub-agent with nothing yet reads «no output yet» (L345)", () => {
	const both = [job({ id: "job-1", label: "ci" }), { id: "agent-9", kind: "agent", label: "x", agent: "worker", task: "t", ownerSessionId: "s1", startedAt: 0, status: "running", activity: [], tokens: 0, cost: 0, thread: [] } as AgentRun];
	const agents = harness(both, 20, "agents");
	try {
		const agent = agents.screen().join("\n");
		assert.match(agent, /no output yet/);
		assert.doesNotMatch(agent, /waiting for the first/);
	} finally { agents.view.dispose(); }
	const h = harness(both, 20);
	try {
		const jobText = h.screen().join("\n");
		assert.match(jobText, /no output yet/);
		assert.doesNotMatch(jobText, /waiting for the first/);
	} finally { h.view.dispose(); }
	const view = new TasksView({ theme, rows: () => 20, tasks: () => [job({ id: "job-1", label: LONG_LABEL })], now: () => 125_000, onStop() {}, onClose() {}, requestRender() {}, ascii: true });
	try {
		const text = view.render(60).map(stripAnsi).join("\n");
		assert.ok(/^[\x20-\x7e\n]*$/.test(text), "ASCII only");
		const header = text.split("\n").slice(1, -2).map((l) => l.split("|")[2]?.trim() ?? "").join(" ");
		assert.ok(header.includes(`${LONG_LABEL} - running - 2m 5s`), header);
		assert.match(text, /no output yet/);
	} finally { view.dispose(); }
});
