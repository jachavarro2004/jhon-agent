// Package workspace is the multi-repository workspace (T57 W2, L1299): a
// primary repository names its sibling repositories in its LDD config,
//
//	workspace:
//	  lib: ../lib
//
// each a name ([a-z0-9-]+) and a path relative to the primary's top (its
// main worktree); a sibling joins with the owner's `jhon workspace join
// <primary>`, a pointer under ${XDG_STATE_HOME:-~/.local/state}/jhon/workspace/
// <sha256 of the sibling's main worktree>.json, so its own files never
// change. The guards keep that folder from every agent (grant.InGrants).
// A member counts only when its pointer names a primary whose config lists a
// path resolving to it. This slice only reports: no guard, root or delivery
// reads it yet.
package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// The roles a repository plays.
const (
	Primary = "primary"
	Member  = "member"
	None    = "none"
)

var memberName = lazyre.New(`^[a-z0-9-]+$`)

// A Pointer is a sibling's record that it joined the primary's workspace.
type Pointer struct {
	Root     string `json:"root"`
	Primary  string `json:"primary"`
	JoinedAt string `json:"joined_at"`
}

// A Listed member is one name in the primary's config: its path (absolute)
// and whether its pointer names the primary.
type Listed struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Joined bool   `json:"joined"`
	top    string // the main worktree the path resolves to; "" when it is no git top
}

// Info is a repository's workspace: its role, the primary, the members (as
// `jhon status --json` prints them), and, not printed, this repository's
// name as a member, why it is none, and the notes status adds.
type Info struct {
	Role    string   `json:"role"`
	Primary string   `json:"primary"`
	Members []Listed `json:"members"`
	Name    string   `json:"-"`
	Reason  string   `json:"-"`
	Notes   []string `json:"-"`
}

// File is the pointer file of root, a main worktree.
func File(root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(grant.WorkspaceDir(), hex.EncodeToString(sum[:])+".json")
}

// Main is the main worktree of the repository dir is in; "" for no repository.
func Main(dir string) string {
	return grant.Root(project.Root(dir))
}

// PointerOf is root's pointer: unreadable, of another root or naming no
// primary, none.
func PointerOf(root string) (Pointer, bool) {
	b, err := os.ReadFile(File(root))
	if err != nil {
		return Pointer{}, false
	}
	var p Pointer
	if json.Unmarshal(b, &p) != nil || p.Root != root || p.Primary == "" {
		return Pointer{}, false
	}
	return p, true
}

// Join records that root joined primary's workspace, both main worktrees,
// through a temporary file renamed into place.
func Join(root, primary string) error {
	file := File(root)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(Pointer{Root: root, Primary: primary, JoinedAt: isodate.Format(time.Now())}); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// Leave removes root's pointer, the one it had, and whether it had one.
func Leave(root string) (Pointer, bool, error) {
	p, ok := PointerOf(root)
	err := os.Remove(File(root))
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	return p, ok, err
}

// Pointers are every readable pointer, by file name; a file that is not
// its own root's is none.
func Pointers() []Pointer {
	entries, _ := os.ReadDir(grant.WorkspaceDir())
	out := []Pointer{}
	for _, e := range entries {
		file := filepath.Join(grant.WorkspaceDir(), e.Name())
		b, err := os.ReadFile(file)
		var p Pointer
		if err != nil || json.Unmarshal(b, &p) != nil || p.Root == "" || p.Primary == "" || File(p.Root) != file {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Members are the members primary's LDD config lists, in its order, and the
// notes on the entries it ignores or cannot follow; ok is false when the
// config has no `workspace:` key. A path is followed only to a git
// repository's top, never up from it.
func Members(primary string) (members []Listed, notes []string, ok bool) {
	text, err := project.ReadLddConfig(primary)
	if err != nil {
		return nil, nil, false
	}
	m := discover.ConfigMap(text, "workspace")
	if m == nil {
		return nil, nil, false
	}
	members = []Listed{}
	for _, name := range m.Keys {
		rel := m.Values[name]
		switch {
		case !memberName.MatchString(name):
			notes = append(notes, fmt.Sprintf("workspace member name '%s' is not [a-z0-9-]+; ignored", name))
			continue
		case filepath.IsAbs(rel):
			notes = append(notes, fmt.Sprintf("workspace member %s: %s is not a path relative to the primary's top; ignored", name, rel))
			continue
		}
		l := Listed{Name: name, Path: project.Realish(filepath.Join(primary, rel))}
		if repo := project.ReadRepo(l.Path); repo.Top != "" && repo.Top == l.Path {
			l.top = Main(l.Path)
			p, has := PointerOf(l.top)
			l.Joined = has && p.Primary == primary
		}
		members = append(members, l)
	}
	return members, notes, true
}

// Of is the workspace of the repository root is in: primary when its main
// worktree's LDD config has `workspace:`, member when its pointer names a
// primary that lists it, none otherwise (with the reason, and a note when a
// pointer names a primary that does not list it).
func Of(root string) Info {
	main := Main(root)
	if main == "" {
		return Info{Role: None, Reason: "no git repository"}
	}
	if members, notes, ok := Members(main); ok {
		for _, l := range members {
			switch {
			case l.top == "":
				notes = append(notes, fmt.Sprintf("workspace member %s: %s is not the top of a git repository; not followed", l.Name, l.Path))
			case !l.Joined:
				notes = append(notes, fmt.Sprintf("workspace member %s (%s) has not joined: the owner runs `jhon workspace join %s` in it", l.Name, l.Path, main))
			}
		}
		return Info{Role: Primary, Primary: main, Members: members, Notes: notes}
	}
	p, ok := PointerOf(main)
	if !ok {
		return Info{Role: None, Reason: "no workspace pointer"}
	}
	leave := " (the owner's `jhon workspace leave` removes the pointer)"
	if Main(p.Primary) != p.Primary {
		why := "workspace pointer names " + p.Primary + ", which is no git repository's main worktree: not a member"
		return Info{Role: None, Reason: why, Notes: []string{why + leave}}
	}
	members, _, _ := Members(p.Primary)
	for _, l := range members {
		if l.top == main {
			return Info{Role: Member, Primary: p.Primary, Members: members, Name: l.Name}
		}
	}
	why := "workspace pointer names " + p.Primary + ", whose LDD config lists no path to this repository: not a member"
	return Info{Role: None, Reason: why, Notes: []string{why + leave}}
}
