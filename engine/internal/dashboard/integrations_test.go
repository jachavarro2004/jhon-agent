package dashboard

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
)

// T58b O3 (L1167): /api/integrations is one row per manifest, by order, and
// degrades: an integration that is off, or on but unreachable, is a row
// saying so with its reason and no numbers, never an error answer.

// integrationsAt serves /api/integrations under the settings text, with
// the binaries found as onPath says and the remote probe answering probe;
// asked counts the probes.
func integrationsAt(t *testing.T, settingsText string, onPath map[string]bool, probe error, route string) (response, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(path, []byte(settingsText), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JHON_SETTINGS", path)
	savedLook, savedProbe := lookPath, probeRemote
	t.Cleanup(func() { lookPath, probeRemote = savedLook, savedProbe })
	lookPath = func(bin string) (string, error) {
		if onPath[bin] {
			return "/bin/" + bin, nil
		}
		return "", errors.New("not found")
	}
	asked := 0
	probeRemote = func(string) error { asked++; return probe }
	s := serve(t, memoryProjects(t))
	r := s.get(t, route, "GET")
	return r, asked
}

func engramStats(t *testing.T, status int) {
	engram(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stats" || status != 200 {
			http.Error(w, "boom", status)
			return
		}
		w.Write([]byte(`{"total_observations":12}`))
	})
}

func TestIntegrationsOffShowsItsReasonAndNoNumbers(t *testing.T) {
	engramStats(t, 200)
	r, asked := integrationsAt(t, "schema = 1\n\n[integrations]\ncontext7 = false\n", map[string]bool{"engram": true}, nil, "/api/integrations?probe=1")
	want := `[{"name":"engram","display":"Engram","required":true,"state":"on","reason":"","numbers":{"observations":12}},` +
		`{"name":"codegraph","display":"CodeGraph","required":true,"state":"unavailable","reason":"codegraph is not on PATH","numbers":null},` +
		`{"name":"context7","display":"Context7","required":false,"state":"off","reason":"integrations.context7 = false","numbers":null},` +
		`{"name":"m365","display":"Microsoft 365","required":false,"state":"off","reason":"claude_code.connectors = false","numbers":null}]`
	if r.status != 200 || r.typ != "application/json; charset=utf-8" || r.body != want || asked != 0 {
		t.Errorf("asked %d, %d %s\n got %s\nwant %s", asked, r.status, r.typ, r.body, want)
	}
}

func TestIntegrationsUnreachableShowsItsReasonAndNoNumbers(t *testing.T) {
	engramStats(t, 500)
	on := "schema = 1\n\n[claude_code]\nconnectors = true\n"
	paths := map[string]bool{"engram": true, "codegraph": true}
	r, asked := integrationsAt(t, on, paths, errors.New("HTTP 503"), "/api/integrations?probe=1")
	want := `[{"name":"engram","display":"Engram","required":true,"state":"unavailable","reason":"Engram does not answer: /stats answered 500","numbers":null},` +
		`{"name":"codegraph","display":"CodeGraph","required":true,"state":"on","reason":"","numbers":null},` +
		`{"name":"context7","display":"Context7","required":false,"state":"unavailable","reason":"https://mcp.context7.com/mcp: HTTP 503","numbers":null},` +
		`{"name":"m365","display":"Microsoft 365","required":false,"state":"on","reason":"","numbers":null}]`
	if r.status != 200 || r.body != want || asked != 1 {
		t.Errorf("asked %d, %d\n got %s\nwant %s", asked, r.status, r.body, want)
	}
	// Without ?probe=1 the hosted endpoint is never asked (doctor's rule).
	r, asked = integrationsAt(t, on, paths, errors.New("HTTP 503"), "/api/integrations")
	if want := `{"name":"context7","display":"Context7","required":false,"state":"on","reason":"not asked: ?probe=1 asks https://mcp.context7.com/mcp","numbers":null}`; r.status != 200 || asked != 0 || !contains(r.body, want) {
		t.Errorf("unprobed: asked %d, %d %s", asked, r.status, r.body)
	}
}

// A fixture set's extra optional integration is a row like the shipped ones.
func TestIntegrationsRowsFollowTheManifests(t *testing.T) {
	engramStats(t, 200)
	extra := integrations.Manifest{Name: "docsfixture", Display: "Docs Fixture", Kind: "mcp-http", Order: 9, Setting: "integrations.docsfixture",
		Default: true, WhenOff: "render", Capabilities: []string{"docsfixture"}}
	t.Cleanup(integrations.Use(append(integrations.All(), extra)))
	r, _ := integrationsAt(t, "schema = 1\n\n[integrations]\ndocsfixture = false\n", map[string]bool{"engram": true, "codegraph": true}, nil, "/api/integrations")
	if want := `{"name":"docsfixture","display":"Docs Fixture","required":false,"state":"off","reason":"integrations.docsfixture = false","numbers":null}]`; r.status != 200 || !contains(r.body, want) {
		t.Errorf("%d %s", r.status, r.body)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
