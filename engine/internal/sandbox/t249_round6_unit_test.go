package sandbox

// T249 round-6 verify (L41, fbe8741), item 2: the non-file rule in
// GitFolderProblem, wherever the entry is.

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func round6Repo(t *testing.T) (Input, string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(base, "p")
	git := filepath.Join(proj, ".git")
	for _, d := range []string{"refs/heads", "logs", "objects/info", "objects/ab", "worktrees/w"} {
		if err := os.MkdirAll(filepath.Join(git, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(git, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := Input{Home: filepath.Join(base, "home"), Roots: []string{proj}, CommonDir: git, Env: map[string]string{}, Stat: Disk, Lstat: DiskLstat}
	return in, git, filepath.Join(git, "worktrees", "w")
}

func TestRound6NonFilesAnywhereInTheGitFolders(t *testing.T) {
	for name, make := range map[string]func(git, admin string) error{
		"FIFO under objects/": func(git, _ string) error { return syscall.Mkfifo(filepath.Join(git, "objects", "ab", "cd"), 0o644) },
		"FIFO as objects/info/alternates": func(git, _ string) error {
			return syscall.Mkfifo(filepath.Join(git, "objects", "info", "alternates"), 0o644)
		},
		"FIFO in the common dir only": func(git, _ string) error { return syscall.Mkfifo(filepath.Join(git, "packed-refs"), 0o644) },
		"FIFO in the admin folder":    func(_, admin string) error { return syscall.Mkfifo(filepath.Join(admin, "FETCH_HEAD"), 0o644) },
		"socket in refs/heads": func(git, _ string) error {
			// A short relative name: a socket path holds at most 108 bytes.
			wd, _ := os.Getwd()
			defer os.Chdir(wd)
			if err := os.Chdir(filepath.Join(git, "refs", "heads")); err != nil {
				return err
			}
			l, err := net.Listen("unix", "s")
			if err == nil {
				l.(*net.UnixListener).SetUnlinkOnClose(false)
				l.Close()
			}
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			in, git, admin := round6Repo(t)
			if err := make(git, admin); err != nil {
				t.Fatal(err)
			}
			if why := in.GitFolderProblem(admin, git); !strings.Contains(why, "is not a folder or a regular file") {
				t.Errorf("GitFolderProblem(admin, common) = %q", why)
			}
		})
	}
}

// A folder of the git folders that cannot be read keeps the line inside.
func TestRound6AnUnreadableFolderKeepsTheLineInside(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads every folder")
	}
	in, git, _ := round6Repo(t)
	d := filepath.Join(git, "refs", "heads")
	if err := os.Chmod(d, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(d, 0o755) })
	if why := in.GitFolderProblem(git, git); why == "" {
		t.Errorf("an unreadable %s passes", d)
	}
}
