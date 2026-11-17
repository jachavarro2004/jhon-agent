package hook

import (
	"slices"
	"strings"
	"testing"
)

// pre-tool joins the guards' notes, and an ask is never read as a note,
// so a guard's note never stands in for another guard's ask.
func TestNoteOf(t *testing.T) {
	if n, ok := noteOf([]byte(noteJSON("allowed by the owner's yolo mode: push: git push"))); !ok || n != "allowed by the owner's yolo mode: push: git push" {
		t.Errorf("noteOf(noteJSON) = %q, %v", n, ok)
	}
	for _, out := range []string{askJSON("push: git push"), "", "{}",
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"x","permissionDecision":"allow"}}`,
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"x"},"decision":"block"}`,
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":""}}`} {
		if n, ok := noteOf([]byte(out)); ok {
			t.Errorf("noteOf(%s) = %q, a note", out, n)
		}
	}
}

func TestReturnShaped(t *testing.T) {
	for text, want := range map[string]bool{
		"status: success\nsummary: added the timeout\nfiles_changed:\n  - src/a.ts: timeout\nvalidation:\n  - npm test: 3 passed\nrisks: none": true,
		"Done.":                                false,
		"status: success\nsummary: x":          false,
		"status: finished\nvalidation:\n  - x": false,
	} {
		if got := ReturnShaped(text); got != want {
			t.Errorf("ReturnShaped(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestBands(t *testing.T) {
	if f, s := bands(""); f != 250_000 || s != 150_000 {
		t.Fatalf("default bands = %d, %d", f, s)
	}
	if f, s := bands("0,5"); f != 250_000 || s != 150_000 {
		t.Fatalf("a zero band falls back: %d, %d", f, s)
	}
	for tokens, want := range map[float64]int{120_000: 0, 250_000: 1, 399_999: 1, 400_000: 2} {
		if got := BandOf(tokens, 250_000, 150_000); got != want {
			t.Errorf("BandOf(%v) = %d, want %d", tokens, got, want)
		}
	}
}

func TestLastContext(t *testing.T) {
	text := `{"type":"assistant","message":{"usage":{"input_tokens":10,"cache_creation_input_tokens":1000,"cache_read_input_tokens":5}}}` + "\n" +
		`{"type":"user","message":{"usage":{"input_tokens":99}}}` + "\n"
	if n, ok := LastContext(text); !ok || n != 1015 {
		t.Fatalf("LastContext = %v, %v", n, ok)
	}
	if _, ok := LastContext(`{"type":"user"}`); ok {
		t.Fatal("no assistant usage read as a context")
	}
}

// Every hook has a handler: an unregistered one would go unanswered, and
// its own tests would still pass.
func TestStepOneHooksAreRegistered(t *testing.T) {
	for _, name := range []string{"context-notice", "framework-guard", "inspect-guard", "return-shape", "safety-guard", "scope-guard", "vcs-guard"} {
		if Handlers[name] == nil {
			t.Errorf("hook %s is not registered", name)
		}
	}
}

func TestWritesReadsTheFileAWriteNames(t *testing.T) {
	if _, paths, ok := writes(Payload{"tool_name": "Write", "tool_input": map[string]any{"file_path": "a.txt"}}); !ok || !slices.Equal(paths, []string{"a.txt"}) {
		t.Errorf("Write: %q %v", paths, ok)
	}
	if _, _, ok := writes(Payload{"tool_name": "Bash"}); ok {
		t.Error("Bash names no file by itself")
	}
}

// The orchestrator learns the owner's shell when it is not POSIX, so a
// command it hands the owner is written for it.
func TestOwnerShellNote(t *testing.T) {
	if got := OwnerShellNote("/usr/bin/fish"); !strings.Contains(got, "owner's shell is fish") || !strings.Contains(got, "set -x") {
		t.Errorf("fish: %q", got)
	}
	// Claude Code's note keeps `!`: pi's is shell/lib/owner-shell.ts (L222).
	for _, s := range []string{"/usr/bin/fish", "/usr/bin/nu"} {
		if got := OwnerShellNote(s); !strings.Contains(got, "(by hand or with `!`)") {
			t.Errorf("%q: %q, want Claude Code's `!`", s, got)
		}
	}
	for _, s := range []string{"/bin/bash", "/usr/bin/zsh", "/bin/sh", ""} {
		if got := OwnerShellNote(s); got != "" {
			t.Errorf("%q: %q, want none", s, got)
		}
	}
}

// The refusal the agent relays points pi's owner to /jhon:grants, where the
// grants live now (L214); the mode card, /jhon mode, the Grants tab and its alt+j are gone.
func TestGrantHintPointsToJhonGrants(t *testing.T) {
	h := grantHint("push")
	if !strings.HasSuffix(h, "or in pi, which has no `!`, from /jhon:grants in the shell.") || strings.Contains(h, "mode") || strings.Contains(h, "Grants tab") || strings.Contains(h, "alt+j") || strings.Contains(h, "/jhon grants") {
		t.Errorf("grantHint(push) = %q, want /jhon:grants and no mode, Grants tab, alt+j or /jhon grants", h)
	}
}
