# LDD — Leapfrog-Driven Development

Value reaches the default branch in integrable, proven slices, with no
ceremonies, and the process leaps over itself whenever the evidence shows
something better.

## Principles

1. **Continuous value.** A slice is a piece of value that can be integrated
   while the rest is still being built, and names the acceptance criteria it
   delivers. Delivered means on the default branch, ready to integrate. The
   business picks the unit: each slice to the default branch, or the whole
   change with its slices as commits (`delivery: change`).
2. **Proven, not ceremonies.** Every risk a meeting or an approval would
   cover is covered by a check: TDD, the delivered tree being the tested
   tree, and an independent verification when the risk asks for one. The
   owner decides once; only real decisions go back to the owner.
3. **Nothing is a contract.** Every rule, guard, or flow step is a
   hypothesis naming its real case; without one it is an advisory. A
   guard carries a card (the risk it covers, the case that put it
   there), `jhon audit --all` measures whether it pays, and it is removed or
   replaced when it does not, or when something better appears (see
   "Rounds"). A rejected idea returns only with new evidence.
4. **A living record, in proportion.** Each change that alters behaviour
   leaves the record it needs and no more ("The record"): always its RED
   and GREEN runs and the commit or PR text that says what was asked;
   tasks.md, a spec delta, a proposal, or a design only when it needs one.
   The main specs say what the system does today.
5. **What can be checked lives in logic.** A rule a program can verify is
   enforced by a guard or a check; prose holds only judgment. Every guard
   is held to principle 3.

State lives in the change directory, or, for a change with no folder, under
the git directory (`<git common dir>/jhon/changes/<change>/`);
`jhon status <change> --json` reads it and reports facts, what the change
needs (`needs`), and one advisory `next`. It keeps no state and never
blocks a step. LDD works best in a git repository with a commit; below
that, status adds a note, and git is initialised only with the owner's
consent.

## Rounds

The method matures in rounds, one after each user story or every few
sessions of real work, never on a calendar:

1. `jhon audit --all --since <the last round>` sets out the numbers LDD
   cares about: per change, its slices, the owner's messages per slice, its
   active time, the time from its start to the default branch, its
   commits, and how many suite-wide test runs were started by hand, in any
   thread, instead of through `jhon executions` (a focused TDD run on named
   files or a filter does not count); per
   guard, its refusals, the time the agents lost after them, and its card
   (`guard.Cards`).
   `jhon audit <session>` states the same per change of one session: its
   slices (the `## Slice` headings of its `tasks.md`) and the owner's
   messages per slice; its planning rewrites after its first verify
   (`post_verify_rewrites`) are information, not a cost: planning as
   you go rewrites the plan.
2. A short proposal follows: what to remove, adjust, or add, each with its
   evidence (a number, a session, an owner's correction) and what it gains.
   A guard that refused nothing for rounds, or whose refusals cost more
   than they prevented, is a candidate to remove.
3. The owner decides. What is approved is built in logic where it can be
   checked; what is rejected is not proposed again without new evidence.

## Layout

```
openspec/
├── config.yaml                        project configuration (init writes it)
├── specs/<domain>/spec.md             main specs, the source of truth
└── changes/
    ├── <change>/
    │   ├── exploration.md             explore
    │   ├── research.md                research
    │   ├── proposal.md                propose
    │   ├── specs/<domain>/spec.md     spec (delta specs)
    │   ├── design.md                  design
    │   ├── tasks.md                   tasks, checked off by apply
    │   ├── tdd-evidence.md            optional witness entries
    │   ├── verify-report.md           verify
    │   ├── execution-record.json      jhon executions run
    │   └── delivery.json              jhon deliver check
    └── archive/<date>-<change>/       archive
```

A change is active while `openspec/changes/<change>/` exists outside
`archive/`. With no name given, `jhon status` infers the change in this
order: an umbrella the branch names; the only active one that is not
an umbrella; the one the branch names (`fix/tenant-caches` names
`fix-tenant-caches`); the only one with uncommitted files; the one the
branch's own commits touched last. Otherwise ask: none is selected, and
`rankedChanges` in `jhon status --json` lists the candidates by pending
work (unchecked tasks first), then the latest movement of their tasks.md or
execution record, then name.

An umbrella change is a starting point other changes come out of (an
inventory, an audit): its `openspec/changes/<change>/.openspec.yaml` says `kind: umbrella`, or it
holds an `exploration.md` and no other artifact (no research.md,
proposal.md, specs/, design.md or tasks.md) and no execution record. It is
selected only when named (`jhon status <umbrella>`) or when the branch
names it, never by default; `umbrellas` in `jhon status --json` lists them
apart, and its next step is `start-change`: split it into changes, or,
for a new change that only has its exploration yet, write its proposal or
tasks.md; never archive it. With only umbrellas active, nothing is selected
and status says "no change in progress; umbrella: <name> (start-change:
split it into changes, or write its proposal or tasks.md if it is one change)".

A small change has no folder: the branch names it (a slash names a dash);
its record is its RED and GREEN runs in `jhon executions` plus the commit
or PR text that says what was asked.

## The record

Where the session says LDD is active, every change starts the same way,
before its first behaviour-changing edit:

1. Run `jhon status` and `jhon risk` in its tree.
2. Write the record the change needs (below).
3. Write the test first and run it through `jhon executions run <change>
   --affected <base>` while it fails (RED); make the change; then once it
   passes (GREEN), unit runs between ("Test runs"). TDD always.

A change's record holds only what it needs:

- **Always:** its RED and GREEN runs in `jhon executions`, and the commit
  or PR text that says what was asked. `jhon hook route-guard` refuses the
  PR of a behaviour change without its RED before GREEN.
- **tasks.md** when the work cannot be resumed from the diff. Jhon asks
  for it (`needs` in `jhon status --json`) over about 400 lines of code
  (record files not counted), at risk high, or when more than one
  component is touched. Create it yourself, without being asked, when you
  see several rules or cases to cover or a design decision on the way.
- **A spec delta only when a main spec under `openspec/specs/` changes or
  is added.**
- **proposal.md or design.md** only when the route asks for one
  (`jhon status` suggests it) or the owner asks. No guard refuses either.
- **No folder** for a change that needs none of these: see above.

Archive only a change with a folder and its RED before GREEN on record;
`jhon status` never suggests archive without them.

`explore → research → propose → spec → design → tasks → apply → verify → archive`

These are the steps a change's record can take, not stages it must pass:
skip, merge, or reorder them when that serves the change. The `verify`
agent may run on any change.

| Step | Runs in |
|---|---|
| explore, research | the `explore` agent, or inline |
| init, propose, spec, design, tasks, archive | the orchestrator, inline, with the step's reference in `skills/ldd/references/` |
| apply | the `worker` agent, or inline for a small task |
| verify | the `verify` agent: an independent verifier, never the writer |

A delegated step carries the output of `jhon context <change> --phase
<phase>` in its prompt, in a fenced `jhon-context` block, so the audit
attributes the agent to its change and step.

| Artifact | `done` when |
|---|---|
| `exploration.md` | it names at least one backticked repository path |
| `research.md` | `## Sources` lists `- [S<n>]` items and every item of `## Claims` cites one |
| `proposal.md` | it has `## Why` and `## What Changes` and no placeholder (`TBD`, `TODO`, `<...>`) |
| `specs/` | every `### Requirement` has a `#### Scenario` |
| `design.md` | it has content beyond headings |
| `tasks.md` | it has at least one `- [ ]` or `- [x]` line |
| `verify-report.md` | it states `verdict: pass` |

Status also notes what LDD asks and a change still lacks: a proposal
without `## Acceptance Criteria`; a `tasks.md` without `## Intent` and no
proposal; tasks outside a `## Slice <n>:` section, in a later slice than
the current one (an outline is expected), or past 12 in a slice; a slice
heading without `Delivers:`;
the delta-spec scenarios no test names yet; and a verify-report test run
whose exit the execution record contradicts. Each is a note, never a
refusal.

Status suggests one of `propose`, `spec`, `design`, `tasks`, `apply`,
`verify`, `archive`, `select-change`, or `start-change`: the next step the
change needs after the furthest output that exists. It never suggests an
artifact the change does not need, nor `archive` without RED before GREEN
or without a folder. It suggests `verify` only when it is due: on the verified route, or when
`review_required` names it. Its notes (a VCS state below ready, an edit
target outside the repository, legacy configuration) inform; none stops
anything.

## Slices

A change is built in slices, never as one pass of every step over the
whole change:

1. **Intent.** Why the change exists and its acceptance criteria, what
   verify checks against: in the commit or PR text, under `## Intent` in
   tasks.md when the change has one, or in `proposal.md` when one is
   written. `jhon risk` reads the tier from the
   paths the change touches; it reads the change's own record, its record files
   (Markdown, JSON, YAML) in `openspec/changes/**` and in the specs it
   is archived into (`openspec/specs/**`), as passive, so the record
   never raises a change's tier or route (a `risk_rules` entry still
   overrides it); any other file there, code, a migration, or a `.env`,
   is read by the default rules.
   A tasks.md, if any, outlines every slice first,
   `## Slice <n>: <name>` and
   `Delivers: <acceptance criteria or observable value>`: integrable
   value that builds, tests, and delivers alone. Only the current slice
   holds tasks, written after the one before is delivered; evidence
   revises the outline. Explore or research run
   only when a decision needs facts the code and memory do not give.
2. **Per slice:** its spec delta when one is needed (the scenarios it adds
   or changes), its tasks under `## Slice <n>: <name>` when the change has
   a tasks.md, a failing test (RED), the code, the affected tests (GREEN), each run
   as "Test runs" says, and delivery (`jhon deliver check`, then a commit or a merge
   the owner authorizes). With `delivery: change` in
   `openspec/config.yaml`, a slice ends in a commit on the change's
   branch, and that commit is the tree its last test run tested (outside
   `openspec/`): `jhon hook route-guard` refuses a commit that closes a
   slice otherwise. The change is delivered once, after its last slice: one PR,
   one CI run, merged with `gh pr merge <n> --rebase --delete-branch` so
   each slice stays its own commit; `jhon deliver check --action merge` is
   not ready while a task is unchecked. `jhon status` suggests `tasks`
   for the next slice once one is checked off. A slice's size is
   advisory: `jhon status` reports the lines of code the slice in progress
   adds (outside tests and documents, since the last commit that closed a
   slice, or the merge base before the first), and one over
   `route_verified_lines` (about 400) gets a note with that count, in
   status and on the commit that closes it, offering a split.
   A test names the scenario it proves (title or `scenario: <name>`); a
   slice commit under `delivery: change` needs those it adds or edits.
3. **Back, not over.** A gap found while building (a missing scenario, a
   contract or a migration not foreseen) returns to that slice's spec or
   tasks; a product decision about an earlier slice goes to the owner, a
   technical rework does not. Replan from what exists when the evidence
   says so.
4. **The record is always; the rigor rises with the risk.** A change that
   alters behaviour (a file `jhon risk` reads above passive) carries the
   record it needs ("The record"), and `jhon hook route-guard` refuses its
   PR without its RED before GREEN. The route (`jhon status`,
   `ldd/route.go`) sets only the rigor, re-read on each slice's diff; it
   only rises: `verified` at the verified tier (`high`,
   `route_verified_tier`) or when `review_required` names `verify`,
   `standard` under it, whatever its size. What each tier verifies, asks and
   reviews is the one table, `contracts/delivery.md`'s; `review_required`
   (see "Delivery") always wins over it.
A change with a folder is archived once, after its last slice is merged; a
change with no folder has nothing to archive.

## Verification

`verify-report.md` is written first, with `verdict: fail` until every check
is done, and filled as the verifier checks, so a verifier stopped short
still leaves what it found and never a pass it did not reach. It ends with one line `verdict: pass` or `verdict: fail`, the
test runs it relied on (command, exit code, tree), and its findings in two
classes:

- **blocking**: a stated requirement or required check is violated, or a
  real defect exists. Any blocking finding makes the verdict `fail`.
- **follow-up**: everything else. Follow-ups never fail the verdict;
  `PASS WITH WARNINGS` is a pass.

Only commands that ran appear, counts are measured, and a recorded failure
is never overwritten into a pass. Remediation fixes the blocking findings
and nothing else, then re-runs what the fix touched and verifies once more.
A review, when wanted, runs before the final verification.

## Code checks

Each project's lint and format commands are found the way its test command
is, by `jhon init`, and are part of style no agent decides: a toolchain's
own checker (`go vet` and `gofmt`, `cargo clippy` and `cargo fmt --check`),
or a tool a file names (a package.json script `lint` or `format:check`,
a ruff configuration, run through the project's uv or poetry). Each only
reads: a format check fails on unformatted code. `lint_command_override`
and `format_command_override` in `openspec/config.yaml`, keyed by project
path, replace the discovered check of their kind, or add one.

A file an agent edits or writes is rewritten at once by its project's own
formatter (`jhon hook format-edit`, on Claude Code's `PostToolUse` and pi's
`tool_result`): gofmt, rustfmt at the crate's edition, ruff where it is
configured, or the project's installed prettier. It never fails the edit,
and when it changes the file it tells the agent to read it again.

## Test runs

`jhon executions run <change> [--affected [<base>]]` runs each project's
tests and code checks and records them in `execution-record.json`, which
`jhon deliver check` and `jhon audit` read; its scoping, parallelism,
reuse, and `executions_*` keys are in `jhon executions run --help`. A
unit's tests run with a bare `--affected`; the RED record and a slice's
close with a base. A
whole suite run by hand gets a note, never a refusal, and the audit
counts it (`ByHand`).

### Light verify

When a slice of a change that alters behaviour closes on the standard
route, the `verify` agent verifies it lightly: it reads the slice's diff
and its tests, runs the focused tests, and records its verdict with
`jhon executions verify <change> --verdict pass|fail [--note <text>]`.
`jhon status` notes when one is due; a PR with no passing one of the
tree the last run tested runs with a note: none caught a defect on record.
A small change
(`jhon status` says so) keeps no tasks.md, archive, light verify or todo
list. The verified route keeps its full `verify-report.md`.

## Delivery

Consent is the owner's: the safety guard asks before a push, a `gh` write,
a publish, or a deploy. The framework adds identity: **the delivered
revision is the tested revision.** `jhon deliver check <change>
[--action merge|deploy|release] [--artifact <digest|commit>]` reports `ready` when
the tree is clean, the last test run passed complete and undisturbed, and
HEAD's tree is the tree it tested; it is not ready while a scenario of the
change's delta specs is named by no test, and lists those scenarios (a
change without delta specs is unaffected). A deploy or a release names its
artifact immutably: an image or package by its content digest; an
infrastructure deploy (CDK, Terraform) by the full commit it deploys from,
which must be the tested HEAD. A ready check writes `delivery.json`. What reaches the
default branch is delivered (a merge into it, or a push to it), and so is a
deploy or a release; a push of a change branch is review. A PR merged
with `gh pr merge <n>` after its CI reported every check green is tested:
`gh pr checks <n>` (`--watch` waits for it) exiting 0, its output unfiltered
or filtered to the `ci-success` gate row passing (the gate sums every
check), with no push after it; a red or pending check is no ready CI.

A change `jhon risk` reads as `high` is delivered only after an independent
verification: `verify-report.md` stating `verdict: pass` (the `verify`
agent). `jhon deliver check` is not ready without one; `jhon audit`
reports a high-risk delivery made without one. The tier only rises: a ready
check records it in `delivery.json`, and a later slice whose own diff reads
lower is checked at the highest tier the change delivered.

On a change whose risk reaches the verified tier, 4R is due too: the route
guard opens its PR only once the change carries `review-4r.md` naming the
lenses it ran (`lenses: risk, reliability`, at least one of `risk`,
`resilience`, `reliability`, `readability`) and stating `verdict: pass`
(`contracts/review-4r.md`). `jhon status`
reports each review the change owes (`route.reviews`), due or done.

`review_required` in `openspec/config.yaml` names the reviews a change or an
area needs whatever its route: each entry has a `change` (its name) or an
`area` (a path glob, `**` across directories, matching a path the change's
diff touches or a directory holding it), and its `review`: `verify`, `4r`,
or a list of them.

```yaml
review_required:
  - change: billing-rewrite
    review: verify
  - area: "src/payments/**"
    review: [verify, 4r]
```

It always wins: `jhon status` reports the reviews that apply to the
selected change and which are missing, the route guard refuses the PR until
each is on record, and a
reviewer it names runs without asking the owner, who configured it. The 4R
correction is a `worker` with its `## Allowed edit surfaces`, and `verify`
confirms it, so `verify` runs whenever 4R is due (`contracts/review-4r.md`,
"Reduction").

An entry jhon cannot read is never ignored: it shows in `jhon status`
(`route.reviews`, with its line and why) and holds the PR until fixed; the
syntax jhon reads is in `jhon status --help`.

With `docs_with_code: true` in `openspec/config.yaml`, a project that keeps
its documentation in `docs/` updates it with the code it documents: a PR
whose change touches a project's code and not its `docs/` is refused, unless
the change's tasks.md states `No docs change: <why>`.

With `worktree_per_change: true` in the layout's config (default false),
each change works in its own git worktree outside the repository, and the
session brief names its lifecycle and cleanup; without it nothing changes.

Code is merged while the change is active. A change with a folder is
archived afterwards; how its commit is delivered is the owner's call.

## Every step

What every step shares, run inline or by the `explore`, `worker`, or
`verify` agent.

### Artifacts

Read and write artifacts in `openspec/changes/<change>/`; a missing one the
change needs blocks: report it. The record is the state; the engine keeps
one copy in Engram, with its decisions and their code. Read memory with
`mem_search`, then `mem_get_observation`, never a preview.

### Result

End with text, not a tool call: `status` (`success`, `partial`, or
`blocked`), what was done, the artifacts written, risks, and the next
suggested step. A `blocked` result carries the literal refusal or the full
choice for the owner. The detailed report lives in the artifact.

### Repository state

Any agent that holds Bash may perform repository-state operations locally:
branches, worktrees, commits, merges, rebases. `jhon hook safety-guard`
refuses irreversible operations to every agent and the top-level session
(a forced push, history rewritten, `clean -x`, which deletes ignored
credentials). A deletion that loses work (`reset --hard`, `clean -f`, a
checkout that overwrites the working tree, a dropped stash, a forced branch
delete whose work is nowhere else, a worktree removed with its changes)
runs once the owner approves it: class `destroy`. It asks for the outward-facing ones: a push, a
`gh` write, a publish, a deploy, remote access; where no prompt can reach
the owner, they need `jhon grant <class>`. No agent writes into `.git/` or
another VCS directory. A sub-agent's wait is bounded: an `until` or `while`
loop that sleeps is refused without a `timeout` around it, since a sub-agent
that ends or is stopped would leave it running; a `for` over a fixed range,
or a foreground command, waits instead.

A runtime isolation refusal of a write is a stop as well. Creating or
selecting another worktree or root and making the refused write there is a
bypass (`contracts/runtime-boundaries.md`); report the refusal and let the
owner settle the tree. `jhon audit` reads it with `isolation-bypass`.

The runtime's environment block (working directory, "Is a git repository")
is a snapshot from session start, never evidence of current state; the
repository and `jhon status` are.
