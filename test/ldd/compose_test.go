// `jhon compose <main-spec> <delta-spec>`, run as the binary. Composition
// itself, against the hand-written fixtures under test/testdata/compose/, is
// unit-tested in internal/compose. Refusals run the CLI against a temporary
// copy and assert that the main spec's bytes did not change.
package ldd

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func mainOf(c string) string {
	if p := support.Fixture("compose", c, "main.md"); exists(p) {
		return p
	}
	return support.Fixture("compose", "base-main.md")
}

type staged struct{ dir, main, delta string }

// stage is a temporary copy of a case's main spec and delta, for the CLI to
// mutate.
func stage(t *testing.T, c string) staged {
	t.Helper()
	dir := temp(t)
	s := staged{dir, filepath.Join(dir, "spec.md"), filepath.Join(dir, "delta.md")}
	support.Write(t, dir, map[string]string{
		"spec.md":  support.Read(t, mainOf(c)),
		"delta.md": support.Read(t, support.Fixture("compose", c, "delta.md")),
	})
	return s
}

func compose(t *testing.T, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, "", support.Env(), append([]string{"compose"}, args...)...)
}

func TestComposeWrites(t *testing.T) {
	t.Parallel()
	s := stage(t, "combined")
	r := compose(t, s.main, s.delta)
	equal(t, r.Code, 0, r.Stderr)
	equal(t, support.Read(t, s.main) == support.Read(t, support.Fixture("compose", "combined", "expected.md")), true, "composed spec differs from expected.md")
}

func TestComposeRefusals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, message string }{
		{"refuse-unknown", `MODIFIED "Password Reset".*no such requirement`},
		{"refuse-removed-no-reason", `REMOVED "Logout".*\(Reason: …\)`},
		{"refuse-renamed-no-reason", `RENAMED "Logout → Sign Out".*\(Reason: …\)`},
		{"refuse-renamed-malformed", `RENAMED "Logout to Sign Out".*<old> → <new>`},
		{"refuse-added-duplicate", `ADDED "Refresh".*more than once`},
		{"refuse-added-exists", `ADDED "Login".*already`},
		{"refuse-main-duplicate", `main spec "Login".*lines 9 and 19`},
		{"refuse-outside-section", `delta "Orphan".*outside`},
		{"refuse-removed-content", `REMOVED "Logout".*carries content`},
	} {
		t.Run("jhon compose refuses "+c.name+": nonzero exit, stderr names it, main unchanged", func(t *testing.T) {
			t.Parallel()
			s := stage(t, c.name)
			before := support.Read(t, s.main)
			r := compose(t, s.main, s.delta)
			if r.Code == 0 {
				t.Error("exit 0")
			}
			match(t, r.Stderr, c.message)
			equal(t, support.Read(t, s.main) == before, true, "main spec bytes changed")
		})
	}
}

// A main spec has one of two structural variants, requirement depth 2 or 3,
// detected per file (contracts/spec-delta.md, "Main specification structural
// variants"). A main spec that fits neither is refused before anything runs;
// a delta keeps its fixed depth and is refused at any other.
// 521ae75 refused every main spec not at depth 3: its refuse-depth-added and
// refuse-depth-modified cases are depth-2 specs and now compose (depth2-*);
// refuse-depth-mixed is a scenario one level off and stays refused
// (refuse-depth-scenario).
func TestComposeDepthRefusals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, file            string
		line, found, expected int
		why                   string
	}{
		{"refuse-depth-requirements-mixed", "spec.md", 9, 2, 3, `Requirement headings in one main spec share one depth`},
		{"refuse-depth-scenario", "spec.md", 17, 3, 4, `Scenario headings sit one level below`},
		{"refuse-depth-delta", "delta.md", 9, 3, 4, `heading depth compose does not support`},
	} {
		t.Run("jhon compose refuses "+c.name+": the heading depth is named, main unchanged", func(t *testing.T) {
			t.Parallel()
			s := stage(t, c.name)
			before := support.Read(t, s.main)
			r := compose(t, s.main, s.delta)
			if r.Code == 0 {
				t.Error("exit 0")
			}
			path := regexp.QuoteMeta(filepath.Join(s.dir, c.file))
			match(t, r.Stderr, path+`.*line `+strconv.Itoa(c.line)+`.*level `+strconv.Itoa(c.found)+`.*level `+strconv.Itoa(c.expected))
			match(t, r.Stderr, c.why)
			noMatch(t, r.Stderr, `no such requirement`)
			equal(t, support.Read(t, s.main) == before, true, "main spec bytes changed")
		})
	}
}

func TestComposeMissingMain(t *testing.T) {
	t.Parallel()
	s := stage(t, "added")
	absent := filepath.Join(s.dir, "absent.md")
	r := compose(t, absent, s.delta)
	if r.Code == 0 {
		t.Error("exit 0")
	}
	match(t, r.Stderr, `does not exist`)
	equal(t, exists(absent), false)
}

func TestComposeInvalidUTF8(t *testing.T) {
	t.Parallel()
	s := stage(t, "added")
	if err := os.WriteFile(s.main, append([]byte(support.Read(t, s.main)), 0xff, 0x0a), 0o666); err != nil {
		t.Fatal(err)
	}
	before := support.Read(t, s.main)
	r := compose(t, s.main, s.delta)
	if r.Code == 0 {
		t.Error("exit 0")
	}
	match(t, r.Stderr, `UTF-8`)
	equal(t, support.Read(t, s.main) == before, true, "main spec bytes changed")
}
