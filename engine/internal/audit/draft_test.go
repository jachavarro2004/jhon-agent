package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T55: `jhon audit --all --draft <dir>` groups repeated findings and drafts
// one task file per group, with the evidence cited.

func draftFinding(check, transcript string, line int, at, message string) Finding {
	return Finding{Check: check, Anchor: "ldd/red-first", ValidationStatus: "pending-real-fixture", Severity: "suspect", Kind: "would-be-violation",
		Transcript: transcript, Line: line, At: at, Agent: "a1", AgentType: "worker", Evidence: "Edit src/app.go", Message: message}
}

func audited(path, runtime string, fs ...Finding) Audited {
	return Audited{Path: path, Result: &Result{Status: "findings", Runtime: runtime, Findings: fs}}
}

// The key never carries a path, a line, or a date: the same cause in other
// sessions, files, and days is one group.
func TestDraftKeyIsStableAcrossPathsLinesAndDates(t *testing.T) {
	a := Cause("refusal-bypass", WouldBeViolation+": refused rm /work/p/a.txt at agent-1.jsonl:12 on 2026-09-30T10:00:01.000Z, then wrote /home/owner/q/b.txt (sha 0123abcd9)")
	b := Cause("refusal-bypass", WouldBeViolation+": refused rm /srv/other/c.txt at agent-9.jsonl:340 on 2026-11-02T08:30:00.000Z, then wrote /tmp/x/y.txt (sha fedcba98765)")
	if a != b {
		t.Fatalf("causes differ:\n%s\n%s", a, b)
	}
	if DraftKey("refusal-bypass", "runtime-boundaries/same-effect", "pi", "worker", a) != DraftKey("refusal-bypass", "runtime-boundaries/same-effect", "pi", "worker", b) {
		t.Fatal("keys differ")
	}
	if k := DraftKey("refusal-bypass", "runtime-boundaries/same-effect", "pi", "worker", a); len(k) != 12 || k == DraftKey("refusal-bypass", "runtime-boundaries/same-effect", "claude-code", "worker", a) {
		t.Fatalf("key %q: want 12 hex characters that change with the runtime", k)
	}
	// edit-before-red's cause is its form, never the change it names.
	if Cause("edit-before-red", "Edit a.go changes code of c1 before its RED, the first failing `jhon executions run c1` (line 9): x") !=
		Cause("edit-before-red", "Write b/c.go changes code of other-change before its RED, the first failing `jhon executions run other-change` (line 40): x") {
		t.Fatal("edit-before-red: the change name reached the cause")
	}
}

func TestGroupDraftsKeepsGroupsOfAtLeastMin(t *testing.T) {
	msg := "Edit src/app.go changes code of c1 before its RED, the first failing `jhon executions run c1` (line 9): rule"
	results := []Audited{
		audited("/s/one.jsonl", "pi", draftFinding("edit-before-red", "one.jsonl", 9, "2026-09-30T10:00:00.000Z", msg)),
		audited("/s/two.jsonl", "pi", draftFinding("edit-before-red", "two.jsonl", 40, "2026-10-01T10:00:00.000Z", strings.ReplaceAll(msg, "c1", "c2"))),
		audited("/s/three.jsonl", "claude-code", draftFinding("edit-before-red", "three.jsonl", 3, "2026-10-02T10:00:00.000Z", msg)),
	}
	gs := GroupDrafts(results, 2)
	if len(gs) != 1 || gs[0].Runtime != "pi" || len(gs[0].Items) != 2 || gs[0].Sessions != 2 {
		t.Fatalf("min 2: %+v", gs)
	}
	if g := gs[0]; g.First != "2026-09-30T10:00:00.000Z" || g.Last != "2026-10-01T10:00:00.000Z" {
		t.Errorf("first/last: %s %s", g.First, g.Last)
	}
	if gs := GroupDrafts(results, 1); len(gs) != 2 {
		t.Fatalf("min 1: want 2 groups, got %d", len(gs))
	}
	if gs := GroupDrafts(results, 3); len(gs) != 0 {
		t.Fatalf("min 3: want none, got %d", len(gs))
	}
}

// pi-only → shell; Claude Code or both → core; audit-error or indeterminate → dashboard.
func TestSuspectedOwnerByRule(t *testing.T) {
	for _, c := range []struct {
		check    string
		runtimes []string
		kinds    []string
		want     string
	}{
		{"edit-before-red", []string{"pi"}, []string{"finding"}, "shell"},
		{"edit-before-red", []string{"claude-code"}, []string{"finding"}, "core"},
		{"edit-before-red", []string{"claude-code", "pi"}, []string{"finding"}, "core"},
		{"delivered-untested", []string{"pi"}, []string{"indeterminate"}, "dashboard"},
		{"delivered-untested", []string{"claude-code"}, []string{"indeterminate", "finding"}, "core"},
		{"audit-error", []string{"pi"}, []string{"audit-error"}, "dashboard"},
	} {
		if got := SuspectedOwner(c.check, c.runtimes, c.kinds); got != c.want {
			t.Errorf("%s %v %v: %s, want %s", c.check, c.runtimes, c.kinds, got, c.want)
		}
	}
	// Repeated audit errors are a group too, owned by the dashboard.
	r := Audited{Path: "/s/x.jsonl", Result: &Result{Status: "audit-error", Errors: []AuditError{{"x.jsonl", 4, "unknown line type foo"}}}}
	r2 := Audited{Path: "/s/y.jsonl", Result: &Result{Status: "audit-error", Errors: []AuditError{{"y.jsonl", 70, "unknown line type foo"}}}}
	if gs := GroupDrafts([]Audited{r, r2}, 2); len(gs) != 1 || gs[0].Check != "audit-error" || gs[0].Owner != "dashboard" {
		t.Fatalf("audit errors: %+v", gs)
	}
}

const goldenDraft = `---
key: KEY
check: edit-before-red
anchor: ldd/red-first
severity: suspect
runtime: pi
agent_type: worker
sessions: 2
occurrences: 2
first: 2026-09-30T10:00:00.000Z
last: 2026-10-01T10:00:00.000Z
owner: shell
---

# edit-before-red: pi worker

## Finding

edit-before-red (ldd/red-first, pending-real-fixture) in pi worker sessions, 2 time(s) in 2 session(s). Cause: before-red

Example: Edit src/app.go changes code of c1 before its RED, the first failing ` + "`jhon executions run c1`" + ` (line 9): rule

## Evidence

- /s/one.jsonl:9 (2026-09-30T10:00:00.000Z, suspect) Edit src/app.go: Edit src/app.go changes code of c1 before its RED, the first failing ` + "`jhon executions run c1`" + ` (line 9): rule
- /s/sub/two.jsonl:40 (2026-10-01T10:00:00.000Z, suspect) Edit src/app.go: Edit src/app.go changes code of c2 before its RED, the first failing ` + "`jhon executions run c2`" + ` (line 9): rule

## Suspected owner

shell (by rule: pi only → shell; Claude Code or both → core; audit-error or indeterminate → dashboard)

## Proposal / owner decision

`

func goldenGroups() []DraftGroup {
	msg := "Edit src/app.go changes code of c1 before its RED, the first failing `jhon executions run c1` (line 9): rule"
	return GroupDrafts([]Audited{
		audited("/s/one.jsonl", "pi", draftFinding("edit-before-red", "one.jsonl", 9, "2026-09-30T10:00:00.000Z", msg)),
		audited("/s/two.jsonl", "pi", draftFinding("edit-before-red", "sub/two.jsonl", 40, "2026-10-01T10:00:00.000Z", strings.ReplaceAll(msg, "c1", "c2"))),
	}, 2)
}

func TestDraftMarkdownGolden(t *testing.T) {
	gs := goldenGroups()
	if len(gs) != 1 {
		t.Fatalf("groups: %+v", gs)
	}
	want := strings.Replace(goldenDraft, "KEY", gs[0].Key, 1)
	if got := gs[0].Markdown(); got != want {
		t.Errorf("draft:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteDraftsSkipsAnExistingDraftAndRefreshKeepsTheProposal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "drafts")
	gs := goldenGroups()
	name := filepath.Join(dir, "edit-before-red-"+gs[0].Key+".md")
	out, err := WriteDrafts(dir, gs, false)
	must(t, err)
	if len(out.Written) != 1 || out.Written[0] != name {
		t.Fatalf("first run: %+v", out)
	}
	// The owner writes a proposal; a second run writes nothing.
	b, _ := os.ReadFile(name)
	owned := string(b) + "Owner: split the worker's prompt.\n\n  keep   this  spacing\n"
	must(t, os.WriteFile(name, []byte(owned), 0o644))
	out, err = WriteDrafts(dir, gs, false)
	must(t, err)
	if len(out.Written)+len(out.Refreshed) != 0 || len(out.Kept) != 1 {
		t.Fatalf("second run: %+v", out)
	}
	if b, _ := os.ReadFile(name); string(b) != owned {
		t.Fatal("second run changed the draft")
	}
	// --refresh rewrites Evidence and the counts, the rest byte for byte.
	more := goldenGroups()
	more[0].Items = append(more[0].Items, DraftItem{Session: "/s/three.jsonl", Transcript: "/s/three.jsonl", Line: 7, At: "2026-10-05T10:00:00.000Z", Severity: "suspect", Evidence: "Write x.go", Message: "m"})
	more[0].Sessions, more[0].Last = 3, "2026-10-05T10:00:00.000Z"
	out, err = WriteDrafts(dir, more, true)
	must(t, err)
	if len(out.Refreshed) != 1 {
		t.Fatalf("refresh: %+v", out)
	}
	b, _ = os.ReadFile(name)
	got := string(b)
	for _, want := range []string{"sessions: 3\n", "occurrences: 3\n", "last: 2026-10-05T10:00:00.000Z\n", "- /s/three.jsonl:7 (2026-10-05T10:00:00.000Z, suspect) Write x.go: m\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("refreshed draft lacks %q:\n%s", want, got)
		}
	}
	proposal := owned[strings.Index(owned, "## Proposal / owner decision"):]
	if !strings.HasSuffix(got, proposal) {
		t.Errorf("refresh changed the proposal:\n%s", got)
	}
	finding := owned[strings.Index(owned, "# edit-before-red"):strings.Index(owned, "## Evidence")]
	if !strings.Contains(got, finding) {
		t.Errorf("refresh changed the finding:\n%s", got)
	}
}
