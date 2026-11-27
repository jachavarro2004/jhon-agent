package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// frictionCounting is the rule this build counts friction by: 1 counted
// the Claude Agent SDK's copies of a pi session whose first entrypoint says
// cli, and a resumed session's copies, once more each; 2 counts each tool
// call once, names the stops that were "other" and counts the sandbox's.
const frictionCounting = 2

// stopWindow is how far into a tool result a guard's name and its colon
// must lie for the result to be the guard's stop (L1772): a result that
// only quotes a guard (a grep, a changelog, a transcript read back) has it
// further in.
const stopWindow = 90

// hookErrorWindow is that reach in a result that begins with the older
// Claude Code hook error's prefix, `PreToolUse:<Tool> hook error: [<the
// hook command>]: `, whose bracketed command puts the guard's name far in.
const hookErrorWindow = 600

// hookErrorLead is that prefix's start.
var hookErrorLead = regexp.MustCompile(`^PreToolUse:\w+ hook error:`)

var (
	// guardStop is a guard's name followed by a colon; no longer word holds it.
	guardStop = regexp.MustCompile(`(?:^|[^\w-])((?:safety|route|inspect|vcs|framework|secrets|scope)-guard):`)
	// hookTimeout is the shell's refusal of a guard that did not answer in time.
	hookTimeout = regexp.MustCompile(`did not answer in \d+(?:\.\d+)?s; the call is refused`)
)

// stopClasses are the rule classes, by the refusal's wording, first match
// wins; a stop none matches is "other". "hook timeout" is hookTimeout's.
var stopClasses = []struct {
	name string
	re   *regexp.Regexp
}{
	{"credentials match", regexp.MustCompile(`credential`)},
	{"inspect shell syntax", regexp.MustCompile(`shell syntax`)},
	{"inspect allowlist", regexp.MustCompile(`allowlist`)},
	{"VCS directory", regexp.MustCompile(`vcs directory`)},
	{"unresolved target", regexp.MustCompile(`cannot resolve|unresolved`)},
	{"unbounded wait", regexp.MustCompile(`unbounded|nothing bounding|bound the wait`)},
	{"review due", regexp.MustCompile(`review is due|judgment day|\b4r\b|(verify|checker) is due`)},
	{"no record", regexp.MustCompile(`no change with its record`)},
	{"slice or delivery discipline", regexp.MustCompile(`closes slice|red before green|planned ahead|ldd is not set up|slices? left|docs_with_code|merge gate|tested tree`)},
	{"scope surfaces", regexp.MustCompile(`surfaces`)},
	{"unreadable or unknown", regexp.MustCompile(`unreadable|unknown`)},
	// Named in T270 among the stops that were "other"; an unknown verdict
	// whose target cannot be resolved stays unknown, above.
	{"unresolved target", regexp.MustCompile(`cannot be resolved`)},
	{"light verify", regexp.MustCompile(`light verify`)},
	{"workspace members' PRs", regexp.MustCompile(`members of the workspace`)},
	{"before the last slice", regexp.MustCompile(`after its last slice`)},
	{"merge method", regexp.MustCompile(`merges with (?:rebase|squash|merge)`)},
	{"owner's grant", regexp.MustCompile(`needs the owner's authorization`)},
	{"outside the project", regexp.MustCompile(`outside the project and the scratch roots`)},
	{"hard line", regexp.MustCompile(`never granted to an agent`)},
	{"framework path", regexp.MustCompile(`protected path`)},
}

// sandboxGuard is the guard of a command the sandbox stopped.
const sandboxGuard = "sandbox"

// The sandbox's stops, by their own words (engine/internal/sandbox and the
// shell's lib/sandbox.ts): readOnly is the kernel's message for EROFS as a
// program prints it (libc, Rust, Python), the one the sandbox's refusal
// note keys on.
const (
	readOnly     = "Read-only file system"
	refusedWrite = "the sandbox refused a write to "
	sandboxSays  = "jhon sandbox: "
)

var (
	// sandboxShell is the shell's refusal of a command with sandbox.mode on
	// where the sandbox cannot run or has no project to pin; it opens the
	// result.
	sandboxShell = regexp.MustCompile("^jhon: (?:sandbox\\.mode is on and |`jhon sandbox status --json` gave no answer)")
	// sandboxNote is the sandbox's note on a write it refused: a line of its
	// own, the rest of it the reason.
	sandboxNote = regexp.MustCompile(`(?m)^[ \t]*` + refusedWrite + `(.*)$`)
	// sandboxNotRun is jhon sandbox's own line on a sandbox that did not start.
	sandboxNotRun = regexp.MustCompile(`(?m)^[ \t]*` + sandboxSays + `.*so nothing was run`)
	// sandboxDocker is its note on docker, whose socket the sandbox hides.
	sandboxDocker = regexp.MustCompile(`(?m)^[ \t]*` + sandboxSays + `docker cannot reach its daemon`)
	// readOnlyLine is a line where a program says Read-only file system after
	// the path or call it names (": ") or Python's errno ("] "), with no
	// backquote before it, as a report quoting it has.
	readOnlyLine = regexp.MustCompile("^[^`]*(?:: |\\] )" + readOnly)
)

// readOnlyStop is whether a line of text is a program's Read-only file
// system (readOnlyLine), not the sandbox status's own words on a sandbox
// that cannot start (bwrap's uid map), which stopped no command.
func readOnlyStop(text string) bool {
	for line := range strings.Lines(text) {
		if strings.Contains(line, readOnly) && readOnlyLine.MatchString(line) && !strings.Contains(line, "cannot start a sandbox") {
			return true
		}
	}
	return false
}

// sandboxStop classes a result the sandbox stopped, by the reason its note
// gives; ok is false for any other result.
func sandboxStop(text string) (class string, ok bool) {
	switch {
	case sandboxShell.MatchString(text):
		return "mode on, cannot run", true
	case strings.Contains(text, refusedWrite):
		if m := sandboxNote.FindStringSubmatch(text); m != nil {
			switch why := m[1]; {
			case strings.Contains(why, "sandbox.allow_write opens it"):
				return "allow_write opens it", true
			case strings.Contains(why, "no setting") || strings.Contains(why, "does not open it"):
				return "no setting opens it", true
			}
			return "refused write", true
		}
	}
	if strings.Contains(text, sandboxSays) {
		switch {
		case sandboxNotRun.MatchString(text):
			return "did not start", true
		case sandboxDocker.MatchString(text):
			return "docker hidden", true
		}
	}
	if strings.Contains(text, readOnly) && readOnlyStop(text) {
		return "read-only, no note", true
	}
	return "", false
}

// classify reads one tool result: a stop when a guard's name and a colon
// lie in its first stopWindow characters (hookErrorWindow after the older
// hook error's prefix), when it is the shell's hook timeout within
// stopWindow, or when the sandbox stopped it (sandboxStop); then its guard
// ("hook-timeout" for a timeout, "sandbox") and its class, read from the
// text after the guard's name.
func classify(text string) (guard, class string, ok bool) {
	text = strings.TrimSpace(text)
	if loc := hookTimeout.FindStringIndex(text); loc != nil && utf8.RuneCountInString(text[:loc[0]]) < stopWindow {
		return "hook-timeout", "hook timeout", true
	}
	window := stopWindow
	if hookErrorLead.MatchString(text) {
		window = hookErrorWindow
	}
	head := text
	if r := []rune(text); len(r) > window {
		head = string(r[:window])
	}
	m := guardStop.FindStringSubmatchIndex(head)
	if m == nil {
		if class, ok := sandboxStop(text); ok {
			return sandboxGuard, class, true
		}
		return "", "", false
	}
	guard, rest := head[m[2]:m[3]], strings.ToLower(text[m[1]:])
	for _, c := range stopClasses {
		if c.re.MatchString(rest) {
			return guard, c.name, true
		}
	}
	return guard, "other", true
}

// scratchPrefixes are the starts of a transcript folder's name, its leading
// and trailing dashes trimmed, that make it a scratch folder: the runtimes
// name the folder after the session's working directory, / written -.
var scratchPrefixes = []string{"tmp", "var-tmp", "private-tmp", "private-var-folders", "dev-shm"}

// bridgeMirror is the left-out key of the Claude Agent SDK's transcripts:
// pi on pi-claude-bridge reaches Claude through the SDK, which writes its
// own copy of the pi session under ~/.claude/projects, each entry with an
// entrypoint saying sdk-ts; counted there, every result would count twice.
const bridgeMirror = "bridge_mirror"

// Each tool call counts once, by its id (counting rule 2): bridgeCopy is
// the left-out key of a Claude Code result that copies a pi result the
// count already holds (an SDK transcript whose first entrypoint says cli),
// repeated that of any other copy (a resumed Claude Code session copies
// the results of the one it resumes). A file whose results are all copies
// is a session of that key.
const (
	bridgeCopy = "bridge_copy"
	repeated   = "repeated"
)

// leftOut is why a project's transcript folder is left out of the count:
// "jhon" for one whose name holds jhon (Jhon's own development, L1772),
// "scratch" for a scratch folder, "" to count it.
func leftOut(folder string) string {
	name := strings.ToLower(strings.Trim(folder, "-"))
	if strings.Contains(name, "jhon") {
		return "jhon"
	}
	for _, p := range scratchPrefixes {
		if name == p || strings.HasPrefix(name, p+"-") {
			return "scratch"
		}
	}
	return ""
}

// countFriction counts, in every transcript under each runtime's root
// (runtime -> folder) changed since since (all when zero), the tool results
// and the stops among them, by guard, class, ISO week and runtime, leaving
// Jhon's own development and scratch folders out and counting them apart.
// Each tool call counts once: pi's transcripts are read first, and a
// result whose id was counted is a copy, counted apart.
func countFriction(roots map[string]string, since time.Time) (*Friction, error) {
	f := &Friction{Counting: frictionCounting, Since: "all", PerRuntime: map[string]*Count{}, PerGuard: map[string]int{},
		PerClass: map[string]ClassRate{}, PerWeek: map[string]*Count{},
		LeftOut: map[string]*Count{"jhon": {}, "scratch": {}, bridgeMirror: {}, bridgeCopy: {}, repeated: {}}}
	if !since.IsZero() {
		f.Since = since.Format(time.DateOnly)
	}
	classes := map[string]int{}
	// counted is the runtime that counted each tool call id.
	counted := map[string]string{}
	runtimes := slices.SortedFunc(maps.Keys(roots), func(a, b string) int {
		return cmp.Or(cmp.Compare(boolInt(a != "pi"), boolInt(b != "pi")), cmp.Compare(a, b))
	})
	for _, runtime := range runtimes {
		root := roots[runtime]
		folders, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("the %s transcripts cannot be listed: %v", runtime, err)
		}
		rt := &Count{}
		f.PerRuntime[runtime] = rt
		for _, folder := range folders {
			if !folder.IsDir() {
				continue
			}
			out := leftOut(folder.Name())
			err := filepath.WalkDir(filepath.Join(root, folder.Name()), func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
					return nil
				}
				info, err := d.Info()
				if err != nil || info.ModTime().Before(since) {
					return nil
				}
				file, err := os.Open(p)
				if err != nil {
					return nil
				}
				defer file.Close()
				// A file's results are kept until its entrypoint is known.
				type read struct {
					at           time.Time
					id           string
					guard, class string
					stop         bool
				}
				var reads []read
				entrypoint := readTranscript(file, since, info.ModTime(), func(at time.Time, id, text string) {
					guard, class, stop := classify(text)
					reads = append(reads, read{at, id, guard, class, stop})
				})
				out := out
				if out == "" && runtime == "claude-code" && entrypoint == "sdk-ts" {
					out = bridgeMirror
				}
				count := rt
				if out != "" {
					count = f.LeftOut[out]
				}
				// copyOf is the left-out key of a copy of a counted call, ""
				// for a call not counted yet.
				copyOf := func(id string) string {
					switch first, seen := counted[id]; {
					case out != "" || id == "" || !seen:
						return ""
					case first == "pi" && runtime != "pi":
						return bridgeCopy
					}
					return repeated
				}
				session := count
				if len(reads) > 0 {
					key := repeated
					for _, r := range reads {
						c := copyOf(r.id)
						if c == "" {
							key = ""
							break
						}
						if c == bridgeCopy {
							key = bridgeCopy
						}
					}
					if key != "" {
						session = f.LeftOut[key]
					}
				}
				session.Sessions++
				for _, r := range reads {
					if c := copyOf(r.id); c != "" {
						f.LeftOut[c].ToolResults++
						if r.stop {
							f.LeftOut[c].Stops++
						}
						continue
					}
					count.ToolResults++
					if r.stop {
						count.Stops++
					}
					if out != "" {
						continue
					}
					if r.id != "" {
						counted[r.id] = runtime
					}
					y, w := r.at.ISOWeek()
					week := fmt.Sprintf("%04d-W%02d", y, w)
					if f.PerWeek[week] == nil {
						f.PerWeek[week] = &Count{}
					}
					f.PerWeek[week].ToolResults++
					if r.stop {
						f.PerWeek[week].Stops++
						f.PerGuard[r.guard]++
						classes[r.class]++
					}
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		f.Sessions += rt.Sessions
		f.ToolResults += rt.ToolResults
		f.Stops += rt.Stops
	}
	f.Per100 = per100(f.Stops, f.ToolResults)
	for _, c := range f.PerRuntime {
		c.Per100 = per100(c.Stops, c.ToolResults)
	}
	for _, c := range f.LeftOut {
		c.Per100 = per100(c.Stops, c.ToolResults)
	}
	for _, c := range f.PerWeek {
		c.Per100 = per100(c.Stops, c.ToolResults)
	}
	for class, n := range classes {
		f.PerClass[class] = ClassRate{Stops: n, Per100: per100(n, f.ToolResults)}
	}
	return f, nil
}

// transcriptLine is what a line of either runtime's transcript says of a
// tool result: Claude Code's user line holds tool_result parts, pi's
// message line has the toolResult role.
type transcriptLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   *struct {
		Role       string          `json:"role"`
		ToolCallID string          `json:"toolCallId"`
		Content    json.RawMessage `json:"content"`
	} `json:"message"`
}

// readTranscript calls each for every tool result in r at or after since,
// with its time (the line's, else modified), its tool call's id (pi's
// toolCallId, Claude Code's tool_use_id, the same for one call) and its
// text. It returns the entrypoint of the first entry that carries one, ""
// when none does.
func readTranscript(r io.Reader, since, modified time.Time, each func(at time.Time, id, text string)) (entrypoint string) {
	br := bufio.NewReaderSize(r, 1<<20)
	decided := false
	for {
		raw, err := br.ReadBytes('\n')
		if !decided && bytes.Contains(raw, []byte(`"entrypoint"`)) {
			var e struct {
				Entrypoint *string `json:"entrypoint"`
			}
			if json.Unmarshal(raw, &e) == nil && e.Entrypoint != nil {
				entrypoint, decided = *e.Entrypoint, true
			}
		}
		if len(raw) > 0 && (bytes.Contains(raw, []byte(`"tool_result"`)) || bytes.Contains(raw, []byte(`"toolResult"`))) {
			var l transcriptLine
			if json.Unmarshal(raw, &l) == nil && l.Message != nil {
				at := modified
				if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
					at = t.Local()
				}
				if !at.Before(since) {
					switch {
					case l.Type == "message" && l.Message.Role == "toolResult":
						each(at, l.Message.ToolCallID, contentText(l.Message.Content))
					case l.Type == "user":
						var parts []struct {
							Type      string          `json:"type"`
							ToolUseID string          `json:"tool_use_id"`
							Content   json.RawMessage `json:"content"`
						}
						if json.Unmarshal(l.Message.Content, &parts) == nil {
							for _, p := range parts {
								if p.Type == "tool_result" {
									each(at, p.ToolUseID, contentText(p.Content))
								}
							}
						}
					}
				}
			}
		}
		if err != nil {
			return entrypoint // the end, or an unreadable rest that counts as the end
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// contentText is a content's text: a string, or its text parts joined.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}
