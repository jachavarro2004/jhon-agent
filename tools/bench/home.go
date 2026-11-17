package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Installer installs Jhon into a bench HOME, with env as the install's
// whole environment.
type Installer interface {
	Install(home string, env []string, out io.Writer) error
}

// releaseInstaller installs the local checkout's release the way the clean
// install test does (test/release/clean_install_test.go): the checkout's
// build/jhon packs an artifact, and the artifact's own jhon runs
// `release install` on it with HOME the bench HOME.
//
// With a ref, the release is that commit's instead (T60: the baseline of
// the installed build, run by T60's bench): a clone of the checkout at the
// commit, in scratch, is built with build and packed. A clone, not a `git
// archive`, because `release pack` archives its source's HEAD and stamps
// that commit, so the source must be a repository at the commit. The
// checkout itself is never changed.
type releaseInstaller struct {
	checkout string
	ref      string
	// build builds <src>/build/jhon; goBuild when nil.
	build func(src string, env []string) error
}

// goBuild is `go build -o build/jhon ./engine/cmd/jhon` in src.
func goBuild(src string, env []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join("build", "jhon"), "./engine/cmd/jhon")
	c.Dir, c.Env = src, env
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("go build in %s: %v\n%s", src, err, out)
	}
	return nil
}

// cloneAt clones checkout into dir with HEAD detached at ref's commit,
// sharing the checkout's objects read only.
func cloneAt(checkout, ref, dir string) error {
	full, err := exec.Command("git", "-C", checkout, "rev-parse", "--verify", "--quiet", ref+"^{commit}").Output()
	if err != nil {
		return fmt.Errorf("--ref %s is no commit of %s", ref, checkout)
	}
	commit := strings.TrimSpace(string(full))
	for _, args := range [][]string{
		{"clone", "--quiet", "--shared", "--no-checkout", checkout, dir},
		{"-C", dir, "checkout", "--quiet", "--detach", commit},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

// installTimeout bounds each step of the install.
const installTimeout = 10 * time.Minute

func (ri releaseInstaller) Install(home string, env []string, out io.Writer) error {
	// As the clean install test: the owner's Go caches, so nothing downloads,
	// and no go test during the install's sync.
	env = append(slices.Clone(env), "JHON_SYNC_GO_TEST=0", "GOPROXY=off", "GOTOOLCHAIN=local")
	if b, err := exec.Command("go", "env", "GOCACHE", "GOMODCACHE").Output(); err == nil {
		if v := strings.Split(strings.TrimSpace(string(b)), "\n"); len(v) == 2 {
			env = append(env, "GOCACHE="+v[0], "GOMODCACHE="+v[1])
		}
	}
	tmp, err := os.MkdirTemp("", "jhon-bench-pack-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if ri.ref != "" {
		src := filepath.Join(tmp, "source")
		if err := cloneAt(ri.checkout, ri.ref, src); err != nil {
			return err
		}
		build := ri.build
		if build == nil {
			build = goBuild
		}
		if err := build(src, env); err != nil {
			return err
		}
		ri.checkout = src
	}
	cli := filepath.Join(ri.checkout, "build", "jhon")
	if _, err := os.Stat(cli); err != nil {
		return fmt.Errorf("no %s: build it first (go build -o build/jhon ./engine/cmd/jhon)", cli)
	}
	step := func(dir string, argv ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
		defer cancel()
		c := exec.CommandContext(ctx, argv[0], argv[1:]...)
		c.Dir, c.Env, c.Stdout, c.Stderr = dir, env, out, out
		if err := c.Run(); err != nil {
			return fmt.Errorf("%s: %v", commandLine(argv), err)
		}
		return nil
	}
	if err := step(ri.checkout, cli, "release", "pack", "--out", tmp); err != nil {
		return err
	}
	artifacts, _ := filepath.Glob(filepath.Join(tmp, "jhon-*.tar.gz"))
	if len(artifacts) != 1 {
		return fmt.Errorf("release pack left %d artifacts in %s", len(artifacts), tmp)
	}
	unpacked := filepath.Join(tmp, "unpacked")
	if err := os.MkdirAll(unpacked, 0o755); err != nil {
		return err
	}
	if err := step(unpacked, "tar", "-xzf", artifacts[0], "build/jhon"); err != nil {
		return err
	}
	return step(home, filepath.Join(unpacked, "build", "jhon"), "release", "install", artifacts[0])
}

// Prepare makes home a bench HOME: it creates it with a .pi directory (so
// the release renders Jhon's pi profile there, as on a machine with pi) and
// installs Jhon into it with an environment that names only it. It refuses
// the owner's own HOME.
func Prepare(home, owner string, base []string, in Installer, out io.Writer) error {
	abs, err := filepath.Abs(home)
	if err != nil {
		return err
	}
	if o, err := filepath.Abs(owner); err == nil && owner != "" && filepath.Clean(o) == filepath.Clean(abs) {
		return fmt.Errorf("%s is your own HOME; the bench needs a HOME of its own", abs)
	}
	if err := os.MkdirAll(filepath.Join(abs, ".pi"), 0o755); err != nil {
		return err
	}
	if err := in.Install(abs, benchEnv(base, abs, filepath.Join(abs, ".cache")), out); err != nil {
		return err
	}
	if _, err := os.Stat(benchJhon(abs)); err != nil {
		return fmt.Errorf("the install left no jhon at %s", benchJhon(abs))
	}
	if v, err := benchCommit(abs); err == nil {
		fmt.Fprintf(out, "the bench HOME's jhon: jhon %s (%s)\n", v.Commit, v.Version)
	}
	fmt.Fprintf(out, "installed Jhon in %s\n\nNext, log in to Claude there once (your own action; the bench never copies credentials):\n\n  HOME=%s claude\n\nthen type /login, finish it, and quit. Check it with: bench login-check --home %s\n",
		abs, shellQuote(abs), shellQuote(abs))
	return nil
}

// jhonVersion is `jhon version --json`.
type jhonVersion struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// benchCommit is the bench HOME's jhon's version and commit.
func benchCommit(home string) (jhonVersion, error) {
	var v jhonVersion
	b, err := exec.Command(benchJhon(home), "version", "--json").Output()
	if err != nil {
		return v, fmt.Errorf("%s version --json: %v", benchJhon(home), err)
	}
	if err := json.Unmarshal(b, &v); err != nil || v.Commit == "" {
		return v, fmt.Errorf("%s version --json names no commit: %s", benchJhon(home), bytes.TrimSpace(b))
	}
	return v, nil
}

// checkCommit is nil when the bench HOME's jhon is the commit expect (a
// prefix of 7 characters or more).
func checkCommit(home, expect string) (jhonVersion, error) {
	v, err := benchCommit(home)
	if expect == "" {
		return v, nil
	}
	if len(expect) < 7 {
		return v, fmt.Errorf("--expect-commit %s: give at least 7 characters of the commit", expect)
	}
	if err != nil {
		return v, err
	}
	if !strings.HasPrefix(v.Commit, expect) {
		return v, fmt.Errorf("the bench HOME's jhon is %s (%s), not %s: run bench prepare --ref %s first", v.Commit[:min(12, len(v.Commit))], v.Version, expect, expect)
	}
	return v, nil
}

// LoginCheck asks the claude on PATH, run with HOME the bench HOME, whether
// it is logged in (`claude auth status --json`, local and free). It reads no
// credential file itself.
func LoginCheck(home string, out io.Writer) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, "claude", "auth", "status", "--json")
	c.Dir, c.Env = home, benchEnv(os.Environ(), home, filepath.Join(home, ".cache"))
	var stderr bytes.Buffer
	c.Stderr = &stderr
	b, err := c.Output()
	login := fmt.Sprintf("Log in once: HOME=%s claude  then type /login", shellQuote(home))
	var status struct {
		LoggedIn *bool `json:"loggedIn"`
	}
	if jerr := json.Unmarshal(b, &status); jerr != nil || status.LoggedIn == nil {
		fmt.Fprintf(out, "could not read `claude auth status --json` in %s (%v %s).\n%s\n", home, err, bytes.TrimSpace(stderr.Bytes()), login)
		return false, nil
	}
	if *status.LoggedIn {
		fmt.Fprintf(out, "the bench HOME %s is logged in to Claude\n", home)
		return true, nil
	}
	fmt.Fprintf(out, "the bench HOME %s is not logged in to Claude.\n%s\n", home, login)
	return false, nil
}
