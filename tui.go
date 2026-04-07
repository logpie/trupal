package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Styles (pre-defined, reused) ---

var (
	headerStyle = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	cyanStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	sepStyle    = lipgloss.NewStyle().Faint(true)
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

// --- Model ---

type model struct {
	lines        []string
	width        int
	height       int
	scrollOffset int // 0 = bottom, positive = lines scrolled up
	ccStatus     string
	brainState   string
	buildState   string
	elapsed      string
	project      string
	lastFileLine string
	quitting     bool
}

func initialModel(project string) model {
	return model{
		project:    project,
		ccStatus:   "starting",
		brainState: "starting",
	}
}

func (m model) Init() tea.Cmd {
	// No mouse capture — preserves native text selection/copy.
	// Use keyboard for scrolling (j/k, pgup/pgdown, g/G).
	return tickEvery()
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
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			m.scrollUp(1)
		case "down", "j":
			m.scrollDown(1)
		case "pgup":
			m.scrollUp(10)
		case "pgdown":
			m.scrollDown(10)
		case "home", "g":
			m.scrollOffset = m.maxScroll()
		case "end", "G":
			m.scrollOffset = 0
		}
		return m, nil

	case tickMsg:
		return m, tickEvery()

	case statusMsg:
		m.ccStatus = msg.ccStatus
		if msg.elapsed != "" {
			m.elapsed = msg.elapsed
		}
		if msg.project != "" {
			m.project = msg.project
		}
		if msg.buildOK != nil {
			if *msg.buildOK {
				m.buildState = okStyle.Render("✓ build")
			} else {
				label := fmt.Sprintf("✗ %d err", msg.buildErrs)
				if msg.buildTrend != "" {
					label += " (" + msg.buildTrend + ")"
				}
				m.buildState = errStyle.Render(label)
			}
		}
		fileLine := ""
		if len(msg.files) > 0 || len(msg.newFiles) > 0 {
			parts := []string{}
			if len(msg.files) > 0 {
				parts = append(parts, fmt.Sprintf("%d mod: %s", len(msg.files), joinBase(msg.files, 3)))
			}
			if len(msg.newFiles) > 0 {
				parts = append(parts, fmt.Sprintf("%d new: %s", len(msg.newFiles), joinBase(msg.newFiles, 2)))
			}
			fileLine = strings.Join(parts, "  ")
		}
		if fileLine != "" && fileLine != m.lastFileLine {
			m.appendLog(dimStyle.Render(fileLine))
			m.lastFileLine = fileLine
		}
		return m, nil

	case nudgeMsg:
		icon := warnStyle.Render("⚠")
		if msg.finding.Severity == "error" {
			icon = errStyle.Render("✗")
		}
		w := m.contentWidth()
		m.appendLog("")
		lines := wordWrap(msg.finding.Nudge, w)
		for i, line := range lines {
			if i == 0 {
				m.appendLog(icon + " " + line)
			} else {
				m.appendRaw("  " + line)
			}
		}
		if msg.finding.Reasoning != "" {
			for _, line := range wordWrap(msg.finding.Reasoning, w) {
				m.appendRaw("  " + dimStyle.Render(line))
			}
		}
		m.appendLog("")
		return m, nil

	case resolvedMsg:
		w := m.contentWidth()
		for i, line := range wordWrap(msg.finding.Nudge, w) {
			if i == 0 {
				m.appendLog(okStyle.Render("✓") + " " + dimStyle.Render(line))
			} else {
				m.appendRaw("  " + dimStyle.Render(line))
			}
		}
		return m, nil

	case trajectoryMsg:
		m.appendLog(warnStyle.Render("▸") + " " + msg.message)
		return m, nil

	case brainStatusMsg:
		if msg.thinking {
			m.brainState = cyanStyle.Render("◌ analyzing")
		} else if !msg.lastTime.IsZero() {
			ago := int(time.Since(msg.lastTime).Seconds())
			if ago < 60 {
				m.brainState = dimStyle.Render(fmt.Sprintf("✓ %ds ago", ago))
			} else {
				m.brainState = dimStyle.Render(fmt.Sprintf("✓ %dm ago", ago/60))
			}
		}
		return m, nil

	case logLineMsg:
		m.appendLog(msg.line)
		return m, nil
	}

	return m, nil
}

// --- Log management ---

// appendLog adds a timestamped line. Auto-scrolls to bottom if at bottom.
func (m *model) appendLog(line string) {
	ts := dimStyle.Render(time.Now().Format("15:04:05"))
	if line == "" {
		m.lines = append(m.lines, "")
	} else {
		m.lines = append(m.lines, ts+" "+line)
	}
	// Auto-scroll: if user was at bottom, stay at bottom.
	if m.scrollOffset == 0 {
		// Already at bottom.
	}
	m.trimLines()
}

// appendRaw adds a continuation line (no timestamp, indented).
func (m *model) appendRaw(line string) {
	m.lines = append(m.lines, "         "+line)
	m.trimLines()
}

func (m *model) trimLines() {
	if len(m.lines) > 500 {
		m.lines = m.lines[len(m.lines)-500:]
	}
}

func (m *model) scrollUp(n int) {
	m.scrollOffset += n
	max := m.maxScroll()
	if m.scrollOffset > max {
		m.scrollOffset = max
	}
}

func (m *model) scrollDown(n int) {
	m.scrollOffset -= n
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m model) maxScroll() int {
	logH := m.logHeight()
	if len(m.lines) <= logH {
		return 0
	}
	return len(m.lines) - logH
}

func (m model) logHeight() int {
	h := m.height - 4 // header(1) + sep(1) + sep(1) + footer(1)
	if h < 1 {
		return 1
	}
	return h
}

func (m model) contentWidth() int {
	w := m.width - 12 // timestamp + padding
	if w < 20 {
		return 20
	}
	return w
}

// --- View ---

func (m model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return "starting..."
	}

	// Header
	ccIcon := dimStyle.Render("○")
	if m.ccStatus == "active" || m.ccStatus == "thinking" {
		ccIcon = okStyle.Render("●")
	}
	header := lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Render(
		fmt.Sprintf(" %s  %s cc  %s  %s",
			headerStyle.Render("trupal"), ccIcon, m.brainState, m.buildState))

	sep := sepStyle.Render(strings.Repeat("─", m.width))

	// Footer
	scrollHint := ""
	if m.scrollOffset > 0 {
		scrollHint = fmt.Sprintf(" ↑%d", m.scrollOffset)
	}
	footer := lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Render(
		dimStyle.Render(fmt.Sprintf(" %s · %s%s", m.project, m.elapsed, scrollHint)))

	// Log area — windowed view into m.lines
	logH := m.logHeight()
	total := len(m.lines)
	end := total - m.scrollOffset
	start := end - logH
	if end > total {
		end = total
	}
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}

	visible := make([]string, 0, logH)
	if start < end {
		visible = append(visible, m.lines[start:end]...)
	}

	// Truncate long lines to pane width to prevent wrapping.
	for i, line := range visible {
		visible[i] = lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Render(line)
	}

	// Pad to fill log area so footer stays at bottom.
	for len(visible) < logH {
		visible = append(visible, "")
	}

	return header + "\n" + sep + "\n" +
		strings.Join(visible, "\n") + "\n" +
		sep + "\n" + footer
}

// --- Helpers ---

func wordWrap(text string, width int) []string {
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
