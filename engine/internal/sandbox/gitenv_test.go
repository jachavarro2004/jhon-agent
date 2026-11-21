package sandbox

import (
	"strconv"
	"strings"
	"testing"
)

// L1318 (b): the events a network command fires are among the ones turned off.
func TestHookEventsHoldEveryEventANetworkCommandFires(t *testing.T) {
	for _, event := range []string{"pre-push", "post-merge", "post-checkout", "post-rewrite", "reference-transaction", "pre-auto-gc", "pre-rebase", "pre-merge-commit", "prepare-commit-msg", "commit-msg", "post-index-change"} {
		if !strings.Contains(strings.Join(UnsandboxedEnv(nil, nil), "\n"), "=hook."+event+".enabled\n") {
			t.Errorf("hook.%s.enabled is not set", event)
		}
	}
}

// L1306: the overrides follow the owner's own entries, the owner's helpers
// follow the reset, GIT_CONFIG_PARAMETERS is gone, the rest is kept.
func TestUnsandboxedEnvAppendsTheOverridesAfterTheOwners(t *testing.T) {
	env := []string{"PATH=/usr/bin", "GIT_CONFIG_PARAMETERS='core.hookspath'='x'", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0=o", "GIT_CONFIG_KEY_5=stale", "HOME=/home/u"}
	got := strings.Join(UnsandboxedEnv(env, [][2]string{{"credential.helper", "cache"}}), "\n")
	want := strings.Join([]string{"PATH=/usr/bin", "GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0=o", "HOME=/home/u",
		"GIT_CONFIG_KEY_1=core.hooksPath", "GIT_CONFIG_VALUE_1=/dev/null",
		"GIT_CONFIG_KEY_2=core.sshCommand", "GIT_CONFIG_VALUE_2=ssh",
		"GIT_CONFIG_KEY_3=core.fsmonitor", "GIT_CONFIG_VALUE_3=false",
		"GIT_CONFIG_KEY_4=credential.helper", "GIT_CONFIG_VALUE_4=",
		"GIT_CONFIG_KEY_5=credential.helper", "GIT_CONFIG_VALUE_5=cache",
		"GIT_CONFIG_KEY_6=protocol.ext.allow", "GIT_CONFIG_VALUE_6=never",
		"GIT_CONFIG_KEY_7=protocol.fd.allow", "GIT_CONFIG_VALUE_7=never",
		"GIT_CONFIG_KEY_8=protocol.file.allow", "GIT_CONFIG_VALUE_8=never",
		"GIT_CONFIG_KEY_9=submodule.recurse", "GIT_CONFIG_VALUE_9=false",
		"GIT_CONFIG_KEY_10=fetch.recurseSubmodules", "GIT_CONFIG_VALUE_10=false"}, "\n")
	// L1318 (b): every hook event off, configured hooks included.
	for i, event := range hookEvents {
		want += "\nGIT_CONFIG_KEY_" + strconv.Itoa(11+i) + "=hook." + event + ".enabled\nGIT_CONFIG_VALUE_" + strconv.Itoa(11+i) + "=false"
	}
	want += "\nGIT_CONFIG_COUNT=" + strconv.Itoa(11+len(hookEvents))
	if got != want {
		t.Errorf("UnsandboxedEnv =\n%s\nwant\n%s", got, want)
	}
	if got := UnsandboxedEnv([]string{"GIT_CONFIG_COUNT=x"}, nil); got[0] != "GIT_CONFIG_KEY_0=core.hooksPath" || got[len(got)-1] != "GIT_CONFIG_COUNT="+strconv.Itoa(9+len(hookEvents)) {
		t.Errorf("a count that is not a number: %q", got)
	}
}

func TestHelpersAreTheCredentialHelpersGitLists(t *testing.T) {
	got := Helpers("credential.helper cache --timeout=1\ncredential.https://github.com.helper\ncredential.https://github.com.helper !/usr/bin/gh auth git-credential\ncredential.username x\n")
	want := [][2]string{{"credential.helper", "cache --timeout=1"}, {"credential.https://github.com.helper", ""}, {"credential.https://github.com.helper", "!/usr/bin/gh auth git-credential"}}
	if len(got) != len(want) {
		t.Fatalf("Helpers = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Helpers[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// L1305: uv's tool folders move under the cache, unless the owner set them.
func TestUVToolEnvMovesUvsToolFoldersUnderTheCache(t *testing.T) {
	in := sample(machine)
	if got := strings.Join(UVToolEnv(in), " "); got != "UV_TOOL_DIR=/home/u/.cache/jhon/sandbox/uv-tools UV_TOOL_BIN_DIR=/home/u/.cache/jhon/sandbox/uv-tools/bin" {
		t.Errorf("UVToolEnv = %s", got)
	}
	in.Env["XDG_CACHE_HOME"], in.Env["UV_TOOL_DIR"] = "/data/cache", "/opt/uv"
	if got := strings.Join(UVToolEnv(in), " "); got != "UV_TOOL_BIN_DIR=/data/cache/jhon/sandbox/uv-tools/bin" {
		t.Errorf("UVToolEnv with the owner's UV_TOOL_DIR = %s", got)
	}
}

// L1306: a git line stays unsandboxed only working inside the project; a
// clone only into it. gh lines that write files where the line names give
// work.
func TestProfileAtKeepsGitInsideTheProject(t *testing.T) {
	d := with(machine, disk{"/home/u/project/sub": "dir", "/home/u/project/link": "-> /home/u", "/home/u/other": "dir"})
	roots := []string{"/home/u/project", "/home/u/project-worktrees"}
	for _, c := range []struct{ cwd, line, want string }{
		{"/home/u/project", "git fetch", Unsandboxed},
		{"/home/u/project/sub", "git -C .. pull", Unsandboxed},
		{"/home/u/project", "git -C /home/u/project/sub fetch", Unsandboxed},
		{"/home/u/project", "git clone https://h/o/r.git", Unsandboxed},
		{"/home/u/project", "git clone https://h/o/r.git new/place", Unsandboxed},
		{"/home/u/project", "git clone --bare -b main https://h/o/r", Unsandboxed},
		{"/home/u/project", "git clone https://h/o/r.git ../r", Work},
		{"/home/u/project", "git clone https://h/o/r.git /home/u/r", Work},
		{"/home/u/project", "git clone https://h/o/r.git link/r", Work},
		{"/home/u/project", "git -C /home/u/other fetch", Work},
		{"/home/u/project", "git -C .. fetch", Work},
		{"/home/u/project", "git -C sub -C ../.. fetch", Work},
		{"/home/u/project", "git fetch && git -C /home/u/other push", Work},
		{"/home/u/other", "git fetch", Work},
		{"/home/u/project", "git clone https://h/o/..", Work},
		{"relative", "git fetch", Work},
		{"/home/u/project", "gh pr list", Unsandboxed},
		{"/home/u/project", "gh repo clone o/r", Work},
		{"/home/u/project", "gh repo sync", Work},
		{"/home/u/project", "gh repo fork o/r --clone", Work},
		{"/home/u/project", "gh repo create x --public --clone", Work},
		{"/home/u/project", "gh repo create x --public", Unsandboxed},
		{"/home/u/project", "gh release download v1", Work},
		{"/home/u/project", "gh run download 1", Work},
		{"/home/u/project", "gh issue develop 3 --checkout", Work},
		{"/home/u/project", "gh run list", Unsandboxed},
	} {
		if got := ProfileAt(c.line, c.cwd, roots, d.stat, nil); got != c.want {
			t.Errorf("ProfileAt(%q in %s) = %q, want %q", c.line, c.cwd, got, c.want)
		}
	}
}
