package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/palette"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/setupui"
	"github.com/jachavarro2004/jhon-agent/engine/internal/update"
)

// `jhon setup` (T5, S1, S4): the setup wizard (internal/setupui) on the
// terminal: the runtimes, the opt-in integrations, the checks, then it
// writes the settings and installs the runtimes from the active release.
// Without a terminal it exits 2 and writes nothing.
func init() { register("setup", runSetup) }

const setupUsage = "usage: jhon setup\n"

func runSetup(argv []string) (int, error) {
	if len(argv) > 0 {
		fmt.Fprintf(os.Stderr, "jhon setup: unknown argument: %s\n%s", argv[0], setupUsage)
		return 2, nil
	}
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		fmt.Fprintln(os.Stderr, setupui.ErrNoTerminal)
		return 2, nil
	}
	p, desktop := palette.Current()
	sys := setupui.NewSystem(settings.Path(), filepath.Join(project.Home(), ".local", "bin", "jhon"), setupPins(), reinstall)
	u := &tuiUpdater{c: update.New(), r: release.Locate()}
	defer u.close()
	sys.Updates = u
	if err := setupui.Run(os.Stdin, os.Stdout, sys, setupui.ModeSetup, setupui.Colors(palette.Semantic(p, desktop))); err != nil {
		if errors.Is(err, setupui.ErrNoTerminal) {
			fmt.Fprintln(os.Stderr, err)
			return 2, nil
		}
		return die(err.Error())
	}
	return 0, nil
}

// reinstall is setup's install step: the active release installs every
// runtime's projections again with the settings just written, as
// `jhon profile use` does (rerender), its output kept for the failure
// screen instead of drawn over the wizard (release.InstallTo).
func reinstall(setupui.Choices) error {
	r := release.Locate()
	current := r.Share + "/current"
	if !release.Exists(current + "/" + release.Binary) {
		return errors.New("no active release at " + release.Tilde(current) + "\nRun 'jhon sync' or install a release, then retry.")
	}
	var out bytes.Buffer
	if !release.InstallTo(current, r.Source, &out, &out) {
		return errors.New("the install of the runtimes failed\n" + strings.TrimSpace(out.String()))
	}
	return nil
}

// setupPins are the versions setup installs: render's versions table (T50g).
func setupPins() setupui.Pins {
	return setupui.Pins{ClaudeCode: render.ClaudeCodeVersion, Engram: render.EngramVersion, CodeGraph: render.CodeGraphVersion}
}
