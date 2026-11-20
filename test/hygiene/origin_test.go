// Tracked files name no old origin or company (T175, L1452): Jhon is a
// personal project whose only home is github.com/jachavarro2004/jhon-agent,
// so the earlier organization's handle, in either spelling, appears nowhere
// git tracks: not in a file's text, its path or a symlink's target. The scan
// reads from the repository root.
package hygiene

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// oldOrigin is built in parts so this file does not match itself: the
// handle and its spaced form, in any case.
var oldOrigin = regexp.MustCompile(`(?i)` + "s" + `d[ ]?` + "soft" + `ware`)

func TestTrackedFilesNameNoOldOrigin(t *testing.T) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	root := strings.TrimSpace(string(out))
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	files := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	if len(files) < 10 {
		t.Fatalf("git ls-files listed %d files, want the tree", len(files))
	}
	var bad []string
	for _, f := range files {
		if fi, err := os.Lstat(filepath.Join(root, f)); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filepath.Join(root, f))
			if err != nil {
				t.Fatalf("readlink %s: %v", f, err)
			}
			if oldOrigin.MatchString(target) || oldOrigin.MatchString(f) {
				bad = append(bad, f)
			}
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			if os.IsNotExist(err) {
				continue // deleted in the working tree, not yet committed
			}
			t.Fatalf("read %s: %v", f, err)
		}
		// A binary file (media, fonts) names nothing: its bytes match by chance.
		if bytes.IndexByte(data, 0) >= 0 {
			data = nil
		}
		if oldOrigin.Match(data) || oldOrigin.MatchString(f) {
			bad = append(bad, f)
		}
	}
	if len(bad) > 0 {
		t.Errorf("tracked files name the old origin (%s); point to github.com/jachavarro2004/jhon-agent:\n  %s", oldOrigin, strings.Join(bad, "\n  "))
	}
}
