package release

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// publishRemote sends commit as branch to remote of source, as `jhon
// publish` does (T250, the re-verifies of 5e9b12e and 18deae4). publish
// runs outside the sandbox, and a sandboxed command can write the source's
// .git/config, which chooses programs git runs (a transport, a proxy, a
// signer, the far end's own when the remote is local), so the push never
// runs with the source's configuration: it runs from a private repository
// of jhon's own (privateRepo), whose configuration jhon writes and whose
// objects are the source's, read through objects/info/alternates; git
// reads the owner's system and global configuration there, so his
// insteadOf, ssh command and signing apply, and nothing of the source's.
// Its target is remote.<name>.url, refused where the source's own pushurl,
// insteadOf or pushInsteadOf would change it (publishURL), and where, as
// the owner's configuration rewrites it, it is not an ssh or https remote
// (pushTarget). A hook runs only when it is committed code (committedHooks):
// jhon runs the committed pre-push itself, in the source's work tree, with
// git's stdin protocol and jhon's neutral git environment, before the push;
// one that fails sends nothing. After the push the source's
// refs/remotes/<remote>/<branch> follows, as after git push. target is
// where it went; refused is the note for a push refused before anything
// ran, failed for one that ran and failed.
func publishRemote(source, remote, branch, commit string, stdout, stderr *os.File) (target, refused, failed string) {
	const tail = "; nothing was pushed; fix: "
	hooks, refused := committedHooks(source)
	if refused != "" {
		return "", refused, ""
	}
	url, refused := publishURL(source, remote)
	if refused != "" {
		return "", refused, ""
	}
	priv, done, err := privateRepo(source, remote, url, branch, commit)
	if err != nil {
		return "", "cannot set up the push of " + remote + ": " + err.Error() + tail + "run jhon publish again", ""
	}
	defer done()
	run := func(args ...string) (string, bool) {
		c := exec.Command("git", append([]string{"-C", priv}, args...)...)
		c.Env = privateEnv()
		out, err := c.Output()
		return strings.TrimSpace(string(out)), err == nil
	}
	target, _ = run("remote", "get-url", "--push", remote)
	if why := pushTarget(target); why != "" {
		return target, remote + " is " + target + ", " + why + tail + "set remote." + remote + ".url to the ssh or https address of the remote (a rewrite of your own, url.<base>.insteadOf, belongs in your global configuration)", ""
	}
	top, _ := project.ReadGit(source, "rev-parse", "--show-toplevel")
	if pre := filepath.Join(hooks, "pre-push"); hooks != "/dev/null" && executable(pre) {
		remoteSha := strings.Repeat("0", 40)
		out, ok := run("ls-remote", remote, "refs/heads/"+branch)
		if !ok {
			return target, "", "cannot reach " + remote + " (" + target + ") for the pre-push hook's remote ref; nothing was pushed"
		}
		if f := strings.Fields(out); len(f) >= 1 {
			remoteSha = f[0]
		}
		h := exec.Command(pre, remote, target)
		h.Dir, h.Env = top, project.NeutralGitEnv(source)
		h.Stdin = strings.NewReader("refs/heads/" + branch + " " + commit + " refs/heads/" + branch + " " + remoteSha + "\n")
		h.Stdout, h.Stderr = stdout, stderr
		if h.Run() != nil {
			return target, "", "the committed pre-push hook refused the push to " + remote + "; nothing was pushed"
		}
	}
	send := exec.Command("git", "-C", priv, "push", remote, "refs/heads/"+branch+":refs/heads/"+branch)
	send.Env = privateEnv()
	send.Stdin, send.Stdout, send.Stderr = os.Stdin, stdout, stderr
	if send.Run() != nil {
		return target, "", "push to " + remote + " failed"
	}
	if fetch, _ := project.ReadGit(source, "config", "--get-all", "remote."+remote+".fetch"); strings.Contains(fetch, "refs/remotes/"+remote+"/*") {
		_ = project.ReadGitCmd(source, "update-ref", "refs/remotes/"+remote+"/"+branch, commit).Run()
	}
	return target, "", ""
}

// executable is whether p is a regular file someone may run, as git runs a
// hook.
func executable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0
}

// privateEnv is the environment of the private repository's git: the
// caller's (GIT_SSH_COMMAND, GIT_ASKPASS, GIT_CONFIG_GLOBAL and the like are
// the owner's), but no variable that names another repository, index or
// object folder, no configuration given on a command line or in the
// environment's entries, and no replace refs.
func privateEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "GIT_DIR", k == "GIT_WORK_TREE", k == "GIT_INDEX_FILE", k == "GIT_OBJECT_DIRECTORY", k == "GIT_ALTERNATE_OBJECT_DIRECTORIES", k == "GIT_COMMON_DIR", k == "GIT_NAMESPACE", k == "GIT_CONFIG", k == "GIT_CONFIG_PARAMETERS", k == "GIT_CONFIG_COUNT", strings.HasPrefix(k, "GIT_CONFIG_KEY_"), strings.HasPrefix(k, "GIT_CONFIG_VALUE_"):
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GIT_NO_REPLACE_OBJECTS=1")
}

// privateRepo is a bare repository of jhon's own, made fresh in go's work
// folder (goWork: one no sandbox binds, refused where a sandboxed command
// could change it), with no template, no hook (core.hooksPath=/dev/null),
// the source's objects through objects/info/alternates (git reads objects
// there and runs nothing), refs/heads/<branch> at commit, and remote.<name>
// at url; done deletes it.
func privateRepo(source, remote, url, branch, commit string) (dir string, done func(), err error) {
	work, err := goWork(goState())
	if err != nil {
		return "", nil, err
	}
	common, ok := project.ReadGit(source, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if !ok {
		return "", nil, errors.New("git cannot name the source's git folder")
	}
	dir, err = os.MkdirTemp(work, "publish-")
	if err != nil {
		return "", nil, err
	}
	done = func() { os.RemoveAll(dir) }
	git := func(args ...string) error {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = privateEnv()
		if out, err := c.CombinedOutput(); err != nil {
			return errors.New("git " + args[0] + ": " + strings.TrimSpace(string(out)))
		}
		return nil
	}
	steps := []func() error{
		func() error { return git("init", "-q", "--bare", "--template=") },
		func() error {
			if err := os.MkdirAll(filepath.Join(dir, "objects", "info"), 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "objects", "info", "alternates"), []byte(filepath.Join(common, "objects")+"\n"), 0o600)
		},
		func() error { return git("config", "core.hooksPath", "/dev/null") },
		func() error { return git("config", "remote."+remote+".url", url) },
		func() error { return git("update-ref", "refs/heads/"+branch, commit) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			done()
			return "", nil, err
		}
	}
	return dir, done, nil
}

// publishURL is the one remote.<remote>.url the source names, refused where
// the source's own configuration (any scope but the system's and the
// global one, which are the owner's) would change where the push goes: a
// remote.<remote>.pushurl, or a url.<base>.insteadOf or pushInsteadOf
// whose value begins the url. Each refusal names the key, its origin and
// the fix.
func publishURL(source, remote string) (url, refused string) {
	const tail = "; nothing was pushed; fix: "
	out, _ := project.ReadGitCmd(source, "config", "--show-scope", "--show-origin", "--get-regexp", `^remote\.`+regexp.QuoteMeta(remote)+`\.(url|pushurl)$|^url\..*\.(insteadof|pushinsteadof)$`).Output()
	type entry struct{ scope, origin, key, value string }
	var entries []entry
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) != 3 {
			continue
		}
		k, v, _ := strings.Cut(f[2], " ")
		entries = append(entries, entry{f[0], f[1], k, v})
	}
	var urls []string
	for _, e := range entries {
		if strings.EqualFold(e.key, "remote."+remote+".url") {
			urls = append(urls, e.value)
		}
	}
	if len(urls) != 1 {
		return "", remote + " names " + strconv.Itoa(len(urls)) + " urls (remote." + remote + ".url), not one" + tail + "leave one url for " + remote
	}
	url = urls[0]
	for _, e := range entries {
		if e.scope == "system" || e.scope == "global" {
			continue
		}
		lower := strings.ToLower(e.key)
		rewrites := (strings.HasSuffix(lower, ".insteadof") || strings.HasSuffix(lower, ".pushinsteadof")) && strings.HasPrefix(url, e.value)
		if strings.EqualFold(e.key, "remote."+remote+".pushurl") || rewrites {
			return "", e.key + " = " + e.value + " (" + e.origin + ") would change where " + remote + " is pushed, and the repository's configuration may be a sandboxed command's" + tail + "remove it (git config --unset " + e.key + "); a rewrite of your own belongs in your global configuration"
		}
	}
	return url, ""
}

// pushTarget is why publish does not push to url, the address git would
// push to as the owner's configuration rewrites it, "" for an ssh
// (ssh://, or [user@]host:path) or https/http remote: a path or file://
// is a repository on this machine, whose own configuration git runs
// (core.alternateRefsCommand, its hooks) and where no remote is reached; a
// git:// remote goes through core.gitProxy; anything else names a transport
// helper, a program git runs.
func pushTarget(url string) string {
	local := "a repository on this machine, where git runs that repository's programs (core.alternateRefsCommand, its hooks) and reaches no remote"
	switch {
	case url == "":
		return "an address git cannot read"
	case strings.Contains(url, "::"):
		return "a transport helper's (a program git runs)"
	}
	if scheme, _, ok := strings.Cut(url, "://"); ok && !strings.ContainsAny(scheme, "/:@") {
		switch strings.ToLower(scheme) {
		case "ssh", "git+ssh", "ssh+git", "https", "http":
			return ""
		case "file":
			return local
		case "git":
			return "a git:// remote, which core.gitProxy reaches through a program"
		}
		return "a transport helper's (a program git runs)"
	}
	// scp-like: [user@]host:path, with no slash before the colon.
	if i := strings.Index(url, ":"); i > 0 && !strings.Contains(url[:i], "/") {
		return ""
	}
	return local
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
