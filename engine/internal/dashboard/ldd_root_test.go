package dashboard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// An openspec/ in a folder above a repository is not the repository's
// (L786, L853): the LDD panel shows none of its changes and the memory
// project is the repository's folder; at the repository's root it is found.
func TestLddOfIgnoresOpenSpecAboveTheRepository(t *testing.T) {
	if os.Getenv("JHON_ROOT") == "" {
		repo, _ := filepath.Abs("../../..")
		t.Setenv("JHON_ROOT", repo)
	}
	layout := func(dir string) {
		c := filepath.Join(dir, "openspec", "changes", "c1")
		if err := os.MkdirAll(c, 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, "openspec", "config.yaml"), "delivery: slice\n")
		write(t, filepath.Join(c, "tasks.md"), "## Slice 1: one\n\n- [ ] 1.1 a\n")
	}
	gitInit := func(dir string) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", "-C", dir, "init", "-q")
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %s", out)
		}
	}
	panel := func(cwd string) string {
		transcript := filepath.Join(t.TempDir(), "s.jsonl")
		write(t, transcript, `{"type":"user","cwd":"`+cwd+`"}`+"\n")
		return js.StringifyValueCompact(LddOf(transcript, []string{"c1"}))
	}
	parent, _ := filepath.EvalSymlinks(t.TempDir())
	layout(parent)
	inner := filepath.Join(parent, "inner")
	gitInit(inner)
	if out := panel(inner); out != "{}" {
		t.Errorf("LDD panel in a repository under openspec/: %s", out)
	}
	if got := engramProjectOf(inner); got != "inner" {
		t.Errorf("memory project in a repository under openspec/: %q, want inner", got)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	layout(root)
	gitInit(root)
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out := panel(filepath.Join(root, "src")); out == "{}" {
		t.Errorf("LDD panel from a subfolder of a repository with openspec/ at its root: %s", out)
	}
	if got := engramProjectOf(filepath.Join(root, "src")); got != filepath.Base(root) {
		t.Errorf("memory project from a subfolder: %q, want %q", got, filepath.Base(root))
	}
}
