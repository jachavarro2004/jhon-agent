package profiles

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

var testTiers = map[string]map[string]string{
	"claude-code": {"mid": "claude-sonnet-5-5@medium", "strong": "claude-opus-5-5@medium"},
	"pi":          {"mid": "claude-bridge/claude-sonnet-5-5@medium", "strong": "claude-bridge/claude-opus-5-5@medium"},
}

var testCtx = Context{Runtimes: []string{"claude-code", "other", "pi"}, Tiers: []string{"cheap", "mid", "strong", "frontier"}, Agents: []string{"worker", "review-risk"}}

func parse(t *testing.T, s string) any {
	t.Helper()
	v, err := js.ParseJSON(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTierValue(t *testing.T) {
	if v, ok := TierValue("claude-opus-5-5@high"); !ok || v != (Tier{"claude-opus-5-5", "high"}) {
		t.Errorf("TierValue = %v %v", v, ok)
	}
	if v, ok := TierValue("claude-bridge/claude-opus-5-5"); !ok || v != (Tier{Model: "claude-bridge/claude-opus-5-5"}) {
		t.Errorf("TierValue = %v %v", v, ok)
	}
	for _, bad := range []string{"x@", "@high", ""} {
		if _, ok := TierValue(bad); ok {
			t.Errorf("TierValue(%q) accepted", bad)
		}
	}
}

// An agent override beats a tier override, which beats agents.yaml.
func TestModelFor(t *testing.T) {
	profile := parse(t, `{"tiers": {"pi": {"mid": "claude-bridge/claude-sonnet-5-5@high"}}, "agents": {"review-risk": {"pi": "claude-bridge/claude-fable-5-1@high"}}}`)
	worker, risk := Agent{"worker", "mid"}, Agent{"review-risk", "strong"}
	for _, c := range []struct {
		profile       any
		runtime       string
		agent         Agent
		value, source string
	}{
		{profile, "pi", worker, "claude-bridge/claude-sonnet-5-5@high", "profile tier pi.mid"},
		{profile, "pi", risk, "claude-bridge/claude-fable-5-1@high", "profile agent review-risk.pi"},
		{profile, "claude-code", worker, "claude-sonnet-5-5@medium", "agents.yaml tiers.claude-code.mid"},
		{js.NewObject(), "pi", risk, "claude-bridge/claude-opus-5-5@medium", "agents.yaml tiers.pi.strong"},
	} {
		m, ok := ModelFor(testTiers, c.profile, c.runtime, c.agent)
		if !ok || m.Value != c.value || m.Source != c.source {
			t.Errorf("ModelFor(%s, %s) = %v %v", c.runtime, c.agent.Name, m, ok)
		}
	}
	if _, ok := ModelFor(testTiers, js.NewObject(), "other", worker); ok {
		t.Error("an undeclared tier resolved")
	}
}

func TestReadAbsentIsDefault(t *testing.T) {
	f, problems := Read(filepath.Join(t.TempDir(), "profiles.json"))
	if len(problems) > 0 || f.HasActive || len(f.Profiles.Keys()) > 0 {
		t.Errorf("Read = %v %v", f, problems)
	}
	if name, p := Active(f); name != Default || js.StringifyValue(p) != "{}" {
		t.Errorf("Active = %s %v", name, p)
	}
}

func TestActiveAndUndefinedActive(t *testing.T) {
	f := File{Active: "cheap", HasActive: true, Profiles: parse(t, `{"cheap": {"tiers": {"claude-code": {"strong": "claude-sonnet-5-5@medium"}}}}`).(*js.Object)}
	if name, p := Active(f); name != "cheap" || p != js.Prop(f.Profiles, "cheap") {
		t.Errorf("Active = %s %v", name, p)
	}
	if p := Problems(f, testCtx); len(p) > 0 {
		t.Errorf("Problems = %v", p)
	}
	ghost := File{Active: "ghost", HasActive: true, Profiles: js.NewObject()}
	if p := strings.Join(Problems(ghost, testCtx), "\n"); !strings.Contains(p, "active profile 'ghost' is not defined") {
		t.Errorf("Problems = %q", p)
	}
}

func TestProblems(t *testing.T) {
	f := File{Profiles: parse(t, `{"bad": {
		"tiers": {"emacs": {"mid": "x"}, "claude-code": {"huge": "x", "mid": "claude-sonnet-5-5@turbo", "strong": "@high"}},
		"agents": {"ghost": {"pi": "x"}, "worker": {"pi": "claude-bridge/claude-opus-5-5@loud"}},
		"extra": 1}}`).(*js.Object)}
	want := []string{
		"bad: unknown key 'extra'",
		"bad.tiers: emacs is not a runtime",
		"bad.tiers.claude-code: huge is not a tier",
		"bad.tiers.claude-code.mid: effort 'turbo' is not one of low, medium, high, xhigh, max",
		"bad.tiers.claude-code.strong: write the value as model or model@effort",
		"bad.agents.worker.pi: thinking 'loud' is not one of off, minimal, low, medium, high, xhigh, max",
	}
	if got := Problems(f, testCtx); !reflect.DeepEqual(got, want) {
		t.Errorf("Problems =\n%s", strings.Join(got, "\n"))
	}
}

func TestReadMalformed(t *testing.T) {
	dir := t.TempDir()
	for name, c := range map[string]struct{ text, want string }{
		"a.json": {"{ nope", "is not valid JSON"},
		"b.json": {`{"profiles": []}`, "profiles must be an object"},
		"c.json": {`[]`, "must hold a JSON object"},
		"d.json": {`{"active": 1}`, "active must be a profile name"},
	} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(c.text), 0o644)
		if _, problems := Read(p); len(problems) != 1 || !strings.HasSuffix(problems[0], c.want) {
			t.Errorf("Read(%s) = %v", name, problems)
		}
	}
}

// WriteActive keeps the file's other keys, in their order.
func TestWriteActive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "profiles.json")
	if err := WriteActive(p, "x"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "{\n  \"profiles\": {},\n  \"active\": \"x\"\n}\n" {
		t.Errorf("new file = %q", b)
	}
	os.WriteFile(p, []byte(`{"active":"a","note":1.50,"2":true,"profiles":{"a":{}}}`), 0o644)
	if err := WriteActive(p, "default"); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"2\": true,\n  \"active\": \"default\",\n  \"note\": 1.5,\n  \"profiles\": {\n    \"a\": {}\n  }\n}\n"
	if b, _ := os.ReadFile(p); string(b) != want {
		t.Errorf("rewritten = %q", b)
	}
}

// A profile naming an agent the manifest no longer has (one retired) is a
// warning, not a problem: its entry is ignored and the projection runs (L48).
func TestRetiredAgentIsAWarning(t *testing.T) {
	f := File{Profiles: parse(t, `{"mine": {"agents": {"review-gone": {"pi": "x"}, "worker": {"pi": "claude-bridge/claude-opus-5-5@high"}}}}`).(*js.Object)}
	if got := Problems(f, testCtx); len(got) != 0 {
		t.Errorf("Problems = %v, want none", got)
	}
	want := []string{"mine.agents: review-gone is not an agent; its entry is ignored"}
	if got := Warnings(f, testCtx); !reflect.DeepEqual(got, want) {
		t.Errorf("Warnings = %v, want %v", got, want)
	}
}
