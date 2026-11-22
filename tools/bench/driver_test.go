package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// multiTask is pagination-bug sent as two owner messages, with extra task.yaml
// lines, loaded from a scratch copy.
func multiTask(t *testing.T, extra string) Task {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pagination-bug")
	if err := copyTree(filepath.Join("tasks", "pagination-bug"), dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "task.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(b), "prompt: |")
	yaml := head + "prompt_1: |\n  first message\nprompt_2: |\n  second message\n" + extra
	if err := os.WriteFile(filepath.Join(dir, "task.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// multiOptions are options over the two-message task, against the fake f.
func multiOptions(t *testing.T, f fake, extra string, conds ...string) *Options {
	t.Helper()
	o, _ := options(t, f, "pagination-bug", conds, 1)
	o.Tasks = []Task{multiTask(t, extra)}
	return o
}

// boundaries are the bench_message records of a stream, as "n phase reason".
func boundaries(b []byte) []string {
	var out []string
	for _, r := range records(b) {
		if str(r["type"]) == boundaryType {
			out = append(out, strings.TrimSpace(strings.Join([]string{strconv.Itoa(int(num(r["n"]))), str(r["phase"]), str(r["reason"])}, " ")))
		}
	}
	return out
}

func readStream(t *testing.T, o *Options, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(o.Results, "runs", name, "stream.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAMultiMessagePiRunUsesRPCAndNoPromptArgument(t *testing.T) {
	f := newFake(t)
	o := multiOptions(t, f, "", "pi-jhon")
	runs, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{benchJhon(f.home), "start", "pi", "--mode", "rpc", "--provider", "claude-bridge", "--model", "haiku", "--session-id", runs[0].SessionID}
	if !slices.Equal(runs[0].Argv, want) || !runs[0].RPC || runs[0].Next != nil {
		t.Fatalf("argv %q rpc %t next %q", runs[0].Argv, runs[0].RPC, runs[0].Next)
	}
}

func TestThePiDriverSendsEachMessageAfterTheLastSettled(t *testing.T) {
	f := newFake(t)
	o := multiOptions(t, f, "", "pi-jhon")
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	stream := readStream(t, o, "pagination-bug-plain-pi-jhon-1")
	if got := boundaries(stream); !slices.Equal(got, []string{"1 start", "1 end settled", "2 start", "2 end settled"}) {
		t.Errorf("boundaries %q", got)
	}
	// The second message starts only after the first agent_settled.
	settled := strings.Index(string(stream), `"agent_settled"`)
	if second := strings.Index(string(stream), `"n":2,"phase":"start"`); settled < 0 || second < settled {
		t.Errorf("message 2 sent at %d, first agent_settled at %d:\n%s", second, settled, stream)
	}
	prompts, err := os.ReadFile(filepath.Join(f.dir, "pi-prompts.log"))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"id":"bench-1","message":"first message\n","type":"prompt"}` + "\n" + `{"id":"bench-2","message":"second message\n","type":"prompt"}` + "\n"; string(prompts) != want {
		t.Errorf("pi read\n%s\nwant\n%s", prompts, want)
	}
	m := metricsOf(t, o, "pagination-bug-plain-pi-jhon-1")
	if m.Exit != 0 || m.Turns != 6 || m.TurnCapped {
		t.Errorf("%+v", m)
	}
}

func TestThePiTurnCapCoversEveryMessage(t *testing.T) {
	f := newFake(t)
	t.Setenv("FAKE_PI_TURNS", "40")
	o := multiOptions(t, f, "", "pi-jhon")
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	m := metricsOf(t, o, "pagination-bug-plain-pi-jhon-1")
	if !m.TurnCapped || m.Turns != 61 {
		t.Errorf("turn cap over the run: %+v", m)
	}
	if got := boundaries(readStream(t, o, "pagination-bug-plain-pi-jhon-1")); !slices.Equal(got, []string{"1 start", "1 end settled", "2 start", "2 end turn_cap"}) {
		t.Errorf("boundaries %q", got)
	}
}

func TestASingleMessagePiRunKeepsJSONModeAndNoBoundaries(t *testing.T) {
	f := newFake(t)
	o, _ := options(t, f, "pagination-bug", []string{"pi-jhon"}, 1)
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	if got := boundaries(readStream(t, o, "pagination-bug-plain-pi-jhon-1")); got != nil {
		t.Errorf("boundaries %q", got)
	}
	if !strings.Contains(strings.Join(f.calls(t), "\n"), "pi --mode json") {
		t.Errorf("calls %q", f.calls(t))
	}
}

func TestAMultiMessageClaudeRunResumesItsSessionPerMessage(t *testing.T) {
	f := newFake(t)
	o := multiOptions(t, f, "", "jhon", "clean")
	runs, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		first, sid := r.Argv, r.SessionID
		if r.RPC || len(r.Next) != 1 || !slices.Contains(first, "first message\n") || !slices.Contains(first, "--session-id") {
			t.Fatalf("%s: argv %q rpc %t next %q", r.Name(), first, r.RPC, r.Next)
		}
		next := r.Next[0]
		at := slices.Index(next, "--resume")
		if at < 0 || next[at+1] != sid || slices.Contains(next, "--session-id") || !slices.Contains(next, "second message\n") {
			t.Errorf("%s: next %q", r.Name(), next)
		}
		// Everything else is the first call's.
		strip := func(argv []string, flag string) []string {
			i := slices.Index(argv, flag)
			return slices.Concat(argv[:i], argv[i+2:])
		}
		a, b := strip(first, "--session-id"), strip(next, "--resume")
		a[slices.Index(a, "first message\n")], b[slices.Index(b, "second message\n")] = "", ""
		if !slices.Equal(a, b) {
			t.Errorf("%s: calls differ\n%q\n%q", r.Name(), a, b)
		}
	}
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(f.calls(t), "\n")
	for _, name := range []string{"pagination-bug-plain-jhon-1", "pagination-bug-plain-clean-1"} {
		m := metricsOf(t, o, name)
		if got := boundaries(readStream(t, o, name)); !slices.Equal(got, []string{"1 start", "1 end settled", "2 start", "2 end settled"}) {
			t.Errorf("%s: boundaries %q", name, got)
		}
		if m.Exit != 0 || m.Turns != 14 || m.OutputTokens != 1800 || len(m.Problems) != 0 {
			t.Errorf("%s: %+v", name, m)
		}
		if !strings.Contains(calls, "--max-turns 53 ") || !strings.Contains(calls, "--resume "+m.SessionID) {
			t.Errorf("%s: no resumed call with the turns left:\n%s", name, calls)
		}
		if b, _ := os.ReadFile(m.Transcript); strings.Count(string(b), "\n") != 2 {
			t.Errorf("%s: the resumed call did not append to the transcript: %q", name, b)
		}
	}
}

func TestAFailedClaudeCallSendsNoFurtherMessage(t *testing.T) {
	f := newFake(t)
	t.Setenv("FAKE_CLAUDE_EXIT", "1")
	o := multiOptions(t, f, "", "clean")
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(f.calls(t), "\n"), "--resume") {
		t.Errorf("a second call followed a failed one: %q", f.calls(t))
	}
	m := metricsOf(t, o, "pagination-bug-plain-clean-1")
	if got := boundaries(readStream(t, o, "pagination-bug-plain-clean-1")); m.Exit != 1 || !slices.Equal(got, []string{"1 start", "1 end exit 1"}) {
		t.Errorf("exit %d boundaries %q", m.Exit, got)
	}
}

func TestTheDryRunShowsEachMessage(t *testing.T) {
	f := newFake(t)
	o := multiOptions(t, f, "", "clean", "pi-jhon")
	o.DryRun = true
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	out := o.Out.(interface{ String() string }).String()
	for _, s := range []string{"messages: 2", "message 2: $ ", "-p 'second message\n' --safe-mode", "--resume 00000000-0000-4000-8000-000000000001",
		"start pi --mode rpc", "messages 1-2: a prompt record each on stdin, the next after agent_settled"} {
		if !strings.Contains(out, s) {
			t.Errorf("dry run lacks %q:\n%s", s, out)
		}
	}
}

// Claude Code's agent and background tools run unprompted, as pi's subagent
// and bash_background do in the shell (T46 tool parity; names from claude
// 2.1.310: Agent, Monitor, TaskOutput, TaskStop; Bash runs in background).
func TestTheClaudeCodeConditionsAllowTheAgentAndBackgroundTools(t *testing.T) {
	allowed := strings.Split(DefaultAllowedTools, ",")
	for _, tool := range []string{"Bash", "Edit", "MultiEdit", "Write", "Read", "Glob", "Grep", "Agent", "Monitor", "TaskOutput", "TaskStop"} {
		if !slices.Contains(allowed, tool) {
			t.Errorf("--allowedTools lacks %s: %s", tool, DefaultAllowedTools)
		}
	}
}

// A guard dialog pi raises in RPC mode is answered as cancelled, as Claude
// Code -p turns an ask into a refusal, and counted (T47); a fire-and-forget
// request gets no answer.
func TestThePiDriverCancelsADialogAndCountsIt(t *testing.T) {
	f := newFake(t)
	t.Setenv("FAKE_PI_UI", "1")
	o := multiOptions(t, f, "", "pi-jhon")
	o.Tasks[0].Timeout = 20 * time.Second
	start := time.Now()
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("the run waited %s on the dialog", took)
	}
	answers, _ := os.ReadFile(filepath.Join(f.dir, "ui-answers.log"))
	if want := `{"cancelled":true,"id":"ui-2","type":"extension_ui_response"}` + "\n"; string(answers) != want {
		t.Errorf("pi read %q, want %q", answers, want)
	}
	name := "pagination-bug-plain-pi-jhon-1"
	if got := boundaries(readStream(t, o, name)); !slices.Equal(got, []string{"1 start", "1 end settled", "2 start", "2 end settled"}) {
		t.Errorf("boundaries %q", got)
	}
	if m := metricsOf(t, o, name); m.UIRequestsDeclined != 1 || m.TimedOut {
		t.Errorf("%+v", m)
	}
}

// A prompt pi rejects because a notice turn is running is sent again once
// that turn settles, never dropped (L444: slow-suite-ldd-pi-jhon-1).
func TestABusyPromptIsSentAgainAfterTheTurnSettles(t *testing.T) {
	f := newFake(t)
	t.Setenv("FAKE_PI_BUSY", "1")
	o := multiOptions(t, f, "", "pi-jhon")
	o.Tasks[0].Timeout = 20 * time.Second
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	name := "pagination-bug-plain-pi-jhon-1"
	if got := boundaries(readStream(t, o, name)); !slices.Equal(got, []string{"1 start", "1 end settled", "2 start", "2 busy", "2 end settled"}) {
		t.Errorf("boundaries %q", got)
	}
	prompts, _ := os.ReadFile(filepath.Join(f.dir, "pi-prompts.log"))
	if n := strings.Count(string(prompts), `"id":"bench-2"`); n != 2 {
		t.Errorf("message 2 was sent %d times, want 2:\n%s", n, prompts)
	}
	if m := metricsOf(t, o, name); m.TimedOut {
		t.Errorf("%+v", m)
	}
}

// withExitGrace sets exitGrace to d for the test.
func withExitGrace(t *testing.T, d time.Duration) {
	t.Helper()
	old := exitGrace
	exitGrace = d
	t.Cleanup(func() { exitGrace = old })
}

// A pi still running a grace period after the bench closed its stdin, every
// message settled, is killed with its process group: the run ends then, not
// at its timeout, and is no timeout (T201, L1630).
func TestAPiThatOutlivesItsLastMessageIsEndedAfterTheGrace(t *testing.T) {
	f := newFake(t)
	t.Setenv("FAKE_PI_STAY", "60")
	withExitGrace(t, time.Second)
	o := multiOptions(t, f, "", "pi-jhon")
	o.Tasks[0].Timeout = 20 * time.Second
	start := time.Now()
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("the run took %s after its last message settled", took)
	}
	name := "pagination-bug-plain-pi-jhon-1"
	if got := boundaries(readStream(t, o, name)); !slices.Equal(got, []string{"1 start", "1 end settled", "2 start", "2 end settled"}) {
		t.Errorf("boundaries %q", got)
	}
	if m := metricsOf(t, o, name); m.TimedOut || m.Exit != -1 {
		t.Errorf("timed_out %t exit %d, want false -1", m.TimedOut, m.Exit)
	}
}

// The run's deadline ends it even when a process outside pi's process group
// holds pi's stdout: the group is killed at the deadline and the output cut
// a grace period later, and the run is recorded as a timeout (T201, L1630).
func TestTheTimeoutEndsARunWhoseOutputAnotherProcessHolds(t *testing.T) {
	f := newFake(t)
	t.Setenv("FAKE_PI_ORPHAN", "45")
	t.Cleanup(func() {
		if b, err := os.ReadFile(filepath.Join(f.dir, "orphan.pid")); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	withExitGrace(t, time.Second)
	o := multiOptions(t, f, "", "pi-jhon")
	o.Tasks[0].Timeout = 3 * time.Second
	start := time.Now()
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 15*time.Second {
		t.Errorf("a 3s run took %s", took)
	}
	name := "pagination-bug-plain-pi-jhon-1"
	if got := boundaries(readStream(t, o, name)); !slices.Equal(got, []string{"1 start", "1 end timeout"}) {
		t.Errorf("boundaries %q", got)
	}
	if m := metricsOf(t, o, name); !m.TimedOut {
		t.Errorf("timed_out %t, want true", m.TimedOut)
	}
}
