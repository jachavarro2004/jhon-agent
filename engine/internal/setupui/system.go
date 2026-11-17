package setupui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/muesli/termenv"

	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

// InstallFunc installs the core and the chosen runtimes; the CLI passes the
// release install, so this package does not import it.
type InstallFunc func(Choices) error

// System is the real setup backend: it finds commands on PATH, installs
// through Install, and writes the choices to SettingsPath through
// settings.Set. Updates is the update backend, UpdatesNotAvailableYet when nil.
type System struct {
	SettingsPath, BinaryPath string
	// Home is shown as ~ in paths.
	Home     string
	LookPath func(string) (string, error)
	Install  InstallFunc
	Updates  Updater
	// Pins are the versions setup installs.
	Pins Pins
}

// Pins are the versions setup installs (T50g): the CLI passes render's
// versions table, so setup and the render pin from one place.
type Pins struct{ ClaudeCode, Engram, CodeGraph string }

// NewSystem is a System on PATH and the user's home.
func NewSystem(settingsPath, binaryPath string, pins Pins, install InstallFunc) *System {
	home, _ := os.UserHomeDir()
	return &System{SettingsPath: settingsPath, BinaryPath: binaryPath, Home: home, LookPath: exec.LookPath, Install: install, Pins: pins}
}

// fix is how to install name on this machine, at its pin: a command it can
// run, or advice when it has none of the tools one needs. Engram's
// INSTALLATION.md names brew, go install of its v3 module, and its release
// binaries; brew's tap has no pin, so go install comes first and brew is
// the fallback (jhon doctor warns when it installed another version): when
// go install fails, brew runs if it is on PATH, else the advice is to
// download the release (T51). CodeGraph's README names npm.
func (s *System) fix(name string) (cmd, adv string) {
	switch name {
	case "claude":
		return "npm install -g @anthropic-ai/claude-code@" + s.Pins.ClaudeCode, ""
	case "engram":
		const brew = "brew install gentleman-programming/tap/engram"
		download := "download engram v" + s.Pins.Engram + " from https://github.com/Gentleman-Programming/engram/releases/tag/v" + s.Pins.Engram
		goInstall := "go install github.com/Gentleman-Programming/engram/v3/cmd/engram@v" + s.Pins.Engram
		hasBrew := s.find("brew") != ""
		switch {
		case s.find("go") != "" && hasBrew:
			return goInstall + " || " + brew, ""
		case s.find("go") != "":
			return goInstall, "if that fails, " + download
		case hasBrew:
			return brew, ""
		}
		return "", download
	case "codegraph":
		if s.find("npm") != "" || s.find("node") != "" {
			return "npm install -g @colbymchenry/codegraph@" + s.Pins.CodeGraph, ""
		}
		return "", "install codegraph " + s.Pins.CodeGraph + " as https://github.com/colbymchenry/codegraph#install says"
	}
	return "", advice[name]
}

// advice is what to do for a dependency no one command installs everywhere.
var advice = map[string]string{
	"git":  "install git with your system's package manager",
	"node": "install Node.js 20 or newer with your system's package manager or nvm",
}

func (s *System) tilde(p string) string {
	if s.Home != "" && (p == s.Home || strings.HasPrefix(p, s.Home+string(filepath.Separator))) {
		return "~" + p[len(s.Home):]
	}
	return p
}

func (s *System) find(name string) string {
	p, err := s.LookPath(name)
	if err != nil {
		return ""
	}
	return s.tilde(p)
}

// Detect is each runtime's command on PATH, else its directory under the
// home (T10g: what settings.Detected turns a runtime on by).
func (s *System) Detect() Detection {
	found := func(command, dir string) string {
		if p := s.find(command); p != "" {
			return p
		}
		if fi, err := os.Stat(filepath.Join(s.Home, dir)); s.Home != "" && err == nil && fi.IsDir() {
			return s.tilde(filepath.Join(s.Home, dir))
		}
		return ""
	}
	return Detection{Claude: found("claude", ".claude"), Pi: found("pi", ".pi")}
}

// Saved reads the settings file through internal/settings, without the
// environment: a key the file does not set stays nil.
func (s *System) Saved() Saved {
	vals, err := settings.Load(s.SettingsPath, func(string) (string, bool) { return "", false })
	if err != nil {
		return Saved{}
	}
	file := map[string]*bool{}
	for _, v := range vals {
		if b, ok := v.Value.(bool); ok && v.Source == settings.SourceFile {
			file[v.Key] = &b
		}
	}
	var palette *string
	for _, v := range vals {
		if p, ok := v.Value.(string); ok && v.Key == "integrations.theme.palette" && v.Source == settings.SourceFile {
			palette = &p
		}
	}
	return Saved{
		Palette:    palette,
		ClaudeCode: file["runtimes.claude_code"], Pi: file["runtimes.pi"],
		Telemetry: file["integrations.telemetry"],
	}
}

// Checks are git, claude when Claude Code is chosen, node when pi is (setup
// installs pi itself with npm), engram and codegraph.
func (s *System) Checks(c Choices) []Check {
	names := []string{"git"}
	if c.ClaudeCode {
		names = append(names, "claude")
	}
	if c.Pi {
		names = append(names, "node")
	}
	names = append(names, "engram", "codegraph")
	out := make([]Check, 0, len(names))
	for _, n := range names {
		if p := s.find(n); p != "" {
			out = append(out, Check{Name: n, OK: true, Detail: p})
		} else {
			cmd, adv := s.fix(n)
			out = append(out, Check{Name: n, Detail: "not on PATH", Fix: cmd, Advice: adv})
		}
	}
	return out
}

func runtimes(c Choices) string {
	var r []string
	if c.ClaudeCode {
		r = append(r, "Claude Code")
	}
	if c.Pi {
		r = append(r, "pi")
	}
	return strings.Join(r, ", ")
}

// Steps write the settings first: the install renders each runtime from
// them, so a runtime turned off is not installed.
func (s *System) Steps(c Choices) []Step {
	return []Step{
		{Name: "Write settings", Detail: s.tilde(s.SettingsPath), WritesSettings: true},
		{Name: "Install runtimes", Detail: runtimes(c)},
	}
}

func (s *System) RunStep(c Choices, i int) error {
	switch i {
	case 0:
		return s.writeSettings(c)
	case 1:
		if s.Install == nil {
			return errors.New("this build has no installer")
		}
		return s.Install(c)
	}
	return errors.New("no install step " + strconv.Itoa(i))
}

// SetupKeys are the settings `jhon setup` shows and writes (T15.7, L96): the
// runtimes and the integrations. The shell's own, shell.*, are its Settings
// overlay's (/jhon:customize); Elsewhere names where each remaining one is set.
var SetupKeys = []string{
	"runtimes.claude_code", "runtimes.pi",
	"integrations.engram", "integrations.codegraph",
	"integrations.telemetry", "integrations.theme.palette",
}

// Elsewhere are the settings neither setup nor the shell shows, and where each is set.
var Elsewhere = map[string]string{
	"pi.profile":             "jhon profile use, or /jhon:profile in the shell",
	"claude_code.connectors": "jhon settings set claude_code.connectors true|false",
	"agents.act_on_announce": "jhon settings set agents.act_on_announce true|false",
	"integrations.context7":  "jhon settings set integrations.context7 true|false",
	"sandbox.mode":           "jhon settings set sandbox.mode on|off",
	"sandbox.allow_write":    "jhon settings set sandbox.allow_write <folder>,<folder>",
}

// writeSettings sets every choice. A runtime turned on is written before
// one turned off, so the file never holds two runtimes off in between.
func (s *System) writeSettings(c Choices) error {
	type kv struct {
		key string
		on  bool
	}
	vals := []kv{
		{"runtimes.claude_code", c.ClaudeCode}, {"runtimes.pi", c.Pi},
		{"integrations.engram", true}, {"integrations.codegraph", true},
		{"integrations.telemetry", c.Telemetry},
	}
	if !c.ClaudeCode {
		vals[0], vals[1] = vals[1], vals[0]
	}
	for _, v := range vals {
		if _, err := settings.Set(s.SettingsPath, v.key, strconv.FormatBool(v.on)); err != nil {
			return err
		}
	}
	palette := c.Palette
	if palette == "" {
		palette = "rose-ink"
	}
	_, err := settings.Set(s.SettingsPath, "integrations.theme.palette", palette)
	return err
}

func (s *System) Paths() Paths {
	return Paths{Binary: s.tilde(s.BinaryPath), Settings: s.tilde(s.SettingsPath)}
}

// Copy puts text on the terminal's clipboard (OSC 52).
func (s *System) Copy(text string) error {
	termenv.Copy(text)
	return nil
}

func (s *System) updates() Updater {
	if s.Updates == nil {
		return UpdatesNotAvailableYet{}
	}
	return s.Updates
}

func (s *System) Latest() (Release, error) { return s.updates().Latest() }
func (s *System) Update(r Release) error   { return s.updates().Update(r) }
func (s *System) Rollback(v string) error  { return s.updates().Rollback(v) }
