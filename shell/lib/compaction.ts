/**
 * The main session's own compaction (T115a, T121): at shell.behaviour.compact_at
 * of its window or past shell.behaviour.compact_tokens, whichever is first.
 * Each turn re-reads the whole context, so a lighter one answers sooner.
 *
 * When it compacts (pi 1.5.2, @earendil-works/pi-coding-agent dist/core):
 * - after a reply (agent_settled), as before;
 * - before the first prompt of a session already over budget (a resume, or a
 *   model switch to a smaller window): the `input` handler awaits the
 *   compaction, so the owner's prompt runs right after it on the compacted
 *   context. pi's prompt() runs the input handlers and awaits each
 *   (agent-session.js:1736, extensions/runner.js:1223) before _startRun;
 *   the stale pre-compaction usage of the last reply no longer recompacts
 *   (agent-session.js:2505-2509);
 * - between the turns of a long run: pi compacts mid-run only past
 *   window − compaction.reserveTokens (agent-session.js:472-487), so a 1M
 *   window ran 12 turns past 200K (L762 F1). A turn that ends with tool
 *   results over budget calls ctx.compact(), which aborts the run first and
 *   never continues it (agent-session.js:2315-2327); when it completes, or
 *   fails, a custom message with triggerTurn starts the next run that tells
 *   the agent to carry on (agent-session.js:1967-1972, deferred past a
 *   settle at :1968-1970). A reply's last turn (no tool results) is left to
 *   agent_settled. A headless run (print/json) is not interrupted: it
 *   returns at its settle and could not be continued.
 *
 * After a compaction pi reports the context as unknown until the next reply
 * (getContextUsage, agent-session.js:3607-3634), so nothing recompacts on the
 * stale usage. A child never compacts here (lib/child-budget.ts).
 */
import { fromTheShell } from "./from-the-shell.ts";

/** The custom message that continues a run its compaction interrupted. */
export const COMPACTION_CONTINUE_ENTRY = "jhon-compaction-continue";

export type ContextUsage = { tokens?: number | null; percent?: number | null; contextWindow?: number } | undefined;

export type CompactionDeps = {
	/** compact_at's share, undefined when it leaves compaction to pi. */
	at: () => number | undefined;
	/** compact_tokens; 0 is no budget. */
	budget: () => number;
	record?: (name: string, attributes: Record<string, unknown>) => void;
	/** pi.sendMessage. */
	send?: (message: { customType: string; content: string; display: boolean }, options: Record<string, unknown>) => void;
};

/** Why the context is due a compaction, or undefined. */
export function compactionDue(usage: ContextUsage, at: number | undefined, budget: number): { byShare: boolean; notice: string; attributes: Record<string, unknown> } | undefined {
	if (at === undefined || !usage) return undefined;
	const byShare = typeof usage.percent === "number" && usage.percent >= at;
	const byTokens = !byShare && budget > 0 && typeof usage.tokens === "number" && usage.tokens >= budget;
	if (!byShare && !byTokens) return undefined;
	const notice = byShare
		? `Compacting the context: it is at ${Math.round(usage.percent as number)}% and Jhon compacts at ${at}%.`
		: `Compacting the context: it is at ${Math.round((usage.tokens as number) / 1000)}K tokens and Jhon compacts at ${Math.round(budget / 1000)}K tokens.`;
	const attributes = {
		...(typeof usage.percent === "number" ? { "jhon.context.percent": Math.round(usage.percent) } : {}),
		"jhon.compact_at": at,
		...(byTokens ? { "jhon.compact_tokens": budget } : {}),
		...(typeof usage.tokens === "number" ? { "jhon.context.tokens": usage.tokens } : {}),
	};
	return { byShare, notice, attributes };
}

/** The message that continues a run a compaction stopped; it says it is not the owner (T194). */
export function continueNotice(): string {
	return fromTheShell("The context was compacted between the turns of your run, which stopped it. Continue the interrupted work from where the summary leaves it; do not repeat finished steps or ask the owner again.");
}

const headless = (ctx: any) => { try { return ctx?.mode === "print" || ctx?.mode === "json"; } catch { return false; } };

/** One compaction at a time for the main session. */
export class MainCompaction {
	private running = false;
	private readonly deps: CompactionDeps;
	constructor(deps: CompactionDeps) { this.deps = deps; }

	/** Whether a compaction is running: the context usage pi reports is stale until it ends. */
	get compacting(): boolean { return this.running; }

	private due(usage: ContextUsage) {
		return compactionDue(usage, this.deps.at(), this.deps.budget());
	}

	/** Starts a compaction if the context is due; resolves when it ends, true if one started. */
	private start(ctx: any, usage: ContextUsage, when: "reply" | "prompt" | "turn", onEnd?: () => void): Promise<boolean> | undefined {
		if (this.running || typeof ctx?.compact !== "function") return undefined;
		const due = this.due(usage);
		if (!due) return undefined;
		this.running = true;
		ctx.ui?.notify?.(due.notice, "info");
		this.deps.record?.("jhon.compaction", { ...due.attributes, "jhon.compaction.when": when });
		return new Promise<boolean>((resolve) => {
			const done = () => { this.running = false; try { onEnd?.(); } finally { resolve(true); } };
			try { ctx.compact({ onComplete: done, onError: done }); } catch { done(); }
		});
	}

	/** After a reply: compacts when due; true if it started (the reply's notices read a stale context). */
	afterReply(ctx: any, usage: ContextUsage): boolean {
		return this.start(ctx, usage, "reply") !== undefined;
	}

	/** Before an idle prompt: compacts when due and waits, so the prompt runs on the compacted context. */
	async beforePrompt(event: any, ctx: any): Promise<void> {
		if (event?.streamingBehavior) return;
		const started = this.start(ctx, ctx?.getContextUsage?.(), "prompt");
		if (started) await started;
	}

	/** After a turn that continues the run (tool results): compacts when due, then the run carries on. */
	afterTurn(event: any, ctx: any): void {
		if (!Array.isArray(event?.toolResults) || event.toolResults.length === 0 || headless(ctx)) return;
		void this.start(ctx, ctx?.getContextUsage?.(), "turn", () => {
			this.deps.send?.({ customType: COMPACTION_CONTINUE_ENTRY, content: continueNotice(), display: false }, { triggerTurn: true });
		});
	}
}
