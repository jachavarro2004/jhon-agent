package judgment

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

func TestMain(m *testing.M) {
	root, _ := filepath.Abs("../../..")
	os.Setenv("JHON_ROOT", root)
	os.Exit(m.Run())
}

var fixtures = filepath.Join("..", "..", "..", "test", "testdata", "judgment")

var step = regexp.MustCompile(`^\d\d-.*\.json$`)

// appendRaw is appendEvent without the replay: the input's canonical bytes
// chained to the record. The reader under test judges what it builds.
func appendRaw(t *testing.T, record, input string) string {
	t.Helper()
	v, err := js.ParseJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	e := v.(*js.Object)
	e.Set("prev", sha256Hex(record))
	return record + renderEvent(e)
}

// build is the record after a case's first upto events (all when upto < 0).
func build(t *testing.T, c string, upto int) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(fixtures, c))
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		if step.MatchString(e.Name()) {
			files = append(files, e.Name())
		}
	}
	slices.Sort(files)
	if upto >= 0 {
		files = files[:upto]
	}
	record := ""
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(fixtures, c, f))
		if err != nil {
			t.Fatal(err)
		}
		record = appendRaw(t, record, string(b))
	}
	return record
}

func status(t *testing.T, record string) Status {
	t.Helper()
	s, err := StatusOf(record)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type pair [2]string

func statuses(s Status) []pair {
	var out []pair
	for _, p := range s.Points {
		out = append(out, pair{p.ID, p.Status})
	}
	return out
}

func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

func TestDefectAfterR0(t *testing.T) {
	s := status(t, build(t, "defect-close", 3))
	eq(t, "mode", *s.Mode, "defect")
	eq(t, "round", *s.Round, 0)
	eq(t, "contested", s.Contested, []string{"A1", "B1"})
	eq(t, "info", s.Info, []string{"A2"})
	eq(t, "nextAction", s.NextAction, "next-round")
	eq(t, "newProgress", s.NewProgress, (*bool)(nil))
}

func TestDefectAdoptionConfirmsThenCloses(t *testing.T) {
	s := status(t, build(t, "defect-close", 5))
	eq(t, "round", *s.Round, 1)
	eq(t, "contested", s.Contested, []string{})
	eq(t, "points", statuses(s), []pair{{"A1", "confirmed"}, {"B1", "confirmed"}})
	eq(t, "nextAction", s.NextAction, "close")
	// The fix set is exactly the confirmed severe points (docket AY B5).
	eq(t, "fix", ids(s.Fix), []string{"A1", "B1"})
	eq(t, "fix while contested", ids(status(t, build(t, "defect-close", 3)).Fix), []string{})
	s = status(t, build(t, "defect-close", -1))
	eq(t, "nextAction", s.NextAction, "closed")
	eq(t, "verdict", *s.Verdict, "APPROVED")
	if v, err := VerdictOf(build(t, "defect-close", -1)); v != "APPROVED" || err != nil {
		t.Errorf("VerdictOf = %q, %v", v, err)
	}
	if v, err := VerdictOf(build(t, "defect-close", 5)); v != "" || err != nil {
		t.Errorf("VerdictOf an open record = %q, %v", v, err)
	}
}

func TestDocketHumanDecision(t *testing.T) {
	s := status(t, build(t, "docket-human", 5))
	eq(t, "newProgress", *s.NewProgress, true)
	eq(t, "nextAction", s.NextAction, "next-round")
	s = status(t, build(t, "docket-human", 7))
	eq(t, "round", *s.Round, 2)
	eq(t, "newProgress", *s.NewProgress, false)
	eq(t, "nextAction", s.NextAction, "escalate")
	eq(t, "reason", *s.Reason, "no-progress")
	s = status(t, build(t, "docket-human", 8))
	eq(t, "points", statuses(s), []pair{{"A1", "confirmed"}, {"B1", "dropped"}})
	eq(t, "nextAction", s.NextAction, "close")
	eq(t, "a docket fixes nothing", ids(s.Fix), []string{})
	s = status(t, build(t, "docket-human", -1))
	eq(t, "nextAction", s.NextAction, "closed")
	eq(t, "verdict", *s.Verdict, "CLOSED")
}

func TestPendingBeforeR0Completes(t *testing.T) {
	s := status(t, build(t, "defect-close", 2))
	eq(t, "round", s.Round, (*int)(nil))
	eq(t, "pending", s.Pending, []string{"R0.B"})
	eq(t, "nextAction", s.NextAction, "next-round")
}

func TestProgressAndTermination(t *testing.T) {
	s := status(t, build(t, "progress-concession", -1))
	eq(t, "concession progress", *s.NewProgress, true)
	eq(t, "A1", s.Points[slices.IndexFunc(s.Points, func(p Point) bool { return p.ID == "A1" })].Status, "dropped")
	eq(t, "contested", s.Contested, []string{"B1"})
	eq(t, "nextAction", s.NextAction, "next-round")

	s = status(t, build(t, "progress-new-proof", -1))
	eq(t, "new proof progress", *s.NewProgress, true)
	eq(t, "nextAction", s.NextAction, "next-round")

	s = status(t, build(t, "no-progress", -1))
	eq(t, "repeated proof progress", *s.NewProgress, false)
	eq(t, "nextAction", s.NextAction, "escalate")
	eq(t, "reason", *s.Reason, "no-progress")

	s = status(t, build(t, "round-limit", -1))
	eq(t, "round", *s.Round, 2)
	eq(t, "newProgress", *s.NewProgress, true)
	eq(t, "nextAction", s.NextAction, "escalate")
	eq(t, "reason", *s.Reason, "round-limit")
}

func TestEmptyRecord(t *testing.T) {
	s := status(t, "")
	eq(t, "status", s, Status{Pending: []string{}, Points: []Point{}, Fix: []Point{}, Info: []string{}, Contested: []string{}, Closed: []string{}, NextAction: "next-round"})
}

// refused is the refusal StatusOf gives, which must match why.
func refused(t *testing.T, record string, why string) {
	t.Helper()
	_, err := StatusOf(record)
	var r *Refusal
	if !errors.As(err, &r) || !regexp.MustCompile(why).MatchString(r.Msg) {
		t.Errorf("StatusOf = %v, want a refusal matching %q", err, why)
	}
}

func TestRefusesAModifiedRecord(t *testing.T) {
	record := build(t, "refuse-modify-prior", -1)
	status(t, record)
	tampered := strings.Replace(record, "evict() drops the newest entry", "evict() drops the oldest entry", 1)
	if tampered == record {
		t.Fatal("the fixture no longer holds the text to tamper with")
	}
	refused(t, tampered, `^R0\.B at byte \d+: prev does not match`)
}

func TestRefusesNonCanonicalBytes(t *testing.T) {
	record := build(t, "defect-close", 1)
	refused(t, record+"trailing", `^record is not an event log at byte `+strconv.Itoa(len(record))+`: expected '## <event>' followed by one json block$`)
	refused(t, strings.Replace(record, `"mode": "defect"`, `"mode":"defect"`, 1), `^OPEN at byte 0 is not in canonical form; the record was edited by hand$`)
	refused(t, strings.Replace(record, "## OPEN", "## SHUT", 1), `^heading SHUT at byte 0 names a different event than its body \(OPEN\)`)
	refused(t, strings.Replace(record, `"mode": "defect"`, `"mode": defect`, 1), `^event OPEN at byte 0 is not valid JSON`)

	// Keys out of their canonical order are an edit too.
	reordered := "## OPEN\n\n```json\n{\n  \"prev\": \"" + sha256Hex("") + "\",\n  \"event\": \"OPEN\",\n  \"mode\": \"defect\",\n  \"target\": \"x\"\n}\n```\n\n"
	refused(t, reordered, `^OPEN at byte 0 is not in canonical form`)

	// A body that is null fails the read as node does, without a refusal.
	_, err := StatusOf("## OPEN\n\n```json\nnull\n```\n\n")
	var r *Refusal
	if err == nil || errors.As(err, &r) || err.Error() != "Cannot read properties of null (reading 'event')" {
		t.Errorf("null body: %v", err)
	}
}

func TestOffsetsCountUTF16Units(t *testing.T) {
	record := appendRaw(t, "", `{"event": "OPEN", "mode": "defect", "target": "café 😀"}`)
	// One rune of the record, the emoji, is two UTF-16 units.
	refused(t, record+"x", `at byte `+strconv.Itoa(len([]rune(record))+1)+`:`)
}

func TestRefusesWhatTheContractForbids(t *testing.T) {
	read := func(c, f string) string {
		b, err := os.ReadFile(filepath.Join(fixtures, c, f))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, c := range []struct{ name, why string }{
		{"refuse-r1-before-r0", `R1\.A.*R0\.B`},
		{"refuse-r0-twice", `R0\.A.*already`},
		{"refuse-after-close", `closed`},
		{"refuse-close-wrong-mode", `CLOSED.*defect`},
		{"refuse-adopt-without-proof", `adopt.*A1.*own proof`},
		{"refuse-judge-mismatch", `R0\.A.*judge A.*B`},
	} {
		refused(t, appendRaw(t, build(t, c.name, -1), read(c.name, "refused.json")), c.why)
	}
	refused(t, appendRaw(t, build(t, "refuse-adopt-without-proof", -1), read("refuse-adopt-without-proof", "refused-copied.json")), `adopt.*A1.*own proof`)
	r2a := read("round-limit", "06-r2a.json")
	refused(t, appendRaw(t, build(t, "no-progress", -1), r2a), `no progress`)
	refused(t, appendRaw(t, build(t, "round-limit", -1), strings.ReplaceAll(r2a, "R2.A", "R3.A")), `limit`)
	decide := `{"event": "HUMAN_DECISION", "decisions": [{"point": "A1", "outcome": "dropped", "rationale": "x"}]}`
	refused(t, appendRaw(t, build(t, "defect-close", 5), decide), `A1.*not contested`)
	r3a := strings.ReplaceAll(read("docket-human", "06-r2a.json"), "R2.A", "R3.A")
	refused(t, appendRaw(t, build(t, "docket-human", 8), r3a), `human decision`)
	open := strings.Replace(read("docket-human", "01-open.json"), `"removal_option": "remove"`, `"removal_option": "rewrite"`, 1)
	if open == read("docket-human", "01-open.json") {
		t.Fatal("the docket OPEN fixture no longer names removal_option remove")
	}
	refused(t, appendRaw(t, "", open), `removal_option`)
}

func ids(ps []Point) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}
