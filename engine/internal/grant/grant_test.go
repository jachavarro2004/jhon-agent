package grant

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
)

func TestWriteListRevoke(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	live := isodate.Format(time.Now().Add(time.Hour))
	if err := Write(Record{"push", root, isodate.Format(time.Now()), live}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(File(root, "push"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "{\n  \"class\": \"push\",\n  \"root\": ") || !strings.HasSuffix(string(data), "\"\n}\n") {
		t.Fatalf("record is not JSON.stringify(rec, null, 2) + newline:\n%s", data)
	}
	if !Live(root, "push") {
		t.Fatal("a written grant is not live")
	}
	Write(Record{"gh-write", root, "2020-01-01T00:00:00.000Z", "2020-01-01T00:30:00.000Z"})
	got := List()
	want := []string{
		"gh-write       " + root + "  expired at 2020-01-01T00:30:00.000Z",
		"push           " + root + "  live until " + live,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("List() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if err := Revoke(root, "push"); err != nil || Live(root, "push") {
		t.Fatalf("Revoke: %v, still live: %v", err, Live(root, "push"))
	}
	if err := Revoke(root, "push"); err != nil {
		t.Fatalf("revoking an absent grant: %v", err)
	}
}

func TestListSkipsUnreadableRecords(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	Write(Record{"push", root, "", "x"})
	os.WriteFile(File(root, "deploy"), []byte("{broken"), 0o600)
	os.WriteFile(File(root, "remote"), []byte("null"), 0o600)
	b, _ := json.Marshal(map[string]any{"class": 7})
	os.WriteFile(File(root, "publish"), b, 0o600)
	got := strings.Join(List(), "\n")
	want := "7              undefined  expired at undefined\npush           " + root + "  expired at x"
	if got != want {
		t.Fatalf("List() =\n%s\nwant\n%s", got, want)
	}
}

func TestListWithoutGrants(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if got := List(); len(got) != 0 {
		t.Fatalf("List() = %q", got)
	}
}

// Records is List's records typed, each with whether it is live now, and
// leaves out one that is unreadable or whose fields are not strings.
func TestRecords(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	live := isodate.Format(time.Now().Add(time.Hour))
	Write(Record{"push", root, isodate.Format(time.Now()), live})
	Write(Record{"gh-write", root, "2020-01-01T00:00:00.000Z", "2020-01-01T00:30:00.000Z"})
	os.WriteFile(File(root, "deploy"), []byte("{broken"), 0o600)
	b, _ := json.Marshal(map[string]any{"class": 7})
	os.WriteFile(File(root, "publish"), b, 0o600)
	got := Records()
	if len(got) != 2 || got[0].Class != "gh-write" || got[0].Live || got[1].Class != "push" || !got[1].Live || got[1].ExpiresAt != live {
		t.Fatalf("Records() = %+v", got)
	}
}

// A grant of a retired class (review, verified) is not listed and can be
// revoked, which removes its file (L48).
func TestRetiredClasses(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	live := isodate.Format(time.Now().Add(time.Hour))
	Write(Record{"verified", root, isodate.Format(time.Now()), live})
	Write(Record{"push", root, isodate.Format(time.Now()), live})
	if got := List(); len(got) != 1 || !strings.HasPrefix(got[0], "push ") {
		t.Errorf("List() = %v, want push alone", got)
	}
	if r := Records(); len(r) != 1 || r[0].Class != "push" {
		t.Errorf("Records() = %v, want push alone", r)
	}
	if err := Revoke(root, "verified"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(File(root, "verified")); !os.IsNotExist(err) {
		t.Errorf("the retired grant's file is still there: %v", err)
	}
}

// bypass-session is no class (S31): a session started without prompts is
// judged by whether Jhon's guards run in it, never granted.
func TestClasses(t *testing.T) {
	if got := strings.Join(Classes, " "); got != "push gh-write publish remote deploy outside-write destroy" {
		t.Errorf("Classes = %s", got)
	}
}

// A mode is recorded for a project's main checkout, with no expiry, and
// every change is in its history; normal is the mode of a project none set.
func TestModeStore(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// A project is a git repository (T57a).
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if m := ModeOf(root); m.Mode != ModeNormal || m.Root != root || m.Since != "" {
		t.Errorf("unset: %+v", m)
	}
	if _, err := SetMode(root, "turbo"); err == nil {
		t.Error("an unknown mode was written")
	}
	rec, err := SetMode(root, ModeYolo)
	if err != nil || rec.Mode != ModeYolo || rec.Root != root || rec.Since == "" {
		t.Fatalf("SetMode = %+v, %v", rec, err)
	}
	if m := ModeOf(root); m != rec {
		t.Errorf("ModeOf = %+v, want %+v", m, rec)
	}
	if !InGrants(ModeFile(root)) || !InGrants(ModeHistoryFile()) || !InGrants(ModesDir()) {
		t.Error("the modes are not protected as the grants are")
	}
	// A record for another root, or of an unknown mode, is normal.
	os.WriteFile(ModeFile(root), []byte(`{"mode":"yolo","root":"/elsewhere","since":"x"}`), 0o600)
	if m := ModeOf(root); m.Mode != ModeNormal {
		t.Errorf("another root's record: %+v", m)
	}
	os.WriteFile(ModeFile(root), []byte(`{"mode":"turbo","root":"`+root+`","since":"x"}`), 0o600)
	if m := ModeOf(root); m.Mode != ModeNormal {
		t.Errorf("an unknown mode's record: %+v", m)
	}
	SetMode(root, ModeSuperYolo)
	SetMode(root, ModeNormal)
	var got []string
	for _, h := range ModeHistory() {
		if h.Root != root || h.By != "owner" || h.At == "" {
			t.Errorf("history %+v", h)
		}
		got = append(got, h.Mode)
	}
	if strings.Join(got, " ") != "yolo super-yolo normal" {
		t.Errorf("history = %v", got)
	}
	// A folder that is no repository takes no mode, whatever its record says.
	folder := t.TempDir()
	if _, err := SetMode(folder, ModeYolo); err != nil {
		t.Fatal(err)
	}
	if m := ModeOf(folder); m.Mode != ModeNormal {
		t.Errorf("a folder's record: %+v", m)
	}
}

// yolo covers every class but destroy; super-yolo every class; normal none.
// Of the questions no grant answers (L154, L155), review is yolo's and
// super-yolo's, a command the guard cannot read super-yolo's alone, and a
// settings write or a git config that changes what git runs no mode's; a
// class neither names is no mode's.
func TestModeCovers(t *testing.T) {
	for _, c := range Classes {
		if ModeCovers(ModeNormal, c) || !ModeCovers(ModeSuperYolo, c) || ModeCovers(ModeYolo, c) != (c != "destroy") {
			t.Errorf("%s: normal %v, yolo %v, super-yolo %v", c, ModeCovers(ModeNormal, c), ModeCovers(ModeYolo, c), ModeCovers(ModeSuperYolo, c))
		}
	}
	for c, want := range map[string][3]bool{
		"review": {false, true, true}, "unreadable": {false, false, true}, "unknown": {false, false, true},
		"git-config": {false, false, false}, "settings": {false, false, false}, "bypass-session": {false, false, false}, "": {false, false, false},
	} {
		if got := [3]bool{ModeCovers(ModeNormal, c), ModeCovers(ModeYolo, c), ModeCovers(ModeSuperYolo, c)}; got != want {
			t.Errorf("%q: normal, yolo, super-yolo cover %v, want %v", c, got, want)
		}
	}
}

func TestModeAllowedLog(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := RecordAllowed(Allowed{At: "2026-11-04T10:00:00.000Z", Mode: ModeYolo, Root: "/p", Class: "push", What: "git push", Guard: "safety-guard"}); err != nil {
		t.Fatal(err)
	}
	RecordAllowed(Allowed{At: "2026-11-04T11:00:00.000Z", Mode: ModeSuperYolo, Root: "/p", Class: "destroy", What: "git reset --hard", Guard: "safety-guard"})
	got := AllowedCalls()
	if len(got) != 2 || got[0].Class != "push" || got[1].Mode != ModeSuperYolo {
		t.Errorf("AllowedCalls() = %+v", got)
	}
}
