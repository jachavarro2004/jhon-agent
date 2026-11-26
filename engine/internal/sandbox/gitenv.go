package sandbox

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// gitOverrides are the configuration git runs with on an unsandboxed line
// (T128g, L1306): a command in the work profile may write the project's
// .git/config and .git/hooks, and git reads them outside the sandbox. Given
// through GIT_CONFIG_COUNT, they come after every configuration file, so
// they win over the repository's: no hook, no ssh command but ssh, no
// fsmonitor, no credential helper of the repository's (an empty value
// drops every helper read so far; the owner's are added back after it), no
// ext::, fd:: or local-path transport, no submodule recursion, and no
// automatic gc or maintenance in the background, which would outlive the
// line and its record in the registry (L36). They hold against what the
// repository holds besides its configuration (.git/hooks) and against the
// owner's own configuration; the repository's configuration itself is
// judged by an allowlist (RepositoryGitConfig, L39): a line whose
// repository sets any other key runs in the sandbox.
var gitOverrides = [][2]string{
	{"core.hooksPath", "/dev/null"},
	{"core.sshCommand", "ssh"},
	{"core.fsmonitor", "false"},
	{"credential.helper", ""},
	{"protocol.ext.allow", "never"},
	{"protocol.fd.allow", "never"},
	{"protocol.file.allow", "never"},
	{"submodule.recurse", "false"},
	{"fetch.recurseSubmodules", "false"},
	{"gc.autoDetach", "false"},
	{"maintenance.autoDetach", "false"},
}

// hookEvents are the hook events of githooks(5) (git 2.56): an unsandboxed
// git runs with hook.<event>.enabled=false for each (L1318 b). Since git
// 2.54 a hook may be named in the configuration (hook.<name>.command with
// hook.<name>.event), and core.hooksPath does not turn those off; git has
// no switch for every event, and an older git ignores the key. The list
// holds every event, not only the ones a network command fires (pre-push,
// post-merge, post-checkout, post-rewrite, reference-transaction,
// pre-auto-gc, pre-rebase, pre-merge-commit, prepare-commit-msg,
// commit-msg, post-index-change), so a later path to one fires nothing.
var hookEvents = []string{
	"applypatch-msg", "pre-applypatch", "post-applypatch", "pre-commit", "pre-merge-commit", "prepare-commit-msg", "commit-msg", "post-commit",
	"pre-rebase", "post-checkout", "post-merge", "pre-push", "pre-receive", "update", "proc-receive", "post-receive", "post-update",
	"reference-transaction", "push-to-checkout", "pre-auto-gc", "post-rewrite", "sendemail-validate", "fsmonitor-watchman", "post-index-change",
}

// UnsandboxedEnv is env (KEY=VALUE) for an unsandboxed line: without
// GIT_CONFIG_PARAMETERS and CDPATH (a leading `cd <folder>` goes where
// ProfileAt judged it, T181), and with gitOverrides as GIT_CONFIG_KEY_<n> and
// GIT_CONFIG_VALUE_<n> after the owner's own GIT_CONFIG_COUNT entries (none
// when the count is not a number), the owner's credential helpers (each a
// key and a value, as `git config --get-regexp` lists the global and system
// ones) right after the reset of the helpers. gate, when given, are Jhon's
// merge gate's entries (T219 S5, hook.MergeGateUnsandboxed: every other
// hook the repository names for reference-transaction disabled, the gate's
// own values forced): they replace the switch that turns the event off, so
// git runs the gate alone for it.
func UnsandboxedEnv(env []string, helpers [][2]string, gate ...[2]string) []string {
	n := 0
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GIT_CONFIG_COUNT="); ok {
			if c, err := strconv.Atoi(v); err == nil && c > 0 {
				n = c
			}
		}
	}
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k == "GIT_CONFIG_PARAMETERS" || k == "GIT_CONFIG_COUNT" || k == "CDPATH" {
			continue
		}
		if i, ok := configIndex(k); ok && i >= n {
			continue
		}
		out = append(out, kv)
	}
	var entries [][2]string
	for _, o := range gitOverrides {
		entries = append(entries, o)
		if o[0] == "credential.helper" {
			entries = append(entries, helpers...)
		}
	}
	for _, event := range hookEvents {
		if event == "reference-transaction" && len(gate) > 0 {
			entries = append(entries, gate...)
			continue
		}
		entries = append(entries, [2]string{"hook." + event + ".enabled", "false"})
	}
	for i, e := range entries {
		out = append(out, "GIT_CONFIG_KEY_"+strconv.Itoa(n+i)+"="+e[0], "GIT_CONFIG_VALUE_"+strconv.Itoa(n+i)+"="+e[1])
	}
	return append(out, "GIT_CONFIG_COUNT="+strconv.Itoa(n+len(entries)))
}

// RepositoryGitConfig is why an unsandboxed line may not run outside the
// sandbox given the configuration `git config --list --show-scope -z`
// lists in a folder it works in (T249 A2, L39; GitFolders), "" when it may:
// every key the repository's own configuration sets (the local and worktree
// scopes, and what their include.* bring in; any scope but system, global
// and command) must be on the allowlist (safeRepositoryKey), with its
// value, or be one of forced, the entries the line runs with anyway (the
// merge gate's, hook.MergeGateUnsandboxed) with the same value. Any other
// key (http.cookieFile, http.proxy, url.*.insteadOf, lfs.*, include.path, a
// filter, a key git adds tomorrow) keeps the line in the sandbox: a
// denylist of the keys that run a command or write a file missed some twice
// (L33, L39), so none is kept. The owner's keys (system, global, command)
// are not judged.
func RepositoryGitConfig(listing string, forced [][2]string) string {
	fields := strings.Split(listing, "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		scope := fields[i]
		key, value, _ := strings.Cut(fields[i+1], "\n")
		switch scope {
		case "system", "global", "command":
			continue
		}
		if key == "core.worktree" {
			return "the repository's configuration sets core.worktree (as a submodule's does), which may put the files git writes outside the repository"
		}
		if safeRepositoryKey(key, value) || slices.ContainsFunc(forced, func(e [2]string) bool { return strings.EqualFold(e[0], key) && e[1] == value }) {
			continue
		}
		return "the repository's configuration sets " + key + ", which is not among the keys a git line runs outside the sandbox with"
	}
	return ""
}

// safeRepositoryKey is whether key, as git lists it (section and name in
// lower case), may be set by the repository on a line that runs outside the
// sandbox, with value. None names a program, a file git writes or a place
// it connects to but the remote itself:
//   - core's format keys, which only say how the repository is laid out;
//     core.hooksPath, which gitOverrides sets to /dev/null after it;
//   - extensions.* of the format (objectformat, worktreeconfig,
//     refstorage, noop, preciousobjects); partialclone is out (it fetches
//     from a promisor remote with keys of its own);
//   - a remote's url and pushurl (not a <transport>:: helper, not an
//     option), fetch and push refspecs (refs inside the repository),
//     tagopt (--tags or --no-tags) and prune; a branch's merge, rebase
//     (not interactive, which opens an editor), and remote and pushremote,
//     judged as a remote's url (either may be a URL itself);
//   - user.name and user.email; the signing switches and format
//     (commit.gpgsign, tag.gpgsign, gpg.format) and user.signingkey, which
//     picks one of the owner's keys (an accepted choice: the programs,
//     gpg.program and gpg.<format>.program and gpg.ssh.defaultKeyCommand,
//     stay out); gpg.ssh.allowedSignersFile, a file only read to verify;
//   - init.defaultBranch, pull.rebase (not interactive) and pull.ff,
//     push.default and push.autoSetupRemote, fetch.prune.
//
// submodule.* is out (submodules are off on such a line anyway).
func safeRepositoryKey(key, value string) bool {
	section, rest, _ := strings.Cut(key, ".")
	sub, name := "", rest
	if i := strings.LastIndex(rest, "."); i >= 0 {
		sub, name = rest[:i], rest[i+1:]
	}
	notInteractive := func(v string) bool {
		switch strings.ToLower(v) {
		case "", "true", "false", "merges", "yes", "no", "on", "off", "1", "0":
			return true
		}
		return false
	}
	switch key {
	case "core.repositoryformatversion", "core.filemode", "core.bare", "core.logallrefupdates", "core.ignorecase",
		"core.precomposeunicode", "core.symlinks", "core.hookspath",
		"extensions.objectformat", "extensions.worktreeconfig", "extensions.refstorage", "extensions.noop", "extensions.preciousobjects",
		"user.name", "user.email", "user.signingkey", "commit.gpgsign", "tag.gpgsign", "gpg.ssh.allowedsignersfile",
		"init.defaultbranch", "pull.ff", "push.default", "push.autosetupremote", "fetch.prune":
		return true
	case "gpg.format":
		return slices.Contains([]string{"openpgp", "x509", "ssh"}, strings.ToLower(value))
	case "pull.rebase":
		return notInteractive(value)
	}
	switch {
	case section == "remote" && sub != "" && (name == "url" || name == "pushurl"):
		return !strings.Contains(value, "::") && !strings.HasPrefix(value, "-")
	case section == "remote" && sub != "" && (name == "fetch" || name == "push" || name == "prune"):
		return true
	case section == "remote" && sub != "" && name == "tagopt":
		return value == "--tags" || value == "--no-tags"
	case section == "branch" && sub != "" && (name == "remote" || name == "pushremote"):
		// It names a remote, or is itself the URL when no remote has that
		// name: judged as a remote's url (L40 A4).
		return !strings.Contains(value, "::") && !strings.HasPrefix(value, "-")
	case section == "branch" && sub != "" && name == "merge":
		return true
	case section == "branch" && sub != "" && name == "rebase":
		return notInteractive(value)
	}
	return false
}

// configIndex is the n of GIT_CONFIG_KEY_<n> or GIT_CONFIG_VALUE_<n>.
func configIndex(key string) (int, bool) {
	for _, prefix := range []string{"GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_"} {
		if v, ok := strings.CutPrefix(key, prefix); ok {
			i, err := strconv.Atoi(v)
			return i, err == nil
		}
	}
	return 0, false
}

// Helpers are the credential helpers `git config --get-regexp
// '^credential\..*helper$'` lists, one `<key> <value>` per line (a key with
// an empty value alone): `credential.helper` and the URL-scoped
// `credential.<url>.helper`, which gh's setup-git writes.
func Helpers(listing string) [][2]string {
	var out [][2]string
	for _, line := range strings.Split(listing, "\n") {
		if line == "" {
			continue
		}
		k, v, _ := strings.Cut(line, " ")
		if strings.HasPrefix(k, "credential.") && strings.HasSuffix(k, ".helper") {
			out = append(out, [2]string{k, v})
		}
	}
	return out
}

// UVToolEnv are the variables that move uv's tool folders, for a work
// command (T128g, L1305): its tools go to a folder of the sandbox's own
// under the cache, $XDG_CACHE_HOME/jhon/sandbox/uv-tools and its bin, so
// uvx runs and the owner's installed tools (~/.local/share/uv/tools, linked
// from ~/.local/bin) stay read-only. A variable the owner set is kept.
func UVToolEnv(in Input) []string {
	dir := filepath.Join(in.xdg("XDG_CACHE_HOME", ".cache"), "jhon", "sandbox", "uv-tools")
	var out []string
	for _, v := range [][2]string{{"UV_TOOL_DIR", dir}, {"UV_TOOL_BIN_DIR", filepath.Join(dir, "bin")}} {
		if _, set := in.Env[v[0]]; !set {
			out = append(out, v[0]+"="+v[1])
		}
	}
	return out
}
