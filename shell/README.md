# shell/

`jhon-shell`, Jhon's pi package. pi is the runtime; the framework is the
`jhon` binary and the prose it renders. The shell is the code that runs
inside pi, kept apart from the framework the way a client is kept apart from
its server: it talks to Jhon only through the binary, as `CONTRACT.md`
states, and never reads `prose/`.

pi is installed only while `runtimes.pi` is on in the owner's settings (on
by default; `jhon sync` skips it otherwise and leaves `~/.pi/jhon` as it is).
The provider pi runs on is the active profile's: `claude-bridge` by default,
any pi provider a profile's pi models name otherwise (`anthropic/…` with an
API key, `openai/…`, a local one). `pi-claude-bridge` is installed for a
profile on the bridge, and also whenever Claude Code is one of the install's
runtimes: a profile that names no bridge model then loads it as a provider
only (its `AskClaude` tool off), so the session's tools and prompt are the
ones it has without the bridge. `/jhon:profile` shows each profile's
provider; the models it offers for an agent are every model pi's registry
holds in this session and every model a profile names; on a save, a model the
session's registry does not list as available is said so: it runs once pi
lists it, with its provider loaded and a credential for it. Loaded, the
bridge sets `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` in pi's process (as
it always did for a profile on the bridge), and a project's own
`.pi/claude-bridge.json` can turn `AskClaude` back on.

A session never calls a model nobody chose (`lib/model-choice.ts`). The shell
judges with what it knows for certain at session start and in the process,
and never reads a session's history. A session's model is chosen when it is
a named default (the agent folder's, which is the profile's, or the one the
project's `.pi/settings.json` names), when the command line names `--model`
or `--provider`, when models are scoped (a `--models` pattern matched), or
when the owner said yes to the question below or picked a model in this
process (the pick is put back on a later `/new` of the same process).
Otherwise, when a named default cannot run (its provider has no credential,
or pi has no such model), pi would start on its first available model
without a word, and the shell names both models instead; when the project's
and the profile's defaults both cannot run the line names both, and it never
names one that can. When no named default is broken, nothing is said or
held, whatever model the session is on.

Without a UI (`-p`, `--mode json`) the run ends there with exit 1 and no
model call, and the line says to pass `--model <provider/id>`. With one, the
line is an error at session start (a `/new` that repeats it is quiet) and
the session is held: the owner's next prompt asks whether to run this
session on that model. Yes makes it their choice for this process and lets
that prompt through; no, or a dismissed dialog, refuses that prompt and the
next one asks again. One question is open at a time: a prompt that arrives
meanwhile, a run another extension starts (aborted before its first
request), a compaction and a tree summary are refused or cancelled with the
line. In RPC mode the question is pi's `select` dialog request to the
client. Picking another model also lifts the hold.

Deliberately so: a resumed session on a model the owner picked in an earlier
process, while a named default cannot run, is asked once per process with a
UI, and without one ends with the line and needs `--model`.

A sub-agent runs on an explicit `--model`, which pi never replaces: with no
credential it fails before any call, and its parent's result names the
provider pi found no credential for and the model the agent was launched on.
Not covered: an extension that calls a model itself through pi's registry.

| Path | Holds |
|---|---|
| `package.json` | The pi package: its `pi` manifest names the one extension pi loads. |
| `extensions/jhon.ts` | The entry point: the orchestrator's prompt in the main session, with the project memory it shares with Claude Code (`lib/memory.ts`), both kept byte-identical for the whole session so the provider's prompt cache holds on every turn (`lib/session-prompt.ts`: the prompt of the session's first run, the memory index read once, each skill's catalog description cut to 200 characters; the brief's route line, which follows the diff, goes out as a `jhon-route` message when it changes), the tool list decided once per session (`lib/tools-deferred.ts`: the main session declares Engram's `mem_search`, `mem_save`, `mem_get_observation`, `mem_context`, `mem_judge`, `mem_judge_many` and `mem_session_summary`; Engram's other tools and the web tools are deferred, and `tools_more` lists them and enables a named one for the rest of the session; codegraph's tool only where the repository is indexed), the `m365_read` tool that reads Microsoft 365 through a headless Claude Code limited to its read tools (`lib/m365.ts`), the guards on every tool call (in the main session and in each sub-agent), `/jhon:status [<change>]` (the LDD detail), `/jhon:grants` (the grants; the permission mode is Customize's), `/jhon:profile`, and `/jhon:dashboard` (this session's audit in the browser). |
| `lib/guards.ts` | Turns a pi call into a guard payload and runs `jhon hook <guard>` (about 50 ms, so it runs in line); a guard that does not answer in 10 s refuses the call, and `jhon contract` gets 5 s. The `inspect` tool and the formatter after an edit run without blocking the TUI; `inspect` starts its program as `lib/sandbox.ts` says (in the sandbox with `sandbox.mode` on). |
| `lib/sandbox.ts` | T128b: the OS sandbox around the commands the model chooses. At session start it asks `jhon sandbox status --json` once and keeps the answer; with `sandbox.mode` on, `bash`, `bash_background` and `monitor` run as `jhon sandbox run --project <project> --profile work -- <shell> -c <command>` (the command one argument, the project pinned for the session), after the guards judged the original command, and `inspect` starts the program the model names as `jhon sandbox run --project <project> --profile work -- <program> <args>`, after the inspect guard. `bash` is then pi's own definition registered again with only its operations replaced, so the model reads the same tool and prompt lines. Where the sandbox cannot run, or no project can be pinned, the call is refused with the reason and the fix, `inspect` with the same line. With the mode off nothing is registered or wrapped, and `inspect` starts its program directly. The owner's `!` commands and own jobs are never wrapped. One pin per session: a session that pinned a project exports it as `JHON_SANDBOX_PROJECT`, which its guards and its children read; a sub-agent, a child pi in a folder the model may choose (the `subagent` tool's `cwd`), asks `jhon sandbox status --json --project <its parent's project>` and runs its commands in that project's sandbox, and is refused where that folder is gone. A session start that ends with no pin (no answer, the mode off, a sandbox that cannot run, no repository) takes `JHON_SANDBOX_PROJECT` out of the environment, so no child inherits an earlier session's project. |
| `lib/agents.ts` | The `subagent` tool: runs `agents/<name>.md` as a child pi process with its model and tools, without pi's skills catalog: it loads only the skills its definition names by a `skills/<name>/` path among the ones the main session resolved (worker, verify and explore get the LDD skill), plus the exact SKILL.md paths the call passes in `skills`, and passes a bounded writer's task so its scope is enforced. In the main session every launch runs in the background and returns its task id at once; a request for the foreground (`background: false`, any `mode` but `background`) still runs in the background and the result says so, adding that the completion is pushed automatically. A sub-agent's own sub-agents run in the foreground: there the call shows the child's elapsed time and latest activity as it works; `timeout_seconds` stops it and fails the call. `subagent_continue` runs a saved one again on its own session, in its cwd, with its launch scope, unless a continue message carries its own `## Allowed edit surfaces` heading: that list is the scope from then on (the last message that carries the heading wins, T133), and only the launching side's messages set it, never what the child wrote or read. No agent has a turn cap (T131): a run ends when the agent returns, at its `timeout_seconds`, or when it is cancelled; a `maxTurns` line an older install left in an agent file is ignored. T134: a launch (not a continue) ends the child's first message with what `lib/launch-context.ts` gathers at once: `## Decisions on record`, the decisions of `jhon context --phase none --handoff --json` in the child's folder (on the change STATUS shows when it is that folder's), `- #<id> (<type>) <title>`, at most 12 lines, a 2 s deadline; and `## Code map`, what `codegraph query <name> --json` gives for up to 8 of the files that exist in the child's folder and the backticked identifiers the brief names (`- <kind> <name> <path>:<line> <signature>`, with `; callers: …` from `codegraph callers` for the first three symbols), at most 4 KB and 3 s in all, only where the child's folder is indexed, or, in a linked worktree with no index, the main checkout is (`codegraphIndexRoot`, results kept only for files the worktree holds). A failure, a timeout or an empty result leaves its heading out and the launch goes on, so a session with neither Engram nor CodeGraph launches the brief alone. The scope a bounded writer's guard reads is the brief as written, and a continued child's scope is its first message without these blocks. |
| `lib/background-tasks.ts` | The main session's background work in one registry: commands and monitors (`bash_background`, `monitor`) and sub-agents (every main-session `subagent` call). Each task has an id, belongs to its session, and settles once when it ends on its own; a stop settles nothing. At most 25 run at once. |
| `lib/background-tasks-tools.ts` | The model's side of it: `bash_background`, `monitor`, and `task_list`, `task_result`, and `task_stop`, which take any task by id, plus `/jhon:agents` and `/jhon:jobs`. A task's end reaches the agent as a follow-up message that starts a turn when it is idle; one that lands while a turn runs waits in the shell and goes out once when the turn settles (with the next message after an abort), since Esc clears pi's own queue; a task the agent stopped sends nothing, one the owner stopped is reported. A headless run (`pi -p`, `--mode json`), which pi ends at its settle and never wakes again, waits at `agent_before_settle` while its session's background sub-agents, commands or monitors run (never the owner's commands), sends each notice as a follow-up that pi runs as a new turn before settling, and repeats until nothing is pending; one wait is capped at `JHON_HEADLESS_WAIT_SECONDS` (an hour by default), past which a notice names the tasks whose results are lost (on stderr too) and the run ends. Background stays the rule there too (L741). The bar counts the running tasks. A foreground bash that only waits (a sleep over 5 s, `gh run watch`, `wait`, `tail -f`, `timeout` over 60 s, or a standalone sleep while a task runs) runs, and its result carries one note pointing to bash_background or monitor (T155; refused before). Jhon's guards judge their commands as bash; a sub-agent gets none of it, and cannot start a background sub-agent. extensions/jhon.ts registers it. |
| `lib/background-tasks-view.ts` | `/jhon:agents` and `/jhon:jobs`: the sub-agents, or the commands and monitors, running first, beside the selected one's thread: a command's line, output file and output tail; a sub-agent's task, its thread (thinking, text, each tool with its status and output, from the child's JSON events), and answer. `s` stops the selected one. |
| `lib/review-tool.ts` | T118c: the `jhon_review` tool, the main session's only: the native Prism review in one call. `jhon review start` freezes once; each facet prompt runs as one tool-less completion through pi's model registry (`find`, then `streamSimple`: one user message, no tools, a fresh session id, the effort as reasoning, a time bound that grows with the prompt, at most one retry), all facets at once; then `refute` → the challenger, `validate` → the arbiter (skipped when nothing is left to judge), `record` writes review-prism.md. Every completion runs on exactly the model and effort the engine names for its routing key; a role with none, or one pi cannot resolve, falls back: the tool says which review agents to launch. A facet that fails twice or answers malformed is `unavailable` in the record. The model gets the verdict, the counts and the correction set (at most 2,000 characters), never the prompts. |
| `lib/events.ts` | The pi.events channel the background tasks share with the UI: the running tasks. |
| `lib/ui-use.ts` | Counts the shell's own UI use: every command and shortcut an extension registers through `counted(pi)` appends a `jhon-ui-use` session entry naming it (`/jhon:agents`, `key alt+s`), and so does a click the shell handles that opens a view (`click agents`, `click jobs`), outside the model's context; `jhon audit --all` counts them (`ui_use`). shell/CONTRACT.md, "What the shell records of its own use". |
| `lib/ask.ts` | The `ask_user` tool, Jhon's AskUserQuestion: 1 to 4 questions, each with 2 to 4 described options (or none for a free answer), multiple choice, and a "Write my own answer." row for a typed one, asked one after another in the question view `lib/ui/ask-view.ts` (pi's select dialog where the UI has no `ui.custom`). The view sits in the dock between two accent rules, never an overlay; keys and clicks reach it. |
| `lib/context-backstop.ts` | T115e: a delegation signal for the main session, never a child. Once its context passes half of `shell.behaviour.compact_tokens` (100000 tokens; `compact_tokens = 0` turns it off) or 15% of the model's window, whichever comes first, the orchestrator hears once to delegate its next unit that needs several steps (its reads, edits and checks) with a short handoff, and keep single small edits and state checks inline. It reads the context usage at most once per 5 s, re-arms after a compaction drops the context below it, and reaches the agent appended to a tool result or as a persisted message after a reply, never in the system prompt. The ladder: this backstop, then the fresh-session notice, then compaction; two on one reply are one notice. |
| `lib/fresh-session.ts` | One session per change: after a reply that leaves the main session's context past ten points of the window before `shell.behaviour.compact_at` (50%) or three quarters of `shell.behaviour.compact_tokens` (150000 tokens), whichever comes first, the owner (a notice) and the agent (a message on its next turn) hear once to save a handoff and start a fresh session; a `jhon archive` brings the same notice once per change. Never a block, never in a child. |
| `lib/desktop-palette.ts` | With `integrations.theme.palette = "desktop"` (`"ryoku"`, its older name, too), where a desktop palette file stands (`JHON_DESKTOP_COLORS`), draws the rashin poster as pi's header; off, the default, nothing reads it. |
| `themes/` | The pi themes Customize → Appearance's Theme row lists after rose ink ("Adding a theme"); none today. |
| `fixtures/kanagawa.json` | The former Kanagawa theme, kept as the reference that declares every color pi's theme schema requires (`lib/desktop-palette.ts`); not registered with pi, so the Theme row does not list it. |
| `lib/rose-ink.ts` | With `integrations.theme.palette = "desktop"`, asks `jhon palette --write-pi` for the rose ink theme now and on every desktop palette change, while rose ink is pi's active theme; and, whatever the palette, when Customize switches back to rose ink, before applying it. |
| `extensions/ui.ts` | The shell UI: the status bar and header, the prompt editor (double-Esc cancel, animations; after 60 s with no stream event and no tool running its working line reads `waiting for Claude API · <N>s`; after 75 s the main session aborts the turn as Esc does and sends one continue, once per owner message, never while a tool or an owner prompt is open; `shell.behaviour.stall_seconds` sets the seconds and `0` turns all recovery off; on the `claude-bridge` provider or while a reasoning model thinks above minimal, where minutes can pass with no stream event, the threshold is `shell.behaviour.thinking_stall_seconds` instead, 300 s, `0` never recovering there), the right pane (`lib/ui/pane.ts`), and Customize. |
| `extensions/pretty.ts` | pi-pretty (syntax-highlighted reads, diffs, file trees) under the UI: the render lists `@heyhuynhgiabuu/pi-pretty` pinned with nothing loaded, so pi installs it into `<agent dir>/npm`, and this loads it from there, never in a sub-agent, keeping the prompt editor the UI's and its FFF store in the agent dir. |
| `extensions/todo.ts` | The `todo` tool the model keeps its task list with, drawn in the right pane's TODOS section while no LDD change is active, restored from the session. With no list, the model is told when to start one, and three tool-use turns that change something without a list bring one nudge per owner message. The list block and the start rule never sit in the system prompt: each goes out as a persisted `jhon-todo-list` message when it changed, and the nudges (`jhon-todo-start`, `jhon-todo-reminder`) as persisted steer messages after a tool-use turn, so the prompt cache holds (T115d). |
| `extensions/quiet.ts` | The tool cards (design 01, T174-S6; drawn by `lib/ui/tool-card.ts`): `read`, `grep`, `find`, `ls`, `edit`, `write` and `bash` as cards with bounded previews, executing as pi's own tools (`bash` in the OS sandbox when `sandbox.mode` is on, `lib/sandbox.ts`, drawn the same); a call a Jhon guard refused is a red band; every other tool's call gets its family icon (`lib/ui/tool-icons.ts`). Always Jhon's cards (T15.3: `quiet_tools` is gone). |
| `lib/ui/` | The UI's modules, one per piece: bars, prompt, the right pane (`pane.ts`) and its sections (`panel.ts`), Customize and `primitives/` (cards, tinted blocks, rules, glyphs). `ldd-status.ts` reads the session's LDD change and the other worktrees' through `jhon status --json`; their keys (`t` tests, `o` tasks.md) live in the LDD detail (alt+j). Under a multiplexer the pane shows it too: the multiplexer shows nothing of LDD (L112). |
| `lib/profiles.ts` | The owner's active Jhon profile, shown in the header poster and the bar, and a profile's delegation mode (`delegation` in profiles.json: `writer`, else `inline`). |
| `lib/session-profile.ts` | T120: a profile used in one session only, apart from the default for new sessions. `u` in the profiles panel appends the session entry `jhon-shell.session-profile/v1` (`{ profile }`) and writes nothing global: no `jhon profile use`, and settings' `pi.profile`, profiles.json's `active`, the agent folder's `settings.json` and the rendered agent files stay as they are, so the other sessions are untouched. From then on the session's sub-agents run on that profile's model and thinking level for each agent (read from `jhon profile json` at each launch, in place of the rendered file's `model:` and `thinking:`), its orchestrator switches to the profile's model and level through `pi.setModel` and `pi.setThinkingLevel` with a notice (pi saves neither as a default), its delegation mode follows the profile, and the header, the status bar and the panel name it, marked while it is not the default (`<name> (session)`; `●` on its tab). The entry is read from the session's branch at each start: a resumed session comes back with its profile, `/new` starts on the default. A sub-agent hears its parent session's profile in `JHON_PI_SESSION_PROFILE`, so the sub-agents it launches follow it. `u` is refused, with one line and nothing changed, when this pi does not list one of the profile's models as available (its provider is not loaded in this process, or has no credential): `d` and a restart load it, as the line says; the panel wraps a note wider than the card onto up to three rows. A session profile that was renamed or deleted puts the session back on the default with a one-line notice. `d` is unchanged: `jhon profile use`, the default every session without a profile of its own reads. Claude Code sessions (no panel; their agent files are global) and `jhon review-native` keep reading the default profile. |
| `lib/delegation-mode.ts` | T117c: the active profile's delegation mode, read once per session with the prompt's frozen parts, so the prompt stays byte-identical. In `writer` mode the main session's prompt carries `jhon-orchestrator-writer.md` (rendered beside the orchestrator's file) in place of the orchestrator's region between `<!-- delegation:begin -->` and `<!-- delegation:end -->`; a missing file, a missing marker or markers out of order keep the organic rules whole, said once. A child never gets it. The profiles panel's actions (`JhonPanelDeps`): `setMode` (`w` then `s`) runs `jhon profile mode <profile> <inline\|writer>` (from the next session); `useNow` (`u`) makes a profile this session's only (T120, `lib/session-profile.ts`) and, when this session's mode changes, rebuilds the prompt once with the notice "The orchestrator's rules switch to writer mode from the next turn." (one notice with the orchestrator's model switch before it when `u` changes both: pi draws only the last of two notices sent back to back); `makeDefault` (`d`) and `use` activate it without the rebuild (the engine has one `active` profile, which is also the default for new sessions). |
| `lib/writer-guard.ts` | T117c, T117e: in `writer` mode the main session's `edit` and `write` calls change at most 20 lines per owner request, summed over its calls since the owner's last message (pi's `input` event from the owner; the extensions' messages, such as notices, sub-agent completions and follow-ups, do not reset it). A call counts when it is let through and is taken back when it fails or another guard refuses it; an edit counts its replacements, a write its new content against the file's current one, their common head and tail left out. The call that would pass 20 is refused with the total so far and a reason that points to a worker with a self-contained brief. Files under `odd/` (the plan doc, the Log) always pass and are not counted; `inline` mode and children are not limited. It reads the same session-frozen mode as the prompt. |
| `lib/compaction.ts` | T121: the main session's own compaction at `compact_at` or `compact_tokens`, one at a time: after a reply; in the `input` event before an idle prompt when the session is already over budget (the prompt waits and runs on the compacted context); and at a `turn_end` with tool results over budget, where `ctx.compact()` stops the run and a `jhon-compaction-continue` message with `triggerTurn` continues it once the compaction ends or fails. A headless run is not interrupted mid-run; a child never compacts here. |
| `lib/child-budget.ts` | T122: a sub-agent child's own ladder (the main session's backstop, fresh-session notice and compaction never reach a child). Past `shell.behaviour.child_backstop_tokens` (80000 tokens; 0 off) or 40% of the window, whichever comes first, the child hears once per crossing to wrap up and return path:line evidence and decisions, not raw output. No cap: a child's tool calls are never refused for its context (T155). An explore child that may call CodeGraph hears once, past 12 lookups or 100,000 bytes since its last CodeGraph call, to prefer `codegraph_explore`. T125: a worker-type child (`JHON_PI_AGENT` `worker`, or a name ending `-worker`) that may call CodeGraph gets the same count with a worker's reminder: CodeGraph returns a symbol's source already read, read only the lines you change, and prefer the read tool with offset/limit over cat/sed through bash. Lookups are read, grep, find, ls, inspect, and a bash command that only reads (`readOnlyBash`: cat, sed -n, grep, ls, head…, behind one leading `cd <dir> &&` too, T134), with their output bytes; a bash that writes or runs something is not counted; a CodeGraph call re-arms the count. All of it rides the tool result, so the child's system prompt stays byte-stable. |
| `lib/announce-stop.ts` | T135: the main session only. A run whose final reply announces an action of the agent's own, or holds no text (L1063), and makes no tool call is sent back once per owner turn as one follow-up user message, with the reason `jhon hook announce-stop` gives (the rule Claude Code's Stop hook runs); never after an abort or an error, in a child, with `agents.act_on_announce` off, or when the check fails or takes over 5 s. |
| `tsconfig.json` | Strict type checking (`npm run check`, `tsc -p shell`), run by `jhon test` and by the pre-commit gate when a commit touches the shell's TypeScript. |
| `test/` | Its tests: `node --experimental-strip-types --test shell/test/*.test.ts`, also run by `jhon test`, which first runs `npm ci` here when `node_modules/` is missing (the tests import pi's packages, this package's only devDependencies; at run time pi provides them). |

## How it is loaded

`jhon sync` ships the shell in the release, and the pi render
(`engine/internal/render/pi.go`) lists `<release>/shell` among the packages
in `~/.pi/jhon/settings.json`, so pi loads it from that path.
`jhon start pi` runs pi on `~/.pi/jhon`.

## The UI

| Command | Shortcut | Does |
|---|---|---|
| `/jhon:status [<change>]` | `alt+j` | A change's LDD detail, near full screen: the route, its risk and why, each slice and what it delivers, the open slice's tasks, the record, the last run and the notes. `t` runs its tests in the background (`jhon executions run <change> --affected <base>`, the owner's run), `o` opens its tasks.md, `r` reads it again, `↑/↓` scroll, `esc` closes. The argument opens a named change (L214) and completes to the project's changes. The audit and the memory are the dashboard's. |
| `/jhon-ldd [<action>] [<change>]` | | An LDD action on a change, as in Claude Code: the agent gets the prompt the release's `jhon-ldd` skill holds, the arguments in place, and the command waits for the turn to end. The argument completes to the actions (`new`, `ff`, `continue`, `status`, `explore`, `research`, `apply`, `verify`, `archive`, `init`), then to the project's changes. |
| `/jhon:grants` | | The project's grants (the permission mode is Customize → Permissions', L214): every live grant with its class, project and time left, then each class (push, gh-write, publish, remote, deploy, outside-write, destroy) with its state here. `enter` grants the chosen class for the chosen duration (`d`), `x` revokes it, each after a confirming key; `r` reads them again, `esc` closes. |
| `/jhon:profile` | | The model profiles near full screen, the chosen one's model and effort per agent: `←/→` pick a profile, `↑/↓` an agent, `enter` its model, `e` its effort, `w` the delegation mode, `s` saves, `n` creates a copy, `x` deletes; `u` uses the profile in this session only (T120, `lib/session-profile.ts`), `d` makes it the default for new sessions. `/jhon:profile list \| show [<name>] \| use <name>` runs the CLI (`use` sets the default). |
| `/jhon:customize [<category>]` | | The shell's settings by category, each setting one line with its values and a preview of the highlighted one. Appearance opens with the Theme row and ends with Reset (every `shell.appearance.*` key back to its default after a second `enter`). Behaviour holds the timings and budgets. Keys holds each shortcut: a key pi already uses in the editor, the app or the fullscreen transcript is refused, naming pi's action (`ctrl+shift+up is pi's key for "Jump to the previous marked message" (tui.altScreen.previousPrompt); pick another key.`), and nothing is written; Reload now reloads pi so a changed key applies. Permissions holds the project's mode (normal, yolo, super yolo): `←/→` hold a choice, `enter` sets it with `jhon mode <mode> --root <root>`, the only place the mode changes (L214), super yolo only after your Yes. `↑/↓` or `j/k` move, `→`, `Tab`, `Enter` or `Space` open a category, `←/→` change a setting and apply it at once, `Enter` edits a free value, `Esc` closes; a click picks a category, a setting or a value. A refused value is a notice (`Customize: <why>`). Runtimes and integrations are `jhon setup`'s; `pi.profile` is `/jhon:profile`'s. |
| `/jhon:dashboard` | | This session's audit in the browser at `http://127.0.0.1:7480/#/s/<session id>`, starting `jhon dashboard` detached when it does not answer, or replacing one that runs an older build than this shell's `jhon` (no `/api/version`, or another binary; stopped only when it is proven Jhon's). One that is not replaced is opened all the same, and the notice says to run `jhon dashboard --port <p>` to see why. With no display it says the address instead. |
| `/jhon:agents` | `alt+s` | The sub-agents, full screen (T92, L657): this session's, or every session's this pi ran after `a`, running first, beside the selected one's live thread (its model, tokens, cost and time, its task, its thinking, text and tool events, then its answer). `j/k` select, `ctrl+j/k` scroll, `f` follows, `s` stops the selected agent, `o` opens its saved session in `$VISUAL`/`$EDITOR`, `F` (or `tab` when narrow) shows the thread alone, `esc` goes back, `q` closes; a click selects a row or presses a button. The work row opens it. |
| `/jhon:jobs` | `alt+o` | The background commands and monitors (T92): the list beside the selected job's command line, output file and tail, with the agents view's keys but `a` and `o`. |
| `/jhon:usage` | `alt+u` | Each provider's subscription usage (T174-S5b, `lib/ui/usage.ts`): the session's provider first, marked `力`, then each one the sub-agents' models name, each with its plan, its windows (name, meter, used share, reset) and a warning from 80%; a provider without data says why. Claude (anthropic, claude-bridge) and ChatGPT (openai-codex) send theirs with each reply; NaN Cloud (`nan`) is fetched from its quota endpoint with the key pi holds for it (redirects refused, no stored copy), one allowance per model: the bar follows the session's model's own, else its family's total, else the account's (`glm total`, `nan total`: shares of the summed allowances), and the overlay lists a family together, the most used first; a provider another extension registers (`jhon-shell:usage-source/v1` on pi.events) is fetched the same way: at a session's start, at most every 5 minutes after each turn, and on `r`, each fetch bounded by 10 s (`JHON_SHELL_USAGE_TIMEOUT_MS`). A session on a profile of its own (T149) lists the providers of that profile's sub-agent models. `r` refreshes, `esc` or `q` closes; the pointer paints a key hint and a click on it does what its key does. |
| | `alt+t` | Folds or unfolds the agent's todo list; `alt+PgUp/PgDn` scroll it three rows. |

`/jhon:stats` and the rail's STATS went (L117, T169), and T174 removed
`/jhon:changes`.

Each key is set or turned off (`off`) in `[shell.keys]`: `panel` (alt+j),
`agents` (alt+s), `jobs` (alt+o), `usage` (alt+u) and `todos` (alt+t);
each has its variable (`JHON_SHELL_JHON_KEY`, `JHON_SHELL_TASKS_KEY`,
`JHON_SHELL_JOBS_KEY`, `JHON_SHELL_USAGE_KEY`, `JHON_SHELL_TODO_KEY`).
Customize → Keys has a row for each, and each refuses a key pi uses.
`keys.tasks` reads as `keys.agents`; `keys.changes` is retired with a notice
(read until v0.4); `jhon settings migrate` moves or drops them after a backup.
`alt+a` stays the prompt's select-all alias, so the Agents view takes `alt+s`;
the Jobs view takes `alt+o`, which pi binds nowhere.

The chrome wears Jhon's brand (`力`, rose ink) and has only Jhon's options
(L146). In a fullscreen terminal the top row holds the project and its path
and nothing else (` 力 <project>  <~path>`; the project alone when a
multiplexer host that shows the path is announced, `jhon:host`), and the
status bar sits at the bottom: on the left `⎇ <branch>` with `±N`, the files
the session's own writes changed (`lib/ui/session-changes.ts`), the sandbox's real
state (`sandbox on`, `sandbox off`, or `sandbox not running · <why>` when
the mode is on but the sandbox cannot run, no project can be pinned or the
status gave no answer: what `lib/sandbox.ts` read at the session's start)
and the permission mode when it is not normal
(`YOLO` in amber, `SUPER YOLO` in red and bold), then what other extensions
report on pi's status line (an MCP adapter's servers), in their own words and
the bar's palette; on the right
`model · effort · profile`, `ctx` with its 70/90 meter, the percent and
`· compacts at N%` near it, the subscription's windows (T174-S5b): the week
with its meter, its percent and `· resets Thu 14:00`, and `5h N%` once the
5-hour window reaches 70% (the reset then on the more urgent of the two),
and the cost, thin `│` dividers between. Running agents and jobs are not on
the bar. Under 140 columns the status bar takes its narrow form: no meter,
the branch cut to 18 cells. Narrowing further, the branch shortens first (to
8 cells); then why the sandbox is not running, the other extensions'
statuses, the profile, the reset, the week's meter (the profile before the
windows' meters and resets, as before T174), the context meter, the
compaction note, the window without the reset, the windows, model · effort,
sandbox, the branch with its `±N`, ctx and the mode badge go in that order;
the cost goes last. `shell.appearance.status = "hidden"` hides it, with the right pane and
the strip.

Cards come in two styles, `shell.appearance.cards`: `float` (the default: a
tinted panel with a `▎` bar in the card's tone) or `neon` (a rounded
outline). Notices take it; a theme that cannot paint a background draws the
outline. Tool cards (T174-S6, design 01) map it onto design/shell-tui's
forms: `float` is the tinted block (the rows on the Box surface, a failed
call's header on errorContainer), `neon` design 02's outline (the title and
the state in the top edge, the frame red on a failure). A tool card is a
header, `<icon> <name>  <target>` with its state at the right (`◐ running`,
`✓ 2.3s`, `+3 −1`, `↳ 61 lines`, `✗ failed`), then its body: bash's
`$ <command>` (a long or multi-line command continues under the header,
every word kept) and the last five output lines under
`... (N earlier lines, ctrl+o to expand)`, a failure's exit code, first
failure and place, an edit's numbered diff (`87 − old`, `87 + new`), the
first lines a read or a search found. A call a Jhon guard refused (the
engine's `<name>-guard:` reasons, the shell's own `jhon:` refusals, an ask
the owner declined) is a red band, `✗ refused  <call>  <guard>`, with the
reason and the fix under it. ctrl+o (pi's `app.tools.expand`, or a click on
the card) shows all of it, rows wrapped instead of cut; its hint rides the
header's right while it would change something. `shell.appearance.ascii`
draws every glyph of the chrome in ASCII (`力` reads `jhon`).

The surface, `shell.appearance.surface`: `transparent` (the default) leaves
the right pane's sections on the terminal's background; `tinted` paints them
on one panel, and paints the top row, the strip and the status bar on it too.

The density, `shell.appearance.density`, picks one of the pane's two forms
(L1495): `comfortable` draws each of the pane's sections (LDD,
TODOS, Agents, Jobs) in its own rounded box, in rose ink's border role, in
the same pane; `compact` (the default, L1498) draws them flat with one blank
row between them (design 01); `minimal` stacks them with none and keeps bash tool cards quiet (`↳ N lines` and a short
preview, no output tail or duration, and an edit's diff to three rows); tool calls are otherwise full cards (a
bash card shows its whole command, the last five output lines with the
ctrl+o hint, and its duration, `✓ 2.3s`) (T91, L656).

The right pane (`lib/ui/pane.ts`; design 01) sits beside the conversation
and the editor behind one thin `│`, 47 columns wide whatever the terminal's
width (the divider, a space, 42 columns of panel and 2 blank ones), between
the top row and the status bar. Under 140 columns it folds into one strip
under the top row (design 04): ` ◆ <change>  ◐ slice 3/4 · tasks 2/4 · next
<step>  ✓ passed 6m … alt+j ▾`, giving way from the right (the key, the last
run, the next step, the tasks, the slice; the change last). The pane rides a
pi-tui internal, the layout root's layout method (`lib/ui/pi-layout-hook.ts`,
the only file that names it): where it is missing the shell says so once and
keeps the narrow form. A regular (not fullscreen) terminal has no layout of
its own to put a pane in either: it shows the top row and the strip above
the editor and the status bar under it.

The pane's sections (`lib/ui/panel.ts`; design 01, 02): LDD first (L174),
then TODOS, Agents and Jobs. LDD says the change, its route, strict TDD and
risk, the next step; `Slices n/m · tasks a/b`, a row per slice with the open
one's tasks under it; the last run (verdict, scope, age, then the commit);
the other worktrees' changes. With no change it is one dim line. Agents and
Jobs show only while something runs or ended in the last minute. Agents: a
row per sub-agent running or failed in that minute, its state glyph, its name
(the column fits the longest agent name), what it does, its tokens and
elapsed time at the right (`48k · 4m12s`, `failed 1m03s`); those done and
cancelled fold into `✓ N done · /jhon:agents` and `– N cancelled`; past eight
rows `… N more · /jhon:agents` stands for the rest. Jobs: a row per command or
monitor, `⧗ <id> <command>` and its time, then the newest three that ended in
the last minute (`✓` a clean exit, `✗` a failure, `–` stopped). A click on a
row opens `/jhon:agents` or `/jhon:jobs`; a click on a section's title (the
change's row for LDD) folds it to its title and first row (`▸` at its right);
the pointer over a title, or over an Agents or Jobs row, paints it in the
hover role. The wheel over the pane scrolls the part under it first (the
todo list), then the pane, and never the conversation.

TODOS is the agent's own list (the todo tool) (`lib/ui/todo-section.ts`):
the done and dropped tasks folded into one `✓ N done · M dropped` row, then
one row per open task with its state glyph and, in progress or blocked, its
note; the task at hand wraps instead of being cut.
It shows only while it has tasks and the LDD section has no change (L1469):
with one, the change's tasks are the LDD section's alone (L154). Its title
names its fold key and `done/planned` (`▾ TODOS … alt+t  0/2`), and
`stale · N turns` once the agent let open work sit two turns. `alt+t`
(`shell.keys.todos`), or a click on the title, folds it to the task at hand;
it keeps to a third of the terminal (16 rows at most), past which `alt+PgUp`
and `alt+PgDn` (or the wheel over it) scroll it three rows and a row says where it is
(`↑↓ 4–11/12 · alt+PgUp/PgDn`). In a regular terminal and the narrow form it
sits above the prompt as the Running block does: `── ▾ Todos  alt+t · 0/2 ──`,
its rows two columns in, then a blank row.

In a fullscreen terminal the prompt is a ruled editor:
`─ 力 ready for you ── <hint> ─`, the petal's tone turning while the agent works, the esc hint
(`esc again clears it`, `esc again stops it`) at the rule's right end, then
`› <input>`. A regular terminal draws it as a card in
`shell.appearance.cards`' style. In a fullscreen terminal without the pane
(under 140 columns, Status hidden, or no layout hook) the Running block sits
above the rule while agents or jobs run (design 04): `── Running  2 agents ·
1 job ──`, then their rows (the finished ones of the last minute too); the
pointer paints the row under it and a click opens its view. With
no pane, `alt+j` (the LDD status), `alt+s` (the agents), `alt+o` (the jobs),
`alt+u` (the usage) and `alt+t` (the todo list, above the rule) remain. `ask_user` asks in the dock between two accent
rules, never an overlay.

Every tool call's header starts with its family's icon, geometric by default:
memory ▢, search ⌕, web ◍, shell `$`, edit and write ✎, read ▤, sub-agents ◇,
`ask_user` `?`, and `·` for anything else; a memory call reads as one line
(`▢ save “title”`, then `↳ ✓ <summary>`). `shell.appearance.icons = "emoji"`
draws 🧠 🔎 🌐 📄 🤖 ❓ instead, and `shell.appearance.ascii = true` plain letters,
winning over both.

Its settings are the owner's `~/.config/jhon/settings.toml` (S38), read and
written only through `jhon settings` (`lib/settings.ts`): loaded once per
process with `jhon settings list --json`, and saved with `jhon settings set`.
The animations (`full` or `off`), icons, status and double-Esc preferences
read them, and so do the keys, `ascii`, `compact_at`, `compact_tokens`,
`child_backstop_tokens`, `stall_seconds` and `thinking_stall_seconds`. The
main session compacts at `compact_at`'s share of the window (60%) or past
`compact_tokens` (200000 tokens), whichever comes first, and the
fresh-session notice comes ten points of the window before `compact_at`
(50%) or past three quarters of `compact_tokens` (150000 tokens) the same
way: a share of a very large window never comes. A budget of 0 turns it off
and leaves the share; `JHON_SHELL_COMPACT_AT=off` leaves the share to pi but
keeps the token budget (`compact_tokens = 0` turns that off too). It
compacts after a reply, before an idle prompt of a session already over
budget (a resume, a smaller model; the prompt runs right after it) and
between the turns of a long run, which a follow-up message then continues
(`lib/compaction.ts`, T121); a reply that compacts gives no backstop or
fresh-session notice, which wait for a reply that measures the compacted
context (T124). Before both, past half of `compact_tokens` (100000 tokens)
or 15% of the window, whichever comes first, the orchestrator hears once to
delegate its next several-step unit (`lib/context-backstop.ts`;
`compact_tokens = 0` turns it off, the share too). A sub-agent child has its
own notice (`lib/child-budget.ts`): past `child_backstop_tokens` (80000) or
40% of the window it hears once to wrap up and return; 0 turns it off. No
cap: a child's tool calls are never refused for its context (T155).
`shell.appearance.cards` (`float` or `neon`) and `shell.appearance.surface`
(`transparent` or `tinted`) pick the card style and the chrome's surface;
`shell.appearance.density` (`comfortable`, `compact`, the default, or
`minimal`) its spacing. A key v0.1 removed (`card_style`, which reads as
`shell.appearance.cards` when it was `float` or `neon`, `quiet_tools`,
`key_hints`, the audio backend and timings, `[shell.extra]`, the sounds of
events that never fire) is dropped at the first start with
`jhon settings migrate`, backed up, and the owner told once what went and why; a renamed
value (`animations = "potato"`, the status placement) takes its new place.
With no `jhon`, or one that fails or does not answer in 5 s, every key takes
its default and nothing is saved. The `JHON_SHELL_*` variables
(`JHON_SHELL_COMPACT_AT`, `JHON_SHELL_JHON_KEY` and the rest) still override
the file, but `jhon settings` applies them: no shell module reads them
itself. The old `<agent dir>/jhon-shell/*.json` files are no longer read,
nor migrated since v0.2. The desktop palette's paths are
`JHON_DESKTOP_COLORS` and `JHON_DESKTOP_NVIM_SPEC`; their `JHON_RYOKU_*`
names are no longer read. `JHON_SHELL=off` turns the UI off; a sub-agent's
child process never loads it. The prompt has no vim mode since v0.2;
`shell.editor.vim` is retired, read with a note and dropped by
`jhon settings migrate`. The shell is built and tested against pi 1.5.0 (the
`@earendil-works/pi-*` dev dependencies in `package.json`), the release it
runs on; a new pi release is audited and pinned here before it is supported.

## Adding to Customize

Customize is one registry, `lib/ui/customize/registry.ts`, with one file per
category (`appearance.ts`, `behaviour.ts`, `keys.ts`, `permissions.ts`). A row for a new key is three places, and only these:

1. the row in `shell/lib/ui/customize/<category>.ts`;
2. the key and its default in `SETTINGS_DEFAULTS`, `shell/lib/settings.ts`
   (the shell's defaults when jhon does not answer);
3. its schema line in `engine/internal/settings` (`settings.go`), the one
   source of its kind, values and default (`jhon settings list --json`).

A row over a key that already exists is step 1 alone.
`test/customize-registry.test.ts` and `test/ui-settings-view.test.ts` hold
every row and `SETTINGS_DEFAULTS` to the engine, so a missed step fails
there. A row declares its key, label, note, kind (`choice`, `text`, or `action`, which enter runs: Reset, Reload now),
values, preview, how it reads its value, and where it saves
(`jhon settings set <key>`, or its own handler such as the mode's
`changeMode`); `live` applies it at once, `confirm` makes enter apply it,
`hidden` leaves it out. A new category is one file and one entry in
`CUSTOMIZE_CATEGORIES`; the overlay's categories, the `/jhon:customize <category>`
words, its description and its notice come from there.

## Adding a theme

Customize → Appearance's Theme row lists the pi themes Jhon registers: rose
ink, which `jhon palette --write-pi` writes into the agent dir (rewritten on
a palette change only while it is the active theme, `lib/rose-ink.ts`), and
every JSON in `themes/`, which `package.json`'s `pi.themes` hands to pi
(none today, so the row shows rose ink alone; `fixtures/kanagawa.json` is a
complete pi theme to start from).
Adding a theme is one file, `themes/<name>.json`, a pi theme whose `name` is
`<name>`; the row shows it after pi's next start, previews its `accent` and
`text`, and applies it with pi's `setTheme`, which saves it.

## Where it came from

The UI and its companions (pretty, todo) are adapted from
gentle-pi 4.1.0 (the tool cards, quiet, are Jhon's own since T174-S6) (the question view's layout from 4.6.0), which the render
loaded as a filtered bridge until each had its own place here (docket AY,
B0); gentle-pi is no longer loaded. `THIRD_PARTY_NOTICES.md`, at the repository's root, lists
each adapted file and carries gentle-pi's MIT license.
