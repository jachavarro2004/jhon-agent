package render

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// leanLimit is T146h's ceiling for the lean text of either runtime: the
// design's 4 KB target with its tests' 0.5 KB margin.
const leanLimit = 4608

// leanCore are the sentences the lean text may never lose (T146h L1): the
// small path's RED and GREEN, the stop and the same effect, credentials,
// remote work, the owner's decision, and what to read when.
var leanCore = []string{
	"Guards refuse or ask. A refusal is a stop. Never reach the same effect another way: other paths, shells, interpreters or delegates.",
	"Never touch credential files",
	"Remote work needs the owner's explicit authorization. Ambient credentials are not permission.",
	"Report success only when it was observed. Relay the owner's choices completely and never pick one for them.",
	"A read-only request writes nothing.",
	"Write the test first. Run `jhon executions run` and see it fail for the expected reason (RED).",
	"Change the code and run `jhon executions run` again (GREEN). That is two runs exactly.",
	"Run `git commit`, then `jhon deliver check`, with no re-run.",
	"A small change keeps no tasks.md, archive, todo list, verify or memory.",
	"Inside an authorized change, decide and state the decision. Ask only about scope, destructive actions or outward actions.",
	"Never weaken a failing test.",
	"Conventional commits, with no AI attribution.",
}

// leanPointersAt are the paths the lean text of runtime names, at the
// install's real path.
func leanPointersAt(e *engine, runtime string) []string {
	root := filepath.Join(e.current(), ProseDir, "contracts") + "/"
	return []string{
		"`" + e.fullFile(runtime) + "`",
		"`" + root + "asked-operations.md`",
		"`" + root + "delegation.md`",
		"`" + root + "delivery.md`",
		"`" + root + "witness.md`",
		"`" + root + "archify.md`",
	}
}

// T146h L1: the lean text is the only fixed text of a small task, at most
// 4.5 KB on both runtimes, measured at a representative home.
func TestLeanTextFitsBothRuntimes(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	e.home = "/home/owner"
	for _, rt := range []string{"claude-code", "pi"} {
		lean, full := e.leanText(rt), e.fullText(rt)
		t.Logf("%s: lean %d bytes, full %d bytes", rt, len(lean), len(full))
		if len(lean) == 0 || len(lean) > leanLimit {
			t.Errorf("%s's lean text is %d bytes, want 1..%d", rt, len(lean), leanLimit)
		}
		if len(full) <= len(lean) {
			t.Errorf("%s's full text (%d bytes) is not larger than its lean text (%d)", rt, len(full), len(lean))
		}
	}
	// What the main session's fixed prompt is on a small task: Claude Code's
	// definition body (the lean text and its guarantees), pi's lean file.
	def := promptOf(t, claude{e}, filepath.Join(e.claudeAgents(), piMain+".md"))
	t.Logf("claude-code: definition body %d bytes; pi: jhon-lean.md %d bytes", len(def)-strings.Index(def, "\n---\n")-5, len(promptOf(t, pi{e}, pi{e}.piLeanFile())))
}

// T146h L1: the key sentences and every read-more pointer, at the
// install's path, in both runtimes' lean text; no runtime term is left.
func TestLeanTextKeepsItsCore(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	for _, rt := range []string{"claude-code", "pi"} {
		lean := flatText(e.leanText(rt))
		for _, w := range append(append([]string{}, leanCore...), leanPointersAt(e, rt)...) {
			if !strings.Contains(lean, w) {
				t.Errorf("%s's lean text lacks %q", rt, w)
			}
		}
		if m := runtimePlaceholder.FindString(lean); m != "" {
			t.Errorf("%s's lean text keeps %s", rt, m)
		}
		if strings.Contains(lean, runtimeRef) {
			t.Errorf("%s's lean text names %s, not the install's real path", rt, runtimeRef)
		}
		// L4: the bare command only, and no unit's run between.
		for _, gone := range []string{"executions run <change>", "--affected", "unit's run", "executions run --help"} {
			if strings.Contains(lean, gone) {
				t.Errorf("%s's lean text names %q", rt, gone)
			}
		}
	}
}

// T217 S3: before it runs anything, the agent of either runtime reads that
// git over SSH, gh and the owner's deploy tools run outside the sandbox only
// alone on their line, git and gh also in a repository inside a
// sandbox.allow_write folder named by its real path (T251, L1722), and that
// a failure inside ends with a note whose fix it follows, not by turning
// the sandbox off (L1729: running it alone is the fix only when the note
// says so).
func TestLeanTextSaysWhatRunsOutsideTheSandbox(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	for _, rt := range []string{"claude-code", "pi"} {
		lean := flatText(e.leanText(rt))
		for _, w := range []string{
			"With Jhon's sandbox on, git over SSH, `gh` and `sandbox.deploy_tools` run outside, with the owner's credentials, alone on their line, by name:",
			"no pipes, `;`, `&&` (but a leading `cd <absolute dir> &&`), `$(…)`, `$VAR`, `~` or `VAR=value` in front.",
			"git and `gh` also run outside in a git repository in `sandbox.allow_write`, by its real path with no `..`.",
			"One kept inside says why: follow that fix, never turn the sandbox off.",
		} {
			if !strings.Contains(lean, w) {
				t.Errorf("%s's lean text lacks %q", rt, w)
			}
		}
	}
}

// T146h L2, the large path: the full context of each runtime is today's
// fixed context whole, the orchestrator's body and the always-on layer with
// the runtime's guarantees (pi's with its edits, as jhon-orchestrator.md
// carries it), so a change that is not small loses no rule.
func TestFullContextIsTodaysFixedContext(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	guarantees := func(rt string) string { return "\n\n## Runtime guarantees\n\n" + e.runtimeGuarantees(rt) }
	today := map[string]string{
		"claude-code": e.bodyFor("claude-code", e.mainAgent(piMain)) + "\n\n" + e.alwaysOnSection("claude-code", true) + guarantees("claude-code"),
		"pi":          pi{e}.promptText() + guarantees("pi"),
	}
	for rt, want := range today {
		if got := e.fullText(rt); got != want {
			t.Errorf("%s's full text (%d bytes) is not today's fixed context (%d bytes)", rt, len(got), len(want))
		}
		// Every always-on file and the orchestrator, sentence for sentence.
		full := flatText(e.fullText(rt))
		for _, part := range e.fixedParts(rt) {
			if rt == "pi" && part[0] == "contracts/shell-tools.md" {
				continue // pi's edit drops Claude Code's tool lines (pilean.go)
			}
			if !strings.Contains(full, part[1]) {
				t.Errorf("%s's full text lost %s", rt, part[0])
			}
		}
	}
	if got := promptOf(t, pi{e}, pi{e}.piPromptFile()); got != e.fullText("pi")+"\n" {
		t.Error("pi's jhon-orchestrator.md is not the full text")
	}
}

// T146h L1/L2: Claude Code's main agent is defined by the lean text; its
// full context is the file the lean text points to. pi renders the lean
// text beside its full prompt, and jhon.json names both and the full file.
func TestRuntimesRenderLeanBesideFull(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	def := promptOf(t, claude{e}, filepath.Join(e.claudeAgents(), piMain+".md"))
	body := def[strings.Index(def, "\n---\n")+5:]
	if !strings.HasPrefix(body, e.leanText("claude-code")) {
		t.Errorf("Claude Code's %s.md does not open with the lean text:\n%s", piMain, body[:min(300, len(body))])
	}
	for _, gone := range []string{"## The limits", "## Proportion", "# Jhon — Persona", "## Routing"} {
		if strings.Contains(body, gone) {
			t.Errorf("Claude Code's %s.md still carries %q", piMain, gone)
		}
	}
	p := pi{e}
	if got := promptOf(t, p, p.piLeanFile()); got != e.leanText("pi")+"\n" {
		t.Errorf("pi's lean file is not the lean text:\n%s", got)
	}
	v, err := js.ParseJSON(promptOf(t, p, p.piConfig()))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"orchestrator": p.piPromptFile(), "lean": p.piLeanFile(), "full": e.fullFile("pi")} {
		if got := js.Prop(v.(*js.Object), key); got != want {
			t.Errorf("jhon.json %s = %v, want %s", key, got, want)
		}
	}
	if want := filepath.Join(e.current(), ProseDir, "full-pi.md"); e.fullFile("pi") != want {
		t.Errorf("pi's full file is %s, want %s", e.fullFile("pi"), want)
	}
	// Sub-agents keep the full always-on layer.
	worker := promptOf(t, claude{e}, filepath.Join(e.claudeAgents(), "jhon-builder.md"))
	if !strings.Contains(worker, "## Runtime boundaries") {
		t.Error("Claude Code's worker lost the always-on layer")
	}
}

// T146h L2: an install writes each runtime's full file into the install's
// prose/ when it is there, and says so; without it, it skips the file.
func TestInstallWritesTheFullFile(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	var out bytes.Buffer
	e.out = &out
	s := &run{engine: e}
	s.writeFull("pi")
	if exists(e.fullFile("pi")) || !strings.Contains(out.String(), "skip") {
		t.Fatalf("a missing install wrote %s:\n%s", e.fullFile("pi"), out.String())
	}
	if err := os.MkdirAll(filepath.Join(e.current(), ProseDir), 0o755); err != nil {
		t.Fatal(err)
	}
	s.writeFull("pi")
	s.writeFull("claude-code")
	for _, rt := range []string{"pi", "claude-code"} {
		if got := readText(e.fullFile(rt)); got != e.fullText(rt)+"\n" {
			t.Errorf("%s's full file is not its full text (%d bytes)", rt, len(got))
		}
	}
}

// T146h, rule 31d extended: a lean pointer the lean text lost is reported
// against prose/lean.md.
func TestOnDemandProblemsReportALostLeanPointer(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	e.prose = proseCopy(t, e)
	path := filepath.Join(e.prose, "lean.md")
	text := strings.ReplaceAll(mustReadText(path), "contracts/witness.md", "contracts/nowhere.md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := e.onDemandProblems(); !hasOnDemandProblem(e, got, "lean.md", "witness.md") {
		t.Errorf("want the lost lean pointer reported against lean.md among:\n%v", got)
	}
}
