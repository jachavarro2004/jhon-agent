package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// A cli integration (kind cli) is a program of its own, its doctor.binary,
// which answers two questions in JSON (integrations/README.md, "A cli
// integration"; odd/tasks/jhon-studio.md S3):
//
//   - `<binary> integration --json`, what a runtime loads: {version,
//     plugin_dir, skill}, plugin_dir a Claude Code plugin folder and skill
//     the SKILL.md pi's shell sends as a command's prompt;
//   - `<binary> doctor --json`, its own checks: {ok, checks: [{id, ok,
//     detail, fix}]}, exiting 1 when one fails.
//
// jhon start asks the first for an integration that is on (when_off:
// launch), jhon doctor the second; neither asks one that is off.

// Answer is `<binary> integration --json`.
type Answer struct {
	Version   string `json:"version"`
	PluginDir string `json:"plugin_dir"`
	Skill     string `json:"skill"`
}

// Check is one of `<binary> doctor --json`'s checks: Fix says how to mend it
// when it fails.
type Check struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

// askTimeout bounds each question: a program that does not answer in it
// has no answer, and nothing waits on it longer.
const askTimeout = 10 * time.Second

// AskIntegration is m's program's `integration --json`; an error says why
// there is none (not on PATH, a failure, not the JSON, no plugin_dir).
func AskIntegration(m Manifest) (Answer, error) {
	var a Answer
	out, err := ask(m.Doctor.Binary, "integration", "--json")
	if err != nil {
		return a, err
	}
	if json.Unmarshal(out, &a) != nil {
		return a, fmt.Errorf("%s integration --json: not the JSON {version, plugin_dir, skill}", m.Doctor.Binary)
	}
	if a.PluginDir == "" {
		return a, fmt.Errorf("%s integration --json: names no plugin_dir", m.Doctor.Binary)
	}
	return a, nil
}

// AskDoctor is m's program's `doctor --json` checks, in its order; an exit
// 1 that still prints its report is an answer.
func AskDoctor(m Manifest) ([]Check, error) {
	out, err := ask(m.Doctor.Binary, "doctor", "--json")
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return nil, err
	}
	var r struct {
		Checks *[]Check `json:"checks"`
	}
	if json.Unmarshal(out, &r) != nil || r.Checks == nil {
		return nil, fmt.Errorf("%s doctor --json: not the JSON {ok, checks: [{id, ok, detail, fix}]}", m.Doctor.Binary)
	}
	return *r.Checks, nil
}

// ask runs bin with args, bounded by askTimeout: its standard output, or
// why it gave none (an *exec.ExitError, wrapped, keeps its exit code).
func ask(bin string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("%s is not on PATH", bin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), askTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	// A program whose child keeps stdout open must not hold the caller past the bound.
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return out, fmt.Errorf("%s %s: did not answer in %s", bin, strings.Join(args, " "), askTimeout)
	}
	if err != nil {
		return out, fmt.Errorf("%s %s: %w", bin, strings.Join(args, " "), err)
	}
	return out, nil
}
