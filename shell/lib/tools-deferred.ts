/**
 * shell/lib/tools-deferred.ts — T115c/T115d (L707, L708): the tools the main
 * session declares to the model, decided once per session so the tool list
 * stays identical across requests.
 *
 * mcp.json mounts Engram and codegraph with `direct` exposure, and pi-web-access
 * registers its tools directly, so pi declares all of them on every request
 * (in the L689 session 57.5K chars of definitions, 33K of them never or
 * rarely called). The orchestrator keeps the Engram tools it uses; the rest of
 * Engram and the web tools are deferred: `tools_more` lists them and enables a
 * named one for the rest of the session. Codegraph without an index stays out
 * (T42c). pi's own tool_search reaches only non-`direct` tools, so the shell
 * keeps its own loader. A child keeps its `--tools` list.
 *
 * T133e (L1016): mcp.json also mounts context7 `direct`, the only exposure a
 * child's `--tools` can name. Its tools are explore's and worker's: the main
 * session never declares them, and tools_more neither lists nor enables them.
 */

import { CODEGRAPH_TOOL } from "./codegraph.ts";
import { leanDeferred } from "./lean-context.ts";

/** The Engram tools the orchestrator keeps declared (L707: the ones it calls; mem_session_summary for its handoff). */
export const KEPT_ENGRAM = new Set(["mem_search", "mem_save", "mem_get_observation", "mem_context", "mem_judge", "mem_judge_many", "mem_session_summary"].map((t) => `mcp__engram__${t}`));

/** pi-web-access's tools. */
export const WEB_TOOLS = new Set(["web_search", "fetch_content", "get_search_content", "source_check"]);

/** The prefix of context7's tools as pi's mcp.json mounts it. */
export const CONTEXT7_PREFIX = "mcp__context7__";

/** The loader's name. */
export const TOOLS_MORE = "tools_more";

/** The session entry that records a tool tools_more enabled. */
export const TOOLS_MORE_ENTRY = "jhon-tools-more";

/** Whether the main session defers a tool until tools_more enables it. */
export function deferrable(name: string): boolean {
	return WEB_TOOLS.has(name) || (name.startsWith("mcp__engram__") && !KEPT_ENGRAM.has(name));
}

/**
 * The session's tool decision: codegraph is in or out once (its index is
 * read at the first ask), deferred tools stay out until enabled.
 */
export class ToolPolicy {
	private enabled = new Set<string>();
	private codegraph = new Map<string, boolean>();
	private lean = false;

	private readonly main: boolean;
	private readonly indexed: (cwd: string) => boolean;

	constructor(main: boolean, indexed: (cwd: string) => boolean) {
		this.main = main;
		this.indexed = indexed;
	}

	/** Whether codegraph's tool is in for cwd, decided at the session's first ask there. */
	codegraphOn(cwd: string): boolean {
		let on = this.codegraph.get(cwd);
		if (on === undefined) {
			on = this.indexed(cwd);
			this.codegraph.set(cwd, on);
		}
		return on;
	}

	/** T146h L3: a lean session defers Jhon's own tools too, until the full context loads. */
	setLean(on: boolean): void {
		this.lean = on;
	}

	/** Whether the main session defers name now: always deferred, or deferred while lean. */
	deferred(name: string): boolean {
		return deferrable(name) || (this.lean && leanDeferred(name));
	}

	/** Whether the tool stays out of the requests. */
	held(name: string, cwd: string): boolean {
		if (name === CODEGRAPH_TOOL) return !this.codegraphOn(cwd);
		if (name.startsWith(CONTEXT7_PREFIX)) return this.main;
		return this.main && this.deferred(name) && !this.enabled.has(name);
	}

	/** names without the held ones; undefined when none is held, so nothing is set. */
	filter(names: readonly string[], cwd: string): string[] | undefined {
		const kept = names.filter((n) => !this.held(n, cwd));
		return kept.length === names.length ? undefined : kept;
	}

	enable(names: readonly string[]): void {
		for (const n of names) this.enabled.add(n);
	}

	/** A new session: decided again, with what its branch enabled. */
	reset(enabled: readonly string[] = []): void {
		this.enabled = new Set(enabled);
		this.codegraph.clear();
		this.lean = false;
	}
}

/** The tools a branch's tools_more entries enabled. */
export function enabledOnBranch(branch: readonly unknown[] | undefined): string[] {
	const out: string[] = [];
	for (const e of branch ?? []) {
		const entry = e as { type?: string; customType?: string; data?: { names?: unknown } };
		if (entry?.type !== "custom" || entry.customType !== TOOLS_MORE_ENTRY || !Array.isArray(entry.data?.names)) continue;
		for (const n of entry.data.names) if (typeof n === "string") out.push(n);
	}
	return out;
}

interface ToolsHost {
	getActiveTools?(): string[];
	setActiveTools?(names: string[]): void;
	getAllTools?(): Array<{ name: string; description?: string }>;
	appendEntry?(type: string, data: unknown): void;
}

const text = (t: string) => ({ content: [{ type: "text", text: t }], details: {} });

/** tools_more: lists the deferred tools, or enables named ones for the rest of the session. */
export function toolsMoreTool(pi: ToolsHost, policy: ToolPolicy) {
	return {
		name: TOOLS_MORE,
		label: "More tools",
		description: "Lists the deferred tools (memory tools such as mem_update, mem_review; web search and fetch; on a small task, subagent and the rest), or, given names, enables them for the session. Call with no names to list.",
		parameters: { type: "object", additionalProperties: false, properties: { names: { type: "array", items: { type: "string" }, description: "Tools to enable, from the list." } } },
		async execute(_id: string, params: { names?: unknown }, _signal?: unknown, _onUpdate?: unknown, ctx?: { cwd?: string }) {
			const cwd = ctx?.cwd ?? process.cwd();
			const deferred = (pi.getAllTools?.() ?? []).filter((t) => policy.deferred(t.name) && policy.held(t.name, cwd));
			const names = Array.isArray(params?.names) ? params.names.filter((n): n is string => typeof n === "string") : [];
			if (!names.length) {
				return text(deferred.length ? `Deferred tools (enable with names):\n${deferred.map((t) => `- ${t.name}: ${(t.description ?? "").trim().split(/\r?\n/)[0]}`).join("\n")}` : "No deferred tools.");
			}
			const known = new Set(deferred.map((t) => t.name));
			const ok = names.filter((n) => known.has(n)), bad = names.filter((n) => !known.has(n));
			if (ok.length) {
				policy.enable(ok);
				const active = pi.getActiveTools?.() ?? [];
				pi.setActiveTools?.([...active, ...ok.filter((n) => !active.includes(n))]);
				pi.appendEntry?.(TOOLS_MORE_ENTRY, { names: ok });
			}
			return text([ok.length ? `Enabled: ${ok.join(", ")} (callable from the next step on).` : "", bad.length ? `Unknown or not deferred: ${bad.join(", ")}.` : ""].filter(Boolean).join("\n"));
		},
	};
}
