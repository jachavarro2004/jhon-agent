/**
 * The owner's mode contract by place (T59, L462, L464) on pi's path: each
 * cell of test/guards/mode_matrix_test.go's table, judged by the shell's
 * judge() through the tree's jhon (HOOK_BIN), by the main session with a UI,
 * the main session without one, and a child. Today's behaviour is the
 * contract; mode_matrix_test.go says it in full.
 *
 *   jhon test
 */

import "./isolated-env.ts";
import { test, after } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { formatEdited, judge, type CallContext, type JhonPiConfig } from "../lib/guards.ts";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const HOOK_BIN = process.env.JHON_TEST_BIN || join(REPO, "build", "jhon");
const SCRATCH = realpathSync(mkdtempSync(join(tmpdir(), "jhon-mode-matrix-")));
after(() => rmSync(SCRATCH, { recursive: true, force: true }));
// The guards this process spawns read their state here, never the machine's;
// route-guard reads the tree's prose (JHON_ROOT, as the Go suite's newRG).
Object.assign(process.env, { HOME: "/home/jhon-mode-matrix-test", XDG_STATE_HOME: join(SCRATCH, "state"), JHON_ROOT: REPO });

type Place = "ldd" | "repo" | "folder";
type Mode = "normal" | "yolo" | "super-yolo";
type Caller = "interactive" | "non-interactive" | "child";
type Fixture = { place: Place; dir: string; fw: string };
type Row = { name: string; kind: "ask" | "refuse" | "allow" | "delivery" | "commit" | "code" | "code-bash" | "outside"; cls?: string; passes?: Mode[]; call: (f: Fixture) => [string, Record<string, unknown>] };

const yoloUp: Mode[] = ["yolo", "super-yolo"];
const superOnly: Mode[] = ["super-yolo"];
const bash = (command: string) => (): [string, Record<string, unknown>] => ["bash", { command }];
const write = (path: (f: Fixture) => string) => (f: Fixture): [string, Record<string, unknown>] => ["write", { path: path(f), content: "x" }];

/** mode_matrix_test.go's matrixTable, as pi's tools call it. */
const TABLE: Row[] = [
  { name: "destroy-ask", kind: "ask", cls: "destroy", passes: superOnly, call: bash("git reset --hard") },
  { name: "destroy-deny-force-push", kind: "refuse", call: bash("git push --force") },
  { name: "destroy-deny-clean-fdx", kind: "refuse", call: bash("git clean -fdx") },
  { name: "outside-write-tool", kind: "outside", call: write(() => "/etc/jhon.conf") },
  { name: "outside-write-bash", kind: "allow", call: bash("echo x > /etc/jhon.conf") },
  { name: "push", kind: "ask", cls: "push", passes: yoloUp, call: bash("git push origin main") },
  { name: "gh-write", kind: "ask", cls: "gh-write", passes: yoloUp, call: bash("gh pr merge 1 --squash") },
  { name: "remote", kind: "ask", cls: "remote", passes: yoloUp, call: bash("ssh host") },
  { name: "publish", kind: "ask", cls: "publish", passes: yoloUp, call: bash("npm publish") },
  { name: "deploy", kind: "ask", cls: "deploy", passes: yoloUp, call: bash("kubectl apply -f x.yaml") },
  { name: "credentials-read", kind: "allow", call: (f) => ["read", { path: join(f.dir, ".env") }] },
  { name: "credentials-bash", kind: "allow", call: bash("cat .env") },
  { name: "settings-write", kind: "ask", cls: "settings", call: write((f) => join(f.dir, ".claude", "settings.json")) },
  { name: "git-config", kind: "ask", cls: "git-config", call: bash("git config core.hooksPath /tmp/h") },
  { name: "framework-write", kind: "refuse", call: write((f) => join(f.fw, "orchestrator.md")) },
  { name: "dot-git-write", kind: "refuse", call: write((f) => join(f.dir, ".git", "config")) },
  { name: "agent-jhon-grant", kind: "refuse", call: bash("jhon grant push") },
  { name: "agent-jhon-mode", kind: "refuse", call: bash("jhon mode yolo") },
  { name: "unreadable", kind: "ask", cls: "unreadable", passes: superOnly, call: bash("echo (pwd)") },
  { name: "unknown", kind: "ask", cls: "unknown", passes: superOnly, call: bash("rm -f /tmp/$X") },
  { name: "delivery-pr-without-red-green", kind: "delivery", call: bash("gh pr create --title x --body y") },
  { name: "delivery-commit", kind: "commit", call: bash("git commit -q -m x") },
  { name: "code-write", kind: "code", call: write((f) => join(f.dir, "src", "app.py")) },
  { name: "code-edit", kind: "code", call: (f) => ["edit", { path: join(f.dir, "src", "app.py"), edits: [{ oldText: "2", newText: "3" }] }] },
  { name: "code-bash-write", kind: "code-bash", call: bash("printf 'X = 3\\n' > src/app.py") },
];

/** route-guard's note on a code edit in a repository with no layout (T89, T153). */
const SET_UP_NOTE = "note (not a refusal; the call ran): LDD is not set up in this project yet: set it up by following `" + join(REPO, "prose/skills/ldd/references/init.md") +
  "` (ask how to deliver and test with your ask tool, or use the recommended values), and before you finish, start this edit's change (tasks.md) and record a run (`jhon executions run`)";
/** vcs-guard's note on a file tool's write outside the project and the scratch roots (T153). */
const OUTSIDE_NOTE = /^note \(not a refusal; the call ran\): (Write|Edit) of .* lands outside the project \(.*\) and the scratch roots; check that the task means this file$/;

function want(row: Row, place: Place, mode: Mode, caller: Caller): "pass" | "allow" | "set-up" | "outside" | "ask" | "refuse" {
  if (place === "folder") mode = "normal";
  // T72 (L557): a repository without LDD delivers nothing until LDD is set up;
  // T89 (L646): any file written in it, outside .git and the layout's directory,
  // runs with route-guard's set-up note (refused until T153, L957).
  if (row.kind === "refuse" || (row.kind === "delivery" && place !== "folder") || (row.kind === "commit" && place === "repo")) return "refuse";
  if ((row.kind === "code" || row.kind === "code-bash") && place === "repo") return "set-up";
  // T153: a file tool's write outside the project runs with vcs-guard's note (pi's
  // folder is under the scratch roots, so its code writes are allowed with none).
  if (row.kind === "outside") return "outside";
  if (row.kind === "allow" || row.kind === "delivery" || row.kind === "commit" || row.kind === "code" || row.kind === "code-bash") return "allow";
  if (row.passes?.includes(mode)) return "pass";
  return caller === "interactive" ? "ask" : "refuse";
}

function git(dir: string, ...args: string[]): void {
  const r = spawnSync("git", ["-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main", ...args], { cwd: dir, encoding: "utf8" });
  assert.equal(r.status, 0, `git ${args.join(" ")}: ${r.stderr}`);
}

function put(dir: string, files: Record<string, string>): void {
  for (const [rel, body] of Object.entries(files)) {
    mkdirSync(dirname(join(dir, rel)), { recursive: true });
    writeFileSync(join(dir, rel), body);
  }
}

/** The places of mode_matrix_test.go's newMatrixFixture. */
function fixture(place: Place, mode: Mode): Fixture {
  const base = join(SCRATCH, `${place}-${mode}`);
  const f: Fixture = { place, dir: join(base, "proj"), fw: join(base, "fw") };
  mkdirSync(f.fw, { recursive: true });
  put(f.dir, { "src/app.py": "X = 1\n", ...(place === "ldd" ? { "openspec/config.yaml": "" } : {}) });
  if (place !== "folder") {
    git(f.dir, "init", "-q");
    git(f.dir, "add", "-A");
    git(f.dir, "commit", "-q", "-m", "base");
    put(f.dir, { "src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n", ...(place === "ldd" ? { "openspec/changes/c1/tasks.md": "## Intent\nDelivers: X is 2\n" } : {}) });
  }
  const r = spawnSync(HOOK_BIN, ["mode", mode], { cwd: f.dir, encoding: "utf8" });
  if (place === "folder") {
    // A folder in no repository is no project: it takes no mode (T57a).
    assert.equal(r.status, 2, `jhon mode ${mode} in a folder: ${r.stdout}`);
    assert.match(r.stderr, /not a git repository/);
  } else {
    assert.equal(r.status, 0, `jhon mode ${mode}: ${r.stderr}`);
  }
  return f;
}

const ctxOf = (config: JhonPiConfig, f: Fixture, caller: Caller): CallContext =>
  ({ config, cwd: f.dir, child: caller === "child", interactive: caller === "interactive" });

for (const place of ["ldd", "repo", "folder"] as Place[]) {
  for (const mode of ["normal", "yolo", "super-yolo"] as Mode[]) {
    test(`pi judge: ${place}/${mode}`, async (t) => {
      const f = fixture(place, mode);
      const config: JhonPiConfig = { jhon: HOOK_BIN, orchestrator: "/nonexistent", vcsDirs: [".git", ".hg", ".svn"], protected: [f.fw], inspectAllow: [] };
      for (const row of TABLE) {
        for (const caller of ["interactive", "non-interactive", "child"] as Caller[]) {
          const cell = `${place}/${mode}/${row.name}/${caller}`;
          await t.test(`${row.name}/${caller}`, async () => {
            const [tool, input] = row.call(f);
            const got = await judge(tool, input, ctxOf(config, f, caller));
            const note = await formatEdited(config, tool, input, f.dir);
            switch (want(row, place, mode, caller)) {
              case "pass":
                assert.equal(got, null, `${cell}: ${JSON.stringify(got)}`);
                assert.ok(note?.includes(`allowed by the owner's ${mode} mode: ${row.cls}: `), `${cell}: note ${note}`);
                break;
              case "allow":
                assert.equal(got, null, `${cell}: ${JSON.stringify(got)}`);
                break;
              case "set-up":
                assert.equal(got, null, `${cell}: ${JSON.stringify(got)}`);
                assert.equal(note, SET_UP_NOTE, cell);
                break;
              case "outside":
                assert.equal(got, null, `${cell}: ${JSON.stringify(got)}`);
                assert.match(note ?? "", OUTSIDE_NOTE, cell);
                break;
              case "ask":
                assert.ok(got && "ask" in got && got.ask.startsWith(`${row.cls}: `), `${cell}: ${JSON.stringify(got)}`);
                break;
              case "refuse":
                assert.ok(got && "block" in got, `${cell}: ${JSON.stringify(got)}`);
                if (row.kind === "delivery" || row.kind === "commit") assert.match(got.reason, /route-guard: /, cell);
                break;
            }
          });
        }
      }
    });
  }
}
