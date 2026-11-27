package hooks

// jhon-studio's own files under a project's .jhon/studio/ (a video's brief,
// script, screens, review image, renders and state) are its working output,
// not code a change is responsible for (T8g): route-guard notes no LDD set-up
// for writing them, and the session brief's route does not count them as the
// change's edits. A source edit beside them keeps every prompt it had.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// studioOutput is what /jhon:video leaves in a project: the files of one
// video, tour, as jhon-studio's commands write them.
var studioOutput = map[string]string{
	".jhon/studio/tour/brief.md":     "# tour\n",
	".jhon/studio/tour/script.json":  "{\"shots\":[{\"id\":\"a\",\"text\":\"hello\"}]}\n",
	".jhon/studio/tour/review.png":   "PNG\n",
	".jhon/studio/tour/state.json":   "{\"step\":\"render\"}\n",
	".jhon/studio/tour/out/tour.mp4": "MP4\n",
}

// studioScratch is a repository with code, committed on main, with LDD set
// up (openspec/config.yaml) or not, and the environment its hooks run in:
// a HOME of its own with the studio setting on.
func studioScratch(t *testing.T, ldd bool) (repo string, env []string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo, home := filepath.Join(base, "proj"), filepath.Join(base, "home")
	files := map[string]string{"src/app.py": "X = 1\n"}
	if ldd {
		files["openspec/config.yaml"] = "delivery: change\n"
		files["openspec/specs/.keep"] = ""
		files["openspec/changes/archive/.keep"] = ""
	}
	support.Write(t, repo, files)
	support.Write(t, home, map[string]string{".config/jhon/settings.toml": "schema = 1\n\n[integrations]\nstudio = true\n"})
	support.GitInit(t, repo)
	support.Git(t, repo, "add", "-A")
	support.Git(t, repo, "commit", "-q", "-m", "base")
	if ldd {
		support.Git(t, repo, "switch", "-q", "-c", "feat/tour")
	}
	return repo, support.Env("HOME="+home, "XDG_STATE_HOME="+filepath.Join(base, "state"), "XDG_CACHE_HOME="+filepath.Join(base, "cache"), "JHON_ROOT="+support.Root())
}

// guarded is the pre-tool call the runtimes make for a tool call in repo,
// with route-guard among its guards.
func guarded(t *testing.T, env []string, repo, tool string, input map[string]any) support.Result {
	t.Helper()
	payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input, "cwd": repo, "session_id": "s", "permission_mode": "default"}
	return support.Hook(t, env, "pre-tool", payload, "--agent", "*", "safety-guard", "route-guard")
}

// brief is the session brief of a session starting in repo.
func brief(t *testing.T, env []string, repo string) (support.Result, map[string]string) {
	t.Helper()
	r := support.Hook(t, env, "session-brief", map[string]any{"cwd": repo}, "jhon-orchestrator")
	var out map[string]string
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatalf("session-brief: %v\n%s", err, r.Stdout)
	}
	return r, out
}

// quiet fails the test unless r ran with exit 0 and printed nothing.
func quiet(t *testing.T, r support.Result, what string) {
	t.Helper()
	if r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
		t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit 0 and nothing printed", what, r.Code, r.Stdout, r.Stderr)
	}
}

func TestAStudioWriteGetsNoLddPrompt(t *testing.T) {
	t.Parallel()
	// No layout: writing jhon-studio's files is no code edit to set LDD up for.
	repo, env := studioScratch(t, false)
	support.Write(t, repo, studioOutput)
	for _, f := range []string{".jhon/studio/tour/script.json", ".jhon/studio/tour/review.png", ".jhon/studio/tour/out/tour.mp4"} {
		quiet(t, guarded(t, env, repo, "Write", map[string]any{"file_path": filepath.Join(repo, f), "content": "x"}), "Write "+f)
	}
	quiet(t, guarded(t, env, repo, "Edit", map[string]any{"file_path": filepath.Join(repo, ".jhon/studio/tour/brief.md"), "old_string": "tour", "new_string": "Tour"}), "Edit brief.md")
	quiet(t, guarded(t, env, repo, "Bash", map[string]any{"command": "printf '{}' > .jhon/studio/tour/state.json"}), "a Bash write of state.json")
	quiet(t, guarded(t, env, repo, "Bash", map[string]any{"command": "mkdir -p .jhon/studio/tour/screens"}), "a Bash mkdir under .jhon/studio")

	// Under LDD, on the change's branch: the studio's files are no edit of
	// the change, so the route has none to count and a PR asks for no RED.
	repo, env = studioScratch(t, true)
	support.Write(t, repo, studioOutput)
	r, out := brief(t, env, repo)
	if r.Code != 0 || r.Stderr != "" {
		t.Fatalf("session-brief: exit %d, stderr %q", r.Code, r.Stderr)
	}
	const want = "- Route, from the change's diff now: standard: risk passive is under the verified tier (high); its affected tests deliver it. The method is LDD"
	if !strings.Contains(out["brief"], want) || strings.Contains(out["brief"], "it alters behaviour") || out["context"] != "lean" {
		t.Errorf("session-brief with studio files only: want its route to read %q, lean:\n%s", want, r.Stdout)
	}
	quiet(t, guarded(t, env, repo, "Bash", map[string]any{"command": "gh pr create --title x --body y"}), "gh pr create with studio files only")
}

func TestASourceEditBesideStudioFilesKeepsItsLddPrompt(t *testing.T) {
	t.Parallel()
	// No layout: the source edit's note is the one it gets with no studio file.
	with, env := studioScratch(t, false)
	without, envWithout := studioScratch(t, false)
	support.Write(t, with, studioOutput)
	edit := func(env []string, repo string) support.Result {
		return guarded(t, env, repo, "Write", map[string]any{"file_path": filepath.Join(repo, "src", "app.py"), "content": "X = 2\n"})
	}
	got, want := edit(env, with), edit(envWithout, without)
	if got.Code != 0 || got.Stderr != "" || !strings.Contains(got.Stdout, "LDD is not set up in this project yet: set it up by following `") ||
		!strings.Contains(got.Stdout, "start this edit's change (tasks.md) and record a run (`jhon executions run`)") || got.Stdout != want.Stdout {
		t.Errorf("Write src/app.py beside studio files: exit %d, stdout %q, stderr %q; want the set-up note it gets without them, %q", got.Code, got.Stdout, got.Stderr, want.Stdout)
	}

	// Under LDD: the brief and a PR read the source edit alone.
	with, env = studioScratch(t, true)
	without, envWithout = studioScratch(t, true)
	support.Write(t, with, studioOutput)
	for _, repo := range []string{with, without} {
		support.Write(t, repo, map[string]string{"src/app.py": "X = 2\n"})
	}
	gotBrief, gotOut := brief(t, env, with)
	wantBrief, _ := brief(t, envWithout, without)
	if gotBrief.Stdout != wantBrief.Stdout || !strings.Contains(gotOut["brief"], "it alters behaviour, so its record goes with it") {
		t.Errorf("session-brief beside studio files:\n%s\nwant the one without them:\n%s", gotBrief.Stdout, wantBrief.Stdout)
	}
	pr := func(env []string, repo string) support.Result {
		return guarded(t, env, repo, "Bash", map[string]any{"command": "gh pr create --title x --body y"})
	}
	gotPR, wantPR := pr(env, with), pr(envWithout, without)
	if gotPR.Code != 2 || !strings.HasPrefix(gotPR.Stderr, "route-guard: this change alters behaviour") || gotPR.Stderr != wantPR.Stderr || gotPR.Stdout != wantPR.Stdout {
		t.Errorf("gh pr create beside studio files: exit %d, stderr %q; want the refusal it gets without them, exit %d, %q", gotPR.Code, gotPR.Stderr, wantPR.Code, wantPR.Stderr)
	}
	if _, err := os.Stat(filepath.Join(with, ".jhon", "studio", "tour", "script.json")); err != nil {
		t.Errorf("the studio's files are left in place: %v", err)
	}
}
