/**
 * shell/lib/worktree.ts — a git worktree of its own for a sub-agent that
 * writes in parallel with others (`subagent` with worktree: true).
 *
 * It sits where the codegraph contract places worktrees, beside the
 * repository: `<repo-parent>/<repo>-worktrees/<agent>-<stamp>`, on a new
 * branch `jhon/<agent>-<stamp>` from HEAD, and the sub-agent runs in the same
 * subfolder of it as the session runs in the repository. Nothing is merged or
 * removed here: the orchestrator integrates the branch and removes the tree.
 */

import { execFileSync } from "node:child_process";
import { existsSync, realpathSync } from "node:fs";
import { basename, dirname, join, relative } from "node:path";

export interface AgentWorktree {
  /** The worktree's root. */
  dir: string;
  /** Where the sub-agent runs: the session's subfolder inside the worktree. */
  cwd: string;
  branch: string;
}

type Git = (args: string[], cwd: string) => string;

const git: Git = (args, cwd) => execFileSync("git", args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();

/** A new worktree for agent, branched from HEAD of the repository cwd is in. */
export function createWorktree(cwd: string, agent: string, now = Date.now(), run: Git = git): AgentWorktree {
  let root: string;
  try {
    root = run(["rev-parse", "--show-toplevel"], cwd);
  } catch {
    throw new Error(`jhon: ${cwd} is not in a git repository; a sub-agent's worktree needs one`);
  }
  const name = `${agent.replace(/[^A-Za-z0-9._-]+/g, "-")}-${now.toString(36)}`;
  const dir = join(dirname(root), `${basename(root)}-worktrees`, name);
  const branch = `jhon/${name}`;
  try {
    run(["worktree", "add", "-b", branch, dir, "HEAD"], root);
  } catch (error) {
    const why = error instanceof Error && "stderr" in error ? String((error as { stderr: unknown }).stderr).trim() : String(error);
    throw new Error(`jhon: could not create the worktree ${dir}: ${why}`);
  }
  // A subfolder git does not track is not in the worktree: its root then.
  const sub = join(dir, relative(root, cwd));
  return { dir, cwd: existsSync(sub) ? sub : dir, branch };
}

/** What the orchestrator reads after a sub-agent worked in its own worktree. */
export function worktreeNote(w: AgentWorktree): string {
  return `The sub-agent worked in its own worktree ${w.dir}, on branch ${w.branch} (from HEAD: it did not see uncommitted changes). Integrate it on the change's branch with \`git merge ${w.branch}\` and run the affected tests; then remove it with \`git worktree remove ${w.dir}\` and \`git branch -d ${w.branch}\`.`;
}

/**
 * T81: the folder a session in the main checkout names for `subagent` or
 * `inspect` (the change's worktree), resolved to its top level. It must be a
 * git worktree of the session's own repository: the same git common dir.
 */
export function sameRepoWorktree(sessionCwd: string, dir: string, run: Git = git): string {
  const rule = `jhon: ${dir} must be a git worktree of this session's repository (the same git common dir); pass the change's worktree, or omit cwd`;
  const common = (cwd: string) => realpathSync(run(["rev-parse", "--path-format=absolute", "--git-common-dir"], cwd));
  let top: string;
  try {
    if (common(dir) !== common(sessionCwd)) throw new Error("another repository");
    top = realpathSync(run(["rev-parse", "--show-toplevel"], dir));
  } catch {
    throw new Error(rule);
  }
  return top;
}
