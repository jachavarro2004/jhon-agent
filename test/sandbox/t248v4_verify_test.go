package sandbox

// T248 fourth verify (odd/tasks/phase1-security.md S3, after 4e08d49): the
// race note decided by enter's start byte.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const t248v4Race = "bwrap: Race condition binding dirfd: File exists\n"
const t248v4Note = "jhon sandbox: bubblewrap refused the sandbox because a folder changed while it mounted it, so nothing was run; fix: run the command again\n"

// t248v4Load starts CPU and IO load for the test's length when
// T248V4_LOAD is set: one busy loop per CPU and one writer per two CPUs.
func t248v4Load(t *testing.T, dir string) {
	t.Helper()
	if os.Getenv("T248V4_LOAD") == "" {
		return
	}
	var procs []*exec.Cmd
	for i := 0; i < runtime.NumCPU(); i++ {
		procs = append(procs, exec.Command("sh", "-c", "while :; do :; done"))
	}
	for i := 0; i < max(1, runtime.NumCPU()/2); i++ {
		f := filepath.Join(dir, fmt.Sprintf("io-%d", i))
		procs = append(procs, exec.Command("sh", "-c", `while :; do dd if=/dev/zero of="$1" bs=1M count=256 conv=fsync status=none; done`, "sh", f))
	}
	for _, p := range procs {
		if err := p.Start(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, p := range procs {
			p.Process.Kill()
			p.Wait()
		}
	})
}

// bubblewrap's real race refusal (a run of the enclosing project swaps
// work/sub with a link to the home while runs of work/sub/victim start)
// gets the note right after bubblewrap's line, and no run whose program ran
// (it wrote $TMPDIR/m-<n>) gets it; under load when T248V4_LOAD is set.
func TestT248V4TheRaceNoteFollowsOnlyARealRefusal(t *testing.T) {
	b := newBox(t)
	t248v4Load(t, b.base)
	work := filepath.Join(b.home, "work")
	victim := filepath.Join(work, "sub", "victim")
	mkdirs(t, victim, filepath.Join(b.home, "victim"))
	support.GitRepo(t, work)
	support.GitRepo(t, victim)
	support.Write(t, work, map[string]string{"flip.py": t248v2Flip})
	done := make(chan support.Result, 1)
	go func() {
		done <- support.Jhon(t, work, b.env, "sandbox", "run", "--", "python3", filepath.Join(work, "flip.py"), work, t248v2Seconds())
	}()
	time.Sleep(time.Second)
	runs, ran, noted, raceLines := 0, 0, 0, 0
loop:
	for {
		select {
		case <-done:
			break loop
		default:
		}
		runs++
		marker := filepath.Join(b.tmp, fmt.Sprintf("m-%d", runs))
		// The program itself prints bubblewrap's line and exits 1 once it ran.
		r := support.Jhon(t, victim, b.env, "sandbox", "run", "--", "sh", "-c", `echo x > "$1"; echo 'bwrap: Race condition binding dirfd: File exists' >&2; exit 1`, "sh", marker)
		didRun := exists(marker)
		if didRun {
			ran++
			if strings.Contains(r.Stderr, "nothing was run") {
				t.Errorf("run %d ran (it wrote %s) but got the note: %q", runs, marker, r.Stderr)
			}
			continue
		}
		if strings.HasPrefix(r.Stderr, "bwrap: Race condition binding dirfd") {
			raceLines++
			if r.Stderr == t248v4Race+t248v4Note && r.Code == 1 {
				noted++
			} else {
				t.Errorf("run %d: bubblewrap refused, but exit %d, stderr %q; want its line and the note", runs, r.Code, r.Stderr)
			}
		}
	}
	if raceLines == 0 || ran == 0 {
		t.Errorf("the race gave %d refusals by bubblewrap and %d runs that ran, of %d; want both", raceLines, ran, runs)
	}
	t.Logf("%d runs: %d ran (no note), %d refused by bubblewrap (%d noted)", runs, ran, raceLines, noted)
}

// A program cannot reach the start pipe: it holds 0, 1 and 2, bubblewrap's
// pid 1 holds no pipe but those, a program that runs jhon sandbox enter
// --started N with every N, reads every descriptor it can find, and then
// prints bubblewrap's race line and exits 1, gets no note.
func TestT248V4AProgramCannotForgeOrEatTheStartByte(t *testing.T) {
	b := newBox(t)
	marker := filepath.Join(b.proj, "ran")
	// The listing is taken before the shell opens anything of its own.
	prog := fmt.Sprintf(`ls -l /proc/1/fd /proc/$$/fd | sed -n 's#.* \([0-9][0-9]*\) -> \(.*\)#FD \1 -> \2#p' | grep -v "^FD [012] "
for p in /proc/1/fd/*; do timeout 0.2 head -c1 "$p" >/dev/null 2>&1 < /dev/null; done
for n in $(seq 3 64); do %q sandbox enter --started $n -- true 2>/dev/null; done
echo x > %q; echo 'bwrap: Race condition binding dirfd: File exists' >&2; exit 1`, support.Bin(t), marker)
	r := b.sh(t, b.proj, "", prog)
	if !exists(marker) {
		t.Fatalf("the program did not run: exit %d, %q", r.Code, r.Stderr)
	}
	for _, line := range strings.Split(r.Stdout, "\n") {
		if strings.HasPrefix(line, "FD ") && !strings.Contains(line, "anon_inode:[eventfd]") {
			t.Errorf("a descriptor above 2: %s", line)
		}
	}
	if strings.Contains(r.Stderr, "nothing was run") || r.Code != 1 {
		t.Errorf("exit %d, stderr %q; want exit 1 and no note", r.Code, r.Stderr)
	}
}
