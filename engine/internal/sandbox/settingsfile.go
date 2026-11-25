package sandbox

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// SettingsFile is why the sandbox of in runs nothing while the settings
// file JHON_SETTINGS names, file, lies where a sandboxed command may change
// it, and the fix; nil when file is "" or it lies outside every such folder
// (sandbox-deploy-tools S9). A command that could create, rewrite, rename or
// relink that file could write `[sandbox] mode = "off"` and turn the sandbox
// off for every later command. A read-only bind does not keep it: a missing
// file is not bound at all, a bound one moves away with its folder when the
// command renames the folder, and a link is replaced, not bound. The
// default file is ConfigSettings'.
//
// It is not in Check: Check is the probe that decides the default mode, and
// a settings file the sandbox could write must stop the run, never turn the
// default mode off.
func SettingsFile(in Input, file string) *Problem {
	if file == "" {
		return nil
	}
	// A relative path, or one through /proc, names another file in each
	// folder or process a later jhon call starts in.
	if !filepath.IsAbs(file) || underProc(file) {
		return &Problem{
			Reason: "the settings file JHON_SETTINGS names (" + file + ") is not an absolute path outside /proc, so a later jhon call started in another folder would read another file, one a sandboxed command could write",
			Fix:    "set JHON_SETTINGS to an absolute path outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write, or unset it to use ~/.config/jhon/settings.toml",
		}
	}
	if !mayChange(in, file) {
		return nil
	}
	return &Problem{
		Reason: "the settings file JHON_SETTINGS names (" + file + ") is in a folder a sandboxed command may write, where a command could rewrite it and turn the sandbox off",
		Fix:    "keep the file outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write, or unset JHON_SETTINGS to use ~/.config/jhon/settings.toml",
	}
}

// ConfigSettings is SettingsFile for the settings file with JHON_SETTINGS
// unset: file is $XDG_CONFIG_HOME/jhon/settings.toml, with variable
// "XDG_CONFIG_HOME", or ~/.config/jhon/settings.toml, with variable ""
// (sandbox-deploy-tools S10). Its folder is bound read-only, which keeps
// the file only where no command can move the folder: outside every
// writable folder, or bound directly in one, where the kernel refuses to
// rename, replace or remove the mount point (mayChange).
func ConfigSettings(in Input, file, variable string) *Problem {
	return ownPath(in, file, variable, ownPathText{
		what:   "the settings file",
		harm:   "create or replace it and turn the sandbox off",
		folder: "~/.config/jhon",
		unset:  "~/.config/jhon/settings.toml",
	})
}

// StateFolder is ConfigSettings for Jhon's state folder, which holds the
// grants and the modes: dir is $XDG_STATE_HOME/jhon, with variable
// "XDG_STATE_HOME", or ~/.local/state/jhon, with variable "" (S10). A
// command that could create or replace it could grant itself what the
// guards ask for.
func StateFolder(in Input, dir, variable string) *Problem {
	return ownPath(in, dir, variable, ownPathText{
		what:   "Jhon's state folder",
		holds:  ", which holds the grants and the modes,",
		harm:   "create or replace it and grant itself what the guards ask for",
		folder: "~/.local/state/jhon",
		unset:  "~/.local/state/jhon",
	})
}

// ownPathText is how ownPath names one of Jhon's own paths.
type ownPathText struct {
	what, holds, harm string
	// folder is the default folder, unset what the variable's absence
	// gives.
	folder, unset string
}

// ownPath is the problem of Jhon's own path, named by variable or by its
// default under the home ("").
func ownPath(in Input, path, variable string, t ownPathText) *Problem {
	if path == "" {
		return nil
	}
	named := t.what + " (" + path + ")" + t.holds
	fix := "keep " + t.folder + " outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write, or as a folder (not a link) directly inside a sandbox.allow_write folder"
	if variable != "" {
		named = t.what + " under " + variable + " (" + path + ")" + t.holds
		fix = "set " + variable + " to a folder outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write, or unset it to use " + t.unset
	}
	// A path through /proc names another one in each process a later jhon
	// call starts as.
	if !filepath.IsAbs(path) || underProc(path) {
		return &Problem{Reason: named + " is not an absolute path outside /proc, so a later jhon call started in another folder would read another one, one a sandboxed command could write", Fix: fix}
	}
	if !mayChange(in, path) {
		return nil
	}
	return &Problem{Reason: named + " is in a folder a sandboxed command may write, where a command could " + t.harm, Fix: fix}
}

// underProc is whether path is /proc or lies under it.
func underProc(path string) bool {
	return path == "/proc" || strings.HasPrefix(filepath.Clean(path), "/proc/")
}

// maxLinks is how many links mayChange follows on one path, as the kernel
// does before it gives up (ELOOP).
const maxLinks = 40

// mayChange is whether a sandboxed command of in may change what path names:
// the path is walked from / as the kernel walks it, each link followed, and
// any entry on the way (a folder, a link or the file) that sits in a folder
// a sandboxed command may write can be created, replaced, renamed or
// removed by one, unless the sandbox mounts a folder there that is not
// writable: the kernel refuses to rename, replace or remove a mount point
// from inside its mount namespace (EBUSY). A read-only file bind is not
// taken as such: the S9 file stays refused. An entry that does not exist in
// a folder no command may write cannot appear there. Anything that cannot
// be read is taken as changeable.
//
// The mounts are the ones under any PATH (layout without PATH's folders):
// a folder a PATH folder keeps read-only counts as writable here, as in
// writable.
func mayChange(in Input, path string) bool {
	mounts, _ := layout(in, false)
	return changeable(mounts, path)
}

// changeable is mayChange with the mounts under any PATH given.
func changeable(mounts []mount, path string) bool {
	// open is whether a command may create, rename or remove an entry in
	// folder dir: the innermost area that holds it is writable (/dev is).
	open := func(dir string) bool {
		if within(dir, []string{"/dev"}) {
			return true
		}
		kind := read
		// Parents first: the last area that holds dir is the innermost.
		for _, m := range mounts {
			if (m.dir || m.path == dir) && project.Within(dir, m.path) {
				kind = m.kind
			}
		}
		return kind == write
	}
	// pinned is whether p is a folder the sandbox mounts in a writable one,
	// so a mount point the kernel keeps where it is.
	pinned := func(p string) bool {
		return slices.ContainsFunc(mounts, func(m mount) bool { return m.dir && m.kind != write && m.path == p })
	}
	// Not filepath.Abs: it cleans `link/..` away, where the kernel goes up
	// from the link's target.
	if !filepath.IsAbs(path) {
		wd, err := os.Getwd()
		if err != nil {
			return true
		}
		path = wd + string(filepath.Separator) + path
	}
	todo := strings.Split(path, string(filepath.Separator))
	cur, links := string(filepath.Separator), 0
	for len(todo) > 0 {
		part := todo[0]
		todo = todo[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, part)
		if open(cur) && !pinned(next) {
			return true
		}
		fi, err := os.Lstat(next)
		if errors.Is(err, fs.ErrNotExist) {
			return false
		}
		if err != nil {
			return true
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			cur = next
			continue
		}
		if links++; links > maxLinks {
			return true
		}
		target, err := os.Readlink(next)
		if err != nil {
			return true
		}
		if filepath.IsAbs(target) {
			cur = string(filepath.Separator)
		}
		todo = append(strings.Split(target, string(filepath.Separator)), todo...)
	}
	return open(cur)
}
