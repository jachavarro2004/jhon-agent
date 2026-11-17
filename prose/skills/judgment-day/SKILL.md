---
name: judgment-day
description: "Trigger: judgment day, dual review, adversarial review, juzgar. Run explicit blind dual review, deliberate over disputes, and let the human decide what the judges cannot."
license: Apache-2.0
metadata:
  author: jhon
  version: "2.0"
---

## Judgment Day

Two blind judges on one frozen target, deliberation over what they dispute,
and the owner deciding the rest. Use it when the owner asks for it, or when
`review_required` in `openspec/config.yaml` names `jd` for the change; it
replaces 4R for that target. The protocol, record, and verdicts are
`~/.local/share/jhon/current/prose/contracts/judgment.md`; the prompts are in
[references/prompts-and-formats.md](references/prompts-and-formats.md).

1. Choose the mode: `defect` for frozen code, `docket` for a design
   decision. For a docket, agree the invariant with the owner first, list the
   options with removal among them, and state the burden of proof.
2. Write each event as a JSON file (for example under
   `~/.cache/jhon/judgment-events/`) and append it with
   `jhon judgment append <record> <event-file>`; never edit the record.
   Transcribe judge results and owner decisions verbatim. Start with `OPEN`.
3. R0: launch judge A (`jd-judge-a`) and judge B (`jd-judge-b`) in
   parallel, blind, with the same target, scope, and project skills.
4. Route only on `nextAction` from
   `jhon judgment status <record> --json`:
   - `next-round`: launch both judges fresh with the round prompt;
   - `escalate` or `human-decision`: ask the owner about each contested
     point (its claim, both latest positions and proof, outcomes `confirmed`
     or `dropped`) and append one `HUMAN_DECISION`;
   - `close`: in `defect` mode, hand the status's `fix` list, verbatim and
     nothing else, to `jd-fix-agent` (skip the fix round when it is empty), re-judge the fix delta with both judges, at most twice;
     then append `CLOSE`.
5. Close with `APPROVED` or `ESCALATED` (defect), `CLOSED` with the option
   and any conditions, or `ESCALATED` (docket). An owner who declines to
   decide, or anything severe left after the second fix round, is
   `ESCALATED`.

A judgment is information: it authorizes no commit, push, or release. Use
frontier models only on the owner's opt-in for that review, on both judges
(`model: fable` on both judges).

Return the record path, the mode, the rounds run, the points by status, the
owner's decisions, the fixes (defect) or chosen option (docket), and one
final line: `JUDGMENT: APPROVED ✅`, `JUDGMENT: CLOSED ✅`, or
`JUDGMENT: ESCALATED ⚠️`.
