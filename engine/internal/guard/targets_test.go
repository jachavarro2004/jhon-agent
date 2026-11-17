package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitInit is a fresh repository under a temporary directory, symlinks resolved.
func gitInit(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("git", "init", "-q", dir)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			c.Env = append(c.Env, kv)
		}
	}
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return dir
}

// Targets places each program where it acts, by the safety guard's walk of
// cd, pushd, subshells, env -C and git -C (T140, L1085).
func TestTargetsPlaceEachProgramWhereItActs(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	session := gitInit(t, filepath.Join(base, "session"))
	other := gitInit(t, filepath.Join(base, "other"))
	commitRoot := func(command string) []string {
		t.Helper()
		var roots []string
		for _, tg := range Targets(command, session) {
			if tg.Program.Name == "git" {
				roots = append(roots, tg.Root)
			}
		}
		return roots
	}
	for command, want := range map[string][]string{
		"git commit -m x":                          {session},
		"cd " + other + " && git commit -m x":      {other},
		"cd ../other && git commit -m x":           {other},
		"pushd ../other && git commit -m x":        {other},
		"git -C ../other commit -m x":              {other},
		"env -C ../other git commit -m x":          {other},
		"(cd ../other) && git commit -m x":         {session},
		"(cd ../other && git commit -m x)":         {other},
		`cd "$X" && git commit -m x`:               {UnknownRoot},
		`git -C "$X" commit -m x`:                  {UnknownRoot},
		"cd - && git commit -m x":                  {UnknownRoot},
		"bash -c 'cd ../other' && git commit -m x": {UnknownRoot},
		"cd /nowhere-at-all && git commit -m x":    {UnknownRoot},
		"D=../other; cd $D && git commit -m x":     {other},
	} {
		got := commitRoot(command)
		if len(got) != len(want) || (len(got) > 0 && got[0] != want[0]) {
			t.Errorf("%s: roots %q, want %q", command, got, want)
		}
	}
}
