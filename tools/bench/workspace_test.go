package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// workspaceIDs are the multi-package tasks (T192, S6): a TypeScript pnpm
// workspace like the owner's finanzas, where a non-small change spans
// several packages and its acceptance has tests in each.
var workspaceIDs = []string{"workspace-budgets"}

// workspacePackages are workspace-budgets' packages, each with its own tests.
var workspacePackages = []string{"packages/core", "packages/api", "apps/cli"}

// nodeTests runs `node --test` in dir and fails t unless it passes.
func nodeTests(t *testing.T, dir string) string {
	t.Helper()
	c := exec.Command("node", "--test")
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test fails in %s: %v\n%s", dir, err, out)
	}
	return string(out)
}

func TestTheWorkspaceTaskLoads(t *testing.T) {
	tasks, err := LoadTasks("tasks", workspaceIDs)
	if err != nil {
		t.Fatal(err)
	}
	task := tasks[0]
	if task.Kind != "long" || task.Size != SizeLarge || task.MaxTurns != 90 || task.Timeout != 45*time.Minute || task.MultiMessage() {
		t.Errorf("kind %q size %q max_turns %d timeout %s messages %d", task.Kind, task.Size, task.MaxTurns, task.Timeout, len(task.Prompts))
	}
	if task.Tests != "*.test.ts" || task.TestCommand != "node --test" || task.LDDConfig != filepath.Join("tasks", "workspace-budgets", "ldd-config.yaml") {
		t.Errorf("tests %q test_command %q ldd config %q", task.Tests, task.TestCommand, task.LDDConfig)
	}
	for _, pkg := range workspacePackages {
		hidden := pkg + "/test/budget.acceptance.test.ts"
		if !strings.Contains(task.Check, hidden) || !fileExists(filepath.Join(task.HiddenDir(), hidden)) {
			t.Errorf("the check %q does not run %s's hidden acceptance", task.Check, pkg)
		}
		if fileExists(filepath.Join(task.Template(), hidden)) {
			t.Errorf("%s's hidden test is visible in the template", pkg)
		}
		if !fileExists(filepath.Join(task.Template(), pkg, "package.json")) || !task.InScope(pkg+"/src/budget.ts") {
			t.Errorf("%s: no package.json, or its sources are out of scope %q", pkg, task.Scope)
		}
		if !strings.Contains(task.Prompt, pkg) {
			t.Errorf("the prompt does not name %s", pkg)
		}
	}
	for _, f := range []string{"pnpm-workspace.yaml", "package.json", "docs/LEDGER.md"} {
		if !fileExists(filepath.Join(task.Template(), f)) {
			t.Errorf("the template has no %s", f)
		}
	}
	words := strings.FieldsFunc(strings.ToLower(task.Prompt), func(r rune) bool { return !('a' <= r && r <= 'z' || r == '-' || r == '_') })
	for _, w := range toolWords {
		if slices.Contains(words, w) || slices.Contains(words, w+"s") {
			t.Errorf("the prompt names %q", w)
		}
	}
	if strings.Contains(task.Prompt, "acceptance") {
		t.Error("the prompt names a hidden test")
	}
	if !task.IsTest("packages/core/test/budget.test.ts") || task.IsTest("packages/core/src/budget.ts") || task.InScope("packages/core/package.json") {
		t.Error("tests and scope do not tell a test from a source or a manifest")
	}
}

// TestTheWorkspaceTaskFailsOnTheTemplateAndPassesOnTheReference is the
// proof workspace-budgets is a task: every package's own tests pass on the
// template, the hidden acceptance fails there (RED) and passes on the
// reference in testdata/solutions (GREEN), whose own tests pass too; the
// reference adds a test in each package and writes nothing out of scope.
func TestTheWorkspaceTaskFailsOnTheTemplateAndPassesOnTheReference(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("the task needs node")
	}
	task, err := LoadTask(filepath.Join("tasks", "workspace-budgets"))
	if err != nil {
		t.Fatal(err)
	}
	untouched := t.TempDir()
	if err := copyTree(task.Template(), untouched); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range workspacePackages {
		nodeTests(t, filepath.Join(untouched, pkg))
	}
	if ok, out, err := task.Accept(untouched, os.Environ()); err != nil || ok {
		t.Fatalf("RED: the hidden acceptance passes on the untouched template (%v):\n%s", err, out)
	}
	solution := filepath.Join("testdata", "solutions", task.ID)
	added := map[string]bool{}
	if err := filepath.WalkDir(solution, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(solution, p)
		rel = filepath.ToSlash(rel)
		if !task.InScope(rel) && !task.IsTest(rel) {
			t.Errorf("the reference writes %s, outside the scope %q", rel, task.Scope)
		}
		for _, pkg := range workspacePackages {
			if task.IsTest(rel) && strings.HasPrefix(rel, pkg+"/") && !fileExists(filepath.Join(task.Template(), rel)) {
				added[pkg] = true
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range workspacePackages {
		if !added[pkg] {
			t.Errorf("the reference adds no test in %s", pkg)
		}
	}
	solved := t.TempDir()
	for _, src := range []string{task.Template(), solution} {
		if err := copyTree(src, solved); err != nil {
			t.Fatal(err)
		}
	}
	for _, pkg := range workspacePackages {
		nodeTests(t, filepath.Join(solved, pkg))
	}
	ok, out, err := task.Accept(solved, os.Environ())
	if err != nil || !ok {
		t.Fatalf("GREEN: the hidden acceptance fails on the reference (%v):\n%s", err, out)
	}
	nodeTests(t, solved)
}

// A task with its own LDD config (Task.LDDConfig) starts every Jhon variant
// and the steady state on it, not on the bench's Python config, and gets no
// pyproject.toml: in workspace-budgets' steady copy `jhon status --json`
// reports Strict TDD on with evidence ok from ldd/config.yaml and every
// package testable with npm test.
func TestTheWorkspaceTaskStartsOnItsOwnLDDConfig(t *testing.T) {
	task, err := LoadTask(filepath.Join("tasks", "workspace-budgets"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(task.LDDConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(want), "strict_tdd_policy: require") || strings.Contains(string(want), "python") {
		t.Fatalf("the task's config:\n%s", want)
	}
	for variant, config := range map[string]string{"plain": "ldd/config.yaml", "ldd": "openspec/config.yaml", "ldd-native": "ldd/config.yaml"} {
		repo := filepath.Join(t.TempDir(), "repo")
		if err := prepareRepo(task, variant, repo); err != nil {
			t.Fatal(err)
		}
		if variant == "plain" {
			if fileExists(filepath.Join(repo, "ldd")) {
				t.Errorf("the plain copy has ldd/ before the steady set-up")
			}
			if err := writeSteadySetup(task, repo); err != nil {
				t.Fatal(err)
			}
		}
		got, err := os.ReadFile(filepath.Join(repo, config))
		if err != nil || string(got) != string(want) {
			t.Errorf("%s: %s is not the task's config (%v):\n%s", variant, config, err, got)
		}
		if fileExists(filepath.Join(repo, "pyproject.toml")) {
			t.Errorf("%s: the copy has a pyproject.toml", variant)
		}
		if variant != "plain" {
			continue
		}
		if err := gitRepo(repo, append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")); err != nil {
			t.Fatal(err)
		}
		s := status(t, repo)
		tdd, _ := s["strict_tdd"].(map[string]any)
		if tdd["value"] != true || tdd["evidence"] != "ok" || !strings.Contains(str(tdd["reason"]), "ldd/config.yaml") {
			t.Errorf("steady copy: strict_tdd %v", tdd)
		}
		testable := map[string]string{}
		for _, p := range list(s["projects"]) {
			if str(at(p, "testability")) == "testable" {
				testable[str(at(p, "path"))] = str(at(p, "command"))
			}
		}
		for _, pkg := range workspacePackages {
			if testable[pkg] != "npm test" {
				t.Errorf("steady copy: %s is not testable with npm test: %v", pkg, testable)
			}
		}
	}
	// A Python task keeps the bench's config and its pyproject.toml.
	py, err := LoadTask(filepath.Join("tasks", "two-modules"))
	if err != nil || py.LDDConfig != "" {
		t.Fatalf("two-modules: ldd config %q %v", py.LDDConfig, err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := prepareRepo(py, "ldd-native", repo); err != nil {
		t.Fatal(err)
	}
	bench, _ := os.ReadFile(filepath.Join("ldd", "config.yaml"))
	if got, _ := os.ReadFile(filepath.Join(repo, "ldd", "config.yaml")); string(got) != string(bench) || !fileExists(filepath.Join(repo, "pyproject.toml")) {
		t.Errorf("two-modules' ldd-native copy is not the bench's layout")
	}
}

// The audit says per run which sub-agents the session launched and whether
// it orchestrated: a verify sub-agent launched and an independent verify
// recorded, its verdict (pass or fail) read by the audit or on the change's
// execution record (ldd_layout.light_verify, `jhon executions verify`).
// metrics.json carries launches, verify_recorded and orchestrated.
func TestTheAuditSaysWhetherTheSessionOrchestrated(t *testing.T) {
	cases := []struct {
		name     string
		changes  string
		light    string // the record's light verify verdict
		launches map[string]int
		recorded int
		want     bool
	}{
		{"scout, builder and a passing checker", `[{"agents":3,"verify_runs":1,"launches":{"jhon-scout":1,"jhon-builder":1,"jhon-checker":1},"verify_verdicts":{"pass":1,"fail":0,"unknown":0}},
			{"agents":2,"verify_runs":1,"launches":{"jhon-builder":1,"jhon-checker":1},"verify_verdicts":{"pass":0,"fail":1,"unknown":0}}]`,
			"", map[string]int{"jhon-scout": 1, "jhon-builder": 2, "jhon-checker": 2}, 2, true},
		{"a checker the audit read no verdict of, none on record", `[{"agents":2,"verify_runs":1,"launches":{"jhon-builder":1,"jhon-checker":1},"verify_verdicts":{"pass":0,"fail":0,"unknown":1}}]`,
			"", map[string]int{"jhon-builder": 1, "jhon-checker": 1}, 0, false},
		{"a checker whose verdict is on the execution record", `[{"agents":1,"verify_runs":1,"launches":{"jhon-checker":1},"verify_verdicts":{"pass":0,"fail":0,"unknown":1}}]`,
			"pass", map[string]int{"jhon-checker": 1}, 0, true},
		{"a scout but no checker, a verdict the main thread recorded", `[{"agents":1,"verify_runs":0,"launches":{"jhon-scout":1},"verify_verdicts":{"pass":0,"fail":0,"unknown":0}}]`,
			"pass", map[string]int{"jhon-scout": 1}, 0, false},
		{"no sub-agent", `[{"agents":0,"verify_runs":0,"launches":{},"verify_verdicts":{"pass":0,"fail":0,"unknown":0}}]`, "", nil, 0, false},
		{"no change", `[]`, "", nil, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Metrics{LddLayout: &LddLayout{LightVerify: c.light}}
			if err := m.fromAudit([]byte(`{"outcomes":{"session":{},"changes":` + c.changes + `}}`)); err != nil {
				t.Fatal(err)
			}
			if m.Orchestrated == nil || *m.Orchestrated != c.want || m.VerifyRecorded != c.recorded || len(m.Launches) != len(c.launches) {
				t.Fatalf("orchestrated %v verify_recorded %d launches %v", m.Orchestrated, m.VerifyRecorded, m.Launches)
			}
			for agent, n := range c.launches {
				if m.Launches[agent] != n {
					t.Errorf("launches %v, want %v", m.Launches, c.launches)
				}
			}
			b, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var back map[string]any
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatal(err)
			}
			if back["orchestrated"] != c.want || back["verify_recorded"] != float64(c.recorded) || (c.launches != nil) != (back["launches"] != nil) {
				t.Errorf("metrics.json: orchestrated %v verify_recorded %v launches %v", back["orchestrated"], back["verify_recorded"], back["launches"])
			}
		})
	}
	// A run with no audit has no answer.
	var none Metrics
	if b, _ := json.Marshal(none); !strings.Contains(string(b), `"orchestrated":null`) {
		t.Errorf("no audit: %s", b)
	}
}

// ldd_layout.light_verify is the verdict of the change's latest light verify
// in `jhon status --json` (tests.light_verify, one record or a list).
func TestTheLDDLayoutCarriesTheRecordsLightVerify(t *testing.T) {
	repo := t.TempDir()
	for status, want := range map[string]string{
		`{"tests":{"light_verify":{"verdict":"pass","commit":"84225d4"}}}`:   "pass",
		`{"tests":{"light_verify":[{"verdict":"pass"},{"verdict":"fail"}]}}`: "fail",
		`{"tests":{"passed":true}}`:                                          "",
		`{"tests":null}`:                                                     "",
	} {
		if got := lddLayoutOf(repo, []byte(status)).LightVerify; got != want {
			t.Errorf("%s: light_verify %q, want %q", status, got, want)
		}
	}
}
