/**
 * shell/lib/ui/usage.ts — the subscription usage (T174-S5b, L1493-L1495).
 *
 * A subscription has windows: a share of it used over 5 hours, over a week,
 * each with when it resets. Claude (anthropic, and claude-bridge, which
 * re-sends Claude Code's headers) and ChatGPT (openai-codex) report theirs
 * with every reply, as response headers or, on the Codex WebSocket, as a
 * `codex.rate_limits` stream event; nothing is fetched for them. Another
 * extension may register a source for its own provider on pi.events
 * (USAGE_SOURCE_EVENT): the tracker fetches it at most every REFRESH_MS, and
 * forced by `r`, each fetch bounded by a timeout.
 *
 * What the status bar shows (barWindows) and what the overlay lists
 * (usageRows) are pure: data in, rows out. Nothing here keeps an account's
 * details beyond the plan's name, and no key is ever drawn or kept.
 */
import { visibleWidth } from "@earendil-works/pi-tui";
import { clip, glyph, METER_WARN, meterTone, paintMeter, themePainter, type Role, type ThemeLike } from "./primitives/index.ts";
import { sanitizeTerminalText } from "./terminal-theme.ts";

/** One window of a limit: how much of it is used and when it resets (epoch ms, or null when not said). */
export interface UsageWindow {
	label: string;
	usedPercent: number;
	windowSeconds: number;
	resetAt: number | null;
}

/** A limit a provider applies: its windows, and whether it was hit. */
export interface UsageLimit {
	name: string;
	windows: UsageWindow[];
	limitReached: boolean;
}

/** What one provider last said of its limits. */
export interface ProviderUsage {
	provider: string;
	plan?: string;
	limits: UsageLimit[];
	fetchedAt: number;
}

export const ANTHROPIC = "anthropic";
export const CLAUDE_BRIDGE = "claude-bridge";
export const OPENAI_CODEX = "openai-codex";
/** The providers whose windows come with their replies. */
const ON_REPLY = new Set([ANTHROPIC, CLAUDE_BRIDGE, OPENAI_CODEX]);

const MINUTE = 60;
const HOUR = 3_600;
const DAY = 86_400;
export const WEEK_SECONDS = 7 * DAY;
export const FIVE_HOUR_SECONDS = 5 * HOUR;
/** The 5h window joins the status bar from the meter ladder's warn step (S1). */
export const FIVE_HOUR_FROM = METER_WARN;
/** From this share of a window the overlay says the limit is near. */
export const NEAR_LIMIT_PERCENT = 80;

/** `week`, `5h`, `3d`, `45m`. */
export function windowLabel(seconds: number): string {
	if (seconds === WEEK_SECONDS) return "week";
	if (seconds >= DAY && seconds % DAY === 0) return `${seconds / DAY}d`;
	if (seconds >= HOUR && seconds % HOUR === 0) return `${seconds / HOUR}h`;
	return `${Math.round(seconds / MINUTE)}m`;
}

/** A number as a provider sends it: a JSON number or a header's decimal text; anything else is no number. */
function number(value: unknown): number | undefined {
	const parsed = typeof value === "string" && value.trim() !== "" ? Number(value) : value;
	return typeof parsed === "number" && Number.isFinite(parsed) ? parsed : undefined;
}

// ---------------------------------------------------------------- Claude

const UNIFIED = "anthropic-ratelimit-unified-";
/** The windows Claude's unified headers name: the 5-hour one and the week. */
const CLAUDE_WINDOWS: ReadonlyArray<[key: string, seconds: number]> = [["5h", FIVE_HOUR_SECONDS], ["7d", WEEK_SECONDS]];
/** A model's own week (`seven_day_opus`); `oi` and `overage` are the account's week again, not a model. */
const MODEL_WEEK = /^anthropic-ratelimit-unified-(?:seven_day|7d)_([a-z][a-z0-9]*)-utilization$/;
const NOT_A_MODEL = new Set(["oi", "overage"]);

function claudeWindow(headers: Record<string, string>, key: string, seconds: number): UsageWindow | undefined {
	// Claude sends a 0..1 fraction and the reset in Unix seconds.
	const fraction = number(headers[`${UNIFIED}${key}-utilization`]);
	if (fraction === undefined) return undefined;
	const reset = number(headers[`${UNIFIED}${key}-reset`]);
	return { label: windowLabel(seconds), usedPercent: fraction * 100, windowSeconds: seconds, resetAt: reset !== undefined && reset > 0 ? reset * 1000 : null };
}

function claudeUsage(headers: Record<string, string>, now: number, provider: string): ProviderUsage | undefined {
	const windows = CLAUDE_WINDOWS.map(([key, seconds]) => claudeWindow(headers, key, seconds)).filter((w): w is UsageWindow => w !== undefined);
	const limits: UsageLimit[] = windows.length ? [{ name: "claude", windows, limitReached: headers[`${UNIFIED}status`] === "rejected" }] : [];
	for (const name of Object.keys(headers)) {
		const model = MODEL_WEEK.exec(name)?.[1];
		if (!model || NOT_A_MODEL.has(model)) continue;
		const window = claudeWindow(headers, name.slice(UNIFIED.length, -"-utilization".length), WEEK_SECONDS);
		if (window) limits.push({ name: model, windows: [window], limitReached: false });
	}
	return limits.length ? { provider, limits, fetchedAt: now } : undefined;
}

// ---------------------------------------------------------------- ChatGPT

/** The Codex CLI's names for the default family's two windows. */
const CODEX_WINDOWS = ["primary", "secondary"] as const;
export const CODEX_RATE_LIMITS = "codex.rate_limits";

function codexWindow(used: unknown, minutes: unknown, resetAt: unknown): UsageWindow | undefined {
	const percent = number(used);
	const length = number(minutes);
	// Without its share or its length a window has no number and no name: none is guessed.
	if (percent === undefined || length === undefined || length <= 0) return undefined;
	const reset = number(resetAt);
	return { label: windowLabel(length * MINUTE), usedPercent: percent, windowSeconds: length * MINUTE, resetAt: reset !== undefined && reset > 0 ? reset * 1000 : null };
}

function codexUsage(windows: Array<UsageWindow | undefined>, plan: unknown, now: number, provider: string): ProviderUsage | undefined {
	const present = windows.filter((w): w is UsageWindow => w !== undefined);
	if (!present.length) return undefined;
	return { provider, ...(typeof plan === "string" && plan ? { plan } : {}), limits: [{ name: "codex", windows: present, limitReached: present.some((w) => w.usedPercent >= 100) }], fetchedAt: now };
}

/**
 * A reply's windows from its headers: Claude's unified ones, else Codex's
 * x-codex-*; undefined when the reply carries none. pi's event names no
 * provider: the session's model's is the one that answered.
 */
export function usageFromHeaders(headers: Record<string, string>, now: number, provider?: string): ProviderUsage | undefined {
	const claude = claudeUsage(headers, now, provider ?? ANTHROPIC);
	if (claude) return claude;
	const windows = CODEX_WINDOWS.map((name) => codexWindow(headers[`x-codex-${name}-used-percent`], headers[`x-codex-${name}-window-minutes`], headers[`x-codex-${name}-reset-at`]));
	return codexUsage(windows, undefined, now, provider ?? OPENAI_CODEX);
}

/** The Codex WebSocket's `codex.rate_limits` event; any other event, or another family's limits, is undefined. */
export function usageFromCodexEvent(data: unknown, now: number, provider?: string): ProviderUsage | undefined {
	if (!data || typeof data !== "object") return undefined;
	const event = data as { type?: unknown; plan_type?: unknown; rate_limits?: unknown; metered_limit_name?: unknown; limit_name?: unknown };
	if (event.type !== CODEX_RATE_LIMITS) return undefined;
	const family = event.metered_limit_name ?? event.limit_name;
	if (family !== undefined && family !== null && family !== "codex") return undefined;
	const limits = (event.rate_limits && typeof event.rate_limits === "object" ? event.rate_limits : {}) as Record<string, { used_percent?: unknown; window_minutes?: unknown; reset_at?: unknown } | undefined>;
	const windows = CODEX_WINDOWS.map((name) => {
		const raw = limits[name];
		return raw && typeof raw === "object" ? codexWindow(raw.used_percent, raw.window_minutes, raw.reset_at) : undefined;
	});
	return codexUsage(windows, event.plan_type, now, provider ?? OPENAI_CODEX);
}

// ---------------------------------------------------------------- the bar

/** A window as the status bar shows it; `reset` marks the one whose reset it shows. */
export interface BarWindow {
	label: string;
	percent: number;
	resetAt: number | null;
	reset: boolean;
	/** The week's meter: only the week draws one. */
	meter: boolean;
}

const TONE_RANK = { dim: 0, success: 0, warning: 1, error: 2 } as const;

/**
 * The windows the status bar shows for the session's model: the week (or
 * the limit's first window when it reports no week) and the 5h window from
 * FIVE_HOUR_FROM, the 5h first. The reset sits on the more urgent of the two
 * on the meter ladder, the 5h on a tie: it is the one about to bite.
 */
export function barWindows(usage: ProviderUsage | undefined, modelId?: string): BarWindow[] {
	const limit = usage ? (usage.limits.find((l) => l.name === modelId) ?? usage.limits[0]) : undefined;
	if (!limit?.windows.length) return [];
	const five = limit.windows.find((w) => w.windowSeconds === FIVE_HOUR_SECONDS);
	const week = limit.windows.find((w) => w.windowSeconds === WEEK_SECONDS) ?? (five ? undefined : limit.windows[0]);
	const shownFive = five && five.usedPercent >= FIVE_HOUR_FROM ? five : undefined;
	const rank = (w: UsageWindow) => TONE_RANK[meterTone(w.usedPercent)];
	const resetOnFive = Boolean(shownFive && (!week || rank(shownFive) >= rank(week)));
	const out: BarWindow[] = [];
	if (shownFive) out.push({ label: "5h", percent: shownFive.usedPercent, resetAt: shownFive.resetAt, reset: resetOnFive && shownFive.resetAt !== null, meter: false });
	if (week) {
		const label = week.windowSeconds === WEEK_SECONDS ? "week" : clean(week.label || limit.name);
		out.push({ label, percent: week.usedPercent, resetAt: week.resetAt, reset: !resetOnFive && week.resetAt !== null, meter: true });
	}
	return out;
}

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"] as const;

/** When a window resets, in the local week: `Thu 14:00`. */
export function resetDay(at: number): string {
	const d = new Date(at);
	return `${WEEKDAYS[d.getDay()]} ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

/** `resets in 41m`, `resets in 3h 12m`, `resets in 2d 4h`, `resets now`; nothing when not said. */
export function resetsIn(resetAt: number | null, now: number): string {
	if (resetAt === null) return "";
	const seconds = Math.floor((resetAt - now) / 1000);
	if (seconds <= 0) return "resets now";
	if (seconds < HOUR) return `resets in ${Math.max(1, Math.round(seconds / MINUTE))}m`;
	if (seconds < DAY) return `resets in ${Math.floor(seconds / HOUR)}h ${Math.floor((seconds % HOUR) / MINUTE)}m`;
	return `resets in ${Math.floor(seconds / DAY)}d ${Math.floor((seconds % DAY) / HOUR)}h`;
}

/** Foreign text the shell may print: no escapes, whitespace as single spaces. */
function clean(text: string): string {
	return sanitizeTerminalText(text.replace(/\s+/g, " ")).trim();
}

// ---------------------------------------------------------------- sources

/**
 * The hook another extension registers its provider's usage with: it emits
 * `{ schema, provider, fetch, pendingNote? }` on pi.events under this name.
 * The shell never learns the provider's endpoint or key: it calls fetch with
 * the key pi holds for that provider and validates what comes back.
 */
export const USAGE_SOURCE_EVENT = "jhon-shell:usage-source/v1";
export const USAGE_SOURCE_SCHEMA = "jhon-shell.usage-source/v1";
const PROVIDER_NAME = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

export interface UsageSource {
	provider: string;
	pendingNote?: string;
	fetch(apiKey: string | undefined, fetchFn: typeof fetch, now: number): unknown;
}

/** A registration as it crossed the event bus, checked field by field; undefined for any other shape. */
export function parseUsageSource(value: unknown): UsageSource | undefined {
	if (!value || typeof value !== "object") return undefined;
	const raw = value as Record<string, unknown>;
	if (raw.schema !== USAGE_SOURCE_SCHEMA || typeof raw.provider !== "string" || !PROVIDER_NAME.test(raw.provider) || typeof raw.fetch !== "function") return undefined;
	if (raw.pendingNote !== undefined && typeof raw.pendingNote !== "string") return undefined;
	return { provider: raw.provider, fetch: raw.fetch as UsageSource["fetch"], ...(typeof raw.pendingNote === "string" ? { pendingNote: raw.pendingNote } : {}) };
}

const finite = (value: unknown): value is number => typeof value === "number" && Number.isFinite(value);

/**
 * What a source answered, rebuilt from scratch: foreign code's object is
 * never kept by reference, and any field of the wrong kind refuses it whole.
 */
export function parseProviderUsage(value: unknown, provider: string): ProviderUsage | undefined {
	if (!value || typeof value !== "object") return undefined;
	const raw = value as Record<string, unknown>;
	if (raw.provider !== provider || !finite(raw.fetchedAt) || !Array.isArray(raw.limits)) return undefined;
	if (raw.plan !== undefined && typeof raw.plan !== "string") return undefined;
	const limits: UsageLimit[] = [];
	for (const entry of raw.limits as unknown[]) {
		const limit = entry as Record<string, unknown> | null;
		if (!limit || typeof limit.name !== "string" || typeof limit.limitReached !== "boolean" || !Array.isArray(limit.windows)) return undefined;
		const windows: UsageWindow[] = [];
		for (const w of limit.windows as unknown[]) {
			const win = w as Record<string, unknown> | null;
			if (!win || typeof win.label !== "string" || !finite(win.usedPercent) || !finite(win.windowSeconds) || (win.resetAt !== null && !finite(win.resetAt))) return undefined;
			windows.push({ label: win.label, usedPercent: win.usedPercent, windowSeconds: win.windowSeconds, resetAt: win.resetAt as number | null });
		}
		limits.push({ name: limit.name, windows, limitReached: limit.limitReached });
	}
	return { provider, ...(typeof raw.plan === "string" ? { plan: raw.plan } : {}), limits, fetchedAt: raw.fetchedAt };
}

/** A source's provider is fetched again only after this long, unless forced. */
export const REFRESH_MS = 5 * 60_000;
const TIMEOUT_MS = 10_000;

/** The window one fetch gets, JHON_SHELL_USAGE_TIMEOUT_MS (ms) or 10 s; a value that is not a positive number is the default. */
export function usageTimeoutMs(env: NodeJS.ProcessEnv): number {
	const value = Number.parseInt(env.JHON_SHELL_USAGE_TIMEOUT_MS ?? "", 10);
	return Number.isFinite(value) && value > 0 ? value : TIMEOUT_MS;
}

export interface TrackerDeps {
	now(): number;
	fetch: typeof fetch;
	timeoutMs: number;
	/** The key pi holds for a provider; never drawn, logged or kept. */
	apiKey(provider: string): Promise<string | undefined>;
	/** A provider settled (a snapshot or a failure): repaint. */
	onSettled(): void;
}

/**
 * Every provider's last usage, from replies and from registered sources. A
 * refresh fetches each source of the given providers concurrently, one
 * bounded window each (the key's lookup and the fetch together); only the
 * newest refresh of a provider may change what is kept, so a late answer,
 * success or failure, never overwrites a newer one.
 */
export class UsageTracker {
	readonly #deps: TrackerDeps;
	readonly #usage = new Map<string, ProviderUsage>();
	readonly #sources = new Map<string, UsageSource>();
	readonly #fetchedAt = new Map<string, number>();
	readonly #generation = new Map<string, number>();
	readonly #failed = new Set<string>();

	constructor(deps: TrackerDeps) {
		this.#deps = deps;
	}

	/** A reply's snapshot: it replaces the provider's last one and clears its failure. */
	record(usage: ProviderUsage | undefined): void {
		if (!usage) return;
		this.#usage.set(usage.provider, usage);
		this.#failed.delete(usage.provider);
		this.#deps.onSettled();
	}

	get(provider: string | undefined): ProviderUsage | undefined {
		return provider === undefined ? undefined : this.#usage.get(provider);
	}

	/** The provider's latest refresh answered nothing. */
	failed(provider: string): boolean {
		return this.#failed.has(provider);
	}

	/** A registration from pi.events; false for a shape that is not one. The newest for a provider wins. */
	register(payload: unknown): UsageSource | false {
		const source = parseUsageSource(payload);
		if (!source) return false;
		this.#sources.set(source.provider, source);
		return source;
	}

	/** What a provider with no snapshot says. */
	note(provider: string): string {
		if (this.#failed.has(provider)) return "the fetch failed · r to retry";
		if (ON_REPLY.has(provider)) return "usage arrives with the first reply";
		const source = this.#sources.get(provider);
		if (source) return source.pendingNote ?? "not fetched yet · r fetches";
		return "this provider does not report subscription usage";
	}

	/** Fetches the registered sources among providers: those fetched in the last REFRESH_MS only when forced. */
	async refresh(providers: readonly string[], force: boolean): Promise<void> {
		const now = this.#deps.now();
		await Promise.all(providers.map((provider) => this.#refreshOne(provider, force, now)));
	}

	async #refreshOne(provider: string, force: boolean, now: number): Promise<void> {
		const source = this.#sources.get(provider);
		if (!source) return;
		if (!force && now - (this.#fetchedAt.get(provider) ?? -Infinity) < REFRESH_MS) return;
		this.#fetchedAt.set(provider, now);
		const generation = (this.#generation.get(provider) ?? 0) + 1;
		this.#generation.set(provider, generation);
		const controller = new AbortController();
		let timer: ReturnType<typeof setTimeout> | undefined;
		const expired = new Promise<undefined>((resolve) => {
			timer = setTimeout(() => { controller.abort(); resolve(undefined); }, this.#deps.timeoutMs);
		});
		// The window's signal joins the caller's own, so either one aborts the fetch.
		const bounded: typeof fetch = (input, init) => {
			const own = init?.signal ?? (typeof Request !== "undefined" && input instanceof Request ? input.signal : undefined);
			return this.#deps.fetch(input, { ...init, signal: own ? AbortSignal.any([own, controller.signal]) : controller.signal });
		};
		const work = (async () => {
			const apiKey = await this.#deps.apiKey(provider).catch(() => undefined);
			if (controller.signal.aborted) return undefined;
			return parseProviderUsage(await source.fetch(apiKey, bounded, this.#deps.now()), provider);
		})().catch(() => undefined);
		const answer = await Promise.race([work, expired]);
		clearTimeout(timer);
		// A newer refresh, or a newer source, owns the provider now.
		if (this.#generation.get(provider) !== generation || this.#sources.get(provider) !== source) return;
		if (answer) {
			this.#usage.set(provider, answer);
			this.#failed.delete(provider);
		} else this.#failed.add(provider);
		this.#deps.onSettled();
	}
}

/**
 * The providers the overlay shows: the session's first, then each one the
 * sub-agents' models name (`provider/model`), once each in that order. A bare
 * model id names its provider only when resolve finds exactly one.
 */
export function usageScope(main: string | undefined, models: ReadonlyArray<string | undefined>, resolve: (modelId: string) => string | undefined = () => undefined): string[] {
	const out = new Set<string>();
	if (main) out.add(main);
	for (const model of models) {
		const ref = model?.trim();
		if (!ref) continue;
		const slash = ref.indexOf("/");
		const provider = slash > 0 ? ref.slice(0, slash) : resolve(ref);
		if (provider) out.add(provider);
	}
	return [...out];
}

// ---------------------------------------------------------------- the overlay's rows

export interface UsageRowsInput {
	/** The providers to list, in order. */
	scope: readonly string[];
	/** The session's provider: marked with Jhon's brand. */
	active?: string;
	tracker: Pick<UsageTracker, "get" | "failed" | "note">;
	now: number;
	ascii?: boolean;
}

/** The overlay meter's cells. */
const METER_CELLS = 16;
const METER_ROLE: Record<string, Role> = { success: "ok", warning: "warn", error: "bad", dim: "dim" };

/** `updated just now`, `updated 4m ago`. */
function updated(at: number, now: number): string {
	const minutes = Math.floor((now - at) / 60_000);
	return minutes < 1 ? "updated just now" : `updated ${minutes}m ago`;
}

/** `⊘ codex is at 84% of 5h; resets in 41m.` */
function nearLimit(provider: string, name: string, window: UsageWindow, now: number, ascii: boolean): string {
	const head = `${ascii ? "!" : "⊘"} ${provider} is at ${Math.round(window.usedPercent)}% of ${name}`;
	if (window.resetAt === null) return `${head}.`;
	const minutes = Math.max(1, Math.round((window.resetAt - now) / 60_000));
	const until = minutes < 60 ? `${minutes}m` : minutes < 1440 ? `${Math.floor(minutes / 60)}h ${String(minutes % 60).padStart(2, "0")}m` : `${Math.round(minutes / 1440)}d`;
	return `${head}; resets in ${until}.`;
}

/**
 * The overlay's body, each row at most width cells: per provider in scope a
 * title row (`力 anthropic · max · updated 2m ago`), then one row per window,
 * its name, meter, % and reset; near a limit one row says so. A provider with
 * nothing yet says why on its title row.
 */
export function usageRows(input: UsageRowsInput, theme: ThemeLike, width: number): string[] {
	const paint = themePainter(theme);
	const ascii = input.ascii ?? false;
	const dot = paint.fg("dim", ascii ? " - " : " · ");
	const rows: string[] = [];
	if (!input.scope.length) return [paint.fg("dim", clip("No subscription usage yet. It arrives with the next reply, or press r to fetch it.", width, { ascii }))];
	for (const provider of input.scope) {
		const mark = provider === input.active ? `${paint.fg("accent", glyph("brand", ascii))} ` : "";
		const name = paint.fg("ink", clean(provider));
		const usage = input.tracker.get(provider);
		if (!usage) {
			rows.push(`${mark}${name}${dot}${paint.fg("dim", input.tracker.note(provider))}`);
			continue;
		}
		rows.push(`${mark}${name}${usage.plan ? `${dot}${paint.fg("soft", clean(usage.plan))}` : ""}${dot}${paint.fg("dim", updated(usage.fetchedAt, input.now))}`);
		const lines = usage.limits.flatMap((limit) => limit.windows.map((window) => ({ name: [clean(limit.name), clean(window.label)].filter(Boolean).join(" "), window })));
		const nameCells = Math.max(0, ...lines.map((line) => visibleWidth(line.name)));
		for (const line of lines) {
			const percent = `${Math.round(line.window.usedPercent)}%`.padStart(4);
			const reset = resetsIn(line.window.resetAt, input.now);
			const tone = METER_ROLE[meterTone(line.window.usedPercent)]!;
			rows.push(`  ${paint.fg("soft", line.name.padEnd(nameCells))} ${paintMeter(line.window.usedPercent, theme, METER_CELLS, ascii)} ${paint.fg(tone, percent)}${reset ? `${dot}${paint.fg("dim", reset)}` : ""}`);
		}
		const nearest = lines.filter((line) => line.window.usedPercent >= NEAR_LIMIT_PERCENT).sort((a, b) => b.window.usedPercent - a.window.usedPercent)[0];
		if (nearest) rows.push(`  ${paint.fg("warn", nearLimit(clean(provider), nearest.name, nearest.window, input.now, ascii))}`);
		if (input.tracker.failed(provider)) rows.push(`  ${paint.fg("dim", input.tracker.note(provider))}`);
	}
	return rows;
}
