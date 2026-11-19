// T149 (L1339, L1340 U1): a ChatGPT subscription's usage, from the rate-limit
// data the Codex backend sends with each reply. The names are Codex CLI's own
// (openai/codex, codex-rs/codex-api/src/rate_limits.rs): the x-codex-primary-*
// and x-codex-secondary-* headers on an SSE reply, and the `codex.rate_limits`
// event on the WebSocket transport, which carries no response headers.
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { parseCodexRateLimitEvent, parseUsageHeaders, providerNote, renderUsagePanel } from "../lib/ui/shell-usage.ts";

const NOW = 1_788_600_000_000;
const SECONDS = NOW / 1000;
const plain = { fg: (_c: string, text: string) => text };

const CODEX_HEADERS: Record<string, string> = {
	"x-codex-primary-used-percent": "12.5",
	"x-codex-primary-window-minutes": "300",
	"x-codex-primary-reset-at": String(SECONDS + 3600),
	"x-codex-secondary-used-percent": "40",
	"x-codex-secondary-window-minutes": "10080",
	"x-codex-secondary-reset-at": String(SECONDS + 3 * 86_400),
};

const CODEX_WINDOWS = [
	{ label: "5h", usedPercent: 12.5, windowSeconds: 18_000, resetAt: (SECONDS + 3600) * 1000 },
	{ label: "week", usedPercent: 40, windowSeconds: 604_800, resetAt: (SECONDS + 3 * 86_400) * 1000 },
];

test("Codex headers give the 5h and weekly windows, with used % and reset, keyed by the answering provider", () => {
	assert.deepEqual(parseUsageHeaders(CODEX_HEADERS, NOW, "openai-codex"), {
		provider: "openai-codex",
		plan: undefined,
		limits: [{ name: "codex", windows: CODEX_WINDOWS, limitReached: false }],
		fetchedAt: NOW,
	});
	assert.equal(parseUsageHeaders(CODEX_HEADERS, NOW)?.provider, "openai-codex", "with no provider named, Codex headers are openai-codex's");
	const full = parseUsageHeaders({ ...CODEX_HEADERS, "x-codex-primary-used-percent": "100" }, NOW, "openai-codex");
	assert.equal(full?.limits[0]?.limitReached, true, "a window at 100% is a reached limit");
});

test("a missing or partial Codex header set gives no invented numbers", () => {
	assert.equal(parseUsageHeaders({}, NOW, "openai-codex"), undefined);
	assert.equal(parseUsageHeaders({ "x-codex-primary-used-percent": "12.5" }, NOW, "openai-codex"), undefined, "no window length: no 5h guessed");
	assert.equal(parseUsageHeaders({ "x-codex-primary-window-minutes": "300", "x-codex-primary-reset-at": String(SECONDS) }, NOW, "openai-codex"), undefined, "no used %: no 0% guessed");
	for (const bad of ["", "abc", "12abc", "NaN", "Infinity"]) {
		assert.equal(parseUsageHeaders({ "x-codex-primary-used-percent": bad, "x-codex-primary-window-minutes": "300" }, NOW, "openai-codex"), undefined, `used % ${JSON.stringify(bad)}`);
	}
	assert.equal(parseUsageHeaders({ "x-codex-primary-used-percent": "5", "x-codex-primary-window-minutes": "0" }, NOW, "openai-codex"), undefined, "a zero-length window is no window");
	const primaryOnly = parseUsageHeaders({ "x-codex-primary-used-percent": "12.5", "x-codex-primary-window-minutes": "300", "x-codex-secondary-used-percent": "40" }, NOW, "openai-codex");
	assert.deepEqual(primaryOnly?.limits, [{ name: "codex", windows: [{ label: "5h", usedPercent: 12.5, windowSeconds: 18_000, resetAt: null }], limitReached: false }], "the partial secondary is dropped; a missing reset is null");
	assert.equal(parseUsageHeaders({ "x-codex-other-primary-used-percent": "80", "x-codex-other-primary-window-minutes": "300" }, NOW, "openai-codex"), undefined, "another limit family is not the subscription's");
});

test("the WebSocket transport's codex.rate_limits event gives the same windows and the plan", () => {
	const event = {
		type: "codex.rate_limits",
		plan_type: "plus",
		rate_limits: {
			primary: { used_percent: 12.5, window_minutes: 300, reset_at: SECONDS + 3600 },
			secondary: { used_percent: 40, window_minutes: 10080, reset_at: SECONDS + 3 * 86_400 },
		},
	};
	assert.deepEqual(parseCodexRateLimitEvent(event, NOW, "openai-codex"), {
		provider: "openai-codex",
		plan: "plus",
		limits: [{ name: "codex", windows: CODEX_WINDOWS, limitReached: false }],
		fetchedAt: NOW,
	});
	assert.equal(parseCodexRateLimitEvent({ type: "response.output_text.delta", delta: "hi" }, NOW, "openai-codex"), undefined);
	assert.equal(parseCodexRateLimitEvent({ ...event, metered_limit_name: "codex_other" }, NOW, "openai-codex"), undefined, "another limit family is not the subscription's");
	assert.equal(parseCodexRateLimitEvent({ type: "codex.rate_limits", rate_limits: { primary: { used_percent: 12.5, window_minutes: null } } }, NOW, "openai-codex"), undefined);
	assert.equal(parseCodexRateLimitEvent({ type: "codex.rate_limits" }, NOW, "openai-codex"), undefined);
	assert.equal(parseCodexRateLimitEvent(undefined, NOW, "openai-codex"), undefined);
});

test("the panel shows openai-codex's windows, and before the first reply says usage arrives with it", () => {
	assert.equal(providerNote("openai-codex"), "usage arrives with the first reply");
	const scope = { providers: ["openai-codex"], failed: new Set<string>() };
	assert.deepEqual(renderUsagePanel([], plain, 100, NOW, { provider: "openai-codex" }, undefined, scope), ["力 openai-codex · usage arrives with the first reply"]);
	const lines = renderUsagePanel([parseUsageHeaders(CODEX_HEADERS, NOW, "openai-codex")!], plain, 100, NOW, { provider: "openai-codex" }, undefined, scope);
	assert.match(lines[0]!, /^力 openai-codex · updated just now$/);
	assert.match(lines[1]!, /^ {2}codex 5h +[▰▱]{16} +13% · resets in 1h 0m$/);
	assert.match(lines[2]!, /^ {2}codex week +[▰▱]{16} +40% · resets in 3d 0h$/);
});
