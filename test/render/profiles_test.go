// Model profiles: named overrides of the models agents.yaml assigns, kept in
// the owner's ~/.config/jhon/profiles.json (or JHON_PROFILES), outside the
// framework. A profile overrides a runtime's tier, or one agent's model in one
// runtime; `default` is agents.yaml itself. `jhon profile use` activates one
// and re-renders every runtime from the active release. Reading, resolving,
// and checking profiles is unit-tested in internal/profiles.
package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// profileHome is a HOME whose active release is a copy of this tree, with
// ~/.pi present, and the path of its profiles file.
func profileHome(t *testing.T) (home, profiles string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	share := filepath.Join(home, ".local", "share", "jhon")
	release := filepath.Join(share, "releases", strings.Repeat("a", 40))
	copyTree(t, repoRoot, release)
	if err := os.Symlink(release, filepath.Join(share, "current")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o777); err != nil {
		t.Fatal(err)
	}
	return home, filepath.Join(home, ".config", "jhon", "profiles.json")
}

// profileRun runs jhon as this checkout's own build/jhon would run: the
// checkout is its runtime (JHON_ROOT, since the test binary lives elsewhere)
// and its source.
func profileRun(t *testing.T, home string, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, "", support.Env("HOME="+home, "JHON_SOURCE="+repoRoot, "JHON_ROOT="+repoRoot, support.RuntimesPath(t)), args...)
}

func TestProfiles(t *testing.T) {
	t.Parallel()

	t.Run("jhon profile lists default and the file's profiles, marking the active one, each with pi's provider", func(t *testing.T) {
		t.Parallel()
		home, profiles := profileHome(t)
		write(t, profiles, `{"active":"mixed","profiles":{"mixed":{},"cheap":{},"api":{"tiers":{"pi":{"strong":"anthropic/claude-opus-4-5@medium"}}}}}`)
		r := profileRun(t, home, "profile")
		wantCode(t, r, 0)
		match(t, r.Out(), `(?m)^  default  pi: claude-bridge$`)
		match(t, r.Out(), `(?m)^\* mixed    pi: claude-bridge$`)
		match(t, r.Out(), `(?m)^  cheap    pi: claude-bridge$`)
		match(t, r.Out(), `(?m)^  api      pi: anthropic$`)
	})

	t.Run("jhon profile show names each agent's model in each runtime and where it comes from", func(t *testing.T) {
		t.Parallel()
		home, profiles := profileHome(t)
		write(t, profiles, `{"profiles":{"mixed":{"agents":{"prism-safety":{"pi":"claude-bridge/claude-fable-5-1@high"}}}}}`)
		r := profileRun(t, home, "profile", "show", "mixed")
		wantCode(t, r, 0)
		match(t, r.Out(), `(?m)^  prism-safety +pi +claude-bridge/claude-fable-5-1@high +profile agent prism-safety\.pi$`)
		match(t, r.Out(), `(?m)^  jhon-builder +claude-code +claude-opus-5-5@medium +agents\.yaml tiers\.claude-code\.strong$`)
	})

	t.Run("jhon profile json gives the shell's panel every profile, the active one, and each agent's model in each runtime", func(t *testing.T) {
		t.Parallel()
		home, profiles := profileHome(t)
		write(t, profiles, `{"active":"mixed","profiles":{"mixed":{"agents":{"prism-safety":{"pi":"claude-bridge/claude-fable-5-1@high"}}}}}`)
		r := profileRun(t, home, "profile", "json")
		wantCode(t, r, 0)
		var got struct {
			Active   string
			Profiles []struct {
				Name     string
				Provider string
				Agents   []map[string]string
			}
		}
		if err := json.Unmarshal([]byte(r.Stdout), &got); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, r.Stdout)
		}
		equal(t, got.Active, "mixed")
		equal(t, len(got.Profiles), 2)
		equal(t, got.Profiles[0].Name, "default")
		equal(t, []string{got.Profiles[0].Provider, got.Profiles[1].Provider}, []string{"claude-bridge", "claude-bridge"}, "pi's provider, per profile")
		found := false
		for _, a := range got.Profiles[1].Agents {
			found = found || (a["agent"] == "prism-safety" && a["runtime"] == "pi" && a["model"] == "claude-bridge/claude-fable-5-1@high" && a["source"] == "profile agent prism-safety.pi")
		}
		equal(t, found, true)
	})

	t.Run("jhon profile new, set, and rm edit the profiles, each checked whole before it is written", func(t *testing.T) {
		t.Parallel()
		home, profiles := profileHome(t)
		write(t, profiles, `{"profiles":{"base":{"agents":{"jhon-checker":{"pi":"claude-bridge/claude-opus-5-5@high"}}}}}`)
		wantCode(t, profileRun(t, home, "profile", "new", "fast", "--from", "base"), 0)
		wantCode(t, profileRun(t, home, "profile", "set", "fast", "jhon-builder", "pi", "claude-bridge/claude-sonnet-5-5@low"), 0)
		got := readJSON(t, profiles).(map[string]any)["profiles"].(map[string]any)["fast"].(map[string]any)["agents"].(map[string]any)
		equal(t, got["jhon-builder"].(map[string]any)["pi"], "claude-bridge/claude-sonnet-5-5@low")
		equal(t, got["jhon-checker"].(map[string]any)["pi"], "claude-bridge/claude-opus-5-5@high", "a copy of base")
		before := support.Read(t, profiles)
		bad := profileRun(t, home, "profile", "set", "fast", "jhon-builder", "pi", "m@ultra")
		wantCode(t, bad, 1)
		match(t, bad.Out(), `thinking 'ultra' is not one of`)
		equal(t, support.Read(t, profiles), before, "a refused edit writes nothing")
		wantCode(t, profileRun(t, home, "profile", "set", "fast", "jhon-builder", "pi", "-"), 0)
		_, has := readJSON(t, profiles).(map[string]any)["profiles"].(map[string]any)["fast"].(map[string]any)["agents"].(map[string]any)["jhon-builder"]
		equal(t, has, false, "- puts the agent back on agents.yaml")
		match(t, profileRun(t, home, "profile", "new", "default").Out(), `agents.yaml itself`)
		match(t, profileRun(t, home, "profile", "new", "base").Out(), `already exists`)
		wantCode(t, profileRun(t, home, "profile", "rm", "base"), 0)
		_, has = readJSON(t, profiles).(map[string]any)["profiles"].(map[string]any)["base"]
		equal(t, has, false)
	})

	t.Run("jhon profile set on the active profile re-renders every runtime; rm refuses the active one", func(t *testing.T) {
		t.Parallel()
		home, profiles := profileHome(t)
		write(t, profiles, `{"active":"mine","profiles":{"mine":{}}}`)
		wantCode(t, profileRun(t, home, "profile", "set", "mine", "prism-safety", "pi", "claude-bridge/claude-fable-5-1@high"), 0)
		match(t, support.Read(t, filepath.Join(home, ".pi", "jhon", "agents", "prism-safety.md")), `(?m)^model: claude-bridge/claude-fable-5-1\nthinking: high$`)
		r := profileRun(t, home, "profile", "rm", "mine")
		wantCode(t, r, 1)
		match(t, r.Out(), `mine is the active profile`)
	})

	t.Run("jhon profile use activates a profile and re-renders from the active release; use default restores agents.yaml", func(t *testing.T) {
		t.Parallel()
		home, profiles := profileHome(t)
		write(t, profiles, `{"profiles":{"mixed":{"tiers":{"claude-code":{"strong":"claude-opus-5-5@high"}},"agents":{"prism-safety":{"pi":"claude-bridge/claude-fable-5-1@high"}}}}}`)
		r := profileRun(t, home, "profile", "use", "mixed")
		wantCode(t, r, 0)
		equal(t, readJSON(t, profiles).(map[string]any)["active"], "mixed")
		match(t, support.Read(t, filepath.Join(home, ".claude", "agents", "jhon-builder.md")), `(?m)^model: claude-opus-5-5\neffort: high$`)
		match(t, support.Read(t, filepath.Join(home, ".pi", "jhon", "agents", "prism-safety.md")), `(?m)^model: claude-bridge/claude-fable-5-1\nthinking: high$`)
		back := profileRun(t, home, "profile", "use", "default")
		wantCode(t, back, 0)
		match(t, support.Read(t, filepath.Join(home, ".claude", "agents", "jhon-builder.md")), `(?m)^model: claude-opus-5-5\neffort: medium$`)
	})

	t.Run("the active profile is settings' pi.profile, else profiles.json's active; jhon profile use writes both (T56b)", func(t *testing.T) {
		t.Parallel()
		home, profiles := profileHome(t)
		write(t, profiles, `{"active":"cheap","profiles":{"mixed":{},"cheap":{}}}`)
		match(t, profileRun(t, home, "profile").Out(), `(?m)^\* cheap +pi: claude-bridge$`, "no settings.toml: profiles.json's active")
		settingsToml := filepath.Join(home, ".config", "jhon", "settings.toml")
		write(t, settingsToml, "schema = 1\n\n# the owner's note\n[pi]\nprofile = \"mixed\"\n")
		list := profileRun(t, home, "profile")
		wantCode(t, list, 0)
		match(t, list.Out(), `(?m)^\* mixed +pi: claude-bridge$`, "settings' pi.profile wins")
		match(t, list.Out(), `(?m)^  cheap +pi: claude-bridge$`)
		match(t, profileRun(t, home, "profile", "json").Stdout, `"active":"mixed"`)
		match(t, profileRun(t, home, "profile", "show").Out(), `(?m)^profile mixed$`)
		use := profileRun(t, home, "profile", "use", "cheap")
		wantCode(t, use, 0)
		match(t, use.Stdout, `^profile cheap active\n`)
		equal(t, support.Read(t, settingsToml), "schema = 1\n\n# the owner's note\n[pi]\nprofile = \"cheap\"\n", "pi.profile written, the comment kept")
		equal(t, readJSON(t, profiles).(map[string]any)["active"], "cheap", "profiles.json follows, for older readers")
		match(t, profileRun(t, home, "profile").Out(), `(?m)^\* cheap +pi: claude-bridge$`)
	})

	t.Run("jhon profile use with unwritable settings writes nothing (T56c)", func(t *testing.T) {
		t.Parallel()
		home, profiles := profileHome(t)
		body := `{"active":"mixed","profiles":{"mixed":{},"cheap":{}}}`
		write(t, profiles, body)
		settingsToml := filepath.Join(home, ".config", "jhon", "settings.toml")
		write(t, settingsToml, "[broken\n")
		use := profileRun(t, home, "profile", "use", "cheap")
		wantCode(t, use, 1)
		equal(t, use.Stderr, "jhon profile: ~/.config/jhon/settings.toml: "+settingsToml+": toml: line 2: expected '.' or ']' to end table name, but got '\\n' instead\n")
		equal(t, support.Read(t, profiles), body, "profiles.json untouched")
		equal(t, support.Read(t, settingsToml), "[broken\n", "settings.toml untouched")
		match(t, profileRun(t, home, "profile").Out(), `(?m)^\* mixed +pi: claude-bridge$`)
	})

	t.Run("a pi.profile naming no profile is blamed on settings.toml (T56c)", func(t *testing.T) {
		t.Parallel()
		home, profiles := profileHome(t)
		write(t, profiles, `{"profiles":{"mixed":{}}}`)
		write(t, filepath.Join(home, ".config", "jhon", "settings.toml"), "schema = 1\n[pi]\nprofile = \"ghost\"\n")
		r := install(t, home, repoRoot)
		wantCode(t, r, 1)
		match(t, r.Out(), `(?m)^profile: 1 problem\(s\) in ~/\.config/jhon/profiles\.json \(active: ghost, from pi\.profile in ~/\.config/jhon/settings\.toml\)$`)
	})

	t.Run("an active profile naming no profile only in profiles.json is blamed on profiles.json", func(t *testing.T) {
		t.Parallel()
		home, profiles := profileHome(t)
		write(t, profiles, `{"active":"ghost","profiles":{"mixed":{}}}`)
		write(t, filepath.Join(home, ".config", "jhon", "settings.toml"), "schema = 1\n")
		r := install(t, home, repoRoot)
		wantCode(t, r, 1)
		match(t, r.Out(), `(?m)^profile: 1 problem\(s\) in ~/\.config/jhon/profiles\.json \(active: ghost\)$`)
		noMatch(t, r.Out(), `from pi\.profile`)
	})

	t.Run("jhon profile use refuses an unknown profile and an invalid file, and changes nothing", func(t *testing.T) {
		t.Parallel()
		home, profiles := profileHome(t)
		write(t, profiles, `{"profiles":{"bad":{"tiers":{"emacs":{}}}}}`)
		unknown := profileRun(t, home, "profile", "use", "ghost")
		wantCode(t, unknown, 1)
		match(t, unknown.Out(), `no profile 'ghost'`)
		invalid := profileRun(t, home, "profile", "use", "bad")
		wantCode(t, invalid, 1)
		match(t, invalid.Out(), `emacs is not a runtime`)
		if _, ok := readJSON(t, profiles).(map[string]any)["active"]; ok {
			t.Error("a profile was activated")
		}
		equal(t, exists(filepath.Join(home, ".claude", "agents")), false, "nothing was rendered")
		equal(t, exists(filepath.Join(home, ".config", "jhon", "settings.toml")), false, "no settings written")
	})
}
