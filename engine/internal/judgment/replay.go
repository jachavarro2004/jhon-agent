package judgment

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// ── field checks ────────────────────────────────────────────────────────────

func isArray(v any) bool {
	_, ok := v.([]any)
	return ok
}

// text is a string that is not blank.
func text(v any) bool {
	s, ok := v.(string)
	return ok && js.Trim(s) != ""
}

// stringList is an array of strings that are not blank.
func stringList(v any) bool {
	a, ok := v.([]any)
	if !ok {
		return false
	}
	for _, s := range a {
		if !text(s) {
			return false
		}
	}
	return true
}

// asStrings is an array already checked to hold only strings.
func asStrings(v any) []string {
	a, _ := v.([]any)
	out := make([]string, len(a))
	for i, s := range a {
		out[i], _ = s.(string)
	}
	return out
}

// includes is list.includes(v): v is one of the strings.
func includes(list []string, v any) bool {
	s, ok := v.(string)
	return ok && slices.Contains(list, s)
}

func join(list []string) string { return strings.Join(list, ", ") }

func num(f float64) string { return js.NumberString(f) }

// ── replay ──────────────────────────────────────────────────────────────────

type point struct {
	Point
	refs []string
}

type position struct {
	point, position string
	proofRefs       []string
}

type set map[string]bool

// state is everything the record establishes, rebuilt from its events in order.
type state struct {
	v *vocabulary
	// mode and verdict are "" until the record states them.
	mode   string
	open   *js.Object
	r0     map[string][]any
	points []*point
	info   []string
	// rounds[n][judge] is the positions, for n >= 1; index 0 stands for R0 and stays empty.
	rounds   []map[string][]position
	progress map[int]bool
	// refsBy is every proof_ref each judge has cited, for the adopt rule.
	refsBy map[string]set
	// refsBefore[n] is every proof_ref cited before round n started, for the progress rule.
	refsBefore map[int]set
	human      bool
	verdict    string
}

func (s *state) started() int { return len(s.rounds) - 1 }

func (s *state) r0Done() bool {
	for _, j := range s.v.judges {
		if _, ok := s.r0[j]; !ok {
			return false
		}
	}
	return true
}

// complete is the last round both judges answered: 0 after R0, false before.
func (s *state) complete() (int, bool) {
	if !s.r0Done() {
		return 0, false
	}
	n := s.started()
	if n >= 1 {
		for _, j := range s.v.judges {
			if _, ok := s.rounds[n][j]; !ok {
				return n - 1, true
			}
		}
	}
	return max(n, 0), true
}

func (s *state) halfRound() bool {
	c, ok := s.complete()
	return s.started() >= 1 && (!ok || c != s.started())
}

func (s *state) contested() []string {
	out := []string{}
	for _, p := range s.points {
		if p.Status == s.v.contested {
			out = append(out, p.ID)
		}
	}
	return out
}

func (s *state) find(id any) *point {
	for _, p := range s.points {
		if pid, ok := id.(string); ok && p.ID == pid {
			return p
		}
	}
	return nil
}

func (s *state) cite(judge string, refs []string) {
	if s.refsBy[judge] == nil {
		s.refsBy[judge] = set{}
	}
	for _, r := range refs {
		s.refsBy[judge][r] = true
	}
}

func (s *state) allRefs() set {
	all := set{}
	for _, refs := range s.refsBy {
		for r := range refs {
			all[r] = true
		}
	}
	return all
}

func (s *state) pending() []string {
	out := []string{}
	if s.open == nil || s.verdict != "" {
		return out
	}
	if !s.r0Done() {
		for _, j := range s.v.judges {
			if _, ok := s.r0[j]; !ok {
				out = append(out, s.v.roundPrefix+"0."+j)
			}
		}
	} else if s.halfRound() {
		n := s.started()
		for _, j := range s.v.judges {
			if _, ok := s.rounds[n][j]; !ok {
				out = append(out, s.v.roundPrefix+strconv.Itoa(n)+"."+j)
			}
		}
	}
	return out
}

// roundBlocked is why no further round may start, or "" when one may.
func (s *state) roundBlocked() string {
	n, _ := s.complete()
	switch {
	case len(s.contested()) == 0:
		return "no point is contested"
	case s.human:
		return "deliberation ended with the human decision"
	case n >= 1 && !s.progress[n]:
		return "round " + strconv.Itoa(n) + " made no progress"
	case float64(n) >= s.v.limits[s.mode]:
		return "the " + s.mode + " round limit of " + num(s.v.limits[s.mode]) + " has been reached"
	}
	return ""
}

func (s *state) apply(e *js.Object) {
	v := s.v
	id := js.Prop(e, "event").(string)
	if s.verdict != "" {
		refuse("%s: the record is closed; nothing is appended after %s", id, v.close)
	}
	if s.open == nil && id != v.open {
		refuse("%s: the record must start with %s", id, v.open)
	}
	switch id {
	case v.open:
		s.applyOpen(e)
		return
	case v.human:
		s.applyHuman(e)
		return
	case v.close:
		s.applyClose(e)
		return
	}
	m := v.round.FindStringSubmatch(id)
	if m == nil || !slices.Contains(v.judges, m[2]) {
		refuse("%s: not an event type contracts/judgment.md declares", id)
	}
	n, _ := strconv.ParseFloat(m[1], 64)
	judge := m[2]
	need(js.Prop(e, "judge") == any(judge), "%s: the event names judge %s, but its judge field is %s", id, judge, js.String(js.Prop(e, "judge")))
	if n == 0 {
		s.applyR0(e, judge)
		return
	}
	s.applyRound(e, n, judge)
}

func (s *state) applyOpen(e *js.Object) {
	v := s.v
	need(s.open == nil, "%s: the record is already open", v.open)
	need(includes(v.modes, js.Prop(e, "mode")), "%s: mode must be one of %s", v.open, join(v.modes))
	mode := js.Prop(e, "mode").(string)
	if mode == v.docket {
		for _, f := range []string{"question", "invariant", "burden_of_proof"} {
			need(text(js.Prop(e, f)), "%s: a %s record needs %s", v.open, v.docket, f)
		}
		opts, ok := js.Prop(e, "options").([]any)
		valid := ok && len(opts) >= 2
		for _, o := range opts {
			valid = valid && js.IsObject(o) && text(js.Prop(o, "id")) && text(js.Prop(o, "summary"))
		}
		need(valid, "%s: options must be at least two objects with id and summary", v.open)
		ids := make([]string, len(opts))
		seen := set{}
		for i, o := range opts {
			ids[i] = js.Prop(o, "id").(string)
			seen[ids[i]] = true
		}
		need(len(seen) == len(ids), "%s: option ids repeat", v.open)
		need(includes(ids, js.Prop(e, "removal_option")), "%s: removal_option must name one of the options (%s); removing the thing in question is always an option", v.open, join(ids))
	} else {
		need(text(js.Prop(e, "target")), "%s: a %s record needs the frozen target identity in target", v.open, v.defect)
	}
	s.open = e
	s.mode = mode
}

func (s *state) applyR0(e *js.Object, judge string) {
	v := s.v
	id := js.Prop(e, "event").(string)
	_, done := s.r0[judge]
	need(!done, "%s: already recorded; R0 results are immutable", id)
	r := js.Prop(e, "result")
	need(js.IsObject(r) && isArray(js.Prop(r, "findings")) && stringList(js.Prop(r, "evidence")),
		`%s: result must be the judge's native result, {"findings": [...], "evidence": [...]}`, id)
	findings := js.Prop(r, "findings").([]any)
	for i, f := range findings {
		fid := judge + strconv.Itoa(i+1)
		need(js.IsObject(f) && text(js.Prop(f, "location")) && text(js.Prop(f, "claim")) && includes(v.severities, js.Prop(f, "severity")) && isArray(js.Prop(f, "proof_refs")),
			"%s: finding %s needs location, claim, proof_refs, and a severity in %s", id, fid, join(v.severities))
		if s.mode != v.docket && !includes(v.severe, js.Prop(f, "severity")) {
			s.info = append(s.info, fid)
			continue
		}
		refs := js.Prop(f, "proof_refs")
		need(stringList(refs) && len(refs.([]any)) > 0, "%s: finding %s is a point and must carry proof_refs", id, fid)
		s.points = append(s.points, &point{Point{
			ID: fid, Owner: judge, Status: v.contested, Severity: js.Prop(f, "severity").(string),
			Location: js.Prop(f, "location").(string), Claim: js.Prop(f, "claim").(string),
		}, asStrings(refs)})
		s.cite(judge, asStrings(refs))
	}
	s.r0[judge] = findings
	// Keep a stable order regardless of which judge arrived first.
	slices.SortStableFunc(s.points, func(a, b *point) int {
		if d := slices.Index(v.judges, a.Owner) - slices.Index(v.judges, b.Owner); d != 0 {
			return d
		}
		d := idNumber(a.ID) - idNumber(b.ID)
		switch {
		case d < 0:
			return -1
		case d > 0:
			return 1
		}
		return 0 // equal, or NaN, which sort takes as equal
	})
	slices.SortStableFunc(s.info, js.Compare)
}

// idNumber is Number(id.slice(1)): the finding's number after its judge.
func idNumber(id string) float64 {
	_, size := utf8.DecodeRuneInString(id)
	return js.Number(id[size:])
}

func (s *state) applyRound(e *js.Object, n float64, judge string) {
	v := s.v
	id := js.Prop(e, "event").(string)
	if !s.r0Done() {
		refuse("%s: round %s before R0 is complete; waiting for %s", id, num(n), join(s.pending()))
	}
	if s.human {
		refuse("%s: no round follows a human decision", id)
	}
	if started := float64(s.started()); n <= started {
		_, done := s.rounds[int(n)][judge]
		need(!done, "%s: already recorded", id)
		need(n == started, "%s: round %s is over", id, num(n))
	} else {
		need(n == started+1, "%s: round %s cannot start before round %s", id, num(n), num(n-1))
		need(!s.halfRound(), "%s: round %d is incomplete", id, s.started())
		if blocked := s.roundBlocked(); blocked != "" {
			refuse("%s: no further round: %s", id, blocked)
		}
		s.rounds = append(s.rounds, map[string][]position{})
		s.refsBefore[int(n)] = s.allRefs()
	}
	round := int(n)
	positions, ok := js.Prop(e, "positions").([]any)
	for _, p := range positions {
		ok = ok && js.IsObject(p)
	}
	need(ok, "%s: positions must be an array of objects", id)
	open := s.contested()
	seen := set{}
	out := []position{}
	for _, p := range positions {
		pt := s.find(js.Prop(p, "point"))
		need(pt != nil, "%s: %s is not a point of this record", id, js.String(js.Prop(p, "point")))
		pid := pt.ID
		need(slices.Contains(open, pid), "%s: %s is not contested; a closed point is final", id, pid)
		need(!seen[pid], "%s: %s has two positions", id, pid)
		seen[pid] = true
		owner := pt.Owner == judge
		allowed, role := v.opponentPositions, "opponent"
		if owner {
			allowed, role = v.ownerPositions, "owner"
		}
		need(includes(allowed, js.Prop(p, "position")),
			"%s: %s: judge %s is its %s and may take %s, not %s", id, pid, judge, role, join(allowed), js.String(js.Prop(p, "position")))
		need(text(js.Prop(p, "reason")), "%s: %s: a position needs a reason", id, pid)
		need(stringList(js.Prop(p, "proof_refs")), "%s: %s: proof_refs must be a list of strings", id, pid)
		refs := asStrings(js.Prop(p, "proof_refs"))
		taken := js.Prop(p, "position").(string)
		if taken == v.maintain {
			need(len(refs) > 0, "%s: %s: %s must bring evidence in proof_refs", id, pid, v.maintain)
		}
		if taken == v.adopt {
			theirs := s.refsBy[pt.Owner]
			need(slices.ContainsFunc(refs, func(r string) bool { return !theirs[r] }),
				"%s: %s of %s needs the adopter's own proof: at least one proof_ref judge %s has never cited; agreement without it is %s", id, v.adopt, pid, pt.Owner, v.retract)
		}
		out = append(out, position{pid, taken, refs})
	}
	var missing []string
	for _, p := range open {
		if !seen[p] {
			missing = append(missing, p)
		}
	}
	need(len(missing) == 0, "%s: takes no position on contested %s", id, join(missing))
	s.rounds[round][judge] = out
	var cited []string
	for _, p := range out {
		cited = append(cited, p.proofRefs...)
	}
	s.cite(judge, cited)
	for _, j := range v.judges {
		if _, ok := s.rounds[round][j]; !ok {
			return
		}
	}
	s.resolve(round)
}

// resolve applies a complete round: it resolves each point by the
// contract's table and computes progress.
func (s *state) resolve(n int) {
	v := s.v
	before := s.refsBefore[n]
	progress := false
	for _, j := range v.judges {
		for _, p := range s.rounds[n][j] {
			progress = progress || p.position == v.concede || p.position == v.retract ||
				slices.ContainsFunc(p.proofRefs, func(r string) bool { return !before[r] })
		}
	}
	s.progress[n] = progress
	positionOf := func(judge, id string) string {
		for _, p := range s.rounds[n][judge] {
			if p.point == id {
				return p.position
			}
		}
		return ""
	}
	for _, pt := range s.points {
		if pt.Status != v.contested {
			continue
		}
		opponent := ""
		for _, j := range v.judges {
			if j != pt.Owner {
				opponent = j
				break
			}
		}
		own, opp := positionOf(pt.Owner, pt.ID), positionOf(opponent, pt.ID)
		switch {
		case own == v.maintain && opp == v.adopt:
			pt.Status = v.confirmed
		case own == v.maintain && opp == v.retract:
			pt.Status = v.unconfirmed
		case own == v.maintain:
			pt.Status = v.contested
		case opp == v.adopt:
			pt.Status = v.contested
		default:
			pt.Status = v.dropped
		}
	}
}

func (s *state) applyHuman(e *js.Object) {
	v := s.v
	id := js.Prop(e, "event").(string)
	need(s.r0Done(), "%s: R0 is not complete", id)
	need(!s.halfRound(), "%s: round %d is incomplete", id, s.started())
	ds, ok := js.Prop(e, "decisions").([]any)
	ok = ok && len(ds) > 0
	for _, d := range ds {
		ok = ok && js.IsObject(d)
	}
	need(ok, "%s: decisions must be a non-empty array of objects", id)
	seen := set{}
	for _, d := range ds {
		pt := s.find(js.Prop(d, "point"))
		need(pt != nil, "%s: %s is not a point of this record", id, js.String(js.Prop(d, "point")))
		need(pt.Status == v.contested, "%s: %s is not contested; the human decides only residual disputes", id, pt.ID)
		need(!seen[pt.ID], "%s: %s is decided twice", id, pt.ID)
		need(includes(v.humanOutcomes, js.Prop(d, "outcome")), "%s: %s: outcome must be one of %s", id, pt.ID, join(v.humanOutcomes))
		need(text(js.Prop(d, "rationale")), "%s: %s: a decision needs a rationale", id, pt.ID)
		seen[pt.ID] = true
	}
	for _, d := range ds {
		s.find(js.Prop(d, "point")).Status = js.Prop(d, "outcome").(string)
	}
	s.human = true
}

func (s *state) applyClose(e *js.Object) {
	v := s.v
	id := js.Prop(e, "event").(string)
	consensus, escalated := v.verdicts[s.mode][0], v.verdicts[s.mode][1]
	verdict := js.Prop(e, "verdict")
	need(includes([]string{consensus, escalated}, verdict),
		"%s: %s is not a %s verdict; a %s record closes %s or %s", id, js.String(verdict), s.mode, s.mode, consensus, escalated)
	need(text(js.Prop(e, "summary")), "%s: a summary is required", id)
	if verdict == any(consensus) {
		need(s.r0Done() && !s.halfRound(), "%s: %s needs R0 and every started round complete", id, consensus)
		contested := s.contested()
		need(len(contested) == 0, "%s: %s with %s still contested", id, consensus, join(contested))
		if s.mode == v.docket {
			var ids []string
			for _, o := range js.Prop(s.open, "options").([]any) {
				ids = append(ids, js.Prop(o, "id").(string))
			}
			need(includes(ids, js.Prop(e, "option")), "%s: %s must name the chosen option (%s)", id, consensus, join(ids))
			conditions := js.Prop(e, "conditions")
			_, absent := conditions.(js.Undefined)
			need(absent || stringList(conditions), "%s: conditions must be a list of strings", id)
		}
	}
	s.verdict = verdict.(string)
}

func (s *state) status() Status {
	v := s.v
	out := Status{
		Pending: s.pending(), Points: []Point{}, Fix: []Point{}, Info: append([]string{}, s.info...),
		Contested: s.contested(), Closed: []string{}, NextAction: v.nextRound,
	}
	complete, ok := s.complete()
	if ok {
		out.Round = &complete
	}
	if ok && complete >= 1 {
		out.NewProgress = new(s.progress[complete])
	}
	switch {
	case s.verdict != "":
		out.NextAction = v.closed
	case !s.r0Done() || s.halfRound():
		out.NextAction = v.nextRound
	case len(out.Contested) == 0:
		out.NextAction = v.closeAction
	case s.human:
		out.NextAction = v.humanDecision
	case out.NewProgress != nil && !*out.NewProgress:
		out.NextAction, out.Reason = v.escalate, new(v.noProgress)
	case float64(complete) >= v.limits[s.mode]:
		out.NextAction, out.Reason = v.escalate, new(v.roundLimit)
	}
	if s.mode != "" {
		out.Mode = new(s.mode)
	}
	if s.verdict != "" {
		out.Verdict = new(s.verdict)
	}
	for _, p := range s.points {
		out.Points = append(out.Points, p.Point)
		if p.Status != v.contested {
			out.Closed = append(out.Closed, p.ID)
		}
		if s.mode == v.defect && p.Status == v.confirmed && slices.Contains(v.severe, p.Severity) {
			out.Fix = append(out.Fix, p.Point)
		}
	}
	return out
}

func replay(events []*js.Object) *state {
	s := &state{
		v: vocab(), r0: map[string][]any{}, info: []string{}, rounds: []map[string][]position{{}},
		progress: map[int]bool{}, refsBy: map[string]set{}, refsBefore: map[int]set{},
	}
	for _, e := range events {
		s.apply(e)
	}
	return s
}
