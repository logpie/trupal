package main

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestMouseMotionWithoutPressStillSelects(t *testing.T) {
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
	if cmd == nil {
		t.Fatal("expected copy command when drag starts from motion")
	}

	_ = cmd()
	if copied != "line11\nline12" {
		t.Fatalf("expected copied text %q, got %q", "line11\nline12", copied)
	}
}
