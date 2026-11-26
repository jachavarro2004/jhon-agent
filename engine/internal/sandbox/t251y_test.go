package sandbox

// T251's second re-verify (odd/tasks/v0-1-release.md L1731): every folder a
// line writes or works in is listed for the anchors, and the notes the
// advisories named.

import (
	"strings"
	"testing"
)

// The folders LineFoldersProblem anchors: the repository's top, the working
// folder, a leading cd's folder, each -C as written in the chain, and the
// clone's new folder as written (its deepest existing part is found when
// it is checked), each with its root (top or
// the project's) and how its `..` is judged.
func TestT251YLineFoldersListsEveryFolderTheLineWritesOrWorksIn(t *testing.T) {
	d := with(t251Disk, disk{"/home/u/work/b/sub": "dir"})
	in := t251Input("/home/u/project", "/home/u/work")
	in.Stat = d.stat
	type f struct {
		written, root string
		strict, here  bool
	}
	got := func(line, top string) []f {
		var out []f
		for _, x := range in.lineFolders(line, top) {
			out = append(out, f{x.written, x.root, x.strict, x.here})
		}
		return out
	}
	const p, b = "/home/u/project", "/home/u/work/b"
	for _, c := range []struct {
		line, top string
		want      []f
	}{
		{"git fetch", "", []f{{p, p, false, true}}},
		{"git -C /home/u/project/sub/../b fetch", "", []f{{p, p, false, true}, {p + "/sub/../b", p, false, false}}},
		{"git -C sub -C .. fetch", "", []f{{p, p, false, true}, {p + "/sub", p, false, false}, {p + "/sub/..", p, false, false}}},
		{"git -C /home/u/work/b clone https://h/o/r sub/newc", b, []f{{p, p, false, true}, {b, b, true, false}, {b + "/sub/newc", b, true, false}}},
		{"cd /home/u/work/b && git pull", b, []f{{p, p, false, true}, {b, b, true, false}}},
		{"git clone https://h/o/r.git", "", []f{{p, p, false, true}, {p + "/r", p, false, false}}},
	} {
		w := got(c.line, c.top)
		if len(w) != len(c.want) {
			t.Errorf("lineFolders(%q) = %+v, want %+v", c.line, w, c.want)
			continue
		}
		for i := range w {
			if w[i] != c.want[i] {
				t.Errorf("lineFolders(%q) = %+v, want %+v", c.line, w, c.want)
				break
			}
		}
	}
}

// Notes (L1731): cdk in the repository says why, not "run it alone"; the
// `..` fix names a real path only where it runs; a removed working folder
// says so; settings that cannot be read (allow_write = ["~"]) are named.
func TestT251YNotesForTheAdvisories(t *testing.T) {
	h := host("linux", t251Disk, nil)
	in := t251Input("/home/u/project", "/home/u/work")
	in.DeployTools = []string{"cdk"}
	want := "cdk ran in the sandbox, not outside it: cdk runs outside the sandbox only in the project, and /home/u/work/b is in a git repository inside a sandbox.allow_write folder, where only git and gh do; fix: run cdk on a line of its own, from the project or a session of that repository"
	if got := DeployNoteAt(h, in, []string{"/usr/bin/bash", "-c", "cd /home/u/work/b && cdk deploy"}, nil); got != want {
		t.Errorf("cdk in b:\n %q\nwant\n %q", got, want)
	}
	if got := DeployNoteAt(h, in, []string{"/usr/bin/bash", "-c", "cdk deploy | tail -1"}, nil); !strings.Contains(got, "run it alone") {
		t.Errorf("a piped cdk keeps its note: %q", got)
	}
	gone := t251Input("", "/home/u/work")
	if got := DeliveryNoteAt(h, gone, []string{"/usr/bin/bash", "-c", "git fetch origin"}, nil); got != keptPrefix+"the folder this line runs in cannot be named (it was removed, or is not an absolute path); fix: run the line from a folder that exists" {
		t.Errorf("a removed working folder: %q", got)
	}
	unread := t251Input("/home/u/project")
	unread.SettingsProblem = `settings.toml: sandbox.allow_write: "~" is not accepted`
	if got := DeliveryNoteAt(h, unread, []string{"/usr/bin/bash", "-c", "git -C /home/u/work/b fetch"}, nil); !strings.HasSuffix(got, `; sandbox.allow_write counts for nothing now, as the settings cannot be read: settings.toml: sandbox.allow_write: "~" is not accepted`) {
		t.Errorf("unreadable settings: %q", got)
	}
	if p := RepositoryGitConfigKept(entry("local", "core.worktree", "../../../s"), nil); p == nil || p.Fix != "run the line in the repository that holds it (a submodule's superproject), or yourself in a terminal" {
		t.Errorf("core.worktree: %+v", p)
	}
	if p := RepositoryGitConfigKept(entry("local", "http.cookiefile", "/x"), nil); p == nil || p.Fix != plantedFix {
		t.Errorf("a key: %+v", p)
	}
}
