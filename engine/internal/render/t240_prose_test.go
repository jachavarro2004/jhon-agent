package render

import (
	"path/filepath"
	"strings"
	"testing"
)

// T240 (odd/tasks/studio-integration-v1.md S5, the first real run's audit):
// the agent started building while the owner only asked how it would be
// done ("No lo hagas aun."), assumed the narration's language, handed one
// builder a deliverable too large to finish, and ran a third-party CLI from
// a repository whose .env it then read.

// planWaits is rule 1 in the lean text (a small task) and in routing.md,
// which the full text and every agent carry.
const (
	planWaitsLean = "A read-only request writes nothing. Asked for a plan or how something would be done, explain and wait for a go: no todo, no command but reads."
	planWaitsFull = "Investigation, explanation, review, audit, comparison, proposal, plan and how-to requests are read-only until the owner says go: explain, recommend and wait; no file, todo or command but reads."
	// askLanguages is rule 2, in the lean text and in language.md.
	askLanguages = "Always ask the languages of narration, subtitles, UI copy or end-user docs that the owner or the project did not set."
	// cliFolder is rule 4, in the lean text, the orchestrator's limits and
	// the builder's body: the agents that run commands.
	cliFolder = "Run third-party CLIs in the task's own folder (scratch or output), never in a repository with a `.env` or secrets."
	// builderUnits is rule 3: the delegation contract splits a large
	// deliverable, and the builder writes each part as it finishes it.
	splitUnits   = "A deliverable of many like parts (screens, pages, mockups, shots) is split into units of a few parts, each unit with its own files and its own builder."
	builderParts = "Write each part to its file as soon as it is finished, never all at the end, so a stop leaves finished work."
)

func TestMainSessionExplainsAndWaitsAsksLanguagesAndKeepsCLIsAway(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	for _, rt := range []string{"claude-code", "pi"} {
		lean, full := flatText(e.leanText(rt)), flatText(e.fullText(rt))
		for _, w := range []string{planWaitsLean, askLanguages, cliFolder} {
			if n := strings.Count(lean, w); n != 1 {
				t.Errorf("%s's lean text holds %q %d times, want once", rt, w, n)
			}
		}
		for _, w := range []string{planWaitsFull, askLanguages, cliFolder} {
			if n := strings.Count(full, w); n != 1 {
				t.Errorf("%s's full text holds %q %d times, want once", rt, w, n)
			}
		}
		if strings.Contains(full, "read-only unless the user explicitly asks for a change") {
			t.Errorf("%s's full text keeps the routing line rule 1 replaces", rt)
		}
	}
}

func TestBuilderTasksAreSizedToFinish(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	if d := flatText(mustReadText(filepath.Join(e.prose, "contracts", "delegation.md"))); !strings.Contains(d, splitUnits) {
		t.Errorf("contracts/delegation.md lacks %q", splitUnits)
	}
	for name, path := range map[string]string{
		"claude-code": filepath.Join(e.claudeAgents(), "jhon-builder.md"),
		"pi":          filepath.Join(e.piAgents(), "jhon-builder.md"),
	} {
		var r Renderer = claude{e}
		if name == "pi" {
			r = pi{e}
		}
		body := flatText(promptOf(t, r, path))
		for _, w := range []string{builderParts, cliFolder} {
			if !strings.Contains(body, w) {
				t.Errorf("%s's jhon-builder lacks %q", name, w)
			}
		}
	}
	for name, path := range map[string]string{
		"claude-code": filepath.Join(e.claudeAgents(), "jhon-checker.md"),
		"pi":          filepath.Join(e.piAgents(), "jhon-checker.md"),
	} {
		var r Renderer = claude{e}
		if name == "pi" {
			r = pi{e}
		}
		if body := flatText(promptOf(t, r, path)); !strings.Contains(body, cliFolder) {
			t.Errorf("%s's jhon-checker lacks %q", name, cliFolder)
		}
	}
}

// T240 verifier probes (studio-integration-v1.md S5): rules 1 and 2 reach
// every sub-agent in both runtimes through the always-on layer.
func TestT240RulesReachEverySubAgent(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	for _, name := range e.m.AgentNames() {
		if name == piMain {
			continue
		}
		for rt, path := range map[string]string{
			"claude-code": filepath.Join(e.claudeAgents(), name+".md"),
			"pi":          filepath.Join(e.piAgents(), name+".md"),
		} {
			var r Renderer = claude{e}
			if rt == "pi" {
				r = pi{e}
			}
			body := flatText(promptOf(t, r, path))
			for _, w := range []string{planWaitsFull, askLanguages} {
				if n := strings.Count(body, w); n != 1 {
					t.Errorf("%s's %s holds %q %d times, want once", rt, name, w, n)
				}
			}
		}
	}
}

// The trimmed lean text keeps the pointer to the full context, the remote
// operations it names, and the template the credentials rule allows.
func TestT240TrimmedLeanTextKeepsItsMeaning(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	for _, rt := range []string{"claude-code", "pi"} {
		lean := flatText(e.leanText(rt))
		for _, w := range []string{
			"## Read more when",
			"read the full context, `" + e.fullFile(rt) + "`",
			"push, `gh` write, publish, deploy",
			"A template (`.env.example`) is fine.",
		} {
			if !strings.Contains(lean, w) {
				t.Errorf("%s's lean text lacks %q", rt, w)
			}
		}
	}
}
