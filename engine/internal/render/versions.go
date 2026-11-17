package render

import "github.com/jachavarro2004/jhon-agent/engine/internal/integrations"

// The one table of the versions Jhon installs (T50g, F7): jhon setup
// installs Claude Code and each integration's binary at these versions (the
// CLI hands them to setupui), the pi render pins its packages to them, and
// jhon doctor warns when an installed tool differs. An integration's pin is
// its manifest's doctor.version (integrations/<name>/manifest.yaml, T58a).
// Bump a version here, or in the manifest, in the commit that moves to it.
const (
	ClaudeCodeVersion     = "2.1.323" // npm @anthropic-ai/claude-code
	PiWebAccessVersion    = "0.43.0"  // npm pi-web-access
	PiClaudeBridgeVersion = "0.9.3"   // npm pi-claude-bridge
	PiPrettyVersion       = "0.6.27"  // npm @heyhuynhgiabuu/pi-pretty
)

// A Tool is a command on PATH whose version doctor compares with its pin:
// `<Command> --version` prints it.
type Tool struct{ Command, Version string }

// Tools are the installed tools doctor compares: Claude Code, then each
// integration's binary in its manifests' order; pi's packages are the
// render's own, written at their pins.
var Tools = func() []Tool {
	out := []Tool{{"claude", ClaudeCodeVersion}}
	for _, m := range integrations.All() {
		if m.Doctor.Binary != "" {
			out = append(out, Tool{m.Doctor.Binary, m.Doctor.Version})
		}
	}
	return out
}()

// EngramVersion and CodeGraphVersion are the core integrations' pins, by
// name ("" when the set lacks one: jhon validate refuses that set), for
// setupui.Pins (cli/cmd_setup.go), whose shape the CLI's tests fix; T58b
// reads every pin from Tools instead.
var (
	EngramVersion    = pin(integrations.Engram)
	CodeGraphVersion = pin(integrations.CodeGraph)
)

// pin is the doctor.version of the integration called name, "" without one.
func pin(name string) string {
	m, _ := integrations.Get(name)
	return m.Doctor.Version
}

// pi's pinned packages.
const (
	piWebAccess    = "npm:pi-web-access@" + PiWebAccessVersion
	piClaudeBridge = "npm:pi-claude-bridge@" + PiClaudeBridgeVersion
)
