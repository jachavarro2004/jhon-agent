package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// How a run's owner messages reach the agent (T46). A single-message run
// starts one command with the prompt in its argv. A multi-message run
// either starts one command and writes its messages to stdin (pi's RPC
// mode: one `prompt` record, then wait for agent_settled before the next)
// or starts one command per message on the same session (Claude Code: -p
// with --session-id, then --resume). Either way the timeout and the turn cap
// cover the whole run, and stream.jsonl holds a bench_message record before
// and after each message.

// boundaryType is the bench's own stream record type around a message.
const boundaryType = "bench_message"

// boundary is a bench_message record: phase start before message n is
// sent, end when it settled or the run stopped (reason says which: settled,
// handled, rejected, exit <code>, timeout, turn_cap), or wait when the agent
// settled with background jobs still running (reason: their ids) and the
// message stays open; at_ms is since the run started.
type boundary struct {
	Type   string `json:"type"`
	N      int    `json:"n"`
	Phase  string `json:"phase"`
	AtMs   int64  `json:"at_ms"`
	Reason string `json:"reason,omitempty"`
}

// agentResult is how the agent's process ended.
type agentResult struct {
	exit     int
	timedOut bool
	// uiDeclined are the dialogs pi raised in RPC mode, each answered as
	// cancelled.
	uiDeclined int
	turnCapped bool
	wall       time.Duration
	stream     []byte
}

// session is one run's agent: its stream so far and its turns.
type session struct {
	r       *Run
	env     []string
	stderr  io.Writer
	stream  io.Writer
	buf     bytes.Buffer
	start   time.Time
	ctx     context.Context
	cancel  context.CancelFunc
	turns   int
	res     agentResult
	settled func(n int)
}

func (s *session) write(line []byte) {
	s.buf.Write(line)
	_, _ = s.stream.Write(line)
}

// mark writes a bench_message record when the run has several messages.
func (s *session) mark(n int, phase, reason string) {
	if !s.r.Task.MultiMessage() {
		return
	}
	b, _ := json.Marshal(boundary{Type: boundaryType, N: n, Phase: phase, AtMs: time.Since(s.start).Milliseconds(), Reason: reason})
	s.write(append(b, '\n'))
}

// countTurn counts a turn_start for a condition that caps turns itself and
// stops the run past the task's turns.
func (s *session) countTurn(line []byte) {
	if s.r.Cond.CapsTurns() && bytes.Contains(line, []byte(`"type":"turn_start"`)) {
		if s.turns++; s.turns > s.r.Task.MaxTurns && !s.res.turnCapped {
			s.res.turnCapped = true
			s.cancel()
		}
	}
}

// stopReason is why the run stopped, for an end record.
func (s *session) stopReason(exit int) string {
	switch {
	case s.res.turnCapped:
		return "turn_cap"
	case errors.Is(s.ctx.Err(), context.DeadlineExceeded):
		return "timeout"
	case exit == 0:
		return "settled"
	}
	return "exit " + strconv.Itoa(exit)
}

// exitGrace bounds how long the bench waits for an agent it is done with
// (T201): an agent still running this long after the bench closed its stdin
// is killed with its process group, and its stdout is cut this long after the
// group was killed when a process outside the group still holds it.
var exitGrace = 10 * time.Second

// stdin is an agent's stdin whose closed channel closes with it.
type stdin struct {
	io.WriteCloser
	once   sync.Once
	closed chan struct{}
}

func (w *stdin) Close() error {
	w.once.Do(func() { close(w.closed) })
	return w.WriteCloser.Close()
}

// run starts argv in the run's repo, calls begin with its stdin when begin
// is not nil, and line with each stdout line once it is in the stream. The
// process group is killed when the run's context ends, or exitGrace after
// the bench closed its stdin; see reap.
func (s *session) run(argv []string, begin func(io.WriteCloser), line func([]byte, io.WriteCloser)) (int, error) {
	c := exec.CommandContext(s.ctx, argv[0], argv[1:]...)
	c.Dir, c.Env, c.Stderr = s.r.Repo, s.env, s.stderr
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = exitGrace
	var in io.WriteCloser
	closed := make(chan struct{})
	if begin != nil {
		p, err := c.StdinPipe()
		if err != nil {
			return 0, err
		}
		in = &stdin{WriteCloser: p, closed: closed}
	}
	out, err := c.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err := c.Start(); err != nil {
		return 0, err
	}
	done := make(chan struct{})
	reaped := s.reap(c.Process.Pid, closed, out, done)
	if begin != nil {
		begin(in)
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		l := append(sc.Bytes(), '\n')
		s.write(l)
		line(l, in)
	}
	// The process is not reaped before reap returns, so its pid still names
	// its group when reap kills it.
	close(done)
	<-reaped
	err = c.Wait()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	case s.ctx.Err() != nil:
		return -1, nil
	}
	return 0, err
}

// reap ends the agent in process group pgid once the bench is done with it,
// until done closes (its stdout ended), and returns a channel closed when it
// stops. When the run's context ends, exec's Cancel kills the group; when
// closed closes (the bench closed its stdin: every message settled, or the
// run stopped) and the agent's stdout is still open exitGrace later, reap
// kills the group. Either way, if a process outside the group still holds the
// stdout exitGrace after that, reap closes out, so the scan ends and the run
// never holds its slot past its deadline (T201, L1630).
func (s *session) reap(pgid int, closed <-chan struct{}, out io.Closer, done <-chan struct{}) <-chan struct{} {
	reaped := make(chan struct{})
	go func() {
		defer close(reaped)
		select {
		case <-done:
			return
		case <-s.ctx.Done():
		case <-closed:
			grace := time.NewTimer(exitGrace)
			defer grace.Stop()
			select {
			case <-done:
				return
			case <-s.ctx.Done():
			case <-grace.C:
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
		}
		cut := time.NewTimer(exitGrace)
		defer cut.Stop()
		select {
		case <-done:
		case <-cut.C:
			_ = out.Close()
		}
	}()
	return reaped
}

// startAgent runs r's agent in its repo with env, copying stdout to
// stream.jsonl and stderr to stderr.txt, and calls settled after each owner
// message settles (nil: nothing to do).
func startAgent(r *Run, env []string, settled func(n int)) (agentResult, error) {
	stream, err := os.Create(filepath.Join(r.Dir, "stream.jsonl"))
	if err != nil {
		return agentResult{}, err
	}
	defer stream.Close()
	stderr, err := os.Create(filepath.Join(r.Dir, "stderr.txt"))
	if err != nil {
		return agentResult{}, err
	}
	defer stderr.Close()
	if settled == nil {
		settled = func(int) {}
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.Timeout)
	defer cancel()
	s := &session{r: r, env: env, stderr: stderr, stream: stream, start: time.Now(), ctx: ctx, cancel: cancel, settled: settled}
	if r.RPC {
		err = s.rpc()
	} else {
		err = s.calls()
	}
	s.res.wall, s.res.stream = time.Since(s.start), s.buf.Bytes()
	s.res.timedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	return s.res, err
}

// calls runs r.Argv, then each of r.Next on the same session once the one
// before ended well, the turns left in the task as its --max-turns.
func (s *session) calls() error {
	for i, argv := range append([][]string{s.r.Argv}, s.r.Next...) {
		n := i + 1
		if i > 0 {
			left := s.r.Task.MaxTurns - s.turns
			if left < 1 {
				s.res.turnCapped = true
				return nil
			}
			argv = withMaxTurns(argv, left)
		}
		s.mark(n, "start", "")
		from := s.buf.Len()
		exit, err := s.run(argv, nil, func(l []byte, _ io.WriteCloser) { s.countTurn(l) })
		if err != nil {
			return err
		}
		s.res.exit = exit
		if !s.r.Cond.CapsTurns() {
			s.turns += s.r.Cond.Stream(s.buf.Bytes()[from:]).Turns
		}
		s.mark(n, "end", s.stopReason(exit))
		if exit != 0 || s.ctx.Err() != nil {
			return nil
		}
		s.settled(n)
	}
	return nil
}

// withMaxTurns is argv with its --max-turns value set to n.
func withMaxTurns(argv []string, n int) []string {
	out := append([]string(nil), argv...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == "--max-turns" {
			out[i+1] = strconv.Itoa(n)
		}
	}
	return out
}

// busyRetries is how many times a message pi refused as busy is sent again.
const busyRetries = 3

// rpc runs r.Argv once and writes the task's messages to its stdin, each
// after the one before settled; stdin closes after the last, which ends pi
// (or, exitGrace later, reap).
func (s *session) rpc() error {
	prompts, n, open := s.r.Task.Prompts, 0, false
	// busy counts the times pi refused the open message because a turn of
	// its own (a background notice) was running: the message goes again
	// once that turn settles, at most busyRetries times (L444).
	busy, resent := 0, true
	jobs := backgroundJobs{running: map[string]bool{}, waited: map[string]bool{}, agents: map[string]bool{}}
	write := func(in io.WriteCloser) {
		b, _ := json.Marshal(map[string]string{"id": fmt.Sprintf("bench-%d", n), "type": "prompt", "message": prompts[n-1]})
		_, _ = in.Write(append(b, '\n'))
	}
	send := func(in io.WriteCloser) {
		if n == len(prompts) || s.ctx.Err() != nil {
			_ = in.Close()
			return
		}
		n++
		busy = 0
		s.mark(n, "start", "")
		open = true
		write(in)
	}
	end := func(in io.WriteCloser, reason string, next bool) {
		s.mark(n, "end", reason)
		open = false
		if !next {
			_ = in.Close()
			return
		}
		s.settled(n)
		send(in)
	}
	exit, err := s.run(s.r.Argv, send, func(l []byte, in io.WriteCloser) {
		s.countTurn(l)
		if bytes.Contains(l, []byte(`"type":"extension_ui_request"`)) {
			s.decline(l, in)
			return
		}
		if !open || s.ctx.Err() != nil {
			return
		}
		jobs.see(l)
		switch {
		case bytes.Contains(l, []byte(`"type":"agent_settled"`)):
			if busy > 0 && busy <= busyRetries && !resent {
				resent = true
				write(in)
				return
			}
			if wait := jobs.unwaited(); wait != nil {
				s.mark(n, "wait", strings.Join(wait, " "))
				return
			}
			end(in, "settled", true)
		case bytes.Contains(l, []byte(`"type":"response"`)) && bytes.Contains(l, []byte(`"bench-`+strconv.Itoa(n)+`"`)):
			var r map[string]any
			if json.Unmarshal(l, &r) != nil {
				return
			}
			if r["success"] == false && strings.Contains(str(r["error"]), "already processing") && busy < busyRetries {
				busy++
				resent = false
				s.mark(n, "busy", "")
			} else if r["success"] == false {
				end(in, "rejected", false)
			} else if str(at(r, "data", "disposition")) == "handled" {
				end(in, "handled", true)
			}
		}
	})
	if err != nil {
		return err
	}
	s.res.exit = exit
	if open {
		s.mark(n, "end", s.stopReason(exit))
	}
	return nil
}

// backgroundJobs are the agent's background jobs (bash_background, monitor)
// and background sub-agents as the stream shows them (T47, T209): a job is
// running from a record whose details.jhonTasks.status is running (the tool's
// result, task_result) until one shows another status (the shell's
// jhon-tasks.notice message, task_result, task_stop). A sub-agent is running
// from its launch, the subagent or subagent_continue result whose
// details.task names it (agent-N; its result has no jhonTasks), until a
// record's jhonTasks with its id shows another status. waited are the jobs a
// message already stayed open for once; agents are the sub-agents' ids.
type backgroundJobs struct {
	running, waited, agents map[string]bool
}

// subagentTools are the tools whose result names a background sub-agent it
// launched in details.task.
var subagentTools = []string{"subagent", "subagent_continue"}

func (j backgroundJobs) see(l []byte) {
	if !bytes.Contains(l, []byte(`"jhonTasks"`)) && !bytes.Contains(l, []byte(`"toolName":"subagent`)) {
		return
	}
	var r map[string]any
	if json.Unmarshal(l, &r) != nil {
		return
	}
	if str(r["type"]) == "tool_execution_end" && slices.Contains(subagentTools, str(r["toolName"])) && r["isError"] != true {
		// A foreground sub-agent's result has no task; a launch never
		// overrides an end the stream already showed.
		if id := str(at(r, "result", "details", "task")); id != "" && !j.agents[id] {
			j.agents[id], j.running[id] = true, true
		}
	}
	for _, t := range []any{at(r, "result", "details", "jhonTasks"), at(r, "message", "details", "jhonTasks")} {
		// A monitor watches until it is stopped and may never end: only a
		// command job or a sub-agent holds a message open (L444).
		if id := str(at(t, "id")); id != "" && str(at(t, "kind")) != "monitor" {
			j.running[id] = str(at(t, "status")) == "running"
			if str(at(t, "kind")) == "agent" {
				j.agents[id] = true
			}
		}
	}
}

// unwaited are the running jobs a message waits for at an agent_settled,
// sorted, now marked waited; nil when none. A message whose agent settled
// with a job still running stays open until the next agent_settled (the
// job's notice turn), once per command job: one whose end the stream never
// shows keeps it open no longer. A sub-agent keeps it open at every
// agent_settled until the stream shows its end, as the shell's notice always
// does (T209): another job's notice turn does not close it. The run's
// timeout bounds the wait.
func (j backgroundJobs) unwaited() []string {
	var out []string
	for id, running := range j.running {
		if running && (!j.waited[id] || j.agents[id]) {
			j.waited[id] = true
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// dialogs are the extension UI methods pi blocks on until the client answers
// (rpc-extension-ui.md); the others are fire-and-forget.
var dialogs = []string{"select", "confirm", "input", "editor"}

// decline answers a dialog request as cancelled: nobody is at the bench to
// answer, and Claude Code -p turns a guard's ask into a refusal (T47).
func (s *session) decline(l []byte, in io.WriteCloser) {
	var r map[string]any
	if json.Unmarshal(l, &r) != nil || !slices.Contains(dialogs, str(r["method"])) {
		return
	}
	b, _ := json.Marshal(map[string]any{"type": "extension_ui_response", "id": str(r["id"]), "cancelled": true})
	if _, err := in.Write(append(b, '\n')); err == nil {
		s.res.uiDeclined++
	}
}
