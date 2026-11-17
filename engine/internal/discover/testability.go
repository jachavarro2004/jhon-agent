package discover

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
)

// A pattern is one name pattern of a per-stack map; a trailing `/` names a directory.
type pattern struct {
	text string
	dir  bool
	re   *regexp.Regexp
}

// vocabulary is what discovery reads from contracts/vocabulary.yaml, on
// first use, so a broken vocabulary fails the command that needs it rather
// than every command of the binary.
type vocabulary struct {
	covered, notCovered, coverageUnknown string
	testable, exempt, unknown            string
	conventions, sources                 map[string][]pattern
}

var vocab = sync.OnceValue(func() vocabulary {
	c := contract.MustVocabulary("coverage_state", 3)
	t := contract.MustVocabulary("testability_state", 3)
	return vocabulary{
		covered: c[0], notCovered: c[1], coverageUnknown: c[2],
		testable: t[0], exempt: t[1], unknown: t[2],
		conventions: stackPatterns("testability_test_conventions"),
		sources:     stackPatterns("testability_stack_sources"),
	}
})

// Testable is the testability_state value of a project with a certain test command.
func Testable() string { return vocab().testable }

// Exempt is the testability_state value of a project with nothing to test (structurally_exempt).
func Exempt() string { return vocab().exempt }

// Unknown is the testability_state value of a project that may hold tests but has no certain command.
func Unknown() string { return vocab().unknown }

// stackPatterns is a per-stack pattern map from the contract, each pattern compiled to a name matcher.
func stackPatterns(key string) map[string][]pattern {
	stacks, lists := contract.Map(contract.MustContract("vocabulary.yaml"), key)
	if len(stacks) == 0 {
		panic(fmt.Errorf("contracts/vocabulary.yaml: %s declares no stack", key))
	}
	out := map[string][]pattern{}
	for _, stack := range stacks {
		for _, text := range contract.InlineList(lists[stack]) {
			name, dir := strings.CutSuffix(text, "/")
			out[stack] = append(out[stack], pattern{text, dir, glob(name)})
		}
	}
	return out
}

func find(ps []pattern, dir bool, name string) (pattern, bool) {
	for _, p := range ps {
		if p.dir == dir && p.re.MatchString(name) {
			return p, true
		}
	}
	return pattern{}, false
}

// testabilityHit is the first entry under the project that rules out the
// exemption: a test file or directory by convention, or a source file of one
// of its stacks; "" when none does. It walks the whole project, since a
// single stray file is enough, but never crosses what discovery excludes, a
// nested repository, or a nested discovered project, whose files are that
// project's to account for. Symlinked directories are not followed.
func testabilityHit(root string, p *Project, nested map[string]bool) string {
	v := vocab()
	var conventions, sources []pattern
	for _, s := range p.Stacks {
		conventions = append(conventions, v.conventions[s]...)
		sources = append(sources, v.sources[s]...)
	}
	var walk func(rel string) string
	walk = func(rel string) string {
		for _, e := range entries(filepath.Join(root, rel)) {
			child := e.Name()
			if rel != "" {
				child = rel + "/" + child
			}
			if e.IsDir() {
				if excluded[e.Name()] || nested[child] || isRepo(filepath.Join(root, child)) {
					continue
				}
				if t, ok := find(conventions, true, e.Name()); ok {
					return fmt.Sprintf("%s/ is a test directory by convention (%s)", child, t.text)
				}
				if deeper := walk(child); deeper != "" {
					return deeper
				}
				continue
			}
			if t, ok := find(conventions, false, e.Name()); ok {
				return fmt.Sprintf("%s is a test file by convention (%s)", child, t.text)
			}
			if s, ok := find(sources, false, e.Name()); ok {
				return fmt.Sprintf("%s is %s source (%s)", child, strings.Join(p.Stacks, "+"), s.text)
			}
		}
		return ""
	}
	base := p.Path
	if base == "." {
		base = ""
	}
	return walk(base)
}

// testability is a project's classification and why. A nil command with a
// reason means tests are configured but no one command is certain: that is
// unknown, never exempt, or any project lacking a command would slip out of
// the Strict TDD capability requirement.
func testability(root string, p *Project, all []*Project) (state, reason string) {
	v := vocab()
	if p.TestCommand != nil {
		return v.testable, "test command " + *p.TestCommand
	}
	if p.TestCommandSource != nil {
		return v.unknown, "tests are configured but no command is certain: " + *p.TestCommandSource
	}
	nested := map[string]bool{}
	var order []string
	for _, o := range all {
		if o != p && o.Path != "." && (p.Path == "." || strings.HasPrefix(o.Path, p.Path+"/")) {
			nested[o.Path] = true
			order = append(order, o.Path)
		}
	}
	if hit := testabilityHit(root, p, nested); hit != "" {
		return v.unknown, "no test command, but " + hit
	}
	skipped := ""
	if len(order) > 0 {
		skipped = ", outside " + strings.Join(order, ", ")
	}
	return v.exempt, fmt.Sprintf("no test command, no test configuration, and no %s test or source file%s", strings.Join(p.Stacks, "+"), skipped)
}

// IsTestPath is whether a repository path is a test by the stacks'
// conventions (testability_test_conventions): its file name, or any
// directory on it.
func IsTestPath(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, ps := range vocab().conventions {
		if _, ok := find(ps, false, parts[len(parts)-1]); ok {
			return true
		}
		for _, d := range parts[:len(parts)-1] {
			if _, ok := find(ps, true, d); ok {
				return true
			}
		}
	}
	return false
}

// SkipDir is whether a directory is one discovery never enters.
func SkipDir(name string) bool { return excluded[name] }
