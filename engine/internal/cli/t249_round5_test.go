package cli

// T249 round-5 verify (studio-integration-v1.md L41), kept as regressions:
// the git folders repositoryGitProblem judges are the ones git uses.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
)

// git keeps white space at the end of .git/commondir's path (it strips only
// the newline), and rev-parse prints it: a sandboxed command that names
// "<project>/c " as the common dir, with a decoy "<project>/c" beside it,
// must not have the decoy walked in its place.
func TestRepositoryGitProblemWalksTheCommonDirGitPrints(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	for name, tail := range map[string]string{"space": " ", "tab": "\t", "no-break space": "\u00a0"} {
		t.Run(name, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			for _, k := range []string{"GIT_DIR", "GIT_COMMON_DIR", "GIT_WORK_TREE", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT"} {
				t.Setenv(k, "")
				os.Unsetenv(k)
			}
			proj := filepath.Join(home, "project")
			run := func(args ...string) {
				t.Helper()
				c := exec.Command(args[0], args[1:]...)
				c.Dir = proj
				if out, err := c.CombinedOutput(); err != nil {
					t.Fatalf("%v: %v %s", args, err, out)
				}
			}
			if err := os.MkdirAll(proj, 0o755); err != nil {
				t.Fatal(err)
			}
			run("git", "init", "-q", "-b", "main", ".")
			run("git", "-c", "user.name=a", "-c", "user.email=a@example.invalid", "commit", "-q", "--allow-empty", "-m", "i")
			common := filepath.Join(proj, "c"+tail)
			run("cp", "-a", filepath.Join(proj, ".git"), common)
			for _, d := range []string{"c/objects", "c/refs", "c" + tail + "/logs/refs/remotes/origin"} {
				if err := os.MkdirAll(filepath.Join(proj, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(proj, ".git", "commondir"), []byte(common+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(common, "logs", "refs", "remotes", "origin", "main")
			if err := os.Symlink(filepath.Join(home, ".bashrc"), link); err != nil {
				t.Fatal(err)
			}
			// git itself uses the folder with the trailing white space.
			out, err := exec.Command("git", "-C", proj, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
			if err != nil || string(out) != common+"\n" {
				t.Fatalf("git names the common dir %q (%v); want %q", out, err, common+"\n")
			}
			in := sandbox.Current(proj, proj)
			if why := repositoryGitProblem(in, "git fetch origin", nil, ""); why == nil {
				t.Errorf("an outside fetch may run although %s, in the common dir git uses (%q), is a link out of the project", link, common)
			}
		})
	}
}

// L41: the git folder names rev-parse prints are taken as git uses them:
// only the final newline dropped, two lines, absolute, no white space
// around a name.
func TestGitFolderNamesAreTakenAsGitPrintsThem(t *testing.T) {
	if names, why := gitFolderNames("/p/.git\n/p/.git\n"); why != "" || len(names) != 2 || names[0] != "/p/.git" || names[1] != "/p/.git" {
		t.Errorf("a plain answer: %q, %q", names, why)
	}
	for _, out := range []string{
		"/p/.git\n/p/c \n", "/p/.git\n/p/c\t\n", "/p/.git\n/p/c\u00a0\n", "/p/.git\n /p/c\n", "/p/.git \n/p/.git\n",
		"/p/.git\n/p/.git", "/p/.git\n", "/p/.git\n/p/.git\n/p/x\n", "/p/.git\n\n", "/p/.git\nrel\n", "", "\n",
		"/p/.git\n/p/.git\n\n",
	} {
		if names, why := gitFolderNames(out); why == "" {
			t.Errorf("%q gives %q with no problem", out, names)
		}
	}
}
