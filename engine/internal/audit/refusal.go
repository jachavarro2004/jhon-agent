package audit

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// refusal-bypass (contracts/runtime-boundaries.md): a refusal by the safety
// layer followed by the same effect reached another way.

// isolationMarkers are the runtime's isolation refusals: a sub-agent
// confined to its own worktree, and a background session that has not
// isolated yet refusing writes to the shared checkout. isolation-bypass
// reads these alone.
var isolationMarkers = []*lazyre.Regexp{
	lazyre.New(`^This agent is isolated in the worktree (?s:.)*Refusing to run it`),
	lazyre.New(`hasn't isolated yet, so writes to the shared checkout are blocked`),
}

// What makes an error tool_result a refusal of any authority: a denial the
// runtime labels itself, or the text of a hook, a permission rule, or an
// isolation boundary. Every other error is an operational failure.
var (
	denialKinds  = []string{"permission-rule", "automode-blocked", "user-rejected"}
	refusalTexts = append([]*lazyre.Regexp{
		lazyre.New(`^PreToolUse:\w+ hook error:`),
		lazyre.New(`^Permission to use ` + nonSpace + `+ (?s:.)* has been denied`),
	}, isolationMarkers...)
)

// ReviewAgents are the agents whose delegation is a review: the inspect guard binds these.
var ReviewAgents = []string{"review-risk", "review-resilience", "review-readability", "review-reliability", "review-refuter", "review-validator"}

// SafetyGuards are the guards of the safety layer whose refusals refusal-bypass reads.
var SafetyGuards = []string{"safety-guard", "vcs-guard", "framework-guard", "secrets-guard", "inspect-guard"}

func anyMatch(res []*lazyre.Regexp, s string) bool {
	return slices.ContainsFunc(res, func(re *lazyre.Regexp) bool { return re.MatchString(s) })
}

// IsRefusal is whether an error result is a refusal of any authority.
func IsRefusal(r ToolResult, denial string) bool {
	if !r.IsError {
		return false
	}
	if denial != "" && slices.Contains(denialKinds, denial) {
		return true
	}
	return anyMatch(refusalTexts, js.Trim(r.Text))
}

var allowedForms = lazyre.New(`(?m)` + js.LineStart + `allowed forms: (` + js.Dot + `+)`)

// DeclaredForms is the forms a refusal declares its authority allows: each
// entry of its `allowed forms: …` line (the inspect guard's), or nil.
func DeclaredForms(text string) [][]string {
	m := allowedForms.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	var forms [][]string
	for _, f := range strings.Split(m[1], ",") {
		if w := spaces.Split(js.Trim(f), -1); w[0] != "" {
			forms = append(forms, w)
		}
	}
	return forms
}

// inForm is whether an invocation is one of forms: the program by name,
// with no wrapper before it, and its leading operands the form's words (a
// value option such as `git -C <path>` aside, which the guard reads the same way).
func inForm(x Invocation, forms [][]string) bool {
	if x.Via != "plain" {
		return false
	}
	have := append([]string{x.Program}, words(x).operands...)
	return slices.ContainsFunc(forms, func(f []string) bool {
		for i, w := range f {
			if i >= len(have) || have[i] != w {
				return false
			}
		}
		return true
	})
}

// injected is a notification or injection the runtime writes as a user string, for an entry with no origin.
var injected = lazyre.New(`^` + space + `*<(task-notification|system-reminder|local-command-\w+|command-name)>`)

// notice is a runtime's automatic message that reads as plain text: a
// compaction summary, a background job or sub-agent that ended or asks, a
// system-generated wake (L48).
var notice = lazyre.New(`(?s)^` + space + `*(The conversation history before this point was compacted|Background (job|sub-agent) .*\b(exited|completed|failed|finished)\b|Subagent \S+ (\(task [^)]*\) )?(finished|asks|failed)|.*\[System-generated [^\]]*notification, not written by the user\])`)

// IsGenuineUserMessage is whether an entry is a message the user wrote: a
// user entry that is neither a tool result nor a message the runtime
// injects. The runtime marks a typed prompt `origin.kind: human`, including
// one delivered mid-turn as a `queued_command`, and a task notification
// `origin.kind: task-notification`.
func IsGenuineUserMessage(e *Entry) bool {
	if e.Role != "user" || e.IsMeta || len(e.ToolResults) > 0 {
		return false
	}
	if e.Origin != nil {
		return *e.Origin == "human"
	}
	return !injected.MatchString(e.note()) && !notice.MatchString(e.note())
}

// delegationTools are the main-thread tools that launch a delegation.
var delegationTools = []string{"Agent", "Task"}

var (
	resultTag     = lazyre.New(`<result>((?s:.)*?)</result>`)
	asyncLaunched = lazyre.New(`^Async agent launched`)
)

// delivered is the result a main-thread entry, whose note is note, delivers
// for the delegation launched by toolUseID; ok is false when it delivers none.
func delivered(e *Entry, note, toolUseID string) (out string, ok bool) {
	if strings.Contains(note, "<task-notification>") && strings.Contains(note, "<tool-use-id>"+toolUseID+"</tool-use-id>") {
		out, ok = "", true
		if m := resultTag.FindStringSubmatch(note); m != nil {
			out = m[1]
		}
	}
	for _, r := range e.ToolResults {
		if r.ID == toolUseID && !asyncLaunched.MatchString(r.Text) {
			out, ok = r.Text, true
		}
	}
	return out, ok
}

// deliveryIndex is, per launching tool_use id, the main-thread entries that
// may deliver its result: those whose notification names the id and those
// holding a tool result of it; and each entry's note.
type deliveryIndex struct {
	notes   []string
	entries map[string][]int
}

func newSession(s *Session) *Session {
	s.deliveries = sync.OnceValue(func() *deliveryIndex {
		x := &deliveryIndex{entries: map[string][]int{}}
		add := func(id string, i int) {
			if l := x.entries[id]; len(l) == 0 || l[len(l)-1] != i {
				x.entries[id] = append(l, i)
			}
		}
		for i, e := range s.Main.Entries {
			note := e.note()
			x.notes = append(x.notes, note)
			if strings.Contains(note, "<task-notification>") {
				for rest := note; ; {
					k := strings.Index(rest, "<tool-use-id>")
					if k < 0 {
						break
					}
					rest = rest[k+len("<tool-use-id>"):]
					if end := strings.Index(rest, "</tool-use-id>"); end >= 0 {
						add(rest[:end], i)
					}
				}
			}
			for _, r := range e.ToolResults {
				add(r.ID, i)
			}
		}
		return x
	})
	return s
}

// completion is the main-thread entry that delivers a sub-agent's result,
// and that result: the last one, when it notified more than once; index -1 for none.
func completion(s *Session, a *Subagent) (index int, result string) {
	index = -1
	x := s.deliveries()
	candidates := x.entries[a.ToolUseID]
	if strings.Contains(a.ToolUseID, "<") { // an id the index cannot hold: read every entry
		candidates = nil
		for i := range s.Main.Entries {
			candidates = append(candidates, i)
		}
	}
	for _, i := range candidates {
		if r, ok := delivered(s.Main.Entries[i], x.notes[i], a.ToolUseID); ok {
			index, result = i, r
		}
	}
	return index, result
}

// negated is a negation just before a match: `do not push`, `never commit`, `no merge, push`.
var negated = lazyre.New(ci(`\b(?:not|never|no|don't|do not|without|avoid)` + space + `+(?:[\w` + "`" + `'-]+[` + js.SpaceChars + `,]+){0,3}$`))

// reported is a sentence that reports or forbids an operation rather than
// asking for it, or leaves it to the owner ("the owner runs it himself").
var reported = lazyre.New(ci(`\brefus\w*|\bnot allowed\b|\bforbidden\b|\boutside (?:your|the|its) allowlist\b|\bdo not\b|\bdon't\b|\bnever\b|\bblocked\b|\b(?:him|her)self\b|\bthemselves\b`))

// inSet is whether r is JavaScript whitespace or one of chars.
func inSet(r rune, chars string) bool { return js.IsSpace(r) || strings.ContainsRune(chars, r) }

// mentions is whether text names word as a whole shell word.
func mentions(text, word string) bool {
	for i := 0; i+len(word) <= len(text); i++ {
		if !strings.HasPrefix(text[i:], word) {
			continue
		}
		if i > 0 {
			if r, _ := utf8.DecodeLastRuneInString(text[:i]); !inSet(r, "\"'`(,;:=[") {
				continue
			}
		}
		if e := i + len(word); e < len(text) {
			if r, _ := utf8.DecodeRuneInString(text[e:]); !inSet(r, "\"'`),;:.]") {
				continue
			}
		}
		return true
	}
	return false
}

var shortCluster = lazyre.New(`^-[A-Za-z]+$`)

// shortLetters is the letters of a short option cluster (`-rf`), or nil for anything else.
func shortLetters(a string) []string {
	if !shortCluster.MatchString(a) {
		return nil
	}
	return strings.Split(a[1:], "")
}

// effect is a refused effect. A command effect is a program, the operands
// the refusal names (the subcommand always), and the options of the
// refused call, value options aside; a write effect is the path a write
// tool was refused.
type effect struct {
	write    bool
	program  string
	operands []string
	// args are every operand of the refused invocation, named or not.
	args    []string
	options []string
	path    string
	text    string
}

type split struct{ options, operands []string }

// words is an invocation's options and operands, a value option and its value aside.
func words(x Invocation) split {
	withValue := valueOptions[x.Program]
	var w split
	for k := 0; k < len(x.Args); k++ {
		a := x.Args[k]
		if slices.Contains(withValue, a) {
			k++
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			if a != "--" {
				w.options = append(w.options, a)
			}
		} else {
			w.operands = append(w.operands, a)
		}
	}
	return w
}

// commandEffects is the command effects a refusal of command names:
// invocations whose program its text names, and whose subcommand, when they
// have one, it names too. A compound command is refused for one part.
func commandEffects(command, body string) []effect {
	var out []effect
	xs := plain(command)
	for _, x := range xs {
		if x.Code != nil || slices.Contains(neutral, x.Program) || !mentions(body, x.Program) {
			continue
		}
		if x.Sub != "" && !mentions(body, x.Sub) {
			continue
		}
		// An interpreter refused for its code is not refused as a program:
		// another script is another effect (the code branch reads one that
		// names the refused program).
		if mentions(body, x.Program+" code") {
			continue
		}
		// Of several invocations of one program and subcommand in the refused
		// command, the refused one is the one whose verb the refusal names:
		// `git worktree list` beside `git worktree remove --force` is no effect.
		if verb := verbOf(x); verb != "" && !mentions(body, verb) && slices.ContainsFunc(xs, func(y Invocation) bool {
			return y.Program == x.Program && y.Sub == x.Sub && y.Code == nil && !slices.Equal(y.Args, x.Args)
		}) {
			continue
		}
		w := words(x)
		// A refused forced form (`git worktree remove --force`, `git branch -D`,
		// `docker rm -f`) is not reached by the form without force, which the
		// program itself refuses where work would be lost.
		if forceFlag.MatchString(refusedForm(body)) && !slices.ContainsFunc(w.options, forceOption.MatchString) {
			continue
		}
		named := []string{x.Sub}
		for _, a := range w.operands {
			if mentions(body, a) {
				named = append(named, a)
			}
		}
		var operands []string
		for _, a := range uniq(named) {
			if a != "" {
				operands = append(operands, a)
			}
		}
		out = append(out, effect{program: x.Program, operands: operands, args: w.operands, options: w.options,
			text: strings.Join(slices.Concat([]string{x.Program}, operands, w.options), " ")})
	}
	return out
}

var (
	// forceFlag is a force flag in the refused form a refusal names.
	forceFlag = lazyre.New(`(?:^|\s)(?:--force\b|-[A-Za-z]*[fD](?:\s|$))`)
	// forceOption is an invocation's force option: `--force`, or a short
	// cluster holding f or D (`-f`, `-fd`, `-D`).
	forceOption = lazyre.New(`^(?:--force(?:=.*)?|-[A-Za-z]*[fD][A-Za-z]*)$`)
)

// refusedForm is the form a refusal names, before its reason: `git worktree
// remove --force` of `refused: git worktree remove --force: it discards ...`
// or of `git branch -D x (it deletes ...): destroy needs ...`.
func refusedForm(body string) string {
	s := body
	if i := strings.Index(s, "refused: "); i >= 0 {
		s = s[i+len("refused: "):]
	}
	if j := strings.Index(s, ": "); j >= 0 {
		s = s[:j]
	}
	if j := strings.Index(s, " ("); j >= 0 {
		s = s[:j]
	}
	return s
}

// verbOf is an invocation's first operand after its subcommand, "" for none.
func verbOf(x Invocation) string {
	ops := words(x).operands
	if len(ops) > 0 && ops[0] == x.Sub {
		ops = ops[1:]
	}
	if len(ops) == 0 {
		return ""
	}
	return ops[0]
}

// sharedPath is the first operand a script run (an interpreter or a shell
// given a file) shares with the refused invocation's operands, among those
// that name a path or a variable; "" for none.
func sharedPath(x Invocation, f effect) string {
	_, interp := interpreters[x.Program]
	if x.Code != nil || !(interp || slices.Contains(shells, x.Program)) {
		return ""
	}
	ops := words(x).operands
	if len(ops) < 2 {
		return ""
	}
	for _, a := range ops[1:] {
		if strings.ContainsAny(a, "/$.") && a != "." && a != ".." && slices.Contains(f.args, a) {
			return a
		}
	}
	return ""
}

// reaches is whether invocation x reaches command effect f: the same
// program, every refused operand, and every refused option.
func reaches(x Invocation, f effect) bool {
	if x.Code != nil || x.Program != f.program {
		return false
	}
	w := words(x)
	for _, o := range f.operands {
		if !slices.Contains(w.operands, o) {
			return false
		}
	}
	letters := map[string]bool{}
	for _, o := range w.options {
		for _, l := range shortLetters(o) {
			letters[l] = true
		}
	}
	for _, o := range f.options {
		if l := shortLetters(o); l != nil {
			for _, c := range l {
				if !letters[c] {
					return false
				}
			}
			continue
		}
		if !slices.ContainsFunc(w.options, func(p string) bool { return p == o || strings.HasPrefix(p, o+"=") }) {
			return false
		}
	}
	return true
}

// refusal is a refusal by the safety layer, as refusal-bypass reads it.
type refusal struct {
	guard, body string
	ask         bool
	effects     []effect
	allowed     [][]string
	transcript  *Transcript
	line        int
	timestamp   string
	// cwd is the directory the refused call ran in.
	cwd string
}

// askedClass is a class an asked refusal needs: `<what>: <class> needs the owner's authorization`.
var askedClass = lazyre.New(`([\w-]+) needs the owner's authorization`)

var (
	guardText    = lazyre.New(`^PreToolUse:\w+ hook error: \[(?s:.)*?\]: ([\w-]+-guard): ((?s:.)*)$`)
	refusedLead  = lazyre.New(`^refused:`)
	needsOwner   = lazyre.New(`needs the owner's authorization`)
	writeToolKey = map[string]string{"Edit": "file_path", "MultiEdit": "file_path", "Write": "file_path", "NotebookEdit": "notebook_path"}
)

// askForm is one part of a guard's question: the class it needs and the
// form it names, `destroy` and `git worktree remove --force` of `destroy:
// git worktree remove --force (it discards the worktree's uncommitted work)`.
type askForm struct{ cls, form string }

// askForms is the parts of a guard's question, `<class>: <form> (<why>)` joined by `; `.
func askForms(ask string) []askForm {
	var out []askForm
	for _, part := range strings.Split(ask, "; ") {
		cls, what, ok := strings.Cut(part, ": ")
		form, _, _ := strings.Cut(what, " (")
		if cls, form = js.Trim(cls), js.Trim(form); ok && cls != "" && form != "" {
			out = append(out, askForm{cls, form})
		}
	}
	return out
}

// classesOf is the classes the owner's yeses in the session say a command
// effect needs: those of each question whose form names it.
func classesOf(as []Authorization, f effect) []string {
	var out []string
	if f.write {
		return nil
	}
	for _, a := range as {
		for _, q := range askForms(a.Ask) {
			if mentions(q.form, f.program) && allMentioned(q.form, f.operands) && allMentioned(q.form, f.options) {
				addOnce(&out, q.cls)
			}
		}
	}
	return out
}

// SafetyRefusal is the safety-layer guard that refused a call, and the
// refusal's body; ok is false when no such guard refused it: another hook,
// a permission rule, the runtime, a removed procedure guard, or the inspect
// guard on an agent that is not a reviewer.
func SafetyRefusal(r ToolResult, agentType string) (guard, body string, ask, ok bool) {
	if !r.IsError {
		return
	}
	m := guardText.FindStringSubmatch(js.Trim(r.Text))
	if m == nil || !slices.Contains(SafetyGuards, m[1]) {
		return
	}
	if m[1] == "inspect-guard" && !slices.Contains(ReviewAgents, agentType) {
		return
	}
	// Any guard may ask: vcs-guard asks for outside-write as safety-guard
	// asks for a push, and the owner's grant answers both alike.
	ask = !refusedLead.MatchString(m[2]) && needsOwner.MatchString(m[2])
	return m[1], m[2], ask, true
}

// writePathOf is the absolute path a write tool's input names, "" for none.
func writePathOf(u ToolUse, cwd string) string {
	key, ok := writeToolKey[u.Name]
	if !ok {
		return ""
	}
	p, ok := strProp(u.Input, key)
	if !ok {
		return ""
	}
	return absolute(p, cwd)
}

type hit struct {
	severity string
	p        *refusal
	f        effect
	how      string
}

// checkRefusalBypass is refusal-bypass. Only a refusal by the safety layer
// is read (SafetyRefusal). Its effect is what the refused call would have
// done. A later call in the same agent that reaches that effect and is not
// refused is the bypass: a violation after a refusal that is never granted,
// and a suspect after an outward-facing one the guard asked about, unless
// the owner's grants or mode cover every class it asked for, live at the
// retry and for the retry's project: then it is the owner's decision, a note. So is a
// retry the owner answered yes to (pi's jhon-owner-authorization entry
// matched to the call), and a retry after an outright refusal under a live
// grant (not a yes once) of a class the owner's yeses in the session name
// for the effect: an earlier release refused what the guard now asks. A call
// in a form the refusal declares allowed is compliance; a call refused
// again is the refused route; an interpreter's code naming the refused
// program is a suspect. After a refusal by the safety, write, or framework
// guard, a later main-thread delegation that asks for the effect is a
// suspect (checkDelegatedBypass).
func checkRefusalBypass(s *Session, emit emitFunc, notes *[]string) {
	grants := append(ownerGrants(s), modeGrants()...)
	held := slices.DeleteFunc(slices.Clone(grants), func(g grant) bool { return g.root == "" })
	yes := map[string]Authorization{}
	for _, a := range s.Authorizations {
		if a.ToolUseID != "" {
			yes[a.ToolUseID] = a
		}
	}
	var all []*refusal
	for _, t := range s.transcripts() {
		type done struct {
			r ToolResult
			e *Entry
		}
		results := map[string]done{}
		for _, e := range t.Entries {
			for _, r := range e.ToolResults {
				results[r.ID] = done{r, e}
			}
		}
		var refused []*refusal
		for _, e := range t.Entries {
			for _, u := range e.ToolUses {
				d, ok := results[u.ID]
				if !ok {
					continue
				}
				cwd := e.Cwd
				var command *string
				rawCommand, isBash := strProp(u.Input, "command")
				if isBash = isBash && u.Name == "Bash"; isBash {
					c := WithoutHeredocs(rawCommand)
					command = &c
				}
				writePath := writePathOf(u, cwd)
				if guard, body, ask, ok := SafetyRefusal(d.r, t.AgentType); ok {
					var effects []effect
					if command != nil {
						effects = commandEffects(*command, body)
					} else if writePath != "" {
						effects = []effect{{write: true, path: writePath, text: u.Name + " " + writePath}}
					}
					if len(effects) > 0 {
						refused = append(refused, &refusal{guard, body, ask, effects, DeclaredForms(body), t, e.Line, d.e.Timestamp, cwd})
					}
					continue
				}
				if len(refused) == 0 || IsRefusal(d.r, d.e.Denial) {
					continue
				}
				var h *hit
				for _, p := range refused {
					for _, f := range p.effects {
						if !f.write && command != nil {
							for _, x := range plain(rawCommand) {
								if x.Code != nil && mentions(*x.Code, f.program) && allMentioned(*x.Code, f.operands) {
									if h == nil {
										h = &hit{"suspect", p, f, x.Program + " code that names it"}
									}
								} else if shared := sharedPath(x, f); shared != "" {
									// A script of the agent's own run on the refused
									// paths may do what the refusal prevented (session
									// 49dcc77e: `git checkout --ours -- $F` refused, then
									// a conflict-resolving script on $F).
									if h == nil {
										h = &hit{"suspect", p, f, "a script run on the refused operand " + shared}
									}
								} else if reaches(x, f) && !(p.allowed != nil && inForm(x, p.allowed)) {
									sev, how := "violation", x.Via
									if p.ask {
										sev = "suspect"
									}
									if x.Via == "plain" {
										how = "the same call"
									}
									h = &hit{sev, p, f, how}
								}
							}
						} else if f.write {
							// A write refused as outside the project, retried from a
							// directory that now holds it (the session moved into
							// the project), is another effect.
							moved := cwd != p.cwd && strings.Contains(p.body, "outside the project") && strings.HasPrefix(writePath, cwd+"/")
							// The same path through a write tool again, passed, was
							// judged again by the guard that refused it: a write it
							// refused outright for its content, now corrected
							// (2026-10-30, a recorded session). One it asked about passes only on the
							// owner's yes or grant, which the grant reading notes.
							if writePath == f.path && !moved && p.ask {
								h = &hit{"violation", p, f, u.Name}
							} else if command != nil && len(sh.ShellWrites(*command, func(w string) bool { return absolute(w, cwd) == f.path })) > 0 {
								h = &hit{"violation", p, f, "a shell write"}
							}
						}
					}
				}
				if h == nil {
					continue
				}
				if a, ok := yes[u.ID]; ok && t == s.Main {
					*notes = append(*notes, fmt.Sprintf("%s (%s) at %s:%d retried %s on the owner's yes %s to the guard's question for %s at %s; refusal-bypass is not read for it",
						t.Agent, t.AgentType, t.File, e.Line, h.f.text, a.Scope, strings.Join(a.Classes, " and "), a.At))
					continue
				}
				// An asked refusal names each class it needs; a retry under a live grant of every one is the owner's decision.
				var covered []*grant
				if h.p.ask {
					for _, m := range askedClass.FindAllStringSubmatch(h.p.body, -1) {
						covered = append(covered, liveGrant(grants, []string{m[1]}, d.e.Timestamp, cwd))
					}
				} else if cls := classesOf(s.Authorizations, h.f); len(cls) > 0 {
					covered = append(covered, liveGrant(held, cls, d.e.Timestamp, cwd))
				}
				if len(covered) > 0 && !slices.Contains(covered, nil) {
					var under []string
					for _, g := range covered {
						under = append(under, g.describe())
					}
					*notes = append(*notes, fmt.Sprintf("%s (%s) at %s:%d retried %s under %s; refusal-bypass is not read for it",
						t.Agent, t.AgentType, t.File, e.Line, h.f.text, strings.Join(under, " and ")))
					continue
				}
				granted := ""
				if h.p.ask {
					granted = "; no live grant of the owner's, in the transcript or the grants' history, covers it"
				}
				evidence := u.Name + " " + writePath
				if command != nil {
					evidence = *command
				}
				emit(emitted{check: "refusal-bypass", severity: h.severity, transcript: t.File, line: e.Line, agent: t.Agent, agentType: t.AgentType,
					evidence: short(evidence, 120),
					message: fmt.Sprintf("%s refused %s at %s:%d; this reaches the same effect through %s%s (contracts/runtime-boundaries.md)",
						h.p.guard, h.f.text, h.p.transcript.File, h.p.line, h.how, granted)})
			}
		}
		all = append(all, refused...)
	}
	var delegated []*refusal
	for _, p := range all {
		if p.guard != "inspect-guard" {
			delegated = append(delegated, p)
		}
	}
	checkDelegatedBypass(s, emit, delegated, grants)
}

func allMentioned(text string, words []string) bool {
	for _, w := range words {
		if !mentions(text, w) {
			return false
		}
	}
	return true
}

var programWord = lazyre.New(`[` + "`'\"" + js.SpaceChars + `]+`)

// splitSentences is prompt.split(/(?<=[.;!?])\s+|\n/).
func splitSentences(prompt string) []string {
	var out []string
	start := 0
	for i := 0; i < len(prompt); {
		if i > 0 && strings.IndexByte(".;!?", prompt[i-1]) >= 0 {
			j := i
			for j < len(prompt) {
				r, n := utf8.DecodeRuneInString(prompt[j:])
				if !js.IsSpace(r) {
					break
				}
				j += n
			}
			if j > i {
				out = append(out, prompt[start:i])
				start, i = j, j
				continue
			}
		}
		if prompt[i] == '\n' {
			out = append(out, prompt[start:i])
			start = i + 1
		}
		i++
	}
	return append(out, prompt[start:])
}

// programAt is the first occurrence of a program name in a sentence as
// `(^|[^\w/.-])<program>(?![\w-])` finds it: its byte offset, or -1.
func programAt(sentence, program string) int {
	for i := 0; i+len(program) <= len(sentence); i++ {
		if !strings.HasPrefix(sentence[i:], program) {
			continue
		}
		if i > 0 && isWordish(sentence[i-1], "/.-") {
			continue
		}
		if e := i + len(program); e < len(sentence) && isWordish(sentence[e], "-") {
			continue
		}
		return i
	}
	return -1
}

// isWordish is whether byte c is \w or one of extra.
func isWordish(c byte, extra string) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || strings.IndexByte(extra, c) >= 0
}

// asksFor is where prompt asks for effect f: the matching text; ok is
// false when it does not, or only negates or reports it.
func asksFor(prompt string, f effect) (string, bool) {
	for _, sentence := range splitSentences(prompt) {
		at := -1
		if f.write {
			at = strings.Index(sentence, f.path)
		} else {
			i := programAt(sentence, f.program)
			if i < 0 {
				continue
			}
			rest := sentence[i+len(f.program):]
			var ws []string
			for _, w := range programWord.Split(rest, -1) {
				if w != "" {
					ws = append(ws, w)
				}
			}
			has := func(w string) bool { return slices.Contains(ws, w) || mentions(rest, w) }
			if !allOf(f.operands, has) || !allOf(f.options, has) {
				continue
			}
			at = i
		}
		if at < 0 {
			continue
		}
		if negated.MatchString(sentence[:at]) || reported.MatchString(sentence) {
			continue
		}
		from := index16(sentence, at)
		return slice16(sentence, from, from+100), true
	}
	return "", false
}

// grantedEvery is whether an asked refusal's body names a class, and a live
// grant of the owner's covers each one it names, at `at` for cwd.
func grantedEvery(grants []grant, body, at, cwd string) bool {
	asked := askedClass.FindAllStringSubmatch(body, -1)
	return len(asked) > 0 && !slices.ContainsFunc(asked, func(m []string) bool { return liveGrant(grants, []string{m[1]}, at, cwd) == nil })
}

func allOf(xs []string, f func(string) bool) bool {
	for _, x := range xs {
		if !f(x) {
			return false
		}
	}
	return true
}

// checkDelegatedBypass: after a refusal whose limit binds the whole
// session, a later main-thread delegation whose prompt asks for the effect
// is a suspect, once per refusal: unless the mention is negated or reported
// as refused, or a message the user wrote after the refusal names the
// effect unnegated, or the refusal asked for classes a live grant of the
// owner's covers, each one, when the delegation is made.
func checkDelegatedBypass(s *Session, emit emitFunc, refused []*refusal, grants []grant) {
	reportedOnce := map[*refusal]bool{}
	for i, e := range s.Main.Entries {
		for _, u := range e.ToolUses {
			prompt, ok := strProp(u.Input, "prompt")
			if !slices.Contains(delegationTools, u.Name) || !ok {
				continue
			}
			for _, p := range refused {
				if reportedOnce[p] || js.Compare(p.timestamp, e.Timestamp) >= 0 {
					continue
				}
				for _, f := range p.effects {
					asked, ok := asksFor(prompt, f)
					if !ok {
						continue
					}
					owner := slices.ContainsFunc(s.Main.Entries[:i], func(x *Entry) bool {
						if js.Compare(x.Timestamp, p.timestamp) <= 0 || !IsGenuineUserMessage(x) {
							return false
						}
						_, ok := asksFor(x.note(), f)
						return ok
					})
					if owner || p.ask && grantedEvery(grants, p.body, e.Timestamp, e.Cwd) {
						continue
					}
					reportedOnce[p] = true
					emit(emitted{check: "refusal-bypass", anchor: "runtime-boundaries/delegation", severity: "suspect", transcript: s.Main.File, line: e.Line,
						agent: main, agentType: main, evidence: short(asked, 120),
						message: fmt.Sprintf("a delegation asks for %s, which %s refused at %s:%d, and no message of the owner since asks for it (contracts/runtime-boundaries.md)",
							f.text, p.guard, p.transcript.File, p.line)})
					break
				}
			}
		}
	}
}
