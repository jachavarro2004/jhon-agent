/**
 * The rail's JOBS and AGENTS sections as the Gentle Shell draws them (T15.12g,
 * L146, the verify's fidelity notes). Each expected render is G's own
 * (gentle-shell@46fa28bc3 lib/jobs-widget.ts renderJobsSection and
 * lib/agents-widget.ts renderAgentsSection, its view `/gentle:agents` swapped
 * for Jhon's `/jhon:agents`) for the same jobs, at 46 columns, with every role
 * painted in its own colour so a wrong role fails as surely as a wrong cell.
 */
import assert from "node:assert/strict";
import test from "node:test";
import { agentsWorkSummary, renderShellAgentsSection, renderShellJobsSection, type RunningJobDisplay } from "../lib/ui/shell-bar.ts";

const CODES: Record<string, number> = { accent: 31, text: 32, dim: 33, muted: 34, success: 35, error: 36, warning: 37 };
const theme = { fg: (role: string, text: string) => `\x1b[${CODES[role] ?? 90}m${text}\x1b[39m`, bold: (text: string) => `\x1b[1m${text}\x1b[22m` };
const NOW = 1_000_000;

const jobs: RunningJobDisplay[] = [
	{ id: "job-1", label: "npm test", startedAt: NOW - 75_000 },
	{ id: "job-2", label: "go build", startedAt: NOW - 5_000 },
	{ id: "job-3", label: "lint", startedAt: NOW - 90_000, endedAt: NOW - 10_000, status: "exited", exitCode: 0 },
	{ id: "job-4", label: "deploy", startedAt: NOW - 90_000, endedAt: NOW - 20_000, status: "failed" },
];

// A sub-agent's label is `<type>: <task>` (lib/background-tasks.ts): G's agent kind and task.
const agent = (id: string, label: string, startedAt: number, finish?: Pick<RunningJobDisplay, "endedAt" | "status">, tokens?: number): RunningJobDisplay =>
	({ id, kind: "agent", label, startedAt, ...(finish ?? {}), ...(tokens ? { tokens } : {}) });
const agents: RunningJobDisplay[] = [
	agent("a1", "explore: map the rail", NOW - 65_000, undefined, 12_300),
	agent("a2", "implementation-writer: write the parser and its tests", NOW - 30_000),
	agent("a3", "verify: verify the slice", NOW - 50_000, { endedAt: NOW - 1_000, status: "failed" }),
	agent("a4", "explore: read docs", NOW - 40_000, { endedAt: NOW - 2_000, status: "completed" }),
	agent("a5", "explore: read more", NOW - 40_000, { endedAt: NOW - 3_000, status: "completed" }),
	agent("a6", "explore: stopped one", NOW - 40_000, { endedAt: NOW - 1_000, status: "stopped" }),
];
const many: RunningJobDisplay[] = Array.from({ length: 10 }, (_, i) => agent(`m${i}`, `explore: task ${i}`, NOW - (i + 1) * 1000));

test("JOBS as G: running newest first then the finished, glyph accent, id text, label and time dim", () => {
	assert.deepEqual(renderShellJobsSection(jobs, theme, 46, NOW), JOBS);
});

test("JOBS folded as G: the header, the first job and `… N more`", () => {
	assert.deepEqual(renderShellJobsSection(jobs, theme, 46, NOW, true), JOBS_FOLDED);
});

test("AGENTS as G: what needs you first in start order, the kind in its own column, failed listed, done and cancelled summed with the view", () => {
	assert.deepEqual(renderShellAgentsSection(agents, theme, 46, NOW), AGENTS);
});

test("AGENTS folded as G keeps the first row", () => {
	assert.deepEqual(renderShellAgentsSection(agents, theme, 46, NOW, true), AGENTS_FOLDED);
});

test("AGENTS past G's eight rows: `… N more · /jhon:agents` before the summaries", () => {
	assert.deepEqual(renderShellAgentsSection([...many, agents[3]!], theme, 46, NOW), AGENTS_MANY);
});

const JOBS = [
		"\u001b[34m▾\u001b[39m \u001b[32m\u001b[1mJOBS\u001b[22m\u001b[39m                  \u001b[34m2 running · 2 finished\u001b[39m",
		"  \u001b[31m⧗\u001b[39m \u001b[32mjob-2\u001b[39m \u001b[33mgo build\u001b[39m                          \u001b[33m5s\u001b[39m",
		"  \u001b[31m⧗\u001b[39m \u001b[32mjob-1\u001b[39m \u001b[33mnpm test\u001b[39m                      \u001b[33m1m 15s\u001b[39m",
		"  \u001b[35m✓\u001b[39m \u001b[32mjob-3\u001b[39m \u001b[33mlint\u001b[39m                          \u001b[33m1m 20s\u001b[39m",
		"  \u001b[36m✗\u001b[39m \u001b[32mjob-4\u001b[39m \u001b[33mdeploy\u001b[39m                        \u001b[33m1m 10s\u001b[39m",
	];
const JOBS_FOLDED = [
		"\u001b[34m▸\u001b[39m \u001b[32m\u001b[1mJOBS\u001b[22m\u001b[39m                  \u001b[34m2 running · 2 finished\u001b[39m",
		"  \u001b[31m⧗\u001b[39m \u001b[32mjob-2\u001b[39m \u001b[33mgo build\u001b[39m                          \u001b[33m5s\u001b[39m",
		"  \u001b[33m… 3 more\u001b[39m                                    ",
	];
const AGENTS = [
		"\u001b[34m▾\u001b[39m \u001b[32m\u001b[1mAGENTS\u001b[22m\u001b[39m                             \u001b[34m2 running\u001b[39m",
		"  \u001b[31m◐\u001b[39m \u001b[32mmap the rail\u001b[39m         \u001b[34mexplore\u001b[39m    \u001b[33m 12k 1m05s\u001b[39m",
		"  \u001b[31m◐\u001b[39m \u001b[32mwrite the parser an…\u001b[39m \u001b[34mimplement…\u001b[39m \u001b[33m       30s\u001b[39m",
		"  \u001b[36m✗\u001b[39m \u001b[36mverify the slice\u001b[39m     \u001b[34mverify\u001b[39m     \u001b[33mfailed 49s\u001b[39m",
		"  \u001b[35m✓\u001b[39m \u001b[33m1 done · /jhon:agents\u001b[39m                     ",
		"  \u001b[33m–\u001b[39m \u001b[33m1 cancelled\u001b[39m                               ",
	];
const AGENTS_FOLDED = [
		"\u001b[34m▸\u001b[39m \u001b[32m\u001b[1mAGENTS\u001b[22m\u001b[39m                             \u001b[34m2 running\u001b[39m",
		"  \u001b[31m◐\u001b[39m \u001b[32mmap the rail\u001b[39m         \u001b[34mexplore\u001b[39m    \u001b[33m 12k 1m05s\u001b[39m",
	];
const AGENTS_MANY = [
		"\u001b[34m▾\u001b[39m \u001b[32m\u001b[1mAGENTS\u001b[22m\u001b[39m                            \u001b[34m10 running\u001b[39m",
		"  \u001b[31m◐\u001b[39m \u001b[32mtask 9\u001b[39m                         \u001b[34mexplore\u001b[39m \u001b[33m10s\u001b[39m",
		"  \u001b[31m◐\u001b[39m \u001b[32mtask 8\u001b[39m                         \u001b[34mexplore\u001b[39m \u001b[33m 9s\u001b[39m",
		"  \u001b[31m◐\u001b[39m \u001b[32mtask 7\u001b[39m                         \u001b[34mexplore\u001b[39m \u001b[33m 8s\u001b[39m",
		"  \u001b[31m◐\u001b[39m \u001b[32mtask 6\u001b[39m                         \u001b[34mexplore\u001b[39m \u001b[33m 7s\u001b[39m",
		"  \u001b[31m◐\u001b[39m \u001b[32mtask 5\u001b[39m                         \u001b[34mexplore\u001b[39m \u001b[33m 6s\u001b[39m",
		"  \u001b[31m◐\u001b[39m \u001b[32mtask 4\u001b[39m                         \u001b[34mexplore\u001b[39m \u001b[33m 5s\u001b[39m",
		"  \u001b[33m… 4 more · /jhon:agents\u001b[39m                     ",
		"  \u001b[35m✓\u001b[39m \u001b[33m1 done · /jhon:agents\u001b[39m                     ",
	];

test("AGENTS idle as G: the header and `none running · /jhon:agents` (L165)", () => {
	assert.deepEqual(renderShellAgentsSection([], theme, 46, NOW).map((line) => line.replace(/\x1b\[[0-9;]*m/g, "")), ["▾ AGENTS" + " ".repeat(46 - 12) + "idle", "  none running · /jhon:agents".padEnd(46)]);
});

// G's agentsSummary: `◐ N agents`, `✗ N` for the recent failures; nothing while none is live or failed (L165).
test("the work row's agents digest counts the live agents and `✗ N` failed, as G's", () => {
	const plain = { fg: (_r: string, t: string) => t, bold: (t: string) => t };
	assert.deepEqual(agentsWorkSummary(agents, plain, NOW), { left: "◐ 2 agents · ✗ 1", priority: 1 });
	assert.deepEqual(agentsWorkSummary([agents[2]!], plain, NOW), { left: "✗ 1", priority: 1 }, "a failure alone still leads");
	assert.equal(agentsWorkSummary([agents[3]!], plain, NOW), undefined, "done work says nothing");
	assert.equal(agentsWorkSummary([], plain, NOW), undefined);
});
