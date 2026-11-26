# `jhon status --json`: the contract

Other programs read this output, so the fields below keep their names and
types. A change to one of them is a breaking change: update its readers in the
same commit. `test/ldd/status_contract_test.go` holds the fields to this list.

| Reader | File |
|---|---|
| jhon-shell rail LDD section and change detail (alt+j) | `shell/lib/ui/ldd-status.ts`, `shell/lib/ui/jhon-panel-change.ts` |
| the dashboard's change state | `engine/internal/dashboard/ldd.go` |

The change detail also reads `jhon risk --json` (`tier`, `reasons[{level, file, why}]`) for its risk row.

`jhon status` exits 1 when no single change is selected and still prints the
JSON (`ok: false`, `change: null`, `activeChanges`, `rankedChanges`); readers
use the JSON, not the exit code. A repository whose only active changes are
umbrellas (T37) has nothing to choose between: it exits 0 with `ok: true`,
`change: null`, `next: start-change` and the umbrellas listed.
With no change and no umbrella on the default branch, `next` is still
`start-change`, and the first note names the commands a small change starts
with: `git switch -c <name>`, then `jhon executions run` (T146g).

| Field | Type | Meaning |
|---|---|---|
| `ok` | bool | a single change was selected, or nothing is in progress because every active change is an umbrella (`change` null) |
| `change` | string, null | the change's name |
| `directory` | string, null | its directory, relative to the repository; null for a change with no folder |
| `activeChanges` | array of string | only when no change is selected: the candidates |
| `rankedChanges` | array of string | only when no change is selected: the candidates that are not umbrellas, by pending work — pending tasks first, then the latest movement of `tasks.md` or the execution record, then name |
| `umbrellas` | array of string | always (after `route` for a selected change): the active umbrella changes, never selected by default (an `exploration.md` and nothing else, or `kind: umbrella` in `openspec/changes/<change>/.openspec.yaml`); an umbrella's `next` is `start-change`: split it into changes |
| `artifacts[]` | `{artifact, state, path}` | every artifact and its state; the human view lists only those written or owed; `[]` for a change with no folder |
| `tasks` | object | `total`, `completed`, `pending`, `allComplete`, `unchecked[]`, `slices[]` (each `name`, `total`, `completed`, `delivers`, `repo` (only for a slice with a `Repo: <name>` line, the workspace member it is in; see docs/ldd.md), `tasks[]`, and `outline`: true for a slice with no checkbox task yet), `slice` (the first slice with an unchecked task; null when none is open), `slicesDone` (slices with every task checked; an outline is not done), `current` (the first slice not done, an outline included; null when every slice is done), `next` (the slice after `current`, or null) |
| `next` | string | the suggested next phase; `merge` ends a change with no folder, which has nothing to archive; a change with a folder that alters behaviour on the standard route is at `verify` until a passing light verify of the tree its last run tested is on record; a change that alters no behaviour counts as applied only once a file outside its record differs from the base or is new, so one with nothing applied yet is at `apply`, never `archive`; on every route `next` is `archive` only where `jhon archive` takes the change: with its RED before GREEN on record (`tests.red_before_green`, a workspace member's included), or for a change that alters no behaviour while a file outside the layout's folder differs from the default branch; elsewhere it stays at `apply`, with a note naming the runs that record it |
| `notes[]` | string | notes, never refusals |
| `strict_tdd` | object | `value` (bool), `source`, `evidence`, `reason`, `notes[]` |
| `tests` | object, null | the last recorded run of the change's tests; `red_before_green` (bool): it passed after an earlier run of the change failed, its RED before its GREEN, or a workspace member's run did (`repos.<name>`, T223), whose run stands in, `scope` `repos.<name>`, when the primary has none of its own; `light_verify` (object, null, last): the newest light verify note (see the execution record) |
| `route` | object, null | `level` (standard or verified), `tier`, `lines`, `reasons[]`, `facets[]`, `slice`; null when the route cannot be read |
| `mode` | string | the owner's permission mode for the project (`jhon mode`): `normal`, `yolo`, or `super-yolo`; always `normal` in a folder that is no git repository, which is no project |
| `needs` | object, null | only for a selected change, after `mode`: what its record needs, `tasks`, `spec`, `proposal`, `design` (bool), and `why` with each one's reasons (`[]` when not needed); null without a route |
| `folder` | bool | only for a selected change, the last key but `worktree_per_change`: false for a change with no folder, the one its branch names |
| `worktree_per_change` | bool | only when the layout's config sets `worktree_per_change: true`, the last key but `cleanup` and `workspace`: always `true` when present |
| `cleanup[]` | `{change, worktree, branch, merged, commands[]}` | only when an archived change left its linked worktree or its branch behind, the last key but `workspace` (T80b): `worktree` (string, null) its path, `branch` (string, null), `merged` (bool, null without a branch) whether the branch is merged into the default branch, and `commands`, what removes them: `git worktree remove <path>`, and `git branch -d <branch>` only when merged. Each also stands in `notes[]` as "archived change <change> still has its worktree <path> and its branch <branch>, merged into <default>: remove them with `…` (jhon removes nothing)"; an unmerged branch is said to be not merged and gets no command. Jhon runs none of them |
| `workspace` | object | only for a repository in a multi-repository workspace (T57), the last key but `members`: `role` (`primary`, or `member` of the primary its pointer names), `primary` (the primary's main worktree), `members[]` `{name, path, joined}`: every member the primary's LDD config lists (`workspace:`), `path` absolute, `joined` whether its pointer names this primary. Absent outside one. A listed member with no pointer, a path that is no git repository's top, a name that is not `[a-z0-9-]+`, and a pointer whose primary does not list this repository are notes, never refusals; see docs/ldd.md |
| `members[]` | `{name, ready, reasons[]}` | only for a selected change of a workspace's primary that has members (T57 slice 7), the last key: each workspace member a slice of its tasks.md names (`Repo: <name>`) or its record holds a run of (`repos.<name>`), in the config's order, and whether its delivery is ready as `jhon deliver check <change>` in that member judges it, with its reasons; `jhon deliver check --json` in the primary carries the same `members`. Absent for a change with none; see docs/ldd.md |

A change with no folder (T31) is selected on a branch that is not the
default one, in a project with `openspec/` at its repository's top level,
when the branch changes a file outside `openspec/` and no
`openspec/changes/<name>/` exists: `<name>` is the branch's name, a slash
naming a dash (`feat/small-fix` is `feat-small-fix`). Its record is its RED
and GREEN runs and the PR or commit text saying what was asked.

An archived change's leftovers (`cleanup`) are read from the layout's
archive directory (`<date>-<change>`), the repository's local branches,
and its linked worktrees, only in a project that is its repository's top
level. A branch is the change's when it is the change's name, a slash
naming a dash, or ends with it after a slash or a dash
(`feat/shipping-cost`), and names no longer active change; never the
default branch. A linked worktree is the change's when its branch is, or
when its directory is named for the change and its branch names no change.

## The execution record

`jhon executions run <change>` writes `execution-record.json` in the
change's folder, or, for a change with no folder, under
`$(git rev-parse --git-common-dir)/jhon/changes/<change>/`, so the tree is
never dirtied. A ready `jhon deliver check` writes every change's
`delivery.json` there, folder or not, so a check leaves the tree clean and
no commit only records it; one an earlier jhon wrote in the change's
folder is left as it is and still read. Besides the last
run's executions, the record keeps `history`: each earlier complete run as
`{timestamp, passed}`, the newest 20 (absent in a record written before it
existed, which reads as no RED known). The `--json` summary of a run ends
with `red_before_green`, as `tests` does here.

`jhon executions run <change> --affected` with no base is a unit's run
(T133): it selects what changed since the change's last complete,
undisturbed passing run, else since the commit the slice in progress
started from (the last commit on the branch that closed a slice of the
change in its tasks.md; the change's base before the first, T253), and says
which. Its record holds `unit: true` and `since` (that run's
`working_tree`, the slice's commit, or the base ref), and every record keeps `last_pass`, `{timestamp, working_tree}`,
the last passing run before it. All three are absent from a record written
before them, which reads as a wide run. A slice's close and `jhon deliver
check` refuse a record whose run was a unit's: they take `--affected
<base>` or a full run. A record that holds no execution (an affected run
that selected no project, as a change of documents only does) is refused
too, and the refusal names the full run, `jhon executions run <change>`,
which clears it. A unit's run that selects nothing leaves the record
as the last run wrote it; `tests.unit` appears here only for a unit's run.
The record also keeps `after_working_tree`, the working tree taken again
when the run ends: when it differs from `working_tree` the run is dirty,
and `jhon deliver check` never takes it as the tested tree (T146d). A
record with no `after_working_tree` (one an earlier jhon wrote) is not
taken either: the refusal says to run the tests again (T146e). Nor does a
later run reuse its passes, a unit's run measure from it (or from the
`last_pass` it kept), or a slice's close take its tree as tested: each needs
`after_working_tree` equal to `working_tree` (T146f).

In a multi-repository workspace the primary's record of a change also
keeps `repos`, each member's last run by name (`jhon executions run
<change> --repo <name>`, T57): `{working_tree, after_working_tree,
executions[], passed, complete}`. The primary's own run keeps it; `tests`
here is the primary's own run only. A record whose first run is a
member's holds `executions: null` and reads as no run. `jhon deliver check`
in the member reads its entry (docs/ldd.md). Absent with no member's run.

A uv-run pytest project is narrowed by pytest-testmon, whose record lives
in `~/.cache/jhon/testmon/`, one file per repository and project (T255):
keyed by the repository's git common dir and the project's path, so every
worktree of the repository, a new one per change, narrows from the same
record. When it is missing and the worktree has its own record from
before (keyed by its top level), a run copies that one in once; a dry run
reads it and writes nothing (T256). A run never
writes the record in place: it runs pytest-testmon on a private copy beside
it (`<record>.<pid>-<n>.run`), which replaces the record in one rename only when
pytest ran to its end (exit 0 or 1); a run cut, interrupted or failing
inside pytest leaves the record as it was, and of two runs at once the last
to finish wins (a record of other code only makes pytest-testmon select
more, never less). The record is 0644 whichever file it came from. A copy
or first-run file a killed jhon left beside the records is swept by the
next run when its process is gone or it is over a day old. With no record (or one holding no fingerprints) an
affected run runs the project's whole test command once with
`--testmon-noselect`, into `<record>.<pid>.building`, renamed to the record
only when pytest ran to its end; the dry run and the run name each such
project with how many test functions its test modules define, and in
`--json` its `projects[]` entry carries `first_run: {record, tests,
modules, deadline_minutes}` (`deadline_minutes` absent when `--timeout`
names the run's deadline). A first run its deadline cut keeps none and
leaves `<record>.cut`: later runs run only the test modules the change
touched and name `jhon executions prime --timeout <minutes>`, which builds
every missing record and writes no execution record.

A run whose failures are the test environment's keeps no record, the
shared one or a first one, and says the test services may be down: its
output says a test service refused the connection in its failures (a
traceback's `E` line, a `FAILED` line, the FAILURES section), or anywhere
when tests failed and none passed (a passing test may log a refusal it
retries), pytest could not collect a test module, or every one of at least 5 tests
failed and none passed (a unit's RED, a few failing tests, is kept). A
record that marks most of its tests failed (at least 5), which
pytest-testmon runs again whatever changed, is named in the dry run and the
run (`failed_marks: {failed, total}` in `--json`) with `jhon executions
forget <project>`, which drops the whole record, and every worktree's own
from before (`git worktree list`), which that worktree would copy in again: a test that failed because a service was down recorded only
the code it ran before failing, so trusting it as passing would select
less. A first run, and a pytest-testmon run whose selected tests are all
(or mostly) ones the record marks failed, have a deadline of 180 min
unless the run names one with `--timeout` (`timeout_minutes` on its
`plan[]` entry; `jhon executions prime` too): pytest-testmon writes its
record only at a run's end, so a run cut every time never narrows the
next. A run cut by its deadline says how far pytest got (`pytest got
through about 138 of its 577 selected tests (24%)`), and a cut
pytest-testmon run says the next run selects the same tests again.

With a usable record, a change that reaches half the project's test time or
more, or that changes a file pytest-testmon does not track, also runs only
the test modules it touched. When it touched none, a wide run (`--affected
<base>`, a slice's close) runs the project under `--testmon`: what the
record holds stale, which may be nothing, recorded as an execution. A
unit's run, and a project with no usable record, run none there.

A completed run's last line (stdout; stderr with `--json`) is its status,
`jhon executions: <pass|fail>, exit <code> (<n> of <m> passed; record
<path>)`, so `| tail` still shows it. When `JHON_EXECUTIONS_REPORT` names a
file, the run appends the absolute path of each record it writes; the
shell's background jobs set it for a command that runs `jhon executions
run` and report the job failed when its record holds a failure, whatever
the pipeline's exit code (T108).

`jhon executions verify <change> --verdict pass|fail [--note <text>]
[--json]` records a light verify (T31.5) at the end of the same record,
in `light_verify`: `[{"timestamp","verdict","commit","tree","note"}]`
(`note` only when given; HEAD's commit and tree when it was recorded; the
newest 20; a later run keeps them). `--json` prints the note written; a
change with no run on record exits 2. `tests.light_verify` is the newest
note, or null. On the standard route `jhon status` notes "light verify due
for <change>: …" while the GREEN run is newer than the last light verify,
and route-guard notes, never refuses, a PR of a change that alters behaviour
whose newest light verify did not pass on the tree the last run tested (its
working tree, else HEAD's).
