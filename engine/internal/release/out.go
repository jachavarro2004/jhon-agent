package release

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Out is the CLI's output: ok, FAIL, warn, and note lines, section heads,
// and die, coloured only when stdout is a terminal (tested once, and the
// colours reach stderr too).
type Out struct {
	W, E             io.Writer
	B, R, G, Y, D, Z string
	// Failed counts the FAIL lines, doctor's problems.
	Failed int
}

// NewOut writes to stdout and stderr, in colour when color is set.
func NewOut(color bool) *Out {
	o := &Out{W: os.Stdout, E: os.Stderr}
	if color {
		o.B, o.R, o.G, o.Y, o.D, o.Z = "\033[1m", "\033[31m", "\033[32m", "\033[33m", "\033[2m", "\033[0m"
	}
	return o
}

// OK is `  ok    <msg>`.
func (o *Out) OK(msg string) { fmt.Fprintf(o.W, "  %sok%s    %s\n", o.G, o.Z, msg) }

// Bad is `  FAIL  <msg>`, counted.
func (o *Out) Bad(msg string) {
	fmt.Fprintf(o.W, "  %sFAIL%s  %s\n", o.R, o.Z, msg)
	o.Failed++
}

// Warn is `  warn  <msg>`.
func (o *Out) Warn(msg string) { fmt.Fprintf(o.W, "  %swarn%s  %s\n", o.Y, o.Z, msg) }

// Note is a dim, indented line.
func (o *Out) Note(msg string) { fmt.Fprintf(o.W, "  %s%s%s\n", o.D, msg, o.Z) }

// Head is a section head after a blank line.
func (o *Out) Head(msg string) { fmt.Fprintf(o.W, "\n%s%s%s\n", o.B, msg, o.Z) }

// Die is `jhon: <msg>` on stderr; it returns the exit code, 1.
func (o *Out) Die(msg string) int {
	fmt.Fprintf(o.E, "%sjhon:%s %s\n", o.R, o.Z, msg)
	return 1
}

// Lines is s's lines, s being a command's output with its trailing newlines
// already stripped; an empty s is one empty line.
func Lines(s string) []string {
	return strings.Split(s, "\n")
}

// Last is `tail -1 <<<"$out"`: the last line of a substituted output.
func Last(s string) string {
	l := Lines(s)
	return l[len(l)-1]
}

// Substituted is what `$(…)` keeps of a command's output: every trailing
// newline removed.
func Substituted(b []byte) string {
	return strings.TrimRight(string(b), "\n")
}
