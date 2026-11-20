package audit

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	lddlayout "github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/hook"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The efficiency measurement: how each writer kept to contracts/efficiency.md.

// WriterEfficiency is one writer's measurement. ReturnShape is `ok` when
// its final text carries the `status:` and `validation:` lines, and `n/a`
// in pi, which asks a writer for no shape (no SubagentStop hook). A file is
// written through a write tool or a Bash command (guard.WrittenFiles).
// OutOfScope lists the repository files it wrote that its scope does not
// name, relative to its root; the scope is the launch prompt's `## Allowed
// edit surfaces` when it has them, else its launch prompt and, for an
// apply, its change's tasks.md and design.md, and the change's own
// artifacts and test files are always in it; nil when the scope names no
// file. A write outside the root is not counted; the same directory in
// another worktree of its repository (worktreesOf) is the root too, and a
// write there is relative to it. DependencyEdits are the
// dependency manifests among them. MaxSameFailure is the most failed runs
// of one Bash command; over two is an unbounded correction loop.
type WriterEfficiency struct {
	Agent, AgentType, Change, Phase string
	ReturnShape                     string
	FilesWritten                    int
	OutOfScope                      []string
	DependencyEdits                 []string
	MaxSameFailure                  int
	Bounded                         bool
	// ActiveMs is the writer's own active time: beside FilesWritten, what a
	// delegated file cost, to compare with the main thread's own (ChangeOutcome).
	ActiveMs float64
}

// WriterAgents are the agents contracts/efficiency.md binds, with the retired writers of older sessions.
var WriterAgents = []string{"jhon-builder", "general"}

// correctionBound is the most failed runs of one command the contract allows: an attempt, and one more after a real fix.
const correctionBound = 2

var (
	dependencyFile = lazyre.New(`^(?:package(?:-lock)?\.json|pnpm-lock\.yaml|yarn\.lock|bun\.lockb?|requirements[\w.-]*\.txt|pyproject\.toml|poetry\.lock|uv\.lock|Pipfile(?:\.lock)?|go\.(?:mod|sum)|Cargo\.(?:toml|lock)|Gemfile(?:\.lock)?|composer\.(?:json|lock)|pom\.xml|build\.gradle(?:\.kts)?)$`)
	// pathToken is a path-like token: a file with an extension, or a directory ending in `/`.
	pathToken = lazyre.New(`(?:[\w.@-]+/)*[\w@-][\w.@-]*\.[A-Za-z0-9]+|(?:[\w.@-]+/)+`)
	dotSlash  = lazyre.New(`^\./`)
	fenceOpen = lazyre.New("^(`{3,}|~{3,})")
)

// isLineStart is whether offset i of text is where a line starts, as ^ reads it under the m flag.
func isLineStart(text string, i int) bool {
	if i == 0 {
		return true
	}
	if text[i-1] == '\n' || text[i-1] == '\r' {
		return true
	}
	return strings.HasSuffix(text[:i], " ") || strings.HasSuffix(text[:i], " ")
}

// skipSpace is the offset after the JavaScript whitespace at text[i:].
func skipSpace(text string, i int) int {
	for _, r := range text[i:] {
		if !js.IsSpace(r) {
			break
		}
		i += len(string(r))
	}
	return i
}

// isTerminator is whether text[i:] starts with a line terminator.
func isTerminator(text string, i int) bool {
	return i < len(text) && (text[i] == '\n' || text[i] == '\r' || strings.HasPrefix(text[i:], " ") || strings.HasPrefix(text[i:], " "))
}

// lineEndAfter is where `\s*$` (m flag) ends from offset i: the greedy run
// of whitespace backed off to its last line end; -1 when none holds.
func lineEndAfter(text string, i int) int {
	end := skipSpace(text, i)
	if end == len(text) {
		return end
	}
	for k := end; k > i; {
		k--
		for k > i && !utf8Start(text[k]) {
			k--
		}
		if isTerminator(text, k) {
			return k
		}
	}
	if isTerminator(text, i) {
		return i
	}
	return -1
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// withoutContextBlock is text without its first jhon-context fenced block,
// as text.replace(/^\s*(`{3,}|~{3,})\s*jhon-context\s*$[\s\S]*?^\s*\1\s*$/m, "")
// reads it (the backreference matched by hand).
func withoutContextBlock(text string) string {
	for s := 0; s <= len(text); s++ {
		if s < len(text) && !utf8Start(text[s]) || !isLineStart(text, s) {
			continue
		}
		f := skipSpace(text, s)
		fence := fenceOpen.FindString(text[f:])
		if fence == "" {
			continue
		}
		k := skipSpace(text, f+len(fence))
		if !strings.HasPrefix(text[k:], contextFence) {
			continue
		}
		k += len(contextFence)
		if lineEndAfter(text, k) < 0 {
			continue
		}
		for c := k; c <= len(text); c++ {
			if c < len(text) && !utf8Start(text[c]) || !isLineStart(text, c) || c == k {
				continue
			}
			g := skipSpace(text, c)
			if !strings.HasPrefix(text[g:], fence) {
				continue
			}
			if end := lineEndAfter(text, g+len(fence)); end >= 0 {
				return text[:s] + text[end:]
			}
		}
	}
	return text
}

// namedPaths is the repository paths a scope text names, relative to the
// first of roots each lies in.
func namedPaths(text string, roots ...string) []string {
	plain := withoutContextBlock(text)
	for _, root := range roots {
		plain = strings.ReplaceAll(plain, root+"/", "")
	}
	var out []string
	for _, m := range pathToken.FindAllString(plain, -1) {
		if t := dotSlash.ReplaceAllString(m, ""); !strings.HasPrefix(t, "/") {
			out = append(out, t)
		}
	}
	return out
}

func inScope(rel string, named []string) bool {
	return slices.ContainsFunc(named, func(t string) bool {
		if rel == t {
			return true
		}
		if strings.HasSuffix(t, "/") {
			return strings.HasPrefix(rel, t)
		}
		return strings.HasPrefix(rel, t+"/")
	})
}

// surfaceRoot is the root a writer's surfaces are relative to, as the scope
// guard reads them: the repository abs lies in, else the sub-agent's root. A
// worker started in a project below the repository's top (a service of a
// monorepo) lists its surfaces from the top.
func surfaceRoot(abs, root string) string {
	if r := repoOf(abs); r != "" {
		return r
	}
	return root
}

// writesOf is guard.WrittenFiles and guard.WritesOutsideIn of one command,
// from one guard.WriteGroups: the groups are read once, not once for each.
func writesOf(command, cwd string, in func(abs string) bool) (written, outside []string) {
	for _, g := range guard.WriteGroups(command, cwd) {
		written = append(written, g.Files...)
		var out []string
		for _, f := range g.Files {
			if !in(f) {
				out = append(out, f)
			}
		}
		if !g.Code || !slices.ContainsFunc(g.Targets, in) {
			outside = append(outside, out...)
		}
	}
	return uniq(written), uniq(outside)
}

func writerEfficiency(a *Subagent) *WriterEfficiency {
	change, phase := a.change, a.phase
	root := a.root
	if root == "" {
		root = "/"
	}
	bases := worktreesOf(root)
	failed := failedIDs(a.Entries)
	var writtenPaths []string
	// absOf is each written path's absolute path, in whichever worktree.
	absOf := map[string]string{}
	failures := map[string]int{}
	// The handoff's `## Allowed edit surfaces`, as the scope guard reads them.
	surfaces, listed := guard.SurfacesOf(a.LaunchPrompt)
	in := func(abs string) bool { return guard.InSurfacesOf(abs, surfaces, surfaceRoot(abs, root)) }
	// strayed are the files the scope guard's own rule reads as written
	// outside the surfaces (a script with a target in scope is the change's).
	strayed := map[string]bool{}
	add := func(abs string) {
		for _, b := range bases {
			if strings.HasPrefix(abs, b+"/") {
				addOnce(&writtenPaths, abs[len(b)+1:])
				absOf[abs[len(b)+1:]] = abs
				return
			}
		}
	}
	for _, e := range a.Entries {
		cwd := or(e.Cwd, root)
		for _, u := range e.ToolUses {
			if key, ok := writeToolKey[u.Name]; ok && !failed[u.ID] {
				if p, ok := strProp(u.Input, key); ok {
					abs := absolute(p, cwd)
					add(abs)
					if !in(abs) {
						strayed[abs] = true
					}
				}
			}
			c, ok := strProp(u.Input, "command")
			if u.Name != "Bash" || !ok {
				continue
			}
			if !failed[u.ID] {
				written, outside := writesOf(c, cwd, in)
				for _, abs := range written {
					add(abs)
				}
				for _, abs := range outside {
					strayed[abs] = true
				}
			} else {
				failures[js.Trim(c)]++
			}
		}
	}
	scope := a.LaunchPrompt
	if phase == "apply" && change != "" {
		for _, base := range bases {
			for _, d := range changeDirs(base, change) {
				for _, f := range []string{lddlayout.For(base).Tasks(), "design.md"} {
					if b, err := os.ReadFile(filepath.Join(d, f)); err == nil {
						scope += "\n" + js.DecodeUTF8(b)
					}
				}
			}
		}
	}
	// Without listed surfaces, every path the scope names.
	named := surfaces
	if !listed {
		named = namedPaths(scope, bases...)
	}
	var own []string
	if change != "" {
		for _, l := range lddlayout.Layouts {
			own = append(own, l.ChangeDir(change)+"/")
		}
	}
	files := slices.Clone(writtenPaths)
	slices.SortFunc(files, js.Compare)
	outside := []string{}
	for _, f := range files {
		var in bool
		if listed {
			in = !strayed[absOf[f]]
		} else {
			in = guard.TestFile.MatchString(f) || inScope(f, slices.Concat(named, own))
		}
		if !in {
			outside = append(outside, f)
		}
	}
	deps := []string{}
	for _, f := range outside {
		if dependencyFile.MatchString(baseName(f)) {
			deps = append(deps, f)
		}
	}
	text, _ := lastText(a.Entries)
	most := 0
	for _, n := range failures {
		most = max(most, n)
	}
	w := &WriterEfficiency{Agent: a.Agent, AgentType: a.AgentType, Change: change, Phase: phase, ReturnShape: "missing",
		FilesWritten: len(files), DependencyEdits: deps, MaxSameFailure: most, Bounded: most <= correctionBound,
		ActiveMs: length(union(activity(&a.Transcript)))}
	if hook.ReturnShaped(text) {
		w.ReturnShape = "ok"
	}
	if len(named) > 0 {
		w.OutOfScope = outside
	}
	return w
}

// computeEfficiency is the efficiency measurement of a loaded session: one
// row per writer, in launch order, each measured concurrently.
func computeEfficiency(s *Session) []*WriterEfficiency {
	var writers []*Subagent
	for _, a := range s.Subagents {
		if slices.Contains(WriterAgents, a.AgentType) {
			writers = append(writers, a)
		}
	}
	out := make([]*WriterEfficiency, len(writers))
	parallel(len(writers), func(i int) {
		out[i] = writerEfficiency(writers[i])
		if s.Runtime == "pi" {
			out[i].ReturnShape = "n/a"
		}
	})
	return out
}
