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

Before testing behavior that already exists or reporting a witness, read
`~/.local/share/jhon/current/prose/contracts/witness.md`.
