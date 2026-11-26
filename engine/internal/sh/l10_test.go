package sh

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// T228 L10: ssh's words are read with its getopt grammar; a word that names
// a local-command option the grammar did not read as one is unreadable.
func TestSSHGrammar(t *testing.T) {
	for line, want := range map[string][]string{
		`ssh -vo ProxyCommand=a`:                                     {"a"},
		`ssh -4oProxyCommand=a`:                                      {"a"},
		`ssh -qCo 'ProxyCommand a'`:                                  {"a"},
		`ssh -o '"ProxyCommand"=a'`:                                  {"a"},
		`ssh -o "'proxycommand' a"`:                                  {"a"},
		"ssh -o 'PROXYCOMMAND\ta'":                                   {"a"},
		`ssh -p 22 -i k -o LocalCommand=b -o PermitLocalCommand=yes`: {"b"},
		`ssh host -o KnownHostsCommand=c`:                            {"c"},
		`scp -Co ProxyCommand=d`:                                     {"d"},
		`sftp -o ProxyCommand=e`:                                     {"e"},
		`ssh -o ProxyCommand=none -vvv -J j -F /dev/null -o IdentitiesOnly=yes`: nil,
		`ssh -- -v`: nil,
	} {
		cmds, err := Parse(line)
		if err != nil || len(cmds) != 1 {
			t.Fatalf("Parse(%q): %v", line, err)
		}
		if got, err := SSHCommands(cmds[0].Words); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("SSHCommands(%q) = %q, %v; want %q", line, got, err, want)
		}
	}
	// A word that names a command option outside the grammar's reading.
	for _, line := range []string{`ssh -l proxycommand host`, `ssh -o SetEnv=X=localcommand`, `ssh host echo KnownHostsCommand`,
		`ssh -o 'Proxy"Command"=a'`, `ssh -o PermitLocalCommand=localcommand`, `ssh -- -o ProxyCommand=a`} {
		cmds, _ := Parse(line)
		if _, err := SSHCommands(cmds[0].Words); !errors.As(err, new(*Unreadable)) {
			t.Errorf("SSHCommands(%q) reads it: %v", line, err)
		}
	}
	// --config-env takes two words too; GitSetting reads a key's value.
	if got, transport, _ := GitSetting("core.sshCommand=ssh -vo ProxyCommand=a", nil); !transport || !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("GitSetting(core.sshCommand) = %q", got)
	}
	if got, transport, _ := GitSetting("alias.y=!a b", []string{"y", "c"}); transport || !reflect.DeepEqual(got, []string{"a b c"}) {
		t.Errorf("GitSetting(alias) = %q", got)
	}
	if got, _ := route(t, "git --config-env core.pager=X log"); !strings.Contains(strings.Join(got, "\n"), "| git --config-env core.pager=X log") {
		t.Errorf("git --config-env two words: %q", got)
	}
}
