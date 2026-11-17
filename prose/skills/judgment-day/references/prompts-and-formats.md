# Judgment Day Prompts

## R0

```markdown
You are blind Judge {A|B}, Judgment Day R0, mode {defect|docket}.
Target: {frozen identity and exact paths}
Skills to load: {SKILL.md paths}
Criteria: correctness, edge cases, error handling, performance, security, project conventions.
Read-only: do not edit, delegate, or inspect unrelated scope.

Return one JSON object and no prose:
{"findings":[{"location":"path:line","severity":"CRITICAL","claim":"observable incorrect behavior","evidence_class":"deterministic","causal_disposition":"introduced","proof_refs":["concrete proof"]}],"evidence":["what was inspected"]}
Only those fields. When clean: {"findings":[],"evidence":["what was inspected"]}.
```

Docket addendum: the question, the invariant, the options (removal named),
and the burden of proof; each finding's `location` is `option:<id>` or a
path, and its claim says how the option meets or breaks the invariant.

## Round n

```markdown
You are Judge {A|B}, Judgment Day round {n}, mode {defect|docket}.
Target: {identity, or the docket question, invariant, options, burden of proof}
Your R0 result: {verbatim}
Opponent's R0 result: {verbatim}
Earlier rounds: {verbatim positions}
Contested points: {id, owner, claim}

Take one position on every contested point: on your own `maintain` (with proof_refs) or `concede`; on the opponent's `maintain` (with proof_refs), `adopt` (with proof from your own verification), or `retract`. Proof already in the record is not progress.

Return one JSON object and no prose:
{"positions":[{"point":"A1","position":"maintain","reason":"why","proof_refs":["concrete proof"]}]}
```

## Fix actor

```markdown
Confirmed points: {id, location, claim, proof}
Skills to load: {SKILL.md paths}
Fix only these, each with its focused test result. Report each as fixed, add no points, and return.
```
