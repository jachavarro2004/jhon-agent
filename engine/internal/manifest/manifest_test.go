package manifest

import (
	"reflect"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

func TestParse(t *testing.T) {
	src := `version: 1
tiers:
  other: { cheap: model-a@medium, strong: model-b@medium }   # a comment
  claude-code: { mid: claude-sonnet-5-5 }
capabilities:
  read: [Read, Grep]
  memory-read: [mcp__engram__mem_search]
inspect_allow: [git diff, git show]
agents:
  - name: worker
    tier: mid
    caps: [read, memory-read]
    maxTurns: 80
    description: "Do — the work"
    vars:
      tier: not-this
      quoted: "a, b"
  - name: review-b
    note: x
    tier: strong
    template: prism-facet
  - name: untiered
runtimes: [claude-code, other, pi] # three
`
	m := Parse(src)
	if len(m.Issues) != 0 {
		t.Fatalf("issues = %v", m.Issues)
	}
	if !reflect.DeepEqual(m.Tiers.Keys(), []string{"other", "claude-code"}) || m.Tier("other", "strong") != "model-b@medium" {
		t.Errorf("tiers = %v", m.TierTable())
	}
	if !reflect.DeepEqual(m.TierNames(), []string{"cheap", "strong", "mid"}) {
		t.Errorf("tier names = %v", m.TierNames())
	}
	if caps, _ := m.Capabilities.Get("memory-read"); !reflect.DeepEqual(caps, []string{"mcp__engram__mem_search"}) {
		t.Errorf("a hyphenated capability = %v", caps)
	}
	if !reflect.DeepEqual(m.InspectAllow, []string{"git diff", "git show"}) {
		t.Errorf("inspect_allow = %v", m.InspectAllow)
	}
	if !reflect.DeepEqual(m.AgentNames(), []string{"worker", "review-b", "untiered"}) {
		t.Fatalf("agents = %v", m.AgentNames())
	}
	w := m.Agents[0]
	if w.Tier != "mid" || w.MaxTurns != "80" || w.Description != "Do — the work" || !reflect.DeepEqual(w.Caps, []string{"read", "memory-read"}) {
		t.Errorf("worker = %+v", w)
	}
	if v, _ := w.Vars.Get("quoted"); v != "a, b" || !reflect.DeepEqual(w.Vars.Keys(), []string{"tier", "quoted"}) {
		t.Errorf("vars = %v", w.Vars.Keys())
	}
	u := m.Agents[2]
	if u.TierName() != "undefined" || u.Caps != nil || u.Description != (js.Undefined{}) {
		t.Errorf("untiered = %+v", u)
	}
	if !reflect.DeepEqual(m.Matrix.Runtimes, []string{"claude-code", "other", "pi"}) {
		t.Errorf("runtimes = %v", m.Matrix.Runtimes)
	}
}

// A line the reader cannot place is reported, never skipped: a misspelled
// key silently dropped is a setting the author believes is set.
func TestParseIssues(t *testing.T) {
	m := Parse(`tierz: 1
tiers:
  other: nope
agents:
  - name: a
    tierr: mid
    tier:
    description: not json
    vars: x
    vars:
      ok: fine
      bad
      q: "unterminated
oops
`)
	want := []Issue{
		{1, "unknown top-level key 'tierz'"},
		{3, "cannot parse tiers entry: other: nope"},
		{6, "a: unknown key 'tierr'"},
		{7, "a: 'tier' has no value"},
		{8, "a: description is not a JSON string"},
		{9, "a: vars must be a block of 'name: value' lines"},
		{12, "cannot parse vars entry: bad"},
		{13, `cannot parse vars value: q: "unterminated`},
		{14, "cannot parse: oops"},
	}
	if !reflect.DeepEqual(m.Issues, want) {
		t.Errorf("issues =\n%v\nwant\n%v", m.Issues, want)
	}
}
