package guard

import (
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

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
		strs, err := sh.GitSetting(key+"="+value, rest)
		if err != nil {
			return append(out, unreadableAsk(err))
		}
		for _, s := range strs {
			out = append(out, judgeScript(s, ctx, vars, st, depth, route, envv)...)
		}
	}
	return out
}
