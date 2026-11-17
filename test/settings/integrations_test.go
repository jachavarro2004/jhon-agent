package settings

// The opt-in integrations (T55, S37/S38): telemetry and the desktop palette
// act only when the settings turn them on, and the owner's settings, as the
// migration writes them (L199), keep today's behaviour.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// ownerSettings are the settings the migration writes on the owner's
// machine: every opt-in integration on, the desktop palette Ryoku's.
const ownerSettings = `schema = 1

[integrations]
telemetry = true

[integrations.theme]
palette = "ryoku"
`

// withSettings writes text as h's settings.toml.
func withSettings(t *testing.T, h home, text string) {
	t.Helper()
	support.Write(t, filepath.Dir(h.file()), map[string]string{filepath.Base(h.file()): text})
}

// telemetryLines are the events recorded under h, every day's file.
func telemetryLines(t *testing.T, h home) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(h.dir, ".local", "share", "jhon", "telemetry", "*.jsonl"))
	var lines []string
	for _, f := range files {
		for _, l := range strings.Split(strings.TrimSpace(support.Read(t, f)), "\n") {
			if l != "" {
				lines = append(lines, l)
			}
		}
	}
	return lines
}

// recordSomething runs a guard and a recorded command under h's environment.
func recordSomething(t *testing.T, h home) {
	t.Helper()
	support.Hook(t, h.env, "safety-guard", map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}, "session_id": "s1"})
	support.Jhon(t, h.dir, h.env, "risk", "--help-not-a-flag")
}

func TestTelemetryRecordsOnlyWhenTheSettingIsOn(t *testing.T) {
	for _, c := range []struct {
		name, settings, env string
		records             bool
	}{
		{"no file: off", "", "JHON_TELEMETRY=", false},
		{"telemetry = false", "[integrations]\ntelemetry = false\n", "JHON_TELEMETRY=", false},
		{"the owner's settings", ownerSettings, "JHON_TELEMETRY=", true},
		{"on, JHON_TELEMETRY=off wins", ownerSettings, "JHON_TELEMETRY=off", false},
		{"off, JHON_TELEMETRY=on wins", "", "JHON_TELEMETRY=on", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHome(t, c.env)
			if c.settings != "" {
				withSettings(t, h, c.settings)
			}
			recordSomething(t, h)
			lines := telemetryLines(t, h)
			if !c.records {
				if len(lines) != 0 {
					t.Fatalf("recorded with telemetry off:\n%s", strings.Join(lines, "\n"))
				}
				if _, err := os.Stat(filepath.Join(h.dir, ".local", "share", "jhon", "telemetry")); !os.IsNotExist(err) {
					t.Errorf("the telemetry dir was created: %v", err)
				}
				return
			}
			var hooks, commands int
			for _, l := range lines {
				var e struct{ Name string }
				if err := json.Unmarshal([]byte(l), &e); err != nil {
					t.Fatalf("%v: %s", err, l)
				}
				switch e.Name {
				case "jhon.hook":
					hooks++
				case "jhon.command":
					commands++
				}
			}
			if hooks != 1 || commands != 1 {
				t.Errorf("want one hook and one command event:\n%s", strings.Join(lines, "\n"))
			}
		})
	}
}

// paletteJSON is `jhon palette --json` under h.
func paletteJSON(t *testing.T, h home) map[string]any {
	t.Helper()
	r := support.Jhon(t, h.dir, h.env, "palette", "--json")
	if r.Code != 0 {
		t.Fatalf("palette --json: %d\n%s", r.Code, r.Out())
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatalf("%v\n%s", err, r.Stdout)
	}
	return out
}

func TestTheDesktopPaletteIsFollowedOnlyWhenTheSettingSaysSo(t *testing.T) {
	desktop := `{"primary":"#aabbcc"}`
	for _, c := range []struct {
		name, settings string
		follows        bool
	}{
		{"no file: rose ink's own values", "", false},
		{"palette = rose-ink", "[integrations.theme]\npalette = \"rose-ink\"\n", false},
		{"follow_desktop = false, from before T15.7", "[integrations.theme]\nfollow_desktop = false\nsource = \"ryoku\"\n", false},
		{"the owner's settings", ownerSettings, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHome(t)
			support.Write(t, filepath.Join(h.dir, ".cache", "ryoku"), map[string]string{"colors.json": desktop})
			if c.settings != "" {
				withSettings(t, h, c.settings)
			}
			p := paletteJSON(t, h)
			want, accent := false, "#e7bbc8"
			if c.follows {
				want, accent = true, "#aabbcc"
			}
			if p["desktop"] != want || p["accent"] != accent {
				t.Errorf("desktop %v accent %v, want %v %s", p["desktop"], p["accent"], want, accent)
			}
		})
	}
}

// commandEvents are the jhon.command values of h's recorded command events.
func commandEvents(t *testing.T, h home) []string {
	t.Helper()
	var names []string
	for _, l := range telemetryLines(t, h) {
		var e struct {
			Name       string
			Attributes map[string]any
		}
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("%v: %s", err, l)
		}
		if e.Name == "jhon.command" {
			names = append(names, e.Attributes["jhon.command"].(string))
		}
	}
	return names
}

// A first sync migrates an old shell's settings, which may turn telemetry
// on; whether it records is decided after it runs, so it records itself
// (T61, L208). With telemetry off at the end it records nothing.
func TestAFirstSyncRecordsItsOwnCommand(t *testing.T) {
	for _, c := range []struct {
		name, env string
		want      []string
	}{
		{"telemetry migrated on", "JHON_TELEMETRY=", []string{"sync"}},
		{"telemetry off", "JHON_TELEMETRY=off", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHome(t, c.env)
			shellHome(t, h)
			support.Jhon(t, h.dir, h.env, "sync") // only the migration and the record matter
			if got := commandEvents(t, h); !slices.Equal(got, c.want) {
				t.Errorf("command events after a first sync = %v, want %v", got, c.want)
			}
		})
	}
}
