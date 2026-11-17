// Package audit is `jhon audit <session-id|path> [--json | --html]
// [--context]`: it reads one session (Claude Code or pi) and its
// sub-agents after the fact and reports safety and outcomes, never method
// (contracts/vocabulary.yaml, "Audit rules"):
//
//	refusal-bypass      a refusal by the safety layer followed by the same
//	                    effect reached another way;
//	isolation-bypass    a write the runtime's isolation refused, made again
//	                    under another root;
//	delivered-untested  a push (`jhon publish` included), a merge into the
//	                    default branch, or a deploy whose tree no passing
//	                    test run or ready check is shown to have tested, or
//	                    whose tree's last run failed, or a deploy that names
//	                    no immutable artifact;
//	edit-before-red     a change's first edit of a code file before its RED
//	                    (a failing `jhon executions run <change>`) or before
//	                    its tasks.md;
//	test-weakened       after a failing test run and before the next pass,
//	                    an edit of a test file that adds a skip marker or
//	                    removes a test block;
//	verify-wrote        a verify agent's write of anything but its
//	                    verify-report.md;
//	explore-wrote       an explore agent's write of anything but its
//	                    change's exploration.md or research.md;
//	archive-without-red `jhon archive <change>` with no RED then GREEN of
//	                    the change in the session.
//
// isolation-bypass also reads pi, which has no runtime isolation: a pi
// worker or verify child's write outside its own cwd.
//
// Beside the checks it measures context use, outcomes, efficiency, timing,
// the owner's dialogue with the main thread (dialogue.go), and each agent
// role's use of memory, CodeGraph and the todo list (integrations.go); none is ever
// a finding. Read-only: it parses the files itself and never runs
// anything. Two stages, so a check never sees raw JSONL: LoadSession
// validates every line and normalises the session, and anything it cannot
// vouch for is an audit-error on which no check runs; the checks then read
// the normalised session. Transcripts are read, and agents measured,
// concurrently; every result merges in the order a sequential read gives.
//
// Severities: `violation` comes only from a deterministic check whose
// `audit_rules` entry is `verified`; `suspect` is heuristic, indeterminate,
// or the `would-be-violation` of a check still `pending-real-fixture`.
package audit

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/auditcheck"
	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// Checks are the checks this auditor runs. `audit_rules` must name exactly these.
var Checks = auditcheck.Names

// WouldBeViolation is what a `pending-real-fixture` check says instead of reporting a violation.
const WouldBeViolation = "would-be violation; check not yet validated against real clean and violation fixtures"

// layout is the key order a finding's JSON takes: the order of the object
// literal its check emitted, then the fields emit adds.
type layout int

const (
	// byCheck: check, severity, transcript, line, agent, agentType, evidence, message.
	byCheck layout = iota
	// byBase: check, transcript, line, agent, agentType, evidence, severity, message.
	byBase
	// byBaseKind: byBase with kind before message.
	byBaseKind
)

// Finding is one finding. ValidationStatus is how far its check's verdict
// is backed by real transcripts: `verified`, `pending-real-fixture`, or
// `heuristic`. Kind is `finding`, `indeterminate`, or `would-be-violation`.
// Anchor is the `audit_rules` entry the status was read from, and At the
// timestamp of the finding's line; neither is in the report's JSON.
type Finding struct {
	Check, ValidationStatus, Severity, Kind string
	Anchor, At                              string
	Transcript                              string
	Line                                    int
	Agent, AgentType, Evidence, Message     string
	layout                                  layout
}

// emitted is a finding as a check emits it. anchor names the `audit_rules`
// entry whose validation status the finding carries, for a check registered
// under more than one anchor; without it the check's deterministic status
// applies.
type emitted struct {
	check, severity, kind, anchor       string
	transcript                          string
	line                                int
	agent, agentType, evidence, message string
	layout                              layout
}

type emitFunc func(emitted)

// ValidationStatuses is each check's validation status, read from `audit_rules` (contracts/vocabulary.yaml).
func ValidationStatuses() map[string]string {
	rules := contract.Records(vocab(), "audit_rules")
	out := map[string]string{}
	for _, c := range Checks {
		out[c] = "heuristic"
		if i := slices.IndexFunc(rules, func(r map[string]string) bool {
			return slices.Contains(ruleChecks(r), c) && r["evidence_class"] == "deterministic"
		}); i >= 0 && rules[i]["validation_status"] != "" {
			out[c] = rules[i]["validation_status"]
		}
	}
	return out
}

// CheckAnchors is the anchor of each check's deterministic `audit_rules`
// entry, the one its status is read from.
func CheckAnchors() map[string]string {
	out := map[string]string{}
	for _, r := range contract.Records(vocab(), "audit_rules") {
		for _, c := range ruleChecks(r) {
			if _, ok := out[c]; !ok && r["evidence_class"] == "deterministic" {
				out[c] = r["anchor"]
			}
		}
	}
	return out
}

// ruleChecks are the checks an `audit_rules` entry names: one, or an
// inline list of the forms that read the same rule.
func ruleChecks(r map[string]string) []string {
	if l := contract.InlineList(r["check"]); l != nil {
		return l
	}
	return []string{r["check"]}
}

// PendingOn is, per anchor, the runtimes its entry names under
// `pending_on`: a rule verified in one runtime whose check in another has no
// real fixtures yet, so there it reports only a would-be violation.
func PendingOn() map[string][]string {
	out := map[string][]string{}
	for _, r := range contract.Records(vocab(), "audit_rules") {
		if l := contract.InlineList(r["pending_on"]); l != nil {
			out[r["anchor"]] = l
		}
	}
	return out
}

// AnchorStatuses is each anchor's validation status: a deterministic
// entry's `validation_status`, and `heuristic` for a heuristic entry.
func AnchorStatuses() map[string]string {
	out := map[string]string{}
	for _, r := range contract.Records(vocab(), "audit_rules") {
		switch {
		case r["anchor"] != "" && r["evidence_class"] == "deterministic" && r["validation_status"] != "":
			out[r["anchor"]] = r["validation_status"]
		case r["anchor"] != "" && r["evidence_class"] == "heuristic":
			out[r["anchor"]] = "heuristic"
		}
	}
	return out
}

// RunChecks runs every check over a loaded session.
func RunChecks(s *Session) ([]Finding, []string) {
	return runChecks(s, transcriptEvidence(s))
}

func runChecks(s *Session, ev evidence) ([]Finding, []string) {
	var findings []Finding
	notes := []string{}
	status := ValidationStatuses()
	anchors := AnchorStatuses()
	checkAnchor, pending := CheckAnchors(), PendingOn()
	at := map[string]map[int]string{}
	for _, t := range s.transcripts() {
		at[t.File] = map[int]string{}
		for _, e := range t.Entries {
			at[t.File][e.Line] = e.Timestamp
		}
	}
	// A check not yet validated both ways never reports a violation.
	emit := func(f emitted) {
		vs := anchors[f.anchor]
		anchor := f.anchor
		if f.anchor == "" || vs == "" {
			vs, anchor = status[f.check], checkAnchor[f.check]
		}
		if slices.Contains(pending[anchor], s.Runtime) {
			vs = "pending-real-fixture"
		}
		out := Finding{Check: f.check, ValidationStatus: vs, Severity: f.severity, Kind: f.kind, Anchor: anchor, At: at[f.transcript][f.line], Transcript: f.transcript, Line: f.line,
			Agent: f.agent, AgentType: f.agentType, Evidence: f.evidence, Message: f.message, layout: f.layout}
		if f.severity == "violation" && vs == "pending-real-fixture" {
			out.Severity, out.Kind, out.Message = "suspect", "would-be-violation", WouldBeViolation+": "+f.message
		} else if out.Kind == "" {
			out.Kind = "finding"
		}
		findings = append(findings, out)
	}
	checkRefusalBypass(s, emit, &notes)
	checkIsolationBypass(s, emit)
	checkDeliveredUntested(s, ev, emit, &notes)
	checkEditBeforeRed(s, emit)
	checkPiIsolation(s, emit)
	checkTestWeakened(s, emit)
	checkVerifyWrote(s, emit)
	checkExploreWrote(s, emit)
	checkArchiveWithoutRed(s, emit)
	slices.SortStableFunc(findings, func(a, b Finding) int {
		if c := js.LocaleCompare(a.Transcript, b.Transcript); c != 0 {
			return c
		}
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return js.LocaleCompare(a.Check, b.Check)
	})
	return findings, notes
}

// HasAuditable is whether any check has something to read: a sub-agent, or
// in the main thread a refusal or a delivery. A session with none is
// `nothing-to-audit`, never `clean`.
func HasAuditable(s *Session) bool {
	if len(s.Subagents) > 0 {
		return true
	}
	for _, e := range s.Main.Entries {
		for _, r := range e.ToolResults {
			if IsRefusal(r, e.Denial) {
				return true
			}
		}
		for _, u := range e.ToolUses {
			if c, ok := strProp(u.Input, "command"); ok && u.Name == "Bash" && (MergesToDefault(c) || DeliveryCommand(c) != "" || executionsRun.MatchString(c)) {
				return true
			}
		}
	}
	return false
}

// entrypointKind is the session kind an entrypoint stands for.
var entrypointKind = map[string]string{"cli": "interactive", "sdk-cli": "print"}

// SessionFacts is what the transcript records about the session, as recorded.
type SessionFacts struct {
	SessionKind  string
	AgentSetting *string
	// AgentNames are the session's recorded names: Claude Code 2.1.310
	// records an interactive --agent session, and one /clear relaunched, by
	// name only.
	AgentNames []string
}

// Facts is the session facts the header states, as recorded. Claude Code
// records `sessionKind` only for some sessions; the entrypoint every line
// carries tells an interactive session (cli) from `claude -p`.
func Facts(s *Session) SessionFacts {
	kinds := s.SessionKind
	if len(kinds) == 0 {
		kinds = nil
		for _, e := range s.Entrypoints {
			if k, ok := entrypointKind[e]; ok {
				kinds = append(kinds, k)
			} else {
				kinds = append(kinds, e)
			}
		}
	}
	f := SessionFacts{SessionKind: "unknown"}
	if len(kinds) > 0 {
		f.SessionKind = strings.Join(kinds, ", ")
	}
	f.AgentNames = s.Names
	if len(s.Settings) > 0 {
		setting := strings.Join(s.Settings, ", ")
		f.AgentSetting = &setting
	}
	return f
}

// RunsAs is whether the session ran as one of agents: by its agent
// setting, or by a recorded name that spells one ("jhon orchestrator").
func (f SessionFacts) RunsAs(agents []string) bool {
	if f.AgentSetting != nil && slices.Contains(agents, *f.AgentSetting) {
		return true
	}
	return slices.ContainsFunc(f.AgentNames, func(n string) bool {
		return slices.Contains(agents, strings.ReplaceAll(strings.TrimSpace(n), " ", "-"))
	})
}

// Result is what the audit concluded. Status is `clean` when checks read
// the session and found nothing, `nothing-to-audit` when no check had
// anything to read, `findings` when there is any violation or suspect, and
// `audit-error` when the session could not be read and no check ran. The
// measurements are nil on an audit-error.
type Result struct {
	OK         bool
	Status     string
	Runtime    string
	Version    *string
	Session    *SessionFacts
	Findings   []Finding
	Errors     []AuditError
	Notes      []string
	ContextUse *ContextUse
	Outcomes   *Outcomes
	Efficiency []*WriterEfficiency
	// Authorizations are the owner's yeses to a guard's question the session
	// records, which the report lists for the owner to see what they allowed.
	Authorizations []Authorization
	// Guards and LDD are what `jhon audit --all` aggregates (ldd.go); the
	// report of one session does not print them.
	Guards []GuardRefusal
	LDD    []ChangeLDD
	// Worked is what the session did right, each with its evidence (worked.go).
	Worked []Worked
	// Timing is how the session ran: model, tools, start-up, cache, compactions (timing.go).
	Timing *Timing
	// Dialogue is the owner's dialogue with the main thread, in counts (dialogue.go).
	Dialogue *Dialogue
	// Integrations is how each agent role used memory, CodeGraph and the
	// todo list, in counts (integrations.go).
	Integrations *Integrations
}

// Audit audits the session whose main transcript is mainPath. The checks
// and the three measurements run concurrently.
func Audit(mainPath string) *Result {
	s, errors := LoadSession(mainPath)
	if s == nil {
		return &Result{Status: "audit-error", Findings: []Finding{}, Errors: errors, Notes: []string{}}
	}
	ev := transcriptEvidence(s)
	r := &Result{Runtime: s.Runtime, Version: &s.Version, Errors: []AuditError{}, Authorizations: s.Authorizations}
	parallel(4, func(i int) {
		switch i {
		case 0:
			r.Findings, r.Notes = runChecks(s, ev)
			r.Notes = append(append([]string{}, s.ReadNotes...), r.Notes...)
		case 1:
			r.ContextUse = computeContextUse(s)
		case 2:
			r.Outcomes = computeOutcomes(s, ev)
		case 3:
			r.Efficiency = computeEfficiency(s)
		}
	})
	r.Guards, r.LDD = guardRefusals(s), changeLDD(s, r.Outcomes)
	r.Worked = whatWorked(ev, r.Outcomes, r.LDD, r.Guards)
	r.Timing = computeTiming(s)
	r.Dialogue = computeDialogue(s)
	r.Integrations = computeIntegrations(s)
	facts := Facts(s)
	r.Session = &facts
	r.OK = !slices.ContainsFunc(r.Findings, func(f Finding) bool { return f.Severity == "violation" })
	switch {
	case len(r.Findings) > 0:
		r.Status = "findings"
	case HasAuditable(s):
		r.Status = "clean"
	default:
		r.Status = "nothing-to-audit"
	}
	return r
}

var shortID = lazyre.New(`^[0-9a-fA-F-]+$`)

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// ResolveTarget is a path to a main transcript (resolved against cwd), or a
// session id looked up as `<projects>/*/<id>.jsonl`, or a unique id prefix of
// at least six characters, as the audit and the dashboard print one; ""
// when nothing matches.
func ResolveTarget(arg, cwd, projects string) string {
	if strings.Contains(arg, "/") || strings.HasSuffix(arg, ".jsonl") {
		p := filepath.Clean(arg)
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, arg)
		}
		if isFile(p) {
			return p
		}
		return ""
	}
	entries, err := os.ReadDir(projects)
	if err != nil {
		return ""
	}
	var dirs []string
	for _, e := range entries {
		dirs = append(dirs, e.Name())
	}
	slices.SortFunc(dirs, js.Compare)
	for _, d := range dirs {
		if p := filepath.Join(projects, d, arg+".jsonl"); isFile(p) {
			return p
		}
	}
	if js.Len(arg) < 6 || !shortID.MatchString(arg) {
		return ""
	}
	var hits []string
	for _, d := range dirs {
		dir := filepath.Join(projects, d)
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		files, _ := os.ReadDir(dir)
		for _, f := range files {
			if strings.HasPrefix(f.Name(), arg) && strings.HasSuffix(f.Name(), ".jsonl") {
				hits = append(hits, filepath.Join(dir, f.Name()))
			}
		}
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return ""
}
