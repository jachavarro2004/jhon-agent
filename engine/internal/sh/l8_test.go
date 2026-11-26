package sh

import (
	"strings"
	"testing"
)

// T228 L8: what the reader now reads, as the bare command.
func TestL8Reads(t *testing.T) {
	for src, want := range map[string]string{
		`sed 's/.*/git push/e' x`:                                 "sed | git push",
		`sed -n -e '1e git push' x`:                               "sed | git push",
		`print -P '$(git push)'`:                                  "print | git push",
		`test -v 'a[$(git push)]'`:                                "| git push",
		`printf -v 'a[$(git push)]' x`:                            "| git push",
		`x='a[$(git push)]'; [[ $x -eq 0 ]]`:                      "| git push",
		`[[ -v 'a[$(git push)]' ]]`:                               "| git push",
		`GIT_PAGER='git push' git log`:                            "| git push",
		`export GIT_EXTERNAL_DIFF='git push'`:                     "| git push",
		`env EDITOR='git push' git commit`:                        "env | git push",
		`LESSOPEN='|git push %s' less x`:                          "| git push %s",
		`GIT_CONFIG_VALUE_0='git push' git log`:                   "| git push",
		`GIT_CONFIG_PARAMETERS="'core.pager'='git push'" git log`: "| git push",
		`git -c core.sshCommand='git push' fetch`:                 "git | git push",
		`git -c credential.helper='!git push' fetch`:              "git | git push",
		`git fetch 'ext::sh -c git% push'`:                        "git sh -c | git push",
		`git difftool -x 'git push' -y`:                           "git | git push",
		`git grep -O'git push' x`:                                 "git | git push",
		`git clone --upload-pack='git push' . x`:                  "git | git push",
		`git archive --exec='git push' HEAD`:                      "git | git push",
		`git send-email --sendmail-cmd='git push' x`:              "git | git push",
		`bash -c -e 'git push'`:                                   "bash -c | git push",
		`ls *(e:'sh -c "git push"':)`:                             "sh -c | git push",
	} {
		got, err := route(t, src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if !strings.Contains(strings.Join(got, "\n"), want) {
			t.Errorf("%q: programs %q, want one %q", src, got, want)
		}
	}
	// ssh as an ssh command is the transport git runs anyway; data stays data.
	for _, src := range []string{`git -c core.sshCommand='ssh -i k' fetch`, `GIT_SSH_COMMAND='ssh -o X=1' git fetch`, `sed 's/a/b/w out' x`,
		`print '$(git push)'`, `test -v HOME`} {
		got, err := route(t, src)
		if err != nil || strings.Contains(strings.Join(got, "\n"), "| ssh") || strings.Contains(strings.Join(got, "\n"), "| git push") {
			t.Errorf("%q: programs %q %v", src, got, err)
		}
	}
	if _, err := route(t, `sed e x`); err == nil {
		t.Error("sed's bare e command runs its input: not read as unreadable")
	}
}

// T228 L8: a qualifier whose code may rename its matches makes the word a
// target the reader cannot tell; one that only tests does not.
func TestL8GlobRewrites(t *testing.T) {
	for pat, want := range map[string]bool{
		`e:'REPLY=~':`: true, `e:'reply=(~)':`: true, `P:..:`: true, `:h:h`: true, `[1]:a:h`: true, `:s/a/b/`: true, `+f`: true,
		`e:'[[ -s $REPLY ]]':`: false, `e:'test -x $REPLY':`: false, `oe:'REPLY=${#REPLY}':`: false, `.om[1]`: false, `N`: false,
	} {
		if got := globRewrites(pat); got != want {
			t.Errorf("globRewrites(%q) = %v, want %v", pat, got, want)
		}
	}
	got := words(t, `rm -rf dist/*(e:'REPLY=~':)`)
	if last := got[len(got)-1]; !strings.HasSuffix(last[len(last)-1], "$(…)") {
		t.Errorf("the rewritten glob is a target the reader tells: %q", got)
	}
	if q := qualifierCodes(`e:'$(echo rm) -rf ~':`); !q.dynamic {
		t.Error("a computed program in an e string is told")
	}
}

// T228 L8: the names a line defines as a function or an alias.
func TestDefinedNames(t *testing.T) {
	got := DefinedNames(`ls() { "$@"; }; function grep { :; }; alias cat='nix-shell --run'; echo x`)
	for _, n := range []string{"ls", "grep", "cat"} {
		if !got[n] {
			t.Errorf("%s is not defined: %v", n, got)
		}
	}
	if got["echo"] {
		t.Errorf("echo is defined: %v", got)
	}
}
