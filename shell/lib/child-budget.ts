/**
 * shell/lib/child-budget.ts — T122 (L762 F2, L763): a sub-agent child's own
 * budget. The main session's ladder (delegation notice, fresh-session notice,
 * compaction) never reaches a child. A child (JHON_PI_CHILD=1) gets:
 *
 * - a context budget: once per crossing of min(shell.behaviour.child_backstop_tokens
 *   (CHILD_BACKSTOP_TOKENS; 0 off), CHILD_BACKSTOP_SHARE of the window), the
 *   notice to wrap up and return path:line evidence, not raw output;
 * - for an explore or worker-type child that may call CodeGraph, one
 *   read-budget reminder past CHILD_READ_LOOKUPS lookups or CHILD_READ_BYTES
 *   bytes since its last CodeGraph call (lookups as lib/exploration-budget.ts
 *   counts them: read, grep, find, ls, and a bash command that only reads,
 *   e.g. cat, sed -n, grep through bash). T125 (L783 F5): four workers read
 *   362 KB through bash and never called CodeGraph; a worker's reminder
 *   (workerReadBudgetNotice) is about editing, not exploring.
 *
 * T155 (L1377, L1394): no cap. A child's tool calls are never refused for its
 * context size and it is never stopped for it: the context is the owner's
 * (shell.behaviour.child_cap_tokens is retired).
 *
 * Every notice is appended to a tool result, the T115e channel, so the
 * child's system prompt stays byte-identical (L708).
 */

import { ContextBackstop } from "./context-backstop.ts";
import { isLookup } from "./exploration-budget.ts";
import { shellSettings } from "./settings.ts";

/** The default child budget, in tokens; 0 turns it off. */
export const CHILD_BACKSTOP_TOKENS = 80000;
/** The share of the window, in percent, past which the child budget comes if it is smaller. */
export const CHILD_BACKSTOP_SHARE = 40;
/** Lookups an explore child may make since its last CodeGraph call before the reminder. */
export const CHILD_READ_LOOKUPS = 12;
/** UTF-8 bytes those lookups may return before the reminder. */
export const CHILD_READ_BYTES = 100_000;

const k = (tokens: number) => Math.round(tokens / 1000);

export function childBudgetNotice(tokens: number, threshold: number): string {
  return `Context budget: this sub-agent's context is at ${k(tokens)}K tokens, past its ${k(threshold)}K-token budget. Wrap up: finish the step in hand, then return what you found as path:line evidence and decisions, not raw output, and what remains.`;
}

export function readBudgetNotice(lookups: number, bytes: number): string {
  return `Read budget: ${lookups} lookups, ${bytes} bytes without a CodeGraph call. codegraph_explore returns the source of the symbols it finds, already read: prefer it to more grep and read, and return path:line evidence, not raw output.`;
}

/** T125: a worker-type child's reminder: CodeGraph instead of reading whole files, the read tool's ranges instead of cat/sed. */
export function workerReadBudgetNotice(lookups: number, bytes: number): string {
  return `Read budget: ${lookups} lookups, ${bytes} bytes without CodeGraph. codegraph_explore returns the source of a symbol already read; read only the lines you change; prefer the read tool with offset/limit over cat/sed through bash.`;
}

/** A child's context budget: a notice once per crossing, never a refusal. */
export class ChildContextBudget {
  private readonly backstop: ContextBackstop;

  constructor(budget: () => number = () => shellSettings().childBackstopTokens()) {
    this.backstop = new ContextBackstop(budget, CHILD_BACKSTOP_SHARE, childBudgetNotice);
  }

  /** The budget notice for this tool result, read at most once per BACKSTOP_INTERVAL_MS. */
  measure(ctx: any): string | undefined {
    return this.backstop.measure(ctx);
  }
}

/** An explore or worker child's lookups since its last CodeGraph call; one reminder per child. */
export class ChildReadBudget {
  private lookups = 0;
  private bytes = 0;
  private notified = false;

  private readonly kind: "explore" | "worker";

  constructor(kind: "explore" | "worker" = "explore") { this.kind = kind; }

  result(event: { toolName?: unknown; input?: unknown; content?: unknown; isError?: unknown }): string | undefined {
    if (this.notified || event.isError || typeof event.toolName !== "string") return undefined;
    if (event.toolName.includes("codegraph")) { this.lookups = 0; this.bytes = 0; return undefined; }
    if (!isLookup(event.toolName, event.input as Record<string, unknown> | undefined)) return undefined;
    this.lookups++;
    for (const part of Array.isArray(event.content) ? event.content : []) {
      if (part?.type === "text" && typeof part.text === "string") this.bytes += Buffer.byteLength(part.text, "utf8");
    }
    if (this.lookups <= CHILD_READ_LOOKUPS && this.bytes <= CHILD_READ_BYTES) return undefined;
    this.notified = true;
    return (this.kind === "worker" ? workerReadBudgetNotice : readBudgetNotice)(this.lookups, this.bytes);
  }
}

/** The read budget's kind for a child agent: explore, a worker-type writer (worker, jd-fix-agent, *-worker), or none. */
export function readBudgetKind(agent: string | undefined): "explore" | "worker" | undefined {
  if (agent === "explore") return "explore";
  if (agent === "worker" || agent === "jd-fix-agent" || /-worker$/.test(agent ?? "")) return "worker";
  return undefined;
}

/** Whether a child running `agent` with `tools` gets the read budget: an explore or worker-type child that may call CodeGraph. */
export function readBudgetApplies(agent: string | undefined, tools: string[] | undefined): boolean {
  return readBudgetKind(agent) !== undefined && (tools ?? []).some((t) => t.includes("codegraph"));
}
