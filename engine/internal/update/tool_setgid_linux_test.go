//go:build linux

package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// T247's verify (odd/tasks/phase1-security.md T4): a setgid ~/.local/share
// (chmod g+s, a Debian SETGID_HOME) or a setgid TMPDIR passes the setgid
// bit to every folder made inside; that bit gives no access, so the staged
// copy and the one unpacked in TMPDIR keep one digest and the install is
// verified.
func TestInstallToolUnderASetgidFolder(t *testing.T) {
	tool, vs := studioTool(t), virtual(t)
	for _, where := range []string{"share", "tmpdir"} {
		t.Run(where, func(t *testing.T) {
			r := &repo{}
			c := r.serve(t, vs)
			p := home(t, tool)
			dir := filepath.Dir(p.Dir)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if where == "tmpdir" {
				dir = t.TempDir()
				t.Setenv("TMPDIR", dir)
			}
			if err := os.Chmod(dir, 0o755|os.ModeSetgid); err != nil {
				t.Fatal(err)
			}
			r.releases = []toolRelease{signedTool(t, vs, "0.1.0", studioTar(t, "0.1.0", ""), "")}
			if res, err := c.InstallTool(context.Background(), tool, p, "0.5.0", false); err != nil || res.Status != ToolInstalled {
				t.Fatalf("result = %+v, %v", res, err)
			}
			if in := tool.Installed(p); in.Active != "0.1.0" || !in.Verified {
				t.Errorf("Installed = %+v", in)
			}
		})
	}
}
