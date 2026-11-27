package execution

import (
	"path/filepath"
	"reflect"
	"testing"
)

// jhon-studio's working output, .jhon/studio/ at any depth (T8g), is no
// project input: alone it selects nothing, with its own reason; beside a
// source edit the selection is the one the edit makes without it.
func TestSelectAffectedLeavesStudioOutputOut(t *testing.T) {
	root := nodeWorkspace(t)
	write(t, filepath.Join(root, ".jhon/studio/tour/state.json"), "{}\n")
	write(t, filepath.Join(root, ".jhon/studio/tour/out/tour.mp4"), "MP4\n")
	write(t, filepath.Join(root, "app/.jhon/studio/tour/script.json"), "{}\n")
	s, msg, err := SelectAffected(root, "base", projects, projects, nil)
	want := Selection{Affected, []Selected{}, []string{"3 changed file(s) under .jhon/studio/ ignored: jhon-studio's working output is not project input", "no project is affected since base"}}
	if msg != "" || err != nil || s.Scope != want.Scope || len(s.Selected) != 0 || !reflect.DeepEqual(s.Reasons, want.Reasons) {
		t.Errorf("studio output alone: %+v, %q, %v; want %+v", s, msg, err, want)
	}

	write(t, filepath.Join(root, "lib/index.js"), "x\n")
	s, msg, err = SelectAffected(root, "base", projects, projects, nil)
	want = Selection{Affected, []Selected{{"app", "depends on lib"}, {"lib", "changed: lib/index.js"}, {"tool", "depends on lib"}},
		[]string{"3 changed file(s) under .jhon/studio/ ignored: jhon-studio's working output is not project input"}}
	if msg != "" || err != nil || !reflect.DeepEqual(s, want) {
		t.Errorf("a source edit beside studio output: %+v, %q, %v; want %+v", s, msg, err, want)
	}
	// The record's own line is unchanged beside it.
	write(t, filepath.Join(root, "openspec/x.md"), "y\n")
	if s, _, _ := SelectAffected(root, "base", projects, projects, nil); !reflect.DeepEqual(s.Reasons, []string{
		"1 changed file(s) under openspec/ ignored: the change's record is not project input",
		"3 changed file(s) under .jhon/studio/ ignored: jhon-studio's working output is not project input",
	}) || !reflect.DeepEqual(s.Selected, want.Selected) {
		t.Errorf("the record beside studio output: %+v", s)
	}
}
