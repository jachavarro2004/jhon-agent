package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeScript is a jhon that logs each call (its niceness, its folder, its
// words, a hook's payload) and refuses to run beside another of its calls:
// a second call while one runs logs OVERLAP. It has no `risk`. Its status
// writes a file into .git/objects in a project holding .grow.
const fakeScript = `#!/bin/sh
log="$PHASEGATE_FAKE_LOG"
if ! mkdir "$log.lock" 2>/dev/null; then echo "OVERLAP $*" >> "$log"; exit 97; fi
trap 'rmdir "$log.lock"' EXIT
echo "CALL nice=$(nice) sp=$JHON_SANDBOX_PROJECT $*" >> "$log"
case "$1" in
version) echo '{"version":"9.9.9","commit":"abc123","date":"2026-11-27"}' ;;
status) if [ -e .grow ]; then : > ".git/objects/grown-$$"; fi; sleep 0.02; echo '{}' ;;
hook) printf 'PAYLOAD ' >> "$log"; cat >> "$log"; echo >> "$log"; sleep 0.02 ;;
sandbox)
  case "$2" in
  status) echo '{"can_run":true}' ;;
  run) shift 3; "$@" ;;
  esac ;;
*) echo "jhon: unknown command: $1" >&2; exit 1 ;;
esac
`

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@invalid", "-c", "commit.gpgsign=false"}, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func TestMeasureRunsOneAtATimeAndFailsAProjectWhoseObjectsGrew(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "home"))
	log := filepath.Join(dir, "fake.log")
	t.Setenv("PHASEGATE_FAKE_LOG", log)
	t.Setenv("JHON_SANDBOX_PROJECT", "/elsewhere")
	fake := filepath.Join(dir, "bin", "jhon")
	writeFile(t, fake, fakeScript, 0o755)
	root := filepath.Join(dir, "root")
	writeFile(t, filepath.Join(root, "prose", "contracts", "vocabulary.yaml"), "vcs_dirs: [.git, .hg]\n", 0o644)
	writeFile(t, filepath.Join(root, "prose", "agents.yaml"), "agents:\n  - name: jhon-scout\n    description: x\n  - name: jhon-builder\n    bounded: true\n  - name: jhon-orchestrator\n", 0o644)
	var projects []string
	for _, name := range []string{"clean", "grows"} {
		p := filepath.Join(dir, name)
		writeFile(t, filepath.Join(p, ".editorconfig"), "root = true\n", 0o644)
		writeFile(t, filepath.Join(p, "app.txt"), "hi\n", 0o644)
		gitIn(t, p, "init", "-q", "-b", "main")
		gitIn(t, p, "add", "-A")
		gitIn(t, p, "commit", "-q", "-m", "base")
		projects = append(projects, p)
	}
	writeFile(t, filepath.Join(projects[1], ".grow"), "", 0o644)
	pf := filepath.Join(dir, "projects.txt")
	writeFile(t, pf, "# the owner's projects\n"+projects[0]+"\n\n"+projects[1]+"\n", 0o644)
	suiteTmp := filepath.Join(dir, "suite-tmp")
	writeFile(t, filepath.Join(suiteTmp, "left-1", "x"), "", 0o644)
	writeFile(t, filepath.Join(suiteTmp, "left-2"), "", 0o644)
	out := filepath.Join(dir, "out")
	empty := filepath.Join(dir, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"measure", "--label", "t1", "--jhon", fake, "--projects-file", pf, "--out", out, "--repo", empty,
		"--jhon-root", root, "--pi-sessions", empty, "--claude-projects", empty, "--suite-tmp", suiteTmp,
		"--result", "go-suite=pass", "--result", "doctor=fail"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (a project's objects grew); stderr %s\nstdout %s", code, stderr.String(), stdout.String())
	}
	raw, err := os.ReadFile(filepath.Join(out, "t1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}

	// One at a time, each under nice -n 10.
	logText, _ := os.ReadFile(log)
	if strings.Contains(string(logText), "OVERLAP") {
		t.Errorf("two calls ran at once:\n%s", logText)
	}
	calls := regexp.MustCompile(`(?m)^CALL nice=(\d+) `).FindAllStringSubmatch(string(logText), -1)
	// version, then per project 5 status, 1 risk (unknown), 3x5 hooks, 1 sandbox status, 5 sandbox runs.
	if len(calls) != 1+2*(5+1+15+1+5) {
		t.Errorf("%d calls, want %d:\n%s", len(calls), 1+2*27, logText)
	}
	for _, c := range calls {
		if n, _ := strconv.Atoi(c[1]); n < 10 {
			t.Errorf("a call ran at niceness %d, want 10 or more", n)
		}
	}

	// The hook calls: the main agent's guards, three payloads.
	for _, want := range []string{
		// Pinned to the project, as `jhon start` pins a session's sandbox.
		" sp=" + projects[0] + " hook pre-tool --agent jhon-orchestrator safety-guard vcs-guard .git .hg framework-guard ",
		" sp=" + projects[1] + " status --json\n", " sp=" + projects[1] + " sandbox run -- true\n",
		" secrets-guard route-guard jhon-builder\n",
		`"tool_name":"Bash"`, `"tool_input":{"command":"git status"}`, `"tool_input":{"command":"gh pr create --fill"}`,
		`"tool_name":"Edit"`, `"file_path":"` + filepath.Join(projects[0], "app.txt") + `"`,
		`"cwd":"` + projects[0] + `"`, `"hook_event_name":"PreToolUse"`,
	} {
		if !strings.Contains(string(logText), want) {
			t.Errorf("the log lacks %q:\n%s", want, logText)
		}
	}

	if r.Label != "t1" || r.Jhon.Version != "9.9.9" || r.Jhon.Commit != "abc123" {
		t.Errorf("label %q, jhon %+v", r.Label, r.Jhon)
	}
	if r.Speed == nil || r.Speed.Runs != 5 || len(r.Speed.Projects) != 2 {
		t.Fatalf("speed %+v", r.Speed)
	}
	for i, p := range r.Speed.Projects {
		if p.Index != i+1 {
			t.Errorf("project %d has index %d", i, p.Index)
		}
		for _, name := range []string{"status", "hook-git-status", "hook-edit", "hook-gh-pr-create", "sandbox-run"} {
			tm, ok := p.Commands[name]
			if !ok || tm.Runs != 5 || tm.MedianMS <= 0 || tm.MaxMS < tm.MedianMS || tm.ExitCodes["0"] != 5 || tm.Timeouts != 0 {
				t.Errorf("p%d %s = %+v, %v", p.Index, name, tm, ok)
			}
		}
		if _, timed := p.Commands["risk"]; timed || p.Skipped["risk"] != "the binary has no risk" {
			t.Errorf("p%d risk: timed %v, skipped %q", p.Index, timed, p.Skipped["risk"])
		}
	}
	clean, grows := r.Speed.Projects[0], r.Speed.Projects[1]
	if clean.ObjectsGrew || clean.Before != clean.After || clean.Before.Project == 0 {
		t.Errorf("clean project: grew %v, before %+v, after %+v", clean.ObjectsGrew, clean.Before, clean.After)
	}
	if !grows.ObjectsGrew || grows.After.Project != grows.Before.Project+5 {
		t.Errorf("growing project: grew %v, before %+v, after %+v", grows.ObjectsGrew, grows.Before, grows.After)
	}
	if !strings.Contains(stdout.String(), "p2: objects grew") {
		t.Errorf("stdout does not say p2's objects grew:\n%s", stdout.String())
	}

	if r.Budgets != nil || r.Skipped["budgets"] == "" {
		t.Errorf("budgets %v, skipped %q; want skipped in a folder with no renderer", r.Budgets, r.Skipped["budgets"])
	}
	if r.Friction == nil || r.Friction.ToolResults != 0 || r.Friction.Since != "all" {
		t.Errorf("friction %+v", r.Friction)
	}
	if r.Hygiene == nil || r.Hygiene.SuiteTmpLeftovers != 2 {
		t.Errorf("hygiene %+v", r.Hygiene)
	}
	if r.Results["go-suite"] != "pass" || r.Results["doctor"] != "fail" || len(r.Results) != 2 {
		t.Errorf("results %v", r.Results)
	}
	// Privacy: no path, no project name, no command text.
	for _, bad := range []string{dir, "clean", "grows", "app.txt", "git status", "gh pr"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("the record holds %q:\n%s", bad, raw)
		}
	}
}

func TestMeasureRejectsABadResultAndALabelWithAPath(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"measure", "--label", "a/b", "--jhon", "/bin/true", "--out", dir},
		{"measure", "--label", "x", "--jhon", "/bin/true", "--out", dir, "--result", "go-suite=maybe"},
		{"measure", "--jhon", "/bin/true", "--out", dir},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a refused measure wrote %d file(s)", len(entries))
	}
}

func TestTimedKillsACallAtItsTimeout(t *testing.T) {
	start := time.Now()
	r := timed(t.TempDir(), os.Environ(), nil, 300*time.Millisecond, "sh", "-c", "sleep 5; :")
	if !r.timedOut || time.Since(start) > 3*time.Second {
		t.Errorf("timed out %v after %v", r.timedOut, time.Since(start))
	}
	r = timed(t.TempDir(), os.Environ(), []byte("in"), 5*time.Second, "sh", "-c", "cat >/dev/null; echo no >&2; exit 3")
	if r.timedOut || r.code != 3 || r.stderr != "no\n" || r.elapsed <= 0 {
		t.Errorf("result %+v", r)
	}
}
