package cli

import (
	"fmt"
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/hook"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
)

// `jhon archive <change-name> <YYYY-MM-DD>`: move a change directory into
// openspec/changes/archive mechanically and verify it (internal/ldd,
// archive.go). Run from the project root. An archive that succeeded ends
// with the one-session-per-change notice (S35); a change archives once, so
// the notice comes once per change. Then the change's state, read before
// the move, goes to Engram (T134 H4): a write that fails is one note on
// stderr, and the archive's exit status stands.
func init() {
	register("archive", func(argv []string) (int, error) {
		var state *ldd.ChangeState
		if len(argv) == 2 && engramOn() {
			root := cwd()
			s := ldd.StateOf(root, argv[0], "archive", "archived to "+layout.For(root).Archive(argv[1], argv[0]), nil)
			state = &s
		}
		code := ldd.Archive(argv, os.Stdout, os.Stderr)
		if code == 0 {
			fmt.Fprintln(os.Stdout, hook.FreshSession(argv[0]+" is archived.", "jhon context --phase none --handoff"))
			if state != nil {
				saveState(cwd(), *state)
			}
		}
		return code, nil
	})
}
