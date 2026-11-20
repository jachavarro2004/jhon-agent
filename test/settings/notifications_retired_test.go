package settings

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T169 (L1438): the shell has no notification sounds. Every
// shell.notifications key is retired as shell.editor.vim was: a file that
// holds one is read, never refused; list says once per key that it is no
// longer read and why; set exits 2 saying it was removed in v0.4; migrate
// drops them, sections and all, with a backup.
const soundsWhy = "the notification sounds were removed from the shell"

func TestTheRetiredNotificationKeysEndToEnd(t *testing.T) {
	h := newHome(t)
	old := "schema = 1\n\n[shell.notifications]\nenabled = false # mine\n\n[shell.notifications.audio.events]\n\"agent.failed\" = \"none\"\n\n[integrations]\ntelemetry = false\n"
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": old})
	r := h.jhon(t, "list")
	if r.Code != 0 {
		t.Fatalf("a file with shell.notifications.enabled must still load: %d\n%s", r.Code, r.Out())
	}
	for _, line := range []string{"shell.notifications.enabled = false is no longer read: " + soundsWhy, `shell.notifications.audio.events."agent.failed" = "none" is no longer read: ` + soundsWhy} {
		if n := strings.Count(r.Stderr, line); n != 1 {
			t.Errorf("list warns %d times, want once: %q\n%s", n, line, r.Stderr)
		}
	}
	if strings.Contains(r.Stdout, "shell.notifications") {
		t.Errorf("list shows a retired key as a setting:\n%s", r.Stdout)
	}
	if support.Read(t, h.file()) != old {
		t.Fatal("list rewrote the owner's file")
	}
	for _, args := range [][]string{{"get", "shell.notifications.enabled"}, {"set", "shell.notifications.enabled", "true"}} {
		if r := h.jhon(t, args...); r.Code != 2 || !strings.Contains(r.Stderr, "shell.notifications.enabled: removed in v0.4: "+soundsWhy) {
			t.Errorf("%s: %d %q, want 2 and removed in v0.4", strings.Join(args, " "), r.Code, r.Out())
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
	if r := h.jhon(t, "list"); strings.Contains(r.Stderr, "notifications") {
		t.Errorf("list after migrate still warns: %q", r.Stderr)
	}
}
