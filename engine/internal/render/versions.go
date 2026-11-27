package render

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
)

// The one table of the versions Jhon installs (T50g, F7): jhon setup
// installs Claude Code and each integration's binary at these versions (the
// CLI hands them to setupui), the pi render pins its packages to them, and
// jhon doctor warns when an installed tool differs. An integration's pin is
// its manifest's doctor.version (integrations/<name>/manifest.yaml, T58a).
// Bump a version here, or in the manifest, in the commit that moves to it.
const (
	ClaudeCodeVersion     = "2.1.323" // npm @anthropic-ai/claude-code
	PiWebAccessVersion    = "0.43.0"  // npm pi-web-access
	PiClaudeBridgeVersion = "0.13.0"  // npm pi-claude-bridge; its windows below
	PiPrettyVersion       = "0.6.27"  // npm @heyhuynhgiabuu/pi-pretty
)

// A Tool is a command on PATH whose version doctor compares with its pin:
// `<Command> --version` prints it. Setting is the setting of an optional
// integration's binary, which doctor compares only while it is on; "" for
// one always compared.
type Tool struct{ Command, Version, Setting string }

// Tools are the installed tools doctor compares: Claude Code, then each
// integration's binary in its manifests' order; pi's packages are the
// render's own, written at their pins.
var Tools = func() []Tool {
	out := []Tool{{"claude", ClaudeCodeVersion, ""}}
	for _, m := range integrations.All() {
		if m.Doctor.Binary != "" {
			setting := m.Setting
			if m.Required {
				setting = ""
			}
			out = append(out, Tool{m.Doctor.Binary, m.Doctor.Version, setting})
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

// The context windows pi-claude-bridge PiClaudeBridgeVersion registers for a
// Claude model (T282, L1781), copied by hand from its src/models.ts, which
// the repository has no copy of: re-read that file whenever the pin moves
// (TestTheBridgeWindowsAreThePinnedBridges fails until these follow it). pi
// compacts a session by the registered window, so a model with no row runs
// at 200K, whatever its maker documents.
const (
	bridgeOneM       = 1_000_000 // ONE_M_CONTEXT
	bridgeTwoHundred = 200_000   // TWO_HUNDRED_K_CONTEXT
)

// piClaudeBridgeOneM is MEASURED_ONE_M: the ids the bridge requests as
// `<id>[1m]` and registers at 1M on every plan.
var piClaudeBridgeOneM = []string{
	"claude-mythos-5-2", "claude-mythos-5-1", "claude-fable-5", "claude-fable-5-1", "claude-fable-5-2", "claude-opus-5-5", "claude-opus-5", "claude-opus-4-8", "claude-opus-4-7", "claude-sonnet-5", "claude-sonnet-5-5", "claude-haiku-5-5",
}

// piClaudeBridgePlanGated is PLAN_GATED_ONE_M: the ids at 1M only under
// claude-bridge.json's provider.plan and provider.longContextExtraUsage, each
// with the settings that open it.
var piClaudeBridgePlanGated = map[string]struct {
	open  func(plan string, extraUsage bool) bool
	needs string
}{
	"claude-opus-4-6":   {func(plan string, extra bool) bool { return plan == "max" || extra }, `provider.plan "max" or provider.longContextExtraUsage true`},
	"claude-sonnet-4-6": {func(_ string, extra bool) bool { return extra }, "provider.longContextExtraUsage true"},
}

// piClaudeBridgeTwoHundredK are the Claude models whose maker documents a
// 200K window, each with its reason: the bridge serves them at 200K as
// documented, so doctor names no fix for them, and agents.yaml may name one.
// Read from pi-ai's anthropic catalog (dist/providers/data/anthropic.json;
// 1.4.0, 1.5.2 and 1.6.1 agree): its models at contextWindow 200000 with no
// 1M row and no plan gate, dated snapshot ids aside, which the bridge never
// exposes (L1786).
var piClaudeBridgeTwoHundredK = map[string]string{
	"claude-haiku-4-5":  "pi-ai's anthropic catalog documents a 200,000-token window",
	"claude-opus-4-5":   "pi-ai's anthropic catalog documents a 200,000-token window",
	"claude-sonnet-4-5": "pi-ai's anthropic catalog documents a 200,000-token window",
}

// bridgeWindow is the window the bridge registers for Claude model id under
// provider, claude-bridge.json's provider object, as its
// resolveClaudeCodeRuntimeModel resolves it (forceTwoHundredK, then a 1M
// row, then a plan gate, else 200K), and below 1M the clause that says why
// with the fix; "" at 1M, where the owner pinned it to 200K
// (forceTwoHundredK), which is a choice, or for a model documented at 200K
// (piClaudeBridgeTwoHundredK), which is served as documented.
func bridgeWindow(id string, provider any) (int, string) {
	if forced, ok := js.Prop(provider, "forceTwoHundredK").([]any); ok && slices.Contains(forced, any(id)) {
		return bridgeTwoHundred, ""
	}
	if slices.Contains(piClaudeBridgeOneM, id) {
		return bridgeOneM, ""
	}
	if _, ok := piClaudeBridgeTwoHundredK[id]; ok {
		return bridgeTwoHundred, ""
	}
	pinned := "pi-claude-bridge " + PiClaudeBridgeVersion + ", the version Jhon pins,"
	if g, ok := piClaudeBridgePlanGated[id]; ok {
		plan, _ := js.Prop(provider, "plan").(string)
		if g.open(plan, js.Prop(provider, "longContextExtraUsage") == true) {
			return bridgeOneM, ""
		}
		return bridgeTwoHundred, pinned + " serves it at 1M only with " + g.needs + " in ~/.pi/agent/claude-bridge.json — set it there, or pick a model the bridge serves at 1M (jhon profile set)"
	}
	return bridgeTwoHundred, pinned + " has no 1M row for it, so pi compacts it by 200,000 — update Jhon (jhon update), or pick a model the bridge serves at 1M (jhon profile set)"
}

// BridgeWindowWarnings are jhon doctor's warnings on the active profile (read
// as `jhon doctor --runtime pi` reads it) in the tree at root: one per pi
// model on claude-bridge that the pinned bridge serves below 1M, naming the
// model, the agents that run it, the window and the fix. None while pi is
// off, or where the manifest or the profile cannot be read.
func BridgeWindowWarnings(root string) (out []string) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	e, err := newEngine(resolveRoot(root), args{}, io.Discard, io.Discard)
	if err != nil {
		return nil
	}
	if _, off := e.off["pi"]; off {
		return nil
	}
	config, _ := e.piBridge()
	provider := js.Prop(config, "provider")
	var models []string
	agents := map[string][]string{}
	for _, a := range append([]manifest.Agent{e.mainAgent(piMain)}, e.roles()...) {
		m, ok := e.piModel(a)
		if !ok || piProvider(m.Model) != BridgeProvider {
			continue
		}
		if _, seen := agents[m.Model]; !seen {
			models = append(models, m.Model)
		}
		agents[m.Model] = append(agents[m.Model], a.Name)
	}
	for _, m := range models {
		if window, why := bridgeWindow(strings.TrimPrefix(m, BridgeProvider+"/"), provider); why != "" {
			out = append(out, fmt.Sprintf("pi model %s (profile %s: %s) runs with a %s-token context window, not 1,000,000: %s", m, e.active, strings.Join(agents[m], ", "), thousands(window), why))
		}
	}
	return out
}

// thousands is n with a comma between each group of three digits.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// pi's pinned packages.
const (
	piWebAccess    = "npm:pi-web-access@" + PiWebAccessVersion
	piClaudeBridge = "npm:pi-claude-bridge@" + PiClaudeBridgeVersion
)
