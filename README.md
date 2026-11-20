<p align="center">
  <img src="docs/assets/brand/jhon-banner.gif" alt="Jhon" width="760">
</p>

<h1 align="center">Jhon</h1>

<p align="center">
  <b>Let the agent run. Deliver only what is proven.</b><br>
  An agent framework for Claude Code and pi.
</p>

### ▶ The presentation (3:43)

https://github.com/user-attachments/assets/16898e03-5aae-4702-9ebb-6b16a648962f

<details>
<summary>With Spanish subtitles</summary>

https://github.com/user-attachments/assets/cf117fce-99d9-456c-a42e-f91101aea2b7

</details>

Full resolution: [English](docs/assets/video/jhon-explainer.mp4) · [Spanish subtitles](docs/assets/video/jhon-explainer.es.mp4)

---

## Why Jhon

A coding agent left alone is either asked about everything or trusted with
everything. Jhon takes a third way: the agent gets **freedom of execution
within safety limits**, and **nothing is delivered without evidence**.

- **The limits are code, not advice.** Guards judge every tool call of
  every agent. A destructive command is refused, an outward-facing one is
  asked, and you can widen what passes with a mode or a timed grant.
- **The evidence is a record.** A change's test fails first (RED) and passes
  after (GREEN) in a recorded run; the commit that closes a slice is the
  tree that run tested; a verify comes before the pull request. A change
  without that record does not get its pull request opened.
- **The method is LDD**, Leapfrog-Driven Development
  (`prose/contracts/ldd.md`): value reaches the default branch in
  integrable, proven slices, with no ceremonies, and every rule stays only
  while the evidence backs it.

### What Jhon is

| Part | What it does |
|---|---|
| One orchestrator | The main session. It talks with you, plans the slices, reads the risk, and decides when to delegate. |
| Focused agents | `explore`, `worker`, `verify` and the 4R reviewers, each with its own tools and a launch message that carries what it needs. |
| Guards | Seven checks on every tool call, the same in both runtimes, run by one binary. |
| LDD | The delivery method: slices, recorded test runs, the slice commit and delivery checks. Always on. |
| Memory and the code map | Engram keeps decisions and each change's state; CodeGraph gives agents the code's symbols and callers. Both always on. |
| The `jhon` binary | The engine under both runtimes: the CLI, every hook, the records, the renderer, the audit and the sandbox. |

### What Jhon is not

- **Not a runtime or a model.** It runs inside Claude Code (`jhon start`)
  or pi (`jhon start pi`). Claude Code runs Claude; pi runs Claude or
  another provider, as the model profile says (`jhon profile`).
- **Not a change to your own setup.** A plain `claude` stays standard, and
  your global Claude Code settings and CLAUDE.md stay yours. Your own pi
  directories stay yours too: Jhon's pi lives in `.pi/jhon` in the home.
- **Not process or network isolation.** Each enforced rule holds on the
  surface its hook reads; a program that acts on its own is outside it. The
  OS sandbox (Linux, on by default there where it can run) narrows what a
  command can write and see, with no network isolation.
- **Not CI.** It records and checks evidence on your machine; your CI still
  runs on the pull request.

## A session, from your side

1. You start Jhon in a project: `jhon start` (Claude Code) or
   `jhon start pi` (pi).
2. You ask for something. Read-only work just happens. At the first code
   change in a project with no LDD layout, the session asks you once how
   to deliver and test, sets LDD up, and goes on.
3. The orchestrator outlines the change in slices, and for each one gets a
   failing test recorded, the code written, and the affected tests recorded
   green. It does small work itself and hands the rest to focused agents.
4. Guards judge every tool call on the way. What they refuse stays
   refused; what they ask, you answer, or your mode and grants answer for
   you.
5. A verify checks the slice. The pull request opens only with RED before
   GREEN and a passing verify of the tree the last run tested.
6. Afterwards, `jhon audit` reads the session for what went wrong, and the
   dashboard shows sessions, changes and usage.

## LDD: how a change reaches the default branch

![LDD: a change from its first slice to delivery](docs/assets/diagrams/ldd-flow.svg)

Jhon always works under LDD; there is no switch to turn it off. A project
keeps its record in `openspec/` or in a native `ldd/` of the same shape
(see [docs/ldd.md](docs/ldd.md)).

| Step | What happens | The command |
|---|---|---|
| Intent | Why the change exists and its acceptance criteria; a tasks.md, when there is one, outlines every slice (`## Slice <n>: <name>`, `Delivers:`). The risk tier sets the route. | `jhon risk`, `jhon status` |
| RED | A failing test, recorded before the code. | `jhon executions run <change>` |
| GREEN | The code, then the affected tests, recorded. A pytest project is narrowed by pytest-testmon; `jhon executions prime` builds the missing records. | `jhon executions run <change> --affected <base>` |
| Slice commit | Under `delivery: change`, the commit that closes a slice must be the tree its last run tested. | checked by `route-guard` |
| Verify | A light verify by the `verify` agent on the standard route; a full `verify-report.md` and the 4R lenses at high risk. | `jhon executions verify <change> --verdict pass` |
| Delivery | Ready when the tree is clean and HEAD's tree is the last passing run's. The pull request opens only with RED before GREEN, and a passing verify on the verified route. | `jhon deliver check <change>` |

**The rigor rises with the risk.** `jhon risk` reads a tier (passive,
medium, high) from the paths a change touches, and the route follows it:

| Route | When | Verification | 4R |
|---|---|---|---|
| `standard` | risk under the verified tier, whatever its size | the affected tests of each slice; a light verify is an advisory | when you ask, or `review_required` names it |
| `verified` | risk at the verified tier (`high`), or `review_required` names `verify` | an independent verification before delivery (`verify-report.md`) | automatic, with only the pertinent lenses |

**What the slice commit is checked for**, under `delivery: change`:

| Check | Refuses a slice commit when |
|---|---|
| Tested tree | the commit is not the tree the last test run tested, or that run was a unit's run (a bare `--affected`) |
| Scenario coverage | a scenario the slice adds or edits in the change's spec deltas is named by no test (a scenario copied unchanged into a MODIFIED block does not count) |
| Real-database reach | the slice changes a declared data-access file (`executions_data_access`) and no declared real-database test (`executions_database_tests`) of its recorded run reached it in pytest-testmon's record (with no record, the run and `jhon status` say the reach could not be checked) |

A test that turned green while only its own test file changed is never
refused: it is marked in the record and handed to verify and the 4R
lenses to judge whether it was weakened.

## Guards: around every tool call

![The guards around one tool call](docs/assets/diagrams/guards-tool-call.svg)

Every Jhon agent, the main session and each sub-agent, runs its tool calls
through `jhon hook pre-tool`. Each guard judges only its tools, in this
order; the first refusal answers, otherwise the first ask, otherwise the
call passes with every guard's note joined. Each guard carries a card: the
risk it covers and the real case that put it there (`jhon audit --all`
counts what each one costs).

| Guard | Judges | Covers |
|---|---|---|
| `inspect-guard` | Bash | a reviewer changing what it reviews: 4R reviewers stay read-only |
| `safety-guard` | Bash | a destructive or irreversible command (refused), a write or delete of a credentials file (refused), an outward-facing one without your consent (asked), a sub-agent's unbounded wait; a read, a write outside the project or one it cannot resolve is not limited (the sandbox bounds it) |
| `vcs-guard` | Edit, Write | a write into `.git/` or the grants, an agent writing Claude Code's settings, a write outside the project (asked at the top level); explore's write beyond its record runs with a note |
| `framework-guard` | Edit, Write, Bash | an agent editing Jhon or its installed release while it runs on it (Edit, Write), or running Jhon's own code by hand (Bash); a Bash write or git there is the sandbox's, where they are read-only |
| `secrets-guard` | Read, Grep, Glob, Edit, Write | a file that holds credentials (`.env`, keys) or a home credentials directory; a template such as `.env.example` passes |
| `scope-guard` | Edit, Write, Bash | a writer editing outside the edit surfaces its handoff names; a shell write whose target it cannot resolve runs with a note |
| `route-guard` | Edit, Write, Bash, Agent | LDD: a writer launched without its scope or with a surface that matches nothing, a change without its record, a slice commit of an untested tree, a PR without its docs, a merge without rebase; a code edit before LDD is set up runs with a note |

A file an agent writes is then formatted by its project's own formatter
(`jhon hook format-edit`). On Linux `sandbox.mode` is on by default where
the sandbox can run (one empty sandbox starts); where it cannot (no
bubblewrap, user namespaces off or blocked by AppArmor, a bubblewrap too
old) the default is off, commands run unsandboxed and `jhon doctor` warns
with the fix (an explicit `on` there refuses every command). A probe that
does not end within 5 seconds is no verdict: the mode stays on, commands are
wrapped as usual and fail with bubblewrap's own message if it cannot start,
and `jhon doctor` warns. With it on, an
allowed command runs inside `jhon sandbox run` (bubblewrap): the project, the
temporary folders, the caches and `sandbox.allow_write` are writable,
everything else is read-only, and the home's credentials are not there.
Jhon's source checkout, the one `jhon sync` installs from, stays read-only
unless it is the project itself: a linked worktree of it is another project,
and writes only its own files (and commits through the checkout's git
folder). A
line of git network commands (fetch, pull, push, ls-remote, clone, remote
update) working inside the project, or gh, runs outside the sandbox, still
judged by the guards: git over SSH and gh cannot run inside it. So does a
line of docker or docker-compose, the owner's deployment tool, with the
owner's environment (`DOCKER_HOST`, the docker context) and its volume
mounts not judged; `docker login`, `docker logout` and a change of docker
contexts keep the sandbox. That git
runs with the repository's hooks (configured hooks included), ssh command,
fsmonitor and credential helpers overridden; `url.*.insteadOf` and `include.*` in the repository's
configuration are not, an accepted residual risk. uv's tools go to a
sandbox-only folder under the cache, and uv's own tool folder in the home stays
read-only. A command that fails on a read-only
path ends with a note telling the agent to stop and name the path
(the `sandbox.allow_write` setting), and each run records a `jhon.sandbox`
telemetry event. There is no PID namespace: a child a command detaches
(`setsid`, `nohup`) outlives the sandbox's bubblewrap. With
`sandbox.mode = "on"` set and a sandbox that cannot run (no bubblewrap,
user namespaces off) every command is refused with the fix and `jhon
doctor` fails; `sandbox.mode = "off"` turns it off. Off, nothing guards a shell command's
writes outside the project: the guards leave them to the sandbox, so a
redirect, `dd`, a recursive `chmod` or a write to Jhon's installed files or
source there runs, and on Linux `jhon doctor` warns. macOS and Windows run
unsandboxed (off by default there) with a `jhon doctor` warning. See
`jhon sandbox --help`.

### What each runtime guarantees

Not every rule is enforced. `prose/agents.yaml` (`guarantees`) states, per
rule and runtime, how it holds, and `jhon doctor --runtime <name>` prints it:

| Runtime | Enforced (a hook refuses) | Behavioural (`jhon audit` checks after) | Instruction only |
|---|---|---|---|
| Claude Code | 5 | 12 | 4 |
| pi | 5 | 12 | 4 |

The enforced rules are the safety guard's destructive and outward-facing
rules, the framework boundary, a writer's scope, and the tool list a
sub-agent gets; explore's writes beyond its record run with a note, which
`jhon audit`'s explore-wrote reads. pi has no runtime isolation, so `jhon audit`
reads a pi worker or verify child's write outside its own tree; that check,
like test-weakened, verify-wrote, and archive-without-red, reports a
would-be violation until real fixtures back it.

## Agents: one orchestrator, focused helpers

![Jhon's agents and their launch context](docs/assets/diagrams/agents-launch-context.svg)

The orchestrator keeps the conversation and the plan. Propose, spec,
design, tasks, init and archive run inline in it; three executors carry
every LDD step that leaves its thread. Its delegation mode is the
profile's: `inline` (the default: understood work stays with it, and it
delegates on objective triggers) or `writer` (workers write; it edits at
most about 20 lines per request).

| Agent | Does | Writes |
|---|---|---|
| `explore` | investigates before a change: the code, prior decisions, approaches, outside evidence | only its change's exploration.md or research.md (another write runs with a note) |
| `worker` | builds a slice, or one layer of it | only the edit surfaces its handoff names |
| `verify` | checks a slice or a change independently, from tests that actually ran; fixes nothing | only verify-report.md |
| 4R lenses | review a frozen diff through risk, resilience, reliability and readability; a refuter and a validator then challenge the findings | nothing: the verdict goes to `review-4r.md` |

In pi the 4R runs natively in one tool call (`jhon review start`, then
every pertinent lens at once, then the refuter and validator, then
`jhon review record`); in Claude Code, `jhon review run` does the same in
one Bash call through `claude -p`. The `review-*` lens agents are its fallback.

**What each launch carries.** A sub-agent's first message is its brief,
followed by what Jhon found for it, each block left out on any failure,
timeout or empty result:

| Block | From | For |
|---|---|---|
| `## Allowed edit surfaces` | the orchestrator | a writer: its scope, which `scope-guard` holds it to |
| `## Decisions on record` | Engram, through `jhon context --handoff` | every sub-agent |
| `## Code map` | CodeGraph: `query` and `callers` for the files and symbols the brief names | every sub-agent, where the project has a `.codegraph/` index |
| `## Tests to judge` | the execution record, through `jhon context --judge` | verify and the 4R lenses |

**Memory.** Engram holds decisions, and the engine keeps one state record
per change in it (phase, slices, last run, delivery, code refs), written
after a slice commit, a light verify and an archive, never failing the
step. Claude Code's project memory is shared by both runtimes.

## Measured, not assumed

Every rule is a hypothesis with its evidence, and the evidence is measured.

- **`jhon audit`** reads a Claude Code or pi session afterwards for
  safety and delivery outcomes: `refusal-bypass`, `isolation-bypass`,
  `delivered-untested`, `edit-before-red`. It also measures context use,
  each change's slices and time, each writer's efficiency, how each agent
  used memory and CodeGraph, and your dialogue with the main thread, as
  statistics. `--all` sums every recent session; `--html` gives one page.
- **Rounds.** After a user story or a few sessions, `jhon audit --all`
  sets out the numbers, a short proposal says what to remove, adjust or
  add, and you decide. A guard that pays for nothing is a candidate to go.
- **The bench** (`tools/bench/README.md`, maintainer only) runs the same
  synthetic tasks with Jhon and without it, in both runtimes (`jhon`
  against `clean` Claude Code, `pi-jhon` against `pi-clean`), and judges
  Jhon by equal or better quality and better discipline, with the extra
  cost shown. `bench review-compare` gated the native 4R: on five fixtures
  with seeded defects it found the same 9 of 9 known defects as the lens
  agents for about 3.4× fewer quota-weighted tokens and 2.7× less wall time.

## Where you work

![Two runtimes over one engine](docs/assets/diagrams/runtimes-engine.svg)

| Place | How you open it | What you get |
|---|---|---|
| Claude Code | `jhon start` | `claude --agent jhon-orchestrator` with the release's plugin: the `/jhon-ldd` command and the read-only `jhon` MCP server (audit, status). Every other argument goes to claude; resume with `jhon start --resume <id>`. Your claude.ai connectors stay out unless `claude_code.connectors` is on. |
| pi, with Jhon's shell | `jhon start pi` | pi on Jhon's own agent directory with `jhon-shell`: the orchestrator, the sub-agents, the guards, and the shell's UI in rose ink. The first start installs the pinned pi packages. |
| The dashboard | `jhon dashboard`, or `/jhon:dashboard` in the shell | A read-only page on http://127.0.0.1:7480: Home, Sessions (each with its audit), Changes, Usage (tokens per day, money where pi recorded it, never estimated), Memory (Engram and MEMORY.md, read only) and Telemetry. |
| The audit | `jhon audit <session>`, `jhon audit --all` | What a session did against the rules, read-only; exit 1 only on a verified violation. |

The shell talks to Jhon only through the binary (`shell/CONTRACT.md`).
Its commands:

| Command | Key | Shows |
|---|---|---|
| `/jhon:status` | `alt+j` | the change's LDD detail |
| `/jhon-ldd <action>` | | an LDD action, as in Claude Code |
| `/jhon:agents` | `alt+s` | the session's sub-agents and each one's live thread |
| `/jhon:jobs` | | background commands and monitors |
| `/jhon:changes` | `alt+g` | what the session's agents changed, per worktree |
| `/jhon:panel` | `alt+r` | the rail's sections full width: LDD, todos, jobs, agents, stats, changes, integrations |
| `/jhon:usage` | `alt+u` | each provider's usage windows and resets: the session's provider and every provider its profile (the session's own included) routes sub-agents to; Claude and ChatGPT (openai-codex: 5h and week) from each reply, NaN from its quota |
| `/jhon:profile` | | the model profiles |
| `/jhon:grants` | | the project's grants |
| `/jhon:customize` | | the shell's settings, and the permission mode |
| `/jhon:commands` | `alt+k` | the command palette |

## You decide

The guards are fixed; how much passes without asking is yours. A mode and
a grant are set only by you: an agent may read the mode but never set it,
and never runs `jhon grant` (run them with `!` in Claude Code, or in your
own terminal in pi).

| Mode (`jhon mode`) | Passes without asking |
|---|---|
| `normal` | nothing beyond your live grants: the guards as they are |
| `yolo` | every grant class but `destroy`, and a 4R reviewer you did not ask for |
| `super-yolo` | that, plus `destroy` and a command the guard cannot read |

| Grant class (`jhon grant <class> --for 30m`) | Covers |
|---|---|
| `push` | a push to the default branch, of every branch, or of tags (a push to any other branch passes) |
| `gh-write` | a merge, a release, repository, secret and workflow writes (a PR's create, comment, ready and edit pass) |
| `publish` | a package publish (`npm publish`, `cargo publish`, …) |
| `remote` | a session on or a copy to another host (`ssh`, `scp`, `rsync`, …) |
| `deploy` | a cloud or cluster write (`aws`, `gcloud`, `kubectl`, `terraform apply`, …) |
| `outside-write` | an Edit or Write outside the project (a shell write there is the sandbox's) |
| `destroy` | a deletion that loses work (a forced branch delete, `reset --hard`); the rest of the destructive operations are never grantable |

**The hard lines, in every mode.** Refused: credentials (by the file tools; by the shell, a write or delete), the
never-grantable destructive operations, writes into `.git/`, the grants
and the modes, Jhon's own files. Asked: a write to Claude Code's settings,
and a `git config` that changes what git runs. A folder that is not a git
repository takes no mode and no timed grant; pi can let one session write
there (`jhon grant outside-write --session <id>`). See
[docs/modes-and-grants.md](docs/modes-and-grants.md).

**Settings** are one file, `.config/jhon/settings.toml` in the home:
`jhon settings list` shows every key with its value and source,
`jhon settings get <key>` one of them, and
`jhon settings set <key> <value>` writes one and refuses a value the schema
does not accept; `jhon settings migrate` drops the keys a release removed,
after a backup. The main groups:

| Group | Holds |
|---|---|
| `[runtimes]` | `claude_code`, `pi`: which runtimes are installed (both by default, at least one) |
| `[integrations]` | `engram` and `codegraph` (always on), `context7` (hosted documentation for explore and worker, on by default), telemetry (opt-in), the palette |
| `[sandbox]` | `mode` (`on` or `off`, on by default on Linux where the sandbox can run, off elsewhere), `allow_write` |
| `[agents]` | `act_on_announce`: a turn that announces an action and takes none is sent back once |
| `[pi]`, `[shell.*]` | the active profile, and the shell's appearance, behaviour, keys and notifications |

**Profiles** choose each agent's model per runtime (`jhon profile list`,
`jhon profile use <name>`); `default` is `prose/agents.yaml`. A pi model is
written `provider/model[@thinking]`, so a profile can run pi on Claude
through Claude Code (`claude-bridge`, the default), an API key, or another
provider. `jhon profile mode <profile> inline|writer` sets its delegation
mode.

## Get started

**Needs:** Linux on amd64 or arm64, or Windows with WSL 2; git; Claude Code
(`claude`) for the Claude Code runtime; node for pi; engram and codegraph.
`jhon setup` checks each one and says how to fix a missing one.

| Step | Command |
|---|---|
| Install | `curl -fsSL https://raw.githubusercontent.com/jachavarro2004/jhon-agent/main/install.sh \| sh` (Windows, in PowerShell: `irm https://raw.githubusercontent.com/jachavarro2004/jhon-agent/main/install.ps1 \| iex`) |
| Set up | `jhon setup`: pick the runtimes and the opt-in integrations, check what they need, write the settings, install |
| Start | `jhon start` (Claude Code) or `jhon start pi` (pi), inside your project |
| Check | `jhon doctor`: every health check, no network call (`--remote` adds the remote ones) |
| Update | `jhon update` (verified: sha256 and the release's cosign keyless signature), `jhon update --check`, `jhon update --rollback` |
| From source | `jhon sync` in a checkout: builds a release from the committed HEAD and installs it |

**Turning off or removing.** A runtime turned off in `[runtimes]` is
skipped by the next install and its directory is left as is. Jhon has no
uninstall command; an install lives in the home under `.local/bin/jhon`
(the installer's link), `.local/share/jhon/` (the releases and `current`),
`.config/jhon/` (settings and profiles), `.local/state/jhon/` (grants and
modes), `.pi/jhon/`, and the agents in `.claude/agents/` that
`.claude/agents/.jhon-owned` lists.

Jhon's source, its releases and its installers (`install.sh`,
`install.ps1`) live in one public repository,
[jachavarro2004/jhon-agent](https://github.com/jachavarro2004/jhon-agent),
whose `.github/workflows/release.yml` builds, signs and publishes every
release (`dist/README.md`). To verify a release by hand, with its tarball,
checksum and signature bundle in one directory:

```sh
sha256sum -c jhon-X.Y.Z-linux-amd64.tar.gz.sha256
cosign verify-blob \
  --bundle jhon-X.Y.Z-linux-amd64.tar.gz.sigstore.json \
  --certificate-identity "https://github.com/jachavarro2004/jhon-agent/.github/workflows/release.yml@refs/tags/vX.Y.Z" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  jhon-X.Y.Z-linux-amd64.tar.gz
```

A signature from any other repository, workflow or ref is not Jhon's.

## Commands at a glance

`jhon help` is the full usage; `jhon <command> --help` gives a command's
own help.

| Command | Does |
|---|---|
| `jhon setup` | the setup wizard, on a terminal |
| `jhon start [pi]` | start a session in Claude Code, or in pi |
| `jhon status [<change>]` | where a change stands: artifacts, tasks, last run, advisory next step; never a gate |
| `jhon risk` | the change's risk tier against the default branch, with a reason per file |
| `jhon executions run <change>` | run and record the change's tests (`--affected`, `prime`, `verify`) |
| `jhon deliver check <change>` | whether HEAD is the change's tested revision |
| `jhon review start` | the native 4R (`freeze`, `refute`, `validate`, `record`; `run` is all of it through `claude -p`, for Claude Code) |
| `jhon context` | one delegation's launch context as JSON |
| `jhon compose` | apply a delta spec to a main spec, atomically |
| `jhon archive` | move a change into the archive and verify it |
| `jhon audit` | audit one session, or every recent one with `--all` |
| `jhon dashboard` | the read-only dashboard |
| `jhon mode`, `jhon grant` | your permission mode and timed grants |
| `jhon sandbox run`, `jhon sandbox status`, `jhon sandbox profile` | the OS sandbox, whether it can run here, and how a command line runs in it |
| `jhon settings`, `jhon profile` | your settings and model profiles |
| `jhon doctor` | every health check; `--runtime <name>` for one runtime's guarantees |
| `jhon update`, `jhon version` | update or roll back; the binary's version |
| `jhon init` | read-only project discovery |
| `jhon telemetry` | what Jhon recorded of its own decisions, when telemetry is on |
| `jhon sync`, `jhon publish`, `jhon release pack` | maintainer: install from the checkout, push, pack an artifact |
| `jhon validate`, `jhon check`, `jhon test` | maintainer: the contracts, the rendered artifacts, the tests |

## Read more

| Document | About |
|---|---|
| [docs/ldd.md](docs/ldd.md) | LDD in a project: layouts, the set-up at the first change, a worktree per change, the change's state in Engram, the launch context |
| [docs/modes-and-grants.md](docs/modes-and-grants.md) | where modes and grants apply |
| [docs/status-json.md](docs/status-json.md) | the shape of `jhon status --json` |
| [CHANGELOG.md](CHANGELOG.md) | what changed, release by release |
| `prose/contracts/ldd.md` | the method itself |

## Working on Jhon

### Layout

Each top directory has a README that says what is in it and how it is used.

| Path | Holds |
|---|---|
| `engine/` | The `jhon` binary, in Go (`engine/cmd/jhon`, `engine/internal/`): the CLI, every hook, validation and rendering, status, the launch context, the audit. `engine/README.md` |
| `prose/` | Everything the agents read: `prose/agents.yaml` (every agent's tier, capabilities and body), `prose/orchestrator.md`, the persona, the contracts, the agents' bodies, the skills. `prose/README.md` |
| `runtimes/` | Each runtime's integration; `runtimes/claude-code/plugin/` is the plugin `jhon start` loads. `runtimes/README.md` |
| `shell/` | `jhon-shell`, the pi package, apart from the framework and talking to it only through the binary. `shell/README.md` |
| `dashboard/` | The dashboard page (`index.html`, `app.css`, `app.js`, no build step), reading only the JSON the binary serves. `dashboard/CONTRACT.md` |
| `test/` | The conformance suite, black-box tests of the binary by area (`guards/`, `ldd/`, `render/`, `release/`, `audit/`); `shell/test/` holds the shell's. `test/README.md` |
| `tools/` | The rule coverage matrix (`go run ./tools/coverage`), the git hooks, and the bench. `tools/README.md` |
| `docs/` | Design records and the README's diagrams. |

### How rendering works

Only `jhon sync` installs: it builds a release from the committed HEAD
under `.local/share/jhon/releases/<commit>/` in the home, validates it,
switches `current` to it atomically, and installs the projections from it.
It refuses a dirty tree, pushes nothing and keeps old releases. The
runtimes load that release, never the live source. Each agent in
`prose/agents.yaml` becomes a Claude Code agent (`.claude/agents/<name>.md`
in the home, the always-on layer inline and its guards in its frontmatter)
and a pi agent; your global CLAUDE.md and settings.json stay yours, and an
ownership record lets a removed agent be pruned. Every render validates
first.

### Development workflow

Clone the repository anywhere. A checkout run directly (`build/jhon`, from
`go build -o build/jhon ./engine/cmd/jhon`) is its own source; `jhon sync`
records the checkout it ran from in its install record, and the installed
`jhon` works on that one outside a checkout. `JHON_SOURCE` names
another. A checkout at `~/dev/jhon`, the old default, is still found when
nothing is recorded.

- Strict TDD for framework code (`contracts/tdd.md`).
- `jhon validate` checks references, the manifest, the vocabulary the code
  reads, command routing, and the safety statements.
- `go run ./tools/coverage` injects one defect per rule and expects it
  caught; run it on a clean, committed tree.
- `jhon test` runs the unit tests, then the coverage matrix.
- The pre-commit hook builds and validates, then runs gofmt, go vet,
  staticcheck and `go test -p 4` on the touched packages only, and tsc
  when the shell changed. The pre-push hook, on a push to `main`, runs the
  whole module's vet and staticcheck and the conformance suite; a sha that
  already passed on this machine is skipped. Install both with
  `git config core.hooksPath tools/git-hooks`.

## License

Apache-2.0: [`LICENSE`](LICENSE).
