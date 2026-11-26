package sandbox

// T251's adversarial verify (odd/tasks/v0-1-release.md L1729): a `..`
// after a link (B1), gh run in the repository (B3), an allow_write folder
// that is a link (B4), and the notes the advisories named.

import (
	"testing"
)

// t251xDisk is t251Disk with a link in the allowed folder to a folder of a
// sandboxed command's outside it, and one in the project.
var t251xDisk = with(t251Disk, disk{
	"/home/u/work/x": "-> /home/u/.cache/q/y", "/home/u/.cache/q": "dir", "/home/u/.cache/q/y": "dir", "/home/u/.cache/q/b": "dir", "/home/u/.cache/q/b/.git": "dir",
	"/home/u/project/x": "-> /home/u/.cache/q/y", "/home/u/project/sub": "dir",
})

func t251xInput(cwd string, allow ...string) Input {
	in := t251Input(cwd, allow...)
	in.Stat = t251xDisk.stat
	return in
}

// B1: git -C follows a link physically, so `x/..` is the parent of where x
// leads. A repository line with a `..` in its -C folders or working folder
// stays inside, however it is spelled (one -C, two, after a cd); a project
// line keeps a `..` that follows no link, and loses one that does.
func TestT251XADotDotKeepsARepositoryLineInside(t *testing.T) {
	const project = "/home/u/project"
	for _, c := range []struct{ cwd, line, want string }{
		{project, "git -C /home/u/work/x/../b fetch origin", Work},
		{project, "git -C /home/u/work/x/.. -C b fetch origin", Work},
		{project, "git -C /home/u/work/x -C ../b fetch origin", Work},
		{project, "git -C /home/u/work/b/../b fetch origin", Work},
		{project, "cd /home/u/work/b/sub && git -C .. fetch", Work},
		{"/home/u/work/b/sub", "git -C .. fetch", Work},
		{project, "git -C /home/u/work/b/. fetch origin", Unsandboxed},
		{project, "git -C /home/u/work/b/./sub fetch origin", Unsandboxed},
		// The project: a `..` after a link goes where the link leads.
		{project, "git -C /home/u/project/x/../sub fetch", Work},
		{project, "git -C /home/u/project/x/.. -C sub fetch", Work},
		{project + "/sub", "git -C .. pull", Unsandboxed},
		{project, "git -C /home/u/project/sub/.. fetch", Unsandboxed},
	} {
		if got := ProfileIn(t251xInput(c.cwd, "/home/u/work"), c.line); got != c.want {
			t.Errorf("ProfileIn(%q in %s) = %q, want %q", c.line, c.cwd, got, c.want)
		}
	}
	h := host("linux", t251xDisk, nil)
	for line, want := range map[string]string{
		"git -C /home/u/work/x/../b fetch origin": keptPrefix + "/home/u/work/x/../b goes up with `..`, and a line outside the sandbox works in a repository only by a path with no `..` (git goes up from where a link on the way leads, not from the path as written); fix: it leads to /home/u/.cache/q/b, which is neither in the project nor in a git repository a line outside the sandbox may work in, so run the line yourself in a terminal",
		"git -C /home/u/work/b/../b fetch":        keptPrefix + "/home/u/work/b/../b goes up with `..`, and a line outside the sandbox works in a repository only by a path with no `..` (git goes up from where a link on the way leads, not from the path as written); fix: name its real path (/home/u/work/b) instead",
		"git -C /home/u/project/x/../sub fetch":   keptPrefix + "/home/u/project/x/../sub goes up with `..` after the link /home/u/project/x, so git would work in /home/u/.cache/q/sub, not where the path says; fix: it leads to /home/u/.cache/q/sub, which is neither in the project nor in a git repository a line outside the sandbox may work in, so run the line yourself in a terminal",
	} {
		if got := DeliveryNoteAt(h, t251xInput(project, "/home/u/work"), []string{"/usr/bin/bash", "-c", line}, nil); got != want {
			t.Errorf("DeliveryNoteAt(%q) =\n %q\nwant\n %q", line, got, want)
		}
	}
}

// B3: gh run in the repository is a line of it, as git run there is (L1722:
// git and gh, in the repository or run in it); docker and a deploy tool run
// there stay inside, with the reason. gh and docker run outside the project
// and every allowed repository are as before.
func TestT251XGhRunInTheRepositoryIsItsLine(t *testing.T) {
	h := host("linux", t251Disk, nil)
	const b = "/home/u/work/b"
	for _, cwd := range []string{b, b + "/sub"} {
		in := t251Input(cwd, "/home/u/work")
		if top, kept, ok := Outside(h, in, []string{"/usr/bin/bash", "-c", "gh pr list"}); top != b || kept != nil || !ok {
			t.Errorf("gh pr list in %s: Outside = %q, %v, %v; want b's top", cwd, top, kept, ok)
		}
		if got := OutsideAreas(in, "gh pr list"); !contains(got, b) {
			t.Errorf("gh pr list in %s: OutsideAreas = %q; want b's top", cwd, got)
		}
	}
	in := t251Input(b, "/home/u/work")
	if got := ProfileIn(in, "docker ps"); got != Work {
		t.Errorf("docker ps in b: %q, want work", got)
	}
	want := keptPrefix + "docker runs outside the sandbox only in the project, and /home/u/work/b is in a git repository inside a sandbox.allow_write folder, where only git and gh do; fix: run docker on a line of its own, from the project or a session of that repository"
	if got := DeliveryNoteAt(h, in, []string{"/usr/bin/bash", "-c", "gh pr list && docker ps"}, nil); got != want {
		t.Errorf("gh and docker in b:\n %q\nwant\n %q", got, want)
	}
	for cwd, line := range map[string]string{"/home/u/other": "gh pr list", "/home/u/work/plain": "docker ps", "/home/u/project": "gh pr list"} {
		if got := ProfileIn(t251Input(cwd, "/home/u/work"), line); got != Unsandboxed {
			t.Errorf("%s in %s: %q, want unsandboxed as before", line, cwd, got)
		}
	}
	if top, _, ok := Outside(h, t251Input("/home/u/other", "/home/u/work"), []string{"/usr/bin/bash", "-c", "gh pr list"}); top != "" || !ok {
		t.Errorf("gh pr list outside every allowed folder: %q, %v; want the project's line as before", top, ok)
	}
}

// B4: an allow_write folder that is a link is no folder the sandbox opens
// for writing; the note tells the owner to set the real folder, whichever
// path the line names.
func TestT251XAnAllowedFolderThatIsALinkSaysToSetTheRealFolder(t *testing.T) {
	h := host("linux", t251Disk, nil)
	in := t251Input("/home/u/project", "/home/u/wlink")
	fix := "; fix: set sandbox.allow_write to the real folder (/home/u/work) instead of /home/u/wlink (jhon settings set sandbox.allow_write <folders>,…)"
	for line, want := range map[string]string{
		"git -C /home/u/wlink/b fetch": keptPrefix + "the sandbox.allow_write folder /home/u/wlink is or passes through the link /home/u/wlink, and the sandbox opens no link for writing, so /home/u/wlink/b counts as no project" + fix,
		"git -C /home/u/work/b fetch":  keptPrefix + "/home/u/work/b is in /home/u/work, where the sandbox.allow_write folder /home/u/wlink leads, and the sandbox opens no link for writing, so it counts as no project" + fix,
	} {
		if got := DeliveryNoteAt(h, in, []string{"/usr/bin/bash", "-c", line}, nil); got != want {
			t.Errorf("DeliveryNoteAt(%q) =\n %q\nwant\n %q", line, got, want)
		}
	}
}

// Advisories: a line that is alone is not told to run alone; a submodule's
// core.worktree is not called a move outside the project.
func TestT251XNotesSayTheRealReason(t *testing.T) {
	h := host("linux", t251Disk, nil)
	in := t251Input("/home/u/project", "/home/u/work")
	alone := "git over SSH and gh ran in the sandbox, not outside it: the line is alone, but holds an option, a word the shell would still rewrite ($, a backtick, ~, a glob or brace), a redirect to a file, or a subcommand that never runs outside the sandbox; fix: run git's fetch, pull, push, ls-remote, clone or remote update, or gh's own commands, with only their usual options and literal words, or run the line yourself in a terminal"
	for line, want := range map[string]string{
		"git fetch --upload-pack=x origin":    alone,
		"git fetch origin > /tmp/out":         alone,
		"gh auth token":                       alone,
		"cd /home/u/project && git fetch ~/x": alone,
		"git fetch origin | cat":              deliveryNote,
		"git fetch origin; git push origin":   deliveryNote,
		"git fetch $(echo origin)":            deliveryNote,
	} {
		if got := DeliveryNoteAt(h, in, []string{"/usr/bin/bash", "-c", line}, nil); got != want {
			t.Errorf("DeliveryNoteAt(%q) =\n %q\nwant\n %q", line, got, want)
		}
	}
	if why := RepositoryGitConfig(entry("local", "core.worktree", "../../../sub"), nil); why != "the repository's configuration sets core.worktree (as a submodule's does), which may put the files git writes outside the repository" {
		t.Errorf("core.worktree: %q", why)
	}
}
