package cli

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"

	"github.com/jachavarro2004/jhon-agent/engine/internal/hook"
	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
)

// `jhon start [<claude args>]` and `jhon start pi [<pi args>]`:
// the launcher of a Jhon session. Claude Code runs
// as a Jhon main agent with the release's plugin, which carries the
// /jhon-ldd command, so it exists only in Jhon sessions and a plain
// `claude` stays standard; the plugin is named through the runtime root, so
// a session resumed through `current` gets the plugin of the release active
// then. pi runs on its own agent directory, ~/.pi/jhon, which the pi
// render writes; the owner's ~/.pi/agent stays theirs. Both get Claude
// Code's first-byte window (firstByteEnv) and the project its sandboxed
// commands are pinned to (sandboxProjectEnv). A cli integration that is on
// (jhon-studio, odd/tasks/jhon-studio.md S3) adds its own plugin to the
// Claude Code launch (launchPlugins). The program replaces this process;
// JHON_CLAUDE and JHON_PI name another.
func init() { register("start", runStart) }

func runStart(argv []string) (int, error) {
	if len(argv) > 0 && argv[0] == "pi" {
		return startPi(argv[1:])
	}
	for _, a := range argv {
		flag, _, _ := strings.Cut(a, "=")
		if flag == "--agent" || flag == "--plugin-dir" {
			o := out()
			fmt.Fprintf(o.E, "%sjhon:%s jhon start sets %s itself\nusage: jhon start [<claude args>]\n", o.R, o.Z, flag)
			return 2, nil
		}
	}
	plugin := release.Locate().Runtime + "/" + render.PluginDir
	args := []string{"--agent", "jhon-orchestrator", "--plugin-dir", plugin}
	extra, notes := launchPlugins()
	for _, n := range notes {
		fmt.Fprintln(os.Stderr, n)
	}
	args = append(append(args, extra...), argv...)
	return execProgram(envOr("JHON_CLAUDE", "claude"), args, agentLoadedEnv(argv, connectorsEnv(firstByteEnv(sandboxProjectEnv(os.Environ())))))
}

// launchPlugins are the --plugin-dir arguments of each cli integration the
// launch keeps out while it is off (when_off: launch) and that is on: the
// plugin folder its program names, `<doctor.binary> integration --json`
// (integrations.AskIntegration). Claude Code takes --plugin-dir more than
// once (2.1.323's --help: "repeatable"). One that is on but gives no
// plugin folder is a note each, and the session starts without it; one
// whose answer does not fit this jhon (integrations.Judge: a newer minimum,
// another contract) is off for the session with its one note (T237); one
// that is off is never asked.
func launchPlugins() (args, notes []string) {
	for _, m := range onCLITools() {
		a, err := integrations.AskIntegration(m)
		if err == nil {
			if fit := integrations.Judge(m, a, version.Version); fit.Off != "" {
				notes = append(notes, "jhon: "+fit.Off)
				continue
			}
		}
		if err == nil && !release.IsDir(a.PluginDir) {
			err = fmt.Errorf("its plugin_dir, %s, is not a folder", a.PluginDir)
		}
		if err != nil {
			notes = append(notes, fmt.Sprintf("jhon: %s is on, but %v; this session starts without its plugin", m.Setting, err))
			continue
		}
		args = append(args, "--plugin-dir", a.PluginDir)
	}
	return args, notes
}

// onCLITools are the cli integrations the launch keeps out while they are
// off (when_off: launch) that are on: the ones a session's start asks.
func onCLITools() []integrations.Manifest {
	var out []integrations.Manifest
	for _, m := range integrations.OffAt("launch") {
		if m.Kind == "cli" && m.Doctor.Binary != "" && settings.On(m.Setting) {
			out = append(out, m)
		}
	}
	return out
}

// integrationsOffEnv names, for pi's shell, the setting of each cli
// integration that is on but off for this run (T237): the shell registers
// nothing of one it names (shell/lib/studio-command.ts).
const integrationsOffEnv = "JHON_INTEGRATIONS_OFF"

// toolsOffEnv is env for pi's session: with integrationsOffEnv naming,
// comma-separated, the setting of each on cli integration whose answer does
// not fit this jhon (integrations.Judge), and that one note each; without
// the variable when none is off, a value the caller held never kept. One
// that does not answer is not named: /jhon:video says why at its use.
func toolsOffEnv(env []string) ([]string, []string) {
	env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, integrationsOffEnv+"=") })
	var off, notes []string
	for _, m := range onCLITools() {
		a, err := integrations.AskIntegration(m)
		if err != nil {
			continue
		}
		if fit := integrations.Judge(m, a, version.Version); fit.Off != "" {
			off = append(off, m.Setting)
			notes = append(notes, "jhon: "+fit.Off)
		}
	}
	if len(off) > 0 {
		env = append(env, integrationsOffEnv+"="+strings.Join(off, ","))
	}
	return env, notes
}

// sandboxProjectEnv is env with sandbox.ProjectEnv naming the project this
// session's sandboxed commands are pinned to (sandboxProject), or without the
// variable where this folder is in no repository. A value already there is
// never kept: it would pin the session to a folder nobody resolved.
func sandboxProjectEnv(env []string) []string {
	env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, sandbox.ProjectEnv+"=") })
	if top := sandboxProject(); top != "" {
		return append(env, sandbox.ProjectEnv+"="+top)
	}
	return env
}

// agentLoadedEnv is env with hook.AgentLoadedEnv naming jhon-orchestrator
// when argv runs Claude Code in print mode, where --agent makes the agent's
// definition the system prompt, so SessionStart need not hand it again
// (T42b, L403); otherwise without it: an interactive --agent session does
// not load the definition (Claude Code 2.1.310).
func agentLoadedEnv(argv, env []string) []string {
	env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, hook.AgentLoadedEnv+"=") })
	if slices.Contains(argv, "-p") || slices.Contains(argv, "--print") {
		return append(env, hook.AgentLoadedEnv+"=jhon-orchestrator")
	}
	return env
}

// claudeAIConnectors is Claude Code's switch for the claude.ai account
// connectors: false skips only those, so the plugin's jhon server and the
// owner's own MCP servers (engram, codegraph) stay, where
// --strict-mcp-config would drop them all.
const claudeAIConnectors = "ENABLE_CLAUDEAI_MCP_SERVERS"

// connectorsEnv is env with the claude.ai connectors off unless
// claude_code.connectors (T42a, L403) and every connector integration its
// manifest turns off at launch (kind mcp-connector, when_off: launch;
// m365's is claude_code.connectors itself, T58b, L1167) are on, or the
// owner set the variable. A cli one off at launch is no connector
// (launchPlugins). It fails
// closed: a set with no launch-off manifest keeps them out too (T58c,
// L1193). The variable loads or keeps out every connector at once, so one
// of them off keeps them all out. pi's claude-bridge loads no claude.ai
// connectors, so only this launch takes it.
func connectorsEnv(env []string) []string {
	if os.Getenv(claudeAIConnectors) != "" {
		return env
	}
	off := func(m integrations.Manifest) bool { return m.Kind == "mcp-connector" && !settings.On(m.Setting) }
	if settings.On(settings.Connectors) && !slices.ContainsFunc(integrations.OffAt("launch"), off) {
		return env
	}
	return withEnv(env, claudeAIConnectors+"=false")
}

// firstByteTimeout is how long Claude Code waits for the API's first byte
// before it retries: its own default runs to minutes per attempt, so a turn
// with no answer looks stuck on working. pi's claude-bridge passes its
// environment to Claude Code, so one variable covers both launches.
const firstByteTimeout = "CLAUDE_STREAM_FIRST_BYTE_TIMEOUT_MS"

// firstByteEnv is env with a 60 s first-byte window unless the owner set one.
func firstByteEnv(env []string) []string {
	if os.Getenv(firstByteTimeout) != "" {
		return env
	}
	return withEnv(env, firstByteTimeout+"=60000")
}

// startPi runs pi on ~/.pi/jhon, Jhon's own agent directory, so the owner's
// ~/.pi/agent never steers it. A cli integration that is on but does not
// fit this jhon is off for the session, with its note (toolsOffEnv).
// Before a render wrote the directory it names the fix: jhon sync from a
// source checkout, jhon setup with pi among its runtimes on a release
// install, which has no checkout to sync (T8b).
func startPi(argv []string) (int, error) {
	dir := os.Getenv("HOME") + "/.pi/jhon"
	if !release.IsFile(dir + "/settings.json") {
		// The message names dir in full, $HOME expanded, never as ~.
		o := out()
		fix := "run `jhon setup` and pick pi among its runtimes first"
		if release.Locate().HasSource() {
			fix = "run `jhon sync` with pi installed (~/.pi present) first"
		}
		fmt.Fprintf(o.E, "%sjhon:%s %s has no settings.json; %s\n", o.R, o.Z, dir, fix)
		return 1, nil
	}
	env, notes := toolsOffEnv(withEnv(firstByteEnv(sandboxProjectEnv(os.Environ())), "PI_CODING_AGENT_DIR="+dir))
	for _, n := range notes {
		fmt.Fprintln(os.Stderr, n)
	}
	return execProgram(envOr("JHON_PI", "pi"), argv, env)
}

// withEnv is env with each name=value of set in place of the name's
// earlier value: exec passes the list as is, and a duplicate name would
// leave the first one in force.
func withEnv(env []string, set ...string) []string {
	kept := slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		return slices.ContainsFunc(set, func(s string) bool {
			name, _, _ := strings.Cut(s, "=")
			return strings.HasPrefix(kv, name+"=")
		})
	})
	return append(kept, set...)
}

// envOr is ${name:-def}.
func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// execProgram is a shell's exec: replace this process with prog, found on
// PATH unless it names a path; 127 when it is not found, 126 when it cannot
// be executed.
func execProgram(prog string, args, env []string) (int, error) {
	path := prog
	if !strings.Contains(prog, "/") {
		p, err := exec.LookPath(prog)
		if err != nil {
			fmt.Fprintf(os.Stderr, "jhon: exec: %s: not found\n", prog)
			return 127, nil
		}
		path = p
	}
	err := syscall.Exec(path, append([]string{prog}, args...), env)
	if err == syscall.ENOENT {
		fmt.Fprintf(os.Stderr, "jhon: exec: %s: not found\n", prog)
		return 127, nil
	}
	fmt.Fprintf(os.Stderr, "jhon: exec: %s: cannot execute: %v\n", prog, err)
	return 126, nil
}
