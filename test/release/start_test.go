// `jhon start [<claude args>]`: the launcher of a Jhon session.
// It starts Claude Code as the Jhon main agent, jhon-orchestrator, with the
// release's plugin, which carries the
// `/jhon-ldd` command, so that command exists only in Jhon sessions and a
// plain `claude` stays standard. Every other argument goes to claude as is.
//
// `claude` is a fake named by JHON_CLAUDE: no test starts a real session.
package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// recordArgv is a fake program that writes its arguments, NUL-terminated, to
// seen, and the value of the variable named by env (when not empty) to
// seen.env.
func recordArgv(t *testing.T, path, seen string, env ...string) {
	t.Helper()
	script := "#!/bin/sh\nfor a; do printf '%s\\000' \"$a\"; done > '" + seen + "'\n"
	if len(env) > 0 {
		script += ": > '" + seen + ".env'\n"
	}
	for _, name := range env {
		script += "printf '%s=%s\\n' " + name + " \"$" + name + "\" >> '" + seen + ".env'\n"
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// seenArgv is the arguments a fake recorded, or nil when it never ran.
func seenArgv(seen string) []string {
	b, err := os.ReadFile(seen)
	if err != nil {
		return nil
	}
	out := strings.Split(string(b), "\x00")
	return out[:len(out)-1]
}

func start(t *testing.T, args ...string) (support.Result, []string) {
	t.Helper()
	dir := temp(t)
	seen := filepath.Join(dir, "argv")
	claude := filepath.Join(dir, "claude")
	recordArgv(t, claude, seen)
	r := support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "start"}, args...), Dir: dir,
		Env: support.Env("JHON_CLAUDE="+claude, "JHON_ROOT="+support.Root())})
	return r, seenArgv(seen)
}

func TestStart(t *testing.T) {
	plugin := filepath.Join(support.Root(), "runtimes", "claude-code", "plugin")

	t.Run("jhon start runs claude as jhon-orchestrator with the release's plugin", func(t *testing.T) {
		t.Parallel()
		r, argv := start(t)
		if r.Code != 0 {
			t.Fatal(r.Stderr)
		}
		equal(t, argv, []string{"--agent", "jhon-orchestrator", "--plugin-dir", plugin})
	})

	t.Run("every other argument goes to claude", func(t *testing.T) {
		t.Parallel()
		r, argv := start(t, "--resume", "abc", "-n", "work")
		if r.Code != 0 {
			t.Fatal(r.Stderr)
		}
		equal(t, argv, []string{"--agent", "jhon-orchestrator", "--plugin-dir", plugin, "--resume", "abc", "-n", "work"})
	})

	t.Run("jhon start refuses its own flags from the caller", func(t *testing.T) {
		t.Parallel()
		for _, a := range [][]string{{"--agent", "general"}, {"--plugin-dir", "/x"}, {"--agent=general"}} {
			r, argv := start(t, a...)
			if r.Code != 2 {
				t.Fatalf("%s: exit %d", strings.Join(a, " "), r.Code)
			}
			if argv != nil {
				t.Fatalf("%s: claude ran", strings.Join(a, " "))
			}
		}
	})

	// ── jhon start pi ───────────────────────────────────────────────────────
	// pi runs Jhon on its own agent directory, ~/.pi/jhon (the pi render),
	// selected with PI_CODING_AGENT_DIR; the owner's ~/.pi/agent stays theirs.
	// `pi` is a fake named by JHON_PI.
	startPi := func(t *testing.T, rendered bool, args ...string) (r support.Result, argv []string, env []string, home string) {
		t.Helper()
		base := temp(t)
		home = filepath.Join(base, "home")
		if err := os.MkdirAll(filepath.Join(home, ".pi", "jhon"), 0o755); err != nil {
			t.Fatal(err)
		}
		if rendered {
			support.Write(t, home, map[string]string{".pi/jhon/settings.json": "{}\n"})
		}
		seen := filepath.Join(base, "seen")
		pi := filepath.Join(base, "pi")
		recordArgv(t, pi, seen, "PI_CODING_AGENT_DIR")
		// The owner's own pi directory, set in the caller's environment, must not reach Jhon's pi.
		r = support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "start", "pi"}, args...), Dir: base,
			Env: support.Env("HOME="+home, "JHON_PI="+pi, "PI_CODING_AGENT_DIR=/owner/.pi/agent", "JHON_ROOT="+support.Root())})
		argv = seenArgv(seen)
		if b, err := os.ReadFile(seen + ".env"); err == nil {
			env = strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		}
		return r, argv, env, home
	}

	t.Run("jhon start pi runs pi on Jhon's own agent directory, and every other argument goes to pi", func(t *testing.T) {
		t.Parallel()
		r, argv, env, home := startPi(t, true, "--continue", "-p", "hi")
		if r.Code != 0 {
			t.Fatal(r.Stderr)
		}
		equal(t, argv, []string{"--continue", "-p", "hi"})
		equal(t, env, []string{"PI_CODING_AGENT_DIR=" + filepath.Join(home, ".pi", "jhon")})
	})

	// ── the Claude API's first-byte window ───────────────────────────────────
	// Claude Code waits minutes for a first byte that never comes, so both
	// launches set CLAUDE_STREAM_FIRST_BYTE_TIMEOUT_MS to 60 s; an owner value
	// wins, and an empty one counts as unset.
	const firstByte = "CLAUDE_STREAM_FIRST_BYTE_TIMEOUT_MS"
	launch := func(t *testing.T, pi bool, owner string) []string {
		t.Helper()
		base := temp(t)
		home := filepath.Join(base, "home")
		support.Write(t, home, map[string]string{".pi/jhon/settings.json": "{}\n"})
		seen := filepath.Join(base, "seen")
		prog := filepath.Join(base, "prog")
		recordArgv(t, prog, seen, firstByte)
		args := []string{support.Bin(t), "start"}
		if pi {
			args = append(args, "pi")
		}
		r := support.Run(t, support.Cmd{Args: args, Dir: base,
			Env: support.Env("HOME="+home, "JHON_PI="+prog, "JHON_CLAUDE="+prog, firstByte+"="+owner, "JHON_ROOT="+support.Root())})
		if r.Code != 0 {
			t.Fatal(r.Stderr)
		}
		b, err := os.ReadFile(seen + ".env")
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	}

	t.Run("jhon start and jhon start pi give Claude Code a 60 s first-byte window", func(t *testing.T) {
		t.Parallel()
		for _, pi := range []bool{false, true} {
			equal(t, launch(t, pi, ""), []string{firstByte + "=60000"})
		}
	})

	t.Run("the owner's first-byte window wins", func(t *testing.T) {
		t.Parallel()
		for _, pi := range []bool{false, true} {
			equal(t, launch(t, pi, "120000"), []string{firstByte + "=120000"})
		}
	})

	// ── claude.ai connectors (T42a, L403) ──────────────────────────────────────
	// claude_code.connectors, off by default, keeps the claude.ai account
	// connectors out of a Jhon session with ENABLE_CLAUDEAI_MCP_SERVERS=false:
	// Claude Code then skips only those, so the plugin's jhon server and the
	// owner's own (engram, codegraph) stay. On, the launch is as before; an
	// owner value of the variable wins.
	const connectors = "ENABLE_CLAUDEAI_MCP_SERVERS"
	launchClaude := func(t *testing.T, settingsFile, owner string, args ...string) []string {
		t.Helper()
		base := temp(t)
		home := filepath.Join(base, "home")
		if settingsFile != "" {
			support.Write(t, home, map[string]string{".config/jhon/settings.toml": settingsFile})
		}
		seen := filepath.Join(base, "seen")
		prog := filepath.Join(base, "claude")
		recordArgv(t, prog, seen, connectors)
		r := support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "start"}, args...), Dir: base,
			Env: support.Env("HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "JHON_SETTINGS=",
				"JHON_CLAUDE="+prog, connectors+"="+owner, "JHON_ROOT="+support.Root())})
		if r.Code != 0 {
			t.Fatal(r.Stderr)
		}
		equal(t, seenArgv(seen), append([]string{"--agent", "jhon-orchestrator", "--plugin-dir", plugin}, args...))
		b, err := os.ReadFile(seen + ".env")
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	}

	t.Run("by default jhon start keeps the claude.ai connectors out", func(t *testing.T) {
		t.Parallel()
		equal(t, launchClaude(t, "", ""), []string{connectors + "=false"})
		equal(t, launchClaude(t, "schema = 1\n\n[claude_code]\nconnectors = false\n", "", "-p", "hi"), []string{connectors + "=false"})
	})

	t.Run("with claude_code.connectors on, jhon start leaves the connectors as Claude Code has them", func(t *testing.T) {
		t.Parallel()
		equal(t, launchClaude(t, "schema = 1\n\n[claude_code]\nconnectors = true\n", ""), []string{connectors + "="})
	})

	// ── jhon-studio (odd/tasks/jhon-studio.md S3) ───────────────────────────────
	// integrations.studio on and jhon-studio answering `integration --json`:
	// its plugin is a second --plugin-dir, after the release's. On but not on
	// PATH: claude still starts, with one note. Off: nothing is added or said.
	studio := func(t *testing.T, settingsFile string, onPath bool) (support.Result, []string, string) {
		r, argv, studioPlugin, _ := studioVars(t, settingsFile, onPath)
		return r, argv, studioPlugin
	}

	t.Run("with integrations.studio on and the claude.ai connectors on, both hold", func(t *testing.T) {
		t.Parallel()
		r, argv, studioPlugin, seenConnectors := studioVars(t, "schema = 1\n\n[claude_code]\nconnectors = true\n\n[integrations]\nstudio = true\n", true)
		equal(t, r.Code, 0, r.Stderr)
		equal(t, r.Stderr, "")
		equal(t, argv, []string{"--agent", "jhon-orchestrator", "--plugin-dir", plugin, "--plugin-dir", studioPlugin, "-p", "hi"})
		equal(t, seenConnectors, connectors+"=")
	})

	t.Run("with integrations.studio on and the claude.ai connectors off, the plugin loads and the connectors stay out", func(t *testing.T) {
		t.Parallel()
		_, argv, studioPlugin, seenConnectors := studioVars(t, "schema = 1\n\n[integrations]\nstudio = true\n", true)
		equal(t, argv, []string{"--agent", "jhon-orchestrator", "--plugin-dir", plugin, "--plugin-dir", studioPlugin, "-p", "hi"})
		equal(t, seenConnectors, connectors+"=false")
	})

	t.Run("with integrations.studio on, jhon start loads jhon-studio's plugin too", func(t *testing.T) {
		t.Parallel()
		r, argv, studioPlugin := studio(t, "schema = 1\n\n[integrations]\nstudio = true\n", true)
		equal(t, r.Code, 0, r.Stderr)
		equal(t, r.Stderr, "")
		equal(t, argv, []string{"--agent", "jhon-orchestrator", "--plugin-dir", plugin, "--plugin-dir", studioPlugin, "-p", "hi"})
	})

	t.Run("with integrations.studio on and jhon-studio missing, jhon start still starts and says so once", func(t *testing.T) {
		t.Parallel()
		r, argv, _ := studio(t, "schema = 1\n\n[integrations]\nstudio = true\n", false)
		equal(t, r.Code, 0, r.Stderr)
		equal(t, r.Stderr, "jhon: integrations.studio is on, but jhon-studio is not on PATH; this session starts without its plugin\n")
		equal(t, argv, []string{"--agent", "jhon-orchestrator", "--plugin-dir", plugin, "-p", "hi"})
	})

	t.Run("with integrations.studio off, the default, jhon start adds and says nothing", func(t *testing.T) {
		t.Parallel()
		r, argv, _ := studio(t, "schema = 1\n", true)
		equal(t, r.Code, 0, r.Stderr)
		equal(t, r.Stderr, "")
		equal(t, argv, []string{"--agent", "jhon-orchestrator", "--plugin-dir", plugin, "-p", "hi"})
	})

	t.Run("the owner's ENABLE_CLAUDEAI_MCP_SERVERS wins", func(t *testing.T) {
		t.Parallel()
		equal(t, launchClaude(t, "", "true"), []string{connectors + "=true"})
	})

	// ── the orchestrator body once (T42b, L403) ──────────────────────────────
	// In print mode --agent makes the orchestrator's definition the system
	// prompt, so jhon start tells the plugin's SessionStart hook so with
	// JHON_AGENT_LOADED; an interactive --agent session does not load it
	// (Claude Code 2.1.310), so there the variable stays unset.
	const loaded = "JHON_AGENT_LOADED"
	marker := func(t *testing.T, owner string, args ...string) []string {
		t.Helper()
		base := temp(t)
		seen := filepath.Join(base, "seen")
		prog := filepath.Join(base, "claude")
		recordArgv(t, prog, seen, loaded)
		r := support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "start"}, args...), Dir: base,
			Env: support.Env("JHON_CLAUDE="+prog, loaded+"="+owner, "JHON_ROOT="+support.Root())})
		if r.Code != 0 {
			t.Fatal(r.Stderr)
		}
		b, err := os.ReadFile(seen + ".env")
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	}

	t.Run("a print-mode jhon start tells the hooks the orchestrator is the system prompt", func(t *testing.T) {
		t.Parallel()
		for _, args := range [][]string{{"-p", "hi"}, {"--print", "hi"}, {"--model", "opus", "-p", "hi"}} {
			equal(t, marker(t, "", args...), []string{loaded + "=jhon-orchestrator"}, args)
		}
	})

	t.Run("an interactive jhon start leaves JHON_AGENT_LOADED unset, even if the caller had it", func(t *testing.T) {
		t.Parallel()
		equal(t, marker(t, ""), []string{loaded + "="})
		equal(t, marker(t, "jhon-orchestrator", "--resume", "abc"), []string{loaded + "="})
	})

	t.Run("jhon start pi refuses before a render wrote the directory, and names jhon sync", func(t *testing.T) {
		t.Parallel()
		r, argv, _, _ := startPi(t, false)
		equal(t, r.Code, 1)
		if argv != nil {
			t.Fatal("pi ran")
		}
		match(t, r.Stderr, `jhon sync`)
	})
}

// studioVars runs `jhon start -p hi` under the settings text, with a fake
// jhon-studio on PATH (when onPath) answering integration --json with a
// plugin folder of its own, and a fake claude that records its arguments and
// the ENABLE_CLAUDEAI_MCP_SERVERS it saw ("NAME=value"); the owner sets none.
func studioVars(t *testing.T, settingsFile string, onPath bool) (support.Result, []string, string, string) {
	const connectors = "ENABLE_CLAUDEAI_MCP_SERVERS"
	t.Helper()
	base := temp(t)
	home := filepath.Join(base, "home")
	support.Write(t, home, map[string]string{".config/jhon/settings.toml": settingsFile})
	bin, studioPlugin := filepath.Join(base, "bin"), filepath.Join(base, "studio-plugin")
	if err := os.MkdirAll(studioPlugin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if onPath {
		answer := `{"version":"0.1.0","plugin_dir":"` + studioPlugin + `","skill":"` + studioPlugin + `/skills/video/SKILL.md"}`
		if err := os.WriteFile(filepath.Join(bin, "jhon-studio"), []byte("#!/bin/sh\n[ \"$*\" = 'integration --json' ] && echo '"+answer+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seen := filepath.Join(base, "seen")
	prog := filepath.Join(base, "claude")
	recordArgv(t, prog, seen)
	// The fake also records the connectors' variable claude saw, as NAME=value.
	script, err := os.ReadFile(prog)
	if err != nil {
		t.Fatal(err)
	}
	script = append(script, []byte("printf '%s=%s\\n' "+connectors+" \"$"+connectors+"\" > '"+seen+"-vars"+"'\n")...)
	if err := os.WriteFile(prog, script, 0o755); err != nil {
		t.Fatal(err)
	}
	r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "start", "-p", "hi"}, Dir: base,
		Env: support.Env("HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "JHON_SETTINGS=", "PATH="+bin+":/usr/bin:/bin",
			"JHON_CLAUDE="+prog, connectors+"=", "JHON_ROOT="+support.Root())})
	b, _ := os.ReadFile(seen + "-vars")
	return r, seenArgv(seen), studioPlugin, strings.TrimSuffix(string(b), "\n")
}
