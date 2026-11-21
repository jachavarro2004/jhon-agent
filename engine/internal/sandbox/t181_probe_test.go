package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// T181 (L1503): only a leading `cd <folder> &&` into the roots and a lone
// {word} for gh under bash/sh may move a line from work to unsandboxed.
func TestT181OnlyALeadingCdIntoTheProjectLeavesTheSandbox(t *testing.T) {
	base := t.TempDir()
	p, out := filepath.Join(base, "p"), filepath.Join(base, "outside")
	for _, d := range []string{filepath.Join(p, "sub"), filepath.Join(out, "sub2")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(out, filepath.Join(p, "lnk")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		line, want string
		zsh        bool
	}{
		{"cd sub && git fetch origin", Unsandboxed, false},
		{"cd " + p + "/sub && git fetch origin", Unsandboxed, false},
		{"cd 'sub' && gh api repos/{owner}/{repo}/pulls", Unsandboxed, false},
		{"cd sub && git -C .. fetch origin", Unsandboxed, false},
		{"cd .. && git fetch origin", Work, false},
		{"cd /tmp && git fetch origin", Work, false},
		{"cd ~ && git fetch origin", Work, false},
		{"cd - && git fetch origin", Work, false},
		{"cd $HOME && git fetch origin", Work, false},
		{`cd "$(pwd)" && git fetch origin`, Work, false},
		{"cd lnk && git fetch origin", Work, false},
		{"cd " + p + "/lnk && git fetch origin", Work, false},
		{"cd lnk/sub2 && git fetch origin", Work, false},
		{"cd " + p + "/../outside && git fetch origin", Work, false},
		{"cd -P sub && git fetch origin", Work, false},
		{"cd -- sub && git fetch origin", Work, false},
		{"cd sub; git fetch origin", Work, false},
		{"cd sub && git fetch origin || touch x", Work, false},
		{"cd sub && git fetch origin & touch x", Work, false},
		{"cd sub && git -c core.sshCommand=x fetch origin", Work, false},
		{"cd sub && git -c protocol.ext.allow=always fetch origin", Work, false},
		{"cd sub && git fetch --upload-pack=x origin", Work, false},
		{"cd sub && git fetch 'ext::sh -c x'", Work, false},
		{"cd sub && gh api repos/x/y | sh", Work, false},
		{"gh api {a,b}", Work, false},
		{"gh {x..y}", Work, false},
		{"cd sub && { touch x; }", Work, false},
		{"cd sub && gh api x <<< y", Work, false},
		{"cd sub && git fetch origin\ntouch x", Work, false},
		{"cd sub && cd /tmp && git fetch origin", Work, false},
		{"cd sub && git -C ../.. fetch origin", Work, false},
		{"cd sub >/dev/null && git fetch origin", Work, false},
		{"( cd sub && git fetch origin )", Work, false},
		{"cd sub && git fetch origin", Work, true},
		{"gh api repos/{owner}/{repo}/pulls", Work, true},
		{"cd " + p + "/sub && git fetch origin", Unsandboxed, true},
	}
	for _, c := range cases {
		if got := profileAt(c.line, p, []string{p}, Disk, c.zsh); got != c.want {
			t.Errorf("profileAt(%q, zsh=%v) = %s, want %s", c.line, c.zsh, got, c.want)
		}
	}
}

// The note names git's network subcommands and gh only (T181, L1503).
func TestT181TheDeliveryNoteOnlyForNetworkGitOrGh(t *testing.T) {
	for _, c := range []struct {
		argv []string
		note bool
	}{
		{[]string{"/usr/bin/bash", "-c", "git status >/dev/null; gh pr list"}, true},
		{[]string{"/usr/bin/bash", "-c", "echo x; git fetch nowhere"}, true},
		{[]string{"/usr/bin/bash", "-c", "echo; git -c a.b=c fetch nowhere"}, true},
		{[]string{"/usr/bin/git", "fetch", "nowhere"}, true},
		{[]string{"/usr/bin/bash", "-c", "echo; git remote -v update"}, true},
		{[]string{"/usr/bin/bash", "-c", "echo x; ls /nonexistent"}, false},
		{[]string{"/usr/bin/bash", "-c", "git -C /nonexistent status"}, false},
		{[]string{"/usr/bin/bash", "-c", "git remote add origin https://example.invalid/x.git"}, false},
		{[]string{"/usr/bin/git", "remote", "get-url", "nope"}, false},
	} {
		if got := DeliveryNote(c.argv) != ""; got != c.note {
			t.Errorf("DeliveryNote(%q) gives a note: %v, want %v", c.argv, got, c.note)
		}
	}
}
