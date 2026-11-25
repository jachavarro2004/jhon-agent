package sandbox

// T249 round-5 verify (studio-integration-v1.md L41), kept as regressions:
// whatever a sandboxed command leaves in the git folders, the work tree or
// the object store, the next outside git line (fetch, pull --ff-only)
// writes nothing outside the project and runs no program outside. Real
// bubblewrap.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// plantThenRun plants inside the sandbox in dir (a sandboxed command), then
// runs line there as the runtimes run an unsandboxed one, pinned to dir.
func plantThenRun(t *testing.T, b box, dir, plant, line string) support.Result {
	t.Helper()
	if r := b.sh(t, dir, "", plant+` && echo set`); r.Stdout != "set\n" {
		t.Fatalf("plant: exit %d, %q, %q", r.Code, r.Stdout, r.Stderr)
	}
	return support.Jhon(t, dir, b.env, "sandbox", "run", "--project", dir, "--profile", "unsandboxed", "--", realShell(t, "sh"), "-c", line)
}

// tree is every entry under dir with its content (files) or target
// (links), for a before/after comparison.
func tree(t *testing.T, dir string) string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case d.Type()&os.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			out = append(out, rel+" -> "+l)
		case d.Type().IsRegular():
			b, _ := os.ReadFile(p)
			out = append(out, rel+" = "+string(b))
		default:
			out = append(out, rel+"/")
		}
		return nil
	})
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// A common dir whose name ends in white space: git keeps the space
// (.git/commondir is read up to its newline), so the check must walk the
// folder git writes, not a decoy whose name lacks the space.
func TestACommonDirEndingInWhiteSpaceIsWalkedAsGitUsesIt(t *testing.T) {
	for name, tail := range map[string]string{"space": " ", "tab": "\t", "no-break space": "\u00a0"} {
		for _, line := range []string{"git fetch origin", "git pull --ff-only"} {
			t.Run(name+"/"+line, func(t *testing.T) {
				b := newBox(t)
				note := filepath.Join(b.base, "ssh-ran")
				aheadOrigin(t, &b, note)
				support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
				plant := `c="$PWD/c` + tail + `" && cp -a .git "$c" && mkdir -p c/objects c/refs && printf '%s\n' "$c" > .git/commondir` +
					` && mkdir -p "$c/logs/refs/remotes/origin" "$c/logs/refs/heads" && ln -sfn "$HOME/.bashrc" "$c/logs/refs/remotes/origin/main" && ln -sfn "$HOME/.bashrc" "$c/logs/refs/heads/main"` +
					` && git config user.name 'x; echo injected; #'`
				r := plantThenRun(t, b, b.proj, plant, line)
				if got := support.Read(t, filepath.Join(b.home, ".bashrc")); got != "owner\n" {
					t.Errorf("%q (ran outside: %v, exit %d, stderr %q) wrote ~/.bashrc through a link in the common dir: %q", line, exists(note), r.Code, r.Stderr, got)
				}
			})
		}
	}
}

// A linked worktree: its admin folder (the git dir), the main .git (the
// common dir) and the admin folder's commondir file, which a sandboxed
// command in the worktree can write.
func TestALinkedWorktreesGitFoldersAreJudged(t *testing.T) {
	setup := func(t *testing.T) (box, string, string) {
		b := newBox(t)
		note := filepath.Join(b.base, "ssh-ran")
		aheadOrigin(t, &b, note)
		wt := filepath.Join(b.home, "project-wt")
		support.Git(t, b.proj, "worktree", "add", "-q", "-b", "wt", wt)
		support.Git(t, wt, "config", "branch.wt.remote", "origin")
		support.Git(t, wt, "config", "branch.wt.merge", "refs/heads/main")
		support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
		return b, wt, note
	}
	for name, plant := range map[string]string{
		"admin FETCH_HEAD":   `ln -s "$HOME/.bashrc" "$(git rev-parse --git-dir)/FETCH_HEAD"`,
		"admin logs/HEAD":    `mkdir -p "$(git rev-parse --git-dir)/logs" && ln -sfn "$HOME/.bashrc" "$(git rev-parse --git-dir)/logs/HEAD"`,
		"common remote log":  `d=$(git rev-parse --git-common-dir) && mkdir -p "$d/logs/refs/remotes/origin" && ln -sfn "$HOME/.bashrc" "$d/logs/refs/remotes/origin/main"`,
		"common branch log":  `d=$(git rev-parse --git-common-dir) && ln -sfn "$HOME/.bashrc" "$d/logs/refs/heads/wt"`,
		"admin gitdir file":  `ln -sfn "$HOME/.bashrc" "$(git rev-parse --git-dir)/gitdir.new" && mv "$(git rev-parse --git-dir)/gitdir.new" "$(git rev-parse --git-dir)/gitdir"`,
		"worktree .git file": `g=$(git rev-parse --git-dir) && rm .git && ln -s "$g" .git`,
	} {
		for _, line := range []string{"git fetch origin", "git pull --ff-only"} {
			t.Run(name+"/"+line, func(t *testing.T) {
				b, wt, note := setup(t)
				r := plantThenRun(t, b, wt, plant+` && git config user.name 'x; echo injected; #'`, line)
				if got := support.Read(t, filepath.Join(b.home, ".bashrc")); got != "owner\n" {
					t.Errorf("%q (ran outside: %v, exit %d, stderr %q) wrote ~/.bashrc: %q", line, exists(note), r.Code, r.Stderr, got)
				}
			})
		}
	}
	t.Run("admin commondir names another repository", func(t *testing.T) {
		b, wt, note := setup(t)
		victim := filepath.Join(b.home, "victim")
		mkdirs(t, victim)
		support.GitRepo(t, victim)
		support.Git(t, victim, "remote", "add", "origin", "ssh://example.invalid"+filepath.Join(b.base, "remote.git"))
		before := tree(t, filepath.Join(victim, ".git", "refs")) + tree(t, filepath.Join(victim, ".git", "logs"))
		r := plantThenRun(t, b, wt, `printf '%s\n' "$HOME/victim/.git" > "$(git rev-parse --git-dir)/commondir"`, "git fetch origin")
		if after := tree(t, filepath.Join(victim, ".git", "refs")) + tree(t, filepath.Join(victim, ".git", "logs")); after != before {
			t.Errorf("the fetch (ran outside: %v, exit %d, stderr %q) wrote another repository's refs:\n%s\nwas\n%s", exists(note), r.Code, r.Stderr, after, before)
		}
	})
}

// A pull's checkout and a link the sandbox left in the work tree: an
// untracked link where the pull makes a folder, a tracked file swapped for
// a link and marked assume-unchanged.
func TestAPullsCheckoutWritesThroughNoLinkInTheWorkTree(t *testing.T) {
	setup := func(t *testing.T) (box, string) {
		b := newBox(t)
		note := filepath.Join(b.base, "ssh-ran")
		remote := originOverSSH(t, &b, note)
		support.Write(t, b.proj, map[string]string{"t.txt": "v1\n"})
		support.Git(t, b.proj, "add", "t.txt")
		support.Git(t, b.proj, "commit", "-q", "-m", "v1")
		support.Write(t, b.proj, map[string]string{"t.txt": "v2\n", "d/new.txt": "new\n"})
		support.Git(t, b.proj, "add", "t.txt", "d/new.txt")
		support.Git(t, b.proj, "commit", "-q", "-m", "v2")
		support.Git(t, b.proj, "push", "-q", remote, "HEAD:refs/heads/main")
		support.Git(t, b.proj, "reset", "-q", "--hard", "HEAD~1")
		support.Write(t, b.home, map[string]string{".bashrc": "owner\n", ".config/systemd/user/keep": "x"})
		return b, note
	}
	for name, plant := range map[string]string{
		"untracked folder link":      `ln -s "$HOME/.config/systemd/user" d`,
		"tracked file link, assumed": `rm t.txt && ln -s "$HOME/.bashrc" t.txt && git update-index --assume-unchanged t.txt`,
		"tracked file link, skip-wt": `rm t.txt && ln -s "$HOME/.bashrc" t.txt && git update-index --skip-worktree t.txt`,
	} {
		t.Run(name, func(t *testing.T) {
			b, note := setup(t)
			r := plantThenRun(t, b, b.proj, plant, "git pull --ff-only")
			if got := support.Read(t, filepath.Join(b.home, ".bashrc")); got != "owner\n" {
				t.Errorf("pull (ran outside: %v, exit %d, stderr %q) wrote ~/.bashrc: %q", exists(note), r.Code, r.Stderr, got)
			}
			if p := filepath.Join(b.home, ".config", "systemd", "user", "new.txt"); exists(p) {
				t.Errorf("pull (ran outside: %v, exit %d, stderr %q) made %s", exists(note), r.Code, r.Stderr, p)
			}
		})
	}
}

// The object store: a link inside objects/ where the fetch writes a loose
// object, and objects/info/alternates naming a store a sandboxed command
// can write (its $TMPDIR) or a folder of the home.
func TestTheObjectStoreLeadsNowhereOutside(t *testing.T) {
	t.Run("a link inside objects", func(t *testing.T) {
		b := newBox(t)
		note := filepath.Join(b.base, "ssh-ran")
		aheadOrigin(t, &b, note)
		id := strings.TrimSpace(support.Git(t, filepath.Join(b.base, "remote.git"), "rev-parse", "main"))
		mkdirs(t, filepath.Join(b.home, ".config", "objdir"))
		r := plantThenRun(t, b, b.proj, `rm -rf .git/objects/`+id[:2]+` && ln -s "$HOME/.config/objdir" .git/objects/`+id[:2], "git fetch origin")
		if got := tree(t, filepath.Join(b.home, ".config", "objdir")); got != "." && got != "./" {
			t.Errorf("fetch (ran outside: %v, exit %d, stderr %q) wrote through the objects link:\n%s", exists(note), r.Code, r.Stderr, got)
		}
	})
	for name, store := range map[string]string{"in $TMPDIR": "$TMPDIR/store", "in the home": "$HOME/store"} {
		t.Run("alternates "+name, func(t *testing.T) {
			b := newBox(t)
			note := filepath.Join(b.base, "ssh-ran")
			aheadOrigin(t, &b, note)
			dir := strings.NewReplacer("$TMPDIR", b.tmp, "$HOME", b.home).Replace(store)
			mkdirs(t, filepath.Join(dir, "objects", "info"), filepath.Join(dir, "objects", "pack"))
			before := tree(t, dir)
			r := plantThenRun(t, b, b.proj, `printf '%s\n' "`+store+`/objects" > .git/objects/info/alternates`, "git fetch origin && git pull --ff-only")
			if after := tree(t, dir); after != before {
				t.Errorf("fetch+pull (ran outside: %v, exit %d, stderr %q) wrote into the alternate store:\n%s", exists(note), r.Code, r.Stderr, after)
			}
		})
	}
}

// Hard links: none can be made from inside to a file of the home (another
// mount); one already there sends the line to the sandbox.
func TestAHardLinkInTheGitFolderKeepsTheLineInside(t *testing.T) {
	b := newBox(t)
	note := filepath.Join(b.base, "ssh-ran")
	aheadOrigin(t, &b, note)
	support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
	if r := b.sh(t, b.proj, "", `ln "$HOME/.bashrc" .git/FETCH_HEAD 2>/dev/null && echo linked || echo refused`); r.Stdout != "refused\n" {
		t.Errorf("a sandboxed command hard-linked a home file into .git: %q", r.Stdout)
	}
	os.Remove(filepath.Join(b.proj, ".git", "FETCH_HEAD"))
	if err := os.Link(filepath.Join(b.home, ".bashrc"), filepath.Join(b.proj, ".git", "FETCH_HEAD")); err != nil {
		t.Skipf("no hard link: %v", err)
	}
	// The line must not run outside (inside, the sandbox itself writes a
	// hard-linked file in place: T250 A5, pre-existing).
	r := b.unsandboxed(t, b.env, "git fetch origin")
	if exists(note) || !strings.Contains(r.Stderr, "shares its data with another file") {
		t.Errorf("fetch ran outside (%v) with a hard-linked FETCH_HEAD: exit %d, stderr %q", exists(note), r.Code, r.Stderr)
	}
}

// A FIFO (and a device node) in the git folder: a FIFO leads nowhere
// outside, but an outside line that opens it waits for a reader for as long
// as nobody reads it, holding every sandboxed command of the project. The
// line runs in the sandbox instead, with the note (L41); there it may wait
// too, as any sandboxed command may, holding only outside lines.
func TestAFIFOInTheGitFolderKeepsTheLineInside(t *testing.T) {
	b := newBox(t)
	note := filepath.Join(b.base, "ssh-ran")
	aheadOrigin(t, &b, note)
	if r := b.sh(t, b.proj, "", `mknod .git/null c 1 3 2>/dev/null && echo made || echo refused`); r.Stdout != "refused\n" {
		t.Errorf("a sandboxed command made a device node in .git: %q", r.Stdout)
	}
	if r := b.sh(t, b.proj, "", `mkfifo .git/FETCH_HEAD && echo set`); r.Stdout != "set\n" {
		t.Fatalf("plant: %q %q", r.Stdout, r.Stderr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, support.Bin(t), "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", realShell(t, "sh"), "-c", "git fetch origin")
	var stderr strings.Builder
	cmd.Dir, cmd.Env, cmd.Stderr = b.proj, b.env, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		// The sandboxed fetch waits on the FIFO: read it.
		if f, err := os.OpenFile(filepath.Join(b.proj, ".git", "FETCH_HEAD"), os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
			time.Sleep(time.Second)
			buf := make([]byte, 4096)
			f.Read(buf)
			f.Close()
		}
		<-done
	}
	want := "jhon sandbox: " + filepath.Join(b.proj, ".git", "FETCH_HEAD") + " in the repository's git folder is not a folder or a regular file; the line runs in the sandbox instead\n"
	if exists(note) || !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("ran outside %v, stderr %q; want the line in the sandbox, opening with %q", exists(note), stderr.String(), want)
	}
}
