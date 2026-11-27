package sandbox

import (
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// T281: a write refused in a sibling that joined a workspace whose primary
// does not list it says so, with the entry that lists it (the measured trap:
// a silent "Read-only file system" in such a sibling); any other refused
// path is noted as before.
func TestTheRefusalNamesAJoinedSiblingThePrimaryDoesNotList(t *testing.T) {
	d := with(machine, disk{"/home/u/work/site": "dir", "/home/u/work/site/.git": "dir", "/home/u/docs": "dir"})
	in := sample(d)
	defer func(f func(string) (workspace.Unlisted, bool)) { unlistedAt = f }(unlistedAt)
	site := workspace.Unlisted{Root: "/home/u/work/site", Primary: "/home/u/project", Name: "site", Rel: "../work/site"}
	unlistedAt = func(p string) (workspace.Unlisted, bool) {
		if project.Within(p, site.Root) {
			return site, true
		}
		return workspace.Unlisted{}, false
	}
	const stop = "; that is a stop: tell the owner the path and why, never write it elsewhere"
	want := "the sandbox refused a write to /home/u/work/site/.git/FETCH_HEAD, which does not exist; " + site.Note() + stop
	if got := Refusal(in, "/home/u/work/site/.git/FETCH_HEAD", nil); got != want {
		t.Errorf("in the sibling:\n %s\nwant\n %s", got, want)
	}
	// A delivery line kept inside for working in the sibling says it too.
	site.Root = "/home/u/other"
	neither := "fix: run the line in a session of that repository (jhon start there), add a folder that holds the repository to sandbox.allow_write (jhon settings set sandbox.allow_write <folder>,…) if sandboxed commands may write it, or run the line yourself in a terminal"
	line := []string{"/usr/bin/bash", "-c", "git -C /home/u/other fetch origin"}
	if got, want := DeliveryNoteAt(host("linux", t251Disk, nil), t251Input("/home/u/project", "/home/u/work"), line, nil),
		keptPrefix+"/home/u/other is neither in the project (/home/u/project) nor in a git repository inside a sandbox.allow_write folder; "+site.Note()+"; "+neither; got != want {
		t.Errorf("a delivery line in the sibling:\n %s\nwant\n %s", got, want)
	}
	open := "; no folder the sandbox opens for writing holds it: sandbox.allow_write opens it, or a folder that holds it, from the next command (jhon settings set sandbox.allow_write <folder>,…)"
	if got, want := Refusal(in, "/home/u/docs/x", nil), "the sandbox refused a write to /home/u/docs/x, which does not exist"+open+stop; got != want {
		t.Errorf("elsewhere:\n %s\nwant\n %s", got, want)
	}
}
