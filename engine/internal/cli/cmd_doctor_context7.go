package cli

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
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

// cliIntegrations is a section for each optional cli integration that is on
// (jhon-studio, odd/tasks/jhon-studio.md S3), after the versions, headed by
// its display name: first how its answer fits this jhon (cliFit, T237),
// then its program's own checks, `<doctor.binary> doctor --json`
// (integrations.AskDoctor), each that passes ok and each that fails a
// warning with its fix; its program not on PATH, a warning with how to
// install it. One that does not fit this jhon is off for the run: its one
// note, a warning, and its checks are not asked. Nothing of it fails Jhon's
// doctor: it is the owner's opt-in, and Jhon runs without it. One that is
// off shows nothing.
func (d doctor) cliIntegrations() {
	o := d.o
	for _, m := range integrations.Optional() {
		if m.Kind != "cli" || m.Doctor.Binary == "" || !settings.On(m.Setting) {
			continue
		}
		o.Head(m.Display)
		if _, err := exec.LookPath(m.Doctor.Binary); err != nil {
			o.Warn(fmt.Sprintf("%s is not on PATH (%s is on) — %s", m.Doctor.Binary, m.Setting, installAdvice(m)))
			continue
		}
		if !d.cliFit(m) {
			continue
		}
		checks, err := integrations.AskDoctor(m)
		if err != nil {
			o.Warn(err.Error())
			continue
		}
		for _, c := range checks {
			if c.OK {
				o.OK(c.ID + ": " + c.Detail)
			} else {
				o.Warn(c.ID + ": " + c.Detail + " — fix: " + c.Fix)
			}
		}
	}
}

// cliFit says how m's answer, `<binary> integration --json`, fits this jhon
// (integrations.Judge), and false when it does not: a warning with its one
// note. A minimum it meets is ok; an answer that declares none, and a
// development build of jhon, which checks none, are notes. A program that
// gives no answer says nothing here (jhon start notes it; its checks
// follow).
func (d doctor) cliFit(m integrations.Manifest) bool {
	a, err := integrations.AskIntegration(m)
	if err != nil {
		return true
	}
	fit := integrations.Judge(m, a, version.Version)
	named := m.Doctor.Binary + " " + a.Version
	switch {
	case fit.Off != "":
		d.o.Warn(fit.Off)
		return false
	case fit.NoMinimum:
		d.o.Note(fmt.Sprintf("%s declares no minimum jhon (no requires_jhon in %s integration --json): taken as compatible", named, m.Doctor.Binary))
	case fit.Unchecked:
		d.o.Note(fmt.Sprintf("%s requires jhon %s: not checked on a development build of jhon (%s)", named, a.RequiresJhon, version.Version))
	default:
		d.o.OK(fmt.Sprintf("%s requires jhon %s; this is jhon %s", named, a.RequiresJhon, version.Version))
	}
	return true
}

// installAdvice is how m's manifest says to install its binary: its manual
// way, else the first it lists.
func installAdvice(m integrations.Manifest) string {
	if s := m.InstallCommand("manual", m.Doctor.Version); s != "" {
		return s
	}
	if vias := m.InstallVias(); len(vias) > 0 {
		return m.InstallCommand(vias[0], m.Doctor.Version)
	}
	return "install " + m.Doctor.Binary
}

// shippedHolders are, by capability, the agents of the shipped
// prose/agents.yaml holding a remote-probed integration's capability:
// context7's, explore and worker (T133e, L1016), the row's clause before
// T58b. doctor names them when the judged tree's agents cannot be read
// (T58c, L1193); a test holds them to the checkout's.
var shippedHolders = map[string][]string{"context7": {"jhon-scout", "jhon-builder"}}

// heldBy is ", for <agents>": the agents of the judged tree's
// prose/agents.yaml (doctor's src: the checkout, or the installed release,
// which holds its tree) holding one of m's capabilities; "" when none
// does. With no tree, or one whose agents.yaml cannot be read, they are
// the shipped ones (shippedHolders).
func (d doctor) heldBy(m integrations.Manifest) string {
	names, ok := holders(d.src, m.Capabilities)
	if !ok {
		for _, c := range m.Capabilities {
			names = append(names, shippedHolders[c]...)
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

// holders are the agents of src's prose/agents.yaml holding one of caps;
// false when there is no src or its agents.yaml cannot be read.
func holders(src string, caps []string) ([]string, bool) {
	if src == "" {
		return nil, false
	}
	mf, err := manifest.Load(src)
	if err != nil {
		return nil, false
	}
	var names []string
	for _, a := range mf.Agents {
		if slices.ContainsFunc(caps, func(c string) bool { return slices.Contains(a.Caps, c) }) {
			names = append(names, a.Name)
		}
	}
	return names, true
}

// probeRemote asks a remote integration's endpoint whether it answers
// (integrations.ProbeRemote); a variable for the tests.
var probeRemote = integrations.ProbeRemote
