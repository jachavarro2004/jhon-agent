package cli

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"

	"github.com/jachavarro2004/jhon-agent/engine/internal/hook"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
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
// commands are pinned to (sandboxProjectEnv). The program replaces this
// process; JHON_CLAUDE and JHON_PI name another.
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
	return execProgram(envOr("JHON_CLAUDE", "claude"), append([]string{"--agent", "jhon-orchestrator", "--plugin-dir", plugin}, argv...), agentLoadedEnv(argv, connectorsEnv(firstByteEnv(sandboxProjectEnv(os.Environ())))))
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
// claude_code.connectors is on (T42a, L403) or the owner set the variable.
// pi's claude-bridge loads no claude.ai connectors, so only this launch
// takes it.
func connectorsEnv(env []string) []string {
	if os.Getenv(claudeAIConnectors) != "" || settings.On("claude_code.connectors") {
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
// ~/.pi/agent never steers it.
func startPi(argv []string) (int, error) {
	dir := os.Getenv("HOME") + "/.pi/jhon"
	if !release.IsFile(dir + "/settings.json") {
		// The message names dir in full, $HOME expanded, never as ~.
		o := out()
		fmt.Fprintf(o.E, "%sjhon:%s %s has no settings.json; run `jhon sync` with pi installed (~/.pi present) first\n", o.R, o.Z, dir)
		return 1, nil
	}
	env := withEnv(firstByteEnv(sandboxProjectEnv(os.Environ())), "PI_CODING_AGENT_DIR="+dir)
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
