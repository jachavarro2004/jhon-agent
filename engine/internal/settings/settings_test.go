package settings

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// noEnv is an environment with nothing set.
func noEnv(string) (string, bool) { return "", false }

// envOf is an environment holding exactly vars.
func envOf(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

func write(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func valueOf(t *testing.T, vals []Value, key string) Value {
	t.Helper()
	v, ok := Find(vals, key)
	if !ok {
		t.Fatalf("no key %s", key)
	}
	return v
}

// noRuntimes is a machine with neither runtime: an empty HOME and a PATH
// with no claude or pi (T10g: an unset runtime is on only where detected).
// It returns the HOME.
func noRuntimes(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	return home
}

func mkdirAll(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

// With no setting a runtime is off where nothing of it is found, and on
// once its directory is, still from the default (T10g).
func TestRuntimeDefaultsAreDetected(t *testing.T) {
	home := noRuntimes(t)
	path := filepath.Join(t.TempDir(), "settings.toml")
	vals, err := Load(path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"runtimes.claude_code", "runtimes.pi"} {
		if v := valueOf(t, vals, key); v.Value != false || v.Source != SourceDefault {
			t.Errorf("nothing detected: %s = %#v (%s)", key, v.Value, v.Source)
		}
	}
	mkdirAll(t, filepath.Join(home, ".claude"))
	vals, _ = Load(path, noEnv)
	if v := valueOf(t, vals, "runtimes.claude_code"); v.Value != true || v.Source != SourceDefault {
		t.Errorf("~/.claude present: %#v (%s)", v.Value, v.Source)
	}
	if v := valueOf(t, vals, "runtimes.pi"); v.Value != false {
		t.Errorf("pi still absent: %#v", v.Value)
	}
}

func TestDefaultsWithNoFile(t *testing.T) {
	home := noRuntimes(t)
	mkdirAll(t, filepath.Join(home, ".claude"))
	mkdirAll(t, filepath.Join(home, ".pi"))
	vals, err := Load(filepath.Join(t.TempDir(), "settings.toml"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"runtimes.claude_code":                   true,
		"runtimes.pi":                            true,
		"integrations.engram":                    true,
		"integrations.codegraph":                 true,
		"integrations.context7":                  true,
		"agents.act_on_announce":                 true,
		"integrations.telemetry":                 false,
		"integrations.theme.palette":             "rose-ink",
		"claude_code.connectors":                 false,
		"pi.profile":                             "default",
		"shell.appearance.animations":            "full",
		"shell.appearance.ascii":                 false,
		"shell.appearance.icons":                 "geometric",
		"shell.appearance.status":                "auto",
		"shell.appearance.cards":                 "float",
		"shell.appearance.surface":               "transparent",
		"shell.appearance.density":               "compact", // the owner's choice (L1498)
		"shell.behaviour.compact_at":             60,
		"shell.behaviour.compact_tokens":         200000,
		"shell.behaviour.child_backstop_tokens":  80000,
		"shell.behaviour.stall_seconds":          75,
		"shell.behaviour.thinking_stall_seconds": 300,
		"shell.behaviour.double_esc_cancel":      false, // today's shell default (L199)
		"shell.keys.panel":                       "alt+j",
		"shell.keys.agents":                      "alt+s",
		"shell.keys.jobs":                        "alt+o",
		"shell.keys.usage":                       "alt+u",
		"shell.keys.todos":                       "alt+t",
	}
	for key, w := range want {
		v := valueOf(t, vals, key)
		if v.Value != w || v.Source != SourceDefault {
			t.Errorf("%s = %#v (%s), want %#v (default)", key, v.Value, v.Source, w)
		}
	}
}

func TestFileValuesAndEnvBeatsFile(t *testing.T) {
	path := write(t, `schema = 1
[shell.behaviour]
compact_at = 40
[shell.keys]
panel = "alt+k"
[shell.appearance]
icons = "emoji"
`)
	vals, err := Load(path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "shell.behaviour.compact_at"); v.Value != 40 || v.Source != SourceFile {
		t.Errorf("compact_at = %#v (%s)", v.Value, v.Source)
	}
	if v := valueOf(t, vals, "shell.appearance.icons"); v.Value != "emoji" || v.Source != SourceFile {
		t.Errorf("icons = %#v (%s)", v.Value, v.Source)
	}
	env := envOf(map[string]string{
		"JHON_SHELL_COMPACT_AT":             "55",
		"JHON_SHELL_JHON_KEY":               "off",
		"JHON_SHELL_TASKS_KEY":              "ctrl+b",
		"JHON_SHELL_ASCII":                  "1",
		"JHON_SHELL_STALL_SECONDS":          "0",
		"JHON_SHELL_THINKING_STALL_SECONDS": "0",
		"JHON_SHELL_DOUBLE_ESC_CANCEL":      "off",
		"JHON_TELEMETRY":                    "on",
	})
	vals, err = Load(path, env)
	if err != nil {
		t.Fatal(err)
	}
	for key, w := range map[string]any{
		"shell.behaviour.compact_at":             55,
		"shell.keys.panel":                       "off",
		"shell.keys.agents":                      "ctrl+b",
		"shell.appearance.ascii":                 true,
		"shell.behaviour.stall_seconds":          0,
		"shell.behaviour.thinking_stall_seconds": 0,
		"shell.behaviour.double_esc_cancel":      false,
		"integrations.telemetry":                 true,
	} {
		v := valueOf(t, vals, key)
		if v.Value != w || v.Source != SourceEnv || v.Env == "" {
			t.Errorf("%s = %#v (%s, %s), want %#v from env", key, v.Value, v.Source, v.Env, w)
		}
	}
	// icons has no variable: the file still decides.
	if v := valueOf(t, vals, "shell.appearance.icons"); v.Source != SourceFile {
		t.Errorf("icons source = %s", v.Source)
	}
}

func TestCompactAtOffFromEnvLeavesItToPi(t *testing.T) {
	vals, err := Load(filepath.Join(t.TempDir(), "none.toml"), envOf(map[string]string{"JHON_SHELL_COMPACT_AT": "off"}))
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "shell.behaviour.compact_at"); v.Value != 100 || v.Source != SourceEnv {
		t.Errorf("compact_at = %#v (%s), want 100 from env", v.Value, v.Source)
	}
}

func TestAnUnusableEnvValueIsIgnored(t *testing.T) {
	vals, err := Load(filepath.Join(t.TempDir(), "none.toml"), envOf(map[string]string{"JHON_SHELL_STALL_SECONDS": "soon"}))
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "shell.behaviour.stall_seconds"); v.Value != 75 || v.Source != SourceDefault {
		t.Errorf("stall_seconds = %#v (%s)", v.Value, v.Source)
	}
}

func TestValidationErrors(t *testing.T) {
	for _, c := range []struct{ name, text, want string }{
		{"range", "[shell.behaviour]\ncompact_at = 150\n", `shell.behaviour.compact_at: 150 is not accepted (accepted: a whole number from 1 to 100)`},
		{"type", "[shell.behaviour]\ncompact_at = \"60\"\n", `shell.behaviour.compact_at: "60" is not accepted (accepted: a whole number from 1 to 100)`},
		{"compact tokens", "[shell.behaviour]\ncompact_tokens = -1\n", `shell.behaviour.compact_tokens: -1 is not accepted (accepted: a whole number, 0 or more)`},
		{"compact tokens type", "[shell.behaviour]\ncompact_tokens = 1.5\n", `shell.behaviour.compact_tokens: 1.5 is not accepted (accepted: a whole number, 0 or more)`},
		{"enum", "[shell.appearance]\nicons = \"boxy\"\n", `shell.appearance.icons: "boxy" is not accepted (accepted: geometric, emoji)`},
		{"cards", "[shell.appearance]\ncards = \"sections\"\n", `shell.appearance.cards: "sections" is not accepted (accepted: float, neon)`},
		{"surface", "[shell.appearance]\nsurface = \"band\"\n", `shell.appearance.surface: "band" is not accepted (accepted: transparent, tinted)`},
		{"density", "[shell.appearance]\ndensity = \"tight\"\n", `shell.appearance.density: "tight" is not accepted (accepted: comfortable, compact, minimal)`},
		{"bool", "[shell.appearance]\nascii = \"on\"\n", `shell.appearance.ascii: "on" is not accepted (accepted: true, false)`},
		{"unknown", "[shell.editor]\nemacs = true\n", `shell.editor.emacs: unknown key`},
		{"schema", "schema = 2\n", `schema: 2 is not accepted (accepted: 1)`},
		{"engram", "[integrations]\nengram = false\n", `integrations.engram: required for now: Jhon's own memory/indexer will make it optional`},
		{"codegraph", "[integrations]\ncodegraph = false\n", `integrations.codegraph: required for now: Jhon's own memory/indexer will make it optional`},
		{"runtime", "[runtimes]\nclaude_code = false\npi = false\n", `runtimes: at least one of claude_code and pi must be true`},
		{"malformed", "[shell\n", "settings.toml"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(write(t, c.text), noEnv)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestOneRuntimeIsEnough(t *testing.T) {
	vals, err := Load(write(t, "[runtimes]\nclaude_code = false\n"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "runtimes.claude_code"); v.Value != false || v.Source != SourceFile {
		t.Errorf("claude_code = %#v (%s)", v.Value, v.Source)
	}
}

const commented = `# My settings, by hand.
schema = 1

[shell.behaviour]
# compaction threshold
compact_at   = 60   # percent
double_esc_cancel = false

# keys I like
[shell.keys]
panel = "alt+j" # the panel
`

func TestSetKeepsCommentsAndUnrelatedLines(t *testing.T) {
	path := write(t, commented)
	v, err := Set(path, "shell.behaviour.compact_at", "45")
	if err != nil {
		t.Fatal(err)
	}
	if v.Value != 45 || v.Source != SourceFile {
		t.Errorf("Set returned %#v", v)
	}
	got := read(t, path)
	want := strings.Replace(commented, "compact_at   = 60   # percent", "compact_at   = 45   # percent", 1)
	if got != want {
		t.Fatalf("file:\n%s\nwant:\n%s", got, want)
	}
	if _, err := Set(path, "shell.keys.panel", "alt+p"); err != nil {
		t.Fatal(err)
	}
	want = strings.Replace(want, `panel = "alt+j" # the panel`, `panel = "alt+p" # the panel`, 1)
	if got := read(t, path); got != want {
		t.Fatalf("file:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetAddsAMissingKeyAndSection(t *testing.T) {
	path := write(t, commented)
	if _, err := Set(path, "shell.behaviour.stall_seconds", "90"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(commented, "double_esc_cancel = false\n", "double_esc_cancel = false\nstall_seconds = 90\n", 1)
	if got := read(t, path); got != want {
		t.Fatalf("file:\n%s\nwant:\n%s", got, want)
	}
	if _, err := Set(path, "shell.appearance.ascii", "true"); err != nil {
		t.Fatal(err)
	}
	want += "\n[shell.appearance]\nascii = true\n"
	if got := read(t, path); got != want {
		t.Fatalf("file:\n%s\nwant:\n%s", got, want)
	}
	vals, err := Load(path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "shell.appearance.ascii"); v.Value != true {
		t.Errorf("ascii = %#v", v.Value)
	}
}

func TestSetCreatesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jhon", "settings.toml")
	if _, err := Set(path, "shell.appearance.ascii", "true"); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.Contains(got, "schema = 1\n") || !strings.Contains(got, "[shell.appearance]\nascii = true\n") {
		t.Fatalf("file:\n%s", got)
	}
}

func TestSetRejects(t *testing.T) {
	path := write(t, commented)
	for _, c := range []struct{ key, value, want string }{
		{"integrations.engram", "false", "integrations.engram: required for now: Jhon's own memory/indexer will make it optional"},
		{"integrations.codegraph", "false", "integrations.codegraph: required for now: Jhon's own memory/indexer will make it optional"},
		{"shell.appearance.animations", "fast", `shell.appearance.animations: "fast" is not accepted (accepted: full, off)`},
		{"shell.behaviour.compact_at", "0", `shell.behaviour.compact_at: 0 is not accepted (accepted: a whole number from 1 to 100)`},
		{"shell.behaviour.compact_tokens", "-5", `shell.behaviour.compact_tokens: -5 is not accepted (accepted: a whole number, 0 or more)`},
		{"shell.behaviour.compact_tokens", "1.5", `shell.behaviour.compact_tokens: "1.5" is not accepted (accepted: a whole number, 0 or more)`},
		{"nope.key", "1", "nope.key: unknown key"},
	} {
		_, err := Set(path, c.key, c.value)
		var inv *InvalidError
		if err == nil || !strings.Contains(err.Error(), c.want) || !errors.As(err, &inv) {
			t.Errorf("Set(%s, %s) = %v, want an InvalidError with %q", c.key, c.value, err, c.want)
		}
	}
	if _, err := Set(path, "runtimes.pi", "false"); err != nil {
		t.Fatal(err)
	}
	if _, err := Set(path, "runtimes.claude_code", "false"); err == nil || !strings.Contains(err.Error(), "at least one of claude_code and pi must be true") {
		t.Errorf("both runtimes off: %v", err)
	}
	if got := read(t, path); !strings.Contains(got, "pi = false") || strings.Contains(got, "claude_code") {
		t.Errorf("a refused set wrote:\n%s", got)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProjectKeys(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "openspec"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "# comment\nstrict_tdd_policy: require # why\ndelivery: \"change\"\nrisk_rules:\n  # note\n  \"a/*.ts\": medium\n  \"b/**\": high\n"
	if err := os.WriteFile(filepath.Join(root, "openspec", "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	vals := Project(root)
	if len(vals) != 3 {
		t.Fatalf("project = %#v", vals)
	}
	if vals[0].Key != "strict_tdd_policy" || vals[0].Value != "require" || vals[0].Source != SourceProject {
		t.Errorf("0 = %#v", vals[0])
	}
	if vals[1].Value != "change" {
		t.Errorf("1 = %#v", vals[1])
	}
	lines, _ := vals[2].Value.([]string)
	if vals[2].Key != "risk_rules" || strings.Join(lines, "|") != `"a/*.ts": medium|"b/**": high` {
		t.Errorf("2 = %#v", vals[2])
	}
	if Project(t.TempDir()) != nil {
		t.Error("no config.yaml: want nil")
	}
}

// T50f (F3): the desktop palette is "desktop"; "ryoku", its name before,
// is still read and set, as "desktop", so an owner's file keeps working.
func TestPaletteDesktopAndItsRyokuAlias(t *testing.T) {
	for _, v := range []string{"desktop", "ryoku"} {
		t.Setenv("JHON_SETTINGS", write(t, "[integrations.theme]\npalette = \""+v+"\"\n"))
		if got := Str("integrations.theme.palette"); got != "desktop" {
			t.Errorf("palette = %q reads %q, want desktop", v, got)
		}
		path := filepath.Join(t.TempDir(), "settings.toml")
		if got, err := Set(path, "integrations.theme.palette", v); err != nil || got.Value != "desktop" {
			t.Errorf("set %s: %v, %v", v, got.Value, err)
		}
		if b, _ := os.ReadFile(path); !strings.Contains(string(b), `palette = "desktop"`) {
			t.Errorf("set %s wrote %q", v, b)
		}
	}
	if got := Accepted("integrations.theme.palette"); got != "rose-ink, desktop (ryoku, its older name)" {
		t.Errorf("accepted: %q", got)
	}
}

// T133e (L1016, G4): integrations.context7 is on unless the file turns it
// off; it is a plain bool, never required, and it has no environment name.
func TestContext7IsOnUnlessTheFileTurnsItOff(t *testing.T) {
	vals, err := Load(write(t, "schema = 1\n"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "integrations.context7"); v.Value != true || v.Source != SourceDefault || v.Kind != "bool" || v.Default != true {
		t.Errorf("with no value: %+v", v)
	}
	path := write(t, "schema = 1\n\n[integrations]\ncontext7 = false\n")
	if vals, err = Load(path, noEnv); err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "integrations.context7"); v.Value != false || v.Source != SourceFile {
		t.Errorf("turned off in the file: %+v", v)
	}
	t.Setenv("JHON_SETTINGS", path)
	if On("integrations.context7") {
		t.Error("On: the file turns it off")
	}
	if got := Accepted("integrations.context7"); got != "true, false" {
		t.Errorf("accepted: %q", got)
	}
	if _, err := Load(write(t, "[integrations]\ncontext7 = \"hosted\"\n"), noEnv); err == nil || !strings.Contains(err.Error(), `integrations.context7: "hosted" is not accepted (accepted: true, false)`) {
		t.Errorf("a value that is not a bool: %v", err)
	}
}

// T135 (L1055): agents.act_on_announce, on unless the file turns it off,
// is a plain bool with no environment name: the main session's turn that
// ends announcing an action of its own is sent back once.
func TestActOnAnnounceIsOnUnlessTheFileTurnsItOff(t *testing.T) {
	vals, err := Load(write(t, "schema = 1\n"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "agents.act_on_announce"); v.Value != true || v.Source != SourceDefault || v.Kind != "bool" || v.Default != true || v.Env != "" {
		t.Errorf("with no value: %+v", v)
	}
	path := write(t, "schema = 1\n\n[agents]\nact_on_announce = false\n")
	t.Setenv("JHON_SETTINGS", path)
	if On("agents.act_on_announce") {
		t.Error("On: the file turns it off")
	}
	if _, err := Load(write(t, "[agents]\nact_on_announce = \"no\"\n"), noEnv); err == nil || !strings.Contains(err.Error(), `agents.act_on_announce: "no" is not accepted (accepted: true, false)`) {
		t.Errorf("a value that is not a bool: %v", err)
	}
}

// On and Str read the effective settings now (JHON_SETTINGS, the
// environment); an unreadable file leaves every opt-in integration off.
func TestOnAndStr(t *testing.T) {
	t.Setenv("JHON_SETTINGS", write(t, "[integrations]\ntelemetry = true\n[integrations.theme]\npalette = \"ryoku\"\n"))
	t.Setenv("JHON_TELEMETRY", "")
	if !On("integrations.telemetry") || Str("integrations.theme.palette") != "desktop" {
		t.Errorf("from the file: telemetry %v palette %q", On("integrations.telemetry"), Str("integrations.theme.palette"))
	}
	t.Setenv("JHON_TELEMETRY", "off")
	if On("integrations.telemetry") {
		t.Error("JHON_TELEMETRY=off wins over the file")
	}
	t.Setenv("JHON_SETTINGS", write(t, "[integrations]\ntelemetry = \"yes\"\n"))
	t.Setenv("JHON_TELEMETRY", "")
	if On("integrations.telemetry") || Str("integrations.theme.palette") != "rose-ink" {
		t.Errorf("an unreadable file: off, and the defaults: %v %q", On("integrations.telemetry"), Str("integrations.theme.palette"))
	}
}

// RuntimeOn reads [runtimes] now; with no file every detected runtime is
// on, as the defaults are (T56, T10g).
func TestRuntimeOn(t *testing.T) {
	home := noRuntimes(t)
	mkdirAll(t, filepath.Join(home, ".claude"))
	mkdirAll(t, filepath.Join(home, ".pi"))
	t.Setenv("JHON_SETTINGS", filepath.Join(t.TempDir(), "absent.toml"))
	for _, rt := range []string{"claude-code", "pi"} {
		if on, _ := RuntimeOn(rt); !on {
			t.Errorf("no file: %s is off", rt)
		}
	}
	t.Setenv("JHON_SETTINGS", write(t, "[runtimes]\nclaude_code = false\n"))
	if on, key := RuntimeOn("claude-code"); on || key != "runtimes.claude_code" {
		t.Errorf("claude_code = false: %v %q", on, key)
	}
	if on, key := RuntimeOn("pi"); !on || key != "runtimes.pi" {
		t.Errorf("pi by default: %v %q", on, key)
	}
	t.Setenv("JHON_SETTINGS", write(t, "[runtimes]\npi = \"no\"\n"))
	if on, _ := RuntimeOn("pi"); !on {
		t.Error("an unreadable file leaves a detected pi on")
	}
	if on, key := RuntimeOn("opencode"); !on || key != "" {
		t.Errorf("a runtime with no key: %v %q", on, key)
	}
}

// A settings file that cannot be read never turns a runtime on that is not
// there: each falls back to its detection, from the default (T10j, N2).
func TestRuntimeSettingUnreadableFileFallsBackToDetection(t *testing.T) {
	home := noRuntimes(t)
	t.Setenv("JHON_SETTINGS", write(t, "[runtimes]\npi = \"no\"\n"))
	for _, rt := range []string{"claude-code", "pi"} {
		if on, _, source := RuntimeSetting(rt); on || source != SourceDefault {
			t.Errorf("unreadable file, nothing detected: %s on=%v source %q", rt, on, source)
		}
	}
	mkdirAll(t, filepath.Join(home, ".claude"))
	if on, _, source := RuntimeSetting("claude-code"); !on || source != SourceDefault {
		t.Errorf("unreadable file, ~/.claude present: on=%v source %q", on, source)
	}
}

// osAbs is p, an absolute POSIX path, as one absolute on the running OS: on
// Windows it takes a drive (C:/opt/example; forward slashes need no TOML
// escaping and Windows reads them as separators).
func osAbs(p string) string {
	if runtime.GOOS == "windows" {
		return "C:" + p
	}
	return p
}

// T128a, T128f: sandbox.mode is on unless set (sandbox.Mode reads that
// default as off where the sandbox does not run, D3), and sandbox.allow_write a list of
// folders, each absolute or under ~/, never /, the home folder or one above.
func TestSandboxKeys(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	opt := osAbs("/opt/example")
	vals, err := Load(filepath.Join(t.TempDir(), "settings.toml"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "sandbox.mode"); v.Value != "on" || v.Source != SourceDefault || v.Kind != "enum" {
		t.Errorf("sandbox.mode = %#v (%s, %s), want on by default, an enum", v.Value, v.Source, v.Kind)
	}
	if v := valueOf(t, vals, "sandbox.allow_write"); !same(v.Value, []string{}) || v.Kind != "paths" {
		t.Errorf("sandbox.allow_write = %#v (%s), want an empty list of paths", v.Value, v.Kind)
	}
	vals, err = Load(write(t, "[sandbox]\nmode = \"on\"\nallow_write = [\""+opt+"\", \"~/.android\"]\n"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "sandbox.mode"); v.Value != "on" || v.Source != SourceFile {
		t.Errorf("sandbox.mode = %#v (%s), want on from the file", v.Value, v.Source)
	}
	if v := valueOf(t, vals, "sandbox.allow_write"); !same(v.Value, []string{opt, "~/.android"}) {
		t.Errorf("sandbox.allow_write = %#v, want the file's list as written", v.Value)
	}
	if got := Expand("~/.android"); got != filepath.Join("/home/u", ".android") {
		t.Errorf("Expand(~/.android) = %s", got)
	}
	for _, c := range []struct{ text, bad string }{
		{"[sandbox]\nmode = \"strict\"\n", `sandbox.mode: "strict" is not accepted (accepted: on, off)`},
		{"[sandbox]\nmode = true\n", `sandbox.mode: true is not accepted (accepted: on, off)`},
		{"[sandbox]\nallow_write = \"" + opt + "\"\n", `sandbox.allow_write: "` + opt + `" is not accepted`},
		{"[sandbox]\nallow_write = [\"relative\"]\n", `sandbox.allow_write: "relative" is not accepted`},
		{"[sandbox]\nallow_write = [\"\"]\n", `sandbox.allow_write: "" is not accepted`},
		{"[sandbox]\nallow_write = [\"/\"]\n", `sandbox.allow_write: "/" is not accepted`},
		{"[sandbox]\nallow_write = [\"/home/u/\"]\n", `sandbox.allow_write: "/home/u/" is not accepted`},
		{"[sandbox]\nallow_write = [\"/home\"]\n", `sandbox.allow_write: "/home" is not accepted`},
		{"[sandbox]\nallow_write = [\"~/\"]\n", `sandbox.allow_write: "~/" is not accepted`},
		{"[sandbox]\nallow_write = [1]\n", `sandbox.allow_write: 1 is not accepted`},
	} {
		if _, err := Load(write(t, c.text), noEnv); err == nil || !strings.Contains(err.Error(), c.bad) {
			t.Errorf("%q: %v, want an error holding %q", c.text, err, c.bad)
		}
	}
}

func TestSetWritesAListOnOneLineAndReplacesIt(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	opt := osAbs("/opt/example")
	path := write(t, "schema = 1\n\n[sandbox]\n# what a command may write\nallow_write = [\""+osAbs("/opt/old")+"\", \""+osAbs("/opt/[x]")+"\"] # mine\nmode = \"off\"\n")
	v, err := Set(path, "sandbox.allow_write", opt+", ~/.android")
	if err != nil {
		t.Fatal(err)
	}
	if !same(v.Value, []string{opt, "~/.android"}) {
		t.Errorf("Set returned %#v", v.Value)
	}
	want := "schema = 1\n\n[sandbox]\n# what a command may write\nallow_write = [\"" + opt + "\", \"~/.android\"] # mine\nmode = \"off\"\n"
	if got := read(t, path); got != want {
		t.Errorf("the file holds\n%s\nwant\n%s", got, want)
	}
	// A refused entry writes nothing.
	for _, raw := range []string{"", "relative", "/", "/home/u", osAbs("/opt/a") + ",," + osAbs("/opt/b")} {
		var inv *InvalidError
		if _, err := Set(path, "sandbox.allow_write", raw); !errors.As(err, &inv) {
			t.Errorf("Set(%q) = %v, want it refused", raw, err)
		}
	}
	if got := read(t, path); got != want {
		t.Errorf("a refused value changed the file:\n%s", got)
	}
	// A list that runs over lines is the owner's to edit.
	multi := write(t, "[sandbox]\nallow_write = [\n  \""+osAbs("/opt/old")+"\",\n]\n")
	if _, err := Set(multi, "sandbox.allow_write", opt); err == nil || !strings.Contains(err.Error(), "edit the file by hand") {
		t.Errorf("Set over a multi-line list = %v, want it left to the owner", err)
	}
}
