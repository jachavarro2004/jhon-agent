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
// none, on where the sandbox can run and off where it cannot, so the
// default never limits the user (slice 8, L1375 under L1366). Off Linux the
// default is off (D3); on Linux it is off when Check, which starts no
// process, finds a problem with the sandbox of in (no bubblewrap, one only
// in a folder a sandboxed command may write, user namespaces off), and why
// is that problem: the model's commands then run unsandboxed and `jhon
// doctor` warns with the fix. A bubblewrap Check passes that then cannot
// start keeps the default on: the probe refuses every command with its fix.
// An explicit on where the sandbox cannot run keeps refusing every command
// with the probe's fix. Settings that cannot be read leave the default. in
// is called only for that check, once; with the owner's value or off Linux
// nothing is built or read of the host. The Claude Code wrap and `jhon
// sandbox status`, which pi asks, read this one.
func Mode(h Host, in func() Input) (mode, source string, why *Problem) {
	vals, err := settings.Load(settings.Path(), os.LookupEnv)
	if v, ok := settings.Find(vals, "sandbox.mode"); err == nil && ok && v.Source != settings.SourceDefault {
		if mode, ok := v.Value.(string); ok {
			return mode, ModeSettings, nil
		}
	}
	if h.OS != "linux" {
		return "off", ModeDefault, nil
	}
	if _, p := Check(h, in()); p != nil {
		return "off", ModeDefault, p
	}
	return "on", ModeDefault, nil
}
