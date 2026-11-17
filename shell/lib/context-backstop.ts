/**
 * T115e (L705, L715): the context backstop, a delegation signal for the main
 * session only. Once its context passes min(shell.behaviour.backstop_tokens
 * (BACKSTOP_TOKENS; 0 turns it off), BACKSTOP_SHARE of the model's window), the
 * orchestrator hears once to delegate its next unit that needs several steps,
 * with a short handoff, and keep single small edits and state checks inline.
 * The ladder: this backstop (100K), the fresh-session notice (150K,
 * lib/fresh-session.ts), auto-compaction (200K).
 *
 * ctx.getContextUsage() is read at most once per BACKSTOP_INTERVAL_MS; a
 * compaction lets the next read come at once, and a read below the threshold
 * re-arms it. The notice reaches the agent appended to a tool result or as a
 * persisted message, never in the system prompt (T115c/d: the cached prefix holds).
 */

import { shellSettings } from "./settings.ts";

/** The custom message that hands the agent the backstop after a reply. */
export const BACKSTOP_ENTRY = "jhon-context-backstop";

/** The default backstop, in tokens; 0 turns it off. */
export const BACKSTOP_TOKENS = 100000;

/** The share of the model's window, in percent, past which the backstop comes if it is smaller. */
export const BACKSTOP_SHARE = 15;

/** The least time between two reads of the context usage. */
export const BACKSTOP_INTERVAL_MS = 5000;

/** The tokens the backstop fires at for `budget` and a window, or undefined when it is off. */
export function backstopThreshold(budget: number, contextWindow: unknown, sharePercent = BACKSTOP_SHARE): number | undefined {
  if (!(budget > 0)) return undefined;
  const share = typeof contextWindow === "number" && contextWindow > 0 ? Math.floor((contextWindow * sharePercent) / 100) : Infinity;
  return Math.min(budget, share);
}

export function backstopNotice(tokens: number, threshold: number): string {
  return `Context backstop: the context is at ${Math.round(tokens / 1000)}K tokens, past its ${Math.round(threshold / 1000)}K-token backstop. Delegate the next unit that needs several steps (its reads, edits and checks) to a sub-agent with a short handoff (paths, line ranges, decisions); keep single small edits and state checks inline.`;
}

interface Usage { tokens?: unknown; contextWindow?: unknown }

/** Once per crossing; the budget is read per measurement so a changed setting applies. */
export class ContextBackstop {
  private armed = true;
  private last = -Infinity;
  private readonly budget: () => number;
  private readonly share: number;
  private readonly notice: (tokens: number, threshold: number) => string;

  /** T122: a child's budget reuses this latch with its own share and notice (lib/child-budget.ts). */
  constructor(budget: () => number = () => shellSettings().backstopTokens(), share = BACKSTOP_SHARE, notice = backstopNotice) {
    this.budget = budget;
    this.share = share;
    this.notice = notice;
  }

  /** The notice, reading ctx.getContextUsage() unless it was read in the last BACKSTOP_INTERVAL_MS. */
  measure(ctx: any): string | undefined {
    if (!(this.budget() > 0) || typeof ctx?.getContextUsage !== "function") return undefined;
    if (Date.now() - this.last < BACKSTOP_INTERVAL_MS) return undefined;
    let usage: Usage | undefined;
    try { usage = ctx.getContextUsage(); } catch { usage = undefined; }
    return this.observe(usage);
  }

  /** The notice for a usage already read (it counts as this window's read). */
  observe(usage: Usage | undefined): string | undefined {
    this.last = Date.now();
    const threshold = backstopThreshold(this.budget(), usage?.contextWindow, this.share);
    const tokens = usage?.tokens;
    if (threshold === undefined || typeof tokens !== "number") return undefined;
    if (tokens < threshold) { this.armed = true; return undefined; }
    if (!this.armed) return undefined;
    this.armed = false;
    return this.notice(tokens, threshold);
  }

  /** A compaction: the next read comes at once, and re-arms if it is below the threshold. */
  compacted(): void {
    this.last = -Infinity;
  }

  /** A new session: armed, unread. */
  reset(): void {
    this.armed = true;
    this.last = -Infinity;
  }
}
