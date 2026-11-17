// `jhon status [change] [--json]`. The status object itself is unit-tested in
// internal/ldd (status_test.go); these cases run the binary.
//
// Each fixture under test/testdata/status/ is a project root holding an
// openspec/ tree. Status is informational: it reports facts and one advisory
// `next` suggestion, and never blocks a phase.
package ldd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

var (
	statusFix = support.Fixture("status")
	cycle     = filepath.Join(statusFix, "cycle")
)

func jhonGit(t *testing.T, dir string, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, dir, support.Env(), args...)
}

func statusJSON(t *testing.T, dir string, args ...string) map[string]any {
	t.Helper()
	return object(t, jhonGit(t, dir, append(append([]string{"status"}, args...), "--json")...).Stdout)
}

func TestStatusJSON(t *testing.T) {
	t.Parallel()
	r := jhonGit(t, cycle, "status", "tasks-partial", "--json")
	equal(t, r.Code, 0, r.Stderr)
	s := object(t, r.Stdout)
	equal(t, s["change"], "tasks-partial")
	equal(t, s["next"], "apply")
}

func TestStatusHumanReportOrder(t *testing.T) {
	t.Parallel()
	r := jhonGit(t, filepath.Join(statusFix, "single"), "status")
	equal(t, r.Code, 0, r.Stderr)
	var order []int
	for _, h := range []string{"change", "artifacts", "tasks", "tests", "next"} {
		loc := regexp.MustCompile(`(?m)^` + h + `\b`).FindStringIndex(r.Stdout)
		if loc == nil {
			t.Fatalf("no %s heading:\n%s", h, r.Stdout)
		}
		order = append(order, loc[0])
	}
	if !slices.IsSorted(order) {
		t.Errorf("headings out of order %v:\n%s", order, r.Stdout)
	}
}

func TestStatusUnselectableChange(t *testing.T) {
	t.Parallel()
	r := jhonGit(t, cycle, "status", "--json")
	if r.Code == 0 {
		t.Error("exit 0 with no change selected")
	}
	equal(t, object(t, r.Stdout)["next"], "select-change")
	ghost := jhonGit(t, cycle, "status", "ghost", "--json")
	if ghost.Code == 0 {
		t.Error("exit 0 for an unknown change")
	}
	equal(t, object(t, ghost.Stdout)["next"], "select-change")
}

func TestStatusIdle(t *testing.T) {
	t.Parallel()
	none := filepath.Join(statusFix, "none")
	r := jhonGit(t, none, "status", "--json")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, object(t, r.Stdout)["next"], "start-change")
	h := jhonGit(t, none, "status")
	equal(t, h.Code, 0, h.Stderr)
	match(t, h.Stdout, `(?m)^change\s+none selected`)
	match(t, h.Stdout, `(?m)^next\s+start-change`)
	noMatch(t, h.Stdout, `(?m)^blockers`)
	match(t, h.Stdout, `no active change`)
	// After set-up, the next step names the record and a recorded run (T104, L682).
	match(t, h.Stdout, `(?m)^  - no active change under openspec/changes/; before you finish, start one \(tasks\.md\) and record a run \(`+"`jhon executions run`"+`\)$`)
}

func TestStatusBrief(t *testing.T) {
	t.Parallel()
	r := jhonGit(t, cycle, "status", "tasks-partial", "--brief")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, strings.TrimRight(r.Stdout, " \t\r\n"), strings.Join([]string{
		"change      tasks-partial",
		"next        apply  (suggestion)",
		"tasks       1/3 checked",
		"tests       no recorded run",
		"strict_tdd  true",
		"tree        none",
	}, "\n"))
	f := jhonGit(t, cycle, "status", "verify-fail", "--brief")
	match(t, f.Stdout, `(?m)^next\s+verify\s+\(suggestion\)$`)
	noMatch(t, f.Stdout, `(?m)^(artifacts|projects|phases|deliver|blockers|phase)\b`)
	if both := jhonGit(t, cycle, "status", "tasks-partial", "--brief", "--json"); both.Code == 0 {
		t.Error("--brief and --json are exclusive")
	}
}

// ── strict_tdd: the one resolution apply and verify read ───────────────────
// Fixtures 7 and 8 of the Strict TDD resolution (contracts/tdd.md); the
// resolutions themselves are asserted in internal/ldd. Built in temporary
// directories: a project with openspec/config.yaml, two unrelated projects
// (coverage unknown), and two changes copied from the cycle fixtures, one
// routed to apply and one to verify.

// copyDir copies the directory src to dst.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
}

func strictProject(t *testing.T, config *string) string {
	t.Helper()
	root := support.GitRepo(t, t.TempDir())
	files := map[string]string{
		"A/pyproject.toml": "[tool.pytest.ini_options]\n",
		"B/Cargo.toml":     "[package]\nname = \"b\"\n",
		// M4: A and B each resolve a command, so C (source, no command:
		// unknown) is what keeps this project incapable of Strict TDD.
		"C/pyproject.toml": "[project]\nname = \"c\"\n",
		"C/app.py":         "print('c')\n",
	}
	if config != nil {
		files["openspec/config.yaml"] = *config
	}
	support.Write(t, root, files)
	changes := filepath.Join(root, "openspec", "changes")
	src := filepath.Join(cycle, "openspec", "changes", "tasks-partial")
	copyDir(t, src, filepath.Join(changes, "to-apply"))
	copyDir(t, src, filepath.Join(changes, "to-verify"))
	tasks := filepath.Join(changes, "to-verify", "tasks.md")
	support.Write(t, filepath.Dir(tasks), map[string]string{"tasks.md": strings.ReplaceAll(support.Read(t, tasks), "- [ ]", "- [x]")})
	return root
}

func ptr(s string) *string { return &s }

func TestStatusFixture8SameStrictTDD(t *testing.T) {
	t.Parallel()
	for _, config := range []*string{ptr("strict_tdd: true\n"), ptr("strict_tdd: false\n"), nil} {
		root := strictProject(t, config)
		raw := jhonGit(t, root, "status", "to-apply", "--json").Stdout
		equal(t, keys(t, field(t, []byte(raw), "strict_tdd")), []string{"value", "source", "evidence", "reason", "notes"})
		equal(t, statusJSON(t, root, "to-verify")["strict_tdd"], object(t, raw)["strict_tdd"])
	}
}

func TestStatusIdleStrictTDDInvalid(t *testing.T) {
	t.Parallel()
	root := strictProject(t, ptr("strict_tdd: true\n"))
	if err := os.RemoveAll(filepath.Join(root, "openspec", "changes")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "openspec", "changes", "archive"), 0o777); err != nil {
		t.Fatal(err)
	}
	r := jhonGit(t, root, "status", "--json")
	equal(t, r.Code, 0, r.Stderr)
	s := object(t, r.Stdout)
	tdd, _ := s["strict_tdd"].(map[string]any)
	equal(t, []any{tdd["value"], tdd["evidence"], s["next"]}, []any{true, "invalid", "start-change"})
	equal(t, jhonGit(t, root, "status").Code, 0)
}

func TestStatusLegacyStrictTDDNote(t *testing.T) {
	t.Parallel()
	r := jhonGit(t, strictProject(t, ptr("strict_tdd: false\n")), "status", "--json")
	var notes []string
	for _, n := range object(t, r.Stdout)["notes"].([]any) {
		notes = append(notes, n.(string))
	}
	match(t, strings.Join(notes, "\n"), `/jhon-ldd init`, r.Stderr)
}

func TestStatusHumanStrictTDD(t *testing.T) {
	t.Parallel()
	r := jhonGit(t, strictProject(t, ptr("strict_tdd: true\n")), "status", "to-apply")
	match(t, r.Stdout, `(?m)^strict_tdd\s+true\s+\(source legacy, evidence invalid\)`)
}

// ── git ─────────────────────────────────────────────────────────────────────
// Status reports the root's VCS state as a fact. Below `ready` it adds a note
// and keeps suggesting the same phase: git is never a gate here.

// vcsProject is a root with one project and two changes.
func vcsProject(t *testing.T, setup func(testing.TB, string) string) string {
	t.Helper()
	root := t.TempDir()
	support.Write(t, root, map[string]string{"pyproject.toml": "[project]\nname = \"app\"\n\n[tool.pytest.ini_options]\n"})
	changes := filepath.Join(root, "openspec", "changes")
	src := filepath.Join(cycle, "openspec", "changes", "tasks-partial")
	copyDir(t, src, filepath.Join(changes, "to-apply"))
	copyDir(t, src, filepath.Join(changes, "other"))
	return setup(t, root)
}

func TestStatusVCS(t *testing.T) {
	t.Parallel()
	states := vocab(t, "vcs_state")
	for _, c := range []struct {
		label string
		setup func(testing.TB, string) string
		want  string
	}{
		{"no git", func(_ testing.TB, r string) string { return r }, states[0]},
		{"git init without a commit", support.GitInit, states[1]},
	} {
		t.Run(c.label+": jhon status prints vcs "+c.want+" and the unchanged suggestion", func(t *testing.T) {
			t.Parallel()
			r := jhonGit(t, vcsProject(t, c.setup), "status", "to-apply")
			equal(t, r.Code, 0, r.Stderr)
			match(t, r.Stdout, `(?m)^vcs\s+`+c.want+`\b`)
			match(t, r.Stdout, `(?m)^next\s+apply`)
		})
		t.Run(c.label+": with no active change, jhon status exits 0", func(t *testing.T) {
			t.Parallel()
			root := vcsProject(t, c.setup)
			changes := filepath.Join(root, "openspec", "changes")
			if err := os.RemoveAll(changes); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(changes, 0o777); err != nil {
				t.Fatal(err)
			}
			r := jhonGit(t, root, "status")
			equal(t, r.Code, 0, r.Stdout)
		})
	}
}

// ── the last test run ───────────────────────────────────────────────────────
// `jhon executions run` writes the change's execution record; status reports
// its result as a fact.

func TestStatusFailingLastRun(t *testing.T) {
	t.Parallel()
	root := vcsProject(t, support.GitRepo)
	support.Write(t, root, map[string]string{"openspec/changes/to-apply/execution-record.json": `{"schema":"jhon.execution-record/v1","timestamp":"2026-10-08T10:00:00Z",` +
		`"candidate_commit":"` + strings.Repeat("c", 40) + `","candidate_tree":"` + strings.Repeat("t", 40) + `","working_tree_clean":true,` +
		`"executions":[{"project":"p0","command":"true","cwd":"p0","exit_code":0},{"project":"p1","command":"true","cwd":"p1","exit_code":1}]}`})
	r := jhonGit(t, root, "status", "to-apply")
	match(t, r.Stdout, `(?m)^tests\s+fail\b.*p1`)
}

// `jhon status` reports the lines of code the slice in progress adds, in
// JSON under route.slice and in the human report, and notes one over the
// advisory size (the route's verified line count) with its real count;
// `next` stays a suggestion (S10).
func TestStatusSliceSize(t *testing.T) {
	t.Parallel()
	root := temp(t)
	support.Write(t, root, map[string]string{"openspec/config.yaml": "schema: spec-driven\n", "src/base.py": "X = 1\n"})
	support.GitRepo(t, root)
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n",
		"src/app.py": strings.Repeat("x = 1\n", 450), "docs/guide.md": strings.Repeat("x\n", 450)})
	note := "slice 1 adds 450 lines of code, over the advisory 400: say so to the owner and offer to split it into slices that each build, test, and deliver"
	s := statusJSON(t, root)
	route, _ := s["route"].(map[string]any)
	equal(t, route["slice"], map[string]any{"slice": "1", "lines": float64(450), "advisory": float64(400)}, "route.slice")
	if notes, _ := s["notes"].([]any); !slices.Contains(notes, any(note)) {
		t.Errorf("no size note: %v", notes)
	}
	h := jhonGit(t, root, "status")
	equal(t, h.Code, 0, h.Stderr)
	match(t, h.Stdout, `(?m)^slice       slice 1 adds 450 lines of code \(advisory 400\)$`)
	match(t, h.Stdout, regexp.QuoteMeta("  - "+note))
}

// T127 (L837): `jhon status` counts done over every `## Slice` heading of an
// outline, names the current and the next, and notes only checkbox lines
// in a slice after the current one.
func TestStatusSlicesOutline(t *testing.T) {
	t.Parallel()
	tasks := "## Intent\n\nWhy. Accepted when: CA-01.\n\n"
	for i, name := range []string{"domain", "capture", "durations", "blocking", "cross-validation", "export"} {
		tasks += fmt.Sprintf("## Slice %d: %s\n\nDelivers: CA-0%d\n\n", i+1, name, i+1)
		switch i {
		case 0, 1:
			tasks += fmt.Sprintf("- [x] %d.1 RED\n- [x] %d.2 GREEN\n\n", i+1, i+1)
		case 2:
			for j := 1; j <= 14; j++ {
				tasks += fmt.Sprintf("- [ ] 3.%d task\n", j)
			}
			tasks += "\n"
		case 4:
			tasks += "- [ ] 5.1 RED\n- [ ] 5.2 GREEN\n\n"
		}
	}
	root := tree(t, map[string]string{"openspec/config.yaml": "schema: spec-driven\n", "openspec/changes/c1/tasks.md": tasks})
	h := jhonGit(t, root, "status", "c1")
	equal(t, h.Code, 0, h.Stderr)
	match(t, h.Stdout, `(?m)^slices      2/6 done, current Slice 3: durations \(0/14 checked\), next Slice 4: blocking \(outline\)$`)
	match(t, h.Stdout, regexp.QuoteMeta("1 task(s) in a later slice than the current one (Slice 5: cross-validation); write them when its turn comes"))
	s := statusJSON(t, root, "c1")
	tk, _ := s["tasks"].(map[string]any)
	equal(t, tk["slicesDone"], float64(2), "slicesDone")
	equal(t, tk["current"], "Slice 3: durations", "current")
	equal(t, tk["next"], "Slice 4: blocking", "next")
	equal(t, tk["slice"], "Slice 3: durations", "slice")
	sl, _ := tk["slices"].([]any)
	equal(t, len(sl), 6, "slices")
	four, _ := sl[3].(map[string]any)
	equal(t, four["outline"], true, "slice 4 outline")
	three, _ := sl[2].(map[string]any)
	equal(t, three["outline"], false, "slice 3 outline")

	// An old single-slice tasks.md reads as before.
	old := tree(t, map[string]string{"openspec/config.yaml": "schema: spec-driven\n", "openspec/changes/c1/tasks.md": "## Slice 1: a\n\nDelivers: x\n\n- [x] 1.1 RED\n- [ ] 1.2 GREEN\n"})
	h = jhonGit(t, old, "status", "c1")
	equal(t, h.Code, 0, h.Stderr)
	match(t, h.Stdout, `(?m)^slices      0/1 done, current Slice 1: a \(1/2 checked\)$`)
	noMatch(t, h.Stdout, `next Slice|outline|later slice`)
}

// L842 A1: an outline current slice is next to be tasked: status suggests
// `tasks`, never `archive`, also when a later slice holds tasks; the old
// single planning task reads as before; archive comes once every outlined
// slice is done.
func TestStatusSlicesOutlineCurrent(t *testing.T) {
	t.Parallel()
	first := "## Slice 1: a\n\nDelivers: x\n\n- [x] 1.1 a\n\n"
	note := "Slice 2: b is next: write its spec delta and tasks, and revise the slices after it"
	for _, c := range []struct{ name, tasks, line string }{
		{"outline current", first + "## Slice 2: b\n\nDelivers: y\n\n## Slice 3: c\n\nDelivers: z\n",
			`slices      1/3 done, current Slice 2: b \(outline\), next Slice 3: c \(outline\)`},
		{"outline before a tasked slice", first + "## Slice 2: b\n\nDelivers: y\n\n## Slice 3: c\n\nDelivers: z\n\n- [ ] 3.1 c\n",
			`slices      1/3 done, current Slice 2: b \(outline\), next Slice 3: c \(0/1 checked\)`},
		{"the old single planning task", first + "## Slice 2: b\n\nDelivers: y\n\n- [ ] 2.1 Plan the slice\n",
			`slices      1/2 done, current Slice 2: b \(0/1 checked\)`},
	} {
		root := recordRepo(t, map[string]string{"docs/guide.md": "# Guide\n", "openspec/changes/c1/tasks.md": c.tasks})
		s := statusJSON(t, root, "c1")
		equal(t, s["next"], "tasks", c.name, s["notes"])
		equal(t, anyMatch(s["notes"], "^"+regexp.QuoteMeta(note)+"$"), true, c.name, s["notes"])
		tk, _ := s["tasks"].(map[string]any)
		equal(t, tk["current"], "Slice 2: b", c.name)
		equal(t, tk["slicesDone"], float64(1), c.name)
		h := jhonGit(t, root, "status", "c1")
		equal(t, h.Code, 0, h.Stderr)
		match(t, h.Stdout, `(?m)^`+c.line+`$`)
		match(t, h.Stdout, `(?m)^next        tasks  \(suggestion\)$`)
	}

	all := first + "## Slice 2: b\n\nDelivers: y\n\n- [x] 2.1 b\n\n## Slice 3: c\n\nDelivers: z\n\n- [x] 3.1 c\n"
	root := recordRepo(t, map[string]string{"docs/guide.md": "# Guide\n", "openspec/changes/c1/tasks.md": all})
	s := statusJSON(t, root, "c1")
	equal(t, s["next"], "archive", s["notes"])
	equal(t, anyMatch(s["notes"], "is next: write its"), false, s["notes"])
	tk, _ := s["tasks"].(map[string]any)
	equal(t, tk["slicesDone"], float64(3), "slicesDone")
	equal(t, tk["current"], nil, "current")
	h := jhonGit(t, root, "status", "c1")
	match(t, h.Stdout, `(?m)^slices      3/3 done$`)
	match(t, h.Stdout, `(?m)^next        archive  \(suggestion\)$`)
}
