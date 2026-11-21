# test/

The conformance suite: black-box tests in Go that run the `jhon` binary as an
owner or a runtime would (a command line, or a hook with a JSON payload on
stdin) and judge its output, exit code, and effects on disk. The engine's
unit tests sit beside each package under `engine/`; these test the binary
from the outside.

Run everything with `jhon test` (it builds the binary, runs `go test ./...`,
jhon-shell's tests in `shell/test/`, and the coverage matrix), or one area with
`go test ./test/<area>/`.

| Path | What it tests |
|---|---|
| `guards/` | Every hook: the safety, write (vcs), framework, scope, and inspect guards, the context notice, the return shape, and `jhon grant`. |
| `ldd/` | The LDD commands: `status`, `context`, `risk`, `deliver`, `executions`, `compose`, `archive`, `mutate`, `init`'s discovery, and the VCS state. |
| `architecture/` | The engine's package structure: exactly the internal imports `imports.txt` records, `cli` composed only by `cmd/jhon`, and the leaf packages free of internal dependencies. |
| `render/` | The renderer: what `jhon sync` writes for Claude Code and pi, the validation rules, the runtime matrix, and the model profiles. |
| `release/` | `jhon sync`, `publish`, `start`, `sessions`, and `doctor`, in scratch homes over a scratch source repository. |
| `hooks/` | The repository's own git hooks: `tools/git-hooks/pre-push` runs its checks once per sha. |
| `audit/` | `jhon audit` on recorded sessions, the dashboard, and the MCP server. |
| `dashboard/` | `jhon dashboard --port` on a held port: an older Jhon dashboard replaced, one from this release left running, anything not proven Jhon's reported and untouched. |
| `install/` | The installers, outside Go: `install.sh` runs in full and `install.ps1` is checked statically (`python3 -I -m unittest discover -s test/install -p 'test_*.py'`); its README is the release and install guide. |
| `support/` | What the Go tests share: the binary (built once per run), the process runner, git fixtures, and a copy of the repository for tests that edit it. |
| `testdata/` | The data the tests read (below). |

## testdata/

Inputs, not code. Each set is shaped like what the engine reads in real use.

| Path | Contents | Used to check |
|---|---|---|
| `audit/` | Recorded sessions of Claude Code and pi (`pi-*`), with their sub-agents and `source.json` metadata; those marked `"real": true` are real sessions (personal paths and instructions redacted), the others built to shape. | `jhon audit` finds each violation it must and passes the clean sessions. `prose/contracts/vocabulary.yaml` names the real ones as each audit rule's evidence. |
| `status/` | LDD projects in every state: proposals, specs, designs, tasks, and test records under `openspec/`. | `jhon status` and `jhon context` derive the phase and the next action. |
| `compose/` | Main specs, delta specs, and the expected result, byte for byte. | `jhon compose` merges a delta exactly. |
| `spikes/` | Records of live runs against Claude Code and pi: what was called, what the runtime decided, and the control case. | The evidence `prose/agents.yaml` cites when it marks a runtime capability `verified`; `jhon validate` checks each record's shape. |
| `claude-md/` | `CLAUDE.md` files with Jhon's managed block. | The install keeps the owner's text and replaces only its own block. |

## Rules

- No test touches the real home, runs a real model, or pushes anywhere:
  every test gets a scratch `HOME`, fake `claude` and `pi`
  programs, and a local fake remote.
- A test that runs the binary on this repository sets `JHON_ROOT` itself
  (the test binary lives outside the tree); `support.CopyRepo` gives a test
  that edits the tree its own copy.
