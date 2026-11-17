// Package palette is rose ink, Jhon's one color source (S29): its own fixed
// values, or, only when the settings choose integrations.theme.palette =
// "desktop" (L197, T55, T15.7, T50f; "rose-ink" by default, "ryoku" its
// older name), the Material You roles of a desktop palette file (a
// colors.json a desktop rewrites, matugen's on every wallpaper, say) when
// one stands. It is an opt-in integration no core package imports. The pi theme, the
// dashboard's CSS, and other programs' colors are all made here, so no surface keeps
// a copy of its own.
//
// States keep distinct hues whatever the wallpaper: on 2026-10-29 the shell
// left the desktop palette because states turned near-monochrome; success
// and warning are Kanagawa's fixed green and amber, one family (L79), and
// error reads the palette's own role.
//
// Surfaces are fixed (L66, L72), not the desktop's: the sidebar and its popups one
// dark neutral gray (Frame) with lighter grays for its headings and rows, the pane and pi's shell a blue ink (Page), pi's boxes
// lighter and still blue (Box, BoxStrong). The borders are rose, from the
// palette's primary; the text is its onSurface while it reads at WCAG AA on the
// strongest box. surface, surfaceContainerLowest and surfaceContainerLow are
// never a background.
package palette

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

// Palette is each Material You role's #rrggbb color.
type Palette map[string]string

// Roles are the roles rose ink reads, in order.
var Roles = []string{
	"surface", "surfaceContainerLowest", "surfaceContainerLow", "surfaceContainer", "surfaceContainerHigh", "surfaceContainerHighest",
	"onSurface", "onSurfaceVariant", "outline", "outlineVariant",
	"primary", "primaryContainer", "onPrimaryContainer", "secondary", "secondaryContainer",
	"tertiary", "tertiaryContainer", "error", "errorContainer",
}

// Fallback is rose ink without a desktop palette, Jhon's own fixed values:
// the roles the owner's desktop published for its rose wallpaper when the
// owner chose it (L50).
var Fallback = Palette{
	"surface": "#161313", "surfaceContainerLowest": "#100d0e", "surfaceContainerLow": "#1e1b1b", "surfaceContainer": "#221f1f",
	"surfaceContainerHigh": "#2d292a", "surfaceContainerHighest": "#383434",
	"onSurface": "#e9e0e1", "onSurfaceVariant": "#d2c3c6", "outline": "#9b8d90", "outlineVariant": "#4f4447",
	"primary": "#e7bbc8", "primaryContainer": "#53343f", "onPrimaryContainer": "#f2c5d2",
	"secondary": "#d8c1c7", "secondaryContainer": "#534247",
	"tertiary": "#b6cdae", "tertiaryContainer": "#2f422b", "error": "#ffb4ab", "errorContainer": "#93000a",
}

// Warn is the fixed amber of a warning, whatever the wallpaper.
const Warn = "#e6c384"

// OK is the fixed green of success, Kanagawa's springGreen: one family with
// the amber (L79), whatever the wallpaper.
const OK = "#98bb6c"

// The surfaces are fixed, whatever the wallpaper, and dark for a dark room
// (L72): the pane and pi's shell Kanagawa's sumiInk3 (Page); pi's boxes
// sumiInk4 and sumiInk5, lighter and still blue (Box, and BoxStrong for the
// user's message). Only the accent and the states follow a desktop palette.
//
// Everything another program's sidebar paints is a neutral gray (L71), darker than the
// page (Frame, the sidebar and its popups) and each a step lighter: its
// SPACES/AGENTS headings a band (Section), the active row, neither light nor
// dark (Row), a dragged row (RowRaise).
const (
	Frame     = "#161618"
	Section   = "#1d1d20"
	Row       = "#2b2b2e"
	RowRaise  = "#363639"
	Page      = "#1f1f28"
	Box       = "#2a2a37"
	BoxStrong = "#363646"
)

// Band is the Gentle Shell's darker band (T34, L98): halfway from the page
// to the frame, darker than the page. The sections status line, the rail and
// pi's notices sit on it; pi reads it as customMessageBg.
var Band = Mix(Page, Frame, 0.5)

// FallbackInk is the text when the palette's onSurface does not read at WCAG AA on
// the strongest box: Kanagawa's fujiWhite.
const FallbackInk = "#dcd7ba"

// Path is the desktop palette file: Override, else the default
// ~/.cache/ryoku/colors.json.
func Path() string {
	if p := Override(); p != "" {
		return p
	}
	return filepath.Join(project.Home(), ".cache", "ryoku", "colors.json")
}

// Override is the path JHON_DESKTOP_COLORS names; empty when unset.
func Override() string {
	return os.Getenv("JHON_DESKTOP_COLORS")
}

// Current is rose ink now: the desktop palette (Read of Path) when
// integrations.theme.palette is "desktop", else Jhon's own fixed values
// (a copy of Fallback), with no desktop file read. desktop is whether a
// desktop palette stood.
func Current() (p Palette, desktop bool) {
	if settings.Str("integrations.theme.palette") != "desktop" {
		p = Palette{}
		for k, v := range Fallback {
			p[k] = v
		}
		return p, false
	}
	return Read(Path())
}

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{6})(?:[0-9a-fA-F]{2})?$`)

// Read is rose ink from the palette at path, each role normalised or fallen
// back; desktop is false when no readable palette stands there.
func Read(path string) (p Palette, desktop bool) {
	p = Palette{}
	for k, v := range Fallback {
		p[k] = v
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return p, false
	}
	var raw map[string]any
	if json.Unmarshal(b, &raw) != nil || raw == nil {
		return p, false
	}
	for _, role := range Roles {
		if s, ok := raw[role].(string); ok {
			if m := hexColor.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
				p[role] = "#" + strings.ToLower(m[1])
			}
		}
	}
	return p, true
}

// hsl is a #rrggbb color's hue (degrees) and saturation (0-1).
func hsl(c string) (hue, sat float64) {
	var rgb [3]float64
	for i := range rgb {
		n, _ := strconv.ParseUint(c[1+2*i:3+2*i], 16, 8)
		rgb[i] = float64(n) / 255
	}
	r, g, b := rgb[0], rgb[1], rgb[2]
	hi, lo := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	d := hi - lo
	if d == 0 {
		return 0, 0
	}
	l := (hi + lo) / 2
	sat = d / (1 - math.Abs(2*l-1))
	switch hi {
	case r:
		hue = math.Mod((g-b)/d, 6)
	case g:
		hue = (b-r)/d + 2
	default:
		hue = (r-g)/d + 4
	}
	hue *= 60
	if hue < 0 {
		hue += 360
	}
	return hue, sat
}

// States are success and error: success the fixed green (OK), error the
// palette's error while it reads as its own hue beside the green, the fallback's
// otherwise, so a gray or green wallpaper never makes them alike.
func States(p Palette) (ok, bad string) {
	ok, bad = OK, p["error"]
	h1, _ := hsl(ok)
	h2, s2 := hsl(bad)
	dist := math.Abs(h1 - h2)
	if dist > 180 {
		dist = 360 - dist
	}
	if s2 < 0.15 || dist < 40 {
		return ok, Fallback["error"]
	}
	return ok, bad
}

// AccentSoft is the rose away from focus (L79): primary a quarter of the way
// to the page, about 75% primary, for card titles, headings, bullets and
// labels; focus keeps full primary. A primary too dark for the softened rose
// to read at WCAG AA (4.5:1) on the page stays itself.
func AccentSoft(p Palette) string {
	if soft := Mix(p["primary"], Page, 0.25); contrast(soft, Page) >= 4.5 {
		return soft
	}
	return p["primary"]
}

// FrameSoft is the border of overlays and focused frames (T13, L65): primary
// mixed toward the page in 2% steps, the last step that still reads at
// WCAG's 3:1 for non-text UI (#7b6772 on rose ink, 54%). A primary already
// under 3:1 keeps itself. pi reads it as `border`.
func FrameSoft(p Palette) string {
	soft := p["primary"]
	for k := 1; k <= 50; k++ {
		next := Mix(p["primary"], Page, float64(k)*0.02)
		if contrast(next, Page) < 3 {
			break
		}
		soft = next
	}
	return soft
}

// AccentEdge is the muted rose of another program's sidebar edge (L79): primary 45% of
// the way to the frame, quieter than the focused pane's full primary.
func AccentEdge(p Palette) string {
	return Mix(p["primary"], Frame, 0.45)
}

// Mix is a moved toward b by t (0-1) in sRGB, each channel rounded.
func Mix(a, b string, t float64) string {
	out := "#"
	for i := 0; i < 3; i++ {
		x, _ := strconv.ParseUint(a[1+2*i:3+2*i], 16, 8)
		y, _ := strconv.ParseUint(b[1+2*i:3+2*i], 16, 8)
		out += fmt.Sprintf("%02x", int(math.Round(float64(x)+(float64(y)-float64(x))*t)))
	}
	return out
}

// contrast is the WCAG contrast ratio of two #rrggbb colors.
func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// luminance is the relative luminance of a #rrggbb color.
func luminance(c string) float64 {
	var out float64
	for i, w := range []float64{0.2126, 0.7152, 0.0722} {
		n, _ := strconv.ParseUint(c[1+2*i:3+2*i], 16, 8)
		v := float64(n) / 255
		if v <= 0.03928 {
			v /= 12.92
		} else {
			v = math.Pow((v+0.055)/1.055, 2.4)
		}
		out += w * v
	}
	return out
}

// ThemeColors are every color pi's theme schema requires.
var ThemeColors = []string{
	"accent", "bashMode", "border", "borderAccent", "borderMuted", "customMessageBg", "customMessageLabel", "customMessageText", "dim",
	"error", "mdCode", "mdCodeBlock", "mdCodeBlockBorder", "mdHeading", "mdHr", "mdLink", "mdLinkUrl", "mdListBullet", "mdQuote",
	"mdQuoteBorder", "muted", "selectedBg", "success", "syntaxComment", "syntaxFunction", "syntaxKeyword", "syntaxNumber",
	"syntaxOperator", "syntaxPunctuation", "syntaxString", "syntaxType", "syntaxVariable", "text", "thinkingHigh", "thinkingLow",
	"thinkingMedium", "thinkingMinimal", "thinkingOff", "thinkingText", "thinkingXhigh", "toolDiffAdded", "toolDiffContext",
	"toolDiffRemoved", "toolErrorBg", "toolOutput", "toolPendingBg", "toolSuccessBg", "toolTitle", "userMessageBg", "userMessageText", "warning",
}

// ThemeName is the pi theme rose ink makes.
const ThemeName = "rose-ink"

// Ink is the text: the palette's onSurface while it reads at WCAG AA (4.5:1) on
// the strongest box, FallbackInk otherwise.
func Ink(p Palette) string {
	if contrast(p["onSurface"], BoxStrong) >= 4.5 {
		return p["onSurface"]
	}
	return FallbackInk
}

// PiTheme is pi's theme from p: every color a role or a fixed surface,
// through vars named after them, the fixed amber as `warn`, the rose away
// from focus as `accentSoft`, and the band under notices as `band`.
func PiTheme(p Palette) map[string]any {
	vars := map[string]string{"warn": Warn}
	for _, r := range Roles {
		vars[r] = p[r]
	}
	vars["ok"], vars["bad"] = States(p)
	vars["page"], vars["box"], vars["boxStrong"], vars["ink"] = Page, Box, BoxStrong, Ink(p)
	vars["band"] = Band
	vars["borderMuted"] = Mix(p["primaryContainer"], Page, 0.5)
	vars["accentSoft"] = AccentSoft(p)
	vars["frameSoft"] = FrameSoft(p)
	// The rail and the status line on the frame gray (direction C, R8): pi's
	// one background slot for them is customMessageBg.
	vars["railBand"] = Frame
	colors := map[string]string{
		"accent": "primary", "border": "frameSoft", "borderAccent": "primary", "borderMuted": "borderMuted",
		"text": "ink", "muted": "onSurfaceVariant", "dim": "outline",
		"success": "ok", "warning": "warn", "error": "bad", "selectedBg": "primaryContainer",
		"userMessageBg": "boxStrong", "userMessageText": "ink",
		"customMessageBg": "railBand", "customMessageLabel": "accentSoft", "customMessageText": "onSurfaceVariant",
		"toolPendingBg": "box", "toolSuccessBg": "box", "toolErrorBg": "errorContainer",
		"toolTitle": "ink", "toolOutput": "onSurfaceVariant",
		"toolDiffAdded": "ok", "toolDiffRemoved": "bad", "toolDiffContext": "outline",
		"mdHeading": "accentSoft", "mdLink": "tertiary", "mdLinkUrl": "outline", "mdCode": "tertiary", "mdCodeBlock": "onSurfaceVariant",
		"mdCodeBlockBorder": "outlineVariant", "mdQuote": "onSurfaceVariant", "mdQuoteBorder": "outlineVariant", "mdHr": "outlineVariant",
		"mdListBullet":  "accentSoft",
		"syntaxComment": "outline", "syntaxKeyword": "primary", "syntaxFunction": "tertiary", "syntaxVariable": "ink",
		"syntaxString": "secondary", "syntaxNumber": "warn", "syntaxType": "primary", "syntaxOperator": "onSurfaceVariant",
		"syntaxPunctuation": "outline",
		"thinkingOff":       "outlineVariant", "thinkingMinimal": "outline", "thinkingLow": "onSurfaceVariant", "thinkingMedium": "secondary",
		"thinkingHigh": "primary", "thinkingXhigh": "warn", "thinkingText": "outline", "bashMode": "warn",
	}
	return map[string]any{
		"$schema": "https://pi.dev/schemas/theme.schema.json", "name": ThemeName, "appearance": "dark",
		"vars": vars, "colors": colors,
		"export": map[string]string{"pageBg": "page", "cardBg": "box", "infoBg": "boxStrong"},
	}
}

// PiThemeText is the theme file's text: indented JSON and a newline.
func PiThemeText(p Palette) string {
	b, _ := json.MarshalIndent(PiTheme(p), "", "  ")
	return string(b) + "\n"
}

// WritePiTheme writes the pi theme into dir/themes/rose-ink.json when its
// text changed (pi hot-reloads the active theme from there), and reports
// whether it wrote.
func WritePiTheme(dir string, p Palette) (bool, error) {
	path := filepath.Join(dir, "themes", ThemeName+".json")
	text := PiThemeText(p)
	if b, err := os.ReadFile(path); err == nil && string(b) == text {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

// CSS is the dashboard's :root from p: the variable names app.css uses,
// mapped onto the roles and the fixed surfaces, no hue outside them.
func CSS(p Palette) string {
	ok, bad := States(p)
	ink := Ink(p)
	mix := func(c string, pct int) string { return fmt.Sprintf("color-mix(in srgb,%s %d%%,transparent)", c, pct) }
	vars := [][2]string{
		{"--rail", Frame}, {"--bg", Page}, {"--panel", Frame},
		{"--surface", Box}, {"--surface-hi", BoxStrong},
		{"--sel", p["primaryContainer"]}, {"--sel-edge", p["primary"]},
		{"--ink", ink}, {"--ink2", p["onSurfaceVariant"]}, {"--soft", p["onSurfaceVariant"]}, {"--faint", p["outline"]},
		{"--line", mix(ink, 11)}, {"--line-strong", mix(p["primary"], 30)}, {"--line-rosa", mix(p["primary"], 40)},
		{"--rosa", p["primary"]}, {"--rosa-soft", mix(p["primary"], 10)}, {"--rosa-fill", p["primaryContainer"]}, {"--rosa-ink", p["onPrimaryContainer"]},
		{"--broken", bad}, {"--gold", Warn}, {"--ok", ok},
		{"--shadow", "#000"},
	}
	var b strings.Builder
	b.WriteString(":root{")
	for _, v := range vars {
		b.WriteString(v[0] + ":" + v[1] + ";")
	}
	b.WriteString("}\n")
	return b.String()
}

// Semantic is rose ink by meaning, for programs that are not pi or the
// dashboard: surfaces, ink, the accent and the states;
// desktop says whether a desktop palette stood. frame is the sidebar and its
// popups (rail, panel), section, row and rowRaise its grays, page the pane,
// box and boxStrong what rises on it, band the darker band under the shell's
// status line and notices. accent is focus, accentSoft the rose
// away from it, accentEdge the sidebar's edge, frameSoft other programs' popup
// frames (the overlay role). info and tool stay for the dashboard (T16).
func Semantic(p Palette, desktop bool) map[string]any {
	ok, bad := States(p)
	return map[string]any{
		"frame": Frame, "section": Section, "row": Row, "rowRaise": RowRaise, "page": Page, "box": Box, "boxStrong": BoxStrong, "band": Band,
		"rail": Frame, "panel": Frame, "card": Box, "raise": BoxStrong, "hover": BoxStrong,
		"ink": Ink(p), "soft": p["onSurfaceVariant"], "faint": p["outline"], "line": p["outlineVariant"],
		"accent": p["primary"], "accentFill": p["primaryContainer"], "accentInk": p["onPrimaryContainer"],
		"accentSoft": AccentSoft(p), "accentEdge": AccentEdge(p), "frameSoft": FrameSoft(p),
		"ok": ok, "bad": bad, "warn": Warn, "info": "#7aa89f", "tool": "#957fb8", "desktop": desktop,
	}
}
