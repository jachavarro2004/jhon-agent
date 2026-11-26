package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// pushCmd is `git -C <source> push <remote> <branch>` as `jhon publish` runs
// it (T250, the re-verify of 5e9b12e): outside the sandbox, so it runs no
// program the repository's configuration names, which a sandboxed command
// may have written (.git/config is writable inside). With project.GitReadEnv
// (the caller's environment, and no fsmonitor, configured hook, pager,
// editor, ssh command but ssh, askpass or ext:: transport), the repository's
// credential helpers dropped, the remote's receive-pack program git's own,
// and no submodule pushed; the owner's own ssh command, askpass and
// credential helpers, set where no sandboxed command writes (the system and
// global configuration), added back. A hook runs only when it is committed
// code (committedHooks): the hooks folder core.hooksPath names in the
// repository, whose hook files the repository tracks, unmodified. refused is
// why publish pushes nothing, with the fix, "" when it may push.
func pushCmd(source, remote, branch string) (c *exec.Cmd, refused string) {
	hooks, refused := committedHooks(source)
	if refused != "" {
		return nil, refused
	}
	entries := [][2]string{{"credential.helper", ""}}
	entries = append(entries, project.OwnerGitConfig(`^(core\.sshcommand|core\.askpass|credential\..*helper)$`)...)
	entries = append(entries,
		[2]string{"core.hooksPath", hooks},
		[2]string{"push.recurseSubmodules", "no"},
		[2]string{"submodule.recurse", "false"},
	)
	// git takes the first remote.<name>.receivepack it reads, the
	// repository's, so git's own program is given on the command line.
	c = exec.Command("git", "-C", source, "push", "--receive-pack=git-receive-pack", remote, branch)
	c.Env = project.WithGitConfig(project.GitReadEnv(), entries)
	return c, ""
}

// committedHooks is the folder a publish push takes its hooks from: what
// core.hooksPath names, when it is a folder of the repository's work tree,
// reached with no link, each of whose hook files (a file named as a hook
// event) the repository's HEAD holds with the same content (git
// hash-object --no-filters, which no index flag and no filter changes); or
// /dev/null when no hook would run (no core.hooksPath and no hook in the git
// folder, or a hooks folder that holds none). Any other hook (a hook in the
// git folder, which no commit holds; a folder outside the repository, or
// one reached through a link; a hook file untracked or changed) refuses
// the push: refused names the key, its origin and the fix. A hook is never
// skipped silently.
func committedHooks(source string) (hooks, refused string) {
	const tail = "; nothing was pushed; fix: "
	read := func(args ...string) (string, bool) {
		out, err := project.ReadGitCmd(source, args...).Output()
		return strings.TrimSuffix(string(out), "\n"), err == nil
	}
	top, ok := read("rev-parse", "--show-toplevel")
	if !ok || top == "" {
		return "", "cannot find the work tree of " + source + tail + "run jhon publish in the source checkout"
	}
	listing, _ := read("config", "--show-origin", "--get-all", "core.hooksPath")
	if listing == "" {
		common, ok := read("rev-parse", "--path-format=absolute", "--git-common-dir")
		if !ok {
			return "", "cannot find the git folder of " + source + tail + "run jhon publish in the source checkout"
		}
		for _, event := range project.GitHookEvents {
			p := filepath.Join(common, "hooks", event)
			if fi, err := os.Lstat(p); err == nil && (fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm()&0o111 != 0) {
				return "", p + " is a hook in the git folder, which no commit holds, so a sandboxed command could have put it there" + tail + "remove it, or commit it in a folder of the repository and point core.hooksPath at that folder"
			}
		}
		return "/dev/null", ""
	}
	lines := strings.Split(listing, "\n")
	origin, value, _ := strings.Cut(lines[len(lines)-1], "\t")
	named := "core.hooksPath = " + value + " (" + origin + ")"
	fix := tail + "unset it (git config --unset core.hooksPath) or point it at a folder of hooks the repository tracks, unmodified"
	dir := value
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(top, dir)
	}
	dir = filepath.Clean(dir)
	var present []string
	for _, event := range project.GitHookEvents {
		if _, err := os.Lstat(filepath.Join(dir, event)); err == nil {
			present = append(present, event)
		}
	}
	if len(present) == 0 {
		return "/dev/null", ""
	}
	outside := func(why string) (string, string) {
		return "", named + " names " + dir + ", " + why + fix
	}
	rel, err := filepath.Rel(top, dir)
	if err != nil || strings.HasPrefix(value, "~") || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.ContainsAny(value, "\n") {
		return outside("which is not a folder of committed hooks in the repository, so its " + present[0] + " is not committed code a sandboxed command could not have written")
	}
	// No link on the way from the work tree's top, the folder included.
	cur := top
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		if fi, err := os.Lstat(cur); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return outside("which is reached through a link or is no folder, so its " + present[0] + " may be what a sandboxed command put elsewhere")
		}
	}
	for _, event := range present {
		p := filepath.Join(dir, event)
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() {
			return outside("whose " + event + " is a link or no regular file, not committed code")
		}
		path := filepath.ToSlash(filepath.Join(rel, event))
		blob, ok := read("rev-parse", "--verify", "--quiet", "HEAD:"+path)
		if !ok {
			return outside("which is not a folder of committed hooks in the repository, so its " + event + " is not committed code a sandboxed command could not have written")
		}
		if sum, ok := read("hash-object", "--no-filters", "--", p); !ok || sum != blob {
			return outside("whose " + event + " differs from the one the repository tracks, so a sandboxed command could have changed it")
		}
	}
	return dir, ""
}
