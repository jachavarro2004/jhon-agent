// `jhon sessions [--json]`, run as the binary: stale-session detection
// against the install record (docket AI1). The install record, the guard
// note, and the report are unit-tested in internal/session.
//
// Every source is a fixture built here under a temporary directory: a HOME
// with `.claude/projects/<project>/<session>.jsonl` transcripts and
// `.cache/jhon/install.json`, and a fake `claude` on PATH for
// `claude agents --json`. Nothing reads the real ~/.claude/projects.
package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// at is the instant hoursAgo before now, as toISOString writes it.
func at(hoursAgo float64, now time.Time) string {
	return now.Add(-time.Duration(hoursAgo * float64(time.Hour))).UTC().Format("2006-01-02T15:04:05.000Z")
}

func millis(iso string) int64 {
	tm, _ := time.Parse(time.RFC3339Nano, iso)
	return tm.UnixMilli()
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// install writes the install record into home.
func install(t *testing.T, home, installedAt, commit string) {
	t.Helper()
	support.Write(t, home, map[string]string{".cache/jhon/install.json": jsonOf(t, map[string]string{"installed_at": installedAt, "commit": commit}) + "\n"})
}

// transcript is a transcript as Claude Code 2.1.280 writes it: bookkeeping
// lines with no timestamp first, then conversation lines. agent is the
// agent-setting.
func transcript(t *testing.T, home, project, id, first, last, agent string) {
	t.Helper()
	setting := map[string]any{"type": "agent-setting", "agentSetting": agent, "sessionId": id}
	lines := []any{map[string]any{"type": "custom-title", "customTitle": "T", "sessionId": id}}
	if agent != "" {
		lines = append(lines, setting)
	}
	lines = append(lines,
		map[string]any{"type": "mode", "mode": "normal", "sessionId": id},
		map[string]any{"type": "user", "timestamp": first, "sessionId": id, "message": map[string]any{"role": "user", "content": "hi"}},
		map[string]any{"type": "assistant", "timestamp": last, "sessionId": id, "message": map[string]any{"role": "assistant", "content": []any{}}})
	if agent != "" {
		lines = append(lines, setting)
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(jsonOf(t, l) + "\n")
	}
	support.Write(t, home, map[string]string{filepath.Join(".claude", "projects", project, id+".jsonl"): b.String()})
}

// fakeClaude is a directory holding a fake `claude` that prints out, or exits
// 1 when out is nil.
func fakeClaude(t *testing.T, out []any) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'agents: broken' >&2\nexit 1\n"
	if out != nil {
		script = "#!/bin/sh\n[ \"$1\" = agents ] && [ \"$2\" = --json ] || exit 64\ncat <<'EOF'\n" + jsonOf(t, out) + "\nEOF\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func sessions(t *testing.T, home, path string, args ...string) support.Result {
	t.Helper()
	return support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "sessions"}, args...),
		Env: []string{"HOME=" + home, "PATH=" + path + ":/usr/bin:/bin", "JHON_ROOT=" + support.Root()}})
}

func staleIDs(t *testing.T, out string) (map[string]any, []string) {
	t.Helper()
	var j map[string]any
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	ids := []string{}
	stale, _ := j["stale"].([]any)
	for _, s := range stale {
		ids = append(ids, s.(map[string]any)["session_id"].(string))
	}
	return j, ids
}

const installed = "3eb2e0b0000000000000000000000000000000aa"

func TestSessions(t *testing.T) {
	t.Run("jhon sessions --json reports the stale orchestrator session from the fixture sources", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		now := time.Now()
		install(t, home, at(5, now), installed)
		transcript(t, home, "-a", "old-orch", at(30, now), at(0.1, now), "jhon-orchestrator")
		path := fakeClaude(t, []any{map[string]any{"pid": 1, "id": "x", "cwd": "/a", "kind": "background", "startedAt": millis(at(30, now)), "sessionId": "old-orch", "status": "busy"}})
		r := sessions(t, home, path, "--json")
		if r.Code != 0 {
			t.Fatal(r.Stderr)
		}
		j, ids := staleIDs(t, r.Stdout)
		equal(t, j["source"], any("claude-agents"))
		equal(t, ids, []string{"old-orch"})
		match(t, sessions(t, home, path).Stdout, "(?m)^1 orchestrator session\\(s\\) started before the last install; resume each with `jhon start --resume <id>` to load the current contract and agents$")
	})

	t.Run("jhon sessions degrades to transcripts when claude fails, and says so", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		now := time.Now()
		install(t, home, at(5, now), installed)
		transcript(t, home, "-a", "old-orch", at(30, now), at(1, now), "jhon-orchestrator")
		r := sessions(t, home, fakeClaude(t, nil), "--json")
		if r.Code != 0 {
			t.Fatal(r.Stderr)
		}
		j, ids := staleIDs(t, r.Stdout)
		equal(t, j["source"], any("transcripts"))
		note, _ := j["note"].(string)
		match(t, note, `transcripts only`)
		equal(t, ids, []string{"old-orch"})
	})

	t.Run("jhon sessions prints the stale list for a human, and refuses an unknown argument", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		now := time.Now()
		install(t, home, at(5, now), installed)
		transcript(t, home, "-a", "old-orch", at(30, now), at(0.1, now), "jhon-orchestrator")
		path := fakeClaude(t, []any{map[string]any{"cwd": "/a", "kind": "background", "startedAt": millis(at(30, now)), "sessionId": "old-orch", "status": "busy"}})
		r := sessions(t, home, path)
		if r.Code != 0 {
			t.Fatal(r.Stderr)
		}
		match(t, r.Stdout, `installed 3eb2e0b at `)
		match(t, r.Stdout, `stale\s+old-orch\s+background\s+busy\s+/a`)
		match(t, r.Stdout, `resume`)
		equal(t, sessions(t, home, path, "--bogus").Code, 2)
	})
}
