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

type BuildDisplay struct {
	OK         bool
	ErrorCount int
	Trend      string
}

// --- Simple chat log: print timestamped lines, nothing fancy ---

func ts() string {
	return fmt.Sprintf("%s%s%s", dim, time.Now().Format("15:04"), reset)
}

func LogHeader(projectDir string, cfg Config) {
	fmt.Printf("\n %strupal%s %s— %s%s\n", bold, reset, dim, filepath.Base(projectDir), reset)
	fmt.Printf(" %sbrain: %s/%s%s\n", dim, cfg.BrainProvider, cfg.BrainModel, reset)
	fmt.Printf(" %s%s%s\n\n", dim, strings.Repeat("─", 40), reset)
}

func LogEvent(format string, args ...interface{}) {
	fmt.Printf("%s %s\n", ts(), fmt.Sprintf(format, args...))
}

func LogNudge(f BrainFinding) {
	icon := fmt.Sprintf("%s⚠%s", yellow, reset)
	if f.Severity == "error" {
		icon = fmt.Sprintf("%s✗%s", red, reset)
	}
	fmt.Printf("\n%s %s %s\n", ts(), icon, f.Nudge)
	if f.Reasoning != "" {
		for _, line := range strings.Split(f.Reasoning, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				fmt.Printf("       %s%s%s\n", dim, line, reset)
			}
		}
	}
	fmt.Println()
}

func LogResolved(f BrainFinding) {
	fmt.Printf("%s %s✓%s %s%s%s\n", ts(), green, reset, dim, f.Nudge, reset)
}

func LogTrajectory(f Finding) {
	fmt.Printf("%s %s▸%s %s\n", ts(), yellow, reset, f.Message)
}

func LogStopped(elapsed string, findings []BrainFinding) {
	fmt.Printf("\n %strupal stopped%s %s(%s)%s\n", bold, reset, dim, elapsed, reset)
	active, resolved := 0, 0
	for _, f := range findings {
		if f.Status == "shown" {
			active++
		} else if f.Status == "resolved" {
			resolved++
		}
	}
	if len(findings) > 0 {
		fmt.Printf(" %s%d findings (%d active, %d resolved)%s\n", dim, len(findings), active, resolved, reset)
		for _, f := range findings {
			icon := fmt.Sprintf("%s●%s", yellow, reset)
			if f.Status == "resolved" {
				icon = fmt.Sprintf("%s✓%s", green, reset)
			}
			fmt.Printf(" %s %s\n", icon, f.Nudge)
		}
	} else {
		fmt.Printf(" %sno findings%s\n", dim, reset)
	}
	fmt.Printf(" %slog: .trupal.log%s\n", dim, reset)
	fmt.Printf(" %spress ctrl+c to close%s\n", dim, reset)
}

// Heartbeat and ClearHeartbeat are no-ops — we only print when something happens.
func Heartbeat(ccStatus string, brainThinking bool, brainLastTime time.Time, elapsed string) {}
func ClearHeartbeat() {}

// LogStatus prints when files/build change.
func LogStatus(state DisplayState) {
	parts := []string{filepath.Base(state.ProjectDir)}
	if state.Build != nil {
		if state.Build.OK {
			parts = append(parts, fmt.Sprintf("%s✓%s", green, reset))
		} else {
			label := fmt.Sprintf("%s✗ %d err%s", red, state.Build.ErrorCount, reset)
			if state.Build.Trend != "" {
				label += " (" + state.Build.Trend + ")"
			}
			parts = append(parts, label)
		}
	}
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
	fmt.Printf("%s %s%s%s\n", ts(), dim, strings.Join(parts, "  "), reset)
}

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

func WriteLog(logPath string, state DisplayState) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	t := time.Now().Format("15:04:05")
	fmt.Fprintf(f, "%s cc:%s", t, state.CCStatus)
	if state.Build != nil {
		if state.Build.OK {
			fmt.Fprintf(f, " build:ok")
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
