package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The deterministic gates of `bench certify --gates <checkout>` (T60): the
// T59 mode matrix, the full Go suite, the shell suite and its type check,
// each run once, in an environment of its own the way the repo's tests run
// (env -i with a scratch HOME, XDG dirs, TMPDIR, git config and Go cache;
// the module cache read-only and offline), under nice -n 19 with
// GOMAXPROCS=2 and go test -p 1 (the owner's CPU rule).

// Gate is one deterministic check: its command, where it runs, its env.
type Gate struct {
	Name string
	Dir  string
	Argv []string
	Env  []string
}

// GateResult is how a gate ended: its exit code and its output's tail.
type GateResult struct {
	Exit int
	Tail string
}

// GateRunner runs a gate.
type GateRunner interface {
	Run(g Gate) GateResult
}

// gateEnv is the gates' whole environment: nothing of the caller's but the
// tools' directories on PATH and the Go module cache, read-only.
func gateEnv(scratch string) []string {
	path := []string{"/usr/bin", "/bin"}
	for _, tool := range []string{"go", "node", "npm", "npx"} {
		if p, err := exec.LookPath(tool); err == nil {
			if d := filepath.Dir(p); !strings.Contains(":"+strings.Join(path, ":")+":", ":"+d+":") {
				path = append(path, d)
			}
		}
	}
	modcache := filepath.Join(scratch, "gomodcache")
	if b, err := exec.Command("go", "env", "GOMODCACHE").Output(); err == nil && strings.TrimSpace(string(b)) != "" {
		modcache = strings.TrimSpace(string(b))
	}
	// npm's cache, the caller's when npm names one: the shell deps gate
	// installs from it first (--prefer-offline), never into the caller's HOME.
	npmCache := filepath.Join(scratch, "npm-cache")
	if b, err := exec.Command("npm", "config", "get", "cache").Output(); err == nil && filepath.IsAbs(strings.TrimSpace(string(b))) {
		npmCache = strings.TrimSpace(string(b))
	}
	return []string{
		"HOME=" + filepath.Join(scratch, "home"), "PATH=" + strings.Join(path, ":"),
		"XDG_CONFIG_HOME=" + filepath.Join(scratch, "xdg", "config"), "XDG_DATA_HOME=" + filepath.Join(scratch, "xdg", "data"),
		"XDG_STATE_HOME=" + filepath.Join(scratch, "xdg", "state"), "XDG_CACHE_HOME=" + filepath.Join(scratch, "xdg", "cache"),
		"TMPDIR=" + filepath.Join(scratch, "tmp"), "GIT_CONFIG_GLOBAL=" + filepath.Join(scratch, "gitconfig"), "GIT_CONFIG_NOSYSTEM=1",
		"GOCACHE=" + filepath.Join(scratch, "gocache"), "GOMODCACHE=" + modcache, "GOPROXY=off", "GOFLAGS=-mod=readonly", "GOTOOLCHAIN=local",
		"GOMAXPROCS=2", "npm_config_cache=" + npmCache,
	}
}

// gatesOf are the gates of the checkout repo, with env.
func gatesOf(repo string, env []string) []Gate {
	nice := func(argv ...string) []string { return append([]string{"nice", "-n", "19"}, argv...) }
	shell := filepath.Join(repo, "shell")
	return []Gate{
		{Name: "mode matrix", Dir: repo, Argv: nice("go", "test", "-p", "1", "-count=1", "-run", "TestModeMatrix", "./test/guards/"), Env: env},
		{Name: "go suite", Dir: repo, Argv: nice("go", "test", "-p", "1", "./..."), Env: env},
		// The shell's devDependencies, installed (npm ci) when missing or stale (L564).
		{Name: "shell deps", Dir: repo, Argv: nice("sh", filepath.Join(repo, "tools", "shell-deps.sh"), shell), Env: env},
		{Name: "shell suite", Dir: shell, Argv: nice("sh", "-c", "node --experimental-strip-types --test test/*.test.ts"), Env: env},
		{Name: "tsc", Dir: shell, Argv: nice("npx", "--no-install", "tsc", "-p", "."), Env: env},
	}
}

// runGates runs every gate of repo once with runner, in a scratch
// environment it removes afterwards.
func runGates(repo string, runner GateRunner) []GateVerdict {
	scratch, err := os.MkdirTemp("", "jhon-certify-")
	if err != nil {
		return []GateVerdict{{Name: "scratch", Exit: -1, Tail: err.Error()}}
	}
	defer os.RemoveAll(scratch)
	for _, d := range []string{"home", "tmp", "xdg"} {
		_ = os.MkdirAll(filepath.Join(scratch, d), 0o755)
	}
	_ = os.WriteFile(filepath.Join(scratch, "gitconfig"), nil, 0o644)
	var out []GateVerdict
	for _, g := range gatesOf(repo, gateEnv(scratch)) {
		r := runner.Run(g)
		out = append(out, GateVerdict{Name: g.Name, Command: "(cd " + g.Dir + " && " + commandLine(g.Argv) + ")", Pass: r.Exit == 0, Exit: r.Exit, Tail: r.Tail})
	}
	return out
}

// gateTimeout bounds one gate.
const gateTimeout = 3 * time.Hour

// execGates runs a gate for real.
type execGates struct{}

func (execGates) Run(g Gate) GateResult {
	ctx, cancel := context.WithTimeout(context.Background(), gateTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, g.Argv[0], g.Argv[1:]...)
	c.Dir, c.Env = g.Dir, g.Env
	b, err := c.CombinedOutput()
	r := GateResult{Tail: tail(string(b), 20)}
	if err != nil {
		r.Exit = -1
		if e, ok := err.(*exec.ExitError); ok {
			r.Exit = e.ExitCode()
		}
		if r.Tail == "" {
			r.Tail = err.Error()
		}
	}
	return r
}

// tail is the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.Join(lines[max(len(lines)-n, 0):], "\n")
}
