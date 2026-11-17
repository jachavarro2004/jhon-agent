## CodeGraph

For structural questions (architecture, call flow, references, impact,
"how does X work") and for locating code to change, query CodeGraph before
broad Read/Grep exploration: {{runtime:codegraph-explore}} when the MCP tool exists,
otherwise the read-only CLI (`codegraph query|explore|node|files|callers|callees|impact|affected|status`).

- In a real project whose root (`git rev-parse --show-toplevel`) has no
  `.codegraph/`, run `codegraph init <root>` once; never in `$HOME` or a
  temporary directory. Fall back to filesystem tools only when CodeGraph
  fails, and say so.
- Never run `codegraph uninit|install|uninstall|upgrade`; `codegraph index`
  only to recover a corrupt index. The watcher keeps the index in sync.
- Place worktrees that need CodeGraph under the home directory, as
  `<repo-parent>/<repo>-worktrees/<name>`, never under `/tmp`. Each worktree
  gets its own index; never copy or link another checkout's.
