## Diagrams

Use the `archify` skill; never hand-write its HTML. Pick the type from what
the change alters: components and boundaries (`architecture`), steps and
gates (`workflow`), who calls whom (`sequence`), how data moves
(`dataflow`), states and transitions (`lifecycle`). One diagram per
concern.

In an LDD change, diagrams live in `openspec/changes/<change>/diagrams/` as
`<name>.spec.json` (the source) and `<name>.html` (delivered); commit both.
A diagram of real code is derived from CodeGraph, not memory. Iterate with
`validate`, accept with `deliver` once, and record its hashes. When a change
carries a diagram, verification checks the code against it and reports a
divergence rather than regenerating the diagram.
