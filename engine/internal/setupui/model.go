package setupui

import (
	"errors"
	manifests "github.com/jachavarro2004/jhon-agent/engine/internal/integrations"

	tea "github.com/charmbracelet/bubbletea"
)

// The setup steps, in rail order.
const (
	sWelcome = iota
	sRuntimes
	sIntegrations
	sChecks
	sInstall
	sDone
)

var setupRail = []string{"Welcome", "Runtimes", "Integrations", "Checks", "Install", "Done"}

// The update screens.
const (
	uChecking = iota
	uCheckFailed
	uAvailable
	uApplying
	uApplyFailed
	uUpdated
	uCurrent
	uRollingBack
	uRolledBack
)

var updateRail = []string{"Check", "Review", "Update", "Done"}

// The install step's phases.
const (
	iConfirm = iota
	iRunning
	iFailed
)

// The integration rows: the required integrations' first, locked on
// (Engram and CodeGraph, manifests.Core, read by name; view.go's integrations
// holds one row for each), then Jhon's own, then one for each offered
// optional integration (offered), from rowCount on.
const (
	rowEngram = iota
	rowCodeGraph
	rowTelemetry
	rowPalette
	rowCount
)

// Model is the setup and update TUI.
type Model struct {
	b    Backend
	st   styles
	mode Mode
	w, h int

	step   int // the setup step, or the update screen
	cursor int
	button int
	note   string

	det Detection
	c   Choices
	// kept marks Claude Code and pi as set by the file or the owner:
	// detection fills only the runtimes it leaves unmarked.
	kept    [2]bool
	noneErr bool
	checks  []Check
	checked bool

	steps   []Step
	phase   int
	done    int // install steps finished
	failAt  int
	failErr error
	showLog bool
	cancel  bool

	rel      Release
	relErr   error
	rolledTo string
	stopped  error // why setup ended without installing, for Run
}

type (
	detectMsg Detection
	checksMsg []Check
	stepMsg   struct {
		i   int
		err error
	}
	latestMsg struct {
		r   Release
		err error
	}
	appliedMsg struct{ err error }
	rolledMsg  struct {
		to  string
		err error
	}
	copiedMsg struct {
		name string
		err  error
	}
)

var errCancelled = errors.New("cancelled after an install step; the steps after it did not run")

// NewSetup is the setup wizard on its Welcome step, its choices as the
// settings file holds them.
func NewSetup(b Backend, c Colors) Model {
	m := Model{b: b, st: newStyles(c), mode: ModeSetup, w: 100, h: 24}
	saved := b.Saved()
	for i, f := range []struct {
		from *bool
		to   *bool
	}{
		{saved.ClaudeCode, &m.c.ClaudeCode}, {saved.Pi, &m.c.Pi},
		{saved.Telemetry, &m.c.Telemetry},
	} {
		if f.from != nil {
			*f.to = *f.from
			if i < len(m.kept) {
				m.kept[i] = true
			}
		}
	}
	m.c.Optional = saved.Optional
	if saved.Palette != nil {
		m.c.Palette = *saved.Palette
		if m.c.Palette == "ryoku" { // the desktop palette's older name (T50f)
			m.c.Palette = "desktop"
		}
	}
	return m
}

// NewUpdate is the update flow, checking for the latest release.
func NewUpdate(b Backend, c Colors) Model {
	return Model{b: b, st: newStyles(c), mode: ModeUpdate, w: 100, h: 24}
}

func (m Model) Init() tea.Cmd {
	if m.mode == ModeUpdate {
		return m.latest()
	}
	b := m.b
	return func() tea.Msg { return detectMsg(b.Detect()) }
}

func (m Model) latest() tea.Cmd {
	b := m.b
	return func() tea.Msg { r, err := b.Latest(); return latestMsg{r, err} }
}

func (m Model) runChecks() tea.Cmd {
	b, c := m.b, m.c
	return func() tea.Msg { return checksMsg(b.Checks(c)) }
}

func (m Model) runStep(i int) tea.Cmd {
	b, c := m.b, m.c
	return func() tea.Msg { return stepMsg{i, b.RunStep(c, i)} }
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case detectMsg:
		m.det = Detection(msg)
		if !m.kept[0] {
			m.c.ClaudeCode = m.det.Claude != ""
		}
		if !m.kept[1] {
			m.c.Pi = m.det.Pi != ""
		}
		if !m.kept[0] && !m.c.ClaudeCode && !m.c.Pi {
			m.c.ClaudeCode = true
		}
		return m, nil
	case checksMsg:
		m.checks, m.checked, m.cursor = msg, true, 0
		for i, c := range m.checks {
			if !c.OK {
				m.cursor = i
				break
			}
		}
		return m, nil
	case copiedMsg:
		if msg.err != nil {
			m.note = "✗ could not copy: " + msg.err.Error()
		} else {
			m.note = "✓ copied the " + msg.name + " fix"
		}
		return m, nil
	case stepMsg:
		return m.stepDone(msg)
	case latestMsg:
		m.rel, m.relErr, m.button = msg.r, msg.err, 0
		switch {
		case msg.err != nil:
			m.step = uCheckFailed
		case msg.r.Latest == msg.r.Current:
			m.step = uCurrent
		default:
			m.step = uAvailable
		}
		return m, nil
	case appliedMsg:
		m.button = 0
		if msg.err != nil {
			m.step, m.relErr = uApplyFailed, msg.err
		} else {
			m.step = uUpdated
		}
		return m, nil
	case rolledMsg:
		m.button = 0
		if msg.err != nil {
			m.step, m.relErr = uApplyFailed, msg.err
		} else {
			m.step, m.rolledTo = uRolledBack, msg.to
		}
		return m, nil
	case tea.KeyMsg:
		return m.key(msg.String())
	}
	return m, nil
}

func (m Model) stepDone(msg stepMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.phase, m.failAt, m.failErr, m.button, m.showLog = iFailed, msg.i, msg.err, 0, false
		return m, nil
	}
	m.done = msg.i + 1
	if m.done < len(m.steps) {
		if m.cancel {
			m.stopped = errCancelled
			return m, tea.Quit
		}
		return m, m.runStep(m.done)
	}
	m.step, m.stopped = sDone, nil
	return m, nil
}

func (m Model) key(k string) (tea.Model, tea.Cmd) {
	running := m.mode == ModeSetup && m.step == sInstall && m.phase == iRunning
	if k == "ctrl+c" || k == "q" {
		if running {
			m.cancel = true
			return m, nil
		}
		if m.mode == ModeSetup && m.step == sInstall && m.phase == iFailed {
			m.stopped = m.failErr
		}
		return m, tea.Quit
	}
	switch k {
	case "up", "k":
		k = "up"
	case "down", "j":
		k = "down"
	case "left", "h":
		k = "left"
	case "right", "l":
		k = "right"
	}
	if m.mode == ModeUpdate {
		return m.updateKey(k)
	}
	switch m.step {
	case sWelcome:
		if k == "enter" {
			m.step, m.cursor = sRuntimes, 0
		}
	case sRuntimes:
		m.runtimesKey(k)
	case sIntegrations:
		switch k {
		case "up", "down":
			m.cursor = move(m.cursor, k, rowCount+len(offered()))
		case " ":
			m.toggleIntegration()
		case "enter":
			m.step, m.checked, m.note = sChecks, false, ""
			return m, m.runChecks()
		case "esc":
			m.step, m.cursor, m.note = sRuntimes, 0, ""
		}
	case sChecks:
		return m.checksKey(k)
	case sInstall:
		return m.installKey(k)
	case sDone:
		if k == "enter" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func move(cur int, k string, n int) int {
	if k == "up" && cur > 0 {
		return cur - 1
	}
	if k == "down" && cur < n-1 {
		return cur + 1
	}
	return cur
}

func (m *Model) runtimesKey(k string) {
	switch k {
	case "up", "down":
		m.cursor = move(m.cursor, k, 2)
	case " ":
		if m.cursor == 0 {
			m.c.ClaudeCode = !m.c.ClaudeCode
		} else {
			m.c.Pi = !m.c.Pi
		}
		m.kept[m.cursor] = true
		if m.c.ClaudeCode || m.c.Pi {
			m.noneErr = false
		}
	case "enter":
		if !m.c.ClaudeCode && !m.c.Pi {
			m.noneErr = true
			return
		}
		m.step, m.cursor = sIntegrations, rowTelemetry
	case "esc":
		m.step = sWelcome
	}
}

func (m *Model) toggleIntegration() {
	switch m.cursor {
	case rowEngram, rowCodeGraph:
		core, _ := manifests.Get(manifests.Core[m.cursor])
		m.note = core.Setup.LockedNote
	case rowTelemetry:
		m.c.Telemetry = !m.c.Telemetry
	case rowPalette:
		if m.c.Palette == "desktop" {
			m.c.Palette = "rose-ink"
		} else {
			m.c.Palette = "desktop"
		}
	default:
		if o := offered(); m.cursor >= rowCount && m.cursor < rowCount+len(o) {
			setting := o[m.cursor-rowCount].Setting
			m.c.set(setting, !m.c.Optional.On(setting))
		}
	}
}

// locked is whether a missing required check keeps Install closed.
func (m Model) locked() bool {
	if !m.checked {
		return true
	}
	for _, c := range m.checks {
		if !c.OK {
			return true
		}
	}
	return false
}

func (m Model) checksKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "up", "down":
		m.cursor = move(m.cursor, k, len(m.checks))
	case "c":
		if m.cursor < len(m.checks) && m.checks[m.cursor].Fix != "" {
			b, c := m.b, m.checks[m.cursor]
			return m, func() tea.Msg { return copiedMsg{c.Name, b.Copy(c.Fix)} }
		}
	case "r":
		m.checked, m.note = false, ""
		return m, m.runChecks()
	case "enter":
		if !m.locked() {
			m.step, m.phase, m.steps, m.note = sInstall, iConfirm, m.b.Steps(m.c), ""
		}
	case "esc":
		m.step, m.cursor, m.note = sIntegrations, rowTelemetry, ""
	}
	return m, nil
}

func (m Model) start() (tea.Model, tea.Cmd) {
	m.phase, m.done, m.failErr, m.cancel = iRunning, 0, nil, false
	if len(m.steps) == 0 {
		m.step = sDone
		return m, nil
	}
	return m, m.runStep(0)
}

func (m Model) installKey(k string) (tea.Model, tea.Cmd) {
	switch m.phase {
	case iConfirm:
		switch k {
		case "enter":
			return m.start()
		case "esc":
			m.step = sChecks
		}
	case iFailed:
		switch k {
		case "left", "right":
			m.button = move(m.button, map[string]string{"left": "up", "right": "down"}[k], 3)
		case "enter":
			switch m.button {
			case 0:
				return m.start()
			case 1:
				m.showLog = !m.showLog
			case 2:
				m.stopped = m.failErr
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

// buttons are the update screen's buttons, by screen.
func (m Model) buttons() []string {
	switch m.step {
	case uCheckFailed, uApplyFailed:
		if errors.Is(m.relErr, ErrUpdateNotAvailable) {
			return []string{"Quit"}
		}
		return []string{"Retry", "Quit"}
	case uAvailable:
		return []string{"Update to " + m.rel.Latest, "Later"}
	case uUpdated:
		return []string{"Done", "Roll back to " + m.rel.Current}
	case uCurrent:
		if m.rel.Previous != "" {
			return []string{"Change setup", "Roll back to " + m.rel.Previous, "Quit"}
		}
		return []string{"Change setup", "Quit"}
	case uRolledBack:
		return []string{"Done"}
	}
	return nil
}

func (m Model) updateKey(k string) (tea.Model, tea.Cmd) {
	bs := m.buttons()
	switch k {
	case "left", "right":
		m.button = move(m.button, map[string]string{"left": "up", "right": "down"}[k], len(bs))
		return m, nil
	case "esc":
		if m.step == uAvailable {
			return m, tea.Quit
		}
		return m, nil
	case "enter":
	default:
		return m, nil
	}
	if m.button >= len(bs) {
		return m, nil
	}
	b, r := m.b, m.rel
	switch label := bs[m.button]; {
	case label == "Retry" && m.step == uCheckFailed:
		m.step = uChecking
		return m, m.latest()
	case label == "Retry":
		m.step = uApplying
		return m, func() tea.Msg { return appliedMsg{b.Update(r)} }
	case label == "Update to "+r.Latest:
		m.step = uApplying
		return m, func() tea.Msg { return appliedMsg{b.Update(r)} }
	case label == "Change setup":
		s := NewSetup(m.b, nil)
		s.st, s.w, s.h = m.st, m.w, m.h
		return s, s.Init()
	case m.step == uUpdated && m.button == 1:
		m.step = uRollingBack
		return m, func() tea.Msg { return rolledMsg{r.Current, b.Rollback(r.Current)} }
	case m.step == uCurrent && label == "Roll back to "+r.Previous:
		m.step = uRollingBack
		return m, func() tea.Msg { return rolledMsg{r.Previous, b.Rollback(r.Previous)} }
	}
	return m, tea.Quit
}
