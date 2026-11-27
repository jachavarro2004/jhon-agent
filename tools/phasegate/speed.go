package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// speedRuns is how many times each command runs per project; its
	// median and maximum are recorded.
	speedRuns = 5
	// callTimeout ends one call that runs longer: its run counts the time
	// waited and a timeout.
	callTimeout = 30 * time.Second
	// callNice is the niceness of every call, so the owner keeps his CPU.
	callNice = "10"
	// mainAgent is the agent whose guards the hook calls run: the main
	// session's, as the render names it (render.piMain).
	mainAgent = "jhon-orchestrator"
)

// speedConfig is what the speed measure needs: the binary, the tree it
// reads (for its vcs_dirs and bounded writers), the home, the projects
// and the environment of every call.
type speedConfig struct {
	bin, jhonRoot, home string
	projects            []string
	env                 []string
}

// call is one timed command: its name in the record, its words after the
// binary, and its stdin.
type call struct {
	name  string
	args  []string
	stdin []byte
}

// measureSpeed times every command in every project, one call at a time.
func measureSpeed(c speedConfig) (*Speed, error) {
	hook, err := hookArgs(c)
	if err != nil {
		return nil, err
	}
	s := &Speed{Runs: speedRuns}
	for i, p := range c.projects {
		s.Projects = append(s.Projects, measureProject(c, i+1, p, hook))
	}
	return s, nil
}

// speedCommands are the measured commands, in the order they run.
var speedCommands = []string{"status", "risk", "hook-git-status", "hook-edit", "hook-gh-pr-create", "sandbox-run"}

func measureProject(c speedConfig, index int, p string, hook []string) ProjectSpeed {
	ps := ProjectSpeed{Index: index, Commands: map[string]Timing{}, Skipped: map[string]string{}}
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		for _, name := range speedCommands {
			ps.Skipped[name] = "the project folder does not exist"
		}
		return ps
	}
	top, common := gitDirs(p, c.env)
	// The session's environment as `jhon start` makes it: the project's
	// sandbox pinned to its repository's top (sandboxProjectEnv).
	env := slices.DeleteFunc(slices.Clone(c.env), func(kv string) bool { return strings.HasPrefix(kv, sandboxProjectEnv+"=") })
	env = append(env, "CLAUDE_PROJECT_DIR="+p)
	if top != "" {
		env = append(env, sandboxProjectEnv+"="+top)
	}
	ps.Before = countObjects(p, common)
	bash := func(command string) []byte { return payload(p, "Bash", map[string]any{"command": command}) }
	calls := []call{
		{"status", []string{"status", "--json"}, nil},
		{"risk", []string{"risk"}, nil},
		{"hook-git-status", hook, bash("git status")},
	}
	if f := firstTrackedFile(p, env); f != "" {
		calls = append(calls, call{"hook-edit", hook, payload(p, "Edit", map[string]any{"file_path": filepath.Join(p, f), "old_string": "phasegate-old", "new_string": "phasegate-new"})})
	} else {
		ps.Skipped["hook-edit"] = "the project has no tracked file"
	}
	calls = append(calls, call{"hook-gh-pr-create", hook, bash("gh pr create --fill")})
	if why := sandboxCannotRun(c.bin, p, env); why != "" {
		ps.Skipped["sandbox-run"] = why
	} else {
		calls = append(calls, call{"sandbox-run", []string{"sandbox", "run", "--", "true"}, nil})
	}
	for _, k := range calls {
		if t, skip := timeCommand(c.bin, p, env, k); skip != "" {
			ps.Skipped[k.name] = skip
		} else {
			ps.Commands[k.name] = t
		}
	}
	ps.After = countObjects(p, common)
	ps.ObjectsGrew = ps.After.Project > ps.Before.Project || ps.After.Common > ps.Before.Common
	if len(ps.Skipped) == 0 {
		ps.Skipped = nil
	}
	return ps
}

// timeCommand runs k speedRuns times in dir, one after the other; a binary
// that does not know the command is skipped after its first call.
func timeCommand(bin, dir string, env []string, k call) (Timing, string) {
	t := Timing{ExitCodes: map[string]int{}}
	var ms []float64
	for i := range speedRuns {
		r := timed(dir, env, k.stdin, callTimeout, append([]string{"nice", "-n", callNice, bin}, k.args...)...)
		if i == 0 && unknownTo(r.stderr, k.args) {
			return Timing{}, "the binary has no " + commandWords(k.args)
		}
		ms = append(ms, round(float64(r.elapsed.Microseconds())/1000, 1))
		t.Runs++
		if r.timedOut {
			t.Timeouts++
		} else {
			t.ExitCodes[strconv.Itoa(r.code)]++
		}
	}
	t.MedianMS, t.MaxMS = round(median(ms), 1), maxOf(ms)
	return t, ""
}

// commandWords are a command's words before its first option.
func commandWords(args []string) string {
	var w []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			break
		}
		w = append(w, a)
	}
	return strings.Join(w, " ")
}

// unknownTo is whether stderr is jhon's answer to a command, hook or
// subcommand it does not have.
func unknownTo(stderr string, args []string) bool {
	if len(args) == 0 {
		return false
	}
	if strings.Contains(stderr, "unknown command: "+args[0]) {
		return true
	}
	return len(args) > 1 && (strings.Contains(stderr, "unknown hook: "+args[1]) || strings.Contains(stderr, "unknown subcommand: "+args[1]))
}

// sandboxCannotRun is why `jhon sandbox run` is not timed in p, "" when
// `jhon sandbox status --json` says it can run.
func sandboxCannotRun(bin, p string, env []string) string {
	r := timed(p, env, nil, callTimeout, "nice", "-n", callNice, bin, "sandbox", "status", "--json")
	if unknownTo(r.stderr, []string{"sandbox", "status"}) {
		return "the binary has no sandbox status"
	}
	var s struct {
		CanRun *bool `json:"can_run"`
	}
	if json.Unmarshal([]byte(r.stdout), &s) != nil || s.CanRun == nil {
		return "the sandbox status could not be read"
	}
	if !*s.CanRun {
		return "the sandbox cannot run here"
	}
	return ""
}

// payload is the PreToolUse payload Claude Code sends the main session's
// hook for tool with input in p.
func payload(p, tool string, input map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input,
		"cwd": p, "session_id": "phasegate", "permission_mode": "default"})
	return b
}

var (
	vcsDirsLine = regexp.MustCompile(`(?m)^vcs_dirs:\s*\[(.*)\]`)
	agentName   = regexp.MustCompile(`^\s*-\s*name:\s*(\S+)`)
	boundedLine = regexp.MustCompile(`^\s*bounded:\s*"?true"?\s*$`)
)

// hookArgs are the words of the main session's hook call, as the Claude
// Code render writes them (render/claude.go hooksBlock): the guards of an
// agent with Bash that writes and is not bounded, vcs-guard with the
// vocabulary's VCS directories, framework-guard with the install's paths
// under the home, route-guard with the bounded writers. pre-tool applies
// each to the tools it judges.
func hookArgs(c speedConfig) ([]string, error) {
	voc, err := os.ReadFile(filepath.Join(c.jhonRoot, "prose", "contracts", "vocabulary.yaml"))
	m := vcsDirsLine.FindSubmatch(voc)
	if err != nil || m == nil {
		return nil, fmt.Errorf("no vcs_dirs in the vocabulary of the jhon tree (prose/contracts/vocabulary.yaml); name the tree with --jhon-root")
	}
	args := []string{"hook", "pre-tool", "--agent", mainAgent, "safety-guard", "vcs-guard"}
	for _, d := range strings.Split(string(m[1]), ",") {
		if d = strings.TrimSpace(d); d != "" {
			args = append(args, d)
		}
	}
	local := filepath.Join(c.home, ".local")
	args = append(args, "framework-guard", c.jhonRoot, filepath.Join(c.home, ".agents", "jhon"), filepath.Join(local, "share", "jhon", "releases"),
		filepath.Join(local, "share", "jhon", "current"), filepath.Join(local, "bin", "jhon"), filepath.Join(c.home, ".claude", "CLAUDE.md"))
	args = append(args, "secrets-guard", "route-guard")
	return append(args, boundedWriters(filepath.Join(c.jhonRoot, "prose", "agents.yaml"))...), nil
}

// boundedWriters are the agents agents.yaml marks `bounded: true`.
func boundedWriters(path string) []string {
	b, _ := os.ReadFile(path)
	var out []string
	current := ""
	for _, l := range strings.Split(string(b), "\n") {
		if m := agentName.FindStringSubmatch(l); m != nil {
			current = m[1]
		} else if current != "" && boundedLine.MatchString(l) {
			out = append(out, current)
		}
	}
	return out
}

// sandboxProjectEnv pins a session's sandbox to its project (sandbox.ProjectEnv).
const sandboxProjectEnv = "JHON_SANDBOX_PROJECT"

// gitDirs are p's repository top and git common dir, "" outside a repository.
func gitDirs(p string, env []string) (top, common string) {
	r := timed(p, slices.Concat(env, []string{"GIT_OPTIONAL_LOCKS=0"}), nil, callTimeout, "nice", "-n", callNice, "git", "-C", p, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if r.code != 0 || r.timedOut || len(lines) != 2 {
		return "", ""
	}
	return lines[0], lines[1]
}

// countObjects counts the files under p's .git/objects and under the
// common dir's objects (0 for a folder that is not there).
func countObjects(p, common string) Objects {
	o := Objects{Project: countFiles(filepath.Join(p, ".git", "objects"))}
	if common != "" {
		o.Common = countFiles(filepath.Join(common, "objects"))
	}
	return o
}

func countFiles(dir string) int {
	n := 0
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// firstTrackedFile is the first file git tracks in p whose name does not
// start with a dot, among its first 10000; "" for none.
func firstTrackedFile(p string, env []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "nice", "-n", callNice, "git", "-C", p, "ls-files", "-z")
	c.Env = slices.Concat(env, []string{"GIT_OPTIONAL_LOCKS=0"})
	out, err := c.StdoutPipe()
	if err != nil || c.Start() != nil {
		return ""
	}
	found := ""
	r := bufio.NewReader(out)
	for range 10000 {
		entry, err := r.ReadString(0)
		if name := strings.TrimSuffix(entry, "\x00"); name != "" && !strings.HasPrefix(filepath.Base(name), ".") {
			found = name
			break
		}
		if err != nil {
			break
		}
	}
	cancel()
	io.Copy(io.Discard, out)
	c.Wait()
	return found
}

// result is one call's time, exit code, output (the first 64 KiB of each)
// and whether it hit its timeout.
type result struct {
	elapsed        time.Duration
	code           int
	stdout, stderr string
	timedOut       bool
}

// timed runs argv in dir, in a process group of its own that the timeout
// kills whole.
func timed(dir string, env []string, stdin []byte, timeout time.Duration, argv ...string) result {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir, c.Env = dir, env
	if stdin != nil {
		c.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr capped
	c.Stdout, c.Stderr = &stdout, &stderr
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 2 * time.Second
	start := time.Now()
	err := c.Run()
	r := result{elapsed: time.Since(start), stdout: stdout.String(), stderr: stderr.String(), timedOut: errors.Is(ctx.Err(), context.DeadlineExceeded)}
	switch {
	case c.ProcessState != nil:
		r.code = c.ProcessState.ExitCode()
	case err != nil:
		r.code = -1
	}
	return r
}

// capped keeps the first 64 KiB written to it.
type capped struct{ bytes.Buffer }

func (c *capped) Write(p []byte) (int, error) {
	if room := 64<<10 - c.Len(); room > 0 {
		c.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
