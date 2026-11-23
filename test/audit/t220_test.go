package audit

// T220: `jhon audit` on a synthetic pi session shaped like the owner's
// (test/testdata/audit/pi-owner-inline-merge, no private content): three
// Spanish messages that remind the agent of the method (S1, moved to a later
// task: corrections count as before, so none of them is one), the main
// agent writing the change's code itself after `jhon status` called it not
// small (S2), main moved by `git fetch . fix/x:main` while the check in the
// same line was not ready, then a ready re-check (S3), and the page's view
// of the findings (S4).

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditSeesWhatTheOwnerHadToCorrect(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, ".cache"), "XDG_DATA_HOME=" + filepath.Join(home, ".local", "share")}
	main := mainOf(t, "pi-owner-inline-merge")
	moved := "jhon deliver check monthly-summary --action merge 2>&1 | tail -6; git fetch -q . fix/monthly-summary:main"
	firstEdit := "/work/ledger/test/ledger.test.ts"

	r := audit(t, []string{main, "--json"}, env...)
	equal(t, r.Code, 0, r.Stderr)
	j := parse(t, r.Stdout)
	// S1 moved to a later task (L11): the corrections are counted as before, with no rule kind.
	equal(t, strings.TrimSpace(js(t, get(j, "dialogue", "corrections"))),
		compactJSON(t, `{"owner_messages":5,"correcting":0,"keep_going":0,"repeat":0,"unrequested":0,"decision":0}`))
	equal(t, j["status"], any("findings"))
	fs := get(j, "findings").([]any)
	equal(t, len(fs), 2, r.Stdout)
	// S2: the main agent wrote the change's code itself after status called it not small; its first edit is named.
	equal(t, get(fs[0], "check"), any("inline-not-small"))
	equal(t, get(fs[0], "severity"), any("suspect"))
	equal(t, get(fs[0], "validationStatus"), any("heuristic"))
	equal(t, get(fs[0], "evidence"), any(firstEdit))
	match(t, get(fs[0], "message").(string), `^main main edits /work/ledger/test/ledger\.test\.ts of monthly-summary itself after jhon status called it not small: the `+"`jhon-builder`"+` agent applies a change that is not small, inline only a small one \(contracts/ldd\.md, the apply row\)$`)
	// S3: main moved before a ready check; the re-check after it is no evidence.
	equal(t, get(fs[1], "check"), any("delivered-untested"))
	equal(t, get(fs[1], "evidence"), any(moved))
	match(t, get(fs[1], "message").(string), `delivers monthly-summary \(merge\) .*; the ready check at 2026-11-23T21:30:35\.000Z came after the move: a check made after it is no evidence for it`)
	for _, w := range get(j, "worked").([]any) {
		if get(w, "kind") == "delivery" {
			t.Errorf("what worked cites a check made after the move: %v", w)
		}
	}

	human := audit(t, []string{main}, env...)
	equal(t, human.Code, 0, human.Stderr)
	for _, want := range []string{
		"\n  dialogue  corrections 0 of 5 owner messages  keep-going 0  repeat 0  unrequested 0  decision 0\n",
		"  suspect    inline-not-small [heuristic]",
		"\n             evidence: " + firstEdit + "\n",
		"\n             evidence: " + moved + "\n",
	} {
		if !strings.Contains(human.Stdout, want) {
			t.Errorf("the human output misses %q:\n%s", want, human.Stdout)
		}
	}
	noMatch(t, human.Stdout, `(?m)^  worked  delivery`)
	equal(t, lastLine(human.Stdout), "0 violation(s), 2 suspect(s)")

	// S4: the page's view carries both findings as it carries any other: each check's title and words, then the item.
	view := audit(t, []string{main, "--view"}, env...)
	equal(t, view.Code, 0, view.Stderr)
	groups := map[string]any{}
	for _, g := range get(parse(t, view.Stdout), "view", "findings").([]any) {
		groups[get(g, "check").(string)] = g
	}
	equal(t, len(groups), 2, view.Stdout)
	inline, untested := groups["inline-not-small"], groups["delivered-untested"]
	equal(t, get(inline, "title"), any("Not-small change built inline"))
	match(t, get(inline, "explain").(string), `jhon status called the change not small`)
	equal(t, get(inline, "suspect"), any(float64(1)))
	equal(t, get(inline, "items", 0, "evidence"), any(firstEdit))
	match(t, get(inline, "items", 0, "where").(string), `^2026-11-23T21-30-00-000Z_01a1a220-0000-7000-8000-000000000220\.jsonl:\d+$`)
	equal(t, get(untested, "title"), any("Delivered untested"))
	match(t, get(untested, "explain").(string), `a check made after it does not count`)
	equal(t, get(untested, "items", 0, "evidence"), any(moved))
	match(t, get(untested, "items", 0, "message").(string), `came after the move: a check made after it is no evidence for it`)
}
