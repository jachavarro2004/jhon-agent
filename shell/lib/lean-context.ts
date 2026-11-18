/**
 * shell/lib/lean-context.ts — T146h (L1278, L1280): a small task starts lean.
 *
 * pi's main session appends the lean prompt (jhon.json `lean`, prose/lean.md
 * rendered) instead of the orchestrator when `jhon hook session-brief` says
 * the session starts lean: no change is active, or the active one is small.
 * The full context loads once per session, as one persisted `jhon-full`
 * message, when `jhon status` says the change is not small or a sub-agent is
 * launched (T3); the system prompt never changes, so its cached prefix holds
 * (T115d). Until then Jhon's own tools and the skills list wait behind
 * tools_more (L3); todo, ask_user, inspect and codegraph stay. The todo
 * extension reads the session's level to skip its start rule and nudge (L4).
 */

/** The custom message that carries the full context into a lean session. */
export const FULL_MESSAGE = "jhon-full";

/** Jhon's own tools a lean session defers until the full context loads. */
export const LEAN_DEFERRED = new Set(["jhon_review", "subagent", "subagent_continue", "m365_read", "bash_background", "monitor", "task_list", "task_result", "task_stop"]);

/** Whether a lean main session defers name: Jhon's own tools above, and every Engram tool. */
export function leanDeferred(name: string): boolean {
	return LEAN_DEFERRED.has(name) || name.startsWith("mcp__engram__");
}

/** The line `jhon status` prints for a change that is not small (engine/internal/ldd NotSmallNote). */
const NOT_SMALL = "not small: read ";
const STATUS = /(?:^|[;&|(]\s*|\s)jhon\s+status\b/;

/** Whether a tool result is the moment the full context is due: status's not-small line, or a sub-agent launch. */
export function fullTrigger(toolName: string, input: Record<string, unknown> | undefined, output: string): boolean {
	if (toolName === "subagent" || toolName === "subagent_continue") return true;
	const command = typeof input?.command === "string" ? input.command : "";
	return toolName === "bash" && STATUS.test(command) && output.includes(NOT_SMALL);
}

/** Whether the session's branch already holds the full message (a resumed session). */
export function fullOnBranch(branch: readonly unknown[] | undefined): boolean {
	return (branch ?? []).some((e) => (e as { customType?: string })?.customType === FULL_MESSAGE);
}

/** The context the main session runs with: lean, full, or undefined before its first run. */
export type ContextLevel = "lean" | "full" | undefined;

const SLOT = Symbol.for("jhon-shell.context-level");
const slot = (): { level?: ContextLevel } => ((globalThis as Record<symbol, { level?: ContextLevel }>)[SLOT] ??= {});

/** The main session's context level, shared across the shell's extensions. */
export function contextLevel(): ContextLevel {
	return slot().level;
}

export function setContextLevel(level: ContextLevel): void {
	slot().level = level;
}
