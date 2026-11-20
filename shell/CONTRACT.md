# The shell contract

What `jhon-shell` consumes from the framework. The shell reaches Jhon only
through this surface. It never reads `prose/`, the manifest, or the
engine's files. A change to anything below changes the contract. Every call the shell
makes is listed in "Every `jhon` call the shell makes".

**Version:** `jhon.shell/v1`. The version lives in two places:
`prose/contracts/vocabulary.yaml` (`shell_contract`) for the binary, and
this package's `package.json` (`jhon.contract`) for the shell.
`test/release/contract_test.go` holds them equal.

## The handshake

`jhon contract --json` prints `{"shell": "<version>"}`. When the shell
loads, it runs this command once with the binary that `jhon.json` names.
If the command fails, or prints another version, the shell refuses every
call a guard would judge, and `inspect`, with a reason that names both
versions and `jhon sync`. Calls no guard reads pass, as they do when
`jhon.json` is missing.

## What the binary provides

| Surface | Shape |
|---|---|
| `jhon hook <guard> [args]` | The guards, through `pre-tool --agent * <guards…>` (one process for `safety-guard`, `vcs-guard <vcs dirs>`, `framework-guard <protected paths>`, `secrets-guard`, `route-guard`, and `scope-guard`), and `inspect-guard <allowed prefixes>`. stdin is the call as a Claude Code PreToolUse payload (`tool_name` `Bash`, `Write`, or `Edit`, `tool_input`, `cwd`; a child's carries `agent_id`; a bounded child's carries its launch prompt). Exit 0 allows the call, unless stdout holds a `permissionDecision` of `ask`, whose `permissionDecisionReason` goes to the owner. Exit 2 refuses, with the reason on stderr. Any other exit is a failure, and the shell refuses the call. |
| `jhon profile` | Run as given by `/jhon:profile <args>`, without blocking pi; the text output is shown to the owner. |
| `jhon status [<change>] --json`, `jhon risk --json` | Read without blocking pi by `/jhon:status [<change>]`, which opens the LDD detail (alt+j: the change and its tests in one view, no tabs), never a toast. It shows the change, its route (`standard route` or `verified route`), a risk row (`risk --json`'s `tier` and its top three files above passive, else `route.tier`), and the last run in one vocabulary (`✓ passed`, `✗ N failed`). Its keys: `t` runs the change's tests in the same view, `d` opens the changes overlay on the change's worktree (`/jhon:changes <dir>`), `o` opens its `tasks.md` in `$VISUAL`/`$EDITOR`, or says its path when neither is set. |
| `jhon mode [--root <path>] --json` | `{"mode", "root", "since"}`: the owner's permission mode for the project (`normal`, `yolo`, `super-yolo`; `since` is null when none was ever set). `jhon mode <mode> --root <path>` sets it on the owner's `enter` in Customize → Permissions (super-yolo only after the owner's Yes), the only place the shell changes it; the safety guard refuses it from an agent. `jhon status --json` carries the same `mode`. A guard call a mode lets through exits 0 with stdout `{"hookSpecificOutput": {"hookEventName": "PreToolUse", "additionalContext": "note (not a refusal; the call ran): allowed by the owner's <mode> mode: <class>: <what>"}}`, a note for the agent, never a question or a refusal. |
| `jhon dashboard` | `/jhon:dashboard` asks `http://127.0.0.1:7480/` first; only a 200 with `X-Jhon-Dashboard: 1` counts as a Jhon dashboard. When none answers it starts `jhon dashboard --port <port>` (7480 unless the address names another), detached, and asks again every 250 ms for up to 2 s; when it does not start or answer the owner is told so and nothing opens. When one answers, the shell asks its `/api/version` (`dashboardIsStale`): a Jhon dashboard with no `/api/version` (not a 200: a release before it) or whose `binary` is not the real path of this shell's `jhon` is an older build. The shell then runs the same `jhon dashboard --port <port>`, which stops the older one once proven Jhon's and serves in its place, and asks `/api/version` again every 250 ms for up to 6 s; a dashboard not replaced is opened all the same, and the owner is told to run that command in a terminal to see why. When it cannot tell (no `jhon` found, no answer, no `X-Jhon-Dashboard` header), nothing is replaced. Then it opens the browser (`xdg-open`, `open` on macOS) at `#/s/<this session's id>` (the home page in a session with no file), or, with no display, tells the owner the address. It reads no setting: `integrations.dashboard` is retired. `JHON_DASHBOARD_URL` points it at another address (the tests', never the owner's). |
| `jhon sandbox status --json`, `jhon sandbox profile`, `jhon sandbox run` | The OS sandbox around the commands the model chooses (T128b, `lib/sandbox.ts`). At each session start the shell asks `jhon sandbox status --json` once, in the session's folder, and keeps the answer for the session: `mode` (`sandbox.mode`), `can_run`, `reason`, `fix`, and `project` (the folder that holds `.git`, `""` in no repository). With the mode `on`, `bash`, `bash_background` and `monitor` start their command as `jhon sandbox run --project <project> --profile <profile> -- <shell> -c <command>`, with the profile `jhon sandbox profile --project <project> -- <command>` names (T128g: `unsandboxed` for a line whose programs are all git with a network subcommand or gh with a listed subcommand, which `jhon sandbox run` then runs with no sandbox; `work` for any other line and whenever it gives no other answer): the command is one argument, read by no shell before the one inside the sandbox, with pi's configured shell and command prefix; an abort, the timeout or `task_stop` kills its whole process group. `inspect`, which starts the program the model names with no shell, starts it as `jhon sandbox run --project <project> --profile work -- <program> <args>` after `inspect-guard` admitted the command. `bash` is then registered again from pi's own definition: its name, description, schema, prompt lines and renderers are the built-in's. The guards judge the command as the model wrote it first; the sandbox takes only what they let through. With the mode `on` the call is refused, and nothing runs, when `can_run` is false (with the `reason` and the `fix`), when the status gives no answer or no `project`, and in a session in no repository. The same refusal, with the same line, answers `inspect`. With the mode `off` nothing changes: `bash` is pi's built-in tool, no command is wrapped and `inspect` starts its program directly. The owner's `!` commands, the owner's own jobs (the change detail's `t`) and `m365_read` are never wrapped. One pin per session: a session that pinned a project exports it as `JHON_SANDBOX_PROJECT` (the variable `jhon start` sets), which the guards it calls and the children it starts inherit. A sub-agent is a child pi that loads this extension in a folder the model may choose (the `subagent` tool's `cwd`, a worktree of the session's repository): with that variable set it asks `jhon sandbox status --json --project <its parent's project>` and pins that project, never its own folder's; where the folder is gone the status fails and the child's commands are refused. A child with no project to inherit asks in its own folder. A session start that does not end with a pin (the status gave no answer, the mode is off, the sandbox cannot run, no project) removes the variable from the process, so no child inherits an earlier session's project. |
| `jhon contract --json` | The handshake above. |

## Every `jhon` call the shell makes

Each call the shell makes, with the shape it reads back.
`test/contract-calls.test.ts` reads the calls from the source and holds
this table to them: a call missing from it, or a row no call makes, fails.
"Exit" means only the exit code is read (with stderr as the reason on a
failure); "text" means the output is shown to the owner as it is.

| Call | What the shell reads |
|---|---|
| `jhon contract --json` | `{"shell": "<version>"}` (the handshake above). |
| `jhon hook pre-tool --agent * <guards…>` | Every guarded tool call, judged by `safety-guard`, `vcs-guard <vcs dirs>`, `framework-guard <protected paths>`, `secrets-guard`, `route-guard`, and, for a bounded child, `scope-guard`. The hook protocol above; an allowed call's note is stdout's `hookSpecificOutput.additionalContext`. |
| `jhon hook inspect-guard <allowed prefixes>` | The `inspect` tool's command, as a `Bash` payload. The hook protocol above. |
| `jhon hook format-edit` | stdin: an `Edit`/`Write` payload with `cwd`. stdout: `{"hookSpecificOutput": {"additionalContext": string}}`, the note for the agent; anything else, or a failure, is no note. |
| `jhon hook announce-stop` | The main session's settled turn, once per owner turn (T135). stdin: a Claude Code `Stop` payload (`hook_event_name`, `last_assistant_message`, `stop_hook_active`, `cwd`; the shell passes `""` for a reply with no text and no tool call, and the engine reads `transcript_path` only when `last_assistant_message` is absent). stdout: `{"decision": "block", "reason": string}` when the reply announced an action it did not take or was empty (L1063), the reason sent as one follow-up message; empty stdout, any other output, a failure or over 5 s is no send-back. |
| `jhon hook session-brief jhon-orchestrator` | stdin: `{"cwd"}`. stdout: `{"lead": string, "brief": string}`, both non-empty, else no brief. |
| `jhon status --json` | `docs/status-json.md`. It may exit 1 and still print the status: output that parses is the answer. |
| `jhon status [<change>] --json` | As above, for one change, or the one selected. |
| `jhon sandbox status --json [--project <folder>]` | `{"mode": "on" or "off", "can_run": bool, "reason": string, "fix": string, "project": string}`, once per session start, in the session's folder; a sub-agent child passes `--project` with the project its parent pinned, and `project` is then that folder. A failure, no answer in 10 s, or output without `mode` and `can_run` refuses `bash`, `bash_background` and `monitor` for the session. |
| `jhon sandbox profile [--project <project>] -- <command>` | `unsandboxed` or `work` on one line (T128g; a git line is `unsandboxed` only when its folder, its `-C` folders and a clone's target are inside the project), asked in the command's folder before each `bash`, `bash_background` and `monitor` run with the mode `on`. Any other output, a failure or no answer in 10 s runs the command with `work`. |
| `jhon sandbox run --project <project> --profile <profile> -- <shell> -c <command>` | The command's own stdout, stderr and exit code; killed by its process group. The profile is what `jhon sandbox profile` named. A wrapped command that fails on a read-only path ends with one line on stderr, `the sandbox refused a write to <path>; that is a stop: tell the owner the path (jhon settings sandbox.allow_write), never write it elsewhere`, which ends the tool result. `inspect` runs the same call with `--profile work` and its own program and arguments after `--` (`-- <program> <args>`), and reads that program's stdout, stderr and exit code. |
| `jhon risk --json` | `{"tier": string, "reasons": [{"level", "file", "why"}]}`. |
| `jhon executions run <change> --affected <base> --dry-run --json` | `{"projects": [{"project", "tests", "modules"?, "estimate"?: {"tests", "total", "seconds", "all_seconds"}, "last_seconds"?, "checks": [string], "reused"?}], "timeout_minutes"}`: the change's test plan in the LDD detail. |
| `jhon mode --json` | `{"mode", "root", "since"}` (above). |
| `jhon mode <mode> --root <path>` | Exit. |
| `jhon grant --list --json` | `{"root": string, "classes": [string], "grants": [{"class", "root", "granted_at", "expires_at", "live": bool}]}`. |
| `jhon grant <class…> --for <duration>` | Exit: the owner's grant, from the permission question (`30m`) or `/jhon:grants`. |
| `jhon grant --revoke <class>` | Exit. |
| `jhon grant outside-write --session <id>` | Exit: the owner's yes to writes in a folder that is no git repository, for this session (T57a). |
| `jhon grant --revoke outside-write --session <id>` | Exit: the session's folder grants end with it (session_shutdown). |
| `jhon profile json` | `{"active", "file", "profiles": [{"name", "provider"?, "delegation": "inline" or "writer", "agents": [{"agent", "runtime", "model", "source"}]}], "efforts"?: {"<runtime>": [string]}}`. |
| `jhon profile use <name>` | Exit. |
| `jhon profile new <name> --from <profile>` | Exit. |
| `jhon profile set <profile> <agent> <runtime> <model or ->` | Exit. |
| `jhon profile rm <name>` | Exit. |
| `jhon profile mode <profile> <inline or writer>` | Exit: the profile's delegation mode, saved for the next session (T117c). |
| `jhon profile <args…>` | Text: `/jhon:profile <args>`. |
| `jhon settings list --json` | `{"path": string, "settings": [{"key", "value": bool, number or string, "source": "file", "env" or "default", "env"?}], "retired"?: [{"key", "why"}]}`. |
| `jhon settings set <key> <value>` | Exit. |
| `jhon settings migrate` | Exit. |
| `jhon palette --write-pi <agent dir>` | Exit: rose ink rewritten from the desktop palette. |
| `jhon dashboard --port <port>` | Nothing: started detached (above). |
| `jhon review start --json [--base <ref>] [--change <name>]` | `{"subject", "base", "candidate", "lenses": [{"lens", "routing_key", "model", "effort", "prompt", "bytes", "est_tokens"}]}`: the `jhon_review` tool (T118c) runs each lens's prompt as one tool-less completion on exactly that model and effort. Exit 1 (a prompt over the cap: split the slice) or 2: stderr is the reason, returned as it is. |
| `jhon review refute --json --subject <hash> [--base <ref>] [--change <name>]` | stdin: the lens results (a JSON array). `{"prompt", "bytes", "est_tokens", "model"?, "effort"?}`: the refuter's prompt and its review-refuter model; no model means its agent runs instead. Exit 1 (a subject mismatch, an unreadable result): stderr is the reason. |
| `jhon review validate --json --subject <hash> [--base <ref>] [--change <name>]` | stdin: `{"lenses", "refuter"}`. As refute, for the validator (review-validator). |
| `jhon review record --json --subject <hash> [--base <ref>] [--change <name>]` | stdin: `{"lenses", "refuter", "validator"}` (an unanswered lens as `inspection.status` `unavailable` with its reason). `{"verdict", "correction_set", "for_verify", "follow_ups", "information", "refuted", "unavailable", "record", "subject", "base", "lenses"}`: the tool's compact result. Exit 1 or 2: stderr is the reason. |

## What the render writes for the shell (`~/.pi/jhon/`)

| File | Shape |
|---|---|
| `jhon.json` | `jhon` (the release's binary), `orchestrator` (the main prompt's file), `vcsDirs`, `protected`, `inspectAllow` (string lists). |
| `jhon-orchestrator.md` | The text appended to the main session's system prompt. |
| `agents/<name>.md` | A frontmatter block (`name`, `description`, `model`, optional `thinking`, optional `bounded: true`, a `tools` block list of pi tool names), then the agent's prompt. |
| `settings.json` | Lists `<release>/shell` among its packages, so pi loads this package, and names its theme, `rose-ink`, which `jhon palette` writes under the agent directory's `themes/` (seeded by the render, rewritten by `lib/rose-ink.ts` when the desktop palette changes). |

`/jhon-ldd` reads the release's own `runtimes/claude-code/plugin/skills/jhon-ldd/SKILL.md`
(beside `shell/` in the release): its body is the prompt, its action table the
completions. The render writes no prompt template; `prompts/.jhon-owned` only
prunes the `prompts/jhon-ldd.md` an earlier release wrote.

## What the shell owns

The tools (`subagent`, `ask_user`, `inspect`), the
commands, the desktop poster, and the child processes. Its
environment variables (`JHON_PI_CHILD`, `JHON_PI_AGENT`, `JHON_PI_TOOLS`,
`JHON_PI_TASK_FILE`, `JHON_PI_SESSION_PROFILE`) are internal to it: the binary
reads none of them. So is a session's own profile (T120): the shell keeps it
in the session (`jhon-shell.session-profile/v1`) and reads that profile's pi
rows from `jhon profile json`; the binary knows one default profile.

`jhon status --json`, which the rail's LDD section and the LDD detail read, is a contract of its own: `docs/status-json.md`.

## What the shell records of its own use

The shell appends a `jhon-ui-use` session entry (`pi.appendEntry`, kept out
of the model's context) each time a person uses its UI (`lib/ui-use.ts`):
`{ "type": "custom", "customType": "jhon-ui-use", "data": { "feature": <name> } }`.
`feature` names what ran: `/<command>` for a command (`/jhon:agents`),
`key <key>` for a shortcut (`key alt+u`), and `click <view>` for a click the
shell handles that opens a view: `click agents`, or `click jobs`, for each
request on `jhon-shell:open-tasks` to open the Agents or the Jobs view (the
narrow work row's tap while sub-agents lead it), and `click changes` for the
rail's CHANGES row. A header click that only folds a section, and every pointer move, are
not recorded. The entry records no arguments, text or paths; a session with
no file to record in loses it, and the feature runs all the same.
`jhon audit --all` reads these entries from pi's session files and counts
each feature's runs across the sessions (`ui_use` in its JSON).

## Events for other extensions

The shell knows no multiplexer, terminal or desktop by name. It says what
happens on pi's event bus (`pi.events`) under these names, and any other pi
extension may listen, such as a bridge that drives a multiplexer. The names
and payloads are stable: a change to them is a breaking change of this
contract. A listener must ignore fields it does not know.

| Event | Who emits | Payload | When |
|---|---|---|---|
| `jhon:owner-wait` | the shell | `{ active: true, label: string }`, then `{ active: false }` | An agent starts waiting for the owner (`label`: `question` or `permission: <class>, …`, the class only), then the wait settles: answered, dismissed or failed. Exactly one `active: false` follows each `active: true`. Only the main session emits it, never a child. |
| `jhon:palette-changed` | the shell | `{ palette: string }` | The desktop palette the shell follows changed (`integrations.theme.palette` is `desktop`, or `ryoku`, its older name; `palette` is `desktop`). Rose ink is rewritten from it when it is pi's active theme; the event is emitted on every change either way. The shell runs no other program for it. |
| `jhon:host?` | the shell | `{}` | The main session starts (pi's `session_start`): a query, answered with `jhon:host`. |
| `jhon:host` | a bridge | `{ name: string, alerts?: boolean, showsPath?: boolean }` | A bridge announces the host it runs under: when it starts and in answer to every `jhon:host?`. |

What the shell does with `jhon:host`, heard at any time of the session:

- `alerts: true` — the host plays its own alert while the owner is waited
  for (`jhon:owner-wait`). The shell plays no sounds, so it changes nothing
  in the shell.
- `showsPath: true` — the host shows the path, so the title row names the
  project alone (` 力 <project>`); otherwise ` 力 <project>  <~path>`.
- `name` (1 to 128 characters) identifies the host: an announcement with
  the same name replaces the earlier one; several hosts add up (any
  `showsPath: true` drops the path). A payload of
  another shape is ignored.

Load order does not matter: a bridge loaded before the shell answers its
`jhon:host?` at session start; a bridge loaded after it announces on its own
start. A bridge should do both.
