package sandbox

import (
	"slices"
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
		"GIT_CONFIG_KEY_10=fetch.recurseSubmodules", "GIT_CONFIG_VALUE_10=false",
		// L36: no gc or maintenance left running after the line.
		"GIT_CONFIG_KEY_11=gc.autoDetach", "GIT_CONFIG_VALUE_11=false",
		"GIT_CONFIG_KEY_12=maintenance.autoDetach", "GIT_CONFIG_VALUE_12=false"}, "\n")
	// L1318 (b): every hook event off, configured hooks included.
	for i, event := range hookEvents {
		want += "\nGIT_CONFIG_KEY_" + strconv.Itoa(13+i) + "=hook." + event + ".enabled\nGIT_CONFIG_VALUE_" + strconv.Itoa(13+i) + "=false"
	}
	want += "\nGIT_CONFIG_COUNT=" + strconv.Itoa(13+len(hookEvents))
	if got != want {
		t.Errorf("UnsandboxedEnv =\n%s\nwant\n%s", got, want)
	}
	if got := UnsandboxedEnv([]string{"GIT_CONFIG_COUNT=x"}, nil); got[0] != "GIT_CONFIG_KEY_0=core.hooksPath" || got[len(got)-1] != "GIT_CONFIG_COUNT="+strconv.Itoa(11+len(hookEvents)) {
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

// T219 S5: given the merge gate's entries, reference-transaction stays on
// for it alone: the gate's entries replace the event's switch, every other
// event stays off.
func TestUnsandboxedEnvKeepsTheMergeGateAlone(t *testing.T) {
	gate := [][2]string{{"hook.planted.enabled", "false"}, {"hook.jhon-merge-gate.command", "x"}}
	got := strings.Join(UnsandboxedEnv(nil, nil, gate...), "\n") + "\n"
	for _, event := range hookEvents {
		off := "=hook." + event + ".enabled\nGIT_CONFIG_VALUE_"
		if has := strings.Contains(got, off); has == (event == "reference-transaction") {
			t.Errorf("hook.%s.enabled=false set: %v", event, has)
		}
	}
	if !strings.Contains(got, "=hook.planted.enabled\n") || !strings.Contains(got, "=hook.jhon-merge-gate.command\n") {
		t.Errorf("the gate's entries are missing:\n%s", got)
	}
	if strings.Contains(strings.Join(UnsandboxedEnv(nil, nil), "\n")+"\n", "=hook.jhon-merge-gate.") {
		t.Errorf("with no gate, no gate entry")
	}
}

// L39: the repository's proxy no longer needs GIT_PROXY_COMMAND: a
// repository that sets core.gitProxy keeps the line in the sandbox
// (RepositoryGitConfig), and the owner's own proxy works again.
func TestUnsandboxedEnvLeavesTheOwnersProxy(t *testing.T) {
	if got := UnsandboxedEnv([]string{"PATH=/usr/bin"}, nil); slices.ContainsFunc(got, func(kv string) bool { return strings.HasPrefix(kv, "GIT_PROXY_COMMAND") }) {
		t.Errorf("UnsandboxedEnv sets GIT_PROXY_COMMAND: %q", got[:3])
	}
	if got := UnsandboxedEnv([]string{"GIT_PROXY_COMMAND=/usr/bin/corkscrew"}, nil); got[0] != "GIT_PROXY_COMMAND=/usr/bin/corkscrew" {
		t.Errorf("the owner's GIT_PROXY_COMMAND was not kept: %q", got[:2])
	}
}

// entry is one entry of `git config --list --show-scope -z`.
func entry(scope, key, value string) string { return scope + "\x00" + key + "\n" + value + "\x00" }

// L39 (A2's second break, the owner's rule: drop the denylist): an outside
// git line runs outside only while every key the repository's own
// configuration sets is on the allowlist; any other keeps it in the
// sandbox. The owner's keys are not judged.
func TestRepositoryGitConfigAllowsOnlyTheAllowlist(t *testing.T) {
	// The owner's own repository (jhon-agent's .git/config, values made up
	// where they are his), as git lists it: every key stays outside.
	owner := entry("global", "credential.helper", "!gh auth git-credential") + entry("global", "http.proxy", "http://owner") +
		entry("local", "core.repositoryformatversion", "0") + entry("local", "core.filemode", "true") + entry("local", "core.bare", "false") +
		entry("local", "core.logallrefupdates", "true") + entry("local", "core.hookspath", "tools/git-hooks") +
		entry("local", "remote.origin.url", "git@github-personal:o/r.git") + entry("local", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*") +
		entry("local", "branch.main.remote", "origin") + entry("local", "branch.main.merge", "refs/heads/main") +
		entry("local", "gpg.format", "ssh") + entry("local", "user.signingkey", "key::ssh-ed25519 AAAA") +
		entry("local", "commit.gpgsign", "true") + entry("local", "tag.gpgsign", "true") +
		entry("local", "gpg.ssh.allowedsignersfile", "/home/u/.config/git/allowed_signers")
	if why := RepositoryGitConfig(owner, nil); why != "" {
		t.Errorf("the owner's repository: %s", why)
	}
	allowed := []string{
		entry("local", "core.ignorecase", "true"), entry("local", "core.precomposeunicode", "true"), entry("local", "core.symlinks", "true"),
		entry("worktree", "extensions.worktreeconfig", "true"), entry("local", "extensions.objectformat", "sha256"), entry("local", "extensions.refstorage", "reftable"),
		entry("local", "remote.up.pushurl", "https://example.invalid/r.git"), entry("local", "remote.up.push", "HEAD"), entry("local", "remote.up.tagopt", "--no-tags"),
		entry("local", "remote.up.prune", "true"), entry("local", "branch.x.rebase", "merges"), entry("local", "branch.x.pushremote", "up"),
		entry("local", "user.name", "o"), entry("local", "user.email", "o@x"), entry("local", "init.defaultbranch", "main"),
		entry("local", "pull.rebase", "true"), entry("local", "pull.ff", "only"), entry("local", "push.default", "simple"),
		entry("local", "push.autosetupremote", "true"), entry("local", "fetch.prune", "true"), entry("local", "gpg.format", "openpgp"),
	}
	for _, e := range allowed {
		if why := RepositoryGitConfig(e, nil); why != "" {
			t.Errorf("%q: %s", e, why)
		}
	}
	for _, e := range []string{
		entry("local", "http.cookiefile", "/home/u/target"), entry("local", "http.savecookies", "true"), entry("local", "http.proxy", "http://x"),
		entry("local", "http.sslverify", "false"), entry("local", "url.https://evil/.insteadof", "https://github.com/"), entry("local", "lfs.url", "https://x"),
		entry("local", "include.path", "../x"), entry("local", "includeif.onbranch:main.path", "../x"), entry("local", "filter.evil.smudge", "evil"),
		entry("local", "core.gitproxy", "evil"), entry("local", "core.sshcommand", "evil"), entry("local", "core.fsmonitor", "evil"),
		entry("local", "core.askpass", "evil"), entry("local", "core.editor", "evil"), entry("local", "core.pager", "evil"),
		entry("local", "credential.helper", "evil"), entry("local", "gpg.program", "evil"), entry("local", "gpg.ssh.program", "evil"),
		entry("local", "gpg.ssh.defaultkeycommand", "evil"), entry("local", "merge.x.driver", "evil"), entry("local", "diff.x.textconv", "evil"),
		entry("local", "remote.origin.vcs", "evil"), entry("local", "remote.origin.uploadpack", "evil"), entry("local", "remote.origin.receivepack", "evil"),
		entry("local", "remote.origin.proxy", "http://x"), entry("local", "remote.origin.url", "evil::x"), entry("local", "remote.origin.url", "-oProxyCommand=x"),
		entry("local", "remote.origin.tagopt", "--upload-pack=evil"), entry("local", "pull.rebase", "interactive"), entry("local", "branch.main.rebase", "i"),
		entry("local", "gpg.format", "evil"), entry("local", "submodule.x.update", "!evil"), entry("local", "extensions.partialclone", "origin"),
		entry("local", "hook.x.command", "evil"), entry("local", "alias.fetch", "!evil"), entry("local", "a.key.git.adds.tomorrow", "x"),
		entry("worktree", "http.cookiefile", "/home/u/target"),
	} {
		if why := RepositoryGitConfig(e, nil); why == "" {
			t.Errorf("%q stays outside", e)
		}
	}
	if why := RepositoryGitConfig(entry("local", "core.worktree", "/home/u"), nil); why != "the repository's configuration sets core.worktree, which would move the files git writes outside the project" {
		t.Errorf("core.worktree: %q", why)
	}
	if why := RepositoryGitConfig(entry("local", "http.cookiefile", "/x"), nil); why != "the repository's configuration sets http.cookiefile, which is not among the keys a git line runs outside the sandbox with" {
		t.Errorf("the reason: %q", why)
	}
	// The merge gate's own entries, with the values the line forces.
	gate := [][2]string{{"hook.jhon-merge-gate.event", ""}, {"hook.jhon-merge-gate.event", "reference-transaction"}, {"hook.jhon-merge-gate.command", "'/j' hook merge-gate"}, {"hook.jhon-merge-gate.enabled", "true"}}
	installed := entry("local", "hook.jhon-merge-gate.event", "reference-transaction") + entry("local", "hook.jhon-merge-gate.command", "'/j' hook merge-gate") + entry("local", "hook.jhon-merge-gate.enabled", "true")
	if why := RepositoryGitConfig(installed, gate); why != "" {
		t.Errorf("the merge gate's entries: %s", why)
	}
	if why := RepositoryGitConfig(entry("local", "hook.jhon-merge-gate.command", "evil"), gate); why == "" {
		t.Error("a merge gate command the line does not force stays outside")
	}
	if why := RepositoryGitConfig(installed, nil); why == "" {
		t.Error("the merge gate's entries stay outside where the line forces none")
	}
	if why := RepositoryGitConfig("", nil); why != "" {
		t.Errorf("an empty listing: %q", why)
	}
}
