package render

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// T134 H7 (L1019): the todo list is the agent's own next steps, never a copy
// of the change's tasks. No rendered agent or prompt tells an agent to put
// tasks.md's items in it, and the orchestrator's live task list says so.
var copiesTasks = regexp.MustCompile(`(?i)one (todo )?per task|tasks\.md is the list|(write|list) its tasks with`)

func TestNoRenderedTextCopiesTheChangeTasksIntoTheTodoList(t *testing.T) {
	home := renderedHome(t)
	_, pi := sharedPiHome(t)
	seen := 0
	for _, dir := range []string{filepath.Join(home, ".claude", "agents"), pi} {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				t.Error(err)
				return nil
			}
			seen++
			if m := copiesTasks.FindString(string(b)); m != "" {
				t.Errorf("%s tells an agent to copy the change's tasks: %q", p, m)
			}
			return nil
		})
	}
	if seen < 20 {
		t.Fatalf("read %d rendered files", seen)
	}
	want := map[string]string{
		filepath.Join(home, ".local", "share", "jhon", "current", "prose", "full-claude-code.md"): "keep in `TaskCreate` tasks your own next steps for this request",
		filepath.Join(pi, "jhon-orchestrator.md"):                                                 "keep in `todo` your own next steps for this request",
		filepath.Join(pi, "agents", "jhon-builder.md"):                                            "keep a todo list of your own next steps for this request",
	}
	for p, w := range want {
		if s := flat(string(must(os.ReadFile(p)))); !strings.Contains(s, w) {
			t.Errorf("%s lacks %q", p, w)
		}
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
