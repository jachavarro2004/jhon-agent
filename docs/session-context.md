# What a session starts with: lean or full

A Jhon session starts with one of two fixed texts (T146h).

- **Lean** is used when no change is active, or the active change is small.
  It is `prose/lean.md`, rendered for each runtime, and stays under 4 KB
  (about 1k tokens). It holds four things:
  - identity;
  - the limits that cannot be dropped: a refusal is a stop, credential
    files, remote work, observed success, and the owner's choices;
  - the small change's steps;
  - what to read when the work grows.

  The small change's steps are: `git switch -c <name>`; the test first;
  `jhon executions run` while it fails (RED); `jhon executions run` again
  once it passes (GREEN); then `git commit` and `jhon deliver check`, with
  no re-run. That is two runs exactly, with no unit's run between.
- **Full** is used when the active change is not small. It is today's
  orchestrator and always-on layer. The install writes it for each runtime
  as `~/.local/share/jhon/current/prose/full-claude-code.md` and
  `full-pi.md`.

Sub-agents keep the full always-on layer in every case.

## When the full context loads

- **`jhon status`, both runtimes.** For a change that is not small, the
  first note reads `not small: read <full file> before the next edit`. The
  file named is the one for the runtime the command runs in: pi when
  `PI_CODING_AGENT_DIR` is set, Claude Code when `CLAUDECODE` is set. With
  neither, the note names Claude Code's file and pi's beside it. A small
  change is never told this. Its next command is the bare
  `jhon executions run`.
- **Session start.**
  - Claude Code's main agent definition, `~/.claude/agents/jhon-orchestrator.md`,
    is the lean text. SessionStart hands the full file instead when the
    active change is not small. In print mode, where the definition is the
    system prompt, the full file follows as SessionStart context.
  - pi's `jhon hook session-brief` says `"context": "lean"` or `"full"`.
    The shell appends `~/.pi/jhon/jhon-lean.md` (jhon.json `lean`) or the
    orchestrator (`orchestrator`) to match.
- **pi, mid-session.** The full context arrives as one persisted
  `jhon-full` message, once per session. Two calls trigger it: a
  `jhon status` whose output carries the not-small line, and a `subagent`
  call. The system prompt is never swapped, so its cached prefix holds.
- **Claude Code, mid-session.** PostToolUse adds a pointer to the full
  file, once per session. It fires on the same two triggers, `jhon status`
  and an `Agent` launch. The full text exceeds the 10,000 characters a
  hook's context shows, so the pointer names the file instead.

## pi's tools on a lean session

Until the full context loads, the following tools are registered but
deferred behind `tools_more`:

- `jhon_review`, `subagent`, `subagent_continue` and `m365_read`;
- the background tools (`bash_background`, `monitor`, `task_*`);
- every Engram tool.

The skills list is deferred too; the `jhon-full` message carries it.

These stay available: `todo`, `ask_user`, `inspect` and codegraph. A lean
session gets no todo start rule and no start nudge, and the todo tool's
guideline says a small change keeps no list.

## Help

`jhon executions --help` and `jhon executions run --help` open with the
two-line common case: `jhon executions run` while the new test fails (RED),
then again once it passes (GREEN).
