package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const prefix = "This session started before the framework was last installed (3eb2e0b at "

func install(t *testing.T, home, at string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", "")
	os.MkdirAll(filepath.Dir(filepath.Join(home, InstallRecord)), 0o755)
	os.WriteFile(filepath.Join(home, InstallRecord), []byte(`{"installed_at":"`+at+`","commit":"3eb2e0b0000000000000000000000000000000aa"}`), 0o644)
}

func transcript(t *testing.T, home, start string) string {
	t.Helper()
	p := filepath.Join(home, "s1.jsonl")
	os.WriteFile(p, []byte(`{"type":"agent-setting","agentSetting":"jhon-orchestrator"}`+"\n"+`{"type":"user","timestamp":"`+start+`"}`+"\n"), 0o644)
	return p
}

func ms(s string) float64 {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return float64(t.UnixMilli())
}

func noClaude() ([]Agent, bool) { return nil, false }
func noAncestors() []int        { return nil }

const fullNote = prefix + "2026-09-25T01:19:40.000Z); restart it with `jhon start --resume <id>` to load the current contract and agents."

// T249: the install record lives in Jhon's state folder, which the sandbox
// keeps read-only, not in the cache every sandboxed command may write.
func TestInstallRecordPath(t *testing.T) {
	if InstallRecord != filepath.Join(".local", "state", "jhon", "install.json") {
		t.Errorf("InstallRecord = %q", InstallRecord)
	}
	if LegacyInstallRecord != filepath.Join(".cache", "jhon", "install.json") {
		t.Errorf("LegacyInstallRecord = %q", LegacyInstallRecord)
	}
	t.Setenv("XDG_STATE_HOME", "")
	if got := InstallPath("/home/u"); got != "/home/u/.local/state/jhon/install.json" {
		t.Errorf("InstallPath = %q", got)
	}
	if got := InstallShown("/home/u"); got != "~/.local/state/jhon/install.json" {
		t.Errorf("InstallShown = %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "/srv/state")
	if got := InstallPath("/home/u"); got != "/srv/state/jhon/install.json" {
		t.Errorf("InstallPath under XDG_STATE_HOME = %q", got)
	}
	if got := InstallShown("/home/u"); got != "/srv/state/jhon/install.json" {
		t.Errorf("InstallShown under XDG_STATE_HOME = %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "relative")
	if got := InstallPath("/home/u"); got != "/home/u/.local/state/jhon/install.json" {
		t.Errorf("InstallPath with a relative XDG_STATE_HOME = %q", got)
	}
}

// T249: a record at the legacy path is moved to the state folder once,
// outside the sandbox, source included; inside the sandbox it is never read,
// and once a record is in the state folder the legacy one is never read
// again, whatever a command writes there.
func TestALegacyInstallRecordIsMovedOnceAndOnlyOutsideTheSandbox(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	legacy := func(home, source string) {
		t.Helper()
		p := filepath.Join(home, LegacyInstallRecord)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(`{"installed_at":"2026-09-25T01:19:40.000Z","commit":"abc","source":"`+source+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inside := t.TempDir()
	legacy(inside, "/owner/checkout")
	t.Setenv("JHON_IN_SANDBOX", "1")
	if r, ok := ReadInstall(inside); ok {
		t.Errorf("inside the sandbox the legacy record was read: %+v", r)
	}
	if _, err := os.Stat(InstallPath(inside)); err == nil {
		t.Error("inside the sandbox the legacy record was moved")
	}
	t.Setenv("JHON_IN_SANDBOX", "")
	home := t.TempDir()
	legacy(home, "/owner/checkout")
	want := Install{InstalledAt: "2026-09-25T01:19:40.000Z", Commit: "abc", Source: "/owner/checkout"}
	if r, ok := ReadInstall(home); !ok || r != want {
		t.Fatalf("ReadInstall = %+v, %v; want the legacy record moved", r, ok)
	}
	if r, ok := ReadInstall(home); !ok || r != want {
		t.Fatalf("ReadInstall from the state folder = %+v, %v", r, ok)
	}
	legacy(home, "/attacker/folder")
	if r, ok := ReadInstall(home); !ok || r.Source != "/owner/checkout" {
		t.Errorf("a legacy record written after the move was read: %+v, %v", r, ok)
	}
	if err := WriteInstall(home, Install{InstalledAt: "2026-09-26T01:19:40.000Z", Commit: "def", Source: "/owner/checkout"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(home, InstallRecord)); !strings.Contains(got, `"commit": "def"`) {
		t.Errorf("WriteInstall wrote %s", got)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(home, LegacyInstallRecord))); !strings.Contains(got, "/attacker/folder") {
		t.Errorf("WriteInstall touched the legacy record: %s", got)
	}
	// A malformed legacy record moves nothing.
	bad := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(bad, LegacyInstallRecord)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, LegacyInstallRecord), []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadInstall(bad); ok {
		t.Error("a malformed legacy record was read")
	}
	if _, err := os.Stat(InstallPath(bad)); err == nil {
		t.Error("a malformed legacy record was moved")
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReadInstall(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	if _, ok := ReadInstall(home); ok {
		t.Fatal("no record read as one")
	}
	install(t, home, "2026-09-25T01:19:40.000Z")
	if r, ok := ReadInstall(home); !ok || r.InstalledAt != "2026-09-25T01:19:40.000Z" {
		t.Fatalf("ReadInstall = %v, %v", r, ok)
	}
	os.WriteFile(filepath.Join(home, InstallRecord), []byte(`{"installed_at":"2026-09-25T01:19:40.000Z","commit":"dirty"}`+"\n"), 0o644)
	if r, ok := ReadInstall(home); !ok || r != (Install{InstalledAt: "2026-09-25T01:19:40.000Z", Commit: "dirty"}) {
		t.Fatalf("ReadInstall = %v, %v", r, ok)
	}
	os.WriteFile(filepath.Join(home, InstallRecord), []byte(`{not json`), 0o644)
	if _, ok := ReadInstall(home); ok {
		t.Fatal("malformed JSON read as a record")
	}
	os.WriteFile(filepath.Join(home, InstallRecord), []byte(`{"installed_at":"yesterday","commit":"x"}`), 0o644)
	if _, ok := ReadInstall(home); ok {
		t.Fatal("an installed_at that is not a date is no record")
	}
}

func TestTranscriptStart(t *testing.T) {
	home := t.TempDir()
	if got := TranscriptStart(transcript(t, home, "2026-09-24T18:13:29.864Z")); got != "2026-09-24T18:13:29.864Z" {
		t.Fatalf("TranscriptStart = %q", got)
	}
	sessionTranscript(t, home, "-p", "s1", "2026-09-24T18:13:29.864Z", "2026-09-25T17:00:00.000Z", Orchestrator, time.Time{})
	if got := TranscriptStart(filepath.Join(home, ".claude", "projects", "-p", "s1.jsonl")); got != "2026-09-24T18:13:29.864Z" {
		t.Fatalf("TranscriptStart after bookkeeping lines = %q", got)
	}
	if got := TranscriptStart(filepath.Join(home, "missing.jsonl")); got != "" {
		t.Fatalf("missing transcript: %q", got)
	}
}

func TestNoteFromTranscript(t *testing.T) {
	home := t.TempDir()
	p := transcript(t, home, "2026-09-24T18:13:29.864Z")
	if got := note("", p, home, noClaude, noAncestors); got != "" {
		t.Fatalf("no install record: %q", got)
	}
	install(t, home, "2026-09-25T01:19:40.000Z")
	if got := note("", p, home, noClaude, noAncestors); got != fullNote {
		t.Fatalf("older session: %q", got)
	}
	if got := note("", "", home, noClaude, noAncestors); got != "" {
		t.Fatalf("no transcript: %q", got)
	}
	if got := note("", filepath.Join(home, "gone.jsonl"), home, noClaude, noAncestors); got != "" {
		t.Fatalf("a transcript that is gone: %q", got)
	}
	transcript(t, home, "2026-09-25T02:00:00.000Z")
	if got := note("", p, home, noClaude, noAncestors); got != "" {
		t.Fatalf("newer session: %q", got)
	}
}

func TestNotePrefersTheProcessStart(t *testing.T) {
	home := t.TempDir()
	install(t, home, "2026-09-25T01:19:40.000Z")
	p := transcript(t, home, "2026-09-24T18:13:29.864Z")
	resumed := func() ([]Agent, bool) {
		return []Agent{{SessionID: "s1", StartedAt: ms("2026-09-25T04:45:21.000Z"), HasStart: true}}, true
	}
	if got := note("s1", p, home, resumed, noAncestors); got != "" {
		t.Fatalf("resumed session: %q", got)
	}
	old := func() ([]Agent, bool) {
		return []Agent{{SessionID: "s1", Kind: "bg", Status: "busy", StartedAt: ms("2026-09-24T20:14:41.000Z"), HasStart: true}}, true
	}
	if got := note("s1", p, home, old, noAncestors); got != fullNote {
		t.Fatalf("an old process: %q", got)
	}
}

func TestNoteReadsTheCallingProcess(t *testing.T) {
	home := t.TempDir()
	install(t, home, "2026-09-30T19:16:57.250Z")
	p := transcript(t, home, "2026-09-24T18:13:29.864Z")
	both := func() ([]Agent, bool) {
		return []Agent{
			{SessionID: "s1", StartedAt: ms("2026-09-24T18:13:28.231Z"), HasStart: true},
			{SessionID: "s1", PID: 4242, HasPID: true, StartedAt: ms("2026-09-30T19:17:21.554Z"), HasStart: true},
		}, true
	}
	if got := note("s1", p, home, both, func() []int { return []int{4242, 1} }); got != "" {
		t.Fatalf("the caller is the resumed process: %q", got)
	}
	if got := note("s1", p, home, both, noAncestors); got != "" {
		t.Fatalf("no ancestry, the latest start counts: %q", got)
	}
	oldOnly := func() ([]Agent, bool) {
		return []Agent{
			{SessionID: "s1", PID: 7, HasPID: true, StartedAt: ms("2026-09-24T18:13:28.231Z"), HasStart: true},
			{SessionID: "s1", PID: 4242, HasPID: true, StartedAt: ms("2026-09-30T19:17:21.554Z"), HasStart: true},
		}, true
	}
	if got := note("s1", p, home, oldOnly, func() []int { return []int{7, 1} }); !strings.HasPrefix(got, prefix+"2026-09-30T19:16:57.250Z)") {
		t.Fatalf("the caller is the old process: %q", got)
	}
}
