// With the default settings every shell write outside the project that the
// guards no longer judge (T128f slices 4 and 5) fails in the sandbox, and
// with sandbox.mode off on Linux `jhon doctor` warns that those writes run
// unguarded (T128f slice 7, L1367 Q2/Q3 and gap 2, L1371).
package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestEveryOutsideWriteFailsWithTheDefaultSettings(t *testing.T) {
	b := newBox(t, "JHON_SETTINGS=")
	other := filepath.Join(b.home, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	support.GitRepo(t, other)
	support.Write(t, b.home, map[string]string{
		"plain/a.txt":                       "a\n",
		".claude/CLAUDE.md":                 "# owner\n",
		".config/jhon/settings.toml":        "",
		".local/share/jhon/current/VERSION": "0.3.7\n",
	})
	plain := filepath.Join(b.home, "plain")
	if err := os.Chmod(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, script, file string }{
		{"a redirect into the home", `echo x > "$HOME/x"`, filepath.Join(b.home, "x")},
		{"dd into the home", `dd if=/dev/zero of="$HOME/x" bs=1 count=1`, filepath.Join(b.home, "x")},
		{"touch in another repository", `touch "` + other + `/x"`, filepath.Join(other, "x")},
		{"a redirect into /etc", `echo x > /etc/jhon-probe`, "/etc/jhon-probe"},
		{"a redirect into /var/tmp", `echo x > /var/tmp/x`, ""},
		{"a recursive chmod in the home", `chmod -R 700 "$HOME/plain"`, ""},
		{"the installed release", `echo x > "$HOME/.local/share/jhon/current/x"`, filepath.Join(b.home, ".local", "share", "jhon", "current", "x")},
		{"the owner's CLAUDE.md", `echo x >> "$HOME/.claude/CLAUDE.md"`, ""},
		{"Jhon's settings folder", `echo x > "$HOME/.config/jhon/x"`, filepath.Join(b.home, ".config", "jhon", "x")},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--project", b.proj, "--", "sh", "-c", c.script)
			if r.Code == 0 || r.Stdout != "" || !strings.Contains(r.Stderr, "Read-only file system") || !strings.Contains(r.Stderr, "the sandbox refused a write to ") {
				t.Fatalf("exit %d, stdout %q, stderr %q; want a read-only failure with the note", r.Code, r.Stdout, r.Stderr)
			}
			if c.file != "" && exists(c.file) {
				t.Errorf("%s was written", c.file)
			}
		})
	}
	if fi, err := os.Stat(plain); err != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("the recursive chmod changed %s: %v %v", plain, fi.Mode(), err)
	}
	if got := support.Read(t, filepath.Join(b.home, ".claude", "CLAUDE.md")); got != "# owner\n" {
		t.Errorf("CLAUDE.md now holds %q", got)
	}
	// The project stays writable.
	r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--project", b.proj, "--", "sh", "-c", `echo x > "$1"`, "sh", filepath.Join(b.proj, "x"))
	if r.Code != 0 || r.Stderr != "" || support.Read(t, filepath.Join(b.proj, "x")) != "x\n" {
		t.Errorf("a write in the project: exit %d, stderr %q; want it written", r.Code, r.Stderr)
	}
}

// gap 2 of L1367: sandbox.mode off on Linux, where the sandbox can run,
// leaves the shell's writes outside the project unguarded (the guards no
// longer judge them): doctor warns, never ok, and the verdict stays the one
// without the warning's problem count.
func TestDoctorWarnsThatModeOffLeavesShellWritesUnguarded(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipped: the sandbox runs on Linux only")
	}
	b := newBox(t)
	support.Write(t, b.home, map[string]string{"bin/claude": "#!/bin/sh\nexit 1\n"})
	if err := os.Chmod(filepath.Join(b.home, "bin", "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(append([]string{}, b.env...), "PATH="+filepath.Join(b.home, "bin")+":"+os.Getenv("PATH"))
	b.settings(t, "[sandbox]\nmode = \"off\"\n")
	r := support.Jhon(t, b.proj, env, "doctor")
	row := "\nsandbox\n  warn  sandbox off; it can run here; with it off the model's shell commands run unguarded outside the project: a write there, Jhon's installed files and source included, is not stopped; fix: jhon settings set sandbox.mode on\n"
	if !strings.Contains(r.Stdout, row) || strings.Contains(r.Stdout, "  ok    sandbox off") {
		t.Errorf("doctor with sandbox.mode off: exit %d\n%s", r.Code, r.Out())
	}
	// Preserve: with the default (on) doctor says ok.
	b.settings(t, "")
	if on := support.Jhon(t, b.proj, env, "doctor"); !strings.Contains(on.Stdout, "\nsandbox\n  ok    sandbox on; it can run here\n") {
		t.Errorf("doctor with the default mode:\n%s", on.Out())
	}
}
