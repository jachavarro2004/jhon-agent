package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T17d: the verify's fixes (L142): a linked memory directory, the Host and
// Origin a request comes with, a non-JSON Engram, and what "All" covers.

func TestMemoryRefusesASymlinkedMemoryDir(t *testing.T) {
	dir := memoryProjects(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "MEMORY.md"), "- [Stolen](stolen.md) — from outside\n")
	write(t, filepath.Join(outside, "stolen.md"), "outside the projects\n")
	os.MkdirAll(filepath.Join(dir, "-home-u-jhon2"), 0o755)
	write(t, filepath.Join(dir, "-home-u-jhon2", "cccccccc-0000-4000-8000-000000000003.jsonl"), "{}\n")
	if err := os.Symlink(outside, filepath.Join(dir, "-home-u-jhon2", "memory")); err != nil {
		t.Fatal(err)
	}
	s := serve(t, dir)
	if r := s.get(t, "/api/memory/file?project=-home-u-jhon2&name=stolen.md", "GET"); r.status != 404 || r.body != "no such memory file\n" {
		t.Errorf("a file through a linked memory dir: %d %s", r.status, r.body)
	}
	if r := s.get(t, "/api/memory/files?project=-home-u-jhon2", "GET"); r.status != 200 || strings.Contains(r.body, "Stolen") || !strings.Contains(r.body, `"index":false,"files":[]`) {
		t.Errorf("the index through a linked memory dir: %d %s", r.status, r.body)
	}
	if r := s.get(t, "/api/memory/file?project=-home-u-jhon&name=plan.md", "GET"); r.status != 200 {
		t.Errorf("a real memory dir still reads: %d %s", r.status, r.body)
	}
}

func (s *served) with(t *testing.T, path, host, origin string) response {
	t.Helper()
	req, _ := http.NewRequest("GET", s.url+path, nil)
	if host != "" {
		req.Host = host
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return response{res.StatusCode, res.Header.Get("Content-Type"), string(b)}
}

func TestTheAPIAnswersOnlyItsOwnHostAndOrigin(t *testing.T) {
	s := serve(t, projects(t))
	port := s.url[strings.LastIndex(s.url, ":")+1:]
	for _, host := range []string{"127.0.0.1:" + port, "localhost:" + port, "[::1]:" + port} {
		if r := s.with(t, "/api/sessions", host, "http://"+host); r.status != 200 {
			t.Errorf("%s: %d %s", host, r.status, r.body)
		}
	}
	if r := s.with(t, "/api/sessions", "", ""); r.status != 200 {
		t.Errorf("no Origin: %d", r.status)
	}
	for _, host := range []string{"evil.example:" + port, "127.0.0.1:1", "localhost", "127.0.0.1.evil.example:" + port} {
		if r := s.with(t, "/api/memory/files?project=-home-u-jhon", host, ""); r.status != 403 || r.body != "the dashboard answers only at 127.0.0.1, localhost or [::1] on its own port\n" {
			t.Errorf("host %s: %d %s", host, r.status, r.body)
		}
	}
	for _, origin := range []string{"http://evil.example", "http://localhost:" + port, "null", "https://127.0.0.1:" + port} {
		if r := s.with(t, "/api/sessions", "127.0.0.1:"+port, origin); r.status != 403 || r.body != "a request from another origin is refused\n" {
			t.Errorf("origin %s: %d %s", origin, r.status, r.body)
		}
	}
	if r := s.with(t, "/", "evil.example:"+port, ""); r.status != 200 {
		t.Errorf("the page itself still loads: %d", r.status)
	}
	if r := s.with(t, "/", "", ""); !strings.Contains(r.typ, "text/html") {
		t.Errorf("page: %s", r.typ)
	}
}

func TestEveryAnswerSaysItIsAJhonDashboard(t *testing.T) {
	s := serve(t, projects(t))
	res, err := http.Get(s.url + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.Header.Get("X-Jhon-Dashboard") != "1" {
		t.Errorf("headers: %v", res.Header)
	}
}

func TestEngramAnswersThatAreNotJSONAre502(t *testing.T) {
	engram(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><body>Welcome to nginx</body></html>"))
	})
	s := serve(t, memoryProjects(t))
	for _, path := range []string{"/api/memory/engram?project=-home-u-jhon", "/api/memory/engram/7"} {
		if r := s.get(t, path, "GET"); r.status != 502 || r.body != "Engram answered something that is not JSON\n" {
			t.Errorf("%s: %d %s", path, r.status, r.body)
		}
	}
}

func TestUsageSaysWhetherTheListWasCut(t *testing.T) {
	dir := projects(t)
	s := serve(t, dir)
	var u struct {
		Listed int  `json:"listed"`
		Total  int  `json:"total"`
		Capped bool `json:"capped"`
	}
	json.Unmarshal([]byte(s.get(t, "/api/usage", "GET").body), &u)
	if u.Listed != 2 || u.Total != 2 || u.Capped {
		t.Errorf("two sessions, nothing cut: %+v", u)
	}
	json.Unmarshal([]byte(s.get(t, "/api/coverage", "GET").body), &u)
	if u.Listed != 2 || u.Total != 2 || u.Capped {
		t.Errorf("coverage: %+v", u)
	}
	for i := 0; i < limit; i++ {
		write(t, filepath.Join(dir, "-home-u-jhon", strings.Replace("dddddddd-0000-4000-8000-0000000000xx", "xx", string(rune('a'+i/26))+string(rune('a'+i%26)), 1)+".jsonl"), "{}\n")
	}
	json.Unmarshal([]byte(s.get(t, "/api/usage", "GET").body), &u)
	if u.Listed != limit || u.Total != limit+2 || !u.Capped {
		t.Errorf("82 sessions, 80 listed: %+v", u)
	}
}
