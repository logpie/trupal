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

	sNudgeWarnMarker = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("3"))
	sNudgeWarnText   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	sNudgeErrMarker  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("1"))
	sNudgeErrText    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))

	sHeaderTitle  = lipgloss.NewStyle().Bold(true).PaddingLeft(1)
	sHeaderLine   = lipgloss.NewStyle().PaddingLeft(1)
	sIndicatorGap = lipgloss.NewStyle().PaddingRight(2)
	sFooterLine   = lipgloss.NewStyle().Faint(true).PaddingLeft(1)

	sLogTimeCell   = lipgloss.NewStyle().Faint(true).Width(logTimeWidth).Align(lipgloss.Right)
	sLogGapCell    = lipgloss.NewStyle().Width(logGapWidth)
	sLogMarkerCell = lipgloss.NewStyle().Width(logMarkerWidth).Align(lipgloss.Center)
	sLogGutterCell = lipgloss.NewStyle().Faint(true).Width(logMarkerWidth).Align(lipgloss.Center)
)

const (
	logTimeWidth   = 5
	logGapWidth    = 2
	logMarkerWidth = 2
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
type observationMsg struct{ text string }
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
	buildState string
	findings   int // active count
	resolved   int
	brain      brainIndicatorState

	// Footer state
	fileLine    string // current files summary
	toastMsg    string // transient message (e.g. "copied!")
	toastExpiry time.Time

	// Selection
	sel *Selection

	quitting bool
}

type brainIndicatorState struct {
	thinking bool
	lastTime time.Time
	tick     int
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func initialModel(project string) model {
	return model{
		project:  project,
		ccStatus: "starting",
		sel:      NewSelection(),
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
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
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
			Debugf("[sel] press x=%d y=%d", msg.X, msg.Y)
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
				Debugf("[sel] motion-start x=%d y=%d line=%d col=%d", msg.X, msg.Y, point.Line, point.Col)
				m.sel.PrepareDrag(point.Line, point.Col, m.logRect())
			}
			m.sel.HandleDrag(point.Line, point.Col)
		case msg.Action == tea.MouseActionRelease:
			Debugf("[sel] release btn=%d anchor=%v active=%v", msg.Button, m.sel.Anchor.Valid(), m.sel.Active)
			if m.sel.Anchor.Valid() {
				if point, ok := m.selectionPointAt(msg.X, msg.Y, true); ok {
					if m.sel.Active || point.Line != m.sel.Anchor.Line || point.Col != m.sel.Anchor.Col {
						m.sel.HandleDrag(point.Line, point.Col)
					}
				}
				if m.sel.FinishDrag() {
					text := m.sel.SelectedText(m.lines, selectionTabWidth)
					Debugf("[sel] copied %d chars", len(text))
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
		if m.brain.thinking {
			m.brain.tick++
		}
		return m, tickEvery()

	// Spinner animation happens via tickEvery (3s ticks)
	// Brain tick advances each cycle when thinking

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
				label := "build failing"
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
		label, textStyle := nudgePresentation(msg.finding.Severity)
		if msg.finding.Reasoning != "" {
			m.logStyled(sCyan.Bold(true).Render("i"), msg.finding.Reasoning, m.width, lipgloss.NewStyle())
		}
		m.logStyled(label, msg.finding.Nudge, m.width, textStyle)

	case resolvedMsg:
		m.findings--
		if m.findings < 0 {
			m.findings = 0
		}
		m.resolved++
		m.logStyled(sOk.Bold(true).Render("✓"), msg.finding.Nudge, m.width, sDim)

	case observationMsg:
		m.logStyled(sCyan.Bold(true).Render("i"), msg.text, m.width, lipgloss.NewStyle())

	case trajectoryMsg:
		m.logStyled(sWarn.Bold(true).Render("→"), msg.message, m.width, lipgloss.NewStyle())

	case brainStatusMsg:
		if msg.thinking {
			m.brain.thinking = true
			m.brain.tick = 0
		} else {
			m.brain.thinking = false
			if !msg.lastTime.IsZero() {
				m.brain.lastTime = msg.lastTime
			}
		}

	case SelectionCopiedMsg:
		if msg.Err != nil {
			m.toastMsg = "⚠ copy failed"
			Debugf("[sel] copy failed: %v", msg.Err)
		} else {
			m.toastMsg = "✓ copied! paste with prefix+]"
			Debugf("[sel] copied to tmux buffer: %d chars", len(msg.Text))
		}
		m.toastExpiry = time.Now().Add(3 * time.Second)
		m.sel.Clear()
		return m, nil

	case logLineMsg:
		m.log(msg.line)
	}

	return m, nil
}

func (m model) brainIndicator() string {
	if m.brain.thinking {
		frame := spinnerFrames[m.brain.tick%len(spinnerFrames)]
		return sCyan.Render(frame) + " analyzing"
	}
	if !m.brain.lastTime.IsZero() {
		ago := int(time.Since(m.brain.lastTime).Seconds())
		if ago < 0 {
			ago = 0
		}
		if ago < 60 {
			return sOk.Render("●") + sDim.Render(fmt.Sprintf(" %ds ago", ago))
		}
		return sOk.Render("●") + sDim.Render(fmt.Sprintf(" %dm ago", ago/60))
	}
	return sDim.Render("starting")
}

// --- Log helpers ---

// logStyled renders a compact event lane tuned for narrow panes:
// "HH:MM  !  first line of text"
// "       │  continuation line"
// Wrap width is derived from the visible prefix width so the text uses the
// full pane, even when styles add ANSI escape sequences.
func (m *model) logStyled(label, text string, w int, textStyle lipgloss.Style) {
	textW := logTextWidth(w)
	if textW < 18 {
		textW = 18
	}
	lines := wrap(text, textW)
	if len(lines) == 0 {
		lines = []string{""}
	}

	ts := time.Now().Format("15:04")
	for i, line := range lines {
		body := textStyle.Render(line)
		if i == 0 {
			m.lines = append(m.lines, renderLogLine(ts, label, body))
			m.trim()
		} else {
			m.lines = append(m.lines, renderContinuationLine(body))
			m.trim()
		}
	}
}

func (m *model) log(line string) {
	ts := sLogTimeCell.Render(time.Now().Format("15:04"))
	if line == "" {
		m.lines = append(m.lines, "")
	} else {
		m.lines = append(m.lines, lipgloss.JoinHorizontal(lipgloss.Top, ts, sLogGapCell.Render(""), line))
	}
	if m.scrollOffset == 0 { /* auto-scroll: already at bottom */
	}
	m.trim()
}

func (m *model) raw(line string) {
	m.lines = append(m.lines, lipgloss.JoinHorizontal(
		lipgloss.Top,
		sLogTimeCell.Render(""),
		sLogGapCell.Render(""),
		line,
	))
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
	w := logTextWidth(m.width)
	if w < 18 {
		return 18
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
	h1 := lipgloss.JoinHorizontal(
		lipgloss.Top,
		sHeaderTitle.Render("trupal"),
		sDim.Render(" · "+m.project+" · "+m.elapsed),
	)

	// ── Header line 2: status indicators ──
	indicators := []string{}
	switch m.ccStatus {
	case "active", "thinking":
		indicators = append(indicators, sOk.Render("●")+" cc")
	default:
		indicators = append(indicators, sDim.Render("○")+" cc")
	}
	indicators = append(indicators, m.brainIndicator())
	if m.buildState != "" {
		indicators = append(indicators, m.buildState)
	}
	if m.findings > 0 {
		indicators = append(indicators, sWarn.Render(fmt.Sprintf("⚠ %d", m.findings)))
	}
	if m.resolved > 0 {
		indicators = append(indicators, sOk.Render(fmt.Sprintf("✓ %d", m.resolved)))
	}
	h2Items := make([]string, 0, len(indicators))
	for i, indicator := range indicators {
		if i < len(indicators)-1 {
			h2Items = append(h2Items, sIndicatorGap.Render(indicator))
		} else {
			h2Items = append(h2Items, indicator)
		}
	}
	h2 := sHeaderLine.Render(lipgloss.JoinHorizontal(lipgloss.Top, h2Items...))

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
	footerParts := []string{}
	if m.scrollOffset > 0 {
		footerParts = append(footerParts, fmt.Sprintf("↑%d", m.scrollOffset))
	}
	if m.fileLine != "" {
		used := joinWidth(footerParts, "  ")
		remaining := w - used - 1
		if len(footerParts) > 0 {
			remaining -= 2
		}
		if remaining > 0 {
			footerParts = append(footerParts, truncateWidth(m.fileLine, remaining))
		}
	}
	// Toast message (temporary)
	if m.toastMsg != "" && time.Now().Before(m.toastExpiry) {
		used := joinWidth(footerParts, "  ")
		remaining := w - used - 1
		if len(footerParts) > 0 {
			remaining -= 2
		}
		if remaining > 0 {
			footerParts = append(footerParts, truncateWidth(m.toastMsg, remaining))
		}
	}
	footer := sFooterLine.Render(strings.Join(footerParts, "  "))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		h1,
		h2,
		sep,
		strings.Join(visible, "\n"),
		sep,
		footer,
	)
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
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if text == "" {
		return nil
	}

	words := strings.Fields(text)
	lines := make([]string, 0, len(words))
	current := ""

	for _, word := range words {
		if lipgloss.Width(word) > width {
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
			lines = append(lines, wrapWord(word, width)...)
			continue
		}

		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if lipgloss.Width(candidate) <= width {
			current = candidate
			continue
		}

		lines = append(lines, current)
		current = word
	}

	if current != "" {
		lines = append(lines, current)
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

func logTextWidth(total int) int {
	return total - lipgloss.Width(logPrefix("", ""))
}

func renderLogLine(ts, marker, text string) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		sLogTimeCell.Render(ts),
		sLogGapCell.Render(""),
		sLogMarkerCell.Render(marker),
		sLogGapCell.Render(""),
		text,
	)
}

func renderContinuationLine(text string) string {
	return renderContinuationLineWithMarker("│", text)
}

func renderContinuationLineWithMarker(marker, text string) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		sLogTimeCell.Render(""),
		sLogGapCell.Render(""),
		sLogGutterCell.Render(marker),
		sLogGapCell.Render(""),
		text,
	)
}

func nudgePresentation(severity string) (string, lipgloss.Style) {
	if severity == "error" {
		return sNudgeErrMarker.Render("⚠"), sNudgeErrText
	}
	return sNudgeWarnMarker.Render("⚡"), sNudgeWarnText
}

func logPrefix(ts, marker string) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		sLogTimeCell.Render(ts),
		sLogGapCell.Render(""),
		sLogMarkerCell.Render(marker),
		sLogGapCell.Render(""),
	)
}

func wrapWord(word string, width int) []string {
	if width < 1 {
		return []string{word}
	}

	var parts []string
	var chunk strings.Builder
	for _, r := range word {
		next := chunk.String() + string(r)
		if chunk.Len() > 0 && lipgloss.Width(next) > width {
			parts = append(parts, chunk.String())
			chunk.Reset()
		}
		chunk.WriteRune(r)
	}
	if chunk.Len() > 0 {
		parts = append(parts, chunk.String())
	}
	return parts
}

func truncateWidth(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}

	var out strings.Builder
	for _, r := range text {
		next := out.String() + string(r)
		if lipgloss.Width(next+"…") > width {
			break
		}
		out.WriteRune(r)
	}
	return out.String() + "…"
}

func joinWidth(parts []string, sep string) int {
	if len(parts) == 0 {
		return 0
	}
	return lipgloss.Width(strings.Join(parts, sep))
}
