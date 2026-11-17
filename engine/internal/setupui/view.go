package setupui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	manifests "github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
)

// Colors are rose ink's semantic roles as palette.Semantic gives them
// (frame, row, band, page, box, ink, soft, faint, line, accent, accentFill,
// accentInk, accentSoft, ok, warn, bad). The CLI passes them in: the palette
// is an opt-in integration only the CLI imports (S37), and this package
// keeps no color of its own. A missing role paints nothing.
type Colors map[string]any

// styles are the artboard's color roles.
type styles struct {
	ink, soft, faint, line, rose, roseFill, roseInk, roseSoft, ok, warn, bad string
	frame, row, band, page, box                                              string
}

func newStyles(c Colors) styles {
	role := func(k string) string { v, _ := c[k].(string); return v }
	return styles{
		ink: role("ink"), soft: role("soft"), faint: role("faint"), line: role("line"),
		rose: role("accent"), roseFill: role("accentFill"), roseInk: role("accentInk"), roseSoft: role("accentSoft"),
		ok: role("ok"), warn: role("warn"), bad: role("bad"),
		frame: role("frame"), row: role("row"), band: role("band"), page: role("page"), box: role("box"),
	}
}

// seg is a run of text in one look; bg empty takes the line's background.
type seg struct {
	s, fg, bg string
	bold      bool
}

// line is one content row; focus paints it as the selected row.
type line struct {
	focus bool
	segs  []seg
}

func (st styles) paint(s seg, bg string) string {
	if s.bg != "" {
		bg = s.bg
	}
	style := lipgloss.NewStyle().Bold(s.bold)
	if bg != "" {
		style = style.Background(lipgloss.Color(bg))
	}
	if s.fg != "" {
		style = style.Foreground(lipgloss.Color(s.fg))
	}
	return style.Render(s.s)
}

// cells is segs on bg, cut with … or padded to exactly width cells.
func (st styles) cells(segs []seg, width int, bg string) string {
	var b strings.Builder
	left := width
	for _, s := range segs {
		if left <= 0 {
			break
		}
		if w := ansi.StringWidth(s.s); w > left {
			s.s = ansi.Truncate(s.s, left, "…")
		}
		left -= ansi.StringWidth(s.s)
		b.WriteString(st.paint(s, bg))
	}
	if left > 0 {
		b.WriteString(st.paint(seg{s: strings.Repeat(" ", left)}, bg))
	}
	return b.String()
}

func pad(s string, n int) string {
	if w := ansi.StringWidth(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// The rail's item states.
const (
	pending = iota
	active
	done
	failed
)

type railItem struct {
	name  string
	state int
}

func (m Model) narrow() bool { return m.w < 96 }

func (m Model) rail() []railItem {
	names, cur, curState := setupRail, m.step, active
	if m.mode == ModeUpdate {
		names = updateRail
		switch m.step {
		case uChecking:
			cur = 0
		case uCheckFailed:
			cur, curState = 0, failed
		case uAvailable:
			cur = 1
		case uApplying, uRollingBack:
			cur = 2
		case uApplyFailed:
			cur, curState = 2, failed
		default:
			cur = len(names)
		}
	} else if m.step == sInstall && m.phase == iFailed {
		curState = failed
	} else if m.step == sDone {
		cur = len(names)
	}
	items := make([]railItem, len(names))
	for i, n := range names {
		items[i] = railItem{n, pending}
		switch {
		case i < cur:
			items[i].state = done
		case i == cur:
			items[i].state = curState
		}
	}
	return items
}

func (m Model) railCell(r int, items []railItem, width int) string {
	st := m.st
	i := r - 1
	if i < 0 || i >= len(items) {
		return st.cells(nil, width, st.frame)
	}
	it := items[i]
	bg, marker := st.frame, " "
	glyph, gfg, nfg, bold := "○", st.faint, st.faint, false
	switch it.state {
	case active:
		bg, marker, glyph, gfg, nfg, bold = st.row, "▎", "◐", st.rose, st.ink, true
	case failed:
		bg, marker, glyph, gfg, nfg, bold = st.row, "▎", "✗", st.bad, st.ink, true
	case done:
		glyph, gfg, nfg = "✓", st.ok, st.soft
	}
	if m.narrow() {
		return st.cells([]seg{{s: marker, fg: st.rose}, {s: glyph + " ", fg: gfg, bold: bold}, {s: strconv.Itoa(i + 1), fg: nfg, bold: bold}}, width, bg)
	}
	return st.cells([]seg{{s: marker, fg: st.rose}, {s: "  " + glyph + " ", fg: gfg, bold: bold}, {s: it.name, fg: nfg, bold: bold}}, width, bg)
}

func (m Model) contentCell(l line, width int) string {
	st := m.st
	bg := st.page
	lead := []seg{{s: "  "}}
	if l.focus {
		bg = st.row
		lead = []seg{{s: "▎", fg: st.rose}, {s: " "}}
	}
	if len(l.segs) == 0 && !l.focus {
		lead = nil
	}
	return st.cells(append(lead, l.segs...), width, bg)
}

type hint struct{ key, desc string }

func (m Model) View() string {
	st := m.st
	w, h := max(m.w, 30), max(m.h, 6)
	railW := 22
	if m.narrow() {
		railW = 5
	}
	cw := w - railW - 1
	items := m.rail()
	body := m.content(cw)

	label := "  setup"
	if m.mode == ModeUpdate {
		label = "  update"
	}
	right := version.Version + " "
	head := []seg{{s: " 力 ", fg: st.roseInk, bg: st.roseFill, bold: true}, {s: " jhon", fg: st.ink, bold: true}, {s: label, fg: st.soft}}
	used := 0
	for _, s := range head {
		used += ansi.StringWidth(s.s)
	}
	out := []string{st.cells(head, max(w-ansi.StringWidth(right), used), st.band) + st.cells([]seg{{s: right, fg: st.faint}}, min(ansi.StringWidth(right), w-used), st.band)}
	for r := 0; r < h-2; r++ {
		l := line{}
		if r > 0 && r-1 < len(body) {
			l = body[r-1]
		}
		out = append(out, m.railCell(r, items, railW)+st.paint(seg{s: "│", fg: st.line}, st.frame)+m.contentCell(l, cw))
	}
	hints, note := m.hints()
	var foot []seg
	for i, k := range hints {
		sep := "  "
		if i == 0 {
			sep = " "
		}
		foot = append(foot, seg{s: sep}, seg{s: k.key, fg: st.roseSoft, bold: true}, seg{s: " " + k.desc, fg: st.faint})
	}
	if note == "" {
		out = append(out, st.cells(foot, w, st.band))
	} else {
		note += " "
		nw := min(ansi.StringWidth(note), w)
		out = append(out, st.cells(foot, w-nw, st.band)+st.cells([]seg{{s: note, fg: st.faint}}, nw, st.band))
	}
	return strings.Join(out, "\n")
}

// hints are the footer's keys, and the note at its right end.
func (m Model) hints() ([]hint, string) {
	next := "continue"
	if m.narrow() {
		next = "next"
	}
	choose := []hint{{"←→", "choose"}, {"enter", "confirm"}, {"q", "quit"}}
	if m.mode == ModeUpdate {
		switch m.step {
		case uChecking, uApplying, uRollingBack:
			return nil, ""
		case uAvailable:
			return []hint{{"←→", "choose"}, {"enter", "confirm"}, {"esc", "back"}, {"q", "quit"}}, ""
		case uRolledBack:
			return []hint{{"enter", "close"}, {"q", "quit"}}, ""
		}
		return choose, ""
	}
	switch m.step {
	case sWelcome:
		return []hint{{"enter", "start setup"}, {"q", "quit"}}, ""
	case sRuntimes, sIntegrations:
		return []hint{{"↑↓", "move"}, {"space", "toggle"}, {"enter", next}, {"esc", "back"}, {"q", "quit"}}, ""
	case sChecks:
		hs := []hint{{"↑↓", "move"}}
		for _, c := range m.checks {
			if c.Fix != "" {
				hs = append(hs, hint{"c", "copy fix"})
				break
			}
		}
		if !m.locked() {
			hs = append(hs, hint{"enter", next})
		}
		hs = append(hs, hint{"r", "check again"}, hint{"esc", "back"}, hint{"q", "quit"})
		if m.checked && m.locked() {
			return hs, "Install locked"
		}
		return hs, ""
	case sInstall:
		switch m.phase {
		case iConfirm:
			return []hint{{"enter", "install"}, {"esc", "back"}, {"q", "quit"}}, ""
		case iRunning:
			return []hint{{"q", "cancel safely"}}, ""
		}
		return choose, ""
	}
	return []hint{{"enter", "close"}, {"q", "quit"}}, ""
}

// Text helpers, each one role.
func (m Model) title(s string) seg { return seg{s: s, fg: m.st.ink, bold: true} }
func (m Model) text(s string) seg  { return seg{s: s, fg: m.st.soft} }
func (m Model) dim(s string) seg   { return seg{s: s, fg: m.st.faint} }
func (m Model) head(s string) seg  { return seg{s: s, fg: m.st.roseSoft, bold: true} }
func (m Model) good(s string) seg  { return seg{s: s, fg: m.st.ok, bold: true} }
func (m Model) bad(s string) seg   { return seg{s: s, fg: m.st.bad, bold: true} }
func l(segs ...seg) line           { return line{segs: segs} }

// heading is a step's title and its wide note; narrow, the rail has no
// names, so a title without a note says the step.
func (m Model) heading(t string, wide ...seg) line {
	if m.narrow() && m.mode == ModeSetup && len(wide) == 0 {
		return l(m.title(t), m.dim(fmt.Sprintf("   step %d of %d", m.step+1, len(setupRail))))
	}
	return l(append([]seg{m.title(t)}, wide...)...)
}

func (m Model) buttonRow(labels []string) line {
	var segs []seg
	for i, b := range labels {
		if i > 0 {
			segs = append(segs, seg{s: "  "})
		}
		if i == m.button {
			segs = append(segs, seg{s: " " + b + " ", fg: m.st.page, bg: m.st.rose, bold: true})
		} else {
			segs = append(segs, seg{s: " " + b + " ", fg: m.st.soft, bg: m.st.box})
		}
	}
	return l(segs...)
}

func (m Model) box(on bool) seg {
	if on {
		return seg{s: "[●] ", fg: m.st.rose}
	}
	return seg{s: "[ ] ", fg: m.st.faint}
}

func (m Model) content(cw int) []line {
	if m.mode == ModeUpdate {
		return m.updateContent()
	}
	switch m.step {
	case sWelcome:
		return m.welcome()
	case sRuntimes:
		return m.runtimes(cw)
	case sIntegrations:
		return m.integrations()
	case sChecks:
		return m.checkLines(cw)
	case sInstall:
		return m.install()
	}
	return m.doneLines()
}

func (m Model) welcome() []line {
	item := func(n, s string) line { return l(m.dim(n+"  "), m.text(s)) }
	return []line{
		m.heading("Welcome to Jhon"),
		l(m.text("An LDD agent framework: an engine, guards and prose,")),
		l(m.text("worked from Claude Code or pi.")),
		{},
		l(m.head("This setup will")),
		item("1", "pick the runtimes you work from"),
		item("2", "choose integrations (Engram and CodeGraph come with the core)"),
		item("3", "check git, claude, engram and codegraph, and say how to fix each"),
		item("4", "install the core and write "+m.b.Paths().Settings),
		{},
		l(m.text("Nothing is written until you confirm on the Install step.")),
		{},
		l(seg{s: " Start setup ", fg: m.st.page, bg: m.st.rose, bold: true}, seg{s: "   "}, seg{s: "enter", fg: m.st.roseSoft, bold: true}),
	}
}

func (m Model) found(cmd, path, missing string) seg {
	if path == "" {
		return m.dim("○ " + cmd + " " + missing)
	}
	return seg{s: "✓ " + cmd + " found  " + path, fg: m.st.soft}
}

func (m Model) runtimes(cw int) []line {
	claude := m.found("claude", m.det.Claude, "not found, the checks say how to install it")
	pi := m.found("pi", m.det.Pi, "not found, setup installs it (needs node)")
	if m.det.Claude != "" {
		claude = seg{s: "✓", fg: m.st.ok}
		claude.s = "✓ claude found  " + m.det.Claude
	}
	ls := []line{
		m.heading("Where will you work from?", m.dim("   at least one")),
		{},
		{focus: m.cursor == 0, segs: []seg{m.box(m.c.ClaudeCode), {s: pad("Claude Code", 16), fg: m.st.ink}, claude}},
		l(m.dim("    Anthropic's agent CLI; Jhon renders its hooks, skills, commands")),
		{},
		{focus: m.cursor == 1, segs: []seg{m.box(m.c.Pi), {s: pad("pi", 16), fg: m.st.ink}, pi}},
		l(m.dim("    a terminal coding agent; Jhon ships its shell, rail and panel")),
	}
	if m.c.Pi {
		ls = append(ls, l(m.dim("    provider  "), m.text("claude-bridge"), m.dim("   default, chosen per profile")))
	} else {
		ls = append(ls, line{})
	}
	ls = append(ls, line{})
	if m.noneErr {
		return append(ls, l(m.bad("✗ Pick at least one runtime to continue.")))
	}
	return append(ls,
		l(seg{s: strings.Repeat("─", max(cw-4, 0)), fg: m.st.line}),
		l(m.dim("Change later with "), m.text("jhon settings set runtimes.pi "+strconv.FormatBool(!m.c.Pi))))
}

// integration is one row of the Integrations step: wide and narrow labels.
type integration struct {
	name, short, desc, narrowDesc, long string
}

// The required integrations' rows are their manifests' display name and
// setup text (integrations/<name>/manifest.yaml); there must be one for each
// locked row (model.go).
var integrations = [rowCount]integration{
	core(rowEngram),
	core(rowCodeGraph),
	{"Telemetry", "Telemetry", "record usage events on this machine", "usage events, local", "one JSON line per event under ~/.local/share/jhon/telemetry"},
	{"Palette", "Palette", "Jhon's colours, or your wallpaper's", "Rose Ink or Desktop", "Rose Ink is Jhon's own; Desktop follows a desktop palette file (JHON_DESKTOP_COLORS)"},
}

// core is the row of the i-th required integration.
func core(i int) integration {
	m := manifests.Required()[i]
	return integration{m.Display, m.Display, m.Setup.Summary, m.Setup.SummaryNarrow, m.Setup.Detail}
}

// stateWidth is the integrations' state column: its widest value, "locked on"
// or the palette's "Rose Ink".
var stateWidth = func() int {
	w := 0
	for _, v := range []string{"locked on", "on", "off", "Rose Ink", "Desktop"} {
		w = max(w, len([]rune(v)))
	}
	return w
}()

func (m Model) integrations() []line {
	on := [rowCount]bool{true, true, m.c.Telemetry, m.c.Palette == "desktop"}
	narrow := m.narrow()
	row := func(i int) line {
		it := integrations[i]
		name, desc, nw, dw := it.name, it.desc, 24, 38
		if narrow {
			name, desc, nw, dw = it.short, it.narrowDesc, 12, 26
		}
		state := m.dim("off")
		switch {
		case i < rowTelemetry:
			state = m.dim("locked on")
		case i == rowPalette && on[i]:
			state = seg{s: "Desktop", fg: m.st.ok}
		case i == rowPalette:
			state = m.dim("Rose Ink")
		case on[i]:
			state = seg{s: "on", fg: m.st.ok}
		}
		// The description keeps two cells before the state, which is
		// stateWidth wide: its widest value fits (L99).
		if d := []rune(desc); len(d) > dw-2 {
			desc = string(d[:dw-3]) + "…"
		}
		state.s = pad(state.s, stateWidth)
		if i == rowPalette {
			// The palette is a choice of two, not an on/off box (T15.9): the
			// chosen one marked ●, the other ○; space steps it.
			rose, desktop := "● Rose Ink", "○ Desktop"
			if on[i] {
				rose, desktop = "○ Rose Ink", "● Desktop"
			}
			choice := rose + "  " + desktop
			return line{focus: m.cursor == i, segs: []seg{{s: "    "}, {s: pad(name, nw), fg: m.st.ink}, m.text(pad(choice, dw)), {s: pad("", stateWidth)}}}
		}
		return line{focus: m.cursor == i, segs: []seg{m.box(on[i]), {s: pad(name, nw), fg: m.st.ink}, m.text(pad(desc, dw)), state}}
	}
	long := func(i int) line { return l(m.dim("    " + integrations[i].long)) }
	ls := []line{m.heading("Integrations"), l(m.head("Required for now"))}
	if narrow {
		ls = append(ls, row(rowEngram), row(rowCodeGraph), l(m.dim("    coupled to the core for now")))
	} else {
		ls = append(ls, row(rowEngram), long(rowEngram), row(rowCodeGraph), long(rowCodeGraph))
	}
	ls = append(ls, line{}, l(m.head("Optional, off by default")))
	for i := rowTelemetry; i < rowCount; i++ {
		ls = append(ls, row(i))
		if !narrow && m.cursor == i {
			ls = append(ls, long(i))
		}
	}
	switch {
	case m.note != "":
		ls = append(ls, line{}, l(m.text(m.note)))
	case !narrow:
		ls = append(ls, line{}, l(m.dim("Each one toggles later in "), m.text("jhon settings")))
	}
	return ls
}

func (m Model) checkLines(cw int) []line {
	if !m.checked {
		return []line{m.heading("Dependencies"), {}, l(m.dim("Checking…"))}
	}
	missing := 0
	for _, c := range m.checks {
		if !c.OK {
			missing++
		}
	}
	count := seg{s: "   all found", fg: m.st.ok}
	if missing > 0 {
		count = seg{s: fmt.Sprintf("   %d missing", missing), fg: m.st.warn}
	}
	ls := []line{m.heading("Dependencies", count), {}}
	for i, c := range m.checks {
		mark := seg{s: "✓", fg: m.st.ok}
		if !c.OK {
			mark = m.bad("✗")
		}
		ls = append(ls, line{focus: m.cursor == i, segs: []seg{mark, {s: " " + pad(c.Name, 11), fg: m.st.ink}, m.text(c.Detail)}})
		if !c.OK && c.Fix != "" {
			// The row keeps the rule's right margin: lead, "  $ ", text, 2 cells.
			for j, part := range wrapFix(c.Fix, cw-8, cw-6) {
				prefix := m.dim("  $ ")
				if j > 0 {
					prefix = m.dim("  ")
				}
				ls = append(ls, l(prefix, seg{s: strings.TrimRight(part, " "), fg: m.st.ink}))
			}
		}
		if !c.OK && c.Advice != "" {
			ls = append(ls, l(m.dim("  "), m.text(c.Advice)))
		}
	}
	ls = append(ls, line{}, l(seg{s: strings.Repeat("─", max(cw-4, 0)), fg: m.st.line}))
	if missing > 0 {
		ls = append(ls,
			l(m.text("Run the fix in another terminal, then "), seg{s: "r", fg: m.st.roseSoft, bold: true}, m.text(" to check again.")),
			l(m.text("Install stays locked while a required check is missing.")))
	} else {
		ls = append(ls, l(m.text("Every dependency is here; "), seg{s: "enter", fg: m.st.roseSoft, bold: true}, m.text(" goes to the Install step.")))
	}
	if m.note != "" {
		ls = append(ls, line{}, l(m.text(m.note)))
	}
	return ls
}

func (m Model) stepRows() []line {
	nw := 0
	for _, s := range m.steps {
		nw = max(nw, ansi.StringWidth(s.Name)+3)
	}
	var ls []line
	for i, s := range m.steps {
		glyph, detail := seg{s: "○ ", fg: m.st.faint}, m.text(s.Detail)
		name := seg{s: pad(s.Name, nw), fg: m.st.soft}
		switch {
		case m.phase == iFailed && i == m.failAt:
			reason, _, _ := strings.Cut(m.failErr.Error(), "\n")
			glyph, name.fg, detail = m.bad("✗ "), m.st.ink, seg{s: reason, fg: m.st.bad}
		case m.phase == iFailed && i > m.failAt:
			detail = seg{}
			name.s = s.Name
		case m.phase == iRunning && i == m.done:
			glyph, name.fg = seg{s: "◐ ", fg: m.st.rose, bold: true}, m.st.ink
		case m.phase != iConfirm && i < m.done:
			glyph, name.fg = seg{s: "✓ ", fg: m.st.ok}, m.st.ink
		}
		ls = append(ls, l(glyph, name, detail))
	}
	return ls
}

func (m Model) install() []line {
	n := len(m.steps)
	switch m.phase {
	case iConfirm:
		ls := append([]line{m.heading("Ready to install", m.dim(fmt.Sprintf("   %d steps", n))), {}}, m.stepRows()...)
		return append(ls, line{},
			l(seg{s: " Install ", fg: m.st.page, bg: m.st.rose, bold: true}, seg{s: "   "}, seg{s: "enter", fg: m.st.roseSoft, bold: true}),
			l(m.text("Nothing is written until you press enter.")))
	case iRunning:
		ls := append([]line{m.heading("Installing", m.dim(fmt.Sprintf("   %d of %d", m.done+1, n))), {}}, m.stepRows()...)
		const bar = 40
		fill := m.done * bar / max(n, 1)
		ls = append(ls, line{}, l(seg{s: strings.Repeat("█", fill), fg: m.st.rose}, seg{s: strings.Repeat("░", bar-fill), fg: m.st.line},
			m.text(fmt.Sprintf("  %d%%", m.done*100/max(n, 1)))))
		if m.cancel {
			ls = append(ls, line{}, l(seg{s: "Cancelling after this step; nothing after it runs.", fg: m.st.warn}))
		}
		return ls
	}
	ls := append([]line{m.heading("Install stopped", m.dim(fmt.Sprintf("   at step %d of %d", m.failAt+1, n))), {}}, m.stepRows()...)
	ls = append(ls, line{})
	msg := strings.Split(m.failErr.Error(), "\n")
	if help := msg[1:]; len(help) > 0 {
		for _, h := range help {
			ls = append(ls, l(m.text(h)))
		}
	} else {
		for i := m.failAt + 1; i < n; i++ {
			if m.steps[i].WritesSettings {
				ls = append(ls, l(m.text("Your settings were not written.")))
				break
			}
		}
		ls = append(ls, l(m.text("Retry runs every step again; it is safe to repeat.")))
	}
	ls = append(ls, line{}, m.buttonRow([]string{"Retry", "Show log", "Quit"}))
	if m.showLog {
		ls = append(ls, line{})
		for _, s := range msg {
			ls = append(ls, l(m.dim(s)))
		}
	}
	return ls
}

func (m Model) chosen() (runtimes, integrations string) {
	var r []string
	if m.c.ClaudeCode {
		r = append(r, "Claude Code")
	}
	if m.c.Pi {
		r = append(r, "pi (claude-bridge)")
	}
	var in []string
	for _, x := range manifests.Required() {
		in = append(in, x.Display)
	}
	for _, x := range []struct {
		on   bool
		name string
	}{{m.c.Telemetry, "Telemetry"}, {m.c.Palette == "desktop", "Desktop palette"}} {
		if x.on {
			in = append(in, x.name)
		}
	}
	return strings.Join(r, ", "), strings.Join(in, ", ")
}

func (m Model) doneLines() []line {
	rt, in := m.chosen()
	p := m.b.Paths()
	kv := func(k, v string, more ...seg) line {
		return l(append([]seg{m.dim(pad(k, 14)), m.text(v)}, more...)...)
	}
	next := func(cmd, what string) line {
		return l(seg{s: pad(cmd, 15), fg: m.st.ink, bg: m.st.box}, m.text("  "+what))
	}
	ls := []line{
		l(m.good("✓ Jhon " + version.Version + " is installed")),
		{},
		kv("runtimes", rt),
		kv("integrations", in),
		kv("binary", p.Binary),
		kv("settings", p.Settings, m.dim("   edit with jhon settings")),
		{},
		l(m.head("Next")),
	}
	if m.c.ClaudeCode {
		ls = append(ls, next("jhon start", "open Claude Code in this project"))
	}
	if m.c.Pi {
		ls = append(ls, next("jhon start pi", "open pi in this project"))
	}
	return append(ls, next("jhon setup", "come back here to update or change choices"))
}

func (m Model) updateContent() []line {
	r := m.rel
	still := l(m.text("Jhon " + version.Version + " is still installed and working. Nothing changed."))
	switch m.step {
	case uChecking:
		return []line{l(m.title("Checking for a newer release"))}
	case uCheckFailed:
		if errors.Is(m.relErr, ErrUpdateNotAvailable) {
			return []line{l(m.bad("✗ Updates are not available yet")), l(m.dim("This build of Jhon cannot look for releases.")), {}, still, {}, m.buttonRow(m.buttons())}
		}
		return []line{l(m.bad("✗ Could not reach GitHub")), l(m.dim(m.relErr.Error())), {}, still,
			l(m.text("Check the connection or a proxy, then retry.")), {}, m.buttonRow(m.buttons())}
	case uAvailable:
		signed := []seg{m.dim("signed   "), {s: "✓ verified", fg: m.st.ok}, m.dim("  " + r.Signature)}
		if !r.Verified {
			signed = []seg{m.dim("signed   "), m.bad("✗ not verified")}
		}
		ls := []line{l(m.title("Update available")), {},
			l(m.dim("current  "), m.text(r.Current)),
			l(m.dim("latest   "), seg{s: r.Latest, fg: m.st.ink, bold: true}, m.dim("   released "+r.Date)),
			l(signed...), {}}
		if len(r.Notes) > 0 {
			ls = append(ls, l(m.head("What changed")))
			for _, n := range r.Notes {
				ls = append(ls, l(m.dim("· "), m.text(n)))
			}
			ls = append(ls, line{})
		}
		return append(ls, m.buttonRow(m.buttons()),
			l(m.text("Your settings and profiles are kept. "+r.Current+" stays installed for a roll-back.")))
	case uApplying:
		return []line{l(m.title("Updating to " + r.Latest))}
	case uRollingBack:
		return []line{l(m.title("Rolling back"))}
	case uApplyFailed:
		return []line{l(m.bad("✗ The update stopped")), l(m.dim(m.relErr.Error())), {}, still, {}, m.buttonRow(m.buttons())}
	case uUpdated:
		return []line{l(m.good("✓ Updated to " + r.Latest)), {},
			l(m.dim("previous  "), m.text(r.Current), m.dim("   kept, ready to roll back")),
			l(m.dim("settings  "), m.text("unchanged")), {},
			m.buttonRow(m.buttons()),
			l(m.text("Roll back reinstalls " + r.Current + " as it was; restart open sessions after either."))}
	case uRolledBack:
		return []line{l(m.good("✓ Rolled back to " + m.rolledTo)), {}, l(m.text("Restart open sessions to use it.")), {}, m.buttonRow(m.buttons())}
	}
	if r.NoStable {
		return []line{l(m.good("✓ No stable release is published yet")), l(m.text("Jhon " + r.Current + " is installed; nothing changed.")),
			l(m.dim("checked "), m.text(r.Source), m.dim(" just now")), {}, m.buttonRow(m.buttons())}
	}
	return []line{l(m.good("✓ Jhon " + r.Current + " is up to date")), l(m.dim("checked "), m.text(r.Source), m.dim(" just now")), {}, m.buttonRow(m.buttons())}
}

// wrapFix breaks a command the user must type into lines of at most first
// cells, then rest: at a space, or inside one long token after a /, and
// never with an ellipsis. Joined, the lines are the command.
func wrapFix(cmd string, first, rest int) []string {
	var out []string
	cur, width := "", max(first, 1)
	flush := func() {
		out = append(out, cur)
		cur, width = "", max(rest, 1)
	}
	for i, w := range strings.Split(cmd, " ") {
		if i > 0 {
			if ansi.StringWidth(cur)+1+ansi.StringWidth(w) <= width {
				cur += " " + w
				continue
			}
			cur += " "
			flush()
		}
		for ansi.StringWidth(cur)+ansi.StringWidth(w) > width {
			room := width - ansi.StringWidth(cur)
			cut := strings.LastIndex(ansi.Truncate(w, room, ""), "/") + 1
			if cut == 0 && cur != "" {
				flush()
				continue
			}
			if cut == 0 {
				cut = len(ansi.Truncate(w, room, ""))
			}
			cur += w[:cut]
			w = w[cut:]
			flush()
		}
		cur += w
	}
	return append(out, cur)
}
