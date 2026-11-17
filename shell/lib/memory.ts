/**
 * shell/lib/memory.ts — the project memory pi shares with Claude Code.
 *
 * Claude Code keeps a file memory per project under
 * `<config dir>/projects/<slug>/memory/`: one file per memory and an index,
 * MEMORY.md, that it loads into every main session. pi's main session reads
 * and writes the same directory, so a plan or a lesson one runtime saved is
 * the other's too, with no copy to keep in step. The slug is the project
 * root with every character but a letter or a digit turned into `-`, and the
 * root is the main worktree, so every worktree of a clone shares one memory,
 * as Claude Code's does.
 */

import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { gitReadEnv } from "./git-read.ts";

/** The lines of MEMORY.md a session loads; Claude Code loads the same. */
export const MEMORY_INDEX_LINES = 200;

/** The clone's main worktree, or cwd itself outside Git. */
export function projectRoot(cwd: string): string {
	const r = spawnSync("git", ["-C", cwd, "rev-parse", "--path-format=absolute", "--git-common-dir"], { encoding: "utf8", env: gitReadEnv() });
	const common = r.status === 0 ? r.stdout.trim() : "";
	if (!common) return resolve(cwd);
	return basename(common) === ".git" ? dirname(common) : resolve(cwd);
}

/** The memory directory of the project at root, as Claude Code names it. */
export function memoryDir(root: string, env: NodeJS.ProcessEnv = process.env): string {
	const config = env.CLAUDE_CONFIG_DIR || join(homedir(), ".claude");
	return join(config, "projects", root.replace(/[^a-zA-Z0-9]/g, "-"), "memory");
}

/** What the main session is told about the memory in dir, with its index as it stands. */
export function memoryPrompt(dir: string): string {
	const index = join(dir, "MEMORY.md");
	let loaded = "";
	if (existsSync(index)) {
		const lines = readFileSync(index, "utf8").split("\n");
		loaded = lines.slice(0, MEMORY_INDEX_LINES).join("\n").trim();
		if (lines.length > MEMORY_INDEX_LINES) loaded += `\n\n(MEMORY.md has ${lines.length} lines; only the first ${MEMORY_INDEX_LINES} are loaded. Keep the index short.)`;
	}
	return [
		"## Project memory",
		"",
		`This project's file memory is \`${dir}/\`, the one Claude Code keeps for it: what you save there, a Claude Code session on this project reads, and what it saved is below. It holds what the repository does not: the owner's plan and decisions, their preferences, and lessons. It is not Engram; Engram stays the searchable history.`,
		"",
		"Each memory is one file holding one fact, with frontmatter:",
		"",
		"```markdown",
		"---",
		"name: <short-kebab-case-slug>",
		"description: <one-line summary, used to decide relevance>",
		"metadata:",
		"  type: user | feedback | project | reference",
		"---",
		"",
		"<the fact; for feedback and project, follow it with **Why:** and **How to apply:** lines>",
		"```",
		"",
		"After writing the file, add one line to MEMORY.md: `- [Title](file.md) — hook`. Before saving, read the file that already covers it and update it rather than add a duplicate; when an item of a plan in memory closes, update that file in the same turn. A memory names files and states as they were: check one before acting on it.",
		"",
		loaded ? `### MEMORY.md\n\n${loaded}` : "MEMORY.md does not exist yet.",
	].join("\n");
}
