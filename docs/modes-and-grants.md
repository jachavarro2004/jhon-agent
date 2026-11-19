# Modes, grants and the session grant

`jhon mode` and `jhon grant` are the owner's acts; an agent never runs
them. See `jhon help` and the README's `jhon grant` and `jhon mode` rows.

## Where they apply

- A git repository, with or without LDD, is a project: it takes a mode
  (`normal`, `yolo`, `super-yolo`) and grants for a while (`--for 30m`).
- A folder that is not a git repository is no project. `jhon mode` and a
  timed `jhon grant` refuse there, as in HOME and `/`, and its calls are
  judged as in normal mode. An Edit or Write that lands outside the
  scratch roots asks; a shell write is the sandbox's (T128f).

## The session grant

When pi asks about such a write, it offers "Yes, and allow writes in this
folder for this session". That runs
`jhon grant outside-write --session <id>` in the folder.

- **Covers:** Edit and Write calls whose resolved path is inside the
  folder, for that session id only. A link that points outside
  the folder, and a git repository nested in it, are not covered: those
  writes still ask.
- **Never:** HOME, `/`, a folder above HOME, or a folder that is or holds
  jhon's state. It lifts only `outside-write`: deletions, credentials,
  `.git`, the framework, the settings and every refusal stay as they are.
  An agent cannot create it. A session id with a control character is
  refused.
- **Ends:** pi revokes it when the session shuts down
  (`jhon grant --revoke outside-write --session <id>`). A session that
  never ends cleanly loses it after 12 h at the latest.
- **Sub-agents:** a call that carries the same session id shares the
  grant. In Claude Code a sub-agent's calls carry its session's id, so they
  share it; this is intended. A pi child runs its own session and does not.
- **Claude Code:** there the ask is Claude Code's own permission prompt,
  and jhon never learns the answer. A yes covers that one call, and the
  next write in the folder asks again.
