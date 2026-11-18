package ldd

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T146h (L1278, L1280): a small task starts lean. T1: `jhon status` tells a
// change that is not small to read the full context of the runtime it runs
// in, by its install path; a small change is never told. L4: the small path
// names the bare `jhon executions run`, two runs, no unit's run between.

// statusIn is `jhon status` in dir with HOME at home and the runtime's
// environment: pi's agent directory, or Claude Code's marker.
func statusIn(t *testing.T, dir, home, runtime string, args ...string) support.Result {
	t.Helper()
	env := []string{"HOME=" + home, "PI_CODING_AGENT_DIR=", "CLAUDECODE="}
	switch runtime {
	case "pi":
		env = append(env[:1], "PI_CODING_AGENT_DIR="+filepath.Join(home, ".pi/jhon"), "CLAUDECODE=")
	case "claude-code":
		env = append(env[:1], "PI_CODING_AGENT_DIR=", "CLAUDECODE=1")
	}
	return support.Jhon(t, dir, support.Env(env...), append([]string{"status"}, args...)...)
}

// notSmallLine is the line status prints for a change that is not small.
func notSmallLine(home, file string) string {
	return "not small: read " + filepath.Join(home, ".local/share/jhon/current/prose", file) + " before the next edit"
}

func TestStatusTellsANotSmallChangeToReadTheFullContext(t *testing.T) {
	t.Parallel()
	folder := recordRepo(t, map[string]string{"a/b.go": "package a\n\nvar B = 1\n", "a/b_test.go": "package a\n"})
	home := temp(t)
	for rt, file := range map[string]string{"claude-code": "full-claude-code.md", "pi": "full-pi.md"} {
		r := statusIn(t, folder, home, rt, "c1")
		equal(t, r.Code, 0, r.Out())
		match(t, r.Stdout, `(?m)^  - `+regexp.QuoteMeta(notSmallLine(home, file))+`$`)
		j := object(t, statusIn(t, folder, home, rt, "c1", "--json").Stdout)
		equal(t, anyMatch(j["notes"], "^"+regexp.QuoteMeta(notSmallLine(home, file))+"$"), true, j["notes"])
	}
	// Outside both runtimes, both files are named.
	r := statusIn(t, folder, home, "", "c1")
	match(t, r.Stdout, `(?m)^  - not small: read `+regexp.QuoteMeta(filepath.Join(home, ".local/share/jhon/current/prose/full-claude-code.md"))+` \(pi: full-pi\.md\) before the next edit$`)
}

func TestStatusNeverTellsASmallChangeOrNoChangeToReadMore(t *testing.T) {
	t.Parallel()
	home := temp(t)
	small := smallRepo(t)
	s := statusIn(t, small, home, "pi")
	equal(t, s.Code, 0, s.Out())
	noMatch(t, s.Stdout, "not small: read")
	support.Git(t, small, "checkout", "-q", "main")
	n := statusIn(t, small, home, "pi")
	equal(t, n.Code, 0, n.Out())
	noMatch(t, n.Stdout, "not small: read")
}

// L4: the small change's next command and its RED note name the bare run,
// twice, and no unit's run between.
func TestSmallChangeNamesTheBareRun(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	h := jhonGit(t, root, "status")
	equal(t, h.Code, 0, h.Stderr)
	match(t, h.Stdout, `(?m)^next        apply  \(suggestion\)\ncommand     jhon executions run$`)
	match(t, h.Stdout, "(?m)^  - no RED before GREEN on record for feat-small-fix: run `jhon executions run` while its new test fails \\(RED\\), then `jhon executions run` again once it passes \\(GREEN\\): two runs, nothing between$")
	noMatch(t, h.Stdout, "unit's run")
	b := jhonGit(t, root, "status", "--brief")
	match(t, b.Stdout, `(?m)^command     jhon executions run$`)

	// A change with a folder keeps the slice base and the unit's run.
	folder := recordRepo(t, map[string]string{"a/b.go": "package a\n\nvar B = 1\n", "a/b_test.go": "package a\n"})
	support.Write(t, folder, map[string]string{"openspec/changes/c1/tasks.md": "## Intent\n\nx\n\nDelivers: x\n\n- [x] 1.1 x\n"})
	f := jhonGit(t, folder, "status", "c1")
	noMatch(t, f.Stdout, "(?m)^command     jhon executions run$")
}

// A branch that would name a change with no file yet is told the bare run.
func TestPendingSmallChangeNamesTheBareRun(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	support.Git(t, root, "stash", "-u", "-q")
	r := jhonGit(t, root, "status")
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, "(?m)names change feat-small-fix, which has no changes yet: [^\n]*then run `jhon executions run`$")
}

// L4: `jhon executions run --help` opens with the two-line common case, the
// bare run twice; the inner loop's detail follows, once.
func TestExecutionsRunHelpOpensWithTheCommonCase(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"executions", "run", "--help"}, {"executions", "--help"}} {
		r := jhonGit(t, temp(t), args...)
		equal(t, r.Code, 0, args, r.Out())
		lines := strings.SplitN(r.Stdout, "\n", 4)
		equal(t, lines[:3], []string{
			"common case: `jhon executions run` while the new test fails (RED),",
			"then `jhon executions run` again once it passes (GREEN); commit, then `jhon deliver check`.",
			"",
		}, args)
		match(t, r.Stdout, "(?m)^  jhon executions run \\[<change>\\]")
		equal(t, strings.Count(r.Stdout, "common case:"), 1, args)
	}
}
