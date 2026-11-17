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
	// The repository root's installers and its README, the user manual.
	for _, name := range []string{"install.sh", "install.ps1", "README.md"} {
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
