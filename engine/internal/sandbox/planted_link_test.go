package sandbox

import (
	"strings"
	"testing"
)

// L26 blocker (odd/tasks/studio-integration-v1.md): a write area whose path
// as written lies in another writable area (an allow_write entry in the
// project or /tmp, a TMPDIR or an env-named cache under /tmp) can be
// replaced by a sandboxed command with a link; the next run must not bind
// where the link leads. An owner's link in a folder no command can write
// (~/data -> /mnt/data) is still bound where it leads.
func TestAWritableAreaPlantedAsALinkInAWritableFolderIsNotBoundWhereItLeads(t *testing.T) {
	d := with(machine, disk{
		"/home/u/.bashrc": "file", "/home/u/.config/systemd/user": "dir",
		"/home/u/project/out": "-> /home/u/.bashrc",
		"/tmp/aw":             "-> /home/u/.config/systemd/user",
		"/tmp/td":             "-> /home/u/.config/systemd/user",
		"/tmp/gomod":          "-> /home/u/.config/systemd/user",
		"/home/u/data":        "-> /mnt/data", "/mnt/data": "dir",
	})
	in := sample(d)
	in.AllowWrite = []string{"/home/u/project/out", "/tmp/aw", "/home/u/data"}
	in.Env["TMPDIR"] = "/tmp/td"
	in.Env["GOMODCACHE"] = "/tmp/gomod"
	in.Tmp = []string{"/tmp", "/tmp/td"}
	args := Args(in)
	got := strings.Join(args, " ")
	for _, target := range []string{"/home/u/.bashrc", "/home/u/.config/systemd/user"} {
		if index(args, "--bind", target) >= 0 {
			t.Errorf("%s, where a planted link leads, is bound writable:\n%s", target, got)
		}
	}
	if index(args, "--bind", "/mnt/data") < 0 {
		t.Errorf("the owner's link ~/data -> /mnt/data, in a folder no command can write, is not bound:\n%s", got)
	}
	if !strings.Contains(got, "--setenv TMPDIR /tmp") {
		t.Errorf("a TMPDIR that leads to a read-only folder is kept:\n%s", got)
	}
	for _, p := range writable(in) {
		if p == "/home/u/.bashrc" || p == "/home/u/.config/systemd/user" {
			t.Errorf("writable names %s", p)
		}
	}
}
