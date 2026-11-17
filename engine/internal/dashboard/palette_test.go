package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/palette"
)

// The self-contained page inlines rose ink after app.css, so the palette
// wins over the stylesheet's fallback (S29).
func TestPageInlinesThePalette(t *testing.T) {
	colors := filepath.Join(t.TempDir(), "colors.json")
	os.WriteFile(colors, []byte(`{"primary":"#101010"}`), 0o600)
	t.Setenv("JHON_DESKTOP_COLORS", colors)
	followDesktop(t)
	assets := t.TempDir()
	os.WriteFile(filepath.Join(assets, "index.html"), []byte("<head><link rel=\"stylesheet\" href=\"app.css\">\n<link rel=\"stylesheet\" href=\"palette.css\">\n</head><script type=\"module\" src=\"app.js\"></script>"), 0o600)
	os.WriteFile(filepath.Join(assets, "app.css"), []byte(":root{--rosa:#d27e99}"), 0o600)
	os.WriteFile(filepath.Join(assets, "app.js"), []byte(""), 0o600)
	page, err := Page(assets, map[string]any{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(page, "--rosa:#d27e99"), strings.Index(page, "--rosa:#101010;")
	if i < 0 || j < 0 || j < i {
		t.Errorf("the palette follows app.css: %d %d\n%s", i, j, page)
	}
}

// The server answers /palette.css from the desktop palette on every request.
func TestServerAnswersThePalette(t *testing.T) {
	colors := filepath.Join(t.TempDir(), "colors.json")
	os.WriteFile(colors, []byte(`{"primary":"#101010"}`), 0o600)
	t.Setenv("JHON_DESKTOP_COLORS", colors)
	followDesktop(t)
	srv := httptest.NewServer(Server(projects(t), nil, t.TempDir(), nil))
	defer srv.Close()
	get := func() (int, string, string) {
		res, err := http.Get(srv.URL + "/palette.css")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return res.StatusCode, res.Header.Get("Content-Type"), string(b)
	}
	if code, typ, body := get(); code != 200 || !strings.HasPrefix(typ, "text/css") || !strings.Contains(body, "--rosa:#101010;") {
		t.Errorf("palette.css: %d %s %q", code, typ, body)
	}
	// The roles design/system.md names for headers and overlay frames (T17b).
	_, _, body := get()
	for _, role := range []string{"--accent-soft:" + palette.AccentSoft(palette.Palette{"primary": "#101010"}), "--frame-soft:#"} {
		if !strings.Contains(body, role) {
			t.Errorf("palette.css holds %s: %q", role, body)
		}
	}
	os.WriteFile(colors, []byte(`{"primary":"#202020"}`), 0o600)
	if _, _, body := get(); !strings.Contains(body, "--rosa:#202020;") {
		t.Errorf("a new wallpaper's palette is served at once: %q", body)
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
