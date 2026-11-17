# tools/bench

The maintainer's benchmark of Jhon (T39, `odd/proposals/benchmarks-mvp.md`). It is not part of the release.

It runs synthetic tasks in four conditions and compares the results:

| Condition | Command (in the bench HOME) |
|---|---|
| `jhon` | `jhon start -p <prompt> --mcp-config <bench HOME>/.local/share/jhon-bench/engram-mcp.json --output-format stream-json --verbose --max-turns 60 --model M --permission-mode acceptEdits --allowedTools … --session-id <id>` |
| `clean` | `claude -p <prompt> --safe-mode --setting-sources project,local --strict-mcp-config` with the same flags |
| `pi-jhon` | `jhon start pi --mode json --provider claude-bridge --model M --session-id <id> -- <prompt>` |
| `pi-clean` | `pi --no-extensions --extension <bench HOME>/.pi/jhon/npm/node_modules/pi-claude-bridge/src/index.ts --no-skills --no-prompt-templates --no-themes --no-mcp --mode json --provider claude-bridge --model M --session-id <id> -- <prompt>`, with `PI_CODING_AGENT_DIR=<run>/pi-agent` |

`pi-clean` is pi with no Jhon (L552), as `clean` is Claude Code with no Jhon, so the shell's own contribution can be measured. It runs the `pi` on PATH, not `jhon start pi`, with an agent dir of the run's own that holds only `claude-bridge.json`: no Jhon settings, packages, prompt, agents, guards, skills or MCP servers (Engram included). Every discovery is off and the one extension loaded is pi-claude-bridge, the one `bench prepare` installed in the bench HOME, so it runs on the same provider and model as `pi-jhon`; its `claude-bridge.json` names the same hooks-off Claude Code wrapper (`<bench HOME>/.pi/jhon/claude-code-provider`). Its tools are pi's defaults (read, bash, edit, write); context files (`AGENTS.md`, `CLAUDE.md` of the task repo) still load, as `clean` reads the project's. A bench HOME without the bridge or the wrapper stops the run and says so. A multi-message task runs it with `--mode rpc`, as `pi-jhon`. `pi-clean` is not in the default `--conditions`: name it. Its session lands in `<run>/pi-agent/sessions/`, and it is measured like `pi-jhon` (stream, transcript, `jhon audit`). No real `pi-clean` run has been made yet: that pi loads the bridge from that path with `--no-extensions`, and that the bridge reads `claude-bridge.json` from `PI_CODING_AGENT_DIR`, rest on `pi --help` and the bridge's `src/config.ts`.

A task with several owner messages (below) runs each condition on one session:

| Condition | Owner messages 2, 3 … (T46) |
|---|---|
| `jhon`, `clean` | one `-p` call per message: the first with `--session-id <id>`, each later one the same command with `--resume <id>` in its place. `claude --help` (2.1.310): `--resume` takes a session id and keeps it (only `--fork-session` makes a new one), so the transcript stays `<id>.jsonl`; the two flags are never combined. A later call starts only after the one before exited 0, with `--max-turns` set to the turns the task has left. |
| `pi-jhon` | `jhon start pi --mode rpc --provider claude-bridge --model M --session-id <id>`, no prompt argument: the bench writes one `{"id":"bench-<n>","type":"prompt","message":…}` record per message to stdin and sends the next only after `agent_settled` (pi's `docs/rpc.md`: `agent_end` may still be followed by retries or compaction; `agent_settled` means pi will not continue on its own). A `prompt` response with `"disposition":"handled"` ends the message at once; `success: false` ends the run, except pi's "Agent is already processing", which a background notice's own turn causes: the message is marked `busy` and sent again once that turn settles, at most 3 times. After the last message the bench closes stdin, which shuts pi down. A dialog pi raises (`extension_ui_request` with method `select`, `confirm`, `input` or `editor`, such as the safety guard's ask) is answered at once with `{"type":"extension_ui_response","id":…,"cancelled":true}` (`docs/rpc-extension-ui.md`), as Claude Code `-p` turns a guard's ask into a refusal; `ui_requests_declined` counts them. When the agent settles while a background job it started still runs (a record's `details.jhonTasks.status` is `running`, and no later one for that job says otherwise, such as the shell's `jhon-tasks.notice` message; a `monitor` never holds it, since it watches until stopped), the message stays open until the next `agent_settled`, the job's notice turn; a `bench_message` record with phase `wait` and the job ids marks it. Each job holds a message open once, so a job whose end the stream never shows costs one extra settle at most, and the run's timeout bounds the wait. |

Either way the task's timeout and turn cap cover the whole run, not each message, and `stream.jsonl` holds the bench's own record around each message: `{"type":"bench_message","n":1,"phase":"start","at_ms":0}` before it is sent and `{"type":"bench_message","n":1,"phase":"end","at_ms":…,"reason":"settled"}` when it settled (or `handled`, `rejected`, `exit <code>`, `timeout`, `turn_cap`). A single-message task runs exactly as before, with no such records.

Both Claude Code conditions pass `--allowedTools Bash,Edit,MultiEdit,Write,Read,Glob,Grep,Agent,Monitor,TaskOutput,TaskStop`: the agent and background tools (names from the claude 2.1.310 binary; Bash itself runs in the background), for parity with the shell's `subagent` and `bash_background`.

**Isolation of `jhon` (T146 FB4).** `clean`'s `--safe-mode` drops every CLAUDE.md and the auto-memory; `jhon` cannot take it (it drops `--agent`, SPIKE.md R4), and `--setting-sources` does not keep out a CLAUDE.md found on the way up from the run's repo: the results live under your HOME, so your `~/.claude/CLAUDE.md` loaded as a "Project" memory (SPIKE.md R3). The `jhon` runs therefore get `CLAUDE_CODE_DISABLE_CLAUDE_MDS=1` and `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1` (read by Claude Code 2.1.323), the dry run shows them, and no run inherits `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD`. Jhon's own text is in its plugin, never in a CLAUDE.md, and no task ships one.

Every run uses the bench HOME (default `~/.local/share/jhon-bench/home`, `--home` to change it), never your own `~/.claude`, `~/.config/jhon` or `~/.pi`. Each run gets a fresh git copy of the task's template (with a repo-local identity, `bench <bench@localhost>`, so the agent's commits work, and `__pycache__/` in its `.gitignore`), its own `XDG_CACHE_HOME`, a fixed session id, and the task's timeout and turns (30 minutes and 60 turns for the six short tasks, 40-45 minutes and 80-90 turns for the long ones).

### Any pi model (T77)

Jhon must work the same on any model, with more iterations allowed (L582), so the pi conditions take any pi provider: `bench run --pi-model PROVIDER/MODEL[@THINKING]` (for example `zai/glm-5.3`, `zai/glm-5.3@high`, `openai/gpt-5`) runs `pi-jhon` and `pi-clean` with `--provider PROVIDER --model MODEL` (and `--thinking THINKING`) in place of `--provider claude-bridge --model M`; the Claude Code conditions keep `--model`. With `--pi-model zai/glm-5.3`:

| Condition | Command |
| --- | --- |
| `pi-jhon` | `jhon start pi --mode json --provider zai --model glm-5.3 --session-id <id> -- <prompt>` (`jhon start pi` hands its arguments to pi as they are, on the bench HOME's `~/.pi/jhon`) |
| `pi-clean` | `pi --no-extensions --no-skills --no-prompt-templates --no-themes --no-mcp --mode json --provider zai --model glm-5.3 --session-id <id> -- <prompt>`, with `PI_CODING_AGENT_DIR=<run>/pi-agent` (empty: no extension, no `claude-bridge.json`, no bridge needed in the bench HOME) |

Without `--pi-model` both run on claude-bridge exactly as before (unless the bench HOME has a profile, below). Only the main pi session takes the named model: `pi-jhon`'s sub-agents (explore, workers, verify) run the tiers of the bench HOME's active Jhon profile; to measure a whole profile, install one (next section).

A built-in provider reads its key from the environment. `--pi-env NAME[,NAME]` names the variables the pi runs need (`--pi-env ZAI_API_KEY`): a run refuses to start when one is unset, and the pi runs get them from the bench's own environment. The bench never prints, logs or writes their values: the dry run (`pass env: ZAI_API_KEY`), `metrics.json` (`pi_env`) and the reports carry the names only. Default: none. (The bench environment already keeps every parent variable it does not strip, so `--pi-env` is the check and the record, not a filter.)

The model is recorded per run: `metrics.json`'s `model` is the effective one, `PROVIDER/MODEL[@THINKING]` for the pi conditions (`claude-bridge/M` without `--pi-model`) and `M` for the Claude Code ones. The default results dir is named for the time and the pi model (`20261113T010203Z-zai-glm-5.3`). A cell is one condition on one model: `bench report` never merges runs of two models (with several in a dir, each row names its model after the condition), `bench value` compares `pi-jhon` with the `pi-clean` of the same model and gives one runtime line per model (`runtime pi zai/glm-5.3 (pi-jhon vs pi-clean)`), and `bench certify` refuses a cell whose model differs between the baseline and the candidate (`… the baseline ran claude-bridge/opus, the candidate zai/glm-5.3; certify compares a model with itself`) unless `--allow-model-change`, which compares them and notes each change. `claude-bridge/M` and `M` are the same model; a run with no model (an old `report.json`) matches any.

### A whole Jhon profile (T77b)

A Jhon profile picks every agent's model per runtime, the orchestrator's and each sub-agent's (explore, workers, verify). To measure a whole profile (only GLM, GLM with DeepSeek, mixed with Claude, only OpenAI), install it in the bench HOME:

```sh
go run ./tools/bench prepare --pi-profile ~/.config/jhon/profiles.json --profile glm
```

After the install, `prepare` copies the named file (read once, never written) to the bench HOME's `~/.config/jhon/profiles.json` (where `jhon profile` reads it: `internal/profiles.Path`, under `HOME`) and runs the bench HOME's own `<bench HOME>/.local/bin/jhon profile use glm` there, with the bench environment (`HOME`, `XDG_CONFIG_HOME` and the rest naming the bench HOME; `JHON_PROFILES` removed). `profile use` sets the bench HOME's `pi.profile` setting and re-renders every runtime with the profile's models, sub-agents included; it installs pi-claude-bridge only when a pi model of the profile runs on `claude-bridge`, so an all-GLM profile needs no bridge (and no Claude login for the pi conditions). `--profile NAME` alone activates a profile already in the bench HOME's file; `--pi-profile` needs `--profile`. Your own `~/.config/jhon` is never touched.

The file is `internal/profiles`' format: a profile overrides a runtime's tiers (`tiers.<runtime>.<cheap|mid|strong|frontier>`) or one agent (`agents.<agent>.<runtime>`), each `model` or `model@effort`; pi models are `provider/model`. An all-GLM pi profile:

```json
{
  "profiles": {
    "glm": {
      "tiers": {
        "pi": {
          "cheap": "zai/glm-5.3-flash",
          "mid": "zai/glm-5.3-flash",
          "strong": "zai/glm-5.3",
          "frontier": "zai/glm-5.3"
        }
      }
    }
  }
}
```

`bench run` reads the bench HOME's active profile (its `pi.profile`, else `profiles.json`'s `active`; `default` is none) and:

- records it in each Jhon run's `metrics.json` (`"profile": "glm"`; `jhon` and `pi-jhon` only, `clean` and `pi-clean` run no Jhon), and prints it (`profile: glm (the bench HOME's active Jhon profile: the sub-agents of jhon, pi-jhon)`), on the dry run too;
- without `--pi-model`, runs the pi conditions' main session on the profile's orchestrator model, read from the bench HOME's rendered `~/.pi/jhon/settings.json` (`defaultProvider`/`defaultModel`[`@defaultThinkingLevel`]): `pi main session: zai/glm-5.3, profile glm's orchestrator model`. `pi-clean` takes the same model, so `bench value` has its counterpart;
- with `--pi-model`, that model sets the main session only, and says so: `--pi-model zai/glm-5.3-flash sets pi-jhon's main session only; its sub-agents run profile glm's models`. The Claude Code `jhon` condition keeps `--model` for its main session; the profile's `claude-code` tiers set its sub-agents.

The profile is part of a cell's model identity, written `MODEL profile NAME` (`zai/glm-5.3 profile glm`): `bench report` never merges two profiles' runs, `bench value` keeps one cell and one runtime line per model and profile and compares each with the `pi-clean`/`clean` cell of its main model, and `bench certify` refuses a cell whose profile changed (`… the baseline ran zai/glm-5.3, the candidate zai/glm-5.3 profile glm; certify compares a model with itself`) unless `--allow-model-change`. A bench HOME with no profile (or `default`) runs, records and prints exactly as before.

### Project settings

`bench run --project-setting KEY=VALUE` (repeatable or comma-separated) sets a project setting the product reads in each run's LDD config, after the repo is laid out and before the agent starts; it is committed with the template, as the variant's config is (T86, L615). The only key is `worktree_per_change` (`true` or `false`; engine/internal/ldd/config.go, docs/ldd.md "A worktree per change"); an unknown key, another value or a key given twice is refused. A setting applies to the variants whose repo has a config at the start, `ldd` (`openspec/config.yaml`) and `ldd-native` (`ldd/config.yaml`), where the line `worktree_per_change: true` is appended (or replaces the key's line); naming `plain` or `ldd-ask` (the default variants include `plain`) is refused: `--project-setting sets the task repo's LDD config; variant plain has none at the start (it applies to ldd, ldd-native)`.

The settings are recorded per run (`metrics.json`'s `project_settings`), the dry run and the batch print them, and the default results dir names them (`20261113T010203Z-worktree_per_change-true`). They are part of a cell's model identity, written `MODEL with KEY=VALUE` (`opus with worktree_per_change=true`): `bench report` and `bench value` never merge runs with and without a setting (value compares each with the same no-Jhon counterpart), and `bench certify` refuses a cell whose settings differ between the baseline and the candidate (`… the baseline ran project settings none, the candidate worktree_per_change=true; certify compares cells with the same project settings`) unless `--allow-setting-change`, which compares them and notes each change. Without the option, plans, repos and metrics are exactly as before.

The targeted run with a worktree per change on (L615):

    go run ./tools/bench run --variants ldd,ldd-native --conditions jhon,pi-jhon --repeats 3 --model opus --project-setting worktree_per_change=true

With the setting on, the agent works a change in `<repo>-worktrees/<change>` outside the repo and delivers it to the default branch; the delivered tree is then the main checkout and the change's record is found under the git common dir (tree.go, T82b), whether the worktree was removed or left.

### Engram in both Jhon conditions

Both Jhon conditions carry engram, as in the owner's real use (L424), with the same server and tools, each run with a store of its own:

- **pi-jhon** gets it from `<bench HOME>/.pi/jhon/mcp.json`, which the release renders: `engram mcp --tools=agent`, a stdio MCP server.
- **jhon** gets it through `--mcp-config`, because `jhon start` refuses `--plugin-dir` (it sets its own). Before the first `jhon` run, `bench run` writes `<bench HOME>/.local/share/jhon-bench/engram-mcp.json` from pi's `engram` entry, minus pi's own `exposure` key. Its tools are named `mcp__engram__*`; the owner's plugin names them `mcp__plugin_engram_engram__*`, with the same schemas. The `ENABLE_CLAUDEAI_MCP_SERVERS=false` that `jhon start` sets drops only claude.ai connectors, so this server stays. With no `engram` in pi's mcp.json, the run stops and says so.
- The config holds the MCP server only. The real engram plugin's hooks call an HTTP server on `127.0.0.1:7437`, which is the owner's, so the bench leaves them out.
- Every run's env sets `ENGRAM_DATA_DIR=<run>/cache/engram` and drops every inherited `ENGRAM_*` variable (`ENGRAM_URL`, `ENGRAM_PORT`, `ENGRAM_CLOUD_*`, …). Each run therefore starts with an empty store of its own, so no run recalls what an earlier one saved, and never touches the owner's `~/.engram`. The stdio server opens that store directly and starts no HTTP server.

## Tasks

`tasks/<id>/task.yaml` holds the id, kind, `size` (`small` or `large`, T146: it picks the task's `bench value` gates and default repeats), prompt, default model, `max_turns`, `timeout`, the hidden test's directory and the `check` command (exit 0 is success). It also names the test files' pattern (`tests`, default `test_*.py`), the project's test command as an agent runs it (`test_command`, default `python3 -m unittest`) and the `scope`: one `path.Match` pattern per line, the files a correct solution may touch. The agent works in a copy of `tasks/<id>/template`. The hidden test (`tasks/<id>/hidden`) is copied in only after the run. The reference solutions live in `testdata/solutions` and the trap tasks' naive solutions in `testdata/naive`; neither is ever copied into a run.

| Task | Kind |
|---|---|
| `pagination-bug` | a small bug |
| `shipping-rules` | a function with 3 rules |
| `account-import` | a risky change: validating imported data |
| `search-accents` | trap: a hidden regression |
| `contacts-migration` | trap: a risky data change |
| `late-fee` | trap: an ambiguous rule the repo's docs resolve |
| `invoice-slices` | long: 3 owner messages, a requirement change in message 2 |
| `slow-suite` | long: 2 owner messages, a ~90-second full suite |
| `two-modules` | long: 2 independent modules whose rules are only in `docs/` |

`pagination-bug`, `shipping-rules` and `account-import` are `size: small`; the trap and long tasks are `size: large`.

**The one-time set-up (T146 FB2).** `setup/ldd-setup` is a tiny repo with no LDD (one module, one test) and the prompt "Set this repository up for LDD. Change no code and no test."; its hidden check wants `ldd/config.yaml` (or `openspec/config.yaml`) and the code unchanged. A full `bench run` (no `--tasks`) adds it, and `--tasks ldd-setup` names it: it runs once per Jhon condition (`ldd-setup-plain-jhon-1`, `ldd-setup-plain-pi-jhon-1`), plain, on a fresh repo, whatever `--repeats` says, never under `clean`, `pi-clean` or `--smoke`. `bench report` and `bench value` show it on its own line (`one-time set-up …`) and `report.json`/`value.json` under `setup`; it is in no cell, no sum and no certification cell.

The first three are easy: in the first full run every condition passed 45/45 (L397), so they measure Jhon's cost, not its value. The trap tasks (T40) are the ones a hasty agent plausibly fails and a careful one (reading the repo, running the whole suite, a test first) gets right:

- **`search-accents`.** The ticket asks search to ignore accents. The obvious fix, accent folding in the shared `textutil.normalize`, breaks `handles.handle_key`, where "José" and "Jose" must stay two handles. Only `test_handles.py`, run with the whole suite, or reading `normalize`'s other caller shows it. The hidden test checks the fix and the handles.
- **`contacts-migration`.** The ticket asks for a schema 1 → 2 migration of `data/contacts.json`. `docs/DATA.md`, linked from the README and not repeated in the prompt, requires keeping unknown fields, a `<file>.bak` backup, an atomic write and idempotence. The hidden test checks all four on a fresh file, then that `store.py` reads the result.
- **`late-fee`.** The ticket says "a 5% fee". `docs/RULES.md` says a percentage applies to the balance (amount minus credits) and rounds half up with `money.percent_of`, never `round()`. The hidden test follows the docs: 5% of 10,100 cents of balance on a 20,000 invoice is 505, and 5% of 1,050 is 53.

### Long tasks (T46)

The six short tasks hit the model's ceiling (every condition passes), so they measure cost, not value. The owner's frame (L436, L437): the shell's plus shows in the **quality of the final result**, not in context size. The long tasks are built so that method decides the result: several owner messages, a requirement change that must not break what came before, rules only in the repo's docs, a slow suite.

A multi-message task has `prompt_1: |`, `prompt_2: |` … in place of `prompt:` (both together is an error, and so is a gap in the numbers or an empty message). It may name a check per message, `check_1:`, `check_2:` …: after message *n* settles, the bench runs every `check_k` with *k* ≤ *n* on a copy of the repository with the hidden directory in it (the agent never sees it), so a check of message 1 that fails after message 2 is a **regression**. Their output goes to the run's `checks.txt`.

- **`invoice-slices`** (3 messages, 90 turns, 45 min). 1: a CSV export (`export_csv`, `cli.py export`) with columns `id, customer, issued, amount_cents` in that order, rows by date then id, half-up cents. 2: the requirement changes: `amount` with two decimals, half up (`10.005` → `10.01`, which `round(float)` gets wrong), in the same position; "everything else stays". 3: `--since YYYY-MM-DD`, inclusive; a bad date exits 2 and writes no file. `check_1` (`test_m1`) checks what must last from message 1 (the command, column and row order, quoting) and so catches a message 2 or 3 that reorders the columns; `check_2` and `check_3` check each message's own rule; the final check is all three.
- **`slow-suite`** (2 messages, 80 turns, 40 min). 1: two bugs (a refund counted as money in; an account matched by prefix, `acme-1` taking `acme-10`'s postings) that the fast unit tests miss and the full suite, `python3 run_suite.py`, shows. The suite is deterministic: 30 scenarios of 3 seconds, about 90 seconds in all, which is where running it while working on something else pays. 2: a new rule, voided entries count nowhere, and the suite must still pass. `check_1` keeps checking both fixes after message 2.
- **`two-modules`** (1 message, 80 turns, 40 min). An importer (a `;`-separated supplier CSV with European prices, `1.234,56`) and an exporter (JSON Lines in a fixed key order), which do not depend on each other, then the wiring of both into `cli.py convert`. Every rule is in `docs/IMPORT.md` and `docs/EXPORT.md`, none in the prompt: a sku upper-cased and a later row replacing an earlier one, `1.000` meaning one thousand, more than two decimals rejecting the row, tags sorted and deduplicated, free samples left out of the export, text written as it is.

No prompt names a tool (a test refuses `agent`, `background`, `monitor`, `bash` …), and none names a hidden test. The names are neutral (Acme, Globex …).

The tasks are Python with the standard library only. `go test ./tools/bench` proves, for each task, that the visible tests pass on the template, the hidden test fails on it and passes on the reference solution. For each trap it also proves that the hidden test fails on the naive solution, and that both solutions stay in the task's scope. For each long task it proves the same at every reference state, `testdata/solutions/<id>/<n>/` after message *n* (applied in turn over the template): the visible tests pass, every `check_k` with *k* ≤ *n* passes, `check_n` failed at the state before, every file a state writes is in scope or a test, and the hidden test passes at the last state. For `slow-suite` it also runs the full suite with its delay set to 0 in a scratch copy: 15 of 30 scenarios fail on the template, none at either state.

## Variants

Each task runs in one of four variants (L395, L483), chosen with `--variants`. `bench run` takes `plain,ldd` when none is named; `certify --plan` names all four.

| Variant | The agent's repository |
|---|---|
| `plain` | the template as it is; under `jhon` and `pi-jhon` the template plus the LDD set-up, `ldd/config.yaml` from `ldd/steady-config.yaml` (what Jhon's init writes with the recommended values: `strict_tdd_policy: require`, `test_command_override` `"."`: `python3 -m unittest`, a short `context`), committed with the template before the agent starts: the steady state of a project after day one (T146 FB2). The run must still end with `ldd/` and its record (L554-L559). metrics.json says `steady_state: true`. |
| `ldd` | the template plus `ldd/config.yaml` as `openspec/config.yaml` and `ldd/pyproject.toml` at its root |
| `ldd-native` | the template plus the same config at `ldd/config.yaml` (Jhon's native layout, no `openspec/`) and the same `pyproject.toml` |
| `ldd-ask` | the plain template; the first owner message starts with **"Use LDD for this work."** and a blank line |

`ldd/config.yaml` turns Jhon's LDD on: `strict_tdd_policy: require`, and `test_command_override` makes the project `.` testable with `python3 -m unittest`. Jhon discovers a Python project only through `pyproject.toml`, so the variant adds a minimal one. `go test ./tools/bench` proves that `jhon status --json` in an `ldd` copy reports `strict_tdd` true with evidence `ok` and the project testable, and that a `plain` copy has no `openspec/`. For `ldd-native` it proves that `jhon status --json` reports `ldd.active` true and `strict_tdd` true with evidence `ok` from `ldd/config.yaml`, and that the copy has the native layout. The project's testability is `unknown` there today: the engine reads `test_command_override` only from `openspec/config.yaml` (`engine/internal/discover/projects.go` `ConfigFor`), a known gap outside the bench. For `ldd-ask` it proves that the copy has no `openspec/`, no `ldd/` and no `pyproject.toml` before the run, and that LDD is not active there.

`ldd-ask` tests the owner's rule (L483): Jhon creates `ldd/` in a repo without `openspec/` "Cuando le pides LDD" (`/jhon-ldd init` or "usa LDD"), without asking further. The sentence is the same for every task and goes before the first message only; later messages are unchanged.

The hidden test still decides success.

Jhon always works under LDD (T72, L554-L559: "siempre usar ldd, si quieren trabajar sin ldd que usen un agente limpio"): in a repo without a layout it sets `ldd/` up with the recommended config, asking the owner once how they deliver and test; a run that cannot ask, like the bench's, takes the recommended values. So a `plain` run under `jhon` or `pi-jhon` is judged like `ldd-ask`: it must end with the `ldd` layout and its record, and what the set-up writes under `ldd/` is not out of scope.

`clean` and `pi-clean` run only the `plain` variant, since a runtime without Jhon ignores `openspec`; they are the no-Jhon baseline (L552) and their plain repo is judged as before. `jhon` and `pi-jhon` run every variant. The plan says so.

The plan is repeat-major: repeat 1 of every task × variant × condition, then repeat 2, and so on. A budget stop then leaves balanced data. Run ids carry the variant: `pagination-bug-ldd-jhon-1`.

## Steps

All commands run from the checkout root.

1. **Prepare the bench HOME.** Build the checkout (`go build -o build/jhon ./engine/cmd/jhon`), then:

   ```sh
   go run ./tools/bench prepare
   ```

   It packs this checkout's release and installs it in the bench HOME the way `test/release/clean_install_test.go` does, without `JHON_ROOT` or `JHON_SETTINGS`. It also creates `.pi` there, so the install renders Jhon's pi profile.

2. **Log in once (your action).** The bench never copies or reads credentials.

   ```sh
   HOME=~/.local/share/jhon-bench/home claude
   ```

   Type `/login`, finish it, and quit. Then check it:

   ```sh
   go run ./tools/bench login-check
   ```

3. **Dry run.** This prints every run, its directories and its command, and starts nothing:

   ```sh
   go run ./tools/bench run --tasks pagination-bug --repeats 2 --model haiku --dry-run
   ```

   Without `--repeats`, each task runs its size's default (T146 FB5): 5 times for a small task, 3 for a large one (or one with no size); the one-time set-up runs once.

4. **Pilot.** 1 task × 3 conditions × 2 repeats, plain variant only = 6 runs:

   ```sh
   go run ./tools/bench run --tasks pagination-bug --variants plain --conditions jhon,clean,pi-jhon --repeats 2 --model haiku --budget-points 10
   ```

   Results go to `~/.local/share/jhon-bench/results/<timestamp>/runs/<task>-<variant>-<condition>-<n>/`: `repo/`, `cache/`, `stream.jsonl`, `stderr.txt`, `check.txt`, `audit.json` and `metrics.json`.

   **The checked tree (T82).** A run's result is the final tree of the change's branch: what would be delivered. Since T80 the agent works a change in a git worktree outside the repository (`<run>/repo-worktrees/<change>`) and leaves it on the change's branch for delivery by PR, so after the run the bench picks the tree it reads: the main checkout `repo/` when it holds the work (its working tree differs from the template commit outside the LDD layout's own files: `ldd/` and `openspec/` and ignored files do not count, T82b, L605, as an agent sets LDD up in the main checkout and works the change in its worktree); otherwise the one worktree of the repository (`git worktree list`) whose branch has commits beyond the template commit or uncommitted changes; the main checkout when none holds work. When several worktrees hold work none is checked: the hidden check FAILs with a note naming them (branch and path), in `check.txt` and `problems`, and the other facts are read on the main checkout. The hidden check, `changed_files`, `out_of_scope_files`, `test_changed`, the ceremony's layouts are read on the checked tree (the LDD layout and record as `ldd_layout` says); a worktree directory is never a changed file. `metrics.json` records `checked_tree` (`main`, `worktree:<branch>` or `none`) and `check.txt` starts with `checked tree: …`. The per-message checks of a multi-message task still copy the main checkout.

   `go run ./tools/bench recheck [--home DIR] <results dir>` recomputes, with no model call, the hidden check and those tree facts of every finished run in place (the transcript facts are read again from the run's transcript): each run's old `metrics.json` and `check.txt` are kept beside as `*.before-recheck`, and it prints each run's success before and after. It repairs a run's worktree links first (`git worktree repair`), so a copied results directory reads its own worktrees, and skips a run whose worktree lies outside its directory. It writes into the results directory: run it on a copy to keep the originals untouched.

   The budget stop: the first weekly reading (the `rate_limit_event`'s `seven_day` utilization) comes from the batch's first run. Before each later run, the batch stops and says so if the latest reading is more than `--budget-points` above the first one.

5. **Full run.** With no `--tasks`, every task runs: the 6 short and the 3 long tasks × (1 clean + 2 jhon + 2 pi-jhon) × 3 repeats = 135 runs. The 6 short tasks alone, named with `--tasks`, are 90 runs:

   ```sh
   go run ./tools/bench run --repeats 3 --model opus --budget-points 10
   ```

   The 3 trap tasks only, 45 runs:

   ```sh
   go run ./tools/bench run --tasks search-accents,contacts-migration,late-fee --repeats 3 --model opus --budget-points 10
   ```

   Add `--dry-run` first to see the plan. The budget stop still applies: the plan is repeat-major, so a stop leaves whole repeats.

   **In parallel (T83).** `--parallel N` executes up to N runs at once (default 1: one after another, exactly as before). Each run already has its own directory, repo, cache, engram store and session id; the engram MCP config is written once before the first run, and the bench HOME's jhon, settings and pi agent dir are only read (Claude Code and pi name their session files by the run's repo and session id). Each output line is written whole, never mixed with another run's, but the order follows the runs: a run's `run i/n <name> ...` line when it starts, and `run i/n <name>: success=…` when it ends, so results come in the order runs finish. Each run's `metrics.json` is in its own directory, and `bench report` reads them in name order, whatever order they finished in. The budget stop checks before each launch against the readings of the runs finished so far; once over the limit, no new run starts and the running ones finish. `--dry-run` shows the parallelism.

   **The smoke tier (T83, L601).** `--smoke` keeps one plain run, repeat 1, per task and Jhon condition among `--conditions` (`clean` and `pi-clean` are dropped): `--conditions jhon,pi-jhon --smoke` is 9 tasks × 2 = 18 runs. They write results like any run. The command exits 1, naming the runs and why, when any of them fails its hidden check or does not end with `ldd/` and a record (the plain set-up rule of certify); else 0.

   **The recommended sequence.** Gates first (`certify --gates`), then the smoke tier into its own results directory, then the full run, in parallel, only when the smoke exits 0:

   ```sh
   go run ./tools/bench run --conditions jhon,pi-jhon --model opus --smoke --parallel 4 --results ~/.local/share/jhon-bench/results/<name>-smoke \
     && go run ./tools/bench run --conditions jhon,pi-jhon --variants plain,ldd,ldd-native,ldd-ask --repeats 3 --model opus --parallel 4 --results ~/.local/share/jhon-bench/results/<name>
   ```

   **The prompt dump.** `--debug-prompt` keeps what each Jhon run sends, in `runs/<run>/prompt-dump/`:

   - **pi-jhon:** the run gets `CLAUDE_BRIDGE_DEBUG=1` and `CLAUDE_BRIDGE_DEBUG_PATH=<run>/prompt-dump/claude-bridge.log`. pi-claude-bridge writes its debug log there, with the full prompt, and writes its Claude Code subprocess logs beside it in `cc-cli-logs/`. The bridge writes straight into the run, so nothing is copied, and runs never share a log.
   - **jhon:** Claude Code gets `--debug-file <run>/prompt-dump/claude-debug.log`. This is Claude Code's own debug log, the same one the bridge asks its subprocess for. It has not been checked yet whether that log holds the whole system prompt.
   - **clean:** nothing.

   Without the option, the bench removes any inherited `CLAUDE_BRIDGE_DEBUG` and `CLAUDE_BRIDGE_DEBUG_PATH`, so no run dumps anything. A one-run debug check:

   ```sh
   go run ./tools/bench run --tasks account-import --variants plain --conditions pi-jhon --repeats 1 --model opus --debug-prompt
   ```

6. **Report.**

   ```sh
   go run ./tools/bench report ~/.local/share/jhon-bench/results/<timestamp>
   ```

   It prints, **quality first** (L436), then what it cost, and writes the same in `report.json` (`{"rows": […], "by_condition": […], "overhead": […]}`) in that directory. A run recorded before variants counts as `plain`; a run recorded before T41 or T46 shows `—` where a fact is unknown.

   1. **Results**, per task × variant × condition: passed (the final hidden check, runs that passed / runs), final checks (the per-message checks run after the last message, passed / run, summed over the runs) and regressions (summed). Each row's `quality` in report.json.
   2. **Methodology fidelity** (L437), per task × variant × condition: `test_changed`, `red_before_green` and `tests_run_before_end` as runs where it holds / runs that know it, the median of `out_of_scope_files`, the findings by check summed over the runs, regressions, rework and the pi dialogs declined (`ui_requests_declined`, summed). Each row's `process` and `quality`.
   3. **By condition**, every task together, per condition × variant: the results and the fidelity summed. Read the results across every condition against `clean` (does the methodology bring better results than a clean runtime?) and the fidelity of `jhon` against `pi-jhon` (which runtime runs the methodology more fully?). `by_condition` in report.json.
   4. **Tokens and time**: each cell's median with its spread, `median (min-max)` (T146 FB5; report.json's `spread`, `[min, max]` per measure), of turns, input, output, cache-read and cache-write tokens, tokens processed (cache read + cache write + input + output), cache hit, active, elapsed and wall time, agents launched, verify runs, findings, quota points, `cost_weighted` (tokens weighted by the run's provider's prices, `cost.go`) and `cost_usd` (only when the stream gave one). Each row's `medians`.
   5. **Per message**, for multi-message tasks: the medians of each message's turns, tokens processed, cache-write tokens and wall time, over the runs that reached it (a cache write that stays flat after message 1 is the shell's cache reuse). Each row's `message_medians`.
   6. **Overhead vs clean**: for each task, every condition × variant's median tokens processed and active time as a ratio to the task's clean plain median (for example `4.5×`).

## What the metrics come from

- **Success:** the hidden test's exit code.
- **Turns, output tokens and cost:** the stream. For Claude Code that is the `result` event. For pi it is the `turn_start` events and the assistant `message_end` usage.
- **Input, cache-read and cache-write tokens, active and elapsed time, cache hit, agents, verify runs and findings by check:** `jhon audit <transcript> --json`, run with the run's `XDG_CACHE_HOME`.
- **Quota points:** the change in `seven_day` utilization since the previous reading.

### Process quality (T41)

Each is `null` in metrics.json when it is unknown, as in a run recorded before them; the report counts unknown apart from false.

- **`test_changed`:** a file matching the task's `tests` pattern was added or changed. Source: the run repo's changes since its start commit (`git diff --name-only <start>` plus untracked files), read before the hidden test is copied in. `changed_files` lists them.
- **`tests_run_before_end`:** the transcript has a Bash call whose command contains the task's `test_command` or `jhon executions run`, after the last Edit, MultiEdit, Write or NotebookEdit (pi: edit, write). With no edit, any test run counts. A test run started in the background (pi's `bash_background`, Claude Code's Bash with `run_in_background`) counts at the place it was started once its job's end reaches the transcript (pi: the `jhon-tasks.notice` "exited with code N"; Claude Code: the `<task-notification>` with the job's id, its status and exit code); it passed on exit code 0. A job that never reported its end is no run, neither passing nor failing. Edits made through Bash (`sed -i`, `cat >`) are not seen.
- **`red_before_green`:** a test run failed before a later one passed. In an `ldd` run of `jhon` or `pi-jhon`, from `jhon status --json` `tests.red_before_green` in the run repo when the change has a test record; otherwise from the transcript, where a run failed when its result is an error or its output has `FAILED (` or `"status": "fail"`. `red_before_green_source` is `ldd` or `transcript`.
- **`out_of_scope_files`:** changed files that match neither the task's `scope` nor its `tests` pattern (tests are always allowed). `out_of_scope` lists them. Files Jhon itself writes in the repo count too; the list shows which. An LDD variant's record under `openspec/` or `ldd/` is what the variant asks for, so it is never out of scope there.
- **Findings:** the total as before, and findings by check from `jhon audit --json`.

### Quality and per-message metrics (T46)

- **`messages`:** one entry per owner message: input, output, cache-read and cache-write tokens, turns, wall time from its start record to its end record, and why it ended. They come from the stream between the message's `bench_message` records: Claude Code's `result` events (`num_turns`, `usage`), pi's `turn_start` events and assistant `message_end` usage (`input`, `output`, `cacheRead`, `cacheWrite`). A single-message run has one entry over the whole stream. The wall time leaves out the per-message checks, which run between messages.
- **`checks_per_message`:** `{"after": n, "check": k, "passed": …}` for every `check_k` run after message *n*; absent when the task names no check.
- **`regressions`:** the times a check failed after it had passed after an earlier message. `null` when the task names no check.
- **`rework`:** edits (Edit, MultiEdit, Write, NotebookEdit; pi: edit, write) of a file that was already edited in the same owner message and that a passing test run (as for `tests_run_before_end`) has covered since. Every such edit counts. A new owner message (a user message with text, not a tool result) starts over, so a requirement change that edits the same file again is not rework. Bash-made edits are not seen. Each tool call counts once: Claude Code's `api-request-blob` records, which replay earlier records, and a `tool_use` whose id came before are skipped, so a replay is no new edit and does not hide a failed run's result. `null` when the transcript is missing.

### Certification metrics (T60)

- **`ceremony`**, plain runs only (`null` otherwise), recorded but no longer judged by certify (Jhon plain runs set LDD up, L554-L559): what the run did that a repo without LDD never asks for, by kind: `layouts` (an `openspec/` or `ldd/` that exists at the end), `executions`, `archive` and `deliver` (Bash calls that run `jhon executions`, `jhon archive` or `jhon deliver` at the start of a shell segment: a mention inside quotes does not count) and `branches` (local branches beyond the starting one), with their `total`. A plain run is clean only with a total of 0. `commits` (commits on any branch since the start commit) is recorded beside them and is never ceremony. The owner's decision (L488): "Commits sí, el resto no": a commit per finished, tested slice is an organised delivery; a new branch, `jhon executions`/`archive`/`deliver`, or creating `openspec/` or `ldd/` is ceremony.
- **`ldd_layout`**, LDD variants and the `plain` variant of `jhon` and `pi-jhon`: the layout the run repo has at the end (`openspec`, `ldd`, or `none`); `tasks_md` (a change's `tasks.md` under `<layout>/changes/`, archived ones too); `test_record` (`jhon status --json` has a `tests` record); `route_requires_folder` (`true`, `false` or `unknown`); and `record`. The record follows the change's route. The owner's decision (after L493): "Sí, que siga la ruta": the LDD record is the test record plus whatever the change's route requires, so a change folder and `tasks.md` count only when the route requires them. `route_requires_folder` is `jhon status --json`'s `needs.tasks` (`ldd.NeedsTasks`: a change over the advisory size or at a high tier needs `tasks.md`, so a folder; a small change has none). The layout is read where it is: the checked tree, else the main checkout. The record is the run's change's wherever it lives (T82b, L605): `jhon status --json` in the checked tree when it selects a change; else the run's change is the checked worktree's branch, else every change record created during the run (under the git common dir's `jhon/changes/<name>/` or a layout's `changes/<name>/`; a run that split its work into several changes kept a record only when every one did, L606), and the bench runs `jhon status --json <change>` on the change's branch (or, once that branch is deleted, the commit its `delivery.json`/`execution-record.json` names) in a throwaway clone under the run directory whose default branch is the template commit, with the record copied in; the run's own repository, branches and trees are only read, and the clone is removed after. It is `unknown` when no change can be evaluated that way. `record` is true with a test record and either `tasks.md` or a route that needs no folder. It is false without a test record, or when the route needs a folder and `tasks.md` is missing. It is `null` (unknown) with a test record, no `tasks.md` and an unknown route. The report prints `record x/y` as runs that kept it / LDD runs, with the unknown ones named, and certify fails a cell with any unknown.
- **`slices`**, tasks with per-message checks: the fraction of messages whose check passed after that message (integrable and tested) and failed after no later one. A message the run never reached is a slice that did not hold. `null` without per-message checks.
- **`refusals`:** tool calls whose result is an error with a refusal's text: a hook's denial (`PreToolUse:<tool> hook error:`), a permission rule's (`Permission to use … has been denied`), or the Jhon safety guard's. `null` when the transcript is missing.

report.json carries them per row: `quality.ceremony` (summed), `quality.ldd` (`layouts` counted, `records` of `known`, and `unknown`), `process.refusals` and `process.out_of_scope_files_total` (summed), and the medians `slices`, `refusals`, `rework` and `quota_weighted` (input + 1.25 × cache write + 0.1 × cache read + 5 × output, L432). The fidelity table shows the ceremony and the LDD layout.

The transcripts are in these places:

- `<bench HOME>/.claude/projects/<cwd, every / and . as ->/<session id>.jsonl` for Claude Code;
- `<bench HOME>/.pi/jhon/sessions/*/<timestamp>_<session id>.jsonl` for pi.

## Certify (T60)

The owner's words (L482): "el framework deberia funcionar igual o mejor que antes, no puede emperorar, o sino no se instala, cuales son las metricas, que en un repo sin ldd funcionen los guards igualmente e intente trabajar de todas formas de una manera organizada y agil, con ldd funcione como hoy en dia, si hay openspec funciona normal, sino crea ldd solo, y ademas en mode normal los grants los debe pedir, en yolo solo destroy y en super yolo ni distroy, ademas de que debe seguir la metodologia al 100%, entregables en slices que son integrables y probados, recuerda que es una metodologia agil sin ceremonias... todo esto lo debe certificar el bench antes de instalar."

And the decisions (L483): Jhon creates `ldd/` in a repo without `openspec/` "Cuando le pides LDD" (it creates it without asking further; a repo without LDD keeps working without ceremony); cost margin "15% con 3 corridas" (median of 3 runs per task; quality has no margin; since L567 the 15 % bounds the geometric mean of the cell ratios and discipline is pooled, see the rules below). Certification = T60: `bench certify`, PASS required before `jhon sync`. Superseded for plain repos by T72 (L554-L559): Jhon always sets LDD up, so a repo without LDD no longer exists for Jhon.

### The steps

1. **See the plan.** `go run ./tools/bench certify --plan` prints the two command sequences (prepare, login check, run) and the matrix: every task (the six short ones and the three long ones, `two-modules` among them), conditions `jhon` and `pi-jhon`, variants `plain`, `ldd`, `ldd-native` and `ldd-ask`, 3 repeats, opus: 216 runs. It also prints the matrix for a baseline of the installed build, which has no `ldd-native` or `ldd-ask` to run (`plain` and `ldd` only, 108 runs), and the `bench run` command of each. For each it estimates the quota points and the wall time from the bench's own records under `~/.local/share/jhon-bench/results` (`--results-root`), read only, and prints the method: the weekly utilization's rise over the quota-weighted tokens of the batches with two readings or more gives the points per token; each planned cell's median weighted tokens and wall time come from its own past runs, else the same task and condition's `ldd`, else any variant of it, else every run; opus runs only. It runs nothing and writes nothing.
2. **Run the baseline, then the candidate**, each with its sequence from the plan, with the owner's OK for the quota. The plan prints both exactly, for example:

   ```sh
   go run ./tools/bench prepare --ref 752cb382
   go run ./tools/bench login-check
   go run ./tools/bench run --conditions jhon,pi-jhon --variants plain,ldd --repeats 3 --model opus --expect-commit 752cb382 --results <results>/baseline-752cb382
   ```

   and the same for the candidate with its commit and all four variants. `--baseline-ref` (default: the commit of the `jhon` on PATH, read with `jhon version --json`) and `--candidate-ref` (default: the checkout's HEAD) name the two commits. The bench code is this checkout's (T60's) on both sides, so the baseline's runs carry T60's metrics too; only the jhon in the bench HOME differs.

   - **`bench prepare --ref <commit>`** installs that commit's build instead of the checkout's `build/jhon`: it clones the checkout into a scratch directory (`git clone --shared --no-checkout`, then `git checkout --detach <commit>`), runs `go build -o build/jhon ./engine/cmd/jhon` there, and packs and installs that clone's release exactly as without `--ref`. It is a clone and not a `git archive` because `jhon release pack` archives its source's HEAD and stamps that commit, so the source must be a repository at the commit. The checkout is never changed (no branch, worktree or checkout of its own), and the scratch directory is removed afterwards. `prepare` then prints the bench HOME's `jhon version --json` commit.
   - **`bench run --expect-commit <commit>`** (7 characters or more) refuses to start when the bench HOME's `jhon version --json` names another commit, and says which `prepare --ref` to run; so a candidate run cannot use the baseline's jhon by accident. Every run records the jhon it ran in metrics.json as `jhon_commit` and `jhon_version`, with or without the flag.
3. **Certify:**

   ```sh
   go run ./tools/bench certify --baseline <baseline results> --candidate <candidate results> --gates <checkout>
   ```

   Each side is one or more results dirs (comma-separated or the flag repeated). A dir with `runs/*/metrics.json` is read run by run, and several such dirs make one side together. A dir with only `report.json` is read as its rows; a cell may come from one report.json only, since medians do not merge. Without `--candidate`, the newest dir under `--results-root` that is not a baseline is the candidate. `certify.json` goes to the first candidate dir (`--out` to change it). `--json` prints certify.json in place of the lines. The exit code is 0 on PASS, 1 on FAIL, 2 on a usage or read error.

Certify judges the Jhon conditions (`jhon`, `pi-jhon`) only: `clean` and `pi-clean` cells, on either side, measure the model, not Jhon, so they are left out of every metric (pooled discipline and cost mean included) and a note says how many runs were skipped; `bench value` compares them (L575).

### The rules

Per task × variant × condition, the candidate's cell against the baseline's:

| Metric | PASS when |
|---|---|
| repeats | both cells have 3 runs or more. Fewer on either side is FAIL with "not enough runs", never a silent PASS. A baseline cell the candidate lacks is FAIL ("missing in the candidate"), and so is a candidate cell with no baseline ("no baseline cell"), except the new variants below. |
| final checks | runs that passed the hidden test / runs ≥ the baseline's, and the per-message checks after the last message, passed / run, ≥ the baseline's |
| per-message checks | the median `slices` ≥ the baseline's |
| regressions | 0 in every run |
| rework | rework per run ≤ the baseline's |
| red before green | pooled over every run of every compared cell (below): not worse than the baseline's beyond chance, one-sided Fisher exact p ≥ 0.05. A candidate cell that knows it in no run is FAIL. |
| tests run before end | the same |
| out of scope | the same, counting runs with at least one out-of-scope file (a report from before T73 has only the total or median: 0 counts no run, more counts every run) |
| refusals and declined dialogs | (refusals + pi dialogs declined) per run ≤ the baseline's. Both sides count refusals the same way (L761): each run's are recounted from its `stream.jsonl` (the count metrics.json carries since T105); a run with no readable stream keeps metrics.json's count, and a note says how many runs each side recounted. A Claude Code permission denial of a tool no Jhon guard judges (the bench HOME's engram, which `--allowedTools` leaves out) is no refusal: a note lists it per tool and run. A baseline from before T60 has no refusal count, so the dialogs alone are compared. |
| plain sets up ldd with a record | `jhon` and `pi-jhon` plain cells (Jhon always works under LDD, L554-L559): every run ends with the `ldd` layout and the record its route asks for (none unknown), as `ldd-ask`; `clean` and `pi-clean` plain cells have no such check |
| ldd-ask layout and record | every run ends with the `ldd` layout and the record its route asks for (none unknown) |
| ldd-native active with a record | the same: the `ldd` layout (its config is there, so LDD is active) and the record in every run |
| quota-weighted tokens | the geometric mean, over every judged cell, of the cell's candidate/baseline median ratio ≤ 1.15 (below); `ldd-ask` and the Jhon `plain` cells are informational and left out. A judged cell whose cost is unknown (missing or 0 on either side) is FAIL. |
| turns | the same |
| active time | the same |

**Aggregate cost and pooled discipline (T73, L566, L567).** Certifying the installed build against itself with 3 repeats per cell failed 7 of 13 metrics cell by cell while the aggregates held (L566), so the owner chose (L567):

- *Cost*: per metric, the ratios of every judged cell (candidate median / baseline median) are averaged geometrically; PASS when the mean is ≤ 1.15. Each cell still shows its ratio; a cell over ×1.5 is flagged `review` (certify.json `"review": true`, a `review <cell>:` line under the metric) but does not fail by itself.
- *Discipline*: RED before GREEN, tests before the end and out of scope are pooled over every run, known values only, of every compared cell. Each candidate cell counts against the baseline cell it is compared with: a cell in both against the same cell, a new variant (`ldd-native`, `ldd-ask`) against the baseline's `ldd` cell of its task and condition (so those baseline runs count once per variant compared with them), a Jhon `plain` cell against the baseline's `plain`. The 2×2 table (runs that hold / do not, candidate / baseline) gets a one-sided Fisher exact test (hypergeometric, alternative: the candidate's rate is lower; for out of scope, its rate of runs with out-of-scope files is higher); FAIL only when p < 0.05. Under the metric line every cell where the candidate does worse than its baseline cell is listed.

The repeats, final checks (the L545 per-task rule for new variants), per-message checks, regressions, rework, refusals and declined dialogs and the layout/record checks stay per cell with no margin.

Quality has no margin. A metric the candidate does not know (a run from before T60: no slices or refusals; a plain run from before T72b: no layout; a missing transcript) is FAIL: certification needs the candidate's runs to carry it.

**New variants.** A baseline of a build from before T60 has no `ldd-native` or `ldd-ask`. Such a cell is compared with the baseline's `ldd` cell of the same task and condition for cost, and judged on the absolute quality rules: the hidden test passes at least as often, pooled over the new variants of that task and condition, as the baseline's runs of that task and condition pooled over its variants (a task the installed build fails one time in six may fail as often here, never more), every run passes the final message checks, every slice holds, no refusal or declined dialog, regressions 0, the layout and record rule; RED before GREEN, tests before the end and out of scope count in the pooled discipline against the baseline's `ldd` runs; rework is not compared. An `ldd-ask` cell's cost (quota-weighted tokens, turns, active time) is shown but does not fail the certification: it sets LDD up in the run, which the baseline's `ldd` cell never pays (L548); `ldd-native` counts in the geometric mean. The output says so in a note.

**Plain under always-LDD (T72b, L554-L559).** A baseline of a build from before T72 (752cb382) works plain without LDD; the candidate's `jhon` and `pi-jhon` plain cells set LDD up, a different behaviour by the owner's decision. Such a cell is still compared with the baseline's plain cell for quality (the hidden test, the per-message checks, regressions, out of scope, refusals and the rest, as any cell), judged by the layout-and-record rule above, and its cost is shown but does not fail the certification: it pays the set-up the baseline's plain cell never paid (L548). A note says "plain now sets LDD up (L554-L559)". The `clean` and `pi-clean` plain cells count in the geometric mean.

**Gates.** `--gates <checkout>` runs, once, the deterministic checks, each in an environment of its own the way the repo's tests run (`env -i` with a scratch HOME, XDG dirs, TMPDIR, `GIT_CONFIG_GLOBAL` and `GOCACHE`, `GIT_CONFIG_NOSYSTEM=1`, the module cache read-only with `GOPROXY=off`, `GOFLAGS=-mod=readonly`, `GOTOOLCHAIN=local`), under `nice -n 19` with `GOMAXPROCS=2`:

- `mode matrix`: `go test -p 1 -count=1 -run TestModeMatrix ./test/guards/` (T59);
- `go suite`: `go test -p 1 ./...`;
- `shell suite`: `node --experimental-strip-types --test test/*.test.ts` in `shell/`;
- `tsc`: `npx --no-install tsc -p .` in `shell/`.

A gate passes on exit 0; its last 20 lines go to certify.json. `--gates` alone certifies the gates only. The tests check the plumbing with a fake runner and never run the real suites.

**Replay (`--replay-guard <jhon>`, T64d, L531).** A refusal or a declined guard dialog usually comes from guard behaviour the installed build shares: it tracks how the agent phrased a command, not a regression. `--replay-guard` names the reference build's `jhon` (the baseline's, e.g. 752cb382 built from `git archive`). Every refusal and declined dialog of every run, on both sides, is found in its `stream.jsonl`:

- Claude Code: a `tool_result` with `is_error` whose text is a guard's refusal (the same `refusalText` the refusal count uses), with its Bash `tool_use`'s command; under a `parent_tool_use_id` it is a sub-agent's (the `Agent` call's `subagent_type`), which metrics.json does not count: it is replayed and listed, never excluded;
- pi: an `extension_ui_request` `select` titled `Jhon safety guard · <reason>`, with the bash call (`tool_execution_start`) whose error result starts with that reason.

Each one goes, as the payload it was, into the reference: `jhon hook pre-tool --agent * safety-guard vcs-guard .git secrets-guard route-guard` (pi's entry and the render's, less framework-guard, whose protected paths are the bench HOME's), the Bash command with any `…/runs/<run>/repo` path pointed at the run's `repo/` where it is read now, `cwd` that `repo/` (its final state), `permission_mode` `default`, and `agent_id`/`agent_type` for a sub-agent's. It runs with no inherited environment: a scratch HOME, XDG dirs, TMPDIR and `GIT_CONFIG_GLOBAL` under `<out>/replay/`, PATH `/usr/local/bin:/usr/bin:/bin`, 30 s at most; nothing in it reaches a network. Exit 2 is refused, an `ask` decision asked, exit 0 otherwise passed.

When the reference refuses or asks too, the refusal or dialog is today's behaviour: it is taken off the run's count before the comparison and listed under `baseline-shared`. When the reference passes it, or the replay cannot judge it (no `stream.jsonl`, no `repo/`, a stream whose top-level refusals differ from metrics.json's, a command the stream does not name, a binary error or a broken-render refusal), it counts as before, with no margin, and is listed under `counted`. The baseline's go through the same reference, so the comparison is symmetric; since the reference is the baseline's build, every one should be shared, and the output says how many are. A side given as a `report.json` only is not replayed (a note says so). Without `--replay-guard`, the output and certify.json are what they were.

```text
replay: reference guard <jhon>
  baseline-shared (excluded): 2
        baseline invoice-slices plain pi-jhon invoice-slices-plain-pi-jhon-1: dialog, reference asked: …; D=$OLDPWD ⏎ python3 $D/cli.py …
        candidate contacts-migration plain jhon contacts-migration-plain-jhon-1: refusal, reference refused: … cat data/*.json; …
  counted: 0
  baseline: 1 of 1 refusals and dialogs shared
```

certify.json has the same under `replay`: `guard`, `baseline_shared` and `counted`, each event with its `side`, `cell`, `run`, `kind`, `command` and `reference` (refused, asked, passed, or `replay error: …`).

### Reading the output

```text
certify: candidate <candidate dir> against baseline <baseline dir>
  note: the baseline has no ldd-ask: its cells are compared with the baseline's ldd of the same task and condition for cost, and judged on the absolute quality rules (…)
  note: plain now sets LDD up (L554-L559): a Jhon plain cell must end with ldd/ and its record, its quality is compared with the baseline's plain cell (…)
PASS  final checks                     8/8 cells
…
FAIL  plain sets up ldd with a record  1/2 cells
        invoice-slices plain jhon: none:3 (record 0/3), need ldd:3 (record 3/3)
FAIL  ldd-ask layout and record        1/2 cells
        invoice-slices ldd-ask pi-jhon: none:3 (record 0/3), need ldd:3 (record 3/3)
PASS  red before green                 8/8 cells; pooled 23/24 vs baseline 24/24, one-sided Fisher p=0.5 (FAIL below 0.05)
        invoice-slices plain jhon: 2/3 vs baseline 3/3
FAIL  quota-weighted tokens            8/8 cells; geometric mean 1.2 over 4 cells, limit 1.15 × (4 informational, 1 review)
        geometric mean 1.2 > 1.15 × the baseline
        review invoice-slices ldd pi-jhon: 140160 vs baseline 87600 (1.6×); review: > 1.5 × the baseline
…
PASS  gate mode matrix                 (cd <checkout> && nice -n 19 go test -p 1 -count=1 -run TestModeMatrix ./test/guards/)
verdict: FAIL — 5 of 19 metrics failed
certify.json: <candidate dir>/certify.json
```

One line per metric: PASS or FAIL and how many cells hold it, and for the cost and discipline metrics the geometric mean or the pooled counts with the Fisher p; under it, each failing cell with the candidate's value and the baseline's, the cells worse than their baseline for a pooled metric, and the cost cells flagged for review. Then one line per gate and the verdict. `jhon sync` only after `verdict: PASS`. certify.json holds the same: `metrics` (one verdict per metric, with the aggregate under `detail` for the cost and pooled metrics), `checks` (every cell's every metric with its detail), `gates`, `notes`, `verdict` and, with `--replay-guard`, `replay`.

## Value (T70, T146)

`bench value` answers what Jhon adds over not using it, with the owner's criterion (L552): "Igual o mejor calidad, mejor disciplina", and, since T146, the owner's cost and time targets per task size (L1205).

```sh
go run ./tools/bench value [--json] [--out DIR] [--tasks-dir DIR] <results dirs>
```

The results dirs (comma-separated or several arguments) are read as certify reads a side: `runs/*/metrics.json`, or a `report.json`. Each Jhon cell (task × variant) is compared with its counterpart, the same task's `plain` cell of the condition without Jhon: `jhon` against `clean`, `pi-jhon` against `pi-clean`. The no-Jhon conditions run `plain` only, so an `ldd` cell is compared with the plain one (a note says so). A Jhon `plain` cell starts on a repo already set up for LDD (the steady state, T146 FB2); the one-time set-up is the `ldd-setup` run, shown on its own line and never in a cell. The task's size comes from its runs' `task_size`, else from its `task.yaml` under `--tasks-dir`. A cell PASSes when:

| Metric | Rule |
|---|---|
| repeats | 3 runs or more on each side (L483) |
| hidden tests | pass fraction at least the clean one's |
| red before green | more often than the clean one, or in every run of both |
| tests run before end | more often than the clean one, or in every run of both |
| out of scope | median files no higher than the clean one's |
| size | the task has one (`small` or `large`); none is FAIL |
| weighted tokens | the median tokens weighted by the provider's prices (`cost_weighted`) at most the size's limit × the clean median: small 1.1×, large 1.25× |
| cost usd | the same limit, when both cells have `cost_usd` (the stream gave one); absent otherwise |
| active time | the same limit on the median active time |
| better quality | large only: a higher hidden-test pass rate than clean, or, where both pass every run, fewer regressions and rework per run |
| turns, quota points | shown (`INFO`), never failed; quota points are the subscription's weekly quota, never mixed into the cost |

The limits live in one place, `valueGates` in `value.go`, with the owner's decision (L1205: D1 small "≤ 1,1×", D2 large "≤ 1,25×" with strictly better quality). Every measure is shown as `median (min-max)` on each side. The token weights per provider are one table, `providerTokenWeights` in `cost.go`, with its sources: Claude's (input 1, cache write 1.25, cache read 0.1, output 5) for Claude Code and claude-bridge, GLM's (Z.ai's GLM-4.6 prices: cache read 0.11/0.60, output 2.20/0.60, cache write as input) for a GLM model on any provider; another provider takes Claude's with a note. A `report.json` from before T146 has no `cost_weighted`: it is weighed from the token medians, with a note.

A metric unknown on either side is FAIL, and so is a Jhon cell with no counterpart (`no pi-clean run of <task> plain: run --conditions pi-clean`). Then one line per runtime (`runtime claude-code (jhon vs clean)`, `runtime pi (pi-jhon vs pi-clean)`): PASS when every cell of it passed, with the count. The verdict is PASS when every runtime is. `value.json` (`results`, `notes`, `cells` with their `size` and `checks`, `setup`, `runtimes`, `verdict`) goes to the first results dir (`--out` to change it); `--json` prints it in place of the lines. The exit code is 0 on PASS, 1 on FAIL, 2 on a usage or read error.

## The pi-jhon condition is unverified

The fake runtimes in the tests cover the runner. No real pi run has been made. These points rest on pi 1.5.0's documentation (`docs/cli.md`, `docs/json.md`, `docs/session-format.md`) and on the Jhon shell's own child runs (`shell/lib/agents.ts` `childArgs`). They are not observed yet:

1. **The bench HOME's pi profile runs on claude-bridge.** The bench HOME needs the claude-bridge package installed and selected, and the bridge must find the bench HOME's Claude Code login. `render/pi.go` writes `claude-bridge.json` only when the active profile runs on claude-bridge.
2. **`--provider claude-bridge --model haiku` resolves.** The model name may need the full id, such as `claude-haiku-5-5`.
3. **The tools run with no prompt in JSON mode.** pi's default tools (read, bash, edit, write) may still be held by the Jhon shell's permission mode.
4. **The session file has the run's id.** `--session-id <id>` should leave the session as `sessions/<cwd>/<timestamp>_<id>.jsonl`, and `jhon audit` should read it.
5. **Quota.** pi has no `rate_limit_event` of its own. The bridge re-emits Claude Code's as Anthropic unified headers. The runner looks for `anthropic-ratelimit-unified-7d-utilization` anywhere in the stream. If it is not there, pi runs record no quota points, and the budget stop relies on the Claude Code runs' readings.
6. **The turn cap.** pi has no `--max-turns`. The runner counts `turn_start` events and kills the process group past the task's `max_turns`, counted over every message of the run.

**The first real check for the pilot** is one cheap pi-jhon run, alone, before the 6-run pilot:

```sh
go run ./tools/bench run --tasks pagination-bug --conditions pi-jhon --repeats 1 --model haiku
```

Then look at the run's directory:

- `stream.jsonl` starts with a `session` header whose `id` is the planned id, and has `turn_start` events;
- `stderr.txt` names no provider, model or permission error;
- `metrics.json` has a `transcript` and no `problems`;
- `stream.jsonl` either has a utilization header or does not (`utilization` null); if it does not, say so.

If the model does not resolve, run `HOME=<bench home> <bench home>/.local/bin/jhon start pi --list-models claude-bridge`. It makes no model call.

The Claude Code conditions follow `SPIKE.md`. Two things there were not tried yet: `--allowedTools` and `--session-id` with `jhon start -p`.

The multi-message drivers (T46) are covered by the fake runtimes only. Not observed yet: pi's RPC records through `jhon start pi --mode rpc` on the bridge (the `prompt` response and `agent_settled` as `docs/rpc.md` shows them), whether a background notice after `agent_settled` starts a turn the bench does not wait for, `--resume <id>` with `jhon start -p` keeping the transcript's id, whether `-p` keeps a background job alive past its exit, and whether `TaskOutput` (which the binary also lists among older names) is still a tool.

## The 4R gate: `bench review-compare` (T118e, L727)

The native 4R (`jhon_review`: one tool call whose lens, refuter and validator prompts run as tool-less completions through pi's model registry) is installed only if it costs less than today's lens agents and finds no fewer known defects. `bench review-compare` measures both on the same frozen changes, in the `pi-jhon` condition, on the bench HOME's profile:

    go run ./tools/bench review-compare --dry-run                 # the plan: 5 fixtures × 2 arms
    go run ./tools/bench review-compare [--repeats N] [--pi-model P/M[@T]] [--pi-env NAMES]
    go run ./tools/bench review-compare --report <results dir>    # the report again from the runs

| Arm | The run |
|---|---|
| `native` | `jhon start pi --mode json … -- <prompt>` with `JHON_REVIEW_NATIVE=on` |
| `agents` | the same with `JHON_REVIEW_NATIVE=off`: the shell's `jhon_review` runs `jhon review start` and answers with its fallback (launch `review-<lens>`, then `review-refuter` and `review-validator`) without a completion |

Both arms get the same owner message: run the 4R of the branch's change against `main` until review-4r.md is recorded, then stop (no edits, no fixes, no verify, no delivery). The main session runs `--pi-model`, else the profile's orchestrator model, else claude-bridge on `--model` (default `opus`); every reviewer runs the profile's model for its routing key.

Fixtures live in `review/<id>/`: `base/` is committed on `main` with the native `ldd/config.yaml` (plus a `review_required` entry naming the fixture's change with `review: 4r`, so the 4R is owed and the route guard lets the agents arm launch its pertinent `review-<lens>`, `review-refuter` and `review-validator` headless, as the native arm runs — without it the guard asks and headless pi cannot answer, L739) and a `pyproject.toml`, `candidate/` is committed on `change/<change>` (with its record `ldd/changes/<change>/`), and `manifest.json` lists the seeded defects (`file`, `lines` [from, to], `lens`, `severity`, `what`); `"clean": true` marks the fixture with none.

| Fixture | Lenses the engine picks | Known defects |
|---|---|---|
| `webhook-notify` | risk, resilience | D1 the API token logged (risk); D2 an unbounded retry with no backoff (resilience) |
| `ledger-cache` | resilience, reliability | D1 a race on the shared cache and counter (resilience); D2 `subprocess.run`'s exit code ignored (reliability) |
| `orders-summary` | risk, reliability, readability | D1 a 65-line, 14-level function (readability); D2 returns `total` (float) where the contract requires integer `total_cents` (risk); D3 float math on cents (reliability) |
| `late-fees` | reliability | D1 `except Exception: pass` drops fees (reliability); D2 division by zero on an empty batch (reliability) |
| `slug-clean` | reliability | none: every standing finding is a false positive |

Per run, from the pi session: the parent's tokens and cost (its own assistant messages), the reviewers' (the native completions, which only `jhon_review`'s result `details.reviewers` holds, or the `review-*` child sessions under `subagents/`), every other child session's, wall time, and the change's review-4r.md (verdict, lenses, correction set, for verify). A finding hits a known defect when its location names the defect's file and its lines overlap the defect's; refuted findings count neither way; any other standing finding is a false positive. Each run writes `runs/<fixture>-<arm>-pi-jhon-<n>/review-metrics.json`; the batch writes `report.md` and `report.json`: a table per fixture, pooled totals per arm, and the gate — **PASS** when the native total cost < the agents' AND native hits ≥ agents' hits, else **FAIL**, with the numbers. Cost is the providers' reported dollars when both arms report some, else the quota-weighted tokens (input + 1.25 cache write + 0.1 cache read + 5 output).
