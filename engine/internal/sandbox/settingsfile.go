package sandbox

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SettingsFile is why the sandbox of in runs nothing while the settings
// file JHON_SETTINGS names, file, lies where a sandboxed command may change
// it, and the fix; nil when file is "" or it lies outside every such folder
// (sandbox-deploy-tools S9). A command that could create, rewrite, rename or
// relink that file could write `[sandbox] mode = "off"` and turn the sandbox
// off for every later command. A read-only bind does not keep it: a missing
// file is not bound at all, a bound one moves away with its folder when the
// command renames the folder, and a link is replaced, not bound. The
// default file needs none of this: its folder is read-only and lies outside
// the writable areas.
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
	if !filepath.IsAbs(file) || file == "/proc" || strings.HasPrefix(filepath.Clean(file), "/proc/") {
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

// maxLinks is how many links mayChange follows on one path, as the kernel
// does before it gives up (ELOOP).
const maxLinks = 40

// mayChange is whether a sandboxed command of in may change what path names:
// the path is walked from / as the kernel walks it, each link followed, and
// any entry on the way (a folder, a link or the file) that sits in a folder
// a sandboxed command may write (writable) can be created, replaced, renamed
// or removed by one. An entry that does not exist in a folder no command may
// write cannot appear there. Anything that cannot be read is taken as
// changeable.
func mayChange(in Input, path string) bool {
	open := writable(in)
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
		if within(cur, open) {
			return true
		}
		next := filepath.Join(cur, part)
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
	return within(cur, open)
}
