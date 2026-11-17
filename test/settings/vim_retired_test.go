package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T29 S3 (L243): the shell has no vim editor. shell.editor.vim is retired
// as integrations.herdr was: a file that holds it is read, never refused;
// list says once that it is no longer read and why; get and set exit 2
// saying it was removed in v0.2; migrate drops it with a backup.
const vimWhy = "the vim editor was removed from the shell"

func TestTheRetiredVimKeyEndToEnd(t *testing.T) {
	h := newHome(t)
	old := "schema = 1\n\n[shell.editor]\nvim = true # mine\n\n[integrations]\ntelemetry = false\n"
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": old})
	r := h.jhon(t, "list")
	if r.Code != 0 {
		t.Fatalf("a file with shell.editor.vim must still load: %d\n%s", r.Code, r.Out())
	}
	if n := strings.Count(r.Stderr, "shell.editor.vim = "); n != 1 || !strings.Contains(r.Stderr, "shell.editor.vim = true is no longer read: "+vimWhy) {
		t.Errorf("list warns %d times, want once with the why:\n%s", n, r.Stderr)
	}
	if strings.Contains(r.Stdout, "shell.editor.vim") {
		t.Errorf("list shows the retired key as a setting:\n%s", r.Stdout)
	}
	if support.Read(t, h.file()) != old {
		t.Fatal("list rewrote the owner's file")
	}
	for _, args := range [][]string{{"get", "shell.editor.vim"}, {"set", "shell.editor.vim", "true"}} {
		if r := h.jhon(t, args...); r.Code != 2 || !strings.Contains(r.Stderr, "shell.editor.vim: removed in v0.2: "+vimWhy) {
			t.Errorf("%s: %d %q, want 2 and removed in v0.2", strings.Join(args, " "), r.Code, r.Out())
		}
	}
	if support.Read(t, h.file()) != old {
		t.Fatal("a refused set wrote the file")
	}
	if r := h.jhon(t, "migrate"); r.Code != 0 || !strings.Contains(r.Stdout, "backup:") {
		t.Fatalf("migrate: %d\n%s", r.Code, r.Out())
	}
	backups, _ := filepath.Glob(filepath.Join(h.config, "jhon", "settings-backup-*", "settings.toml"))
	if len(backups) != 1 || support.Read(t, backups[0]) != old {
		t.Fatalf("backups = %v, want one copy of the file as it was", backups)
	}
	want := "schema = 1\n\n[integrations]\ntelemetry = false\n"
	if now := support.Read(t, h.file()); now != want {
		t.Errorf("after migrate:\n%s\nwant:\n%s", now, want)
	}
	if r := h.jhon(t, "list"); strings.Contains(r.Stderr, "vim") {
		t.Errorf("list after migrate still warns: %q", r.Stderr)
	}
}

// An old shell's vim.json is not carried into settings.toml by the
// first-run migration: it would write the retired key.
func TestTheMigrationNeverWritesTheVimKey(t *testing.T) {
	h := newHome(t)
	shellHome(t, h)
	r := support.Jhon(t, h.dir, h.env, "settings", "list")
	if strings.Contains(r.Stderr, "vim") {
		t.Errorf("list after migrating an old shell names vim: %q", r.Stderr)
	}
	b, err := os.ReadFile(h.file())
	if err != nil {
		t.Fatalf("no settings.toml after the migration: %v (exit %d, %s)", err, r.Code, r.Out())
	}
	if strings.Contains(string(b), "vim") {
		t.Errorf("settings.toml names vim:\n%s", b)
	}
}
