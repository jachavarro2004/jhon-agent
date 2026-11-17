package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Recheck recomputes, without a model call, the hidden check and the
// tree-dependent facts (tree.go) of every finished run under results, in
// place: each run's old metrics.json and check.txt are kept beside as
// metrics.json.before-recheck and check.txt.before-recheck (the first
// backup is never overwritten). A run's worktrees are repaired first
// (`git worktree repair`), so a copied results directory reads its own
// worktrees; a run whose worktree lies outside its directory is skipped.
func Recheck(results, home, tasksDir string, out io.Writer) error {
	dirs, err := filepath.Glob(filepath.Join(results, "runs", "*", "metrics.json"))
	if err != nil {
		return err
	}
	if len(dirs) == 0 {
		return fmt.Errorf("no runs with a metrics.json under %s", results)
	}
	o := &Options{Home: home}
	for _, f := range dirs {
		dir := filepath.Dir(f)
		before, after, err := recheckRun(o, dir, tasksDir)
		if err != nil {
			fmt.Fprintf(out, "%s: skipped: %v\n", filepath.Base(dir), err)
			continue
		}
		fmt.Fprintf(out, "%s: success %t -> %t, checked tree %s\n", filepath.Base(dir), before, after.Success, after.CheckedTree)
	}
	return nil
}

func recheckRun(o *Options, dir, tasksDir string) (bool, *Metrics, error) {
	b, err := os.ReadFile(filepath.Join(dir, "metrics.json"))
	if err != nil {
		return false, nil, err
	}
	old, m := &Metrics{}, &Metrics{}
	if err := json.Unmarshal(b, old); err != nil {
		return false, nil, err
	}
	_ = json.Unmarshal(b, m)
	tasks, err := LoadTasks(tasksDir, []string{m.Task})
	if err != nil {
		return false, nil, err
	}
	cond, err := conditionNamed(m.Condition)
	if err != nil {
		return false, nil, err
	}
	r := &Run{Task: tasks[0], Variant: m.Variant, Cond: cond, Dir: dir, Repo: filepath.Join(dir, "repo")}
	env := benchEnv(os.Environ(), o.Home, filepath.Join(dir, "cache"))
	var wts []string
	for _, pat := range []string{"*", "*/*", "*/*/*"} {
		found, _ := filepath.Glob(filepath.Join(dir, "repo-worktrees", pat, ".git"))
		for _, g := range found {
			wts = append(wts, filepath.Dir(g))
		}
	}
	if len(wts) > 0 {
		c := exec.Command("git", append([]string{"worktree", "repair"}, wts...)...)
		c.Dir, c.Env = r.Repo, env
		if out, err := c.CombinedOutput(); err != nil {
			return false, nil, fmt.Errorf("git worktree repair: %v %s", err, strings.TrimSpace(string(out)))
		}
	}
	listed, err := worktrees(r.Repo, env)
	if err != nil {
		return false, nil, err
	}
	for _, w := range listed {
		if rel, err := filepath.Rel(dir, w.path); err != nil || strings.HasPrefix(rel, "..") {
			return false, nil, fmt.Errorf("worktree %s lies outside the run", w.path)
		}
	}
	c := exec.Command("git", "rev-list", "--max-parents=0", "HEAD")
	c.Dir, c.Env = r.Repo, env
	root, err := c.Output()
	if err != nil {
		return false, nil, fmt.Errorf("the template commit: %v", err)
	}
	start := strings.Fields(string(root))[0]
	// The hidden test an earlier check copied in is not the agent's.
	ignore := func(f string) bool { return fileExists(filepath.Join(r.Task.HiddenDir(), filepath.FromSlash(f))) }
	m.Ceremony, m.LddLayout, m.RedBeforeGreen, m.RedBeforeGreenSource = nil, nil, nil, ""
	m.Problems = nil
	for _, p := range old.Problems {
		if !strings.HasPrefix(p, "hidden test: ") && !strings.HasPrefix(p, "delivered tree: ") && !strings.HasPrefix(p, "ceremony: ") {
			m.Problems = append(m.Problems, p)
		}
	}
	check := m.treeFacts(o, r, start, env, ignore)
	if t, err := os.ReadFile(m.Transcript); err == nil && m.Transcript != "" {
		m.fromTranscript(t, r.Task.TestCommand)
	} else {
		if m.RedBeforeGreen == nil {
			m.RedBeforeGreen, m.RedBeforeGreenSource = old.RedBeforeGreen, old.RedBeforeGreenSource
		}
		if m.Ceremony != nil && old.Ceremony != nil {
			m.Ceremony.Executions, m.Ceremony.Archive, m.Ceremony.Deliver = old.Ceremony.Executions, old.Ceremony.Archive, old.Ceremony.Deliver
		}
	}
	for _, f := range []string{"metrics.json", "check.txt"} {
		backup := filepath.Join(dir, f+".before-recheck")
		if _, err := os.Stat(backup); err == nil {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
			if err := os.WriteFile(backup, b, 0o644); err != nil {
				return false, nil, err
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "check.txt"), []byte(check), 0o644); err != nil {
		return false, nil, err
	}
	return old.Success, m, writeJSON(filepath.Join(dir, "metrics.json"), m)
}
