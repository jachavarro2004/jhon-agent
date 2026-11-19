// The stale-session report in `jhon doctor`, run as the binary: stale-session
// detection against the install record (docket AI1). `jhon sessions` folded
// into doctor (T157) and is an unknown command now. The install record, the
// guard note, and the report are unit-tested in internal/session.
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

// doctorSessions runs `jhon doctor` against the fixture sources and returns
// its sessions section: the lines after the section's head, up to the next
// blank line.
func doctorSessions(t *testing.T, home, path string) string {
	t.Helper()
	r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "doctor"},
		Env: []string{"HOME=" + home, "PATH=" + path + ":/usr/bin:/bin", "JHON_ROOT=" + support.Root(),
			"JHON_ENGRAM=" + support.NoEngram, "JHON_SETTINGS=" + filepath.Join(home, "settings.toml")}})
	_, section, ok := strings.Cut(r.Stdout, "\nsessions\n")
	if !ok {
		t.Fatalf("doctor printed no sessions section: exit %d\n%s", r.Code, r.Out())
	}
	section, _, _ = strings.Cut(section, "\n\n")
	return section
}

const installed = "3eb2e0b0000000000000000000000000000000aa"

func TestDoctorSessions(t *testing.T) {
	t.Run("jhon doctor warns of the stale orchestrator session from the fixture sources", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		now := time.Now()
		install(t, home, at(5, now), installed)
		transcript(t, home, "-a", "old-orch", at(30, now), at(0.1, now), "jhon-orchestrator")
		path := fakeClaude(t, []any{map[string]any{"pid": 1, "id": "x", "cwd": "/a", "kind": "background", "startedAt": millis(at(30, now)), "sessionId": "old-orch", "status": "busy"}})
		out := doctorSessions(t, home, path)
		match(t, out, `(?m)^  installed 3eb2e0b at `)
		match(t, out, `(?m)^  source: claude agents --json$`)
		match(t, out, `(?m)^  warn  stale  old-orch  background  busy  /a  started `)
		match(t, out, "(?m)^  warn  1 orchestrator session\\(s\\) started before the last install; resume each with `jhon start --resume <id>` to load the current contract and agents$")
	})

	t.Run("jhon doctor degrades to transcripts when claude fails, and says so", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		now := time.Now()
		install(t, home, at(5, now), installed)
		transcript(t, home, "-a", "old-orch", at(30, now), at(1, now), "jhon-orchestrator")
		out := doctorSessions(t, home, fakeClaude(t, nil))
		match(t, out, `(?m)^  source: transcripts$`)
		match(t, out, `(?m)^  .*transcripts only`)
		match(t, out, `(?m)^  warn  stale  old-orch  `)
	})

	t.Run("jhon doctor says when no orchestrator session is stale", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		now := time.Now()
		install(t, home, at(5, now), installed)
		transcript(t, home, "-a", "new-orch", at(1, now), at(0.1, now), "jhon-orchestrator")
		out := doctorSessions(t, home, fakeClaude(t, []any{map[string]any{"cwd": "/a", "kind": "background", "startedAt": millis(at(1, now)), "sessionId": "new-orch", "status": "busy"}}))
		match(t, out, `(?m)^  ok    no stale orchestrator session$`)
		noMatch(t, out, `stale  new-orch`)
	})

	t.Run("jhon sessions is an unknown command", func(t *testing.T) {
		t.Parallel()
		for _, args := range [][]string{{"sessions"}, {"sessions", "--json"}} {
			r := support.Run(t, support.Cmd{Args: append([]string{support.Bin(t)}, args...),
				Env: []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin", "JHON_ROOT=" + support.Root()}})
			equal(t, r.Code, 1, args)
			equal(t, r.Stdout, "", args)
			match(t, r.Stderr, `^jhon: unknown command: sessions\n\n`)
		}
	})
}
