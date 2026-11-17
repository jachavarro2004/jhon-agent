package audit

import (
	"os"
	"testing"
)

// BenchmarkAudit audits the session JHON_AUDIT_BENCH names, so a real
// session's cost can be profiled (`go test -run X -bench Audit -cpuprofile`);
// skipped without one.
func BenchmarkAudit(b *testing.B) {
	path := os.Getenv("JHON_AUDIT_BENCH")
	if path == "" {
		b.Skip("JHON_AUDIT_BENCH names no session")
	}
	for b.Loop() {
		Audit(path).Report()
	}
}
