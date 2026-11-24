package setupui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/jachavarro2004/jhon-agent/engine/internal/palette"
	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
)

// rose is rose ink as the CLI passes it.
var rose = Colors(palette.Semantic(palette.Fallback, false))

func TestMain(m *testing.M) {
	version.Version = "0.1.0"
	os.Exit(m.Run())
}

// fake is a Backend that records what the model asked of it.
type fake struct {
	det     Detection
	saved   Saved
	checks  []Check
	nChecks int
	steps   []Step
	fail    map[int]error
	ran     []int
	choices Choices

	latest    Release
	latestErr error
	updated   int
	rolled    int
	copied    string
}

func (f *fake) Detect() Detection { return f.det }
func (f *fake) Saved() Saved      { return f.saved }
func (f *fake) Checks(c Choices) []Check {
	f.nChecks++
	return f.checks
}
func (f *fake) Steps(c Choices) []Step { return f.steps }
func (f *fake) RunStep(c Choices, i int) error {
	f.choices = c
	f.ran = append(f.ran, i)
	if err := f.fail[i]; err != nil {
		delete(f.fail, i)
		return err
	}
	return nil
}
func (f *fake) Paths() Paths {
	return Paths{Binary: "~/.local/bin/jhon", Settings: "~/.config/jhon/settings.toml"}
}
func (f *fake) Copy(s string) error      { f.copied = s; return nil }
func (f *fake) Latest() (Release, error) { return f.latest, f.latestErr }
func (f *fake) Update(r Release) error   { f.updated++; return nil }
func (f *fake) Rollback(v string) error  { f.rolled++; return nil }

// engramFix is engram's real fix line, longer than a 100-column frame's row.
const engramFix = "go install github.com/Gentleman-Programming/engram/v3/cmd/engram@latest"

func artboardChecks(missing bool) []Check {
	cs := []Check{
		{Name: "git", OK: true, Detail: "2.47.1"},
		{Name: "claude", OK: true, Detail: "2.1.3  ~/.local/bin/claude"},
		{Name: "node", OK: true, Detail: "22.11.0  for pi"},
		{Name: "engram", OK: true, Detail: "~/go/bin/engram"},
		{Name: "codegraph", OK: true, Detail: "~/.local/bin/codegraph"},
	}
	if missing {
		cs[3] = Check{Name: "engram", Detail: "not on PATH", Fix: engramFix}
		cs[4] = Check{Name: "codegraph", Detail: "not on PATH", Fix: "npm install -g @colbymchenry/codegraph"}
	}
	return cs
}

func newFake() *fake {
	return &fake{
		det:    Detection{Claude: "~/.local/bin/claude"},
		checks: artboardChecks(true),
		steps: []Step{
			{Name: "Download jhon 0.1.0 linux/amd64", Detail: "18.4 MB"},
			{Name: "Verify signature", Detail: "cosign, keyless, jachavarro2004/jhon-agent"},
			{Name: "Install the core", Detail: "~/.local/share/jhon"},
			{Name: "Render runtimes", Detail: "Claude Code, pi"},
			{Name: "Install pi", Detail: "npm, provider claude-bridge"},
			{Name: "Set up integrations", Detail: "Engram, CodeGraph", WritesSettings: true},
		},
		fail: map[int]error{},
		latest: Release{
			Current: "0.1.0", Latest: "0.1.1", Date: "2026-11-21", Verified: true,
			Signature: "cosign, keyless, jachavarro2004/jhon-agent", Source: "jachavarro2004/jhon-agent",
			Notes: []string{
				"setup retries a failed download once before it stops",
				"jhon start pi keeps the profile's provider",
				"the rail folds finished jobs after a minute",
			},
		},
	}
}

func key(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// run is every message cmd yields, a batch flattened.
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, run(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// pump feeds msg and every message its commands yield until none is left.
func pump(m tea.Model, msg tea.Msg) tea.Model {
	queue := []tea.Msg{msg}
	for len(queue) > 0 {
		msg, queue = queue[0], queue[1:]
		if _, ok := msg.(tea.QuitMsg); ok {
			continue
		}
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		queue = append(queue, run(cmd)...)
	}
	return m
}

func press(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		m = pump(m, key(k))
	}
	return m
}

func start(t *testing.T, m Model, w, h int) tea.Model {
	t.Helper()
	var tm tea.Model = m
	for _, msg := range run(tm.Init()) {
		tm = pump(tm, msg)
	}
	return pump(tm, tea.WindowSizeMsg{Width: w, Height: h})
}

func screen(m tea.Model) string { return ansi.Strip(m.View()) }

// frame checks the screen is w x h cells, every line exactly w wide.
func frame(t *testing.T, m tea.Model, w, h int) string {
	t.Helper()
	s := screen(m)
	lines := strings.Split(s, "\n")
	if len(lines) != h {
		t.Fatalf("screen has %d lines, want %d:\n%s", len(lines), h, s)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Errorf("line %d is %d cells wide, want %d: %q", i, got, w, l)
		}
	}
	return s
}

// artboard compares the screen with the artboard's frame in testdata.
func artboard(t *testing.T, m tea.Model, name string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSuffix(string(b), "\n")
	w := ansi.StringWidth(strings.SplitN(want, "\n", 2)[0])
	got := frame(t, m, w, 24)
	// UPDATE_ARTBOARDS=1 rewrites the artboard from the frame; review its diff.
	if os.Getenv("UPDATE_ARTBOARDS") == "1" {
		if err := os.WriteFile(filepath.Join("testdata", name+".txt"), []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if got != want {
		gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
		for i := range wl {
			if i >= len(gl) || gl[i] != wl[i] {
				g := ""
				if i < len(gl) {
					g = gl[i]
				}
				t.Errorf("%s line %d\n got: %q\nwant: %q", name, i, g, wl[i])
			}
		}
	}
}

func contains(t *testing.T, s string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Errorf("screen lacks %q:\n%s", w, s)
		}
	}
}

func setup(t *testing.T, f *fake, w int) tea.Model {
	return start(t, NewSetup(f, rose), w, 24)
}

func TestWelcomeIsTheArtboard(t *testing.T) {
	m := setup(t, newFake(), 100)
	artboard(t, m, "welcome")
	s := frame(t, setup(t, newFake(), 80), 80, 24)
	contains(t, s, " 力  jhon  setup", "0.1.0 ", "▎◐ 1 │", " ○ 6 │", "Welcome to Jhon   step 1 of 6", " Start setup ")
}

func TestRuntimesIsTheArtboard(t *testing.T) {
	m := press(setup(t, newFake(), 100), "enter", "down", "space", "up")
	artboard(t, m, "runtimes")
	s := frame(t, press(setup(t, newFake(), 80), "enter"), 80, 24)
	contains(t, s, "Where will you work from?", "[●] Claude Code", "[ ] pi", "▎◐ 2 │")
}

func TestRuntimesNonePickedRefusesToContinue(t *testing.T) {
	f := newFake()
	m := press(setup(t, f, 100), "enter", "space", "enter")
	artboard(t, m, "runtimes-none")
	// Enter again stays: still runtimes, the checks never ran.
	m = press(m, "enter")
	contains(t, screen(m), "✗ Pick at least one runtime to continue.", "▎  ◐ Runtimes")
	if f.nChecks != 0 {
		t.Errorf("checks ran %d times with no runtime picked", f.nChecks)
	}
	// Picking one clears the error and continues.
	m = press(m, "space")
	if strings.Contains(screen(m), "Pick at least one") {
		t.Errorf("the error stays after a runtime is picked")
	}
	m = press(m, "enter")
	contains(t, screen(m), "▎  ◐ Integrations")
	s := frame(t, press(setup(t, newFake(), 80), "enter", "space", "enter"), 80, 24)
	contains(t, s, "✗ Pick at least one runtime to continue.")
}

func TestIntegrationsIsTheArtboard(t *testing.T) {
	m := press(setup(t, newFake(), 100), "enter", "down", "space", "enter", "space")
	artboard(t, m, "integrations")
	m = press(setup(t, newFake(), 80), "enter", "down", "space", "enter", "space")
	artboard(t, m, "integrations-80")
}

func TestEngramAndCodeGraphCannotBeTurnedOff(t *testing.T) {
	f := newFake()
	f.checks = artboardChecks(false)
	m := press(setup(t, f, 100), "enter", "enter", "up", "space", "up", "space")
	s := screen(m)
	contains(t, s, "[●] Engram", "[●] CodeGraph", "Engram stays on for now: it ships coupled to the core.")
	// Telemetry toggles both ways and is off by default; no herdr row (T24b).
	m = press(m, "down", "down")
	contains(t, screen(m), "[ ] Telemetry", "Palette", "● Rose Ink  ○ Desktop")
	if strings.Contains(screen(m), "herdr") {
		t.Errorf("setup still offers herdr:\n%s", screen(m))
	}
	m = press(m, "space")
	contains(t, screen(m), "[●] Telemetry")
	m = press(m, "space")
	contains(t, screen(m), "[ ] Telemetry")
	press(m, "space", "enter", "enter", "enter")
	if !f.choices.Telemetry || !f.choices.ClaudeCode || f.choices.Pi {
		t.Errorf("choices = %+v, want claude and telemetry only", f.choices)
	}
}

func TestChecksShowEachMissingFixAndLockInstall(t *testing.T) {
	f := newFake()
	m := press(setup(t, f, 100), "enter", "down", "space", "enter", "enter")
	artboard(t, m, "checks")
	m = press(m, "enter")
	contains(t, screen(m), "▎  ◐ Checks", "Install locked")
	m = press(m, "c")
	if f.copied != engramFix {
		t.Errorf("copied %q", f.copied)
	}
	f.checks = artboardChecks(false)
	m = press(m, "r")
	contains(t, screen(m), "Dependencies   all found")
	if strings.Contains(screen(m), "Install locked") {
		t.Errorf("install stays locked after every check passed")
	}
	f2 := newFake()
	s := frame(t, press(setup(t, f2, 80), "enter", "enter", "enter"), 80, 24)
	f3 := newFake()
	f3.checks[0] = Check{Name: "git", Detail: "not on PATH", Advice: "install git with your system's package manager"}
	s3 := screen(press(setup(t, f3, 100), "enter", "enter", "enter"))
	contains(t, s3, "    install git with your system's package manager")
	if strings.Contains(s3, "$ install git") {
		t.Errorf("advice reads as a command:\n%s", s3)
	}
	contains(t, s, "✗ engram", "Install locked")
}

// fixText is the fix lines of a screen joined: each line from its $ (or
// the column under it) to its last non-space cell, spaces dropped.
func fixText(t *testing.T, s string) string {
	t.Helper()
	var parts []string
	in := false
	for _, ln := range strings.Split(ansi.Strip(s), "\n") {
		_, content, _ := strings.Cut(ln, "│")
		i := strings.Index(content, "$ ")
		switch {
		case i >= 0:
			in = true
			parts = append(parts, strings.ReplaceAll(strings.TrimSpace(content[i+2:]), " ", ""))
		case in && strings.HasPrefix(content, "    ") && strings.TrimSpace(content) != "" && !strings.ContainsAny(content, "✓✗─"):
			parts = append(parts, strings.ReplaceAll(strings.TrimSpace(content), " ", ""))
		default:
			in = false
		}
		if in && (strings.Contains(content, "…") || !strings.HasSuffix(content, "  ")) {
			t.Errorf("a fix line is cut or runs into the frame's right margin: %q", ln)
		}
	}
	return strings.Join(parts, "|")
}

func TestAFixCommandWrapsInsteadOfBeingCut(t *testing.T) {
	for _, w := range []int{100, 80} {
		f := newFake()
		s := frame(t, press(setup(t, f, w), "enter", "enter", "enter"), w, 24)
		if strings.Contains(s, "…") {
			t.Errorf("%d columns: the screen cuts a line:\n%s", w, s)
		}
		got := strings.ReplaceAll(fixText(t, s), "|", "")
		for _, want := range []string{engramFix, "npm install -g @colbymchenry/codegraph"} {
			if !strings.Contains(got, strings.ReplaceAll(want, " ", "")) {
				t.Errorf("%d columns: the fix lines %q lack %q:\n%s", w, fixText(t, s), want, s)
			}
		}
		contains(t, s, "Install stays locked while a required check is missing.")
		press(setup(t, f, w), "enter", "enter", "enter", "c")
		if f.copied != engramFix {
			t.Errorf("%d columns: copied %q, want %q", w, f.copied, engramFix)
		}
	}
	// One long token breaks after a /, never inside a word, never with an ellipsis.
	if got := wrapFix("go install "+strings.Repeat("abcdefgh/", 4)+"x@latest", 20, 22); strings.Join(got, "") != "go install "+strings.Repeat("abcdefgh/", 4)+"x@latest" {
		t.Errorf("wrapFix lost text: %q", got)
	} else {
		for _, p := range got {
			if len(p) > 22 || strings.Contains(p, "…") {
				t.Errorf("wrapFix line %q", p)
			}
		}
	}
}

// toInstall is a setup on the Install step's confirmation, every check found.
func toInstall(t *testing.T, f *fake, w int) tea.Model {
	f.checks = artboardChecks(false)
	return press(setup(t, f, w), "enter", "down", "space", "enter", "space", "enter", "enter")
}

func TestInstallWaitsForConfirmation(t *testing.T) {
	f := newFake()
	m := toInstall(t, f, 100)
	contains(t, screen(m), "▎  ◐ Install", "Ready to install   6 steps", " Install ", "Nothing is written until you press enter.")
	if len(f.ran) != 0 {
		t.Errorf("steps ran before the confirmation: %v", f.ran)
	}
}

func TestInstallProgressIsTheArtboard(t *testing.T) {
	f := newFake()
	m := toInstall(t, f, 100)
	m, cmd := m.Update(key("enter"))
	for range 2 {
		m, cmd = m.Update(cmd())
	}
	artboard(t, m, "install")
	contains(t, screen(press(m, "q")), "Cancelling after this step")
}

func TestInstallFailureThenRetry(t *testing.T) {
	f := newFake()
	f.fail[1] = errors.New("the signature does not match\nNothing was installed and your settings were not written.\nThe file may be damaged or not from jachavarro2004/jhon-agent.\nRetry downloads it again; it is safe to repeat.")
	m := press(toInstall(t, f, 100), "enter")
	artboard(t, m, "install-failed")
	m = press(m, "enter")
	if want := []int{0, 1, 0, 1, 2, 3, 4, 5}; !equal(f.ran, want) {
		t.Errorf("ran %v, want %v", f.ran, want)
	}
	artboard(t, m, "done")
}

func TestInstallFailureWithAOneLineError(t *testing.T) {
	f := newFake()
	f.fail[2] = errors.New("disk full")
	m := press(toInstall(t, f, 80), "enter")
	s := frame(t, m, 80, 24)
	contains(t, s, "Install stopped   at step 3 of 6", "✗ Install the core", "disk full",
		"Your settings were not written.", "Retry runs every step again; it is safe to repeat.")
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDoneNamesTheSettingsAndTheNextCommands(t *testing.T) {
	f := newFake()
	m := press(toInstall(t, f, 100), "enter")
	artboard(t, m, "done")
	s := frame(t, press(toInstall(t, newFake(), 80), "enter"), 80, 24)
	contains(t, s, "settings      ~/.config/jhon/settings.toml", "jhon start pi", "jhon setup", "✓ 6 │")
}

func update(t *testing.T, f *fake, w int) tea.Model {
	return start(t, NewUpdate(f, rose), w, 24)
}

func TestUpdateAvailableIsTheArtboard(t *testing.T) {
	f := newFake()
	artboard(t, update(t, f, 100), "update-available")
	m := press(update(t, f, 100), "enter")
	if f.updated != 1 {
		t.Fatalf("update ran %d times", f.updated)
	}
	artboard(t, m, "updated")
	m = press(m, "right", "enter")
	if f.rolled != 1 {
		t.Fatalf("roll back ran %d times", f.rolled)
	}
	contains(t, screen(m), "✓ Rolled back to 0.1.0")
	contains(t, frame(t, update(t, newFake(), 80), 80, 24), "Update available", " Update to 0.1.1 ", "▎◐ 2 │")
}

func TestUpToDateIsTheArtboard(t *testing.T) {
	f := newFake()
	f.latest = Release{Current: "0.1.1", Latest: "0.1.1", Previous: "0.1.0", Source: "jachavarro2004/jhon-agent"}
	artboard(t, update(t, f, 100), "up-to-date")
	m := press(update(t, f, 100), "enter")
	contains(t, screen(m), "▎  ◐ Welcome", "Welcome to Jhon", "jhon  setup")
	frame(t, update(t, f, 80), 80, 24)
}

func TestNetworkErrorIsTheArtboard(t *testing.T) {
	f := newFake()
	f.latestErr = errors.New("api.github.com, jachavarro2004/jhon-agent releases: connection timed out")
	m := update(t, f, 100)
	artboard(t, m, "network-error")
	f.latestErr = nil
	m = press(m, "enter")
	contains(t, screen(m), "Update available")
	frame(t, update(t, f, 80), 80, 24)
}

// TestNoStableReleaseIsNotAFailure (T11e, S11): with only pre-releases
// published, the screen says so as the up-to-date one does, not as an error.
func TestNoStableReleaseIsNotAFailure(t *testing.T) {
	f := newFake()
	f.latest = Release{Current: "0.1.0-rc.1", Latest: "0.1.0-rc.1", NoStable: true, Source: "jachavarro2004/jhon-agent"}
	s := frame(t, update(t, f, 100), 100, 24)
	contains(t, s, "✓ No stable release is published yet", "Jhon 0.1.0-rc.1 is installed; nothing changed.")
	for _, bad := range []string{"✗", "up to date", "GitHub"} {
		if strings.Contains(s, bad) {
			t.Errorf("the screen says %q:\n%s", bad, s)
		}
	}
	frame(t, update(t, f, 80), 80, 24)
}

func TestUpdateNotAvailableYetSaysSo(t *testing.T) {
	f := newFake()
	f.latest, f.latestErr = UpdatesNotAvailableYet{}.Latest()
	s := frame(t, update(t, f, 100), 100, 24)
	contains(t, s, "✗ Updates are not available yet", "Jhon 0.1.0 is still installed and working. Nothing changed.")
	if strings.Contains(s, "GitHub") {
		t.Errorf("a build without updates blames the network:\n%s", s)
	}
}

func TestRunRefusesANonTerminal(t *testing.T) {
	err := Run(strings.NewReader(""), &strings.Builder{}, newFake(), ModeSetup, rose)
	if err == nil || err.Error() != "jhon setup needs a terminal" {
		t.Errorf("Run = %v", err)
	}
	file, err := os.CreateTemp(t.TempDir(), "in")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := Run(file, file, newFake(), ModeUpdate, rose); err == nil || err.Error() != "jhon setup needs a terminal" {
		t.Errorf("Run on a file = %v", err)
	}
}

// bg is the truecolor background sequence of a #rrggbb color.
func bg(hex string) string {
	var r, g, b int
	fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	return fmt.Sprintf("48;2;%d;%d;%d", r, g, b)
}

func TestColorsComeFromRoseInk(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	m := press(setup(t, newFake(), 100), "enter")
	v := m.View()
	for name, c := range map[string]string{
		"frame": palette.Frame, "page": palette.Page, "row": palette.Row, "band": palette.Band,
		"rose fill": palette.Fallback["primaryContainer"],
	} {
		if !strings.Contains(v, bg(c)) {
			t.Errorf("no %s background (%s) on the runtimes screen", name, c)
		}
	}
}

func set(b bool) *bool { return &b }

// TestASecondSetupStartsFromTheSavedSettings: the runtimes and integrations
// start as settings.toml holds them; detection only fills what it leaves unset.
func TestASecondSetupStartsFromTheSavedSettings(t *testing.T) {
	f := newFake()
	f.det = Detection{Claude: "~/.local/bin/claude", Pi: "~/.local/bin/pi"}
	f.saved = Saved{Pi: set(false), Telemetry: set(true)}
	m := press(setup(t, f, 100), "enter")
	contains(t, screen(m), "[●] Claude Code", "[ ] pi", "✓ pi found  ~/.local/bin/pi")
	m = press(m, "enter")
	contains(t, screen(m), "[●] Telemetry")
}

// TestDetectionNeverOverwritesAToggle: a runtime toggled before the
// detection arrives keeps the owner's choice.
func TestDetectionNeverOverwritesAToggle(t *testing.T) {
	f := newFake()
	f.det = Detection{Claude: "~/.local/bin/claude"}
	var m tea.Model = NewSetup(f, rose)
	m = pump(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = press(m, "enter", "space", "space", "down", "space")
	m = pump(m, detectMsg(f.det))
	contains(t, screen(m), "[ ] Claude Code", "[●] pi", "✓ claude found  ~/.local/bin/claude")
}

// TestTheHeaderAndDoneShowTheFullVersion: a stamped dev build shows its +sha.
func TestTheHeaderAndDoneShowTheFullVersion(t *testing.T) {
	defer func(v string) { version.Version = v }(version.Version)
	version.Version = "0.0.0-dev+eba2f16"
	m := setup(t, newFake(), 100)
	if head := strings.Split(screen(m), "\n")[0]; !strings.HasSuffix(head, " 0.0.0-dev+eba2f16 ") {
		t.Errorf("header = %q", head)
	}
	contains(t, screen(press(toInstall(t, newFake(), 100), "enter")), "✓ Jhon 0.0.0-dev+eba2f16 is installed")
}

// TestSpaceOnEngramLeavesItOnWithANote: the verifier's probe (L17).
func TestSpaceOnEngramLeavesItOnWithANote(t *testing.T) {
	f := newFake()
	m := press(setup(t, f, 100), "enter", "enter", "up", "up", "space")
	contains(t, screen(m), "[●] Engram", "Engram stays on for now: it ships coupled to the core.")
	press(m, "enter", "enter")
	if f.choices != (Choices{}) {
		t.Errorf("choices = %+v before any install step", f.choices)
	}
}

// T15.7 (L97): one Palette row, Rose Ink or Desktop (T50f): space steps
// it; a saved one is kept, "ryoku", the desktop palette's older name, too. T15.9 (L103): its two values read as a choice, the chosen
// one marked ●, never a [ ] checkbox.
func TestPaletteIsOneRowInSetup(t *testing.T) {
	f := newFake()
	f.checks = artboardChecks(false)
	// From Telemetry, one row down; the offered integrations' rows follow it.
	m := press(setup(t, f, 100), "enter", "enter", "down")
	s := screen(m)
	contains(t, s, "Palette", "● Rose Ink  ○ Desktop")
	if strings.Contains(s, "] Palette") {
		t.Errorf("the palette is a choice, not a checkbox:\n%s", s)
	}
	if strings.Contains(s, "Follow desktop") || strings.Contains(s, "Palette source") {
		t.Errorf("one palette row only:\n%s", s)
	}
	m = press(m, "space")
	contains(t, screen(m), "○ Rose Ink  ● Desktop")
	press(m, "enter", "enter", "enter")
	if f.choices.Palette != "desktop" {
		t.Errorf("choices = %+v, want the desktop palette", f.choices)
	}
	for _, saved := range []string{"desktop", "ryoku"} {
		g := newFake()
		g.saved.Palette = &saved
		m := press(setup(t, g, 100), "enter", "enter")
		contains(t, screen(m), "○ Rose Ink  ● Desktop")
		contains(t, screen(press(m, "down", "space")), "● Rose Ink  ○ Desktop")
	}
}

// T15.8 (L99): the state column fits its widest value ("Rose Ink", "locked on")
// and a description as long as its column still leaves the gap before it.
func TestTheStateColumnFitsItsValues(t *testing.T) {
	saved := integrations[rowTelemetry]
	t.Cleanup(func() { integrations[rowTelemetry] = saved })
	integrations[rowTelemetry].desc = strings.Repeat("d", 38)
	integrations[rowTelemetry].narrowDesc = strings.Repeat("d", 26)
	for _, w := range []int{100, 80} {
		f := newFake()
		f.checks = artboardChecks(false)
		s := screen(press(setup(t, f, w), "enter", "enter"))
		for _, row := range strings.Split(s, "\n") {
			if !strings.Contains(row, "[ ] Telemetry") {
				continue
			}
			if !strings.Contains(row, "d…  off") {
				t.Errorf("%d: the description runs into the state: %q", w, row)
			}
		}
	}
}

// T17b (L138, L139): integrations.dashboard is retired, /jhon:dashboard
// opens the dashboard whenever asked, so setup offers no Dashboard row.
func TestSetupOffersNoDashboardRow(t *testing.T) {
	f := newFake()
	f.checks = artboardChecks(false)
	m := press(setup(t, f, 100), "enter", "enter")
	s := screen(m)
	contains(t, s, "[●] Engram", "[ ] Telemetry", "Palette")
	if strings.Contains(s, "Dashboard") {
		t.Errorf("setup offers the retired Dashboard row:\n%s", s)
	}
}

// T51: a fix with a fallback advice shows both, the command then the advice.
func TestAFixWithAdviceShowsBoth(t *testing.T) {
	f := newFake()
	f.checks[3] = Check{Name: "engram", Detail: "not on PATH", Fix: "go install example.com/engram@v1", Advice: "if that fails, download engram v1"}
	s := screen(press(setup(t, f, 100), "enter", "enter", "enter"))
	contains(t, s, "  $ go install example.com/engram@v1", "    if that fails, download engram v1")
}

// odd/tasks/jhon-studio.md S3: the Integrations step offers jhon-studio
// under the optional ones, off by default; space turns it on and the
// install gets it chosen; a saved one starts on.
func TestStudioIsAnOptionalRowOffByDefault(t *testing.T) {
	f := newFake()
	f.checks = artboardChecks(false)
	m := press(setup(t, f, 100), "enter", "enter")
	s := screen(m)
	contains(t, s, "[ ] jhon-studio", "presentation videos for any project")
	m = press(m, "down", "down", "space")
	contains(t, screen(m), "▎ [●] jhon-studio", "/jhon-studio:video in Claude Code, /jhon:video in pi")
	press(m, "enter", "enter", "enter")
	if !f.choices.Optional.On("integrations.studio") {
		t.Errorf("choices = %+v, want studio on", f.choices)
	}
	contains(t, frame(t, press(setup(t, newFake(), 80), "enter", "enter"), 80, 24), "[ ] jhon-studio", "project videos")
	g := newFake()
	g.saved.Optional = Toggles("integrations.studio\n")
	contains(t, screen(press(setup(t, g, 100), "enter", "enter")), "[●] jhon-studio")
}

// A check's advice too long for its row wraps, never cut, and a name longer
// than the column keeps two cells before its detail (jhon-studio's manual
// install, odd/tasks/jhon-studio.md S3).
func TestALongAdviceWrapsAndALongNameKeepsItsGap(t *testing.T) {
	const manual = "git clone https://github.com/jachavarro2004/jhon-studio ~/.local/share/jhon-studio && ln -s ~/.local/share/jhon-studio/bin/jhon-studio ~/.local/bin/jhon-studio"
	for _, w := range []int{100, 80} {
		f := newFake()
		f.checks = append(artboardChecks(false), Check{Name: "jhon-studio", Detail: "not on PATH", Advice: manual})
		s := frame(t, press(setup(t, f, w), "enter", "enter", "enter"), w, 24)
		if strings.Contains(s, "…") {
			t.Errorf("%d columns: the advice is cut:\n%s", w, s)
		}
		contains(t, s, "✗ jhon-studio  not on PATH", "    git clone https://github.com/jachavarro2004/jhon-studio")
		var joined string
		for _, ln := range strings.Split(s, "\n") {
			if _, content, ok := strings.Cut(ln, "│"); ok && strings.HasPrefix(content, "    ") {
				joined += strings.TrimSpace(content) + " "
			}
		}
		if !strings.Contains(joined, manual) {
			t.Errorf("%d columns: the advice's lines %q lack %q", w, joined, manual)
		}
	}
}
