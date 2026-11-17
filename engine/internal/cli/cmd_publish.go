package cli

import (
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
)

// `jhon publish` (internal/release): push the current branch to origin and
// mirror, refused unless the tree is clean and HEAD is the active release.
// Publishing is explicit and separate from activation; it takes no argument.
func init() { register("publish", runPublish) }

func runPublish(argv []string) (int, error) {
	if len(argv) > 0 {
		return usageError("publish takes no argument: " + strings.Join(argv, " "))
	}
	return release.Publish(release.Locate(), out()), nil
}
