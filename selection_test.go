package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSelectionPointAtUsesVisibleWindow(t *testing.T) {
	m := initialModel("test")
	m.width = 40
	m.height = 15 // logH = 9

	for i := 0; i < 20; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line%02d", i))
	}

	point, ok := m.selectionPointAt(0, 3, false)
	if !ok {
		t.Fatal("expected selection point in log area")
	}
	if point.Line != 11 {
		t.Fatalf("expected first visible line to map to absolute line 11, got %d", point.Line)
	}
	if point.Col != 0 {
		t.Fatalf("expected col 0, got %d", point.Col)
	}
}

func TestSelectionPointAtUsesScrollOffset(t *testing.T) {
	m := initialModel("test")
	m.width = 40
	m.height = 15 // logH = 9
	m.scrollOffset = 5

	for i := 0; i < 20; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line%02d", i))
	}

	point, ok := m.selectionPointAt(0, 3, false)
	if !ok {
		t.Fatal("expected selection point in log area")
	}
	if point.Line != 6 {
		t.Fatalf("expected first visible line to map to absolute line 6, got %d", point.Line)
	}
}

func TestSelectionPointAtUsesVisibleRangeWhenContentShort(t *testing.T) {
	m := initialModel("test")
	m.width = 40
	m.height = 15 // logH = 9
	m.lines = []string{"first", "second"}

	point, ok := m.selectionPointAt(0, 3, false)
	if !ok {
		t.Fatal("expected first visible line to map")
	}
	if point.Line != 0 {
		t.Fatalf("expected row 0 to map to line 0, got %d", point.Line)
	}

	point, ok = m.selectionPointAt(0, 4, false)
	if !ok {
		t.Fatal("expected second visible line to map")
	}
	if point.Line != 1 {
		t.Fatalf("expected row 1 to map to line 1, got %d", point.Line)
	}

	if _, ok := m.selectionPointAt(0, 5, false); ok {
		t.Fatal("expected blank viewport rows to be non-selectable")
	}

	point, ok = m.selectionPointAt(0, 5, true)
	if !ok {
		t.Fatal("expected clamped blank-row selection to succeed")
	}
	if point.Line != 1 {
		t.Fatalf("expected clamped blank-row selection to snap to last visible line, got %d", point.Line)
	}
}

func TestMouseWheelScrollsOnlyInsideLogArea(t *testing.T) {
	m := initialModel("test")
	m.width = 40
	m.height = 15

	for i := 0; i < 20; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line%02d", i))
	}

	newM, _ := m.Update(tea.MouseMsg{
		X:      0,
		Y:      0,
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	m = newM.(model)
	if m.scrollOffset != 0 {
		t.Fatalf("wheel in header should not scroll, got %d", m.scrollOffset)
	}

	newM, _ = m.Update(tea.MouseMsg{
		X:      0,
		Y:      3,
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	m = newM.(model)
	if m.scrollOffset != 3 {
		t.Fatalf("wheel in log area should scroll by 3, got %d", m.scrollOffset)
	}
}

func TestMouseDragCopiesSelection(t *testing.T) {
	m := initialModel("test")
	m.width = 40
	m.height = 15

	for i := 0; i < 20; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line%02d", i))
	}

	var copied string
	prevCopy := copySelectedText
	copySelectedText = func(text string) error {
		copied = text
		return nil
	}
	defer func() {
		copySelectedText = prevCopy
	}()

	newM, _ := m.Update(tea.MouseMsg{
		X:      0,
		Y:      3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = newM.(model)

	newM, _ = m.Update(tea.MouseMsg{
		X:      20,
		Y:      4,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	m = newM.(model)

	newM, cmd := m.Update(tea.MouseMsg{
		X:      20,
		Y:      4,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	m = newM.(model)
	if cmd == nil {
		t.Fatal("expected copy command on drag release")
	}
	if m.sel.HasSelection() {
		t.Fatal("selection should clear immediately on release")
	}

	msg := cmd()
	copiedMsg, ok := msg.(SelectionCopiedMsg)
	if !ok {
		t.Fatalf("expected SelectionCopiedMsg, got %T", msg)
	}
	if copied != "line11\nline12" {
		t.Fatalf("expected copied text %q, got %q", "line11\nline12", copied)
	}
	if copiedMsg.Err != nil {
		t.Fatalf("unexpected copy error: %v", copiedMsg.Err)
	}

	newM, _ = m.Update(copiedMsg)
	m = newM.(model)
	if m.toastMsg != "✓ copied! paste with prefix+]" {
		t.Fatalf("expected success toast, got %q", m.toastMsg)
	}
	if m.sel.HasSelection() {
		t.Fatal("selection should be cleared after copy")
	}
}

func TestMouseDragCopiesSelectionFromScrolledView(t *testing.T) {
	m := initialModel("test")
	m.width = 40
	m.height = 15
	m.scrollOffset = 5

	for i := 0; i < 20; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line%02d", i))
	}

	var copied string
	prevCopy := copySelectedText
	copySelectedText = func(text string) error {
		copied = text
		return nil
	}
	defer func() {
		copySelectedText = prevCopy
	}()

	newM, _ := m.Update(tea.MouseMsg{
		X:      0,
		Y:      3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = newM.(model)

	newM, _ = m.Update(tea.MouseMsg{
		X:      20,
		Y:      4,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	m = newM.(model)

	newM, cmd := m.Update(tea.MouseMsg{
		X:      20,
		Y:      4,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	m = newM.(model)
	if cmd == nil {
		t.Fatal("expected copy command on drag release")
	}
	if m.sel.HasSelection() {
		t.Fatal("selection should clear immediately on release")
	}

	_ = cmd()
	if copied != "line06\nline07" {
		t.Fatalf("expected copied text %q, got %q", "line06\nline07", copied)
	}
}

func TestMouseMotionWithoutPressDoesNotSelect(t *testing.T) {
	m := initialModel("test")
	m.width = 40
	m.height = 15

	for i := 0; i < 20; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line%02d", i))
	}

	var copied string
	prevCopy := copySelectedText
	copySelectedText = func(text string) error {
		copied = text
		return nil
	}
	defer func() {
		copySelectedText = prevCopy
	}()

	newM, _ := m.Update(tea.MouseMsg{
		X:      0,
		Y:      3,
		Button: tea.MouseButtonNone,
		Action: tea.MouseActionMotion,
	})
	m = newM.(model)

	newM, cmd := m.Update(tea.MouseMsg{
		X:      20,
		Y:      4,
		Button: tea.MouseButtonNone,
		Action: tea.MouseActionRelease,
	})
	m = newM.(model)
	if cmd != nil {
		t.Fatal("did not expect copy command without a press")
	}
	if copied != "" {
		t.Fatalf("expected no copied text, got %q", copied)
	}
	if m.sel.HasSelection() {
		t.Fatal("selection should remain empty without a press")
	}
}

func TestMouseDragAutoScrollsAboveViewport(t *testing.T) {
	m := initialModel("test")
	m.width = 40
	m.height = 15
	m.scrollOffset = 3

	for i := 0; i < 20; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line%02d", i))
	}

	var copied string
	prevCopy := copySelectedText
	copySelectedText = func(text string) error {
		copied = text
		return nil
	}
	defer func() {
		copySelectedText = prevCopy
	}()

	newM, _ := m.Update(tea.MouseMsg{
		X:      0,
		Y:      3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = newM.(model)

	newM, _ = m.Update(tea.MouseMsg{
		X:      0,
		Y:      2,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	m = newM.(model)
	if m.scrollOffset != 4 {
		t.Fatalf("expected drag above viewport to scroll up by 1, got %d", m.scrollOffset)
	}

	newM, cmd := m.Update(tea.MouseMsg{
		X:      0,
		Y:      2,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	m = newM.(model)
	if cmd == nil {
		t.Fatal("expected copy command on drag release")
	}

	_ = cmd()
	if copied != "line07\nl" {
		t.Fatalf("expected copied text %q, got %q", "line07\nl", copied)
	}
}

func TestSelectionPointAtAndSelectedTextHandleANSIAndTabs(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15
	m.logStyled("!", "prefix\talpha", 80, sWarn)

	lineIdx := len(m.lines) - 1
	line := selectionDisplayLine(m.lines[lineIdx], selectionTabWidth)
	startCol := strings.Index(ansi.Strip(line), "alpha")
	if startCol < 0 {
		t.Fatalf("expected alpha in rendered line: %q", ansi.Strip(line))
	}

	point, ok := m.selectionPointAt(startCol, 3, false)
	if !ok {
		t.Fatal("expected selection point for styled line")
	}
	if point.Line != lineIdx {
		t.Fatalf("expected line %d, got %d", lineIdx, point.Line)
	}
	if point.Col != startCol {
		t.Fatalf("expected col %d, got %d", startCol, point.Col)
	}

	m.sel.Start = selectionPoint{Line: lineIdx, Col: startCol}
	m.sel.End = selectionPoint{Line: lineIdx, Col: startCol + len("alpha") - 1}
	text := m.sel.SelectedText(m.lines, selectionTabWidth)
	if text != "alpha" {
		t.Fatalf("expected copied text %q, got %q", "alpha", text)
	}

	view := m.View()
	if !strings.Contains(view, selectionBgANSI+"a") {
		t.Fatalf("expected highlighted alpha in view, got %q", view)
	}
}

func TestCopySelectedToClipboardReturnsTmuxError(t *testing.T) {
	t.Setenv("TMUX", "1")

	wantErr := errors.New("tmux load-buffer failed")
	prevLoad := loadTmuxBuffer
	loadTmuxBuffer = func(text string) error {
		if text != "hello" {
			t.Fatalf("unexpected text %q", text)
		}
		return wantErr
	}
	defer func() {
		loadTmuxBuffer = prevLoad
	}()

	if err := CopySelectedToClipboard("hello"); !errors.Is(err, wantErr) {
		t.Fatalf("expected tmux error %v, got %v", wantErr, err)
	}
}

func TestTrimClearsSelectionWhenLinesEvicted(t *testing.T) {
	m := initialModel("test")
	m.width = 60
	m.height = 20
	m.sel.Start = selectionPoint{Line: 0, Col: 0}
	m.sel.End = selectionPoint{Line: 1, Col: 1}
	m.sel.Anchor = selectionPoint{Line: 0, Col: 0}

	for i := 0; i < 520; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line-%03d", i))
	}
	m.trim()

	if m.sel.HasSelection() {
		t.Fatal("expected selection to clear after trim evicts selected lines")
	}
}

func TestCurrentIssuesPanelTextIsSelectable(t *testing.T) {
	m := initialModel("test")
	m.width = 80
	m.height = 15
	newM, _ := m.Update(statusMsg{
		issues: []CurrentIssue{{Nudge: "mutex missing"}, {Nudge: "marshal errors swallowed"}},
	})
	m = newM.(model)
	m.issuePanelVisible = true

	lines := m.contentLines()
	var issueLine int
	for i, line := range lines {
		if strings.Contains(line, "Mutex missing") {
			issueLine = i
			break
		}
	}

	expanded := selectionDisplayLine(lines[issueLine], selectionTabWidth)
	startCol := strings.Index(ansi.Strip(expanded), "Mutex missing")
	if startCol < 0 {
		t.Fatalf("expected Mutex missing in issue line %q", ansi.Strip(expanded))
	}
	m.sel.Start = selectionPoint{Line: issueLine, Col: max(0, startCol-2)}
	m.sel.End = selectionPoint{Line: issueLine, Col: startCol + len("Mutex missing") + 2}
	text := m.sel.SelectedText(lines, selectionTabWidth)
	if !strings.Contains(text, "missing") {
		t.Fatalf("expected selected text to include issue panel content, got %q", text)
	}
}
