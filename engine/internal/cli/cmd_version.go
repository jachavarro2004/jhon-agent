package cli

import (
	"fmt"
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
)

// `jhon version [--json]` (and `jhon --version`): the version stamped into
// this binary (internal/version). Read-only; exit 2 on a usage error.
func init() { register("version", runVersion) }

const versionUsage = "usage: jhon version [--json]\n"

func runVersion(argv []string) (int, error) {
	json := false
	for _, a := range argv {
		if a != "--json" {
			fmt.Fprintf(os.Stderr, "jhon version: unknown argument: %s\n%s", a, versionUsage)
			return 2, nil
		}
		json = true
	}
	if !json {
		fmt.Printf("jhon %s\n", version.String())
		return 0, nil
	}
	fmt.Println(js.StringifyValue(js.Obj("version", version.Version, "commit", version.Commit, "date", version.Date)))
	return 0, nil
}
