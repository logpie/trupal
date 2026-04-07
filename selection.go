package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
)

const (
	selectionTabWidth = 8
	selectionBgANSI   = "\x1b[48;5;238m"
)

var ansiResetRe = regexp.MustCompile(`\x1b\[0?m`)

// Selection tracks drag selection state in the log viewport.
type Selection struct {
	Active bool
	Start  selectionPoint
	End    selectionPoint
	Anchor selectionPoint
	View   selectionRect
}

type selectionPoint struct {
	Line int
	Col  int
}

func (p selectionPoint) Valid() bool {
	return p.Line >= 0 && p.Col >= 0
}

func (p selectionPoint) Before(other selectionPoint) bool {
	return p.Line < other.Line || (p.Line == other.Line && p.Col < other.Col)
}

type selectionRect struct {
	X int
	Y int
	W int
	H int
}

func (r selectionRect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func (r selectionRect) Clamp(x, y int) (int, int) {
	if r.W > 0 {
		if x < r.X {
			x = r.X
		}
		if x >= r.X+r.W {
			x = r.X + r.W - 1
		}
	}
	if r.H > 0 {
		if y < r.Y {
			y = r.Y
		}
		if y >= r.Y+r.H {
			y = r.Y + r.H - 1
		}
	}
	return x, y
}

func NewSelection() *Selection {
	s := &Selection{}
	s.Clear()
	return s
}

func (s *Selection) Clear() {
	s.Active = false
	s.Start = selectionPoint{-1, -1}
	s.End = selectionPoint{-1, -1}
	s.Anchor = selectionPoint{-1, -1}
	s.View = selectionRect{}
}

func (s *Selection) HasSelection() bool {
	return s.Start.Valid() && s.End.Valid()
}

func (s *Selection) PrepareDrag(line, col int, view selectionRect) {
	s.Active = false
	s.Start = selectionPoint{-1, -1}
	s.End = selectionPoint{-1, -1}
	s.Anchor = selectionPoint{Line: line, Col: col}
	s.View = view
}

func (s *Selection) HandleDrag(line, col int) {
	current := selectionPoint{Line: line, Col: col}
	if !s.Anchor.Valid() {
		s.Anchor = current
	}
	if !s.Start.Valid() {
		s.Start = s.Anchor
		s.End = s.Anchor
	}

	s.Active = true

	if current.Before(s.Anchor) {
		s.Start = current
		s.End = s.Anchor
		return
	}
	s.Start = s.Anchor
	s.End = current
}

func (s *Selection) FinishDrag() bool {
	if !s.Start.Valid() {
		s.Clear()
		return false
	}
	s.Active = false
	return true
}

func (s *Selection) IsLineSelected(lineIdx int) bool {
	if !s.HasSelection() {
		return false
	}
	start, end := s.normalized()
	return lineIdx >= start.Line && lineIdx <= end.Line
}

func (s *Selection) GetLineSelectionCols(lineIdx int) (startCol, endCol int) {
	if !s.HasSelection() {
		return -1, -1
	}

	start, end := s.normalized()
	if lineIdx < start.Line || lineIdx > end.Line {
		return -1, -1
	}
	if start.Line == end.Line {
		return start.Col, end.Col
	}
	if lineIdx == start.Line {
		return start.Col, -1
	}
	if lineIdx == end.Line {
		return 0, end.Col
	}
	return 0, -1
}

func (s *Selection) SelectedText(lines []string, tabWidth int) string {
	if !s.HasSelection() || len(lines) == 0 {
		return ""
	}

	start, end := s.normalized()
	if end.Line < 0 || start.Line >= len(lines) {
		return ""
	}
	if start.Line < 0 {
		start.Line = 0
	}
	if end.Line >= len(lines) {
		end.Line = len(lines) - 1
	}
	if start.Line > end.Line {
		return ""
	}

	selected := make([]string, 0, end.Line-start.Line+1)
	for lineIdx := start.Line; lineIdx <= end.Line; lineIdx++ {
		expanded := ExpandTabs(lines[lineIdx], tabWidth)
		switch {
		case start.Line == end.Line:
			selected = append(selected, VisualSubstring(expanded, start.Col, end.Col+1))
		case lineIdx == start.Line:
			selected = append(selected, VisualSubstring(expanded, start.Col, -1))
		case lineIdx == end.Line:
			selected = append(selected, VisualSubstring(expanded, 0, end.Col+1))
		default:
			selected = append(selected, ansi.Strip(expanded))
		}
	}

	return strings.Join(selected, "\n")
}

func (s *Selection) normalized() (selectionPoint, selectionPoint) {
	start := s.Start
	end := s.End
	if end.Before(start) {
		start, end = end, start
	}
	return start, end
}

func CopySelectedToClipboard(text string) error {
	if text == "" {
		return nil
	}

	if os.Getenv("TMUX") != "" {
		loadCmd := exec.Command("tmux", "load-buffer", "-")
		loadCmd.Stdin = strings.NewReader(text)
		if err := loadCmd.Run(); err == nil {
			return nil
		}

		setCmd := exec.Command("tmux", "set-buffer", "--", text)
		if err := setCmd.Run(); err == nil {
			return nil
		}
	}

	return clipboard.WriteAll(text)
}

func ExpandTabs(line string, tabWidth int) string {
	if tabWidth <= 0 || !strings.Contains(line, "\t") {
		return line
	}

	var sb strings.Builder
	sb.Grow(len(line))

	state := ansi.NormalState
	column := 0
	for len(line) > 0 {
		seq, width, n, newState := ansi.GraphemeWidth.DecodeSequenceInString(line, state, nil)
		if n <= 0 {
			sb.WriteString(line)
			break
		}
		if seq == "\t" && width == 0 {
			spaces := tabWidth - (column % tabWidth)
			if spaces == 0 {
				spaces = tabWidth
			}
			sb.WriteString(strings.Repeat(" ", spaces))
			column += spaces
		} else {
			sb.WriteString(seq)
			column += width
		}
		state = newState
		line = line[n:]
	}

	return sb.String()
}

// VisualSubstring returns plain text for the visual column range [startCol, endCol).
// If endCol is -1 the range extends to the end of the line.
func VisualSubstring(s string, startCol, endCol int) string {
	if s == "" {
		return ""
	}

	var sb strings.Builder
	state := ansi.NormalState
	cumWidth := 0

	for len(s) > 0 {
		seq, width, n, newState := ansi.GraphemeWidth.DecodeSequenceInString(s, state, nil)
		if n <= 0 {
			break
		}
		if width > 0 {
			charStart := cumWidth
			charEnd := cumWidth + width
			cumWidth = charEnd

			inRange := false
			if endCol == -1 {
				inRange = charEnd > startCol
			} else {
				inRange = charStart < endCol && charEnd > startCol
			}
			if inRange {
				sb.WriteString(seq)
			}
			if endCol >= 0 && cumWidth >= endCol {
				break
			}
		}

		state = newState
		s = s[n:]
	}

	return ansi.Strip(sb.String())
}

func InjectSelectionBackground(s string) string {
	result := selectionBgANSI + s
	result = ansiResetRe.ReplaceAllString(result, "${0}"+selectionBgANSI)
	return result + "\x1b[49m"
}

func InjectCharacterRangeBackground(line string, startCol, endCol int) string {
	if startCol == 0 && endCol == -1 {
		return InjectSelectionBackground(line)
	}

	var sb strings.Builder
	sb.Grow(len(line) + 64)

	state := ansi.NormalState
	cumWidth := 0
	inSelection := false

	for len(line) > 0 {
		seq, width, n, newState := ansi.GraphemeWidth.DecodeSequenceInString(line, state, nil)
		if n <= 0 {
			sb.WriteString(line)
			break
		}

		if width > 0 {
			charInRange := false
			if endCol == -1 {
				charInRange = cumWidth >= startCol
			} else {
				charInRange = cumWidth >= startCol && cumWidth <= endCol
			}

			if charInRange && !inSelection {
				sb.WriteString(selectionBgANSI)
				inSelection = true
			} else if !charInRange && inSelection {
				sb.WriteString("\x1b[49m")
				inSelection = false
			}

			sb.WriteString(seq)
			cumWidth += width

			if endCol >= 0 && cumWidth > endCol && inSelection {
				sb.WriteString("\x1b[49m")
				inSelection = false
			}
		} else {
			sb.WriteString(seq)
			if inSelection && ansiResetRe.MatchString(seq) {
				sb.WriteString(selectionBgANSI)
			}
		}

		state = newState
		line = line[n:]
	}

	if inSelection {
		sb.WriteString("\x1b[49m")
	}

	return sb.String()
}

func VisualColAtRelativeX(expandedLine string, relX int) int {
	if relX < 0 {
		return 0
	}

	state := ansi.NormalState
	cumWidth := 0
	lastCharCol := 0
	hasChars := false

	for len(expandedLine) > 0 {
		_, width, n, newState := ansi.GraphemeWidth.DecodeSequenceInString(expandedLine, state, nil)
		if n <= 0 {
			break
		}
		if width > 0 {
			hasChars = true
			if relX >= cumWidth && relX < cumWidth+width {
				return cumWidth
			}
			lastCharCol = cumWidth
			cumWidth += width
		}
		state = newState
		expandedLine = expandedLine[n:]
	}

	if !hasChars {
		return 0
	}
	if relX >= cumWidth {
		return lastCharCol
	}
	return relX
}

// SelectionCopiedMsg is sent when text is copied to the tmux buffer or clipboard.
type SelectionCopiedMsg struct {
	Text string
	Time time.Time
	Err  error
}
