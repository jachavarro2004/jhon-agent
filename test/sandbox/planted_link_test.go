package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// L26 blocker (odd/tasks/studio-integration-v1.md): run 1 replaces a
// sandbox.allow_write entry in the project, and one in the scratch root,
// with links to ~/.bashrc and ~/.config/systemd/user; run 2 must find
// neither writable.
func TestAnAllowWriteEntryReplacedByALinkDoesNotOpenWhereItLeads(t *testing.T) {
	b := newBox(t)
	units := filepath.Join(b.home, ".config", "systemd", "user")
	if err := os.MkdirAll(units, 0o755); err != nil {
		t.Fatal(err)
	}
	bashrc := filepath.Join(b.home, ".bashrc")
	if err := os.WriteFile(bashrc, []byte("# owner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\""+filepath.Join(b.proj, "out")+"\", \""+filepath.Join(b.tmp, "aw")+"\"]\n")
	r := b.sh(t, b.proj, "", `ln -s "$HOME/.bashrc" "$HOME/project/out" && ln -s "$HOME/.config/systemd/user" "`+filepath.Join(b.tmp, "aw")+`" && echo planted`)
	if r.Code != 0 || r.Stdout != "planted\n" {
		t.Fatalf("run 1: exit %d, stdout %q, stderr %q; want the links planted", r.Code, r.Stdout, r.Stderr)
	}
	r = b.sh(t, b.proj, "", `echo PWNED >> "$HOME/.bashrc"; touch "$HOME/.config/systemd/user/evil.service"`)
	if got := support.Read(t, bashrc); got != "# owner\n" {
		t.Errorf("run 2 wrote ~/.bashrc through the planted link: %q (exit %d, stderr %q)", got, r.Code, r.Stderr)
	}
	if exists(filepath.Join(units, "evil.service")) {
		t.Errorf("run 2 wrote ~/.config/systemd/user through the planted link (exit %d, stderr %q)", r.Code, r.Stderr)
	}
}
