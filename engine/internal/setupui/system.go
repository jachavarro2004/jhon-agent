package setupui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/muesli/termenv"

	manifests "github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
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
	// Ask is a cli integration's answer, `<binary> integration --json`
	// (manifests.AskIntegration), and Jhon the running jhon's version: a
	// chosen tool found on PATH that does not fit it is noted (T237).
	Ask  func(manifests.Manifest) (manifests.Answer, error)
	Jhon string
	// InstallTool installs a chosen cli integration's program from its
	// signed GitHub releases (T242): the newest that supports Jhon,
	// verified; a development clone is left alone. The CLI passes
	// internal/update's. Nil, setup installs no tool, and a missing one's
	// check says how to install it by hand.
	InstallTool func(manifests.Manifest) error
}

// Pins are the versions setup installs (T50g): the CLI passes render's
// versions table, so setup and the render pin from one place. Engram and
// CodeGraph are the core integrations' pins (manifests.Core), by name.
type Pins struct{ ClaudeCode, Engram, CodeGraph string }

// of is the pin of an integration's binary: a core one's from Pins,
// another's its manifest's.
func (p Pins) of(m manifests.Manifest) string {
	switch m.Name {
	case manifests.Engram:
		return p.Engram
	case manifests.CodeGraph:
		return p.CodeGraph
	}
	return m.Doctor.Version
}

// binaries are the integrations setup checks for, by their binary's name:
// each required one's, and an optional one's only when c chooses it on.
func binaries(c Choices) []manifests.Manifest {
	var out []manifests.Manifest
	for _, m := range manifests.All() {
		if m.Doctor.Binary != "" && (m.Required || c.Optional.On(m.Setting)) {
			out = append(out, m)
		}
	}
	return out
}

// offered are the optional integrations setup shows a row for, by Order:
// each with setup text in its manifest (setup.summary; jhon-studio,
// odd/tasks/jhon-studio.md S3). Every other optional one is set elsewhere
// (Elsewhere).
func offered() []manifests.Manifest {
	var out []manifests.Manifest
	for _, m := range manifests.Optional() {
		if m.Setup.Summary != "" {
			out = append(out, m)
		}
	}
	return out
}

// NewSystem is a System on PATH and the user's home.
func NewSystem(settingsPath, binaryPath string, pins Pins, install InstallFunc) *System {
	home, _ := os.UserHomeDir()
	return &System{SettingsPath: settingsPath, BinaryPath: binaryPath, Home: home, LookPath: exec.LookPath, Install: install, Pins: pins,
		Ask: manifests.AskIntegration, Jhon: version.Version}
}

// fix is how to install name on this machine, at its pin: a command it can
// run, or advice when it has none of the tools one needs.
func (s *System) fix(name string) (cmd, adv string) {
	if name == "claude" {
		return "npm install -g @anthropic-ai/claude-code@" + s.Pins.ClaudeCode, ""
	}
	for _, m := range manifests.All() {
		if m.Doctor.Binary == name {
			return s.install(m)
		}
	}
	return "", advice[name]
}

// install is an integration's fix from its manifest's setup.install: the
// ways it lists, in order, that this machine has the tool for (go; npm,
// which node brings; brew). Two found: the first, and the second when it
// fails. One found: it, with the manual way as advice when a later way it
// lists is missing (Engram: go install of its v3 module first, brew's
// unpinned tap the fallback, else download the release, T51). None: the
// manual way as advice.
func (s *System) install(m manifests.Manifest) (cmd, adv string) {
	version := s.Pins.of(m)
	have := map[string]bool{"go": s.find("go") != "", "npm": s.find("npm") != "" || s.find("node") != "", "brew": s.find("brew") != ""}
	var found []string
	missingAfter := false
	for _, via := range m.InstallVias() {
		switch {
		case via == "manual":
		case have[via]:
			found = append(found, m.InstallCommand(via, version))
		case len(found) > 0:
			missingAfter = true
		}
	}
	manual := m.InstallCommand("manual", version)
	switch {
	case len(found) == 0:
		return "", manual
	case len(found) > 1:
		return found[0] + " || " + found[1], ""
	case missingAfter:
		return found[0], "if that fails, " + manual
	}
	return found[0], ""
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
	var optional Toggles
	for _, m := range offered() {
		on := m.Default
		if v := file[m.Setting]; v != nil {
			on = *v
		}
		optional = optional.with(m.Setting, on)
	}
	return Saved{
		Palette:    palette,
		ClaudeCode: file["runtimes.claude_code"], Pi: file["runtimes.pi"],
		Telemetry: file["integrations.telemetry"],
		Optional:  optional,
	}
}

// released are the chosen optional integrations whose program setup
// installs from its releases (InstallTool, manifests.Manifest.ReleaseRepo),
// by Order; none without InstallTool.
func (s *System) released(c Choices) []manifests.Manifest {
	if s.InstallTool == nil {
		return nil
	}
	var out []manifests.Manifest
	for _, m := range binaries(c) {
		if _, _, ok := m.ReleaseRepo(); ok && !m.Required {
			out = append(out, m)
		}
	}
	return out
}

// Checks are git, claude when Claude Code is chosen, node when pi is (setup
// installs pi itself with npm), and each integration's binary (Engram,
// CodeGraph; an optional one's only when chosen on). A chosen cli tool
// found on PATH whose answer does not fit this jhon (manifests.Judge) is
// found with its one note: it is off, the core installs all the same. A
// chosen one setup installs from its releases (released) that is not on
// PATH locks nothing: its Install step adds it.
func (s *System) Checks(c Choices) []Check {
	names := []string{"git"}
	if c.ClaudeCode {
		names = append(names, "claude")
	}
	if c.Pi {
		names = append(names, "node")
	}
	tools := map[string]manifests.Manifest{}
	for _, m := range binaries(c) {
		names = append(names, m.Doctor.Binary)
		if m.Kind == "cli" {
			tools[m.Doctor.Binary] = m
		}
	}
	installs := map[string]manifests.Manifest{}
	for _, m := range s.released(c) {
		installs[m.Doctor.Binary] = m
	}
	out := make([]Check, 0, len(names))
	for _, n := range names {
		if p := s.find(n); p != "" {
			out = append(out, Check{Name: n, OK: true, Detail: p, Note: s.offNote(tools, n)})
		} else if m, ok := installs[n]; ok {
			owner, repo, _ := m.ReleaseRepo()
			out = append(out, Check{Name: n, OK: true, Detail: "not on PATH yet: Install adds its newest signed release from github.com/" + owner + "/" + repo})
		} else {
			cmd, adv := s.fix(n)
			out = append(out, Check{Name: n, Detail: "not on PATH", Fix: cmd, Advice: adv})
		}
	}
	return out
}

// offNote is the note of the cli tool called name when its answer does not
// fit this jhon; "" for any other binary, one that fits, or one that gives
// no answer (jhon doctor says why).
func (s *System) offNote(tools map[string]manifests.Manifest, name string) string {
	m, ok := tools[name]
	if !ok || s.Ask == nil {
		return ""
	}
	a, err := s.Ask(m)
	if err != nil {
		return ""
	}
	return manifests.Judge(m, a, s.Jhon).Off
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
// them, so a runtime turned off is not installed. Then each chosen tool
// setup installs from its releases (released) has a step of its own.
func (s *System) Steps(c Choices) []Step {
	steps := []Step{
		{Name: "Write settings", Detail: s.tilde(s.SettingsPath), WritesSettings: true},
		{Name: "Install runtimes", Detail: runtimes(c)},
	}
	for _, m := range s.released(c) {
		steps = append(steps, Step{Name: "Install " + m.Display, Detail: "its newest signed release that supports jhon " + s.Jhon})
	}
	return steps
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
	if tools := s.released(c); i >= 2 && i-2 < len(tools) {
		return s.InstallTool(tools[i-2])
	}
	return errors.New("no install step " + strconv.Itoa(i))
}

// SetupKeys are the settings `jhon setup` shows and writes (T15.7, L96): the
// runtimes and the integrations, the offered optional ones among them. The shell's own, shell.*, are its Settings
// overlay's (/jhon:customize); Elsewhere names where each remaining one is set.
var SetupKeys = func() []string {
	keys := []string{"runtimes.claude_code", "runtimes.pi"}
	for _, m := range manifests.Required() {
		keys = append(keys, m.Setting)
	}
	keys = append(keys, "integrations.telemetry", "integrations.theme.palette")
	for _, m := range offered() {
		keys = append(keys, m.Setting)
	}
	return keys
}()

// Elsewhere are the settings neither setup nor the shell shows, and where
// each is set; every optional integration's setting setup does not offer
// among them, whatever its when_off (its manifest's, T58b).
var Elsewhere = func() map[string]string {
	e := map[string]string{
		"pi.profile":             "jhon profile use, or /jhon:profile in the shell",
		"agents.act_on_announce": "jhon settings set agents.act_on_announce true|false",
		"sandbox.mode":           "jhon settings set sandbox.mode on|off (on by default where the sandbox can run)",
		"sandbox.allow_write":    "jhon settings set sandbox.allow_write <folder>,<folder>",
		"sandbox.deploy_tools":   "jhon settings set sandbox.deploy_tools <program>,<program>",
		"sandbox.docker":         "jhon settings set sandbox.docker true|false (false by default: the docker socket hidden inside the sandbox)",
	}
	for _, m := range manifests.Optional() {
		if m.Setup.Summary == "" {
			e[m.Setting] = "jhon settings set " + m.Setting + " true|false"
		}
	}
	return e
}()

// writeSettings sets every choice. A runtime turned on is written before
// one turned off, so the file never holds two runtimes off in between. An
// offered optional integration at its default is left out of the file.
func (s *System) writeSettings(c Choices) error {
	type kv struct {
		key string
		on  bool
	}
	vals := []kv{{"runtimes.claude_code", c.ClaudeCode}, {"runtimes.pi", c.Pi}}
	for _, m := range manifests.Required() {
		vals = append(vals, kv{m.Setting, true})
	}
	vals = append(vals, kv{"integrations.telemetry", c.Telemetry})
	if !c.ClaudeCode {
		vals[0], vals[1] = vals[1], vals[0]
	}
	for _, v := range vals {
		if _, err := settings.Set(s.SettingsPath, v.key, strconv.FormatBool(v.on)); err != nil {
			return err
		}
	}
	// An offered optional integration is written only when it differs from
	// its default, and its line dropped when it is back at it: a binary that
	// does not know the key (an older release after a rollback) still reads
	// the file (odd/tasks/jhon-studio.md L11).
	for _, m := range offered() {
		if on := c.Optional.On(m.Setting); on != m.Default {
			if _, err := settings.Set(s.SettingsPath, m.Setting, strconv.FormatBool(on)); err != nil {
				return err
			}
		} else if _, err := settings.Unset(s.SettingsPath, m.Setting); err != nil {
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
