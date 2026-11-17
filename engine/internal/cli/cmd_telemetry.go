package cli

import (
	"fmt"
	"os"
	"strconv"

	"github.com/jachavarro2004/jhon-agent/engine/internal/telemetry"
)

// `jhon telemetry [--days N] [--json]`: what Jhon recorded of its own
// decisions and runs (engine/internal/telemetry) over the last N days (7 by
// default, at most telemetry.Keep): each hook's decisions and durations, the
// commands, test runs and delivery checks, the sub-agents and how they
// ended, the compactions, and the latest refusals. Read-only; exit 2 on
// an unknown argument.
func init() { register("telemetry", runTelemetry) }

func runTelemetry(argv []string) (int, error) {
	asJSON, days := false, 7
	usage := func(msg string) (int, error) {
		fmt.Fprintf(os.Stderr, "jhon telemetry: %s\nusage: jhon telemetry [--days N] [--json]\n", msg)
		return 2, nil
	}
	for i := 0; i < len(argv); i++ {
		switch a := argv[i]; a {
		case "--json":
			asJSON = true
		case "--days":
			if i+1 >= len(argv) {
				return usage("--days needs a number")
			}
			n, err := strconv.Atoi(argv[i+1])
			if err != nil || n < 1 || n > telemetry.Keep {
				return usage(fmt.Sprintf("--days is a number from 1 to %d", telemetry.Keep))
			}
			days, i = n, i+1
		default:
			return usage("unexpected argument: " + a)
		}
	}
	s := telemetry.Last(days)
	if asJSON {
		fmt.Println(s.JSON())
	} else {
		fmt.Println(s.Human())
	}
	return 0, nil
}
