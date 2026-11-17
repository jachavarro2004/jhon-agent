package ldd

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"maps"
	"slices"
	"strings"
)

// What a change's record needs (T31, L252, L311): only what Jhon can
// measure. tasks.md when the work cannot be resumed from the diff: over the
// advisory lines of code (the route's, LDD record files never counted), a
// high-risk item, or more than one component (the discovered projects the
// route reads the change touching). A spec delta when a main spec under
// openspec/specs/ changes or is added. A proposal and a design when the
// route asks for them, as today. Facts for status; nothing here gates.

// Needs is what the change's record needs, and why each.
type Needs struct {
	Tasks    bool     `json:"tasks"`
	Spec     bool     `json:"spec"`
	Proposal bool     `json:"proposal"`
	Design   bool     `json:"design"`
	Why      NeedsWhy `json:"why"`
}

// NeedsWhy is the reasons for each need; empty when it is not needed.
type NeedsWhy struct {
	Tasks    []string `json:"tasks"`
	Spec     []string `json:"spec"`
	Proposal []string `json:"proposal"`
	Design   []string `json:"design"`
}

// mainSpecs is where the main specs live, relative to the git top level.
func mainSpecs(top string) string { return layout.For(top).SpecsDir() + "/" }

// NeedsTasks is why route r's change needs tasks.md; none when it does not.
func NeedsTasks(r Route) []string {
	var why []string
	if r.Lines > r.VerifiedLines {
		why = append(why, fmt.Sprintf("%d lines of code, over the advisory %d", r.Lines, r.VerifiedLines))
	}
	if r.Tier == High {
		why = append(why, "risk "+string(High))
	}
	if len(r.Projects) > 1 {
		why = append(why, fmt.Sprintf("%d components: %s", len(r.Projects), strings.Join(r.Projects, ", ")))
	}
	return why
}

// NeedsSpec is why a change whose changed files are changed (path: git's
// status letter, A for added) needs a spec delta: each main spec it changes
// or adds.
func NeedsSpec(changed map[string]string) []string {
	var why []string
	for _, f := range slices.Sorted(maps.Keys(changed)) {
		if !slices.ContainsFunc(layout.Layouts, func(l layout.Layout) bool { return strings.HasPrefix(f, l.SpecsDir()+"/") }) {
			continue
		}
		verb := "changes"
		if changed[f] == "A" {
			verb = "is added"
		}
		why = append(why, "main spec "+f+" "+verb)
	}
	return why
}

// NeedsDesign is why route r's change needs a proposal and a design: the
// route asks for them (Route.Design).
func NeedsDesign(r Route) []string {
	if !r.Design {
		return nil
	}
	return []string{"the route asks for them: it spans " + strings.Join(r.Spans, ", ")}
}

// NeedsOf is what the change on route r in the repository whose top level
// is top needs.
func NeedsOf(top string, r Route) Needs { return gitReads(nil).needsOf(top, r) }

func (g gitReads) needsOf(top string, r Route) Needs {
	or := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	design := or(NeedsDesign(r))
	why := NeedsWhy{Tasks: or(NeedsTasks(r)), Spec: or(NeedsSpec(g.mainSpecsChanged(top))), Proposal: design, Design: design}
	return Needs{len(why.Tasks) > 0, len(why.Spec) > 0, len(design) > 0, len(design) > 0, why}
}

// mainSpecsChanged is the files under openspec/specs/ changed against the
// branch's base, with git's status letter; an untracked one is added.
func (g gitReads) mainSpecsChanged(top string) map[string]string {
	out := map[string]string{}
	base, _, ok := g.baseOf(top, nil)
	if !ok {
		return out
	}
	diff, _ := g.git(top, "diff", "--name-status", "--no-renames", base, "--", mainSpecs(top))
	for _, l := range nonEmpty(diff) {
		if f := strings.SplitN(l, "\t", 2); len(f) == 2 {
			out[f[1]] = f[0]
		}
	}
	untracked, _ := g.git(top, "ls-files", "--others", "--exclude-standard", "--", mainSpecs(top))
	for _, f := range nonEmpty(untracked) {
		out[f] = "A"
	}
	return out
}
