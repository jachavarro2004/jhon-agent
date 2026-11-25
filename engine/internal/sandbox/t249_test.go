package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// T249 blocker 1: an allow_write folder outside the home whose parent a run
// replaced with a link to the home is not bound where the link leads; the
// home's own way, $HOME as written, still is the owner's.
func TestALinkToTheHomeElsewhereIsALinkLikeAnyOther(t *testing.T) {
	// The fake disk resolves no parent link: the area's own entry says where
	// its path leads.
	d := with(machine, disk{"/srv/aw": "dir", "/srv/aw/build": "-> /home/u", "/srv/aw/build/out": "-> /home/u/out", "/home/u/out": "dir"})
	in := sample(d)
	in.Env["HOME"] = "/home/u"
	in.AllowWrite = []string{"/srv/aw", "/srv/aw/build/out"}
	args := Args(in)
	if index(args, "--bind", "/home/u/out") >= 0 {
		t.Errorf("~/out is bound writable through /srv/aw/build -> /home/u:\n%s", strings.Join(args, " "))
	}
	if !slices.ContainsFunc(UnusedWriteAreas(in), func(u Unused) bool {
		return u.Path == "/srv/aw/build/out" && u.Link == "/srv/aw/build"
	}) {
		t.Errorf("UnusedWriteAreas = %+v; want /srv/aw/build/out through the link /srv/aw/build", UnusedWriteAreas(in))
	}
	// A home reached through /home -> /var/home as $HOME is written: the
	// folder under it is bound at its real path.
	d = disk{"/home": "-> /var/home", "/home/u": "-> /var/home/u", "/var/home": "dir", "/var/home/u": "dir", "/home/u/videos": "-> /var/home/u/videos", "/var/home/u/videos": "dir", "/tmp": "dir"}
	in = sample(d)
	in.Home, in.Env["HOME"], in.Roots, in.CommonDir = "/var/home/u", "/home/u", nil, ""
	in.AllowWrite = []string{"/home/u/videos"}
	if args := Args(in); index(args, "--bind", "/var/home/u/videos") < 0 {
		t.Errorf("an allow_write folder under $HOME through /home -> /var/home is not bound:\n%s", strings.Join(args, " "))
	}
}

// T249 blocker 2: the project's folders pass the no-link check of every
// writable area: <root>-worktrees planted as a link is not bound where it
// leads, and status names it.
func TestAProjectFolderThatIsALinkIsNotBound(t *testing.T) {
	d := with(machine, disk{"/home/u/project-worktrees": "-> /home/u/.config/systemd/user", "/home/u/.config/systemd/user": "dir"})
	in := sample(d)
	args := Args(in)
	if index(args, "--bind", "/home/u/.config/systemd/user") >= 0 {
		t.Errorf("the planted project-worktrees link is bound where it leads:\n%s", strings.Join(args, " "))
	}
	if index(args, "--bind", "/home/u/project") < 0 {
		t.Errorf("the project itself is no longer bound:\n%s", strings.Join(args, " "))
	}
	want := Unused{Name: "the project's folder", Path: "/home/u/project-worktrees", Link: "/home/u/project-worktrees", Real: "/home/u/.config/systemd/user"}
	if !slices.Contains(UnusedWriteAreas(in), want) {
		t.Errorf("UnusedWriteAreas = %+v; want %+v", UnusedWriteAreas(in), want)
	}
	// A linked root is not the project's when PATH folders are judged: a
	// cache that holds a PATH folder under its target is not bound.
	if got := in.ownRoots(); !slices.Equal(got, []string{"/home/u/project"}) {
		t.Errorf("ownRoots = %q; want the project alone", got)
	}
}

// T249 A1: a line outside the sandbox gets PATH without the project's
// folders and every folder a sandboxed command may change; the others keep
// their order.
func TestOutsidePathLeavesOutWhatACommandMayChange(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	proj, owner, tmp, cache := filepath.Join(home, "project"), filepath.Join(base, "owner", "bin"), filepath.Join(base, "tmp"), filepath.Join(home, ".cache")
	for _, dir := range []string{filepath.Join(proj, ".git"), filepath.Join(proj, "bin"), owner, filepath.Join(tmp, "bin"), filepath.Join(cache, "tool", "bin"), filepath.Join(home, ".local", "bin")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	in := Input{
		Cwd: proj, Roots: []string{proj, proj + "-worktrees"}, CommonDir: filepath.Join(proj, ".git"),
		Tmp: []string{tmp}, Home: home, Env: map[string]string{"HOME": home, "TMPDIR": tmp},
		Stat: Disk, Lstat: DiskLstat,
		Path: []string{filepath.Join(proj, "bin"), owner, "relative", filepath.Join(tmp, "bin"), filepath.Join(cache, "tool", "bin"), filepath.Join(home, ".local", "bin"), filepath.Join(proj, "node_modules", ".bin"), "/usr/bin"},
	}
	want := strings.Join([]string{owner, filepath.Join(home, ".local", "bin"), "/usr/bin"}, ":")
	if got := OutsidePath(in); got != want {
		t.Errorf("OutsidePath =\n%s\nwant\n%s", got, want)
	}
}

// T249 A2: the folders whose git configuration a line reads: where it works
// (a leading cd's folder) and each git's -C folder.
func TestGitFoldersAreWhereTheLineAndEachGitWork(t *testing.T) {
	for line, want := range map[string][]string{
		"git fetch origin":                          {"/p"},
		"git -C sub pull --ff-only":                 {"/p", "/p/sub"},
		"cd /p/w && git -C /p/x push && gh pr list": {"/p/w", "/p/x"},
		"aws s3 ls": {"/p"},
	} {
		if got := GitFolders(line, "/p"); !slices.Equal(got, want) {
			t.Errorf("GitFolders(%q) = %q; want %q", line, got, want)
		}
	}
	if got := GitFolders("git fetch", "relative"); got != nil {
		t.Errorf("a relative working folder: %q", got)
	}
}

// L35 ("Solo durante git de afuera"): every outside line waits for a
// sandboxed run; a sandboxed run waits only for an outside git (or gh)
// line; a record of no known kind holds every run.
func TestWhichRunHoldsWhich(t *testing.T) {
	for _, c := range []struct {
		other, self string
		want        bool
	}{
		{KindSandboxed, KindOutsideGit, true}, {KindSandboxed, KindOutside, true}, {KindSandboxed, KindSandboxed, false},
		{KindOutsideGit, KindSandboxed, true}, {KindOutsideGit, KindOutside, false}, {KindOutsideGit, KindOutsideGit, false},
		{KindOutside, KindSandboxed, false}, {KindOutside, KindOutside, false}, {KindOutside, KindOutsideGit, false},
		{"", KindSandboxed, true}, {"", KindOutside, true}, {"", KindOutsideGit, true},
	} {
		if got := holds(c.other, c.self); got != c.want {
			t.Errorf("holds(%q, %q) = %v, want %v", c.other, c.self, got, c.want)
		}
	}
	for line, want := range map[string]bool{
		"git pull --ff-only": true, "cd /p && git -C x fetch origin": true, "gh pr list": true,
		"aws s3 sync dist s3://x": false, "cdk deploy": false, "docker compose up -d": false,
	} {
		if got := RunsGit(line); got != want {
			t.Errorf("RunsGit(%q) = %v, want %v", line, got, want)
		}
	}
}
