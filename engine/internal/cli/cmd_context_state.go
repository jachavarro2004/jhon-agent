package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// commitWait is how long the state write after a slice's commit waits for
// the commit route-guard accepted to land.
const commitWait = 30 * time.Second

// engramOn is whether the engine writes the change's state to Engram: the
// owner's `integrations.engram` is on and Engram resolves (ldd.EngramBin).
func engramOn() bool {
	return settings.On("integrations.engram") && ldd.EngramBin() != ""
}

// saveState writes change's state, read before the transition as s, to
// Engram from root; its one note, when the write fails, goes to stderr.
func saveState(root string, s ldd.ChangeState) {
	if note := ldd.SaveState(root, s); note != "" {
		fmt.Fprintln(os.Stderr, note)
	}
}

// saveStateAfterCommit is `jhon context <change> --phase <phase>
// --save-state --after <sha>`: once HEAD in root moves from after to a
// commit that closes a slice of change, within commitWait, the change's
// state goes to Engram; nothing when HEAD does not move (the commit was
// refused or failed) or the commit closes none.
func saveStateAfterCommit(root, change, phase, after string) {
	if !engramOn() {
		return
	}
	closed := ldd.AwaitSliceCommit(root, change, after, commitWait)
	if len(closed) == 0 {
		return
	}
	head := vcs.HeadSha(root)
	delivery := strings.Join(closed, ", ") + " committed at " + head[:min(len(head), 12)]
	saveState(root, ldd.StateOf(root, change, phase, delivery, []string{"HEAD^", "HEAD"}))
}
