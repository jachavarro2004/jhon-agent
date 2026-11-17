# runtimes/

What each runtime needs from Jhon beyond the files the engine renders. The
engine renders every runtime's agent definitions and settings from `prose/`
(`engine/internal/render`: `claude.go`, `pi.go`); this directory
holds only the parts that run inside a runtime.

| Runtime | Here | Rendered by the engine |
|---|---|---|
| Claude Code | `claude-code/plugin/`: the plugin `jhon start` loads, with the `/jhon-ldd` command (`skills/jhon-ldd`), the read-only `jhon` MCP server (`.mcp.json`), and the `SubagentStop` hook that asks a writer for its return shape, and the `Stop` hook that sends back once a reply that announces an action and stops (`jhon hook announce-stop`) (`hooks/hooks.json`). | `~/.claude/agents/<name>.md`, each with its tools and its guards as hooks. |
| pi | nothing: the pi code is `jhon-shell`, its own package in `shell/` (`shell/README.md`), which the render loads as a local pi package from the release. | `~/.pi/jhon/`: `settings.json`, `jhon.json` (what the shell reads), `mcp.json` (Engram, codegraph, and `jhon mcp`), the orchestrator's prompt, and one definition per agent; `/jhon-ldd` is the shell's own command there (`shell/lib/ldd-command.ts`). Start it with `jhon start pi`. |

Every guard is the engine's own: a runtime hook or the pi shell runs
`<release>/build/jhon hook <name>` and honours its verdict.

`jhon audit` reads the sessions of both runtimes, so the rules it
checks are checked on each. How much each runtime enforces differs: `jhon doctor --runtime <name>` lists,
for every rule, whether the runtime enforces it, `jhon audit` checks it after
the fact, or it binds by instruction alone. Claude Code is the reference.
