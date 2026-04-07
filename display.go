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
	BrainLastTime      time.Time
	CCStatus           string
}

// BuildDisplay carries build result info for rendering.
type BuildDisplay struct {
	OK         bool
	ErrorCount int
	Trend      string
}

// --- Chat-style log rendering ---
// No screen clearing. No alt screen. Just append timestamped lines.
// The display acts like `tail -f` — new events at bottom, scroll up for history.

// LogEvent prints a single timestamped event line to stdout.
func LogEvent(format string, args ...interface{}) {
	ts := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s%s%s %s\n", dim, ts, reset, msg)
}

// LogBrain prints a brain status/assessment line.
func LogBrain(msg string) {
	ts := time.Now().Format("15:04:05")
	fmt.Printf("%s%s%s %s•%s %s\n", dim, ts, reset, dim, reset, msg)
}

// LogNudge prints a nudge finding — conversational, prominent.
func LogNudge(f BrainFinding) {
	ts := f.Timestamp.Format("15:04:05")
	severity := fmt.Sprintf("%s⚠%s", yellow, reset)
	if f.Severity == "error" {
		severity = fmt.Sprintf("%s✗%s", red, reset)
	}

	// Nudge as the main message (conversational).
	fmt.Printf("%s%s%s %s %s\n", dim, ts, reset, severity, f.Nudge)

	// Reasoning as context underneath (dim, indented).
	if f.Reasoning != "" {
		for _, line := range strings.Split(f.Reasoning, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				fmt.Printf("         %s%s%s\n", dim, line, reset)
			}
		}
	}
}

// LogResolved prints when a finding is resolved.
func LogResolved(f BrainFinding) {
	ts := time.Now().Format("15:04:05")
	fmt.Printf("%s%s%s %s✓%s %s%s%s\n", dim, ts, reset, green, reset, dim, f.Nudge, reset)
}

// LogStatus prints a compact status line (CC + build + files).
func LogStatus(state DisplayState) {
	parts := []string{}

	// CC status.
	switch state.CCStatus {
	case "active":
		parts = append(parts, fmt.Sprintf("%s●%s cc", green, reset))
	case "thinking":
		parts = append(parts, fmt.Sprintf("%s●%s cc:thinking", yellow, reset))
	default:
		parts = append(parts, fmt.Sprintf("%s○%s cc:idle", dim, reset))
	}

	// Build.
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

	// Files.
	nMod := len(state.ChangedFiles)
	nNew := len(state.UntrackedFiles)
	if nMod > 0 {
		names := baseNames(state.ChangedFiles, 3)
		parts = append(parts, fmt.Sprintf("%d mod: %s", nMod, strings.Join(names, " ")))
	}
	if nNew > 0 {
		names := baseNames(state.UntrackedFiles, 2)
		parts = append(parts, fmt.Sprintf("%d new: %s", nNew, strings.Join(names, " ")))
	}

	ts := time.Now().Format("15:04:05")
	fmt.Printf("%s%s%s %s\n", dim, ts, reset, strings.Join(parts, "  "))
}

// LogTrajectory prints trajectory warnings.
func LogTrajectory(f Finding) {
	ts := time.Now().Format("15:04:05")
	fmt.Printf("%s%s%s %s▸%s %s\n", dim, ts, reset, yellow, reset, f.Message)
}

// LogHeader prints the startup banner.
func LogHeader(projectDir string, cfg Config) {
	fmt.Printf("\n %strupal%s %s— watching %s%s\n", bold, reset, dim, filepath.Base(projectDir), reset)
	if cfg.BuildCmd != "" {
		fmt.Printf(" %sbuild: %s%s\n", dim, cfg.BuildCmd, reset)
	}
	fmt.Printf(" %sbrain: %s/%s (effort: %s)%s\n", dim, cfg.BrainProvider, cfg.BrainModel, cfg.BrainEffort, reset)
	fmt.Printf(" %s%s%s\n", dim, strings.Repeat("─", 50), reset)
	fmt.Println()
}

// LogStopped prints the stop summary.
func LogStopped(elapsed string, findings []BrainFinding) {
	fmt.Println()
	fmt.Printf(" %strupal stopped%s  %s%s%s\n", bold, reset, dim, elapsed, reset)

	active := 0
	resolved := 0
	for _, f := range findings {
		if f.Status == "shown" {
			active++
		} else if f.Status == "resolved" {
			resolved++
		}
	}

	if len(findings) > 0 {
		fmt.Printf(" %sfindings: %d active, %d resolved%s\n", dim, active, resolved, reset)
		for _, f := range findings {
			status := fmt.Sprintf("%s●%s", yellow, reset)
			if f.Status == "resolved" {
				status = fmt.Sprintf("%s✓%s", green, reset)
			}
			ts := f.Timestamp.Format("15:04")
			fmt.Printf(" %s %s%s%s %s\n", status, dim, ts, reset, f.Nudge)
		}
	} else {
		fmt.Printf(" %sno findings this session%s\n", dim, reset)
	}

	fmt.Printf("\n %slog: .trupal.log  debug: .trupal.debug%s\n", dim, reset)
	fmt.Printf(" %spress ctrl+c to close pane%s\n", dim, reset)
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

// WriteLog appends a plain-text summary to the log file.
func WriteLog(logPath string, state DisplayState) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(f, "%s cc:%s", ts, state.CCStatus)
	if state.Build != nil {
		if state.Build.OK {
			fmt.Fprintf(f, " build:clean")
		} else {
			fmt.Fprintf(f, " build:%d-err", state.Build.ErrorCount)
		}
	}
	if len(state.ChangedFiles) > 0 {
		fmt.Fprintf(f, " mod:%s", strings.Join(state.ChangedFiles, ","))
	}
	for _, bf := range state.BrainFindings {
		if bf.Status == "shown" {
			fmt.Fprintf(f, "\n  ⚠ %s", bf.Nudge)
		}
	}
	fmt.Fprintf(f, "\n")
}
