package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
	"github.com/jachavarro2004/jhon-agent/engine/internal/setupui"
)

func TestRefHead(t *testing.T) {
	listing := "aaa\tHEAD\nbbb\trefs/heads/main\nccc\trefs/heads/main2"
	if got := refHead(listing, "refs/heads/main"); got != "bbb" {
		t.Errorf("refHead(main) = %q; want bbb", got)
	}
	if got := refHead(listing, "refs/heads/"); got != "" {
		t.Errorf("refHead on a detached HEAD = %q; want none", got)
	}
}

func TestCountNamed(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"a/SKILL.md", "b/c/SKILL.md", "SKILL.md"} {
		must(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755))
		must(t, os.WriteFile(filepath.Join(dir, p), nil, 0o644))
	}
	// find does not follow a link to a directory.
	must(t, os.Symlink(filepath.Join(dir, "a"), filepath.Join(dir, "linked")))
	if got := countNamed(dir, "SKILL.md", 2); got != 2 {
		t.Errorf("countNamed(depth 2) = %d; want 2", got)
	}
	if got := countNamed(dir, "SKILL.md", -1); got != 3 {
		t.Errorf("countNamed(unbounded) = %d; want 3", got)
	}
	if got := countNamed(filepath.Join(dir, "none"), "*.md", -1); got != 0 {
		t.Errorf("countNamed(missing) = %d; want 0", got)
	}
}

func TestEnclosingProject(t *testing.T) {
	t.Setenv("GIT_DIR", "")
	os.Unsetenv("GIT_DIR")
	dir := t.TempDir()
	deep := filepath.Join(dir, "spec", "x")
	must(t, os.MkdirAll(filepath.Join(dir, "openspec"), 0o755))
	must(t, os.MkdirAll(deep, 0o755))
	if got := enclosingProject(deep); got != dir {
		t.Errorf("enclosingProject(under openspec's parent) = %s; want %s", got, dir)
	}
	bare := t.TempDir()
	if got := enclosingProject(bare); got != "" {
		t.Errorf("enclosingProject(scratch) = %s; want none", got)
	}
	must(t, os.WriteFile(filepath.Join(bare, "go.mod"), nil, 0o644))
	if got := enclosingProject(bare); got != bare {
		t.Errorf("enclosingProject(manifest) = %s; want %s", got, bare)
	}
}

func TestGlobalMCP(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".claude.json")
	for text, want := range map[string]bool{
		`{"mcpServers":{"jhon":{"command":"jhon"}}}`: true,
		`{"mcpServers":{"jhon":0}}`:                  false,
		`{"mcpServers":{"other":{}}}`:                false,
		`[1]`:                                        false,
		`{ not json`:                                 false,
	} {
		must(t, os.WriteFile(file, []byte(text), 0o644))
		if got := globalMCP(file); got != want {
			t.Errorf("globalMCP(%s) = %v; want %v", text, got, want)
		}
	}
}

func TestWithEnvReplaces(t *testing.T) {
	got := withEnv([]string{"A=1", "AB=2", "B=3"}, "A=9")
	if !slices.Equal(got, []string{"AB=2", "B=3", "A=9"}) {
		t.Errorf("withEnv = %q", got)
	}
}

func TestStartRefusesItsOwnFlags(t *testing.T) {
	for _, a := range []string{"--agent", "--agent=x", "--plugin-dir", "--plugin-dir=/x"} {
		if code, err := runStart([]string{a}); code != 2 || err != nil {
			t.Errorf("start %s = %d, %v; want 2", a, code, err)
		}
	}
}

func TestDispatch(t *testing.T) {
	for _, args := range [][]string{{"no-such-command"}, {"no-such-command", "x"}} {
		if code, _ := Run(args); code != 1 {
			t.Errorf("Run(%q) = %d; want 1, an unknown command", args, code)
		}
	}
	// A help flag after any command, even an unknown one, prints the usage.
	for _, args := range [][]string{nil, {""}, {"help"}, {"-h"}, {"--help"}, {"sync", "--help"}, {"no-such-command", "-h"}} {
		if code, err := Run(args); code != 0 || err != nil {
			t.Errorf("Run(%q) = %d, %v; want the usage and 0", args, code, err)
		}
	}
	if code, _ := Run([]string{"sync", "--dry-run"}); code != 2 {
		t.Errorf("Run(sync --dry-run) = %d; want 2", code)
	}
	if code, _ := Run([]string{"render"}); code != 1 {
		t.Errorf("Run(render) = %d; want 1", code)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// T50g (F7): setup installs render's pins, and doctor warns, never fails,
// when an installed version differs from its pin.
func TestSetupInstallsTheVersionsTable(t *testing.T) {
	want := setupui.Pins{ClaudeCode: render.ClaudeCodeVersion, Engram: render.EngramVersion, CodeGraph: render.CodeGraphVersion}
	if got := setupPins(); got != want {
		t.Errorf("setupPins = %+v, want %+v", got, want)
	}
}

// A --version output with no x.y.z is a note, never a failure (T50g verify).
func TestDoctorVersionNoteForUnparsableOutput(t *testing.T) {
	saved := probeVersion
	defer func() { probeVersion = saved }()
	probeVersion = func(string) (string, bool) { return "dev build", true }
	var b strings.Builder
	o := &release.Out{W: &b, E: &b}
	doctor{o: o}.versions()
	if got := b.String(); !strings.Contains(got, "claude printed no version (Jhon pins "+render.ClaudeCodeVersion+")") {
		t.Errorf("no note for an unparsable version:\n%s", got)
	}
	if o.Failed != 0 {
		t.Errorf("an unparsable version failed doctor: %d", o.Failed)
	}
}

func TestDoctorWarnsWhenAnInstalledVersionDiffersFromItsPin(t *testing.T) {
	saved := probeVersion
	defer func() { probeVersion = saved }()
	probeVersion = func(command string) (string, bool) {
		switch command {
		case "claude":
			return render.ClaudeCodeVersion + " (Claude Code)", true
		case "engram":
			return "engram 0.0.1", true
		}
		return "", false
	}
	var b strings.Builder
	o := &release.Out{W: &b, E: &b}
	doctor{o: o}.versions()
	got := b.String()
	for _, want := range []string{
		"ok    claude " + render.ClaudeCodeVersion + " (pinned)",
		"warn  engram 0.0.1 is installed; Jhon pins " + render.EngramVersion,
		"codegraph is not installed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor's versions lack %q:\n%s", want, got)
		}
	}
	if o.Failed != 0 {
		t.Errorf("a version mismatch failed doctor: %d", o.Failed)
	}
}

func TestInstalledVersionIsTheFirstVersionInTheOutput(t *testing.T) {
	for out, want := range map[string]string{"2.1.310 (Claude Code)": "2.1.310", "engram 3.6.1\n": "3.6.1", "v1.6.2": "1.6.2", "none": ""} {
		if got := installedVersion(out); got != want {
			t.Errorf("installedVersion(%q) = %q, want %q", out, got, want)
		}
	}
}
