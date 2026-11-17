package audit

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// How each agent role used the integrations (T134, L1021 H8): memory,
// CodeGraph and the todo list, in counts. A statistic like the dialogue:
// never a finding, and no tool argument, memory text or file name ever
// leaves it. The main thread is the orchestrator; a sub-agent's role is its
// agent type.

// orchestrator is the main thread's role.
const orchestrator = "orchestrator"

// RoleUse is what the threads of one role did: how many ran (Launches), how
// many called a memory read and a memory write at least once, their
// CodeGraph calls, their plain reads, and their todo calls.
type RoleUse struct {
	Role                                    string
	Launches                                int
	MemoryReadLaunches, MemoryWriteLaunches int
	CodegraphCalls, PlainReads, TodoCalls   int
}

// Integrations is the session's roles, the orchestrator first, then by name.
type Integrations struct {
	Roles []RoleUse
}

// Engram's tools, by their own name: those that read memory and those that write it.
var (
	memoryReads  = []string{"mem_search", "mem_context", "mem_get_observation", "mem_current_project"}
	memoryWrites = []string{"mem_save", "mem_update", "mem_judge", "mem_session_summary", "mem_save_prompt"}
)

// readTools are the tools that read files or list them, by their own name:
// Claude Code's, and pi's (piTools maps them onto the first).
var readTools = []string{"Read", "read", "Grep", "grep", "Glob", "find", "LS", "ls"}

// todoTools are the tools of the agent's todo list.
var todoTools = []string{"TodoWrite", "todo"}

// readPrograms are the programs whose run, first in a command line, is a
// plain read; sed only with -n.
var readPrograms = []string{"cat", "sed", "head", "tail", "grep", "rg", "ls", "find"}

// ownName is a tool's own name: an MCP tool's last part, whatever its
// server or plugin prefix (`mcp__plugin_engram_engram__mem_search` is
// mem_search), the name itself otherwise.
func ownName(name string) string {
	if !strings.HasPrefix(name, "mcp__") {
		return name
	}
	return name[strings.LastIndex(name, "__")+2:]
}

// firstProgram is the first program a command line runs, after an optional
// leading `cd <dir> &&`; ok is false for none.
func firstProgram(command string) (Invocation, bool) {
	invs := raw(command)
	if len(invs) == 0 {
		return Invocation{}, false
	}
	if invs[0].Program == "cd" {
		rest := strings.TrimLeftFunc(strings.TrimPrefix(js.Trim(command), invs[0].Raw), js.IsSpace)
		if len(invs) < 2 || !strings.HasPrefix(rest, "&&") {
			return Invocation{}, false
		}
		return invs[1], true
	}
	return invs[0], true
}

// plainShellRead is whether a shell command is a plain read: its first
// program, after an optional `cd <dir> &&`, is one of readPrograms.
func plainShellRead(command string) bool {
	x, ok := firstProgram(command)
	if !ok || !slices.Contains(readPrograms, x.Program) {
		return false
	}
	return x.Program != "sed" || slices.ContainsFunc(x.Args, func(a string) bool { return sedQuiet.MatchString(a) || a == "--quiet" || a == "--silent" }) &&
		!slices.ContainsFunc(x.Args, func(a string) bool { return sedInPlace.MatchString(a) || strings.HasPrefix(a, "--in-place") })
}

// asksCodegraph is whether a call asks CodeGraph: a tool of its own (pi's
// codegraph, an MCP server's codegraph_*), or a shell command whose first
// program is codegraph.
func asksCodegraph(u ToolUse) bool {
	if strings.HasPrefix(ownName(u.Name), integrations.CodeGraph) {
		return true
	}
	if server, ok := mcpServer(u.Name); ok && strings.Contains(server, integrations.CodeGraph) {
		return true
	}
	if c, ok := shellCommand(u); ok {
		x, ok := firstProgram(c)
		return ok && x.Program == codegraphBinary()
	}
	return false
}

// codegraphBinary is CodeGraph's program, its manifest's doctor.binary.
func codegraphBinary() string {
	m, _ := integrations.Get(integrations.CodeGraph)
	return m.Doctor.Binary
}

// shellCommand is a shell call's command line; ok is false for any other call.
func shellCommand(u ToolUse) (string, bool) {
	if n := ownName(u.Name); n != "Bash" && n != "bash" {
		return "", false
	}
	return strProp(u.Input, "command")
}

// threadUse is what one thread did, as one launch of its role. Every call
// counts, whatever its result.
func threadUse(t *Transcript, role string) RoleUse {
	u := RoleUse{Role: role, Launches: 1}
	var read, wrote bool
	for _, e := range t.Entries {
		for _, x := range e.ToolUses {
			name := ownName(x.Name)
			command, shell := shellCommand(x)
			switch {
			case slices.Contains(memoryReads, name):
				read = true
			case slices.Contains(memoryWrites, name):
				wrote = true
			case slices.Contains(todoTools, name):
				u.TodoCalls++
			case asksCodegraph(x):
				u.CodegraphCalls++
			case slices.Contains(readTools, name), shell && plainShellRead(command):
				u.PlainReads++
			}
		}
	}
	if read {
		u.MemoryReadLaunches = 1
	}
	if wrote {
		u.MemoryWriteLaunches = 1
	}
	return u
}

// computeIntegrations counts the integrations each role of s used: the
// main thread as the orchestrator, each sub-agent by its agent type.
func computeIntegrations(s *Session) *Integrations {
	f := &FleetIntegrations{}
	add := func(u RoleUse) { f.add(&Integrations{Roles: []RoleUse{u}}) }
	add(threadUse(s.Main, orchestrator))
	for _, a := range s.Subagents {
		add(threadUse(&a.Transcript, or(a.AgentType, "unknown")))
	}
	if f.Roles == nil {
		f.Roles = []RoleUse{}
	}
	return &f.Integrations
}

// CodegraphShare is the CodeGraph calls' share of the CodeGraph calls and
// plain reads, NaN with neither.
func (u RoleUse) CodegraphShare() float64 {
	if n := u.CodegraphCalls + u.PlainReads; n > 0 {
		return float64(u.CodegraphCalls) / float64(n)
	}
	return math.NaN()
}

func (u RoleUse) value() *js.Object {
	return js.Obj("launches", float64(u.Launches), "launches_with_memory_read", float64(u.MemoryReadLaunches),
		"launches_with_memory_write", float64(u.MemoryWriteLaunches), "codegraph_calls", float64(u.CodegraphCalls),
		"plain_reads", float64(u.PlainReads), "codegraph_share", measure(u.CodegraphShare()), "todo_calls", float64(u.TodoCalls))
}

// value is the roles as the report states them: one entry per role.
func (x Integrations) value() *js.Object {
	o := js.NewObject()
	for _, u := range x.Roles {
		o.Set(u.Role, u.value())
	}
	return o
}

// Percent is a share as a person reads it, to one decimal; "-" for NaN.
func Percent(share float64) string {
	if math.IsNaN(share) {
		return "-"
	}
	return js.ToFixed(100*share, 1) + "%"
}

// humanIntegrations is one line per role that ran.
func humanIntegrations(x *Integrations) []string {
	var out []string
	for _, u := range x.Roles {
		if u.Launches == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("  integrations  %s  launches %d  memory read %d  memory write %d  codegraph %d  plain reads %d  codegraph share %s  todo %d",
			u.Role, u.Launches, u.MemoryReadLaunches, u.MemoryWriteLaunches, u.CodegraphCalls, u.PlainReads, Percent(u.CodegraphShare()), u.TodoCalls))
	}
	return out
}

// sortRoles puts the orchestrator first, then the roles by name.
func sortRoles(us []RoleUse) {
	slices.SortFunc(us, func(a, b RoleUse) int {
		switch {
		case a.Role == b.Role:
			return 0
		case a.Role == orchestrator:
			return -1
		case b.Role == orchestrator:
			return 1
		}
		return strings.Compare(a.Role, b.Role)
	})
}

// FleetIntegrations is the integrations of many sessions (`jhon audit
// --all`): each role's counts summed over the sessions whose audit read them.
type FleetIntegrations struct {
	Sessions int
	Integrations
}

// add sums one session's integrations into the fleet's.
func (f *FleetIntegrations) add(x *Integrations) {
	f.Sessions++
	for _, u := range x.Roles {
		i := slices.IndexFunc(f.Roles, func(r RoleUse) bool { return r.Role == u.Role })
		if i < 0 {
			f.Roles = append(f.Roles, RoleUse{Role: u.Role})
			i = len(f.Roles) - 1
		}
		r := &f.Roles[i]
		r.Launches += u.Launches
		r.MemoryReadLaunches += u.MemoryReadLaunches
		r.MemoryWriteLaunches += u.MemoryWriteLaunches
		r.CodegraphCalls += u.CodegraphCalls
		r.PlainReads += u.PlainReads
		r.TodoCalls += u.TodoCalls
	}
	sortRoles(f.Roles)
}

// Value is the fleet's integrations as `jhon audit --all --json` states
// them: the sessions summed, then one entry per role.
func (f FleetIntegrations) Value() *js.Object {
	return js.Obj("sessions", float64(f.Sessions), "roles", f.Integrations.value())
}
