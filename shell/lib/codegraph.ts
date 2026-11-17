import { existsSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { basename, dirname, join, resolve } from "node:path";
import { gitReadEnv } from "./git-read.ts";

/** codegraph's tool as pi's mcp.json mounts it. */
export const CODEGRAPH_TOOL = "mcp__codegraph__codegraph_explore";

// CodeGraph keeps a project's index in <root>/.codegraph/codegraph.db;
// CODEGRAPH_DIR renames that directory, and WSL may use .codegraph-wsl.
function dataDirs(env: NodeJS.ProcessEnv): string[] {
	const named = env.CODEGRAPH_DIR;
	const custom = named && !/[\\/]/.test(named) && named !== "." && named !== ".." ? [named] : [];
	return [...custom, ".codegraph", ".codegraph-wsl"];
}

/** The root CodeGraph indexes for cwd, which its paths are relative to: the git top level, else cwd. */
export function codegraphRoot(cwd: string): string {
	const r = spawnSync("git", ["-C", cwd, "rev-parse", "--show-toplevel"], { encoding: "utf8", env: gitReadEnv() });
	return r.status === 0 && r.stdout.trim() ? r.stdout.trim() : resolve(cwd);
}

const hasIndex = (root: string, env: NodeJS.ProcessEnv) => dataDirs(env).some((d) => existsSync(join(root, d, "codegraph.db")));

/** Whether the repository holding cwd (its git top level, else cwd) has a CodeGraph index. */
export function codegraphIndexed(cwd: string, env: NodeJS.ProcessEnv = process.env): boolean {
	return hasIndex(codegraphRoot(cwd), env);
}

/**
 * The checkout whose CodeGraph index can answer for cwd: cwd's own root when
 * it is indexed; else, when cwd is in a linked worktree (a change's worktree
 * has no index of its own, L1024), the main checkout when that one is
 * indexed; else undefined. It only reads: no index is built or written.
 */
export function codegraphIndexRoot(cwd: string, env: NodeJS.ProcessEnv = process.env): string | undefined {
	const root = codegraphRoot(cwd);
	if (hasIndex(root, env)) return root;
	const r = spawnSync("git", ["-C", cwd, "rev-parse", "--path-format=absolute", "--git-common-dir", "--git-dir"], { encoding: "utf8", env: gitReadEnv() });
	if (r.status !== 0) return undefined;
	const [common, own] = r.stdout.trim().split("\n");
	// A linked worktree's git dir is <common>/worktrees/<name>; the main
	// checkout is the parent of a common dir named .git (a bare one has none).
	if (!common || !own || common === own || basename(common) !== ".git") return undefined;
	const main = dirname(common);
	return main !== root && hasIndex(main, env) ? main : undefined;
}

// The rule that indexes a new repository: it stays in an unindexed one,
// word for word, up to the end of its first sentence.
const INIT_RULE = /- In a real project whose root[\s\S]*?`codegraph init <root>`[\s\S]*?\./;

/** text with its `## CodeGraph` section (up to the next `#`/`##` heading) cut to its init rule. */
export function withoutCodegraph(text: string): string {
	return text.replace(/(^|\n)## CodeGraph\n[\s\S]*?(?=\n#{1,2} |$)/, (section, lead: string) => {
		const init = INIT_RULE.exec(section)?.[0];
		return init ? `${lead}## CodeGraph\n\n${init}\n` : "";
	});
}

/** options' tools without codegraph's, in place. */
export function dropCodegraphTool(options: { selectedTools?: string[] } | undefined): void {
	const tools = options?.selectedTools;
	if (!Array.isArray(tools)) return;
	const at = tools.indexOf(CODEGRAPH_TOOL);
	if (at >= 0) tools.splice(at, 1);
}
