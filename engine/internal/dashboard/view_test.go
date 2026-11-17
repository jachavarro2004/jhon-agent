package dashboard

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The view of one audit report and the page that embeds it.
// testdata/report.json is the report.

func report(t *testing.T) any {
	t.Helper()
	b, err := os.ReadFile("testdata/report.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := js.ParseJSON(string(b))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// at is the compact JSON of v at a path of keys and indices.
func at(v any, path ...any) string {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			v = js.Prop(v, k)
		case int:
			v = v.([]any)[k]
		}
	}
	return js.StringifyValueCompact(v)
}

func eq(t *testing.T, got, want, what string) {
	t.Helper()
	if got != want {
		t.Errorf("%s:\n got %s\nwant %s", what, got, want)
	}
}

func TestViewSummary(t *testing.T) {
	eq(t, at(View(report(t)), "summary"),
		`{"elapsed_ms":14400000,"active_ms":7200000,"resumes":2,"subagents":3,"processed":100000000,"changes":2,"findings":{"violation":1,"suspect":2}}`,
		"the summary counts time, sub-agents (not the main thread), tokens, changes, and findings by severity")
}

func TestViewChanges(t *testing.T) {
	v := View(report(t))
	eq(t, at(v, "changes", 0, "name")+at(v, "changes", 1, "name"), `"beta""alpha"`, "a change that needs attention comes first")
	alpha := js.Prop(v, "changes").([]any)[1]
	eq(t, at(alpha, "phases"), `[["explore",1],["verify",1],["worker",2]]`, "agent types by name")
	eq(t, at(alpha, "processed")+" "+at(alpha, "peak")+" "+at(alpha, "share"), "9500000 280000 0.095", "tokens")
	eq(t, at(alpha, "attention"), `[]`, "nothing stands out")
	eq(t, at(v, "changes", 0, "attention"),
		`["verify failed 2 time(s)","1 agent(s) stopped short","1 relaunch(es)","the last test record failed","1 test timeout(s)"]`, "beta's attention")
}

func TestViewUnattributed(t *testing.T) {
	eq(t, at(View(report(t)), "unattributed"), `{"agents":1,"processed":89500000,"share":0.895}`, "work outside any change")
}

func TestViewAgentTypes(t *testing.T) {
	v := View(report(t))
	eq(t, strconv.Itoa(len(js.Prop(v, "agentTypes").([]any))), "2", "agent types")
	eq(t, at(v, "agentTypes", 0, "type")+at(v, "agentTypes", 1, "type"), `"worker""verify"`, "by tokens, without the main thread")
	eq(t, at(v, "agentTypes", 0), `{"type":"worker","count":2,"avg_turns":60,"peak":280000,"processed":10000000,"share":0.1,"long":1,"rereads":2}`, "worker")
}

func TestViewAgents(t *testing.T) {
	v := View(report(t))
	var ids []string
	for i := range js.Prop(v, "agents").([]any) {
		ids = append(ids, at(v, "agents", i, "id"))
	}
	eq(t, strings.Join(ids, ","), `"a1","a2","a3"`, "sub-agents by tokens")
	eq(t, at(v, "agents", 0, "change"), `"alpha"`, "its change")
	eq(t, at(v, "agents", 0, "long")+at(v, "agents", 1, "long"), `"280K context in one turn"null`, "a long one says why")
	eq(t, at(v, "agents", 0, "reads"), `{"full":100,"ranged":0,"shell":0,"codegraph":200,"engram":0,"search":0,"other":0}`, "reads")
}

func TestViewFindings(t *testing.T) {
	v := View(report(t))
	eq(t, at(v, "findings", 0, "check")+at(v, "findings", 0, "violation")+at(v, "findings", 0, "suspect"), `"refusal-bypass"11`, "violations first")
	eq(t, at(v, "findings", 1, "check")+at(v, "findings", 1, "violation")+at(v, "findings", 1, "suspect"), `"delivered-untested"01`, "then suspects")
	if !regexp.MustCompile(`(?i)refus`).MatchString(at(v, "findings", 0, "explain")) {
		t.Error("each check is explained")
	}
	eq(t, strconv.Itoa(len(js.Prop(v, "findings").([]any)))+strconv.Itoa(len(js.Prop(js.Prop(v, "findings").([]any)[0], "items").([]any))), "22", "two checks, two refusal-bypass items")
	eq(t, at(v, "findings", 0, "items", 0, "severity")+at(v, "findings", 0, "items", 0, "where"), `"violation""s.jsonl:9"`, "a violation first, where it is")
}

func TestViewReads(t *testing.T) {
	eq(t, at(View(report(t)), "reads"), `{"full":400,"ranged":0,"shell":0,"codegraph":800,"engram":0,"search":0,"other":0}`, "summed by source")
}

func TestViewWithoutContext(t *testing.T) {
	r, _ := js.ParseJSON(`{"ok":true,"status":"clean","findings":[],"errors":[],"notes":[]}`)
	v := View(r)
	eq(t, at(v, "summary", "subagents")+at(v, "changes")+at(v, "agentTypes")+at(v, "hasContext"), `0[][]false`, "a view without --context or outcomes")
}

func TestViewArithmetic(t *testing.T) {
	// JavaScript's operators on a malformed report: `+` joins text, a missing number is NaN (null).
	r, _ := js.ParseJSON(`{"context_use":{"agents":[{"agent_type":"t","turns":2,"tool_result_bytes":{"full_read_bytes":"5"}}]},
		"outcomes":{"changes":[{"change":"c","test_runs":{"timeouts":2},"record":{"passed":true,"timeouts":"1"}}]}}`)
	v := View(r)
	eq(t, at(v, "reads", "full"), `"05"`, "0 + \"5\"")
	eq(t, at(v, "changes", 0, "attention"), `["21 test timeout(s)"]`, "2 + \"1\"")
	eq(t, at(v, "agentTypes", 0, "processed")+at(v, "agentTypes", 0, "peak"), "nullnull", "undefined sums to NaN")
}

// assets is the repository's dashboard component.
const assets = "../../../dashboard"

func TestPage(t *testing.T) {
	r := report(t)
	session := SessionData(Session{ID: "abc12345-0000", Name: "acme-app"}, r)
	html, err := Page(assets, session, r)
	if err != nil {
		t.Fatal(err)
	}
	embedded := func(id string) string {
		m := regexp.MustCompile(`<script type="application/json" id="` + id + `">([\s\S]*?)</script>`).FindStringSubmatch(html)
		if m == nil {
			t.Fatalf("no %s", id)
		}
		return strings.ReplaceAll(m[1], `\u003c`, "<")
	}
	eq(t, embedded("report"), js.StringifyValueCompact(r), "the report")
	eq(t, embedded("session"), js.StringifyValueCompact(session), "the session and its view")
	eq(t, at(session, "session"), `{"id":"abc12345-0000","project":"acme-app","modified":"","runtime":"claude-code"}`, "the session's metadata, Claude Code unless it says otherwise")
	if !strings.Contains(html, "<style>") || !strings.Contains(html, `<script type="module">`) || !strings.Contains(html, "function verdictOf") {
		t.Error("the stylesheet and the script are inlined")
	}
	if regexp.MustCompile(`<(script|link)[^>]+(src|href)=|https?://`).MatchString(html) {
		t.Error("loads something external")
	}
	if got := embed("</script>"); got != `"\u003c/script>"` {
		t.Errorf("embed: %s", got)
	}
	if _, err := Page(t.TempDir(), session, r); err == nil || !strings.Contains(err.Error(), "run `jhon sync`") {
		t.Errorf("a missing component: %v", err)
	}
}

func TestHTMLOfAGoValue(t *testing.T) {
	type rep struct {
		Status string `json:"status"`
		Notes  []any  `json:"notes"`
	}
	eq(t, at(View(rep{"clean", []any{"n<"}}), "notes"), `["n<"]`, "a Go report reads through its JSON")
}

// A summary's checks carry View's title, so the session list and Home name
// a check in the same words as the session's Findings tab (T17b, V3).
func TestSummaryChecksCarryTheirTitle(t *testing.T) {
	r, _ := js.ParseJSON(`{"ok":false,"status":"findings","findings":[{"check":"isolation-bypass","severity":"violation","message":"m","where":"w","agent":"a"}]}`)
	got := js.StringifyValueCompact(js.Prop(SummaryOf(View(r)), "checks"))
	if want := `[{"check":"isolation-bypass","title":"Isolation left","violation":1,"suspect":0}]`; got != want {
		t.Errorf("checks = %s, want %s", got, want)
	}
	if !strings.HasPrefix(summaryKey(Session{}), "v6:") {
		t.Errorf("a summary without titles is audited again: key %q", summaryKey(Session{}))
	}
}

// A summary counts the audit's errors, so the list reads them as the page
// does: something to look at (L139).
func TestSummaryCountsTheAuditErrors(t *testing.T) {
	r, _ := js.ParseJSON(`{"ok":false,"status":"clean","findings":[],"errors":[{"message":"a"},{"message":"b"}]}`)
	if got := js.Prop(SummaryOf(View(r)), "errors"); got != 2.0 {
		t.Errorf("errors = %v, want 2", got)
	}
}
