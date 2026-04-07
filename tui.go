package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Styles ---

var (
	sTitle = lipgloss.NewStyle().Bold(true)
	sDim   = lipgloss.NewStyle().Faint(true)
	sWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	sErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	sOk    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	sCyan  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	sSep   = lipgloss.NewStyle().Faint(true)
)

// --- Messages ---

type statusMsg struct {
	ccStatus   string
	buildOK    *bool
	buildErrs  int
	buildTrend string
	files      []string
	newFiles   []string
	elapsed    string
	project    string
}
type nudgeMsg struct{ finding BrainFinding }
type resolvedMsg struct{ finding BrainFinding }
type trajectoryMsg struct{ message string }
type brainStatusMsg struct {
	thinking bool
	lastTime time.Time
}
type logLineMsg struct{ line string }
type tickMsg time.Time

var copySelectedText = CopySelectedToClipboard

// --- Model ---

type model struct {
	// Log
	lines        []string
	scrollOffset int

	// Layout
	width  int
	height int

	// Header state
	project    string
	elapsed    string
	ccStatus   string
	brainState string
	buildState string
	findings   int // active count
	resolved   int

	// Footer state
	fileLine    string // current files summary
	toastMsg    string // transient message (e.g. "copied!")
	toastExpiry time.Time

	// Selection
	sel *Selection

	quitting bool
}

func initialModel(project string) model {
	return model{
		project:    project,
		ccStatus:   "starting",
		brainState: sDim.Render("starting"),
		sel:        NewSelection(),
	}
}

func (m model) Init() tea.Cmd { return tickEvery() }

// ProgramOptions returns the tea.ProgramOption list for the trupal TUI.
func ProgramOptions() []tea.ProgramOption {
	return []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	}
}

func tickEvery() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// --- Update ---

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			m.scroll(1)
		case "down", "j":
			m.scroll(-1)
		case "pgup":
			m.scroll(10)
		case "pgdown":
			m.scroll(-10)
		case "g", "home":
			m.scrollOffset = m.maxScroll()
		case "G", "end":
			m.scrollOffset = 0
		}

	case tea.MouseMsg:
		switch {
		case msg.Button == tea.MouseButtonWheelUp && m.logRect().Contains(msg.X, msg.Y):
			m.scroll(3)
		case msg.Button == tea.MouseButtonWheelDown && m.logRect().Contains(msg.X, msg.Y):
			m.scroll(-3)
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
			point, ok := m.selectionPointAt(msg.X, msg.Y, false)
			if !ok {
				m.sel.Clear()
				return m, nil
			}
			m.sel.PrepareDrag(point.Line, point.Col, m.logRect())
		case msg.Action == tea.MouseActionMotion && (msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonNone):
			point, ok := m.selectionPointAt(msg.X, msg.Y, true)
			if !ok {
				return m, nil
			}
			if !m.sel.Anchor.Valid() {
				m.sel.PrepareDrag(point.Line, point.Col, m.logRect())
			}
			m.sel.HandleDrag(point.Line, point.Col)
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionRelease,
			msg.Button == tea.MouseButtonNone && msg.Action == tea.MouseActionRelease:
			if m.sel.Anchor.Valid() {
				if point, ok := m.selectionPointAt(msg.X, msg.Y, true); ok {
					if m.sel.Active || point.Line != m.sel.Anchor.Line || point.Col != m.sel.Anchor.Col {
						m.sel.HandleDrag(point.Line, point.Col)
					}
				}
				if m.sel.FinishDrag() {
					text := m.sel.SelectedText(m.lines, selectionTabWidth)
					if text != "" {
						return m, func() tea.Msg {
							return SelectionCopiedMsg{
								Text: text,
								Time: time.Now(),
								Err:  copySelectedText(text),
							}
						}
					}
				}
			}
		}
		return m, nil

	case tickMsg:
		return m, tickEvery()

	case statusMsg:
		if msg.ccStatus != "" {
			m.ccStatus = msg.ccStatus
		}
		if msg.elapsed != "" {
			m.elapsed = msg.elapsed
		}
		if msg.project != "" {
			m.project = msg.project
		}
		if msg.buildOK != nil {
			if *msg.buildOK {
				m.buildState = sOk.Render("✓") + " build"
			} else {
				label := fmt.Sprintf("%d err", msg.buildErrs)
				if msg.buildTrend != "" {
					label += " (" + msg.buildTrend + ")"
				}
				m.buildState = sErr.Render("✗") + " " + label
			}
		}
		// Update footer files (shown in footer, not logged to chat).
		fl := filesLine(msg.files, msg.newFiles)
		if fl != "" {
			m.fileLine = fl
		}

	case nudgeMsg:
		m.findings++
		icon := sWarn.Render("⚠")
		if msg.finding.Severity == "error" {
			icon = sErr.Render("✗")
		}
		w := m.contentW()
		m.log("")
		for i, line := range wrap(msg.finding.Nudge, w) {
			if i == 0 {
				m.log(icon + " " + line)
			} else {
				m.raw("  " + line)
			}
		}
		if msg.finding.Reasoning != "" {
			for _, line := range wrap(msg.finding.Reasoning, w) {
				m.raw("  " + sDim.Render(line))
			}
		}
		m.log("")

	case resolvedMsg:
		m.findings--
		if m.findings < 0 {
			m.findings = 0
		}
		m.resolved++
		w := m.contentW()
		for i, line := range wrap(msg.finding.Nudge, w) {
			if i == 0 {
				m.log(sOk.Render("✓") + " " + sDim.Render(line))
			} else {
				m.raw("  " + sDim.Render(line))
			}
		}

	case trajectoryMsg:
		m.log(sWarn.Render("▸") + " " + msg.message)

	case brainStatusMsg:
		if msg.thinking {
			m.brainState = sCyan.Render("◌") + " analyzing"
		} else if !msg.lastTime.IsZero() {
			ago := int(time.Since(msg.lastTime).Seconds())
			if ago < 60 {
				m.brainState = sOk.Render("●") + sDim.Render(fmt.Sprintf(" %ds", ago))
			} else {
				m.brainState = sOk.Render("●") + sDim.Render(fmt.Sprintf(" %dm", ago/60))
			}
		}

	case SelectionCopiedMsg:
		if msg.Err != nil {
			m.toastMsg = "copy failed"
		} else {
			m.toastMsg = "copied to tmux buffer"
		}
		m.toastExpiry = time.Now().Add(2 * time.Second)
		return m, nil

	case logLineMsg:
		m.log(msg.line)
	}

	return m, nil
}

// --- Log helpers ---

func (m *model) log(line string) {
	ts := sDim.Render(time.Now().Format("15:04:05"))
	if line == "" {
		m.lines = append(m.lines, "")
	} else {
		m.lines = append(m.lines, ts+"  "+line)
	}
	if m.scrollOffset == 0 { /* auto-scroll: already at bottom */
	}
	m.trim()
}

func (m *model) raw(line string) {
	m.lines = append(m.lines, "          "+line) // align with text after timestamp
	m.trim()
}

func (m *model) trim() {
	if len(m.lines) > 500 {
		m.lines = m.lines[len(m.lines)-500:]
	}
}

func (m *model) scroll(delta int) {
	m.scrollOffset += delta
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	if max := m.maxScroll(); m.scrollOffset > max {
		m.scrollOffset = max
	}
}

func (m model) maxScroll() int {
	lh := m.logH()
	if len(m.lines) <= lh {
		return 0
	}
	return len(m.lines) - lh
}

func (m model) logH() int {
	// header(2) + sep(1) + log + sep(1) + footer(1) = 6 chrome lines (including newlines)
	h := m.height - 6
	if h < 1 {
		return 1
	}
	return h
}

func (m model) contentW() int {
	w := m.width - 14
	if w < 20 {
		return 20
	}
	return w
}

func (m model) logRect() selectionRect {
	return selectionRect{
		X: 0,
		Y: 3,
		W: m.width,
		H: m.logH(),
	}
}

func (m model) visibleLogRange() (start, end int) {
	total := len(m.lines)
	end = total - m.scrollOffset
	if end > total {
		end = total
	}
	start = end - m.logH()
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	return start, end
}

func (m model) selectionPointAt(x, y int, clamp bool) (selectionPoint, bool) {
	rect := m.logRect()
	if rect.W <= 0 || rect.H <= 0 {
		return selectionPoint{}, false
	}
	if !rect.Contains(x, y) {
		if !clamp {
			return selectionPoint{}, false
		}
		x, y = rect.Clamp(x, y)
	}

	start, end := m.visibleLogRange()
	if end <= start {
		return selectionPoint{}, false
	}

	lineIdx := start + (y - rect.Y)
	if lineIdx < start {
		lineIdx = start
	}
	if lineIdx >= end {
		lineIdx = end - 1
	}
	if lineIdx < 0 || lineIdx >= len(m.lines) {
		return selectionPoint{}, false
	}

	relX := x - rect.X
	if relX < 0 {
		relX = 0
	}
	expanded := ExpandTabs(m.lines[lineIdx], selectionTabWidth)
	return selectionPoint{
		Line: lineIdx,
		Col:  VisualColAtRelativeX(expanded, relX),
	}, true
}

// --- View ---

func (m model) View() string {
	if m.quitting || m.width == 0 {
		return ""
	}
	w := m.width

	// ── Header line 1: trupal · project · 5m ──
	h1 := sTitle.Render(" trupal") + sDim.Render(" · "+m.project+" · "+m.elapsed)

	// ── Header line 2: status indicators ──
	indicators := []string{}
	switch m.ccStatus {
	case "active", "thinking":
		indicators = append(indicators, sOk.Render("●")+" cc")
	default:
		indicators = append(indicators, sDim.Render("○")+" cc")
	}
	indicators = append(indicators, m.brainState)
	if m.buildState != "" {
		indicators = append(indicators, m.buildState)
	}
	if m.findings > 0 {
		indicators = append(indicators, sWarn.Render(fmt.Sprintf("⚠ %d", m.findings)))
	}
	if m.resolved > 0 {
		indicators = append(indicators, sOk.Render(fmt.Sprintf("✓ %d", m.resolved)))
	}
	h2 := " " + strings.Join(indicators, "  ")

	sep := sSep.Render(strings.Repeat("─", w))

	// ── Log area ──
	lh := m.logH()
	start, end := m.visibleLogRange()

	visible := make([]string, 0, lh)
	if start < end {
		for i, line := range m.lines[start:end] {
			absIdx := start + i
			if m.sel.IsLineSelected(absIdx) {
				startCol, endCol := m.sel.GetLineSelectionCols(absIdx)
				line = InjectCharacterRangeBackground(line, startCol, endCol)
			}
			visible = append(visible, line)
		}
	}
	for len(visible) < lh {
		visible = append(visible, "")
	}

	// ── Footer — truncate to fit pane width ──
	footerText := ""
	if m.scrollOffset > 0 {
		footerText = fmt.Sprintf(" ↑%d", m.scrollOffset)
	}
	if m.fileLine != "" {
		remaining := w - len(footerText) - 3
		fl := m.fileLine
		if len(fl) > remaining && remaining > 10 {
			fl = fl[:remaining-1] + "…"
		}
		footerText += "  " + fl
	}
	// Toast message (temporary)
	if m.toastMsg != "" && time.Now().Before(m.toastExpiry) {
		footerText += "  " + m.toastMsg
	}
	footer := sDim.Render(footerText)

	return h1 + "\n" + h2 + "\n" + sep + "\n" +
		strings.Join(visible, "\n") + "\n" +
		sep + "\n" + footer
}

// --- Helpers ---

func filesLine(files, newFiles []string) string {
	parts := []string{}
	if len(files) > 0 {
		parts = append(parts, fmt.Sprintf("%d mod: %s", len(files), joinBase(files, 3)))
	}
	if len(newFiles) > 0 {
		parts = append(parts, fmt.Sprintf("%d new: %s", len(newFiles), joinBase(newFiles, 2)))
	}
	return strings.Join(parts, "  ")
}

func wrap(text string, width int) []string {
	if width < 10 {
		width = 10
	}
	text = strings.ReplaceAll(text, "\n", " ")
	var lines []string
	for len(text) > 0 {
		if len(text) <= width {
			lines = append(lines, text)
			break
		}
		cut := width
		for cut > 0 && text[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = width
		}
		lines = append(lines, text[:cut])
		text = strings.TrimLeft(text[cut:], " ")
	}
	return lines
}

func joinBase(files []string, max int) string {
	var names []string
	for i, f := range files {
		if i >= max {
			names = append(names, fmt.Sprintf("+%d", len(files)-max))
			break
		}
		names = append(names, filepath.Base(f))
	}
	return strings.Join(names, " ")
}
