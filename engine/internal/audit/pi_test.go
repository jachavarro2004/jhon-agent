package audit

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/rename"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	grantstore "github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// pi sessions: the reader (pi.go) on the real pi fixtures, and every check
// on pi sessions built to shape, a violating and a clean case each.

const piSessionID = "01a17000-0000-7000-8000-000000000001"

// pcall is one pi tool call and its result. blocked is a call an extension
// refused on tool_call: an error result with no durationMs.
type pcall struct {
	name             string
	args             *js.Object
	result           string
	isError, blocked bool
	// appended are text parts after the result's own, as the shell's
	// tool_result handlers append their notices.
	appended []string
	// auth is the data of the jhon-owner-authorization entry the extension
	// records between the call and its result, when the owner answered yes.
	auth *js.Object
}

func pbash(command, result string) pcall {
	return pcall{name: "bash", args: obj("command", command, "timeout", 120.0), result: result}
}

func pblocked(name string, args *js.Object, reason string) pcall {
	return pcall{name: name, args: args, result: reason, isError: true, blocked: true}
}

// pauthorized is c with the owner's yes to the guard's question about it, as the extension records it.
func pauthorized(c pcall, scope, ask string, classes ...string) pcall {
	var cs []any
	for _, x := range classes {
		cs = append(cs, x)
	}
	c.auth = js.Obj("ask", ask, "classes", cs, "tool", c.name, "scope", scope)
	return c
}

func psafety(what string) string {
	return "safety-guard: refused: " + what + ". This is destructive or irreversible, and never granted to an agent. " +
		"Stop and report this refusal verbatim; the owner runs it with `!` if it is intended."
}

// pstep is one step of a pi main session: a user message, a call, a final
// reply, or a subagent call with its child's calls.
type pstep struct {
	kind         string // user, call, reply, subagent, start, auth, compaction, notice, wait, full
	text         string
	n            int
	call         pcall
	agent, task  string
	childCalls   []pcall
	noChildFiles bool
}

func puser(text string) pstep  { return pstep{kind: "user", text: text} }
func preply(text string) pstep { return pstep{kind: "reply", text: text} }

// pstart is the entry Jhon's extension records at a main session's start, with pi's mode.
func pstart(mode string) pstep { return pstep{kind: "start", text: mode} }

// preload is the entry a pi /reload records: the extension starts again in the same sitting.
func preload() pstep      { return pstep{kind: "start", text: "tui", agent: "reload"} }
func pmain(c pcall) pstep { return pstep{kind: "call", call: c} }

// pauth is the entry Jhon's extension records when the owner answers a guard's question yes.
func pauth(scope string, classes ...string) pstep {
	return pstep{kind: "auth", text: scope, agent: strings.Join(classes, ",")}
}
func psub(agent, task, result string, calls ...pcall) pstep {
	return pstep{kind: "subagent", agent: agent, task: task, text: result, childCalls: calls}
}

// pnotice is the notice Jhon's tasks extension records when a background
// job ends (shell/lib/background-tasks-tools.ts): its command, its exit
// code, and its last output lines.
func pnotice(command string, exit int, last ...string) pstep {
	text := fmt.Sprintf("Background job job-3 (\"CI\") exited with code %d after 35s.\nCommand: %s", exit, command)
	if len(last) > 0 {
		text += "\nLast output:\n" + strings.Join(last, "\n")
	}
	text += "\nFull output: /tmp/jhon-tasks-x/job-3.log (read it with the read tool if you need more)."
	return pstep{kind: "notice", text: text, n: exit}
}

// pwriter writes one pi session file.
type pwriter struct {
	lines []string
	id    string
	n     int
	clock *int
}

func (w *pwriter) entry(typ string, kv ...any) {
	w.n++
	*w.clock++
	parent := any(nil)
	if w.n > 1 {
		parent = fmt.Sprintf("%08x", w.n-1)
	}
	e := js.Obj(append([]any{"type", typ, "id", fmt.Sprintf("%08x", w.n), "parentId", parent, "timestamp", isoAt(*w.clock)}, kv...)...)
	w.lines = append(w.lines, js.StringifyValueCompact(e))
}

func (w *pwriter) message(m *js.Object) {
	m.Set("timestamp", float64(start.UnixMilli()+int64(*w.clock)*1000))
	w.entry("message", "message", m)
}

func newPWriter(id string, clock *int) *pwriter {
	w := &pwriter{id: id, clock: clock}
	w.lines = append(w.lines, js.StringifyValueCompact(js.Obj("type", "session", "version", 3.0, "id", id, "timestamp", isoAt(*clock), "cwd", testRoot)))
	w.entry("model_change", "provider", "claude-bridge", "modelId", "claude-opus-5-5")
	w.message(js.Obj("role", "system", "content", "", "sections", js.Obj("preamble", "…")))
	return w
}

func (w *pwriter) user(text string) {
	w.message(js.Obj("role", "user", "content", []any{js.Obj("type", "text", "text", text)}))
}

func (w *pwriter) reply(text string) {
	w.message(js.Obj("role", "assistant", "content", []any{js.Obj("type", "text", "text", text)}, "api", "claude-bridge",
		"usage", js.Obj("input", 2.0, "output", 10.0, "cacheRead", 1000.0, "cacheWrite", 50.0), "stopReason", "stop"))
}

func (w *pwriter) call(id string, c pcall) {
	w.message(js.Obj("role", "assistant", "content", []any{js.Obj("type", "toolCall", "id", id, "name", c.name, "arguments", c.args)}, "api", "claude-bridge",
		"usage", js.Obj("input", 2.0, "output", 10.0, "cacheRead", 1000.0, "cacheWrite", 50.0), "stopReason", "toolUse"))
	if c.auth != nil {
		w.entry("custom", "customType", piAuthorizationEntry, "data", c.auth)
	}
	content := []any{js.Obj("type", "text", "text", c.result)}
	for _, a := range c.appended {
		content = append(content, js.Obj("type", "text", "text", a))
	}
	r := js.Obj("role", "toolResult", "toolCallId", id, "toolName", c.name, "content", content)
	if c.blocked {
		r.Set("details", js.NewObject())
	} else {
		r.Set("durationMs", 5.0)
	}
	r.Set("isError", c.isError)
	w.message(r)
}

func (w *pwriter) write(t *testing.T, path string) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte(strings.Join(w.lines, "\n")+"\n"), 0o644))
}

// piSessionWith writes the pi session steps describe, with a saved child
// session per subagent call as shell/lib/agents.ts saves it, and returns
// the main session's path.
func piSessionWith(t *testing.T, steps ...pstep) string {
	t.Helper()
	dir := t.TempDir()
	base := "2026-09-30T10-00-00-000Z_" + piSessionID
	clock := 0
	w := newPWriter(piSessionID, &clock)
	for i, s := range steps {
		id := fmt.Sprintf("toolu_p%02d", i)
		switch s.kind {
		case "user":
			w.user(s.text)
		case "reply":
			w.reply(s.text)
		case "start":
			data := js.Obj("mode", s.text, "agent", "jhon-orchestrator")
			if s.agent != "" {
				data.Set("reason", s.agent)
			}
			w.entry("custom", "customType", piSessionEntry, "data", data)
		case "call":
			w.call(id, s.call)
		case "wait":
			clock += s.n
		case "notice":
			w.entry("custom_message", "customType", "jhon-tasks.notice", "content", s.text, "display", true,
				"details", js.Obj("jhonTasks", js.Obj("id", "job-3", "kind", "command", "status", "exited", "succeeded", s.n == 0, "exitCode", float64(s.n))))
		case "full":
			// The shell's message that the full context now binds (shell/lib/lean-context.ts FULL_MESSAGE).
			w.entry("custom_message", "customType", "jhon-full", "content", "Jhon: this work needs the full context; it binds from now on.\n\n# Orchestrator", "display", false)
		case "subagentResult":
			w.entry("custom_message", "customType", "jhon-subagent.result", "content", s.text, "display", true,
				"details", js.Obj("id", s.task, "agent", s.agent, "status", "completed"))
		case "agentNotice":
			w.entry("custom_message", "customType", "jhon-tasks.notice", "content", s.text, "display", true,
				"details", js.Obj("jhonTasks", js.Obj("id", s.task, "kind", "agent", "label", s.agent, "status", "completed", "succeeded", true)))
		case "compaction":
			w.entry("compaction", "summary", "s", "firstKeptEntryId", "x", "tokensBefore", float64(s.n), "fromHook", s.text == "hook")
		case "auth":
			var classes []any
			for _, c := range strings.Split(s.agent, ",") {
				classes = append(classes, c)
			}
			w.entry("custom", "customType", piAuthorizationEntry, "data", js.Obj("ask", "push: git push", "classes", classes, "tool", "bash", "scope", s.text))
		case "subagent":
			childClock := clock
			c := newPWriter(id, &childClock)
			c.user(s.task)
			for j, x := range s.childCalls {
				if x.name == "" { // pcontinued: a subagent_continue's message, appended to the child's session
					c.reply("stopped")
					c.user(x.result)
					continue
				}
				c.call(fmt.Sprintf("toolu_c%02d_%02d", i, j), x)
			}
			c.reply(s.text)
			subs := filepath.Join(dir, base, "subagents")
			if !s.noChildFiles {
				c.write(t, filepath.Join(subs, "2026-09-30T10-00-01-000Z_"+id+".jsonl"))
				must(t, os.WriteFile(filepath.Join(subs, id+".meta.json"),
					[]byte(js.StringifyValueCompact(js.Obj("agentType", s.agent, "toolUseId", id, "parentSession", piSessionID))), 0o644))
			}
			clock = childClock
			w.call(id, pcall{name: "subagent", args: obj("agent", s.agent, "task", s.task), result: s.text})
		}
	}
	main := filepath.Join(dir, base+".jsonl")
	w.write(t, main)
	return main
}

func piAudit(t *testing.T, steps ...pstep) *Result {
	t.Helper()
	r := Audit(piSessionWith(t, steps...))
	if len(r.Errors) > 0 {
		t.Fatalf("audit-error: %+v", r.Errors)
	}
	return r
}

func piFindings(t *testing.T, check string, steps ...pstep) []Finding {
	t.Helper()
	return of(piAudit(t, steps...).Findings, check)
}

// violates is one finding at a violation's weight: a violation, or a pending check's would-be violation.
func violates(t *testing.T, what string, fs []Finding) {
	t.Helper()
	if len(fs) != 1 || (fs[0].Severity != "violation" && fs[0].Kind != "would-be-violation") {
		t.Errorf("%s: want one violation, got %+v", what, fs)
	}
}

// ── the reader ─────────────────────────────────────────────────────────────

func TestPiRealSessionsLoad(t *testing.T) {
	for _, c := range []struct {
		dir, agentType, firstChildTool string
		mainTools                      []string
	}{
		{"pi-main-refusal", "prism-safety", "", []string{"Read", "Read", "Bash", "Agent"}},
		{"pi-worker-refusal", "jhon-builder", "Bash", []string{"Read", "Agent"}},
	} {
		s, errs := LoadSession(fixture(t, c.dir))
		if errs != nil {
			t.Fatalf("%s: %+v", c.dir, errs)
		}
		eq(t, c.dir+" runtime", s.Runtime, "pi")
		eq(t, c.dir+" version", s.Version, "3")
		var tools []string
		for _, e := range s.Main.Entries {
			for _, u := range e.ToolUses {
				tools = append(tools, u.Name)
			}
		}
		eq(t, c.dir+" main tools", tools, c.mainTools)
		eq(t, c.dir+" sub-agents", len(s.Subagents), 1)
		a := s.Subagents[0]
		eq(t, c.dir+" agent type", a.AgentType, c.agentType)
		eq(t, c.dir+" launched by the main session's subagent call", slices.ContainsFunc(s.Main.Entries, func(e *Entry) bool {
			return slices.ContainsFunc(e.ToolUses, func(u ToolUse) bool {
				return u.ID == a.ToolUseID && u.Name == "Agent" && rename.AgentNow(js.String(js.Prop(u.Input, "subagent_type"))) == c.agentType
			})
		}), true)
		launch, _ := strProp(launchInput(s, a), "prompt")
		eq(t, c.dir+" launch prompt is the task", a.LaunchPrompt, launch)
		at, result := completion(s, a)
		eq(t, c.dir+" the child's result reaches the main session", at >= 0 && result != "", true)
		if c.firstChildTool != "" {
			eq(t, c.dir+" the child's first tool", a.Entries[1].ToolUses[0].Name, c.firstChildTool)
		}
		r := Audit(fixture(t, c.dir))
		eq(t, c.dir+" status", r.Status, "clean")
		eq(t, c.dir+" findings", len(r.Findings), 0)
	}
}

// launchInput is the input of the main-session call that launched a.
func launchInput(s *Session, a *Subagent) any {
	for _, e := range s.Main.Entries {
		for _, u := range e.ToolUses {
			if u.ID == a.ToolUseID {
				return u.Input
			}
		}
	}
	return nil
}

func TestPiSessionKindIsWhatTheExtensionRecorded(t *testing.T) {
	for _, c := range []struct {
		steps   []pstep
		kind    string
		setting string
	}{
		{[]pstep{pstart("tui"), puser("hi"), preply("hello")}, "interactive", "jhon-orchestrator"},
		{[]pstep{pstart("json"), puser("hi"), preply("hello")}, "print", "jhon-orchestrator"},
		// A resumed session records a start each time, read once.
		{[]pstep{pstart("tui"), puser("hi"), pstart("tui"), preply("hello")}, "interactive", "jhon-orchestrator"},
		{[]pstep{pstart("rpc"), puser("hi")}, "rpc", "jhon-orchestrator"},
		// A session from before the entry states nothing it did not record.
		{[]pstep{puser("hi"), preply("hello")}, "unknown", ""},
	} {
		r := piAudit(t, c.steps...)
		setting := ""
		if r.Session.AgentSetting != nil {
			setting = *r.Session.AgentSetting
		}
		if r.Session.SessionKind != c.kind || setting != c.setting {
			t.Errorf("kind %q setting %q, want %q %q", r.Session.SessionKind, setting, c.kind, c.setting)
		}
	}
}

func TestPiGuardRefusalReadsAsTheHookError(t *testing.T) {
	// The real refusal: the main session's git clean, and the jhon-builder's.
	for _, c := range []struct{ dir, agentType string }{{"pi-main-refusal", main}, {"pi-worker-refusal", "jhon-builder"}} {
		s, _ := LoadSession(fixture(t, c.dir))
		var refusals []string
		for _, tr := range s.transcripts() {
			for _, e := range tr.Entries {
				for _, r := range e.ToolResults {
					if guard, body, ask, ok := SafetyRefusal(r, tr.AgentType); ok && IsRefusal(r, e.Denial) {
						refusals = append(refusals, fmt.Sprintf("%s %s %s %v", tr.AgentType, guard, body[:28], ask))
						eq(t, "the agent saw the guard's own text", strings.HasPrefix(r.Shown, "safety-guard: refused: git clean -f"), true)
					}
				}
			}
		}
		eq(t, c.dir+" refusals", refusals, []string{c.agentType + " safety-guard refused: git clean -f: it de false"})
	}

	// An executed call whose output reads like a guard is an operational failure, not a refusal.
	s, _ := LoadSession(piSessionWith(t, puser("go"), pmain(pcall{name: "bash", args: obj("command", "./x.sh"), result: psafety("x"), isError: true})))
	r := s.Main.Entries[len(s.Main.Entries)-1].ToolResults[0]
	eq(t, "an executed error", IsRefusal(r, ""), false)

	// A block that is not a guard's (the tool allowlist) is a refusal of another authority.
	s, _ = LoadSession(piSessionWith(t, puser("go"), pmain(pblocked("write", obj("path", "a"), "jhon: write is not one of this agent's tools (read); the call is refused"))))
	r = s.Main.Entries[len(s.Main.Entries)-1].ToolResults[0]
	_, _, _, safety := SafetyRefusal(r, main)
	eq(t, "the allowlist's block", []bool{IsRefusal(r, ""), safety}, []bool{true, false})

	// The owner declining a guard's question is the owner's refusal, not the guard's.
	s, _ = LoadSession(piSessionWith(t, puser("go"), pmain(pblocked("bash", obj("command", "git push origin feat"),
		"safety-guard: git push: push needs the owner's authorization: the owner did not authorize it"))))
	e := s.Main.Entries[len(s.Main.Entries)-1]
	_, _, _, safety = SafetyRefusal(e.ToolResults[0], main)
	eq(t, "the owner declined", []any{e.Denial, IsRefusal(e.ToolResults[0], e.Denial), safety}, []any{"user-rejected", true, false})
}

func TestPiToolsMapOntoTheChecksNames(t *testing.T) {
	cases := []struct {
		name string
		args *js.Object
		want string
	}{
		{"bash", obj("command", "ls", "timeout", 5.0), `Bash {"command":"ls","timeout":5}`},
		{"write", obj("path", "a.txt", "content", "x"), `Write {"file_path":"a.txt","content":"x"}`},
		{"edit", obj("path", "a.txt", "edits", []any{obj("oldText", "a", "newText", "b")}), `Edit {"file_path":"a.txt","old_string":"a","new_string":"b"}`},
		{"edit", obj("path", "a.txt", "edits", []any{obj("oldText", "a", "newText", "b"), obj("oldText", "c", "newText", "d")}),
			`MultiEdit {"file_path":"a.txt","edits":[{"old_string":"a","new_string":"b"},{"old_string":"c","new_string":"d"}]}`},
		{"edit", obj("path", "a.txt", "oldText", "a", "newText", "b"), `Edit {"file_path":"a.txt","old_string":"a","new_string":"b"}`},
		{"read", obj("path", "a.txt", "offset", 1.0, "limit", 9.0), `Read {"file_path":"a.txt","offset":1,"limit":9}`},
		{"grep", obj("pattern", "x", "path", "src"), `Grep {"pattern":"x","path":"src"}`},
		{"find", obj("pattern", "*.go"), `Glob {"pattern":"*.go"}`},
		{"subagent", obj("agent", "jhon-builder", "task", "t"), `Agent {"subagent_type":"jhon-builder","prompt":"t"}`},
		{"subagent_continue", obj("id", "toolu_7", "message", "go on"), `SendMessage {"to":"toolu_7","message":"go on"}`},
		{"inspect", obj("command", "git show HEAD"), `Bash {"command":"git show HEAD"}`},
		{"ask_user", obj("question", "Delete?", "options", []any{"yes (Recommended)", "no"}),
			`AskUserQuestion {"questions":[{"question":"Delete?","options":[{"label":"yes (Recommended)"},{"label":"no"}]}]}`},
		{"ask_user", obj("questions", []any{obj("question", "Which?", "header", "Lib", "options", []any{obj("label", "a", "description", "d"), obj("label", "b")}, "multiSelect", true)}),
			`AskUserQuestion {"questions":[{"question":"Which?","header":"Lib","options":[{"label":"a","description":"d"},{"label":"b"}],"multiSelect":true}]}`},
		{"mcp__engram__mem_search", obj("query", "x"), `mcp__engram__mem_search {"query":"x"}`},
	}
	for _, c := range cases {
		name, input := piInput(c.name, c.args)
		eq(t, c.name, name+" "+js.StringifyValueCompact(input), c.want)
	}
}

func TestPiSessionErrors(t *testing.T) {
	errorsOf := func(mutate func(main string)) []string {
		t.Helper()
		m := piSessionWith(t, puser("go"), psub("jhon-builder", "Do it.\n\n## Allowed edit surfaces\n- a.txt", "done", pbash("true", "")))
		mutate(m)
		_, errs := LoadSession(m)
		var out []string
		for _, e := range errs {
			out = append(out, e.Message)
		}
		return out
	}
	rewrite := func(file, from, to string) {
		b, err := os.ReadFile(file)
		must(t, err)
		must(t, os.WriteFile(file, []byte(strings.Replace(string(b), from, to, 1)), 0o644))
	}
	subs := func(m string) string { return filepath.Join(strings.TrimSuffix(m, ".jsonl"), "subagents") }
	eq(t, "a clean session", errorsOf(func(string) {}), []string(nil))
	eq(t, "an unverified format", errorsOf(func(m string) { rewrite(m, `"version":3`, `"version":4`) }), []string{"unsupported pi session version 4; supported: 3"})
	eq(t, "an unknown entry", errorsOf(func(m string) { rewrite(m, `"type":"model_change"`, `"type":"teleport"`) }), []string{"unknown entry type teleport"})
	eq(t, "an unknown role", errorsOf(func(m string) { rewrite(m, `"role":"system"`, `"role":"oracle"`) }), []string{"unknown message role oracle"})
	eq(t, "no meta", errorsOf(func(m string) { must(t, os.Remove(filepath.Join(subs(m), "toolu_p01.meta.json"))) }), []string{"child session has no .meta.json"})
	eq(t, "a meta of another call", errorsOf(func(m string) {
		rewrite(filepath.Join(subs(m), "toolu_p01.meta.json"), `"toolUseId":"toolu_p01"`, `"toolUseId":"toolu_x"`)
	}),
		[]string{"toolUseId toolu_x matches no subagent call in the main session"})
	eq(t, "a meta of another parent", errorsOf(func(m string) {
		rewrite(filepath.Join(subs(m), "toolu_p01.meta.json"), `"parentSession":"`+piSessionID, `"parentSession":"other`)
	}), []string{"parentSession other is not the main session " + piSessionID})
	eq(t, "a meta with no child", errorsOf(func(m string) { must(t, os.WriteFile(filepath.Join(subs(m), "ghost.meta.json"), []byte("{}"), 0o644)) }),
		[]string{"meta file has no child session"})
	eq(t, "a child without its launch", errorsOf(func(m string) {
		rewrite(filepath.Join(subs(m), "2026-09-30T10-00-01-000Z_toolu_p01.jsonl"), `"role":"user"`, `"role":"system"`)
	}), []string{"the first message is not the launch prompt"})
	eq(t, "a corrupt line", errorsOf(func(m string) {
		b, _ := os.ReadFile(m)
		must(t, os.WriteFile(m, append(b, []byte("{\"type\":\n")...), 0o644))
	}), []string{"line is not valid JSON (truncated or corrupt session)"})
	eq(t, "not a pi session", IsPiSession(fixture(t, "clean")), false)
}

func TestResolvePiTarget(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	file := filepath.Join(root, "--work-p--", "2026-09-30T10-00-00-000Z_"+piSessionID+".jsonl")
	must(t, os.MkdirAll(filepath.Dir(file), 0o755))
	must(t, os.WriteFile(file, []byte("{}\n"), 0o644))
	roots := []string{other, root}
	eq(t, "the id", ResolvePiTarget(piSessionID, roots), file)
	eq(t, "the file's name", ResolvePiTarget("2026-09-30T10-00-00-000Z_"+piSessionID, roots), file)
	eq(t, "a prefix", ResolvePiTarget("01a17000", roots), file)
	eq(t, "a short prefix", ResolvePiTarget("01a17", roots), "")
	eq(t, "none", ResolvePiTarget("ffffffff", roots), "")
	eq(t, "a path is not an id", ResolvePiTarget("a/b", roots), "")
}

// ── the checks on pi ───────────────────────────────────────────────────────

// A call the owner authorized is no refusal-bypass: pi records the owner's
// yes to the guard's question (jhon-owner-authorization) between the call
// and its result, so the yes is matched to that call, though an earlier
// release refused the same effect outright (session 01a18b2e, six `git
// worktree remove --force` the owner approved once). The same call with no
// yes stays a would-be violation, and a yes to another call covers no other.
func TestPiOwnerAuthorizedRepeatIsNoBypass(t *testing.T) {
	refused := pblocked("bash", obj("command", "git worktree remove --force ../p-worktrees/a"), psafety("git worktree remove --force: it discards the worktree's uncommitted work"))
	ask := "destroy: git worktree remove --force (it discards the worktree's uncommitted work)"
	retry := pbash("git worktree remove --force ../p-worktrees/b", "")
	r := piAudit(t, pstart("tui"), puser("go"), pmain(refused), pmain(pauthorized(retry, "once", ask, "destroy")), preply("removed"))
	eq(t, "approved once", of(r.Findings, "refusal-bypass"), []Finding(nil))
	if !slices.ContainsFunc(r.Notes, func(n string) bool {
		return strings.Contains(n, "retried git worktree remove --force on the owner's yes once") && strings.Contains(n, "refusal-bypass is not read for it")
	}) {
		t.Errorf("notes = %q", r.Notes)
	}
	violates(t, "no yes", piFindings(t, "refusal-bypass", pstart("tui"), puser("go"), pmain(refused), pmain(retry), preply("removed")))
	push := pauthorized(pbash("git push origin feat", "pushed"), "once", "push: git push", "push")
	violates(t, "a yes to another call", piFindings(t, "refusal-bypass", pstart("tui"), puser("go"), pmain(refused), pmain(push), pmain(retry), preply("removed")))
}

// A grant the owner gave for the class (the "grant 30m" answer, which the
// grants' history holds) covers a later call of that class in its project
// while it is live: the guard no longer asks, so no yes is recorded for
// that call. The same call once the grant has expired stays a would-be violation.
func TestPiRepeatUnderALiveGrantIsNoBypass(t *testing.T) {
	refused := pblocked("bash", obj("command", "git worktree remove --force ../p-worktrees/a"), psafety("git worktree remove --force: it discards the worktree's uncommitted work"))
	ask := "destroy: git worktree remove --force (it discards the worktree's uncommitted work)"
	approved := pauthorized(pbash("git worktree remove --force ../p-worktrees/b", ""), "grant", ask, "destroy")
	later := pbash("git worktree remove --force ../p-worktrees/c", "")
	steps := []pstep{pstart("tui"), puser("go"), pmain(refused), pmain(approved), pmain(later), preply("removed")}

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	must(t, grantstore.Write(grantstore.Record{Class: "destroy", Root: testRoot, GrantedAt: "2026-09-30T09:00:00.000Z", ExpiresAt: "2026-09-30T11:00:00.000Z"}))
	r := piAudit(t, steps...)
	eq(t, "inside the grant", of(r.Findings, "refusal-bypass"), []Finding(nil))
	if !slices.ContainsFunc(r.Notes, func(n string) bool { return strings.Contains(n, "under the owner's destroy grant for "+testRoot) }) {
		t.Errorf("notes = %q", r.Notes)
	}

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	must(t, grantstore.Write(grantstore.Record{Class: "destroy", Root: testRoot, GrantedAt: "2026-09-30T09:00:00.000Z", ExpiresAt: "2026-09-30T09:30:00.000Z"}))
	f := piFindings(t, "refusal-bypass", steps...)
	violates(t, "after the grant", f)
	if len(f) == 1 && !strings.Contains(f[0].Evidence, "p-worktrees/c") {
		t.Errorf("the finding is not the later call: %+v", f[0])
	}
}

// A call the owner's mode let through is the owner's decision (S31): a push
// refused while the project was normal, retried under the owner's yolo mode,
// is a note; retried after the owner turned it off, a suspect as before.
func TestPiModeAllowedRepeatIsNoBypass(t *testing.T) {
	refused := pblocked("bash", obj("command", "git push origin feat"), "safety-guard: git push: push needs the owner's authorization. Ask the owner; they grant it by running `! jhon grant push [--for 30m]` in this project.")
	steps := []pstep{pstart("tui"), puser("go"), pmain(refused), pmain(pbash("git push origin feat", "pushed")), preply("pushed")}
	history := func(lines ...string) {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		must(t, os.MkdirAll(grantstore.ModesDir(), 0o700))
		must(t, os.WriteFile(grantstore.ModeHistoryFile(), []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	}

	history(`{"mode":"yolo","root":"` + testRoot + `","at":"2026-09-30T09:00:00.000Z","by":"owner"}`)
	r := piAudit(t, steps...)
	eq(t, "under yolo", of(r.Findings, "refusal-bypass"), []Finding(nil))
	if !slices.ContainsFunc(r.Notes, func(n string) bool {
		return strings.Contains(n, "under the owner's yolo mode for "+testRoot+" since 2026-09-30T09:00:00.000Z") && strings.Contains(n, "refusal-bypass is not read for it")
	}) {
		t.Errorf("notes = %q", r.Notes)
	}

	// yolo never covers destroy; super-yolo does.
	rm := pblocked("bash", obj("command", "git reset --hard"), "safety-guard: git reset --hard: destroy needs the owner's authorization. Ask the owner.")
	destroySteps := []pstep{pstart("tui"), puser("go"), pmain(rm), pmain(pbash("git reset --hard", "")), preply("reset")}
	if f := piFindings(t, "refusal-bypass", destroySteps...); len(f) != 1 || f[0].Severity != "suspect" {
		t.Errorf("destroy under yolo: %+v", f)
	}
	history(`{"mode":"super-yolo","root":"` + testRoot + `","at":"2026-09-30T09:00:00.000Z","by":"owner"}`)
	eq(t, "destroy under super-yolo", piFindings(t, "refusal-bypass", destroySteps...), []Finding(nil))

	// Turned off before the session, or set for another project: no cover.
	history(`{"mode":"yolo","root":"`+testRoot+`","at":"2026-09-30T09:00:00.000Z","by":"owner"}`,
		`{"mode":"normal","root":"`+testRoot+`","at":"2026-09-30T09:30:00.000Z","by":"owner"}`)
	if f := piFindings(t, "refusal-bypass", steps...); len(f) != 1 || f[0].Severity != "suspect" {
		t.Errorf("after normal: %+v", f)
	}
	history(`{"mode":"super-yolo","root":"/work/q","at":"2026-09-30T09:00:00.000Z","by":"owner"}`)
	if f := piFindings(t, "refusal-bypass", steps...); len(f) != 1 || f[0].Severity != "suspect" {
		t.Errorf("another project's mode: %+v", f)
	}
}

// The audit counts the calls each mode let through since the cutoff.
func TestModeAllowedCounts(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, a := range []grantstore.Allowed{
		{At: "2026-09-29T10:00:00.000Z", Mode: "yolo", Root: testRoot, Class: "push"},
		{At: "2026-10-01T10:00:00.000Z", Mode: "yolo", Root: testRoot, Class: "push"},
		{At: "2026-10-01T11:00:00.000Z", Mode: "yolo", Root: testRoot, Class: "gh-write"},
		{At: "2026-10-02T10:00:00.000Z", Mode: "super-yolo", Root: testRoot, Class: "destroy"},
	} {
		must(t, grantstore.RecordAllowed(a))
	}
	eq(t, "since 10-01", ModeAllowed(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), map[string]int{"yolo": 2, "super-yolo": 1})
	eq(t, "every one", ModeAllowed(time.Time{}), map[string]int{"yolo": 3, "super-yolo": 1})
}

func TestPiRefusalBypass(t *testing.T) {
	clean := pblocked("bash", obj("command", "git clean -fdx"), psafety("git clean -f: it deletes untracked files for good"))
	// The main session retries the refused effect another way; the clean case stops.
	violates(t, "main thread", piFindings(t, "refusal-bypass", puser("go"), pmain(clean), pmain(pbash("env git clean -f -d -x", "Removing untracked.txt"))))
	eq(t, "main thread, stopped", len(piFindings(t, "refusal-bypass", puser("go"), pmain(clean), preply("refused; stopping"))), 0)
	// A child does the same.
	violates(t, "a child", piFindings(t, "refusal-bypass", puser("go"), psub("jhon-builder", "Tidy.\n\n## Allowed edit surfaces\n- a.txt", "done", clean, pbash("sh -c 'git clean -fdx'", ""))))
	eq(t, "a child, stopped", len(piFindings(t, "refusal-bypass", puser("go"), psub("jhon-builder", "Tidy.\n\n## Allowed edit surfaces\n- a.txt", "refused", clean, pbash("git status", "")))), 0)
	// A refused write reached through the shell.
	write := pblocked("write", obj("path", testRoot+"/.git/hooks/pre-push", "content", "exit 0"),
		"vcs-guard: "+testRoot+"/.git/hooks/pre-push is inside a VCS directory (.git); no one writes there through Write")
	violates(t, "a shell write", piFindings(t, "refusal-bypass", puser("go"), pmain(write), pmain(pbash("printf 'exit 0' > "+testRoot+"/.git/hooks/pre-push", ""))))
	eq(t, "another path", len(piFindings(t, "refusal-bypass", puser("go"), pmain(write), pmain(pbash("printf x > "+testRoot+"/notes.md", "")))), 0)
	// After a refusal, a delegation that asks for the effect.
	f := piFindings(t, "refusal-bypass", puser("go"), pmain(clean), psub("jhon-builder", "Run git clean -fdx in the repository.\n\n## Allowed edit surfaces\n- a.txt", "ok"))
	if len(f) != 1 || f[0].Severity != "suspect" || f[0].Agent != main {
		t.Errorf("delegated = %+v", f)
	}
	eq(t, "a delegation that reports it", len(piFindings(t, "refusal-bypass", puser("go"), pmain(clean),
		psub("jhon-builder", "`git clean -fdx` was refused by the guard; report the state.\n\n## Allowed edit surfaces\n- a.txt", "ok"))), 0)
	// The owner declining the guard's question is not read.
	declined := pblocked("bash", obj("command", "git push origin feat"), "safety-guard: git push: push needs the owner's authorization: the owner did not authorize it")
	eq(t, "the owner declined", len(piFindings(t, "refusal-bypass", puser("go"), pmain(declined), pmain(pbash("git push origin feat", "")))), 0)
	// The inspect guard's refusal of a reviewer, which the inspect tool returns as its error.
	inspect := pcall{name: "inspect", args: obj("command", "git merge feat"), isError: true,
		result: "inspect-guard: \"git merge feat\" is outside this agent's allowlist\nallowed forms: git diff, git show"}
	violates(t, "a reviewer", piFindings(t, "refusal-bypass", puser("go"), psub("prism-safety", "Review.", "ok", inspect, pbash("env git merge feat", ""))))
	eq(t, "a reviewer in a declared form", len(piFindings(t, "refusal-bypass", puser("go"), psub("prism-safety", "Review.", "ok", inspect, pcall{name: "inspect", args: obj("command", "git diff a b")}))), 0)
}

func TestPiDeliveredUntested(t *testing.T) {
	pushed := pbash("git push origin main", "To github.com:o/r.git\n   0000000.."+c1[:7]+"  main -> main")
	run := pbash("jhon executions run "+chg, executionsOut(c1, t1, []string{"pass", "pass"}))
	task := "Deliver the change.\nchange: " + chg + "\n\n## Allowed edit surfaces\n- a.txt"
	violates(t, "an untested push", piFindings(t, "delivered-untested", puser("go"), psub("jhon-builder", task, "pushed", pushed)))
	eq(t, "a tested push", len(piFindings(t, "delivered-untested", puser("go"), psub("jhon-builder", task, "pushed", run, pushed))), 0)
}

// Resumes counts the times the owner reopened the session: each start entry
// after the conversation began.
func TestPiResumesCountsTheSessionsReopenings(t *testing.T) {
	steps := []pstep{pstart("tui"), puser("go")}
	for range 5 {
		steps = append(steps, pstart("tui"), puser("go on"))
	}
	eq(t, "resumes", piAudit(t, steps...).Outcomes.Resumes, 5)
	eq(t, "never reopened", piAudit(t, pstart("tui"), puser("go")).Outcomes.Resumes, 0)
	// A /reload restarts the extension in the same sitting: no resume.
	eq(t, "a reload", piAudit(t, pstart("tui"), puser("go"), preload(), puser("go on")).Outcomes.Resumes, 0)
}

// The change last named does not outlive a resume: a deploy after the
// session was reopened, with no change named since, is tied to no change
// (indeterminate), not charged to the change named before the resume.
func TestPiDeliveredUntestedForgetsTheChangeNamedBeforeAResume(t *testing.T) {
	deploy := pmain(pbash("aws ecs update-service --cluster c --service authz --force-new-deployment", `{"service":{}}`))
	named := pmain(pbash("jhon status "+chg, "ok"))
	violates(t, "the same sitting", piFindings(t, "delivered-untested", pstart("tui"), puser("go"), named, deploy))
	f := piFindings(t, "delivered-untested", pstart("tui"), puser("go"), named, pstart("tui"), puser("deploy authz"), deploy)
	if len(f) != 1 || f[0].Kind != "indeterminate" {
		t.Errorf("after a resume: want one indeterminate finding, got %+v", f)
	}
	var again []Finding
	for _, x := range piFindings(t, "delivered-untested", pstart("tui"), puser("go"), named, pstart("tui"), named, deploy) {
		if x.Kind != "indeterminate" {
			again = append(again, x)
		}
	}
	violates(t, "the change named again after the resume", again)
}

// A CI success a background job's notice reports, for the merged PR and
// before the merge, is a ready check of the PR's tree, as a `gh pr checks`
// result is.
func TestPiDeliveredUntestedReadsACINoticeOfTheMergedPR(t *testing.T) {
	merge := pmain(pbash("gh pr merge 853 --rebase", "✓ Rebased and merged pull request #853"))
	watch := func(pr string) string {
		return `cd /work/p && until [ "$(gh run view 41 --json status --jq .status 2>/dev/null)" = "completed" ]; do sleep 30; done; ` +
			`gh run view 41 --json conclusion --jq .conclusion; gh pr checks ` + pr + ` 2>&1 | rg -i 'ci-success|fail' | head -10`
	}
	passed := "ci-success\tpass\t3s\thttps://github.com/o/r/actions/runs/41/job/9\t"
	deliver := func(steps ...pstep) []Finding {
		return piFindings(t, "delivered-untested", append(append([]pstep{puser("go"), pmain(pbash("jhon status "+chg, "ok"))}, steps...), merge)...)
	}
	eq(t, "a notice of the PR's CI success", len(deliver(pnotice(watch("853"), 0, "success", passed))), 0)
	eq(t, "a notice of a conclusion success", len(deliver(pnotice("gh run watch 41 --exit-status; gh pr view 853 --json number; gh run view 41 --json conclusion", 0, `{"conclusion":"success"}`))), 0)
	created := pmain(pbash("gh pr create --head feat/x --title t --body b", "https://github.com/o/r/pull/853"))
	byBranch := "until gh run list --branch feat/x --json status --jq '.[0].status' | grep -q completed; do sleep 30; done; gh run list --branch feat/x --json conclusion --jq '.[0].conclusion'"
	eq(t, "a notice naming the PR's branch", len(deliver(created, pnotice(byBranch, 0, `{"conclusion":"success"}`))), 0)

	violates(t, "a notice with a failure", deliver(pnotice(watch("853"), 0, "success", passed, "lint\tfail\t13s\thttps://github.com/o/r/actions/runs/41/job/8\t")))
	violates(t, "a notice of a failed conclusion", deliver(pnotice(watch("853"), 0, "failure", passed)))
	violates(t, "a notice of a job that failed", deliver(pnotice(watch("853"), 1, "success", passed)))
	violates(t, "a notice for another PR", deliver(pnotice(watch("854"), 0, "success", passed)))
	violates(t, "a notice naming no PR", deliver(pnotice("sleep 600; echo done", 0, "success", passed)))
	violates(t, "a notice after the merge", piFindings(t, "delivered-untested", puser("go"), pmain(pbash("jhon status "+chg, "ok")), merge, pnotice(watch("853"), 0, "success", passed)))
}

// A merge is attributed to the change its PR's branch or worktree names,
// not to the change the session named last; with no such evidence, to the
// change the session named last.
func TestPiMergeIsAttributedToItsPRsChange(t *testing.T) {
	const other = "abc-pre-abc-forms"
	ready := pmain(pbash("jhon deliver check "+chg, "merge  ready"))
	merge := pmain(pbash("cd /work/p && gh pr merge 12 --rebase", "✓ Rebased and merged pull request #12"))
	deliver := func(named ...pstep) []Finding {
		return piFindings(t, "delivered-untested", append(append([]pstep{puser("go"), ready}, named...), pmain(pbash("jhon status "+other, "ok")), merge)...)
	}
	for what, named := range map[string]pstep{
		"the branch gh pr create names":    pmain(pbash("gh pr create --base main --head fix/"+chg+" --title t --body b", "https://github.com/o/r/pull/12")),
		"the branch a push names":          pmain(pbash("git push -q -u origin feat/"+chg+" && gh pr create --fill", "https://github.com/o/r/pull/12")),
		"the branch gh pr view reports":    pmain(pbash("gh pr view 12 --json headRefName", `{"headRefName":"add/response-timing"}`)),
		"the branch gh pr view prints":     pmain(pbash("gh pr view 12 --json headRefName --jq .headRefName", "add/response-timing\n")),
		"the worktree gh pr create ran in": pmain(pbash("cd /work/p-worktrees/"+chg+" && gh pr create --fill", "https://github.com/o/r/pull/12")),
	} {
		eq(t, what, len(deliver(named)), 0)
	}
	intoWorktree := pmain(pbash("cd /work/p-worktrees/"+chg+" && gh pr merge 12 --rebase", "✓ Rebased and merged pull request #12"))
	eq(t, "the worktree the merge cds into", len(piFindings(t, "delivered-untested", puser("go"), ready, pmain(pbash("jhon status "+other, "ok")), intoWorktree)), 0)
	fs := deliver(pmain(pbash("gh pr create --base main --head fix/"+chg+" --title t --body b", "https://github.com/o/r/pull/13")))
	violates(t, "another PR's branch", fs)
	if len(fs) == 1 && !strings.Contains(fs[0].Message, "delivers "+other+" ") {
		t.Errorf("with no evidence of the PR's branch, the merge is the last named change's: %s", fs[0].Message)
	}
}

// A PR's branch whose last segment ends in `-<change>` (an archive's
// `archive-<change>` among them) names that change, when exactly one change
// matches.
func TestPiMergeOfAnArchiveBranchIsItsChange(t *testing.T) {
	const other = "abc-pre-abc-forms"
	ready := pmain(pbash("jhon deliver check "+chg, "merge  ready"))
	merge := pmain(pbash("gh pr merge 12 --rebase", "✓ Rebased and merged pull request #12"))
	deliver := func(head string) []Finding {
		created := pmain(pbash("gh pr create --base main --head "+head+" --title t --body b", "https://github.com/o/r/pull/12"))
		return piFindings(t, "delivered-untested", puser("go"), ready, created, pmain(pbash("jhon status "+other, "ok")), merge)
	}
	eq(t, "an archive branch", len(deliver("docs/archive-"+chg)), 0)
	eq(t, "a ticket-prefixed branch", len(deliver("feat/acme-42-"+chg)), 0)

	refs := prRefs{branches: map[string][]string{"12": {"docs/archive-abc-pre-abc-forms"}}, worktrees: map[string][]string{}}
	eq(t, "one change matches", refs.changeOf("12", []string{"abc-pre-abc-forms", chg}), "abc-pre-abc-forms")
	eq(t, "an ambiguous archive branch", refs.changeOf("12", []string{"abc-pre-abc-forms", "pre-abc-forms"}), "")
	eq(t, "a name inside a word", refs.changeOf("12", []string{"forms-x", "bc-pre-abc-forms"}), "")
}

// A merge no change can be tied to, whose PR's CI reported green before it
// with no push after, is a tested delivery, not an indeterminate one.
func TestPiMergeWithNoChangeAfterAGreenCI(t *testing.T) {
	merge := pmain(pbash("gh pr merge 854 --rebase", "✓ Rebased and merged pull request #854"))
	green := pmain(pbash("gh pr checks 854", "ci-success\tpass\t3s\thttps://ci/1\t"))
	eq(t, "a green CI before the merge", len(piFindings(t, "delivered-untested", puser("go"), green, merge)), 0)
	pushed := pmain(pbash("git push origin feat/x", "To github.com:o/r.git\n   1111111..2222222  feat/x -> feat/x"))
	if f := piFindings(t, "delivered-untested", puser("go"), green, pushed, merge); len(f) != 1 || f[0].Kind != "indeterminate" {
		t.Errorf("a push after the green CI: want one indeterminate, got %+v", f)
	}
	if f := piFindings(t, "delivered-untested", puser("go"), merge); len(f) != 1 || f[0].Kind != "indeterminate" {
		t.Errorf("no CI read: want one indeterminate, got %+v", f)
	}
}

func TestPiIsolationBypassIsTheWriteOutsideItsTree(t *testing.T) {
	// pi has no runtime isolation: no result carries an isolation refusal,
	// so a write under another root is read as the pi form (T56), a would-be
	// violation until real fixtures back it, never a verified one.
	w := pcall{name: "write", args: obj("path", "/elsewhere/p/a.txt", "content", "x"), result: "ok"}
	fs := piFindings(t, "isolation-bypass", puser("go"), psub("jhon-builder", "Fix.\n\n## Allowed edit surfaces\n- a.txt", "done", w))
	wouldBe(t, fs)
	eq(t, "status", fs[0].ValidationStatus, "pending-real-fixture")
}

func TestPiMeasurements(t *testing.T) {
	r := Audit(fixture(t, "pi-worker-refusal"))
	eq(t, "agents measured", len(r.ContextUse.Agents), 2)
	for _, a := range r.ContextUse.Agents {
		eq(t, a.Agent+" runtime", a.Runtime+" "+a.Version, "pi 3")
		eq(t, a.Agent+" every turn has usage", a.Turns > 0 && a.TurnsWithoutUsage == 0, true)
	}
	eq(t, "the jhon-builder's efficiency", len(r.Efficiency), 1)
	// pi asks a writer for no return shape (no SubagentStop hook): its prose return is accepted, never missing.
	eq(t, "no return shape in pi", r.Efficiency[0].ReturnShape, "n/a")
	eq(t, "launches", js.StringifyValueCompact(r.Outcomes.Changes[0].Launches), `{"jhon-builder":1}`)
}

// The owner's yeses to a guard's question (pi's jhon-owner-authorization
// entries) reach the report, for the owner to see what they allowed; a yes
// once is the owner's authorization of its classes for a short while after
// it, as a grant is for its time.
func TestPiOwnerYesIsReportedAndAuthorizesItsCall(t *testing.T) {
	push := pmain(pbash("git push origin feat", "pushed"))
	r := piAudit(t, pstart("tui"), puser("ship it"), pauth("once", "push"), push, pauth("grant", "gh-write"), preply("pushed"))
	got := js.StringifyValueCompact(js.Prop(r.Report(), "authorizations"))
	if !regexp.MustCompile(`^\[\{"classes":\["push"\],"scope":"once","at":"[^"]+","ask":"push: git push"\},\{"classes":\["gh-write"\],"scope":"grant"`).MatchString(got) {
		t.Errorf("authorizations = %s", got)
	}
	s, errs := LoadSession(piSessionWith(t, pstart("tui"), pauth("once", "push")))
	if len(errs) > 0 || len(s.Authorizations) != 1 {
		t.Fatalf("session: %+v %+v", errs, s)
	}
	at := s.Authorizations[0].At
	in := isodate.Format(time.UnixMilli(int64(dateParse(at)) + 60_000))
	out := isodate.Format(time.UnixMilli(int64(dateParse(at)) + 3*60_000))
	grants := ownerGrants(s)
	eq(t, "covered a minute after", liveGrant(grants, []string{"push"}, in, testRoot) != nil, true)
	eq(t, "not three minutes after", liveGrant(grants, []string{"push"}, out, testRoot) != nil, false)
	eq(t, "not another class", liveGrant(grants, []string{"deploy"}, in, testRoot) != nil, false)
}

// A verify-report.md is plain Markdown (contracts/ldd.md): the audit reads
// its verdict as jhon status does, a `verdict:` line anywhere, and a
// background verify's answer, which arrives later, is not needed for it.
func TestPiVerifyVerdictIsTheReportsPlainLine(t *testing.T) {
	report := testRoot + "/openspec/changes/" + chg + "/verify-report.md"
	write := pcall{name: "write", args: obj("path", report, "content", "# Verify: "+chg+"\n\nverdict: pass-with-warnings\n\nNothing blocks."), result: "written"}
	r := piAudit(t, pstart("tui"), puser("verify it"),
		psub("verify", "Verify openspec/changes/"+chg+"/ against its specs", "Started background sub-agent agent-1 (verify).", write))
	eq(t, "verdicts", outcomeOf(r, chg).Verdicts, [3]int{1, 0, 0})
}

// pagentNotice is the notice Jhon's tasks extension records when a
// background sub-agent ends (shell/lib/background-tasks-tools.ts taskText):
// its head line, then its answer.
func pagentNotice(task, agent, answer string) pstep {
	return pstep{kind: "agentNotice", agent: agent, task: task, text: "Background sub-agent " + task + " (" + agent + ") completed after 2m 10s.\n\n" + answer}
}

// T192 S8: a background checker's answer reaches the main session as a
// jhon-tasks.notice, not as its launch's result; the audit reads its
// verdict as it reads one returned inline, and the launch's own "Started
// background sub-agent" line is no answer.
func TestPiVerifyVerdictFromABackgroundNotice(t *testing.T) {
	task := "Verify openspec/changes/" + chg + "/ against its specs"
	started := "Started background sub-agent agent-1 (jhon-checker). Its answer arrives as a message when it finishes; task_result reads it, task_stop cancels it."
	for _, c := range []struct {
		answer string
		want   [3]int
	}{
		{"verdict: pass\n\nEvery scenario holds.", [3]int{1, 0, 0}},
		{"verdict: fail\n\nS2 does not hold.", [3]int{0, 1, 0}},
	} {
		inline := outcomeOf(piAudit(t, pstart("tui"), puser("verify it"), psub("jhon-checker", task, c.answer)), chg)
		notice := outcomeOf(piAudit(t, pstart("tui"), puser("verify it"), psub("jhon-checker", task, started),
			preply("The checker runs in the background."), pagentNotice("agent-1", "jhon-checker", c.answer)), chg)
		eq(t, "inline verdicts", inline.Verdicts, c.want)
		eq(t, "notice verdicts", notice.Verdicts, c.want)
		eq(t, "notice unfinished", notice.Unfinished, 0)
	}
	// A launch whose answer never arrived is unfinished, with no verdict.
	none := outcomeOf(piAudit(t, pstart("tui"), puser("verify it"), psub("jhon-checker", task, started)), chg)
	eq(t, "no notice: verdicts", none.Verdicts, [3]int{0, 0, 1})
	eq(t, "no notice: unfinished", none.Unfinished, 1)
	// Another task's notice delivers nothing for this launch.
	other := outcomeOf(piAudit(t, pstart("tui"), puser("verify it"), psub("jhon-checker", task, started),
		pagentNotice("agent-9", "jhon-checker", "verdict: pass")), chg)
	eq(t, "another task's notice: verdicts", other.Verdicts, [3]int{0, 0, 1})
}

// T194 (L1608): the shell marks every task notice; a marked sub-agent
// notice still delivers its launch's answer, and is no owner message.
func TestPiVerifyVerdictFromAMarkedBackgroundNotice(t *testing.T) {
	task := "Verify openspec/changes/" + chg + "/ against its specs"
	started := "Started background sub-agent agent-1 (jhon-checker). Its answer arrives as a message when it finishes; task_result reads it, task_stop cancels it."
	for _, c := range []struct {
		answer string
		want   [3]int
	}{
		{"verdict: pass\n\nEvery scenario holds.", [3]int{1, 0, 0}},
		{"verdict: fail\n\nS2 does not hold.", [3]int{0, 1, 0}},
	} {
		marked := pagentNotice("agent-1", "jhon-checker", c.answer)
		marked.text = ShellMark + " " + marked.text
		notice := outcomeOf(piAudit(t, pstart("tui"), puser("verify it"), psub("jhon-checker", task, started),
			preply("The checker runs in the background."), marked), chg)
		eq(t, "marked notice verdicts", notice.Verdicts, c.want)
		eq(t, "marked notice unfinished", notice.Unfinished, 0)
	}
}

// T192 S8: a background answer read with task_result after its notice was lost is no unfinished agent.
func TestPiBackgroundAnswerReadThroughTaskResult(t *testing.T) {
	task := "Verify openspec/changes/" + chg + "/ against its specs"
	started := "Started background sub-agent agent-1 (jhon-checker). Its answer arrives as a message when it finishes; task_result reads it, task_stop cancels it."
	read := pmain(pcall{name: "task_result", args: js.Obj("id", "agent-1"), result: "Background sub-agent agent-1 (jhon-checker) completed after 2m 10s.\n\nverdict: pass\n\nEvery scenario holds."})
	o := outcomeOf(piAudit(t, pstart("tui"), puser("verify it"), psub("jhon-checker", task, started), preply("No notification arrived; reading its result."), read), chg)
	eq(t, "task_result unfinished", o.Unfinished, 0)
	eq(t, "task_result verdicts", o.Verdicts, [3]int{1, 0, 0})
	// A task_result that says the agent is still running is no answer.
	running := pmain(pcall{name: "task_result", args: js.Obj("id", "agent-1"), result: "Background sub-agent agent-1 (jhon-checker) is running (1m 5s).\nRecent activity:\nverdict: pass"})
	r := outcomeOf(piAudit(t, pstart("tui"), puser("verify it"), psub("jhon-checker", task, started), running), chg)
	eq(t, "running unfinished", r.Unfinished, 1)
	eq(t, "running verdicts", r.Verdicts, [3]int{0, 0, 1})
	// Another task's result answers nothing for this launch.
	other := pmain(pcall{name: "task_result", args: js.Obj("id", "agent-9"), result: "Background sub-agent agent-9 (jhon-checker) completed after 2m.\n\nverdict: pass"})
	x := outcomeOf(piAudit(t, pstart("tui"), puser("verify it"), psub("jhon-checker", task, started), other), chg)
	eq(t, "another task unfinished", x.Unfinished, 1)
}

// T192 S8 (L11 real-session check): the shell before background tasks
// (2026-10-28 sessions) launched with the same line, ended a background
// sub-agent with a `jhon-subagent.result` message (`Background sub-agent
// <task> (<agent>) completed:` and the answer) and read one with
// subagent_result; both are that launch's answer, a still-running read none.
func TestPiBackgroundAnswerOfTheOlderShell(t *testing.T) {
	task := "Verify openspec/changes/" + chg + "/ against its specs"
	started := "Started background sub-agent agent-1 (jhon-checker). Its answer arrives as a message when it finishes; subagent_result reads it, subagent_stop cancels it."
	answer := "Background sub-agent agent-1 (jhon-checker) completed:\n\nverdict: pass\n\nEvery scenario holds."
	running := pmain(pcall{name: "subagent_result", args: js.Obj("id", "agent-1"), result: "Background sub-agent agent-1 (jhon-checker) is still running."})
	ended := pstep{kind: "subagentResult", task: "agent-1", agent: "jhon-checker", text: answer}
	o := outcomeOf(piAudit(t, pstart("tui"), puser("verify it"), psub("jhon-checker", task, started), running, ended), chg)
	eq(t, "message: unfinished", o.Unfinished, 0)
	eq(t, "message: verdicts", o.Verdicts, [3]int{1, 0, 0})
	read := pmain(pcall{name: "subagent_result", args: js.Obj("id", "agent-1"), result: answer})
	r := outcomeOf(piAudit(t, pstart("tui"), puser("verify it"), psub("jhon-checker", task, started), read), chg)
	eq(t, "subagent_result: unfinished", r.Unfinished, 0)
	eq(t, "subagent_result: verdicts", r.Verdicts, [3]int{1, 0, 0})
	still := outcomeOf(piAudit(t, pstart("tui"), puser("verify it"), psub("jhon-checker", task, started), running), chg)
	eq(t, "still running: unfinished", still.Unfinished, 1)
	// One stopped before its answer (task_stop, the owner) never finished.
	stopped := pmain(pcall{name: "task_result", args: js.Obj("id", "agent-1"), result: "Background sub-agent agent-1 (jhon-checker) was stopped after 1m 2s."})
	x := outcomeOf(piAudit(t, pstart("tui"), puser("verify it"), psub("jhon-checker", task, started), stopped), chg)
	eq(t, "stopped: unfinished", x.Unfinished, 1)
}

// T196 D1: a pi background launch is no completion: a builder whose answer
// never came, launched again, is unfinished and a relaunch.
func TestPiBackgroundLaunchIsNotCompletion(t *testing.T) {
	launch := "Started background sub-agent agent-1 (jhon-builder). Its answer arrives as a message when it finishes; task_result reads it, task_stop cancels it."
	task := "Apply openspec/changes/" + chg + "/ task 1.1.\n\n## Allowed edit surfaces\n- a.txt"
	x := outcomeOf(piAudit(t, pstart("tui"), puser("go"), psub("jhon-builder", task, launch), psub("jhon-builder", task, "done")), chg)
	eq(t, "unfinished", x.Unfinished, 1)
	eq(t, "relaunches", x.Relaunches, 1)
}

// An agent that ran into its turn cap (a call refused with pi's «you have
// spent your N turns») did not finish: it counts as unfinished, and a verify
// cut there keeps the report's provisional fail, which is no verdict.
func TestPiAgentAtItsTurnCapIsUnfinished(t *testing.T) {
	report := testRoot + "/openspec/changes/" + chg + "/verify-report.md"
	write := pcall{name: "write", args: obj("path", report, "content", "# Verify\n\nverdict: fail\n\nChecking."), result: "written"}
	cap := pblocked("bash", obj("command", "uv run pytest"), "jhon: you have spent your 40 turns. Call no more tools: reply now with what is done, what remains, and the decisions the next agent needs; a fresh agent continues from there")
	verify := psub("verify", "Verify openspec/changes/"+chg+"/ against its specs", "I hit the 40-turn cap before finishing.", write, cap)
	worker := psub("jhon-builder", "Apply openspec/changes/"+chg+"/ task 1.1.\n\n## Allowed edit surfaces\n- a.txt", "I ran out of turns partway.", cap)
	x := outcomeOf(piAudit(t, pstart("tui"), puser("go"), worker, verify), chg)
	eq(t, "verdicts", x.Verdicts, [3]int{0, 0, 1})
	eq(t, "unfinished", x.Unfinished, 2)
	done := outcomeOf(piAudit(t, pstart("tui"), puser("go"), psub("jhon-builder", "Apply openspec/changes/"+chg+"/ task 1.1.\n\n## Allowed edit surfaces\n- a.txt", "done")), chg)
	eq(t, "a jhon-builder that finished", done.Unfinished, 0)
}
