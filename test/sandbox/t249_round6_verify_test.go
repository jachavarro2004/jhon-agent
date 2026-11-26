package sandbox

// T249 round-6 verify (studio-integration-v1.md L41, fbe8741): whatever a
// sandboxed command writes into the names of the git folders (.git/commondir,
// a .git file, a worktree's admin folder) or the configuration that picks
// them, the next outside git line (fetch, pull --ff-only) writes nothing
// outside the project and runs no program outside. Real bubblewrap.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// commonDirPlant makes a copy of .git named "$PWD/c<tail>" (tail is a shell
// expression), names it in .git/commondir with content (a printf format
// whose %s is the folder), links its reflogs to ~/.bashrc, and keeps a
// clean decoy "c" beside it when the tail is not empty.
func commonDirPlant(tail, format string) string {
	return `t=` + tail + ` && c="$PWD/c$t" && cp -a .git "$c" && mkdir -p c/objects c/refs` +
		` && printf '` + format + `' "$c" > .git/commondir` +
		` && mkdir -p "$c/logs/refs/remotes/origin" "$c/logs/refs/heads" && ln -sfn "$HOME/.bashrc" "$c/logs/refs/remotes/origin/main" && ln -sfn "$HOME/.bashrc" "$c/logs/refs/heads/main"` +
		` && ln -sfn "$HOME/.bashrc" "$c/FETCH_HEAD"` +
		` && git config user.name 'x; echo injected; #'`
}

func TestRound6CommonDirNamesAreJudgedAsGitReadsThem(t *testing.T) {
	cases := map[string]string{
		"U+2028 at the end":       commonDirPlant(`"$(printf '\342\200\250')"`, `%s\n`),
		"U+3000 at the end":       commonDirPlant(`"$(printf '\343\200\200')"`, `%s\n`),
		"NEL at the end":          commonDirPlant(`"$(printf '\302\205')"`, `%s\n`),
		"CR at the end, stripped": commonDirPlant(`""`, `%s\r\n`),
		"space then CR":           commonDirPlant(`" "`, `%s\r\n`),
		"NUL inside":              commonDirPlant(`""`, `%s\000junk\n`),
		"no final newline":        commonDirPlant(`""`, `%s`),
		"trailing slash":          commonDirPlant(`" x"`, `%s/\n`),
		"dot-dot segments":        commonDirPlant(`" x"`, `%s/../c x\n`),
		"inner space only":        commonDirPlant(`" x"`, `%s\n`),
		"leading space in a part": `c="$PWD/ c" && cp -a .git "$c" && printf '../ c\n' > .git/commondir && mkdir -p "$c/logs/refs/remotes/origin" && ln -sfn "$HOME/.bashrc" "$c/logs/refs/remotes/origin/main" && ln -sfn "$HOME/.bashrc" "$c/FETCH_HEAD" && git config user.name 'x; echo injected; #'`,
		"relative with a link":    `cp -a .git real && ln -s real lnk && d="$PWD/real" && mkdir -p "$d/logs/refs/remotes/origin" && ln -sfn "$HOME/.bashrc" "$d/logs/refs/remotes/origin/main" && ln -sfn "$HOME/.bashrc" "$d/FETCH_HEAD" && printf '../lnk\n' > .git/commondir && git config user.name 'x; echo injected; #'`,
		"a newline inside":        `c="$PWD/c$(printf '\nx')" && cp -a .git "$c" && printf '%s\n' "$c" > .git/commondir && ln -sfn "$HOME/.bashrc" "$c/FETCH_HEAD"`,
	}
	for name, plant := range cases {
		for _, line := range []string{"git fetch origin", "git pull --ff-only"} {
			t.Run(name+"/"+line, func(t *testing.T) {
				b := newBox(t)
				note := filepath.Join(b.base, "ssh-ran")
				aheadOrigin(t, &b, note)
				support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
				r := plantThenRun(t, b, b.proj, plant, line)
				if got := support.Read(t, filepath.Join(b.home, ".bashrc")); got != "owner\n" {
					t.Errorf("%q (ran outside: %v, exit %d, stderr %q) wrote ~/.bashrc: %q", line, exists(note), r.Code, r.Stderr, got)
				}
			})
		}
	}
}

// .git replaced by a .git file whose gitdir line carries what git keeps (a
// trailing space, U+00A0) or strips (CR), with a clean decoy beside the
// folder git uses.
func TestRound6GitFileNamesAreJudgedAsGitReadsThem(t *testing.T) {
	gitFile := func(tail, format string) string {
		return `t=` + tail + ` && g="$PWD/g$t" && cp -a .git "$g" && mkdir -p g && cp -a .git/. g/ && rm -rf .git && printf '` + format + `' "$g" > .git` +
			` && mkdir -p "$g/logs/refs/remotes/origin" && ln -sfn "$HOME/.bashrc" "$g/logs/refs/remotes/origin/main" && ln -sfn "$HOME/.bashrc" "$g/FETCH_HEAD" && ln -sfn "$HOME/.bashrc" "$g/logs/HEAD"` +
			` && git config user.name 'x; echo injected; #'`
	}
	cases := map[string]string{
		"trailing space":                       gitFile(`" "`, `gitdir: %s\n`),
		"trailing U+00A0":                      gitFile(`"$(printf '\302\240')"`, `gitdir: %s\n`),
		"trailing U+2028":                      gitFile(`"$(printf '\342\200\250')"`, `gitdir: %s\n`),
		"CR, stripped":                         gitFile(`" x"`, `gitdir: %s\r\n`),
		"NUL inside":                           gitFile(`" x"`, `gitdir: %s\000/zz\n`),
		"no final newline":                     gitFile(`" x"`, `gitdir: %s`),
		"trailing slash":                       gitFile(`" x"`, `gitdir: %s/\n`),
		"dot-dot segments":                     gitFile(`" x"`, `gitdir: %s/../g x\n`),
		"gitdir in $TMPDIR":                    `g="$TMPDIR/g" && cp -a .git "$g" && rm -rf .git && printf 'gitdir: %s\n' "$g" > .git && ln -sfn "$HOME/.bashrc" "$g/FETCH_HEAD" && mkdir -p "$g/logs/refs/remotes/origin" && ln -sfn "$HOME/.bashrc" "$g/logs/refs/remotes/origin/main"`,
		"gitdir whose commondir is in $TMPDIR": `c="$TMPDIR/c" && cp -a .git "$c" && mkdir -p g && cp -a .git/. g/ && rm -rf .git && printf 'gitdir: %s\n' "$PWD/g" > .git && printf '%s\n' "$c" > g/commondir && ln -sfn "$HOME/.bashrc" "$c/FETCH_HEAD" && mkdir -p "$c/logs/refs/remotes/origin" && ln -sfn "$HOME/.bashrc" "$c/logs/refs/remotes/origin/main"`,
	}
	for name, plant := range cases {
		for _, line := range []string{"git fetch origin", "git pull --ff-only"} {
			t.Run(name+"/"+line, func(t *testing.T) {
				b := newBox(t)
				note := filepath.Join(b.base, "ssh-ran")
				aheadOrigin(t, &b, note)
				support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
				r := plantThenRun(t, b, b.proj, plant, line)
				if got := support.Read(t, filepath.Join(b.home, ".bashrc")); got != "owner\n" {
					t.Errorf("%q (ran outside: %v, exit %d, stderr %q) wrote ~/.bashrc: %q", line, exists(note), r.Code, r.Stderr, got)
				}
			})
		}
	}
}

// A linked worktree whose .git file (ReadRepo trims it) names a copy of its
// admin folder with white space at the end; and a worktree config that sets
// core.worktree outside the project, with extensions.worktreeConfig on.
func TestRound6LinkedWorktreeNamesAndWorktreeConfig(t *testing.T) {
	setup := func(t *testing.T) (box, string, string) {
		b := newBox(t)
		note := filepath.Join(b.base, "ssh-ran")
		aheadOrigin(t, &b, note)
		wt := filepath.Join(b.home, "project-wt")
		support.Git(t, b.proj, "worktree", "add", "-q", "-b", "wt", wt)
		support.Git(t, wt, "config", "branch.wt.remote", "origin")
		support.Git(t, wt, "config", "branch.wt.merge", "refs/heads/main")
		support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
		mkdirs(t, filepath.Join(b.home, "outside"))
		return b, wt, note
	}
	for name, plant := range map[string]string{
		"admin copy with a trailing space": `a=$(git rev-parse --git-dir) && cp -a "$a" "$a " && printf 'gitdir: %s \n' "$a" > .git && ln -sfn "$HOME/.bashrc" "$a /FETCH_HEAD" && mkdir -p "$a /logs" && ln -sfn "$HOME/.bashrc" "$a /logs/HEAD"`,
		"admin copy with a trailing tab":   `a=$(git rev-parse --git-dir) && cp -a "$a" "$a	" && printf 'gitdir: %s\t\n' "$a" > .git && ln -sfn "$HOME/.bashrc" "$a	/FETCH_HEAD"`,
		"admin commondir with U+3000":      `a=$(git rev-parse --git-dir) && c=$(git rev-parse --git-common-dir) && cp -a "$c" "$c$(printf '\343\200\200')" && printf '%s\343\200\200\n' "$c" > "$a/commondir" && ln -sfn "$HOME/.bashrc" "$c$(printf '\343\200\200')/FETCH_HEAD" && mkdir -p "$c$(printf '\343\200\200')/logs/refs/remotes/origin" && ln -sfn "$HOME/.bashrc" "$c$(printf '\343\200\200')/logs/refs/remotes/origin/main"`,
		"worktree config core.worktree":    `git config extensions.worktreeConfig true && git config --worktree core.worktree "$HOME/outside"`,
	} {
		for _, line := range []string{"git fetch origin", "git pull --ff-only"} {
			t.Run(name+"/"+line, func(t *testing.T) {
				b, wt, note := setup(t)
				r := plantThenRun(t, b, wt, plant+` && git config user.name 'x; echo injected; #'`, line)
				if got := support.Read(t, filepath.Join(b.home, ".bashrc")); got != "owner\n" {
					t.Errorf("%q (ran outside: %v, exit %d, stderr %q) wrote ~/.bashrc: %q", line, exists(note), r.Code, r.Stderr, got)
				}
				if got, _ := os.ReadDir(filepath.Join(b.home, "outside")); len(got) != 0 {
					t.Errorf("%q (ran outside: %v, exit %d, stderr %q) wrote into ~/outside: %v", line, exists(note), r.Code, r.Stderr, got)
				}
			})
		}
	}
}

// GIT_DIR and GIT_COMMON_DIR in the caller's environment: rev-parse and the
// line see the same ones; a link in the folder they name keeps the line in.
func TestRound6GitDirFromTheEnvironment(t *testing.T) {
	for _, k := range []string{"GIT_DIR", "GIT_COMMON_DIR"} {
		t.Run(k, func(t *testing.T) {
			b := newBox(t)
			note := filepath.Join(b.base, "ssh-ran")
			aheadOrigin(t, &b, note)
			support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
			if r := b.sh(t, b.proj, "", `ln -sfn "$HOME/.bashrc" .git/FETCH_HEAD && echo set`); r.Stdout != "set\n" {
				t.Fatalf("plant: %q %q", r.Stdout, r.Stderr)
			}
			env := append(append([]string{}, b.env...), k+"="+filepath.Join(b.proj, ".git"))
			r := support.Jhon(t, b.proj, env, "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", realShell(t, "sh"), "-c", "git fetch origin")
			if got := support.Read(t, filepath.Join(b.home, ".bashrc")); got != "owner\n" {
				t.Errorf("fetch with %s (ran outside: %v, exit %d, stderr %q) wrote ~/.bashrc: %q", k, exists(note), r.Code, r.Stderr, got)
			}
		})
	}
}

// Item 2 spot checks: a unix socket a sandboxed command binds in .git, and a
// FIFO under objects/ (where the hard-link rule does not look) keep the line
// inside, with the note.
func TestRound6SocketAndObjectsFIFOKeepTheLineInside(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("no python3")
	}
	for name, plant := range map[string]string{
		"socket in .git":     `python3 -c 'import socket; s=socket.socket(socket.AF_UNIX); s.bind(".git/sock")'`,
		"socket in refs":     `python3 -c 'import socket; s=socket.socket(socket.AF_UNIX); s.bind(".git/refs/heads/sock")'`,
		"FIFO in objects/xx": `mkdir -p .git/objects/ab && mkfifo .git/objects/ab/cdef`,
	} {
		t.Run(name, func(t *testing.T) {
			b := newBox(t)
			note := filepath.Join(b.base, "ssh-ran")
			aheadOrigin(t, &b, note)
			if r := b.sh(t, b.proj, "", plant+` && echo set`); r.Stdout != "set\n" {
				t.Fatalf("plant: %q %q", r.Stdout, r.Stderr)
			}
			r := b.unsandboxed(t, b.env, "git fetch origin")
			if exists(note) || !strings.Contains(r.Stderr, "is not a folder or a regular file; the line runs in the sandbox instead") {
				t.Errorf("fetch ran outside (%v) with %s: exit %d, stderr %q", exists(note), name, r.Code, r.Stderr)
			}
		})
	}
}
