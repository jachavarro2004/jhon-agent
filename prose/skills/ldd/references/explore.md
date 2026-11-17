## Explore

Investigate a question before anything is proposed: what exists, what it
touches, and which approaches are open.

- When `openspec/changes/{change-name}/exploration.md` already exists, read it first and extend it:
  investigate only what it leaves open, and add a dated section rather
  than starting over.
- Locate code with CodeGraph first (`contracts/codegraph.md`), then read
  what the question needs. Search Engram for prior decisions.
- Compare real approaches with their tradeoffs; recommend one.
- Without a named change, return the analysis and write nothing. With one,
  write `openspec/changes/{change-name}/exploration.md` and no other file;
  it must name the repository paths it inspected (backticked), or status
  reads it as `partial`. Save to Engram only a non-obvious decision.

A useful shape:

```markdown
## Exploration: {topic}
### Current State
### Affected Areas
### Approaches
| Approach | Pros | Cons | Effort |
### Recommendation
### Risks
```
