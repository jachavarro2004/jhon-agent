package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// T146h (L1278, L1280): a small task starts with the lean text alone
// (prose/lean.md: identity, the limits, the small change's two runs, and
// what to read when), about 1k tokens; the full context, today's
// orchestrator and always-on layer, is rendered per runtime as
// <install>/prose/full-<runtime>.md and loads on a trigger: `jhon status`'s
// not-small line, a session started on a change that is not small, pi's
// first `subagent` call, Claude Code's PostToolUse pointer. Sub-agents keep
// the full always-on layer.

// leanProse is the lean text's prose file, relative to prose/.
const leanProse = "lean.md"

// leanText is the lean text as runtime renders it: each runtime term in its
// words, every path at the install's real path, and no line about a cli
// tool that is off (studio.go).
func (e *engine) leanText(runtime string) string {
	return e.atInstall(inRuntime(runtime, withoutOffTools(js.Trim(mustReadText(filepath.Join(e.prose, leanProse))), e.toolsOff)))
}

// fullText is the full context of runtime: what its main session carried
// before T146h, the orchestrator's body and the always-on layer (pi's with
// its edits, pilean.go), then the runtime's guarantees.
func (e *engine) fullText(runtime string) string {
	guarantees := "\n\n## Runtime guarantees\n\n" + e.runtimeGuarantees(runtime)
	if runtime == "pi" {
		return pi{e}.promptText() + guarantees
	}
	return e.bodyFor(runtime, e.mainAgent(piMain)) + "\n\n" + e.alwaysOnSection(runtime, true) + guarantees
}

// fullFile is where the full context of runtime is installed: the install's
// prose/, beside the contracts the full text points to.
func (e *engine) fullFile(runtime string) string {
	return filepath.Join(e.current(), ProseDir, "full-"+runtime+".md")
}

// piLeanFile is pi's lean prompt, which the shell appends instead of
// jhon-orchestrator.md when the session starts lean.
func (e *engine) piLeanFile() string { return filepath.Join(e.piHome(), "jhon-lean.md") }

// writeFull writes (or, under --check, compares) runtime's full file where
// the install's prose/ is a directory; without one there is no install to
// write into, and the line says so.
func (s *run) writeFull(runtime string) {
	file := s.fullFile(runtime)
	if st, err := os.Stat(filepath.Dir(file)); err != nil || !st.IsDir() {
		s.println(fmt.Sprintf("  skip   %s: no install at %s", s.tilde(file), s.tilde(s.current())))
		return
	}
	s.writeFiles([]File{{file, s.fullText(runtime) + "\n"}})
}

// leanPointer is a sentence the lean text must hold, raw as prose/lean.md
// writes it: rule 31d reports one that is gone.
var leanPointers = []string{
	"`~/.local/share/jhon/current/prose/full-{{runtime:runtime}}.md`",
	"`~/.local/share/jhon/current/prose/contracts/asked-operations.md`",
	"`~/.local/share/jhon/current/prose/contracts/delegation.md`",
	"`~/.local/share/jhon/current/prose/contracts/delivery.md`",
	"`~/.local/share/jhon/current/prose/contracts/witness.md`",
	"`~/.local/share/jhon/current/prose/contracts/archify.md`",
}

// leanProblems are rule 31d's for the lean text: a pointer prose/lean.md
// lost, or one a runtime's rendered lean text does not carry at the
// install's path, and a rule moved on demand the lean text states again.
func (e *engine) leanProblems() []problem {
	var out []problem
	path := filepath.Join(e.prose, leanProse)
	add := func(msg string) { out = append(out, problem{path, 0, "on-demand", msg}) }
	if !exists(path) {
		add(leanProse + " is missing")
		return out
	}
	raw := flattened(mustReadText(path))
	for _, p := range leanPointers {
		if !strings.Contains(raw, p) {
			add(fmt.Sprintf("%s does not hold the pointer %s", leanProse, p))
			continue
		}
		for _, rt := range []string{"claude-code", "pi"} {
			if !strings.Contains(flattened(e.leanText(rt)), e.atInstall(inRuntime(rt, p))) {
				add(fmt.Sprintf("%s's lean text does not point to %s", rt, e.atInstall(inRuntime(rt, p))))
			}
		}
	}
	for _, c := range onDemandContracts {
		for _, r := range c.rules {
			if strings.Contains(raw, r) {
				add(fmt.Sprintf("%s states %q, moved to %s", leanProse, r, c.file))
			}
		}
	}
	return out
}
