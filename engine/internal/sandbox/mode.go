package sandbox

import (
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

// Where the effective sandbox.mode comes from: `jhon sandbox status
// --json`'s mode_source.
const (
	ModeDefault  = "default"
	ModeSettings = "settings"
)

// Mode is sandbox.mode as it applies on h (T128f, L1352 F1; design section
// 5, D3): the owner's value in the settings file, on every system; with
// none, on where the sandbox runs (Linux) and off elsewhere, where the
// model's commands run unsandboxed and `jhon doctor` warns. An explicit on
// where the sandbox cannot run keeps refusing every command with the
// probe's fix. Settings that cannot be read leave the default. The Claude
// Code wrap and `jhon sandbox status`, which pi asks, read this one.
func Mode(h Host) (mode, source string) {
	vals, err := settings.Load(settings.Path(), os.LookupEnv)
	if v, ok := settings.Find(vals, "sandbox.mode"); err == nil && ok && v.Source != settings.SourceDefault {
		if mode, ok := v.Value.(string); ok {
			return mode, ModeSettings
		}
	}
	if h.OS != "linux" {
		return "off", ModeDefault
	}
	return "on", ModeDefault
}
