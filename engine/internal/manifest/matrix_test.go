package manifest

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// matrixSrc is a two-runtime, two-primitive matrix in agents.yaml's shape.
const matrixSrc = `version: 1
runtimes: [alpha, beta]
primitives: [hooks, transcripts]
platforms: [linux, windows]
runtime_primitives:
  alpha:
    hooks: { capability: true, verification: verified, version: "1.0", source: "test/testdata/spikes/alpha-hooks/record.json, a spike" }
    transcripts: { capability: true, verification: verified, version: "1.0", source: "test/testdata/audit/real-one, a real session" }
  beta:
    hooks: { capability: false, verification: unknown, version: "2.0", source: "no evidence" }   # a comment
    transcripts: { capability: false, verification: documented, version: "2.0", source: "vendor docs" }
contract_delivery:
  alpha: { max_instruction_bytes: none, imports: true, source: "vendor docs" }
  beta: { max_instruction_bytes: 100, imports: false, source: "vendor docs" }
observation_route:
  alpha: jhon audit
  beta: none
guarantees:
  rule/one: { by: hooks, alpha: enforced, beta: unsupported }
  rule/two: { alpha: behavioural, beta: unsupported, alpha@windows: unsupported }
`

var levels = []string{"enforced", "behavioural", "unsupported"}

func projections(m map[string][]string) Map[[]string] {
	var out Map[[]string]
	for _, k := range []string{"alpha", "beta"} {
		if v, ok := m[k]; ok {
			out.Set(k, v)
		}
	}
	return out
}

func matrixCtx() MatrixContext {
	evidence := map[string]Evidence{
		"test/testdata/spikes/alpha-hooks/record.json": {Kind: "spike"},
		"test/testdata/audit/real-one":                 {Kind: "real-session"},
		"test/testdata/spikes/broken/record.json":      {Invalid: "lacks no_effect_check"},
	}
	return MatrixContext{
		Levels: levels, Verifications: []string{"verified", "documented", "unknown"},
		Anchors:     []string{"rule/one", "rule/two"},
		AuditRules:  []map[string]string{{"anchor": "rule/one", "check": "none", "reason": "a guard holds it"}, {"anchor": "rule/two", "check": "c2"}},
		Projections: projections(map[string][]string{"alpha": {"hooks"}}),
		Exists: func(p string) bool {
			return slices.Contains([]string{"test/runtimes.test.ts", "test/testdata/spikes/alpha-hooks/record.json", "test/testdata/audit/real-one", "test/testdata/spikes/broken/record.json"}, p)
		},
		Evidence:          func(p string, _ Claim) Evidence { return evidence[p] },
		RefusalPrimitives: []string{"hooks"},
	}
}

// problems are the matrix's problems with find replaced by repl, as
// `rule: msg` lines.
func problems(t *testing.T, find, repl string, edit func(*MatrixContext)) []string {
	t.Helper()
	if !strings.Contains(matrixSrc, find) {
		t.Fatalf("anchor not in the matrix: %s", find)
	}
	ctx := matrixCtx()
	if edit != nil {
		edit(&ctx)
	}
	var out []string
	for _, p := range MatrixProblems(ParseMatrix(strings.Replace(matrixSrc, find, repl, 1)), ctx) {
		out = append(out, p.Rule+": "+p.Msg)
	}
	return out
}

func reports(t *testing.T, lines []string, want string) {
	t.Helper()
	re := regexp.MustCompile(want)
	if !slices.ContainsFunc(lines, re.MatchString) {
		t.Errorf("no problem matches %s in:\n%s", want, strings.Join(lines, "\n"))
	}
}

func TestParseMatrix(t *testing.T) {
	m := ParseMatrix(matrixSrc)
	if len(m.Issues) != 0 || !reflect.DeepEqual(m.Runtimes, []string{"alpha", "beta"}) || !reflect.DeepEqual(m.Platforms, []string{"linux", "windows"}) {
		t.Fatalf("matrix = %+v", m)
	}
	if s, _ := m.Field("alpha", "hooks", "source"); s != "test/testdata/spikes/alpha-hooks/record.json, a spike" {
		t.Errorf("a quoted value with a comma: %q", s)
	}
	two, _ := m.Guarantees.Get("rule/two")
	if !reflect.DeepEqual(two.Keys(), []string{"alpha", "beta", "alpha@windows"}) {
		t.Errorf("guarantee keys = %v", two.Keys())
	}
	if o, _ := m.Observation.Get("alpha"); o != "jhon audit" {
		t.Errorf("observation = %q", o)
	}
	if b, _ := m.Observation.Get("beta"); b != "none" || !reflect.DeepEqual(m.Observation.Keys(), []string{"alpha", "beta"}) {
		t.Errorf("observation beta = %q, keys %v", b, m.Observation.Keys())
	}
	if !reflect.DeepEqual(m.Primitives, []string{"hooks", "transcripts"}) {
		t.Errorf("primitives = %v", m.Primitives)
	}
	alpha, _ := m.Primitive.Get("alpha")
	hooks, _ := alpha.Get("hooks")
	if got := flat(hooks); !reflect.DeepEqual(got, map[string]string{"capability": "true", "verification": "verified", "version": "1.0", "source": "test/testdata/spikes/alpha-hooks/record.json, a spike"}) {
		t.Errorf("alpha.hooks = %v", got)
	}
	if s, _ := m.Field("beta", "hooks", "source"); s != "no evidence" {
		t.Errorf("a trailing comment: %q", s)
	}
	beta, _ := m.Delivery.Get("beta")
	if got := flat(beta); !reflect.DeepEqual(got, map[string]string{"max_instruction_bytes": "100", "imports": "false", "source": "vendor docs"}) {
		t.Errorf("delivery beta = %v", got)
	}
	if got := flat(two); !reflect.DeepEqual(got, map[string]string{"alpha": "behavioural", "beta": "unsupported", "alpha@windows": "unsupported"}) {
		t.Errorf("rule/two = %v", got)
	}
	if one, _ := m.Guarantees.Get("rule/one"); flat(one)["by"] != "hooks" {
		t.Errorf("rule/one by = %v", flat(one))
	}
}

func flat(m Map[string]) map[string]string {
	out := map[string]string{}
	for _, k := range m.Keys() {
		out[k], _ = m.Get(k)
	}
	return out
}

func TestParseMatrixIssues(t *testing.T) {
	m := ParseMatrix("runtimes: alpha\nguarantees:\n  r: { a: \"x\" y }\n  s: { nope }\n  bad line\n")
	var got []string
	for _, i := range m.Issues {
		got = append(got, i.Msg)
	}
	want := []string{
		"runtimes must be a list on one line",
		"cannot parse guarantees entry r: Unexpected non-whitespace character after JSON at position 4 (line 1 column 5)",
		"cannot parse guarantees entry s: 'nope' is not a key: value pair",
		"cannot parse guarantees entry: bad line",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("issues = %q", got)
	}
}

func TestMatrixProblems(t *testing.T) {
	if p := problems(t, "", "", nil); len(p) != 0 {
		t.Fatalf("the example has problems: %v", p)
	}
	const spike = `source: "test/testdata/spikes/alpha-hooks/record.json, a spike"`
	for _, c := range []struct {
		find, repl string
		edit       func(*MatrixContext)
		want       string
	}{
		{"    transcripts: { capability: false, verification: documented, version: \"2.0\", source: \"vendor docs\" }\n", "", nil, `^runtime-matrix: beta: no runtime_primitives entry for transcripts$`},
		{"capability: false, verification: unknown", "capability: maybe, verification: unknown", nil, `^runtime-matrix: beta\.hooks: capability must be true or false, found 'maybe'$`},
		{"verification: unknown", "verification: probable", nil, `^runtime-matrix: beta\.hooks: verification must be one of verified\|documented\|unknown, found 'probable'$`},
		{`version: "2.0", source: "no evidence"`, `source: "no evidence"`, nil, `^runtime-matrix: beta\.hooks: has no version$`},
		{spike, `source: "a live run I remember"`, nil, `^runtime-matrix: alpha\.hooks: verified needs a spike record \(test/testdata/spikes/<name>/record\.json\); its source cites none$`},
		{spike, `source: "test/testdata/ghost"`, nil, `^runtime-matrix: alpha\.hooks: test/testdata/ghost does not exist$`},
		{`source: "test/testdata/audit/real-one, a real session"`, `source: "test/runtimes.test.ts"`, nil, `^runtime-matrix: alpha\.transcripts: verified needs a spike record \(test/testdata/spikes/<name>/record\.json\) or a real-session fixture \(test/testdata/audit/<name>, source\.json real\); its source cites none$`},
		{spike, `source: "test/testdata/audit/real-one"`, nil, `^runtime-matrix: alpha\.hooks: hooks needs a spike record: a real-session fixture does not show a refusal honoured$`},
		{spike, `source: "test/testdata/spikes/broken/record.json"`, nil, `^runtime-matrix: alpha\.hooks: test/testdata/spikes/broken/record\.json: lacks no_effect_check$`},
		{"  beta: { max_instruction_bytes: 100, imports: false, source: \"vendor docs\" }\n", "", nil, `^runtime-matrix: beta: no contract_delivery entry$`},
		{"max_instruction_bytes: 100", "max_instruction_bytes: lots", nil, `^runtime-matrix: beta: max_instruction_bytes must be a byte count, none, or unknown, found 'lots'$`},
		{"  beta: none\n", "", nil, `^runtime-matrix: beta: no observation_route entry$`},
		{"  rule/one: { by: hooks, alpha: enforced, beta: unsupported }\n", "", nil, `^runtime-guarantee: rule/one has no guarantees entry$`},
		{"", "", func(c *MatrixContext) { c.Anchors = []string{"rule/two"} }, `^runtime-guarantee: rule/one is not a behavioural anchor the renderer enumerates$`},
		{"alpha: behavioural, beta: unsupported,", "alpha: behavioural,", nil, `^runtime-guarantee: rule/two: no level for beta$`},
		{"beta: unsupported }", "beta: partial }", nil, `^runtime-guarantee: rule/one: beta: level must be one of enforced\|behavioural\|unsupported, found 'partial'$`},
		{"beta: unsupported }", "beta: unsupported, gamma: enforced }", nil, `^runtime-guarantee: rule/one: gamma is not a runtime$`},
		{"{ by: hooks, alpha: enforced", "{ alpha: enforced", nil, `^runtime-guarantee: rule/one: alpha is enforced but the entry names no enabling primitive \(by\)$`},
		{"{ by: hooks, alpha: enforced", "{ by: magic, alpha: enforced", nil, `^runtime-guarantee: rule/one: by: magic is not a primitive$`},
		{"hooks: { capability: true, verification: verified", "hooks: { capability: true, verification: documented", nil, `^runtime-guarantee: rule/one: alpha is enforced but hooks is documented there, not verified$`},
		{"beta: unsupported }", "beta: enforced }", nil, `^runtime-guarantee: rule/one: beta is enforced but beta has no hooks$`},
		{"", "", func(c *MatrixContext) { c.Projections = projections(map[string][]string{"alpha": {}}) }, `^runtime-guarantee: rule/one: alpha is enforced through hooks, which the alpha render does not project$`},
		{"", "", func(c *MatrixContext) { c.Projections = projections(nil) }, `^runtime-guarantee: rule/one: alpha is enforced, but alpha is not rendered, so no Jhon mechanism runs there$`},
		{"  alpha: jhon audit\n", "  alpha: none\n", nil, `^runtime-guarantee: rule/two: alpha is behavioural but alpha has no observation route; without one it is unsupported$`},
		{"", "", func(c *MatrixContext) { c.AuditRules[1]["check"] = "none" }, `^runtime-guarantee: rule/two: alpha is behavioural but its audit_rules entry has check none; without a measurement it is unsupported$`},
		{"alpha@windows: unsupported", "alpha@amiga: unsupported", nil, `^runtime-guarantee: rule/two: alpha@amiga: amiga is not a platform$`},
		{"alpha@windows: unsupported", "alpha@windows: behavioural", nil, `^runtime-guarantee: rule/two: alpha@windows: the level is the same as alpha's; an exception exists only where the level changes$`},
	} {
		reports(t, problems(t, c.find, c.repl, c.edit), c.want)
	}
	// An incomplete spike record verifies nothing.
	reports(t, problems(t, spike, `source: "test/testdata/spikes/broken/record.json"`, nil), `^runtime-matrix: alpha\.hooks: verified needs a spike record`)
	// A unit test is not verified evidence: it shows Jhon's side, not the runtime's.
	reports(t, problems(t, spike, `source: "test/runtimes.test.ts exercises the guard"`, nil), `^runtime-matrix: alpha\.hooks: verified needs a spike record \(test/testdata/spikes/<name>/record\.json\); its source cites none$`)
	reports(t, problems(t, "imports: false", "imports: sometimes", nil), `^runtime-matrix: beta: imports must be true or false, found 'sometimes'$`)
}

func TestBudgetProblem(t *testing.T) {
	m := ParseMatrix(matrixSrc)
	if BudgetProblem(m, "beta", 100) != "" || BudgetProblem(m, "alpha", 10_000_000) != "" {
		t.Error("a bundle within budget, or with imports, is refused")
	}
	if got := BudgetProblem(m, "beta", 101); got != "the always-on bundle is 101 bytes and beta takes at most 100 bytes, inline (no imports)" {
		t.Errorf("over budget = %q", got)
	}
	if BudgetProblem(ParseMatrix(strings.Replace(matrixSrc, "max_instruction_bytes: 100", "max_instruction_bytes: unknown", 1)), "beta", 10_000_000) != "" {
		t.Error("an unknown budget is the caller's to report")
	}
}

func TestReport(t *testing.T) {
	ctx := matrixCtx()
	o := ReportOptions{AuditRules: ctx.AuditRules, Levels: levels, BundleBytes: 50}
	text, failed := Report(ParseMatrix(matrixSrc), "alpha", o)
	for _, want := range []string{
		`(?m)^runtime alpha$`, `(?m)^  enforced +rule/one +by hooks$`, `(?m)^  behavioural +rule/two +jhon audit \(c2\); windows: unsupported$`,
		`(?m)^  verified +hooks +yes +1\.0 +test/testdata/spikes/alpha-hooks/record\.json, a spike$`,
		`(?m)^  verified +transcripts +yes +1\.0 +test/testdata/audit/real-one, a real session$`,
		`(?m)^  ok +imports: the runtime takes imports, so no instruction budget binds the always-on bundle$`,
	} {
		if !regexp.MustCompile(want).MatchString(text) {
			t.Errorf("report lacks %s:\n%s", want, text)
		}
	}
	if failed {
		t.Error("a clean report failed")
	}
	if regexp.MustCompile(`score|%`).MatchString(text) {
		t.Errorf("the report scores:\n%s", text)
	}
	o.BundleBytes = 101
	if text, failed := Report(ParseMatrix(matrixSrc), "beta", o); !failed || !strings.Contains(text, "  FAIL  the always-on bundle is 101 bytes and beta takes at most 100 bytes, inline (no imports)\n") || !strings.Contains(text, "  unknown     hooks        no   2.0  no evidence\n") ||
		!regexp.MustCompile(`(?m)^  unsupported +rule/one$`).MatchString(text) {
		t.Errorf("over budget:\n%s", text)
	}
	o.BundleBytes, o.Problems = 50, []Finding{{"runtime-guarantee", "rule/one: beta is enforced but beta has no hooks"}}
	if text, failed := Report(ParseMatrix(matrixSrc), "beta", o); !failed || !regexp.MustCompile(`(?m)^  FAIL +runtime-guarantee: rule/one: beta is enforced but beta has no hooks$`).MatchString(text) {
		t.Errorf("a problem naming beta does not fail its report:\n%s", text)
	}
	if _, failed := Report(ParseMatrix(matrixSrc), "alpha", o); failed {
		t.Error("a problem naming beta fails alpha's report")
	}
	o.Problems = nil
	text, failed = Report(ParseMatrix(strings.Replace(matrixSrc, "max_instruction_bytes: 100", "max_instruction_bytes: unknown", 1)), "beta", o)
	if failed || !regexp.MustCompile(`(?m)^  warn +the always-on bundle is 50 bytes; beta's instruction budget is unknown, so whether it fits is unchecked$`).MatchString(text) {
		t.Errorf("an unknown budget is unchecked, not fitting:\n%s", text)
	}
}

func TestDeclaration(t *testing.T) {
	m := ParseMatrix(matrixSrc)
	alpha := Declaration(m, "alpha", levels)
	if !strings.HasPrefix(alpha, "Runtime guarantees in alpha (agents.yaml `guarantees`): 1 enforced, 1 behavioural (measured by jhon audit), 0 unsupported.\n") ||
		!strings.Contains(alpha, "`jhon doctor --runtime alpha` lists every rule with its level and what holds or measures it.") ||
		strings.Contains(alpha, "rule/one") || strings.Contains(alpha, "rule/two") || len(alpha) >= 600 {
		t.Errorf("alpha = %q", alpha)
	}
	beta := Declaration(m, "beta", levels)
	if !strings.Contains(beta, ": 0 enforced, 0 behavioural (measured by none), 2 unsupported.") || !strings.HasSuffix(beta, "\nAbsent here: hooks, transcripts.") ||
		!strings.Contains(beta, "An unsupported rule is neither enforced nor measured here; it binds by instruction alone.") {
		t.Errorf("beta = %q", beta)
	}
}

// T56: the status Report shows is the one jhon audit gives a finding of the
// rule on that runtime: the entry's validation_status, or
// pending-real-fixture where `pending_on` names the runtime; a list check
// prints as its names.
func TestReportStatusPerRuntime(t *testing.T) {
	rules := func(pending string) []map[string]string {
		return []map[string]string{{"anchor": "rule/two", "check": "[c2, c3]", "evidence_class": "deterministic", "validation_status": "verified", "pending_on": pending}}
	}
	o := ReportOptions{AuditRules: rules("[alpha]"), Levels: levels, BundleBytes: 50}
	text, _ := Report(ParseMatrix(matrixSrc), "alpha", o)
	if !regexp.MustCompile(`(?m)^  behavioural +rule/two +jhon audit \(c2, c3, pending-real-fixture\); windows: unsupported$`).MatchString(text) {
		t.Errorf("pending on alpha:\n%s", text)
	}
	o.AuditRules = rules("[beta]")
	text, _ = Report(ParseMatrix(matrixSrc), "alpha", o)
	if !regexp.MustCompile(`(?m)^  behavioural +rule/two +jhon audit \(c2, c3, verified\); windows: unsupported$`).MatchString(text) {
		t.Errorf("pending on beta only:\n%s", text)
	}
}
