package compose

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/requirements"
)

func TestMain(m *testing.M) {
	root, _ := filepath.Abs("../../..")
	os.Setenv("JHON_ROOT", root)
	os.Exit(m.Run())
}

var fixtures = filepath.Join("..", "..", "..", "test", "testdata", "compose")

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mainOf(c string) string {
	if p := filepath.Join(fixtures, c, "main.md"); exists(p) {
		return p
	}
	return filepath.Join(fixtures, "base-main.md")
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func contract(t *testing.T) requirements.DeltaContract {
	t.Helper()
	c, err := requirements.Delta()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// stage is a temporary copy of a case's main spec and delta.
func stage(t *testing.T, c string) (dir, main, delta string) {
	t.Helper()
	dir = t.TempDir()
	main, delta = filepath.Join(dir, "spec.md"), filepath.Join(dir, "delta.md")
	os.WriteFile(main, []byte(read(t, mainOf(c))), 0o644)
	os.WriteFile(delta, []byte(read(t, filepath.Join(fixtures, c, "delta.md"))), 0o644)
	return dir, main, delta
}

func TestComposeMatchesTheHandWrittenBytes(t *testing.T) {
	for _, c := range []string{
		"added", "modified", "removed", "renamed", "combined", "preserve", "crlf", "no-trailing-newline", "fenced-depth",
		"depth2-added", "depth2-modified", "depth2-combined",
	} {
		got, err := Compose(read(t, mainOf(c)), read(t, filepath.Join(fixtures, c, "delta.md")), contract(t), Roles)
		if err != nil {
			t.Errorf("%s: %v", c, err)
		} else if want := read(t, filepath.Join(fixtures, c, "expected.md")); got != want {
			t.Errorf("%s:\n%s\nwant\n%s", c, got, want)
		}
	}
}

func TestComposeFileWrites(t *testing.T) {
	_, main, delta := stage(t, "combined")
	os.Chmod(main, 0o600)
	if _, err := ComposeFile(main, delta, OS); err != nil {
		t.Fatal(err)
	}
	if read(t, main) != read(t, filepath.Join(fixtures, "combined", "expected.md")) {
		t.Error("composed bytes differ from expected.md")
	}
	if info, _ := os.Stat(main); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want the main spec's own 0600", info.Mode().Perm())
	}
}

// refuses runs ComposeFile on a staged case and checks the refusal and that
// the main spec is unchanged.
func refuses(t *testing.T, c, why string) {
	t.Helper()
	_, main, delta := stage(t, c)
	before := read(t, main)
	_, err := ComposeFile(main, delta, OS)
	var r *Refusal
	if !errors.As(err, &r) || !regexp.MustCompile(why).MatchString(err.Error()) {
		t.Errorf("%s: %v, want a refusal matching %s", c, err, why)
	}
	if read(t, main) != before {
		t.Errorf("%s: main spec bytes changed", c)
	}
}

func TestRefusals(t *testing.T) {
	for c, why := range map[string]string{
		"refuse-unknown":           `^MODIFIED "Password Reset".*no such requirement`,
		"refuse-removed-no-reason": `REMOVED "Logout".*\(Reason: …\)`,
		"refuse-renamed-no-reason": `RENAMED "Logout → Sign Out".*\(Reason: …\)`,
		"refuse-renamed-malformed": `RENAMED "Logout to Sign Out".*<old> → <new>`,
		"refuse-added-duplicate":   `ADDED "Refresh".*more than once`,
		"refuse-added-exists":      `ADDED "Login".*already`,
		"refuse-main-duplicate":    `main spec "Login".*lines 9 and 19`,
		"refuse-outside-section":   `delta "Orphan".*outside`,
		"refuse-removed-content":   `REMOVED "Logout".*carries content`,
	} {
		refuses(t, c, why)
	}
}

func TestDepthRefusals(t *testing.T) {
	for _, c := range []struct {
		name, file, why string
	}{
		{"refuse-depth-requirements-mixed", "spec.md", `line 9.*level 2.*level 3.*Requirement headings in one main spec share one depth`},
		{"refuse-depth-scenario", "spec.md", `line 17 is a level 3 Scenario.*Scenario headings sit one level below them, at level 4`},
		{"refuse-depth-delta", "delta.md", `line 9.*level 3.*level 4.*heading depth compose does not support`},
	} {
		refuses(t, c.name, regexp.QuoteMeta(c.file)+`".*`+c.why)
	}
}

const add = "# Delta\n\n## ADDED Requirements\n\n### Requirement: B\n\nB.\n\n#### Scenario: b\n\n- THEN b\n"

func TestMainSpecVariants(t *testing.T) {
	for _, row := range []struct{ label, main, want string }{
		{"depth 2", "# S\n\n## Requirement: A\n\nA.\n\n### Scenario: a\n\n- THEN a\n",
			"# S\n\n## Requirement: A\n\nA.\n\n### Scenario: a\n\n- THEN a\n\n## Requirement: B\n\nB.\n\n### Scenario: b\n\n- THEN b\n"},
		{"depth 3", "# S\n\n## Requirements\n\n### Requirement: A\n\nA.\n\n#### Scenario: a\n\n- THEN a\n",
			"# S\n\n## Requirements\n\n### Requirement: A\n\nA.\n\n#### Scenario: a\n\n- THEN a\n\n### Requirement: B\n\nB.\n\n#### Scenario: b\n\n- THEN b\n"},
		{"requirements section", "# S\n\n## Purpose\n\nP.\n\n## Requirements\n\n## Other\n\nO.\n",
			"# S\n\n## Purpose\n\nP.\n\n## Requirements\n\n### Requirement: B\n\nB.\n\n#### Scenario: b\n\n- THEN b\n\n## Other\n\nO.\n"},
		{"no section", "# S\n\n## Purpose\n\nP.\n",
			"# S\n\n## Purpose\n\nP.\n\n## Requirement: B\n\nB.\n\n### Scenario: b\n\n- THEN b\n"},
		{"mixed", "# S\n\n## Requirements\n\n### Requirement: A\n\n## Requirement: C\n",
			`main spec .*line 7 is a level 2 Requirement: heading.*level 3 \(line 5\).*share one depth`},
		{"scenario", "# S\n\n## Requirement: A\n\n#### Scenario: a\n",
			`main spec .*line 5 is a level 4 Scenario: heading.*level 3`},
		{"out of range", "# S\n\n#### Requirement: A\n",
			`main spec .*line 3 is a level 4 Requirement: heading.*level 2 or 3`},
		{"counterpart depth 3", "# S\n\n## Requirements\n\n### Requirement: Refund\n\nR.\n\n## Other\n\n### Requirement: Charge\n\nC.\n",
			"# S\n\n## Requirements\n\n### Requirement: Refund\n\nR.\n\n## Other\n\n### Requirement: Charge\n\nC.\n\n### Requirement: B\n\nB.\n\n#### Scenario: b\n\n- THEN b\n"},
		{"counterpart mixed", "# S\n\n## Requirements\n\n### Requirement: Refund\n\n## Requirement: Charge\n",
			`main spec .*line 7 is a level 2 Requirement: heading.*level 3 \(line 5\)`},
	} {
		got, err := Compose(row.main, add, contract(t), Roles)
		if strings.HasPrefix(row.want, "main spec") {
			var r *Refusal
			if !errors.As(err, &r) || !regexp.MustCompile(row.want).MatchString(err.Error()) {
				t.Errorf("%s: %v, want a refusal matching %s", row.label, err, row.want)
			}
		} else if err != nil || got != row.want {
			t.Errorf("%s: %q, %v", row.label, got, err)
		}
	}
}

func TestDepth2KeepsUntouchedBytes(t *testing.T) {
	main := read(t, filepath.Join(fixtures, "depth2-combined", "main.md"))
	got, err := Compose(main, read(t, filepath.Join(fixtures, "depth2-combined", "delta.md")), contract(t), Roles)
	if err != nil {
		t.Fatal(err)
	}
	if head := main[:strings.Index(main, "## Requirement: Charge")]; !strings.HasPrefix(got, head) {
		t.Error("bytes before the first touched block changed")
	}
	if tail := main[strings.Index(main, "## Notes"):]; !strings.HasSuffix(got, tail) {
		t.Error("bytes after the last touched block changed")
	}
	if !regexp.MustCompile(`(?m)^### Requirement: Example inside a fence$`).MatchString(got) {
		t.Error("a fenced heading in a delta block was re-levelled")
	}
	if regexp.MustCompile(`\((Previously|Reason|Migration):`).MatchString(got) {
		t.Error("delta metadata reached the main spec")
	}
}

func TestNoDeltaSection(t *testing.T) {
	_, err := Compose("# S\n", "# Delta\n\nnothing\n", contract(t), Roles)
	if err == nil || err.Error() != `delta "(file)": has no delta section; expected ## RENAMED Requirements, ## MODIFIED Requirements, ## REMOVED Requirements, ## ADDED Requirements` {
		t.Errorf("%v", err)
	}
}

func TestCrashBeforeRenameLeavesTheOriginal(t *testing.T) {
	dir, main, delta := stage(t, "combined")
	before := read(t, main)
	crash := IO{WriteFile: os.WriteFile, Rename: func(string, string) error { return errors.New("simulated crash before rename") }}
	if _, err := ComposeFile(main, delta, crash); err == nil || err.Error() != "simulated crash before rename" {
		t.Errorf("%v", err)
	}
	if read(t, main) != before {
		t.Error("main spec bytes changed")
	}
	entries, _ := os.ReadDir(dir)
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"delta.md", "spec.md"}) {
		t.Errorf("temporary file left behind: %v", names)
	}
}

func TestRefusesAbsentMainAndInvalidUTF8(t *testing.T) {
	dir, main, delta := stage(t, "added")
	absent := filepath.Join(dir, "absent.md")
	if _, err := ComposeFile(absent, delta, OS); err == nil || !strings.Contains(err.Error(), "does not exist") || exists(absent) {
		t.Errorf("absent main spec: %v", err)
	}
	os.WriteFile(main, append([]byte(read(t, main)), 0xff, '\n'), 0o644)
	before := read(t, main)
	if _, err := ComposeFile(main, delta, OS); err == nil || !strings.Contains(err.Error(), "UTF-8") || read(t, main) != before {
		t.Errorf("invalid UTF-8: %v", err)
	}
}
