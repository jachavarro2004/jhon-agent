package ldd

import (
	"fmt"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// worktreeKey is the LDD config key that turns on a worktree per change.
const worktreeKey = "worktree_per_change"

// WorktreePerChange is the project's `worktree_per_change` setting in its
// LDD config (config, the text of file): a worktree per change, default
// off (T85a, L609). A value other than true or false is off, with a note
// naming it.
func WorktreePerChange(config, file string) (bool, string) {
	raw := contract.Scalar(config, worktreeKey)
	switch v := stripComment(raw); v {
	case "", "false":
		return false, ""
	case "true":
		return true, ""
	default:
		return false, fmt.Sprintf("%s sets %s to '%s'; it takes true or false, so a worktree per change is off", file, worktreeKey, v)
	}
}

// worktreeOf is root's setting and its note; off when root has no
// readable LDD config.
func worktreeOf(root string) (bool, string) {
	b, err := project.ReadLddConfig(root)
	if err != nil {
		return false, ""
	}
	return WorktreePerChange(b, layout.For(root).Config())
}
