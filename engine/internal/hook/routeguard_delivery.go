package hook

import (
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// A delivery is what a Bash command delivers: a commit, a PR, or both.
type delivery struct{ commit, pr bool }

// deliveryOf reads command's delivery from its parsed programs (T109 WU2,
// L784 a), unwrapped as safety-guard reads them (sh.JudgeString: env,
// command, sudo, a shell's -c string, eval, xargs, find -exec): a simple
// command is a commit when its program is git and its subcommand, after
// git's global options and an alias the line defines with -c, is commit;
// a PR when its program is gh and its words are `pr create` or `pr new`,
// after gh's -R. Text that only names a commit or a PR (an echo, a grep,
// a heredoc, a comment, `git commit-tree`, `--help`) delivers nothing.
// It fails closed: a line the reader cannot read, a program or a git
// subcommand it cannot name, and code or input it cannot see are judged as
// delivery whenever their text names a commit or a PR.
func deliveryOf(command string) delivery {
	ps, err := sh.JudgeString(command, nil, nil, sh.Opts{})
	if err != nil {
		return mentions(command)
	}
	var d delivery
	for _, p := range ps {
		d = d.or(programDelivery(p, 0))
	}
	return d
}

func (d delivery) or(e delivery) delivery { return delivery{d.commit || e.commit, d.pr || e.pr} }

// any is whether d delivers anything.
func (d delivery) any() bool { return d.commit || d.pr }

// mentions is the delivery text names, loosely: the fail-closed reading of
// what the reader cannot see.
func mentions(text string) delivery {
	return delivery{commit: mentionsCommit.MatchString(text), pr: mentionsPR.MatchString(text)}
}

var (
	mentionsCommit = lazyre.New(`\bgit\b[\s\S]*\bcommit\b|\bcommit\b[\s\S]*\bgit\b`)
	mentionsPR     = lazyre.New(`\bgh\b[\s\S]*\bpr\b|\bpr\b[\s\S]*\bgh\b`)
	// unnamed is a word whose value the reader does not know: an expansion
	// or a substitution.
	unnamed = lazyre.New("[$`]|\\(…\\)")
)

// programDelivery is what one program delivers; depth bounds alias chains.
func programDelivery(p sh.Program, depth int) delivery {
	if len(p.Argv) == 0 {
		return delivery{}
	}
	text := strings.Join(p.Argv, " ")
	if p.Code != nil {
		text += "\n" + *p.Code
	}
	if p.StdinOpaque != "" || p.Code != nil {
		// Code or input the reader does not show: judged by its text, and
		// by the whole line's when the input is unseen.
		if p.StdinOpaque != "" && p.Raw != nil {
			text += "\n" + *p.Raw
		}
		return mentions(text)
	}
	if unnamed.MatchString(p.Argv[0]) {
		// A program the reader cannot name may be git or gh.
		d := mentions(text)
		d.commit = d.commit || slices.Contains(p.Argv[1:], "commit")
		d.pr = d.pr || ghPR(p.Argv[1:])
		return d
	}
	switch p.Name {
	case "git":
		return gitDelivery(p, depth)
	case "gh":
		return delivery{pr: ghPR(p.Argv[1:])}
	}
	return delivery{}
}

// gitDelivery reads a git command line: its subcommand after the global
// options, an alias the line itself defines (-c alias.X=...) resolved.
func gitDelivery(p sh.Program, depth int) delivery {
	g := sh.GitParts(p.Argv[1:])
	if slices.Contains(g.Globals, "--help") || slices.Contains(g.Globals, "-h") || g.Sub == "help" {
		return delivery{}
	}
	if g.Sub == "" {
		return delivery{}
	}
	if unnamed.MatchString(g.Sub) {
		return delivery{commit: true}
	}
	for i, c := range g.Configs {
		name, value, _ := strings.Cut(c, "=")
		if !strings.EqualFold(name, "alias."+g.Sub) {
			continue
		}
		// --config-env takes its value from a variable the reader cannot see.
		if isConfigEnv(g.Globals, i) || unnamed.MatchString(value) || depth > 8 {
			return delivery{commit: true}
		}
		if strings.HasPrefix(value, "!") {
			ps, err := sh.JudgeString(value[1:]+" "+strings.Join(g.Rest, " "), nil, nil, sh.Opts{})
			if err != nil {
				return mentions(value)
			}
			var d delivery
			for _, q := range ps {
				d = d.or(programDelivery(q, depth+1))
			}
			return d
		}
		words := sh.Words(value)
		argv := append(append([]string{"git"}, g.Globals...), append(words, g.Rest...)...)
		q := p
		q.Argv = argv
		return gitDelivery(q, depth+1)
	}
	if g.Sub != "commit" {
		return delivery{}
	}
	for _, a := range g.Rest {
		if a == "--" {
			break
		}
		if a == "--help" {
			return delivery{}
		}
	}
	if len(g.Rest) == 1 && g.Rest[0] == "-h" {
		return delivery{}
	}
	return delivery{commit: true}
}

// isConfigEnv is whether the i-th configuration git was given came from
// --config-env.
func isConfigEnv(globals []string, i int) bool {
	n := -1
	for k := 0; k+1 < len(globals); k++ {
		if globals[k] == "-c" || globals[k] == "--config-env" {
			n++
			if n == i {
				return globals[k] == "--config-env"
			}
			k++
		}
	}
	return false
}

// ghPR is whether gh's arguments open a PR: `pr create` or `pr new`, with
// -R/--repo (and --hostname) anywhere before it, and no --help.
func ghPR(args []string) bool {
	var words []string
	for k := 0; k < len(args); k++ {
		a := args[k]
		switch {
		case a == "--help" || a == "-h":
			return false
		case a == "-R" || a == "--repo" || a == "--hostname":
			k++
		case strings.HasPrefix(a, "-"):
		default:
			words = append(words, a)
		}
	}
	if len(words) < 2 {
		return false
	}
	// A word the reader cannot name may be either one (fail closed).
	opens := func(w string) bool { return w == "create" || w == "new" || unnamed.MatchString(w) }
	return (words[0] == "pr" || unnamed.MatchString(words[0])) && opens(words[1])
}

// deliveryRoots are the project roots of the repositories command, run in
// cwd, commits in (pr false) or opens a PR from (pr true), each once (T140,
// L1085): where each git or gh program runs as the safety guard reads the
// line (guard.Targets: a cd, a pushd, a subshell, env -C, git -C). A
// repository without LDD's layout is a root too: it is judged as a session
// rooted there (L957). It fails closed to top, the session's root, today's
// answer, for a delivery it cannot place: a directory the guard cannot tell
// (`cd "$X"`), one outside any repository, a program it cannot name (code,
// input it cannot see, a line it cannot read, a git alias), gh given its
// repository (-R, --repo, GH_REPO), and a delivery the walk does not reach.
func deliveryRoots(command, cwd, top string, pr bool) []string {
	of := func(d delivery) bool { return d.pr && pr || d.commit && !pr }
	var roots []string
	add := func(r string) {
		if !slices.Contains(roots, r) {
			roots = append(roots, r)
		}
	}
	ps, err := sh.JudgeString(command, nil, nil, sh.Opts{})
	if err != nil {
		return []string{top}
	}
	want := 0
	for _, p := range ps {
		if of(programDelivery(p, 0)) {
			want++
			if !placeable(p, command) {
				add(top)
			}
		}
	}
	placed := 0
	for _, t := range guard.Targets(command, cwd) {
		if !of(programDelivery(t.Program, 0)) {
			continue
		}
		placed++
		r := t.Root
		if !placeable(t.Program, command) || r == guard.UnknownRoot || r == "" {
			r = top
		}
		add(r)
	}
	if placed < want || len(roots) == 0 {
		add(top)
	}
	return roots
}

// placeable is whether p, a delivering program of command, delivers in the
// repository of the directory it runs in (or git's -C, --git-dir,
// --work-tree): a git or gh the reader names, with no code or unseen input,
// no git alias, and no repository gh is given.
func placeable(p sh.Program, command string) bool {
	if p.Code != nil || p.StdinOpaque != "" || len(p.Argv) == 0 || unnamed.MatchString(p.Argv[0]) {
		return false
	}
	switch p.Name {
	case "git":
		g := sh.GitParts(p.Argv[1:])
		return !unnamed.MatchString(g.Sub) && !slices.ContainsFunc(g.Configs, func(c string) bool {
			return strings.HasPrefix(strings.ToLower(c), "alias.")
		})
	case "gh":
		return !strings.Contains(command, "GH_REPO") && !slices.ContainsFunc(p.Argv[1:], func(a string) bool {
			return strings.HasPrefix(a, "-R") || strings.HasPrefix(a, "--repo")
		})
	}
	return false
}
