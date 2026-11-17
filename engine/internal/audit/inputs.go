package audit

import (
	"fmt"
	lddlayout "github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
)

// What an audit reads besides the session's transcripts (S14): a change's
// records (its directory, active and archived), a background command's
// output file, the grants' history, and its repositories' git state (which
// commits exist, which worktrees). An audit run with AuditTracked names them,
// so a cache of its output can tell when one of them moved.

// Inputs is what one audit read besides the session's transcripts.
type Inputs struct {
	// Trees are files and directories read whole: a directory with
	// everything under it.
	Trees []string
	// Stats are files and directories whose presence or entries were read.
	Stats []string
	// Exists are paths only whether each exists, and is a directory, was read.
	Exists []string
	// Repos are common git directories whose commits were read: their refs and HEADs.
	Repos []string
}

type tracker struct {
	mu                          sync.Mutex
	trees, stats, exists, repos map[string]bool
}

// tracking is the tracker of the audit AuditTracked runs; nil outside one.
var tracking atomic.Pointer[tracker]

// trackOne keeps one tracked audit at a time.
var trackOne sync.Mutex

func track(add func(t *tracker)) {
	if t := tracking.Load(); t != nil {
		t.mu.Lock()
		add(t)
		t.mu.Unlock()
	}
}

// readTree marks p read whole.
func readTree(p string) { track(func(t *tracker) { t.trees[p] = true }) }

// readStat marks p's presence or entries read.
func readStat(p string) { track(func(t *tracker) { t.stats[p] = true }) }

// readExists marks only whether p exists, and is a directory, read.
func readExists(p string) { track(func(t *tracker) { t.exists[p] = true }) }

// readRepo marks the commits of the repository whose common git directory is common read.
func readRepo(common string) {
	if common != "" {
		track(func(t *tracker) { t.repos[common] = true })
	}
}

// AuditTracked is Audit, and what it read besides the session's transcripts.
func AuditTracked(mainPath string) (*Result, Inputs) {
	trackOne.Lock()
	defer trackOne.Unlock()
	t := &tracker{trees: map[string]bool{}, stats: map[string]bool{}, exists: map[string]bool{}, repos: map[string]bool{}}
	tracking.Store(t)
	defer tracking.Store(nil)
	r := Audit(mainPath)
	keys := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	return r, Inputs{keys(t.trees), keys(t.stats), keys(t.exists), keys(t.repos)}
}

// trackChange marks a change's records under root read: its directory, the
// archive's entries, and each archived copy of it.
func trackChange(root, change string) {
	if tracking.Load() == nil {
		return
	}
	l := lddlayout.For(root)
	readTree(lddlayout.At(root, l.ChangeDir(change)))
	archive := lddlayout.At(root, l.ArchiveDir())
	readStat(archive)
	entries, _ := os.ReadDir(archive)
	for _, e := range entries {
		if d := e.Name(); d == change || strings.HasSuffix(d, "-"+change) {
			readTree(filepath.Join(archive, d))
		}
	}
}

// repoOf is guard.RepoOf, each `.git` it looked for marked read.
func repoOf(abs string) string {
	top := guard.RepoOf(abs)
	if tracking.Load() == nil {
		return top
	}
	for d := filepath.Dir(abs); ; d = filepath.Dir(d) {
		readExists(filepath.Join(d, ".git"))
		if d == top || filepath.Dir(d) == d {
			break
		}
	}
	return top
}

// Encode is in as text Decode reads; ok is false when a path cannot be
// written as one line.
func (in Inputs) Encode() (string, bool) {
	var b strings.Builder
	for _, l := range []struct {
		kind  string
		paths []string
	}{{"t", in.Trees}, {"s", in.Stats}, {"e", in.Exists}, {"g", in.Repos}} {
		for _, p := range l.paths {
			if strings.ContainsAny(p, "\n\r") {
				return "", false
			}
			b.WriteString(l.kind + " " + p + "\n")
		}
	}
	return b.String(), true
}

// DecodeInputs is the Inputs Encode wrote.
func DecodeInputs(s string) Inputs {
	var in Inputs
	for _, l := range strings.Split(s, "\n") {
		kind, p, ok := strings.Cut(l, " ")
		switch {
		case !ok:
		case kind == "t":
			in.Trees = append(in.Trees, p)
		case kind == "s":
			in.Stats = append(in.Stats, p)
		case kind == "e":
			in.Exists = append(in.Exists, p)
		case kind == "g":
			in.Repos = append(in.Repos, p)
		}
	}
	return in
}

// Key is the identity of in as it is on disk now: each path's size and
// mtime (a tree's, every file and directory under it), and each
// repository's refs and HEADs. Stat calls only.
func (in Inputs) Key() string {
	key, _ := in.keyAndNewest()
	return key
}

// KeyAsOf is Key, and ok false when an input was modified after since (less
// a margin for a coarse mtime): an audit that started at since may have read
// it before, so its output is not the one of the inputs Key names.
func (in Inputs) KeyAsOf(since time.Time) (key string, ok bool) {
	key, newest := in.keyAndNewest()
	return key, newest.Before(since.Add(-2 * time.Second))
}

func (in Inputs) keyAndNewest() (string, time.Time) {
	var b strings.Builder
	var newest time.Time
	stat := func(p string) {
		if info, err := os.Stat(p); err != nil {
			fmt.Fprintf(&b, "%s\x00missing\n", p)
		} else {
			fmt.Fprintf(&b, "%s\x00%d\x00%d\x00%v\n", p, info.Size(), info.ModTime().UnixNano(), info.IsDir())
			if info.ModTime().After(newest) {
				newest = info.ModTime()
			}
		}
	}
	walk := func(p string) {
		stat(p)
		_ = filepath.WalkDir(p, func(f string, d fs.DirEntry, err error) error {
			if err == nil && f != p {
				stat(f)
			}
			return nil
		})
	}
	for _, p := range in.Trees {
		walk(p)
	}
	for _, p := range in.Stats {
		stat(p)
	}
	for _, p := range in.Exists {
		info, err := os.Stat(p)
		fmt.Fprintf(&b, "%s\x00%v\x00%v\n", p, err == nil, err == nil && info.IsDir())
	}
	for _, g := range in.Repos {
		for _, f := range []string{"HEAD", "packed-refs", "worktrees"} {
			stat(filepath.Join(g, f))
		}
		walk(filepath.Join(g, "refs"))
		entries, _ := os.ReadDir(filepath.Join(g, "worktrees"))
		for _, e := range entries {
			stat(filepath.Join(g, "worktrees", e.Name(), "HEAD"))
		}
	}
	return b.String(), newest
}
