package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The fixtures under review/ load, each defect's lines exist in its
// candidate, every fixture's own tests pass on base+candidate, and there is
// at least one clean fixture and every facet is seeded at least once.
func TestReviewFixtures(t *testing.T) {
	fxs, err := LoadReviewFixtures(filepath.Join(source(), "review"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fxs) < 4 || len(fxs) > 6 {
		t.Fatalf("%d fixtures, want 4-6", len(fxs))
	}
	facets, clean := map[string]bool{}, 0
	for _, fx := range fxs {
		if fx.Clean {
			clean++
		}
		for _, d := range fx.Defects {
			facets[d.Facet] = true
		}
		dir := t.TempDir()
		for _, part := range []string{"base", "candidate"} {
			if err := copyTree(filepath.Join(fx.Dir, part), dir); err != nil {
				t.Fatal(err)
			}
		}
		c := exec.Command("python3", "-m", "unittest", "discover", "-p", "test_*.py")
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Errorf("%s: its tests fail on the candidate: %v\n%s", fx.ID, err, out)
		}
	}
	if clean != 1 {
		t.Errorf("%d clean fixtures, want 1", clean)
	}
	for _, l := range reviewFacets {
		if !facets[l] {
			t.Errorf("no fixture seeds a %s defect", l)
		}
	}
}

func TestParseReviewManifestRefuses(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fx")
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("candidate/a.py", "1\n2\n3\n")
	write("candidate/ldd/changes/c/tasks.md", "x\n")
	good := `{"id":"fx","title":"t","change":"c","defects":[{"id":"D1","file":"a.py","lines":[2,3],"facet":"safety","severity":"CRITICAL","what":"w"}]}`
	fx, err := parseReviewManifest([]byte(good), dir)
	if err != nil {
		t.Fatal(err)
	}
	if fx.Dir != dir || fx.Defects[0].Lines != [2]int{2, 3} {
		t.Errorf("parsed %+v", fx)
	}
	for _, c := range []struct{ body, want string }{
		{strings.Replace(good, `"id":"fx"`, `"id":"other"`, 1), `id "other" is not the directory's name "fx"`},
		{strings.Replace(good, `"change":"c"`, `"change":"C c"`, 1), `change "C c"`},
		{strings.Replace(good, `"change":"c"`, `"change":"d"`, 1), "no candidate/ldd/changes/d/"},
		{strings.Replace(good, `"facet":"safety"`, `"facet":"style"`, 1), `facet "style" is not one of`},
		{strings.Replace(good, `"CRITICAL"`, `"HIGH"`, 1), `severity "HIGH"`},
		{strings.Replace(good, `[2,3]`, `[3,2]`, 1), "are not a range from 1"},
		{strings.Replace(good, `[2,3]`, `[2,9]`, 1), "pass the end of a.py (3 lines)"},
		{strings.Replace(good, `"a.py"`, `"b.py"`, 1), "b.py is not in candidate/"},
		{strings.Replace(good, `"what":"w"`, `"what":""`, 1), "say what the defect is"},
		{strings.Replace(good, `"title":"t"`, `"title":"t","clean":true`, 1), "a clean fixture lists no defect"},
		{`{"id":"fx","title":"t","change":"c","defects":[]}`, "no defect: list them, or mark the fixture clean"},
		{strings.Replace(good, `"title"`, `"tittle"`, 1), "unknown field"},
		{strings.Replace(good, `}]}`, `},{"id":"D1","file":"a.py","lines":[1,1],"facet":"safety","severity":"WARNING","what":"w"}]}`, 1), `defect id "D1" is empty or repeated`},
	} {
		if _, err := parseReviewManifest([]byte(c.body), dir); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.body, err, c.want)
		}
	}
}

// recordSample is review-prism.md as ldd.RecordText writes it.
const recordSample = `# Prism review: late-fees

facets: behavior, safety
verdict: fail
subject: abc123
base: 0f0f

## Correction set

- [behavior-1] CRITICAL billing/fees.py:16-18: except Exception: pass drops a fee (proof: billing/fees.py:17; tests)

## For verify (unconfirmed)

- [behavior-2] CRITICAL ./billing/fees.py:L24: divides by zero: on an empty batch

## Findings (information)

- [safety-1] SUGGESTION billing/fees.py: name the constant

## Refuted

- [safety-2] WARNING billing/fees.py:7: min() is wrong

## Unavailable facets

- clarity: the completion failed
`

func TestParseReviewRecord(t *testing.T) {
	r := parseReviewRecord(recordSample)
	if r.Verdict != "fail" || r.Subject != "abc123" || !slices.Equal(r.Facets, []string{"behavior", "safety"}) {
		t.Errorf("head %+v", r)
	}
	want := []RecordFinding{
		{"behavior-1", "CRITICAL", "billing/fees.py:16-18", "except Exception: pass drops a fee", "correction_set"},
		{"behavior-2", "CRITICAL", "./billing/fees.py:L24", "divides by zero: on an empty batch", "for_verify"},
		{"safety-1", "SUGGESTION", "billing/fees.py", "name the constant", "information"},
		{"safety-2", "WARNING", "billing/fees.py:7", "min() is wrong", "refuted"},
	}
	if !slices.Equal(r.Findings, want) {
		t.Errorf("findings\n got %+v\nwant %+v", r.Findings, want)
	}
	if !slices.Equal(r.Unavailable, []string{"clarity: the completion failed"}) {
		t.Errorf("unavailable %v", r.Unavailable)
	}
}

func TestParseLocation(t *testing.T) {
	for _, c := range []struct {
		in       string
		file     string
		from, to int
		ok       bool
	}{
		{"a/b.py:12", "a/b.py", 12, 12, true},
		{"a/b.py:12-20", "a/b.py", 12, 20, true},
		{"a/b.py:L12-L20", "a/b.py", 12, 20, true},
		{"a/b.py:12:5", "a/b.py", 12, 12, true},
		{"`./a/b.py:20–12`", "a/b.py", 12, 20, true},
		{"a/b.py:12..14", "a/b.py", 12, 14, true},
		{"a/b.py", "a/b.py", 0, 0, false},
		{"a/b.py:send_event", "a/b.py", 0, 0, false},
	} {
		f, a, b, ok := parseLocation(c.in)
		if f != c.file || a != c.from || b != c.to || ok != c.ok {
			t.Errorf("%q: got %q %d-%d %t", c.in, f, a, b, ok)
		}
	}
}

func lateFees() ReviewFixture {
	return ReviewFixture{ID: "late-fees", Change: "late-fees", Defects: []ReviewDefect{
		{ID: "D1", File: "billing/fees.py", Lines: [2]int{16, 18}, Facet: "behavior", Severity: "CRITICAL"},
		{ID: "D2", File: "billing/fees.py", Lines: [2]int{24, 24}, Facet: "behavior", Severity: "WARNING"},
		{ID: "D3", File: "billing/other.py", Lines: [2]int{1, 3}, Facet: "safety", Severity: "WARNING"},
	}}
}

// A finding hits when its file matches and its lines overlap; refuted
// findings count neither way; a line-less or elsewhere finding is a false
// positive; a clean fixture makes every standing finding one.
func TestMatchFindings(t *testing.T) {
	m := matchFindings(lateFees(), parseReviewRecord(recordSample).Findings)
	if !slices.Equal(m.Hits, []string{"D1", "D2"}) || !slices.Equal(m.Misses, []string{"D3"}) || !slices.Equal(m.FalsePositives, []string{"safety-1"}) {
		t.Errorf("match %+v", m)
	}
	if !slices.Equal(m.HitsBy["D2"], []string{"behavior-2"}) {
		t.Errorf("hits by %v", m.HitsBy)
	}
	edge := []RecordFinding{
		{ID: "a", Location: "src/billing/fees.py:18-20", Section: "information"}, // suffix path, overlaps D1's end
		{ID: "b", Location: "billing/fees.py:19-23", Section: "information"},     // between D1 and D2
		{ID: "c", Location: "fees.py:16", Section: "information"},                // not the defect's path
	}
	m = matchFindings(lateFees(), edge)
	if !slices.Equal(m.Hits, []string{"D1"}) || !slices.Equal(m.FalsePositives, []string{"b", "c"}) {
		t.Errorf("edges %+v", m)
	}
	m = matchFindings(ReviewFixture{ID: "clean", Clean: true}, edge)
	if len(m.Hits) != 0 || len(m.Misses) != 0 || len(m.FalsePositives) != 3 {
		t.Errorf("clean %+v", m)
	}
}

func armsWith(nCost, aCost float64, nHits, aHits int) map[string]*ArmTotals {
	return map[string]*ArmTotals{
		"native": {Runs: 5, Hits: nHits, Known: 9, Total: TokenUsage{Cost: nCost, Input: 100, Output: 10, CacheRead: 1000, CacheWrite: 40}},
		"agents": {Runs: 5, Hits: aHits, Known: 9, Total: TokenUsage{Cost: aCost, Input: 300, Output: 50, CacheRead: 9000, CacheWrite: 400}},
	}
}

func TestReviewGate(t *testing.T) {
	g := reviewGate(armsWith(0.6, 1.6, 7, 7))
	if g.Verdict != "PASS" || g.CostMeasure != "cost_usd" || g.NativeCost != 0.6 || g.AgentsCost != 1.6 {
		t.Errorf("pass %+v", g)
	}
	if want := "native costs $0.6000 < agents' $1.6000 and finds 7 ≥ 7 of 9 known defects"; !slices.Equal(g.Reasons, []string{want}) {
		t.Errorf("reasons %q", g.Reasons)
	}
	g = reviewGate(armsWith(1.6, 1.6, 8, 7))
	if g.Verdict != "FAIL" || !slices.Equal(g.Reasons, []string{"native costs $1.6000, not less than agents' $1.6000"}) {
		t.Errorf("cost fail %+v", g)
	}
	g = reviewGate(armsWith(0.5, 1.6, 6, 7))
	if g.Verdict != "FAIL" || !slices.Equal(g.Reasons, []string{"native found 6 known defects, fewer than agents' 7"}) {
		t.Errorf("hits fail %+v", g)
	}
	// No reported cost: the quota-weighted tokens decide.
	g = reviewGate(armsWith(0, 0, 7, 7))
	if g.Verdict != "PASS" || g.CostMeasure != "quota_weighted_tokens" || g.NativeCost != 300 || g.AgentsCost != 1950 {
		t.Errorf("weighted %+v", g)
	}
	arms := armsWith(0.5, 1.6, 7, 7)
	arms["agents"].Runs = 4
	if g = reviewGate(arms); g.Verdict != "FAIL" || !strings.Contains(g.Reasons[0], "the arms ran different sets") {
		t.Errorf("unequal %+v", g)
	}
	if g = reviewGate(map[string]*ArmTotals{"native": {Runs: 1}}); g.Verdict != "FAIL" {
		t.Errorf("one arm %+v", g)
	}
}

// writeLines writes JSONL records.
func writeLines(t *testing.T, path string, recs ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(recs, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

const asst = `{"type":"message","message":{"role":"assistant","usage":{"input":%d,"output":10,"cacheRead":1000,"cacheWrite":50,"totalTokens":0,"cost":{"total":0.01}}}}`

func asstLine(in int) string { return strings.Replace(asst, "%d", itoa(in), 1) }

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// The parent's own messages, the native completions (jhon_review's details)
// and the child sessions by agent type: review-* are reviewers.
func TestCollectReviewRun(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "2026_sess.jsonl")
	writeLines(t, main, `{"type":"session","id":"sess"}`, asstLine(100), asstLine(200),
		`{"type":"message","message":{"role":"toolResult","toolName":"jhon_review","details":{"status":"recorded","reviewers":{"calls":4,"input":40000,"output":3000,"cacheRead":0,"cacheWrite":0,"cost":0.2}}}}`,
		`{"type":"message","message":{"role":"toolResult","toolName":"bash","details":{"status":"x","reviewers":{"calls":9}}}}`, `not json`)
	subs := filepath.Join(dir, "2026_sess", "subagents")
	writeLines(t, filepath.Join(subs, "2026-11-14T00_toolu_1.jsonl"), asstLine(5), asstLine(5))
	writeLines(t, filepath.Join(subs, "toolu_1.meta.json"), `{"agentType":"prism-safety","toolUseId":"toolu_1"}`)
	writeLines(t, filepath.Join(subs, "2026-11-14T01_toolu_2.jsonl"), asstLine(7))
	writeLines(t, filepath.Join(subs, "toolu_2.meta.json"), `{"agentType":"verify"}`)
	repo := t.TempDir()
	writeLines(t, filepath.Join(repo, "ldd", "changes", "late-fees", "review-prism.md"), recordSample)
	var m ReviewRunMetrics
	collectReviewRun(&m, lateFees(), main, repo)
	if m.Parent != (TokenUsage{Calls: 2, Input: 300, Output: 20, CacheRead: 2000, CacheWrite: 100, Cost: 0.02}) {
		t.Errorf("parent %+v", m.Parent)
	}
	if m.Reviewers != (TokenUsage{Calls: 6, Input: 40010, Output: 3020, CacheRead: 2000, CacheWrite: 100, Cost: 0.22}) {
		t.Errorf("reviewers %+v", m.Reviewers)
	}
	if m.Others != (TokenUsage{Calls: 1, Input: 7, Output: 10, CacheRead: 1000, CacheWrite: 50, Cost: 0.01}) {
		t.Errorf("others %+v", m.Others)
	}
	if m.ReviewCalls != 1 || !slices.Equal(m.ReviewStatus, []string{"recorded"}) || m.ReviewAgents["prism-safety"] != 1 || m.OtherAgents["verify"] != 1 {
		t.Errorf("calls %+v", m)
	}
	if m.Record == nil || m.Record.Verdict != "fail" || !slices.Equal(m.Match.Hits, []string{"D1", "D2"}) || len(m.Problems) != 0 {
		t.Errorf("record %+v match %+v problems %v", m.Record, m.Match, m.Problems)
	}
	if got := m.Total().Cost; got != 0.25 {
		t.Errorf("total cost %v", got)
	}
	// No transcript and no record: problems, every defect a miss.
	var empty ReviewRunMetrics
	collectReviewRun(&empty, lateFees(), "", t.TempDir())
	if !slices.Equal(empty.Problems, []string{"no session transcript", "no review-prism.md for late-fees"}) || len(empty.Match.Misses) != 3 {
		t.Errorf("empty %+v", empty)
	}
}

func TestRenderReviewCompare(t *testing.T) {
	fx := lateFees()
	fx.Title = "late fees"
	rec := parseReviewRecord(recordSample)
	runs := []ReviewRunMetrics{
		{Fixture: fx.ID, Arm: "native", Repeat: 1, WallMs: 90000, Parent: TokenUsage{Calls: 4, Input: 1000, Cost: 0.1}, Reviewers: TokenUsage{Calls: 4, Input: 40000, Cost: 0.2},
			Record: &rec, Match: matchFindings(fx, rec.Findings)},
		{Fixture: fx.ID, Arm: "agents", Repeat: 1, WallMs: 300000, Parent: TokenUsage{Calls: 9, Input: 3000, Cost: 0.5}, Reviewers: TokenUsage{Calls: 60, Input: 900000, Cost: 1.5},
			Match: matchFindings(fx, nil), Problems: []string{"no review-prism.md for late-fees"}},
	}
	var out bytes.Buffer
	dir := t.TempDir()
	if err := writeReviewCompare(dir, ReviewCompareReport{Fixtures: []ReviewFixture{fx}, Runs: runs}, &out); err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "report.md"))
	for _, want := range []string{
		"## late-fees — late fees (3 known defects)",
		"- D1 billing/fees.py:16-18 behavior CRITICAL",
		"| native | 1 | fail | behavior, safety | 4 | 1.0k / 0 / 0 / 0 | 4 | 40.0k / 0 / 0 / 0 | 0 | 0.3000 | 41.0k | 90 | D1 D2 | D3 | safety-1 | behavior-1 | behavior-2 |  |",
		"| agents | 1 | none |  | 9 | 3.0k / 0 / 0 / 0 | 60 | 900.0k / 0 / 0 / 0 | 0 | 2.0000 | 903.0k | 300 | — | D1 D2 D3 | — | — | — | no review-prism.md for late-fees |",
		"| native | 1 | 1 | 1.0k / 0 / 0 / 0 | 40.0k / 0 / 0 / 0 | 0 | 41.0k | 0.3000 | 41.0k | 90 | 2 / 3 | 1 (0) |",
		"## Gate (L727): PASS",
		"- native costs $0.3000 < agents' $2.0000 and finds 2 ≥ 0 of 3 known defects",
	} {
		if !strings.Contains(string(md), want) {
			t.Errorf("report.md lacks %q\n%s", want, md)
		}
	}
	if !strings.Contains(out.String(), "## Gate (L727): PASS") {
		t.Errorf("stdout %s", out.String())
	}
	var rep ReviewCompareReport
	b, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	if err := json.Unmarshal(b, &rep); err != nil || rep.Gate.Verdict != "PASS" || rep.Totals["agents"].Reviewers.Calls != 60 {
		t.Errorf("report.json %v %+v", err, rep.Gate)
	}
}

// The repo: base on main with the native LDD config, the candidate one
// commit ahead on change/<change>, which is checked out.
func TestBuildReviewRepo(t *testing.T) {
	fxs, err := LoadReviewFixtures(filepath.Join(source(), "review"), []string{"late-fees"})
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	env := benchEnv(os.Environ(), t.TempDir(), t.TempDir())
	if err := buildReviewRepo(fxs[0], repo, env); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		c := exec.Command("git", args...)
		c.Dir, c.Env = repo, env
		out, err := c.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	if b := git("branch", "--show-current"); b != "change/late-fees" {
		t.Errorf("branch %q", b)
	}
	if d := git("diff", "--name-only", "main"); d != "billing/fees.py\nbilling/test_fees.py\nldd/changes/late-fees/tasks.md" {
		t.Errorf("diff against main:\n%s", d)
	}
	if !fileExists(filepath.Join(repo, "ldd", "config.yaml")) || git("status", "--porcelain") != "" {
		t.Error("no ldd/config.yaml or a dirty tree")
	}
	// The change owes the Prism review (review_required), so the route guard lets the
	// agents arm launch its pertinent facets, the challenger and the arbiter
	// headless, as the native arm runs (L739).
	cfg, err := os.ReadFile(filepath.Join(repo, "ldd", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "\nreview_required:\n  - change: late-fees\n    review: prism\n"; !strings.HasSuffix(string(cfg), want) {
		t.Errorf("ldd/config.yaml does not end with %q:\n%s", want, cfg)
	}
	if !strings.HasPrefix(string(cfg), "# The ldd variant's") {
		t.Errorf("ldd/config.yaml lost the source config:\n%s", cfg)
	}
}

// The plan: per fixture both arms on the same pi model, the agents arm with
// the native review off; the dry run prints it and starts nothing.
func TestReviewCompareDryRun(t *testing.T) {
	f := newFake(t)
	var out, errOut bytes.Buffer
	results := filepath.Join(t.TempDir(), "res")
	code := reviewCompareCLI([]string{"--home", f.home, "--fixtures", "late-fees,slug-clean", "--repeats", "2", "--pi-model", "zai/glm-5.3", "--results", results, "--dry-run"}, &out, &errOut, time.Now())
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	s := out.String()
	for _, want := range []string{
		"plan: 8 runs (2 fixtures × arms native,agents × 2 repeats), pi model zai/glm-5.3, profile default",
		"run 1/8 late-fees-native-pi-jhon-1\n  env: JHON_REVIEW_NATIVE=on",
		"run 2/8 late-fees-agents-pi-jhon-1\n  env: JHON_REVIEW_NATIVE=off",
		"run 8/8 slug-clean-agents-pi-jhon-2",
		"start pi --mode json --provider zai --model glm-5.3 --session-id",
		"(base main + change/product-slugs)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("dry run lacks %q\n%s", want, s)
		}
	}
	if fileExists(results) || f.calls(t) != nil {
		t.Error("a dry run started something")
	}
	for _, c := range [][]string{{"--arms", "native,both"}, {"--fixtures", "nope"}, {"--repeats", "0"}} {
		errOut.Reset()
		if code := reviewCompareCLI(append(c, "--home", f.home, "--dry-run"), &out, &errOut, time.Now()); code != 1 {
			t.Errorf("%v: exit %d %s", c, code, errOut.String())
		}
	}
}

// One fixture end to end on the fakes: each arm's pi gets its switch, the
// session and the record are read, and the report and the gate are written.
func TestReviewCompareFake(t *testing.T) {
	f := newFake(t)
	// pi logs the switch it got, then runs the fake.
	real := filepath.Join(f.dir, "bin", "pi")
	if err := os.Rename(real, real+"-fake"); err != nil {
		t.Fatal(err)
	}
	wrapper := "#!/bin/sh\necho \"JHON_REVIEW_NATIVE=$JHON_REVIEW_NATIVE\" >> \"$FAKE_DIR/switch.log\"\nexec \"" + real + "-fake\" \"$@\"\n"
	if err := os.WriteFile(real, []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, session, asstLine(100),
		`{"type":"message","message":{"role":"toolResult","toolName":"jhon_review","details":{"status":"recorded","reviewers":{"calls":3,"input":30000,"output":2000,"cost":0.1}}}}`)
	t.Setenv("FAKE_TRANSCRIPT", session)
	solution := t.TempDir()
	writeLines(t, filepath.Join(solution, "ldd", "changes", "late-fees", "review-prism.md"), recordSample)
	t.Setenv("FAKE_SOLUTION", solution)
	fxs, err := LoadReviewFixtures(filepath.Join(source(), "review"), []string{"late-fees"})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	results := filepath.Join(t.TempDir(), "res")
	o := &ReviewCompareOptions{Fixtures: fxs, Arms: ReviewArms, Repeats: 1, Model: "opus", Home: f.home, Results: results,
		Timeout: time.Minute, MaxTurns: 10, Out: &out, NewID: ids()}
	if err := ReviewCompare(o); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	sw, _ := os.ReadFile(filepath.Join(f.dir, "switch.log"))
	if string(sw) != "JHON_REVIEW_NATIVE=on\nJHON_REVIEW_NATIVE=off\n" {
		t.Errorf("switches %q", sw)
	}
	var rep ReviewCompareReport
	b, err := os.ReadFile(filepath.Join(results, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Runs) != 2 || rep.Runs[0].Arm != "native" || rep.Runs[0].Reviewers.Calls != 3 || !slices.Equal(rep.Runs[1].Match.Hits, []string{"D1", "D2"}) {
		t.Errorf("runs %+v", rep.Runs)
	}
	// Equal cost and hits on the fakes: native does not cost less.
	if rep.Gate.Verdict != "FAIL" || !fileExists(filepath.Join(results, "report.md")) || !fileExists(filepath.Join(results, "runs", "late-fees-agents-pi-jhon-1", "review-metrics.json")) {
		t.Errorf("gate %+v", rep.Gate)
	}
	// --report re-reads the runs.
	var again bytes.Buffer
	if code := reviewCompareCLI([]string{"--report", results}, &again, &again, time.Now()); code != 0 || !strings.Contains(again.String(), "## Gate (L727): FAIL") {
		t.Errorf("--report exit %d\n%s", code, again.String())
	}
}
