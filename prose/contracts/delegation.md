# Delegation

- A count or a price ratio never sizes a task; the counts below are notes.
- From the main checkout, pass the change's worktree to sub-agents and
  inspect (their `cwd` where the tool takes one, else its absolute path in the
  task and `git -C <worktree>`); never launch agents by running Jhon's
  internals by hand (`shell/lib`, node imports).
- **Hand off by reference.** Name the artifact and its ids instead of
  paraphrasing them; the executor reads the source. State the files it may edit under a
  `## Allowed edit surfaces` heading, one path or narrow glob per line
  (jhon hook scope-guard refuses the worker's and the fix agent's writes
  outside it), the exact test commands, and any failure already present on
  the base. Before launching a worker on a slice, map with CodeGraph (then
  reads) every integration point it touches (models, migrations, routers
  or handlers, permissions and their categories or registries, permission
  seeds, inventories or manifests, docs, existing tests) and list them all.
  A writer blocked on a file outside the list returns `blocked`
  naming it: widen the list in a resumed delegation, or make the edit
  yourself. A worker stopped on scope is a planning error: fix the plan,
  never ask the owner. A writer follows `contracts/efficiency.md`.
- **Size each worker slice to fit its context budget; the worker
  checkpoints its progress and partial state at its context notice and
  returns what is done and what remains: hand the remainder to a fresh worker.**
  A slice that crosses layers runs as passes, one layer each, in
  dependency order: pure domain; persistence (migration, models, migration
  test); application (commands, handlers, repository port and adapter);
  API and record (route, schemas, permission rows, contract test, docs).
  One worker per pass, its brief naming its exact files from tasks.md, its
  Allowed edit surfaces and its one commit, each under the child
  budget (wrap-up 80K/40%, cap 150K/75%); independent passes run in
  parallel, each in its own worktree.
- **Validate consequential premises before building.** At most one
  independent read-only challenge, for a high-consequence premise nothing
  has proved; a deterministic failure gets a fix, not a debate.

One explore per change: when `openspec/changes/<change>/exploration.md`
exists, hand it to the next phase instead of exploring again; launch
another explore only for a question it does not answer, and it extends
that file.

A delegation prompt carries the owner's request verbatim, beside your
reading of it. For a child session, or before a fresh session, include
`jhon context <change> --phase none --handoff`: the change's risk tier and
the ids of the decisions already recorded for its areas, which the receiver
reads before deciding anything they cover. An explore or worker task
carries the standing decisions found, each id with one line, and asks it
to search for more on what it covers. A requirement you inferred is stated as an assumption or
asked, never presented as the owner's. A blocker is measured against the
base: a test that also fails on the base is reported as pre-existing, not as
this change's blocker.
On return, merge each branch on the change's
branch, test once, and remove each worktree (`git worktree remove <path>`),
then its branch (`git branch -d <branch>`), one command each, never
`&&`.
