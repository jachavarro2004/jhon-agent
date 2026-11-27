package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// T281 (S12): the session's working set is the project, its worktrees, the
// listed and joined members and the sandbox.allow_write folders; a
// repository declared nowhere, a listed member that has not joined and a
// joined sibling the primary does not list stay outside it, and the last
// one is said with the entry that lists it.

// fourRepos is a scratch home holding a primary app listing two members,
// lib (joined) and web (not joined), a sibling site that joined app but is
// not listed, a repository shared that only sandbox.allow_write names, and
// other, declared nowhere. The state and the settings are the test's own.
func fourRepos(t *testing.T) (base string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("JHON_SETTINGS", filepath.Join(base, "settings.toml"))
	for _, r := range []string{"app", "lib", "web", "site", "shared", "other"} {
		dir := filepath.Join(base, "work", r)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, out)
		}
	}
	write(t, filepath.Join(base, "work", "app", "openspec", "config.yaml"), "workspace:\n  lib: ../lib\n  web: ../web\n")
	write(t, filepath.Join(base, "settings.toml"), "[sandbox]\nallow_write = [\""+filepath.Join(base, "work", "shared")+"\"]\n")
	app := filepath.Join(base, "work", "app")
	for _, r := range []string{"lib", "site"} {
		if err := Join(filepath.Join(base, "work", r), app); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkingSetHoldsTheDeclaredRepositoriesAndNothingElse(t *testing.T) {
	base := fourRepos(t)
	w := func(p ...string) string { return filepath.Join(append([]string{base, "work"}, p...)...) }
	set := WorkingSet(w("app"))
	want := []string{w("app"), w("app-worktrees"), w("lib"), w("lib-worktrees"), w("shared")}
	if got := set.Roots(); !slices.Equal(got, want) {
		t.Errorf("roots = %q, want %q", got, want)
	}
	for _, p := range []string{w("app", "src", "a.py"), w("app-worktrees", "wt", "a.py"), w("lib", "a.py"), w("shared", "new", "b.py")} {
		if !set.Holds(p) {
			t.Errorf("%s is outside the working set", p)
		}
	}
	for _, p := range []string{w("web", "a.py"), w("site", "a.py"), w("other", "a.py"), filepath.Join(base, "home", "x")} {
		if set.Holds(p) {
			t.Errorf("%s is inside the working set", p)
		}
	}
	// A member's session sees the same members and the same shared folder.
	if got := WorkingSet(w("lib")).Roots(); !slices.Equal(got, []string{w("lib"), w("lib-worktrees"), w("app"), w("app-worktrees"), w("shared")}) {
		t.Errorf("from the member: %q", got)
	}
}

// A path that reaches the set only through a link is judged by where it
// leads; an allow_write folder that is a link, the home itself, or settings
// that cannot be read add nothing.
func TestTheWorkingSetJudgesRealPathsAndOpensNoLink(t *testing.T) {
	base := fourRepos(t)
	w := func(p ...string) string { return filepath.Join(append([]string{base, "work"}, p...)...) }
	if err := os.Symlink(w("other"), filepath.Join(base, "via")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(w("shared"), filepath.Join(base, "into")); err != nil {
		t.Fatal(err)
	}
	set := WorkingSet(w("app"))
	if set.Holds(filepath.Join(base, "via", "a.py")) {
		t.Error("a link to a repository declared nowhere is inside the set")
	}
	if !set.Holds(filepath.Join(base, "into", "a.py")) {
		t.Error("a link into the shared folder is outside the set, though its real path is in it")
	}
	write(t, filepath.Join(base, "settings.toml"), "[sandbox]\nallow_write = [\""+filepath.Join(base, "via")+"\", \"~/\"]\n")
	if got := AllowWriteFolders(); len(got) != 0 {
		t.Errorf("a link and the home opened: %q", got)
	}
	write(t, filepath.Join(base, "settings.toml"), "[sandbox]\nallow_write = \"not a list\"\n")
	if got := AllowWriteFolders(); len(got) != 0 {
		t.Errorf("unreadable settings opened %q", got)
	}
	// Without a workspace or settings, the set is the project, as before.
	write(t, filepath.Join(base, "settings.toml"), "")
	if got := WorkingSet(w("other")).Roots(); !slices.Equal(got, []string{w("other"), w("other-worktrees")}) {
		t.Errorf("a project with no workspace: %q", got)
	}
}

func TestAJoinedSiblingTheConfigDoesNotListIsSaidWithTheEntry(t *testing.T) {
	base := fourRepos(t)
	w := func(p ...string) string { return filepath.Join(append([]string{base, "work"}, p...)...) }
	u, ok := UnlistedAt(w("site", "src"))
	want := Unlisted{Root: w("site"), Primary: w("app"), Name: "site", Rel: "../site"}
	if !ok || u != want {
		t.Fatalf("UnlistedAt = %+v %v, want %+v", u, ok, want)
	}
	note := w("site") + " joined the workspace of " + w("app") + ", whose LDD config lists no path to it, so it is no member and stays outside the session's working set; to make it one, add `site: ../site` under `workspace:` in " + w("app", "openspec", "config.yaml") + " (or the owner runs `jhon workspace leave` in it)"
	if got := u.Note(); got != note {
		t.Errorf("note:\n %s\nwant\n %s", got, note)
	}
	if got := UnlistedOf(w("app")); len(got) != 1 || got[0] != want {
		t.Errorf("UnlistedOf = %+v", got)
	}
	// A joined member, a member not joined, and a repository with no pointer are none.
	for _, r := range []string{"lib", "web", "other", "app"} {
		if u, ok := UnlistedAt(w(r)); ok {
			t.Errorf("%s is said unlisted: %+v", r, u)
		}
	}
	// Once listed, it is a member and no longer said.
	write(t, w("app", "openspec", "config.yaml"), "workspace:\n  lib: ../lib\n  site: ../site\n")
	if u, ok := UnlistedAt(w("site")); ok {
		t.Errorf("a listed sibling is said unlisted: %+v", u)
	}
	if !WorkingSet(w("app")).Holds(w("site", "a.py")) {
		t.Error("a listed and joined sibling is outside the set")
	}
	if got := memberNameFor("My_Site.v2"); got != "my-site-v2" {
		t.Errorf("name = %q", got)
	}
}
