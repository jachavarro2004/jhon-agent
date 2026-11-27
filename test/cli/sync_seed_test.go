// T266, the owner's decision in L1768: `jhon sync`, from the owner's
// terminal, seeds the launch check's records of the roots `jhon start`
// never saw: every root under sandbox.allow_write (each repository, its
// worktrees, the folder itself) and every project with Jhon state. It shows
// the roots where something runs, a count per root and no line of theirs,
// and asks once; with no terminal it skips in one line; inside the sandbox
// it never seeds; a root already recorded is not asked again.
//
// Each sync here runs with JHON_ROOT on a folder that is no checkout, so
// after the seed the sync proper stops at once (no source checkout), and
// no test builds a release.
package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// seedMachine is a machine whose allow_write folder ~/work holds a
// repository with a committed hook (a), whose worktree lives outside it
// (~/elsewhere/side), and one where nothing runs (b); its project has a
// mode (Jhon state) and an MCP server.
func seedMachine(t *testing.T) (machine, []string) {
	t.Helper()
	m := newMachine(t)
	work := filepath.Join(m.home, "work")
	a, b := filepath.Join(work, "a"), filepath.Join(work, "b")
	for _, d := range []string{a, b, filepath.Join(m.home, "elsewhere")} {
		must(t, os.MkdirAll(d, 0o755))
	}
	support.GitRepo(t, a)
	support.GitRepo(t, b)
	support.Write(t, a, map[string]string{".claude/settings.json": plantedHook})
	commit(t, a, "a hook")
	support.Write(t, b, map[string]string{".claude/rules/x.md": "prose\n"})
	commit(t, b, "prose")
	support.Git(t, a, "worktree", "add", "-q", "-b", "side", filepath.Join(m.home, "elsewhere", "side"))
	support.Write(t, m.proj, map[string]string{".mcp.json": `{"mcpServers":{"x":{"command":"x-mcp","env":{"X_TOKEN":"s3cr3t"}}}}`})
	commit(t, m.proj, "a server")
	support.Write(t, m.home, map[string]string{".config/jhon/settings.toml": "schema = 1\n\n[sandbox]\nallow_write = [\"~/work\"]\n"})
	if r := support.Jhon(t, m.proj, m.env, "mode", "yolo", "--root", m.proj); r.Code != 0 {
		t.Fatalf("jhon mode yolo: exit %d, stderr %q", r.Code, r.Stderr)
	}
	noCheckout := t.TempDir()
	env := append(slices.Clone(m.env), "JHON_ROOT="+noCheckout)
	return m, env
}

const noSource = "jhon: sync needs the Jhon source checkout, not found at ~/dev/jhon (run 'jhon sync' from your checkout once, or set JHON_SOURCE); to update an installed Jhon run 'jhon update'\n"

func seedSummary(m machine) string {
	return "jhon: what runs outside the sandbox in 3 folders you never started in, to record as approved:\n" +
		"  " + filepath.Join(m.home, "elsewhere", "side") + ": 1 file that runs\n" +
		"  " + m.proj + ": 1 file that runs\n" +
		"  " + filepath.Join(m.home, "work", "a") + ": 1 file that runs\n" +
		"  and 2 folders where nothing runs\n" +
		"jhon: `jhon start` asks once in each of them unless you record them now; record them? [y/N] "
}

// syncOnTerminal runs `jhon sync` with a terminal on its stdin that
// answers answer: its exit code and stderr.
func syncOnTerminal(t *testing.T, env []string, dir, answer string) (int, string) {
	t.Helper()
	ptm, pts := openPTY(t)
	defer ptm.Close()
	cmd := exec.Command(support.Bin(t), "sync")
	cmd.Dir, cmd.Env, cmd.Stdin = dir, env, pts
	var errb strings.Builder
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pts.Close()
	if _, err := ptm.Write([]byte(answer)); err != nil {
		t.Fatal(err)
	}
	go func() {
		b := make([]byte, 256)
		for {
			if _, err := ptm.Read(b); err != nil {
				return
			}
		}
	}()
	err := cmd.Wait()
	code := 0
	if x, ok := err.(*exec.ExitError); ok {
		code = x.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, errb.String()
}

// firstStarts asks whether each root where something runs still asks at
// its first start (want true) or starts with no question.
func firstStarts(t *testing.T, m machine, what string, ask bool) {
	t.Helper()
	for _, dir := range []string{filepath.Join(m.home, "work", "a"), filepath.Join(m.home, "elsewhere", "side"), m.proj} {
		l := m.try(t, dir)
		if ask {
			asked(t, what+": "+dir, l, dir+" (first start here)\n")
		} else {
			quiet(t, what+": "+dir, l)
		}
	}
	quiet(t, what+": nothing runs", m.try(t, filepath.Join(m.home, "work", "b")))
}

func TestSyncSeedsTheHookRecordsOnceFromTheOwnersTerminal(t *testing.T) {
	t.Parallel()
	m, env := seedMachine(t)
	code, stderr := syncOnTerminal(t, env, m.home, "y\n")
	want := seedSummary(m) + "jhon: recorded 5 folders\n" + noSource
	if code != 1 || stderr != want {
		t.Errorf("sync answered y: exit %d, stderr:\n%s\nwant exit 1 (no checkout), stderr:\n%s", code, stderr, want)
	}
	if strings.Contains(stderr, "s3cr3t") || strings.Contains(stderr, "pwned") {
		t.Errorf("the seed shows a line of a file: %q", stderr)
	}
	firstStarts(t, m, "after the seed", false)

	// Idempotent: every root is recorded, so the next sync asks nothing,
	// and a root already recorded is not asked again even when it changed
	// since (its next start asks instead).
	support.Write(t, m.proj, map[string]string{".claude/hooks/x.sh": "#!/bin/sh\n"})
	code, stderr = syncOnTerminal(t, env, m.home, "y\n")
	if code != 1 || stderr != noSource {
		t.Errorf("the second sync: exit %d, stderr:\n%s\nwant only:\n%s", code, stderr, noSource)
	}
	asked(t, "a change after the seed", m.try(t, m.proj), "new .claude/hooks/x.sh\n")
}

func TestSyncSeedRecordsNothingUnlessTheOwnerSaysYes(t *testing.T) {
	t.Parallel()
	m, env := seedMachine(t)
	code, stderr := syncOnTerminal(t, env, m.home, "n\n")
	want := seedSummary(m) + "jhon: nothing was recorded\n" + noSource
	if code != 1 || stderr != want {
		t.Errorf("sync answered n: exit %d, stderr:\n%s\nwant:\n%s", code, stderr, want)
	}
	firstStarts(t, m, "after n", true)
}

// With no terminal the seed is skipped in one line and the sync goes on;
// inside the sandbox it never seeds, terminal or not.
func TestSyncSkipsTheSeedWithNoTerminalAndInsideTheSandbox(t *testing.T) {
	t.Parallel()
	m, env := seedMachine(t)
	r := support.Jhon(t, m.home, env, "sync")
	want := "jhon: the hook seed was skipped: no terminal to ask on; 3 folders where something runs will ask at the first `jhon start` there\n" + noSource
	if r.Code != 1 || r.Stdout != "" || r.Stderr != want {
		t.Errorf("sync with no terminal: exit %d, stdout %q, stderr:\n%s\nwant:\n%s", r.Code, r.Stdout, r.Stderr, want)
	}
	code, stderr := syncOnTerminal(t, append(slices.Clone(env), "JHON_IN_SANDBOX=1"), m.home, "y\n")
	if code != 1 || stderr != noSource {
		t.Errorf("sync inside the sandbox: exit %d, stderr:\n%s\nwant only:\n%s", code, stderr, noSource)
	}
	firstStarts(t, m, "after no seed", true)
}
