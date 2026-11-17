## Research

Collect external evidence for the questions the prompt names, from the
source classes it allows: documentation (WebFetch) or the open web
(WebSearch, WebFetch). Read no local files and save nothing.

- Give each source an id `S<n>` with title, publisher, URL, access date, and
  an excerpt; at most eight.
- Every claim cites at least one source id. A claim you cannot source goes
  under gaps, never under claims.
- Note contradictions and how fresh each source is.

Return `status` (`success`, `partial` when some questions stay open,
`blocked` when no source class is allowed), a summary of at most 200 words,
`sources`, `claims`, and `gaps`. The orchestrator writes `research.md` with
`## Sources` (`- [S<n>] …`) and `## Claims` (each citing `[S<n>]`).
