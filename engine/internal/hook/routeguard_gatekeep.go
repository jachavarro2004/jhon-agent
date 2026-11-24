package hook

import (
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// gateConfig is a configuration key (or `key=value`) that can turn the
// merge gate off: its own entries, the switch of its event, and an include
// that could set either.
var gateConfig = regexp.MustCompile(`(?i)^(?:hook\.(?:jhon-[^.=\s]*|reference-transaction)(?:\.[^=\s]*)?|include(?:if\.[^=\s]*)?(?:\.path)?)(?:=.*)?$`)

// configEnv is a variable that gives git configuration, named anywhere on
// the line: set, exported, declared, read into or printed into (printf -v).
var configEnv = regexp.MustCompile(`\bGIT_CONFIG_(?:PARAMETERS|COUNT|KEY_\w*|VALUE_\w*|GLOBAL|SYSTEM)\b`)

// gateWord is a key or a value that reaches the gate's configuration: a
// hook's, core.hooksPath, or an include (an alias value with -c in it).
var gateWord = regexp.MustCompile(`(?i)\bhook\.|core\.hookspath|\binclude`)

// markerWord names the agent's marker, for a line the parser cannot read.
var markerWord = regexp.MustCompile(`\b` + AgentVariable + `\b`)

// gateBypass is why command, run from cwd in a session rooted at top, may
// not run, "" when it may (T219 S6): in a repository under LDD it would
// remove the merge gate from git's configuration or turn it off for a
// command: `git config` writing hook.jhon-*, hook.reference-transaction.*
// or an include, `-c`/`--config-env` with one, or git configuration given
// through the environment (GIT_CONFIG_PARAMETERS, GIT_CONFIG_COUNT and its
// keys, GIT_CONFIG_GLOBAL, GIT_CONFIG_SYSTEM). core.hooksPath does not turn
// a configured hook off, so it stays allowed. A key the guard cannot read
// in a configuration write is refused.
func gateBypass(command, cwd, top string) string {
	lower := strings.ToLower(command)
	envWhy := envChange(command)
	if envWhy == "" && !strings.Contains(lower, "hook") && !strings.Contains(lower, "include") && !strings.Contains(lower, "config") {
		return ""
	}
	targets := guard.Targets(command, cwd)
	gated := top != "" && gatedRoot(top)
	for _, t := range targets {
		if !gated && t.Root != "" && t.Root != guard.UnknownRoot {
			gated = gatedRoot(t.Root)
		}
	}
	if !gated {
		return ""
	}
	if envWhy != "" {
		return gateOff(command, envWhy+": the merge gate knows the agent's git by "+AgentVariable+" and reads git's configuration, which a line may not change")
	}
	for _, t := range targets {
		p := t.Program
		if len(p.Argv) == 0 || p.Name != "git" && !unnamed.MatchString(p.Argv[0]) {
			continue
		}
		g := sh.GitParts(p.Argv[1:])
		for _, c := range g.Configs {
			key, value, _ := strings.Cut(c, "=")
			alias := strings.HasPrefix(strings.ToLower(key), "alias.") && gateWord.MatchString(unquoted(value))
			if gateConfig.MatchString(c) || alias || unnamed.MatchString(c) {
				return gateOff(strings.Join(p.Argv, " "), "`-c "+c+"` sets it for this command")
			}
		}
		if g.Sub == "config" {
			if why := configWrite(g.Rest); why != "" {
				return gateOff(strings.Join(p.Argv, " "), why)
			}
		}
	}
	return ""
}

// configWrite is why `git config <rest>` would change the gate's
// configuration: it writes one of its keys, a key the guard cannot read,
// or opens the file in an editor; "" for a read or another key.
func configWrite(rest []string) string {
	read := slices.ContainsFunc(rest, func(a string) bool {
		return a == "-l" || a == "--list" || strings.HasPrefix(a, "--get") || a == "--show-origin" && len(rest) == 1
	})
	var words []string
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			words = append(words, a)
		}
	}
	if len(words) > 0 && (words[0] == "get" || words[0] == "list") {
		read = true
	}
	if read {
		return ""
	}
	if slices.Contains(rest, "-e") || slices.Contains(rest, "--edit") || len(words) > 0 && words[0] == "edit" {
		return "it opens the configuration in an editor"
	}
	// An alias's value may carry -c with a gate key (L12 B4).
	for _, a := range rest {
		if gateWord.MatchString(unquoted(a)) && !gateConfig.MatchString(a) && slices.ContainsFunc(words, func(w string) bool { return strings.HasPrefix(strings.ToLower(w), "alias.") }) {
			return "it writes an alias that sets " + a
		}
	}
	for _, w := range words {
		if gateConfig.MatchString(w) {
			return "it writes " + w
		}
		if unnamed.MatchString(w) {
			return "it writes a key the guard cannot read (" + w + ")"
		}
	}
	return ""
}

// gateOff is the refusal of a command that would turn the merge gate off.
func gateOff(form, why string) string {
	return "`" + clipForm(form) + "` would turn off the merge gate git runs in this repository (hook." + MergeGateName +
		", T219): " + why + ". The gate refuses a move of the default branch that no ready merge check covers; leave it on: " + s4Instruction("<change>")
}

// unquoted is s with the quotes and backslashes git's own splitting of an
// alias value removes (L14 d2).
func unquoted(s string) string {
	return strings.NewReplacer(`"`, "", `'`, "", `\`, "").Replace(s)
}
