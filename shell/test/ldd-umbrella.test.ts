/**
 * Umbrella changes and ranked lanes on the rail (T37, L365-L368): the
 * session's change follows jhon status, which never selects an umbrella by
 * default; the other worktrees' changes are ranked by pending work, then
 * their latest movement; umbrellas come last, marked, never with ◆.
 *
 *   node --experimental-strip-types --test test/ldd-umbrella.test.ts
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { changeCount, estadoLines, lddLanes, readLddStatus, umbrellasAt, type LddStatus } from "../lib/ui/ldd-status.ts";
import { changeCardLines } from "../lib/ui/jhon-panel-change.ts";

const plain = { fg: (_r: string, t: string) => t, bold: (t: string) => t, value: (t: string) => t, label: (t: string) => t };

const repoWith = async (prefix: string, names: string[]) => {
  const { execFileSync } = await import("node:child_process");
  const { mkdtempSync, realpathSync } = await import("node:fs");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const base = realpathSync(mkdtempSync(join(tmpdir(), prefix)));
  const repo = join(base, "app");
  execFileSync("git", ["init", "-q", repo]);
  execFileSync("git", ["-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"]);
  const wts = names.map((name) => { const p = join(base, "wts", name); execFileSync("git", ["-C", repo, "worktree", "add", "-q", "-b", name, p]); return p; });
  return { base, repo, wts };
};

test("readLddStatus keeps the umbrellas each folder's status lists, with or without a change", async () => {
  const { mkdtempSync, writeFileSync, chmodSync, rmSync } = await import("node:fs");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const dir = mkdtempSync(join(tmpdir(), "ldd-umbrella-"));
  try {
    const bin = (name: string, body: string) => { const p = join(dir, name); writeFileSync(p, `#!/bin/sh\n${body}\n`); chmodSync(p, 0o755); return p; };
    assert.equal(await readLddStatus(dir, bin("only", `echo '{"change":null,"next":"start-change","umbrellas":["umbrella"]}'`)), undefined, "an umbrella alone is no session change");
    assert.deepEqual(umbrellasAt(dir), ["umbrella"]);
    assert.equal((await readLddStatus(dir, bin("some", `echo '{"change":"change-a","tasks":{"slices":[]},"umbrellas":[]}'`)))?.change, "change-a");
    assert.deepEqual(umbrellasAt(dir), [], "a read that lists none clears them");
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

test("lddLanes ranks the lanes by pending work, then the latest movement, then name; umbrellas last, marked", async () => {
  const { writeFileSync, utimesSync, rmSync } = await import("node:fs");
  const { join } = await import("node:path");
  const { base, repo, wts } = await repoWith("wt-rank-", ["own", "old", "fresh", "done", "same"]);
  try {
    const [own, old, fresh, done, same] = wts as [string, string, string, string, string];
    const tasks = (w: string, at: number) => { const f = join(w, "tasks.md"); writeFileSync(f, "- [ ] a\n"); utimesSync(f, at, at); return f; };
    const status = (change: string, completed: number, total: number, tasksFile: string): LddStatus =>
      ({ change, slices: [completed < total ? "current" : "done"], tasks: { completed, total }, tasksFile, next: completed < total ? "apply" : "archive" });
    const statuses: Record<string, LddStatus> = {
      [own]: status("change-own", 0, 1, tasks(own, 1_000)),
      [old]: status("change-old", 0, 2, tasks(old, 1_000_000)),
      [same]: status("change-same", 1, 2, tasks(same, 1_000_000)),
      [fresh]: status("change-fresh", 1, 3, tasks(fresh, 2_000_000)),
      [done]: status("change-done", 2, 2, tasks(done, 3_000_000)),
    };
    const umbrellas: Record<string, string[]> = { [repo]: ["umbrella"], [done]: ["umbrella"] };
    const lanes = await lddLanes(repo, { ...statuses[own]!, root: own }, async (cwd) => statuses[cwd], undefined, Date.now(), (cwd) => umbrellas[cwd] ?? []);
    assert.deepEqual(lanes.map((l) => l.change), ["change-fresh", "change-old", "change-same", "change-done", "umbrella"]);
    assert.deepEqual(lanes.at(-1), { change: "umbrella", worktree: "app", root: repo, done: 0, slices: 0, next: "start-change", umbrella: true }, "an umbrella once, where it was found first");
  } finally { rmSync(base, { recursive: true, force: true }); }
});

test("an umbrella lane is drawn last, marked umbrella, never with ◆, and is not counted as a change", () => {
  const shown: LddStatus = { change: "change-a", slices: ["current"], next: "apply", worktree: "change-a", lanes: [
    { change: "change-b", worktree: "change-b", done: 0, slices: 1, next: "apply" },
    { change: "umbrella", worktree: "app", done: 0, slices: 0, next: "start-change", umbrella: true },
  ] };
  const lines = estadoLines(shown, plain, 46);
  const at = lines.findIndex((l) => l.includes("umbrella") && l.includes("⎇ app"));
  assert.ok(at > 0 && !lines[at]!.includes("◆") && lines[at]!.startsWith("○ umbrella"), lines.join("\n"));
  assert.equal(lines[at + 1], "  umbrella · start-change: split it", lines.join("\n"));
  assert.equal(lines.filter((l) => l.startsWith("◆")).length, 2, "◆ only on changes in progress");
  assert.equal(changeCount(shown), 2, "the session's change and one lane; the umbrella is no change in progress");
});

test("the change card says an umbrella alone is no change in progress, and marks umbrella lanes", () => {
  const none = changeCardLines({ change: null, activeChanges: ["umbrella"], umbrellas: ["umbrella"], next: "start-change" }, plain, 70).join("\n");
  assert.match(none, /No change in progress; umbrella: umbrella \(start-change: split it into changes\)/);
  const lanes = changeCardLines({ change: null, activeChanges: [], umbrellas: [], lanes: [{ change: "umbrella", worktree: "app", done: 0, slices: 0, next: "start-change", umbrella: true }] }, plain, 70).join("\n");
  assert.match(lanes, /umbrella {2}app · umbrella · start-change: split it/);
});
