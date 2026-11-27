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
- **Delivery waits for the set-up.** In a git repository with no layout,
  route-guard refuses `git commit` and `gh pr create` in every mode and for
  every caller, naming the init steps; a code edit runs, with a note that
  names them:

  ```text
  note (not a refusal; the call ran): LDD is not set up in this project yet: set it up by following `<install>/prose/skills/ldd/references/init.md` (ask how to deliver and test with your ask tool, or use the recommended values), and before you finish, start this edit's change (tasks.md) and record a run (`jhon executions run`)
  ```

  A code edit is any file the call writes inside the repository outside
  `.git/` and the layouts' directories (`openspec/`, `ldd/`), with no
  exemption (a README or docs count too): an Edit, Write or NotebookEdit
  in Claude Code, pi's edit and write, and a Bash command the safety guard
  reads as writing a file (`>`, `tee`, `sed -i`, `cp`, ...). What sets LDD
  up gets no note (`jhon init`, `mkdir -p ldd/specs ldd/changes/archive`,
  writing `ldd/config.yaml` by a tool or Bash), nor do reads, searches, or a
  command that writes no file in the repository. Once the layout's config
  exists, edits pass with none and delivery follows LDD's rules; an
  OpenSpec project is unchanged. A folder in no git repository gets none.

## Test runs: the inner loop

Every intermediate pass is a unit's run, `jhon executions run <change>
--affected` with no base: it runs what changed since the change's last
passing run, else since the commit the slice in progress started from
(T253). The slice base, `--affected <base-ref>`, the commit the slice
started from and never the default branch, is only for the RED record and
the slice's closing run, which a slice commit and delivery require (a
unit's run closes no slice). A unit's run whose last pass is older than
the slice's start says so, with the slice's base as the narrower run. A
changed project that brings two or more projects into the selection only
as its dependents is named in a note, with the slice's base when the run
measured from further back, never a wider one. In a wide selection a project with
a usable pytest-testmon record runs the test modules the change touched
narrowed by it, and a test of another project selects it only by naming a
changed file's whole path or import path (`jhon executions --help`).
With no change named, `jhon executions run` runs the change `jhon status`
selects on the branch, a change with no folder included, and its first line
names it; `jhon status` prints the exact next command (`command`), the
change's name and base filled in.

`jhon archive <change> <date>` moves a change with a folder into the
archive only once its RED before GREEN is on record, the fact
`jhon status <change> --json` reports as `tests.red_before_green`, a
workspace member's (`jhon executions run <change> --repo <name>`, kept under
`repos.<name>`) included: without it, it moves nothing, exits 1, and says
how to record it (the failing test run through `jhon executions run`, then
the passing one). The one exception is a change that alters no behaviour,
whose route records no tests by design: every file outside the layout's
folder that differs from the default branch is a test or a document (what
`jhon risk` reads as passive), and at least one does. It archives with
`no RED before GREEN needed: <change> alters no behaviour (…)` as its first
line. An empty diff earns no exemption: after the change is merged, or
before anything is applied, it needs its RED before GREEN like any other.
`jhon status` agrees: it suggests `archive` only where `jhon archive` takes
the change; elsewhere it stays at `apply`, and its note and `command` name
the runs. `jhon archive --help` prints its own help.

## A small change

A change with no folder whose record needs no tasks.md (one component,
under the advisory lines of code, not high risk) is small, and `jhon status`
says so: it writes no tasks.md, saves no memory or session summary, and
keeps no todo list. Its RED and GREEN runs are its record. When the run that
recorded GREEN started on uncommitted work and the commit holds exactly the
tree it tested, `jhon deliver check` takes that run as the delivery
evidence: no re-run on the clean tree. For a change with a folder, the files
under it (tasks.md, the proposal, the design, the spec deltas) are its
record, not project input: marking a task after the GREEN run and committing
it needs no re-run either. An edit anywhere else, another change's folder
included, does. A ready `jhon deliver check` records itself in
`$(git rev-parse --git-common-dir)/jhon/changes/<change>/delivery.json`,
outside the work tree, where the merge gate and `jhon audit` read it: the
tree stays clean, so a merge needs no commit that only records the check. A
`delivery.json` an earlier jhon wrote in the change's folder is left as it
is and still read.

A data migration is high risk, so never small: `jhon risk` reads as
`migration or schema` a code file whose base name starts with `migrate` or
`migration` (`migrate.py`, `migrations.ts`, `migrate_users.go`), any file
under a `migrations`, `migration`, `migrate` or `db/migrate` folder at any
depth, and SQL (`NNNN_*.up.sql`, Flyway's `V1__*.sql`), and the change takes
the verified route. The exception is a migration that is clearly additive:
when its text holds only additive operations, it reads as
`migration (additive)` at medium, and the other rules still apply. Additive
means SQL `CREATE TABLE`, `CREATE INDEX`, or `ADD COLUMN` that is nullable or
has a `DEFAULT`; Rails `create_table`, `add_column`, `add_index`,
`add_reference`; Django `CreateModel`, `AddField` with `null=True` or
`default=`, `AddIndex`; Alembic `op.create_table`, `op.add_column` with
`nullable=True` or `server_default`, `op.create_index`. Comments, transaction
wrappers and framework boilerplate are allowed around them. Only the up part
of a file with a down part is read. Anything else stays high: a drop, rename,
type change, `SET NOT NULL`, a backfill, a data-rewriting script, an unknown
language, or a file that cannot be read. A document or a test that only names
a migration (`docs/migration-guide.md`, `MIGRATING.md`, `test_migrate.py`),
and vendored code under `vendor/` or `node_modules/`, keep their own reading.

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
in `jhon executions` and, on the verified route, its independent
verification (a missing light verify is a note). Linked delivery, the
primary's own PR for a change with members, is below.

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
(`jhon/changes/<change>/`), as every ready check writes its own, so
neither tree changes.

A change that spans members delivers one PR per repository, the primary's
last (T57 slice 7). Its members are the workspace members a slice of its
tasks.md names (`Repo: <name>`) or its record holds a run of
(`repos.<name>`); a name the primary does not list is none. `jhon deliver
check <change>` in the primary also judges each member's delivery, for a
merge, as the check run in that member does, against the member's HEAD and
the primary's record, and is not ready while a member is not ("workspace
member lib is not ready: run `jhon deliver check c` in <path>"): it prints
`member  lib  ready` or `member  lib  not ready` with that member's reasons,
and `members: [{name, ready, reasons}]` with `--json`. A member that has not
joined is not ready, and is named with the owner's `jhon workspace join
<primary>` instead of the check. `jhon status <change>` in the primary shows
them in one line, `members     lib ready, web not ready  (`jhon deliver check
c` in a member says why)`, and as `members` with `--json`.

Route-guard opens the primary's PR (`gh pr create`) for such a change, an
archived one included (its tasks.md and record read under the archive), only
when every member is ready and the PR's body names, for each member, a pull
request of the repository its `origin` remote names,
`https://<host>/<owner>/<repo>/pull/<n>` (read from the member's git config,
with no network): the command line, a heredoc body included, or the file
its `--body-file` names. A member with no hosted `origin` is refused with
the command that shows it. A member's own PR keeps the rule above; a change
with no member, a merge, and every repository outside a workspace are
judged as before. A member's run is recorded while the change is active: for
an archived change whose member is not ready, route-guard says to move it
back from the archive and run it. Whether the members' PRs merged is not
checked.

Route-guard gives the members' check 6 s, well under the 10 s the shell
waits for a guard (T268). When the check runs out of time, the owner is asked
at the top level, and a sub-agent's PR is refused. Either way the message
names `jhon deliver check <change>` in each member it could not judge, and
says not to retry the PR before then.

### A tool's own data is never project input

CodeGraph's index (`.codegraph/`, `.codegraph-wsl/`), jhon-studio's output
(`.jhon/studio/`) and Engram's data folder (`.engram/`), at any depth, are
left out of every tree Jhon reads, whatever the project's .gitignore says
(T268). That covers a run's candidate and working tree, deliver check,
status, route-guard, the risk of the diff and the route, an `--affected`
run's selection, and the audit's edit-before-red. Git is given
pathspec excludes, so it never reads their bytes. A tree an earlier jhon
recorded with them in it is still the tested tree once they are ignored.
`jhon doctor` warns about each such folder that git does not ignore, with
the `.gitignore` line.

`jhon status`, deliver check and the guards read the working tree into a
private object directory in the system's temporary folder, with the
repository's objects as an alternate. The tree id is the one a run records,
nothing lands in the repository's `.git`, and the folder is removed after.
They hash at most 10000 untracked files or 128 MB that git does not ignore
(measured: about 2 s on the owner's machine). Over that, the check is not
ready, and it names the untracked folders, the largest first, with the
`.gitignore` lines that leave them out. The risk and the route keep the
same budget: past it they judge the tracked changes only. The risk then
says so once, in the same words; the route lists the untracked files but
counts no line of them. Within the budget, a file over 1 MiB is never read
whole. An untracked binary file (a NUL in its first 8000 bytes, as git
tells one) has no content for the risk's rules, as a tracked one's diff has
no added lines. A reading's folder that a killed process left behind is
removed by the next reading once it is 10 minutes old. A write to `.git/info/exclude`
stays refused, and the refusal says to write the same lines to the
project's `.gitignore`, which is allowed.

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

Inside Jhon's sandbox Engram's folder (`ENGRAM_DATA_DIR`, else ~/.engram)
stays read-only: a writable one would let a sandboxed command steer what
Engram does outside the sandbox. So a state record written there (a
`jhon executions verify` or `jhon archive` from sandboxed bash), or one
Engram refuses on a read-only file system, is queued instead, one file per
record in `<git common dir>/jhon/engram-queue/`, and the note says `jhon:
the state of <change> was queued for Engram; Jhon saves it outside the
sandbox`. The next jhon command run outside the sandbox (the agent's next
tool call runs a jhon hook, in both runtimes, which flushes the queue of
its payload's folder) saves each queued record to Engram from the
repository's main worktree and removes it. The queue is a sandboxed
command's to write, so only a well-formed state record of Jhon's own is
saved: a file named `<time>-<change>.json`, at most 64 KiB, holding exactly
`title` (`ldd state: <change>`), `topic` (`ldd-state/<change>`), `content`
(the state, opening with the change's name) and `project` (the
repository's main worktree, whatever worktree queued it; Engram never runs
in a folder a record names). Anything else (another title, topic or
project, a link, a folder, a file too large) is moved to
`engram-queue/refused/`, which keeps the newest 100, reported on stderr
with its name quoted, and never saved; a write left half-done for a minute
is removed. A flush takes the entries named as a record first, oldest
first, and the others after, and refuses at most 32 entries, leaving the
rest for the next flush, so nothing sorted before a record keeps it from
being saved. No link is followed anywhere on the way: the git common dir
is opened with `openat2` and `RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS`, so
the kernel refuses a link in any component of its path (a linked
worktree's common dir is a path its `.git` file names, which a sandboxed
command may write, under folders it may swap for a link); `jhon/`, the
queue and `refused/` are then each opened relative to the one before with
`O_NOFOLLOW`, and every read, move and removal is relative to those open
folders, never to a path. Engram is run in the main worktree, opened the
same way and entered through that open folder. So a sandboxed command that
swaps any folder on the way for a link reaches nothing outside it. On a
kernel without `openat2` (before Linux 5.6) the queue fails closed:
nothing is read, written or saved, the next jhon says so once on stderr,
and `jhon doctor` warns with the fix, a Linux kernel 5.6 or newer. Where a
seccomp filter refuses `openat2` (`EPERM`, as an older container profile
answers a syscall it does not know) the queue fails closed the same way,
said as `openat2 is refused here (operation not permitted: a seccomp
filter, as a container's, blocks it)`, with the fix `allow openat2 in the
seccomp filter jhon runs under (a container's profile), or run jhon
outside it`: never as a link, and never with doctor's fix to remove the
queue, which would discard the records it holds. One flush takes at most
5 s, its listing, the ages of `refused/`'s entries and its removals
included, and leaves what it did not reach for the next one. When a flush
keeps records, the next jhon says so once on stderr, counting records
only: `jhon: the Engram queue kept <n> records for the next jhon command:
Engram did not take them ("<why>")` where Engram was asked, `Engram was
not asked ("<why>")` where the flush stopped before asking it (no time
left, the main worktree not opened). `jhon doctor` says why Engram did
not take them, or was not asked, how many records wait, how many entries
were refused, and a queue it cannot read, with the fix, every path quoted.
A queue that cannot be written leaves the note `jhon: the state of
<change> was not saved to Engram (…, and the queue "<path>" could not be
written: "<error>"; fix: run jhon doctor)`, its path and error quoted.
A search inside the sandbox fails the same way: `decisions_note` names the
folder the sandbox keeps read-only and says to search with the MCP memory
tool (`mem_search`). Accepted residuals: a sandboxed command can queue a
well-formed record with false content for any change name, saved under
`ldd-state/<change>` (never under another topic or title); and the Engram
project a record lands in is the one Engram derives from the main
worktree's files (its git remote, `.engram/config.json`), which a sandboxed
command can write, as it can for the agents' own saves.

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
`jhon-checker` and the Prism facets also get `## Tests to judge`: each test the
change's execution record marks as turned green while only its own test
file changed in its project (`jhon context --judge`), for the arbiter to
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
