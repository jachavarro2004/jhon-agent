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

// Effective is sandbox.mode as it applies on a host, and where it comes
// from (ModeDefault or ModeSettings). Probed is whether Mode probed the
// sandbox to decide it, the default on Linux: Problem is then the probe's
// verdict, nil where one empty sandbox started (TimedOut where it did not
// end in time, the mode then on), and a caller that probes
// the same sandbox takes it instead of starting bubblewrap again.
type Effective struct {
	Mode, Source string
	Probed       bool
	Problem      *Problem
}

// Mode is sandbox.mode as it applies on h (T128f, L1352 F1; design section
// 5, D3): the owner's value in the settings file, on every system; with
// none, on only where the sandbox runs, so the default never limits the
// user (slice 8 L1375 and 8b L1379, under L1366). Off Linux the default is
// off (D3); on Linux it is on when Probe starts one empty sandbox of in,
// and off with the probe's problem when it does not: no bubblewrap, one
// only in a folder a sandboxed command may write, user namespaces off,
// AppArmor blocking them (Ubuntu 24.04) or a bubblewrap too old. The
// model's commands then run unsandboxed and `jhon doctor` warns with the
// fix. An explicit on where the sandbox cannot run keeps refusing every
// command with the probe's fix. Settings that cannot be read leave the
// default. A probe that times out is no verdict (slice 8c, L1382): the
// default stays on, with the probe's problem, and the command is wrapped as
// usual. in is called only for that probe, once; with the owner's value
// or off Linux nothing is built, read or started. The Claude Code wrap and
// `jhon sandbox status`, which pi asks, read this one.
func Mode(h Host, in func() Input) Effective {
	vals, err := settings.Load(settings.Path(), os.LookupEnv)
	if v, ok := settings.Find(vals, "sandbox.mode"); err == nil && ok && v.Source != settings.SourceDefault {
		if mode, ok := v.Value.(string); ok {
			return Effective{Mode: mode, Source: ModeSettings}
		}
	}
	if h.OS != "linux" {
		return Effective{Mode: "off", Source: ModeDefault}
	}
	p := Probe(h, in())
	if p != nil && !p.TimedOut {
		return Effective{Mode: "off", Source: ModeDefault, Probed: true, Problem: p}
	}
	return Effective{Mode: "on", Source: ModeDefault, Probed: true, Problem: p}
}
