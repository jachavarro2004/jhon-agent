package audit

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// How each agent role used memory, CodeGraph and the todo list
// (integrations.go, T134 L1021 H8): the same script told to a Claude Code
// transcript and to a pi session gives the same counts.

// used is one tool call as each runtime names it, with its input; a
// command is a shell command.
type used struct {
	claude, pi string
	input      *js.Object
}

func memSearch() used {
	return used{"mcp__plugin_engram_engram__mem_search", "mem_search", obj("query", "SECRETQUERY")}
}
func memContext() used { return used{"mcp__engram__mem_context", "mem_context", obj()} }
func memGet() used {
	return used{"mcp__engram__mem_get_observation", "mem_get_observation", obj("id", 7.0)}
}
func memProject() used { return used{"mcp__engram__mem_current_project", "mem_current_project", obj()} }
func memSave() used {
	return used{"mcp__plugin_engram_engram__mem_save", "mem_save", obj("content", "SECRETMEMORY")}
}
func memUpdate() used {
	return used{"mcp__engram__mem_update", "mem_update", obj("content", "SECRETMEMORY")}
}
func codegraphCall() used {
	return used{"mcp__codegraph__codegraph_explore", "codegraph", obj("query", "SECRETSYMBOL")}
}
func readFile() used  { return used{"Read", "read", obj("file_path", "SECRETFILE.go")} }
func grepFiles() used { return used{"Grep", "grep", obj("pattern", "SECRETPATTERN")} }
func globFiles() used { return used{"Glob", "find", obj("pattern", "SECRETGLOB")} }
func listDir() used   { return used{"LS", "ls", obj("path", "SECRETDIR")} }
func todoCall() used  { return used{"TodoWrite", "todo", obj("todos", []any{})} }
func shell(command string) used {
	return used{"Bash", "bash", obj("command", command)}
}

// launched is one sub-agent of the script: its type and its calls.
type launched struct {
	typ   string
	calls []used
}

// integrationScript is a session: the main thread's calls, then each sub-agent's.
type integrationScript struct {
	main []used
	subs []launched
}

func (x integrationScript) claude(t *testing.T) *Result {
	t.Helper()
	var steps []step
	for _, u := range x.main {
		steps = append(steps, step{kind: "tool", tool: u.claude, input: u.input})
	}
	for i, s := range x.subs {
		var calls []tcall
		for _, u := range s.calls {
			calls = append(calls, tcall{name: u.claude, input: u.input})
		}
		steps = append(steps, delegate("a"+string(rune('0'+i)), s.typ, chg, calls...))
	}
	return auditOf(t, steps...)
}

func (x integrationScript) pi(t *testing.T) *Result {
	t.Helper()
	steps := []pstep{pstart("tui")}
	for _, u := range x.main {
		steps = append(steps, pmain(pcall{name: u.pi, args: piArgs(u), result: "ok"}))
	}
	for _, s := range x.subs {
		var calls []pcall
		for _, u := range s.calls {
			calls = append(calls, pcall{name: u.pi, args: piArgs(u), result: "ok"})
		}
		steps = append(steps, psub(s.typ, "the task", "done", calls...))
	}
	return piAudit(t, steps...)
}

// piArgs is u's input as pi names its keys: a path, not a file_path.
func piArgs(u used) *js.Object {
	o := js.NewObject()
	for _, k := range u.input.Keys() {
		key := k
		if k == "file_path" {
			key = "path"
		}
		o.Set(key, js.Prop(u.input, k))
	}
	return o
}

// onBothRuntimes runs check on the script's audit as each runtime records it.
func onBothRuntimes(t *testing.T, x integrationScript, check func(t *testing.T, r *Result)) {
	t.Helper()
	t.Run("claude code", func(t *testing.T) { check(t, x.claude(t)) })
	t.Run("pi", func(t *testing.T) { check(t, x.pi(t)) })
}

// theIntegrations: the orchestrator reads and writes memory, asks CodeGraph
// once, reads three times (one a `cd x && cat f`; `cd x && go test` is no
// read) and writes its todo list twice; two workers, one explore that reads
// memory twice in one launch, and a verify that uses nothing.
var theIntegrations = integrationScript{
	main: []used{memSearch(), memSave(), codegraphCall(), readFile(), shell("cd /work/p && cat SECRETFILE.go"),
		shell("cd /work/p && go test ./..."), shell("sed -n '1,5p' SECRETFILE.go"), todoCall(), todoCall()},
	subs: []launched{
		{"worker", []used{memContext(), codegraphCall(), codegraphCall(), grepFiles(), shell("cd /work/p && rg SECRETPATTERN"), todoCall()}},
		{"worker", []used{memUpdate(), globFiles(), listDir(), shell("head -5 SECRETFILE.go"), shell("cd /work/p && go test ./...")}},
		{"explore", []used{memGet(), memProject(), readFile(), shell("tail -n 3 SECRETFILE.go")}},
		{"verify", []used{shell("go test ./...")}},
	},
}

// role is the counts of one role, failing the test when it is absent.
func role(t *testing.T, r *Result, name string) RoleUse {
	t.Helper()
	if r.Integrations == nil {
		t.Fatalf("no integrations")
	}
	for _, u := range r.Integrations.Roles {
		if u.Role == name {
			return u
		}
	}
	t.Fatalf("no role %s in %+v", name, r.Integrations.Roles)
	return RoleUse{}
}

func TestIntegrationsCountEachRolesLaunches(t *testing.T) {
	onBothRuntimes(t, theIntegrations, func(t *testing.T, r *Result) {
		var roles []string
		for _, u := range r.Integrations.Roles {
			roles = append(roles, u.Role)
		}
		eq(t, "roles: the orchestrator first, then by name", roles, []string{"orchestrator", "explore", "verify", "worker"})
		eq(t, "launches", [4]int{role(t, r, "orchestrator").Launches, role(t, r, "explore").Launches, role(t, r, "verify").Launches, role(t, r, "worker").Launches}, [4]int{1, 1, 1, 2})
	})
}

func TestIntegrationsCountTheLaunchesThatReadMemory(t *testing.T) {
	onBothRuntimes(t, theIntegrations, func(t *testing.T, r *Result) {
		eq(t, "launches with a memory read: orchestrator, explore, verify, worker",
			[4]int{role(t, r, "orchestrator").MemoryReadLaunches, role(t, r, "explore").MemoryReadLaunches, role(t, r, "verify").MemoryReadLaunches, role(t, r, "worker").MemoryReadLaunches},
			[4]int{1, 1, 0, 1})
	})
}

func TestIntegrationsCountTheLaunchesThatWriteMemory(t *testing.T) {
	onBothRuntimes(t, theIntegrations, func(t *testing.T, r *Result) {
		eq(t, "launches with a memory write: orchestrator, explore, verify, worker",
			[4]int{role(t, r, "orchestrator").MemoryWriteLaunches, role(t, r, "explore").MemoryWriteLaunches, role(t, r, "verify").MemoryWriteLaunches, role(t, r, "worker").MemoryWriteLaunches},
			[4]int{1, 0, 0, 1})
	})
}

func TestIntegrationsCountCodeGraphCalls(t *testing.T) {
	onBothRuntimes(t, theIntegrations, func(t *testing.T, r *Result) {
		eq(t, "CodeGraph calls: orchestrator, explore, verify, worker",
			[4]int{role(t, r, "orchestrator").CodegraphCalls, role(t, r, "explore").CodegraphCalls, role(t, r, "verify").CodegraphCalls, role(t, r, "worker").CodegraphCalls},
			[4]int{1, 0, 0, 2})
	})
}

// A plain read is a read, grep, find or ls tool, or a shell command whose
// first program, after an optional `cd <dir> &&`, reads: `cd x && cat f`
// is one, `cd x && go test` is not.
func TestIntegrationsCountPlainReads(t *testing.T) {
	onBothRuntimes(t, theIntegrations, func(t *testing.T, r *Result) {
		eq(t, "plain reads: orchestrator, explore, verify, worker",
			[4]int{role(t, r, "orchestrator").PlainReads, role(t, r, "explore").PlainReads, role(t, r, "verify").PlainReads, role(t, r, "worker").PlainReads},
			[4]int{3, 2, 0, 5})
	})
	for _, c := range []struct {
		command string
		read    bool
	}{
		{"cd /work/p && cat f.go", true}, {"cd /work/p && go test ./...", false}, {"cat f.go", true}, {"sed -n '1,5p' f.go", true},
		{"sed -i 's/a/b/' f.go", false}, {"head -5 f.go", true}, {"tail f.go", true}, {"grep -n x f.go", true}, {"rg x", true},
		{"ls", true}, {"find . -name x", true}, {"go test ./...", false}, {"cd /work/p; cat f.go", false}, {"git status && cat f", false},
	} {
		if got := plainShellRead(c.command); got != c.read {
			t.Errorf("plainShellRead(%q) = %v, want %v", c.command, got, c.read)
		}
	}
}

func TestIntegrationsShareCodeGraphAgainstPlainReads(t *testing.T) {
	onBothRuntimes(t, theIntegrations, func(t *testing.T, r *Result) {
		eq(t, "orchestrator share", role(t, r, "orchestrator").CodegraphShare(), 0.25)
		eq(t, "worker share", role(t, r, "worker").CodegraphShare(), 2.0/7)
		eq(t, "explore share", role(t, r, "explore").CodegraphShare(), 0.0)
		if s := role(t, r, "verify").CodegraphShare(); !math.IsNaN(s) {
			t.Errorf("verify share: %v, want none", s)
		}
	})
}

func TestIntegrationsCountTodoCalls(t *testing.T) {
	onBothRuntimes(t, theIntegrations, func(t *testing.T, r *Result) {
		eq(t, "todo calls: orchestrator, explore, verify, worker",
			[4]int{role(t, r, "orchestrator").TodoCalls, role(t, r, "explore").TodoCalls, role(t, r, "verify").TodoCalls, role(t, r, "worker").TodoCalls},
			[4]int{2, 0, 0, 1})
	})
}

// The report states the integrations as statistics: an object of counts per
// role in the JSON, one line per role in the human output, and nothing
// else moves: no finding, the same status, the same closing line, and no
// tool argument, memory text or file name.
func TestIntegrationsAreReportedAsStatisticsNeverAFinding(t *testing.T) {
	onBothRuntimes(t, theIntegrations, func(t *testing.T, r *Result) {
		got := js.StringifyValueCompact(js.Prop(r.Report(), "integrations"))
		entry := func(launches, read, write, cg, plain int, share string, todo int) string {
			return strings.NewReplacer("L", itoa(launches), "R", itoa(read), "W", itoa(write), "C", itoa(cg), "P", itoa(plain), "S", share, "T", itoa(todo)).
				Replace(`{"launches":L,"launches_with_memory_read":R,"launches_with_memory_write":W,"codegraph_calls":C,"plain_reads":P,"codegraph_share":S,"todo_calls":T}`)
		}
		want := `{"orchestrator":` + entry(1, 1, 1, 1, 3, "0.25", 2) + `,"explore":` + entry(1, 1, 0, 0, 2, "0", 0) +
			`,"verify":` + entry(1, 0, 0, 0, 0, "null", 0) + `,"worker":` + entry(2, 1, 1, 2, 5, js.NumberString(2.0/7), 1) + `}`
		eq(t, "integrations", got, want)
		out := Human(r, "s.jsonl", false)
		lines := "\n  integrations  orchestrator  launches 1  memory read 1  memory write 1  codegraph 1  plain reads 3  codegraph share 25.0%  todo 2\n" +
			"  integrations  explore  launches 1  memory read 1  memory write 0  codegraph 0  plain reads 2  codegraph share 0.0%  todo 0\n" +
			"  integrations  verify  launches 1  memory read 0  memory write 0  codegraph 0  plain reads 0  codegraph share -  todo 0\n" +
			"  integrations  worker  launches 2  memory read 1  memory write 1  codegraph 2  plain reads 5  codegraph share 28.6%  todo 1\n"
		if !strings.Contains(out, lines) {
			t.Errorf("missing %q in\n%s", lines, out)
		}
		if i, j := strings.Index(out, "\n  dialogue  "), strings.Index(out, "\n  integrations  "); i < 0 || j < i {
			t.Errorf("the integrations lines follow the dialogue lines:\n%s", out)
		}
		eq(t, "findings", len(r.Findings), 0)
		eq(t, "status", r.Status, "clean")
		if !strings.HasSuffix(out, "\nclean") {
			t.Errorf("closing line: %s", out)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "  integrations  ") && strings.Contains(line, "SECRET") {
				t.Errorf("an integrations line holds the session's text: %s", line)
			}
		}
		if strings.Contains(got, "SECRET") {
			t.Errorf("the integrations hold the session's text: %s", got)
		}
	})
}

func itoa(n int) string { return js.NumberString(float64(n)) }

// An audit that could not read the session measures no integrations.
func TestIntegrationsAreNullOnAnAuditError(t *testing.T) {
	r := Audit(fixture(t, "bad-json"))
	eq(t, "status", r.Status, "audit-error")
	if r.Integrations != nil || !r.Report().Has("integrations") || js.Prop(r.Report(), "integrations") != nil {
		t.Errorf("integrations: %+v, %v", r.Integrations, js.Prop(r.Report(), "integrations"))
	}
	if out := Human(r, "s.jsonl", false); strings.Contains(out, "integrations") {
		t.Errorf("the human output of an audit-error:\n%s", out)
	}
}

// jhon audit --all sums each role's counts over the sessions, Jhon's apart.
func TestAggregateSumsTheIntegrations(t *testing.T) {
	first := theIntegrations.claude(t)
	second := integrationScript{main: []used{memSearch(), readFile()}, subs: []launched{{"worker", []used{memSave(), codegraphCall()}}}}.pi(t)
	f := Aggregate([]Audited{{Path: "a.jsonl", Result: first}, {Path: "b.jsonl", Jhon: true, Result: second},
		{Path: "c.jsonl", Jhon: true, Result: &Result{Status: "audit-error", Errors: []AuditError{{Message: "unreadable"}}}}}, nil, time.Time{})
	eq(t, "sessions summed: all, Jhon's", [2]int{f.Integrations.Sessions, f.JhonIntegrations.Sessions}, [2]int{2, 1})
	eq(t, "all", js.StringifyValueCompact(js.Prop(js.Prop(f.Integrations.Value(), "roles"), "worker")),
		`{"launches":3,"launches_with_memory_read":1,"launches_with_memory_write":2,"codegraph_calls":3,"plain_reads":5,"codegraph_share":0.375,"todo_calls":1}`)
	eq(t, "all: orchestrator launches", f.Integrations.Roles[0], RoleUse{Role: "orchestrator", Launches: 2, MemoryReadLaunches: 2, MemoryWriteLaunches: 1, CodegraphCalls: 1, PlainReads: 4, TodoCalls: 2})
	eq(t, "Jhon's", f.JhonIntegrations.Roles, []RoleUse{{Role: "orchestrator", Launches: 1, MemoryReadLaunches: 1, PlainReads: 1}, {Role: "worker", Launches: 1, MemoryWriteLaunches: 1, CodegraphCalls: 1}})
}
