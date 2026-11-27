# What safety-guard reads, and where it stops

safety-guard reads what a Bash line will run when it can read it: the
command itself, the strings a shell, an interpreter or a runner is given,
the text a program reads on its input or from a `<(…)` file, a script or a
git configuration file on disk, and a value a program runs as a command.
When it cannot read what will run (a fetched or generated script, a
descriptor it does not follow), it asks at the top level and refuses a
sub-agent. It is a filter, not a container: it reads the text of a line,
and some spellings still slip past it. What contains a command is the OS
sandbox (`sandbox.mode on`): the project, the temporary folders and the
caches are writable, everything else is read-only, whatever the guard let
through. In super-yolo, the owner's own mode, a line the guard cannot read
runs with a note, so there the sandbox is the only containment.

## Credentials by real reach (T271)

Credential contents are the owner's alone (S10): in every mode, for every
agent, a listed reader (`cat`, `head`, `grep`, `rg`, `jq`, `git show`,
`git diff`, `git grep`, … the programs of the read-only list below) whose
operand reaches a credentials file (`.env`, keys, `credentials.json`, and
anything in a home credentials folder such as `~/.ssh`) is refused, with
the fix: read the template (`.env.example`) instead, or the owner reads the
file himself. A word reaches one when it is a path that is one (by its
name, or a link's target), a `rev:path` naming one, or a shell glob or
brace operand whose expansion holds one (the shell expands it in its
folder and the reader reads each match; `**/` reaches any depth).

A selection glob (`rg -g`, `rg --iglob`, `grep --include`, `find -name` or
`-path` feeding a reader, a git pathspec with a wildcard) narrows a
recursion, so it counts only when it is credentials by name, decided with
no file: fill each `*` or `**` with nothing and with a plain word, each `?`
with a letter, each class with one of its members, each `{a,b}` with each
alternative, and ask whether the result is a credentials file by name
(templates excluded). `*.json` gives `word.json`: it is recursion and is
not judged, whatever the tree holds. `.env*`, `**/.env`, `*.pem` and
`id_rsa*` give a credentials name: such a glob is refused when a bounded
walk of its folder (tool data such as `.codegraph/` aside, first hit)
meets such a file that is not a template, or cannot finish. Globs such as
`cred*` and `id_*` give no credentials name, so they pass even where
`credentials.json` or `id_rsa` lies.

Never by spelling: a glob or pathspec that names files to leave them out
(`-g '!*.env'`, `--exclude`, find's `!` and `-not`, `':!*.env'`,
`':(exclude)…'`), a search that merely recurses into a folder, a template,
and a program that is no listed reader (an interpreter, `cp`, `source`)
are not judged. The Grep and Glob tools keep their own reading
(secrets-guard, which walks an extension glob); a Grep glob starting with
`!` leaves files out there too.

## Read-only by construction: inspect-guard (T271)

An inspecting agent (the Prism facets, pi's `inspect` tool) runs any line
whose every construct and every program is on a closed read-only list, and
keeps that program's rule: pipes, `&&`, `;`, quoting, subshells, `for`
loops, `$(…)` and `<(…)` of read-only programs all pass. Anything not on
the list is refused with the program named and a fix (the Read, Grep or
Glob tool, or hand the line to the agent that launched you).

- Programs with no option that writes or runs: `cat tac head tail wc grep
  egrep fgrep ls stat du df cut tr paste column nl fold rev comm join cmp
  diff od hexdump strings *sum cksum basename dirname realpath readlink pwd
  echo true false test [ seq sleep id whoami uname nproc which type jq`,
  and `cd pushd popd shift exit break continue :`.
- With a rule: `rg` (no `--pre`, `--hostname-bin`), `fd` (no `-x`, `-X`),
  `find` (no `-delete`, `-fprint*`, `-fls`; `-exec`, `-execdir`, `-ok`,
  `-okdir` only of a program above), `xargs` (operands it does not show only
  to a program above), `sort` (no `-o`, `-T`, `--compress-program`), `uniq`
  and `xxd` (one operand, no `-r`), `tree` (no `-o`, `-R`), `file` (no
  `-C`), `tee` (no file), `base64` (no `-o`), `date` (a `+format` only),
  `printf -v` and `read` (the names below), `set` (`-e -u -x -v -f`, `-o
  pipefail`).
- `git`: `-C`, `--git-dir`, `--work-tree`, `--no-pager` and the pathspec
  globals; the reading subcommands (log, show, diff, grep, blame, ls-files,
  ls-tree, cat-file, rev-parse, rev-list, describe, merge-base, status, …);
  branch and tag only listing, remote only bare, `-v` and `get-url`, config
  only get and list, stash, worktree, reflog and notes only list or show.
  Never `-c`, `--config-env`, `--exec-path`, `--output`, `--ext-diff`,
  `--textconv`, `--filters` or `git grep -O`; fetch, push and ls-remote
  reach the network.
- Redirections: `<`, here-docs and here-strings, `N>&M`, and `>` only to
  `/dev/null`, `/dev/stdout` or `/dev/stderr`.
- An assigned name (`x=v`, `read`, `for`, `printf -v`) is lowercase and
  absent from the environment, so no assignment changes what a program
  reads or which program runs. `export`, functions, aliases, `source`,
  `eval`, `[[ ]]`, arithmetic, background jobs and every wrapper (`timeout`,
  `env`, `command`, `time`) are not on the list.
- A word whose first character the shell decides (a leading glob, a
  variable, a substitution) before `--`, given to a program with an option
  that writes or runs, is refused: a file named `--pre=sh` would become one.
- Jhon's own readers in `inspect_allow` (`codegraph callers`, `jhon review
  freeze`, …) run besides, each as one program of the line. A read reaches
  any folder, as the Read tool's does; credential contents stay refused as
  above.

pi's `inspect` tool runs the very string the guard judged through `bash
--noprofile --norc -c`, with `BASH_ENV` and `ENV` unset. This too is a
filter, not a container: the sandbox contains.
