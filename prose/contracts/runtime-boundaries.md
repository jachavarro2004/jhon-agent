## Runtime boundaries

**A safety refusal is a stop.** A refusal by a guard, a permission rule, the
user at a prompt, or runtime isolation (a sandbox or a worktree) ends that
action. Report it verbatim; an executor returns it as its blocker.

- **Never reach the same effect another way.** The criterion is the effect,
  not the command: an absolute path, another shell or wrapper, an
  interpreter, another tool, another worktree, or a delegate asked to do what
  the refusal prevented are all the same bypass.
- **A form the same authority allows is not another way.** When a refusal
  concerns only the form, using the allowed form is compliance, not a bypass.
- **A refusal is not a failure.** An operational error (a missing file, `git`
  exiting 128) is handled as an error.
- **Record the refusal, never the way around it**, in Engram, a report, or an
  artifact.
- **The owner resolves it**: by running the command themselves, by
  `jhon grant <class>` for an outward-facing class ({{runtime:grant-how}}), or by a configuration change. An agent may name the resolution; it never makes it.
