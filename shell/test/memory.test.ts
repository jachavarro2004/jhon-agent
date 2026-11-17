/**
 * Unit tests for shell/lib/memory.ts: pi's main session reads and writes the
 * project memory Claude Code keeps, named as Claude Code names it, shared by
 * every worktree of a clone, with its index loaded into the prompt.
 *
 *   jhon test
 */

import { test, after } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { MEMORY_INDEX_LINES, memoryDir, memoryPrompt, projectRoot } from "../lib/memory.ts";

const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
const tempDir = (prefix: string) => { const d = realpathSync(mkdtempSync(join(tmpdir(), prefix))); temps.push(d); return d; };

test("the memory directory is Claude Code's: every character but a letter or a digit of the root becomes -", () => {
  const env = { CLAUDE_CONFIG_DIR: "/c" };
  assert.equal(memoryDir("/home/u/.agents/jhon", env), "/c/projects/-home-u--agents-jhon/memory");
  assert.equal(memoryDir("/home/u/Projects/work/acme_app", env), "/c/projects/-home-u-Projects-work-acme-app/memory");
});

test("every worktree of a clone, and a directory inside it, share the main worktree's memory; outside Git it is the directory's", () => {
  const repo = tempDir("jhon-mem-repo-");
  const git = (...a: string[]) => spawnSync("git", ["-c", "user.email=t@t", "-c", "user.name=t", ...a], { cwd: repo, encoding: "utf8" });
  git("init", "-q");
  git("commit", "-q", "--allow-empty", "-m", "init");
  const linked = join(tempDir("jhon-mem-wt-"), "feature");
  assert.equal(git("worktree", "add", "-q", linked).status, 0);
  mkdirSync(join(repo, "sub"));
  assert.equal(projectRoot(repo), repo);
  assert.equal(projectRoot(join(repo, "sub")), repo);
  assert.equal(projectRoot(linked), repo);
  const plain = tempDir("jhon-mem-plain-");
  assert.equal(projectRoot(plain), plain);
});

test("the prompt names the directory and how to save, and loads MEMORY.md as it stands, up to its cap", () => {
  const dir = tempDir("jhon-mem-dir-");
  assert.match(memoryPrompt(dir), /MEMORY\.md does not exist yet\./);
  writeFileSync(join(dir, "MEMORY.md"), "- [Plan](plan.md) — slice 5 next\n");
  const prompt = memoryPrompt(dir);
  assert.ok(prompt.includes(`\`${dir}/\``), prompt);
  assert.match(prompt, /name: <short-kebab-case-slug>/);
  assert.match(prompt, /### MEMORY\.md\n\n- \[Plan\]\(plan\.md\) — slice 5 next$/);
  writeFileSync(join(dir, "MEMORY.md"), Array.from({ length: MEMORY_INDEX_LINES + 5 }, (_, i) => `- line ${i + 1}`).join("\n"));
  const long = memoryPrompt(dir);
  assert.ok(long.includes(`- line ${MEMORY_INDEX_LINES}\n`));
  assert.ok(!long.includes(`- line ${MEMORY_INDEX_LINES + 1}\n`));
  assert.match(long, /only the first 200 are loaded/);
});
