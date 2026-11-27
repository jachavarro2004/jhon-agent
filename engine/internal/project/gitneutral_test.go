package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// T250 item 2: jhon's own git, run outside the sandbox, runs no program a
// sandboxed command could have named in the repository's configuration: a
// core.fsmonitor, a filter's clean and smudge, a textconv, an external diff,
// a configured hook or core.hooksPath. The plants are live (plain git runs
// each); a `git config` read still answers what the file says.
func TestJhonsOwnGitRunsNoProgramTheRepositoryNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipped: the planted programs are shell scripts")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("skipped: no git")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "r")
	ran := filepath.Join(base, "ran")
	plant := filepath.Join(base, "plant.sh")
	if err := os.WriteFile(plant, []byte("#!/bin/sh\necho \"$1\" >> '"+ran+"'\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(env []string, args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", repo}, args...)...)
		c.Env = env
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	clean := func() []string {
		var env []string
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "GIT_") {
				env = append(env, kv)
			}
		}
		return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	}()
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(clean, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.txt filter=x diff=y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(clean, "add", "-A")
	git(clean, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "a")
	for _, kv := range [][2]string{
		{"core.fsmonitor", plant + " fsmonitor"},
		{"filter.x.clean", plant + " clean"}, {"filter.x.smudge", plant + " smudge"},
		{"diff.y.textconv", plant + " textconv"}, {"diff.external", plant + " external"},
		{"core.hooksPath", base}, {"hook.p.command", plant + " hook"}, {"hook.p.event", "post-index-change"},
		{"core.pager", plant + " pager"},
	} {
		git(clean, "config", kv[0], kv[1])
	}
	if err := os.WriteFile(filepath.Join(base, "post-index-change"), []byte("#!/bin/sh\necho hookspath >> '"+ran+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The control: plain git runs the plants.
	git(clean, "status", "--porcelain")
	c := exec.Command("git", "-C", repo, "diff")
	c.Env = clean
	_ = c.Run()
	if got, _ := os.ReadFile(ran); !strings.Contains(string(got), "fsmonitor") || !strings.Contains(string(got), "clean") || !strings.Contains(string(got), "external") {
		t.Fatalf("the plants are not live: %q", got)
	}
	if err := os.Remove(ran); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	for _, args := range [][]string{
		{"status", "--porcelain", "--untracked-files=all"},
		{"diff"}, {"diff", "--binary", "HEAD"}, {"log", "-p", "-1"}, {"show", "HEAD"},
		{"archive", "--format=tar", "HEAD"},
		{"rev-parse", "HEAD"},
	} {
		if err := ReadGitCmd(repo, args...).Run(); err != nil {
			t.Errorf("ReadGitCmd %v: %v", args, err)
		}
	}
	// GitReadEnv, for the commands that run no filter (it knows no folder to
	// read the filters' names in), runs no fsmonitor, hook or pager.
	for _, args := range [][]string{{"rev-parse", "--show-toplevel"}, {"ls-files"}} {
		c = exec.Command("git", append([]string{"-C", repo}, args...)...)
		c.Env = GitReadEnv()
		_ = c.Run()
	}
	if got, err := os.ReadFile(ran); err == nil {
		t.Errorf("jhon's own git ran what the repository names: %q", got)
	}
	if v, _ := ReadGit(repo, "config", "--get", "core.hooksPath"); v != base {
		t.Errorf("git config --get core.hooksPath = %q, want the file's %q", v, base)
	}
	// The owner's own GIT_CONFIG_COUNT entries are kept, before Jhon's.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "user.name")
	t.Setenv("GIT_CONFIG_VALUE_0", "owner")
	c = exec.Command("git", "-C", repo, "var", "GIT_AUTHOR_IDENT")
	c.Env = append(GitReadEnv(), "GIT_AUTHOR_EMAIL=o@example.invalid")
	if out, err := c.Output(); err != nil || !strings.HasPrefix(string(out), "owner ") {
		t.Errorf("the owner's GIT_CONFIG_COUNT entries: %q %v", out, err)
	}
}

// T250 (the final re-verify of 62839a7): jhon's own git honours no replace
// ref a sandboxed command wrote: ReadGit reads the object itself, and
// GitReadEnv and NeutralGitEnv carry GIT_NO_REPLACE_OBJECTS=1.
func TestJhonsOwnGitHonoursNoReplaceRef(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("skipped: no git")
	}
	repo := t.TempDir()
	run := func(stdin string, args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		c.Stdin = strings.NewReader(stdin)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("", "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("", "add", "-A")
	run("", "commit", "-q", "-m", "one")
	run("", "replace", run("", "rev-parse", "HEAD:go.mod"), run("module planted\n", "hash-object", "-w", "--stdin"))
	if got := run("", "show", "HEAD:go.mod"); got != "module planted" {
		t.Fatalf("the plant is not live: %q", got)
	}
	if got, _ := ReadGit(repo, "show", "HEAD:go.mod"); got != "module real" {
		t.Errorf("ReadGit show HEAD:go.mod = %q, want the object itself", got)
	}
	for name, env := range map[string][]string{"GitReadEnv": GitReadEnv(), "NeutralGitEnv": NeutralGitEnv(repo)} {
		if !slices.Contains(env, "GIT_NO_REPLACE_OBJECTS=1") {
			t.Errorf("%s carries no GIT_NO_REPLACE_OBJECTS=1", name)
		}
	}
}

// T250: every git command that may run a filter driver is among
// filterCommands, `mv` included (it cleans the moved path through the index).
func TestFilterCommandsHoldEveryCommandThatRunsAFilter(t *testing.T) {
	for _, c := range []string{"mv", "rm", "status", "diff", "archive", "add"} {
		if !slices.Contains(filterCommands, c) {
			t.Errorf("git %s may run a filter driver and is not among filterCommands", c)
		}
	}
}
