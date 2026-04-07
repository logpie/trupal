package main

import (
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
)

// Selection tracks mouse text selection state in the log area.
type Selection struct {
	active    bool
	startLine int
	startCol  int
	endLine   int
	endCol    int
	// Log area position (for converting mouse coords to log positions).
	logTop    int // Y offset of first log line on screen
	logLeft   int // X offset (usually 0)
}

func NewSelection() *Selection {
	return &Selection{startLine: -1, endLine: -1}
}

// StartDrag begins a selection at the given screen position.
func (s *Selection) StartDrag(x, y, logTop int, scrollOffset int) {
	logLine := (y - logTop) + scrollOffset
	s.startLine = logLine
	s.startCol = x
	s.endLine = logLine
	s.endCol = x
	s.logTop = logTop
	s.active = false // becomes active on first drag motion
}

// UpdateDrag updates the selection end point during mouse drag.
func (s *Selection) UpdateDrag(x, y, scrollOffset int) {
	s.active = true
	s.endLine = (y - s.logTop) + scrollOffset
	s.endCol = x
}

// FinishDrag completes the selection. Returns the selected text (full lines, ANSI stripped).
func (s *Selection) FinishDrag(lines []string) string {
	if !s.active || s.startLine < 0 {
		s.Clear()
		return ""
	}

	// Normalize: start before end
	sl, el := s.startLine, s.endLine
	if sl > el {
		sl, el = el, sl
	}

	// Extract full lines, strip ANSI
	var selected []string
	for i := sl; i <= el && i < len(lines); i++ {
		if i < 0 {
			continue
		}
		line := strings.TrimSpace(stripAnsi(lines[i]))
		if line != "" {
			selected = append(selected, line)
		}
	}

	text := strings.Join(selected, "\n")
	s.active = false
	return text
}

// Clear resets the selection.
func (s *Selection) Clear() {
	s.active = false
	s.startLine = -1
	s.endLine = -1
}

// IsActive returns whether a drag selection is in progress.
func (s *Selection) IsActive() bool {
	return s.active
}

// IsLineSelected returns true if the given log line index is within the selection.
func (s *Selection) IsLineSelected(lineIdx int) bool {
	if !s.active && s.startLine < 0 {
		return false
	}
	sl, el := s.startLine, s.endLine
	if sl > el {
		sl, el = el, sl
	}
	return lineIdx >= sl && lineIdx <= el
}

// CopySelectedToClipboard copies text to clipboard.
// Tries tmux buffer first (works over SSH), falls back to system clipboard.
func CopySelectedToClipboard(text string) error {
	if text == "" {
		return nil
	}
	// tmux set-buffer works everywhere in tmux, even over SSH.
	if os.Getenv("TMUX") != "" {
		cmd := exec.Command("tmux", "set-buffer", text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return clipboard.WriteAll(text)
}

// stripAnsi removes ANSI escape codes from a string.
func stripAnsi(s string) string {
	return ansi.Strip(s)
}

// SelectionCopiedMsg is sent when text is copied to clipboard.
type SelectionCopiedMsg struct {
	Text string
	Time time.Time
}
