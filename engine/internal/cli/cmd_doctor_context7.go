package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

// remoteProbed is doctor's row for each optional integration its manifest
// probes remotely (doctor.probe: remote; context7, T133e, L1016), after the
// versions: a hosted endpoint, so there is no version to compare, and
// whether it answers is asked only with --remote, never by a plain doctor
// or at a session's start. Not answering is a warning: its tools then
// answer with an error, and nothing else of Jhon depends on it. Its name,
// setting and url are its manifest's (T58b, L1167).
func (d doctor) remoteProbed(remote bool) {
	o := d.o
	for _, m := range integrations.RemoteProbed() {
		url := m.Doctor.URL
		switch {
		case !settings.On(m.Setting):
			o.Note(fmt.Sprintf("%s is off (%s = false)", m.Name, m.Setting))
		case !remote:
			o.Note(fmt.Sprintf("%s is the hosted %s%s: nothing installed or pinned; run 'jhon doctor --remote' to ask whether it answers", m.Name, url, d.heldBy(m)))
		default:
			if err := probeRemote(url); err != nil {
				o.Warn(fmt.Sprintf("%s unreachable (%s: %s) — offline, or the service is down; its tools answer with an error until it is back", m.Name, url, err))
				continue
			}
			o.OK(m.Name + " answers at " + url)
		}
	}
}

// heldBy is ", for <agents>": the agents of the judged tree's
// prose/agents.yaml holding one of m's capabilities; "" when none does or
// the file cannot be read.
func (d doctor) heldBy(m integrations.Manifest) string {
	if d.src == "" {
		return ""
	}
	mf, err := manifest.Load(d.src)
	if err != nil {
		return ""
	}
	var names []string
	for _, a := range mf.Agents {
		if slices.ContainsFunc(m.Capabilities, func(c string) bool { return slices.Contains(a.Caps, c) }) {
			names = append(names, a.Name)
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return ", for " + names[0]
	}
	return ", for " + strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// probeRemote asks a remote integration's endpoint whether it answers
// (integrations.ProbeRemote); a variable for the tests.
var probeRemote = integrations.ProbeRemote
