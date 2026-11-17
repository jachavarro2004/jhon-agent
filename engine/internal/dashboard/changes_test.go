package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// view is a session's view as the summaries read it.
func viewOf(t *testing.T, s string) *js.Object {
	t.Helper()
	v, err := js.ParseJSON(s)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*js.Object)
}

// A summary names every change of its session, the most worked first as `change`.
func TestSummaryNamesEveryChange(t *testing.T) {
	sum := js.StringifyValueCompact(SummaryOf(viewOf(t, `{"changes":[{"name":"a","processed":1},{"name":"b","processed":5}]}`)))
	if !strings.Contains(sum, `"change":"b"`) || !strings.Contains(sum, `"changes":["a","b"]`) {
		t.Errorf("summary: %s", sum)
	}
}

// /api/changes is every change the listed sessions name, by project, each
// with its sessions newest first and its LDD state now when the project
// holds it; newest change first.
func TestChangesAcrossTheSessions(t *testing.T) {
	if os.Getenv("JHON_ROOT") == "" {
		repo, _ := filepath.Abs("../../..")
		t.Setenv("JHON_ROOT", repo)
	}
	dir := projects(t)
	root := t.TempDir()
	change := filepath.Join(root, "openspec", "changes", "c1")
	if err := os.MkdirAll(change, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(change, "tasks.md"), "## Slice 1: one\n\n- [x] 1.1 a\n\n## Slice 2: two\n\n- [ ] 2.1 b\n")
	a := filepath.Join(dir, "-home-u-Projects-acme", idA+".jsonl")
	write(t, a, `{"type":"user","cwd":"`+root+`","message":{"role":"user","content":"Fix the soft delete"}}`+"\n")
	b := filepath.Join(dir, "-home-u-jhon", idB+".jsonl")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(b, old, old)
	s := serve(t, dir)
	if r := s.get(t, "/api/changes", "GET"); r.status != 200 || r.body != "[]" {
		t.Errorf("no summary, no change: %d %s", r.status, r.body)
	}
	all := Sessions(dir, 10, false)
	s.summaries.Put(all[0], viewOf(t, `{"changes":[{"name":"c1","processed":9},{"name":"c2","processed":1}]}`))
	s.summaries.Put(all[1], viewOf(t, `{"changes":[{"name":"c2","processed":3}]}`))
	r := s.get(t, "/api/changes", "GET")
	if r.status != 200 || !strings.HasPrefix(r.typ, "application/json") {
		t.Fatalf("changes: %d %s %s", r.status, r.typ, r.body)
	}
	var got []struct {
		Name     string `json:"name"`
		Project  string `json:"project"`
		Sessions []struct {
			ID       string  `json:"id"`
			Title    *string `json:"title"`
			Modified string  `json:"modified"`
			Runtime  string  `json:"runtime"`
		} `json:"sessions"`
		Ldd map[string]any `json:"ldd"`
	}
	if err := json.Unmarshal([]byte(r.body), &got); err != nil {
		t.Fatal(err, r.body)
	}
	if len(got) != 3 {
		t.Fatalf("one row per project and change: %s", r.body)
	}
	if got[0].Name != "c1" || got[0].Project != "acme" || len(got[0].Sessions) != 1 || got[0].Sessions[0].ID != idA || got[0].Sessions[0].Title == nil || *got[0].Sessions[0].Title != "Fix the soft delete" {
		t.Errorf("c1, its project, and its session: %s", r.body)
	}
	if got[0].Ldd == nil || got[0].Ldd["slice"] != "Slice 2: two" || len(got[0].Ldd["slices"].([]any)) != 2 {
		t.Errorf("c1's LDD state now: %v", got[0].Ldd)
	}
	if _, ok := got[0].Ldd["route"]; !ok {
		t.Errorf("the LDD state carries the route (null without git): %v", got[0].Ldd)
	}
	if got[1].Name != "c2" || got[1].Project != "acme" || got[1].Ldd != nil {
		t.Errorf("c2 in acme, a change its project does not hold has no LDD state: %s", r.body)
	}
	if got[2].Name != "c2" || got[2].Project != "jhon" || got[2].Sessions[0].ID != idB {
		t.Errorf("c2 in jhon, apart from acme's: %s", r.body)
	}
	if strings.Contains(r.body, `"path"`) {
		t.Error("the path is not served")
	}
}

// A summary written before it named every change still names its main one.
func TestChangesReadAnOlderSummarysChange(t *testing.T) {
	rows := changesOf([]Session{{ID: idA, Name: "acme", Summary: json.RawMessage(`{"change":"c9"}`)}}, func(Session, string) any { return nil })
	out := js.StringifyValueCompact(rows)
	if !strings.Contains(out, `"name":"c9"`) {
		t.Errorf("rows: %s", out)
	}
}
