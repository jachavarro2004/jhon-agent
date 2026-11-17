package sh

import "strings"

// GitValueGlobals are git's global options that take a separate value.
var GitValueGlobals = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true,
	"--exec-path": true, "--config-env": true, "--super-prefix": true, "--list-cmds": true,
}

// Git is a git command line split: its global options, the configuration
// they give, its subcommand, and the subcommand's arguments.
type Git struct {
	Globals, Configs []string
	Sub              string
	Rest             []string
}

// GitParts splits git's global options from its subcommand.
func GitParts(args []string) Git {
	var g Git
	k := 0
	for k < len(args) && strings.HasPrefix(args[k], "-") {
		a := args[k]
		if len(a) > 2 && strings.HasPrefix(a, "-c") {
			// An attached -c<name>=<value>: git refuses it today, but it is
			// read as the configuration it spells, never as a harmless flag.
			g.Configs = append(g.Configs, a[2:])
			g.Globals = append(g.Globals, "-c", a[2:])
			k++
			continue
		}
		name, value, attached := strings.Cut(a, "=")
		if GitValueGlobals[name] {
			if !attached {
				k++
				value = at(args, k)
			}
			if name == "-c" || name == "--config-env" {
				g.Configs = append(g.Configs, value)
			}
			g.Globals = append(g.Globals, name, value)
		} else {
			g.Globals = append(g.Globals, name)
		}
		k++
	}
	g.Sub = at(args, k)
	if k+1 < len(args) {
		g.Rest = args[k+1:]
	}
	return g
}

// at is args[k], or "" past the end.
func at(args []string, k int) string {
	if k >= 0 && k < len(args) {
		return args[k]
	}
	return ""
}
