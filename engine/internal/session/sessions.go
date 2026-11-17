package session

// The report of `jhon sessions` (docket AI1): the live
// orchestrator sessions that started before the framework was last
// installed. Such a session keeps the prompt and agent list it loaded at
// start while the guards and sub-agents it runs are the installed ones. It is
// stale when it is active (listed by `claude agents --json`, or, without
// claude, its transcript recorded activity within the window), it started
// before the install record's installed_at (the process start claude
// reports, else its transcript's first timestamp), and its transcript's
// agent-setting is a main Jhon agent. Read-only: it never resumes, stops, or
// messages a session.

import (
	"encoding/json"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// Orchestrator is the agent setting a session must have to be reported.
const Orchestrator = "jhon-orchestrator"

// MainAgents are the main Jhon agents: a session run as one keeps the prompt it loaded at start.
var MainAgents = []string{Orchestrator}

// ActiveWindowHours is how recent a transcript's last activity must be for
// a session to count as active without claude.
const ActiveWindowHours = 12

// Sources are where the report reads; a zero field is the real source:
// HOME, now, `claude agents --json`, and ActiveWindowHours.
type Sources struct {
	Home        string
	Now         time.Time
	Claude      func() ([]Agent, error)
	WindowHours float64
	// Checkout is whether this runs with the maintainer's source checkout
	// (hasCheckout when nil): it decides what the no-install-record note
	// tells to do (L51).
	Checkout func() bool
}

// hasCheckout is whether the source checkout is here, as release.Locate
// finds it: the runtime root (JHON_ROOT, else the parent of the binary's
// directory) holding .git, or the source (JHON_SOURCE, else the checkout
// `jhon sync` recorded, else the legacy ~/dev/jhon or ~/.agents/jhon)
// holding the binary's source. A user has only the release.
func hasCheckout() bool {
	root := os.Getenv("JHON_ROOT")
	if root == "" {
		if exe, err := os.Executable(); err == nil {
			if real, err := filepath.EvalSymlinks(exe); err == nil {
				root = filepath.Dir(filepath.Dir(real))
			}
		}
	}
	if root != "" && project.Exists(filepath.Join(root, ".git")) {
		return true
	}
	home := project.Home()
	rec, _ := ReadInstall(home)
	for _, src := range []string{os.Getenv("JHON_SOURCE"), rec.Source, filepath.Join(home, "dev", "jhon"), filepath.Join(home, ".agents", "jhon")} {
		if src != "" && project.Exists(filepath.Join(src, "engine", "cmd", "jhon", "main.go")) {
			return true
		}
	}
	return false
}

// Row is one active session, its keys in report order.
type Row struct {
	SessionID    string  `json:"session_id"`
	Cwd          *string `json:"cwd"`
	Kind         any     `json:"kind"`
	Status       any     `json:"status"`
	StartedAt    *string `json:"started_at"`
	StartSource  *string `json:"start_source"` // process, transcript, or null
	LastActivity *string `json:"last_activity"`
	AgentSetting *string `json:"agent_setting"`
	Transcript   *string `json:"transcript"`
	Stale        bool    `json:"stale"`
}

// Installed is the install record as the report shows it.
type Installed struct {
	InstalledAt string `json:"installed_at"`
	Commit      string `json:"commit"`
}

// Report is the `jhon sessions --json` document.
type Report struct {
	Installed   *Installed `json:"installed"`
	Source      string     `json:"source"` // claude-agents or transcripts
	Note        *string    `json:"note"`
	WindowHours float64    `json:"window_hours"`
	Sessions    []*Row     `json:"sessions"`
	Stale       []*Row     `json:"stale"`
}

// projectDirs are the transcript directories under ~/.claude/projects.
func projectDirs(home string) []string {
	root := filepath.Join(home, ".claude", "projects")
	entries, _ := os.ReadDir(root)
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	return out
}

// findTranscript is the main transcript of a session, in any project directory, or "".
func findTranscript(home, id string) string {
	for _, dir := range projectDirs(home) {
		if p := filepath.Join(dir, id+".jsonl"); project.Exists(p) {
			return p
		}
	}
	return ""
}

type facts struct{ agentSetting, last, cwd *string }

// transcriptFacts are what a transcript records: the last agent-setting,
// the last timestamp, and the first cwd.
func transcriptFacts(path string) facts {
	var f facts
	data, err := os.ReadFile(path)
	if err != nil {
		return f
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, `"agent-setting"`) && !strings.Contains(line, `"timestamp"`) && (f.cwd != nil || !strings.Contains(line, `"cwd"`)) {
			continue
		}
		var v any
		if json.Unmarshal([]byte(line), &v) != nil {
			continue
		}
		o, _ := v.(map[string]any)
		if s, ok := o["agentSetting"].(string); ok && o["type"] == "agent-setting" {
			f.agentSetting = &s
		}
		if t, ok := o["timestamp"].(string); ok {
			if _, ok := isodate.Parse(t); ok {
				f.last = &t
			}
		}
		if c, ok := o["cwd"].(string); ok && c != "" && f.cwd == nil {
			f.cwd = &c
		}
	}
	return f
}

type transcriptFile struct{ id, path string }

// recentTranscripts are the transcripts modified since since (ms): the
// candidates for an active session when claude cannot list them, by id.
func recentTranscripts(home string, since float64) []transcriptFile {
	var out []transcriptFile
	for _, dir := range projectDirs(home) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			path := filepath.Join(dir, name)
			fi, err := os.Stat(path)
			if err != nil || float64(fi.ModTime().UnixNano())/1e6 < since {
				continue
			}
			out = append(out, transcriptFile{strings.TrimSuffix(name, ".jsonl"), path})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return js.LocaleCompare(out[i].id, out[j].id) < 0 })
	return out
}

// millis is Date.parse of an ISO 8601 date-time, in milliseconds.
func millis(s string) (float64, bool) {
	t, ok := isodate.Parse(s)
	return float64(t.UnixMilli()), ok
}

func str(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Sessions is sessionsReport: the active sessions and the stale ones among them.
func Sessions(src Sources) Report {
	home, now, claude, windowHours := src.Home, src.Now, src.Claude, src.WindowHours
	if home == "" {
		home = project.Home()
	}
	if now.IsZero() {
		now = time.Now()
	}
	if claude == nil {
		claude = ListAgents
	}
	if windowHours == 0 {
		windowHours = ActiveWindowHours
	}
	var installed *Installed
	if rec, ok := ReadInstall(home); ok {
		installed = &Installed{rec.InstalledAt, rec.Commit}
	}
	listed, listErr := claude()
	var notes []string
	rows := []*Row{}

	row := func(id, path string, cwd *string, kind, status any, start, startSource string) *Row {
		var f facts
		if path != "" {
			f = transcriptFacts(path)
		}
		if start == "" && path != "" {
			start, startSource = TranscriptStart(path), "transcript"
		}
		if cwd == nil {
			cwd = f.cwd
		}
		if kind == nil {
			kind = "unknown"
		}
		if status == nil {
			status = "unknown"
		}
		r := &Row{SessionID: id, Cwd: cwd, Kind: kind, Status: status, LastActivity: f.last, AgentSetting: f.agentSetting, Transcript: str(path)}
		if start != "" {
			r.StartedAt, r.StartSource = &start, &startSource
		}
		return r
	}

	if listErr == nil {
		for _, s := range listed {
			rows = append(rows, row(s.SessionID, findTranscript(home, s.SessionID), s.Cwd, s.Kind, s.Status, ProcessStart(&s), "process"))
		}
	} else {
		hours := strconv.FormatFloat(windowHours, 'f', -1, 64)
		notes = append(notes, fmt.Sprintf("`claude agents --json` is unavailable (%v); reading transcripts only: a session counts as active when its transcript recorded activity within the last %sh", listErr, hours))
		since := float64(now.UnixMilli()) - windowHours*3600_000
		for _, t := range recentTranscripts(home, since) {
			r := row(t.id, t.path, nil, nil, nil, "", "")
			if r.LastActivity == nil {
				continue
			}
			if last, ok := millis(*r.LastActivity); ok && last >= since {
				rows = append(rows, r)
			}
		}
	}

	if installed == nil {
		checkout := src.Checkout
		if checkout == nil {
			checkout = hasCheckout
		}
		if checkout() {
			notes = append(notes, "no install record (~/.cache/jhon/install.json); run `jhon sync` to create it")
		} else {
			// An install writes the record when it installs a runtime: none is.
			notes = append(notes, "no install record (~/.cache/jhon/install.json): no runtime is installed yet; run `jhon setup` to choose one")
		}
	}
	stale := []*Row{}
	for _, r := range rows {
		if installed == nil || r.AgentSetting == nil || !slices.Contains(MainAgents, *r.AgentSetting) || r.StartedAt == nil {
			continue
		}
		began, ok1 := millis(*r.StartedAt)
		at, ok2 := millis(installed.InstalledAt)
		r.Stale = ok1 && ok2 && began < at
		if r.Stale {
			stale = append(stale, r)
		}
	}
	rep := Report{Installed: installed, Source: "claude-agents", WindowHours: windowHours, Sessions: rows, Stale: stale}
	if listErr != nil {
		rep.Source = "transcripts"
	}
	if len(notes) > 0 {
		n := strings.Join(notes, "; ")
		rep.Note = &n
	}
	return rep
}

// Human is the report for a reader.
func Human(r Report) string {
	var out []string
	if r.Installed != nil {
		out = append(out, fmt.Sprintf("installed %s at %s", ShortCommit(r.Installed.Commit), r.Installed.InstalledAt))
	} else {
		out = append(out, "installed: unknown")
	}
	if r.Source == "claude-agents" {
		out = append(out, "source: claude agents --json")
	} else {
		out = append(out, "source: transcripts")
	}
	if r.Note != nil {
		out = append(out, "note: "+*r.Note)
	}
	for _, s := range r.Stale {
		cwd := "?"
		if s.Cwd != nil {
			cwd = *s.Cwd
		}
		out = append(out, fmt.Sprintf("  stale  %s  %s  %s  %s  started %s (%s)", s.SessionID, js.String(s.Kind), js.String(s.Status), cwd, *s.StartedAt, *s.StartSource))
	}
	if len(r.Stale) > 0 {
		out = append(out, fmt.Sprintf("%d orchestrator session(s) started before the last install; resume each with `jhon start --resume <id>` to load the current contract and agents", len(r.Stale)))
	} else {
		out = append(out, "no stale orchestrator session")
	}
	return strings.Join(out, "\n")
}
