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
	probeVersion = func(command string) (string, error) {
		if command == "jhon-studio" && report != "" {
			return "jhon-studio 0.0.9\n", nil
		}
		return "", errNotInstalled
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
		"  ok    jhon-studio 0.0.9 (tested with " + m.Doctor.Version + ")\n",
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
		"  jhon-studio is not installed (tested with " + m.Doctor.Version + ")\n",
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

// L11-L12: a jhon-studio on PATH that exits 1 with no output is said as
// that, in the versions and in its section, never "not installed" or a
// malformed report.
func TestDoctorSaysAnOnCLIIntegrationThatExits1Silently(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	must(t, os.WriteFile(path, []byte("schema = 1\n\n[integrations]\nstudio = true\n"), 0o600))
	t.Setenv("JHON_SETTINGS", path)
	must(t, os.WriteFile(filepath.Join(dir, "jhon-studio"), []byte("#!/bin/sh\nexit 1\n"), 0o755))
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	var b strings.Builder
	o := &release.Out{W: &b, E: &b}
	d := doctor{o: o}
	d.versions()
	d.cliIntegrations()
	got := b.String()
	m, _ := integrations.Get("studio")
	for _, want := range []string{
		"  jhon-studio exited 1 (tested with " + m.Doctor.Version + ")\n",
		"\njhon-studio\n  warn  jhon-studio exited 1\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	for _, bad := range []string{"jhon-studio is not installed", "not the JSON"} {
		if strings.Contains(got, bad) {
			t.Errorf("says %q:\n%s", bad, got)
		}
	}
	if o.Failed != 0 {
		t.Errorf("failed %d", o.Failed)
	}
}

// T237 (odd/tasks/studio-integration-v1.md S5): a kind cli tool's manifest
// version is the one Jhon was tested with, never a pin or a requirement:
// what it needs of jhon is its own requires_jhon.
func TestDoctorShowsACLIToolsManifestVersionAsTestedWith(t *testing.T) {
	m, _ := integrations.Get("studio")
	for _, c := range []struct{ out, want string }{
		{"jhon-studio " + m.Doctor.Version + "\n", "  ok    jhon-studio " + m.Doctor.Version + " (tested with " + m.Doctor.Version + ")\n"},
		{"jhon-studio 9.1.0\n", "  ok    jhon-studio 9.1.0 (tested with " + m.Doctor.Version + ")\n"},
		{"dev build\n", "  jhon-studio printed no version (tested with " + m.Doctor.Version + ")\n"},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.toml")
		must(t, os.WriteFile(path, []byte("schema = 1\n\n[integrations]\nstudio = true\n"), 0o600))
		t.Setenv("JHON_SETTINGS", path)
		saved := probeVersion
		probeVersion = func(command string) (string, error) {
			if command == "jhon-studio" {
				return c.out, nil
			}
			return "", errNotInstalled
		}
		var b strings.Builder
		o := &release.Out{W: &b, E: &b}
		doctor{o: o}.versions()
		probeVersion = saved
		got := b.String()
		if !strings.Contains(got, c.want) {
			t.Errorf("lacks %q:\n%s", c.want, got)
		}
		for _, bad := range []string{"jhon-studio " + m.Doctor.Version + " (pinned)", "Jhon pins " + m.Doctor.Version, "warn  jhon-studio"} {
			if strings.Contains(got, bad) {
				t.Errorf("says %q:\n%s", bad, got)
			}
		}
		if o.Failed != 0 {
			t.Errorf("failed %d", o.Failed)
		}
	}
}

// studioAnswering runs doctor's cli section with a fake jhon-studio whose
// `integration --json` prints answer and whose `doctor --json` passes one
// check; the calls it got are returned too.
func studioAnswering(t *testing.T, answer string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	must(t, os.WriteFile(path, []byte("schema = 1\n\n[integrations]\nstudio = true\n"), 0o600))
	t.Setenv("JHON_SETTINGS", path)
	log := filepath.Join(dir, "calls")
	must(t, os.WriteFile(filepath.Join(dir, "jhon-studio"), []byte("#!/bin/sh\necho \"$*\" >> '"+log+"'\ncase \"$*\" in\n"+
		"  'integration --json') cat <<'EOF'\n"+answer+"\nEOF\n ;;\n"+
		"  'doctor --json') echo '{\"ok\":true,\"checks\":[{\"id\":\"node\",\"ok\":true,\"detail\":\"node 22.11.0\",\"fix\":\"\"}]}' ;;\n"+
		"  *) exit 2 ;;\nesac\n"), 0o755))
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	var b strings.Builder
	o := &release.Out{W: &b, E: &b}
	doctor{o: o}.cliIntegrations()
	calls, _ := os.ReadFile(log)
	return b.String(), string(calls), o.Failed
}

// A jhon older than the tool's minimum, or a contract jhon does not speak:
// one warning with the fix, the tool off for this run (its own checks are
// not asked), and Jhon's doctor does not fail.
func TestDoctorSaysWhenAToolNeedsANewerJhon(t *testing.T) {
	jhonIs(t, "0.5.0-rc.1")
	got, calls, failed := studioAnswering(t, `{"version":"0.2.0","plugin_dir":"/p","skill":"/p/SKILL.md","requires_jhon":">=0.6.0","contract":"jhon.integration/v1"}`)
	want := "\njhon-studio\n  warn  jhon-studio 0.2.0 requires jhon >=0.6.0, and this is jhon 0.5.0-rc.1: jhon-studio is off for this run; update jhon (jhon update), or install a jhon-studio version that supports jhon 0.5.0-rc.1\n"
	if got != want || calls != "integration --json\n" || failed != 0 {
		t.Errorf("a newer minimum: failed %d, calls %q\n got %q\nwant %q", failed, calls, got, want)
	}
	got, _, failed = studioAnswering(t, `{"version":"1.0.0","contract":"jhon.integration/v2"}`)
	want = "\njhon-studio\n  warn  jhon-studio 1.0.0 speaks jhon.integration/v2, and this jhon 0.5.0-rc.1 speaks jhon.integration/v1: jhon-studio is off for this run; update jhon (jhon update), or install a jhon-studio version that speaks jhon.integration/v1\n"
	if got != want || failed != 0 {
		t.Errorf("another contract: failed %d\n got %q\nwant %q", failed, got, want)
	}
}

// A tool that fits says what it requires and this jhon; an older one that
// declares no minimum is noted as such; on a development build the
// minimum is said not checked. Its own checks follow, as before.
func TestDoctorSaysHowAToolFitsThisJhon(t *testing.T) {
	const checks = "  ok    node: node 22.11.0\n"
	for _, c := range []struct{ jhon, answer, line string }{
		{"0.5.0", `{"version":"0.2.0","plugin_dir":"/p","requires_jhon":">=0.5.0-rc.1","contract":"jhon.integration/v1"}`,
			"  ok    jhon-studio 0.2.0 requires jhon >=0.5.0-rc.1; this is jhon 0.5.0\n"},
		{"0.5.0", `{"version":"0.1.0","plugin_dir":"/p"}`,
			"  jhon-studio 0.1.0 declares no minimum jhon (no requires_jhon in jhon-studio integration --json): taken as compatible\n"},
		{"0.0.0-dev+abc1234", `{"version":"0.2.0","plugin_dir":"/p","requires_jhon":">=9.0.0"}`,
			"  jhon-studio 0.2.0 requires jhon >=9.0.0: not checked on a development build of jhon (0.0.0-dev+abc1234)\n"},
	} {
		jhonIs(t, c.jhon)
		got, calls, failed := studioAnswering(t, c.answer)
		if want := "\njhon-studio\n" + c.line + checks; got != want || calls != "integration --json\ndoctor --json\n" || failed != 0 {
			t.Errorf("jhon %s: failed %d, calls %q\n got %q\nwant %q", c.jhon, failed, calls, got, want)
		}
	}
}
