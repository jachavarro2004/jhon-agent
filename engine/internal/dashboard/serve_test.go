package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/telemetry"
)

// The projects directory is a fixture and the auditor is injected, so no test reads ~/.claude or runs a real audit.

const (
	idA = "aaaaaaaa-0000-4000-8000-000000000001"
	idB = "bbbbbbbb-0000-4000-8000-000000000002"
)

func projects(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	for _, d := range []string{"-home-u-Projects-acme", "-home-u-jhon"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	lines := []string{
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>ignore</local-command-caveat>"}}`,
		`{"type":"user","message":{"role":"user","content":"<command-name>/clear</command-name>"}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Fix the soft delete in   the ABC service, then run the tests"}]}}`,
	}
	write(t, filepath.Join(dir, "-home-u-Projects-acme", idA+".jsonl"), strings.Join(lines, "\n")+"\n")
	b := filepath.Join(dir, "-home-u-jhon", idB+".jsonl")
	write(t, b, "{}\n{}\n")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(b, old, old); err != nil {
		t.Fatal(err)
	}
	return dir
}

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

type served struct {
	url       string
	calls     []string
	summaries *Summaries
}

func serve(t *testing.T, dir string) *served {
	s := &served{summaries: NewSummaries(t.TempDir())}
	srv := httptest.NewServer(Server(dir, func(path string) any {
		s.calls = append(s.calls, path)
		r, _ := js.ParseJSON(`{"ok":true,"status":"clean","session":{"sessionKind":"interactive"},"findings":[]}`)
		return r
	}, assets, s.summaries))
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s
}

type response struct {
	status int
	typ    string
	body   string
}

func (s *served) get(t *testing.T, path, method string) response {
	t.Helper()
	req, _ := http.NewRequest(method, s.url+path, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("%s: headers %v", path, res.Header)
	}
	return response{res.StatusCode, res.Header.Get("Content-Type"), string(b)}
}

func apiSessions(t *testing.T, s *served) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(s.get(t, "/api/sessions", "GET").body), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestComponentAndSessions(t *testing.T) {
	s := serve(t, projects(t))
	for path, typ := range map[string]string{"/": "text/html", "/app.css": "text/css", "/app.js": "text/javascript"} {
		r := s.get(t, path, "GET")
		want, _ := os.ReadFile(filepath.Join(assets, map[string]string{"/": "index.html", "/app.css": "app.css", "/app.js": "app.js"}[path]))
		if r.status != 200 || r.typ != typ+"; charset=utf-8" || r.body != string(want) {
			t.Errorf("%s: %d %s", path, r.status, r.typ)
		}
	}
	api := apiSessions(t, s)
	if len(api) != 2 || api[0]["id"] != idA || api[1]["id"] != idB || api[0]["project"] != "-home-u-Projects-acme" {
		t.Errorf("api, newest first: %v", api)
	}
	if _, ok := api[0]["path"]; ok {
		t.Error("the path is not served")
	}
	missing := httptest.NewServer(Server(projects(t), nil, t.TempDir(), nil))
	defer missing.Close()
	res, err := http.Get(missing.URL + "/app.js")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 500 || !strings.Contains(string(b), "run `jhon sync`") {
		t.Errorf("a missing component: %d %s", res.StatusCode, b)
	}
}

func TestSessionDataCached(t *testing.T) {
	dir := projects(t)
	s := serve(t, dir)
	api := s.get(t, "/api/session/"+idA[:8], "GET")
	var got struct {
		Session map[string]any `json:"session"`
		View    map[string]any `json:"view"`
	}
	if err := json.Unmarshal([]byte(api.body), &got); err != nil || api.typ != "application/json; charset=utf-8" {
		t.Fatalf("%v %s %s", err, api.typ, api.body)
	}
	if got.Session["id"] != idA || got.Session["project"] != "acme" || got.Session["title"] != "Fix the soft delete in the ABC service, then run the tests" ||
		got.View["status"] != "clean" {
		t.Errorf("session data: %s", api.body)
	}
	// The session page shows what the session did (L138, answer 4): the
	// changes' state today is the Changes pages' only.
	var keys map[string]any
	json.Unmarshal([]byte(api.body), &keys)
	if _, ok := keys["ldd"]; ok || len(keys) != 3 || keys["usage"] == nil {
		t.Errorf("a session's data is its metadata, its view and its usage, no state today: %s", api.body)
	}
	s.get(t, "/api/session/"+idA, "GET")
	if len(s.calls) != 1 {
		t.Errorf("the second request reused the cached audit: %d calls", len(s.calls))
	}
	write(t, filepath.Join(dir, "-home-u-Projects-acme", idA+".jsonl"), "{}\n{}\n{}\n")
	s.get(t, "/api/session/"+idA, "GET")
	if len(s.calls) != 2 {
		t.Errorf("a changed transcript is audited again: %d calls", len(s.calls))
	}
	s.get(t, "/api/session/"+idA+"?fresh=1", "GET")
	if len(s.calls) != 3 {
		t.Errorf("?fresh=1 audits again: %d calls", len(s.calls))
	}
}

// The old page's address (`/session/<id>`) is gone (T17b, D8): the page
// lives at /#/s/<id>, and nothing in the tree links the old one.
func TestTheOldSessionAddressIsGone(t *testing.T) {
	s := serve(t, projects(t))
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(s.url + "/session/" + idA[:8])
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 || res.Header.Get("Location") != "" {
		t.Errorf("/session/<id>: %d %q, want 404 and no redirect", res.StatusCode, res.Header.Get("Location"))
	}
	if len(s.calls) != 0 {
		t.Errorf("no audit ran: %v", s.calls)
	}
}

func TestOnlyGetHexAndExisting(t *testing.T) {
	s := serve(t, projects(t))
	for path, want := range map[string]int{
		"/api/session/..%2F..%2Fetc%2Fpasswd":               400,
		"/api/session/cccccccc-0000-4000-8000-000000000003": 404,
		"/nowhere":                    404,
		"/api/session/aaaaaaa":        404, // a prefix shorter than 8 is no match
		"/x/../api/sessions?q=1":      200,
		"/api/session/%ff":            400,
		"/api/session/" + idA + "/..": 404,
	} {
		if r := s.get(t, path, "GET"); r.status != want {
			t.Errorf("%s: %d, want %d", path, r.status, want)
		}
	}
	if r := s.get(t, "/", "POST"); r.status != 405 || r.body != "GET only\n" {
		t.Errorf("POST: %d %q", r.status, r.body)
	}
	if len(s.calls) != 0 {
		t.Errorf("no audit ran: %v", s.calls)
	}
}

func TestSessionNames(t *testing.T) {
	s := serve(t, projects(t))
	api := apiSessions(t, s)
	if api[0]["name"] != "acme" || api[0]["title"] != "Fix the soft delete in the ABC service, then run the tests" {
		t.Errorf("first: %v", api[0])
	}
	if api[1]["name"] != "jhon" || api[1]["title"] != nil {
		t.Errorf("a session with no request has no title: %v", api[1])
	}
}

func TestProjectNameAndTitle(t *testing.T) {
	for dir, want := range map[string]string{
		"-tmp-claude-1000--home-u--agents-jhon-501636fc-007d-4af0-8ce5-55cae5ee23e2-scratchpad-guardrepo": "scratch guardrepo",
		"-tmp-claude-1000--home-u--agents-jhon-501636fc-007d-4af0-8ce5-55cae5ee23e2-scratchpad":           "scratch",
		"-home-u-Projects-work-acme-app": "acme-app",
		"-home-u-":                       "-home-u-",
	} {
		if got := ProjectName(dir); got != want {
			t.Errorf("%s: %q, want %q", dir, got, want)
		}
	}
	f := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, f, `{"type":"user","message":{"role":"user","content":"<pasted_content id=\"7e25\">\n├──┤ Ctrl + Shift\n</pasted_content> fix my terminal keys"}}`+"\n")
	if got := SessionTitle(f); got == nil || *got != "Ctrl + Shift fix my terminal keys" {
		t.Errorf("title: %v", got)
	}
	write(t, f, `{"type":"user","message":{"content":"`+strings.Repeat("é", 130)+`"}}`)
	if got := SessionTitle(f); got == nil || *got != strings.Repeat("é", 119)+"…" {
		t.Errorf("a long title is cut at 120: %v", got)
	}
}

func TestSummariesListAndPersist(t *testing.T) {
	dir := projects(t)
	s := serve(t, dir)
	if api := apiSessions(t, s); api[0]["summary"] != nil {
		t.Errorf("no summary before an audit: %v", api[0])
	}
	s.get(t, "/api/session/"+idA, "GET")
	api := apiSessions(t, s)
	sum, _ := api[0]["summary"].(map[string]any)
	if sum == nil || sum["status"] != "clean" || sum["violation"] != 0.0 || api[1]["summary"] != nil {
		t.Errorf("the audited session carries its summary: %v", api)
	}
	again := NewSummaries(s.summaries.dir)
	all := Sessions(dir, 10, false)
	if _, ok := again.Get(all[0]); !ok {
		t.Error("summaries persist on disk")
	}
	write(t, filepath.Join(dir, "-home-u-Projects-acme", idA+".jsonl"), "{}\n")
	if _, ok := again.Get(Sessions(dir, 10, false)[0]); ok {
		t.Error("a changed transcript's summary no longer matches")
	}
}

func TestSummariesBackground(t *testing.T) {
	dir := projects(t)
	c := NewSummaries(t.TempDir())
	var audited []string
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		c.Background(dir, func(path string) any {
			audited = append(audited, filepath.Base(path))
			if len(audited) == 2 {
				close(stop)
			}
			r, _ := js.ParseJSON(`{"ok":false,"status":"findings","findings":[{"check":"delivered-untested","severity":"violation","transcript":"a.jsonl","line":3}]}`)
			return r
		}, 10, time.Hour, stop)
		close(done)
	}()
	<-done
	if len(audited) != 2 {
		t.Fatalf("each session without a summary is audited once: %v", audited)
	}
	raw, ok := c.Get(Sessions(dir, 10, false)[0])
	if !ok || !strings.Contains(string(raw), `"violation":1`) || !strings.Contains(string(raw), `"check":"delivered-untested"`) {
		t.Errorf("summary: %s", raw)
	}
}

func TestPiSessionsAreListedBesideClaudeCodes(t *testing.T) {
	dir := projects(t)
	pi := t.TempDir()
	t.Cleanup(func() { PiSessions = "" })
	PiSessions = pi
	file := filepath.Join(pi, "--home-u-Projects-work-acme_app--", "2026-10-29T10-01-15-091Z_01a1871b-4a53-7504-abd1-df609beb96cf.jsonl")
	os.MkdirAll(filepath.Dir(file), 0o755)
	os.MkdirAll(filepath.Join(pi, "--home-u--"), 0o755)
	write(t, file, `{"type":"session","version":3,"id":"01a1871b-4a53-7504-abd1-df609beb96cf","cwd":"/home/u/Projects/work/acme_app"}`+"\n"+
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"Revisa el plan"}]}}`+"\n")
	write(t, filepath.Join(pi, "--home-u--", "notes.jsonl"), "{}\n")
	var found *Session
	for _, s := range Sessions(dir, 100, true) {
		if s.Runtime == "pi" {
			found = &s
		}
	}
	if found == nil || found.ID != "01a1871b-4a53-7504-abd1-df609beb96cf" || found.Name != "acme-app" || found.Title == nil || *found.Title != "Revisa el plan" {
		t.Fatalf("the pi session, named as Claude Code names its project, titled by its first request: %+v", found)
	}
	for _, s := range Sessions(dir, 100, false) {
		if s.Runtime == "" || (s.Runtime == "pi" && s.ID == "notes") {
			t.Errorf("every session says its runtime, and a file not named like a pi session is none: %+v", s)
		}
	}
}

func TestTelemetryServesTheSummaryOfItsDays(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("JHON_TELEMETRY", "on")
	telemetry.Record(telemetry.Event{Name: "jhon.hook", Attributes: map[string]any{"jhon.hook.name": "safety-guard", "jhon.decision": "deny"}})
	s := serve(t, projects(t))
	for _, path := range []string{"/api/telemetry", "/api/telemetry?days=1", "/api/telemetry?days=x"} {
		r := s.get(t, path, "GET")
		var out struct {
			Days, Events int
			Hooks        []map[string]any
		}
		if r.status != 200 || !strings.HasPrefix(r.typ, "application/json") || json.Unmarshal([]byte(r.body), &out) != nil {
			t.Fatalf("%s: %d %s %s", path, r.status, r.typ, r.body)
		}
		if out.Events != 1 || len(out.Hooks) != 1 || out.Hooks[0]["deny"] != 1.0 {
			t.Errorf("%s: %s", path, r.body)
		}
		if want := map[string]int{"/api/telemetry": 7, "/api/telemetry?days=1": 1, "/api/telemetry?days=x": 7}[path]; out.Days != want {
			t.Errorf("%s: %d days, want %d", path, out.Days, want)
		}
	}
}

func TestSessionEntrypointIsTheHeadsFirst(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"cli.jsonl":  "{\"type\":\"mode\"}\n{\"type\":\"user\",\"entrypoint\":\"cli\"}\n{\"type\":\"user\",\"entrypoint\":\"sdk-ts\"}\n",
		"sdk.jsonl":  "{\"type\":\"user\",\"entrypoint\":\"sdk-ts\"}\n",
		"none.jsonl": "{\"type\":\"user\"}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{"cli.jsonl": "cli", "sdk.jsonl": "sdk-ts", "none.jsonl": "", "missing.jsonl": ""} {
		if got := SessionEntrypoint(filepath.Join(dir, name)); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}
