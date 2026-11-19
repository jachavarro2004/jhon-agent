/**
 * shell/lib/exploration-budget.ts — what counts as a lookup: read, grep, find,
 * ls, inspect, codegraph, and a bash command that only reads. A sub-agent's
 * CodeGraph read budget counts with it (lib/child-budget.ts, T125, T134).
 *
 * T155 (L1377, L1394): the main session's exploration-budget notice (T117e)
 * is gone; how much it reads is the owner's.
 */

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
