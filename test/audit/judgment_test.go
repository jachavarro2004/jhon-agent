// `jhon judgment append|status`, run as the binary. The record encoding,
// status, and refusals are unit-tested in internal/judgment.
package audit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func judgmentFixture(rel ...string) string {
	return support.Fixture(append([]string{"judgment"}, rel...)...)
}

// steps is a case's numbered event files, in order.
func steps(t *testing.T, c string) []string {
	t.Helper()
	es, err := os.ReadDir(judgmentFixture(c))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		if regexp.MustCompile(`^\d\d-.*\.json$`).MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

// stage is a record path in the standalone location, inside a fresh temporary repository.
func stage(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docs", "judgments", "2026-09-22-cache-review.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func jhon(t *testing.T, args ...string) support.Result {
	t.Helper()
	return support.Run(t, support.Cmd{Args: append([]string{support.Bin(t)}, args...), Env: jhonEnv()})
}

// staged is a record path holding a case's events, each appended by `jhon judgment append`.
func staged(t *testing.T, c string) string {
	t.Helper()
	path := stage(t)
	for _, f := range steps(t, c) {
		r := jhon(t, "judgment", "append", path, judgmentFixture(c, f))
		equal(t, r.Code, 0, r.Stderr)
	}
	return path
}

func TestJudgment(t *testing.T) {
	t.Run("jhon judgment append writes the event; a refusal exits nonzero and leaves the bytes unchanged", func(t *testing.T) {
		t.Parallel()
		path := staged(t, "refuse-adopt-without-proof")
		before := support.Read(t, path)
		r := jhon(t, "judgment", "append", path, judgmentFixture("refuse-adopt-without-proof", "refused.json"))
		if r.Code == 0 {
			t.Fatal("the refusal exited 0")
		}
		match(t, r.Stderr, `own proof`)
		equal(t, support.Read(t, path), before)
	})

	t.Run("jhon judgment append reads the event from stdin with -", func(t *testing.T) {
		t.Parallel()
		path := stage(t)
		r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "judgment", "append", path, "-"}, Env: jhonEnv(),
			Stdin: support.Read(t, judgmentFixture("defect-close", "01-open.json"))})
		equal(t, r.Code, 0, r.Stderr)
		match(t, support.Read(t, path), "^## OPEN\n")
	})

	t.Run("jhon judgment append refuses a record outside the contract's record locations", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "notes", "review.md")
		r := jhon(t, "judgment", "append", path, judgmentFixture("defect-close", "01-open.json"))
		if r.Code == 0 {
			t.Fatal("the append exited 0")
		}
		match(t, r.Stderr, `judgments`)
		if _, err := os.Stat(path); err == nil {
			t.Fatal("the record was written")
		}
	})

	t.Run("jhon judgment status prints the state, and --json the same state as data", func(t *testing.T) {
		t.Parallel()
		path := staged(t, "no-progress")
		human := jhon(t, "judgment", "status", path)
		equal(t, human.Code, 0, human.Stderr)
		match(t, human.Stdout, `next\s+escalate \(no-progress\)`)
		js := jhon(t, "judgment", "status", path, "--json")
		equal(t, js.Code, 0, js.Stderr)
		s := parse(t, js.Stdout)
		equal(t, s["nextAction"], any("escalate"))
		equal(t, s["reason"], any("no-progress"))
	})

	t.Run("jhon judgment status reports a modified record as invalid and exits nonzero", func(t *testing.T) {
		t.Parallel()
		path := staged(t, "refuse-modify-prior")
		modified := strings.Replace(support.Read(t, path), "evict() drops the newest entry", "evict() drops the oldest entry", 1)
		if err := os.WriteFile(path, []byte(modified), 0o644); err != nil {
			t.Fatal(err)
		}
		r := jhon(t, "judgment", "status", path)
		if r.Code == 0 {
			t.Fatal("status exited 0")
		}
		match(t, r.Stderr, `prev`)
	})
}
