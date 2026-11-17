// T92 (L658): the rail's STATS section, as the Gentle Shell's: the session's
// spend in its header, `profile <name>`, `spent … · N turns · <wall time>`,
// `tokens in … · out …`, `cache N% hit · … read`, `tools N calls · N failed`,
// `agents N runs · <tokens> · $…`, then one row per model.
import assert from "node:assert/strict";
import test from "node:test";
import { collectSessionStats, renderShellStatsSection } from "../lib/ui/shell-stats.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";

const theme = { fg: (_role: string, text: string) => text, bold: (text: string) => text };
const at = (s: number) => new Date(Date.UTC(2026, 10, 14, 10, 0, s)).toISOString();
const assistant = (s: number, model: string, input: number, output: number, cacheRead: number, cost: number) =>
	({ type: "message", timestamp: at(s), message: { role: "assistant", model, usage: { input, output, cacheRead, cacheWrite: 0, cost: { total: cost } } } });
const ENTRIES = [
	{ type: "session", timestamp: at(0) },
	assistant(10, "claude-bridge/claude-opus-5-5", 1000, 500, 9000, 1.5),
	{ type: "message", timestamp: at(20), message: { role: "toolResult", isError: false } },
	{ type: "message", timestamp: at(30), message: { role: "toolResult", isError: true } },
	assistant(95, "claude-sonnet-5-5", 1000, 500, 0, 0.25),
];

test("collectSessionStats sums the session's entries: spend, turns, wall time, tokens, cache, tools, per model", () => {
	const stats = collectSessionStats(ENTRIES);
	assert.equal(stats.cost, 1.75);
	assert.equal(stats.turns, 2);
	assert.equal(stats.spanMs, 95_000);
	assert.deepEqual([stats.input, stats.output, stats.cacheRead], [2000, 1000, 9000]);
	assert.equal(stats.cacheHitRate, 9000 / 11000);
	assert.deepEqual([stats.toolCalls, stats.toolErrors], [2, 1]);
	assert.deepEqual(stats.models, [{ model: "claude-opus-5-5", turns: 1, tokens: 10_500, cost: 1.5 }, { model: "claude-sonnet-5-5", turns: 1, tokens: 1500, cost: 0.25 }]);
});

test("STATS reads as G's: the spend in its header, the profile, totals, the sub-agents' spend, one row per model", () => {
	const stats = { ...collectSessionStats(ENTRIES), subagents: { runs: 3, tokens: 48_000, cost: 0.9 } };
	const lines = renderShellStatsSection(stats, theme, 60, { profile: "current", subscription: true }).map(stripAnsi);
	const expected = [
		/^▾ STATS +\$1\.75 sub$/,
		/^ {2}profile current$/,
		/^ {2}spent  \$1\.75 sub +2 turns · 1m 35s$/,
		/^ {2}tokens in 2\.0k · out 1\.0k$/,
		/^ {2}cache  82% hit · 9\.0k read$/,
		/^ {2}tools  2 calls · 1 failed$/,
		/^ {2}agents 3 runs · 48k · \$0\.900 sub$/,
		/^ {2}claude-opus-5-5 +1 turn · 11k · \$1\.50 sub$/,
		/^ {2}claude-sonnet-5-5 +1 turn · 1\.5k · \$0\.250 sub$/,
	];
	assert.equal(lines.length, expected.length, lines.join("\n"));
	lines.forEach((line, i) => assert.match(line.trimEnd(), expected[i]!));
	for (const line of [lines[0]!, lines[2]!, lines[7]!]) assert.equal(line.length, 60, "the right column ends at the rail's edge");
	for (const line of lines) assert.ok(line.length <= 60, line);
	assert.deepEqual(renderShellStatsSection(stats, theme, 60, { folded: true }).map(stripAnsi).map((l) => l.trimEnd()), ["▸ STATS" + " ".repeat(60 - 7 - 5) + "$1.75"], "folded, the header alone");
	assert.deepEqual(renderShellStatsSection(collectSessionStats([]), theme, 60, {}).map(stripAnsi).length, 0, "a session that has not answered yet shows no STATS");
});
