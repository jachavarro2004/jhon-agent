package sandbox

// T251 (odd/tasks/v0-1-release.md L1721, L1722): a git repository inside a
// sandbox.allow_write folder counts as a project for a delivery line, which
// then runs outside the sandbox with the checks the pinned project's lines
// get; a delivery line kept inside says the real reason, not "run it alone".

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// t251Disk is the owner's machine: the session's project at
// /home/u/project, the second repository b inside the sandbox.allow_write
// folder /home/u/work, a folder there that is no repository, another
// repository c there, a repository outside every writable folder, and links
// to b and to the allowed folder.
var t251Disk = with(machine, disk{
	"/home/u/work": "dir", "/home/u/work/b": "dir", "/home/u/work/b/.git": "dir", "/home/u/work/b/sub": "dir",
	"/home/u/work/plain": "dir", "/home/u/work/c": "dir", "/home/u/work/c/.git": "dir",
	"/home/u/other": "dir", "/home/u/other/.git": "dir",
	"/home/u/blink": "-> /home/u/work/b", "/home/u/wlink": "-> /home/u/work",
	"/usr/bin": "dir", "/usr/bin/bash": "file", "/usr/bin/zsh": "file", "/usr/bin/git": "file", "/usr/bin/gh": "file", "/usr/bin/ssh": "file",
})

// t251Input is a session pinned to the project with /home/u/work in
// sandbox.allow_write, started in cwd.
func t251Input(cwd string, allow ...string) Input {
	in := sample(t251Disk)
	in.Cwd = cwd
	in.AllowWrite = allow
	in.Path = []string{"/usr/bin"}
	return in
}

// A delivery line in a repository inside an allow_write folder runs outside
// the sandbox, named with -C, a leading cd or run in it; one through a link,
// in no repository, outside every allowed folder, in two repositories, or a
// clone out of the repository, keeps the work profile. The project's own
// lines are as before, and ProfileAt (no allow_write) never counts another
// repository.
func TestT251ProfileInCountsARepositoryInsideAnAllowWriteFolder(t *testing.T) {
	const project, b = "/home/u/project", "/home/u/work/b"
	for _, c := range []struct{ cwd, line, want string }{
		{project, "git -C /home/u/work/b fetch origin", Unsandboxed},
		{project, "git -C /home/u/work/b/sub pull --ff-only", Unsandboxed},
		{project, "cd /home/u/work/b && git push origin main", Unsandboxed},
		{project, "git -C /home/u/work/b ls-remote origin", Unsandboxed},
		{project, "git -C /home/u/work/b clone https://h/o/r.git sub/r", Unsandboxed},
		{project, "git -C /home/u/work/b fetch && git -C /home/u/work/b/sub fetch", Unsandboxed},
		{project, "cd /home/u/work/b && gh pr list", Unsandboxed},
		{b, "git fetch origin", Unsandboxed},
		{b + "/sub", "git pull", Unsandboxed},
		{project, "git -C /home/u/work/b clone https://h/o/r.git ../r", Work},
		{project, "git -C /home/u/blink fetch", Work},
		{project, "cd /home/u/blink && git fetch", Work},
		{project, "git -C /home/u/wlink/b fetch", Work},
		{project, "git -C /home/u/other fetch", Work},
		{project, "git -C /home/u/work/plain fetch", Work},
		{project, "git -C /home/u/work fetch", Work},
		{project, "git fetch && git -C /home/u/work/b fetch", Work},
		{project, "git -C /home/u/work/b fetch && git -C /home/u/work/c fetch", Work},
		{project, "git -C /home/u/work/b fetch | cat", Work},
		// The project's lines, as before.
		{project, "git fetch", Unsandboxed},
		{project, "gh pr list", Unsandboxed},
		{project, "git -C /home/u/project-worktrees fetch", Unsandboxed},
	} {
		in := t251Input(c.cwd, "/home/u/work")
		if got := ProfileIn(in, c.line); got != c.want {
			t.Errorf("ProfileIn(%q in %s) = %q, want %q", c.line, c.cwd, got, c.want)
		}
	}
	// Only git and gh count the repository (L1722): docker and a deploy tool
	// run outside only from the project, as before.
	for line, want := range map[string]string{
		"cd /home/u/work/b && docker compose up":      Work,
		"cd /home/u/work/b && aws s3 ls":              Work,
		"cd /home/u/work/b && git fetch && aws s3 ls": Work,
		"cd /home/u/project && docker compose up":     Unsandboxed,
		"git -C /home/u/work/b fetch && gh pr list":   Unsandboxed,
	} {
		in := t251Input(project, "/home/u/work")
		in.DeployTools = []string{"aws"}
		if got := ProfileIn(in, line); got != want {
			t.Errorf("ProfileIn(%q) = %q, want %q", line, got, want)
		}
	}
	// No allow_write: the second repository is any other folder.
	if got := ProfileIn(t251Input(project), "git -C /home/u/work/b fetch"); got != Work {
		t.Errorf("with no allow_write: %q, want work", got)
	}
	if got := ProfileAt("git -C /home/u/work/b fetch", project, []string{project}, t251Disk.stat, nil); got != Work {
		t.Errorf("ProfileAt counts a repository of allow_write: %q", got)
	}
	// An allow_write folder the sandbox does not open for writing (the home,
	// a link) holds no repository that counts.
	for _, allow := range []string{"/home/u", "/home/u/wlink"} {
		for _, line := range []string{"git -C /home/u/other fetch", "git -C /home/u/wlink/b fetch", "git -C /home/u/work/b fetch"} {
			if got := ProfileIn(t251Input(project, allow), line); got != Work {
				t.Errorf("allow_write %s: ProfileIn(%q) = %q, want work", allow, line, got)
			}
		}
	}
}

// RunsOutside takes the line as ProfileIn does, with the shell's and the
// programs' own checks: a delivery line in b from the project's session
// runs outside, and a planted git keeps it inside as before.
func TestT251RunsOutsideForARepositoryInsideAnAllowWriteFolder(t *testing.T) {
	h := host("linux", t251Disk, nil)
	in := t251Input("/home/u/project", "/home/u/work")
	for line, want := range map[string]bool{
		"git -C /home/u/work/b fetch origin":       true,
		"cd /home/u/work/b && git pull --ff-only":  true,
		"git -C /home/u/blink fetch origin":        false,
		"git -C /home/u/other fetch origin":        false,
		"git fetch && git -C /home/u/work/b fetch": false,
	} {
		if got := RunsOutside(h, in, []string{"/usr/bin/bash", "-c", line}); got != want {
			t.Errorf("RunsOutside(%q) = %v, want %v", line, got, want)
		}
	}
	top, kept, ok := Outside(h, in, []string{"/usr/bin/bash", "-c", "git -C /home/u/work/b/sub fetch"})
	if top != "/home/u/work/b" || kept != nil || !ok {
		t.Errorf("Outside(b/sub) = %q, %v, %v; want b's top, no problem, true", top, kept, ok)
	}
	if top, kept, ok := Outside(h, in, []string{"/usr/bin/bash", "-c", "git fetch"}); top != "" || kept != nil || !ok {
		t.Errorf("Outside(the project) = %q, %v, %v; want no repository, no problem, true", top, kept, ok)
	}
	planted := with(t251Disk, disk{"/home/u/work/bin": "dir", "/home/u/work/bin/git": "file"})
	pin := t251Input("/home/u/project", "/home/u/work")
	pin.Stat, pin.Path = planted.stat, []string{"/home/u/work/bin", "/usr/bin"}
	if RunsOutside(host("linux", planted, nil), pin, []string{"/usr/bin/bash", "-c", "git -C /home/u/work/b fetch"}) {
		t.Error("a git planted in the allowed folder first on PATH runs outside")
	}
}

// keptPrefix opens every note of a delivery line that ran in the sandbox
// for a reason other than its shape.
const keptPrefix = "git over SSH and gh ran in the sandbox, not outside it: "

// The note of a failing delivery line kept inside names the real reason:
// the folder is neither the project nor in an allowed repository, it is
// reached through a link, it is in an allowed folder but in no repository,
// the line works in two repositories, a clone writes out of the repository,
// or the reason the run found (kept). A line kept inside for its shape keeps
// the "run it alone" note.
func TestT251TheNoteOfALineKeptInsideSaysTheRealReason(t *testing.T) {
	h := host("linux", t251Disk, nil)
	in := t251Input("/home/u/project", "/home/u/work")
	neither := "fix: run the line in a session of that repository (jhon start there), add a folder that holds the repository to sandbox.allow_write (jhon settings set sandbox.allow_write <folder>,…) if sandboxed commands may write it, or run the line yourself in a terminal"
	for _, c := range []struct{ line, want string }{
		{"git -C /home/u/other fetch origin", keptPrefix + "/home/u/other is neither in the project (/home/u/project) nor in a git repository inside a sandbox.allow_write folder; " + neither},
		{"git -C /home/u/blink fetch origin", keptPrefix + "/home/u/blink is or passes through the link /home/u/blink, and a line outside the sandbox reaches a repository only by a path with no link; fix: name its real path (/home/u/work/b) instead"},
		{"cd /home/u/wlink/b && git pull", keptPrefix + "/home/u/wlink/b is or passes through the link /home/u/wlink, and a line outside the sandbox reaches a repository only by a path with no link; fix: name its real path (/home/u/work/b) instead"},
		{"git -C /home/u/work/plain fetch", keptPrefix + "/home/u/work/plain is in the sandbox.allow_write folder /home/u/work but in no git repository inside it; fix: only a git repository there counts as a project: run the line in one, or run it yourself in a terminal"},
		{"git fetch && git -C /home/u/work/b push", keptPrefix + "the line works in more than one repository (/home/u/project and /home/u/work/b), and a line runs outside the sandbox in one only; fix: run it once for each repository, on a line of its own"},
		{"git -C /home/u/work/b clone https://h/o/r.git ../r", keptPrefix + "git clone writes /home/u/work/r, which is outside the repository it works in (/home/u/work/b); fix: clone into that repository, or run the line yourself in a terminal"},
		{"cd /home/u/work/b && git fetch && docker ps", keptPrefix + "docker runs outside the sandbox only in the project, and /home/u/work/b is in a git repository inside a sandbox.allow_write folder, where only git and gh do; fix: run docker on a line of its own, from the project or a session of that repository"},
		{"git -C /home/u/work/b fetch | cat", "git over SSH and gh run outside the sandbox only on a line of their own, with no pipes, $(…) or other commands: run it alone, as in git -C <dir> fetch origin main"},
		{"make", ""},
	} {
		if got := DeliveryNoteAt(h, in, []string{"/usr/bin/bash", "-c", c.line}, nil); got != c.want {
			t.Errorf("DeliveryNoteAt(%q) =\n %q\nwant\n %q", c.line, got, c.want)
		}
	}
	// The home as allow_write opens nothing for writing: a repository there
	// does not count.
	home := t251Input("/home/u/project", "/home/u")
	want := keptPrefix + "the repository /home/u/other lies in the sandbox.allow_write folder /home/u, which the sandbox does not open for writing there (jhon sandbox status says why), so it does not count as a project; fix: run the line yourself in a terminal"
	if got := DeliveryNoteAt(h, home, []string{"/usr/bin/bash", "-c", "git -C /home/u/other fetch"}, nil); got != want {
		t.Errorf("the home as allow_write:\n %q\nwant\n %q", got, want)
	}
	// zsh may take a relative cd folder from a cdpath.
	want = keptPrefix + "zsh may take the relative folder work/b of the leading cd from a cdpath; fix: name the folder by its absolute path, as in cd /path/to/repository && git fetch origin"
	if got := DeliveryNoteAt(h, t251Input("/home/u", "/home/u/work"), []string{"/usr/bin/zsh", "-c", "cd work/b && git fetch"}, nil); got != want {
		t.Errorf("zsh's relative cd:\n %q\nwant\n %q", got, want)
	}
	if !RunsOutside(h, t251Input("/home/u", "/home/u/work"), []string{"/usr/bin/bash", "-c", "cd work/b && git fetch"}) {
		t.Error("bash's relative cd into b does not run outside")
	}
	// Claude Code's wrap passes the command after the shell's $0.
	wrapped := []string{"/usr/bin/bash", "-c", `trap "x" EXIT; eval " $1"`, "/usr/bin/bash", "git -C /home/u/other fetch origin"}
	if got := DeliveryNoteAt(h, in, wrapped, nil); !strings.HasPrefix(got, keptPrefix+"/home/u/other is neither") {
		t.Errorf("the wrapped command: %q", got)
	}
	// A reason the run found wins.
	kept := &Problem{Reason: "the repository's configuration sets http.cookiefile, which is not among the keys a git line runs outside the sandbox with", Fix: "x"}
	if got := DeliveryNoteAt(h, in, []string{"/usr/bin/bash", "-c", "git -C /home/u/work/b fetch"}, kept); got != keptPrefix+kept.Line() {
		t.Errorf("a kept reason: %q", got)
	}
	if got := DeliveryNoteAt(h, in, []string{"/usr/bin/bash", "-c", "docker ps"}, kept); got != "" {
		t.Errorf("a line with no git or gh gets a note: %q", got)
	}
	// A planted program says so.
	planted := with(t251Disk, disk{"/home/u/project/bin": "dir", "/home/u/project/bin/git": "file"})
	pin := t251Input("/home/u/project", "/home/u/work")
	pin.Stat, pin.Path = planted.stat, []string{"/home/u/project/bin", "/usr/bin"}
	want = keptPrefix + "git on PATH is /home/u/project/bin/git, a file a sandboxed command may write; fix: take /home/u/project/bin off PATH, or put a folder that holds git and that no sandboxed command may write before it"
	if got := DeliveryNoteAt(host("linux", planted, nil), pin, []string{"/usr/bin/bash", "-c", "git fetch"}, nil); got != want {
		t.Errorf("a planted git:\n %q\nwant\n %q", got, want)
	}
}

// An outside line in b records b's top among the folders it reads, so a
// sandboxed run that can write b (any run with the allowed folder) waits
// for it, and it for them (Registry); the project's line records what it
// did.
func TestT251OutsideAreasHoldTheRepository(t *testing.T) {
	in := t251Input("/home/u/project", "/home/u/work")
	got := OutsideAreas(in, "git -C /home/u/work/b/sub fetch")
	if !slices.Contains(got, "/home/u/work/b") {
		t.Errorf("OutsideAreas = %q; want b's top among them", got)
	}
	if !shares(got, WritableAreas(in)) {
		t.Error("a sandboxed run with the allowed folder does not share an area with the line")
	}
	if got := OutsideAreas(in, "git fetch"); slices.ContainsFunc(got, func(a string) bool { return strings.HasPrefix(a, "/home/u/work") }) {
		t.Errorf("the project's line records the allowed folder: %q", got)
	}
}

// The git folders of a line in b must lie in b: the walk's rules hold, and
// a git folder that is the project's or another one's is not b's.
func TestT251TheGitFolderWalkOfARepositoryInsideAnAllowWriteFolder(t *testing.T) {
	base := t.TempDir()
	proj, work := filepath.Join(base, "project"), filepath.Join(base, "work")
	b, other := filepath.Join(work, "b"), filepath.Join(work, "c")
	for _, d := range []string{filepath.Join(proj, ".git"), filepath.Join(b, ".git", "refs"), filepath.Join(other, ".git")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	in := Input{Roots: []string{proj}, CommonDir: filepath.Join(proj, ".git"), Home: base, Stat: Disk, Lstat: DiskLstat, AllowWrite: []string{work}}
	git := filepath.Join(b, ".git")
	if why := in.LineGitFolderProblem(b, b, git, git); why != "" {
		t.Errorf("b's own git folder: %q", why)
	}
	if why := in.LineGitFolderProblem(b, b, filepath.Join(other, ".git"), filepath.Join(other, ".git")); why != "the repository's git folder "+filepath.Join(other, ".git")+" is not in the repository "+b {
		t.Errorf("another repository's git folder: %q", why)
	}
	if why := in.LineGitFolderProblem(b, b, filepath.Join(proj, ".git"), filepath.Join(proj, ".git")); why == "" {
		t.Error("the project's git folder passes for b's")
	}
	// The project's folder is judged as the project's.
	if why := in.LineGitFolderProblem(b, proj, filepath.Join(proj, ".git"), filepath.Join(proj, ".git")); why != "" {
		t.Errorf("the project's own folder: %q", why)
	}
	if err := os.Symlink(filepath.Join(base, "victim"), filepath.Join(git, "FETCH_HEAD")); err != nil {
		t.Fatal(err)
	}
	if why := in.LineGitFolderProblem(b, b, git, git); why != filepath.Join(git, "FETCH_HEAD")+" in the repository's git folder is a link, which git would write through" {
		t.Errorf("a link in b's git folder: %q", why)
	}
}
