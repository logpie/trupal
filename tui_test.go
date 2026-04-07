package main

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestScrollBasic(t *testing.T) {
	m := initialModel("test")
	// Simulate window size
	m.width = 50
	m.height = 20 // logH = 20-6 = 14

	// Add 30 lines — more than logH
	for i := 0; i < 30; i++ {
		m.log(fmt.Sprintf("line %d", i))
	}

	if len(m.lines) != 30 {
		t.Fatalf("expected 30 lines, got %d", len(m.lines))
	}

	// At bottom (scrollOffset=0), last lines visible
	if m.scrollOffset != 0 {
		t.Fatalf("expected scrollOffset=0, got %d", m.scrollOffset)
	}
	if m.maxScroll() != 16 { // 30 - 14 = 16
		t.Fatalf("expected maxScroll=16, got %d", m.maxScroll())
	}

	// Scroll up 5
	m.scroll(5)
	if m.scrollOffset != 5 {
		t.Fatalf("expected scrollOffset=5, got %d", m.scrollOffset)
	}

	// View should show earlier lines
	view := m.View()
	if view == "" {
		t.Fatal("empty view")
	}

	// Scroll past max should clamp
	m.scroll(100)
	if m.scrollOffset != m.maxScroll() {
		t.Fatalf("expected scrollOffset=%d (max), got %d", m.maxScroll(), m.scrollOffset)
	}

	// Scroll down past 0 should clamp
	m.scroll(-200)
	if m.scrollOffset != 0 {
		t.Fatalf("expected scrollOffset=0, got %d", m.scrollOffset)
	}
}

func TestScrollKeyHandling(t *testing.T) {
	m := initialModel("test")
	m.width = 50
	m.height = 20

	for i := 0; i < 30; i++ {
		m.log(fmt.Sprintf("line %d", i))
	}

	// Test 'k' scrolls up
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = newM.(model)
	if m.scrollOffset != 1 {
		t.Fatalf("k: expected scrollOffset=1, got %d", m.scrollOffset)
	}

	// Test 'j' scrolls down
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = newM.(model)
	if m.scrollOffset != 0 {
		t.Fatalf("j: expected scrollOffset=0, got %d", m.scrollOffset)
	}

	// Test pgup scrolls 10
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = newM.(model)
	if m.scrollOffset != 10 {
		t.Fatalf("pgup: expected scrollOffset=10, got %d", m.scrollOffset)
	}

	// Test G goes to bottom
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = newM.(model)
	if m.scrollOffset != 0 {
		t.Fatalf("G: expected scrollOffset=0, got %d", m.scrollOffset)
	}

	// Test g goes to top
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = newM.(model)
	if m.scrollOffset != m.maxScroll() {
		t.Fatalf("g: expected scrollOffset=%d, got %d", m.maxScroll(), m.scrollOffset)
	}
}

func TestScrollViewContent(t *testing.T) {
	m := initialModel("test")
	m.width = 60
	m.height = 15 // logH = 15-6 = 9

	// Add numbered lines
	for i := 0; i < 20; i++ {
		m.log(fmt.Sprintf("content-%02d", i))
	}

	// At bottom — should see lines 11-19 (last 9)
	view := m.View()
	if !containsStr(view, "content-19") {
		t.Fatal("bottom: should see content-19")
	}
	if containsStr(view, "content-05") {
		t.Fatal("bottom: should NOT see content-05")
	}

	// Scroll to top — should see lines 0-8
	m.scrollOffset = m.maxScroll()
	view = m.View()
	if !containsStr(view, "content-00") {
		t.Fatal("top: should see content-00")
	}
	if containsStr(view, "content-19") {
		t.Fatal("top: should NOT see content-19")
	}

	// Scroll indicator in footer
	if !containsStr(view, "↑") {
		t.Fatal("should have scroll indicator when scrolled up")
	}

	// At bottom — no scroll indicator
	m.scrollOffset = 0
	view = m.View()
	if containsStr(view, "↑") {
		t.Fatal("should NOT have scroll indicator at bottom")
	}
}

func TestNoScrollWhenContentFits(t *testing.T) {
	m := initialModel("test")
	m.width = 60
	m.height = 20 // logH = 14

	// Add only 5 lines — fits in view
	for i := 0; i < 5; i++ {
		m.log(fmt.Sprintf("line %d", i))
	}

	if m.maxScroll() != 0 {
		t.Fatalf("expected maxScroll=0 when content fits, got %d", m.maxScroll())
	}

	// Scrolling should be no-op
	m.scroll(10)
	if m.scrollOffset != 0 {
		t.Fatalf("should not scroll when content fits, got %d", m.scrollOffset)
	}
}

func TestLineTrimming(t *testing.T) {
	m := initialModel("test")
	m.width = 50
	m.height = 20

	// Add 600 lines — should be trimmed to 500
	for i := 0; i < 600; i++ {
		m.log(fmt.Sprintf("line %d", i))
	}

	if len(m.lines) != 500 {
		t.Fatalf("expected 500 lines after trim, got %d", len(m.lines))
	}
}

func TestLogStyledCompactWrap(t *testing.T) {
	m := initialModel("test")
	m.width = 48

	text := "GetOrLoadConfig swallows both os.ReadFile and json.Unmarshal errors instead of returning them."
	m.logStyled("!", text, m.width, lipgloss.NewStyle())

	if len(m.lines) < 2 {
		t.Fatalf("expected wrapped log entry, got %d line(s)", len(m.lines))
	}

	wrapped := wrap(text, logTextWidth(m.width))
	if len(wrapped) < 2 {
		t.Fatalf("expected wrapped content, got %d segment(s)", len(wrapped))
	}

	for i, line := range m.lines {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("line %d exceeds pane width: got %d want <= %d", i, got, m.width)
		}
	}

	firstIndent := lipgloss.Width(m.lines[0]) - lipgloss.Width(wrapped[0])
	secondIndent := lipgloss.Width(m.lines[1]) - lipgloss.Width(wrapped[1])
	if firstIndent != secondIndent {
		t.Fatalf("wrapped text does not align: first indent=%d second indent=%d", firstIndent, secondIndent)
	}

	if !containsStr(m.lines[1], "│") {
		t.Fatalf("expected continuation marker in wrapped line: %q", m.lines[1])
	}
}

func TestLogNudgeUsesSharedLayoutWithoutAccentBar(t *testing.T) {
	m := initialModel("test")
	m.width = 48

	text := "Return the real parse error here so the next retry doesn't keep masking the root cause."
	label, textStyle := nudgePresentation("warn")
	m.logStyled(label, text, m.width, textStyle)

	if len(m.lines) < 2 {
		t.Fatalf("expected wrapped nudge entry, got %d line(s)", len(m.lines))
	}

	wrapped := wrap(text, logTextWidth(m.width))
	if len(wrapped) < 2 {
		t.Fatalf("expected wrapped nudge content, got %d segment(s)", len(wrapped))
	}

	for i, line := range m.lines {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("line %d exceeds pane width: got %d want <= %d", i, got, m.width)
		}
		if containsStr(line, "▌") {
			t.Fatalf("unexpected nudge accent bar in line %d: %q", i, line)
		}
	}

	firstIndent := lipgloss.Width(m.lines[0]) - lipgloss.Width(wrapped[0])
	secondIndent := lipgloss.Width(m.lines[1]) - lipgloss.Width(wrapped[1])
	if firstIndent != secondIndent {
		t.Fatalf("wrapped nudge text does not align: first indent=%d second indent=%d", firstIndent, secondIndent)
	}
}

func TestBrainStatusThinkingStartsSpinnerWithoutLogging(t *testing.T) {
	m := initialModel("test")
	m.width = 60
	m.height = 15

	newM, _ := m.Update(brainStatusMsg{thinking: true})
	m = newM.(model)

	if !m.brain.thinking {
		t.Fatal("expected brain thinking state to be active")
	}
	// No separate spinner command — animation runs via tickEvery
	if len(m.lines) != 0 {
		t.Fatalf("expected no log lines for analyzing state, got %d", len(m.lines))
	}
	if !containsStr(m.View(), "analyzing") {
		t.Fatalf("expected analyzing indicator in header, got %q", m.View())
	}
}

func TestBrainStatusFinishedShowsRelativeAge(t *testing.T) {
	m := initialModel("test")
	m.width = 60
	m.height = 15

	newM, _ := m.Update(brainStatusMsg{thinking: false, lastTime: time.Now().Add(-65 * time.Second)})
	m = newM.(model)

	if m.brain.thinking {
		t.Fatal("expected brain thinking state to be inactive")
	}
	view := m.View()
	if containsStr(view, "analyzing") {
		t.Fatalf("expected finished brain indicator, got %q", view)
	}
	if !containsStr(view, "1m ago") {
		t.Fatalf("expected relative age in header, got %q", view)
	}
}

func TestBrainStatsDisplaysCompactHeader(t *testing.T) {
	m := initialModel("test")
	m.width = 48
	m.height = 15

	newM, _ := m.Update(brainStatsMsg{stats: BrainStats{
		TotalInputTokens:         58,
		TotalOutputTokens:        5053,
		TotalCacheReadTokens:     392000,
		TotalCacheCreationTokens: 36000,
		TotalCostUSD:             0.11,
	}})
	m = newM.(model)

	view := m.View()
	if !containsStr(view, "in=58 out=5053 92% $0.1100") {
		t.Fatalf("expected compact brain stats in header, got %q", view)
	}
	if containsStr(view, "cache_read=") {
		t.Fatalf("expected narrow header to avoid detailed cache counts, got %q", view)
	}
}

func TestBrainStatsDisplaysMinimalCacheFallback(t *testing.T) {
	m := initialModel("test")
	m.width = 34
	m.height = 15

	newM, _ := m.Update(brainStatsMsg{stats: BrainStats{
		TotalInputTokens:         58,
		TotalOutputTokens:        5053,
		TotalCacheReadTokens:     392000,
		TotalCacheCreationTokens: 36000,
		TotalCostUSD:             0.11,
	}})
	m = newM.(model)

	view := m.View()
	if !containsStr(view, "$0.1100 92% cache") {
		t.Fatalf("expected minimal cache fallback in header, got %q", view)
	}
	if containsStr(view, "in=58 out=5053") {
		t.Fatalf("expected minimal fallback to drop detailed token counts, got %q", view)
	}
}

func TestBrainStatsDisplaysDetailedHeaderWhenWide(t *testing.T) {
	m := initialModel("test")
	m.width = 120
	m.height = 15

	newM, _ := m.Update(brainStatsMsg{stats: BrainStats{
		TotalInputTokens:         58,
		TotalOutputTokens:        5053,
		TotalCacheReadTokens:     392000,
		TotalCacheCreationTokens: 36000,
		TotalCostUSD:             0.11,
	}})
	m = newM.(model)

	view := m.View()
	if !containsStr(view, "in=58 out=5053 cache_read=392K cache_create=36K 92% cost=$0.1100") {
		t.Fatalf("expected detailed brain stats in wide header, got %q", view)
	}
}

func TestFormatTokenCount(t *testing.T) {
	cases := map[int]string{
		58:     "58",
		5053:   "5053",
		14500:  "14.5K",
		36000:  "36K",
		392000: "392K",
	}

	for input, want := range cases {
		if got := formatTokenCount(input); got != want {
			t.Fatalf("formatTokenCount(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestStatusMsgClearsBuildAndFileState(t *testing.T) {
	m := initialModel("test")

	buildOK := true
	newM, _ := m.Update(statusMsg{
		buildOK:  &buildOK,
		files:    []string{"a.go"},
		newFiles: []string{"b.go"},
	})
	m = newM.(model)
	if m.buildState == "" {
		t.Fatal("expected build state to be set")
	}
	if m.fileLine == "" {
		t.Fatal("expected file line to be set")
	}

	newM, _ = m.Update(statusMsg{
		buildOK:  nil,
		files:    nil,
		newFiles: nil,
	})
	m = newM.(model)
	if m.buildState != "" {
		t.Fatalf("expected build state to clear, got %q", m.buildState)
	}
	if m.fileLine != "" {
		t.Fatalf("expected file line to clear, got %q", m.fileLine)
	}
}

func containsStr(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
