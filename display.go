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
	Build              *BuildDisplay // nil = no build configured or not run this cycle
	TrajectoryFindings []Finding
	PatternFindings    []PatternFinding
	DeletedTests       []string
}

// BuildDisplay carries build result info for rendering.
type BuildDisplay struct {
	OK         bool
	ErrorCount int
	Trend      string // e.g. "was 5", "stalled x4", "" (first run)
}

// Render clears the screen and draws a full status frame.
func Render(state DisplayState) {
	// Clear screen and move cursor to top-left.
	fmt.Print("\033[2J\033[H")

	// Header — keep it short for narrow panes.
	fmt.Printf("%s─── trupal ───%s\n", dim, reset)
	fmt.Printf("%s%s | %s%s\n", dim, filepath.Base(state.ProjectDir), state.Elapsed, reset)
	fmt.Println()

	hasContent := len(state.ChangedFiles) > 0 ||
		len(state.UntrackedFiles) > 0 ||
		state.Build != nil ||
		len(state.TrajectoryFindings) > 0 ||
		len(state.PatternFindings) > 0 ||
		len(state.DeletedTests) > 0

	if !hasContent {
		fmt.Printf("  %swatching...%s\n", dim, reset)
	} else {
		// Changed files — one per line, basename only to fit narrow pane.
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

		// Untracked files.
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

		fmt.Println()

		// Build status.
		if state.Build != nil {
			renderBuild(state.Build)
		}

		// Findings — these are the important part.
		for _, f := range state.TrajectoryFindings {
			fmt.Printf("  %s▸ %s%s\n", yellow, f.Message, reset)
		}
		for _, pf := range state.PatternFindings {
			fmt.Printf("  %s▸ %s%s\n", yellow, formatPatternFinding(pf), reset)
		}
		for _, dt := range state.DeletedTests {
			fmt.Printf("  %s▸ deleted %s%s\n", yellow, filepath.Base(dt), reset)
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

func formatPatternFinding(pf PatternFinding) string {
	base := filepath.Base(pf.File)
	return fmt.Sprintf("%s:%d %s", base, pf.Line, pf.Pattern)
}

// WriteLog appends a plain-text (no ANSI) summary of the display state to the log file.
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
	for _, pf := range state.PatternFindings {
		fmt.Fprintf(f, "  ! %s:%d %s\n", pf.File, pf.Line, pf.Pattern)
	}
	for _, dt := range state.DeletedTests {
		fmt.Fprintf(f, "  ! deleted test: %s\n", dt)
	}

	fmt.Fprintf(f, "\n")
}
