package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T24b (L291, L304): herdr is no longer part of Jhon. Its three keys are
// retired as integrations.dashboard was: a file that holds them is read,
// never refused; list says once per key that it is no longer read and why;
// get and set exit 2 saying it was removed; migrate drops them with a backup.
const herdrWhy = "herdr is no longer part of Jhon; use any multiplexer, or none"

var herdrKeys = []string{"integrations.herdr", "integrations.herdr_bin", "integrations.herdr_plugin_dir"}

func TestTheRetiredHerdrKeysEndToEnd(t *testing.T) {
	h := newHome(t)
	old := "schema = 1\n\n[integrations]\nherdr = true # mine\nherdr_bin = \"~/.local/bin/herdr-jhon\"\nherdr_plugin_dir = \"~/.config/herdr/plugins/local/jhon-sidebar\"\ntelemetry = false\n"
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": old})
	r := h.jhon(t, "list")
	if r.Code != 0 {
		t.Fatalf("a file with the herdr keys must still load: %d\n%s", r.Code, r.Out())
	}
	for _, key := range herdrKeys {
		if n := strings.Count(r.Stderr, key+" = "); n != 1 {
			t.Errorf("list warns about %s %d times, want once:\n%s", key, n, r.Stderr)
		}
		if strings.Contains(r.Stdout, key) {
			t.Errorf("list shows the retired %s as a setting:\n%s", key, r.Stdout)
		}
	}
	for _, line := range []string{
		"integrations.herdr = true is no longer read: " + herdrWhy,
		`integrations.herdr_bin = "~/.local/bin/herdr-jhon" is no longer read: ` + herdrWhy,
		`integrations.herdr_plugin_dir = "~/.config/herdr/plugins/local/jhon-sidebar" is no longer read: ` + herdrWhy,
	} {
		if !strings.Contains(r.Stderr, line) {
			t.Errorf("list does not say %q:\n%s", line, r.Stderr)
		}
	}
	if support.Read(t, h.file()) != old {
		t.Fatal("list rewrote the owner's file")
	}
	for _, key := range herdrKeys {
		for _, args := range [][]string{{"get", key}, {"set", key, "true"}} {
			if r := h.jhon(t, args...); r.Code != 2 || !strings.Contains(r.Stderr, key+": removed in v0.2: "+herdrWhy) {
				t.Errorf("%s: %d %q, want 2 and removed in v0.2", strings.Join(args, " "), r.Code, r.Out())
			}
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
	if r := h.jhon(t, "list"); strings.Contains(r.Stderr, "herdr") {
		t.Errorf("list after migrate still warns: %q", r.Stderr)
	}
}

// JHON_HERDR and HERDR_JHON_BIN mean nothing now: list never names them.
func TestTheHerdrVariablesAreNotRead(t *testing.T) {
	h := newHome(t, "JHON_HERDR=1", "HERDR_JHON_BIN=/opt/herdr")
	r := h.jhon(t, "list")
	if r.Code != 0 || strings.Contains(r.Out(), "herdr") || strings.Contains(r.Out(), "HERDR") {
		t.Errorf("list with the old herdr variables: %d\n%s", r.Code, r.Out())
	}
}

// A machine that once ran herdr (its install state and binary present) no
// longer gets integrations.herdr from the first-run migration, nor from sync.
func TestTheMigrationNoLongerWritesHerdr(t *testing.T) {
	for _, cmd := range [][]string{{"settings", "list"}, {"settings", "migrate"}, {"sync"}} {
		t.Run(strings.Join(cmd, " "), func(t *testing.T) {
			h := newHome(t)
			shellHome(t, h)
			support.Write(t, filepath.Join(h.dir, ".local", "share", "jhon"), map[string]string{"herdr.json": "{}\n"})
			support.Write(t, filepath.Join(h.dir, ".local", "bin"), map[string]string{"herdr-jhon": "#!/bin/sh\n"})
			r := support.Jhon(t, h.dir, h.env, cmd...)
			b, err := os.ReadFile(h.file())
			if err != nil {
				t.Fatalf("no settings.toml after %v: %v (exit %d, %s)", cmd, err, r.Code, r.Out())
			}
			if strings.Contains(string(b), "herdr") {
				t.Errorf("settings.toml after %v names herdr:\n%s", cmd, b)
			}
		})
	}
}
