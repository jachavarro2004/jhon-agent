// Package release is the committed release (docket AV1) and its
// publication: `jhon sync` and `jhon publish`, and the roots both resolve.
//
// `jhon sync` archives the source's HEAD into
// ~/.local/share/jhon/releases/<commit>/, preflights it, switches
// ~/.local/share/jhon/current to it atomically, and installs the runtime
// projections from it; `jhon publish` pushes the active release's branch to
// origin and mirror. Rendering is internal/render's, through render.go.
package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/session"
)

// Roots are the CLI's two roots (docket AV1) and the share directory.
//
// Runtime is where the binary runs from: the active release
// (~/.local/share/jhon/current) when it is the installed CLI, or a checkout
// run directly; the runtime commands always run its code. Source is the
// maintenance repository doctor, test, and sync work on: Runtime when it is
// a checkout (it holds .git), else JHON_SOURCE, else the checkout `jhon sync`
// last installed from (the install record's source, while it exists), else
// the legacy defaults: ~/dev/jhon (L207: ~/.agents is read by several tools,
// which took the checkout's agents and node_modules for theirs), or
// ~/.agents/jhon when only that one exists.
type Roots struct {
	Runtime, Source, Share string
}

// Locate resolves the roots of this process. The binary is <root>/build/jhon,
// so the runtime is the parent of the directory holding the program,
// reached through the path it was started by (Invoked) with each link it is
// followed in turn (Here). A checkout run
// directly is its own runtime and source; the CLI linked from ~/.local/bin
// into a release runs from ~/.local/share/jhon/current and works on
// JHON_SOURCE or the checkout sync recorded. JHON_ROOT, when set, is the runtime (the
// override the conformance suite runs a checkout with).
func Locate() Roots {
	runtime := os.Getenv("JHON_ROOT")
	if runtime == "" {
		runtime = Here(Invoked())
	}
	return roots(runtime, os.Getenv("JHON_SOURCE"), os.Getenv("HOME"))
}

func roots(runtime, jhonSource, home string) Roots {
	r := Roots{Runtime: runtime, Share: home + "/.local/share/jhon"}
	switch {
	case Exists(filepath.Join(runtime, ".git")):
		r.Source = runtime
	case jhonSource != "":
		r.Source = jhonSource
	case recordedSource(home) != "":
		r.Source = recordedSource(home)
	case !Exists(home+"/dev/jhon") && Exists(home+"/.agents/jhon"):
		// A checkout from before L207 still found (T56c).
		r.Source = home + "/.agents/jhon"
	default:
		r.Source = home + "/dev/jhon"
	}
	return r
}

// recordedSource is the checkout the install record under home names, the
// one `jhon sync` last installed from, when it still exists; "" otherwise.
func recordedSource(home string) string {
	if rec, ok := session.ReadInstall(home); ok && rec.Source != "" && Exists(rec.Source) {
		return rec.Source
	}
	return ""
}

// Invoked is the path this process was started by, a shell's $0: argv[0] when
// it names a path, else the PATH entry it was found in, else the
// executable the kernel reports.
func Invoked() string {
	a := os.Args[0]
	if strings.Contains(a, "/") {
		return a
	}
	if p, err := exec.LookPath(a); err == nil && filepath.IsAbs(p) {
		return p
	}
	exe, _ := os.Executable()
	return exe
}

// Here follows the links that self is, one at a
// time (a relative target against the link's directory), then take the
// parent of the directory holding the result, logically (`cd …/.. && pwd`).
// A link in a directory component is kept, so the CLI linked into
// ~/.local/bin resolves to ~/.local/share/jhon/current, not to the release
// current names: a session started through it follows `current` when it
// resumes.
func Here(self string) string {
	for range 40 {
		fi, err := os.Lstat(self)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			break
		}
		target, err := os.Readlink(self)
		if err != nil {
			break
		}
		if filepath.IsAbs(target) {
			self = target
		} else {
			self = filepath.Join(filepath.Dir(self), target)
		}
	}
	here, err := filepath.Abs(filepath.Join(filepath.Dir(self), ".."))
	if err != nil {
		return filepath.Dir(filepath.Dir(self))
	}
	return here
}

// Tilde is p with a leading $HOME written as ~. The
// match is a plain prefix, with no boundary: an empty HOME prefixes every
// path with ~.
func Tilde(p string) string {
	home := os.Getenv("HOME")
	if strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// Exists is the shell's -e: the path, followed through links, exists.
func Exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// IsFile is the shell's -f: a regular file, through links.
func IsFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// IsDir is the shell's -d: a directory, through links.
func IsDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
