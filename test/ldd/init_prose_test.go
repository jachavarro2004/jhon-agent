package ldd

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T64 (L506): a session asked for LDD sets it up by reading init.md, with
// no Skill tool. Followed with only Bash, Read and Write in a marker-less
// Python repository, the files and commands init.md gives make LDD active
// under ldd/ and let `jhon executions` run the tests; it asks for no skill
// and no command the guards refuse an agent.
func TestInitStepsByReadingMakeLDDActive(t *testing.T) {
	t.Parallel()
	steps := support.Read(t, support.Path("prose", "skills", "ldd", "references", "init.md"))
	// LDD is always on (T72, L555, L559): no switch to run; one batched
	// question on delivery and testing, the recommended values when no one
	// can answer, and the set-up told in one line.
	if strings.Contains(steps, "jhon ldd on") || strings.Contains(steps, "jhon ldd off") {
		t.Errorf("init.md still names the retired switch")
	}
	for _, want := range []string{"tell the owner in one line", "ask the owner one batched question", "`delivery: change`", "`strict_tdd_policy: require` (recommended)", "(recommended; no `delivery` key)",
		// Only through an ask tool; never a prose question (T74, L572).
		"Ask through your ask tool only (AskUserQuestion in Claude Code, `ask_user` in pi)",
		"With no ask tool, or no answer, do not ask in a message", "go on with the task in the same turn",
		// T104 (L682): set-up is followed by the record and a recorded run.
		"before you finish the task, start its change (tasks.md) and record a run (`jhon executions run`)"} {
		if !strings.Contains(steps, want) {
			t.Errorf("init.md lacks %q", want)
		}
	}
	if strings.Contains(steps, "Skill tool") || regexp.MustCompile("`/jhon-ldd (new|ff|continue|apply)").MatchString(steps) {
		t.Errorf("init.md sends the reader to a skill")
	}
	block := func(lang string) string {
		m := regexp.MustCompile("(?s)```" + lang + "\n(.*?)\n *```").FindStringSubmatch(steps)
		if m == nil {
			t.Fatalf("init.md has no %s block", lang)
		}
		var lines []string
		for _, l := range strings.Split(m[1], "\n") {
			lines = append(lines, strings.TrimPrefix(l, "   "))
		}
		return strings.Join(lines, "\n") + "\n"
	}
	root := support.GitRepo(t, temp(t))
	support.Write(t, root, map[string]string{
		"accounts.py":      "def load():\n    return []\n",
		"test_accounts.py": "import unittest\nimport accounts\n\n\nclass T(unittest.TestCase):\n    def test_load(self):\n        self.assertIsInstance(accounts.load(), list)\n",
	})
	commit(t, root, "base")
	// Step 1, read-only.
	equal(t, rootProject(t, root)["testability"], "unknown")
	// Step 2: the layout's directories, by init.md's own command.
	mk := exec.Command("sh", "-c", block("sh"))
	mk.Dir = root
	if out, err := mk.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	// Step 3: the tests' command runs once, then the config init.md gives.
	run := exec.Command("python3", "-m", "unittest")
	run.Dir = root
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	support.Write(t, root, map[string]string{"ldd/config.yaml": block("yaml")})
	for _, dir := range []string{"ldd/specs", "ldd/changes/archive"} {
		equal(t, exists(filepath.Join(root, dir)), true, dir)
	}
	equal(t, exists(filepath.Join(root, "openspec")), false, "init.md created openspec/")
	// Step 5.
	r := jhonGit(t, root, "status", "--json")
	equal(t, r.Code, 0, r.Stderr)
	s := object(t, r.Stdout)
	equal(t, s["ldd"].(map[string]any)["active"], true)
	notes, _ := s["notes"].([]any)
	if len(notes) == 0 || !strings.Contains(notes[0].(string), "ldd/changes/") {
		t.Errorf("status does not read the ldd/ layout: %v", notes)
	}
	projects, _ := s["projects"].([]any)
	if len(projects) != 1 || projects[0].(map[string]any)["command"] != "python3 -m unittest" {
		t.Fatalf("projects %v", projects)
	}
	support.Git(t, root, "checkout", "-q", "-b", "fix/rows")
	support.Write(t, root, map[string]string{"accounts.py": "def load():\n    return [1]\n"})
	dry := jhonGit(t, root, "executions", "run", "fix-rows", "--dry-run")
	equal(t, dry.Code, 0, dry.Stderr)
	match(t, dry.Stdout, `python3 -m unittest`)
}

// L548 (the owner's rule): asking for LDD sets up its layout and nothing
// else; init.md builds no skill registry, so a setup run writes no
// `.atl/` file the task did not ask for.
func TestInitWritesNoSkillRegistry(t *testing.T) {
	t.Parallel()
	steps := support.Read(t, support.Path("prose", "skills", "ldd", "references", "init.md"))
	for _, s := range []string{".atl/", "skill-registry"} {
		if strings.Contains(steps, s) {
			t.Errorf("init.md still names %q", s)
		}
	}
	if !regexp.MustCompile("(?m)^4\\. Run `jhon status --json`").MatchString(steps) {
		t.Errorf("init.md's steps are not renumbered: status is not step 4")
	}
}
