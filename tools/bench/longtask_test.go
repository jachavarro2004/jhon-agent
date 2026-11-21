package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// longIDs are the long tasks (T46): several owner messages or longer work,
// where method decides the result.
var longIDs = []string{"invoice-slices", "slow-suite", "two-modules"}

// toolWords are words a prompt never uses: it never names a tool.
var toolWords = []string{"background", "subagent", "sub-agent", "agent", "monitor", "bash", "tool", "parallel", "jhon"}

// Every task is one of the six short, the three long or the workspace ones (T192).
func TestEveryTaskIsShortLongOrAWorkspace(t *testing.T) {
	tasks, err := LoadTasks("tasks", nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	if want := slices.Sorted(slices.Values(slices.Concat(taskIDs, longIDs, workspaceIDs))); !slices.Equal(ids, want) {
		t.Fatalf("tasks %v, want %v", ids, want)
	}
}

func TestTheLongTasksLoad(t *testing.T) {
	tasks, err := LoadTasks("tasks", longIDs)
	if err != nil {
		t.Fatal(err)
	}
	messages := map[string]int{"invoice-slices": 3, "slow-suite": 2, "two-modules": 1}
	for _, task := range tasks {
		if task.MaxTurns < 80 || task.MaxTurns > 100 || task.Timeout < 40*time.Minute || task.Timeout > 45*time.Minute {
			t.Errorf("%s: max_turns %d timeout %s, want 80-100 and 40-45m", task.ID, task.MaxTurns, task.Timeout)
		}
		if len(task.Prompts) != messages[task.ID] || (task.MultiMessage() && len(task.MessageChecks) != len(task.Prompts)) {
			t.Errorf("%s: %d prompts, checks %q", task.ID, len(task.Prompts), task.MessageChecks)
		}
		for i, p := range task.Prompts {
			words := strings.FieldsFunc(strings.ToLower(p), func(r rune) bool { return !('a' <= r && r <= 'z' || r == '-' || r == '_') })
			for _, w := range toolWords {
				if slices.Contains(words, w) || slices.Contains(words, w+"s") {
					t.Errorf("%s: prompt %d names %q", task.ID, i+1, w)
				}
			}
			if strings.Contains(p, "test_acceptance") || strings.Contains(p, "test_m") {
				t.Errorf("%s: prompt %d names a hidden test", task.ID, i+1)
			}
		}
		if _, err := os.Stat(filepath.Join(task.Template(), "test_acceptance.py")); err == nil {
			t.Errorf("%s: the hidden test is visible in the template", task.ID)
		}
		if len(task.Scope) == 0 || task.Tests != "test_*.py" || task.TestCommand != "python3 -m unittest" {
			t.Errorf("%s: scope %q tests %q test_command %q", task.ID, task.Scope, task.Tests, task.TestCommand)
		}
	}
}

// states are a long task's reference states: testdata/solutions/<id>/<n>/
// for each message n of a multi-message task, else testdata/solutions/<id>.
func states(task Task) []string {
	base := filepath.Join("testdata", "solutions", task.ID)
	if !task.MultiMessage() {
		return []string{base}
	}
	var out []string
	for n := 1; n <= len(task.Prompts); n++ {
		out = append(out, filepath.Join(base, strconv.Itoa(n)))
	}
	return out
}

// runCheck runs command in a copy of repo with the hidden directory in it.
func runCheck(t *testing.T, task Task, repo, command string) (bool, string) {
	t.Helper()
	dir := t.TempDir()
	for _, src := range []string{repo, task.HiddenDir()} {
		if err := copyTree(src, dir); err != nil {
			t.Fatal(err)
		}
	}
	c := exec.Command("sh", "-c", command)
	c.Dir, c.Env = dir, append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	out, err := c.CombinedOutput()
	return err == nil, string(out)
}

// TestEachLongTaskHoldsAtEveryReferenceState is the proof each long task is
// a task: the visible tests pass on the template and at every reference
// state; the hidden test fails on the template and passes on the last
// state; after message n's state every check_k with k <= n passes, and
// check_n failed at the state before (message n asks for something new);
// every file a state writes is in the task's scope or a test.
func TestEachLongTaskHoldsAtEveryReferenceState(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("the tasks need python3")
	}
	tasks, err := LoadTasks("tasks", longIDs)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		t.Run(task.ID, func(t *testing.T) {
			repo := t.TempDir()
			if err := copyTree(task.Template(), repo); err != nil {
				t.Fatal(err)
			}
			visible(t, repo)
			if ok, out := runCheck(t, task, repo, task.Check); ok {
				t.Fatalf("RED: the hidden test passes on the template:\n%s", out)
			}
			for i, state := range states(task) {
				n := i + 1
				if task.MultiMessage() && task.MessageChecks[i] != "" {
					if ok, out := runCheck(t, task, repo, task.MessageChecks[i]); ok {
						t.Errorf("check_%d passes before message %d:\n%s", n, n, out)
					}
				}
				if err := filepath.WalkDir(state, func(p string, d os.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return err
					}
					rel, _ := filepath.Rel(state, p)
					if !task.InScope(rel) && !task.IsTest(rel) {
						t.Errorf("%s writes %s, outside the scope %q", state, rel, task.Scope)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if err := copyTree(state, repo); err != nil {
					t.Fatal(err)
				}
				visible(t, repo)
				for k, check := range task.MessageChecks[:min(n, len(task.MessageChecks))] {
					if ok, out := runCheck(t, task, repo, check); !ok {
						t.Errorf("check_%d fails after message %d's reference:\n%s", k+1, n, out)
					}
				}
			}
			if ok, out := runCheck(t, task, repo, task.Check); !ok {
				t.Fatalf("GREEN: the hidden test fails on the reference:\n%s", out)
			} else {
				t.Logf("GREEN %s: %s", task.ID, lastLine(out))
			}
		})
	}
}

// TestTheSlowSuiteFailsOnTheTemplateAndPassesAtEachState runs slow-suite's
// full suite with its delay set to 0 in a scratch copy: it fails on the
// template (both bugs show) and passes at both reference states. As shipped
// it takes about 90 seconds: 30 scenarios of 3 seconds each.
func TestTheSlowSuiteFailsOnTheTemplateAndPassesAtEachState(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("the tasks need python3")
	}
	task, err := LoadTask(filepath.Join("tasks", "slow-suite"))
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(task.Template(), "run_suite.py"))
	if err != nil {
		t.Fatal(err)
	}
	scenarios, err := os.ReadFile(filepath.Join(task.Template(), "suite", "scenarios.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "\nSCENARIO_SECONDS = 3.0\n") || strings.Count(string(scenarios), `"name"`) != 30 {
		t.Fatal("the suite is no longer 30 scenarios of 3 seconds")
	}
	repo := t.TempDir()
	if err := copyTree(task.Template(), repo); err != nil {
		t.Fatal(err)
	}
	fast := strings.Replace(string(src), "\nSCENARIO_SECONDS = 3.0\n", "\nSCENARIO_SECONDS = 0\n", 1)
	suite := func() (bool, string) {
		if err := os.WriteFile(filepath.Join(repo, "run_suite.py"), []byte(fast), 0o644); err != nil {
			t.Fatal(err)
		}
		c := exec.Command("python3", "-B", "run_suite.py")
		c.Dir = repo
		out, err := c.CombinedOutput()
		return err == nil, lastLine(string(out))
	}
	if ok, out := suite(); ok || !strings.Contains(out, "30 scenarios, 15 failed") {
		t.Fatalf("the template's suite: %t %s", ok, out)
	}
	for _, state := range states(task) {
		if err := copyTree(state, repo); err != nil {
			t.Fatal(err)
		}
		if ok, out := suite(); !ok {
			t.Errorf("the suite fails at %s: %s", state, out)
		}
	}
}
