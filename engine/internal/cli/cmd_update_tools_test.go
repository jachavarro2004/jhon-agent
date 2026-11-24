package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/update"
)

// T242 (odd/tasks/studio-integration-v1.md S5): jhon update updates an on
// jhon-studio installed from its releases after jhon itself, --rollback
// links its previous version again, and doctor says where it came from. The
// download and verification are internal/update's (tool_test.go); here the
// releases API is an httptest server and nothing newer is published, so no
// signature is asked.

const studioOn = "schema = 1\n\n[integrations]\nstudio = true\n"

// toolHome is a scratch HOME with settings text, jhon at v, and, for each
// version, ~/.local/share/jhon-studio/<version>/bin/jhon-studio, the link
// at active ("" for none). It returns the HOME.
func toolHome(t *testing.T, text, v, active string, versions ...string) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	t.Setenv("HOME", home)
	path := filepath.Join(home, "settings.toml")
	must(t, os.WriteFile(path, []byte(text), 0o600))
	t.Setenv("JHON_SETTINGS", path)
	t.Setenv("JHON_ROOT", t.TempDir())
	jhonIs(t, v)
	share := filepath.Join(home, ".local", "share", "jhon-studio")
	for _, ver := range versions {
		must(t, os.MkdirAll(filepath.Join(share, ver, "bin"), 0o755))
		must(t, os.WriteFile(filepath.Join(share, ver, "bin", "jhon-studio"), []byte("#!/bin/sh\necho "+ver+"\n"), 0o755))
	}
	if active != "" {
		must(t, os.MkdirAll(filepath.Join(home, ".local", "bin"), 0o755))
		must(t, os.Symlink(filepath.Join(share, active, "bin", "jhon-studio"), filepath.Join(home, ".local", "bin", "jhon-studio")))
	}
	return home
}

// releasesServed serves jhon's latest release as none yet (404) and
// jhon-studio's releases as body with status code; JHON_RELEASES_URL and
// the tool client point at it.
func releasesServed(t *testing.T, code int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/jachavarro2004/jhon-studio/releases" {
			w.WriteHeader(code)
			w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("JHON_RELEASES_URL", srv.URL+"/releases/latest")
	saved := toolClient
	t.Cleanup(func() { toolClient = saved })
	toolClient = func() *update.Client { return &update.Client{URL: srv.URL, HTTP: srv.Client()} }
}

func linkOf(t *testing.T, home string) string {
	t.Helper()
	l, err := os.Readlink(filepath.Join(home, ".local", "bin", "jhon-studio"))
	must(t, err)
	return l
}

func TestUpdateUpdatesAnOnToolAfterJhon(t *testing.T) {
	toolHome(t, studioOn, "0.5.0", "0.1.0", "0.1.0")
	releasesServed(t, http.StatusOK, `[{"tag_name":"v0.1.0","prerelease":false,"assets":[]}]`)
	code, out, errOut := captureAll(t, []string{"update"})
	want := "no stable release is published yet; jhon 0.5.0 is installed\n" +
		"jhon-studio 0.1.0 is up to date (the newest release of github.com/jachavarro2004/jhon-studio that supports jhon 0.5.0)\n"
	if code != 0 || out != want || errOut != "" {
		t.Errorf("exit %d\nstdout %q\nwant   %q\nstderr %q", code, out, want, errOut)
	}
	code, out, _ = captureAll(t, []string{"update", "--json"})
	if want := `{"current":"0.5.0","status":"no-release","tools":[{"name":"jhon-studio","current":"0.1.0","latest":"0.1.0","status":"up-to-date"}]}` + "\n"; code != 0 || out != want {
		t.Errorf("--json: exit %d\n got %q\nwant %q", code, out, want)
	}
}

func TestUpdateSaysAToolItCouldNotUpdateAndExits1(t *testing.T) {
	home := toolHome(t, studioOn, "0.5.0", "0.1.0", "0.1.0")
	releasesServed(t, http.StatusInternalServerError, "down")
	code, out, errOut := captureAll(t, []string{"update"})
	if code != 1 || out != "no stable release is published yet; jhon 0.5.0 is installed\n" ||
		!strings.HasPrefix(errOut, "jhon: jhon-studio: ") || !strings.HasSuffix(errOut, " answered 500 Internal Server Error; nothing was changed\n") {
		t.Errorf("exit %d\nstdout %q\nstderr %q", code, out, errOut)
	}
	if l := linkOf(t, home); !strings.HasSuffix(l, "/0.1.0/bin/jhon-studio") {
		t.Errorf("link moved: %s", l)
	}
	code, out, _ = captureAll(t, []string{"update", "--json"})
	if !strings.HasPrefix(out, `{"current":"0.5.0","status":"no-release","tools":[{"name":"jhon-studio","current":"0.1.0","status":"failed","error":"`) || code != 1 {
		t.Errorf("--json: exit %d %q", code, out)
	}
}

// A development clone is said and left alone; nothing asks the network.
func TestUpdateLeavesADevelopmentCloneAlone(t *testing.T) {
	home := toolHome(t, studioOn, "0.5.0", "")
	must(t, os.MkdirAll(filepath.Join(home, ".local", "share", "jhon-studio", ".git"), 0o755))
	releasesServed(t, http.StatusInternalServerError, "must not be asked")
	code, out, errOut := captureAll(t, []string{"update"})
	want := "no stable release is published yet; jhon 0.5.0 is installed\njhon-studio is a development clone at ~/.local/share/jhon-studio: jhon update leaves it alone\n"
	if code != 0 || out != want || errOut != "" {
		t.Errorf("exit %d\nstdout %q\nstderr %q", code, out, errOut)
	}
}

// PRESERVE: with studio off, jhon update says what it said, and --json has
// no tools.
func TestUpdateWithNoToolOnIsUnchanged(t *testing.T) {
	toolHome(t, "schema = 1\n", "0.5.0", "0.1.0", "0.1.0")
	releasesServed(t, http.StatusInternalServerError, "must not be asked")
	code, out, errOut := captureAll(t, []string{"update"})
	if code != 0 || out != "no stable release is published yet; jhon 0.5.0 is installed\n" || errOut != "" {
		t.Errorf("exit %d\nstdout %q\nstderr %q", code, out, errOut)
	}
	code, out, _ = captureAll(t, []string{"update", "--json"})
	if code != 0 || out != `{"current":"0.5.0","status":"no-release"}`+"\n" {
		t.Errorf("--json: exit %d %q", code, out)
	}
}

func TestRollbackLinksAToolsPreviousVersion(t *testing.T) {
	home := toolHome(t, studioOn, "0.5.0", "0.2.0", "0.1.0", "0.2.0")
	code, out, errOut := captureAll(t, []string{"update", "--rollback"})
	want := "no release older than jhon 0.5.0 is kept; jhon stays at 0.5.0\nrolled back jhon-studio 0.2.0 -> 0.1.0\n"
	if code != 0 || out != want || errOut != "" {
		t.Errorf("exit %d\nstdout %q\nwant   %q\nstderr %q", code, out, want, errOut)
	}
	if l := linkOf(t, home); l != filepath.Join(home, ".local", "share", "jhon-studio", "0.1.0", "bin", "jhon-studio") {
		t.Errorf("link -> %s", l)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "jhon-studio", "0.2.0", "bin", "jhon-studio")); err != nil {
		t.Errorf("the newer version is gone: %v", err)
	}
	// Nothing older left: the message and exit 1 of before.
	code, out, errOut = captureAll(t, []string{"update", "--rollback"})
	if code != 1 || out != "" || errOut != "jhon: no release older than jhon 0.5.0 is kept; nothing was changed\n" {
		t.Errorf("again: exit %d\nstdout %q\nstderr %q", code, out, errOut)
	}
}

// PRESERVE: with studio off, a roll-back with no older jhon kept says what
// it said and changes nothing of the tool.
func TestRollbackWithNoToolOnIsUnchanged(t *testing.T) {
	home := toolHome(t, "schema = 1\n", "0.5.0", "0.2.0", "0.1.0", "0.2.0")
	code, out, errOut := captureAll(t, []string{"update", "--rollback"})
	if code != 1 || out != "" || errOut != "jhon: no release older than jhon 0.5.0 is kept; nothing was changed\n" {
		t.Errorf("exit %d\nstdout %q\nstderr %q", code, out, errOut)
	}
	if l := linkOf(t, home); !strings.HasSuffix(l, "/0.2.0/bin/jhon-studio") {
		t.Errorf("link -> %s", l)
	}
}

// doctorTool runs doctor's cli section (remote or not) with a jhon-studio on
// PATH that fits and passes one check.
func doctorTool(t *testing.T, remote bool) string {
	t.Helper()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "jhon-studio"), []byte("#!/bin/sh\ncase \"$*\" in\n"+
		"  'integration --json') echo '{\"version\":\"0.1.0\",\"plugin_dir\":\"/p\",\"requires_jhon\":\">=0.5.0\"}' ;;\n"+
		"  'doctor --json') echo '{\"ok\":true,\"checks\":[{\"id\":\"node\",\"ok\":true,\"detail\":\"node 22.11.0\",\"fix\":\"\"}]}' ;;\n"+
		"  *) exit 2 ;;\nesac\n"), 0o755))
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	var b strings.Builder
	doctor{o: &release.Out{W: &b, E: &b}, remote: remote}.cliIntegrations()
	return b.String()
}

func TestDoctorSaysWhereAToolCameFrom(t *testing.T) {
	const fit = "  ok    jhon-studio 0.1.0 requires jhon >=0.5.0; this is jhon 0.5.0\n  ok    node: node 22.11.0\n"
	toolHome(t, studioOn, "0.5.0", "0.1.0", "0.1.0")
	want := "\njhon-studio\n" +
		"  ok    jhon-studio 0.1.0 from its signed release v0.1.0 of github.com/jachavarro2004/jhon-studio (~/.local/share/jhon-studio/0.1.0)\n" +
		"  run 'jhon doctor --remote' to look for a newer jhon-studio release that supports this jhon\n" + fit
	if got := doctorTool(t, false); got != want {
		t.Errorf("a release:\n got %q\nwant %q", got, want)
	}

	releasesServed(t, http.StatusOK, `[{"tag_name":"v0.1.0","assets":[]}]`)
	want = "\njhon-studio\n" +
		"  ok    jhon-studio 0.1.0 from its signed release v0.1.0 of github.com/jachavarro2004/jhon-studio (~/.local/share/jhon-studio/0.1.0)\n" +
		"  ok    jhon-studio 0.1.0 is the newest release of github.com/jachavarro2004/jhon-studio that supports jhon 0.5.0\n" + fit
	if got := doctorTool(t, true); got != want {
		t.Errorf("--remote, up to date:\n got %q\nwant %q", got, want)
	}
	releasesServed(t, http.StatusInternalServerError, "down")
	if got := doctorTool(t, true); !strings.Contains(got, "\n  warn  cannot look for a newer jhon-studio release: ") || strings.Contains(got, "nothing was changed") {
		t.Errorf("--remote, unreachable:\n%s", got)
	}

	home := toolHome(t, studioOn, "0.5.0", "")
	must(t, os.MkdirAll(filepath.Join(home, ".local", "share", "jhon-studio", ".git"), 0o755))
	releasesServed(t, http.StatusInternalServerError, "must not be asked")
	want = "\njhon-studio\n  jhon-studio is a development install: a git clone at ~/.local/share/jhon-studio, which jhon setup and jhon update leave alone\n" + fit
	if got := doctorTool(t, true); got != want {
		t.Errorf("a development clone:\n got %q\nwant %q", got, want)
	}
}
