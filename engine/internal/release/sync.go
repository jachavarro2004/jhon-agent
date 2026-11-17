package release

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/session"
	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
)

// git runs git in dir with the caller's environment; its
// stderr reaches this process's unless quiet. It returns the substituted
// stdout and whether git exited 0.
func git(dir string, quiet bool, args ...string) (string, bool) {
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = project.GitReadEnv()
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
		if out, ok := SourceChecks(release, r.Source); !ok {
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

// goEnv is the caller's environment with go's config directory a throwaway
// one, so go's telemetry counters (which GOTELEMETRY=off in the environment
// does not stop: go reads that mode from the config directory) land there,
// not under the user's home; GOENV keeps the user's own go env file. done
// removes the directory.
func goEnv() (env []string, done func(), err error) {
	dir, err := os.MkdirTemp("", "jhon-go-config-")
	if err != nil {
		return nil, nil, err
	}
	env = os.Environ()
	if _, ok := os.LookupEnv("GOENV"); !ok {
		if user, err := os.UserConfigDir(); err == nil {
			env = append(env, "GOENV="+filepath.Join(user, "go", "env"))
		}
	}
	env = append(env, "XDG_CONFIG_HOME="+dir)
	return env, func() { os.RemoveAll(dir) }, nil
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
func SourceChecks(release, source string) (string, bool) {
	if out, ok := goTest(source); !ok {
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
func goTest(dir string) (string, bool) {
	if os.Getenv("JHON_SYNC_GO_TEST") == "0" {
		return "", true
	}
	// Bounded: a test that hangs fails the preflight with its stack instead
	// of holding the sync (and `jhon test`) for go's default ten minutes.
	return goCmd(dir, "test", "-trimpath", "-timeout", "300s", "./engine/...")
}

// archive is `git archive --format=tar <commit> | tar -x -C <dir>` under
// pipefail: whether both sides succeeded.
func archive(source, commit, dir string) bool {
	g := exec.Command("git", "-C", source, "archive", "--format=tar", commit)
	g.Env = project.GitReadEnv()
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
		fmt.Fprintf(o.W, "pushing %s to %s...\n", branch, remote)
		if Run(exec.Command("git", "-C", r.Source, "push", remote, branch)) != 0 {
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
