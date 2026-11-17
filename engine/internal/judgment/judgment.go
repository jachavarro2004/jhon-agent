// Package judgment applies contracts/judgment.md to a judgment record:
// an append-only event log in which two judges
// deliberate and the human decides what they could not. Whether
// deliberation made progress, when it must stop, and whether an event may
// be appended are computed here, from the record alone, so the same bytes
// always give the same answer.
//
// One replay serves both commands: status replays the record and reports
// the state, append replays it and then asks the same state whether the new
// event is allowed. The reading side is StatusOf (and VerdictOf, for `jhon
// deliver`); the writing side, AppendEvent and AppendFile (append.go).
//
// Every value (modes, verdicts, round limits, positions, statuses, next
// actions, record locations) is read from the contract by position or by
// key, never spelled here, on first use.
package judgment

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"regexp"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// ── contract ────────────────────────────────────────────────────────────────

type vocabulary struct {
	modes, judges, events      []string
	open, human, close         string
	roundPrefix                string
	round                      *regexp.Regexp
	verdicts                   map[string][]string
	limits                     map[string]float64
	ownerPositions             []string
	opponentPositions          []string
	maintain, concede          string
	adopt, retract             string
	contested, confirmed       string
	unconfirmed, dropped       string
	humanOutcomes, severities  []string
	severe                     []string
	nextRound, escalate        string
	humanDecision, closeAction string
	closed                     string
	noProgress, roundLimit     string
	recordShapes               []string
	defect, docket             string
}

// values is a list of exactly n values (n < 0: at least one), or a loud
// failure: position carries meaning.
func values(src, key string, n int) []string {
	v := contract.List(src, key)
	if len(v) == 0 || n >= 0 && len(v) != n {
		want := "at least one"
		if n >= 0 {
			want = fmt.Sprint(n)
		}
		panic(fmt.Errorf("contracts/judgment.md: %s must declare %s value(s), found %d", key, want, len(v)))
	}
	return v
}

var vocab = sync.OnceValue(func() *vocabulary {
	src := contract.MustContract("judgment.md")
	v := &vocabulary{}
	v.modes = values(src, "judgment_modes", -1)
	v.judges = values(src, "judgment_judges", 2)
	v.events = values(src, "judgment_events", -1)
	v.open, v.human, v.close = v.events[0], v.events[max(len(v.events)-2, 0)], v.events[len(v.events)-1]
	// `R<n>.A` in the contract: the round-event template, with `<n>` the round number.
	template := ""
	for _, e := range v.events {
		if strings.Contains(e, "<n>") {
			template = e
			break
		}
	}
	if template == "" {
		panic(fmt.Errorf("contracts/judgment.md: judgment_events declares no R<n> template"))
	}
	v.roundPrefix = template[:strings.Index(template, "<n>")]
	v.round = regexp.MustCompile(`^` + regexp.QuoteMeta(v.roundPrefix) + `([0-9]+)\.(` + js.Dot + `+)$`)
	v.verdicts = map[string][]string{}
	_, verdicts := contract.Map(src, "judgment_verdicts")
	for m, list := range verdicts {
		list = strings.TrimSuffix(strings.TrimPrefix(list, "["), "]")
		var items []string
		for _, s := range strings.Split(list, ",") {
			items = append(items, js.Trim(s))
		}
		v.verdicts[m] = items
	}
	v.limits = map[string]float64{}
	_, limits := contract.Map(src, "judgment_round_limit")
	for m, n := range limits {
		v.limits[m] = js.Number(n)
	}
	for _, m := range v.modes {
		if len(v.verdicts[m]) != 2 {
			panic(fmt.Errorf("contracts/judgment.md: judgment_verdicts.%s must hold two verdicts", m))
		}
		if l, ok := v.limits[m]; !ok || !(l >= 1) {
			panic(fmt.Errorf("contracts/judgment.md: judgment_round_limit.%s must be a positive integer", m))
		}
	}
	v.ownerPositions = values(src, "judgment_owner_positions", 2)
	v.maintain, v.concede = v.ownerPositions[0], v.ownerPositions[1]
	v.opponentPositions = values(src, "judgment_opponent_positions", 3)
	v.adopt, v.retract = v.opponentPositions[1], v.opponentPositions[2]
	status := values(src, "judgment_point_status", 4)
	v.contested, v.confirmed, v.unconfirmed, v.dropped = status[0], status[1], status[2], status[3]
	v.humanOutcomes = values(src, "judgment_human_outcomes", -1)
	v.severities = values(src, "judgment_severities", -1)
	v.severe = values(src, "judgment_severe", -1)
	next := values(src, "judgment_next_action", 5)
	v.nextRound, v.escalate, v.humanDecision, v.closeAction, v.closed = next[0], next[1], next[2], next[3], next[4]
	reasons := values(src, "judgment_escalation_reason", 2)
	v.noProgress, v.roundLimit = reasons[0], reasons[1]
	keys, shapes := contract.Map(src, "judgment_record")
	for _, k := range keys {
		v.recordShapes = append(v.recordShapes, shapes[k])
	}
	// Docket mode is the one whose OPEN names options; by position, the second mode.
	v.defect = v.modes[0]
	if len(v.modes) > 1 {
		v.docket = v.modes[1]
	}
	return v
})

// ── types ───────────────────────────────────────────────────────────────────

// Point is a finding the judges deliberate on.
type Point struct {
	ID       string `json:"id"`
	Owner    string `json:"owner"`
	Status   string `json:"status"`
	Severity string `json:"severity"`
	Location string `json:"location"`
	Claim    string `json:"claim"`
}

// Status is the state of a record (JudgmentStatus).
type Status struct {
	Mode *string `json:"mode"`
	// Round is the last complete round: 0 once both R0 results are in, nil before.
	Round *int `json:"round"`
	// Pending is the events the round in progress still awaits.
	Pending []string `json:"pending"`
	Points  []Point  `json:"points"`
	// Fix is what a fix round may take: in defect mode, the confirmed
	// severe points; empty in docket mode and while none is confirmed.
	Fix []Point `json:"fix"`
	// Info is the findings that are not points: in defect mode, the non-severe ones.
	Info      []string `json:"info"`
	Contested []string `json:"contested"`
	Closed    []string `json:"closed"`
	// NewProgress is whether the last complete deliberation round made
	// progress; nil before round 1.
	NewProgress *bool   `json:"newProgress"`
	NextAction  string  `json:"nextAction"`
	Reason      *string `json:"reason"`
	Verdict     *string `json:"verdict"`
}

// Refusal is an event or a record the contract forbids
// (JudgmentRefusal). The record is never changed by one.
type Refusal struct{ Msg string }

func (r *Refusal) Error() string { return r.Msg }

// typeError is what node throws reading a property of null: an event body
// that is the JSON null. It fails the read without being a refusal.
type typeError struct{ msg string }

func (e *typeError) Error() string { return e.msg }

func refuse(format string, a ...any) { panic(&Refusal{fmt.Sprintf(format, a...)}) }

func need(cond bool, format string, a ...any) {
	if !cond {
		refuse(format, a...)
	}
}

// recovered turns a refusal or type error raised inside the replay into
// the error it returns; anything else, a broken contract included, keeps
// panicking.
func recovered(err *error) {
	switch r := recover().(type) {
	case nil:
	case *Refusal:
		*err = r
	case *typeError:
		*err = r
	default:
		panic(r)
	}
}

// ── encoding ────────────────────────────────────────────────────────────────

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// renderEvent is the canonical bytes of an event: `event` and `prev` first,
// then its fields as given.
func renderEvent(e *js.Object) string {
	out := js.NewObject()
	out.Set("event", js.Prop(e, "event"))
	out.Set("prev", js.Prop(e, "prev"))
	for _, k := range e.Keys() {
		if k != "event" && k != "prev" {
			out.Set(k, js.Prop(e, k))
		}
	}
	return "## " + js.String(js.Prop(e, "event")) + "\n\n```json\n" + js.StringifyValue(out) + "\n```\n\n"
}

var eventBlock = lazyre.New("^## ([^" + js.SpaceChars + "]+)\n\n```json\n((?s:.)*?)\n```\n\n")

// parseRecord is the events of a record. It refuses anything that is not
// exactly the canonical rendering of a chain of events: a record is only
// ever written by append, so any other byte means it was edited by hand.
// Offsets in its messages count UTF-16 code units, not bytes.
func parseRecord(text string) []*js.Object {
	var out []*js.Object
	for at := 0; at < len(text); {
		pos := js.Len(text[:at])
		m := eventBlock.FindStringSubmatch(text[at:])
		if m == nil {
			refuse("record is not an event log at byte %d: expected '## <event>' followed by one json block", pos)
		}
		v, err := js.ParseJSON(m[2])
		if err != nil {
			refuse("event %s at byte %d is not valid JSON", m[1], pos)
		}
		if v == nil {
			panic(&typeError{"Cannot read properties of null (reading 'event')"})
		}
		if name, _ := js.Prop(v, "event").(string); name != m[1] {
			refuse("heading %s at byte %d names a different event than its body (%s)", m[1], pos, js.String(js.Prop(v, "event")))
		}
		e := v.(*js.Object)
		if prev, ok := js.Prop(e, "prev").(string); !ok || prev != sha256Hex(text[:at]) {
			refuse("%s at byte %d: prev does not match the bytes before it; an earlier event was modified", m[1], pos)
		}
		if renderEvent(e) != m[0] {
			refuse("%s at byte %d is not in canonical form; the record was edited by hand", m[1], pos)
		}
		out = append(out, e)
		at += len(m[0])
	}
	return out
}

// ── public surface ──────────────────────────────────────────────────────────

// StatusOf is the state of a record. The error is a *Refusal when the
// record is invalid.
func StatusOf(record string) (s Status, err error) {
	defer recovered(&err)
	return replay(parseRecord(record)).status(), nil
}

// VerdictOf is the verdict a record closed with, "" while it is open: an
// ldd.VerdictFunc for a change's independent verification.
func VerdictOf(record string) (string, error) {
	s, err := StatusOf(record)
	if err != nil || s.Verdict == nil {
		return "", err
	}
	return *s.Verdict, nil
}
