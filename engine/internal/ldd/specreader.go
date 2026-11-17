package ldd

// This file tells the shaped layouts (ldd/layout: OpenSpec, Native) how to
// read a spec: the requirement, scenario and delta operation headings of
// contracts/spec-delta.md, as package requirements parses them (T57 slice
// 1). Requirements is a leaf and layout stays portable to Windows, so
// neither imports the other: this package, which imports both, joins them.

import (
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/requirements"
)

func init() { layout.ReadSpecsWith(layoutScenarios) }

// layoutScenarios is a spec's requirements and scenarios as the shaped
// layouts read them (layout.Layout.Scenarios), with the delta grammar of
// this release's contracts/spec-delta.md (requirements.Delta).
func layoutScenarios(src string, depth int) ([]layout.Requirement, error) {
	c, err := requirements.Delta()
	if err != nil {
		return nil, err
	}
	parsed := requirements.ParseSpec(src, requirements.Options{Metadata: c.Metadata, Ops: c.Order, Depth: depth})
	if depth == 0 {
		depth = requirements.RequirementLevel
	}
	op := map[*requirements.Requirement]string{}
	for _, s := range parsed.Sections {
		if s.Op != nil {
			for _, r := range s.Requirements {
				op[r] = *s.Op
			}
		}
	}
	lines := requirements.MarkdownLines(src)
	out := make([]layout.Requirement, 0, len(parsed.Requirements))
	for _, r := range parsed.Requirements {
		req := layout.Requirement{Name: r.Name, Op: op[r], Scenarios: []layout.Scenario{}}
		for _, h := range c.HeadingOnly {
			req.HeadingOnly = req.HeadingOnly || (op[r] != "" && op[r] == h)
		}
		for _, s := range r.Scenarios {
			var body []string
			for i := s.Line; i < len(lines) && lines[i].Start < r.End; i++ {
				if h := requirements.HeadingOf(lines[i]); h != nil && h.Level <= depth+1 {
					break
				}
				body = append(body, strings.TrimRight(lines[i].Text, " \t\r"))
			}
			for len(body) > 0 && body[0] == "" {
				body = body[1:]
			}
			for len(body) > 0 && body[len(body)-1] == "" {
				body = body[:len(body)-1]
			}
			req.Scenarios = append(req.Scenarios, layout.Scenario{Name: s.Name, Body: strings.Join(body, "\n")})
		}
		out = append(out, req)
	}
	return out, nil
}
