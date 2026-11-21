// T181 (L1500-L1503): delivery from a sandboxed session. Inside bubblewrap's
// user namespace the files root owns show as owned by nobody, so ssh
// refuses a system drop-in its configuration includes, and the work
// profile hides ~/.ssh, ~/.config/gh and the agent socket: git over SSH and
// gh run with no sandbox, on a line of their own, which may open with `cd
// <folder> &&` into the project. A failing work line that holds one of them
// ends with a note that says so.
package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// deliveryNote is the note a failing work line with git over SSH or gh
// ends with.
const deliveryNote = "git over SSH and gh run outside the sandbox only on a line of their own, with no pipes, $(…) or other commands: run it alone, as in git -C <dir> fetch origin main\n"

// The cause, with the real ssh: a configuration that includes a drop-in
// root owns reads outside the sandbox, and inside it ssh stops on the
// drop-in's owner (readconf.c checks an included file's owner).
func TestSSHRefusesARootOwnedIncludeInsideTheSandbox(t *testing.T) {
	b := newBox(t)
	ssh := realShell(t, "ssh")
	include := ""
	confs, _ := filepath.Glob("/etc/ssh/ssh_config.d/*.conf")
	for _, c := range confs {
		fi, err := os.Stat(c)
		if st, ok := fi.Sys().(*syscall.Stat_t); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o022 == 0 && ok && st.Uid == 0 {
			include = c
			break
		}
	}
	if include == "" {
		t.Skip("skipped: no root-owned /etc/ssh/ssh_config.d/*.conf on this machine")
	}
	cfg := filepath.Join(b.proj, "ssh_config")
	support.Write(t, b.proj, map[string]string{"ssh_config": "Include " + include + "\n"})
	outside := support.Run(t, support.Cmd{Args: []string{ssh, "-G", "-F", cfg, "localhost"}, Dir: b.proj, Env: b.env})
	if outside.Code != 0 || strings.Contains(outside.Stderr, "Bad owner") {
		t.Fatalf("outside the sandbox: exit %d, stderr %q; want the configuration read", outside.Code, outside.Stderr)
	}
	r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--profile", "work", "--", ssh, "-G", "-F", cfg, "localhost")
	if r.Code == 0 || !strings.Contains(r.Stderr, "Bad owner or permissions on "+include) {
		t.Errorf("inside the work profile: exit %d, stderr %q; want ssh to refuse %s for its owner", r.Code, r.Stderr, include)
	}
}

// A line that opens with a cd into the project fetches over SSH with no
// sandbox: the stub ssh, on a PATH folder no sandboxed command may write,
// serves the remote with git-upload-pack and writes its note outside
// every writable folder, which it can do only unsandboxed.
func TestALeadingCdIntoTheProjectFetchesOverSSHOutsideTheSandbox(t *testing.T) {
	b := newBox(t)
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("skipped: no /bin/sh")
	}
	ran := filepath.Join(b.base, "ssh-ran")
	originOverSSH(t, &b, ran)
	sub := filepath.Join(b.proj, "sub")
	mkdirs(t, sub)
	r := support.Jhon(t, sub, b.env, "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", "/bin/sh", "-c", "cd "+b.proj+" && git fetch origin")
	if r.Code != 0 || !exists(ran) {
		t.Fatalf("exit %d, stderr %q, ssh ran unsandboxed %v; want the fetch run with no sandbox", r.Code, r.Stderr, exists(ran))
	}
	if got := strings.TrimSpace(support.Git(t, b.proj, "rev-parse", "refs/remotes/origin/main")); got != strings.TrimSpace(support.Git(t, b.proj, "rev-parse", "HEAD")) {
		t.Errorf("origin/main is %q after the fetch; want HEAD", got)
	}
	// A cd out of the project leaves the line in the work profile.
	os.Remove(ran)
	if r := support.Jhon(t, sub, b.env, "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", "/bin/sh", "-c", "cd "+b.home+" && git -C "+b.proj+" fetch origin"); exists(ran) {
		t.Errorf("a cd out of the project: exit %d, stderr %q; want it run with the work profile", r.Code, r.Stderr)
	}
}

// A failing work line that holds git over SSH or gh ends with the note, once,
// on stderr; a failing line with neither, and a line that does not fail,
// get none.
func TestAFailingWorkLineWithADeliveryProgramEndsWithTheNote(t *testing.T) {
	b := newBox(t)
	bash := realShell(t, "bash")
	run := func(line string) support.Result {
		return support.Jhon(t, b.proj, b.env, "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", bash, "-c", line)
	}
	r := run("set -o pipefail; git fetch origin | cat")
	if r.Code == 0 || !strings.HasSuffix(r.Stderr, "\n"+deliveryNote) || strings.Count(r.Stderr, deliveryNote) != 1 {
		t.Errorf("git fetch origin | cat: exit %d, stderr %q; want a failure ending with the note once", r.Code, r.Stderr)
	}
	if r := run("false"); r.Code != 1 || r.Stderr != "" {
		t.Errorf("false: exit %d, stderr %q; want 1 and no note", r.Code, r.Stderr)
	}
	if r := run("git fetch origin 2>/dev/null | cat"); r.Code != 0 || strings.Contains(r.Stderr, deliveryNote) {
		t.Errorf("a line that does not fail: exit %d, stderr %q; want 0 and no note", r.Code, r.Stderr)
	}
}
