package ldd

// This file reads the scenarios a slice adds or edits in its change's spec
// deltas, which a slice commit of a `delivery: change` asks a test to name
// (T136), and the scenarios a delta copies unchanged into a MODIFIED block,
// which it never asks for: they are the main spec's, not the slice's.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/requirements"
)

// specScenario is one scenario of a spec: the spec's path under specs/
// (slash-separated), its requirement, its name, the delta operation of its
// block ("" outside one), and its body: the lines under its heading, each
// without trailing blanks, the blank lines around them dropped.
type specScenario struct {
	file, requirement, name, op, body string
}

// key is the scenario's identity across two readings of a spec.
func (s specScenario) key() string { return s.file + "\x00" + s.requirement + "\x00" + s.name }

// scenariosOf are the named scenarios of the spec src at file, its
// requirements at depth (0: a delta's fixed depth).
func scenariosOf(file, src string, depth int, c requirements.DeltaContract) []specScenario {
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
	var out []specScenario
	for _, r := range parsed.Requirements {
		for _, s := range r.Scenarios {
			if s.Name == "" {
				continue
			}
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
			out = append(out, specScenario{file: filepath.ToSlash(file), requirement: r.Name, name: s.Name, op: op[r], body: strings.Join(body, "\n")})
		}
	}
	return out
}

// copiedScenarios are the keys of the scenarios the change's delta specs
// (specFiles' list) copy unchanged into a MODIFIED block: the main spec the
// delta modifies, under the specs directory of the repository at root, holds
// the same scenario of the same requirement with the same body. A main spec
// that cannot be read, or fits no structural variant, holds no copy.
func copiedScenarios(root string, deltas [][2]string, c requirements.DeltaContract) map[string]bool {
	out := map[string]bool{}
	for _, f := range deltas {
		var modified []specScenario
		for _, s := range scenariosOf(f[0], f[1], 0, c) {
			if s.op == "MODIFIED" {
				modified = append(modified, s)
			}
		}
		if len(modified) == 0 {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(layout.For(root).SpecsDir()), f[0]))
		if err != nil {
			continue
		}
		depth, refused := requirements.MainSpecDepth(string(b), c.Main)
		if refused != nil {
			continue
		}
		main := map[string]string{}
		for _, s := range scenariosOf(f[0], string(b), depth, c) {
			main[s.key()] = s.body
		}
		for _, s := range modified {
			if body, ok := main[s.key()]; ok && body == s.body {
				out[s.key()] = true
			}
		}
	}
	return out
}

// deltaSpecsAt are the change's delta specs in commit, as specFiles lists
// them from the working tree: path under specs/ and text.
func (g gitReads) deltaSpecsAt(top, change, commit string) [][2]string {
	dir := layout.For(top).ChangeDir(change) + "/specs/"
	names, ok := g.git(top, "ls-tree", "-r", "--name-only", commit, "--", dir)
	if !ok {
		return nil
	}
	var out [][2]string
	for _, f := range nonEmpty(names) {
		if path.Base(f) != "spec.md" || !strings.HasPrefix(f, dir) {
			continue
		}
		if src, ok := g.git(top, "show", commit+":"+f); ok {
			out = append(out, [2]string{strings.TrimPrefix(f, dir), src})
		}
	}
	return out
}

// unnamedSliceScenarios is how many of the scenarios the slice in progress
// of change adds or edits a test names, and which not yet, in the delivery
// check's words; "" when a test names each. A scenario the slice adds or
// edits is one in the change's delta specs in the working tree, the tree the
// slice commit commits, that its base (sliceBase) lacks or holds with
// another body; a scenario copied unchanged into a MODIFIED block
// (copiedScenarios) is not one.
func unnamedSliceScenarios(top, change string) string {
	c, err := requirements.Delta()
	if err != nil {
		return ""
	}
	now, err := specFiles(filepath.Join(top, execution.ChangesOf(top), change))
	if err != nil || len(now) == 0 {
		return ""
	}
	g := gitReads{}
	before := map[string]string{}
	if base, ok := g.sliceBase(top, change); ok {
		for _, f := range g.deltaSpecsAt(top, change, base) {
			for _, s := range scenariosOf(f[0], f[1], 0, c) {
				before[s.key()] = s.body
			}
		}
	}
	copies := copiedScenarios(top, now, c)
	var names []string
	for _, f := range now {
		for _, s := range scenariosOf(f[0], f[1], 0, c) {
			if body, had := before[s.key()]; (had && body == s.body) || copies[s.key()] || slices.Contains(names, s.name) {
				continue
			}
			names = append(names, s.name)
		}
	}
	coverage := namedCoverage(names, testFiles(top))
	if coverage.Covered == coverage.Total {
		return ""
	}
	return fmt.Sprintf("scenarios named by a test: %d/%d; not yet: %s", coverage.Covered, coverage.Total, quotedList(coverage.Uncovered))
}

// copiesNote is, for a change at root whose delta specs (specFiles' list)
// a test names as coverage says, how many scenarios not yet named are
// copies (copiedScenarios) and how many the change's own, as `jhon status`
// says it; "" when no copy is unnamed. A name a copy shares with a scenario
// of the change's own is the change's own.
func copiesNote(root string, deltas [][2]string, coverage Coverage) string {
	c, err := requirements.Delta()
	if err != nil || len(coverage.Uncovered) == 0 {
		return ""
	}
	copies := copiedScenarios(root, deltas, c)
	if len(copies) == 0 {
		return ""
	}
	var copied, own []string
	for _, f := range deltas {
		for _, s := range scenariosOf(f[0], f[1], 0, c) {
			if copies[s.key()] {
				copied = append(copied, s.name)
			} else {
				own = append(own, s.name)
			}
		}
	}
	var unnamed []string
	for _, n := range coverage.Uncovered {
		if slices.Contains(copied, n) && !slices.Contains(own, n) {
			unnamed = append(unnamed, n)
		}
	}
	if len(unnamed) == 0 {
		return ""
	}
	return fmt.Sprintf("of the scenarios not yet named by a test, copied unchanged from the main spec into MODIFIED blocks: %d (%s); the change's own: %d. A slice commit never asks a test to name a copy; delivery does",
		len(unnamed), quotedList(unnamed), len(coverage.Uncovered)-len(unnamed))
}

// quotedList is names quoted, comma-separated, as the coverage messages list them.
func quotedList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = strconv.Quote(n)
	}
	return strings.Join(quoted, ", ")
}
