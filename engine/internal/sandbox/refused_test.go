package sandbox

import (
	"strings"
	"testing"
)

// T128g (L1303 G3): the note names the path the first "Read-only file
// system" line of a command's output names, as the common tools write it.
func TestRefusedWriteNamesThePathTheOutputSaysIsReadOnly(t *testing.T) {
	for _, c := range []struct{ output, path string }{
		{"sh: 1: cannot create /home/u/escape.txt: Read-only file system\n", "/home/u/escape.txt"},
		{"bash: line 1: /home/u/escape.txt: Read-only file system\n", "/home/u/escape.txt"},
		{"/usr/bin/bash: line 1: /home/u/escape.txt: Read-only file system\n", "/home/u/escape.txt"},
		{"touch: cannot touch '/home/u/a b/c.txt': Read-only file system\n", "/home/u/a b/c.txt"},
		{"mkdir: cannot create directory ‘/home/u/x’: Read-only file system\n", "/home/u/x"},
		{"error: failed to create directory `/home/u/.local/share/uv/tools/ruff`: Read-only file system (os error 30)\n", "/home/u/.local/share/uv/tools/ruff"},
		{"OSError: [Errno 30] Read-only file system: '/home/u/.local/lib/x'\n", "/home/u/.local/lib/x"},
		{"ok\nwarning: first\nrm: cannot remove '/a/b': Read-only file system\nrm: cannot remove '/c/d': Read-only file system\n", "/a/b"},
		{"Read-only file system", ""},
	} {
		path, said := RefusedWrite(c.output)
		if !said || path != c.path {
			t.Errorf("RefusedWrite(%q) = %q, %v; want %q, true", c.output, path, said, c.path)
		}
	}
	for _, output := range []string{"", "done\n", "permission denied: /home/u/x\n", "read-only file system\n"} {
		if path, said := RefusedWrite(output); said {
			t.Errorf("RefusedWrite(%q) = %q, true; want nothing said", output, path)
		}
	}
}

// The note is the spec's sentence, with the path; a line that names none
// still gets the note.
func TestRefusalNoteIsTheStopWithThePath(t *testing.T) {
	if got, want := RefusalNote("/home/u/x"), "the sandbox refused a write to /home/u/x; that is a stop: tell the owner the path (jhon settings sandbox.allow_write), never write it elsewhere"; got != want {
		t.Errorf("RefusalNote = %q, want %q", got, want)
	}
	if got := RefusalNote(""); !strings.Contains(got, "the sandbox refused a write to the path the output names; that is a stop") {
		t.Errorf("RefusalNote(\"\") = %q", got)
	}
}

// Watch sees the output as it is written, in pieces that may cut a line.
func TestWatchFindsTheLineAcrossWrites(t *testing.T) {
	var w Watch
	for _, piece := range []string{"build ok\nsh: 1: cannot create /home", "/u/e.txt: Read-only fi", "le system\nmore\n"} {
		if n, err := w.Write([]byte(piece)); n != len(piece) || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	if path, said := w.Refused(); !said || path != "/home/u/e.txt" {
		t.Errorf("Refused = %q, %v; want /home/u/e.txt", path, said)
	}
	var tail Watch
	tail.Write([]byte("x: /a/b: Read-only file system"))
	if path, said := tail.Refused(); !said || path != "/a/b" {
		t.Errorf("a last line with no newline: %q, %v", path, said)
	}
}
