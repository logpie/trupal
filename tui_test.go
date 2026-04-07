package main

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
