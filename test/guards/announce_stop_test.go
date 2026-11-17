// announce-stop (T135, L1055): the main session's turn that ends with a reply
// announcing an action of its own, undone, is sent back once through the
// release plugin's Stop hook; pi's shell calls the same hook.

package guards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func announceStopRun(t *testing.T, settingsText string, kv ...any) support.Result {
	t.Helper()
	path := filepath.Join(realTemp(t), "settings.toml")
	if err := os.WriteFile(path, []byte(settingsText), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := with(map[string]any{"hook_event_name": "Stop", "stop_hook_active": false, "session_id": "s1",
		"last_assistant_message": "Slice 2 is green. I'll now run the full suite."}, kv...)
	return hook(t, support.Env("JHON_SETTINGS="+path), "announce-stop", nil, payload)
}

func TestAnnounceStopBlocksOnce(t *testing.T) {
	t.Parallel()
	r := announceStopRun(t, "schema = 1\n")
	if r.Code != 0 || r.Stderr != "" {
		t.Fatalf("code = %d: %s", r.Code, r.Stderr)
	}
	var j struct{ Decision, Reason string }
	if err := json.Unmarshal([]byte(r.Stdout), &j); err != nil {
		t.Fatalf("stdout %q: %v", r.Stdout, err)
	}
	if want := `Jhon: you announced "I'll now run the full suite." and ended the turn without doing it. Do it now, or say in one line why not.`; j.Decision != "block" || j.Reason != want {
		t.Errorf("got %+v; want block with %q", j, want)
	}
	for _, c := range []struct {
		why      string
		settings string
		kv       []any
	}{
		{"already sent back this owner turn", "schema = 1\n", []any{"stop_hook_active", true}},
		{"a sub-agent", "schema = 1\n", []any{"agent_id", "a1"}},
		{"turned off", "schema = 1\n\n[agents]\nact_on_announce = false\n", nil},
		{"a finished outcome", "schema = 1\n", []any{"last_assistant_message", "Done: the suite passes."}},
	} {
		if r := announceStopRun(t, c.settings, c.kv...); r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
			t.Errorf("%s: code %d stdout %q stderr %q; want the turn to end", c.why, r.Code, r.Stdout, r.Stderr)
		}
	}
}

// L1063 KA1, KA2: a Stop payload without last_assistant_message is judged on
// the last reply in its transcript_path; an empty reply is sent back with
// its own reason; an unreadable transcript sends nothing.
func TestAnnounceStopFromTheTranscript(t *testing.T) {
	t.Parallel()
	dir := realTemp(t)
	run := func(lines ...string) support.Result {
		path := filepath.Join(dir, "settings.toml")
		if err := os.WriteFile(path, []byte("schema = 1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		transcript := filepath.Join(realTemp(t), "session.jsonl")
		if lines != nil {
			if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		payload := map[string]any{"hook_event_name": "Stop", "stop_hook_active": false, "session_id": "s1", "transcript_path": transcript}
		return hook(t, support.Env("JHON_SETTINGS="+path), "announce-stop", nil, payload)
	}
	reason := func(r support.Result) string {
		var j struct{ Decision, Reason string }
		if r.Code != 0 || r.Stderr != "" || json.Unmarshal([]byte(r.Stdout), &j) != nil || j.Decision != "block" {
			t.Fatalf("code %d stdout %q stderr %q; want a block", r.Code, r.Stdout, r.Stderr)
		}
		return j.Reason
	}
	const prompt = `{"type":"user","message":{"role":"user","content":"lee a.txt"}}`
	if got, want := reason(run(prompt,
		`{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"thinking","thinking":"x"}]}}`,
		`{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"Sigo con la lectura de a.txt."}]}}`)),
		`Jhon: you announced "Sigo con la lectura de a.txt." and ended the turn without doing it. Do it now, or say in one line why not.`; got != want {
		t.Errorf("announced: got %q; want %q", got, want)
	}
	if got, want := reason(run(prompt, `{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"thinking","thinking":"x"}]}}`)),
		"Jhon: your turn ended with an empty reply. Continue the work, or say in one line why you stopped."; got != want {
		t.Errorf("empty: got %q; want %q", got, want)
	}
	for why, r := range map[string]support.Result{
		"no transcript": run(),
		"malformed":     run(prompt, `{"type":"assistant","mess`),
	} {
		if r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
			t.Errorf("%s: code %d stdout %q stderr %q; want the turn to end", why, r.Code, r.Stdout, r.Stderr)
		}
	}
}
