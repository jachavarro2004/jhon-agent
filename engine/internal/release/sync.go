package release

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/session"
	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
)

// git runs git in dir as jhon's own reads run it (project.ReadGitCmd: no
// inherited GIT_* variable, and no fsmonitor, hook, filter or other program
// the repository's configuration names, which a sandboxed command may have
// written, T250); its stderr reaches this process's unless quiet. It
// returns the substituted stdout and whether git exited 0.
func git(dir string, quiet bool, args ...string) (string, bool) {
	c := project.ReadGitCmd(dir, args...)
	if !quiet {
		c.Stderr = os.Stderr
	}
	out, err := c.Output()
	return Substituted(out), err == nil
}

// dirty is `[ -n "$(git -C <source> status --porcelain)" ]`.
func dirty(source string, quiet bool) bool {
	out, _ := git(source, quiet, "status", "--porcelain")
	return out != ""
}

// SourceMarker is the file that makes a directory the framework's source.
const SourceMarker = "engine/cmd/jhon/main.go"

// NeedRepo is whether the source holds the binary's source; when it does
// not, what (the command) is refused in one line naming the checkout and,
// for a user who has only the release, `jhon update` (L51).
func (r Roots) NeedRepo(o *Out, what string) (int, bool) {
	if !r.HasSource() {
		return o.Die(NoSource(what, r.Source)), false
	}
	return 0, true
}

// HasSource is whether the source holds the binary's source: the
// maintainer's checkout. A user's install has only the release (L51).
func (r Roots) HasSource() bool { return IsFile(filepath.Join(r.Source, SourceMarker)) }

// NoSource is the line a command that needs the checkout refuses with.
func NoSource(what, source string) string {
	return fmt.Sprintf("%s needs the Jhon source checkout, not found at %s (run 'jhon sync' from your checkout once, or set JHON_SOURCE); to update an installed Jhon run 'jhon update'", what, Tilde(source))
}

// NeedNode is whether node is on PATH, for what still runs node: `jhon test`.
func NeedNode(o *Out) (int, bool) {
	if !HasNode() {
		return o.Die("node not found; it is required to render and validate"), false
	}
	return 0, true
}

// Sync is `jhon sync` (docket AV1) after its argument check: build the
// release of the source's committed HEAD unless it exists, build its binary
// (build/jhon, which every rendered hook runs) unless it has one, preflight
// it (its Go tests, then the renderer's preflight), switch current to it,
// install from it, and report the sessions that predate the install. A
// release this sync built that fails the build or the preflight is removed,
// and current stays where it was. It pushes nothing.
func Sync(r Roots, o *Out) int {
	if code, ok := r.NeedRepo(o, "sync"); !ok {
		return code
	}
	if dirty(r.Source, false) {
		return o.Die("working tree is dirty; commit first (sync activates, it does not commit)")
	}
	commit, ok := git(r.Source, false, "rev-parse", "--verify", "HEAD")
	if !ok {
		return o.Die("cannot resolve HEAD in " + r.Source)
	}
	release := filepath.Join(r.Share, "releases", commit)
	built := false
	if !IsDir(release) {
		if code, ok := build(r, o, commit, release); !ok {
			return code
		}
		built = true
	}
	if !IsFile(filepath.Join(release, Binary)) {
		if out, ok := buildBinary(release, r.Source, commit, DevVersion(commit)); !ok {
			fmt.Fprintf(o.E, "%s\n", out)
			if built {
				os.RemoveAll(release)
			}
			return o.Die(fmt.Sprintf("release %s failed to build %s; nothing was activated", commit, Binary))
		}
	}
	if code, ok := activate(r, o, commit, release, built, true); !ok {
		return code
	}
	recordSource(r, o)
	if dirty(r.Source, false) {
		return o.Die("the install changed tracked files in the source; review them")
	}
	sessions(o)
	fmt.Fprint(o.W, "\nactivated locally; nothing was pushed (publish with 'jhon publish')\n")
	return 0
}

// recordSource writes the checkout sync ran from into the install record,
// where Locate finds it when the installed CLI runs outside a checkout and
// JHON_SOURCE is unset. With no record (no runtime installed) there is
// nothing to add it to.
func recordSource(r Roots, o *Out) {
	src, err := filepath.Abs(r.Source)
	if err != nil {
		src = r.Source
	}
	home := project.Home()
	rec, ok := session.ReadInstall(home)
	if !ok || rec.Source == src {
		return
	}
	rec.Source = src
	if err := session.WriteInstall(home, rec); err != nil {
		o.Warn(fmt.Sprintf("cannot record the source checkout in %s: %v", Tilde(filepath.Join(home, session.InstallRecord)), err))
	}
}

// activate is the half sync and an artifact's install share: preflight the
// release at releases/<commit> (its Go tests too when tests is set, which
// needs the toolchain an artifact's install does without), switch current
// to it, and install the runtime projections from it. A release this run
// placed (built) that fails the preflight is removed, and current stays.
func activate(r Roots, o *Out, commit, release string, built, tests bool) (int, bool) {
	failed := func(out, why string) (int, bool) {
		fmt.Fprintf(o.E, "%s\n", out)
		if built {
			os.RemoveAll(release)
		}
		return o.Die(fmt.Sprintf("release %s %s; nothing was activated", commit, why)), false
	}
	fmt.Fprintf(o.W, "release %s:\n", Tilde(release))
	if tests {
		if out, ok := SourceChecks(release, r.Source, commit); !ok {
			return failed(out, "failed the preflight")
		}
	}
	out, ok := Preflight(release, r.Source)
	if !ok {
		return failed(out, "failed the preflight")
	}
	fmt.Fprintf(o.W, "  %s\n", Last(out))
	current := filepath.Join(r.Share, "current")
	if err := Activate(release, current); err != nil {
		fmt.Fprintln(o.E, err)
		return o.Die("cannot switch " + current), false
	}
	o.OK("current -> " + Tilde(release))
	if !InstallTo(current, r.Source, o.W, o.E) {
		return o.Die("install from the release failed; current points at " + commit), false
	}
	return 0, true
}

// sessions reports the sessions that predate the install.
func sessions(o *Out) {
	fmt.Fprint(o.W, "\nsessions:\n")
	for _, l := range Lines(session.Human(session.Sessions(session.Sources{}))) {
		fmt.Fprintf(o.W, "  %s\n", l)
	}
}

// build archives commit into release through a .building directory beside
// it, renamed into place only when the archive is whole.
func build(r Roots, o *Out, commit, release string) (int, bool) {
	tmp := filepath.Join(r.Share, "releases", fmt.Sprintf(".building-%s-%d", commit, os.Getpid()))
	if err := os.MkdirAll(tmp, 0o777); err != nil {
		fmt.Fprintf(o.E, "mkdir: %v\n", err)
		return o.Die("cannot create " + tmp), false
	}
	if !archive(r.Source, commit, tmp) {
		os.RemoveAll(tmp)
		return o.Die(fmt.Sprintf("git archive of %s failed; nothing activated", commit)), false
	}
	if err := os.Rename(tmp, release); err != nil {
		fmt.Fprintf(o.E, "mv: %v\n", err)
		os.RemoveAll(tmp)
		return o.Die("cannot place the release at " + release), false
	}
	return 0, true
}

// DevVersion is the version of a build of commit that no tag names:
// 0.0.0-dev+<short commit>.
func DevVersion(commit string) string { return "0.0.0-dev+" + short(commit) }

// Binary is a release's own binary, relative to the release.
const Binary = "build/jhon"

// outputGrace is how long goCmd waits, once go has exited, for its output
// to close: a process go left behind holds the pipe, and the wait stops there.
var outputGrace = 5 * time.Second

// goCmd is the go toolchain run in dir with the caller's environment, its
// config directory aside (goEnv).
func goCmd(dir string, args ...string) (string, bool) { return goCmdEnv(dir, nil, args...) }

// goCmdEnv is goCmd with extra KEY=VALUE pairs over the environment.
func goCmdEnv(dir string, extra []string, args ...string) (string, bool) {
	env, done, err := goEnv()
	var refused goRefused
	if errors.As(err, &refused) {
		return err.Error(), false
	}
	if err != nil {
		return "cannot create a config directory for go: " + err.Error(), false
	}
	defer done()
	c := exec.Command("go", args...)
	c.Dir, c.Env = dir, append(env, extra...)
	c.WaitDelay = outputGrace
	out, err := c.CombinedOutput()
	// go exited 0 and only its leftover held the output: go's verdict stands.
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	if err != nil && len(out) == 0 {
		out = []byte(err.Error())
	}
	return Substituted(out), err == nil
}

// MayChange is whether a sandboxed command may change what path names, as
// the sandbox of the folder jhon runs in reads it (the cli sets it to
// sandbox.Input.MayChange); nil, in a test, checks nothing.
var MayChange func(path string) bool

// goRefused is goEnv's refusal: a folder or file go would read that a
// sandboxed command may change. Its text is the whole note, with the fix.
type goRefused string

func (r goRefused) Error() string { return string(r) }

// goEnv is the caller's environment with go's config directory a throwaway
// one, so go's telemetry counters (which GOTELEMETRY=off in the environment
// does not stop: go reads that mode from the config directory) land there,
// not under the user's home; GOENV keeps the user's own go env file. done
// removes the directory. Telemetry is off in it: in a fresh config
// directory go would start a detached telemetry child that creates the
// directory again after done removed it (T113).
//
// go builds with caches no sandboxed command can write (T250 A8): the
// caller's GOCACHE (~/.cache/go-build) and GOMODCACHE (~/go/pkg/mod) are
// writable inside the sandbox, where a command could plant a compiled
// package or change a module's source, and the next sync would link it
// into jhon. So the module cache and the build cache are <state>/go/{mod,
// build}, under Jhon's state folder, which the sandbox keeps read-only and
// refuses to run with where a command could change it, and go's work
// folder one of its own no sandbox binds (goWork); GOWORK=off, so no
// go.work a command planted above the tree joins the build; JHON_GO_CACHE, absolute,
// names another folder for both caches, checked the same way (the test
// suites share one across their scratch homes);
// the toolchain is the local one (GOTOOLCHAIN=local, no other downloaded
// and run), go.mod and go.sum are only read (GOFLAGS=-mod=readonly, the
// caller's flags dropped; -modcacherw leaves the module cache's folders
// removable, as a scratch state folder is removed with its home: the
// sandbox, not the file modes, keeps it), and the modules come first from the caller's
// download cache (GOPROXY=file://<old GOMODCACHE>/cache/download, then the
// caller's proxy), whose zips go checks against go.sum as from any proxy:
// a changed one is refused, and a first build needs no network. A cache
// folder or a go env file a sandboxed command may change (MayChange) is
// refused before go runs (goRefused).
func goEnv() (env []string, done func(), err error) {
	state := goState()
	caches := [][2]string{{"GOMODCACHE", filepath.Join(state, "mod")}, {"GOCACHE", filepath.Join(state, "build")}}
	goenv, set := os.LookupEnv("GOENV")
	if !set {
		if user, err := os.UserConfigDir(); err == nil {
			goenv = filepath.Join(user, "go", "env")
		}
	}
	if MayChange != nil {
		for _, c := range caches {
			if MayChange(c[1]) {
				return nil, nil, goRefused("jhon builds with the Go caches in " + state + ", and a sandboxed command may change " + c[1] + ", where it could plant code the build would take into jhon; fix: keep Jhon's state folder outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write")
			}
		}
		if goenv != "off" && goenv != "" && MayChange(goenv) {
			return nil, nil, goRefused("go reads its settings from " + goenv + ", which a sandboxed command may change, where it could set what the build of jhon takes (GOFLAGS, GOPROXY, GOWORK); fix: keep that file outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write, or point GOENV at one that is")
		}
	}
	work, err := goWork(state)
	if err != nil {
		return nil, nil, err
	}
	proxy := "file://" + filepath.Join(callerModCache(), "cache", "download") + "," + callerProxy(goenv)
	dir, err := os.MkdirTemp("", "jhon-go-config-")
	if err != nil {
		return nil, nil, err
	}
	telemetry := filepath.Join(dir, "go", "telemetry")
	if err = os.MkdirAll(telemetry, 0o700); err == nil {
		err = os.WriteFile(filepath.Join(telemetry, "mode"), []byte("off"), 0o600)
	}
	if err != nil {
		os.RemoveAll(dir)
		return nil, nil, err
	}
	env = os.Environ()
	if !set && goenv != "" {
		env = append(env, "GOENV="+goenv)
	}
	env = append(env, "XDG_CONFIG_HOME="+dir)
	for _, c := range caches {
		env = append(env, c[0]+"="+c[1])
	}
	env = append(env, "GOTMPDIR="+work, "TMPDIR="+work, "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly -modcacherw", "GOPROXY="+proxy)
	return env, func() { os.RemoveAll(dir) }, nil
}

// goState is the folder of the Go caches jhon builds with: <state>/go under
// Jhon's state folder, or JHON_GO_CACHE when absolute.
func goState() string {
	if v := os.Getenv("JHON_GO_CACHE"); filepath.IsAbs(v) {
		return filepath.Clean(v)
	}
	return filepath.Join(filepath.Dir(grant.Dir()), "go")
}

// goWork is go's work folder (GOTMPDIR), one per cache folder state and
// user, kept across runs (T250 round 3: a fresh one per run is a new
// TMPDIR for the tests, which go test's cache keys on, so every warm sync
// ran the whole engine suite again): /var/tmp/jhon-go-<uid>-<hash of
// state>, mode 0700, owned by this user, no link. It is where go compiles
// each package before the build cache takes it, and where sync tests the
// commit's tree (goTestTree), so a sandboxed command that changed a file
// there would put its code in the cache or the tests. It is under /var/tmp,
// which no sandbox binds unless $TMPDIR or sandbox.allow_write names it,
// not /tmp, which every sandbox does; and not under the home, as the tests
// a sync runs make their t.TempDir in GOTMPDIR, and some judge a path by
// whether it lies in the home. It is their TMPDIR too, so t.TempDir and
// os.MkdirTemp share one folder, as the tests expect. One a sandboxed
// command may change (MayChange), or that another user made, is a link or
// is open to others, is refused.
func goWork(state string) (string, error) {
	work := goWorkPath(state)
	base := filepath.Dir(work)
	if err := os.Mkdir(work, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	fi, err := os.Lstat(work)
	if err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm()&0o077 != 0 || !ok || int(st.Uid) != os.Getuid() {
		return "", goRefused("jhon builds in " + work + ", go's work folder, which is not a folder of this user's open to no one else (a link, another user's, or with others' permissions), so another program could change what go compiles there; fix: remove it, and jhon makes it again")
	}
	if MayChange != nil && MayChange(work) {
		return "", goRefused("jhon builds in a work folder of go's under " + base + ", which a sandboxed command may write, where it could change a package go compiled before the build cache takes it; fix: run jhon sync with $TMPDIR outside " + base + ", or take " + base + " out of sandbox.allow_write")
	}
	return work, nil
}

// goWorkPath is where goWork keeps go's work folder for state: under
// /var/tmp, or with JHON_GO_CACHE, which names state, in it (the test
// suites' folder, removed with their run).
func goWorkPath(state string) string {
	if v := os.Getenv("JHON_GO_CACHE"); filepath.IsAbs(v) && filepath.Clean(v) == state {
		return filepath.Join(state, "work")
	}
	base := "/var/tmp"
	if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
		base = os.TempDir()
	}
	sum := sha256.Sum256([]byte(state))
	return filepath.Join(base, fmt.Sprintf("jhon-go-%d-%x", os.Getuid(), sum[:6]))
}

// callerModCache is the module cache go uses for the caller with no
// GOMODCACHE of Jhon's: $GOMODCACHE, else GOPATH's first folder's pkg/mod,
// else ~/go/pkg/mod.
func callerModCache() string {
	if m := os.Getenv("GOMODCACHE"); filepath.IsAbs(m) {
		return m
	}
	if p := filepath.SplitList(os.Getenv("GOPATH")); len(p) > 0 && filepath.IsAbs(p[0]) {
		return filepath.Join(p[0], "pkg", "mod")
	}
	return filepath.Join(project.Home(), "go", "pkg", "mod")
}

// callerProxy is the proxy go uses for the caller: $GOPROXY, else the go
// env file's GOPROXY line, else go's default.
func callerProxy(goenv string) string {
	if p := os.Getenv("GOPROXY"); p != "" {
		return p
	}
	if goenv != "off" && goenv != "" {
		if data, err := os.ReadFile(goenv); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "GOPROXY="); ok && v != "" {
					return v
				}
			}
		}
	}
	return "https://proxy.golang.org,direct"
}

// buildBinary builds the release's binary from its own source, through a
// file beside it renamed into place, so a release never holds half a binary.
// -trimpath keeps the build independent of where the release lives. It is
// stamped ver (sync's is DevVersion), with the commit and the commit's date
// (internal/version), so two builds of one commit are the same binary.
func buildBinary(release, source, commit, ver string) (string, bool) {
	dst := filepath.Join(release, Binary)
	tmp := fmt.Sprintf("%s.%d.tmp", dst, os.Getpid())
	date, _ := git(source, true, "log", "-1", "--format=%cI", commit)
	// -s -w strip the symbol table and DWARF; the pclntab stays, so a
	// panic still names each function, file and line.
	flags := "-s -w " + version.LDFlags(ver, commit, date)
	// CGO_ENABLED=0: a static binary with no glibc floor (T7b; WSL's
	// Ubuntu 22.04), for a packed release and a synced one alike.
	if out, ok := goCmdEnv(release, []string{"CGO_ENABLED=0"}, buildArgs(tmp, flags)...); !ok {
		os.Remove(tmp)
		return out, false
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err.Error(), false
	}
	return "", true
}

// buildArgs is the reproducible `go build` of the binary into out (T52):
// -trimpath drops the build directory from file paths, -buildvcs=false
// leaves out the VCS state (ldflags stamp the version, commit and commit
// date instead), and nothing reads the clock.
func buildArgs(out, ldflags string) []string {
	return []string{"build", "-trimpath", "-buildvcs=false", "-ldflags", ldflags, "-o", out, "./engine/cmd/jhon"}
}

// SourceChecks are what sync and pack judge in the clean source checkout at
// the release's commit, since the release ships without the development
// half they read (T10f): the engine's tests (goTest) and the repository
// validation (README references, audit fixtures, matrix evidence) by the
// release's own renderer.
func SourceChecks(release, source, commit string) (string, bool) {
	if out, ok := goTest(source, commit); !ok {
		return out, false
	}
	// An empty HOME: this judges the repository; the projected runtime is
	// the release preflight's to judge, as the install will leave it.
	home, err := os.MkdirTemp("", "jhon-source-check-")
	if err != nil {
		return "cannot create a scratch home: " + err.Error(), false
	}
	defer os.RemoveAll(home)
	c := exec.Command(filepath.Join(release, Binary), "render-engine", "--validate")
	c.Env = append(os.Environ(), "JHON_ROOT="+source, "HOME="+home)
	out, err := c.CombinedOutput()
	return Substituted(out), err == nil
}

// goTest is the engine tests of the checkout at dir: `go test ./engine/...`,
// the unit tests (the conformance suite under test/ is `jhon test`'s, and
// runs syncs of its own). They read test/testdata, which a release does not
// ship, so they run in the clean source at the release's commit.
// JHON_SYNC_GO_TEST=0 skips them, for the conformance suite, which runs them
// on their own.
func goTest(source, commit string) (string, bool) {
	if os.Getenv("JHON_SYNC_GO_TEST") == "0" {
		return "", true
	}
	// One sync or pack at a time checks its tree out and tests it: the tree
	// is one per work folder (goTestTree), which a second one would rewrite
	// under the first one's tests.
	work, err := goWork(goState())
	if err != nil {
		return err.Error(), false
	}
	lock, err := os.OpenFile(filepath.Join(work, "tree.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return "cannot lock the tree the tests run in: " + err.Error(), false
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "cannot lock the tree the tests run in: " + err.Error(), false
	}
	tree, out, ok := goTestTree(source, commit)
	if !ok {
		return out, false
	}
	// Bounded: a test that hangs fails the preflight with its stack instead
	// of holding the sync (and `jhon test`) for go's default ten minutes.
	return goCmd(tree, "test", "-trimpath", "-timeout", "300s", "./engine/...")
}

// goTestTree is the tree of commit in source, checked out by git from the
// commit itself into go's work folder (goWork), where the engine's tests
// run (T250 round 3, the verify's A1): not the live checkout, where a
// sandboxed command can put a go.work, a _test.go hidden by
// .git/info/exclude, or a changed file git is told not to look at, all of
// which go test would compile and run outside the sandbox. git updates
// only the files that differ from the last sync's (read-tree --reset -u
// with an index of the tree's own), so go test's cache still knows the
// rest. It runs no filter or hook the repository names (project.ReadGitCmd).
func goTestTree(source, commit string) (tree, out string, ok bool) {
	work, err := goWork(goState())
	if err != nil {
		return "", err.Error(), false
	}
	tree = filepath.Join(work, "tree")
	if err := os.MkdirAll(tree, 0o700); err != nil {
		return "", err.Error(), false
	}
	c := project.ReadGitCmd(source, "read-tree", "--reset", "-u", commit)
	c.Env = append(c.Env, "GIT_INDEX_FILE="+filepath.Join(work, "tree.index"), "GIT_WORK_TREE="+tree)
	if b, err := c.CombinedOutput(); err != nil {
		return "", "cannot check " + commit + " out for its tests: " + strings.TrimSpace(string(b)), false
	}
	return tree, "", true
}

// archive is `git archive --format=tar <commit> | tar -x -C <dir>` under
// pipefail: whether both sides succeeded.
func archive(source, commit, dir string) bool {
	// No filter the source's configuration names runs (git archive smudges
	// what it exports), as for every read of jhon's (project.ReadGitCmd).
	g := project.ReadGitCmd(source, "archive", "--format=tar", commit)
	t := exec.Command("tar", "-x", "-C", dir)
	g.Stderr, t.Stderr, t.Stdout = os.Stderr, os.Stderr, os.Stdout
	pipe, err := g.StdoutPipe()
	if err != nil {
		return false
	}
	t.Stdin = pipe
	if err := g.Start(); err != nil {
		return false
	}
	terr := t.Run()
	gerr := g.Wait()
	return gerr == nil && terr == nil
}

// Activate points link at target atomically: a new link beside it renamed
// over it, so a reader sees the old release or the new one, never neither.
func Activate(target, link string) error {
	tmp := fmt.Sprintf("%s.%d.tmp", link, os.Getpid())
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, link)
}

// Publish is `jhon publish` after its argument check: push the current
// branch to origin and mirror, refused unless the tree is clean and HEAD is
// the commit current points at, so a remote never receives a commit this
// machine has not validated and activated.
func Publish(r Roots, o *Out) int {
	if code, ok := r.NeedRepo(o, "publish"); !ok {
		return code
	}
	if dirty(r.Source, false) {
		return o.Die("working tree is dirty; commit and sync first (publish pushes the active release)")
	}
	commit, ok := git(r.Source, false, "rev-parse", "--verify", "HEAD")
	if !ok {
		return o.Die("cannot resolve HEAD in " + r.Source)
	}
	active := ""
	if p, ok := ReadlinkF(filepath.Join(r.Share, "current")); ok {
		active = filepath.Base(p)
	}
	if active != commit {
		return o.Die(fmt.Sprintf("HEAD %s is not the active release (%s); run 'jhon sync' first", short(commit), short(active)))
	}
	branch, _ := git(r.Source, false, "branch", "--show-current")
	if branch == "" {
		return o.Die("HEAD is detached; publish pushes a branch")
	}
	for _, remote := range []string{"origin", "mirror"} {
		if _, ok := git(r.Source, true, "remote", "get-url", remote); !ok {
			o.Warn(remote + " not configured, skipped")
			continue
		}
		// Outside the sandbox: no program the repository names runs, and a
		// hook only when it is committed code (pushCmd, T250).
		push, refused := pushCmd(r.Source, remote, branch)
		if refused != "" {
			return o.Die(refused)
		}
		fmt.Fprintf(o.W, "pushing %s to %s...\n", branch, remote)
		if Run(push) != 0 {
			return o.Die("push to " + remote + " failed")
		}
	}
	return 0
}

// short is ${s:0:7}.
func short(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// ReadlinkF is `readlink -f`: p with every link resolved; every component
// but the last must exist.
func ReadlinkF(p string) (string, bool) {
	p, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	for range 40 {
		dir, err := filepath.EvalSymlinks(filepath.Dir(p))
		if err != nil {
			return "", false
		}
		p = filepath.Join(dir, filepath.Base(p))
		fi, err := os.Lstat(p)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			return p, true
		}
		target, err := os.Readlink(p)
		if err != nil {
			return "", false
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		p = target
	}
	return "", false
}

// RealpathM is `realpath -m`: p with every existing link resolved and the
// rest taken lexically, so a path need not exist.
func RealpathM(p string) string {
	p, _ = filepath.Abs(p)
	resolved := "/"
	rest := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for hops := 0; len(rest) > 0; {
		c := rest[0]
		rest = rest[1:]
		switch c {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, c)
		fi, err := os.Lstat(next)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 || hops > 40 {
			resolved = next
			continue
		}
		target, err := os.Readlink(next)
		if err != nil {
			resolved = next
			continue
		}
		hops++
		if filepath.IsAbs(target) {
			resolved = "/"
		}
		rest = append(strings.Split(target, "/"), rest...)
	}
	return resolved
}
