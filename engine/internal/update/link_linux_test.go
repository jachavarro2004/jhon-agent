//go:build linux && (amd64 || arm64)

package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T247 (odd/tasks/phase1-security.md S4): what was swapped in at the link
// and could not be put back is named where it now is: the temporary name
// the swap left it at.
func TestAFailedSwapBackNamesWhereItIs(t *testing.T) {
	tool, vs := studioTool(t), virtual(t)
	r, c, p := installed01(t, vs)
	r.releases = append([]toolRelease{signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ""), "")}, r.releases...)
	hookBeforeLink = func() {
		os.Remove(p.Link)
		os.WriteFile(p.Link, []byte("mine"), 0o644)
	}
	// Once the swap put jhon's new link at the name, it is taken away: the
	// swap back has nothing to swap with.
	hookBeforeSwapBack = func() { os.Remove(p.Link) }
	t.Cleanup(func() { hookBeforeLink, hookBeforeSwapBack = nil, nil })
	tmp := filepath.Join(filepath.Dir(p.Link), fmt.Sprintf(".jhon-studio.jhon-%d.tmp", os.Getpid()))
	_, err := c.InstallTool(context.Background(), tool, p, "0.5.0", false)
	want := "jhon-studio 0.2.0 is at " + filepath.Join(p.Dir, "0.2.0") + " but not linked: " + p.Link +
		" was replaced during the install and could not be put back: it is at " + tmp + " (nothing to swap with)"
	if err == nil || err.Error() != want {
		t.Errorf("error = %v\nwant %s", err, want)
	}
	if b, err := os.ReadFile(tmp); err != nil || string(b) != "mine" {
		t.Errorf("what was swapped in: %q, %v", b, err)
	}
}

// T247's verify probe: a swap back the folder's permissions refuse names
// where what was swapped in now is, too.
func TestASwapBackRefusedByPermissionsNamesWhereItIs(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a read-only folder")
	}
	tool, vs := studioTool(t), virtual(t)
	r, c, p := installed01(t, vs)
	r.releases = append([]toolRelease{signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ""), "")}, r.releases...)
	bin := filepath.Dir(p.Link)
	hookBeforeLink = func() {
		os.Remove(p.Link)
		os.WriteFile(p.Link, []byte("mine"), 0o644)
	}
	hookBeforeSwapBack = func() { os.Chmod(bin, 0o555) }
	t.Cleanup(func() {
		hookBeforeLink, hookBeforeSwapBack = nil, nil
		os.Chmod(bin, 0o755)
	})
	_, err := c.InstallTool(context.Background(), tool, p, "0.5.0", false)
	tmp := filepath.Join(bin, fmt.Sprintf(".jhon-studio.jhon-%d.tmp", os.Getpid()))
	if err == nil || !strings.HasSuffix(err.Error(), " was replaced during the install and could not be put back: it is at "+tmp+" (permission denied)") {
		t.Errorf("error = %v", err)
	}
	if b, _ := os.ReadFile(tmp); string(b) != "mine" {
		t.Errorf("%s holds %q", tmp, b)
	}
}
