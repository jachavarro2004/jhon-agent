// Package lddswitch reads the records of the retired `jhon ldd on|off`
// (T57c, L475): one per main checkout under
// ${XDG_STATE_HOME:-~/.local/state}/jhon/ldd-switch/<sha256 of the root>.json.
// LDD is always on with Jhon now (T72, L555): a record turns nothing off;
// an off record only has the project's sessions told so, once (Tell writes
// <sha256>.told beside it). The directory is jhon's state, which the guards
// keep from every agent (grant.InGrants).
package lddswitch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	// On is a record of LDD on, as it always is now.
	On = "on"
	// Off is an old record of LDD switched off, which no longer applies.
	Off = "off"
	// SourceDefault is a project whose owner never set the switch.
	SourceDefault = "default"
	// SourceOwner is the owner's record.
	SourceOwner = "owner"
)

// Dir is where the switch's records live.
func Dir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" || !filepath.IsAbs(state) {
		home := os.Getenv("HOME")
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "jhon", "ldd-switch")
}

// File is root's record file.
func File(root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(Dir(), hex.EncodeToString(sum[:])+".json")
}

// Record is a project's switch and since when; Since is "" by default.
type Record struct {
	LDD    string `json:"ldd"`
	Root   string `json:"root"`
	Since  string `json:"since"`
	Source string `json:"source"`
}

// Recorded is whether any switch was ever recorded: none, and nothing is read.
func Recorded() bool {
	_, err := os.Stat(Dir())
	return err == nil
}

// Of is root's switch, root a main checkout: on by default; a record that
// is unreadable, of another root, or of no state is the default.
func Of(root string) Record {
	def := Record{LDD: On, Root: root, Source: SourceDefault}
	if root == "" {
		return def
	}
	b, err := os.ReadFile(File(root))
	if err != nil {
		return def
	}
	var rec Record
	if json.Unmarshal(b, &rec) != nil || rec.Root != root || (rec.LDD != On && rec.LDD != Off) {
		return def
	}
	rec.Source = SourceOwner
	return rec
}

// Told is whether root's sessions were told its off record no longer applies.
func Told(root string) bool {
	_, err := os.Stat(toldFile(root))
	return err == nil
}

// Tell records that root's sessions were told, so they are told once.
func Tell(root string) error {
	file := toldFile(root)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	return os.WriteFile(file, nil, 0o600)
}

// toldFile is the marker Tell writes beside root's record.
func toldFile(root string) string { return strings.TrimSuffix(File(root), ".json") + ".told" }
