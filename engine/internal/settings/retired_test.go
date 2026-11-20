package settings

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// An owner's file from before T15.3 (L76): keys the shell no longer reads,
// renamed values, and the [shell.extra] passthrough.
const oldOwnerFile = `# the owner's settings
schema = 1

[shell.appearance]
card_style = "float"
animations = "potato"

[shell.behaviour]
quiet_tools = false
key_hints = false
stall_seconds = 45 # mine

[shell.notifications.audio]
backend = "auto"
minimum_interval_ms = 2000

[shell.notifications.audio.events]
"context.high" = "builtin:attention"
"subagent.timed_out" = "builtin:error"
"agent.completed" = "builtin:success"

[shell.extra]
visual_customization = '{"statusPlacement":"hidden","headerPlacement":"top","density":"compact","visibility":{"changes":true,"agents":true,"todo":true,"usageCost":false,"modelDetails":true,"contextWarning":true}}'
card_content = '{"content":"minimal"}'
notification_event_subagent_paused = '"builtin:error"'
`

func noteKeys(notes []Note) []string {
	var out []string
	for _, n := range notes {
		out = append(out, n.Key)
	}
	slices.Sort(out)
	return out
}

// T15.3: a removed key is warned and dropped, never an error that would
// throw the whole file away; a renamed value reads as its new name.
func TestRetiredKeysAreNotesNotErrors(t *testing.T) {
	path := write(t, oldOwnerFile)
	vals, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("an old file must still load: %v", err)
	}
	for key, want := range map[string]any{
		"shell.appearance.animations":   "off",
		"shell.appearance.cards":        "float",
		"shell.appearance.status":       "hidden",
		"shell.behaviour.stall_seconds": 45,
	} {
		if v := valueOf(t, vals, key); v.Value != want || v.Source != SourceFile {
			t.Errorf("%s = %#v (%s), want %#v from the file", key, v.Value, v.Source, want)
		}
	}
	for _, v := range vals {
		if strings.HasPrefix(v.Key, "shell.extra.") {
			t.Errorf("[shell.extra] is gone, still listed: %s", v.Key)
		}
	}
	notes, err := Retired(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`shell.appearance.animations`,
		`shell.appearance.card_style`,
		`shell.behaviour.key_hints`,
		`shell.behaviour.quiet_tools`,
		`shell.extra.card_content`,
		`shell.extra.notification_event_subagent_paused`,
		`shell.extra.visual_customization`,
		`shell.notifications.audio.backend`,
		`shell.notifications.audio.events."agent.completed"`,
		`shell.notifications.audio.events."context.high"`,
		`shell.notifications.audio.events."subagent.timed_out"`,
		`shell.notifications.audio.minimum_interval_ms`,
	}
	if got := noteKeys(notes); !slices.Equal(got, want) {
		t.Errorf("notes = %v, want %v", got, want)
	}
	for _, n := range notes {
		switch n.Key {
		case "shell.behaviour.key_hints":
			// T15.13: the rail has no key line; the README lists the keys (T169: the palette went).
			if n.Why != "the keys are listed in the README" {
				t.Errorf("key_hints why = %q", n.Why)
			}
		case "shell.notifications.audio.backend":
			if !n.Silent {
				t.Error("backend had one value: it goes silently")
			}
		case "shell.appearance.card_style":
			if n.Silent || n.Now != `shell.appearance.cards = "float"` || !strings.Contains(n.Why, "shell.appearance.cards") {
				t.Errorf("card_style: a float owner's style reads as shell.appearance.cards: %+v", n)
			}
		case "shell.appearance.animations":
			if n.Now != `shell.appearance.animations = "off"` {
				t.Errorf("potato reads as off: %+v", n)
			}
		case "shell.extra.visual_customization":
			if n.Now != `shell.appearance.status = "hidden"` {
				t.Errorf("the status position moves to its key: %+v", n)
			}
		default:
			if n.Silent || n.Why == "" {
				t.Errorf("%s says why it goes: %+v", n.Key, n)
			}
		}
	}
}

func TestTheSchemaNoLongerKnowsTheRetiredKeys(t *testing.T) {
	keys := Keys()
	for _, gone := range []string{
		"shell.appearance.card_style", "shell.behaviour.quiet_tools", "shell.behaviour.key_hints",
		"shell.notifications.audio.backend", "shell.notifications.audio.minimum_interval_ms", "shell.notifications.audio.coalesce_window_ms",
		`shell.notifications.audio.events."session.shutdown"`, `shell.notifications.audio.events."context.high"`,
		`shell.notifications.audio.events."subagent.queued"`, `shell.notifications.audio.events."subagent.running"`,
		`shell.notifications.audio.events."subagent.waiting"`, `shell.notifications.audio.events."subagent.cancelled"`,
		`shell.notifications.audio.events."subagent.timed_out"`, "shell.extra.visual_profiles",
		`shell.notifications.audio.events."agent.attention"`, `shell.notifications.audio.events."subagent.failed"`,
		"shell.keys.rail", "shell.keys.commands", "shell.keys.tasks",
	} {
		if slices.Contains(keys, gone) || Known(gone) {
			t.Errorf("%s is retired, still in the schema", gone)
		}
	}
	for _, kept := range []string{
		"shell.appearance.icons", "shell.appearance.status", "shell.keys.agents", "shell.keys.usage",
	} {
		if !slices.Contains(keys, kept) {
			t.Errorf("%s is missing", kept)
		}
	}
}

// T15.3 adds: the icon family and the status line's switch. (Its rail sheet
// and palette keys went in T169, L1438: TestThePaletteAndSheetKeysAreRetired.)
func TestTheNewKeys(t *testing.T) {
	vals, err := Load(filepath.Join(t.TempDir(), "settings.toml"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"shell.appearance.icons": "geometric", "shell.appearance.status": "auto"} {
		if v := valueOf(t, vals, key); v.Value != want || v.Source != SourceDefault {
			t.Errorf("%s = %#v (%s)", key, v.Value, v.Source)
		}
	}
	for _, refused := range [][2]string{{"shell.appearance.icons", "fancy"}, {"shell.appearance.status", "bottom"}, {"shell.appearance.animations", "reduced"}} {
		if _, err := Set(filepath.Join(t.TempDir(), "s.toml"), refused[0], refused[1]); err == nil {
			t.Errorf("set %s %s must be refused", refused[0], refused[1])
		}
	}
}

// T15.12d (L153 answer 1): the card style is back as shell.appearance.cards:
// a float or neon card_style reads as it; any other value (sections) goes,
// saying where the style lives now; an explicit cards key wins.
func TestCardStyleMovesToTheCardsKey(t *testing.T) {
	for was, want := range map[string]string{"float": "float", "neon": "neon", "sections": ""} {
		path := write(t, "[shell.appearance]\ncard_style = \""+was+"\"\n")
		vals, err := Load(path, noEnv)
		if err != nil {
			t.Fatal(err)
		}
		v := valueOf(t, vals, "shell.appearance.cards")
		notes, _ := Retired(path)
		if len(notes) != 1 || !strings.Contains(notes[0].Why, "shell.appearance.cards") {
			t.Fatalf("%s: notes = %+v", was, notes)
		}
		if want == "" {
			if v.Value != "float" || v.Source != SourceDefault || notes[0].Now != "" {
				t.Errorf("%s: cards = %#v (%s), note %+v; want the default and no successor", was, v.Value, v.Source, notes[0])
			}
			continue
		}
		if v.Value != want || v.Source != SourceFile || notes[0].Now != `shell.appearance.cards = "`+want+`"` {
			t.Errorf("%s: cards = %#v (%s), note %+v", was, v.Value, v.Source, notes[0])
		}
	}
	path := write(t, "[shell.appearance]\ncards = \"neon\"\ncard_style = \"float\"\n")
	vals, _ := Load(path, noEnv)
	if v := valueOf(t, vals, "shell.appearance.cards"); v.Value != "neon" {
		t.Errorf("the explicit key wins: %#v", v.Value)
	}
	if notes, _ := Retired(path); len(notes) != 1 || notes[0].Ignored != "shell.appearance.cards" {
		t.Errorf("the note says cards is set: %+v", notes)
	}
}

func TestStatusPositionMovesToItsKey(t *testing.T) {
	for placement, want := range map[string]string{"right": "auto", "auto": "auto", "bottom": "auto", "hidden": "hidden"} {
		path := write(t, "[shell.extra]\nvisual_customization = '{\"statusPlacement\":\""+placement+"\"}'\n")
		vals, err := Load(path, noEnv)
		if err != nil {
			t.Fatal(err)
		}
		if v := valueOf(t, vals, "shell.appearance.status"); v.Value != want {
			t.Errorf("%s: status = %#v, want %s", placement, v.Value, want)
		}
	}
	// An explicit key wins over the old JSON.
	path := write(t, "[shell.appearance]\nstatus = \"auto\"\n\n[shell.extra]\nvisual_customization = '{\"statusPlacement\":\"hidden\"}'\n")
	vals, err := Load(path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "shell.appearance.status"); v.Value != "auto" {
		t.Errorf("the key wins: %#v", v.Value)
	}
}

func TestSetRefusesARetiredKeyAndSaysWhy(t *testing.T) {
	_, err := Set(filepath.Join(t.TempDir(), "settings.toml"), "shell.appearance.card_style", "float")
	var inv *InvalidError
	if !errors.As(err, &inv) || !strings.Contains(inv.Msg, "removed") || !strings.Contains(inv.Msg, "shell.appearance.cards") {
		t.Errorf("err = %v, want a refusal that says it was removed and why", err)
	}
	// An old file still takes a new value: Set never builds on it as broken.
	path := write(t, oldOwnerFile)
	if _, err := Set(path, "shell.appearance.icons", "emoji"); err != nil {
		t.Errorf("set on an old file: %v", err)
	}
}

// `jhon settings migrate` drops the retired keys and writes the renamed
// values, keeps every comment and unrelated line, and backs the file up;
// it tells what it did, and a second run has nothing to do.
func TestCleanupDropsRetiredKeysAndKeepsTheRest(t *testing.T) {
	path := write(t, oldOwnerFile)
	rep, err := Cleanup(path, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Done || len(rep.Notes) != 12 || rep.Backup == "" {
		t.Fatalf("report = %+v", rep)
	}
	if b, err := os.ReadFile(rep.Backup); err != nil || string(b) != oldOwnerFile {
		t.Errorf("the backup holds the old file: %v", err)
	}
	b, _ := os.ReadFile(path)
	text := string(b)
	for _, gone := range []string{"card_style", "quiet_tools", "key_hints", "backend", "minimum_interval_ms", "context.high", "subagent.timed_out", "[shell.extra]", "visual_customization", "card_content", "notification_event", "[shell.notifications", "agent.completed"} {
		if strings.Contains(text, gone) {
			t.Errorf("%s is still in the file:\n%s", gone, text)
		}
	}
	for _, kept := range []string{"# the owner's settings", "stall_seconds = 45 # mine", `animations = "off"`, `cards = "float"`, `status = "hidden"`} {
		if !strings.Contains(text, kept) {
			t.Errorf("%q is missing:\n%s", kept, text)
		}
	}
	if notes, err := Retired(path); err != nil || len(notes) != 0 {
		t.Errorf("the file reads clean: %v %v", notes, err)
	}
	if _, err := Load(path, noEnv); err != nil {
		t.Errorf("the file loads: %v", err)
	}
	again, err := Cleanup(path, time.Now())
	if err != nil || again.Done {
		t.Errorf("a second run has nothing to do: %+v %v", again, err)
	}
}

// backups lists the settings-backup-* directories beside path.
func backups(t *testing.T, path string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "settings-backup-*"))
	return m
}

// T15.6: an inline table cannot be cleaned line by line; Cleanup refuses,
// says what it refused and why, and leaves the file and the directory as they were.
func TestCleanupRefusesInlineTableWithoutWriting(t *testing.T) {
	const in = "schema = 1\n[shell.notifications]\naudio = { backend = \"auto\", minimum_interval_ms = 5 }\n"
	path := write(t, in)
	_, err := Cleanup(path, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("Cleanup did not refuse")
	}
	msg := err.Error()
	if strings.Contains(msg, "<nil>") || !strings.Contains(msg, "shell.notifications.audio.backend") || !strings.Contains(msg, "left as is") {
		t.Errorf("the refusal names the key still there and that the file was left as is: %q", msg)
	}
	if b, _ := os.ReadFile(path); string(b) != in {
		t.Errorf("the file changed:\n%s", b)
	}
	if b := backups(t, path); len(b) != 0 {
		t.Errorf("a backup was made: %v", b)
	}
}

func TestCleanupLeavesCleanFileUntouched(t *testing.T) {
	const in = "schema = 1\n# hi\n[shell.appearance]\nascii = true\n"
	path := write(t, in)
	before, _ := os.Stat(path)
	rep, err := Cleanup(path, time.Now())
	if err != nil || rep.Done {
		t.Fatalf("a clean file has nothing to do: %+v %v", rep, err)
	}
	after, _ := os.Stat(path)
	if b, _ := os.ReadFile(path); string(b) != in || !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("the file was touched: %q %v %v", b, before.ModTime(), after.ModTime())
	}
	if b := backups(t, path); len(b) != 0 {
		t.Errorf("a backup was made: %v", b)
	}
}

const mixedFile = `# mine, keep me
schema = 1

[shell.appearance]
# the cards
card_style = "float"
animations = "potato"
ascii = true # plain
icons = "emoji"

[shell.behaviour]
quiet_tools = false
key_hints = false
compact_at = 70 # later
compact_tokens = 40
stall_seconds = 30

[shell.editor]
vim = true

[shell.keys]
panel = "alt+p"
tasks = "off"
usage = "off"

[shell.notifications]
enabled = true

[shell.notifications.audio]
backend = "auto"
minimum_interval_ms = 2000

[shell.notifications.audio.events]
"context.high" = "builtin:attention"
"agent.failed" = "none" # quiet

[shell.extra]
visual_customization = '{"statusPlacement":"hidden","headerPlacement":"top","density":"compact","visibility":{"changes":true,"agents":true,"todo":true,"usageCost":true,"modelDetails":true,"contextWarning":true}}'
card_content = '{"content":"minimal"}'
`

const mixedCleaned = `# mine, keep me
schema = 1

[shell.appearance]
# the cards
ascii = true # plain
icons = "emoji"
animations = "off"
cards = "float"
status = "hidden"

[shell.behaviour]
compact_at = 70 # later
compact_tokens = 40
stall_seconds = 30

[shell.keys]
panel = "alt+p"
usage = "off"
agents = "off"
`

func TestCleanupKeepsCommentsAndKeptValues(t *testing.T) {
	path := write(t, mixedFile)
	rep, err := Cleanup(path, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	if err != nil || !rep.Done {
		t.Fatalf("%+v %v", rep, err)
	}
	if b, err := os.ReadFile(rep.Backup); err != nil || string(b) != mixedFile {
		t.Errorf("the backup is the input byte for byte: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != mixedCleaned {
		t.Errorf("cleaned file:\n%s\nwant:\n%s", b, mixedCleaned)
	}
	again, err := Cleanup(path, time.Now())
	if err != nil || again.Done {
		t.Errorf("a second run has nothing to do: %+v %v", again, err)
	}
}

// T15.7 (L97): the palette is one setting, Rose Ink or the desktop palette
// ("ryoku", its older name, reads as "desktop": T50f). follow_desktop
// and source are retired, and since T29 E6 (L329) their values pick nothing:
// each is a note, and only palette names the palette.
func TestThePaletteIsOneSetting(t *testing.T) {
	for _, c := range []struct {
		name, file, want string
	}{
		{"follow on", "schema = 1\n[integrations.theme]\nfollow_desktop = true\nsource = \"ryoku\"\n", "rose-ink"},
		{"follow on, no source", "schema = 1\n[integrations.theme]\nfollow_desktop = true\n", "rose-ink"},
		{"palette ryoku", "schema = 1\n[integrations.theme]\nfollow_desktop = false\npalette = \"ryoku\"\n", "desktop"},
		{"follow off", "schema = 1\n[integrations.theme]\nfollow_desktop = false\nsource = \"ryoku\"\n", "rose-ink"},
		{"absent", "schema = 1\n", "rose-ink"},
		{"the new key wins", "schema = 1\n[integrations.theme]\nfollow_desktop = true\npalette = \"rose-ink\"\n", "rose-ink"},
	} {
		path := write(t, c.file)
		vals, err := Load(path, noEnv)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if v, _ := Find(vals, "integrations.theme.palette"); v.Value != c.want {
			t.Errorf("%s: palette = %v, want %s", c.name, v.Value, c.want)
		}
		for _, gone := range []string{"integrations.theme.follow_desktop", "integrations.theme.source"} {
			if _, ok := Find(vals, gone); ok {
				t.Errorf("%s: %s is still a setting", c.name, gone)
			}
		}
	}
	notes, err := Retired(write(t, "schema = 1\n[integrations.theme]\nfollow_desktop = true\nsource = \"ryoku\"\n"))
	if err != nil || len(notes) != 2 {
		t.Fatalf("notes = %+v %v", notes, err)
	}
	if notes[0].Key != "integrations.theme.follow_desktop" || notes[0].Now != "" || notes[0].Why != `the palette is integrations.theme.palette now: set it to "desktop" to follow the desktop palette` ||
		notes[1].Key != "integrations.theme.source" || notes[1].Now != "" || notes[1].Why != `the palette names its source: integrations.theme.palette = "desktop"` {
		t.Errorf("notes = %+v", notes)
	}
	path := write(t, "schema = 1\n\n[integrations.theme]\nfollow_desktop = true # mine\nsource = \"ryoku\"\n")
	if rep, err := Cleanup(path, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)); err != nil || !rep.Done {
		t.Fatalf("%+v %v", rep, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "schema = 1\n" {
		t.Errorf("cleaned:\n%s", b)
	}
	if _, err := Set(path, "integrations.theme.palette", "dusk"); err == nil {
		t.Error("a palette that is neither rose-ink nor desktop is refused")
	}
}

// T15.9 (L103, B1): a retired key that remaps to a key the file sets itself
// is ignored: the explicit key wins when reading and when migrating, the
// retired key goes, and the note says the new key is set.
func TestAnExplicitNewKeyWinsOverARetiredOne(t *testing.T) {
	for _, tc := range []struct {
		file, setting, want, retired, cleaned string
	}{
		{"schema = 1\n[shell.appearance]\nstatus = \"hidden\"\n[shell.extra]\nvisual_customization = '{\"statusPlacement\":\"right\"}'\n", "shell.appearance.status", "hidden", "shell.extra.visual_customization",
			"schema = 1\n[shell.appearance]\nstatus = \"hidden\"\n"},
		{"schema = 1\n[shell.keys]\nagents = \"alt+a\"\ntasks = \"ctrl+b\"\n", "shell.keys.agents", "alt+a", "shell.keys.tasks",
			"schema = 1\n[shell.keys]\nagents = \"alt+a\"\n"},
	} {
		path := write(t, tc.file)
		get := func() any {
			vals, err := Load(path, noEnv)
			if err != nil {
				t.Fatal(err)
			}
			v, _ := Find(vals, tc.setting)
			return v.Value
		}
		if got := get(); got != tc.want {
			t.Errorf("%s before: %v", tc.setting, got)
		}
		notes, _ := Retired(path)
		if len(notes) != 1 || notes[0].Key != tc.retired || notes[0].Ignored != tc.setting || notes[0].Now != "" {
			t.Errorf("the note says %s is set: %+v", tc.setting, notes)
		}
		rep, err := Cleanup(path, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
		if err != nil || !rep.Done {
			t.Fatalf("%+v %v", rep, err)
		}
		if got := get(); got != tc.want {
			t.Errorf("%s after: %v", tc.setting, got)
		}
		if b, _ := os.ReadFile(path); string(b) != tc.cleaned {
			t.Errorf("cleaned:\n%s", b)
		}
		if b, _ := os.ReadFile(rep.Backup); string(b) != tc.file {
			t.Errorf("the backup is the file byte for byte:\n%s", b)
		}
	}
}

// T167 (L1437, L1438): alt+u, which opens /jhon:usage, is a setting again
// (T15.10 had retired the key with the old /jhon:usage): shell.keys.usage,
// alt+u by default, JHON_SHELL_USAGE_KEY overriding it as the other keys'
// variables do. A file that sets it reads it, and migrate leaves it.
func TestTheUsageKeyIsASetting(t *testing.T) {
	if !Known("shell.keys.usage") || !slices.Contains(Keys(), "shell.keys.usage") {
		t.Fatal("shell.keys.usage is not in the schema")
	}
	if why := RetiredWhy("shell.keys.usage"); why != "" {
		t.Errorf("shell.keys.usage still reads as retired: %q", why)
	}
	none := filepath.Join(t.TempDir(), "settings.toml")
	defaults, err := Load(none, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, defaults, "shell.keys.usage"); v.Value != "alt+u" || v.Source != SourceDefault {
		t.Errorf("usage default = %+v, want alt+u", v)
	}
	for raw, want := range map[string]string{"ctrl+shift+u": "ctrl+shift+u", "off": "off", "": "off"} {
		vals, err := Load(none, envOf(map[string]string{"JHON_SHELL_USAGE_KEY": raw}))
		if err != nil {
			t.Fatal(err)
		}
		if v := valueOf(t, vals, "shell.keys.usage"); v.Value != want || v.Source != SourceEnv || v.Env != "JHON_SHELL_USAGE_KEY" {
			t.Errorf("JHON_SHELL_USAGE_KEY=%q: %+v, want %s from the variable", raw, v, want)
		}
	}
	const file = "schema = 1\n[shell.keys]\nusage = \"alt+y\"\npanel = \"alt+j\"\n"
	path := write(t, file)
	vals, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("a file with the usage key must load: %v", err)
	}
	if v := valueOf(t, vals, "shell.keys.usage"); v.Value != "alt+y" || v.Source != SourceFile {
		t.Errorf("usage = %+v, want alt+y from the file", v)
	}
	if notes, err := Retired(path); err != nil || len(notes) != 0 {
		t.Errorf("notes = %+v, %v; want none", notes, err)
	}
	if rep, err := Cleanup(path, time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)); err != nil || rep.Done {
		t.Errorf("cleanup = %+v, %v; want nothing to do", rep, err)
	}
	if b, _ := os.ReadFile(path); string(b) != file {
		t.Errorf("the file changed:\n%s", b)
	}
	if v, err := Set(path, "shell.keys.usage", "alt+o"); err != nil || v.Value != "alt+o" {
		t.Errorf("set usage = %+v, %v", v, err)
	}
}

// T167: shell.keys.tasks opened /jhon:agents, so it is shell.keys.agents
// now, alt+s by default, JHON_SHELL_TASKS_KEY still overriding it. A file
// that sets the old key reads its value as the new one; set refuses the old
// key (removed in v0.4); migrate moves the value, backed up.
const tasksWhy = "the key opens /jhon:agents, so it is shell.keys.agents now"

func TestTheTasksKeyIsTheAgentsKey(t *testing.T) {
	if Known("shell.keys.tasks") || slices.Contains(Keys(), "shell.keys.tasks") || !slices.Contains(Keys(), "shell.keys.agents") {
		t.Fatalf("keys = %v, want shell.keys.agents and no shell.keys.tasks", Keys())
	}
	if got := RetiredWhy("shell.keys.tasks"); got != tasksWhy {
		t.Errorf("RetiredWhy = %q, want %q", got, tasksWhy)
	}
	if got := RetiredIn("shell.keys.tasks"); got != "v0.4" {
		t.Errorf("RetiredIn = %q, want v0.4", got)
	}
	none := filepath.Join(t.TempDir(), "settings.toml")
	defaults, _ := Load(none, noEnv)
	if v := valueOf(t, defaults, "shell.keys.agents"); v.Value != "alt+s" || v.Source != SourceDefault {
		t.Errorf("agents default = %+v, want alt+s", v)
	}
	env, _ := Load(none, envOf(map[string]string{"JHON_SHELL_TASKS_KEY": "ctrl+b"}))
	if v := valueOf(t, env, "shell.keys.agents"); v.Value != "ctrl+b" || v.Env != "JHON_SHELL_TASKS_KEY" {
		t.Errorf("agents from the variable = %+v", v)
	}
	const old = "# mine\nschema = 1\n\n[shell.keys]\ntasks = \"ctrl+b\" # mine\npanel = \"alt+j\"\n"
	path := write(t, old)
	vals, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("a file with shell.keys.tasks must still load: %v", err)
	}
	if v := valueOf(t, vals, "shell.keys.agents"); v.Value != "ctrl+b" || v.Source != SourceFile {
		t.Errorf("agents = %+v, want ctrl+b from the file", v)
	}
	notes, err := Retired(path)
	if err != nil || !slices.Equal(noteKeys(notes), []string{"shell.keys.tasks"}) {
		t.Fatalf("notes = %+v, %v", notes, err)
	}
	if n := notes[0]; n.Why != tasksWhy || n.Now != `shell.keys.agents = "ctrl+b"` || n.Silent {
		t.Errorf("note = %+v", n)
	}
	_, err = Set(path, "shell.keys.tasks", "alt+b")
	var inv *InvalidError
	if want := "shell.keys.tasks: removed in v0.4: " + tasksWhy; err == nil || err.Error() != want || !errors.As(err, &inv) {
		t.Errorf("Set = %v, want %q", err, want)
	}
	if b, _ := os.ReadFile(path); string(b) != old {
		t.Errorf("a refused set wrote the file:\n%s", b)
	}
	rep, err := Cleanup(path, time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	if err != nil || !rep.Done || rep.Backup == "" {
		t.Fatalf("cleanup = %+v, %v", rep, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "# mine\nschema = 1\n\n[shell.keys]\npanel = \"alt+j\"\nagents = \"ctrl+b\"\n" {
		t.Errorf("cleaned file:\n%s", b)
	}
	if b, _ := os.ReadFile(rep.Backup); string(b) != old {
		t.Errorf("the backup is the file byte for byte:\n%s", b)
	}
	vals, _ = Load(path, noEnv)
	if v := valueOf(t, vals, "shell.keys.agents"); v.Value != "ctrl+b" || v.Source != SourceFile {
		t.Errorf("agents after migrate = %+v", v)
	}
}

// T17b (L138, answer 5): /jhon:dashboard opens the dashboard at the current
// session whenever asked, so integrations.dashboard gates nothing. A file
// that still sets it loads, says why once, and `jhon settings migrate`
// drops it, backed up, keeping the other integrations.
func TestTheDashboardKeyIsRetired(t *testing.T) {
	const file = "schema = 1\n[integrations]\nengram = true\ndashboard = true\ntelemetry = false\n"
	if Known("integrations.dashboard") || slices.Contains(Keys(), "integrations.dashboard") {
		t.Error("integrations.dashboard is retired, still in the schema")
	}
	path := write(t, file)
	if _, err := Load(path, noEnv); err != nil {
		t.Fatalf("a file with the dashboard key must still load: %v", err)
	}
	notes, err := Retired(path)
	if err != nil || !slices.Equal(noteKeys(notes), []string{"integrations.dashboard"}) || notes[0].Silent || !strings.Contains(notes[0].Why, "/jhon:dashboard") {
		t.Fatalf("notes = %+v, %v; want one note for integrations.dashboard that names /jhon:dashboard", notes, err)
	}
	rep, err := Cleanup(path, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	if err != nil || !rep.Done || rep.Backup == "" {
		t.Fatalf("cleanup = %+v, %v", rep, err)
	}
	b, _ := os.ReadFile(path)
	if text := string(b); text != "schema = 1\n[integrations]\nengram = true\ntelemetry = false\n" {
		t.Errorf("the dashboard key goes, the others stay:\n%s", text)
	}
}

// T155 (L1377, L1394): the context ladder is two keys, compact_at and
// compact_tokens; the delegation notice and the fresh-session notice come
// from them. The sub-agent cap is gone (the wrap-up notice at
// child_backstop_tokens stays). A file that still sets a removed key, even
// to a value the old schema refused, loads; the key is a note that says
// why, `set` refuses it, and Cleanup drops it.
func TestTheContextLadderIsTwoKeys(t *testing.T) {
	keys := Keys()
	for _, kept := range []string{"shell.behaviour.compact_at", "shell.behaviour.compact_tokens", "shell.behaviour.child_backstop_tokens"} {
		if !slices.Contains(keys, kept) {
			t.Errorf("%s is missing", kept)
		}
	}
	why := map[string]string{
		"shell.behaviour.fresh_session_at":     "the fresh-session notice comes ten points of the window before shell.behaviour.compact_at",
		"shell.behaviour.fresh_session_tokens": "the fresh-session notice comes at three quarters of shell.behaviour.compact_tokens",
		"shell.behaviour.backstop_tokens":      "the delegation notice comes at half of shell.behaviour.compact_tokens, or 15% of the window if that comes first",
		"shell.behaviour.child_cap_tokens":     "sub-agents are no longer capped; past shell.behaviour.child_backstop_tokens they hear once to wrap up",
	}
	for key, w := range why {
		if slices.Contains(keys, key) || Known(key) {
			t.Errorf("%s is retired, still in the schema", key)
		}
		if got := RetiredWhy(key); got != w {
			t.Errorf("RetiredWhy(%s) = %q, want %q", key, got, w)
		}
		if got := RetiredIn(key); got != "v0.4" {
			t.Errorf("RetiredIn(%s) = %q, want v0.4", key, got)
		}
	}
	old := "# mine\nschema = 1\n\n[shell.behaviour]\ncompact_at = 55 # mine\nfresh_session_at = 45\nfresh_session_tokens = \"150K\"\nbackstop_tokens = -1\nchild_backstop_tokens = 60000\nchild_cap_tokens = 0\n"
	path := write(t, old)
	vals, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("a file with the old ladder keys must still load: %v", err)
	}
	for key, want := range map[string]any{"shell.behaviour.compact_at": 55, "shell.behaviour.child_backstop_tokens": 60000} {
		if v := valueOf(t, vals, key); v.Value != want || v.Source != SourceFile {
			t.Errorf("%s = %#v (%s), want %#v from the file", key, v.Value, v.Source, want)
		}
	}
	notes, err := Retired(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := noteKeys(notes), []string{"shell.behaviour.backstop_tokens", "shell.behaviour.child_cap_tokens", "shell.behaviour.fresh_session_at", "shell.behaviour.fresh_session_tokens"}; !slices.Equal(got, want) {
		t.Errorf("notes = %v, want %v", got, want)
	}
	for _, n := range notes {
		if n.Silent || n.Why != why[n.Key] || n.Now != "" {
			t.Errorf("%s: %+v", n.Key, n)
		}
	}
	for key := range why {
		_, err := Set(path, key, "1000")
		var inv *InvalidError
		if want := key + ": removed in v0.4: " + why[key]; err == nil || err.Error() != want || !errors.As(err, &inv) {
			t.Errorf("Set(%s) = %v, want %q", key, err, want)
		}
	}
	rep, err := Cleanup(path, time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	if err != nil || !rep.Done {
		t.Fatalf("%+v %v", rep, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "# mine\nschema = 1\n\n[shell.behaviour]\ncompact_at = 55 # mine\nchild_backstop_tokens = 60000\n" {
		t.Errorf("cleaned file:\n%s", b)
	}
}

// T169 (L1438): the notification sounds are gone, and every
// shell.notifications key goes with them. A file that still sets one, even
// to a value the old schema refused, loads; the key is a note that says
// why, `set` refuses it, and Cleanup drops it with its sections.
func TestTheNotificationKeysAreRetired(t *testing.T) {
	for _, key := range Keys() {
		if strings.HasPrefix(key, "shell.notifications.") {
			t.Errorf("%s is retired, still in the schema", key)
		}
	}
	const why = "the notification sounds were removed from the shell"
	for key, in := range map[string]string{
		"shell.notifications.enabled":                           "v0.4",
		`shell.notifications.audio.events."agent.failed"`:       "v0.4",
		`shell.notifications.audio.events."subagent.completed"`: "v0.4",
		"shell.notifications.audio.minimum_interval_ms":         "v0.1",
	} {
		if Known(key) {
			t.Errorf("%s is still known", key)
		}
		if got := RetiredWhy(key); got != why {
			t.Errorf("RetiredWhy(%s) = %q, want %q", key, got, why)
		}
		if got := RetiredIn(key); got != in {
			t.Errorf("RetiredIn(%s) = %q, want %s", key, got, in)
		}
	}
	old := "# mine\nschema = 1\n\n[shell.behaviour]\nstall_seconds = 45 # mine\n\n[shell.notifications]\nenabled = \"yes\"\n\n[shell.notifications.audio.events]\n\"agent.failed\" = \"none\"\n\"agent.completed\" = \"builtin:success\"\n"
	path := write(t, old)
	vals, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("a file with the notification keys must still load: %v", err)
	}
	if v := valueOf(t, vals, "shell.behaviour.stall_seconds"); v.Value != 45 || v.Source != SourceFile {
		t.Errorf("stall_seconds = %+v, want 45 from the file", v)
	}
	notes, err := Retired(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := noteKeys(notes), []string{`shell.notifications.audio.events."agent.completed"`, `shell.notifications.audio.events."agent.failed"`, "shell.notifications.enabled"}; !slices.Equal(got, want) {
		t.Errorf("notes = %v, want %v", got, want)
	}
	for _, n := range notes {
		if n.Silent || n.Why != why || n.Now != "" {
			t.Errorf("%s: %+v", n.Key, n)
		}
	}
	_, err = Set(path, "shell.notifications.enabled", "true")
	var inv *InvalidError
	if want := "shell.notifications.enabled: removed in v0.4: " + why; err == nil || err.Error() != want || !errors.As(err, &inv) {
		t.Errorf("Set = %v, want %q", err, want)
	}
	if b, _ := os.ReadFile(path); string(b) != old {
		t.Errorf("a refused set wrote the file:\n%s", b)
	}
	rep, err := Cleanup(path, time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	if err != nil || !rep.Done || len(rep.Notes) != 3 {
		t.Fatalf("%+v %v", rep, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "# mine\nschema = 1\n\n[shell.behaviour]\nstall_seconds = 45 # mine\n" {
		t.Errorf("cleaned file:\n%s", b)
	}
}

// T169 (L1437, L1438): the command palette (/jhon:commands, alt+k) and the
// panel sheet (/jhon:panel, alt+r) are gone, and their keys with them. A
// file that still sets one loads, its variable is no longer read, the key is
// a note that says why, `set` refuses it (removed in v0.4), and Cleanup
// drops it.
func TestThePaletteAndSheetKeysAreRetired(t *testing.T) {
	why := map[string]string{
		"shell.keys.rail":     "the panel sheet (/jhon:panel) was removed from the shell",
		"shell.keys.commands": "the command palette (/jhon:commands) was removed from the shell",
	}
	for key, w := range why {
		if Known(key) || slices.Contains(Keys(), key) {
			t.Errorf("%s is retired, still in the schema", key)
		}
		if got := RetiredWhy(key); got != w {
			t.Errorf("RetiredWhy(%s) = %q, want %q", key, got, w)
		}
		if got := RetiredIn(key); got != "v0.4" {
			t.Errorf("RetiredIn(%s) = %q, want v0.4", key, got)
		}
	}
	vals, err := Load(filepath.Join(t.TempDir(), "settings.toml"), envOf(map[string]string{"JHON_SHELL_RAIL_KEY": "ctrl+r", "JHON_SHELL_COMMANDS_KEY": "off"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vals {
		if v.Env == "JHON_SHELL_RAIL_KEY" || v.Env == "JHON_SHELL_COMMANDS_KEY" {
			t.Errorf("%s still reads %s", v.Key, v.Env)
		}
	}
	old := "schema = 1\n\n[shell.keys]\npanel = \"alt+j\" # mine\nrail = \"alt+r\"\ncommands = 5\n"
	path := write(t, old)
	vals, err = Load(path, noEnv)
	if err != nil {
		t.Fatalf("a file with the old keys, even a value the old schema refused, must load: %v", err)
	}
	if v := valueOf(t, vals, "shell.keys.panel"); v.Value != "alt+j" || v.Source != SourceFile {
		t.Errorf("panel = %+v", v)
	}
	notes, err := Retired(path)
	if err != nil || !slices.Equal(noteKeys(notes), []string{"shell.keys.commands", "shell.keys.rail"}) {
		t.Fatalf("notes = %+v, %v", notes, err)
	}
	for _, n := range notes {
		if n.Silent || n.Why != why[n.Key] || n.Now != "" {
			t.Errorf("%+v", n)
		}
	}
	for key := range why {
		_, err := Set(path, key, "alt+x")
		if want := key + ": removed in v0.4: " + why[key]; err == nil || err.Error() != want {
			t.Errorf("Set(%s) = %v, want %q", key, err, want)
		}
	}
	if b, _ := os.ReadFile(path); string(b) != old {
		t.Errorf("a refused set wrote the file:\n%s", b)
	}
	rep, err := Cleanup(path, time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	if err != nil || !rep.Done || len(rep.Notes) != 2 {
		t.Fatalf("%+v %v", rep, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "schema = 1\n\n[shell.keys]\npanel = \"alt+j\" # mine\n" {
		t.Errorf("cleaned file:\n%s", b)
	}
}
