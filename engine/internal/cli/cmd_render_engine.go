package cli

import (
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
)

// `jhon render-engine <flags…>`, hidden: the renderer (internal/render)
// run with its own flags, where root is the tree
// the binary runs from (JHON_ROOT in the tests). The conformance suite and
// the release scripts drive the renderer through it.
func init() {
	register("render-engine", func(argv []string) (int, error) {
		return render.Run(release.Locate().Runtime, argv, os.Stdout, os.Stderr), nil
	})
}
