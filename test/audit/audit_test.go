// `jhon audit <session-id|path> [--json | --html] [--context]` through the
// binary: exit codes, the human and --json output, and target resolution.
// The checks and measurements themselves are unit-tested in internal/audit.
//
// Each directory under test/testdata/audit/ is one Claude Code session: the
// main transcript `<session>.jsonl` and `<session>/subagents/agent-<id>.jsonl`
// with its `.meta.json`. sessionWith() builds a synthetic one.
package audit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// jhonEnv is this process's environment with overrides, and the repository
// as the binary's runtime.
func jhonEnv(overrides ...string) []string {
	return support.Env(append([]string{"JHON_ROOT=" + support.Root()}, overrides...)...)
}

// mainOf is the main transcript of a fixture directory.
func mainOf(t *testing.T, dir string) string {
	t.Helper()
	es, err := os.ReadDir(support.Fixture("audit", dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			return support.Fixture("audit", dir, e.Name())
		}
	}
	t.Fatalf("no main transcript in %s", dir)
	return ""
}

func audit(t *testing.T, args []string, env ...string) support.Result {
	t.Helper()
	return support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "audit"}, args...), Env: jhonEnv(env...)})
}

// js is v as JSON, with <, >, and & as they are.
func js(t testing.TB, v any) string {
	t.Helper()
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func parse(t testing.TB, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("%v\n%s", err, s)
	}
	return v
}

// get walks v by keys and indexes.
func get(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			a, _ := v.([]any)
			if k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

// isNull fails unless m holds key with the value null.
func isNull(t testing.TB, m map[string]any, key string, msg ...any) {
	t.Helper()
	if v, ok := m[key]; !ok || v != nil {
		t.Fatalf("%s is %#v (present %v), want null %v", key, v, ok, msg)
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func match(t testing.TB, text, re string) {
	t.Helper()
	if !regexp.MustCompile(re).MatchString(text) {
		t.Fatalf("no match for %s in:\n%s", re, text)
	}
}

func noMatch(t testing.TB, text, re string) {
	t.Helper()
	if regexp.MustCompile(re).MatchString(text) {
		t.Fatalf("unexpected match for %s in:\n%s", re, text)
	}
}

func equal[T any](t testing.TB, got, want T, msg ...any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v %v", got, want, msg)
	}
}

// ── synthetic sessions ──────────────────────────────────────────────────────

type usage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}

// call is one tool call of a sub-agent; an empty result is "ok".
type call struct {
	name   string
	input  map[string]any
	result string
	usage  *usage
}

// A step is a userStep, a rawStep, or a delegation.
type step any

type userStep string

type rawStep map[string]any

// delegation is a sub-agent's launch, transcript, and completion. change is
// nil or a change name; at and span, when set, put its lines on their own clock.
type delegation struct {
	id, typ        string
	phase          string // the launch block's phase; "none" when empty
	change         any
	prompt, result string
	calls          []call
	at, span       *int
}

func ptr(n int) *int { return &n }

const (
	session = "66666666-0000-4000-8000-000000000001"
	root    = "/work/p"
)

var startAt = time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)

func iso(s int) string {
	return startAt.Add(time.Duration(s) * time.Second).Format("2006-01-02T15:04:05.000Z")
}

func with(base map[string]any, extra ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range append([]map[string]any{base}, extra...) {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// contextBlock is the jhon-context block, keys in its written order.
type contextBlock struct {
	ContextID   string `json:"context_id"`
	ResolvedAt  string `json:"resolved_at"`
	Head        string `json:"head"`
	ProjectRoot string `json:"project_root"`
	Change      any    `json:"change"`
	Phase       string `json:"phase"`
	VCS         struct {
		State  string `json:"state"`
		Reason string `json:"reason"`
	} `json:"vcs"`
}

// sessionWith is a session built from steps, in main-thread order: a genuine
// user message, a delegation (its launch, its sub-agent transcript, and its
// completion notification), or a raw line. Every line advances a clock by
// one second; a delegation with at writes its sub-agent lines on their own
// clock from second at, its last line at at + span.
func sessionWith(t *testing.T, steps ...step) string {
	t.Helper()
	dir := t.TempDir()
	subdir := filepath.Join(dir, session, "subagents")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	common := map[string]any{"userType": "external", "cwd": root, "sessionId": session, "version": "2.1.280"}
	var main []any
	clock, n := 0, 0
	ts := func() string { s := iso(clock); clock++; return s }
	line := func(typ string, content any, extra map[string]any) {
		n++
		main = append(main, with(map[string]any{"parentUuid": nil, "isSidechain": false, "type": typ,
			"message": map[string]any{"role": typ, "content": content}, "uuid": fmt.Sprintf("m-%d", n), "timestamp": ts()}, common, extra))
	}
	jsonl := func(lines []any) string {
		var b strings.Builder
		for _, l := range lines {
			b.WriteString(js(t, l) + "\n")
		}
		return b.String()
	}
	for _, s := range steps {
		switch s := s.(type) {
		case userStep:
			line("user", string(s), map[string]any{"origin": map[string]any{"kind": "human"}})
		case rawStep:
			main = append(main, map[string]any(s))
		case delegation:
			id := s.id
			block := contextBlock{ContextID: "c0ffee00-" + id, ResolvedAt: iso(clock), Head: "0123456789abcdef0123456789abcdef01234567",
				ProjectRoot: root, Change: s.change, Phase: "none"}
			if s.phase != "" {
				block.Phase = s.phase
			}
			block.VCS.State, block.VCS.Reason = "ready", "HEAD resolves to 0123456789ab"
			var pretty bytes.Buffer
			e := json.NewEncoder(&pretty)
			e.SetEscapeHTML(false)
			e.SetIndent("", "  ")
			if err := e.Encode(block); err != nil {
				t.Fatal(err)
			}
			task := s.prompt
			if task == "" {
				task = "<task elided>"
			}
			prompt := "```jhon-context\n" + strings.TrimSuffix(pretty.String(), "\n") + "\n```\n\n" + task
			line("assistant", []any{map[string]any{"type": "tool_use", "id": "toolu_" + id, "name": "Agent",
				"input": map[string]any{"description": "Task", "subagent_type": s.typ, "prompt": prompt}}}, nil)
			line("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_" + id, "content": "Async agent launched successfully."}}, nil)
			var own *int
			if s.at != nil {
				own = ptr(*s.at)
			}
			sts := func() string {
				if own == nil {
					return ts()
				}
				v := iso(*own)
				*own++
				return v
			}
			sc := with(map[string]any{"parentUuid": nil, "isSidechain": true, "agentId": id}, common)
			sl := []any{with(sc, map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": prompt}, "uuid": id + "-0", "timestamp": sts()})}
			for i, c := range s.calls {
				cid := fmt.Sprintf("toolu_%s_%d", id, i)
				msg := map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": cid, "name": c.name, "input": c.input}}}
				if c.usage != nil {
					msg["id"], msg["usage"] = "msg_"+cid, c.usage
				}
				result := c.result
				if result == "" {
					result = "ok"
				}
				sl = append(sl, with(sc, map[string]any{"type": "assistant", "message": msg, "uuid": fmt.Sprintf("%s-a%d", id, i), "timestamp": sts()}))
				sl = append(sl, with(sc, map[string]any{"type": "user", "message": map[string]any{"role": "user",
					"content": []any{map[string]any{"type": "tool_result", "tool_use_id": cid, "content": result}}}, "uuid": fmt.Sprintf("%s-u%d", id, i), "timestamp": sts()}))
			}
			result := s.result
			if result == "" {
				result = "**Status**: success"
			}
			var end string
			if s.at != nil && s.span != nil {
				end = iso(*s.at + *s.span)
			} else {
				end = sts()
			}
			sl = append(sl, with(sc, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
				"content": []any{map[string]any{"type": "text", "text": result}}}, "uuid": id + "-end", "timestamp": end}))
			support.Write(t, subdir, map[string]string{
				"agent-" + id + ".jsonl":     jsonl(sl),
				"agent-" + id + ".meta.json": js(t, map[string]any{"agentType": s.typ, "description": "Task", "toolUseId": "toolu_" + id}),
			})
			line("user", "<task-notification>\n<task-id>"+id+"</task-id>\n<tool-use-id>toolu_"+id+"</tool-use-id>\n<status>completed</status>\n"+
				"<summary>Agent finished</summary>\n<result>"+result+"</result>\n</task-notification>", map[string]any{"origin": map[string]any{"kind": "task-notification"}})
		default:
			t.Fatalf("unknown step %T", s)
		}
	}
	file := filepath.Join(dir, session+".jsonl")
	support.Write(t, dir, map[string]string{session + ".jsonl": jsonl(main)})
	return file
}

const x = "add-response-timing"

var push = call{name: "Bash", input: map[string]any{"command": "git push origin main"},
	result: "To github.com:o/r.git\n   0000000.." + strings.Repeat("1", 7) + "  main -> main"}

// topKeys is the keys of a JSON object, in order.
func topKeys(t *testing.T, s string) []string {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(s))
	if tok, err := d.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("not an object: %v", err)
	}
	var keys []string
	for d.More() {
		tok, err := d.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := d.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

const shaped = "status: success\nsummary: added the timeout\nfiles_changed:\n  - src/a.ts: timeout\nvalidation:\n  - npm test: 3 passed\nrisks: none"

func use(input, create, read int) *usage {
	return &usage{InputTokens: input, CacheCreationInputTokens: create, CacheReadInputTokens: read, OutputTokens: 10}
}

func TestAudit(t *testing.T) {
	t.Run("nothing-to-audit exits 0 and says so, in human and --json output", func(t *testing.T) {
		t.Parallel()
		out := audit(t, []string{mainOf(t, "nothing-to-audit")})
		equal(t, out.Code, 0, out.Stdout)
		equal(t, lastLine(out.Stdout), "nothing-to-audit")
		j := parse(t, audit(t, []string{mainOf(t, "nothing-to-audit"), "--json"}).Stdout)
		equal(t, j["ok"], any(true))
		equal(t, j["status"], any("nothing-to-audit"))
	})

	t.Run("the human header states sessionKind and agentSetting as recorded", func(t *testing.T) {
		t.Parallel()
		main := sessionWith(t, rawStep{"type": "agent-setting", "agentSetting": "jhon-orchestrator", "sessionId": session}, userStep("go"),
			delegation{id: "s01", typ: "general"})
		match(t, audit(t, []string{main}).Stdout, `(?m)^  session  sessionKind: unknown  agentSetting: jhon-orchestrator$`)
	})

	t.Run("--json through a pipe is complete when the output is larger than one pipe buffer", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		var b strings.Builder
		for i := range 3000 {
			fmt.Fprintf(&b, "not json %d\n", i)
		}
		support.Write(t, dir, map[string]string{"large-output.jsonl": b.String()})
		r := audit(t, []string{filepath.Join(dir, "large-output.jsonl"), "--json"})
		if len(r.Stdout) <= 65536 {
			t.Fatalf("output is %d bytes", len(r.Stdout))
		}
		invalid := 0
		for _, e := range get(parse(t, r.Stdout), "errors").([]any) {
			if regexp.MustCompile(`not valid JSON`).MatchString(get(e, "message").(string)) {
				invalid++
			}
		}
		equal(t, invalid, 3000)
		equal(t, r.Code, 2)
	})

	t.Run("outcomes: --json carries outcomes and the human output a short table before the closing status line", func(t *testing.T) {
		t.Parallel()
		main := sessionWith(t, userStep("go"), delegation{id: "o01", typ: "worker", phase: "apply", change: x, at: ptr(0), span: ptr(5400)})
		j := parse(t, audit(t, []string{main, "--json"}).Stdout)
		equal(t, get(j, "outcomes", "changes", 0, "change"), any(x))
		out := audit(t, []string{main}).Stdout
		match(t, out, `(?m)^  outcomes +change +elapsed +active +agent-time +main-time +main-files +launches +verify +replans +relaunches +tests$`)
		match(t, out, `(?m)^    `+x+` +1\.5h +1\.5h +1\.5h +0s +0 +1 +0 +0 +0 +0$`)
		equal(t, lastLine(out), "clean")
		isNull(t, parse(t, audit(t, []string{mainOf(t, "missing-field"), "--json"}).Stdout), "outcomes")
	})

	t.Run("jhon audit shows the validation status after the check id, in human and --json output", func(t *testing.T) {
		t.Parallel()
		match(t, audit(t, []string{mainOf(t, "apply-worktree-bypass")}).Stdout, `violation\s+isolation-bypass \[verified\]\s+\S+\.jsonl:\d+`)
		j := parse(t, audit(t, []string{mainOf(t, "apply-worktree-bypass"), "--json"}).Stdout)
		n := 0
		for _, f := range get(j, "findings").([]any) {
			if get(f, "check") == "isolation-bypass" {
				n++
				equal(t, get(f, "validationStatus"), any("verified"))
			}
		}
		if n == 0 {
			t.Fatal("no isolation-bypass finding")
		}
	})

	t.Run("a pending check's would-be violation exits 0 and the human output marks it", func(t *testing.T) {
		t.Parallel()
		out := audit(t, []string{mainOf(t, "refusal-allowed-form")})
		equal(t, out.Code, 0, out.Stdout)
		match(t, out.Stdout, `suspect\s+refusal-bypass \[pending-real-fixture\].*would-be violation`)
	})

	// delivery/tested-revision is verified (real fixtures both ways), so a
	// delivery with no passing test run is a violation and fails the audit.
	t.Run("a delivery with no passing test run is a verified violation and exits 1", func(t *testing.T) {
		t.Parallel()
		main := sessionWith(t, userStep("go"), delegation{id: "d01", typ: "general", change: x, calls: []call{push}})
		out := audit(t, []string{main})
		equal(t, out.Code, 1, out.Stdout)
		match(t, out.Stdout, `violation\s+delivered-untested \[verified\]`)
	})

	t.Run("jhon audit exits 0 when clean or suspect-only, 1 on a verified check's violation, and 2 on an audit-error", func(t *testing.T) {
		t.Parallel()
		for fixture, code := range map[string]int{"clean": 0, "refusal-allowed-form": 0, "apply-worktree-bypass": 1, "missing-field": 2} {
			equal(t, audit(t, []string{mainOf(t, fixture)}).Code, code, fixture)
		}
	})

	t.Run("jhon audit --json prints ok, status, version, session, findings, errors, notes, context_use, outcomes, efficiency, authorizations, worked, integrations, timing, and dialogue", func(t *testing.T) {
		t.Parallel()
		r := audit(t, []string{mainOf(t, "apply-worktree-bypass"), "--json"})
		equal(t, r.Code, 1, r.Stderr)
		equal(t, topKeys(t, r.Stdout), []string{"ok", "status", "version", "session", "findings", "errors", "notes", "context_use", "outcomes", "efficiency", "authorizations", "worked", "integrations", "timing", "dialogue"})
		j := parse(t, r.Stdout)
		equal(t, j["ok"], any(false))
		equal(t, j["version"], any("2.1.280"))
	})

	t.Run("jhon audit resolves a session id under ~/.claude/projects", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		proj := filepath.Join(home, ".claude", "projects", "-work-p")
		if err := os.CopyFS(proj, os.DirFS(support.Fixture("audit", "apply-worktree-bypass"))); err != nil {
			t.Fatal(err)
		}
		id := strings.TrimSuffix(filepath.Base(mainOf(t, "apply-worktree-bypass")), ".jsonl")
		r := audit(t, []string{id, "--json"}, "HOME="+home)
		equal(t, r.Code, 1, r.Stderr)
		found := false
		for _, f := range get(parse(t, r.Stdout), "findings").([]any) {
			found = found || get(f, "check") == "isolation-bypass"
		}
		if !found {
			t.Fatal(r.Stdout)
		}
	})

	// jhon audit --all: every session since a date, audited and aggregated.
	t.Run("jhon audit --all aggregates every recent session's findings", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		for _, fx := range []string{"background-run-delivered", "background-run-failed-delivered"} {
			if err := os.CopyFS(filepath.Join(home, ".claude", "projects", "-work-"+fx), os.DirFS(support.Fixture("audit", fx))); err != nil {
				t.Fatal(err)
			}
		}
		r := audit(t, []string{"--all", "--json"}, "HOME="+home)
		equal(t, r.Code, 0, r.Stderr)
		j := parse(t, r.Stdout)
		equal(t, get(j, "sessions"), any(2.0))
		equal(t, get(get(get(j, "findings"), "delivered-untested"), "violation"), any(1.0))
		human := audit(t, []string{"--all"}, "HOME="+home)
		match(t, human.Stdout, `2 session\(s\), 0 Jhon, 0 audit-error\(s\)`)
		match(t, human.Stdout, `delivered-untested\s+1 violation \| -`)
		equal(t, audit(t, []string{"--all", "--since", "2099-01-01", "--json"}, "HOME="+home).Stdout != "", true)
		equal(t, get(parse(t, audit(t, []string{"--all", "--since", "2099-01-01", "--json"}, "HOME="+home).Stdout), "sessions"), any(0.0), "nothing after a future date")
		equal(t, audit(t, []string{"--all", "--since", "soon"}, "HOME="+home).Code, 2)
	})

	t.Run("jhon audit refuses a missing target and bad usage with exit 2", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		equal(t, audit(t, []string{"00000000-dead-beef-0000-000000000000"}, "HOME="+home).Code, 2)
		for _, args := range [][]string{{}, {"a", "b"}, {"--bogus"}} {
			equal(t, audit(t, args).Code, 2, args)
		}
	})

	// ── efficiency ──────────────────────────────────────────────────────────

	t.Run("efficiency: the human output and --json carry the section", func(t *testing.T) {
		t.Parallel()
		main := sessionWith(t, userStep("go"),
			delegation{id: "e00", typ: "explore", change: x, result: "found it"},
			delegation{id: "e01", typ: "worker", change: x, prompt: "Fix `src/a.ts`.",
				calls: []call{{name: "Write", input: map[string]any{"file_path": root + "/src/a.ts", "content": "x"}}}, result: shaped})
		equal(t, get(parse(t, audit(t, []string{main, "--json"}).Stdout), "efficiency", "writers", 0, "agent"), any("e01"))
		match(t, audit(t, []string{main}).Stdout, `(?m)^  efficiency  e01 \(worker\)  change add-response-timing  return ok  outside scope 0  dependencies 0  same failure max 0  active 3s  files 1$`)
	})

	// ── context-use ─────────────────────────────────────────────────────────

	t.Run("context-use: --json prints it as context_use, and it is metric-only: findings, ok, status, and the exit code are those of the same session without usage", func(t *testing.T) {
		t.Parallel()
		steps := func(u *usage) []step {
			read := call{name: "Read", input: map[string]any{"file_path": root + "/a.ts"}, result: "1\tx", usage: u}
			return []step{userStep("go"), delegation{id: "cu01", typ: "general", calls: []call{read, read}}}
		}
		heavy := sessionWith(t, steps(use(10, 400000, 500000))...)
		bare := sessionWith(t, steps(nil)...)
		h, b := audit(t, []string{heavy, "--json"}), audit(t, []string{bare, "--json"})
		equal(t, h.Code, b.Code)
		hj, bj := parse(t, h.Stdout), parse(t, b.Stdout)
		for _, k := range []string{"ok", "status", "findings", "errors", "notes"} {
			equal(t, hj[k], bj[k], k)
		}
		var a any
		for _, ag := range get(hj, "context_use", "agents").([]any) {
			if get(ag, "agent") == "cu01" {
				a = ag
			}
		}
		equal(t, get(a, "peak_context_per_turn"), any(float64(900010)))
		equal(t, get(a, "same_file_rereads"), any(float64(1)))
		isNull(t, parse(t, audit(t, []string{mainOf(t, "missing-field"), "--json"}).Stdout), "context_use", "an audit-error measures nothing")
	})

	t.Run("context-use: the default human output does not print it; --context adds the section before the closing status line", func(t *testing.T) {
		t.Parallel()
		main := sessionWith(t, userStep("go"), delegation{id: "cu01", typ: "worker", phase: "apply", change: x, calls: []call{
			{name: "Read", input: map[string]any{"file_path": root + "/a.ts"}, result: "1\tx", usage: use(1, 2, 3)}}})
		plain := audit(t, []string{main})
		wide := audit(t, []string{main, "--context"})
		noMatch(t, plain.Stdout, `context-use`)
		equal(t, wide.Code, plain.Code)
		equal(t, lastLine(wide.Stdout), lastLine(plain.Stdout))
		match(t, wide.Stdout, `(?m)^ {2}context-use {2}measurement only; never a finding$`)
		match(t, wide.Stdout, `(?m)^ {4}agent {2}cu01 \(worker\) {2}phase apply {2}change add-response-timing {2}turns 2 {2}peak 6 {2}processed 6 {2}rereads 0 {2}whole reads over limit 0$`)
		match(t, wide.Stdout, `(?m)^ {4}by phase {2}apply {2}agents 1 {2}processed 6 \(100\.0%\) {2}peak max 6 {2}read bytes full 3 ranged 0 shell 0 {2}codegraph 0 {2}engram 0 {2}search 0 {2}other 0$`)
	})

	// ── dashboard ───────────────────────────────────────────────────────────
	// `--html` prints the same report as `--json`, embedded in one self-contained
	// page: no server, no network, nothing leaves the machine.

	t.Run("--html prints a self-contained page that embeds the --json report", func(t *testing.T) {
		t.Parallel()
		html := audit(t, []string{"--html", "--context", mainOf(t, "clean")})
		jsonOut := audit(t, []string{"--json", "--context", mainOf(t, "clean")})
		equal(t, html.Code, jsonOut.Code, html.Stderr)
		match(t, html.Stdout, `^<!doctype html>`)
		m := regexp.MustCompile(`<script type="application/json" id="report">((?s:.*?))</script>`).FindStringSubmatch(html.Stdout)
		if m == nil || m[1] == "" {
			t.Fatal("the report is not embedded")
		}
		equal(t, parse(t, m[1]), parse(t, jsonOut.Stdout))
		match(t, html.Stdout, `<script type="application/json" id="session">\{"session":`)
		noMatch(t, html.Stdout, `<(script|link)[^>]+(src|href)=|https?://`)
	})

	// `--view` prints what the page embeds as the session: the shell's Jhon
	// panel reads it with the same component's functions.
	t.Run("--view prints the session data the --html page embeds", func(t *testing.T) {
		t.Parallel()
		html := audit(t, []string{"--html", mainOf(t, "clean")})
		view := audit(t, []string{"--view", mainOf(t, "clean")})
		equal(t, view.Code, html.Code, view.Stderr)
		m := regexp.MustCompile(`<script type="application/json" id="session">((?s:.*?))</script>`).FindStringSubmatch(html.Stdout)
		if m == nil {
			t.Fatal("the session is not embedded")
		}
		equal(t, parse(t, view.Stdout), parse(t, m[1]))
	})
}

// dialogueSession is a Claude Code session of one dialogue, a line a
// second: a question in plain text and its answer (a "keep going" that
// answers, no correction), a command, a round of
// three questions and its answer, and a closing reply. Its texts are marked
// so that a test can tell whether any reaches an output.
func dialogueSession(t *testing.T) string {
	t.Helper()
	n := 0
	line := func(typ string, content any, extra map[string]any) rawStep {
		n++
		return rawStep(with(map[string]any{"parentUuid": nil, "isSidechain": false, "type": typ, "message": map[string]any{"role": typ, "content": content},
			"uuid": fmt.Sprintf("d-%d", n), "timestamp": iso(n), "userType": "external", "cwd": root, "sessionId": session, "version": "2.1.280"}, extra))
	}
	human := map[string]any{"origin": map[string]any{"kind": "human"}}
	text := func(s string) []any { return []any{map[string]any{"type": "text", "text": s}} }
	use := func(id, name string, input map[string]any) []any {
		return []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}}
	}
	result := func(id, s string) []any {
		return []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": s}}
	}
	question := func(q string) map[string]any {
		return map[string]any{"question": q, "header": "MARKHEAD", "multiSelect": false, "options": []any{map[string]any{"label": "MARKOPTION", "description": "MARKDESC"}, map[string]any{"label": "other", "description": ""}}}
	}
	return sessionWith(t,
		line("user", "go", human),
		line("assistant", text("MARKPROSE which store?"), nil),
		line("user", "MARKANSWER the small one, keep going", human),
		line("assistant", use("toolu_ls", "Bash", map[string]any{"command": "ls"}), nil),
		line("user", result("toolu_ls", "ok"), nil),
		line("assistant", use("toolu_ask", "AskUserQuestion", map[string]any{"questions": []any{question("MARKQ1?"), question("MARKQ2?"), question("MARKQ3?")}}), nil),
		line("user", result("toolu_ask", `User has answered your questions: "MARKQ1?"="MARKPICK".`), nil),
		line("assistant", text("MARKEND closed."), nil))
}

// piDialogueSession writes, under dir, a pi session of one dialogue: a
// question in plain text and its answer, which repeats itself, then a
// round of one question.
func piDialogueSession(t *testing.T, dir string) string {
	t.Helper()
	id := "01a17000-0000-7000-8000-0000000000d1"
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
		entry(map[string]any{"role": "assistant", "content": text("MARKPIPROSE ready?"), "stopReason": "stop"}),
		entry(map[string]any{"role": "user", "content": text("MARKPIANSWER yes, te dije")}),
		entry(map[string]any{"role": "assistant", "stopReason": "toolUse", "content": []any{map[string]any{"type": "toolCall", "id": "toolu_ask", "name": "ask_user",
			"arguments": map[string]any{"questions": []any{map[string]any{"question": "MARKPIQ?", "header": "h", "options": []any{map[string]any{"label": "MARKPIOPTION"}, map[string]any{"label": "other"}}}}}}}}),
		entry(map[string]any{"role": "toolResult", "toolCallId": "toolu_ask", "toolName": "ask_user", "content": text("MARKPIPICK"), "isError": false, "durationMs": 5}),
		entry(map[string]any{"role": "assistant", "content": text("MARKPIEND closed."), "stopReason": "stop"}),
	}
	name := "2026-09-30T10-00-00-000Z_" + id + ".jsonl"
	support.Write(t, dir, map[string]string{name: strings.Join(lines, "\n") + "\n"})
	return filepath.Join(dir, name)
}

// marks are the texts of the two dialogue sessions: none may reach an output.
var marks = regexp.MustCompile(`MARK[A-Z0-9]+`)

// T66a (L990 C1-C5), T76b (L1074 K1-K4): `jhon audit` states the owner's dialogue, its corrections included, as
// statistics, for a Claude Code transcript and a pi session, in --json and
// the human output; `jhon audit --all` sums them; none is a finding, the
// exit code is the session's own, and no output holds the session's text.
func TestAuditDialogue(t *testing.T) {
	t.Parallel()
	claude := dialogueSession(t)
	home := t.TempDir()
	pi := piDialogueSession(t, filepath.Join(home, ".pi", "jhon", "sessions", "--work-p--"))
	if err := os.CopyFS(filepath.Join(home, ".claude", "projects", "-work-p"), os.DirFS(filepath.Dir(claude))); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ what, main, json, human string }{
		{"claude code", claude,
			`{"owner_answers":2,"responses_per_answer":{"median":1,"max":2},"tool_calls_per_answer":{"median":0,"max":1},"ms_per_answer":{"median":1000,"max":3000},` +
				`"ask_tool_calls":1,"questions_per_call":{"1":0,"2":0,"3":1,"4_or_more":0},"prose_questions":1,` +
				`"corrections":{"owner_messages":2,"correcting":0,"keep_going":0,"repeat":0,"unrequested":0,"decision":0}}`,
			"\n  dialogue  owner answers 2  responses per answer 1 (max 2)  tool calls per answer 0 (max 1)  time per answer 1s (max 3s)\n" +
				"  dialogue  ask-tool calls 1  questions per call 1:0 2:0 3:1 4+:0  prose questions 1 (lower bound)\n" +
				"  dialogue  corrections 0 of 2 owner messages  keep-going 0  repeat 0  unrequested 0  decision 0\n"},
		{"pi", pi,
			`{"owner_answers":2,"responses_per_answer":{"median":1,"max":1},"tool_calls_per_answer":{"median":0,"max":0},"ms_per_answer":{"median":1000,"max":1000},` +
				`"ask_tool_calls":1,"questions_per_call":{"1":1,"2":0,"3":0,"4_or_more":0},"prose_questions":1,` +
				`"corrections":{"owner_messages":2,"correcting":1,"keep_going":0,"repeat":1,"unrequested":0,"decision":0}}`,
			"\n  dialogue  owner answers 2  responses per answer 1 (max 1)  tool calls per answer 0 (max 0)  time per answer 1s (max 1s)\n" +
				"  dialogue  ask-tool calls 1  questions per call 1:1 2:0 3:0 4+:0  prose questions 1 (lower bound)\n" +
				"  dialogue  corrections 1 of 2 owner messages  keep-going 0  repeat 1  unrequested 0  decision 0\n"},
	} {
		r := audit(t, []string{c.main, "--json"})
		equal(t, r.Code, 0, c.what, r.Stderr)
		j := parse(t, r.Stdout)
		equal(t, strings.TrimSpace(js(t, j["dialogue"])), compactJSON(t, c.json), c.what)
		equal(t, j["status"], any("nothing-to-audit"), c.what)
		equal(t, len(get(j, "findings").([]any)), 0, c.what)
		human := audit(t, []string{c.main})
		equal(t, human.Code, 0, c.what)
		if !strings.Contains(human.Stdout, c.human) {
			t.Errorf("%s: the human output misses the dialogue lines:\n%s", c.what, human.Stdout)
		}
		equal(t, lastLine(human.Stdout), "nothing-to-audit", c.what)
		view := audit(t, []string{c.main, "--view"})
		equal(t, view.Code, 0, c.what)
		for mode, out := range map[string]string{"--json": r.Stdout, "human": human.Stdout, "--view": view.Stdout} {
			if m := marks.FindString(out); m != "" {
				t.Errorf("%s %s holds the session's text %s", c.what, mode, m)
			}
		}
	}

	r := audit(t, []string{"--all", "--json"}, "HOME="+home)
	equal(t, r.Code, 0, r.Stderr)
	j := parse(t, r.Stdout)
	equal(t, strings.TrimSpace(js(t, j["dialogue"])), compactJSON(t, `{"sessions":2,"owner_answers":4,"responses_per_answer":{"median":1,"max":2},"tool_calls_per_answer":{"median":0,"max":1},`+
		`"ms_per_answer":{"median":1000,"max":3000},"ask_tool_calls":2,"questions_per_call":{"1":1,"2":0,"3":1,"4_or_more":0},"prose_questions":2,`+
		`"corrections":{"owner_messages":4,"correcting":1,"keep_going":0,"repeat":1,"unrequested":0,"decision":0}}`))
	equal(t, strings.TrimSpace(js(t, j["jhon_dialogue"])), compactJSON(t, `{"sessions":1,"owner_answers":2,"responses_per_answer":{"median":1,"max":1},"tool_calls_per_answer":{"median":0,"max":0},`+
		`"ms_per_answer":{"median":1000,"max":1000},"ask_tool_calls":1,"questions_per_call":{"1":1,"2":0,"3":0,"4_or_more":0},"prose_questions":1,`+
		`"corrections":{"owner_messages":2,"correcting":1,"keep_going":0,"repeat":1,"unrequested":0,"decision":0}}`))
	keys := topKeys(t, r.Stdout)
	equal(t, keys[len(keys)-3:], []string{"ldd", "dialogue", "jhon_dialogue"})
	equal(t, len(get(j, "findings").(map[string]any)), 0, "a statistic is no finding")
	human := audit(t, []string{"--all"}, "HOME="+home)
	equal(t, human.Code, 0, human.Stderr)
	match(t, human.Stdout, `(?m)^owner dialogue in the main threads, as statistics \(`)
	match(t, human.Stdout, `(?m)^  all\s+2\s+4\s+1 / 2\s+0 / 1\s+1s / 3s\s+2\s+1\s+0\s+1\s+0\s+2$`)
	match(t, human.Stdout, `(?m)^  Jhon's\s+1\s+2\s+1 / 1\s+0 / 0\s+1s / 1s\s+1\s+1\s+0\s+0\s+0\s+1$`)
	// The corrections, summed: one "te dije" in Jhon's pi session; the "keep going" that answers a question is none.
	match(t, human.Stdout, `(?m)^\s+sessions\s+owner messages\s+correcting\s+keep-going\s+repeat\s+unrequested\s+decision$`)
	match(t, human.Stdout, `(?m)^  all\s+2\s+4\s+1\s+0\s+1\s+0\s+0$`)
	match(t, human.Stdout, `(?m)^  Jhon's\s+1\s+2\s+1\s+0\s+1\s+0\s+0$`)
	for mode, out := range map[string]string{"--all --json": r.Stdout, "--all": human.Stdout} {
		if m := marks.FindString(out); m != "" {
			t.Errorf("%s holds a session's text %s", mode, m)
		}
	}
}

// T134 (L1021 H8): `jhon audit` states how each agent role used memory,
// CodeGraph and the todo list, as statistics, for a Claude Code transcript
// and a pi session, in --json and the human output; `jhon audit --all` sums
// them, Jhon's sessions apart; none is a finding, the exit code is the
// session's own, and no output holds the session's text.
func TestAuditIntegrations(t *testing.T) {
	t.Parallel()
	claude := dialogueSession(t)
	home := t.TempDir()
	pi := piDialogueSession(t, filepath.Join(home, ".pi", "jhon", "sessions", "--work-p--"))
	if err := os.CopyFS(filepath.Join(home, ".claude", "projects", "-work-p"), os.DirFS(filepath.Dir(claude))); err != nil {
		t.Fatal(err)
	}
	role := func(plain int, share string) string {
		return `{"launches":1,"launches_with_memory_read":0,"launches_with_memory_write":0,"codegraph_calls":0,"plain_reads":` + strconv.Itoa(plain) +
			`,"codegraph_share":` + share + `,"todo_calls":0}`
	}
	for _, c := range []struct{ what, main, json, human string }{
		// The Claude Code session's `ls` is a plain read; its ask-tool call is none.
		{"claude code", claude, `{"orchestrator":` + role(1, "0") + `}`,
			"\n  integrations  orchestrator  launches 1  memory read 0  memory write 0  codegraph 0  plain reads 1  codegraph share 0.0%  todo 0\n"},
		{"pi", pi, `{"orchestrator":` + role(0, "null") + `}`,
			"\n  integrations  orchestrator  launches 1  memory read 0  memory write 0  codegraph 0  plain reads 0  codegraph share -  todo 0\n"},
	} {
		r := audit(t, []string{c.main, "--json"})
		equal(t, r.Code, 0, c.what, r.Stderr)
		j := parse(t, r.Stdout)
		equal(t, strings.TrimSpace(js(t, j["integrations"])), compactJSON(t, c.json), c.what)
		equal(t, j["status"], any("nothing-to-audit"), c.what)
		equal(t, len(get(j, "findings").([]any)), 0, c.what)
		human := audit(t, []string{c.main})
		equal(t, human.Code, 0, c.what)
		if !strings.Contains(human.Stdout, c.human) {
			t.Errorf("%s: the human output misses the integrations line:\n%s", c.what, human.Stdout)
		}
		equal(t, lastLine(human.Stdout), "nothing-to-audit", c.what)
	}

	r := audit(t, []string{"--all", "--json"}, "HOME="+home)
	equal(t, r.Code, 0, r.Stderr)
	j := parse(t, r.Stdout)
	equal(t, strings.TrimSpace(js(t, j["integrations"])), compactJSON(t, `{"sessions":2,"roles":{"orchestrator":{"launches":2,"launches_with_memory_read":0,"launches_with_memory_write":0,`+
		`"codegraph_calls":0,"plain_reads":1,"codegraph_share":0,"todo_calls":0}}}`))
	equal(t, strings.TrimSpace(js(t, j["jhon_integrations"])), compactJSON(t, `{"sessions":1,"roles":{"orchestrator":`+role(0, "null")+`}}`))
	equal(t, len(get(j, "findings").(map[string]any)), 0, "a statistic is no finding")
	human := audit(t, []string{"--all"}, "HOME="+home)
	equal(t, human.Code, 0, human.Stderr)
	match(t, human.Stdout, `(?m)^agent integrations by role, as statistics, over 2 session\(s\), 1 Jhon's \(`)
	match(t, human.Stdout, `(?m)^  all\s+orchestrator\s+2\s+0\s+0\s+0\s+1\s+0\.0%\s+0$`)
	match(t, human.Stdout, `(?m)^  Jhon's\s+orchestrator\s+1\s+0\s+0\s+0\s+0\s+-\s+0$`)
	for mode, out := range map[string]string{"--all --json": r.Stdout, "--all": human.Stdout} {
		if m := marks.FindString(out); m != "" {
			t.Errorf("%s holds a session's text %s", mode, m)
		}
	}
}

// compactJSON is s as js prints a parsed value: its keys sorted.
func compactJSON(t *testing.T, s string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(js(t, v))
}
