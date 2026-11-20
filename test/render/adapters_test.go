// The runtimes are adapters (S37, T56): settings.runtimes picks Claude Code
// and/or pi, and an install or a check projects onto, and compares, only the
// runtimes that are on. pi's provider is the active profile's: claude-bridge
// by default, any pi provider a profile names otherwise.
package render

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// settingsFile is the owner's settings.toml under home.
func settingsFile(home string) string {
	return filepath.Join(home, ".config", "jhon", "settings.toml")
}

// snapshot is every entry under dir: a file's bytes, a link's target, a
// directory's name; nil when dir is absent.
func snapshot(t testing.TB, dir string) map[string]string {
	t.Helper()
	if !exists(dir) {
		return nil
	}
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			out[p] = "-> " + target
		case d.IsDir():
			out[p] = "dir"
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[p] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// apiProfile is a profile on pi's anthropic provider (an API key), every
// tier its own model.
const apiProfile = `{"active":"api","profiles":{"api":{"tiers":{"pi":{` +
	`"cheap":"anthropic/claude-haiku-4-5@medium","mid":"anthropic/claude-sonnet-4-5@medium",` +
	`"strong":"anthropic/claude-opus-4-5@medium","frontier":"anthropic/claude-opus-4-5@high"}}}}}`

// ownerBridge is an owner's own claude-bridge.json, AskClaude on.
const ownerBridge = `{"owner":true,"askClaude":{"enabled":true,"allowFullMode":true},"provider":{"plan":"max"}}` + "\n"

// apiWithClaude and apiAlone are pi renders of apiProfile, shared read-only:
// with Claude Code one of the install's runtimes, and with it turned off.
var apiWithClaude, apiAlone lazyHome

func apiHome(t *testing.T, l *lazyHome, settings string) (home, pi string) {
	t.Helper()
	home = l.get(t, func(home string) string {
		_, _, err := piHomeAt(t, home, func(h string) {
			write(t, filepath.Join(h, ".config", "jhon", "profiles.json"), apiProfile)
			write(t, filepath.Join(h, ".pi", "agent", "claude-bridge.json"), ownerBridge)
			if settings != "" {
				write(t, settingsFile(h), settings)
			}
		})
		return err
	})
	return home, filepath.Join(home, ".pi", "jhon")
}

func TestRuntimesAsAdapters(t *testing.T) {
	t.Parallel()

	t.Run("runtimes.claude_code = false: an install and a check touch no ~/.claude path, and say Claude Code is skipped; pi is installed", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		write(t, settingsFile(home), "schema = 1\n\n[runtimes]\nclaude_code = false\n")
		write(t, filepath.Join(home, ".pi", "agent", "auth.json"), "{}\n")
		// What an earlier release left: a block in CLAUDE.md and an agent.
		write(t, filepath.Join(home, ".claude", "CLAUDE.md"), "@RTK.md\n\n<!-- jhon:start -->\n<!-- jhon:end -->\n")
		write(t, filepath.Join(home, ".claude", "agents", "jhon-builder.md"), "stale\n")
		before := snapshot(t, filepath.Join(home, ".claude"))

		r := install(t, home, repoRoot)
		wantCode(t, r, 0)
		match(t, r.Out(), `(?m)^skip: claude-code \(runtimes\.claude_code = false\); ~/\.claude is left as is$`)
		noMatch(t, r.Out(), `(?m)^claude-code:$`)
		equal(t, snapshot(t, filepath.Join(home, ".claude")), before, "the install touched ~/.claude")
		if !exists(filepath.Join(home, ".pi", "jhon", "settings.json")) {
			t.Fatal("pi was not installed\n" + r.Out())
		}

		c := render(t, home, "--check")
		wantCode(t, c, 0)
		match(t, c.Out(), `(?m)^skip: claude-code \(runtimes\.claude_code = false\); ~/\.claude is left as is$`)
		noMatch(t, c.Out(), `DRIFT`)
		match(t, c.Out(), `(?m)^up to date$`)
		equal(t, snapshot(t, filepath.Join(home, ".claude")), before, "the check touched ~/.claude")
	})

	t.Run("runtimes.claude_code = false with no ~/.claude: nothing under it is created", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		write(t, settingsFile(home), "[runtimes]\nclaude_code = false\n")
		write(t, filepath.Join(home, ".pi", "agent", "auth.json"), "{}\n")
		wantCode(t, install(t, home, repoRoot), 0)
		if exists(filepath.Join(home, ".claude")) {
			t.Fatal("~/.claude was created")
		}
	})

	t.Run("runtimes.pi = false: an install and a check touch no ~/.pi/jhon path, and say pi is skipped; a pi turned off later is left as is, with no drift", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		write(t, filepath.Join(home, ".pi", "agent", "auth.json"), "{}\n")
		write(t, settingsFile(home), "[runtimes]\npi = false\n")
		r := install(t, home, repoRoot)
		wantCode(t, r, 0)
		match(t, r.Out(), `(?m)^skip: pi \(runtimes\.pi = false\); ~/\.pi/jhon is left as is$`)
		noMatch(t, r.Out(), `(?m)^pi:$`)
		if exists(filepath.Join(home, ".pi", "jhon")) {
			t.Fatal("~/.pi/jhon was created")
		}
		if !exists(filepath.Join(home, ".claude", "agents", "jhon-builder.md")) {
			t.Fatal("Claude Code was not installed\n" + r.Out())
		}

		// Both on, then pi off: what pi has stays, stale or not.
		home2 := t.TempDir()
		write(t, filepath.Join(home2, ".pi", "agent", "auth.json"), "{}\n")
		wantCode(t, install(t, home2, repoRoot), 0)
		write(t, filepath.Join(home2, ".pi", "jhon", "settings.json"), "{}\n")
		write(t, settingsFile(home2), "[runtimes]\npi = false\n")
		before := snapshot(t, filepath.Join(home2, ".pi"))
		c := render(t, home2, "--check")
		wantCode(t, c, 0)
		noMatch(t, c.Out(), `DRIFT`)
		match(t, c.Out(), `(?m)^skip: pi \(runtimes\.pi = false\); ~/\.pi/jhon is left as is$`)
		wantCode(t, install(t, home2, repoRoot), 0)
		equal(t, snapshot(t, filepath.Join(home2, ".pi")), before, "an install with pi off touched ~/.pi")
	})

	t.Run("both runtimes on (no settings.toml): both are installed and nothing is skipped", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		write(t, filepath.Join(home, ".pi", "agent", "auth.json"), "{}\n")
		r := install(t, home, repoRoot)
		wantCode(t, r, 0)
		noMatch(t, r.Out(), `(?m)^skip: `)
		match(t, r.Out(), `(?m)^claude-code:$`)
		match(t, r.Out(), `(?m)^pi:$`)
	})

	t.Run("the default profile renders pi's settings.json byte for byte as before: claude-bridge, its package, its config and wrapper", func(t *testing.T) {
		t.Parallel()
		home, pi := sharedPiHome(t)
		current := filepath.Join(home, ".local", "share", "jhon", "current")
		want := `{
  "packages": [
    "npm:pi-claude-bridge@0.9.3",
    "npm:pi-web-access@0.43.0",
    {
      "source": "npm:@heyhuynhgiabuu/pi-pretty@0.6.27",
      "extensions": [],
      "skills": [],
      "prompts": [],
      "themes": []
    },
    "` + filepath.Join(current, "shell") + `"
  ],
  "extensions": [
    "-builtin:codemode"
  ],
  "skills": [
    "` + filepath.Join(current, "prose", "skills") + `"
  ],
  "defaultProvider": "claude-bridge",
  "defaultModel": "claude-opus-5-5",
  "defaultThinkingLevel": "medium",
  "theme": "rose-ink",
  "quietStartup": true
}
`
		equal(t, support.Read(t, filepath.Join(pi, "settings.json")), want)
		for _, f := range []string{"claude-bridge.json", "claude-code-provider"} {
			if !exists(filepath.Join(pi, f)) {
				t.Error("the default profile lacks", f)
			}
		}
	})

	t.Run("a profile on the bridge keeps the owner's AskClaude as written: claude-bridge.json byte for byte as before", func(t *testing.T) {
		t.Parallel()
		_, pi := piHome(t, func(h string) {
			write(t, filepath.Join(h, ".pi", "agent", "claude-bridge.json"), ownerBridge)
		})
		equal(t, support.Read(t, filepath.Join(pi, "claude-bridge.json")), `{
  "owner": true,
  "askClaude": {
    "enabled": true,
    "allowFullMode": true
  },
  "provider": {
    "plan": "max",
    "pathToClaudeCodeExecutable": "`+filepath.Join(pi, "claude-code-provider")+`"
  }
}
`)
	})

	t.Run("a profile on provider anthropic, Claude Code a runtime of the install: pi-claude-bridge is in the packages as a provider only, its config the owner's with the wrapper and AskClaude off (T130, L909)", func(t *testing.T) {
		t.Parallel()
		home, pi := apiHome(t, &apiWithClaude, "")
		s := readJSON(t, filepath.Join(pi, "settings.json")).(map[string]any)
		equal(t, []any{s["defaultProvider"], s["defaultModel"], s["defaultThinkingLevel"]}, []any{"anthropic", "claude-opus-4-5", "medium"})
		equal(t, s["packages"], piPackages(filepath.Join(home, ".local", "share", "jhon", "current")), "pi-claude-bridge first, as a bridged profile lists it")
		equal(t, support.Read(t, filepath.Join(pi, "claude-bridge.json")), `{
  "owner": true,
  "askClaude": {
    "enabled": false,
    "allowFullMode": true
  },
  "provider": {
    "plan": "max",
    "pathToClaudeCodeExecutable": "`+filepath.Join(pi, "claude-code-provider")+`"
  }
}
`)
		_, bridged := sharedPiHome(t)
		equal(t, support.Read(t, filepath.Join(pi, "claude-code-provider")), support.Read(t, filepath.Join(bridged, "claude-code-provider")), "the wrapper a bridged profile gets")
		if fi, err := os.Stat(filepath.Join(pi, "claude-code-provider")); err != nil || fi.Mode()&0o111 == 0 {
			t.Fatalf("the wrapper is not executable: %v", err)
		}
		equal(t, support.Read(t, filepath.Join(home, ".pi", "agent", "claude-bridge.json")), ownerBridge, "the owner's own file")
		c := render(t, home, "--check", "--only", "pi")
		wantCode(t, c, 0)
		noMatch(t, c.Out(), `DRIFT`)
	})

	t.Run("that profile's session is what it was without the bridge: every file under ~/.pi/jhon but settings.json's packages, the bridge's config and its wrapper (L708, L909)", func(t *testing.T) {
		t.Parallel()
		homeWith, piWith := apiHome(t, &apiWithClaude, "")
		homeAlone, piAlone := apiHome(t, &apiAlone, "[runtimes]\nclaude_code = false\n")
		// Each home's own path, taken out of names and contents.
		flat := func(home, pi string) map[string]string {
			out := map[string]string{}
			for p, body := range snapshot(t, pi) {
				out[strings.ReplaceAll(p, home, "~")] = strings.ReplaceAll(body, home, "~")
			}
			return out
		}
		with, alone := flat(homeWith, piWith), flat(homeAlone, piAlone)
		for _, f := range []string{"claude-bridge.json", "claude-code-provider"} {
			if _, ok := with["~/.pi/jhon/"+f]; !ok {
				t.Error("with Claude Code, the render lacks", f)
			}
			delete(with, "~/.pi/jhon/"+f)
		}
		settings := func(home, pi string) map[string]any {
			var v map[string]any
			if err := json.Unmarshal([]byte(strings.ReplaceAll(support.Read(t, filepath.Join(pi, "settings.json")), home, "~")), &v); err != nil {
				t.Fatal(err)
			}
			return v
		}
		sWith, sAlone := settings(homeWith, piWith), settings(homeAlone, piAlone)
		equal(t, sWith["packages"].([]any)[0], "npm:pi-claude-bridge@0.9.3")
		sWith["packages"] = sWith["packages"].([]any)[1:]
		equal(t, sWith, sAlone, "settings.json, the bridge's package aside")
		delete(with, "~/.pi/jhon/settings.json")
		delete(alone, "~/.pi/jhon/settings.json")
		equal(t, with, alone, "the agent files, the prompts, jhon.json, mcp.json and the rest")
	})

	t.Run("a profile on provider anthropic, Claude Code not a runtime of the install (runtimes.claude_code = false): pi's defaultProvider anthropic, and neither pi-claude-bridge nor its config and wrapper, as before", func(t *testing.T) {
		t.Parallel()
		home, pi := apiHome(t, &apiAlone, "[runtimes]\nclaude_code = false\n")
		s := readJSON(t, filepath.Join(pi, "settings.json")).(map[string]any)
		equal(t, []any{s["defaultProvider"], s["defaultModel"], s["defaultThinkingLevel"]}, []any{"anthropic", "claude-opus-4-5", "medium"})
		pkgs := piPackages(filepath.Join(home, ".local", "share", "jhon", "current"))[1:]
		equal(t, s["packages"], pkgs, "no pi-claude-bridge")
		for _, f := range []string{"claude-bridge.json", "claude-code-provider"} {
			if exists(filepath.Join(pi, f)) {
				t.Error("a non-bridge profile installed", f)
			}
		}
		match(t, frontmatter(support.Read(t, filepath.Join(pi, "agents", "jhon-builder.md"))), `(?m)^model: anthropic/claude-opus-4-5\nthinking: medium$`)
		c := render(t, home, "--check", "--only", "pi")
		wantCode(t, c, 0)
		noMatch(t, c.Out(), `DRIFT`)
	})

	t.Run("switching from the bridge to another provider leaves the bridge's files as they are, and a check reports no drift for them", func(t *testing.T) {
		t.Parallel()
		home, pi := piHome(t, nil)
		write(t, filepath.Join(home, ".config", "jhon", "profiles.json"), apiProfile)
		wrapper := support.Read(t, filepath.Join(pi, "claude-code-provider"))
		wantCode(t, install(t, home, repoRoot, "--only", "pi"), 0)
		equal(t, support.Read(t, filepath.Join(pi, "claude-code-provider")), wrapper)
		c := render(t, home, "--check", "--only", "pi")
		wantCode(t, c, 0)
		noMatch(t, c.Out(), `DRIFT`)
	})
}

// doctorIn is `jhon doctor --runtime <args>` from this checkout under home,
// with PATH as given.
func doctorIn(t *testing.T, home, path string, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, repoRoot, support.Env("HOME="+home, "JHON_HOME=", "JHON_ROOT="+repoRoot, "PATH="+path), append([]string{"doctor", "--runtime"}, args...)...)
}

func TestRuntimeDoctorAdapters(t *testing.T) {
	t.Parallel()

	t.Run("jhon doctor --runtime pi with a profile off the bridge does not require the claude binary", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		write(t, filepath.Join(home, ".config", "jhon", "profiles.json"), apiProfile)
		if err := os.MkdirAll(filepath.Join(home, ".pi"), 0o755); err != nil { // pi is present (T10g)
			t.Fatal(err)
		}
		r := doctorIn(t, home, filepath.Join(home, "no-bin"), "pi")
		wantCode(t, r, 0)
		match(t, r.Out(), `(?m)^  ok +provider anthropic \(profile api\): Claude Code is not needed$`)
		noMatch(t, r.Out(), `claude.*(not found|missing)`)
	})

	t.Run("jhon doctor --runtime pi on the bridge names it and whether claude is on PATH", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".pi"), 0o755); err != nil { // pi is present (T10g)
			t.Fatal(err)
		}
		r := doctorIn(t, home, filepath.Join(home, "no-bin"), "pi")
		wantCode(t, r, 0)
		match(t, r.Out(), `(?m)^  warn +provider claude-bridge \(profile default\): claude is not on PATH; the bridge runs Claude Code$`)
		bin := filepath.Join(home, "bin")
		write(t, filepath.Join(bin, "claude"), "#!/bin/sh\n")
		if err := os.Chmod(filepath.Join(bin, "claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		r = doctorIn(t, home, bin, "pi")
		wantCode(t, r, 0)
		match(t, r.Out(), `(?m)^  ok +provider claude-bridge \(profile default\): claude at `+regexp.QuoteMeta(filepath.Join(bin, "claude"))+`$`)
	})

	t.Run("jhon doctor --runtime pi with pi nowhere and no setting says it was not found", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		r := doctorIn(t, home, filepath.Join(home, "no-bin"), "pi")
		wantCode(t, r, 0)
		equal(t, r.Stdout, "runtime pi was not found (no ~/.pi, no pi on PATH) and runtimes.pi is not set; nothing to check\n")
	})

	t.Run("jhon doctor --runtime claude-code with runtimes.claude_code = false says it is off", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		write(t, settingsFile(home), "[runtimes]\nclaude_code = false\n")
		r := doctorIn(t, home, os.Getenv("PATH"), "claude-code")
		wantCode(t, r, 0)
		match(t, r.Out(), `(?m)^runtime claude-code is off \(runtimes\.claude_code = false\); nothing to check$`)
		noMatch(t, r.Out(), `guarantees`)
	})
}

// TestProviderOnlyBridgeNeverWritesTheOwnersFile: Jhon's claude-bridge.json is
// its own file whatever the profile, and the owner's is never written, not
// even through a link an earlier release left (T130, L920).
func TestProviderOnlyBridgeNeverWritesTheOwnersFile(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	owner := filepath.Join(home, ".pi", "agent", "claude-bridge.json")
	own := filepath.Join(home, ".pi", "jhon", "claude-bridge.json")
	profiles := filepath.Join(home, ".config", "jhon", "profiles.json")
	write(t, filepath.Join(home, ".pi", "agent", "auth.json"), "{}\n")
	write(t, owner, ownerBridge)
	write(t, profiles, apiProfile)
	if err := os.MkdirAll(filepath.Dir(own), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(owner, own); err != nil {
		t.Fatal(err)
	}
	askClaude := func() any {
		t.Helper()
		return readJSON(t, own).(map[string]any)["askClaude"].(map[string]any)["enabled"]
	}
	settled := func(enabled bool, when string) {
		t.Helper()
		wantCode(t, install(t, home, repoRoot, "--only", "pi"), 0)
		fi, err := os.Lstat(own)
		if err != nil || !fi.Mode().IsRegular() {
			t.Fatalf("%s: Jhon's claude-bridge.json is not a regular file: %v", when, err)
		}
		equal(t, askClaude(), enabled, when)
		equal(t, support.Read(t, owner), ownerBridge, when+": the owner's own file")
		c := render(t, home, "--check", "--only", "pi")
		wantCode(t, c, 0)
		noMatch(t, c.Out(), `DRIFT`, when)
	}
	settled(false, "an old link, a profile off the bridge")
	write(t, profiles, `{"active":"b","profiles":{"b":{}}}`)
	settled(true, "switched to a profile on the bridge")
	write(t, profiles, apiProfile)
	settled(false, "switched back off the bridge")
}
