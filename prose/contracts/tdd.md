## Strict TDD

**Mode: enabled.**

TDD always. New behavior gets a test observed failing for the
expected reason before the implementation made it pass; a bug fix starts
with a test that reproduces it. In a project that uses LDD, run the test
through `jhon executions run <change> --affected <base>` while it fails
(RED), unit runs (`--affected`) between, and `--affected <base>` once
it passes (GREEN): `jhon hook route-guard` refuses the PR of a
behaviour change without its RED before GREEN. Test through the
public surface. Never delete, skip, or weaken a failing test to go green.

For behavior that already exists, the witness is a mutation: break the code
temporarily, see the test fail, restore, see it pass. Run it with
`jhon mutate <file> --find <text> --replace <text> -- <test command>`, which
restores the file itself, not git. A new test observed
RED needs no mutation. A witness may be reported as:

```text
test: <file> — <test name>
witness: {RED|mutation}
command: <the exact command run>
failure: <the observed failing line>
green: <command>: <observed passing result>
```

Inside a project with `openspec/config.yaml`, `strict_tdd` in
`jhon status --json` reports the project's preference
(`strict_tdd_policy: require|forbid`, else whether its projects
are testable).
