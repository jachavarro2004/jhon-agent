// Package support is what the conformance tests share: the jhon binary they
// run, the repository they read, the process runner, and git fixtures. The
// conformance tests are black-box: they run the binary and judge its output
// and its effects, never call engine packages.
package support

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The binaries the tests run are the tests', not the owner's: they record
// no telemetry (engine/internal/telemetry), and they read the settings of
// the HOME a test gives them (~/.config/jhon/settings.toml), never the
// owner's through an inherited XDG_CONFIG_HOME or JHON_SETTINGS. With no
// file sandbox.mode is on on Linux (T128f): a test that needs the
// unsandboxed path of the Claude Code wrap or doctor sets it off in its own
// settings file, and one that pins the default gives an empty file.
//
// Nor the owner's state folder (T249): Jhon's install record lives there
// (${XDG_STATE_HOME:-~/.local/state}/jhon/install.json) with the grants and
// the modes, so XDG_STATE_HOME is unset here, and every environment a test
// runs a program with (Env, Run) names a state folder of the test's own
// (isolateState).
func init() {
	os.Setenv("JHON_TELEMETRY", "off")
	os.Unsetenv("XDG_CONFIG_HOME")
	os.Unsetenv("JHON_SETTINGS")
	os.Unsetenv("XDG_STATE_HOME")
	os.Unsetenv("JHON_IN_SANDBOX")
}

// callerState and callerHome are the XDG_STATE_HOME and HOME of whoever runs
// the tests, read before init touches the environment.
var (
	callerState = os.Getenv("XDG_STATE_HOME")
	callerHome  = os.Getenv("HOME")
)

// isolateState is env with XDG_STATE_HOME the test's own: kept when env
// names one that is not the caller's; with env's HOME not the caller's, the
// caller's dropped and none added, so the program's state folder is the
// one under that HOME (and a test that unsets the variable keeps it unset);
// else a folder of the test binary's own (binaryState). So no test reads or
// writes the owner's install record, grants or modes.
func isolateState(env []string) []string {
	var home, state string
	var hasHome, hasState bool
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "HOME":
			home, hasHome = v, true
		case "XDG_STATE_HOME":
			state, hasState = v, true
		}
	}
	if hasState && state != "" && state != callerState {
		return env
	}
	ownHome := hasHome && filepath.IsAbs(home) && home != callerHome
	if ownHome && !hasState {
		return env
	}
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); k != "XDG_STATE_HOME" {
			out = append(out, kv)
		}
	}
	if ownHome {
		return out
	}
	return append(out, "XDG_STATE_HOME="+binaryState())
}

var (
	stateOnce   sync.Once
	stateFolder string
)

// binaryState is the state folder of a program a test runs with the
// caller's HOME: a folder beside the test binary when go test built it in
// its work folder (go-build…), which go removes after the run; else a new
// temporary folder.
func binaryState() string {
	stateOnce.Do(func() {
		if exe, err := os.Executable(); err == nil && strings.Contains(exe, string(filepath.Separator)+"go-build") {
			dir := filepath.Join(filepath.Dir(exe), "xdg-state")
			if os.MkdirAll(dir, 0o700) == nil {
				stateFolder = dir
				return
			}
		}
		stateFolder, _ = os.MkdirTemp("", "jhon-test-state-")
	})
	return stateFolder
}

// Root is the repository root. Under -trimpath (a release's `go test
// -trimpath ./...` in `jhon sync`) the source path is module-relative, so the
// root is found from the test's directory instead: the nearest go.mod above it.
func Root() string {
	_, file, _, _ := runtime.Caller(0)
	if filepath.IsAbs(file) {
		return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	}
	dir, _ := os.Getwd()
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return dir
		}
	}
}

// Path is a path under the repository root.
func Path(rel ...string) string { return filepath.Join(append([]string{Root()}, rel...)...) }

// Fixture is a path under test/testdata.
func Fixture(rel ...string) string { return Path(append([]string{"test", "testdata"}, rel...)...) }

var (
	binOnce sync.Once
	binPath string
	binErr  error
)

// Bin is the jhon binary the tests run: JHON_TEST_BIN when set, else the
// tree's own, built once per state of the engine's source and shared by
// every test process: <tmp>/jhon-test-bin/<source hash>/jhon. go test runs
// packages in parallel processes, so a build goes to a directory of its own
// and is renamed into place; one that finds the binary already built uses it.
// Builds of other source states older than an hour are removed. The build
// directory is no input of the tests (the source hash names the binary), so
// it is touched only through calls go test does not record.
// Being outside any tree, it finds the tree it serves through JHON_ROOT: a
// test that runs it on this repository sets JHON_ROOT=Root() itself, never
// for every process, so a scratch source's own CLI never reaches this tree.
func Bin(t testing.TB) string {
	t.Helper()
	binOnce.Do(func() {
		source := watchInputs()
		if p := os.Getenv("JHON_TEST_BIN"); p != "" {
			binPath = p
			return
		}
		builds := filepath.Join(os.TempDir(), "jhon-test-bin")
		dir := filepath.Join(builds, source)
		binPath = filepath.Join(dir, "jhon")
		if exists := syscall.Stat(binPath, &syscall.Stat_t{}) == nil; exists {
			// A build in use stays fresh, so another source state's prune
			// (untouched for an hour) never removes it mid-run (L750).
			now := time.Now()
			_ = os.Chtimes(dir, now, now)
			return
		}
		if err := syscall.Mkdir(builds, 0o755); err != nil && err != syscall.EEXIST {
			binErr = err
			return
		}
		pruneBuilds(builds, source)
		tmp, err := os.MkdirTemp(builds, "build-")
		if err != nil {
			binErr = err
			return
		}
		defer os.RemoveAll(tmp)
		c := exec.Command("go", "build", "-o", filepath.Join(tmp, "jhon"), "./engine/cmd/jhon")
		c.Dir = Root()
		if out, err := c.CombinedOutput(); err != nil {
			binErr = errors.New("go build: " + err.Error() + "\n" + string(out))
			return
		}
		// Another process may have placed the same build first: either is it.
		if err := os.Rename(tmp, dir); err != nil && syscall.Stat(binPath, &syscall.Stat_t{}) != nil {
			binErr = err
		}
	})
	if binErr != nil {
		t.Fatal(binErr)
	}
	return binPath
}

// pruneBuilds removes the builds of other source states, and abandoned
// partial builds, untouched for an hour.
func pruneBuilds(builds, keep string) {
	_ = exec.Command("find", builds, "-mindepth", "1", "-maxdepth", "1", "-mmin", "+60", "!", "-name", keep, "-exec", "rm", "-rf", "{}", "+").Run()
}

// binInputs are what the binary's behaviour depends on: its source (with the
// modules vendored under third_party/), and what it reads at run time from
// the tree (prose/, through JHON_ROOT) and from the fixtures the tests hand
// it.
var binInputs = []string{"engine", "third_party", "prose", filepath.Join("test", "testdata"), "go.mod", "go.sum"}

// watchInputs opens every file and directory of binInputs, and is the hash
// of the binary's source: the paths and contents of engine/, third_party/,
// go.mod, and go.sum. go test caches a package's result by what its own
// process opened; the binary runs in another process, so without this a
// change to the engine or a contract would leave every conformance test
// passing from its cache. An opened file counts by its size and modification
// time, an opened directory by its entries, so a changed, added, or removed
// input reruns the tests.
func watchInputs() string { return inputsHash(Root()) }

// inputsHash is watchInputs over the tree at root.
func inputsHash(root string) string {
	h := sha256.New()
	for _, in := range binInputs {
		source := in != "prose" && in != filepath.Join("test", "testdata")
		_ = filepath.WalkDir(filepath.Join(root, in), func(p string, d os.DirEntry, err error) error {
			if err != nil || !(d.IsDir() || d.Type().IsRegular()) {
				return nil
			}
			if !source || d.IsDir() {
				if f, err := os.Open(p); err == nil {
					f.Close()
				}
				return nil
			}
			if b, err := os.ReadFile(p); err == nil {
				rel, _ := filepath.Rel(root, p)
				fmt.Fprintf(h, "%s %d\n", rel, len(b))
				h.Write(b)
			}
			return nil
		})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Cmd is one process to run.
type Cmd struct {
	Args  []string // program and arguments
	Dir   string   // working directory; the test's when empty
	Env   []string // the full environment; this process's when nil
	Stdin string
}

// Result is how a process ended.
type Result struct {
	Code           int // exit code; -1 when killed by a signal
	Stdout, Stderr string
}

// Out is stdout and stderr, as a shell's 2>&1 would interleave them only
// approximately: stdout first.
func (r Result) Out() string { return r.Stdout + r.Stderr }

// procs bounds the processes one test binary runs at once (L677): parallel
// tests each spawning `jhon hook` took a 16-core machine to load 55 when two
// suites overlapped. JHON_TEST_PROCS sets the bound; by default it is a
// quarter of the CPUs, at least 2. Run holds a slot only while its process
// runs, and no process Run starts waits on another, so the bound cannot
// deadlock.
var procs = make(chan struct{}, procLimit(os.Getenv("JHON_TEST_PROCS"), runtime.NumCPU()))

// procLimit is the bound for env (JHON_TEST_PROCS) on a machine of cpus.
func procLimit(env string, cpus int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(env)); err == nil && n > 0 {
		return n
	}
	return max(2, cpus/4)
}

// Run runs c and returns how it ended; a program that cannot start fails
// the test. At most cap(procs) Run processes of a test binary run at once.
func Run(t testing.TB, c Cmd) Result {
	t.Helper()
	procs <- struct{}{}
	defer func() { <-procs }()
	cmd := exec.Command(c.Args[0], c.Args[1:]...)
	env := c.Env
	if env == nil {
		env = os.Environ()
	}
	cmd.Dir, cmd.Env = c.Dir, isolateState(env)
	cmd.Stdin = strings.NewReader(c.Stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("run %q: %v", c.Args, err)
	}
	code := 0
	if exit != nil {
		code = exit.ExitCode()
	}
	return Result{code, out.String(), errb.String()}
}

// Jhon runs `jhon <args>` with env (this process's when nil) in dir.
func Jhon(t testing.TB, dir string, env []string, args ...string) Result {
	t.Helper()
	return Run(t, Cmd{Args: append([]string{Bin(t)}, args...), Dir: dir, Env: env})
}

// Hook runs `jhon hook <name> <args>` with payload as its JSON stdin.
func Hook(t testing.TB, env []string, name string, payload any, args ...string) Result {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return Run(t, Cmd{Args: append([]string{Bin(t), "hook", name}, args...), Env: env, Stdin: string(b)})
}

// NoEngram is the JHON_ENGRAM every test runs with unless it names its own
// fake: a path that cannot exist, so no test reaches the owner's Engram
// (T134: the engine writes a change's state there at a slice's commit, a
// verify, and an archive).
const NoEngram = "/dev/null/engram"

func init() {
	if _, set := os.LookupEnv("JHON_ENGRAM"); !set {
		os.Setenv("JHON_ENGRAM", NoEngram)
	}
}

// Env is this process's environment with overrides (KEY=VALUE) applied and
// every GIT_* variable removed, so a test run from a git hook never points
// git at the framework's own repository, and XDG_STATE_HOME the test's own
// (isolateState).
func Env(overrides ...string) []string {
	keys := map[string]bool{}
	for _, o := range overrides {
		k, _, _ := strings.Cut(o, "=")
		keys[k] = true
	}
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GIT_") || keys[k] {
			continue
		}
		out = append(out, kv)
	}
	return isolateState(append(out, overrides...))
}

// Git runs git in dir with a fixed identity and no signing; it fails the
// test when git fails and returns its stdout.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	r := Run(t, Cmd{Args: append([]string{"git",
		"-c", "user.name=jhon-test", "-c", "user.email=jhon-test@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...), Dir: dir, Env: Env()})
	if r.Code != 0 {
		t.Fatalf("git %s in %s: %s", strings.Join(args, " "), dir, r.Stderr)
	}
	return r.Stdout
}

// GitInit is `git init` in dir with no commit: HEAD does not resolve.
func GitInit(t testing.TB, dir string) string {
	t.Helper()
	Git(t, dir, "init", "-q")
	return dir
}

// GitRepo is `git init` plus one empty commit: HEAD resolves.
func GitRepo(t testing.TB, dir string) string {
	t.Helper()
	GitInit(t, dir)
	Git(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

// Write writes files (relative path -> content) under dir, creating parents.
func Write(t testing.TB, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o666); err != nil {
			t.Fatal(err)
		}
	}
}

// Read is a file's text; it fails the test when the file cannot be read.
func Read(t testing.TB, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// CopyRepo copies the repository (without .git, node_modules, .codegraph,
// and .claude) into a new temporary directory, with the test binary at its
// build/jhon, for a test that edits the tree it renders or validates.
func CopyRepo(t testing.TB) string {
	t.Helper()
	dst := t.TempDir()
	root := Root()
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == ".git" && !d.IsDir() {
			return nil // a worktree's .git file names the real repository
		}
		// Any node_modules (shell/'s holds tens of thousands of files) is
		// installed, not source: copying it cost each copy ~190 MB and made go
		// test's cache check of every file it read take seconds.
		if d.IsDir() && (rel == ".git" || d.Name() == "node_modules" || rel == ".codegraph" || rel == ".claude") {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, b, info.Mode().Perm())
		}
		return nil // sockets and the like
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Bin(t))
	if err == nil {
		err = os.MkdirAll(filepath.Join(dst, "build"), 0o777)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dst, "build", "jhon"), b, 0o755)
	}
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// RuntimesPath is PATH with stub `claude` and `pi` commands first: both
// runtimes detected (settings.Detected, T10g), as on a machine that has them,
// whatever this one has. A stub exits 1 and prints nothing; a test that runs
// a runtime brings its own. Use it where a test means "the runtimes are
// installed" and writes no runtimes.* setting.
func RuntimesPath(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"claude", "pi"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return "PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")
}
