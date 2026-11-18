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
| `tasks` | object | `total`, `completed`, `pending`, `allComplete`, `unchecked[]`, `slices[]` (each `name`, `total`, `completed`, `delivers`, `tasks[]`, and `outline`: true for a slice with no checkbox task yet), `slice` (the first slice with an unchecked task; null when none is open), `slicesDone` (slices with every task checked; an outline is not done), `current` (the first slice not done, an outline included; null when every slice is done), `next` (the slice after `current`, or null) |
| `next` | string | the suggested next phase; `merge` ends a change with no folder, which has nothing to archive; a change with a folder that alters behaviour on the standard route is at `verify` until a passing light verify of the tree its last run tested is on record; a change that alters no behaviour counts as applied only once a file outside its record differs from the base or is new, so one with nothing applied yet is at `apply`, never `archive` |
| `notes[]` | string | notes, never refusals |
| `strict_tdd` | object | `value` (bool), `source`, `evidence`, `reason`, `notes[]` |
| `tests` | object, null | the last recorded run of the change's tests; `red_before_green` (bool): it passed after an earlier run of the change failed, its RED before its GREEN; `light_verify` (object, null, last): the newest light verify note (see the execution record) |
| `route` | object, null | `level` (standard or verified), `tier`, `lines`, `reasons[]`, `lenses[]`, `slice`; null when the route cannot be read |
| `mode` | string | the owner's permission mode for the project (`jhon mode`): `normal`, `yolo`, or `super-yolo`; always `normal` in a folder that is no git repository, which is no project |
| `needs` | object, null | only for a selected change, after `mode`: what its record needs, `tasks`, `spec`, `proposal`, `design` (bool), and `why` with each one's reasons (`[]` when not needed); null without a route |
| `folder` | bool | only for a selected change, the last key but `worktree_per_change`: false for a change with no folder, the one its branch names |
| `worktree_per_change` | bool | only when the layout's config sets `worktree_per_change: true`, the last key but `cleanup` and `workspace`: always `true` when present |
| `cleanup[]` | `{change, worktree, branch, merged, commands[]}` | only when an archived change left its linked worktree or its branch behind, the last key but `workspace` (T80b): `worktree` (string, null) its path, `branch` (string, null), `merged` (bool, null without a branch) whether the branch is merged into the default branch, and `commands`, what removes them: `git worktree remove <path>`, and `git branch -d <branch>` only when merged. Each also stands in `notes[]` as "archived change <change> still has its worktree <path> and its branch <branch>, merged into <default>: remove them with `…` (jhon removes nothing)"; an unmerged branch is said to be not merged and gets no command. Jhon runs none of them |
| `workspace` | object | only for a repository in a multi-repository workspace (T57), the last key: `role` (`primary`, or `member` of the primary its pointer names), `primary` (the primary's main worktree), `members[]` `{name, path, joined}`: every member the primary's LDD config lists (`workspace:`), `path` absolute, `joined` whether its pointer names this primary. Absent outside one. A listed member with no pointer, a path that is no git repository's top, a name that is not `[a-z0-9-]+`, and a pointer whose primary does not list this repository are notes, never refusals; see docs/ldd.md |

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
`$(git rev-parse --git-common-dir)/jhon/changes/<change>/`, where its
`delivery.json` goes too, so the tree is never dirtied. Besides the last
run's executions, the record keeps `history`: each earlier complete run as
`{timestamp, passed}`, the newest 20 (absent in a record written before it
existed, which reads as no RED known). The `--json` summary of a run ends
with `red_before_green`, as `tests` does here.

`jhon executions run <change> --affected` with no base is a unit's run
(T133): it selects what changed since the change's last complete,
undisturbed passing run, else since the change's base, and says which. Its
record holds `unit: true` and `since` (that run's `working_tree`, or the
base ref), and every record keeps `last_pass`, `{timestamp, working_tree}`,
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

A uv-run pytest project is narrowed by pytest-testmon, whose record lives
in `~/.cache/jhon/testmon/`, one file per checkout and project. With no
record (or one holding no fingerprints) an affected run runs the project's
whole test command once with `--testmon-noselect`, into `<record>.building`,
renamed to the record only when pytest ran to its end (exit 0 or 1). A
first run its deadline cut keeps none and leaves `<record>.cut`: later runs
run only the test modules the change touched and name `jhon executions
prime --timeout <minutes>`, which builds every missing record and writes no
execution record.

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
and route-guard opens a PR of a change that alters behaviour only once its
newest light verify passed on the tree the last run tested (its working
tree, else HEAD's).
