package sandbox

import "testing"

// After T228 the guard reads what git runs from its own words (an option's
// program, a configured command, an alias): that is something the line
// holds, not another command beside it. A line that is alone keeps T251's
// note; a line with company keeps "run it alone".
func TestT228AnOptionGitRunsIsNoCompany(t *testing.T) {
	h := host("linux", t251Disk, nil)
	in := t251Input("/home/u/project", "/home/u/work")
	alone := keptNote + "the line is alone, but holds an option, a word the shell would still rewrite ($, a backtick, ~, a glob or brace), a redirect to a file, or a subcommand that never runs outside the sandbox; fix: run git's fetch, pull, push, ls-remote, clone or remote update, or gh's own commands, with only their usual options and literal words, or run the line yourself in a terminal"
	for line, want := range map[string]string{
		"git fetch --upload-pack=x origin":                    alone,
		"git -c core.sshCommand=x fetch origin":               alone,
		"git -c alias.f='!git fetch' f origin":                alone,
		"GIT_SSH_COMMAND='ssh -i k' git fetch origin":         alone,
		"git fetch --upload-pack=x origin | cat":              deliveryNote,
		"git -c core.sshCommand=x fetch origin; git push":     deliveryNote,
		"git fetch --upload-pack=x origin && git push origin": deliveryNote,
		"bash -c 'git fetch --upload-pack=x origin | cat'":    deliveryNote,
	} {
		if got := DeliveryNoteAt(h, in, []string{"/usr/bin/bash", "-c", line}, nil); got != want {
			t.Errorf("DeliveryNoteAt(%q) =\n %q\nwant\n %q", line, got, want)
		}
	}
}
