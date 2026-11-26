package cli

import "testing"

// L1729: the fix of a line repositoryGitProblem kept inside says a
// sandboxed command may have put the problem there only where one could
// have: not for a worktree's or submodule's git folder elsewhere, a
// submodule's core.worktree, or a walk past its limit.
func TestT251XRepositoryGitFixFitsTheReason(t *testing.T) {
	planted := "remove that from the repository (a sandboxed command may have put it there), or run the line yourself in a terminal"
	for why, want := range map[string]string{
		"/r/.git/FETCH_HEAD in the repository's git folder is a link, which git would write through":                                           planted,
		"the repository's configuration sets http.cookiefile, which is not among the keys a git line runs outside the sandbox with":            planted,
		"the repository's git folders hold more than 200000 entries":                                                                           "run the line yourself in a terminal",
		"the repository's git folder /r/main/.git/worktrees/w is not in the repository /r/w":                                                   "if it is a worktree or a submodule of a repository elsewhere, run the line in that repository's own session; otherwise remove the .git file that points there (a sandboxed command may have put it there); or run the line yourself in a terminal",
		"the repository's git folder /p/.git/modules/s is not in the project":                                                                  "if it is a worktree or a submodule of a repository elsewhere, run the line in that repository's own session; otherwise remove the .git file that points there (a sandboxed command may have put it there); or run the line yourself in a terminal",
		"the repository's configuration sets core.worktree (as a submodule's does), which may put the files git writes outside the repository": "run the line in the repository that holds it (a submodule's superproject), or yourself in a terminal",
		"git cannot list the configuration in /r (exit status 128)":                                                                            "run the line yourself in a terminal",
	} {
		if got := repositoryGitFix(why); got != want {
			t.Errorf("repositoryGitFix(%q) = %q, want %q", why, got, want)
		}
	}
}
