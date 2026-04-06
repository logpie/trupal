package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	// Brain
	BrainFindings []BrainFinding
	BrainThinking bool
	CCStatus      string // "active", "thinking", "idle"
}

// BuildDisplay carries build result info for rendering.
type BuildDisplay struct {
	OK         bool
	ErrorCount int
	Trend      string
}

// Render clears the screen and draws a full status frame.
func Render(state DisplayState) {
	fmt.Print("\033[2J\033[H")

	// Header.
	fmt.Printf("%s─── trupal ───%s\n", dim, reset)
	status := ""
	if state.CCStatus != "" {
		status = " cc:" + state.CCStatus
	}
	fmt.Printf("%s%s | %s%s%s\n", dim, filepath.Base(state.ProjectDir), state.Elapsed, status, reset)
	fmt.Println()

	// --- Status section ---
	hasStatus := len(state.ChangedFiles) > 0 ||
		len(state.UntrackedFiles) > 0 ||
		state.Build != nil ||
		len(state.TrajectoryFindings) > 0 ||
		len(state.DeletedTests) > 0

	if !hasStatus && len(state.BrainFindings) == 0 && !state.BrainThinking {
		fmt.Printf("  %swatching...%s\n", dim, reset)
	} else {
		if len(state.ChangedFiles) > 0 {
			fmt.Printf("  %smodified:%s\n", dim, reset)
			limit := 6
			for i, f := range state.ChangedFiles {
				if i >= limit {
					fmt.Printf("  %s+%d more%s\n", dim, len(state.ChangedFiles)-limit, reset)
					break
				}
				fmt.Printf("  %s· %s%s\n", dim, filepath.Base(f), reset)
			}
		}

		if len(state.UntrackedFiles) > 0 {
			fmt.Printf("  %snew:%s\n", dim, reset)
			limit := 4
			for i, f := range state.UntrackedFiles {
				if i >= limit {
					fmt.Printf("  %s+%d more%s\n", dim, len(state.UntrackedFiles)-limit, reset)
					break
				}
				fmt.Printf("  %s· %s%s\n", dim, filepath.Base(f), reset)
			}
		}

		if state.Build != nil {
			renderBuild(state.Build)
		}

		for _, f := range state.TrajectoryFindings {
			fmt.Printf("  %s▸ %s%s\n", yellow, f.Message, reset)
		}
		for _, dt := range state.DeletedTests {
			fmt.Printf("  %s▸ deleted %s%s\n", yellow, filepath.Base(dt), reset)
		}
	}

	// --- Brain section ---
	if len(state.BrainFindings) > 0 || state.BrainThinking {
		fmt.Println()
		fmt.Printf("%s─── brain ────%s\n", dim, reset)

		if state.BrainThinking {
			fmt.Printf("  %sthinking...%s\n", dim, reset)
		}

		for _, f := range state.BrainFindings {
			renderBrainFinding(f)
		}
	}

	fmt.Println()
	fmt.Printf("%s──────────────%s\n", dim, reset)
}

func renderBuild(b *BuildDisplay) {
	if b.OK {
		fmt.Printf("  %s✓ build clean%s\n", green, reset)
		return
	}
	label := fmt.Sprintf("%d errors", b.ErrorCount)
	if b.Trend != "" {
		label += " (" + b.Trend + ")"
	}
	fmt.Printf("  %s✗ %s%s\n", red, label, reset)
}

func renderBrainFinding(f BrainFinding) {
	ts := f.Timestamp.Format("15:04")
	statusTag := ""
	if f.Status == "resolved" {
		statusTag = " [resolved]"
	} else if f.Status == "waived" {
		statusTag = " [waived]"
	}

	// Reasoning in dim.
	if f.Reasoning != "" {
		lines := strings.Split(f.Reasoning, "\n")
		for i, line := range lines {
			if i == 0 {
				fmt.Printf("  %s%s%s %s%s\n", dim, ts, statusTag, line, reset)
			} else {
				fmt.Printf("  %s      %s%s\n", dim, line, reset)
			}
		}
	}

	// Nudge in yellow.
	if f.Nudge != "" {
		color := yellow
		if f.Status == "resolved" || f.Status == "waived" {
			color = dim
		}
		fmt.Printf("  %s  → %s%s\n", color, f.Nudge, reset)
	}
}

// WriteLog appends a plain-text summary to the log file.
func WriteLog(logPath string, state DisplayState) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(f, "--- %s (session: %s) ---\n", ts, state.Elapsed)

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
		fmt.Fprintf(f, "  ! %s\n", finding.Message)
	}
	for _, dt := range state.DeletedTests {
		fmt.Fprintf(f, "  ! deleted test: %s\n", dt)
	}
	for _, bf := range state.BrainFindings {
		fmt.Fprintf(f, "  brain [%s]: %s\n", bf.Status, bf.Reasoning)
		if bf.Nudge != "" {
			fmt.Fprintf(f, "    → %s\n", bf.Nudge)
		}
	}

	fmt.Fprintf(f, "\n")
}
