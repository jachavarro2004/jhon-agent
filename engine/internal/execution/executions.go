package execution

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// This file is the recorder of test runs: an optional,
// recommended run of every selected project's resolved command and code
// checks (each recorded with its kind), one after
// another, through `sh -c` in the project's directory, continuing after a
// failure, and the record of it that status and delivery read.
//
//   - Deadline: each project gets its minutes. A project past them has its
//     whole process group terminated (TERM, then KILL after a grace period),
//     is recorded as `timeout`, and the run continues with the next project.
//   - Partial results: the record is rewritten atomically after every
//     project, `complete: false` until the run ends, so an interrupted or
//     killed run leaves what it observed.
//   - Interruption: SIGINT or SIGTERM stops the running project's process
//     group (TERM, a bounded wait, then KILL) before the partial record is
//     written and the executor exits; no project outlives it.
//   - Identity: the candidate (HEAD's commit and tree, and whether the working
//     tree is clean) is read before the first command and again after the
//     last, and so is the working tree's git tree (`working_tree`, then
//     `after_working_tree`). A difference marks the record `dirty`: what was
//     tested is not one revision. The change's own record files never make
//     the tree unclean.
//
// Projects run sequentially: their output streams to
// the terminal unmixed, and each deadline is the project's alone.

type vocabulary struct{ schema, file string }

// vocab is the record's schema and file name, on first use.
var vocab = sync.OnceValues(func() (vocabulary, error) {
	schema, err := contract.Vocabulary("execution_record_schema", 1)
	if err != nil {
		return vocabulary{}, err
	}
	src, err := contract.Contract("vocabulary.yaml")
	if err != nil {
		return vocabulary{}, err
	}
	_, keys := contract.Map(src, "execution_record")
	if keys["file"] == "" {
		return vocabulary{}, errors.New("contracts/vocabulary.yaml: execution_record must name file")
	}
	return vocabulary{schema[0], keys["file"]}, nil
})

// Vocabulary reads the record's vocabulary, so a command fails up front when it is broken.
func Vocabulary() error {
	_, err := vocab()
	return err
}

// mustVocab is the vocabulary; a caller that has not checked Vocabulary
// panics on a broken one.
func mustVocab() vocabulary {
	v, err := vocab()
	if err != nil {
		panic(err)
	}
	return v
}

func recordFile() string { return mustVocab().file }

// Schema is the execution_record_schema a record names.
func Schema() string { return mustVocab().schema }

// Changes is the directory of the active changes, relative to the workspace root.
func ChangesOf(root string) string { return filepath.FromSlash(layout.For(root).ChangesDir()) }

// DefaultTimeoutMinutes is a project's deadline when the run names none.
const DefaultTimeoutMinutes = 20

// grace is the time between TERM and KILL for a project past its deadline.
const grace = 5 * time.Second

// The statuses of an execution.
const (
	Pass    = "pass"
	Fail    = "fail"
	Timeout = "timeout"
)

// RecordFiles are the change's own record files: written after the
// candidate is fixed, never part of what was tested.
func RecordFiles() []string { return []string{"verify-report.md", recordFile(), "delivery.json"} }

// Execution is one project's run.
type Execution struct {
	Project string `json:"project"`
	// Kind is the code check's kind (`code_check_kind`); absent for the test command.
	Kind       string `json:"kind,omitempty"`
	Command    string `json:"command"`
	Cwd        string `json:"cwd"`
	ExitCode   *int   `json:"exit_code"`
	Status     string `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	// Reused is a pass carried from the last run on the same working tree.
	Reused bool `json:"reused,omitempty"`
	// OutputTail is the end of a run that did not pass, and FailedTests the
	// tests it names as failed: what failed, read from the record, with no
	// run outside jhon to find it.
	OutputTail  string   `json:"output_tail,omitempty"`
	FailedTests []string `json:"failed_tests,omitempty"`
}

// TailBytes is how much of a run's output a record keeps.
const TailBytes = 8 << 10

// Tail keeps the last TailBytes of what is written to it.
type Tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *Tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > TailBytes {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-TailBytes:]...)
	}
	return len(p), nil
}

// String is the kept output, from its first whole line.
func (t *Tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := string(t.buf)
	if len(t.buf) == TailBytes {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	return strings.TrimRight(s, "\n")
}

// failedTest is a failed test as pytest's summary names it (`FAILED
// tests/a.py::test_x - why`, `ERROR tests/a.py::test_y`), or as Vitest's
// does (`FAIL  src/a.test.ts > case`).
var failedTest = lazyre.New(`(?m)^(?:FAILED|ERROR) +(\S+::\S+)|^ *FAIL +(\S+\.(?:test|spec)\.[cm]?[jt]sx?(?: > [^\n]+)?)`)

// FailedTests is the tests output names as failed, each once, in order.
func FailedTests(output string) []string {
	var out []string
	for _, m := range failedTest.FindAllStringSubmatch(output, -1) {
		name := m[1]
		if name == "" {
			name = strings.TrimSpace(m[2])
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// CandidateFields are a candidate as a record writes it; WorkingTreeClean is
// a bool, or vcs.CandidateUnknown().
type CandidateFields struct {
	CandidateCommit  string `json:"candidate_commit"`
	CandidateTree    string `json:"candidate_tree"`
	WorkingTreeClean any    `json:"working_tree_clean"`
}

// Fields is a candidate's record fields.
func Fields(c vcs.Candidate) CandidateFields { return CandidateFields{c.Commit, c.Tree, c.Clean} }

// Record is a change's execution record.
type Record struct {
	Schema         string     `json:"schema"`
	Timestamp      string     `json:"timestamp"`
	Complete       bool       `json:"complete"`
	Scope          string     `json:"scope"`
	Base           *string    `json:"base"`
	ScopeReasons   []string   `json:"scope_reasons"`
	Selection      []Selected `json:"selection"`
	TimeoutMinutes float64    `json:"timeout_minutes"`
	CandidateFields
	// WorkingTree is the git tree of the working state the run tested
	// (vcs.WorkingTree): what a later run on the same bytes may reuse.
	WorkingTree string `json:"working_tree,omitempty"`
	// After is the candidate read after the run; nil until it ends.
	After *CandidateFields `json:"after"`
	// AfterWorkingTree is WorkingTree read again after the run; a
	// difference marks the record Dirty. Empty until the run ends.
	AfterWorkingTree string      `json:"after_working_tree,omitempty"`
	Dirty            bool        `json:"dirty"`
	Executions       []Execution `json:"executions"`
	// History is the outcome of each earlier complete run (HistoryOf).
	History []PastRun `json:"history,omitempty"`
	// LightVerify is the change's light verify notes (AddLightVerify).
	LightVerify []VerifyNote `json:"light_verify,omitempty"`
	// Unit is a unit's run, `--affected` with no base: the narrow kind, which
	// closes no slice and delivers nothing. Since is what it measured from:
	// the working tree of the change's last passing run, or the change's
	// base ref when there was none.
	Unit  bool   `json:"unit,omitempty"`
	Since string `json:"since,omitempty"`
	// LastPass is the change's last passing run before this one (LastPassOf).
	LastPass *PassedRun `json:"last_pass,omitempty"`
	// TurnedGreen is the tests turned green by editing only their test
	// (MarkTurnedGreen), kept from run to run (TurnedGreenOf).
	TurnedGreen []TurnedGreen `json:"turned_green,omitempty"`
}

// RecordPath is the record's path, relative to root, for one change in its
// folder under root's layout; its base name is every layout's.
func RecordPath(root, change string) string {
	return filepath.Join(ChangesOf(root), change, recordFile())
}

// ChangeFile is the absolute path of file, one of change's record files,
// under root: in the change's folder, openspec/changes/<change>/, when it
// has one. A change with no folder, a small change its branch names (T31,
// L254), keeps its record under the git common dir, jhon/changes/<change>/,
// so its runs never dirty the tree (L315); outside git, its folder's path.
func ChangeFile(root, change, file string) string {
	dir := filepath.Join(root, ChangesOf(root), change)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		if common := gitCommonDir(root); common != "" {
			return filepath.Join(common, "jhon", "changes", change, file)
		}
	}
	return filepath.Join(dir, file)
}

// RecordFile is the absolute path of change's execution record (ChangeFile).
func RecordFile(root, change string) string { return ChangeFile(root, change, recordFile()) }

// RecordShown is the record's path as a message prints it: relative to
// root when it is under it (RecordPath for a change with a folder).
func RecordShown(root, change string) string {
	abs := RecordFile(root, change)
	if rel, err := filepath.Rel(root, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
		return rel
	}
	return abs
}

// gitCommonDir is the absolute git common dir of the repository holding
// root, shared by its worktrees; "" outside git.
func gitCommonDir(root string) string {
	out, err := project.ReadGitCmd(root, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// LastTestRun is how long the project's test command took in the most
// recently written execution record, active or archived, that ran it to a
// pass or a failure; ok is false when no record has it. A long run is
// announced with it, so the owner knows when to expect the result.
func LastTestRun(root, project string) (ms int64, ok bool) {
	files, _ := filepath.Glob(filepath.Join(root, ChangesOf(root), "*", recordFile()))
	archived, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(layout.For(root).ArchiveDir()), "*", recordFile()))
	var newest time.Time
	for _, f := range append(files, archived...) {
		st, err := os.Stat(f)
		if err != nil || !st.ModTime().After(newest) {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r struct{ Executions []Execution }
		if json.Unmarshal(b, &r) != nil {
			continue
		}
		for _, e := range r.Executions {
			if e.Project == project && e.Kind == "" && !e.Reused && (e.Status == Pass || e.Status == Fail) {
				newest, ms, ok = st.ModTime(), e.DurationMs, true
			}
		}
	}
	return ms, ok
}

// ChangeDirInRepo is the change directory relative to the git top level, as git names paths.
func ChangeDirInRepo(root, change string) string {
	prefix := ""
	if top := vcs.GitTopLevel(root); top != "" {
		prefix = filepath.ToSlash(relReal(top, root))
	}
	return path.Join(prefix, layout.For(root).ChangeDir(change))
}

// ChangeRecordPaths are the change's record files, relative to the git top
// level, as vcs.CandidateOf exempts them.
func ChangeRecordPaths(root, change string) map[string]bool {
	dir := ChangeDirInRepo(root, change)
	out := map[string]bool{}
	for _, f := range RecordFiles() {
		out[path.Join(dir, f)] = true
	}
	return out
}

// PassesOn are the passing commands of the change's last run that a run on
// workingTree may reuse instead of running again, keyed by Key: that run
// tested the same working tree, ended complete, and saw no change while it
// ran. None when workingTree is "" or the record cannot be read.
func PassesOn(root, change, workingTree string) map[string]Execution {
	out := map[string]Execution{}
	b, err := os.ReadFile(RecordFile(root, change))
	var r Record
	if workingTree == "" || err != nil || json.Unmarshal(b, &r) != nil || r.WorkingTree != workingTree || !r.Complete || r.Dirty {
		return out
	}
	for _, e := range r.Executions {
		if e.Status == Pass && e.ExitCode != nil && *e.ExitCode == 0 {
			out[Key(e.Project, e.Kind, e.Command)] = e
		}
	}
	return out
}

// Key names one command of one project for PassesOn.
func Key(project, kind, command string) string { return project + "\x00" + kind + "\x00" + command }

// LastRun is the last recorded test run of a change, as status and delivery
// read it: a fact, never a gate.
type LastRun struct {
	Timestamp string   `json:"timestamp"`
	Passed    bool     `json:"passed"`
	Failed    []string `json:"failed"`
	Scope     string   `json:"scope"`
	Complete  bool     `json:"complete"`
	Dirty     bool     `json:"dirty"`
	Commit    string   `json:"commit"`
	Tree      string   `json:"tree"`
	Clean     bool     `json:"clean"`
	// RedBeforeGreen is a pass after an earlier failing run (RedBeforeGreen).
	RedBeforeGreen bool `json:"red_before_green"`
	// LightVerify is the change's newest light verify note, or nil.
	LightVerify *VerifyNote `json:"light_verify"`
	// Unit is a unit's run, the narrow kind (Record.Unit).
	Unit bool `json:"unit,omitempty"`
	// WorkingTree is the tree of the working state the run tested
	// (Record.WorkingTree): what delivery compares a committed tree with
	// when the run started on uncommitted work. Not in status's JSON.
	WorkingTree string `json:"-"`
	// AfterWorkingTree is WorkingTree read again when the run ended
	// (Record.AfterWorkingTree); a record an earlier binary wrote has none.
	// Not in status's JSON.
	AfterWorkingTree string `json:"-"`
}

// Empty is a complete run that holds no execution: it selected no project,
// as an affected run of a change of documents only does. Only a full run
// (`jhon executions run <change>`) is sure to hold one.
func (r *LastRun) Empty() bool { return r.Complete && !r.Passed && len(r.Failed) == 0 }

// ReadLastRun is the change's execution record read as its last test run,
// or nil when there is none or it cannot be parsed. Passed is a complete run
// of at least one execution in which every execution passed; Failed names
// the projects that did not pass (failed or timed out). A v1 record, which
// had neither `status` nor `complete`, is read by its exit codes. The error
// is a record that exists and cannot be read, or one that is JSON null:
// neither is a run to read.
func ReadLastRun(root, change string) (*LastRun, error) {
	abs := RecordFile(root, change)
	if _, err := os.Stat(abs); err != nil {
		return nil, nil
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var data any
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	if !json.Valid(b) || dec.Decode(&data) != nil {
		return nil, nil
	}
	if data == nil {
		return nil, errors.New("Cannot read properties of null (reading 'executions')")
	}
	obj, _ := data.(map[string]any)
	items, ok := obj["executions"].([]any)
	if !ok {
		return nil, nil
	}
	passed := func(e any) bool {
		m, _ := e.(map[string]any)
		if st, has := m["status"]; has {
			return st == Pass
		}
		n, isNum := m["exit_code"].(json.Number)
		return isNum && js.Number(string(n)) == 0
	}
	failed := []string{}
	for _, e := range items {
		if !passed(e) {
			m, _ := e.(map[string]any)
			p, has := m["project"]
			name := "undefined"
			if has {
				name = js.String(p)
			}
			if kind, ok := m["kind"].(string); ok && kind != "" {
				name += " (" + kind + ")"
			}
			failed = append(failed, name)
		}
	}
	str := func(k string) string {
		if v := obj[k]; v != nil {
			return js.String(v)
		}
		return ""
	}
	complete := obj["complete"] != false
	scope, isStr := obj["scope"].(string)
	if !isStr {
		scope = Full
	}
	passedAll := complete && len(items) > 0 && len(failed) == 0
	return &LastRun{
		Timestamp: str("timestamp"),
		Passed:    passedAll,
		Failed:    failed,
		Scope:     scope,
		Complete:  complete,
		Dirty:     obj["dirty"] == true,
		Commit:    str("candidate_commit"),
		Tree:      str("candidate_tree"),
		Clean:     obj["working_tree_clean"] == true,

		RedBeforeGreen: redBeforeGreen(passedAll, pastRuns(root, change)),
		LightVerify:    lastLightVerify(root, change),
		Unit:           obj["unit"] == true,
		WorkingTree:    str("working_tree"),

		AfterWorkingTree: str("after_working_tree"),
	}, nil
}

// WriteRecord writes a record atomically: a reader sees the previous record
// or this one, never half of it.
func WriteRecord(abs string, r *Record) error {
	s, err := js.Stringify(r)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", abs, os.Getpid())
	if err := os.WriteFile(tmp, []byte(s+"\n"), 0o666); err != nil {
		return err
	}
	return os.Rename(tmp, abs)
}

// ── processes ───────────────────────────────────────────────────────────────

// signalGroup sends sig to every process of group pgid; a group already gone is no error.
func signalGroup(pgid int, sig syscall.Signal) { _ = syscall.Kill(-pgid, sig) }

// groupAlive is whether any process of group pgid is still alive.
func groupAlive(pgid int) bool { return syscall.Kill(-pgid, 0) == nil }

// stopGroup stops a process group: SIGTERM, a wait of up to the grace period
// for every member to go, then SIGKILL whatever ignored it. It returns once
// the group is gone or killed, so an interrupted executor never exits with a
// project still running.
func stopGroup(pgid int) {
	signalGroup(pgid, syscall.SIGTERM)
	end := time.Now().Add(grace)
	for groupAlive(pgid) && time.Now().Before(end) {
		time.Sleep(50 * time.Millisecond)
	}
	if groupAlive(pgid) {
		signalGroup(pgid, syscall.SIGKILL)
	}
}

// Deadline is a project's deadline for minutes, as Node's setTimeout takes
// it: a delay below 1 ms or above 2^31-1 ms becomes 1 ms.
func Deadline(minutes float64) time.Duration {
	ms := minutes * 60_000
	if !(ms >= 1 && ms <= 1<<31-1) {
		ms = 1
	}
	return time.Duration(ms * float64(time.Millisecond))
}

// A Run is how one command runs: where its stdout and stderr go (stderr
// inherited when nil), its deadline, and the signals that interrupt it.
type Run struct {
	Stdout, Stderr *os.File
	Deadline       time.Duration
	Signals        <-chan os.Signal
	// Capture also receives stdout and stderr, when it is set.
	Capture io.Writer
}

// Outcome is how one command ended. Code is nil when the deadline expired and
// the group was killed; Interrupted is the signal that stopped it, after its
// group is gone; StartError is why sh could not start.
type Outcome struct {
	Code        *int
	Interrupted os.Signal
	StartError  string
}

// exitCode is a finished process's code; a signal is recorded as 128 + its
// number, the shell's convention, so it stays a non-zero integer.
func exitCode(st *os.ProcessState) int {
	if ws, ok := st.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return st.ExitCode()
}

// RunOne runs command through `sh -c` in cwd, in its own session and so its
// own process group, with stdin closed and stderr inherited. Past the
// deadline the group gets SIGTERM, then SIGKILL after the grace period, and
// whatever the command left in its group is killed once it exits. A signal
// on r.Signals stops the group and returns.
func RunOne(command, cwd string, r Run) Outcome {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = cwd
	// The environment as given: os/exec would otherwise add PWD for Dir.
	cmd.Env = os.Environ()
	cmd.Stdout, cmd.Stderr = r.Stdout, r.Stderr
	if r.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	if r.Capture != nil {
		var stdout io.Writer = io.Discard
		if r.Stdout != nil {
			stdout = r.Stdout
		}
		cmd.Stdout, cmd.Stderr = io.MultiWriter(stdout, r.Capture), io.MultiWriter(cmd.Stderr, r.Capture)
		// A process the command left behind holds the pipes; the wait stops there.
		cmd.WaitDelay = grace
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return Outcome{StartError: js.SpawnError("spawn", "sh", err)}
	}
	pgid := cmd.Process.Pid
	done := make(chan *os.ProcessState, 1)
	go func() {
		cmd.Wait()
		done <- cmd.ProcessState
	}()
	deadline := time.NewTimer(r.Deadline)
	defer deadline.Stop()
	var killer <-chan time.Time
	timedOut := false
	for {
		select {
		case st := <-done:
			if timedOut {
				signalGroup(pgid, syscall.SIGKILL)
				return Outcome{}
			}
			code := exitCode(st)
			return Outcome{Code: &code}
		case <-deadline.C:
			timedOut = true
			signalGroup(pgid, syscall.SIGTERM)
			killer = time.After(grace)
		case <-killer:
			signalGroup(pgid, syscall.SIGKILL)
		case sig := <-r.Signals:
			stopGroup(pgid)
			return Outcome{Interrupted: sig}
		}
	}
}
