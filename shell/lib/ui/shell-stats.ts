import { visibleWidth } from "@earendil-works/pi-tui";
import { elapsed, formatCost, formatTokens } from "./shell-bar.ts";
import { PANEL_ROW_INDENT, renderPanelHeader, renderPanelRow } from "./shell-panel.ts";
import { asciiText } from "./primitives/glyphs.ts";
import { sanitizeTerminalText } from "./terminal-theme.ts";
import { shellSettings } from "../settings.ts";

// The rail's STATS section, ported from the Gentle Shell's lib/shell-stats.ts
// (T92, L658): what the session spent, per model, and how it behaves (cache
// reuse, tool failures, pace), from the session's own entries, and what its
// sub-agents spent, from the background tasks.

export interface ModelStats {
	model: string;
	turns: number;
	tokens: number;
	cost: number;
}
export interface SessionStats {
	cost: number;
	input: number;
	output: number;
	cacheRead: number;
	cacheWrite: number;
	/** Assistant responses. */
	turns: number;
	toolCalls: number;
	toolErrors: number;
	/** From the first to the last dated entry. */
	spanMs: number;
	/** Cache reads over every prompt token, or undefined before the first prompt. */
	cacheHitRate: number | undefined;
	/** Most expensive first; ties by tokens. */
	models: ModelStats[];
	/** What this session's sub-agents spent (lib/background-tasks-tools.ts). */
	subagents?: { runs: number; tokens: number; cost: number };
}

type StatsEntry = {
	type?: string;
	timestamp?: unknown;
	message?: { role?: string; model?: unknown; isError?: unknown; usage?: { input?: number; output?: number; cacheRead?: number; cacheWrite?: number; cost?: { total?: number } } };
};

const count = (value: unknown) => (typeof value === "number" && Number.isFinite(value) && value > 0 ? value : 0);

/** G's collectSessionStats: the totals of a session's entries. */
export function collectSessionStats(entries: readonly unknown[]): SessionStats {
	const stats: SessionStats = { cost: 0, input: 0, output: 0, cacheRead: 0, cacheWrite: 0, turns: 0, toolCalls: 0, toolErrors: 0, spanMs: 0, cacheHitRate: undefined, models: [] };
	const models = new Map<string, ModelStats>();
	let first = Infinity;
	let last = -Infinity;
	for (const raw of entries) {
		const entry = raw as StatsEntry;
		const at = typeof entry.timestamp === "string" ? Date.parse(entry.timestamp) : count(entry.timestamp) || Number.NaN;
		if (Number.isFinite(at)) { first = Math.min(first, at); last = Math.max(last, at); }
		if (entry.type !== "message") continue;
		const message = entry.message;
		if (message?.role === "toolResult") {
			stats.toolCalls++;
			if (message.isError === true) stats.toolErrors++;
			continue;
		}
		if (message?.role !== "assistant") continue;
		const usage = message.usage ?? {};
		const [input, output, cacheRead, cacheWrite, cost] = [count(usage.input), count(usage.output), count(usage.cacheRead), count(usage.cacheWrite), count(usage.cost?.total)];
		stats.turns++;
		stats.input += input;
		stats.output += output;
		stats.cacheRead += cacheRead;
		stats.cacheWrite += cacheWrite;
		stats.cost += cost;
		// A provider prefix (`claude-bridge/…`) goes: the row names the model.
		const id = typeof message.model === "string" ? sanitizeTerminalText(message.model).replace(/[\x00-\x1f\x7f]/g, "").trim() : "";
		const name = id.slice(id.lastIndexOf("/") + 1) || "unknown";
		const model = models.get(name) ?? { model: name, turns: 0, tokens: 0, cost: 0 };
		model.turns++;
		model.tokens += input + output + cacheRead + cacheWrite;
		model.cost += cost;
		models.set(name, model);
	}
	const prompt = stats.input + stats.cacheRead + stats.cacheWrite;
	stats.cacheHitRate = prompt > 0 ? stats.cacheRead / prompt : undefined;
	stats.spanMs = last > first ? last - first : 0;
	stats.models = [...models.values()].sort((a, b) => b.cost - a.cost || b.tokens - a.tokens);
	return stats;
}

type StatsTheme = { fg(role: string, text: string): string; bold(text: string): string };
const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

/**
 * The rail's STATS section as G's: `▾ STATS … $<spent>`, `profile <name>`, then
 * spent/turns/wall time, tokens, cache, tools, the sub-agents' spend, and one
 * row per model (its numbers on a row of their own when the rail is narrow).
 * A session that has not answered yet shows none; folded, the header alone.
 */
export function renderShellStatsSection(stats: SessionStats, theme: StatsTheme, width: number, options: { profile?: string; subscription?: boolean; folded?: boolean; hovered?: boolean } = {}): string[] {
	if (stats.turns === 0) return [];
	const subscription = options.subscription ?? false;
	const ascii = shellSettings().ascii();
	const header = renderPanelHeader(theme, width, { title: "STATS", count: formatCost(stats.cost, subscription), open: !options.folded, hovered: options.hovered ?? false, ascii });
	if (options.folded) return [ascii ? asciiText(header) : header];
	const label = (text: string) => theme.fg("muted", text.padEnd(7));
	const value = (text: string) => theme.fg("text", text);
	const muted = (text: string) => theme.fg("muted", text);
	const profile = options.profile ? sanitizeTerminalText(options.profile) : "";
	const rows = [
		...(profile ? [renderPanelRow(width, `${muted("profile")} ${value(profile)}`)] : []),
		renderPanelRow(width, `${label("spent")}${value(formatCost(stats.cost, subscription))}`, muted(`${plural(stats.turns, "turn")} · ${elapsed(stats.spanMs)}`)),
		renderPanelRow(width, `${label("tokens")}${value(`in ${formatTokens(stats.input)} · out ${formatTokens(stats.output)}`)}`),
	];
	if (stats.cacheHitRate !== undefined) rows.push(renderPanelRow(width, `${label("cache")}${value(`${Math.round(stats.cacheHitRate * 100)}% hit · ${formatTokens(stats.cacheRead)} read`)}`));
	const failed = stats.toolErrors ? ` · ${theme.fg("warning", `${stats.toolErrors} failed`)}` : "";
	rows.push(renderPanelRow(width, `${label("tools")}${value(plural(stats.toolCalls, "call"))}${failed}`));
	if (stats.subagents?.runs) rows.push(renderPanelRow(width, `${label("agents")}${value(`${plural(stats.subagents.runs, "run")} · ${formatTokens(stats.subagents.tokens)} · ${formatCost(stats.subagents.cost, subscription)}`)}`));
	for (const model of stats.models) {
		const numbers = `${plural(model.turns, "turn")} · ${formatTokens(model.tokens)} · ${formatCost(model.cost, subscription)}`;
		if (visibleWidth(model.model) + 2 + visibleWidth(numbers) <= width - PANEL_ROW_INDENT) rows.push(renderPanelRow(width, value(model.model), muted(numbers)));
		else rows.push(renderPanelRow(width, value(model.model)), renderPanelRow(width, muted(numbers), "", PANEL_ROW_INDENT + 2));
	}
	const lines = [header, ...rows];
	return ascii ? lines.map(asciiText) : lines;
}
