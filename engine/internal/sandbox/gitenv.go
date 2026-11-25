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
// ext::, fd:: or local-path transport, no submodule recursion. The
// repository's other keys that run a command (ProjectGitConfig, T249 A2)
// follow them, and git's proxy for git:// is off (GIT_PROXY_COMMAND, empty,
// unless the owner's environment names one). url.*.insteadOf in the
// repository's configuration is not neutralized: an accepted residual risk;
// what include.* brings in is read as the repository's own.
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
//
// project are the entries ProjectGitConfig gives for the repository's own
// keys that run a command; they follow gitOverrides.
func UnsandboxedEnv(env []string, helpers, project [][2]string, gate ...[2]string) []string {
	n := 0
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GIT_CONFIG_COUNT="); ok {
			if c, err := strconv.Atoi(v); err == nil && c > 0 {
				n = c
			}
		}
	}
	var out []string
	proxy := false
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k == "GIT_CONFIG_PARAMETERS" || k == "GIT_CONFIG_COUNT" || k == "CDPATH" {
			continue
		}
		if i, ok := configIndex(k); ok && i >= n {
			continue
		}
		proxy = proxy || k == "GIT_PROXY_COMMAND"
		out = append(out, kv)
	}
	// git reads core.gitProxy only when the variable is unset; empty, it
	// runs no proxy (the repository's would win over a later entry: git
	// takes its first matching value).
	if !proxy {
		out = append(out, "GIT_PROXY_COMMAND=")
	}
	var entries [][2]string
	for _, o := range gitOverrides {
		entries = append(entries, o)
		if o[0] == "credential.helper" {
			entries = append(entries, helpers...)
		}
	}
	entries = append(entries, project...)
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

// ProjectGitConfig is what an unsandboxed line takes from the configuration
// `git config --list --show-scope -z` lists in a folder it works in (T249
// A2; GitFolders): an entry for each key the repository's own configuration
// (the local and worktree scopes, with what their include.* bring in; any
// scope but system, global and command) sets that runs a command, in the
// listing's order: the owner's last value for it (system, global or
// command), else a value that runs nothing the repository chose
// (neutralGit). The keys gitOverrides sets already (core.hooksPath,
// core.sshCommand, core.fsmonitor, the credential helpers, protocol.*,
// hook.*) and core.gitProxy (UnsandboxedEnv's GIT_PROXY_COMMAND) take
// none. refuse is why the line may not run outside the sandbox at all: the
// repository's core.worktree, which moves the files git writes (a pull's
// checkout) outside the project; "" when nothing stands in the way.
func ProjectGitConfig(listing string) (entries [][2]string, refuse string) {
	type entry struct{ scope, key, value string }
	var all []entry
	fields := strings.Split(listing, "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		key, value, _ := strings.Cut(fields[i+1], "\n")
		all = append(all, entry{fields[i], key, value})
	}
	owner := map[string]string{}
	var keys []string
	for _, e := range all {
		switch e.scope {
		case "system", "global", "command":
			owner[e.key] = e.value
			continue
		}
		if e.key == "core.worktree" {
			refuse = "the repository's configuration sets core.worktree, which would move the files git writes outside the project"
		}
		if _, ok := neutralGit(e.key); ok && !slices.Contains(keys, e.key) {
			keys = append(keys, e.key)
		}
	}
	for _, k := range keys {
		v, mine := owner[k]
		if !mine {
			v, _ = neutralGit(k)
		}
		entries = append(entries, [2]string{k, v})
	}
	return entries, refuse
}

// neutralGit is the value for key, a key as git lists it (section and name
// in lower case), that runs nothing the repository chose, and whether key
// is one ProjectGitConfig overrides: a filter's clean, smudge and process
// and a diff driver's command and textconv run nothing; a merge driver runs
// false, so the merge stops with a conflict; a remote's vcs names no
// helper, so its fetch fails, and its upload-pack and receive-pack are
// git's own; the editors are :, the pager none, a pager.<command> off; the
// signing programs git's defaults and gpg.ssh.defaultKeyCommand false.
func neutralGit(key string) (string, bool) {
	section, rest, _ := strings.Cut(key, ".")
	sub, name := "", rest
	if i := strings.LastIndex(rest, "."); i >= 0 {
		sub, name = rest[:i], rest[i+1:]
	}
	switch key {
	case "core.askpass", "core.pager", "core.alternaterefscommand", "diff.external":
		return "", true
	case "core.editor", "sequence.editor":
		return ":", true
	case "gpg.program", "gpg.openpgp.program":
		return "gpg", true
	case "gpg.x509.program":
		return "gpgsm", true
	case "gpg.ssh.program":
		return "ssh-keygen", true
	case "gpg.ssh.defaultkeycommand":
		return "false", true
	}
	switch {
	case section == "pager" && sub == "":
		return "false", true
	case section == "filter" && sub != "" && slices.Contains([]string{"clean", "smudge", "process"}, name):
		return "", true
	case section == "merge" && sub != "" && name == "driver":
		return "false", true
	case section == "diff" && sub != "" && (name == "command" || name == "textconv"):
		return "", true
	case section == "remote" && sub != "" && name == "vcs":
		return "", true
	case section == "remote" && sub != "" && name == "uploadpack":
		return "git-upload-pack", true
	case section == "remote" && sub != "" && name == "receivepack":
		return "git-receive-pack", true
	case section == "gpg" && sub != "" && name == "program":
		return "false", true
	}
	return "", false
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
