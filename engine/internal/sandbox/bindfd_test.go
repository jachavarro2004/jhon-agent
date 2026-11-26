//go:build linux

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// T248 (odd/tasks/phase1-security.md S3): the sandbox checks a writable
// folder (no link on its way, not nested, not the home), and bubblewrap
// mounts it later. A concurrent sandboxed run that can write a folder on
// the way may swap the checked folder for a link in between; a path-based
// --bind would then follow the link and open a home folder for writing.
// Each bind is opened instead, with no link followed, checked on its
// descriptor, and handed to bubblewrap as --bind-fd / --ro-bind-fd.

// swapBox is a home under the test's scratch with a sandbox.allow_write
// folder, area/shared, and a home folder of the same name the sandbox keeps
// read-only, shared: a link put in place of area leads area/shared there.
func swapBox(t *testing.T, base string) (in Input, shared, victim string) {
	t.Helper()
	home := filepath.Join(base, "home", "u")
	shared, victim = filepath.Join(home, "area", "shared"), filepath.Join(home, "shared")
	for _, d := range []string{shared, victim} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	in = Input{Cwd: home, Home: home, AllowWrite: []string{shared}, Env: map[string]string{}, Stat: remembered(Disk), Lstat: rememberedLstat(DiskLstat)}
	return in, shared, victim
}

// rememberedLstat is lstat with each path's answer kept, as remembered keeps
// Stat's: what Jhon checked stays what it saw, whatever changes after.
func rememberedLstat(lstat func(string) (string, bool, bool)) func(string) (string, bool, bool) {
	type answer struct {
		link       string
		isLink, ok bool
	}
	seen := map[string]answer{}
	return func(path string) (string, bool, bool) {
		a, ok := seen[path]
		if !ok {
			a.link, a.isLink, a.ok = lstat(path)
			seen[path] = a
		}
		return a.link, a.isLink, a.ok
	}
}

// swap replaces folder with a link to target, the folder kept beside it.
func swap(t *testing.T, folder, target string) string {
	t.Helper()
	moved := folder + ".moved"
	if err := os.Rename(folder, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, folder); err != nil {
		t.Fatal(err)
	}
	return moved
}

// The window itself, with a real bubblewrap: the folder is checked and
// opened, then the folder that holds it is swapped for a link to the home,
// where a folder of the same name lies, before bubblewrap mounts it (a
// link in the folder's own place bubblewrap refuses as a mount point). The
// program's write lands in the folder that was checked, never in the home
// folder the link leads to.
func TestAFolderSwappedForALinkBeforeTheMountIsWrittenWhereItWasChecked(t *testing.T) {
	bwrap, base := realSandbox(t)
	in, shared, victim := swapBox(t, base)
	if index(Args(in), "--bind", shared) < 0 {
		t.Fatalf("the allow_write folder is not bound writable: %v", Args(in))
	}
	b, p := Open(in)
	if p != nil {
		t.Fatalf("Open: %s; fix: %s", p.Reason, p.Fix)
	}
	defer b.Close()
	moved := swap(t, filepath.Dir(shared), in.Home)
	cmd := exec.Command(bwrap, append(slices.Clone(b.Args), "--", "sh", "-c", `echo x > "$1/planted"`, "sh", shared)...)
	cmd.ExtraFiles = b.ExtraFiles()
	cmd.WaitDelay = 10 * time.Second
	out, err := cmd.CombinedOutput()
	if _, statErr := os.Lstat(filepath.Join(victim, "planted")); statErr == nil {
		t.Fatalf("the write went through the link into the home folder %s (exit %v, %q)", victim, err, out)
	}
	if err != nil {
		t.Fatalf("the run failed: %v, %q", err, out)
	}
	if _, err := os.Stat(filepath.Join(moved, "shared", "planted")); err != nil {
		t.Errorf("the write did not land in the folder that was checked (%s): %v", filepath.Join(moved, "shared"), err)
	}
}

// Swapped before it is opened: the run is refused with why and the fix,
// whatever the change: the folder a link, a folder on its way a link, the
// folder a file, the folder gone.
func TestAFolderChangedBeforeItIsOpenedRefusesTheRun(t *testing.T) {
	_, base := realSandbox(t)
	for _, c := range []struct {
		name   string
		change func(t *testing.T, shared, home string)
		why    string
	}{
		{"the folder a link", func(t *testing.T, shared, home string) { swap(t, shared, filepath.Join(home, "shared")) }, "it is or passes through a link now"},
		{"a folder on its way a link", func(t *testing.T, shared, home string) { swap(t, filepath.Dir(shared), home) }, "it is or passes through a link now"},
		{"the folder a file", func(t *testing.T, shared, home string) {
			if err := os.Rename(shared, shared+".moved"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(shared, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}, "it is no longer a folder"},
		{"a folder on its way a file", func(t *testing.T, shared, home string) {
			area := filepath.Dir(shared)
			if err := os.Rename(area, area+".moved"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(area, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}, "a folder on its way is no longer a folder"},
		{"the folder gone", func(t *testing.T, shared, home string) {
			if err := os.Rename(shared, shared+".moved"); err != nil {
				t.Fatal(err)
			}
		}, "it is gone"},
	} {
		t.Run(c.name, func(t *testing.T) {
			in, shared, _ := swapBox(t, filepath.Join(base, strings.ReplaceAll(c.name, " ", "-")))
			if index(Args(in), "--bind", shared) < 0 {
				t.Fatalf("%s is not bound writable: %v", shared, Args(in))
			}
			c.change(t, shared, in.Home)
			b, p := Open(in)
			if p == nil {
				b.Close()
				t.Fatalf("Open gave %v; want a refusal", b.Args)
			}
			reason := "the folder " + shared + ", which the sandbox opens for writing, changed after Jhon checked it: " + c.why
			fix := "run the command again; if it is refused again, a command may be replacing " + shared + ": find it and stop it"
			if p.Reason != reason || p.Fix != fix {
				t.Errorf("Open: %q; fix %q\nwant %q; fix %q", p.Reason, p.Fix, reason, fix)
			}
		})
	}
}

// Every bind of Args, writable or read-only, is opened: the project's
// folders, <root>-worktrees, the git common dir, the cache, $TMPDIR, a
// sandbox.allow_write folder, the telemetry folder, a PATH folder and
// Jhon's settings inside a writable folder. Only the machine's root stays
// a path. Args itself stays readable, and each descriptor is the path that
// stands next to it.
func TestOpenBindsEveryAreaThroughADescriptor(t *testing.T) {
	_, base := realSandbox(t)
	home := filepath.Join(base, "home", "u")
	proj := filepath.Join(home, "project")
	folders := []string{
		proj, proj + "-worktrees", filepath.Join(base, "common"), filepath.Join(home, ".cache"), filepath.Join(home, "tmp"),
		filepath.Join(home, "shared"), filepath.Join(home, "shared", "bin"), filepath.Join(home, ".local", "share", "jhon", "telemetry"),
		filepath.Join(home, "shared", "jhon"),
	}
	for _, d := range folders {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	settingsFile := filepath.Join(home, "shared", "jhon", "settings.toml")
	if err := os.WriteFile(settingsFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	in := Input{
		Cwd: proj, Roots: []string{proj, proj + "-worktrees"}, CommonDir: filepath.Join(base, "common"), Home: home,
		Tmp: []string{filepath.Join(home, "tmp")}, Env: map[string]string{"TMPDIR": filepath.Join(home, "tmp")},
		Path: []string{filepath.Join(home, "shared", "bin")}, AllowWrite: []string{filepath.Join(home, "shared")},
		Protected: []string{settingsFile}, Telemetry: filepath.Join(home, ".local", "share", "jhon", "telemetry"),
		Stat: remembered(Disk), Lstat: DiskLstat,
	}
	readable := Args(in)
	b, p := Open(in)
	if p != nil {
		t.Fatalf("Open: %s; fix: %s", p.Reason, p.Fix)
	}
	defer b.Close()
	if !slices.Equal(Args(in), readable) {
		t.Errorf("Open changed Args:\n%v\nwas\n%v", Args(in), readable)
	}
	count := func(args []string, opt string) int {
		n := 0
		for i := 0; i < len(args); i++ {
			if args[i] == opt {
				n++
			}
		}
		return n
	}
	if n := count(readable, "--bind"); n < 6 || count(b.Args, "--bind-fd") != n {
		t.Errorf("%d writable binds, %d through a descriptor:\n%v", n, count(b.Args, "--bind-fd"), b.Args)
	}
	if n := count(readable, "--ro-bind") - 1; n < 2 || count(b.Args, "--ro-bind-fd") != n {
		t.Errorf("%d read-only binds besides /, %d through a descriptor:\n%v", n, count(b.Args, "--ro-bind-fd"), b.Args)
	}
	if count(b.Args, "--bind") != 0 || count(b.Args, "--ro-bind") != 1 || index(b.Args, "--ro-bind", "/") != 0 {
		t.Errorf("a bind is left by path:\n%v", b.Args)
	}
	for i, a := range b.Args {
		if a != "--bind-fd" && a != "--ro-bind-fd" {
			continue
		}
		fd, err := strconv.Atoi(b.Args[i+1])
		if err != nil {
			t.Fatalf("%s %q: %v", a, b.Args[i+1], err)
		}
		if at, err := os.Readlink("/proc/self/fd/" + b.Args[i+1]); err != nil || at != b.Args[i+2] {
			t.Errorf("%s %d %s: the descriptor is %q (%v)", a, fd, b.Args[i+2], at, err)
		}
		if !slices.ContainsFunc(b.ExtraFiles(), func(f *os.File) bool { return f != nil && int(f.Fd()) == fd }) {
			t.Errorf("descriptor %d is not among the files the run passes on", fd)
		}
		if extra := b.ExtraFiles(); fd-3 >= len(extra) || extra[fd-3] == nil || int(extra[fd-3].Fd()) != fd {
			t.Errorf("descriptor %d is not passed on at its own number", fd)
		}
	}
	if len(b.Files) != count(b.Args, "--bind-fd")+count(b.Args, "--ro-bind-fd") {
		t.Errorf("%d files for %d binds", len(b.Files), count(b.Args, "--bind-fd")+count(b.Args, "--ro-bind-fd"))
	}
}

// The descriptors reach bubblewrap, which closes them: the program inside
// has none of them, only its standard three.
func TestTheProgramInsideHoldsNoDescriptorOfTheBinds(t *testing.T) {
	bwrap, base := realSandbox(t)
	in, shared, _ := swapBox(t, base)
	b, p := Open(in)
	if p != nil {
		t.Fatalf("Open: %s; fix: %s", p.Reason, p.Fix)
	}
	defer b.Close()
	if len(b.Files) == 0 {
		t.Fatalf("Open opened no descriptor for %s: %v", shared, b.Args)
	}
	cmd := exec.Command(bwrap, append(slices.Clone(b.Args), "--", "sh", "-c", `ls /proc/$$/fd; :`)...)
	cmd.ExtraFiles = b.ExtraFiles()
	cmd.WaitDelay = 10 * time.Second
	out, err := cmd.Output()
	if err != nil || string(out) != "0\n1\n2\n" {
		t.Errorf("the program's descriptors: %v, %q; want 0, 1 and 2", err, out)
	}
}

// Without openat2 (Linux before 5.6), or where a seccomp filter refuses it,
// the sandbox cannot bind with no link followed: the run fails closed, as
// the Engram queue does (T227), and never falls back to a path.
func TestOpenFailsClosedWithoutOpenat2(t *testing.T) {
	_, base := realSandbox(t)
	in, _, _ := swapBox(t, base)
	was := openat2
	t.Cleanup(func() { openat2 = was })
	for _, c := range []struct {
		errno       syscall.Errno
		reason, fix string
	}{
		{syscall.ENOSYS, "this kernel has no openat2 (Linux 5.6 or newer), which the sandbox needs to bind its folders with no link followed", "a Linux kernel 5.6 or newer"},
		{syscall.EPERM, "openat2 is refused here (operation not permitted: a seccomp filter, as a container's, blocks it), which the sandbox needs to bind its folders with no link followed", "allow openat2 in the seccomp filter jhon runs under (a container's profile), or run jhon outside it"},
	} {
		openat2 = func(string) (int, error) { return -1, c.errno }
		b, p := Open(in)
		if p == nil {
			b.Close()
			t.Errorf("%v: Open gave %v; want a refusal", c.errno, b.Args)
			continue
		}
		if p.Reason != c.reason || p.Fix != c.fix {
			t.Errorf("%v: %q; fix %q\nwant %q; fix %q", c.errno, p.Reason, p.Fix, c.reason, c.fix)
		}
		// The probe says the same: status and doctor agree with run.
		if p := probe("/bin/true"); p == nil || p.Reason != c.reason || p.Fix != c.fix {
			t.Errorf("%v: the probe gave %+v; want %q", c.errno, p, c.reason)
		}
	}
}

// On a terminal jhon replaces itself with bubblewrap: the descriptors lose
// close-on-exec there, and only there.
func TestInheritKeepsTheDescriptorsAcrossExec(t *testing.T) {
	_, base := realSandbox(t)
	in, _, _ := swapBox(t, base)
	b, p := Open(in)
	if p != nil {
		t.Fatalf("Open: %s; fix: %s", p.Reason, p.Fix)
	}
	defer b.Close()
	if len(b.Files) == 0 {
		t.Fatal("Open opened no descriptor")
	}
	flags := func(f *os.File) int {
		v, _, e := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_GETFD, 0)
		if e != 0 {
			t.Fatal(e)
		}
		return int(v)
	}
	for _, f := range b.Files {
		if flags(f)&syscall.FD_CLOEXEC == 0 {
			t.Errorf("descriptor %d is opened without close-on-exec", f.Fd())
		}
	}
	if err := b.Inherit(); err != nil {
		t.Fatal(err)
	}
	for _, f := range b.Files {
		if flags(f)&syscall.FD_CLOEXEC != 0 {
			t.Errorf("descriptor %d keeps close-on-exec", f.Fd())
		}
	}
}
