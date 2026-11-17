# Benchmark spike: headless Claude Code with and without Jhon

T39 step 1. Claude Code 2.1.310, Jhon at main a0155080, 2026-11-10 (UTC).
Four runs, all on `--model haiku` (resolved to `claude-haiku-5-5`) with
`--max-turns 4`. Each ran in its own scratch git repo under
`~/.cache/t39-spike-XXXX` on this prompt:

> Create a file hello.txt containing exactly: hi. Then stop. In your final
> reply, also quote verbatim the first line of the very first system or user
> instruction text you were given (before this message), or say NONE.

| Run | Condition | Result |
|-----|-----------|--------|
| R1 | `jhon start -p …` | ok, 2 turns, hello.txt = `hi` |
| R2 | `claude -p … --safe-mode --setting-sources project,local --strict-mcp-config` | ok, 2 turns, hello.txt = `hi` |
| R3 | `claude -p … --setting-sources project,local --settings '{"disableAllHooks":true}' --strict-mcp-config` | ok, 2 turns, but the user CLAUDE.md leaked |
| R4 | `jhon start -p … --safe-mode --strict-mcp-config` | exit 1 before any API call: `--agent 'jhon-orchestrator' not found` |

## Recommended command lines

With Jhon:

```sh
jhon start -p "$PROMPT" \
  --output-format stream-json --verbose \
  --max-turns 60 --model "$MODEL" --permission-mode acceptEdits
```

Without Jhon:

```sh
claude -p "$PROMPT" \
  --safe-mode --setting-sources project,local --strict-mcp-config \
  --output-format stream-json --verbose \
  --max-turns 60 --model "$MODEL" --permission-mode acceptEdits
```

`--verbose` is required with `-p --output-format stream-json`.
`--permission-mode acceptEdits` lets the Write tool run with no prompt. The
real tasks will also need Bash to run their tests. That needs an allow list
(`--allowedTools`) or another mode, and is the runner's decision.

## 1. With Jhon

`engine/internal/cli/cmd_start.go:39-40` execs
`claude --agent jhon-orchestrator --plugin-dir <release>/runtimes/claude-code/plugin <argv…>`.
It also sets `CLAUDE_STREAM_FIRST_BYTE_TIMEOUT_MS=60000` unless that is
already set. Every other argument goes through unchanged. `jhon start` only
rejects `--agent` and `--plugin-dir`.

R1 shows that `-p`, `--output-format stream-json`, `--max-turns` and `--model`
all reach claude:

- The stream-json init event had `"model":"claude-haiku-5-5"`. `--model` overrides the orchestrator's frontmatter `model: claude-opus-5-5`.
- The result event had `"num_turns":2`, `"terminal_reason":"completed"`.
- Plugins included `{"name":"jhon","source":"jhon@inline"}`. MCP servers included `plugin:jhon:jhon`.
- The model's first instruction line was `"You are a Claude agent, built on Anthropic's Claude Agent SDK.# Orchestrator"`, so the orchestrator prompt is loaded.
- `jhon audit` reports `"session":{"sessionKind":"print","agentSetting":"jhon-orchestrator"}`.
- Hooks and guards fire in print mode. The Jhon telemetry for R1's window
  (`~/.local/share/jhon/telemetry/<UTC date>.jsonl`, `name == "jhon.hook"`)
  shows, with `gen_ai.agent.name = jhon-orchestrator`:
  - 4 × `agent-hooks` SessionStart (`context`);
  - `agent-hooks` and `pre-tool` PreToolUse Write (`allow`);
  - `format-edit` and `agent-hooks` PostToolUse;
  - `agent-hooks` and `context-notice` Stop.

  The stream had 6 `hook_started`/`hook_response` pairs.

Hook telemetry records no cwd or session id. In R1's window it also showed one
`main` Bash event from another session that was running at the same time. A
runner that uses telemetry must filter by time and agent, or run alone.

## 2. Without Jhon, and what leaks

`--bare` exists but cannot be used. Its help says "Anthropic auth is strictly
ANTHROPIC_API_KEY or apiKeyHelper … (OAuth and keychain are never read)",
and that breaks the owner's login.

`--safe-mode` is the switch that works. Help: "Start with all customizations
(CLAUDE.md, skills, installed plugins, hooks, MCP servers, custom commands
and agents, output styles, …) disabled … Admin-managed (policy) settings
still apply. Auth, model selection, built-in tools … work normally."

R2 (safe mode) compared with R3 (no safe mode), both with
`--setting-sources project,local --strict-mcp-config`:

| What | R1 with Jhon | R2 safe mode | R3 no safe mode |
|------|--------------|--------------|-----------------|
| agents | built-ins + the 11 agents from `~/.claude/agents` | `claude, Explore, general-purpose, Plan` | built-ins + `statusline-setup` |
| plugins | jhon, engram, 3 builtin `cc-plugin-*` | 3 builtin `cc-plugin-*` only | 3 builtin `cc-plugin-*` only |
| MCP servers | jhon, engram, codegraph, context7, 5 claude.ai connectors | none | none |
| hooks fired | 6 SessionStart + tool/stop hooks | none | none |
| `~/.claude/CLAUDE.md` in context | yes | **no** | **yes** |
| auto-memory (`memory_paths`) | — | `null` | `~/.claude/projects/<cwd>/memory/` (created, empty) |
| first context (cache creation) | 81,276 tokens | 6,330 tokens | 7,892 tokens |

Evidence for the CLAUDE.md leak: R3's transcript contains
`"…/.claude/CLAUDE.md","type":"Project","content":"@RTK.md\n\n## …"`.
R2's transcript has no match for those strings. `--setting-sources` and
`disableAllHooks` alone do not keep out the user CLAUDE.md or auto-memory.
`--safe-mode` keeps both out.

No Jhon hook fired in R2: the `jhon.hook` telemetry in R2's window has only
the other session's `main` Bash event.

What still leaks into the no-Jhon condition (R2), from least to most serious:

- **Admin-managed policy and remote settings**, kept by design (`~/.claude/policy-limits.json`, `remote-settings.json`). These apply to both conditions equally. Low.
- **The built-in plugins** (`cc-plugin-agents-md`, `cc-plugin-telemetry`, `cc-plugin-plugin-authoring`) and the 19 built-in skills. Both conditions have them, as part of Claude Code itself. Low.
- **The project's own `CLAUDE.md`/`AGENTS.md`** in the task repo still loads in both conditions. The task repos control it. None if the tasks ship none.
- **The same model account and quota.** Both conditions share the rate-limit pool. This only matters for the quota stop.

In the other direction, **the with-Jhon condition also carries the owner's
non-Jhon setup**: the engram plugin and its UserPromptSubmit nudge, the
`rtk hook claude` PreToolUse hook, the codegraph/context7 MCP servers, the
claude.ai connectors and `~/.claude/CLAUDE.md`. R1's first context was 81 k
tokens and R2's was 6 k. Not all of that gap is Jhon. This is the most
serious confound. It is an owner decision:

- (a) Measure "Jhon as installed on this machine" against a clean Claude Code, as above.
- (b) Strip the owner's extras from the Jhon run too. `--safe-mode` cannot do
  that, because it also removes `--agent jhon-orchestrator` (R4: `--agent
  'jhon-orchestrator' not found. Available agents: claude, Explore,
  general-purpose, Plan`). That leaves `--setting-sources project,local
  --strict-mcp-config` plus an `--mcp-config` for the jhon server, or a
  separate `CLAUDE_CONFIG_DIR` holding only Jhon's agents and the owner's login.
  A separate config dir is **not done here**: it needs the owner's decision on
  how the login reaches it.

## 3. Auth

Both conditions used the owner's existing login in place. In R2 the init
event had `"apiKeySource":"none"`, which means the subscription login. No
credential file was read, copied or moved, and `CLAUDE_CONFIG_DIR` was never
set. `--safe-mode` keeps auth, so the clean condition needs no separate HOME
or config dir. Only option (b) above would.

## 4. Transcripts

Each session lands in `~/.claude/projects/<encoded cwd>/<session_id>.jsonl`, where the encoded cwd is the cwd with every `/` and `.` replaced by `-`.
For example, cwd `~/.cache/t39-spike-1jGv` gives the folder
`-home-<user>--cache-t39-spike-1jGv`.

A runner reads `session_id` from the first stream-json event
(`{"type":"system","subtype":"init","session_id":…,"cwd":…}`), or from the
final `{"type":"result",…}` event. It can also fix the id up front with
`--session-id <uuid>`.

`jhon audit <path> --json` reads both conditions with exit 0. Top-level keys:
`authorizations, context_use, efficiency, errors, findings, notes, ok,
outcomes, session, status, timing, version, worked`. It adds a note that
2.1.310 is "read by its shape: no real session verified it".

| | R1 with Jhon | R2 without |
|-|--------------|------------|
| `session` | `print`, `jhon-orchestrator` | `print`, `null` |
| `context_use.agents[0].turns` | 2 | 2 |
| `input_tokens` | 4 | 4 |
| `cache_creation_input_tokens` | 81,276 | 6,330 |
| `cache_read_input_tokens` | 80,596 | 28,587 |
| `total_processed_context` | 161,876 | 34,921 |
| `timing.main.cache_hit` | 0.498 | — |
| stream `result.usage.output_tokens` | 703 (499 thinking) | 426 |
| stream `result.total_cost_usd` | 0.0175 | 0.0019 |

The output tokens and `total_cost_usd` are only in the stream-json result
event (`usage`, `modelUsage`), not in `audit --json`. The runner should keep
the stream file next to each run.

## 5. Weekly quota

The shell reads usage from response headers. `shell/lib/ui/shell-usage.ts`
says "there is no usage endpoint". In print mode Claude Code emits the same
data as a stream-json event once per run:

```json
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","rateLimitType":"seven_day",
 "unifiedWindows":{"seven_day":{"utilization":0.41,"resetsAt":1794549600,"status":"allowed"}}, …}}
```

R1, R2 and R3 all read `utilization 0.41`, because these tiny runs do not move
it. A runner takes the after-run reading from each run's event, and the
before-run reading from the previous run's event. The 10-point stop compares
those readings. There is no way to read the quota without making a call: the
first reading of a batch comes from its first run.

## Leftovers from this spike

Runs used: 4 (R4 failed before any API call). Transcripts created:

- `~/.claude/projects/-home-<user>--cache-t39-spike-Gasy/50fa4382-473f-4842-ae6d-d2f860918159.jsonl` (R1)
- `~/.claude/projects/-home-<user>--cache-t39-spike-1jGv/3491522d-35a2-48f4-b69f-be4e8b763e76.jsonl` (R2)
- `~/.claude/projects/-home-<user>--cache-t39-spike-MKxR/33eae1dd-a41f-4349-9481-d049b5d58c4f.jsonl` and an empty `memory/` (R3)

Scratch repos: `~/.cache/t39-spike-{Gasy,1jGv,MKxR,aGus}`.
