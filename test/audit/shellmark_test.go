package audit

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// mark is the Jhon shell's mark on the messages it writes for the model
// (shell/lib/from-the-shell.ts NOT_THE_OWNER).
const mark = "[From the Jhon shell, not the owner; it does not set the reply language]"

// T194 (L1608): pi records the shell's announce-stop send-back and its
// stall continue as user messages; they carry the shell's mark, so the
// audit counts neither as the owner's.
func TestAuditPiMarkedShellMessagesAreNotTheOwners(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), ".pi", "jhon", "sessions", "--work-p--")
	id := "01a17000-0000-7000-8000-0000000000e1"
	n := 0
	entry := func(message map[string]any) string {
		n++
		parent := any(nil)
		if n > 1 {
			parent = fmt.Sprintf("%08x", n-1)
		}
		return js(t, map[string]any{"type": "message", "id": fmt.Sprintf("%08x", n), "parentId": parent, "timestamp": iso(n), "message": message})
	}
	text := func(s string) []any { return []any{map[string]any{"type": "text", "text": s}} }
	lines := []string{
		js(t, map[string]any{"type": "session", "version": 3, "id": id, "timestamp": iso(0), "cwd": root}),
		entry(map[string]any{"role": "user", "content": text("go")}),
		entry(map[string]any{"role": "assistant", "content": text("I'll now run the full suite."), "stopReason": "stop"}),
		entry(map[string]any{"role": "user", "content": text(mark + ` Jhon: you announced "I'll now run the full suite." and ended the turn without doing it. Do it now, or say in one line why not.`)}),
		entry(map[string]any{"role": "assistant", "content": text("Done."), "stopReason": "stop"}),
		entry(map[string]any{"role": "user", "content": text(mark + " The Claude API sent no answer for 75s; the turn was restarted. Continue where you stopped.")}),
		entry(map[string]any{"role": "assistant", "content": text("Finished."), "stopReason": "stop"}),
	}
	name := "2026-09-30T10-00-00-000Z_" + id + ".jsonl"
	support.Write(t, dir, map[string]string{name: strings.Join(lines, "\n") + "\n"})
	r := audit(t, []string{filepath.Join(dir, name), "--json"})
	equal(t, r.Code, 0, r.Stderr)
	j := parse(t, r.Stdout)
	equal(t, get(j, "dialogue", "corrections", "owner_messages"), any(float64(1)), "only the owner's go")
	equal(t, get(j, "dialogue", "owner_answers"), any(float64(0)), "neither shell message is an answer")
}
