# LDD in a project: always on

Jhon always works under LDD (T72): whoever wants to work without it uses
a clean agent. LDD is set up in a project when it has an LDD config;
until then, read-only work sets nothing up, and the first code change
sets it up.

## Layouts

A project keeps its LDD record in one layout:

- **OpenSpec**, `openspec/`: `openspec/config.yaml`,
  `openspec/changes/<change>/`, `openspec/changes/archive/<date>-<change>/`,
  `openspec/specs/`. It comes first when a project has both.
- **Native**, `ldd/`, the same shape: `ldd/config.yaml`,
  `ldd/changes/<change>/`, `ldd/changes/archive/<date>-<change>/`,
  `ldd/specs/`. It is detected by `ldd/config.yaml` or `ldd/changes/`, never
  by a folder named `ldd` alone. `/jhon-ldd init` creates it only when the
  project has no layout; with `openspec/` it uses that.

Every command and guard reads either the same way, and names the
project's own paths in its messages (`ldd/changes/<change>/` in a native
project).

## Where "set up" is read

In a git repository, the layout is the repository's: `openspec/` may be
found in a folder above it (as it always was), a native `ldd/` only inside
it. In a folder that is not a git repository, jhon reads the layout at the
current directory (or, for `openspec/`, above it), as before: a folder with
`ldd/config.yaml` or `openspec/config.yaml` has LDD set up there, and pi's
reminder follows the engine's answer.
`jhon status` shows it: `ldd  on  (default; active|inactive)`, active once
the config is there, and `"ldd": {"state", "source", "active"}` in
`--json`, `state` always `on` and `source` always `default` (the shape the
retired switch had).

## Set up at the first change

- A session in a project with no layout, in Claude Code and pi alike, gets
  one line in place of the project's rules: "LDD is not set up in this
  project yet". Read-only work sets nothing up. At its first code change
  the session sets LDD up by following
  `prose/skills/ldd/references/init.md`, named by its absolute path under
  the install the hook runs from (`JHON_ROOT`, else the root its binary is
  invoked from: `~/.local/share/jhon/current` as installed, the checkout in
  development), so no agent has to expand `~`; where that install has no
  such file it is named under `~/.local/share/jhon/current`. The
  steps keep `openspec/` when it exists and create `ldd/` otherwise.
- Before writing the config the session asks the owner one batched
  question, through its ask tool only, never in prose, each option naming the recommended one: how to deliver (each
  slice to the default branch, recommended, or the whole change in one
  merge after its last slice, `delivery: change`) and how to test
  (`strict_tdd_policy: require`, recommended, `forbid`, or unset, and the
  test command of each project `jhon init` cannot tell). Where no one can
  answer (no ask tool, a headless run, no answer) it uses the recommended
  values. Either way it says in one line what it set up and goes on with
  the task under LDD in the same turn, on the change's own branch.
- **Delivery and code wait for the set-up.** In a git repository with no
  layout, route-guard refuses `git commit` and `gh pr create`, and the
  first code edit, in every mode and for every caller (sub-agents too: the
  main session sets LDD up first), naming the init steps:

  ```text
  route-guard: refused: LDD is not set up in this project yet; set it up first by following `<install>/prose/skills/ldd/references/init.md` (ask how to deliver and test with your ask tool, or use the recommended values), then make this edit and, before you finish, start its change (tasks.md) and record a run (`jhon executions run`)
  ```

  A code edit is any file the call writes inside the repository outside
  `.git/` and the layouts' directories (`openspec/`, `ldd/`), with no
  exemption (a README or docs count too): an Edit, Write or NotebookEdit
  in Claude Code, pi's edit and write, and a Bash command the safety guard
  reads as writing a file (`>`, `tee`, `sed -i`, `cp`, ...). What sets LDD
  up is not refused (`jhon init`, `mkdir -p ldd/specs ldd/changes/archive`,
  writing `ldd/config.yaml` by a tool or Bash), nor reads, searches, or a
  command that writes no file in the repository. Once the layout's config
  exists, edits pass and delivery follows LDD's rules; an OpenSpec project
  is unchanged. A folder in no git repository is not refused.

## Test runs: the inner loop

Every intermediate pass is a unit's run, `jhon executions run <change>
--affected` with no base: it runs what changed since the change's last
passing run. The slice base, `--affected <base-ref>`, is only for the RED
record and the slice's closing run, which a slice commit and delivery
require (a unit's run closes no slice). In a wide selection a project with
a usable pytest-testmon record runs the test modules the change touched
narrowed by it, and a test of another project selects it only by naming a
changed file's whole path or import path (`jhon executions --help`).
With no change named, `jhon executions run` runs the change `jhon status`
selects on the branch, a change with no folder included, and its first line
names it; `jhon status` prints the exact next command (`command`), the
change's name and base filled in.

## A small change

A change with no folder whose record needs no tasks.md (one component,
under the advisory lines of code, not high risk) is small, and `jhon status`
says so: it writes no tasks.md, saves no memory or session summary, and
keeps no todo list. Its RED and GREEN runs are its record. When the run that
recorded GREEN started on uncommitted work and the commit holds exactly the
tree it tested, `jhon deliver check` takes that run as the delivery
evidence: no re-run on the clean tree.

## A worktree per change

By default a change works on its own branch in the main checkout.
`worktree_per_change: true` in the layout's config (default false; the
set-up never sets it unless the owner asks) gives each change its own git
worktree outside the repository (`<repo>-worktrees/<change>`) on its
branch. The session brief then names the lifecycle: create the worktree
once, then start each command with `cd <its absolute path> &&` (the shell
may reset the directory between calls) and repeat nothing else; run RED
once; a change building on another's undelivered work branches from that
change's branch, and the final message names the branch holding the whole
delivery; delivery puts the work
on the default branch (merged locally when there is no remote or PR flow);
after delivery and archive, `git worktree remove <that path>` and delete
the merged branch, never the worktrees' container or anything outside the
change's own worktree; handoff files go outside the repository. `jhon
status` shows `worktree    per change` (and `"worktree_per_change": true`
with `--json`) only when it is true; any value other than `true` or
`false` is off, with a note naming it.

## A workspace of several repositories

A change may span an app and its sibling repositories (T57). The primary
repository names them in its LDD config, each a name (`[a-z0-9-]+`) and a
path relative to its top (its main worktree):

```yaml
workspace:
  lib: ../lib
  web: ../web
```

The siblings' files never change: in each, the owner runs `jhon workspace
join <primary>`, which records a pointer under Jhon's state
(`${XDG_STATE_HOME:-~/.local/state}/jhon/workspace/<sha256 of the sibling's
main worktree>.json`); `jhon workspace leave` removes it and `jhon workspace
list [--json]` shows every pointer. Joining and leaving are the owner's acts:
the safety guard refuses them from an agent, as it refuses `jhon grant` and
`jhon mode`, and keeps the pointers' folder from every agent, as it keeps
the grants': that folder itself, through any link into it; a project whose
own path has a `jhon/workspace` folder is edited as any other. A sibling is a member only while its pointer names a primary
whose config lists a path that is the sibling's top; a path is followed only
to a git repository's top, never up from it.

`jhon status` shows `workspace   primary  (lib joined, web not joined)` in
the primary, `workspace   member lib  (primary <path>)` in a member, and
`workspace` with `--json` (docs/status-json.md); nothing outside a workspace.
Its notes name a listed sibling that has not joined, a path that is no git
repository's top, an ignored name or absolute path, and, in a sibling, a
pointer whose primary does not list it.

In a session whose project is the primary or a joined member, the primary
and every joined member are the project for the guards that judge "outside
the project": the vcs guard's and the safety guard's writes (and their
deletions inside a root; removing a member's root itself stays refused), and
the OS sandbox's writable roots, read from the disk with no git process.
Editing a sibling the owner joined needs no `outside-write` grant. A listed
sibling that has not joined, a joined one the primary does not list, and
any other repository stay outside, as before.

A joined member needs no LDD layout of its own: route-guard lets its edits
and commits through, and judges its PR with the primary's record. The
member's branch names the change (its name, a slash a dash: `feat/c1` is
`feat-c1`), and a PR whose diff alters behaviour in the member, by the
member's own route, opens once the primary's change has its RED before GREEN
in `jhon executions`, a passing light verify on the standard route, and its
independent verification on the verified route. Linked delivery is not here
yet.

The change lives in the primary; its tests may run in every member (T57
slice 6). `jhon executions run <change> --repo <name>`, in the primary or in
any member, runs member `name`'s own projects (each testable one's resolved
test command, then its code checks) in the member, whole and one after
another, and records the run in the primary's record of the change under
`repos.<name>`: `{working_tree, after_working_tree, executions, passed,
complete}`, the member's working tree read before the first command and
again after the last. Without `--repo` a run is the current repository's,
as before; the primary's own run keeps every member's entry. `--affected`,
`--dry-run` and `--jobs` are not taken with `--repo`; a repository in no
workspace, a name the primary does not list, a member that has not joined
and a change the primary does not have are refused (exit 2), with nothing
written. A record with no member's run has no `repos` key.

A slice of tasks.md names the repository it is in with a `Repo: <name>`
line under its heading, written as a `Delivers:` line is; `jhon status`
shows it in the slices line (`next Slice 2: lib (0/1 checked, repo lib)`)
and as the slice's `repo` with `--json`.

`jhon deliver check <change>` in a member, for a change of the primary's,
reads the member's entry with the same tested-tree rules as a change's own
run: it passed and finished, its working tree was the same at its end, the
member's tree is clean, and HEAD's tree is the tree it tested (before the
commit, it says to commit with no re-run). The member's risk is its own
diff's; a high one needs the change's independent verification in the
primary. The scenarios and `delivery: change` stay the primary's check's. A
ready check writes the member's `delivery.json` under its git common dir
(`jhon/changes/<change>/`), as a change with no folder keeps its record, so
neither tree changes.

## The change's state in Engram

The change's record is its state. The engine keeps one copy of it in
Engram, `ldd state: <change>` (type `ldd-state`, topic key
`ldd-state/<change>`, which `engram save --topic` updates in place), written
after a commit that closes a slice lands, after `jhon executions verify`
records a light verify, and after `jhon archive`: the phase, slices done of
the total, the last test run, the delivery, the decisions the handoff's
search finds, and `Refs:` lines, `<kind> <name> @ <path>` from CodeGraph for
the files and symbols the diff touches (no line numbers, at most 20, none
without a `.codegraph/` index). The write never fails or slows the
transition: 3 s at most, one note line on stderr when it fails, nothing when
Engram does not resolve (`JHON_ENGRAM`, else `engram` on PATH) or
`integrations.engram` is off. `jhon context --handoff` searches decisions
by the change, its areas, and the symbol and file names of its diff, so a
decision saved with its code's refs is found from that code.

A sub-agent's first message carries, after its brief, what its launch
found: `## Decisions on record`, the decisions of the handoff packet (`jhon
context [<change>] --phase none --handoff`; id, type and title, at most 12
lines, 2 s), and `## Code map`, CodeGraph's `query` and `callers` for up to
8 files and backticked symbols the brief names (at most 4096 bytes, 3 s;
only with a `.codegraph/` index, or, in a change's linked worktree, the main
checkout's, kept to the files the worktree has). pi's shell appends them to
a launch; in Claude Code `jhon hook pre-tool` does, for an `Agent` call of
the main session that the guards allow: it answers with `updatedInput`, the
input as it came with the prompt extended, and `allow` in the default,
`acceptEdits` and `bypassPermissions` modes, no decision in plan or auto
mode. A deny or an ask is answered as before, with nothing added, and so is
a sub-agent's own `Agent` call. Each block is left out on any failure,
timeout or empty result; with neither, the launch is the brief alone. The
blocks open with their own `## ` heading, so a writer's `## Allowed edit
surfaces` ends where they begin.
Verify and the 4R lenses also get `## Tests to judge`: each test the
change's execution record marks as turned green while only its own test
file changed in its project (`jhon context --judge`), for the validator to
judge whether it was weakened; absent when there is none.

## The retired `jhon ldd on|off`

`jhon ldd` is no command any more. A record an old `jhon ldd off` left in
jhon's state (`$XDG_STATE_HOME/jhon/ldd-switch/`) turns nothing off: the
project's LDD rules, its hooks and route-guard bind as in any LDD project.
Its sessions are told once, "LDD is always on with Jhon now; the old
switch-off no longer applies"; the engine records that it told them
beside the record. The safety guard still refuses `jhon ldd on|off` from
any agent, and any write into jhon's state.

- In a Python repository with no marker (no pyproject.toml), `jhon init`
  reports its root as a Python project with no test command when it finds
  no other project; `/jhon-ldd init` runs the tests' command once and,
  when it runs, writes it as `test_command_override` for `"."`.
