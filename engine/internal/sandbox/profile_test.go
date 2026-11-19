package sandbox

import (
	"slices"
	"testing"
)

// T128g (L1303 G1, L888 F1): a line runs with no sandbox only as
// `<shell> -c <line>`, the shell absolute and a known one, the line a
// network line, and neither the shell nor the git or gh the shell finds
// first on PATH a file a sandboxed command could have written.
func TestRunsOutsideOnlyANetworkLineOfTrustedPrograms(t *testing.T) {
	d := with(machine, disk{
		"/usr/bin": "dir", "/usr/bin/bash": "file", "/usr/bin/git": "file", "/usr/bin/gh": "file", "/usr/bin/fish": "file",
		"/home/u/project/bin": "dir", "/home/u/project/bin/git": "file", "/home/u/project/bin/bash": "file",
		"/home/u/.cache/bin": "dir", "/home/u/.cache/bin/gh": "file",
		"/usr/local/bin": "dir", "/usr/local/bin/git": "-> /home/u/project/bin/git",
	})
	h := host("linux", d, nil)
	for _, c := range []struct {
		name string
		path []string
		argv []string
		want bool
	}{
		{"a network line", []string{"/usr/bin"}, []string{"/usr/bin/bash", "-c", "git fetch origin"}, true},
		{"gh", []string{"/usr/bin"}, []string{"/usr/bin/bash", "-c", "gh pr list"}, true},
		{"a planted git first on PATH", []string{"/home/u/project/bin", "/usr/bin"}, []string{"/usr/bin/bash", "-c", "git fetch"}, false},
		{"a link to a planted git", []string{"/usr/local/bin", "/usr/bin"}, []string{"/usr/bin/bash", "-c", "git fetch"}, false},
		{"a planted gh in the cache", []string{"/home/u/.cache/bin", "/usr/bin"}, []string{"/usr/bin/bash", "-c", "git fetch"}, false},
		{"a planted git after the real one", []string{"/usr/bin", "/home/u/project/bin"}, []string{"/usr/bin/bash", "-c", "git fetch"}, true},
		{"a relative PATH folder", []string{"bin", "/usr/bin"}, []string{"/usr/bin/bash", "-c", "git fetch"}, false},
		{"a planted shell", []string{"/usr/bin"}, []string{"/home/u/project/bin/bash", "-c", "git fetch"}, false},
		{"a shell by name", []string{"/usr/bin"}, []string{"bash", "-c", "git fetch"}, false},
		{"another shell", []string{"/usr/bin"}, []string{"/usr/bin/fish", "-c", "git fetch"}, false},
		{"a work line", []string{"/usr/bin"}, []string{"/usr/bin/bash", "-c", "git fetch && make"}, false},
		{"more words", []string{"/usr/bin"}, []string{"/usr/bin/bash", "-c", "git fetch", "x"}, false},
		{"no -c", []string{"/usr/bin"}, []string{"/usr/bin/bash", "git fetch"}, false},
		{"no shell", []string{"/usr/bin"}, []string{"/usr/bin/git", "fetch"}, false},
	} {
		in := sample(Work, d)
		in.Path = slices.Clone(c.path)
		if got := RunsOutside(h, in, c.argv); got != c.want {
			t.Errorf("%s: RunsOutside(%q) = %v, want %v", c.name, c.argv, got, c.want)
		}
	}
}

// T128g (L1303 G1; the classifier of T128d, L1245 K1, as proven on
// 1ac9872): a line whose programs are all git with a network subcommand, or
// gh with a listed subcommand, runs unsandboxed; any other line gets work,
// and so does one the reader cannot read.
func TestProfileForLeavesUnsandboxedOnlyGitNetworkCommandsAndGh(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		// git's network subcommands and gh.
		{"git fetch", Unsandboxed},
		{"git fetch origin main --prune 2>&1", Unsandboxed},
		{"git pull --rebase", Unsandboxed},
		{"git push origin HEAD", Unsandboxed},
		{"git push -u origin feat/x", Unsandboxed},
		{"git ls-remote origin", Unsandboxed},
		{"git clone git@github.com:o/r.git", Unsandboxed},
		{"git submodule update --init --recursive", Work},
		{"git submodule --quiet update", Work},
		{"git remote update", Unsandboxed},
		{"git -C ../other fetch", Unsandboxed},
		{"git --no-pager fetch", Unsandboxed},
		{"git fetch && git push", Unsandboxed},
		{"git fetch origin; git pull", Unsandboxed},
		{"gh pr create --fill", Unsandboxed},
		{"gh api repos/o/r/pulls", Unsandboxed},
		{"gh pr view 1 && git fetch", Unsandboxed},
		// git's local subcommands, other programs, mixed lines.
		{"git commit -m x", Work},
		{"git rebase origin/main", Work},
		{"git status", Work},
		{"git", Work},
		{"git submodule status", Work},
		{"git remote -v", Work},
		{"git remote add origin x", Work},
		{"git fetch && git rebase origin/main", Work},
		{"git fetch | cat", Work},
		{"cd ../other && git fetch", Work},
		{"echo hi", Work},
		{"", Work},
		{"git push origin $(git branch --show-current)", Work},
		{"git() { cat x; }; git fetch", Work},
		// Lines the reader cannot read: fail closed.
		{"git fetch 'unterminated", Work},
		{"git fetch $(", Work},
		// What would run another program with the credentials.
		{"GIT_SSH_COMMAND=x git fetch", Work},
		{"GIT_SSH_COMMAND=x; git fetch", Work},
		{"export GIT_SSH_COMMAND=x; git fetch", Work},
		{"env GIT_SSH_COMMAND=x git fetch", Work},
		{"git -c core.sshCommand=x fetch", Work},
		{"git -ccore.sshCommand=x fetch", Work},
		{"git --config-env=core.sshCommand=X fetch", Work},
		{"git --exec-path=/tmp/x fetch", Work},
		{"git --git-dir=/tmp/x fetch", Work},
		{"git fetch --upload-pack=x origin", Work},
		{"git fetch --upload-pack x origin", Work},
		{"git ls-remote --upload-pack=x origin", Work},
		{"git push --receive-pack=x origin", Work},
		{"git push --exec=x origin", Work},
		{"git clone -u x repo", Work},
		{"git clone --config core.sshCommand=x repo", Work},
		{"git clone -c core.sshCommand=x repo", Work},
		{"git clone --template=/tmp/t repo", Work},
		{"git fetch ext::sh", Work},
		{"git $SUB origin", Work},
		{"git fetch $OPTS origin", Work},
		{"git fetch `echo --upload-pack=x` origin", Work},
		{"git fetch * origin", Work},
		{"./git fetch", Work},
		{"/usr/bin/git fetch", Work},
		{"sh -c 'git fetch'", Work},
		{"timeout 60 git fetch", Work},
		{"sudo git fetch", Work},
		{"xargs git fetch", Work},
		{"GH_TOKEN=x gh pr list", Work},
		{"gh auth token", Work},
		{"gh auth status --show-token", Work},
		{"gh $SUB", Work},
	} {
		if got := ProfileFor(c.line); got != c.want {
			t.Errorf("ProfileFor(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

// T128d2 (L1249, L1250 A1): the classifier is an allowlist. Each line the
// verifier made run a program with the credentials gets work: git takes
// unambiguous abbreviations of its long options and clusters of short ones,
// the shell's brace expansion rewrites words after the check, a gh global
// option hides the subcommand, and a function defined on the line replaces
// git.
func TestProfileForGivesWorkToEveryVerifierCounterexample(t *testing.T) {
	for _, line := range []string{
		"git ls-remote --upl=x /tmp/r",
		"git fetch --upload-pa=x",
		"git pull --upl=x",
		"git clone --upl=x /tmp/r",
		"git push --rec=x",
		"git push --ex=x",
		"git clone -nux /tmp/r",
		"git clone '-nutouch x; git-upload-pack' /tmp/r",
		"git clone --temp=/tmp/t /tmp/r",
		"git clone --conf=core.sshCommand=x /tmp/r",
		"git clone --separate-git-dir=/tmp/x /tmp/r",
		"git ls-remote {--upload-pack=x,/tmp/r}",
		"git fetch {a,--upload-pack=x}",
		"gh {auth,} token",
		"gh au{th,} token",
		"gh -u bob auth token",
		"gh -h github.com auth token",
		"git() { gh x; }; git fetch",
	} {
		if got := ProfileFor(line); got != Work {
			t.Errorf("ProfileFor(%q) = %q, want work", line, got)
		}
	}
}

// T128d2 (L1250 A1): what the allowlist leaves out gets work: an option of
// a network subcommand not listed, a short cluster, a value option's
// abbreviation, a git global but the four, a gh subcommand not listed (an
// extension's name included), a word with a brace, an alias, a remote
// helper's transport and gh's pass-through to git.
func TestProfileForGivesWorkToWhatTheAllowlistLeavesOut(t *testing.T) {
	for _, line := range []string{
		"git fetch --upload-pack x origin",
		"git fetch --dep=1 origin",
		"git fetch -pq origin",
		"git fetch --server-option=x origin",
		"git fetch --prune=yes origin",
		"git fetch -- origin {a,b}",
		"git pull -s ours origin main",
		"git pull --strategy=ours origin main",
		"git pull --rebase=interactive origin main",
		"git pull -S origin main",
		"git push --signed origin main",
		"git push --repo=x",
		"git push -uf origin main",
		"git clone --reference /tmp/x /tmp/r",
		"git clone --bundle-uri=x /tmp/r",
		"git clone -c x=y /tmp/r",
		"git clone -u x /tmp/r",
		"git submodule update --reference /tmp/x",
		"git submodule foreach git fetch",
		"git remote update --upload-pack=x",
		"git --namespace=x fetch",
		"git --bare fetch",
		"git --literal-pathspecs fetch",
		"git -C ~/x fetch",
		"git fetch evil::x",
		"git fetch 'ext::sh -c x'",
		"git clone fd::3 /tmp/d",
		"gh",
		"gh auth token",
		"gh auth status",
		"gh extension install o/r",
		"gh alias set x y",
		"gh config set editor x",
		"gh my-extension run",
		"gh --repo o/r pr list",
		"gh repo clone o/r -- --upload-pack=x",
		"gh pr list {a,b}",
		"alias git=gh; git fetch",
		"f() { git fetch; }; f",
		"function git { gh x; }; git fetch",
	} {
		if got := ProfileFor(line); got != Work {
			t.Errorf("ProfileFor(%q) = %q, want work", line, got)
		}
	}
}

// T128d2 (L1250 A1): the common network lines run unsandboxed.
func TestProfileForLeavesTheCommonNetworkLinesUnsandboxed(t *testing.T) {
	for _, line := range []string{
		"git fetch",
		"git fetch origin",
		"git fetch --prune origin",
		"git fetch --depth=1 origin main",
		"git fetch --depth 1 origin main",
		"git fetch -p origin",
		"git fetch --all --tags",
		"git pull --rebase origin main",
		"git pull --rebase=merges",
		"git pull --ff-only",
		"git push -u origin feat/x",
		"git push --force-with-lease origin feat/x",
		"git push --force-with-lease=feat/x:abc origin feat/x",
		"git push origin --delete feat/x",
		"git push -o ci.skip origin main",
		"git ls-remote origin",
		"git ls-remote --heads origin",
		"git clone https://github.com/o/r.git r",
		"git clone git@github.com:o/r.git /tmp/r",
		"git clone --depth 1 -b main https://github.com/o/r.git",
		"git -C ../other fetch",
		"git -C /tmp/r --no-pager fetch",
		"git remote update",
		"git remote update --prune origin",
		"gh pr create --title t --body b",
		"gh pr list",
		"gh api repos/o/r",
		"gh issue view 3",
		"gh run watch",
		"gh workflow run ci.yml -f x=y",
		"gh release list",
		"gh repo view o/r",
		"gh search repos x",
		"gh label list",
		"gh status",
		"gh browse",
		"gh cache list",
		"gh pr view 1 && git fetch",
		// T128d3 (L1254 R1): operands after `--`, and gh lines that read no
		// local file.
		"git fetch -- origin",
		"git pull origin -- main",
		"git push origin -- feat/x",
		"git remote update -- origin",
		"gh api -F n=1 repos/o/r/issues",
		"gh api -F=n=1 repos/o/r/issues",
		"gh api -f body=@literal repos/o/r/issues",
		"gh api --field=n=1 repos/o/r/issues",
		"gh workflow run ci.yml -F x=y",
		"gh release create v1 -t T -n notes",
		"gh release create v1 --generate-notes --target main",
		"gh release create v1 -R o/r --latest=false",
		"gh pr create --title t --body @x",
		"gh issue comment 3 --body b",
		"gh pr view 1 --json title",
	} {
		if got := ProfileFor(line); got != Unsandboxed {
			t.Errorf("ProfileFor(%q) = %q, want unsandboxed", line, got)
		}
	}
}

// T128d3 (L1253, L1254 R1): git pull and git remote update hand the words
// after `--` to fetch, which reads them as options (`git pull origin --
// --upload-pack=<cmd>` ran <cmd>), so any word after `--` that starts with
// `-` gives work for every subcommand. No submodule work runs unsandboxed: a
// submodule's own configuration is never checked. gh pr checkout runs git,
// and a gh line that reads a local file could upload any file it sees.
func TestProfileForGivesWorkToOptionsAfterDoubleDashSubmodulesAndGhFileReads(t *testing.T) {
	for _, line := range []string{
		// The verifier's `--` lines, and the same for every subcommand.
		"git pull -- origin --upload-pack=x",
		"git pull origin -- --upload-pack=x",
		"git remote update -- --upload-pack=x",
		"git remote update origin -- --upload-pack=x",
		"git fetch origin -- --upload-pack=x",
		"git push origin -- --receive-pack=x",
		"git ls-remote -- --upload-pack=x origin",
		"git clone -- --upload-pack=x repo",
		"git fetch -- -x",
		"git pull -- -",
		// Submodule work.
		"git submodule update",
		"git submodule update --init --recursive",
		"git submodule --quiet update",
		"git submodule update --init -- sub",
		"git fetch --recurse-submodules",
		"git fetch --recurse-submodules=yes origin",
		"git fetch --recurse-submodules=on-demand",
		"git fetch --no-recurse-submodules",
		"git pull --recurse-submodules",
		"git pull --recurse-submodules=no",
		"git push --recurse-submodules=check origin main",
		"git push --recurse-submodules=on-demand origin main",
		"git clone --recurse-submodules https://github.com/o/r.git",
		"git clone --recurse-submodules=sub https://github.com/o/r.git",
		"git clone --shallow-submodules https://github.com/o/r.git",
		"git clone --remote-submodules https://github.com/o/r.git",
		// gh that runs git.
		"gh pr checkout 1",
		"gh pr -R o/r checkout 1",
		"gh pr checkout --force 1",
		// gh that reads a local file.
		"gh api repos/o/r/issues --input body.json",
		"gh api repos/o/r/issues --input=body.json",
		"gh api repos/o/r/issues --input -",
		"gh api graphql -F query=@q.graphql",
		"gh api repos/o/r/issues -F body=@/home/u/.ssh/id_ed25519",
		"gh api repos/o/r/issues --field body=@x",
		"gh api repos/o/r/issues --field=body=@x",
		"gh api repos/o/r/issues -Fbody=@x",
		"gh api repos/o/r/issues -F @x",
		// L1318: pflag takes -F=<value> as -F <value>, the = dropped.
		"gh api x -F=b=@file",
		"gh api x -iF=b=@file",
		"gh api x -F=@file",
		"gh workflow run ci.yml -F=x=@secret",
		"gh workflow run ci.yml -F x=@secret",
		"gh issue create --title t --body-file b.md",
		"gh issue create --title t --body-file=b.md",
		"gh issue create --title t -F b.md",
		"gh issue comment 3 -F b.md",
		"gh pr create --title t -F b.md",
		"gh pr create --title t --body-file b.md",
		"gh pr comment 1 -F b.md",
		"gh pr review 1 --approve -F b.md",
		"gh pr edit 1 -F b.md",
		"gh release create v1 -F notes.md",
		"gh release create v1 -dF notes.md",
		"gh release create v1 --notes-file notes.md",
		"gh release edit v1 --notes-file=notes.md",
		"gh release upload v1 a.zip",
		"gh release upload v1 /home/u/.ssh/id_ed25519 --clobber",
		"gh release -R o/r upload v1 a.zip",
		"gh release create v1 a.zip",
		"gh release create v1 -t T ./dist/a.zip",
		"gh release create v1 --notes n /home/u/.ssh/id_ed25519",
		"gh release create -t T v1 a.zip#label",
	} {
		if got := ProfileFor(line); got != Work {
			t.Errorf("ProfileFor(%q) = %q, want work", line, got)
		}
	}
}

// T128g (L1303 G1): an unsandboxed line writes nothing through the shell:
// a redirect to a file, a here-document and a here-string give work; a
// descriptor's copy (2>&1) and a write to /dev/null leave it unsandboxed.
func TestProfileForGivesWorkToALineThatRedirectsToAFile(t *testing.T) {
	for line, want := range map[string]string{
		"git fetch > out.txt":                    Work,
		"git fetch >> ../x/rc":                   Work,
		"gh api repos/o/r/contents/x > out.txt":  Work,
		"git fetch 2> err.txt":                   Work,
		"git fetch &> log":                       Work,
		"gh pr create --title t < body.md":       Work,
		"gh api graphql <<< x":                   Work,
		"gh api graphql <<EOF\nx\nEOF":           Work,
		"git fetch >| out":                       Work,
		"git fetch > $F":                         Work,
		"git fetch 2>&1":                         Unsandboxed,
		"git fetch >&2":                          Unsandboxed,
		"git fetch > /dev/null 2>&1":             Unsandboxed,
		"git fetch &>/dev/null":                  Unsandboxed,
		"git fetch 2>/dev/null && git push 2>&1": Unsandboxed,
	} {
		if got := ProfileFor(line); got != want {
			t.Errorf("ProfileFor(%q) = %q, want %q", line, got, want)
		}
	}
}

// T128h (L1331, L1332 K1): Docker is where the owner deploys, so a line
// whose every program is docker or docker-compose runs unsandboxed, with
// the line rules of the git/gh rule; login, logout and a change of the
// owner's contexts stay work, as does anything the allowlist cannot read.
func TestProfileForLeavesDockerLinesUnsandboxedButLoginAndContexts(t *testing.T) {
	for line, want := range map[string]string{
		"docker ps":                                     Unsandboxed,
		"docker images":                                 Unsandboxed,
		"docker build -t app .":                         Unsandboxed,
		"docker run --rm -p 8080:80 app":                Unsandboxed,
		"docker push o/app:1":                           Unsandboxed,
		"docker compose up -d":                          Unsandboxed,
		"docker compose down":                           Unsandboxed,
		"docker compose logs -f api":                    Unsandboxed,
		"docker compose exec api npm test":              Unsandboxed,
		"docker-compose up -d":                          Unsandboxed,
		"docker-compose -f compose.yml ps":              Unsandboxed,
		"docker --context rootless ps":                  Unsandboxed,
		"docker -c rootless ps":                         Unsandboxed,
		"docker -H unix:///run/user/1000/d.sock ps":     Unsandboxed,
		"docker --debug --log-level=info ps":            Unsandboxed,
		"docker context ls":                             Unsandboxed,
		"docker context inspect rootless":               Unsandboxed,
		"docker context show":                           Unsandboxed,
		"docker ps 2>&1":                                Unsandboxed,
		"docker ps > /dev/null 2>&1":                    Unsandboxed,
		"docker compose build && docker compose up -d":  Unsandboxed,
		"docker build -t app . && git push origin main": Unsandboxed,
		// Credentials and the owner's contexts.
		"docker login":                                  Work,
		"docker login -u bob registry.example":          Work,
		"docker logout":                                 Work,
		"docker --config /tmp/c login":                  Work,
		"docker -H tcp://x:2375 login":                  Work,
		"docker --context=rootless login":               Work,
		"docker context create x --docker host=tcp://x": Work,
		"docker context use rootless":                   Work,
		"docker context rm x":                           Work,
		"docker context update x --description y":       Work,
		"docker context import x x.tar":                 Work,
		"docker context export x":                       Work,
		"docker context":                                Work,
		"docker ps && docker login":                     Work,
		// The line rules of the git/gh rule.
		"docker":                         Work,
		"docker ps && npm test":          Work,
		"docker ps | grep api":           Work,
		"docker ps > out.txt":            Work,
		"docker save app > app.tar":      Work,
		"docker ps 'unterminated":        Work,
		"docker $CMD":                    Work,
		"docker run $(echo app)":         Work,
		"docker run {a,b}":               Work,
		"docker --unknown ps":            Work,
		"docker -- ps":                   Work,
		"docker --config":                Work,
		"DOCKER_HOST=tcp://x docker ps":  Work,
		"sudo docker ps":                 Work,
		"/usr/bin/docker ps":             Work,
		"./docker ps":                    Work,
		"sh -c 'docker ps'":              Work,
		"docker() { cat x; }; docker ps": Work,
		// The verifier's regression rows (L1338).
		"docker build . ; make":                         Work,
		"docker run img | tee out.log":                  Work,
		"docker logs x > logs.txt":                      Work,
		"docker logs x >> logs.txt":                     Work,
		"docker ps &> out.txt":                          Work,
		"docker ps 2> err.txt":                          Work,
		"env X=1 docker ps":                             Work,
		"command docker ps":                             Work,
		"nohup docker ps":                               Work,
		"cd ../x && docker ps":                          Work,
		"docker -c rootless login":                      Work,
		"docker --config=/tmp/c login":                  Work,
		"docker -l debug login":                         Work,
		"docker --context rootless context use default": Work,
		"echo pw | docker login -u x --password-stdin":  Work,
		"docker-compose":                                Work,
		"docker compose -f ../other/compose.yml up":     Unsandboxed,
	} {
		if got := ProfileFor(line); got != want {
			t.Errorf("ProfileFor(%q) = %q, want %q", line, got, want)
		}
	}
}

// T128h2 (L1338): a single-quoted word, and a double-quoted one with no $
// or backtick, is literal text: its braces and glob characters reach the
// program as written, so they do not force work. A parameter, a command
// substitution, a $ or a backtick still do, and so do an unquoted brace and
// an unquoted glob, which the shell may rewrite into words the check never
// saw (as for git, `git fetch * origin`). A quoted word is still judged by
// its text: quoting hides no option, gh subcommand or file read. The
// version forms of docker and docker-compose run unsandboxed.
func TestProfileForReadsQuotedWordsLiterally(t *testing.T) {
	for line, want := range map[string]string{
		"docker ps -a --format '{{.Names}}'":            Unsandboxed,
		`docker ps -a --format "{{.Names}}"`:            Unsandboxed,
		"docker inspect -f '{{.State.Status}}' api":     Unsandboxed,
		"docker ps --filter 'name=a*'":                  Unsandboxed,
		`docker ps --filter "name=a?"`:                  Unsandboxed,
		"docker run --rm app echo '$HOME'":              Unsandboxed,
		"docker --version":                              Unsandboxed,
		"docker -v":                                     Unsandboxed,
		"docker version":                                Unsandboxed,
		"docker compose version":                        Unsandboxed,
		"docker-compose --version":                      Unsandboxed,
		"git push origin 'feat/x'":                      Unsandboxed,
		`gh pr create --title 'fix: a {b}' --body "c*"`: Unsandboxed,
		"gh pr list --search 'is:open [x]'":             Unsandboxed,
		"docker ps --filter name=a*":                    Work,
		"docker ps --format {{.Names}}":                 Work,
		`docker ps --format \{\{.Names\}\}`:             Work,
		`docker ps --format "$F"`:                       Work,
		`docker ps --format "${F}"`:                     Work,
		`docker ps --format "a$"`:                       Work,
		"docker ps --format \"`id`\"":                   Work,
		"docker run app echo $HOME":                     Work,
		"docker run app ${X}":                           Work,
		"docker run app `id`":                           Work,
		"docker run app $'{a,b}'":                       Unsandboxed,
		"docker --version login":                        Work,
		"git fetch '--upload-pack=x' origin":            Work,
		`git fetch "--upl=x" origin`:                    Work,
		"git fetch '*' origin":                          Unsandboxed,
		"git -C '~/x' fetch":                            Work,
		"gh '{auth,}' token":                            Work,
		"gh api x -F 'b=@file'":                         Work,
		"gh issue create --title t --body-file 'b.md'":  Work,
		"gh pr list --search '$(id)' > /dev/null 2>&1":  Unsandboxed,
		`gh pr list --search "$(id)"`:                   Work,
	} {
		if got := ProfileFor(line); got != want {
			t.Errorf("ProfileFor(%q) = %q, want %q", line, got, want)
		}
	}
}

// T128h (L1332 K1): an unsandboxed docker line runs only when the docker
// and docker-compose the shell finds first on PATH are not files a
// sandboxed command could have written.
func TestRunsOutsideRefusesAPlantedDocker(t *testing.T) {
	d := with(machine, disk{
		"/usr/bin": "dir", "/usr/bin/bash": "file", "/usr/bin/docker": "file", "/usr/bin/docker-compose": "file", "/usr/bin/git": "file", "/usr/bin/gh": "file",
		"/home/u/project/bin": "dir", "/home/u/project/bin/docker": "file", "/home/u/project/bin/docker-compose": "file",
	})
	h := host("linux", d, nil)
	for _, c := range []struct {
		name string
		path []string
		line string
		want bool
	}{
		{"docker", []string{"/usr/bin"}, "docker compose up -d", true},
		{"docker-compose", []string{"/usr/bin"}, "docker-compose up -d", true},
		{"a planted docker", []string{"/home/u/project/bin", "/usr/bin"}, "docker ps", false},
		{"a planted docker-compose", []string{"/home/u/project/bin", "/usr/bin"}, "docker-compose ps", false},
		{"a planted docker after the real one", []string{"/usr/bin", "/home/u/project/bin"}, "docker ps", true},
		{"docker login", []string{"/usr/bin"}, "docker login", false},
		{"a planted docker sends git fetch to work", []string{"/home/u/project/bin", "/usr/bin"}, "git fetch", false},
		{"a planted docker sends gh pr list to work", []string{"/home/u/project/bin", "/usr/bin"}, "gh pr list", false},
	} {
		in := sample(Work, d)
		in.Path = slices.Clone(c.path)
		if got := RunsOutside(h, in, []string{"/usr/bin/bash", "-c", c.line}); got != c.want {
			t.Errorf("%s: RunsOutside(%q) = %v, want %v", c.name, c.line, got, c.want)
		}
	}
}
