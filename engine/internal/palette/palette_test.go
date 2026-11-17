package palette

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "colors.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Rose ink is the desktop palette when one stands, each role read from it (S29).
func TestReadTakesTheDesktopRoles(t *testing.T) {
	p, desktop := Read(write(t, `{"surface":"#101010","primary":"#ABCDEF","onSurface":"#eeeeee"}`))
	if !desktop {
		t.Fatal("a readable colors.json is a desktop palette")
	}
	if p["surface"] != "#101010" || p["primary"] != "#abcdef" || p["onSurface"] != "#eeeeee" {
		t.Errorf("roles: %v", p)
	}
	if p["tertiary"] != Fallback["tertiary"] {
		t.Errorf("a missing role falls back: %q", p["tertiary"])
	}
}

// Without a desktop palette, rose ink is its own fixed values, never another theme.
func TestReadWithoutADesktopPaletteIsTheFallback(t *testing.T) {
	p, desktop := Read(filepath.Join(t.TempDir(), "absent.json"))
	if desktop {
		t.Error("an absent file is no desktop palette")
	}
	if p["surface"] != "#161313" || p["primary"] != "#e7bbc8" {
		t.Errorf("fallback: %v", p)
	}
	if bad, _ := Read(write(t, `not json`)); bad["primary"] != "#e7bbc8" {
		t.Error("an unreadable file falls back")
	}
}

// States keep distinct hues whatever the wallpaper (the reason Kanagawa
// replaced the desktop palette on 2026-10-29): warning is fixed amber, never the accent.
func TestStatesStayDistinct(t *testing.T) {
	p, _ := Read(write(t, `{"primary":"#c5c5d5","tertiary":"#c5c5d5","error":"#c5c5d5"}`))
	th := PiTheme(p)
	vars := th["vars"].(map[string]string)
	colors := th["colors"].(map[string]string)
	if vars[colors["warning"]] != Warn {
		t.Errorf("warning is %q, want the fixed amber %q", vars[colors["warning"]], Warn)
	}
	if vars[colors["success"]] == vars[colors["error"]] {
		t.Errorf("success and error share a color: %q", vars[colors["success"]])
	}
}

// The pi theme is complete for pi's schema and named rose-ink.
func TestPiThemeIsComplete(t *testing.T) {
	th := PiTheme(Fallback)
	if th["name"] != "rose-ink" || th["appearance"] != "dark" {
		t.Errorf("name/appearance: %v %v", th["name"], th["appearance"])
	}
	colors := th["colors"].(map[string]string)
	vars := th["vars"].(map[string]string)
	for _, c := range ThemeColors {
		v, ok := colors[c]
		if !ok {
			t.Errorf("missing color %s", c)
			continue
		}
		if _, ok := vars[v]; !ok {
			t.Errorf("color %s names an undefined var %s", c, v)
		}
	}
	if len(colors) != len(ThemeColors) {
		t.Errorf("colors: %d, want exactly %d", len(colors), len(ThemeColors))
	}
	text := PiThemeText(Fallback)
	var round map[string]any
	if json.Unmarshal([]byte(text), &round) != nil || !strings.HasSuffix(text, "}\n") {
		t.Errorf("theme text is not JSON with a final newline: %q", text[:40])
	}
}

// The dashboard's CSS defines every variable app.css and app.js use, from
// the palette, and no hue outside rose ink's roles (T17b: nothing reads
// --aqua, --violet, --orange or --blue).
func TestCSSDefinesTheDashboardVariables(t *testing.T) {
	css := CSS(Fallback)
	for _, v := range []string{"--rail", "--bg", "--panel", "--surface", "--surface-hi", "--sel", "--sel-edge", "--ink", "--ink2", "--soft", "--faint",
		"--line", "--line-strong", "--line-rosa", "--rosa", "--rosa-soft", "--rosa-fill", "--rosa-ink", "--broken", "--gold", "--ok", "--shadow"} {
		if !strings.Contains(css, v+":") {
			t.Errorf("CSS lacks %s", v)
		}
	}
	for _, v := range []string{"--aqua", "--violet", "--orange", "--blue"} {
		if strings.Contains(css, v+":") {
			t.Errorf("CSS still emits %s, which nothing reads", v)
		}
	}
	if !strings.HasPrefix(css, ":root{") || !strings.Contains(css, "--bg:#1f1f28") || !strings.Contains(css, "--rosa:#e7bbc8") {
		t.Errorf("CSS: %s", css)
	}
}

// The semantic tokens are what another program reads:
// surfaces, ink, accent and the states, from the palette.
func TestSemantic(t *testing.T) {
	s := Semantic(Fallback, true)
	want := map[string]string{"page": "#1f1f28", "rail": "#161618", "panel": "#161618", "card": "#2a2a37", "raise": "#363646", "hover": "#363646",
		"frame": "#161618", "section": "#1d1d20", "row": "#2b2b2e", "rowRaise": "#363639", "box": "#2a2a37", "boxStrong": "#363646",
		"ink": "#e9e0e1", "soft": "#d2c3c6", "faint": "#9b8d90", "line": "#4f4447", "accent": "#e7bbc8", "accentFill": "#53343f", "accentInk": "#f2c5d2",
		"ok": "#98bb6c", "bad": "#ffb4ab", "warn": Warn, "info": "#7aa89f", "tool": "#957fb8",
		"accentSoft": "#b594a0", "accentEdge": "#897179", "band": Band,
		// T16: other programs' popup frames, the overlay role of design/system.md.
		"frameSoft": "#7b6772"}
	for k, v := range want {
		if s[k] != v {
			t.Errorf("%s = %v, want %s", k, s[k], v)
		}
	}
	if s["desktop"] != true {
		t.Errorf("desktop = %v", s["desktop"])
	}
}

// One surface ladder (L66): the sidebar and its popups are one neutral gray;
// the pane and pi's shell a blue ink, its boxes lighter and still blue; the
// borders rose, from the palette's primary. No background is one of its
// near-black surface roles.
func TestSurfaceLadder(t *testing.T) {
	th := PiTheme(Fallback)
	vars := th["vars"].(map[string]string)
	colors := th["colors"].(map[string]string)
	export := th["export"].(map[string]string)
	css := CSS(Fallback)
	cssVar := func(v string) string {
		i := strings.Index(css, v+":")
		if i < 0 {
			t.Fatalf("CSS lacks %s", v)
		}
		return css[i+len(v)+1 : i+len(v)+8]
	}
	s := Semantic(Fallback, false)
	str := func(k string) string { v, _ := s[k].(string); return v }

	frame := str("frame")
	if s["rail"] != frame || s["panel"] != frame || cssVar("--rail") != frame || cssVar("--panel") != frame {
		t.Errorf("sidebar and popups differ: rail %v panel %v css %s %s, frame %s", s["rail"], s["panel"], cssVar("--rail"), cssVar("--panel"), frame)
	}
	// L71: everything another program's sidebar paints is a neutral gray, never the
	// page's or a box's blue.
	for _, k := range []string{"frame", "section", "row", "rowRaise"} {
		if c := str(k); c == "" || !neutral(c) {
			t.Errorf("%s %s is not a neutral gray", k, c)
		}
	}

	page := str("page")
	bluish := func(name, c string) {
		r, g, b := rgb(c)
		if b <= r+8 || b <= g+8 {
			t.Errorf("%s = %s is not bluish", name, c)
		}
	}
	bluish("page", page)
	if vars[export["pageBg"]] != page || cssVar("--bg") != page {
		t.Errorf("pi pageBg %s, css --bg %s, want the page %s", vars[export["pageBg"]], cssVar("--bg"), page)
	}

	box, strong := str("box"), str("boxStrong")
	for name, c := range map[string]string{"box": box, "boxStrong": strong} {
		bluish(name, c)
		if luminance(c) <= luminance(page) {
			t.Errorf("%s = %s is not lighter than the page %s", name, c, page)
		}
	}
	if luminance(strong) <= luminance(box) {
		t.Errorf("box strong %s is not lighter than the box %s", strong, box)
	}
	for _, c := range []string{"toolPendingBg", "toolSuccessBg"} {
		if vars[colors[c]] != box {
			t.Errorf("%s = %s, want the box %s", c, vars[colors[c]], box)
		}
	}
	// T34 (L98), T15.5 (R8): pi's notices, the rail and the status line sit on
	// the frame gray, darker than the page; the band var stays for the semantic set.
	if colors["customMessageBg"] != "railBand" || vars["railBand"] != str("frame") || vars["band"] != str("band") {
		t.Errorf("customMessageBg = %s (%s), want the rail band %s", colors["customMessageBg"], vars[colors["customMessageBg"]], str("frame"))
	}
	if vars[colors["userMessageBg"]] != strong || vars[export["infoBg"]] != strong || vars[export["cardBg"]] != box {
		t.Errorf("userMessageBg %s infoBg %s cardBg %s", vars[colors["userMessageBg"]], vars[export["infoBg"]], vars[export["cardBg"]])
	}
	if cssVar("--surface") != box || cssVar("--surface-hi") != strong || s["card"] != box || s["raise"] != strong || s["hover"] != strong {
		t.Errorf("box tokens: css %s %s, semantic %v %v %v", cssVar("--surface"), cssVar("--surface-hi"), s["card"], s["raise"], s["hover"])
	}
	if colors["selectedBg"] != "primaryContainer" || colors["toolErrorBg"] != "errorContainer" {
		t.Errorf("selectedBg %s toolErrorBg %s", colors["selectedBg"], colors["toolErrorBg"])
	}

	if colors["border"] != "frameSoft" || colors["borderAccent"] != "primary" {
		t.Errorf("border %s borderAccent %s, want frameSoft and primary", colors["border"], colors["borderAccent"])
	}
	if want := Mix(Fallback["primaryContainer"], page, 0.5); vars[colors["borderMuted"]] != want {
		t.Errorf("borderMuted = %s, want primaryContainer halfway to the page %s", vars[colors["borderMuted"]], want)
	}
	if !strings.Contains(css, "--line-rosa:color-mix(in srgb,"+Fallback["primary"]) || !strings.Contains(css, "--line-strong:color-mix(in srgb,"+Fallback["primary"]) {
		t.Errorf("dashboard lines do not lean on primary: %s", css)
	}

	never := map[string]bool{}
	for _, role := range []string{"surface", "surfaceContainerLowest", "surfaceContainerLow"} {
		never[role], never[Fallback[role]] = true, true
	}
	for name, v := range colors {
		if strings.HasSuffix(name, "Bg") && (never[v] || never[vars[v]]) {
			t.Errorf("colors.%s uses %s", name, v)
		}
	}
	for name, v := range export {
		if never[v] || never[vars[v]] {
			t.Errorf("export.%s uses %s", name, v)
		}
	}
	for _, v := range []string{"--rail", "--bg", "--panel", "--surface", "--surface-hi"} {
		if never[cssVar(v)] {
			t.Errorf("css %s uses %s", v, cssVar(v))
		}
	}
}

// Mix moves one color toward another in sRGB, rounding each channel.
// The band (T34, L98) is the Gentle Shell's darker band: the status line,
// the rail and pi's notices sit on it. It is halfway from the page to the
// frame: darker than the page, never darker than the frame, and a neutral
// gray like everything else darker than the page.
func TestBandIsDarkerThanThePage(t *testing.T) {
	if want := Mix(Page, Frame, 0.5); Band != want {
		t.Errorf("Band = %s, want Mix(Page, Frame, 0.5) = %s", Band, want)
	}
	if luminance(Band) >= luminance(Page) {
		t.Errorf("band %s is not darker than the page %s", Band, Page)
	}
	if luminance(Band) < luminance(Frame) {
		t.Errorf("band %s is darker than the frame %s", Band, Frame)
	}
	// The band is the page's family, a darker ink like the Gentle Shell's,
	// not one of the sidebar's neutral grays (L72): its blue tint sits
	// between none and the page's own.
	_, bg, bb := rgb(Band)
	_, pg, pb := rgb(Page)
	if tint := bb - bg; tint < 0 || tint > pb-pg {
		t.Errorf("band %s tint %d is outside the page's family (0..%d)", Band, tint, pb-pg)
	}
	vars := PiTheme(Fallback)["vars"].(map[string]string)
	if vars["band"] != Band {
		t.Errorf("pi theme var band = %s, want %s", vars["band"], Band)
	}
}

// Thinking text is dim (T34, L98): pi sets it in italic, rose ink in the dim role's color.
func TestThinkingIsDim(t *testing.T) {
	colors := PiTheme(Fallback)["colors"].(map[string]string)
	if colors["thinkingText"] != colors["dim"] {
		t.Errorf("thinkingText = %s, want the dim role's %s", colors["thinkingText"], colors["dim"])
	}
}

func TestMix(t *testing.T) {
	for _, c := range []struct {
		a, b string
		t    float64
		want string
	}{
		{"#000000", "#ffffff", 0, "#000000"},
		{"#000000", "#ffffff", 1, "#ffffff"},
		{"#000000", "#ffffff", 0.5, "#808080"},
		{"#53343f", "#24242f", 0.5, "#3c2c37"},
		{"#102030", "#102030", 0.3, "#102030"},
	} {
		if got := Mix(c.a, c.b, c.t); got != c.want {
			t.Errorf("Mix(%s, %s, %v) = %s, want %s", c.a, c.b, c.t, got, c.want)
		}
	}
}

// Text reads at WCAG AA (4.5:1): the ink on the strongest box and the muted
// ink on the page. A wallpaper whose onSurface fails on the strongest box
// gives way to Kanagawa's fujiWhite.
func TestTextContrast(t *testing.T) {
	th := PiTheme(Fallback)
	vars := th["vars"].(map[string]string)
	colors := th["colors"].(map[string]string)
	if vars[colors["text"]] != Fallback["onSurface"] {
		t.Errorf("text = %s, want onSurface %s", vars[colors["text"]], Fallback["onSurface"])
	}
	if c := contrast(vars[colors["text"]], BoxStrong); c < 4.5 {
		t.Errorf("text on box strong: %.2f:1", c)
	}
	if c := contrast(vars[colors["text"]], Row); c < 4.5 {
		t.Errorf("text on the sidebar's active row: %.2f:1", c)
	}
	if c := contrast(vars[colors["muted"]], Page); c < 4.5 {
		t.Errorf("muted on the page: %.2f:1", c)
	}
	if c := contrast("#000000", "#ffffff"); c < 20.9 || c > 21.1 {
		t.Errorf("black on white: %.2f:1, want 21", c)
	}
	dark, _ := Read(write(t, `{"onSurface":"#1d1b1c"}`))
	th = PiTheme(dark)
	vars, colors = th["vars"].(map[string]string), th["colors"].(map[string]string)
	if vars[colors["text"]] != "#dcd7ba" || vars[colors["userMessageText"]] != "#dcd7ba" || Semantic(dark, true)["ink"] != "#dcd7ba" ||
		!strings.Contains(CSS(dark), "--ink:#dcd7ba;") {
		t.Errorf("a dark onSurface on dark boxes: text %s, semantic ink %v", vars[colors["text"]], Semantic(dark, true)["ink"])
	}
}

func rgb(c string) (r, g, b int) {
	fmt.Sscanf(c, "#%02x%02x%02x", &r, &g, &b)
	return
}

// neutral is a gray: no channel more than 3 apart from the next.
func neutral(c string) bool {
	r, g, b := rgb(c)
	return abs(r-g) <= 3 && abs(g-b) <= 3
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// L68, L72: the pane is lighter than the sidebar, and each step of the
// ladder reads apart at dark-room thresholds: frame → page 1.08:1, frame →
// section band 1.05:1, section band → active row 1.15:1, and 1.12:1 for
// active row → raised row, page → box and box → box strong.
func TestStepsReadApart(t *testing.T) {
	steps := []struct {
		dark, light, name string
		min               float64
	}{
		{Frame, Page, "frame → page", 1.08},
		{Frame, Section, "frame → section", 1.05},
		{Section, Row, "section → active row", 1.15},
		{Row, RowRaise, "active row → raised row", 1.12},
		{Page, Box, "page → box", 1.12},
		{Box, BoxStrong, "box → box strong", 1.12},
	}
	for _, s := range steps {
		if c := contrast(s.light, s.dark); c < s.min || luminance(s.light) <= luminance(s.dark) {
			t.Errorf("%s: %s on %s is %.2f:1, want the lighter one at %.2f:1 or more", s.name, s.light, s.dark, c, s.min)
		}
	}
}

// L70: the rail cards (STATUS, TODOS, BACKGROUND) frame in borderAccent, a
// rose that reads at WCAG non-text contrast (3:1) on the page; the plain
// border stays primaryContainer for every other frame.
func TestRailCardBorderReadsOnThePage(t *testing.T) {
	th := PiTheme(Fallback)
	vars := th["vars"].(map[string]string)
	colors := th["colors"].(map[string]string)
	if c := contrast(vars[colors["borderAccent"]], Page); c < 3 {
		t.Errorf("borderAccent %s on the page %s: %.2f:1, want 3:1 or more", vars[colors["borderAccent"]], Page, c)
	}
}

// L79: a softer rose everywhere but at focus. accentSoft is about 75%
// primary toward the page and reads at WCAG AA (4.5:1) on it; card titles,
// markdown headings and bullets and message labels take it, while the
// accent, the active border and the selection keep full primary.
func TestAccentSoftIsTheRoseAwayFromFocus(t *testing.T) {
	th := PiTheme(Fallback)
	vars := th["vars"].(map[string]string)
	colors := th["colors"].(map[string]string)
	soft := vars["accentSoft"]
	if want := Mix(Fallback["primary"], Page, 0.25); soft != want || soft != "#b594a0" {
		t.Errorf("accentSoft = %s, want primary 75%% of the way (%s)", soft, want)
	}
	if c := contrast(soft, Page); c < 4.5 {
		t.Errorf("accentSoft %s on the page: %.2f:1, want 4.5:1 or more", soft, c)
	}
	for _, c := range []string{"mdHeading", "mdListBullet", "customMessageLabel"} {
		if colors[c] != "accentSoft" {
			t.Errorf("%s = %s, want accentSoft", c, colors[c])
		}
	}
	for _, c := range []string{"accent", "borderAccent"} {
		if colors[c] != "primary" {
			t.Errorf("%s = %s, focus keeps full primary", c, colors[c])
		}
	}
	if colors["selectedBg"] != "primaryContainer" {
		t.Errorf("selectedBg = %s", colors["selectedBg"])
	}
	s := Semantic(Fallback, true)
	if s["accentSoft"] != soft || s["accent"] != Fallback["primary"] {
		t.Errorf("semantic accentSoft %v accent %v", s["accentSoft"], s["accent"])
	}
	// The sidebar's edge: a muted rose, primary 45% of the way to the frame.
	if want := Mix(Fallback["primary"], Frame, 0.45); s["accentEdge"] != want {
		t.Errorf("accentEdge = %v, want %s", s["accentEdge"], want)
	}
	// A wallpaper whose softened primary no longer reads on the page keeps
	// the primary itself.
	dark, _ := Read(write(t, `{"primary":"#7a4a5a"}`))
	if got := PiTheme(dark)["vars"].(map[string]string)["accentSoft"]; got != "#7a4a5a" {
		t.Errorf("an unreadable soft rose: accentSoft = %s, want primary #7a4a5a", got)
	}
}

// L79: green and amber from one family. ok is Kanagawa's springGreen and
// warn its amber whatever the wallpaper; bad stays the palette's error, or the
// fallback's when the palette's reads like ok. ok reads at 4.5:1 on the page and
// apart from bad.
func TestStatesAreOneFamily(t *testing.T) {
	for _, body := range []string{`{}`, `{"tertiary":"#b6cdae","error":"#ffb4ab"}`, `{"tertiary":"#c5c5d5","error":"#c5c5d5"}`, `{"error":"#a0c070"}`} {
		p, _ := Read(write(t, body))
		ok, bad := States(p)
		if ok != "#98bb6c" {
			t.Errorf("%s: ok = %s, want springGreen #98bb6c", body, ok)
		}
		if c := contrast(ok, Page); c < 4.5 {
			t.Errorf("%s: ok on the page %.2f:1", body, c)
		}
		h1, _ := hsl(ok)
		h2, _ := hsl(bad)
		if d := math.Min(math.Abs(h1-h2), 360-math.Abs(h1-h2)); d < 40 || bad == ok {
			t.Errorf("%s: ok %s and bad %s are %.0f degrees apart", body, ok, bad, d)
		}
		th := PiTheme(p)
		vars, colors := th["vars"].(map[string]string), th["colors"].(map[string]string)
		if vars[colors["success"]] != OK || vars[colors["warning"]] != Warn || Semantic(p, true)["ok"] != OK || !strings.Contains(CSS(p), "--ok:#98bb6c;") {
			t.Errorf("%s: success %s warning %s", body, vars[colors["success"]], vars[colors["warning"]])
		}
	}
	p, _ := Read(write(t, `{"error":"#ff8080"}`))
	if _, bad := States(p); bad != "#ff8080" {
		t.Errorf("a distinct desktop error stays: %s", bad)
	}
	p, _ = Read(write(t, `{"error":"#a0c070"}`))
	if _, bad := States(p); bad != Fallback["error"] {
		t.Errorf("a green desktop error gives way to the fallback: %s", bad)
	}
}

// shellFallback is the line shell/lib/desktop-palette.ts embeds: Fallback, in
// Roles order, for the shell to fall back to when it reads the desktop palette itself.
func shellFallback() string {
	var b strings.Builder
	for i, r := range Roles {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: %q", r, Fallback[r])
	}
	return "export const FALLBACK: Record<string, string> = { " + b.String() + " };"
}

// The shell keeps no palette of its own (S30, L83): its fallback is this
// package's Fallback, generated here; the test fails when the two drift and
// prints the line to embed.
func TestShellFallbackIsTheEngines(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "shell", "lib", "desktop-palette.ts"))
	if err != nil {
		t.Fatal(err)
	}
	want := shellFallback()
	if n := strings.Count(string(src), "export const FALLBACK"); n != 1 || !strings.Contains(string(src), "\n"+want+"\n") {
		t.Errorf("shell/lib/desktop-palette.ts drifted from palette.Fallback (%d declarations); embed exactly:\n%s", n, want)
	}
}

// The desktop palette's path is JHON_DESKTOP_COLORS, else Ryoku's
// ~/.cache/ryoku/colors.json; JHON_RYOKU_COLORS, its name before T59, is no
// longer read (T29 E6, L329).
func TestPathTakesTheDesktopOverrideOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JHON_DESKTOP_COLORS", "")
	t.Setenv("JHON_RYOKU_COLORS", "")
	if got := Path(); got != filepath.Join(home, ".cache", "ryoku", "colors.json") {
		t.Errorf("no override: %s", got)
	}
	t.Setenv("JHON_RYOKU_COLORS", "/old.json")
	if got := Path(); got != filepath.Join(home, ".cache", "ryoku", "colors.json") {
		t.Errorf("the old name is no longer read: %s", got)
	}
	t.Setenv("JHON_DESKTOP_COLORS", "/new.json")
	if got := Path(); got != "/new.json" {
		t.Errorf("JHON_DESKTOP_COLORS wins: %s", got)
	}
}

// Following a desktop palette is opt-in (L197, T55): with
// integrations.theme.palette = "rose-ink" (the default; T15.7, L97) Current
// is rose ink's own values and reads no desktop palette, even one standing
// at Ryoku's path; "ryoku" is today's Read. A file from before, with
// follow_desktop and source, no longer follows the desktop (T29 E6).
func TestCurrentFollowsTheDesktopOnlyWhenTheSettingsSaySo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JHON_DESKTOP_COLORS", "")
	t.Setenv("JHON_RYOKU_COLORS", "")
	ryoku := filepath.Join(home, ".cache", "ryoku", "colors.json")
	if err := os.MkdirAll(filepath.Dir(ryoku), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ryoku, []byte(`{"primary":"#aabbcc"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(t.TempDir(), "settings.toml")
	t.Setenv("JHON_SETTINGS", settings)
	for _, text := range []string{"", "[integrations.theme]\nfollow_desktop = false\nsource = \"ryoku\"\n", "[integrations.theme]\npalette = \"rose-ink\"\n"} {
		if text != "" {
			if err := os.WriteFile(settings, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		p, desktop := Current()
		if desktop || p["primary"] != Fallback["primary"] || len(p) != len(Fallback) {
			t.Errorf("settings %q: desktop %v primary %s", text, desktop, p["primary"])
		}
		p["primary"] = "#000000"
		if Fallback["primary"] == "#000000" {
			t.Fatal("Current handed out Fallback itself")
		}
	}
	// herdr and dashboard are retired keys (T17b, T24b): still read, never refused.
	owner := "schema = 1\n\n[integrations]\nherdr = true\ndashboard = true\ntelemetry = true\n\n[integrations.theme]\nfollow_desktop = true\nsource = \"ryoku\"\n"
	if err := os.WriteFile(settings, []byte(owner), 0o600); err != nil {
		t.Fatal(err)
	}
	if p, desktop := Current(); desktop || p["primary"] != Fallback["primary"] {
		t.Errorf("the retired follow_desktop picks nothing: desktop %v primary %s", desktop, p["primary"])
	}
	if err := os.WriteFile(settings, []byte("schema = 1\n[integrations.theme]\npalette = \"ryoku\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if p, desktop := Current(); !desktop || p["primary"] != "#aabbcc" {
		t.Errorf("palette = ryoku follows Ryoku: desktop %v primary %s", desktop, p["primary"])
	}
}

// T50f (F3): palette = "desktop" follows the desktop palette file, and
// "ryoku", its older name, does too; JHON_DESKTOP_COLORS names the file.
func TestCurrentFollowsTheDesktopPaletteFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	file := filepath.Join(t.TempDir(), "colors.json")
	if err := os.WriteFile(file, []byte(`{"primary":"#123456"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JHON_DESKTOP_COLORS", file)
	settings := filepath.Join(t.TempDir(), "settings.toml")
	t.Setenv("JHON_SETTINGS", settings)
	for _, v := range []string{"desktop", "ryoku"} {
		if err := os.WriteFile(settings, []byte("schema = 1\n[integrations.theme]\npalette = \""+v+"\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if p, desktop := Current(); !desktop || p["primary"] != "#123456" {
			t.Errorf("palette = %s: desktop %v primary %s", v, desktop, p["primary"])
		}
	}
}

// Overlay and focused frames are frameSoft (T13, L65): primary mixed toward
// the page in 2% steps, the last step still at WCAG's 3:1 for non-text UI.
// pi reads it as `border` (was primaryContainer, 1.5:1, audit P3).
func TestFrameSoftIsTheLeastRoseAtThreeToOne(t *testing.T) {
	if got := FrameSoft(Fallback); got != "#7b6772" {
		t.Errorf("FrameSoft(rose ink) = %s, want #7b6772 (primary 54%% toward the page)", got)
	}
	desktop := Palette{}
	for k, v := range Fallback {
		desktop[k] = v
	}
	desktop["primary"] = "#ffb0c8"
	if got := FrameSoft(desktop); got != "#866272" {
		t.Errorf("FrameSoft(desktop #ffb0c8) = %s, want #866272", got)
	}
	for _, p := range []Palette{Fallback, desktop} {
		soft := FrameSoft(p)
		if c := contrast(soft, Page); c < 3 {
			t.Errorf("frameSoft %s is %.2f:1 on the page, under 3:1", soft, c)
		}
		// One more 2% step would fall under 3:1: it is the least rose that reads.
		for k := 0; k <= 50; k++ {
			if Mix(p["primary"], Page, float64(k)*0.02) == soft {
				if next := Mix(p["primary"], Page, float64(k+1)*0.02); contrast(next, Page) >= 3 {
					t.Errorf("frameSoft %s stops early: %s still reads at %.2f:1", soft, next, contrast(next, Page))
				}
			}
		}
		th := PiTheme(p)
		if th["colors"].(map[string]string)["border"] != "frameSoft" || th["vars"].(map[string]string)["frameSoft"] != soft {
			t.Errorf("pi border is not frameSoft %s", soft)
		}
	}
	dark := Palette{}
	for k, v := range Fallback {
		dark[k] = v
	}
	dark["primary"] = "#8a4a5e"
	if got := FrameSoft(dark); got != "#8a4a5e" {
		t.Errorf("a primary already under 3:1 keeps itself, got %s", got)
	}
}

// The rail and the status line sit on the frame gray (direction C, R8):
// pi's one background slot for them is customMessageBg, now railBand =
// Frame, a real step from the page (1.11:1, was the band's 1.06:1), and
// every text role still reads at AA on it.
func TestRailBandIsTheFrameGray(t *testing.T) {
	th := PiTheme(Fallback)
	vars, colors := th["vars"].(map[string]string), th["colors"].(map[string]string)
	if vars["railBand"] != Frame || colors["customMessageBg"] != "railBand" {
		t.Errorf("customMessageBg = %s (%s), want railBand = %s", colors["customMessageBg"], vars[colors["customMessageBg"]], Frame)
	}
	if c := contrast(Frame, Page); c < 1.10 || c > 1.12 {
		t.Errorf("rail band step %.3f:1, want about 1.11:1", c)
	}
	for _, role := range []string{"ink", "onSurfaceVariant", "outline", "accentSoft"} {
		if c := contrast(vars[role], Frame); c < 4.5 {
			t.Errorf("%s %s reads %.2f:1 on the rail band, under AA", role, vars[role], c)
		}
	}
	if vars["band"] != Band {
		t.Errorf("the band var stays for the dashboard and other programs: %s", vars["band"])
	}
}
