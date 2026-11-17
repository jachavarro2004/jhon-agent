package project

import (
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lddswitch"
)

// LddConfigFile is root's LDD config file.
func LddConfigFile(root string) string { return layout.At(root, layout.For(root).Config()) }

// UsesLdd is whether the project at root has LDD set up: it has an LDD
// config. LDD is always on with Jhon (T72, L555); a root of "" (no
// project) never has it.
func UsesLdd(root string) bool {
	return root != "" && Exists(LddConfigFile(root))
}

// switchKey is the main checkout of root, as the retired switch recorded it.
func switchKey(root string) string {
	if main := MainWorktree(root); main != "" {
		return main
	}
	return root
}

// OldSwitchOffUntold is whether the retired `jhon ldd off` (T57c) left an
// off record for the project at root that its sessions were not yet told
// no longer applies (T72, L555). The record changes nothing else.
func OldSwitchOffUntold(root string) bool {
	if root == "" || !lddswitch.Recorded() {
		return false
	}
	key := switchKey(root)
	return lddswitch.Of(key).LDD == lddswitch.Off && !lddswitch.Told(key)
}

// TellOldSwitchOff records that the project at root was told.
func TellOldSwitchOff(root string) error { return lddswitch.Tell(switchKey(root)) }

// ReadLddConfig is the text of root's LDD config; an error when root is no
// project or the file cannot be read.
func ReadLddConfig(root string) (string, error) {
	if root == "" {
		return "", os.ErrNotExist
	}
	b, err := os.ReadFile(LddConfigFile(root))
	return string(b), err
}
