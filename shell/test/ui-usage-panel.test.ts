// T93 (L659): the usage view is back for every configured provider, as the
// Gentle Shell's alt+u panel: each provider's windows with their meters and
// reset times, the active provider first and marked with Jhon's 力; restored
// from 7e204a89^ (renderUsagePanel) and 2ad34ac0^ (UsageView, /jhon:usage, alt+u).
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import { formatReset, parseAnthropicHeaders, parseNanQuota, renderUsagePanel, UsageStore, type ProviderUsage } from "../lib/ui/shell-usage.ts";
import { UsageView } from "../lib/ui/shell-usage-view.ts";
import { usageShortcut } from "../extensions/ui.ts";
import { stripAnsi } from "../lib/ui/terminal-theme.ts";

const plain = { fg: (_c: string, text: string) => text };
const NOW = 1_788_600_000_000;
const claude = (five = "0.25"): ProviderUsage => parseAnthropicHeaders({
	"anthropic-ratelimit-unified-5h-utilization": five, "anthropic-ratelimit-unified-5h-reset": "1788620161",
	"anthropic-ratelimit-unified-7d-utilization": "0.5", "anthropic-ratelimit-unified-7d-reset": "1788777491",
}, NOW, "claude-bridge")!;
const nan = () => parseNanQuota({ periodEnd: "2026-10-01T00:00:00.000Z", models: [{ model: "glm5.3", cap: 1_000_000_000, tokensUsed: 270_000_000 }] }, NOW);

test("formatReset speaks in minutes, hours, or days", () => {
	assert.equal(formatReset(NOW + 25 * 60_000, NOW), "resets in 25m");
	assert.equal(formatReset(NOW + (1 * 3600 + 48 * 60) * 1000, NOW), "resets in 1h 48m");
	assert.equal(formatReset(NOW + (2 * 86_400 + 5 * 3600) * 1000, NOW), "resets in 2d 5h");
	assert.equal(formatReset(NOW - 1000, NOW), "resets now");
	assert.equal(formatReset(null, NOW), "");
});

test("the panel lists every provider in scope: the active one first with 力, each window with its meter, percent and reset", () => {
	const lines = renderUsagePanel([nan(), claude()], plain, 80, NOW, { provider: "claude-bridge" }, undefined, { providers: ["claude-bridge", "nan", "openai"], failed: new Set() });
	for (const line of lines) assert.ok(visibleWidth(line) <= 80, line);
	assert.match(lines[0]!, /^力 claude-bridge · updated just now$/);
	assert.match(lines[1]!, /^ {2}claude 5h +[▰▱]{16} +25% · resets in 5h 36m$/);
	assert.match(lines[2]!, /^ {2}claude week +[▰▱]{16} +50% · resets in 2d 1h$/);
	assert.match(lines[3]!, /^nan · updated just now$/);
	assert.match(lines[4]!, /^ {2}glm5\.3 +[▰▱]{16} +27% · resets in/);
	assert.equal(lines[5], "openai · this provider does not report subscription usage");
	assert.deepEqual(renderUsagePanel([], plain, 100, NOW, { provider: "claude-bridge" }), ["力 claude-bridge · usage arrives with the first reply"]);
	assert.deepEqual(renderUsagePanel([], plain, 100, NOW, { provider: "nan" }, undefined, { providers: ["nan"], failed: new Set(["nan"]) }), ["力 nan · the fetch failed · r to retry"]);
	assert.match(renderUsagePanel([claude("0.84")], plain, 100, NOW, { provider: "claude-bridge" }).join("\n"), /⊘ claude-bridge is at 84% of claude 5h; resets in 5h 36m\./, "near a limit it warns");
	assert.match(renderUsagePanel([claude()], plain, 100, NOW, { provider: "claude-bridge" }, undefined, undefined, true)[0]!, /^jhon claude-bridge/, "力 reads jhon in ASCII");
});

test("UsageView frames the panel under `◆ Subscriptions`, refreshes on open and on r, and closes on esc or q", async () => {
	const store = new UsageStore();
	store.record(claude());
	let refreshes = 0;
	let closed = 0;
	const view = new UsageView(store, { theme: plain, now: () => NOW, active: () => ({ provider: "claude-bridge" }), onRefresh: async () => { refreshes += 1; }, onClose: () => { closed += 1; }, requestRender: () => {} });
	const lines = view.render(70).map(stripAnsi);
	assert.match(lines[0]!, /^╭─ ◆ Subscriptions ─+╮$/);
	assert.match(lines[1]!, /^│ 力 claude-bridge · updated just now +│$/);
	assert.match(lines.at(-2)!, /^│ r refresh {3}esc close +│$/);
	view.handleInput("r");
	assert.match(stripAnsi(view.render(70)[0]!), /refreshing…/);
	await new Promise((resolve) => setImmediate(resolve));
	assert.equal(refreshes, 1);
	view.handleInput("\x1b");
	view.handleInput("q");
	assert.equal(closed, 2);
});

test("the usage key is alt+u; JHON_SHELL_USAGE_KEY sets another or turns it off", () => {
	assert.equal(usageShortcut({}), "alt+u");
	assert.equal(usageShortcut({ JHON_SHELL_USAGE_KEY: "ctrl+shift+u" }), "ctrl+shift+u");
	assert.equal(usageShortcut({ JHON_SHELL_USAGE_KEY: "off" }), undefined);
});
