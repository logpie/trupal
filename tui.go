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
	headerStyle = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	cyanStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan
)

// --- Messages (events from the watcher goroutine) ---

type statusMsg struct {
	ccStatus  string
	buildOK   *bool
	buildErrs int
	buildTrend string
	files     []string
	newFiles  []string
	elapsed   string
	project   string
}

type nudgeMsg struct {
	finding BrainFinding
}

type resolvedMsg struct {
	finding BrainFinding
}

type trajectoryMsg struct {
	message string
}

type brainStatusMsg struct {
	thinking bool
	lastTime time.Time
}

type logLineMsg struct {
	line string
}

type tickMsg time.Time

// --- Model ---

type model struct {
	lines        []string // chat log lines
	width        int
	height       int
	scrollOffset int    // 0 = bottom (latest), positive = scrolled up
	ccStatus     string
	brainState   string
	buildState   string
	elapsed      string
	project      string
	lastFileLine string // dedup repeated file status
	quitting     bool
}

func initialModel(project string) model {
	return model{
		project:    project,
		ccStatus:   "starting",
		brainState: "starting",
		buildState: "",
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tickEvery(), tea.ClearScreen, tea.EnableMouseAllMotion)
}

func tickEvery() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

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
			if m.scrollOffset < len(m.lines)-1 {
				m.scrollOffset++
			}
		case "down", "j":
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}
		case "pgup":
			m.scrollOffset += 10
			max := len(m.lines) - 1
			if max < 0 {
				max = 0
			}
			if m.scrollOffset > max {
				m.scrollOffset = max
			}
		case "pgdown":
			m.scrollOffset -= 10
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
		case "home", "g":
			m.scrollOffset = len(m.lines) - 1
		case "end", "G":
			m.scrollOffset = 0
		}
		return m, nil

	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonWheelUp {
			m.scrollOffset += 3
			max := len(m.lines) - 1
			if max < 0 {
				max = 0
			}
			if m.scrollOffset > max {
				m.scrollOffset = max
			}
		} else if msg.Button == tea.MouseButtonWheelDown {
			m.scrollOffset -= 3
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
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
		// Log file changes only if different from last logged.
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
			m.addLine(dimStyle.Render(fileLine))
			m.lastFileLine = fileLine
		}
		return m, nil

	case nudgeMsg:
		icon := warnStyle.Render("⚠")
		if msg.finding.Severity == "error" {
			icon = errStyle.Render("✗")
		}
		m.addLine("")
		nudgeWidth := m.width - 12
		if nudgeWidth < 20 {
			nudgeWidth = 20
		}
		for i, line := range wordWrap(msg.finding.Nudge, nudgeWidth) {
			if i == 0 {
				m.addLine(fmt.Sprintf("%s %s", icon, line))
			} else {
				m.addContinuation("  " + line)
			}
		}
		if msg.finding.Reasoning != "" {
			for _, line := range wordWrap(msg.finding.Reasoning, nudgeWidth) {
				m.addContinuation("  " + dimStyle.Render(line))
			}
		}
		m.addLine("")
		return m, nil

	case resolvedMsg:
		resolveWidth := m.width - 12
		if resolveWidth < 20 {
			resolveWidth = 20
		}
		for i, line := range wordWrap(msg.finding.Nudge, resolveWidth) {
			if i == 0 {
				m.addLine(okStyle.Render("✓") + " " + dimStyle.Render(line))
			} else {
				m.addContinuation("  " + dimStyle.Render(line))
			}
		}
		return m, nil

	case trajectoryMsg:
		m.addLine(warnStyle.Render("▸") + " " + msg.message)
		return m, nil

	case brainStatusMsg:
		if msg.thinking {
			m.brainState = cyanStyle.Render("◌ analyzing")
		} else if !msg.lastTime.IsZero() {
			ago := int(time.Since(msg.lastTime).Seconds())
			if ago < 60 {
				m.brainState = fmt.Sprintf("✓ %ds ago", ago)
			} else {
				m.brainState = fmt.Sprintf("✓ %dm ago", ago/60)
			}
		}
		return m, nil

	case logLineMsg:
		m.addLine(msg.line)
		return m, nil
	}

	return m, nil
}

func (m *model) addLine(line string) {
	ts := dimStyle.Render(time.Now().Format("15:04:05"))
	if line == "" {
		m.lines = append(m.lines, "")
	} else {
		m.lines = append(m.lines, ts+" "+line)
	}
	// Auto-scroll to bottom if user is at bottom.
	if m.scrollOffset == 0 {
		// Already at bottom, stay there.
	}
	m.trimLines()
}

// addContinuation adds an indented line without timestamp (for word-wrap continuations).
func (m *model) addContinuation(line string) {
	m.lines = append(m.lines, "         "+line)
	m.trimLines()
}

func (m *model) trimLines() {
	if len(m.lines) > 200 {
		m.lines = m.lines[len(m.lines)-200:]
	}
	// Cap at 200 lines.
	if len(m.lines) > 200 {
		m.lines = m.lines[len(m.lines)-200:]
	}
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return "loading..."
	}

	// --- Header (fixed, 2 lines) ---
	ccIcon := dimStyle.Render("○")
	if m.ccStatus == "active" || m.ccStatus == "thinking" {
		ccIcon = okStyle.Render("●")
	}

	header := fmt.Sprintf(" %s  %s cc  %s  %s",
		headerStyle.Render("trupal"),
		ccIcon,
		m.brainState,
		m.buildState,
	)
	sep := dimStyle.Render(strings.Repeat("─", m.width))

	// --- Footer (fixed, 1 line) ---
	footer := dimStyle.Render(fmt.Sprintf(" %s · %s", m.project, m.elapsed))

	// --- Log area (scrollable, fills remaining space) ---
	headerLines := 2 // header + separator
	footerLines := 2 // separator + footer
	logHeight := m.height - headerLines - footerLines
	if logHeight < 1 {
		logHeight = 1
	}

	// Window into the log based on scroll offset.
	total := len(m.lines)
	end := total - m.scrollOffset
	if end < 0 {
		end = 0
	}
	start := end - logHeight
	if start < 0 {
		start = 0
	}

	visibleLines := make([]string, 0, logHeight)
	if start < end && end <= total {
		visibleLines = append(visibleLines, m.lines[start:end]...)
	}

	// Pad with empty lines.
	for len(visibleLines) < logHeight {
		visibleLines = append(visibleLines, "")
	}

	// Scroll indicator in footer.
	scrollHint := ""
	if m.scrollOffset > 0 {
		scrollHint = fmt.Sprintf(" ↑%d", m.scrollOffset)
	}

	logContent := strings.Join(visibleLines, "\n")

	return header + "\n" + sep + "\n" + logContent + "\n" + sep + "\n" + footer + dimStyle.Render(scrollHint)
}

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
