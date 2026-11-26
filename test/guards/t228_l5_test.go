package guards

import (
	"strings"
	"testing"
)

// t228L5Preserve are ordinary lines that name a destructive word only as
// data: a program the guard knows never runs its arguments, git, or an
// unlisted program whose words hold no destructive command.
var t228L5Preserve = []string{
	"grep rm notes.txt", "grep -rn 'rm -rf' .", `echo "rm -rf ~"`, `printf 'rm -rf ~\n'`, "git log --grep=rm",
	`git commit -m "drop rm -rf ~ from setup"`, "cat notes-about-rm.txt", "rg 'rm -rf ~' docs", "head -n 5 rm.log", "wc -l rm",
	"jq '.rm' f.json", "ls rm", "sed -i 's/rm -rf ~//' x.sh", "python3 -m pytest -k rm", "npm test -- --grep 'rm -rf'", "make clean",
	"go test ./... -run TestRm", "tmux send-keys -t x 'git status' Enter", "nix-shell --run 'go test ./...'", "entr go test ./...",
	"awk '{print $1}' f", "watchexec -- go test ./...", "conda run -n base python x.py",
}

func notAllowed(t *testing.T, s sgEnv, c string) {
	t.Helper()
	sub := s.bash(t, c, sgOpts{})
	code(t, sub, 2, "sub-agent: "+c)
	tp := s.bash(t, c, top())
	if d, _, ok := decision(t, tp.Stdout); tp.Code != 2 && (!ok || d != "ask") {
		t.Errorf("top: %q is allowed: %d %q %q", c, tp.Code, tp.Stdout, tp.Stderr)
	}
}

func allowed(t *testing.T, s sgEnv, c string) {
	t.Helper()
	for _, o := range []sgOpts{{}, top()} {
		r := s.bash(t, c, o)
		code(t, r, 0, c)
		if r.Stdout != "" || r.Stderr != "" {
			t.Errorf("top %v: %q: output %q %q", o.top, c, r.Stdout, r.Stderr)
		}
	}
}

func refused(t *testing.T, s sgEnv, c, why string) {
	t.Helper()
	for _, o := range []sgOpts{{}, top()} {
		r := s.bash(t, c, o)
		code(t, r, 2, c)
		if !strings.HasPrefix(r.Stderr, "safety-guard: refused: ") || !strings.Contains(r.Stderr, why) {
			t.Errorf("top %v: %q: %s", o.top, c, r.Stderr)
		}
	}
}

// TestT228L5UnlistedRunnersAreNotAllowed: L6's class rule.
func TestT228L5UnlistedRunnersAreNotAllowed(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228L5Unlisted {
		notAllowed(t, s, c)
	}
}

// TestT228L5ClassRuleKeepsData: a destructive word given as data keeps its verdict.
func TestT228L5ClassRuleKeepsData(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228L5Preserve {
		allowed(t, s, c)
	}
}

// TestT228L5ReaderBlockers: each is refused as the removal it runs.
func TestT228L5ReaderBlockers(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228L5Reader {
		refused(t, s, c, "")
	}
	for _, c := range t228L5GlobSubst {
		refused(t, s, c, ": it turns on zsh's GLOB_SUBST")
	}
}

// TestT228L5CleanupTrapAndExtglobsStayAllowed: the regressions 9dac1fa caused.
func TestT228L5CleanupTrapAndExtglobsStayAllowed(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228L5Allowed {
		allowed(t, s, c)
	}
}

// TestT228L5TrapAbuseStaysRefused: every trap abuse the verifier tried.
func TestT228L5TrapAbuseStaysRefused(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228L5Abuse {
		for _, o := range []sgOpts{{}, top()} {
			code(t, s.bash(t, c, o), 2, c)
		}
	}
}
