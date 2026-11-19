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
| `lib/background-tasks-tools.ts` | The model's side of it: `bash_background`, `monitor`, and `task_list`, `task_result`, and `task_stop`, which take any task by id, plus `/jhon:agents` and `/jhon:jobs`. A task's end reaches the agent as a follow-up message that starts a turn when it is idle; one that lands while a turn runs waits in the shell and goes out once when the turn settles (with the next message after an abort), since Esc clears pi's own queue (a sub-agent's also fires its notification); a task the agent stopped sends nothing, one the owner stopped is reported. A headless run (`pi -p`, `--mode json`), which pi ends at its settle and never wakes again, waits at `agent_before_settle` while its session's background sub-agents, commands or monitors run (never the owner's commands), sends each notice as a follow-up that pi runs as a new turn before settling, and repeats until nothing is pending; one wait is capped at `JHON_HEADLESS_WAIT_SECONDS` (an hour by default), past which a notice names the tasks whose results are lost (on stderr too) and the run ends. Background stays the rule there too (L741). The bar counts the running tasks. A foreground bash that only waits (a sleep over 5 s, `gh run watch`, `wait`, `tail -f`, `timeout` over 60 s, or a standalone sleep while a task runs) runs, and its result carries one note pointing to bash_background or monitor (T155; refused before). Jhon's guards judge their commands as bash; a sub-agent gets none of it, and cannot start a background sub-agent. extensions/jhon.ts registers it. |
| `lib/background-tasks-view.ts` | `/jhon:agents` and `/jhon:jobs`, two views drawn as G's agents view: the sub-agents, or the commands and monitors, running first, beside the selected one's thread: a command's line, output file and output tail; a sub-agent's task, its thread (thinking, text, each tool with its status and output, from the child's JSON events), and answer. `s` stops the selected one, as G's. |
| `lib/review-tool.ts` | T118c: the `jhon_review` tool, the main session's only: the native 4R in one call. `jhon review start` freezes once; each lens prompt runs as one tool-less completion through pi's model registry (`find`, then `streamSimple`: one user message, no tools, a fresh session id, the effort as reasoning, a time bound that grows with the prompt, at most one retry), all lenses at once; then `refute` → the refuter, `validate` → the validator (skipped when nothing is left to judge), `record` writes review-4r.md. Every completion runs on exactly the model and effort the engine names for its routing key; a role with none, or one pi cannot resolve, falls back: the tool says which review agents to launch. A lens that fails twice or answers malformed is `unavailable` in the record. The model gets the verdict, the counts and the correction set (at most 2,000 characters), never the prompts. |
| `lib/events.ts` | The pi.events channel the background tasks share with the UI: the running tasks. |
| `lib/ask.ts` | The `ask_user` tool, as Claude Code's AskUserQuestion: 1 to 4 questions, each with 2 to 4 described options (or none for a free answer), multiple choice, and G's "Type something." row for a typed answer, put through the question view `lib/ui/ask-view.ts` one after another (pi's select dialog where the UI has no `ui.custom`). The view is G's questionnaire, in the dock inside G's native fullscreen interaction (keys go to the view, clicks reach it) between two accent rules: `[n/m]  ▸ <header>`, the question, `❯` on the focused option with its description dim under it, `↑↓ move · enter select · esc cancel` (`space toggle` too on a multiple choice); a click picks an option. |
| `lib/context-backstop.ts` | T115e: a delegation signal for the main session, never a child. Once its context passes half of `shell.behaviour.compact_tokens` (100000 tokens; `compact_tokens = 0` turns it off) or 15% of the model's window, whichever comes first, the orchestrator hears once to delegate its next unit that needs several steps (its reads, edits and checks) with a short handoff, and keep single small edits and state checks inline. It reads the context usage at most once per 5 s, re-arms after a compaction drops the context below it, and reaches the agent appended to a tool result or as a persisted message after a reply, never in the system prompt. The ladder: this backstop, then the fresh-session notice, then compaction; two on one reply are one notice. |
| `lib/fresh-session.ts` | One session per change: after a reply that leaves the main session's context past ten points of the window before `shell.behaviour.compact_at` (50%) or three quarters of `shell.behaviour.compact_tokens` (150000 tokens), whichever comes first, the owner (a notice) and the agent (a message on its next turn) hear once to save a handoff and start a fresh session; a `jhon archive` brings the same notice once per change. Never a block, never in a child. |
| `lib/desktop-palette.ts` | With `integrations.theme.palette = "desktop"` (`"ryoku"`, its older name, too), where a desktop palette file stands (`JHON_DESKTOP_COLORS`), draws the rashin poster as pi's header; off, the default, nothing reads it. |
| `themes/` | The pi themes Customize → Appearance's Theme row lists after rose ink ("Adding a theme"); none today. |
| `fixtures/kanagawa.json` | The former Kanagawa theme, kept as the reference that declares every color pi's theme schema requires (`lib/desktop-palette.ts`); not registered with pi, so the Theme row does not list it. |
| `lib/rose-ink.ts` | With `integrations.theme.palette = "desktop"`, asks `jhon palette --write-pi` for the rose ink theme now and on every desktop palette change, while rose ink is pi's active theme; and, whatever the palette, when Customize switches back to rose ink, before applying it. |
| `extensions/ui.ts` | The shell UI: the status bar and header, the prompt editor (double-Esc cancel, animations; after 60 s with no stream event and no tool running its working line reads `waiting for Claude API · <N>s`; after 75 s the main session aborts the turn as Esc does and sends one continue, once per owner message, never while a tool or an owner prompt is open; `shell.behaviour.stall_seconds` sets the seconds and `0` turns all recovery off; on the `claude-bridge` provider or while a reasoning model thinks above minimal, where minutes can pass with no stream event, the threshold is `shell.behaviour.thinking_stall_seconds` instead, 300 s, `0` never recovering there), the sidebar, the changes view, the command palette, and Customize. |
| `extensions/pretty.ts` | pi-pretty (syntax-highlighted reads, diffs, file trees) under the UI: the render lists `@heyhuynhgiabuu/pi-pretty` pinned with nothing loaded, so pi installs it into `<agent dir>/npm`, and this loads it from there, never in a sub-agent, keeping the prompt editor the UI's and its FFF store in the agent dir. |
| `extensions/todo.ts` | The `todo` tool the model keeps its task list with, drawn as a card above the editor and in the sidebar, restored from the session. With no list, the model is told when to start one, and three tool-use turns that change something without a list bring one nudge per owner message. The list block and the start rule never sit in the system prompt: each goes out as a persisted `jhon-todo-list` message when it changed, and the nudges (`jhon-todo-start`, `jhon-todo-reminder`) as persisted steer messages after a tool-use turn, so the prompt cache holds (T115d). |
| `extensions/quiet.ts` | The quiet tool cards: `read`, `grep`, `find`, `ls`, `edit`, and `write` drawn as compact cards with bounded previews (and `bash` too under the minimal card content), executing as pi's own tools (`bash` in the OS sandbox when `sandbox.mode` is on, `lib/sandbox.ts`, drawn the same); every other tool's call gets its family icon (`lib/ui/tool-icons.ts`). Always Jhon's cards (T15.3: `quiet_tools` is gone). |
| `extensions/notifications.ts` | Audio notifications (agent finished, failed, needs attention): tones in `assets/sounds/`, played through PulseAudio natively; configured in `/jhon:customize` → Notifications. The context has one notice, the fresh-session one, ten points before `shell.behaviour.compact_at` or at three quarters of `shell.behaviour.compact_tokens`, whichever comes first. |
| `lib/ui/` | The UI's modules, one per piece: bar, cards, prompt, sidebar, changes, usage (the status bar's windows and the /jhon:usage panel), stats (the rail's STATS), palette and Customize. The rail's LDD section (`ldd-status.ts`), first in the rail, shows every LDD change in progress, one block each, the session's own first: `◆ <change>` with `⎇ <worktree>` at the right, then indented `N/M slices · <route> · on n: <slice>` and `next <step> · tests ✓ passed`, `✗ N failed` or `⧗ none`; the other worktrees' changes say `N/M slices · next <step>`. Its header's count is `N changes`, or the route when there is one change. Its keys (`t` tests, `d` diff, `o` tasks.md) live in the LDD detail (alt+j), which also shows the slices one by one. Under a multiplexer the rail draws it too: the multiplexer shows nothing of LDD (L112). |
| `lib/profiles.ts` | The owner's active Jhon profile, shown in the header poster and the bar, and a profile's delegation mode (`delegation` in profiles.json: `writer`, else `inline`). |
| `lib/session-profile.ts` | T120: a profile used in one session only, apart from the default for new sessions. `u` in the profiles panel appends the session entry `jhon-shell.session-profile/v1` (`{ profile }`) and writes nothing global: no `jhon profile use`, and settings' `pi.profile`, profiles.json's `active`, the agent folder's `settings.json` and the rendered agent files stay as they are, so the other sessions are untouched. From then on the session's sub-agents run on that profile's model and thinking level for each agent (read from `jhon profile json` at each launch, in place of the rendered file's `model:` and `thinking:`), its orchestrator switches to the profile's model and level through `pi.setModel` and `pi.setThinkingLevel` with a notice (pi saves neither as a default), its delegation mode follows the profile, and the header, the status bar and the panel name it, marked while it is not the default (`<name> (session)`; `●` on its tab). The entry is read from the session's branch at each start: a resumed session comes back with its profile, `/new` starts on the default. A sub-agent hears its parent session's profile in `JHON_PI_SESSION_PROFILE`, so the sub-agents it launches follow it. `u` is refused, with one line and nothing changed, when this pi does not list one of the profile's models as available (its provider is not loaded in this process, or has no credential): `d` and a restart load it, as the line says; the panel wraps a note wider than the card onto up to three rows. A session profile that was renamed or deleted puts the session back on the default with a one-line notice. `d` is unchanged: `jhon profile use`, the default every session without a profile of its own reads. Claude Code sessions (no panel; their agent files are global) and `jhon review-native` keep reading the default profile. |
| `lib/delegation-mode.ts` | T117c: the active profile's delegation mode, read once per session with the prompt's frozen parts, so the prompt stays byte-identical. In `writer` mode the main session's prompt carries `jhon-orchestrator-writer.md` (rendered beside the orchestrator's file) in place of the orchestrator's region between `<!-- delegation:begin -->` and `<!-- delegation:end -->`; a missing file, a missing marker or markers out of order keep the organic rules whole, said once. A child never gets it. The profiles panel's actions (`JhonPanelDeps`): `setMode` (`w` then `s`) runs `jhon profile mode <profile> <inline\|writer>` (from the next session); `useNow` (`u`) makes a profile this session's only (T120, `lib/session-profile.ts`) and, when this session's mode changes, rebuilds the prompt once with the notice "The orchestrator's rules switch to writer mode from the next turn." (one notice with the orchestrator's model switch before it when `u` changes both: pi draws only the last of two notices sent back to back); `makeDefault` (`d`) and `use` activate it without the rebuild (the engine has one `active` profile, which is also the default for new sessions). |
| `lib/writer-guard.ts` | T117c, T117e: in `writer` mode the main session's `edit` and `write` calls change at most 20 lines per owner request, summed over its calls since the owner's last message (pi's `input` event from the owner; the extensions' messages, such as notices, sub-agent completions and follow-ups, do not reset it). A call counts when it is let through and is taken back when it fails or another guard refuses it; an edit counts its replacements, a write its new content against the file's current one, their common head and tail left out. The call that would pass 20 is refused with the total so far and a reason that points to a worker with a self-contained brief. Files under `odd/` (the plan doc, the Log) always pass and are not counted; `inline` mode and children are not limited. It reads the same session-frozen mode as the prompt. |
| `lib/compaction.ts` | T121: the main session's own compaction at `compact_at` or `compact_tokens`, one at a time: after a reply; in the `input` event before an idle prompt when the session is already over budget (the prompt waits and runs on the compacted context); and at a `turn_end` with tool results over budget, where `ctx.compact()` stops the run and a `jhon-compaction-continue` message with `triggerTurn` continues it once the compaction ends or fails. A headless run is not interrupted mid-run; a child never compacts here. |
| `lib/child-budget.ts` | T122: a sub-agent child's own ladder (the main session's backstop, fresh-session notice and compaction never reach a child). Past `shell.behaviour.child_backstop_tokens` (80000 tokens; 0 off) or 40% of the window, whichever comes first, the child hears once per crossing to wrap up and return path:line evidence and decisions, not raw output. No cap: a child's tool calls are never refused for its context (T155). An explore child that may call CodeGraph hears once, past 12 lookups or 100,000 bytes since its last CodeGraph call, to prefer `codegraph_explore`. T125: a worker-type child (`JHON_PI_AGENT` `worker`, `jd-fix-agent`, or a name ending `-worker`) that may call CodeGraph gets the same count with a worker's reminder: CodeGraph returns a symbol's source already read, read only the lines you change, and prefer the read tool with offset/limit over cat/sed through bash. Lookups are read, grep, find, ls, and a bash command that only reads (`readOnlyBash`: cat, sed -n, grep, ls, head…, behind one leading `cd <dir> &&` too, T134), with their output bytes; a bash that writes or runs something is not counted; a CodeGraph call re-arms the count. All of it rides the tool result, so the child's system prompt stays byte-stable. |
| `lib/exploration-budget.ts` | What counts as a lookup (read, grep, find, ls, inspect, codegraph, and a bash command whose every segment only reads), which a sub-agent's CodeGraph read budget counts with; the main session's exploration-budget notice is gone (T155). |
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
| `/jhon:commands` | `alt+k` | The command palette, G's Commands card: `Commands … esc`, `› Search`, Jhon's commands in G's groups (Configuration, Session, Diagnostics) with G's labels where G has the command, the selected row on the `selectedBg` bar (`▸` without one), the shortcut at the right, `↑/↓ move • enter run • esc close`. A click runs a row, the wheel moves, the pointer paints the row it is on. |
| `/jhon:changes` | `alt+g` | What the session's agents changed, committed or not, per worktree (T132): each of the session's roots, its own folder and every worktree a sub-agent runs in (the `subagent` tool registers the one it creates with `worktree: true` or is given as `cwd`; `session_worktree_register` adds another), is compared against its base, the commit its HEAD was at when the session first registered it, kept in the session so a resume compares against the same commit; untracked files count too, and a root with nothing different from its base is not listed. A root whose base no longer exists, or is no longer an ancestor of its HEAD (a rebase, another branch), and a root of a session saved before bases were kept, reads against HEAD, uncommitted changes only: its row ends in `· vs HEAD` and its summary says why. So does a root the session registered before the repository's first commit (no base is taken later: the first commit may be the session's own work), and a root known only from an edit. With nothing to list, the notice says `No changes since the session's start …` only when every root was compared with its base; for a root read against HEAD it says `No uncommitted changes in <root>, compared with HEAD: <why>.`, and that the session's other roots have no changes against their base. A sub-agent's work merged into another root is counted once: its own worktree is not listed again while its working tree is clean and another listed root (registered before it, compared with its base) both lists every path it lists and has a branch that holds its HEAD; with uncommitted work, a commit that branch lacks, or a file the other root does not list (a reverted merge, a squash or a cherry-pick, an edit back to the base's content), both are listed as before. `/jhon:changes <dir>` on such a worktree says `<dir> has no row of its own: its work is merged into <root> and is listed there.` and opens on that root. While another root has changes, a root read against HEAD that has none still has its row, with no files, the `· vs HEAD` mark and its sentence: its commits are not counted, so the count is not the session's total. Names are read with `-z`, so a file whose name Git would quote (not plain ASCII, a tab, a newline) counts its lines. Two limits: an uncommitted edit that puts a committed file back at its base's content hides the file, though `git status` still marks it; and a root registered in the middle of a rebase takes the rebase's detached HEAD as its base. A file's diff is against the same commit as its counts. Drawn as G's changes view, full screen: `╭─ ✎ Changes · N worktrees ─╮`, the worktrees as groups (`▾ <branch> · <dir>`) with their files (`M <file>  +a -d  <dir>`, `U` for untracked) beside the selected file's path and diff, and G's keys line `j/k select   enter toggle/open   ← fold   ctrl+j/k scroll   r refresh   F diff only   esc close`; `F` shows the diff alone (`j/k file … F files   esc files`). `/jhon:changes <dir>` (the LDD detail's `d`) shows only the worktree that holds dir. The one diff view. |
| `/jhon:status [<change>]` | `alt+j` | The LDD status, the LDD detail (an LDD change's, never the git changes `/jhon:changes` shows), the rail's LDD section expanded, one overlay near full screen as G draws its profiles view (L174: the whole terminal, a rounded frame in the border role with `│ ` margins; inside, the view's tab `LDD` over its rule with the tab's `━` in the accent, the title row `LDD · <change>`, the body, then G's key hints dim joined by two spaces (`t tests  d diff  …  esc close`, `esc close` never dropped) and a feedback row; blank rows fill the rest, `↑/↓` scroll a longer body): the route, its risk and why, every slice and what it delivers, the open slice's tasks, the record, the last run and the notes. `t` runs its tests (`jhon executions run <change> --affected <base>` in the background, the owner's run; its time and end show here), `d` opens `/jhon:changes` on its worktree, `o` its tasks.md, `r` reads it again. `/jhon:status <change>` opens a named change (L214); its argument completes to the project's changes, each with its intent's first line. The audit and the memory are the dashboard's (`/jhon:dashboard`). |
| `/jhon-ldd [<action>] [<change>]` | | An LDD action on a change, as Claude Code's `/jhon-ldd`: the agent gets the prompt the release's `jhon-ldd` skill holds, the arguments in place (byte for byte what pi's prompt template sent, which the render no longer writes); while the agent works it waits for the turn to end. Its argument completes to the actions (`new`, `ff`, `continue`, `status`, `explore`, `research`, `apply`, `verify`, `archive`, `init`), each with the first clause of its line in the skill, then to the project's changes. |
| `/jhon:grants` | | The owner's grants for the project (the permission mode is only in Customize → Permissions, L214), its own overlay in G's detail card (`Grants … esc`): `Active grants · N`, every live grant one row (its class, its project, `this project` for the session's, and `until HH:MM · N min`) or `No active grants: enter on a class below gives one.` (L175), then each grant class (push, gh-write, publish, remote, deploy, outside-write, destroy) one row with its state here under `Grants · n/m`. No key here changes the mode. `enter` grants the chosen class for the chosen duration (`d`), `x` revokes it, each after a confirming key, `r` reads them again, `esc` closes. |
| `/jhon:profile` | | The model profiles near full screen as G's profiles view (L174: the tab `Profiles` over its rule, the title row `Models and profiles`, the body, G's key hints dim; the same frame as the LDD detail), as radios (`●` the active one) under `Profiles · n/m`, the chosen one's model per agent as its Preview: `enter` activates, `n` creates a copy, `e` edits an agent's model and effort, `-` puts an agent back on agents.yaml, `d` then `d` deletes. In the panel, `u` uses the chosen profile in this session only (T120, `lib/session-profile.ts`) and `d` makes it the default for new sessions. `/jhon:profile list \| show [<name>] \| use <name>` runs the CLI (`use` sets the default); its argument completes to those words. |
| | `alt+t` | Folds or opens the rail's TODOS section. |
| `/jhon:customize` | | Customize (Settings until L174; `/jhon:settings` is gone, L195), drawn as G's Customization view: G's Commands card (70% wide, at most 24 rows) with `Customize … esc`, CATEGORIES on the left (Appearance, Behaviour, Keys, Notifications: the shell's own `shell.*` settings; Permissions: the project's permission mode, `● normal ○ yolo ○ super yolo`, `←/→` only hold a pending choice (`· enter applies`), `enter` sets it with `jhon mode <mode> --root <root>` (the only place the mode changes, L214), super yolo only after your Yes, `esc` or leaving the row drops it, its note pointing at `/jhon:grants` for the grants, as G keeps its permissions among its Customization rows, L175; Appearance opens with the Theme row and ends with Reset, which puts every `shell.appearance.*` key back to its default after `enter` holds it and a second `enter` applies it, `esc` drops it; Keys ends with Reload now, which closes Customize and reloads pi so changed keys apply without a restart) and `<Category> · n/m` over the selected category's settings, each one line with its values inline (`● current  ○ other`; `‹ value › n/m` when they do not fit), the Preview of the highlighted row (a focused setting's `<label> — <what it does>`, else `Preview · <value> · <what it shows>`, then its sample: the animation, the glyph set, the tool icons, a notice and a tool card in the card style, the rail on the surface, a status line, what a key opens, what a number does, the sound) and `Click · <keys>` at the bottom. G's keys: `↑/↓` or `j/k` move in the focused pane, `→`, `Tab`, `Enter` or `Space` open a category, `←/→` on a setting change its value and apply it at once (`←` past the first value goes back), `Tab` returns, `Enter` on a free value edits it in place (`Enter` saves, `Esc` cancels), `Esc` closes; a click picks a category, a setting or a value. Under 60 columns the categories take the card until one is opened. A refused value is a notice (`Customize: <why>`), as G's; the Preview keeps its headline. `/jhon:customize <category>` opens one; its argument completes to the categories, each with what it holds. It holds what `/jhon:customize`, `/jhon:animations` and `/jhon:double-esc-cancel` did. Runtimes and integrations are `jhon setup`'s; `pi.profile` is `/jhon:profile`'s. |
| `/jhon:panel` | `alt+r` | The panel sheet, as the Gentle Shell's: the rail's sections (LDD, TODOS, JOBS, AGENTS, STATS, CHANGES, INTEGRATIONS) full width from the top, under a ` 力 Panel … esc back` row, one blank row between them, `↓ N more · scroll` while rows hide below; it leaves six rows for the work row, the editor and the status bar; a tinted surface paints it. `esc`, `q` or a click on `esc back` close it; `↑/↓`, `j/k` and the wheel scroll it. The editor rule's `[ Panel ]` and a tap on the work row open it too. `alt+r` (`shell.keys.rail`, Jhon's extra) works under a multiplexer or not; `/jhon:panel`, the palette's Panel row and `alt+r` do nothing while the rail is visible. |
| `/jhon:dashboard` | | This session's audit in the dashboard: the browser opens at `http://127.0.0.1:7480/#/s/<this session's id>` (its home for a session not saved to disk), and `jhon dashboard` is started, detached, when it does not answer; when the dashboard answering there runs an older build than this shell's `jhon` (no `/api/version`, or another binary) it is replaced the same way, `jhon dashboard` stopping it only when it is proven Jhon's; one that is not replaced is opened all the same, and the notice says to run `jhon dashboard --port <p>` in a terminal to see why. With no display (no `DISPLAY` or `WAYLAND_DISPLAY`; macOS always has one) it opens no browser and says the address instead. `integrations.dashboard` is retired: nothing gates it. |
| `/jhon:agents` | `alt+s` | The sub-agents, as G's Agents overlay, full screen (T92, L657): the top border `力 Agents · this session · N active · M finished` (`all sessions` after `a`) with `[F Fullscreen]` (`[← Back]` on the thread alone) and `[× Close]`; the list (`▸ └ ◐ Subagent <agent> · <its task's first line>  <time>`, running first) beside the selected agent's live thread: its header `<agent> · <status> · <model> · <session> · <tokens> · <cost> · <time>`, its task, then `Thinking` and `Text` blocks and each tool event (`Tool · bash · Complete`, `  Output` and its lines), then its answer; G's footer `[ Follow ] [ Open session ] [Stop] [Scope]` and `j/k task   ctrl+j/k scroll   f follow   s Stop selected   a all sessions   o open session   esc back   q close`. `a` (or `[Scope]`) shows the agents of every session this pi has run and back; `o` opens the agent's saved session file in `$VISUAL`/`$EDITOR`; `F` (or `tab` when narrow) shows the thread alone; `s` stops the selected agent at once; a click selects a row or presses a button. The rail's AGENTS section and the work row open it. |
| `/jhon:jobs` | | The background commands and monitors, in their own view (T92): `力 Jobs · N active · M finished`, the list beside the selected job's command line, output file and tail; `[ Follow ] [Stop]` and the same keys but `a` and `o`. A tap on the rail's JOBS section in the work row opens it. |
| `/jhon:usage` | `alt+u` | Subscription usage for every configured provider (T93, L659), as G's alt+u panel: `◆ Subscriptions` (`· refreshing…` while it fetches), one block per provider in scope — the session's own first, marked with `力` (`jhon` in ASCII), then each provider the active profile's sub-agents route to — `<provider> · <plan> · updated Nm ago`, then one row per window (`claude 5h ▰▰▱… 25% · resets in 5h 36m`), a `⊘ … is at 84% …; resets in …` line near a limit; a provider without data says why (`usage arrives with the first reply`, `no usage yet · r to fetch`, `the fetch failed · r to retry`, `this provider does not report subscription usage`). It refreshes on opening; `r` refreshes, `esc` or `q` closes. The status bar keeps the answering provider's 5h and week windows. `JHON_SHELL_USAGE_KEY` sets another key or `off` (`shell.keys.usage` stays retired). |

`/jhon:stats` stays gone (L117): STATS is a rail section now (T92), and the
usage history is the dashboard's.

Each key is set or turned off (`off`) in `[shell.keys]`: `panel`, `tasks`,
`todos`, `changes`, `rail`, `commands` (`usage` is retired there: warned once,
dropped by `jhon settings migrate` after a backup; `alt+u` is set by
`JHON_SHELL_USAGE_KEY`). `alt+a`
stays the prompt's select-all alias, so the Agents view takes `alt+s`.

The shell's whole visual is the Gentle Shell's (L146), with Jhon's brand
(`力`, rose ink) and only Jhon's options. Its chrome is G's split layout
(L153 answer 3): a title row on top (` 力 <project>  <~path>`, the session
name at the right; the project alone when a multiplexer host that shows the path is announced, `jhon:host`) and the status bar at the
bottom (the branch and its `±n` count, the files the session changed, committed or not, as `/jhon:changes` lists them; the owner's permission mode
when it is not normal, `YOLO` in amber or `SUPER YOLO` in red and bold, read
from the status the LDD section already reads, and without the rail
`⧗ N jobs`, on the left; `model · effort · profile`, `ctx` on the one 70/90
meter ladder with `· compacts at N%` near it, the cost, and the subscription's
windows in G's usage slot on the right: the week with its reset, `5h` from
70%, the reset on the window with the highest tone). Narrowing, it degrades as
G's: the windows' reset and gauges go, then all but the first window, then
the windows, the profile at each step, then the model; a long branch shortens
first; the badge goes last. Beside the rail (140 columns and up) each sits
between two margin rows; under 140 columns the title row carries
`model · effort · profile` at its right and there is exactly one status row
without the model, as G's: a row of its own under the transcript and the
editor, pi's footer painting nothing and giving its row back. The title row
clips the path with `…` when it runs out of room, never drops it. A regular
(not fullscreen) terminal draws G's one compact bar instead:
`力 jhon ⟡ <path> <branch> ±changed ⟡ model · effort ⟡ ctx ⟡ cost ⟡ <windows> ⟡
<statuses>`, where `⧗ N jobs` and the mode badge sit among the extensions'
statuses in key order, as G's jobs and permission statuses do; the session
name at the right while it fits; overflowing, the path keeps its last
segment, a long branch shortens, then segments go from the end.
`shell.appearance.status = "hidden"` hides it.

Cards come in G's two styles (L153 answer 1), `shell.appearance.cards`:
`float` (the default: a tinted panel inside a one-column margin, the `▎` bar
in the card's tone on every row, a blank row above the heading and before
the body) or `neon` (the rounded outline, `╭─ <glyph> <title> ── <hint> ╮`).
Notices (a background task's end), tool cards (`<icon> <name> <args>`, then
`↳ ✓ <summary>` or `↳ ✗ <why>` and the preview), the todo card above the
prompt (`力 Todos ▾ Collapse · 2 of 5`, its fold key and action at the right;
G's body rows: every task in its order, a done one struck, until twelve tasks,
when done and dropped fold into `✓ N done` and `✕ N dropped`; folded, the task
in progress, else the next one, else `· N open`) and the rail's cards take it. A theme that
cannot paint a background draws the outline. A card that names no glyph
wears the brand, `力`, where G's wears its `✿`. `shell.appearance.ascii` draws
`+ - |` and `|` for the bar, and every piece of the chrome in the glyph
table's ASCII forms: the prompt's rule and card, the title row and the bars
(`力` reads `jhon`, but the title row drops it before the name and the
prompt's petal reads `*`; `│` and `⟡` read `|`, a meter `#####---`), the Status and
LDD rows, TODOS, JOBS and AGENTS, the cards' titles and the todo card.

The surface is G's too (L153 answer 2), `shell.appearance.surface`:
`transparent` (the default) draws each rail section as its own card in the
card style on the terminal's background, a clear row under the title row;
`tinted` paints them all, headers `▾ NAME … count` and rows indented under
them, on one panel down to the bottom, and paints the title row, the status
bar, the narrow work row and the panel sheet on it too.

The density is G's too (L195), `shell.appearance.density`: `comfortable` (the
default, as G's) draws a margin row above and below the title row and the
status bar and a blank row between the rail's sections; `compact` and
`minimal` draw none of them (a float card keeps its blank row, or two would
fuse, as G's). G's `minimal` also drops the brand, the gauges and the key
hints from its bars; here `minimal` spaces as `compact` does and drops
nothing else from the bars. Tool calls are full cards by default, as G's (a
bash card shows its whole command, the last five output lines with the
ctrl+o hint, and "Took Ns"); `minimal` keeps them quiet, as G's "Card
content: minimal" does (T91, L656).

The rail's sections are G's panel sections: a header `▾ NAME … count` (the
fold glyph muted, the name upper-case in text bold, the count muted at the
right) and rows indented under it. The rail is 50 columns at every width,
orders them LDD first (L174), then G's TODOS, JOBS, AGENTS and the rest of
the Status sections (L153 answer 4): `▾ LDD … verified route` (or `N changes`),
`▾ CHANGES … 2 files` (`+a −d` and `/jhon:changes`, counted as `/jhon:changes`
counts them, each root against the commit the session first saw it at, or
`none` over `no changes since session start`; while a root is read against
HEAD and nothing is listed, `none` over `no uncommitted changes vs HEAD` and,
under it, each such root's `<dir>: <why>`, since its commits are not counted;
while other roots do have changes, the counts, then `commits not counted,
read vs HEAD:` and the same `<dir>: <why>` rows, so the count is not read as
the session's total; when a root's Git status could not be read and nothing
is listed, `unknown` over `Git status could not be read` and `in X of Y
roots`, never `no changes`; a click on
that row opens `/jhon:changes`) and `▾ INTEGRATIONS … 1` (`permissions <what
the mode means>` while the mode is not normal, then each extension's status,
or `none`); there is no STATUS card; a section with nothing to
show is left out (L173) but AGENTS, which idle reads `none running ·
/jhon:agents`, as G's (L165), and TODOS, which idle reads the same way:
`▾ TODOS … alt+t  idle` and `no tasks` (L175); JOBS still hides while empty. No header carries a key but TODOS' own fold key, dim,
before its count: `▾ TODOS … alt+t  2/5`; a stale list says so in that count,
in the warning colour (`alt+t  stale · 3 turns · 2/5`), with no row of its
own. Every header is a control: the pointer over it paints its glyph and title
in the hover role, and a click folds the section to its header for the
session; folded TODOS keeps its task in progress under the header. `alt+t`
folds TODOS, and `alt+j` (LDD) and `alt+s` (AGENTS) open their views,
listed in the palette and the key table. The rail has no key line:
`/ commands` is only the prompt's placeholder. TODOS is bounded as the card
above the prompt is, a third of the screen and at most 16 rows, and scrolls
(`ctrl+shift+↑/↓`, `↑↓ 1–12/20` saying where) past them; done and dropped
tasks fold into `✓ N done` and `✕ N dropped`, the task in progress (or
blocked) in full with its note under it (`└ <note>`), every other task one
row cut with `…`. TODOS is only the agent's own list (the todo tool), inside an
LDD change too: the change's tasks.md and its slices are the LDD section's, so
nothing shows twice (L154); the prompt's rule to start a list and its nudge stay
silent inside a change. JOBS is G's: one row per background command or
monitor (`⧗ <id> <label>  <time>`, running newest first, a finished one
`✓`/`✗`/`–` for a minute; `N running · N finished`), folded the first job and
`… N more`, hidden while there is none. AGENTS is G's too: the running
sub-agents and those that failed in the last minute, in start order, one row
each (`◐ <task>  <kind>  <tokens> <time>`, the kind in its own column, a
failure `✗ <task>  <kind>  failed <time>`), the done and the stopped summed in
one row each (`✓ N done · /jhon:agents`, `– N cancelled`), past eight rows
`… N more · /jhon:agents`; folded, its first row. STATS is G's too (T92,
L658), after AGENTS: `▾ STATS … $<spent>`, `profile <name>`, `spent $… · N
turns · <wall time>`, `tokens in … · out …`, `cache N% hit · … read`,
`tools N calls · N failed`, `agents N runs · <tokens> · $…` (this session's
sub-agents, finished ones too), then one row per model (`<model>  N turns ·
<tokens> · $…`), from the session's entries; hidden until the first answer,
folded by its header. The work row's digest
is `◐ N agents · ✗ N` (the failures of the last minute); `alt+s` keeps a sub-agent's
latest step. The panel sheet (`/jhon:panel`, `alt+r`) holds the
same sections.

In a fullscreen terminal the prompt is G's panel editor:
`─ 力 waiting for input ──── <hint> ─` (the petal is the brand, G's `✿`; working,
its tone turns), the esc hint
(`esc again to clear`, `esc again to cancel`) at the rule's right end, then
`› <input>` (the `›` in the accent) and a blank last row; when the label and
the hint do not fit, the rule is blank, as G's. A regular (not fullscreen)
terminal draws G's card prompt in `shell.appearance.cards`' style: float,
tinted `▎` rows with `力 waiting for input · <hint>` on the first, or neon, a
rounded frame with the hint in its bottom rule. In a fullscreen terminal without the
rail (under 140 columns, or Status hidden) the rule ends in G's `[ Panel ]`,
which opens the panel sheet, and running sub-agents and the todo list fold
into one work row above it (` ▸ ◐ 1 agent … 2/5 todos`, the agents leading,
else the task in progress) in place of the TODOS card above the prompt; a tap
on the row opens the leading section's own view, as G's: `/jhon:agents` while
sub-agents run, the panel sheet for the todo list. `ask_user` asks as G's
questionnaire, drawn in the dock between two accent rules, never an overlay:
the question bold, each option behind `❯ ` when focused, its description dim
below it.

Every tool call's header starts with its family's icon, geometric by default:
memory ▢, search ⌕, web ◍, shell `$`, edit and write ✎, read ▤, sub-agents ◇,
`ask_user` `?`, and `·` for anything else; a memory call reads as one line
(`▢ save “title”`, then `↳ ✓ <summary>`). `shell.appearance.icons = "emoji"`
draws 🧠 🔎 🌐 📄 🤖 ❓ instead, and `shell.appearance.ascii = true` plain letters,
winning over both.

Its settings are the owner's `~/.config/jhon/settings.toml` (S38), read and
written only through `jhon settings` (`lib/settings.ts`): loaded once per
process with `jhon settings list --json`, and saved with `jhon settings set`.
The animations (`full` or `off`), icons, status, double-Esc and
notification preferences read them, and so do the keys, `ascii`,
`compact_at`, `compact_tokens`, `child_backstop_tokens`, `stall_seconds` and `thinking_stall_seconds`. The main session compacts at `compact_at`'s share of
the window (60%) or past `compact_tokens` (200000 tokens), whichever comes first, and
the fresh-session notice comes ten points of the window before `compact_at`
(50%) or past three quarters of `compact_tokens` (150000 tokens) the same way: a share of a very large
window never comes. A budget of 0 turns it off and leaves the share;
`JHON_SHELL_COMPACT_AT=off` leaves the share to pi but keeps the token budget
(`compact_tokens = 0` turns that off too). It compacts after a reply, before an
idle prompt of a session already over budget (a resume, a smaller model; the
prompt runs right after it) and between the turns of a long run, which a
follow-up message then continues (`lib/compaction.ts`, T121); a reply that
compacts gives no backstop or fresh-session notice, which wait for a reply that
measures the compacted context (T124). Before both, past half of `compact_tokens`
(100000 tokens) or 15% of the window, whichever comes first, the orchestrator
hears once to delegate its next several-step unit (`lib/context-backstop.ts`;
`compact_tokens = 0` turns it off, the share too). A sub-agent child has its own
notice (`lib/child-budget.ts`): past `child_backstop_tokens` (80000) or 40% of the
window it hears once to wrap up and return; 0 turns it off. No cap: a child's
tool calls are never refused for its context (T155). `shell.appearance.cards` (`float` or `neon`) and `shell.appearance.surface`
(`transparent` or `tinted`) pick the Gentle Shell's card style and chrome
surface; `shell.appearance.density` (`comfortable`, `compact` or `minimal`) its
spacing. A key v0.1 removed
(`card_style`, which reads as `shell.appearance.cards` when it was `float` or `neon`, `quiet_tools`, `key_hints`, the audio backend and timings,
`[shell.extra]`, the sounds of events that never fire, `keys.usage`) is dropped at the
first start with `jhon settings migrate`, backed up, and the owner told once
what went and why; a renamed value (`animations = "potato"`, the status
placement) takes its new place. With no `jhon`, or one that fails or
does not answer in 5 s, every key takes its default and nothing is saved.
The `JHON_SHELL_*` variables (`JHON_SHELL_COMPACT_AT`, `JHON_SHELL_JHON_KEY`
and the rest) still override the file, but `jhon settings` applies them: no
shell module reads them itself. The
old `<agent dir>/jhon-shell/*.json` files are no longer read, nor migrated
since v0.2. The desktop palette's paths are `JHON_DESKTOP_COLORS` and
`JHON_DESKTOP_NVIM_SPEC`; their `JHON_RYOKU_*` names are no longer read. `JHON_SHELL=off` turns the UI off; a sub-agent's child process never
loads it. The prompt has no vim mode since v0.2; `shell.editor.vim` is
retired, read with a note and dropped by `jhon settings migrate`.
The shell is built and tested against pi 1.5.0 (the `@earendil-works/pi-*`
dev dependencies in `package.json`), the release it runs on; a new pi release
is audited and pinned here before it is supported.

## Adding to Customize

Customize is one registry, `lib/ui/customize/registry.ts`, with one file per
category (`appearance.ts`, `behaviour.ts`, `keys.ts`, `notifications.ts`,
`permissions.ts`). A row for a new key is three places, and only these:

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

The UI and its companions (pretty, todo, quiet, notifications, stats)
are adapted from gentle-pi 4.1.0, which the render loaded as a filtered
bridge until each had its own place here (docket AY, B0); gentle-pi is no
longer loaded. `THIRD_PARTY_NOTICES.md`, at the repository's root, lists
each adapted file and carries gentle-pi's MIT license.
