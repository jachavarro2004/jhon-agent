package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyFixtureLines(t *testing.T) {
	for _, c := range []struct{ text, guard, class string }{
		{"safety-guard: refused: cat .env: it reads a file that holds credentials", "safety-guard", "credentials match"},
		{"secrets-guard: Read of src/.env matches a credentials file", "secrets-guard", "credentials match"},
		{"inspect-guard: command is outside this agent's Bash allowlist (git status, git diff)", "inspect-guard", "inspect allowlist"},
		{"inspect-guard: shell syntax is not allowed here: |", "inspect-guard", "inspect shell syntax"},
		{"scope-guard: Edit of src/b.ts is outside the allowed edit surfaces (src/a.ts)", "scope-guard", "scope surfaces"},
		{"scope-guard: refused: rm \"$d\" (a target the guard cannot resolve)", "scope-guard", "unresolved target"},
		{"safety-guard: refused: the line is unreadable: a word the reader cannot place", "safety-guard", "unreadable or unknown"},
		{"route-guard: this commit closes Slice 2 of c1; run the slice's tests first", "route-guard", "slice or delivery discipline"},
		{"route-guard: this change alters behaviour, and its RED before GREEN is not on record (LDD)", "route-guard", "slice or delivery discipline"},
		{"route-guard: Prism review is due before delivery (c1)", "route-guard", "review due"},
		{"route-guard: this change alters behaviour, and the branch carries no change with its record (LDD)", "route-guard", "no record"},
		{"vcs-guard: Write of src/.git/config is inside a VCS directory (.git); no one writes there", "vcs-guard", "VCS directory"},
		{"safety-guard: refused: an `until` or `while` loop that sleeps with nothing bounding it.", "safety-guard", "unbounded wait"},
		{"jhon hook pre-tool did not answer in 10s; the call is refused. Do not run the same call again", "hook-timeout", "hook timeout"},
		{"framework-guard: Edit of Jhon's own install is refused", "framework-guard", "other"},
		{"  \nsafety-guard: refused: git push --force: it rewrites history", "safety-guard", "other"},
	} {
		guard, class, ok := classify(c.text)
		if !ok || guard != c.guard || class != c.class {
			t.Errorf("classify(%q) = %q, %q, %v; want %q, %q, true", c.text, guard, class, ok, c.guard, c.class)
		}
	}
}

// hookPrefix is the older Claude Code hook error's lead: the runtime's
// prefix and the hook command in brackets, long enough to put the guard's
// name far past the first 90 characters. Its words (credentials, unknown)
// must not classify the stop.
func hookPrefix(tool string) string {
	return "PreToolUse:" + tool + " hook error: [/home/owner/.local/bin/jhon hook pre-tool --agent jhon-orchestrator safety-guard vcs-guard .git .hg .svn " +
		"framework-guard /home/owner/.agents/jhon /home/owner/.local/share/jhon/releases /home/owner/.local/share/jhon/current /home/owner/.local/bin/jhon " +
		"/home/owner/.claude/CLAUDE.md /home/owner/.config/credentials-unknown secrets-guard route-guard jhon-builder]: "
}

func TestClassifyTheOlderHookErrorPrefix(t *testing.T) {
	if n := len(hookPrefix("Bash")); n < 300 || n > 560 {
		t.Fatalf("the fixture prefix is %d characters, want 300..560", n)
	}
	for _, c := range []struct{ text, guard, class string }{
		{hookPrefix("Bash") + "safety-guard: refused: cat .env: it reads a file that holds credentials", "safety-guard", "credentials match"},
		{hookPrefix("Bash") + "route-guard: Prism review is due before delivery (c1)", "route-guard", "review due"},
		{hookPrefix("Write") + "vcs-guard: Write of x lands outside the project", "vcs-guard", "other"},
		{"PreToolUse:Edit hook error: ['/opt/scope-guard.mjs' '--agent' 'w']: scope-guard: Edit of src/b.ts is outside the allowed edit surfaces", "scope-guard", "scope surfaces"},
	} {
		guard, class, ok := classify(c.text)
		if !ok || guard != c.guard || class != c.class {
			t.Errorf("classify(%.60q…) = %q, %q, %v; want %q, %q, true", c.text, guard, class, ok, c.guard, c.class)
		}
	}
	for _, text := range []string{
		// The guard's name past the first 600 characters.
		"PreToolUse:Bash hook error: [" + strings.Repeat("z", 600) + "]: safety-guard: refused: x",
		// The prefix is not where the result begins.
		"grep: " + hookPrefix("Bash") + "safety-guard: refused: x",
	} {
		if guard, class, ok := classify(text); ok {
			t.Errorf("classify(%.60q…) = %q, %q, true; want no stop", text, guard, class)
		}
	}
}

func TestClassifyLeavesOutWhatOnlyQuotesAGuard(t *testing.T) {
	for _, text := range []string{
		"the file says the safety-guard refused it",                              // no colon after the name
		strings.Repeat("x", 80) + " route-guard: refused: x",                     // past the first 90 characters
		strings.Repeat("y", 100) + " did not answer in 10s; the call is refused", // the same for a timeout
		"safety-guards: a plural is no guard's name",
		"unsafety-guard: a longer word is no guard's name",
		"plain output with no guard",
		"",
	} {
		if guard, class, ok := classify(text); ok {
			t.Errorf("classify(%q) = %q, %q, true; want no stop", text, guard, class)
		}
	}
}

func TestLeftOutFolders(t *testing.T) {
	for folder, want := range map[string]string{
		"-home-owner-Projects-personal-jhon":           "jhon",
		"--home-owner-Projects-personal-jhon-studio--": "jhon",
		"-home-owner-Projects-JHON-agent":              "jhon",
		"-tmp-abc":                                     "scratch",
		"--tmp-x--":                                    "scratch",
		"-var-tmp-t269w-proj":                          "scratch",
		"-private-var-folders-xy-T-proj":               "scratch",
		"-home-owner-work-shop":                        "",
		"--home-owner-work-api--":                      "",
		"-home-owner-tmpfiles":                         "",
	} {
		if got := leftOut(folder); got != want {
			t.Errorf("leftOut(%q) = %q, want %q", folder, got, want)
		}
	}
}

// jsonl writes lines, each a JSON value, as one transcript file.
func jsonl(t *testing.T, path string, lines ...any) {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		j, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

type m = map[string]any

// ccResult is a Claude Code user line carrying tool results; a string is a
// result whose content is that string, a []string one whose content is
// text parts.
func ccResult(ts string, results ...any) m {
	var content []any
	for _, r := range results {
		c := m{"type": "tool_result", "tool_use_id": "t", "is_error": true}
		switch r := r.(type) {
		case string:
			c["content"] = r
		case []string:
			var parts []any
			for _, p := range r {
				parts = append(parts, m{"type": "text", "text": p})
			}
			c["content"] = parts
		}
		content = append(content, c)
	}
	return m{"type": "user", "timestamp": ts, "message": m{"role": "user", "content": content}}
}

func piResult(ts, text string) m {
	return m{"type": "message", "timestamp": ts, "message": m{"role": "toolResult", "toolCallId": "c", "toolName": "bash",
		"content": []any{m{"type": "text", "text": text}}, "isError": true}}
}

// entry is l with its entrypoint, as Claude Code writes it on an entry.
func entry(l m, entrypoint string) m {
	l["entrypoint"] = entrypoint
	return l
}

// frictionFixture is the two runtimes' transcripts: a client project's
// sessions (one with a sub-agent), Jhon's own development, a scratch
// folder, an old file, one pi session, the Claude Agent SDK's mirror of a
// pi session through the bridge, and a Claude Code CLI session holding an
// older hook error.
func frictionFixture(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	cc, pi := filepath.Join(dir, "claude"), filepath.Join(dir, "pi")
	shop := filepath.Join(cc, "-home-owner-work-shop")
	jsonl(t, filepath.Join(shop, "s1.jsonl"),
		m{"type": "assistant", "timestamp": "2026-11-03T09:59:00Z", "message": m{"role": "assistant", "content": []any{m{"type": "tool_use", "id": "t", "name": "Bash", "input": m{"command": "cat .env"}}}}},
		ccResult("2026-11-03T10:00:00Z", "file contents"),
		ccResult("2026-11-03T10:01:00Z", []string{"safety-guard: refused: cat .env: it reads a file that holds credentials"}),
		ccResult("2026-11-10T10:00:00Z", "ok", "route-guard: Prism review is due before delivery (c1)"),
		ccResult("2026-11-01T12:00:00Z", "safety-guard: refused: before the window"),
		m{"type": "user", "timestamp": "2026-11-10T11:00:00Z", "message": m{"role": "user", "content": "safety-guard: refused: the owner pasted this"}},
	)
	jsonl(t, filepath.Join(shop, "s1", "subagents", "agent-abc.jsonl"),
		ccResult("2026-11-10T12:00:00Z", "scope-guard: Edit of src/b.ts is outside the allowed edit surfaces (src/a.ts)"))
	old := filepath.Join(shop, "old.jsonl")
	jsonl(t, old, ccResult("2026-10-01T10:00:00Z", "safety-guard: refused: old"))
	if err := os.Chtimes(old, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	jsonl(t, filepath.Join(cc, "-home-owner-Projects-personal-jhon", "s2.jsonl"),
		ccResult("2026-11-10T10:00:00Z", "safety-guard: refused: x is unknown", "ok"))
	jsonl(t, filepath.Join(cc, "-tmp-scratch1", "s3.jsonl"),
		ccResult("2026-11-10T10:00:00Z", "vcs-guard: Write of x is inside a VCS directory (.git)"))
	ccAPI := filepath.Join(cc, "-home-owner-work-api")
	jsonl(t, filepath.Join(ccAPI, "c1.jsonl"),
		m{"type": "summary", "summary": "no entrypoint on this entry"},
		entry(ccResult("2026-11-18T10:00:00Z", hookPrefix("Bash")+"safety-guard: refused: cat .env: it reads a file that holds credentials"), "cli"))
	// The mirror: its first entry carries no entrypoint, the first that does
	// says sdk-ts, and that decides the file.
	jsonl(t, filepath.Join(ccAPI, "mirror.jsonl"),
		m{"type": "queue-operation", "timestamp": "2026-11-17T10:00:00Z"},
		entry(ccResult("2026-11-17T10:00:02Z", "jhon hook pre-tool did not answer in 10s; the call is refused. Do not run the same call again"), "sdk-ts"),
		entry(ccResult("2026-11-17T10:00:03Z", "listing"), "sdk-ts"),
		entry(ccResult("2026-11-17T10:00:04Z", "ok"), "cli"))
	api := filepath.Join(pi, "--home-owner-work-api--")
	jsonl(t, filepath.Join(api, "2026-11-17T10-00-00-000Z_p1.jsonl"),
		m{"type": "session", "version": 3, "id": "p1", "timestamp": "2026-11-17T10:00:00Z", "cwd": "/home/owner/work/api"},
		m{"type": "message", "timestamp": "2026-11-17T10:00:01Z", "message": m{"role": "assistant", "content": []any{m{"type": "text", "text": "safety-guard: refused: an assistant saying it"}}}},
		piResult("2026-11-17T10:00:02Z", "jhon hook pre-tool did not answer in 10s; the call is refused. Do not run the same call again"),
		piResult("2026-11-17T10:00:03Z", "listing"),
	)
	if err := os.WriteFile(filepath.Join(api, "2026-11-17T10-00-00-000Z_p1.meta.json"), []byte(`{"agentType":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"claude-code": cc, "pi": pi}
}

func TestCountFrictionOnFixtureTranscripts(t *testing.T) {
	since := time.Date(2026, 11, 2, 0, 0, 0, 0, time.Local)
	f, err := countFriction(frictionFixture(t), since)
	if err != nil {
		t.Fatal(err)
	}
	if f.Since != "2026-11-02" || f.Sessions != 4 || f.ToolResults != 8 || f.Stops != 5 || f.Per100 != 62.5 {
		t.Errorf("since %q, sessions %d, tool results %d, stops %d, per 100 %v; want 2026-11-02, 4, 8, 5, 62.5", f.Since, f.Sessions, f.ToolResults, f.Stops, f.Per100)
	}
	wantGuard := map[string]int{"safety-guard": 2, "route-guard": 1, "scope-guard": 1, "hook-timeout": 1}
	if len(f.PerGuard) != len(wantGuard) {
		t.Errorf("per guard %v, want %v", f.PerGuard, wantGuard)
	}
	for g, n := range wantGuard {
		if f.PerGuard[g] != n {
			t.Errorf("per guard %v, want %v", f.PerGuard, wantGuard)
		}
	}
	wantClass := map[string]ClassRate{"credentials match": {2, 25}, "review due": {1, 12.5}, "scope surfaces": {1, 12.5}, "hook timeout": {1, 12.5}}
	if len(f.PerClass) != len(wantClass) {
		t.Errorf("per class %v, want %v", f.PerClass, wantClass)
	}
	for c, r := range wantClass {
		if f.PerClass[c] != r {
			t.Errorf("per class %v, want %v", f.PerClass, wantClass)
		}
	}
	wantWeek := map[string]Count{"2026-W45": {0, 2, 1, 50}, "2026-W46": {0, 3, 2, 66.667}, "2026-W47": {0, 3, 2, 66.667}}
	if len(f.PerWeek) != len(wantWeek) {
		t.Errorf("per week %v, want %v", f.PerWeek, wantWeek)
	}
	for w, c := range wantWeek {
		if got := f.PerWeek[w]; got == nil || *got != c {
			t.Errorf("week %s = %+v, want %+v", w, got, c)
		}
	}
	wantRuntime := map[string]Count{"claude-code": {3, 6, 4, 66.667}, "pi": {1, 2, 1, 50}}
	for r, c := range wantRuntime {
		if got := f.PerRuntime[r]; got == nil || *got != c {
			t.Errorf("runtime %s = %+v, want %+v", r, got, c)
		}
	}
	wantOut := map[string]Count{"jhon": {1, 2, 1, 50}, "scratch": {1, 1, 1, 100}, "bridge_mirror": {1, 3, 1, 33.333}}
	for k, c := range wantOut {
		if got := f.LeftOut[k]; got == nil || *got != c {
			t.Errorf("left out %s = %+v, want %+v", k, got, c)
		}
	}
	// Privacy: counts only, no command text, no path, no project name.
	j, _ := json.Marshal(f)
	for _, bad := range []string{"shop", "owner", "api", "cat .env", "/", "src/", "Prism", "c1"} {
		if strings.Contains(string(j), bad) {
			t.Errorf("the friction record holds %q: %s", bad, j)
		}
	}
}
