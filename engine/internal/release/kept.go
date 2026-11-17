package release

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Kept is a release kept on disk, releases/<commit>, with the version its
// binary is stamped with, whether current points at it, and when it was
// placed. `jhon update --rollback` (T6) picks one and re-activates it.
type Kept struct {
	Commit, Version, Dir string
	Active               bool
	Placed               time.Time
}

// KeptReleases are the releases on disk whose binary reports its commit and
// version, oldest placed first.
func KeptReleases(r Roots) []Kept {
	releases := filepath.Join(r.Share, "releases")
	es, err := os.ReadDir(releases)
	if err != nil {
		return nil
	}
	active, _ := ReadlinkF(filepath.Join(r.Share, "current"))
	var out []Kept
	for _, e := range es {
		dir := filepath.Join(releases, e.Name())
		if !commitID.MatchString(e.Name()) || !IsFile(filepath.Join(dir, Binary)) {
			continue
		}
		commit, ver := stamp(dir)
		if commit != e.Name() || ver == "" {
			continue
		}
		fi, err := os.Stat(dir)
		if err != nil {
			continue
		}
		out = append(out, Kept{Commit: commit, Version: ver, Dir: dir, Active: dir == active, Placed: fi.ModTime()})
	}
	slices.SortStableFunc(out, func(a, b Kept) int { return a.Placed.Compare(b.Placed) })
	return out
}

// Reactivate switches current back to the kept release k through the same
// activation an install uses (the renderer's preflight, current, the
// runtime projections). A failed preflight leaves current where it was;
// the release is never removed.
func Reactivate(r Roots, o *Out, k Kept) int {
	if code, ok := activate(r, o, k.Commit, k.Dir, false, false); !ok {
		return code
	}
	sessions(o)
	fmt.Fprintf(o.W, "\nactivated jhon %s from releases/%s\n", k.Version, short(k.Commit))
	return 0
}
