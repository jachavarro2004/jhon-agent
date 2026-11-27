package main

import (
	"encoding/json"
	"fmt"
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

// The classes T270 named among the stops that were "other": each from an
// invented refusal of the same shape, keyed on the guard's own wording.
func TestClassifyTheClassesNamedFromOther(t *testing.T) {
	for _, c := range []struct{ text, guard, class string }{
		{"safety-guard: refused: rm -r $(…): its target cannot be resolved. This is destructive or irreversible, and never granted to an agent.", "safety-guard", "unresolved target"},
		// An unknown verdict keeps its class, whatever its reason says.
		{"safety-guard: unknown: > $out/acme.json: its target cannot be resolved; it is refused outside a prompt the owner can answer", "safety-guard", "unreadable or unknown"},
		{"route-guard: this change alters behaviour, and no passing light verify of the tree its last run tested is on record (LDD): launch the verify agent", "route-guard", "light verify"},
		{"route-guard: the change acme-orders delivers in members of the workspace too, one PR per repository, the primary's last: this PR's body names no pull request of member web", "route-guard", "workspace members' PRs"},
		{"route-guard: delivery: change opens one PR per change, after its last slice: acme-orders has 2 unchecked task(s) (3.1 RED)", "route-guard", "before the last slice"},
		{"route-guard: acme-orders: delivery: change in openspec/config.yaml merges the change once, after its last slice; 1 task(s) of tasks.md are unchecked", "route-guard", "before the last slice"},
		{"route-guard: delivery: change merges with rebase, so each slice stays its own commit: run `gh pr merge <n> --rebase --delete-branch`.", "route-guard", "merge method"},
		{"safety-guard: ./scripts/ship.sh: aws s3 sync: deploy needs the owner's authorization. Ask the owner; they grant it by running `! jhon grant deploy`", "safety-guard", "owner's grant"},
		{"safety-guard: > src/report.txt: outside-write needs the owner's authorization. Ask the owner", "safety-guard", "owner's grant"},
		{"safety-guard: refused: rm /srv/x: it is outside the project and the scratch roots (/tmp, $TMPDIR, ~/.cache). This is destructive or irreversible, and never granted to an agent.", "safety-guard", "outside the project"},
		{"safety-guard: refused: git push --force: it overwrites remote history. This is destructive or irreversible, and never granted to an agent. Stop and report this refusal verbatim", "safety-guard", "hard line"},
		{"framework-guard: `git log --oneline a..b` runs git on the protected path /opt/acme-tool: no Jhon agent writes the framework's source", "framework-guard", "framework path"},
		{"framework-guard: Edit of /opt/acme-tool/x is under the protected path /opt/acme-tool: no Jhon agent writes it", "framework-guard", "framework path"},
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

// A command the sandbox stopped is the guard "sandbox": its note on a
// refused write, a line saying Read-only file system as a program prints
// it, the shell's refusal with the mode on where the sandbox cannot run, a
// sandbox that did not start, and docker with its socket hidden.
func TestClassifySandboxStops(t *testing.T) {
	for _, c := range []struct{ text, class string }{
		{"mkdir: cannot create directory ‘/home/owner/acme-out’: Read-only file system\nthe sandbox refused a write to /home/owner/acme-out, which does not exist; no folder the sandbox opens for writing holds it: sandbox.allow_write opens it, or a folder that holds it, from the next command (jhon settings set sandbox.allow_write <folder>,…); that is a stop: tell the owner the path and why, never write it elsewhere\n\nCommand exited with code 1", "allow_write opens it"},
		{"touch: cannot touch '/home/owner/.ssh/x': Read-only file system\nthe sandbox refused a write to /home/owner/.ssh/x; it is in /home/owner/.ssh, which the sandbox hides (credentials, session sockets, snapshots): no setting opens it; that is a stop: tell the owner the path and why, never write it elsewhere", "no setting opens it"},
		{"cp: cannot create regular file '/home/owner/acme/x': Read-only file system\nthe sandbox refused a write to /home/owner/acme/x; that is a stop: tell the owner the path (jhon settings sandbox.allow_write), never write it elsewhere", "refused write"},
		{"Fetching acme\nfatal: unable to create '/home/owner/acme/.git/index.lock': Read-only file system\nok", "read-only, no note"},
		{"error: Could not create temporary file\n  cause: Read-only file system (os error 30) at path \"/home/owner/.local/share/acme/.tmp1\"", "read-only, no note"},
		{"OSError: [Errno 30] Read-only file system: '/home/owner/acme.lock'", "read-only, no note"},
		{"jhon: sandbox.mode is on and this session is in no git repository, so there is no project to pin and the command is refused, never run unpinned. Fix: start the session inside a repository", "mode on, cannot run"},
		{"jhon: `jhon sandbox status --json` gave no answer (timeout), so whether this command runs in the sandbox is unknown; the command is refused", "mode on, cannot run"},
		{"jhon sandbox: bubblewrap refused the sandbox because a folder changed while it mounted it, so nothing was run; fix: run the command again", "did not start"},
		{"failed to connect to the docker API at unix:///var/run/docker.sock\njhon sandbox: docker cannot reach its daemon from inside the sandbox, which hides the docker socket (sandbox.docker is off)", "docker hidden"},
	} {
		guard, class, ok := classify(c.text)
		if !ok || guard != "sandbox" || class != c.class {
			t.Errorf("classify(%.70q…) = %q, %q, %v; want sandbox, %q, true", c.text, guard, class, ok, c.class)
		}
	}
	// What only quotes the sandbox is no stop: a help text, a report, a
	// question, Jhon's own queued save, a status line.
	for _, text := range []string{
		"  A failure whose output says a path is read-only\n  (Read-only file system) ends with one line on\n  stderr, the sandbox refused a write to <path>; that is a stop",
		"The push failed (`git push` said: `refs/acme.lock: Read-only file system`), so the owner runs it.",
		"The folder is outside the project and git fetch failed with 'Read-only file system'. Open it with sandbox.allow_write?",
		"jhon: engram save queued (open /home/owner/.engram/.instance-id.lock: read-only file system); the verify stands",
		"sandbox on; this command runs inside Jhon's sandbox, where another cannot start, so nothing was probed",
		"sandbox on; it cannot run here: bubblewrap cannot start a sandbox (bwrap: setting up uid map: Read-only file system); fix: turn unprivileged user namespaces on",
		"the notes say jhon: sandbox.mode is on and the sandbox cannot run here",
	} {
		if guard, class, ok := classify(text); ok {
			t.Errorf("classify(%.70q…) = %q, %q, true; want no stop", text, guard, class)
		}
	}
	// A guard's stop stays the guard's, whatever it quotes.
	if guard, _, _ := classify("safety-guard: refused: rm /x: Read-only file system"); guard != "safety-guard" {
		t.Errorf("a guard's stop quoting the kernel's message went to %q", guard)
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

// lastID numbers the fixtures' tool calls, so that each has an id of its own
// unless a fixture names one (withID).
var lastID int

func newID() string {
	lastID++
	return fmt.Sprintf("toolu_%04d", lastID)
}

// withID is a result with the tool call id given: the copy of a result
// another transcript holds.
type withID struct{ id, text string }

// ccResult is a Claude Code user line carrying tool results; a string is a
// result whose content is that string, a []string one whose content is
// text parts, a withID a string with its id.
func ccResult(ts string, results ...any) m {
	var content []any
	for _, r := range results {
		c := m{"type": "tool_result", "tool_use_id": newID(), "is_error": true}
		switch r := r.(type) {
		case string:
			c["content"] = r
		case withID:
			c["tool_use_id"], c["content"] = r.id, r.text
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

func piResult(ts, text string) m { return piResultID(ts, newID(), text) }

func piResultID(ts, id, text string) m {
	return m{"type": "message", "timestamp": ts, "message": m{"role": "toolResult", "toolCallId": id, "toolName": "bash",
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

// Each tool call is counted once (L5's double count, T270): a Claude Code
// transcript whose first entrypoint says cli and whose later entries say
// sdk-ts holds the bridge's copies of a pi session's results, and a resumed
// Claude Code session copies the results of the one it resumes. pi is read
// first; a copy of a pi result is a bridge copy, any other repeat a repeat.
func TestCountFrictionCountsEachToolResultOnce(t *testing.T) {
	dir := t.TempDir()
	cc, pi := filepath.Join(dir, "claude"), filepath.Join(dir, "pi")
	stop := "route-guard: Prism review is due before delivery (c1)"
	jsonl(t, filepath.Join(pi, "--home-owner-work-acme--", "2026-11-17T10-00-00-000Z_p1.jsonl"),
		m{"type": "session", "version": 3, "id": "p1", "timestamp": "2026-11-17T10:00:00Z", "cwd": "/home/owner/work/acme"},
		piResultID("2026-11-17T10:00:01Z", "toolu_pi1", stop),
		piResultID("2026-11-17T10:00:02Z", "toolu_pi2", "listing"),
		piResultID("2026-11-17T10:00:03Z", "toolu_pi3", "ok"),
	)
	acme := filepath.Join(cc, "-home-owner-work-acme")
	// The bridge's copy that the entrypoint rule misses: cli first.
	jsonl(t, filepath.Join(acme, "bridge.jsonl"),
		entry(ccResult("2026-11-17T10:00:01Z", withID{"toolu_pi1", stop}), "cli"),
		entry(ccResult("2026-11-17T10:00:02Z", withID{"toolu_pi2", "listing"}), "sdk-ts"),
		entry(ccResult("2026-11-17T10:00:03Z", withID{"toolu_pi3", "ok"}), "sdk-ts"))
	// A Claude Code session, and the session that resumed it, which copies
	// its results before its own.
	jsonl(t, filepath.Join(acme, "a-first.jsonl"),
		entry(ccResult("2026-11-18T10:00:00Z", withID{"toolu_cc1", stop}), "cli"),
		entry(ccResult("2026-11-18T10:00:01Z", withID{"toolu_cc2", "ok"}), "cli"))
	jsonl(t, filepath.Join(acme, "b-resumed.jsonl"),
		entry(ccResult("2026-11-18T10:00:00Z", withID{"toolu_cc1", stop}), "cli"),
		entry(ccResult("2026-11-18T10:00:01Z", withID{"toolu_cc2", "ok"}), "cli"),
		entry(ccResult("2026-11-19T10:00:00Z", withID{"toolu_cc3", stop}), "cli"))
	f, err := countFriction(map[string]string{"claude-code": cc, "pi": pi}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if f.Counting != frictionCounting || f.Sessions != 3 || f.ToolResults != 6 || f.Stops != 3 {
		t.Errorf("counting %d, sessions %d, tool results %d, stops %d; want %d, 3, 6, 3", f.Counting, f.Sessions, f.ToolResults, f.Stops, frictionCounting)
	}
	for k, want := range map[string]Count{"pi": {1, 3, 1, 33.333}, "claude-code": {2, 3, 2, 66.667}} {
		if got := f.PerRuntime[k]; got == nil || *got != want {
			t.Errorf("runtime %s = %+v, want %+v", k, got, want)
		}
	}
	for k, want := range map[string]Count{"bridge_copy": {1, 3, 1, 33.333}, "repeated": {0, 2, 1, 50}, "bridge_mirror": {}} {
		if got := f.LeftOut[k]; got == nil || *got != want {
			t.Errorf("left out %s = %+v, want %+v", k, got, want)
		}
	}
	if f.PerClass["review due"].Stops != 3 || f.PerGuard["route-guard"] != 3 {
		t.Errorf("per class %v, per guard %v; want 3 review-due stops by route-guard", f.PerClass, f.PerGuard)
	}
}
