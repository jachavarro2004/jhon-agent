package lddswitch

import (
	"os"
	"testing"
)

func TestSwitchRecord(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	if Recorded() {
		t.Fatal("recorded before any switch")
	}
	if r := Of(root); r.LDD != On || r.Source != SourceDefault || r.Since != "" {
		t.Fatalf("default: %+v", r)
	}
	os.MkdirAll(Dir(), 0o700)
	os.WriteFile(File(root), []byte(`{"ldd":"off","root":"`+root+`","since":"x"}`), 0o600)
	if r := Of(root); r.LDD != Off || r.Source != SourceOwner || r.Since != "x" || !Recorded() {
		t.Fatalf("Of = %+v", r)
	}
	if Told(root) {
		t.Fatal("told before telling")
	}
	if err := Tell(root); err != nil || !Told(root) {
		t.Fatalf("Tell: %v", err)
	}
	os.WriteFile(File(root), []byte(`{"ldd":"off","root":"/elsewhere","since":"x"}`), 0o600)
	if r := Of(root); r.LDD != On {
		t.Errorf("another root's record: %+v", r)
	}
}
