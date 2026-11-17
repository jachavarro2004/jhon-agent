// Package compose applies a delta spec to a main spec under
// contracts/spec-delta.md. Archive used to do this by having
// a model Read the main spec and Edit it, which is how unrelated
// requirements were dropped and deltas left half-applied while the phase
// reported success.
//
// Composition replaces byte ranges of the main spec's own string: it never
// parses the spec and prints it back, so every byte outside a touched block
// is the byte that was there. Refusals are computed before anything is
// written, and the write is a temporary file plus a rename in the same
// directory, so a reader never sees a half-composed spec.
package compose

import (
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/requirements"
)

// Refusal is a delta composition cannot honour (ComposeRefusal). Where is
// the section, `main spec`, `delta`, or `file`.
type Refusal struct{ Where, Name, Reason string }

func (r *Refusal) Error() string { return r.Where + ` "` + r.Name + `": ` + r.Reason }

func refusal(where, name, reason string) *Refusal { return &Refusal{where, name, reason} }

// Files names the two inputs in a refusal.
type Files struct{ Main, Delta string }

// Roles is the names a refusal gives inputs that are not files.
var Roles = Files{Main: "(main spec)", Delta: "(delta)"}

// splitTrailing is everything before the last non-blank line's EOL, and
// that EOL plus any blank lines after it.
func splitTrailing(s string) (body, gap string) {
	lines := requirements.MarkdownLines(s)
	for i := len(lines) - 1; i >= 0; i-- {
		if js.Trim(lines[i].Text) != "" {
			cut := lines[i].Start + len(lines[i].Text)
			return s[:cut], s[cut:]
		}
	}
	return "", s
}

var hashes = lazyre.New(`^( {0,3})#+`)

// relevel is one delta line as it lands in a main spec at depth. This is
// the declared depth adaptation: an unfenced Requirement: heading at the
// delta's level takes depth, a Scenario: heading at the delta's level takes
// depth + 1, and only the run of `#` changes. Every other line is returned
// as it was.
func relevel(src string, l requirements.Line, depth int) string {
	raw := src[l.Start:l.End]
	h := requirements.HeadingOf(l)
	if h == nil {
		return raw
	}
	target := 0
	switch kind := requirements.HeadingKind(h.Text); {
	case kind == requirements.KindRequirement && h.Level == requirements.RequirementLevel:
		target = depth
	case kind == requirements.KindScenario && h.Level == requirements.ScenarioLevel:
		target = depth + 1
	}
	if target == 0 || target == h.Level {
		return raw
	}
	m := hashes.FindStringSubmatchIndex(raw)
	return raw[:m[3]] + strings.Repeat("#", target) + raw[m[1]:]
}

// deltaBody is a delta block as it lands in a main spec at depth:
// annotation lines removed, requirement and scenario headings re-levelled,
// trailing blank lines trimmed. Fencing is read over the whole delta so a
// fenced line inside the block stays content.
func deltaBody(src string, r *requirements.Requirement, depth int) string {
	annotations := map[int]bool{}
	for _, a := range r.Annotations {
		annotations[a.Start] = true
	}
	var out strings.Builder
	for _, l := range requirements.MarkdownLines(src) {
		if l.Start < r.Start || l.Start >= r.End || annotations[l.Start] {
			continue
		}
		out.WriteString(relevel(src, l, depth))
	}
	body, _ := splitTrailing(out.String())
	return body
}

func byName(p requirements.Parsed, name string) *requirements.Requirement {
	for _, r := range p.Requirements {
		if r.Name == name {
			return r
		}
	}
	return nil
}

// refuseDeltaDepth refuses a delta whose Requirement or Scenario headings
// sit at a depth other than the delta's fixed one. The parser would read it
// as holding no requirements, and composing on that reading disables every
// name check, so the depth is reported instead of guessed at.
func refuseDeltaDepth(file, src string) error {
	m := requirements.HeadingDepthMismatch(src)
	if m == nil {
		return nil
	}
	return refusal("delta", file, fmt.Sprintf("line %d is a level %d %s: heading; the contract expects level %d. "+
		"The delta uses a heading depth compose does not support", m.Line, m.Found, m.Kind, m.Expected))
}

// mainDepth is the main spec's requirement depth, or a refusal naming the
// line that fits no structural variant. Compose never repairs a main spec: a
// spec it cannot read unambiguously is reported, not rewritten.
func mainDepth(file, src string, c requirements.DeltaContract) (int, error) {
	depth, r := requirements.MainSpecDepth(src, c.Main)
	if r == nil {
		return depth, nil
	}
	at := fmt.Sprintf("line %d is a level %d %s: heading", r.Line, r.Found, r.Kind)
	var why string
	switch r.Reason {
	case requirements.RefusalOutOfRange:
		levels := make([]string, len(r.Expected))
		for i, e := range r.Expected {
			levels[i] = strconv.Itoa(e)
		}
		why = fmt.Sprintf("%s; a main spec's requirements sit at level %s", at, strings.Join(levels, " or "))
	case requirements.RefusalMixed:
		why = fmt.Sprintf("%s; the spec's first requirement is at level %d (line %d). Requirement headings in one main spec share one depth", at, r.Expected[0], r.FirstLine)
	default:
		why = fmt.Sprintf("%s; the spec's requirements are at level %d, and Scenario headings sit one level below them, at level %d", at, r.Expected[0]-1, r.Expected[0])
	}
	return 0, refusal("main spec", file, why+". Compose never repairs a main spec")
}

// rename is a RENAMED block's old and new names.
type rename struct{ from, to string }

// Compose composes delta into main and returns the new main spec. The error
// is a *Refusal, nothing having changed, when the contract forbids it; files
// names the two inputs in a refusal.
func Compose(main, delta string, c requirements.DeltaContract, files Files) (string, error) {
	eol := "\n"
	if strings.Contains(main, "\r\n") {
		eol = "\r\n"
	}

	// Heading depth before anything else: every later check reads the parse.
	// The delta is parsed at its fixed depth, the main spec at the one detected.
	depth, err := mainDepth(files.Main, main, c)
	if err != nil {
		return "", err
	}
	if err := refuseDeltaDepth(files.Delta, delta); err != nil {
		return "", err
	}
	parse := func(s string) requirements.Parsed {
		return requirements.ParseSpec(s, requirements.Options{Metadata: c.Metadata, Ops: c.Order, Depth: depth})
	}

	// The main spec first: a duplicate name makes every match ambiguous.
	seen := map[string]int{}
	for _, r := range parse(main).Requirements {
		if first, dup := seen[r.Name]; dup {
			return "", refusal("main spec", r.Name, fmt.Sprintf("requirement appears twice, at lines %d and %d", first, r.Line))
		}
		seen[r.Name] = r.Line
	}

	// The delta's structure, before any operation runs.
	d := requirements.ParseSpec(delta, requirements.Options{Metadata: c.Metadata, Ops: c.Order})
	var sections []*requirements.Section
	for _, s := range d.Sections {
		if s.Op != nil {
			sections = append(sections, s)
		}
	}
	for _, r := range d.Requirements {
		if !slices.ContainsFunc(sections, func(s *requirements.Section) bool { return slices.Contains(s.Requirements, r) }) {
			return "", refusal("delta", r.Name, fmt.Sprintf("line %d is outside every delta section", r.Line))
		}
	}
	if len(sections) == 0 {
		expected := make([]string, len(c.Order))
		for i, o := range c.Order {
			expected[i] = "## " + o + " Requirements"
		}
		return "", refusal("delta", "(file)", "has no delta section; expected "+strings.Join(expected, ", "))
	}

	blocks := func(op string) []*requirements.Requirement {
		var out []*requirements.Requirement
		for _, s := range sections {
			if *s.Op == op {
				out = append(out, s.Requirements...)
			}
		}
		return out
	}
	for _, op := range []string{"RENAMED", "MODIFIED", "REMOVED", "ADDED"} {
		if !slices.Contains(c.Order, op) {
			return "", errors.New("contracts/spec-delta.md: delta_order lacks " + op)
		}
	}

	k := &composer{delta: delta, depth: depth, eol: eol, c: c, renames: map[*requirements.Requirement]rename{}}
	for _, s := range sections {
		for _, r := range s.Requirements {
			if err := k.check(*s.Op, r); err != nil {
				return "", err
			}
		}
	}
	added := map[string]bool{}
	for _, r := range blocks("ADDED") {
		if added[r.Name] {
			return "", refusal("ADDED", r.Name, "is added more than once")
		}
		added[r.Name] = true
	}

	// Apply in contract order, each operation on the text the previous one left.
	text := main
	for _, op := range c.Order {
		for _, r := range blocks(op) {
			if text, err = k.apply(text, op, r, parse(text)); err != nil {
				return "", err
			}
		}
	}
	return text, nil
}

// composer is what every delta block is checked and applied with.
type composer struct {
	delta   string
	depth   int
	eol     string
	c       requirements.DeltaContract
	renames map[*requirements.Requirement]rename
}

// check refuses a delta block its operation cannot carry, and records a
// RENAMED block's names.
func (k *composer) check(op string, r *requirements.Requirement) error {
	for _, need := range k.c.Required[op] {
		if !slices.ContainsFunc(r.Annotations, func(a requirements.Annotation) bool { return a.Name == need }) {
			return refusal(op, r.Name, "lacks the required ("+need+": …) annotation")
		}
	}
	if op == "RENAMED" {
		parts := strings.Split(r.Name, "→")
		if len(parts) != 2 || js.Trim(parts[0]) == "" || js.Trim(parts[1]) == "" {
			return refusal(op, r.Name, "malformed RENAMED heading; expected `### Requirement: <old> → <new>`")
		}
		k.renames[r] = rename{js.Trim(parts[0]), js.Trim(parts[1])}
	}
	if slices.Contains(k.c.HeadingOnly, op) {
		// Only the heading and the annotations carry meaning here. Content
		// would be dropped silently, so it is refused instead.
		annotated := map[int]bool{}
		for _, a := range r.Annotations {
			annotated[a.Line] = true
		}
		for i, l := range requirements.MarkdownLines(k.delta[r.Start:r.End])[1:] {
			if t := js.Trim(l.Text); t != "" && !annotated[r.Line+1+i] {
				return refusal(op, r.Name, `carries content ("`+t+`"); a change to a requirement's content is a MODIFIED block`)
			}
		}
	}
	return nil
}

// apply is text with one delta block's operation applied; cur is text parsed.
func (k *composer) apply(text, op string, r *requirements.Requirement, cur requirements.Parsed) (string, error) {
	names := k.renames[r]
	switch op {
	case "RENAMED":
		target := byName(cur, names.from)
		if target == nil {
			return "", refusal(op, r.Name, `no such requirement "`+names.from+`" in the main spec`)
		}
		if byName(cur, names.to) != nil {
			return "", refusal(op, r.Name, `"`+names.to+`" already exists in the main spec`)
		}
		return text[:target.NameRange.Start] + names.to + text[target.NameRange.End:], nil
	case "MODIFIED":
		target := byName(cur, r.Name)
		if target == nil {
			return "", refusal(op, r.Name, "no such requirement in the main spec")
		}
		_, gap := splitTrailing(text[target.Start:target.End])
		return text[:target.Start] + deltaBody(k.delta, r, k.depth) + gap + text[target.End:], nil
	case "REMOVED":
		target := byName(cur, r.Name)
		if target == nil {
			return "", refusal(op, r.Name, "no such requirement in the main spec")
		}
		if target.End < len(text) {
			return text[:target.Start] + text[target.End:], nil
		}
		// The last block: keep the file's own ending rather than leave the
		// previous block's separator dangling at EOF.
		_, gap := splitTrailing(text[target.Start:])
		body, _ := splitTrailing(text[:target.Start])
		return body + gap, nil
	case "ADDED":
		if byName(cur, r.Name) != nil {
			return "", refusal(op, r.Name, "already exists in the main spec")
		}
		start, end := 0, len(text)
		if n := len(cur.Requirements); n > 0 {
			start, end = cur.Requirements[n-1].Start, cur.Requirements[n-1].End
		} else if i := slices.IndexFunc(cur.Sections, func(s *requirements.Section) bool { return s.Title == k.c.Main.RequirementsSection }); i >= 0 {
			start, end = cur.Sections[i].Start, cur.Sections[i].End
		}
		body, gap := splitTrailing(text[start:end])
		block := deltaBody(k.delta, r, k.depth)
		joined := block
		if body != "" {
			joined = body + k.eol + k.eol + block
		}
		return text[:start] + joined + gap + text[end:], nil
	}
	return text, nil
}

// readExact decodes UTF-8, refusing input that would not re-encode to the
// same bytes.
func readExact(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", js.FSError(err)
	}
	if !utf8.Valid(b) {
		return "", refusal("file", path, "is not valid UTF-8; composing it would rewrite bytes outside the delta")
	}
	return string(b), nil
}

// IO is the filesystem calls ComposeFile makes, replaceable so a crash can
// be simulated.
type IO struct {
	WriteFile func(name string, data []byte, perm fs.FileMode) error
	Rename    func(oldpath, newpath string) error
}

// OS is the real filesystem.
var OS = IO{WriteFile: os.WriteFile, Rename: os.Rename}

// ComposeFile composes the delta file into the main spec file, atomically,
// and returns the composed text. It fails, leaving the main spec untouched,
// on any refusal or I/O failure; a temporary file is removed on the way out.
func ComposeFile(mainPath, deltaPath string, io IO) (string, error) {
	if _, err := os.Stat(mainPath); err != nil {
		return "", refusal("main spec", mainPath, "does not exist; with no main spec, archive copies the delta as a full spec")
	}
	main, err := readExact(mainPath)
	if err != nil {
		return "", err
	}
	delta, err := readExact(deltaPath)
	if err != nil {
		return "", err
	}
	c, err := requirements.Delta()
	if err != nil {
		return "", err
	}
	next, err := Compose(main, delta, c, Files{mainPath, deltaPath})
	if err != nil {
		return "", err
	}
	tmp := filepath.Join(filepath.Dir(mainPath), "."+filepath.Base(mainPath)+".compose-"+strconv.Itoa(os.Getpid())+"-"+strconv.FormatInt(time.Now().UnixMilli(), 10))
	if err := write(io, tmp, mainPath, next); err != nil {
		os.Remove(tmp) // never created, or already renamed
		return "", js.FSError(err)
	}
	return next, nil
}

// write puts the bytes in tmp, gives it the main spec's mode, and renames it
// over the main spec.
func write(io IO, tmp, mainPath, next string) error {
	if err := io.WriteFile(tmp, []byte(next), 0o666); err != nil {
		return err
	}
	info, err := os.Stat(mainPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(tmp, info.Mode()&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky)); err != nil {
		return err
	}
	return io.Rename(tmp, mainPath)
}
