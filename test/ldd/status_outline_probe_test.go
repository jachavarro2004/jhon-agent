package ldd

// T127 verifier probes (scratch only).

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestProbeT127OutlineCurrentSuggestsTasks(t *testing.T) {
	t.Parallel()
	first := "## Slice 1: a\n\nDelivers: x\n\n- [x] 1.1 a\n\n"
	for _, c := range []struct {
		name    string
		files   map[string]string
		next    string
		current any
		note    string
	}{
		{"outline current, a passing verify report", map[string]string{"openspec/changes/c1/tasks.md": first + "## Slice 2: b\n\nDelivers: y\n", "openspec/changes/c1/verify-report.md": "verdict: pass\n"}, "tasks", "Slice 2: b", "Slice 2: b"},
		{"every slice outlined, none tasked", map[string]string{"openspec/changes/c1/tasks.md": "## Intent\n\nWhy.\n\n## Slice 1: a\n\nDelivers: x\n\n## Slice 2: b\n\nDelivers: y\n"}, "tasks", "Slice 1: a", ""},
		{"current tasked, a later outline", map[string]string{"openspec/changes/c1/tasks.md": first + "## Slice 2: b\n\nDelivers: y\n\n- [ ] 2.1 b\n\n## Slice 3: c\n\nDelivers: z\n"}, "apply", "Slice 2: b", ""},
		{"old plan task, a later outline", map[string]string{"openspec/changes/c1/tasks.md": first + "## Slice 2: b\n\nDelivers: y\n\n- [ ] 2.1 Plan the slice\n\n## Slice 3: c\n\nDelivers: z\n"}, "tasks", "Slice 2: b", "Slice 2: b"},
		{"all done with a passing verify", map[string]string{"openspec/changes/c1/tasks.md": first + "## Slice 2: b\n\nDelivers: y\n\n- [x] 2.1 b\n", "openspec/changes/c1/verify-report.md": "verdict: pass\n"}, "archive", nil, ""},
	} {
		files := map[string]string{"docs/guide.md": "# Guide\n"}
		for k, v := range c.files {
			files[k] = v
		}
		root := recordRepo(t, files)
		s := statusJSON(t, root, "c1")
		equal(t, s["next"], c.next, c.name, s["notes"])
		tk, _ := s["tasks"].(map[string]any)
		equal(t, tk["current"], c.current, c.name)
		if c.note != "" {
			equal(t, anyMatch(s["notes"], "^"+regexp.QuoteMeta(c.note+" is next: write its spec delta and tasks, and revise the slices after it")+"$"), true, c.name, s["notes"])
		} else {
			equal(t, anyMatch(s["notes"], "is next: write its"), false, c.name, s["notes"])
		}
	}
}

func TestProbeT127LaterSliceNote(t *testing.T) {
	t.Parallel()
	src := "## Slice 1: a\n\nDelivers: x\n\n- [x] 1.1 a\n\n## Slice 2: b\n\nDelivers: y\n\n## Slice 3: c\n\nDelivers: z\n\n- [ ] 3.1 c\n- [ ] 3.2 c\n"
	root := tree(t, map[string]string{"openspec/config.yaml": "schema: spec-driven\n", "openspec/changes/c1/tasks.md": src})
	s := statusJSON(t, root, "c1")
	equal(t, anyMatch(s["notes"], "^"+regexp.QuoteMeta("1 task(s) in a later slice than the current one (Slice 3: c); write them when its turn comes, on what exists then")+"$"), true, s["notes"])
	equal(t, anyMatch(s["notes"], "planned ahead"), false, s["notes"])
	root = tree(t, map[string]string{"openspec/config.yaml": "schema: spec-driven\n", "openspec/changes/c1/tasks.md": "## Slice 1: a\n\nDelivers: x\n\n- [ ] 1.1 a\n\n## Slice 2: b\n\nDelivers: y\n"})
	s = statusJSON(t, root, "c1")
	equal(t, anyMatch(s["notes"], "later slice"), false, s["notes"])
}

func TestProbeT127RankedChanges(t *testing.T) {
	t.Parallel()
	root := tree(t, map[string]string{"openspec/config.yaml": "schema: spec-driven\n",
		"openspec/changes/c-done/tasks.md":    "## Slice 1: a\nDelivers: A\n- [x] 1.1 a\n",
		"openspec/changes/c-outline/tasks.md": "## Slice 1: a\nDelivers: A\n- [x] 1.1 a\n\n## Slice 2: b\nDelivers: B\n",
		"openspec/changes/c-bare/tasks.md":    "## Slice 1: a\nDelivers: A\n\n## Slice 2: b\nDelivers: B\n",
		"openspec/changes/c-open/tasks.md":    "## Slice 1: a\nDelivers: A\n- [ ] 1.1 a\n",
	})
	now := time.Now()
	for c, ago := range map[string]time.Duration{"c-outline": 4 * time.Hour, "c-bare": 3 * time.Hour, "c-open": 2 * time.Hour, "c-done": time.Minute} {
		w := now.Add(-ago)
		if err := os.Chtimes(filepath.Join(root, "openspec/changes", c, "tasks.md"), w, w); err != nil {
			t.Fatal(err)
		}
	}
	r := jhonGit(t, root, "status", "--json")
	s := object(t, r.Stdout)
	equal(t, s["rankedChanges"], []any{"c-open", "c-bare", "c-outline", "c-done"}, r.Stdout)
}

func TestProbeT127DeliverCheckMerge(t *testing.T) {
	t.Parallel()
	cfg := "delivery: change\ntest_command_override:\n  a: \"true\"\n"
	outline := "## Slice 1: a\nDelivers: A\n- [x] 1.1 a\n\n## Slice 2: b\nDelivers: B\n\n## Slice 3: c\nDelivers: C\n"
	want := "delivery: change in openspec/config.yaml merges the change once, after its last slice; 2 slice(s) of tasks.md are outlined with no tasks yet (Slice 2: b, Slice 3: c): commit each slice on the change's branch and merge when every slice is done"
	root := deliverProject(t, "true")
	support.Write(t, root, map[string]string{"openspec/config.yaml": cfg, deliverDir + "/tasks.md": outline})
	commit(t, root, "outline")
	tested(t, root)
	r := check(t, root, "--action", "merge")
	equal(t, r.json["ready"], false, r.Out())
	if r.Code == 0 {
		t.Errorf("not ready exits 0: %s", r.Out())
	}
	equal(t, reasons(r), `["`+want+`"]`, r.Out())
	d := check(t, root, "--action", "deploy", "--artifact", digest)
	equal(t, d.json["ready"], true, d.Out())

	support.Write(t, root, map[string]string{deliverDir + "/tasks.md": outline + "- [ ] 3.1 c\n"})
	commit(t, root, "unchecked")
	tested(t, root)
	r = check(t, root, "--action", "merge")
	equal(t, r.json["ready"], false, r.Out())
	match(t, reasons(r), `1 task\(s\) of tasks\.md are unchecked \(3\.1 c\)`, r.Out())
	match(t, reasons(r), `1 slice\(s\) of tasks\.md are outlined with no tasks yet \(Slice 2: b\)`, r.Out())

	support.Write(t, root, map[string]string{deliverDir + "/tasks.md": "## Slice 1: a\nDelivers: A\n- [x] 1.1 a\n\n## Slice 2: b\nDelivers: B\n- [x] 2.1 b\n"})
	commit(t, root, "done")
	tested(t, root)
	r = check(t, root, "--action", "merge")
	equal(t, r.Code, 0, r.Out())
	equal(t, r.json["ready"], true, r.Out())
	match(t, r.Out(), `merge with rebase`)

	per := deliverProject(t, "true")
	support.Write(t, per, map[string]string{deliverDir + "/tasks.md": outline})
	commit(t, per, "outline")
	tested(t, per)
	r = check(t, per, "--action", "merge")
	equal(t, r.json["ready"], true, r.Out())
}
