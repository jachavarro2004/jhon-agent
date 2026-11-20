import { truncateToWidth } from "@earendil-works/pi-tui";
import { paintGauge } from "./shell-gauge.ts";
import { glyph } from "./primitives/index.ts";

// jhon-shell subscription usage: the rate-limit windows each connected
// provider reports. Anthropic and Codex send them with each reply and NaN
// through its quota endpoint; all land in the same model. Parsing is pure and never
// keeps account details beyond the plan name.

export interface UsageWindow {
	label: string;
	usedPercent: number;
	windowSeconds: number;
	resetAt: number | null;
	// Raw allowance numbers, kept only by providers that report them (NaN).
	// Aggregates are weighted by budget, so averaging percentages is never
	// needed; nothing renders these fields directly.
	used?: number;
	budget?: number;
}

export interface UsageLimit {
	name: string;
	windows: UsageWindow[];
	limitReached: boolean;
}

export interface ProviderUsage {
	provider: string;
	plan: string | undefined;
	limits: UsageLimit[];
	fetchedAt: number;
}

export interface UsageTheme {
	fg(color: string, text: string): string;
}

interface RawNanModel {
	model?: unknown;
	cap?: unknown;
	fullCap?: unknown;
	tokensUsed?: unknown;
	periodEnd?: unknown;
	windowHours?: unknown;
	windowTokens?: unknown;
	fullWindowTokens?: unknown;
	windowTokensUsed?: unknown;
	windowResetsAt?: unknown;
}

interface RawNanQuota {
	models?: unknown;
	periodEnd?: unknown;
}

export const ANTHROPIC_PROVIDER = "anthropic";
// pi-claude-bridge runs Claude Code and re-emits its rate_limit_event as the
// same unified headers (src/rate-limit.ts), so its usage rides on replies too.
export const CLAUDE_BRIDGE_PROVIDER = "claude-bridge";
export const NAN_PROVIDER = "nan";
// A ChatGPT subscription, as pi-ai's openai-codex provider signs in to it.
export const OPENAI_CODEX_PROVIDER = "openai-codex";
const ANTHROPIC_MAIN_LIMIT = "claude";
// Codex CLI's own names (openai/codex, codex-rs/codex-api/src/rate_limits.rs):
// the default `codex` family's primary and secondary windows, used percent,
// window length in minutes and reset in Unix seconds, as SSE reply headers or
// as the WebSocket transport's `codex.rate_limits` event.
const CODEX_LIMIT = "codex";
const CODEX_HEADER_PREFIX = "x-codex-";
const CODEX_WINDOW_NAMES = ["primary", "secondary"] as const;
export const CODEX_RATE_LIMITS_EVENT = "codex.rate_limits";
const ANTHROPIC_PREFIX = "anthropic-ratelimit-unified-";
const ANTHROPIC_WINDOWS: ReadonlyArray<[key: string, seconds: number]> = [
	["5h", 18_000],
	["7d", 604_800],
];
// A per-model weekly window (seven_day_opus): the bridge passes a name it has no
// abbreviation for through as is. seven_day_overage_included (7d_oi) is the
// account week again, with overage counted, so it is not a model of its own.
const ANTHROPIC_MODEL_WEEK = /^anthropic-ratelimit-unified-(?:seven_day|7d)_([a-z][a-z0-9]*)-utilization$/;
const ANTHROPIC_NOT_A_MODEL = new Set(["oi", "overage"]);
// The NaN Cloud dashboard backend; not part of NaN's published OpenAPI, so the
// fetch that uses it is fixed-origin, redirect-refusing, and schema-validated.
export const NAN_QUOTA_URL = "https://cloud-api.nan.builders/api/usage/quota";
// The model's own allowance for the billing period carries no label: the model
// id names it in the bar, and the reset text says what the window is in the
// panel. Only a sub-window on top of it (a rolling `4h`) needs a name.
const NAN_PERIOD_LABEL = "";
// The dashboard's own published fallbacks for a model that reports rolling
// numbers without naming its budget.
const NAN_DEFAULT_WINDOW_TOKENS = 400_000_000;
const NAN_DEFAULT_WINDOW_HOURS = 4;
const PANEL_METER_CELLS = 16;
const MINUTE = 60;
const HOUR = 3600;
const DAY = 86_400;
const WEEK = 604_800;
const ROLE = {
	PROVIDER: "text",
	PLAN: "muted",
	LIMIT: "customMessageLabel",
	LABEL: "muted",
	PERCENT: "text",
	RESET: "dim",
	SEPARATOR: "muted",
} as const;
export const USAGE_EMPTY_MESSAGE = "No subscription usage yet. It arrives with the next reply, or press r to fetch it.";
const DEFAULT_PENDING_NOTE = "not fetched yet · r fetches";
const REPLY_PENDING_NOTE = "usage arrives with the first reply";
const PENDING_NOTE: Record<string, string> = {
	[NAN_PROVIDER]: DEFAULT_PENDING_NOTE,
	[ANTHROPIC_PROVIDER]: REPLY_PENDING_NOTE,
	[CLAUDE_BRIDGE_PROVIDER]: REPLY_PENDING_NOTE,
	[OPENAI_CODEX_PROVIDER]: REPLY_PENDING_NOTE,
};
const UNSUPPORTED_NOTE = "this provider does not report subscription usage";
// What a targeted provider with no snapshot says after a refresh actually ran
// and answered nothing. Generic on purpose: it names no endpoint, key, or
// account detail, exactly like the other notes here.
export const USAGE_FETCH_FAILED_NOTE = "the fetch failed · r to retry";

export interface ActiveProvider {
	provider: string;
}

// A generic hook so an extension holding its own provider (its own API token,
// its own usage endpoint) can plug a usage source into the shell without the
// shell ever knowing that provider's name. Emitted on `pi.events` as payload
// under `USAGE_SOURCE_EVENT`; jhon-shell validates the shape below and
// ignores anything else, so a malformed or foreign event never reaches a
// fetch call. Re-registration for the same provider replaces the previous
// source, so a second `session_start` emitting the same payload is a no-op
// in effect, not an accumulation.
export const USAGE_SOURCE_EVENT = "jhon-shell:usage-source/v1";
export const USAGE_SOURCE_SCHEMA = "jhon-shell.usage-source/v1";
const USAGE_SOURCE_PROVIDER_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

export interface UsageSource {
	schema: typeof USAGE_SOURCE_SCHEMA;
	provider: string;
	pendingNote?: string;
	fetch(apiKey: string | undefined, fetchFn: typeof fetch, now: number): Promise<ProviderUsage | undefined>;
}

// Pure and defensive: the payload crosses an event bus from another
// extension, so nothing here is trusted until every field is checked. Any
// mismatch returns undefined rather than throwing, exactly like the other
// payload parsers in this file.
export function parseUsageSource(value: unknown): UsageSource | undefined {
	if (!value || typeof value !== "object") return undefined;
	const raw = value as Record<string, unknown>;
	if (raw.schema !== USAGE_SOURCE_SCHEMA) return undefined;
	if (typeof raw.provider !== "string" || !USAGE_SOURCE_PROVIDER_PATTERN.test(raw.provider)) return undefined;
	if (typeof raw.fetch !== "function") return undefined;
	if (raw.pendingNote !== undefined && typeof raw.pendingNote !== "string") return undefined;
	const source: UsageSource = { schema: USAGE_SOURCE_SCHEMA, provider: raw.provider, fetch: raw.fetch as UsageSource["fetch"] };
	if (typeof raw.pendingNote === "string") source.pendingNote = raw.pendingNote;
	return source;
}

// One source per provider, most recent registration wins. Nothing here fetches
// or touches the network; it only remembers who to ask.
export class UsageSourceRegistry {
	private readonly sources = new Map<string, UsageSource>();

	register(source: UsageSource): void {
		this.sources.set(source.provider, source);
	}

	get(provider: string): UsageSource | undefined {
		return this.sources.get(provider);
	}

	has(provider: string): boolean {
		return this.sources.has(provider);
	}

	note(provider: string): string | undefined {
		const source = this.sources.get(provider);
		return source ? (source.pendingNote ?? DEFAULT_PENDING_NOTE) : undefined;
	}
}

export function providerNote(provider: string, registry?: UsageSourceRegistry): string {
	return PENDING_NOTE[provider] ?? registry?.note(provider) ?? UNSUPPORTED_NOTE;
}

function isFiniteNumber(value: unknown): value is number {
	return typeof value === "number" && Number.isFinite(value);
}

function parseSourceUsageWindow(value: unknown): UsageWindow | undefined {
	if (!value || typeof value !== "object") return undefined;
	const raw = value as Record<string, unknown>;
	if (typeof raw.label !== "string") return undefined;
	if (!isFiniteNumber(raw.usedPercent)) return undefined;
	if (!isFiniteNumber(raw.windowSeconds)) return undefined;
	if (raw.resetAt !== null && !isFiniteNumber(raw.resetAt)) return undefined;
	if (raw.used !== undefined && !isFiniteNumber(raw.used)) return undefined;
	if (raw.budget !== undefined && !isFiniteNumber(raw.budget)) return undefined;
	const window: UsageWindow = { label: raw.label, usedPercent: raw.usedPercent, windowSeconds: raw.windowSeconds, resetAt: raw.resetAt as number | null };
	if (raw.used !== undefined) window.used = raw.used as number;
	if (raw.budget !== undefined) window.budget = raw.budget as number;
	return window;
}

function parseSourceUsageLimit(value: unknown): UsageLimit | undefined {
	if (!value || typeof value !== "object") return undefined;
	const raw = value as Record<string, unknown>;
	if (typeof raw.name !== "string") return undefined;
	if (typeof raw.limitReached !== "boolean") return undefined;
	if (!Array.isArray(raw.windows)) return undefined;
	const windows: UsageWindow[] = [];
	for (const entry of raw.windows) {
		const window = parseSourceUsageWindow(entry);
		if (!window) return undefined;
		windows.push(window);
	}
	return { name: raw.name, limitReached: raw.limitReached, windows };
}

// A registered source's resolved value crosses the same trust boundary a
// parsed HTTP payload does: it is foreign code's own object, so it is
// validated field by field and never recorded by reference. Every accepted
// shape is rebuilt from scratch, so a source mutating its own object after
// returning it can never reach a snapshot jhon-shell already recorded.
export function parseProviderUsage(value: unknown, expectedProvider: string): ProviderUsage | undefined {
	if (!value || typeof value !== "object") return undefined;
	const raw = value as Record<string, unknown>;
	if (raw.provider !== expectedProvider) return undefined;
	if (raw.plan !== undefined && typeof raw.plan !== "string") return undefined;
	if (!isFiniteNumber(raw.fetchedAt)) return undefined;
	if (!Array.isArray(raw.limits)) return undefined;
	const limits: UsageLimit[] = [];
	for (const entry of raw.limits) {
		const limit = parseSourceUsageLimit(entry);
		if (!limit) return undefined;
		limits.push(limit);
	}
	return { provider: raw.provider, plan: typeof raw.plan === "string" ? raw.plan : undefined, limits, fetchedAt: raw.fetchedAt };
}

export function windowLabel(seconds: number): string {
	if (seconds === WEEK) return "week";
	if (seconds >= DAY && seconds % DAY === 0) return `${seconds / DAY}d`;
	if (seconds >= HOUR && seconds % HOUR === 0) return `${seconds / HOUR}h`;
	return `${Math.round(seconds / MINUTE)}m`;
}

export function formatReset(resetAt: number | null, now: number): string {
	if (resetAt === null) return "";
	const seconds = Math.floor((resetAt - now) / 1000);
	if (seconds <= 0) return "resets now";
	if (seconds < HOUR) return `resets in ${Math.max(1, Math.round(seconds / MINUTE))}m`;
	if (seconds < DAY) return `resets in ${Math.floor(seconds / HOUR)}h ${Math.floor((seconds % HOUR) / MINUTE)}m`;
	return `resets in ${Math.floor(seconds / DAY)}d ${Math.floor((seconds % DAY) / HOUR)}h`;
}

// Claude Pro/Max sends utilization as a 0..1 fraction per window and reset
// times in Unix seconds on every response; there is no usage endpoint.
function anthropicWindow(headers: Record<string, string>, key: string, seconds: number): UsageWindow | undefined {
	const fraction = Number.parseFloat(headers[`${ANTHROPIC_PREFIX}${key}-utilization`] ?? "");
	if (!Number.isFinite(fraction)) return undefined;
	const reset = Number.parseInt(headers[`${ANTHROPIC_PREFIX}${key}-reset`] ?? "", 10);
	return { label: windowLabel(seconds), usedPercent: fraction * 100, windowSeconds: seconds, resetAt: Number.isFinite(reset) ? reset * 1000 : null };
}

export function parseAnthropicHeaders(headers: Record<string, string>, now: number, provider: string = ANTHROPIC_PROVIDER): ProviderUsage | undefined {
	const status = headers[`${ANTHROPIC_PREFIX}status`];
	const limitReached = status === "rejected";
	const windows = ANTHROPIC_WINDOWS.map(([key, seconds]) => anthropicWindow(headers, key, seconds)).filter((window): window is UsageWindow => window !== undefined);
	const limits: UsageLimit[] = windows.length > 0 ? [{ name: ANTHROPIC_MAIN_LIMIT, windows, limitReached }] : [];
	for (const name of Object.keys(headers)) {
		const model = ANTHROPIC_MODEL_WEEK.exec(name)?.[1];
		if (!model || ANTHROPIC_NOT_A_MODEL.has(model)) continue;
		const key = name.slice(ANTHROPIC_PREFIX.length, -"-utilization".length);
		const window = anthropicWindow(headers, key, WEEK);
		if (window) limits.push({ name: model, windows: [window], limitReached: false });
	}
	if (limits.length === 0) return undefined;
	return { provider, plan: undefined, limits, fetchedAt: now };
}

// A number as Codex sends it: a JSON number, or a header's decimal text.
// Anything else is absent, never 0.
function codexNumber(value: unknown): number | undefined {
	const parsed = typeof value === "string" && value.trim() !== "" ? Number(value) : value;
	return typeof parsed === "number" && Number.isFinite(parsed) ? parsed : undefined;
}

// A window needs its used percent and its length: without either there is no
// number to show and no label to give it, so none is guessed.
function codexWindow(usedPercent: unknown, windowMinutes: unknown, resetAt: unknown): UsageWindow | undefined {
	const used = codexNumber(usedPercent);
	const minutes = codexNumber(windowMinutes);
	if (used === undefined || minutes === undefined || minutes <= 0) return undefined;
	const reset = codexNumber(resetAt);
	const seconds = minutes * MINUTE;
	return { label: windowLabel(seconds), usedPercent: used, windowSeconds: seconds, resetAt: reset !== undefined && reset > 0 ? reset * 1000 : null };
}

function codexUsage(windows: Array<UsageWindow | undefined>, plan: unknown, now: number, provider: string): ProviderUsage | undefined {
	const present = windows.filter((window): window is UsageWindow => window !== undefined);
	if (present.length === 0) return undefined;
	const limitReached = present.some((window) => window.usedPercent >= 100);
	return { provider, plan: typeof plan === "string" && plan ? plan : undefined, limits: [{ name: CODEX_LIMIT, windows: present, limitReached }], fetchedAt: now };
}

/** A ChatGPT subscription's windows from the x-codex-* headers of a Codex SSE reply; undefined when none is whole. */
export function parseCodexHeaders(headers: Record<string, string>, now: number, provider: string = OPENAI_CODEX_PROVIDER): ProviderUsage | undefined {
	const windows = CODEX_WINDOW_NAMES.map((name) => {
		const key = `${CODEX_HEADER_PREFIX}${name}`;
		return codexWindow(headers[`${key}-used-percent`], headers[`${key}-window-minutes`], headers[`${key}-reset-at`]);
	});
	return codexUsage(windows, undefined, now, provider);
}

/** The same windows from the Codex WebSocket transport's `codex.rate_limits` event; any other event, or another limit family's, is undefined. */
export function parseCodexRateLimitEvent(data: unknown, now: number, provider: string = OPENAI_CODEX_PROVIDER): ProviderUsage | undefined {
	if (!data || typeof data !== "object") return undefined;
	const event = data as { type?: unknown; plan_type?: unknown; rate_limits?: unknown; metered_limit_name?: unknown; limit_name?: unknown };
	if (event.type !== CODEX_RATE_LIMITS_EVENT) return undefined;
	const family = event.metered_limit_name ?? event.limit_name;
	if (family !== undefined && family !== null && family !== CODEX_LIMIT) return undefined;
	const limits = (event.rate_limits && typeof event.rate_limits === "object" ? event.rate_limits : {}) as Record<string, unknown>;
	const windows = CODEX_WINDOW_NAMES.map((name) => {
		const raw = limits[name];
		if (!raw || typeof raw !== "object") return undefined;
		const window = raw as { used_percent?: unknown; window_minutes?: unknown; reset_at?: unknown };
		return codexWindow(window.used_percent, window.window_minutes, window.reset_at);
	});
	return codexUsage(windows, event.plan_type, now, provider);
}

/** The snapshot belongs to the provider that answered: pi's event does not name it, the session's model does. */
export function parseUsageHeaders(headers: Record<string, string>, now: number, provider?: string): ProviderUsage | undefined {
	return parseAnthropicHeaders(headers, now, provider ?? ANTHROPIC_PROVIDER) ?? parseCodexHeaders(headers, now, provider ?? OPENAI_CODEX_PROVIDER);
}

// NaN Cloud reports one allowance per model for the billing period, plus the
// rolling window the model applies on top of it. Percentages follow the
// dashboard exactly: tokens used over the period allowance, and window tokens
// over the full window budget. Every field is optional, because this payload
// lives outside NaN's published contract.
function quotaTimestamp(value: unknown): number | null {
	if (typeof value === "number" && Number.isFinite(value) && value > 0) return value * 1000;
	if (typeof value !== "string") return null;
	const parsed = Date.parse(value);
	return Number.isNaN(parsed) ? null : parsed;
}

function quotaNumber(value: unknown, positive: boolean): number | undefined {
	if (typeof value !== "number" || !Number.isFinite(value)) return undefined;
	return positive ? (value > 0 ? value : undefined) : value >= 0 ? value : undefined;
}

function nanRollingWindow(raw: RawNanModel): UsageWindow | undefined {
	const used = quotaNumber(raw.windowTokensUsed, false);
	if (used === undefined) return undefined;
	const budget = quotaNumber(raw.fullWindowTokens, true) ?? quotaNumber(raw.windowTokens, true) ?? NAN_DEFAULT_WINDOW_TOKENS;
	const hours = quotaNumber(raw.windowHours, true) ?? NAN_DEFAULT_WINDOW_HOURS;
	return { label: windowLabel(hours * HOUR), usedPercent: (used / budget) * 100, windowSeconds: hours * HOUR, resetAt: quotaTimestamp(raw.windowResetsAt) };
}

// The allowance the dashboard divides by is the full-period cap, because `cap`
// is the allowance of the period in progress and comes back prorated on a first
// period. A model that reports neither figure reports no allowance at all, which
// is a state the surfaces already know how to draw nothing for.
function nanEffectiveAllowance(raw: RawNanModel): number | undefined {
	return quotaNumber(raw.fullCap, true) ?? quotaNumber(raw.cap, true);
}

// One allowance per metered model, weighted by that model's own cap. The raw
// numbers travel with the period window so the bar and the panel can aggregate
// without ever averaging percentages.
function nanPeriodWindow(tokensUsed: number, cap: number, resetAt: number | null, now: number): UsageWindow {
	return {
		label: NAN_PERIOD_LABEL,
		usedPercent: (tokensUsed / cap) * 100,
		windowSeconds: resetAt === null ? 0 : Math.max(0, Math.round((resetAt - now) / 1000)),
		resetAt,
		used: tokensUsed,
		budget: cap,
	};
}

export function parseNanQuota(payload: unknown, now: number): ProviderUsage {
	const raw = (payload ?? {}) as RawNanQuota;
	const fallbackResetAt = quotaTimestamp(raw.periodEnd);
	const limits: UsageLimit[] = [];
	if (Array.isArray(raw.models)) {
		for (const entry of raw.models) {
			if (!entry || typeof entry !== "object") continue;
			const model = entry as RawNanModel;
			if (typeof model.model !== "string" || model.model.length === 0) continue;
			const allowance = nanEffectiveAllowance(model);
			// A model that reports no allowance is not drift — the dashboard draws
			// nothing for it either, and the live payload carries such entries. A metered
			// allowance whose usage cannot be read is drift: a partial snapshot would
			// understate every aggregate it feeds, so the read fails whole and the last
			// valid snapshot survives instead.
			if (allowance === undefined) continue;
			const tokensUsed = quotaNumber(model.tokensUsed, false);
			if (tokensUsed === undefined) return { provider: NAN_PROVIDER, plan: undefined, limits: [], fetchedAt: now };
			const resetAt = quotaTimestamp(model.periodEnd) ?? fallbackResetAt;
			const windows: UsageWindow[] = [nanPeriodWindow(tokensUsed, allowance, resetAt, now)];
			const rolling = nanRollingWindow(model);
			if (rolling) windows.push(rolling);
			limits.push({ name: model.model, windows, limitReached: tokensUsed >= allowance });
		}
	}
	return { provider: NAN_PROVIDER, plan: undefined, limits, fetchedAt: now };
}

// Aggregation. NaN reports one allowance per metered model and the payload
// order is the server's business, so surfaces pick by meaning, not by position.
function rawAllowance(limit: UsageLimit): UsageWindow | undefined {
	const [first] = limit.windows;
	if (!first || first.used === undefined || first.budget === undefined) return undefined;
	return first.budget > 0 ? first : undefined;
}

// The leading alphabetic run of a model id: glm5.3-flash and glm5.2 are both
// "glm". Derived from the id the payload reports, never from a vendor list.
export function modelFamily(modelId: string): string {
	return (/^[a-z]+/i.exec(modelId)?.[0] ?? modelId).toLowerCase();
}

// Only NaN carries raw allowance numbers, so this one gate is what keeps
// Anthropic on exactly the rows and the meter it had before. One metered
// model is still a payload that carries them: the gate answers "does this
// provider report allowances", never "are there enough rows to sort", because
// a single allowance read as "no allowances" sent the bar back to whichever
// model the payload listed first.
export function allowanceGroupsSupported(limits: readonly UsageLimit[]): boolean {
	return limits.length > 0 && limits.every((limit) => rawAllowance(limit) !== undefined);
}

function percentOf(limit: UsageLimit): number {
	return limit.windows[0]?.usedPercent ?? 0;
}

const GROUP_SUFFIX = " total";

// A group is an allowance share, never an average of shares: Σused / Σbudget.
// It carries no reset, because its members close their own billing period on
// their own date, and a single reset would be a lie.
function allowanceTotal(name: string, limits: readonly UsageLimit[]): UsageLimit | undefined {
	const windows = limits.map(rawAllowance).filter((window): window is UsageWindow => window !== undefined);
	if (windows.length === 0 || windows.length !== limits.length) return undefined;
	const used = windows.reduce((total, window) => total + (window.used ?? 0), 0);
	const budget = windows.reduce((total, window) => total + (window.budget ?? 0), 0);
	if (budget <= 0) return undefined;
	return {
		name,
		windows: [{ label: NAN_PERIOD_LABEL, usedPercent: (used / budget) * 100, windowSeconds: 0, resetAt: null }],
		limitReached: limits.some((limit) => limit.limitReached),
	};
}

// What the grouping is for now that the totals are gone: the order. A family
// stays together, families sort by what they consume and the members inside one
// follow the same rule, most used first. The account and family totals are the
// bar's fallback ladder only — they are never rows, because a total nobody can
// act on only costs space. Every row is a limit block, so nothing here
// introduces a shape the other providers do not already use.
export function groupUsageLimits(limits: readonly UsageLimit[]): UsageLimit[] {
	if (!allowanceGroupsSupported(limits)) return [...limits];
	const order: string[] = [];
	const members = new Map<string, UsageLimit[]>();
	for (const limit of limits) {
		const family = modelFamily(limit.name);
		if (!members.has(family)) {
			members.set(family, []);
			order.push(family);
		}
		members.get(family)?.push(limit);
	}
	return order
		.map((family) => {
			const sorted = [...(members.get(family) ?? [])].sort((a, b) => percentOf(b) - percentOf(a));
			const total = allowanceTotal(`${family}${GROUP_SUFFIX}`, sorted);
			return { percent: total?.windows[0]?.usedPercent ?? percentOf(sorted[0]), sorted };
		})
		.sort((a, b) => b.percent - a.percent)
		.flatMap((family) => family.sorted);
}

// The bar follows the model the session is using: exact allowance, then its
// family, then the account total, then the first limit (which is what every
// provider without raw numbers keeps using, and what a missing model keeps).
export function selectUsageLimit(usage: ProviderUsage, activeModelId?: string): UsageLimit | undefined {
	if (!activeModelId) return usage.limits[0];
	const exact = usage.limits.find((limit) => limit.name === activeModelId);
	if (exact) return exact;
	if (allowanceGroupsSupported(usage.limits)) {
		const family = modelFamily(activeModelId);
		const members = usage.limits.filter((limit) => modelFamily(limit.name) === family);
		// The family rung is about the name of the meter, not about printing a row,
		// so a single member counts: its family is a closer statement of what the
		// session is drawing from than the whole account.
		if (members.length > 0) {
			const total = allowanceTotal(`${family}${GROUP_SUFFIX}`, members);
			if (total) return total;
		}
		const account = allowanceTotal(`${usage.provider}${GROUP_SUFFIX}`, usage.limits);
		if (account) return account;
	}
	return usage.limits[0];
}

function paintMeter(percent: number, cells: number, theme: UsageTheme): string {
	return paintGauge(percent, theme, cells);
}

function updatedAgo(fetchedAt: number, now: number): string {
	const minutes = Math.floor((now - fetchedAt) / 60_000);
	return minutes < 1 ? "updated just now" : `updated ${minutes}m ago`;
}

// The providers one refresh targets: the session's own provider first, then
// every provider the active profile's subagent routing names, deduplicated in
// first-seen order. A qualified ref ("provider/id") resolves directly; a bare
// model id resolves through the registry callback only when that callback can
// name exactly one provider, so an ambiguous or unknown ref never guesses one
// and is dropped instead.
export function usageScopeProviders(
	mainProvider: string | undefined,
	routingModels: ReadonlyArray<string | undefined> | undefined,
	resolveBareModel?: (modelId: string) => string | undefined,
): string[] {
	const providers = new Set<string>();
	if (mainProvider) providers.add(mainProvider);
	for (const model of routingModels ?? []) {
		const ref = parseModelRef(model);
		if (!ref) continue;
		const provider = ref.provider ?? resolveBareModel?.(ref.id);
		if (provider) providers.add(provider);
	}
	return [...providers];
}

// The current routing scope the panel draws: which providers are targeted, and
// which of them failed their latest settled refresh. A provider recorded
// earlier but no longer targeted is simply absent — it is not current scope.
export interface UsagePanelScope {
	providers: readonly string[];
	// Actual failures only: a refresh that settled without a snapshot. A
	// provider still fetching is never in here, so an in-flight refresh cannot
	// read as failed, and a successful refresh removes the provider again.
	failed: ReadonlySet<string>;
}

// The active provider comes first, marked with the petal, and explains
// itself when it has no data yet. Other providers seen this session follow.
// With a scope, the panel draws exactly the targeted providers in scope order:
// one with no snapshot explains itself (its latest refresh failed, or its
// pending note otherwise), one whose latest refresh failed after a good
// snapshot keeps that snapshot and says so beside it, and a provider recorded
// under a previous profile's routing is never presented as current scope after
// a profile switch.
/** From this share of a window on, the usage panel warns that the limit is near. */
export const LIMIT_WARNING_PERCENT = 80;

/** The warning for a window near its limit: «⊘ codex is at 84% of 5h; resets in 41m.» */
export function limitWarning(provider: string, name: string, window: UsageWindow, now: number): string {
	const head = `⊘ ${provider} is at ${Math.round(window.usedPercent)}%${name ? ` of ${name}` : ""}`;
	if (window.resetAt === null) return `${head}.`;
	const m = Math.max(1, Math.round((window.resetAt - now) / 60000));
	const until = m < 60 ? `${m}m` : m < 1440 ? `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, "0")}m` : `${Math.round(m / 1440)}d`;
	return `${head}; resets in ${until}.`;
}

export function renderUsagePanel(usages: ProviderUsage[], theme: UsageTheme, width: number, now: number, active?: ActiveProvider, registry?: UsageSourceRegistry, scope?: UsagePanelScope, ascii = false): string[] {
	const activeUsage = active ? usages.find((usage) => usage.provider === active.provider) : undefined;
	const byProvider = new Map(usages.map((usage) => [usage.provider, usage] as const));
	const rows = scope
		? scope.providers.map((provider) => ({ provider, usage: byProvider.get(provider) }))
		: [...(active ? [{ provider: active.provider, usage: activeUsage }] : []), ...usages.filter((usage) => usage !== activeUsage).map((usage) => ({ provider: usage.provider, usage }))];
	if (rows.length === 0) return [truncateToWidth(USAGE_EMPTY_MESSAGE, width, "…")];
	const lines: string[] = [];
	for (const row of rows) {
		// The active provider wears Jhon's brand where G draws its ✿.
		const mark = active?.provider === row.provider ? `${theme.fg(ROLE.LIMIT, glyph("brand", ascii))} ` : "";
		if (!row.usage) {
			// A settled refresh that answered nothing is a failure; a provider
			// whose usage arrives with responses (Anthropic) or that has not
			// failed keeps its pending note.
			const note = scope?.failed.has(row.provider) ? USAGE_FETCH_FAILED_NOTE : providerNote(row.provider, registry);
			lines.push(`${mark}${theme.fg(ROLE.PROVIDER, row.provider)} ${theme.fg(ROLE.SEPARATOR, "·")} ${theme.fg(ROLE.RESET, note)}`);
			continue;
		}
		const usage = row.usage;
		const plan = usage.plan ? ` ${theme.fg(ROLE.SEPARATOR, "·")} ${theme.fg(ROLE.PLAN, usage.plan)}` : "";
		lines.push(`${mark}${theme.fg(ROLE.PROVIDER, usage.provider)}${plan} ${theme.fg(ROLE.SEPARATOR, "·")} ${theme.fg(ROLE.RESET, updatedAgo(usage.fetchedAt, now))}`);
		// One row per window: the limit name, its meter, its percentage and the reset
		// that window reports, all on one line. A window without its own label (the
		// model's allowance) is named by its limit alone, and one without a reset ends
		// at its percentage, never on a dangling separator.
		const rows = groupUsageLimits(usage.limits).flatMap((limit) =>
			limit.windows.map((window) => ({ name: [limit.name, window.label].filter((part) => part.length > 0).join(" "), window })),
		);
		const nameWidth = rows.reduce((widest, row) => Math.max(widest, row.name.length), 0);
		for (const row of rows) {
			const percent = `${Math.round(row.window.usedPercent)}%`.padStart(4);
			const reset = formatReset(row.window.resetAt, now);
			const tail = reset.length > 0 ? ` ${theme.fg(ROLE.SEPARATOR, "·")} ${theme.fg(ROLE.RESET, reset)}` : "";
			lines.push(`  ${theme.fg(ROLE.LABEL, row.name.padEnd(nameWidth))} ${paintMeter(row.window.usedPercent, PANEL_METER_CELLS, theme)} ${theme.fg(ROLE.PERCENT, percent)}${tail}`);
		}
		// Near a limit, one plain line says so and when it lifts, before it bites.
		const nearest = rows.filter((r) => r.window.usedPercent >= LIMIT_WARNING_PERCENT).sort((a, b) => b.window.usedPercent - a.window.usedPercent)[0];
		if (nearest) lines.push(`  ${theme.fg("warning", limitWarning(usage.provider, nearest.name, nearest.window, now))}`);
		// A failure after a good snapshot never hides either fact: the last good
		// snapshot stays on its rows and the failed refresh says so beside it.
		if (scope?.failed.has(row.provider)) lines.push(`  ${theme.fg(ROLE.RESET, USAGE_FETCH_FAILED_NOTE)}`);
	}
	return lines.map((line) => truncateToWidth(line, width, "…"));
}

export class UsageStore {
	private readonly usages = new Map<string, ProviderUsage>();

	record(usage: ProviderUsage): void {
		this.usages.set(usage.provider, usage);
	}

	get(provider: string): ProviderUsage | undefined {
		return this.usages.get(provider);
	}

	all(): ProviderUsage[] {
		return [...this.usages.values()];
	}
}

/** A `provider/id` model reference; a bare id has no provider. */
export interface ModelRef { provider: string | undefined; id: string }

/** The `provider/id` reference a setting names; undefined when it names none. */
export function parseModelRef(value: unknown): ModelRef | undefined {
	if (typeof value !== "string") return undefined;
	const trimmed = value.trim();
	if (trimmed.length === 0) return undefined;
	const slash = trimmed.indexOf("/");
	if (slash <= 0) return { provider: undefined, id: trimmed };
	return { provider: trimmed.slice(0, slash), id: trimmed.slice(slash + 1) };
}
