package render

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

// fixedContext is the text every first call of runtime carries before any
// task: the always-on layer as AlwaysOnBundle gives it plus the
// orchestrator's body as that runtime renders it (T146 FA3).
func fixedContext(e *engine, runtime string) (bundle, body int) {
	return len(e.alwaysOnBundle(runtime)), len(e.bodyFor(runtime, e.mainAgent(piMain)))
}

// allOptionalOn is a settings file with every optional integration on: the
// largest fixed context an owner can choose.
func allOptionalOn() string {
	sections := map[string][]string{}
	for _, m := range integrations.Optional() {
		if m.Setting != "" {
			i := strings.LastIndex(m.Setting, ".")
			sections[m.Setting[:i]] = append(sections[m.Setting[:i]], m.Setting[i+1:]+" = true")
		}
	}
	return settingsFile(sections)
}

// settingsFile is a schema 1 settings file of sections, each a table's
// "key = value" lines, the tables sorted.
func settingsFile(sections map[string][]string) string {
	text := "schema = 1\n"
	names := make([]string, 0, len(sections))
	for name := range sections {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		text += fmt.Sprintf("\n[%s]\n%s\n", name, strings.Join(sections[name], "\n"))
	}
	return text
}

// T146 FA3a (L1218): the fixed context is at most 70% of fix/t145b's
// (Claude Code 32879 bytes, pi 32661), each rule moved out of it read on
// demand (ondemand.go). Its pointers name the install's real path (T146c
// K3), so it is measured at a representative home, not the test's long
// temporary one.
//
// T264 (L1735): it is measured at the worst case on purpose, every optional
// integration on through a settings file of the test's own, so it fails the
// same for the owner and in CI: it once read the caller's settings, and
// passed in CI with the defaults while the owner's jhon-studio line put it
// over.
func TestFixedContextBytes(t *testing.T) {
	t.Setenv("JHON_SETTINGS", writeFile(t, allOptionalOn()))
	for _, m := range integrations.Optional() {
		if m.Setting != "" && !settings.On(m.Setting) {
			t.Fatalf("%s is not on in the worst case:\n%s", m.Setting, allOptionalOn())
		}
	}
	e := engineAt(t, repo, "--validate")
	e.home = "/home/owner"
	for rt, before := range map[string]int{"claude-code": 32879, "pi": 32661} {
		b, o := fixedContext(e, rt)
		t.Logf("%s: bundle %d + orchestrator %d = %d (fix/t145b %d, %.1f%%)", rt, b, o, b+o, before, 100*float64(b+o)/float64(before))
		if limit := before * 70 / 100; b+o > limit {
			t.Errorf("%s's fixed context with every optional integration on is %d bytes, over 70%% of fix/t145b's %d (%d)", rt, b+o, before, limit)
		}
	}
}
