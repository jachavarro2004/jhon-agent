// Package grant reads and writes the owner's grants of an ASK class
// (`jhon grant`, read by the guards): one JSON record per class and project root
// under ${XDG_STATE_HOME:-~/.local/state}/jhon/grants/<sha256 of the root>/<class>.json,
// and every grant and revocation appended to the history beside them,
// which jhon audit reads.
package grant

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// Classes are the ASK classes a grant may name.
var Classes = []string{"push", "gh-write", "publish", "remote", "deploy", "outside-write", "destroy"}

// Retired are classes no longer granted (L19): a record of one is not
// listed, and --revoke removes it (L48).
var Retired = []string{"review", "verified"}

// Dir is where grants live.
func Dir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" || !filepath.IsAbs(state) {
		state = filepath.Join(project.Home(), ".local", "state")
	}
	return filepath.Join(state, "jhon", "grants")
}

var grantsPath = lazyre.New(`[\\/]jhon[\\/](?:grants|modes|ldd-switch|session-grants)(?:[\\/]|$)|[\\/]\.local[\\/]state[\\/]jhon[\\/]?$`)

// InGrants is whether abs is in the grants directory or the modes beside
// it (the owner's alone), as configured or under any jhon/grants or
// jhon/modes, read through the links of its existing prefix. A path with a
// glob is in them when its segments can match theirs (`jhon/mo*/k.json`).
func InGrants(abs string) bool {
	// The state/jhon directory that holds both is the owner's too: replacing
	// it, or copying a tree into it, plants a mode or a grant history.jsonl
	// never records, and yolo passes outside writes (re-verify of T38b).
	for _, dir := range []string{filepath.Dir(Dir()), Dir(), ModesDir()} {
		for _, p := range []string{abs, project.Realish(abs)} {
			if project.Within(p, dir) || project.Within(p, project.Realish(dir)) || grantsPath.MatchString(p) {
				return true
			}
			if strings.ContainsAny(p, "*?[") && (globReaches(p, dir) || globReaches(p, project.Realish(dir)) || globNamesState(p)) {
				return true
			}
		}
	}
	return false
}

func segments(p string) []string { return strings.Split(filepath.Clean(p), string(filepath.Separator)) }

// globReaches is whether the pattern p can name dir or a path inside it,
// matched segment by segment.
func globReaches(p, dir string) bool {
	ps, ds := segments(p), segments(dir)
	if len(ps) < len(ds) {
		return false
	}
	for i, d := range ds {
		if ok, err := filepath.Match(ps[i], d); err != nil || !ok {
			return false
		}
	}
	return true
}

// globNamesState is whether two segments of the pattern p in a row, each
// with a literal character (`*/*` names any two), can match jhon/grants or
// jhon/modes, wherever the state directory is.
func globNamesState(p string) bool {
	ps := segments(p)
	literal := func(s string) bool { return strings.Trim(s, "*?") != "" }
	for i := 0; i+1 < len(ps); i++ {
		if ok, _ := filepath.Match(ps[i], "jhon"); !ok || !literal(ps[i]) || !literal(ps[i+1]) {
			continue
		}
		for _, d := range []string{"grants", "modes", "ldd-switch", "session-grants"} {
			if ok, _ := filepath.Match(ps[i+1], d); ok {
				return true
			}
		}
	}
	return false
}

// File is the record file of a grant of cls for root.
func File(root, cls string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(Dir(), hex.EncodeToString(sum[:]), cls+".json")
}

// Root is the root a grant is recorded under: the main worktree of the
// repository a project root belongs to.
func Root(root string) string {
	if root == "" {
		return ""
	}
	if main := project.MainWorktree(root); main != "" {
		return main
	}
	return root
}

// Live is whether a live grant of cls covers the project root belongs to.
func Live(proj, cls string) bool {
	root := Root(proj)
	if root == "" || !slices.Contains(Classes, cls) {
		return false
	}
	data, err := os.ReadFile(File(root, cls))
	if err != nil {
		return false
	}
	var rec struct {
		Class   any `json:"class"`
		Root    any `json:"root"`
		Expires any `json:"expires_at"`
	}
	if json.Unmarshal(data, &rec) != nil || rec.Class != cls || rec.Root != root {
		return false
	}
	s, _ := rec.Expires.(string)
	until, ok := isodate.Parse(s)
	return ok && until.After(time.Now())
}

// Record is a grant's record file, its keys in the order written.
type Record struct {
	Class     string `json:"class"`
	Root      string `json:"root"`
	GrantedAt string `json:"granted_at"`
	ExpiresAt string `json:"expires_at"`
}

// Write records rec as two-space-indented JSON in field order, through a
// temporary file renamed into place.
func Write(rec Record) error {
	file := File(rec.Root, rec.Class)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rec); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		return err
	}
	return appendHistory(rec)
}

// HistoryFile is every grant and revocation, one record a line: a record
// file keeps only its class's latest grant for a root, and an audit reads
// a session against the grants live during it, wherever they were run.
func HistoryFile() string { return filepath.Join(Dir(), "history.jsonl") }

// appendHistory appends rec to the history; a revocation is a record that
// expires when it is granted.
func appendHistory(rec Record) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(HistoryFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// History is the history's records in the order written, each grant's
// expiry brought forward to a later revocation of its class and root; an
// unreadable line is skipped.
func History() []Record {
	b, err := os.ReadFile(HistoryFile())
	if err != nil {
		return nil
	}
	var out []Record
	for _, l := range strings.Split(string(b), "\n") {
		var rec Record
		if json.Unmarshal([]byte(l), &rec) != nil || rec.Class == "" {
			continue
		}
		if rec.GrantedAt == rec.ExpiresAt {
			for i := range out {
				if out[i].Class == rec.Class && out[i].Root == rec.Root && out[i].ExpiresAt > rec.ExpiresAt {
					out[i].ExpiresAt = rec.ExpiresAt
				}
			}
			continue
		}
		out = append(out, rec)
	}
	return out
}

// Revoke removes the grant of cls for root, and records it in the
// history; an absent one is no error.
func Revoke(root, cls string) error {
	if err := os.Remove(File(root, cls)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	now := isodate.Format(time.Now())
	return appendHistory(Record{Class: cls, Root: root, GrantedAt: now, ExpiresAt: now})
}

// List is one line per readable record, by root key then class:
// the class padded to 14, the root, and whether it is live until its expiry.
func List() []string {
	dir := Dir()
	var lines []string
	keys, _ := os.ReadDir(dir)
	for _, key := range keys {
		files, err := os.ReadDir(filepath.Join(dir, key.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, key.Name(), f.Name()))
			if err != nil {
				continue
			}
			var rec any
			if json.Unmarshal(data, &rec) != nil || rec == nil {
				continue // an unreadable record grants nothing
			}
			m, _ := rec.(map[string]any)
			if c, _ := m["class"].(string); slices.Contains(Retired, c) {
				continue
			}
			expires, _ := m["expires_at"].(string)
			until, ok := isodate.Parse(expires)
			state := "expired at"
			if ok && until.After(time.Now()) {
				state = "live until"
			}
			cls := js.PadEnd(field(m, "class"), 14)
			lines = append(lines, fmt.Sprintf("%s %s  %s %s", cls, field(m, "root"), state, field(m, "expires_at")))
		}
	}
	return lines
}

// Listed is a readable record and whether it is live now.
type Listed struct {
	Record
	Live bool `json:"live"`
}

// Records is every readable record, by root key then class, as List orders
// them; a record whose fields are not strings grants nothing and is left out.
func Records() []Listed {
	dir := Dir()
	out := []Listed{}
	keys, _ := os.ReadDir(dir)
	for _, key := range keys {
		files, err := os.ReadDir(filepath.Join(dir, key.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, key.Name(), f.Name()))
			if err != nil {
				continue
			}
			var rec Record
			if json.Unmarshal(data, &rec) != nil || rec.Class == "" || rec.Root == "" || slices.Contains(Retired, rec.Class) {
				continue
			}
			until, ok := isodate.Parse(rec.ExpiresAt)
			out = append(out, Listed{rec, ok && until.After(time.Now())})
		}
	}
	return out
}

// field is String(rec[key]): a record's value as JavaScript prints it.
func field(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return "undefined"
	}
	return js.String(v)
}
