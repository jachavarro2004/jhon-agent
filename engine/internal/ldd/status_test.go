package ldd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The status tests that call the package directly. Each
// fixture under test/testdata/status/ is a project root holding an openspec/
// tree; the CLI is covered by the conformance tests under test/.

var statusFixtures, _ = filepath.Abs("../../../test/testdata/status")
var cycle = filepath.Join(statusFixtures, "cycle")

func statusOf(t *testing.T, root string, name *string) Status {
	t.Helper()
	jhonRoot(t)
	s, err := StatusOf(root, name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func of(t *testing.T, change string) Status { t.Helper(); return statusOf(t, cycle, &change) }

func stateOf(s Status, artifact string) string {
	for _, a := range s.Artifacts {
		if a.Artifact == artifact {
			return a.State
		}
	}
	return ""
}

func notes(s Status) string { return strings.Join(s.Notes, "\n") }

func TestChangeSelection(t *testing.T) {
	s := statusOf(t, filepath.Join(statusFixtures, "single"), nil)
	if !s.Ok || *s.Change != "only" || *s.Directory != "openspec/changes/only" {
		t.Errorf("one active change is selected: %+v", s)
	}
	s = statusOf(t, cycle, nil)
	if s.Ok || s.Next != "select-change" || !slices.Contains(s.ActiveChanges, "fresh") || !slices.Contains(s.ActiveChanges, "verify-pass") || slices.Contains(s.ActiveChanges, "archive") {
		t.Errorf("several active changes: %+v", s)
	}
	s = statusOf(t, filepath.Join(statusFixtures, "none"), nil)
	if !s.Ok || s.Change != nil || s.Next != "start-change" || len(s.ActiveChanges) != 0 || s.ActiveChanges == nil || !strings.Contains(notes(s), "no active change") {
		t.Errorf("idle: %+v", s)
	}
	for _, name := range []string{"ghost", "archive"} {
		if s := of(t, name); s.Ok || s.Next != "select-change" || !strings.Contains(notes(s), "does not exist or is not an active change") {
			t.Errorf("%s: %+v", name, s)
		}
	}
}

func TestFixtureRouting(t *testing.T) {
	s := of(t, "fresh")
	var states, names []string
	for _, a := range s.Artifacts {
		states, names = append(states, a.State), append(names, a.Artifact)
	}
	if !slices.Equal(states, slices.Repeat([]string{"missing"}, 8)) || !slices.Equal(names, []string{"exploration.md", "research.md", "proposal.md", "specs/", "design.md", "tasks.md", "tdd-evidence.md", "verify-report.md"}) ||
		s.Artifacts[2].Path != "openspec/changes/fresh/proposal.md" || s.Next != "propose" || s.Tests != nil {
		t.Errorf("fresh: %+v", s)
	}
	for _, c := range []struct{ change, artifact, state, next string }{
		{"research-skeleton", "research.md", "partial", "propose"},
		{"research-done", "research.md", "done", "propose"},
		{"research-done", "exploration.md", "done", "propose"},
		{"research-unsourced", "research.md", "partial", ""},
		{"proposal-placeholder", "proposal.md", "partial", "propose"},
		{"spec-no-scenario", "specs/", "partial", "spec"},
		// No RED before GREEN on record: never archive (T31).
		{"verify-pass", "specs/", "done", "apply"},
		{"verify-pass", "verify-report.md", "done", "apply"},
		{"verify-fail", "verify-report.md", "partial", "verify"},
		{"skipped-design", "design.md", "missing", "tasks"},
		{"crlf", "proposal.md", "done", "apply"},
		{"crlf", "specs/", "done", "apply"},
	} {
		s := of(t, c.change)
		if stateOf(s, c.artifact) != c.state || (c.next != "" && s.Next != c.next) {
			t.Errorf("%s: %s is %s, next %s; want %s, %s", c.change, c.artifact, stateOf(s, c.artifact), s.Next, c.state, c.next)
		}
	}
	if !strings.Contains(notes(of(t, "verify-fail")), "verdict") {
		t.Error("a failing report is noted")
	}
	if !of(t, "verify-pass").Tasks.AllComplete {
		t.Error("verify-pass has every task checked")
	}
	if u := of(t, "crlf").Tasks.Unchecked; !slices.Equal(u, []string{"1.2 Second"}) {
		t.Errorf("crlf unchecked: %q", u)
	}
}

func TestTasksAndOutOfRepoNotes(t *testing.T) {
	s := of(t, "tasks-partial")
	want := TaskProgress{3, 1, 2, false, []string{"1.2 Wire the timer in `src/auth/timer.ts`, reading `/etc/timezone` (read-only)", "1.3 Update `../other-repo/config.ts`"}, []SliceProgress{}, nil, 0, nil, nil}
	if !reflect.DeepEqual(s.Tasks, want) || s.Next != "apply" || !strings.Contains(notes(s), "../other-repo/config.ts") {
		t.Errorf("tasks-partial: %+v, next %s, notes %q", s.Tasks, s.Next, s.Notes)
	}
	// contracts/ldd.md "Slices": once a slice is delivered,
	// the next step plans the following one, not applies it.
	if s := of(t, "slice-to-plan"); s.Next != "tasks" || !strings.Contains(notes(s), "Slice 2: warn is next") {
		t.Errorf("slice-to-plan: next %s, notes %q", s.Next, s.Notes)
	}
	if s := of(t, "tasks-out-of-scope"); s.Next != "apply" || !strings.Contains(notes(s), "/etc/hosts") {
		t.Errorf("tasks-out-of-scope: %s %q", s.Next, s.Notes)
	}
	outside := func(s Status) string {
		return strings.Join(slices.DeleteFunc(slices.Clone(s.Notes), func(n string) bool { return !strings.Contains(n, "outside the repository") }), "\n")
	}
	if s := of(t, "tasks-http-routes"); s.Next != "apply" || regexp.MustCompile(`/health|/openapi\.json`).MatchString(outside(s)) {
		t.Errorf("HTTP routes are not out-of-repo notes: %q", s.Notes)
	}
}

// Docket AC1: which backticked tokens are edit-target candidates, then
// whether those resolve outside the repository.
func TestEditCandidate(t *testing.T) {
	none := func(string) bool { return false }
	for _, tok := range []string{"/health", "/docs/oauth2-redirect", "/openapi.json", "https://example.com/foo", "HTTPS://x/y", "foo/bar"} {
		if EditCandidate(tok, "/work/repo", none) {
			t.Errorf("%s is not a candidate", tok)
		}
	}
	onDisk := func(p string) bool { return p == "/etc/hosts" }
	for _, tok := range []string{"/etc/hosts", "~/x", "../x", "./x"} {
		if !EditCandidate(tok, "/work/repo", onDisk) {
			t.Errorf("%s is a candidate", tok)
		}
	}
	rootHasSrc := func(p string) bool { return p == "/work/repo/src" }
	for _, tok := range []string{"app/main.py", "docs/api.md", "src/auth"} {
		if !EditCandidate(tok, "/work/repo", rootHasSrc) {
			t.Errorf("%s is a candidate", tok)
		}
	}
	for _, tok := range []string{"/etc/hosts", "~/x", "../x", ".."} {
		if !OutsideRepo(tok, "/work/repo") {
			t.Errorf("%s is outside", tok)
		}
	}
	for _, tok := range []string{"app/main.py", "docs/api.md", "./x", "/work/repo/a"} {
		if OutsideRepo(tok, "/work/repo") {
			t.Errorf("%s is inside", tok)
		}
	}
}

func s(v string) *string { return &v }

func TestArtifactRules(t *testing.T) {
	jhonRoot(t)
	ok := "## Why\n\nx\n\n## What Changes\n\ny\n"
	for _, c := range []struct {
		got, want string
	}{
		{ProposalState(nil), "missing"},
		{ProposalState(s(ok)), "done"},
		{ProposalState(s(ok + "\nTODO later\n")), "partial"},
		{ProposalState(s(ok + "\nOwner: <name>\n")), "partial"},
		{ProposalState(s("## Why\n\n## What Changes\n\ny\n")), "partial"},
		{ProposalState(s("## Why\n\nx\n")), "partial"},
		{ProposalState(s(ok + "\n```\nTODO <x>\n```\n")), "done"},
		{ExplorationState(s("Read `render.ts`.")), "done"},
		{ExplorationState(s("Nothing concrete, ran `git log`.")), "partial"},
		{ExplorationState(s("```\n`src/a.ts`\n```\n")), "partial"},
		{ExplorationState(nil), "missing"},
		{DesignState(s("# Design\n\n## Approach\n\nTBD\n\n## Risks\n\n<list them>\n")), "partial"},
		{DesignState(s("# Design\n\n## Approach\n\nA timer.\n")), "done"},
		{VerifyState(s("# Verification Report\n\nverdict: pass\n")), "done"},
		{VerifyState(s("```yaml\nverdict: pass\n```\n")), "done"},
		{VerifyState(s("Intro first.\n\n**Verdict**: PASS WITH WARNINGS\n")), "done"},
		{VerifyState(s("- verdict: `fail`\n")), "partial"},
		{VerifyState(s("# Report\n\nNo verdict stated.\n")), "partial"},
		{VerifyState(nil), "missing"},
		{TasksState(nil), "missing"},
		{TasksState(s("# Tasks\n")), "partial"},
	} {
		if c.got != c.want {
			t.Errorf("case %+v", c)
		}
	}
	research := func(claims string) *string { return s("## Sources\n\n- [S1] a\n- [S2] b\n\n## Claims\n\n" + claims) }
	if ResearchState(research("- one [S2]\n")) != "done" || ResearchState(research("")) != "partial" ||
		ResearchState(research("- one [S3]\n")) != "partial" || ResearchState(nil) != "missing" ||
		ResearchState(research("- one [S1]\n  more [S9]\n- two\n")) != "partial" {
		t.Error("research needs a claim, and each claim a declared source")
	}
}

func TestProgressOf(t *testing.T) {
	p := ProgressOf(s("- [x] a\n- [X] b\n- [ ] c\n```\n- [ ] d\n```\n"))
	if want := (TaskProgress{3, 2, 1, false, []string{"c"}, []SliceProgress{}, nil, 0, nil, nil}); !reflect.DeepEqual(p, want) {
		t.Errorf("fences ignored, x in either case: %+v", p)
	}
	// contracts/ldd.md "Slices": tasks.md plans the change slice by slice.
	p = ProgressOf(s("# Tasks\n\n## Slice 1: limit check\n- [x] 1.1 RED\n- [x] 1.2 GREEN\n\n## Slice 2: exact limit\n- [x] 2.1 RED\n- [ ] 2.2 GREEN\n\n### Slice 3 — audit trail\n- [ ] 3.1 Plan the slice\n"))
	want := []SliceProgress{
		{Name: "Slice 1: limit check", Total: 2, Completed: 2, Tasks: []SliceTask{{"1.1 RED", true}, {"1.2 GREEN", true}}},
		{Name: "Slice 2: exact limit", Total: 2, Completed: 1, Tasks: []SliceTask{{"2.1 RED", true}, {"2.2 GREEN", false}}},
		{Name: "Slice 3 — audit trail", Total: 1, Tasks: []SliceTask{{"3.1 Plan the slice", false}}},
	}
	if !reflect.DeepEqual(p.Slices, want) || p.Slice == nil || *p.Slice != "Slice 2: exact limit" {
		t.Errorf("slices: %+v", p)
	}
	if line := slicesLine(p); line != "slices      1/3 done, current Slice 2: exact limit (1/2 checked), next Slice 3 — audit trail (0/1 checked)" {
		t.Errorf("slicesLine = %q", line)
	}
	if p := ProgressOf(s("## Slice 1: a\n- [x] done\n")); p.Slice != nil {
		t.Error("every slice done: no current slice")
	}
	// JavaScript's i flag without u does not fold ſ to s; a fifth # is no slice heading.
	if p := ProgressOf(s("## ſlice 1\n##### Slice 2\n## SLICE 3  \n- [ ] a\n")); len(p.Slices) != 1 || p.Slices[0].Name != "SLICE 3" {
		t.Errorf("slice headings: %+v", p.Slices)
	}
	if ProgressOf(nil).Unchecked == nil || ProgressOf(nil).Slices == nil {
		t.Error("no tasks.md is empty progress, never null lists")
	}
}

func TestBrief(t *testing.T) {
	st := of(t, "tasks-partial")
	want := "change      tasks-partial\nnext        apply  (suggestion)\ntasks       1/3 checked\ntests       no recorded run\nstrict_tdd  true\ntree        none"
	if got := Brief(st); got != want {
		t.Errorf("Brief = %q", got)
	}
	st.Tests = &execution.LastRun{Passed: true, Failed: []string{}, Scope: "full", Complete: true, Commit: strings.Repeat("c", 40), Tree: strings.Repeat("t", 40), Clean: true}
	if !regexp.MustCompile(`(?m)^tree\s+t{40}$`).MatchString(Brief(st)) {
		t.Errorf("the tested tree: %s", Brief(st))
	}
	if got := testsLine(&execution.LastRun{Timestamp: "x", Scope: "full", Tree: "é😀abcdefghijk", Failed: []string{"a", "b"}, Clean: true}); got != "partial  x  full  tree é😀abcdefghi  failed: a, b" {
		t.Errorf("testsLine = %q", got)
	}
}

// strictProject is fixtures 7 and 8 of the Strict TDD resolution
// (contracts/tdd.md): two capable projects and an unknown one, and two
// changes copied from the cycle fixtures, one routed to apply and one to verify.
func strictProject(t *testing.T, config *string) string {
	t.Helper()
	files := map[string]string{
		"A/pyproject.toml": "[tool.pytest.ini_options]\n",
		"B/Cargo.toml":     "[package]\nname = \"b\"\n",
		"C/pyproject.toml": "[project]\nname = \"c\"\n",
		"C/app.py":         "print('c')\n",
	}
	if config != nil {
		files["openspec/config.yaml"] = *config
	}
	root := tree(t, files)
	gitT(t, root, "init", "-q")
	gitT(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	copyChange(t, root, "tasks-partial", "to-apply")
	copyChange(t, root, "tasks-partial", "to-verify")
	tasks := filepath.Join(root, "openspec/changes/to-verify/tasks.md")
	b, _ := os.ReadFile(tasks)
	write(t, tasks, strings.ReplaceAll(string(b), "- [ ]", "- [x]"))
	return root
}

// copyChange copies a cycle fixture's change into root under another name.
func copyChange(t *testing.T, root, from, to string) {
	t.Helper()
	src := filepath.Join(cycle, "openspec/changes", from)
	filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(src, p)
			write(t, filepath.Join(root, "openspec/changes", to, rel), string(b))
		}
		return err
	})
}

func TestStrictTddInStatus(t *testing.T) {
	jhonRoot(t)
	root := strictProject(t, cfg("strict_tdd: true\n"))
	apply := statusOf(t, root, s("to-apply"))
	if triple(apply.StrictTdd) != [3]any{true, "legacy", "invalid"} || apply.Next != "apply" || !strings.Contains(notes(apply), "strict_tdd is true but") {
		t.Errorf("fixture 7, true without capability is a note: %+v", apply)
	}
	// Its route is standard, so verify is not due and is not suggested (S28);
	// with its RED before GREEN and a passing light verify on record it is
	// archived (T31, T31b).
	lightVerified(t, filepath.Join(root, "openspec/changes/to-verify/execution-record.json"))
	if statusOf(t, root, s("to-verify")).Next != "archive" {
		t.Error("fixture 7: archive")
	}
	for _, config := range []*string{cfg("strict_tdd: true\n"), cfg("strict_tdd: false\n"), nil} {
		root := strictProject(t, config)
		src, _ := os.ReadFile(filepath.Join(os.Getenv("JHON_ROOT"), "prose/contracts/tdd.md"))
		mode, _ := TddContractMode(string(src))
		want := ResolveStrictTdd(config, mode, discover.Discover(root))
		if !reflect.DeepEqual(statusOf(t, root, s("to-apply")).StrictTdd, want) || !reflect.DeepEqual(statusOf(t, root, s("to-verify")).StrictTdd, want) {
			t.Errorf("fixture 8: status carries the resolution for %v", config)
		}
	}
	none := statusOf(t, strictProject(t, cfg("strict_tdd: false\n")), nil)
	if none.Ok || none.StrictTdd.Source != "legacy" || !strings.Contains(notes(none), "/jhon-ldd init") {
		t.Errorf("no change selected still reports strict_tdd: %+v", none)
	}
	idleRoot := strictProject(t, cfg("strict_tdd: true\n"))
	os.RemoveAll(filepath.Join(idleRoot, "openspec/changes"))
	os.MkdirAll(filepath.Join(idleRoot, "openspec/changes/archive"), 0o755)
	if st := statusOf(t, idleRoot, nil); triple(st.StrictTdd) != [3]any{true, "legacy", "invalid"} || !st.Ok || st.Next != "start-change" {
		t.Errorf("idle: %+v", st)
	}
	required := statusOf(t, strictProject(t, cfg("strict_tdd_policy: require\n")), s("to-apply"))
	if triple(required.StrictTdd) != [3]any{true, "policy", "invalid"} || required.Next != "apply" {
		t.Errorf("J2: %+v", required)
	}
}

func TestSpecDepthNote(t *testing.T) {
	root := tree(t, nil)
	copyChange(t, root, "tasks-partial", "deep")
	write(t, filepath.Join(root, "openspec/changes/deep/specs/auth/spec.md"), "# Auth\n\n## Requirement: Login\n\nThe system MUST log in.\n\n### Scenario: ok\n\n- THEN in\n")
	st := statusOf(t, root, s("deep"))
	if !st.Ok || !regexp.MustCompile(`specs/auth/spec\.md.*line 3.*level 2.*level 3`).MatchString(notes(st)) {
		t.Errorf("depth note: %q", st.Notes)
	}
}

func TestVcsTestsAndEvidence(t *testing.T) {
	plain := tree(t, map[string]string{"pyproject.toml": "[project]\nname = \"app\"\n\n[tool.pytest.ini_options]\n"})
	copyChange(t, plain, "tasks-partial", "to-apply")
	st := statusOf(t, plain, s("to-apply"))
	if st.Vcs.State != "none" || st.Next != "apply" || !regexp.MustCompile(`vcs none\b`).MatchString(notes(st)) {
		t.Errorf("no git is noted, never a gate: %+v", st)
	}
	gitT(t, plain, "init", "-q")
	if st := statusOf(t, plain, s("to-apply")); st.Vcs.State != "no_head" || !strings.Contains(notes(st), "vcs no_head") {
		t.Errorf("no commit: %+v", st.Vcs)
	}
	gitT(t, plain, "commit", "-q", "--allow-empty", "-m", "init")
	st = statusOf(t, plain, s("to-apply"))
	if st.Vcs.State != "ready" || regexp.MustCompile(`\bvcs\b`).MatchString(notes(st)) || stateOf(st, "tdd-evidence.md") != "missing" {
		t.Errorf("ready: %+v", st)
	}
	dir := filepath.Join(plain, "openspec/changes/to-apply")
	write(t, filepath.Join(dir, "tdd-evidence.md"), "anything at all\n")
	write(t, filepath.Join(dir, "execution-record.json"), `{"timestamp":"2026-10-08T10:00:00Z","candidate_commit":"c","candidate_tree":"t","working_tree_clean":true,"executions":[{"project":"p0","exit_code":0},{"project":"p1","exit_code":1}]}`)
	st = statusOf(t, plain, s("to-apply"))
	if stateOf(st, "tdd-evidence.md") != "done" || strings.Contains(notes(st), "tdd-evidence") {
		t.Errorf("tdd-evidence.md is present and never read: %+v", st)
	}
	if st.Tests == nil || st.Tests.Passed || !slices.Equal(st.Tests.Failed, []string{"p1"}) || !strings.HasPrefix(testsLine(st.Tests), "fail  2026-10-08T10:00:00Z  full  tree t  failed: p1") {
		t.Errorf("tests: %+v", st.Tests)
	}
}

// `## Skipped phases` is no longer read (S28): it adds no note.
func TestSkippedPhasesAndRouting(t *testing.T) {
	root := tree(t, map[string]string{
		"openspec/changes/c/proposal.md": "## Why\n\nx\n\n## What Changes\n\ny\n\n## Skipped phases\n\n- design: small\n- spec, because\n- the design again\n",
		"openspec/changes/c/tasks.md":    "- [x] a\n",
	})
	st := statusOf(t, root, s("c"))
	if st.Next != "verify" || strings.Contains(notes(st), "skipped") {
		t.Errorf("skipped phases: %s %q", st.Next, st.Notes)
	}
}

// The JSON keeps its printed keys and order, which differ
// between the idle state and a selected change.
func TestStatusJSON(t *testing.T) {
	keys := func(st Status) []string {
		var out []string
		for _, m := range st.object() {
			out = append(out, m.key)
		}
		return out
	}
	if got := keys(of(t, "fresh")); !slices.Equal(got, []string{"ok", "change", "directory", "artifacts", "tasks", "next", "notes", "tests", "strict_tdd", "projects", "vcs", "route", "umbrellas", "needs", "folder"}) {
		t.Errorf("selected keys: %q", got)
	}
	idle := statusOf(t, cycle, nil)
	if got := keys(idle); !slices.Equal(got, []string{"ok", "change", "directory", "activeChanges", "rankedChanges", "umbrellas", "artifacts", "vcs", "tasks", "next", "notes", "tests", "strict_tdd", "projects"}) {
		t.Errorf("idle keys: %q", got)
	}
	out, err := js.Stringify(of(t, "fresh"))
	if err != nil || !strings.Contains(out, "\"artifact\": \"exploration.md\",\n      \"state\": \"missing\",\n      \"path\"") || !strings.Contains(out, "\"notes\": []\n") {
		t.Errorf("Stringify: %v\n%s", err, out)
	}
}

// jsonValue is v as encoding/json decodes it, to compare what the CLI prints.
func jsonValue(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	json.Unmarshal(b, &out)
	return out
}

func TestStrictTddResolutionsInStatus(t *testing.T) {
	jhonRoot(t)
	root := strictProject(t, cfg("strict_tdd: false\n"))
	st := statusOf(t, root, s("to-apply"))
	lightVerified(t, filepath.Join(root, "openspec/changes/to-verify/execution-record.json"))
	if st.StrictTdd.Value || st.StrictTdd.Evidence != "n/a" || st.Next != "apply" || statusOf(t, root, s("to-verify")).Next != "archive" {
		t.Errorf("fixture 7, strict_tdd false suggests the same phases: %+v", st)
	}
	if !regexp.MustCompile(`(?s)legacy.*strict_tdd.*/jhon-ldd init`).MatchString(notes(st)) {
		t.Errorf("J3: a legacy strict_tdd is a note: %q", st.Notes)
	}
	for _, n := range st.StrictTdd.Notes {
		if !slices.Contains(st.Notes, n) {
			t.Errorf("J3: the resolution note %q does not reach status notes", n)
		}
	}

	for _, config := range []*string{cfg("strict_tdd: true\n"), cfg("strict_tdd: false\n"), nil} {
		root := strictProject(t, config)
		src, _ := os.ReadFile(filepath.Join(os.Getenv("JHON_ROOT"), "prose/contracts/tdd.md"))
		mode, _ := TddContractMode(string(src))
		want := jsonValue(t, ResolveStrictTdd(config, mode, discover.Discover(root)))
		out, err := js.Stringify(statusOf(t, root, s("to-apply")))
		var printed map[string]any
		json.Unmarshal([]byte(out), &printed)
		if err != nil || !reflect.DeepEqual(printed["strict_tdd"], want) {
			t.Errorf("fixture 8: the printed strict_tdd for %v is %v, want %v", config, printed["strict_tdd"], want)
		}
	}

	required := statusOf(t, strictProject(t, cfg("strict_tdd_policy: require\n")), s("to-apply"))
	if !strings.Contains(notes(required), "strict_tdd is true but") {
		t.Errorf("J2: require without capability is noted: %q", required.Notes)
	}
	derived := statusOf(t, strictProject(t, cfg("schema: spec-driven\n")), s("to-apply"))
	if derived.StrictTdd.Value || derived.StrictTdd.Source != "derived" || derived.Next != "apply" {
		t.Errorf("J2: a derived false suggests apply: %+v", derived.StrictTdd)
	}
}

func TestLegacyStrictTddRegressions(t *testing.T) {
	jhonRoot(t)
	// A prose block scalar that mentions the key, the mode nested under
	// testing:, two python projects that resolve a command, and an unknown one.
	root := tree(t, map[string]string{
		"pyproject.toml":         "[project]\nname = \"root\"\n\n[tool.pytest.ini_options]\n",
		"service/pyproject.toml": "[project]\nname = \"service\"\n\n[tool.pytest.ini_options]\n",
		"tools/pyproject.toml":   "[project]\nname = \"tools\"\n",
		"tools/cli.py":           "print('t')\n",
		"openspec/config.yaml":   "schema: spec-driven\n\ncontext: |\n  Team rule: strict_tdd: true for every change.\n\ntesting:\n  strict_tdd: true\n  test_command: \"pytest\"\n",
	})
	gitT(t, root, "init", "-q")
	gitT(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	copyChange(t, root, "tasks-partial", "to-apply")
	st := statusOf(t, root, s("to-apply"))
	if !st.StrictTdd.Value || st.StrictTdd.Evidence != "invalid" || strings.Contains(st.StrictTdd.Reason, "does not set") || st.Next != "apply" {
		t.Errorf("a legacy nested testing: strict_tdd: true reads as true: %+v", st.StrictTdd)
	}

	// strict_tdd: false written before the project had tests stays false.
	root = tree(t, map[string]string{"openspec/config.yaml": "schema: spec-driven\n\nstrict_tdd: false\n"})
	gitT(t, root, "init", "-q")
	gitT(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	copyChange(t, root, "tasks-partial", "to-apply")
	write(t, filepath.Join(root, "pyproject.toml"), "[project]\nname = \"app\"\n\n[tool.pytest.ini_options]\n")
	st = statusOf(t, root, s("to-apply"))
	if triple(st.StrictTdd) != [3]any{false, "legacy", "n/a"} || !strings.Contains(notes(st), "/jhon-ldd init") {
		t.Errorf("regression J: %+v %q", st.StrictTdd, st.Notes)
	}
}

// M4: an acme-shaped workspace, one member unknown, as status routes it.
func TestAcmeWorkspaceInStatus(t *testing.T) {
	jhonRoot(t)
	member := func(n string) string {
		return "[project]\nname = \"" + n + "\"\n\n[project.optional-dependencies]\ndev = [\"pytest\"]\n\n[tool.pytest.ini_options]\n"
	}
	acme := func(config string) Status {
		root := tree(t, map[string]string{
			"pyproject.toml":        "[project]\nname = \"acme\"\n\n[tool.uv.workspace]\nmembers = [\"svc/*\"]\n",
			"uv.lock":               "version = 1\n",
			"svc/a/pyproject.toml":  member("a"),
			"svc/a/tests/test_a.py": "",
			"svc/b/pyproject.toml":  member("b"),
			"svc/b/tests/test_b.py": "",
			"svc/c/pyproject.toml":  "[project]\nname = \"c\"\n",
			"svc/c/c.py":            "print('c')\n",
			"openspec/config.yaml":  config,
		})
		gitT(t, root, "init", "-q")
		gitT(t, root, "commit", "-q", "--allow-empty", "-m", "init")
		copyChange(t, root, "tasks-partial", "to-apply")
		return statusOf(t, root, s("to-apply"))
	}
	invalid := acme("strict_tdd_policy: require\nstrict_tdd_evidence:\n  svc/a: Makefile:1\n")
	if invalid.StrictTdd.Evidence != "invalid" || !strings.Contains(notes(invalid), "unknown: svc/c") ||
		!strings.Contains(notes(invalid), "legacy strict_tdd_evidence; reconcile into test_command_override with /jhon-ldd init") || invalid.Next != "apply" {
		t.Errorf("one member unknown is invalid under require: %+v %q", invalid.StrictTdd, invalid.Notes)
	}
	ok := acme("strict_tdd_policy: require\ntest_command_override:\n  svc/c: uv run --with pytest pytest\n")
	if !ok.StrictTdd.Value || ok.StrictTdd.Evidence != "ok" {
		t.Errorf("its override makes it ok: %+v", ok.StrictTdd)
	}
	i := slices.IndexFunc(ok.Projects, func(p discover.ResolvedProject) bool { return p.Path == "svc/c" })
	if i < 0 || ok.Projects[i].CommandSource == nil || *ok.Projects[i].CommandSource != "override" {
		t.Errorf("svc/c command source: %+v", ok.Projects)
	}
}

// vcsProject is a root with one project and two changes, in the VCS state setup leaves.
func vcsProject(t *testing.T, setup func(root string)) string {
	t.Helper()
	root := tree(t, map[string]string{"pyproject.toml": "[project]\nname = \"app\"\n\n[tool.pytest.ini_options]\n"})
	copyChange(t, root, "tasks-partial", "to-apply")
	copyChange(t, root, "tasks-partial", "other")
	setup(root)
	return root
}

func TestVcsStatesInStatus(t *testing.T) {
	jhonRoot(t)
	initRepo := func(root string) { gitT(t, root, "init", "-q") }
	commit := func(root string) { initRepo(root); gitT(t, root, "commit", "-q", "--allow-empty", "-m", "init") }
	for _, c := range []struct {
		setup func(string)
		want  string
	}{{func(string) {}, "none"}, {initRepo, "no_head"}} {
		root := vcsProject(t, c.setup)
		st := statusOf(t, root, s("to-apply"))
		if !st.Ok || st.Vcs.State != c.want || st.Next != "apply" || !regexp.MustCompile(`vcs `+c.want+`\b`).MatchString(notes(st)) {
			t.Errorf("%s: noted, and the suggestion is unchanged: %+v", c.want, st)
		}
		os.RemoveAll(filepath.Join(root, "openspec/changes"))
		os.MkdirAll(filepath.Join(root, "openspec/changes"), 0o755)
		if idle := statusOf(t, root, nil); !idle.Ok || idle.Next != "start-change" {
			t.Errorf("%s: with no active change, the idle state: %+v", c.want, idle)
		}
	}
	root := vcsProject(t, commit)
	st := statusOf(t, root, s("to-apply"))
	if st.Vcs.State != "ready" || st.Next != "apply" || regexp.MustCompile(`\bvcs\b`).MatchString(notes(st)) {
		t.Errorf("ready: %+v", st)
	}
	if next := statusOf(t, root, nil).Next; next != "select-change" {
		t.Errorf("ready with two changes and no name: %s", next)
	}
}

func TestLastRunInStatus(t *testing.T) {
	jhonRoot(t)
	root := vcsProject(t, func(root string) {
		gitT(t, root, "init", "-q")
		gitT(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	})
	record := func(codes ...int) {
		var execs []string
		for i, c := range codes {
			execs = append(execs, fmt.Sprintf(`{"project":"p%d","command":"true","cwd":"p%d","exit_code":%d}`, i, i, c))
		}
		write(t, filepath.Join(root, "openspec/changes/to-apply/execution-record.json"), `{"schema":"jhon.execution-record/v1","timestamp":"2026-10-08T10:00:00Z",`+
			`"candidate_commit":"`+strings.Repeat("c", 40)+`","candidate_tree":"`+strings.Repeat("t", 40)+`","working_tree_clean":true,"executions":[`+strings.Join(execs, ",")+`]}`)
	}
	record(0, 0)
	want := execution.LastRun{Timestamp: "2026-10-08T10:00:00Z", Passed: true, Failed: []string{}, Scope: "full", Complete: true,
		Commit: strings.Repeat("c", 40), Tree: strings.Repeat("t", 40), Clean: true}
	if st := statusOf(t, root, s("to-apply")); st.Tests == nil || !reflect.DeepEqual(*st.Tests, want) {
		t.Errorf("a passing run: %+v", st.Tests)
	}
	record(0, 1)
	if st := statusOf(t, root, s("to-apply")); st.Tests == nil || st.Tests.Passed || !slices.Equal(st.Tests.Failed, []string{"p1"}) {
		t.Errorf("a failing run: %+v", st.Tests)
	}
}

func TestTheBranchSelectsOneOfSeveralActiveChanges(t *testing.T) {
	jhonRoot(t)
	root := vcsProject(t, func(root string) {
		gitT(t, root, "init", "-q")
		gitT(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	})
	copyChange(t, root, "tasks-partial", "to-apply-more")
	if st := statusOf(t, root, nil); st.Ok || st.Next != "select-change" {
		t.Errorf("a branch that names none leaves the choice to the caller: %+v", st)
	}
	gitT(t, root, "checkout", "-q", "-b", "feat/to-apply-more")
	st := statusOf(t, root, nil)
	if !st.Ok || st.Change == nil || *st.Change != "to-apply-more" {
		t.Errorf("the branch names the change, the longest of those it contains: %+v", st)
	}
	if got := BranchChange(root, []string{"to-apply", "to-apply-more", "other"}); got != "to-apply-more" {
		t.Errorf("BranchChange: %q", got)
	}
}

func TestABranchNamesItsChangeWithASlashForTheDash(t *testing.T) {
	jhonRoot(t)
	root := vcsProject(t, func(root string) {
		gitT(t, root, "init", "-q")
		gitT(t, root, "add", "-A")
		gitT(t, root, "commit", "-q", "-m", "init")
	})
	copyChange(t, root, "tasks-partial", "fix-tenant-caches")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "plan")
	gitT(t, root, "checkout", "-q", "-b", "fix/tenant-caches")
	st := statusOf(t, root, nil)
	if !st.Ok || st.Change == nil || *st.Change != "fix-tenant-caches" {
		t.Errorf("fix/tenant-caches names fix-tenant-caches: %+v", st)
	}
	if got := BranchChange(root, []string{"to-apply", "fix-tenant-caches", "other"}); got != "fix-tenant-caches" {
		t.Errorf("BranchChange: %q", got)
	}
}

func TestTheOneChangeBeingEditedIsTheSessions(t *testing.T) {
	jhonRoot(t)
	root := vcsProject(t, func(root string) {
		gitT(t, root, "init", "-q")
		gitT(t, root, "add", "-A")
		gitT(t, root, "commit", "-q", "-m", "init")
	})
	// A worktree's branch with no commits of its own, naming no change: the
	// change just planned there, not yet committed, is the one in progress.
	gitT(t, root, "checkout", "-q", "-b", "fix/caches")
	copyChange(t, root, "tasks-partial", "tenant-statement")
	st := statusOf(t, root, nil)
	if !st.Ok || st.Change == nil || *st.Change != "tenant-statement" {
		t.Errorf("the one change with uncommitted files: %+v", st)
	}
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "plan")
	gitT(t, root, "checkout", "-q", "main")
	write(t, filepath.Join(root, "openspec/changes/other/tasks.md"), "## Slice 1: b\n- [x] 1.1 b\n")
	if st := statusOf(t, root, nil); !st.Ok || st.Change == nil || *st.Change != "other" {
		t.Errorf("a committed change edited, on any branch: %+v", st)
	}
	write(t, filepath.Join(root, "openspec/changes/to-apply/tasks.md"), "## Slice 1: a\n- [x] 1.1 a\n")
	if st := statusOf(t, root, nil); st.Ok || st.Next != "select-change" {
		t.Errorf("two changes edited leave the choice to the caller: %+v", st)
	}
}

func TestALaneBranchSelectsTheChangeItsOwnCommitsLastTouched(t *testing.T) {
	jhonRoot(t)
	root := vcsProject(t, func(root string) {
		gitT(t, root, "init", "-q")
		gitT(t, root, "add", "-A")
		gitT(t, root, "commit", "-q", "-m", "init")
	})
	// A lane worktree: its branch names no change, and its commits work on one.
	gitT(t, root, "checkout", "-q", "-b", "chore/lane")
	if st := statusOf(t, root, nil); st.Ok {
		t.Errorf("a branch with no commits of its own selects nothing: %+v", st)
	}
	write(t, filepath.Join(root, "openspec/changes/to-apply/tasks.md"), "## Slice 1: a\n- [ ] 1.1 a\n")
	gitT(t, root, "commit", "-q", "-am", "plan to-apply")
	write(t, filepath.Join(root, "openspec/changes/other/tasks.md"), "## Slice 1: b\n- [ ] 1.1 b\n")
	gitT(t, root, "commit", "-q", "-am", "plan other")
	st := statusOf(t, root, nil)
	if !st.Ok || st.Change == nil || *st.Change != "other" {
		t.Errorf("the change the branch's latest commit touched: %+v", st)
	}
	gitT(t, root, "checkout", "-q", "-")
	if st := statusOf(t, root, nil); st.Ok {
		t.Errorf("the default branch still leaves the choice to the caller: %+v", st)
	}
}

func TestSlicesCarryWhatTheyDeliver(t *testing.T) {
	src := "## Slice 1: launch\nDelivers: an exercise can be launched\n\n- [x] 1.1 a\n\n## Slice 2: panel\n- **Delivers:** the tracking panel\n- [ ] 2.1 b\n\n## Slice 3: none\n- [ ] 3.1 c\n"
	got := ProgressOf(&src).Slices
	want := []string{"an exercise can be launched", "the tracking panel", ""}
	for i, s := range got {
		if s.Delivers != want[i] {
			t.Errorf("slice %d delivers %q, want %q", i+1, s.Delivers, want[i])
		}
	}
	if len(got) != 3 || got[1].Total != 1 {
		t.Errorf("a Delivers line is no task: %+v", got)
	}
}

func TestProgressOfWrappedDelivers(t *testing.T) {
	src := "## Slice 1: copy\n\nDelivers: CA-07 (deadline), CA-06 (a copy restarts every\nexercise, keeping its instructions)\n\n- [x] 1.1 a\n\n## Slice 2: launch\nDelivers: CA-09\n- [ ] 2.1 b\nnot part of it\n"
	got := ProgressOf(&src)
	want := []string{"CA-07 (deadline), CA-06 (a copy restarts every exercise, keeping its instructions)", "CA-09"}
	for i, s := range got.Slices {
		if s.Delivers != want[i] {
			t.Errorf("slice %d delivers %q, want %q", i+1, s.Delivers, want[i])
		}
	}
	if got.Total != 2 {
		t.Errorf("the wrapped paragraph is no task: %+v", got)
	}
}

// T127 (L837): tasks.md outlines every slice from the first plan; only the
// current one holds checkbox tasks. Status counts done over every heading.
func TestSlicesOutline(t *testing.T) {
	tasks := ""
	for i, name := range []string{"domain", "capture", "durations", "blocking", "cross-validation", "export"} {
		tasks += fmt.Sprintf("## Slice %d: %s\nDelivers: CA-0%d\n", i+1, name, i+1)
		switch i {
		case 0, 1:
			tasks += fmt.Sprintf("- [x] %d.1 RED\n- [x] %d.2 GREEN\n", i+1, i+1)
		case 2:
			tasks += checkboxes(14, "")
		}
	}
	p := ProgressOf(s(tasks))
	if p.SlicesDone != 2 || p.Current == nil || *p.Current != "Slice 3: durations" || p.Next == nil || *p.Next != "Slice 4: blocking" {
		t.Errorf("outline progress: done %d current %v next %v", p.SlicesDone, p.Current, p.Next)
	}
	if !p.Slices[3].Outline || p.Slices[2].Outline || p.Slices[0].Outline {
		t.Errorf("outline flags: %+v", p.Slices)
	}
	if line := slicesLine(p); line != "slices      2/6 done, current Slice 3: durations (0/14 checked), next Slice 4: blocking (outline)" {
		t.Errorf("slicesLine = %q", line)
	}
	// An outline slice is not done, and is current once every earlier one is.
	p = ProgressOf(s("## Slice 1: a\nDelivers: x\n- [x] 1.1 RED\n## Slice 2: b\nDelivers: y\n## Slice 3: c\nDelivers: z\n"))
	if p.SlicesDone != 1 || p.Current == nil || *p.Current != "Slice 2: b" || p.Slice != nil {
		t.Errorf("outline current: %+v", p)
	}
	if line := slicesLine(p); line != "slices      1/3 done, current Slice 2: b (outline), next Slice 3: c (outline)" {
		t.Errorf("slicesLine = %q", line)
	}
	// An outline slice before the one with unchecked tasks is still current.
	p = ProgressOf(s("## Slice 1: a\n- [x] 1.1\n## Slice 2: b\n## Slice 3: c\n- [ ] 3.1\n"))
	if p.Current == nil || *p.Current != "Slice 2: b" {
		t.Errorf("first slice not done is current: %+v", p)
	}
	// An old single-slice tasks.md reads as before.
	if line := slicesLine(ProgressOf(s("## Slice 1: a\n- [x] 1.1\n- [ ] 1.2\n"))); line != "slices      0/1 done, current Slice 1: a (1/2 checked)" {
		t.Errorf("single slice = %q", line)
	}
	if line := slicesLine(ProgressOf(s("## Slice 1: a\n- [x] 1.1\n"))); line != "slices      1/1 done" {
		t.Errorf("single slice done = %q", line)
	}
}
