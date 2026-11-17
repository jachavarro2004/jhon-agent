package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The Memory page's routes: the file memory under the projects directory
// (a fixture) and Engram through a fake server; no test reaches the
// owner's Engram.

func memoryProjects(t *testing.T) string {
	dir := projects(t)
	mem := filepath.Join(dir, "-home-u-jhon", "memory")
	os.MkdirAll(mem, 0o755)
	write(t, filepath.Join(mem, "MEMORY.md"), "# Memory\n\n- [Owner's plan](plan.md) — what ships in v0.1\n* [Lesson](lesson.md)\nnot an entry\n")
	write(t, filepath.Join(mem, "plan.md"), "---\nname: plan\n---\nShip it.\n")
	write(t, filepath.Join(dir, "secret.md"), "outside the memory\n")
	os.Symlink(filepath.Join(dir, "secret.md"), filepath.Join(mem, "link.md"))
	return dir
}

func TestMemoryFilesListTheIndexAndReadOnlyABareName(t *testing.T) {
	s := serve(t, memoryProjects(t))
	r := s.get(t, "/api/memory/files?project=-home-u-jhon", "GET")
	var files struct {
		Project string `json:"project"`
		Dir     string `json:"dir"`
		Index   bool   `json:"index"`
		Files   []struct{ Title, File, Hook string }
	}
	if err := json.Unmarshal([]byte(r.body), &files); err != nil || r.status != 200 || !files.Index || !strings.HasSuffix(files.Dir, "/-home-u-jhon/memory") ||
		len(files.Files) != 2 || files.Files[0].Title != "Owner's plan" || files.Files[0].File != "plan.md" || files.Files[0].Hook != "what ships in v0.1" || files.Files[1].Hook != "" {
		t.Errorf("the index: %d %s", r.status, r.body)
	}
	if r := s.get(t, "/api/memory/files?project=-home-u-Projects-acme", "GET"); r.status != 200 || !strings.Contains(r.body, `"index":false,"files":[]`) {
		t.Errorf("no file memory: %d %s", r.status, r.body)
	}
	if r := s.get(t, "/api/memory/file?project=-home-u-jhon&name=plan.md", "GET"); r.status != 200 || r.body != `{"name":"plan.md","text":"---\nname: plan\n---\nShip it.\n"}` {
		t.Errorf("a file: %d %s", r.status, r.body)
	}
	for _, name := range []string{"../secret.md", "..%2Fsecret.md", "%2Fetc%2Fpasswd", "plan.txt", ".hidden.md", "a%5Cb.md", "", "..", "plan.md%00"} {
		if r := s.get(t, "/api/memory/file?project=-home-u-jhon&name="+name, "GET"); r.status != 400 || r.body != "a memory file is a bare *.md name of the project's memory\n" {
			t.Errorf("%q: %d %s", name, r.status, r.body)
		}
	}
	for _, name := range []string{"link.md", "lesson.md"} {
		if r := s.get(t, "/api/memory/file?project=-home-u-jhon&name="+name, "GET"); r.status != 404 || r.body != "no such memory file\n" {
			t.Errorf("%s (a link out, a missing file): %d %s", name, r.status, r.body)
		}
	}
	for path, want := range map[string]string{
		"/api/memory/files?project=..":                         "400 a project is a directory the sessions name\n",
		"/api/memory/files?project=-home-u-jhon%2F..":          "400 a project is a directory the sessions name\n",
		"/api/memory/files":                                    "400 a project is a directory the sessions name\n",
		"/api/memory/files?project=-home-u-other":              "404 no session names this project\n",
		"/api/memory/file?project=..%2F..&name=secret.md":      "400 a project is a directory the sessions name\n",
		"/api/memory/engram?project=-home-u-other":             "404 no session names this project\n",
		"/api/memory/engram?project=-home-u-jhon%2F..%2F..%2F": "400 a project is a directory the sessions name\n",
	} {
		if r := s.get(t, path, "GET"); itoa(r.status)+" "+r.body != want {
			t.Errorf("%s: %d %s", path, r.status, r.body)
		}
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

type fakeEngram struct {
	mu    sync.Mutex
	paths []string
}

func engram(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *fakeEngram {
	f := &fakeEngram{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.Method+" "+r.URL.RequestURI())
		f.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ENGRAM_URL", srv.URL)
	return f
}

func TestMemoryEngramIsReadThroughItsLocalAPIOnly(t *testing.T) {
	f := engram(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/observations/recent":
			w.Write([]byte(`[{"id":7,"type":"decision","title":"Use rose ink","content":"long text","created_at":"2026-11-01 10:00:00","project":"jhon"}]`))
		case "/stats":
			w.Write([]byte(`{"total_observations":12}`))
		case "/search":
			w.Write([]byte(`[{"id":8,"type":"bugfix","title":"Fix <it>","content":"c","created_at":"2026-11-02 09:00:00"}]`))
		case "/observations/7":
			w.Write([]byte(`{"id":7,"type":"decision","title":"Use rose ink","content":"long text","created_at":"2026-11-01 10:00:00","topic_key":"palette"}`))
		default:
			http.NotFound(w, r)
		}
	})
	s := serve(t, memoryProjects(t))
	if r := s.get(t, "/api/memory/engram?project=-home-u-jhon", "GET"); r.status != 200 ||
		r.body != `{"project":"jhon","total":12,"query":null,"observations":[{"id":7,"type":"decision","title":"Use rose ink","created_at":"2026-11-01 10:00:00"}]}` {
		t.Errorf("recent: %d %s", r.status, r.body)
	}
	if r := s.get(t, "/api/memory/engram?project=-home-u-jhon&q=fix+it", "GET"); r.status != 200 ||
		r.body != `{"project":"jhon","total":12,"query":"fix it","observations":[{"id":8,"type":"bugfix","title":"Fix <it>","created_at":"2026-11-02 09:00:00"}]}` {
		t.Errorf("search: %d %s", r.status, r.body)
	}
	if r := s.get(t, "/api/memory/engram/7", "GET"); r.status != 200 || !strings.Contains(r.body, `"content":"long text"`) || !strings.Contains(r.body, `"topic_key":"palette"`) {
		t.Errorf("one observation: %d %s", r.status, r.body)
	}
	for _, id := range []string{"x", "7%2F..%2F..%2Fadmin", "-1", "12345678901234567890"} {
		if r := s.get(t, "/api/memory/engram/"+id, "GET"); r.status != 400 || r.body != "an observation id is digits\n" {
			t.Errorf("%s: %d %s", id, r.status, r.body)
		}
	}
	want := []string{"GET /observations/recent?project=jhon&limit=40", "GET /stats?project=jhon", "GET /search?q=fix+it&project=jhon&limit=30", "GET /stats?project=jhon", "GET /observations/7"}
	f.mu.Lock()
	defer f.mu.Unlock()
	eqJSON(t, f.paths, want, "only Engram's read endpoints, GET")
}

func TestMemoryEngramThatDoesNotAnswerIsSaidSo(t *testing.T) {
	engram(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", 500) })
	s := serve(t, memoryProjects(t))
	r := s.get(t, "/api/memory/engram?project=-home-u-jhon", "GET")
	if r.status != 502 || !strings.HasPrefix(r.body, "Engram does not answer: ") || !strings.Contains(r.body, "/observations/recent answered 500") {
		t.Errorf("an Engram error: %d %s", r.status, r.body)
	}
	old := engramTimeout
	engramTimeout = 50 * time.Millisecond
	t.Cleanup(func() { engramTimeout = old })
	engram(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	})
	start := time.Now()
	r = s.get(t, "/api/memory/engram/7", "GET")
	if r.status != 502 || !strings.HasPrefix(r.body, "Engram does not answer: ") || time.Since(start) > 900*time.Millisecond {
		t.Errorf("a slow Engram is cut at its timeout: %d %s after %s", r.status, r.body, time.Since(start))
	}
}

func TestEngramProjectIsTheOriginsNameElseTheFolders(t *testing.T) {
	dir := t.TempDir()
	if got := engramProjectOf(filepath.Join(dir, "gone", "my-app")); got != "my-app" {
		t.Errorf("a folder that is gone: %q", got)
	}
	if got := engramProjectOf(""); got != "" {
		t.Errorf("no working directory: %q", got)
	}
	if got := originName("git@github-personal:jachavarro2004/jhon-agent.git\n"); got != "jhon-agent" {
		t.Errorf("origin: %q", got)
	}
	repo := filepath.Join(dir, "checkout")
	os.MkdirAll(filepath.Join(repo, "src"), 0o755)
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://example.com/team/acme-app.git"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	if got := engramProjectOf(filepath.Join(repo, "src")); got != "acme-app" {
		t.Errorf("a checkout files under its origin's name: %q", got)
	}
}
