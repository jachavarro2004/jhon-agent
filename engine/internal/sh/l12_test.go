package sh

import "testing"

// T228 L12: only the commands of an ssh transport that is one plain forward
// to %h/%p are marked Forward; a pager, an alias, a bare ssh line, and any
// other transport command are not.
func TestForwardMarks(t *testing.T) {
	marked := func(src string) (forward []string) {
		t.Helper()
		cmds, err := Parse(src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		for _, c := range cmds {
			ps, err := CommandPrograms(c, nil, nil, Opts{})
			if err != nil {
				t.Fatalf("%q: %v", src, err)
			}
			for _, p := range ps {
				if p.Forward {
					forward = append(forward, p.Name)
				}
			}
		}
		return forward
	}
	for src, want := range map[string]string{
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh -W %h:%p b"' git fetch`:                    "ssh",
		`git -c core.sshCommand='ssh -o ProxyCommand="nc %h %p"' fetch`:                       "nc",
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh b nc %h %p"' git fetch`:                    "ssh",
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh -J j %h"' git fetch`:                       "ssh",
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="aws ssm start-session --target %h"' git fetch`: "aws",
	} {
		if got := marked(src); len(got) != 1 || got[0] != want {
			t.Errorf("%q: forward programs %q, want %s", src, got, want)
		}
	}
	for _, src := range []string{`PAGER='ssh b' git log`, `git -c alias.x='!ssh b' x`, `ssh -W x:22 b`, `git -c core.pager='ssh b' log`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="sh -c \"aws ssm x\""' git fetch`, `GIT_SSH_COMMAND='ssh -oKnownHostsCommand="kh %H"' git fetch`} {
		if got := marked(src); len(got) > 0 {
			t.Errorf("%q: forward programs %q", src, got)
		}
	}
	for v, want := range map[string]bool{
		"ssh -W %h:%p bastion": true, "ssh -W %h:%p bastion 2>/dev/null": true, "autossh -M 0 -W %h:%p b": true, "ssh -qJ j %h": true,
		"ssh b ncat %h %p": true, "cloudflared access ssh --hostname %h": true, "nc %h %p": true,
		"ssh b": false, "ssh b uptime %h": false, "ssh -W %h:%p b; x": false, "ssh -W %h:%p b > log": false, "ssh -W %h:%p $(b)": false,
		"ssh -W %h:%p b &": false, "a %h | b": false, "ssh -W %h:%p b nc %h %p": false, "ssh b nc %h": false, "X=1 nc %h %p": false,
		"(nc %h %p)": false, "terraform apply": false,
	} {
		if got := ForwardCommand(v); got != want {
			t.Errorf("ForwardCommand(%q) = %v, want %v", v, got, want)
		}
	}
}
