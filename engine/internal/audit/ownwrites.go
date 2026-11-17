package audit

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// Two checks of where an executor writes (T56), each pending-real-fixture
// until real clean and violation fixtures back it:
//
//	isolation-bypass, pi form (isolation/no-other-tree, `pending_on: [pi]`):
//	    pi has no runtime isolation to refuse a write, so a pi worker or
//	    verify child's Edit or Write outside its own tree is itself the write
//	    in another tree; once per child. Its tree is the cwd its session
//	    header records and every absolute path its launch prompt names (the
//	    worktree the orchestrator hands it: pi records the child's cwd as the
//	    main session's).
//	verify-wrote (ldd/independent-verify): a verify agent's Edit or Write
//	    of anything but its verify-report.md; once per agent.
//	explore-wrote (ldd/explore-scope, T123): an explore agent's Edit or
//	    Write that ran (its result is no error: a refused write never ran)
//	    of anything but a change's exploration.md or research.md, in either
//	    layout (guard.ExploreMayWrite, the rule vcs-guard holds it to);
//	    once per agent.
//
// Neither reads a write through Bash, nor one under the system temp
// directory, where test scratch lives.

// writeOf is the tool and file of an Edit, MultiEdit, or Write, Claude Code's or pi's.
func writeOf(u ToolUse) (tool, file string, ok bool) {
	switch u.Name {
	case "Edit", "MultiEdit", "Write", "edit", "write":
	default:
		return "", "", false
	}
	file, _ = strProp(u.Input, "file_path")
	if file == "" {
		file, _ = strProp(u.Input, "path")
	}
	return u.Name, file, file != ""
}

// scratch is whether p is under the system temp directory.
func scratch(p string) bool {
	return strings.HasPrefix(p, "/tmp/") || strings.HasPrefix(p, "/var/tmp/")
}

// cwdOf is the directory a child's session records, else its launch's root.
func cwdOf(a *Subagent) string {
	for _, e := range a.Entries {
		if e.Cwd != "" {
			return normalizeRoot(e.Cwd)
		}
	}
	return a.root
}

// namedPath is an absolute path of at least three parts in a launch prompt.
var namedPath = lazyre.New(`(?:/[\w.@+~-]+){3,}`)

// handedTrees are the absolute paths a's launch prompt names, normalised.
func handedTrees(a *Subagent) []string {
	var out []string
	for _, p := range namedPath.FindAllString(a.LaunchPrompt, -1) {
		out = append(out, normalizeRoot(strings.TrimRight(p, ".")))
	}
	return out
}

func checkPiIsolation(s *Session, emit emitFunc) {
	if s.Runtime != "pi" {
		return
	}
	for _, a := range s.Subagents {
		root := cwdOf(a)
		if root == "" || (a.AgentType != "worker" && a.AgentType != "verify") {
			continue
		}
		handed := handedTrees(a)
	agent:
		for _, e := range a.Entries {
			for _, u := range e.ToolUses {
				tool, file, ok := writeOf(u)
				if !ok {
					continue
				}
				p := file
				if !strings.HasPrefix(p, "/") {
					p = join(root, p)
				}
				p = normalizeRoot(p)
				inTree := func(t string) bool { _, in := under(p, t); return in || p == t }
				if inTree(root) || slices.ContainsFunc(handed, inTree) || scratch(p) {
					continue
				}
				emit(emitted{check: "isolation-bypass", anchor: "isolation/no-other-tree", severity: "violation", transcript: a.File, line: e.Line,
					agent: a.Agent, agentType: a.AgentType, evidence: short(tool+" "+p, 120),
					message: fmt.Sprintf("this pi %s child's %s writes %s, outside its own tree %s: pi has no runtime isolation to refuse it, so the write in another tree is the bypass (contracts/runtime-boundaries.md)",
						a.AgentType, tool, p, root)})
				break agent
			}
		}
	}
}

// verifyRecord is the file a verify agent writes.
const verifyRecord = "verify-report.md"

func checkVerifyWrote(s *Session, emit emitFunc) {
	for _, a := range s.Subagents {
		if a.AgentType != "verify" {
			continue
		}
	agent:
		for _, e := range a.Entries {
			for _, u := range e.ToolUses {
				tool, file, ok := writeOf(u)
				if !ok || path.Base(file) == verifyRecord || scratch(file) {
					continue
				}
				emit(emitted{check: "verify-wrote", severity: "violation", transcript: a.File, line: e.Line,
					agent: a.Agent, agentType: a.AgentType, evidence: short(tool+" "+file, 120),
					message: fmt.Sprintf("the verify agent's %s writes %s, not its %s: an independent verifier reports and fixes nothing (contracts/ldd.md)", tool, file, verifyRecord)})
				break agent
			}
		}
	}
}

func checkExploreWrote(s *Session, emit emitFunc) {
	for _, a := range s.Subagents {
		if a.AgentType != guard.ExploreAgent {
			continue
		}
		failed := map[string]bool{}
		for _, e := range a.Entries {
			for _, r := range e.ToolResults {
				failed[r.ID] = r.IsError
			}
		}
	agent:
		for _, e := range a.Entries {
			for _, u := range e.ToolUses {
				tool, file, ok := writeOf(u)
				if !ok || failed[u.ID] {
					continue
				}
				p := file
				if !strings.HasPrefix(p, "/") {
					p = join(cwdOf(a), p)
				}
				if guard.ExploreMayWrite(p, cwdOf(a)) || scratch(p) {
					continue
				}
				emit(emitted{check: "explore-wrote", severity: "violation", transcript: a.File, line: e.Line,
					agent: a.Agent, agentType: a.AgentType, evidence: short(tool+" "+file, 120),
					message: fmt.Sprintf("the explore agent's %s writes %s, not its change's exploration.md or research.md: explore investigates and writes only that record, never code or the change's other artifacts (prose/agents.yaml)", tool, file)})
				break agent
			}
		}
	}
}
