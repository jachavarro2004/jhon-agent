/**
 * Read-only git the shell spawns takes no optional lock (T35, L320): a
 * `git status` that refreshes the index takes .git/index.lock, and one killed
 * by a timeout leaves it behind for every later git command.
 *
 *   jhon test
 */

import "./isolated-env.ts";
import { test, after } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { folderlessRecordDir, lddInWorktrees, lddLanes, lddWatchTargets } from "../lib/ui/ldd-status.ts";
import { projectRoot } from "../lib/memory.ts";

const scratch = realpathSync(mkdtempSync(join(tmpdir(), "jhon-git-locks-")));
after(() => rmSync(scratch, { recursive: true, force: true }));

const realGit = execFileSync("sh", ["-c", "command -v git"], { encoding: "utf8" }).trim();
const repo = join(scratch, "repo");
mkdirSync(repo);
execFileSync(realGit, ["init", "-q", repo], { env: { PATH: process.env.PATH ?? "", HOME: scratch } });

/** Runs fn with a `git` first on PATH that logs each run's GIT_OPTIONAL_LOCKS and arguments; returns the log lines. */
function logged(fn: () => unknown): Promise<string[]> {
	const bin = mkdtempSync(join(scratch, "bin-"));
	const log = join(bin, "git.log");
	writeFileSync(join(bin, "git"), `#!/bin/sh\nprintf '%s\\t%s\\n' "\${GIT_OPTIONAL_LOCKS-unset}" "$*" >> '${log}'\nexec '${realGit}' "$@"\n`);
	chmodSync(join(bin, "git"), 0o755);
	const path = process.env.PATH;
	process.env.PATH = `${bin}:${path}`;
	return Promise.resolve().then(fn).finally(() => { process.env.PATH = path; })
		.then(() => existsSync(log) ? readFileSync(log, "utf8").trim().split("\n") : []);
}

test("the LDD status and the memory root read git with GIT_OPTIONAL_LOCKS=0", async () => {
	const lines = await logged(async () => {
		folderlessRecordDir(repo, "c");
		await lddInWorktrees(repo, [], async () => undefined);
		await lddLanes(repo, undefined, async () => undefined);
		lddWatchTargets(repo, undefined);
		projectRoot(repo);
	});
	assert.equal(lines.length, 5, lines.join("\n"));
	for (const l of lines) assert.match(l, /^0\t/, l);
});
