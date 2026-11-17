package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var taskIDs = []string{"account-import", "contacts-migration", "late-fee", "pagination-bug", "search-accents", "shipping-rules"}

// trapIDs are the trap tasks (T40): a hasty fix fails their hidden test.
var trapIDs = []string{"contacts-migration", "late-fee", "search-accents"}

func TestTheSixTasksLoad(t *testing.T) {
	tasks, err := LoadTasks("tasks", taskIDs)
	if err != nil {
		t.Fatal(err)
	}
	var ids, kinds []string
	for _, task := range tasks {
		ids, kinds = append(ids, task.ID), append(kinds, task.Kind)
		if task.MaxTurns != 60 || task.Timeout != 30*time.Minute {
			t.Errorf("%s: max_turns %d timeout %s, want 60 and 30m", task.ID, task.MaxTurns, task.Timeout)
		}
		if task.Model == "" || !strings.HasSuffix(task.Prompt, "\n") || strings.Contains(task.Prompt, "test_acceptance") {
			t.Errorf("%s: model %q, prompt %q", task.ID, task.Model, task.Prompt)
		}
		if _, err := os.Stat(filepath.Join(task.Template(), "test_acceptance.py")); err == nil {
			t.Errorf("%s: the hidden test is visible in the template", task.ID)
		}
		if len(task.Scope) == 0 || task.Tests != "test_*.py" || task.TestCommand != "python3 -m unittest" {
			t.Errorf("%s: scope %q tests %q test_command %q", task.ID, task.Scope, task.Tests, task.TestCommand)
		}
	}
	if strings.Join(ids, ",") != strings.Join(taskIDs, ",") || strings.Join(kinds, ",") != "risky,data,ambiguous,bug,regression,rules" {
		t.Fatalf("tasks %v kinds %v", ids, kinds)
	}
}

func TestParseFlatYAMLReadsBlocksAndRefusesBadLines(t *testing.T) {
	f, err := parseFlatYAML("id: a\n# note\nprompt: |\n  one\n\n  two\ncheck: \"x y\"\n")
	if err != nil || f["id"] != "a" || f["prompt"] != "one\n\ntwo\n" || f["check"] != "x y" {
		t.Fatalf("%q %v", f, err)
	}
	f, err = parseFlatYAML("scope: |\n  a.py\n  b/c.py\n")
	if err != nil || f["scope"] != "a.py\nb/c.py\n" {
		t.Fatalf("%q %v", f, err)
	}
	for _, bad := range []string{"no colon\n", "  indented: x\n", "id: a\nid: b\n"} {
		if _, err := parseFlatYAML(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// visible runs the template's own tests in repo.
func visible(t *testing.T, repo string) string {
	t.Helper()
	c := exec.Command("python3", "-B", "-m", "unittest", "-q")
	c.Dir = repo
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("visible tests fail in %s: %v\n%s", repo, err, out)
	}
	return string(out)
}

// TestEachHiddenTestFailsOnTheTemplateAndPassesOnTheReference is the proof
// each task is a task: the template's own tests pass, the hidden test fails
// on the untouched template (RED) and passes on the reference solution in
// testdata/solutions (GREEN), with the visible tests still passing.
func TestEachHiddenTestFailsOnTheTemplateAndPassesOnTheReference(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("the tasks need python3")
	}
	tasks, err := LoadTasks("tasks", taskIDs)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		t.Run(task.ID, func(t *testing.T) {
			untouched := t.TempDir()
			if err := copyTree(task.Template(), untouched); err != nil {
				t.Fatal(err)
			}
			visible(t, untouched)
			ok, out, err := task.Accept(untouched, os.Environ())
			if err != nil || ok {
				t.Fatalf("RED: the hidden test passes on the untouched template (%v):\n%s", err, out)
			}
			t.Logf("RED %s:\n%s", task.ID, lastLine(out))

			solved := t.TempDir()
			if err := copyTree(task.Template(), solved); err != nil {
				t.Fatal(err)
			}
			if err := copyTree(filepath.Join("testdata", "solutions", task.ID), solved); err != nil {
				t.Fatal(err)
			}
			ok, out, err = task.Accept(solved, os.Environ())
			if err != nil || !ok {
				t.Fatalf("GREEN: the hidden test fails on the reference (%v):\n%s", err, out)
			}
			t.Logf("GREEN %s:\n%s", task.ID, lastLine(out))
			visible(t, solved)
		})
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// solvedWith is a copy of task's template with testdata/<kind>/<id> over it.
func solvedWith(t *testing.T, task Task, kind string) string {
	t.Helper()
	dir := t.TempDir()
	if err := copyTree(task.Template(), dir); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(filepath.Join("testdata", kind, task.ID), dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestEachTrapFailsOnTheNaiveSolution is the proof each trap is a trap: the
// hasty fix in testdata/naive (the one the prompt alone suggests) fails the
// hidden test, while the reference in testdata/solutions passes it (the
// test above). Every file either solution touches is in the task's scope.
func TestEachTrapFailsOnTheNaiveSolution(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("the tasks need python3")
	}
	tasks, err := LoadTasks("tasks", trapIDs)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		t.Run(task.ID, func(t *testing.T) {
			naive := solvedWith(t, task, "naive")
			ok, out, err := task.Accept(naive, os.Environ())
			if err != nil || ok {
				t.Fatalf("the hidden test passes on the naive solution (%v):\n%s", err, out)
			}
			t.Logf("NAIVE %s:\n%s", task.ID, lastLine(out))
			for _, kind := range []string{"naive", "solutions"} {
				entries, err := os.ReadDir(filepath.Join("testdata", kind, task.ID))
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range entries {
					if !task.InScope(e.Name()) {
						t.Errorf("%s/%s is outside the scope %q", kind, e.Name(), task.Scope)
					}
				}
			}
		})
	}
}

// writeTask writes a task.yaml with body after the required keys, and a template.
func writeTask(t *testing.T, body string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "multi")
	if err := os.MkdirAll(filepath.Join(dir, "template"), 0o755); err != nil {
		t.Fatal(err)
	}
	head := "id: multi\nkind: long\nmodel: sonnet\nmax_turns: 90\ntimeout: 45m\ncheck: python3 -m unittest\n"
	if err := os.WriteFile(filepath.Join(dir, "task.yaml"), []byte(head+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestATaskTakesNumberedPromptsAndTheirChecks(t *testing.T) {
	task, err := LoadTask(writeTask(t, "prompt_2: |\n  two\nprompt_1: |\n  one\ncheck_2: python3 -m unittest test_m2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(task.Prompts, []string{"one\n", "two\n"}) || task.Prompt != "one\n" || !task.MultiMessage() {
		t.Errorf("prompts %q prompt %q", task.Prompts, task.Prompt)
	}
	if !slices.Equal(task.MessageChecks, []string{"", "python3 -m unittest test_m2"}) {
		t.Errorf("message checks %q", task.MessageChecks)
	}
	single, err := LoadTask(writeTask(t, "prompt: |\n  only\n"))
	if err != nil || !slices.Equal(single.Prompts, []string{"only\n"}) || single.MultiMessage() || single.MessageChecks != nil {
		t.Errorf("single %+v %v", single, err)
	}
}

func TestNumberedPromptsMustHaveNoGapsAndNoPlainPrompt(t *testing.T) {
	for body, want := range map[string]string{
		"prompt_1: |\n  a\nprompt_3: |\n  c\n":            "prompt_2 is missing",
		"prompt_2: |\n  b\n":                              "prompt_1 is missing",
		"prompt: x\nprompt_1: |\n  a\n":                   "prompt and prompt_1",
		"prompt_x: |\n  a\n":                              "prompt_x is not prompt_<n>",
		"prompt_0: |\n  a\nprompt_1: |\n  b\n":            "prompt_0 is not prompt_<n>",
		"prompt_1: |\n  a\ncheck_2: x\n":                  "check_2 has no prompt_2",
		"prompt_1: |\n  a\nprompt_2: |\n  \ncheck_1: x\n": "prompt_2 is empty",
	} {
		if _, err := LoadTask(writeTask(t, body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err %v, want %q", body, err, want)
		}
	}
}
