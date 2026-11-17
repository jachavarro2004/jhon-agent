package grant

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// The owner's permission modes (`jhon mode`, S31), one per project root,
// recorded beside the grants under ${XDG_STATE_HOME:-~/.local/state}/jhon/modes/:
// <sha256 of the root>.json, every change appended to history.jsonl, and
// each call a mode let through to allowed.jsonl, which jhon audit counts.
// A mode has no expiry: it lasts until the owner sets another.
const (
	// ModeNormal is the guards as they are.
	ModeNormal = "normal"
	// ModeYolo passes every class but destroy without asking.
	ModeYolo = "yolo"
	// ModeSuperYolo passes every class without asking, destroy included.
	ModeSuperYolo = "super-yolo"
)

// Modes are the modes the owner may set.
var Modes = []string{ModeNormal, ModeYolo, ModeSuperYolo}

// ModesDir is where the modes live, beside the grants.
func ModesDir() string { return filepath.Join(filepath.Dir(Dir()), "modes") }

// ModeFile is the record file of root's mode.
func ModeFile(root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(ModesDir(), hex.EncodeToString(sum[:])+".json")
}

// ModeHistoryFile is every mode the owner set, one line each.
func ModeHistoryFile() string { return filepath.Join(ModesDir(), "history.jsonl") }

// AllowedFile is every call a mode let through, one line each.
func AllowedFile() string { return filepath.Join(ModesDir(), "allowed.jsonl") }

// ModeRecord is a project's mode and since when it holds; Since is "" for
// a project whose owner never set one.
type ModeRecord struct {
	Mode  string `json:"mode"`
	Root  string `json:"root"`
	Since string `json:"since"`
}

// ModeChange is one line of the history: the owner set mode for root at At.
type ModeChange struct {
	Mode string `json:"mode"`
	Root string `json:"root"`
	At   string `json:"at"`
	By   string `json:"by"`
}

// Allowed is one call a mode let through: the class it asked for and what.
type Allowed struct {
	At      string `json:"at"`
	Mode    string `json:"mode"`
	Root    string `json:"root"`
	Class   string `json:"class"`
	What    string `json:"what"`
	Guard   string `json:"guard"`
	Session string `json:"session,omitempty"`
	Agent   string `json:"agent,omitempty"`
}

// ModeOf is the mode of the project proj belongs to, its worktrees
// resolving to the main checkout as a grant's do; a record that is
// unreadable, of another root, or of no mode is normal.
func ModeOf(proj string) ModeRecord {
	root := Root(proj)
	normal := ModeRecord{Mode: ModeNormal, Root: root}
	// A folder that is no git repository is no project and takes no mode,
	// whatever a record from before says (T57a).
	if root == "" || (project.MainWorktree(root) == "" && project.Root(root) == "") {
		return normal
	}
	data, err := os.ReadFile(ModeFile(root))
	if err != nil {
		return normal
	}
	var rec ModeRecord
	if json.Unmarshal(data, &rec) != nil || rec.Root != root || !slices.Contains(Modes, rec.Mode) {
		return normal
	}
	return rec
}

// SetMode records mode for root, a main checkout, from now on, and
// appends the change to the history as the owner's.
func SetMode(root, mode string) (ModeRecord, error) {
	if !slices.Contains(Modes, mode) {
		return ModeRecord{}, fmt.Errorf("unknown mode '%s'", mode)
	}
	rec := ModeRecord{Mode: mode, Root: root, Since: isodate.Format(time.Now())}
	file := ModeFile(root)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return rec, err
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rec); err != nil {
		return rec, err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return rec, err
	}
	if err := os.Rename(tmp, file); err != nil {
		return rec, err
	}
	return rec, appendLine(ModeHistoryFile(), ModeChange{Mode: mode, Root: root, At: rec.Since, By: "owner"})
}

// Questions are the asks no grant answers, with the modes that pass them
// without asking (L154, L155): yolo asks only for destroy, super-yolo for
// nothing. A command or target the guard cannot read may hide a destroy,
// so yolo still asks it; a write to the Claude Code settings and a git
// config that changes what git runs could switch the guards off, so every
// mode asks them.
var Questions = map[string][]string{
	"review":     {ModeYolo, ModeSuperYolo},
	"unreadable": {ModeSuperYolo},
	"unknown":    {ModeSuperYolo},
	"settings":   nil,
	"git-config": nil,
}

// ModeCovers is whether mode passes an ask of cls without asking: yolo
// every grantable class but destroy, super-yolo every grantable class, and
// a question no grant answers as Questions says; a class neither names is
// never a mode's.
func ModeCovers(mode, cls string) bool {
	if modes, ok := Questions[cls]; ok {
		return slices.Contains(modes, mode)
	}
	if !slices.Contains(Classes, cls) {
		return false
	}
	return mode == ModeSuperYolo || (mode == ModeYolo && cls != "destroy")
}

// ModeHistory is the history's changes in the order written; an unreadable
// line is skipped.
func ModeHistory() []ModeChange {
	var out []ModeChange
	for _, l := range lines(ModeHistoryFile()) {
		var c ModeChange
		if json.Unmarshal([]byte(l), &c) == nil && c.Mode != "" && c.Root != "" {
			out = append(out, c)
		}
	}
	return out
}

// RecordAllowed appends a call a mode let through to the log.
func RecordAllowed(a Allowed) error { return appendLine(AllowedFile(), a) }

// AllowedCalls is the log's calls in the order written.
func AllowedCalls() []Allowed {
	var out []Allowed
	for _, l := range lines(AllowedFile()) {
		var a Allowed
		if json.Unmarshal([]byte(l), &a) == nil && a.Mode != "" {
			out = append(out, a)
		}
	}
	return out
}

func lines(file string) []string {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	return strings.Split(string(b), "\n")
}

// appendLine appends v to file as one JSON line.
func appendLine(file string, v any) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}
