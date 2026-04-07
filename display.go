package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	reset  = "\033[0m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	dim    = "\033[2m"
	bold   = "\033[1m"
	cyan   = "\033[36m"
)

// DisplayState holds all the information needed to render one frame.
type DisplayState struct {
	ProjectDir         string
	Elapsed            string
	ChangedFiles       []string
	UntrackedFiles     []string
	Build              *BuildDisplay
	TrajectoryFindings []Finding
	DeletedTests       []string
	BrainFindings      []BrainFinding
	BrainThinking      bool
	BrainLastMsg       string // latest brain reasoning (even with 0 nudges)
	CCStatus           string
}

// BuildDisplay carries build result info for rendering.
type BuildDisplay struct {
	OK         bool
	ErrorCount int
	Trend      string
}

// getPaneWidth returns the width of this process's tmux pane.
func getPaneWidth() int {
	// Use TMUX_PANE to query our specific pane's width.
	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return 45
	}
	out, err := exec.Command("tmux", "display", "-t", paneID, "-p", "#{pane_width}").Output()
	if err != nil {
		return 45
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || n < 20 {
		return 45
	}
	return n
}

// Render redraws the status frame in place (no scrollback pollution).
func Render(state DisplayState) {
	// Move cursor home + clear screen. Use ED (erase display) mode 2 only on first render,
	// then use ED mode 0 (erase below cursor) to avoid scrollback issues.
	fmt.Print("\033[H\033[J")
	w := getPaneWidth()

	// ── Header ──
	printLine(w, fmt.Sprintf(" %strupal%s  %s%s%s", bold, reset, dim, state.Elapsed, reset))

	// CC status + build on one line
	parts := []string{}
	if state.CCStatus != "" {
		switch state.CCStatus {
		case "active":
			parts = append(parts, fmt.Sprintf("%s●%s cc", green, reset))
		case "thinking":
			parts = append(parts, fmt.Sprintf("%s◌%s cc", yellow, reset))
		default:
			parts = append(parts, fmt.Sprintf("%s○%s cc", dim, reset))
		}
	}
	if state.Build != nil {
		if state.Build.OK {
			parts = append(parts, fmt.Sprintf("%s✓%s build", green, reset))
		} else {
			label := fmt.Sprintf("%d err", state.Build.ErrorCount)
			if state.Build.Trend != "" {
				label += " (" + state.Build.Trend + ")"
			}
			parts = append(parts, fmt.Sprintf("%s✗%s %s", red, reset, label))
		}
	}
	if len(parts) > 0 {
		fmt.Printf(" %s\n", strings.Join(parts, "  "))
	}

	// ── Files ── (compact: just count + names)
	nMod := len(state.ChangedFiles)
	nNew := len(state.UntrackedFiles)
	if nMod > 0 || nNew > 0 {
		fileParts := []string{}
		if nMod > 0 {
			names := baseNames(state.ChangedFiles, 4)
			fileParts = append(fileParts, fmt.Sprintf("%s%d mod:%s %s", dim, nMod, reset, strings.Join(names, " ")))
		}
		if nNew > 0 {
			names := baseNames(state.UntrackedFiles, 3)
			fileParts = append(fileParts, fmt.Sprintf("%s%d new:%s %s", dim, nNew, reset, strings.Join(names, " ")))
		}
		for _, p := range fileParts {
			fmt.Printf(" %s\n", p)
		}
	}

	// ── Trajectory warnings ──
	for _, f := range state.TrajectoryFindings {
		wrapPrint(w, fmt.Sprintf(" %s▸ %s%s", yellow, f.Message, reset), 3)
	}
	for _, dt := range state.DeletedTests {
		wrapPrint(w, fmt.Sprintf(" %s▸ deleted %s%s", yellow, filepath.Base(dt), reset), 3)
	}

	// ── Brain ──
	hasBrain := len(state.BrainFindings) > 0 || state.BrainThinking || state.BrainLastMsg != ""
	if hasBrain {
		fmt.Println()
		separator(w)

		if state.BrainThinking {
			fmt.Printf(" %s◌ analyzing...%s\n", cyan, reset)
		} else if len(state.BrainFindings) == 0 && state.BrainLastMsg != "" {
			// No nudges but brain has a status — show it dimmed.
			lines := wordWrap(state.BrainLastMsg, w-2)
			for _, line := range lines {
				fmt.Printf(" %s%s%s\n", dim, line, reset)
			}
		}

		for _, f := range state.BrainFindings {
			renderFinding(f, w)
		}
	}

	// No content at all
	if !hasAnyContent(state) {
		fmt.Printf(" %s…%s\n", dim, reset)
	}

	fmt.Println()
}

func hasAnyContent(s DisplayState) bool {
	return len(s.ChangedFiles) > 0 || len(s.UntrackedFiles) > 0 ||
		s.Build != nil || len(s.TrajectoryFindings) > 0 ||
		len(s.DeletedTests) > 0 || len(s.BrainFindings) > 0 || s.BrainThinking
}

func renderFinding(f BrainFinding, w int) {
	ts := f.Timestamp.Format("15:04")

	// Status prefix
	prefix := ""
	switch f.Status {
	case "resolved":
		prefix = fmt.Sprintf(" %s%s ✓%s ", dim, ts, reset)
	case "waived":
		prefix = fmt.Sprintf(" %s%s -%s ", dim, ts, reset)
	default:
		prefix = fmt.Sprintf(" %s%s%s ", dim, ts, reset)
	}

	// Reasoning (dim, wrapped)
	if f.Reasoning != "" {
		color := dim
		if f.Status == "resolved" || f.Status == "waived" {
			color = dim
		}
		lines := wordWrap(f.Reasoning, w-2)
		for i, line := range lines {
			if i == 0 {
				fmt.Printf("%s%s%s%s\n", prefix, color, line, reset)
			} else {
				fmt.Printf(" %s%s%s\n", color, strings.Repeat(" ", len(ts)+1)+line, reset)
			}
		}
	}

	// Nudge (yellow arrow, prominent)
	if f.Nudge != "" {
		nudgeColor := yellow
		if f.Status == "resolved" || f.Status == "waived" {
			nudgeColor = dim
		}
		lines := wordWrap("→ "+f.Nudge, w-4)
		for _, line := range lines {
			fmt.Printf("   %s%s%s\n", nudgeColor, line, reset)
		}
	}
	fmt.Println()
}

func separator(w int) {
	n := w - 2
	if n < 10 {
		n = 10
	}
	fmt.Printf(" %s%s%s\n", dim, strings.Repeat("─", n), reset)
}

func printLine(w int, content string) {
	fmt.Printf("%s\n", content)
}

// baseNames returns base filenames, truncated to max count.
func baseNames(files []string, max int) []string {
	var result []string
	for i, f := range files {
		if i >= max {
			result = append(result, fmt.Sprintf("+%d", len(files)-max))
			break
		}
		result = append(result, filepath.Base(f))
	}
	return result
}

// wordWrap breaks text into lines that fit within width.
func wordWrap(text string, width int) []string {
	if width < 10 {
		width = 10
	}
	// Replace newlines with spaces for wrapping.
	text = strings.ReplaceAll(text, "\n", " ")

	var lines []string
	for len(text) > 0 {
		if len(text) <= width {
			lines = append(lines, text)
			break
		}
		// Find last space within width.
		cut := width
		for cut > 0 && text[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			// No space found — hard break.
			cut = width
		}
		lines = append(lines, text[:cut])
		text = strings.TrimLeft(text[cut:], " ")
	}
	return lines
}

// wrapPrint prints a line, word-wrapping if it exceeds pane width.
func wrapPrint(w int, text string, indent int) {
	// Strip ANSI for length calculation — approximate by just printing.
	fmt.Println(text)
}

// WriteLog appends a plain-text summary to the log file.
func WriteLog(logPath string, state DisplayState) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(f, "─── %s (session: %s) ───\n", ts, state.Elapsed)

	if state.CCStatus != "" {
		fmt.Fprintf(f, "  cc: %s\n", state.CCStatus)
	}
	if len(state.ChangedFiles) > 0 {
		fmt.Fprintf(f, "  modified: %s\n", strings.Join(state.ChangedFiles, ", "))
	}
	if len(state.UntrackedFiles) > 0 {
		fmt.Fprintf(f, "  new: %s\n", strings.Join(state.UntrackedFiles, ", "))
	}
	if state.Build != nil {
		if state.Build.OK {
			fmt.Fprintf(f, "  build: clean\n")
		} else {
			trend := ""
			if state.Build.Trend != "" {
				trend = " (" + state.Build.Trend + ")"
			}
			fmt.Fprintf(f, "  build: %d errors%s\n", state.Build.ErrorCount, trend)
		}
	}
	for _, finding := range state.TrajectoryFindings {
		fmt.Fprintf(f, "  ▸ %s\n", finding.Message)
	}
	for _, dt := range state.DeletedTests {
		fmt.Fprintf(f, "  ▸ deleted test: %s\n", dt)
	}
	for _, bf := range state.BrainFindings {
		fmt.Fprintf(f, "  brain [%s] %s\n", bf.Status, bf.Reasoning)
		if bf.Nudge != "" {
			fmt.Fprintf(f, "    → %s\n", bf.Nudge)
		}
	}
	fmt.Fprintf(f, "\n")
}
