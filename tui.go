package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// --- Styles ---

var (
	sTitle = lipgloss.NewStyle().Bold(true)
	sDim   = lipgloss.NewStyle().Faint(true)
	sWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	sErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	sOk    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	sCyan  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	sSep   = lipgloss.NewStyle().Faint(true)

	sNudgeMarker = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	sNudgeText   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	sHeaderTitle  = lipgloss.NewStyle().Bold(true).PaddingLeft(1)
	sHeaderLine   = lipgloss.NewStyle().PaddingLeft(1)
	sBrandChip    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	sMetaChip     = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236")).Padding(0, 1)
	sStatusChip   = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")).Padding(0, 1)
	sStatusChipOk = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("22")).Padding(0, 1)
	sStatusChipHi = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("17")).Padding(0, 1)
	sStatusChipWr = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("94")).Padding(0, 1)
	sStatusChipEr = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("88")).Padding(0, 1)
	sIndicatorGap = lipgloss.NewStyle().PaddingRight(2)
	sFooterLine   = lipgloss.NewStyle().Faint(true).PaddingLeft(1)

	sLogTimeCell   = lipgloss.NewStyle().Faint(true).Width(logTimeWidth).Align(lipgloss.Right)
	sLogGapCell    = lipgloss.NewStyle().Width(logGapWidth)
	sLogMarkerCell = lipgloss.NewStyle().Width(logMarkerWidth).Align(lipgloss.Center)
	sLogGutterCell = lipgloss.NewStyle().Faint(true).Width(logMarkerWidth).Align(lipgloss.Center)

	sIssueTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24"))
	sIssueBullet  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	sIssueText    = lipgloss.NewStyle().Foreground(lipgloss.Color("230"))
	sIssueWhy     = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	sIssuePreview = lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
	sFocusLabel   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	sFocusBody    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	sDetailCode   = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236"))
	sDetailCodeNo = lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	sInspectorTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	sInspectorBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("239")).Padding(0, 1).Foreground(lipgloss.Color("252"))
	sInspectorCode  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Background(lipgloss.Color("235")).Padding(0, 1).Foreground(lipgloss.Color("252"))
)

const (
	logTimeWidth   = 5
	logGapWidth    = 2
	logMarkerWidth = 2
)

// --- Messages ---

type statusMsg struct {
	agentLabel string
	ccStatus   string
	buildOK    *bool
	buildErrs  int
	buildTrend string
	files      []string
	newFiles   []string
	elapsed    string
	project    string
	findings   int
	resolved   int
	issues     []CurrentIssue
}
type nudgeMsg struct {
	finding BrainFinding
	detail  []string
}
type resolvedMsg struct{ finding BrainFinding }
type trajectoryMsg struct{ message string }
type patternMsg struct{ finding PatternFinding }
type brainStatusMsg struct {
	thinking bool
	lastTime time.Time
}
type brainStatsMsg struct{ stats BrainStats }
type logLineMsg struct{ line string }
type replaceStatusMsg struct{ line string }
type tickMsg time.Time

var copySelectedText = CopySelectedToClipboard

type timelineEntry struct {
	ID      string
	Kind    string
	Time    string
	Marker  string
	Summary string
	Detail  []string
}

// --- Model ---

type model struct {
	// Timeline
	entries       []timelineEntry
	selectedEntry int
	scrollOffset  int
	recentEvents []string
	lines        []string // legacy raw line buffer used by low-level log/selection tests

	// Layout
	width  int
	height int

	// Header state
	project    string
	elapsed    string
	agentLabel string
	ccStatus   string
	buildState string
	findings   int // active count
	resolved   int
	brain      brainIndicatorState

	// Footer state
	fileLine           string // current files summary
	issueItems         []CurrentIssue
	issueCursor        int
	issuesPopupVisible bool
	issuePanelVisible  bool // compatibility alias for existing tests while popup work lands
	detailOpen  map[string]bool
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
	stats    BrainStats
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func initialModel(project string) model {
	return model{
		project:    project,
		agentLabel: "agent",
		ccStatus:   "starting",
		detailOpen: make(map[string]bool),
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
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) popupVisible() bool { return m.issuesPopupVisible || m.issuePanelVisible }

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
			if m.issuesPopupVisible {
				if len(m.issueItems) > 0 {
					m.issueCursor = (m.issueCursor - 1 + len(m.issueItems)) % len(m.issueItems)
				}
			} else {
				m.moveSelection(-1)
			}
		case "down", "j":
			if m.issuesPopupVisible {
				if len(m.issueItems) > 0 {
					m.issueCursor = (m.issueCursor + 1) % len(m.issueItems)
				}
			} else {
				m.moveSelection(1)
			}
		case "pgup":
			m.scroll(10)
		case "pgdown":
			m.scroll(-10)
		case "g", "home":
			if len(m.entries) > 0 {
				m.selectedEntry = 0
				m.scrollSelectedIntoView()
			} else {
				m.scrollOffset = m.maxScroll()
			}
		case "G", "end":
			if len(m.entries) > 0 {
				m.selectedEntry = len(m.entries) - 1
				m.scrollSelectedIntoView()
			} else {
				m.scrollOffset = 0
			}
		case "o":
			if m.popupVisible() {
				m.issuesPopupVisible = false
				m.issuePanelVisible = false
			}
			if len(m.entries) > 0 {
				entry := m.entries[m.selectedEntry]
				if len(entry.Detail) > 0 {
					key := entry.ID
					if key == "" {
						key = fmt.Sprintf("entry-%d", m.selectedEntry)
					}
					m.toggleEntryDetail(key)
				}
			}
		case "p":
			if len(m.issueItems) > 0 {
				for key := range m.detailOpen {
					m.detailOpen[key] = false
				}
				next := !m.popupVisible()
				m.issuesPopupVisible = next
				m.issuePanelVisible = next
				if next {
					m.syncIssueCursorToSelection()
				}
			}
		case "]":
			if len(m.issueItems) > 0 && m.popupVisible() {
				m.issueCursor = (m.issueCursor + 1) % len(m.issueItems)
			}
		case "[":
			if len(m.issueItems) > 0 && m.popupVisible() {
				m.issueCursor = (m.issueCursor - 1 + len(m.issueItems)) % len(m.issueItems)
			}
		case "esc":
			m.issuesPopupVisible = false
			m.issuePanelVisible = false
		case "enter", " ":
			if m.popupVisible() && len(m.issueItems) > 0 {
				m.jumpToIssue(m.issueItems[m.issueCursor].Key())
				m.issuesPopupVisible = false
				m.issuePanelVisible = false
			}
		}

	case tea.MouseMsg:
		switch {
		case msg.Button == tea.MouseButtonWheelUp && m.popupVisible() && msg.Y >= 3 && msg.Y < 3+len(m.issuePinnedLines()):
			if len(m.issueItems) > 0 {
				m.issueCursor = (m.issueCursor - 1 + len(m.issueItems)) % len(m.issueItems)
			}
		case msg.Button == tea.MouseButtonWheelDown && m.popupVisible() && msg.Y >= 3 && msg.Y < 3+len(m.issuePinnedLines()):
			if len(m.issueItems) > 0 {
				m.issueCursor = (m.issueCursor + 1) % len(m.issueItems)
			}
		case msg.Button == tea.MouseButtonWheelUp && m.bodyRect().Contains(msg.X, msg.Y):
			m.scroll(3)
		case msg.Button == tea.MouseButtonWheelDown && m.bodyRect().Contains(msg.X, msg.Y):
			m.scroll(-3)
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
			Debugf("[sel] press x=%d y=%d", msg.X, msg.Y)
			point, ok := m.selectionPointAt(msg.X, msg.Y, false)
			if !ok {
				m.sel.Clear()
				return m, nil
			}
			m.sel.PrepareDrag(point.Line, point.Col, m.bodyRect())
		case msg.Action == tea.MouseActionMotion && (msg.Button == tea.MouseButtonLeft || (msg.Button == tea.MouseButtonNone && m.sel.Anchor.Valid())):
			if m.sel.Anchor.Valid() {
				m.autoScrollSelection(msg.Y)
			}
			point, ok := m.selectionPointAt(msg.X, msg.Y, true)
			if !ok {
				return m, nil
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
					text := m.sel.SelectedText(m.contentLines(), selectionTabWidth)
					Debugf("[sel] copied %d chars", len(text))
					if text != "" {
						m.sel.Clear()
						return m, func() tea.Msg {
							return SelectionCopiedMsg{
								Text: text,
								Time: time.Now(),
								Err:  copySelectedText(text),
							}
						}
					}
				}
				m.sel.Clear()
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
		if msg.agentLabel != "" {
			m.agentLabel = msg.agentLabel
		}
		if msg.ccStatus != "" {
			m.ccStatus = msg.ccStatus
		}
		if msg.elapsed != "" {
			m.elapsed = msg.elapsed
		}
		if msg.project != "" {
			m.project = msg.project
		}
		if msg.buildOK == nil {
			m.buildState = ""
		} else {
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
		m.fileLine = filesLine(msg.files, msg.newFiles)
		prevKey := ""
		if len(m.issueItems) > 0 && m.issueCursor < len(m.issueItems) {
			prevKey = m.issueItems[m.issueCursor].Key()
		}
		m.issueItems = append([]CurrentIssue(nil), msg.issues...)
		m.issueCursor = 0
		for i, issue := range m.issueItems {
			if issue.Key() == prevKey {
				m.issueCursor = i
				break
			}
		}
		m.findings = msg.findings
		m.resolved = msg.resolved

	case nudgeMsg:
		m.findings++
		m.logIssueEvent("nudge", msg.finding, msg.detail)

	case resolvedMsg:
		m.findings--
		if m.findings < 0 {
			m.findings = 0
		}
		m.resolved++
		resolved := false
		for i := range m.entries {
			if m.entries[i].ID != msg.finding.ID {
				continue
			}
			m.entries[i].Kind = "resolved"
			m.entries[i].Time = time.Now().Format("15:04")
			m.entries[i].Marker = "✓"
			m.entries[i].Summary = normalizeIssueText(msg.finding.Nudge)
			delete(m.detailOpen, msg.finding.ID)
			resolved = true
			break
		}
		if !resolved {
			text := "resolved: " + normalizeIssueText(msg.finding.Nudge)
			if m.shouldLogEvent("resolved", text) {
				m.appendEntry(timelineEntry{
					ID:      msg.finding.ID,
					Kind:    "resolved",
					Time:    time.Now().Format("15:04"),
					Marker:  "✓",
					Summary: normalizeIssueText(msg.finding.Nudge),
				})
			}
		}

	case trajectoryMsg:
		m.appendEntry(timelineEntry{
			Kind:    "note",
			Time:    time.Now().Format("15:04"),
			Marker:  "→",
			Summary: msg.message,
		})

	case patternMsg:
		m.logIssueEvent("pattern", BrainFinding{
			ID:       msg.finding.Key,
			Severity: msg.finding.Level,
			Nudge:    msg.finding.Message,
			Why:      shortIssueWhy(msg.finding),
		}, nil)

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

	case brainStatsMsg:
		m.brain.stats = msg.stats

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
		m.appendEntry(timelineEntry{
			Kind:    "log",
			Time:    time.Now().Format("15:04"),
			Marker:  "",
			Summary: msg.line,
		})

	case replaceStatusMsg:
		for i := len(m.entries) - 1; i >= 0; i-- {
			if m.entries[i].Kind == "log" {
				m.entries[i].Time = time.Now().Format("15:04")
				m.entries[i].Summary = msg.line
				return m, nil
			}
		}
		m.appendEntry(timelineEntry{
			Kind:    "log",
			Time:    time.Now().Format("15:04"),
			Marker:  "",
			Summary: msg.line,
		})
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

func formatCostUSD(cost float64) string {
	if cost == 0 {
		return "$0.00"
	}
	return fmt.Sprintf("$%.4f", cost)
}

func formatTokenCount(n int) string {
	if n < 10000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 100000 {
		value := float64(n) / 1000
		text := fmt.Sprintf("%.1fK", value)
		return strings.Replace(text, ".0K", "K", 1)
	}
	return fmt.Sprintf("%dK", (n+500)/1000)
}

func brainStatsCandidates(stats BrainStats) []string {
	cost := ""
	if stats.CostKnown {
		cost = formatCostUSD(stats.TotalCostUSD)
	}
	last := ""
	if stats.LastDuration > 0 || strings.TrimSpace(stats.LastEffort) != "" {
		last = fmt.Sprintf("last=%s/%s", roundDuration(stats.LastDuration), defaultString(stats.LastEffort, "?"))
	}
	if stats.PromptTokens() == 0 && stats.TotalOutputTokens == 0 {
		if cost != "" {
			if last != "" {
				return []string{last + " " + cost, cost}
			}
			return []string{cost}
		}
		if last != "" {
			return []string{last, "brain idle"}
		}
		return []string{"brain idle"}
	}

	uncached := formatTokenCount(stats.UncachedPromptTokens())
	out := formatTokenCount(stats.TotalOutputTokens)
	cacheRead := formatTokenCount(stats.TotalCacheReadTokens)
	cacheCreate := formatTokenCount(stats.TotalCacheCreationTokens)
	prompt := formatTokenCount(stats.PromptTokens())
	cachePct := fmt.Sprintf("%d%%", stats.CacheHitRate())

	var candidates []string
	if cost != "" {
		candidates = append(candidates,
			joinNonEmpty(" ", fmt.Sprintf("prompt=%s uncached=%s cache_read=%s cache_create=%s %s out=%s", prompt, uncached, cacheRead, cacheCreate, cachePct, out), last, "cost="+cost),
			joinNonEmpty(" ", fmt.Sprintf("prompt=%s uncached=%s cached=%s %s out=%s", prompt, uncached, cacheRead, cachePct, out), last, cost),
			joinNonEmpty(" ", fmt.Sprintf("uncached=%s cached=%s %s out=%s", uncached, cacheRead, cachePct, out), last, cost),
			joinNonEmpty(" ", fmt.Sprintf("cached=%s %s out=%s", cacheRead, cachePct, out), last, cost),
			fmt.Sprintf("%s %s cache", cost, cachePct),
			cost,
		)
	} else {
		candidates = append(candidates,
			joinNonEmpty(" ", fmt.Sprintf("prompt=%s uncached=%s cache_read=%s cache_create=%s %s out=%s", prompt, uncached, cacheRead, cacheCreate, cachePct, out), last),
			joinNonEmpty(" ", fmt.Sprintf("prompt=%s uncached=%s cached=%s %s out=%s", prompt, uncached, cacheRead, cachePct, out), last),
			joinNonEmpty(" ", fmt.Sprintf("uncached=%s cached=%s %s out=%s", uncached, cacheRead, cachePct, out), last),
			joinNonEmpty(" ", fmt.Sprintf("cached=%s %s out=%s", cacheRead, cachePct, out), last),
			joinNonEmpty(" ", fmt.Sprintf("cached=%s %s", cacheRead, cachePct), last),
			last,
			cachePct,
		)
	}
	return candidates
}

func joinNonEmpty(sep string, parts ...string) string {
	var filtered []string
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, sep)
}

func chooseBrainStatsIndicator(stats BrainStats, maxWidth int) string {
	candidates := brainStatsCandidates(stats)
	if maxWidth <= 0 {
		return candidates[len(candidates)-1]
	}
	for _, candidate := range candidates {
		if lipgloss.Width(candidate) <= maxWidth {
			return candidate
		}
	}
	return candidates[len(candidates)-1]
}

// --- Log helpers ---

// logStyled renders a compact event lane tuned for narrow panes:
// "HH:MM  !  first line of text"
// "       │  continuation line"
// Wrap width is derived from the visible prefix width so the text uses the
// full pane, even when styles add ANSI escape sequences.
func (m *model) appendEntry(entry timelineEntry) {
	atEnd := len(m.entries) == 0 || m.selectedEntry >= len(m.entries)-1
	m.entries = append(m.entries, entry)
	if len(m.entries) > 200 {
		evicted := len(m.entries) - 200
		m.entries = m.entries[evicted:]
		m.selectedEntry -= evicted
		if m.selectedEntry < 0 {
			m.selectedEntry = 0
		}
		m.sel.Clear()
	}
	if atEnd {
		m.selectedEntry = len(m.entries) - 1
		m.scrollSelectedIntoView()
	}
}

// logStyled and log are retained for low-level wrapping/selection tests.
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
		} else {
			m.lines = append(m.lines, renderContinuationLine(body))
		}
	}
	m.trim()
}

func (m *model) log(line string) {
	ts := sLogTimeCell.Render(time.Now().Format("15:04"))
	if line == "" {
		m.lines = append(m.lines, "")
	} else {
		m.lines = append(m.lines, lipgloss.JoinHorizontal(lipgloss.Top, ts, sLogGapCell.Render(""), line))
	}
	m.trim()
}

func (m *model) logIssueEvent(kind string, finding BrainFinding, extraDetail []string) {
	raw := strings.TrimSpace(strings.ReplaceAll(finding.Nudge, "`", ""))
	short := normalizeIssueText(raw)
	text := "issue: " + short
	if m.shouldLogEvent(kind, text) {
		entry := timelineEntry{
			ID:      strings.TrimSpace(finding.ID),
			Kind:    "issue",
			Time:    time.Now().Format("15:04"),
			Marker:  "!",
			Summary: short,
		}
		if claim := strings.TrimSpace(finding.Claim); claim != "" {
			entry.Detail = append(entry.Detail, "Codex said\n"+claim)
		}
		if verified := strings.TrimSpace(finding.Verified); verified != "" {
			entry.Detail = append(entry.Detail, "TruPal verified\n"+verified)
		} else if raw != "" && realityAddsValue(short, raw) {
			entry.Detail = append(entry.Detail, "TruPal verified\n"+realityText(raw))
		}
		if why := strings.TrimSpace(finding.Impact); why != "" {
			entry.Detail = append(entry.Detail, "Why it matters\n"+why)
		} else if why := strings.TrimSpace(finding.Why); why != "" {
			entry.Detail = append(entry.Detail, "Why it matters\n"+why)
		}
		entry.Detail = append(entry.Detail, extraDetail...)
		if tell := strings.TrimSpace(finding.Tell); tell != "" {
			entry.Detail = append(entry.Detail, "Tell Codex\n"+tell)
		}
		m.appendEntry(entry)
	}
}

func realityText(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "`", ""))
	prefixes := []string{
		"hey, ",
		"hey ",
		"you still ",
		"you’re still ",
		"you're still ",
		"you’ve ",
		"you've ",
	}
	lower := strings.ToLower(text)
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			text = strings.TrimSpace(text[len(prefix):])
			break
		}
	}
	if text == "" {
		return ""
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

func realityAddsValue(summary, raw string) bool {
	summary = strings.ToLower(strings.TrimSpace(summary))
	raw = strings.ToLower(strings.TrimSpace(realityText(raw)))
	if summary == "" || raw == "" {
		return false
	}
	return !strings.Contains(raw, summary) && !strings.Contains(summary, raw)
}

func (m *model) trim() {
	if len(m.lines) > 500 {
		evicted := len(m.lines) - 500
		m.lines = m.lines[evicted:]
		m.sel.ShiftLinesAfter(len(m.issuesPopupLines()), evicted)
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
	lh := m.logViewportH()
	lines, _, _ := m.renderedTimeline()
	if len(lines) <= lh {
		return 0
	}
	return len(lines) - lh
}

func (m model) logH() int {
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

func (m model) renderEntry(entry timelineEntry, width int, selected bool) []string {
	textW := logTextWidth(width)
	if textW < 18 {
		textW = 18
	}

	summaryLines := wrap(entry.Summary, textW)
	if len(summaryLines) == 0 {
		summaryLines = []string{""}
	}

	var lines []string
	for i, line := range summaryLines {
		body := line
		if entry.Kind == "issue" {
			body = sIssueText.Render(line)
		} else if entry.Kind == "resolved" {
			body = sDim.Render(line)
		}
		marker := entry.Marker
		if selected {
			body = sFocusBody.Render("› " + ansi.Strip(line))
		} else if entry.Kind == "log" && marker == "" {
			marker = "·"
		}
		if i == 0 {
			lines = append(lines, renderLogLine(entry.Time, marker, body))
		} else {
			lines = append(lines, renderContinuationLine(body))
		}
	}

	return lines
}

func (m model) renderedTimeline() ([]string, []int, []bool) {
	if len(m.entries) == 0 {
		owners := make([]int, len(m.lines))
		heads := make([]bool, len(m.lines))
		for i := range owners {
			owners[i] = -1
		}
		return append([]string{}, m.lines...), owners, heads
	}
	var lines []string
	var owners []int
	var heads []bool
	for i, entry := range m.entries {
		key := entry.ID
		if key == "" {
			key = fmt.Sprintf("entry-%d", i)
		}
		entryLines := m.renderEntry(entry, m.width, i == m.selectedEntry)
		for j, line := range entryLines {
			lines = append(lines, line)
			owners = append(owners, i)
			heads = append(heads, j == 0)
		}
		if detail := m.selectedInspectorLinesForEntry(i, key); len(detail) > 0 {
			for _, line := range detail {
				lines = append(lines, line)
				owners = append(owners, i)
				heads = append(heads, false)
			}
		}
		if i < len(m.entries)-1 {
			lines = append(lines, "")
			owners = append(owners, -1)
			heads = append(heads, false)
		}
	}
	return lines, owners, heads
}

func (m *model) moveSelection(delta int) {
	if len(m.entries) == 0 {
		m.scroll(-delta)
		return
	}
	m.selectedEntry += delta
	if m.selectedEntry < 0 {
		m.selectedEntry = 0
	}
	if m.selectedEntry >= len(m.entries) {
		m.selectedEntry = len(m.entries) - 1
	}
	m.scrollSelectedIntoView()
}

func (m *model) scrollSelectedIntoView() {
	if len(m.entries) == 0 {
		return
	}
	lines, owners, _ := m.renderedTimeline()
	if len(lines) == 0 || len(owners) == 0 {
		m.scrollOffset = 0
		return
	}
	startLine, endLine := -1, -1
	for i, owner := range owners {
		if owner != m.selectedEntry {
			continue
		}
		if startLine == -1 {
			startLine = i
		}
		endLine = i
	}
	if startLine == -1 {
		return
	}
	lh := m.logViewportH()
	start, end := m.visibleLogRange()
	if startLine < start {
		m.scrollOffset = len(lines) - (startLine + lh)
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
		return
	}
	if endLine >= end {
		m.scrollOffset = len(lines) - (endLine + 1)
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
	}
}

func (m *model) toggleEntryDetail(key string) {
	if m.detailOpen[key] {
		delete(m.detailOpen, key)
		m.scrollSelectedIntoView()
		return
	}
	for existing := range m.detailOpen {
		delete(m.detailOpen, existing)
	}
	m.detailOpen[key] = true
	m.scrollSelectedIntoView()
}

func (m *model) jumpToIssue(key string) {
	for i, entry := range m.entries {
		if entry.ID == key {
			m.selectedEntry = i
			m.scrollSelectedIntoView()
			return
		}
	}
}

func (m model) selectedInspectorLines() []string {
	if len(m.entries) == 0 {
		return nil
	}
	entry := m.entries[m.selectedEntry]
	key := entry.ID
	if key == "" {
		key = fmt.Sprintf("entry-%d", m.selectedEntry)
	}
	return m.selectedInspectorLinesForEntry(m.selectedEntry, key)
}

func (m model) selectedInspectorLinesForEntry(entryIdx int, key string) []string {
	return m.selectedInspectorAllLinesForEntry(entryIdx, key)
}

func (m model) selectedInspectorAllLines() []string {
	if len(m.entries) == 0 {
		return nil
	}
	entry := m.entries[m.selectedEntry]
	key := entry.ID
	if key == "" {
		key = fmt.Sprintf("entry-%d", m.selectedEntry)
	}
	return m.selectedInspectorAllLinesForEntry(m.selectedEntry, key)
}

func (m model) selectedInspectorAllLinesForEntry(entryIdx int, key string) []string {
	if len(m.entries) == 0 || entryIdx < 0 || entryIdx >= len(m.entries) {
		return nil
	}
	entry := m.entries[entryIdx]
	if !m.detailOpen[key] || len(entry.Detail) == 0 {
		return nil
	}

	width := m.width - 2
	if width < 20 {
		width = 20
	}
	lines := []string{}
	for _, block := range entry.Detail {
		parts := strings.SplitN(block, "\n", 2)
		if len(parts) != 2 {
			continue
		}
		label := parts[0]
		value := parts[1]
		style := sIssueWhy
		if label == "Seen" {
			style = sIssuePreview
		}
		lines = append(lines, renderDetailField(label, value, width-lipgloss.Width(logPrefix("", ""))-logGapWidth, style)...)
	}
	lines = append(lines, sSep.Render(strings.Repeat("─", m.width)))
	return lines
}

func (m *model) syncIssueCursorToSelection() {
	if len(m.issueItems) == 0 || len(m.entries) == 0 {
		return
	}
	selectedID := strings.TrimSpace(m.entries[m.selectedEntry].ID)
	if selectedID == "" {
		return
	}
	for i, issue := range m.issueItems {
		if issue.Key() == selectedID {
			m.issueCursor = i
			return
		}
	}
}

func (m model) bodyRect() selectionRect {
	return selectionRect{
		X: 0,
		Y: 3,
		W: m.width,
		H: m.logH(),
	}
}

func (m model) logRect() selectionRect {
	issueLines := len(m.issuePinnedLines())
	return selectionRect{
		X: 0,
		Y: 3 + issueLines,
		W: m.width,
		H: m.logViewportH(),
	}
}

func controlsHint() string {
	return "j/k scroll  pgup/pgdn  drag copy  ctrl+c quit"
}

func issueControlsHint() string {
	return "j/k move  enter jump  p hide"
}

func renderLabeledField(label, text string, width int, bodyStyle lipgloss.Style) []string {
	labelRendered := sFocusLabel.Render(label)
	labelWidth := lipgloss.Width(labelRendered)
	bodyWidth := width - labelWidth - 1
	if bodyWidth < 12 {
		bodyWidth = 12
	}
	lines := wrap(text, bodyWidth)
	if len(lines) == 0 {
		return []string{renderContinuationLineWithMarker(" ", labelRendered)}
	}

	out := []string{
		renderContinuationLineWithMarker(" ", labelRendered+" "+bodyStyle.Render(lines[0])),
	}
	padding := strings.Repeat(" ", labelWidth+1)
	for _, line := range lines[1:] {
		out = append(out, renderContinuationLineWithMarker(" ", padding+bodyStyle.Render(line)))
	}
	return out
}

func renderDetailField(label, text string, width int, bodyStyle lipgloss.Style) []string {
	switch label {
	case "Code":
		return renderCodeField("Code evidence", text, width)
	case "Claim":
		label = "Codex said"
	case "Reality":
		label = "TruPal verified"
	}
	boxWidth := width
	if boxWidth < 20 {
		boxWidth = 20
	}
	var parts []string
	for _, raw := range strings.Split(strings.TrimSpace(text), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		bullet := strings.HasPrefix(raw, "- ")
		if bullet {
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "- "))
			wrapped := wrap(raw, boxWidth-8)
			for i, line := range wrapped {
				if i == 0 {
					parts = append(parts, "• "+line)
				} else {
					parts = append(parts, "  "+line)
				}
			}
			continue
		}
		parts = append(parts, wrap(raw, boxWidth-6)...)
	}
	title := renderContinuationLineWithMarker(" ", sInspectorTitle.Render(label))
	block := sInspectorBox.Width(boxWidth).Render(bodyStyle.Render(strings.Join(parts, "\n")))
	lines := []string{title}
	for _, line := range strings.Split(block, "\n") {
		lines = append(lines, renderContinuationLineWithMarker(" ", line))
	}
	return lines
}

func renderCodeField(label, text string, width int) []string {
	bodyWidth := width
	if bodyWidth < 20 {
		bodyWidth = 20
	}

	var bodyLines []string
	for _, raw := range strings.Split(strings.TrimSpace(text), "\n") {
		raw = strings.TrimRight(raw, " ")
		if raw == "" {
			continue
		}
		parts := strings.SplitN(raw, ": ", 2)
		line := raw
		if len(parts) == 2 && isDigits(parts[0]) {
			line = sDetailCodeNo.Render(parts[0]) + " " + sDetailCode.Render(parts[1])
		} else {
			line = sDetailCode.Render(raw)
		}
		bodyLines = append(bodyLines, line)
	}
	title := renderContinuationLineWithMarker(" ", sInspectorTitle.Render(label))
	if len(bodyLines) == 0 {
		return []string{title}
	}
	block := sInspectorCode.Width(bodyWidth).Render(strings.Join(bodyLines, "\n"))
	lines := []string{title}
	for _, line := range strings.Split(block, "\n") {
		lines = append(lines, renderContinuationLineWithMarker(" ", line))
	}
	return lines
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (m model) issuesPopupLines() []string {
	if len(m.issueItems) == 0 || !m.popupVisible() {
		return nil
	}
	width := m.width - lipgloss.Width(logPrefix("", "")) - logGapWidth
	if width < 20 {
		width = 20
	}
	current := m.issueItems[m.issueCursor%len(m.issueItems)]
	lines := []string{renderLogLine("", sIssueBullet.Render("!"), sIssueTitle.Render(fmt.Sprintf("open issues %d/%d", m.issueCursor+1, len(m.issueItems))))}
	for i, issue := range m.issueItems {
		marker := fmt.Sprintf("%d.", i+1)
		style := sIssuePreview
		if i == m.issueCursor {
			marker = "›"
			style = sIssueText.Bold(true)
		}
		lines = append(lines, renderContinuationLineWithMarker(marker, style.Render(wrapSingleLine(normalizeIssueText(issue.Nudge), width-4))))
	}
	if strings.TrimSpace(current.Why) != "" {
		lines = append(lines, renderLabeledField("Why", strings.TrimSpace(current.Why), width, sIssueWhy)...)
	}
	lines = append(lines, sSep.Render(strings.Repeat("─", m.width)))
	return lines
}

func (m model) issuePinnedLines() []string {
	lines := m.issuesPopupLines()
	if len(lines) == 0 {
		return nil
	}
	max := m.logH() - m.minLogViewportH()
	if max < 1 {
		return nil
	}
	if len(lines) <= max {
		return lines
	}
	truncated := append([]string{}, lines[:max]...)
	truncated[max-1] = sSep.Render(strings.Repeat("─", m.width))
	return truncated
}

func (m model) minLogViewportH() int {
	switch {
	case m.logH() >= 16:
		return 6
	case m.logH() >= 12:
		return 4
	case m.logH() >= 8:
		return 3
	default:
		return 1
	}
}

func (m model) logViewportH() int {
	h := m.logH() - len(m.issuePinnedLines())
	min := m.minLogViewportH()
	if h < min {
		return min
	}
	return h
}

func (m model) contentLines() []string {
	lines, _, _ := m.renderedTimeline()
	content := append([]string{}, m.issuesPopupLines()...)
	content = append(content, lines...)
	return content
}

func (m model) visibleLogRange() (start, end int) {
	lines, _, _ := m.renderedTimeline()
	total := len(lines)
	end = total - m.scrollOffset
	if end > total {
		end = total
	}
	start = end - m.logViewportH()
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	return start, end
}

func (m *model) autoScrollSelection(y int) {
	rect := m.bodyRect()
	if rect.H <= 0 {
		return
	}

	switch {
	case y < rect.Y:
		m.scroll(rect.Y - y)
	case y >= rect.Y+rect.H:
		m.scroll(-(y - (rect.Y + rect.H) + 1))
	}
}

func (m model) selectionPointAt(x, y int, clamp bool) (selectionPoint, bool) {
	rect := m.bodyRect()
	if rect.W <= 0 || rect.H <= 0 {
		return selectionPoint{}, false
	}
	if !rect.Contains(x, y) {
		if !clamp {
			return selectionPoint{}, false
		}
		x, y = rect.Clamp(x, y)
	}

	content := m.contentLines()
	if len(content) == 0 {
		return selectionPoint{}, false
	}

	popupCount := len(m.issuesPopupLines())
	start, end := m.visibleLogRange()
	visibleCount := popupCount + (end - start)
	if visibleCount <= 0 {
		return selectionPoint{}, false
	}

	row := y - rect.Y
	if row < 0 {
		if !clamp {
			return selectionPoint{}, false
		}
		row = 0
	}
	if row >= visibleCount {
		if !clamp {
			return selectionPoint{}, false
		}
		row = visibleCount - 1
	}
	lineIdx := row
	if row >= popupCount {
		lineIdx = popupCount + start + (row - popupCount)
	}

	relX := x - rect.X
	if relX < 0 {
		relX = 0
	}
	expanded := selectionDisplayLine(content[lineIdx], selectionTabWidth)
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
	h1 := renderHeaderTitle(m.project, m.elapsed, w)

	// ── Header line 2: status indicators ──
	indicators := []string{}
	agentLabel := m.agentLabel
	if agentLabel == "" {
		agentLabel = "agent"
	}
	switch m.ccStatus {
	case "active", "thinking":
		indicators = append(indicators, "● "+agentLabel)
	default:
		indicators = append(indicators, "○ "+agentLabel)
	}
	if m.buildState != "" {
		indicators = append(indicators, m.buildState)
	}
	if m.findings > 0 {
		indicators = append(indicators, fmt.Sprintf("⚠ %d", m.findings))
	}
	if m.resolved > 0 {
		indicators = append(indicators, fmt.Sprintf("✓ %d", m.resolved))
	}
	indicators = append(indicators, m.brainIndicator())

	statsMaxWidth := w - 1
	if used := joinWidth(indicators, " "); used > 0 {
		statsMaxWidth -= used + 2
	}
	statsIndicator := chooseBrainStatsIndicator(m.brain.stats, statsMaxWidth)
	indicators = append(indicators, statsIndicator)
	if w < 44 {
		indicators = []string{statsIndicator}
	}

	h2 := renderHeaderIndicators(indicators, w)

	sep := sSep.Render(strings.Repeat("─", w))

	// ── Log area ──
	issueLines := m.issuePinnedLines()
	lh := m.logViewportH()
	start, end := m.visibleLogRange()
	content, owners, heads := m.renderedTimeline()
	pinnedCount := len(issueLines)

	bodyLines := make([]string, 0, pinnedCount+lh)
	for i, line := range issueLines {
		line = selectionDisplayLine(line, selectionTabWidth)
		if m.sel.IsLineSelected(i) {
			startCol, endCol := m.sel.GetLineSelectionCols(i)
			line = InjectCharacterRangeBackground(line, startCol, endCol)
		}
		bodyLines = append(bodyLines, line)
	}

	visible := make([]string, 0, lh)
	if start < end {
		for i, line := range content[start:end] {
			absIdx := pinnedCount + start + i
			line = selectionDisplayLine(line, selectionTabWidth)
			if m.sel.IsLineSelected(absIdx) {
				startCol, endCol := m.sel.GetLineSelectionCols(absIdx)
				line = InjectCharacterRangeBackground(line, startCol, endCol)
			}
			if !m.sel.HasSelection() && start+i < len(owners) && owners[start+i] == m.selectedEntry && start+i < len(heads) && heads[start+i] {
				line = InjectCharacterRangeBackground(line, 0, -1)
			}
			visible = append(visible, line)
		}
	}
	for len(visible) < lh {
		visible = append(visible, "")
	}
	bodyLines = append(bodyLines, visible...)

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
	} else {
		used := joinWidth(footerParts, "  ")
		remaining := w - used - 1
		if len(footerParts) > 0 {
			remaining -= 2
		}
		if remaining > 0 {
			hint := controlsHint()
			if len(m.issueItems) > 0 {
				if m.popupVisible() {
					hint = issueControlsHint()
				} else {
					hint = "o focus  j/k scroll  pgup/pgdn"
				}
			}
			footerParts = append(footerParts, truncateWidth(hint, remaining))
		}
	}
	footer := sFooterLine.Render(strings.Join(footerParts, "  "))

	parts := []string{h1, h2, sep, strings.Join(bodyLines, "\n"), sep, footer}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func normalizeEventKey(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(text)), " "))
}

func compactEventText(text string, max int) string {
	text = strings.TrimSpace(strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " "))
	text = strings.ReplaceAll(text, "`", "")
	if max > 0 && len(text) > max {
		return text[:max-3] + "..."
	}
	return text
}

func (m *model) shouldLogEvent(kind, text string) bool {
	key := kind + ":" + normalizeEventKey(text)
	for _, existing := range m.recentEvents {
		if existing == key {
			return false
		}
	}
	m.recentEvents = append(m.recentEvents, key)
	if len(m.recentEvents) > 40 {
		m.recentEvents = m.recentEvents[len(m.recentEvents)-40:]
	}
	return true
}

// --- Helpers ---

func filesLine(files, newFiles []string) string {
	parts := []string{}
	if len(files) > 0 {
		parts = append(parts, fmt.Sprintf("%d mod: %s", len(files), joinDisplayPaths(files, 3)))
	}
	if len(newFiles) > 0 {
		parts = append(parts, fmt.Sprintf("%d new: %s", len(newFiles), joinDisplayPaths(newFiles, 2)))
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

func wrapSingleLine(text string, width int) string {
	lines := wrap(text, width)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func joinDisplayPaths(files []string, max int) string {
	duplicateBase := make(map[string]bool)
	baseCounts := make(map[string]int)
	for _, file := range files {
		base := filepath.Base(file)
		baseCounts[base]++
		if baseCounts[base] > 1 {
			duplicateBase[base] = true
		}
	}

	var names []string
	for i, f := range files {
		if i >= max {
			names = append(names, fmt.Sprintf("+%d", len(files)-max))
			break
		}
		base := filepath.Base(f)
		if duplicateBase[base] {
			names = append(names, shortDisplayPath(f))
			continue
		}
		names = append(names, base)
	}
	return strings.Join(names, " ")
}

func shortDisplayPath(path string) string {
	path = filepath.Clean(path)
	base := filepath.Base(path)
	dir := filepath.Base(filepath.Dir(path))
	if dir == "." || dir == string(filepath.Separator) || dir == "" {
		return base
	}
	return filepath.ToSlash(filepath.Join(dir, base))
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
		return sNudgeMarker.Render("⚠"), sNudgeText
	}
	return sNudgeMarker.Render("▸"), sNudgeText
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

func truncateRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func renderHeaderTitle(project, elapsed string, width int) string {
	contentWidth := width - 1
	if contentWidth <= 0 {
		return ""
	}

	brand := sBrandChip.Render("TRUPAL")
	projectText := sTitle.Render(truncateWidth(project, max(10, contentWidth/2)))
	elapsedChip := ""
	if strings.TrimSpace(elapsed) != "" {
		elapsedChip = sMetaChip.Render(elapsed)
	}

	parts := []string{brand}
	if projectText != "" {
		parts = append(parts, projectText)
	}
	if elapsedChip != "" {
		parts = append(parts, elapsedChip)
	}

	line := ""
	for _, part := range parts {
		candidate := strings.TrimSpace(line + " " + part)
		if lipgloss.Width(candidate) > contentWidth {
			break
		}
		line = candidate
	}
	return " " + line
}

func renderHeaderIndicators(indicators []string, width int) string {
	contentWidth := width - 1
	if contentWidth <= 0 {
		return ""
	}

	if len(indicators) == 0 {
		return ""
	}
	styled := make([]string, 0, len(indicators))
	for _, indicator := range indicators {
		styled = append(styled, styleHeaderIndicator(indicator))
	}

	var visible []string
	for _, indicator := range styled {
		candidate := append(append([]string{}, visible...), indicator)
		if joinWidth(candidate, " ") <= contentWidth {
			visible = candidate
			continue
		}

		if len(visible) == 0 {
			return " " + truncateWidth(indicator, contentWidth)
		}

		withEllipsis := append(append([]string{}, visible...), sDim.Render("…"))
		for len(withEllipsis) > 0 && joinWidth(withEllipsis, " ") > contentWidth {
			withEllipsis = withEllipsis[:len(withEllipsis)-1]
		}
		if len(withEllipsis) == 0 {
			return " " + truncateWidth(indicator, contentWidth)
		}
		return " " + strings.Join(withEllipsis, " ")
	}

	// Prefer keeping the rightmost (usually brain stats) visible on narrow widths.
	last := styled[len(styled)-1]
	if len(visible) > 0 && !containsStyled(visible, last) && lipgloss.Width(last) <= contentWidth {
		withLast := append([]string{}, visible...)
		withLast = append(withLast, last)
		for len(withLast) > 1 && joinWidth(withLast, " ") > contentWidth {
			withLast = withLast[1:]
		}
		if joinWidth(withLast, " ") <= contentWidth {
			visible = withLast
		}
	}

	return " " + strings.Join(visible, " ")
}

func containsStyled(parts []string, target string) bool {
	for _, part := range parts {
		if part == target {
			return true
		}
	}
	return false
}

func styleHeaderIndicator(indicator string) string {
	plain := ansi.Strip(indicator)
	switch {
	case strings.Contains(plain, "build") && strings.Contains(plain, "✗"):
		return sStatusChipEr.Render(plain)
	case strings.Contains(plain, "build"):
		return sStatusChipOk.Render(plain)
	case strings.Contains(plain, "analyzing") || strings.Contains(plain, "ago") || strings.Contains(plain, "starting") || strings.Contains(plain, "brain idle"):
		return sStatusChipHi.Render(plain)
	case strings.Contains(plain, "⚠"):
		return sStatusChipWr.Render(plain)
	case strings.Contains(plain, "✓"):
		return sStatusChipOk.Render(plain)
	case strings.Contains(plain, "agent") || strings.Contains(plain, "codex") || strings.Contains(plain, "claude"):
		return sStatusChip.Render(plain)
	default:
		return sMetaChip.Render(plain)
	}
}

func joinWidth(parts []string, sep string) int {
	if len(parts) == 0 {
		return 0
	}
	return lipgloss.Width(strings.Join(parts, sep))
}
