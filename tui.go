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

	sBrandChip       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	sMetaChip        = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236")).Padding(0, 1)
	sHeaderLabelText = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	sHeaderValueText = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	sFooterLine      = lipgloss.NewStyle().Faint(true).PaddingLeft(1)

	sLogTimeCell   = lipgloss.NewStyle().Faint(true).Width(logTimeWidth).Align(lipgloss.Right)
	sLogGapCell    = lipgloss.NewStyle().Width(logGapWidth)
	sLogMarkerCell = lipgloss.NewStyle().Width(logMarkerWidth).Align(lipgloss.Center)
	sLogGutterCell = lipgloss.NewStyle().Faint(true).Width(logMarkerWidth).Align(lipgloss.Center)

	sIssueTitle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24"))
	sIssueBullet    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	sIssueText      = lipgloss.NewStyle().Foreground(lipgloss.Color("230"))
	sIssueWhy       = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	sIssuePreview   = lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
	sFocusLabel     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	sSelectedRow    = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Bold(true)
	sTimelineInfo   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	sTimelineWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	sTimelineOk     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	sTimelineNote   = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	sTimelineSel    = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	sDetailCode     = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236"))
	sDetailCodeNo   = lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	sDockLabel      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	sDockValue      = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	sDockMuted      = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	sDockAccent     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	sDockWarn       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	sInspectorTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	sInspectorBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("239")).Padding(0, 1).Foreground(lipgloss.Color("252"))
	sInspectorCode  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Background(lipgloss.Color("235")).Padding(0, 1).Foreground(lipgloss.Color("252"))
)

const (
	headerLineCount = 3
	logTimeWidth    = 5
	logGapWidth     = 2
	logMarkerWidth  = 2
)

// --- Messages ---

type statusMsg struct {
	agentLabel    string
	ccStatus      string
	sessionModel  string
	brainIdentity string
	agentStats    AgentUsageStats
	repoRoot      string
	agentPaneID   string
	buildOK       *bool
	buildErrs     int
	buildTrend    string
	files         []string
	elapsed       string
	project       string
	findings      int
	resolved      int
	issues        []CurrentIssue
}
type nudgeMsg struct {
	finding BrainFinding
	detail  []string
}
type resolvedMsg struct {
	finding BrainFinding
	detail  []string
}
type trajectoryMsg struct{ message string }
type patternMsg struct{ finding PatternFinding }
type infoMsg struct{ message string }
type steeringSentMsg struct {
	findingID string
	message   string
	source    string
	at        time.Time
	logErr    error
}
type steeringSendFailedMsg struct {
	findingID string
	err       error
}
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
	recentEvents  []string
	lines         []string // legacy raw line buffer used by low-level log/selection tests

	// Layout
	width  int
	height int

	// Header state
	project       string
	elapsed       string
	agentLabel    string
	ccStatus      string
	buildState    string
	findings      int // active count
	resolved      int
	brain         brainIndicatorState
	agentUsage    AgentUsageStats
	sessionModel  string
	brainIdentity string
	repoRoot      string
	agentPaneID   string

	// Footer state
	fileLine           string // current files summary
	issueItems         []CurrentIssue
	issueCursor        int
	issuesPopupVisible bool
	issuePanelVisible  bool // compatibility alias for existing tests while popup work lands
	detailOpen         map[string]bool
	toastMsg           string // transient message (e.g. "copied!")
	toastExpiry        time.Time
	steerModeAuto      bool
	sentNudges         map[string]SteeringSendState
	lastSteerAt        time.Time
	steerInFlight      bool
	activeSteerKey     string
	activeSteerMessage string

	// Selection
	sel *Selection

	quitting    bool
	quitPending bool
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
		sentNudges: make(map[string]SteeringSendState),
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

func (m *model) setPopupVisible(visible bool) {
	m.issuesPopupVisible = visible
	m.issuePanelVisible = visible
}

func (m model) watchStatus() string {
	switch m.ccStatus {
	case "active", "thinking":
		return "active"
	case "starting":
		return "starting"
	case "":
		return "idle"
	default:
		return m.ccStatus
	}
}

func (m model) selectionAreaAt(x, y int) (selectionRect, bool) {
	if m.footerRect().Contains(x, y) {
		return m.footerRect(), true
	}
	if m.bodyRect().Contains(x, y) {
		return m.bodyRect(), true
	}
	return selectionRect{}, false
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
			if m.quitPending {
				m.quitting = true
				return m, tea.Quit
			}
			m.quitPending = true
			return m, nil
		case "up", "k":
			m.quitPending = false
			if m.issuesPopupVisible {
				if len(m.issueItems) > 0 {
					m.issueCursor = (m.issueCursor - 1 + len(m.issueItems)) % len(m.issueItems)
				}
			} else {
				m.moveSelection(-1)
			}
		case "down", "j":
			m.quitPending = false
			if m.issuesPopupVisible {
				if len(m.issueItems) > 0 {
					m.issueCursor = (m.issueCursor + 1) % len(m.issueItems)
				}
			} else {
				m.moveSelection(1)
			}
		case "pgup":
			m.quitPending = false
			m.scroll(10)
		case "pgdown":
			m.quitPending = false
			m.scroll(-10)
		case "g", "home":
			m.quitPending = false
			if len(m.entries) > 0 {
				m.selectedEntry = 0
				m.scrollSelectedIntoView()
			} else {
				m.scrollOffset = m.maxScroll()
			}
		case "G", "end":
			m.quitPending = false
			if len(m.entries) > 0 {
				m.selectedEntry = len(m.entries) - 1
				m.scrollSelectedIntoView()
			} else {
				m.scrollOffset = 0
			}
		case "o":
			m.quitPending = false
			if m.popupVisible() {
				m.setPopupVisible(false)
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
			m.quitPending = false
			if len(m.issueItems) > 0 {
				for key := range m.detailOpen {
					m.detailOpen[key] = false
				}
				next := !m.popupVisible()
				m.setPopupVisible(next)
				if next {
					m.syncIssueCursorToSelection()
				}
			}
		case "s":
			m.quitPending = false
			if issue, ok := m.currentSteeringIssue(); ok && m.canSendNudge(issue, true) {
				m.steerInFlight = true
				return m, m.sendNudgeCmd(issue, "manual")
			}
		case "a":
			m.quitPending = false
			m.steerModeAuto = !m.steerModeAuto
			if m.steerModeAuto {
				m.toastMsg = "auto steer on"
			} else {
				m.toastMsg = "auto steer off"
			}
			m.toastExpiry = time.Now().Add(3 * time.Second)
			if m.steerModeAuto {
				if issue, ok := m.autoSteeringIssue(); ok && m.canSendNudge(issue, false) {
					m.steerInFlight = true
					return m, m.sendNudgeCmd(issue, "auto")
				}
			}
		case "]":
			m.quitPending = false
			if len(m.issueItems) > 0 && m.popupVisible() {
				m.issueCursor = (m.issueCursor + 1) % len(m.issueItems)
			}
		case "[":
			m.quitPending = false
			if len(m.issueItems) > 0 && m.popupVisible() {
				m.issueCursor = (m.issueCursor - 1 + len(m.issueItems)) % len(m.issueItems)
			}
		case "esc":
			if m.quitPending {
				m.quitPending = false
				return m, nil
			}
			m.setPopupVisible(false)
		case "enter", " ":
			m.quitPending = false
			if m.popupVisible() && len(m.issueItems) > 0 {
				m.jumpToIssue(m.issueItems[m.issueCursor].Key())
				m.setPopupVisible(false)
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
			view, _ := m.selectionAreaAt(msg.X, msg.Y)
			m.sel.PrepareDrag(point.Line, point.Col, view)
		case msg.Action == tea.MouseActionMotion && (msg.Button == tea.MouseButtonLeft || (msg.Button == tea.MouseButtonNone && m.sel.Anchor.Valid())):
			if m.sel.Anchor.Valid() && m.sel.View == m.bodyRect() {
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
		if msg.sessionModel != "" {
			m.sessionModel = msg.sessionModel
		}
		if msg.brainIdentity != "" {
			m.brainIdentity = msg.brainIdentity
		}
		if msg.repoRoot != "" {
			m.repoRoot = msg.repoRoot
		}
		m.agentPaneID = msg.agentPaneID
		m.agentUsage = msg.agentStats
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
		m.fileLine = filesLine(msg.files)
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
		m.reconcileActiveSteeringIssue()
		m.findings = msg.findings
		m.resolved = msg.resolved
		if m.steerModeAuto {
			if issue, ok := m.autoSteeringIssue(); ok && m.canSendNudge(issue, false) {
				m.steerInFlight = true
				return m, m.sendNudgeCmd(issue, "auto")
			}
		}

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
			blocks := msg.detail
			if len(blocks) == 0 {
				blocks = []string{"Updated\nResolved in the latest review. TruPal no longer reproduced this contradiction after the latest changes."}
			}
			for _, block := range blocks {
				parts := strings.SplitN(block, "\n", 2)
				if len(parts) != 2 {
					continue
				}
				m.entries[i].Detail = upsertDetailBlockPreferred(m.entries[i].Detail, parts[0], parts[1])
			}
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
					Detail: func() []string {
						if len(msg.detail) > 0 {
							return msg.detail
						}
						return []string{"Updated\nResolved in the latest review. TruPal no longer reproduced this contradiction after the latest changes."}
					}(),
				})
			}
		}

	case trajectoryMsg:
		m.appendEntry(timelineEntry{
			Kind:    "info",
			Time:    time.Now().Format("15:04"),
			Marker:  "i",
			Summary: msg.message,
		})

	case infoMsg:
		m.appendEntry(timelineEntry{
			Kind:    "info",
			Time:    time.Now().Format("15:04"),
			Marker:  "i",
			Summary: msg.message,
		})

	case steeringSentMsg:
		m.steerInFlight = false
		m.sentNudges[msg.findingID] = SteeringSendState{
			Message: msg.message,
			Source:  msg.source,
			At:      msg.at,
		}
		m.activeSteerKey = msg.findingID
		m.activeSteerMessage = strings.TrimSpace(msg.message)
		m.lastSteerAt = msg.at
		if msg.logErr != nil {
			m.toastMsg = "⚠ nudge sent, log failed"
			Debugf("[steer] log failed for %s: %v", msg.findingID, msg.logErr)
		} else {
			m.toastMsg = "✓ nudge sent"
		}
		m.toastExpiry = time.Now().Add(3 * time.Second)

	case steeringSendFailedMsg:
		m.steerInFlight = false
		m.toastMsg = "⚠ send failed"
		m.toastExpiry = time.Now().Add(3 * time.Second)
		Debugf("[steer] send failed for %s: %v", msg.findingID, msg.err)

	case patternMsg:
		m.logIssueEvent("pattern", BrainFinding{
			ID:       msg.finding.Key,
			Severity: msg.finding.Level,
			Nudge:    steerablePatternNudge(msg.finding),
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

func (m model) brainUsageCandidates() []string {
	stats := m.brain.stats
	if stats.PromptTokens() == 0 && stats.TotalOutputTokens == 0 {
		return []string{sDockMuted.Render("no brain usage yet")}
	}

	metrics := []dockMetric{
		{Label: "in", Value: formatTokenCount(stats.UncachedPromptTokens()), Accent: false},
		{Label: "cache", Value: formatTokenCount(stats.TotalCacheReadTokens), Accent: false},
		{Label: "hit", Value: fmt.Sprintf("%d%%", stats.CacheHitRate()), Accent: true},
		{Label: "out", Value: formatTokenCount(stats.TotalOutputTokens), Accent: false},
	}
	if stats.LastDuration > 0 || strings.TrimSpace(stats.LastEffort) != "" {
		metrics = append(metrics, dockMetric{Label: "turn", Value: fmt.Sprintf("%s/%s", roundDuration(stats.LastDuration), defaultString(stats.LastEffort, "?"))})
	}
	if stats.CostKnown && stats.TotalCostUSD > 0 {
		metrics = append(metrics, dockMetric{Label: "cost", Value: formatCostUSD(stats.TotalCostUSD)})
	}
	return dockMetricCandidates(metrics)
}

func (m model) agentUsageCandidates() []string {
	stats := m.agentUsage
	if !stats.Available() {
		return []string{sDockMuted.Render("usage unavailable")}
	}

	metrics := []dockMetric{
		{Label: "in", Value: formatTokenCount(stats.UncachedPromptTokens())},
		{Label: "cache", Value: formatTokenCount(stats.TotalCachedTokens)},
		{Label: "hit", Value: fmt.Sprintf("%d%%", stats.CacheHitRate()), Accent: true},
		{Label: "out", Value: formatTokenCount(stats.TotalOutputTokens)},
	}
	return dockMetricCandidates(metrics)
}

type dockMetric struct {
	Label  string
	Value  string
	Accent bool
}

func renderDockMetric(metric dockMetric) string {
	if strings.TrimSpace(metric.Value) == "" {
		return ""
	}
	valueStyle := sDockValue
	if metric.Accent {
		valueStyle = sDockAccent
	}
	return sDockMuted.Render(metric.Label) + " " + valueStyle.Render(metric.Value)
}

func dockMetricCandidates(metrics []dockMetric) []string {
	if len(metrics) == 0 {
		return []string{""}
	}
	var candidates []string
	for n := len(metrics); n >= 1; n-- {
		candidates = append(candidates, joinDockColumns(metrics[:n]))
	}
	return candidates
}

var dockColumnWidths = []int{9, 11, 7, 7, 11}

const dockLabelWidth = 8

func joinDockColumns(metrics []dockMetric) string {
	if len(metrics) == 0 {
		return ""
	}
	padded := make([]string, 0, len(metrics))
	for i, metric := range metrics {
		part := renderDockMetric(metric)
		if part == "" {
			continue
		}
		if i == len(metrics)-1 {
			padded = append(padded, part)
			continue
		}
		target := 14
		if i < len(dockColumnWidths) {
			target = dockColumnWidths[i]
		}
		width := lipgloss.Width(part)
		if width < target {
			part += strings.Repeat(" ", target-width)
		}
		padded = append(padded, part)
	}
	return strings.Join(padded, "  ")
}

func chooseUsageCandidate(candidates []string, maxWidth int) string {
	if len(candidates) == 0 {
		return ""
	}
	if maxWidth <= 0 {
		return candidates[len(candidates)-1]
	}
	for _, candidate := range candidates {
		if lipgloss.Width(candidate) <= maxWidth {
			return candidate
		}
	}
	return truncateWidth(candidates[len(candidates)-1], maxWidth)
}

func (m model) renderUsageDockLine(label string, candidates []string, width int) string {
	labelText := sDockLabel.Render(label)
	labelWidth := lipgloss.Width(label)
	if labelWidth < dockLabelWidth {
		labelText += strings.Repeat(" ", dockLabelWidth-labelWidth)
		labelWidth = dockLabelWidth
	}
	summary := chooseUsageCandidate(candidates, width-lipgloss.Width(labelText)-3)
	if strings.TrimSpace(summary) == "" {
		summary = sDockMuted.Render("no data")
	}
	content := labelText + "  " + summary
	if lipgloss.Width(content) <= width-1 {
		return content
	}
	maxSummary := width - labelWidth - 3
	if maxSummary < 10 {
		maxSummary = 10
	}
	return labelText + "  " + truncateWidth(summary, maxSummary)
}

func (m model) footerDockLines() []string {
	return m.footerDockLinesWithState("", m.width)
}

func (m model) footerDockLinesWithState(controls string, width int) []string {
	footerParts := []string{}
	if controls != "" {
		footerParts = append(footerParts, controls)
	} else {
		footerParts = append(footerParts, "")
	}

	agentLabel := sessionProviderDisplayName(m.agentUsage.Provider)
	if strings.TrimSpace(agentLabel) == "" {
		agentLabel = defaultString(m.agentLabel, "agent")
		if agentLabel != "" {
			agentLabel = strings.ToUpper(agentLabel[:1]) + agentLabel[1:]
		}
	}

	return []string{
		sFooterLine.Render(footerParts[0]),
		sFooterLine.Render(m.renderUsageDockLine("TruPal", m.brainUsageCandidates(), width)),
		sFooterLine.Render(m.renderUsageDockLine(agentLabel, m.agentUsageCandidates(), width)),
	}
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
		entry.Detail = detailBlocksForFinding(short, raw, finding, extraDetail)
		m.appendEntry(entry)
	}
}

func detailBlocksForFinding(summary, raw string, finding BrainFinding, extraDetail []string) []string {
	var detail []string
	if claim := strings.TrimSpace(finding.Claim); claim != "" {
		detail = upsertDetailBlockPreferred(detail, "Codex said", claim)
	}
	if verified := strings.TrimSpace(finding.Verified); verified != "" {
		detail = upsertDetailBlockPreferred(detail, "TruPal verified", verified)
	} else if raw != "" && realityAddsValue(summary, raw) {
		detail = upsertDetailBlockPreferred(detail, "TruPal verified", realityText(raw))
	}
	if why := strings.TrimSpace(finding.Impact); why != "" {
		detail = upsertDetailBlockPreferred(detail, "Why it matters", why)
	} else if why := strings.TrimSpace(finding.Why); why != "" {
		detail = upsertDetailBlockPreferred(detail, "Why it matters", why)
	}
	for _, block := range extraDetail {
		parts := strings.SplitN(block, "\n", 2)
		if len(parts) != 2 {
			detail = append(detail, block)
			continue
		}
		detail = upsertDetailBlockPreferred(detail, parts[0], parts[1])
	}
	return detail
}

func normalizeForCompare(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "`", "")
	text = strings.Join(strings.Fields(text), " ")
	return strings.Trim(text, ".! ")
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

func upsertDetailBlock(detail []string, label, value string) []string {
	prefix := label + "\n"
	for i, block := range detail {
		if strings.HasPrefix(block, prefix) {
			detail[i] = prefix + value
			return detail
		}
	}
	return append(detail, prefix+value)
}

func upsertDetailBlockPreferred(detail []string, label, value string) []string {
	prefix := label + "\n"
	for i, block := range detail {
		if !strings.HasPrefix(block, prefix) {
			continue
		}
		existing := strings.TrimPrefix(block, prefix)
		if len(strings.TrimSpace(value)) > len(strings.TrimSpace(existing)) {
			detail[i] = prefix + value
		}
		return detail
	}
	return append(detail, prefix+value)
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
	h := m.height - (headerLineCount + 2 + len(m.footerDockLines()))
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

	summary := entry.Summary
	if entry.Kind == "issue" {
		summary = m.issueSummaryByKey(entry.ID, entry.Summary)
	}
	summaryLines := wrap(summary, textW)
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
		} else if entry.Kind == "info" {
			body = sTimelineInfo.Render(line)
		}
		marker := styledTimelineMarker(entry.Kind, entry.Marker)
		if selected {
			body = sSelectedRow.Render(ansi.Strip(line))
			marker = sTimelineSel.Render("›")
		}
		if i == 0 {
			lines = append(lines, renderLogLine(entry.Time, marker, body))
		} else {
			lines = append(lines, renderContinuationLine(body))
		}
	}

	return lines
}

func styledTimelineMarker(kind, marker string) string {
	switch kind {
	case "issue":
		return sTimelineWarn.Render("●")
	case "resolved":
		return sTimelineOk.Render("✓")
	case "note":
		return sTimelineNote.Render("→")
	case "info":
		return sTimelineInfo.Render("i")
	default:
		if marker == "" {
			return sTimelineInfo.Render("○")
		}
		return sTimelineInfo.Render(marker)
	}
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
	hadOpen := len(m.detailOpen) > 0
	m.selectedEntry += delta
	if m.selectedEntry < 0 {
		m.selectedEntry = 0
	}
	if m.selectedEntry >= len(m.entries) {
		m.selectedEntry = len(m.entries) - 1
	}
	if hadOpen {
		m.openSelectedEntryDetail()
	}
	m.scrollSelectedIntoView()
}

func (m *model) scrollSelectedIntoView() {
	if len(m.entries) == 0 {
		if m.scrollOffset > m.maxScroll() {
			m.scrollOffset = m.maxScroll()
		}
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
	blockHeight := endLine - startLine + 1
	if blockHeight >= lh {
		m.scrollOffset = len(lines) - (startLine + lh)
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
		return
	}
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
	if m.scrollOffset > m.maxScroll() {
		m.scrollOffset = m.maxScroll()
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

func (m *model) openSelectedEntryDetail() {
	for existing := range m.detailOpen {
		delete(m.detailOpen, existing)
	}
	if len(m.entries) == 0 {
		return
	}
	entry := m.entries[m.selectedEntry]
	if len(entry.Detail) == 0 {
		return
	}
	key := entry.ID
	if key == "" {
		key = fmt.Sprintf("entry-%d", m.selectedEntry)
	}
	m.detailOpen[key] = true
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
	if sent, ok := m.sentNudges[entry.ID]; ok {
		status := fmt.Sprintf("%s at %s", sent.Source, sent.At.Format("15:04"))
		lines = append(lines, renderDetailField("Sent", status, width-lipgloss.Width(logPrefix("", ""))-logGapWidth, sIssuePreview)...)
	}
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

func (m model) currentSteeringIssue() (CurrentIssue, bool) {
	if len(m.issueItems) == 0 {
		return CurrentIssue{}, false
	}
	if m.popupVisible() {
		return m.issueItems[m.issueCursor%len(m.issueItems)], true
	}
	if len(m.entries) == 0 {
		return CurrentIssue{}, false
	}
	selectedID := strings.TrimSpace(m.entries[m.selectedEntry].ID)
	for _, issue := range m.issueItems {
		if issue.Key() == selectedID {
			return issue, true
		}
	}
	return CurrentIssue{}, false
}

func (m model) autoSteeringIssue() (CurrentIssue, bool) {
	if strings.TrimSpace(m.activeSteerKey) != "" {
		return CurrentIssue{}, false
	}
	for _, issue := range m.issueItems {
		if m.canSendNudge(issue, false) {
			return issue, true
		}
	}
	return CurrentIssue{}, false
}

func (m model) canSendNudge(issue CurrentIssue, manual bool) bool {
	if strings.TrimSpace(issue.Message()) == "" || strings.TrimSpace(m.agentPaneID) == "" {
		return false
	}
	if m.steerInFlight {
		return false
	}
	if !manual && !m.lastSteerAt.IsZero() && time.Since(m.lastSteerAt) < 30*time.Second {
		return false
	}
	record, ok := m.sentNudges[issue.Key()]
	if !ok {
		return true
	}
	if strings.TrimSpace(record.Message) != strings.TrimSpace(issue.Message()) {
		return true
	}
	if manual {
		return time.Since(record.At) > 5*time.Second
	}
	return time.Since(record.At) > 30*time.Second
}

func (m model) sendNudgeCmd(issue CurrentIssue, source string) tea.Cmd {
	repoRoot := m.repoRoot
	paneID := m.agentPaneID
	message := issue.Message()
	findingID := issue.Key()
	return func() tea.Msg {
		err := sendSteeringMessage(paneID, message)
		if err != nil {
			return steeringSendFailedMsg{findingID: findingID, err: err}
		}
		logErr := recordSteeringEvent(repoRoot, SteeringEvent{
			Timestamp: time.Now().Format(time.RFC3339Nano),
			FindingID: findingID,
			Message:   message,
			Source:    source,
			PaneID:    paneID,
		})
		return steeringSentMsg{
			findingID: findingID,
			message:   message,
			source:    source,
			at:        time.Now(),
			logErr:    logErr,
		}
	}
}

func (m *model) reconcileActiveSteeringIssue() {
	if strings.TrimSpace(m.activeSteerKey) == "" {
		return
	}
	for _, issue := range m.issueItems {
		if issue.Key() != m.activeSteerKey {
			continue
		}
		if strings.TrimSpace(issue.Message()) == m.activeSteerMessage {
			return
		}
		break
	}
	m.activeSteerKey = ""
	m.activeSteerMessage = ""
}

func (m model) issueSentStatus(issue CurrentIssue) string {
	sent, ok := m.sentNudges[issue.Key()]
	if !ok {
		return ""
	}
	status := fmt.Sprintf("[%s %s]", sent.Source, sent.At.Format("15:04"))
	if issue.Key() == m.activeSteerKey && strings.TrimSpace(issue.Message()) == m.activeSteerMessage {
		status += " [active]"
	}
	return status
}

func (m model) issueSummaryWithStatus(issue CurrentIssue) string {
	text := normalizeIssueText(issue.Nudge)
	if sent := m.issueSentStatus(issue); sent != "" {
		text += " " + sent
	}
	return text
}

func (m model) issuePopupSummary(issue CurrentIssue) string {
	text := normalizeIssueText(issue.Nudge)
	if sent := m.issueSentStatus(issue); sent != "" {
		return sent + " " + text
	}
	return text
}

func (m model) issueSummaryByKey(key, fallback string) string {
	key = strings.TrimSpace(key)
	for _, issue := range m.issueItems {
		if issue.Key() == key {
			return m.issueSummaryWithStatus(issue)
		}
	}
	return fallback
}

func (m model) bodyRect() selectionRect {
	return selectionRect{
		X: 0,
		Y: headerLineCount + 1,
		W: m.width,
		H: m.logH(),
	}
}

func (m model) footerRect() selectionRect {
	return selectionRect{
		X: 0,
		Y: headerLineCount + 2 + m.logH(),
		W: m.width,
		H: len(m.footerDockLines()),
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
	return "move j/k  page pgup/pgdn  details o  issues p  send s  auto a  copy drag  quit ctrl+c"
}

func issueControlsHint() string {
	return "issues j/k  jump enter  send s  auto a  close p"
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
		label = "Why TruPal believes this"
	case "TruPal verified":
		label = "Why TruPal believes this"
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
		text := m.issuePopupSummary(issue)
		lines = append(lines, renderContinuationLineWithMarker(marker, style.Render(wrapSingleLine(text, width-4))))
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
	content := m.baseContentLines()
	content = append(content, m.footerDockLines()...)
	return content
}

func (m model) baseContentLines() []string {
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
	content := m.contentLines()
	if len(content) == 0 {
		return selectionPoint{}, false
	}

	body := m.bodyRect()
	footer := m.footerRect()
	inBody := body.Contains(x, y)
	inFooter := footer.Contains(x, y)
	if !inBody && !inFooter {
		if !clamp {
			return selectionPoint{}, false
		}
		switch {
		case y < footer.Y:
			x, y = body.Clamp(x, y)
			inBody = true
		default:
			x, y = footer.Clamp(x, y)
			inFooter = true
		}
	}

	popupCount := len(m.issuesPopupLines())
	start, end := m.visibleLogRange()
	visibleCount := popupCount + (end - start)
	baseCount := len(m.baseContentLines())
	if visibleCount <= 0 && !inFooter {
		return selectionPoint{}, false
	}

	var lineIdx int
	if inFooter {
		row := y - footer.Y
		if row < 0 {
			row = 0
		}
		if row >= footer.H {
			row = footer.H - 1
		}
		lineIdx = baseCount + row
	} else {
		row := y - body.Y
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
		lineIdx = row
		if row >= popupCount {
			lineIdx = popupCount + start + (row - popupCount)
		}
	}

	relX := x
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

	// ── Header line 1: brand · elapsed ──
	h1 := renderHeaderTitle(m.elapsed, w)

	// ── Header line 2: watched session ──
	agentLabel := m.agentLabel
	if agentLabel == "" {
		agentLabel = "agent"
	}
	watchStatus := "idle"
	switch m.ccStatus {
	case "active", "thinking":
		watchStatus = "active"
	case "starting":
		watchStatus = "starting"
	case "":
		watchStatus = "idle"
	default:
		watchStatus = m.ccStatus
	}
	h2 := renderHeaderRoleLine("watch", m.headerWatchParts(agentLabel, watchStatus, w), w)

	// ── Header line 3: trupal brain / build / issue summary ──
	h3 := renderHeaderRoleLine("brain", m.headerBrainParts(), w)

	sep := sSep.Render(strings.Repeat("─", w))

	// ── Log area ──
	issueLines := m.issuePinnedLines()
	lh := m.logViewportH()
	start, end := m.visibleLogRange()
	content, _, _ := m.renderedTimeline()
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
			visible = append(visible, line)
		}
	}
	for len(visible) < lh {
		visible = append(visible, "")
	}
	bodyLines = append(bodyLines, visible...)

	// ── Footer dock ──
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
			if m.quitPending {
				hint = "press ctrl+c again to quit  esc cancel"
			}
			if len(m.issueItems) > 0 {
				if m.popupVisible() {
					hint = issueControlsHint()
				} else {
					hint = "move j/k  details o  issues p  page pgup/pgdn"
				}
			}
			footerParts = append(footerParts, truncateWidth(hint, remaining))
		}
	}
	footerLines := m.footerDockLinesWithState(strings.Join(footerParts, "  "), w)
	footerBase := len(m.baseContentLines())
	for i, line := range footerLines {
		if m.sel.IsLineSelected(footerBase + i) {
			startCol, endCol := m.sel.GetLineSelectionCols(footerBase + i)
			footerLines[i] = InjectCharacterRangeBackground(selectionDisplayLine(line, selectionTabWidth), startCol, endCol)
		}
	}
	controlsLine := footerLines[0]
	brainLine := footerLines[1]
	agentLine := footerLines[2]

	parts := []string{h1, h2, h3, sep, strings.Join(bodyLines, "\n"), sep, controlsLine, brainLine, agentLine}
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

func filesLine(files []string) string {
	parts := []string{}
	if len(files) > 0 {
		parts = append(parts, fmt.Sprintf("%d mod: %s", len(files), joinDisplayPaths(files, 3)))
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

func renderHeaderTitle(elapsed string, width int) string {
	contentWidth := width - 1
	if contentWidth <= 0 {
		return ""
	}

	brand := sBrandChip.Render("◉☰ TRUPAL")
	elapsedChip := ""
	if strings.TrimSpace(elapsed) != "" {
		elapsedChip = sMetaChip.Render(elapsed)
	}

	parts := []string{brand}
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

func (m model) headerWatchParts(agentLabel, watchStatus string, width int) []string {
	parts := []string{sHeaderValueText.Render(agentLabel)}
	if strings.TrimSpace(m.sessionModel) != "" {
		parts = append(parts, sHeaderValueText.Render(m.sessionModel))
	}
	if strings.TrimSpace(m.project) != "" {
		parts = append(parts, sHeaderValueText.Render(truncateWidth(m.project, max(18, width/3))))
	}
	parts = append(parts, styleHeaderStatusValue(watchStatus))
	return parts
}

func (m model) headerBrainParts() []string {
	var parts []string
	if strings.TrimSpace(m.brainIdentity) != "" {
		parts = append(parts, sHeaderValueText.Render(m.brainIdentity))
	}
	parts = append(parts, styleHeaderStatusValue(ansi.Strip(m.brainIndicator())))
	if m.steerModeAuto {
		parts = append(parts, sHeaderValueText.Render("steer auto"))
	} else {
		parts = append(parts, sDim.Render("steer manual"))
	}
	if m.buildState != "" {
		parts = append(parts, styleHeaderBuildValue(ansi.Strip(m.buildState)))
	}
	if m.findings > 0 {
		parts = append(parts, sWarn.Render(fmt.Sprintf("%d open", m.findings)))
	}
	if m.resolved > 0 {
		parts = append(parts, sOk.Render(fmt.Sprintf("%d resolved", m.resolved)))
	}
	return parts
}

func renderHeaderRoleLine(label string, parts []string, width int) string {
	contentWidth := width - 1
	if contentWidth <= 0 {
		return ""
	}

	line := sHeaderLabelText.Render(label + "  ")
	first := true
	for _, part := range parts {
		if strings.TrimSpace(ansi.Strip(part)) == "" {
			continue
		}
		candidate := line
		if !first {
			candidate += sDim.Render(" · ")
		}
		candidate += part
		if lipgloss.Width(candidate) > contentWidth {
			break
		}
		if !first {
			line += sDim.Render(" · ")
		}
		line += part
		first = false
	}

	return " " + line
}

func styleHeaderStatusValue(status string) string {
	plain := strings.TrimSpace(status)
	switch {
	case plain == "":
		return ""
	case strings.Contains(plain, "analyzing"):
		return sCyan.Render(plain)
	case strings.Contains(plain, "active"):
		return sCyan.Render(plain)
	case strings.Contains(plain, "starting"):
		return sHeaderValueText.Render(plain)
	case strings.Contains(plain, "ago"):
		return sHeaderValueText.Render(plain)
	case strings.Contains(plain, "idle"):
		return sDim.Render(plain)
	default:
		return sHeaderValueText.Render(plain)
	}
}

func styleHeaderBuildValue(text string) string {
	plain := strings.TrimSpace(text)
	switch {
	case strings.Contains(plain, "✗"):
		return sErr.Render(plain)
	case strings.Contains(plain, "✓"):
		return sOk.Render(plain)
	default:
		return sHeaderValueText.Render(plain)
	}
}

func joinWidth(parts []string, sep string) int {
	if len(parts) == 0 {
		return 0
	}
	return lipgloss.Width(strings.Join(parts, sep))
}
