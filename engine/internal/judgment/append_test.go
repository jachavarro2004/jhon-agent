package judgment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func appended(t *testing.T, record, input string) string {
	t.Helper()
	out, err := AppendEvent(record, input)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func refusesAppend(t *testing.T, record, input, why string) {
	t.Helper()
	_, err := AppendEvent(record, input)
	var r *Refusal
	if !errors.As(err, &r) || !regexp.MustCompile(why).MatchString(r.Msg) {
		t.Errorf("AppendEvent: %v, want a refusal matching %s", err, why)
	}
}

func fixture(t *testing.T, c, f string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtures, c, f))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// steps is a case's event files, in name order.
func steps(t *testing.T, c string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(fixtures, c))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if step.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestAppendChainsCanonicalBlocks(t *testing.T) {
	// An independent reader: encoding/json and crypto/sha256, not parseRecord.
	block := regexp.MustCompile("^## (\\S+)\n\n```json\n((?s:.)*?)\n```\n\n")
	record := ""
	files := steps(t, "defect-close")
	for _, f := range files {
		record = appended(t, record, fixture(t, "defect-close", f))
	}
	at := 0
	for i, f := range files {
		m := block.FindStringSubmatch(record[at:])
		if m == nil {
			t.Fatalf("no event block at byte %d", at)
		}
		var got, want map[string]any
		if err := json.Unmarshal([]byte(m[2]), &got); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(record[:at]))
		if got["event"] != m[1] || got["prev"] != hex.EncodeToString(sum[:]) {
			t.Errorf("event %d: heading %s, event %v, prev %v", i, m[1], got["event"], got["prev"])
		}
		delete(got, "prev")
		json.Unmarshal([]byte(fixture(t, "defect-close", f)), &want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("event %d = %v, want %v", i, got, want)
		}
		if !strings.HasPrefix(m[2], "{\n  \"event\": ") {
			t.Errorf("event %d is not two-space-indented with event first", i)
		}
		at += len(m[0])
	}
	if at != len(record) {
		t.Errorf("%d trailing bytes", len(record)-at)
	}
}

func TestAppendExtendsTheRecord(t *testing.T) {
	before := build(t, "defect-close", 3)
	next := appended(t, before, fixture(t, "defect-close", "04-r1a.json"))
	if !strings.HasPrefix(next, before) || len(next) <= len(before) {
		t.Error("the new record does not extend the old one")
	}
}

func TestAppendRefusesBadInput(t *testing.T) {
	record := build(t, "defect-close", 3)
	r1a := fixture(t, "refuse-modify-prior", "refused.json")
	refusesAppend(t, record, "{", `^the event is not valid JSON$`)
	refusesAppend(t, record, "["+r1a+","+r1a+"]", `exactly one event`)
	refusesAppend(t, record, `{"event": 1}`, `exactly one event`)
	refusesAppend(t, record, "null", `exactly one event`)
	stale := strings.Replace(r1a, "{", `{"prev": "`+strings.Repeat("0", 64)+`",`, 1)
	refusesAppend(t, record, stale, `^R1\.A: prev does not match the record's current bytes`)
	// A prev that is the record's tip is accepted.
	appended(t, record, strings.Replace(r1a, "{", `{"prev": "`+sha256Hex(record)+`",`, 1))
}

func TestAppendRefusesWhatTheReplayRefuses(t *testing.T) {
	record := build(t, "refuse-modify-prior", -1)
	appended(t, record, fixture(t, "refuse-modify-prior", "refused.json"))
	tampered := strings.Replace(record, "evict() drops the newest entry", "evict() drops the oldest entry", 1)
	refusesAppend(t, tampered, fixture(t, "refuse-modify-prior", "refused.json"), `R0\.B.*prev`)
	refusesAppend(t, build(t, "refuse-adopt-without-proof", -1), fixture(t, "refuse-adopt-without-proof", "refused.json"), `adopt.*A1.*own proof`)
	// The record's only contested-point check runs on the appended event too.
	var r1a map[string]any
	json.Unmarshal([]byte(fixture(t, "defect-close", "04-r1a.json")), &r1a)
	r1a["positions"] = r1a["positions"].([]any)[:1]
	b, _ := json.Marshal(r1a)
	refusesAppend(t, build(t, "defect-close", 3), string(b), `R1\.A.*B1`)
}

func TestAppendRefusesWhatTheContractForbids(t *testing.T) {
	for _, c := range []struct{ name, file, why string }{
		{"refuse-r1-before-r0", "refused.json", `R1\.A.*R0\.B`},
		{"refuse-r0-twice", "refused.json", `R0\.A.*already`},
		{"refuse-after-close", "refused.json", `closed`},
		{"refuse-close-wrong-mode", "refused.json", `CLOSED.*defect`},
		{"refuse-adopt-without-proof", "refused-copied.json", `adopt.*A1.*own proof`},
		{"refuse-judge-mismatch", "refused.json", `R0\.A.*judge A.*B`},
	} {
		refusesAppend(t, build(t, c.name, -1), fixture(t, c.name, c.file), c.why)
	}
	r2a := fixture(t, "round-limit", "06-r2a.json")
	refusesAppend(t, build(t, "no-progress", -1), r2a, `no progress`)
	refusesAppend(t, build(t, "round-limit", -1), strings.ReplaceAll(r2a, "R2.A", "R3.A"), `limit`)
	decide := `{"event": "HUMAN_DECISION", "decisions": [{"point": "A1", "outcome": "dropped", "rationale": "x"}]}`
	refusesAppend(t, build(t, "defect-close", 5), decide, `A1.*not contested`)
	r3a := strings.ReplaceAll(fixture(t, "docket-human", "06-r2a.json"), "R2.A", "R3.A")
	refusesAppend(t, build(t, "docket-human", 8), r3a, `human decision`)
	var open map[string]any
	json.Unmarshal([]byte(fixture(t, "docket-human", "01-open.json")), &open)
	open["removal_option"] = "rewrite"
	b, _ := json.Marshal(open)
	refusesAppend(t, "", string(b), `removal_option`)
}

func TestRecordLocations(t *testing.T) {
	for path, ok := range map[string]bool{
		"docs/judgments/2026-09-22-cache-review.md":                       true,
		"/x/openspec/changes/add-cache/judgments/2026-09-22-cache.md":     true,
		"docs/judgments/2026-9-22-cache.md":                               false,
		"docs/judgments/2026-09-22-Cache.md":                              false,
		"notes/review.md":                                                 false,
		"/x/openspec/changes/a/b/judgments/2026-09-22-cache.md":           false,
		"docs/judgments/../judgments/2026-09-22-cache-review.md":          true,
		"docs/judgments/2026-09-22-cache-review.md/../other.md":           false,
		"/x/docs/judgments/2026-09-22-cache-review.md.bak":                false,
		"/x/mydocs/judgments/2026-09-22-cache-review.md":                  false,
		"/x/openspec/changes/add-cache/judgments/2026-09-22--cache.md":    false,
		"/x/openspec/changes/add-cache/judgments/2026-09-22-0-cache-2.md": true,
	} {
		if got := RecordLocationOK(path); got != ok {
			t.Errorf("RecordLocationOK(%s) = %v, want %v", path, got, ok)
		}
	}
}

func TestAppendFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docs", "judgments", "2026-09-22-cache-review.md")
	for _, f := range steps(t, "refuse-adopt-without-proof") {
		if err := AppendFile(path, fixture(t, "refuse-adopt-without-proof", f)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	var r *Refusal
	if err := AppendFile(path, fixture(t, "refuse-adopt-without-proof", "refused.json")); !errors.As(err, &r) {
		t.Errorf("refused event: %v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("a refusal changed the record")
	}
	if err := AppendFile(path, `{"event": "HUMAN_DECISION", "decisions": [{"point": "A1", "outcome": "dropped", "rationale": "x"}]}`); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Errorf("mode %v, want the record's own 0640", info.Mode().Perm())
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("temporary file left behind: %v", entries)
	}

	outside := filepath.Join(dir, "notes", "review.md")
	err := AppendFile(outside, fixture(t, "defect-close", "01-open.json"))
	if !errors.As(err, &r) || !strings.Contains(r.Msg, "is not a record location; contracts/judgment.md allows openspec/changes/<change>/judgments/<date>-<slug>.md or docs/judgments/<date>-<slug>.md") {
		t.Errorf("outside a record location: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(outside)); err == nil {
		t.Error("a refused location was created")
	}
}
