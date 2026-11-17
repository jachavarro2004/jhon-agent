## Spec

Write what the current slice must do (`contracts/ldd.md`,
"Slices"), as delta specs in
`openspec/changes/{change-name}/specs/{domain}/spec.md` (or
`specs/{owner}/{capability}/spec.md` for a grouped layout). The format is
`~/.local/share/jhon/current/prose/contracts/spec-delta.md`; read the main spec
first, and add to the change's delta when a later slice comes, rather
than writing every slice's scenarios up front. Use RFC 2119 keywords and observable scenarios. Every requirement
outside REMOVED and RENAMED needs at least one scenario.

```markdown
# Delta for {Domain}

## ADDED Requirements

### Requirement: {name}

The system MUST {behavior}.

#### Scenario: {name}

- GIVEN {precondition}
- WHEN {action}
- THEN {outcome}

## MODIFIED Requirements

### Requirement: {existing name}

{the full replacement text and every surviving scenario}
(Previously: {one line})

## REMOVED Requirements

### Requirement: {name}

(Reason: {why})
(Migration: {what replaces it, or None})

## RENAMED Requirements

### Requirement: {old} → {new}

(Reason: {why})
```

For a domain with no main spec, write a full spec instead
(`# {Domain} Specification`, `## Purpose`, `## Requirements`). Save to
Engram only a non-obvious decision.
