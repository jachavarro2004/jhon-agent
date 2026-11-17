/**
 * shell/lib/exploration-budget.ts — T117e (L745, L746): the main session's
 * reading per owner request. Past EXPLORATION_LOOKUPS lookups (read, grep,
 * find, ls, inspect, codegraph, a bash command that only reads) or
 * EXPLORATION_BYTES UTF-8 bytes they returned, the orchestrator hears once
 * per request that further discovery goes to one explore sub-agent. Advisory
 * only; it reaches the agent appended to the tool result, like the context
 * backstop, so the system prompt stays byte-stable (L708).
 */

/** Lookups a request may make before the notice. */
export const EXPLORATION_LOOKUPS = 3;

/** UTF-8 bytes the lookups may return before the notice. */
export const EXPLORATION_BYTES = 40_000;

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

export function explorationNotice(lookups: number, bytes: number, mode: "inline" | "writer" | undefined): string {
	const tail = mode === "writer"
		? "the reading that prepares a worker belongs in its brief."
		: "reading that prepares an understood inline write is exempt.";
	return `Exploration budget: ${lookups} lookups, ${bytes} bytes this request. Send further discovery to one explore sub-agent with a short handoff; ${tail}`;
}

/** The lookups and bytes since the owner's last message; one notice per request. */
export class ExplorationBudget {
	private lookups = 0;
	private bytes = 0;
	private notified = false;

	/** The owner's new message. */
	reset(): void {
		this.lookups = 0;
		this.bytes = 0;
		this.notified = false;
	}

	/** The notice for this tool result, once per request; undefined otherwise. */
	result(event: { toolName?: unknown; input?: unknown; content?: unknown; isError?: unknown }, mode: () => "inline" | "writer" | undefined): string | undefined {
		if (event.isError || typeof event.toolName !== "string") return undefined;
		if (!isLookup(event.toolName, event.input as Record<string, unknown> | undefined)) return undefined;
		this.lookups++;
		for (const part of Array.isArray(event.content) ? event.content : []) {
			if (part?.type === "text" && typeof part.text === "string") this.bytes += Buffer.byteLength(part.text, "utf8");
		}
		if (this.notified || (this.lookups <= EXPLORATION_LOOKUPS && this.bytes <= EXPLORATION_BYTES)) return undefined;
		this.notified = true;
		return explorationNotice(this.lookups, this.bytes, mode());
	}
}
