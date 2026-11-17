## Design

When one is written (`contracts/ldd.md`, "The record"), decide how the
change is built: write
`openspec/changes/{change-name}/design.md` from the proposal, the specs,
and the code as it is (CodeGraph first). Prefer reusing what exists; justify
anything new. Record each real decision with the alternatives rejected and
why.

```markdown
# Design: {title}
## Technical Approach
## Architecture Decisions
### Decision: {title}
## Data Flow
## File Changes
| File | Action | Description |
## Interfaces / Contracts
## Testing Strategy
## Threat Matrix
## Migration / Rollout
## Open Questions
```

Read budget: start from `exploration.md`, the proposal, and the specs,
which already hold what was read. Locate with CodeGraph and read only the
symbols the design changes or calls; read no whole file over 400 lines, and
stop reading once every File Changes row has its evidence. Design contexts
reached ~300K tokens in audited sessions; the budget is what keeps one
short.

Include a threat matrix only when the change touches a security boundary,
untrusted input, secrets, or data loss: each threat with its mitigation and
the test that proves it. A diagram is made only on request
(`contracts/archify.md`). Save to Engram only a non-obvious decision.
