// T57 slice 5 (L1314 S1): in a multi-repository workspace the sandbox's
// writable roots are the primary's and every joined member's, from the
// primary and from a member, read with no git process; a listed sibling that
// has not joined stays read-only.

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestAJoinedMemberIsWritableInTheSandbox(t *testing.T) {
	b := newBox(t)
	lib, web := filepath.Join(b.home, "lib"), filepath.Join(b.home, "web")
	for _, d := range []string{lib, web} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		support.GitRepo(t, d)
	}
	support.Write(t, b.proj, map[string]string{"ldd/config.yaml": "workspace:\n  lib: ../lib\n  web: ../web\n"})
	write := func(dir, target string) support.Result {
		t.Helper()
		return support.Jhon(t, dir, b.env, "sandbox", "run", "--", "sh", "-c", `echo x > "$1"`, "sh", filepath.Join(target, "w.txt"))
	}
	readOnly := func(dir, target, label string) {
		t.Helper()
		if r := write(dir, target); r.Code == 0 || exists(filepath.Join(target, "w.txt")) {
			t.Errorf("%s: a write to %s from %s: exit %d, %q; want a read-only failure and no file", label, target, dir, r.Code, r.Out())
		}
	}
	writable := func(dir, target, label string) {
		t.Helper()
		if r := write(dir, target); r.Code != 0 || r.Out() != "" || !exists(filepath.Join(target, "w.txt")) {
			t.Errorf("%s: a write to %s from %s: exit %d, %q; want it written", label, target, dir, r.Code, r.Out())
		}
		os.Remove(filepath.Join(target, "w.txt"))
	}
	readOnly(b.proj, lib, "lib before it joined")
	readOnly(lib, b.proj, "the primary from lib before it joined")

	for _, d := range []string{lib, web} {
		if r := support.Jhon(t, d, b.env, "workspace", "join", b.proj); r.Code != 0 {
			t.Fatalf("jhon workspace join in %s: exit %d, %q", d, r.Code, r.Out())
		}
	}
	support.Write(t, b.proj, map[string]string{"ldd/config.yaml": "workspace:\n  lib: ../lib\n  web: ../not-web\n"})
	writable(b.proj, lib, "a joined member from the primary")
	writable(lib, b.proj, "the primary from a joined member")
	// web joined, but the primary does not list it: fail closed, both ways.
	readOnly(b.proj, web, "a joined sibling the primary does not list")
	readOnly(web, b.proj, "the primary from a sibling it does not list")
	readOnly(lib, web, "an unlisted sibling from a member")

	// The workspace is read with no git process (L888).
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(b.tmp, "git.log")
	support.Write(t, b.base, map[string]string{"git-bin/git": "#!/bin/sh\necho \"$*\" >> '" + log + "'\nexec '" + real + "' \"$@\"\n"})
	if err := os.Chmod(filepath.Join(b.base, "git-bin", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(append([]string{}, b.env...), "PATH="+filepath.Join(b.base, "git-bin")+":"+os.Getenv("PATH"))
	for _, dir := range []string{b.proj, lib} {
		if r := support.Jhon(t, dir, env, "sandbox", "run", "--", "true"); r.Code != 0 || r.Out() != "" {
			t.Fatalf("in %s: exit %d, %q", dir, r.Code, r.Out())
		}
	}
	if exists(log) {
		t.Errorf("jhon sandbox run started git in a workspace:\n%s", support.Read(t, log))
	}
}
