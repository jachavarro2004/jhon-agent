package ldd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// T223: `jhon status` suggests archive only when `jhon archive` takes the
// change. A change's RED before GREEN on record always does; without one,
// only a change that alters no behaviour whose diff against the default
// branch is not empty (outside its record) does: one whose route records no
// tests by design (S2, L4). An empty diff, after its merge or before
// anything is applied, earns no exemption, nor does a change that alters
// behaviour: those stay at apply, with the note and the command that
// record the RED and the GREEN (RedGreenHint), in the text and in --json.
func TestStatusSuggestsArchiveOnlyWhenArchiveTakesIt(t *testing.T) {
	jhonRoot(t)
	for _, c := range []struct{ name, config, report string }{
		{"standard route", "", ""},
		{"verified route", "route_verified_tier: passive\n", "verdict: pass\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			change := func(t *testing.T, files ...string) string {
				t.Helper()
				root := configuredRepo(t, c.config, files...)
				write(t, filepath.Join(root, "openspec/changes/c/tasks.md"), "## Intent\nDelivers: the guide\n\n- [x] 1.1 guide\n")
				if c.report != "" {
					write(t, filepath.Join(root, "openspec/changes/c/verify-report.md"), c.report)
				}
				return root
			}
			hint := "no RED before GREEN on record for c: " + RedGreenHint("c")
			owes := func(t *testing.T, root, what string) {
				t.Helper()
				st := statusOf(t, root, s("c"))
				if st.Next != "apply" || !strings.Contains(notes(st), hint) || !strings.HasPrefix(st.Command, "jhon executions run c") {
					t.Errorf("%s: next %s, command %q, notes %q", what, st.Next, st.Command, st.Notes)
				}
				text := Human(st)
				if !strings.Contains(text, "next        apply  (suggestion)") || !strings.Contains(text, "command     jhon executions run c") || !strings.Contains(text, hint) {
					t.Errorf("%s: the text:\n%s", what, text)
				}
				var js map[string]any
				b, _ := json.Marshal(st)
				if err := json.Unmarshal(b, &js); err != nil {
					t.Fatal(err)
				}
				if js["next"] != "apply" || !strings.Contains(strings.Join(strs(js["notes"]), "\n"), hint) {
					t.Errorf("%s: --json next %v, notes %v", what, js["next"], js["notes"])
				}
			}
			archives := func(t *testing.T, root, what string) {
				t.Helper()
				st := statusOf(t, root, s("c"))
				if st.Next != "archive" || strings.Contains(notes(st), "RED before GREEN") || st.Command != "" {
					t.Errorf("%s: next %s, command %q, notes %q", what, st.Next, st.Command, st.Notes)
				}
			}

			// A docs-only change whose diff is not empty: no test by design.
			docs := change(t, "docs/guide.md")
			if st := statusOf(t, docs, s("c")); st.Route == nil || st.Route.Behaviour || (c.report != "" && st.Route.Level != RouteVerified) {
				t.Fatalf("the fixture: %+v", st.Route)
			}
			archives(t, docs, "a docs-only change with a diff")

			// The same change once merged: its diff is empty, no exemption.
			gitT(t, docs, "add", "-A")
			gitT(t, docs, "commit", "-q", "-m", "c")
			gitT(t, docs, "branch", "-f", "main", "HEAD")
			owes(t, docs, "the docs-only change after its merge")

			// A change that alters behaviour (on the standard route: the
			// verified one owes its reviews first).
			roots := []string{docs}
			if c.config == "" {
				code := change(t, "src/app.py", "tests/test_app.py")
				owes(t, code, "a behaviour change")
				roots = append(roots, code)
			}

			// PRESERVE: with its RED before GREEN on record, each is archived.
			for _, root := range roots {
				ran(t, filepath.Join(root, "openspec/changes/c/execution-record.json"), true)
				lightVerified(t, filepath.Join(root, "openspec/changes/c/execution-record.json"))
				archives(t, root, "RED before GREEN on record")
			}
		})
	}
}

func strs(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, x := range items {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
