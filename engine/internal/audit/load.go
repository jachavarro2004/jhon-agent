package audit

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/rename"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The normalised session (LoadSession): every transcript line
// validated, and anything the reader cannot vouch for an AuditError on which
// no check runs.

// SupportedVersions are the Claude Code versions whose transcript format was
// verified against a real session. The version is no gate (docket AY B6):
// the shape is, line by line (required fields, message content, line
// types), and fails closed; a session of another version is read by its
// shape and a note says so. 2.1.278 was checked against session 80e15f89, 2.1.280 against the
// fixtures under test/testdata/audit, 2.1.289 against c0197ff5, 0809c288,
// and b2de037d, 2.1.292 against 1ae89fce, 2.1.293 against 9e887528 and
// 45dd4c7c, 2.1.296 against 865fe832 and de2db623, and 2.1.304 against
// ac9a28f7.
var SupportedVersions = []string{"2.1.278", "2.1.280", "2.1.289", "2.1.292", "2.1.293", "2.1.296", "2.1.299", "2.1.304"}

// required is the line types that carry conversation, and the fields each must have.
var required = map[string][]string{
	"user":       {"uuid", "sessionId", "version", "timestamp", "message"},
	"assistant":  {"uuid", "sessionId", "version", "timestamp", "message"},
	"attachment": {"uuid", "sessionId", "version", "timestamp"},
	"system":     {"uuid", "sessionId", "version", "timestamp"},
}

// metadataTypes are bookkeeping line types: they carry no conversation, so
// only their `type` is required; a type in neither list is an audit-error,
// because a new kind of line may carry what a check must read. The
// api-request lines (2.1.304) record each API request by digest.
var metadataTypes = []string{
	"last-prompt", "mode", "permission-mode", "atis-latch", "file-history-snapshot", "file-history-delta",
	"ai-title", "agent-name", "agent-setting", "custom-title", "queue-operation", "cost-state", "continued-in", "pr-link",
	"api-request", "api-request-blob", "api-request-shape",
}

// ToolUse is one tool call; Input is its input as parsed ({} when absent).
type ToolUse struct {
	ID, Name string
	Input    any
	// named is the change the call names (namedBy), read once at load.
	named string
}

// ToolResult is one tool result. Text is the full output: for a result the
// runtime persisted, the file it saved in the session's tool-results
// directory. Shown is what the agent received, the preview of a persisted result.
type ToolResult struct {
	ID      string
	IsError bool
	Text    string
	Shown   string
}

// Usage is the token counts of one API response, as `message.usage` records them.
type Usage struct{ Input, CacheCreation, CacheRead float64 }

// Entry is one user or assistant line. A nil pointer is a JSON null.
type Entry struct {
	Line      int
	Role      string
	Timestamp string
	Version   string
	// Cwd is the working directory the line records, "" for none.
	Cwd string
	// MessageID is `message.id` of an assistant line: every line of one response carries the same id.
	MessageID *string
	Usage     *Usage
	IsMeta    bool
	// Denial is the runtime's toolDenialKind, "" for none.
	Denial string
	// Origin is `origin.kind` of a user entry: `human` for a typed message, `task-notification` for a runtime one.
	Origin *string
	// Prompt is a user message given as a plain string: a prompt or a notification.
	Prompt      *string
	Texts       []string
	ToolUses    []ToolUse
	ToolResults []ToolResult
	// job is, for pi's background-job notice (piJobNotice), how the job ended.
	job *jobNotice
	// delivers is, for pi's notice that a background sub-agent ended
	// (piJobNotice), the call that launched it; delivery is its answer.
	delivers, delivery string
}

// note is `e.prompt ?? e.texts.join("\n")`.
func (e *Entry) note() string {
	if e.Prompt != nil {
		return *e.Prompt
	}
	return strings.Join(e.Texts, "\n")
}

// Transcript is one agent's normalised transcript.
type Transcript struct {
	File, Agent, AgentType string
	Entries                []*Entry
}

// Subagent is a sub-agent's transcript and its launch; change, phase, and
// root are read once from the launch (changeOf, phaseOf, rootOf).
type Subagent struct {
	Transcript
	ToolUseID    string
	LaunchPrompt string
	LaunchLine   int
	// Parent is the sub-agent that launched it ("" for the main thread):
	// Claude Code records a nested one's parentAgentId in its meta file,
	// and its toolUseId is a call of that parent's transcript.
	Parent string

	change, phase, root string
}

// Session is a loaded session.
type Session struct {
	deliveries func() *deliveryIndex

	Runtime   string
	Version   string
	SessionID string
	// Dir is the directory holding the main transcript; "" when unknown.
	Dir         string
	Main        *Transcript
	Subagents   []*Subagent
	SessionKind []string
	Settings    []string
	Names       []string
	Entrypoints []string
	// ReadNotes say how the reader read what it does not vouch for: a
	// version no real session verified, a message-less unknown line type.
	ReadNotes []string
	// Authorizations are the owner's yeses to a guard's question the session
	// records (pi's jhon-owner-authorization entries); Claude Code records none.
	Authorizations []Authorization
	// Compactions are the main thread's: when, what started it, its context
	// before, and how long it took (0 when the runtime does not say).
	Compactions []Compaction
	// Resumes are the main transcript's lines where the owner reopened the
	// session (pi's piSessionEntry after the conversation began); Claude
	// Code records none.
	Resumes []int
}

// Compaction is one compaction of a thread's context.
type Compaction struct {
	At           string
	Trigger      string
	TokensBefore float64
	DurationMs   float64
}

// Authorization is one yes of the owner's to a guard's question: the
// classes it asked for, when, and its scope: once (the call it answered) or
// grant (a jhon grant, which the grants' history holds too); and the call
// it answered, "" when none can be told.
type Authorization struct {
	Classes   []string
	Scope     string
	At        string
	Ask       string
	ToolUseID string
}

// transcripts is [s.main, ...s.subagents].
func (s *Session) transcripts() []*Transcript {
	out := []*Transcript{s.Main}
	for _, a := range s.Subagents {
		out = append(out, &a.Transcript)
	}
	return out
}

// own is the change of the i-th of transcripts(): "" for the main thread.
func (s *Session) own(i int) string {
	if i == 0 {
		return ""
	}
	return s.Subagents[i-1].change
}

// AuditError is a problem that stops the audit.
type AuditError struct {
	Transcript string
	Line       int
	Message    string
}

const main = "main"

// contentText is a message content as text: a string, or the text of each part.
func contentText(c any) string {
	switch x := c.(type) {
	case string:
		return x
	case []any:
		var b strings.Builder
		for _, p := range x {
			if t, ok := strProp(p, "text"); ok {
				b.WriteString(t)
			}
		}
		return b.String()
	}
	return ""
}

func usageOf(u any) *Usage {
	switch u.(type) {
	case *js.Object, []any:
		return &Usage{count(prop(u, "input_tokens")), count(prop(u, "cache_creation_input_tokens")), count(prop(u, "cache_read_input_tokens"))}
	}
	return nil
}

var savedTo = lazyre.New(`Full output saved to: (` + nonSpace + `+)`)

// fullText is the full text of a result the runtime persisted: its preview
// names the saved file, which is read from the session's own tool-results
// directory by its name, and nowhere else. The preview when the file is not there.
func fullText(shown, toolResults string) string {
	if !strings.HasPrefix(shown, "<persisted-output>") {
		return shown
	}
	m := savedTo.FindStringSubmatch(shown)
	if m == nil {
		return shown
	}
	file := filepath.Join(toolResults, baseName(m[1]))
	if st, err := os.Stat(file); err != nil || !st.Mode().IsRegular() {
		return shown
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return shown
	}
	return js.DecodeUTF8(b)
}

// nonEmpty is v when it is a non-empty string, else "".
func nonEmpty(v any) string {
	s, _ := v.(string)
	return s
}

func optString(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

func entryOf(o *js.Object, line int, toolResults string) *Entry {
	typ := prop(o, "type").(string)
	msg := prop(o, "message")
	content := prop(msg, "content")
	e := &Entry{
		Line: line, Role: typ, Timestamp: js.String(prop(o, "timestamp")), Version: js.String(prop(o, "version")),
		Cwd: nonEmpty(prop(o, "cwd")), IsMeta: isTrue(o, "isMeta"),
		Origin: optString(prop(prop(o, "origin"), "kind")), Prompt: optString(content),
	}
	if d, ok := strProp(o, "toolDenialKind"); ok {
		e.Denial = d
	}
	if typ == "assistant" {
		e.MessageID = optString(prop(msg, "id"))
		e.Usage = usageOf(prop(msg, "usage"))
	}
	parts, _ := content.([]any)
	for _, c := range parts {
		switch prop(c, "type") {
		case "text":
			if t, ok := strProp(c, "text"); ok {
				e.Texts = append(e.Texts, t)
			}
		case "tool_use":
			input := prop(c, "input")
			if js.Nullish(input) {
				input = js.NewObject()
			}
			u := ToolUse{ID: js.String(prop(c, "id")), Name: js.String(prop(c, "name")), Input: input}
			u.named = namedBy(u)
			if cmd, ok := strProp(input, "command"); ok && u.Name == "Bash" {
				raw(cmd) // parsed here, concurrently, for the checks to read
			}
			e.ToolUses = append(e.ToolUses, u)
		case "tool_result":
			shown := contentText(prop(c, "content"))
			isErr, _ := prop(c, "is_error").(bool)
			e.ToolResults = append(e.ToolResults, ToolResult{js.String(prop(c, "tool_use_id")), isErr, fullText(shown, toolResults), shown})
		}
	}
	return e
}

// queuedEntry is a `queued_command` attachment as the user entry it stands
// for: a message the user typed mid-turn, or a task notification the
// runtime delivered within a running turn instead of as a turn of its own.
func queuedEntry(o *js.Object, line int) *Entry {
	a := prop(o, "attachment")
	if !js.Truthy(a) || prop(a, "type") != "queued_command" {
		return nil
	}
	p := prop(a, "prompt")
	prompt, ok := p.(string)
	if !ok {
		prompt = contentText(p)
	}
	if prompt == "" {
		return nil
	}
	return &Entry{
		Line: line, Role: "user", Timestamp: js.String(prop(o, "timestamp")), Version: js.String(prop(o, "version")),
		Cwd: nonEmpty(prop(o, "cwd")), Origin: optString(prop(prop(a, "origin"), "kind")), Prompt: &prompt,
	}
}

// facts is what the main transcript records about the session, in first-seen order.
type facts struct {
	kinds, settings, names, entrypoints []string
	compactions                         []Compaction
}

func addOnce(xs *[]string, x string) {
	if !slices.Contains(*xs, x) {
		*xs = append(*xs, x)
	}
}

// read is one transcript file read and normalised: its entries, its
// problems, and the versions its lines state, in first-seen order.
type read struct {
	entries  []*Entry
	errors   []AuditError
	versions []string
	notes    []string
	facts    facts
}

// readTranscript validates and normalises one transcript file. agentID is
// the sub-agent's id, "" for the main thread.
func readTranscript(file, rel, agentID, toolResults string, sub bool) read {
	var r read
	err := func(line int, message string) { r.errors = append(r.errors, AuditError{rel, line, message}) }
	b, e := os.ReadFile(file)
	if e != nil {
		// A transcript that cannot be read (a link whose target is gone) is
		// its own audit-error, never a crash of the whole session's audit.
		err(0, "the transcript cannot be read: "+e.Error())
		return r
	}
	for i, raw := range strings.Split(js.DecodeUTF8(b), "\n") {
		n := i + 1
		if js.Trim(raw) == "" {
			continue
		}
		v, e := js.ParseJSON(raw)
		if e != nil {
			err(n, "line is not valid JSON (truncated or corrupt transcript)")
			continue
		}
		o, _ := v.(*js.Object)
		typ, ok := strProp(v, "type")
		if !ok {
			err(n, "line has no string type")
			continue
		}
		if !sub {
			if k, _ := strProp(o, "sessionKind"); k != "" {
				addOnce(&r.facts.kinds, k)
			}
			if k, _ := strProp(o, "entrypoint"); k != "" {
				addOnce(&r.facts.entrypoints, k)
			}
			if k, _ := strProp(o, "agentSetting"); typ == "agent-setting" && k != "" {
				addOnce(&r.facts.settings, k)
			}
			if k, _ := strProp(o, "agentName"); typ == "agent-name" && k != "" {
				addOnce(&r.facts.names, k)
			}
			if sub, _ := strProp(o, "subtype"); typ == "system" && sub == "compact_boundary" {
				md := prop(o, "compactMetadata")
				trigger, _ := strProp(md, "trigger")
				pre, _ := prop(md, "preTokens").(float64)
				ms, _ := prop(md, "durationMs").(float64)
				r.facts.compactions = append(r.facts.compactions, Compaction{js.String(prop(o, "timestamp")), trigger, pre, ms})
			}
		}
		fields, ok := required[typ]
		if !ok {
			// A line type the reader does not know is bookkeeping only when it
			// carries no message: one that does may hold conversation.
			switch {
			case slices.Contains(metadataTypes, typ):
			case js.Nullish(prop(o, "message")):
				addOnce(&r.notes, "line type "+typ+" carries no message; read as bookkeeping")
			default:
				err(n, "unknown line type "+typ)
			}
			continue
		}
		for _, k := range fields {
			if js.Nullish(prop(o, k)) {
				err(n, fmt.Sprintf("missing required field %s (%s)", k, typ))
			}
		}
		if ver, ok := strProp(o, "version"); ok {
			addOnce(&r.versions, ver)
		}
		if typ == "attachment" {
			if q := queuedEntry(o, n); q != nil {
				r.entries = append(r.entries, q)
			}
			continue
		}
		if typ != "user" && typ != "assistant" {
			continue
		}
		if id := prop(o, "agentId"); sub && id != any(agentID) {
			err(n, fmt.Sprintf("agentId %s does not match the transcript agent %s", js.String(id), agentID))
		}
		msg := prop(o, "message")
		content := prop(msg, "content")
		_, isStr := content.(string)
		_, isList := content.([]any)
		if !isMessage(msg) || (!isStr && !isList) {
			err(n, fmt.Sprintf("message has no content (%s)", typ))
			continue
		}
		if typ == "assistant" && !isList {
			err(n, "assistant message content is not a list")
			continue
		}
		r.entries = append(r.entries, entryOf(o, n, toolResults))
	}
	return r
}

// isMessage is typeof message === "object": an object, an array, or null.
func isMessage(v any) bool {
	switch v.(type) {
	case nil, *js.Object, []any:
		return true
	}
	return false
}

var (
	metaName  = lazyre.New(`^agent-(.+)\.meta\.json$`)
	agentName = lazyre.New(`^agent-(.+)\.jsonl$`)
)

// LoadSession parses a session into a Session, or the errors that prevent
// it. The sub-agents are `<session>/subagents/agent-<id>.jsonl`, each with a
// `.meta.json` naming its agentType and the main-thread tool_use that
// launched it; persisted results are in `<session>/tool-results/`. Every
// transcript is read concurrently; the results merge in reading order (the
// main transcript, then each sub-agent's by name), so errors and versions
// keep that order.
func LoadSession(mainPath string) (*Session, []AuditError) {
	if IsPiSession(mainPath) {
		return loadPiSession(mainPath) // pi.go
	}
	root := filepath.Dir(mainPath)
	sessionID := strings.TrimSuffix(filepath.Base(mainPath), ".jsonl")
	toolResults := filepath.Join(root, sessionID, "tool-results")
	rel := func(p string) string {
		r, err := filepath.Rel(root, p)
		if err != nil {
			return p
		}
		return r
	}
	dir := filepath.Join(root, sessionID, "subagents")
	var names []string
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			names = append(names, e.Name())
		}
	} else if !os.IsNotExist(err) {
		panic(err.Error())
	}
	slices.SortFunc(names, js.Compare)
	type subFile struct{ id, file, metaFile string }
	var subs []subFile
	for _, name := range names {
		if m := agentName.FindStringSubmatch(name); m != nil {
			subs = append(subs, subFile{m[1], filepath.Join(dir, name), filepath.Join(dir, "agent-"+m[1]+".meta.json")})
		}
	}

	reads := make([]read, len(subs)+1)
	var wg sync.WaitGroup
	panics := make([]any, len(subs)+1)
	run := func(i int, f func() read) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { panics[i] = recover() }()
			reads[i] = f()
		}()
	}
	run(0, func() read { return readTranscript(mainPath, rel(mainPath), "", toolResults, false) })
	for i, sf := range subs {
		run(i+1, func() read { return readTranscript(sf.file, rel(sf.file), sf.id, toolResults, true) })
	}
	wg.Wait()
	for _, p := range panics {
		if p != nil {
			panic(p)
		}
	}

	errors := reads[0].errors
	versions := reads[0].versions
	m := &Transcript{File: rel(mainPath), Agent: main, AgentType: main, Entries: reads[0].entries}
	launched := map[string]bool{}
	for _, e := range m.Entries {
		for _, t := range e.ToolUses {
			launched[t.ID] = true
		}
	}
	// A nested sub-agent's launch is a call of its parent's transcript.
	launchedBy := map[string]map[string]bool{}
	for i, sf := range subs {
		calls := map[string]bool{}
		for _, e := range reads[i+1].entries {
			for _, t := range e.ToolUses {
				calls[t.ID] = true
			}
		}
		launchedBy[sf.id] = calls
	}
	nested := 0
	for _, name := range names {
		if mm := metaName.FindStringSubmatch(name); mm != nil && !slices.Contains(names, "agent-"+mm[1]+".jsonl") {
			errors = append(errors, AuditError{rel(filepath.Join(dir, name)), 0, "meta file has no transcript"})
		}
	}
	var subagents []*Subagent
	for i, sf := range subs {
		metaRel := rel(sf.metaFile)
		var meta any
		if _, err := os.Stat(sf.metaFile); err != nil {
			errors = append(errors, AuditError{rel(sf.file), 0, "sub-agent transcript has no .meta.json"})
		} else if b, err := os.ReadFile(sf.metaFile); err != nil {
			panic(err.Error())
		} else if meta, err = js.ParseJSON(js.DecodeUTF8(b)); err != nil {
			errors = append(errors, AuditError{metaRel, 0, "meta file is not valid JSON"})
		}
		if js.Truthy(meta) {
			for _, k := range []string{"agentType", "toolUseId"} {
				if s, _ := strProp(meta, k); s == "" {
					errors = append(errors, AuditError{metaRel, 0, "meta file has no " + k})
				}
			}
			parent, _ := strProp(meta, "parentAgentId")
			if id, ok := strProp(meta, "toolUseId"); ok && parent != "" && launchedBy[parent] != nil {
				if !launchedBy[parent][id] {
					errors = append(errors, AuditError{metaRel, 0, "toolUseId " + id + " matches no tool_use in its parent sub-agent " + parent + "'s transcript"})
				}
				nested++
			} else if ok && !launched[id] {
				errors = append(errors, AuditError{metaRel, 0, "toolUseId " + id + " matches no tool_use in the main transcript"})
			}
		}
		r := reads[i+1]
		errors = append(errors, r.errors...)
		for _, v := range r.versions {
			addOnce(&versions, v)
		}
		launch := ""
		unreadable := len(r.entries) == 0 && len(r.errors) > 0 && r.errors[0].Line == 0
		if !unreadable && (len(r.entries) == 0 || r.entries[0].Role != "user" || r.entries[0].Line != 1 || (r.entries[0].Prompt == nil && len(r.entries[0].Texts) == 0)) {
			errors = append(errors, AuditError{rel(sf.file), 1, "line 1 is not the launch prompt"})
		}
		if len(r.entries) > 0 {
			launch = r.entries[0].note()
		}
		a := &Subagent{
			Transcript: Transcript{File: rel(sf.file), Agent: sf.id, AgentType: rename.AgentNow(metaString(meta, "agentType")), Entries: r.entries},
			ToolUseID:  metaString(meta, "toolUseId"), LaunchPrompt: launch, LaunchLine: 1,
		}
		if parent := metaString(meta, "parentAgentId"); launchedBy[parent] != nil {
			a.Parent = parent
		}
		a.change, a.phase, a.root = changeOf(a.LaunchPrompt), phaseOf(a.LaunchPrompt, a.AgentType), rootOf(a)
		subagents = append(subagents, a)
	}
	if len(versions) == 0 {
		errors = append(errors, AuditError{m.File, 0, "no line states a Claude Code version"})
	}
	if len(errors) > 0 {
		return nil, errors
	}
	var notes []string
	if n := unverifiedNote(versions); n != "" {
		notes = append(notes, n)
	}
	if nested > 0 {
		notes = append(notes, fmt.Sprintf("%d sub-agent(s) were launched by another sub-agent, not the main thread: each is read with its parent's launch", nested))
	}
	for _, r := range reads {
		for _, n := range r.notes {
			addOnce(&notes, n)
		}
	}
	f := reads[0].facts
	return newSession(&Session{Runtime: "claude-code", Version: versions[0], SessionID: sessionID, Dir: root, Main: m, Subagents: subagents,
		SessionKind: f.kinds, Settings: f.settings, Names: f.names, Entrypoints: f.entrypoints, ReadNotes: notes, Compactions: f.compactions}), nil
}

// metaString is String(meta?.[k] ?? "").
func metaString(meta any, k string) string { return orString(prop(meta, k), "") }

func joinVersions() string { return strings.Join(SupportedVersions, ", ") }

// unverifiedNote is one note naming every Claude Code version no real
// session verified, "" when there is none (L48).
func unverifiedNote(versions []string) string {
	var unverified []string
	for _, v := range versions {
		if !slices.Contains(SupportedVersions, v) && !slices.Contains(unverified, v) {
			unverified = append(unverified, v)
		}
	}
	switch len(unverified) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("Claude Code %s is read by its shape: no real session verified it (verified: %s)", unverified[0], joinVersions())
	}
	return fmt.Sprintf("Claude Code %s are read by their shape: no real session verified them (verified: %s)", strings.Join(unverified, ", "), joinVersions())
}
