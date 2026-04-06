package main

import (
	"fmt"
	"os"
	"hash/fnv"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runWatchLoop is the main polling loop. It creates a Session, prints startup
// info, waits 3 seconds, then loops forever: poll → render → sleep.
func runWatchLoop(projectDir string, cfg Config) {
	session := NewSession(projectDir)
	logPath := filepath.Join(projectDir, ".trupal.log")
	var lastLogHash uint64

	// Truncate log on start.
	os.WriteFile(logPath, nil, 0644)

	fmt.Printf("trupal watching: %s\n", shortenPath(projectDir))
	if cfg.BuildCmd != "" {
		fmt.Printf("build command: %s\n", cfg.BuildCmd)
		if len(cfg.BuildExtensions) > 0 {
			fmt.Printf("build extensions: %s\n", strings.Join(cfg.BuildExtensions, ", "))
		}
	}
	fmt.Printf("poll interval: %ds\n", cfg.PollInterval)

	time.Sleep(3 * time.Second)

	for {
		state := pollCycle(session, projectDir, cfg)
		Render(state)

		// Only log when findings change (dedup identical cycles).
		h := stateHash(state)
		if h != lastLogHash {
			WriteLog(logPath, state)
			lastLogHash = h
		}

		time.Sleep(time.Duration(cfg.PollInterval) * time.Second)
	}
}

// pollCycle runs one watch cycle and returns a DisplayState ready for rendering.
func pollCycle(session *Session, projectDir string, cfg Config) DisplayState {
	changedFiles := gitDiffNameOnly(projectDir)
	rawDiff := gitDiff(projectDir)
	nameStatus := gitDiffNameStatus(projectDir)

	fileDiffs := splitDiffByFile(rawDiff)
	session.UpdateFileEdits(fileDiffs)

	var buildDisplay *BuildDisplay
	if cfg.BuildCmd != "" && len(changedFiles) > 0 && ShouldRunBuild(changedFiles, cfg.BuildExtensions) {
		result := RunBuildCheck(projectDir, cfg.BuildCmd)
		session.AppendErrorCount(result.ErrorCount)

		trend := buildTrend(session.ErrorHistory, result.OK)
		buildDisplay = &BuildDisplay{
			OK:         result.OK,
			ErrorCount: result.ErrorCount,
			Trend:      trend,
		}
	}

	patternFindings := ScanDiffPatterns(rawDiff)
	deletedTests := ScanDeletedTests(nameStatus)
	trajectoryFindings := session.EvalTrajectory()

	untrackedFiles := gitUntrackedFiles(projectDir)

	return DisplayState{
		ProjectDir:         shortenPath(projectDir),
		Elapsed:            session.Elapsed(),
		ChangedFiles:       changedFiles,
		UntrackedFiles:     untrackedFiles,
		Build:              buildDisplay,
		TrajectoryFindings: trajectoryFindings,
		PatternFindings:    patternFindings,
		DeletedTests:       deletedTests,
	}
}

// buildTrend computes the Trend string for BuildDisplay from the error history.
// It is called after the current cycle's error count has been appended, so the
// last entry is the current count.
func buildTrend(history []int, buildOK bool) string {
	if buildOK || len(history) < 2 {
		return ""
	}

	curr := history[len(history)-1]
	prev := history[len(history)-2]

	// fix-then-break: previously clean, now broken.
	if prev == 0 && curr > 0 {
		return "was clean"
	}

	// Progress: error count decreased.
	if prev > curr {
		return fmt.Sprintf("was %d", prev)
	}

	// Regression: error count increased from a non-zero baseline.
	if prev > 0 && curr > prev {
		return fmt.Sprintf("was %d", prev)
	}

	// Stall: same non-zero count for several cycles.
	if prev == curr && curr > 0 {
		stallLen := 0
		for i := len(history) - 1; i >= 0; i-- {
			if history[i] == curr {
				stallLen++
			} else {
				break
			}
		}
		if stallLen >= 3 {
			return fmt.Sprintf("stalled x%d", stallLen)
		}
	}

	return ""
}

// stateHash returns a hash of the meaningful display state (excluding elapsed time).
// Used to deduplicate log entries.
func stateHash(state DisplayState) uint64 {
	h := fnv.New64a()
	for _, f := range state.ChangedFiles {
		h.Write([]byte(f))
	}
	for _, f := range state.UntrackedFiles {
		h.Write([]byte(f))
	}
	if state.Build != nil {
		if state.Build.OK {
			h.Write([]byte("ok"))
		} else {
			fmt.Fprintf(h, "%d%s", state.Build.ErrorCount, state.Build.Trend)
		}
	}
	for _, f := range state.TrajectoryFindings {
		h.Write([]byte(f.Message))
	}
	for _, f := range state.PatternFindings {
		fmt.Fprintf(h, "%s%d%s", f.File, f.Line, f.Pattern)
	}
	for _, f := range state.DeletedTests {
		h.Write([]byte(f))
	}
	return h.Sum64()
}

// splitDiffByFile splits a unified diff into per-file chunks keyed by filename.
// File boundaries are detected by "diff --git" headers; the filename is
// extracted from "+++ b/<path>" lines.
func splitDiffByFile(rawDiff string) map[string]string {
	result := make(map[string]string)
	if rawDiff == "" {
		return result
	}

	lines := strings.Split(rawDiff, "\n")
	var currentFile string
	var buf strings.Builder

	flush := func() {
		if currentFile != "" && buf.Len() > 0 {
			result[currentFile] = buf.String()
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			currentFile = ""
			buf.Reset()
		}

		if strings.HasPrefix(line, "+++ b/") {
			currentFile = strings.TrimPrefix(line, "+++ b/")
		}

		if currentFile != "" || strings.HasPrefix(line, "diff --git ") {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	flush()

	return result
}

// gitDiffNameOnly returns the list of changed files relative to HEAD using
// "git diff --name-only HEAD".
func gitDiffNameOnly(projectDir string) []string {
	out := runGit(projectDir, "diff", "--name-only", "HEAD")
	if out == "" {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

// gitUntrackedFiles returns untracked files (not in .gitignore), excluding trupal's own files.
func gitUntrackedFiles(projectDir string) []string {
	out := runGit(projectDir, "ls-files", "--others", "--exclude-standard")
	if out == "" {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(filepath.Base(line), ".trupal.") {
			continue
		}
		files = append(files, line)
	}
	return files
}

// gitDiffNameStatus returns the output of "git diff --name-status HEAD".
func gitDiffNameStatus(projectDir string) string {
	return runGit(projectDir, "diff", "--name-status", "HEAD")
}

// gitDiff returns the full unified diff output of "git diff HEAD".
func gitDiff(projectDir string) string {
	return runGit(projectDir, "diff", "HEAD")
}

// runGit executes a git command in projectDir and returns its combined output.
// On error, an empty string is returned silently.
func runGit(projectDir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// shortenPath replaces the user's home directory prefix with "~".
func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
