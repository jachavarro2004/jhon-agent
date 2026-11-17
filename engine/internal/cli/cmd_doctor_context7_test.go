package cli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
)

// T133e (L1016, G6): doctor says how context7 is mounted and asks its
// endpoint only with --remote; nothing of it is a failure.

// context7Rows is doctor's context7 rows under the settings text ("" for no
// file), with the endpoint answering as probe says; asked is how many times
// the endpoint was asked.
func context7Rows(t *testing.T, settings string, remote bool, probe error) (rows string, asked int, failed int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.toml")
	if settings != "" {
		must(t, os.WriteFile(path, []byte(settings), 0o600))
	}
	t.Setenv("JHON_SETTINGS", path)
	saved := probeRemote
	defer func() { probeRemote = saved }()
	probeRemote = func(url string) error {
		asked++
		if url != "https://mcp.context7.com/mcp" {
			t.Errorf("asked %s; want context7's", url)
		}
		return probe
	}
	var b strings.Builder
	o := &release.Out{W: &b, E: &b}
	doctor{o: o, src: repoRoot(t)}.remoteProbed(remote)
	return b.String(), asked, o.Failed
}

func TestDoctorContext7MakesNoNetworkCallWithoutRemote(t *testing.T) {
	rows, asked, failed := context7Rows(t, "", false, nil)
	want := "  context7 is the hosted https://mcp.context7.com/mcp, for explore and worker: nothing installed or pinned; run 'jhon doctor --remote' to ask whether it answers\n"
	if rows != want || asked != 0 || failed != 0 {
		t.Errorf("doctor without --remote: asked %d time(s), %d failed\n got %q\nwant %q", asked, failed, rows, want)
	}
}

func TestDoctorContext7RemoteSaysWhetherItAnswers(t *testing.T) {
	rows, asked, failed := context7Rows(t, "", true, nil)
	if want := "  ok    context7 answers at https://mcp.context7.com/mcp\n"; rows != want || asked != 1 || failed != 0 {
		t.Errorf("--remote, answering: asked %d, failed %d\n got %q\nwant %q", asked, failed, rows, want)
	}
	rows, asked, failed = context7Rows(t, "", true, errors.New("HTTP 503"))
	want := "  warn  context7 unreachable (https://mcp.context7.com/mcp: HTTP 503) — offline, or the service is down; its tools answer with an error until it is back\n"
	if rows != want || asked != 1 || failed != 0 {
		t.Errorf("--remote, not answering: a warning, never a failure: asked %d, failed %d\n got %q\nwant %q", asked, failed, rows, want)
	}
}

func TestDoctorContext7OffIsSaidAndNeverAsked(t *testing.T) {
	for _, remote := range []bool{false, true} {
		rows, asked, failed := context7Rows(t, "schema = 1\n\n[integrations]\ncontext7 = false\n", remote, nil)
		if want := "  context7 is off (integrations.context7 = false)\n"; rows != want || asked != 0 || failed != 0 {
			t.Errorf("off (remote %v): asked %d, failed %d\n got %q\nwant %q", remote, asked, failed, rows, want)
		}
	}
}

// The probe is one MCP initialize, answered 200 by a live endpoint; any
// other answer, or none, is an error.
func TestProbeContext7(t *testing.T) {
	var got struct{ method, accept, body string }
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 512)
		n, _ := r.Body.Read(b)
		got.method, got.accept, got.body = r.Method, r.Header.Get("Accept"), string(b[:n])
		if r.Header.Get("Authorization") != "" {
			t.Error("the probe sent a credential")
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	if err := probeRemote(srv.URL); err != nil {
		t.Errorf("a 200 answer: %v", err)
	}
	if got.method != "POST" || got.accept != "application/json, text/event-stream" || !strings.Contains(got.body, `"method":"initialize"`) {
		t.Errorf("the probe sent %+v", got)
	}
	status = http.StatusServiceUnavailable
	if err := probeRemote(srv.URL); err == nil || err.Error() != "HTTP 503" {
		t.Errorf("a 503 answer: %v", err)
	}
	srv.Close()
	if err := probeRemote(srv.URL); err == nil {
		t.Error("no answer is an error")
	}
}

// T58b O1 (L1167): doctor's remote row is every optional integration whose
// manifest probes it remotely, by its own setting and url: a fixture set's
// extra one is said off, and asked only with --remote, as context7 is.
func TestDoctorRemoteRowsFollowTheManifests(t *testing.T) {
	extra := integrations.Manifest{Name: "docsfixture", Kind: "mcp-http", Order: 9, Setting: "integrations.docsfixture", Default: true,
		WhenOff: "render", Capabilities: []string{"docsfixture"}, Doctor: integrations.Doctor{Probe: "remote", URL: "https://docs.example.invalid/mcp"}}
	t.Cleanup(integrations.Use(append(integrations.All(), extra)))
	path := filepath.Join(t.TempDir(), "settings.toml")
	must(t, os.WriteFile(path, []byte("schema = 1\n\n[integrations]\ncontext7 = false\ndocsfixture = false\n"), 0o600))
	t.Setenv("JHON_SETTINGS", path)
	saved := probeRemote
	defer func() { probeRemote = saved }()
	asked := 0
	probeRemote = func(string) error { asked++; return nil }
	var b strings.Builder
	o := &release.Out{W: &b, E: &b}
	doctor{o: o, src: repoRoot(t)}.remoteProbed(true)
	want := "  context7 is off (integrations.context7 = false)\n  docsfixture is off (integrations.docsfixture = false)\n"
	if b.String() != want || asked != 0 {
		t.Errorf("asked %d\n got %q\nwant %q", asked, b.String(), want)
	}
	must(t, os.WriteFile(path, []byte("schema = 1\n\n[integrations]\ncontext7 = false\n"), 0o600))
	b.Reset()
	doctor{o: o, src: repoRoot(t)}.remoteProbed(true)
	if want := "  context7 is off (integrations.context7 = false)\n  ok    docsfixture answers at https://docs.example.invalid/mcp\n"; b.String() != want || asked != 1 {
		t.Errorf("asked %d\n got %q\nwant %q", asked, b.String(), want)
	}
}

// T58c J2 (L1193): the plain row names the agents holding context7's
// capability, exactly as before T58b, whatever tree doctor judges: a
// checkout, an installed release (its archived tree), none (an empty src),
// or one whose prose/agents.yaml cannot be read.
func TestDoctorContext7NamesItsAgentsWhateverTheTree(t *testing.T) {
	installed := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(installed, "prose"), 0o755))
	agents, err := os.ReadFile(filepath.Join(repoRoot(t), "prose", "agents.yaml"))
	must(t, err)
	must(t, os.WriteFile(filepath.Join(installed, "prose", "agents.yaml"), agents, 0o644))
	path := filepath.Join(t.TempDir(), "settings.toml")
	t.Setenv("JHON_SETTINGS", path)
	want := "  context7 is the hosted https://mcp.context7.com/mcp, for explore and worker: nothing installed or pinned; run 'jhon doctor --remote' to ask whether it answers\n"
	for name, src := range map[string]string{"a checkout": repoRoot(t), "an installed release": installed, "no source": "", "unreadable agents": t.TempDir()} {
		var b strings.Builder
		o := &release.Out{W: &b, E: &b}
		doctor{o: o, src: src}.remoteProbed(false)
		if b.String() != want || o.Failed != 0 {
			t.Errorf("%s: %d failed\n got %q\nwant %q", name, o.Failed, b.String(), want)
		}
	}
}

// T58c J2 (L1193): the shipped holders doctor falls back to are the
// checkout's own, capability by capability.
func TestDoctorShippedHoldersAreTheCheckouts(t *testing.T) {
	for c, want := range shippedHolders {
		got, ok := holders(repoRoot(t), []string{c})
		if !ok || !slices.Equal(got, want) {
			t.Errorf("%s: the checkout's agents.yaml has %v (read %v); shippedHolders has %v", c, got, ok, want)
		}
	}
}
