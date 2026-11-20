import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { sliceByColumn, visibleWidth } from "@earendil-works/pi-tui";
import {
	compactionPercent, compactAt,
	formatCost,
	formatTokens,
	gaugeTone,
	renderGauge,
	renderShellStatusLayout,
	renderShellStatusSections,
	renderShellStatusBar,
	renderShellAgentsSection,
	renderShellJobsSection,
	elapsed,
	shellEnabled,
	type ShellBarModel,
	type ShellBarTheme,
} from "../lib/ui/shell-bar.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";
import { envOverrides, memorySettings } from "./fake-jhon-settings.ts";

// The settings `jhon settings` would give the shell under an environment (S38).
const envSettings = (env: NodeJS.ProcessEnv) => memorySettings({}, undefined, envOverrides(env));

// The shell bar replaces pi's three-line footer with one line of
// segments. Rendering is pure so it can be verified without a TUI.

const taggedTheme: ShellBarTheme = {
	fg(color: string, value: string) {
		return `<${color}>${value}</${color}>`;
	},
	bold(value: string) {
		return value;
	},
};

const plainTheme: ShellBarTheme = {
	fg(_color: string, value: string) {
		return value;
	},
	bold(value: string) {
		return value;
	},
};


function model(overrides: Partial<ShellBarModel> = {}): ShellBarModel {
	return {
		cwd: "~/work/jhon-demo",
		branch: "main",
		sessionName: undefined,
		modelId: "gpt-5.5",
		effort: "medium",
		contextPercent: 45,
		contextWindow: 272_000,
		costTotal: 9.49,
		subscription: true,
		...overrides,
	};
}

test("under a multiplexer that shows the path the rail draws STATUS as without one: the change is the shell's (L112)", () => {
	const ldd = { change: "x", slice: "Slice 2: launch", slices: ["done", "current"], route: "standard" } as const;
	const inside = renderShellStatusSections(model({ ldd: { ...ldd, slices: [...ldd.slices] }, hostShowsPath: true }), plainTheme, 50);
	assert.match(inside.join("\n"), /◆ x/);
	assert.deepEqual(inside, renderShellStatusSections(model({ ldd: { ...ldd, slices: [...ldd.slices] } }), plainTheme, 50));
});

test("running tasks leave STATUS for the JOBS card, newest first, each with its id and its time to the second", () => {
	const data = model({ jobs: [
		{ id: "job-2", label: "CI for PR 1860", startedAt: 2 },
		{ id: "job-1", label: "Build runtime", startedAt: 1 },
	] });
	for (const width of [30, 60]) {
		assert.doesNotMatch(renderShellStatusSections(data, plainTheme, width).join("\n"), /En curso|CI for PR/);
		const lines = renderShellJobsSection(data.jobs, plainTheme, width, 61_000);
		const text = lines.join("\n");
		assert.match(text, width === 60 ? /JOBS[\s\S]*2 running[\s\S]*⧗ job-2 CI for PR 1860[\s\S]*1m 1s[\s\S]*⧗ job-1 Build runtime/ : /JOBS[\s\S]*2 running[\s\S]*1m 1s/);
		assert.ok(lines.every((line) => visibleWidth(line) <= width));
	}
	assert.deepEqual(renderShellJobsSection([], plainTheme, 60), [], "no running task, no card");
});

test("Status title stays plain", () => {
	const lines = renderShellStatusSections(model(), plainTheme, 60);
	assert.match(lines[0], /^▾ LDD *$/, "the section's header: the title, no key (C6)");
});

test("Status respects terminal width", () => {
	for (const width of [8, 12, 16, 20, 32, 60]) {
		const lines = renderShellStatusSections(model(), plainTheme, width);
		for (const line of lines) assert.ok(visibleWidth(line) <= width, `${width}: ${line}`);
		if (width >= 20) assert.match(lines[0], /^▾ LDD/);
	}
});

test("renderGauge fills cells proportionally to the percentage", () => {
	assert.equal(renderGauge(45, 8), "▰▰▰▰▱▱▱▱");
	assert.equal(renderGauge(0, 8), "▱▱▱▱▱▱▱▱");
	assert.equal(renderGauge(100, 8), "▰▰▰▰▰▰▰▰");
	assert.equal(renderGauge(null, 8), "▱▱▱▱▱▱▱▱");
});

test("gaugeTone reads the one meter ladder: ok under 70, warning from 70, error from 90 (S1)", () => {
	assert.equal(gaugeTone(45), "success");
	assert.equal(gaugeTone(69), "success");
	assert.equal(gaugeTone(70), "warning");
	assert.equal(gaugeTone(89), "warning");
	assert.equal(gaugeTone(90), "error");
	assert.equal(gaugeTone(null), "dim");
});

test("formatTokens and formatCost keep the bar compact", () => {
	assert.equal(formatTokens(950), "950");
	assert.equal(formatTokens(4_200), "4.2k");
	assert.equal(formatTokens(272_000), "272k");
	assert.equal(formatTokens(13_000_000), "13M");
	assert.equal(formatCost(9.49, true), "$9.49 sub");
	assert.equal(formatCost(0.004, false), "$0.004");
});

test("shellEnabled stays off inside a jhon child session", () => {
	assert.equal(shellEnabled({ JHON_PI_CHILD: "1" }), false);
});

test("shellEnabled honors JHON_SHELL=0", () => {
	assert.equal(shellEnabled({}), true);
	assert.equal(shellEnabled({ JHON_SHELL: "1" }), true);
	assert.equal(shellEnabled({ JHON_SHELL: "0" }), false);
	assert.equal(shellEnabled({ JHON_SHELL: "false" }), false);
});

// T15.12c slice 3, T15.12d: the rail's section headers are the Gentle
// Shell's renderPanelHeader: the fold glyph muted, the title upper-case in
// text bold, the count muted at the right; no key on a header (C6); the rail
// draws them in their cards.
const boldTheme: ShellBarTheme = { fg: (color: string, value: string) => `<${color}>${value}</${color}>`, bold: (value: string) => `<b>${value}</b>` };

test("every rail section header is the Gentle Shell's: ▾ muted, the title text bold, the count muted, no key (T15.12c)", () => {
	const jobs = [{ id: "job-1", label: "npm test", startedAt: 0, kind: "command" as const }, { id: "agent-1", label: "worker: slice 1", startedAt: 0, kind: "agent" as const }];
	const ldd = { change: "rail", route: "verified", slices: ["current"], next: "apply" } as ShellBarModel["ldd"];
	const cards = {
		// Tagged roles count as columns, so the cards are read at a width they fit in.
		LDD: [renderShellStatusSections(model({ ldd }), boldTheme, 300), "verified route"],
		JOBS: [renderShellJobsSection(jobs, boldTheme, 300, 1_000), "1 running"],
		AGENTS: [renderShellAgentsSection(jobs, boldTheme, 300, 1_000), "1 running"],
	} as const;
	for (const [name, [lines, count]] of Object.entries(cards)) {
		assert.match(lines[0]!, new RegExp(`^<muted>▾</muted> <text><b>${name}</b></text> +`), `${name}: the G header`);
		assert.ok(lines[0]!.endsWith(`<muted>${count}</muted>`), `${name}: the count muted at the right: ${lines[0]}`);
		assert.doesNotMatch(lines[0]!, /accent|alt\+[jsr]|[╭╮]/, `${name}: no accent title, no key, no frame: the rail draws the card`);
	}
});

test("hovering a rail header paints its glyph and title in the hover role (T15.12c)", () => {
	const jobs = [{ id: "job-1", label: "npm test", startedAt: 0, kind: "command" as const }, { id: "agent-1", label: "worker: slice 1", startedAt: 0, kind: "agent" as const }];
	const hovered = (lines: readonly string[], name: string) => assert.match(lines[0]!, new RegExp(`<warning>▾</warning> <warning><b>${name}</b></warning>`), name);
	for (const name of ["LDD"] as const) {
		const lines = renderShellStatusSections(model(), boldTheme, 300, new Set(), name);
		hovered([lines.find((line) => line.includes(`<b>${name}</b>`))!], name);
		assert.equal(lines.filter((line) => line.includes("<warning>")).length, 1, `${name}: only its own header`);
	}
	hovered(renderShellJobsSection(jobs, boldTheme, 300, 1_000, false, true), "JOBS");
	hovered(renderShellAgentsSection(jobs, boldTheme, 300, 1_000, false, true), "AGENTS");
	assert.doesNotMatch(renderShellStatusSections(model(), boldTheme, 300).join("\n"), /<warning>/, "no hover, no hover role");
});

test("the Status sections report the LDD header's row, the click target (T15.12c)", () => {
	const { lines, targets } = renderShellStatusLayout(model(), plainTheme, 46);
	assert.deepEqual(targets.map((target) => [target.kind, target.section, stripAnsi(lines[target.row]!).match(/▾ (\S+)/)?.[1]]), [["toggle", "LDD", "LDD"]]);
});


// T174 (L1462): no INTEGRATIONS section; the status line keeps the mode's badge.
test("the integrations are gone from the rail; the status line keeps a mode's badge", () => {
	const yolo = renderShellStatusSections(model({ mode: "yolo" }), plainTheme, 50).join("\n");
	assert.doesNotMatch(yolo, /INTEGRATIONS|permissions|nothing reported/);
	assert.match(renderShellStatusBar(model({ mode: "yolo" }), plainTheme, 160).rows[0]!, /YOLO/, "the status bar keeps its badge");
});


// L153 answer 4, T174 (L1462): the rail's Status is the LDD section alone:
// no CHANGES, no INTEGRATIONS, no Stats (the dashboard's), no STATUS card.
test("Status is the rail's LDD section alone; no STATUS card, no CHANGES, INTEGRATIONS or Stats", () => {
	const ldd = { change: "rail-redesign", route: "verified", slices: ["done", "current"], next: "apply" } as ShellBarModel["ldd"];
	const data = model({ ldd, sessionName: "session", profile: "team" });
	const layout = renderShellStatusLayout(data, plainTheme, 46);
	const lines = layout.lines;
	assert.deepEqual(layout.groups.map((group) => lines[group.start]!.match(/^▾ (\S+)/)?.[1]), ["LDD"]);
	assert.match(lines[0]!, /^▾ LDD +verified route$/, "LDD keeps the route as its count, no key (C6)");
	const text = lines.join("\n");
	assert.match(text, /LDD[\s\S]*◆ rail-redesign/);
	assert.doesNotMatch(text, /STATUS|STATS|CHANGES|INTEGRATIONS|\/jhon:changes|Project|Branch|main|Session|session|Profile|team/, "the header and the transcript say those");
	for (const width of [1, 8, 24, 46]) assert.ok(renderShellStatusSections(data, plainTheme, width).every((line) => visibleWidth(line) <= width));
	assert.match(renderShellStatusSections(model(), plainTheme, 46).join("\n"), /▾ LDD *\n {2}No active LDD change/);
	const folded = renderShellStatusSections(data, plainTheme, 46, new Set(["LDD"]));
	assert.equal(folded.length, 1, "a folded section is its header alone");
	assert.match(folded[0]!, /^▸ LDD +verified route$/);
});

test("the Status sections drop Model, Effort, Context and Cost, all the header's", () => {
	const data = model({ profile: "team", sessionName: "session" });
	const text = renderShellStatusSections(data, plainTheme, 60).join("\n");
	for (const gone of [/Usage/, /Model/, /Effort/, /Context/, /Cost/, /\$9\.49/, /gpt-5\.5/]) assert.doesNotMatch(text, gone);
});

test("a filling context says where pi compacts", () => {
	assert.equal(compactionPercent(200_000), 60, "Jhon compacts the main session at 60%");
	assert.equal(compactionPercent(200_000, null), 92, "with Jhon's off, pi's own");
	assert.equal(compactionPercent(undefined), undefined);
	assert.deepEqual([compactAt(envSettings({})), compactAt(envSettings({ JHON_SHELL_COMPACT_AT: "70" })), compactAt(envSettings({ JHON_SHELL_COMPACT_AT: "off" })), compactAt(envSettings({ JHON_SHELL_COMPACT_AT: "x" }))], [60, 70, undefined, 60]);
	const plain = { fg: (_r: string, t: string) => t, bold: (t: string) => t };
	const at = (percent: number) => renderShellStatusBar(model({ contextPercent: percent, contextWindow: 200_000 }), plain, 200).rows[0]!;
	assert.doesNotMatch(at(45), /compacts/);
	assert.match(at(55), /ctx ▰+▱* 55% · compacts at 60%/, "ten points before it, the gauge says where");
});

test("JOBS and AGENTS count their running tasks and run their time to the second (L168)", () => {
	const jobs = [
		{ id: "a", kind: "agent" as const, label: "worker: slice 7", startedAt: 0, step: "bash · go test" },
		{ id: "b", kind: "command" as const, label: "jhon affected tests", startedAt: 0 },
		{ id: "c", kind: "command" as const, label: "build", startedAt: 0 },
	];
	const agents = renderShellAgentsSection(jobs, plainTheme, 70, 312_000).join("\n");
	const commands = renderShellJobsSection(jobs, plainTheme, 70, 312_000).join("\n");
	assert.match(agents, /AGENTS[\s\S]*1 running/);
	assert.match(commands, /JOBS[\s\S]*2 running/);
	// L168: one row per sub-agent, `◐ <task>  <kind>  <time>` as G's; the
	// latest step lives in alt+s.
	assert.match(agents, /◐ slice 7 +worker 5m12s/);
	assert.doesNotMatch(agents, /↳|bash · go test|jhon affected/);
	assert.match(commands, /⧗ b jhon affected tests +5m 12s/);
	assert.doesNotMatch(commands, /slice 7/);
	assert.deepEqual([elapsed(45_000), elapsed(312_000), elapsed(3_900_000)], ["45s", "5m 12s", "1h 5m"]);
});

test("AGENTS: one row per sub-agent, no sub-agent word, the time right-aligned at narrow widths (L141, L168)", (t) => {
	const jobs = [
		{ id: "a", kind: "agent" as const, label: "explore: Read-only exploration of the whole shell folder", startedAt: 0, step: "bash · rg something" },
		{ id: "b", kind: "agent" as const, label: "worker: slice 7", startedAt: 100_000, step: "edit · file.ts" },
	];
	for (const style of ["sections"]) {
		for (const width of [30, 40, 60]) {
			const lines = renderShellAgentsSection(jobs, plainTheme, width, 166_000).map(stripAnsi);
			assert.match(lines[0]!, /^▾ AGENTS +2 running$/, "the count stays");
			const rows = lines.filter((line) => line.includes("◐"));
			assert.equal(rows.length, 2, `${style} ${width}: one row per sub-agent`);
			assert.ok(!lines.some((line) => line.includes("↳")), `${style} ${width}: no activity line`);
			assert.ok(!rows.some((line) => line.includes("sub-agent")), `${style} ${width}: no sub-agent word in the rows`);
			// G's start order: the oldest first.
			assert.match(rows[0]!, /◐ Read.* explore /);
			assert.match(rows[1]!, /◐ slice 7.* worker /);
			const ends = rows.map((row) => row.replace(/[│ ]+$/, ""));
			assert.match(ends[0]!, /2m46s$/, `${style} ${width}: the time ends the row`);
			assert.match(ends[1]!, /1m06s$/, `${style} ${width}: the time ends the row`);
			assert.equal(ends[0]!.length, ends[1]!.length, `${style} ${width}: both times end in the same column`);
			if (width === 30) assert.match(rows[0]!, /…/, `${style}: a long task is clipped`);
			for (const line of lines) assert.ok(visibleWidth(line) <= width);
		}
	}
});

// L168: BACKGROUND splits into JOBS (commands and monitors: glyph, id, label,
// time) and AGENTS (sub-agents: glyph, task, type, tokens, time), as in Gentle
// Shell; both hide while they have nothing: only STATUS always shows (L173).
test("JOBS and AGENTS render one row each at 30, 40 and 60 columns, finished jobs a minute, tokens only when known", (t) => {
	const now = 317_000;
	const jobs = [
		{ id: "job-1", kind: "command" as const, label: "Build runtime", startedAt: 0 },
		{ id: "job-2", kind: "monitor" as const, label: "watch CI", startedAt: 100_000 },
		{ id: "job-3", kind: "command" as const, label: "lint", startedAt: 0, endedAt: 300_000, status: "exited" as const, exitCode: 0 },
		{ id: "job-4", kind: "command" as const, label: "old", startedAt: 0, endedAt: 1_000, status: "exited" as const, exitCode: 1 },
		{ id: "agent-1", kind: "agent" as const, label: "worker: stall recovery and lost notice", startedAt: 0, tokens: 2_600_000 },
		{ id: "agent-2", kind: "agent" as const, label: "explore: read the shell", startedAt: 100_000 },
	];
	for (const style of ["sections"]) {
		for (const width of [30, 40, 60]) {
			const at = `${style} ${width}`;
			const jobLines = renderShellJobsSection(jobs, plainTheme, width, now).map(stripAnsi);
			const agentLines = renderShellAgentsSection(jobs, plainTheme, width, now).map(stripAnsi);
			for (const line of [...jobLines, ...agentLines]) assert.ok(visibleWidth(line) <= width, `${at}: ${JSON.stringify(line)}`);
			// The G header: the count at the right end.
			assert.match(jobLines[0]!, /^▾ JOBS +2 running · 1 finished$/, at);
			assert.match(agentLines[0]!, /^▾ AGENTS +2 running$/, at);
			const end = (line: string) => line.replace(/[│ ]+$/, "");
			const jobRows = jobLines.filter((line) => /[⧗✓✗]/.test(line));
			assert.equal(jobRows.length, 3, `${at}: one row per job, a finished one past a minute gone: ${JSON.stringify(jobLines)}`);
			assert.match(jobRows[0]!, /⧗ job-2 /, at);
			assert.match(end(jobRows[0]!), /3m 37s$/, at);
			assert.match(jobRows[1]!, /⧗ job-1 /, at);
			assert.match(end(jobRows[1]!), /5m 17s$/, at);
			assert.match(jobRows[2]!, /✓ job-3 /, at);
			assert.match(end(jobRows[2]!), /5m 0s$/, `${at}: a finished job's time stops at its end`);
			assert.ok(!jobLines.some((line) => /agent-|worker|explore/.test(line)), `${at}: no sub-agent in JOBS`);
			const agentRows = agentLines.filter((line) => line.includes("◐"));
			assert.equal(agentRows.length, 2, `${at}: one row per sub-agent`);
			// G's start order: the oldest first.
			assert.match(end(agentRows[1]!), / explore +3m37s$/, `${at}: no tokens when unknown`);
			assert.match(agentRows[0]!, /◐ \S.* worker +2\.6M 5m17s/, `${at}: task, kind, tokens, time: ${JSON.stringify(agentRows[0])}`);
			assert.ok(!agentLines.some((line) => /job-|Build runtime/.test(line)), `${at}: no command in AGENTS`);
			if (width === 60) assert.match(agentRows[0]!, /◐ stall recovery and lost n/, at);
		}
		const idleJobs = renderShellJobsSection([], plainTheme, 50, now);
		const idleAgents = renderShellAgentsSection([], plainTheme, 50, now).map(stripAnsi);
		assert.deepEqual(idleJobs, [], `${style}: JOBS hides with nothing to show`);
		assert.deepEqual(idleAgents, ["▾ AGENTS" + " ".repeat(50 - 12) + "idle", "  no agent running · /jhon:agents".padEnd(50)], `${style}: AGENTS idle as G's (L165)`);
	}
});
