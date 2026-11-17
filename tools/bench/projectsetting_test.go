package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const wtOn = "worktree_per_change=true"

func TestProjectSettingFlagParsing(t *testing.T) {
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		base := []string{"run", "--home", t.TempDir(), "--results", filepath.Join(t.TempDir(), "r"), "--tasks", "pagination-bug",
			"--variants", "ldd,ldd-native", "--conditions", "jhon", "--dry-run"}
		code := cli(append(base, args...), &out, &errOut, time.Now())
		return code, out.String(), errOut.String()
	}
	for _, args := range [][]string{
		{"--project-setting", wtOn},
		{"--project-setting", "worktree_per_change=false"},
	} {
		code, out, errOut := run(args...)
		want := "project settings: " + args[1] + " (written to each run's LDD config before the agent starts: ldd, ldd-native)\n"
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("%v: exit %d, want %q\n%s%s", args, code, want, out, errOut)
		}
	}
	for args, want := range map[[2]string]string{
		{"--project-setting", "foo=bar"}:                           "bench: --project-setting foo=bar: unknown key foo (known: worktree_per_change)\n",
		{"--project-setting", "worktree_per_change=yes"}:           "bench: --project-setting worktree_per_change=yes: worktree_per_change takes true or false\n",
		{"--project-setting", "worktree_per_change"}:               "bench: --project-setting worktree_per_change: give KEY=VALUE\n",
		{"--project-setting", wtOn + ",worktree_per_change=false"}: "bench: --project-setting worktree_per_change is given twice\n",
		{"--project-setting", wtOn + ",foo=1"}:                     "bench: --project-setting foo=1: unknown key foo (known: worktree_per_change)\n",
	} {
		if code, out, errOut := run(args[:]...); code != 1 || errOut != want {
			t.Errorf("%v: exit %d stderr %q, want %q\n%s", args, code, errOut, want, out)
		}
	}
	// Repeated flags add up (the duplicate is caught across them).
	if code, _, errOut := run("--project-setting", wtOn, "--project-setting", "worktree_per_change=false"); code != 1 || errOut != "bench: --project-setting worktree_per_change is given twice\n" {
		t.Errorf("repeated: exit %d %q", code, errOut)
	}
	if !strings.Contains(usage, "[--project-setting KEY=VALUE[,KEY=VALUE]]") {
		t.Errorf("usage lacks --project-setting:\n%s", usage)
	}
}

func TestProjectSettingRefusesVariantsWithoutConfig(t *testing.T) {
	f := newFake(t)
	for variants, want := range map[string]string{
		"plain,ldd":         "--project-setting sets the task repo's LDD config; variant plain has none at the start (it applies to ldd, ldd-native)",
		"plain,ldd-ask,ldd": "--project-setting sets the task repo's LDD config; variants plain, ldd-ask have none at the start (it applies to ldd, ldd-native)",
		"ldd-ask":           "--project-setting sets the task repo's LDD config; variant ldd-ask has none at the start (it applies to ldd, ldd-native)",
	} {
		o, _ := options(t, f, "pagination-bug", []string{"jhon"}, 1)
		o.Variants, o.ProjectSettings = split(variants), []string{wtOn}
		if _, err := Plan(o); err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %q", variants, err, want)
		}
	}
	// The default variants include plain: refused too.
	o, _ := options(t, f, "pagination-bug", []string{"jhon"}, 1)
	o.Variants, o.ProjectSettings = nil, []string{wtOn}
	if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), "variant plain has none") {
		t.Errorf("default variants: %v", err)
	}
}

func TestResultsNameRecordsTheProjectSettings(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	if got := resultsName(now, "", wtOn); got != "20261113T100000Z-worktree_per_change-true" {
		t.Errorf("got %q", got)
	}
	if got := resultsName(now, "zai/glm-5.3", wtOn); got != "20261113T100000Z-zai-glm-5.3-worktree_per_change-true" {
		t.Errorf("got %q", got)
	}
	if got := resultsName(now, ""); got != "20261113T100000Z" {
		t.Errorf("got %q", got)
	}
}

// The setting is written into the task repo's LDD config, committed with
// the template, and recorded in every run's metrics; without it the config
// and metrics are as before.
func TestProjectSettingIsWrittenCommittedAndRecorded(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join(source(), "ldd", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, on := range []bool{true, false} {
		f := newFake(t)
		putBridge(t, f)
		o, out := options(t, f, "pagination-bug", []string{"jhon", "pi-jhon"}, 1)
		o.claudePath = "/usr/bin/claude"
		o.Variants = []string{"ldd", "ldd-native"}
		if on {
			o.ProjectSettings = []string{wtOn}
		}
		if err := Execute(o); err != nil {
			t.Fatalf("on=%t: %v\n%s", on, err, out)
		}
		want := string(fixture)
		if on {
			want += "worktree_per_change: true\n"
		}
		for _, run := range []struct{ name, config string }{
			{"pagination-bug-ldd-jhon-1", "openspec/config.yaml"}, {"pagination-bug-ldd-pi-jhon-1", "openspec/config.yaml"},
			{"pagination-bug-ldd-native-jhon-1", "ldd/config.yaml"}, {"pagination-bug-ldd-native-pi-jhon-1", "ldd/config.yaml"},
		} {
			dir := filepath.Join(o.Results, "runs", run.name)
			c := exec.Command("git", "show", "HEAD:"+run.config)
			c.Dir = filepath.Join(dir, "repo")
			committed, err := c.Output()
			if err != nil || string(committed) != want {
				t.Errorf("on=%t %s: committed %s %q (%v), want %q", on, run.name, run.config, committed, err, want)
			}
			b, _ := os.ReadFile(filepath.Join(dir, "metrics.json"))
			var m Metrics
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
			if on && (len(m.ProjectSettings) != 1 || m.ProjectSettings[0] != wtOn) {
				t.Errorf("%s project settings %q", run.name, m.ProjectSettings)
			}
			if !on && bytes.Contains(b, []byte("project_settings")) {
				t.Errorf("%s metrics name project settings without the option", run.name)
			}
		}
		if strings.Contains(out.String(), "project settings") != on {
			t.Errorf("on=%t output:\n%s", on, out)
		}
	}
}

// Value keeps a cell run with the setting apart from the same cell
// without it, and compares each with the same no-Jhon counterpart.
func TestValueAndReportKeepProjectSettingsApart(t *testing.T) {
	on, off := t.TempDir(), t.TempDir()
	modelRuns(t, on, "pi-jhon", "zai/glm-5.3", 1, func(m *Metrics) { m.ProjectSettings = []string{wtOn} })
	modelRuns(t, on, "pi-clean", "zai/glm-5.3", 0.5, func(m *Metrics) {
		if m.Repeat == 1 {
			m.RedBeforeGreen = bp(false)
		}
	})
	modelRuns(t, off, "pi-jhon", "zai/glm-5.3", 1, func(m *Metrics) { m.OutOfScopeFiles = ip(1) })
	code, out, errOut := value(t, on, off)
	for _, s := range []string{
		"PASS  invoice-slices plain pi-jhon vs pi-clean plain, model zai/glm-5.3 with worktree_per_change=true",
		"FAIL  invoice-slices plain pi-jhon vs pi-clean plain, model zai/glm-5.3\n",
		"runtime pi zai/glm-5.3 with worktree_per_change=true (pi-jhon vs pi-clean): PASS — 1/1 cells",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s%s", s, out, errOut)
		}
	}
	if code != 1 {
		t.Errorf("exit %d", code)
	}
	var rep bytes.Buffer
	if err := Report(on, &rep); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.String(), "pi-jhon zai/glm-5.3 with worktree_per_change=true") {
		t.Errorf("report:\n%s", rep.String())
	}
}

func TestCertifyKeepsProjectSettingsApart(t *testing.T) {
	base, cand := t.TempDir(), t.TempDir()
	modelRuns(t, base, "pi-jhon", "zai/glm-5.3", 1, nil)
	modelRuns(t, cand, "pi-jhon", "zai/glm-5.3", 1, func(m *Metrics) { m.ProjectSettings = []string{wtOn} })
	code, out, errOut := certify(t, nil, "--baseline", base, "--candidate", cand)
	want := "invoice-slices plain pi-jhon: the baseline ran project settings none, the candidate worktree_per_change=true; certify compares cells with the same project settings (--allow-setting-change to compare them anyway)"
	if code != 2 || !strings.Contains(errOut, want) {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	code, out, errOut = certify(t, nil, "--baseline", base, "--candidate", cand, "--allow-setting-change")
	if !strings.Contains(out, "note: the project settings changed: invoice-slices plain pi-jhon none → worktree_per_change=true") {
		t.Errorf("exit %d\n%s\n%s", code, out, errOut)
	}
	// One side never mixes runs with and without the setting in a cell.
	mixed := t.TempDir()
	modelRuns(t, mixed, "pi-jhon", "zai/glm-5.3", 1, nil)
	modelRuns(t, mixed, "pi-jhon", "zai/glm-5.3@x", 1, func(m *Metrics) { m.Model = "zai/glm-5.3"; m.ProjectSettings = []string{wtOn} })
	code, out, errOut = certify(t, nil, "--baseline", base, "--candidate", mixed)
	if code != 2 || !strings.Contains(errOut, "invoice-slices plain pi-jhon: the candidate holds models zai/glm-5.3 and zai/glm-5.3 with worktree_per_change=true") &&
		!strings.Contains(errOut, "invoice-slices plain pi-jhon: the candidate holds models zai/glm-5.3 with worktree_per_change=true and zai/glm-5.3") {
		t.Errorf("mixed: exit %d\n%s\n%s", code, out, errOut)
	}
}

// T80/T85a's lifecycle with the setting on: the change's worktree outside
// the repository (<repo>-worktrees/<change>), delivered to the default
// branch, then removed with its branch (or left): the delivered tree is the
// main checkout and the change's record is found.
func TestTreeWorktreeDeliveredToDefaultBranch(t *testing.T) {
	for _, removed := range []bool{true, false} {
		w := newTreeRun(t)
		o := w.withJhon()
		w.setUpLDD(w.r.Repo)
		path := filepath.Join(w.r.Repo+"-worktrees", "fix-page")
		w.git(w.r.Repo, "worktree", "add", "-q", "-b", "fix-page", path)
		w.write(path, "pager.py", "fixed\n")
		w.git(path, "commit", "-q", "-am", "fix")
		w.record("fix-page")
		head, _ := headCommit(path, w.env)
		w.write(filepath.Join(w.r.Repo, ".git", "jhon", "changes", "fix-page"), "delivery.json", `{"action":"merge","head_commit":"`+head+`"}`)
		w.git(w.r.Repo, "merge", "-q", "--ff-only", "fix-page")
		if removed {
			w.git(w.r.Repo, "worktree", "remove", path)
			w.git(w.r.Repo, "branch", "-q", "-d", "fix-page")
		}
		m, out := w.factsWith(o)
		if !m.Success || m.CheckedTree != "main" {
			t.Fatalf("removed=%t success=%t checked_tree=%q out=%q problems=%v", removed, m.Success, m.CheckedTree, out, m.Problems)
		}
		if got := recordOf(m); got != "ldd test_record=true route=false record=true" {
			t.Errorf("removed=%t ldd layout %s problems %v", removed, got, m.Problems)
		}
		if len(m.OutOfScope) != 0 {
			t.Errorf("removed=%t out of scope %v", removed, m.OutOfScope)
		}
	}
}
