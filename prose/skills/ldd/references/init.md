## Init

Prepare a project for LDD: detect the real stack and tests, write the
LDD skeleton, and index the available skills. `/jhon-ldd init` runs these
steps; a session without that skill follows them by reading this file,
with Bash, Read and Write only. Run every command from the project root.
LDD is always on: a session sets it up at its first code change, without
waiting to be asked; a commit or PR is refused until then.
When done, tell the owner in one line what you set up;
before you finish the task, start its change (tasks.md) and record a run (`jhon executions run`).

1. Run `jhon init <root> --json`. It discovers every project, its
   `testability`, and its test command, and reports `vcs.state`; below
   `ready`, note it in the result and never run `git init` or commit.
2. Use the project's layout: `openspec/` when it exists, else `ldd/`.
   Create `<layout>/config.yaml`, `<layout>/specs/`, and
   `<layout>/changes/archive/` when absent; for `ldd/`:

   ```sh
   mkdir -p ldd/specs ldd/changes/archive
   ```

   A new config holds a short
   `context:` (under ten lines), the testing capabilities, and any
   `rules.<phase>`; never derived values (`strict_tdd`, a `projects:` list,
   coverage).
3. Write what the owner states, passed in the prompt; for what the prompt
   leaves open, ask the owner one batched question, each option naming the
   recommended one:
   - delivery: each slice delivered to the default branch on its own
     (recommended; no `delivery` key), or the whole change in one merge
     after its last slice, `delivery: change` (contracts/ldd.md, "Slices");
   - testing: `strict_tdd_policy: require` (recommended), `forbid`, or
     `unset` (no key); and the test command of each project `jhon init`
     reports `unknown`, written as `test_command_override` keyed by project
     path as `jhon init` reports it (`".": npm test`), and likewise
     `lint_command_override` and `format_command_override`
     (contracts/ldd.md, "Code checks").
   Never write `worktree_per_change: true` (default false) unless the
   owner asks for it.
   Ask through your ask tool only (AskUserQuestion in Claude Code, `ask_user` in pi).
   With no ask tool, or no answer, do not ask in a message: write the
   recommended values, say so in one line, and go on with the task in the same turn.
   On an existing config change only those keys, plus
   removing legacy `strict_tdd` and `strict_tdd_evidence` keys when a
   policy is passed. Never guess a test command; name each `unknown`
   project left without one. One exception: when `jhon init` reports only `.`, with no
   marker (a Python repository whose tests have no pyproject.toml), and
   the config has no override for it, run the command its tests use
   (`python3 -m unittest`) once; when it runs, write it as
   `test_command_override` for `"."`, so `jhon executions` can run it. A
   new `ldd/config.yaml` for such a repository is then:

   ```yaml
   context: |
     <what the project is, in under ten lines>
   test_command_override:
     ".": python3 -m unittest
   ```
4. Run `jhon status --json` and report `strict_tdd`, `projects`, `ldd`, and
   the notes; `ldd.active` is true once the config exists. When a
   memory tool is available, save the context as `ldd-init/{project}`
   (type `config`).

This phase is the only writer of the layout's `config.yaml`.
