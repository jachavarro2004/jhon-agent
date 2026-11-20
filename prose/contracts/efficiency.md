# Efficiency

How a writer keeps the change the size the task needs. It binds the
writer: the worker.

## Writer scope

- Change only what the task needs, inside the files the prompt lists under
  `## Allowed edit surfaces` (test files and the change's LDD record are
  always in scope). Each path listed exists, or its top-level folder
  does; route-guard refuses a launch that names one matching nothing. A
  file outside them is a question for the parent, not an edit: return
  `blocked` naming it and why. In Claude Code, scope-guard refuses such a
  write, through Edit, Write, or the shell; the refusal is a stop, never a
  reason to reach the file another way. A shell write whose target
  scope-guard cannot resolve runs with a note: keep it inside the surfaces.
- Preserve the project's architecture and conventions. Invent no
  abstraction, helper, layer, or option the task does not use now; three
  similar lines are cheaper than a premature one.
- No drive-by refactor, rename, reformat, or dependency change. A problem
  found on the way is reported, not fixed.
- Never cut size cosmetically (whitespace, comments, tests) nor split a
  coherent change artificially.
- Preserve the working tree's unrelated changes, tracked or untracked.

## Checks

Run the focused tests while iterating, using the commands the prompt or
`jhon status --json` gives, as written; run the wider suite only when the
prompt authorizes it. Stop correcting a failing check once the attempts
stop converging; return `partial` with the failing command and its
output. A failure the prompt names as already failing on the base is
evidence, not this task's blocker.

## Return

End with text:

```text
status: success | partial | blocked
summary: <what changed and why, the task or requirement ids covered>
files_changed:
  - <path>: <change>
validation:
  - <exact command>: <observed result>
risks: <what remains, or none>
decision: <only when the owner must choose: the question and its closed options>
```

A choice for the owner is a closed set of options derived from the work,
never a request to author paths or commands. Report `partial` and `blocked`
honestly: an exact handoff beats a claimed completion.
In a session `jhon start` opened, a writer that stops without the `status:`
and `validation:` lines stops with a note to the owner (jhon hook
return-shape, the release plugin's SubagentStop hook).

## Measurement

`jhon audit` measures each writer against this contract, never as a
finding: the return shape (Claude Code; pi reports `n/a`), files written
outside its scope, the dependency manifests among them, and the most times
one command failed (over two is an unbounded correction loop).
