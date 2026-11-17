---
name: ldd
description: "Leapfrog-Driven Development: one reference per step of a change's record. Trigger: an LDD step is run, inline or by the explore, worker, or verify agent."
disable-model-invocation: true
user-invocable: false
license: MIT
metadata:
  author: jhon
  version: "4.0"
---

## LDD

The steps of a change's record, their artifacts, who runs each, and what
every step shares are in `~/.local/share/jhon/current/prose/contracts/ldd.md`.
Read only the
reference of the phase you run:

| Phase | Reference |
|---|---|
| init | `~/.local/share/jhon/current/prose/skills/ldd/references/init.md` |
| explore | `~/.local/share/jhon/current/prose/skills/ldd/references/explore.md` |
| research | `~/.local/share/jhon/current/prose/skills/ldd/references/research.md` |
| propose | `~/.local/share/jhon/current/prose/skills/ldd/references/propose.md` |
| spec | `~/.local/share/jhon/current/prose/skills/ldd/references/spec.md` |
| design | `~/.local/share/jhon/current/prose/skills/ldd/references/design.md` |
| tasks | `~/.local/share/jhon/current/prose/skills/ldd/references/tasks.md` |
| apply | `~/.local/share/jhon/current/prose/skills/ldd/references/apply.md` |
| verify | `~/.local/share/jhon/current/prose/skills/ldd/references/verify.md` |
| archive | `~/.local/share/jhon/current/prose/skills/ldd/references/archive.md` |

`jhon status <change> --json` says which artifacts exist and suggests the next
phase; the suggestion never binds. A change's intent goes in its commit or
PR text, or under `## Intent` in tasks.md when it has one; what a change
needs is in `contracts/ldd.md`, "The record".
