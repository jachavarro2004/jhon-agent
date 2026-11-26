package guard

import (
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// transportOutward are the outward classes a transport's plain forward is
// not judged by on its own (T228 L12): a ProxyCommand, KnownHostsCommand or
// LocalCommand an ssh transport value runs that is one forward to its host
// (sh.ForwardCommand: `ssh -W %h:%p bastion`, `aws ssm start-session
// --target %h …`) is the connection the git command already makes. Only
// "remote": a deploy is never the connection, and every destructive and
// local-harm reading (a removal, the class rule, an environment change, a
// write, a push) stays.
var transportOutward = set("remote")

// transportFindings is fs without the asks of transportOutward classes.
func transportFindings(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Level == "ask" && transportOutward[f.Cls] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// judgeGitConfigEnv is the findings of git's --config-env=<key>=<VAR>
// settings (T228 L10): git takes the key's value from VAR, so when the line
// sets VAR (before git, by export or by env; env holds both) its value is
// read as that key's value (sh.GitSetting: a command key's command, an
// alias, an ssh command's own commands). A VAR the line does not set is a
// value the guard cannot see, as any computed one.
func judgeGitConfigEnv(args []string, env map[string]string, ctx *Ctx, vars map[string]string, st *State, depth int, route []string, envv map[string]string) []Finding {
	g := sh.GitParts(args)
	rest := append([]string{g.Sub}, g.Rest...)
	var out []Finding
	for i := 0; i+1 < len(g.Globals); i++ {
		if g.Globals[i] != "--config-env" {
			continue
		}
		key, name, ok := strings.Cut(g.Globals[i+1], "=")
		value, set := env[name]
		if !ok || !set {
			continue
		}
		strs, transport, err := sh.GitSetting(key+"="+value, rest)
		if err != nil {
			return append(out, unreadableAsk(err))
		}
		for _, s := range strs {
			f := judgeScript(s, ctx, vars, st, depth, route, envv)
			if transport && sh.ForwardCommand(s) {
				f = transportFindings(f)
			}
			out = append(out, f...)
		}
	}
	return out
}
