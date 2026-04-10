package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestScrollBasic(t *testing.T) {
	m := initialModel("test")
	// Simulate window size
	m.width = 50
	m.height = 20 // logH = 20-8 = 12 with 3-line header + 3-line footer

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
	if m.maxScroll() != 18 { // 30 - 12 = 18
		t.Fatalf("expected maxScroll=18, got %d", m.maxScroll())
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
	m.height = 15 // logH = 15-8 = 7

	// Add numbered lines
	for i := 0; i < 20; i++ {
		m.log(fmt.Sprintf("content-%02d", i))
	}

	// At bottom — should see lines 13-19 (last 7)
	view := m.View()
	if !containsStr(view, "content-19") {
		t.Fatal("bottom: should see content-19")
	}
	if containsStr(view, "content-11") {
		t.Fatal("bottom: should NOT see content-11")
	}

	// Scroll to top — should see lines 0-6
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

func TestHeaderLinesFitWithinPaneWidth(t *testing.T) {
	m := initialModel("very-long-project-name-that-would-otherwise-wrap")
	m.width = 32
	m.height = 12
	m.elapsed = "12m"
	m.ccStatus = "active"
	m.buildState = sErr.Render("✗") + " build failing"
	m.findings = 4
	m.resolved = 2
	m.brain.lastTime = time.Now()
	m.brain.stats = BrainStats{
		TotalInputTokens:         120000,
		TotalOutputTokens:        5000,
		TotalCacheReadTokens:     150000,
		TotalCacheCreationTokens: 3000,
		TotalCostUSD:             1.2345,
	}

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected header lines in view, got %d lines", len(lines))
	}
	if got := lipgloss.Width(lines[0]); got > m.width {
		t.Fatalf("header line 1 wrapped: got width %d want <= %d", got, m.width)
	}
	if got := lipgloss.Width(lines[1]); got > m.width {
		t.Fatalf("header line 2 wrapped: got width %d want <= %d", got, m.width)
	}
	if got := lipgloss.Width(lines[2]); got > m.width {
		t.Fatalf("header line 3 wrapped: got width %d want <= %d", got, m.width)
	}
}

func TestHeaderCanShowWatchedSessionModel(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15
	newM, _ := m.Update(statusMsg{
		agentLabel:    "codex",
		ccStatus:      "active",
		sessionModel:  "gpt-5.4-mini",
		brainIdentity: "codex/default",
	})
	m = newM.(model)

	view := m.View()
	if !containsStr(view, "watch  codex") || !containsStr(view, "gpt-5.4-mini") || !containsStr(view, "brain  codex/default") {
		t.Fatalf("expected watched session model and brain identity in header, got %q", view)
	}
}

func TestFilesLineDisambiguatesDuplicateBasenames(t *testing.T) {
	line := filesLine([]string{"pkg/a/main.go", "cmd/main.go"})
	if !strings.Contains(line, "a/main.go") {
		t.Fatalf("expected duplicate basename to show parent dir, got %q", line)
	}
	if !strings.Contains(line, "cmd/main.go") {
		t.Fatalf("expected duplicate basename to show parent dir, got %q", line)
	}
}

func TestFooterCanShowCurrentIssueSummary(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15

	newM, _ := m.Update(statusMsg{
		issues: []CurrentIssue{{Nudge: "mutex missing"}, {Nudge: "marshal errors swallowed"}},
	})
	m = newM.(model)

	view := m.View()
	if containsStr(view, "mutex missing") {
		t.Fatalf("expected issue panel to be hidden by default, got %q", view)
	}
}

func TestIssuePanelReducesVisibleLogHeight(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15

	newM, _ := m.Update(statusMsg{
		issues: []CurrentIssue{{Nudge: "mutex missing"}, {Nudge: "marshal errors swallowed"}},
	})
	m = newM.(model)
	m.issuePanelVisible = true

	view := m.View()
	if !containsStr(view, "open issues 1/2") || !containsStr(view, "›   Mutex missing") {
		t.Fatalf("expected dedicated current-issues panel, got %q", view)
	}
}

func TestFooterShowsControlsHintWhenNoToast(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15

	view := m.View()
	if !containsStr(view, "move j/k") || !containsStr(view, "details o") {
		t.Fatalf("expected footer controls hint, got %q", view)
	}
}

func TestFooterShowsIssueControlsWhenIssuesExist(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15

	newM, _ := m.Update(statusMsg{
		issues: []CurrentIssue{{Nudge: "mutex missing"}, {Nudge: "marshal errors swallowed"}},
	})
	m = newM.(model)

	view := m.View()
	if !containsStr(view, "details o") || !containsStr(view, "issues p") {
		t.Fatalf("expected collapsed issue hint, got %q", view)
	}

	m.issuePanelVisible = true
	view = m.View()
	if !containsStr(view, "issues j/k") || !containsStr(view, "jump enter") {
		t.Fatalf("expected issue controls hint, got %q", view)
	}
}

func TestIssuePanelParticipatesInScrollableContent(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15
	newM, _ := m.Update(statusMsg{
		issues: []CurrentIssue{{Nudge: "mutex missing"}, {Nudge: "marshal errors swallowed"}},
	})
	m = newM.(model)
	m.issuePanelVisible = true

	content := m.contentLines()
	if len(content) == 0 || !containsStr(content[0], "open issues 1/2") {
		t.Fatalf("expected issue panel in content lines, got %#v", content)
	}
}

func TestIssuePanelCanExpandWhy(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15
	newM, _ := m.Update(statusMsg{
		issues: []CurrentIssue{{ID: "f-1", Nudge: "lock the sessions map", Why: "Both handlers touch the same global map."}},
	})
	m = newM.(model)
	m.issuePanelVisible = true

	view := m.View()
	if !containsStr(view, "Why Both handlers touch the same global map.") {
		t.Fatalf("expected expanded why text in issue panel, got %q", view)
	}
}

func TestIssuePanelStaysVisibleAtBottomOfLog(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15

	newM, _ := m.Update(statusMsg{
		issues: []CurrentIssue{{Nudge: "mutex missing"}, {Nudge: "marshal errors swallowed"}},
	})
	m = newM.(model)
	m.issuePanelVisible = true

	for i := 0; i < 20; i++ {
		m.log(fmt.Sprintf("content-%02d", i))
	}

	view := m.View()
	if !containsStr(view, "open issues 1/2") || !containsStr(view, "›   Mutex missing") {
		t.Fatalf("expected pinned issue panel, got %q", view)
	}
	if !containsStr(view, "content-19") {
		t.Fatalf("expected newest log lines to remain visible, got %q", view)
	}
}

func TestClosingExpandedEntryReclampsScrollAndKeepsLaterItemsVisible(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15
	m.entries = []timelineEntry{
		{
			ID:      "f-1",
			Kind:    "issue",
			Time:    "12:00",
			Marker:  "!",
			Summary: "first issue",
			Detail: []string{
				"Why it matters\nline one line two line three line four line five line six line seven line eight line nine line ten",
				"Code\n1: one\n2: two\n3: three\n4: four\n5: five\n6: six",
			},
		},
		{ID: "f-2", Kind: "issue", Time: "12:01", Marker: "!", Summary: "second issue"},
		{ID: "f-3", Kind: "issue", Time: "12:02", Marker: "!", Summary: "third issue"},
	}
	m.selectedEntry = 0
	m.openSelectedEntryDetail()
	m.scrollSelectedIntoView()

	// Closing the large first drawer should clamp scroll and keep later issues reachable/visible.
	m.toggleEntryDetail("f-1")
	view := m.View()
	if !containsStr(view, "second issue") || !containsStr(view, "third issue") {
		t.Fatalf("expected later issues to remain visible after closing drawer, got %q", view)
	}
}

func TestMouseWheelScrollsInspectorWhenDetailOverflows(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15
	m.entries = []timelineEntry{{
		ID:      "f-1",
		Kind:    "issue",
		Time:    "12:00",
		Marker:  "!",
		Summary: "selected issue",
		Detail: []string{
			"Why it matters\nline one line two line three line four line five line six line seven line eight",
			"Code\n1: one\n2: two\n3: three\n4: four\n5: five\n6: six\n7: seven\n8: eight",
		},
	}}
	m.selectedEntry = 0
	m.detailOpen["f-1"] = true

	lines, owners, _ := m.renderedTimeline()
	if len(lines) == 0 || len(owners) == 0 {
		t.Fatal("expected rendered timeline content")
	}
	foundDetail := false
	for i, owner := range owners {
		if owner == 0 && strings.Contains(lines[i], "Why it matters") {
			foundDetail = true
			break
		}
	}
	if !foundDetail {
		t.Fatal("expected opened inspector lines to be inline in the timeline")
	}
}

func TestDetailBlocksDoNotIncludeNextStep(t *testing.T) {
	finding := BrainFinding{
		Nudge: "Handle json.Marshal failures in ActiveJSON()",
		Tell:  "Handle json.Marshal failures in ActiveJSON().",
	}
	detail := detailBlocksForFinding(normalizeIssueText(finding.Nudge), finding.Nudge, finding, nil)
	for _, block := range detail {
		if strings.HasPrefix(block, "Next step\n") {
			t.Fatalf("expected redundant next step to be omitted, got %q", block)
		}
	}
}

func TestDetailBlocksDeduplicateCodexSaidByKeepingRicherClaim(t *testing.T) {
	finding := BrainFinding{
		Nudge: "Handle ListenAndServe errors",
		Claim: "made the session expiration logic actually remove expired entries before returning the current map",
	}
	extra := []string{
		"Codex said\nAdded POST /refresh in main.go and made the session expiration logic actually remove expired entries in main.go.",
	}
	detail := detailBlocksForFinding(normalizeIssueText(finding.Nudge), finding.Nudge, finding, extra)
	count := 0
	var claimBlock string
	for _, block := range detail {
		if strings.HasPrefix(block, "Codex said\n") {
			count++
			claimBlock = block
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one Codex said block, got %d in %#v", count, detail)
	}
	if !strings.Contains(claimBlock, "Added POST /refresh") {
		t.Fatalf("expected richer claim block to win, got %q", claimBlock)
	}
}

func TestResolvedEntryShowsUpdatedBlock(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15
	m.entries = []timelineEntry{{
		ID:      "f-1",
		Kind:    "issue",
		Time:    "12:00",
		Marker:  "!",
		Summary: "Handle json.Marshal failures in ActiveJSON()",
		Detail:  []string{"Why it matters\nBroken responses hide failures."},
	}}

	newM, _ := m.Update(resolvedMsg{finding: BrainFinding{ID: "f-1", Nudge: "Handle json.Marshal failures in ActiveJSON()"}})
	m = newM.(model)
	if m.entries[0].Kind != "resolved" {
		t.Fatalf("expected resolved kind, got %q", m.entries[0].Kind)
	}
	found := false
	for _, block := range m.entries[0].Detail {
		if strings.HasPrefix(block, "Updated\n") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Updated block in detail, got %#v", m.entries[0].Detail)
	}
}

func TestBuildResolvedDetailCombinesLatestChangeAndEvidence(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/main.go"
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tdelete(sessions, id)\n}\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	entries := []JSONLEntry{{
		Type:     "assistant",
		HasText:  true,
		TextSnip: "Added POST /refresh in main.go and made the session expiration logic actually remove expired entries in main.go.",
	}}
	detail := buildResolvedDetail(dir, []string{"main.go"}, entries, "Fix Expire() so expired sessions are removed")
	if len(detail) != 1 {
		t.Fatalf("expected single Updated block, got %#v", detail)
	}
	if !strings.Contains(detail[0], "Latest change:") || !strings.Contains(detail[0], "Latest evidence:") {
		t.Fatalf("expected combined latest change/evidence in Updated block, got %q", detail[0])
	}
}

func TestInfoMsgRendersAsInfoTimelineEntry(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15

	newM, _ := m.Update(infoMsg{message: "Verification target changed; re-check runtime assumptions."})
	m = newM.(model)

	view := m.View()
	if !containsStr(view, "Verification target changed") {
		t.Fatalf("expected info message in timeline, got %q", view)
	}
	if !containsStr(view, "i") {
		t.Fatalf("expected info marker in timeline, got %q", view)
	}
	if containsStr(view, "●   Verification target changed") {
		t.Fatalf("expected info message to be visually distinct from issue nudge, got %q", view)
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

func TestBrainStatsDisplaysCompactFooterDock(t *testing.T) {
	m := initialModel("test")
	m.width = 48
	m.height = 15

	newM, _ := m.Update(brainStatsMsg{stats: BrainStats{
		CostKnown:                true,
		TotalInputTokens:         58,
		TotalOutputTokens:        5053,
		TotalCacheReadTokens:     392000,
		TotalCacheCreationTokens: 36000,
		TotalCostUSD:             0.11,
	}})
	m = newM.(model)

	view := m.View()
	if !containsStr(view, "TruPal") || !containsStr(view, "in 36.1K") || !containsStr(view, "cache 392K") {
		t.Fatalf("expected compact brain stats in footer dock, got %q", view)
	}
	if containsStr(view, "cache_read=") || containsStr(view, "uncached=58") || containsStr(view, "out 5053") {
		t.Fatalf("expected narrow footer dock to avoid detailed cache counts, got %q", view)
	}
}

func TestBrainStatsDisplaysMinimalCacheFallback(t *testing.T) {
	m := initialModel("test")
	m.width = 40
	m.height = 15

	newM, _ := m.Update(brainStatsMsg{stats: BrainStats{
		CostKnown:                true,
		TotalInputTokens:         58,
		TotalOutputTokens:        5053,
		TotalCacheReadTokens:     392000,
		TotalCacheCreationTokens: 36000,
		TotalCostUSD:             0.11,
	}})
	m = newM.(model)

	view := m.View()
	if !containsStr(view, "in 36.1K") || !containsStr(view, "cache 392K") {
		t.Fatalf("expected minimal cache fallback in footer dock, got %q", view)
	}
	if containsStr(view, "uncached=58") || containsStr(view, "hit 92%") {
		t.Fatalf("expected minimal fallback to drop detailed token counts, got %q", view)
	}
}

func TestBrainStatsDisplaysDetailedFooterDockWhenWide(t *testing.T) {
	m := initialModel("test")
	m.width = 120
	m.height = 15

	newM, _ := m.Update(brainStatsMsg{stats: BrainStats{
		CostKnown:                true,
		TotalInputTokens:         58,
		TotalOutputTokens:        5053,
		TotalCacheReadTokens:     392000,
		TotalCacheCreationTokens: 36000,
		TotalCostUSD:             0.11,
	}})
	m = newM.(model)

	view := m.View()
	if !containsStr(view, "TruPal") || !containsStr(view, "in 36.1K") || !containsStr(view, "cache 392K") || !containsStr(view, "hit 92%") || !containsStr(view, "out 5053") || !containsStr(view, "cost $0.1100") {
		t.Fatalf("expected detailed brain stats in wide footer dock, got %q", view)
	}
}

func TestBrainStatsOmitsCostWhenUnknown(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15

	newM, _ := m.Update(brainStatsMsg{stats: BrainStats{
		Provider:                 ProviderCodex,
		CostKnown:                false,
		TotalInputTokens:         18128,
		TotalOutputTokens:        21,
		TotalCacheReadTokens:     3456,
		TotalCacheCreationTokens: 0,
	}})
	m = newM.(model)

	view := m.View()
	if containsStr(view, "$0.00") || containsStr(view, "cost=") {
		t.Fatalf("expected unknown cost to be omitted, got %q", view)
	}
	if !containsStr(view, "cache 3456") || !containsStr(view, "hit 19%") {
		t.Fatalf("expected explicit cache display, got %q", view)
	}
}

func TestBrainStatsCanDisplayLastTurnDiagnostics(t *testing.T) {
	m := initialModel("test")
	m.width = 120
	m.height = 15

	newM, _ := m.Update(brainStatsMsg{stats: BrainStats{
		Provider:             ProviderCodex,
		CostKnown:            false,
		LastDuration:         12 * time.Second,
		LastEffort:           "medium",
		TotalInputTokens:     36574,
		TotalOutputTokens:    54,
		TotalCacheReadTokens: 33792,
	}})
	m = newM.(model)

	view := m.View()
	if !containsStr(view, "turn 12s/medium") {
		t.Fatalf("expected last-turn diagnostics in footer dock, got %q", view)
	}
}

func TestFooterDockMetricColumnsStayAligned(t *testing.T) {
	m := initialModel("test")
	m.width = 120
	m.height = 18
	newM, _ := m.Update(statusMsg{
		agentLabel: "codex",
		ccStatus:   "active",
		agentStats: AgentUsageStats{
			Provider:          ProviderCodex,
			TotalInputTokens:  107000,
			TotalCachedTokens: 89300,
			TotalOutputTokens: 1844,
		},
	})
	m = newM.(model)
	newM, _ = m.Update(brainStatsMsg{stats: BrainStats{
		Provider:             ProviderCodex,
		TotalInputTokens:     303000,
		TotalCacheReadTokens: 197000,
		TotalOutputTokens:    4932,
		LastDuration:         16 * time.Second,
		LastEffort:           "high",
	}})
	m = newM.(model)

	lines := m.footerDockLines()
	if len(lines) < 3 {
		t.Fatalf("expected footer dock lines, got %d", len(lines))
	}
	trupal := ansi.Strip(selectionDisplayLine(lines[1], selectionTabWidth))
	codex := ansi.Strip(selectionDisplayLine(lines[2], selectionTabWidth))
	for _, marker := range []string{"cache", "hit", "out"} {
		if strings.Index(trupal, marker) != strings.Index(codex, marker) {
			t.Fatalf("expected %q column to align: trup=%q codex=%q", marker, trupal, codex)
		}
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
		buildOK: &buildOK,
		files:   []string{"a.go"},
	})
	m = newM.(model)
	if m.buildState == "" {
		t.Fatal("expected build state to be set")
	}
	if m.fileLine == "" {
		t.Fatal("expected file line to be set")
	}

	newM, _ = m.Update(statusMsg{
		buildOK: nil,
		files:   nil,
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

func TestCtrlCRequiresSecondPressToQuit(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15

	newM, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = newM.(model)
	if cmd != nil {
		t.Fatal("expected first ctrl+c to not quit immediately")
	}
	if !m.quitPending {
		t.Fatal("expected quitPending after first ctrl+c")
	}
	if !containsStr(m.View(), "press ctrl+c again to quit") {
		t.Fatalf("expected quit confirmation hint, got %q", m.View())
	}

	newM, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = newM.(model)
	if cmd == nil {
		t.Fatal("expected second ctrl+c to quit")
	}
	if !m.quitting {
		t.Fatal("expected quitting on second ctrl+c")
	}
}

func TestEscCancelsPendingQuit(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = newM.(model)
	if !m.quitPending {
		t.Fatal("expected quitPending after ctrl+c")
	}

	newM, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newM.(model)
	if cmd != nil {
		t.Fatal("expected esc cancel to not quit")
	}
	if m.quitPending {
		t.Fatal("expected esc to clear quitPending")
	}
}
