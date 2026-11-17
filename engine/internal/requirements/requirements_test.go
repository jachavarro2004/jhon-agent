package requirements

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	root, _ := filepath.Abs("../../..")
	os.Setenv("JHON_ROOT", root)
	os.Exit(m.Run())
}

var (
	meta = []string{"Previously", "Reason", "Migration"}
	ops  = []string{"RENAMED", "MODIFIED", "REMOVED", "ADDED"}
)

func parse(src string) Parsed { return ParseSpec(src, Options{Metadata: meta, Ops: ops}) }

func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

func names(rs []*Requirement) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func scenarioNames(r *Requirement) []string {
	out := []string{}
	for _, s := range r.Scenarios {
		out = append(out, s.Name)
	}
	return out
}

var mainSpec = strings.Join([]string{
	"# Auth Specification", "", "## Purpose", "", "Sessions.", "", "## Requirements", "",
	"### Requirement: Login", "", "The system MUST log in.", "",
	"#### Scenario: ok", "", "- GIVEN a user", "",
	"#### Scenario: bad password", "", "- THEN refuse", "",
	"### Requirement: Logout", "", "The system MUST log out.", "",
	"## Notes", "", "Trailing prose.", "",
}, "\n")

func TestRequirementBlockRunsToTheNextHeadingAtItsLevel(t *testing.T) {
	reqs := parse(mainSpec).Requirements
	eq(t, "names", names(reqs), []string{"Login", "Logout"})
	login, logout := reqs[0], reqs[1]
	block := mainSpec[login.Start:login.End]
	if !strings.HasPrefix(block, "### Requirement: Login\n") || !strings.HasSuffix(block, "- THEN refuse\n\n") {
		t.Errorf("login block = %q", block)
	}
	eq(t, "logout block", mainSpec[logout.Start:logout.End], "### Requirement: Logout\n\nThe system MUST log out.\n\n")
	eq(t, "scenarios", scenarioNames(login), []string{"ok", "bad password"})
	eq(t, "logout scenarios", logout.Scenarios, []Scenario{})
	eq(t, "line", login.Line, 9)
	eq(t, "name range", mainSpec[login.NameRange.Start:login.NameRange.End], "Login")
}

func TestSections(t *testing.T) {
	sections := parse(mainSpec).Sections
	var got [][2]any
	for _, s := range sections {
		got = append(got, [2]any{s.Title, s.Op})
	}
	eq(t, "sections", got, [][2]any{{"Purpose", (*string)(nil)}, {"Requirements", (*string)(nil)}, {"Notes", (*string)(nil)}})
	eq(t, "requirements", names(sections[1].Requirements), []string{"Login", "Logout"})
	eq(t, "section", *sections[1].Requirements[0].Section, "Requirements")
}

func TestDeltaSections(t *testing.T) {
	src := strings.Join([]string{
		"# Delta for Auth", "## RENAMED Requirements", "### Requirement: Old → New", "(Reason: clearer)",
		"## ADDED Requirements", "### Requirement: Fresh", "#### Scenario: s", "## UNKNOWN Requirements", "",
	}, "\n")
	sections := parse(src).Sections
	var got []string
	for _, s := range sections {
		if s.Op == nil {
			got = append(got, "<nil>")
		} else {
			got = append(got, *s.Op)
		}
	}
	eq(t, "ops", got, []string{"RENAMED", "ADDED", "<nil>"})
	r := sections[0].Requirements[0]
	eq(t, "name", r.Name, "Old → New")
	eq(t, "name range", src[r.NameRange.Start:r.NameRange.End], "Old → New")
}

func TestCRLF(t *testing.T) {
	src := strings.ReplaceAll(mainSpec, "\n", "\r\n")
	reqs := parse(src).Requirements
	eq(t, "names", names(reqs), []string{"Login", "Logout"})
	eq(t, "block", src[reqs[1].Start:reqs[1].End], "### Requirement: Logout\r\n\r\nThe system MUST log out.\r\n\r\n")
	eq(t, "scenarios", scenarioNames(reqs[0]), []string{"ok", "bad password"})
}

func TestNoTrailingNewline(t *testing.T) {
	src := "## Requirements\n\n### Requirement: Only\n\ntext"
	only := parse(src).Requirements[0]
	eq(t, "end", only.End, len(src))
	eq(t, "block", src[only.Start:only.End], "### Requirement: Only\n\ntext")
}

func TestFencedHeadingsAreContent(t *testing.T) {
	src := strings.Join([]string{
		"### Requirement: Real", "```markdown", "### Requirement: Fake", "#### Scenario: fake", "```",
		"~~~", "## Not a section", "~~~", "#### Scenario: real", "",
	}, "\n")
	p := parse(src)
	eq(t, "names", names(p.Requirements), []string{"Real"})
	eq(t, "scenarios", scenarioNames(p.Requirements[0]), []string{"real"})
	eq(t, "sections", p.Sections, []*Section{})
	eq(t, "end", p.Requirements[0].End, len(src))
}

func TestAnnotations(t *testing.T) {
	src := strings.Join([]string{
		"### Requirement: R", "(Reason: gone)", "  (Migration: none)", "Text (Reason: inline is not an annotation)",
		"(Note: undeclared key)", "(Previously: old)", "",
	}, "\n")
	r := parse(src).Requirements[0]
	var got []string
	for _, a := range r.Annotations {
		got = append(got, a.Name)
	}
	eq(t, "annotations", got, []string{"Reason", "Previously"})
	eq(t, "slice", src[r.Annotations[0].Start:r.Annotations[0].End], "(Reason: gone)\n")
}

func TestDeltaContract(t *testing.T) {
	c, err := Delta()
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "order", c.Order, ops)
	eq(t, "metadata", c.Metadata, meta)
	eq(t, "required", c.Required, map[string][]string{"REMOVED": {"Reason"}, "RENAMED": {"Reason"}})
	eq(t, "headingOnly", c.HeadingOnly, []string{"REMOVED", "RENAMED"})
	eq(t, "main", c.Main, MainSpecShape{RequirementDepths: []int{2, 3}, RequirementsSection: "Requirements", DepthWithSection: 3, DepthOtherwise: 2})
}

func TestMainSpecContractRefusesMalformedValues(t *testing.T) {
	const want = "contracts/spec-delta.md: main_spec must declare requirement_depths, requirements_section, and depth_with_section and depth_otherwise within requirement_depths"
	for _, src := range []string{
		"main_spec:\n  requirement_depths: [2, 3]\n  requirements_section: Requirements\n  depth_with_section: 3\n",
		"main_spec:\n  requirement_depths: [2, 3.5]\n  requirements_section: Requirements\n  depth_with_section: 3\n  depth_otherwise: 2\n",
		"main_spec:\n  requirement_depths: [2, 3]\n  depth_with_section: 3\n  depth_otherwise: 2\n",
		"main_spec:\n  requirement_depths: [2, 3]\n  requirements_section: Requirements\n  depth_with_section: 4\n  depth_otherwise: 2\n",
		"",
	} {
		if _, err := ParseMainSpecContract(src); err == nil || err.Error() != want {
			t.Errorf("ParseMainSpecContract(%q) = %v", src, err)
		}
	}
	shape, err := ParseMainSpecContract("main_spec:\n  requirement_depths: [\"2\", 0x3]\n  requirements_section: Requirements\n  depth_with_section: \" 3 \"\n  depth_otherwise: 2.0\n")
	eq(t, "lenient numbers", shape, MainSpecShape{[]int{2, 3}, "Requirements", 3, 2})
	eq(t, "error", err, nil)
}

func TestMainSpecVariants(t *testing.T) {
	c, err := Delta()
	if err != nil {
		t.Fatal(err)
	}
	refusal := func(reason, kind string, line, found int, expected []int, first int) *MainDepthRefusal {
		return &MainDepthRefusal{reason, kind, line, found, expected, first}
	}
	for _, v := range []struct {
		label, src string
		depth      int
		refusal    *MainDepthRefusal
	}{
		{"depth 2 with requirements", "# S\n\n## Requirement: A\n\n### Scenario: a\n", 2, nil},
		{"depth 3 with requirements", "# S\n\n## Requirements\n\n### Requirement: A\n\n#### Scenario: a\n", 3, nil},
		{"no requirements, exact ## Requirements section", "# S\n\n## Requirements\n\n## Other\n", 3, nil},
		{"no requirements, no section", "# S\n\n## Purpose\n\nP.\n", 2, nil},
		{"mixed requirement depths", "# S\n\n## Requirements\n\n### Requirement: A\n\n## Requirement: B\n", 0,
			refusal("mixed", "Requirement", 7, 2, []int{3}, 5)},
		{"scenario not at requirement depth + 1", "# S\n\n## Requirement: A\n\n#### Scenario: a\n", 0,
			refusal("scenario", "Scenario", 5, 4, []int{3}, 3)},
		{"requirement depth outside the variants", "# S\n\n#### Requirement: A\n", 0,
			refusal("out-of-range", "Requirement", 3, 4, []int{2, 3}, 3)},
		{"counterpart: requirements under ## Requirements and under ## Other, all at 3",
			"# S\n\n## Requirements\n\n### Requirement: Refund\n\n## Other\n\n### Requirement: Charge\n", 3, nil},
		{"counterpart: ### Requirement under ## Requirements plus a ## Requirement",
			"# S\n\n## Requirements\n\n### Requirement: Refund\n\n## Requirement: Charge\n", 0,
			refusal("mixed", "Requirement", 7, 2, []int{3}, 5)},
		{"the section never overrides existing requirements", "# S\n\n## Requirements\n\nprose\n\n## Requirement: A\n", 2, nil},
		{"a section title that is not exact does not count", "# S\n\n## Requirements and notes\n\n### Requirements\n", 2, nil},
		{"fenced headings are content", "# S\n\n```\n## Requirement: X\n#### Scenario: y\n## Requirements\n```\n", 2, nil},
		{"a scenario off the default depth", "# S\n\n## Requirements\n\n### Scenario: a\n", 0,
			refusal("scenario", "Scenario", 5, 3, []int{4}, 5)},
	} {
		depth, refusal := MainSpecDepth(v.src, c.Main)
		if depth != v.depth || !reflect.DeepEqual(refusal, v.refusal) {
			t.Errorf("%s: MainSpecDepth = %d, %+v; want %d, %+v", v.label, depth, refusal, v.depth, v.refusal)
		}
	}
}

func TestParseSpecAtDepth2(t *testing.T) {
	src := "# S\n\n## Requirement: A\n\nA.\n\n### Scenario: a\n\n### Notes\n\n## Requirement: B\n\n## Tail\n"
	p := ParseSpec(src, Options{Metadata: meta, Ops: ops, Depth: 2})
	eq(t, "names", names(p.Requirements), []string{"A", "B"})
	eq(t, "scenarios", scenarioNames(p.Requirements[0]), []string{"a"})
	eq(t, "A", src[p.Requirements[0].Start:p.Requirements[0].End], "## Requirement: A\n\nA.\n\n### Scenario: a\n\n### Notes\n\n")
	eq(t, "B", src[p.Requirements[1].Start:p.Requirements[1].End], "## Requirement: B\n\n")
	eq(t, "sections", []string{p.Sections[0].Title}, []string{"Tail"})
	eq(t, "section count", len(p.Sections), 1)
}

func TestMarkdownLines(t *testing.T) {
	lines := MarkdownLines("a\r\n````js\n```\n````\n~~~\n``` x`y\nz")
	var fenced []bool
	for _, l := range lines {
		fenced = append(fenced, l.Fenced)
	}
	eq(t, "fenced", fenced, []bool{false, true, true, true, true, true, true})
	eq(t, "first", lines[0], Line{Start: 0, End: 3, Text: "a", Number: 1})
	eq(t, "last", lines[6], Line{Start: 31, End: 32, Text: "z", Number: 7, Fenced: true})
	eq(t, "empty", MarkdownLines(""), []Line{})
	// An info string holding a backtick opens no backtick fence.
	eq(t, "inline", MarkdownLines("``` a`b\n# H")[1].Fenced, false)
}

func TestHeadingOf(t *testing.T) {
	for text, want := range map[string]*Heading{
		"## Requirements  ":        {2, "Requirements", 3},
		"   #\tTitle # ":           {1, "Title #", 5},
		"####### seven":            nil,
		"    # indented code":      nil,
		"##NoSpace":                nil,
		"## with\rcarriage return": nil,
	} {
		eq(t, text, HeadingOf(Line{Text: text}), want)
	}
	eq(t, "fenced", HeadingOf(Line{Text: "# H", Fenced: true}), (*Heading)(nil))
}

func TestHeadingKindAndDepthMismatch(t *testing.T) {
	eq(t, "requirement", HeadingKind("Requirement:\tX"), KindRequirement)
	eq(t, "scenario", HeadingKind("Scenario: y"), KindScenario)
	eq(t, "neither", HeadingKind("Requirements"), "")
	eq(t, "contract levels", HeadingDepthMismatch("## R\n### Requirement: A\n#### Scenario: a\n"), (*DepthMismatch)(nil))
	eq(t, "scenario off", HeadingDepthMismatch("### Requirement: A\n```\n## Requirement: fenced\n```\n### Scenario: a\n"),
		&DepthMismatch{Line: 5, Kind: KindScenario, Found: 3, Expected: 4})
}

func TestTableCells(t *testing.T) {
	for row, want := range map[string][]string{
		"| a | b |":           {"a", "b"},
		"a | b":               {"a", "b"},
		"| `x \\| y` | z \\|": {"`x | y`", "z |"},
		"|  |":                {""},
		"|":                   {""},
		"| a \\\\| b |":       {`a \| b`}, // the lookbehind sees one backslash
		" | a |\t":            {"a"},
	} {
		eq(t, row, TableCells(row), want)
	}
}
