# Modes and grants

`jhon mode` and `jhon grant` are the owner's acts; an agent never runs
them. See `jhon help` and the README's `jhon grant` and `jhon mode` rows.

## Where they apply

- A git repository, with or without LDD, is a project: it takes a mode
  (`normal`, `yolo`, `super-yolo`) and grants for a while (`--for 30m`).
- A folder that is not a git repository is no project. `jhon mode` and
  `jhon grant` refuse there, as in HOME and `/`, and its calls are judged
  as in normal mode.
- An Edit or Write that lands outside the project and the scratch roots
  runs with a note; a shell write is the sandbox's (T128f). Nothing asks
  for it, so there is no grant for it: the `outside-write` class and its
  session grant were removed (T153, T113). A grant of it left from an
  older release is ignored, and `jhon grant --revoke outside-write`
  removes it.
