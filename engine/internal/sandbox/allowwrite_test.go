package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// T239 (odd/tasks/studio-integration-v1.md S5): a sandbox.allow_write folder
// that does not exist yet is created outside the sandbox before a run, only
// where that is safe: under the home, through no link, in and around no path
// the sandbox hides or keeps read-only, and only where no area a sandboxed
// command may write already holds it. Every other one is named with why.
func TestAMissingAllowWriteFolderIsCreatedOnlyWhereItIsSafe(t *testing.T) {
	d := with(machine, disk{
		"/home/u/videos": "dir", "/home/u/tools": "dir", "/home/u/notes": "file",
		"/home/u/link": "-> /mnt/data", "/mnt/data": "dir", "/srv/existing": "dir",
	})
	in := sample(d)
	in.Path = []string{"/home/u/tools/bin", "/usr/bin", "relative"}
	in.AllowWrite = []string{
		"/home/u/videos/demo/out",
		"/home/u/new/a",
		"/home/u/videos/demo/out",
		"/srv/existing",
		"/home/u/.cache/x/y",
		"/home/u/project/out",
		"/tmp/x",
		"/home/u/.ssh/work",
		"/home/u/.gnupg/work",
		"/home/u/.config/jhon/extra",
		"/home/u/.local/share/jhon/x",
		"/home/u/.claude/x",
		"/home/u/.local",
		"/home/u/tools/bin",
		"/home/u/tools/sub",
		"/opt/new",
		"/home/u/link/new",
		"/home/u/notes/x",
		"/home/u",
		"relative",
	}
	got := MissingAllowWrite(in)
	want := []MissingFolder{
		{Folder: "/home/u/videos/demo/out", Create: true},
		{Folder: "/home/u/new/a", Create: true},
		{Folder: "/home/u/.ssh/work", Why: "it lies in /home/u/.ssh, which the sandbox hides: pick a folder elsewhere"},
		{Folder: "/home/u/.gnupg/work", Why: "it lies in /home/u/.gnupg, which the sandbox hides: pick a folder elsewhere"},
		{Folder: "/home/u/.config/jhon/extra", Why: "it lies in /home/u/.config/jhon, one of Jhon's or the runtimes' own folders, which the sandbox keeps read-only: pick a folder elsewhere"},
		{Folder: "/home/u/.local/share/jhon/x", Why: "it lies in /home/u/.local/share/jhon, one of Jhon's or the runtimes' own folders, which the sandbox keeps read-only: pick a folder elsewhere"},
		{Folder: "/home/u/.claude/x", Why: "it lies in /home/u/.claude, one of Jhon's or the runtimes' own folders, which the sandbox keeps read-only: pick a folder elsewhere"},
		{Folder: "/home/u/.local", Why: "it holds /home/u/.local/state/jhon, one of Jhon's or the runtimes' own folders, which the sandbox keeps read-only: pick a folder that holds none"},
		{Folder: "/home/u/tools/bin", Why: "it is /home/u/tools/bin, a PATH folder, which the sandbox keeps read-only: pick a folder that is not on PATH"},
		{Folder: "/home/u/tools/sub", Create: true},
		{Folder: "/opt/new", Why: "Jhon creates a missing folder only under the home folder (/home/u): create it yourself (mkdir -p /opt/new)"},
		{Folder: "/home/u/link/new", Why: "/home/u/link is a link, and Jhon creates no folder through a link: set sandbox.allow_write to the folder it leads to, or create it yourself (mkdir -p /home/u/link/new)"},
		{Folder: "/home/u/notes/x", Why: "/home/u/notes is a file, not a folder"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("MissingAllowWrite =\n%s\nwant\n%s", show(got), show(want))
	}
	// A PATH folder that does not exist yet is held by the folder too.
	in.AllowWrite = []string{"/home/u/kit"}
	in.Path = []string{"/home/u/kit/bin"}
	if got, want := MissingAllowWrite(in), []MissingFolder{{Folder: "/home/u/kit", Why: "it holds /home/u/kit/bin, a PATH folder, which the sandbox keeps read-only: pick a folder that holds none"}}; !slices.Equal(got, want) {
		t.Errorf("a folder that holds a PATH folder: %s, want %s", show(got), show(want))
	}
	// A home that is / or no real folder makes nothing.
	for _, home := range []string{"/", "/home/u/link", "/home/missing"} {
		in := sample(with(d, disk{"/": "dir"}))
		in.Home, in.AllowWrite = home, []string{"/home/u/videos/x"}
		if got := MissingAllowWrite(in); len(got) != 1 || got[0].Create || !strings.HasPrefix(got[0].Why, "Jhon creates a missing folder only under the home folder") {
			t.Errorf("home %s: %s; want nothing made", home, show(got))
		}
	}
	// A home reached through a link (/home -> /var/home): the folder the
	// settings name under the link's path is the one under the real home.
	d = with(d, disk{"/home": "-> /var/home", "/home/u": "-> /var/home/u", "/var/home": "dir", "/var/home/u": "dir"})
	in = sample(d)
	in.Home = "/var/home/u"
	in.AllowWrite = []string{"/home/u/videos2/out"}
	if got, want := MissingAllowWrite(in), []MissingFolder{{Folder: "/home/u/videos2/out", Create: true}}; !slices.Equal(got, want) {
		t.Errorf("under a linked home: %s, want %s", show(got), show(want))
	}
}

func show(folders []MissingFolder) string {
	var b strings.Builder
	for _, f := range folders {
		b.WriteString("  " + f.Folder + " create=" + map[bool]string{true: "yes", false: "no"}[f.Create] + " why=" + f.Why + "\n")
	}
	return b.String()
}

// On the disk: the folder and its missing parents are made, with no link
// followed; a link anywhere on the way, also one that appears after the
// check, stops it and nothing is made where the link leads.
func TestCreateAllowWriteMakesTheFolderAndFollowsNoLink(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, elsewhere := filepath.Join(base, "home", "u"), filepath.Join(base, "elsewhere")
	for _, dir := range []string{home, elsewhere} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(elsewhere, filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}
	made, linked, ssh := filepath.Join(home, "videos", "demo", "out"), filepath.Join(home, "link", "new"), filepath.Join(home, ".ssh", "work")
	in := Input{Home: home, Env: map[string]string{}, Stat: Disk, AllowWrite: []string{made, linked, ssh}}
	created, missing := CreateAllowWrite(in)
	if !slices.Equal(created, []string{made}) {
		t.Errorf("created %v, want only %s", created, made)
	}
	if fi, err := os.Lstat(made); err != nil || !fi.IsDir() || fi.Mode().Perm()&0o022 != 0 {
		t.Errorf("%s: %v, %v; want a folder no one else may write", made, fi, err)
	}
	if len(missing) != 2 || missing[0].Folder != linked || missing[1].Folder != ssh || missing[0].Create || missing[1].Create {
		t.Errorf("missing %s; want the linked and the hidden folder, neither created", show(missing))
	}
	for _, p := range []string{filepath.Join(elsewhere, "new"), filepath.Join(home, ".ssh")} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s was made", p)
		}
	}
	// A link that appears between the check and the make: the make stops.
	if err := os.MkdirAll(filepath.Join(home, "late"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(home, "late", "swap")); err != nil {
		t.Fatal(err)
	}
	if err := mkdirUnder(home, filepath.Join(home, "late", "swap", "x")); err == nil {
		t.Error("mkdirUnder went through a link")
	}
	if _, err := os.Lstat(filepath.Join(elsewhere, "x")); err == nil {
		t.Error("a folder was made where the link leads")
	}
	// A folder that could not be made says why, with the fix.
	in.AllowWrite = []string{filepath.Join(home, "late", "swap2", "x")}
	if err := os.WriteFile(filepath.Join(home, "late", "swap2"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if created, missing := CreateAllowWrite(in); len(created) != 0 || len(missing) != 1 || !strings.Contains(missing[0].Why, "is a file, not a folder") {
		t.Errorf("a file in the way: created %v, missing %s", created, show(missing))
	}
}

// The note on a refused write says whether the path exists (unless the
// sandbox hides it) and which setting, if any, opens it.
func TestTheRefusalSaysWhichSettingOpensThePathAndWhetherItExists(t *testing.T) {
	d := with(machine, disk{
		"/home/u/notes": "file", "/home/u/tools": "dir", "/home/u/tools/bin": "dir", "/home/u/.ssh/id": "file",
		"/home/u/project/ro": "dir",
	})
	in := sample(d)
	in.AllowWrite = []string{"/home/u/tools"}
	in.Path = []string{"/home/u/tools/bin"}
	missing := []MissingFolder{{Folder: "/home/u/.gnupg/work", Why: "it lies in /home/u/.gnupg, which the sandbox hides: pick a folder elsewhere"}}
	const stop = "; that is a stop: tell the owner the path and why, never write it elsewhere"
	open := "; no folder the sandbox opens for writing holds it: sandbox.allow_write opens it, or a folder that holds it, from the next command (jhon settings set sandbox.allow_write <folder>,…)"
	for _, c := range []struct{ path, want string }{
		{"/home/u/notes", "the sandbox refused a write to /home/u/notes, which exists" + open + stop},
		{"/home/u/escape.txt", "the sandbox refused a write to /home/u/escape.txt, which does not exist" + open + stop},
		{"/home/u/.gnupg", "the sandbox refused a write to /home/u/.gnupg, which does not exist; sandbox.allow_write names /home/u/.gnupg/work, which does not exist and Jhon did not create it: it lies in /home/u/.gnupg, which the sandbox hides: pick a folder elsewhere" + stop},
		{"/home/u/.gnupg/work/x", "the sandbox refused a write to /home/u/.gnupg/work/x, which does not exist; sandbox.allow_write names /home/u/.gnupg/work, which does not exist and Jhon did not create it: it lies in /home/u/.gnupg, which the sandbox hides: pick a folder elsewhere" + stop},
		{"/home/u/tools/bin/x", "the sandbox refused a write to /home/u/tools/bin/x, which does not exist; it is in /home/u/tools/bin, a PATH folder, which stays read-only so that no sandboxed command changes a program run later: sandbox.allow_write does not open it" + stop},
		{"/home/u/.config/jhon/new.toml", "the sandbox refused a write to /home/u/.config/jhon/new.toml, which does not exist; it is in /home/u/.config/jhon, one of Jhon's or the runtimes' own folders, read-only by design: no setting opens it" + stop},
		{"/home/u/.config/jhon/settings.toml", "the sandbox refused a write to /home/u/.config/jhon/settings.toml, which exists; it is /home/u/.config/jhon/settings.toml, one of Jhon's or the runtimes' own files, read-only by design: no setting opens it" + stop},
		{"/home/u/.ssh/id", "the sandbox refused a write to /home/u/.ssh/id; it is in /home/u/.ssh, which the sandbox hides (credentials, session sockets, snapshots): no setting opens it" + stop},
		{"/home/u/.netrc", "the sandbox refused a write to /home/u/.netrc; it is /home/u/.netrc, which the sandbox hides (credentials, session sockets, snapshots): no setting opens it" + stop},
		{"/home/u/project/ro/x", "the sandbox refused a write to /home/u/project/ro/x, which does not exist; it is in /home/u/project, a folder the sandbox opens for writing, so the file system itself refused it, not the sandbox: no setting of Jhon's opens it" + stop},
		{"", RefusalNote("")},
	} {
		if got := Refusal(in, c.path, missing); got != c.want {
			t.Errorf("Refusal(%q) =\n%s\nwant\n%s", c.path, got, c.want)
		}
	}
	if !strings.HasPrefix(Refusal(in, "/home/u/notes", nil), "the sandbox refused a write to ") {
		t.Error("the note lost the words the audit reads it by")
	}
}
