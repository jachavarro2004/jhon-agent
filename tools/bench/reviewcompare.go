package main

// `bench review-compare` (T118e, L727): the native Prism review (`jhon_review`, one
// tool call whose facet, challenger and arbiter completions run through pi's
// model registry) against today's facet agents (prism-<facet>, then
// prism-challenger and prism-arbiter) on the same frozen changes, the same
// pi-jhon condition and the same profile. The agents arm runs with
// JHON_REVIEW_NATIVE=off, which makes the shell's jhon_review answer with its
// fallback (launch the facet agents) without running a completion.
//
// Each fixture under review/<id>/ is a repository: base/ is the default
// branch's one commit (with the native LDD layout's ldd/config.yaml and a
// pyproject.toml added), candidate/ is laid over it and committed on a
// branch, the frozen change, with its record ldd/changes/<change>/. Its
// manifest.json lists the defects seeded in the candidate (file, line range,
// facet, severity); a clean fixture lists none, to count false positives.
//
// A run's measures come from its pi session: the parent's tokens and cost
// (its own assistant messages), each child session's (subagents/, by
// agentType: prism-* are reviewers, review-* before T177), and the native completions' usage,
// which the session holds only in jhon_review's result details.reviewers.
// The change's review-prism.md gives the verdict and the findings; a finding
// hits a known defect when its location names the defect's file and its line
// range overlaps the defect's. Refuted findings count neither way.
//
// The gate: PASS when the native arm's total cost is below the agents arm's
// AND the native arm hits no fewer known defects; FAIL otherwise. Cost is the
// providers' reported dollars when both arms report some, else the bench's
// quota-weighted tokens (input + 1.25 cache write + 0.1 cache read + 5
// output), and the report says which.

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ReviewArms are the two arms, in report order.
var ReviewArms = []string{"native", "agents"}

// reviewFacets and reviewSeverities are the Prism contract's names.
var (
	reviewFacets     = []string{"safety", "failure", "clarity", "behavior"}
	reviewSeverities = []string{"BLOCKER", "CRITICAL", "WARNING", "SUGGESTION"}
)

// ReviewDefect is one defect seeded in a fixture's candidate.
type ReviewDefect struct {
	ID       string `json:"id"`
	File     string `json:"file"`
	Lines    [2]int `json:"lines"`
	Facet    string `json:"facet"`
	Severity string `json:"severity"`
	What     string `json:"what"`
}

// ReviewFixture is one frozen change and its known defects.
type ReviewFixture struct {
	ID      string         `json:"id"`
	Title   string         `json:"title"`
	Change  string         `json:"change"`
	Clean   bool           `json:"clean,omitempty"`
	Defects []ReviewDefect `json:"defects"`
	Dir     string         `json:"-"`
}

var changeName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// parseReviewManifest reads a fixture's manifest.json; dir is the fixture's
// directory, whose name is its id and whose candidate/ holds every defect's
// file and the change's record.
func parseReviewManifest(b []byte, dir string) (ReviewFixture, error) {
	var fx ReviewFixture
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fx); err != nil {
		return fx, fmt.Errorf("%s: %v", filepath.Join(dir, "manifest.json"), err)
	}
	fx.Dir = dir
	bad := func(format string, v ...any) (ReviewFixture, error) {
		return fx, fmt.Errorf("%s: "+format, append([]any{filepath.Join(dir, "manifest.json")}, v...)...)
	}
	switch {
	case fx.ID != filepath.Base(dir):
		return bad("id %q is not the directory's name %q", fx.ID, filepath.Base(dir))
	case !changeName.MatchString(fx.Change):
		return bad("change %q: give the change's name (lower-case, digits, -)", fx.Change)
	case fx.Clean && len(fx.Defects) > 0:
		return bad("a clean fixture lists no defect")
	case !fx.Clean && len(fx.Defects) == 0:
		return bad("no defect: list them, or mark the fixture clean")
	}
	if !fileExists(filepath.Join(dir, "candidate", "ldd", "changes", fx.Change)) {
		return bad("no candidate/ldd/changes/%s/, the change's record", fx.Change)
	}
	seen := map[string]bool{}
	for _, d := range fx.Defects {
		switch {
		case d.ID == "" || seen[d.ID]:
			return bad("defect id %q is empty or repeated", d.ID)
		case !slices.Contains(reviewFacets, d.Facet):
			return bad("defect %s: facet %q is not one of %s", d.ID, d.Facet, strings.Join(reviewFacets, ", "))
		case !slices.Contains(reviewSeverities, d.Severity):
			return bad("defect %s: severity %q is not one of %s", d.ID, d.Severity, strings.Join(reviewSeverities, ", "))
		case d.Lines[0] < 1 || d.Lines[1] < d.Lines[0]:
			return bad("defect %s: lines %v are not a range from 1", d.ID, d.Lines)
		case d.What == "":
			return bad("defect %s: say what the defect is", d.ID)
		}
		seen[d.ID] = true
		b, err := os.ReadFile(filepath.Join(dir, "candidate", filepath.FromSlash(d.File)))
		if err != nil {
			return bad("defect %s: %s is not in candidate/", d.ID, d.File)
		}
		if n := strings.Count(string(b), "\n"); d.Lines[1] > n {
			return bad("defect %s: lines %v pass the end of %s (%d lines)", d.ID, d.Lines, d.File, n)
		}
	}
	return fx, nil
}

// LoadReviewFixtures are the fixtures under root, the named ones (all when
// none is named), in name order.
func LoadReviewFixtures(root string, ids []string) ([]ReviewFixture, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []ReviewFixture
	for _, e := range entries {
		if !e.IsDir() || len(ids) > 0 && !slices.Contains(ids, e.Name()) {
			continue
		}
		dir := filepath.Join(root, e.Name())
		b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			return nil, err
		}
		fx, err := parseReviewManifest(b, dir)
		if err != nil {
			return nil, err
		}
		out = append(out, fx)
	}
	for _, id := range ids {
		if !slices.ContainsFunc(out, func(f ReviewFixture) bool { return f.ID == id }) {
			return nil, fmt.Errorf("no review fixture %q under %s", id, root)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no review fixture under %s", root)
	}
	return out, nil
}

// reviewPrompt is the owner message of both arms: the Prism review of the branch's
// change, nothing else.
func reviewPrompt(fx ReviewFixture) string {
	return fmt.Sprintf("The change `%s` on this branch is complete and committed. Its Prism review is due before delivery. "+
		"Run the Prism review of this change against base `main` now, as the Prism contract says, until review-prism.md is recorded. "+
		"Then stop: do not edit any file, do not fix the findings, do not run verify, do not deliver. "+
		"Reply with the verdict and the findings.", fx.Change)
}

// buildReviewRepo lays fx out in repo: base/ with the native LDD config and
// a pyproject.toml committed on main, then candidate/ committed on the
// branch change/<change>, which stays checked out. The config names the
// change under review_required with review prism, so the Prism review is owed and the
// route guard lets both arms run it headless under the same rule (L739).
func buildReviewRepo(fx ReviewFixture, repo string, env []string) error {
	if err := copyTree(filepath.Join(fx.Dir, "base"), repo); err != nil {
		return err
	}
	for _, f := range [][2]string{{"config.yaml", "ldd/config.yaml"}, {"pyproject.toml", "pyproject.toml"}} {
		b, err := os.ReadFile(filepath.Join(source(), "ldd", f[0]))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, f[1])), 0o755); err != nil {
			return err
		}
		if f[0] == "config.yaml" {
			b = append(b, fmt.Sprintf("\nreview_required:\n  - change: %s\n    review: prism\n", fx.Change)...)
		}
		if err := os.WriteFile(filepath.Join(repo, f[1]), b, 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("__pycache__/\n"), 0o644); err != nil {
		return err
	}
	git := func(args ...string) error {
		c := exec.Command("git", args...)
		c.Dir, c.Env = repo, env
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v\n%s", args, err, out)
		}
		return nil
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "bench"}, {"config", "user.email", "bench@localhost"},
		{"config", "commit.gpgsign", "false"}, {"add", "-A"}, {"commit", "-q", "--no-verify", "-m", "base"}, {"checkout", "-q", "-b", "change/" + fx.Change}} {
		if err := git(args...); err != nil {
			return err
		}
	}
	if err := copyTree(filepath.Join(fx.Dir, "candidate"), repo); err != nil {
		return err
	}
	if err := git("add", "-A"); err != nil {
		return err
	}
	return git("commit", "-q", "--no-verify", "-m", fx.Change)
}

// RecordFinding is one finding line of review-prism.md.
type RecordFinding struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Location string `json:"location"`
	Claim    string `json:"claim"`
	Section  string `json:"section"`
}

// ReviewRecord is what review-prism.md says.
type ReviewRecord struct {
	Facets      []string        `json:"facets"`
	Verdict     string          `json:"verdict"`
	Subject     string          `json:"subject,omitempty"`
	Findings    []RecordFinding `json:"findings"`
	Unavailable []string        `json:"unavailable,omitempty"`
}

// The record's sections (ldd.RecordText) by their heading.
var recordSections = map[string]string{"Correction set": "correction_set", "For verify (unconfirmed)": "for_verify",
	"Follow-ups (pre-existing)": "follow_ups", "Findings (information)": "information", "Refuted": "refuted", "Unavailable facets": "unavailable"}

var (
	findingLine  = regexp.MustCompile(`^- \[([^\]]+)\] (BLOCKER|CRITICAL|WARNING|SUGGESTION) (.*)$`)
	proofSuffix  = regexp.MustCompile(` \(proof: [^()]*\)$`)
	locationHead = regexp.MustCompile(`^(.*?[^\s:]):(.*)$`)
)

// parseReviewRecord reads review-prism.md as ldd.RecordText writes it. The
// location ends at the first ": " that the claim follows.
func parseReviewRecord(text string) ReviewRecord {
	rec := ReviewRecord{Facets: []string{}, Findings: []RecordFinding{}}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if h, ok := strings.CutPrefix(line, "## "); ok {
			section = recordSections[strings.TrimSpace(h)]
			continue
		}
		if section == "" {
			k, v, _ := strings.Cut(line, ": ")
			switch k {
			case "facets":
				rec.Facets = split(v)
			case "verdict":
				rec.Verdict = strings.TrimSpace(v)
			case "subject":
				rec.Subject = strings.TrimSpace(v)
			}
			continue
		}
		if section == "unavailable" {
			if u, ok := strings.CutPrefix(line, "- "); ok {
				rec.Unavailable = append(rec.Unavailable, u)
			}
			continue
		}
		m := findingLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rest := proofSuffix.ReplaceAllString(m[3], "")
		loc, claim, _ := strings.Cut(rest, ": ")
		rec.Findings = append(rec.Findings, RecordFinding{ID: m[1], Severity: m[2], Location: loc, Claim: claim, Section: section})
	}
	return rec
}

var lineRange = regexp.MustCompile(`^\s*L?(\d+)(?:\s*(?:-|–|\.\.)\s*L?(\d+))?`)

// parseLocation is a finding location's file and line range: `file:12`,
// `file:12-20`, `file:L12-L20`, `file:12:5`; ok false without a line.
func parseLocation(loc string) (file string, from, to int, ok bool) {
	loc = strings.Trim(strings.TrimSpace(loc), "`")
	m := locationHead.FindStringSubmatch(loc)
	if m == nil {
		return strings.TrimPrefix(loc, "./"), 0, 0, false
	}
	file = strings.TrimPrefix(strings.Trim(m[1], "`"), "./")
	r := lineRange.FindStringSubmatch(m[2])
	if r == nil {
		return file, 0, 0, false
	}
	from, _ = strconv.Atoi(r[1])
	to = from
	if r[2] != "" {
		to, _ = strconv.Atoi(r[2])
	}
	if to < from {
		from, to = to, from
	}
	return file, from, to, true
}

// sameFile is whether a finding's file names the repository path want.
func sameFile(got, want string) bool {
	return got == want || strings.HasSuffix(got, "/"+want)
}

// ReviewMatch is how a run's findings meet a fixture's known defects.
type ReviewMatch struct {
	Hits           []string `json:"hits"`
	Misses         []string `json:"misses"`
	FalsePositives []string `json:"false_positives"`
	// HitsBy is the finding ids that hit each defect.
	HitsBy map[string][]string `json:"hits_by,omitempty"`
}

// matchFindings matches findings to fx's defects: a finding hits a defect
// when its file is the defect's and its lines overlap the defect's; a
// standing finding (not refuted) that hits none is a false positive.
func matchFindings(fx ReviewFixture, findings []RecordFinding) ReviewMatch {
	m := ReviewMatch{Hits: []string{}, Misses: []string{}, FalsePositives: []string{}, HitsBy: map[string][]string{}}
	for _, f := range findings {
		if f.Section == "refuted" {
			continue
		}
		file, from, to, ok := parseLocation(f.Location)
		hit := false
		for _, d := range fx.Defects {
			if ok && sameFile(file, d.File) && from <= d.Lines[1] && to >= d.Lines[0] {
				m.HitsBy[d.ID] = append(m.HitsBy[d.ID], f.ID)
				hit = true
			}
		}
		if !hit {
			m.FalsePositives = append(m.FalsePositives, f.ID)
		}
	}
	for _, d := range fx.Defects {
		if len(m.HitsBy[d.ID]) > 0 {
			m.Hits = append(m.Hits, d.ID)
		} else {
			m.Misses = append(m.Misses, d.ID)
		}
	}
	return m
}

// TokenUsage is what a set of model calls spent.
type TokenUsage struct {
	Calls      int     `json:"calls"`
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Cost       float64 `json:"cost"`
}

func (u *TokenUsage) add(v TokenUsage) {
	u.Calls += v.Calls
	u.Input += v.Input
	u.Output += v.Output
	u.CacheRead += v.CacheRead
	u.CacheWrite += v.CacheWrite
	u.Cost = math.Round((u.Cost+v.Cost)*1e9) / 1e9
}

// Tokens is every token counted.
func (u TokenUsage) Tokens() float64 { return u.Input + u.Output + u.CacheRead + u.CacheWrite }

// Weighted is the bench's quota-weighted tokens (L432).
func (u TokenUsage) Weighted() float64 {
	return u.Input + 1.25*u.CacheWrite + 0.1*u.CacheRead + 5*u.Output
}

// piSessionFacts are what one pi session file says.
type piSessionFacts struct {
	Usage TokenUsage
	// Native is the usage jhon_review's results report (details.reviewers);
	// ReviewCalls and ReviewStatus are its calls and their statuses.
	Native       TokenUsage
	ReviewCalls  int
	ReviewStatus []string
}

// readPiSessionFacts reads a pi session: each assistant message's usage,
// and each jhon_review result's details.
func readPiSessionFacts(path string) (piSessionFacts, error) {
	var f piSessionFacts
	file, err := os.Open(path)
	if err != nil {
		return f, err
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var e struct {
			Type    string `json:"type"`
			Message struct {
				Role     string `json:"role"`
				ToolName string `json:"toolName"`
				Usage    *struct {
					Input, Output, CacheRead, CacheWrite float64
					Cost                                 struct{ Total float64 }
				} `json:"usage"`
				Details struct {
					Status string `json:"status"`
					Raw    *struct {
						Calls                                int
						Input, Output, CacheRead, CacheWrite float64
						Cost                                 float64
					} `json:"reviewers"`
				} `json:"details"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Type != "message" {
			continue
		}
		switch msg := e.Message; {
		case msg.Role == "assistant" && msg.Usage != nil:
			u := msg.Usage
			f.Usage.add(TokenUsage{Calls: 1, Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, Cost: u.Cost.Total})
		case msg.Role == "toolResult" && msg.ToolName == "jhon_review":
			f.ReviewCalls++
			f.ReviewStatus = append(f.ReviewStatus, cmpOr(msg.Details.Status, "error"))
			if r := msg.Details.Raw; r != nil {
				f.Native.add(TokenUsage{Calls: r.Calls, Input: r.Input, Output: r.Output, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite, Cost: r.Cost})
			}
		}
	}
	return f, sc.Err()
}

// ReviewRunMetrics are one run's numbers, its review-metrics.json.
type ReviewRunMetrics struct {
	Fixture   string `json:"fixture"`
	Arm       string `json:"arm"`
	Repeat    int    `json:"repeat"`
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
	Profile   string `json:"profile,omitempty"`
	Exit      int    `json:"exit"`
	TimedOut  bool   `json:"timed_out"`
	WallMs    int64  `json:"wall_ms"`
	// Parent is the main session's own messages; Reviewers the review
	// completions (native) or the prism-* child sessions (agents);
	// Others every other child session (jhon-scout, jhon-checker, jhon-builder).
	Parent    TokenUsage `json:"parent"`
	Reviewers TokenUsage `json:"reviewers"`
	Others    TokenUsage `json:"others"`
	// ReviewAgents are the prism-* child sessions by agent type.
	ReviewAgents map[string]int `json:"review_agents"`
	OtherAgents  map[string]int `json:"other_agents,omitempty"`
	ReviewCalls  int            `json:"jhon_review_calls"`
	ReviewStatus []string       `json:"jhon_review_status,omitempty"`
	Record       *ReviewRecord  `json:"record"`
	Match        ReviewMatch    `json:"match"`
	Problems     []string       `json:"problems,omitempty"`
}

// Total is everything the run spent.
func (m ReviewRunMetrics) Total() TokenUsage {
	var t TokenUsage
	t.add(m.Parent)
	t.add(m.Reviewers)
	t.add(m.Others)
	return t
}

// collectReviewRun fills m from the run's session (transcript) and the
// change's record in repo.
func collectReviewRun(m *ReviewRunMetrics, fx ReviewFixture, transcript, repo string) {
	m.ReviewAgents, m.Match = map[string]int{}, matchFindings(fx, nil)
	if transcript == "" {
		m.Problems = append(m.Problems, "no session transcript")
	} else if f, err := readPiSessionFacts(transcript); err != nil {
		m.Problems = append(m.Problems, "session: "+err.Error())
	} else {
		m.Parent, m.ReviewCalls, m.ReviewStatus = f.Usage, f.ReviewCalls, f.ReviewStatus
		m.Reviewers.add(f.Native)
		children, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents", "*.jsonl"))
		slices.Sort(children)
		for _, c := range children {
			agent := "unknown"
			if _, id, ok := strings.Cut(strings.TrimSuffix(filepath.Base(c), ".jsonl"), "_"); ok {
				var meta struct{ AgentType string }
				if b, err := os.ReadFile(filepath.Join(filepath.Dir(c), id+".meta.json")); err == nil && json.Unmarshal(b, &meta) == nil && meta.AgentType != "" {
					agent = meta.AgentType
				}
			}
			cf, err := readPiSessionFacts(c)
			if err != nil {
				m.Problems = append(m.Problems, "child session "+filepath.Base(c)+": "+err.Error())
				continue
			}
			if strings.HasPrefix(agent, "prism-") || strings.HasPrefix(agent, "review-") { // review-* is a run recorded before T177
				m.Reviewers.add(cf.Usage)
				m.ReviewAgents[agent]++
			} else {
				m.Others.add(cf.Usage)
				if m.OtherAgents == nil {
					m.OtherAgents = map[string]int{}
				}
				m.OtherAgents[agent]++
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(repo, "ldd", "changes", fx.Change, "review-prism.md"))
	if err != nil {
		m.Problems = append(m.Problems, "no review-prism.md for "+fx.Change)
		return
	}
	rec := parseReviewRecord(string(b))
	m.Record, m.Match = &rec, matchFindings(fx, rec.Findings)
}

// ArmTotals are one arm's sums over its runs.
type ArmTotals struct {
	Runs           int        `json:"runs"`
	Recorded       int        `json:"recorded"`
	Parent         TokenUsage `json:"parent"`
	Reviewers      TokenUsage `json:"reviewers"`
	Others         TokenUsage `json:"others"`
	Total          TokenUsage `json:"total"`
	WallMs         int64      `json:"wall_ms"`
	Hits           int        `json:"hits"`
	Known          int        `json:"known"`
	FalsePositives int        `json:"false_positives"`
	// CleanFalsePositives are the false positives on clean fixtures.
	CleanFalsePositives int `json:"clean_false_positives"`
}

// ReviewGate is the install gate's verdict (L727).
type ReviewGate struct {
	Verdict     string   `json:"verdict"`
	CostMeasure string   `json:"cost_measure"`
	NativeCost  float64  `json:"native_cost"`
	AgentsCost  float64  `json:"agents_cost"`
	NativeHits  int      `json:"native_hits"`
	AgentsHits  int      `json:"agents_hits"`
	Known       int      `json:"known"`
	Reasons     []string `json:"reasons"`
}

// ReviewCompareReport is the batch's report.json.
type ReviewCompareReport struct {
	Fixtures []ReviewFixture       `json:"fixtures"`
	Runs     []ReviewRunMetrics    `json:"runs"`
	Totals   map[string]*ArmTotals `json:"totals"`
	Gate     ReviewGate            `json:"gate"`
}

// armTotals sums runs by arm.
func armTotals(fixtures []ReviewFixture, runs []ReviewRunMetrics) map[string]*ArmTotals {
	clean := map[string]bool{}
	for _, f := range fixtures {
		clean[f.ID] = f.Clean
	}
	out := map[string]*ArmTotals{}
	for _, a := range ReviewArms {
		out[a] = &ArmTotals{}
	}
	for _, r := range runs {
		t := out[r.Arm]
		if t == nil {
			t = &ArmTotals{}
			out[r.Arm] = t
		}
		t.Runs++
		if r.Record != nil {
			t.Recorded++
		}
		t.Parent.add(r.Parent)
		t.Reviewers.add(r.Reviewers)
		t.Others.add(r.Others)
		t.Total.add(r.Total())
		t.WallMs += r.WallMs
		t.Hits += len(r.Match.Hits)
		t.Known += len(r.Match.Hits) + len(r.Match.Misses)
		t.FalsePositives += len(r.Match.FalsePositives)
		if clean[r.Fixture] {
			t.CleanFalsePositives += len(r.Match.FalsePositives)
		}
	}
	return out
}

// reviewGate judges the totals: PASS when native costs less and hits no
// fewer known defects. Dollars when both arms report a cost, else the
// quota-weighted tokens. Unequal run sets cannot be judged.
func reviewGate(totals map[string]*ArmTotals) ReviewGate {
	n, a := totals["native"], totals["agents"]
	g := ReviewGate{Verdict: "FAIL"}
	if n == nil || a == nil || n.Runs == 0 || a.Runs == 0 {
		g.Reasons = []string{"both arms need runs"}
		return g
	}
	g.CostMeasure, g.NativeCost, g.AgentsCost = "cost_usd", n.Total.Cost, a.Total.Cost
	if n.Total.Cost <= 0 || a.Total.Cost <= 0 {
		g.CostMeasure, g.NativeCost, g.AgentsCost = "quota_weighted_tokens", math.Round(n.Total.Weighted()), math.Round(a.Total.Weighted())
	}
	g.NativeHits, g.AgentsHits, g.Known = n.Hits, a.Hits, n.Known
	if n.Runs != a.Runs || n.Known != a.Known {
		g.Reasons = append(g.Reasons, fmt.Sprintf("the arms ran different sets (native %d runs, %d known defects; agents %d runs, %d)", n.Runs, n.Known, a.Runs, a.Known))
	}
	if g.NativeCost >= g.AgentsCost {
		g.Reasons = append(g.Reasons, fmt.Sprintf("native costs %s, not less than agents' %s", fmtCost(g.CostMeasure, g.NativeCost), fmtCost(g.CostMeasure, g.AgentsCost)))
	}
	if n.Hits < a.Hits {
		g.Reasons = append(g.Reasons, fmt.Sprintf("native found %d known defects, fewer than agents' %d", n.Hits, a.Hits))
	}
	if len(g.Reasons) == 0 {
		g.Verdict = "PASS"
		g.Reasons = []string{fmt.Sprintf("native costs %s < agents' %s and finds %d ≥ %d of %d known defects",
			fmtCost(g.CostMeasure, g.NativeCost), fmtCost(g.CostMeasure, g.AgentsCost), n.Hits, a.Hits, n.Known)}
	}
	return g
}

func fmtCost(measure string, v float64) string {
	if measure == "cost_usd" {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("%s weighted tokens", fmtTokens(v))
}

// fmtTokens is n in k or M.
func fmtTokens(n float64) string {
	switch {
	case n >= 1e6:
		return fmt.Sprintf("%.2fM", n/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fk", n/1e3)
	}
	return strconv.FormatFloat(n, 'f', 0, 64)
}

// renderReviewCompare writes the report: a table per fixture (a row per
// run), the pooled totals per arm, and the gate.
func renderReviewCompare(w io.Writer, rep ReviewCompareReport) {
	fmt.Fprintln(w, "# bench review-compare (T118e): native Prism review vs facet agents")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Tokens are input / output / cache read / cache write; cost is the providers' reported dollars (0 when none reports); weighted = input + 1.25 cache write + 0.1 cache read + 5 output.")
	row := func(u TokenUsage) string {
		return fmt.Sprintf("%s / %s / %s / %s", fmtTokens(u.Input), fmtTokens(u.Output), fmtTokens(u.CacheRead), fmtTokens(u.CacheWrite))
	}
	for _, fx := range rep.Fixtures {
		kind := fmt.Sprintf("%d known defects", len(fx.Defects))
		if fx.Clean {
			kind = "clean: every standing finding is a false positive"
		}
		fmt.Fprintf(w, "\n## %s — %s (%s)\n\n", fx.ID, fx.Title, kind)
		for _, d := range fx.Defects {
			fmt.Fprintf(w, "- %s %s:%d-%d %s %s — %s\n", d.ID, d.File, d.Lines[0], d.Lines[1], d.Facet, d.Severity, d.What)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "| arm | rep | verdict | facets | parent calls | parent tokens | reviewer runs | reviewer tokens | other agents | cost $ | weighted | wall s | hits | misses | false positives | correction set | for verify | problems |")
		fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|")
		for _, r := range rep.Runs {
			if r.Fixture != fx.ID {
				continue
			}
			verdict, facets, cs, fv := "none", "", []string{}, []string{}
			if r.Record != nil {
				verdict, facets = r.Record.Verdict, strings.Join(r.Record.Facets, ", ")
				for _, f := range r.Record.Findings {
					switch f.Section {
					case "correction_set":
						cs = append(cs, f.ID)
					case "for_verify":
						fv = append(fv, f.ID)
					}
				}
			}
			t := r.Total()
			fmt.Fprintf(w, "| %s | %d | %s | %s | %d | %s | %d | %s | %s | %.4f | %s | %.0f | %s | %s | %s | %s | %s | %s |\n",
				r.Arm, r.Repeat, verdict, facets, r.Parent.Calls, row(r.Parent), r.Reviewers.Calls, row(r.Reviewers), fmtTokens(r.Others.Tokens()),
				t.Cost, fmtTokens(t.Weighted()), float64(r.WallMs)/1000, orNone(r.Match.Hits), orNone(r.Match.Misses), orNone(r.Match.FalsePositives),
				orNone(cs), orNone(fv), strings.ReplaceAll(strings.Join(r.Problems, "; "), "|", "/"))
		}
	}
	fmt.Fprintln(w, "\n## Pooled totals")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| arm | runs | recorded | parent tokens | reviewer tokens | other agents | total tokens | cost $ | weighted | wall s | hits / known | false positives (on clean) |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|---|---|")
	for _, a := range ReviewArms {
		t := rep.Totals[a]
		if t == nil {
			continue
		}
		fmt.Fprintf(w, "| %s | %d | %d | %s | %s | %s | %s | %.4f | %s | %.0f | %d / %d | %d (%d) |\n", a, t.Runs, t.Recorded, row(t.Parent), row(t.Reviewers),
			fmtTokens(t.Others.Tokens()), fmtTokens(t.Total.Tokens()), t.Total.Cost, fmtTokens(t.Total.Weighted()), float64(t.WallMs)/1000,
			t.Hits, t.Known, t.FalsePositives, t.CleanFalsePositives)
	}
	g := rep.Gate
	fmt.Fprintf(w, "\n## Gate (L727): %s\n\n", g.Verdict)
	fmt.Fprintf(w, "PASS when the native total cost < the agents' AND native hits ≥ agents' hits on known defects. Cost measure: %s.\n\n", g.CostMeasure)
	for _, r := range g.Reasons {
		fmt.Fprintf(w, "- %s\n", r)
	}
}

func orNone(xs []string) string {
	if len(xs) == 0 {
		return "—"
	}
	return strings.Join(xs, " ")
}

// ReviewCompareOptions are one `bench review-compare`.
type ReviewCompareOptions struct {
	Fixtures []ReviewFixture
	Arms     []string
	Repeats  int
	Model    string // the Claude model on claude-bridge when no pi model
	PiModel  string
	PiEnv    []string
	Home     string
	Results  string
	Timeout  time.Duration
	MaxTurns int
	DryRun   bool
	Out      io.Writer
	NewID    func() string
}

// reviewArmEnv is the arm's switch: the agents arm turns the native review
// off; the native arm sets it on, whatever the parent environment says.
func reviewArmEnv(arm string) []string {
	if arm == "agents" {
		return []string{"JHON_REVIEW_NATIVE=off"}
	}
	return []string{"JHON_REVIEW_NATIVE=on"}
}

// planReviewCompare is every run, repeat-major and arms interleaved per
// fixture, so a stopped batch stays balanced.
func planReviewCompare(o *ReviewCompareOptions) ([]*Run, []ReviewFixture, error) {
	if o.Repeats < 1 {
		return nil, nil, fmt.Errorf("--repeats must be 1 or more")
	}
	for _, a := range o.Arms {
		if !slices.Contains(ReviewArms, a) {
			return nil, nil, fmt.Errorf("unknown arm %q (known: %s)", a, strings.Join(ReviewArms, ", "))
		}
	}
	if o.PiModel != "" {
		if err := parsePiModel(o.PiModel); err != nil {
			return nil, nil, err
		}
	}
	model := o.PiModel
	if model == "" && activeProfile(o.Home) != "" {
		main, err := profileMainModel(o.Home)
		if err != nil {
			return nil, nil, err
		}
		model = main
	}
	if model == "" {
		model = bridgeModel(o.Model)
	}
	newID := o.NewID
	if newID == nil {
		newID = uuid4
	}
	opts := &Options{Home: o.Home}
	var runs []*Run
	var fxOf []ReviewFixture
	for rep := 1; rep <= o.Repeats; rep++ {
		for _, fx := range o.Fixtures {
			for _, arm := range o.Arms {
				p := reviewPrompt(fx)
				r := &Run{Task: Task{ID: fx.ID, Prompt: p, Prompts: []string{p}, MaxTurns: o.MaxTurns, Timeout: o.Timeout},
					Variant: arm, Cond: piJhon{}, Repeat: rep, Model: model, SessionID: newID(), Timeout: o.Timeout,
					Env: reviewArmEnv(arm), PassEnv: o.PiEnv, Profile: activeProfile(o.Home)}
				r.Dir = filepath.Join(o.Results, "runs", r.Name())
				r.Repo, r.Cache = filepath.Join(r.Dir, "repo"), filepath.Join(r.Dir, "cache")
				r.Argv = r.Cond.Argv(r, opts)
				runs = append(runs, r)
				fxOf = append(fxOf, fx)
			}
		}
	}
	for i, r := range runs {
		r.N, r.Of = i+1, len(runs)
	}
	return runs, fxOf, nil
}

// ReviewCompare runs the batch (or prints its plan) and writes the report.
func ReviewCompare(o *ReviewCompareOptions) error {
	runs, fxOf, err := planReviewCompare(o)
	if err != nil {
		return err
	}
	if o.DryRun {
		fmt.Fprintf(o.Out, "plan: %d runs (%d fixtures × arms %s × %d repeats), pi model %s, profile %s, timeout %s, max turns %d, results %s\n",
			len(runs), len(o.Fixtures), strings.Join(o.Arms, ","), o.Repeats, runs[0].Model, cmpOr(runs[0].Profile, "default"), o.Timeout, o.MaxTurns, o.Results)
		for _, r := range runs {
			fmt.Fprintf(o.Out, "\nrun %d/%d %s\n  env: %s\n  cwd: %s (base main + change/%s)\n  $ %s\n", r.N, r.Of, r.Name(), strings.Join(r.Env, " "), r.Repo, fxOf[r.N-1].Change, commandLine(r.Argv))
		}
		return nil
	}
	for _, n := range o.PiEnv {
		if _, ok := os.LookupEnv(n); !ok {
			return fmt.Errorf("--pi-env %s is not set in the environment", n)
		}
	}
	rep := ReviewCompareReport{Fixtures: o.Fixtures}
	for i, r := range runs {
		fx := fxOf[i]
		fmt.Fprintf(o.Out, "run %d/%d %s ...\n", r.N, r.Of, r.Name())
		if err := os.MkdirAll(r.Cache, 0o755); err != nil {
			return err
		}
		env := benchEnv(os.Environ(), o.Home, r.Cache)
		if err := buildReviewRepo(fx, r.Repo, env); err != nil {
			return err
		}
		res, err := startAgent(r, append(append(slices.Clone(env), r.Env...), passEnv(r.PassEnv)...), nil)
		if err != nil {
			return err
		}
		m := ReviewRunMetrics{Fixture: fx.ID, Arm: r.Variant, Repeat: r.Repeat, SessionID: r.SessionID, Model: r.Model, Profile: r.Profile,
			Exit: res.exit, TimedOut: res.timedOut, WallMs: res.wall.Milliseconds()}
		transcript, terr := r.Cond.Transcript(r, &Options{Home: o.Home})
		if terr != nil {
			m.Problems = append(m.Problems, terr.Error())
		}
		collectReviewRun(&m, fx, transcript, r.Repo)
		if m.TimedOut {
			m.Problems = append(m.Problems, "timed out")
		}
		if err := writeJSON(filepath.Join(r.Dir, "review-metrics.json"), m); err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "  verdict=%s hits=%d/%d false_positives=%d tokens=%s wall=%s%s\n", recordVerdict(m.Record), len(m.Match.Hits),
			len(m.Match.Hits)+len(m.Match.Misses), len(m.Match.FalsePositives), fmtTokens(m.Total().Tokens()), res.wall.Round(time.Second), problemsOf(m.Problems))
		rep.Runs = append(rep.Runs, m)
	}
	return writeReviewCompare(o.Results, rep, o.Out)
}

func recordVerdict(r *ReviewRecord) string {
	if r == nil {
		return "none"
	}
	return r.Verdict
}

func problemsOf(p []string) string {
	if len(p) == 0 {
		return ""
	}
	return " problems: " + strings.Join(p, "; ")
}

// writeReviewCompare totals rep, judges the gate and writes report.json and
// report.md to dir, the report also to out.
func writeReviewCompare(dir string, rep ReviewCompareReport, out io.Writer) error {
	rep.Totals = armTotals(rep.Fixtures, rep.Runs)
	rep.Gate = reviewGate(rep.Totals)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "report.json"), rep); err != nil {
		return err
	}
	var b strings.Builder
	renderReviewCompare(&b, rep)
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Fprint(out, "\n"+b.String())
	fmt.Fprintf(out, "\nreport in %s\n", filepath.Join(dir, "report.md"))
	return nil
}

// reReportReviewCompare re-reads a batch's runs and writes its report again.
func reReportReviewCompare(dir, fixturesRoot string, out io.Writer) error {
	files, _ := filepath.Glob(filepath.Join(dir, "runs", "*", "review-metrics.json"))
	if len(files) == 0 {
		return fmt.Errorf("no runs/*/review-metrics.json under %s", dir)
	}
	slices.Sort(files)
	rep := ReviewCompareReport{}
	ids := []string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var m ReviewRunMetrics
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("%s: %v", f, err)
		}
		rep.Runs = append(rep.Runs, m)
		if !slices.Contains(ids, m.Fixture) {
			ids = append(ids, m.Fixture)
		}
	}
	fx, err := LoadReviewFixtures(fixturesRoot, ids)
	if err != nil {
		return err
	}
	rep.Fixtures = fx
	return writeReviewCompare(dir, rep, out)
}

const reviewCompareUsage = `usage: bench review-compare [--fixtures a,b] [--arms native,agents] [--repeats N] [--model M] [--pi-model PROVIDER/MODEL[@THINKING]]
                            [--pi-env NAMES] [--home DIR] [--results DIR] [--timeout D] [--max-turns N] [--dry-run]
       bench review-compare --report <results dir>
`

func reviewCompareCLI(args []string, out, errOut io.Writer, now time.Time) int {
	fs := flag.NewFlagSet("bench review-compare", flag.ContinueOnError)
	fs.SetOutput(errOut)
	home := fs.String("home", filepath.Join(root(), "home"), "the bench HOME")
	fixtures := fs.String("fixtures", "", "comma-separated fixture ids under tools/bench/review (default: all)")
	fixturesRoot := fs.String("fixtures-dir", filepath.Join(source(), "review"), "where the fixtures are")
	arms := fs.String("arms", strings.Join(ReviewArms, ","), "comma-separated arms: native (jhon_review), agents (JHON_REVIEW_NATIVE=off: the facet agents)")
	repeats := fs.Int("repeats", 1, "runs per fixture and arm")
	model := fs.String("model", "opus", "the claude-bridge model of the pi main session when the bench HOME has no profile and no --pi-model")
	piModel := fs.String("pi-model", "", "the pi main session's provider/model[@thinking] (default: the profile's orchestrator model, else claude-bridge on --model)")
	piEnv := fs.String("pi-env", "", "comma-separated environment variable names the pi runs need (values never written)")
	results := fs.String("results", "", "this batch's results directory (default: under ~/.local/share/jhon-bench/results, <time>-review-compare)")
	timeout := fs.Duration("timeout", 20*time.Minute, "each run's timeout")
	maxTurns := fs.Int("max-turns", 40, "each run's turn cap")
	dry := fs.Bool("dry-run", false, "print the plan and start nothing")
	report := fs.String("report", "", "write the report again from a batch's runs")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		fmt.Fprint(errOut, reviewCompareUsage)
		return 2
	}
	fail := func(err error) int { fmt.Fprintf(errOut, "bench review-compare: %v\n", err); return 1 }
	if *report != "" {
		if err := reReportReviewCompare(*report, *fixturesRoot, out); err != nil {
			return fail(err)
		}
		return 0
	}
	fx, err := LoadReviewFixtures(*fixturesRoot, split(*fixtures))
	if err != nil {
		return fail(err)
	}
	if *results == "" {
		*results = filepath.Join(root(), "results", resultsName(now, *piModel, "review-compare"))
	}
	abs, _ := filepath.Abs(*home)
	o := &ReviewCompareOptions{Fixtures: fx, Arms: split(*arms), Repeats: *repeats, Model: *model, PiModel: *piModel, PiEnv: split(*piEnv),
		Home: abs, Results: *results, Timeout: *timeout, MaxTurns: *maxTurns, DryRun: *dry, Out: out}
	if !*dry {
		if _, err := os.Stat(benchJhon(abs)); err != nil {
			return fail(fmt.Errorf("no jhon in the bench HOME %s: run `bench prepare` first", abs))
		}
		if _, err := os.Stat(*results); err == nil {
			return fail(fmt.Errorf("%s already exists", *results))
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
	}
	if err := ReviewCompare(o); err != nil {
		return fail(err)
	}
	return 0
}
