// Tracked files name no other framework's commands, agents or skills (T61,
// L482): foreignName, as a word or the start of one, appears nowhere git tracks
// except go.sum, whose hashes are not names. The scan reads from the
// repository root.
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

// foreignName is built in parts so this file does not match itself.
var foreignName = regexp.MustCompile(`(?i)\b` + "s" + `dd`)

func TestTrackedFilesNameNoForeignFramework(t *testing.T) {
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
		if filepath.Base(f) == "go.sum" {
			continue
		}
		// A tracked symlink's content, as git stores it, is its target.
		if fi, err := os.Lstat(filepath.Join(root, f)); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filepath.Join(root, f))
			if err != nil {
				t.Fatalf("readlink %s: %v", f, err)
			}
			if foreignName.MatchString(target) || foreignName.MatchString(f) {
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
		if foreignName.Match(data) || foreignName.MatchString(f) {
			bad = append(bad, f)
		}
	}
	if len(bad) > 0 {
		t.Errorf("tracked files name another framework (%s):\n  %s", foreignName, strings.Join(bad, "\n  "))
	}
}
