# Judgment

Two judges examine one frozen target blind, deliberate over what they
dispute, and hand the owner only what they cannot settle. The
`judgment-day` skill (`~/.local/share/jhon/current/prose/skills/judgment-day/SKILL.md`) drives it; `jhon judgment append` and
`jhon judgment status` enforce the record rules from the values at the end
of this file.

## Modes

- **defect**: the target is frozen code (a commit, diff, or paths at a
  recorded identity). Only severe findings (`judgment_severe`) are points.
  Confirmed points (`jhon judgment status` lists them under `fix`) are
  fixed afterwards, and nothing else, in at most two fix rounds, each
  followed by a scoped re-judgment of the fix delta. Verdicts: `APPROVED` or
  `ESCALATED`.
- **docket**: the target is a design decision. OPEN records the question,
  the invariant agreed with the owner, the options (always including
  removal, and the smallest change that keeps the invariant), and who
  carries the burden of proof. Every finding is a point.
  Verdicts: `CLOSED` (naming the option, possibly with conditions) or
  `ESCALATED`.

## Protocol

1. **OPEN** names the mode and the target.
2. **R0**: both judges review in parallel without seeing each other. Their
   results are immutable.
3. **Rounds**: each judge is launched fresh with both R0 results and every
   earlier round, and takes one position on every contested point.
4. **Human decision** on the points still contested when deliberation stops.
5. **CLOSE** with a verdict valid for the mode.

A point is a finding named by judge and index (`A2`). Positions:

| Position | Who | Evidence |
|---|---|---|
| `maintain` | owner or opponent | `proof_refs` required |
| `concede` | owner | optional |
| `adopt` | opponent | its own new `proof_refs` required |
| `retract` | opponent | optional |

| Owner \ Opponent | `maintain` | `adopt` | `retract` |
|---|---|---|---|
| `maintain` | contested | confirmed | unconfirmed |
| `concede` | dropped | contested | dropped |

Only `confirmed` points are fixed. A round made progress when a judge
conceded, retracted, or cited proof new to the record. Deliberation stops on
a round without progress (`no-progress`) or at the mode's round limit
(`round-limit`).

## Record

An append-only markdown file: inside an LDD change
`openspec/changes/<change>/judgments/<date>-<slug>.md`, otherwise
`docs/judgments/<date>-<slug>.md`. Each event is a `## <event>` heading and
one fenced `json` block whose `prev` is the SHA-256 of every earlier byte.
Write it only through `jhon judgment append <record> <event-file>`; route
on `nextAction` from `jhon judgment status <record> --json`
(`next-round`, `escalate`, `human-decision`, `close`, `closed`).

| Event | Fields |
|---|---|
| `OPEN` | `mode`; defect `target`; docket `question`, `invariant`, `options` (`id`, `summary`), `removal_option`, `burden_of_proof` |
| `R0.A`, `R0.B` | `judge`, `result` (`findings` with `location`, `severity`, `claim`, `proof_refs`; `evidence`), optional `execution` |
| `R<n>.A`, `R<n>.B` | `judge`, `positions` (`point`, `position`, `reason`, `proof_refs`), optional `execution` |
| `HUMAN_DECISION` | `decisions` (`point`, `outcome`, `rationale`) |
| `CLOSE` | `verdict`; docket `CLOSED` also `option` and optional `conditions`; `summary` |

`execution` is kept for records written when judge B ran outside the
runtime; an agent judge writes none.

## Judges

Judge A (`jd-judge-a`) and judge B (`jd-judge-b`) are agents of the
principal runtime, each fresh and blind to the other. Frontier models are
used only on the owner's explicit opt-in for one review, on both judges.

## Values

```yaml
judgment_modes: [defect, docket]
judgment_judges: [A, B]
judgment_events: [OPEN, R0.A, R0.B, R<n>.A, R<n>.B, HUMAN_DECISION, CLOSE]
judgment_verdicts:
  defect: [APPROVED, ESCALATED]
  docket: [CLOSED, ESCALATED]
judgment_round_limit:
  defect: 2
  docket: 6
judgment_positions: [maintain, concede, adopt, retract]
judgment_owner_positions: [maintain, concede]
judgment_opponent_positions: [maintain, adopt, retract]
judgment_point_status: [contested, confirmed, unconfirmed, dropped]
judgment_human_outcomes: [confirmed, dropped]
judgment_severities: [BLOCKER, CRITICAL, WARNING, SUGGESTION]
judgment_severe: [BLOCKER, CRITICAL]
judgment_next_action: [next-round, escalate, human-decision, close, closed]
judgment_escalation_reason: [no-progress, round-limit]
judgment_record:
  change: openspec/changes/<change>/judgments/<date>-<slug>.md
  standalone: docs/judgments/<date>-<slug>.md
```
