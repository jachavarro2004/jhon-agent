package audit

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The owner's dialogue with the main thread (dialogue.go), in counts: the
// same script told to a Claude Code transcript and to a pi session gives the
// same numbers (T66a, L990 C1-C5).

// said is one step of a dialogue, told to either builder: the owner's
// message, the agent's reply, a command, a round of the ask tool (failed:
// its result is an error), or seconds that pass.
type said struct {
	kind      string // owner, reply, bash, ask, wait, notice
	text      string
	questions []string
	failed    bool
	n         int
}

func owner(text string) said { return said{kind: "owner", text: text} }
func agent(text string) said { return said{kind: "reply", text: text} }
func ran(command string) said {
	return said{kind: "bash", text: command}
}
func asked(questions ...string) said { return said{kind: "ask", questions: questions} }
func later(seconds int) said         { return said{kind: "wait", n: seconds} }

// claudeSteps and piSteps are the dialogue as each runtime records it; every
// line of either takes one second.
func claudeSteps(d []said) []step {
	var out []step
	for _, x := range d {
		switch x.kind {
		case "owner":
			out = append(out, user(x.text))
		case "reply":
			out = append(out, step{kind: "reply", text: x.text})
		case "bash":
			out = append(out, mainBash(x.text))
		case "ask":
			s := step{kind: "ask", text: x.questions[0], more: x.questions[1:], options: []string{"Option kept private", "Other"}}
			if x.failed {
				s.isError, s.result = true, "The owner doesn't want to proceed with this tool use."
			}
			out = append(out, s)
		case "wait":
			out = append(out, step{kind: "wait", wait: x.n})
		case "notice":
			out = append(out, step{kind: "notify", text: x.text, result: "/tmp/b1.output"})
		}
	}
	return out
}

func piSteps(d []said) []pstep {
	out := []pstep{pstart("tui")}
	for _, x := range d {
		switch x.kind {
		case "owner":
			out = append(out, puser(x.text))
		case "reply":
			out = append(out, preply(x.text))
		case "bash":
			out = append(out, pmain(pbash(x.text, "ok")))
		case "ask":
			var qs []any
			for _, q := range x.questions {
				qs = append(qs, obj("question", q, "header", "Decision", "options", []any{obj("label", "Option kept private", "description", "d"), obj("label", "Other")}))
			}
			out = append(out, pmain(pcall{name: "ask_user", args: obj("questions", qs), result: "The owner answered: Option kept private", isError: x.failed}))
		case "wait":
			out = append(out, pstep{kind: "wait", n: x.n})
		case "notice":
			out = append(out, pstep{kind: "notice", text: "Background job job-3 (\"CI\") exited with code 0 after 35s.\nCommand: make\nLast output:\n" + x.text})
		}
	}
	return out
}

// onBoth runs check on the dialogue's audit as each runtime records it.
func onBoth(t *testing.T, d []said, check func(t *testing.T, r *Result)) {
	t.Helper()
	t.Run("claude code", func(t *testing.T) { check(t, auditOf(t, claudeSteps(d)...)) })
	t.Run("pi", func(t *testing.T) { check(t, piAudit(t, piSteps(d)...)) })
}

// theDialogue is one session: a first message, a question in plain text and
// its answer, work, a round of two questions, a second plain question, and
// a last answer with some work after it.
var theDialogue = []said{
	owner("build it"),                 // 0s: the first message is no answer
	agent("Which database?"),          // 1s: a question in plain text
	owner("the small one"),            // 2s: answer 1
	ran("ls"),                         // 3s, 4s: a response and its call
	ran("pwd"),                        // 5s, 6s: a second
	later(54),                         // the model takes its time
	asked("Deploy now?", "Where to?"), // 61s: the third response asks; 62s: answer 2
	agent("Done. **Anything else?**"), // 63s: one response, a plain question
	owner("no"),                       // 64s: answer 3
	ran("date"),                       // 65s, 66s
	later(10),                         // the model again
	agent("All closed."),              // 77s: its second response
}

func TestDialogueCountsTheOwnersAnswers(t *testing.T) {
	onBoth(t, theDialogue, func(t *testing.T, r *Result) {
		if got := len(r.Dialogue.Answers); got != 3 {
			t.Errorf("owner answers = %d, want 3: two messages after the first and one round of the ask tool", got)
		}
	})
	// The first message, a tool result, and a round the ask tool failed are no answers.
	onBoth(t, []said{owner("go"), ran("ls"), {kind: "ask", questions: []string{"Sure?"}, failed: true}, agent("Stopped.")}, func(t *testing.T, r *Result) {
		if got := len(r.Dialogue.Answers); got != 0 {
			t.Errorf("owner answers = %d, want 0", got)
		}
		eq(t, "ask-tool calls", r.Dialogue.AskCalls, 1)
	})
}

func TestDialogueMeasuresEachAnswerUntilTheNextWait(t *testing.T) {
	onBoth(t, theDialogue, func(t *testing.T, r *Result) {
		want := []AnswerCost{{Responses: 3, ToolCalls: 2, Ms: 59000}, {Responses: 1, ToolCalls: 0, Ms: 1000}, {Responses: 2, ToolCalls: 1, Ms: 13000}}
		eq(t, "each answer's responses, tool calls, and time", r.Dialogue.Answers, want)
		median, max := r.Dialogue.Responses()
		eq(t, "responses: median, max", [2]float64{median, max}, [2]float64{2, 3})
		median, max = r.Dialogue.ToolCalls()
		eq(t, "tool calls: median, max", [2]float64{median, max}, [2]float64{1, 2})
		median, max = r.Dialogue.Ms()
		eq(t, "ms: median, max", [2]float64{median, max}, [2]float64{13000, 59000})
	})
}

func TestDialogueCountsAskCallsByTheirQuestions(t *testing.T) {
	d := []said{owner("go"), asked("a?"), asked("b?"), asked("c?", "d?"), asked("e?", "f?", "g?"), asked("h?", "i?", "j?", "k?"), asked("l?", "m?", "n?", "o?", "p?"), agent("Done.")}
	onBoth(t, d, func(t *testing.T, r *Result) {
		eq(t, "ask-tool calls", r.Dialogue.AskCalls, 6)
		eq(t, "calls with 1, 2, 3, and 4 or more questions", r.Dialogue.QuestionsPerCall, [4]int{2, 1, 1, 2})
		eq(t, "each round answered is an owner answer", len(r.Dialogue.Answers), 6)
		eq(t, "a round of the ask tool is no question in plain text", r.Dialogue.ProseQuestions, 0)
	})
}

func TestDialogueCountsTurnsThatEndInAPlainQuestion(t *testing.T) {
	onBoth(t, theDialogue, func(t *testing.T, r *Result) {
		eq(t, "prose questions", r.Dialogue.ProseQuestions, 2)
	})
	for _, c := range []struct {
		what string
		d    []said
		want int
	}{
		{"the session ends on a question", []said{owner("go"), agent("Shall I go on?")}, 1},
		{"closing markdown, quotes, and whitespace after the mark", []said{owner("go"), agent("Is it `this?`"), owner("yes"), agent("Or \"that?\" \n"), owner("no"), agent("So: *“the other?”*")}, 3},
		{"a question followed by more text is not counted: a lower bound", []said{owner("go"), agent("Ready? I will wait for your word.")}, 0},
		{"no question", []said{owner("go"), agent("Done."), owner("thanks"), agent("Closed.")}, 0},
		{"a turn the owner cut short did not end in its question", []said{owner("go"), agent("Is this right?"), ran("ls"), owner("stop")}, 0},
		{"a turn that holds an ask-tool call is not a plain question", []said{owner("go"), {kind: "ask", questions: []string{"Sure?"}, failed: true}, agent("Then shall I stop?")}, 0},
	} {
		onBoth(t, c.d, func(t *testing.T, r *Result) {
			eq(t, c.what, r.Dialogue.ProseQuestions, c.want)
		})
	}
}

// Only the main thread is measured: a sub-agent's question and its calls are
// its own, and the notice of its end is not the owner's answer.
func TestDialogueLeavesSubAgentsOut(t *testing.T) {
	question := "Which one?"
	sub := delegate("d01", "worker", chg, tcall{name: "AskUserQuestion", input: obj("questions", []any{obj("question", "Sure?")}), text: &question}, bash("ls"))
	sub.result = "Shall I go on?"
	r := auditOf(t, user("go"), step{kind: "reply", text: "Start?"}, user("yes"), sub, step{kind: "reply", text: "Done."})
	eq(t, "ask-tool calls", r.Dialogue.AskCalls, 0)
	eq(t, "prose questions", r.Dialogue.ProseQuestions, 1)
	// The launch is the answer's one call; the launch and the last reply are its responses.
	eq(t, "owner answers", len(r.Dialogue.Answers), 1)
	for _, a := range r.Dialogue.Answers {
		eq(t, "the answer's responses and calls", [2]int{a.Responses, a.ToolCalls}, [2]int{2, 1})
	}

	p := piAudit(t, puser("go"), preply("Start?"), puser("yes"),
		psub("worker", "Do it.\n\n## Allowed edit surfaces\n- a.txt", "Shall I go on?", pcall{name: "ask_user", args: obj("question", "Sure?", "options", []any{"a", "b"}), result: "a"}), preply("Done."))
	eq(t, "pi: ask-tool calls", p.Dialogue.AskCalls, 0)
	eq(t, "pi: prose questions", p.Dialogue.ProseQuestions, 1)
	eq(t, "pi: owner answers", len(p.Dialogue.Answers), 1)
	for _, a := range p.Dialogue.Answers {
		eq(t, "pi: the answer's responses and calls", [2]int{a.Responses, a.ToolCalls}, [2]int{2, 1})
	}
}

// The lines of one Claude Code response carry its id and are one response.
func TestDialogueCountsAResponseOnceWhateverItsLines(t *testing.T) {
	part := func(n int, id string, content *js.Object) step {
		return step{kind: "raw", raw: obj("parentUuid", nil, "isSidechain", false, "type", "assistant", "uuid", fmt.Sprintf("r-%d", n), "timestamp", isoAt(10+n),
			"userType", "external", "cwd", testRoot, "sessionId", sessionID, "version", "2.1.280",
			"message", obj("role", "assistant", "id", id, "content", []any{content}))}
	}
	r := auditOf(t, user("go"), step{kind: "reply", text: "Start?"}, user("yes"),
		part(1, "msg_1", obj("type", "thinking", "thinking", "", "signature", "")), part(2, "msg_1", obj("type", "text", "text", "On it. Which one?")),
		part(3, "msg_2", obj("type", "text", "text", "Tell me: this one?")), part(4, "msg_2", obj("type", "thinking", "thinking", "", "signature", "")))
	eq(t, "owner answers", len(r.Dialogue.Answers), 1)
	for _, a := range r.Dialogue.Answers {
		eq(t, "responses", a.Responses, 2)
	}
	// The last response's last text ends in the question, though a line with no text follows it.
	eq(t, "prose questions", r.Dialogue.ProseQuestions, 2)
}

// A session with no owner answer has no median to state.
func TestDialogueOfASessionWithNoAnswer(t *testing.T) {
	onBoth(t, []said{owner("go"), agent("Done.")}, func(t *testing.T, r *Result) {
		got := js.StringifyValueCompact(js.Prop(r.Report(), "dialogue"))
		want := `{"owner_answers":0,"responses_per_answer":{"median":null,"max":null},"tool_calls_per_answer":{"median":null,"max":null},"ms_per_answer":{"median":null,"max":null},` +
			`"ask_tool_calls":0,"questions_per_call":{"1":0,"2":0,"3":0,"4_or_more":0},"prose_questions":0,` +
			`"corrections":{"owner_messages":1,"correcting":0,"keep_going":0,"repeat":0,"unrequested":0,"decision":0}}`
		eq(t, "dialogue", got, want)
		if out := Human(r, "s.jsonl", false); !strings.Contains(out, "\n  dialogue  owner answers 0\n  dialogue  ask-tool calls 0  questions per call 1:0 2:0 3:0 4+:0  prose questions 0 (lower bound)\n"+
			"  dialogue  corrections 0 of 1 owner messages  keep-going 0  repeat 0  unrequested 0  decision 0\n") {
			t.Errorf("the human lines:\n%s", out)
		}
	})
}

// The report states the dialogue as statistics: an object of counts in the
// JSON after timing, two lines in the human output, and nothing else moves:
// no finding, the same status, the same closing line.
func TestDialogueIsReportedAsStatisticsNeverAFinding(t *testing.T) {
	onBoth(t, theDialogue, func(t *testing.T, r *Result) {
		got := js.StringifyValueCompact(js.Prop(r.Report(), "dialogue"))
		want := `{"owner_answers":3,"responses_per_answer":{"median":2,"max":3},"tool_calls_per_answer":{"median":1,"max":2},"ms_per_answer":{"median":13000,"max":59000},` +
			`"ask_tool_calls":1,"questions_per_call":{"1":0,"2":1,"3":0,"4_or_more":0},"prose_questions":2,` +
			`"corrections":{"owner_messages":3,"correcting":0,"keep_going":0,"repeat":0,"unrequested":0,"decision":0}}`
		eq(t, "dialogue", got, want)
		keys := r.Report().Keys()
		eq(t, "the report's last keys", keys[len(keys)-2:], []string{"timing", "dialogue"})
		out := Human(r, "s.jsonl", false)
		lines := "\n  dialogue  owner answers 3  responses per answer 2 (max 3)  tool calls per answer 1 (max 2)  time per answer 13s (max 59s)\n" +
			"  dialogue  ask-tool calls 1  questions per call 1:0 2:1 3:0 4+:0  prose questions 2 (lower bound)\n" +
			"  dialogue  corrections 0 of 3 owner messages  keep-going 0  repeat 0  unrequested 0  decision 0\n"
		if !strings.Contains(out, lines) {
			t.Errorf("missing %q in\n%s", lines, out)
		}
		if i, j := strings.Index(out, "\n  timing  main  "), strings.Index(out, "\n  dialogue  "); i < 0 || j < i {
			t.Errorf("the dialogue lines follow the timing lines:\n%s", out)
		}
		eq(t, "status", r.Status, "nothing-to-audit")
		eq(t, "ok", r.OK, true)
		eq(t, "findings", len(r.Findings), 0)
		if !strings.HasSuffix(out, "\nnothing-to-audit") {
			t.Errorf("closing line: %s", out)
		}
		// Counts only: no message, question, or option text in either output.
		for _, text := range []string{"Which database", "the small one", "Deploy now", "Where to", "Anything else", "Option kept private", "All closed"} {
			if strings.Contains(out, text) || strings.Contains(js.StringifyValue(r.Report()), text) {
				t.Errorf("the report holds the session's text %q", text)
			}
		}
	})
}

// An audit that could not read the session measures no dialogue.
func TestDialogueIsNullOnAnAuditError(t *testing.T) {
	r := Audit(fixture(t, "bad-json"))
	eq(t, "status", r.Status, "audit-error")
	if r.Dialogue != nil || !r.Report().Has("dialogue") || js.Prop(r.Report(), "dialogue") != nil {
		t.Errorf("dialogue: %+v, %v", r.Dialogue, js.Prop(r.Report(), "dialogue"))
	}
	if out := Human(r, "s.jsonl", false); strings.Contains(out, "dialogue") {
		t.Errorf("the human output of an audit-error:\n%s", out)
	}
}

// jhon audit --all sums the sessions' dialogues, Jhon's apart, and takes
// the median and the maximum over every answer of every session.
func TestAggregateSumsTheDialogue(t *testing.T) {
	first := auditOf(t, claudeSteps(theDialogue)...)
	second := piAudit(t, piSteps([]said{owner("go"), agent("Ready?"), owner("yes"), ran("a"), ran("b"), ran("c"), ran("d"), later(100), asked("x?", "y?", "z?", "w?"), agent("Done.")})...)
	f := Aggregate([]Audited{{Path: "a.jsonl", Result: first}, {Path: "b.jsonl", Jhon: true, Result: second},
		{Path: "c.jsonl", Jhon: true, Result: &Result{Status: "audit-error", Errors: []AuditError{{Message: "unreadable"}}}}}, nil, time.Time{})
	all, jhon := f.Dialogue, f.JhonDialogue
	eq(t, "sessions summed: all, Jhon's", [2]int{all.Sessions, jhon.Sessions}, [2]int{2, 1})
	eq(t, "owner answers", [2]int{len(all.Answers), len(jhon.Answers)}, [2]int{5, 2})
	eq(t, "ask-tool calls", [2]int{all.AskCalls, jhon.AskCalls}, [2]int{2, 1})
	eq(t, "questions per call", [2][4]int{all.QuestionsPerCall, jhon.QuestionsPerCall}, [2][4]int{{0, 1, 0, 1}, {0, 0, 0, 1}})
	eq(t, "prose questions", [2]int{all.ProseQuestions, jhon.ProseQuestions}, [2]int{3, 1})
	// Responses per answer: 3, 1, 2 and 5, 1; tool calls: 2, 0, 1 and 4, 0.
	median, max := all.Responses()
	eq(t, "responses over every answer: median, max", [2]float64{median, max}, [2]float64{2, 5})
	median, max = all.ToolCalls()
	eq(t, "tool calls over every answer: median, max", [2]float64{median, max}, [2]float64{1, 4})
	median, max = jhon.Ms()
	eq(t, "Jhon's ms: median, max", [2]float64{median, max}, [2]float64{1000, 109000})
}
