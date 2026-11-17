package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T29 E6 (L329): the legacy layer of the Ryoku palette goes; the Ryoku
// option, integrations.theme.palette = "ryoku", stays as it is.

const followWhy = `the palette is integrations.theme.palette now: set it to "desktop" to follow the desktop palette`

// follow_desktop and source are still read with a note, never refused, but
// no longer pick the palette; migrate drops them with a backup.
func TestTheRetiredThemeKeysNoLongerPickThePalette(t *testing.T) {
	h := newHome(t)
	support.Write(t, filepath.Join(h.dir, ".cache", "ryoku"), map[string]string{"colors.json": `{"primary":"#aabbcc"}`})
	old := "schema = 1\n\n[integrations.theme]\nfollow_desktop = true # mine\nsource = \"ryoku\"\n"
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": old})
	r := h.jhon(t, "list")
	if r.Code != 0 {
		t.Fatalf("a file with the old theme keys must still load: %d\n%s", r.Code, r.Out())
	}
	for _, line := range []string{
		"integrations.theme.follow_desktop = true is no longer read: " + followWhy,
		`integrations.theme.source = "ryoku" is no longer read: the palette names its source: integrations.theme.palette = "desktop"`,
	} {
		if strings.Count(r.Stderr, line) != 1 {
			t.Errorf("list does not say once %q:\n%s", line, r.Stderr)
		}
	}
	if got, _ := find(list(t, h, h.dir), "integrations.theme.palette"); got.Value != "rose-ink" || got.Source != "default" {
		t.Errorf("palette = %#v, want rose-ink (default): the retired keys pick nothing", got)
	}
	if p := paletteJSON(t, h); p["desktop"] != false || p["accent"] != "#e7bbc8" {
		t.Errorf("palette --json follows the desktop for a retired key: %v", p)
	}
	if support.Read(t, h.file()) != old {
		t.Fatal("list rewrote the owner's file")
	}
	if r := h.jhon(t, "migrate"); r.Code != 0 || !strings.Contains(r.Stdout, "backup:") {
		t.Fatalf("migrate: %d\n%s", r.Code, r.Out())
	}
	backups, _ := filepath.Glob(filepath.Join(h.config, "jhon", "settings-backup-*", "settings.toml"))
	if len(backups) != 1 || support.Read(t, backups[0]) != old {
		t.Fatalf("backups = %v, want one copy of the file as it was", backups)
	}
	if now := support.Read(t, h.file()); now != "schema = 1\n" {
		t.Errorf("after migrate:\n%s", now)
	}
}

// The older name of the desktop palette still works: palette = "ryoku"
// reads as "desktop" and follows the desktop palette file (T50f).
func TestThePaletteRyokuStillFollowsTheDesktop(t *testing.T) {
	h := newHome(t)
	support.Write(t, filepath.Join(h.dir, ".cache", "ryoku"), map[string]string{"colors.json": `{"primary":"#aabbcc"}`})
	withSettings(t, h, ownerSettings)
	if r := h.jhon(t, "get", "integrations.theme.palette"); r.Code != 0 || r.Stdout != "desktop\n" || r.Stderr != "" {
		t.Errorf("get: %d %q", r.Code, r.Out())
	}
	if p := paletteJSON(t, h); p["desktop"] != true || p["accent"] != "#aabbcc" {
		t.Errorf("palette = ryoku: %v", p)
	}
}

// JHON_RYOKU_COLORS, the name before T59, is no longer read; the current
// JHON_DESKTOP_COLORS still names the desktop palette.
func TestJHONRyokuColorsIsIgnored(t *testing.T) {
	colors := filepath.Join(t.TempDir(), "colors.json")
	if err := os.WriteFile(colors, []byte(`{"primary":"#112233"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name    string
		follows bool
	}{{"JHON_RYOKU_COLORS", false}, {"JHON_DESKTOP_COLORS", true}} {
		t.Run(c.name, func(t *testing.T) {
			h := newHome(t, c.name+"="+colors)
			withSettings(t, h, ownerSettings)
			p := paletteJSON(t, h)
			want, accent := false, "#e7bbc8"
			if c.follows {
				want, accent = true, "#112233"
			}
			if p["desktop"] != want || p["accent"] != accent {
				t.Errorf("%s: desktop %v accent %v, want %v %s", c.name, p["desktop"], p["accent"], want, accent)
			}
		})
	}
}

// The pre-v0.1 jhon-shell/*.json files are no longer migrated: the
// first-run migration reads profiles.json alone, and a desktop palette on
// the machine no longer writes palette = "ryoku".
func TestTheOldShellFilesAreNotMigrated(t *testing.T) {
	h := newHome(t)
	shellHome(t, h)
	support.Write(t, filepath.Join(h.dir, ".cache", "ryoku"), map[string]string{"colors.json": "{}\n"})
	support.Jhon(t, h.dir, h.env, "settings", "list")
	text := support.Read(t, h.file())
	if !strings.Contains(text, "[pi]\nprofile = \"default\"\n") {
		t.Errorf("profiles.json was not migrated:\n%s", text)
	}
	for _, gone := range []string{"[shell", "palette", "animations", "cards"} {
		if strings.Contains(text, gone) {
			t.Errorf("settings.toml holds %q from the old shell files:\n%s", gone, text)
		}
	}
	if old, _ := filepath.Glob(filepath.Join(h.config, "jhon", "settings-backup-*", "jhon-shell")); len(old) != 0 {
		t.Errorf("the old shell files were backed up: %v", old)
	}
}

// migrate no longer reads the shell's old JSON files, and help says so.
func TestMigrateHelpNoLongerNamesTheShellFiles(t *testing.T) {
	h := newHome(t)
	r := support.Jhon(t, h.dir, h.env, "help")
	if out := r.Out(); !strings.Contains(out, "jhon settings migrate") || strings.Contains(out, "shell's JSON files") {
		t.Errorf("help must describe migrate without the removed jhon-shell/*.json move:\n%s", out)
	}
}
