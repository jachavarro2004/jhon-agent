package cli

import (
	"fmt"
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// `jhon risk [--base <ref>] [--json]`: the risk tier of a
// change, by fixed rules over its diff (internal/ldd, risk.go). Read-only.
// Exit 0 with a tier, 2 on a usage error.
func init() { register("risk", runRisk) }

const riskUsage = "usage: jhon risk [--base <ref>] [--json]\n"

func runRisk(argv []string) (int, error) {
	json := false
	var base *string
	for i := 0; i < len(argv); i++ {
		switch {
		case argv[i] == "--json":
			json = true
		case argv[i] == "--base" && i+1 < len(argv):
			i++
			base = &argv[i]
		default:
			fmt.Fprintf(os.Stderr, "jhon risk: unknown argument: %s\n%s", argv[i], riskUsage)
			return 2, nil
		}
	}
	r, err := ldd.RiskOf(project.Resolve("", cwd()), base)
	if err != nil {
		return 1, err
	}
	if json {
		s, err := js.Stringify(r)
		if err != nil {
			return 1, err
		}
		fmt.Println(s)
		return 0, nil
	}
	label := r.Base
	if label == "" {
		label = "none"
	}
	fmt.Printf("risk: %s (base %s)\n", r.Tier, label)
	for _, x := range r.Reasons {
		file := ""
		if x.File != "" {
			file = x.File + ": "
		}
		fmt.Printf("  %s %s%s\n", js.PadEnd(string(x.Level), 7), file, x.Why)
	}
	return 0, nil
}
