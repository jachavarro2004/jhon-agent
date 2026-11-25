package release

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// layout builds the installed CLI's links under a scratch HOME:
// ~/.local/bin/jhon -> <share>/current/build/jhon, current -> releases/<c>.
func layout(t *testing.T) (home, link string) {
	t.Helper()
	home = t.TempDir()
	share := filepath.Join(home, ".local", "share", "jhon")
	release := filepath.Join(share, "releases", "abc")
	must(t, os.MkdirAll(filepath.Join(release, "build"), 0o755))
	must(t, os.WriteFile(filepath.Join(release, "build", "jhon"), nil, 0o755))
	must(t, os.Symlink(filepath.Join("releases", "abc"), filepath.Join(share, "current")))
	must(t, os.MkdirAll(filepath.Join(home, ".local", "bin"), 0o755))
	link = filepath.Join(home, ".local", "bin", "jhon")
	must(t, os.Symlink(filepath.Join(share, "current", "build", "jhon"), link))
	return home, link
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestHereKeepsCurrent(t *testing.T) {
	home, link := layout(t)
	want := filepath.Join(home, ".local", "share", "jhon", "current")
	if got := Here(link); got != want {
		t.Fatalf("Here(%s) = %s; want %s, the link current kept", link, got, want)
	}
	// A relative link resolves against its own directory, link by link.
	rel := filepath.Join(home, "rel")
	must(t, os.Symlink(filepath.Join(".local", "bin", "jhon"), rel))
	if got := Here(rel); got != want {
		t.Fatalf("Here(%s) = %s; want %s", rel, got, want)
	}
	// A path with no link is taken as it is.
	if got := Here("/x/y/build/jhon"); got != "/x/y" {
		t.Fatalf("Here(/x/y/build/jhon) = %s; want /x/y", got)
	}
}

func TestRootsSource(t *testing.T) {
	checkout := t.TempDir()
	must(t, os.Mkdir(filepath.Join(checkout, ".git"), 0o755))
	bare, agentsOnly, both := t.TempDir(), t.TempDir(), t.TempDir()
	must(t, os.MkdirAll(filepath.Join(agentsOnly, ".agents", "jhon"), 0o755))
	must(t, os.MkdirAll(filepath.Join(both, ".agents", "jhon"), 0o755))
	must(t, os.MkdirAll(filepath.Join(both, "dev", "jhon"), 0o755))
	for _, c := range []struct{ runtime, jhonSource, home, want string }{
		{checkout, "/elsewhere", agentsOnly, checkout},
		{"/release", "/elsewhere", agentsOnly, "/elsewhere"},
		{"/release", "", bare, bare + "/dev/jhon"},                 // L207: the source moved out of ~/.agents
		{"/release", "", agentsOnly, agentsOnly + "/.agents/jhon"}, // T56c: an old checkout still found
		{"/release", "", both, both + "/dev/jhon"},
	} {
		r := roots(c.runtime, c.jhonSource, c.home)
		if r.Source != c.want || r.Runtime != c.runtime || r.Share != c.home+"/.local/share/jhon" {
			t.Errorf("roots(%q, %q, %q) = %+v; want source %s", c.runtime, c.jhonSource, c.home, r, c.want)
		}
	}
}

// The checkout `jhon sync` last installed from, recorded in the install
// record, is the source when the runtime is no checkout and JHON_SOURCE is
// unset: ahead of the legacy ~/dev/jhon and ~/.agents/jhon, behind the two.
func TestRootsRecordedSource(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "") // each home's own state folder
	recorded := t.TempDir()
	record := func(home, source string) {
		t.Helper()
		p := filepath.Join(home, ".local", "state", "jhon", "install.json")
		must(t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(t, os.WriteFile(p, []byte(`{"installed_at":"2026-11-16T10:00:00.000Z","commit":"abc","source":"`+source+`"}`+"\n"), 0o644))
	}
	checkout := t.TempDir()
	must(t, os.Mkdir(filepath.Join(checkout, ".git"), 0o755))
	bare, both, gone := t.TempDir(), t.TempDir(), t.TempDir()
	record(bare, recorded)
	record(both, recorded)
	must(t, os.MkdirAll(filepath.Join(both, ".agents", "jhon"), 0o755))
	must(t, os.MkdirAll(filepath.Join(both, "dev", "jhon"), 0o755))
	record(gone, filepath.Join(gone, "removed"))
	for _, c := range []struct{ name, runtime, jhonSource, home, want string }{
		{"the recorded source when JHON_SOURCE is unset and the runtime is no checkout", "/release", "", bare, recorded},
		{"the recorded source ahead of the legacy paths", "/release", "", both, recorded},
		{"JHON_SOURCE ahead of the recorded source", "/release", "/elsewhere", bare, "/elsewhere"},
		{"a checkout runtime ahead of the recorded source", checkout, "", bare, checkout},
		{"a recorded source that is gone falls back to ~/dev/jhon", "/release", "", gone, gone + "/dev/jhon"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if r := roots(c.runtime, c.jhonSource, c.home); r.Source != c.want {
				t.Errorf("roots(%q, %q, %q).Source = %s; want %s", c.runtime, c.jhonSource, c.home, r.Source, c.want)
			}
		})
	}
}

// T249: the source the legacy install record (~/.cache/jhon/install.json,
// which any sandboxed command may write) names is never the source inside
// the sandbox, nor once a record is in the state folder.
func TestRootsNeverTrustALegacyRecordACommandCouldWrite(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "") // each home's own state folder
	owner, planted := t.TempDir(), t.TempDir()
	legacy := func(home, source string) {
		t.Helper()
		p := filepath.Join(home, ".cache", "jhon", "install.json")
		must(t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(t, os.WriteFile(p, []byte(`{"installed_at":"2026-11-16T10:00:00.000Z","commit":"abc","source":"`+source+`"}`+"\n"), 0o644))
	}
	inside := t.TempDir()
	legacy(inside, planted)
	t.Setenv("JHON_IN_SANDBOX", "1")
	if r := roots("/release", "", inside); r.Source != inside+"/dev/jhon" {
		t.Errorf("inside the sandbox the source is %s; want the fallback, not the legacy record's", r.Source)
	}
	t.Setenv("JHON_IN_SANDBOX", "")
	home := t.TempDir()
	legacy(home, owner)
	if r := roots("/release", "", home); r.Source != owner {
		t.Fatalf("the source is %s; want the legacy record moved once (%s)", r.Source, owner)
	}
	legacy(home, planted)
	if r := roots("/release", "", home); r.Source != owner {
		t.Errorf("a legacy record repointed after the move gives the source %s; want %s", r.Source, owner)
	}
}

func TestLocateHonoursJhonRoot(t *testing.T) {
	t.Setenv("JHON_ROOT", "/some/release")
	t.Setenv("JHON_SOURCE", "/some/source")
	if r := Locate(); r.Runtime != "/some/release" || r.Source != "/some/source" {
		t.Fatalf("Locate() = %+v", r)
	}
}

func TestTilde(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	for in, want := range map[string]string{"/home/u/x": "~/x", "/home/u": "~", "/home/uv": "~v", "/other": "/other"} {
		if got := Tilde(in); got != want {
			t.Errorf("Tilde(%s) = %s; want %s", in, got, want)
		}
	}
	t.Setenv("HOME", "")
	if got := Tilde("/a"); got != "~/a" {
		t.Errorf("Tilde with an empty HOME = %s; want ~/a", got)
	}
}

func TestReadlinkF(t *testing.T) {
	home, link := layout(t)
	share := filepath.Join(home, ".local", "share", "jhon")
	if p, ok := ReadlinkF(filepath.Join(share, "current")); !ok || filepath.Base(p) != "abc" {
		t.Errorf("ReadlinkF(current) = %s, %v; want the release", p, ok)
	}
	if p, ok := ReadlinkF(link); !ok || p != filepath.Join(share, "releases", "abc", "build", "jhon") {
		t.Errorf("ReadlinkF(link) = %s, %v", p, ok)
	}
	// The last component may be missing; a missing directory may not.
	if p, ok := ReadlinkF(filepath.Join(share, "gone")); !ok || filepath.Base(p) != "gone" {
		t.Errorf("ReadlinkF(missing leaf) = %s, %v", p, ok)
	}
	if _, ok := ReadlinkF(filepath.Join(home, "no", "such")); ok {
		t.Error("ReadlinkF resolved a path under a missing directory")
	}
}

func TestRealpathM(t *testing.T) {
	home, _ := layout(t)
	share := filepath.Join(home, ".local", "share", "jhon")
	if got, want := RealpathM(filepath.Join(share, "current", "hooks", "..", "x")), filepath.Join(share, "releases", "abc", "x"); got != want {
		t.Errorf("RealpathM = %s; want %s", got, want)
	}
	if got := RealpathM("/no/such/../dir"); got != "/no/dir" {
		t.Errorf("RealpathM(missing) = %s; want /no/dir", got)
	}
}

func TestLines(t *testing.T) {
	if got := Last(Substituted([]byte("a\nb\n\n"))); got != "b" {
		t.Errorf("Last = %q; want b", got)
	}
	if got := Lines(""); len(got) != 1 || got[0] != "" {
		t.Errorf("Lines(\"\") = %q; want one empty line", got)
	}
}

func TestActivateReplacesTheLink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "current")
	must(t, Activate("one", link))
	must(t, Activate("two", link))
	if target, _ := os.Readlink(link); target != "two" {
		t.Fatalf("current -> %s; want two", target)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("Activate left %d entries; want the link alone", len(entries))
	}
}

func TestStatus(t *testing.T) {
	if got := Status(exec.Command("sh", "-c", "exit 3").Run()); got != 3 {
		t.Errorf("Status(exit 3) = %d", got)
	}
	if got := Status(exec.Command("sh", "-c", "kill -TERM $$").Run()); got != 128+15 {
		t.Errorf("Status(SIGTERM) = %d; want 143", got)
	}
}

// A publish against scratch bare remotes: refused until HEAD is the active
// release, then pushed to origin, with mirror unconfigured and skipped.
func TestPublish(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	src := filepath.Join(home, "src")
	origin := filepath.Join(home, "origin.git")
	run := func(dir string, args ...string) string {
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@x", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	must(t, os.MkdirAll(src, 0o755))
	must(t, os.MkdirAll(filepath.Join(src, "engine", "cmd", "jhon"), 0o777))
	must(t, os.WriteFile(filepath.Join(src, SourceMarker), nil, 0o644))
	run(src, "init", "-q")
	run(src, "add", "-A")
	run(src, "commit", "-qm", "one")
	run(home, "init", "-q", "--bare", origin)
	run(src, "remote", "add", "origin", origin)
	r := Roots{Runtime: src, Source: src, Share: filepath.Join(home, ".local", "share", "jhon")}
	var out, errs bytes.Buffer
	o := &Out{W: &out, E: &errs}
	if code := Publish(r, o); code != 1 || !strings.Contains(errs.String(), "is not the active release ()") {
		t.Fatalf("Publish before sync = %d, %q", code, errs.String())
	}
	head := run(src, "rev-parse", "HEAD")
	must(t, os.MkdirAll(filepath.Join(r.Share, "releases", head), 0o755))
	must(t, Activate(filepath.Join(r.Share, "releases", head), filepath.Join(r.Share, "current")))
	out.Reset()
	errs.Reset()
	if code := Publish(r, o); code != 0 {
		t.Fatalf("Publish = %d, %q", code, errs.String())
	}
	if want := "pushing main to origin...\n  warn  mirror not configured, skipped\n"; out.String() != want {
		t.Errorf("Publish said %q; want %q", out.String(), want)
	}
	if got := run(origin, "rev-parse", "refs/heads/main"); got != head {
		t.Errorf("origin main = %s; want %s", got, head)
	}
}

// fakeRelease is a release whose build/jhon records its argv and JHON_ROOT,
// prints "fake renderer", and exits with code.
func fakeRelease(t *testing.T, code int) (release, seen string) {
	t.Helper()
	release = t.TempDir()
	seen = filepath.Join(t.TempDir(), "seen")
	bin := filepath.Join(release, Binary)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s %%s\\n' \"$JHON_ROOT\" \"$*\" > '%s'\necho fake renderer\nexit %d\n", seen, code)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return release, seen
}

func TestPreflightAndInstallRunTheReleasesOwnBinary(t *testing.T) {
	// The release's binary judges and installs it, never this process's: a
	// release that adds a command or changes the render is checked and
	// written by its own code.
	src := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(src, filepath.Dir(SourceMarker)), 0o755))
	must(t, os.WriteFile(filepath.Join(src, SourceMarker), nil, 0o644))
	for _, code := range []int{0, 3} {
		release, seen := fakeRelease(t, code)
		out, ok := Preflight(release, src)
		if ok != (code == 0) || !strings.Contains(out, "fake renderer") {
			t.Errorf("Preflight with exit %d: ok %v, out %q", code, ok, out)
		}
		b, _ := os.ReadFile(seen)
		if got, want := string(b), release+" render-engine --preflight --source "+src+"\n"; got != want {
			t.Errorf("Preflight ran %q, want %q", got, want)
		}
	}
	release, seen := fakeRelease(t, 0)
	if !Install(release, src) {
		t.Error("Install: a passing renderer failed")
	}
	b, _ := os.ReadFile(seen)
	if got, want := string(b), release+" render-engine --install --source "+src+"\n"; got != want {
		t.Errorf("Install ran %q, want %q", got, want)
	}
	// With no checkout (a user's install, L51) the release itself is the
	// --source, so nothing rendered names a source path that does not exist.
	release, seen = fakeRelease(t, 0)
	if !Install(release, "/no/such/checkout") {
		t.Error("Install with no checkout failed")
	}
	b, _ = os.ReadFile(seen)
	if got, want := string(b), release+" render-engine --install --source "+release+"\n"; got != want {
		t.Errorf("Install with no checkout ran %q, want %q", got, want)
	}
	failing, _ := fakeRelease(t, 1)
	if Install(failing, "/src") {
		t.Error("Install: a failing renderer passed")
	}
}
