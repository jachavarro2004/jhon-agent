package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// odd/tasks/jhon-studio.md L11-L12: Unset drops a key's line so the file
// reads it at its default again, keeping every other byte; a section left
// with no key goes too. An older binary, which does not know the key, then
// reads the file again.
func TestUnsetDropsTheKeyAndKeepsTheRest(t *testing.T) {
	path := write(t, "# mine\nschema = 1\n\n[integrations]\n# the videos\nstudio = true\ntelemetry = true\n")
	removed, err := Unset(path, "integrations.studio")
	if err != nil || !removed {
		t.Fatalf("Unset = %v, %v", removed, err)
	}
	b, _ := os.ReadFile(path)
	if got := string(b); got != "# mine\nschema = 1\n\n[integrations]\n# the videos\ntelemetry = true\n" {
		t.Errorf("file:\n%s", got)
	}
	vals, err := Load(path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "integrations.studio"); v.Value != false || v.Source != SourceDefault {
		t.Errorf("integrations.studio = %+v", v)
	}
	if removed, err := Unset(path, "integrations.studio"); err != nil || removed {
		t.Errorf("a second Unset = %v, %v", removed, err)
	}
	if b2, _ := os.ReadFile(path); string(b2) != string(b) {
		t.Errorf("a second Unset changed the file:\n%s", b2)
	}
}

func TestUnsetDropsASectionItEmpties(t *testing.T) {
	path := write(t, "schema = 1\n\n[shell.appearance]\nascii = true\n\n[integrations]\nstudio = true\n")
	if removed, err := Unset(path, "integrations.studio"); err != nil || !removed {
		t.Fatalf("Unset = %v, %v", removed, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "schema = 1\n\n[shell.appearance]\nascii = true\n" {
		t.Errorf("file:\n%s", b)
	}
}

func TestUnsetWritesNothingWhereThereIsNothingToDrop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	if removed, err := Unset(path, "integrations.studio"); err != nil || removed {
		t.Errorf("no file: %v, %v", removed, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Unset created the file: %v", err)
	}
	var invalid *InvalidError
	if _, err := Unset(path, "integrations.nope"); !errors.As(err, &invalid) || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("an unknown key: %v", err)
	}
}
