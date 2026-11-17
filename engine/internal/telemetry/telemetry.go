// Package telemetry records what Jhon itself decides and runs, as events:
// one JSON line each, appended to a file per UTC day under
// ~/.local/share/jhon/telemetry/ (where the safety guard lets no agent
// write). Each line is an OpenTelemetry log record in the logs data model's
// terms, so an exporter can send it as one without reading it again:
//
//	{"time": "<RFC 3339, ms>", "name": "<event.name>", "severity": "INFO" | "WARN",
//	 "attributes": {…}}
//
// Attributes use OpenTelemetry's semantic conventions where one fits
// (session.id, gen_ai.agent.name, gen_ai.tool.name, process.exit.code,
// error.type) and jhon.* otherwise. Recording never fails its caller: a
// guard or a command runs the same whether the line was written or not.
// Telemetry is an opt-in integration no core package imports (T55): the cli
// records, and hands the hooks a recorder, only when integrations.telemetry
// is on (JHON_TELEMETRY=on or off overrides it). JHON_TELEMETRY=off records
// nothing. A day's file older than Keep days is removed when a new day's
// file starts.
package telemetry

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// Event is one record before it is written.
type Event struct {
	Time       time.Time
	Name       string
	Severity   string
	Attributes map[string]any
}

// Dir is where the records are: ~/.local/share/jhon/telemetry.
func Dir() string {
	return filepath.Join(project.Home(), ".local", "share", "jhon", "telemetry")
}

// Off is whether recording is turned off: JHON_TELEMETRY=off, or a test
// binary's run, which records only when a test sets JHON_TELEMETRY=on.
func Off() bool {
	v := os.Getenv("JHON_TELEMETRY")
	return v == "off" || (testing.Testing() && v != "on")
}

// now is the clock, a variable for the tests.
var now = time.Now

// Line is an event as its file holds it, newline-terminated.
func Line(e Event) []byte {
	severity := e.Severity
	if severity == "" {
		severity = "INFO"
	}
	attrs := e.Attributes
	if attrs == nil {
		attrs = map[string]any{}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(struct {
		Time       string         `json:"time"`
		Name       string         `json:"name"`
		Severity   string         `json:"severity"`
		Attributes map[string]any `json:"attributes"`
	}{e.Time.UTC().Format("2006-01-02T15:04:05.000Z"), e.Name, severity, attrs})
	return b.Bytes()
}

var mu sync.Mutex

// Record appends e to its day's file, timed now when e.Time is zero. One
// write of one line under O_APPEND: lines from concurrent processes (every
// hook is its own) do not interleave.
func Record(e Event) {
	if Off() || e.Name == "" {
		return
	}
	if e.Time.IsZero() {
		e.Time = now()
	}
	mu.Lock()
	defer mu.Unlock()
	dir := Dir()
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	file := filepath.Join(dir, e.Time.UTC().Format("2006-01-02")+".jsonl")
	if _, err := os.Stat(file); err != nil {
		prune(dir, e.Time)
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(Line(e))
}

// Keep is how many days of records are kept.
const Keep = 30

// prune removes the day files older than Keep days before t.
func prune(dir string, t time.Time) {
	oldest := t.UTC().AddDate(0, 0, -Keep).Format("2006-01-02")
	files, _ := filepath.Glob(filepath.Join(dir, "????-??-??.jsonl"))
	for _, f := range files {
		if strings.TrimSuffix(filepath.Base(f), ".jsonl") < oldest {
			_ = os.Remove(f)
		}
	}
}

// noted are the attributes the running command adds to its own event (Note).
var noted = map[string]any{}

// Note adds an attribute to the running command's event: what it decided
// (a delivery check's readiness, a test run's result), beside what every
// command's event says.
func Note(key string, value any) {
	mu.Lock()
	defer mu.Unlock()
	noted[key] = value
}

// Noted is what the running command noted, and forgets it.
func Noted() map[string]any {
	mu.Lock()
	defer mu.Unlock()
	out := noted
	noted = map[string]any{}
	return out
}
