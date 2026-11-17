package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/palette"
)

// `jhon palette --css|--pi|--write-pi <dir>`: rose ink, Jhon's one color
// source (internal/palette): its fixed values, or a desktop palette when
// integrations.theme.palette is "desktop" and one stands. --css is the dashboard's :root, --pi the pi theme, --json the
// semantic tokens other programs read, --write-pi
// writes <dir>/themes/rose-ink.json when it changed (pi hot-reloads it).
func init() { register("palette", runPalette) }

const paletteUsage = "usage: jhon palette --css | --pi | --json | --write-pi <agent-dir>\n"

func runPalette(argv []string) (int, error) {
	p, desktop := palette.Current()
	switch {
	case len(argv) == 1 && argv[0] == "--css":
		fmt.Print(palette.CSS(p))
	case len(argv) == 1 && argv[0] == "--json":
		b, _ := json.Marshal(palette.Semantic(p, desktop))
		fmt.Println(string(b))
	case len(argv) == 1 && argv[0] == "--pi":
		fmt.Print(palette.PiThemeText(p))
	case len(argv) == 2 && argv[0] == "--write-pi":
		wrote, err := palette.WritePiTheme(argv[1], p)
		if err != nil {
			return 1, err
		}
		if wrote {
			fmt.Println("wrote", argv[1]+"/themes/"+palette.ThemeName+".json")
		} else {
			fmt.Println("unchanged", argv[1]+"/themes/"+palette.ThemeName+".json")
		}
	default:
		fmt.Fprint(os.Stderr, "jhon palette: "+paletteUsage)
		return 2, nil
	}
	return 0, nil
}
