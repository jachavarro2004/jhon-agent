package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// link points p.Link at version's program, through the folder that holds
// the link (os.Root): a new link is made beside it and moved over it
// atomically, and only while p.Link is absent or jhon's link to a version
// (ours). Where the system can (Linux, swap), the move itself never
// replaces anything else: a file or a link swapped in between the check
// and the move is swapped back, left as it is, and refuses it.
func (t Tool) link(p Place, version string) error {
	dir, name := filepath.Dir(p.Link), filepath.Base(p.Link)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer r.Close()
	tmp := fmt.Sprintf(".%s.jhon-%d.tmp", name, os.Getpid())
	if fi, err := r.Lstat(tmp); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		r.Remove(tmp)
	}
	if err := r.Symlink(t.program(p, version), tmp); err != nil {
		return err
	}
	foreign := func() error {
		_, why := t.linkState(p)
		if why == "" {
			why = p.Link + " is not jhon's link to a " + t.Binary + " release"
		}
		return errors.New(became(why))
	}
	ownsName := func(n string) bool {
		target, err := r.Readlink(n)
		_, ok := t.ours(p, target)
		return err == nil && ok
	}
	if _, why := t.linkState(p); why != "" {
		r.Remove(tmp)
		return errors.New(became(why))
	}
	if hookBeforeLink != nil {
		hookBeforeLink()
	}
	switch err := swapIn(r, tmp, name); {
	case err == nil:
		// tmp now holds what was at name: ours, or put back.
		if ownsName(tmp) {
			r.Remove(tmp)
			return nil
		}
		if hookBeforeSwapBack != nil {
			hookBeforeSwapBack()
		}
		if err := swapIn(r, tmp, name); err != nil {
			// What was swapped in stays at tmp: said where (T247).
			return fmt.Errorf("%s was replaced during the install and could not be put back: it is at %s (%v)", p.Link, filepath.Join(dir, tmp), err)
		}
		r.Remove(tmp)
		return foreign()
	case errors.Is(err, errAbsent):
		if err := placeNew(r, tmp, name); err != nil {
			r.Remove(tmp)
			if errors.Is(err, os.ErrExist) {
				return foreign()
			}
			return err
		}
		return nil
	case errors.Is(err, errNoSwap):
		// No atomic swap here: the check stands for the move.
		if _, why := t.linkState(p); why != "" {
			r.Remove(tmp)
			return errors.New(became(why))
		}
		return r.Rename(tmp, name)
	default:
		r.Remove(tmp)
		return err
	}
}

// errAbsent is swapIn's answer when nothing is at the name; errNoSwap when
// the system cannot swap two names.
var (
	errAbsent = errors.New("nothing to swap with")
	errNoSwap = errors.New("no atomic swap")
)
