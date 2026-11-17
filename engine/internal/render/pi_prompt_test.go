package render

import (
	"path/filepath"
	"strings"
	"testing"
)

// promptOf is the content of the file at path among r's files.
func promptOf(t *testing.T, r Renderer, path string) string {
	t.Helper()
	for _, f := range r.Files() {
		if f.Path == path {
			return f.Content
		}
	}
	t.Fatalf("%s renders no %s", r.Runtime(), path)
	return ""
}

// section is text's `## name` section, heading included, up to the next
// heading of level one or two.
func section(t *testing.T, text, name string) string {
	t.Helper()
	start := strings.Index(text, "\n## "+name+"\n")
	if start < 0 {
		t.Fatalf("no ## %s", name)
	}
	rest := text[start+1:]
	end := len(rest)
	for _, h := range []string{"\n## ", "\n# "} {
		if i := strings.Index(rest[1:], h); i >= 0 && i+1 < end {
			end = i + 1
		}
	}
	return rest[:end]
}

// T42c: pi's appended orchestrator text keeps the rules word for word, puts
// rarely needed detail behind a reference that resolves, and states a
// duplicated rule once; Claude Code's definition is untouched.
func TestPiPromptCarriesWhatPiUses(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	p := pi{e}
	got := promptOf(t, p, p.piPromptFile())
	full := p.bodyFor("pi", p.mainAgent(piMain)) + "\n\n" + p.alwaysOnSection("pi", true)

	// Word for word: Strict TDD, Routing's LDD lines, The limits, the
	// persona and language, Engram's sections.
	keep := []string{section(t, full, "Strict TDD"), section(t, full, "The limits"), section(t, full, "Engram"),
		section(t, full, "Engram session"), section(t, full, "Artifact Language"), section(t, full, "Runtime boundaries")}
	routing := section(t, full, "Routing")
	keep = append(keep, routing[strings.Index(routing, "A change that alters behaviour carries its LDD record"):])
	persona := full[strings.Index(full, "# Jhon — Persona"):strings.Index(full, "## Always-on contracts")]
	keep = append(keep, persona)
	for _, k := range keep {
		if !strings.Contains(got, k) {
			t.Errorf("pi's prompt lost, word for word:\n%s", k)
		}
	}

	// Behind references: Claude Code's tool lines, the remote list, the 4R
	// and Judgment Day table, the --affected and CI-watch detail.
	for _, gone := range []string{"(Read, Grep, Glob, Edit)", "- Ambient SSH agents", "| Tool | Pays when | Where |",
		"`gh run watch <run-id> --exit-status`", "`--dry-run` estimate"} {
		if strings.Contains(got, gone) {
			t.Errorf("pi's prompt still carries %q", gone)
		}
	}
	prose := filepath.Join(p.current(), ProseDir)
	for _, ref := range []string{"orchestrator.md", "contracts/remote-auth.md", "contracts/review-4r.md", "contracts/judgment.md", "contracts/ldd.md"} {
		if !strings.Contains(got, "`"+filepath.Join(prose, ref)+"`") {
			t.Errorf("pi's prompt does not name %s", ref)
		}
		if !exists(filepath.Join(e.prose, ref)) {
			t.Errorf("%s does not resolve", ref)
		}
	}
	if !strings.Contains(mustReadText(filepath.Join(e.prose, "orchestrator.md")), "**Check proportionately.**") {
		t.Error("orchestrator.md has no \"Check proportionately\" to read")
	}

	// Once: LDD's RED and GREEN. The LDD lead and Routing's todo lines stay
	// (L417): without the lead's `openspec/` condition, the executions rules
	// read as binding in a project without LDD, and a plain task spent turns
	// on `jhon executions`, branches and commits.
	for phrase, want := range map[string]int{"never copied into it": 1, "**Where the session says LDD is active, these bind.**": 1, "When LDD's tools cannot record a run": 1,
		"keep a todo list of your own next steps for\nthis request, started before the first edit;": 1, "while it fails (RED)": 1} {
		if n := strings.Count(got, phrase); n != want {
			t.Errorf("%q appears %d times, want %d", phrase, n, want)
		}
	}
	if len(got) >= len(full) {
		t.Errorf("pi's prompt is %d chars, the full text %d", len(got), len(full))
	}

	// Claude Code keeps everything.
	for _, r := range e.renderers() {
		if r.Runtime() != "claude-code" {
			continue
		}
		cc := promptOf(t, r, filepath.Join(e.claudeAgents(), "jhon-orchestrator.md"))
		for _, kept := range []string{"(Read, Grep, Glob, Edit)", "- Ambient SSH agents", "| Tool | Pays when | Where |", "`gh run watch <run-id> --exit-status`", "these bind.**"} {
			if !strings.Contains(cc, kept) {
				t.Errorf("Claude Code's orchestrator lost %q", kept)
			}
		}
	}
}

// An edit whose text is gone is reported, never applied elsewhere.
func TestPiLeanReportsAnEditWhoseTextIsGone(t *testing.T) {
	out, missing := piLean("nothing to edit", "/p")
	if out != "nothing to edit" || len(missing) != len(piEdits("/p")) {
		t.Errorf("out %q, missing %v", out, missing)
	}
}

// T76 (L579, L583, L584): on both runtimes the orchestrator looks for an
// existing decision in the code and in memory before deciding, hands the
// standing decisions to its executors, and replaces a sub-agent whose
// premise the owner corrected instead of continuing it.
func TestOrchestratorResearchesDecisionsFirst(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	wants := []string{
		"**Existing decisions first.** Before deciding, proposing a design, or asking the owner, look for a decision already made:",
		"CodeGraph first when the session has it, then reads",
		"`mem_search` for each entity the request names, and the project's file memory when present",
		"Cite the decision ids found.",
		"A recorded owner decision overrides the source document's wording: state the deviation, never follow the document against it.",
		"An explore or worker task carries the standing decisions found, each id with one line, and asks it to search for more on what it covers.",
		"When the owner corrects a premise, stop the running sub-agent and launch a fresh one with the correction; never continue one whose work rests on the corrected premise.",
	}
	p := pi{e}
	prompts := map[string]string{"pi": p.promptFlat(t)}
	for _, r := range e.renderers() {
		if r.Runtime() == "claude-code" {
			prompts["claude-code"] = flatText(promptOf(t, r, filepath.Join(e.claudeAgents(), "jhon-orchestrator.md")))
		}
	}
	for _, rt := range []string{"pi", "claude-code"} {
		got, ok := prompts[rt]
		if !ok {
			t.Fatalf("no %s orchestrator prompt", rt)
		}
		t.Logf("%s orchestrator prompt: %d bytes", rt, len(got))
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s's orchestrator does not say %q", rt, w)
			}
		}
	}
}

// T78-T79 (L586, L587, L609): no turn ends on an authorized next step; a
// worker gets every integration point of its slice. T80's change lifecycle
// is a per-project setting (worktree_per_change), not orchestrator prose.
func TestOrchestratorKeepsWorkingAndPlansLifecycle(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	wants := []string{
		"**Continue.** Never end a turn announcing a next step that is already authorized: do it in the same turn.",
		"End a turn only for an owner decision that is really needed, a refusal, or a blocker you cannot resolve; a progress message is no reason to stop.",
		"Before launching a worker on a slice, map with CodeGraph (then reads) every integration point it touches",
		"permissions and their categories or registries, permission seeds",
		"A worker stopped on scope is a planning error: fix the plan, never ask the owner.",
		// T81, re-applied in T87a (L596, L629): from the main checkout, sub-agents and inspect reach the change's worktree.
		"From the main checkout, pass the change's worktree to sub-agents and inspect (their `cwd` where the tool takes one, else its absolute path in the task and `git -C <worktree>`); never launch agents by running Jhon's internals by hand (`shell/lib`, node imports).",
		// T95 (L629, L634): fixes after a worker, long suites, worker sizing, delivery order.
		"A fix needed after a worker returns goes to a worker (continue it, or launch a fresh one), never inline.",
		"A suite longer than a few minutes runs once, recorded (`jhon executions run`), in the background; never pipe it through `tail`, `head`, or `grep` (the pipe hides its exit code and failures): read its summary and record.",
		"A slice's commit holds exactly the tree of its last recorded passing run: RED, GREEN, recorded run, commit.",
		// T131 (L905, L908): no turn cap; a slice is sized to the context budget.
		"**Size each worker slice to fit its context budget; the worker checkpoints its progress and partial state at its context notice and returns what is done and what remains: hand the remainder to a fresh worker.**",
		"Order: commit, then a recorded run (`jhon executions run`) on the clean tree at HEAD, then `jhon deliver check`.",
	}
	p := pi{e}
	prompts := map[string]string{"pi": p.promptFlat(t)}
	for _, r := range e.renderers() {
		if r.Runtime() == "claude-code" {
			prompts["claude-code"] = flatText(promptOf(t, r, filepath.Join(e.claudeAgents(), "jhon-orchestrator.md")))
		}
	}
	for _, rt := range []string{"pi", "claude-code"} {
		got, ok := prompts[rt]
		if !ok {
			t.Fatalf("no %s orchestrator prompt", rt)
		}
		t.Logf("%s orchestrator prompt: %d bytes", rt, len(got))
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s's orchestrator does not say %q", rt, w)
			}
		}
	}
}

// T90 (L648, L649): every sub-agent runs in the background on both
// runtimes; the orchestrator waits for each answer's notification and never
// blocks its turn on one, keeping the parallel launches in one turn.
func TestOrchestratorRunsSubAgentsInTheBackground(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	common := "each answer arrives as a notification: never block the turn waiting on one, and never poll or sleep for it."
	wants := map[string][]string{
		"pi":          {"**Background sub-agents.** Sub-agents always run in the background: `subagent` returns a task id at once and shows the agent on the agents rail; " + common},
		"claude-code": {"**Background sub-agents.** Sub-agents always run in the background: launch every `Agent` call with `run_in_background: true` where the tool offers it (an interactive session already runs them there); " + common},
	}
	p := pi{e}
	prompts := map[string]string{"pi": p.promptFlat(t)}
	for _, r := range e.renderers() {
		if r.Runtime() == "claude-code" {
			prompts["claude-code"] = flatText(promptOf(t, r, filepath.Join(e.claudeAgents(), "jhon-orchestrator.md")))
		}
	}
	for rt, ws := range wants {
		got, ok := prompts[rt]
		if !ok {
			t.Fatalf("no %s orchestrator prompt", rt)
		}
		t.Logf("%s orchestrator prompt: %d bytes", rt, len(got))
		for _, w := range ws {
			if !strings.Contains(got, w) {
				t.Errorf("%s's orchestrator does not say %q", rt, w)
			}
		}
		if strings.Contains(got, "run_in_background: false") || strings.Contains(got, "`background: true`") {
			t.Errorf("%s's orchestrator still offers a foreground or opt-in background launch", rt)
		}
	}
}

func (p pi) promptFlat(t *testing.T) string { return flatText(promptOf(t, p, p.piPromptFile())) }

// flatText joins wrapped lines so a sentence matches across line breaks.
func flatText(s string) string { return strings.Join(strings.Fields(s), " ") }

// T131 (L905, L908): jhon-shell reads no turn cap, so pi's agent files carry
// none even where the manifest names one; Claude Code's `maxTurns` is its own.
func TestAManifestTurnCapRendersForClaudeCodeOnly(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	for i := range e.m.Agents {
		if e.m.Agents[i].Name == "worker" {
			if e.m.Agents[i].MaxTurns != "" {
				t.Errorf("the release's worker names maxTurns %q", e.m.Agents[i].MaxTurns)
			}
			e.m.Agents[i].MaxTurns = "40"
		}
	}
	if got := promptOf(t, pi{e}, filepath.Join(e.piAgents(), "worker.md")); strings.Contains(got, "maxTurns") {
		t.Errorf("pi's worker carries a turn cap:\n%s", got[:min(400, len(got))])
	}
	if got := promptOf(t, claude{e}, filepath.Join(e.claudeAgents(), "worker.md")); !strings.Contains(got, "\nmaxTurns: 40\n") {
		t.Errorf("Claude Code's worker lost its maxTurns:\n%s", got[:min(400, len(got))])
	}
}
