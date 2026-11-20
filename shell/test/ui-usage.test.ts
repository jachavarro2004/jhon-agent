/**
 * T174-S5b (L1493-L1495): the subscription usage, back in the shell's own
 * code. Claude (anthropic, claude-bridge) and ChatGPT (openai-codex) send
 * their rate-limit windows with each reply; a provider another extension
 * registers is fetched at most every 5 minutes, each fetch bounded by a
 * timeout. The status bar shows the week, with its meter and reset, and the
 * 5h window from 70%; /jhon:usage and alt+u (shell.keys.usage) open the
 * overlay: r refreshes, esc or q closes.
 *
 *   cd shell && node --experimental-strip-types --test test/ui-usage.test.ts
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { visibleWidth } from "@earendil-works/pi-tui";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { renderStatusBar, type BarModel } from "../lib/ui/bars.ts";
import { barWindows, NAN, NAN_QUOTA_URL, parseNanQuota, resetDay, selectLimit, usageFromCodexEvent, usageFromHeaders, usageRows, UsageTracker, USAGE_SOURCE_EVENT, USAGE_SOURCE_SCHEMA, usageTimeoutMs, type ProviderUsage } from "../lib/ui/usage.ts";
import { installUsage } from "../lib/ui/usage-shell.ts";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { UsageOverlay } from "../lib/ui/usage-view.ts";
import { stripAnsi } from "../lib/ui/primitives/index.ts";
import jhonShellUi from "../extensions/ui.ts";
import { assertRows, plainTheme, spread } from "./rows.ts";
import { useMemorySettings } from "./fake-jhon-settings.ts";

const HOUR = 3_600_000;
// Thursday 19 November 2026, 14:00 local time.
const THU_14 = new Date(2026, 10, 19, 14, 0).getTime();
const NOW = THU_14 - 2 * 24 * HOUR;

const acme = (overrides: Partial<BarModel> = {}): BarModel => ({
	cwd: "/home/dev/acme_app", branch: "feat/add-invoice-export", sandbox: "on",
	modelId: "opus-5-5", effort: "high", contextPercent: 38, contextWindow: 1_000_000, costTotal: 1.42, subscription: false,
	...overrides,
});

const claude = (fiveHour: number, week: number): ProviderUsage => usageFromHeaders({
	"anthropic-ratelimit-unified-status": "allowed",
	"anthropic-ratelimit-unified-5h-utilization": String(fiveHour / 100),
	"anthropic-ratelimit-unified-5h-reset": String(Math.floor((NOW + 3 * HOUR) / 1000)),
	"anthropic-ratelimit-unified-7d-utilization": String(week / 100),
	"anthropic-ratelimit-unified-7d-reset": String(Math.floor(THU_14 / 1000)),
}, NOW)!;

test("Claude's reply headers give the 5h and week windows, used percent and reset; a reply without them gives none", () => {
	const usage = claude(12, 23);
	assert.equal(usage.provider, "anthropic");
	assert.deepEqual(usage.limits.map((limit) => limit.name), ["claude"]);
	assert.deepEqual(usage.limits[0]!.windows.map((w) => [w.label, Math.round(w.usedPercent), w.windowSeconds, w.resetAt]), [
		["5h", 12, 18_000, Math.floor((NOW + 3 * HOUR) / 1000) * 1000],
		["week", 23, 604_800, THU_14],
	]);
	assert.equal(usageFromHeaders({ "anthropic-ratelimit-unified-5h-utilization": "0.5" }, NOW, "claude-bridge")?.provider, "claude-bridge", "the bridge re-sends the same headers under its own provider");
	assert.equal(usageFromHeaders({ "content-type": "text/event-stream" }, NOW), undefined);
});

test("ChatGPT's windows come from the x-codex-* headers or the codex.rate_limits event; another family's event is not ours", () => {
	const fromHeaders = usageFromHeaders({
		"x-codex-primary-used-percent": "84", "x-codex-primary-window-minutes": "300", "x-codex-primary-reset-at": "1790000000",
		"x-codex-secondary-used-percent": "40", "x-codex-secondary-window-minutes": "10080",
	}, NOW)!;
	assert.equal(fromHeaders.provider, "openai-codex");
	assert.deepEqual(fromHeaders.limits[0]!.windows.map((w) => [w.label, w.usedPercent, w.resetAt]), [["5h", 84, 1_790_000_000_000], ["week", 40, null]]);
	const event = { type: "codex.rate_limits", plan_type: "plus", rate_limits: { primary: { used_percent: 9, window_minutes: 300, reset_at: 1_790_000_000 }, secondary: { used_percent: 51, window_minutes: 10_080 } } };
	const fromEvent = usageFromCodexEvent(event, NOW)!;
	assert.equal(fromEvent.plan, "plus");
	assert.deepEqual(fromEvent.limits[0]!.windows.map((w) => [w.label, w.usedPercent]), [["5h", 9], ["week", 51]]);
	assert.equal(usageFromCodexEvent({ ...event, limit_name: "other" }, NOW), undefined);
	assert.equal(usageFromCodexEvent({ type: "response.created" }, NOW), undefined);
});

test("the bar's windows: the week always, the 5h only from 70%; the reset sits on the more urgent one, the 5h on a tie", () => {
	assert.equal(resetDay(THU_14), "Thu 14:00");
	assert.deepEqual(barWindows(claude(12, 23)).map((w) => [w.label, Math.round(w.percent), w.reset]), [["week", 23, true]]);
	assert.deepEqual(barWindows(claude(72, 23)).map((w) => [w.label, Math.round(w.percent), w.reset]), [["5h", 72, true], ["week", 23, false]]);
	assert.deepEqual(barWindows(claude(72, 93)).map((w) => [w.label, Math.round(w.percent), w.reset]), [["5h", 72, false], ["week", 93, true]]);
	assert.deepEqual(barWindows(claude(75, 80)).map((w) => [w.label, w.reset]), [["5h", true], ["week", false]], "both warn: the 5h bites first");
	assert.deepEqual(barWindows(undefined), []);
});

test("the status bar shows the week's meter, % and reset before the cost, at 180 and in the narrow form at 100", () => {
	const usage = barWindows(claude(12, 23));
	assertRows((width, theme) => [renderStatusBar(acme({ usage }), theme, width)], {
		180: [spread(" ⎇ feat/add-invoice-export │ sandbox on", "opus-5-5 · high │ ctx ▰▰▰▱▱▱▱▱ 38% │ week ▰▰▱▱▱▱▱▱ 23% · resets Thu 14:00 │ $1.42 ", 180)],
		100: [spread(" ⎇ feat/add-invoice-… │ sandbox on", "opus-5-5 · high │ ctx 38% │ week 23% · resets Thu 14:00 │ $1.42 ", 100)],
	}, "status bar with the week");
	assert.equal(stripAnsi(renderStatusBar(acme({ usage: barWindows(claude(72, 23)) }), plainTheme, 180)),
		spread(" ⎇ feat/add-invoice-export │ sandbox on", "opus-5-5 · high │ ctx ▰▰▰▱▱▱▱▱ 38% │ 5h 72% · resets Thu 14:00 │ week ▰▰▱▱▱▱▱▱ 23% │ $1.42 ", 180).replace("Thu 14:00", resetDay(Math.floor((NOW + 3 * HOUR) / 1000) * 1000)),
		"from 70% the 5h window joins, carrying the reset");
	const tagged = { fg: (role: string, text: string) => `<${role}>${text}</${role}>`, bold: (text: string) => text };
	const roles = renderStatusBar(acme({ usage: barWindows(claude(12, 93)) }), tagged, 180);
	assert.match(roles, /<dim>week<\/dim> <error>▰{7}<\/error><border>▱<\/border> <error>93%<\/error><dim> · resets Thu 14:00<\/dim>/, "the label dim, the meter and % in the one ladder's tone, the reset dim");
});

test("truncation with the windows: the reset, then the week's meter, then the second window, then the windows go, before model · effort; the cost stays last", (t) => {
	useMemorySettings(t, {});
	const data = acme({ usage: barWindows(claude(72, 23)) });
	const facts: Array<[string, RegExp]> = [["resets", /resets/], ["week meter", /week ▰/], ["second window", /week/], ["windows", /5h 72%/], ["model", /opus-5-5/], ["cost", /\$1\.42/]];
	let gone = 0;
	for (let width = 240; width >= 8; width--) {
		const row = stripAnsi(renderStatusBar(data, plainTheme, width, { narrow: false }));
		assert.equal(visibleWidth(row), width, `${width}: ${row}`);
		const shown = facts.map(([, fact]) => fact.test(row));
		while (gone < facts.length && !shown[gone]) gone++;
		for (let i = 0; i < facts.length - 1; i++) assert.equal(shown[i], i >= gone, `${width}: ${facts[i]![0]} ${i < gone ? "came back" : "went out of order"}: ${JSON.stringify(row)}`);
		assert.match(row, /\$1\.42/, `${width}: the cost stays`);
	}
	assert.ok(gone >= 5, "the windows and the model went before the cost");
});

/** A registered source's fetch, counted; it answers after `delay` ms (never when Infinity). */
function source(provider: string, delay = 0, usedPercent = 40) {
	const calls: Array<{ apiKey: string | undefined }> = [];
	return {
		calls,
		payload: {
			schema: USAGE_SOURCE_SCHEMA, provider,
			fetch: (apiKey: string | undefined, _fetch: typeof fetch, now: number) => {
				calls.push({ apiKey });
				const answer = { provider, plan: "pro", fetchedAt: now, limits: [{ name: provider, limitReached: false, windows: [{ label: "week", usedPercent, windowSeconds: 604_800, resetAt: null }] }] };
				return delay === Infinity ? new Promise(() => {}) : new Promise((resolve) => setTimeout(() => resolve(answer), delay));
			},
		},
	};
}

test("a registered source is fetched at most every 5 minutes unless forced, with the provider's key; a fetch past the timeout fails and its late answer is dropped", async () => {
	let now = NOW;
	const settled: string[] = [];
	const tracker = new UsageTracker({ now: () => now, fetch: globalThis.fetch, timeoutMs: 30, apiKey: async (provider) => `key-${provider}`, onSettled: () => settled.push("settled") });
	const quick = source("acme");
	assert.ok(tracker.register(quick.payload), "a source registers");
	assert.equal(tracker.register({ schema: "other", provider: "x", fetch: () => undefined }), false, "a foreign shape is ignored");
	await tracker.refresh(["acme", "anthropic"], false);
	assert.equal(quick.calls.length, 1);
	assert.equal(quick.calls[0]!.apiKey, "key-acme");
	assert.equal(tracker.get("acme")?.limits[0]?.windows[0]?.usedPercent, 40);
	now += 4 * 60_000;
	await tracker.refresh(["acme"], false);
	assert.equal(quick.calls.length, 1, "within 5 minutes a turn end does not fetch again");
	await tracker.refresh(["acme"], true);
	assert.equal(quick.calls.length, 2, "r forces it");
	now += 6 * 60_000;
	await tracker.refresh(["acme"], false);
	assert.equal(quick.calls.length, 3, "after 5 minutes it fetches again");
	const stuck = source("slow", Infinity);
	tracker.register(stuck.payload);
	await tracker.refresh(["slow"], true);
	assert.equal(tracker.failed("slow"), true, "past the timeout the provider failed");
	assert.equal(tracker.get("slow"), undefined);
	assert.ok(settled.length >= 4, "each settle repaints");
	assert.equal(usageTimeoutMs({}), 10_000);
	assert.equal(usageTimeoutMs({ JHON_SHELL_USAGE_TIMEOUT_MS: "250" }), 250);
	assert.equal(usageTimeoutMs({ JHON_SHELL_USAGE_TIMEOUT_MS: "soon" }), 10_000);
});

test("the overlay: Subscriptions, each provider's windows with meter, % and reset, the session's marked; r refreshes, esc or q closes", async () => {
	let closed = 0;
	let refreshed = 0;
	const tracker = new UsageTracker({ now: () => NOW, fetch: globalThis.fetch, timeoutMs: 100, apiKey: async () => undefined, onSettled: () => {} });
	tracker.record(claude(12, 84));
	const overlay = new UsageOverlay({
		tracker, now: () => NOW, theme: plainTheme, scope: () => ["anthropic", "openai-codex"], active: () => "anthropic",
		refresh: async () => { refreshed++; }, close: () => { closed++; }, requestRender: () => {},
	});
	const rows = overlay.render(80).map(stripAnsi);
	assert.ok(rows.every((row) => visibleWidth(row) === 80), "every row exactly the width");
	assert.match(rows[0]!, /^╭─+╮$/);
	assert.match(rows[1]!, /^│ Subscriptions +│$/);
	assert.match(rows[3]!, /^│ 力 anthropic · updated just now +│$/);
	assert.match(rows[4]!, /^│ {3}claude 5h +▰▰▱▱▱▱▱▱▱▱▱▱▱▱▱▱ +12% · resets in 3h 0m +│$/);
	assert.match(rows[5]!, /^│ {3}claude week +▰{13}▱{3} +84% · resets in 2d 0h +│$/);
	assert.match(rows[6]!, /^│ {3}⊘ anthropic is at 84% of claude week; resets in 2d\. +│$/);
	assert.match(rows[7]!, /^│ openai-codex · usage arrives with the first reply +│$/);
	assert.match(rows.at(-2)!, /^│ r refresh {2}esc close +│$/);
	assert.match(rows.at(-1)!, /^╰─+╯$/);
	overlay.handleInput("r");
	await new Promise((resolve) => setImmediate(resolve));
	assert.equal(refreshed, 1);
	overlay.handleInput("q");
	overlay.handleInput("\x1b");
	assert.equal(closed, 2);
	overlay.handleInput("x");
	assert.equal(refreshed + closed, 3, "other keys do nothing");
});

type Handler = (event: unknown, ctx: ExtensionContext) => unknown;

function fakePi() {
	const handlers = new Map<string, Handler[]>();
	const commands = new Map<string, { handler(args: string, ctx: ExtensionContext): Promise<void> }>();
	const shortcuts = new Map<string, { handler(ctx: ExtensionContext): Promise<void> }>();
	const listeners = new Map<string, Array<(data: unknown) => void>>();
	const pi = {
		on(event: string, handler: Handler) { handlers.set(event, [...(handlers.get(event) ?? []), handler]); },
		appendEntry() {},
		events: { on(name: string, fn: (data: unknown) => void) { listeners.set(name, [...(listeners.get(name) ?? []), fn]); return () => {}; }, emit(name: string, data: unknown) { for (const fn of listeners.get(name) ?? []) fn(data); } },
		registerTool() {}, registerMessageRenderer() {}, sendMessage() {},
		registerCommand(name: string, registration: never) { commands.set(name, registration); },
		registerShortcut(key: string, registration: never) { shortcuts.set(key, registration); },
		getThinkingLevel: () => "high",
	} as unknown as ExtensionAPI;
	const fire = async (event: string, ctx: ExtensionContext, payload: unknown = {}) => { for (const handler of handlers.get(event) ?? []) await handler(payload, ctx); };
	return { pi, commands, shortcuts, fire, emit: (name: string, data: unknown) => pi.events.emit(name, data) };
}

function fakeCtx() {
	const ui = { footer: undefined as unknown, overlay: undefined as undefined | { render(width: number): string[]; handleInput(data: string): void }, done: 0 };
	const ctx = {
		hasUI: true, mode: "tui", cwd: "/repo",
		model: { id: "claude-opus-5-5", provider: "anthropic", reasoning: true, contextWindow: 200_000 },
		hasPendingMessages: () => false, isIdle: () => true, abort() {},
		sessionManager: { getCwd: () => "/repo", getEntries: () => [], getBranch: () => [], getSessionId: () => "usage-session" },
		modelRegistry: { isUsingOAuth: () => true, getApiKeyForProvider: async () => undefined, getAll: () => [] },
		getContextUsage: () => ({ tokens: 20_000, contextWindow: 200_000, percent: 10 }),
		ui: {
			theme: plainTheme,
			setFooter(factory: unknown) { ui.footer = factory; },
			setWidget() {}, setEditorComponent() {}, getEditorComponent: () => undefined, setWorkingVisible() {}, notify() {},
			custom(factory: (tui: unknown, theme: unknown, keys: unknown, done: (value: unknown) => void) => { render(width: number): string[]; handleInput(data: string): void }) {
				return new Promise((resolve) => { ui.overlay = factory({ requestRender() {}, terminal: { rows: 40, columns: 120 } }, plainTheme, {}, (value) => { ui.done++; resolve(value); }); });
			},
		},
	} as unknown as ExtensionContext;
	return { ctx, ui };
}

const footerData = { getGitBranch: () => "main", getExtensionStatuses: () => new Map(), getAvailableProviderCount: () => 1, onBranchChange: () => () => {} };

test("/jhon:usage and alt+u open the overlay; a reply's headers reach the status bar; shell.keys.usage off registers no key", async (t) => {
	useMemorySettings(t, {});
	const { pi, commands, shortcuts, fire } = fakePi();
	jhonShellUi(pi, { JHON_SHELL: "1" }, { lddStatus: async () => undefined, now: () => NOW });
	assert.ok(commands.has("jhon:usage"), "/jhon:usage is registered");
	assert.ok(shortcuts.has("alt+u"), "alt+u is registered");
	const { ctx, ui } = fakeCtx();
	await fire("session_start", ctx);
	await fire("after_provider_response", ctx, { type: "after_provider_response", status: 200, headers: {
		"anthropic-ratelimit-unified-7d-utilization": "0.23", "anthropic-ratelimit-unified-7d-reset": String(Math.floor(THU_14 / 1000)),
	} });
	const footer = (ui.footer as (tui: unknown, theme: unknown, data: unknown) => { render(width: number): string[] })({ requestRender() {} }, plainTheme, footerData);
	assert.match(stripAnsi(footer.render(180)[0]!), /│ week ▰▰▱▱▱▱▱▱ 23% · resets Thu 14:00 │ \$0\.000 sub $/);
	void shortcuts.get("alt+u")!.handler(ctx);
	await new Promise((resolve) => setImmediate(resolve));
	assert.ok(ui.overlay, "alt+u opened the overlay");
	assert.ok(ui.overlay!.render(80).map(stripAnsi).some((row) => /claude week/.test(row)), "it shows the reply's windows");
	ui.overlay!.handleInput("q");
	assert.equal(ui.done, 1, "q closed it");
	void commands.get("jhon:usage")!.handler("", ctx);
	await new Promise((resolve) => setImmediate(resolve));
	ui.overlay!.handleInput("\x1b");
	assert.equal(ui.done, 2, "esc closed /jhon:usage's overlay");

	useMemorySettings(t, { "shell.keys.usage": "off" });
	const off = fakePi();
	jhonShellUi(off.pi, { JHON_SHELL: "1" }, { lddStatus: async () => undefined });
	assert.ok(off.commands.has("jhon:usage"), "the command stays");
	assert.ok(![...off.shortcuts.keys()].includes("alt+u"), "no key with shell.keys.usage off");
});

test("another extension's usage source registers on pi.events and is fetched for the session's provider", async (t) => {
	useMemorySettings(t, {});
	const { pi, fire, emit } = fakePi();
	jhonShellUi(pi, { JHON_SHELL: "1" }, { lddStatus: async () => undefined, now: () => NOW });
	const { ctx } = fakeCtx();
	(ctx as unknown as { model: { provider: string } }).model.provider = "acme";
	await fire("session_start", ctx);
	const acmeSource = source("acme");
	emit(USAGE_SOURCE_EVENT, acmeSource.payload);
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(acmeSource.calls.length, 1, "a registration for the session's provider fetches once");
});

// ---------------------------------------------------------------- T174-S5b part 2: B's parity

const NAN_PAYLOAD = {
	periodEnd: "2026-12-01T00:00:00Z",
	models: [
		{ model: "glm5.2", cap: 500, fullCap: 1_000, tokensUsed: 600, periodEnd: "2026-11-25T00:00:00Z" },
		{ model: "glm5.3-flash", cap: 3_000, tokensUsed: 300 },
		{ model: "kimi-k2", cap: 1_000, tokensUsed: 100, windowTokensUsed: 50, windowTokens: 200, windowHours: 4, windowResetsAt: Math.floor((NOW + HOUR) / 1000) },
		{ model: "no-meter" },
	],
};

/** A fetch that answers payload as JSON and keeps every call; no network. */
function nanFetch(payload: unknown, status = 200) {
	const calls: Array<{ url: string; init: RequestInit | undefined }> = [];
	const fetchFn = (async (input: string | URL | Request, init?: RequestInit) => {
		calls.push({ url: String(input), init });
		return new Response(JSON.stringify(payload), { status });
	}) as typeof fetch;
	return { calls, fetchFn };
}

test("NaN: the tracker fetches its quota with the key pi holds, at its fixed origin, refusing redirects; a model with no allowance is left out", async () => {
	const { calls, fetchFn } = nanFetch(NAN_PAYLOAD);
	let settled = 0;
	const tracker = new UsageTracker({ now: () => NOW, fetch: fetchFn, timeoutMs: 1_000, apiKey: async (provider) => (provider === NAN ? "nan-key" : undefined), onSettled: () => { settled++; } });
	assert.equal(tracker.note(NAN), "not fetched yet · r fetches", "nan is the shell's own source");
	await tracker.refresh([NAN], true);
	assert.equal(calls.length, 1);
	assert.equal(calls[0]!.url, NAN_QUOTA_URL);
	assert.equal(calls[0]!.init?.redirect, "error");
	assert.equal(calls[0]!.init?.cache, "no-store");
	assert.equal((calls[0]!.init?.headers as Record<string, string>).Authorization, "Bearer nan-key");
	assert.equal(settled, 1);
	const usage = tracker.get(NAN)!;
	assert.deepEqual(usage.limits.map((limit) => [limit.name, limit.windows.map((w) => [w.label, Math.round(w.usedPercent)])]), [
		["glm5.2", [["", 60]]], ["glm5.3-flash", [["", 10]]], ["kimi-k2", [["", 10], ["4h", 25]]],
	], "the full-period cap, not a first period's prorated one");
	assert.deepEqual(parseNanQuota({ models: [{ model: "glm5.2", cap: 10 }] }, NOW), undefined, "a metered model whose use cannot be read refuses the whole answer");
	const failing = new UsageTracker({ now: () => NOW, fetch: nanFetch({}, 401).fetchFn, timeoutMs: 1_000, apiKey: async () => "nan-key", onSettled() {} });
	await failing.refresh([NAN], true);
	assert.equal(failing.failed(NAN), true, "a refused fetch is a failure to show");
});

test("NaN on the bar: the session's model's own allowance, else its family's total, else the account's, each a share of the summed allowances", () => {
	const usage = parseNanQuota(NAN_PAYLOAD, NOW)!;
	const bar = (model: string) => barWindows(usage, model).map((w) => [w.label, Math.round(w.percent * 10) / 10, w.reset]);
	assert.deepEqual(bar("glm5.2"), [["glm5.2", 60, true]], "its own, with its reset");
	assert.deepEqual(bar("glm5.4"), [["glm total", 22.5, false]], "glm: 900 of 4000 tokens, no reset: each model closes its own period");
	assert.deepEqual(bar("qwen3"), [["nan total", 20, false]], "the account: 1000 of 5000");
	assert.deepEqual(selectLimit(claude(10, 20), "anything")?.name, "claude", "a provider without allowances keeps its first limit");
});

test("NaN in the overlay: a family together, the most used family and member first; no total is a row", () => {
	const tracker = new UsageTracker({ now: () => NOW, fetch: globalThis.fetch, timeoutMs: 100, apiKey: async () => undefined, onSettled() {} });
	tracker.record(parseNanQuota({ models: [
		{ model: "kimi-k2", cap: 1_000, tokensUsed: 400 },
		{ model: "glm5.3-flash", cap: 1_000, tokensUsed: 100 },
		{ model: "glm5.2", cap: 1_000, tokensUsed: 900 },
	] }, NOW));
	const rows = usageRows({ scope: [NAN], active: NAN, tracker, now: NOW }, plainTheme, 90).map(stripAnsi);
	assert.deepEqual(rows.slice(1, 4).map((row) => row.trim().split(" ")[0]), ["glm5.2", "glm5.3-flash", "kimi-k2"], "glm (1000 of 2000, 50%) before kimi (40%), though kimi came first; glm5.2 (90%) before glm5.3-flash (10%)");
	assert.ok(!rows.some((row) => /total/.test(row)));
});

test("the overlay's key hints are controls: the pointer paints one, a click does what its key does", async () => {
	let closed = 0;
	let refreshed = 0;
	const tracker = new UsageTracker({ now: () => NOW, fetch: globalThis.fetch, timeoutMs: 100, apiKey: async () => undefined, onSettled() {} });
	const tagged = { ...plainTheme, fg: (role: string, text: string) => (role === "warning" ? `«${text}»` : text) };
	const overlay = new UsageOverlay({ tracker, now: () => NOW, theme: tagged, scope: () => [], active: () => undefined, refresh: async () => { refreshed++; }, close: () => { closed++; }, requestRender() {} });
	const hints = overlay.render(80).length - 2;
	assert.equal(stripAnsi(overlay.render(80)[hints]!).trim(), "│ r refresh  esc close                                                         │".trim());
	assert.deepEqual(overlay.handleMouse({ type: "move", button: "none", x: 13, y: hints } as never), { handled: true, render: true });
	assert.match(stripAnsi(overlay.render(80)[hints]!), /^│ r refresh {2}«esc» «close» +│$/, "esc close under the pointer");
	assert.deepEqual(overlay.handleMouse({ type: "press", button: "left", x: 3, y: hints } as never), { handled: true }, "it takes the press, so pi makes the click");
	overlay.handleMouse({ type: "click", button: "left", x: 3, y: hints } as never);
	await new Promise((resolve) => setImmediate(resolve));
	assert.equal(refreshed, 1, "r refresh refreshes");
	overlay.handleMouse({ type: "click", button: "left", x: 14, y: hints } as never);
	assert.equal(closed, 1, "esc close closes");
	assert.equal(overlay.handleMouse({ type: "click", button: "left", x: 30, y: hints } as never), undefined, "past the hints, nothing");
	overlay.handleMouse({ type: "move", button: "none", x: 30, y: 1 } as never);
	assert.doesNotMatch(stripAnsi(overlay.render(80)[hints]!), /«/, "off the hints, none painted");
});

test("T149: the overlay's providers follow a session-only profile's sub-agent models, read once the profile is known", async (t) => {
	const settings = useMemorySettings(t, {});
	const dir = mkdtempSync(join(tmpdir(), "jhon-usage-scope-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	mkdirSync(join(dir, "agents"));
	writeFileSync(join(dir, "agents", "jhon-builder.md"), "---\nname: jhon-builder\nmodel: anthropic/claude-opus-5-5\n---\nBuild.\n");
	let profile: string | undefined;
	let reads = 0;
	const { pi, commands, fire } = fakePi();
	const { ctx, ui } = fakeCtx();
	(ctx as unknown as { model: { provider: string } }).model.provider = "openai-codex";
	installUsage(pi, { PI_CODING_AGENT_DIR: dir }, {
		settings, now: () => NOW, fetch: nanFetch(NAN_PAYLOAD).fetchFn, current: () => ctx, repaint() {},
		sessionProfile: () => profile,
		profilesJson: async () => { reads++; return { profiles: [{ name: "jhon-nan", agents: [{ agent: "jhon-builder", runtime: "pi", model: "nan/glm5.2@high" }] }] }; },
	});
	await fire("session_start", ctx);
	const providers = async () => {
		void commands.get("jhon:usage")!.handler("", ctx);
		await new Promise((resolve) => setTimeout(resolve, 20));
		return ui.overlay!.render(100).map(stripAnsi).filter((row) => /^│ (力 )?[a-z]/.test(row) && !/Subscriptions|refresh/.test(row)).map((row) => row.slice(2).replace(/^力 /, "").split(" ")[0]);
	};
	assert.deepEqual(await providers(), ["openai-codex", "anthropic"], "the default: the agent file's model");
	profile = "jhon-nan";
	assert.deepEqual(await providers(), ["openai-codex", "nan"], "the session's profile: its row's model");
	assert.equal(reads, 1, "the profile's rows are read once");
});
