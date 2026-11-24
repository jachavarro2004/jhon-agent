package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
)

// odd/tasks/jhon-studio.md S3: jhon doctor checks jhon-studio only while
// integrations.studio is on: its version among the versions, then its own
// checks (`jhon-studio doctor --json`), each failing one with its fix.
// Nothing of it is a failure of Jhon's, and off nothing of it is shown.

// studioDoctor runs doctor's versions and cli sections under the settings
// text, with a fake jhon-studio on PATH printing report for `doctor --json`
// (exit 1, as it does when a check fails) unless report is "".
func studioDoctor(t *testing.T, text, report string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	must(t, os.WriteFile(path, []byte(text), 0o600))
	t.Setenv("JHON_SETTINGS", path)
	if report != "" {
		must(t, os.WriteFile(filepath.Join(dir, "jhon-studio"), []byte("#!/bin/sh\n[ \"$*\" = 'doctor --json' ] || exit 2\ncat <<'EOF'\n"+report+"\nEOF\nexit 1\n"), 0o755))
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	saved := probeVersion
	t.Cleanup(func() { probeVersion = saved })
	probeVersion = func(command string) (string, bool) {
		if command == "jhon-studio" && report != "" {
			return "jhon-studio 0.0.9\n", true
		}
		return "", false
	}
	var b strings.Builder
	o := &release.Out{W: &b, E: &b}
	d := doctor{o: o}
	d.versions()
	d.cliIntegrations()
	return b.String(), o.Failed
}

func TestDoctorShowsNothingOfAnOffCLIIntegration(t *testing.T) {
	got, failed := studioDoctor(t, "schema = 1\n", `{"ok":true,"checks":[]}`)
	if strings.Contains(got, "studio") || failed != 0 {
		t.Errorf("off: %d failed\n%s", failed, got)
	}
}

func TestDoctorChecksAnOnCLIIntegrationWithItsFixes(t *testing.T) {
	const report = `{"ok":false,"checks":[{"id":"node","ok":true,"detail":"node 22.11.0","fix":""},{"id":"ffmpeg","ok":false,"detail":"ffmpeg is not on PATH","fix":"install ffmpeg with your package manager"}]}`
	got, failed := studioDoctor(t, "schema = 1\n\n[integrations]\nstudio = true\n", report)
	m, _ := integrations.Get("studio")
	for _, want := range []string{
		"  warn  jhon-studio 0.0.9 is installed; Jhon pins " + m.Doctor.Version + "\n",
		"\njhon-studio\n  ok    node: node 22.11.0\n  warn  ffmpeg: ffmpeg is not on PATH — fix: install ffmpeg with your package manager\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	if failed != 0 {
		t.Errorf("a failing check of jhon-studio failed Jhon's doctor: %d", failed)
	}
}

func TestDoctorSaysHowToInstallAnOnCLIIntegrationNotOnPath(t *testing.T) {
	got, failed := studioDoctor(t, "schema = 1\n\n[integrations]\nstudio = true\n", "")
	m, _ := integrations.Get("studio")
	for _, want := range []string{
		"  jhon-studio is not installed (Jhon pins " + m.Doctor.Version + ")\n",
		"\njhon-studio\n  warn  jhon-studio is not on PATH (integrations.studio is on) — " + m.InstallCommand("manual", m.Doctor.Version) + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	if failed != 0 {
		t.Errorf("failed %d", failed)
	}
}

func TestDoctorSaysWhenAnOnCLIIntegrationGivesNoChecks(t *testing.T) {
	got, _ := studioDoctor(t, "schema = 1\n\n[integrations]\nstudio = true\n", "nope")
	if want := "\njhon-studio\n  warn  jhon-studio doctor --json: not the JSON {ok, checks: [{id, ok, detail, fix}]}\n"; !strings.Contains(got, want) {
		t.Errorf("lacks %q:\n%s", want, got)
	}
}
