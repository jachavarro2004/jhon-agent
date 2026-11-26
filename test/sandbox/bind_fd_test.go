package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T248 (odd/tasks/phase1-security.md S3): `jhon sandbox run` checks each
// writable area (no link on its way, not nested, not the home), and
// bubblewrap mounts it later. A sandboxed run of another project that can
// write a folder on the way could swap it for a link in between, and a bind
// by path would open the home folder the link leads to for writing. The
// run opens each bind with no link followed and hands bubblewrap the
// descriptor (--bind-fd, --ro-bind-fd).

// withSwappingBwrap is the box's environment with a bwrap first on PATH, in
// a folder no sandboxed command may write, that swaps folder for a link to
// target and then starts the real bubblewrap with the arguments it got:
// the swap lands after every check of jhon's and before bubblewrap mounts
// anything, as a concurrent run's would.
func withSwappingBwrap(t *testing.T, b box, folder, target string) []string {
	t.Helper()
	real, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("skipped: no bubblewrap on this machine")
	}
	bin := filepath.Join(b.base, "swap-bin")
	script(t, filepath.Join(bin, "bwrap"), "#!/bin/sh\nmv '"+folder+"' '"+folder+".moved' && ln -s '"+target+"' '"+folder+"' || exit 99\nexec '"+real+"' \"$@\"\n")
	return append(slices.Clone(b.env), "PATH="+bin+":"+os.Getenv("PATH"))
}

// The project, <root>-worktrees, $TMPDIR and a sandbox.allow_write folder,
// all in area/, are checked; then area/ becomes a link to the home, where a
// folder of each name lies. bubblewrap mounts the checked folders where the
// link leads, so jhon sandbox enter refuses the run before the program
// starts: nothing is written, in the home or in the folders checked.
func TestAFolderSwappedForALinkBeforeBubblewrapMountsItIsNotWrittenThrough(t *testing.T) {
	b := newBox(t)
	area := filepath.Join(b.home, "area")
	proj := filepath.Join(area, "project")
	mkdirs(t, proj, proj+"-worktrees", filepath.Join(area, "tmp"), filepath.Join(area, "shared"),
		filepath.Join(b.home, "project-worktrees"), filepath.Join(b.home, "shared"))
	support.GitRepo(t, proj)
	b.settings(t, "[sandbox]\nallow_write = [\""+filepath.Join(area, "shared")+"\"]\n")
	env := append(withSwappingBwrap(t, b, area, b.home), "TMPDIR="+filepath.Join(area, "tmp"))
	names := []string{"project", "project-worktrees", "tmp", "shared"}
	var folders []string
	for _, n := range names {
		folders = append(folders, filepath.Join(area, n))
	}
	r := support.Jhon(t, proj, env, append([]string{"sandbox", "run", "--", "sh", "-c", `for d in "$@"; do echo x > "$d/planted" || exit 1; done; echo wrote`, "sh"}, folders...)...)
	for _, n := range names {
		if exists(filepath.Join(b.home, n, "planted")) {
			t.Errorf("the write to area/%s went through the link into the home folder %s", n, filepath.Join(b.home, n))
		}
	}
	if r.Code != 126 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, "jhon sandbox: "+area+"/") || !strings.HasSuffix(r.Stderr, enterRefused) {
		t.Errorf("exit %d, stdout %q, stderr %q; want 126 and the note", r.Code, r.Stdout, r.Stderr)
	}
	for _, n := range names {
		if exists(filepath.Join(area+".moved", n, "planted")) {
			t.Errorf("the program ran: it wrote area/%s", n)
		}
	}
}

// The descriptors reach bubblewrap alone, which closes them: the program
// holds only its standard three.
func TestTheProgramHoldsNoDescriptorOfTheBinds(t *testing.T) {
	b := newBox(t)
	r := b.sh(t, b.proj, "", `ls /proc/$$/fd; :`)
	if r.Code != 0 || r.Stdout != "0\n1\n2\n" || r.Stderr != "" {
		t.Errorf("exit %d, stdout %q, stderr %q; want descriptors 0, 1 and 2 only", r.Code, r.Stdout, r.Stderr)
	}
}

// A bubblewrap that does not know --bind-fd cannot run the sandbox: status
// and doctor say so with the fix, as the probe starts its sandbox with the
// options a run uses, and run fails in bubblewrap's own words with nothing
// run.
func TestABubblewrapWithoutBindFdCannotRun(t *testing.T) {
	b := newBox(t)
	b.settings(t, "[sandbox]\nmode = \"off\"\n")
	bin := filepath.Join(b.base, "no-bind-fd-bin")
	script(t, filepath.Join(bin, "bwrap"), "#!/bin/sh\nfor a in \"$@\"; do\n  [ \"$a\" = -- ] && break\n  case \"$a\" in --bind-fd|--ro-bind-fd) echo \"bwrap: Unknown option $a\" >&2; exit 1;; esac\ndone\nexit 0\n")
	env := append(slices.Clone(b.env), "PATH="+bin+":"+os.Getenv("PATH"))
	reason := "bubblewrap is too old for the sandbox (bwrap: Unknown option --bind-fd)"
	fix := "update bubblewrap to a release that has --bind-fd and --ro-bind-fd (pacman -S bubblewrap, apt install bubblewrap or dnf install bubblewrap)"
	if got := statusOf(t, b, env); got != (status{Mode: "off", Reason: reason, Fix: fix, Project: b.proj}) {
		t.Errorf("status --json: %+v; want unable, %q and %q", got, reason, fix)
	}
	if r := support.Jhon(t, b.proj, env, "doctor"); !strings.Contains(r.Stdout, "\nsandbox\n  warn  sandbox off; it cannot run here: "+reason+"; fix: "+fix+"; the model's commands run unsandboxed\n") {
		t.Errorf("doctor:\n%s", r.Out())
	}
	marker := filepath.Join(b.proj, "ran.txt")
	if r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo ran > "$1"`, "sh", marker); r.Code == 0 || !strings.Contains(r.Stderr, "Unknown option --bind-fd") || exists(marker) {
		t.Errorf("run: exit %d, stderr %q; want bubblewrap's refusal and nothing run", r.Code, r.Stderr)
	}
}
