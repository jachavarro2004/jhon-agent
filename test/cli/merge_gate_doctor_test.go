// `jhon doctor` says when a repository under LDD has no merge gate in git
// (T219 S6): git then does not refuse a move of the default branch that no
// ready merge check covers. The row is a warning with the fix.
package cli

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestDoctorNamesAMissingMergeGate(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	missing := "  warn  merge gate: git in " + m.proj + " does not refuse a move of the default branch that no ready merge check covers — run 'jhon hook merge-gate --install' there (route-guard installs it on the next guarded command)\n"
	installed := "  ok    merge gate: git in " + m.proj + " refuses a move of the default branch that no ready merge check covers\n"
	doctor := func() string {
		t.Helper()
		r := support.Jhon(t, m.proj, m.env, "doctor")
		if !strings.Contains(r.Stdout, "discovery") {
			t.Fatalf("doctor: exit %d\n%s", r.Code, r.Out())
		}
		return r.Stdout
	}
	if out := doctor(); strings.Contains(out, "merge gate") {
		t.Errorf("a repository without LDD, doctor says:\n%s\nwant no merge gate row", out)
	}
	support.Write(t, m.proj, map[string]string{"openspec/config.yaml": "schema: spec-driven\n"})
	if out := doctor(); !strings.Contains(out, missing) {
		t.Errorf("under LDD with no gate, doctor says:\n%s\nwant the row %q", out, missing)
	}
	if r := support.Jhon(t, m.proj, m.env, "hook", "merge-gate", "--install"); r.Code != 0 {
		t.Fatalf("merge-gate --install: exit %d %s", r.Code, r.Out())
	}
	if out := doctor(); !strings.Contains(out, installed) {
		t.Errorf("with the gate installed, doctor says:\n%s\nwant the row %q", out, installed)
	}
}
