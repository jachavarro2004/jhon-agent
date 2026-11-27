# tools/phasegate

The phase gate (T269). At the close of every phase it measures a jhon build on what the owner and the agent feel
(speed, friction in real use, the fixed texts' budgets, the suite's leftovers, the correctness results) and
holds the new numbers to the previous phase's, failing on any regression past its tolerance. It reads the
owner's projects and transcripts and writes only its own record.

```sh
go run ./tools/phasegate measure --label <name> --jhon <binary> [--projects-file <file>] [--sessions-since <YYYY-MM-DD>] [--out <dir>] \
    [--pi-sessions <dir>] [--claude-projects <dir>] [--repo <dir>] [--jhon-root <dir>] [--suite-tmp <dir>] [--result <name>=pass|fail]... \
    [--against <old binary> --against-label <name>]
go run ./tools/phasegate compare --base <label> --new <label> [--out <dir>] [--markdown]
```

**Back to back.** A record taken at another moment carries that moment's machine load into its times. With
`--against <old binary> --against-label <name>` (and `--projects-file`), `measure` times the previous phase's
binary and the new one in the same run: in each project, each run of each command is the old binary's call, then
the new one's (old, new, old, new…), still one call at a time. It writes `<out>/<label>.json` for `--jhon` and
`<out>/<against-label>.json` for `--against`, each naming the other in `pair`, so
`compare --base <against-label> --new <label>` holds two sets of times that met the same load, and says
`phasegate: base and new were timed back to back, in one run`. The old binary reads its own tree (found from the
binary, else `--repo`; `--jhon-root` is the new one's) and is skipped alone for a command it lacks. Friction,
budgets, hygiene and results do not depend on the binary: they are measured once, in the new record. The objects
are counted around each project's interleaved calls, so a growth is in both records. Without `--against`, measure
times one binary as before.

`measure` writes `<out>/<label>.json` (`--out` defaults to `$XDG_STATE_HOME/jhon-phasegate`, else
`~/.local/state/jhon-phasegate`), prints a summary, and exits 1 when a project's objects grew, 2 when it cannot
run, 0 otherwise. `compare` prints `measure, base, new, change, verdict` per measure (plain text, or a Markdown
table with `--markdown`), then `phasegate: <n> worse`, and exits 1 when any measure is worse, 2 when a record
cannot be read, 0 otherwise. Run it from the owner's terminal, outside Jhon's sandbox.

- **Speed.** For each project in `--projects-file` (one absolute path per line, `#` comments; the owner's file,
  never tracked), each command runs 5 times and its median and maximum are recorded in milliseconds:
  `jhon status --json`; `jhon risk`; `jhon hook pre-tool` with three Claude Code payloads (a `git status` Bash
  call, an Edit of the first tracked file, a `gh pr create --fill` Bash call); and `jhon sandbox run -- true` when
  `jhon sandbox status --json` says it can run. The hook call is the main session's as the Claude Code render
  writes it: `--agent jhon-orchestrator` with safety-guard, vcs-guard (the vocabulary's VCS directories),
  framework-guard (the install's paths), secrets-guard and route-guard (the bounded writers), read from the
  binary's tree (`--jhon-root`, found from the binary, else `--repo`). Each call gets the session's environment
  as `jhon start` makes it (`JHON_SANDBOX_PROJECT` pinned to the repository's top, `CLAUDE_PROJECT_DIR`) and
  `JHON_TELEMETRY=off`. One process at a time, each under `nice -n 10` with a 30 s timeout (a timed-out run counts
  30 s and a timeout). A command the binary does not have is skipped and recorded. These commands are read-only
  by contract: the files under `<project>/.git/objects` and under the common dir's objects are counted before and
  after each project, and growth fails it.
- **Friction.** Every transcript under `--pi-sessions` (`~/.pi/jhon/sessions`) and `--claude-projects`
  (`~/.claude/projects`), sub-agents' included, changed since `--sessions-since` (default: all), and each tool
  result in it from that date. A stop is a result with a guard's name and a colon (`safety-guard:`, route,
  inspect, vcs, framework, secrets, scope) in its first 90 characters (600 when it begins with the older Claude
  Code prefix `PreToolUse:<Tool> hook error: [<hook command>]:`), or the shell's `did not answer in <n>s; the
  call is refused` in its first 90 (a hook timeout). Left out and counted apart: projects whose folder name holds
  `jhon` (Jhon's own development), scratch folders (`/tmp`, `/var/tmp`, …), and bridge mirrors, the Claude
  Code transcripts whose first entry with an `entrypoint` says `sdk-ts` (the Claude Agent SDK's copy of a pi
  session on pi-claude-bridge, which would count it twice). Each tool call counts once, by its id (pi's
  `toolCallId`, Claude Code's `tool_use_id`): pi's transcripts are read first, and a copy of a counted call is
  left out apart, as `bridge_copy` when it copies a pi result (the SDK's transcripts whose first entrypoint
  says `cli`, which the entrypoint rule misses) and as `repeated` otherwise (a resumed Claude Code session copies
  the results of the one it resumes); a file of copies only is a session of that key. A command the sandbox
  stopped is a stop of the guard `sandbox`: a line of its own saying `the sandbox refused a write to …` (its
  note), a line where a program says `Read-only file system` after `: ` or `] ` with no backquote before it (not
  the sandbox status's `cannot start a sandbox`), the shell's `jhon: sandbox.mode is on and …` or
  ``jhon: `jhon sandbox status --json` gave no answer`` opening the result, or a line of `jhon sandbox: ` that
  says `so nothing was run` or that docker cannot reach its daemon. Each stop gets one class by its wording
  after the guard's name: credentials match, inspect shell syntax, inspect allowlist, VCS directory, unresolved
  target, unbounded wait, review due, no record, slice or delivery discipline, scope surfaces, unreadable or
  unknown, light verify, workspace members' PRs, before the last slice, merge method, owner's grant, outside the
  project, hard line, framework path, hook timeout; the sandbox's by its note's reason: allow_write opens it, no
  setting opens it, refused write (a note with no reason), read-only, no note, mode on, cannot run, did not start,
  docker hidden; other. Recorded: the counting rule (`counting`, 2; a record without it was counted by rule 1,
  which counted the copies), stops per 100 tool results in total, per runtime, per ISO week, per guard and per
  class.
- **Budgets.** `go test -run '^(TestFixedContextBytes|TestLeanTextFitsBothRuntimes)$' -v ./engine/internal/render`
  in `--repo` (under `nice -n 19`): each runtime's fixed context and lean text from the tests' logs, each limit
  from their source (fix/t145b's bytes times the test's percentage; `leanLimit`).
- **Hygiene.** The entries left in `--suite-tmp`, the TMPDIR a suite run was given. The tool runs no suite.
- **Correctness.** `--result <name>=pass|fail`, repeated (`go-suite=pass ci-main=pass doctor=pass`), stored as given.

The record holds counts only: no command text, no path, no project name (a project is `p<n>`, its line in the
projects file, so keep the file's order between phases).

**Tolerances** (`compare.go`): a median time is worse when it grows by more than 25% and by more than 100 ms; a
hook median over 1000 ms or a status median over 3000 ms is worse even with no base; stops per 100 tool results
is worse when it grows by more than 10% (per class and per runtime are shown, not gated); a budget is worse when its headroom is
below 0; hygiene is worse when the leftovers grow; a result is worse when it goes from pass to fail, or fails
with no base result (a fail in both is shown as `still fails`). A project whose objects grew is worse. Friction
counted by two rules is not held to the base: its rows say `not comparable`, and a line under the table says
`phasegate: friction counted by rule <b> in base and rule <n> in new: its rows are not comparable`.
