package sandbox

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

// The note on a write the sandbox refused (T128g, L1302, L1303 G3): an agent
// that met a read-only path once moved the write elsewhere (UV_TOOL_DIR in
// /tmp), a relocation the worker contract calls a stop. A wrapped command
// that fails and whose output says "Read-only file system" ends with one
// note naming the path, so the agent stops and tells the owner.

// readOnly is the kernel's message for EROFS, as libc and Rust print it,
// and busy its message for EBUSY: a mount point the sandbox keeps where it
// is (a pin, a read-only file a runtime runs, a hidden path), which no
// command moves or removes (T250 round 3).
const (
	readOnly = "Read-only file system"
	busy     = "Device or resource busy"
)

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
// first, as a quoted path may hold blanks. For a busy one it is the first
// before the message: the one the command could not move or remove (mv
// names its source, then where it was going), T250.
func RefusedWrite(output string) (path string, said bool) {
	for _, line := range strings.Split(output, "\n") {
		before, after, found := strings.Cut(line, readOnly)
		if !found {
			before, after, found = strings.Cut(line, busy)
			if m := quotedPath.FindStringSubmatch(before); found && m != nil {
				return strings.TrimRight(m[1], "."), true
			}
		}
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

// RefusalNote is the note for a write refused to path, or to the path the
// output names when the line named none: what a run ends with when it has
// no sandbox to explain the path by (--profile unsandboxed) or no path;
// a work run's note is Refusal's (T239).
func RefusalNote(path string) string {
	if path == "" {
		path = "the path the output names"
	}
	return escapeControls("the sandbox refused a write to " + path + "; that is a stop: tell the owner the path (jhon settings sandbox.allow_write), never write it elsewhere")
}

// escapeControls is note with every character that could move, rewrite or
// hide what a terminal or a transcript shows escaped as text (L26 A3): the
// C0 and C1 controls and DEL as \xHH (\r, ESC, NEL), the line and paragraph
// separators, the bidirectional and zero-width controls and the byte order
// mark as \uHHHH, and a byte that is not UTF-8 as \xHH. The path a refused
// write names comes from a command's output, which a command chooses.
func escapeControls(note string) string {
	var b strings.Builder
	for i := 0; i < len(note); {
		r, size := utf8.DecodeRuneInString(note[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			fmt.Fprintf(&b, `\x%02x`, note[i])
		case r < 0x20 || r >= 0x7f && r <= 0x9f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r >= 0x200b && r <= 0x200f || r >= 0x2028 && r <= 0x202e || r >= 0x2060 && r <= 0x2069 || r == 0xfeff:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteString(note[i : i+size])
		}
		i += size
	}
	return b.String()
}

// dockerUnreached are the docker client's own words for a daemon it cannot
// reach: docker 29's, and those of older clients and docker-compose v1.
// With the sandbox hiding the docker socket (sandbox.docker off, L37) every
// docker call inside it ends so, and an agent read that as a machine with
// no Docker (T252, L1721).
var dockerUnreached = []string{"failed to connect to the docker API at ", "Cannot connect to the Docker daemon at "}

// DockerUnreached is whether a line of output says the docker client could
// not reach its daemon.
func DockerUnreached(line string) bool {
	return slices.ContainsFunc(dockerUnreached, func(s string) bool { return strings.Contains(line, s) })
}

// DockerNote is the note a work run in in ends with when its output says
// docker could not reach its daemon (DockerUnreached, T252): "" with
// sandbox.docker on, where the socket is there and the message means what
// it says.
func DockerNote(in Input) string {
	if in.Docker {
		return ""
	}
	return dockerNote
}

const dockerNote = "jhon sandbox: docker cannot reach its daemon from inside the sandbox, which hides the docker socket (sandbox.docker is off), so this says nothing of whether Docker runs on this machine; fix: run the docker command on a line of its own in the project, with no pipes or other commands (as in docker compose up -d), which runs outside the sandbox, or ask the owner to run jhon settings set sandbox.docker true, which shows the socket inside the sandbox, where a container can write anywhere the user can"

// watchLine is the longest line Watch keeps: a longer one is cut there.
const watchLine = 8192

// Watch is a writer that looks for the first refused write (RefusedWrite)
// in what is written to it, line by line, across writes that may cut a line,
// and for a docker client that could not reach its daemon (DockerUnreached);
// it writes nowhere. Its methods may be called from more than one goroutine.
type Watch struct {
	mu      sync.Mutex
	partial []byte
	path    string
	said    bool
	docker  bool
}

func (w *Watch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.said && w.docker {
		return len(p), nil
	}
	rest := p
	for len(rest) > 0 && !(w.said && w.docker) {
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
	if !w.said {
		if path, said := RefusedWrite(string(w.partial)); said {
			w.path, w.said = path, true
		}
	}
	w.docker = w.docker || DockerUnreached(string(w.partial))
	w.partial = w.partial[:0]
}

// Refused is the first refused write's path, and whether there was one; a
// last line with no newline counts.
func (w *Watch) Refused() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.last()
	return w.path, w.said
}

// Docker is whether a line said docker could not reach its daemon; a last
// line with no newline counts.
func (w *Watch) Docker() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.last()
	return w.docker
}

// last looks at a last line with no newline, once.
func (w *Watch) last() {
	if len(w.partial) > 0 {
		w.line()
	}
}
