# Asked operations

- Ambient SSH agents, sockets, credentials, and authenticated sessions are not
  permission to use them.
- No wrapper, interpreter, library, or delegate widens the authorized scope.
- The safety guard asks before remote access; where it cannot ask, the owner
  authorizes a class for a while with `jhon grant remote` (or `push`,
  `gh-write`, `publish`, `deploy`), given as the always-on "Runtime
  boundaries" rule says; the owner's `jhon mode yolo` lets them pass without
  asking until the owner turns it off.
- In a normal session only what truly matters asks (S33). A push to a
  branch that is not the remote's default (its HEAD, else `main` or
  `master`; the current branch when the push names none) passes, and so do
  GitHub reads and a pull request's own work: `gh pr create`, `view`,
  `checks`, `comment`, `ready`, `edit`, `gh issue comment`, `gh run
  watch`/`view`, and `gh api` writes that open, edit, or comment on a pull
  request. Still asked: a push to the default branch, of every branch, or
  of tags (a tag can start a release), or one whose target the guard cannot
  tell (a URL, a configuration that maps or mirrors it, `GIT_DIR`); `gh pr
  merge`, `close`, `review`, releases, repository, secret and workflow
  writes, every other `gh api` write; and deploy, destroy, publish, remote
  and outside-write, a report in the home's Documents and a sibling project such
  as a frontend included. One command that needs several classes asks one
  question that names them all.
- A grant and a mode are the owner's act alone. The safety guard refuses
  `mode <a mode>` and `grant <a class>` (or `grant --revoke`) in any
  program's arguments, whatever the program is called (`go run`, a copied,
  built or linked binary, a subprocess's literal argv, concatenated
  literals included), and any write into the grants or the modes directory,
  through a glob or a link too. Its limit: a program that computes its
  arguments or a path at run time can still write a file, and no reading of
  the command's text can stop that. The modes' history records every change
  with `by: "owner"`, and `jhon audit` reads it.
- **Deletions the owner asks for.** A deletion that loses work or data (a
  forced branch delete or worktree removal, `reset --hard`, `clean -f`, a
  dropped stash, a destructive Docker command: `docker compose down -v`,
  `docker volume rm`, `prune`, `docker rm -f`; a path in the home directory
  outside the project): name each one, say which keep data and which delete
  it, and run it once the owner approves (class `destroy`: the guard's
  question, or their `jhon grant destroy`).
