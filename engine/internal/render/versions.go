package render

// The one table of the versions Jhon installs (T50g, F7): jhon setup
// installs Claude Code, Engram and CodeGraph at these versions (the CLI
// hands them to setupui), the pi render pins its packages to them, and
// jhon doctor warns when an installed tool differs. Bump a version here,
// in the commit that moves to it.
const (
	ClaudeCodeVersion     = "2.1.323" // npm @anthropic-ai/claude-code
	EngramVersion         = "3.7.2"   // github.com/Gentleman-Programming/engram/v3
	CodeGraphVersion      = "1.6.2"   // npm @colbymchenry/codegraph
	PiWebAccessVersion    = "0.43.0"  // npm pi-web-access
	PiClaudeBridgeVersion = "0.9.3"   // npm pi-claude-bridge
	PiPrettyVersion       = "0.6.27"  // npm @heyhuynhgiabuu/pi-pretty
)

// A Tool is a command on PATH whose version doctor compares with its pin:
// `<Command> --version` prints it.
type Tool struct{ Command, Version string }

// Tools are the installed tools doctor compares; pi's packages are the
// render's own, written at their pins.
var Tools = []Tool{{"claude", ClaudeCodeVersion}, {"engram", EngramVersion}, {"codegraph", CodeGraphVersion}}

// pi's pinned packages.
const (
	piWebAccess    = "npm:pi-web-access@" + PiWebAccessVersion
	piClaudeBridge = "npm:pi-claude-bridge@" + PiClaudeBridgeVersion
)
