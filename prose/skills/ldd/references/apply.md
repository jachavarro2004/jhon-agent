## Apply

Implement the tasks named, or the current slice's unchecked tasks (the
first `## Slice` with one), in `openspec/changes/{change-name}/`; never
the next slice's. A change with no tasks.md is the work the delegation
names. The specs are the acceptance criteria and the design the
approach. A gap found while building (a missing scenario, a contract or a
migration not foreseen) goes back to that slice's spec delta and tasks: add
the scenario or the task, report it as a deviation, and go on. Only a
product decision about an earlier slice is returned to the orchestrator
for the owner; a technical rework is not.

- `jhon status {change-name} --json` reports the tasks, `strict_tdd`, and
  each project's test command (`projects`); use that command as written.
- Write each behavior's test first and run it through
  `jhon executions run {change-name}` while it fails (RED) for the
  expected reason, before the behaviour change; run it again once it
  passes (GREEN). Report a witness for each new test (`contracts/tdd.md`).
- Match the project's patterns and conventions, and apply any `rules.apply`
  in `openspec/config.yaml`.
- When the change has a tasks.md, check each task off (`- [x]`) there as
  it is done. A task already
  checked is done: skip it. That file is the progress record.
- Run each unit's tests (`jhon executions run {change-name} --affected`)
  and, at the slice's close, `--affected <base-ref>`; report each with its
  observed result.
- A remediation fixes only the verify report's blocking findings.
- A runtime isolation refusal of a write is a stop: report it verbatim and
  never create or select another worktree or root to make the write there.

Save to Engram only a non-obvious decision, and return the tasks done,
the files changed, the test results, and any deviation.
