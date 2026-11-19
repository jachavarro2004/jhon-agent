package sandbox

import (
	"path/filepath"
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
// ext::, fd:: or local-path transport, no submodule recursion.
// url.*.insteadOf and include.* in the repository's configuration are not
// neutralized: an accepted residual risk.
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
// GIT_CONFIG_PARAMETERS, and with gitOverrides as GIT_CONFIG_KEY_<n> and
// GIT_CONFIG_VALUE_<n> after the owner's own GIT_CONFIG_COUNT entries (none
// when the count is not a number), the owner's credential helpers (each a
// key and a value, as `git config --get-regexp` lists the global and system
// ones) right after the reset of the helpers.
func UnsandboxedEnv(env []string, helpers [][2]string) []string {
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
		if k == "GIT_CONFIG_PARAMETERS" || k == "GIT_CONFIG_COUNT" {
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
		entries = append(entries, [2]string{"hook." + event + ".enabled", "false"})
	}
	for i, e := range entries {
		out = append(out, "GIT_CONFIG_KEY_"+strconv.Itoa(n+i)+"="+e[0], "GIT_CONFIG_VALUE_"+strconv.Itoa(n+i)+"="+e[1])
	}
	return append(out, "GIT_CONFIG_COUNT="+strconv.Itoa(n+len(entries)))
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
