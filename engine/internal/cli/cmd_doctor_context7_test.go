package cli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
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
	saved := probeContext7
	defer func() { probeContext7 = saved }()
	probeContext7 = func(url string) error {
		asked++
		if url != render.Context7URL {
			t.Errorf("asked %s; want %s", url, render.Context7URL)
		}
		return probe
	}
	var b strings.Builder
	o := &release.Out{W: &b, E: &b}
	doctor{o: o}.context7(remote)
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
	if err := probeContext7(srv.URL); err != nil {
		t.Errorf("a 200 answer: %v", err)
	}
	if got.method != "POST" || got.accept != "application/json, text/event-stream" || !strings.Contains(got.body, `"method":"initialize"`) {
		t.Errorf("the probe sent %+v", got)
	}
	status = http.StatusServiceUnavailable
	if err := probeContext7(srv.URL); err == nil || err.Error() != "HTTP 503" {
		t.Errorf("a 503 answer: %v", err)
	}
	srv.Close()
	if err := probeContext7(srv.URL); err == nil {
		t.Error("no answer is an error")
	}
}
