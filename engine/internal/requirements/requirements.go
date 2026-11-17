// Package requirements is the structure of a spec file, and nothing else.
//
// `jhon status` and `jhon compose` both need to know where requirement
// blocks start and end. Two parsers would disagree on an edge (a heading
// inside a code fence, a CRLF file, a missing trailing newline), and the
// disagreement would surface as a status that calls a spec done while
// archive cannot compose it. This package is the one reading both use.
//
// It reports structure: sections, requirement blocks with offset ranges into
// the source string, scenarios, and annotation lines. It holds no business
// rule. Which annotations are required, what a duplicate means, and how a
// delta applies belong to the callers and to contracts/spec-delta.md.
//
// Offsets are byte offsets into the source string, not UTF-16 units:
// callers slice the same Go string they parsed, so every
// untouched range is reproduced exactly.
package requirements

import (
	"errors"
	"math"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// Range is a half-open byte range of the source.
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Line is one physical line. End includes the line's EOL; Text excludes it.
type Line struct {
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Text   string `json:"text"`
	Number int    `json:"number"`
	Fenced bool   `json:"fenced"`
}

// Scenario is a scenario heading of a requirement.
type Scenario struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

// Annotation is a whole line naming a declared metadata key.
type Annotation struct {
	Name  string `json:"name"`
	Line  int    `json:"line"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// Requirement is a requirement block, from its heading to the next heading
// at its level or above.
type Requirement struct {
	// Name is the heading text after `Requirement:`, trimmed.
	Name string `json:"name"`
	// NameRange is where that name sits in the source, for a rename in place.
	NameRange Range `json:"nameRange"`
	Line      int   `json:"line"`
	// Section is the title of the enclosing level-two section, nil before the first.
	Section     *string      `json:"section"`
	Start       int          `json:"start"`
	End         int          `json:"end"`
	Scenarios   []Scenario   `json:"scenarios"`
	Annotations []Annotation `json:"annotations"`
}

// Section is a level-two section.
type Section struct {
	Title string `json:"title"`
	// Op is the delta operation this section declares, when its title is
	// `<op> Requirements`; nil otherwise.
	Op           *string        `json:"op"`
	Line         int            `json:"line"`
	Start        int            `json:"start"`
	End          int            `json:"end"`
	Requirements []*Requirement `json:"requirements"`
}

// Parsed is a spec's structure.
type Parsed struct {
	Sections     []*Section     `json:"sections"`
	Requirements []*Requirement `json:"requirements"`
}

var (
	eol         = regexp.MustCompile(`\r?\n$`)
	fenceMarker = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(" + js.Dot + `*)$`)
	atxHeading  = regexp.MustCompile(`^( {0,3})(#{1,6})([ \t]+)(` + js.Dot + `*?)[ \t]*$`)
)

// MarkdownLines splits src into lines, marking fenced content. A fence
// opens with three or more backticks or tildes (up to three spaces of
// indent) and closes with a line of the same character at least as long.
// The marker lines count as fenced.
func MarkdownLines(src string) []Line {
	out := []Line{}
	var fenceCh byte
	fenceLen := 0
	open := false
	for start, number := 0, 1; start < len(src); number++ {
		end := len(src)
		if nl := strings.IndexByte(src[start:], '\n'); nl >= 0 {
			end = start + nl + 1
		}
		text := eol.ReplaceAllString(src[start:end], "")
		marker := fenceMarker.FindStringSubmatch(text)
		fenced := open
		if open {
			if marker != nil && marker[1][0] == fenceCh && len(marker[1]) >= fenceLen && js.Trim(marker[2]) == "" {
				open = false
			}
		} else if marker != nil && !(marker[1][0] == '`' && strings.Contains(marker[2], "`")) {
			open, fenceCh, fenceLen = true, marker[1][0], len(marker[1])
			fenced = true
		}
		out = append(out, Line{Start: start, End: end, Text: text, Number: number, Fenced: fenced})
		start = end
	}
	return out
}

// Heading is an ATX heading: its level, its trimmed text, and the byte
// offset of that text in the line.
type Heading struct {
	Level     int
	Text      string
	TextStart int
}

// HeadingOf is the ATX heading a line holds outside a fence, or nil.
func HeadingOf(line Line) *Heading {
	if line.Fenced {
		return nil
	}
	m := atxHeading.FindStringSubmatch(line.Text)
	if m == nil {
		return nil
	}
	return &Heading{Level: len(m[2]), Text: m[4], TextStart: len(m[1]) + len(m[2]) + len(m[3])}
}

// TableCells is the trimmed cells of a markdown table row `| a | b |`. As in
// GFM, `\|` is a literal pipe inside a cell, code spans included, never a
// column break (docket BA1).
func TableCells(row string) []string {
	inner := strings.TrimPrefix(js.Trim(row), "|")
	if n := len(inner); n > 0 && inner[n-1] == '|' && (n == 1 || inner[n-2] != '\\') {
		inner = inner[:n-1]
	}
	var cells []string
	from := 0
	for i := 0; i < len(inner); i++ {
		if inner[i] == '|' && (i == 0 || inner[i-1] != '\\') {
			cells = append(cells, inner[from:i])
			from = i + 1
		}
	}
	cells = append(cells, inner[from:])
	for i, c := range cells {
		cells[i] = js.Trim(strings.ReplaceAll(c, `\|`, "|"))
	}
	return cells
}

var (
	requirementLead = regexp.MustCompile(`^Requirement:[ \t]*`)
	scenarioLead    = regexp.MustCompile(`^Scenario:[ \t]*`)
	opSection       = regexp.MustCompile(`^([^` + js.SpaceChars + `]+) Requirements$`)
)

// The heading levels contracts/spec-delta.md gives a delta's requirements
// and scenarios. A main spec's depth is detected instead; see MainSpecDepth.
const (
	RequirementLevel = 3
	ScenarioLevel    = 4
)

// The kinds of heading HeadingKind tells apart.
const (
	KindRequirement = "Requirement"
	KindScenario    = "Scenario"
)

// HeadingKind is whether an unfenced heading's text names a requirement
// (KindRequirement), a scenario (KindScenario), or neither ("").
func HeadingKind(text string) string {
	switch {
	case requirementLead.MatchString(text):
		return KindRequirement
	case scenarioLead.MatchString(text):
		return KindScenario
	}
	return ""
}

// DepthMismatch is a requirement or scenario heading at another level than
// the contract's.
type DepthMismatch struct {
	Line     int    `json:"line"`
	Kind     string `json:"kind"`
	Found    int    `json:"found"`
	Expected int    `json:"expected"`
}

// HeadingDepthMismatch is the first unfenced `Requirement:` or `Scenario:`
// heading at a level other than the contract's, or nil. ParseSpec
// recognises only the contract's levels, so a spec written at another depth
// parses as holding no requirements at all; callers that must not act on
// that reading check here.
func HeadingDepthMismatch(src string) *DepthMismatch {
	for _, line := range MarkdownLines(src) {
		h := HeadingOf(line)
		if h == nil {
			continue
		}
		kind := HeadingKind(h.Text)
		if kind == "" {
			continue
		}
		expected := ScenarioLevel
		if kind == KindRequirement {
			expected = RequirementLevel
		}
		if h.Level != expected {
			return &DepthMismatch{Line: line.Number, Kind: kind, Found: h.Level, Expected: expected}
		}
	}
	return nil
}

// MainSpecShape is a main spec's structural variants, from
// contracts/spec-delta.md `main_spec`.
type MainSpecShape struct {
	RequirementDepths   []int  `json:"requirementDepths"`
	RequirementsSection string `json:"requirementsSection"`
	DepthWithSection    int    `json:"depthWithSection"`
	DepthOtherwise      int    `json:"depthOtherwise"`
}

// The reasons a main spec fits no variant.
const (
	RefusalMixed      = "mixed"
	RefusalOutOfRange = "out-of-range"
	RefusalScenario   = "scenario"
)

// MainDepthRefusal is why a main spec fits no variant.
type MainDepthRefusal struct {
	// Reason is RefusalMixed, RefusalOutOfRange, or RefusalScenario.
	Reason string `json:"reason"`
	Kind   string `json:"kind"`
	Line   int    `json:"line"`
	Found  int    `json:"found"`
	// Expected is the level or levels the line may take.
	Expected []int `json:"expected"`
	// FirstLine is the line that fixed the expectation: the first requirement heading.
	FirstLine int `json:"firstLine"`
}

type leveled struct{ line, level int }

// MainSpecDepth is the requirement depth of a main spec, or why it fits no
// variant (a non-nil refusal). The order is the contract's: requirement
// headings decide when they exist; otherwise an exact requirements section;
// otherwise the default. The section never overrides requirements the spec
// already holds.
func MainSpecDepth(src string, shape MainSpecShape) (int, *MainDepthRefusal) {
	var reqs, scenarios []leveled
	section := false
	for _, line := range MarkdownLines(src) {
		h := HeadingOf(line)
		if h == nil {
			continue
		}
		switch HeadingKind(h.Text) {
		case KindRequirement:
			reqs = append(reqs, leveled{line.Number, h.Level})
		case KindScenario:
			scenarios = append(scenarios, leveled{line.Number, h.Level})
		default:
			if h.Level == 2 && h.Text == shape.RequirementsSection {
				section = true
			}
		}
	}
	offScenario := func(depth int) *leveled {
		if i := slices.IndexFunc(scenarios, func(sc leveled) bool { return sc.level != depth+1 }); i >= 0 {
			return &scenarios[i]
		}
		return nil
	}
	if len(reqs) == 0 {
		depth := shape.DepthOtherwise
		if section {
			depth = shape.DepthWithSection
		}
		if off := offScenario(depth); off != nil {
			return 0, &MainDepthRefusal{RefusalScenario, KindScenario, off.line, off.level, []int{depth + 1}, off.line}
		}
		return depth, nil
	}
	first := reqs[0]
	if !slices.Contains(shape.RequirementDepths, first.level) {
		return 0, &MainDepthRefusal{RefusalOutOfRange, KindRequirement, first.line, first.level, shape.RequirementDepths, first.line}
	}
	if i := slices.IndexFunc(reqs, func(r leveled) bool { return r.level != first.level }); i >= 0 {
		return 0, &MainDepthRefusal{RefusalMixed, KindRequirement, reqs[i].line, reqs[i].level, []int{first.level}, first.line}
	}
	if off := offScenario(first.level); off != nil {
		return 0, &MainDepthRefusal{RefusalScenario, KindScenario, off.line, off.level, []int{first.level + 1}, first.line}
	}
	return first.level, nil
}

// Options are what ParseSpec reads besides the source: the declared
// metadata keys, the delta operations, and the requirement heading level
// (zero: RequirementLevel).
type Options struct {
	Metadata []string
	Ops      []string
	Depth    int
}

// ParseSpec parses a spec's structure. Depth is the requirement heading
// level, the delta's fixed level unless a caller has detected a main spec's;
// scenarios sit one level below it. At depth 2 a `## Requirement:` heading
// is a requirement, not a section, and still closes the section before it.
func ParseSpec(src string, opts Options) Parsed {
	reqLevel := opts.Depth
	if reqLevel == 0 {
		reqLevel = RequirementLevel
	}
	scenarioLevel := reqLevel + 1
	var annotation *regexp.Regexp
	if len(opts.Metadata) > 0 {
		keys := make([]string, len(opts.Metadata))
		for i, k := range opts.Metadata {
			keys[i] = regexp.QuoteMeta(k)
		}
		annotation = regexp.MustCompile(`^\((` + strings.Join(keys, "|") + `):` + js.Dot + `*\)` + js.Space + `*$`)
	}
	out := Parsed{Sections: []*Section{}, Requirements: []*Requirement{}}
	var section *Section
	var req *Requirement
	closeReq := func(at int) {
		if req != nil {
			req.End, req = at, nil
		}
	}
	closeSection := func(at int) {
		if section != nil {
			section.End, section = at, nil
		}
	}

	for _, line := range MarkdownLines(src) {
		h := HeadingOf(line)
		if h != nil && h.Level <= reqLevel {
			closeReq(line.Start)
		}
		if h != nil && h.Level <= 2 {
			closeSection(line.Start)
		}

		switch {
		case h != nil && h.Level == reqLevel && requirementLead.MatchString(h.Text):
			lead := len(requirementLead.FindString(h.Text))
			raw := h.Text[lead:]
			name := js.Trim(raw)
			nameStart := line.Start + h.TextStart + lead + (len(raw) - len(strings.TrimLeftFunc(raw, js.IsSpace)))
			req = &Requirement{
				Name: name, NameRange: Range{nameStart, nameStart + len(name)},
				Line: line.Number, Start: line.Start, End: len(src),
				Scenarios: []Scenario{}, Annotations: []Annotation{},
			}
			if section != nil {
				title := section.Title
				req.Section = &title
			}
			out.Requirements = append(out.Requirements, req)
			if section != nil {
				section.Requirements = append(section.Requirements, req)
			}
		case h != nil && h.Level == 2:
			section = &Section{Title: h.Text, Line: line.Number, Start: line.Start, End: len(src), Requirements: []*Requirement{}}
			if m := opSection.FindStringSubmatch(h.Text); m != nil && slices.Contains(opts.Ops, m[1]) {
				section.Op = &m[1]
			}
			out.Sections = append(out.Sections, section)
		case req != nil && h != nil && h.Level == scenarioLevel && scenarioLead.MatchString(h.Text):
			req.Scenarios = append(req.Scenarios, Scenario{js.Trim(scenarioLead.ReplaceAllString(h.Text, "")), line.Number})
		case req != nil && !line.Fenced && annotation != nil:
			if a := annotation.FindStringSubmatch(line.Text); a != nil {
				req.Annotations = append(req.Annotations, Annotation{a[1], line.Number, line.Start, line.End})
			}
		}
	}
	closeReq(len(src))
	closeSection(len(src))
	return out
}

// DeltaContract is the delta format's machine-readable values, from
// contracts/spec-delta.md.
type DeltaContract struct {
	Order    []string
	Metadata []string
	// Required is the annotations each operation requires.
	Required    map[string][]string
	HeadingOnly []string
	Main        MainSpecShape
}

// ParseDeltaContract reads the delta format's values from the text of
// contracts/spec-delta.md; an error when main_spec is malformed.
func ParseDeltaContract(src string) (DeltaContract, error) {
	main, err := ParseMainSpecContract(src)
	if err != nil {
		return DeltaContract{}, err
	}
	required := map[string][]string{}
	keys, values := contract.Map(src, "delta_required")
	for _, op := range keys {
		required[op] = contract.InlineList(values[op])
	}
	return DeltaContract{
		Order:       contract.List(src, "delta_order"),
		Metadata:    contract.List(src, "delta_metadata"),
		Required:    required,
		HeadingOnly: contract.List(src, "delta_heading_only"),
		Main:        main,
	}, nil
}

var delta = sync.OnceValues(func() (DeltaContract, error) {
	src, err := contract.Contract("spec-delta.md")
	if err != nil {
		return DeltaContract{}, err
	}
	return ParseDeltaContract(src)
})

// Delta is the delta format's values from this release's
// contracts/spec-delta.md, read on first use.
func Delta() (DeltaContract, error) { return delta() }

var errMainSpec = errors.New("contracts/spec-delta.md: main_spec must declare requirement_depths, requirements_section, and depth_with_section and depth_otherwise within requirement_depths")

// ParseMainSpecContract reads a main spec's structural variants from the
// text of contracts/spec-delta.md. Malformed values stop the tool: an error.
func ParseMainSpecContract(src string) (MainSpecShape, error) {
	_, m := contract.Map(src, "main_spec")
	var depths []float64
	for _, d := range contract.InlineList(m["requirement_depths"]) {
		depths = append(depths, js.Number(d))
	}
	// Number(undefined) is NaN, not Number("")'s 0.
	number := func(k string) float64 {
		if v, ok := m[k]; ok {
			return js.Number(v)
		}
		return math.NaN()
	}
	with, otherwise := number("depth_with_section"), number("depth_otherwise")
	if len(depths) == 0 || !all(depths, js.IsInteger) || m["requirements_section"] == "" ||
		!slices.Contains(depths, with) || !slices.Contains(depths, otherwise) {
		return MainSpecShape{}, errMainSpec
	}
	shape := MainSpecShape{RequirementsSection: m["requirements_section"], DepthWithSection: int(with), DepthOtherwise: int(otherwise)}
	for _, d := range depths {
		shape.RequirementDepths = append(shape.RequirementDepths, int(d))
	}
	return shape, nil
}

func all(fs []float64, ok func(float64) bool) bool {
	for _, f := range fs {
		if !ok(f) {
			return false
		}
	}
	return true
}
