package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// staleLockAge is how old an empty lock must be before the doctor calls it
// stale: a git command holds one for well under a second.
const staleLockAge = 2 * time.Minute

// staleLocks are the warning lines for the git locks of the work tree top
// that a killed git left behind (L320): index.lock and HEAD.lock in its own
// git dir, index.lock in the common one, each empty, older than
// staleLockAge, and, where processes can be read, with no git process
// running inside top or its git dirs. The doctor names them and never
// removes one.
func staleLocks(top string, now time.Time) []string {
	dirs := map[string]bool{}
	var locks []string
	for _, which := range []string{"--git-dir", "--git-common-dir"} {
		b, err := project.ReadGitCmd(top, "rev-parse", "--path-format=absolute", which).Output()
		d := strings.TrimSpace(string(b))
		if err != nil || d == "" || dirs[d] {
			continue
		}
		dirs[d] = true
		names := []string{"HEAD.lock", "index.lock"}
		if which == "--git-common-dir" {
			names = []string{"index.lock"}
		}
		for _, n := range names {
			locks = append(locks, filepath.Join(d, n))
		}
	}
	var stale []string
	for _, l := range locks {
		st, err := os.Lstat(l)
		if err != nil || !st.Mode().IsRegular() || st.Size() != 0 || now.Sub(st.ModTime()) < staleLockAge {
			continue
		}
		age := now.Sub(st.ModTime()).Truncate(time.Minute).String()
		age = strings.TrimSuffix(age, "0s")
		fix := "; close whatever runs git there, then: rm " + l
		roots := append([]string{top}, keys(dirs)...)
		switch running, checked := gitRunningIn(roots); {
		case !checked:
			stale = append(stale, fmt.Sprintf("possibly stale git lock %s: 0 bytes, %s old (could not check processes)%s", l, age, fix))
		case !running:
			stale = append(stale, fmt.Sprintf("stale git lock %s: 0 bytes, %s old, no git process running in %s%s", l, age, top, fix))
		}
	}
	return stale
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// gitRunningIn is whether a process named git has its cwd inside one of
// roots; checked is false where processes cannot be read (no Linux /proc).
func gitRunningIn(roots []string) (running, checked bool) {
	if runtime.GOOS != "linux" {
		return false, false
	}
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return false, false
	}
	for _, p := range procs {
		comm, err := os.ReadFile(filepath.Join("/proc", p.Name(), "comm"))
		if err != nil || strings.TrimSpace(string(comm)) != "git" {
			continue
		}
		cwd, err := os.Readlink(filepath.Join("/proc", p.Name(), "cwd"))
		if err != nil {
			continue
		}
		for _, r := range roots {
			if cwd == r || strings.HasPrefix(cwd, r+string(filepath.Separator)) {
				return true, true
			}
		}
	}
	return false, true
}
