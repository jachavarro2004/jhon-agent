import assert from "node:assert/strict";
import test from "node:test";
import {
	parseAnthropicHeaders,
	parseNanQuota,
	parseProviderUsage,
	parseUsageHeaders,
	parseUsageSource,
	selectUsageLimit,
	usageScopeProviders,
	UsageSourceRegistry,
	USAGE_SOURCE_SCHEMA,
	UsageStore,
	windowLabel,
	type ProviderUsage,
	type UsageSource,
	type UsageWindow,
} from "../lib/ui/shell-usage.ts";

// Subscription usage: what each connected provider says about its windows.
// Parsers are pure; the store only remembers the latest snapshot.

const NOW = 1_788_600_000_000;

// A Claude Pro/Max response: utilization fractions per window and reset times
// in Unix seconds. The 5h window resets 5h 36m after NOW, the weekly one 2d 1h.
const ANTHROPIC_HEADERS: Record<string, string> = {
	"anthropic-ratelimit-unified-5h-utilization": "0.25",
	"anthropic-ratelimit-unified-5h-reset": "1788620161",
	"anthropic-ratelimit-unified-7d-utilization": "0.5",
	"anthropic-ratelimit-unified-7d-reset": "1788777491",
};

function anthropicUsage(headers: Record<string, string> = ANTHROPIC_HEADERS): ProviderUsage {
	const usage = parseAnthropicHeaders(headers, NOW);
	assert.ok(usage);
	return usage;
}

test("windowLabel names the common windows and falls back to hours or days", () => {
	assert.equal(windowLabel(18_000), "5h");
	assert.equal(windowLabel(604_800), "week");
	assert.equal(windowLabel(10_800), "3h");
	assert.equal(windowLabel(172_800), "2d");
	assert.equal(windowLabel(1_800), "30m");
});

// NaN Cloud quota: per-model allowances for the billing period, plus the
// rolling window the model reports. Percentages are tokensUsed over cap, the
// same ratio the dashboard draws. The payload shape was read off the official
// dashboard bundle, not a published schema, so every field stays optional.
const NAN_QUOTA = {
	periodEnd: "2026-10-01T00:00:00.000Z",
	models: [
		{
			model: "glm5.3",
			cap: 3_000_000_000,
			fullCap: 3_000_000_000,
			tokensUsed: 820_000_000,
			windowHours: 4,
			windowTokens: 400_000_000,
			windowTokensUsed: 120_000_000,
			windowResetsAt: 1_788_620_161,
			email: "someone@example.com",
		},
		{ model: "deepseek-v4-flash", cap: 1_500_000_000, tokensUsed: 150_000_000 },
		{ model: "qwen3.8-flash", cap: 0, tokensUsed: 10 },
	],
};

// The billing-period window carries no label: the model id in front of it names
// the allowance, and the reset text says what the window is. Only a sub-window
// on top (a rolling `4h`) needs a name, so `windowText` spells the unlabeled one
// out for the assertions below.
function windowText(window: UsageWindow): string {
	return `${window.label === "" ? "period" : window.label}:${window.usedPercent}`;
}

test("parseNanQuota maps each model allowance and its rolling window", () => {
	const usage = parseNanQuota(NAN_QUOTA, NOW);
	assert.equal(usage.provider, "nan");
	assert.equal(usage.plan, undefined);
	assert.equal(usage.fetchedAt, NOW);
	assert.deepEqual(usage.limits.map((limit) => limit.name), ["glm5.3", "deepseek-v4-flash"]);

	const [glm, deepseek] = usage.limits;
	assert.deepEqual(glm.windows.map((window) => window.label), ["", "4h"]);
	assert.equal(glm.windows[0].usedPercent, (820_000_000 / 3_000_000_000) * 100);
	assert.equal(glm.windows[0].windowSeconds, 2_212_800);
	assert.equal(glm.windows[0].resetAt, 1_790_812_800_000);
	assert.equal(glm.windows[1].usedPercent, 30);
	assert.equal(glm.windows[1].windowSeconds, 14_400);
	assert.equal(glm.windows[1].resetAt, 1_788_620_161_000);
	assert.equal(glm.limitReached, false);
	assert.deepEqual(deepseek.windows.map(windowText), ["period:10"]);
	assert.equal(JSON.stringify(usage).includes("example.com"), false, "the quota parser must not keep unrelated account fields");
});

test("parseNanQuota falls back to the top-level period end and defaults the window budget", () => {
	const topLevel = parseNanQuota({ periodEnd: 1_790_812_800, models: [{ model: "glm5.3", cap: 3_000_000_000, tokensUsed: 0 }] }, NOW);
	assert.equal(topLevel.limits[0].windows[0].resetAt, 1_790_812_800_000);

	const defaulted = parseNanQuota({ models: [{ model: "glm5.3", cap: 3_000_000_000, tokensUsed: 0, windowTokensUsed: 100_000_000 }] }, NOW);
	assert.deepEqual(defaulted.limits[0].windows.map(windowText), ["period:0", "4h:25"]);

	const overCap = parseNanQuota({ models: [{ model: "glm5.3", cap: 3_000_000_000, tokensUsed: 3_000_000_000, windowHours: 12, windowTokensUsed: 60_000_000 }] }, NOW);
	assert.deepEqual(overCap.limits[0].windows.map(windowText), ["period:100", "12h:15"]);
	assert.equal(overCap.limits[0].limitReached, true);
});

test("parseNanQuota degrades to no data instead of throwing", () => {
	assert.deepEqual(parseNanQuota({}, NOW).limits, []);
	assert.deepEqual(parseNanQuota(undefined, NOW).limits, []);
	assert.deepEqual(parseNanQuota({ models: "nope" }, NOW).limits, []);
	assert.deepEqual(parseNanQuota({ models: [null, "glm5.3", 7] }, NOW).limits, []);
	assert.deepEqual(parseNanQuota({ models: [{ model: "glm5.3", cap: "3000000000", tokensUsed: 1 }] }, NOW).limits, []);
	assert.deepEqual(parseNanQuota({ models: [{ model: "glm5.3", cap: 3_000_000_000 }] }, NOW).limits, []);
	assert.deepEqual(parseNanQuota({ models: [{ model: "", cap: 3_000_000_000, tokensUsed: 1 }] }, NOW).limits, []);
});

// A generic hook: any extension can register a usage source for its own
// provider at runtime, without jhon-shell knowing anything about it.

test("parseUsageSource validates the registration payload and ignores anything malformed", () => {
	const fetchFn = async () => undefined;
	const valid = parseUsageSource({ schema: USAGE_SOURCE_SCHEMA, provider: "acme-cloud", fetch: fetchFn });
	assert.equal(valid?.schema, USAGE_SOURCE_SCHEMA);
	assert.equal(valid?.provider, "acme-cloud");
	assert.equal(valid?.fetch, fetchFn);
	assert.equal(valid?.pendingNote, undefined);

	const withNote = parseUsageSource({ schema: USAGE_SOURCE_SCHEMA, provider: "acme-cloud", pendingNote: "warming up", fetch: fetchFn });
	assert.equal(withNote?.pendingNote, "warming up");

	assert.equal(parseUsageSource({ schema: "jhon-shell.usage-source/v0", provider: "acme-cloud", fetch: fetchFn }), undefined, "wrong schema");
	assert.equal(parseUsageSource({ schema: USAGE_SOURCE_SCHEMA, provider: "", fetch: fetchFn }), undefined, "empty provider");
	assert.equal(parseUsageSource({ schema: USAGE_SOURCE_SCHEMA, provider: "acme cloud", fetch: fetchFn }), undefined, "unsafe provider id");
	assert.equal(parseUsageSource({ schema: USAGE_SOURCE_SCHEMA, provider: "acme-cloud", fetch: "nope" }), undefined, "fetch not a function");
	assert.equal(parseUsageSource({ schema: USAGE_SOURCE_SCHEMA, provider: "acme-cloud", pendingNote: 7, fetch: fetchFn }), undefined, "pendingNote must be a string when present");
	assert.equal(parseUsageSource(null), undefined);
	assert.equal(parseUsageSource(undefined), undefined);
	assert.equal(parseUsageSource("acme-cloud"), undefined);
	assert.equal(parseUsageSource({}), undefined);
});

test("UsageSourceRegistry replaces a provider's source on re-registration", () => {
	const registry = new UsageSourceRegistry();
	assert.equal(registry.has("acme-cloud"), false);
	assert.equal(registry.get("acme-cloud"), undefined);

	const first: UsageSource = { schema: USAGE_SOURCE_SCHEMA, provider: "acme-cloud", fetch: async () => undefined };
	registry.register(first);
	assert.equal(registry.has("acme-cloud"), true);
	assert.equal(registry.get("acme-cloud"), first);

	const second: UsageSource = { schema: USAGE_SOURCE_SCHEMA, provider: "acme-cloud", pendingNote: "still warming up", fetch: async () => undefined };
	registry.register(second);
	assert.equal(registry.get("acme-cloud"), second, "re-registration replaces, never accumulates");
});

// A registered source's resolved value crosses the same trust boundary a
// parsed HTTP payload does. parseProviderUsage validates it exactly like
// one, and the object jhon-shell records is always freshly built, never
// the caller's own reference.

const VALID_SOURCE_USAGE = {
	provider: "acme-cloud",
	plan: "Acme · 42 credits",
	fetchedAt: 100,
	limits: [
		{
			name: "acme-cloud",
			limitReached: false,
			windows: [
				{ label: "week", usedPercent: 40, windowSeconds: 604_800, resetAt: null },
				{ label: "day", usedPercent: 12, windowSeconds: 86_400, resetAt: 200, used: 5, budget: 40 },
			],
		},
	],
};

test("parseProviderUsage accepts a matching shape and copies it defensively", () => {
	const source = structuredClone(VALID_SOURCE_USAGE);
	const parsed = parseProviderUsage(source, "acme-cloud");
	assert.deepEqual(parsed, source);
	assert.notEqual(parsed, source, "the recorded object must never be the caller's own reference");
	assert.notEqual(parsed?.limits, source.limits);
	assert.notEqual(parsed?.limits[0], source.limits[0]);
	assert.notEqual(parsed?.limits[0].windows, source.limits[0].windows);
	assert.notEqual(parsed?.limits[0].windows[0], source.limits[0].windows[0]);

	// Mutating the caller's own object after the fact must never reach the copy.
	source.limits[0].windows[0].usedPercent = 999;
	assert.equal(parsed?.limits[0].windows[0].usedPercent, 40);
});

test("parseProviderUsage rejects a resolution for a different provider", () => {
	assert.equal(parseProviderUsage(structuredClone(VALID_SOURCE_USAGE), "other-provider"), undefined);
});

test("parseProviderUsage rejects a missing or malformed limits array", () => {
	assert.equal(parseProviderUsage({ provider: "acme-cloud", plan: undefined, fetchedAt: 0 }, "acme-cloud"), undefined, "limits missing entirely");
	assert.equal(parseProviderUsage({ provider: "acme-cloud", plan: undefined, fetchedAt: 0, limits: "nope" }, "acme-cloud"), undefined, "limits not an array");
	assert.equal(
		parseProviderUsage({ provider: "acme-cloud", plan: undefined, fetchedAt: 0, limits: [{ name: "x", limitReached: "nope", windows: [] }] }, "acme-cloud"),
		undefined,
		"limitReached must be a boolean",
	);
});

test("parseProviderUsage rejects a non-numeric usedPercent inside a window", () => {
	const usage = structuredClone(VALID_SOURCE_USAGE);
	(usage.limits[0].windows[0] as unknown as { usedPercent: unknown }).usedPercent = "40";
	assert.equal(parseProviderUsage(usage, "acme-cloud"), undefined);
});

test("parseProviderUsage rejects a limit whose windows is not an array", () => {
	const usage = structuredClone(VALID_SOURCE_USAGE);
	(usage.limits[0] as unknown as { windows: unknown }).windows = "nope";
	assert.equal(parseProviderUsage(usage, "acme-cloud"), undefined);
});

test("parseProviderUsage rejects malformed inputs outright", () => {
	assert.equal(parseProviderUsage(undefined, "acme-cloud"), undefined);
	assert.equal(parseProviderUsage(null, "acme-cloud"), undefined);
	assert.equal(parseProviderUsage("acme-cloud", "acme-cloud"), undefined);
	assert.equal(parseProviderUsage({ provider: "acme-cloud", plan: 7, fetchedAt: 0, limits: [] }, "acme-cloud"), undefined, "plan must be a string when present");
	assert.equal(parseProviderUsage({ provider: "acme-cloud", plan: undefined, fetchedAt: Number.POSITIVE_INFINITY, limits: [] }, "acme-cloud"), undefined, "fetchedAt must be finite");
	assert.deepEqual(parseProviderUsage({ provider: "acme-cloud", plan: undefined, fetchedAt: 0, limits: [] }, "acme-cloud"), { provider: "acme-cloud", plan: undefined, fetchedAt: 0, limits: [] });
});

// The server picks the order of the per-model allowances, so drawing the first
// one showed DeepSeek's meter inside a GLM session. The bar follows the model
// the session actually uses, and falls back to the account total when that
// model holds no allowance of its own. `chosen` spells out the limit it picks:
// its name, then each window's label and percentage.
function chosen(usage: ProviderUsage, activeModelId?: string): string | undefined {
	const limit = selectUsageLimit(usage, activeModelId);
	return limit && [limit.name, ...limit.windows.map((w) => `${w.label ? `${w.label} ` : ""}${Math.round(w.usedPercent)}%`)].join(" ");
}

test("selectUsageLimit prefers the active model allowance over the payload order", () => {
	const usage = parseNanQuota(NAN_QUOTA, NOW);
	assert.equal(chosen(usage, "deepseek-v4-flash"), "deepseek-v4-flash 10%");
	assert.equal(chosen(usage, "glm5.3"), "glm5.3 27% 4h 30%");
	assert.equal(chosen(usage), "glm5.3 27% 4h 30%", "without an active model the first limit still wins");
	assert.equal(chosen(usage, "gemma4"), "nan total 22%", "an unmetered model reports the account, never another model");
	assert.equal(chosen(usage, "qwen3.8-flash"), "nan total 22%", "a model the payload skips holds no allowance either");
});

test("selectUsageLimit prefers the active model's family before the account total", () => {
	const usage = parseNanQuota({
		periodEnd: "2026-10-01T00:00:00.000Z",
		models: [
			{ model: "glm5.3-flash", cap: 2_000_000_000, tokensUsed: 200_000_000 },
			{ model: "deepseek-v4-flash", cap: 4_000_000_000, tokensUsed: 200_000_000 },
		],
	}, NOW);
	// The session model holds no allowance of its own and its family has exactly
	// one member: the family is still a closer name for the meter than the whole
	// account, so the ladder visits it before the account rung.
	assert.match(chosen(usage, "glm5.3-turbo") ?? "", /^glm total 10%$/);
	assert.match(chosen(usage, "gemma4") ?? "", /^nan total 7%$/, "no member of the family means the account is the only honest name");
});

test("a single metered allowance still takes the family and account names", () => {
	const usage = parseNanQuota({
		periodEnd: "2026-10-01T00:00:00.000Z",
		models: [
			{ model: "glm5.3-flash", cap: 2_000_000_000, tokensUsed: 400_000_000 },
			{ model: "gemma4", cap: 0 },
		],
	}, NOW);
	// One metered model is still a payload that carries raw allowances, so the bar
	// names the allowance the session draws from instead of listing whichever
	// model the payload happened to report.
	assert.deepEqual(usage.limits.map((limit) => limit.name), ["glm5.3-flash"]);
	assert.equal(chosen(usage, "glm5.3-flash"), "glm5.3-flash 20%");
	assert.match(chosen(usage, "glm5.4") ?? "", /^glm total 20%$/);
	assert.match(chosen(usage, "gemma4") ?? "", /^nan total 20%$/);
});

test("selectUsageLimit leaves providers without raw allowances on their first limit", () => {
	assert.equal(chosen(anthropicUsage(), "claude-opus-5-5"), "claude 5h 25% week 50%");
});

test("parseNanQuota weights the period window by the effective allowance", () => {
	const usage = parseNanQuota({
		periodEnd: "2026-10-01T00:00:00.000Z",
		models: [
			{ model: "glm5.3-flash", cap: 1_500_000_000, fullCap: 2_000_000_000, tokensUsed: 200_000_000 },
			{ model: "glm5.3", cap: 0, fullCap: 3_000_000_000, tokensUsed: 300_000_000 },
		],
	}, NOW);
	// The dashboard divides by the full-period allowance, not by the prorated cap
	// the current period reports, so the percentages keep matching it.
	assert.deepEqual(usage.limits.map((limit) => limit.name), ["glm5.3-flash", "glm5.3"]);
	const [prorated, noPeriodCap] = usage.limits;
	assert.equal(prorated.windows[0].budget, 2_000_000_000, "fullCap is the denominator when it is positive");
	assert.equal(prorated.windows[0].usedPercent, 10);
	assert.equal(noPeriodCap.windows[0].budget, 3_000_000_000, "a period cap prorated to zero still reports a metered model");
	assert.equal(noPeriodCap.windows[0].usedPercent, 10);
	assert.equal(noPeriodCap.limitReached, false);
});

test("parseNanQuota refuses a payload that hides a metered model's usage", () => {
	// A sibling with a metered allowance whose usage cannot be read is drift, not
	// a model to skip: a partial snapshot would understate every aggregate it
	// feeds, so the read fails whole and the last valid snapshot survives.
	const drifted = parseNanQuota({
		periodEnd: "2026-10-01T00:00:00.000Z",
		models: [
			{ model: "glm5.3", cap: 3_000_000_000, tokensUsed: 820_000_000 },
			{ model: "deepseek-v4-flash", cap: 2_000_000_000 },
		],
	}, NOW);
	assert.deepEqual(drifted.limits, []);
	// No allowance at all is not drift: the dashboard draws nothing for these
	// models either, and today's real payload carries them.
	const unmetered = parseNanQuota({
		models: [
			{ model: "glm5.3", cap: 3_000_000_000, tokensUsed: 820_000_000 },
			{ model: "gemma4", cap: 0 },
			{ model: "qwen3.8-flash", tokensUsed: 10 },
		],
	}, NOW);
	assert.deepEqual(unmetered.limits.map((limit) => limit.name), ["glm5.3"]);
	assert.deepEqual(parseNanQuota({ models: "none" }, NOW).limits, []);
});

test("parseNanQuota keeps the raw numbers the aggregates are weighted by", () => {
	const [glm] = parseNanQuota(NAN_QUOTA, NOW).limits;
	assert.equal(glm.windows[0].used, 820_000_000);
	assert.equal(glm.windows[0].budget, 3_000_000_000);
	const [claude] = anthropicUsage().limits[0].windows;
	assert.equal(claude.used, undefined, "only NaN reports raw allowance numbers, which is what gates the aggregates");
	assert.equal(claude.budget, undefined);
});

test("UsageStore keeps the latest snapshot per provider and lists them in order", () => {
	const store = new UsageStore();
	const first: ProviderUsage = { provider: "nan", plan: undefined, limits: [], fetchedAt: 1 };
	const second: ProviderUsage = { provider: "nan", plan: undefined, limits: [], fetchedAt: 2 };
	store.record(first);
	store.record({ provider: "anthropic", plan: undefined, limits: [], fetchedAt: 1 });
	store.record(second);
	assert.equal(store.get("nan"), second);
	assert.deepEqual(store.all().map((usage) => usage.provider), ["nan", "anthropic"]);
});

test("parseAnthropicHeaders turns the unified utilization fractions into 5h and weekly windows", () => {
	const headers = {
		"anthropic-ratelimit-unified-status": "allowed_warning",
		"anthropic-ratelimit-unified-5h-utilization": "0.42",
		"anthropic-ratelimit-unified-5h-reset": "1788620161",
		"anthropic-ratelimit-unified-7d-utilization": "0.875",
		"anthropic-ratelimit-unified-7d-reset": "1789206961",
		"anthropic-ratelimit-unified-representative-claim": "seven_day",
	};
	const usage = parseAnthropicHeaders(headers, NOW);
	assert.ok(usage);
	assert.equal(usage.provider, "anthropic");
	assert.deepEqual(usage.limits.map((limit) => limit.name), ["claude"]);
	assert.deepEqual(usage.limits[0].windows.map((w) => `${w.label}:${w.usedPercent}:${w.resetAt}`), ["5h:42:1788620161000", "week:87.5:1789206961000"]);
	assert.equal(usage.limits[0].limitReached, false);
	assert.equal(parseAnthropicHeaders({ ...headers, "anthropic-ratelimit-unified-status": "rejected" }, NOW)?.limits[0].limitReached, true);
	assert.equal(parseAnthropicHeaders({ "anthropic-ratelimit-requests-remaining": "99" }, NOW), undefined);
});

test("parseAnthropicHeaders keeps per-model weekly windows as their own limits, and skips the overage-included total", () => {
	// pi-claude-bridge passes a window name it has no abbreviation for through as is.
	const usage = parseAnthropicHeaders({
		"anthropic-ratelimit-unified-5h-utilization": "0.1",
		"anthropic-ratelimit-unified-7d-utilization": "0.5",
		"anthropic-ratelimit-unified-7d_oi-utilization": "0.55",
		"anthropic-ratelimit-unified-seven_day_opus-utilization": "0.3",
		"anthropic-ratelimit-unified-seven_day_opus-reset": "1789206961",
		"anthropic-ratelimit-unified-seven_day_sonnet-utilization": "0.25",
	}, NOW);
	assert.ok(usage);
	assert.deepEqual(usage.limits.map((limit) => `${limit.name}:${limit.windows.map((w) => `${w.label}:${w.usedPercent}:${w.resetAt}`).join(",")}`), [
		"claude:5h:10:null,week:50:null",
		"opus:week:30:1789206961000",
		"sonnet:week:25:null",
	]);
	const onlyModel = parseAnthropicHeaders({ "anthropic-ratelimit-unified-seven_day_opus-utilization": "0.3" }, NOW);
	assert.deepEqual(onlyModel?.limits.map((limit) => limit.name), ["opus"]);
});

test("parseUsageHeaders records the snapshot under the provider that answered", () => {
	assert.equal(parseUsageHeaders({ "anthropic-ratelimit-unified-5h-utilization": "0.1" }, NOW, "claude-bridge")?.provider, "claude-bridge");
	assert.equal(parseUsageHeaders({ "anthropic-ratelimit-unified-5h-utilization": "0.1" }, NOW, undefined)?.provider, "anthropic");
});

test("parseUsageHeaders reads the Anthropic rate-limit headers and nothing else", () => {
	assert.equal(parseUsageHeaders({ "anthropic-ratelimit-unified-5h-utilization": "0.1" }, NOW)?.provider, "anthropic");
	assert.equal(parseUsageHeaders({ "content-type": "application/json" }, NOW), undefined);
});

// Targeted providers: the session's own provider plus every provider the
// active profile's subagent routing names. A qualified ref ("provider/id")
// resolves directly; a bare model id resolves only through the registry
// callback, so an ambiguous ref is dropped instead of guessed.

test("usageScopeProviders targets the main provider and every qualified routing provider", () => {
	assert.deepEqual(
		usageScopeProviders("anthropic", ["nan/glm5.3", "anthropic/claude-opus-5-5", undefined], undefined),
		["anthropic", "nan"],
	);
});

test("usageScopeProviders resolves bare model ids only through the registry callback", () => {
	assert.deepEqual(usageScopeProviders(undefined, ["glm5.3"], (id) => (id === "glm5.3" ? "nan" : undefined)), ["nan"]);
	assert.deepEqual(
		usageScopeProviders(undefined, ["glm5.3", "mystery"], () => undefined),
		[],
		"an id the registry cannot resolve to exactly one provider is dropped, never guessed",
	);
});


