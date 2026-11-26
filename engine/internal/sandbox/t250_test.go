package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// t250Disk is a scratch machine on the real disk (mayChange reads it): a
// home with a project and a cache, the tests' own temporary folder.
func t250Disk(t *testing.T) (base, home, proj string, in Input) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home = filepath.Join(base, "home", "u")
	proj = filepath.Join(home, "project")
	for _, d := range []string{filepath.Join(proj, ".git"), filepath.Join(home, ".cache"), filepath.Join(home, ".config")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	in = Input{Cwd: proj, Roots: []string{proj}, Home: home, Env: map[string]string{"HOME": home}, Stat: Disk, Lstat: DiskLstat, Path: []string{"/usr/bin"}}
	return base, home, proj, in
}

// A6: a cache a variable names that holds one of Jhon's own paths where a
// command could change it (a folder around it renamed) is not bound
// writable, in the home or not, and status says so; under any PATH it still
// counts as writable. One bound read-only directly inside the cache is a
// mount point no command moves (S10), and a cache that holds none is bound.
func TestACacheThatHoldsOneOfJhonsOwnPathsIsNotBoundWritable(t *testing.T) {
	base, home, _, in := t250Disk(t)
	local, share := filepath.Join(home, ".local"), filepath.Join(home, ".local", "share", "jhon")
	src := filepath.Join(base, "opt", "src", "jhon")
	for _, d := range []string{share, src, filepath.Join(base, "opt", "mod")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	in.Kept = []Kept{{What: "Jhon's release folder", Path: share}, {What: "Jhon's source checkout", Path: src}}
	for _, c := range []struct {
		env, cache string
		kept       *Kept
	}{
		{"GOMODCACHE", local, &in.Kept[0]},
		{"GOMODCACHE", filepath.Join(base, "opt"), &in.Kept[1]},
		// The release folder bound read-only directly inside the cache.
		{"GOMODCACHE", filepath.Join(home, ".local", "share"), nil},
		{"GOMODCACHE", filepath.Join(base, "opt", "mod"), nil},
	} {
		in.Env = map[string]string{"HOME": home, c.env: c.cache}
		args := Args(in)
		bound := index(args, "--bind", c.cache) >= 0
		if bound != (c.kept == nil) {
			t.Errorf("$%s=%s: bound writable %v, want %v:\n%s", c.env, c.cache, bound, c.kept == nil, strings.Join(args, " "))
		}
		if !slices.Contains(writable(in), c.cache) {
			t.Errorf("$%s=%s: writable, under any PATH, leaves it out: %v", c.env, c.cache, writable(in))
		}
		var unused []Unused
		for _, u := range UnusedWriteAreas(in) {
			if u.Kept != "" {
				unused = append(unused, u)
			}
		}
		if c.kept == nil {
			if len(unused) != 0 {
				t.Errorf("$%s=%s: UnusedWriteAreas = %+v, want none for a kept path", c.env, c.cache, unused)
			}
			continue
		}
		want := Unused{Name: "$" + c.env, Path: c.cache, Real: c.cache, Kept: c.kept.Path, KeptWhat: c.kept.What}
		if len(unused) != 1 || unused[0] != want {
			t.Fatalf("$%s=%s: UnusedWriteAreas = %+v, want [%+v]", c.env, c.cache, unused, want)
		}
		line := "$" + c.env + " " + c.cache + " is not used for writing: it holds " + c.kept.What + " (" + c.kept.Path + "), which a sandboxed command could replace by renaming a folder around it: point $" + c.env + " at a folder that holds none of Jhon's own files"
		if got := unused[0].Line(); got != line {
			t.Errorf("Line =\n%s\nwant\n%s", got, line)
		}
	}
	// The home itself is never bound writable: nothing to say of it.
	in.Env = map[string]string{"HOME": home, "GOMODCACHE": home}
	for _, u := range UnusedWriteAreas(in) {
		if u.Kept != "" {
			t.Errorf("the home as a cache is named: %+v", u)
		}
	}
}

// L43e: a PATH folder in another git repository (its work tree, its git
// folder, its <top>-worktrees) is left out of an outside line's PATH; one in
// no repository, and one under a home that holds a `.git`, stay.
func TestOutsidePathLeavesOutAnotherRepositorysFolders(t *testing.T) {
	base, home, _, in := t250Disk(t)
	other := filepath.Join(home, "other")
	inTree, inGit, inWorktrees := filepath.Join(other, "tools", "bin"), filepath.Join(other, ".git", "bin"), filepath.Join(home, "other-worktrees", "bin")
	bare := filepath.Join(base, "bare.git")
	plain, homeBin := filepath.Join(base, "opt", "bin"), filepath.Join(home, ".local", "bin")
	for _, d := range []string{inTree, inGit, inWorktrees, filepath.Join(bare, "objects"), filepath.Join(bare, "refs", "bin"), plain, homeBin, filepath.Join(other, ".git", "objects"), filepath.Join(other, ".git", "refs")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{filepath.Join(other, ".git", "HEAD"), filepath.Join(bare, "HEAD"), filepath.Join(home, ".git")} {
		if err := os.WriteFile(f, []byte("ref: refs/heads/main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	in.Path = []string{inTree, plain, inGit, inWorktrees, filepath.Join(bare, "refs", "bin"), homeBin, "/usr/bin"}
	if got, want := OutsidePath(in), plain+":"+homeBin+":/usr/bin"; got != want {
		t.Errorf("OutsidePath = %s\nwant %s", got, want)
	}
}

// L43c: the owner's keys read from a file a sandboxed command may change
// keep the line inside; from one it may not, or from the command line,
// they do not; the repository's keys are judged as before.
func TestGitConfigFromAFileACommandMayChangeKeepsTheLineInside(t *testing.T) {
	_, home, proj, in := t250Disk(t)
	inc, conf := filepath.Join(home, ".cache", "x.inc"), filepath.Join(home, ".config", "git", "config")
	entry := func(scope, origin, kv string) string { return scope + "\x00" + origin + "\x00" + kv + "\x00" }
	for _, c := range []struct {
		name, listing, reason string
	}{
		{"a global include in the cache", entry("global", "file:"+conf, "include.path\n"+inc) + entry("global", "file:"+inc, "core.gitproxy\n/x"), "git's global configuration reads " + inc + ", a file a sandboxed command may change, where it could name a program git runs outside the sandbox"},
		{"a system file in the project", entry("system", "file:"+filepath.Join(proj, "sys"), "user.name\nx"), "git's system configuration reads " + filepath.Join(proj, "sys") + ", a file a sandboxed command may change, where it could name a program git runs outside the sandbox"},
		{"a relative origin taken from the folder", entry("command", "file:.git/extra", "user.name\nx"), "git's command configuration reads " + filepath.Join(proj, ".git", "extra") + ", a file a sandboxed command may change, where it could name a program git runs outside the sandbox"},
		{"the owner's files", entry("global", "file:"+conf, "core.gitproxy\n/x") + entry("system", "file:/etc/gitconfig", "user.name\nx"), ""},
		{"the command line", entry("command", "command line:", "core.gitproxy\n/x"), ""},
		{"a repository key off the allowlist", entry("local", "file:.git/config", "core.gitproxy\n/x"), "the repository's configuration sets core.gitproxy, which is not among the keys a git line runs outside the sandbox with"},
		{"a repository key on it", entry("local", "file:.git/config", "user.name\nx"), ""},
		{"a listing cut short", "global\x00file:" + conf, "git listed its configuration in " + proj + " in a form Jhon cannot read"},
	} {
		p := in.GitConfigKept(c.listing, proj, nil)
		switch {
		case c.reason == "" && p != nil:
			t.Errorf("%s: %+v, want none", c.name, p)
		case c.reason != "" && (p == nil || p.Reason != c.reason):
			t.Errorf("%s: %+v, want %q", c.name, p, c.reason)
		}
	}
}

// L43d: docker's configuration folder, its plugins and the extra folders
// its config.json lists are judged where the line works; a value of
// another option (-H) is not taken as one.
func TestDockerConfigFoldersACommandMayChangeKeepTheLineInside(t *testing.T) {
	base, home, proj, in := t250Disk(t)
	safe := filepath.Join(base, "conf")
	if err := os.MkdirAll(safe, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		line, docker, folder string
	}{
		{"docker --config conf ps", "", filepath.Join(proj, "conf")},
		{"cd " + base + " && docker --config conf ps", "", ""},
		{"docker -H unix:///x --config=" + safe + " ps", "", ""},
		{"docker ps", filepath.Join(home, ".cache", "d"), filepath.Join(home, ".cache", "d")},
		{"docker ps", "", ""},
		{"docker-compose up", "rel", filepath.Join(proj, "rel")},
		{"git fetch", filepath.Join(home, ".cache", "d"), ""},
	} {
		in.Env = map[string]string{"HOME": home}
		if c.docker != "" {
			in.Env["DOCKER_CONFIG"] = c.docker
		}
		p := in.DockerConfigProblem(c.line)
		want := ""
		if c.folder != "" {
			want = "docker loads CLI plugins from " + c.folder + ", which a sandboxed command may change, so a plugin it put there would run outside the sandbox"
		}
		if got := ""; p != nil {
			got = p.Reason
			if got != want {
				t.Errorf("%q (DOCKER_CONFIG %q): %q, want %q", c.line, c.docker, got, want)
			}
		} else if want != "" {
			t.Errorf("%q (DOCKER_CONFIG %q): no problem, want %q", c.line, c.docker, want)
		}
	}
}

// L43a: the folders a pull works in, gh's merge that deletes the branch
// among them (gh then checks out and pulls the default branch where it
// runs), and the walk of its work tree: a .gitattributes or a .gitignore
// that is not a regular file anywhere in it, but in a git folder, which
// GitFolderProblem walks; each said for what it is.
func TestAPullsWorkTreeHoldsNoGitattributesGitWouldWaitOn(t *testing.T) {
	for _, c := range []struct {
		line string
		want []string
	}{
		{"git fetch && git -C a pull && git pull -q && git -C /p pull", []string{"/p/a", "/p"}},
		{"cd sub && git pull --ff-only", []string{"/p/sub"}},
		{"git fetch", nil},
		{"gh pr merge 5 --squash --delete-branch", []string{"/p"}},
		{"cd sub && gh pr merge 5 -d --squash", []string{"/p/sub"}},
		{"gh pr merge 5 -sd", []string{"/p"}},
		{"gh pr merge --delete-branch=true 5", []string{"/p"}},
		{"gh pr merge 5 --squash", nil},
		{"gh pr merge 5 --delete-branch=false", nil},
		{"gh pr view 5 --json title", nil},
		{"gh pr list --delete-branch", nil},
	} {
		if got := PullFolders(c.line, "/p"); !slices.Equal(got, c.want) {
			t.Errorf("PullFolders(%q) = %v, want %v", c.line, got, c.want)
		}
	}
	top, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{filepath.Join(top, ".git", "info"), filepath.Join(top, "a", "b"), filepath.Join(top, "c")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".gitattributes", ".gitignore"} {
		if err := os.WriteFile(filepath.Join(top, "a", name), []byte("*.o\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(filepath.Join(top, ".git", "info", name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if p := WorkTreeProblem(top); p != nil {
		t.Errorf("a work tree with a regular .gitattributes and .gitignore: %+v", p)
	}
	for _, name := range []string{".gitattributes", ".gitignore"} {
		pipe, link, folder := filepath.Join(top, "a", "b", name), filepath.Join(top, "c", name), filepath.Join(top, "a", "b", name)
		for _, c := range []struct {
			bad, want string
			make      func(string) error
		}{
			{pipe, pipe + " is not a regular file, and git would wait on it when it checks out a file in its folder", func(p string) error { return syscall.Mkfifo(p, 0o644) }},
			{link, link + " is a link, not a regular file: git does not read it (it warns), but a git before 2.32 follows it, to a pipe it would wait on too", func(p string) error { return os.Symlink(filepath.Join(top, "a", name), p) }},
			{folder, folder + " is not a regular file, which git cannot read as one", func(p string) error { return os.Mkdir(p, 0o755) }},
		} {
			if err := c.make(c.bad); err != nil {
				t.Fatal(err)
			}
			if p := WorkTreeProblem(top); p == nil || p.Reason != c.want || p.Fix != plantedFix {
				t.Errorf("WorkTreeProblem = %+v, want %q", p, c.want)
			}
			if err := os.Remove(c.bad); err != nil {
				t.Fatal(err)
			}
		}
	}
	if p := WorkTreeProblem("rel"); p == nil {
		t.Error("a relative work tree is taken")
	}
}

// L43f: the program a supervised run starts gets SIGKILL when the thread
// that started it ends.
func TestDieWithParentSetsTheParentDeathSignal(t *testing.T) {
	cmd := exec.Command("true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	DieWithParent(cmd)
	if cmd.SysProcAttr.Pdeathsig != syscall.SIGKILL || !cmd.SysProcAttr.Setpgid {
		t.Errorf("SysProcAttr = %+v, want Pdeathsig SIGKILL and the rest kept", cmd.SysProcAttr)
	}
}
