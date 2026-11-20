package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// oldShell is a pre-v0.1 shell config home, as the shell wrote it: the
// migration no longer reads it (T29 E6, L329).
var oldShell = map[string]string{
	"card-style.json":    `{"schema":"jhon-shell.card-style/v1","style":"float"}`,
	"animations.json":    `{"schema":"jhon-shell.animations/v1","policy":"potato"}`,
	"notifications.json": `{"schema":"jhon-shell.notifications/v1","enabled":true}`,
}

// oldFiles is an old machine: the shell's JSON files beside profiles.json,
// and where settings.toml goes.
func oldFiles(t *testing.T) (Sources, string, string) {
	t.Helper()
	dir := t.TempDir()
	shell := filepath.Join(dir, "jhon-shell")
	if err := os.MkdirAll(shell, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range oldShell {
		if err := os.WriteFile(filepath.Join(shell, name), []byte(body+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	profiles := filepath.Join(dir, "profiles.json")
	if err := os.WriteFile(profiles, []byte(`{"active":"fast","profiles":{"fast":{}}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return Sources{Profiles: profiles}, filepath.Join(dir, "config", "jhon", "settings.toml"), shell
}

var at = time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC)

func TestMigrateMapsTheProfileAndTelemetry(t *testing.T) {
	src, path, shell := oldFiles(t)
	if !Pending(path, src) {
		t.Fatal("Pending = false with profiles.json and no settings.toml")
	}
	rep, err := Migrate(path, src, Evidence{Telemetry: true}, at)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Done {
		t.Fatalf("not done: %#v", rep)
	}
	vals, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("the migrated file does not load: %v\n%s", err, read(t, path))
	}
	for key, w := range map[string]any{"pi.profile": "fast", "integrations.telemetry": true} {
		if v := valueOf(t, vals, key); v.Value != w || v.Source != SourceFile {
			t.Errorf("%s = %#v (%s), want %#v from the file", key, v.Value, v.Source, w)
		}
	}
	// The old shell's files are not read: their keys keep their defaults,
	// and the palette is never written.
	for _, key := range []string{"shell.appearance.animations", "shell.appearance.cards", "integrations.theme.palette"} {
		if v := valueOf(t, vals, key); v.Source != SourceDefault {
			t.Errorf("%s = %#v (%s), want the default", key, v.Value, v.Source)
		}
	}
	want := filepath.Join(filepath.Dir(path), "settings-backup-20261106T093000Z")
	if rep.Backup != want {
		t.Errorf("backup = %s, want %s", rep.Backup, want)
	}
	if got := read(t, filepath.Join(rep.Backup, "profiles.json")); !strings.Contains(got, `"active":"fast"`) {
		t.Errorf("profiles backup = %q", got)
	}
	if _, err := os.Stat(filepath.Join(rep.Backup, "jhon-shell")); !os.IsNotExist(err) {
		t.Error("the old shell files were backed up")
	}
	for name, body := range oldShell {
		if got := read(t, filepath.Join(shell, name)); got != body+"\n" {
			t.Errorf("original %s changed: %q", name, got)
		}
	}
	if len(rep.Skipped) != 0 {
		t.Errorf("skipped = %v", rep.Skipped)
	}
}

func TestMigrateTwiceDoesNothing(t *testing.T) {
	src, path, _ := oldFiles(t)
	if _, err := Migrate(path, src, Evidence{}, at); err != nil {
		t.Fatal(err)
	}
	before := read(t, path)
	if Pending(path, src) {
		t.Error("Pending after a migration")
	}
	rep, err := Migrate(path, src, Evidence{Telemetry: true}, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Done || rep.Backup != "" {
		t.Errorf("second run = %#v", rep)
	}
	if read(t, path) != before {
		t.Error("the second run rewrote settings.toml")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "settings-backup-20261106T103000Z")); !os.IsNotExist(err) {
		t.Error("the second run made a backup")
	}
}

func TestMigrateWithoutEvidenceKeepsTheDefaults(t *testing.T) {
	src, path, _ := oldFiles(t)
	if _, err := Migrate(path, src, Evidence{}, at); err != nil {
		t.Fatal(err)
	}
	vals, err := Load(path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"integrations.telemetry", "integrations.theme.palette"} {
		if v := valueOf(t, vals, key); v.Source != SourceDefault {
			t.Errorf("%s = %#v (%s), want the default", key, v.Value, v.Source)
		}
	}
}

func TestMigrateKeepsAnUnusableFileOutOfTheSettings(t *testing.T) {
	src, path, _ := oldFiles(t)
	if err := os.WriteFile(src.Profiles, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := Migrate(path, src, Evidence{}, at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(rep.Skipped, "\n"), "profiles.json") {
		t.Errorf("skipped = %v", rep.Skipped)
	}
	if _, err := Load(path, noEnv); err != nil {
		t.Fatal(err)
	}
}

func TestNothingPendingWithoutOldFiles(t *testing.T) {
	dir := t.TempDir()
	src := Sources{Profiles: filepath.Join(dir, "none.json")}
	path := filepath.Join(dir, "settings.toml")
	if Pending(path, src) {
		t.Error("Pending with no old files")
	}
	rep, err := Migrate(path, src, Evidence{Telemetry: true}, at)
	if err != nil || rep.Done {
		t.Errorf("rep = %#v, err = %v", rep, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a migration with nothing to migrate wrote settings.toml")
	}
}
