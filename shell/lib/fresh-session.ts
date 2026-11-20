/**
 * One session per change (S35): the main session suggests a fresh one, with
 * a handoff, once when its context passes ten points of its window before
 * shell.behaviour.compact_at or, whichever is first, three quarters of
 * shell.behaviour.compact_tokens (0 is no budget, T115a: a share of a very
 * large window never comes) after a reply, and once per change `jhon archive` archived. T155
 * (L1394) derived both from the compaction keys. The ladder's middle rung
 * (T115e, L715): after the context backstop (lib/context-backstop.ts), before
 * compaction. A notice, never a block. The wording is engine/internal/hook
 * FreshSession's, which `jhon archive` prints and Claude Code's
 * context-notice gives.
 */

import { shellSettings } from "./settings.ts";

/** The custom message that hands the agent the context notice. */
export const FRESH_SESSION_ENTRY = "jhon-fresh-session";

export function freshSessionNotice(why: string, handoff: string): string {
  return `One session per change: ${why} Save a handoff (\`${handoff}\`, then mem_session_summary), then start a fresh session (/new in pi; /clear or a restart in Claude Code) and give it the handoff.`;
}

const ARCHIVE = /(?:^|[\s;&|(])jhon\s+archive\s+["']?([A-Za-z0-9][A-Za-z0-9._-]*)/;
const LINE = /^One session per change: .*$/m;

/** Each notice at most once: the context one per session, the archive one per change. */
export class FreshSessionLatch {
  private contextShown = false;
  private readonly archived = new Set<string>();
  /** The share of the window past which the context notice is due: the owner's settings by default. */
  private readonly at: number;
  /** The context tokens past which it is due, whichever comes first; 0 is no budget. */
  private readonly budget: number;

  constructor(at: number = shellSettings().freshSessionAt(), budget: number = shellSettings().freshSessionTokens()) {
    this.at = at;
    this.budget = budget;
  }

  /** The notice for a reply that left the context at `percent` of its window and `tokens`, or undefined; it names the limit reached. */
  context(percent: unknown, tokens?: unknown): string | undefined {
    if (this.contextShown) return undefined;
    let why: string | undefined;
    if (typeof percent === "number" && percent >= this.at) why = `the context is at ${Math.round(percent)}% of its window, and every turn re-reads it.`;
    else if (this.budget > 0 && typeof tokens === "number" && tokens >= this.budget) why = `the context is at ${Math.round(tokens / 1000)}K tokens, past its ${Math.round(this.budget / 1000)}K-token budget, and every turn re-reads it.`;
    if (!why) return undefined;
    this.contextShown = true;
    return freshSessionNotice(why, "jhon context <change> --phase none --handoff");
  }

  /** The notice line of a bash call that archived a change, or undefined. */
  archive(command: unknown, output: string): string | undefined {
    const change = typeof command === "string" ? ARCHIVE.exec(command)?.[1] : undefined;
    const line = change ? LINE.exec(output)?.[0] : undefined;
    if (!change || !line || this.archived.has(change)) return undefined;
    this.archived.add(change);
    return line;
  }
}
