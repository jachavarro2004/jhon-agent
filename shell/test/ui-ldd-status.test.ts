/**
 * The STATUS card's LDD change (lib/ui/ldd-status.ts), a sub-agent's live
 * step under its task, and the Todos progress bar.
 *
 *   jhon test
 */

import "./isolated-env.ts";
import { after, test } from "node:test";
import assert from "node:assert/strict";
import { rmSync } from "node:fs";
import { ago, estadoLines, lddFromStatus } from "../lib/ui/ldd-status.ts";
import { renderShellJobsSection, renderShellStatusSections, type ShellBarModel } from "../lib/ui/shell-bar.ts";

const plain = { fg: (_r: string, t: string) => t, bold: (t: string) => t, value: (t: string) => t, label: (t: string) => t };

// The scratch folders the tests make, removed when the file's tests end.
const temps: string[] = [];
after(() => { for (const d of temps) rmSync(d, { recursive: true, force: true }); });
const scratch = (d: string) => { temps.push(d); return d; };

test("STATUS says the change and where it is, its slices done of all and the open one, the next step, and the tests (T35, L110)", () => {
  const status = { route: { level: "verified" }, next: "apply", strict_tdd: { value: true },
    tasks: { total: 8, completed: 5, slice: "Slice 2: deadline extension", slices: [{ name: "Slice 1: launch", total: 4, completed: 4 },
      { name: "Slice 2: deadline extension", total: 3, completed: 1, delivers: "CA-13", tasks: [{ text: "2.1 a", done: true }, { text: "2.2 extend the deadline", done: false }, { text: "2.3 c", done: false }] },
      { name: "Slice 3: panel", total: 1, completed: 0 }] },
    tests: { timestamp: "2026-10-29T05:00:00Z", passed: false, failed: ["abc"], scope: "affected" } };
  const s = lddFromStatus("abc-process-exercises", status)!;
  assert.deepEqual([s.slices, s.route, s.next, s.detail?.[1]?.delivers], [["done", "current", "pending"], "verified", "apply", "CA-13"]);
  assert.deepEqual([s.tests, s.tasks, s.strictTdd], [{ passed: false, failed: 1, at: "2026-10-29T05:00:00Z", scope: "affected" }, { completed: 5, total: 8 }, true]);
  const now = Date.parse("2026-10-29T05:26:00Z");
  assert.deepEqual(estadoLines(s, plain, 60), [
    "◆ abc-process-exercises",
    "  1/3 slices · verified route · on 2: deadline extension",
    "  next apply → verify · tests ✗ 1 failed",
  ], "one block: the change; indented, its slices done of all, its route and the open slice; the next step and the tests");
  const hint = { ...plain, fg: (r: string, t: string) => `<${r}>${t}` };
  assert.ok(!estadoLines(s, hint).some((l) => /enter detail|t tests|d diff|o tasks\.md/.test(l)), "no action line: its keys work only inside the alt+j panel (L110)");
  assert.ok(!estadoLines(s, plain).some((l) => /last run/.test(l)), "the run is the tests, never «last run»");
  assert.equal(estadoLines(s, hint, 60)[2], "  next apply → verify · tests <error>✗ 1 failed", "a failed run in the failed state's glyph and role");
  const passed = lddFromStatus("x", { ...status, tests: { ...status.tests, passed: true, failed: [] } })!;
  assert.equal(estadoLines(passed, hint, 60)[2], "  next apply → verify · tests <success>✓ passed", "a passed run says passed, with no count where the record has none");
  const failedUncounted = lddFromStatus("x", { ...status, tests: { ...status.tests, failed: [] } })!;
  assert.equal(estadoLines(failedUncounted, plain, 60)[2], "  next apply → verify · tests ✗ failed", "a failed run with no count says failed");
  assert.ok(!estadoLines(s, plain).some((l) => /green|red\b|of 3|tasks 5\/8|strict TDD|◐ 2|· 3 panel|2\.2 extend/.test(l)), "no slice rows, no old words, no facts other surfaces own");
  const long = lddFromStatus("x", { ...status, tasks: { ...status.tasks, slices: status.tasks.slices.map((x: any, i: number) => i === 1 ? { ...x, delivers: "RN-05/CA-11 under concurrent approvals, and the verify follow-ups the owner chose" } : x) } })!;
  assert.deepEqual(estadoLines(long, plain, 46)[1], "  1/3 slices · verified route · on 2: deadlin…", "the slice line is cut to the width, one row");
  assert.ok(estadoLines(long, plain, 46).every((l) => l.length <= 46), "every line fits the width");
  const standard = lddFromStatus("x", { ...status, route: { level: "standard" }, tests: undefined })!;
  assert.deepEqual(estadoLines(standard, plain, 20), ["◆ x", "  1/3 slices · stan…", "  next apply · test…"], "verify follows apply only on the verified route; each line cut to the width");
  assert.equal(estadoLines(standard, plain, 46)[2], "  next apply · tests ⧗ none", "with no run, the tests say none");
  const done = lddFromStatus("x", { tasks: { slices: [{ name: "Slice 1: a", total: 1, completed: 1 }] } })!;
  assert.deepEqual(estadoLines(done, plain), ["◆ x", "  1/1 slices", "  tests ⧗ none"], "every slice done; no route, step, or run known, nothing said of them but the tests");
  // L79: a change with no slices says nothing of slices, rather than «no slices».
  assert.deepEqual(estadoLines({ change: "x", slices: [], next: "apply" }, plain), ["◆ x", "  next apply · tests ⧗ none"], "no slices, no slices part");
  assert.deepEqual(estadoLines({ change: "x", slices: [], route: "standard" }, plain), ["◆ x", "  standard route", "  tests ⧗ none"], "no slices, the route alone");
  const away = { ...lddFromStatus("architecture-compliance-audit", { ...status, route: { level: "standard" } })!, worktree: "geo-audit" };
  assert.equal(estadoLines(away, plain, 46)[0], "◆ architecture-compliance-audit    ⎇ geo-audit", "the worktree on the name line, at the right");
  assert.equal(estadoLines({ ...away, change: "x", worktree: "w" }, plain, 46)[0], "◆ x" + " ".repeat(40) + "⎇ w", "a short worktree ends at the right edge");
  assert.equal(estadoLines({ ...away, change: "a-change-name-far-longer-than-the-card-width" }, plain, 30)[0], "◆ a-change-name-f… ⎇ geo-audit", "a name that does not fit is cut first, the worktree kept");
  const unnamed = lddFromStatus("x", { tasks: { slice: "Slice 1: launch the API", slices: [{ name: "Slice 1: launch the API", total: 2, completed: 0 }] } })!;
  assert.equal(estadoLines(unnamed, plain)[1], "  0/1 slices · on 1: launch the API", "the open slice's name");
  assert.deepEqual(estadoLines(undefined, plain), ["No active LDD change"]);
  assert.equal(lddFromStatus("x", { tasks: {} }), undefined);
  assert.deepEqual([ago("2026-10-29T03:00:00Z", now), ago("2026-10-26T05:00:00Z", now)], ["2 h ago", "3 d ago"]);
});

test("the STATUS card names its route, and JOBS a running task in one row, its step left to alt+s (L141, L168)", () => {
  const model: ShellBarModel = { cwd: "~/acme", branch: "feat/x", dirty: 0, sessionName: undefined, modelId: "m", effort: undefined, contextPercent: null, contextWindow: 0, costTotal: 0, subscription: true, usage: undefined, statuses: [],
    ldd: { change: "x", slice: "Slice 2: launch", slices: ["done", "current"], route: "standard" },
    jobs: [{ id: "agent-1", label: "worker: slice 2", startedAt: 1, step: "bash · uv run pytest" }] };
  const text = renderShellStatusSections(model, plain, 60).join("\n");
  assert.match(text, /^▾ LDD +standard route\n {2}◆ x *\n {4}1\/2 slices · on 2: launch/, "the route is the section's count, said once");
  assert.doesNotMatch(text, /enter detail|o tasks\.md/, "the rail takes no keys: no action line (L110)");
  assert.doesNotMatch(text, /✓ 1 done/, "no per-slice rows");
  assert.doesNotMatch(text, /worker: slice 2/, "running agents have their own card");
  const agents = renderShellJobsSection(model.jobs, plain, 60, 1 + 125_000).join("\n");
  assert.match(agents, /▾ JOBS +1 running\n {2}⧗ agent-1 worker: slice 2 +2m 5s$/m);
  assert.doesNotMatch(agents, /↳|uv run pytest/, "the latest step is in the alt+s view, not the rail");
});

test("the change's tasks.md is found from the session's folder or a parent, for STATUS to watch", async () => {
  const { mkdtempSync, mkdirSync, writeFileSync } = await import("node:fs");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const { findUp } = await import("../lib/ui/ldd-status.ts");
  const root = scratch(mkdtempSync(join(tmpdir(), "ldd-")));
  mkdirSync(join(root, "openspec/changes/x"), { recursive: true });
  mkdirSync(join(root, "src/deep"), { recursive: true });
  writeFileSync(join(root, "openspec/changes/x/tasks.md"), "- [ ] a\n");
  assert.equal(findUp(join(root, "src/deep"), "openspec/changes/x/tasks.md"), join(root, "openspec/changes/x/tasks.md"));
  assert.equal(findUp(join(root, "src"), "openspec/changes/y/tasks.md"), undefined);
});

test("a fresh session finds the change of the repository's worktree whose tasks.md moved last, skipping its own folder and the ones it read", async () => {
  const { execFileSync } = await import("node:child_process");
  const { mkdtempSync, realpathSync, writeFileSync, utimesSync } = await import("node:fs");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const { lddInWorktrees } = await import("../lib/ui/ldd-status.ts");
  const base = scratch(realpathSync(mkdtempSync(join(tmpdir(), "wt-estado-"))));
  const repo = join(base, "app");
  execFileSync("git", ["init", "-q", repo]);
  execFileSync("git", ["-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"]);
  const wt = (name: string) => { const p = join(base, "wts", name); execFileSync("git", ["-C", repo, "worktree", "add", "-q", "-b", name, p]); return p; };
  const [old, fresh, done] = [wt("old"), wt("fresh"), wt("done")];
  const tasks = (w: string, at: number) => { const f = join(w, "tasks.md"); writeFileSync(f, "- [ ] a\n"); utimesSync(f, at, at); return f; };
  const files: Record<string, string> = { [old]: tasks(old, 1_000_000), [fresh]: tasks(fresh, 2_000_000) };
  const read = async (cwd: string) => files[cwd] ? { change: cwd.split("/").pop()!, slices: ["current" as const], tasksFile: files[cwd] } : undefined;
  const got = await lddInWorktrees(repo, [], read);
  assert.deepEqual([got?.change, got?.worktree], ["fresh", "fresh"]);
  assert.equal((await lddInWorktrees(repo, [fresh], read))?.change, "old", "a worktree already read is skipped");
  assert.equal(await lddInWorktrees(join(base, "nowhere"), [], read), undefined, "outside a repository, none");
  void done;
});

test("lddLanes reads the other worktrees' changes in progress, one row each, after the session's own", async () => {
  const { execFileSync } = await import("node:child_process");
  const { mkdtempSync, realpathSync } = await import("node:fs");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const { lddLanes } = await import("../lib/ui/ldd-status.ts");
  const base = scratch(realpathSync(mkdtempSync(join(tmpdir(), "wt-lanes-"))));
  const repo = join(base, "app");
  execFileSync("git", ["init", "-q", repo]);
  execFileSync("git", ["-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"]);
  const wt = (name: string) => { const p = join(base, "wts", name); execFileSync("git", ["-C", repo, "worktree", "add", "-q", "-b", name, p]); return p; };
  const [own, common, iam, idle] = [wt("own"), wt("tst-common"), wt("tst-iam"), wt("idle")];
  const lane = (change: string, slices: Array<"done" | "current" | "pending">, next?: string) => ({ change, slices, ...(next ? { next } : {}) });
  const statuses: Record<string, any> = { [own]: lane("test-suite-review", ["done", "done", "done"], "verify"),
    [common]: lane("test-suite-tidyup-common", ["done", "current", "pending"], "apply"), [iam]: lane("test-suite-tidyup-iam", ["current", "pending"], "apply") };
  const read = async (cwd: string) => statuses[cwd];
  const shown = { ...statuses[own], worktree: "own", root: own };
  const lanes = await lddLanes(repo, shown, read);
  assert.deepEqual(lanes.map((l) => [l.change, l.worktree]).sort(), [["test-suite-tidyup-common", "tst-common"], ["test-suite-tidyup-iam", "tst-iam"]], "the shown change and a worktree with none are left out");
  const lines = estadoLines({ ...shown, lanes }, plain, 46);
  const blocks: Array<[string, string]> = [["◆ test-suite-tidyup-common        ⎇ tst-common", "  1/3 slices · next apply"], ["◆ test-suite-tidyup-iam           ⎇ tst-iam", "  0/2 slices · next apply"]];
  assert.deepEqual(lines.slice(-4).filter((l) => l.startsWith("◆")).sort(), blocks.map(([name]) => name), `the lanes after the session's own:\n${lines.join("\n")}`);
  for (const [name, detail] of blocks) assert.equal(lines[lines.indexOf(name) + 1], detail, `each lane one block, its slices under its name:\n${lines.join("\n")}`);
  void idle;
});

test("STATUS draws no row per slice: the slices to come and the one in progress are the change detail's", () => {
  const s = lddFromStatus("x", { tasks: { slice: "Slice 1: a", slices: [{ name: "Slice 1: a", total: 26, completed: 18 }, { name: "Slice 2: build and push only the images that changed (CI saving A)", total: 1, completed: 0 }] } })!;
  const lines = estadoLines(s, plain, 34);
  assert.ok(!lines.some((l) => /^[·◐✓▰]|18\/26|build and push/.test(l)), lines.join("\n"));
  assert.equal(lines[1], "  0/2 slices · on 1: a");
});

test("a session that worked in a worktree shows that worktree's change, or none: never another worktree's in its place", async () => {
  const { execFileSync } = await import("node:child_process");
  const { mkdtempSync, realpathSync } = await import("node:fs");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const { lddOfSession } = await import("../lib/ui/ldd-status.ts");
  const base = scratch(realpathSync(mkdtempSync(join(tmpdir(), "wt-session-"))));
  const repo = join(base, "app");
  execFileSync("git", ["init", "-q", repo]);
  execFileSync("git", ["-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"]);
  const wt = (name: string) => { const p = join(base, "wts", name); execFileSync("git", ["-C", repo, "worktree", "add", "-q", "-b", name, p]); return p; };
  const [audit, caches] = [wt("arch-audit"), wt("tenant-caches")];
  const statuses: Record<string, any> = { [audit]: { change: "architecture-compliance-audit", slices: [] } };
  const read = async (cwd: string) => statuses[cwd];
  const exists = () => true;
  assert.equal(await lddOfSession(repo, [caches], read, exists), undefined, "the session's worktree selects none: none, not arch-audit's");
  statuses[caches] = { change: "fix-tenant-statement-caches", slices: ["current"] };
  const got = await lddOfSession(repo, [caches], read, exists);
  assert.deepEqual([got?.change, got?.worktree, got?.root], ["fix-tenant-statement-caches", "tenant-caches", caches], "its own, and where");
  statuses[repo] = { change: "on-main", slices: [] };
  assert.equal((await lddOfSession(repo, [caches], read, exists))?.change, "on-main", "pi's folder first");
  delete statuses[repo];
  delete statuses[audit];
  assert.equal((await lddOfSession(repo, [], read, exists))?.change, "fix-tenant-statement-caches", "a session that worked in no worktree yet: the repository's worktrees");
});

test("a status read that fails is no answer: STATUS never falls back to an older worktree in its place", async () => {
  const { lddOfSession } = await import("../lib/ui/ldd-status.ts");
  const statuses: Record<string, any> = { "/w/tenant-caches": { change: "fix-tenant-statement-caches", slices: ["done"] } };
  const read = async (cwd: string) => { if (cwd === "/w/geo-handlers") throw new Error("jhon status timed out"); return statuses[cwd]; };
  await assert.rejects(lddOfSession("/repo", ["/w/geo-handlers", "/w/tenant-caches"], read, () => true), /timed out/, "the latest worktree's failed read is no reason to show an older one");
});

test("readLddStatus keeps the owner's mode each folder's status reported, with or without a change, for the status line's badge (S31)", async (t) => {
  const { mkdtempSync, writeFileSync, chmodSync, rmSync } = await import("node:fs");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const { readLddStatus, permissionMode } = await import("../lib/ui/ldd-status.ts");
  const dir = mkdtempSync(join(tmpdir(), "ldd-mode-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const bin = (name: string, body: string) => { const p = join(dir, name); writeFileSync(p, `#!/bin/sh\n${body}\n`); chmodSync(p, 0o755); return p; };
  assert.equal(permissionMode(dir), undefined, "nothing read yet");
  assert.equal(await readLddStatus(dir, bin("none", `echo '{"change":null,"mode":"yolo"}'; exit 1`)), undefined);
  assert.equal(permissionMode(dir), "yolo", "no change selected, the mode still read");
  await readLddStatus(dir, bin("some", `echo '{"change":"x","tasks":{"slices":[]},"mode":"super-yolo"}'`));
  assert.equal(permissionMode(dir), "super-yolo");
  await readLddStatus(dir, bin("old", `echo '{"change":null}'; exit 1`));
  assert.equal(permissionMode(dir), undefined, "a status without mode says none");
  await assert.rejects(readLddStatus(dir, bin("broken", "echo half; exit 2")));
  assert.equal(permissionMode(dir), undefined);
});

test("readLddStatus: a change, no change, and a read that fails are three answers", async () => {
  const { mkdtempSync, writeFileSync, chmodSync } = await import("node:fs");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const { readLddStatus } = await import("../lib/ui/ldd-status.ts");
  const dir = scratch(mkdtempSync(join(tmpdir(), "ldd-read-")));
  const bin = (name: string, body: string) => { const p = join(dir, name); writeFileSync(p, `#!/bin/sh\n${body}\n`); chmodSync(p, 0o755); return p; };
  const some = await readLddStatus(dir, bin("some", `echo '{"change":"x","tasks":{"slices":[]}}'`));
  assert.equal(some?.change, "x");
  assert.equal(await readLddStatus(dir, bin("none", `echo '{"change":null}'; exit 1`)), undefined, "a project with no change selected");
  await assert.rejects(readLddStatus(dir, bin("broken", "echo half; exit 2")), "output that is no status");
});

const laneRepo = async (prefix: string) => {
  const { execFileSync } = await import("node:child_process");
  const { mkdtempSync, realpathSync } = await import("node:fs");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const base = scratch(realpathSync(mkdtempSync(join(tmpdir(), prefix))));
  const repo = join(base, "app");
  execFileSync("git", ["init", "-q", repo]);
  execFileSync("git", ["-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"]);
  const lane = join(base, "wts", "lane");
  execFileSync("git", ["-C", repo, "worktree", "add", "-q", "-b", "lane", lane]);
  return { repo, lane };
};

test("a lane no watch saw move is read again once the safety net's five minutes pass, never sooner", async () => {
  const { lddLanes, laneReads } = await import("../lib/ui/ldd-status.ts");
  const { repo, lane } = await laneRepo("wt-net-");
  const statuses: Record<string, any> = { [lane]: { change: "c", slices: ["current"] } };
  const read = async (cwd: string) => statuses[cwd];
  const since = laneReads();
  const t0 = 1_000_000;
  assert.deepEqual((await lddLanes(repo, undefined, read, since, t0)).map((l) => l.done), [0]);
  statuses[lane] = { change: "c", slices: ["done"] };
  assert.equal(since.moved.size, 0, "no watch fired");
  assert.deepEqual((await lddLanes(repo, undefined, read, since, t0 + 60_000)).map((l) => l.done), [0], "within the five minutes, the earlier read stands");
  assert.deepEqual((await lddLanes(repo, undefined, read, since, t0 + 5 * 60_000)).map((l) => l.done), [1], "after them, every lane is read again");
});

test("a lane's older read finishing after a newer one never overwrites it", async () => {
  const { lddLanes, laneReads } = await import("../lib/ui/ldd-status.ts");
  const { realpathSync } = await import("node:fs");
  const { repo, lane } = await laneRepo("wt-seq-");
  let release!: () => void;
  const gate = new Promise<void>((r) => { release = r; });
  let calls = 0;
  const read = async (cwd: string) => {
    if (cwd !== lane) return undefined;
    calls++;
    if (calls === 1) { await gate; return { change: "c", slices: ["current" as const] }; }
    return { change: "c", slices: ["done" as const] };
  };
  const since = laneReads();
  const older = lddLanes(repo, undefined, read, since, 0);
  await new Promise((r) => setTimeout(r, 50));
  since.moved.add(realpathSync(lane));
  assert.deepEqual((await lddLanes(repo, undefined, read, since, 0)).map((l) => l.done), [1], "the newer read");
  release();
  await older;
  assert.deepEqual((await lddLanes(repo, undefined, read, since, 0)).map((l) => l.done), [1], "the older read, finished last, left the newer one standing");
});

test("STATUS watches each worktree's reflog too: a commit with no branch switch moves its lane", async () => {
  const { lddWatchTargets } = await import("../lib/ui/ldd-status.ts");
  const { execFileSync } = await import("node:child_process");
  const { join } = await import("node:path");
  const { lane } = await laneRepo("wt-refs-");
  const gitDir = execFileSync("git", ["-C", lane, "rev-parse", "--absolute-git-dir"], { encoding: "utf8" }).trim();
  const targets = lddWatchTargets(lane, undefined);
  assert.ok(targets.some(([dir, only, roots]) => dir === join(gitDir, "logs") && only === "HEAD" && roots.includes(lane)), JSON.stringify(targets));
});

test("STATUS lists every change in progress, one block each: the session's own first, with its slices, route, step and tests, then the other worktrees' (T29, L93; T35, L110)", () => {
  const session = { ...lddFromStatus("refactor-layering-by-service", { route: { level: "verified" }, next: "verify",
    tasks: { slices: [1, 2, 3, 4, 5, 6, 7].map((i) => ({ name: `Slice ${i}: s${i}`, total: 1, completed: 1 })) },
    tests: { timestamp: "2026-10-29T05:00:00Z", passed: false, failed: [] } })!, worktree: "layering", root: "/w/layering" };
  const lanes = [{ change: "fix-infra-hardening", worktree: "infra-hardening", root: "/w/infra-hardening", done: 10, slices: 10, next: "archive" }];
  const tag = { ...plain, fg: (r: string, t: string) => r === "dim" ? `<dim>${t}` : t };
  assert.deepEqual(estadoLines({ ...session, lanes }, tag, 50), [
    "◆ refactor-layering-by-service   ⎇ layering",
    "  7/7 slices · verified route",
    "  next verify · tests ✗ failed",
    "<dim>◆ fix-infra-hardening            ⎇ infra-hardening",
    "<dim>  10/10 slices · next archive",
  ], "the owner's two-change example (L110): one block per change, the worktrees in one column at the right, each lane dim");
  assert.deepEqual(estadoLines(session, tag, 50), estadoLines({ ...session, lanes: [] }, tag, 50), "with one change, no lane lines");
  assert.deepEqual(estadoLines(session, tag, 50), ["◆ refactor-layering-by-service" + " ".repeat(10) + "⎇ layering", "  7/7 slices · verified route", "  next verify · tests ✗ failed"], "one change: one block of three lines");
  const many = Array.from({ length: 6 }, (_, i) => ({ change: `c${i}`, worktree: `w${i}`, done: i, slices: 6 }));
  const capped = estadoLines({ ...session, lanes: many }, plain, 50);
  assert.deepEqual(capped.slice(3), [...[0, 1, 2, 3].flatMap((i) => [`◆ c${i}` + " ".repeat(36) + `⎇ w${i}`, `  ${i}/6 slices`]), "+2 more"], "four lanes at most, and how many more");
  const narrow = estadoLines({ ...session, lanes: [{ ...lanes[0]!, change: "a-lane-whose-name-runs-far-past-the-card-width" }] }, plain, 30);
  assert.deepEqual(narrow, ["◆ refactor-… ⎇ layering", "  7/7 slices · verified route", "  next verify · tests ✗ failed", "◆ a-lane-wh… ⎇ infra-hardening", "  10/10 slices · next archive"], "at a narrow width the names are cut first, the worktrees kept in one column");
  assert.ok(narrow.every((l) => l.length <= 30), narrow.join("\n"));
});

test("the card never says the route word bare: verified and standard are routes, never states (T29, L93)", () => {
  const card = (level: string) => estadoLines(lddFromStatus("x", { route: { level }, next: "apply", tasks: { slices: [{ name: "Slice 1: a", total: 1, completed: 0 }] } })!, plain, 46);
  assert.match(card("verified")[1]!, /· verified route ·/);
  assert.match(card("standard")[1]!, /· standard route ·/);
  assert.ok(!card("verified").some((l) => /\bverified\b(?! route)/.test(l)), card("verified").join("\n"));
});

test("a change with no folder (directory null) is found, shown with its next step, and STATUS watches its executions record under git's common dir instead of tasks.md (T31.3)", async () => {
  const { mkdtempSync, mkdirSync, realpathSync, writeFileSync, chmodSync } = await import("node:fs");
  const { execFileSync } = await import("node:child_process");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const { readLddStatus, lddWatchTargets } = await import("../lib/ui/ldd-status.ts");
  const repo = scratch(realpathSync(mkdtempSync(join(tmpdir(), "ldd-nofolder-"))));
  execFileSync("git", ["-C", repo, "init", "-q"]);
  const bin = join(repo, "fake-jhon");
  writeFileSync(bin, `#!/bin/sh\necho '{"ok":true,"change":"feat-small-fix","directory":null,"tasks":{"total":0,"completed":0,"slices":[],"slice":null},"next":"merge","route":{"level":"standard"},"tests":{"timestamp":"2026-10-29T05:00:00Z","passed":true,"failed":[],"red_before_green":true},"folder":false}'\n`);
  chmodSync(bin, 0o755);
  const s = await readLddStatus(repo, bin);
  assert.ok(s, "found");
  assert.equal(s!.change, "feat-small-fix");
  assert.equal(s!.tasksFile, undefined, "no tasks.md");
  const common = join(repo, ".git");
  assert.equal(s!.recordDir, join(common, "jhon", "changes", "feat-small-fix"), "where `jhon executions run` writes its record");
  assert.deepEqual(estadoLines(s, plain, 60), ["◆ feat-small-fix", "  standard route", "  next merge · tests ✓ passed"], "shown, merge read as the engine names it");
  // Before its first run the record's folder does not exist: git's dir is watched for jhon/ to appear.
  let targets = lddWatchTargets(repo, s);
  assert.ok(targets.some(([dir, only]) => dir === common && only === "jhon"), JSON.stringify(targets));
  assert.ok(targets.some(([dir, only]) => dir === common && only === "HEAD"), "HEAD still watched");
  mkdirSync(s!.recordDir!, { recursive: true });
  targets = lddWatchTargets(repo, s);
  assert.ok(targets.some(([dir, only, roots]) => dir === s!.recordDir && only === undefined && roots.includes(repo)), JSON.stringify(targets));
});
