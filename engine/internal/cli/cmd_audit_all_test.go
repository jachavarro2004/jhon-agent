package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/audit"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"reflect"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
)

// An agent without a turn cap is measured against the review mark, and a
// cap wins where one is set.
func TestTurnMarksAreTheCapOrTheReviewMark(t *testing.T) {
	got := turnMarks([]manifest.Agent{{Name: "worker"}, {Name: "explore", MaxTurns: "60"}})
	if want := map[string]int{"worker": reviewTurns, "explore": 60}; !reflect.DeepEqual(got, want) {
		t.Errorf("turnMarks: %v, want %v", got, want)
	}
}

// T131 (L905, L908): a run is measured, never stopped, at its mark.
func TestTheSubAgentsHeaderSaysPiStopsNoRunAtItsMark(t *testing.T) {
	want := "sub-agents of Jhon's sessions started since the cutoff, each against the review mark of 100 turns, or its turn cap where its manifest names one (past it: counted; pi stops no run there)"
	if got := subAgentsHeader(); got != want {
		t.Errorf("header:\n got %s\nwant %s", got, want)
	}
}

// T131 (L905, L908): the worker has no turn cap in agents.yaml, so its audit
// mark is the review mark, as the other executors'.
func TestTheWorkersAuditMarkIsTheReviewMark(t *testing.T) {
	marks := turnMarks(manifest.MustLoad(filepath.Join("..", "..", "..")).Agents)
	if marks["jhon-builder"] != reviewTurns || marks["jhon-checker"] != reviewTurns || marks["jhon-scout"] != reviewTurns {
		t.Errorf("turnMarks: jhon-builder %d, jhon-checker %d, jhon-scout %d; want %d each", marks["jhon-builder"], marks["jhon-checker"], marks["jhon-scout"], reviewTurns)
	}
}

// The LDD table shows each change's slices beside the tasks of its largest,
// so an inflated slice shows, in the text and the JSON alike.
func TestLDDShowsTheLargestSlice(t *testing.T) {
	cs := []audit.ChangeLDD{{Change: "c1", Start: "2026-10-28T05:00:00.000Z", Slices: 3, MaxTasks: 14, OwnerMessages: 6, Commits: 4}}
	lines := lddLines(cs)
	if !strings.Contains(lines[1], "slices  max tasks") || !regexp.MustCompile(`^  c1\s+3\s+14\s+6\s`).MatchString(lines[2]) {
		t.Errorf("lddLines:\n%s", strings.Join(lines, "\n"))
	}
	row := js.StringifyValueCompact(lddValue(cs))
	if !strings.Contains(row, `"slices":3,"max_tasks":14`) {
		t.Errorf("lddValue: %s", row)
	}
}

// T55: --draft [<dir>] (default $XDG_STATE_HOME/jhon/audit-drafts), --min,
// and --refresh; --min and --refresh only with --draft.
func TestAuditAllDraftOptions(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	for _, c := range []struct {
		argv []string
		want auditAllOptions
		ok   bool
	}{
		{[]string{"--all"}, auditAllOptions{since: "30d", min: 2}, true},
		{[]string{"--all", "--draft"}, auditAllOptions{since: "30d", min: 2, draft: "/state/jhon/audit-drafts"}, true},
		{[]string{"--all", "--draft", "--json"}, auditAllOptions{since: "30d", min: 2, json: true, draft: "/state/jhon/audit-drafts"}, true},
		{[]string{"--all", "--draft", "d", "--min", "3", "--refresh"}, auditAllOptions{since: "30d", min: 3, draft: "d", refresh: true}, true},
		{[]string{"--all", "--min", "3"}, auditAllOptions{}, false},
		{[]string{"--all", "--refresh"}, auditAllOptions{}, false},
		{[]string{"--all", "--draft", "d", "--min", "0"}, auditAllOptions{}, false},
		{[]string{"--all", "--draft", "d", "--min"}, auditAllOptions{}, false},
	} {
		got, ok := parseAuditAll(c.argv)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("%v: %+v %v, want %+v %v", c.argv, got, ok, c.want, c.ok)
		}
	}
}

// Without --draft nothing is written; with it, the repeated finding's draft is.
func TestAuditAllWritesDraftsOnlyWithDraft(t *testing.T) {
	home, state := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", state)
	root, _ := filepath.Abs(filepath.Join("..", "..", ".."))
	t.Setenv("JHON_ROOT", root)
	src := filepath.Join("..", "..", "..", "test", "testdata", "audit", "background-run-failed-delivered")
	dst := filepath.Join(home, ".claude", "projects", "-work-p")
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	devnull, _ := os.Open(os.DevNull)
	os.Stdout = devnull
	defer func() { os.Stdout = stdout; devnull.Close() }()
	if code, err := runAuditAll([]string{"--all", "--since", "3650d"}); code != 0 || err != nil {
		t.Fatalf("audit --all: %d %v", code, err)
	}
	if _, err := os.Stat(filepath.Join(state, "jhon")); !os.IsNotExist(err) {
		t.Fatalf("audit --all without --draft wrote under the state dir: %v", err)
	}
	if code, err := runAuditAll([]string{"--all", "--since", "3650d", "--draft", "--min", "1"}); code != 0 || err != nil {
		t.Fatalf("audit --all --draft: %d %v", code, err)
	}
	drafts, _ := filepath.Glob(filepath.Join(state, "jhon", "audit-drafts", "delivered-untested-*.md"))
	if len(drafts) != 1 {
		t.Fatalf("drafts: %v", drafts)
	}
}

// T66a (L990 C3): the owner's dialogue summed across the sessions, Jhon's
// apart: the answers, what each cost until the next wait (median / max over
// every answer), the ask-tool calls by their questions, and the prose
// questions; counts only, in the text and the JSON alike.
func TestAuditAllStatesTheOwnersDialogue(t *testing.T) {
	all := audit.FleetDialogue{Sessions: 2, Dialogue: audit.Dialogue{
		Answers:  []audit.AnswerCost{{Responses: 3, ToolCalls: 2, Ms: 59000}, {Responses: 1, ToolCalls: 0, Ms: 1000}, {Responses: 2, ToolCalls: 1, Ms: 13000}, {Responses: 5, ToolCalls: 4, Ms: 109000}, {Responses: 1, ToolCalls: 0, Ms: 1000}},
		AskCalls: 2, QuestionsPerCall: [4]int{0, 1, 0, 1}, ProseQuestions: 3,
		Corrections: audit.Corrections{OwnerMessages: 11, Correcting: 8, KeepGoing: 4, Repeat: 3, Unrequested: 2, Decision: 3}}}
	none := audit.FleetDialogue{}
	lines := dialogueLines(all, none)
	if len(lines) != 7 || !strings.HasPrefix(lines[0], "owner dialogue in the main threads, as statistics (") ||
		!strings.Contains(lines[0], "corrections: the owner's messages that correct the agent, by kind; keep-going never counts an answer to a question") ||
		!regexp.MustCompile(`^\s+sessions\s+answers\s+responses\s+tool calls\s+time\s+ask-tool calls\s+1q\s+2q\s+3q\s+4\+q\s+prose questions$`).MatchString(lines[1]) ||
		!regexp.MustCompile(`^  all\s+2\s+5\s+2 / 5\s+1 / 4\s+13s / 2m\s+2\s+0\s+1\s+0\s+1\s+3$`).MatchString(lines[2]) ||
		!regexp.MustCompile(`^  Jhon's\s+0\s+0\s+-\s+-\s+-\s+0\s+0\s+0\s+0\s+0\s+0$`).MatchString(lines[3]) ||
		!regexp.MustCompile(`^\s+sessions\s+owner messages\s+correcting\s+keep-going\s+repeat\s+unrequested\s+decision$`).MatchString(lines[4]) ||
		!regexp.MustCompile(`^  all\s+2\s+11\s+8\s+4\s+3\s+2\s+3$`).MatchString(lines[5]) ||
		!regexp.MustCompile(`^  Jhon's\s+0\s+0\s+0\s+0\s+0\s+0\s+0$`).MatchString(lines[6]) {
		t.Fatalf("dialogueLines:\n%s", strings.Join(lines, "\n"))
	}
	for _, table := range [][]string{lines[1:4], lines[4:7]} {
		for _, l := range table {
			if len(l) != len(table[0]) {
				t.Errorf("the columns do not line up:\n%s", strings.Join(lines, "\n"))
			}
		}
	}
	got := js.StringifyValueCompact(all.Value())
	if want := `{"sessions":2,"owner_answers":5,"responses_per_answer":{"median":2,"max":5},"tool_calls_per_answer":{"median":1,"max":4},"ms_per_answer":{"median":13000,"max":109000},` +
		`"ask_tool_calls":2,"questions_per_call":{"1":0,"2":1,"3":0,"4_or_more":1},"prose_questions":3,` +
		`"corrections":{"owner_messages":11,"correcting":8,"keep_going":4,"repeat":3,"unrequested":2,"decision":3}}`; got != want {
		t.Errorf("the fleet's dialogue:\n got %s\nwant %s", got, want)
	}
	if got := js.StringifyValueCompact(none.Value()); !strings.Contains(got, `"owner_answers":0,"responses_per_answer":{"median":null,"max":null}`) {
		t.Errorf("no session: %s", got)
	}
}

// T66a (L990 C5): the help names the dialogue statistics of one session's
// audit and of --all, and says they are never a finding.
func TestHelpNamesTheAuditsDialogueStatistics(t *testing.T) {
	_, out := capture(t, []string{"--help"})
	out = strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		"and the owner's dialogue summed, Jhon's sessions apart;",
		"the owner's dialogue with the main thread, as statistics and never a finding: the owner's answers,",
		"the responses, tool calls and time each took until the session next waited for the owner (median and maximum),",
		"the ask-tool calls by their questions (1, 2, 3, 4 or more),",
		"the turns that end in a question in plain text with no ask-tool call (a lower bound),",
		"and the owner's corrections: of the messages the owner wrote, those that correct the agent, counted by kind,",
		`keep-going ("sigue", "no pares", "por qué paraste", "keep going", "don't stop"), never in a message that answers a question`,
		`repeat ("te dije", "otra vez", "te repito", "I told you"), unrequested ("no te pedí", "eso no", "sobreingeniería", "no hagas eso", "I didn't ask")`,
		`and decision ("no decidas", "pregúntame", "esa decisión es mía", "ask me"), accents optional`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("jhon --help does not say %q", want)
		}
	}
}

// T134 (L1021 H8): the help names each agent role's integration statistics
// of one session's audit and of --all.
func TestHelpNamesTheAuditsIntegrationStatistics(t *testing.T) {
	_, out := capture(t, []string{"--help"})
	out = strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		"each agent role's use of memory, CodeGraph and the todo list summed, Jhon's sessions apart,",
		"how each agent role (the main thread as the orchestrator, each sub-agent by its type) used the integrations: its launches, those with a memory read and with a memory write,",
		"its CodeGraph calls against its plain reads",
		"with CodeGraph's share, and its todo calls;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("jhon --help does not say %q", want)
		}
	}
}
