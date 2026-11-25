package sandbox

// T249 round-5 verify (studio-integration-v1.md L41), kept as regressions:
// the git folder walk's cap; container socket spellings (A1), docker
// contexts that never block (A2) and branch remotes judged as urls (A4).

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Past maxGitFolderEntries the line runs in the sandbox; at the cap the walk
// still judges every entry, and takes little time.
func TestTheGitFolderWalkNearItsCap(t *testing.T) {
	if testing.Short() {
		t.Skip("makes 200000 files")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(base, "p")
	git := filepath.Join(proj, ".git")
	for _, d := range []string{"refs/heads", "logs"} {
		if err := os.MkdirAll(filepath.Join(git, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// .git, refs, refs/heads, logs, objects, objects/<n> folders and files.
	have := 5
	for i := 0; have < maxGitFolderEntries; i++ {
		dir := filepath.Join(git, "objects", strconv.Itoa(i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		have++
		for j := 0; j < 1000 && have < maxGitFolderEntries; j++ {
			f, err := os.Create(filepath.Join(dir, strconv.Itoa(j)))
			if err != nil {
				t.Fatal(err)
			}
			f.Close()
			have++
		}
	}
	in := Input{Home: filepath.Join(base, "home"), Roots: []string{proj}, CommonDir: git, Env: map[string]string{}, Stat: Disk, Lstat: DiskLstat}
	start := time.Now()
	if why := in.GitFolderProblem(git, git); why != "" {
		t.Errorf("at the cap (%d entries): %q", have, why)
	}
	t.Logf("walk of %d entries: %s", have, time.Since(start))
	// One more entry, a link sorted after objects/: refused either way.
	if err := os.Symlink(filepath.Join(base, "home", ".bashrc"), filepath.Join(git, "refs", "heads", "main")); err != nil {
		t.Fatal(err)
	}
	if why := in.GitFolderProblem(git, git); why == "" || !(strings.Contains(why, "more than") || strings.Contains(why, "is a link")) {
		t.Errorf("past the cap with a link: %q", why)
	}
}

func TestMoreContainerSocketSpellingsAreHidden(t *testing.T) {
	d := with(machine, disk{"/srv": "dir", "/srv/a": "dir", "/srv/a/docker.sock": "file"})
	for _, c := range []struct{ variable, value string }{
		{"DOCKER_HOST", "Unix:///srv/a/docker.sock"},
		{"CONTAINER_HOST", "unix:///srv/a/docker.sock#frag"},
		{"CONTAINER_HOST", "unix://localhost/srv/a/docker.sock"},
		{"CONTAINER_HOST", "uNiX:/srv/a/docker.sock?x=1"},
		{"DOCKER_HOST", "unix:///srv/a/../a/docker.sock"},
	} {
		in := sample(d)
		in.Env[c.variable] = c.value
		if !hiddenIn(Args(in), "/srv/a/docker.sock") {
			t.Errorf("%s=%s leaves /srv/a/docker.sock reachable", c.variable, c.value)
		}
	}
	// Not a socket path: nothing more hidden, nothing fails.
	for _, h := range []string{"unix:", "unix://", "unix:relative.sock", "UNIX", "tcp://unix:/srv/a"} {
		in := sample(d)
		in.Env["DOCKER_HOST"] = h
		if hiddenIn(Args(in), "/srv/a/docker.sock") {
			t.Errorf("DOCKER_HOST=%q hides /srv/a/docker.sock", h)
		}
	}
}

func TestDockerContextsNeverBlockOnAnyShape(t *testing.T) {
	home := t.TempDir()
	meta := filepath.Join(home, ".docker", "contexts", "meta")
	good := filepath.Join(meta, "zz", "meta.json")
	os.MkdirAll(filepath.Dir(good), 0o755)
	os.WriteFile(good, []byte(`{"Endpoints":{"docker":{"Host":"unix:///srv/ok.sock"}}}`), 0o644)
	fifo := filepath.Join(home, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip(err)
	}
	os.MkdirAll(filepath.Join(meta, "aa"), 0o755)
	os.Symlink(fifo, filepath.Join(meta, "aa", "meta.json"))   // a link to a FIFO
	os.MkdirAll(filepath.Join(meta, "bb", "meta.json"), 0o755) // a folder
	dc := filepath.Join(home, "dc")
	os.MkdirAll(filepath.Join(dc, "contexts", "meta", "cc"), 0o755)
	syscall.Mkfifo(filepath.Join(dc, "contexts", "meta", "cc", "meta.json"), 0o644) // under DOCKER_CONFIG
	done := make(chan []string, 1)
	go func() { done <- DockerContexts(home, map[string]string{"DOCKER_CONFIG": dc}) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		for _, f := range []string{fifo, filepath.Join(dc, "contexts", "meta", "cc", "meta.json")} {
			if w, err := os.OpenFile(f, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
				w.Close()
			}
		}
		t.Fatal("DockerContexts blocks")
	}
	got := strings.Join(DockerContexts(home, map[string]string{}), " ")
	if !strings.Contains(got, "/srv/ok.sock") {
		t.Errorf("the regular context is lost: %q", got)
	}
}

func TestBranchRemotesAreJudgedAsURLs(t *testing.T) {
	for _, key := range []string{"branch.main.remote", "branch.main.pushremote", "branch.feat/x.remote"} {
		for _, v := range []string{"ext::sh -c x", "EXT::sh -c x", "fd::3", "x::y", "-oProxyCommand=x", "--upload-pack=x"} {
			if safeRepositoryKey(key, v) {
				t.Errorf("%s=%q is on the allowlist", key, v)
			}
		}
		for _, v := range []string{"origin", "upstream", "https://example.invalid/r.git", "git@github.com:o/r.git", "ssh://h/r"} {
			if !safeRepositoryKey(key, v) {
				t.Errorf("%s=%q is refused", key, v)
			}
		}
	}
}

// L41: a FIFO, a socket or a device in the git folders keeps the line in
// the sandbox: git would wait on it or write to it.
func TestAGitFolderEntryThatIsNoFileOrFolderIsAProblem(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := filepath.Join(base, "p", ".git")
	if err := os.MkdirAll(filepath.Join(git, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	in := Input{Home: filepath.Join(base, "home"), Roots: []string{filepath.Join(base, "p")}, CommonDir: git, Env: map[string]string{}, Stat: Disk, Lstat: DiskLstat}
	fifo := filepath.Join(git, "FETCH_HEAD")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("no FIFO: %v", err)
	}
	if why := in.GitFolderProblem(git, git); why != fifo+" in the repository's git folder is not a folder or a regular file" {
		t.Errorf("a FIFO FETCH_HEAD: %q", why)
	}
}
