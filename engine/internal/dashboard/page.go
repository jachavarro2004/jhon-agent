package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/palette"
)

// Page is one session's dashboard as a single self-contained page (`jhon
// audit --html`): the component's index.html with its stylesheet and script
// inlined from assets, and the session (SessionData's shape) and its report
// embedded as JSON, so nothing is fetched. The script shows the embedded
// session alone.
func Page(assets string, session, report any) (string, error) {
	read := func(name string) (string, error) {
		b, err := os.ReadFile(filepath.Join(assets, name))
		if err != nil {
			return "", fmt.Errorf("the dashboard component is missing: %s; run `jhon sync`", name)
		}
		return string(b), nil
	}
	index, err := read("index.html")
	if err != nil {
		return "", err
	}
	css, err := read("app.css")
	if err != nil {
		return "", err
	}
	script, err := read("app.js")
	if err != nil {
		return "", err
	}
	for _, r := range [][2]string{
		{`<link rel="stylesheet" href="app.css">`, "<style>" + css + "</style>"},
		{`<link rel="stylesheet" href="palette.css">`, "<style>" + paletteCSS() + "</style>"},
		{`<script type="module" src="app.js"></script>`, `<script type="application/json" id="session">` + embed(session) + "</script>\n" +
			`<script type="application/json" id="report">` + embed(report) + "</script>\n" +
			"<script type=\"module\">" + strings.ReplaceAll(script, "</script", `<\/script`) + "</script>"},
	} {
		if strings.Count(index, r[0]) != 1 {
			return "", fmt.Errorf("the dashboard component's index.html does not hold %s once", r[0])
		}
		index = strings.Replace(index, r[0], r[1], 1)
	}
	return index, nil
}

// embed is JSON safe inside a script element: `<` is escaped so no text can close it.
func embed(v any) string {
	return strings.ReplaceAll(js.StringifyValueCompact(toValue(v)), "<", "\\u003c")
}

// paletteCSS is rose ink's :root now (internal/palette), read on every page
// and request so the dashboard follows the wallpaper when the settings
// follow the desktop palette; then the two roles design/system.md gives
// section headers (accentSoft) and overlay frames (frameSoft).
func paletteCSS() string {
	p, _ := palette.Current()
	return palette.CSS(p) + ":root{--accent-soft:" + palette.AccentSoft(p) + ";--frame-soft:" + palette.FrameSoft(p) + ";}\n"
}
