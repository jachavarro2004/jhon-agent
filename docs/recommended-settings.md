# Recommended settings for the owner's work project

The least intrusive setup that keeps every guard on while you work in
super-yolo across several repositories. Measured on jhon fdadbf9 (T260), Linux,
with copies of the settings and state (`JHON_SETTINGS`, `XDG_STATE_HOME`). The
shape: a primary at `~/Projects/work/<primary>`, its `<primary>-worktrees`
folder, and sibling repositories at `~/Projects/work/<repo>`.

## The settings

| Setting | Value | What it costs | What it protects |
|---|---|---|---|
| `sandbox.mode` | `on` | About 90 ms per Bash call: the hook took 109 ms (median of 5) with the sandbox on and 91 ms with it off, and starting the sandbox (`jhon sandbox run -- true`) took 76 ms. A write outside the writable folders stops with a note that names the path. Claude Code's own Bash deny rules stop matching (`jhon sandbox status` counts them). | Every Bash command (the model's own, a test, an install script) can write only to the project, its worktrees, the workspace members, `sandbox.allow_write`, `/tmp` and the toolchain caches. `~/.ssh`, `~/.aws`, gh's login and the docker socket are hidden. Jhon's settings, modes and grants are read-only. |
| `sandbox.allow_write` | every repository you work in that is not a listed member, plus any folder outside the project you want commands to write (for example `<media folder>`) | Nothing per call. Each folder is writable by any sandboxed command, including its `.git` (config and hooks too). Since T281 the guards count it as the project too: an Edit or Write there no longer asks outside-write, and a removal there is judged as in the project. | Every repository left out stays read-only. Listing a repository also lets `git push` and `fetch` there run outside the sandbox. If you leave it out, they run inside the sandbox and fail, because the SSH agent is not there. Removing the folder itself or a repository in it, its credentials, `.git` config and hooks stay refused. |
| Workspace members | each sibling named under `workspace:` in the primary's LDD config, then `jhon workspace join <primary>` in it | One line in the primary's config (a commit) and one join per member | A member that is listed and joined is writable from the primary's sandbox even without `allow_write`. An Edit there counts as inside the project, and route-guard and `jhon executions run --repo` read it. A sibling that joined but is not listed is no member: `jhon workspace list` and `jhon doctor` say so, with the `workspace:` entry that lists it (T281). |
| Modes | `jhon mode super-yolo` in the primary | Nothing is asked: push, gh-write, publish, remote, deploy, outside-write and destroy all pass with a note. Writing Claude Code settings or a git config still asks in every mode (`grant/mode.go`). Since T281 a listed, joined member with no mode of its own takes the primary's in a session of the workspace (the note says `super-yolo mode, from <primary>`; `jhon mode` in the member says it too), so no member needs its own. | A refusal still refuses. Every pass is logged for `jhon audit` (`modes/allowed.jsonl`). To keep one member `normal`, run `jhon mode normal` in it: a member's own mode is kept. A repository only `allow_write` names takes no other mode. |
| Grants | none while the primary has its mode. For a repository outside the workspace you want to keep `normal`, use `jhon grant push --for 8h --root <repo>` (the default is 30m). | It expires, and the next push asks again. | One class, one repository, for a limited time |
| `sandbox.deploy_tools` | `aws,cdk,sam` (as today) | A tool runs outside the sandbox only on a line of its own, called by its name. `AWS_PROFILE=x aws …` stays inside the sandbox, so use `--profile`. `aws --version` took 0.6–1.2 s. | `~/.aws` is never visible to any other command. A shell or runner (`npx`, `bash`, `python`) cannot be listed. |
| `sandbox.docker` | `false` (the default) | A docker call inside a longer line cannot reach the daemon, and the error comes with a note. `docker version` on a line of its own runs outside (0.1–0.2 s). | A container cannot write anywhere you can write, and cannot keep running after the command ends. |
| Executions timeouts | no setting: use `jhon executions run --timeout <minutes>` per run (default 20 minutes; 180 for a first or pytest-testmon rebuild run) | A project that hits the deadline is recorded as cut, and the run moves on. | A hung test cannot hold the session. |

## What each place gets (session pinned to the primary, recommended settings)

| Place | Tree | `.git` index and refs | `git push origin main` runs | Push from a top-level agent, super-yolo in the primary only | Super-yolo there too |
|---|---|---|---|---|---|
| The primary | writable | writable | outside | allowed, with a note | — |
| A linked worktree (measured on a scratch copy of the shape) | writable | writable, through the common dir | outside | allowed (read in the code, not measured: it takes the primary's mode) | — |
| Its member (listed, joined) | writable | writable | outside | allowed, with a note naming the primary (T281; asked before) | allowed, with a note |
| A sibling that is joined but not listed, and not in `allow_write` (said by `jhon workspace list` and `jhon doctor` since T281) | **read-only** | **read-only** | **inside (fails)** | **asks** | allowed, with a note |
| The same sibling in `allow_write` | writable | writable | outside | **asks** | allowed, with a note |
| A repository declared nowhere | read-only | read-only | inside (fails) | not measured | — |

Caches: `~/.npm`, `~/.cache/pip`, `~/.cache/pnpm`, a project's `node_modules`
and `.venv` are writable. uv's cache and pnpm's store are read-only by design:
installs inside the sandbox use the sandbox's own stores under
`~/.cache/jhon/sandbox` and copy files instead of linking them.

To check a row yourself (no write happens; `test -w` answers false on a
read-only folder):

    jhon sandbox run --project ~/Projects/work/<primary> -- sh -c 'test -w ~/Projects/work/<repo>/.git/index && echo writable || echo read-only'
    cd ~/Projects/work/<repo>; jhon sandbox profile --project ~/Projects/work/<primary> -- 'git push origin main'

For a guard row, pipe a PreToolUse payload (`{"tool_name":"Bash","tool_input":{"command":"git push origin main"},"cwd":"<member>",…}`)
into the hook command written in `~/.claude/agents/jhon-orchestrator.md`, with
`JHON_SANDBOX_PROJECT` set to the primary. This is how `tools/phasegate` times its hook rows.

## What to avoid and why

- **`sandbox.mode off` with super-yolo.** Nothing asks and nothing contains
  the command: one bad line can write to any repository and use your SSH and
  AWS credentials.
- **`sandbox.docker true`.** Docker lines already run outside the sandbox
  when they are on a line of their own. With the socket shown, any sandboxed
  command can start a container that writes anywhere and keeps running.
- **A broad `allow_write` folder** (`~/Projects`, `~/Projects/work`). It opens
  every repository below it, and since T281 the guards count all of it as
  the project too: an Edit or a removal anywhere below it is no longer
  asked (removing the folder itself or a repository's top right below it
  stays refused). A folder that holds Jhon's source checkout, its
  release or a PATH folder makes every sandboxed command refuse to run.
- **Listing a runner as a deploy tool** (`npx`, `node`, `bash`, `make`). Jhon
  refuses it, because any line would then run outside the sandbox.
- **Keeping the settings file or the state folder inside a writable folder**
  (`JHON_SETTINGS` under `/tmp` or a project). `jhon sandbox run` then runs
  nothing (exit 126).
- **Counting on Claude Code's Bash deny rules.** With the sandbox on, they are
  matched against the wrapped command and no longer apply.

## Known gaps (phase 2; not fixed here)

1. **Closed by T281: a mode reaches the listed members.** A member the
   primary lists, that joined and that has no mode of its own takes the
   primary's in a session of the workspace (`workspace.ModeOf`); the
   `jhon mode super-yolo --root <member>` commands are no longer needed. A
   member with its own mode keeps it. Still open: a repository only
   `allow_write` names, or a sibling the primary does not list, keeps its
   own mode, and asks.
2. **Said by T281: a joined sibling the primary does not list is no
   member.** It stays outside the working set (read-only in the sandbox,
   outside-write for an Edit), and now `jhon workspace list`, `jhon doctor`,
   the guards' outside-write ask and the sandbox's note say so, with the
   entry that lists it: `<name>: <path>` under `workspace:` in the primary's
   LDD config.
   Shown by `jhon sandbox run … test -w <repo>/.git/index` → read-only.
3. **The Edit and Write tools are not sandboxed.** Under super-yolo, an Edit
   in a repository declared nowhere passes with an outside-write note. Only
   Bash is contained.
4. **The sandbox's own package stores start empty** (on this machine, your own
   stores hold 1.8 GB for uv and 242 MB for pnpm). The first install inside the
   sandbox downloads again and copies files instead of linking them. How long
   that takes was not measured.
5. **cdk through `npx` stays inside the sandbox**, where `~/.aws` is hidden
   (`jhon sandbox profile -- 'npx cdk deploy'` → `work`). `cdk` and `sam` were
   not on PATH on this machine, so a deploy through them was not measured. No
   cloud call was made.
6. **No setting for execution deadlines.** You can only pass `--timeout` on
   each run.
7. **Not measured:** sessions that start inside a member, pi's own wrapping,
   and the first-start hook question in a newly listed repository.
