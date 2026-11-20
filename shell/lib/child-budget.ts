/**
 * shell/lib/child-budget.ts — T122 (L762 F2, L763): a sub-agent child's own
 * budget. The main session's ladder (delegation notice, fresh-session notice,
 * compaction) never reaches a child. A child (JHON_PI_CHILD=1) gets:
 *
 * - a context budget: once per crossing of min(shell.behaviour.child_backstop_tokens
 *   (0 off), CHILD_BACKSTOP_SHARE of the window), the notice to wrap up and
 *   return path:line evidence, not raw output;
 * - for a jhon-scout or builder-type child that may call CodeGraph, one
 *   read-budget reminder past CHILD_READ_LOOKUPS lookups or CHILD_READ_BYTES
 *   bytes since its last CodeGraph call (lookups as isLookup counts them:
 *   read, grep, find, ls, inspect, codegraph, and a bash command that only
 *   reads, e.g. cat, sed -n, grep through bash). T125 (L783 F5): four
 *   builders read 362 KB through bash and never called CodeGraph; a jhon-builder's reminder
 *   (workerReadBudgetNotice) is about editing, not exploring.
 *
 * T155 (L1377, L1394): no cap. A child's tool calls are never refused for its
 * context size and it is never stopped for it: the context is the owner's
 * (shell.behaviour.child_cap_tokens is retired).
 *
 * Every notice is appended to a tool result, the T115e channel, so the
 * child's system prompt stays byte-identical (L708).
 *
 * T166 (L1438): the lookup classifier (isLookup, readOnlyBash), once
 * lib/exploration-budget.ts, lives here, its only user. The main session's
 * exploration-budget notice (T117e) is gone since T155 (L1377, L1394).
 */

import { ContextBackstop } from "./context-backstop.ts";
import { shellSettings } from "./settings.ts";

/** The share of the window, in percent, past which the child budget comes if it is smaller. */
export const CHILD_BACKSTOP_SHARE = 40;
/** Lookups a jhon-scout child may make since its last CodeGraph call before the reminder. */
export const CHILD_READ_LOOKUPS = 12;
/** UTF-8 bytes those lookups may return before the reminder. */
export const CHILD_READ_BYTES = 100_000;

const READ_TOOLS = new Set(["read", "grep", "find", "ls", "inspect", "codegraph", "codegraph_query", "codegraph_explore", "mcp__codegraph__codegraph_query", "mcp__codegraph__codegraph_explore"]);

// Commands that only read; git only with a reading subcommand, sed only with -n, find never with an action.
const READ_COMMANDS = new Set(["cat", "head", "tail", "less", "ls", "tree", "wc", "grep", "rg", "ag", "find", "fd", "stat", "file", "du", "jq", "nl", "sort", "uniq", "cut", "awk", "sed", "git", "pwd", "realpath", "dirname", "basename", "echo"]);
const GIT_READS = new Set(["log", "show", "diff", "status", "grep", "blame", "ls-files", "rev-parse", "branch"]);

// T134 (L1021 H3): one leading `cd <dir> &&`, how most of the children's reads open.
const LEADING_CD = /^\s*cd\s+(?:'[^']*'|"[^"]*"|[^\s;&|'"]+)\s*&&/;

/** Whether a bash command only reads: every segment of its pipes and chains is a reading command, with no redirection into a file; one leading `cd <dir> &&` is allowed. */
export function readOnlyBash(command: unknown): boolean {
  if (typeof command !== "string" || !command.trim()) return false;
  if (/[`]|\$\(|>(?!&)|<\(/.test(command.replace(/2>&1|2>\/dev\/null|>\/dev\/null/g, ""))) return false;
  const segments = command.replace(LEADING_CD, "").split(/\|\||&&|[|;\n]/).map((s) => s.trim()).filter(Boolean);
  return segments.length > 0 && segments.every((segment) => {
    const words = segment.split(/\s+/).filter((w) => !/^\w+=/.test(w));
    const [cmd, ...args] = words;
    if (!cmd || !READ_COMMANDS.has(cmd)) return false;
    if (cmd === "git") return GIT_READS.has(args.find((a) => !a.startsWith("-")) ?? "");
    if (cmd === "sed") return args.includes("-n") && !args.some((a) => /^-i/.test(a));
    if (cmd === "find") return !args.some((a) => /^-(delete|exec|execdir|ok|fprint)/.test(a));
    return true;
  });
}

/** Whether a tool call is a lookup. */
export function isLookup(toolName: string, input: Record<string, unknown> | undefined): boolean {
  if (READ_TOOLS.has(toolName)) return true;
  return toolName === "bash" && readOnlyBash(input?.command);
}

const k = (tokens: number) => Math.round(tokens / 1000);

export function childBudgetNotice(tokens: number, threshold: number): string {
  return `Context budget: this sub-agent's context is at ${k(tokens)}K tokens, past its ${k(threshold)}K-token budget. Wrap up: finish the step in hand, then return what you found as path:line evidence and decisions, not raw output, and what remains.`;
}

export function readBudgetNotice(lookups: number, bytes: number): string {
  return `Read budget: ${lookups} lookups, ${bytes} bytes without a CodeGraph call. codegraph_explore returns the source of the symbols it finds, already read: prefer it to more grep and read, and return path:line evidence, not raw output.`;
}

/** T125: a builder-type child's reminder: CodeGraph instead of reading whole files, the read tool's ranges instead of cat/sed. */
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

/** A jhon-scout or builder child's lookups since its last CodeGraph call; one reminder per child. */
export class ChildReadBudget {
  private lookups = 0;
  private bytes = 0;
  private notified = false;

  private readonly kind: "scout" | "builder";

  constructor(kind: "scout" | "builder" = "scout") { this.kind = kind; }

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
    return (this.kind === "builder" ? workerReadBudgetNotice : readBudgetNotice)(this.lookups, this.bytes);
  }
}

/** The read budget's kind for a child agent: jhon-scout, a builder-type writer (jhon-builder, *-builder, *-worker), or none. */
export function readBudgetKind(agent: string | undefined): "scout" | "builder" | undefined {
  if (agent === "jhon-scout") return "scout";
  if (/-(builder|worker)$/.test(agent ?? "")) return "builder";
  return undefined;
}

/** Whether a child running `agent` with `tools` gets the read budget: a jhon-scout or builder-type child that may call CodeGraph. */
export function readBudgetApplies(agent: string | undefined, tools: string[] | undefined): boolean {
  return readBudgetKind(agent) !== undefined && (tools ?? []).some((t) => t.includes("codegraph"));
}
