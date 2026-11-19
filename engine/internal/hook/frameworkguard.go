package hook

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

const frameworkRule = "no Jhon agent writes the framework's source, releases, current pointer, CLI link, owned projections, or " +
	"~/.claude/CLAUDE.md, or runs git on them (docket AV2). A framework rule that blocks project work is reported as a " +
	"blocker; changing it is a separate framework docket, outside Jhon agents. Stop and report this refusal verbatim; " +
	"never reach the same effect another way (contracts/runtime-boundaries.md)."

// byHand is what an agent does instead of running Jhon's own code (T81, L596).
const byHand = "Jhon's own code is never run or imported by hand: launch a sub-agent with the subagent tool (pi) or the Agent tool " +
	"(Claude Code), and read the change with inspect. "

var (
	// scriptRunners run a script file whose code the guard never reads: what
	// it runs is known only from the paths it is handed.
	scriptRunners = map[string]bool{"node": true, "deno": true, "bun": true, "tsx": true, "ts-node": true, "perl": true, "ruby": true, "php": true, "Rscript": true, "lua": true}
	pythonName    = lazyre.New(`^python[0-9.]*$`)
)

// expand resolves a word's leading ~ against HOME.
func expand(w string) string {
	if w == "~" {
		return project.Home()
	}
	if strings.HasPrefix(w, "~/") {
		return project.Resolve(project.Home(), w[2:])
	}
	return w
}

// FrameworkGuard refuses every Jhon agent's file-tool write of the framework
// (framework-guard, docket AV2) and a Bash line that runs Jhon's own code by
// hand (T81). A Bash write outside the project is the sandbox's (T128f).
func FrameworkGuard(args []string, io IO) int {
	scoped, name, rest := owner(args)
	var roots []string
	for _, r := range rest {
		if r = strings.TrimSpace(r); r != "" {
			roots = append(roots, r)
		}
	}
	var p Payload
	block := func(reason string) int {
		fmt.Fprintf(io.Err, "framework-guard: %s\n%s", reason, staleNote(p))
		return 2
	}
	p, ok := read(io.In)
	if !ok {
		return block("hook payload is not valid JSON")
	}
	if !scoped || name == "" {
		return block("the guard was rendered without --agent <owner>; the render is broken")
	}
	if othersCall(scoped, name, p) {
		return 0
	}
	if len(roots) == 0 {
		return block("no protected paths were given; the render is broken")
	}
	tool := p.Str("tool_name")
	if tool != "Edit" && tool != "MultiEdit" && tool != "Write" && tool != "Bash" {
		return 0
	}
	input := p.Obj("tool_input")
	cwd := p.Cwd()

	// Each protected path as written and through its real path.
	var guarded []string
	for _, r := range roots {
		abs := project.Resolve("", r)
		guarded = append(guarded, abs, project.Realish(abs))
	}
	hit := func(w, dir string) string {
		if w == "" {
			return ""
		}
		abs := project.Resolve(dir, expand(w))
		for _, q := range []string{abs, project.Realish(abs)} {
			if at := slices.IndexFunc(guarded, func(r string) bool { return project.Within(q, r) }); at >= 0 {
				return roots[at/2]
			}
		}
		return ""
	}

	if tool != "Bash" {
		_, paths, _ := writes(p)
		if len(paths) == 0 {
			return block(tool + " call carries no file_path")
		}
		for _, raw := range paths {
			if root := hit(raw, cwd); root != "" {
				return block(fmt.Sprintf("%s of %s is under the protected path %s: %s", tool, raw, root, frameworkRule))
			}
		}
		return 0
	}

	// A Bash line is the sandbox's (T128f): outside the project it writes
	// nothing. What stays here is the T81 rule, which no sandbox enforces:
	// Jhon's own code is never run or imported by hand.
	command, _ := input["command"].(string)
	if strings.TrimSpace(command) == "" {
		return block("Bash call carries no command")
	}
	cmds, err := sh.Parse(command)
	if err != nil {
		return 0
	}
	// Walk the programs in order, following each cd. What a line runs is
	// read from its visible words: a path named elsewhere in it (a reader,
	// a heredoc a script is written from) is no script's (L1311).
	dir := cwd
	judge := func(p sh.Program, c *sh.Command) string {
		raw := c.Raw
		if p.Raw != nil {
			raw = *p.Raw
		}
		prog, args := "", []string(nil)
		if len(p.Argv) > 0 {
			prog, args = p.Argv[0], p.Argv[1:]
		}
		// Interpreter code naming a protected path runs or imports Jhon's own
		// code by hand (T81, L596: sub-agents launched from inline node code
		// importing shell/lib/agents.ts, outside every Jhon tool).
		interpreter := scriptRunners[p.Name] || pythonName.MatchString(p.Name)
		if p.Code != nil && interpreter {
			for _, t := range sh.PathTokens.FindAllString(*p.Code, -1) {
				if root := hit(t, dir); root != "" {
					return fmt.Sprintf("`%s` runs %s code naming the protected path %s. %s%s", raw, p.Name, root, byHand, frameworkRule)
				}
			}
		}
		// A script run from a file is code the guard cannot read: handed a
		// protected path, or run inside a protected directory, it may run or
		// import Jhon's own code.
		if p.Code == nil && len(args) > 0 && (interpreter || sh.Shells[p.Name]) {
			root := hit(dir, dir)
			for _, a := range args {
				if root == "" && !strings.HasPrefix(a, "-") {
					root = hit(a, dir)
				}
			}
			if root != "" {
				return fmt.Sprintf("`%s` runs a script, whose code this guard cannot read, on the protected path %s. %s%s", raw, root, byHand, frameworkRule)
			}
		}
		if (p.Name == "cd" || p.Name == "pushd") && prog != "" {
			to := "~"
			if i := slices.IndexFunc(args, func(a string) bool { return !strings.HasPrefix(a, "-") }); i >= 0 {
				to = args[i]
			}
			dir = project.Resolve(dir, expand(to))
		}
		return ""
	}
	for _, c := range cmds {
		ps, err := sh.CommandPrograms(c, nil, nil, sh.Opts{})
		if err != nil {
			continue
		}
		for _, prog := range ps {
			// env -C: this program runs there; the command's own directory does not move.
			outer := dir
			for _, d := range prog.Chdir {
				dir = project.Resolve(dir, expand(d))
			}
			if why := judge(prog, c); why != "" {
				return block(why)
			}
			if len(prog.Chdir) > 0 {
				dir = outer
			}
		}
	}
	return 0
}
