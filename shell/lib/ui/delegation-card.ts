/**
 * shell/lib/ui/delegation-card.ts — a delegation's card (design/shell-tui 01, T174-S7).
 *
 *   → Delegating · jhon-builder · claude-opus-5-5 · high              ◐ running
 *     Started background sub-agent agent-3 (jhon-builder). Its answer …
 *
 * The heading names the verb, the agent, its model and its thinking, as far
 * as they are known; at the right the state of the agent it launched
 * (`◐ running`, `✓ done`, `✗ failed`, `✗ stopped`), followed while it runs in
 * the background; under it the tool's first line, every word kept. ctrl+o
 * shows the rest of a foreground answer. shell.appearance.density "minimal"
 * keeps that line to one row; shell.appearance.ascii writes `->` and `-`.
 */
import type { ToolRenderers } from "@earendil-works/pi-coding-agent";
import type { TaskStatus } from "../background-tasks.ts";
import { shellSettings } from "../settings.ts";
import { asciiText } from "./primitives/glyphs.ts";
import { stateGlyph } from "./primitives/states.ts";
import { sanitizeTerminalText } from "./terminal-theme.ts";
import { SplitBody, SplitCall, wrapRows, type SplitContext, type SplitResult } from "./tool-card.ts";
import { ownsIcon } from "./tool-icons.ts";

type CardTheme = { fg(color: string, text: string): string; bold(text: string): string };

/** What a delegation runs on: the agent's model and thinking, when its definition can be read. */
export type DelegationProfile = (agent: string) => { model?: string; thinking?: string } | undefined;

/** The state of a background agent by its task id and the session that launched it; undefined once it is gone. */
export type DelegationStatus = (task: string, owner: string) => TaskStatus | undefined;

function clean(value: unknown): string {
	return sanitizeTerminalText(String(value ?? "")).replace(/\s+/g, " ").trim();
}

function ascii(): boolean {
	try { return shellSettings().ascii(); } catch { return false; }
}

/** The heading's text: the verb, then the agent, its model and its thinking, as far as they are known. */
export function delegationHeading(verb: string, agent: string, profile: DelegationProfile, plain = ascii()): string {
	const name = clean(agent);
	const known = name ? profile(name) : undefined;
	const parts = [verb, name, clean(known?.model), clean(known?.thinking)].filter(Boolean).join(" · ");
	return `${plain ? "->" : "→"} ${plain ? asciiText(parts) : parts}`;
}

function paintedHeading(verb: string, agent: string, profile: DelegationProfile, t: CardTheme, plain: boolean): string {
	const name = clean(agent);
	const known = name ? profile(name) : undefined;
	const dot = t.fg("muted", plain ? " - " : " · ");
	const rest = [clean(known?.model), clean(known?.thinking)].filter(Boolean).map((part) => t.fg("muted", plain ? asciiText(part) : part));
	return [t.fg("toolTitle", t.bold(verb)), ...(name ? [t.fg("accent", plain ? asciiText(name) : name)] : []), ...rest].join(dot);
}

function textOf(result: { content?: unknown }): string {
	const content = Array.isArray(result?.content) ? result.content : [];
	return sanitizeTerminalText(content.map((part: any) => (part?.type === "text" ? part.text : "")).join("\n")).trim();
}

const STATE: Partial<Record<TaskStatus, { glyph: "working" | "done" | "failed"; role: string; word: string }>> = {
	running: { glyph: "working", role: "warning", word: "running" },
	completed: { glyph: "done", role: "success", word: "done" },
	exited: { glyph: "done", role: "success", word: "done" },
	failed: { glyph: "failed", role: "error", word: "failed" },
	stopped: { glyph: "failed", role: "error", word: "stopped" },
};

function delegationResult(result: { content?: unknown; details?: unknown }, isError: boolean, isPartial: boolean, t: CardTheme, status?: DelegationStatus): SplitResult {
	const text = textOf(result);
	const details = (result?.details ?? {}) as { task?: unknown; owner?: unknown };
	const task = typeof details.task === "string" ? details.task : undefined;
	const owner = typeof details.owner === "string" ? details.owner : "";
	const tone = isError ? "error" : "normal";
	return {
		get right() {
			const now: TaskStatus | undefined = isPartial ? "running" : isError ? "failed" : task ? status?.(task, owner) : "completed";
			const state = now && STATE[now];
			return state ? t.fg(state.role, `${stateGlyph(state.glyph, ascii())} ${state.word}`) : undefined;
		},
		tone,
		rows(cells, look, expanded) {
			const role = isError ? "error" : "muted";
			const lines = text ? text.split("\n") : [];
			const shown = expanded || isPartial ? lines : lines.slice(0, 1);
			let minimal = false;
			try { minimal = shellSettings().density() === "minimal"; } catch { /* Not minimal. */ }
			if (minimal && !expanded) return shown.slice(0, 1).map((line) => t.fg(role, line));
			return shown.flatMap((line) => wrapRows(t.fg(role, line), cells));
		},
	};
}

/**
 * The renderers of a tool that launches a sub-agent: its argument `key`
 * names the agent; `status` follows the background agent a launch started.
 */
export function delegationRenderers(verb: string, key: string, profile: DelegationProfile, status?: DelegationStatus): Pick<ToolRenderers, "renderCall" | "renderResult"> {
	return {
		renderCall: ownsIcon((args, theme, context) => {
			const t = theme as unknown as CardTheme;
			const agent = (args as Record<string, unknown> | undefined)?.[key];
			return new SplitCall((look) => ({
				icon: look.ascii ? "->" : "→",
				title: paintedHeading(verb, typeof agent === "string" ? agent : "", profile, t, look.ascii),
			}), t as never, (context ?? {}) as SplitContext);
		}),
		renderResult(result, options, theme, context) {
			const t = theme as unknown as CardTheme;
			const ctx = (context ?? {}) as SplitContext;
			const isError = ctx.isError ?? ("isError" in options && (options as { isError?: boolean }).isError === true);
			return new SplitBody(delegationResult(result as never, isError === true, options.isPartial === true, t, status), { ...ctx, expanded: options.expanded }, t as never);
		},
	};
}
