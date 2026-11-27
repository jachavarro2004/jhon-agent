package render

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
	"github.com/jachavarro2004/jhon-agent/engine/internal/profiles"
)

// T282 (L1781): piClaudeBridgeOneM is MEASURED_ONE_M of the pinned bridge's
// src/models.ts, and piClaudeBridgePlanGated its PLAN_GATED_ONE_M, copied by
// hand. A pin moved without re-reading that file fails here, before an
// owner's model silently runs at 200K (pi-claude-bridge 0.9.3 had no row for
// claude-fable-5-2).
func TestTheBridgeWindowsAreThePinnedBridges(t *testing.T) {
	const read = "0.13.0"
	oneM := []string{"claude-mythos-5-2", "claude-mythos-5-1", "claude-fable-5", "claude-fable-5-1", "claude-fable-5-2", "claude-opus-5-5", "claude-opus-5", "claude-opus-4-8", "claude-opus-4-7", "claude-sonnet-5", "claude-sonnet-5-5", "claude-haiku-5-5"}
	gated := []string{"claude-opus-4-6", "claude-sonnet-4-6"}
	var gotGated []string
	for id := range piClaudeBridgePlanGated {
		gotGated = append(gotGated, id)
	}
	slices.Sort(gotGated)
	if PiClaudeBridgeVersion != read || !slices.Equal(piClaudeBridgeOneM, oneM) || !slices.Equal(gotGated, gated) {
		t.Errorf("pi-claude-bridge is pinned at %s, but the windows were read from %s's src/models.ts: re-read MEASURED_ONE_M and PLAN_GATED_ONE_M in the pinned version's src/models.ts, then update piClaudeBridgeOneM and piClaudeBridgePlanGated (versions.go) and this test\n1M: %q\nplan-gated: %q", PiClaudeBridgeVersion, read, piClaudeBridgeOneM, gotGated)
	}
	// L1786 advisory: the Claude models pi-ai's anthropic catalog documents at
	// 200,000 (pi-ai 1.4.0, 1.5.2 and 1.6.1 agree), dated snapshot ids aside,
	// which the bridge never exposes.
	var gotTwoHundred []string
	for id := range piClaudeBridgeTwoHundredK {
		gotTwoHundred = append(gotTwoHundred, id)
	}
	slices.Sort(gotTwoHundred)
	if want := []string{"claude-haiku-4-5", "claude-opus-4-5", "claude-sonnet-4-5"}; !slices.Equal(gotTwoHundred, want) {
		t.Errorf("piClaudeBridgeTwoHundredK = %q, want %q: re-read the anthropic models with contextWindow 200000 in pi-ai's dist/providers/data/anthropic.json", gotTwoHundred, want)
	}
}

// T282: every Claude model agents.yaml names has a 1M row in the pinned
// bridge, or is on piClaudeBridgeTwoHundredK with the reason it runs at 200K.
func TestEveryClaudeModelOfAgentsYamlHasABridgeWindow(t *testing.T) {
	m, err := manifest.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for runtime, row := range m.TierTable() {
		for tier, value := range row {
			v, _ := profiles.TierValue(value)
			id := strings.TrimPrefix(v.Model, BridgeProvider+"/")
			if !strings.HasPrefix(id, "claude-") {
				continue
			}
			seen++
			if slices.Contains(piClaudeBridgeOneM, id) {
				continue
			}
			if reason, ok := piClaudeBridgeTwoHundredK[id]; ok && reason != "" {
				continue
			}
			t.Errorf("agents.yaml's %s %s tier names %s, which pi-claude-bridge %s serves at 200,000 tokens (no row in its src/models.ts MEASURED_ONE_M): pick a model with a row, pin a bridge that has one, or add it to piClaudeBridgeTwoHundredK with the reason", runtime, tier, id, PiClaudeBridgeVersion)
		}
	}
	if seen == 0 {
		t.Error("agents.yaml names no Claude model: the check read nothing")
	}
	for id, reason := range piClaudeBridgeTwoHundredK {
		if _, gated := piClaudeBridgePlanGated[id]; reason == "" || slices.Contains(piClaudeBridgeOneM, id) || gated {
			t.Errorf("piClaudeBridgeTwoHundredK[%s] = %q: an entry needs its reason, and none has a 1M row or a plan gate", id, reason)
		}
	}
}

// T282: the window the pinned bridge registers, as its
// resolveClaudeCodeRuntimeModel resolves it: forceTwoHundredK first, then a
// 1M row, then a plan gate, else 200K.
func TestBridgeWindow(t *testing.T) {
	for _, c := range []struct {
		id, provider string
		want         int
		fix          bool
	}{
		{"claude-fable-5-2", `{}`, 1_000_000, false},
		{"claude-opus-5-5", `{"plan":"pro"}`, 1_000_000, false},
		{"claude-fable-5-3", `{}`, 200_000, true},
		// Documented at 200K by its maker: no fix to suggest.
		{"claude-opus-4-5", `{}`, 200_000, false},
		{"claude-sonnet-4-5", `{"plan":"max","longContextExtraUsage":true}`, 200_000, false},
		{"claude-opus-4-6", `{}`, 200_000, true},
		{"claude-opus-4-6", `{"plan":"max"}`, 1_000_000, false},
		{"claude-opus-4-6", `{"longContextExtraUsage":true}`, 1_000_000, false},
		{"claude-sonnet-4-6", `{"plan":"max"}`, 200_000, true},
		{"claude-sonnet-4-6", `{"longContextExtraUsage":true}`, 1_000_000, false},
		// The owner's own pin to 200K is a choice, not a problem to fix.
		{"claude-fable-5-2", `{"forceTwoHundredK":["claude-fable-5-2"]}`, 200_000, false},
	} {
		provider, err := js.ParseJSON(c.provider)
		if err != nil {
			t.Fatal(err)
		}
		got, fix := bridgeWindow(c.id, provider)
		if got != c.want || (fix != "") != c.fix {
			t.Errorf("bridgeWindow(%s, %s) = %d, %q; want %d, a fix %v", c.id, c.provider, got, fix, c.want, c.fix)
		}
	}
}

// T282: BridgeWindowWarnings, doctor's, is one warning per pi model of the
// active profile on claude-bridge that the pinned bridge serves below 1M,
// naming the model, its agents, the window and the fix; none for the
// default profile, a model with a row, or a provider other than the bridge.
func TestBridgeWindowWarnings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// pi counts as installed by its ~/.pi, with no pi on PATH (CI).
	if err := os.Mkdir(filepath.Join(dir, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JHON_SETTINGS", filepath.Join(dir, "settings.toml"))
	file := filepath.Join(dir, "profiles.json")
	t.Setenv("JHON_PROFILES", file)
	if got := BridgeWindowWarnings(repo); len(got) != 0 {
		t.Errorf("the default profile warns: %q", got)
	}
	for _, c := range []struct{ profiles, want string }{
		{`{"active":"work","profiles":{"work":{"agents":{"jhon-orchestrator":{"pi":"claude-bridge/claude-fable-5-2@xhigh"}}}}}`, ""},
		{`{"active":"work","profiles":{"work":{"agents":{"jhon-orchestrator":{"pi":"anthropic/claude-fable-5-3@xhigh"}}}}}`, ""},
		{`{"active":"work","profiles":{"work":{"agents":{"jhon-orchestrator":{"pi":"claude-bridge/claude-sonnet-4-5@high"}}}}}`, ""},
		{`{"active":"work","profiles":{"work":{"agents":{"jhon-orchestrator":{"pi":"claude-bridge/claude-fable-5-3@xhigh"},"jhon-builder":{"pi":"claude-bridge/claude-fable-5-3@high"}}}}}`,
			"pi model claude-bridge/claude-fable-5-3 (profile work: jhon-orchestrator, jhon-builder) runs with a 200,000-token context window, not 1,000,000: pi-claude-bridge " + PiClaudeBridgeVersion + ", the version Jhon pins, has no 1M row for it, so pi compacts it by 200,000 — update Jhon (jhon update), or pick a model the bridge serves at 1M (jhon profile set)"},
	} {
		if err := os.WriteFile(file, []byte(c.profiles), 0o644); err != nil {
			t.Fatal(err)
		}
		got := BridgeWindowWarnings(repo)
		if c.want == "" && len(got) != 0 || c.want != "" && !slices.Equal(got, []string{c.want}) {
			t.Errorf("%s:\ngot  %q\nwant %q", c.profiles, got, c.want)
		}
	}
}

// T50g (F7): pi's packages are pinned from the versions table, and every
// tool doctor compares names its command and pin.
func TestPiPackagesArePinnedFromTheVersionsTable(t *testing.T) {
	if piWebAccess != "npm:pi-web-access@"+PiWebAccessVersion {
		t.Errorf("pi-web-access = %s", piWebAccess)
	}
	if piClaudeBridge != "npm:pi-claude-bridge@"+PiClaudeBridgeVersion {
		t.Errorf("pi-claude-bridge = %s", piClaudeBridge)
	}
	if got := js.Prop(piPretty(), "source"); got != "npm:@heyhuynhgiabuu/pi-pretty@"+PiPrettyVersion {
		t.Errorf("pi-pretty source = %v", got)
	}
	want := map[string]Tool{"claude": {"claude", ClaudeCodeVersion, ""}, "engram": {"engram", EngramVersion, ""}, "codegraph": {"codegraph", CodeGraphVersion, ""}}
	// An optional integration's binary is compared only while its setting is on.
	for _, m := range integrations.Optional() {
		if m.Doctor.Binary != "" {
			want[m.Doctor.Binary] = Tool{m.Doctor.Binary, m.Doctor.Version, m.Setting}
		}
	}
	if len(Tools) != len(want) {
		t.Errorf("tools = %+v", Tools)
	}
	for _, tool := range Tools {
		if want[tool.Command].Version == "" || tool != want[tool.Command] {
			t.Errorf("tool %+v is not the table's", tool)
		}
	}
}
