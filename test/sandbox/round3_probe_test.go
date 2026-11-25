package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T239 round-3 verify (studio-integration-v1 L30, T249): an allow_write folder
// outside the home, inside another writable area, whose parent a run replaced
// with a link to the home, must not open ~/<rest> (here ~/out).
func TestAnAllowWriteParentLinkedToTheHomeDoesNotOpenTheHome(t *testing.T) {
	b := newBox(t)
	aw, out := filepath.Join(b.base, "aw"), filepath.Join(b.home, "out")
	for _, d := range []string{filepath.Join(aw, "build", "out"), out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\""+aw+"\", \""+filepath.Join(aw, "build", "out")+"\"]\n")
	r := b.sh(t, b.proj, "", `rm -r "`+filepath.Join(aw, "build")+`" && ln -s "$HOME" "`+filepath.Join(aw, "build")+`" && echo planted`)
	if r.Code != 0 || r.Stdout != "planted\n" {
		t.Fatalf("run 1: exit %d, stdout %q, stderr %q; want the link planted", r.Code, r.Stdout, r.Stderr)
	}
	r = b.sh(t, b.proj, "", `echo x > "$HOME/out/owned"`)
	if exists(filepath.Join(out, "owned")) {
		t.Errorf("run 2 wrote ~/out through a link to the home planted on the allow_write folder's way (exit %d, stderr %q)", r.Code, r.Stderr)
	}
}

// T249: ~/project/sub-worktrees, a root of the nested repository
// ~/project/sub, planted by a run in ~/project as a link.
func TestAWorktreesRootPlantedAsALinkDoesNotOpenWhereItLeads(t *testing.T) {
	b := newBox(t)
	units, sub := filepath.Join(b.home, ".config", "systemd", "user"), filepath.Join(b.proj, "sub")
	for _, d := range []string{units, sub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	support.GitRepo(t, sub)
	bashrc := filepath.Join(b.home, ".bashrc")
	if err := os.WriteFile(bashrc, []byte("# owner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := b.sh(t, b.proj, "", `ln -s "$HOME/.config/systemd/user" sub-worktrees && echo planted`)
	if r.Code != 0 || r.Stdout != "planted\n" {
		t.Fatalf("run 1: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	r = b.sh(t, sub, "", `touch "$HOME/.config/systemd/user/evil.service"`)
	if exists(filepath.Join(units, "evil.service")) {
		t.Errorf("a run in ~/project/sub wrote ~/.config/systemd/user through the planted sub-worktrees link (exit %d, stderr %q)", r.Code, r.Stderr)
	}
	if r = b.sh(t, b.proj, "", `rm sub-worktrees && ln -s "$HOME/.bashrc" sub-worktrees`); r.Code != 0 {
		t.Fatalf("run 3: exit %d, stderr %q", r.Code, r.Stderr)
	}
	r = b.sh(t, sub, "", `echo PWNED >> "$HOME/.bashrc"`)
	if got := support.Read(t, bashrc); got != "# owner\n" {
		t.Errorf("a run in ~/project/sub wrote ~/.bashrc through the planted sub-worktrees link: %q (exit %d, stderr %q)", got, r.Code, r.Stderr)
	}
}

// T249: a project cloned into $TMPDIR plants $TMPDIR/clone-worktrees as a link.
func TestAProjectInTMPDIRCannotPlantItsWorktreesRoot(t *testing.T) {
	b := newBox(t)
	units, clone := filepath.Join(b.home, ".config", "systemd", "user"), filepath.Join(b.tmp, "clone")
	for _, d := range []string{units, clone} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	support.GitRepo(t, clone)
	if r := b.sh(t, clone, "", `ln -s "$HOME/.config/systemd/user" "$TMPDIR/clone-worktrees" && echo planted`); r.Code != 0 || r.Stdout != "planted\n" {
		t.Fatalf("run 1: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	r := b.sh(t, clone, "", `touch "$HOME/.config/systemd/user/evil.service"`)
	if exists(filepath.Join(units, "evil.service")) {
		t.Errorf("run 2 wrote ~/.config/systemd/user through the planted clone-worktrees link (exit %d, stderr %q)", r.Code, r.Stderr)
	}
}

// T249: a run repoints ~/.cache/jhon/install.json at a folder of its own;
// allow_write ~/Projects must still be refused (126) for the real checkout
// under it, and the checkout stays the owner's.
func TestARepointedInstallRecordDoesNotUnprotectTheSourceCheckout(t *testing.T) {
	b := newBox(t)
	// Locate reads the record only for a runtime with no .git and no JHON_SOURCE.
	runtime := filepath.Join(b.base, "runtime")
	source := filepath.Join(b.home, "Projects", "personal", "jhon")
	main := filepath.Join(source, "engine", "cmd", "jhon", "main.go")
	record := filepath.Join(b.home, ".cache", "jhon", "install.json")
	for _, d := range []string{runtime, filepath.Dir(main), filepath.Dir(record)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(main, []byte("owner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte(`{"installed_at":"2026-11-01T00:00:00Z","commit":"abc","source":"`+source+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	b.env = append(b.env, "JHON_ROOT="+runtime, "XDG_STATE_HOME="+filepath.Join(b.base, "state"))
	b.settings(t, "schema = 1\n")
	b.sh(t, b.proj, "", `mkdir -p evil && printf '{"installed_at":"2026-11-01T00:00:00Z","commit":"abc","source":"%s"}' "$PWD/evil" > "$HOME/.cache/jhon/install.json"`)
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/Projects\"]\n")
	r := b.sh(t, b.proj, "", `cd "$HOME/Projects" && mv personal personal.old && mkdir -p personal/jhon/engine/cmd/jhon && echo EVIL > personal/jhon/engine/cmd/jhon/main.go`)
	if got := support.Read(t, main); got != "owner\n" {
		t.Errorf("Jhon's source checkout now holds %q (exit %d, stderr %q)", got, r.Code, r.Stderr)
	}
	if r.Code != 126 || !strings.Contains(r.Stderr, "holds Jhon's source checkout") {
		t.Errorf("exit %d, stderr %q; want the run refused (126) for the source checkout", r.Code, r.Stderr)
	}
}
