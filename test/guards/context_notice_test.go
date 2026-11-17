// context-notice (plan item 8): a Stop hook on the main Jhon agents. When
// the last turn's context (input, cache-creation, and cache-read tokens of
// the transcript's last assistant usage) reaches a band (250K, then every
// 150K more, or JHON_CONTEXT_NOTICE=<first>,<step>), it prints a
// systemMessage once for that band per session: one session per change, so
// save a handoff and start a fresh one (/new in pi, /clear in Claude Code).
// It never blocks and never makes the model continue.

package guards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

type noticeSession struct{ dir, transcript string }

func newNoticeSession(t *testing.T) noticeSession {
	t.Helper()
	dir := realTemp(t)
	s := noticeSession{dir, filepath.Join(dir, "s.jsonl")}
	support.Write(t, dir, map[string]string{"s.jsonl": js(t, map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "go"}}) + "\n"})
	return s
}

// turn appends an assistant turn whose context is tokens.
func (s noticeSession) turn(t *testing.T, tokens int) {
	t.Helper()
	line := js(t, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "usage": map[string]any{
		"input_tokens": 10, "cache_creation_input_tokens": 1000, "cache_read_input_tokens": tokens - 1010, "output_tokens": 50,
	}}}) + "\n"
	f, err := os.OpenFile(s.transcript, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

type notice struct {
	code         int
	message, out string
}

func (s noticeSession) stop(t *testing.T, env []string, extra map[string]any) notice {
	t.Helper()
	payload := with(map[string]any{"hook_event_name": "Stop", "session_id": "s1", "transcript_path": s.transcript, "stop_hook_active": false})
	for k, v := range extra {
		payload[k] = v
	}
	r := hook(t, support.Env(append([]string{"XDG_CACHE_HOME=" + s.dir}, env...)...), "context-notice", nil, payload)
	n := notice{code: r.Code, out: r.Stdout}
	if strings.TrimSpace(r.Stdout) != "" {
		var j struct {
			SystemMessage string `json:"systemMessage"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &j); err != nil {
			t.Fatalf("stdout is not JSON: %q", r.Stdout)
		}
		n.message = j.SystemMessage
	}
	return n
}

func matches(t *testing.T, s, re, label string) {
	t.Helper()
	if !regexp.MustCompile(re).MatchString(s) {
		t.Errorf("%s: %q does not match /%s/", label, s, re)
	}
}

func TestContextNoticeBelowFirstBand(t *testing.T) {
	t.Parallel()
	s := newNoticeSession(t)
	s.turn(t, 120_000)
	r := s.stop(t, nil, nil)
	if r.code != 0 || r.out != "" {
		t.Fatalf("stop = %d, %q; want 0 and nothing", r.code, r.out)
	}
}

func TestContextNoticeOncePerBand(t *testing.T) {
	t.Parallel()
	s := newNoticeSession(t)
	s.turn(t, 262_000)
	first := s.stop(t, nil, nil)
	if first.code != 0 {
		t.Fatalf("code = %d", first.code)
	}
	matches(t, first.message, `262K tokens`, "first")
	matches(t, first.message, `jhon context <change> --phase none --handoff`, "first")
	matches(t, first.message, `/clear`, "first")
	// S35: one session per change; the notice names what to run in each runtime.
	matches(t, first.message, `One session per change`, "first")
	matches(t, first.message, `/new in pi`, "first")
	matches(t, first.message, `/clear or a restart in Claude Code`, "first")
	s.turn(t, 300_000)
	if out := s.stop(t, nil, nil).out; out != "" {
		t.Errorf("the same band is not repeated: %q", out)
	}
	s.turn(t, 410_000)
	matches(t, s.stop(t, nil, nil).message, `410K tokens`, "the next band notifies again")
}

func TestContextNoticeConfigurableAndSilentOnBadInput(t *testing.T) {
	t.Parallel()
	s := newNoticeSession(t)
	s.turn(t, 120_000)
	matches(t, s.stop(t, []string{"JHON_CONTEXT_NOTICE=100000,50000"}, nil).message, `120K tokens`, "configured bands")
	if c := s.stop(t, nil, map[string]any{"transcript_path": filepath.Join(s.dir, "missing.jsonl")}).code; c != 0 {
		t.Errorf("missing transcript: code %d", c)
	}
	bad := hook(t, support.Env("XDG_CACHE_HOME="+s.dir), "context-notice", nil, "not json")
	if bad.Code != 0 || bad.Stdout != "" {
		t.Errorf("bad payload = %d, %q; want 0 and nothing", bad.Code, bad.Stdout)
	}
}
