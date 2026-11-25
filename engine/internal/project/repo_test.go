package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// git runs git in dir with a fixed identity, failing the test when it fails.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitCmd(dir, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=jhon-test", "-c", "user.email=jhon-test@example.invalid", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GIT_") })
	return cmd
}

// gitRepo is the Repo of dir, or of its nearest folder that exists, as git
// gives it: its top folder and its common dir; the zero Repo where git names
// no work tree.
func gitRepo(dir string) Repo {
	for !Exists(dir) {
		dir = filepath.Dir(dir)
	}
	out, err := gitCmd(dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir").Output()
	top, common, two := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if err != nil || !two {
		return Repo{}
	}
	return Repo{Top: Realish(top), Common: Realish(common)}
}

func mkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// scratch is a scratch folder's real path, with the home at home/u in it.
func scratch(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", mkdir(t, filepath.Join(base, "home", "u")))
	return base
}

// noGit leaves PATH with no program at all: what ReadRepo answers after it,
// it read from the disk.
func noGit(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// ReadRepo reads from the disk what git answers for the layouts git itself
// makes: the same top folder and common dir, for every place a command may
// start in.
func TestReadRepoAgreesWithGit(t *testing.T) {
	base := scratch(t)
	main := mkdir(t, filepath.Join(base, "home", "u", "project"))
	git(t, main, "init", "-q")
	git(t, main, "commit", "-q", "--allow-empty", "-m", "init")
	deep := mkdir(t, filepath.Join(main, "a", "b"))
	linked := filepath.Join(base, "home", "u", "project-worktrees", "example-change")
	git(t, main, "worktree", "add", "-q", "-b", "example-change", linked)
	// A linked worktree whose gitdir files are relative, as git writes them
	// with worktree.useRelativePaths.
	relative := filepath.Join(base, "relative")
	git(t, main, "worktree", "add", "-q", "-b", "relative", relative)
	admin := filepath.Join(main, ".git", "worktrees", "relative")
	for file, text := range map[string]string{
		filepath.Join(relative, ".git"): "gitdir: ../home/u/project/.git/worktrees/relative\n",
		filepath.Join(admin, "gitdir"):  "../../../../../../relative/.git\n",
	} {
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bare := filepath.Join(base, "bare.git")
	git(t, base, "init", "-q", "--bare", bare)
	none := mkdir(t, filepath.Join(base, "none"))
	link := filepath.Join(base, "link")
	if err := os.Symlink(deep, link); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, dir string
		project   bool
	}{
		{"the top folder", main, true},
		{"a folder below it", deep, true},
		{"a folder that does not exist yet", filepath.Join(deep, "new", "x"), true},
		{"through a symlink", link, true},
		{"a linked worktree", linked, true},
		{"a linked worktree with relative gitdir files", relative, true},
		{"inside .git", filepath.Join(main, ".git", "hooks"), false},
		{"a bare repository", bare, false},
		{"no repository", none, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, want := ReadRepo(c.dir), gitRepo(c.dir)
			if got != want {
				t.Errorf("ReadRepo = %+v, git says %+v", got, want)
			}
			if (got.Top != "") != c.project {
				t.Errorf("ReadRepo = %+v; a project: want %v", got, c.project)
			}
			if c.project && !slices.Equal(got.Roots(), Roots(c.dir)) {
				t.Errorf("Roots = %v, Roots(dir) says %v", got.Roots(), Roots(c.dir))
			}
		})
	}
	// The listed worktrees, from every side, and the main worktree as a root
	// of a linked one: all without a git process.
	noGit(t)
	for _, dir := range []string{main, linked, relative} {
		if got := ReadRepo(dir).Worktrees(); !slices.Equal(got, []string{linked, relative}) {
			t.Errorf("Worktrees from %s = %v, want the two linked ones", dir, got)
		}
	}
	if got, want := ReadRepo(linked).Roots(), []string{linked, main, linked + "-worktrees", main + "-worktrees"}; !slices.Equal(got, want) {
		t.Errorf("Roots of the linked worktree = %v, want %v", got, want)
	}
}

// A repository whose top is the home folder is no project, as for Root.
func TestARepositoryAtTheHomeFolderIsNoProject(t *testing.T) {
	base := scratch(t)
	home := filepath.Join(base, "home", "u")
	git(t, home, "init", "-q")
	notes := mkdir(t, filepath.Join(home, "notes"))
	if got := ReadRepo(notes); got != (Repo{}) || Root(notes) != "" {
		t.Errorf("a repository at the home folder: %+v, want no project", got)
	}
}

// core.worktree and a config include move the top folder for git. The
// sandbox does not follow them, and asks no git: a command may write both,
// and the project is the folder that holds `.git` (T128a, L888, F3).
func TestCoreWorktreeAndAConfigIncludeAreNotFollowed(t *testing.T) {
	base := scratch(t)
	moved := mkdir(t, filepath.Join(base, "moved"))
	direct := mkdir(t, filepath.Join(base, "direct"))
	git(t, direct, "init", "-q")
	git(t, direct, "config", "core.worktree", moved)
	perWorktree := mkdir(t, filepath.Join(base, "per-worktree"))
	git(t, perWorktree, "init", "-q")
	if err := os.WriteFile(filepath.Join(perWorktree, ".git", "config.worktree"), []byte("[core]\n\tworktree = "+moved+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	included := mkdir(t, filepath.Join(base, "included"))
	git(t, included, "init", "-q")
	if err := os.WriteFile(filepath.Join(included, ".git", "moved.cfg"), []byte("[core]\n\tworktree = "+moved+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, included, "config", "include.path", "moved.cfg")
	t.Setenv("GIT_DIR", filepath.Join(direct, ".git"))
	t.Setenv("GIT_WORK_TREE", moved)
	noGit(t)
	for _, repo := range []string{direct, perWorktree, included} {
		want := Repo{Top: repo, Common: filepath.Join(repo, ".git")}
		if got := ReadRepo(repo); got != want || slices.Contains(got.Roots(), moved) {
			t.Errorf("ReadRepo(%s) = %+v with roots %v, want %+v", repo, got, got.Roots(), want)
		}
	}
}

// A worktree admin dir is the repository's own, but its gitdir file is a file
// inside the work tree, which a sandboxed command may write: a target that is
// not a linked worktree of this repository is no root of it (T128a, B2).
func TestAWorktreeGitdirPointingOutsideTheRepositoryIsNoWorktree(t *testing.T) {
	base := scratch(t)
	proj := mkdir(t, filepath.Join(base, "proj"))
	git(t, proj, "init")
	git(t, proj, "commit", "--allow-empty", "-m", "init")
	git(t, proj, "worktree", "add", filepath.Join(base, "wt"), "-b", "wt")
	elsewhere := mkdir(t, filepath.Join(base, "elsewhere"))
	admin, _ := filepath.Glob(filepath.Join(proj, ".git", "worktrees", "*"))
	if len(admin) != 1 {
		t.Fatalf("worktree admin dirs %v, want one", admin)
	}
	if err := os.WriteFile(filepath.Join(admin[0], "gitdir"), []byte(filepath.Join(elsewhere, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadRepo(proj).Worktrees(); len(got) != 0 {
		t.Errorf("worktrees %v, want none: %s is not the repository's", got, elsewhere)
	}
}

// An admin entry that is a link to another repository's admin folder lists
// nothing either: that repository's worktree points back at its own admin
// folder, not at this one (T128a, B2).
func TestAWorktreeAdminLinkToAnotherRepositoryIsNoWorktree(t *testing.T) {
	base := scratch(t)
	other := mkdir(t, filepath.Join(base, "other"))
	git(t, other, "init")
	git(t, other, "commit", "--allow-empty", "-m", "init")
	git(t, other, "worktree", "add", filepath.Join(base, "other-wt"), "-b", "wt")
	proj := mkdir(t, filepath.Join(base, "proj"))
	git(t, proj, "init")
	mkdir(t, filepath.Join(proj, ".git", "worktrees"))
	if err := os.Symlink(filepath.Join(other, ".git", "worktrees", "other-wt"), filepath.Join(proj, ".git", "worktrees", "planted")); err != nil {
		t.Fatal(err)
	}
	if got := ReadRepo(proj).Worktrees(); len(got) != 0 {
		t.Errorf("worktrees %v, want none: they are another repository's", got)
	}
}

// A `.git` file is a linked worktree's only when the gitdir it names points
// back at it: another repository's git dir, or the admin folder of another
// repository's worktree, makes no root here. The folder that holds the file
// is still the top (T128a, B2).
func TestAGitFileNotPointedBackAtIsNoWorktree(t *testing.T) {
	base := scratch(t)
	other := mkdir(t, filepath.Join(base, "other"))
	git(t, other, "init")
	git(t, other, "commit", "--allow-empty", "-m", "init")
	git(t, other, "worktree", "add", filepath.Join(base, "other-wt"), "-b", "wt")
	proj := mkdir(t, filepath.Join(base, "proj"))
	git(t, proj, "init")
	git(t, proj, "commit", "--allow-empty", "-m", "init")
	sub := mkdir(t, filepath.Join(proj, "sub"))
	for _, gitdir := range []string{filepath.Join(other, ".git"), filepath.Join(other, ".git", "worktrees", "other-wt")} {
		if err := os.WriteFile(filepath.Join(sub, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		repo := ReadRepo(sub)
		if want := (Repo{Top: sub}); repo != want || !slices.Equal(repo.Roots(), []string{sub, sub + "-worktrees"}) || len(repo.Worktrees()) != 0 {
			t.Errorf("gitdir: %s: ReadRepo = %+v, roots %v, worktrees %v; want %+v and no root outside it", gitdir, repo, repo.Roots(), repo.Worktrees(), want)
		}
	}
	// A `.git` link to another repository's git folder proves no more.
	if err := os.Remove(filepath.Join(sub, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, ".git"), filepath.Join(sub, ".git")); err != nil {
		t.Fatal(err)
	}
	if repo := ReadRepo(sub); repo != (Repo{Top: sub}) {
		t.Errorf("a .git link: ReadRepo = %+v, want the folder as the top and no common dir", repo)
	}
	// Nor does a commondir file written in the project's own git folder.
	if err := os.WriteFile(filepath.Join(proj, ".git", "commondir"), []byte(filepath.Join(other, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if repo := ReadRepo(proj); repo.Common == filepath.Join(other, ".git") || slices.Contains(repo.Roots(), other) {
		t.Errorf("a commondir file: ReadRepo = %+v, roots %v; want nothing of %s", repo, repo.Roots(), other)
	}
}

// RepoAt takes the folder it is given as the top, whatever is above it: a
// nested repository whose `.git` a command removed is still the project, and
// the enclosing repository is no root of it. What its own `.git` proves
// still counts: a linked worktree's common dir and main worktree.
func TestRepoAtPinsTheTopAndReadsOnlyItsOwnGitEntry(t *testing.T) {
	base := scratch(t)
	enclosing := mkdir(t, filepath.Join(base, "enclosing"))
	git(t, enclosing, "init", "-q")
	git(t, enclosing, "commit", "-q", "--allow-empty", "-m", "init")
	nested := mkdir(t, filepath.Join(enclosing, "nested"))
	git(t, nested, "init", "-q")
	linked := filepath.Join(base, "linked")
	git(t, enclosing, "worktree", "add", "-q", "-b", "example-change", linked)
	noGit(t)
	if got, want := RepoAt(nested), (Repo{Top: nested, Common: filepath.Join(nested, ".git")}); got != want {
		t.Errorf("RepoAt(nested) = %+v, want %+v", got, want)
	}
	if err := os.RemoveAll(filepath.Join(nested, ".git")); err != nil {
		t.Fatal(err)
	}
	if got := ReadRepo(nested); got.Top != enclosing {
		t.Errorf("ReadRepo(nested) without its .git = %+v, want the enclosing repository", got)
	}
	got := RepoAt(nested)
	if want := (Repo{Top: nested}); got != want || !slices.Equal(got.Roots(), []string{nested, nested + "-worktrees"}) || len(got.Worktrees()) != 0 {
		t.Errorf("RepoAt(nested) without its .git = %+v, roots %v, worktrees %v; want %+v and nothing of the enclosing repository", got, got.Roots(), got.Worktrees(), want)
	}
	if got, want := RepoAt(linked), (Repo{Top: linked, Common: filepath.Join(enclosing, ".git")}); got != want || !slices.Contains(got.Roots(), enclosing) {
		t.Errorf("RepoAt(linked) = %+v, roots %v; want %+v and the main worktree as a root", got, got.Roots(), want)
	}
	if got := RepoAt(filepath.Join(base, "home", "u")); got != (Repo{}) {
		t.Errorf("RepoAt(home) = %+v, want no project", got)
	}
}

// L41: a commondir file is read as git reads it, its trailing newline and
// carriage returns dropped and nothing else, so the common dir is the folder
// git uses even when its name ends in white space, never a decoy beside it.
func TestCommonOfKeepsTheWhiteSpaceGitKeeps(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows names no folder with white space at its end; the sandbox this guards runs on Linux")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	admin := filepath.Join(base, "admin")
	for _, tail := range []string{" ", "\t", "\u00a0"} {
		common := filepath.Join(base, "c"+tail)
		for _, d := range []string{admin, common, filepath.Join(base, "c")} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(admin, "commondir"), []byte(common+"\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := commonOf(admin); got != common {
			t.Errorf("commondir %q: commonOf = %q, want %q", common, got, common)
		}
	}
}
