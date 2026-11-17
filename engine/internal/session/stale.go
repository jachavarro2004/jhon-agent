// Package session tells whether a session predates the framework's last
// install, so a refusal can say so.
package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// InstallRecord is the install record, relative to HOME.
var InstallRecord = filepath.Join(".cache", "jhon", "install.json")

// Install is the install record: when the framework was last installed, its
// commit, and the source checkout `jhon sync` installed from ("" when no
// checkout was ever recorded).
type Install struct{ InstalledAt, Commit, Source string }

// ReadInstall reads the install record under home; ok is false when it is absent or malformed.
func ReadInstall(home string) (Install, bool) {
	data, err := os.ReadFile(filepath.Join(home, InstallRecord))
	if err != nil {
		return Install{}, false
	}
	var r map[string]any
	if json.Unmarshal(data, &r) != nil {
		return Install{}, false
	}
	at, _ := r["installed_at"].(string)
	commit, _ := r["commit"].(string)
	source, _ := r["source"].(string)
	if !isodate.IsDate(at) || commit == "" {
		return Install{}, false
	}
	return Install{InstalledAt: at, Commit: commit, Source: source}, true
}

// WriteInstall writes r as the install record under home: the one writer of
// it, for the render's install and `jhon sync`'s source.
func WriteInstall(home string, r Install) error {
	p := filepath.Join(home, InstallRecord)
	if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
		return err
	}
	text, err := js.Stringify(struct {
		InstalledAt string `json:"installed_at"`
		Commit      string `json:"commit"`
		Source      string `json:"source,omitempty"`
	}{r.InstalledAt, r.Commit, r.Source})
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte(js.WellFormed(text+"\n")), 0o666)
}

const headBytes = 1 << 20

// TranscriptStart is the first timestamp in a transcript's first megabyte, or "".
func TranscriptStart(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, headBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return ""
	}
	lines := strings.Split(string(buf[:n]), "\n")
	if n == headBytes {
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		if !strings.Contains(line, `"timestamp"`) {
			continue
		}
		var o map[string]any
		if json.Unmarshal([]byte(line), &o) != nil {
			continue
		}
		if t, ok := o["timestamp"].(string); ok && isodate.IsDate(t) {
			return t
		}
	}
	return ""
}

// Agent is one session `claude agents --json` lists. Cwd is set when the
// listing gives a string; Kind and Status are as listed, nil when absent.
type Agent struct {
	SessionID    string
	PID          float64
	HasPID       bool
	StartedAt    float64
	HasStart     bool
	Cwd          *string
	Kind, Status any
}

// ClaudeAgents lists the live sessions; ok is false when claude is absent, fails, or prints no list.
func ClaudeAgents() ([]Agent, bool) {
	agents, err := ListAgents()
	return agents, err == nil
}

// ListAgents is claudeAgents: the live sessions, or why claude could not
// list them (absent, failed, timed out, or printed something not a list).
// It only lists; it never changes a session.
func ListAgents() ([]Agent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "agents", "--json")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return nil, errors.New("claude: spawnSync claude ETIMEDOUT")
	case errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist):
		return nil, errors.New("claude: command not found")
	case cmd.ProcessState == nil && err != nil:
		return nil, fmt.Errorf("claude: %v", err)
	case err != nil:
		status := "null" // killed by a signal
		if c := cmd.ProcessState.ExitCode(); c >= 0 {
			status = strconv.Itoa(c)
		}
		first, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		if first == "" {
			first = "no output"
		}
		return nil, fmt.Errorf("claude agents --json exited %s: %s", status, first)
	}
	var parsed any
	if json.Unmarshal(out.Bytes(), &parsed) != nil {
		return nil, errors.New("claude agents --json did not print JSON")
	}
	list, ok := parsed.([]any)
	if !ok {
		return nil, errors.New("claude agents --json did not print a list")
	}
	agents := []Agent{}
	for _, x := range list {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		id, ok := m["sessionId"].(string)
		if !ok {
			continue
		}
		a := Agent{SessionID: id, Kind: m["kind"], Status: m["status"]}
		a.PID, a.HasPID = m["pid"].(float64)
		a.StartedAt, a.HasStart = m["startedAt"].(float64)
		if c, ok := m["cwd"].(string); ok {
			a.Cwd = &c
		}
		agents = append(agents, a)
	}
	return agents, nil
}

// ProcessStart is an agent's start as ISO 8601, or "".
func ProcessStart(a *Agent) string {
	if a == nil || !a.HasStart || math.IsInf(a.StartedAt, 0) || math.IsNaN(a.StartedAt) || a.StartedAt <= 0 {
		return ""
	}
	return isodate.Format(time.UnixMilli(int64(a.StartedAt)))
}

// Ancestors are the pids above this process, nearest first, read from /proc.
func Ancestors() []int {
	var out []int
	pid := os.Getppid()
	for i := 0; i < 32 && pid > 1; i++ {
		out = append(out, pid)
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			break
		}
		s := string(stat)
		fields := strings.Fields(s[strings.LastIndex(s, ")")+1:])
		if len(fields) < 2 {
			break
		}
		if pid, err = strconv.Atoi(fields[1]); err != nil {
			break
		}
	}
	return out
}

// callerEntry is the listed entry of the process that made the call: the
// one whose pid is an ancestor of this process, else the latest start.
func callerEntry(entries []Agent, ancestors func() []int) *Agent {
	if len(entries) == 0 {
		return nil
	}
	if len(entries) == 1 {
		return &entries[0]
	}
	up := map[int]bool{}
	for _, p := range ancestors() {
		up[p] = true
	}
	for i := range entries {
		if entries[i].HasPID && up[int(entries[i].PID)] && float64(int(entries[i].PID)) == entries[i].PID {
			return &entries[i]
		}
	}
	best := &entries[0]
	for i := range entries[1:] {
		if entries[i+1].StartedAt > best.StartedAt {
			best = &entries[i+1]
		}
	}
	return best
}

var sha = lazyre.New(`^[0-9a-f]{40}$`)

// ShortCommit is a sha shortened to seven characters, any other value as is.
func ShortCommit(c string) string {
	if sha.MatchString(c) {
		return c[:7]
	}
	return c
}

// Note is the line a guard appends to a refusal when the call's session
// started before the last install, or "" when it did not or cannot be told.
func Note(sessionID, transcript, home string) string {
	return note(sessionID, transcript, home, ClaudeAgents, Ancestors)
}

func note(id, path, home string, claude func() ([]Agent, bool), ancestors func() []int) string {
	record, ok := ReadInstall(home)
	if !ok || (id == "" && path == "") {
		return ""
	}
	start := ""
	if id != "" {
		if listed, ok := claude(); ok {
			var same []Agent
			for _, a := range listed {
				if a.SessionID == id {
					same = append(same, a)
				}
			}
			start = ProcessStart(callerEntry(same, ancestors))
		}
	}
	if start == "" {
		start = TranscriptStart(path)
	}
	began, ok := isodate.Parse(start)
	installed, _ := isodate.Parse(record.InstalledAt)
	if start == "" || !ok || !began.Before(installed) {
		return ""
	}
	return fmt.Sprintf("This session started before the framework was last installed (%s at %s); ", ShortCommit(record.Commit), record.InstalledAt) +
		"restart it with `jhon start --resume <id>` to load the current contract and agents."
}
