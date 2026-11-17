# engine/

The `jhon` binary, in Go: the CLI, every hook, and the renderer. One binary
does all of it; `jhon sync` builds it into each release as `build/jhon`, and
the installed CLI and every runtime hook run that copy.

Build it with `go build -o build/jhon ./engine/cmd/jhon` (from the repository
root, where `go.mod` is). A binary outside a release or checkout finds its
tree through `JHON_ROOT`.

## How a call is routed

`cmd/jhon/main.go` hands the arguments to `internal/cli`:

- `jhon hook <name> [args]` runs a hook handler from `internal/hook`
  (`hook.Handlers`), which reads the runtime's JSON payload on stdin and
  answers with an exit code (2 refuses), a message, or an ask.
- `jhon <command> [args]` runs a command registered in
  `internal/cli/cmd_<command>.go`. `jhon help` lists them, and `jhon
  validate` checks that every `jhon <command>` the prose names is one of
  them.

## Packages (`internal/`)

| Package | What it does |
|---|---|
| `cli` | Routes a call; one `cmd_<name>.go` per command. |
| `hook` | The hook handlers: `safety-guard`, `vcs-guard`, `framework-guard`, `scope-guard`, `inspect-guard`, `context-notice`, `return-shape`. |
| `guard` | What the guards decide beyond one hook: the destructive and outward-facing operations, protected paths, a sub-agent's unbounded wait loops, and a writer's edit surfaces (shared with the audit). |
| `shell` | Reads a shell command line as the guards judge it: parses it (mvdan.cc/sh) and reduces it to programs, arguments, redirections, and inline code. |
| `grant` | The owner's time-boxed grants of an ask class (`jhon grant`). |
| `project` | The project root of a directory, and where a write may land without asking. |
| `session` | Whether a session predates the last install, so a refusal can say so. |
| `render` | Projects `prose/` onto Claude Code (`claude.go`) and pi (`pi.go`), validates the prose (`validate.go`), and installs from a release. |
| `manifest` | Reads `prose/agents.yaml`: agents, tiers, capabilities, and the runtime capability matrix. |
| `contract` | Reads the values the contracts declare (`prose/contracts/vocabulary.yaml` and small YAML blocks in the contracts). |
| `profiles` | The owner's model profiles (`jhon profile`). |
| `release` | `jhon sync` (build, test, preflight, activate, install) and `jhon publish`, and the roots both resolve. |
| `ldd` | The LDD records a change carries: `status`, `context`, `risk`, `deliver`, strict TDD, verification, and `archive`. |
| `discover` | `jhon init`'s read-only project discovery and the resolved projects. |
| `execution` | Runs and records a change's test runs (`jhon executions`): which projects `--affected` selects, and how pytest-testmon, Nx, and Vitest narrow them. |
| `requirements` | The structure of a spec file. |
| `compose` | Applies a delta spec to a main spec (`jhon compose`). |
| `judgment` | Judgment-day records: validates and appends events (`jhon judgment`). |
| `mutate` | The mutation witness for existing behaviour (`jhon mutate`). |
| `vcs` | The VCS state of a project root. |
| `audit` | `jhon audit`: reads a session after the fact (Claude Code transcripts, pi sessions in `pi.go`), sub-agents included, and reports safety findings and outcomes. |
| `dashboard` | `jhon dashboard`: a local read-only page of recent sessions and their audits. |
| `mcp` | `jhon mcp`: a read-only MCP server exposing `audit` and `status`. |
| `toml` | A structural TOML reader, enough for `pyproject.toml`. |
| `isodate`, `js` | Timestamps, JSON, and string rules exactly as Jhon's records and outputs need them. |

## Layout

- A Go package is one directory, so each directory's files are flat: `cli/`
  holds one `cmd_<command>.go` per command, and the code of `jhon
  executions` is `cli/cmd_executions.go` plus the `execution` package.
- Unit tests sit beside each package (`*_test.go`), where Go lets them reach
  its unexported names; the black-box conformance suite, which runs the built
  binary from outside, is under `test/` (see its README).
- `cli/cmd_testsuite.go` is the `jhon test` command, not a test: a file
  named `*_test.go` would be one, and left out of the binary.
- The imports between packages are fixed: `test/architecture/imports.txt`
  lists every one, and its test holds the tree to that list. Only
  `cmd/jhon` imports `cli`; `js`, `isodate`, `contract`, `toml`, and
  `requirements` are leaves.
