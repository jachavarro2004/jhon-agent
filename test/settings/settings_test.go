// `jhon settings`: the owner's settings (settings.toml, schema 1), read and
// written through the binary, in scratch homes only.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// home is a scratch home: HOME, XDG_CONFIG_HOME and XDG_CACHE_HOME inside a
// temp dir, and no inherited variable that could reach a real config.
type home struct {
	dir, config string
	env         []string
}

func newHome(t *testing.T, extra ...string) home {
	t.Helper()
	dir := t.TempDir()
	h := home{dir: dir, config: filepath.Join(dir, "xdg-config")}
	var env []string
	for _, kv := range support.Env() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "JHON_") || strings.HasPrefix(k, "XDG_") || k == "PI_CODING_AGENT_DIR" || k == "HOME" {
			continue
		}
		env = append(env, kv)
	}
	h.env = append(env, "HOME="+dir, "XDG_CONFIG_HOME="+h.config, "XDG_CACHE_HOME="+filepath.Join(dir, "xdg-cache"), "JHON_TELEMETRY=off")
	h.env = append(h.env, extra...)
	return h
}

func (h home) file() string { return filepath.Join(h.config, "jhon", "settings.toml") }

func (h home) jhon(t *testing.T, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, h.dir, h.env, append([]string{"settings"}, args...)...)
}

type row struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Source string `json:"source"`
	Env    string `json:"env"`
}

type listing struct {
	Schema   int    `json:"schema"`
	Path     string `json:"path"`
	Settings []row  `json:"settings"`
	Project  *struct {
		Root     string `json:"root"`
		File     string `json:"file"`
		Settings []row  `json:"settings"`
	} `json:"project"`
}

func list(t *testing.T, h home, dir string) listing {
	t.Helper()
	r := support.Jhon(t, dir, h.env, "settings", "list", "--json")
	if r.Code != 0 {
		t.Fatalf("list --json: %d\n%s", r.Code, r.Out())
	}
	var l listing
	if err := json.Unmarshal([]byte(r.Stdout), &l); err != nil {
		t.Fatalf("list --json is not JSON: %v\n%s", err, r.Stdout)
	}
	return l
}

func find(l listing, key string) (row, bool) {
	for _, r := range l.Settings {
		if r.Key == key {
			return r, true
		}
	}
	return row{}, false
}

func TestListJSONShapeAndSources(t *testing.T) {
	h := newHome(t, "JHON_SHELL_COMPACT_AT=70")
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": "schema = 1\n[shell.notifications]\nenabled = true\n"})
	l := list(t, h, h.dir)
	if l.Schema != 1 || l.Path != h.file() {
		t.Errorf("schema %d, path %s", l.Schema, l.Path)
	}
	for key, want := range map[string]row{
		"shell.notifications.enabled": {Value: true, Source: "file"},
		"shell.behaviour.compact_at":  {Value: float64(70), Source: "env", Env: "JHON_SHELL_COMPACT_AT"},
		"integrations.engram":         {Value: true, Source: "default"},
		"shell.appearance.icons":      {Value: "geometric", Source: "default"},
	} {
		got, ok := find(l, key)
		if !ok || got.Value != want.Value || got.Source != want.Source || got.Env != want.Env {
			t.Errorf("%s = %#v, want %#v", key, got, want)
		}
	}
	if l.Project != nil {
		t.Errorf("project outside a project: %#v", l.Project)
	}
	// The plain listing names every key with its value and source.
	r := h.jhon(t)
	if r.Code != 0 || !strings.Contains(r.Stdout, "shell.notifications.enabled") || !strings.Contains(r.Stdout, "file") || !strings.Contains(r.Stdout, "JHON_SHELL_COMPACT_AT") {
		t.Errorf("jhon settings: %d\n%s", r.Code, r.Out())
	}
}

// T15.15 (L179): the engine is the one source of each key's kind, accepted
// values and default; --json adds them beside key, value and source.
func TestListJSONDescribesEachKey(t *testing.T) {
	h := newHome(t)
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": "schema = 1\n[shell.appearance]\nicons = \"emoji\"\n"})
	r := support.Jhon(t, h.dir, h.env, "settings", "list", "--json")
	if r.Code != 0 {
		t.Fatalf("list --json: %d\n%s", r.Code, r.Out())
	}
	var out struct {
		Settings []map[string]any `json:"settings"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err)
	}
	by := map[string]map[string]any{}
	for _, e := range out.Settings {
		for _, field := range []string{"key", "value", "source", "kind", "default"} {
			if _, ok := e[field]; !ok {
				t.Errorf("%v has no %q", e["key"], field)
			}
		}
		by[e["key"].(string)] = e
	}
	for key, want := range map[string]map[string]any{
		"shell.appearance.icons":                          {"value": "emoji", "source": "file", "kind": "enum", "values": []any{"geometric", "emoji"}, "default": "geometric"},
		"shell.notifications.enabled":                     {"value": false, "source": "default", "kind": "bool", "default": false},
		"shell.behaviour.compact_at":                      {"kind": "int", "default": float64(60), "min": float64(1), "max": float64(100)},
		"shell.behaviour.stall_seconds":                   {"kind": "int", "default": float64(75), "min": float64(0)},
		"shell.behaviour.thinking_stall_seconds":          {"kind": "int", "default": float64(300), "min": float64(0), "source": "default", "value": float64(300)},
		"shell.keys.panel":                                {"kind": "key", "default": "alt+j"},
		`shell.notifications.audio.events."agent.failed"`: {"kind": "sound", "default": "builtin:error"},
		"pi.profile":                                      {"kind": "name", "default": "default"},
		"claude_code.connectors":                          {"value": false, "source": "default", "kind": "bool", "default": false},
	} {
		got := by[key]
		for field, v := range want {
			if g, _ := json.Marshal(got[field]); string(g) != mustJSON(t, v) {
				t.Errorf("%s.%s = %s, want %s", key, field, g, mustJSON(t, v))
			}
		}
	}
	if _, ok := by["shell.behaviour.stall_seconds"]["max"]; ok {
		t.Error("an unbounded int has no max")
	}
	if _, ok := by["shell.notifications.enabled"]["values"]; ok {
		t.Error("a bool lists no values")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestListShowsTheProjectRulesReadOnly(t *testing.T) {
	h := newHome(t)
	repo := filepath.Join(h.dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	support.GitRepo(t, repo)
	cfg := "strict_tdd_policy: require\nrisk_rules:\n  \"a/*\": high\n"
	support.Write(t, repo, map[string]string{"openspec/config.yaml": cfg})
	sub := filepath.Join(repo, "openspec")
	l := list(t, h, sub)
	if l.Project == nil || len(l.Project.Settings) != 2 {
		t.Fatalf("project = %#v", l.Project)
	}
	if p := l.Project.Settings[0]; p.Key != "strict_tdd_policy" || p.Value != "require" || p.Source != "project" {
		t.Errorf("project[0] = %#v", p)
	}
	if got := support.Read(t, filepath.Join(repo, "openspec", "config.yaml")); got != cfg {
		t.Errorf("config.yaml changed: %q", got)
	}
}

func TestGet(t *testing.T) {
	h := newHome(t)
	r := h.jhon(t, "get", "shell.keys.panel")
	if r.Code != 0 || r.Stdout != "alt+j\n" {
		t.Errorf("get: %d %q %q", r.Code, r.Stdout, r.Stderr)
	}
	r = h.jhon(t, "get", "shell.keys.panel", "--json")
	var got row
	if r.Code != 0 || json.Unmarshal([]byte(r.Stdout), &got) != nil || got.Key != "shell.keys.panel" || got.Value != "alt+j" || got.Source != "default" {
		t.Errorf("get --json: %d %q", r.Code, r.Out())
	}
	r = h.jhon(t, "get", "shell.nope")
	if r.Code != 2 || !strings.Contains(r.Stderr, "shell.nope: unknown key") {
		t.Errorf("unknown key: %d %q", r.Code, r.Out())
	}
	r = h.jhon(t, "get", "shell.appearance.card_style")
	if r.Code != 2 || !strings.Contains(r.Stderr, "shell.appearance.card_style: removed in v0.1: the card style is shell.appearance.cards now (float or neon)") {
		t.Errorf("a retired key says why: %d %q", r.Code, r.Out())
	}
}

func TestSetWritesKeepingComments(t *testing.T) {
	h := newHome(t)
	text := "# mine\nschema = 1\n\n[shell.behaviour]\ncompact_at = 60 # percent\n"
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": text})
	r := h.jhon(t, "set", "shell.behaviour.compact_at", "45")
	if r.Code != 0 {
		t.Fatalf("set: %d\n%s", r.Code, r.Out())
	}
	if got := support.Read(t, h.file()); got != "# mine\nschema = 1\n\n[shell.behaviour]\ncompact_at = 45 # percent\n" {
		t.Errorf("file = %q", got)
	}
	r = h.jhon(t, "set", "shell.appearance.icons", "emoji")
	if r.Code != 0 || !strings.HasSuffix(support.Read(t, h.file()), "\n[shell.appearance]\nicons = \"emoji\"\n") {
		t.Errorf("set a new section: %d %q\n%s", r.Code, r.Out(), support.Read(t, h.file()))
	}
	if r := h.jhon(t, "get", "shell.appearance.icons"); r.Stdout != "emoji\n" {
		t.Errorf("get after set: %q", r.Out())
	}
}

func TestSetRejectsAnInvalidValue(t *testing.T) {
	h := newHome(t)
	for _, c := range []struct{ key, value, want string }{
		{"shell.appearance.icons", "boxy", `shell.appearance.icons: "boxy" is not accepted (accepted: geometric, emoji)`},
		{"shell.appearance.card_style", "float", "shell.appearance.card_style: removed in v0.1: the card style is shell.appearance.cards now (float or neon)"},
		{"shell.appearance.cards", "sections", `shell.appearance.cards: "sections" is not accepted (accepted: float, neon)`},
		{"shell.appearance.surface", "band", `shell.appearance.surface: "band" is not accepted (accepted: transparent, tinted)`},
		{"shell.behaviour.compact_at", "101", "accepted: a whole number from 1 to 100"},
		{"integrations.engram", "false", "required for now: Jhon's own memory/indexer will make it optional"},
		{"integrations.codegraph", "false", "required for now: Jhon's own memory/indexer will make it optional"},
	} {
		r := h.jhon(t, "set", c.key, c.value)
		if r.Code != 2 || !strings.Contains(r.Stderr, c.want) {
			t.Errorf("set %s %s: %d %q", c.key, c.value, r.Code, r.Out())
		}
	}
	if _, err := os.Stat(h.file()); !os.IsNotExist(err) {
		t.Error("a refused set wrote settings.toml")
	}
}

func TestAnInvalidFileFailsTheRead(t *testing.T) {
	h := newHome(t)
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": "[runtimes]\nclaude_code = false\npi = false\n"})
	r := h.jhon(t, "list")
	if r.Code != 1 || !strings.Contains(r.Stderr, "runtimes: at least one of claude_code and pi must be true") {
		t.Errorf("list: %d %q", r.Code, r.Out())
	}
}

func TestJHONSettingsNamesTheFile(t *testing.T) {
	h := newHome(t)
	path := filepath.Join(h.dir, "elsewhere.toml")
	h.env = append(h.env, "JHON_SETTINGS="+path)
	if r := h.jhon(t, "set", "shell.notifications.enabled", "true"); r.Code != 0 {
		t.Fatalf("set: %d %q", r.Code, r.Out())
	}
	if !strings.Contains(support.Read(t, path), "enabled = true") {
		t.Error("JHON_SETTINGS was not written")
	}
	if _, err := os.Stat(h.file()); !os.IsNotExist(err) {
		t.Error("the XDG file was written too")
	}
}

func TestHelp(t *testing.T) {
	h := newHome(t)
	r := h.jhon(t, "--help")
	if r.Code != 0 || !strings.Contains(r.Stdout, "jhon settings set <key> <value>") {
		t.Errorf("--help: %d %q", r.Code, r.Out())
	}
	r = support.Jhon(t, h.dir, h.env, "help")
	if !strings.Contains(r.Out(), "jhon settings") {
		t.Errorf("jhon help has no settings row:\n%s", r.Out())
	}
	r = h.jhon(t, "frob")
	if r.Code != 2 {
		t.Errorf("unknown subcommand: %d %q", r.Code, r.Out())
	}
}

// shellHome writes an old machine: the pre-v0.1 shell files, which nothing
// reads any more (T29 E6), and the profiles.json the first-run migration
// still reads.
func shellHome(t *testing.T, h home) string {
	t.Helper()
	support.Write(t, filepath.Join(h.dir, ".config", "jhon"), map[string]string{"profiles.json": `{"active":"default","profiles":{"default":{}}}` + "\n"})
	dir := filepath.Join(h.dir, ".pi", "jhon", "jhon-shell")
	support.Write(t, dir, map[string]string{
		"card-style.json":    `{"schema":"jhon-shell.card-style/v1","style":"neon"}` + "\n",
		"animations.json":    `{"schema":"jhon-shell.animations/v1","policy":"potato"}` + "\n",
		"vim.json":           `{"schema":"jhon-shell.vim/v1","policy":"on"}` + "\n",
		"card-content.json":  `{"schema":"jhon-shell.card-content/v1","content":"minimal"}` + "\n",
		"notifications.json": `{"schema":"jhon-shell.notifications/v1","enabled":true,"audio":{"backend":"auto","minimumIntervalMs":1000,"coalesceWindowMs":300,"events":{}}}` + "\n",
	})
	return dir
}

func TestListMigratesOnce(t *testing.T) {
	h := newHome(t)
	shellHome(t, h)
	profiles := filepath.Join(h.dir, ".config", "jhon", "profiles.json")
	h.env = append(h.env, "JHON_TELEMETRY=on")
	l := list(t, h, h.dir)
	for key, want := range map[string]any{
		"pi.profile": "default",
	} {
		if got, ok := find(l, key); !ok || got.Value != want || got.Source != "file" {
			t.Errorf("%s = %#v, want %#v from the file", key, got, want)
		}
	}
	// telemetry is on (JHON_TELEMETRY=on): the file says true and the variable agrees.
	if got, _ := find(l, "integrations.telemetry"); got.Value != true {
		t.Errorf("telemetry = %#v", got)
	}
	// integrations.dashboard is retired (T17b): the migration writes nothing for it.
	if got, ok := find(l, "integrations.dashboard"); ok {
		t.Errorf("dashboard = %#v, want no such setting", got)
	}
	backups, _ := filepath.Glob(filepath.Join(h.config, "jhon", "settings-backup-*", "profiles.json"))
	if len(backups) != 1 {
		t.Errorf("backups = %v", backups)
	}
	if _, err := os.Stat(profiles); err != nil {
		t.Error("the original was removed")
	}
	before := support.Read(t, h.file())
	r := h.jhon(t, "migrate")
	if r.Code != 0 || !strings.Contains(r.Stdout, "nothing to migrate") || support.Read(t, h.file()) != before {
		t.Errorf("second migrate: %d %q", r.Code, r.Out())
	}
	if again, _ := filepath.Glob(filepath.Join(h.config, "jhon", "settings-backup-*")); len(again) != 1 {
		t.Errorf("backups after a second run = %v", again)
	}
}

// The old shell's files under $PI_CODING_AGENT_DIR/jhon-shell are not read
// any more (T29 E6, L329): with nothing else, there is nothing to migrate.
func TestMigrateNoLongerReadsTheShellFiles(t *testing.T) {
	h := newHome(t)
	agent := filepath.Join(h.dir, "agent")
	support.Write(t, filepath.Join(agent, "jhon-shell"), map[string]string{"double-esc-cancel.json": `{"schema":"jhon-shell.double-esc-cancel/v1","policy":"on"}` + "\n"})
	h.env = append(h.env, "PI_CODING_AGENT_DIR="+agent, "JHON_SHELL_CONFIG_HOME="+filepath.Join(agent, "jhon-shell"))
	r := h.jhon(t, "migrate", "--json")
	var rep struct {
		Done   bool              `json:"done"`
		Reason string            `json:"reason"`
		Mapped []json.RawMessage `json:"mapped"`
	}
	if r.Code != 0 || json.Unmarshal([]byte(r.Stdout), &rep) != nil || rep.Done || rep.Reason != "no old settings files" || len(rep.Mapped) != 0 {
		t.Fatalf("migrate --json: %d %q", r.Code, r.Out())
	}
	if _, err := os.Stat(h.file()); !os.IsNotExist(err) {
		t.Error("settings.toml was written from the old shell files")
	}
}

// A sync reads the integrations from settings.toml, so on a machine with only
// the old settings it migrates them first: otherwise telemetry would read as
// off on the first sync (L204). A desktop palette on the machine no longer
// writes palette = "ryoku" (T29 E6, L329).
func TestSyncMigratesTheOldSettingsFirst(t *testing.T) {
	h := newHome(t)
	shellHome(t, h)
	h.env = append(h.env, "JHON_TELEMETRY=on")
	support.Write(t, filepath.Join(h.dir, ".cache", "ryoku"), map[string]string{"colors.json": "{}\n"})
	r := support.Jhon(t, h.dir, h.env, "sync") // the scratch home has no release to build; only the migration matters
	// Read the file, not `jhon settings list`, which would migrate on its own.
	b, err := os.ReadFile(h.file())
	if err != nil {
		t.Fatalf("no settings.toml after a sync: %v (exit %d, stderr %q)", err, r.Code, r.Stderr)
	}
	if !strings.Contains(string(b), "telemetry = true") || strings.Contains(string(b), "palette") {
		t.Fatalf("settings.toml after a sync, want telemetry and no palette:\n%s", b)
	}
}

// The old shell opened the dashboard from its Audit tab (L208); the switch
// is retired now (T17b), so migrating from it writes no dashboard key.
func TestMigrationWritesNoDashboardKeyForAnOldShell(t *testing.T) {
	h := newHome(t)
	shellHome(t, h)
	support.Jhon(t, h.dir, h.env, "settings", "list")
	if text := support.Read(t, h.file()); strings.Contains(text, "dashboard") {
		t.Fatalf("settings.toml after migrating an old shell:\n%s", text)
	}
}

// T15.3 (L76): a file that still holds a key the shell no longer reads is
// read, never refused: list says once, on stderr, what goes and why, and
// lists the retired keys in --json; `jhon settings migrate` drops them,
// backed up, keeping the owner's comments, and says what it did.
func TestRetiredKeysAreWarnedThenMigrated(t *testing.T) {
	h := newHome(t)
	old := "# mine\nschema = 1\n\n[shell.appearance]\ncard_style = \"float\"\nanimations = \"potato\"\n\n[shell.behaviour]\nquiet_tools = false\nstall_seconds = 45\n\n[shell.notifications.audio]\nbackend = \"auto\"\n\n[shell.extra]\nvisual_customization = '{\"statusPlacement\":\"right\"}'\n"
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": old})
	r := h.jhon(t, "list")
	if r.Code != 0 {
		t.Fatalf("an old file still lists: %d\n%s", r.Code, r.Out())
	}
	for _, said := range []string{
		"shell.appearance.card_style = \"float\" reads as shell.appearance.cards = \"float\"",
		"shell.appearance.animations = \"potato\" reads as shell.appearance.animations = \"off\"",
		"shell.behaviour.quiet_tools = false is no longer read",
		"shell.extra.visual_customization",
		"jhon settings migrate",
	} {
		if !strings.Contains(r.Stderr, said) {
			t.Errorf("list's warning lacks %q:\n%s", said, r.Stderr)
		}
	}
	if strings.Contains(r.Stderr, "backend") {
		t.Errorf("the backend goes silently:\n%s", r.Stderr)
	}
	if support.Read(t, h.file()) != old {
		t.Error("list never rewrites the owner's file")
	}
	j := support.Jhon(t, h.dir, h.env, "settings", "list", "--json")
	var out struct {
		Settings []row `json:"settings"`
		Retired  []struct {
			Key string `json:"key"`
			Why string `json:"why"`
		} `json:"retired"`
	}
	if j.Code != 0 || json.Unmarshal([]byte(j.Stdout), &out) != nil || len(out.Retired) != 5 {
		t.Fatalf("list --json: %d %q", j.Code, j.Out())
	}
	l := list(t, h, h.dir)
	if got, _ := find(l, "shell.appearance.status"); got.Value != "auto" || got.Source != "file" {
		t.Errorf("right reads as auto: %#v", got)
	}
	m := h.jhon(t, "migrate")
	if m.Code != 0 || !strings.Contains(m.Stdout, "dropped 5 retired setting") || !strings.Contains(m.Stdout, "backup:") {
		t.Fatalf("migrate: %d\n%s", m.Code, m.Out())
	}
	now := support.Read(t, h.file())
	for _, gone := range []string{"card_style", "quiet_tools", "backend", "[shell.extra]"} {
		if strings.Contains(now, gone) {
			t.Errorf("%s stays:\n%s", gone, now)
		}
	}
	for _, kept := range []string{"# mine", "stall_seconds = 45", "animations = \"off\"", "cards = \"float\"", "status = \"auto\""} {
		if !strings.Contains(now, kept) {
			t.Errorf("%q is missing:\n%s", kept, now)
		}
	}
	if again := h.jhon(t, "list"); again.Stderr != "" {
		t.Errorf("after migrate, list warns no more: %q", again.Stderr)
	}
}

// T15.9 (L103, B1): the owner sets the new key over a retired one; the
// migration keeps it. list says the retired key is ignored, and after
// `migrate` get still prints the owner's value.
func TestAnExplicitNewKeySurvivesTheMigration(t *testing.T) {
	h := newHome(t)
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": "schema = 1\n\n[integrations.theme]\nfollow_desktop = true\n"})
	if r := h.jhon(t, "set", "integrations.theme.palette", "rose-ink"); r.Code != 0 {
		t.Fatalf("set: %d %s", r.Code, r.Out())
	}
	if r := h.jhon(t, "list"); !strings.Contains(r.Stderr, "integrations.theme.follow_desktop = true is no longer read: ") {
		t.Errorf("list's warning:\n%s", r.Stderr)
	}
	if r := h.jhon(t, "migrate"); r.Code != 0 {
		t.Fatalf("migrate: %d %s", r.Code, r.Out())
	}
	if r := h.jhon(t, "get", "integrations.theme.palette"); strings.TrimSpace(r.Stdout) != "rose-ink" {
		t.Errorf("get after migrate: %q", r.Out())
	}
	if text := support.Read(t, h.file()); strings.Contains(text, "follow_desktop") || !strings.Contains(text, `palette = "rose-ink"`) {
		t.Errorf("the file:\n%s", text)
	}
}

// T15.11 (L121): shell.keys.usage went with /jhon:usage (L117). A file that
// still holds it, with its own comment, between keys that stay: list reads
// it without touching the file, get refuses the key, migrate backs the file
// up byte for byte and drops exactly that line, and a second migrate is a no-op.
func TestTheRetiredUsageKeyEndToEnd(t *testing.T) {
	h := newHome(t)
	old := "schema = 1\n\n[shell.keys]\npanel = \"alt+j\"\nusage = \"alt+u\" # x\nrail = \"alt+r\"\n"
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": old})
	if r := h.jhon(t, "list"); r.Code != 0 || !strings.Contains(r.Stderr, "shell.keys.usage") {
		t.Fatalf("list: %d\n%s", r.Code, r.Out())
	}
	if support.Read(t, h.file()) != old {
		t.Fatal("list rewrote the owner's file")
	}
	if r := h.jhon(t, "get", "shell.keys.usage"); r.Code != 2 {
		t.Errorf("get shell.keys.usage: %d %q, want 2", r.Code, r.Out())
	}
	if r := h.jhon(t, "migrate"); r.Code != 0 || !strings.Contains(r.Stdout, "backup:") {
		t.Fatalf("migrate: %d\n%s", r.Code, r.Out())
	}
	backups, _ := filepath.Glob(filepath.Join(h.config, "jhon", "settings-backup-*", "settings.toml"))
	if len(backups) != 1 || support.Read(t, backups[0]) != old {
		t.Fatalf("backups = %v, want one copy of the file as it was", backups)
	}
	want := "schema = 1\n\n[shell.keys]\npanel = \"alt+j\"\nrail = \"alt+r\"\n"
	if now := support.Read(t, h.file()); now != want {
		t.Errorf("after migrate:\n%s\nwant:\n%s", now, want)
	}
	if r := h.jhon(t, "migrate"); r.Code != 0 || !strings.Contains(r.Stdout, "nothing to migrate") || support.Read(t, h.file()) != want {
		t.Errorf("second migrate: %d %q", r.Code, r.Out())
	}
	if again, _ := filepath.Glob(filepath.Join(h.config, "jhon", "settings-backup-*")); len(again) != 1 {
		t.Errorf("backups after a second run = %v", again)
	}
}

// T17b (L138, answer 5): integrations.dashboard is retired as
// shell.keys.usage was: `list` warns and leaves the file, get and set exit 2
// saying it was removed, migrate drops it once with a backup.
func TestTheRetiredDashboardKeyEndToEnd(t *testing.T) {
	h := newHome(t)
	old := "schema = 1\n\n[integrations]\nengram = true\ndashboard = true # x\ntelemetry = false\n"
	support.Write(t, filepath.Join(h.config, "jhon"), map[string]string{"settings.toml": old})
	r := h.jhon(t, "list")
	if r.Code != 0 || strings.Count(r.Stderr, "integrations.dashboard") != 1 || !strings.Contains(r.Stderr, "/jhon:dashboard") {
		t.Fatalf("list warns once and says why: %d\n%s", r.Code, r.Out())
	}
	if strings.Contains(r.Stdout, "integrations.dashboard") {
		t.Errorf("list shows a retired key as a setting:\n%s", r.Stdout)
	}
	if support.Read(t, h.file()) != old {
		t.Fatal("list rewrote the owner's file")
	}
	for _, args := range [][]string{{"get", "integrations.dashboard"}, {"set", "integrations.dashboard", "true"}} {
		if r := h.jhon(t, args...); r.Code != 2 || !strings.Contains(r.Stderr, "integrations.dashboard: removed in v0.1: ") {
			t.Errorf("%s: %d %q, want 2 and removed in v0.1", strings.Join(args, " "), r.Code, r.Out())
		}
	}
	if support.Read(t, h.file()) != old {
		t.Fatal("a refused set wrote the file")
	}
	if r := h.jhon(t, "migrate"); r.Code != 0 || !strings.Contains(r.Stdout, "backup:") {
		t.Fatalf("migrate: %d\n%s", r.Code, r.Out())
	}
	backups, _ := filepath.Glob(filepath.Join(h.config, "jhon", "settings-backup-*", "settings.toml"))
	if len(backups) != 1 || support.Read(t, backups[0]) != old {
		t.Fatalf("backups = %v, want one copy of the file as it was", backups)
	}
	want := "schema = 1\n\n[integrations]\nengram = true\ntelemetry = false\n"
	if now := support.Read(t, h.file()); now != want {
		t.Errorf("after migrate:\n%s\nwant:\n%s", now, want)
	}
	if r := h.jhon(t, "migrate"); r.Code != 0 || !strings.Contains(r.Stdout, "nothing to migrate") || support.Read(t, h.file()) != want {
		t.Errorf("second migrate: %d %q", r.Code, r.Out())
	}
	if r := h.jhon(t, "list"); strings.Contains(r.Stderr, "integrations.dashboard") {
		t.Errorf("list after migrate still warns: %q", r.Stderr)
	}
}

// T15.12d (L153 answers 1-2): the Gentle Shell's card style and chrome
// surface are shell settings, float and transparent by default.
func TestCardsAndSurfaceAreShellSettings(t *testing.T) {
	h := newHome(t)
	l := list(t, h, h.dir)
	for key, want := range map[string]string{"shell.appearance.cards": "float", "shell.appearance.surface": "transparent"} {
		if got, ok := find(l, key); !ok || got.Value != want || got.Source != "default" {
			t.Errorf("%s = %#v, want %s (default)", key, got, want)
		}
	}
	// A slice, not a map: set appends each new key in call order, so the
	// file's line order below follows this order.
	for _, kv := range [][2]string{{"shell.appearance.cards", "neon"}, {"shell.appearance.surface", "tinted"}} {
		key, value := kv[0], kv[1]
		if r := h.jhon(t, "set", key, value); r.Code != 0 {
			t.Fatalf("set %s %s: %d %q", key, value, r.Code, r.Out())
		}
		if r := h.jhon(t, "get", key); r.Code != 0 || r.Stdout != value+"\n" {
			t.Errorf("get %s: %d %q", key, r.Code, r.Out())
		}
	}
	if text := support.Read(t, h.file()); !strings.Contains(text, "[shell.appearance]\ncards = \"neon\"\nsurface = \"tinted\"\n") {
		t.Errorf("settings.toml:\n%s", text)
	}
}
