# What a session starts with: lean or full

A Jhon session starts with one of two fixed texts (T146h).

- **Lean** is used when no change is active, or the active change is small.
  It is `prose/lean.md`, rendered for each runtime, and stays under 5 KB
  (about 1.1k tokens). It holds four things:
  - identity, with the reply language (below);
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

## The reply language

Both texts, on both runtimes, carry the same rule (T194): Jhon answers the
owner in the language of the owner's latest own natural-language message.
That covers everything addressed to the owner: replies, progress lines, todo
text, and each question with its header, options, descriptions and
recommended tag. A slash command, a short confirmation ("ok", "sí") or
pasted text does not change it. Harness notes, background notices,
sub-agent results, tool output, skills and summaries are not the owner and
never change it; a sub-agent's result is translated when the main session
relays it. Error text, refusals and quoted output stay literal when
relayed; only the explanation is translated. Artifacts (code, commits,
docs) stay in English, and that is not the reply language.

pi hands the shell's own messages to the model as user text, so each opens
with `[From the Jhon shell, not the owner; it does not set the reply
language]`: the `jhon-full` message, the task notices, the steers and
notices, the todo list block, and the route line. pi's `ask_user`
description says the same of its questions and options. `jhon audit` counts
a user message that opens with the mark (pi records the announce-stop
send-back and the stall continue as user messages) as the shell's, not as
an owner message or answer.

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
  `jhon-full` message, once per session. Three calls trigger it: a
  `jhon status` whose output carries the not-small line, whatever route
  read it (`bash`, `inspect`, or the jhon MCP server's `mcp__jhon__status`
  tool); a `subagent` call; and a call that may make or select a change
  when the engine then says it is not small (below). The system prompt is
  never swapped, so its cached prefix holds.
- **Claude Code, mid-session.** PostToolUse adds a pointer to the full
  file, once per session. It fires on the same three triggers: `jhon status`
  (through Bash or the jhon MCP server's status tool,
  `mcp__plugin_jhon_jhon__status`), an `Agent` launch, and a call that
  makes or selects a change that is not small. The full text exceeds the
  10,000 characters a hook's context shows, so the pointer names the file
  instead.
- **A change that appears mid-session, both runtimes.** A fresh
  repository starts lean: no change exists yet. Once the agent makes or
  selects one, the session switches before its next edit, with no
  `jhon status` needed. The moments are a write or edit under the layout's
  changes folder (`ldd/changes/` or `openspec/changes/`, such as a new
  `tasks.md`), and a bash command that switches branch
  (`git switch`, `git checkout`), runs `jhon executions` or `jhon risk`, or
  names the changes folder. Only at such a moment, and only while the
  session is still lean, the engine is asked as at the start: pi runs
  `jhon hook session-brief` and switches on `"context": "full"` (an
  answer slower than 3 seconds counts as unknown: the session stays lean); Claude
  Code's hook reads the same answer (the active change is not small). A
  small change, or no change, keeps the session lean. Other tools never
  ask.

## pi's tools on a lean session

Until the full context loads, the following tools are registered but
deferred behind `tools_more`:

- `jhon_review` and `m365_read`;
- every Engram tool.

The skills list is deferred too; the `jhon-full` message carries it.

The delegation tools are never deferred: `subagent`, `subagent_continue`,
`task_list`, `task_result`, `task_stop`, `bash_background` and `monitor`
are offered from the first request of every main session, lean or full, on
every provider. pi-claude-bridge reads the tool list only when a prompt
starts, so a tool held back at the start would stay out of reach for the
rest of that prompt.

`tools_more` asked for a tool the session already has answers
`Already enabled: <names>.`, and for a tool it enables
`Enabled: <names> (callable from the next step on).`. On claude-bridge a
tool enabled during a turn reaches the model only at the next prompt, so
both answers end with `On claude-bridge the tool list is read when a prompt
starts, so it is available but reaches the model only from the next prompt:
its absence from this prompt's tools does not make it unavailable.` (`they
are … them` for several), and the enabled one drops the next-step note.

These stay available too: `todo`, `ask_user`, `inspect` and codegraph. A lean
session gets no todo start rule and no start nudge, and the todo tool's
guideline says a small change keeps no list.

## Help

`jhon executions --help` and `jhon executions run --help` open with the
two-line common case: `jhon executions run` while the new test fails (RED),
then again once it passes (GREEN).
