package hook

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

var (
	// Operators, substitution, expansion, redirection, quoting, globbing,
	// comments, and line breaks: each defeats the prefix check.
	shellSyntax = lazyre.New(`[;&|` + "`" + `$(){}<>\\'"\n\r*?[\]#!]`)
	leadingGit  = lazyre.New(`^\s*git\s`)
	// The three wrappers an allowed read may carry (docket AT1): a trailing
	// `| head`, `| tail`, or `| wc` with bounded arguments, a literal
	// `2>/dev/null`, and one leading `cd <plain dir> &&`.
	trailingPipe = lazyre.New(`\s*\|\s*(?:(?:head|tail)(?:\s+(?:-n\s*[0-9]+|-[0-9]+))?|wc(?:\s+-[lwc])?)\s*$`)
	quietErrors  = lazyre.New(`(^|\s)2>/dev/null(\s|$)`)
	leadingCd    = lazyre.New(`^\s*cd\s+([^\s;&|<>$` + "`" + `'"\\(){}*?[\]#!~]+)\s+&&\s+`)
	writerOpts   = []string{"--output", "--ext-diff", "--textconv"}
)

// withoutTrees drops each `^{tree}` that ends a git argument, which is
// literal to the shell (docket F8): one preceded by a character other than
// a space or a brace, and followed by a space or the end.
func withoutTrees(s string) string {
	const tree = "^{tree}"
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], tree) && i > 0 && !strings.ContainsRune(" \t\n\r\f\v{}", rune(s[i-1])) {
			if j := i + len(tree); j == len(s) || strings.ContainsRune(" \t\n\r\f\v", rune(s[j])) {
				i = j
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// InspectGuard narrows Bash to the read-only prefixes an inspecting agent
// was given (inspect-guard). It never parses shell: anything
// whose meaning depends on shell parsing is refused.
func InspectGuard(args []string, io IO) int {
	scoped, name, rest := owner(args)
	var allowed [][]string
	var shown []string
	for _, a := range rest {
		if f := strings.Fields(a); len(f) > 0 {
			allowed = append(allowed, f)
			shown = append(shown, strings.Join(f, " "))
		}
	}
	list := strings.Join(shown, ", ")
	var p Payload
	block := func(reason string) int {
		fmt.Fprintf(io.Err, "inspect-guard: %s\n%s", reason, staleNote(p))
		return 2
	}
	blockForm := func(reason string) int {
		forms := ""
		if list != "" {
			forms = "allowed forms: " + list + "\n"
		}
		fmt.Fprintf(io.Err, "inspect-guard: %s\n%s%s", reason, forms, staleNote(p))
		return 2
	}
	p, ok := read(io.In)
	if !ok {
		return block("hook payload is not valid JSON")
	}
	if scoped && name == "" {
		return block("--agent was given without an agent name")
	}
	if othersCall(scoped, name, p) {
		return 0
	}
	if p["tool_name"] != "Bash" {
		return 0
	}
	command, isStr := p.Obj("tool_input")["command"].(string)
	if !isStr || strings.TrimSpace(command) == "" {
		return block("Bash call carries no command")
	}
	// Strip the wrappers; what is left is judged as any command is.
	base, original := p.Cwd(), command
	command = trailingPipe.ReplaceAllString(command, "")
	command = quietErrors.ReplaceAllString(command, "$1$2")
	if m := leadingCd.FindStringSubmatch(command); m != nil {
		dir := project.Resolve(base, m[1])
		if !inProject(dir, base) {
			return blockForm(fmt.Sprintf("cd %s is outside the session's project (%s) and its worktrees", m[1], base))
		}
		base, command = dir, command[len(m[0]):]
	}
	judged := command
	if leadingGit.MatchString(command) {
		judged = withoutTrees(command)
	}
	if shellSyntax.MatchString(judged) {
		return blockForm("shell syntax is not allowed here: " + marshal(original))
	}

	tokens := strings.Fields(command)
	if len(tokens) > 1 && tokens[0] == "git" && tokens[1] == "-C" {
		g := sh.GitParts(tokens[1:])
		path := ""
		if len(g.Globals) > 1 {
			path = g.Globals[1]
		}
		if len(g.Globals) != 2 || g.Globals[0] != "-C" || path == "" || strings.HasPrefix(path, "~") || g.Sub == "" {
			return blockForm("only one leading `git -C <path>` into the session's project is read here: " + marshal(original))
		}
		cwd := p.Cwd()
		if !inProject(project.Resolve(base, path), cwd) {
			return blockForm(fmt.Sprintf("git -C %s is outside the session's project (%s) and its worktrees", path, cwd))
		}
		tokens = append([]string{"git", g.Sub}, g.Rest...)
	}
	for _, t := range tokens {
		if strings.HasPrefix(t, "~") {
			return blockForm("tilde expansion is not allowed")
		}
	}
	matches := false
	for _, pre := range allowed {
		if len(pre) <= len(tokens) && slicesEqual(pre, tokens[:len(pre)]) {
			matches = true
			break
		}
	}
	if !matches {
		none := list
		if none == "" {
			none = "none"
		}
		return blockForm("command is outside this agent's Bash allowlist (" + none + ")")
	}
	// git accepts any unambiguous abbreviation of a long option.
	for _, t := range tokens {
		name, _, _ := strings.Cut(t, "=")
		if len(name) <= 2 || !strings.HasPrefix(name, "--") {
			continue
		}
		for _, w := range writerOpts {
			if strings.HasPrefix(w, name) || strings.HasPrefix(name, w) {
				return block("option " + name + " is not allowed: it writes a file or runs an external program")
			}
		}
	}
	return 0
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// inProject is whether p is inside cwd, or inside a linked worktree of cwd's repository.
func inProject(p, cwd string) bool {
	real := func(x string) string {
		if r, err := filepath.EvalSymlinks(x); err == nil {
			return r
		}
		return project.Resolve("", x)
	}
	if project.Within(real(p), real(cwd)) {
		return true
	}
	if !project.Exists(p) {
		return false
	}
	common := func(dir string) string {
		c := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
		c.Env = project.GitReadEnv()
		out, err := c.Output()
		if err != nil {
			return ""
		}
		return real(strings.TrimSpace(string(out)))
	}
	mine := common(cwd)
	return mine != "" && common(p) == mine
}
