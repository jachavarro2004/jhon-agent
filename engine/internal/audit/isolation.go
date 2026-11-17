package audit

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	lddlayout "github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
)

// isolation-bypass, and what a sub-agent's launch says about it: its
// root, its change, and its phase.

// contextFence is the info string of the fence an optional `jhon context` launch context is pasted in.
const contextFence = "jhon-context"

var fenceLine = lazyre.New(`^` + space + "*(`{3,}|~{3,})(" + js.Dot + `*)$`)

// launchBlock is the parsed first ```jhon-context block of a launch prompt, or nil.
func launchBlock(prompt string) *js.Object {
	lines := strings.Split(prompt, "\n")
	for i := 0; i < len(lines); i++ {
		m := fenceLine.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		close := regexp.MustCompile(`^` + space + `*` + regexp.QuoteMeta(m[1][:1]) + fmt.Sprintf("{%d,}", len(m[1])) + space + `*$`)
		j := i + 1
		for j < len(lines) && !close.MatchString(lines[j]) {
			j++
		}
		if js.Trim(m[2]) == contextFence {
			v, err := js.ParseJSON(strings.Join(lines[i+1:min(j, len(lines))], "\n"))
			if err != nil {
				return nil
			}
			o, _ := v.(*js.Object)
			return o
		}
		i = j
	}
	return nil
}

// under is path relative to root, both lexically normalized; ok is false when it is not strictly under it.
func under(path, root string) (string, bool) {
	p := normalizeRoot(path)
	if root == "/" {
		if len(p) > 1 {
			return p[1:], true
		}
		return "", false
	}
	if strings.HasPrefix(p, root+"/") {
		return p[len(root)+1:], true
	}
	return "", false
}

// rootOf is a sub-agent's root: its launch block's project_root, else the
// directory its transcript records; "" when neither is known.
func rootOf(a *Subagent) string {
	if r, _ := strProp(launchBlock(a.LaunchPrompt), "project_root"); r != "" {
		return normalizeRoot(r)
	}
	for _, e := range a.Entries {
		if e.Cwd != "" {
			return normalizeRoot(e.Cwd)
		}
	}
	return ""
}

// addsWorktree is whether a command line runs `git worktree add`.
func addsWorktree(command string) bool {
	for _, x := range codeless(raw(command)) {
		if x.Program == "git" && x.Sub == "worktree" {
			if next, _ := at(x.Args, indexOf(x.Args, "worktree")+1); next == "add" {
				return true
			}
		}
	}
	return false
}

// checkIsolationBypass is isolation-bypass (contracts/runtime-boundaries.md;
// contracts/ldd.md section I). In one sub-agent, an Edit or Write
// whose result is a runtime isolation refusal of a path under the
// sub-agent's root fixes the refused repository-relative path. A later Edit
// or Write in the same agent to that path under any other root is the
// refused effect reached in another tree: a violation, once per refused
// path. The evidence names the last `git worktree add` before it, when
// there is one. Not read: a retry of the refused path itself, another path,
// a write through Bash, a refusal outside the root, and a sub-agent whose
// root is unknown.
func checkIsolationBypass(s *Session, emit emitFunc) {
	type write struct {
		tool, path string
		line       int
	}
	type refusedWrite struct {
		write
		rel string
	}
	for _, a := range s.Subagents {
		root := a.root
		if root == "" {
			continue
		}
		writes := map[string]write{}
		var refused []refusedWrite
		reportedRel := map[string]bool{}
		worktree := 0
		for _, e := range a.Entries {
			for _, r := range e.ToolResults {
				w, ok := writes[r.ID]
				if !ok || !r.IsError || !anyMatch(isolationMarkers, js.Trim(r.Text)) {
					continue
				}
				rel, ok := under(w.path, root)
				if ok && !slices.ContainsFunc(refused, func(x refusedWrite) bool { return x.rel == rel }) {
					refused = append(refused, refusedWrite{write{w.tool, w.path, e.Line}, rel})
				}
			}
			for _, u := range e.ToolUses {
				if c, ok := strProp(u.Input, "command"); ok && u.Name == "Bash" && addsWorktree(c) {
					worktree = e.Line
				}
				file, _ := strProp(u.Input, "file_path")
				if (u.Name != "Edit" && u.Name != "Write") || file == "" {
					continue
				}
				path := file
				if !strings.HasPrefix(file, "/") {
					path = join(root, file)
				}
				path = normalizeRoot(path)
				writes[u.ID] = write{u.Name, path, e.Line}
				for _, x := range refused {
					if reportedRel[x.rel] || path == x.path || !strings.HasSuffix(path, "/"+x.rel) {
						continue
					}
					other := path[:len(path)-len(x.rel)-1]
					if other == "" {
						other = "/"
					}
					if other == root {
						continue
					}
					reportedRel[x.rel] = true
					created := ""
					if worktree != 0 {
						created = fmt.Sprintf(", a tree `git worktree add` created at line %d", worktree)
					}
					emit(emitted{check: "isolation-bypass", severity: "violation", transcript: a.File, line: e.Line, agent: a.Agent, agentType: a.AgentType,
						evidence: short(u.Name+" "+path, 120),
						message: fmt.Sprintf("runtime isolation refused %s %s at %s:%d; this %s writes the same repository-relative path %s under another root, %s%s: the refused effect in another tree is a bypass (contracts/runtime-boundaries.md)",
							x.tool, x.path, a.File, x.line, u.Name, x.rel, other, created)})
				}
			}
		}
	}
}

var (
	changePath = lazyre.New(`(?:` + lddlayout.DirsPattern() + `)/changes/([\w.-]+)`)
	changeLine = lazyre.New(`(?m)^` + space + `*(?:[-*]` + space + `+)?` + "`?change`?" + space + `*[:=]` + space + "*`?([\\w.-]+)`?" + space + `*$`)
)

// changesNamed is the distinct changes an openspec/changes/<change> path in text names, the archive aside.
func changesNamed(text string) []string {
	var out []string
	for _, m := range changePath.FindAllStringSubmatch(text, -1) {
		if m[1] != "archive" {
			out = append(out, m[1])
		}
	}
	return uniq(out)
}

// changeOf is the change a delegation is for: its launch block's `change`;
// else the one change its launch prompt names by its
// `openspec/changes/<change>` path (the archive aside) or by a `change:
// <name>` line; else "", kept as unknown.
func changeOf(prompt string) string {
	if block := launchBlock(prompt); block != nil && block.Has("change") {
		c, _ := strProp(block, "change")
		return c
	}
	if named := changesNamed(prompt); len(named) == 1 {
		return named[0]
	}
	var lines []string
	for _, m := range changeLine.FindAllStringSubmatch(prompt, -1) {
		if m[1] != "null" && m[1] != "none" {
			lines = append(lines, m[1])
		}
	}
	if lines = uniq(lines); len(lines) == 1 {
		return lines[0]
	}
	return ""
}

// phaseOf is the phase a delegation is to: its block's `phase`; else the
// phase the agent type always runs (`verify`); else "". `explore` and
// `worker` also run work outside a change's phases, so only their block
// names a phase.
func phaseOf(prompt, agentType string) string {
	if p, _ := strProp(launchBlock(prompt), "phase"); p != "" {
		return p
	}
	if agentType == "verify" {
		return "verify"
	}
	return ""
}
