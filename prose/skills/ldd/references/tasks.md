## Tasks

Write tasks.md only when the change needs one: when the work cannot be
resumed from the diff. Jhon asks for it (`needs` in `jhon status --json`)
over about 400 lines of code, at risk high, or when more than one
component is touched. Create it yourself when you see several rules or
cases to cover or a design decision on the way. A change without one
keeps its intent in the commit or PR text.

Open `openspec/changes/{change-name}/tasks.md` with `## Intent`: why the
change exists and its acceptance criteria, what verify checks against
(or in `proposal.md` when one is written). Then outline every slice,
`## Slice <n>: <name>`: an integrable functionality that builds, tests,
and delivers alone, with `Delivers:` and the acceptance criteria it
meets or the value it brings. Only the current slice gets tasks, written
once the one before is delivered; revise the outline when evidence says
so. Only checkbox lines, outside fenced code, are tasks. Tasks in a later
slice, over 12 in a slice, or a slice without `Delivers:` get a note in
`jhon status`.

```markdown
# Tasks: {title}

## Intent

{Why the change exists.} Accepted when: {CA-01 ...}, {CA-02 ...}, {CA-03 ...}.

## Slice 1: {name}

Delivers: {CA-01, CA-02, or the value a user or caller observes}

- [ ] 1.1 RED {test file}: {behavior}
- [ ] 1.2 GREEN {file}: {change}
- [ ] 1.3 Affected tests pass and the slice is delivered

## Slice 2: {name}

Delivers: {CA-03}
```

- Each task names its files.
- A behavior's failing test comes before its implementation, run through
  `jhon executions run {change-name}` (RED, then GREEN).
- A slice that crosses layers lists its passes as its tasks, one layer each
  in dependency order, each naming its files and its one commit; tick each
  on its commit.
- With `delivery: change`, commit a slice only after its affected tests
  pass on the tree you commit; checking its tasks off afterwards is fine.

Save to Engram only a non-obvious decision.
