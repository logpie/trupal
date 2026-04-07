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
	BrainLastMsg       string
	BrainLastTime      time.Time // when brain last responded
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

// EnterAltScreen switches to the alternate screen buffer (no scrollback).
func EnterAltScreen() {
	fmt.Print("\033[?1049h")
}

// LeaveAltScreen switches back to the normal screen buffer.
func LeaveAltScreen() {
	fmt.Print("\033[?1049l")
}

// Render redraws the status frame in place on the alternate screen.
func Render(state DisplayState) {
	fmt.Print("\033[H\033[2J")
	w := getPaneWidth()
	sep := strings.Repeat("─", w-2)

	// ── Header: always shows trupal is alive ──
	now := time.Now().Format("15:04:05")
	fmt.Printf(" %strupal%s %s%s%s\n", bold, reset, dim, now, reset)
	fmt.Printf(" %s%s%s\n", dim, sep, reset)

	// ── Status line: CC + brain + build ──
	ccLabel := ccStatusLabel(state.CCStatus)
	brainLabel := brainStatusLabel(state.BrainThinking, state.BrainLastTime)
	fmt.Printf(" %s  %s\n", ccLabel, brainLabel)

	if state.Build != nil {
		renderBuild(state.Build)
	}

	// ── Files ──
	nMod := len(state.ChangedFiles)
	nNew := len(state.UntrackedFiles)
	if nMod > 0 || nNew > 0 {
		parts := []string{}
		if nMod > 0 {
			names := baseNames(state.ChangedFiles, 4)
			parts = append(parts, fmt.Sprintf("%d mod: %s", nMod, strings.Join(names, " ")))
		}
		if nNew > 0 {
			names := baseNames(state.UntrackedFiles, 3)
			parts = append(parts, fmt.Sprintf("%d new: %s", nNew, strings.Join(names, " ")))
		}
		for _, p := range parts {
			fmt.Printf(" %s%s%s\n", dim, p, reset)
		}
	}

	// ── Trajectory ──
	for _, f := range state.TrajectoryFindings {
		fmt.Printf(" %s▸ %s%s\n", yellow, f.Message, reset)
	}
	for _, dt := range state.DeletedTests {
		fmt.Printf(" %s▸ deleted %s%s\n", yellow, filepath.Base(dt), reset)
	}

	// ── Brain section ──
	// Always show: active findings persist until resolved, plus brain's latest status.
	activeFindings := 0
	resolvedFindings := 0
	for _, f := range state.BrainFindings {
		if f.Status == "shown" {
			activeFindings++
		} else {
			resolvedFindings++
		}
	}

	hasBrainContent := len(state.BrainFindings) > 0 || state.BrainLastMsg != ""
	if hasBrainContent {
		fmt.Println()
		fmt.Printf(" %s%s%s\n", dim, sep, reset)

		// Active findings first (yellow — needs attention).
		for _, f := range state.BrainFindings {
			if f.Status == "shown" {
				renderFinding(f, w)
			}
		}

		// Resolved findings (dimmed — addressed).
		if resolvedFindings > 0 {
			for _, f := range state.BrainFindings {
				if f.Status == "resolved" {
					renderFinding(f, w)
				}
			}
		}

		// Brain's latest assessment (dim, below findings).
		if state.BrainLastMsg != "" {
			fmt.Println()
			lines := wordWrap("brain: "+state.BrainLastMsg, w-2)
			for _, line := range lines {
				fmt.Printf(" %s%s%s\n", dim, line, reset)
			}
		}
	}

	// ── Footer: session duration ──
	fmt.Println()
	fmt.Printf(" %ssession: %s%s\n", dim, state.Elapsed, reset)
}

func ccStatusLabel(status string) string {
	switch status {
	case "active":
		return fmt.Sprintf("%s●%s cc:active", green, reset)
	case "thinking":
		return fmt.Sprintf("%s●%s cc:thinking", yellow, reset)
	default:
		return fmt.Sprintf("%s○%s cc:idle", dim, reset)
	}
}

func brainStatusLabel(thinking bool, lastTime time.Time) string {
	if thinking {
		return fmt.Sprintf("%s◌%s brain:analyzing", cyan, reset)
	}
	if lastTime.IsZero() {
		return fmt.Sprintf("%s○%s brain:starting", dim, reset)
	}
	ago := time.Since(lastTime).Truncate(time.Second)
	return fmt.Sprintf("%s●%s brain:%s ago", dim, reset, ago)
}

func renderBuild(b *BuildDisplay) {
	if b.OK {
		fmt.Printf(" %s✓ build clean%s\n", green, reset)
		return
	}
	label := fmt.Sprintf("%d errors", b.ErrorCount)
	if b.Trend != "" {
		label += " (" + b.Trend + ")"
	}
	fmt.Printf(" %s✗ %s%s\n", red, label, reset)
}

func renderFinding(f BrainFinding, w int) {
	ts := f.Timestamp.Format("15:04")

	statusTag := ""
	if f.Status == "resolved" {
		statusTag = " ✓"
	}

	// Reasoning in dim.
	if f.Reasoning != "" {
		lines := wordWrap(f.Reasoning, w-len(ts)-4)
		for i, line := range lines {
			if i == 0 {
				fmt.Printf(" %s%s%s%s %s%s\n", dim, ts, statusTag, reset, dim+line, reset)
			} else {
				fmt.Printf(" %s%s%s\n", dim, strings.Repeat(" ", len(ts)+2)+line, reset)
			}
		}
	}

	// Nudge in yellow.
	if f.Nudge != "" {
		color := yellow
		if f.Status == "resolved" {
			color = dim
		}
		lines := wordWrap("→ "+f.Nudge, w-4)
		for _, line := range lines {
			fmt.Printf("   %s%s%s\n", color, line, reset)
		}
	}
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
	text = strings.ReplaceAll(text, "\n", " ")

	var lines []string
	for len(text) > 0 {
		if len(text) <= width {
			lines = append(lines, text)
			break
		}
		cut := width
		for cut > 0 && text[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = width
		}
		lines = append(lines, text[:cut])
		text = strings.TrimLeft(text[cut:], " ")
	}
	return lines
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
