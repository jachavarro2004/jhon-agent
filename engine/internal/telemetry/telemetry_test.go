package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordAppendsOneLinePerEventToItsUTCDay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("JHON_TELEMETRY", "on")
	at := time.Date(2026, 10, 30, 23, 59, 59, 500e6, time.UTC)
	Record(Event{Time: at, Name: "jhon.hook", Attributes: map[string]any{"jhon.decision": "deny", "a<b": "x&y"}, Severity: "WARN"})
	Record(Event{Time: at, Name: "jhon.command"})
	Record(Event{Time: at}) // no name: nothing
	b, err := os.ReadFile(filepath.Join(Dir(), "2026-10-30.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) != 3 || lines[2] != "" {
		t.Fatalf("%q", b)
	}
	if want := `{"time":"2026-10-30T23:59:59.500Z","name":"jhon.hook","severity":"WARN","attributes":{"a<b":"x&y","jhon.decision":"deny"}}`; lines[0] != want {
		t.Errorf("got  %s\nwant %s", lines[0], want)
	}
	var e map[string]any
	if json.Unmarshal([]byte(lines[1]), &e) != nil || e["severity"] != "INFO" || len(e["attributes"].(map[string]any)) != 0 {
		t.Errorf("defaults: %s", lines[1])
	}
}

func TestOffRecordsNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("JHON_TELEMETRY", "off")
	Record(Event{Name: "jhon.hook"})
	if _, err := os.Stat(Dir()); err == nil {
		t.Error("JHON_TELEMETRY=off wrote the directory")
	}
}

func TestNotedIsTheRunningCommandsAndForgotten(t *testing.T) {
	Note("jhon.change", "c1")
	Note("jhon.delivery.ready", true)
	if got := Noted(); len(got) != 2 || got["jhon.change"] != "c1" || got["jhon.delivery.ready"] != true {
		t.Errorf("%v", got)
	}
	if got := Noted(); len(got) != 0 {
		t.Errorf("not forgotten: %v", got)
	}
}

func TestANewDayRemovesTheDaysPastKeep(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("JHON_TELEMETRY", "on")
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"2026-09-29", "2026-09-30", "2026-10-29"} {
		if err := os.WriteFile(filepath.Join(Dir(), d+".jsonl"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	Record(Event{Time: time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC), Name: "jhon.hook"})
	files, _ := filepath.Glob(filepath.Join(Dir(), "*.jsonl"))
	var days []string
	for _, f := range files {
		days = append(days, filepath.Base(f))
	}
	if strings.Join(days, " ") != "2026-09-30.jsonl 2026-10-29.jsonl 2026-10-30.jsonl" {
		t.Errorf("%v", days)
	}
}
