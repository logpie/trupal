package main

import (
	"fmt"
	"strings"
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

	// Header.
	fmt.Printf("%s--- TruPal ---%s\n", dim, reset)
	fmt.Printf("%swatching: %s%s\n", dim, state.ProjectDir, reset)
	fmt.Printf("%ssession: %s%s\n", dim, state.Elapsed, reset)
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
		// Changed files.
		if len(state.ChangedFiles) > 0 {
			limit := 8
			shown := state.ChangedFiles
			if len(shown) > limit {
				extra := len(shown) - limit
				shown = shown[:limit]
				fmt.Printf("  %schanged: %s +%d more%s\n", dim, strings.Join(shown, ", "), extra, reset)
			} else {
				fmt.Printf("  %schanged: %s%s\n", dim, strings.Join(shown, ", "), reset)
			}
			fmt.Println()
		}

		// Untracked files.
		if len(state.UntrackedFiles) > 0 {
			limit := 5
			shown := state.UntrackedFiles
			if len(shown) > limit {
				extra := len(shown) - limit
				shown = shown[:limit]
				fmt.Printf("  %snew: %s +%d more%s\n", dim, strings.Join(shown, ", "), extra, reset)
			} else {
				fmt.Printf("  %snew: %s%s\n", dim, strings.Join(shown, ", "), reset)
			}
			fmt.Println()
		}

		// Build status.
		if state.Build != nil {
			fmt.Print("  build: ")
			renderBuild(state.Build)
			fmt.Println()
			fmt.Println()
		}

		// Trajectory findings.
		for _, f := range state.TrajectoryFindings {
			fmt.Printf("  %s! %s%s\n", yellow, f.Message, reset)
		}

		// Pattern findings.
		for _, pf := range state.PatternFindings {
			fmt.Printf("  %s! %s (%s:+%d)%s\n", yellow, pf.Pattern, pf.File, pf.Line, reset)
		}

		// Deleted tests.
		for _, dt := range state.DeletedTests {
			fmt.Printf("  %s! deleted %s%s\n", yellow, dt, reset)
		}
	}

	fmt.Println()
	// Footer.
	fmt.Printf("  %slast check: just now%s\n", dim, reset)
	fmt.Printf("%s%s%s\n", dim, strings.Repeat("-", 40), reset)
}

// renderBuild prints the build status inline (no trailing newline).
func renderBuild(b *BuildDisplay) {
	if b.OK {
		fmt.Printf("%s%sclean%s", green, bold, reset)
		return
	}
	label := fmt.Sprintf("%d errors", b.ErrorCount)
	if b.Trend != "" {
		label = fmt.Sprintf("%s (%s)", label, b.Trend)
	}
	fmt.Printf("%s%s%s", red, label, reset)
}
