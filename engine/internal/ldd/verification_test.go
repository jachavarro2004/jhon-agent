package ldd

import (
	"os"
	"path/filepath"
	"testing"
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

// closed is a Judgment Day record closed APPROVED, which verifies nothing
// since Judgment Day was removed (T172).
const closed = "# Judgment\n\n## OPEN\n\n```json\n{}\n```\n\n## CLOSE\n\n```json\n{\"verdict\": \"APPROVED\"}\n```\n"

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
	if v := IndependentVerification(root, "c"); v != nil {
		t.Errorf("none on record: %+v", v)
	}
	write(t, filepath.Join(dir, "verify-report.md"), "verdict: fail\n")
	write(t, filepath.Join(dir, "judgments", "a.md"), closed)
	if v := IndependentVerification(root, "c"); v != nil {
		t.Errorf("a failing report, or a judgments/ record, verifies nothing: %+v", v)
	}
	write(t, filepath.Join(dir, "verify-report.md"), "**Verdict**: PASS\n")
	if v := IndependentVerification(root, "c"); v == nil || *v != (Verification{"verify-report.md (verdict: pass)", filepath.Join(dir, "verify-report.md")}) {
		t.Errorf("a passing report: %+v", v)
	}
}
