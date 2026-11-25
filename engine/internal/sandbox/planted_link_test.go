package sandbox

import (
	"strings"
	"testing"
)

// L26 blocker (odd/tasks/studio-integration-v1.md): a write area whose path
// as written lies in another writable area (an allow_write entry in the
// project or /tmp, a TMPDIR or an env-named cache under /tmp) can be
// replaced by a sandboxed command with a link; the next run must not bind
// where the link leads. After L28 ("No seguir enlaces nunca") an owner's link
// in a folder no command can write (~/data -> /mnt/data) is not bound either.
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
	if index(args, "--bind", "/mnt/data") >= 0 {
		t.Errorf("the link ~/data -> /mnt/data is bound where it leads:\n%s", got)
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

// L27 B1: an allow_write entry inside another project, which a run there
// replaced with a link, is not bound where it leads; with L28 no writable
// area that is or passes through a link is, and the real path named
// directly is.
func TestAnAllowWriteEntryPlantedFromAnotherProjectIsNotBoundWhereItLeads(t *testing.T) {
	d := with(machine, disk{
		"/home/u/.bashrc": "file", "/home/u/.config/systemd/user": "dir",
		"/home/u/other": "dir", "/home/u/other/.git": "dir",
		"/home/u/other/out":   "-> /home/u/.bashrc",
		"/home/u/other/units": "-> /home/u/.config/systemd/user",
		"/home/u/data":        "-> /mnt/data", "/mnt/data": "dir", "/mnt/data/sub": "dir",
		"/home/u/via": "-> /mnt/data", "/home/u/via/sub": "-> /mnt/data/sub", "/home/u/gm": "-> /mnt/gm", "/mnt/gm": "dir",
	})
	in := sample(d)
	in.AllowWrite = []string{"/home/u/other/out", "/home/u/other/units", "/home/u/data", "/home/u/via/sub"}
	in.Env["GOMODCACHE"] = "/home/u/gm"
	args := Args(in)
	got := strings.Join(args, " ")
	for _, target := range []string{"/home/u/.bashrc", "/home/u/.config/systemd/user", "/mnt/data", "/mnt/data/sub", "/mnt/gm"} {
		if index(args, "--bind", target) >= 0 {
			t.Errorf("%s, reached through a link, is bound writable:\n%s", target, got)
		}
	}
	in.AllowWrite = []string{"/mnt/data"}
	if args := Args(in); index(args, "--bind", "/mnt/data") < 0 {
		t.Errorf("/mnt/data named directly is not bound:\n%s", strings.Join(args, " "))
	}
}

// L28: status and the note say an area is not used because it is or passes
// through a link, and name the real path.
func TestAWritableAreaThroughALinkIsNamedWithItsRealPath(t *testing.T) {
	d := with(machine, disk{
		"/home/u/data": "-> /mnt/data", "/mnt/data": "dir", "/mnt/data/sub": "dir",
		"/home/u/via": "-> /mnt/data", "/home/u/via/sub": "-> /mnt/data/sub", "/home/u/gm": "-> /mnt/gm", "/mnt/gm": "dir",
		"/home/u/tl": "-> /tmp", "/home/u/plain": "dir",
	})
	in := sample(d)
	in.AllowWrite = []string{"/home/u/data", "/home/u/via/sub", "/home/u/plain", "/home/u/missing"}
	in.Env["GOMODCACHE"] = "/home/u/gm"
	in.Env["TMPDIR"] = "/home/u/tl"
	want := []Unused{
		{Name: "sandbox.allow_write folder", Path: "/home/u/data", Link: "/home/u/data", Real: "/mnt/data"},
		{Name: "sandbox.allow_write folder", Path: "/home/u/via/sub", Link: "/home/u/via", Real: "/mnt/data/sub"},
		{Name: "$GOMODCACHE", Path: "/home/u/gm", Link: "/home/u/gm", Real: "/mnt/gm"},
		{Name: "$TMPDIR", Path: "/home/u/tl", Link: "/home/u/tl", Real: "/tmp"},
	}
	got := UnusedWriteAreas(in)
	if len(got) != len(want) {
		t.Fatalf("UnusedWriteAreas = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("UnusedWriteAreas[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if line, want := got[1].Line(), "sandbox.allow_write folder /home/u/via/sub is not used for writing: it passes through the link /home/u/via, and the sandbox opens no link for writing: name the real path (/mnt/data/sub) instead"; line != want {
		t.Errorf("Line =\n%s\nwant\n%s", line, want)
	}
	if line, want := got[0].Line(), "sandbox.allow_write folder /home/u/data is not used for writing: it is a link, and the sandbox opens no link for writing: name the real path (/mnt/data) instead"; line != want {
		t.Errorf("Line =\n%s\nwant\n%s", line, want)
	}
	note := Refusal(in, "/home/u/data/x", nil)
	if want := "the sandbox refused a write to /home/u/data/x, which does not exist; sandbox.allow_write folder /home/u/data is not used for writing: it is a link, and the sandbox opens no link for writing: name the real path (/mnt/data) instead; that is a stop: tell the owner the path and why, never write it elsewhere"; note != want {
		t.Errorf("Refusal =\n%s\nwant\n%s", note, want)
	}
	// A TMPDIR that is a link is not the program's: it gets /tmp.
	if got := strings.Join(Args(in), " "); !strings.Contains(got, "--setenv TMPDIR /tmp") {
		t.Errorf("a TMPDIR that is a link is kept:\n%s", got)
	}
}
