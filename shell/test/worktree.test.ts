import "./isolated-env.ts";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { createWorktree, worktreeNote } from "../lib/worktree.ts";

const sh = (cwd: string, ...args: string[]) => execFileSync("git", args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });

test("a sub-agent's worktree sits beside the repository, on its own branch from HEAD, in the session's subfolder", (t) => {
  const base = realpathSync(mkdtempSync(join(tmpdir(), "wt-")));
  t.after(() => rmSync(base, { recursive: true, force: true }));
  const repo = join(base, "app");
  mkdirSync(join(repo, "src"), { recursive: true });
  sh(repo, "init", "-q");
  sh(repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init");
  const w = createWorktree(join(repo, "src"), "worker", 1_000_000);
  assert.equal(w.dir, join(base, "app-worktrees", "worker-lfls"));
  assert.equal(w.branch, "jhon/worker-lfls");
  assert.equal(w.cwd, w.dir, "src holds nothing git tracks, so it is not in the worktree: its root");
  assert.ok(existsSync(w.dir));
  assert.match(sh(repo, "branch", "--list", "jhon/*"), /jhon\/worker-lfls/);
  assert.match(worktreeNote(w), /git merge jhon\/worker-lfls[\s\S]*git worktree remove/);
  assert.throws(() => createWorktree(tmpdir() + "/nowhere-" + Date.now(), "worker"), /not in a git repository/);
});
