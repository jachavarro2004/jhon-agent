// T255 end to end with real pytest and pytest-testmon: a second worktree of
// the repository narrows from the record the first one built. Skipped
// without uv, or when uv cannot run pytest and pytest-testmon from its cache
// offline: this test never reaches the network.
package ldd

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// realUv is the environment in which uv runs pytest-testmon offline, from
// the cache it already has, and the project's own pytest-testmon records
// under cache; it skips the test when uv cannot.
func realUv(t *testing.T, cache string) []string {
	t.Helper()
	uv, err := exec.LookPath("uv")
	if err != nil {
		t.Skip("uv is not on PATH")
	}
	out, err := exec.Command(uv, "cache", "dir").Output()
	if err != nil {
		t.Skip("uv has no cache")
	}
	env := support.Env("XDG_CACHE_HOME="+cache, "UV_CACHE_DIR="+strings.TrimSpace(string(out)), "UV_OFFLINE=1", "UV_NO_PROGRESS=1", "UV_PYTHON_DOWNLOADS=never")
	// The project's test command as an affected run runs it (testCommand).
	probe := exec.Command(uv, "run", "--with", "pytest-testmon", "--no-project", "--with", "pytest", "pytest", "--version")
	probe.Dir, probe.Env = temp(t), env
	if b, err := probe.CombinedOutput(); err != nil {
		t.Skipf("uv cannot run pytest and pytest-testmon offline: %s", strings.TrimSpace(string(b)))
	}
	return env
}

// testCommand runs pytest with no project environment to resolve, so uv
// needs only pytest and pytest-testmon from its cache.
const testCommand = "uv run --no-project --with pytest pytest"

func TestExecutionsASecondWorktreeSelectsOnlyTheAffectedTestsWithRealTestmon(t *testing.T) {
	t.Parallel()
	cache := temp(t)
	env := realUv(t, cache)
	root := tree(t, map[string]string{
		"lib/pyproject.toml":           "[project]\nname = \"lib\"\nversion = \"0.0.0\"\n",
		"lib/a.py":                     "def a():\n    return 1\n",
		"lib/b.py":                     "def b():\n    return 2\n",
		"lib/tests/conftest.py":        "import os, sys\nsys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))\n",
		"lib/tests/test_a.py":          "from a import a\n\n\ndef test_a():\n    assert a() == 1\n",
		"lib/tests/test_b.py":          "from b import b\n\n\ndef test_b():\n    assert b() == 2\n",
		".gitignore":                   ".venv/\n__pycache__/\nuv.lock\n.pytest_cache/\n",
		"openspec/changes/c1/tasks.md": "- [x] done\n",
		"openspec/config.yaml":         "test_command_override:\n  lib: " + testCommand + "\n",
	})
	support.GitRepo(t, root)
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "tag", "base")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"lib/a.py": "def a():\n    return 1  # feat\n"})
	support.Git(t, root, "commit", "-qam", "feat changes a")

	run := func(dir string, args ...string) support.Result {
		t.Helper()
		r := support.Jhon(t, dir, env, append([]string{"executions", "run", "c1", "--affected", "base"}, args...)...)
		equal(t, r.Code, 0, r.Out())
		return r
	}
	first := run(root)
	match(t, first.Stdout, `--testmon-noselect`, "the main checkout's first run builds the record")
	match(t, first.Stdout, `2 passed`)

	wt := filepath.Join(temp(t), "wt")
	support.Git(t, root, "worktree", "add", "-q", "-b", "feat2", wt, "feat")
	support.Write(t, wt, map[string]string{"lib/b.py": "def b():\n    return 1 + 1\n"})
	dry := support.Jhon(t, wt, env, "executions", "run", "c1", "--affected", "base", "--dry-run")
	equal(t, dry.Code, 0, dry.Out())
	match(t, dry.Stdout, `--testmon --rootdir=`, "the second worktree narrows from the shared record")
	noMatch(t, dry.Stdout, `--testmon-noselect|no record`)
	second := run(wt)
	match(t, second.Stdout, `(?m)^collected 1 item$`, "only the test of the changed file runs")
	match(t, second.Stdout, `(?m)^tests/test_b\.py \.`)
	noMatch(t, second.Stdout, `tests/test_a\.py`)
	match(t, second.Stdout, `=+ 1 passed in `)
}
