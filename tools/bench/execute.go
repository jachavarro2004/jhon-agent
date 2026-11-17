package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

func jsonIndent(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	return append(b, '\n'), err
}

// gitRepo makes dir a git repository with the template as its one commit.
// The repo has an identity of its own in its local config (never the global
// one), so the agent's commits work, and ignores Python's bytecode, as a real
// Python project does (L415, L418).
func gitRepo(dir string, env []string) error {
	ignore := filepath.Join(dir, ".gitignore")
	if b, err := os.ReadFile(ignore); err != nil || !bytes.Contains(b, []byte("__pycache__/")) {
		if len(b) > 0 && b[len(b)-1] != '\n' {
			b = append(b, '\n')
		}
		if err := os.WriteFile(ignore, append(b, "__pycache__/\n"...), 0o644); err != nil {
			return err
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.name", "bench"},
		{"config", "user.email", "bench@localhost"},
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-q", "--no-verify", "-m", "template"},
	} {
		c := exec.Command("git", args...)
		c.Dir, c.Env = dir, env
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v\n%s", args, err, out)
		}
	}
	return nil
}

// execute is one run: a fresh copy of the template, the agent, the hidden
// test, the transcript and its audit. A missing transcript or a failed audit
// is a problem the metrics carry, not an error that stops the batch.
func execute(o *Options, r *Run) (*Metrics, error) {
	if err := os.MkdirAll(r.Cache, 0o755); err != nil {
		return nil, err
	}
	if err := prepareRepo(r.Task, r.Variant, r.Repo); err != nil {
		return nil, err
	}
	if r.Steady {
		if err := writeSteadySetup(r.Repo); err != nil {
			return nil, err
		}
	}
	// Committed with the template, as the variant's config is (T86).
	if err := writeProjectSettings(r.Repo, r.Variant, r.ProjectSettings); err != nil {
		return nil, err
	}
	env := benchEnv(os.Environ(), o.Home, r.Cache)
	if err := gitRepo(r.Repo, env); err != nil {
		return nil, err
	}
	start, err := headCommit(r.Repo, env)
	if err != nil {
		return nil, err
	}
	if pc, ok := r.Cond.(piClean); ok {
		if err := pc.prepare(r, o); err != nil {
			return nil, err
		}
	}
	if slices.ContainsFunc(r.Env, func(kv string) bool { return strings.HasPrefix(kv, "CLAUDE_BRIDGE_DEBUG") }) || slices.Contains(r.Argv, "--debug-file") {
		if err := os.MkdirAll(r.promptDump(), 0o755); err != nil {
			return nil, err
		}
	}
	var checks []MessageCheck
	var checkErr error
	res, err := startAgent(r, append(append(slices.Clone(env), r.Env...), passEnv(r.PassEnv)...), func(n int) {
		if err := messageChecks(r, env, start, n, &checks); err != nil && checkErr == nil {
			checkErr = err
		}
	})
	if err != nil {
		return nil, err
	}
	facts := r.Cond.Stream(res.stream)
	m := &Metrics{Task: r.Task.ID, Variant: r.Variant, Condition: r.Cond.Name(), Repeat: r.Repeat, SessionID: r.SessionID, Model: r.Model, PiEnv: r.PassEnv, Profile: r.Profile, ProjectSettings: r.ProjectSettings,
		TaskSize: r.Task.Size, SteadyState: r.Steady, Exit: res.exit, TimedOut: res.timedOut, TurnCapped: res.turnCapped, UIRequestsDeclined: res.uiDeclined, Turns: facts.Turns,
		OutputTokens: facts.OutputTokens, CostUSD: facts.CostUSD, WallMs: float64(res.wall.Milliseconds()), Utilization: facts.Utilization}
	m.Messages = messageMetrics(res.stream, m.WallMs)
	// Refusals come from the stream, the replay's source, on both runtimes (T105).
	m.Refusals = streamRefusals(res.stream)
	if len(r.Task.MessageChecks) > 0 {
		n := regressions(checks)
		m.ChecksPerMessage, m.Regressions = checks, &n
		m.Slices = slicesHeld(checks, messageCheckCount(r.Task))
	}
	if checkErr != nil {
		m.Problems = append(m.Problems, "message check: "+checkErr.Error())
	}
	if facts.SessionID != "" && facts.SessionID != r.SessionID {
		m.Problems = append(m.Problems, "the stream names session "+facts.SessionID)
	}
	// The tree facts and the hidden check read the delivered tree (tree.go).
	out := m.treeFacts(o, r, start, env, nil)
	if err := os.WriteFile(filepath.Join(r.Dir, "check.txt"), []byte(out), 0o644); err != nil {
		return nil, err
	}
	transcript, err := r.Cond.Transcript(r, o)
	if err != nil {
		m.Problems = append(m.Problems, err.Error())
		return m, nil
	}
	m.Transcript = transcript
	if b, err := os.ReadFile(transcript); err != nil {
		m.Problems = append(m.Problems, err.Error())
	} else {
		m.fromTranscript(b, r.Task.TestCommand)
	}
	c := exec.Command(benchJhon(o.Home), "audit", transcript, "--json")
	c.Dir, c.Env = r.Repo, env
	var stderr bytes.Buffer
	c.Stderr = &stderr
	audit, err := c.Output()
	if werr := os.WriteFile(filepath.Join(r.Dir, "audit.json"), audit, 0o644); werr != nil {
		return nil, werr
	}
	if err != nil && len(audit) == 0 {
		m.Problems = append(m.Problems, fmt.Sprintf("jhon audit: %v %s", err, bytes.TrimSpace(stderr.Bytes())))
		return m, nil
	}
	if err := m.fromAudit(audit); err != nil {
		m.Problems = append(m.Problems, "jhon audit --json: "+err.Error())
	}
	return m, nil
}
