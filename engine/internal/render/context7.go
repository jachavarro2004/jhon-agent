package render

import (
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
)

// An optional integration is one its setting turns off at render (its
// manifest's when_off: render). Off, no agent holds its capabilities, no
// server is mounted for it, and a body's lines that name it are not
// rendered (withoutLinesNaming).
//
// context7 is library and framework documentation as an MCP server (T133e,
// L1016), for the agents that hold the `context7` capability in agents.yaml:
// explore and worker (L1000).
//
// It is the hosted endpoint, which answers without a key (a key only raises
// its rate limit; Jhon asks for none and stores none). So nothing is
// installed, the versions table holds no pin for it, and the render writes
// a URL and never reaches the network. What leaves the machine is what a
// call carries: the library's name or id and the question asked (and the
// language, when given), to Context7URL; pi also connects to it when a
// session or a sub-agent starts, naming itself (pi and its version) and no
// project content. pi's MCP client would answer a server's `roots/list` with
// the session folder's path; context7's published server asks for none.
// Without a network a call answers with an error and the session goes on.
//
// pi: mcp.json mounts it `direct`, the only exposure a child's --tools can
// name; jhon-shell keeps its tools out of the main session
// (shell/lib/tools-deferred.ts). Claude Code: the capability names the tools
// of a server called context7, which the two agents get where the owner's
// Claude Code has one; Jhon registers none there.
//
// Context7Setting = false turns it off: no agent holds the capability, no
// server is mounted, and the sentence of a body that names it is not
// rendered, so the render is what it was without it. Settings that cannot be
// read leave it off, as every optional integration. Its URL and setting
// are its manifest's, integrations/context7/manifest.yaml, read by name: ""
// when the set lacks it, a set jhon validate refuses (integrations.Assumed).
var (
	Context7URL     = remote().Doctor.URL
	Context7Setting = remote().Setting
)

// remote is the integration doctor probes over the network; a zero
// Manifest when the set has none.
func remote() integrations.Manifest {
	m, _ := integrations.Get(integrations.Context7)
	return m
}

// withoutOff is agents without the capabilities of each optional
// integration that is off, and which of them are on.
func withoutOff(agents []manifest.Agent, on func(setting string) bool) ([]manifest.Agent, map[string]bool) {
	state := map[string]bool{}
	for _, m := range integrations.Optional() {
		if state[m.Name] = on(m.Setting); !state[m.Name] {
			for _, c := range m.Capabilities {
				agents = withoutCap(agents, c)
			}
		}
	}
	return agents, state
}

// withoutUnheld is body without the lines naming each optional integration
// whose capabilities the agent does not hold.
func withoutUnheld(body string, caps []string) string {
	for _, m := range integrations.Optional() {
		if !slices.ContainsFunc(m.Capabilities, func(c string) bool { return slices.Contains(caps, c) }) {
			body = withoutLinesNaming(body, m.Name)
		}
	}
	return body
}

// withoutCap is the manifest's agents, none holding capability c.
func withoutCap(agents []manifest.Agent, c string) []manifest.Agent {
	out := slices.Clone(agents)
	for i, a := range out {
		if slices.Contains(a.Caps, c) {
			out[i].Caps = slices.DeleteFunc(slices.Clone(a.Caps), func(have string) bool { return have == c })
		}
	}
	return out
}

// withoutLinesNaming is body without the lines that name an integration:
// its sentence stands on lines of its own (context7_test.go), so the rest
// reads as written.
func withoutLinesNaming(body, name string) string {
	if !strings.Contains(strings.ToLower(body), name) {
		return body
	}
	lines := strings.Split(body, "\n")
	return strings.Join(slices.DeleteFunc(lines, func(line string) bool {
		return strings.Contains(strings.ToLower(line), name)
	}), "\n")
}
