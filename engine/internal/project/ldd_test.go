package project

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lddswitch"
)

// One function says whether a project uses LDD (T57a, G3): no other
// non-test engine file names the config's path as a literal of its own.
func TestLddConfigNamedOnce(t *testing.T) {
	named := regexp.MustCompile(`"openspec",\s*"config\.yaml"|"openspec/config\.yaml"`)
	engine := filepath.Join("..", "..")
	var found []string
	err := filepath.WalkDir(engine, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		if filepath.Base(p) == "ldd.go" && filepath.Base(filepath.Dir(p)) == "project" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if named.MatchString(line) || (strings.Contains(line, `"openspec"`) && strings.Contains(line, "config.yaml")) {
				found = append(found, p+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Errorf("openspec/config.yaml named outside the layout (use project.UsesLdd / ReadLddConfig / LddConfigFile): %s", strings.Join(found, ", "))
	}
}

func TestUsesLdd(t *testing.T) {
	dir := t.TempDir()
	if UsesLdd(dir) || UsesLdd("") {
		t.Fatal("no config, no LDD")
	}
	if err := os.MkdirAll(filepath.Join(dir, "openspec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LddConfigFile(dir), []byte("delivery: change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !UsesLdd(dir) {
		t.Fatal("a config is LDD")
	}
	if s, err := ReadLddConfig(dir); err != nil || s != "delivery: change\n" {
		t.Fatalf("ReadLddConfig: %q %v", s, err)
	}
	if _, err := ReadLddConfig(""); err == nil {
		t.Fatal("no project has no config")
	}
}

// LDD is always on (T72, L555): an old `jhon ldd off` record changes
// nothing, and is to be told once.
func TestUsesLddIgnoresAnOldSwitchOff(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "openspec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LddConfigFile(dir), []byte("delivery: change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if OldSwitchOffUntold(dir) {
		t.Fatal("no record, yet one to tell")
	}
	os.MkdirAll(lddswitch.Dir(), 0o700)
	if err := os.WriteFile(lddswitch.File(dir), []byte(`{"ldd":"off","root":"`+dir+`","since":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !UsesLdd(dir) {
		t.Fatal("an old switch-off still turns LDD off")
	}
	if s, err := ReadLddConfig(dir); err != nil || s != "delivery: change\n" {
		t.Fatalf("ReadLddConfig: %q %v", s, err)
	}
	if !OldSwitchOffUntold(dir) {
		t.Fatal("an old switch-off is not to be told")
	}
	if err := TellOldSwitchOff(dir); err != nil || OldSwitchOffUntold(dir) {
		t.Fatalf("told, still to tell: %v", err)
	}
}
