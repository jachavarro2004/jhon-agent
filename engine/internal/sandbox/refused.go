package sandbox

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
)

// The note on a write the sandbox refused (T128g, L1302, L1303 G3): an agent
// that met a read-only path once moved the write elsewhere (UV_TOOL_DIR in
// /tmp), a relocation the worker contract calls a stop. A wrapped command
// that fails and whose output says "Read-only file system" ends with one
// note naming the path, so the agent stops and tells the owner.

// readOnly is the kernel's message for EROFS, as libc and Rust print it.
const readOnly = "Read-only file system"

var (
	// quotedPath is an absolute path in the quotes the common tools use:
	// '…', "…", `…` and ‘…’.
	quotedPath = regexp.MustCompile("['\"`‘](/[^'\"`’\n]*)['\"`’]")
	// barePath is an absolute path as a word: after the line's start, a
	// blank or a colon.
	barePath = regexp.MustCompile(`(?:^|[\s:=])(/[^\s:'"` + "`" + `’,;()]+)`)
)

// RefusedWrite is the path the first line of output that says "Read-only
// file system" names, and whether a line says it; "" when the line names
// none. The path is the last one before the message (`<shell>: line 1:
// <path>: Read-only file system` names the shell first), else the first
// after it (Python's `Read-only file system: '<path>'`); a quoted one
// first, as a quoted path may hold blanks.
func RefusedWrite(output string) (path string, said bool) {
	for _, line := range strings.Split(output, "\n") {
		before, after, found := strings.Cut(line, readOnly)
		if !found {
			continue
		}
		last := func(re *regexp.Regexp, s string) string {
			if m := re.FindAllStringSubmatch(s, -1); m != nil {
				return m[len(m)-1][1]
			}
			return ""
		}
		first := func(re *regexp.Regexp, s string) string {
			if m := re.FindStringSubmatch(s); m != nil {
				return m[1]
			}
			return ""
		}
		for _, p := range []string{last(quotedPath, before), first(quotedPath, after), last(barePath, before), first(barePath, after)} {
			if p != "" {
				return strings.TrimRight(p, "."), true
			}
		}
		return "", true
	}
	return "", false
}

// RefusalNote is the note for a write the sandbox refused to path, or to the
// path the output names when the line named none.
func RefusalNote(path string) string {
	if path == "" {
		path = "the path the output names"
	}
	return "the sandbox refused a write to " + path + "; that is a stop: tell the owner the path (jhon settings sandbox.allow_write), never write it elsewhere"
}

// watchLine is the longest line Watch keeps: a longer one is cut there.
const watchLine = 8192

// Watch is a writer that looks for the first refused write (RefusedWrite)
// in what is written to it, line by line, across writes that may cut a line;
// it writes nowhere. Its methods may be called from more than one goroutine.
type Watch struct {
	mu      sync.Mutex
	partial []byte
	path    string
	said    bool
}

func (w *Watch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.said {
		return len(p), nil
	}
	rest := p
	for len(rest) > 0 && !w.said {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			w.partial = append(w.partial, rest[:min(len(rest), max(0, watchLine-len(w.partial)))]...)
			break
		}
		w.partial = append(w.partial, rest[:min(i, max(0, watchLine-len(w.partial)))]...)
		w.line()
		rest = rest[i+1:]
	}
	return len(p), nil
}

// line looks at the line kept so far, and forgets it.
func (w *Watch) line() {
	if path, said := RefusedWrite(string(w.partial)); said {
		w.path, w.said = path, true
	}
	w.partial = w.partial[:0]
}

// Refused is the first refused write's path, and whether there was one; a
// last line with no newline counts.
func (w *Watch) Refused() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.said && len(w.partial) > 0 {
		w.line()
	}
	return w.path, w.said
}
