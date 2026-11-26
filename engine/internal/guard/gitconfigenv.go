package guard

import (
	"os"
	"path/filepath"
	"regexp"
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

// gitConfigFileVars name a configuration file git reads besides the
// repository's own.
var gitConfigFileVars = []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"}

// gitIncludeKey is a key that makes git read another file: include.path,
// includeIf.<condition>.path.
var gitIncludeKey = regexp.MustCompile(`(?i)^include(?:if\..+)?\.path$`)

// judgeGitConfigFiles is the findings of the configuration files a git
// command line is pointed at (T257): GIT_CONFIG_GLOBAL or GIT_CONFIG_SYSTEM
// (set on the line or exported), and a -c or --config-env include.path or
// includeIf.<condition>.path. Each file is read as git reads it, its
// includes too, and every setting in it is read as -c's is
// (sh.GitSetting: an alias, a command key). A file the line writes, or one
// the guard cannot read, is judged by the line's visible words alone, as a
// script file is (L1311); /dev/null holds none.
func judgeGitConfigFiles(args []string, env map[string]string, ctx *Ctx, vars map[string]string, st *State, depth int, route []string, envv map[string]string) []Finding {
	g := sh.GitParts(args)
	rest := append([]string{g.Sub}, g.Rest...)
	var files []string
	for _, n := range gitConfigFileVars {
		if v := env[n]; v != "" {
			files = append(files, v)
		}
	}
	for _, c := range g.Configs {
		if k, v, ok := strings.Cut(c, "="); ok && gitIncludeKey.MatchString(k) {
			files = append(files, v)
		}
	}
	for i := 0; i+1 < len(g.Globals); i++ {
		if k, name, ok := strings.Cut(g.Globals[i+1], "="); g.Globals[i] == "--config-env" && ok && gitIncludeKey.MatchString(k) && env[name] != "" {
			files = append(files, env[name])
		}
	}
	var out []Finding
	for _, f := range files {
		path := pathOf(f, st.Dir, ctx, vars)
		if path == "" || path == os.DevNull || st.wrote(f, ctx, vars) {
			continue
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() || info.Size() > scriptMaxBytes {
			continue
		}
		listed, ok := readGit(filepath.Dir(path), "config", "--file", path, "--includes", "--null", "--list")
		if !ok {
			continue
		}
		for _, kv := range strings.Split(listed, "\x00") {
			key, value, _ := strings.Cut(kv, "\n")
			strs, transport, err := sh.GitSetting(key+"="+value, rest)
			if err != nil {
				return append(out, unreadableAsk(err))
			}
			for _, s := range strs {
				found := judgeScript(s, ctx, vars, st, depth, route, envv)
				if transport && sh.ForwardCommand(s) {
					found = transportFindings(found)
				}
				out = append(out, found...)
			}
		}
	}
	return out
}
