package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

func TestLddOf(t *testing.T) {
	if os.Getenv("JHON_ROOT") == "" {
		repo, _ := filepath.Abs("../../..")
		t.Setenv("JHON_ROOT", repo)
	}
	root := t.TempDir()
	change := filepath.Join(root, "openspec", "changes", "add-x")
	if err := os.MkdirAll(change, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(change, "tasks.md"), "## Slice 1: one\n\n- [x] 1.1 a\n- [x] 1.2 b\n\n## Slice 2: two\n\n- [ ] 2.1 c\n")
	transcript := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, transcript, `{"type":"summary"}`+"\n"+`{"type":"user","cwd":"`+root+`"}`+"\n")
	if got := sessionCwd(transcript); got != root {
		t.Fatalf("cwd: %q", got)
	}
	out := js.StringifyValueCompact(LddOf(transcript, []string{"add-x", "gone"}))
	for _, want := range []string{`"add-x":{"total":3,"completed":2`, `"slice":"Slice 2: two"`, `{"name":"Slice 1: one","total":2,"completed":2}`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	if strings.Contains(out, `"gone"`) {
		t.Errorf("a change the project does not hold is absent: %s", out)
	}
	write(t, transcript, `{"type":"user","cwd":"`+filepath.Join(root, "nowhere")+`"}`+"\n")
	if out := js.StringifyValueCompact(LddOf(transcript, []string{"add-x"})); out != "{}" {
		t.Errorf("a project that is gone: %s", out)
	}
}

// L842 A1: the payload keeps its keys and adds the current slice (an
// outline one included) and how many slices are done, so the page tells
// an outline current slice from a change whose slices are all done.
func TestLddViewOutlineCurrent(t *testing.T) {
	if os.Getenv("JHON_ROOT") == "" {
		repo, _ := filepath.Abs("../../..")
		t.Setenv("JHON_ROOT", repo)
	}
	root := t.TempDir()
	change := filepath.Join(root, "openspec", "changes", "add-x")
	if err := os.MkdirAll(change, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(change, "tasks.md"), "## Slice 1: one\n\n- [x] 1.1 a\n\n## Slice 2: two\n\nDelivers: y\n")
	transcript := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, transcript, `{"type":"user","cwd":"`+root+`"}`+"\n")
	var views map[string]json.RawMessage
	if err := json.Unmarshal([]byte(js.StringifyValueCompact(LddOf(transcript, []string{"add-x"}))), &views); err != nil || views["add-x"] == nil {
		t.Fatalf("no view: %v %v", err, views)
	}
	out := string(views["add-x"])
	var got []string
	dec := json.NewDecoder(strings.NewReader(out))
	for depth := 0; ; {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch tok {
		case json.Delim('{'), json.Delim('['):
			depth++
			continue
		case json.Delim('}'), json.Delim(']'):
			depth--
			continue
		}
		if k, ok := tok.(string); ok && depth == 1 {
			got = append(got, k)
			// Skip the value: a scalar is the next token; a nested one is walked at depth 2+.
			if dec.More() {
				var v json.RawMessage
				if err := dec.Decode(&v); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	want := []string{"total", "completed", "slices", "slice", "unchecked", "artifacts", "next", "notes", "tests", "route", "current", "slicesDone"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("keys %v, want %v", got, want)
	}
	for _, want := range []string{`"slice":null`, `"current":"Slice 2: two"`, `"slicesDone":1`, `"next":"tasks"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
}
