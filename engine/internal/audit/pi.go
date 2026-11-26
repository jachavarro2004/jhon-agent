package audit

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/rename"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// A pi session (LoadSession on a file whose first line is pi's session
// header), read into the same normalised model as a Claude Code session so
// that every check runs on it unchanged.
//
// The main session is `<dir>/<timestamp>_<id>.jsonl`. Each sub-agent Jhon's
// `subagent` tool ran (shell/lib/agents.ts) is a child session saved under
// `<dir>/<timestamp>_<id>/subagents/`: `<timestamp>_<child id>.jsonl`, and
// `<child id>.meta.json` naming its agent (agentType), the main-session call
// that launched it (toolUseId), and the parent session (parentSession).
//
// The tools map onto the names the checks read: bash → Bash (command), write
// → Write (file_path, content), edit → Edit (file_path, old_string,
// new_string) or MultiEdit for several edits, read → Read (file_path),
// grep → Grep, find → Glob, ls → LS, subagent → Agent (subagent_type,
// prompt), ask_user → AskUserQuestion, exit_plan_mode → ExitPlanMode, and
// the reviewers' inspect → Bash (command), since it runs one command the
// inspect guard admits. MCP tools keep pi's names, `mcp__<server>__<tool>`,
// which are Claude Code's.
//
// A call an extension blocked on pi's tool_call never ran: its result is an
// error with no `durationMs`. Jhon's guards block that way, with the guard's
// own message (`safety-guard: refused: …`), the message a Claude Code hook
// error carries after its prefix; such a result is read as the hook error
// it stands for, `PreToolUse:<tool> hook error: [pi tool_call]: <message>`,
// so refusal-bypass reads it as it reads Claude Code's. The inspect tool
// runs the inspect guard itself, so its error that is the guard's message
// is read the same way. A block whose message ends `: the owner did not
// authorize it` is the owner declining the guard's question: a
// `user-rejected` denial, as Claude Code records one.

// A main session Jhon's extension ran records, at each start, a custom
// entry piSessionEntry with pi's mode and the agent it runs as: the mode is
// the session kind (tui is interactive, print and json are print, as
// `claude -p` is), and the agent its agent setting. pi appends a resumed
// session to the same file with no second header, so such an entry after
// the conversation began is a resume (Session.Resumes), unless its reason,
// pi's session_start reason, says a /reload.

// PiSessionVersions are pi's session format versions verified against a real
// session: 3 against the fixtures test/testdata/audit/pi-* (pi 1.4.0). Any
// other is an audit-error, as an unverified Claude Code version is.
var PiSessionVersions = []string{"3"}

// piMetadata are the entry types that carry no conversation the checks read.
var piMetadata = []string{
	"model_change", "thinking_level_change", "usage", "compaction", "context_edit",
	"branch_summary", "custom", "label", "session_info",
}

// piRoles are the message roles: system and bashExecution (the owner's own
// `!` command, not a call of the agent's) carry nothing the checks read.
var piRoles = []string{"system", "user", "assistant", "toolResult", "bashExecution", "custom", "branchSummary", "compactionSummary"}

// piTools are pi's tool names and the Claude Code names the checks read.
var piTools = map[string]string{
	"bash": "Bash", "inspect": "Bash", "write": "Write", "read": "Read", "grep": "Grep", "find": "Glob", "ls": "LS",
	// A background command is Claude Code's Bash run in the background; edit writes its file.
	"bash_background": "Bash", "edit": "Edit",
	"subagent": "Agent", "subagent_continue": "SendMessage", "ask_user": "AskUserQuestion", "exit_plan_mode": "ExitPlanMode",
}

// piRenamed are the input keys each mapped tool renames.
var piRenamed = map[string]map[string]string{
	"write": {"path": "file_path"}, "read": {"path": "file_path"}, "edit": {"path": "file_path"},
	"subagent":          {"agent": "subagent_type", "task": "prompt"},
	"subagent_continue": {"id": "to"},
}

var (
	piGuardMessage  = lazyre.New(`^[\w-]+-guard: `)
	piOwnerDeclined = lazyre.New(`: the owner did not authorize it$`)
	piChildFile     = lazyre.New(`^[^_]+_(.+)\.jsonl$`)
)

// piSessionEntry is the custom entry the main session records at its start (shell/extensions/jhon.ts SESSION_ENTRY).
const piSessionEntry = "jhon.session/v1"

// piAuthorizationEntry is the custom entry the main session records when the
// owner answers a guard's question with a yes (shell/extensions/jhon.ts
// AUTHORIZATION_ENTRY): its classes, and whether it was once or a grant.
// The extension records it on tool_call, after the assistant message that
// made the call and before its result, and names no call id: the call it
// answered is its toolCallId when it names one, else the pending call (no
// result yet) of its tool whose command names the ask's forms, else the
// first pending call of its tool, else the next call of its tool.
const piAuthorizationEntry = "jhon-owner-authorization"

// piJobNotice is the custom message Jhon's tasks extension sends when a
// background job ends (shell/lib/background-tasks-tools.ts
// TASK_NOTICE_TYPE): `<job> exited with code <n> …`, `Command: <command>`,
// then `Last output:` and the job's last lines; its details carry
// jhonTasks.exitCode. When a background sub-agent ends it sends the same
// message, `Background sub-agent <task> (<agent>) <outcome> …` and the
// agent's answer: the delivery of the subagent call that started that task
// (T192 S8), read as an inline answer is.
const piJobNotice = "jhon-tasks.notice"

// piSubagentResult is the message the shell before its background tasks
// (sessions to late October 2026) sent when a background sub-agent ended:
// `Background sub-agent <task> (<agent>) completed:` and the answer; its
// subagent_result read one as task_result does now. Both deliver the
// launch as their successors do.
const piSubagentResult = "jhon-subagent.result"

var (
	noticeExit    = lazyre.New(`\bexited with code (-?\d+)\b`)
	noticeCommand = lazyre.New(`(?m)^Command: (.*)$`)
	// A background sub-agent's launch result names its task (its details
	// carry it as `task`), and its end notice, or task_result's answer once
	// it ended, opens with the same task (details jhonTasks.id, kind agent)
	// and how it ended, its answer after a blank line
	// (shell/lib/background-tasks-tools.ts taskText); one still running, or
	// stopped before its answer, gave none. The notice opens with the shell's
	// mark (ShellMark, T194); task_result's answer does not.
	agentStarted = lazyre.New(`^Started background sub-agent (\S+) \(`)
	// A subagent_continue's result names the child session it continued and
	// the task it runs as (its details carry the task as `task` too):
	// `Continued sub-agent <child> (<agent>) in the background as <task>.`
	// (shell/extensions/jhon.ts). That task's answer delivers the call that
	// launched the child, which its meta file names (toolUseId).
	agentContinued = lazyre.New(`^Continued sub-agent (\S+) \([^)\n]*\) in the background as (\S+?)\.(?:\s|$)`)
	agentEnded     = lazyre.New(`^(?:` + regexp.QuoteMeta(ShellMark) + ` )?Background sub-agent (\S+) \([^)\n]*\) (?:completed|failed)\b`)
)

// startedTask is the background task a subagent call's result says it
// started; "" for an inline run.
func startedTask(text string, details any) string {
	if id, _ := strProp(details, "task"); id != "" && agentStarted.MatchString(text) {
		return id
	}
	if m := agentStarted.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// continuedTask is the child session a subagent_continue's result says it
// continued and the background task it runs as; "" for none.
func continuedTask(text string, details any) (child, task string) {
	m := agentContinued.FindStringSubmatch(text)
	if m == nil {
		return "", ""
	}
	task = m[2]
	if id, _ := strProp(details, "task"); id != "" {
		task = id
	}
	return m[1], task
}

// endedAgent is the background sub-agent task a jhon-tasks.notice or a
// task_result says ended, and its answer; "" for a job's or a monitor's, or
// for an agent still running.
func endedAgent(text string, details any) (task, answer string) {
	m := agentEnded.FindStringSubmatch(text)
	if m == nil {
		return "", ""
	}
	task = m[1]
	if d := prop(details, "jhonTasks"); js.Truthy(d) {
		kind, _ := strProp(d, "kind")
		status, _ := strProp(d, "status")
		if kind != "agent" || status == "running" {
			return "", ""
		}
		if id, _ := strProp(d, "id"); id != "" {
			task = id
		}
	}
	_, answer, _ = strings.Cut(text, "\n\n")
	return task, answer
}

// readJobNotice is how a job notice says its job ended; nil when it shows no command.
func readJobNotice(text string, details any) *jobNotice {
	c := noticeCommand.FindStringSubmatch(text)
	if c == nil {
		return nil
	}
	j := &jobNotice{command: c[1]}
	if code, ok := prop(prop(details, "jhonTasks"), "exitCode").(float64); ok {
		n := int(code)
		j.exit = &n
	} else if m := noticeExit.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		j.exit = &n
	}
	if _, last, ok := strings.Cut(text, "\nLast output:\n"); ok {
		last, _, _ = strings.Cut(last, "\nFull output: ")
		j.output = last
	}
	return j
}

// piModeKind is the session kind a pi mode stands for.
var piModeKind = map[string]string{"tui": "interactive", "print": "print", "json": "print"}

// IsPiSession is whether the file at path is a pi session: its first line is pi's session header.
func IsPiSession(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	line, _, _ := strings.Cut(string(buf[:n]), "\n")
	v, err := js.ParseJSON(line)
	if err != nil {
		return false
	}
	typ, _ := strProp(v, "type")
	return typ == "session"
}

// piInput is a pi tool call's name and arguments as the checks read them.
func piInput(name string, args any) (string, any) {
	if js.Nullish(args) {
		args = js.NewObject()
	}
	o, ok := args.(*js.Object)
	if !ok {
		return name, args
	}
	if name == "edit" {
		return piEdit(o)
	}
	if name == "ask_user" {
		// Its questions are AskUserQuestion's own; a session from before
		// they were asked one question with plain-text options.
		if qs, ok := propList(o, "questions"); ok {
			return "AskUserQuestion", js.Obj("questions", qs)
		}
		var options []any
		list, _ := propList(o, "options")
		for _, x := range list {
			options = append(options, js.Obj("label", x))
		}
		if options == nil {
			options = []any{}
		}
		return "AskUserQuestion", js.Obj("questions", []any{js.Obj("question", prop(o, "question"), "options", options)})
	}
	mapped, ok := piTools[name]
	if !ok {
		return name, o
	}
	renamed := piRenamed[name]
	out := js.NewObject()
	for _, k := range o.Keys() {
		key := k
		if r, ok := renamed[k]; ok {
			key = r
		}
		out.Set(key, prop(o, k))
	}
	return mapped, out
}

// piEdit is pi's edit, `{path, edits: [{oldText, newText}]}` (or one
// oldText/newText at the top level): an Edit for one replacement, else a MultiEdit.
func piEdit(o *js.Object) (string, any) {
	pair := func(x any) *js.Object {
		return js.Obj("old_string", prop(x, "oldText"), "new_string", prop(x, "newText"))
	}
	edits, ok := propList(o, "edits")
	if !ok {
		edits = []any{o}
	}
	if len(edits) == 1 {
		p := pair(edits[0])
		return "Edit", js.Obj("file_path", prop(o, "path"), "old_string", prop(p, "old_string"), "new_string", prop(p, "new_string"))
	}
	var list []any
	for _, x := range edits {
		list = append(list, pair(x))
	}
	if list == nil {
		list = []any{}
	}
	return "MultiEdit", js.Obj("file_path", prop(o, "path"), "edits", list)
}

// piRead is one pi session file read and normalised.
type piRead struct {
	read
	id, cwd, version string
	// kinds and agents are what its piSessionEntry entries record.
	kinds, agents []string
	// authorizations are the owner's yeses its piAuthorizationEntry entries record.
	authorizations []Authorization
	// resumes are the lines of its piSessionEntry entries that follow a
	// conversation entry: each is the owner reopening the session.
	resumes []int
	// continued are the entries that deliver the answer of a continued
	// child, by its session id: loadPiSession sets the call they deliver,
	// the one its meta file names.
	continued []continuedAnswer
}

// continuedAnswer is an entry that delivers a continued child's answer.
type continuedAnswer struct {
	entry         *Entry
	child, answer string
}

// readPiSession validates and normalises one pi session file. A result is
// read for the tool its call named; the result's own toolName otherwise.
func readPiSession(file, rel string) piRead {
	var r piRead
	err := func(line int, message string) { r.errors = append(r.errors, AuditError{rel, line, message}) }
	b, e := os.ReadFile(file)
	if e != nil {
		panic(e.Error())
	}
	tools := map[string]string{}    // call id → pi tool name
	var pending []string            // call ids with no result yet, in call order
	launched := map[string]string{} // background sub-agent task id → the subagent call that started it
	// background sub-agent task id → the child session a subagent_continue runs again as it
	continued := map[string]string{}
	// deliver is what an answer of the background task delivers: its launch, or its continued child's.
	deliver := func(e *Entry, task, answer string) {
		if call := launched[task]; call != "" {
			e.delivers, e.delivery = call, answer
		} else if child := continued[task]; child != "" {
			r.continued = append(r.continued, continuedAnswer{e, child, answer})
		}
	}
	commands := map[string]string{}
	answered := map[string]bool{}
	type awaited struct {
		index int
		tool  string
	}
	var awaiting []awaited // authorizations that answered a call not made yet
	header := false
	for i, rawLine := range strings.Split(js.DecodeUTF8(b), "\n") {
		n := i + 1
		if js.Trim(rawLine) == "" {
			continue
		}
		v, e := js.ParseJSON(rawLine)
		if e != nil {
			err(n, "line is not valid JSON (truncated or corrupt session)")
			continue
		}
		o, _ := v.(*js.Object)
		typ, ok := strProp(v, "type")
		if !ok {
			err(n, "line has no string type")
			continue
		}
		if !header {
			header = true
			if typ != "session" {
				err(n, "line 1 is not a pi session header")
				continue
			}
			r.id, _ = strProp(o, "id")
			r.cwd, _ = strProp(o, "cwd")
			r.version = orString(prop(o, "version"), "")
			if r.id == "" {
				err(n, "the session header has no id")
			}
			if r.version == "" {
				err(n, "the session header states no version")
			} else {
				addOnce(&r.versions, r.version)
				if !slices.Contains(PiSessionVersions, r.version) {
					err(n, fmt.Sprintf("unsupported pi session version %s; supported: %s", r.version, strings.Join(PiSessionVersions, ", ")))
				}
			}
			continue
		}
		if typ == "session" {
			err(n, "a second session header")
			continue
		}
		for _, k := range []string{"id", "timestamp"} {
			if js.Nullish(prop(o, k)) {
				err(n, fmt.Sprintf("missing required field %s (%s)", k, typ))
			}
		}
		if typ == "custom" && prop(o, "customType") == piAuthorizationEntry {
			data := prop(o, "data")
			var classes []string
			if list, ok := prop(data, "classes").([]any); ok {
				for _, c := range list {
					if s, ok := c.(string); ok && s != "" {
						classes = append(classes, s)
					}
				}
			}
			at, _ := strProp(o, "timestamp")
			scope, _ := strProp(data, "scope")
			ask, _ := strProp(data, "ask")
			tool, _ := strProp(data, "tool")
			if len(classes) > 0 && at != "" {
				call, _ := strProp(data, "toolCallId")
				if call == "" {
					call = answeredCall(pending, tools, commands, answered, tool, ask)
				}
				if call == "" {
					awaiting = append(awaiting, awaited{len(r.authorizations), tool})
				} else {
					answered[call] = true
				}
				r.authorizations = append(r.authorizations, Authorization{Classes: classes, Scope: scope, At: at, Ask: ask, ToolUseID: call})
			}
		}
		if typ == "custom" && prop(o, "customType") == piSessionEntry {
			data := prop(o, "data")
			// A /reload restarts the extension in the same sitting; an entry
			// from before the reason was recorded counts as a resume.
			// A resume starts a new process whose counter hands the task ids out
			// again: none names an earlier process's launch.
			if reason, _ := strProp(data, "reason"); len(r.entries) > 0 && reason != "reload" {
				r.resumes = append(r.resumes, n)
				clear(launched)
				clear(continued)
			}
			if mode, ok := strProp(data, "mode"); ok && mode != "" {
				kind, known := piModeKind[mode]
				if !known {
					kind = mode
				}
				addOnce(&r.kinds, kind)
			}
			if agent, ok := strProp(data, "agent"); ok && agent != "" {
				addOnce(&r.agents, rename.AgentNow(agent))
			}
		}
		if typ == "compaction" {
			// pi says neither what started it nor how long it took; an extension's (the jhon shell's) is fromHook.
			trigger := ""
			if prop(o, "fromHook") == true {
				trigger = "extension"
			}
			before, _ := prop(o, "tokensBefore").(float64)
			r.facts.compactions = append(r.facts.compactions, Compaction{js.String(prop(o, "timestamp")), trigger, before, 0})
		}
		if slices.Contains(piMetadata, typ) {
			continue
		}
		base := Entry{Line: n, Timestamp: js.String(prop(o, "timestamp")), Version: r.version, Cwd: r.cwd}
		if typ == "custom_message" {
			if text := contentText(prop(o, "content")); text != "" {
				e := base
				e.Role, e.Origin, e.Prompt = "user", optString("extension"), &text
				if ct := prop(o, "customType"); ct == piJobNotice || ct == piSubagentResult {
					if ct == piJobNotice {
						e.job = readJobNotice(text, prop(o, "details"))
					}
					// T192 S8: a background sub-agent's answer delivers its launch.
					if task, answer := endedAgent(text, prop(o, "details")); task != "" {
						deliver(&e, task, answer)
					}
				}
				r.entries = append(r.entries, &e)
			}
			continue
		}
		if typ != "message" {
			err(n, "unknown entry type "+typ)
			continue
		}
		msg := prop(o, "message")
		role, _ := strProp(msg, "role")
		if !slices.Contains(piRoles, role) {
			err(n, "unknown message role "+js.String(prop(msg, "role")))
			continue
		}
		content := prop(msg, "content")
		_, isStr := content.(string)
		parts, isList := content.([]any)
		switch role {
		case "user", "custom":
			if !isStr && !isList {
				err(n, fmt.Sprintf("message has no content (%s)", role))
				continue
			}
			e := base
			e.Role = "user"
			origin := "human"
			if role == "custom" {
				origin = "extension"
			}
			e.Origin = &origin
			if isStr {
				e.Prompt = optString(content)
			}
			for _, p := range parts {
				if t, ok := strProp(p, "text"); ok && prop(p, "type") == "text" {
					e.Texts = append(e.Texts, t)
				}
			}
			r.entries = append(r.entries, &e)
		case "assistant":
			if !isList {
				err(n, "assistant message content is not a list")
				continue
			}
			e := base
			e.Role = "assistant"
			e.MessageID = optString(prop(o, "id"))
			if u := prop(msg, "usage"); js.Truthy(u) {
				e.Usage = &Usage{count(prop(u, "input")), count(prop(u, "cacheWrite")), count(prop(u, "cacheRead"))}
			}
			for _, p := range parts {
				switch prop(p, "type") {
				case "text":
					if t, ok := strProp(p, "text"); ok {
						e.Texts = append(e.Texts, t)
					}
				case "toolCall":
					id, name := js.String(prop(p, "id")), js.String(prop(p, "name"))
					tools[id] = name
					pending = append(pending, id)
					if cmd, ok := strProp(prop(p, "arguments"), "command"); ok {
						commands[id] = cmd
					}
					if k := slices.IndexFunc(awaiting, func(w awaited) bool { return w.tool == "" || w.tool == name }); k >= 0 {
						r.authorizations[awaiting[k].index].ToolUseID = id
						answered[id] = true
						awaiting = slices.Delete(awaiting, k, k+1)
					}
					mapped, input := piInput(name, prop(p, "arguments"))
					u := ToolUse{ID: id, Name: mapped, Input: input}
					u.named = namedBy(u)
					if cmd, ok := strProp(input, "command"); ok && u.Name == "Bash" {
						raw(cmd) // parsed here, concurrently, for the checks to read
					}
					e.ToolUses = append(e.ToolUses, u)
				}
			}
			r.entries = append(r.entries, &e)
		case "toolResult":
			id, ok := strProp(msg, "toolCallId")
			pending = slices.DeleteFunc(pending, func(p string) bool { return p == id })
			if !ok || !isList {
				err(n, "tool result has no toolCallId or content")
				continue
			}
			e := base
			e.Role = "user"
			shown := contentText(content)
			isErr := isTrue(msg, "isError")
			text := shown
			tool := js.String(prop(msg, "toolName"))
			if name, ok := tools[id]; ok {
				tool = name
			}
			if tool == "subagent" && !isErr {
				if task := startedTask(shown, prop(msg, "details")); task != "" {
					launched[task] = id
					delete(continued, task)
				}
			}
			if tool == "subagent_continue" && !isErr {
				if child, task := continuedTask(shown, prop(msg, "details")); task != "" {
					continued[task] = child
					delete(launched, task)
				}
			}
			// T192 S8: an answer read with task_result (subagent_result before), its notice lost, delivers its launch as the notice would.
			if (tool == "task_result" || tool == "subagent_result") && !isErr {
				if task, answer := endedAgent(shown, prop(msg, "details")); task != "" {
					deliver(&e, task, answer)
				}
			}
			mapped, _ := piInput(tool, nil)
			blocked := isErr && !has(msg, "durationMs")
			guarded := blocked || (isErr && tool == "inspect")
			switch {
			case blocked && piOwnerDeclined.MatchString(js.Trim(shown)):
				e.Denial = "user-rejected"
			case blocked || (guarded && piGuardMessage.MatchString(js.Trim(shown))):
				text = "PreToolUse:" + mapped + " hook error: [pi tool_call]: " + js.Trim(shown)
			}
			e.ToolResults = []ToolResult{{ID: id, IsError: isErr, Text: text, Shown: shown}}
			r.entries = append(r.entries, &e)
		}
	}
	if !header {
		err(0, "the session is empty")
	}
	return r
}

// answeredCall is the call an authorization of tool (pi's name, "" for any)
// for ask answered, among the pending calls no other authorization
// answered: the first whose command names every word of one of the ask's
// forms, else the first; "" for none.
func answeredCall(pending []string, tools, commands map[string]string, answered map[string]bool, tool, ask string) string {
	first := ""
	for _, id := range pending {
		if answered[id] || (tool != "" && tools[id] != tool) {
			continue
		}
		if first == "" {
			first = id
		}
		if slices.ContainsFunc(askForms(ask), func(f askForm) bool { return allMentioned(commands[id], strings.Fields(f.form)) }) {
			return id
		}
	}
	return first
}

// loadPiSession is LoadSession for a pi session: the main session and every
// child session under its subagents directory, read concurrently.
func loadPiSession(mainPath string) (*Session, []AuditError) {
	root := filepath.Dir(mainPath)
	rel := func(p string) string {
		r, err := filepath.Rel(root, p)
		if err != nil {
			return p
		}
		return r
	}
	dir := filepath.Join(strings.TrimSuffix(mainPath, ".jsonl"), "subagents")
	var names []string
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			names = append(names, e.Name())
		}
	} else if !os.IsNotExist(err) {
		panic(err.Error())
	}
	slices.SortFunc(names, js.Compare)
	type child struct{ id, file, metaFile string }
	var children []child
	for _, name := range names {
		if m := piChildFile.FindStringSubmatch(name); m != nil {
			children = append(children, child{m[1], filepath.Join(dir, name), filepath.Join(dir, m[1]+".meta.json")})
		}
	}
	reads := make([]piRead, len(children)+1)
	parallel(len(reads), func(i int) {
		if i == 0 {
			reads[0] = readPiSession(mainPath, rel(mainPath))
			return
		}
		c := children[i-1]
		reads[i] = readPiSession(c.file, rel(c.file))
	})

	m := reads[0]
	errors := m.errors
	mt := &Transcript{File: rel(mainPath), Agent: main, AgentType: main, Entries: m.entries}
	calls := map[string]string{} // main-session call id → mapped tool name
	for _, e := range mt.Entries {
		for _, u := range e.ToolUses {
			calls[u.ID] = u.Name
		}
	}
	for _, name := range names {
		if id, ok := strings.CutSuffix(name, ".meta.json"); ok && !slices.ContainsFunc(children, func(c child) bool { return c.id == id }) {
			errors = append(errors, AuditError{rel(filepath.Join(dir, name)), 0, "meta file has no child session"})
		}
	}
	var subagents []*Subagent
	launchOf := map[string]string{} // child session id → the call that launched it, as its meta file names it
	for i, c := range children {
		r := reads[i+1]
		errors = append(errors, r.errors...)
		metaRel := rel(c.metaFile)
		var meta any
		if b, err := os.ReadFile(c.metaFile); os.IsNotExist(err) {
			errors = append(errors, AuditError{rel(c.file), 0, "child session has no .meta.json"})
		} else if err != nil {
			panic(err.Error())
		} else if meta, err = js.ParseJSON(js.DecodeUTF8(b)); err != nil {
			errors = append(errors, AuditError{metaRel, 0, "meta file is not valid JSON"})
		}
		if js.Truthy(meta) {
			for _, k := range []string{"agentType", "toolUseId", "parentSession"} {
				if s, _ := strProp(meta, k); s == "" {
					errors = append(errors, AuditError{metaRel, 0, "meta file has no " + k})
				}
			}
			if id, ok := strProp(meta, "toolUseId"); ok && calls[id] != "Agent" {
				errors = append(errors, AuditError{metaRel, 0, "toolUseId " + id + " matches no subagent call in the main session"})
			}
			if p, ok := strProp(meta, "parentSession"); ok && p != "" && p != m.id {
				errors = append(errors, AuditError{metaRel, 0, "parentSession " + p + " is not the main session " + m.id})
			}
		}
		if r.id != "" && r.id != c.id {
			errors = append(errors, AuditError{rel(c.file), 1, fmt.Sprintf("session id %s does not match the file's %s", r.id, c.id)})
		}
		launch, line := "", 0
		if len(r.entries) == 0 || r.entries[0].Role != "user" || len(r.entries[0].ToolResults) > 0 || (r.entries[0].Prompt == nil && len(r.entries[0].Texts) == 0) {
			errors = append(errors, AuditError{rel(c.file), 0, "the first message is not the launch prompt"})
		} else {
			launch, line = r.entries[0].note(), r.entries[0].Line
		}
		a := &Subagent{
			Transcript: Transcript{File: rel(c.file), Agent: c.id, AgentType: rename.AgentNow(metaString(meta, "agentType")), Entries: r.entries},
			ToolUseID:  metaString(meta, "toolUseId"), LaunchPrompt: launch, LaunchLine: line,
		}
		a.change, a.phase, a.root = changeOf(a.LaunchPrompt), phaseOf(a.LaunchPrompt, a.AgentType), rootOf(a)
		subagents = append(subagents, a)
		launchOf[c.id] = a.ToolUseID
	}
	if len(errors) > 0 {
		return nil, errors
	}
	// T262: a continued child's answer delivers the call that launched it; a child not saved, none.
	for _, x := range m.continued {
		if call := launchOf[x.child]; call != "" {
			x.entry.delivers, x.entry.delivery = call, x.answer
		}
	}
	return newSession(&Session{Runtime: "pi", Version: m.version, SessionID: m.id, Main: mt, Subagents: subagents, SessionKind: m.kinds, Settings: m.agents, Authorizations: m.authorizations, Compactions: m.facts.compactions, Resumes: m.resumes}), nil
}

// ResolvePiTarget is a pi session id looked up under each of roots
// (`<root>/<cwd dir>/<timestamp>_<id>.jsonl`): the id, the file's name
// without `.jsonl`, or a unique id prefix of at least six characters; ""
// when nothing matches.
func ResolvePiTarget(arg string, roots []string) string {
	if strings.Contains(arg, "/") || arg == "" {
		return ""
	}
	var files []string
	for _, root := range roots {
		dirs, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		var names []string
		for _, d := range dirs {
			names = append(names, d.Name())
		}
		slices.SortFunc(names, js.Compare)
		for _, d := range names {
			entries, err := os.ReadDir(filepath.Join(root, d))
			if err != nil {
				continue
			}
			for _, f := range entries {
				if strings.HasSuffix(f.Name(), ".jsonl") && f.Type().IsRegular() {
					files = append(files, filepath.Join(root, d, f.Name()))
				}
			}
		}
	}
	idOf := func(f string) string {
		if m := piChildFile.FindStringSubmatch(filepath.Base(f)); m != nil {
			return m[1]
		}
		return ""
	}
	for _, f := range files {
		if idOf(f) == arg || filepath.Base(f) == arg+".jsonl" {
			return f
		}
	}
	if js.Len(arg) < 6 || !shortID.MatchString(arg) {
		return ""
	}
	var hits []string
	for _, f := range files {
		if strings.HasPrefix(idOf(f), arg) {
			hits = append(hits, f)
		}
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return ""
}
