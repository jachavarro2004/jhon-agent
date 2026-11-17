package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

var now = time.Date(2026, 9, 25, 18, 0, 0, 0, time.UTC)

func ago(hours float64) string {
	return isodate.Format(now.Add(-time.Duration(hours * float64(time.Hour))))
}

// sessionTranscript writes a transcript as Claude Code writes it:
// bookkeeping lines without a timestamp first; agent is the agent-setting.
func sessionTranscript(t *testing.T, home, proj, id, first, last, agent string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", proj)
	os.MkdirAll(dir, 0o755)
	var lines []string
	add := func(o map[string]any) { b, _ := json.Marshal(o); lines = append(lines, string(b)) }
	add(map[string]any{"type": "custom-title", "customTitle": "T", "sessionId": id})
	if agent != "" {
		add(map[string]any{"type": "agent-setting", "agentSetting": agent, "sessionId": id})
	}
	add(map[string]any{"type": "user", "timestamp": first, "sessionId": id, "cwd": "/from-transcript"})
	add(map[string]any{"type": "assistant", "timestamp": last, "sessionId": id})
	p := filepath.Join(dir, id+".jsonl")
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	if !mtime.IsZero() {
		os.Chtimes(p, mtime, mtime)
	}
}

func listing(agents ...Agent) func() ([]Agent, error) {
	return func() ([]Agent, error) { return agents, nil }
}

func listed(id, cwd, kind string, start string) Agent {
	a := Agent{SessionID: id, Cwd: &cwd, Kind: kind, Status: "idle"}
	if start != "" {
		a.StartedAt, a.HasStart = ms(start), true
	}
	return a
}

func ids(rows []*Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.SessionID)
	}
	return out
}

func TestSessionsListedOrchestratorBeforeTheInstallIsStale(t *testing.T) {
	home := t.TempDir()
	install(t, home, "2026-09-25T01:19:40.000Z")
	sessionTranscript(t, home, "-a", "old-orch", ago(30), ago(0.1), Orchestrator, time.Time{})
	sessionTranscript(t, home, "-b", "new-orch", ago(10), ago(0.1), Orchestrator, time.Time{})
	sessionTranscript(t, home, "-c", "old-plain", ago(30), ago(0.1), "", time.Time{})
	r := Sessions(Sources{Home: home, Now: now, Claude: listing(
		listed("old-orch", "/a", "bg", ago(30)), listed("new-orch", "/b", "interactive", ago(10)), listed("old-plain", "/c", "interactive", ago(30)))})
	if r.Source != "claude-agents" || r.Note != nil || r.Installed == nil || *r.Installed != (Installed{"2026-09-25T01:19:40.000Z", "3eb2e0b0000000000000000000000000000000aa"}) {
		t.Fatalf("report = %+v", r)
	}
	if got := ids(r.Stale); !slices.Equal(got, []string{"old-orch"}) {
		t.Fatalf("stale = %v", got)
	}
	row := r.Sessions[0]
	if *row.AgentSetting != Orchestrator || *row.StartSource != "process" || *row.StartedAt != ago(30) || *row.Cwd != "/a" || *row.LastActivity != ago(0.1) {
		t.Fatalf("row = %+v", row)
	}
	for _, row := range r.Sessions {
		if row.SessionID == "old-plain" && row.Stale {
			t.Error("a session that is not an orchestrator's is not stale")
		}
	}
}

func TestSessionsProcessStartWinsOverTheTranscript(t *testing.T) {
	home := t.TempDir()
	install(t, home, "2026-09-25T01:19:40.000Z")
	sessionTranscript(t, home, "-a", "resumed", ago(30), ago(0.1), Orchestrator, time.Time{})
	if r := Sessions(Sources{Home: home, Now: now, Claude: listing(listed("resumed", "/a", "interactive", ago(2)))}); len(r.Stale) != 0 {
		t.Fatalf("stale = %v", ids(r.Stale))
	}
}

func TestSessionsWithoutStartedAtReadTheTranscript(t *testing.T) {
	home := t.TempDir()
	install(t, home, "2026-09-25T01:19:40.000Z")
	sessionTranscript(t, home, "-a", "s", ago(30), ago(0.1), Orchestrator, time.Time{})
	r := Sessions(Sources{Home: home, Now: now, Claude: listing(Agent{SessionID: "s"})})
	if len(r.Stale) != 1 || *r.Stale[0].StartSource != "transcript" || r.Stale[0].Kind != "unknown" || *r.Stale[0].Cwd != "/from-transcript" {
		t.Fatalf("stale = %+v", r.Stale)
	}
}

func TestSessionsWithoutClaudeReadRecentTranscripts(t *testing.T) {
	home := t.TempDir()
	install(t, home, "2026-09-25T01:19:40.000Z")
	recent, _ := isodate.Parse(ago(1))
	idle, _ := isodate.Parse(ago(13))
	sessionTranscript(t, home, "-a", "recent-orch", ago(30), ago(1), Orchestrator, recent)
	sessionTranscript(t, home, "-b", "idle-orch", ago(40), ago(13), Orchestrator, idle)
	r := Sessions(Sources{Home: home, Now: now, Claude: func() ([]Agent, error) { return nil, errors.New("claude: command not found") }})
	want := "`claude agents --json` is unavailable (claude: command not found); reading transcripts only: a session counts as active when its transcript recorded activity within the last 12h"
	if r.Source != "transcripts" || r.Note == nil || *r.Note != want {
		t.Fatalf("source %q, note %v", r.Source, r.Note)
	}
	if got := ids(r.Sessions); !slices.Equal(got, []string{"recent-orch"}) {
		t.Fatalf("sessions = %v; a session idle beyond the window is not active", got)
	}
	if s := r.Stale; len(s) != 1 || *s[0].StartSource != "transcript" || s[0].Kind != "unknown" {
		t.Fatalf("stale = %+v", s)
	}
}

func TestSessionsWithoutAnInstallRecord(t *testing.T) {
	home := t.TempDir()
	sessionTranscript(t, home, "-a", "s", ago(30), ago(0.1), Orchestrator, time.Time{})
	r := Sessions(Sources{Home: home, Now: now, Claude: listing(listed("s", "/a", "bg", ago(30)))})
	if r.Installed != nil || len(r.Stale) != 0 || r.Note == nil || !strings.Contains(*r.Note, "no install record") {
		t.Fatalf("report = %+v", r)
	}
}

func TestSessionsJSONKeysAndHuman(t *testing.T) {
	home := t.TempDir()
	install(t, home, "2026-09-25T01:19:40.000Z")
	sessionTranscript(t, home, "-a", "old-orch", ago(30), ago(0.1), Orchestrator, time.Time{})
	r := Sessions(Sources{Home: home, Now: now, Claude: listing(listed("old-orch", "/a", "background", ago(30)))})
	b, _ := json.Marshal(r)
	for _, keys := range []string{
		`{"installed":{"installed_at":"2026-09-25T01:19:40.000Z","commit":"3eb2e0b0000000000000000000000000000000aa"},"source":"claude-agents","note":null,"window_hours":12,"sessions":[{"session_id":"old-orch","cwd":"/a","kind":"background","status":"idle","started_at":`,
		`"start_source":"process","last_activity":`, `"agent_setting":"jhon-orchestrator","transcript":`, `"stale":true}],"stale":[{"session_id":"old-orch"`,
	} {
		if !strings.Contains(string(b), keys) {
			t.Fatalf("JSON lacks %s:\n%s", keys, b)
		}
	}
	want := "installed 3eb2e0b at 2026-09-25T01:19:40.000Z\nsource: claude agents --json\n" +
		"  stale  old-orch  background  idle  /a  started " + ago(30) + " (process)\n" +
		"1 orchestrator session(s) started before the last install; resume each with `jhon start --resume <id>` to load the current contract and agents"
	if got := Human(r); got != want {
		t.Fatalf("Human =\n%s\nwant\n%s", got, want)
	}
	if got := Human(Report{Source: "transcripts", Sessions: []*Row{}, Stale: []*Row{}}); got != "installed: unknown\nsource: transcripts\nno stale orchestrator session" {
		t.Fatalf("Human = %q", got)
	}
}

func TestLocaleCompare(t *testing.T) {
	ids := []string{"b", "B", "a-2", "a", "_x", "10", "A"}
	slices.SortStableFunc(ids, js.LocaleCompare)
	if got := strings.Join(ids, " "); got != "_x 10 a A a-2 b B" {
		t.Fatalf("sorted = %s", got)
	}
}

// TestNoInstallRecordNoteByRole (T10i, L51): the maintainer's checkout is
// told to sync; a user, who has only the release, never is: no record means
// no runtime is installed yet, which setup chooses.
func TestNoInstallRecordNoteByRole(t *testing.T) {
	for _, tc := range []struct {
		checkout bool
		want     string
	}{
		{true, "no install record (~/.cache/jhon/install.json); run `jhon sync` to create it"},
		{false, "no install record (~/.cache/jhon/install.json): no runtime is installed yet; run `jhon setup` to choose one"},
	} {
		r := Sessions(Sources{Home: t.TempDir(), Now: now, Claude: listing(), Checkout: func() bool { return tc.checkout }})
		if r.Note == nil || *r.Note != tc.want {
			t.Errorf("checkout %v: note %v, want %q", tc.checkout, r.Note, tc.want)
		}
	}
}

// TestHasCheckout is the role signal: JHON_ROOT (or the binary's root)
// holding .git, or a source checkout at JHON_SOURCE, the source `jhon sync`
// recorded, or ~/dev/jhon.
func TestHasCheckout(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JHON_SOURCE", "")
	t.Setenv("JHON_ROOT", root)
	if hasCheckout() {
		t.Error("a release root with no source anywhere is a checkout")
	}
	recorded := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cache", "jhon"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, InstallRecord), []byte(`{"installed_at":"2026-11-16T10:00:00.000Z","commit":"abc","source":"`+recorded+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if hasCheckout() {
		t.Error("a recorded source without the binary's source is a checkout")
	}
	if err := os.MkdirAll(filepath.Join(recorded, "engine", "cmd", "jhon"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recorded, "engine", "cmd", "jhon", "main.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasCheckout() {
		t.Error("the recorded source holding the source is not a checkout")
	}
	if err := os.Remove(filepath.Join(home, InstallRecord)); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(home, "dev", "jhon")
	if err := os.MkdirAll(filepath.Join(src, "engine", "cmd", "jhon"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "engine", "cmd", "jhon", "main.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasCheckout() {
		t.Error("~/dev/jhon holding the source is not a checkout")
	}
}
