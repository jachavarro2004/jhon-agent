package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

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
}

// classify reads one tool result: a stop when a guard's name and a colon
// lie in its first stopWindow characters (hookErrorWindow after the older
// hook error's prefix), or when it is the shell's hook timeout within
// stopWindow; then its guard ("hook-timeout" for a timeout) and its class,
// read from the text after the guard's name.
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
func countFriction(roots map[string]string, since time.Time) (*Friction, error) {
	f := &Friction{Since: "all", PerRuntime: map[string]*Count{}, PerGuard: map[string]int{},
		PerClass: map[string]ClassRate{}, PerWeek: map[string]*Count{}, LeftOut: map[string]*Count{"jhon": {}, "scratch": {}, bridgeMirror: {}}}
	if !since.IsZero() {
		f.Since = since.Format(time.DateOnly)
	}
	classes := map[string]int{}
	for runtime, root := range roots {
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
					guard, class string
					stop         bool
				}
				var reads []read
				entrypoint := readTranscript(file, since, info.ModTime(), func(at time.Time, text string) {
					guard, class, stop := classify(text)
					reads = append(reads, read{at, guard, class, stop})
				})
				out := out
				if out == "" && runtime == "claude-code" && entrypoint == "sdk-ts" {
					out = bridgeMirror
				}
				count := rt
				if out != "" {
					count = f.LeftOut[out]
				}
				count.Sessions++
				for _, r := range reads {
					count.ToolResults++
					if r.stop {
						count.Stops++
					}
					if out != "" {
						continue
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
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// readTranscript calls each for every tool result in r at or after since,
// with its time (the line's, else modified) and its text. It returns the
// entrypoint of the first entry that carries one, "" when none does.
func readTranscript(r io.Reader, since, modified time.Time, each func(time.Time, string)) (entrypoint string) {
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
						each(at, contentText(l.Message.Content))
					case l.Type == "user":
						var parts []struct {
							Type    string          `json:"type"`
							Content json.RawMessage `json:"content"`
						}
						if json.Unmarshal(l.Message.Content, &parts) == nil {
							for _, p := range parts {
								if p.Type == "tool_result" {
									each(at, contentText(p.Content))
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
