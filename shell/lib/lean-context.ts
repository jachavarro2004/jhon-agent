/**
 * shell/lib/lean-context.ts — T146h (L1278, L1280): a small task starts lean.
 *
 * pi's main session appends the lean prompt (jhon.json `lean`, prose/lean.md
 * rendered) instead of the orchestrator when `jhon hook session-brief` says
 * the session starts lean: no change is active, or the active one is small.
 * The full context loads once per session, as one persisted `jhon-full`
 * message, when `jhon status` says the change is not small or a sub-agent is
 * launched (T3), or when the agent makes or selects a change the engine then
 * calls not small (T192 S7, changeMoment); the system prompt never changes, so its cached prefix holds
 * (T115d). Until then Jhon's own tools and the skills list wait behind
 * tools_more (L3); todo, ask_user, inspect, codegraph and the delegation
 * tools stay (T192 S3: a lean start never takes delegation away, on any
 * provider; pi-claude-bridge reads the tool list only when a prompt starts,
 * so a tool held back at the start would miss the rest of that prompt). The
 * todo extension reads the session's level to skip its start rule and nudge (L4).
 */
import { dirname, isAbsolute, resolve } from "node:path";

/** The custom message that carries the full context into a lean session. */
export const FULL_MESSAGE = "jhon-full";

/** T192 S3: the delegation tools, offered from a main session's first request, lean or full. */
export const DELEGATION_TOOLS: ReadonlySet<string> = new Set(["subagent", "subagent_continue", "task_list", "task_result", "task_stop", "bash_background", "monitor"]);

/** Jhon's own tools a lean session defers until the full context loads; never a delegation tool. */
export const LEAN_DEFERRED = new Set(["jhon_review", "m365_read"]);

/** Whether a lean main session defers name: Jhon's own tools above, and every Engram tool; a delegation tool never. */
export function leanDeferred(name: string): boolean {
	return !DELEGATION_TOOLS.has(name) && (LEAN_DEFERRED.has(name) || name.startsWith("mcp__engram__"));
}

/** The line `jhon status` prints for a change that is not small (engine/internal/ldd NotSmallNote). */
const NOT_SMALL = "not small: read ";
const STATUS = /(?:^|[;&|(]\s*|\s)jhon\s+status\b/;

/** The jhon MCP server's status tool as pi names it (`mcp__<server>__<tool>`): `jhon status --json`. */
export const MCP_STATUS = "mcp__jhon__status";

/**
 * Whether a tool result is the moment the full context is due: status's
 * not-small line, read through bash, inspect or the jhon MCP server's status
 * tool (T192 S5), or a sub-agent launch.
 */
export function fullTrigger(toolName: string, input: Record<string, unknown> | undefined, output: string): boolean {
	if (toolName === "subagent" || toolName === "subagent_continue") return true;
	if (!output.includes(NOT_SMALL)) return false;
	if (toolName === MCP_STATUS) return true;
	const command = typeof input?.command === "string" ? input.command : "";
	return (toolName === "bash" || toolName === "inspect") && STATUS.test(command);
}

/** A layout's changes folder: openspec/changes/ or ldd/changes/ (engine/internal/ldd/layout). */
const CHANGES = /(?:^|[\\/])(?:openspec|ldd)[\\/]changes[\\/]/;
/** A bash command that may make or select a change: a git branch switch, a jhon run or risk read, or one naming the changes folder. */
const CHANGE_COMMAND = /\bgit\s+(?:switch|checkout)\b|(?:^|[;&|(]\s*|\s)jhon\s+(?:executions|risk)\b|(?:^|[\s'"=/])(?:openspec|ldd)\/changes\//;

/**
 * T192 S7: where a lean session asks the engine whether a change that is not
 * small appeared, after a tool result that may have made or selected one: a
 * write or edit under a layout's changes folder (the directory written in),
 * or a bash command that switches branch, runs `jhon executions`/`jhon risk`,
 * or names the changes folder (the session's cwd). undefined for any other
 * tool result, so the engine is never asked on every tool.
 */
export function changeMoment(toolName: string, input: Record<string, unknown> | undefined, cwd: string): string | undefined {
	if (toolName === "write" || toolName === "edit") {
		const path = typeof input?.path === "string" ? input.path : typeof input?.file_path === "string" ? input.file_path : "";
		if (!path) return undefined;
		const file = isAbsolute(path) ? path : resolve(cwd, path);
		return CHANGES.test(file) ? dirname(file) : undefined;
	}
	if (toolName !== "bash") return undefined;
	const command = typeof input?.command === "string" ? input.command : "";
	return CHANGE_COMMAND.test(command) ? cwd : undefined;
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
