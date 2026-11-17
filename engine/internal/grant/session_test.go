package grant

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
)

func TestSessionGrantLifecycle(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	folder, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	below := filepath.Join(folder, "a", "b")
	if err := os.MkdirAll(below, 0o755); err != nil {
		t.Fatal(err)
	}
	if SessionGranted(folder, "s1") != "" {
		t.Fatal("granted before any grant")
	}
	rec, err := GrantSession(folder, "s1")
	if err != nil || rec.Root != folder || rec.Class != SessionClass {
		t.Fatalf("GrantSession = %+v, %v", rec, err)
	}
	if got := SessionGranted(below, "s1"); got != folder {
		t.Fatalf("below the folder: %q", got)
	}
	if SessionGranted(folder, "s2") != "" || SessionGranted(folder, "") != "" {
		t.Fatal("another session, or none, is granted")
	}
	if _, err := GrantSession(folder, " "); err == nil {
		t.Fatal("a grant without a session id")
	}
	if h := History(); len(h) != 1 || h[0].Root != folder || h[0].Class != SessionClass {
		t.Fatalf("history = %+v", h)
	}

	// An expired record grants nothing.
	past := SessionRecord{Record{SessionClass, folder, "2020-01-01T00:00:00.000Z", "2020-01-01T12:00:00.000Z"}, "old"}
	b := []byte(`{"class":"outside-write","root":"` + past.Root + `","granted_at":"` + past.GrantedAt + `","expires_at":"` + past.ExpiresAt + `","session":"old"}`)
	if err := os.WriteFile(SessionFile(folder, "old"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if SessionGranted(folder, "old") != "" {
		t.Fatal("an expired session grant is live")
	}

	folders, err := RevokeSession("s1")
	if err != nil || len(folders) != 1 || folders[0] != folder || SessionGranted(folder, "s1") != "" {
		t.Fatalf("RevokeSession = %v, %v", folders, err)
	}
	until, _ := isodate.Parse(rec.ExpiresAt)
	if d := time.Until(until); d > SessionTTL || d < SessionTTL-time.Minute {
		t.Fatalf("expiry %s is not the session's backstop", rec.ExpiresAt)
	}
}

func TestSessionFolderRefuses(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", home)
	for dir, why := range map[string]string{
		"/":                      "home directory or /",
		home:                     "home directory or /",
		filepath.Dir(home):       "home directory or /",
		filepath.Join(home, "x"): "not a folder",
	} {
		if _, err := SessionFolder(dir); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("SessionFolder(%s) = %v, want %q", dir, err, why)
		}
	}
	jhonState := filepath.Join(state, "jhon")
	if err := os.MkdirAll(jhonState, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := SessionFolder(jhonState); err == nil {
		t.Error("jhon's state is granted")
	}
}
