package audit

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
)

// T220: the audit sees what the owner had to correct. S3: the default
// branch moved, by merge, fast-forward, `git fetch . x:main` or push,
// before a ready `jhon deliver check … --action merge` of that commit; a
// check made after the move is no evidence for it, in the findings and in
// what worked.

// notReady is a `jhon deliver check` that is not ready, as its human output prints it.
func notReady(commit, tree string) string {
	return "merge  not ready\nhead    " + commit + "  tree " + tree + "\ntested  none  tree none\nrisk    medium\n  - no passing execution record of HEAD's tree"
}

// readyOf is a ready `jhon deliver check` of commit and tree, as its human output prints it.
func readyOf(commit, tree string) string {
	return "merge  ready\nhead    " + commit + "  tree " + tree + "\ntested  " + commit + "  tree " + tree + "\nrisk    medium"
}

// moveMain is the owner's session's line: a check piped to tail, then main fast-forwarded by a fetch into it.
var moveMain = "jhon deliver check " + chg + " --action merge 2>&1 | tail -6; git fetch -q . fix/" + chg + ":main"

func TestMergesToDefaultReadsAFetchIntoTheDefaultBranch(t *testing.T) {
	for cmd, want := range map[string]bool{
		"git fetch -q . fix/ledger:main": true,
		moveMain:                         true,
		"git fetch . +refs/heads/fix/ledger:refs/heads/master":        true,
		"git -C /work/p fetch . fix/ledger:main":                      true,
		"git fetch origin main:main":                                  false, // a sync of the default branch
		"git fetch origin refs/heads/main:refs/heads/main":            false,
		"git fetch origin":                                            false,
		"git fetch . fix/ledger:fix/other":                            false,
		"git fetch --dry-run . fix/ledger:main":                       false,
		"echo 'git fetch . fix/ledger:main' >> notes.md":              false,
		"git checkout main && git merge --ff-only fix/ledger":         true,
		"git push . fix/ledger:main":                                  false, // a push: DeliveryCommand's
		"git merge-base HEAD main && git fetch -q origin main:main":   false,
		"git fetch -q . fix/ledger:main && jhon deliver check ledger": true,
	} {
		eq(t, cmd, MergesToDefault(cmd), want)
	}
	eq(t, "a push into main through the local repository", DeliveryCommand("git push . fix/ledger:main"), "push")
}

func TestDeliveredUntestedReadsTheDefaultBranchMovedBeforeAReadyCheck(t *testing.T) {
	task := []pstep{puser("Arranca el cambio de ledger.ts."), pmain(pbash("jhon status "+chg, "change "+chg))}
	recheck := pmain(pbash("jhon deliver check "+chg+" --action merge", readyOf(c1, t1)))
	// The owner's session: the check in the same line was not ready, and the ready re-check came after main moved.
	moved := piAudit(t, append(task, pmain(pbash(moveMain, notReady(c1, t1))), recheck)...)
	fs := of(moved.Findings, "delivered-untested")
	violates(t, "main moved before a ready check", fs)
	if len(fs) == 1 {
		if !strings.Contains(fs[0].Evidence, "git fetch -q . fix/"+chg+":main") || !strings.Contains(fs[0].Message, chg+" (merge)") {
			t.Errorf("the finding names the move and the change: %+v", fs[0])
		}
		if !strings.Contains(fs[0].Message, "the ready check at ") || !strings.Contains(fs[0].Message, "came after the move: a check made after it is no evidence for it") {
			t.Errorf("the finding says the later check is no evidence: %s", fs[0].Message)
		}
	}
	// A passing run and no ready check before the move: still a finding, a suspect.
	run := pmain(pbash("jhon executions run "+chg, executionsOut(c1, t1, []string{"pass"})))
	if fs := of(piAudit(t, append(task, run, pmain(pbash(moveMain, notReady(c1, t1))), recheck)...).Findings, "delivered-untested"); len(fs) != 1 || fs[0].Severity != "suspect" {
		t.Errorf("a passing run, no ready check before the move: %+v", fs)
	}
	// A ready check before the move is its evidence.
	before := piAudit(t, append(task, pmain(pbash("jhon deliver check "+chg+" --action merge", readyOf(c1, t1))), pmain(pbash("git fetch -q . fix/"+chg+":main", "")))...)
	eq(t, "a ready check before the move", len(of(before.Findings, "delivered-untested")), 0)
	// The same on Claude Code: a fetch into main from the main thread.
	if fs := findingsOf(t, "delivered-untested", user("go"), mainBash("jhon status "+chg), mainBash(moveMain, notReady(c1, t1)),
		mainBash("jhon deliver check "+chg+" --action merge", readyOf(c1, t1))); len(fs) != 1 || fs[0].Severity != "violation" {
		t.Errorf("claude code: %+v", fs)
	}
}

// What worked cites a ready check as the evidence of a merge only when it came before the move.
func TestWorkedDeliveryNeverCitesACheckMadeAfterTheMove(t *testing.T) {
	task := []pstep{puser("Arranca el cambio de ledger.ts."), pmain(pbash("jhon status "+chg, "change "+chg))}
	delivery := func(r *Result) []Worked {
		var out []Worked
		for _, w := range r.Worked {
			if w.Kind == "delivery" && w.Change == chg {
				out = append(out, w)
			}
		}
		return out
	}
	// The owner's session: no ready check before main moved; the re-check after it is no evidence.
	moved := piAudit(t, append(task, pmain(pbash(moveMain, notReady(c1, t1))), pmain(pbash("jhon deliver check "+chg+" --action merge", readyOf(cb, tb))))...)
	eq(t, "no delivery worked", len(delivery(moved)), 0)
	// A ready check before the move is cited, not the one after it.
	ok := piAudit(t, append(task, pmain(pbash("jhon deliver check "+chg+" --action merge", readyOf(c1, t1))), pmain(pbash("git fetch -q . fix/"+chg+":main", "")),
		pmain(pbash("jhon deliver check "+chg+" --action merge", readyOf(cb, tb))))...)
	if ws := delivery(ok); len(ws) != 1 || !strings.Contains(ws[0].Evidence, " on "+c1[:8]) || ws[0].What != "merge ready" {
		t.Errorf("the check before the move: %+v", ws)
	}
	// With no move in the session, the last ready check stands, as before.
	alone := piAudit(t, append(task, pmain(pbash("jhon deliver check "+chg+" --action merge", readyOf(cb, tb))))...)
	if ws := delivery(alone); len(ws) != 1 || !strings.Contains(ws[0].Evidence, " on "+cb[:8]) {
		t.Errorf("no move: %+v", ws)
	}
}

// T220 S2: inline-not-small (delegation/not-small-inline). After `jhon
// status` (or the shell's full-context message after a change moment)
// called the change not small, the main thread's own edit of a source file,
// not its record, is a suspect naming the change and that first edit.

// notSmall is `jhon status <change>`'s human output for a change that is not small.
func notSmall(change string) string {
	return "change      " + change + "  (branch fix/" + change + ")\nnext        apply  (suggestion)\nnote        not small: read ~/.local/share/jhon/current/prose/full-pi.md before the next edit"
}

func TestInlineNotSmall(t *testing.T) {
	status := pmain(pbash("jhon status "+chg, notSmall(chg)))
	src := testRoot + "/src/ledger.ts"
	fs := piFindings(t, "inline-not-small", puser("Implementa el resumen mensual."), status,
		pmain(pwrite(testRoot+"/ldd/changes/"+chg+"/tasks.md")), pmain(pedit("src/ledger.ts")), pmain(pedit("src/report.ts")))
	if len(fs) != 1 {
		t.Fatalf("one finding for the change, at its first source edit: %+v", fs)
	}
	f := fs[0]
	eq(t, "severity", f.Severity, "suspect")
	eq(t, "kind", f.Kind, "finding")
	eq(t, "status", f.ValidationStatus, "heuristic")
	eq(t, "anchor", f.Anchor, "delegation/not-small-inline")
	eq(t, "agent", f.Agent+" "+f.AgentType, "main main")
	eq(t, "evidence", f.Evidence, src)
	for _, says := range []string{chg, src, "after jhon status called it not small", "jhon-builder", "contracts/ldd.md"} {
		if !strings.Contains(f.Message, says) {
			t.Errorf("the message does not say %q: %s", says, f.Message)
		}
	}
	// The finding's line is the edit's call.
	p := piSessionWith(t, puser("go"), status, pmain(pedit("src/ledger.ts")))
	if r := Audit(p); len(of(r.Findings, "inline-not-small")) != 1 || of(r.Findings, "inline-not-small")[0].Line != lineOf(t, p, `"name":"edit"`) {
		t.Errorf("the line of the edit: %+v", r.Findings)
	}

	for _, c := range []struct {
		what  string
		steps []pstep
	}{
		{"only its record", []pstep{puser("go"), status, pmain(pwrite(testRoot + "/ldd/changes/" + chg + "/tasks.md")), pmain(pwrite(testRoot + "/openspec/changes/archive/2026-11-23-" + chg + "/notes.md"))}},
		{"an edit before status said so", []pstep{puser("go"), pmain(pedit("src/ledger.ts")), status}},
		{"a small change", []pstep{puser("go"), pmain(pbash("jhon status "+chg, "change      "+chg+"\nnote        small change (no folder, one component, under the advisory size, not high risk)")), pmain(pedit("src/ledger.ts"))}},
		{"a builder's edit", []pstep{puser("go"), status, psub("jhon-builder", "Apply slice 1.\nchange: "+chg+"\n\n## Allowed edit surfaces\n- src/ledger.ts", "done", pedit("src/ledger.ts"))}},
		{"a refused edit", []pstep{puser("go"), status, pmain(pcall{name: "edit", args: obj("path", src, "edits", []any{obj("oldText", "a", "newText", "b")}), result: "Could not find the text", isError: true})}},
		{"the full context after a sub-agent launch", []pstep{puser("go"), psub("jhon-scout", "Explore.\nchange: "+chg, "found"), {kind: "full"}, pmain(pedit("src/ledger.ts"))}},
	} {
		eq(t, c.what, len(piFindings(t, "inline-not-small", c.steps...)), 0)
	}

	// The shell's full-context message after a change moment (no sub-agent) says the change is not small.
	moment := piFindings(t, "inline-not-small", puser("go"), pmain(pbash("git switch -c fix/"+chg, "Switched")), pmain(pbash("jhon executions run "+chg, "fail  core")), pstep{kind: "full"}, pmain(pedit("src/ledger.ts")))
	if len(moment) != 1 || !strings.Contains(moment[0].Message, chg) || !strings.Contains(moment[0].Message, "after the shell loaded the full context") {
		t.Errorf("after the shell's full context: %+v", moment)
	}
	// `jhon status` with no operand names the change in its first line.
	if fs := piFindings(t, "inline-not-small", puser("go"), pmain(pbash("jhon status", notSmall("other-change"))), pmain(pedit("src/ledger.ts"))); len(fs) != 1 || !strings.Contains(fs[0].Message, "other-change") {
		t.Errorf("status with no operand: %+v", fs)
	}
	// Claude Code: the same from a Bash status and an Edit.
	edit := step{kind: "tool", tool: "Edit", input: obj("file_path", src, "old_string", "a", "new_string", "b")}
	if fs := findingsOf(t, "inline-not-small", user("go"), mainBash("jhon status "+chg, notSmall(chg)), edit); len(fs) != 1 || fs[0].Severity != "suspect" {
		t.Errorf("claude code: %+v", fs)
	}
}

// The rule the finding cites is contracts/ldd.md's apply row: the
// jhon-builder agent applies a change, inline only a small one. Matched on
// its stable words, not the exact sentence.
func TestInlineNotSmallCitesTheApplyRow(t *testing.T) {
	var row string
	for _, l := range strings.Split(contract.MustContract("ldd.md"), "\n") {
		if strings.HasPrefix(l, "| apply |") {
			row = l
		}
	}
	if !strings.Contains(row, "`jhon-builder`") || !strings.Contains(row, "inline") || !strings.Contains(row, "small") {
		t.Errorf("contracts/ldd.md's apply row no longer names the jhon-builder and inline for small work: %q", row)
	}
}

// inline-not-small's draft cause is how the main thread was told, never the change or the file it names.
func TestInlineNotSmallCauseIsItsForm(t *testing.T) {
	a := Cause("inline-not-small", "main main edits /work/a/src/x.ts of c1 itself after jhon status called it not small: y")
	b := Cause("inline-not-small", "main main edits README of other-change itself after jhon status called it not small: y")
	eq(t, "status form", a, b)
	eq(t, "the form", a, "after-status")
	eq(t, "the full-context form", Cause("inline-not-small", "main main edits x of c1 itself after the shell loaded the full context for it: y"), "after-full-context")
}

// T220 L6 B2: the not-small state belongs to one change. A later status of
// another change moves it there (not small) or ends it (small); a
// jhon-builder or jhon-checker launched for the change ends it; the owner's
// request naming another change ends it; and no change's record files ever fire.
func TestInlineNotSmallFollowsTheCurrentChange(t *testing.T) {
	const other = "fix-export"
	status := pmain(pbash("jhon status "+chg, notSmall(chg)))
	edit := pmain(pedit("src/ledger.ts"))
	small := pmain(pbash("jhon status "+other, "change      "+other+"\nnote        small change (no folder, one component, under the advisory size, not high risk)"))
	for _, c := range []struct {
		what  string
		steps []pstep
	}{
		{"another change's status calls it small", []pstep{puser("go"), status, small, edit}},
		{"the same change's status calls it small", []pstep{puser("go"), status, pmain(pbash("jhon status "+chg, "change      "+chg+"\nnote        small change (no folder)")), edit}},
		{"any change's record files", []pstep{puser("go"), status, pmain(pwrite(testRoot + "/ldd/changes/" + other + "/tasks.md")),
			pmain(pwrite(testRoot + "/openspec/changes/archive/2026-11-23-" + other + "/notes.md")), pmain(pwrite(testRoot + "/ldd/changes/archive/2026-11-20-old/design.md"))}},
		{"a builder launched for the change", []pstep{puser("go"), status, psub("jhon-builder", "Apply slice 1.\nchange: "+chg+"\n\n## Allowed edit surfaces\n- src/ledger.ts", "done"), edit}},
		{"a checker launched for the change", []pstep{puser("go"), status, psub("jhon-checker", "Verify.\nchange: "+chg, "verdict: pass"), edit}},
		{"the owner's request names another change", []pstep{puser("go"), status, preply("Listo."), puser("Pasemos a " + other + ": agrega el export."), edit,
			pmain(pbash("jhon executions run "+other, "fail  core"))}},
	} {
		eq(t, c.what, len(piFindings(t, "inline-not-small", c.steps...)), 0)
	}
	// Another change's status that is not small moves the state there.
	moved := piFindings(t, "inline-not-small", puser("go"), status, pmain(pbash("jhon status "+other, notSmall(other))), edit)
	if len(moved) != 1 || !strings.Contains(moved[0].Message, " of "+other+" itself") {
		t.Errorf("moved to the other change: %+v", moved)
	}
	// A builder for another change, or an owner message naming none, leaves it.
	kept := piFindings(t, "inline-not-small", puser("go"), status, psub("jhon-builder", "Apply.\nchange: "+other, "done"), preply("Listo."), puser("Recuerda delegar."), edit)
	if len(kept) != 1 || !strings.Contains(kept[0].Message, " of "+chg+" itself") {
		t.Errorf("kept on the change: %+v", kept)
	}
}

// T220 L6: `git fetch . HEAD:master` moves the default branch too.
func TestFetchHeadIntoMasterBeforeReadyCheck(t *testing.T) {
	eq(t, "MergesToDefault", MergesToDefault("git fetch . HEAD:master"), true)
	fs := piFindings(t, "delivered-untested", puser("go"), pmain(pbash("jhon status "+chg, "change "+chg)), pmain(pbash("git fetch . HEAD:master", "")),
		pmain(pbash("jhon deliver check "+chg+" --action merge", readyOf(c1, t1))))
	violates(t, "master moved before a ready check", fs)
	if len(fs) == 1 && !strings.Contains(fs[0].Message, "came after the move") {
		t.Errorf("the later check is no evidence: %s", fs[0].Message)
	}
}

// T220 L8 A1 (verify round 2): the owner's message naming a change the
// session never names otherwise does not end the not-small state. Kept as a
// documented limit, not a fix: a word is known to be a change only when a
// command, a path or a launch in the session names it; any other word may be
// product text, and reading it as a change would end the state on ordinary
// requests.
func TestInlineNotSmallOwnerNamesUnseenChange(t *testing.T) {
	t.Skip("inherent limit (T220 L8 A1): a change no command, path or launch names cannot be told from product text")
	fs := piFindings(t, "inline-not-small", puser("go"), pmain(pbash("jhon status "+chg, notSmall(chg))), preply("Listo."),
		puser("Pasemos a fix-unseen: agrega el export."), pmain(pedit("src/ledger.ts")))
	eq(t, "the owner named another change", len(fs), 0)
}
