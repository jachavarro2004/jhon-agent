package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Task is one benchmark task: tasks/<id>/task.yaml, the repository template
// the agent works in (tasks/<id>/template) and the hidden acceptance test
// (tasks/<id>/<hidden>), copied into the repository only after the run and
// judged by Check (exit 0 is success).
type Task struct {
	ID   string
	Kind string
	// Size is small or large (T146, L1205): it picks the task's `bench
	// value` gates (valueGates) and its default repeats (DefaultRepeats).
	Size string
	// Prompt is the first owner message; Prompts are all of them, in order:
	// task.yaml's `prompt:` alone, or `prompt_1:`, `prompt_2:` … with no gap.
	Prompt  string
	Prompts []string
	// MessageChecks are the commands `check_<n>:` names, one per message (""
	// when none); nil when the task names none. Each runs, after its message
	// and every later one settles, on a copy of the repository with the
	// hidden directory in it, so a later message's regression shows.
	MessageChecks []string
	Model         string
	MaxTurns      int
	Timeout       time.Duration
	Hidden        string
	Check         string
	Dir           string
	// Scope are the files a correct solution may touch, as path.Match
	// patterns relative to the repository; tests are always allowed (T41).
	Scope []string
	// Tests is the test files' pattern, matched against each file's base
	// name: test_*.py when task.yaml names none.
	Tests string
	// TestCommand is the project's test command, as the transcript shows it
	// run: python3 -m unittest when task.yaml names none.
	TestCommand string
}

// InScope is whether the repository path file matches one of t's Scope.
func (t Task) InScope(file string) bool {
	for _, p := range t.Scope {
		if ok, _ := path.Match(p, filepath.ToSlash(file)); ok {
			return true
		}
	}
	return false
}

// IsTest is whether the repository path file is a test file by t's pattern.
func (t Task) IsTest(file string) bool {
	ok, _ := path.Match(t.Tests, path.Base(filepath.ToSlash(file)))
	return ok
}

// MultiMessage is whether t sends more than one owner message.
func (t Task) MultiMessage() bool { return len(t.Prompts) > 1 }

// numbered are the values of key_1, key_2 … in f, by number, and the
// highest number; an error names a key_<x> that is not a positive number.
func numbered(f map[string]string, key string) (map[int]string, int, error) {
	out, top := map[int]string{}, 0
	for k, v := range f {
		digits, ok := strings.CutPrefix(k, key+"_")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(digits)
		if err != nil || n < 1 || strconv.Itoa(n) != digits {
			return nil, 0, fmt.Errorf("%s is not %s_<n>", k, key)
		}
		out[n], top = v, max(top, n)
	}
	return out, top, nil
}

// messages fills t's Prompts and MessageChecks from f.
func (t *Task) messages(f map[string]string) error {
	prompts, n, err := numbered(f, "prompt")
	if err != nil {
		return err
	}
	checks, top, err := numbered(f, "check")
	if err != nil {
		return err
	}
	if n == 0 {
		if top > 0 {
			return fmt.Errorf("check_%d has no prompt_%d", top, top)
		}
		t.Prompts = []string{t.Prompt}
		return nil
	}
	if _, plain := f["prompt"]; plain {
		return fmt.Errorf("task.yaml has both prompt and prompt_1")
	}
	for i := 1; i <= n; i++ {
		p, ok := prompts[i]
		if !ok {
			return fmt.Errorf("prompt_%d is missing", i)
		}
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("prompt_%d is empty", i)
		}
		t.Prompts = append(t.Prompts, p)
	}
	if top > n {
		return fmt.Errorf("check_%d has no prompt_%d", top, top)
	}
	if top > 0 {
		t.MessageChecks = make([]string, n)
		for i, c := range checks {
			t.MessageChecks[i-1] = c
		}
	}
	t.Prompt = t.Prompts[0]
	return nil
}

// Template is the directory the agent's repository is copied from.
func (t Task) Template() string { return filepath.Join(t.Dir, "template") }

// HiddenDir is the directory of the hidden acceptance test.
func (t Task) HiddenDir() string { return filepath.Join(t.Dir, t.Hidden) }

// parseFlatYAML reads the flat `key: value` subset task.yaml uses, with
// `key: |` and indented lines for a block of text. Comments and blank
// lines between keys are skipped.
func parseFlatYAML(text string) (map[string]string, error) {
	out := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			return nil, fmt.Errorf("line %d: unexpected indentation", i+1)
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("line %d: not key: value", i+1)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("line %d: %s repeats", i+1, key)
		}
		if value != "|" {
			out[key] = strings.Trim(value, `"`)
			continue
		}
		var block []string
		for i+1 < len(lines) && (strings.TrimSpace(lines[i+1]) == "" || strings.HasPrefix(lines[i+1], "  ")) {
			i++
			block = append(block, strings.TrimPrefix(lines[i], "  "))
		}
		out[key] = strings.TrimRight(strings.Join(block, "\n"), "\n") + "\n"
	}
	return out, nil
}

// LoadTask reads tasks/<id>/task.yaml under dir.
func LoadTask(dir string) (Task, error) {
	b, err := os.ReadFile(filepath.Join(dir, "task.yaml"))
	if err != nil {
		return Task{}, err
	}
	f, err := parseFlatYAML(string(b))
	if err != nil {
		return Task{}, fmt.Errorf("%s: %v", filepath.Join(dir, "task.yaml"), err)
	}
	t := Task{ID: f["id"], Kind: f["kind"], Size: f["size"], Prompt: f["prompt"], Model: f["model"], Hidden: f["hidden"], Check: f["check"], Dir: dir}
	if t.Size != "" && t.Size != SizeSmall && t.Size != SizeLarge {
		return Task{}, fmt.Errorf("%s: size %q: give %s or %s", dir, t.Size, SizeSmall, SizeLarge)
	}
	if err := t.messages(f); err != nil {
		return Task{}, fmt.Errorf("%s: %v", dir, err)
	}
	if t.Hidden == "" {
		t.Hidden = "hidden"
	}
	t.Tests, t.TestCommand = cmpOr(f["tests"], "test_*.py"), cmpOr(f["test_command"], "python3 -m unittest")
	for _, line := range strings.Split(f["scope"], "\n") {
		if line = strings.TrimSpace(line); line != "" {
			t.Scope = append(t.Scope, line)
		}
	}
	if t.MaxTurns, err = strconv.Atoi(f["max_turns"]); err != nil || t.MaxTurns < 1 {
		return Task{}, fmt.Errorf("%s: max_turns must be a positive integer", dir)
	}
	if t.Timeout, err = time.ParseDuration(f["timeout"]); err != nil || t.Timeout <= 0 {
		return Task{}, fmt.Errorf("%s: timeout must be a duration such as 30m", dir)
	}
	for k, v := range map[string]string{"id": t.ID, "kind": t.Kind, "prompt": t.Prompt, "model": t.Model, "check": t.Check} {
		if strings.TrimSpace(v) == "" {
			return Task{}, fmt.Errorf("%s: task.yaml has no %s", dir, k)
		}
	}
	if t.ID != filepath.Base(dir) {
		return Task{}, fmt.Errorf("%s: id %q is not the directory's name", dir, t.ID)
	}
	return t, nil
}

// The task sizes (T146, L1205).
const (
	SizeSmall = "small"
	SizeLarge = "large"
)

// DefaultRepeats are a task's runs per condition when --repeats is not
// given (T146 FB5): 5 for a small task (cheap, so the spread shows), 3 for a
// large one or one with no size (MinRepeats, L483), 1 for the one-time
// set-up run.
func (t Task) DefaultRepeats() int {
	switch {
	case t.ID == SetupTaskID:
		return 1
	case t.Size == SizeSmall:
		return 5
	}
	return MinRepeats
}

// SetupTaskID is the one-time LDD set-up task (T146 FB2): a tiny repo with
// no LDD that a Jhon condition sets up, measured apart from every cell. It
// lives in setup/, not tasks/, and runs once per Jhon runtime.
const SetupTaskID = "ldd-setup"

// setupTaskDir is the set-up task's directory.
func setupTaskDir() string { return filepath.Join(source(), "setup", SetupTaskID) }

// LoadTasks reads the named tasks under root, or every task when ids is
// empty; the one-time set-up task (SetupTaskID) loads from setup/ when named.
func LoadTasks(root string, ids []string) ([]Task, error) {
	if len(ids) == 0 {
		var err error
		if ids, err = listTaskIDs(root); err != nil {
			return nil, err
		}
	}
	var out []Task
	for _, id := range ids {
		dir := filepath.Join(root, id)
		if id == SetupTaskID {
			dir = setupTaskDir()
		}
		t, err := LoadTask(dir)
		if err != nil {
			return nil, fmt.Errorf("task %s: %v", id, err)
		}
		out = append(out, t)
	}
	return out, nil
}

// listTaskIDs are the ids of every task under root, sorted.
func listTaskIDs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// taskSizes are the sizes of the tasks under root by id, the set-up task's
// too; a task that does not load is left out (`bench value` reads results
// whose metrics.json predates task_size).
func taskSizes(root string) map[string]string {
	sizes := map[string]string{}
	entries, _ := os.ReadDir(root)
	dirs := []string{setupTaskDir()}
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	for _, d := range dirs {
		if t, err := LoadTask(d); err == nil && t.Size != "" {
			sizes[t.ID] = t.Size
		}
	}
	return sizes
}

// checkTimeout bounds the hidden acceptance test.
const checkTimeout = 5 * time.Minute

// Accept copies the hidden acceptance test into repo and runs the task's
// check there: true on exit 0, with the check's combined output.
func (t Task) Accept(repo string, env []string) (bool, string, error) {
	if err := copyTree(t.HiddenDir(), repo); err != nil {
		return false, "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", t.Check)
	c.Dir, c.Env = repo, append(slices.Clone(env), "PYTHONDONTWRITEBYTECODE=1")
	out, err := c.CombinedOutput()
	if _, exit := err.(*exec.ExitError); err != nil && !exit {
		return false, string(out), err
	}
	return err == nil, string(out), nil
}

// copyTree copies the regular files under src into dst, keeping their modes.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if d.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() || strings.HasSuffix(p, ".pyc") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode().Perm())
	})
}
