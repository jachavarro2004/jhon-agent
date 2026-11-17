// The explore agent writes only a change's exploration.md or research.md
// (prose/agents.yaml; T123, L762 F3: an explore wrote tasks.md and a spec
// delta). vcs-guard, which every writer carries in both runtimes, refuses
// any other write of explore's: Claude Code names the agent with `--agent
// explore` (and agent_type), pi's child with JHON_PI_AGENT under `--agent *`.

package guards

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// exploreCall is tool writing rel (under repo) as the agent the runtime names.
func exploreCall(t *testing.T, runtime, agent, tool, repo, rel string) support.Result {
	t.Helper()
	key := "file_path"
	if tool == "NotebookEdit" {
		key = "notebook_path"
	}
	payload := map[string]any{"tool_name": tool, "cwd": repo, "tool_input": map[string]any{key: filepath.Join(repo, rel)}}
	if runtime == "pi" {
		payload["agent_id"] = "pi-child"
		env := support.Env("JHON_PI_CHILD=1", "JHON_PI_AGENT="+agent)
		return hook(t, env, "vcs-guard", append([]string{"--agent", "*"}, vcsDirs(t)...), payload)
	}
	payload["agent_type"] = agent
	return hook(t, support.Env("JHON_PI_CHILD=", "JHON_PI_AGENT="), "vcs-guard", append([]string{"--agent", agent}, vcsDirs(t)...), payload)
}

const exploreRefusal = `^vcs-guard: explore writes only its change's exploration.md or research.md \(<layout>/changes/<change>/exploration.md or research.md, openspec/ or ldd/\); `

func TestExploreWritesOnlyItsRecord(t *testing.T) {
	t.Parallel()
	repo := realTemp(t)
	support.GitRepo(t, repo)
	allowed := []string{
		"openspec/changes/example-change/exploration.md",
		"openspec/changes/example-change/research.md",
		"ldd/changes/pagination/exploration.md",
		"ldd/changes/pagination/research.md",
	}
	refused := []string{
		"openspec/changes/example-change/tasks.md",
		"openspec/changes/example-change/specs/example/area/spec.md",
		"openspec/changes/example-change/proposal.md",
		"ldd/changes/pagination/tasks.md",
		"openspec/changes/archive/2026-01-01-old/exploration.md",
		"openspec/specs/example/spec.md",
		"exploration.md",
		"src/app.go",
		"notes/research.md",
		// Nested layouts: only the project root's openspec/ or ldd/ counts.
		"src/openspec/changes/c/exploration.md",
		"pkg/ldd/changes/c/research.md",
	}
	for _, rt := range []string{"claude-code", "pi"} {
		for _, tool := range []string{"Write", "Edit", "MultiEdit"} {
			for _, rel := range allowed {
				t.Run(rt+" "+tool+" may write "+rel, func(t *testing.T) {
					t.Parallel()
					code(t, exploreCall(t, rt, "explore", tool, repo, rel), 0, rel)
				})
			}
			for _, rel := range refused {
				t.Run(rt+" "+tool+" may not write "+rel, func(t *testing.T) {
					t.Parallel()
					r := exploreCall(t, rt, "explore", tool, repo, rel)
					code(t, r, 2, rel)
					matches(t, r.Stderr, exploreRefusal+tool+` of `+regexp.QuoteMeta(filepath.Join(repo, rel))+` is refused`, rel)
				})
			}
		}
		t.Run(rt+" NotebookEdit may not write a notebook", func(t *testing.T) {
			t.Parallel()
			code(t, exploreCall(t, rt, "explore", "NotebookEdit", repo, "nb/a.ipynb"), 2, "notebook")
		})
	}
}

func TestExploreScopeLeavesOtherAgents(t *testing.T) {
	t.Parallel()
	repo := realTemp(t)
	support.GitRepo(t, repo)
	for _, rt := range []string{"claude-code", "pi"} {
		for _, agent := range []string{"worker", "verify", "jhon-orchestrator"} {
			for _, rel := range []string{"openspec/changes/c/tasks.md", "src/app.go"} {
				t.Run(rt+" "+agent+" "+rel, func(t *testing.T) {
					t.Parallel()
					code(t, exploreCall(t, rt, agent, "Write", repo, rel), 0, rel)
				})
			}
		}
	}
	// pi's top-level session: no child, no agent named.
	payload := map[string]any{"tool_name": "Write", "cwd": repo, "tool_input": map[string]any{"file_path": filepath.Join(repo, "src/app.go")}}
	env := support.Env("JHON_PI_CHILD=", "JHON_PI_AGENT=explore")
	code(t, hook(t, env, "vcs-guard", append([]string{"--agent", "*"}, vcsDirs(t)...), payload), 0, "pi top level")
	// Claude Code: explore's guard leaves a worker's call to the worker's hooks.
	code(t, vcsCall(t, "explore", filepath.Join(repo, "src/app.go"), ptr("worker"), "Write", repo), 0, "another agent's call")
}

// pre-tool, as both runtimes call it, gives the refusal unchanged.
func TestExploreScopeThroughPreTool(t *testing.T) {
	t.Parallel()
	repo := realTemp(t)
	support.GitRepo(t, repo)
	payload := map[string]any{"tool_name": "Write", "cwd": repo, "agent_id": "pi-child", "tool_input": map[string]any{"file_path": filepath.Join(repo, "openspec/changes/c/tasks.md")}}
	env := support.Env("JHON_PI_CHILD=1", "JHON_PI_AGENT=explore")
	args := append(append([]string{"--agent", "*", "safety-guard", "vcs-guard"}, vcsDirs(t)...), "secrets-guard")
	r := hook(t, env, "pre-tool", args, payload)
	code(t, r, 2, "pi pre-tool")
	matches(t, r.Stderr, exploreRefusal, "pi pre-tool")
}

// The guard judges the real path too: a symlink never carries explore's
// write out of its change's folder (T123, L773 G1).
func TestExploreScopeSymlinks(t *testing.T) {
	t.Parallel()
	repo := realTemp(t)
	support.GitRepo(t, repo)
	outside := realTemp(t)
	for _, d := range []string{"src", "openspec/changes/c", "x/openspec", "ldd/changes/d"} {
		if err := os.MkdirAll(filepath.Join(repo, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, at string) {
		if err := os.Symlink(target, filepath.Join(repo, at)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "src/app.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link("../../../src/app.go", "openspec/changes/c/research.md")
	link(outside, "openspec/changes/lnk")
	link("../src", "ldd/changes/d/sub")
	link(filepath.Join(repo, "openspec/changes"), "x/openspec/changes")
	refused := []string{
		"openspec/changes/c/research.md",
		"openspec/changes/lnk/exploration.md",
		"x/openspec/changes/c/exploration.md",
		"src/openspec/changes/c/exploration.md",
	}
	allowed := []string{"openspec/changes/c/exploration.md", "openspec/changes/../changes/c/exploration.md", "ldd/changes/d/research.md"}
	for _, rt := range []string{"claude-code", "pi"} {
		for _, rel := range refused {
			t.Run(rt+" may not write "+rel, func(t *testing.T) {
				t.Parallel()
				r := exploreCall(t, rt, "explore", "Write", repo, rel)
				code(t, r, 2, rel)
				matches(t, r.Stderr, exploreRefusal+`Write of `+regexp.QuoteMeta(filepath.Join(repo, rel))+` is refused`, rel)
			})
		}
		for _, rel := range allowed {
			t.Run(rt+" may write "+rel, func(t *testing.T) {
				t.Parallel()
				code(t, exploreCall(t, rt, "explore", "Write", repo, rel), 0, rel)
			})
		}
		// A relative path, as the agent may give it.
		t.Run(rt+" relative", func(t *testing.T) {
			t.Parallel()
			payload := map[string]any{"tool_name": "Write", "cwd": repo, "tool_input": map[string]any{"file_path": "openspec/changes/c/exploration.md"}}
			code(t, exploreHook(t, rt, payload), 0, "relative record")
			payload = map[string]any{"tool_name": "Write", "cwd": repo, "tool_input": map[string]any{"file_path": "openspec/changes/c/research.md"}}
			code(t, exploreHook(t, rt, payload), 2, "relative symlink")
		})
	}
}

// exploreHook runs vcs-guard on payload as explore in runtime.
func exploreHook(t *testing.T, runtime string, payload map[string]any) support.Result {
	t.Helper()
	if runtime == "pi" {
		payload["agent_id"] = "pi-child"
		return hook(t, support.Env("JHON_PI_CHILD=1", "JHON_PI_AGENT=explore"), "vcs-guard", append([]string{"--agent", "*"}, vcsDirs(t)...), payload)
	}
	payload["agent_type"] = "explore"
	return hook(t, support.Env("JHON_PI_CHILD=", "JHON_PI_AGENT="), "vcs-guard", append([]string{"--agent", "explore"}, vcsDirs(t)...), payload)
}
