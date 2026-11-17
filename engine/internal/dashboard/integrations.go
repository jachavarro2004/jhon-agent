package dashboard

import (
	"os"
	"os/exec"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

// /api/integrations (T58b O3, L1167): each integration of the manifests, by
// their order, as the dashboard shows it: `{name, display, required, state,
// reason, numbers}`, state `on`, `off` (its setting false) or `unavailable`
// (on, but its binary is not on PATH, or it does not answer), reason why
// when it is not plainly on, and numbers null unless it is on and answers.
// It degrades, never fails: whatever is off or unreachable is a row saying
// so. Nothing names an integration but Engram, whose numbers are its
// observations' total (its local API's /stats); a remote one (doctor.probe
// remote) is asked over the network only with ?probe=1, as doctor asks it
// only with --remote.

// engramRoute is the Memory page's Engram route, /api/memory/engram.
var engramRoute = "/api/memory/" + integrations.Engram

// lookPath finds a binary on PATH; probeRemote asks a remote integration
// whether it answers. Variables for the tests.
var (
	lookPath    = exec.LookPath
	probeRemote = integrations.ProbeRemote
)

// integrationRows are the rows of /api/integrations; probe asks the remote
// ones.
func integrationRows(probe bool) []any {
	vals, err := settings.Load(settings.Path(), os.LookupEnv)
	on := func(key string) bool {
		if err != nil {
			return false
		}
		v, _ := settings.Find(vals, key)
		return v.Value == true
	}
	rows := []any{}
	for _, m := range integrations.All() {
		state, reason, numbers := "on", "", any(nil)
		switch {
		case !m.Required && !on(m.Setting):
			state, reason = "off", m.Setting+" = false"
			if err != nil {
				reason = "the settings cannot be read, so it is off"
			}
		case m.Doctor.Binary != "" && !found(m.Doctor.Binary):
			state, reason = "unavailable", m.Doctor.Binary+" is not on PATH"
		case m.Doctor.Probe == "remote" && !probe:
			reason = "not asked: ?probe=1 asks " + m.Doctor.URL
		case m.Doctor.Probe == "remote":
			if err := probeRemote(m.Doctor.URL); err != nil {
				state, reason = "unavailable", m.Doctor.URL+": "+err.Error()
			}
		case m.Name == integrations.Engram:
			var stats struct {
				Total *float64 `json:"total_observations"`
			}
			if err := engramGet("/stats", &stats); err != nil {
				state, reason = "unavailable", engramReason(err)
			} else if stats.Total != nil {
				numbers = js.Obj("observations", *stats.Total)
			}
		}
		rows = append(rows, js.Obj("name", m.Name, "display", m.Display, "required", m.Required, "state", state, "reason", reason, "numbers", numbers))
	}
	return rows
}

// found is whether bin is on PATH.
func found(bin string) bool {
	_, err := lookPath(bin)
	return err == nil
}

// engramReason is engramFailure's text on one line.
func engramReason(err error) string {
	r := engramFailure(err)
	return r[:len(r)-1]
}
