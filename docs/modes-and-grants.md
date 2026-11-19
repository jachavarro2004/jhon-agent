# Modes and grants

`jhon mode` and `jhon grant` are the owner's acts; an agent never runs
them. See `jhon help` and the README's `jhon grant` and `jhon mode` rows.

## Where they apply

- A git repository, with or without LDD, is a project: it takes a mode
  (`normal`, `yolo`, `super-yolo`) and grants for a while (`--for 30m`).
- A folder that is not a git repository is no project. `jhon mode` and a
  timed `jhon grant` refuse there, as in HOME and `/`, and its calls are
  judged as in normal mode.
- An Edit or Write that lands outside the project and the scratch roots
  is `outside-write`: asked at the top level, refused to a sub-agent,
  passed by yolo and super-yolo. A shell write is the sandbox's (T128f).

## The session grant

In a folder, pi's question about such a write offers "Yes, and allow
writes in this folder for this session": `jhon grant outside-write
--session <id>`. It covers that session's Edit and Write calls inside the
folder (not through a link out of it, nor into a nested repository),
never HOME, `/`, a folder above HOME or jhon's state, and nothing but
`outside-write`. pi revokes it at shutdown; at the latest it ends after
12 h. In Claude Code a yes covers that one call.
