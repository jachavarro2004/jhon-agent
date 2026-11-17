package cli

import (
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
)

// `jhon validate` and `jhon check`:
// the renderer's --validate and --check on the SOURCE repository, which
// need no node now that the renderer is Go (internal/render).
func init() {
	register("validate", func(argv []string) (int, error) { return onSource("validate", "--validate") })
	register("check", func(argv []string) (int, error) { return onSource("check", "--check") })
}

// onSource runs the renderer with flag on the source, after NeedRepo:
// the source must hold the binary's source.
func onSource(what, flag string) (int, error) {
	r := release.Locate()
	if !r.HasSource() {
		return die(release.NoSource(what, r.Source))
	}
	return render.Run(r.Source, []string{flag}, os.Stdout, os.Stderr), nil
}
