package cli

// T249 round-6 verify (L41, fbe8741), item 1: gitFolderNames.

import "testing"

func TestRound6GitFolderNames(t *testing.T) {
	for _, out := range []string{
		"/p/.git\n/p/c \n", "/p/.git\n/p/c　\n", "/p/.git\n/p/c\u0085\n", "/p/.git\n/p/c\r\n",
		"/p/.git\n/p/c\v\n", "/p/.git\n/p/c\f\n", "/p/.git\r\n/p/.git\n", "/p/.git\n/p/.git\r\n",
		"/p/.git\n/p/.git\n\n\n", "/p/.git\n/p/\nx\n",
	} {
		if names, why := gitFolderNames(out); why == "" {
			t.Errorf("%q gives %q with no problem", out, names)
		}
	}
	for _, out := range []string{"/p/c x/.git\n/p/c x\n", "/p/ c\n/p/ c\n"} {
		if names, why := gitFolderNames(out); why != "" || len(names) != 2 {
			t.Errorf("%q: %q, %q (white space inside a name is the folder git uses)", out, names, why)
		}
	}
}
