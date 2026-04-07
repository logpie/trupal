package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

func runWatchLoop(projectDir string, cfg Config) {
	InitDebugLog(projectDir)
	defer CloseDebugLog()

	// Handle SIGINT for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	session := NewSession(projectDir)
	findings := NewFindingStore()
	logPath := filepath.Join(projectDir, ".trupal.log")
	var lastLogHash uint64

	os.WriteFile(logPath, nil, 0644)

	// Find CC's session JSONL.
	jsonlPath := FindSessionJSONL(projectDir)
	Debugf("[watcher] JSONL path: %q", jsonlPath)
	if jsonlPath == "" {
		fmt.Printf("trupal watching: %s\n", shortenPath(projectDir))
		fmt.Println("no CC session found — watching for new sessions...")
	} else {
		fmt.Printf("trupal watching: %s\n", shortenPath(projectDir))
		fmt.Printf("CC session: %s\n", filepath.Base(jsonlPath))
	}

	if cfg.BuildCmd != "" {
		fmt.Printf("build: %s\n", cfg.BuildCmd)
	}
	fmt.Printf("brain: %s/%s (effort: %s)\n", cfg.BrainProvider, cfg.BrainModel, cfg.BrainEffort)
	fmt.Println("starting in 3s...")
	time.Sleep(3 * time.Second)

	// Start JSONL watcher.
	var jsonlWatcher *JSONLWatcher
	if jsonlPath != "" {
		var err error
		jsonlWatcher, err = NewJSONLWatcher(jsonlPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not watch JSONL: %v\n", err)
		}
	}

	// Start brain.
	var brain *Brain
	if jsonlPath != "" {
		var err error
		brain, err = StartBrain(cfg, projectDir, jsonlPath, findings.ActiveJSON())
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not start brain: %v\n", err)
		}
	}

	// Debounce channel: AfterFunc sends the reason here; loop reads it next iteration.
	debounceCh := make(chan string, 1)
	var debounceTimer *time.Timer

	brainBusy := false
	brainThinking := false
	brainLastMsg := ""
	interval := time.Duration(cfg.PollInterval) * time.Second

	// Main loop ticker.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Idle detection.
	lastJSONLActivity := time.Now()
	idleNotified := false

	for {
		triggerBrain := false
		triggerReason := ""

		// Drain any pending debounce signal.
		select {
		case reason := <-debounceCh:
			if !brainBusy {
				triggerBrain = true
				triggerReason = reason
			}
		default:
		}

		// Check for JSONL events (non-blocking).
		if jsonlWatcher != nil {
			select {
			case <-jsonlWatcher.Events:
				lastJSONLActivity = time.Now()
				idleNotified = false

				entries := jsonlWatcher.ReadNew()
				Debugf("[watcher] JSONL event: %d new entries", len(entries))
				significant := false
				var summary []string
				for _, e := range entries {
					if e.Type == "assistant" && e.HasText {
						significant = true
						summary = append(summary, "CC sent text response")
					}
					if e.Type == "assistant" && e.HasTool {
						summary = append(summary, "CC used a tool")
					}
					if e.Type == "user" && e.Role == "user" {
						significant = true
					}
				}

				if significant && !brainBusy {
					reason := "CC session updated"
					if len(summary) > 0 {
						reason = strings.Join(summary, "; ")
					}
					// Debounce: wait 2s for burst to settle.
					if debounceTimer != nil {
						debounceTimer.Stop()
					}
					debounceTimer = time.AfterFunc(2*time.Second, func() {
						select {
						case debounceCh <- reason:
						default:
						}
					})
				}
			default:
				// No JSONL events.
			}
		}

		// Check for idle (60s since last JSONL activity).
		if jsonlWatcher != nil && !idleNotified && time.Since(lastJSONLActivity) > 60*time.Second {
			if !brainBusy {
				triggerBrain = true
				triggerReason = "CC has been idle for 60s — good time for a session review"
			}
			idleNotified = true
		}

		// Git poll cycle.
		changedFiles := gitDiffNameOnly(projectDir)
		rawDiff := gitDiff(projectDir)
		nameStatus := gitDiffNameStatus(projectDir)
		untrackedFiles := gitUntrackedFiles(projectDir)

		fileDiffs := splitDiffByFile(rawDiff)
		session.UpdateFileEdits(fileDiffs)

		// Build check.
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

			// Build status changed → trigger brain.
			if !brainBusy && len(session.ErrorHistory) >= 2 {
				prev := session.ErrorHistory[len(session.ErrorHistory)-2]
				curr := result.ErrorCount
				if (prev == 0 && curr > 0) || (prev > 0 && curr == 0) {
					triggerBrain = true
					triggerReason = fmt.Sprintf("build status changed: %d → %d errors", prev, curr)
				}
			}
		}

		deletedTests := ScanDeletedTests(nameStatus)
		trajectoryFindings := session.EvalTrajectory()

		// Trajectory signal → trigger brain.
		if !brainBusy && len(trajectoryFindings) > 0 && !triggerBrain {
			triggerBrain = true
			triggerReason = "trajectory signal: " + trajectoryFindings[0].Message
		}

		// CC status.
		ccStatus := ""
		if jsonlPath != "" {
			ccStatus = DetectCCStatus(jsonlPath)
		}

		// Trigger brain analysis if needed.
		if triggerBrain && brain != nil && !brainBusy {
			Debugf("[watcher] triggering brain: %s", triggerReason)
			brainBusy = true
			brainThinking = true

			go func(reason string) {
				defer func() {
					brainBusy = false
					brainThinking = false
				}()

				resp, err := brain.Notify(reason)
				if err != nil {
					// Brain may have crashed — try restart.
					brain.Stop()
					newBrain, restartErr := RestartBrain(cfg, projectDir, jsonlPath, findings.ActiveJSON(), 5*time.Second)
					if restartErr == nil {
						brain = newBrain
					}
					return
				}

				// Process response.
				if resp != nil {
					brainLastMsg = resp.Reasoning
					for _, nudge := range resp.Nudges {
						findings.Add(nudge.Severity, nudge.Message, resp.Reasoning)
					}
					if len(resp.ResolvedFindings) > 0 {
						findings.Resolve(resp.ResolvedFindings)
					}
				}
			}(triggerReason)
		}

		// Check for new CC session.
		if jsonlPath != "" {
			if newPath := CheckForNewSession(projectDir, jsonlPath); newPath != "" {
				jsonlPath = newPath
				if jsonlWatcher != nil {
					jsonlWatcher.Close()
				}
				jsonlWatcher, _ = NewJSONLWatcher(jsonlPath)
			}
		} else {
			// No session yet — keep looking.
			jsonlPath = FindSessionJSONL(projectDir)
			if jsonlPath != "" {
				jsonlWatcher, _ = NewJSONLWatcher(jsonlPath)
				brain, _ = StartBrain(cfg, projectDir, jsonlPath, findings.ActiveJSON())
			}
		}

		// Build display state.
		state := DisplayState{
			ProjectDir:         shortenPath(projectDir),
			Elapsed:            session.Elapsed(),
			ChangedFiles:       changedFiles,
			UntrackedFiles:     untrackedFiles,
			Build:              buildDisplay,
			TrajectoryFindings: trajectoryFindings,
			DeletedTests:       deletedTests,
			BrainFindings:      findings.Recent(10),
			BrainThinking:      brainThinking,
			BrainLastMsg:       brainLastMsg,
			CCStatus:           ccStatus,
		}

		Render(state)

		h := stateHash(state)
		if h != lastLogHash {
			WriteLog(logPath, state)
			lastLogHash = h
		}

		// Wait for next tick or signal.
		select {
		case <-ticker.C:
		case <-sigCh:
			// Graceful shutdown.
			Debugf("[watcher] received SIGINT, shutting down")
			if brain != nil {
				brain.Stop()
			}
			if jsonlWatcher != nil {
				jsonlWatcher.Close()
			}
			fmt.Print("\033[2J\033[H")
			fmt.Printf(" %strupal stopped%s\n", bold, reset)
			fmt.Printf(" %ssession: %s%s\n", dim, session.Elapsed(), reset)
			nFindings := len(findings.Recent(100))
			fmt.Printf(" %s%d findings this session%s\n", dim, nFindings, reset)
			fmt.Printf("\n %slog: .trupal.log%s\n", dim, reset)
			fmt.Printf(" %sdebug: .trupal.debug%s\n", dim, reset)
			// Block forever — keep process alive so tmux doesn't show "Pane is dead".
			// User closes the pane manually (prefix+x or trupal stop --close).
			select {}
		}
	}
}

// --- Helpers (unchanged from MVP) ---

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
	for _, f := range state.DeletedTests {
		h.Write([]byte(f))
	}
	for _, f := range state.BrainFindings {
		h.Write([]byte(f.ID))
		h.Write([]byte(f.Status))
	}
	h.Write([]byte(fmt.Sprintf("%v", state.BrainThinking)))
	return h.Sum64()
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
