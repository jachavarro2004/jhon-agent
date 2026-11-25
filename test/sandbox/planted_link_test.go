package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// L26 blocker (odd/tasks/studio-integration-v1.md): run 1 replaces a
// sandbox.allow_write entry in the project, and one in the scratch root,
// with links to ~/.bashrc and ~/.config/systemd/user; run 2 must find
// neither writable.
func TestAnAllowWriteEntryReplacedByALinkDoesNotOpenWhereItLeads(t *testing.T) {
	b := newBox(t)
	units := filepath.Join(b.home, ".config", "systemd", "user")
	if err := os.MkdirAll(units, 0o755); err != nil {
		t.Fatal(err)
	}
	bashrc := filepath.Join(b.home, ".bashrc")
	if err := os.WriteFile(bashrc, []byte("# owner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\""+filepath.Join(b.proj, "out")+"\", \""+filepath.Join(b.tmp, "aw")+"\"]\n")
	r := b.sh(t, b.proj, "", `ln -s "$HOME/.bashrc" "$HOME/project/out" && ln -s "$HOME/.config/systemd/user" "`+filepath.Join(b.tmp, "aw")+`" && echo planted`)
	if r.Code != 0 || r.Stdout != "planted\n" {
		t.Fatalf("run 1: exit %d, stdout %q, stderr %q; want the links planted", r.Code, r.Stdout, r.Stderr)
	}
	r = b.sh(t, b.proj, "", `echo PWNED >> "$HOME/.bashrc"; touch "$HOME/.config/systemd/user/evil.service"`)
	if got := support.Read(t, bashrc); got != "# owner\n" {
		t.Errorf("run 2 wrote ~/.bashrc through the planted link: %q (exit %d, stderr %q)", got, r.Code, r.Stderr)
	}
	if exists(filepath.Join(units, "evil.service")) {
		t.Errorf("run 2 wrote ~/.config/systemd/user through the planted link (exit %d, stderr %q)", r.Code, r.Stderr)
	}
}

// L27 B1: a run in another project replaces the allow_write folder there
// with a link to ~/.bashrc; the next run in this project must not open it.
func TestAnAllowWriteEntryPlantedFromAnotherProjectDoesNotOpenWhereItLeads(t *testing.T) {
	b := newBox(t)
	other := filepath.Join(b.home, "other")
	if err := os.MkdirAll(filepath.Join(other, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	support.GitRepo(t, other)
	bashrc := filepath.Join(b.home, ".bashrc")
	if err := os.WriteFile(bashrc, []byte("# owner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/other/out\"]\n")
	r := b.sh(t, other, "", `rmdir out && ln -s "$HOME/.bashrc" out && echo planted`)
	if r.Code != 0 || r.Stdout != "planted\n" {
		t.Fatalf("run 1 in ~/other: exit %d, stdout %q, stderr %q; want the link planted", r.Code, r.Stdout, r.Stderr)
	}
	r = b.sh(t, b.proj, "", `echo PWNED >> "$HOME/other/out"`)
	if got := support.Read(t, bashrc); got != "# owner\n" {
		t.Errorf("run 2 wrote ~/.bashrc through the link planted in ~/other: %q (exit %d, stderr %q)", got, r.Code, r.Stderr)
	}
}

// L27 B2: with allow_write ~/.local, a run renames ~/.local/share and
// ~/.local/bin and plants its own release and jhon program; Jhon's must stay
// as they were (the run is refused, L28).
func TestJhonsReleaseAndProgramCannotBeReplacedThroughARenamedParent(t *testing.T) {
	b := newBox(t)
	// The state folder outside ~/.local, so the state check is not what stops it.
	b.env = append(b.env, "XDG_STATE_HOME="+filepath.Join(b.base, "state"))
	release, program := filepath.Join(b.home, ".local", "share", "jhon", "current", "jhon"), filepath.Join(b.home, ".local", "bin", "jhon")
	for _, f := range []string{release, program} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("owner\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/.local\"]\n")
	r := b.sh(t, b.proj, "", `cd "$HOME/.local" && mv share share.old; mv bin bin.old; mkdir -p share/jhon/current bin && echo EVIL > share/jhon/current/jhon && echo EVIL > bin/jhon`)
	for _, f := range []string{release, program} {
		if got := support.Read(t, f); got != "owner\n" {
			t.Errorf("%s now holds %q (exit %d, stderr %q)", f, got, r.Code, r.Stderr)
		}
	}
	if r.Code != 126 || !strings.Contains(r.Stderr, "name a concrete subfolder in sandbox.allow_write, such as ~/.local/share/<app>") {
		t.Errorf("exit %d, stderr %q; want the run refused (126) with the fix", r.Code, r.Stderr)
	}
}

// L28: an allow_write folder that is a link is not used; status, and the
// note on a write there, say so and name the real path. An allow_write
// that holds Jhon's release makes status say the run runs nothing, with
// the fix.
func TestStatusAndTheNoteNameAnAllowWriteLinkAndAFolderThatHoldsJhonsRelease(t *testing.T) {
	b := newBox(t)
	data, link := filepath.Join(b.base, "data"), filepath.Join(b.home, "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(data, link); err != nil {
		t.Fatal(err)
	}
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/data\"]\n")
	unused := "sandbox.allow_write folder " + link + " is not used for writing: it is a link, and the sandbox opens no link for writing: name the real path (" + data + ") instead"
	if r := support.Jhon(t, b.proj, b.env, "sandbox", "status"); r.Code != 0 || !strings.HasSuffix(r.Stdout, "; "+unused+"\n") {
		t.Errorf("status: exit %d, %q; want it to end with %q", r.Code, r.Stdout, unused)
	}
	r := b.sh(t, b.proj, "", `echo x > "$HOME/data/x"`)
	want := "the sandbox refused a write to " + filepath.Join(link, "x") + ", which does not exist; " + unused + "; that is a stop: tell the owner the path and why, never write it elsewhere\n"
	if r.Code == 0 || !strings.HasSuffix(r.Stderr, want) || exists(filepath.Join(data, "x")) {
		t.Errorf("a write through the link: exit %d, stderr %q; want it refused, ending with %q", r.Code, r.Stderr, want)
	}

	share := filepath.Join(b.home, ".local", "share", "jhon", "current")
	if err := os.MkdirAll(share, 0o755); err != nil {
		t.Fatal(err)
	}
	b.env = append(b.env, "XDG_STATE_HOME="+filepath.Join(b.base, "state"))
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/.local\"]\n")
	refused := "jhon sandbox run runs nothing here: sandbox.allow_write folder " + filepath.Join(b.home, ".local") + " holds Jhon's release folder (" + filepath.Join(b.home, ".local", "share", "jhon") + "), which a sandboxed command could then replace by renaming a folder around it; fix: name a concrete subfolder in sandbox.allow_write, such as ~/.local/share/<app>, not a folder that holds Jhon's own files or a PATH folder"
	if r := support.Jhon(t, b.proj, b.env, "sandbox", "status"); r.Code != 0 || !strings.HasSuffix(r.Stdout, "; "+refused+"\n") {
		t.Errorf("status: exit %d, %q; want it to end with %q", r.Code, r.Stdout, refused)
	}
}

// L29: a run renames the folder in ~/.gradle around a PATH folder and plants
// its own program; the program on PATH stays the owner's.
func TestAProgramOnPATHInAHomeCacheCannotBeReplacedThroughARenamedParent(t *testing.T) {
	b := newBox(t)
	bin := filepath.Join(b.home, ".gradle", "wrapper", "dists", "x", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(bin, "gradle")
	if err := os.WriteFile(program, []byte("owner\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(b.env, "PATH="+bin+":"+os.Getenv("PATH"))
	r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `cd "$HOME/.gradle" && mv wrapper wrapper.old && mkdir -p wrapper/dists/x/bin && echo EVIL > wrapper/dists/x/bin/gradle`)
	if got := support.Read(t, program); got != "owner\n" {
		t.Errorf("%s now holds %q (exit %d, stderr %q)", program, got, r.Code, r.Stderr)
	}
	if r.Code == 0 {
		t.Errorf("the rename succeeded: stderr %q", r.Stderr)
	}
	line := "the tool folder " + filepath.Join(b.home, ".gradle") + " is not used for writing: it holds the PATH folder " + bin + ", where a sandboxed command could put a program run later outside the sandbox: move the tool's bin out of the cache, or take it off PATH"
	if r := support.Jhon(t, b.proj, env, "sandbox", "status"); r.Code != 0 || !strings.HasSuffix(r.Stdout, "; "+line+"\n") {
		t.Errorf("status: exit %d, %q; want it to end with %q", r.Code, r.Stdout, line)
	}
}
