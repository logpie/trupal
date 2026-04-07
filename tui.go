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
	return tea.Batch(tickEvery(), tea.ClearScreen)
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
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
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
		// Word-wrap nudge to pane width.
		nudgeWidth := m.width - 8 // leave room for timestamp + icon
		if nudgeWidth < 20 {
			nudgeWidth = 20
		}
		for i, line := range wordWrap(msg.finding.Nudge, nudgeWidth) {
			if i == 0 {
				m.addLine(fmt.Sprintf("%s %s", icon, line))
			} else {
				m.addLine("  " + line)
			}
		}
		// Reasoning word-wrapped too.
		if msg.finding.Reasoning != "" {
			for _, line := range wordWrap(msg.finding.Reasoning, nudgeWidth) {
				m.addLine("  " + dimStyle.Render(line))
			}
		}
		m.addLine("")
		return m, nil

	case resolvedMsg:
		m.addLine(okStyle.Render("✓") + " " + dimStyle.Render(msg.finding.Nudge))
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
	ts := dimStyle.Render(time.Now().Format("15:04"))
	if line == "" {
		m.lines = append(m.lines, "")
	} else {
		m.lines = append(m.lines, ts+" "+line)
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
	footer := dimStyle.Render(fmt.Sprintf(" %s  session: %s", m.project, m.elapsed))

	// --- Log area (scrollable, fills remaining space) ---
	headerLines := 2 // header + separator
	footerLines := 2 // separator + footer
	logHeight := m.height - headerLines - footerLines
	if logHeight < 1 {
		logHeight = 1
	}

	// Get the last N lines that fit.
	visibleLines := m.lines
	if len(visibleLines) > logHeight {
		visibleLines = visibleLines[len(visibleLines)-logHeight:]
	}

	// Pad with empty lines if log is shorter than available space.
	for len(visibleLines) < logHeight {
		visibleLines = append(visibleLines, "")
	}

	logContent := strings.Join(visibleLines, "\n")

	return header + "\n" + sep + "\n" + logContent + "\n" + sep + "\n" + footer
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
