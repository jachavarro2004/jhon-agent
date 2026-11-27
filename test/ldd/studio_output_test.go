package ldd

// jhon-studio's own files under a project's .jhon/studio/ are its working
// output, not the change's (T8g): `jhon status` finds no change in them and
// asks for no RED, and `jhon risk` does not read them; beside a source edit,
// both say what they say without them.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// studioOutput is what /jhon:video leaves in a project: one video's files.
var studioOutput = map[string]string{
	".jhon/studio/tour/brief.md":     "# tour\n",
	".jhon/studio/tour/script.json":  "{\"shots\":[{\"id\":\"a\",\"text\":\"hello\"}]}\n",
	".jhon/studio/tour/review.png":   "PNG\n",
	".jhon/studio/tour/state.json":   "{\"step\":\"render\"}\n",
	".jhon/studio/tour/out/tour.mp4": "MP4\n",
}

// studioProject is a repository under LDD on branch feat/tour, its code
// committed on main, and the environment jhon runs in there: a HOME of its
// own with the studio setting on.
func studioProject(t *testing.T) (repo string, env []string) {
	t.Helper()
	base := temp(t)
	repo, home := filepath.Join(base, "proj"), filepath.Join(base, "home")
	support.Write(t, repo, map[string]string{"src/app.py": "X = 1\n", "openspec/config.yaml": "delivery: change\n", "openspec/specs/.keep": "", "openspec/changes/archive/.keep": ""})
	support.Write(t, home, map[string]string{".config/jhon/settings.toml": "schema = 1\n\n[integrations]\nstudio = true\n"})
	support.GitInit(t, repo)
	support.Git(t, repo, "add", "-A")
	support.Git(t, repo, "commit", "-q", "-m", "base")
	support.Git(t, repo, "switch", "-q", "-c", "feat/tour")
	return repo, support.Env("HOME="+home, "XDG_STATE_HOME="+filepath.Join(base, "state"), "XDG_CACHE_HOME="+filepath.Join(base, "cache"))
}

// studioStatus is `jhon status --json` in repo, its route, needs, change, next
// and notes, the scratch folder holding the repository and its HOME read
// as <base>.
func studioStatus(t *testing.T, env []string, repo string) map[string]any {
	t.Helper()
	r := support.Jhon(t, repo, env, "status", "--json")
	if r.Code != 0 || r.Stderr != "" {
		t.Fatalf("status --json: exit %d, stderr %q", r.Code, r.Stderr)
	}
	var s map[string]any
	if err := json.Unmarshal([]byte(strings.ReplaceAll(r.Stdout, filepath.Dir(repo), "<base>")), &s); err != nil {
		t.Fatalf("status --json: %v\n%s", err, r.Stdout)
	}
	return map[string]any{"route": s["route"], "needs": s["needs"], "change": s["change"], "next": s["next"], "notes": s["notes"]}
}

func TestStudioOutputIsNoChangeOfTheBranch(t *testing.T) {
	t.Parallel()
	repo, env := studioProject(t)
	fresh, envFresh := studioProject(t)
	support.Write(t, repo, studioOutput)
	s, _ := json.Marshal(studioStatus(t, env, repo))
	want, _ := json.Marshal(studioStatus(t, envFresh, fresh))
	// The branch reads as one that changed nothing yet: no change, no route.
	if string(s) != string(want) || !strings.Contains(string(s), `"change":null`) || !strings.Contains(string(s), `"route":null`) ||
		strings.Contains(string(s), "RED before GREEN") || strings.Contains(string(s), ".jhon/studio") ||
		!strings.Contains(string(s), "branch feat/tour names change feat-tour, which has no changes yet") {
		t.Errorf("status with studio files only:\n%s\nwant the one of a branch that changed nothing:\n%s", s, want)
	}
	r := support.Jhon(t, repo, env, "risk")
	if r.Code != 0 || r.Stderr != "" || r.Stdout != "risk: passive (base HEAD)\n  passive no change\n" {
		t.Errorf("risk with studio files only: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
}

func TestStudioOutputBesideASourceEditLeavesStatusAsWithout(t *testing.T) {
	t.Parallel()
	with, env := studioProject(t)
	without, envWithout := studioProject(t)
	support.Write(t, with, studioOutput)
	for _, repo := range []string{with, without} {
		support.Write(t, repo, map[string]string{"src/app.py": "X = 2\n"})
	}
	got, want := studioStatus(t, env, with), studioStatus(t, envWithout, without)
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) || !strings.Contains(string(g), `"behaviour":true`) || !strings.Contains(string(g), `"change":"feat-tour"`) {
		t.Errorf("status beside studio files:\n%s\nwant the one without them:\n%s", g, w)
	}
	gotRisk, wantRisk := support.Jhon(t, with, env, "risk"), support.Jhon(t, without, envWithout, "risk")
	if gotRisk.Code != 0 || gotRisk.Stdout != wantRisk.Stdout || !strings.Contains(gotRisk.Stdout, "src/app.py") || strings.Contains(gotRisk.Stdout, ".jhon/studio") {
		t.Errorf("risk beside studio files: exit %d, %q; want the one without them, %q", gotRisk.Code, gotRisk.Stdout, wantRisk.Stdout)
	}
}

// goProjects is a repository under LDD with two Go projects, a and b, on
// branch feat/tour, and the environment jhon runs in there.
func goProjects(t *testing.T) (repo string, env []string) {
	t.Helper()
	repo, env = studioProject(t)
	for _, p := range []string{"a", "b"} {
		support.Write(t, repo, map[string]string{
			p + "/go.mod":       "module example.com/" + p + "\n\ngo 1.22\n",
			p + "/" + p + ".go": "package " + p + "\n\nfunc X() int { return 1 }\n",
			p + "/x_test.go":    "package " + p + "\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
		})
	}
	support.Git(t, repo, "add", "-A")
	support.Git(t, repo, "commit", "-q", "-m", "projects")
	support.Git(t, repo, "branch", "-f", "main", "HEAD")
	return repo, env
}

// An affected run reads no studio file as project input: beside a source
// edit in a, it runs a's tests, as without them, and says what it ignored.
func TestAffectedRunLeavesStudioOutputOut(t *testing.T) {
	t.Parallel()
	with, env := goProjects(t)
	without, envWithout := goProjects(t)
	support.Write(t, with, studioOutput)
	for _, repo := range []string{with, without} {
		support.Write(t, repo, map[string]string{"a/a.go": "package a\n\nfunc X() int { return 2 }\n"})
	}
	run := func(env []string, repo string) (support.Result, string) {
		r := support.Jhon(t, repo, env, "executions", "run", "feat-tour", "--affected", "main", "--dry-run")
		return r, strings.ReplaceAll(r.Stdout, filepath.Dir(repo), "<base>")
	}
	got, gotOut := run(env, with)
	want, wantOut := run(envWithout, without)
	const note = "note  5 changed file(s) under .jhon/studio/ ignored: jhon-studio's working output is not project input\n"
	if got.Code != 0 || got.Stderr != "" || !strings.Contains(gotOut, note) || strings.Replace(gotOut, note, "", 1) != wantOut ||
		strings.Contains(gotOut, "every project selected") || !strings.Contains(gotOut, "would run  a: ") || strings.Contains(gotOut, "would run  b: ") {
		t.Errorf("--affected beside studio files: exit %d, stderr %q, stdout:\n%s\nwant the run without them (exit %d) plus %q:\n%s", got.Code, got.Stderr, gotOut, want.Code, note, wantOut)
	}
}

// T8g verify (mwyf7mgi): .jhon/studio.yaml is the project's settings, not
// studio output, so it still counts as a change's edit.
func TestStudioSettingsStillCount(t *testing.T) {
	t.Parallel()
	repo, env := studioProject(t)
	support.Write(t, repo, map[string]string{".jhon/studio.yaml": "x: 1\n"})
	r := support.Jhon(t, repo, env, "risk")
	if r.Code != 0 || !strings.Contains(r.Stdout, "medium  .jhon/studio.yaml: other file") {
		t.Errorf("risk with studio.yaml: %d %q", r.Code, r.Stdout)
	}
	if s := studioStatus(t, env, repo); s["change"] != "feat-tour" {
		t.Errorf("status with studio.yaml: %+v", s)
	}
}

// T268: a tool's own data (CodeGraph's index, untracked, at the root and in
// a project) is no test input either: beside a source edit in a, an
// affected run runs a's tests, as without it, and needs no note for it.
func TestAffectedRunLeavesToolDataOut(t *testing.T) {
	t.Parallel()
	with, env := goProjects(t)
	without, envWithout := goProjects(t)
	support.Write(t, with, map[string]string{".codegraph/codegraph.db": "index\n", "b/.codegraph/codegraph.db": "index\n"})
	for _, repo := range []string{with, without} {
		support.Write(t, repo, map[string]string{"a/a.go": "package a\n\nfunc X() int { return 2 }\n"})
	}
	run := func(env []string, repo string) (support.Result, string) {
		r := support.Jhon(t, repo, env, "executions", "run", "feat-tour", "--affected", "main", "--dry-run")
		return r, strings.ReplaceAll(r.Stdout, filepath.Dir(repo), "<base>")
	}
	got, gotOut := run(env, with)
	_, wantOut := run(envWithout, without)
	if got.Code != 0 || got.Stderr != "" || gotOut != wantOut || !strings.Contains(gotOut, "would run  a: ") || strings.Contains(gotOut, "would run  b: ") {
		t.Errorf("--affected beside CodeGraph's index: exit %d, stderr %q, stdout:\n%s\nwant the run without it:\n%s", got.Code, got.Stderr, gotOut, wantOut)
	}
}
