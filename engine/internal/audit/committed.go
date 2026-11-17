package audit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A slice delivered as a commit (contracts/ldd.md, "Slices") is tested
// uncommitted, then committed as it was tested: its execution record's
// candidate is the commit before, unclean, and its working tree is the tree
// of the commit that follows. That run is a passing run of that commit.

var nextCommits sync.Map // root, commit, tree → the commit found

// committedAsTimeout is how long committedAs waits for git.
var committedAsTimeout = 10 * time.Second

// committedAsCommits bounds how many commits committedAs reads, newest first.
const committedAsCommits = 100000

// committedAs is the commit of dir's repository whose parent is commit and
// whose tree is tree: the commit that committed the tree a run tested on top
// of commit; "" when none does, or git cannot tell in time. Only a commit
// found is kept: a later commit may still commit the tree. Read-only.
func committedAs(dir, commit, tree string) string {
	if commit == "" || tree == "" {
		return ""
	}
	if top := repoOf(filepath.Join(dir, "x")); top != "" {
		readRepo(commonDir(top))
	}
	key := dir + "\x00" + commit + "\x00" + tree
	if v, ok := nextCommits.Load(key); ok {
		return v.(string)
	}
	ctx, cancel := context.WithTimeout(context.Background(), committedAsTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "git", "-C", dir, "log", "--all", "--max-count="+strconv.Itoa(committedAsCommits), "--format=%H %T %P")
	c.WaitDelay = committedAsTimeout / 10
	c.Env = []string{"GIT_OPTIONAL_LOCKS=0"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			c.Env = append(c.Env, kv)
		}
	}
	out, err := c.Output()
	if err != nil || ctx.Err() != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[1] == tree && f[2] == commit {
			nextCommits.Store(key, f[0])
			return f[0]
		}
	}
	return ""
}
