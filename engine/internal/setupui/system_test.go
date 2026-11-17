package setupui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

func noEnv(string) (string, bool) { return "", false }

// scratch is a System on a scratch settings path, with only the named
// commands on PATH.
func scratch(t *testing.T, onPath ...string) (*System, *[]Choices) {
	t.Helper()
	dir := t.TempDir()
	var installed []Choices
	s := NewSystem(filepath.Join(dir, "config", "jhon", "settings.toml"), filepath.Join(dir, "bin", "jhon"), testPins, func(c Choices) error {
		installed = append(installed, c)
		return nil
	})
	s.Home = dir
	s.LookPath = func(name string) (string, error) {
		for _, n := range onPath {
			if n == name {
				return filepath.Join(dir, "bin", name), nil
			}
		}
		return "", errors.New("not found")
	}
	return s, &installed
}

func values(t *testing.T, path string) map[string]any {
	t.Helper()
	vals, err := settings.Load(path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	for _, v := range vals {
		out[v.Key] = v.Value
	}
	return out
}

func TestSystemWritesTheChoicesThroughSettings(t *testing.T) {
	s, _ := scratch(t)
	// The file says pi is off: turning claude off and pi on must still work.
	if _, err := settings.Set(s.SettingsPath, "runtimes.pi", "false"); err != nil {
		t.Fatal(err)
	}
	c := Choices{Pi: true, Telemetry: true, Palette: "desktop"}
	steps := s.Steps(c)
	last := len(steps) - 1
	if !steps[0].WritesSettings || steps[0].Name != "Write settings" || last != 1 {
		t.Fatalf("steps = %+v", steps)
	}
	if err := s.RunStep(c, 0); err != nil {
		t.Fatal(err)
	}
	got := values(t, s.SettingsPath)
	want := map[string]any{
		"runtimes.claude_code": false, "runtimes.pi": true,
		"integrations.engram": true, "integrations.codegraph": true,
		"integrations.telemetry": true, "integrations.theme.palette": "desktop",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	b, _ := os.ReadFile(s.SettingsPath)
	if strings.Contains(string(b), "dashboard") {
		t.Errorf("setup writes the retired integrations.dashboard (T17b):\n%s", b)
	}
	if !strings.Contains(string(b), `palette = "desktop"`) {
		t.Errorf("settings file:\n%s", b)
	}
}

func TestSystemInstallsThroughTheInjectedInstaller(t *testing.T) {
	s, installed := scratch(t)
	c := Choices{ClaudeCode: true}
	steps := s.Steps(c)
	if steps[1].Name != "Install runtimes" || steps[1].Detail != "Claude Code" {
		t.Fatalf("steps = %+v", steps)
	}
	if err := s.RunStep(c, 1); err != nil {
		t.Fatal(err)
	}
	if len(*installed) != 1 || (*installed)[0] != c {
		t.Errorf("installed %+v", *installed)
	}
	if _, err := os.Stat(s.SettingsPath); err == nil {
		t.Errorf("installing wrote the settings outside their step")
	}
	s.Install = func(Choices) error { return errors.New("download failed") }
	if err := s.RunStep(c, 1); err == nil || err.Error() != "download failed" {
		t.Errorf("RunStep = %v", err)
	}
}

func TestSystemDetectsAndChecksOnPath(t *testing.T) {
	s, _ := scratch(t, "claude", "git", "codegraph")
	// A path under the home is shown as ~ and the rest in the OS's own form
	// (~\bin\claude on Windows).
	tilde := func(rest string) string { return filepath.Join("~", filepath.FromSlash(rest)) }
	d := s.Detect()
	if d.Claude != tilde("bin/claude") || d.Pi != "" {
		t.Errorf("Detect = %+v", d)
	}
	// A runtime's directory under the home is detected too (T10g), as
	// settings.Detected sees it; the command, when found, is what is shown.
	if err := os.MkdirAll(filepath.Join(s.Home, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if d := s.Detect(); d.Claude != tilde("bin/claude") || d.Pi != tilde(".pi") {
		t.Errorf("Detect with ~/.pi = %+v", d)
	}
	var names []string
	byName := map[string]Check{}
	for _, c := range s.Checks(Choices{ClaudeCode: true, Pi: true}) {
		names = append(names, c.Name)
		byName[c.Name] = c
	}
	if got := strings.Join(names, " "); got != "git claude node engram codegraph" {
		t.Errorf("checks %s", got)
	}
	if c := byName["git"]; !c.OK || c.Detail != tilde("bin/git") {
		t.Errorf("git = %+v", c)
	}
	if c := byName["engram"]; c.OK || c.Detail != "not on PATH" || c.Fix != "" || c.Advice != engramAdvice {
		t.Errorf("engram = %+v", c)
	}
	if c := byName["node"]; c.OK || c.Fix != "" || c.Advice != "install Node.js 20 or newer with your system's package manager or nvm" {
		t.Errorf("node = %+v", c)
	}
	if c := byName["claude"]; !c.OK {
		t.Errorf("claude = %+v", c)
	}
	// node is only checked for pi; claude only when chosen.
	names = nil
	for _, c := range s.Checks(Choices{ClaudeCode: true}) {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, " "); got != "git claude engram codegraph" {
		t.Errorf("checks without pi %s", got)
	}
	if p := s.Paths(); p.Settings != tilde("config/jhon/settings.toml") || p.Binary != tilde("bin/jhon") {
		t.Errorf("Paths = %+v", p)
	}
}

func TestSystemUpdatesAreNotAvailableYet(t *testing.T) {
	s, _ := scratch(t)
	_, err := s.Latest()
	if !errors.Is(err, ErrUpdateNotAvailable) {
		t.Errorf("Latest = %v", err)
	}
	if err := s.Update(Release{}); !errors.Is(err, ErrUpdateNotAvailable) {
		t.Errorf("Update = %v", err)
	}
}

// TestAFullRunInstallsOnceWithTheSettingsWritten: the verifier's probe (L17):
// the real System through the whole wizard calls the injected install once,
// with the chosen settings already on disk.
func TestAFullRunInstallsOnceWithTheSettingsWritten(t *testing.T) {
	s, _ := scratch(t, "git", "claude", "engram", "codegraph")
	var calls []Choices
	var telemetry any
	s.Install = func(c Choices) error {
		calls = append(calls, c)
		telemetry = values(t, s.SettingsPath)["integrations.telemetry"]
		return nil
	}
	m := press(start(t, NewSetup(s, rose), 100, 24), "enter", "enter", "space", "enter", "enter", "enter")
	contains(t, screen(m), "is installed")
	if len(calls) != 1 || calls[0] != (Choices{ClaudeCode: true, Telemetry: true}) || telemetry != true {
		t.Errorf("install calls %+v, telemetry in the file at install %v", calls, telemetry)
	}
}

// TestSystemSavedIsWhatTheFileSets: Saved reads settings.toml through
// internal/settings, and leaves what it does not set unset.
func TestSystemSavedIsWhatTheFileSets(t *testing.T) {
	s, _ := scratch(t)
	if got := s.Saved(); got != (Saved{}) {
		t.Errorf("Saved without a file = %+v", got)
	}
	for k, v := range map[string]string{"runtimes.pi": "false", "integrations.telemetry": "true"} {
		if _, err := settings.Set(s.SettingsPath, k, v); err != nil {
			t.Fatal(err)
		}
	}
	got := s.Saved()
	if got.ClaudeCode != nil || got.Pi == nil || *got.Pi || got.Telemetry == nil || !*got.Telemetry {
		t.Errorf("Saved = %+v", got)
	}
}

func TestSavedWithNoSettingsFileIsAllNil(t *testing.T) {
	if got := NewSystem(filepath.Join(t.TempDir(), "settings.toml"), "/x", testPins, nil).Saved(); got != (Saved{}) {
		t.Errorf("Saved = %+v", got)
	}
}

// testPins are versions no release pins: the fixes below take whatever
// table the System is given (the CLI gives render's, T50g).
var testPins = Pins{ClaudeCode: "9.9.1", Engram: "9.9.2", CodeGraph: "9.9.3"}

const engramAdvice = "download engram v9.9.2 from https://github.com/Gentleman-Programming/engram/releases/tag/v9.9.2"

// missing is the check for name with only onPath's commands on PATH.
func missing(t *testing.T, name string, onPath ...string) Check {
	t.Helper()
	s, _ := scratch(t, onPath...)
	for _, c := range s.Checks(Choices{}) {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s check", name)
	return Check{}
}

// T50g (F7): setup installs the pinned versions. brew's tap has no pin,
// so go install of the pinned module comes first; brew is the fallback,
// and jhon doctor warns when what it installed differs.
func TestEngramFixIsGoInstallOfThePinWhenGoIsOnPath(t *testing.T) {
	for _, onPath := range [][]string{{"go"}, {"brew", "go"}} {
		c := missing(t, "engram", onPath...)
		if !strings.HasPrefix(c.Fix, "go install github.com/Gentleman-Programming/engram/v3/cmd/engram@v9.9.2") {
			t.Errorf("with %v: engram = %+v", onPath, c)
		}
	}
}

func TestEngramFixIsBrewWhenOnlyBrewIsOnPath(t *testing.T) {
	c := missing(t, "engram", "brew")
	if c.Fix != "brew install gentleman-programming/tap/engram" || c.Advice != "" {
		t.Errorf("engram = %+v", c)
	}
}

func TestClaudeFixIsNpmOfThePin(t *testing.T) {
	s, _ := scratch(t, "npm")
	for _, c := range s.Checks(Choices{ClaudeCode: true}) {
		if c.Name == "claude" && c.Fix != "npm install -g @anthropic-ai/claude-code@9.9.1" {
			t.Errorf("claude = %+v", c)
		}
	}
}

func TestEngramWithoutBrewOrGoIsAdviceToDownload(t *testing.T) {
	c := missing(t, "engram")
	if c.Fix != "" || c.Advice != engramAdvice {
		t.Errorf("engram = %+v", c)
	}
}

func TestCodegraphFixIsNpmWhenNpmOrNodeIsOnPath(t *testing.T) {
	for _, tool := range []string{"npm", "node"} {
		c := missing(t, "codegraph", tool)
		if c.Fix != "npm install -g @colbymchenry/codegraph@9.9.3" || c.Advice != "" {
			t.Errorf("with %s: codegraph = %+v", tool, c)
		}
	}
}

func TestCodegraphWithoutNpmIsAdviceToItsInstallGuide(t *testing.T) {
	c := missing(t, "codegraph")
	if c.Fix != "" || c.Advice != "install codegraph 9.9.3 as https://github.com/colbymchenry/codegraph#install says" {
		t.Errorf("codegraph = %+v", c)
	}
}

// T15.7 (L96): every setting has one place it is edited: shell.* in the
// shell's Settings, SetupKeys here, the rest named in Elsewhere. Nothing
// falls through, and setup writes every key it shows.
func TestEverySettingHasItsPlace(t *testing.T) {
	vals, err := settings.Load(filepath.Join(t.TempDir(), "none.toml"), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	shown := map[string]bool{}
	for _, k := range SetupKeys {
		shown[k] = true
	}
	for _, v := range vals {
		_, other := Elsewhere[v.Key]
		if strings.HasPrefix(v.Key, "shell.") || shown[v.Key] || other {
			continue
		}
		t.Errorf("%s is edited nowhere: add it to setup, the shell's Settings, or Elsewhere", v.Key)
	}
	for k := range Elsewhere {
		if shown[k] {
			t.Errorf("%s is both in setup and Elsewhere", k)
		}
	}
	s, _ := scratch(t)
	if err := s.RunStep(Choices{ClaudeCode: true}, 0); err != nil {
		t.Fatal(err)
	}
	written, _ := settings.Load(s.SettingsPath, func(string) (string, bool) { return "", false })
	for _, v := range written {
		if shown[v.Key] && v.Source != settings.SourceFile {
			t.Errorf("setup shows %s but does not write it", v.Key)
		}
	}
}

// The profile is chosen with /jhon:profile; the panel's Profiles tab is gone (T15.11).
func TestElsewhereNamesJhonProfile(t *testing.T) {
	p := Elsewhere["pi.profile"]
	if !strings.Contains(p, "/jhon:profile") || strings.Contains(p, "Profiles tab") || strings.Contains(p, "alt+j") {
		t.Errorf("Elsewhere[pi.profile] = %q, want /jhon:profile", p)
	}
}

// runFix runs a check's fix line through sh with fake go and brew on PATH
// (T51): go exits goExit, and each command records that it ran.
func runFix(t *testing.T, fix string, goExit int, onPath ...string) []string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(bin, "ran")
	for _, n := range onPath {
		exit := 0
		if n == "go" {
			exit = goExit
		}
		script := "#!/bin/sh\necho " + n + " >> " + log + "\nexit " + strconv.Itoa(exit) + "\n"
		if err := os.WriteFile(filepath.Join(bin, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("/bin/sh", "-c", fix)
	cmd.Env = []string{"PATH=" + bin}
	_ = cmd.Run()
	b, _ := os.ReadFile(log)
	return strings.Fields(string(b))
}

// T51 (verify advisory A1, L453): go comes first; when go install fails and
// brew is on PATH, brew runs; when go install works, brew never runs.
func TestEngramFixFallsBackToBrewWhenGoInstallFails(t *testing.T) {
	c := missing(t, "engram", "brew", "go")
	if c.Fix != "go install github.com/Gentleman-Programming/engram/v3/cmd/engram@v9.9.2 || brew install gentleman-programming/tap/engram" || c.Advice != "" {
		t.Fatalf("engram = %+v", c)
	}
	if runtime.GOOS == "windows" {
		t.Skip("runs the fix with /bin/sh and #!/bin/sh stand-ins for go and brew, which Windows does not have; the fix's text is checked above")
	}
	if ran := runFix(t, c.Fix, 1, "go", "brew"); strings.Join(ran, " ") != "go brew" {
		t.Errorf("go fails: ran %v, want go then brew", ran)
	}
	if ran := runFix(t, c.Fix, 0, "go", "brew"); strings.Join(ran, " ") != "go" {
		t.Errorf("go works: ran %v, want go only", ran)
	}
}

// Without brew, a failed go install falls back to the download advice.
func TestEngramFixWithGoOnlyKeepsTheDownloadAdvice(t *testing.T) {
	c := missing(t, "engram", "go")
	if c.Fix != "go install github.com/Gentleman-Programming/engram/v3/cmd/engram@v9.9.2" || c.Advice != "if that fails, "+engramAdvice {
		t.Errorf("engram = %+v", c)
	}
}
