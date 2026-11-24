// Package setupui is the terminal UI `jhon setup` and `jhon update` open
// (T5, S1, S4, S7): a wizard with a progress rail, drawn from the chosen
// artboard design/installer-tui/01-wizard-rail.html. Setup walks Welcome,
// Runtimes (at least one), Integrations (Engram and CodeGraph locked on,
// the rest off by default, each optional integration with setup text a row
// of its own), Checks, Install and Done; update walks Check,
// Review, Update and Done, with a roll-back.
//
// The model is pure: everything that touches the machine (finding commands,
// the checks, the install steps, the settings, the clipboard, releases)
// goes through Backend, so tests drive it with a fake. System is the real
// setup backend; the install itself is a function the CLI injects, so this
// package does not import the release code. Colors come only from rose ink,
// passed in by the CLI as palette.Semantic's roles (Colors). Bubble Tea and
// Lip Gloss live in this package alone (S7).
package setupui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/termenv"
)

// Nothing here asks the terminal anything: rose ink is a fixed dark
// palette, so the background is set, not queried (an OSC 11 query a
// terminal never answers holds the keys typed meanwhile, L17); the color
// profile comes from the environment (Run). Bubble Tea's own start-up
// query is gone too: third_party/bubbletea is v1.3.10 without tea_init.go.
func init() { lipgloss.SetHasDarkBackground(true) }

// Detection is where each runtime's command was found, as it should read on
// screen (~ for the home), or empty when it is not on PATH.
type Detection struct{ Claude, Pi string }

// Check is one dependency: found (OK) or not, a detail to show, and when it
// is missing either the one command that fixes it (Fix) or, where no one
// command fits every system, what to do (Advice). Note is said under a
// found one that is off all the same: a tool that needs a newer jhon (T237);
// it never locks Install.
type Check struct {
	Name, Detail, Fix, Advice, Note string
	OK                              bool
}

// Step is one install step. WritesSettings marks the step that writes the
// settings file, so a failure before it can say the settings were not written.
type Step struct {
	Name, Detail   string
	WritesSettings bool
}

// Choices are the owner's picks. Engram and CodeGraph are not choices: for now
// ships them coupled to the core, always on (S4).
type Choices struct {
	ClaudeCode, Pi, Telemetry bool
	// Palette is integrations.theme.palette: "rose-ink" (or "", its default) or "desktop", the desktop palette file (T15.7, L97, T50f).
	Palette string
	// Optional are the offered optional integrations chosen on (offered).
	Optional Toggles
}

// set turns the offered integration whose setting is setting on or off.
func (c *Choices) set(setting string, on bool) { c.Optional = c.Optional.with(setting, on) }

// Toggles are the settings of the offered optional integrations that are on
// (offered: jhon-studio, odd/tasks/jhon-studio.md S3), each followed by a
// newline, in their manifests' order: a string, so Choices and Saved stay
// comparable.
type Toggles string

// On is whether setting is among them.
func (t Toggles) On(setting string) bool {
	return setting != "" && strings.Contains("\n"+string(t), "\n"+setting+"\n")
}

// with is t with setting on or off, in the offered integrations' order.
func (t Toggles) with(setting string, on bool) Toggles {
	var out strings.Builder
	for _, m := range offered() {
		if m.Setting == setting && on || m.Setting != setting && t.On(m.Setting) {
			out.WriteString(m.Setting + "\n")
		}
	}
	return Toggles(out.String())
}

// Paths are where the binary and the settings live, as they should read on screen.
type Paths struct{ Binary, Settings string }

// Backend is everything the setup and update screens ask of the machine.
type Backend interface {
	Detect() Detection
	// Saved is the choices the settings file already holds.
	Saved() Saved
	Checks(Choices) []Check
	// Steps are the install steps for the choices; RunStep runs the i-th.
	// A step's error's first line is its reason, every further line a
	// sentence of help under it.
	Steps(Choices) []Step
	RunStep(Choices, int) error
	Paths() Paths
	// Copy puts text on the clipboard (a check's fix).
	Copy(string) error
	Updater
}

// Mode is which flow the TUI opens on.
type Mode int

const (
	ModeSetup Mode = iota
	ModeUpdate
)

// ErrNoTerminal is Run's refusal when the input or output is not a terminal.
var ErrNoTerminal = errors.New("jhon setup needs a terminal")

// Run opens the TUI on the terminal in and out, in mode and colors, until
// the owner closes it. It refuses anything that is not a terminal with ErrNoTerminal.
// A setup the owner quit after a failed or cancelled install is an error.
func Run(in io.Reader, out io.Writer, b Backend, mode Mode, colors Colors) error {
	fi, ok := in.(*os.File)
	fo, ok2 := out.(*os.File)
	if !ok || !ok2 || !term.IsTerminal(fi.Fd()) || !term.IsTerminal(fo.Fd()) {
		return ErrNoTerminal
	}
	lipgloss.SetColorProfile(termenv.NewOutput(out).EnvColorProfile())
	m := NewSetup(b, colors)
	if mode == ModeUpdate {
		m = NewUpdate(b, colors)
	}
	final, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out), tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(Model); ok && fm.stopped != nil {
		return fmt.Errorf("setup stopped: %w", fm.stopped)
	}
	return nil
}

// Saved is each choice as the settings file sets it, nil where the file
// does not: a second setup starts from it instead of from detection.
type Saved struct {
	ClaudeCode, Pi, Telemetry *bool
	Palette                   *string
	// Optional are the offered integrations on: as the file sets each, else
	// its manifest's default.
	Optional Toggles
}
