package integrations

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeCLI puts a program called bin on PATH (alone with /usr/bin and /bin)
// whose `integration --json` prints integration and exits integrationExit,
// whose `doctor --json` prints doctor and exits doctorExit, and which logs
// every call to the returned file.
func fakeCLI(t *testing.T, bin, integration string, integrationExit int, doctor string, doctorExit int) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\ncase \"$*\" in\n" +
		"  'integration --json') cat <<'EOF'\n" + integration + "\nEOF\n  exit " + itoa(integrationExit) + " ;;\n" +
		"  'doctor --json') cat <<'EOF'\n" + doctor + "\nEOF\n  exit " + itoa(doctorExit) + " ;;\n" +
		"esac\nexit 2\n"
	if err := os.WriteFile(filepath.Join(dir, bin), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	return log
}

func itoa(n int) string { return string(rune('0' + n)) }

var cliFixture = Manifest{Name: "clifixture", Kind: "cli", Doctor: Doctor{Binary: "clifixture-bin"}}

// odd/tasks/jhon-studio.md S3: a cli integration's program names what a
// runtime loads, `<binary> integration --json`.
func TestAskIntegrationReadsTheProgramsAnswer(t *testing.T) {
	fakeCLI(t, "clifixture-bin", `{"version":"0.1.0","plugin_dir":"/opt/p","skill":"/opt/p/skills/video/SKILL.md"}`, 0, "", 0)
	a, err := AskIntegration(cliFixture)
	if err != nil || a != (Answer{Version: "0.1.0", PluginDir: "/opt/p", Skill: "/opt/p/skills/video/SKILL.md"}) {
		t.Errorf("AskIntegration = %+v, %v", a, err)
	}
}

func TestAskIntegrationSaysWhyItHasNoAnswer(t *testing.T) {
	for _, c := range []struct {
		what, out string
		exit      int
		want      string
	}{
		{"a failure", `{"plugin_dir":"/opt/p"}`, 1, "clifixture-bin exited 1"},
		{"not JSON", "nope", 0, "clifixture-bin integration --json: not the JSON {version, plugin_dir, skill}"},
		{"no plugin_dir", `{"version":"0.1.0"}`, 0, "clifixture-bin integration --json: names no plugin_dir"},
	} {
		fakeCLI(t, "clifixture-bin", c.out, c.exit, "", 0)
		if _, err := AskIntegration(cliFixture); err == nil || err.Error() != c.want {
			t.Errorf("%s: %v, want %q", c.what, err, c.want)
		}
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := AskIntegration(cliFixture); err == nil || err.Error() != "clifixture-bin is not on PATH" {
		t.Errorf("missing: %v", err)
	}
}

// `<binary> doctor --json` is the program's own checks, each with its fix;
// it exits 1 when one fails and still answers.
func TestAskDoctorReadsEveryCheck(t *testing.T) {
	const report = `{"ok":false,"checks":[{"id":"node","ok":true,"detail":"node 22.11.0","fix":""},{"id":"ffmpeg","ok":false,"detail":"ffmpeg is not on PATH","fix":"install ffmpeg"}]}`
	log := fakeCLI(t, "clifixture-bin", "", 0, report, 1)
	checks, err := AskDoctor(cliFixture)
	want := []Check{{ID: "node", OK: true, Detail: "node 22.11.0"}, {ID: "ffmpeg", Detail: "ffmpeg is not on PATH", Fix: "install ffmpeg"}}
	if err != nil || !slices.Equal(checks, want) {
		t.Errorf("AskDoctor = %+v, %v", checks, err)
	}
	if b, _ := os.ReadFile(log); strings.TrimSpace(string(b)) != "doctor --json" {
		t.Errorf("calls %q", b)
	}
	// L11-L12: an exit 1 with no output is that, not a malformed report.
	fakeCLI(t, "clifixture-bin", "", 0, "", 1)
	if _, err := AskDoctor(cliFixture); err == nil || err.Error() != "clifixture-bin exited 1" {
		t.Errorf("exit 1, no output: %v", err)
	}
	fakeCLI(t, "clifixture-bin", "", 0, "{}", 1)
	if _, err := AskDoctor(cliFixture); err == nil || err.Error() != "clifixture-bin doctor --json: not the JSON {ok, checks: [{id, ok, detail, fix}]}" {
		t.Errorf("no checks: %v", err)
	}
}
