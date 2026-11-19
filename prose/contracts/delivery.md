# Delivery

Act on `jhon risk`'s tier:

| Tier | Verification | The owner | Review |
|---|---|---|---|
| passive | read the diff back | not asked | none |
| medium | run the affected tests and show the result | asked only on a question trigger (`contracts/routing.md`) | only when asked |
| high | an independent verification on record: the `verify` agent's passing `verify-report.md`, or a Judgment Day record closed `APPROVED`; `jhon deliver check` is not ready without one | told the tier's reasons when delivery is asked for | `verify` and an automatic 4R with only the pertinent lenses, recorded in `review-4r.md`; `review_required` always wins; Judgment Day only when the owner asks |

A session no one can answer proceeds and reports the tier and its reasons
instead of asking. **The record is always; the rigor is the change's, not
yours.** Every change that alters behaviour carries the LDD record it
needs: always its RED and GREEN runs and the commit or PR text that says
what was asked; tasks.md when Jhon asks for it. Create tasks.md yourself
when you see several rules or cases to cover or a design decision on the
way. The record is built in slices outlined first, each tasked after the
last is delivered, and delivered alone or, with `delivery: change` in
`openspec/config.yaml`, committed on the change's branch and merged once
with rebase after the last. The rest: `contracts/ldd.md`.

Name `jhon deliver check`'s result when asking to deliver; an untested
revision is called untested. Order: after the GREEN run, commit
the tree it tested, then `jhon deliver check`.
A run whose tested tree is the commit's needs no re-run.
A slice's commit holds exactly the tree of its last recorded passing run:
RED, GREEN, recorded run, commit.

**About 400 changed lines per delivery is a note**, never a cap, a
forced split, or a reason for `verify`. Over it, offer slices that each
build, test, and roll back alone; the owner picks, told `jhon status`'s
count.

**Check proportionately.** Before delivery, `--affected <base>` narrows
each project to the tests the change reaches, shared behavior included. A
suite the PR's CI runs whole is never run whole here too: its green checks
test the delivery. A whole suite run by hand gets a note, never a refusal.
On a wide selection, only the change's test modules run here; merge only
after the PR's CI is green. Wait for that CI as one background job,
`gh run watch <run-id> --exit-status`, whose exit is the notice; never a
`sleep` loop that polls it. A command that did not pass keeps its failed
tests (`failed_tests`) and the end of its output (`output_tail`) in the
record: read them there. For a wide or long run, tell the owner the
`--dry-run` estimate, and when it should end, as you start the run, never
a guess from the files changed.

A 4R's correction is one bounded `worker`, then `verify` confirms
(`contracts/review-4r.md`). Ask the owner only before a 4R or a Judgment
Day that is not due and the owner did not ask for; when the owner asked,
run it: the guard's own confirmation is the single ask.
Judgment Day (`contracts/judgment.md`) runs only when the owner asks, or
`review_required` names it.

One review per code change: 4R reviews code; Judgment Day judges a design
decision or a docket, and on code only when a severe 4R finding stays
contested. A due review's record gates the PR; only blocking findings
warrant a fix: an advisory never starts a correction, in verify as in 4R.
