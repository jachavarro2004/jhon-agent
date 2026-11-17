// `jhon doctor --runtime <name>`, run as the binary: the runtime capability
// model (docket AG) as agents.yaml declares it. The matrix reader, its
// problems, the contract delivery budget, the report, and the declaration the
// render derives are unit-tested in internal/manifest.
package render

import (
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// doctor is `jhon doctor --runtime <args>` from this checkout (its JHON_ROOT),
// under an empty HOME.
func doctor(t *testing.T, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, repoRoot, support.Env("HOME="+t.TempDir(), "JHON_HOME=", "JHON_ROOT="+repoRoot, support.RuntimesPath(t)), append([]string{"doctor", "--runtime"}, args...)...)
}

func TestRuntimeDoctor(t *testing.T) {
	t.Parallel()

	t.Run("jhon doctor --runtime claude-code: the rules the spikes prove are enforced by their primitive; behavioural ones name their audit check", func(t *testing.T) {
		t.Parallel()
		r := doctor(t, "claude-code")
		wantCode(t, r, 0)
		out := r.Out()
		match(t, out, `(?m)^  enforced +6 rule\(s\)$`)
		for _, rule := range []string{"framework-boundary/no-framework-write", "safety/destructive-deny", "safety/outward-ask", "efficiency/writer-scope", "ldd/explore-scope"} {
			match(t, out, `(?m)^  enforced +`+regexp.QuoteMeta(rule)+` +by blocking-hook-agent$`)
		}
		match(t, out, `(?m)^  unsupported +ldd/slices$`)
		match(t, out, `(?m)^  enforced +top-level-tools/executor-never-calls +by agent-tool-allowlist$`)
		match(t, out, `(?m)^  verified +blocking-hook-agent +yes +2\.1\.310 +test/testdata/spikes/claude-code-plugin-agent-hooks/record\.json`)
		match(t, out, `(?m)^  verified +agent-tool-allowlist +yes +2\.1\.280 +test/testdata/spikes/claude-code-tool-allowlist/record\.json`)
		match(t, out, `(?m)^  behavioural +runtime-boundaries/same-effect +jhon audit \(refusal-bypass, pending-real-fixture\)$`)
		match(t, out, `(?m)^  unsupported +runtime-boundaries/stop$`)
		match(t, out, `(?m)^  behavioural +isolation/no-other-tree +jhon audit \(isolation-bypass, verified\)$`)
		match(t, out, `(?m)^  behavioural +ldd/red-first +jhon audit \(edit-before-red, archive-without-red, pending-real-fixture\)$`)
		match(t, out, `(?m)^  verified +named-subagents +yes +2\.1\.280 +test/testdata/audit/apply-tdd-evidence`)
		match(t, out, `(?m)^  ok +imports: the runtime takes imports, so no instruction budget binds the always-on bundle$`)
	})

	t.Run("jhon doctor --runtime pi: pi isolation is behavioural and pending real fixtures (pending_on)", func(t *testing.T) {
		t.Parallel()
		r := doctor(t, "pi")
		wantCode(t, r, 0)
		match(t, r.Out(), `(?m)^  behavioural +isolation/no-other-tree +jhon audit \(isolation-bypass, pending-real-fixture\)$`)
	})

	t.Run("jhon doctor --runtime with an unknown or missing name is a usage error", func(t *testing.T) {
		t.Parallel()
		r := doctor(t, "emacs")
		wantCode(t, r, 2)
		match(t, r.Out(), `(?m)unknown runtime 'emacs'; agents\.yaml declares claude-code, pi$`)
		wantCode(t, doctor(t), 2)
	})

	for _, dropped := range []string{"hermes", "opencode"} {
		t.Run("jhon doctor --runtime "+dropped+" is a usage error: "+dropped+" is not a runtime the matrix describes", func(t *testing.T) {
			t.Parallel()
			r := doctor(t, dropped)
			wantCode(t, r, 2)
			match(t, r.Out(), `unknown runtime '`+dropped+`'`)
		})
	}
}
