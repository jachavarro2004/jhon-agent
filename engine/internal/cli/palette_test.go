package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func capture(t *testing.T, argv []string) (int, string) {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	done := make(chan []byte)
	go func() { b, _ := io.ReadAll(r); done <- b }()
	code, err := Run(argv)
	os.Stdout = old
	w.Close()
	b := <-done
	if err != nil {
		t.Fatal(err)
	}
	return code, string(b)
}

// `jhon palette` prints rose ink from the desktop palette (S29), on fixed
// surfaces (L66): --css the
// dashboard's :root, --pi the theme, --write-pi writes the theme file.
func TestPaletteCommand(t *testing.T) {
	colors := filepath.Join(t.TempDir(), "colors.json")
	os.WriteFile(colors, []byte(`{"surfaceContainer":"#101010","primary":"#abcdef"}`), 0o600)
	t.Setenv("JHON_DESKTOP_COLORS", colors)
	followDesktop(t)
	if code, out := capture(t, []string{"palette", "--css"}); code != 0 || !strings.Contains(out, "--bg:#1f1f28;") || !strings.Contains(out, "--rosa:#abcdef;") {
		t.Errorf("--css: %d %q", code, out)
	}
	if code, out := capture(t, []string{"palette", "--pi"}); code != 0 || !strings.Contains(out, `"name": "rose-ink"`) {
		t.Errorf("--pi: %d %q", code, out)
	}
	if code, out := capture(t, []string{"palette", "--json"}); code != 0 || !strings.Contains(out, `"accent":"#abcdef"`) || !strings.Contains(out, `"desktop":true`) {
		t.Errorf("--json: %d %q", code, out)
	}
	dir := t.TempDir()
	if code, out := capture(t, []string{"palette", "--write-pi", dir}); code != 0 || !strings.Contains(out, "wrote") {
		t.Errorf("--write-pi: %d %q", code, out)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "themes", "rose-ink.json")); err != nil || !strings.Contains(string(b), "#abcdef") {
		t.Errorf("theme file: %v %q", err, b)
	}
	if code, out := capture(t, []string{"palette", "--write-pi", dir}); code != 0 || !strings.Contains(out, "unchanged") {
		t.Errorf("a second write is a no-op: %d %q", code, out)
	}
	if code, _ := capture(t, []string{"palette", "--bogus"}); code != 2 {
		t.Errorf("unknown flag: %d, want 2", code)
	}
}

// followDesktop points JHON_SETTINGS at settings that follow the desktop
// palette, as the owner's do (L199): following one is opt-in (T55).
func followDesktop(t *testing.T) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(p, []byte("[integrations.theme]\npalette = \"ryoku\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JHON_SETTINGS", p)
}
