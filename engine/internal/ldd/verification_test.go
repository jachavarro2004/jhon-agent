package ldd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/judgment"
)

func TestReportVerdict(t *testing.T) {
	for src, want := range map[string]string{
		"# Verify\n\nverdict: fail\n":                     "fail",
		"# Verify\r\n\r\n**Verdict**: PASS WITH WARNINGS": "pass",
		"- __verdict__ : `Pass`":                          "pass",
		"verdict: passing":                                "",
		"the verdict: pass, in prose":                     "",
		"verdict: ſass":                                   "",
		"\u00a0verdict:\u3000fail\nverdict: pass":         "fail",
	} {
		if got := ReportVerdict(src); got != want {
			t.Errorf("ReportVerdict(%q) = %q, want %q", src, got, want)
		}
	}
}

const closed = "# Judgment\n\n## OPEN\n\n```json\n{}\n```\n\n## CLOSE\n\n```json\n{\"verdict\": \"APPROVED\"}\n```\n"

func TestClosedVerdict(t *testing.T) {
	if got := ClosedVerdict(closed); got != "APPROVED" {
		t.Errorf("ClosedVerdict = %q", got)
	}
	for _, src := range []string{closed[:len("# Judgment\n\n## OPEN")], "## CLOSE\n```json\nnot json\n```", "## CLOSE\n```json\n{\"verdict\": 1}\n```", "## CLOSE\n```json\nnull\n```"} {
		if got := ClosedVerdict(src); got != "" {
			t.Errorf("ClosedVerdict(%q) = %q", src, got)
		}
	}
}

// The audit's light reading finds the CLOSE verdict the full validation does.
func TestClosedVerdictOfAJudgmentRecord(t *testing.T) {
	deliverEnv(t)
	fix := filepath.Join("..", "..", "..", "test", "testdata", "judgment", "defect-close")
	entries, err := os.ReadDir(fix)
	if err != nil {
		t.Fatal(err)
	}
	rec := ""
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(fix, e.Name()))
		if rec, err = judgment.AppendEvent(rec, string(b)); err != nil {
			t.Fatal(err)
		}
	}
	if got := ClosedVerdict(rec); got != "APPROVED" {
		t.Errorf("ClosedVerdict = %q, want APPROVED", got)
	}
	if v, err := judgment.VerdictOf(rec); v != "APPROVED" || err != nil {
		t.Errorf("VerdictOf = %q, %v", v, err)
	}
	if got := ClosedVerdict(rec[:strings.Index(rec, "## CLOSE")]); got != "" {
		t.Errorf("ClosedVerdict of an open record = %q", got)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIndependentVerification(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "openspec", "changes", "c")
	if v, err := IndependentVerification(root, "c", nil); v != nil || err != nil {
		t.Errorf("none on record: %+v, %v", v, err)
	}
	write(t, filepath.Join(dir, "verify-report.md"), "verdict: fail\n")
	write(t, filepath.Join(dir, "judgments", "a.md"), closed)
	write(t, filepath.Join(dir, "judgments", "b.md"), "invalid")
	write(t, filepath.Join(dir, "judgments", "c.txt"), closed)
	v, err := IndependentVerification(root, "c", nil)
	if err != nil || v == nil || *v != (Verification{"judgments/a.md (APPROVED)", filepath.Join(dir, "judgments", "a.md")}) {
		t.Errorf("a judgment closed APPROVED: %+v, %v", v, err)
	}
	refuse := func(string) (string, error) { return "", errors.New("invalid record") }
	if v, _ := IndependentVerification(root, "c", refuse); v != nil {
		t.Errorf("a record the validation refuses verifies nothing: %+v", v)
	}
	write(t, filepath.Join(dir, "verify-report.md"), "**Verdict**: PASS\n")
	if v, _ := IndependentVerification(root, "c", refuse); v == nil || v.By != "verify-report.md (verdict: pass)" {
		t.Errorf("a passing report: %+v", v)
	}
}
