# Spec Delta

The format the spec phase writes and `jhon compose` applies at archive.

A delta lives at `openspec/changes/<change>/specs/<domain>/spec.md` and
changes `openspec/specs/<domain>/spec.md`. When no main spec exists, the
file is a full spec and archive copies it into place.

## Structure

- Up to four sections: `## ADDED Requirements`, `## MODIFIED Requirements`,
  `## REMOVED Requirements`, `## RENAMED Requirements`.
- A requirement is `### Requirement: <name>`; a scenario inside it is
  `#### Scenario: <name>`. A RENAMED heading is
  `### Requirement: <old> → <new>`.
- Headings inside fenced code are content. Names compare exactly after
  trimming.
- An annotation is a whole line `(<Name>: <text>)` with a name from
  `delta_metadata`. Annotations stay in the change and never reach the main
  spec. REMOVED and RENAMED blocks must carry `(Reason: …)` and hold only
  their heading and annotations; MODIFIED should carry `(Previously: …)`.

A main spec puts its requirements at depth 2 (`## Requirement:`) or depth 3
(`### Requirement:`), scenarios one level below, one depth per file. The
depth is that of its requirement headings; with none, depth 3 when it has a
`## Requirements` section, else depth 2.

## Composition

- Operations apply in `delta_order`, so a MODIFIED block can name a
  requirement by the name a RENAMED block just gave it.
- A MODIFIED block replaces the main spec's block entirely; ADDED goes after
  the last requirement (or at the end of `## Requirements`, or of the file);
  REMOVED deletes; RENAMED changes only the heading's name. Headings take
  the main spec's depth.
- Every byte outside the touched blocks is preserved exactly.
- Compose refuses, writing nothing, on an unknown requirement, a missing
  required annotation, a duplicate or existing ADDED name, a malformed
  RENAMED heading, duplicate names in the main spec, a main spec of mixed or
  unsupported depth, or a structurally invalid delta. It names the file,
  section, and requirement on stderr.
- The result is written to a temporary file beside the main spec and
  renamed over it.

## Values

```yaml
main_spec:
  requirement_depths: [2, 3]
  requirements_section: Requirements
  depth_with_section: 3
  depth_otherwise: 2
```

```yaml
delta_order: [RENAMED, MODIFIED, REMOVED, ADDED]
delta_metadata: [Previously, Reason, Migration]
delta_required:
  REMOVED: [Reason]
  RENAMED: [Reason]
delta_heading_only: [REMOVED, RENAMED]
```
