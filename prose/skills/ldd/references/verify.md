## Verify

Prove the change, up to its last delivered slice, does what its artifacts
say, from tests that actually ran. A later run replaces the report and
covers every slice so far.
Read whichever of `proposal.md`, `specs/`, `design.md`, and `tasks.md`
exist; check what they allow and name what could not be checked. The
acceptance criteria are those of `tasks.md` `## Intent`, or of
`proposal.md` when it exists, or, with neither, of the commit or PR text.

- **Write the report first.** Your first write is
  `openspec/changes/{change-name}/verify-report.md` with `verdict: fail`
  and the headings below; fill each section as you check it, and change the
  verdict to `pass` only when every check is done. A verifier stopped short
  still leaves what it found, and never a pass it did not reach.
- **Tests that ran.** When `jhon deliver check {change-name}` says the last
  run passed on HEAD's tree, its `execution-record.json` is the test
  evidence: cite its commands, exit codes, and tree, and run nothing again.
  Otherwise run `jhon executions run {change-name} --affected <base-ref>`
  and report each command and exit code. Run the build or type check when
  there is one. Static reading alone is not verification.
- A scenario is covered only by a test that passed. Count requirements and
  scenarios from the specs; never estimate.
- Classify every finding as **blocking** (a violated requirement or required
  check, or a real defect) or **follow-up** (anything else). Any blocking
  finding makes the verdict `fail`.
- Fix nothing. Never overwrite a recorded failure.

Write `openspec/changes/{change-name}/verify-report.md`:

```markdown
# Verify: {change-name}

verdict: pass | fail

## Test runs
| Command | Exit | Tree |
## Compliance
| Requirement | Scenario | Test | Result |
## Blocking
## Follow-up
```

`PASS WITH WARNINGS` states `verdict: pass`.

On a change whose risk reaches the verified tier, 4R is due as well and is
not verify's: it leaves `openspec/changes/{change-name}/review-4r.md` with a
`lenses:` line (the lenses it ran: `risk`, `resilience`, `reliability`,
`readability`) and `verdict: pass` (`contracts/review-4r.md`). When
review-4r.md lists inferential severe findings, confirm or drop each against
the frozen candidate and its tests, and say which; once a `worker` has
corrected the confirmed set, confirm the correction holds. Report a
missing one, or a `review_required` review `jhon status` lists as due, as a
follow-up; the route guard holds the PR for it. Save to Engram only a
non-obvious decision, and return the verdict and every finding with its
class.

### Light verify

When a slice of a change that alters behaviour closes on the standard
route, verify it lightly instead: read the slice's diff and its tests, run
the focused tests (`jhon executions run {change-name} --affected <base-ref>`),
write no report, and record the verdict beside the change's runs:
`jhon executions verify {change-name} --verdict pass|fail --note "<why>"`.
A blocking finding is `fail`. A PR with no `pass` on the last run's tree
gets a note; the verified route keeps the full report above.
