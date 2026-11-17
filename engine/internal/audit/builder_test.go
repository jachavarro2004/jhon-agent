package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The session builder: a synthetic Claude Code
// session written to a temp directory, in main-thread order.

func TestMain(m *testing.M) {
	root, _ := filepath.Abs("../../..")
	os.Setenv("JHON_ROOT", root)
	// The owner's grants' history is never read: a test's grants are its own.
	state, _ := os.MkdirTemp("", "jhon-audit-state-")
	os.Setenv("XDG_STATE_HOME", state)
	code := m.Run()
	os.RemoveAll(state)
	os.Exit(code)
}

const (
	sessionID = "66666666-0000-4000-8000-000000000001"
	testRoot  = "/work/p"
	chg       = "add-response-timing"
)

var start = time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)

func isoAt(s int) string {
	return start.Add(time.Duration(s) * time.Second).Format("2006-01-02T15:04:05.000Z")
}

// tcall is one tool call of a sub-agent: its use, its result, and how the runtime recorded them.
type tcall struct {
	name    string
	input   *js.Object
	result  *string
	isError bool
	denial  string
	usage   []float64 // input, cache creation, cache read
	msgID   string
	text    *string
	split   bool
	wait    int
	persist bool
}

func obj(kv ...any) *js.Object { return js.Obj(kv...) }

func str(s string) *string { return &s }

func bash(command string, result ...any) tcall {
	c := tcall{name: "Bash", input: obj("command", command)}
	if len(result) > 0 {
		c.result = str(result[0].(string))
	}
	if len(result) > 1 {
		c.isError = result[1].(bool)
	}
	return c
}

// step is one main-thread step; kind picks which fields apply.
type step struct {
	kind string // user, queued, reply, delegate, tool, bash, ask, summary, raw, wait, notify
	text string
	// delegate
	id, typ, change, prompt, result, root, phase string
	nullChange, noBlock, queuedResult            bool
	calls                                        []tcall
	at, span                                     *int
	// tool and bash
	tool    string
	input   *js.Object
	isError bool
	denial  string
	wait    int
	usage   []float64
	options []string
	// more are an ask's further questions, put in the same call.
	more []string
	raw  *js.Object
}

func user(text string) step   { return step{kind: "user", text: text} }
func queued(text string) step { return step{kind: "queued", text: text} }

func delegate(id, typ, change string, calls ...tcall) step {
	return step{kind: "delegate", id: id, typ: typ, change: change, nullChange: change == "", calls: calls}
}

// phased is s with its launch block naming phase.
func phased(phase string, s step) step {
	s.phase = phase
	return s
}

// mainBackground is a main-thread Bash call started in the background.
func mainBackground(command string) step {
	return step{kind: "tool", tool: "Bash", input: obj("command", command, "run_in_background", true),
		result: "Command running in background with ID: b1. Output is being written to: /tmp/b1.output. You will be notified when it completes."}
}

// notify is the runtime's notification, as Claude Code writes it, that the
// last tool call's background command ended with exit; output is its output file.
func notify(exit int, output string) step {
	summary := "Background command \"Run the tests\" completed (exit code 0)"
	if exit != 0 {
		summary = fmt.Sprintf("Background command \"Run the tests\" failed with exit code %d", exit)
	}
	return step{kind: "notify", text: summary, result: output}
}

func mainBash(command string, result ...string) step {
	s := step{kind: "bash", text: command, result: "ok"}
	if len(result) > 0 {
		s.result = result[0]
	}
	return s
}

func intp(n int) *int { return &n }

// sessionWith writes the session steps describe and returns its main transcript.
func sessionWith(t *testing.T, steps ...step) string {
	t.Helper()
	dir := t.TempDir()
	subdir := filepath.Join(dir, sessionID, "subagents")
	results := filepath.Join(dir, sessionID, "tool-results")
	must(t, os.MkdirAll(subdir, 0o755))
	must(t, os.MkdirAll(results, 0o755))
	common := func(o *js.Object) *js.Object {
		o.Set("userType", "external")
		if !o.Has("cwd") {
			o.Set("cwd", testRoot)
		}
		o.Set("sessionId", sessionID)
		o.Set("version", "2.1.280")
		return o
	}
	var mainLines []*js.Object
	clock, n := 0, 0
	lastTool := ""
	ts := func() string { s := isoAt(clock); clock++; return s }
	keep := func(id, text string, persist bool) string {
		if !persist {
			return text
		}
		must(t, os.WriteFile(filepath.Join(results, id+".txt"), []byte(text), 0o644))
		return fmt.Sprintf("<persisted-output>\nOutput too large (%.1fKB). Full output saved to: /elsewhere/%s/tool-results/%s.txt\n\nPreview (first 2KB):\n%s\n...\n</persisted-output>",
			float64(js.Len(text))/1024, sessionID, id, text[:min(2048, len(text))])
	}
	line := func(typ string, content any, extra ...any) {
		n++
		o := obj("parentUuid", nil, "isSidechain", false, "type", typ, "message", obj("role", typ, "content", content), "uuid", fmt.Sprintf("m-%d", n), "timestamp", ts())
		for i := 0; i+1 < len(extra); i += 2 {
			o.Set(extra[i].(string), extra[i+1])
		}
		mainLines = append(mainLines, common(o))
	}
	queue := func(prompt, kind string) {
		n++
		at := ts()
		mode := kind
		if kind == "human" {
			mode = "prompt"
		}
		mainLines = append(mainLines, common(obj("parentUuid", nil, "isSidechain", false, "type", "attachment", "uuid", fmt.Sprintf("m-%d", n), "timestamp", at,
			"attachment", obj("type", "queued_command", "prompt", prompt, "commandMode", mode, "origin", obj("kind", kind), "timestamp", at))))
	}
	toolResult := func(id, content string, isError bool) []any {
		r := obj("type", "tool_result", "tool_use_id", id, "content", content)
		if isError {
			r.Set("is_error", true)
		}
		return []any{r}
	}
	toolUse := func(id, name string, input *js.Object) []any {
		return []any{obj("type", "tool_use", "id", id, "name", name, "input", input)}
	}
	for _, s := range steps {
		switch s.kind {
		case "user":
			line("user", s.text, "origin", obj("kind", "human"))
		case "queued":
			queue(s.text, "human")
		case "reply":
			line("assistant", []any{obj("type", "text", "text", s.text)})
		case "raw":
			mainLines = append(mainLines, s.raw)
		case "wait":
			clock += s.wait
		case "summary":
			id := fmt.Sprintf("toolu_summary_%d", n)
			name := s.tool
			if name == "" {
				name = "mcp__plugin_engram_engram__mem_session_summary"
			}
			line("assistant", toolUse(id, name, obj("content", "summary")))
			line("user", toolResult(id, "saved", false))
		case "notify":
			line("user", fmt.Sprintf("<task-notification>\n<task-id>b1</task-id>\n<tool-use-id>%s</tool-use-id>\n<output-file>%s</output-file>\n<status>completed</status>\n<summary>%s</summary>\n</task-notification>",
				lastTool, s.result, s.text), "origin", obj("kind", "task-notification"))
		case "tool":
			id := fmt.Sprintf("toolu_tool_%d", n)
			lastTool = id
			line("assistant", toolUse(id, s.tool, s.input))
			line("user", toolResult(id, or(s.result, "ok"), s.isError))
		case "bash":
			id := fmt.Sprintf("toolu_bash_%d", n)
			lastTool = id
			var extra []any
			if s.usage != nil {
				extra = []any{"message", obj("role", "assistant", "id", fmt.Sprintf("msg_main_%d", n), "usage", usageObj(s.usage), "content", toolUse(id, "Bash", obj("command", s.text)))}
			}
			line("assistant", toolUse(id, "Bash", obj("command", s.text)), extra...)
			clock += s.wait
			var denial []any
			if s.denial != "" {
				denial = []any{"toolDenialKind", s.denial}
			}
			line("user", toolResult(id, s.result, s.isError), denial...)
		case "ask":
			id := fmt.Sprintf("toolu_ask_%d", n)
			labels := s.options
			if labels == nil {
				labels = []string{"Yes", "Not yet"}
			}
			var options []any
			for _, l := range labels {
				options = append(options, obj("label", l, "description", ""))
			}
			var questions []any
			for _, q := range append([]string{s.text}, s.more...) {
				questions = append(questions, obj("question", q, "header", "Decision", "multiSelect", false, "options", options))
			}
			line("assistant", toolUse(id, "AskUserQuestion", obj("questions", questions)))
			line("user", toolResult(id, or(s.result, `User has answered your questions: "`+s.text+`"="Yes".`), s.isError))
		case "delegate":
			id := s.id
			root := or(s.root, testRoot)
			phase := s.phase
			if phase == "" {
				phase = "none"
			}
			var change any = s.change
			if s.nullChange {
				change = nil
			}
			block := obj("context_id", "c0ffee00-"+id, "resolved_at", isoAt(clock), "head", "0123456789abcdef0123456789abcdef01234567", "project_root", root,
				"change", change, "phase", phase, "vcs", obj("state", "ready", "reason", "HEAD resolves to 0123456789ab"))
			task := or(s.prompt, "<task elided>")
			prompt := "```jhon-context\n" + js.StringifyValue(block) + "\n```\n\n" + task
			if s.noBlock {
				prompt = task
			}
			line("assistant", toolUse("toolu_"+id, "Agent", obj("description", "Task", "subagent_type", s.typ, "prompt", prompt)))
			line("user", toolResult("toolu_"+id, "Async agent launched successfully.", false))
			var own *int
			if s.at != nil {
				own = intp(*s.at)
			}
			sts := func() string {
				if own == nil {
					return ts()
				}
				v := isoAt(*own)
				*own++
				return v
			}
			sc := func(o *js.Object) *js.Object {
				o.Set("parentUuid", nil)
				o.Set("isSidechain", true)
				o.Set("agentId", id)
				o.Set("cwd", root)
				return common(o)
			}
			sl := []*js.Object{sc(obj("type", "user", "message", obj("role", "user", "content", prompt), "uuid", id+"-0", "timestamp", sts()))}
			for i, c := range s.calls {
				cid := fmt.Sprintf("toolu_%s_%d", id, i)
				msg := func(content any) *js.Object {
					m := obj("role", "assistant")
					if c.usage != nil {
						m.Set("id", or(c.msgID, "msg_"+cid))
						m.Set("usage", usageObj(c.usage))
					}
					m.Set("content", content)
					return m
				}
				if c.text != nil {
					sl = append(sl, sc(obj("type", "assistant", "message", msg([]any{obj("type", "text", "text", *c.text)}), "uuid", fmt.Sprintf("%s-x%d", id, i), "timestamp", sts())))
				}
				if c.split {
					sl = append(sl, sc(obj("type", "assistant", "message", msg([]any{obj("type", "thinking", "thinking", "", "signature", "")}), "uuid", fmt.Sprintf("%s-t%d", id, i), "timestamp", sts())))
				}
				sl = append(sl, sc(obj("type", "assistant", "message", msg(toolUse(cid, c.name, c.input)), "uuid", fmt.Sprintf("%s-a%d", id, i), "timestamp", sts())))
				if c.wait > 0 {
					if own == nil {
						clock += c.wait
					} else {
						*own += c.wait
					}
				}
				result := "ok"
				if c.result != nil {
					result = *c.result
				}
				u := sc(obj("type", "user", "message", obj("role", "user", "content", toolResult(cid, keep(cid, result, c.persist), c.isError)), "uuid", fmt.Sprintf("%s-u%d", id, i), "timestamp", sts()))
				if c.denial != "" {
					u.Set("toolDenialKind", c.denial)
				}
				sl = append(sl, u)
			}
			result := or(s.result, "**Status**: success")
			end := sts()
			if s.at != nil && s.span != nil {
				end = isoAt(*s.at + *s.span)
			}
			sl = append(sl, sc(obj("type", "assistant", "message", obj("role", "assistant", "content", []any{obj("type", "text", "text", result)}), "uuid", id+"-end", "timestamp", end)))
			writeLines(t, filepath.Join(subdir, "agent-"+id+".jsonl"), sl)
			must(t, os.WriteFile(filepath.Join(subdir, "agent-"+id+".meta.json"), []byte(js.StringifyValueCompact(obj("agentType", s.typ, "description", "Task", "toolUseId", "toolu_"+id))), 0o644))
			note := fmt.Sprintf("<task-notification>\n<task-id>%s</task-id>\n<tool-use-id>toolu_%s</tool-use-id>\n<status>completed</status>\n<summary>Agent finished</summary>\n<result>%s</result>\n</task-notification>", id, id, result)
			if s.queuedResult {
				queue(note, "task-notification")
			} else {
				line("user", note, "origin", obj("kind", "task-notification"))
			}
		}
	}
	file := filepath.Join(dir, sessionID+".jsonl")
	writeLines(t, file, mainLines)
	return file
}

func usageObj(u []float64) *js.Object {
	return obj("input_tokens", u[0], "cache_creation_input_tokens", u[1], "cache_read_input_tokens", u[2], "output_tokens", 10.0)
}

func writeLines(t *testing.T, file string, lines []*js.Object) {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(js.StringifyValueCompact(l) + "\n")
	}
	must(t, os.WriteFile(file, []byte(b.String()), 0o644))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func auditOf(t *testing.T, steps ...step) *Result {
	t.Helper()
	r := Audit(sessionWith(t, steps...))
	if len(r.Errors) > 0 {
		t.Fatalf("audit-error: %+v", r.Errors)
	}
	return r
}

// of is the findings of one check.
func of(fs []Finding, check string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Check == check {
			out = append(out, f)
		}
	}
	return out
}

func findingsOf(t *testing.T, check string, steps ...step) []Finding {
	t.Helper()
	return of(auditOf(t, steps...).Findings, check)
}

// wouldBe asserts the finding is a pending check's would-be violation.
func wouldBe(t *testing.T, fs []Finding) {
	t.Helper()
	if len(fs) != 1 || fs[0].Severity != "suspect" || fs[0].Kind != "would-be-violation" || !strings.HasPrefix(fs[0].Message, WouldBeViolation+": ") {
		t.Fatalf("want one would-be violation, got %+v", fs)
	}
}

// verifiedViolation is one violation of a verified rule.
func verifiedViolation(t *testing.T, fs []Finding) {
	t.Helper()
	if len(fs) != 1 || fs[0].Severity != "violation" || fs[0].Kind != "finding" || fs[0].ValidationStatus != "verified" {
		t.Fatalf("want one violation, got %+v", fs)
	}
}

func fixture(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("../../../test/testdata/audit", dir))
	must(t, err)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			return filepath.Join("../../../test/testdata/audit", dir, e.Name())
		}
	}
	t.Fatalf("no main transcript in %s", dir)
	return ""
}

// A session Claude Code records by name only still ran as its agent.
func TestRunsAs(t *testing.T) {
	main := []string{"jhon-orchestrator"}
	setting := "jhon-orchestrator"
	for _, c := range []struct {
		f    SessionFacts
		want bool
	}{
		{SessionFacts{AgentSetting: &setting}, true},
		{SessionFacts{AgentNames: []string{"jhon orchestrator"}}, true},
		{SessionFacts{AgentNames: []string{"jhon-orchestrator"}}, true},
		{SessionFacts{AgentNames: []string{"Acme Agent"}}, false},
		{SessionFacts{}, false},
	} {
		if got := c.f.RunsAs(main); got != c.want {
			t.Errorf("RunsAs(%+v) = %v, want %v", c.f, got, c.want)
		}
	}
}
