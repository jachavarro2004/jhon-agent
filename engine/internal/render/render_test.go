package render

import (
	"bytes"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
)

// repo is this checkout, which every rule must pass.
const repo = "../../.."

// engineAt is the renderer over root with flags argv, under an empty HOME.
func engineAt(t *testing.T, root string, argv ...string) *engine {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("JHON_PROFILES", "")
	t.Setenv("JHON_DESKTOP_COLORS", "")
	a, perr := parseArgs(argv)
	if perr != "" {
		t.Fatal(perr)
	}
	e, err := newEngine(resolveRoot(root), a, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestParseArgs(t *testing.T) {
	for _, c := range []struct {
		argv []string
		err  string
	}{
		{[]string{"--validate"}, ""},
		{[]string{"--help"}, ""},
		{[]string{"--install", "--source", "x", "--only", "pi"}, ""},
		{[]string{}, "jhon render-engine installs only with --install, from a release, as `jhon sync` runs it"},
		{[]string{"--bogus"}, "unknown option: --bogus"},
		{[]string{"stray"}, "unexpected argument: stray"},
		{[]string{"--only"}, "--only requires a value"},
		{[]string{"--print", "--check"}, "--print requires a value"},
		{[]string{"--only", "opencode"}, "--only must be claude-code or pi, not 'opencode'"},
		{[]string{"--check", "--check"}, "--check given more than once"},
		{[]string{"--install", "--check", "--source", "x"}, "--install takes no --check, --validate, --print, or --preflight"},
		{[]string{"--preflight", "--only", "pi", "--source", "x"}, "--preflight takes no --check, --validate, --print, or --only"},
		{[]string{"--install"}, "--install or --preflight and --source <dir> go together"},
	} {
		if _, err := parseArgs(c.argv); err != c.err {
			t.Errorf("parseArgs(%q) = %q, want %q", c.argv, err, c.err)
		}
	}
}

func TestRunRefusesBeforeValidating(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errw bytes.Buffer
	if code := Run(repo, []string{"--bogus"}, &out, &errw); code != 2 || out.Len() != 0 || !strings.HasPrefix(errw.String(), "jhon render-engine: unknown option: --bogus\n\nusage: ") {
		t.Errorf("code %d, out %q, err %q", code, out.String(), errw.String())
	}
	out.Reset()
	if code := Run(repo, []string{"-h"}, &out, &errw); code != 0 || out.String() != Usage {
		t.Errorf("--help: code %d, out %q", code, out.String())
	}
}

// The repository validates: every rule holds on the files it ships.
func TestRepositoryValidates(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	if p := append(e.validate(), e.validateRuntime()...); len(p) != 0 {
		t.Fatalf("problems: %+v", p)
	}
}

// A contract dropped from the compiled always-on layer fails the
// runtime-boundaries rule.
func TestAlwaysOnWithoutRuntimeBoundaries(t *testing.T) {
	saved := alwaysOnGroups[1].files
	t.Cleanup(func() { alwaysOnGroups[1].files = saved })
	alwaysOnGroups[1].files = slices.DeleteFunc(slices.Clone(saved), func(f string) bool { return f == "contracts/runtime-boundaries.md" })
	e := engineAt(t, repo, "--validate")
	if !slices.Contains(e.validate(), problem{"engine/internal/render/render.go", 0, "runtime-boundaries", "contracts/runtime-boundaries.md is not always on"}) {
		t.Error("the rule did not fire")
	}
}

func TestExpandTemplate(t *testing.T) {
	var v manifest.Map[string]
	v.Set("judge", "A")
	v.Set("spare", "x")
	text, unknown, unused := expandTemplate("judge {{judge}}, {{facet}} and {{facet}}", &v)
	if text != "judge A, {{facet}} and {{facet}}" || !reflect.DeepEqual(unknown, []string{"facet"}) || !reflect.DeepEqual(unused, []string{"spare"}) {
		t.Errorf("%q %v %v", text, unknown, unused)
	}
}

func TestWithoutClaudeMdBlock(t *testing.T) {
	for src, want := range map[string]string{
		"mine\n\n<!-- jhon:start -->\nold\n<!-- jhon:end -->\n\ntail\n": "mine\n\ntail\n",
		"<!-- jhon:start -->x<!-- jhon:end -->\nrest":                   "rest",
		"head\n<!-- jhon:start -->x<!-- jhon:end -->\n\n":               "head\n",
		"no markers": "no markers",
	} {
		if got, ok := withoutClaudeMdBlock(src); !ok || got != want {
			t.Errorf("withoutClaudeMdBlock(%q) = %q", src, got)
		}
	}
	if _, _, _, why := claudeMdBlock("<!-- jhon:end --> <!-- jhon:start -->"); why != "<!-- jhon:end --> comes before <!-- jhon:start -->. Nothing was written: reorder the markers by hand" {
		t.Errorf("reversed: %q", why)
	}
	if _, ok := withoutClaudeMdBlock("<!-- jhon:start --><!-- jhon:start -->"); ok {
		t.Error("duplicated markers were repaired")
	}
}

func TestMergeSettings(t *testing.T) {
	e := engineAt(t, repo, "--check")
	guard := "'" + filepath.Join(e.current(), "hooks") + "/safety-guard.mjs' --x"
	prev := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook claude"},{"type":"command","command":"` + guard + `"}]},{"matcher":"Edit","hooks":[{"command":"` + guard + `"}]}],"Stop":[]},"z":2}`
	got, err := e.mergeSettings(prev)
	want := "{\n  \"z\": 2,\n  \"hooks\": {\n    \"Stop\": [],\n    \"PreToolUse\": [\n      {\n        \"matcher\": \"Bash\",\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"rtk hook claude\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"
	if err != "" || got != want {
		t.Errorf("merge = %q %q", got, err)
	}
	for src, why := range map[string]string{
		"{ not json":                    "is not valid JSON (Expected property name or '}' in JSON at position 2 (line 1 column 3)); nothing was written",
		"[1]":                           "is not a JSON object; nothing was written",
		`{"disableAllHooks": true}`:     "sets `disableAllHooks: true`, which turns every Jhon guard off; nothing was written. Remove it (or set it to false), then sync again",
		`{"hooks": null}`:               "has a `hooks` that is not an object; nothing was written",
		`{"hooks": {"PreToolUse": {}}}`: "has a `hooks.PreToolUse` that is not a list; nothing was written",
	} {
		if _, err := e.mergeSettings(src); err != why {
			t.Errorf("mergeSettings(%q) = %q", src, err)
		}
	}
	if got, _ := e.mergeSettings(`{"hooks":{"PreToolUse":[{"hooks":[{"command":"mine"}]}]}}`); got != `{"hooks":{"PreToolUse":[{"hooks":[{"command":"mine"}]}]}}` {
		t.Errorf("a file without Jhon guards was rewritten: %q", got)
	}
}

func TestSlashCommands(t *testing.T) {
	got := slashCommands("run /jhon-ldd-new or `/ldd-explore`, not $TMPDIR/ldd-archive.X nor skills/ldd-apply/; (/jhon-ldd-x-)")
	if !reflect.DeepEqual(got, []string{"jhon-ldd-new", "ldd-explore", "jhon-ldd-x-"}) {
		t.Errorf("slashCommands = %q", got)
	}
}

func TestResolveRef(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	for ref, want := range map[string]string{
		"~/.agents/jhon/prose/contracts/tdd.md":         filepath.Join(e.root, "prose/contracts/tdd.md"),
		"~/.local/share/jhon/current":                   e.root,
		"~/.local/share/jhon/current/prose/skills/ldd/": filepath.Join(e.root, "prose/skills/ldd") + "/",
		"~/.claude/skills/ldd/SKILL.md":                 filepath.Join(e.root, "prose/skills/ldd/SKILL.md"),
		"~/.claude/skills/not-ours/SKILL.md":            filepath.Join(e.home, ".claude/skills/not-ours/SKILL.md"),
		"contracts/tdd.md":                              filepath.Join(e.root, "prose/contracts/tdd.md"),
		"tools/git-hooks/pre-commit":                    filepath.Join(e.root, "tools/git-hooks/pre-commit"),
	} {
		if got := e.resolveRef(ref); got != want {
			t.Errorf("resolveRef(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestHookCommand(t *testing.T) {
	if got := command("/a b/x.mjs", "--agent", "it's"); got != `'/a b/x.mjs' '--agent' 'it'\''s'` {
		t.Errorf("command = %s", got)
	}
}

// Each runtime's prompts name only its own tools: a body says what differs
// as a runtime term, and no term is left unrendered.
func TestEachRuntimeNamesOnlyItsOwnTools(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	foreign := map[string][]string{
		"pi":          {"SendMessage", "ExitPlanMode", "AskUserQuestion", "~/.claude/plans", "{{runtime:"},
		"claude-code": {"subagent_continue", "exit_plan_mode", "ask_user", "{{runtime:"},
	}
	for _, r := range e.renderers() {
		for _, f := range r.Files() {
			for _, word := range foreign[r.Runtime()] {
				if strings.Contains(f.Content, word) {
					t.Errorf("%s: %s names %q", r.Runtime(), f.Path, word)
				}
			}
		}
	}
	if !strings.Contains(e.bodyFor("pi", e.mainAgent(piMain)), "`subagent_continue`") {
		t.Error("pi's orchestrator is not told how to continue a sub-agent")
	}
}

// A body's runtime term with no words for a runtime is a validation problem.
func TestARuntimeTermWithoutWordsIsAProblem(t *testing.T) {
	words := runtimeTerms["plan-mode"]["pi"]
	delete(runtimeTerms["plan-mode"], "pi")
	defer func() { runtimeTerms["plan-mode"]["pi"] = words }()
	e := engineAt(t, repo, "--validate")
	want := "jhon-orchestrator: runtime term {{runtime:plan-mode}} has no words for pi"
	if p := e.validate(); !slices.ContainsFunc(p, func(x problem) bool { return x.msg == want }) {
		t.Errorf("want %q among %+v", want, p)
	}
}

// pi's owner grants from /jhon:grants (L214: the mode card is gone); the panel's Grants tab is gone (T15.11).
func TestOwnerGrantNamesJhonGrants(t *testing.T) {
	pi := runtimeTerms["owner-grant"]["pi"]
	if !strings.Contains(pi, "`/jhon:grants`") || strings.Contains(pi, "mode") || strings.Contains(pi, "Grants tab") || strings.Contains(pi, "alt+j") || strings.Contains(pi, "/jhon grants") {
		t.Errorf("owner-grant for pi = %q, want /jhon:grants only", pi)
	}
}

// L264: each runtime's prompts carry only its own rules, once: one
// `## Routing`, pi's "no `!`" rule in one place, no Claude Code inside pi's,
// and no pi rule inside Claude Code's.
func TestEachRuntimeGetsItsOwnRulesOnce(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	foreign := map[string][]string{
		"pi":          {"Claude Code"},
		"claude-code": {"in pi", "/jhon:grants", "pi's sub-agents", "`/new`"},
	}
	for _, r := range e.renderers() {
		for _, f := range r.Files() {
			if !strings.HasSuffix(f.Path, ".md") {
				continue
			}
			body := f.Content
			if strings.HasPrefix(body, "---\n") {
				if end := strings.Index(body[4:], "\n---\n"); end >= 0 {
					body = body[end+9:]
				}
			}
			if n := strings.Count(body, "\n## Routing\n"); n > 1 {
				t.Errorf("%s: %s has %d `## Routing` headings", r.Runtime(), f.Path, n)
			}
			// pi's main session hears it once, from the shell (lib/owner-shell.ts).
			if strings.Contains(body, "has no `!`") {
				t.Errorf("%s: %s says pi has no `!`; the shell's note says it", r.Runtime(), f.Path)
			}
			for _, word := range foreign[r.Runtime()] {
				if strings.Contains(body, word) {
					t.Errorf("%s: %s names %q", r.Runtime(), f.Path, word)
				}
			}
		}
	}
	if !strings.Contains(e.alwaysOnSection("pi", true), "\n## Routing\n") {
		t.Error("the always-on layer lost its `## Routing` heading")
	}
}

// An always-on file's runtime term with no words for a runtime is a
// validation problem too.
func TestAnAlwaysOnRuntimeTermWithoutWordsIsAProblem(t *testing.T) {
	words := runtimeTerms["grant-how"]["pi"]
	delete(runtimeTerms["grant-how"], "pi")
	defer func() { runtimeTerms["grant-how"]["pi"] = words }()
	e := engineAt(t, repo, "--validate")
	// T146 FA3: remote-auth.md's grant line moved to a contract read on
	// demand; runtime-boundaries.md keeps the always-on grant-how term.
	want := problem{"contracts/runtime-boundaries.md", 0, "agent-template", "runtime term {{runtime:grant-how}} has no words for pi"}
	if p := e.validate(); !slices.Contains(p, want) {
		t.Errorf("want %+v among %+v", want, p)
	}
}
