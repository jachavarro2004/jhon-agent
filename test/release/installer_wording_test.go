package release

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// The public installers and their README promise nothing a released version
// already broke: no "comes in vX.Y" and no old version names outside the
// install examples (T30).
func TestPublicInstallersPromiseNothingStale(t *testing.T) {
	stale := regexp.MustCompile(`(?i)comes in|\bv0\.[0-9]`)
	// The repository root's installers and the v0.3.0 bridge's copies (T18).
	for _, name := range []string{"install.sh", "install.ps1", "dist/public/install.sh", "dist/public/install.ps1", "dist/public/README.md"} {
		b, err := os.ReadFile(filepath.Join(support.Root(), filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if stale.MatchString(line) {
				t.Errorf("%s:%d promises a version: %s", name, i+1, strings.TrimSpace(line))
			}
		}
	}
}
