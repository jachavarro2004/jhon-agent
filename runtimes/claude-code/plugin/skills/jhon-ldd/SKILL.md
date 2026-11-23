---
name: jhon-ldd
description: "Leapfrog-Driven Development for a change: /jhon-ldd <new|ff|continue|status|explore|research|apply|verify|archive|init> [change]; with no action it continues"
argument-hint: "[action] [change]"
disable-model-invocation: true
---

Arguments: `$ARGUMENTS`. The first word is the action and the rest the
change; for `init`, the rest names a Strict TDD policy or test command
overrides. With no arguments, or a first word that is no action below, the
action is `continue` and every word is the change.

| Action | Do |
|---|---|
| `new` | Write the change's intent and acceptance criteria in tasks.md `## Intent` and outline every slice, tasking the first; write a proposal only if the change needs one or the owner asks, per `~/.local/share/jhon/current/prose/skills/ldd/references/propose.md`; explore first (`~/.local/share/jhon/current/prose/skills/ldd/references/explore.md`) only when a decision needs facts code and memory lack. |
| `ff` | Run propose, then the first slice's spec and tasks, skipping any whose artifact is already `done` in `jhon status`; design only if the route asks for one (`jhon status` suggests it). Outline every slice first, task each in its turn (`~/.local/share/jhon/current/prose/contracts/ldd.md`, "Slices"). |
| `continue` | Run `jhon status <change> --brief` and run the suggested next phase for that change, unless the owner says otherwise; when the current slice is an outline (no tasks yet), write its spec delta and tasks first. |
| `status` | Run `jhon status <change>` from the project root and present its result; the suggested next step is a suggestion. Read-only. |
| `explore` | Explore it (inline within one bounded read batch, else in the `jhon-scout` agent), following `~/.local/share/jhon/current/prose/skills/ldd/references/explore.md`. Without a named change, write nothing. |
| `research` | Collect source-backed evidence with the `jhon-scout` agent, following `~/.local/share/jhon/current/prose/skills/ldd/references/research.md`, then write its envelope to the change's `research.md` (`## Sources`, `## Claims`). |
| `apply` | Run the apply phase (inline only for a small change; otherwise in the `jhon-builder` agent, a slice each, slices that share files one at a time), following `~/.local/share/jhon/current/prose/skills/ldd/references/apply.md`. |
| `verify` | Run the verify phase in the `jhon-checker` agent, independent of the writer, following `~/.local/share/jhon/current/prose/skills/ldd/references/verify.md`. |
| `archive` | Run the archive phase inline, following `~/.local/share/jhon/current/prose/skills/ldd/references/archive.md`. |
| `init` | Run the init phase in this project inline, following `~/.local/share/jhon/current/prose/skills/ldd/references/init.md`. |
