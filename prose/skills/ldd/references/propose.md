## Propose

Turn an idea into the change's intent, `openspec/changes/{change-name}/proposal.md`,
only when the route or the owner asks for one (`contracts/ldd.md`, "The
record"); otherwise the intent goes under `## Intent` in tasks.md when the
change has one, or in the commit or PR text.
Product decisions come confirmed in the prompt; an open one is returned to
the orchestrator for the owner, not guessed. Read the main specs the change
touches (`openspec/specs/`) first.

`proposal.md` needs `## Why`, `## What Changes`, and
`## Acceptance Criteria` (what a user or caller observes when the change
is done; status notes a proposal without them), and no
placeholder (`TBD`, `TODO`, `<...>`). `jhon risk` reads the tier and
tasks.md, when there is one, names the slices, so the proposal restates neither. Add another
section only when it carries a decision, such as a rollback plan:

```markdown
# Proposal: {title}
## Why
## What Changes
## Acceptance Criteria
```

Save to Engram only a non-obvious decision.
