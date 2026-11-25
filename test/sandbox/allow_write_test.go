package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T239 (odd/tasks/studio-integration-v1.md S5, odd/tasks/jhon-studio.md
// L14): a sandbox.allow_write folder that does not exist yet is made by
// `jhon sandbox run` before the sandbox starts, and is writable inside it,
// with no mkdir by the owner; its siblings stay read-only.
func TestAMissingAllowWriteFolderIsMadeAndWritable(t *testing.T) {
	b := newBox(t)
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/videos/demo/out\"]\n")
	out := filepath.Join(b.home, "videos", "demo", "out")
	r := b.sh(t, b.proj, "", `echo x > "$HOME/videos/demo/out/f.txt" && echo done`)
	if r.Code != 0 || r.Stdout != "done\n" || r.Stderr != "" {
		t.Fatalf("a write to the missing allow_write folder: exit %d, stdout %q, stderr %q; want 0, done, none", r.Code, r.Stdout, r.Stderr)
	}
	if got := support.Read(t, filepath.Join(out, "f.txt")); got != "x\n" {
		t.Errorf("the file holds %q", got)
	}
	if fi, err := os.Lstat(out); err != nil || !fi.IsDir() || fi.Mode().Perm()&0o022 != 0 {
		t.Errorf("%s: %v, %v; want a folder no one else may write", out, fi, err)
	}
	// Its parent and siblings, made with it, stay read-only.
	sibling := filepath.Join(b.home, "videos", "demo", "sibling.txt")
	r = b.sh(t, b.proj, "", `echo x > "$HOME/videos/demo/sibling.txt"`)
	if r.Code == 0 || exists(sibling) || !strings.HasSuffix(r.Stderr, outsideNote(sibling)) {
		t.Errorf("a sibling: exit %d, stderr %q; want a refused write ending with %q", r.Code, r.Stderr, outsideNote(sibling))
	}
}

// homeNote is the note on a refused write to path, which does not exist,
// directly in the home: no setting is suggested (L26 A4).
func homeNote(path string) string {
	return "the sandbox refused a write to " + path + ", which does not exist; no folder the sandbox opens for writing holds it, and no setting should open it: it lies directly in the home folder; whether to write it is the owner's decision; that is a stop: tell the owner the path and why, never write it elsewhere\n"
}

// outsideNote is the note on a refused write to path, which does not exist,
// outside every writable area, in a folder under the home.
func outsideNote(path string) string {
	return "the sandbox refused a write to " + path + ", which does not exist; no folder the sandbox opens for writing holds it: sandbox.allow_write opens it, or a folder that holds it, from the next command (jhon settings set sandbox.allow_write <folder>,…); that is a stop: tell the owner the path and why, never write it elsewhere\n"
}

// ownNote is the note on a refused write to path, which does not exist, in
// folder, one of Jhon's or the runtimes' own.
func ownNote(path, folder string) string {
	return "the sandbox refused a write to " + path + ", which does not exist; it is in " + folder + ", one of Jhon's or the runtimes' own folders, read-only by design: no setting opens it; that is a stop: tell the owner the path and why, never write it elsewhere\n"
}

// A missing folder Jhon will not make (through a link, in a hidden folder,
// outside the home) is not made, nothing appears where a link leads, and a
// write there ends with a note naming the setting, the folder, that it does
// not exist and why. `jhon sandbox status` says the same, and that the
// next command makes the others, while it makes none itself.
func TestAMissingAllowWriteFolderJhonWillNotMakeIsNamedWithWhy(t *testing.T) {
	b := newBox(t)
	elsewhere, outside := filepath.Join(b.base, "elsewhere"), filepath.Join(b.base, "outside", "new")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(b.home, "linked")); err != nil {
		t.Fatal(err)
	}
	linked, hidden, later := filepath.Join(b.home, "linked", "new"), filepath.Join(b.home, ".gnupg", "work"), filepath.Join(b.home, "later", "out")
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/linked/new\", \"~/.gnupg/work\", \""+outside+"\", \"~/later/out\"]\n")
	whyLinked := filepath.Join(b.home, "linked") + " is a link, and Jhon creates no folder through a link: set sandbox.allow_write to the folder it leads to, or create it yourself (mkdir -p " + linked + ")"
	whyHidden := "it lies in " + filepath.Join(b.home, ".gnupg") + ", which the sandbox hides: pick a folder elsewhere"
	whyOutside := "Jhon creates a missing folder only under the home folder (" + b.home + "): create it yourself (mkdir -p " + outside + ")"

	// Status first: it names each one and makes none.
	r := support.Jhon(t, b.proj, b.env, "sandbox", "status")
	for _, want := range []string{
		"; sandbox.allow_write folder " + linked + " does not exist, and Jhon will not create it: " + whyLinked,
		"; sandbox.allow_write folder " + hidden + " does not exist, and Jhon will not create it: " + whyHidden,
		"; sandbox.allow_write folder " + outside + " does not exist, and Jhon will not create it: " + whyOutside,
		"; sandbox.allow_write folder " + later + " does not exist: the next sandboxed command creates it",
	} {
		if r.Code != 0 || !strings.Contains(r.Stdout, want) || strings.Count(r.Stdout, "\n") != 1 {
			t.Errorf("status: exit %d, stdout %q; want one line holding %q", r.Code, r.Stdout, want)
		}
	}
	r = support.Jhon(t, b.proj, b.env, "sandbox", "status", "--json")
	var state struct {
		Missing []struct {
			Folder string `json:"folder"`
			Create bool   `json:"create"`
			Why    string `json:"why"`
		} `json:"missing_allow_write"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &state); err != nil || len(state.Missing) != 4 ||
		state.Missing[0].Folder != linked || state.Missing[0].Why != whyLinked || state.Missing[0].Create ||
		state.Missing[3].Folder != later || !state.Missing[3].Create || state.Missing[3].Why != "" {
		t.Errorf("status --json: %v, %q; want missing_allow_write with each folder", err, r.Stdout)
	}
	if exists(filepath.Join(b.home, "later")) {
		t.Error("status made a folder")
	}

	for _, c := range []struct{ name, folder, refused, why string }{
		{"through a link", linked, linked, whyLinked},
		{"in a hidden folder", hidden, filepath.Join(b.home, ".gnupg"), whyHidden},
		{"outside the home", outside, filepath.Join(b.base, "outside"), whyOutside},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--", "sh", "-c", `mkdir -p "$1" && echo x > "$1/f.txt"`, "sh", c.folder)
			want := "the sandbox refused a write to " + c.refused + ", which does not exist; sandbox.allow_write names " + c.folder + ", which does not exist and Jhon did not create it: " + c.why + "; that is a stop: tell the owner the path and why, never write it elsewhere\n"
			if r.Code == 0 || !strings.Contains(r.Stderr, "Read-only file system") || !strings.HasSuffix(r.Stderr, want) {
				t.Errorf("exit %d, stderr %q; want a refused write ending with %q", r.Code, r.Stderr, want)
			}
		})
	}
	for _, p := range []string{filepath.Join(elsewhere, "new"), filepath.Join(b.home, ".gnupg"), filepath.Join(b.base, "outside")} {
		if exists(p) {
			t.Errorf("%s was made", p)
		}
	}
	// The run made the one Jhon may make.
	if fi, err := os.Lstat(later); err != nil || !fi.IsDir() {
		t.Errorf("%s: %v; want the folder made by the run", later, err)
	}
}

// A write Jhon's own design keeps read-only says so: no setting opens it,
// and whether it exists.
func TestARefusedWriteToJhonsOwnFilesSaysNoSettingOpensIt(t *testing.T) {
	b := newBox(t)
	b.settings(t, "schema = 1\n")
	folder := filepath.Join(b.home, ".config", "jhon")
	r := b.sh(t, b.proj, "", `echo x > "$HOME/.config/jhon/new.toml"`)
	want := ownNote(filepath.Join(folder, "new.toml"), folder)
	if r.Code == 0 || !strings.HasSuffix(r.Stderr, want) {
		t.Errorf("exit %d, stderr %q; want it to end with %q", r.Code, r.Stderr, want)
	}
}

// Settings a sandboxed command could have written make no folder: with
// JHON_SETTINGS in the scratch root the run is refused before any
// sandbox.allow_write folder is made.
func TestSettingsACommandCouldWriteMakeNoFolder(t *testing.T) {
	b := newBox(t)
	file := filepath.Join(b.tmp, "settings.toml")
	if err := os.WriteFile(file, []byte("schema = 1\n\n[sandbox]\nallow_write = [\"~/planted/out\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := support.Jhon(t, b.proj, append(b.env, "JHON_SETTINGS="+file), "sandbox", "run", "--", "true")
	if r.Code != 126 || !strings.Contains(r.Stderr, "so nothing was run") {
		t.Errorf("exit %d, stderr %q; want the run refused (126)", r.Code, r.Stderr)
	}
	if exists(filepath.Join(b.home, "planted")) {
		t.Error("a folder was made from settings a sandboxed command could write")
	}
}

// L26 A2: status says the next sandboxed command creates a folder only when
// that command runs: with JHON_SETTINGS where a command could write it, the
// run is refused, and status says so with the run's own reason.
func TestStatusSaysNoFolderIsMadeWhenTheRunWillBeRefused(t *testing.T) {
	b := newBox(t)
	file := filepath.Join(b.tmp, "settings.toml")
	if err := os.WriteFile(file, []byte("schema = 1\n\n[sandbox]\nallow_write = [\"~/later/out\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := filepath.Join(b.home, "later", "out")
	r := support.Jhon(t, b.proj, append(b.env, "JHON_SETTINGS="+file), "sandbox", "status")
	want := "; sandbox.allow_write folder " + later + " does not exist, and Jhon will not create it: Jhon creates it only for a sandboxed command that runs, and none runs here: the settings file JHON_SETTINGS names (" + file + ") is in a folder a sandboxed command may write"
	if r.Code != 0 || !strings.Contains(r.Stdout, want) || strings.Contains(r.Stdout, "the next sandboxed command creates it") {
		t.Errorf("status: exit %d, stdout %q; want it to hold %q", r.Code, r.Stdout, want)
	}
}

// PRESERVE: with every sandbox.allow_write folder there, status says what it
// said and --json has no missing_allow_write.
func TestStatusIsUnchangedWhenEveryAllowWriteFolderExists(t *testing.T) {
	b := newBox(t)
	extra := filepath.Join(b.home, "extra")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/extra\"]\n")
	r := support.Jhon(t, b.proj, b.env, "sandbox", "status")
	if want := "sandbox on; it can run here; project " + b.proj + "\n"; r.Code != 0 || r.Stdout != want {
		t.Errorf("status: exit %d, %q; want %q", r.Code, r.Stdout, want)
	}
	r = support.Jhon(t, b.proj, b.env, "sandbox", "status", "--json")
	if r.Code != 0 || strings.Contains(r.Stdout, "missing_allow_write") {
		t.Errorf("status --json: exit %d, %q; want no missing_allow_write", r.Code, r.Stdout)
	}
}
