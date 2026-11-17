# prose/

Everything the agents read, and the manifest the engine renders from. No
code lives here: the engine (`engine/`) reads these files, validates them
(`jhon validate`), and projects them onto each runtime.

## How the prose is used

- `jhon sync` copies the committed tree into a release; the agents read this
  directory there, as `~/.local/share/jhon/current/prose/…`. An agent that is
  told to read a contract gets that path.
- A reference inside the prose is relative to this directory:
  `contracts/tdd.md` means `prose/contracts/tdd.md`. A path into the rest of
  the tree names its top directory (`engine/…`, `runtimes/…`, `test/…`).
- `jhon validate` holds the prose to its own rules: every reference resolves,
  every `jhon <command>` named exists, the vocabulary the engine reads is well
  formed, and the anchor sentences that state the safety rules are present.
  The pre-commit gate runs it.

## Contents

| Path | Read by | What it is |
|---|---|---|
| `agents.yaml` | the engine | The manifest: every agent's tier, capabilities, description, body or body template, and the runtime capability matrix (`jhon doctor --runtime <name>` reports it). |
| `orchestrator.md` | the `jhon-orchestrator` main agent | Its contract, delivered as its body. |
| `orchestrator-writer.md` | pi's shell, in writer mode | The writer-mode delegation rules, rendered to `~/.pi/jhon/jhon-orchestrator-writer.md`; the shell puts them in place of the orchestrator's region between `<!-- delegation:begin -->` and `<!-- delegation:end -->`, and keeps that region when the file or a marker is missing. |
| `agents/<name>.md` | that sub-agent | Its body. |
| `agents/templates/` | the engine | Bodies shared by several agents (the review lenses, the judgment-day judges), expanded per agent. |
| `persona/jhon.md` | every main agent | How Jhon speaks to the owner. Sub-agents get a short identity line instead. |
| `contracts/` | agents, and the engine for `vocabulary.yaml` | The rules. Always on, inlined in every agent: `language`, `shell-tools`, `remote-auth`, `runtime-boundaries`, `codegraph`, `engram`, `tdd`, `archify-activation`, `routing`. Read on demand: `ldd`, `delivery`, `delegation`, `asked-operations`, `witness`, `spec-delta`, `efficiency`, `judgment`, `review-4r`, `archify`. |
| `contracts/vocabulary.yaml` | the engine | The values the code reads (states, phases, record fields, audit rules), so prose and code cannot drift apart. |
| `templates/claude-md-block.md` | the engine | The fixed text around the always-on layer; each `{{group}}` is replaced by that group's files. |
| `skills/<name>/` | agents, through the runtime's skill loader | Skills Jhon owns: `ldd` (Leapfrog-Driven Development, with one reference per step) and `judgment-day` (two blind judges and the owner). |

## Editing

Edit here, commit (the pre-commit gate validates), then `jhon sync` installs
the change. Never edit the rendered copies under `~/.claude` or
`~/.pi/jhon`: the next sync overwrites them.
