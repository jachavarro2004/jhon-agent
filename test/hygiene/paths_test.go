// Tracked tests, fixtures and sources name no real person's home (T50a, F5):
// a fixture that needs a home uses /home/owner. Nor its dash-encoded form, the
// one a transcript directory's name carries (T175): a fixture uses -home-owner.
// The scan reads what git tracks under test/, engine/ and shell/, from the
// repository root.
package hygiene

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ownerHome is built in parts so this file does not match itself;
// ownerHomeDashed is it as a project directory's name encodes it.
var (
	ownerHome       = []byte("/home/" + "jchavarro")
	ownerHomeDashed = bytes.ReplaceAll(ownerHome, []byte("/"), []byte("-"))
)

func TestTrackedFilesNameNoOwnerHome(t *testing.T) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	root := strings.TrimSpace(string(out))
	cmd := exec.Command("git", "ls-files", "-z", "--", "test", "engine", "shell")
	cmd.Dir = root
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	files := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	if len(files) < 10 {
		t.Fatalf("git ls-files listed %d files, want the tree", len(files))
	}
	var bad, dashed []string
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			if os.IsNotExist(err) {
				continue // deleted in the working tree, not yet committed
			}
			t.Fatalf("read %s: %v", f, err)
		}
		if bytes.Contains(data, ownerHome) {
			bad = append(bad, f)
		}
		if bytes.Contains(data, ownerHomeDashed) {
			dashed = append(dashed, f)
		}
	}
	if len(bad) > 0 {
		t.Errorf("tracked files name %s (use /home/owner):\n  %s", ownerHome, strings.Join(bad, "\n  "))
	}
	if len(dashed) > 0 {
		t.Errorf("tracked files name %s (use -home-owner):\n  %s", ownerHomeDashed, strings.Join(dashed, "\n  "))
	}
}
