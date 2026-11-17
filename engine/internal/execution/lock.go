package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// lockPoll is how often a run waiting for a group's lock tries again.
const lockPoll = 200 * time.Millisecond

// LockGroups takes the lock of each of a project's executions_exclusive
// groups, so the group's projects run one at a time across every run of
// the repository (two sessions, a worker and its verifier), not only within
// one. The lock is per repository, keyed by its main worktree so its linked
// worktrees, which share the same test database, share it too; it lives in
// Jhon's cache and the kernel drops it when the process ends. Groups are
// taken in name order, so two runs never wait on each other. waiting is
// called once with a group another run holds; ok is false when stop closes
// first, and release frees what was taken.
func LockGroups(root string, groups []string, stop <-chan struct{}, waiting func(group string)) (release func(), ok bool) {
	key := project.MainWorktree(root)
	if key == "" {
		key = root
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" || !filepath.IsAbs(cache) {
		cache = filepath.Join(project.Home(), ".cache")
	}
	sum := sha256.Sum256([]byte(key))
	dir := filepath.Join(cache, "jhon", "locks", hex.EncodeToString(sum[:8]))
	var held []*os.File
	release = func() {
		for _, f := range held {
			f.Close() // closing the descriptor drops its lock
		}
	}
	if len(groups) == 0 {
		return release, true
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return release, true // no lock directory: the in-run exclusion still holds
	}
	for _, g := range slices.Sorted(slices.Values(groups)) {
		f, err := os.OpenFile(filepath.Join(dir, g+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			continue
		}
		for told := false; ; {
			err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if err == nil {
				held = append(held, f)
				break
			}
			if !errors.Is(err, syscall.EWOULDBLOCK) {
				f.Close()
				break
			}
			if !told {
				waiting(g)
				told = true
			}
			select {
			case <-stop:
				f.Close()
				release()
				return func() {}, false
			case <-time.After(lockPoll):
			}
		}
	}
	return release, true
}
